package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// navSettled waits until the window's reports of its navigator are done: an agent's value set then
// is not overwritten by a report of the user's that was still on its way.
func navSettled(t *testing.T, sh *shell, q *mainQueue) {
	t.Helper()
	q.until(t, "the navigator's reports", func() bool { return sh.side.rowRep.busy == 0 && sh.side.searchRep.busy == 0 })
}

// lastParams decodes the last call of a command, or fails.
func lastParams[P any](t *testing.T, f *fakeEngine, id string) P {
	t.Helper()
	calls := f.callsOf(id)
	if len(calls) == 0 {
		t.Fatalf("no %s", id)
	}
	var p P
	if err := json.Unmarshal(normalize(calls[len(calls)-1].Params), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// The navigator's search and keyboard row are engine state (A51): the window reports what the user
// types and where the keyboard goes, once per pause, and shows what an agent sets without taking the
// keyboard focus: a search opens the field, a row inside a closed group opens it, a row the tree
// does not have leaves the keyboard where it is, an empty search closes the field.
func TestNavigatorReportsAndShowsItsState(t *testing.T) {
	sh, w, q, f := navScene(t, ThemeDark)
	s, h, tr, c := sh.side, sh.side.head, sh.side.tree, w.Canvas()
	s.searchRep.delay = time.Hour // a slow runner's pause between two letters is no pause of the user's; fireTimer ends it

	test.Tap(h.searchB)
	test.Type(h.entry, "dep")
	fireTimer(s.searchRep.timer)
	q.until(t, "the search reported", func() bool { return f.UIState().Navigator.Search == "dep" })
	if n := len(f.callsOf("ui.setNavigatorSearch")); n != 1 {
		t.Errorf("%d reports for one word typed, want one after the pause", n)
	}
	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	fireTimer(s.searchRep.timer)
	q.until(t, "the cleared search reported", func() bool { return f.UIState().Navigator.Search == "" })

	c.Focus(tr)
	tr.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	tr.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	want := tr.focus
	q.until(t, "the row reported", func() bool { return f.UIState().Navigator.Row == want })
	if p := lastParams[navRowParams](t, f, "ui.setNavigatorRow"); p.Row != want || want == groupPrefix+"custom" {
		t.Errorf("reported %q, the keyboard on %q", p.Row, want)
	}

	// An agent's row inside a closed group opens the group; the keyboard stays the user's focus.
	vpa := "verticalpodautoscalers.autoscaling.k8s.io"
	c.Focus(sh.view.search)
	navSettled(t, sh, q)
	f.update(func(u *engine.UIState) { u.Navigator.Row = vpa })
	q.until(t, "the agent's row", func() bool { return tr.focus == vpa })
	if !tr.IsBranchOpen(groupPrefix+"config") || c.Focused() != sh.view.search {
		t.Errorf("Config open %v, focus on %T", tr.IsBranchOpen(groupPrefix+"config"), c.Focused())
	}
	f.update(func(u *engine.UIState) { u.Navigator.Row = "nosuchkind" })
	q.drain()
	if tr.focus != vpa {
		t.Errorf("a row the tree lacks moved the keyboard to %q", tr.focus)
	}
	f.update(func(u *engine.UIState) { u.Navigator.Row = engine.NavRowRoot })
	q.until(t, "the root", func() bool { return tr.focus == rootNode })

	// An agent's search opens the field without the focus and filters at once; cleared, it closes.
	navSettled(t, sh, q)
	f.update(func(u *engine.UIState) { u.Navigator.Search = "net" })
	q.until(t, "the agent's search", func() bool { return h.searching() && h.entry.Text == "net" && s.filter == "net" })
	if c.Focused() == h.entry {
		t.Error("an agent's search took the focus")
	}
	navSettled(t, sh, q)
	f.update(func(u *engine.UIState) { u.Navigator.Search = "" })
	q.until(t, "the search closed", func() bool { return !h.searching() && s.filter == "" })
	if got := f.callsOf("ui.setNavigatorSearch"); len(got) != 2 || s.searchRep.busy != 0 {
		t.Errorf("the window reported an agent's search back: %d reports, %d waiting", len(got), s.searchRep.busy)
	}
}

// A report of the window's waits for the pause: an agent's value that arrives meanwhile does not
// overwrite what the user is typing, and the user's last word is sent.
func TestNavigatorTypingWinsOverAnEcho(t *testing.T) {
	sh, _, q, f := navScene(t, ThemeDark)
	h := sh.side.head
	sh.side.searchRep.delay = time.Hour // the agent's value lands while the report waits; fireTimer ends the wait
	test.Tap(h.searchB)
	test.Type(h.entry, "po")
	f.update(func(u *engine.UIState) { u.Navigator.Search = "p" }) // an older value arriving late
	q.drain()
	if h.entry.Text != "po" {
		t.Fatalf("the field shows %q while the user types", h.entry.Text)
	}
	if !fireTimer(sh.side.searchRep.timer) {
		t.Fatal("no report waiting for the user's word")
	}
	q.until(t, "the user's word", func() bool { return f.UIState().Navigator.Search == "po" })
	if h.entry.Text != "po" {
		t.Errorf("the field shows %q", h.entry.Text)
	}
}

// The ring marks the keyboard's row only while the navigator has the focus from the keyboard (A53):
// not after a click, back with the next key; at the section too, and drilling in with a click shows
// none in the tree.
func TestNavigatorRingFollowsTheKeyboardOnly(t *testing.T) {
	sh, w, q, _ := navScene(t, ThemeDark)
	tr := sh.side.tree
	test.Tap(tr.rows["nodes"])
	q.until(t, "the Nodes view", func() bool { return sh.ui.View == "nodes" })
	if w.Canvas().Focused() != tr || tr.focus != "nodes" || tr.keyboardOn("nodes") {
		t.Fatalf("after a click: focused %T on %q, ring %v", w.Canvas().Focused(), tr.focus, tr.keyboardOn("nodes"))
	}
	tr.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if !tr.keyboardOn(tr.focus) {
		t.Errorf("no ring after ↓ on %q", tr.focus)
	}
	network := groupPrefix + "network"
	test.Tap(tr.rows[network]) // another row: a second click on nodes would be a double click
	if tr.focus != network || tr.keyboardOn(network) {
		t.Errorf("a click left the ring on, keyboard on %q", tr.focus)
	}
	tr.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn}) // a key that does not move: the ring shows
	if !tr.keyboardOn(network) {
		t.Error("no ring after ↵")
	}
}

