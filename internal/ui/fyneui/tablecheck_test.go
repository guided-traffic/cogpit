package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// selectionRefs are object.deleteMany's parameters.
type selectionRefs struct {
	Refs []model.ObjectRef `json:"refs"`
}

// withDeleteMany registers Delete of a selection in the fake as the engine has it, and answers it
// as the engine does: one confirmation naming every object, then nothing more (the fake deletes
// nothing).
func withDeleteMany(t testing.TB, f *fakeEngine) {
	t.Helper()
	err := engine.Register(f.reg, engine.Spec[selectionRefs]{
		ID: "object.deleteMany", Label: "Delete", Order: engine.OrderDelete, Danger: engine.DangerDestructive,
		Enabled: func(_ *engine.Session, p selectionRefs) error {
			for _, r := range p.Refs {
				if r.Kind == kindPortForwards {
					return engine.ErrNotApplicable
				}
			}
			return f.reason("object.deleteMany")
		},
		Targets: func(_ *engine.Session, p selectionRefs) ([]model.ObjectRef, error) { return p.Refs, nil },
		Run:     func(context.Context, *engine.Session, selectionRefs) (any, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handlers["object.deleteMany"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
		var p selectionRefs
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		c, uiCtx := f.confirmer, f.ui.Context
		f.mu.Unlock()
		req := engine.ConfirmRequest{Command: inv.ID, Label: "Delete", Danger: engine.DangerDestructive, Origin: inv.Origin, Context: uiCtx, Objects: p.Refs}
		for _, r := range p.Refs {
			if !slices.Contains(req.Namespaces, r.Namespace) {
				req.Namespaces = append(req.Namespaces, r.Namespace)
			}
		}
		ok, err := c.Confirm(ctx, req)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, engine.ErrDeclined
		}
		return nil, nil
	}
}

// heldKeys makes the desktop driver's modifier keys, those of the last key event it delivered,
// report mods until the test ends or the returned function runs: what the driver believes is held,
// which is stale after a ⌘ that went down outside the window (keyEventModifiers, G16).
func heldKeys(t *testing.T, mods fyne.KeyModifier) (release func()) {
	t.Helper()
	old := keyEventModifiers
	keyEventModifiers = func() fyne.KeyModifier { return mods }
	release = func() { keyEventModifiers = old }
	t.Cleanup(release)
	return release
}

// mouseShortcut is ⌘ in a mouse event on macOS and Ctrl elsewhere, as Fyne's GLFW driver reports
// them (convertMouseButton, internal/driver/glfw/window_desktop.go:486–497 in v2.8.2).
const mouseShortcut = fyne.KeyModifierControl

// clickWith clicks a table cell as Fyne's GLFW driver does: the primary button's press with the
// click's own modifier keys (mods, in the mouse event's convention), its release, then the tap.
func clickWith(t *testing.T, c *tableCell, mods fyne.KeyModifier) {
	t.Helper()
	ev := &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(1, 1)}, Button: desktop.MouseButtonPrimary, Modifier: mods}
	c.MouseDown(ev)
	c.MouseUp(ev)
	test.Tap(c)
}

// lastSelect is the last ui.select's parameters.
func lastSelect(t *testing.T, f *fakeEngine) selectModeParams {
	t.Helper()
	calls := f.callsOf("ui.select")
	if len(calls) == 0 {
		t.Fatal("no ui.select")
	}
	return decodeParams[selectModeParams](t, calls[len(calls)-1])
}

func refKeys(refs []model.ObjectRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Key()
	}
	return out
}

