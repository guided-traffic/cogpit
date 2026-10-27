package terminal

import (
	"bytes"
	"log"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	widget2 "github.com/fyne-io/terminal/internal/widget"
)

const (
	asciiBell      = 7
	asciiBackspace = 8
	asciiEscape    = 27

	noEscape = 5000
	tabWidth = 8
)

var charSetMap = map[charSet]func(rune) rune{
	charSetANSII: func(r rune) rune {
		return r
	},
	charSetDECSpecialGraphics: func(r rune) rune {
		m, ok := decSpecialGraphics[r]
		if ok {
			return m
		}
		return r
	},
	charSetAlternate: func(r rune) rune {
		return r
	},
}

var specialChars = map[rune]func(t *Terminal){
	asciiBell:      handleOutputBell,
	asciiBackspace: handleOutputBackspace,
	'\n':           handleOutputLineFeed,
	'\v':           handleOutputLineFeed,
	'\f':           handleOutputLineFeed,
	'\r':           handleOutputCarriageReturn,
	'\t':           handleOutputTab,
	0x0e:           handleShiftOut, // handle switch to G1 character set
	0x0f:           handleShiftIn,  // handle switch to G0 character set
}

// decSpecialGraphics is for ESC(0 graphics mode
// https://en.wikipedia.org/wiki/DEC_Special_Graphics
var decSpecialGraphics = map[rune]rune{
	'`': '◆', // filled in diamond
	'a': '▒', // filled in box
	'b': '␉', // horizontal tab symbol
	'c': '␌', // form feed symbol
	'd': '␍', // carriage return symbol
	'e': '␊', // line feed symbol
	'f': '°', // degree symbol
	'g': '±', // plus-minus sign
	'h': '␤', // new line symbol
	'i': '␋', // vertical tab symbol
	'j': '┘', // bottom right
	'k': '┐', // top right
	'l': '┌', // top left
	'm': '└', // bottom left
	'n': '┼', // cross
	'o': '⎺', // scan line 1
	'p': '⎻', // scan line 2
	'q': '─', // scan line 3
	'r': '─', // scan line 4
	's': '⎽', // scan line 5
	't': '├', // vertical and right
	'u': '┤', // vertical and left
	'v': '┴', // horizontal and up
	'w': '┬', // horizontal and down
	'x': '│', // vertical bar
	'y': '≤', // less or equal
	'z': '≥', // greater or equal
	'{': 'π', // pi
	'|': '≠', // not equal
	'}': '£', // Pounds currency symbol
	'~': '·', // centered dot
}

// maxCodeLen bounds the escape sequence or string being parsed; a longer one is dropped.
const maxCodeLen = 1 << 20

type parseState struct {
	code          []byte
	codeTooLong   bool
	esc           int
	escNext       bool // true when ESC was just seen; next char goes to parseEscState
	osc, apc, dcs bool
	vt100         rune
	printing      bool
}

