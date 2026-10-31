package termview

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Older lines (ui-parity › Logs, gap G8): the view says what it holds of each source (Held),
// puts lines older than its oldest before them without repeating one (Prepend), and shows a row
// above its lines (SetHead) that a logs tab makes its "Load older lines" trigger. All of it runs
// on the main goroutine.

// HeldSource is what the view holds of one source's lines.
type HeldSource struct {
	// Source is the "<pod>/<container>" prefix of a prefixed view's lines, "" otherwise.
	Source string
	Lines  int
	// Oldest is the oldest line's timestamp.
	Oldest time.Time
}

// HeadTone is how the head row shows.
type HeadTone int

// Head row tones.
const (
	HeadAction HeadTone = iota // the accent colour: selecting the row acts
	HeadInfo                   // muted
	HeadError                  // the error colour
)

// headRow is the row above the lines; empty text shows none.
type headRow struct {
	text     string
	tone     HeadTone
	onSelect func()
}

// fence holds lines Prepend put before a source the view held nothing of: the stream may still
// bring them (its own history), and apply drops them then, until the source's lines are newer.
type fence struct {
	newest time.Time
	lines  map[string]int
}

// SetHead shows a row above the lines, or none for an empty text. Selecting it (a tap, or the
// keyboard) calls onSelect when there is one. Main goroutine only.
func (l *Logs) SetHead(text string, tone HeadTone, onSelect func()) {
	had := l.headRows()
	l.head = headRow{text: text, tone: tone, onSelect: onSelect}
	if diff := l.headRows() - had; diff != 0 && !l.follow {
		offset := l.list.GetScrollOffset()
		l.list.Refresh()
		l.list.ScrollToOffset(offset + float32(diff)*l.stride())
		return
	}
	l.list.Refresh()
	if l.follow {
		l.toEnd()
	}
}

// Head returns the head row's text, "" when none shows. Main goroutine only.
func (l *Logs) Head() string { return l.head.text }

func (l *Logs) headRows() int {
	if l.head.text == "" {
		return 0
	}
	return 1
}

// headRuns are the head row's runs.
func (l *Logs) headRuns() []logRun {
	c := theme.ColorNamePrimary
	switch l.head.tone {
	case HeadInfo:
		c = theme.ColorNamePlaceHolder
	case HeadError:
		c = theme.ColorNameError
	}
	return []logRun{{text: l.head.text, color: c}}
}

// itemRuns are the runs of list item id: the head row first, then the rows.
func (l *Logs) itemRuns(id widget.ListItemID) []logRun {
	if l.headRows() == 1 {
		if id == 0 {
			return l.headRuns()
		}
		id--
	}
	return l.rowRuns(id)
}

// selected is the list's OnSelected: the head row acts and is not left selected.
func (l *Logs) selected(id widget.ListItemID) {
	if id != 0 || l.headRows() == 0 {
		return
	}
	l.list.Unselect(id)
	if fn := l.head.onSelect; fn != nil {
		fn()
	}
}

// source returns a line's source and timestamp: lines without a "<pod>/<container>" prefix in a
// prefixed view (Cogpit's own) and lines without a timestamp have none.
func (v LogView) source(raw string) (string, time.Time, bool) {
	rest, src := raw, ""
	if v.Prefixed {
		head, tail, found := strings.Cut(raw, " ")
		if !found || !strings.Contains(head, "/") {
			return "", time.Time{}, false
		}
		src, rest = head, tail
	}
	tok, _, _ := strings.Cut(rest, " ")
	if !looksLikeTimestamp(tok) {
		return "", time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, tok)
	if err != nil {
		return "", time.Time{}, false
	}
	return src, ts, true
}

// Held returns what the view holds of each source it shows (its selection), in the order the
// sources first appear. Main goroutine only.
func (l *Logs) Held() []HeldSource {
	idx := map[string]int{}
	var out []HeldSource
	for i := range l.lines.len() {
		raw := l.lines.at(i)
		if !l.view.selects(raw) {
			continue
		}
		src, ts, ok := l.view.source(raw)
		if !ok {
			continue
		}
		j, seen := idx[src]
		if !seen {
			j = len(out)
			idx[src] = j
			out = append(out, HeldSource{Source: src, Oldest: ts})
		}
		out[j].Lines++
		if ts.Before(out[j].Oldest) {
			out[j].Oldest = ts
		}
	}
	return out
}

