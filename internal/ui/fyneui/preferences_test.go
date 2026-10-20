package fyneui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// settingsKubeconfig offers three contexts whose servers nobody answers: the settings pages work
// on contexts without connecting to them.
const settingsKubeconfig = `apiVersion: v1
kind: Config
clusters:
- {name: dev, cluster: {server: "https://127.0.0.1:1"}}
users:
- {name: dev, user: {token: t}}
contexts:
- {name: kind-dev, context: {cluster: dev, user: dev, namespace: team-a}}
- {name: prod-eu-central, context: {cluster: dev, user: dev}}
- {name: staging, context: {cluster: dev, user: dev}}
current-context: kind-dev
`

// settingsEngine is a real engine whose preferences, cluster settings and hotbar live in a
// temporary folder, over a kubeconfig file it never connects with.
func settingsEngine(t *testing.T) (*engine.Session, string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	kc := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kc, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kc}},
		Preferences: engine.NewPreferencesStore(cfg), ClusterSettings: engine.NewClusterSettingsStore(cfg),
		Hotbar: engine.NewHotbar(cfg)})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, cfg
}

// newEngineShell starts a shell on the real engine in a 1440×900 offscreen window, as newTestShell
// does on the fake; without a connection it starts on the welcome page (D-42).
func newEngineShell(t *testing.T, s *engine.Session, opts Options) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(opts.Theme))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, opts, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir
	sh.systemReduce = func() bool { return false } // the computer's Reduce motion stays out of the tests
	sh.confirmer.armDelay = 0
	sh.settings.typing = time.Millisecond
	sh.settings.openFolder = func(string) {}
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.SetConfirmer(nil)
	})
	return sh, w, q
}

func mustDispatch(t *testing.T, s *engine.Session, id string, params any) {
	t.Helper()
	raw := mustJSON(t, params)
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func pressEscape(w fyne.Window) { w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape}) }

var (
	cmdComma      = &desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault}
	cmdShiftComma = &desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift}
)

// ⌘, opens the Preferences page from the window's menu (so from any focus), in place of the
// views; choosing Light switches the window at once and is kept in Cogpit's own file; Escape
// closes the page and the views are back.
func TestPreferencesOpenWithTheShortcutAndSwitchTheTheme(t *testing.T) {
	s, cfg := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	item := menuItemWith(sh.mainMenu(), cmdComma)
	if item == nil || item.Label != "Preferences…" {
		t.Fatalf("⌘, on %+v, want Preferences…", item)
	}
	item.Action()
	q.until(t, "the Preferences page", func() bool { return sh.settings.visible() && sh.center.Objects[0] == sh.settings.root })
	if st := sh.ui.Settings; st == nil || st.Page != engine.SettingsPreferences || st.Section != "app" {
		t.Errorf("UI state %+v", st)
	}
	if sh.settings.title.text != "Preferences" || sh.settings.badge.Visible() {
		t.Errorf("title %q", sh.settings.title.text)
	}
	if th := sh.app.Settings().Theme().(*cogTheme); th.mode != ThemeSystem { // the default follows the computer (A94)
		t.Fatalf("starts %s", th.mode)
	}

	sel := sh.settings.controls["theme"].(*selectField)
	if sel.Selected != "Sync with computer" {
		t.Errorf("theme shows %q", sel.Selected)
	}
	sel.SetSelected("Light")
	q.until(t, "the light theme", func() bool {
		th, ok := sh.app.Settings().Theme().(*cogTheme)
		return ok && th.mode == ThemeLight
	})
	if bg := sh.app.Settings().Theme().Color(theme.ColorNameBackground, theme.VariantDark); bg != lightPalette.fyne[theme.ColorNameBackground] {
		t.Errorf("background %v after Light", bg)
	}
	if p, _ := s.Preferences(); p.Theme != engine.ThemeLight {
		t.Errorf("kept %q", p.Theme)
	}
	if _, err := os.Stat(filepath.Join(cfg, engine.PreferencesFile)); err != nil {
		t.Errorf("preferences.json: %v", err)
	}

	pressEscape(w)
	q.until(t, "the page closed", func() bool { return !sh.settings.visible() && sh.center.Objects[0] == sh.mainRow })
	if sh.ui.Settings != nil {
		t.Errorf("UI state still has %+v", sh.ui.Settings)
	}
}

