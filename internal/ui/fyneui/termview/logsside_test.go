package termview

import (
	"math"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// sideLines are a short line, one far wider than the view, and another short one.
var sideLines = []string{"short", "start " + strings.Repeat("0123456789", 300) + " end", "last"}

// rowsAt is a point of the rows' area, from its top left, in the window.
func rowsAt(l *Logs, x, y float32) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(l.rowsBox).Add(fyne.NewPos(x, y))
}

// TestLogsScrollSideways: while the lines do not wrap, a sideways scroll over them moves the rows
// together as far as the widest line reaches, each drawing the characters in view; a double click
// selects the word under the pointer there; a vertical scroll leaves them; wrapped, nothing
// scrolls sideways; the widest line dropped, the rows narrow to the widest kept.
func TestLogsScrollSideways(t *testing.T) {
	l, w := showMerged(t, sideLines, LogView{}, fyne.NewSize(600, 300))
	long := sideLines[1]
	cw := charWidth(l.list.Theme())
	if got, want := l.sideFill.MinSize().Width, float32(len(long)+1)*cw; got != want {
		t.Fatalf("the rows are %v wide, want %v", got, want)
	}

	test.Scroll(w.Canvas(), rowsAt(l, 100, 20), -1e6, 0) // as far right as it goes
	end := l.sideFill.MinSize().Width - l.side.Size().Width
	if l.shift != end || l.side.Offset.X != end {
		t.Fatalf("scrolled to %v (the scroller to %v), want %v", l.shift, l.side.Offset.X, end)
	}
	if rows := rowTexts(l); rows[0] != "" || !strings.HasSuffix(rows[1], "6789 end") || rows[2] != "" {
		t.Errorf("rows scrolled to the widest line's end: %q, %q…, %q", rows[0], rows[1][:min(len(rows[1]), 20)], rows[2])
	}
	n := float32(len(long) - 2) // the "n" of "end"
	l.surface.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(n*cw-l.shift+cw/2, at(l, 1, 0).Y)})
	if got := l.SelectedText(); got != "end" {
		t.Errorf("a double click on the line's last word selected %q", got)
	}
	if r := l.shownRows[1]; r == nil || r.rend == nil {
		t.Fatal("the long row is not drawn")
	} else if x, want := r.rend.sel.Position().X, (n-1)*cw-l.shift; math.Abs(float64(x-want)) > 0.5 {
		t.Errorf("the selection is drawn at %v, want under the word at %v", x, want)
	}

	test.Scroll(w.Canvas(), rowsAt(l, 100, 20), 0, -40)
	if l.shift != end {
		t.Errorf("a vertical scroll moved the rows sideways to %v", l.shift)
	}

	l.SetView(LogView{Wrap: true})
	test.Scroll(w.Canvas(), rowsAt(l, 100, 20), -100, 0)
	if l.shift != 0 || l.side.Offset.X != 0 || l.sideWidth() != 0 {
		t.Errorf("wrapped: scrolled to %v, the scroller at %v of %v", l.shift, l.side.Offset.X, l.sideWidth())
	}

	l.SetView(LogView{})
	l.lines.evict() // "short" and the long line leave the scrollback
	l.lines.evict()
	l.trimRows()
	if l.widest != int32(len("last")) {
		t.Errorf("the widest kept line has %d characters, want %d", l.widest, len("last"))
	}
}

// TestLogsSideBar: the bar along the bottom has its place while a line is wider than the view, the
// surface ending above it, and a drag on it scrolls the rows without selecting; wrapped, the
// surface reaches the bottom again.
func TestLogsSideBar(t *testing.T) {
	l, w := showMerged(t, sideLines, LogView{}, fyne.NewSize(600, 300))
	bar := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameScrollBar)
	h := l.rowsBox.Size().Height
	if got := l.surface.Size().Height; got != h-bar {
		t.Fatalf("the surface is %v high over %v of rows, want the bar's %v left", got, h, bar)
	}
	on := rowsAt(l, 5, h-1)
	test.MoveMouse(w.Canvas(), on) // the bar shows while the pointer is over it
	test.Drag(w.Canvas(), on, 100, 0)
	if l.shift <= 0 || l.sel.has() {
		t.Errorf("a drag on the bar scrolled the rows to %v, selected %v", l.shift, l.sel.has())
	}

	l.SetView(LogView{Wrap: true})
	if got := l.surface.Size().Height; got != h || l.shift != 0 {
		t.Errorf("wrapped: the surface is %v high of %v, the rows scrolled to %v", got, h, l.shift)
	}
}
