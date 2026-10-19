package fyneui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// pressAt and dragTo are a primary press, and a drag from it, on a selectable text.
func pressAt(s desktop.Mouseable, p fyne.Position) {
	s.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: p}, Button: desktop.MouseButtonPrimary})
}

func dragTo(s fyne.Draggable, from, to fyne.Position) {
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
}

// errorToast is a long error, wrapped over several lines.
const errorToast = "pods.delete: pods \"checkout-api-7d9f8b6c5-x2k4p\" is forbidden: User cannot delete resource \"pods\" in namespace \"shop\""

// TestToastCopyIcon (WF41): the icon at the end of a toast's title line copies the whole message
// as it came, turns into a checkmark for a moment, and raises no toast and no notification of its
// own.
func TestToastCopyIcon(t *testing.T) {
	sh, _, q, _ := notesShell(t, ThemeDark)
	sh.copyConfirm = 30 * time.Millisecond
	msg := "first line\nsecond  line, two spaces"
	sh.toast("error", msg)
	q.until(t, "one unread", func() bool { return sh.ui.Notifications.Unread == 1 })
	card := toastCards(sh)[0]
	glyphs := findAll[*copyGlyph](card)
	if len(glyphs) != 1 || !glyphs[0].Visible() {
		t.Fatalf("%d copy icons on the toast, want one shown", len(glyphs))
	}
	test.Tap(glyphs[0])
	if got := sh.app.Clipboard().Content(); got != msg {
		t.Errorf("the clipboard holds %q, want %q", got, msg)
	}
	if !glyphs[0].done {
		t.Error("no checkmark after the copy")
	}
	q.until(t, "the copy icon back", func() bool { return !glyphs[0].done })
	q.drain()
	if n := len(toastCards(sh)); n != 1 || sh.ui.Notifications.Unread != 1 || sh.ui.Notifications.Total != 1 {
		t.Errorf("%d toasts, %d notifications after the copy", n, sh.ui.Notifications.Total)
	}
}

// TestToastTextSelectable (WF41): a toast's text selects by a drag across its lines, ⌘C copies the
// selection; while it is selected the toast stays though its time ran out, Escape drops the
// selection and the toast goes after its time.
func TestToastTextSelectable(t *testing.T) {
	sh, w, q := newTestShell(t, connectedFake(t, 3), Options{Theme: ThemeDark})
	sh.toastTime = 40 * time.Millisecond
	sh.toast("error", errorToast)
	settle(w)
	card := toastCards(sh)[0]
	text := card.text
	if text == nil || len(text.lines) < 2 {
		t.Fatalf("the toast's text is not a selectable paragraph over lines: %+v", text)
	}
	if texts := findAll[*paragraph](card); len(texts) != 1 || texts[0].text != errorToast {
		t.Errorf("the toast's paragraph %+v", texts)
	}
	_, lineH := text.metrics()
	from, to := fyne.NewPos(1, lineH/2), fyne.NewPos(text.Size().Width, lineH*1.5)
	card.MouseIn(mouse())
	pressAt(text, from)
	dragTo(text, from, to)
	card.MouseOut() // the drag left the toast
	text.DragEnd()
	sel := text.SelectedText()
	if !strings.HasPrefix(sel, "pods.delete: pods") || !strings.Contains(sel, text.lines[1].text) || len(sel) < len(text.lines[0].text)+2 {
		t.Errorf("selected %q over the lines %+v", sel, text.lines)
	}
	if w.Canvas().Focused() != text {
		t.Error("the text did not take the keyboard")
	}
	text.TypedShortcut(&fyne.ShortcutCopy{})
	if got := sh.app.Clipboard().Content(); got != sel {
		t.Errorf("⌘C copied %q", got)
	}
	time.Sleep(150 * time.Millisecond)
	q.drain()
	if len(toastCards(sh)) != 1 {
		t.Fatal("the toast went while its text was selected")
	}
	text.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if text.SelectedText() != "" {
		t.Error("Escape left the selection")
	}
	q.until(t, "the toast to expire", func() bool { return len(toastCards(sh)) == 0 })
	if w.Canvas().Focused() != nil {
		t.Error("the gone toast's text keeps the keyboard")
	}
}

// TestToastHoverOnItsIcons: the pointer on a toast's copy icon or on its × holds the toast as the
// pointer on the toast does; leaving them lets its time run.
func TestToastHoverOnItsIcons(t *testing.T) {
	sh, _, q := newTestShell(t, connectedFake(t, 3), Options{Theme: ThemeDark})
	sh.toastTime = 40 * time.Millisecond
	sh.toast("info", "Port forward started: http://127.0.0.1:8080")
	card := toastCards(sh)[0]
	glyph := findAll[*copyGlyph](card)[0]
	closeBtn := findAll[*toastButton](card)[0]
	// The driver's order: MouseOut of what the pointer leaves, then MouseIn of what it enters.
	card.MouseIn(mouse())
	card.MouseOut()
	glyph.MouseIn(mouse())
	time.Sleep(120 * time.Millisecond)
	q.drain()
	glyph.MouseOut()
	closeBtn.MouseIn(mouse())
	time.Sleep(120 * time.Millisecond)
	q.drain()
	if len(toastCards(sh)) != 1 {
		t.Fatal("the toast went while the pointer was on its icons")
	}
	closeBtn.MouseOut()
	q.until(t, "the toast to expire", func() bool { return len(toastCards(sh)) == 0 })
}

