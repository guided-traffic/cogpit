package fyneui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// crashLoopStatus is what a container in the demo cluster reports for a crash-looping pod, which
// the demo changes every few seconds.
const crashLoopStatus = "Waiting: CrashLoopBackOff (back-off 5m0s restarting failed container=alert-api pod=alert-api-6d9c8b7f6-x2x4v_monitoring(047c82cc-1c0e-4b7e-9d1a-0f6e2d3c4b5a))"

// longDetail is a pod's drawer model whose every kind of value is long: a status pill, a pill in
// the Properties, a chip list, a link, plain text and an event with a long message and reason.
func longDetail(ref model.ObjectRef, status string) *engine.DetailModel {
	long := strings.Repeat("x", 400)
	return &engine.DetailModel{
		Ref: ref, Title: "Pod: " + ref.Name, Status: model.StatusError,
		Sections: []model.DetailSection{
			{Title: model.TitleProperties, Items: []model.DetailItem{
				{Name: "Status", Value: status, Status: model.StatusError},
				{Name: "Phase", Value: status, Pill: true},
				{Name: "Labels", Value: "a=b", Chips: []model.Chip{
					{Text: "app.kubernetes.io/very-long-label-name=" + long}, {Text: "tier=backend"}, {Text: "x=" + status, Status: model.StatusError}}},
				{Name: "Node", Value: long, Link: &model.ObjectRef{Kind: "nodes", Name: long}},
				{Name: "Command", Value: long},
				{Name: "Many", Value: "a\nb", Chips: []model.Chip{{Text: "a"}, {Text: "b"}}},
			}},
			{Title: "app", Group: "Containers", Items: []model.DetailItem{
				{Name: "Status", Value: status, Status: model.StatusError},
			}},
		},
		Events: []engine.EventItem{{Type: "Warning", Reason: "BackOff" + long[:60], Message: status + " " + long, Source: "kubelet, " + long[:80], Count: 12}},
	}
}

func openLongDrawer(t *testing.T) (*shell, fyne.Window, *mainQueue, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "pods")
	ref := podDrawerRef(t)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 })
	settle(w)
	return sh, w, q, ref
}

// show puts a detail model into the drawer the way a refresh does and lays the window out again.
func showDetail(sh *shell, w fyne.Window, q *mainQueue, m *engine.DetailModel) {
	sh.drawer.show(m.Ref, m, nil, sh.drawer.acts)
	q.drain()
	settle(w)
	q.drain()
	settle(w)
}

// TestDrawerWidthNeverFollowsItsContent: a drawer whose content grows (a 400-character value, a
// crash-looping container's status) and shrinks again keeps its width exactly, and so do the
// table and the window's minimum size; no widget of the details asks for more than the drawer has.
func TestDrawerWidthNeverFollowsItsContent(t *testing.T) {
	sh, w, q, ref := openLongDrawer(t)
	width := func() (drawer, table, window float32) {
		return sh.drawer.root.Size().Width, sh.view.root.Size().Width, sh.root.MinSize().Width
	}
	d0, t0, m0 := width()
	if d0 != sh.drawerW {
		t.Fatalf("the drawer is %v wide, the user's width is %v", d0, sh.drawerW)
	}

	showDetail(sh, w, q, longDetail(ref, crashLoopStatus))
	d1, t1, m1 := width()
	if d1 != d0 || t1 != t0 || m1 != m0 {
		t.Fatalf("a long value changed the layout: drawer %v→%v, table %v→%v, window minimum %v→%v", d0, d1, t0, t1, m0, m1)
	}
	if got, room := sh.drawer.details.MinSize().Width, sh.drawer.contentWidth(); got > room {
		t.Errorf("the details ask for %v, the drawer has %v for them", got, room)
	}
	if got := sh.drawer.scroll.Content.MinSize().Width; got > d0 {
		t.Errorf("the scrolled content asks for %v in a drawer of %v", got, d0)
	}

	// The status wraps: its pill has several lines, all within the value's width, and the full
	// text is what the copy icon copies.
	var wrapped *pill
	for _, p := range findAll[*pill](sh.drawer.details) {
		if p.text == crashLoopStatus && p.maxW > 0 {
			wrapped = p
			break
		}
	}
	if wrapped == nil {
		t.Fatal("no wrapping pill with the status")
	}
	if lines := len(wrapped.wrapped()); lines < 2 {
		t.Errorf("the status is on %d line(s), want it wrapped", lines)
	}
	if got := wrapped.MinSize().Width; got > sh.drawer.valueWidth() {
		t.Errorf("the status pill is %v wide, a value has %v", got, sh.drawer.valueWidth())
	}

	showDetail(sh, w, q, longDetail(ref, "Running"))
	d2, t2, m2 := width()
	if d2 != d0 || t2 != t0 || m2 != m0 {
		t.Fatalf("a short value changed the layout: drawer %v→%v, table %v→%v, window minimum %v→%v", d0, d2, t0, t2, m0, m2)
	}
}