// TestRowCheckboxes: every row starts with a checkbox; a checkbox toggles its row without the
// drawer, ⇧ adds the rows from the last one checked, ⌘-click on a row toggles it, a plain click
// opens the drawer and checks nothing (A73); from one selected object the header shows the
// selection's count, Delete (greyed while the connection is interrupted, D-58) and a clear button.
func TestRowCheckboxes(t *testing.T) {
	f := connectedFake(t, 200)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	rows := f.tables["pods"].Rows
	tv := sh.view
	if _, cols := tv.length(); cols != len(tv.cols)+2 {
		t.Fatalf("%d table columns for %d view columns: want the checkbox and the ⋮ besides", cols, len(tv.cols))
	}

	box := cellAt(t, sh, 2, checkColumn)
	if box.content.kind != cellCheck || box.iconRes != tv.cellIcon(cellContent{kind: cellCheck}, false) {
		t.Fatalf("row 2's first cell: kind %v, icon %v", box.content.kind, box.iconRes)
	}
	test.Tap(box)
	q.until(t, "the row checked", func() bool { return len(sh.ui.Selected) == 1 })
	if p := lastSelect(t, f); p.Mode != "toggle" || !p.NoDrawer || len(p.Refs) != 1 || p.Refs[0] != rows[2].Ref {
		t.Fatalf("checkbox sent %+v", p)
	}
	settle(w)
	if c := cellAt(t, sh, 2, checkColumn); !c.checked || c.drawer || c.iconRes != tv.cellIcon(cellContent{kind: cellCheck}, true) || sh.ui.Drawer != nil {
		t.Fatalf("checked row: checked %v, the drawer's %v, checked icon %v, drawer %v", c.checked, c.drawer, c.iconRes == tv.cellIcon(cellContent{kind: cellCheck}, true), sh.ui.Drawer)
	}
	if !tv.check.bar.root.Visible() || tv.check.bar.count.text != "1 selected" { // from one row (owner, 2026-10-15)
		t.Errorf("selection bar for one object: visible %v, %q", tv.check.bar.root.Visible(), tv.check.bar.count.text)
	}
	if tv.check.bar.del.Disabled() {
		t.Error("Delete greyed while connected")
	}
	f.update(func(u *engine.UIState) { u.Connection.State = engine.Interrupted })
	q.until(t, "Delete greyed while interrupted", func() bool { return tv.check.bar.del.Disabled() })
	f.update(func(u *engine.UIState) { u.Connection.State = engine.Connected })
	q.until(t, "Delete back", func() bool { return !tv.check.bar.del.Disabled() })

	// ⇧ on another checkbox adds the rows between.
	clickWith(t, cellAt(t, sh, 5, checkColumn), fyne.KeyModifierShift)
	q.until(t, "the range", func() bool { return len(sh.ui.Selected) == 4 })
	between := []model.ObjectRef{rows[2].Ref, rows[3].Ref, rows[4].Ref, rows[5].Ref}
	if p := lastSelect(t, f); p.Mode != "add" || !slices.Equal(refKeys(p.Refs), refKeys(between)) {
		t.Fatalf("⇧-click sent %+v", p)
	}
	settle(w)
	if !tv.check.bar.root.Visible() || tv.check.bar.count.text != "4 selected" {
		t.Fatalf("selection bar visible %v: %q", tv.check.bar.root.Visible(), tv.check.bar.count.text)
	}

	// ⌘-click on a row toggles it, without the drawer.
	clickWith(t, cellAt(t, sh, 3, firstDataColumn+1), mouseShortcut)
	q.until(t, "the row toggled off", func() bool { return len(sh.ui.Selected) == 3 })
	if p := lastSelect(t, f); p.Mode != "toggle" || p.Refs[0] != rows[3].Ref || sh.ui.Drawer != nil {
		t.Fatalf("⌘-click sent %+v, drawer %v", p, sh.ui.Drawer)
	}

	// The bar's clear button empties the selection.
	test.Tap(tv.check.bar.clear)
	q.until(t, "the selection cleared", func() bool { return len(sh.ui.Selected) == 0 })
	settle(w)
	if tv.check.bar.root.Visible() {
		t.Error("the selection bar stays without a selection")
	}

	// A plain click opens the drawer and checks nothing (A73: ui.select's mode open).
	test.Tap(cellAt(t, sh, 7, firstDataColumn+1))
	q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil })
	if p := lastSelect(t, f); p.Mode != "open" || p.NoDrawer || len(p.Refs) != 1 || p.Refs[0] != rows[7].Ref {
		t.Fatalf("a click sent %+v", p)
	}
	if len(sh.ui.Selected) != 0 {
		t.Errorf("a click checked %v", sh.ui.Selected)
	}
}

