package termview

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Bounds of the log view (budget P5 in docs/developer/testing.md: follow 10,000 lines per second,
// memory bounded by the scrollback).
const (
	logScrollback   = 10_000                 // lines kept
	logMaxBytes     = 32 << 20               // bytes kept over all lines; the oldest go first
	logMaxLineBytes = 64 << 10               // longer lines are cut here
	logFlushPeriod  = 100 * time.Millisecond // at most one UI update per interval
	logTabWidth     = 8
	// logMaxShownBytes bounds the text one row renders. It is wider than any screen (the row is
	// clipped anyway), and measuring a 64 KiB line on every refresh would cost the main goroutine.
	logMaxShownBytes = 2048
)

// truncatedMark ends a line cut at logMaxLineBytes.
const truncatedMark = " …"

// logFlushEvery is the flusher's interval, logFlushPeriod; the package's tests shorten it.
var logFlushEvery = logFlushPeriod

// Logs shows a log stream, following it by default.
//
// A reader goroutine splits the stream into lines, strips escape sequences and control
// characters (log lines are untrusted text, never markup) and queues them; a flusher hands the
// queue to the main goroutine at most every logFlushEvery. Only the main goroutine touches the
// widgets and the scrollback, and the list renders only the visible rows.
//
// How the lines show is a LogView (SetView): the resource names and timestamps in their colours or
// hidden, long lines wrapped, a search highlighted or filtering. The list's rows are an index over
// the kept lines, a part of a line each, kept up to date as lines come and go.
type Logs struct {
	widget.BaseWidget
	stream io.ReadCloser
	// do runs a function on the main goroutine; nil means fyne.DoAndWait. The shell passes its own
	// (fyne.Do in the app, a queue in tests), so that tests drive the main goroutine themselves.
	do func(func())

	// Reader side, guarded by mu.
	mu       sync.Mutex
	pending  *lineRing
	received int64
	ended    bool
	endErr   error

	// Main goroutine only.
	lines       *lineRing
	follow      bool
	under       float32 // how far behind the newest line a followed view stays (logsfollow.go)
	list        *logList
	followCheck *widget.Check
	status      *widget.Label
	controls    *fyne.Container // the caller's controls in the bar (SetControls)
	header      *fyne.Container // the caller's row above the bar (SetHeader); hidden while empty
	batch       []string
	view        LogView
	rows        []displayRow // the list's rows, oldest first
	rowLines    int          // lines with at least one row
	head        headRow      // the row above the lines (logsolder.go)
	fences      map[string]*fence
	prepended   int64 // lines Prepend added
	jumped      int64 // the line the last jump found (logsjump.go), if hasJumped
	hasJumped   bool
	cols        int      // characters per row while wrapping; 0 until laid out
	runs        []logRun // scratch for rendering a row
	// Selecting text (logselect.go): the selection, the surface over the list that makes it, the
	// copy's target, and the rows the list set up last, by item.
	sel       logSel
	surface   *logSurface
	onCopy    func(string)
	shownRows map[widget.ListItemID]*logRow
	// The stream's state as the status shows it: lines received, and its end.
	stReceived int64
	stEnded    bool
	stErr      error

	stop       chan struct{}
	flushed    chan struct{} // closed when the flusher has published the end of the stream or stopped
	closeOnce  sync.Once
	closeErr   error
	onFlush    func(d time.Duration, lines int) // test hook: time spent on the main goroutine per batch
	lineStride float32
}

// NewLogs shows stream; it reads until the stream ends or Close is called.
func NewLogs(stream io.ReadCloser) *Logs { return NewLogsOn(stream, nil) }

// NewLogsOn is NewLogs with the function that runs work on the main goroutine (nil:
// fyne.DoAndWait). The flusher waits for each batch to be applied, or for Close.
func NewLogsOn(stream io.ReadCloser, do func(func())) *Logs {
	l := &Logs{
		do:      do,
		stream:  stream,
		pending: newLineRing(logScrollback, logMaxBytes),
		lines:   newLineRing(logScrollback, logMaxBytes),
		follow:  true,
		stop:    make(chan struct{}),
		flushed: make(chan struct{}),
	}
	l.list = &logList{}
	l.list.Length = func() int { return len(l.rows) + l.headRows() }
	l.list.CreateItem = func() fyne.CanvasObject { return newLogRow() }
	l.list.UpdateItem = l.updateItem
	l.list.OnSelected = l.selected
	l.list.HideSeparators = true
	l.list.onUserScroll = l.userScrolled // Follow by scrolling (logsfollow.go)
	l.list.ExtendBaseWidget(l.list)
	l.controls = container.NewHBox()
	l.header = container.NewHBox()
	l.header.Hide()
	l.followCheck = widget.NewCheck("Follow", nil)
	l.followCheck.Checked = true
	l.followCheck.OnChanged = l.setFollow
	l.status = widget.NewLabel("0 lines")
	l.surface = newLogSurface(l)
	l.shownRows = map[widget.ListItemID]*logRow{}
	l.ExtendBaseWidget(l)
	l.startReading()
	return l
}

// FocusTarget is the widget that takes the keyboard: the surface over the lines, which a press
// focuses too (⌘C, ⌘A, "/" for the tab's search; logselect.go). The dock gives it the keys when the
// user opens the tab (fyneui/dockfocus.go).
func (l *Logs) FocusTarget() fyne.Focusable { return l.surface }

// Close stops reading and closes the stream. It is idempotent.
func (l *Logs) Close() error {
	l.closeOnce.Do(func() {
		close(l.stop)
		if l.stream != nil {
			l.closeErr = l.stream.Close()
		}
	})
	return l.closeErr
}

// CreateRenderer implements fyne.Widget.
func (l *Logs) CreateRenderer() fyne.WidgetRenderer {
	bar := container.NewHBox(l.followCheck, l.controls, layout.NewSpacer(), l.status)
	rows := container.New(surfaceLayout{}, l.list, l.surface) // the surface selects text (logselect.go)
	return &logsRenderer{l: l, root: container.NewBorder(container.NewVBox(l.header, bar), nil, nil, nil, rows)}
}

// logsRenderer lays the bar and the list out and, while wrapping, re-wraps when the width in
// characters changes.
type logsRenderer struct {
	l    *Logs
	root *fyne.Container
}

func (r *logsRenderer) Layout(size fyne.Size) {
	r.root.Resize(size)
	r.l.widthChanged(r.l.list.Size().Width)
	if r.l.follow && r.l.list.scroll != nil {
		r.l.toEnd() // a followed view resized stays at its newest line
	}
}

func (r *logsRenderer) MinSize() fyne.Size           { return r.root.MinSize() }
func (r *logsRenderer) Refresh()                     { r.root.Refresh() }
func (r *logsRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.root} }
func (r *logsRenderer) Destroy()                     {}

