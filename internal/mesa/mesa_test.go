package mesa

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Cogpit names llvmpipe only when the opengl32.dll it loaded lies beside its executable and the
// user named no driver; the folders compare without case.
func TestSoftGLOnlyForMesaBesideCogpit(t *testing.T) {
	dir := filepath.Join("C:", "Users", "someone", "AppData", "Local", "Programs", "Cogpit")
	exe := filepath.Join(dir, "cogpit.exe")
	beside := filepath.Join(dir, "opengl32.dll")
	for _, c := range []struct {
		name, dll, exe, driver string
		mesa                   bool
		set                    string
	}{
		{"Mesa beside, no driver named", beside, exe, "", true, softGLDriver},
		{"the folder in another case", filepath.Join(strings.ToUpper(dir), "OPENGL32.DLL"), exe, "", true, softGLDriver},
		{"the user's driver stays", beside, exe, "d3d12", true, ""},
		{"the system's OpenGL", filepath.Join("C:", "Windows", "System32", "opengl32.dll"), exe, "", false, ""},
		{"in a folder below Cogpit's", filepath.Join(dir, "mesa", "opengl32.dll"), exe, "", false, ""},
		{"no opengl32.dll loaded", "", exe, "", false, ""},
		{"no executable", beside, "", "", false, ""},
	} {
		if mesa, set := softGL(c.dll, c.exe, c.driver); mesa != c.mesa || set != c.set {
			t.Errorf("%s: softGL = %v, %q, want %v, %q", c.name, mesa, set, c.mesa, c.set)
		}
	}
}

// watched logs each line through a Watch, as slog's bridge from the standard log package turns
// each of Fyne's lines into a record, and returns what fired.
func watched(lines ...string) []Failure {
	var got []Failure
	l := slog.New(Watch(slog.NewTextHandler(io.Discard, nil), func(f Failure) { got = append(got, f) }))
	for _, line := range lines {
		l.Info(line)
	}
	return got
}

