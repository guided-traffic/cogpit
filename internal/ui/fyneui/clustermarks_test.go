package fyneui

import (
	"context"
	"image/color"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Several clusters connected in the window (D-69 stage 2, clustermarks.go).

// The clusters of the marks' scene, one per mark.
const (
	cShown  = "kind-cogpit"     // shown, connected: filled green
	cBlink  = "prod-eu-central" // in the background, reconnecting: blinking
	cCold   = "staging-us-east" // in the background, cold: a green ring
	cLapsed = "dev-hfi"         // in the background, its login lapsed: a grey ring
	cWarm   = "demo"            // in the background, warm: filled green
	cFailed = "qa-eu-west"      // its last connect failed: red
)

// marksFake is kind-cogpit shown with four more clusters connected behind it (warm, cold, lapsed and
// one whose reconnect the test starts) and one whose last connect failed, on the hotbar and in the
// navigator's section (scope: the section, else kind-cogpit's tree).
func marksFake(t *testing.T, section bool) (*fakeEngine, *fakeHotbar) {
	t.Helper()
	f := connectedFake(t, 200)
	f.contexts = append(fixtureContexts(), engine.ContextInfo{Name: cFailed, Cluster: "qa-eu-west",
		Server: "https://api.qa-eu-west.example.com:6443", ConnectProblem: true})
	f.ui.Context = cShown
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.ui.Connections = []engine.ConnectedCluster{ // sorted by context id, as the engine keeps them
		{Context: cWarm, Connection: engine.Connection{State: engine.Connected, Demo: true}},
		{Context: cLapsed, Connection: engine.Connection{State: engine.Connected}, LoginLapsed: true},
		{Context: cShown, Connection: engine.Connection{State: engine.Connected}, Shown: true},
		{Context: cBlink, Connection: engine.Connection{State: engine.Connected}},
		{Context: cCold, Connection: engine.Connection{State: engine.Connected}, Cold: true},
	}
	if section {
		f.ui.NavigatorScope = engine.NavScopeLocal
	}
	fakeNavScope(t, f)
	if c, ok := realCommands(t).Get("cluster.disconnect"); ok {
		probe[contextParams](t, f, c)
	}
	fh := withHotbar(f, cShown, cBlink, cCold, cLapsed, cWarm, cFailed, "legacy-onprem [.kube/old.yaml]")
	fh.missing["legacy-onprem [.kube/old.yaml]"] = true
	return f, fh
}

// marksScene shows marksFake; the blinker's clock is the test's.
func marksScene(t *testing.T, mode ThemeMode, section bool) (*shell, fyne.Window, *mainQueue, *fakeEngine, *blinkClock) {
	t.Helper()
	f, _ := marksFake(t, section)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the hotbar and the clusters", func() bool {
		return len(tilesOf(sh.hot)) == 7 && sh.side.sec.loaded && len(sh.kinds) > 0
	})
	return sh, w, q, f, fakeBlinkClock(sh)
}

// interruptInTheBackground makes cBlink's connection interrupted, as its connection watch does.
func interruptInTheBackground(f *fakeEngine, now time.Time) {
	f.update(func(u *engine.UIState) {
		for i := range u.Connections {
			if u.Connections[i].Context == cBlink {
				u.Connections[i].Connection = engine.Connection{State: engine.Interrupted, Since: now,
					NextAttempt: now.Add(8 * time.Second), Reason: "no answer from the API server within 5s"}
			}
		}
	})
}

// tileOf is the hotbar's tile of a cluster.
func tileOf(sh *shell, ctx string) *hotbarTile {
	for _, ti := range tilesOf(sh.hot) {
		if ti.tile.Context == ctx {
			return ti
		}
	}
	return nil
}

// tileDot is a tile's connection dot.
func tileDot(ti *hotbarTile) *connDot { return test.WidgetRenderer(ti).(*hotbarTileRenderer).dot }