// tableScroll is the open view's table's scroller: its offset is how far the table is scrolled.
func tableScroll(t *testing.T, sh *shell) *container.Scroll {
	t.Helper()
	for _, s := range findAll[*container.Scroll](sh.view.table) {
		if s.OnScrolled != nil { // the body's, which resourceTable hooks
			return s
		}
	}
	t.Fatal("no scroller in the table")
	return nil
}

// TestPlainClickKeepsTheCheckedRows: the owner's decision A73: a plain click on a row opens its
// drawer and keeps the checked rows checked. The clicked row is highlighted as the drawer's, its
// checkbox as it was; the selection bar and the header's checkbox still count the checked rows; a
// click on a checked row keeps it checked; ⌘-click still toggles, without moving the drawer or
// scrolling back to the first checked row.
func TestPlainClickKeepsTheCheckedRows(t *testing.T) {
	f := connectedFake(t, 200)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	rows := f.tables["pods"].Rows
	tv := sh.view
	checked, unchecked := tv.cellIcon(cellContent{kind: cellCheck}, true), tv.cellIcon(cellContent{kind: cellCheck}, false)

	test.Tap(cellAt(t, sh, 2, checkColumn))
	q.until(t, "row 2 checked", func() bool { return len(sh.ui.Selected) == 1 })
	test.Tap(cellAt(t, sh, 4, checkColumn))
	q.until(t, "row 4 checked", func() bool { return len(sh.ui.Selected) == 2 })
	want := refKeys([]model.ObjectRef{rows[2].Ref, rows[4].Ref})

	test.Tap(cellAt(t, sh, 7, firstDataColumn+1))
	q.until(t, "the drawer on row 7", func() bool { return sh.ui.Drawer != nil && *sh.ui.Drawer == rows[7].Ref })
	settle(w)
	if got := refKeys(sh.ui.Selected); !slices.Equal(got, want) {
		t.Fatalf("after a click on row 7 the selection is %v, want %v", got, want)
	}
	if p := lastSelect(t, f); p.Mode != "open" || p.NoDrawer || len(p.Refs) != 1 || p.Refs[0] != rows[7].Ref {
		t.Fatalf("the click sent %+v", p)
	}
	for _, r := range []struct {
		row           int
		drawer, check bool
	}{{2, false, true}, {4, false, true}, {7, true, false}, {5, false, false}} {
		box, name := cellAt(t, sh, r.row, checkColumn), cellAt(t, sh, r.row, firstDataColumn+1)
		icon := unchecked
		if r.check {
			icon = checked
		}
		if box.iconRes != icon || name.checked != r.check || name.drawer != r.drawer || box.drawer != r.drawer {
			t.Errorf("row %d: checked icon %v (want %v), the drawer's %v (want %v)", r.row, box.iconRes == checked, r.check, name.drawer, r.drawer)
		}
	}
	if !tv.check.bar.root.Visible() || tv.check.bar.count.text != "2 selected" || tv.check.state != checkSome {
		t.Errorf("selection bar %v %q, header state %d", tv.check.bar.root.Visible(), tv.check.bar.count.text, tv.check.state)
	}

	// A click on a checked row: its drawer, still checked; row 7 is no longer highlighted.
	test.Tap(cellAt(t, sh, 4, firstDataColumn+1))
	q.until(t, "the drawer on row 4", func() bool { return sh.ui.Drawer != nil && *sh.ui.Drawer == rows[4].Ref })
	settle(w)
	if got := refKeys(sh.ui.Selected); !slices.Equal(got, want) {
		t.Fatalf("after a click on the checked row 4 the selection is %v, want %v", got, want)
	}
	if c := cellAt(t, sh, 7, firstDataColumn+1); c.drawer {
		t.Error("row 7 stays marked as the drawer's after the drawer moved on")
	}
	if c := cellAt(t, sh, 4, checkColumn); c.iconRes != checked {
		t.Error("row 4 lost its check with the click")
	}

	// ⌘-click far down toggles that row; the drawer stays, and the table stays where it is.
	far := cellAt(t, sh, 150, firstDataColumn+1) // scrolls there
	scroll := tableScroll(t, sh)
	at := scroll.Offset
	clickWith(t, far, mouseShortcut)
	q.until(t, "row 150 checked", func() bool { return len(sh.ui.Selected) == 3 })
	settle(w)
	if *sh.ui.Drawer != rows[4].Ref {
		t.Errorf("⌘-click moved the drawer to %v", sh.ui.Drawer)
	}
	if scroll.Offset != at {
		t.Errorf("⌘-click scrolled the table from %v to %v (to the first checked row)", at, scroll.Offset)
	}
}

