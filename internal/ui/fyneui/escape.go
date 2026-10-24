package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Escape closes the innermost thing open, and only that (the owner's alpha walk of 2026-10-22,
// AW2, AW3, AW4 and AW19; D-116). Before, the window's Escape knew the palette, the notifications,
// a settings page and the drawer only, so over a dialog it closed the drawer behind it (Rollback's
// confirmation, AW3) or did nothing (Connection Updates, AW19); and Fyne hands a key to the widget
// that has the keyboard, the window's handler hearing it only while nothing has, so a field that
// took the keyboard by a click kept every Escape (a dropdown after its list closed, AW2; a search,
// AW4). The order, outermost last:
//
//  1. what has the keyboard, where it closes something of its own: an open dropdown's list (its
//     field gives the keyboard up, so that the next Escape goes on), a find bar, a search (cleared
//     and left, the keys back to the table or editor), the command palette's filter, a menu;
//  2. the window's top overlay, by what was registered for it (onEscape): a dialog's Cancel, or its
//     close for a dialog without one; a pop-up's close; the palette, the notifications, the MCP
//     details. An overlay nothing was registered for takes Escape and nothing behind it closes;
//  3. the drawer, as before. A settings page is a card over the window (settingsoverlay.go, D-118):
//     the window's top overlay while nothing is opened above it, its close registered for it, so it
//     closes in step 2; the step below stays for a page without one.
//
// The widgets that take the keyboard by a click hand an Escape they do not use on to the window's
// handler (passEscape): Cogpit's dropdown and searches, the fields, checks and sliders of Cogpit's
// dialogs (fieldEntry, fieldSelectEntry, fieldCheck, fieldSlider), and since 2026-10-24 the text
// fields of a settings page (the owner's decision (a) of 2026-10-23: the card closes, the value
// typed set first by D-118's flushTyping; the agent connection form's fields close the form, its
// Cancel). Fyne's own widgets keep theirs elsewhere: a button reached by Tab, a check group's and a
// radio group's items (Argo CD's Sync dialog), the fields inside Fyne's file and colour dialogs.

// overlayEscape is what Escape does over one overlay of a canvas.
type overlayEscape struct {
	canvas fyne.Canvas
	close  func()
}

// overlayEscapes holds, by the overlay object on its canvas's stack, what Escape does over it. A
// pop-up keeps its overlay to itself (Fyne's PopUp and dialogs), so it is registered as the overlay
// its Show added to the stack (onShown). Main goroutine only, as every widget.
var overlayEscapes = map[fyne.CanvasObject]overlayEscape{}

// onEscape makes Escape run shut while overlay is the top of c's overlays.
func onEscape(c fyne.Canvas, overlay fyne.CanvasObject, shut func()) {
	if c == nil || overlay == nil {
		return
	}
	pruneEscapes()
	overlayEscapes[overlay] = overlayEscape{canvas: c, close: shut}
}

// onShown runs show, a pop-up's Show, and makes Escape run shut over the overlay it added to c's
// stack; nothing is registered when it added none (it was shown already, and registered then).
func onShown(c fyne.Canvas, show func(), shut func()) {
	if c == nil {
		show()
		return
	}
	before := c.Overlays().Top()
	show()
	if top := c.Overlays().Top(); top != before {
		onEscape(c, top, shut)
	}
}

// pruneEscapes forgets the overlays no longer on their canvas.
func pruneEscapes() {
	for o, e := range overlayEscapes {
		if !slices.Contains(e.canvas.Overlays().List(), o) {
			delete(overlayEscapes, o)
		}
	}
}

// escapeOverlay is Escape over c's top overlay: it runs what was registered for it and reports
// whether an overlay is shown at all (one without a registration takes the key too).
func escapeOverlay(c fyne.Canvas) bool {
	top := c.Overlays().Top()
	if top == nil {
		return false
	}
	if e, ok := overlayEscapes[top]; ok && e.close != nil {
		e.close()
	}
	return true
}

// passEscape hands Escape to the window's key handler, as the driver does when nothing has the
// keyboard: for a focused widget that has no use of its own for it.
func passEscape(obj fyne.CanvasObject) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(obj); c != nil && c.OnTypedKey() != nil {
		c.OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
	}
}

