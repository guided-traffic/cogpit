package fyneui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// settingsEngineWithFile is settingsEngine whose contexts come from a kubeconfig file in a folder
// Cogpit reads (its default ~/.kube), so that a context names its file; it returns kind-dev's id
// and the file.
func settingsEngineWithFile(t *testing.T) (s *engine.Session, ctx, file string) {
	t.Helper()
	kube := filepath.Join(t.TempDir(), "home", ".kube")
	file = filepath.Join(kube, "config")
	if err := os.MkdirAll(kube, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	s = newTestEngine(engine.Options{Connector: &engine.ClusterConnector{Sources: engine.NewKubeconfigSources(cfg, engine.WithDefaultFolder(kube))},
		Preferences: engine.NewPreferencesStore(cfg), ClusterSettings: engine.NewClusterSettingsStore(cfg), Hotbar: engine.NewHotbar(cfg)})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctxs, err := s.Contexts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ctxs {
		if c.KubeContext == "kind-dev" && c.File != "" {
			return s, c.Name, file
		}
	}
	t.Fatalf("no kind-dev from a file among %+v", ctxs)
	return nil, "", ""
}

// openGeneral opens a context's Settings › General on the real engine and waits for its view.
func openGeneral(t *testing.T, s *engine.Session, ctx string, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": ctx})
	q.until(t, "the General section", func() bool {
		return sh.settings.visible() && sh.settings.view != nil && sh.settings.view.Context == ctx &&
			sh.settings.controls[accentNoneSwatchID] != nil
	})
	return sh, w, q
}

// accentSwatches are the accent row's swatches: None, the eight, the free colour's.
func accentSwatches(t *testing.T, sh *shell) (none *noneSwatch, eight []*swatch, custom *swatch) {
	t.Helper()
	row := sh.settings.controls[accentSetting].(*fyne.Container)
	none = row.Objects[0].(*noneSwatch)
	for _, o := range row.Objects[1 : 1+len(engine.IconColors)] {
		eight = append(eight, o.(*swatch))
	}
	return none, eight, sh.settings.controls["accentCustom"].(*swatch)
}

// TestClusterSettingsAccentColor (D-80, F26): General's Accent colour row starts at None; a tap on
// a swatch sets the cluster's accent colour through the GUI-only command, kept and shown at once; a
// free colour shows as its own swatch; None clears it. The icon colour stays as it was.
func TestClusterSettingsAccentColor(t *testing.T) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "iconColor": "blue"})
	sh, _, q := openGeneral(t, s, "kind-dev", ThemeDark)
	none, eight, custom := accentSwatches(t, sh)
	if !none.selected || custom.Visible() {
		t.Fatalf("a cluster without an accent colour: None selected %v, the free swatch shown %v", none.selected, custom.Visible())
	}
	red := eight[4]
	if red.color != "red" {
		t.Fatalf("the fifth swatch is %q", red.color)
	}
	test.Tap(red)
	q.until(t, "red kept and shown", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.AccentColor == "red" && red.selected && !none.selected
	})
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.IconColor != "blue" {
		t.Errorf("the icon colour moved: %+v", cs)
	}

	mustDispatch(t, s, "cluster.settings.setAccentColor", map[string]any{"context": "kind-dev", "color": "#FFE066"})
	q.until(t, "the free colour's swatch", func() bool {
		return custom.Visible() && custom.selected && custom.color == "#ffe066" && !red.selected
	})

	test.Tap(none)
	q.until(t, "none again", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.AccentColor == "" && none.selected && !custom.Visible()
	})
}

// TestClusterSettingsCopyNameAndPath (F27): the cluster name's field and the kubeconfig's path have
// the drawer's copy icon; a click copies what the field shows (the name typed, else the context's
// name it shows as its placeholder) and the file's whole path, turns the icon into the checkmark
// and says so in a toast.
func TestClusterSettingsCopyNameAndPath(t *testing.T) {
	s, ctx, file := settingsEngineWithFile(t)
	sh, _, q := openGeneral(t, s, ctx, ThemeDark)
	nameCopy := sh.settings.controls["nameCopy"].(*copyButton)
	pathCopy := sh.settings.controls["kubeconfigCopy"].(*copyButton)
	q.until(t, "the placeholder and the path", func() bool {
		return entryOf(sh.settings.controls["name"]).PlaceHolder == "kind-dev" && pathCopy.Visible()
	})
	if !nameCopy.Visible() || !nameCopy.pinned {
		t.Fatal("the name's copy icon is not shown")
	}

	test.Tap(nameCopy)
	if got := sh.app.Clipboard().Content(); got != "kind-dev" {
		t.Errorf("copied %q, want the context's name the field shows", got)
	}
	if !nameCopy.done {
		t.Error("no checkmark after the copy")
	}
	q.until(t, "the toast", func() bool { return strings.Contains(toastTexts(sh), copiedToast) })

	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": ctx, "name": "Dev cluster"})
	q.until(t, "the name shown", func() bool { return entryOf(sh.settings.controls["name"]).Text == "Dev cluster" })
	test.Tap(nameCopy)
	if got := sh.app.Clipboard().Content(); got != "Dev cluster" {
		t.Errorf("copied %q, want the display name", got)
	}

	test.Tap(pathCopy)
	if got := sh.app.Clipboard().Content(); got != file || !pathCopy.done {
		t.Errorf("copied %q, want the kubeconfig's whole path %q (checkmark %v)", got, file, pathCopy.done)
	}
}

// A context merged from the default files names no file of its own: the box says so in a
// sentence, which has no copy icon.
func TestClusterSettingsNoPathNoCopy(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := openGeneral(t, s, "kind-dev", ThemeDark)
	pathCopy := sh.settings.controls["kubeconfigCopy"].(*copyButton)
	q.until(t, "the view", func() bool { return sh.settings.view != nil && sh.settings.view.Kubeconfig == "" })
	q.drain()
	if pathCopy.Visible() {
		t.Error("a copy icon by the sentence")
	}
	if !sh.settings.controls["nameCopy"].(*copyButton).Visible() {
		t.Error("the name has no copy icon")
	}
}

// The General section with an accent colour, a display name and the copy icons, the name's just
// used (its checkmark and the toast): the PNGs of the pull request.
func TestRenderClusterSettingsAccentDark(t *testing.T) {
	renderClusterSettingsAccent(t, ThemeDark, "cluster_settings_accent_dark.png")
}

func renderClusterSettingsAccent(t *testing.T, mode ThemeMode, name string) {
	s, ctx, _ := settingsEngineWithFile(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": ctx, "name": "Dev cluster", "iconColor": "violet"})
	mustDispatch(t, s, "cluster.settings.setAccentColor", map[string]any{"context": ctx, "color": "red"})
	sh, w, q := openGeneral(t, s, ctx, mode)
	_, eight, _ := accentSwatches(t, sh)
	q.until(t, "red selected", func() bool {
		e := entryOf(sh.settings.controls["name"])
		return eight[4].selected && e != nil && e.Text == "Dev cluster"
	})
	test.Tap(sh.settings.controls["nameCopy"].(*copyButton))
	q.drain()
	savePNG(t, w, name)
}