// drawnShape is a dot's circle as drawn: its fill, its line and the line's width.
func drawnShape(d *connDot) (fill, stroke color.NRGBA, width float32) {
	c := test.WidgetRenderer(d).Objects()[0].(*canvas.Circle)
	conv := func(c color.Color) color.NRGBA { return color.NRGBAModel.Convert(c).(color.NRGBA) }
	return conv(c.FillColor), conv(c.StrokeColor), c.StrokeWidth
}

// tokenColor is a theme colour at full alpha.
func tokenColor(token fyne.ThemeColorName) color.NRGBA {
	return color.NRGBAModel.Convert(themeColor(fyne.CurrentApp().Settings().Theme(), token)).(color.NRGBA)
}

// checkMark fails unless a dot is drawn filled in token, or as a ring in it.
func checkMark(t *testing.T, d *connDot, token fyne.ThemeColorName, ring bool, where string) {
	t.Helper()
	fill, stroke, width := drawnShape(d)
	want := tokenColor(token)
	switch {
	case ring && (fill.A != 0 || stroke != want || width != ringStroke):
		t.Errorf("%s: fill %v, line %v × %v; want a ring in %s", where, fill, stroke, width, token)
	case !ring && (fill != want || width != 0):
		t.Errorf("%s: fill %v, line width %v; want filled %s", where, fill, width, token)
	}
}

// Every dot of a cluster shows its mark (D-69 item 5): the shown and a warm cluster filled green, a
// cold one a green ring, one whose login lapsed a grey ring, one whose last connect failed red, at
// the same size; the tooltip names the state. The shown cluster's tile alone is raised.
func TestDotsShowEachClustersMark(t *testing.T) {
	sh, w, q, _, _ := marksScene(t, ThemeDark, true)
	q.until(t, "the section's rows", func() bool { return sh.side.sec.rows[cCold] != nil })
	// The failed connect's red comes with the contexts' read, which may land after the tiles were
	// drawn (CI's race shard, 2026-10-16: the tile stayed grey until the read refreshed the tiles
	// too): wait for it on the tile and the row.
	red := tokenColor(colorStatusError)
	q.until(t, "the failed cluster's red", func() bool {
		settle(w) // the dots draw on refresh
		ti, row := tileOf(sh, cFailed), sh.side.sec.rows[cFailed]
		if ti == nil || row == nil {
			return false
		}
		tf, _, _ := drawnShape(tileDot(ti))
		rf, _, _ := drawnShape(row.dot)
		return tf == red && rf == red
	})
	settle(w)
	marks := []struct {
		ctx   string
		token fyne.ThemeColorName
		ring  bool
		tip   string
	}{
		{cShown, colorContainerRunning, false, "Connected"},
		{cWarm, colorContainerRunning, false, markWarm},
		{cCold, colorContainerRunning, true, markCold},
		{cLapsed, colorStatusUnknown, true, markLapsed},
		{cFailed, colorStatusError, false, markFailed},
	}
	size := tileDot(tileOf(sh, cShown)).Size()
	for _, m := range marks {
		ti, row := tileOf(sh, m.ctx), sh.side.sec.rows[m.ctx]
		if ti == nil || row == nil {
			t.Fatalf("%s: tile %v, row %v", m.ctx, ti != nil, row != nil)
		}
		checkMark(t, tileDot(ti), m.token, m.ring, m.ctx+"'s tile")
		checkMark(t, row.dot, m.token, m.ring, m.ctx+"'s row")
		if got := tileDot(ti).Size(); got != size {
			t.Errorf("%s's dot is %v, the shown one's %v: the layout moved", m.ctx, got, size)
		}
		if tip := row.dot.tipText(); !strings.HasPrefix(tip, m.tip) || !strings.Contains(tip, "Click for its connection updates") {
			t.Errorf("%s's tooltip %q, want it to start with %q", m.ctx, tip, m.tip)
		}
		if ti.active() != (m.ctx == cShown) {
			t.Errorf("%s's tile raised: %v", m.ctx, ti.active())
		}
	}
	if !strings.Contains(tileOf(sh, cCold).tipText(), markCold) {
		t.Errorf("the cold tile's tooltip %q", tileOf(sh, cCold).tipText())
	}
	checkMark(t, sh.top.dot, colorContainerRunning, false, "the top bar")
	checkMark(t, sh.status.dot, colorContainerRunning, false, "the status bar")
}