func (t *Terminal) handleOutput(buf []byte) []byte {
	// Cogpit patch: the selection belongs to Fyne's main goroutine, where the mouse changes it.
	// Read and cleared here, on the goroutine reading the output, it raced that goroutine.
	fyne.Do(func() {
		if t.selectionOnScreen() { // Cogpit patch: a selection in the scrollback stays
			t.clearSelectedText()
		}
	})
	if t.state == nil {
		t.state = &parseState{
			esc: noEscape,
		}
	}
	var (
		size int
		r    rune
		i    = -1
		// Cogpit patch: the characters and control characters in a row go to Fyne's main goroutine
		// in one function; a function each, as many as the output has characters, cost a hand-over
		// between the goroutines each and made output twenty times slower than Terminal.app's.
		pending []rune
	)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending = nil
		fyne.Do(func() {
			for _, r := range batch {
				if out, ok := specialChars[r]; ok {
					out(t)
					continue
				}
				t.handleOutputChar(t.mapCharset(r))
			}
		})
	}
	defer flush()
	for {
		i += size
		buf = buf[size:]
		r, size = utf8.DecodeRune(buf)
		if size == 0 {
			break
		}
		if t.state.printing || r == asciiEscape || t.state.dcs || t.state.escNext || t.state.apc ||
			t.state.osc || t.state.vt100 != 0 || t.state.esc != noEscape {
			flush() // what follows queues its own functions, after these characters
		}
		if r == utf8.RuneError && size == 1 { // not UTF-8
			if !t.state.printing {
				if t.debug {
					log.Println("Invalid UTF-8", buf[0])
				}
				continue
			}
		}

		if t.state.printing {
			t.parsePrinting(buf, size)
			continue
		}
		if r == asciiEscape {
			t.state.esc = i
			t.state.escNext = true
			continue
		}
		if t.state.dcs {
			t.parseDCS(r)
			continue
		}
		if t.state.escNext {
			t.state.escNext = false
			if cont := t.parseEscState(r); cont {
				continue
			}
			t.state.esc = noEscape
			continue
		}
		if t.state.apc {
			t.parseAPC(r)
			continue
		}
		if t.state.osc {
			t.parseOSC(r)
			continue
		} else if t.state.vt100 != 0 {
			// Cogpit patch: the character sets are chosen on Fyne's main goroutine, see below.
			code := string([]rune{t.state.vt100, r})
			fyne.Do(func() {
				t.handleVT100(code)
			})
			t.state.vt100 = 0
			continue
		} else if t.state.esc != noEscape {
			t.parseEscape(r)
			continue
		}

		if out, ok := specialChars[r]; ok && out == nil {
			continue
		}
		// Cogpit patch: shift in and shift out switch the character set on Fyne's main goroutine.
		// Looked up here, on the goroutine reading the output, the set raced them; the lookup and
		// the designations (handleVT100) run on the main goroutine now, in flush.
		pending = append(pending, r)
	}

	return buf
}

// mapCharset returns r in the character set in use.
func (t *Terminal) mapCharset(r rune) rune {
	if t.useG1CharSet {
		return charSetMap[t.g1Charset](r)
	}
	return charSetMap[t.g0Charset](r)
}

func (t *Terminal) parseEscState(r rune) (shouldContinue bool) {
	switch r {
	case '[':
		return true
	case '\\':
		code, ok := t.state.takeCode()
		if t.state.osc && ok {
			fyne.Do(func() {
				t.handleOSC(code)
			})
		}
		// Cogpit patch: ST ends an APC string as well. The APC went on, and all later output was
		// added to it instead of being shown, until a NUL arrived.
		if t.state.apc && ok {
			fyne.Do(func() {
				t.handleAPC(code)
			})
		}
		t.state.osc = false
		t.state.apc = false
	case ']':
		t.state.osc = true
	case '(', ')':
		t.state.vt100 = r
	// Cogpit patch: these four changed the screen from the reader goroutine, racing the main
	// goroutine that renders and handles CSI sequences; they run there now, and a restored cursor
	// is clamped to the (possibly resized) screen.
	case '7':
		fyne.Do(func() {
			t.savedRow = t.cursorRow
			t.savedCol = t.cursorCol
		})
	case '8':
		fyne.Do(func() { t.moveCursor(t.savedRow, t.savedCol) })
	case 'D':
		fyne.Do(t.scrollDown)
	case 'M':
		fyne.Do(t.scrollUp)
	case 'P':
		t.state.dcs = true
	case '_':
		t.state.apc = true
	case '=', '>':
	}
	return false
}

func (t *Terminal) parseEscape(r rune) {
	t.state.addCode(r)
	if (r < '0' || r > '9') && r != ';' && r != '=' && r != '?' && r != '>' {
		code, ok := t.state.takeCode()
		switch {
		case !ok:
		case r == 'i':
			// Cogpit patch: media copy decides how the output after it is parsed, here, so it is
			// handled here. On Fyne's main goroutine, its printing state raced this goroutine,
			// which could parse output meant for the printer as text before it was set.
			escapePrinterMode(t, trimLeftZeros(code[:len(code)-1]))
		default:
			fyne.Do(func() {
				t.handleEscape(code)
			})
		}
		t.state.esc = noEscape
	}
}