// At the section too: a click shows no ring, a key does, and drilling in with a click shows none in
// the tree.
func TestNavigatorSectionRingFollowsTheKeyboardOnly(t *testing.T) {
	sh, w, q, _ := sectionScene(t, ThemeDark)
	n := sh.side.sec
	test.Tap(n.heads[".kube/"]) // a click on a heading: no ring
	if w.Canvas().Focused() != n || n.keyboardOn(n.focus) {
		t.Errorf("after a click at the section: focused %T, ring %v", w.Canvas().Focused(), n.keyboardOn(n.focus))
	}
	n.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight}) // opens .kube/ again: a key, the ring
	if !n.keyboardOn(headKube) {
		t.Errorf("no ring after → on %q", n.focus)
	}
	test.Tap(n.rows[secProd])
	q.until(t, "prod's tree", func() bool { return sh.side.tree.Visible() && w.Canvas().Focused() == sh.side.tree })
	if sh.side.tree.keyboardOn(rootNode) {
		t.Error("drilling in with a click shows the ring in the tree")
	}
}

// A source group's heading collapses and opens its group on a click (A63): the engine keeps the
// collapsed groups (ui.setNavigatorGroup), a collapsed heading counts its clusters, a search shows
// every group open, and an agent's change shows; a row an agent puts the keyboard on inside a
// collapsed group puts it on the heading.
func TestNavigatorSectionGroupsCollapse(t *testing.T) {
	sh, _, q, f := sectionScene(t, ThemeDark)
	n, h := sh.side.sec, sh.side.head
	test.Tap(n.heads[".kube/"])
	if got := n.visible[1:]; !slices.Equal(got, []string{"demo", headKube, headEKS, secEKS}) || n.heads[".kube/"].count.text != "2" ||
		n.heads[".kube/"].open {
		t.Fatalf("collapsed: rows %q, count %q", got, n.heads[".kube/"].count.text)
	}
	q.until(t, "the engine's state", func() bool { return slices.Equal(f.UIState().Navigator.Collapsed, []string{".kube/"}) })
	if p := lastParams[navGroupParams](t, f, "ui.setNavigatorGroup"); p != (navGroupParams{Group: ".kube/", Collapsed: true}) {
		t.Errorf("ui.setNavigatorGroup %+v", p)
	}
	if n.focus != headKube {
		t.Errorf("the keyboard on %q, want the heading clicked", n.focus)
	}

	h.openSearch("prod") // a search shows the collapsed group's matches
	if got := n.visible[1:]; !slices.Equal(got, []string{headKube, secProd}) || !n.heads[".kube/"].open {
		t.Errorf("searching: rows %q", got)
	}
	h.closeSearch()

	navSettled(t, sh, q)
	f.update(func(u *engine.UIState) { u.Navigator.Collapsed = []string{".kube/", "eks.yaml"} }) // an agent's
	q.until(t, "eks.yaml collapsed", func() bool { return slices.Equal(n.visible[1:], []string{"demo", headKube, headEKS}) })
	navSettled(t, sh, q)
	f.update(func(u *engine.UIState) { u.Navigator.Row = engine.NavRowContext(secEKS) })
	q.until(t, "the heading of the agent's row", func() bool { return n.focus == headEKS })

	test.Tap(n.heads["eks.yaml"])
	q.until(t, "eks.yaml open", func() bool { return slices.Equal(f.UIState().Navigator.Collapsed, []string{".kube/"}) })
	if !slices.Contains(n.visible, secEKS) {
		t.Errorf("rows %q", n.visible)
	}
	if got := n.heads["eks.yaml"].tipText(); got != "eks.yaml - 1 cluster, open" {
		t.Errorf("tooltip %q", got)
	}
}