// escape is the window's Escape: the top overlay's (a settings page's card among them), else the
// drawer.
func (sh *shell) escape() {
	if escapeOverlay(sh.win.Canvas()) {
		return
	}
	if sh.settings.visible() { // a settings page lies over the views
		sh.settings.close()
		return
	}
	sh.closeDrawer()
}

// fyneDialog is a dialog of Fyne's dialog package. Its Dismiss closes it as its Cancel does: the
// callback gets false (a file dialog's nil), and a dialog with buttons of Cogpit's own, whose
// Cancel is Hide, answers no in its SetOnClosed for every way it closes.
type fyneDialog interface {
	Show()
	Dismiss()
}

// showDialog shows a dialog of Fyne's dialog package, every one of the window's (a dialog shown
// again included); Escape over it dismisses it.
func (sh *shell) showDialog(d fyneDialog) { onShown(sh.win.Canvas(), d.Show, d.Dismiss) }

// fieldEntry is a text field of a dialog or a settings page: Escape goes on to the window (the
// dialog's Cancel, the settings card's close, which sets the value typed first: D-118's
// flushTyping) instead of stopping in the field, or runs onEscape where the field has a close of
// its own (a find bar's replace field, the agent connection form's Cancel).
type fieldEntry struct {
	widget.Entry
	onEscape func()
}

func newFieldEntry() *fieldEntry {
	e := &fieldEntry{}
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip) // as widget.NewEntry
	e.ExtendBaseWidget(e)
	return e
}

// newFieldMultiLineEntry is a fieldEntry of several lines, as widget.NewMultiLineEntry.
func newFieldMultiLineEntry() *fieldEntry {
	e := newFieldEntry()
	e.MultiLine = true
	return e
}

// newFieldPasswordEntry is a masked fieldEntry with Fyne's eye to show it, as
// widget.NewPasswordEntry (the entry's renderer adds the eye to a password field without one).
func newFieldPasswordEntry() *fieldEntry {
	e := newFieldEntry()
	e.Password = true
	return e
}

// TypedKey implements fyne.Focusable.
func (e *fieldEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if e.onEscape != nil {
			e.onEscape()
		} else {
			passEscape(e)
		}
		return
	}
	e.Entry.TypedKey(ev)
}

// fieldSelectEntry is an editable field with a list in a dialog (the create dialogs' names and
// namespaces) or a settings page (Preferences' time zone, the agent form's model): Escape goes on to
// the window, or runs onEscape where the field has a close of its own; its open list is a menu of
// its own, which Escape closes first.
type fieldSelectEntry struct {
	widget.SelectEntry
	onEscape func()
}

func newFieldSelectEntry(options []string) *fieldSelectEntry {
	e := &fieldSelectEntry{}
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip) // as widget.NewSelectEntry
	e.ExtendBaseWidget(e)
	e.SetOptions(options)
	return e
}

// TypedKey implements fyne.Focusable.
func (e *fieldSelectEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if e.onEscape != nil {
			e.onEscape()
		} else {
			passEscape(e)
		}
		return
	}
	e.SelectEntry.TypedKey(ev)
}

// fieldCheck is a check of a dialog, which takes the keyboard when clicked: Escape goes on to the
// window.
type fieldCheck struct{ widget.Check }

func newFieldCheck(label string) *fieldCheck {
	c := &fieldCheck{}
	c.Text = label
	c.ExtendBaseWidget(c)
	return c
}

// TypedKey implements fyne.Focusable.
func (c *fieldCheck) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		passEscape(c)
		return
	}
	c.Check.TypedKey(ev)
}

// fieldSlider is a slider of a dialog (Scale's), which takes the keyboard when dragged or clicked:
// Escape goes on to the window.
type fieldSlider struct{ widget.Slider }

func newFieldSlider(lo, hi float64) *fieldSlider {
	s := &fieldSlider{}
	s.Min, s.Max, s.Step, s.Orientation = lo, hi, 1, widget.Horizontal // as widget.NewSlider
	s.ExtendBaseWidget(s)
	return s
}

// TypedKey implements fyne.Focusable.
func (s *fieldSlider) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		passEscape(s)
		return
	}
	s.Slider.TypedKey(ev)
}