// A cluster connected in the background that reconnects blinks on every one of its dots, the shown
// cluster's do not; a green ring blinks filled; when it is back, the blink ends after its minimum
// and its mark returns (D-69 item 7).
func TestABackgroundReconnectBlinksItsDots(t *testing.T) {
	sh, w, q, f, clk := marksScene(t, ThemeDark, true)
	q.until(t, "the section's rows", func() bool { return sh.side.sec.rows[cBlink] != nil })
	interruptInTheBackground(f, clk.now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking(cBlink) })
	if sh.blink.blinking(cShown) || !clk.running {
		t.Errorf("the shown cluster blinks %v, ticker %v", sh.blink.blinking(cShown), clk.running)
	}
	if n := len(dotsOf(w, cBlink)); n < 2 {
		t.Fatalf("%d dots of the background cluster", n)
	}
	checkDots(t, w, cBlink, colorContainerRunning, alphaFull, "the first phase")
	clk.after(blinkPhase)
	checkDots(t, w, cBlink, colorContainerRunning, alphaDim, "the second phase")
	checkDots(t, w, cShown, colorContainerRunning, alphaFull, "the shown cluster")
	if tip := sh.side.sec.rows[cBlink].dot.tipText(); !strings.HasPrefix(tip, "Reconnecting…"+markBackground) ||
		!strings.Contains(tip, "next attempt") {
		t.Errorf("the reconnecting row's tooltip %q", tip)
	}

	f.update(func(u *engine.UIState) {
		for i := range u.Connections {
			if u.Connections[i].Context == cBlink {
				u.Connections[i].Connection, u.Connections[i].Cold = engine.Connection{State: engine.Connected}, true
			}
		}
	})
	q.until(t, "connected again", func() bool {
		c, _ := sh.ui.ConnectionOf(cBlink)
		return c.Connection.State == engine.Connected
	})
	clk.after(blinkMinimum)
	if sh.blink.any() || clk.running {
		t.Errorf("still blinking: %v, ticker %v", sh.blink.any(), clk.running)
	}
	checkMark(t, sh.side.sec.rows[cBlink].dot, colorContainerRunning, true, "back, cold")
}

// A click on a connected cluster's tile, section row or welcome-page row only shows it: no login
// program is shown for approval, even one that changed, and the shown cluster's tile does
// nothing. Its menus say Show, not Connect.
func TestAConnectedClusterIsShownWithoutAReview(t *testing.T) {
	f, fh := marksFake(t, true)
	fh.exec[cCold] = engine.ExecChanged
	for i := range f.contexts {
		if f.contexts[i].Name == cCold {
			f.contexts[i].Exec = engine.ExecChanged
		}
	}
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the clusters", func() bool { return len(tilesOf(sh.hot)) == 7 && sh.side.sec.rows[cCold] != nil })
	settle(w)
	test.Tap(tileOf(sh, cShown))
	test.Tap(tileOf(sh, cCold))
	q.until(t, "the tile's show", func() bool { return len(f.callsOf("hotbar.openCluster")) == 1 })
	if c := f.callsOf("hotbar.openCluster")[0]; decodeParams[hotbarSlotParams](t, c).Slot != 3 {
		t.Errorf("hotbar.openCluster %s", c.Params)
	}
	if n := len(f.callsOf("kubeconfig.pendingApprovals")); n != 0 {
		t.Errorf("a connected cluster's login was shown for approval %d times", n)
	}

	f.update(func(u *engine.UIState) { u.Context, u.NavigatorScope = cShown, engine.NavScopeLocal })
	q.until(t, "the section", func() bool { return sh.side.sec.Visible() && sh.side.sec.rows[cLapsed] != nil })
	sh.side.sec.rowMenu(cLapsed, fyne.NewPos(10, 10))
	if m := sh.side.sec.menuModel; m == nil || m.Items[0].Label != "Show" {
		t.Errorf("a connected cluster's row menu: %v", m)
	}
	sh.side.sec.rowMenu(cFailed, fyne.NewPos(10, 10))
	if m := sh.side.sec.menuModel; m == nil || m.Items[0].Label != "Connect" {
		t.Errorf("a cluster not connected: %v", m)
	}
	sh.side.sec.tapped(cLapsed)
	q.until(t, "the row's show", func() bool {
		calls := scopeCalls(t, f)
		return len(calls) > 0 && calls[len(calls)-1] == engine.NavScopeCluster(cLapsed)
	})
	if n := len(f.callsOf("kubeconfig.pendingApprovals")); n != 0 {
		t.Errorf("approvals asked %d times", n)
	}
}