// SetControls puts the caller's controls in the bar, after Follow. Main goroutine only.
func (l *Logs) SetControls(objs ...fyne.CanvasObject) {
	l.controls.Objects = objs
	l.controls.Refresh()
}

// SetHeader puts the caller's row above the bar (a logs tab's pod and container selectors); none
// hides it. Main goroutine only.
func (l *Logs) SetHeader(objs ...fyne.CanvasObject) {
	l.header.Objects = objs
	if len(objs) == 0 {
		l.header.Hide()
	} else {
		l.header.Show()
	}
	l.header.Refresh()
}

// SetOnSlash sets what "/" does while the list has the keyboard focus: the logs tab focuses its
// search. Main goroutine only.
func (l *Logs) SetOnSlash(fn func()) { l.list.onSlash = fn }

// StatusText returns the status at the bar's right: the lines held, shown and dropped, and the
// stream's end. Main goroutine only.
func (l *Logs) StatusText() string { return l.status.Text }

// View returns how the lines show. Main goroutine only.
func (l *Logs) View() LogView { return l.view }

// SetView changes how the lines show and rebuilds the rows. Main goroutine only.
func (l *Logs) SetView(v LogView) {
	if v == l.view {
		return
	}
	if shownChanged(v, l.view) { // the selection's offsets would point elsewhere in the lines
		l.sel = logSel{}
	}
	l.view = v
	l.rebuildRows()
	l.refreshKeepingPlace(0)
	l.updateStatus()
}

// ShownLines returns every line the view holds of the sources it shows, as it shows them (names
// and timestamps as shown, not filtered, not wrapped). Main goroutine only.
func (l *Logs) ShownLines() []string { return l.lines.shown(l.view, false) }

// DownloadLines returns what a download saves (A84, the owner's decision of 2026-10-14 evening):
// the lines the view shows, as it shows them: of the sources it selects, with its filter applied
// while Filter is on (a search alone highlights and hides nothing, so every line is saved), the
// names and timestamps as shown, not wrapped. Main goroutine only.
func (l *Logs) DownloadLines() []string { return l.lines.shown(l.view, l.view.Filter) }

// shown returns the kept lines v shows, as it shows them; filtered, only those matching v's search.
func (r *lineRing) shown(v LogView, filtered bool) []string {
	out := make([]string, 0, r.len())
	for i := range r.len() {
		raw := r.at(i)
		if !v.selects(raw) {
			continue
		}
		text, _ := v.shown(raw)
		if filtered && !v.matches(text) {
			continue
		}
		out = append(out, text)
	}
	return out
}

