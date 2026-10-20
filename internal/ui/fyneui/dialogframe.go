package fyneui

import (
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// dialogFrame is a dialog as Fyne's dialog package draws one (a modal pop-up, the title at its top
// left, the content, the buttons centred under it), built by Cogpit for two things Fyne's cannot do
// (the owner's gate walk of 2026-10-20, GW2 and GW6; D-104):
//
//   - A title that names an object in the one form, "Scale Deployment → gate-web", without a wide
//     gap after the arrow. Fyne's title is one text, shaped as one run: the arrow comes from Fyne's
//     symbol font (InterSymbols), which has no space, and go-text keeps a space after a glyph in
//     that glyph's font, so the space is drawn as the symbol font's missing glyph, about an em
//     wide. objectDialogTitle shapes "Scale Deployment →" and " gate-web" apart.
//   - Tooltips over the dialog: the shell's tooltip layer lies under every overlay, and only the
//     top overlay receives the pointer, so a dialog's "?" hints show in a layer of its own, the
//     frame's topmost object.
//
// The sizes are Fyne's own (sizeDialogPadX and sizeDialogPadY are its padWidth and padHeight), so
// a framed dialog and a dialog of Fyne's look alike. Like Fyne's, it has no Escape of its own.
type dialogFrame struct {
	sh      *shell
	frame   *fyne.Container // background, content, buttons, title, eyebrow, two rules, footer note, tooltips (dialogFrameLayout)
	rim     *glowFrame      // the card's rim: lit for a destructive or an agent-asked dialog (setRim)
	eyebrow *bracketLabel   // a small head over the title ("asked by · agent"); hidden without
	note    *textLine       // a line at the footer's left, on the buttons' baseline ("This cannot be undone."); hidden without
	popup   *widget.PopUp
	tip     *tooltip
	size    fyne.Size // what Resize asked for
	onClose []func()
	shown   bool
}

// newDialogFrame builds a dialog with title (dialogTitle, objectDialogTitle) and no content or
// buttons yet: setContent and SetButtons give them, Show shows it.
func newDialogFrame(sh *shell, title fyne.CanvasObject) *dialogFrame {
	d := &dialogFrame{sh: sh, tip: newDialogTooltip(sh)}
	bg := newSurface(theme.ColorNameOverlayBackground)
	bg.radius = theme.SizeNameDialogRadius
	bg.stroke = colorBracket // a hairline edge: the card parts from the dimmed window by it and by its step of luminance
	d.eyebrow = newBracketLabel("", colorMuted)
	d.eyebrow.Hide()
	d.note = newTextLine("", colorStatusError, theme.SizeNameText, fyne.TextStyle{})
	d.note.Hide()
	d.frame = box(dialogFrameLayout{eyebrow: d.eyebrow, note: d.note}, bg, container.NewStack(), container.NewGridWithRows(1), title,
		d.eyebrow, newDivider(false), newDivider(false), d.note, d.tip.layer)
	d.rim = newGlowFrame(d.frame, colorStatusError, sh.app.Settings().Theme().Size(theme.SizeNameDialogRadius))
	d.rim.SetGlow(false, colorStatusError)
	d.popup = widget.NewModalPopUp(d.rim, sh.win.Canvas())
	return d
}

// setRim lights the card's rim in tone: the error colour for a destructive confirmation, colorAgent
// for a request an agent made. An empty tone puts it out. One glow per dialog, and the dialog
// also says it in words (the red button, the eyebrow), so the glow is never the only signal.
func (d *dialogFrame) setRim(tone fyne.ThemeColorName) { d.rim.SetGlow(tone != "", tone) }

// setEyebrow shows a small bracket head over the title, in tone; empty text hides it.
func (d *dialogFrame) setEyebrow(text string, tone fyne.ThemeColorName) {
	d.eyebrow.tone = tone
	d.eyebrow.SetText(text)
	d.eyebrow.Refresh()
	if text == "" {
		d.eyebrow.Hide()
	} else {
		d.eyebrow.Show()
	}
	d.frame.Refresh()
}

// setNote shows a short line at the footer's left, on the buttons' baseline, in tone: what a
// destructive dialog must say in words ("This cannot be undone."), so that it is read with the
// buttons and does not float in the body. An empty text hides it.
func (d *dialogFrame) setNote(text string, tone fyne.ThemeColorName) {
	d.note.color = tone
	d.note.SetText(text)
	d.note.Refresh()
	if text == "" {
		d.note.Hide()
	} else {
		d.note.Show()
	}
	d.frame.Refresh()
}

// setContent puts content between the title and the buttons.
func (d *dialogFrame) setContent(content fyne.CanvasObject) {
	d.frame.Objects[1] = content
	d.frame.Refresh()
}

// SetButtons sets the row of buttons, as wide as each other, as Fyne's dialogs have them.
func (d *dialogFrame) SetButtons(buttons []fyne.CanvasObject) {
	d.frame.Objects[2] = container.NewGridWithRows(1, buttons...)
	d.frame.Refresh()
}

// SetOnClosed adds a function that runs whenever the dialog is hidden.
func (d *dialogFrame) SetOnClosed(f func()) { d.onClose = append(d.onClose, f) }

// MinSize is the smallest size the dialog takes.
func (d *dialogFrame) MinSize() fyne.Size { return d.popup.MinSize() }

// Resize asks for a size, never below MinSize.
func (d *dialogFrame) Resize(size fyne.Size) {
	d.size = size.Max(d.MinSize())
	d.popup.Resize(d.size)
}

// fit gives a shown dialog its minimum size again, centred in the window, after its content grew
// or shrank (a field's problem line shown or hidden, paramdialog.go): a modal pop-up grows by
// itself but never shrinks, and grows downwards.
func (d *dialogFrame) fit() {
	size := d.MinSize()
	if !d.shown || size == d.popup.Size() {
		return
	}
	d.size = size
	d.popup.Resize(size)
	area := d.sh.win.Canvas().Size()
	d.popup.Move(fyne.NewPos(max((area.Width-size.Width)/2, 0), max((area.Height-size.Height)/2, 0)))
}

// Show shows the dialog over the window.
func (d *dialogFrame) Show() {
	if !d.size.IsZero() {
		d.popup.Resize(d.size)
	}
	d.shown = true
	d.popup.Show()
}

// Hide closes the dialog and runs SetOnClosed's functions; a dialog not shown stays as it is.
func (d *dialogFrame) Hide() {
	if !d.shown {
		return
	}
	d.shown = false
	d.tip.hide()
	d.popup.Hide()
	for _, f := range d.onClose {
		f()
	}
}

// dialogTitle is a dialog's title in the look of Fyne's: bold, in a label's padding.
func dialogTitle(text string) *widget.RichText {
	return titleText(&widget.TextSegment{Text: text, Style: titleStyle()})
}

// titleStyle is a dialog title's style: bold and a step larger than the text, so that the header
// reads as one.
func titleStyle() widget.RichTextStyle {
	st := widget.RichTextStyleStrong
	st.SizeName = theme.SizeNameSubHeadingText
	return st
}

// objectDialogTitle is the title of a dialog that acts on one object, "Scale Deployment →
// gate-web" ("Install Release → web?" with tail "?"), the object in the one form (objectnames.go):
// the verb, the kind and the arrow are one text and the name another, shaped apart, so that no
// space follows the arrow in its run (dialogFrame).
func objectDialogTitle(verb, kind, name, tail string) *widget.RichText {
	lead := kind + " " + objectArrow
	if verb != "" {
		lead = verb + " " + lead
	}
	head := &widget.TextSegment{Text: lead, Style: titleStyle()}
	head.Style.Inline = true
	return titleText(head, &widget.TextSegment{Text: " " + name + tail, Style: titleStyle()})
}

// titleText is a title's rich text: as wide as its text, as Fyne's dialog title is.
func titleText(segs ...widget.RichTextSegment) *widget.RichText { return widget.NewRichText(segs...) }

// titleString is a dialog title's text, for tests and logs.
func titleString(t *widget.RichText) string { return t.String() }

// The framed dialog's metrics, in points: the card's side padding, the header's padding above and
// below the title, the content's distance from the header's rule and the footer's padding around
// the buttons.
const (
	dialogSide    = 22
	dialogHeadV   = 16
	dialogHeadGap = 12
	dialogBodyGap = 14
	dialogFootV   = 12
)

// dialogFrameLayout places a dialogFrame's objects as a card with a header and a footer: the
// background under everything; the header (an optional eyebrow, the title) above a rule; the content;
// a rule, the buttons right-aligned and an optional note left-aligned in the footer; the tooltips'
// layer over all of it. With an eyebrow the bracket's own line is the header's rule, so the second
// hairline is not drawn (two parallel lines read as noise). Objects: background, content, buttons,
// title, eyebrow, header rule, footer rule, note, tooltips.
type dialogFrameLayout struct {
	eyebrow *bracketLabel
	note    *textLine
}

func (l dialogFrameLayout) pads() (gap float32) {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

// head is the header's height: its padding, the eyebrow when shown, the title and the padding below.
func (l dialogFrameLayout) head(title fyne.Size) (h, eyebrow float32) {
	h = dialogHeadV + title.Height + dialogHeadGap
	if l.eyebrow != nil && l.eyebrow.Visible() {
		eyebrow = l.eyebrow.MinSize().Height + 6
		h += eyebrow - dialogHeadGap + 4
	}
	return h, eyebrow
}

// footer is the footer's content height and the width the note and the buttons need side by side.
func (l dialogFrameLayout) footer(buttons fyne.Size) (w, h float32) {
	w, h = buttons.Width, buttons.Height
	if l.note != nil && l.note.Visible() {
		n := l.note.MinSize()
		w += n.Width + 2*dialogSide
		h = max(h, n.Height)
	}
	return w, h
}

func (l dialogFrameLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	gap := l.pads()
	content, buttons, title := objs[1].MinSize(), objs[2].MinSize(), objs[3].MinSize()
	head, _ := l.head(title)
	foot, footH := l.footer(buttons)
	w := max(content.Width, foot, title.Width) + 2*dialogSide
	return fyne.NewSize(w, head+1+dialogBodyGap+content.Height+gap+1+dialogFootV+footH+dialogFootV)
}

func (l dialogFrameLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	gap := l.pads()
	bg, content, buttons, title, eyebrow, headRule, footRule, note, tips := objs[0], objs[1], objs[2], objs[3], objs[4], objs[5], objs[6], objs[7], objs[8]
	bg.Move(fyne.Position{})
	bg.Resize(size)
	titleMin := title.MinSize()
	head, eyebrowH := l.head(titleMin)
	y := float32(dialogHeadV)
	if eyebrow.Visible() {
		eyebrow.Move(fyne.NewPos(dialogSide, y))
		eyebrow.Resize(fyne.NewSize(size.Width-2*dialogSide, eyebrowH-6))
		y += eyebrowH
	}
	title.Move(fyne.NewPos(dialogSide-theme.Padding(), y)) // a rich text carries a padding of its own
	title.Resize(fyne.NewSize(size.Width-2*dialogSide, titleMin.Height))
	headRule.Move(fyne.NewPos(0, head))
	headRule.Resize(fyne.NewSize(size.Width, 1))
	if eyebrow.Visible() {
		headRule.Hide()
	} else {
		headRule.Show()
	}
	btnMin := buttons.MinSize()
	_, footH := l.footer(btnMin)
	buttons.Resize(btnMin)
	buttons.Move(fyne.NewPos(size.Width-dialogSide-btnMin.Width, size.Height-dialogFootV-footH+(footH-btnMin.Height)/2))
	if note.Visible() {
		nm := note.MinSize()
		note.Resize(nm)
		note.Move(fyne.NewPos(dialogSide, size.Height-dialogFootV-footH+(footH-nm.Height)/2))
	}
	footRule.Move(fyne.NewPos(0, size.Height-dialogFootV-footH-dialogFootV-1))
	footRule.Resize(fyne.NewSize(size.Width, 1))
	top := head + 1 + dialogBodyGap
	content.Move(fyne.NewPos(dialogSide, top))
	content.Resize(fyne.NewSize(size.Width-2*dialogSide, max(footRule.Position().Y-gap-top, 0)))
	tips.Move(fyne.Position{})
	tips.Resize(size)
}

// sentenceCase writes "CLUSTER ROLE" as "Cluster role": a field's caption is an ordinary label, and
// lower case belongs to bracket labels only (design-language.md).
func sentenceCase(text string) string {
	r := []rune(strings.ToLower(text))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
