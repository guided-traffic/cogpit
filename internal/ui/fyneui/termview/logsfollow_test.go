package termview

import (
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// followLogs shows a followed log of 200 lines in a 600×300 window, and returns a function that
// writes lines from to to and waits until they are applied.
func followLogs(t *testing.T) (*Logs, fyne.Window, func(from, to int)) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	l, w := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	write := func(from, to int) {
		t.Helper()
		var b strings.Builder
		for i := from; i < to; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		if _, err := io.WriteString(pw, b.String()); err != nil {
			t.Fatal(err)
		}
		waitApplied(to)
	}
	write(0, 200)
	if !l.Following() || belowEnd(l) > 1 {
		t.Fatalf("the followed log is %v above its newest line, following %v", belowEnd(l), l.Following())
	}
	return l, w, write
}

// belowEnd is how far the view is above the newest line: the height of the rows under it.
func belowEnd(l *Logs) float32 {
	rows := float32(len(l.rows) + l.headRows())
	content := rows*l.stride() - l.list.Theme().Size(theme.SizeNamePadding)
	return max(content-l.list.Size().Height-l.list.GetScrollOffset(), 0)
}

// wheel scrolls the log as the mouse wheel or the trackpad does over its lines: dy > 0 up.
func wheel(w fyne.Window, l *Logs, dy float32) {
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(l.list)
	test.Scroll(w.Canvas(), at.Add(fyne.NewPos(100, l.list.Size().Height/2)), 0, dy)
}

// scrollBarOf returns the drag handle of the log's vertical scroll bar.
func scrollBarOf(t *testing.T, l *Logs) fyne.Draggable {
	t.Helper()
	var find func(o fyne.CanvasObject) fyne.Draggable
	find = func(o fyne.CanvasObject) fyne.Draggable {
		if d, ok := o.(fyne.Draggable); ok && o != l.surface {
			return d
		}
		var kids []fyne.CanvasObject
		switch v := o.(type) {
		case fyne.Widget:
			kids = test.WidgetRenderer(v).Objects()
		case *fyne.Container:
			kids = v.Objects
		}
		for _, k := range kids {
			if d := find(k); d != nil {
				return d
			}
		}
		return nil
	}
	for _, o := range test.WidgetRenderer(l.list).Objects() {
		if d := find(o); d != nil {
			return d
		}
	}
	t.Fatal("no scroll bar in the log's list")
	return nil
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) <= 1 }

// TestLogsFollowOffByScrollingUp (the owner's AW14): "wenn der Nutzer 5-6 Zeilen nach oben
// scrollt, wird das Follow-Flag entfernt und wenn er wieder runter scrollt, bis zur aktuellen Zeile
// im Log, wird Follow wieder aktiviert". Two rows up keep following, two rows behind the newest
// line; five rows up in all switch Follow off and the view stands while lines come; the wheel down
// to the newest line switches it on again and the view follows.
func TestLogsFollowOffByScrollingUp(t *testing.T) {
	l, w, write := followLogs(t)
	stride := l.stride()

	wheel(w, l, 2*stride)
	if !l.Following() || !l.followCheck.Checked {
		t.Fatal("two rows up switched Follow off")
	}
	write(200, 210)
	if !l.Following() || !near(belowEnd(l), 2*stride) {
		t.Fatalf("following two rows up: %v above the newest line (want %v), following %v", belowEnd(l), 2*stride, l.Following())
	}

	wheel(w, l, 3*stride) // five rows up in all
	if l.Following() || l.followCheck.Checked {
		t.Fatal("five rows up left Follow on")
	}
	off := l.list.GetScrollOffset()
	write(210, 300)
	if l.Following() || l.list.GetScrollOffset() != off {
		t.Fatalf("new lines while not following: following %v, offset %v → %v", l.Following(), off, l.list.GetScrollOffset())
	}

	wheel(w, l, -stride) // down a row: not at the newest line yet
	if l.Following() {
		t.Fatal("a row down far from the newest line switched Follow on")
	}
	wheel(w, l, -1e6) // down to the newest line
	if !l.Following() || !l.followCheck.Checked || belowEnd(l) > 1 {
		t.Fatalf("at the newest line: following %v, %v above it", l.Following(), belowEnd(l))
	}
	write(300, 320)
	if !l.Following() || belowEnd(l) > 1 {
		t.Fatalf("following again: %v above the newest line", belowEnd(l))
	}
}

// TestLogsFollowByTheScrollBar: dragging the scroll bar up switches Follow off as the wheel does,
// dragging it to the end switches it on.
func TestLogsFollowByTheScrollBar(t *testing.T) {
	l, _, write := followLogs(t)
	bar := scrollBarOf(t, l)
	bar.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -60)})
	bar.DragEnd()
	if l.Following() || l.followCheck.Checked {
		t.Fatalf("the scroll bar dragged up %v left Follow on", belowEnd(l))
	}
	bar.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, 1e4)})
	bar.DragEnd()
	if !l.Following() || belowEnd(l) > 1 {
		t.Fatalf("the scroll bar at the end: following %v, %v above the newest line", l.Following(), belowEnd(l))
	}
	write(200, 220)
	if belowEnd(l) > 1 {
		t.Fatalf("following after the bar: %v above the newest line", belowEnd(l))
	}
}

// TestLogsFollowCheckboxAsBefore: the checkbox keeps its behaviour (the owner: "wie heute"):
// unticked at the newest line the log stands there; the stream's lines, the lines the scrollback
// drops and a resize never switch Follow on again; ticked anywhere, the view goes to the newest
// line and follows.
func TestLogsFollowCheckboxAsBefore(t *testing.T) {
	l, w, write := followLogs(t)
	l.followCheck.SetChecked(false)
	w.Resize(fyne.NewSize(600, 420)) // taller at the newest line: the offset is clamped, no scroll of the user's
	w.Resize(fyne.NewSize(600, 300))
	if l.Following() || l.followCheck.Checked {
		t.Fatal("a resize at the newest line switched Follow on")
	}
	off := l.list.GetScrollOffset()
	write(200, 230)
	if l.Following() || l.list.GetScrollOffset() != off {
		t.Fatalf("unticked: following %v, offset %v → %v", l.Following(), off, l.list.GetScrollOffset())
	}
	for from := 230; from < logScrollback+300; from += 2000 { // the scrollback drops lines at the front: the view moves up with them
		write(from, min(from+2000, logScrollback+300))
	}
	if l.Following() || l.followCheck.Checked {
		t.Fatal("the stream switched Follow on")
	}
	l.followCheck.SetChecked(true)
	if !l.Following() || belowEnd(l) > 1 {
		t.Fatalf("ticked: %v above the newest line", belowEnd(l))
	}
}
