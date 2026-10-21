package fyneui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// uiNotifier is an updater that holds an update for the user, as Sparkle's gentle reminder does
// (internal/update): set changes what it holds and tells the engine on goroutines of their own.
type uiNotifier struct {
	uiUpdater
	mu        sync.Mutex
	version   string
	available bool
	fns       []func()
}

func (n *uiNotifier) AvailableUpdate() (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.version, n.available
}

func (n *uiNotifier) OnUpdateChange(fn func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fns = append(n.fns, fn)
}

func (n *uiNotifier) set(available bool, version string) {
	n.mu.Lock()
	n.available, n.version = available, version
	fns := append([]func(){}, n.fns...)
	n.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

// chatEngine is settingsEngine with the fake chat agent.
func chatEngine(t *testing.T) *engine.Session {
	t.Helper()
	s, _ := settingsEngine(t)
	ch := newFakeChat()
	ch.conns = []engine.ChatConnection{lmStudio()}
	ch.notify = s.NotifyChat
	s.SetChatAgent(ch)
	return s
}

// The three toggles at the top bar's right end (the owner, 2026-10-16 night: after the bell and a
// divider), in VS Code's order, drive the
// engine's state and show it: the navigator hides (the views take its room, the hotbar stays) and
// comes back, the dock collapses to its strip and opens again (its own arrow and a click on its
// shown tab too), the chat column shows; the dock's toggle is disabled while the dock has no tabs
// and no terminal can open (no cluster connected). An agent's change shows the same way. Against the real engine and its registry.
func TestLayoutTogglesThroughTheEngine(t *testing.T) {
	s := chatEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	l := sh.top.layout
	if got := l.toggles.Objects; len(got) != 3 || got[0] != l.navigator || got[1] != l.dock || got[2] != l.chat {
		t.Fatalf("the layout bar holds %v, want Navigator, Dock, Chat", got)
	}
	q.until(t, "the toggles", func() bool {
		return l.navigator.Visible() && l.dock.Visible() && l.chat.Visible() && !l.update.Visible() &&
			l.navigator.Icon == iconNavigatorShown && l.dock.Icon == iconDockClosed && l.dock.Disabled() && l.chat.Icon == iconChatHidden
	})
	if !strings.HasPrefix(l.navigator.tip, "Hide Navigator") || !strings.HasPrefix(l.dock.tip, "Dock: no tabs") ||
		!strings.HasPrefix(l.chat.tip, "Show Agent Chat") {
		t.Errorf("tooltips %q, %q, %q", l.navigator.tip, l.dock.tip, l.chat.tip)
	}
	if !sh.hot.root.Visible() {
		t.Fatal("the hotbar is hidden")
	}

	panel, handle, rest := sh.sideLay.parts(sh.mainRow.Objects)
	test.Tap(l.navigator)
	q.until(t, "the navigator hidden", func() bool {
		return s.UIState().NavigatorHidden && !panel.Visible() && !handle.Visible() && l.navigator.Icon == iconNavigatorHidden
	})
	if rest.Size().Width != sh.mainRow.Size().Width || rest.Position().X != 0 {
		t.Errorf("the views take %v at %v of %v", rest.Size().Width, rest.Position().X, sh.mainRow.Size().Width)
	}
	if !sh.hot.root.Visible() || !strings.HasPrefix(l.navigator.tip, "Show Navigator") {
		t.Errorf("hotbar visible %v, tooltip %q", sh.hot.root.Visible(), l.navigator.tip)
	}
	test.Tap(l.navigator)
	q.until(t, "the navigator back", func() bool {
		return !s.UIState().NavigatorHidden && panel.Visible() && handle.Visible() && l.navigator.Icon == iconNavigatorShown
	})
	if rest.Position().X <= 0 {
		t.Error("the views did not give the navigator its room back")
	}

	mustDispatch(t, s, "diagnostics.open", struct{}{})
	q.until(t, "the dock open", func() bool {
		return sh.dock.open() && !l.dock.Disabled() && l.dock.Icon == iconDockOpen && sh.body.Objects[0] == sh.vsplit
	})
	test.Tap(l.dock)
	q.until(t, "the dock collapsed", func() bool {
		return !s.UIState().DockOpen && !sh.dock.open() && l.dock.Icon == iconDockClosed && sh.body.Objects[0] != sh.vsplit
	})
	if !strings.HasPrefix(l.dock.tip, "Open Dock") {
		t.Errorf("the collapsed dock's tooltip %q", l.dock.tip)
	}
	test.Tap(sh.dock.toggle) // the dock's own arrow goes through the engine too
	q.until(t, "the dock opened by its arrow", func() bool { return s.UIState().DockOpen && l.dock.Icon == iconDockOpen })
	test.Tap(sh.dock.toggle)
	q.until(t, "the dock collapsed by its arrow", func() bool { return !s.UIState().DockOpen && !sh.dock.open() })
	tab, ok := sh.dock.tabRow.Objects[0].(*tabButton)
	if !ok {
		t.Fatalf("the strip's first object %T", sh.dock.tabRow.Objects[0])
	}
	test.Tap(tab) // the shown tab of a collapsed dock opens it
	q.until(t, "the dock opened by its tab", func() bool { return s.UIState().DockOpen && sh.dock.open() })

	test.Tap(l.chat)
	q.until(t, "the chat column", func() bool {
		return s.UIState().Chat && sh.chatRow.Objects[0] == sh.chatSplit && l.chat.Icon == iconChatShown
	})

	// An agent hides the navigator and collapses the dock: the window and the toggles follow.
	for _, id := range []string{"ui.toggleNavigator", "ui.toggleDock"} {
		if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: mustJSON(t, struct{}{}),
			Origin: engine.OriginExternalAgent}); err != nil {
			t.Fatalf("an agent's %s: %v", id, err)
		}
	}
	q.until(t, "the agent's layout", func() bool {
		return !panel.Visible() && !sh.dock.open() && l.navigator.Icon == iconNavigatorHidden && l.dock.Icon == iconDockClosed
	})
}

