package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// drainEngine is the fake engine with the node commands the drain banner dispatches: it answers
// node.pendingDrains with canned drains and records the actions.
type drainEngine struct {
	*fakeEngine
	mu      sync.Mutex
	pending []engine.PendingDrainInfo
	err     error
	actions []engine.Invocation
}

func (d *drainEngine) Dispatch(ctx context.Context, inv engine.Invocation) (any, error) {
	switch inv.ID {
	case "node.pendingDrains":
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.err != nil {
			return nil, d.err
		}
		return append([]engine.PendingDrainInfo(nil), d.pending...), nil
	case "node.drain", "node.uncordon", "node.dismissDrain":
		d.mu.Lock()
		d.actions = append(d.actions, inv)
		d.mu.Unlock()
		return map[string]any{"action": "ok"}, nil
	}
	return d.fakeEngine.Dispatch(ctx, inv)
}

func (d *drainEngine) set(list []engine.PendingDrainInfo, err error) {
	d.mu.Lock()
	d.pending, d.err = list, err
	d.mu.Unlock()
	d.emit(engine.Event{Type: engine.EventDrains})
}

func (d *drainEngine) recorded() []engine.Invocation {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]engine.Invocation(nil), d.actions...)
}

// newDrainTestShell is newTestShell on the drain engine; prep, if given, changes the engine's
// state before the shell starts.
func newDrainTestShell(t *testing.T, mode ThemeMode, prep ...func(*drainEngine)) (*shell, fyne.Window, *mainQueue, *drainEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	f.ui.Context = "kind-cogpit"
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.diag.Context, f.diag.Demo, f.diag.DemoStats = "kind-cogpit", false, nil
	d := &drainEngine{fakeEngine: f}
	for _, fn := range prep {
		fn(d)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(d, a, w, Options{Theme: mode}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.confirmer.armDelay = 0
	d.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	return sh, w, q, d
}

// fixtureDrains: one drain a crash interrupted, one that failed on a PodDisruptionBudget, and
// one whose job an agent cancelled.
func fixtureDrains() []engine.PendingDrainInfo {
	yes := true
	settings := engine.DrainSettings{IgnoreDaemonSets: &yes, TimeoutSeconds: 300}
	started := time.Date(2026, 10, 11, 13, 2, 0, 0, time.UTC)
	return []engine.PendingDrainInfo{
		{
			PendingDrain: engine.PendingDrain{Context: "kind-cogpit", Node: "worker-2", Started: started, StartedBy: engine.OriginGUI,
				State: engine.DrainRunning, Settings: settings},
			Status: engine.DrainStatusInterrupted, NodeState: engine.DrainNodeCordoned,
		},
		{
			PendingDrain: engine.PendingDrain{Context: "kind-cogpit", Node: "worker-3", Started: started.Add(9 * time.Minute),
				StartedBy: engine.OriginExternalAgent, State: engine.DrainFailed, Settings: settings,
				Error: `drain node worker-3: timed out after 5m0s: error when evicting pods/"db-0" -n "shop": global timeout reached`},
			Status: engine.DrainStatusFailed, NodeState: engine.DrainNodeSchedulable,
		},
		{
			PendingDrain: engine.PendingDrain{Context: "kind-cogpit", Node: "worker-4", Started: started.Add(14 * time.Minute),
				StartedBy: engine.OriginGUI, State: engine.DrainStopped, Settings: settings, Error: "cancelled by an agent"},
			Status: engine.DrainStatusStopped, NodeState: engine.DrainNodeCordoned,
		},
	}
}

func bannerButtons(b *drainBanner) map[string][]*widget.Button {
	out := map[string][]*widget.Button{}
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *widget.Button:
			out[x.Text] = append(out[x.Text], x)
		case *textButton:
			out[x.Text] = append(out[x.Text], &x.Button)
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		}
	}
	walk(b.root)
	return out
}

