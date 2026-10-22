package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The dock is open exactly while it can show something: a new tab opens it, a collapse keeps it
// collapsed, another tab made active opens it again, the active tab moving because its tab closed
// does not, and the last tab closing closes it. ui.toggleDock's rule refuses without tabs.
func TestSyncDock(t *testing.T) {
	s, _ := newTestSession(t, nil, time.Hour)
	if _, err := s.SetLayout(LayoutDock, nil); !errors.Is(err, ErrNoDockTabs) {
		t.Fatalf("toggling an empty dock: %v, want ErrNoDockTabs", err)
	}
	add := func(id string) UIState {
		return s.UpdateUI(func(u *UIState) {
			u.Dock = append(u.Dock, DockTab{ID: id, Type: DockTerminal, Title: id})
			u.ActiveDock = id
		})
	}
	if u := add("terminal-1"); !u.DockOpen {
		t.Fatal("a new tab did not open the dock")
	}
	open := false
	if u, err := s.SetLayout(LayoutDock, &open); err != nil || u.DockOpen {
		t.Fatalf("collapsing: %v, open %v", err, u.DockOpen)
	}
	if u := s.UpdateUI(func(u *UIState) { u.Filter = "x" }); u.DockOpen {
		t.Fatal("another change opened the collapsed dock")
	}
	if u := add("terminal-2"); !u.DockOpen {
		t.Fatal("a second tab did not open the collapsed dock")
	}
	if u, _ := s.SetLayout(LayoutDock, nil); u.DockOpen {
		t.Fatal("ui.toggleDock without open did not collapse the open dock")
	}
	if u := s.UpdateUI(func(u *UIState) { u.ActiveDock = "terminal-1" }); !u.DockOpen {
		t.Fatal("activating another tab did not open the dock")
	}
	_, _ = s.SetLayout(LayoutDock, &open)
	u := s.UpdateUI(func(u *UIState) { // the active tab closes: its neighbour becomes active
		u.Dock = u.Dock[1:]
		u.ActiveDock = "terminal-2"
	})
	if u.DockOpen {
		t.Fatal("closing the active tab opened the collapsed dock")
	}
	if u = s.UpdateUI(func(u *UIState) { u.Dock, u.ActiveDock = nil, "" }); u.DockOpen {
		t.Fatal("a dock without tabs is open")
	}
	if u, err := s.SetLayout(LayoutNavigator, nil); err != nil || !u.NavigatorHidden {
		t.Fatalf("hiding the navigator: %v, hidden %v", err, u.NavigatorHidden)
	}
	shown := true
	if u, _ := s.SetLayout(LayoutNavigator, &shown); u.NavigatorHidden {
		t.Fatal("showing the navigator left it hidden")
	}
	if _, err := s.SetLayout("panel", nil); err == nil {
		t.Fatal("an unknown part was accepted")
	}
}

// The navigator's and the chat column's state survive a restart; the dock's is not kept (its tabs
// are not restored), and a file without the layout leaves both at their defaults.
func TestWorkspaceKeepsTheLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	a, _ := workspaceSession(t, dir, time.Hour)
	if _, err := a.SetLayout(LayoutNavigator, nil); err != nil {
		t.Fatal(err)
	}
	a.UpdateUI(func(u *UIState) {
		u.Chat = true
		u.Dock = []DockTab{{ID: "terminal-1", Type: DockTerminal}}
		u.ActiveDock = "terminal-1"
	})
	a.Close()
	if doc := readWorkspace(t, dir); !doc.NavigatorHidden || !doc.Chat {
		t.Fatalf("file keeps navigatorHidden %v, chat %v", doc.NavigatorHidden, doc.Chat)
	}

	b, _ := workspaceSession(t, dir, time.Hour)
	if u := b.UIState(); !u.NavigatorHidden || !u.Chat || u.DockOpen || len(u.Dock) != 0 {
		t.Fatalf("restored navigatorHidden %v, chat %v, dockOpen %v, %d dock tabs", u.NavigatorHidden, u.Chat, u.DockOpen, len(u.Dock))
	}
	shown := true
	if _, err := b.SetLayout(LayoutNavigator, &shown); err != nil {
		t.Fatal(err)
	}
	b.UpdateUI(func(u *UIState) { u.Chat = false })
	b.Close()
	doc := readWorkspace(t, dir)
	if doc.NavigatorHidden || doc.Chat {
		t.Fatalf("file keeps navigatorHidden %v, chat %v after both were reset", doc.NavigatorHidden, doc.Chat)
	}
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: -1})
	if u := func() UIState { c, _ := workspaceSession(t, dir, time.Hour); return c.UIState() }(); u.NavigatorHidden || u.Chat {
		t.Fatalf("a file without the layout restored navigatorHidden %v, chat %v", u.NavigatorHidden, u.Chat)
	}
}

// fakeNotifier is an updater that holds an update for the user, as Sparkle's gentle reminder does.
type fakeNotifier struct {
	mu        sync.Mutex
	version   string
	available bool
	fns       []func()
}

