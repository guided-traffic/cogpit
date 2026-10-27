package terminal

import (
	"bytes"
	"image/color"
	"math/rand"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
)

// newScrollbackTerm returns a 10x5 terminal that showed the lines 1 to 9: 1 to 4 are scrollback,
// 5 to 9 the screen, and the cursor is after the 9.
func newScrollbackTerm(t *testing.T) *Terminal {
	term := New()
	term.config.Columns = 10
	term.config.Rows = 5
	term.scrollBottom = 4
	term.Refresh() // ensure visuals set up
	term.handleOutput([]byte("1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n7\r\n8\r\n9"))
	assert.Equal(t, "5\n6\n7\n8\n9", term.content.Text())
	return term
}

// newScrollbackWindow returns a terminal of cols by rows in a test window.
func newScrollbackWindow(t *testing.T, cols, rows float32) *Terminal {
	test.NewTempApp(t)
	term := New()
	w := test.NewTempWindow(t, term)
	w.SetPadded(false)
	cell := term.guessCellSize()
	w.Resize(fyne.NewSize(cell.Width*(cols+0.5), cell.Height*(rows+0.5)))
	assert.Equal(t, uint(cols), term.config.Columns)
	assert.Equal(t, uint(rows), term.config.Rows)
	return term
}

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		b.WriteString("L" + strconv.Itoa(i) + "\r\n")
	}
	return b.String()
}

func allLines(term *Terminal) []string {
	first, end := term.Lines()
	return term.LineTexts(first, end)
}

func TestScrollback_KeepsLines(t *testing.T) {
	term := newScrollbackTerm(t)
	first, end := term.Lines()
	assert.Equal(t, int64(1), first)
	assert.Equal(t, int64(10), end)
	assert.Equal(t, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, allLines(term))
	assert.Equal(t, []string{"3", "4"}, term.LineTexts(3, 5))
}

func TestScrollback_SequencesChangeTheScreenOnly(t *testing.T) {
	tests := map[string]struct {
		input, screen string
	}{
		"erase characters":  {input: "\x1b[3;1H\x1b[X", screen: "5\n6\n \n8\n9"},
		"delete lines":      {input: "\x1b[2;1H\x1b[M", screen: "5\n7\n8\n9\n"},
		"scroll down":       {input: "\x1b[T", screen: "\n5\n6\n7\n8"},
		"insert lines":      {input: "\x1b[2;1H\x1b[L", screen: "5\n\n6\n7\n8"},
		"delete characters": {input: "\x1b[1;1H\x1b[P", screen: "\n6\n7\n8\n9"},
		"insert characters": {input: "\x1b[1;1H\x1b[@", screen: " 5\n6\n7\n8\n9"},
		"erase in line":     {input: "\x1b[2;1H\x1b[K", screen: "5\n\n7\n8\n9"},
		"reverse index":     {input: "\x1b[1;1H\x1bM", screen: "\n5\n6\n7\n8"},
		"erase in display":  {input: "\x1b[2J", screen: "\n\n\n\n"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			term := newScrollbackTerm(t)
			term.handleOutput([]byte(test.input))
			assert.Equal(t, test.screen, term.content.Text())
			assert.Equal(t, []string{"1", "2", "3", "4"}, term.LineTexts(1, 5))
		})
	}
}

func TestScrollback_ScrollUpSequence(t *testing.T) {
	term := newScrollbackTerm(t)
	term.handleOutput([]byte("\x1b[2S"))
	assert.Equal(t, []string{"1", "2", "3", "4", "5", "6"}, term.LineTexts(1, 7))
}

func TestScrollback_WrappedLineIsOneLine(t *testing.T) {
	term := newScrollbackTerm(t)
	term.handleOutput([]byte("\r\nabcdefghijklmno\r\n\r\n\r\n\r\n\r\n"))
	assert.Contains(t, allLines(term), "abcdefghijklmno")

	_, end := term.Lines()
	term.handleOutput([]byte("\x1b[2J\x1b[3J"))
	first, after := term.Lines()
	assert.Greater(t, first, int64(1))
	assert.Equal(t, end, after) // the numbers go on
	assert.Equal(t, []string{"", "", "", "", ""}, allLines(term))
}

