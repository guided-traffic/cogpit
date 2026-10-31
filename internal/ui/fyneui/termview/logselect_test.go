package termview

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// at is a point of the surface on column col of list item item, the view scrolled as it is.
func at(l *Logs, item, col int) fyne.Position {
	stride := l.stride()
	return fyne.NewPos(float32(col)*charWidth(l.list.Theme())+1, float32(item)*stride+stride/2-l.list.GetScrollOffset())
}

func press(s *logSurface, p fyne.Position, mod fyne.KeyModifier) {
	s.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: p}, Button: desktop.MouseButtonPrimary, Modifier: mod})
	s.MouseUp(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: p}, Button: desktop.MouseButtonPrimary, Modifier: mod})
}

// drag presses at from and drags to to, without letting go.
func drag(s *logSurface, from, to fyne.Position) {
	s.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: from}, Button: desktop.MouseButtonPrimary})
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
}

// copiedBy collects what the view copies.
func copiedBy(l *Logs) *[]string {
	var got []string
	l.SetOnCopy(func(s string) { got = append(got, s) })
	return &got
}

// TestLogSelectionDragAcrossRows (WF36): a drag selects from where it began to the pointer, across
// rows, and stops following; ⌘C copies the lines as the view shows them, joined by "\n", without a
// line ending after the last; Escape drops the selection, and ⌘C then copies nothing.
func TestLogSelectionDragAcrossRows(t *testing.T) {
	l, w := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Stamped: true}, fyne.NewSize(900, 300))
	got := copiedBy(l)
	rows := rowTexts(l)
	s := l.surface
	drag(s, at(l, 1, 5), at(l, 3, 10))
	s.DragEnd()
	want := rows[1][5:] + "\n" + rows[2] + "\n" + rows[3][:10]
	if sel := l.SelectedText(); sel != want {
		t.Errorf("selected %q, want %q", sel, want)
	}
	if l.Following() {
		t.Error("a drag left the view following")
	}
	if w.Canvas().Focused() != s {
		t.Error("the view did not take the keyboard")
	}
	s.TypedShortcut(&fyne.ShortcutCopy{})
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("copied %q", *got)
	}
	// The rows show their part: the first from its fifth column, the middle one whole with its line
	// break, the last to its tenth column.
	cw := charWidth(l.list.Theme())
	for item, want := range map[int]rowSel{0: {}, 1: {from: 5, to: len(rows[1]), eol: true, cont: true}, 2: {from: 0, to: len(rows[2]), eol: true, cont: true}, 3: {from: 0, to: 10}, 4: {}} {
		r := l.shownRows[item]
		if r == nil {
			t.Fatalf("item %d is not on screen", item)
		}
		if r.sel != want {
			t.Errorf("item %d: %+v, want %+v", item, r.sel, want)
		}
		if item == 1 {
			if x := r.rend.sel.Position().X; x < 5*cw-1 || x > 5*cw+1 {
				t.Errorf("item 1's selection starts at %v, want %v", x, 5*cw)
			}
		}
	}
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if l.SelectedText() != "" || l.shownRows[2].sel.on() {
		t.Error("Escape left the selection")
	}
	s.TypedShortcut(&fyne.ShortcutCopy{})
	if len(*got) != 1 {
		t.Errorf("a copy without a selection copied %q", (*got)[1:])
	}
}