// TestDrawerRebuildsToItsWidth: after a drag the details are built for the new width.
func TestDrawerRebuildsToItsWidth(t *testing.T) {
	sh, w, q, ref := openLongDrawer(t)
	showDetail(sh, w, q, longDetail(ref, crashLoopStatus))
	before := sh.drawer.valueWidth()
	dragHandle(t, sh, w, sh.hsplit, -100)
	q.drain()
	settle(w)
	if got := sh.drawer.valueWidth(); got != before+100 {
		t.Fatalf("values are built for %v, want %v", got, before+100)
	}
	if got := sh.drawer.details.MinSize().Width; got > sh.drawer.contentWidth() {
		t.Errorf("after the drag the details ask for %v, the drawer has %v", got, sh.drawer.contentWidth())
	}
}

// dragHandle drags the resize handle in panel by dx points, as the pointer does.
func dragHandle(t *testing.T, sh *shell, w fyne.Window, panel *fyne.Container, dx float32) {
	t.Helper()
	hs := findAll[*resizeHandle](panel)
	if len(hs) == 0 {
		t.Fatal("no resize handle")
	}
	h := hs[0]
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(h).AddXY(h.Size().Width/2, 40)
	test.MoveMouse(w.Canvas(), pos)
	test.Drag(w.Canvas(), pos, dx, 0)
	settle(w)
}

// TestDrawerWidthByDraggingItsEdge: dragging the drawer's left edge changes its width, the width
// stays within the bounds and the table's room, and it is stored when the drag ends.
func TestDrawerWidthByDraggingItsEdge(t *testing.T) {
	sh, w, _, _ := openLongDrawer(t)
	prefs := sh.app.Preferences()
	before := sh.drawer.root.Size().Width

	dragHandle(t, sh, w, sh.hsplit, -100)
	if got := sh.drawer.root.Size().Width; got != before+100 {
		t.Fatalf("dragging the edge 100 left: the drawer is %v wide, want %v", got, before+100)
	}
	if got := float32(prefs.Float(prefDrawerWidth)); got != before+100 {
		t.Fatalf("stored %v, want %v", got, before+100)
	}

	dragHandle(t, sh, w, sh.hsplit, 5000) // as far right as it goes: the minimum
	if got := sh.drawer.root.Size().Width; got != minDrawerWidth {
		t.Errorf("dragged shut: %v wide, want the minimum %v", got, minDrawerWidth)
	}
	dragHandle(t, sh, w, sh.hsplit, -5000) // as far left as it goes: the table keeps its room
	got := sh.drawer.root.Size().Width
	if got > maxDrawerWidth || sh.view.root.Size().Width < minTableRoom {
		t.Errorf("dragged open: the drawer is %v wide (maximum %v), the table %v (room %v)", got, maxDrawerWidth, sh.view.root.Size().Width, minTableRoom)
	}
}