func TestScrollback_AlternateScreen(t *testing.T) {
	term := newScrollbackTerm(t)
	term.handleOutput([]byte("\x1b[?1049h" + strings.Repeat("x\r\n", 12) + "y"))
	assert.Equal(t, "x\nx\nx\nx\ny", term.content.Text())
	assert.Equal(t, []string{"1", "2", "3", "4"}, term.LineTexts(1, 5))

	term.handleOutput([]byte("\x1b[?1049l"))
	assert.Equal(t, "5\n6\n7\n8\n9", term.content.Text())
	assert.Equal(t, 4, term.cursorRow)
	assert.Equal(t, 1, term.cursorCol)
	_, end := term.Lines()
	assert.Equal(t, int64(10), end)
}

func TestScrollback_Reflow(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	cell := term.guessCellSize()
	resize := func(cols, rows float32) {
		term.Resize(fyne.NewSize(cell.Width*(cols+0.5), cell.Height*(rows+0.5)))
	}
	term.handleOutput([]byte("short\r\n0123456789abcde\r\nend\r\n$ "))
	assert.Equal(t, "short\n0123456789\nabcde\nend\n$ ", term.content.Text())

	resize(5, 5) // the scrollback takes the first line; the cursor's line keeps its blanks up to the cursor
	assert.Equal(t, "01234\n56789\nabcde\nend\n$ ", term.content.Text())
	assert.Equal(t, []string{"short", "0123456789abcde", "end", "$"}, allLines(term))
	assert.Equal(t, 4, term.cursorRow)
	assert.Equal(t, 2, term.cursorCol)

	resize(20, 5)
	assert.Equal(t, "short\n0123456789abcde\nend\n$ ", term.content.Text())
	assert.Equal(t, 3, term.cursorRow)
	assert.Equal(t, 2, term.cursorCol)
	term.handleOutput([]byte("ls"))
	assert.Equal(t, "$ ls", allLines(term)[3])

	resize(20, 2) // the screen shrinks to the cursor's line
	assert.Equal(t, "end\n$ ls", term.content.Text())
	resize(20, 8) // and takes the lines back
	assert.Equal(t, "short\n0123456789abcde\nend\n$ ls", term.content.Text())
	assert.Equal(t, 3, term.cursorRow)
}

func TestScrollback_ReflowAfterAlternateScreen(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	cell := term.guessCellSize()
	term.handleOutput([]byte("0123456789abc\r\n$ \x1b[?1049hvim"))
	term.Resize(fyne.NewSize(cell.Width*20.5, cell.Height*5.5))
	assert.Equal(t, "vim\n\n", term.content.Text())

	term.handleOutput([]byte("\x1b[?1049l"))
	assert.Equal(t, "0123456789abc\n$ ", term.content.Text())
	assert.Equal(t, 1, term.cursorRow)
	assert.Equal(t, 2, term.cursorCol)
}

func TestScrollback_Limit(t *testing.T) {
	term := newScrollbackTerm(t)
	term.SetScrollbackLimit(1000)
	term.handleOutput([]byte(numbered(10, 100)))
	first, end := term.Lines()
	assert.Greater(t, first, int64(1))
	assert.Equal(t, "L100", term.LineTexts(end-2, end-1)[0])
	assert.Equal(t, first, term.history().first)

	cells, _ := term.rowCells(term.topRow())
	assert.Contains(t, string(cellRunes(cells)), "older lines dropped")
}

func TestScrollback_Size(t *testing.T) {
	var row widget.TextGridRow
	for i, r := range "pod-1234 " + strings.Repeat("k", 63) + " Running" {
		style := &widget.CustomTextGridStyle{TextStyle: fyne.TextStyle{Monospace: true}} // a style per cell, as output writes them
		if i < 8 {
			style.FGColor = color.NRGBA{G: 0xff, A: 0xff}
		}
		row.Cells = append(row.Cells, widget.TextGridCell{Rune: r, Style: style})
	}
	h := newHistory(80)
	before := heapBytes()
	for i := 0; i < 10000; i++ {
		h.push(row, false)
	}
	perLine := float64(heapBytes()-before) / 10000
	t.Logf("%.0f bytes per line of %d cells", perLine, len(row.Cells))
	assert.Less(t, perLine, 400.0)
	runtime.KeepAlive(h)
}