// addCode adds r to the escape sequence or string being parsed.
//
// Cogpit patch: the sequence was a string extended with +=, which copies all of it every time.
// An unterminated OSC, APC or DCS string, or a long CSI, took time quadratic in its length on the
// goroutine reading the output (about a minute for 1 MiB, in which nothing else was shown), and
// memory without a bound. It is a buffer now, and one longer than maxCodeLen is dropped.
func (s *parseState) addCode(r rune) {
	if len(s.code) >= maxCodeLen {
		s.codeTooLong = true
		return
	}
	s.code = utf8.AppendRune(s.code, r)
}

// takeCode returns the sequence parsed so far and starts a new one. It reports false if the
// sequence was longer than maxCodeLen.
func (s *parseState) takeCode() (string, bool) {
	code, ok := string(s.code), !s.codeTooLong
	s.code, s.codeTooLong = s.code[:0], false
	if cap(s.code) > 4096 {
		s.code = nil // let a long one go
	}
	return code, ok
}

func (t *Terminal) parsePrinting(buf []byte, size int) {
	t.printData = append(t.printData, buf[:size]...)
	if bytes.HasSuffix(t.printData, []byte{asciiEscape, '[', '4', 'i'}) {
		// Handle the end of printing
		t.printData = t.printData[:len(t.printData)-4]
		escapePrinterMode(t, "4")
		t.state.esc = noEscape
	}
}

func (t *Terminal) parseAPC(r rune) {
	if r == 0 {
		if code, ok := t.state.takeCode(); ok {
			fyne.Do(func() {
				t.handleAPC(code)
			})
		}
		t.state.apc = false
	} else {
		t.state.addCode(r)
	}
}

func (t *Terminal) parseOSC(r rune) {
	if r == asciiBell || r == 0 {
		if code, ok := t.state.takeCode(); ok {
			fyne.Do(func() {
				t.handleOSC(code)
			})
		}
		t.state.osc = false
	} else {
		t.state.addCode(r)
	}
}

func (t *Terminal) parseDCS(r rune) {
	if r == '\\' {
		if code, ok := t.state.takeCode(); ok {
			fyne.Do(func() {
				t.handleDCS(code)
			})
		}
		t.state.dcs = false
	} else {
		t.state.addCode(r)
	}
}

func (t *Terminal) handleOutputChar(r rune) {
	if t.cursorCol >= int(t.config.Columns) { // may be beyond the edge if restored from a wider terminal
		if !t.disableAutoWrap {
			t.markWrapped(t.cursorRow) // Cogpit patch: for the scrollback and reflow
			t.cursorCol = 0
			handleOutputLineFeed(t)
		} else {
			// In non-wrap mode, overwrite the last character
			t.cursorCol = int(t.config.Columns) - 1
		}
	}

	var cellStyle widget.TextGridStyle
	textStyle := fyne.TextStyle{
		Monospace: true,

		Bold:          t.bold,
		Italic:        t.italic,
		Underline:     t.underline,
		Strikethrough: t.strikethrough,
	}
	fg, bg := t.cellColors() // Cogpit patch: reverse video applied here, see cellColors
	cellStyle = &widget.CustomTextGridStyle{
		FGColor: fg, BGColor: bg,
		TextStyle: textStyle,
	}
	if t.blinking {
		cellStyle = widget2.NewTermTextGridStyle(fg, bg, highlightBitMask, t.blinking, textStyle)
	}

	row, col := t.cursorRow, t.cursorCol
	cell := widget.TextGridCell{Rune: r, Style: cellStyle}
	oldLen := 0
	if len(t.content.Rows) > row {
		oldLen = len(t.content.Rows[row].Cells)
	}
	t.setCell(row, col, cell) // Cogpit patch: the content has no renderer, see setCell

	for i := oldLen; i < col; i++ {
		if t.content.Rows[row].Cells[i].Rune == 0 {
			t.content.Rows[row].Cells[i].Rune = ' '
		}
	}
	t.lastChar = r
	t.cursorCol++
}

