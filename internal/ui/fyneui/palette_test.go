package fyneui

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Probe parameters with the real commands' required and optional fields (TestPaletteProbesMatchTheRegistry);
// probeContainer is the row menu tests'.
type (
	probeView struct {
		View   string   `json:"view"`
		States []string `json:"states,omitempty"`
	}
	probeContext struct {
		Context string `json:"context"`
	}
	probeFilter struct {
		Filter    string `json:"filter,omitempty"`
		MatchCase *bool  `json:"matchCase,omitempty"`
		Regex     *bool  `json:"regex,omitempty"`
	}
	probeCreateTab struct {
		engine.OnCluster
		YAML     string `json:"yaml,omitempty"`
		Template string `json:"template,omitempty"`
	}
)

// paletteProbes lists the real commands the palette tests register, by parameter type.
var paletteProbes = map[string]func(t testing.TB, f *fakeEngine, cmd *engine.Command){
	"ui.palette.open": probe[struct{}], "ui.palette.close": probe[struct{}], "terminal.open": probe[engine.OnCluster],
	"resource.createTab": probe[probeCreateTab], "ui.closeDrawer": probe[struct{}],
	"cluster.connect": probe[probeContext], "ui.openView": probe[probeView], "ui.setFilter": probe[probeFilter],
	"hotbar.addCluster": probe[hotbarClusterParams], "hotbar.removeCluster": probe[hotbarClusterParams],
	"hotbar.openCluster": probe[hotbarSlotParams],
	"object.copyName":    probe[refParams], "pod.attach": probe[probeContainer], "pod.shell": probe[probeContainer],
	"pod.evict": probe[refParams], "pod.logs": probe[probeContainer], "object.edit": probe[refParams], "object.delete": probe[refParams],
}

// probe registers a stand-in of a real command: its label, menu path, shortcut, kinds, order and
// danger, enabled unless the test disables it (fakeEngine.disabled); the fake's Dispatch runs it.
func probe[P any](t testing.TB, f *fakeEngine, cmd *engine.Command) {
	t.Helper()
	id := cmd.ID
	spec := engine.Spec[P]{ID: id, Label: cmd.Label, MenuPath: cmd.MenuPath, Shortcut: cmd.Shortcut, Kinds: cmd.Kinds,
		Order: cmd.Order, DrawerOnly: cmd.DrawerOnly, Danger: cmd.Danger,
		Enabled: func(*engine.Session, P) error { return f.reason(id) },
		Run:     func(context.Context, *engine.Session, P) (any, error) { return nil, nil }}
	if cmd.Danger == engine.DangerMutating || cmd.Danger == engine.DangerDestructive {
		spec.Targets = func(*engine.Session, P) ([]model.ObjectRef, error) { return nil, nil }
	}
	if err := engine.Register(f.reg, spec); err != nil {
		t.Fatal(err)
	}
}

func realCommands(t testing.TB) *engine.Registry {
	t.Helper()
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	return r
}

// paletteFake is the demo with 10,000 pods, a hotbar of three clusters and the first pod in the drawer,
// its registry the probes of the real commands the palette lists; ui.palette.open and close change
// the fake's UI state as the engine's do.
func paletteFake(t testing.TB) (*fakeEngine, model.ObjectRef) {
	f := connectedFake(t, 10000)
	reg := realCommands(t)
	f.reg = engine.NewRegistry()
	for id, register := range paletteProbes {
		c, ok := reg.Get(id)
		if !ok {
			t.Fatalf("the registry has no %s", id)
		}
		register(t, f, c)
	}
	open := func(on bool) func(context.Context, engine.Invocation) (any, error) {
		return func(context.Context, engine.Invocation) (any, error) {
			f.update(func(u *engine.UIState) { u.Palette = on })
			return nil, nil
		}
	}
	f.handlers[engine.PaletteOpenCommand], f.handlers[engine.PaletteCloseCommand] = open(true), open(false)
	f.handlers["ui.setNavigatorScope"] = f.setNavScope // Connect to Cluster: the section (D-57)
	withHotbar(f, "demo", "kind-cogpit", "prod-eu-central")
	pod := f.tables["pods"].Rows[0].Ref
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{pod}, &pod
	return f, pod
}