func heapBytes() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestScrollback_View(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	output := func(s string) {
		term.handleOutput([]byte(s))
		term.Refresh()
	}
	output(numbered(1, 20))
	assert.Equal(t, term.content.Text(), term.view.Text())
	assert.Len(t, term.view.Rows, 5)

	term.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: term.guessCellSize().Height * 3}})
	assert.Equal(t, "L14\nL15\nL16\nL17\nL18", term.view.Text())
	assert.True(t, term.cursor.Hidden)

	output(numbered(21, 30)) // the view stays where the user scrolled to
	assert.Equal(t, "L14\nL15\nL16\nL17\nL18", term.view.Text())

	term.TypedKey(&fyne.KeyEvent{Name: desktop.KeySuperLeft}) // ⌘, as for ⌘C, keeps it
	term.TypedKey(&fyne.KeyEvent{Name: desktop.KeyControlLeft})
	assert.Equal(t, "L14\nL15\nL16\nL17\nL18", term.view.Text())
	term.TypedRune('a') // typing returns it to the screen
	assert.Equal(t, term.content.Text(), term.view.Text())
}

func TestScrollback_ViewHoldsTheScreenOnly(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	term.handleOutput([]byte(numbered(1, 5000)))
	term.Refresh()
	assert.Len(t, term.view.Rows, 5)
	assert.Less(t, len(term.content.Rows), 6)
}

func TestScrollback_SelectAcrossTheScrollback(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	term.handleOutput([]byte("0123456789abc\r\n" + numbered(1, 10)))
	term.Refresh()
	top := term.topRow()
	term.selStart = &position{Row: top + 1, Col: 3}
	term.selEnd = &position{Row: term.screenStart() + 1, Col: 2}
	term.highlightSelectedText()

	text := term.SelectedText()
	assert.True(t, strings.HasPrefix(text, "23456789abc\nL1\n"), text)
	assert.True(t, strings.HasSuffix(text, "\nL7"), text)

	term.handleOutput([]byte("more\r\n")) // the selection reaches the screen: output clears it
	assert.False(t, term.hasSelectedText())

	term.selStart = &position{Row: top + 1, Col: 1}
	term.selEnd = &position{Row: top + 2, Col: 3}
	term.handleOutput([]byte("more\r\n")) // in the scrollback it stays
	assert.Equal(t, "0123456789abc", term.SelectedText())
}

func TestScrollback_DoubleTapInTheScrollback(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	term.handleOutput([]byte("one two\r\n" + numbered(1, 10)))
	term.Refresh()
	term.scrollTo(term.topRow())
	cell := term.guessCellSize()
	term.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(cell.Width*5.5, cell.Height*0.5)})
	assert.Equal(t, "two", term.SelectedText())
}

func TestScrollback_WheelInTheAlternateScreen(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	in := &bytes.Buffer{}
	term.in = NopCloser(in)
	term.handleOutput([]byte(numbered(1, 20) + "\x1b[?1049h"))
	term.Refresh()
	cell := term.guessCellSize()
	up := &fyne.ScrollEvent{Scrolled: fyne.Delta{DY: cell.Height * 2}}

	term.Scrolled(up) // as the arrow keys in a full-screen program's mode
	assert.Equal(t, "\x1bOA\x1bOA", in.String())
	assert.False(t, term.scrolledBack)

	in.Reset()
	term.handleOutput([]byte("\x1b[?1000h"))
	up.Position = fyne.NewPos(cell.Width*2.5, cell.Height*1.5)
	term.Scrolled(up)
	assert.Equal(t, "\x1b[M`#\"\x1b[M`#\"", in.String())
}

func TestScrollback_ScrollBar(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	assert.False(t, term.bar.Visible())

	term.handleOutput([]byte(numbered(1, 100)))
	term.Refresh()
	assert.True(t, term.bar.Visible())
	bottom := term.screenStart()
	assert.Equal(t, bottom, term.viewTop)

	_, height, ok := term.bar.thumb()
	assert.True(t, ok)
	term.bar.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: -2 * (term.bar.Size().Height - height)}})
	term.bar.DragEnd()
	assert.Equal(t, term.topRow(), term.viewTop)

	term.bar.Tapped(&fyne.PointEvent{Position: fyne.NewPos(1, term.bar.Size().Height-1)})
	assert.Equal(t, term.topRow()+5, term.viewTop)
}

func cellRunes(cells []widget.TextGridCell) []rune {
	runes := make([]rune, len(cells))
	for i, c := range cells {
		runes[i] = c.Rune
	}
	return runes
}