// --theme wins for the run: the preference is kept, the window keeps the flag's theme and the
// page says so.
func TestThemeFlagOverridesThePreference(t *testing.T) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": "light"})
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	q.drain()
	if th := sh.app.Settings().Theme().(*cogTheme); th.mode != ThemeDark {
		t.Errorf("--theme dark, preference light: %s", th.mode)
	}
	mustDispatch(t, s, "preferences.open", struct{}{})
	q.until(t, "the page", sh.settings.visible)
	if !sh.settings.visible() || sh.settings.controls["theme"] == nil {
		t.Fatal("no theme control")
	}
}

// The other preferences reach the window: the tables' row height through the theme, the time zone
// in the charts, the editor's settings in every editor view, the terminal's font size in the
// terminals opened afterwards; text fields set their value after a typing pause, and the engine's
// refusal shows under the field.
func TestPreferencesApplyToTablesEditorsAndTerminals(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.set", map[string]any{"tableRowHeight": 36, "timezone": "Europe/Berlin",
		"editorFontSize": 16, "editorLineNumbers": false, "editorTabSize": 4, "terminalFontSize": 18})
	q.until(t, "the row height", func() bool {
		th, ok := sh.app.Settings().Theme().(*cogTheme)
		return ok && th.rowHeight == 36
	})
	th := sh.app.Settings().Theme()
	if h := rowTextHeight() + 2*th.Size(sizeRowPadding); h < 35 || h > 37 {
		t.Errorf("a row is %v points high, want 36", h)
	}
	if sh.loc().String() != "Europe/Berlin" {
		t.Errorf("charts in %s", sh.loc())
	}
	v := yamlview.New()
	sh.applyEditorPrefs(v)
	if v.TextSize() != 16 || v.LineNumbers() || v.TabSize() != 4 {
		t.Errorf("editor: size %v, numbers %v, tab %d", v.TextSize(), v.LineNumbers(), v.TabSize())
	}
	scoped, ok := sh.terminalScope(widget.NewLabel("$")).(*container.ThemeOverride)
	if !ok || scoped.Theme.Size(theme.SizeNameText) != 18 || scoped.Theme.Size(theme.SizeNamePadding) != th.Size(theme.SizeNamePadding) {
		t.Errorf("terminal scope %T", sh.terminalScope(widget.NewLabel("$")))
	}

	mustDispatch(t, s, "preferences.open", map[string]any{"section": "terminal"})
	q.until(t, "the Terminal section", func() bool { return sh.settings.controls["terminalShell"] != nil })
	shellEntry := sh.settings.controls["terminalShell"].(*widget.Entry)
	test.Type(shellEntry, "/no/such/shell")
	q.until(t, "the refusal", func() bool { return sh.settings.errs["terminalShell"].Visible() })
	shellEntry.SetText("/bin/sh")
	args := sh.settings.controls["terminalShellArgs"].(*widget.Entry)
	args.SetText("-l\n\n--norc\n")
	q.until(t, "the shell kept", func() bool {
		p, _ := s.Preferences()
		return p.TerminalShell == "/bin/sh" && slices.Equal(p.TerminalShellArgs, []string{"-l", "--norc"})
	})
	q.until(t, "the refusal cleared", func() bool { return !sh.settings.errs["terminalShell"].Visible() })
}