// TestLogSelectionWordAndShiftClick: a double click selects the word under the pointer; ⇧-click
// extends a selection from where it began; a plain click drops it.
func TestLogSelectionWordAndShiftClick(t *testing.T) {
	l, _ := showMerged(t, mergedLogs, LogView{Prefixed: true, Stamped: true}, fyne.NewSize(900, 300))
	rows := rowTexts(l)
	s := l.surface
	// Row 4: "ERROR payment provider unreachable: …"; column 8 is in "payment".
	press(s, at(l, 4, 8), 0)
	s.DoubleTapped(&fyne.PointEvent{Position: at(l, 4, 8)})
	if got := l.SelectedText(); got != "payment" {
		t.Errorf("a double click selected %q", got)
	}
	press(s, at(l, 0, 3), 0)
	press(s, at(l, 1, 4), fyne.KeyModifierShift)
	if got, want := l.SelectedText(), rows[0][3:]+"\n"+rows[1][:4]; got != want {
		t.Errorf("⇧-click selected %q, want %q", got, want)
	}
	press(s, at(l, 2, 2), fyne.KeyModifierShift) // extends from the same start
	if got, want := l.SelectedText(), rows[0][3:]+"\n"+rows[1]+"\n"+rows[2][:2]; got != want {
		t.Errorf("a second ⇧-click selected %q, want %q", got, want)
	}
	press(s, at(l, 2, 2), 0)
	if l.SelectedText() != "" {
		t.Error("a click left the selection")
	}
	if a, b := wordAt("a  bc", 3); a != 3 || b != 5 {
		t.Errorf("wordAt in a word: %d, %d", a, b)
	}
	if a, b := wordAt("ab  c", 2); a != 0 || b != 2 {
		t.Errorf("wordAt right after a word: %d, %d", a, b)
	}
	if a, b := wordAt("ab   c", 3); a != b {
		t.Errorf("wordAt between words: %d, %d", a, b)
	}
	if a, b := wordAt("grüße 集群", len("grüße ")); a != len("grüße ") || b != len("grüße 集群") {
		t.Errorf("wordAt in UTF-8: %d, %d", a, b)
	}
}

// TestLogSelectAllIsWhatTheViewShows: ⌘A selects the lines the view shows, its sources and its
// filter applied, as a download saves them; the context menu's Copy All Lines Shown copies the
// same, its Copy what is selected, and Copy is greyed without a selection.
func TestLogSelectAllIsWhatTheViewShows(t *testing.T) {
	l, w := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Stamped: true, Pod: "web-7d9f-b2", Search: "payment", Filter: true}, fyne.NewSize(900, 300))
	got := copiedBy(l)
	want := strings.Join(l.DownloadLines(), "\n")
	if len(l.DownloadLines()) != 2 {
		t.Fatalf("the view shows %q", l.DownloadLines())
	}
	s := l.surface
	s.TappedSecondary(&fyne.PointEvent{Position: at(l, 0, 1), AbsolutePosition: at(l, 0, 1)})
	items := s.items.Items
	if len(items) != 4 || items[0].Label != "Copy" || !items[0].Disabled || items[1].Label != "Select All" || items[3].Label != "Copy All Lines Shown" {
		t.Fatalf("menu %+v", items)
	}
	items[3].Action()
	s.menu.Hide()
	if len(*got) != 1 || (*got)[0] != want {
		t.Errorf("Copy All Lines Shown copied %q, want %q", *got, want)
	}
	w.Canvas().Focus(s)
	s.TypedShortcut(&fyne.ShortcutSelectAll{})
	if sel := l.SelectedText(); sel != want {
		t.Errorf("⌘A selected %q, want %q", sel, want)
	}
	s.TappedSecondary(&fyne.PointEvent{Position: at(l, 0, 1), AbsolutePosition: at(l, 0, 1)})
	if c := s.items.Items[0]; c.Disabled {
		t.Error("Copy is greyed with a selection")
	} else {
		c.Action()
	}
	if len(*got) != 2 || (*got)[1] != want {
		t.Errorf("the menu's Copy copied %q", (*got)[1:])
	}
	// A search alone hides nothing: ⌘A then selects every line of the pod.
	l.SetView(LogView{Prefixed: true, Names: true, Stamped: true, Pod: "web-7d9f-b2", Search: "payment"})
	l.SelectAll()
	if sel := l.SelectedText(); strings.Count(sel, "\n") != 4 {
		t.Errorf("⌘A without the filter selected %q", sel)
	}
}