// TestNotificationRowCopy (WF41): a notification in the bell's panel shows its copy icon while the
// pointer is on its row, without moving the time; the icon copies the whole message; its text is
// selectable.
func TestNotificationRowCopy(t *testing.T) {
	sh, w, q, s := notesShell(t, ThemeDark)
	msg := "Node worker-2: the eviction of pod checkout-api-7d9f8b6c5-x2k4p waits: Cannot evict pod as it would violate the pod's disruption budget."
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning, Message: msg})
	q.until(t, "one unread", func() bool { return sh.ui.Notifications.Unread == 1 })
	test.Tap(sh.top.notes.bell)
	q.until(t, "the list", func() bool { return len(sh.top.notes.list) == 1 })
	settle(w)
	rows := findAll[*noteRow](sh.top.notes.panel)
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	row := rows[0]
	times := findAll[*textLine](row)
	var clock *textLine
	for _, l := range times {
		if l.text == "14:31:00" {
			clock = l
		}
	}
	if clock == nil {
		t.Fatal("no time in the row")
	}
	before := clock.Position()
	if row.glyph.Visible() {
		t.Error("the copy icon shows without the pointer")
	}
	row.MouseIn(mouse())
	settle(w)
	if !row.glyph.Visible() || clock.Position() != before {
		t.Errorf("hovered: icon shown %v, the time moved from %v to %v", row.glyph.Visible(), before, clock.Position())
	}
	row.MouseOut()
	row.glyph.MouseIn(mouse()) // from the row onto the icon
	if !row.glyph.Visible() {
		t.Error("the icon hid under the pointer")
	}
	test.Tap(row.glyph)
	if got := sh.app.Clipboard().Content(); got != msg {
		t.Errorf("the clipboard holds %q", got)
	}
	texts := findAll[*selParagraph](row)
	if len(texts) != 1 || texts[0].p.text != msg {
		t.Fatalf("the row's text %+v", texts)
	}
	texts[0].DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(2, 2)})
	if got := texts[0].SelectedText(); got != "Node" {
		t.Errorf("a double click selected %q", got)
	}
}

// TestSelParagraphShownText: the selection's text is the paragraph as shown: wrapped lines joined
// by a space, the text's line breaks kept.
func TestSelParagraphShownText(t *testing.T) {
	test.NewTempApp(t)
	size := theme.TextSize()
	words := strings.Repeat("word ", 30)
	p := newParagraph("alpha beta\n"+words+"\nomega", theme.ColorNameForeground, fyne.MeasureText("word word word word", size, fyne.TextStyle{}).Width+1)
	s := newSelParagraph(p, theme.ColorNameSelection, nil)
	if len(s.lines) < 8 {
		t.Fatalf("lines %+v", s.lines)
	}
	if want := "alpha beta\n" + strings.TrimSpace(words) + "\nomega"; s.shown != want {
		t.Errorf("shown %q, want %q", s.shown, want)
	}
	for _, l := range s.lines {
		if got := string([]rune(s.shown)[l.start : l.start+len([]rune(l.text))]); got != l.text {
			t.Errorf("line %q starts at %d, which reads %q", l.text, l.start, got)
		}
	}
}

// logSurfaceOf is the layer of a log view that selects its text.
func logSurfaceOf(t *testing.T, root fyne.CanvasObject) interface {
	desktop.Mouseable
	fyne.Draggable
	fyne.Shortcutable
} {
	t.Helper()
	for _, o := range findAll[interface {
		desktop.Mouseable
		fyne.Draggable
		fyne.Shortcutable
	}](root) {
		if fmt.Sprintf("%T", o) == "*termview.logSurface" {
			return o
		}
	}
	t.Fatal("no selecting surface in the log view")
	return nil
}

// logRowPos is a point of a log view's surface on column col of list item row (the head row, when
// there is one, is item 0), the view not scrolled.
func logRowPos(row, col int) fyne.Position {
	th := fyne.CurrentApp().Settings().Theme()
	m := fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true})
	stride := m.Height + th.Size(theme.SizeNamePadding)
	return fyne.NewPos(float32(col)*m.Width+1, float32(row)*stride+stride/2)
}

// TestLogTabCopiesThroughTheShell (WF36): a selection's ⌘C in a logs tab goes through the shell's
// copy, as a plain copy: on the clipboard, no toast.
func TestLogTabCopiesThroughTheShell(t *testing.T) {
	sh, w, q, _ := logsScene(t, ThemeDark, false, engine.LogOptions{})
	settle(w)
	lt := logTabOf(sh)
	s := logSurfaceOf(t, lt.view)
	lines := lt.view.ShownLines()
	first := 0 // the first line's item: after the head row, when there is one
	if lt.view.Head() != "" {
		first = 1
	}
	from, to := logRowPos(first, 2), logRowPos(first+1, 5)
	pressAt(s, from)
	dragTo(s, from, to)
	s.DragEnd()
	want := lines[0][2:] + "\n" + lines[1][:5]
	if got := lt.view.SelectedText(); got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
	toasts := len(sh.toasts.Objects)
	s.TypedShortcut(&fyne.ShortcutCopy{})
	q.drain()
	if got := sh.app.Clipboard().Content(); got != want {
		t.Errorf("the clipboard holds %q", got)
	}
	if len(sh.toasts.Objects) != toasts {
		t.Error("a ⌘C raised a toast")
	}
}

// TestRenderLogsSelection: a deployment's logs tab with a selection across three rows, dark and
// light.
func TestRenderLogsSelection(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		sh, w, q, _ := logsScene(t, mode, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "error"})
		lt := logTabOf(sh)
		q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
		settle(w)
		s := logSurfaceOf(t, lt.view)
		first := 0
		if lt.view.Head() != "" {
			first = 1
		}
		from, to := logRowPos(first+2, 30), logRowPos(first+4, 52)
		pressAt(s, from)
		dragTo(s, from, to)
		s.DragEnd()
		savePNG(t, w, "logs_selection_"+string(mode)+".png")
	}
}