// View › Navigator (⌘B) and View › Dock (⌘J), before Agent Chat (⌘I), checked while their part is
// open, run the toggles; Dock is disabled while the dock has no tabs. The shortcuts do not collide
// with the registry's others (TestRegistryShortcutsDoNotCollide checks the registry as a whole).
func TestLayoutMenuItems(t *testing.T) {
	s := chatEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	for _, c := range []struct {
		label string
		key   fyne.KeyName
	}{{"Navigator", fyne.KeyB}, {"Dock", fyne.KeyJ}, {"Agent Chat", fyne.KeyI}} {
		sc, ok := mainMenuItem(t, sh, c.label).Shortcut.(*desktop.CustomShortcut)
		if !ok || sc.KeyName != c.key || sc.Modifier != fyne.KeyModifierShortcutDefault {
			t.Errorf("%s's shortcut %#v, want ⌘%s", c.label, sc, c.key)
		}
	}
	var view *fyne.Menu
	for _, m := range sh.mainMenu().Items {
		if m.Label == "View" {
			view = m
		}
	}
	labels := make([]string, 0, len(view.Items))
	for _, it := range view.Items {
		labels = append(labels, it.Label)
	}
	if got := strings.Join(labels, "|"); !strings.Contains(got, "Navigator|Dock|Agent Chat|Stop Agent") {
		t.Errorf("the View menu: %s", got)
	}
	nav, dock := mainMenuItem(t, sh, "Navigator"), mainMenuItem(t, sh, "Dock")
	if !nav.Checked || dock.Checked || !dock.Disabled {
		t.Fatalf("Navigator checked %v, Dock checked %v disabled %v", nav.Checked, dock.Checked, dock.Disabled)
	}
	nav.Action()
	q.until(t, "the navigator hidden", func() bool { return s.UIState().NavigatorHidden && !mainMenuItem(t, sh, "Navigator").Checked })
	mustDispatch(t, s, "diagnostics.open", struct{}{})
	q.until(t, "the dock open", func() bool {
		d := mainMenuItem(t, sh, "Dock")
		return d.Checked && !d.Disabled
	})
	mainMenuItem(t, sh, "Dock").Action()
	q.until(t, "the dock collapsed", func() bool { return !s.UIState().DockOpen && !mainMenuItem(t, sh, "Dock").Checked })
}