// TestHeaderCheckbox: the header's checkbox selects every row the view shows, shows a dash while
// some are selected, and clears the selection when all of them are.
func TestHeaderCheckbox(t *testing.T) {
	f := connectedFake(t, 20)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	head := func() *headerCell { return headerFor(t, sh, checkColumn) }
	if h := head(); !h.box || h.boxed != checkNone {
		t.Fatalf("the first header: checkbox %v, state %d", h.box, h.boxed)
	}
	test.Tap(head())
	q.until(t, "every row", func() bool { return len(sh.ui.Selected) == 20 })
	if p := lastSelect(t, f); !p.All || !p.NoDrawer {
		t.Fatalf("the header's checkbox sent %+v", p)
	}
	settle(w)
	if head().boxed != checkAll {
		t.Fatalf("header state %d with every row selected", head().boxed)
	}

	rows := f.tables["pods"].Rows
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{rows[1].Ref, rows[4].Ref} })
	q.until(t, "two selected", func() bool { return len(sh.ui.Selected) == 2 })
	settle(w)
	if head().boxed != checkSome {
		t.Fatalf("header state %d with two rows selected", head().boxed)
	}
	test.Tap(head())
	q.until(t, "every row again", func() bool { return len(sh.ui.Selected) == 20 })
	settle(w)
	test.Tap(head())
	q.until(t, "none", func() bool { return len(sh.ui.Selected) == 0 })
	if p := lastSelect(t, f); p.All || len(p.Refs) != 0 || !p.NoDrawer || p.Mode != "replace" {
		t.Fatalf("clearing sent %+v", p)
	}
}

