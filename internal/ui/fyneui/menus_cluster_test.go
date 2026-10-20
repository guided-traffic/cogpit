package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// windowDisconnect is the Disconnect item of the window's Cluster menu as last set on the window
// (not a menu built afresh: the window shows the one SetMainMenu was given); Cancel Connecting
// while the cluster connects (D-73).
func windowDisconnect(t *testing.T, w fyne.Window) *fyne.MenuItem {
	t.Helper()
	m := w.MainMenu()
	if m == nil || len(m.Items) == 0 {
		t.Fatal("the window has no main menu")
	}
	for _, it := range m.Items[0].Items {
		if it.Label == cancelConnectingLabel {
			return it
		}
	}
	return menuItem(t, m.Items[0], "Disconnect")
}

// TestClusterMenuDisconnectFollowsAReconnect: the window's Cluster › Disconnect follows the
// connection through a disconnect and a reconnect of the same cluster: greyed once disconnected,
// enabled again while the reconnect is under way (Disconnect ends a connect under way), and
// still enabled once connected; greyed again when the connect fails.
func TestClusterMenuDisconnectFollowsAReconnect(t *testing.T) {
	f := connectedFake(t, 20)
	f.ui.Context, f.ui.Connection = "kind-cogpit", engine.Connection{State: engine.Connected}
	if err := engine.Register(f.reg, engine.Spec[struct{}]{ID: "cluster.disconnect", Label: "Disconnect", Danger: engine.DangerUI,
		Run: func(context.Context, *engine.Session, struct{}) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	if windowDisconnect(t, w).Disabled {
		t.Fatal("Disconnect greyed while connected")
	}

	for _, step := range []struct {
		state    engine.ConnectionState
		disabled bool
	}{
		{engine.Disconnected, true},
		{engine.Connecting, false}, // the reconnect: selecting one of the cluster's tabs
		{engine.Connected, false},
		{engine.Disconnected, true},
		{engine.Connecting, false},
		{engine.ConnectError, true},
	} {
		f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: step.state} })
		q.until(t, string(step.state), func() bool { return sh.ui.Connection.State == step.state })
		q.drain()
		item := windowDisconnect(t, w)
		if got := item.Disabled; got != step.disabled {
			t.Errorf("%s: Disconnect disabled %v, want %v", step.state, got, step.disabled)
		}
		if want := map[bool]string{true: cancelConnectingLabel, false: "Disconnect"}[step.state == engine.Connecting]; item.Label != want {
			t.Errorf("%s: the item reads %q, want %q", step.state, item.Label, want)
		}
	}
}

// TestRemovedClusterLeavesTheTabBar: Remove from Cogpit takes the removed file's clusters' tabs out
// of the tab bar (the engine closes them: cluster.remove); another cluster's tab stays.
func TestRemovedClusterLeavesTheTabBar(t *testing.T) {
	s, _ := sourcesWelcomeEngine(t)
	s.UpdateUI(func(u *engine.UIState) {
		u.Tabs = []engine.ViewTab{
			{ID: "tab-1", Context: "kind-dev [.kube/config]", View: "pods"},
			{ID: "tab-2", Context: "team-a [team.yaml]", View: "pods"},
			{ID: "tab-3", Context: "team-b [team.yaml]", View: "services"},
		}
	})
	sh, _, q := newEngineShell(t, s, Options{})
	shown := func() []string { // the UI state's tabs: the bar shows none, no cluster being connected (GW13)
		var out []string
		for _, tab := range sh.ui.Tabs {
			out = append(out, tab.Context)
		}
		return out
	}
	q.until(t, "the three tabs", func() bool { return len(shown()) == 3 })
	raw, _ := json.Marshal(clusterRemoveParams{Context: "team-a [team.yaml]"})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.remove", Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the removed file's tabs gone", func() bool { return slices.Equal(shown(), []string{"kind-dev [.kube/config]"}) })
}