// widthChanged re-wraps the rows when the width in characters changed while wrapping.
func (l *Logs) widthChanged(width float32) {
	th := l.list.Theme()
	// The scroll bar and the row's inner padding take from the text's room.
	room := width - th.Size(theme.SizeNameScrollBar) - 2*th.Size(theme.SizeNamePadding)
	cols := max(int(room/charWidth(th)), 1)
	if cols == l.cols {
		return
	}
	l.cols = cols
	if l.view.Wrap {
		l.rebuildRows()
		l.refreshKeepingPlace(0)
	}
}

// rebuildRows indexes every kept line again under the current view.
func (l *Logs) rebuildRows() {
	l.rows, l.rowLines = l.rows[:0], 0
	for i := range l.lines.len() {
		l.addRows(l.lines.evicted + int64(i))
	}
}

// addRows appends the rows of one kept line, if the view shows it.
func (l *Logs) addRows(abs int64) {
	var ok bool
	if l.rows, ok = l.appendRows(l.rows, abs); ok {
		l.rowLines++
	}
}

// appendRows appends the rows of one kept line to dst, if the view shows it, and reports whether
// it does.
func (l *Logs) appendRows(dst []displayRow, abs int64) ([]displayRow, bool) {
	raw := l.lines.at(int(abs - l.lines.evicted))
	if !l.view.selects(raw) {
		return dst, false
	}
	text, _ := l.view.shown(raw)
	if l.view.Filter && !l.view.matches(text) {
		return dst, false
	}
	if !l.view.Wrap {
		return append(dst, displayRow{line: abs, to: -1}), true
	}
	wrapLine(text, l.cols, func(from, to int) {
		dst = append(dst, displayRow{line: abs, from: int32(from), to: int32(to)})
	})
	return dst, true
}

// trimRows drops the rows of lines the scrollback dropped and returns how many it dropped.
func (l *Logs) trimRows() int {
	first, n := l.lines.evicted, 0
	for n < len(l.rows) && l.rows[n].line < first {
		// A line's first row counts it; line numbers go below zero once lines are prepended.
		if n == 0 || l.rows[n].line != l.rows[n-1].line {
			l.rowLines--
		}
		n++
	}
	if n > 0 {
		l.rows = append(l.rows[:0], l.rows[n:]...)
	}
	return n
}

// updateItem is the list's UpdateItem: a row's runs and its part of the selection. It keeps the
// rows by the item they show, so that a selection repaints them without a refresh of the list.
func (l *Logs) updateItem(id widget.ListItemID, o fyne.CanvasObject) {
	r := o.(*logRow)
	if l.shownRows[r.item] == r {
		delete(l.shownRows, r.item)
	}
	l.shownRows[id] = r
	r.item = id
	r.set(l.itemRuns(id), l.itemSel(id))
	if len(l.shownRows) > maxShownRows {
		first, last := l.onScreen()
		for k := range l.shownRows {
			if k < first || k > last {
				delete(l.shownRows, k) // a row out of sight, or one the list's pool dropped
			}
		}
	}
}

// maxShownRows bounds the rows updateItem keeps: a screen's worth is a few dozen.
const maxShownRows = 256

// onScreen returns the first and last list item in view.
func (l *Logs) onScreen() (first, last widget.ListItemID) {
	stride, offset := l.stride(), l.list.GetScrollOffset()
	first = max(int(offset/stride)-1, 0)
	last = int((offset+l.list.Size().Height)/stride) + 1
	return first, last
}

// rowRuns returns the coloured runs of row id. Main goroutine only.
func (l *Logs) rowRuns(id widget.ListItemID) []logRun {
	if id < 0 || id >= len(l.rows) {
		return nil
	}
	text, sp, from, to, cut := l.rowSpan(id)
	l.runs = runsOf(text, sp, from, to, l.view.Search, l.runs)
	if cut {
		l.runs = append(l.runs, logRun{text: truncatedMark, color: theme.ColorNamePlaceHolder})
	}
	return l.runs
}

// refreshKeepingPlace redraws the list after dropped rows were taken off its front: following, it
// stays at the newest row (or as far behind it as the user scrolled, logsfollow.go); else the rows
// in view stay where they are.
func (l *Logs) refreshKeepingPlace(dropped int) {
	if l.follow {
		l.list.Refresh()
		l.toEnd()
		return
	}
	offset := l.list.GetScrollOffset()
	l.list.Refresh()
	if dropped > 0 {
		l.list.ScrollToOffset(offset - float32(dropped)*l.stride())
	}
}