// paletteShell starts the palette scene and waits for the kinds, the hotbar and the drawer.
func paletteShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, model.ObjectRef) {
	f, pod := paletteFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell", func() bool {
		return len(sh.kinds) > 0 && sh.hot.root.Visible() && sh.drawer.last != nil && sh.diag.total > 0
	})
	return sh, w, q, f, pod
}

// openPalette presses ⌘⇧P: the action of the main menu item that carries the registry's shortcut.
func openPalette(t *testing.T, sh *shell, q *mainQueue) {
	t.Helper()
	item := menuItemWith(sh.mainMenu(), &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift})
	if item == nil {
		t.Fatal("no menu item has ⌘⇧P")
	}
	item.Action()
	q.until(t, "the palette's entries", func() bool { return sh.palette.visible() && len(sh.palette.shown) > 0 })
}

// menuItemWith finds the main menu's item with a shortcut.
func menuItemWith(m *fyne.MainMenu, sc fyne.Shortcut) *fyne.MenuItem {
	for _, menu := range m.Items {
		for _, item := range menu.Items {
			if item.Shortcut != nil && item.Shortcut.ShortcutName() == sc.ShortcutName() {
				return item
			}
		}
	}
	return nil
}

func typeInPalette(t *testing.T, sh *shell, q *mainQueue, text string) {
	t.Helper()
	test.Type(sh.palette.filter, text)
	q.drain()
}

func pressInPalette(sh *shell, q *mainQueue, key fyne.KeyName) {
	sh.palette.filter.TypedKey(&fyne.KeyEvent{Name: key})
	q.drain()
}

func shownLabels(p *cmdPalette) []string {
	out := make([]string, 0, len(p.shown))
	for _, e := range p.shown {
		out = append(out, e.Label)
	}
	return out
}

// ⌘⇧P opens the palette through the engine (ui.palette.open: UI state, so agents see it) with its
// filter focused; the list starts with the drawer object's actions and has Switch to, the hotbar's
// clusters, and the commands with their shortcuts; ⌘P is the Cluster menu's Connect to
// Cluster… (cluster.connect's label and shortcut, D-57).
func TestCommandPaletteOpensWithTheShortcut(t *testing.T) {
	sh, w, q, f, pod := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	if !sh.ui.Palette || len(f.callsOf(engine.PaletteOpenCommand)) != 1 {
		t.Errorf("open state %v after %d ui.palette.open", sh.ui.Palette, len(f.callsOf(engine.PaletteOpenCommand)))
	}
	if w.Canvas().Focused() != sh.palette.filter {
		t.Errorf("focus on %T, want the palette's filter", w.Canvas().Focused())
	}
	got := shownLabels(sh.palette)
	if want := []string{"Copy", "Attach Pod", "Shell", "Evict", "Logs", "Edit", "Delete"}; !slices.Equal(got[:len(want)], want) {
		t.Errorf("first entries %q, want the pod's actions %q", got[:len(want)], want)
	}
	if sh.palette.shown[0].Category != pod.Name {
		t.Errorf("an action's category %q, want the pod %q", sh.palette.shown[0].Category, pod.Name)
	}
	for _, label := range []string{"Switch to Deployments", "Switch to Nodes", "Open kind-cogpit", "Remove from Hotbar",
		"New Terminal", "Search", "Connect to Cluster"} {
		if !slices.Contains(got, label) {
			t.Errorf("no %q in %q", label, got)
		}
	}
	if slices.Contains(got, "Switch to Pods") || slices.Contains(got, "Command Palette") {
		t.Errorf("the open view or the palette itself listed: %q", got)
	}
	if item := menuItemWith(sh.mainMenu(), &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierShortcutDefault}); item == nil || item.Label != "Connect to Cluster…" {
		t.Errorf("⌘P on %+v, want Connect to Cluster…", item)
	}
}