// pngFile writes a small, colourful PNG and returns its path.
func pngFile(t *testing.T) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := range 48 {
		for x := range 48 {
			c := color.NRGBA{R: 0xd3, G: 0x36, B: 0x82, A: 0xff}
			if (x/12+y/12)%2 == 0 {
				c = color.NRGBA{R: 0x26, G: 0x8b, B: 0xd2, A: 0xff}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tileFor(sh *shell, ctx string) *hotbarTile {
	for _, tl := range tilesOf(sh.hot) {
		if tl.tile.Context == ctx {
			return tl
		}
	}
	return nil
}

func tileRenderer(tl *hotbarTile) *hotbarTileRenderer {
	return test.WidgetRenderer(tl).(*hotbarTileRenderer)
}

// A cluster's Settings page from its menu in the navigator's section: its name and colour show at
// once on its hotbar tile and in the section; an uploaded image replaces the initials; the
// terminal's working directory is what the cluster's next local terminal starts in; nothing of it
// is in the kubeconfig.
func TestClusterSettingsFromTheSectionSetIdentityAndTerminal(t *testing.T) {
	s, cfg := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "hotbar.addCluster", map[string]any{"context": "kind-dev"})
	q.until(t, "the tile", func() bool { return tileFor(sh, "kind-dev") != nil })

	n := sh.side.sec
	q.until(t, "the rows", func() bool { return n.loaded && len(n.rows) == 3 })
	n.rowMenu("kind-dev", fyne.NewPos(10, 10))
	var settings *fyne.MenuItem
	for _, it := range n.menuModel.Items {
		if it.Label == "Settings…" {
			settings = it
		}
	}
	if settings == nil {
		t.Fatalf("the section's row menu %+v", n.menuModel.Items)
	}
	settings.Action()
	q.until(t, "the cluster's page", func() bool {
		return sh.settings.visible() && sh.settings.view != nil && sh.settings.view.Context == "kind-dev"
	})
	if sh.settings.title.text != "Settings - kind-dev" || !sh.settings.badge.Visible() {
		t.Errorf("title %q", sh.settings.title.text)
	}

	name := sh.settings.controls["name"].(*widget.Entry)
	if name.PlaceHolder != "kind-dev" {
		t.Errorf("name placeholder %q", name.PlaceHolder)
	}
	test.Type(name, "Dev cluster")
	q.until(t, "the tile's name", func() bool { return tileRenderer(tileFor(sh, "kind-dev")).id.name == "Dev cluster" })
	if sh.settings.title.text != "Settings - Dev cluster" {
		t.Errorf("title %q", sh.settings.title.text)
	}

	sw := sh.settings.controls["swatches"].(*fyne.Container).Objects[2].(*swatch) // violet
	test.Tap(sw)
	violet := themeColor(sh.app.Settings().Theme(), identityColorName("violet"))
	q.until(t, "the tile's colour", func() bool { return tileRenderer(tileFor(sh, "kind-dev")).initials.Color == violet })
	if !sw.selected {
		t.Error("the violet swatch is not marked")
	}
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.Name != "Dev cluster" || cs.IconColor != "violet" {
		t.Errorf("kept %+v", cs)
	}
	q.until(t, "the section's row", func() bool {
		r := n.rows["kind-dev"]
		return r != nil && r.title.text == "Dev cluster" && r.badge.ink == identityColorName("violet")
	})

	// Upload Icon… in the icon's menu: the image takes the initials' place.
	icon := pngFile(t)
	sh.settings.pickIcon = func(done func(string)) { done(icon) }
	sh.settings.iconMenu(fyne.NewPos(0, 0))
	labels := []string{}
	for _, it := range sh.settings.iconMenuModel.Items {
		labels = append(labels, it.Label)
	}
	if !slices.Equal(labels, []string{"Upload Icon…", "Pick Icon Color", "Reset Icon"}) {
		t.Fatalf("icon menu %q", labels)
	}
	sh.settings.iconMenuModel.Items[0].Action()
	q.until(t, "the tile's image", func() bool {
		r := tileRenderer(tileFor(sh, "kind-dev"))
		r.Refresh()
		return r.img.Visible() && r.img.Resource != nil && !r.initials.Visible()
	})
	if entries, _ := os.ReadDir(filepath.Join(cfg, engine.ClusterIconsDir)); len(entries) != 1 {
		t.Errorf("cluster-icons holds %d files", len(entries))
	}

	// Terminal: the working directory the cluster's next terminal starts in.
	test.Tap(sh.settings.navItems["terminal"])
	q.until(t, "the Terminal section", func() bool { return sh.settings.controls["terminalDir"] != nil })
	work := t.TempDir()
	// Typed with the field focused, as a user types: the page leaves a focused field alone when the
	// engine's state changes, and an unfocused one could take a part of the path saved meanwhile
	// back (red on CI under -race, 2026-10-13).
	dirEntry := sh.settings.controls["terminalDir"].(*fyne.Container).Objects[0].(*widget.Entry)
	sh.win.Canvas().Focus(dirEntry)
	test.Type(dirEntry, work)
	q.until(t, "the working directory kept", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.TerminalDir == work
	})
	opts, err := s.LocalTerminalOptions("kind-dev", terminal.LocalOptions{Context: "kind-dev"})
	if err != nil || opts.Dir != work {
		t.Errorf("the next terminal starts in %q (%v), want %q", opts.Dir, err, work)
	}

	// Prometheus (its own section since D-87): its setting needs the cluster connected.
	test.Tap(sh.settings.navItems["prometheus"])
	q.until(t, "the Prometheus section", func() bool { return sh.settings.prom != nil })
	q.until(t, "the fields disabled", func() bool { return sh.settings.prom.address.Disabled() && sh.settings.prom.format.Disabled() })
}

// ⌘⇧, opens the connected cluster's Settings from the window's menu; the hotbar tile's and the
// navigator root's menus have Settings… too.
func TestClusterSettingsShortcutAndMenus(t *testing.T) {
	f := connectedFake(t, 20)
	reg := realCommands(t)
	for _, id := range []string{"cluster.settings.open", "preferences.open"} {
		c, _ := reg.Get(id)
		probe[clusterSettingsOpenParams](t, f, c)
	}
	withHotbar(f, "demo")
	var mu sync.Mutex // the handler runs on dispatch goroutines
	opened := []clusterSettingsOpenParams{}
	openedSoFar := func() []clusterSettingsOpenParams {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(opened)
	}
	f.handlers["cluster.settings.open"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p := decodeParams[clusterSettingsOpenParams](t, inv)
		mu.Lock()
		opened = append(opened, p)
		mu.Unlock()
		if p.Context == "" {
			p.Context = f.UIState().Context
		}
		f.update(func(u *engine.UIState) {
			u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: p.Context, Section: "general"}
		})
		return nil, nil
	}
	f.handlers["cluster.settings.get"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.ClusterSettingsView{Context: "demo", Name: "demo", ContextName: "demo", Connected: true}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the hotbar", func() bool { return sh.hot.root.Visible() && len(sh.kinds) > 0 })

	item := menuItemWith(sh.mainMenu(), cmdShiftComma)
	if item == nil || item.Label != "Cluster Settings…" || item.Disabled {
		t.Fatalf("⌘⇧, on %+v", item)
	}
	item.Action()
	q.until(t, "the page", sh.settings.visible)
	if o := openedSoFar(); len(o) != 1 || o[0].Context != "" || sh.settings.title.text != "Settings - demo" {
		t.Errorf("opened %+v, title %q", o, sh.settings.title.text)
	}

	tilesOf(sh.hot)[0].TappedSecondary(&fyne.PointEvent{})
	if l := sh.hot.menuModel.Items[1].Label; l != "Settings…" {
		t.Errorf("tile menu's second item %q", l)
	}
	sh.hot.menuModel.Items[1].Action()
	sh.side.rootRow.TappedSecondary(&fyne.PointEvent{})
	if m := sh.side.rootRow.menuModel; m == nil || m.Items[0].Label != "Settings…" {
		t.Fatalf("root menu %+v", m)
	}
	sh.side.rootRow.menuModel.Items[0].Action()
	q.until(t, "three opens", func() bool { return len(openedSoFar()) == 3 })
	if o := openedSoFar(); o[1].Context != "demo" || o[2].Context != "demo" {
		t.Errorf("opened %+v", o)
	}
}

// The Preferences page and a cluster's Settings page, dark and light: the PNGs of the pull request
// (AGENTS.md rule 7).
func TestRenderPreferencesDark(t *testing.T) { renderPreferences(t, ThemeDark, "preferences_dark.png") }
func TestRenderPreferencesLight(t *testing.T) {
	renderPreferences(t, ThemeLight, "preferences_light.png")
}
func TestRenderClusterSettingsDark(t *testing.T) {
	renderClusterSettings(t, ThemeDark, "cluster_settings_dark.png")
}
func TestRenderClusterSettingsLight(t *testing.T) {
	renderClusterSettings(t, ThemeLight, "cluster_settings_light.png")
}

func renderPreferences(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "timezone": "Europe/Berlin"})
	sh, w, q := newEngineShell(t, s, Options{}) // the theme from the preference, not --theme
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the page", func() bool { return sh.settings.visible() && sh.settings.controls["theme"] != nil })
	savePNG(t, w, name)
}

func renderClusterSettings(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	for _, ctx := range []string{"kind-dev", "prod-eu-central", "staging"} {
		mustDispatch(t, s, "hotbar.addCluster", map[string]any{"context": ctx})
	}
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet"})
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "staging", "iconColor": "orange"})
	mustDispatch(t, s, "cluster.settings.uploadIcon", map[string]any{"context": "prod-eu-central", "path": pngFile(t)})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the page", func() bool {
		return sh.settings.visible() && sh.settings.view != nil && len(tilesOf(sh.hot)) == 3 &&
			sh.icons != nil && sh.icons[mustIcon(t, s, "prod-eu-central")] != nil
	})
	q.drain()
	savePNG(t, w, name)
}

func mustIcon(t *testing.T, s *engine.Session, ctx string) string {
	t.Helper()
	cs, err := s.ClusterSettingsOf(ctx)
	if err != nil || cs.Icon == "" {
		t.Fatalf("%s has no icon: %v", ctx, err)
	}
	return cs.Icon
}
