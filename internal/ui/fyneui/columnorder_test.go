package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// handleColumnOrder makes the fake answer ui.setColumnOrder as the engine does: the UI state
// records the order and the view's snapshot carries it.
func handleColumnOrder(f *fakeEngine) {
	f.handlers["ui.setColumnOrder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p orderParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		s := *f.tables[p.View]
		s.Order = nil
		if !p.Reset {
			s.Order = engine.ColumnOrder(s.Columns, p.Order)
		}
		s.Revision++
		f.tables[p.View] = &s
		f.mu.Unlock()
		f.update(func(u *engine.UIState) {
			if p.Reset {
				delete(u.ColumnOrder, p.View)
				return
			}
			if u.ColumnOrder == nil {
				u.ColumnOrder = map[string][]string{}
			}
			u.ColumnOrder[p.View] = p.Order
		})
		f.emit(engine.Event{Type: engine.EventTable, View: p.View})
		return nil, nil
	}
}

// visibleIDs returns the ids of the columns the table shows, left to right.
func visibleIDs(tv *tableView) []string {
	out := make([]string, len(tv.cols))
	for i, ci := range tv.cols {
		out[i] = tv.snap.Columns[ci].ID
	}
	return out
}

// dragHeader drags the header of a visible column to x (from the table's left edge) in two moves
// and, unless hold, drops it there, as the GLFW driver sends a drag: positions from the header,
// the pointer's absolute position, the delta of each move.
func dragHeader(t *testing.T, sh *shell, vis int, x float32, hold bool) *headerCell {
	t.Helper()
	h := headerFor(t, sh, firstDataColumn+vis)
	drv := fyne.CurrentApp().Driver()
	start := drv.AbsolutePositionForObject(h).AddXY(h.Size().Width/2, h.Size().Height/2)
	end := fyne.NewPos(drv.AbsolutePositionForObject(sh.view.table).X+x, start.Y)
	hp := drv.AbsolutePositionForObject(h)
	for _, p := range []fyne.Position{start.AddXY((end.X-start.X)/2, 0), end} {
		h.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: p, Position: p.Subtract(hp)}, Dragged: fyne.NewDelta((end.X-start.X)/2, 0)})
	}
	if !hold {
		h.DragEnd()
	}
	return h
}