// Typing filters, ↑↓ move, ↵ runs the highlighted entry once the palette closed: Switch to
// Deployments is ui.openView, as the sidebar's click.
func TestCommandPaletteTypeAndRun(t *testing.T) {
	sh, _, q, f, _ := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "swdep")
	if got := shownLabels(sh.palette); len(got) != 1 || got[0] != "Switch to Deployments" {
		t.Fatalf("filtered for swdep: %q", got)
	}
	sh.palette.filter.SetText("")
	typeInPalette(t, sh, q, "switch")
	if sh.palette.sel != 0 {
		t.Errorf("highlight at %d after typing, want the first", sh.palette.sel)
	}
	pressInPalette(sh, q, fyne.KeyDown)
	pressInPalette(sh, q, fyne.KeyDown)
	pressInPalette(sh, q, fyne.KeyUp)
	want := sh.palette.shown[1]
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the view", func() bool { return len(f.callsOf("ui.openView")) == 1 && !sh.palette.visible() })
	var order []string
	f.mu.Lock()
	calls := slices.Clone(f.calls)
	f.mu.Unlock()
	for _, c := range calls {
		if c.ID == "ui.openView" || c.ID == engine.PaletteCloseCommand {
			order = append(order, c.ID)
		}
	}
	if !slices.Equal(order, []string{engine.PaletteCloseCommand, "ui.openView"}) || string(f.callsOf("ui.openView")[0].Params) != string(want.Params) {
		t.Errorf("calls %v with %s, want the palette closed, then %s", order, f.callsOf("ui.openView")[0].Params, want.Params)
	}
	q.until(t, "the state", func() bool {
		return !sh.ui.Palette && sh.ui.View == strings.TrimSuffix(strings.TrimPrefix(string(want.Params), `{"view":"`), `"}`)
	})
}

// Escape closes the palette, from its filter and from the window; so does a click outside it.
func TestCommandPaletteCloses(t *testing.T) {
	sh, w, q, f, _ := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	pressInPalette(sh, q, fyne.KeyEscape)
	q.until(t, "closed by Escape", func() bool { return !sh.palette.visible() && !sh.ui.Palette })

	openPalette(t, sh, q)
	w.Canvas().Unfocus()
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
	q.until(t, "closed by the window's Escape", func() bool { return !sh.palette.visible() && !sh.ui.Palette })
	if sh.ui.Drawer == nil {
		t.Error("the window's Escape closed the drawer under the palette too")
	}

	openPalette(t, sh, q)
	test.TapCanvas(w.Canvas(), fyne.NewPos(5, w.Canvas().Size().Height-5))
	q.until(t, "closed by a click outside", func() bool { return !sh.palette.visible() && !sh.ui.Palette })
	if n := len(f.callsOf(engine.PaletteCloseCommand)); n != 3 {
		t.Errorf("%d ui.palette.close, want 3", n)
	}

	// An agent closes it too: the window follows the state.
	openPalette(t, sh, q)
	f.update(func(u *engine.UIState) { u.Palette = false })
	q.until(t, "closed by the engine", func() bool { return !sh.palette.visible() })
}