// Disconnect in a connected cluster's tile and row menus passes its context and leaves the shown
// cluster alone; the shown cluster's Disconnect is the window's as before; a cluster not connected
// has none.
func TestDisconnectInAClustersMenuNamesIt(t *testing.T) {
	sh, _, q, f, _ := marksScene(t, ThemeDark, false)
	item := func(ctx string) *fyne.MenuItem {
		for _, it := range sh.clusterItems(ctx) {
			if it.Label == "Disconnect" {
				return it
			}
		}
		return nil
	}
	if item(cFailed) != nil {
		t.Error("a cluster not connected offers Disconnect")
	}
	item(cCold).Action()
	q.until(t, "the disconnect", func() bool { return len(f.callsOf("cluster.disconnect")) == 1 })
	if p := decodeParams[contextParams](t, f.callsOf("cluster.disconnect")[0]); p.Context != cCold {
		t.Errorf("cluster.disconnect %+v", p)
	}
	item(cShown).Action()
	q.until(t, "the shown one's", func() bool { return len(f.callsOf("cluster.disconnect")) == 2 })
	if p := string(f.callsOf("cluster.disconnect")[1].Params); strings.Contains(p, "context") {
		t.Errorf("the shown cluster's Disconnect sent %s", p)
	}
	tileMenu := func(ctx string) {
		for i, ti := range tilesOf(sh.hot) {
			if ti.tile.Context == ctx {
				sh.hot.tileMenu(i+1, fyne.NewPos(10, 10))
			}
		}
	}
	tileMenu(cWarm)
	labels := []string{}
	for _, it := range sh.hot.menuModel.Items {
		labels = append(labels, it.Label)
	}
	if got := strings.Join(labels, ","); !strings.HasPrefix(got, "Show,") || !strings.Contains(got, "Disconnect") {
		t.Errorf("a connected tile's menu %q", got)
	}
}

// The tab bar greys a tab only while its cluster is not connected: a tab of another connected
// cluster is not greyed (D-69).
func TestTabsGreyOnlyWithoutAConnection(t *testing.T) {
	f, _ := marksFake(t, true)
	f.ui.Tabs = []engine.ViewTab{
		{ID: "tab-1", Context: cShown, View: kindPods, Title: "Pods - kind-cogpit", Sort: engine.DefaultSort},
		{ID: "tab-2", Context: cCold, View: "services", Title: "Services - staging-us-east", Sort: engine.DefaultSort},
		{ID: "tab-3", Context: cFailed, View: "nodes", Title: "Nodes - qa-eu-west", Sort: engine.DefaultSort},
	}
	f.ui.ActiveTab = "tab-1"
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the tabs", func() bool { return len(sh.tabs.shown) == 3 })
	for i, want := range []bool{true, true, false} {
		if got := sh.tabs.shown[i].live; got != want {
			t.Errorf("%s: live %v, want %v", sh.tabs.shown[i].tab.ID, got, want)
		}
	}
	f.update(func(u *engine.UIState) { u.Connections = u.Connections[:2] }) // staging-us-east disconnected
	q.until(t, "the greyed tab", func() bool { return !sh.tabs.shown[1].live })
}