// TestDragHeaderReordersColumns: dragging a column's header into another gap sends the view's new
// order and the table follows the snapshot's order; a short drag, a drop on its own place and a drag
// of the checkboxes' header send nothing; the ⋮'s list resets the order.
func TestDragHeaderReordersColumns(t *testing.T) {
	f := connectedFake(t, 20)
	handleColumnOrder(f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	tv := sh.view
	before := visibleIDs(tv)
	if len(before) < 4 {
		t.Fatalf("columns %v", before)
	}
	starts, _ := tv.columnStarts()

	// The third column into the gap before the first.
	dragHeader(t, sh, 2, starts[0]+4, true)
	if !tv.order.active || tv.order.slot != 0 || !tv.order.mark.Visible() || !tv.order.chip.Visible() || tv.order.chipTitle.Text != tv.snap.Columns[tv.cols[2]].Title {
		t.Fatalf("while dragging: active %v slot %d line %v chip %v %q", tv.order.active, tv.order.slot, tv.order.mark.Visible(), tv.order.chip.Visible(), tv.order.chipTitle.Text)
	}
	if x := tv.order.mark.Position().X; x > starts[0] || x < starts[0]-8 {
		t.Errorf("the drop line at %v, want just left of the first column at %v", x, starts[0])
	}
	headerFor(t, sh, firstDataColumn+2).DragEnd()
	q.until(t, "ui.setColumnOrder", func() bool { return len(f.callsOf("ui.setColumnOrder")) == 1 })
	want := append([]string{before[2], before[0], before[1]}, before[3:]...)
	if p := decodeParams[orderParams](t, f.callsOf("ui.setColumnOrder")[0]); p.View != "pods" || !slices.Equal(p.Order, want) {
		t.Fatalf("ui.setColumnOrder %+v, want %v", p, want)
	}
	q.until(t, "the table in the new order", func() bool { return slices.Equal(visibleIDs(tv), want) })
	settle(w)
	if h := headerFor(t, sh, firstDataColumn); h.title != tv.snap.Columns[tv.cols[0]].Title || tv.order.mark.Visible() {
		t.Fatalf("first header %q, line shown %v", h.title, tv.order.mark.Visible())
	}

	// The first column after the last.
	starts, _ = tv.columnStarts()
	last := len(starts) - 1
	dragHeader(t, sh, 0, starts[last]+tv.setWidths[firstDataColumn+last]-2, false)
	q.until(t, "the second order", func() bool { return len(f.callsOf("ui.setColumnOrder")) == 2 })
	if p := decodeParams[orderParams](t, f.callsOf("ui.setColumnOrder")[1]); p.Order[len(p.Order)-1] != want[0] {
		t.Fatalf("moved to the end: %v", p.Order)
	}

	// Nothing for a twitch, its own place, or the checkboxes' header.
	h := headerFor(t, sh, firstDataColumn+1)
	h.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(10, 5)}, Dragged: fyne.NewDelta(3, 0)})
	h.DragEnd()
	starts, _ = tv.columnStarts()
	dragHeader(t, sh, 1, starts[1]+tv.setWidths[firstDataColumn+1]/2-1, false)
	box := headerFor(t, sh, checkColumn)
	box.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(5, 5)}, Dragged: fyne.NewDelta(200, 0)})
	box.DragEnd()
	q.drain()
	if n := len(f.callsOf("ui.setColumnOrder")); n != 2 || tv.order.active {
		t.Fatalf("%d orders sent, drag active %v", n, tv.order.active)
	}

	// The ⋮'s list ends with Reset Column Order while the columns are arranged.
	test.Tap(headerFor(t, sh, tv.menuColumn()))
	var reset *widget.Button
	for _, b := range findAll[*widget.Button](tv.colPopup) {
		if b.Text == "Reset Column Order" {
			reset = b
		}
	}
	if reset == nil {
		t.Fatal("no Reset Column Order in the column list")
	}
	test.Tap(reset)
	q.until(t, "the kind's order", func() bool { return slices.Equal(visibleIDs(tv), before) })
	if p := decodeParams[orderParams](t, f.callsOf("ui.setColumnOrder")[2]); !p.Reset || p.View != "pods" || tv.columnsOpen() {
		t.Fatalf("reset sent %+v, list open %v", p, tv.columnsOpen())
	}
	if f.ui.ColumnOrder["pods"] != nil {
		t.Fatalf("UI state's order %v after the reset", f.ui.ColumnOrder)
	}
}

// renderColumnDrag arranges the pods' columns (Status second), then drags the Age header over the
// Namespace column: the line in the gap, the chip under the pointer.
func renderColumnDrag(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	handleColumnOrder(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	tv := sh.view
	ids := visibleIDs(tv)
	sh.dispatch("ui.setColumnOrder", orderParams{View: "pods", Order: append([]string{"name", "status"}, slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == "name" || id == "status" })...)}, nil)
	q.until(t, "the arranged columns", func() bool { return len(tv.cols) > 2 && tv.snap.Columns[tv.cols[1]].ID == "status" })
	settle(w)
	age := slices.Index(visibleIDs(tv), "age")
	starts, _ := tv.columnStarts()
	dragHeader(t, sh, age, starts[3]+8, true)
	settle(w)
	savePNG(t, w, name)
	headerFor(t, sh, firstDataColumn+age).DragEnd()
}

func TestRenderColumnDragDark(t *testing.T) { renderColumnDrag(t, ThemeDark, "column_drag_dark.png") }