// TestLogSelectionKeepsItsLines: a selection is in the lines, not the rows: it stays on its text
// while new lines arrive, while the view wraps at another width and while a filter hides lines in
// it (a copy then takes the lines shown); a view that shows the text differently drops it.
func TestLogSelectionKeepsItsLines(t *testing.T) {
	pr, pw := io.Pipe()
	l, w := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	write := func(lines ...string) {
		if _, err := io.WriteString(pw, strings.Join(lines, "\n")+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha one", "beta two "+strings.Repeat("x", 120), "gamma three", "delta four")
	waitApplied(4)
	s := l.surface
	drag(s, at(l, 0, 6), at(l, 2, 5))
	s.DragEnd()
	want := "one\nbeta two " + strings.Repeat("x", 120) + "\ngamma"
	if got := l.SelectedText(); got != want {
		t.Fatalf("selected %q", got)
	}
	write("epsilon five", "zeta six")
	waitApplied(6)
	if got := l.SelectedText(); got != want {
		t.Errorf("after new lines %q", got)
	}
	l.SetView(LogView{Wrap: true})
	if len(l.rows) <= 6 {
		t.Fatalf("%d rows: the long line did not wrap", len(l.rows))
	}
	if got := l.SelectedText(); got != want {
		t.Errorf("wrapped %q", got)
	}
	w.Resize(fyne.NewSize(400, 300))
	l.Refresh()
	if got := l.SelectedText(); got != want {
		t.Errorf("wrapped narrower %q", got)
	}
	l.SetView(LogView{Wrap: true, Search: "a", Filter: true}) // "beta two" has an "a" too; "epsilon" none
	if got := l.SelectedText(); got != want {
		t.Errorf("filtered %q", got)
	}
	l.SetView(LogView{Wrap: true, Search: "m", Filter: true}) // only "gamma three"
	if got := l.SelectedText(); got != "gamma" {
		t.Errorf("filtered to one line %q", got)
	}
	l.SetView(LogView{Timestamps: true})
	if l.SelectedText() != "" {
		t.Error("a view showing timestamps kept offsets into the text without them")
	}
	_ = pw.Close()
}

// TestLogSelectionDroppedWithItsLines: lines the scrollback drops take their part of a selection
// with them; a selection wholly in them goes.
func TestLogSelectionDroppedWithItsLines(t *testing.T) {
	pr, pw := io.Pipe()
	l, _ := showLogs(t, pr, fyne.NewSize(600, 300))
	waitApplied := appliedWaiter(t, l)
	write := func(from, to int) {
		var b strings.Builder
		for i := from; i < to; i++ {
			fmt.Fprintf(&b, "line %d\n", i)
		}
		if _, err := io.WriteString(pw, b.String()); err != nil {
			t.Fatal(err)
		}
	}
	write(0, 20)
	waitApplied(20)
	l.setSelection(logPos{line: 2, off: 2}, logPos{line: 12, off: 4})
	write(20, logScrollback+5) // lines 0–4 dropped
	waitApplied(logScrollback + 5)
	if got := l.SelectedText(); !strings.HasPrefix(got, "line 5\n") || !strings.HasSuffix(got, "\nline") || strings.Count(got, "\n") != 7 {
		t.Errorf("after the drop %q", got)
	}
	write(logScrollback+5, logScrollback+20) // lines up to 19 dropped
	waitApplied(logScrollback + 20)
	if l.sel.has() || l.SelectedText() != "" {
		t.Errorf("a selection in dropped lines stays: %+v", l.sel)
	}
	_ = pw.Close()
}

// TestLogSelectionAutoScroll: a drag beyond the view's bottom scrolls it, the farther the faster,
// and the selection follows to the pointer; back inside, the scrolling stops.
func TestLogSelectionAutoScroll(t *testing.T) {
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i)
	}
	l, _ := showMerged(t, lines, LogView{}, fyne.NewSize(600, 300))
	l.SetFollow(false)
	l.list.ScrollToTop()
	s := l.surface
	s.every = 0 // the test steps the scrolling
	h := s.Size().Height
	drag(s, at(l, 2, 0), fyne.NewPos(10, h+3*l.stride()))
	before := l.list.GetScrollOffset()
	s.autoScroll()
	s.autoScroll()
	after := l.list.GetScrollOffset()
	if after < before+6*l.stride() {
		t.Errorf("the view scrolled from %v to %v, want at least 6 rows", before, after)
	}
	if got := l.SelectedText(); !strings.HasPrefix(got, "line 002\n") || strings.Count(got, "\n") < 10 {
		t.Errorf("selected %q", got)
	}
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(10, h/2)}})
	s.autoScroll()
	if l.list.GetScrollOffset() != after {
		t.Error("the view scrolled with the pointer inside")
	}
	s.DragEnd()
	if s.autoStop != nil {
		t.Error("the scrolling runs after the drag")
	}
}

