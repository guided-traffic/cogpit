package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// F1–F12 open the hotbar's first twelve clusters in the plain search fields too (the
// owner's A64, 2026-10-14 evening): the view's search, the navigator's, the logs tab's, the Argo CD
// dashboard's activity filter, the editor's Find field, the command palette's filter and the
// namespace filter's search (nsfilter.go, which closes its list first). A text
// field takes every key while it has the focus, so the window's key handler never sees them there; these fields hand them to the hotbar first. The terminal and the YAML
// editor keep their F-keys: a shell or a program in it may use them, and the editor is a text area,
// not a search field. A field that is no search (a name, a path, a value in a dialog) keeps them too.

// hotbarKey hands an F-key typed in a search field to the hotbar: true when it took it; a shifted
// one it does not take (D-83). Shift comes from the desktop driver, as F-keys arrive without their
// modifiers.
func hotbarKey(sh *shell, ev *fyne.KeyEvent) bool {
	if functionKey(ev.Name) == 0 || sh.hot == nil {
		return false
	}
	return sh.hot.key(ev.Name, shiftHeld())
}

// searchEntry is a plain search field whose F-keys are the hotbar's. Escape ends the search (the
// owner's AW4 of 2026-10-22; escape.go): a find bar closes (escape), any other search is cleared
// and left, the keyboard going to back (the list's table) or nowhere.
type searchEntry struct {
	widget.Entry
	sh *shell
	// escape is the field's Escape in place of clearing and leaving it: a find bar's close.
	escape func()
	// back gets the keyboard when Escape leaves the field; nil: nothing has it.
	back fyne.Focusable
}

// CreateRenderer implements fyne.Widget: the wheel passed on (wheelpass.go).
func (e *searchEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}

func newSearchEntry(sh *shell) *searchEntry {
	e := &searchEntry{sh: sh}
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey implements fyne.Focusable.
func (e *searchEntry) TypedKey(ev *fyne.KeyEvent) {
	if hotbarKey(e.sh, ev) {
		return
	}
	if ev.Name == fyne.KeyEscape {
		e.leave()
		return
	}
	e.Entry.TypedKey(ev)
}

// leave is Escape in the field: the search ends.
func (e *searchEntry) leave() {
	if e.escape != nil {
		e.escape()
		return
	}
	if e.Text != "" {
		e.SetText("") // OnChanged sends the empty search, as typing it away does
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(e)
	if c == nil {
		return
	}
	if e.back != nil {
		c.Focus(e.back)
		return
	}
	c.Unfocus()
}