// TestSelectionMenu: the menu of a row inside a selection of several acts on the selection: its
// count, Delete of every selected object (one confirmation listing each) and Clear Selection; a
// row outside it keeps its own menu. The one checked row's menu is the object's full menu with
// Clear Selection after a separator (N112), another row's has none.
func TestSelectionMenu(t *testing.T) {
	f := connectedFake(t, 20)
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	rows := f.tables["pods"].Rows

	own := labels(openMenu(t, sh, q, 2))
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{rows[2].Ref} })
	q.until(t, "one selected", func() bool { return len(sh.ui.Selected) == 1 })
	settle(w)
	m := openMenu(t, sh, q, 2)
	if got, want := labels(m), append(slices.Clone(own), "", "Clear Selection"); !slices.Equal(got, want) {
		t.Fatalf("the lone checked row's menu %v, want %v", got, want)
	}
	if got := labels(openMenu(t, sh, q, 6)); slices.Contains(got, "Clear Selection") || got[0] != "Show Details" {
		t.Fatalf("an unchecked row's menu beside one checked row %v", got)
	}
	item(t, m, "Clear Selection").Action()
	q.until(t, "the lone row cleared", func() bool { return len(sh.ui.Selected) == 0 })
	if p := lastSelect(t, f); len(p.Refs) != 0 || !p.NoDrawer || p.Mode != "replace" {
		t.Fatalf("Clear Selection sent %+v", p)
	}
	settle(w)
	if got := labels(openMenu(t, sh, q, 2)); !slices.Equal(got, own) {
		t.Fatalf("the row's menu unchecked again %v, want %v", got, own)
	}

	sel := []model.ObjectRef{rows[1].Ref, rows[2].Ref, rows[3].Ref}
	f.update(func(u *engine.UIState) { u.Selected = slices.Clone(sel) })
	q.until(t, "three selected", func() bool { return len(sh.ui.Selected) == 3 })
	settle(w)

	m = openMenu(t, sh, q, 2)
	if got := labels(m); !slices.Equal(got, []string{"3 objects selected", "", "Delete", "", "Clear Selection"}) || !m.Items[0].Disabled {
		t.Fatalf("selection menu %v", got)
	}
	if got := labels(openMenu(t, sh, q, 6)); len(got) == 0 || got[0] != "Show Details" {
		t.Fatalf("an unselected row's menu %v", got)
	}

	m = openMenu(t, sh, q, 3)
	item(t, m, "Delete").Action()
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	call := f.callsOf("object.deleteMany")
	if len(call) != 1 || !slices.Equal(refKeys(decodeParams[selectionRefs](t, call[0]).Refs), refKeys(sel)) {
		t.Fatalf("object.deleteMany %+v", call)
	}
	var texts []string
	for _, c := range findAll[*canvas.Text](sh.win.Canvas().Overlays().Top()) {
		texts = append(texts, c.Text)
	}
	all := strings.Join(texts, "\n")
	for _, r := range sel {
		if !strings.Contains(all, r.Name) {
			t.Errorf("the confirmation does not name %s:\n%s", r.Name, all)
		}
	}
	if !strings.Contains(all, "Objects (3)") {
		t.Errorf("the confirmation's count:\n%s", all)
	}
	// The rows in the place order Context › Namespaces › Objects, no sentence repeating them (WF16).
	if c, n, o := strings.Index(all, "Context"), strings.Index(all, "Namespaces"), strings.Index(all, "Objects (3)"); c < 0 || c >= n || n >= o {
		t.Errorf("the rows out of the order Context, Namespaces, Objects:\n%s", all)
	}
	if strings.Contains(all, "Delete 3 objects") || strings.Contains(all, "(context ") {
		t.Errorf("a sentence repeats the rows:\n%s", all)
	}
	test.Tap(sh.confirmer.current.cancel)
	q.until(t, "the dialog gone", func() bool { return sh.confirmer.current == nil })

	// Delete is greyed with its reason when it cannot run; Clear Selection empties the selection.
	f.disabled["object.deleteMany"] = "not permitted: you may not delete pods in default"
	m = openMenu(t, sh, q, 1)
	if d := item(t, m, "Delete"); !d.Disabled || !strings.Contains(d.Label, "not permitted") {
		t.Fatalf("a denied Delete: %q disabled %v", d.Label, d.Disabled)
	}
	item(t, m, "Clear Selection").Action()
	q.until(t, "the selection cleared", func() bool { return len(sh.ui.Selected) == 0 })
}

// TestSelectionBarDeletes: the header's Delete sends the whole selection.
func TestSelectionBarDeletes(t *testing.T) {
	f := connectedFake(t, 20)
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	rows := f.tables["pods"].Rows
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{rows[0].Ref, rows[9].Ref} })
	q.until(t, "two selected", func() bool { return len(sh.ui.Selected) == 2 })
	settle(w)
	test.Tap(sh.view.check.bar.del)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	if call := f.callsOf("object.deleteMany"); len(call) != 1 || len(decodeParams[selectionRefs](t, call[0]).Refs) != 2 {
		t.Fatalf("object.deleteMany %+v", call)
	}
	test.Tap(sh.confirmer.current.confirm)
	q.until(t, "the dialog gone", func() bool { return sh.confirmer.current == nil })
}

// setDeleteReason makes the fake's object.deleteMany disabled with a reason ("" enables it), as the
// engine's write gate does while the connection is down; under the lock, since the bar asks for the
// reason on another goroutine.
func setDeleteReason(f *fakeEngine, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled[deleteManyID] = reason
}