func TestDrainBannerShowsAndActs(t *testing.T) {
	sh, _, q, d := newDrainTestShell(t, ThemeDark)
	if sh.drains.root.Visible() {
		t.Fatal("banner visible without drains")
	}
	d.set(fixtureDrains(), nil)
	q.until(t, "the banner", func() bool { return sh.drains.root.Visible() && len(sh.drains.drains) == 3 })

	buttons := bannerButtons(sh.drains)
	if len(buttons["Finish drain"]) != 3 || len(buttons["Dismiss"]) != 3 || len(buttons["Uncordon"]) != 2 {
		t.Fatalf("buttons %v (Uncordon only for the cordoned nodes)", buttons)
	}
	// Each tap dispatches on its own goroutine (shell.dispatch): wait for one before the next, so
	// that the recorded order is the order of the taps.
	test.Tap(buttons["Finish drain"][0])
	q.until(t, "the first action", func() bool { return len(d.recorded()) == 1 })
	test.Tap(buttons["Uncordon"][0])
	q.until(t, "the second action", func() bool { return len(d.recorded()) == 2 })
	test.Tap(buttons["Dismiss"][1])
	q.until(t, "three actions", func() bool { return len(d.recorded()) == 3 })
	got := d.recorded()
	want := []struct{ id, params string }{
		{"node.drain", `{"ref":{"context":"kind-cogpit","kind":"nodes","name":"worker-2"},"ignoreDaemonSets":true,"timeoutSeconds":300}`},
		{"node.uncordon", `{"ref":{"context":"kind-cogpit","kind":"nodes","name":"worker-2"}}`},
		{"node.dismissDrain", `{"ref":{"context":"kind-cogpit","kind":"nodes","name":"worker-3"}}`},
	}
	for i, w := range want {
		if got[i].ID != w.id || string(got[i].Params) != w.params || got[i].Origin != engine.OriginGUI {
			t.Errorf("action %d: %s %s, want %s %s", i, got[i].ID, got[i].Params, w.id, w.params)
		}
	}

	// A drain running in this window has no row: its job tab in the dock shows it (D-63). One
	// another Cogpit window runs shows as running and offers nothing to click.
	running := fixtureDrains()[:2]
	running[0].Status, running[0].Job = engine.DrainStatusRunning, "job-3"
	// The progress type lives in package cluster, which the UI does not import: set it as JSON.
	if err := json.Unmarshal([]byte(`{"progress":{"step":"blocked","node":"worker-2","pod":"shop/db-0",`+
		`"message":"the PodDisruptionBudget db allows no disruption now","done":3,"total":5}}`), &running[0]); err != nil {
		t.Fatal(err)
	}
	running[1].Status, running[1].Elsewhere = engine.DrainStatusRunning, true
	running[1].Owner = &engine.JournalOwner{ID: "0123456789abcdef0123456789abcdef", PID: 4242}
	d.set(running, nil)
	q.until(t, "the drain running elsewhere alone", func() bool {
		return len(sh.drains.drains) == 1 && sh.drains.drains[0].Node == "worker-3" && sh.drains.root.Visible()
	})
	if b := bannerButtons(sh.drains); len(b) != 0 {
		t.Errorf("buttons on a running drain: %v", b)
	}
	d.set(running[:1], nil)
	q.until(t, "no banner for this window's running drain", func() bool { return !sh.drains.root.Visible() && len(sh.drains.drains) == 0 })

	// Disconnected or no journal: the banner goes.
	d.set(nil, errors.New("no cluster connected"))
	q.until(t, "the banner to go", func() bool { return !sh.drains.root.Visible() })
}

func TestDrainBannerTexts(t *testing.T) {
	ds := fixtureDrains()
	if got := drainTitle(ds[0]); got != "The drain of node worker-2 did not finish" {
		t.Errorf("title %q", got)
	}
	if got := drainDetail(ds[0], germanTime); got != "Context kind-cogpit, started 11.10.2026 13:02:00 by you · the node is cordoned and partly drained · Cogpit ended before the drain did" {
		t.Errorf("detail %q", got)
	}
	got := drainDetail(ds[1], germanTime)
	if !strings.Contains(got, "by an external agent · the node is schedulable again · timed out after 5m0s") || strings.Contains(got, "drain node worker-3:") {
		t.Errorf("failed detail %q: the node's state before the cause, the title's node not repeated", got)
	}
	// A drain another Cogpit process runs (H-22): running, without progress, naming the process.
	other := ds[0]
	other.Status, other.Elsewhere, other.Owner = engine.DrainStatusRunning, true, &engine.JournalOwner{ID: "0123456789abcdef0123456789abcdef", PID: 4242}
	if got := drainTitle(other); got != "Draining node worker-2" {
		t.Errorf("title %q", got)
	}
	if got := drainDetail(other, germanTime); got != "Context kind-cogpit, started 11.10.2026 13:02:00 by you · in another Cogpit window (process 4242)" {
		t.Errorf("detail %q", got)
	}
	// A drain whose job was stopped (D-63): the cause last.
	if got := drainTitle(ds[2]); got != "The drain of node worker-4 was stopped" {
		t.Errorf("title %q", got)
	}
	if got := drainDetail(ds[2], germanTime); got != "Context kind-cogpit, started 11.10.2026 13:16:00 by you · the node is cordoned and partly drained · cancelled by an agent" {
		t.Errorf("stopped detail %q", got)
	}
}

func renderDrainBanner(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, d := newDrainTestShell(t, mode)
	d.set(fixtureDrains(), nil)
	q.until(t, "the banner", func() bool { return sh.drains.root.Visible() && len(sh.drains.drains) == 3 })
	savePNG(t, w, name)
}

func TestRenderDrainBannerDark(t *testing.T)  { renderDrainBanner(t, ThemeDark, "drains_dark.png") }
func TestRenderDrainBannerLight(t *testing.T) { renderDrainBanner(t, ThemeLight, "drains_light.png") }