func (f *fakeNotifier) Unavailable() error      { return nil }
func (f *fakeNotifier) Start(bool)              {}
func (f *fakeNotifier) CheckForUpdates() error  { return nil }
func (f *fakeNotifier) SetAutomaticChecks(bool) {}

func (f *fakeNotifier) AvailableUpdate() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, f.available
}

func (f *fakeNotifier) OnUpdateChange(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fns = append(f.fns, fn)
}

// set changes what the updater holds and notifies; late runs a notification after the change that
// follows it, as goroutines may.
func (f *fakeNotifier) set(available bool, version string) []func() {
	f.mu.Lock()
	f.available, f.version = available, version
	fns := append([]func(){}, f.fns...)
	f.mu.Unlock()
	return fns
}

// UIState.Update follows the updater: available with its version when it holds an update, zero
// when the session ends; a notification that runs late still leaves the newest state; a version
// text that is not a version's is dropped, the update kept; an updater that cannot update is not
// listened to.
func TestUpdateNotice(t *testing.T) {
	s, _ := newTestSession(t, nil, time.Hour)
	f := &fakeNotifier{}
	s.SetUpdater(f)
	if len(f.fns) != 1 {
		t.Fatalf("%d listeners, want 1", len(f.fns))
	}
	for _, fn := range f.set(true, "0.12.0") {
		fn()
	}
	if u := s.UIState(); u.Update != (UpdateState{Available: true, Version: "0.12.0"}) {
		t.Fatalf("update %+v", u.Update)
	}
	rev := s.UIState().Revision
	for _, fn := range f.set(true, "0.12.0") {
		fn()
	}
	if s.UIState().Revision != rev {
		t.Error("an unchanged notice cost a revision")
	}

	early := f.set(true, "0.13.0")
	for _, fn := range f.set(false, "") { // the session ended before the first notification ran
		fn()
	}
	for _, fn := range early {
		fn()
	}
	if u := s.UIState(); u.Update != (UpdateState{}) {
		t.Fatalf("after a late notification: %+v, want none", u.Update)
	}

	for _, fn := range f.set(true, "1.0 <script>") {
		fn()
	}
	if u := s.UIState(); u.Update != (UpdateState{Available: true}) {
		t.Fatalf("a strange version: %+v, want available without a version", u.Update)
	}
	for v, want := range map[string]string{
		" 0.12.0 ": "0.12.0", "0.12.0-beta.1+build_7 (123)": "0.12.0-beta.1+build_7 (123)",
		"0.12.0\n": "0.12.0", "0.1\x002": "", strings.Repeat("9", 65): "", "": "",
	} {
		if got := cleanUpdateVersion(v); got != want {
			t.Errorf("cleanUpdateVersion(%q) = %q, want %q", v, got, want)
		}
	}

	s2, _ := newTestSession(t, nil, time.Hour)
	g := &fakeNotifier{}
	s2.SetUpdater(unavailableNotifier{g})
	if len(g.fns) != 0 {
		t.Error("an updater that cannot update this Cogpit was listened to")
	}
}

// unavailableNotifier is a notifier that says it cannot update this Cogpit.
type unavailableNotifier struct{ *fakeNotifier }

func (unavailableNotifier) Unavailable() error { return errors.New("this build has no update key") }

// TestShowDockTab (AW9): showing a tab opens the dock, the active tab shown again too, which
// SyncDock alone leaves collapsed; a hidden cluster's tab shown opens that cluster's dock for when it
// is shown, not the shown one.
func TestShowDockTab(t *testing.T) {
	s, _ := poolSession(t, time.Hour, "alpha", "beta")
	mustConnect(t, s, "alpha")
	addDockTab(s, "logs-1", "alpha")
	mustConnect(t, s, "beta")
	addDockTab(s, "terminal-2", "beta")
	collapse := false
	if _, err := s.SetLayout(LayoutDock, &collapse); err != nil {
		t.Fatal(err)
	}
	if u := s.UpdateUI(func(u *UIState) { u.ActiveDock = "terminal-2" }); u.DockOpen {
		t.Fatal("setting the active tab again opened the dock: the case ShowDockTab is for is gone")
	}
	if u := s.UpdateUI(func(u *UIState) { u.ShowDockTab("terminal-2") }); u.ActiveDock != "terminal-2" || !u.DockOpen {
		t.Errorf("the active tab shown again: active %q, open %v", u.ActiveDock, u.DockOpen)
	}
	_, _ = s.SetLayout(LayoutDock, &collapse)
	u := s.UpdateUI(func(u *UIState) { u.ShowDockTab("logs-1") }) // an agent's pod.logs on alpha, not shown
	if u.ActiveDock != "terminal-2" || u.DockOpen {
		t.Errorf("the shown dock: active %q, open %v; want terminal-2, collapsed", u.ActiveDock, u.DockOpen)
	}
	if m := u.dockMemo["alpha"]; m.active != "logs-1" || !m.open {
		t.Errorf("alpha's dock: %+v", m)
	}
}