// An agent may open the palette (ui.palette.open is a UI command), but its filter does not take
// the keyboard focus then: what the user goes on typing cannot become ↵ on an entry, a command in
// the user's name (the overlay has a focus of its own, so the keys reach the window's handler,
// where Escape closes it). The user's ⌘⇧P focuses the filter; closed, the focus is back where it was.
func TestCommandPaletteOpenedByAnAgentTakesNoFocus(t *testing.T) {
	sh, w, q, f, _ := paletteShell(t, ThemeDark)
	w.Canvas().Focus(sh.view.search)
	f.update(func(u *engine.UIState) { u.Palette = true })
	q.until(t, "the palette", func() bool { return sh.palette.visible() && len(sh.palette.shown) > 0 })
	if w.Canvas().Focused() == sh.palette.filter {
		t.Error("an agent's palette took the focus")
	}
	w.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.drain()
	if !sh.palette.visible() || len(f.callsOf("pod.attach")) > 0 || len(f.callsOf("object.copyName")) > 0 {
		t.Error("↵ outside the filter ran an entry")
	}
	openPalette(t, sh, q) // ⌘⇧P on the open palette: the user's, so it takes the focus
	if w.Canvas().Focused() != sh.palette.filter {
		t.Errorf("⌘⇧P left the focus on %T", w.Canvas().Focused())
	}
	pressInPalette(sh, q, fyne.KeyEscape)
	q.until(t, "closed", func() bool { return !sh.palette.visible() })
	if w.Canvas().Focused() != sh.view.search {
		t.Errorf("after closing, the focus is on %T, want the search it was on", w.Canvas().Focused())
	}
}

// A destructive action from the palette asks as from the row menu, naming the object; a greyed one
// does not run and keeps the palette open.
func TestCommandPaletteRunsLikeTheMenus(t *testing.T) {
	sh, _, q, f, pod := paletteShell(t, ThemeDark)
	f.disabled["pod.evict"] = "the pod has no controller"
	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "evict")
	if len(sh.palette.shown) != 1 || sh.palette.shown[0].Reason != "the pod has no controller" {
		t.Fatalf("Evict: %+v", sh.palette.shown)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	if !sh.palette.visible() || len(f.callsOf("pod.evict")) != 0 {
		t.Fatal("a greyed entry ran or closed the palette")
	}

	sh.palette.filter.SetText("")
	typeInPalette(t, sh, q, "delete")
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	if sh.palette.visible() {
		t.Error("the palette stays over the confirmation")
	}
	call := f.callsOf("object.delete")
	if len(call) != 1 || call[0].Origin != engine.OriginGUI || !decodeParams[refParams](t, call[0]).Ref.Same(pod) {
		t.Fatalf("object.delete %+v", call)
	}
	test.Tap(sh.confirmer.current.cancel)
	q.until(t, "the dialog gone", func() bool { return sh.confirmer.current == nil })
}

// A command whose parameters the window asks for runs its dialog: Connect to Cluster shows the
// navigator's section with its search focused (⌘P's, D-57), Search focuses the search field.
func TestCommandPaletteOpensTheWindowsDialogs(t *testing.T) {
	sh, w, q, f, _ := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "connect to")
	pressInPalette(sh, q, fyne.KeyReturn)
	h := sh.side.head
	q.until(t, "the section's search", func() bool {
		return !sh.palette.visible() && sh.side.sec.Visible() && h.searching() && w.Canvas().Focused() == h.entry
	})
	if len(f.callsOf("cluster.connect")) != 0 {
		t.Error("cluster.connect ran without a context")
	}
	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape}) // the focused search takes Escape: back to the cluster
	q.until(t, "the cluster's tree again", func() bool { return sh.side.tree.Visible() && !h.searching() })

	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "search")
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the search focused", func() bool { return w.Canvas().Focused() == sh.view.search })
}

// The probes stand for the real commands: the same required parameters.
func TestPaletteProbesMatchTheRegistry(t *testing.T) {
	f, _ := paletteFake(t)
	reg := realCommands(t)
	for id := range paletteProbes {
		p, _ := f.reg.Get(id)
		r, _ := reg.Get(id)
		pr, rr := slices.Sorted(slices.Values(p.Schema.Required)), slices.Sorted(slices.Values(r.Schema.Required))
		if !slices.Equal(pr, rr) || len(p.Schema.Properties) != len(r.Schema.Properties) {
			t.Errorf("%s: probe requires %v of %d, the command %v of %d", id, pr, len(p.Schema.Properties), rr, len(r.Schema.Properties))
		}
	}
}