// The engine's rejection of a stale report (a row of the level just left) changes nothing the user
// sees: no toast.
func TestNavigatorStaleReportIsQuiet(t *testing.T) {
	sh, _, q, f := sectionScene(t, ThemeDark)
	setHandler(f, "ui.setNavigatorRow", func(context.Context, engine.Invocation) (any, error) {
		return nil, engine.ErrNavRow
	})
	n := sh.side.sec
	sh.win.Canvas().Focus(n)
	n.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	// Under load the focus and the key can report apart (two calls); busy counts the waiting ones.
	q.until(t, "the refused report done", func() bool { return len(f.callsOf("ui.setNavigatorRow")) >= 1 && sh.side.rowRep.busy == 0 })
	if len(sh.toasts.Objects) != 0 {
		t.Errorf("%d toasts for a refused report", len(sh.toasts.Objects))
	}
}

func TestRenderNavigatorSectionCollapsedDark(t *testing.T) {
	renderNavigatorSectionCollapsed(t, ThemeDark, "navigator_section_collapsed_dark.png")
}

// renderNavigatorSectionCollapsed: the section with .kube/ collapsed (its heading counting two
// clusters), the keyboard's ring on it from the keyboard, and the pointer on it showing its tooltip.
func renderNavigatorSectionCollapsed(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, f := sectionScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	n := sh.side.sec
	w.Canvas().Focus(n)
	for _, k := range []fyne.KeyName{fyne.KeyUp, fyne.KeyLeft} { // from kind-cogpit ↑ to .kube/, ← collapses it
		n.TypedKey(&fyne.KeyEvent{Name: k})
	}
	q.until(t, "collapsed", func() bool { return slices.Equal(f.UIState().Navigator.Collapsed, []string{".kube/"}) })
	settle(w)
	sh.tip.delay = time.Millisecond
	head := n.heads[".kube/"]
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(head).AddXY(40, head.Size().Height/2))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}