// TestSelectionBarDeleteNamesTheReason (the owner's audit decision of 2026-10-16, D-58 (c)): while
// the shown cluster's connection is not connected, or its login lapsed, the bar's Delete is greyed
// and its tooltip names the engine's reason as the row menus do, "Delete — connection interrupted";
// another state asks the engine again; a shown tooltip follows; the connection back, Delete works
// and its tooltip says what it does.
func TestSelectionBarDeleteNamesTheReason(t *testing.T) {
	f := connectedFake(t, 20)
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	rows := f.tables["pods"].Rows
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{rows[0].Ref, rows[9].Ref} })
	q.until(t, "two selected", func() bool { return len(sh.ui.Selected) == 2 })
	settle(w)
	bar := sh.view.check.bar
	if bar.del.Disabled() || bar.del.tip != deleteSelectedTip {
		t.Fatalf("connected: disabled %v, tooltip %q", bar.del.Disabled(), bar.del.tip)
	}

	setDeleteReason(f, "connection interrupted")
	f.update(func(u *engine.UIState) { u.Connection.State = engine.Interrupted })
	q.until(t, "the interruption's reason", func() bool {
		return bar.del.Disabled() && bar.del.tip == "Delete — connection interrupted"
	})
	if d := item(t, openMenu(t, sh, q, 0), "Delete"); d.Label != bar.del.tip {
		t.Errorf("the selection's menu says %q, the bar %q", d.Label, bar.del.tip)
	}
	sh.view.menu.Hide()
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(bar.del))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == "Delete — connection interrupted" })

	for _, c := range []struct {
		state  engine.ConnectionState
		lapsed bool
		reason string
	}{
		{engine.LoginExpired, false, "login expired: log in again first"},
		{engine.Disconnected, false, "no cluster connected"},
		{engine.Connected, true, "login lapsed: show the cluster to renew it"},
	} {
		setDeleteReason(f, c.reason)
		f.update(func(u *engine.UIState) {
			u.Connection.State = c.state
			u.Connections = []engine.ConnectedCluster{{Context: u.Context, Connection: u.Connection, Shown: true, LoginLapsed: c.lapsed}}
		})
		want := "Delete — " + c.reason
		q.until(t, want, func() bool { return bar.del.Disabled() && bar.del.tip == want && sh.tip.visibleText() == want })
	}

	setDeleteReason(f, "")
	f.update(func(u *engine.UIState) {
		u.Connection.State = engine.Connected
		u.Connections = []engine.ConnectedCluster{{Context: u.Context, Connection: u.Connection, Shown: true}}
	})
	q.until(t, "Delete back", func() bool {
		return !bar.del.Disabled() && bar.del.tip == deleteSelectedTip && sh.tip.visibleText() == deleteSelectedTip
	})
}

// renderSelectionInterrupted checks three pods of a real cluster's view, interrupts the connection
// and shows the selection bar's greyed Delete with its tooltip naming the reason.
func renderSelectionInterrupted(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	withDeleteMany(t, f)
	f.ui.Context = "prod-eu [config]"
	f.ui.Connection.Demo = false
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	now := interruptedAt.Add(4 * time.Minute)
	sh.now = func() time.Time { return now }
	fakeBlinkClock(sh)
	rows := sh.view.snap.Rows
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{rows[1].Ref, rows[2].Ref, rows[4].Ref} })
	q.until(t, "three selected", func() bool { return len(sh.ui.Selected) == 3 })
	setDeleteReason(f, "connection interrupted")
	interrupt(f, engine.Interrupted, now)
	bar := sh.view.check.bar
	q.until(t, "the reason", func() bool { return bar.del.tip == "Delete — connection interrupted" && sh.conn.root.Visible() })
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(bar.del))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == bar.del.tip })
	savePNG(t, w, name)
}

func TestRenderSelectionInterrupted(t *testing.T) {
	renderSelectionInterrupted(t, ThemeDark, "selection_interrupted_dark.png")
	renderSelectionInterrupted(t, ThemeLight, "selection_interrupted_light.png")
}

