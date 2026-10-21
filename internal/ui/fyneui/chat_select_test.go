package fyneui

import (
	"net/url"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// answerFlow returns the selectable flow of the demo answer whose text holds want.
func answerFlow(t *testing.T, sh *shell, want string) *mdFlow {
	t.Helper()
	a := sh.chat.views[4].(*chatAnswerView)
	for _, o := range a.text.objs {
		if f, ok := o.(*mdFlow); ok && strings.Contains(f.shownText(), want) {
			if f.sel == nil {
				t.Fatalf("the answer's paragraph %q is not selectable", want)
			}
			return f
		}
	}
	t.Fatalf("no paragraph of the answer holds %q", want)
	return nil
}

// wordAt is a point in the middle of the first piece of f that holds word, inside the word.
func wordAt(t *testing.T, f *mdFlow, word string) fyne.Position {
	t.Helper()
	for _, p := range f.pieces {
		if i := strings.Index(p.text, word); i >= 0 {
			x := pieceTextX(p) + fyne.MeasureText(p.text[:i]+word[:len(word)/2], p.size, p.style).Width
			return fyne.NewPos(x, p.y+p.h/2)
		}
	}
	t.Fatalf("no piece holds %q", word)
	return fyne.Position{}
}

// TestChatTextSelectsBlockByBlock: an answer's paragraph is selectable as the drawer's texts are: a
// double click selects a word and takes the focus, the copy shortcut puts the selection on the
// clipboard, Escape drops the selection, and a drag selects from where it began to the pointer; a
// click on a link still opens it, with the hand over it and the text cursor elsewhere.
func TestChatTextSelectsBlockByBlock(t *testing.T) {
	sh, w, q, _, _ := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	f := answerFlow(t, sh, "readiness probe fails")
	sel := f.sel

	sel.DoubleTapped(&fyne.PointEvent{Position: wordAt(t, f, "readiness")})
	q.drain()
	if got := sel.SelectedText(); got != "readiness" {
		t.Fatalf("a double click on a word selects %q", got)
	}
	if w.Canvas().Focused() != sel {
		t.Error("the selected paragraph does not hold the focus, so the copy shortcut would not reach it")
	}
	sel.TypedShortcut(&fyne.ShortcutCopy{})
	if got := sh.app.Clipboard().Content(); got != "readiness" {
		t.Errorf("the copy shortcut put %q on the clipboard", got)
	}

	sel.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if got := sel.SelectedText(); got != "" {
		t.Errorf("Escape left %q selected", got)
	}

	from, to := wordAt(t, f, "readiness"), wordAt(t, f, "fails")
	sel.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
	sel.DragEnd()
	if got := sel.SelectedText(); !strings.HasPrefix(got, "iness probe fa") && !strings.HasPrefix(got, "ness probe fa") {
		t.Errorf("a drag from inside \"readiness\" to inside \"fails\" selects %q", got)
	}

	link := answerFlow(t, sh, "container probes")
	var opened *url.URL
	link.onLink = func(u *url.URL) { opened = u }
	at := wordAt(t, link, "probes")
	for _, p := range link.pieces {
		if p.link != nil {
			at = fyne.NewPos(p.x+p.w/2, p.y+p.h/2)
		}
	}
	link.sel.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: at}})
	if link.sel.Cursor() != desktop.PointerCursor {
		t.Error("no hand over the link")
	}
	link.sel.Tapped(&fyne.PointEvent{Position: at})
	if opened == nil {
		t.Error("a click on the link no longer opens it")
	}
	link.sel.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(1, 1)}})
	if link.sel.Cursor() != desktop.TextCursor {
		t.Error("no text cursor beside the link")
	}
}