// TestSidebarWidthByDraggingItsEdge is the same for the primary sidebar.
func TestSidebarWidthByDraggingItsEdge(t *testing.T) {
	sh, w, _, _ := openLongDrawer(t)
	prefs := sh.app.Preferences()
	side := sh.sideLay.got
	if side != defaultSidebarWidth {
		t.Fatalf("the sidebar starts %v wide, want the default %v", side, defaultSidebarWidth)
	}

	dragHandle(t, sh, w, sh.mainRow, 80)
	if got := sh.sideLay.got; got != side+80 {
		t.Fatalf("dragging the edge 80 right: the sidebar is %v wide, want %v", got, side+80)
	}
	if got := float32(prefs.Float(prefSidebarWidth)); got != side+80 {
		t.Fatalf("stored %v, want %v", got, side+80)
	}
	dragHandle(t, sh, w, sh.mainRow, -5000)
	if got := sh.sideLay.got; got != minSidebarWidth {
		t.Errorf("dragged shut: %v wide, want the minimum %v", got, minSidebarWidth)
	}
	dragHandle(t, sh, w, sh.mainRow, 5000)
	if got := sh.sideLay.got; got > maxSidebarWidth {
		t.Errorf("dragged open: %v wide, the maximum is %v", got, maxSidebarWidth)
	}
	if sh.drawer.root.Size().Width < minDrawerWidth || sh.view.root.Size().Width < minTableRoom-handleWidth-minDrawerWidth {
		t.Errorf("the sidebar took the room of the workspace")
	}
}

// TestPanelWidthsSurviveARestart: a new shell on the same preferences opens with both widths, and
// an invalid stored value falls back to the default.
func TestPanelWidthsSurviveARestart(t *testing.T) {
	sh, w, _, _ := openLongDrawer(t)
	dragHandle(t, sh, w, sh.hsplit, -60)
	dragHandle(t, sh, w, sh.mainRow, 30)
	wantDrawer, wantSide := sh.drawerW, sh.sidebarW
	if wantDrawer == defaultDrawerWidth || wantSide == defaultSidebarWidth {
		t.Fatalf("the drags changed nothing: %v, %v", wantDrawer, wantSide)
	}
	a := sh.app
	sh.stop()

	f := kindsFake(t, "pods")
	ref := podDrawerRef(t)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	w2 := test.NewTempWindow(t, nil)
	w2.SetPadded(false)
	q := &mainQueue{}
	sh2 := newShell(f, a, w2, Options{}, q.do)
	sh2.kc.startDir = t.TempDir
	w2.SetContent(sh2.root)
	w2.Resize(fyne.NewSize(1440, 900))
	sh2.start()
	t.Cleanup(func() { sh2.stop(); q.drain() })
	q.until(t, "the drawer", func() bool { return sh2.drawer.last != nil })
	settle(w2)
	if sh2.drawerW != wantDrawer || sh2.sidebarW != wantSide {
		t.Fatalf("restored %v and %v, want %v and %v", sh2.drawerW, sh2.sidebarW, wantDrawer, wantSide)
	}
	if got := sh2.drawer.root.Size().Width; got != wantDrawer {
		t.Errorf("the drawer opens %v wide, want %v", got, wantDrawer)
	}
	if got := sh2.sideLay.got; got != wantSide {
		t.Errorf("the sidebar opens %v wide, want %v", got, wantSide)
	}

	a.Preferences().SetFloat(prefDrawerWidth, -3)
	a.Preferences().SetFloat(prefSidebarWidth, 1e9)
	sh2.loadWidths()
	if sh2.drawerW != defaultDrawerWidth || sh2.sidebarW != maxSidebarWidth {
		t.Errorf("invalid stored widths gave %v and %v, want %v and %v", sh2.drawerW, sh2.sidebarW, defaultDrawerWidth, maxSidebarWidth)
	}
}

// TestRenderDrawerLongStatus renders the pod drawer with the crash-looping status to a PNG.
func TestRenderDrawerLongStatus(t *testing.T) {
	sh, w, q, ref := openLongDrawer(t)
	m := longDetail(ref, crashLoopStatus)
	// Keep the picture readable: the long unbroken values stay in the width tests.
	m.Sections[0].Items = m.Sections[0].Items[:2]
	m.Sections[0].Items = append(m.Sections[0].Items,
		model.DetailItem{Name: "Labels", Value: "a", Chips: []model.Chip{{Text: "app=alert-api"}, {Text: "tier=backend"}}},
		model.DetailItem{Name: "Node", Value: "kind-worker-3", Link: &model.ObjectRef{Kind: "nodes", Name: "kind-worker-3"}})
	m.Events[0] = engine.EventItem{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container alert-api in pod alert-api-6d9c8b7f6-x2x4v", Source: "kubelet", Count: 12}
	showDetail(sh, w, q, m)
	savePNG(t, w, "pod_drawer_long_status.png")
	if dir := os.Getenv("COGPIT_PNG_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "pod_drawer_long_status.png")); err != nil {
			t.Fatal(err)
		}
	}
}