// A connect that fails while another cluster was shown leaves the window on the failed cluster
// (D-97, the owner's WF40: "Immer"): its error with Connect Again and Connection Updates, its dots
// red, nothing connected or shown by itself; the cluster shown before stays connected behind it.
func TestAFailedConnectStaysOnItsCluster(t *testing.T) {
	sh, w, q, f, _ := marksScene(t, ThemeDark, false)
	f.update(func(u *engine.UIState) { // a connect to qa-eu-west starts: kind-cogpit stays connected behind it
		u.Context, u.Connection = cFailed, engine.Connection{State: engine.Connecting}
		for i := range u.Connections {
			u.Connections[i].Shown = false
		}
	})
	q.until(t, "connecting", func() bool { return sh.ui.Connection.State == engine.Connecting })
	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: engine.ConnectError, Error: "dial tcp 10.0.0.9:6443: connect: connection refused"}
	})
	q.until(t, "the error", func() bool { return sh.ui.Connection.State == engine.ConnectError })
	q.drain()
	if calls := f.callsOf("cluster.connect"); len(calls) != 0 || sh.ui.Context != cFailed {
		t.Errorf("cluster.connect %v, shown %q", calls, sh.ui.Context)
	}
	// The red comes with the contexts' read, which may land after the tile was drawn (as in
	// TestDotsShowEachClustersMark): wait for it.
	red := tokenColor(colorStatusError)
	q.until(t, "the failed tile red", func() bool {
		settle(w)
		ti := tileOf(sh, cFailed)
		if ti == nil {
			return false
		}
		fill, _, width := drawnShape(tileDot(ti))
		return fill == red && width == 0
	})
	if !sh.view.failedBox.Visible() || sh.view.connectButton.Visible() {
		t.Error("the failed connect's buttons are not shown")
	}
	if got := sh.view.placeholderText.text; !strings.Contains(got, "Could not connect to "+cFailed) {
		t.Errorf("placeholder %q", got)
	}
}

// renderFailedConnect draws the window after a connect failed while another cluster was shown: the
// window on the failed cluster, its error, Connect Again and Connection Updates (D-97).
func renderFailedConnect(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, f, _ := marksScene(t, mode, false)
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = cFailed, engine.Connection{State: engine.ConnectError, Error: "the login through walk-login.sh did not finish within 5 minutes; Cogpit ended it"}
		for i := range u.Connections {
			u.Connections[i].Shown = false
		}
		u.View = ""
	})
	q.until(t, "the error", func() bool { return sh.view.failedBox.Visible() })
	savePNG(t, w, name)
}

func TestRenderAFailedConnectStaysDark(t *testing.T) {
	renderFailedConnect(t, ThemeDark, "connect_failed_stays_dark.png")
}

func TestRenderAFailedConnectStaysLight(t *testing.T) {
	renderFailedConnect(t, ThemeLight, "connect_failed_stays_light.png")
}

// Without another connected cluster a failed connect stays shown, as before: the welcome page says
// why, nothing connects by itself.
func TestAFailedConnectWithoutAnotherClusterStays(t *testing.T) {
	f := connectedFake(t, 10)
	f.ui.Context, f.ui.Connection = cShown, engine.Connection{State: engine.Connected}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = cFailed, engine.Connection{State: engine.Connecting}
	})
	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: engine.ConnectError, Error: "refused"}
	})
	q.until(t, "the error", func() bool { return sh.ui.Connection.State == engine.ConnectError })
	q.drain()
	if n := len(f.callsOf("cluster.connect")); n != 0 || sh.ui.Context != cFailed {
		t.Errorf("%d connects, shown %q", n, sh.ui.Context)
	}
}

// The welcome page's rows show a dot for every connected cluster with its mark.
func TestWelcomeRowsShowTheConnectedClusters(t *testing.T) {
	f, _ := marksFake(t, true)
	f.ui.Welcome = true
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the rows", func() bool { return sh.tabs.welcome.shown && len(sh.tabs.welcome.rows) >= 6 })
	dots := map[string]*connDot{}
	for _, r := range sh.tabs.welcome.rows {
		for _, d := range findAll[*connDot](r) {
			dots[d.context] = d
		}
	}
	for _, ctx := range []string{cShown, cWarm, cCold, cLapsed, cBlink} {
		if dots[ctx] == nil {
			t.Errorf("no dot on %s's row", ctx)
		}
	}
	if d := dots[cCold]; d != nil {
		checkMark(t, d, colorContainerRunning, true, "the cold row")
	}
	if dots[cFailed] != nil {
		t.Error("a cluster not connected has a dot on the welcome page")
	}
}

