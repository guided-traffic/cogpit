package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A reconnect of a cluster connected behind the shown one (D-69: several clusters connected, one
// shown; D-81: Reconnect): the engine marks that cluster's own entry of Connections
// (engine/reconnect.go, markReconnecting), and the window reads it there (reconnect.go, reconnecting).

// TestReconnectingAsksTheClustersOwnConnection: reconnecting answers for the cluster it is asked
// about. The shown cluster's answer is the window's Connection while that connection stands;
// another cluster's, and the shown one's before its connection stands, is its own entry of
// Connections, so a reconnect behind the shown cluster is that cluster's alone. No context, and a
// cluster Cogpit holds no connection to, never reconnect.
func TestReconnectingAsksTheClustersOwnConnection(t *testing.T) {
	const shown, behind, idle, unknown = cShown, cBlink, cCold, cFailed // the marks' scene (clustermarks_test.go)
	connected := engine.Connection{State: engine.Connected}
	renewing := engine.Connection{State: engine.Connected, Reconnecting: true}
	// window is the state with shown in the window as conn, behind reconnecting and idle connected
	// behind it, in context order as the engine keeps them.
	window := func(conn engine.Connection) engine.UIState {
		return engine.UIState{Context: shown, Connection: conn, Connections: []engine.ConnectedCluster{
			{Context: shown, Connection: conn, Shown: true},
			{Context: behind, Connection: renewing},
			{Context: idle, Connection: connected},
		}}
	}
	// connecting is the state while the window's connect to shown waits, with entry as shown's entry
	// of Connections: the connect's own (connecting), or the connection a connect run made in the
	// background, which the waiting connect shows next (engine/connections.go, syncConnections).
	connecting := func(entry engine.Connection) engine.UIState {
		return engine.UIState{Context: shown, Connection: engine.Connection{State: engine.Connecting}, Connections: []engine.ConnectedCluster{
			{Context: shown, Connection: entry, Shown: true},
			{Context: behind, Connection: renewing},
		}}
	}
	for _, c := range []struct {
		name string
		ui   engine.UIState
		ctx  string
		want bool
	}{
		{"no context", window(renewing), "", false},
		{"the shown cluster, connected", window(connected), shown, false},
		{"the shown cluster, reconnecting", window(renewing), shown, true},
		{"the shown cluster, interrupted and reconnecting", window(engine.Connection{State: engine.Interrupted, Reconnecting: true}), shown, true},
		{"a cluster behind it, reconnecting", window(connected), behind, true},
		{"a cluster behind it, not reconnecting", window(renewing), idle, false},
		{"a cluster without a connection", window(renewing), unknown, false},
		{"the shown cluster, connecting", connecting(engine.Connection{State: engine.Connecting}), shown, false},
		{"the shown cluster, connecting, its connect run's connection reconnecting", connecting(renewing), shown, true},
	} {
		if got := reconnecting(c.ui, c.ctx); got != c.want {
			t.Errorf("%s: reconnecting(%q) = %v, want %v", c.name, c.ctx, got, c.want)
		}
	}
}

// reconnectBehind sets or clears the Reconnecting of a cluster connected behind the shown one, as
// the engine's markReconnecting does: on that cluster's own entry of Connections.
func reconnectBehind(f *fakeEngine, ctx string, on bool) {
	f.update(func(u *engine.UIState) {
		for i := range u.Connections {
			if u.Connections[i].Context == ctx {
				u.Connections[i].Connection.Reconnecting = on
			}
		}
	})
}

// rowMenuLabels opens the menu of a cluster's row in the list of all clusters and returns its
// items' labels (itemLabels: a greyed one ends in "(off)").
func rowMenuLabels(t *testing.T, sh *shell, ctx string) []string {
	t.Helper()
	sh.side.sec.menuModel = nil
	sh.side.sec.rowMenu(ctx, fyne.NewPos(10, 10))
	if sh.side.sec.menuModel == nil {
		t.Fatalf("%s's row has no menu", ctx)
	}
	return itemLabels(sh.side.sec.menuModel.Items)
}

// tileMenuLabels opens the menu of a cluster's hotbar tile and returns its items' labels.
func tileMenuLabels(t *testing.T, sh *shell, ctx string) []string {
	t.Helper()
	for i, ti := range tilesOf(sh.hot) {
		if ti.tile.Context == ctx {
			sh.hot.menuModel = nil
			sh.hot.tileMenu(i+1, fyne.NewPos(10, 10))
			if sh.hot.menuModel == nil {
				t.Fatalf("%s's tile has no menu", ctx)
			}
			return itemLabels(sh.hot.menuModel.Items)
		}
	}
	t.Fatalf("no tile of %s", ctx)
	return nil
}

// TestReconnectOfAClusterBehindTheShownOne: a cluster connected behind the shown one whose
// connection is made anew says so in the list of all clusters, where its ↻ is muted with
// "Reconnecting…" as its tooltip, in its row's and its hotbar tile's menus, which have
// "Reconnecting…" greyed, and in its open Connection Updates, whose Reconnect says "Reconnecting…",
// disabled. The shown cluster's Reconnect (its ↻ and row menu, the Cluster menu, the Overview's)
// and that of another cluster behind it stay offered. When the reconnect ends each is Reconnect
// again, and the ↻ reconnects that cluster by its context.
func TestReconnectOfAClusterBehindTheShownOne(t *testing.T) {
	f, _ := marksFake(t, true) // kind-cogpit shown; prod-eu-central (cBlink) and staging-us-east (cCold) among those behind it
	fakeReconnect(t, f)
	sh, _, q := newTestShell(t, f, Options{})
	fakeBlinkClock(sh) // the reconnecting cluster's dots blink on the test's clock, without a ticker (conndot_test.go)
	q.until(t, "the hotbar and the clusters", func() bool {
		return len(tilesOf(sh.hot)) == 7 && sh.side.sec.loaded && sh.side.sec.rows[cBlink] != nil
	})
	ring := func(ctx string) *navIconButton { return sh.side.sec.rows[ctx].reconnect }
	if b := ring(cBlink); !b.Visible() || b.muted || b.tip != reconnectTip {
		t.Fatalf("before its reconnect, %s's ↻: shown %v, muted %v, tooltip %q", cBlink, b.Visible(), b.muted, b.tip)
	}
	sh.kc.openConnectLog(cBlink, connectLogView{Context: cBlink})
	cl := sh.kc.connectLog
	if cl == nil || cl.reconnect == nil || cl.reconnect.Text != reconnectLabel || cl.reconnect.Disabled() {
		t.Fatal("before its reconnect, its Connection Updates offer no Reconnect")
	}

	reconnectBehind(f, cBlink, true)
	q.until(t, "its ↻ muted", func() bool { return ring(cBlink).muted })
	if b := ring(cBlink); !b.Visible() || b.tip != reconnectingText {
		t.Errorf("while it reconnects, its ↻: shown %v, tooltip %q", b.Visible(), b.tip)
	}
	q.until(t, "its Connection Updates' Reconnecting…", func() bool { return cl.reconnect.Text == reconnectingText })
	if !cl.reconnect.Disabled() {
		t.Error("while it reconnects, its Connection Updates' Reconnecting… is enabled")
	}
	greyed := reconnectingText + " (off)"
	if labels := rowMenuLabels(t, sh, cBlink); !slices.Contains(labels, greyed) || slices.Contains(labels, reconnectLabel) {
		t.Errorf("while it reconnects, its row's menu: %v", labels)
	}
	if labels := tileMenuLabels(t, sh, cBlink); !slices.Contains(labels, greyed) || slices.Contains(labels, reconnectLabel) {
		t.Errorf("while it reconnects, its tile's menu: %v", labels)
	}
	for _, ctx := range []string{cShown, cCold} {
		if b := ring(ctx); !b.Visible() || b.muted || b.tip != reconnectTip {
			t.Errorf("while %s reconnects, %s's ↻: shown %v, muted %v, tooltip %q", cBlink, ctx, b.Visible(), b.muted, b.tip)
		}
		if labels := rowMenuLabels(t, sh, ctx); !slices.Contains(labels, reconnectLabel) || slices.Contains(labels, greyed) {
			t.Errorf("while %s reconnects, %s's row menu: %v", cBlink, ctx, labels)
		}
	}
	if labels := itemLabels(sh.clusterMenuItems()); len(labels) == 0 || labels[0] != reconnectLabel {
		t.Errorf("while %s reconnects, the Cluster menu: %v", cBlink, labels)
	}
	if p := sh.view.cluster.reconnect; !p.Visible() || p.Text != reconnectLabel || p.Disabled() {
		t.Errorf("while %s reconnects, the Overview's Reconnect: shown %v, %q, disabled %v", cBlink, p.Visible(), p.Text, p.Disabled())
	}

	reconnectBehind(f, cBlink, false)
	q.until(t, "its ↻ back", func() bool { return !ring(cBlink).muted })
	if b := ring(cBlink); !b.Visible() || b.tip != reconnectTip {
		t.Errorf("after its reconnect, its ↻: shown %v, tooltip %q", b.Visible(), b.tip)
	}
	q.until(t, "its Connection Updates' Reconnect back", func() bool { return cl.reconnect.Text == reconnectLabel })
	if cl.reconnect.Disabled() {
		t.Error("after its reconnect, its Connection Updates' Reconnect is disabled")
	}
	if labels := rowMenuLabels(t, sh, cBlink); !slices.Contains(labels, reconnectLabel) || slices.Contains(labels, greyed) {
		t.Errorf("after its reconnect, its row's menu: %v", labels)
	}
	if labels := tileMenuLabels(t, sh, cBlink); !slices.Contains(labels, reconnectLabel) || slices.Contains(labels, greyed) {
		t.Errorf("after its reconnect, its tile's menu: %v", labels)
	}
	test.Tap(ring(cBlink))
	q.until(t, "cluster.reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })
	if got := reconnectCalls(t, f); got[0] != cBlink {
		t.Errorf("the ↻ reconnected %q, want %q", got[0], cBlink)
	}
}