// The Update button shows only while the updater holds a newer release, its version in the label
// and the tooltip, and a click runs app.installUpdate (Sparkle's window for an updater that does not
// install by itself); it goes when the update session ends. An updater that cannot update this
// Cogpit (no key) never shows it, whatever it says.
func TestUpdateButtonShowsOnlyWithAnUpdate(t *testing.T) {
	s, _ := settingsEngine(t)
	n := &uiNotifier{}
	s.SetUpdater(n)
	sh, w, q := newEngineShell(t, s, Options{})
	b := sh.top.layout.update
	if b.Visible() {
		t.Fatal("the Update button shows without an update")
	}
	n.set(true, "0.12.0")
	q.until(t, "the Update button", func() bool {
		return b.Visible() && sh.top.layout.updateBox.Visible() && strings.Contains(b.tip, "Cogpit 0.12.0 is available")
	})
	settle(w)
	if b.Text != "Update to v0.12.0" {
		t.Errorf("the button says %q", b.Text)
	}
	test.Tap(b)
	q.until(t, "Sparkle's window", func() bool { return n.checks.Load() == 1 })
	n.set(false, "")
	q.until(t, "the button gone", func() bool {
		return !b.Visible() && !sh.top.layout.updateBox.Visible() && s.UIState().Update == engine.UpdateState{}
	})

	s2, _ := settingsEngine(t)
	keyless := &uiNotifier{uiUpdater: uiUpdater{why: errors.New("this build has no update key")}}
	s2.SetUpdater(keyless)
	sh2, _, q2 := newEngineShell(t, s2, Options{})
	keyless.set(true, "0.12.0")
	time.Sleep(20 * time.Millisecond)
	q2.drain()
	if sh2.top.layout.update.Visible() || s2.UIState().Update.Available {
		t.Fatal("a build without the update key shows the Update button")
	}
}

// uiRestarter is a notifier whose release can be installed already, as the Linux package's
// updater knows (internal/update, D-108).
type uiRestarter struct {
	uiNotifier
	restart atomic.Bool
}

func (r *uiRestarter) RestartPending() bool { return r.restart.Load() }

// Once apt installed the release under the running app, the button reads Restart to Update, and
// its tooltip says the click restarts Cogpit.
func TestUpdateButtonOffersTheRestart(t *testing.T) {
	s, _ := settingsEngine(t)
	r := &uiRestarter{}
	s.SetUpdater(r)
	sh, w, q := newEngineShell(t, s, Options{})
	b := sh.top.layout.update
	r.set(true, "0.34.0")
	q.until(t, "the Update button", func() bool { return b.Visible() })
	settle(w)
	if b.Text != "Update to v0.34.0" {
		t.Errorf("before the install: %q", b.Text)
	}
	r.restart.Store(true)
	r.set(true, "0.34.0") // the same version, now installed
	// The engine's state first, then the window's: the bar follows the state's change on the main
	// queue, which until drains, a step later on a slow runner (the Windows CI, 2026-10-21).
	q.until(t, "the restart", func() bool { return s.UIState().Update.Restart && b.Text == restartLabel })
	settle(w)
	if b.Text != restartLabel || b.tip != "Cogpit 0.34.0 is installed: restart Cogpit to use it" {
		t.Errorf("after the install: %q, tooltip %q", b.Text, b.tip)
	}
}