// SetFollow turns following on or off, as the Follow check does. Main goroutine only.
func (l *Logs) SetFollow(on bool) { l.followCheck.SetChecked(on) }

// Following reports whether the view follows the newest line. Main goroutine only.
func (l *Logs) Following() bool { return l.follow }

// setFollow is the Follow toggle: on, the view jumps to the newest line and stays there.
func (l *Logs) setFollow(on bool) {
	l.follow, l.under = on, 0
	if on {
		l.list.ScrollToBottom()
	}
}

func (l *Logs) startReading() {
	if l.stream == nil {
		l.ended = true
		close(l.flushed)
		return
	}
	go l.read()
	go l.flush(logFlushEvery)
}

// read splits the stream into cleaned lines.
func (l *Logs) read() {
	var err error
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("log view: internal error: %v", r)
			slog.Error("termview: log reader panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
		l.mu.Lock()
		l.ended, l.endErr = true, err
		l.mu.Unlock()
	}()
	br := bufio.NewReaderSize(l.stream, logMaxLineBytes)
	for {
		line, rerr := br.ReadSlice('\n')
		if errors.Is(rerr, bufio.ErrBufferFull) {
			l.add(cleanLogLine(line) + truncatedMark)
			for errors.Is(rerr, bufio.ErrBufferFull) { // drop the rest of the line
				_, rerr = br.ReadSlice('\n')
			}
			if rerr != nil {
				err = rerr
				return
			}
			continue
		}
		if len(line) > 0 {
			l.add(cleanLogLine(line))
		}
		if rerr != nil {
			err = rerr
			return
		}
	}
}

func (l *Logs) add(line string) {
	l.mu.Lock()
	l.pending.push(line)
	l.received++
	l.mu.Unlock()
}

// flush publishes queued lines to the main goroutine at most once per interval. It waits for each
// update to finish, so a slow main goroutine slows the flusher down instead of queueing updates;
// the reader keeps reading and the bounded queue drops the oldest lines.
func (l *Logs) flush(interval time.Duration) {
	defer close(l.flushed)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("termview: log flusher panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var shownReceived int64
	for {
		select {
		case <-l.stop:
			return
		case <-tick.C:
		}
		l.mu.Lock()
		batch := l.pending.drain(l.batch[:0])
		received, ended, endErr := l.received, l.ended, l.endErr
		l.mu.Unlock()
		l.batch = batch[:0] // reused once the main goroutine is done with it (DoAndWait)

		if len(batch) > 0 || received != shownReceived || ended {
			applied := l.onMain(func() {
				start := time.Now()
				l.apply(batch, received, ended, endErr)
				if l.onFlush != nil {
					l.onFlush(time.Since(start), len(batch))
				}
			})
			if !applied {
				return // closed while the batch waited; it is not reused
			}
			shownReceived = received
		}
		clear(batch)
		if ended {
			return
		}
	}
}

// onMain runs fn on the main goroutine and waits for it, or for Close: it reports whether fn ran.
func (l *Logs) onMain(fn func()) bool {
	if l.do == nil {
		fyne.DoAndWait(fn)
		return true
	}
	done := make(chan struct{})
	l.do(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
		return true
	case <-l.stop:
		return false
	}
}

// apply adds a batch to the scrollback and updates the widgets. Main goroutine only.
func (l *Logs) apply(batch []string, received int64, ended bool, endErr error) {
	if len(l.fences) > 0 {
		batch = l.dropFenced(batch)
	}
	for _, s := range batch {
		l.lines.push(s)
	}
	// Rows of dropped lines go, and the selection's part in them; the new lines that are still kept
	// get theirs. Dropped rows shift every index, so a view that does not follow moves up by their
	// height.
	dropped := l.trimRows()
	l.clampSelection()
	total := l.lines.evicted + int64(l.lines.len())
	for abs := max(total-int64(len(batch)), l.lines.evicted); abs < total; abs++ {
		l.addRows(abs)
	}
	l.stReceived, l.stEnded, l.stErr = received, ended, endErr
	l.updateStatus()
	l.refreshKeepingPlace(dropped)
}

// updateStatus shows how many lines the view holds, how many it shows when a filter or a
// selection narrows it, and the stream's end.
func (l *Logs) updateStatus() {
	text := statusText(l.lines.len(), l.stReceived+l.prepended, l.stEnded, l.stErr)
	if l.view.Filter && l.view.Search != "" || l.view.narrows() {
		text = fmt.Sprintf("%d of %s", l.rowLines, text)
	}
	l.status.SetText(text)
}

// stride is the height of one list row.
func (l *Logs) stride() float32 {
	if l.lineStride == 0 {
		t := canvas.NewText("M", theme.Color(theme.ColorNameForeground))
		t.TextStyle = fyne.TextStyle{Monospace: true}
		l.lineStride = t.MinSize().Height + l.list.Theme().Size(theme.SizeNamePadding)
	}
	return l.lineStride
}

func statusText(kept int, received int64, ended bool, endErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d lines", kept)
	if dropped := received - int64(kept); dropped > 0 {
		fmt.Fprintf(&b, " (%d older dropped)", dropped)
	}
	switch {
	case ended && (endErr == nil || errors.Is(endErr, io.EOF)):
		b.WriteString(" · stream ended")
	case ended:
		b.WriteString(" · stream ended: " + cleanLogLine([]byte(endErr.Error())))
	}
	return b.String()
}

// cleanLogLine turns one raw log line into display text: no line ending, no escape sequences,
// no control characters, tabs expanded, valid UTF-8.
func cleanLogLine(line []byte) string {
	line = trimEOL(line)
	if isPlainText(line) {
		return string(line)
	}
	var b strings.Builder
	b.Grow(len(line))
	col := 0
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == esc:
			i = skipEscape(line, i)
			continue
		case c == '\t':
			n := logTabWidth - col%logTabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			i++
			continue
		case c < 0x20 || c == 0x7f:
			i++
			continue
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			col++
			i++
			continue
		}
		r, size := utf8.DecodeRune(line[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case r >= 0x80 && r < 0xa0: // C1 controls
			continue
		default:
			b.WriteRune(r)
		}
		col++
	}
	return b.String()
}