// The first of Fyne's GL reports fires once, with its cause; Fyne's other errors and other lines
// do not; a report whose next line is no cause fires without one.
func TestWatchFiresOnFyneGLFailures(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  []Failure
	}{
		{"GLFW refused the context", []string{"cogpit started", "Fyne error:  window creation error",
			"  Cause: Requested OpenGL version 2.1, got version 1.1", "  At: window_desktop.go:843"},
			[]Failure{{"window creation error", "Requested OpenGL version 2.1, got version 1.1"}}},
		{"a function missing, twice", []string{"Fyne error:  failed to initialise OpenGL", "  Cause: glAccum",
			"Fyne error:  failed to initialise OpenGL", "  Cause: glAccum"},
			[]Failure{{"failed to initialise OpenGL", "glAccum"}}},
		{"a shader", []string{"Fyne error:  Failed to compile shader simple", "  Cause: 0:1(10): error"},
			[]Failure{{"Failed to compile shader simple", "0:1(10): error"}}},
		{"no cause line", []string{"Fyne error:  failed to initialise OpenGL", "  At: gl_core.go:75"},
			[]Failure{{Reason: "failed to initialise OpenGL"}}},
		{"another of Fyne's errors", []string{"Fyne error:  Failed to set dark mode", "  Cause: access denied"}, nil},
		{"another error before a GL one's cause", []string{"Fyne error:  failed to initialise OpenGL",
			"Fyne error:  Failed to set dark mode", "  Cause: access denied"}, nil},
	} {
		if got := watched(c.lines...); len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
			t.Errorf("%s: fired %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The handlers WithAttrs and WithGroup make share the watch: a report split across them fires once.
func TestWatchIsSharedByDerivedHandlers(t *testing.T) {
	var got []Failure
	h := Watch(slog.NewTextHandler(io.Discard, nil), func(f Failure) { got = append(got, f) })
	slog.New(h.WithGroup("g")).Info("Fyne error:  window creation error")
	slog.New(h.WithAttrs([]slog.Attr{slog.String("k", "v")})).Info("  Cause: no context")
	slog.New(h).Info("Fyne error:  window creation error")
	slog.New(h).Info("  Cause: again")
	if len(got) != 1 || got[0] != (Failure{"window creation error", "no context"}) {
		t.Errorf("fired %+v", got)
	}
}

// Watch hands every record on before it fires: the report is in the log when the box opens.
func TestWatchLogsBeforeItFires(t *testing.T) {
	var logged []string
	h := Watch(recorder{&logged}, func(Failure) {
		if len(logged) != 2 {
			t.Errorf("fired with %q logged", logged)
		}
	})
	slog.New(h).Info("Fyne error:  window creation error")
	slog.New(h).Info("  Cause: no context")
}

type recorder struct{ lines *[]string }

func (recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r recorder) Handle(_ context.Context, rec slog.Record) error {
	*r.lines = append(*r.lines, rec.Message)
	return nil
}
func (r recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r recorder) WithGroup(string) slog.Handler      { return r }

func TestPin(t *testing.T) {
	p := Pin{Version: "26.2.4", SHA256: "351f"}
	if got := p.URL(); got != "https://github.com/pal1000/mesa-dist-win/releases/download/26.2.4/mesa3d-26.2.4-release-msvc.7z" {
		t.Errorf("URL = %q", got)
	}
	if got := (Pin{}).Archive(); got != "mesa3d-<version>-release-msvc.7z" {
		t.Errorf("Archive without a version = %q", got)
	}
	if _, ok := pinned(); ok {
		t.Error("a test build is pinned: the linker sets the pin only for a release cogpit.exe")
	}
}

// The boxes name the failure, the folder, the archive and the release page, and the download's box
// opens with its longest line, which the progress lines after it fit into.
func TestDialogTexts(t *testing.T) {
	f := Failure{"failed to initialise OpenGL", "glAccum"}
	p := Pin{Version: "26.2.4", SHA256: "351f"}
	dir := `C:\Users\someone\AppData\Local\Programs\Cogpit`
	for name, text := range map[string]string{
		"ask":    askText(f, p, dir),
		"manual": manualText(f, p, dir, "This build of Cogpit carries no Mesa release to download."),
	} {
		for _, want := range []string{"failed to initialise OpenGL: glAccum", dir, "mesa3d-26.2.4-release-msvc.7z"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s names no %q:\n%s", name, want, text)
			}
		}
	}
	if !strings.Contains(manualText(f, p, dir, "why"), releasesURL) {
		t.Error("the steps by hand name no release page")
	}
	offline := errors.New(`Get "https://github.com/pal1000/mesa-dist-win/releases/download/26.2.4/mesa3d-26.2.4-release-msvc.7z": dial tcp: lookup github.com: no such host`)
	if w := failedWhy(offline, false); !strings.HasPrefix(w, "Downloading Mesa failed:\n") || !strings.Contains(w, "lookup github.com: no such host") {
		t.Errorf("a failed download: %q", w)
	}
	if w := failedWhy(errors.New("mesa3d-26.2.4-release-msvc.7z holds no x64/opengl32.dll"), true); !strings.HasPrefix(w, "Cogpit downloaded Mesa, but unpacking it failed:\n") {
		t.Errorf("a failed unpacking: %q", w)
	}
	if b := besideText(f, dir+`\opengl32.dll`, `C:\x\cogpit.log`); !strings.Contains(b, dir+`\opengl32.dll`) || !strings.Contains(b, `C:\x\cogpit.log`) {
		t.Errorf("beside:\n%s", b)
	}
	lines := strings.Split(progressText(p, "Unpacking opengl32.dll and libgallium_wgl.dll"), "\n")
	for _, l := range lines[1:] {
		if len(l) > len(lines[0]) {
			t.Errorf("%q is longer than the box's first line %q", l, lines[0])
		}
	}
}

// The Makefile's release build of cogpit.exe writes the pin into version and archiveSHA256 (the
// linker ignores a -X for a missing symbol), and its MESA_VERSION and MESA_SHA256 have the forms
// the URL and the check need.
func TestMakefileBuildsThePinIntoTheApp(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("mesa.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^MESA_PKG\s*:=\s*github\.com/guided-traffic/cogpit/internal/mesa\s*$`).Match(mk) {
		t.Error("the Makefile's MESA_PKG is not this package")
	}
	for v, from := range map[string]string{"version": "MESA_VERSION", "archiveSHA256": "MESA_SHA256"} {
		if !strings.Contains(string(mk), "-X $(MESA_PKG)."+v+"=$("+from+")") {
			t.Errorf("the Makefile's WIN_APP_LDFLAGS set no %s from %s", v, from)
		}
		if !regexp.MustCompile(`(?m)^\s+` + v + `\s+string$`).Match(src) {
			t.Errorf("mesa.go declares no string variable %s", v)
		}
	}
	if !regexp.MustCompile(`(?m)^MESA_VERSION\s*:=\s*[0-9]+(\.[0-9]+)+\s*$`).Match(mk) {
		t.Error("the Makefile's MESA_VERSION is not digits and dots")
	}
	if !regexp.MustCompile(`(?m)^MESA_SHA256\s*:=\s*[0-9a-f]{64}\s*$`).Match(mk) {
		t.Error("the Makefile's MESA_SHA256 is not 64 lower-case hex digits")
	}
}