func (t *Terminal) ringBell() {
	t.bell = true
	t.Refresh()

	go func() {
		time.Sleep(time.Millisecond * 300)
		// Cogpit patch: the bell is read on Fyne's main goroutine when the cursor is drawn;
		// cleared on this goroutine, it raced it. A closed terminal is left alone: its renderers
		// may be gone, and the refresh would make them again.
		fyne.Do(func() {
			if t.isStopping() {
				return
			}
			t.bell = false
			t.Refresh()
		})
	}()
}

func (t *Terminal) scrollUp() {
	// Cogpit patch: the region may reach past the rows written so far.
	if t.scrollTop < 0 || t.scrollBottom < t.scrollTop {
		return
	}
	for len(t.content.Rows) <= t.scrollBottom {
		t.content.Rows = append(t.content.Rows, widget.TextGridRow{})
	}
	for i := t.scrollBottom; i > t.scrollTop; i-- {
		t.content.Rows[i] = t.content.Row(i - 1)
	}
	t.content.Rows[t.scrollTop] = widget.TextGridRow{}
}

func (t *Terminal) scrollDown() {
	// Cogpit patch: an invalid region is ignored rather than indexed.
	if t.scrollTop < 0 || t.scrollBottom < t.scrollTop {
		return
	}
	t.pushToHistory(1) // Cogpit patch: the row scrolling off goes to the scrollback
	i := t.scrollTop
	for ; i < t.scrollBottom && i < len(t.content.Rows)-1; i++ {
		t.content.Rows[i] = t.content.Row(i + 1)
	}
	for ; i < len(t.content.Rows); i++ {
		if len(t.content.Rows) > t.scrollBottom {
			t.content.Rows[t.scrollBottom] = widget.TextGridRow{}
		} else {
			t.content.Rows = append(t.content.Rows, widget.TextGridRow{})
		}
	}
}

func handleOutputBackspace(t *Terminal) {
	row := t.content.Row(t.cursorRow)
	if len(row.Cells) == 0 {
		return
	}
	t.moveCursor(t.cursorRow, t.cursorCol-1)
}

func handleOutputBell(t *Terminal) {
	t.ringBell()
}

func handleOutputCarriageReturn(t *Terminal) {
	t.unwrapPadding() // Cogpit patch
	t.moveCursor(t.cursorRow, 0)
}

func handleOutputLineFeed(t *Terminal) {
	if t.cursorRow == t.scrollBottom {
		t.scrollDown()
		if t.newLineMode {
			t.moveCursor(t.cursorRow, 0)
		}
		return
	}
	if t.newLineMode {
		t.moveCursor(t.cursorRow+1, 0)
		return
	}
	t.moveCursor(t.cursorRow+1, t.cursorCol)
}

func handleOutputTab(t *Terminal) {
	end := t.cursorCol - t.cursorCol%tabWidth + tabWidth
	// Cogpit patch: a tab stops at the right margin and never wraps. Unclamped, the loop wrapped
	// to column 0 before reaching end and never finished, on Fyne's main goroutine.
	if cols := int(t.config.Columns); end > cols-1 {
		end = cols - 1
	}
	for t.cursorCol < end {
		t.handleOutputChar(' ')
	}
}

func handleShiftOut(t *Terminal) {
	t.useG1CharSet = true
}

func handleShiftIn(t *Terminal) {
	t.useG1CharSet = false
}

// SetPrinterFunc sets the printer function which is executed when printing.
func (t *Terminal) SetPrinterFunc(printerFunc PrinterFunc) {
	t.printer = printerFunc
}