func TestParseShortcut(t *testing.T) {
	sd := fyne.KeyModifierShortcutDefault
	for s, want := range map[string]*desktop.CustomShortcut{
		"Cmd+Shift+P":   {KeyName: fyne.KeyP, Modifier: sd | fyne.KeyModifierShift},
		"Cmd+P":         {KeyName: fyne.KeyP, Modifier: sd},
		"Cmd+,":         {KeyName: fyne.KeyComma, Modifier: sd},
		"Alt+Cmd+F2":    {KeyName: fyne.KeyF2, Modifier: sd | fyne.KeyModifierAlt},
		"Cmd+W":         {KeyName: fyne.KeyW, Modifier: sd},
		"Cmd+Alt+Right": {KeyName: fyne.KeyRight, Modifier: sd | fyne.KeyModifierAlt},
		"Ctrl+Shift+-":  {KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift},
		"Cmd+1–9":       nil, // the tab bar's, bound per digit (tabbar.go)
		"F1–F12":        nil, // the hotbar's, a typed key
		"Shift+F1–F12":  nil,
		"Shift+F2":      nil, // Fyne makes no shortcut of a shifted key
		"P":             nil,
		"Hyper+P":       nil,
		"Cmd+Space":     nil,
		"":              nil,
	} {
		got := parseShortcut(s)
		if (got == nil) != (want == nil) || (got != nil && got.ShortcutName() != want.ShortcutName()) {
			t.Errorf("%q: %+v, want %+v", s, got, want)
		}
	}
	for s, want := range map[string][2]string{
		"Cmd+Shift+P": {"⌘⇧P", "Ctrl+Shift+P"}, "Cmd+P": {"⌘P", "Ctrl+P"}, "Shift+F2": {"⇧F2", "Shift+F2"},
		"F1–F12": {"F1–F12", "F1–F12"}, "F3": {"F3", "F3"}, "": {"", ""},
		"Cmd+Alt+Left": {"⌘⌥←", "Ctrl+Alt+Left"}, "Ctrl+-": {"⌃-", "Ctrl+-"}, "Cmd+1–9": {"⌘1–9", "Ctrl+1–9"},
	} {
		if mac, other := shortcutText(s, true), shortcutText(s, false); mac != want[0] || other != want[1] {
			t.Errorf("%q: %q and %q, want %q", s, mac, other, want)
		}
	}
}