// Room returns how many more lines the view keeps before it drops its oldest. Main goroutine
// only.
func (l *Logs) Room() int { return len(l.lines.buf) - l.lines.n }

// Capacity returns how many lines the view keeps at most.
func (l *Logs) Capacity() int { return len(l.lines.buf) }

// Prepend puts older lines (oldest first, written as the stream writes them) before the lines the
// view holds and returns how many it added. Of each source it takes the lines older than the
// oldest it holds and, at that line's timestamp, those it does not hold; of a source it holds
// nothing of, every line, which the stream then drops if it brings them too. When there is no room
// for all, the newest of them go in, next to the lines held. The rows in view stay in view unless
// the view follows. Main goroutine only.
func (l *Logs) Prepend(lines []string) int {
	type edge struct {
		oldest time.Time
		at     map[string]int
	}
	edges := map[string]*edge{}
	for i := range l.lines.len() {
		raw := l.lines.at(i)
		src, ts, ok := l.view.source(raw)
		if !ok {
			continue
		}
		switch e := edges[src]; {
		case e == nil || ts.Before(e.oldest):
			edges[src] = &edge{oldest: ts, at: map[string]int{raw: 1}}
		case ts.Equal(e.oldest):
			e.at[raw]++
		}
	}
	keep := make([]string, 0, len(lines))
	for _, line := range lines {
		raw := cleanPrepended(line)
		src, ts, ok := l.view.source(raw)
		if !ok {
			continue // older lines all carry a source and a timestamp
		}
		if e := edges[src]; e != nil {
			switch {
			case ts.After(e.oldest):
				continue // the view holds it
			case ts.Equal(e.oldest) && e.at[raw] > 0:
				e.at[raw]--
				continue
			}
		}
		keep = append(keep, raw)
	}
	added := 0
	for i := len(keep) - 1; i >= 0 && l.lines.pushFront(keep[i]); i-- {
		added++
	}
	if added == 0 {
		return 0
	}
	keep = keep[len(keep)-added:]
	for _, raw := range keep {
		src, ts, _ := l.view.source(raw)
		if edges[src] != nil {
			continue
		}
		f := l.fences[src]
		if f == nil {
			f = &fence{lines: map[string]int{}}
			if l.fences == nil {
				l.fences = map[string]*fence{}
			}
			l.fences[src] = f
		}
		f.lines[raw]++
		if ts.After(f.newest) {
			f.newest = ts
		}
	}
	rows := make([]displayRow, 0, added+len(l.rows)) // the held rows follow them
	shown := 0
	for abs := l.lines.evicted; abs < l.lines.evicted+int64(added); abs++ {
		var ok bool
		if rows, ok = l.appendRows(rows, abs); ok {
			shown++
		}
	}
	l.rows = append(rows, l.rows...)
	l.rowLines += shown
	l.prepended += int64(added)
	l.updateStatus()
	l.updateSide()
	if l.follow {
		l.list.Refresh()
		l.toEnd()
	} else {
		offset := l.list.GetScrollOffset()
		l.list.Refresh()
		l.list.ScrollToOffset(offset + float32(len(rows))*l.stride())
	}
	return added
}

// cleanPrepended is a prepended line as the stream's reader keeps it: cleaned, cut at the bound.
func cleanPrepended(line string) string {
	if len(line) > logMaxLineBytes {
		return cleanLogLine([]byte(line[:logMaxLineBytes])) + truncatedMark
	}
	return cleanLogLine([]byte(line))
}

// dropFenced drops from a batch the lines Prepend put in already, for each source until its lines
// are newer than those.
func (l *Logs) dropFenced(batch []string) []string {
	out := batch[:0]
	for _, s := range batch {
		if src, ts, ok := l.view.source(s); ok {
			if f := l.fences[src]; f != nil {
				switch {
				case ts.After(f.newest):
					delete(l.fences, src)
				case f.lines[s] > 0:
					f.lines[s]--
					continue
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// pushFront puts s before the oldest line, if the bounds leave room; it reports whether it did.
func (r *lineRing) pushFront(s string) bool {
	if r.n == len(r.buf) || r.bytes+len(s) > r.maxBytes {
		return false
	}
	r.head = (r.head - 1 + len(r.buf)) % len(r.buf)
	r.buf[r.head] = s
	r.n++
	r.bytes += len(s)
	r.evicted--
	return true
}

var _ fyne.Widget = (*Logs)(nil)
