package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const devCtx = "dev [.kube/config]"

// Settings are kept per context, owner-only, read back by the next start; a context back at its
// defaults leaves the file.
func TestClusterSettingsPersistPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	if len(s.UIState().ClusterSettings) != 0 {
		t.Fatalf("a first start has settings: %+v", s.UIState().ClusterSettings)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory exists before the first change: %v", err)
	}
	set := func(ctx string, fn func(*ClusterSettings)) {
		t.Helper()
		if _, err := s.ChangeClusterSettings(ctx, func(c *ClusterSettings) error { fn(c); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	set(devCtx, func(c *ClusterSettings) {
		c.Name, c.IconColor, c.TerminalDir, c.TerminalNamespace = "  Dev  ", "violet", "$HOME/src", "team-a"
	})
	set("prod", func(c *ClusterSettings) { c.Name = "Production" })
	set("prod", func(c *ClusterSettings) { c.Name = "" })
	got := s.UIState().ClusterSettings
	if len(got) != 1 || got[devCtx].Name != "Dev" || got[devCtx].IconColor != "violet" {
		t.Errorf("UI state %+v: want dev alone, its name trimmed", got)
	}
	s.Close()

	path := filepath.Join(dir, ClusterSettingsFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	s2 := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	defer s2.Close()
	cs, _ := s2.ClusterSettingsOf(devCtx)
	if !cs.Equal(ClusterSettings{Name: "Dev", IconColor: "violet", TerminalDir: "$HOME/src", TerminalNamespace: "team-a"}) {
		t.Errorf("after a restart: %+v", cs)
	}
	if v, _ := s2.ClusterSettingsView("prod"); v.Name != "prod" || v.NameSet || !v.Kept {
		t.Errorf("prod's view: %+v", v)
	}
}

func TestClusterSettingsUnreadableFileIsKeptAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ClusterSettingsFile)
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	defer s.Close()
	if _, err := s.ChangeClusterSettings(devCtx, func(c *ClusterSettings) error { c.Name = "Dev"; return nil }); err != nil {
		t.Fatal(err)
	}
	if kept, err := os.ReadFile(path + ".unreadable"); err != nil || string(kept) != "{nope" {
		t.Errorf("the unreadable file was not kept aside: %q, %v", kept, err)
	}
}

// Fields out of bounds in the file are dropped one by one; Check refuses them on a change.
func TestClusterSettingsCheckAndNormalize(t *testing.T) {
	dir := t.TempDir()
	doc := `{"version":1,"clusters":{"dev":{"name":"Dev","iconColor":"pink","icon":"../../etc/passwd",` +
		`"terminalDir":"relative/dir","terminalNamespace":"Team_A"},"":{"name":"nobody"},"ok":{"iconColor":"green"}}}`
	if err := os.WriteFile(filepath.Join(dir, ClusterSettingsFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{ClusterSettings: NewClusterSettingsStore(dir)})
	defer s.Close()
	all := s.UIState().ClusterSettings
	if len(all) != 2 || !all["dev"].Equal(ClusterSettings{Name: "Dev"}) || all["ok"].IconColor != "green" {
		t.Errorf("normalized %+v", all)
	}
	for name, fn := range map[string]func(*ClusterSettings){
		"colour":    func(c *ClusterSettings) { c.IconColor = "pink" },
		"icon":      func(c *ClusterSettings) { c.Icon = "../x.png" },
		"name":      func(c *ClusterSettings) { c.Name = "two\nlines" },
		"long name": func(c *ClusterSettings) { c.Name = strings.Repeat("x", 129) },
		"dir":       func(c *ClusterSettings) { c.TerminalDir = "src" },
		"namespace": func(c *ClusterSettings) { c.TerminalNamespace = "Team_A" },
	} {
		if _, err := s.ChangeClusterSettings("dev", func(c *ClusterSettings) error { fn(c); return nil }); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := s.ChangeClusterSettings("", func(*ClusterSettings) error { return nil }); err == nil {
		t.Error("an empty context accepted")
	}
	abs := "/opt/work"
	if runtime.GOOS == "windows" {
		abs = `C:\work` // /opt/work has no drive there: not absolute
	}
	for _, dir := range []string{"", "$HOME", "~", "~/src", "$HOME/src", abs} {
		if err := checkTerminalDir(dir); err != nil {
			t.Errorf("%q: %v", dir, err)
		}
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// hugePNGHeader is a PNG whose header claims 100,000 × 100,000 pixels: a decompression bomb that
// must be refused from its header, before any pixel is decoded.
func hugePNGHeader() []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 100000)
	binary.BigEndian.PutUint32(ihdr[4:], 100000)
	ihdr[8], ihdr[9] = 8, 6
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// Uploaded images are checked and kept as Cogpit's own copy: a PNG or JPEG as a PNG of at most 256
// pixels a side, an SVG as it is; what is not an image, too large or an SVG with scripts or
// outside references is refused.
func TestCheckIcon(t *testing.T) {
	big := pngOf(t, 600, 300)
	data, format, err := CheckIcon(big)
	if err != nil || format != "png" {
		t.Fatalf("PNG: %s, %v", format, err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 256 || cfg.Height != 128 {
		t.Errorf("a 600×300 PNG kept as %+v, %v; want 256×128", cfg, err)
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 40, 40)), nil); err != nil {
		t.Fatal(err)
	}
	if _, format, err := CheckIcon(jb.Bytes()); err != nil || format != "png" {
		t.Errorf("JPEG: %s, %v", format, err)
	}
	svg := `<?xml version="1.0"?><!-- c --><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><defs><linearGradient id="g"/></defs>` +
		`<rect width="24" height="24" fill="url(#g)"/><use href="#g"/></svg>`
	if data, format, err := CheckIcon([]byte(svg)); err != nil || format != "svg" || string(data) != svg {
		t.Errorf("SVG: %s, %v", format, err)
	}
	for name, raw := range map[string]string{
		"text":       "hello",
		"bomb":       string(hugePNGHeader()),
		"truncated":  string(big[:100]),
		"doctype":    `<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY x "y">]><svg>&x;</svg>`,
		"script":     `<svg><script>alert(1)</script></svg>`,
		"handler":    `<svg onload="alert(1)"/>`,
		"href":       `<svg><image href="file:///etc/passwd"/></svg>`,
		"xlink":      `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="https://example.com/x.svg#a"/></svg>`,
		"css url":    `<svg><rect style="fill:url(https://example.com/x)"/></svg>`,
		"style url":  `<svg><style>rect{fill:url( 'http://x/y')}</style></svg>`,
		"foreign":    `<svg><foreignObject><div/></foreignObject></svg>`,
		"not svg":    `<?xml version="1.0"?><html/>`,
		"two roots":  `<svg/><svg/>`,
		"pi":         `<svg><?php echo 1 ?></svg>`,
		"malformed":  `<svg><g></svg>`,
		"text after": `<svg/>trailing`,
	} {
		if _, _, err := CheckIcon([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	deep := strings.Repeat("<g>", 70) + strings.Repeat("</g>", 70)
	if _, _, err := CheckIcon([]byte("<svg>" + deep + "</svg>")); err == nil {
		t.Error("a deeply nested SVG accepted")
	}
}

// An upload is kept in cluster-icons/ under its hash, owner-only; it replaces the colour; the
// image goes once no context uses it; ClusterIcon checks the kept copy again.
func TestClusterIconUpload(t *testing.T) {
	cfg := t.TempDir()
	s := New(Options{ClusterSettings: NewClusterSettingsStore(cfg)})
	defer s.Close()
	src := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(src, pngOf(t, 64, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings(devCtx, func(c *ClusterSettings) error { c.IconColor = "red"; return nil }); err != nil {
		t.Fatal(err)
	}
	cs, err := s.SetClusterIcon(devCtx, src)
	if err != nil {
		t.Fatal(err)
	}
	if cs.IconColor != "" || !validIconFile(cs.Icon) || !strings.HasSuffix(cs.Icon, ".png") {
		t.Errorf("after the upload: %+v", cs)
	}
	kept := filepath.Join(cfg, ClusterIconsDir, cs.Icon)
	fi, err := os.Stat(kept)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("icon mode %v", fi.Mode().Perm())
	}
	data, format, err := s.ClusterIcon(devCtx)
	if err != nil || format != "png" || len(data) == 0 {
		t.Errorf("ClusterIcon: %d bytes, %s, %v", len(data), format, err)
	}
	if v, _ := s.ClusterSettingsView(devCtx); v.IconImage != "png" {
		t.Errorf("view %+v", v)
	}

	// The same image for a second context shares the file; the file goes with the last user.
	if _, err := s.SetClusterIcon("prod", src); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings(devCtx, func(c *ClusterSettings) error { c.Icon = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("the icon prod still uses was removed: %v", err)
	}
	if _, err := s.ChangeClusterSettings("prod", func(c *ClusterSettings) error { c.Icon, c.IconColor = "", "green"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("an unused icon stays: %v", err)
	}

	// A kept copy replaced behind Cogpit's back is not handed to the GUI.
	if _, err := s.SetClusterIcon(devCtx, src); err != nil {
		t.Fatal(err)
	}
	cs, _ = s.ClusterSettingsOf(devCtx)
	if err := os.WriteFile(filepath.Join(cfg, ClusterIconsDir, cs.Icon), hugePNGHeader(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClusterIcon(devCtx); err == nil {
		t.Error("a foreign file in cluster-icons was handed out")
	}

	// Not an image: refused, nothing changes, nothing is kept.
	txt := filepath.Join(t.TempDir(), "notes.png")
	if err := os.WriteFile(txt, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ClusterSettingsOf("prod")
	if _, err := s.SetClusterIcon("prod", txt); err == nil {
		t.Error("a text file accepted as an icon")
	}
	if after, _ := s.ClusterSettingsOf("prod"); !after.Equal(before) {
		t.Errorf("a refused upload changed the settings: %+v", after)
	}
	if _, err := s.SetClusterIcon("prod", filepath.Join(t.TempDir(), "missing.png")); err == nil || strings.Contains(err.Error(), t.TempDir()) {
		t.Errorf("a missing file: %v", err)
	}
}

// Without a configuration directory the settings last for the run, and an icon cannot be kept.
func TestClusterSettingsInMemory(t *testing.T) {
	s := New(Options{ClusterSettings: NewClusterSettingsStore("")})
	defer s.Close()
	if _, err := s.ChangeClusterSettings(devCtx, func(c *ClusterSettings) error { c.Name = "Dev"; return nil }); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.ClusterSettingsView(devCtx); v.Name != "Dev" || v.Kept {
		t.Errorf("in memory: %+v", v)
	}
	if _, err := s.SetClusterIcon(devCtx, "/dev/null"); err == nil {
		t.Error("an icon accepted without a folder to keep it in")
	}
}

// A cluster colour may be one of the eight accents or any "#rrggbb" (A93); anything else is
// refused.
func TestCustomIconColor(t *testing.T) {
	for c, want := range map[string]bool{"#1a2b3c": true, "#ffffff": true, "#FFFFFF": false, "#12345": false, "1a2b3c": false, "#1a2b3g": false} {
		if got := IsCustomIconColor(c); got != want {
			t.Errorf("IsCustomIconColor(%q) = %v, want %v", c, got, want)
		}
		if err := (ClusterSettings{IconColor: c}).Check(); (err == nil) != want {
			t.Errorf("Check(%q): %v", c, err)
		}
	}
}