// diagnosticsClustersFake is diagnosticsFake with two clusters connected: kind-dev shown, dev-hfi in
// the background.
func diagnosticsClustersFake(t *testing.T) *fakeEngine {
	t.Helper()
	f := diagnosticsFake(t)
	r := engine.ReportOf(f.Diagnostics(), true)
	r.Clusters = []engine.DiagCluster{
		{Context: "dev-hfi", Requests: 12, Open: 2, Warm: 1, Objects: 40, Bytes: 52000},
		{Context: "kind-dev", Shown: true, Requests: 41, Open: 3, Warm: 2, Objects: 352, Bytes: 1800000},
	}
	f.handlers["diagnostics.get"] = func(context.Context, engine.Invocation) (any, error) { return r, nil }
	return f
}

// The Diagnostics tab with two clusters connected, dark: a line for each under the summary.
func TestRenderDiagnosticsClustersDark(t *testing.T) {
	f := diagnosticsClustersFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	openDiagnostics(f)
	q.until(t, "the clusters' lines", func() bool {
		d := diagnosticsTabOf(sh)
		return d != nil && len(d.clusters.Objects) == 3 && sh.dock.open()
	})
	q.drain()
	savePNG(t, w, "diagnostics_clusters_dark.png")
}

// The Diagnostics tab lists every connected cluster's sums, the shown one first, while more than one
// is connected (D-69 item 3).
func TestDiagnosticsListsEachCluster(t *testing.T) {
	f := diagnosticsClustersFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	openDiagnostics(f)
	q.until(t, "the clusters' lines", func() bool { d := diagnosticsTabOf(sh); return d != nil && len(d.clusters.Objects) == 3 })
	d := diagnosticsTabOf(sh)
	lines := []string{d.clusters.Objects[1].(*textLine).text, d.clusters.Objects[2].(*textLine).text}
	if !strings.HasPrefix(lines[0], "kind-dev (shown) · 41 API requests · 3 open watches, 2 warm · 352 objects") ||
		!strings.HasPrefix(lines[1], "dev-hfi (in the background) · 12 API requests") {
		t.Errorf("lines %q", lines)
	}
}

// The marks on the hotbar and in the navigator's section, dark and light (D-69 item 5): kind-cogpit
// shown, prod-eu-central reconnecting in the background (the blink's dimmed phase), staging-us-east
// cold (a green ring), dev-hfi's login lapsed (a grey ring), demo warm, qa-eu-west's connect failed
// (red), legacy-onprem missing; the tooltip of the cold tile, and of the lapsed row's dot.
func TestRenderHotbarMarksDark(t *testing.T) {
	renderMarks(t, ThemeDark, false, "hotbar_marks_dark.png")
}
func TestRenderHotbarMarksLight(t *testing.T) {
	renderMarks(t, ThemeLight, false, "hotbar_marks_light.png")
}
func TestRenderNavigatorSectionMarksDark(t *testing.T) {
	renderMarks(t, ThemeDark, true, "navigator_section_marks_dark.png")
}
func TestRenderNavigatorSectionMarksLight(t *testing.T) {
	renderMarks(t, ThemeLight, true, "navigator_section_marks_light.png")
}

func renderMarks(t *testing.T, mode ThemeMode, section bool, name string) {
	sh, w, q, f, clk := marksScene(t, mode, section)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	now := interruptedAt.Add(time.Minute)
	sh.now = func() time.Time { return now }
	interruptInTheBackground(f, now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking(cBlink) })
	q.until(t, "the tiles read again", func() bool {
		ti := tileOf(sh, cBlink)
		return ti != nil && ti.tile.Connection == engine.Interrupted
	})
	clk.after(blinkPhase)
	settle(w)
	sh.tip.delay = time.Millisecond
	target := fyne.CanvasObject(tileOf(sh, cCold))
	if section {
		target = sh.side.sec.rows[cLapsed].dot
	}
	test.MoveMouse(w.Canvas(), middleOf(target))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}