// Every shortcut the registry declares is bound once and takes nothing the platform, Fyne's text
// fields or the YAML editor already have: macOS's app and Window menus (⌘Q, ⌘H, ⌥⌘H, ⌘M, ⌃⌘F, from
// GLFW), the fields' clipboard and undo keys, the editor's ⌘ and ⌥ arrows; the F-keys stay the
// hotbar's typed keys.
func TestRegistryShortcutsDoNotCollide(t *testing.T) {
	sd := fyne.KeyModifierShortcutDefault
	reserved := map[string]string{}
	platform := []*desktop.CustomShortcut{
		{KeyName: fyne.KeyQ, Modifier: sd}, {KeyName: fyne.KeyH, Modifier: sd}, {KeyName: fyne.KeyH, Modifier: sd | fyne.KeyModifierAlt},
		{KeyName: fyne.KeyM, Modifier: sd},
		{KeyName: fyne.KeyA, Modifier: sd}, {KeyName: fyne.KeyC, Modifier: sd}, {KeyName: fyne.KeyV, Modifier: sd},
		{KeyName: fyne.KeyX, Modifier: sd}, {KeyName: fyne.KeyZ, Modifier: sd}, {KeyName: fyne.KeyY, Modifier: sd},
		{KeyName: fyne.KeyZ, Modifier: sd | fyne.KeyModifierShift},
	}
	if sd != fyne.KeyModifierControl {
		// macOS's full screen, ⌃⌘F; where the default modifier is Control (Linux, Windows) the same
		// expression would read Ctrl+F, the filter's own shortcut there (CI 2026-10-13).
		platform = append(platform, &desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: sd | fyne.KeyModifierControl})
	}
	for _, sc := range platform {
		reserved[sc.ShortcutName()] = "the platform or a text field"
	}
	editing := []fyne.KeyModifier{sd, fyne.KeyModifierControl}
	if runtime.GOOS == "darwin" {
		// ⌥←, ⌥→: a word on macOS. On Windows and Linux a word is Ctrl+← (above); Alt+← and Alt+→
		// are VS Code's Go Back and Go Forward on Windows (D-134), which take them from the editor too.
		editing = append(editing, fyne.KeyModifierAlt)
	}
	for _, k := range []fyne.KeyName{fyne.KeyLeft, fyne.KeyRight, fyne.KeyUp, fyne.KeyDown, fyne.KeyHome, fyne.KeyEnd, fyne.KeyBackspace, fyne.KeyDelete} {
		for _, m := range editing {
			reserved[(&desktop.CustomShortcut{KeyName: k, Modifier: m}).ShortcutName()] = "the YAML editor"
			reserved[(&desktop.CustomShortcut{KeyName: k, Modifier: m | fyne.KeyModifierShift}).ShortcutName()] = "the YAML editor"
		}
	}
	bound := map[string]string{}
	for _, c := range realCommands(t).Commands() {
		for _, key := range append([]string{c.Shortcut}, c.MoreKeys...) { // the further keys too (D-134)
			if key == "" {
				continue
			}
			sc := parseShortcut(key)
			if sc == nil {
				if !strings.Contains(key, "–") {
					t.Errorf("%s: shortcut %q cannot be bound", c.ID, key)
				}
				// A range its owner binds key by key: F1–F12 and ⇧F1–⇧F12, the hotbar's typed keys
				// (hotbar.go); ⌘1–9, the tab bar's main-menu items (tabbar.go), checked below.
				continue
			}
			name := sc.ShortcutName()
			if who, ok := reserved[name]; ok {
				t.Errorf("%s: %s is %s's", c.ID, key, who)
			}
			if other, ok := bound[name]; ok {
				t.Errorf("%s and %s share %s", c.ID, other, key)
			}
			bound[name] = c.ID
		}
	}
	for n := '1'; n <= '9'; n++ { // ⌘1–⌘9: the tab bar's
		name := (&desktop.CustomShortcut{KeyName: fyne.KeyName(string(n)), Modifier: sd}).ShortcutName()
		if other, ok := bound[name]; ok {
			t.Errorf("ui.tab.select and %s share ⌘%c", other, n)
		}
	}
	for _, id := range []string{"ui.palette.open", "cluster.connect", "ui.setFilter", "terminal.open", "ui.tab.close",
		"ui.tab.next", "ui.tab.previous", "ui.tab.back", "ui.tab.forward"} {
		if !slices.Contains(mapValues(bound), id) {
			t.Errorf("%s has no shortcut", id)
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestRenderCommandPaletteDark(t *testing.T) {
	renderCommandPalette(t, ThemeDark, "command_palette_dark.png")
}

func TestRenderCommandPaletteLight(t *testing.T) {
	renderCommandPalette(t, ThemeLight, "command_palette_light.png")
}

// renderCommandPalette: the palette open over the Pods view with a pod in the drawer, "de" typed:
// the pod's Delete (greyed: the access reviews deny it), Close Details, the hotbar's demo tile with
// its key, Switch to Deployments and Nodes, the second row highlighted.
func renderCommandPalette(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, f, _ := paletteShell(t, mode)
	f.disabled["object.delete"] = "not permitted: you may not delete pods in ingress-nginx"
	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "de")
	pressInPalette(sh, q, fyne.KeyDown)
	savePNG(t, w, name)
}
