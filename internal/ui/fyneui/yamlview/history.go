package yamlview

import (
	"slices"
	"strings"
	"time"
)

// The editor's undo history (the owner's WF9; decisions D-84). Every edit is one replacement: at
// start the text old became new, with the caret and the selection before and after it. A run of
// typed characters is one step until a pause of typingPause, a caret move, a new line or an edit of
// another kind; consecutive Backspaces are one step, and so are consecutive forward Deletes; a run
// also ends at runBytes of text; a paste, a cut, a deleted selection, an indentation change and a
// replacement from outside the keyboard (ReplaceSelection: the find widget's Replace and Replace
// All) are a step each.
//
// The history holds only the user's own edits since the text was last set from outside: SetText,
// and UpdateText with a different text, clear it, because its steps describe another text (an
// object loaded, a Revert, a draft restored, an agent's text). It lives in the widget's memory
// only, never on disk or in the engine's state, so a Secret's values typed in the drawer stay
// there (ClearHistory drops it when the drawer moves away).

const (
	// historySteps and historyBytes bound the history: the oldest steps go first, but the newest
	// step is always kept, however large, so that a select-all and delete can be undone.
	historySteps = 1000
	historyBytes = 32 << 20
	// typingPause ends a run of typing, so that one ⌘Z never takes back minutes of typing.
	typingPause = time.Second
	// runBytes ends a run nobody types without a pause (a key held down for minutes, a benchmark),
	// so that what a keystroke adds to its run costs a bounded copy.
	runBytes = 4 << 10
)

// editKind says how an edit coalesces with the step before it.
type editKind uint8

const (
	editOther     editKind = iota // a step of its own
	editTyping                    // text typed at the caret; a new line starts a new run
	editBackspace                 // text deleted before the caret, no selection
	editDelete                    // text deleted after the caret, no selection
)

// cursor is the caret and the other end of the selection.
type cursor struct{ caret, anchor pos }

// step is one undoable edit.
type step struct {
	start, end    pos // end: after new, where a run of typing goes on
	old, new      string
	before, after cursor
	kind          editKind
	last          time.Time // when the step last took an edit
}

// history is the view's undo and redo stack.
type history struct {
	steps []step
	// index divides the steps: steps[:index] are done (Undo takes back steps[index-1]),
	// steps[index:] undone (Redo does steps[index] again).
	index int
	// bytes is the text the steps hold.
	bytes int
	// sealed makes the next edit a step of its own: after a caret move, an undo or a redo.
	sealed bool
	// now is the clock; nil: time.Now. Tests set it.
	now func() time.Time
}

func (h *history) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// record adds an edit, as a step of its own or merged into the newest step. Steps that could be
// redone are dropped: an edit after an undo starts a new branch.
func (h *history) record(e step) {
	now := h.clock()
	if h.index < len(h.steps) {
		for _, s := range h.steps[h.index:] {
			h.bytes -= len(s.old) + len(s.new)
		}
		clear(h.steps[h.index:])
		h.steps = h.steps[:h.index]
	}
	sealed := h.sealed
	h.sealed = false
	if !sealed && len(h.steps) > 0 && h.merge(&h.steps[len(h.steps)-1], e, now) {
		return
	}
	e.last, e.end = now, endOf(e.start, e.new)
	h.steps = append(h.steps, e)
	h.index = len(h.steps)
	h.bytes += len(e.old) + len(e.new)
	h.trim()
}

// merge folds e into s when it continues s: the same kind (not editOther), within typingPause of
// s's last edit, where s ended, and within runBytes.
func (h *history) merge(s *step, e step, now time.Time) bool {
	if e.kind != s.kind || e.kind == editOther || now.Sub(s.last) >= typingPause ||
		len(s.old)+len(s.new)+len(e.old)+len(e.new) > runBytes {
		return false
	}
	switch e.kind {
	case editTyping:
		if e.old != "" || strings.HasPrefix(e.new, "\n") || e.start != s.end {
			return false
		}
		s.new += e.new
		s.end = endOf(e.start, e.new)
	case editBackspace:
		if e.new != "" || endOf(e.start, e.old) != s.start {
			return false
		}
		s.start, s.end, s.old = e.start, e.start, e.old+s.old
	case editDelete:
		if e.new != "" || e.start != s.start {
			return false
		}
		s.old += e.old
	}
	h.bytes += len(e.old) + len(e.new)
	s.after, s.last = e.after, now
	return true
}

// trim drops the oldest steps beyond historySteps or historyBytes, never the newest.
func (h *history) trim() {
	n := 0
	for len(h.steps)-n > 1 && (len(h.steps)-n > historySteps || h.bytes > historyBytes) {
		h.bytes -= len(h.steps[n].old) + len(h.steps[n].new)
		n++
	}
	if n > 0 {
		h.steps = slices.Delete(h.steps, 0, n)
		h.index -= n
	}
}

// undo returns the step to take back, false when there is none.
func (h *history) undo() (step, bool) {
	if h.index == 0 {
		return step{}, false
	}
	h.index--
	h.sealed = true
	return h.steps[h.index], true
}

// redo returns the step to do again, false when there is none.
func (h *history) redo() (step, bool) {
	if h.index == len(h.steps) {
		return step{}, false
	}
	h.index++
	h.sealed = true
	return h.steps[h.index-1], true
}

// seal makes the next edit a step of its own.
func (h *history) seal() { h.sealed = true }

// reset forgets every step and lets go of the text they hold.
func (h *history) reset() {
	clear(h.steps)
	h.steps, h.index, h.bytes, h.sealed = nil, 0, 0, false
}

// endOf is the position after text s inserted at p.
func endOf(p pos, s string) pos {
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return pos{p.line, p.off + len(s)}
	}
	return pos{p.line + strings.Count(s, "\n"), len(s) - i - 1}
}