// TestChatToolResultSelects: an opened tool row's arguments and result are selectable code blocks.
func TestChatToolResultSelects(t *testing.T) {
	sh, _, q, _, _ := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	tool := sh.chat.views[1].(*chatToolView)
	test.Tap(tool.header)
	q.drain()
	var args *mdCode
	for _, o := range tool.body.objs {
		if c, ok := o.(*mdCode); ok {
			args = c
			break
		}
	}
	if args == nil || args.sel == nil {
		t.Fatal("the opened row's arguments are not a selectable code block")
	}
	i := -1
	for n, l := range args.lines {
		if strings.Contains(l, `"namespace"`) {
			i = n
		}
	}
	if i < 0 {
		t.Fatalf("no line with the namespace: %q", args.lines)
	}
	col := strings.Index(args.lines[i], `"namespace"`) + 3
	x := args.pad() + fyne.MeasureText(args.lines[i][:col], args.textSize(), fyne.TextStyle{Monospace: true}).Width
	args.sel.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(x, args.pad()+(float32(i)+0.5)*args.lineHeight())})
	if got := args.sel.SelectedText(); got != `"namespace":` {
		t.Errorf("a double click in the arguments selects %q", got)
	}
	args.sel.TypedShortcut(&fyne.ShortcutCopy{})
	if got := sh.app.Clipboard().Content(); got != `"namespace":` {
		t.Errorf("the copy shortcut put %q on the clipboard", got)
	}
}

// TestChatSelectedTextIsTheText: what a selection copies is the block's text, not its drawn lines: a
// wrapped paragraph runs on, a line break of the text stays (a blank line too), a code line the
// width broke runs on without a space and prose with one.
func TestChatSelectedTextIsTheText(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	size := theme.TextSize()
	words := "alpha beta gamma delta epsilon zeta"
	f := newMDFlow([]mdRun{{text: words}}, theme.ColorNameForeground, nil)
	f.setWidth(fyne.MeasureText("alpha beta ", size, fyne.TextStyle{}).Width + 2)
	f.relayout()
	if f.pieces[len(f.pieces)-1].line == 0 {
		t.Fatal("the paragraph did not wrap")
	}
	if got, _ := f.selText(); got != words {
		t.Errorf("a wrapped paragraph: %q, want %q", got, words)
	}

	f = newMDFlow([]mdRun{{text: "first\n\nthird "}, {text: "code", code: true}, {text: " end"}}, theme.ColorNameForeground, nil)
	f.setWidth(400)
	if got, _ := f.selText(); got != "first\n\nthird code end" {
		t.Errorf("line breaks and a chip: %q", got)
	}

	c := newMDCode("abcdefghijklmnopqrstuvwxyz\nnext")
	c.setWidth(fyne.MeasureText("abcdefghij", size-1, fyne.TextStyle{Monospace: true}).Width + 2*c.pad() + 1)
	c.relayout()
	if len(c.lines) < 3 {
		t.Fatalf("the code line did not break: %q", c.lines)
	}
	if got, _ := c.selText(); got != "abcdefghijklmnopqrstuvwxyz\nnext" {
		t.Errorf("a broken code line: %q", got)
	}
	c = newMDCode("connection refused by the server")
	c.words = true
	c.setWidth(fyne.MeasureText("connection refused", size-1, fyne.TextStyle{Monospace: true}).Width + 2*c.pad() + 1)
	c.relayout()
	if got, _ := c.selText(); got != "connection refused by the server" {
		t.Errorf("wrapped prose in a code block: %q (lines %q)", got, c.lines)
	}
}

// The chat with a word of the answer and part of a tool's arguments selected.
func TestRenderChatSelectionDark(t *testing.T)  { renderChatSelection(t, ThemeDark) }
func TestRenderChatSelectionLight(t *testing.T) { renderChatSelection(t, ThemeLight) }

func renderChatSelection(t *testing.T, mode ThemeMode) {
	sh, w, q, _, _ := chatScene(t, mode, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	f := answerFlow(t, sh, "readiness probe fails")
	from, to := wordAt(t, f, "readiness"), wordAt(t, f, "fails")
	f.sel.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
	f.sel.DragEnd()
	q.drain()
	c := sh.chat
	top := fyne.CurrentApp().Driver().AbsolutePositionForObject(f).Y - fyne.CurrentApp().Driver().AbsolutePositionForObject(c.scroll.Content).Y
	c.scroll.ScrollToOffset(fyne.NewPos(0, max(top-120, 0))) // the selected paragraph in view
	q.drain()
	savePNG(t, w, "chat_selection_"+string(mode)+".png")
}