func TestScrollback_PromptPaddingIsNoWrap(t *testing.T) {
	term := newScrollbackWindow(t, 20, 5)
	cell := term.guessCellSize()
	// zsh after a line without a line feed (PROMPT_SP): its mark, blanks to the right margin and
	// one more, a carriage return, the prompt
	term.handleOutput([]byte("partial%" + strings.Repeat(" ", 19) + "\r$ "))
	term.Resize(fyne.NewSize(cell.Width*30.5, cell.Height*5.5))
	term.Resize(fyne.NewSize(cell.Width*12.5, cell.Height*5.5))
	assert.Equal(t, []string{"partial%", "$"}, allLines(term))
	assert.Equal(t, "partial%\n$ ", term.content.Text())
}

var lineNumber = regexp.MustCompile(`n(\d+)`)

func TestScrollback_RandomOutputKeepsItsOrder(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		term := newScrollbackWindow(t, 20, 5)
		cell := term.guessCellSize()
		n := 0
		for step := 0; step < 60; step++ {
			switch rng.Intn(6) {
			case 0:
				cols, rows := 10+rng.Intn(80), 3+rng.Intn(20)
				term.Resize(fyne.NewSize(cell.Width*(float32(cols)+0.5), cell.Height*(float32(rows)+0.5)))
			case 1:
				term.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: cell.Height * float32(rng.Intn(10))}})
			case 2:
				n++
				pad := strings.Repeat(" ", max(int(term.config.Columns)-1, 0))
				term.handleOutput([]byte("n" + strconv.Itoa(n) + "%" + pad + "\r$ "))
			default:
				var b strings.Builder
				for k := 0; k < 1+rng.Intn(8); k++ {
					n++
					b.WriteString("\r\nn" + strconv.Itoa(n) + strings.Repeat("x", rng.Intn(120)))
				}
				term.handleOutput([]byte(b.String()))
			}
			term.Refresh()
			if term.cursorRow >= int(term.config.Rows) || len(term.content.Rows) > int(term.config.Rows) {
				t.Fatalf("seed %d step %d: the cursor on row %d, %d rows held, a screen of %d", seed, step, term.cursorRow, len(term.content.Rows), term.config.Rows)
			}
			last := 0
			for _, line := range allLines(term) {
				for _, m := range lineNumber.FindAllStringSubmatch(line, -1) {
					v, _ := strconv.Atoi(m[1])
					if v <= last {
						t.Fatalf("seed %d step %d: n%d after n%d", seed, step, v, last)
					}
					last = v
				}
			}
		}
	}
}

func TestScrollback_ShortcutsToTheShellReturnTheView(t *testing.T) {
	prev := goos
	t.Cleanup(func() { goos = prev })
	goos = "darwin"
	term := newScrollbackWindow(t, 10, 5)
	term.in = NopCloser(&bytes.Buffer{})
	term.handleOutput([]byte(numbered(1, 20)))
	term.Refresh()
	back := func() {
		term.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.Delta{DY: term.guessCellSize().Height * 3}})
		assert.True(t, term.scrolledBack)
	}

	back()
	term.TypedShortcut(&fyne.ShortcutCopy{}) // ⌘C copies and leaves the view
	assert.True(t, term.scrolledBack)
	term.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: fyne.KeyModifierControl}) // ctrl+C goes to the shell
	assert.False(t, term.scrolledBack)

	goos = "linux"
	back()
	term.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: fyne.KeyModifierShift | fyne.KeyModifierShortcutDefault})
	assert.True(t, term.scrolledBack)
	term.TypedShortcut(&fyne.ShortcutCopy{}) // ctrl+C there
	assert.False(t, term.scrolledBack)
}

func TestScrollback_DragEndingOutsideKeepsTheFocus(t *testing.T) {
	term := newScrollbackWindow(t, 10, 5)
	term.handleOutput([]byte(numbered(1, 20)))
	term.Refresh()
	c := fyne.CurrentApp().Driver().CanvasForObject(term)
	cell := term.guessCellSize()
	term.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(cell.Width, cell.Height)}, Dragged: fyne.Delta{DX: cell.Width}})
	c.Unfocus() // as Fyne does on a release outside the focused widget
	term.DragEnd()
	assert.Equal(t, fyne.Focusable(term), c.Focused())
	assert.NotEmpty(t, term.SelectedText())
}
