package commands_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// pngOf is a PNG of a side×side square.
func pngOf(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: 0x80, B: uint8(y), A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// iconChart is the docs chart with an icon URL.
func iconChart(icon string) testkit.TestChart {
	c := docsChart("1.0.0", docsReadme)
	c.Icon = icon
	return c
}

// A chart's icon is fetched for the window from its URL on the repository's own host (a loopback
// address, allowed for a repository's host), without the repository's credentials, checked and
// shrunk to 256 pixels, kept in memory and in Cogpit's cache folder (owner-only), where a second
// Cogpit finds it without a download. Agents read the icon's URL in object_get, and nothing they
// do fetches it.
func TestChartIcons(t *testing.T) {
	h := newChartHarness(t, nil)
	icon := h.repo.URL + "/icons/docs.png"
	h.repo.Serve("/icons/docs.png", pngOf(t, 512))
	h.repo.Add(iconChart(icon))
	h.addRepo(t)
	h.charts(t, 2)

	res, err := h.dispatch(engine.OriginExternalAgent, "object.get", map[string]any{"ref": docsRef, "noYaml": true})
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(*engine.DetailModel); m.HelmChart == nil || m.HelmChart.Icon != icon || h.repo.Requests("/icons/docs.png") != 0 {
		t.Errorf("object_get: %+v (%d icon requests)", m.HelmChart, h.repo.Requests("/icons/docs.png"))
	}

	ctx := context.Background()
	data, format, err := h.s.ChartIcon(ctx, icon)
	if err != nil || format != "png" {
		t.Fatalf("icon %s %v", format, err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Errorf("kept %+v %v", cfg, err)
	}
	if _, _, err := h.s.ChartIcon(ctx, icon); err != nil || h.repo.Requests("/icons/docs.png") != 1 || h.repo.Authorized("/icons/docs.png") != 0 {
		t.Errorf("downloads %d, with credentials %d (%v)", h.repo.Requests("/icons/docs.png"), h.repo.Authorized("/icons/docs.png"), err)
	}
	kept, _ := filepath.Glob(filepath.Join(h.cacheDir, "helm", "icons", "*.png"))
	if len(kept) != 1 {
		t.Fatalf("kept icons %v", kept)
	}
	assertMode(t, kept[0], 0o600)

	again := engine.NewHelmRepos(h.helmFile.Path, h.cacheDir, engine.WithChartFetcher(&helm.Fetcher{Client: h.repo.Client()}))
	other := newChartHarnessOn(t, again)
	if got, _, err := other.s.ChartIcon(ctx, icon); err != nil || !bytes.Equal(got, data) || h.repo.Requests("/icons/docs.png") != 1 {
		t.Errorf("the second Cogpit: %v (%d downloads)", err, h.repo.Requests("/icons/docs.png"))
	}
}

// What an icon may not be is refused, and the refusal is remembered (no second download): an http
// URL (never requested), a body over 256 KiB, a GIF, an SVG with a script or a reference outside
// itself, an icon behind the repository's login (no credentials go with it), a host on the
// loopback that is no repository's.
func TestChartIconRefusals(t *testing.T) {
	h := newChartHarness(t, nil)
	serve := map[string][]byte{
		"/big.png":    append(pngOf(t, 4), make([]byte, helm.MaxIconBytes)...),
		"/icon.gif":   []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
		"/script.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"/remote.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><image href="https://tracker.example/x.png"/></svg>`),
	}
	for path, data := range serve {
		h.repo.Serve(path, data)
	}
	h.addRepo(t)
	h.charts(t, 1)
	ctx := context.Background()
	for what, tc := range map[string]struct{ url, want string }{
		"http":         {strings.Replace(h.repo.URL, "https://", "http://", 1) + "/icon.png", "https only"},
		"too large":    {h.repo.URL + "/big.png", "larger than the 256.0 KiB"},
		"a GIF":        {h.repo.URL + "/icon.gif", "not a PNG, JPEG or SVG"},
		"a script":     {h.repo.URL + "/script.svg", "<script>"},
		"a remote ref": {h.repo.URL + "/remote.svg", "outside itself"},
		"not own":      {"https://127.0.0.1:1/icon.png", "not at a public address"},
	} {
		if _, _, err := h.s.ChartIcon(ctx, tc.url); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if n := h.repo.Requests("/icon.png"); n != 0 {
		t.Errorf("the http URL was requested %d times", n)
	}
	before := h.repo.Requests("/icon.gif")
	if _, _, err := h.s.ChartIcon(ctx, h.repo.URL+"/icon.gif"); err == nil || h.repo.Requests("/icon.gif") != before {
		t.Errorf("a refused icon downloaded again (%d)", h.repo.Requests("/icon.gif")-before)
	}

	locked := newChartHarness(t, nil)
	locked.repo.Serve("/icons/x.png", pngOf(t, 8))
	locked.repo.RequireBasicAuth("icon-user-71", "pw-icon-72")
	locked.helmFile.Write(&repo.Entry{Name: "test", URL: locked.repo.URL, Username: "icon-user-71", Password: "pw-icon-72"})
	if _, err := locked.dispatch(engine.OriginGUI, "helm.repo.update", struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := locked.s.ChartIcon(ctx, locked.repo.URL+"/icons/x.png"); err == nil || !strings.Contains(err.Error(), "401") ||
		strings.Contains(err.Error(), "pw-icon-72") || locked.repo.Authorized("/icons/x.png") != 0 {
		t.Errorf("an icon behind the login: %v (%d with credentials)", err, locked.repo.Authorized("/icons/x.png"))
	}
}

// "Load chart icons" is on by default; off, no icon is fetched. Only the user turns it on: agents
// and the CLI may only turn it off.
func TestChartIconsPreference(t *testing.T) {
	r := testkit.NewHelmRepo(t, webChartV("0.1.0"))
	r.Serve("/icon.png", pngOf(t, 8))
	file := testkit.NewHelmRepoFile(t)
	file.Write(r.Entry("test"))
	repos := engine.NewHelmRepos(file.Path, t.TempDir(), engine.WithChartFetcher(&helm.Fetcher{Client: r.Client()}))
	s := engine.New(engine.Options{Connector: &chartConnector{src: testkit.NewMemorySource(), actions: &fakeWriter{}},
		SnapshotInterval: 10 * time.Millisecond, HelmRepos: repos, Preferences: engine.NewPreferencesStore(t.TempDir())})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if p, _ := s.Preferences(); !p.LoadChartIcons {
		t.Fatal("chart icons are off by default")
	}
	set := func(origin engine.Origin, on bool) error {
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: "preferences.set", Origin: origin,
			Params: []byte(`{"loadChartIcons":` + map[bool]string{true: "true", false: "false"}[on] + `}`)})
		return err
	}
	if err := set(engine.OriginExternalAgent, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ChartIcon(context.Background(), r.URL+"/icon.png"); !errors.Is(err, engine.ErrChartIconsOff) || r.Requests("/icon.png") != 0 {
		t.Errorf("off: %v (%d requests)", err, r.Requests("/icon.png"))
	}
	for _, origin := range []engine.Origin{engine.OriginExternalAgent, engine.OriginCLI} {
		if err := set(origin, true); !errors.Is(err, engine.ErrChartIconsGUIOnly) {
			t.Errorf("%s turned icons on: %v", origin, err)
		}
	}
	if err := set(engine.OriginGUI, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ChartIcon(context.Background(), r.URL+"/icon.png"); err != nil || r.Requests("/icon.png") != 1 {
		t.Errorf("on again: %v (%d requests)", err, r.Requests("/icon.png"))
	}
}