// renderSelection checks three pods, opens the selection's menu on one of them.
func renderSelection(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	for _, row := range []int{1, 2, 4} {
		test.Tap(cellAt(t, sh, row, checkColumn))
		q.until(t, "the row checked", func() bool { return sh.view.check.has(sh.view.snap.Rows[row].Ref) })
	}
	settle(w)
	c := cellAt(t, sh, 2, firstDataColumn)
	test.TapSecondaryAt(c, fyne.NewPos(60, c.Size().Height/2))
	q.until(t, "the selection menu", func() bool { return sh.view.menu != nil })
	settle(w)
	savePNG(t, w, name)
}

// renderClickKeepsChecks checks three pods, then clicks a fourth (A73): its drawer opens, the three
// stay checked, and the clicked row is highlighted with its checkbox empty.
func renderClickKeepsChecks(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	for _, row := range []int{1, 2, 4} {
		test.Tap(cellAt(t, sh, row, checkColumn))
		q.until(t, "the row checked", func() bool { return sh.view.check.has(sh.view.snap.Rows[row].Ref) })
	}
	clicked := sh.view.snap.Rows[6].Ref
	test.Tap(cellAt(t, sh, 6, firstDataColumn+1))
	q.until(t, "the drawer", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == clicked })
	if len(sh.ui.Selected) != 3 {
		t.Fatalf("the click left %d rows checked, want 3", len(sh.ui.Selected))
	}
	settle(w)
	savePNG(t, w, name)
}

func TestRenderClickKeepsTheCheckedRows(t *testing.T) {
	renderClickKeepsChecks(t, ThemeDark, "selection_click_dark.png")
	renderClickKeepsChecks(t, ThemeLight, "selection_click_light.png")
}

// renderLoneChecked checks one pod and opens its menu: the object's full menu, then Clear Selection
// after a separator (N112).
func renderLoneChecked(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	test.Tap(cellAt(t, sh, 2, checkColumn))
	q.until(t, "the row checked", func() bool { return sh.view.check.has(sh.view.snap.Rows[2].Ref) })
	settle(w)
	c := cellAt(t, sh, 2, firstDataColumn)
	test.TapSecondaryAt(c, fyne.NewPos(60, c.Size().Height/2))
	q.until(t, "the row menu", func() bool {
		m := sh.view.menuModel
		return sh.view.menu != nil && m != nil && m.Items[len(m.Items)-1].Label == "Clear Selection"
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderLoneCheckedRowMenu(t *testing.T) {
	renderLoneChecked(t, ThemeDark, "selection_one_menu_dark.png")
	renderLoneChecked(t, ThemeLight, "selection_one_menu_light.png")
}

func TestRenderSelectionDark(t *testing.T) { renderSelection(t, ThemeDark, "selection_menu_dark.png") }
func TestRenderSelectionLight(t *testing.T) {
	renderSelection(t, ThemeLight, "selection_menu_light.png")
}

// renderDeleteSelection shows the confirmation of Delete for twelve pods: the count, the first ten
// named in the message, every pod in the scrolling list.
func renderDeleteSelection(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	withDeleteMany(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	rows := f.tables["pods"].Rows
	var sel []model.ObjectRef
	for _, r := range rows[:12] {
		sel = append(sel, r.Ref)
	}
	f.update(func(u *engine.UIState) { u.Selected = sel })
	q.until(t, "twelve selected", func() bool { return len(sh.ui.Selected) == 12 })
	settle(w)
	test.Tap(sh.view.check.bar.del)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	settle(w)
	savePNG(t, w, name)
	test.Tap(sh.confirmer.current.cancel)
}

func TestRenderDeleteSelectionDark(t *testing.T) {
	renderDeleteSelection(t, ThemeDark, "delete_selection_dark.png")
}

func TestRenderDeleteSelectionLight(t *testing.T) {
	renderDeleteSelection(t, ThemeLight, "delete_selection_light.png")
}
