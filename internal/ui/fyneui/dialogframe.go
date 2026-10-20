package fyneui

import (
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
	frame   *fyne.Container // background, content, buttons, title, tooltips
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
	d.frame = box(dialogFrameLayout{}, bg, container.NewStack(), container.NewGridWithRows(1), title, d.tip.layer)
	d.popup = widget.NewModalPopUp(d.frame, sh.win.Canvas())
	return d
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
	return titleText(&widget.TextSegment{Text: text, Style: widget.RichTextStyleStrong})
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
	head := &widget.TextSegment{Text: lead, Style: widget.RichTextStyleStrong}
	head.Style.Inline = true
	return titleText(head, &widget.TextSegment{Text: " " + name + tail, Style: widget.RichTextStyleStrong})
}

// titleText is a title's rich text: as wide as its text, as Fyne's dialog title is.
func titleText(segs ...widget.RichTextSegment) *widget.RichText { return widget.NewRichText(segs...) }

// titleString is a dialog title's text, for tests and logs.
func titleString(t *widget.RichText) string { return t.String() }

// dialogFrameLayout places a dialogFrame's objects as Fyne's dialogLayout does: the background
// under everything, the title at the top left in its own padding, the content below it inset by
// half the side padding, the buttons centred at the bottom, the tooltips' layer over all of it.
type dialogFrameLayout struct{}

func (dialogFrameLayout) pads() (x, y, gap float32) {
	th := fyne.CurrentApp().Settings().Theme()
	return th.Size(sizeDialogPadX), th.Size(sizeDialogPadY), th.Size(theme.SizeNamePadding)
}

func (l dialogFrameLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	padX, padY, gap := l.pads()
	content, buttons, title := objs[1].MinSize(), objs[2].MinSize(), objs[3].MinSize()
	return fyne.NewSize(max(content.Width, buttons.Width, title.Width)+padX,
		content.Height+buttons.Height+title.Height+gap+2*padY)
}

func (l dialogFrameLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	padX, padY, gap := l.pads()
	bg, content, buttons, title, tips := objs[0], objs[1], objs[2], objs[3], objs[4]
	bg.Move(fyne.Position{})
	bg.Resize(size)
	titleMin := title.MinSize()
	title.Move(fyne.Position{})
	title.Resize(fyne.NewSize(size.Width, titleMin.Height))
	btnMin := buttons.MinSize()
	buttons.Resize(btnMin)
	buttons.Move(fyne.NewPos((size.Width-btnMin.Width)/2, size.Height-padY-btnMin.Height))
	top := titleMin.Height + padY
	content.Move(fyne.NewPos(padX/2, top))
	content.Resize(fyne.NewSize(size.Width-padX, max(buttons.Position().Y-gap-top, 0)))
	tips.Move(fyne.Position{})
	tips.Resize(size)
}