// The Update button's label names the version Sparkle reports, a version's own v not doubled, and
// says "Update" without one.
func TestUpdateLabel(t *testing.T) {
	for in, want := range map[string]string{
		"": "Update", "0.16.0": "Update to v0.16.0", "v0.16.0": "Update to v0.16.0", "V1.2": "Update to v1.2",
		"1.0.0-beta.1": "Update to v1.0.0-beta.1", "beta": "Update to beta", "v": "Update to v",
	} {
		if got := updateLabel(in); got != want {
			t.Errorf("updateLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// The Update button's tooltip says what its click does: Sparkle's window on macOS, apt's install on
// Linux, and the restart into a release apt installed under the running app.
func TestUpdateTip(t *testing.T) {
	for _, c := range []struct {
		u    engine.UpdateState
		goos string
		want string
	}{
		{engine.UpdateState{Available: true, Version: "0.34.0"}, "darwin", "Cogpit 0.34.0 is available: show it in the update window"},
		{engine.UpdateState{Available: true}, "darwin", "A newer Cogpit is available: show it in the update window"},
		{engine.UpdateState{Available: true, Version: "0.34.0"}, "linux",
			"Cogpit 0.34.0 is available: install it through apt (asks for an administrator's password)"},
		{engine.UpdateState{Available: true, Version: "0.34.0", Restart: true}, "linux", "Cogpit 0.34.0 is installed: restart Cogpit to use it"},
		{engine.UpdateState{Available: true, Version: "0.36.0"}, "windows", "Cogpit 0.36.0 is available: install it now; Cogpit restarts into it"},
	} {
		if got := updateTip(c.u, c.goos); got != c.want {
			t.Errorf("updateTip(%+v, %s) = %q, want %q", c.u, c.goos, got, c.want)
		}
	}
}

// iconOf is a toggle's icon: its renderer's image.
func iconOf(t *testing.T, b *tipButton) *canvas.Image {
	t.Helper()
	for _, o := range test.WidgetRenderer(b).Objects() {
		if img, ok := o.(*canvas.Image); ok {
			return img
		}
	}
	t.Fatal("a toggle without its icon")
	return nil
}

// The Update button sits on the bar's centre line, as the toggles' icons do, and the line under the
// bar lies directly under it (F11): with a box's padding between the two, the line lay 4 units
// below the bar's end, and in the dark theme, where the window's background and the bar's are
// nearly one colour, everything in the bar looked 2 units high; the pill, 26 of the bar's 40 units,
// most (the owner, 2026-10-16: "hängt ein wenig zu hoch").
func TestUpdateButtonOnTheBarsCentreLine(t *testing.T) {
	s := chatEngine(t)
	n := &uiNotifier{}
	s.SetUpdater(n)
	sh, w, q := newEngineShell(t, s, Options{})
	l := sh.top.layout
	n.set(true, "0.16.0")
	q.until(t, "the Update button and the toggles", func() bool {
		return l.update.Visible() && l.navigator.Visible() && l.dock.Visible() && l.chat.Visible()
	})
	settle(w)
	_, barEnd := absRect(sh.top.root)
	edge, _ := absRect(sh.top.edge)
	near(t, "the line under the bar", edge.Y, barEnd.Y)
	pill := middleOf(l.update).Y
	near(t, "the Update button's centre against the band above the line", pill, edge.Y/2)
	for _, b := range []*tipButton{l.navigator, l.dock, l.chat} {
		near(t, "the Update button's centre against a toggle's icon", pill, middleOf(iconOf(t, b)).Y)
	}
}

// At the narrowest window the app allows (its content's least size, which Fyne makes the window's
// minimum), the Update button with its version still fits beside the bar's left part. Where the
// row lacks the room, the version gives way: the button says "Update" until the row has it, the
// version never widens the row's least width, and the two parts never overlap. The bar names no
// cluster since 2026-10-21 (D-54), so no text of it, a long context name and its kubeconfig file
// as before, makes it the window's widest row: the window's minimum lies far above the bar's own,
// and the second half checks the row's layout itself at its least width and a hair beyond the
// room the long label needs.
func TestUpdateButtonAtTheNarrowestWindow(t *testing.T) {
	s := chatEngine(t)
	n := &uiNotifier{}
	s.SetUpdater(n)
	// macOS's bar: the menus are the system's. With the menus in the bar (Windows and Linux) the bar is
	// the window's widest row, as below.
	sh, w, q := newEngineShell(t, s, Options{menus: menusNative})
	n.set(true, "0.16.0")
	q.until(t, "the Update button", func() bool { return sh.top.layout.update.Visible() })
	settle(w)
	w.Resize(w.Content().MinSize())
	settle(w)
	assertUpdateFits(t, sh, "Update to v0.16.0")

	top, l := sh.top, sh.top.layout
	row := barRowLayout{t: top}
	least := row.MinSize(nil).Width
	height := top.left.Size().Height
	apart := func(when string) {
		t.Helper()
		if end, start := top.left.Position().X+top.left.Size().Width, top.right.Position().X; end > start {
			t.Errorf("%s: the left part ends at %v, the right part starts at %v", when, end, start)
		}
	}
	row.Layout(nil, fyne.NewSize(least, height))
	if l.update.Text != "Update" {
		t.Errorf("at the row's least width the button says %q, want %q", l.update.Text, "Update")
	}
	apart("at the row's least width")
	if got := row.MinSize(nil).Width; got != least {
		t.Errorf("the version changed the row's least width from %v to %v", least, got)
	}
	extra := labelWidth("Update to v0.16.0") - labelWidth("Update")
	row.Layout(nil, fyne.NewSize(least+extra+0.25, height)) // a quarter for the floats' rounding
	if l.update.Text != "Update to v0.16.0" {
		t.Errorf("with room for the version the button says %q", l.update.Text)
	}
	apart("with room for the version")
	if got := row.MinSize(nil).Width; got != least {
		t.Errorf("the version widened the row's least width from %v to %v", least, got)
	}
}

// assertUpdateFits checks the Update button's label and that it lies right of the bar's left part.
func assertUpdateFits(t *testing.T, sh *shell, label string) {
	t.Helper()
	l := sh.top.layout
	if l.update.Text != label {
		t.Errorf("at %v the button says %q, want %q", sh.win.Canvas().Size(), l.update.Text, label)
	}
	_, leftEnd := absRect(sh.top.left)
	pill, pillEnd := absRect(l.update)
	if pill.X < leftEnd.X || pillEnd.X > sh.win.Canvas().Size().Width {
		t.Errorf("the button at %v..%v, the left part ends at %v, the window at %v", pill.X, pillEnd.X, leftEnd.X, sh.win.Canvas().Size().Width)
	}
}

// layoutScene is the demo cluster over a real session with the agent's chat and the given
// updater: the Diagnostics tab in the dock, the chat column shown; then change runs on the engine.
func layoutScene(t *testing.T, mode ThemeMode, u engine.Updater, change func(s *engine.Session)) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 3
	s := engine.New(engine.Options{Connector: conn, Hotbar: engine.NewHotbar(t.TempDir())}) // the hotbar stays
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ch := newFakeChat()
	ch.conns = []engine.ChatConnection{lmStudio()}
	ch.notify = s.NotifyChat
	s.SetChatAgent(ch)
	if u != nil {
		s.SetUpdater(u)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{Theme: mode, AutoConnect: engine.DemoContext}, q.do)
	sh.zone = time.UTC
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.SetConfirmer(nil)
	})
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the demo's pods", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.snap.Rows) > 0
	})
	mustDispatch(t, s, "hotbar.addCluster", struct{}{})
	mustDispatch(t, s, "diagnostics.open", struct{}{})
	mustDispatch(t, s, "chat.toggle", map[string]any{"open": true})
	if change != nil {
		change(s)
	}
	until("the layout", func() bool {
		st := s.UIState()
		return len(sh.ui.Dock) == 1 && sh.ui.Chat && sh.ui.NavigatorHidden == st.NavigatorHidden &&
			sh.ui.DockOpen == st.DockOpen && sh.dock.open() == st.DockOpen && sh.chat.loaded && sh.diag.informers > 0
	})
	return sh, w, q
}