// TestLogSelectionHeadRowActs: a click on the row above the lines runs its action and selects
// nothing; a drag from it selects from the first line.
func TestLogSelectionHeadRowActs(t *testing.T) {
	l, _ := showMerged(t, mergedLogs[:3], LogView{}, fyne.NewSize(600, 300))
	acted := 0
	l.SetHead("Load older lines", HeadAction, func() { acted++ })
	s := l.surface
	head := at(l, 0, 3)
	press(s, head, 0)
	s.Tapped(&fyne.PointEvent{Position: head})
	if acted != 1 || l.sel.has() {
		t.Errorf("acted %d, selection %+v", acted, l.sel)
	}
	drag(s, head, at(l, 1, 4))
	if got := l.SelectedText(); got != mergedLogs[0][:4] {
		t.Errorf("a drag from the head selected %q", got)
	}
}

// TestLogSelectionLongLine: a line wider than the view is copied whole; its row draws the
// characters in view (logsside.go), selected up to the line's end; the pointer past them is
// their end.
func TestLogSelectionLongLine(t *testing.T) {
	long := "start " + strings.Repeat("0123456789", 400) + " end"
	l, _ := showMerged(t, []string{"first", long, "last"}, LogView{}, fyne.NewSize(600, 300))
	l.SelectAll()
	if got := l.SelectedText(); got != "first\n"+long+"\nlast" {
		t.Errorf("selected %d bytes, want the whole long line", len(got))
	}
	if _, _, from, to := l.rowSpan(1); from != 0 || to != 2*l.cols+2 {
		t.Errorf("the long row draws [%d, %d) at %d columns, want twice the view's", from, to, l.cols)
	}
	if r, want := l.shownRows[1], (rowSel{to: len(long), eol: true, cont: true}); r == nil || r.sel != want {
		t.Errorf("the long row's selection %+v, want %+v", r.sel, want)
	}
	if off := l.offsetIn(1, 1e6); off != 2*l.cols+2 {
		t.Errorf("past the row's characters: offset %d, want %d", off, 2*l.cols+2)
	}
}

// TestLogSelectionKeysGoOn: a focused view keeps ⌘C, ⌘A, Escape with a selection and "/"; other
// keys go on to the window's. (Other shortcuts go on to the window's shortcuts too, through the
// desktop canvas's TypedShortcut, which the test driver's canvas has not: not checked here.)
func TestLogSelectionKeysGoOn(t *testing.T) {
	l, w := showMerged(t, mergedLogs[:3], LogView{}, fyne.NewSize(600, 300))
	var keys []fyne.KeyName
	var runes []rune
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) { keys = append(keys, ev.Name) })
	w.Canvas().SetOnTypedRune(func(r rune) { runes = append(runes, r) })
	slashes := 0
	l.SetOnSlash(func() { slashes++ })
	s := l.surface
	w.Canvas().Focus(s)
	test.Type(s, "a/")
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyF1})
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	s.TypedShortcut(&fyne.ShortcutCut{}) // goes on; nothing to cut here
	if slashes != 1 || len(runes) != 1 || runes[0] != 'a' || len(keys) != 2 || keys[0] != fyne.KeyF1 || keys[1] != fyne.KeyEscape {
		t.Errorf("slashes %d, runes %q, keys %v", slashes, runes, keys)
	}
	l.SelectAll()
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if len(keys) != 2 || l.sel.has() {
		t.Errorf("Escape with a selection: keys %v, selection %+v", keys, l.sel)
	}
}

func TestRenderLogsSelection(t *testing.T) {
	l, w := showMerged(t, mergedLogs, LogView{Prefixed: true, Names: true, Timestamps: true, Search: "error"}, fyne.NewSize(900, 260))
	l.Refresh()
	drag(l.surface, at(l, 3, 16), at(l, 5, 40))
	l.surface.DragEnd()
	writePNG(t, w, "logs_selection.png")
}