// shownText is the part of a line a row renders.
func shownText(s string) string {
	if len(s) <= logMaxShownBytes {
		return s
	}
	cut := logMaxShownBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncatedMark
}

func trimEOL(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// isPlainText reports whether line needs no cleaning: printable ASCII only, the common case.
func isPlainText(line []byte) bool {
	for _, c := range line {
		if c < 0x20 || c >= 0x7f {
			return false
		}
	}
	return true
}

// skipEscape returns the index after the escape sequence starting at line[i] (an ESC): a control
// sequence (ESC [ … final), a string (OSC, DCS, APC, PM, SOS: up to BEL or ST), or ESC and one
// more character. A sequence cut off by the end of the line ends there.
func skipEscape(line []byte, i int) int {
	i++ // ESC
	if i >= len(line) {
		return i
	}
	switch line[i] {
	case '[':
		for i++; i < len(line); i++ {
			if line[i] >= 0x40 && line[i] <= 0x7e {
				return i + 1
			}
		}
		return i
	case ']', 'P', '_', '^', 'X':
		for i++; i < len(line); i++ {
			if line[i] == 0x07 {
				return i + 1
			}
			if line[i] == esc && i+1 < len(line) && line[i+1] == '\\' {
				return i + 2
			}
		}
		return i
	default:
		return i + 1
	}
}

// lineRing is a bounded scrollback: at most maxLines lines and maxBytes bytes; the oldest lines
// go first.
type lineRing struct {
	buf      []string
	head     int
	n        int
	bytes    int
	maxBytes int
	evicted  int64 // lines dropped from the front, in total
}

func newLineRing(maxLines, maxBytes int) *lineRing {
	return &lineRing{buf: make([]string, maxLines), maxBytes: maxBytes}
}

func (r *lineRing) len() int { return r.n }

// at returns line i, 0 being the oldest kept; "" when out of range.
func (r *lineRing) at(i int) string {
	if i < 0 || i >= r.n {
		return ""
	}
	return r.buf[(r.head+i)%len(r.buf)]
}

func (r *lineRing) push(s string) {
	if r.n == len(r.buf) {
		r.evict()
	}
	r.buf[(r.head+r.n)%len(r.buf)] = s
	r.n++
	r.bytes += len(s)
	for r.bytes > r.maxBytes && r.n > 1 {
		r.evict()
	}
}

func (r *lineRing) evict() {
	r.bytes -= len(r.buf[r.head])
	r.buf[r.head] = ""
	r.head = (r.head + 1) % len(r.buf)
	r.n--
	r.evicted++
}

// drain appends all lines to dst, oldest first, and empties the ring.
func (r *lineRing) drain(dst []string) []string {
	for i := 0; i < r.n; i++ {
		j := (r.head + i) % len(r.buf)
		dst = append(dst, r.buf[j])
		r.buf[j] = ""
	}
	r.head, r.n, r.bytes = 0, 0, 0
	return dst
}