func renderLayout(t *testing.T, mode ThemeMode, name string, u engine.Updater, change func(s *engine.Session)) {
	_, w, _ := layoutScene(t, mode, u, change)
	savePNG(t, w, name)
}

func hideNavigator(s *engine.Session) { _, _ = s.SetLayout(engine.LayoutNavigator, new(false)) }
func collapseDock(s *engine.Session)  { _, _ = s.SetLayout(engine.LayoutDock, new(false)) }

// The renders of F7: every part open; the navigator hidden; the dock collapsed; the Update button
// shown (Cogpit 0.12.0 held by the updater).
func TestRenderLayoutAllOpenDark(t *testing.T) {
	renderLayout(t, ThemeDark, "layout_all_open_dark.png", nil, nil)
}
func TestRenderLayoutAllOpenLight(t *testing.T) {
	renderLayout(t, ThemeLight, "layout_all_open_light.png", nil, nil)
}
func TestRenderLayoutNavigatorHiddenDark(t *testing.T) {
	renderLayout(t, ThemeDark, "layout_navigator_hidden_dark.png", nil, hideNavigator)
}
func TestRenderLayoutNavigatorHiddenLight(t *testing.T) {
	renderLayout(t, ThemeLight, "layout_navigator_hidden_light.png", nil, hideNavigator)
}
func TestRenderLayoutDockCollapsedDark(t *testing.T) {
	renderLayout(t, ThemeDark, "layout_dock_collapsed_dark.png", nil, collapseDock)
}
func TestRenderLayoutDockCollapsedLight(t *testing.T) {
	renderLayout(t, ThemeLight, "layout_dock_collapsed_light.png", nil, collapseDock)
}

// renderUpdate: the Update button left of the toggles.
func renderUpdate(t *testing.T, mode ThemeMode, name string) {
	n := &uiNotifier{}
	sh, w, q := layoutScene(t, mode, n, nil)
	n.set(true, "0.12.0")
	q.until(t, "the Update button", func() bool { return sh.top.layout.update.Visible() })
	savePNG(t, w, name)
}

func TestRenderLayoutUpdateDark(t *testing.T) { renderUpdate(t, ThemeDark, "layout_update_dark.png") }
func TestRenderLayoutUpdateLight(t *testing.T) {
	renderUpdate(t, ThemeLight, "layout_update_light.png")
}

// TestDockToggleOpensATerminal (WF18): with no dock tab, the dock's toggle is enabled on a real
// cluster, says so in its tooltip and runs ui.toggleDock, which opens a terminal there; on the demo
// cluster (no terminals) it stays disabled with the reason.
func TestDockToggleOpensATerminal(t *testing.T) {
	f := connectedFake(t, 3)
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = "kind-cogpit", engine.Connection{State: engine.Connected}
	})
	sh, _, q := newTestShell(t, f, Options{})
	l := sh.top.layout
	q.until(t, "the enabled toggle", func() bool { return !l.dock.Disabled() })
	if !strings.HasPrefix(l.dock.tip, "Open a Terminal") {
		t.Errorf("the empty dock's tooltip %q", l.dock.tip)
	}
	test.Tap(l.dock)
	q.until(t, "ui.toggleDock", func() bool { return len(f.callsOf("ui.toggleDock")) == 1 })

	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected, Demo: true} })
	q.until(t, "the disabled toggle", func() bool { return l.dock.Disabled() })
	if !strings.HasPrefix(l.dock.tip, "Dock: no tabs; the demo cluster has no terminals") {
		t.Errorf("the demo's tooltip %q", l.dock.tip)
	}
}
