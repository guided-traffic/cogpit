package fyneui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Selecting and copying a toast's text, and a notification's in the bell's panel (the owner's
// WF41, D-99): the text is a selParagraph (a drag, a double click, ⌘C), and a copy icon (before a
// toast's ×, after a notification's time) copies the whole message as it came. The toast's own time stands still while
// the pointer is over it or one of its icons, and while its text holds a selection or a drag
// makes one; it runs again, in full, once neither is so.

// newToastText is a toast's text: selectable, and holding the toast while it is selected.
func (sh *shell) newToastText(card *toastCard, msg string, width float32) *selParagraph {
	t := newSelParagraph(newParagraph(msg, colorToastText, width), colorToastSelection, func(sel string) { sh.copyText(sel, false) })
	t.changed = func() { card.setSelecting(t.selecting()) }
	card.text = t
	return t
}

// setSelecting holds the toast while its text is selected or being selected, and lets its time run
// again in full when it no longer is.
func (c *toastCard) setSelecting(on bool) {
	c.mu.Lock()
	changed := c.selecting != on
	c.selecting = on
	c.mu.Unlock()
	switch {
	case !changed:
	case on:
		c.disarm()
	default:
		c.arm()
	}
}

// dropFocus gives up the keyboard when the toast's text has it: the toast is going.
func (c *toastCard) dropFocus(w fyne.Window) {
	if c.text != nil && w != nil && w.Canvas().Focused() == fyne.Focusable(c.text) {
		w.Canvas().Unfocus()
	}
}

// toastButton is a toast's ×: a button whose hover counts as the toast's, so that the pointer on
// it does not let the toast's time run.
type toastButton struct {
	widget.Button
	card *toastCard
}

// newToastClose is the small × of a toast.
func newToastClose(card *toastCard, fn func()) *toastButton {
	b := &toastButton{card: card}
	b.Icon, b.Importance, b.OnTapped = themedIcon(theme.CancelIcon(), colorToastText), widget.LowImportance, fn
	b.ExtendBaseWidget(b)
	return b
}

// MouseIn implements desktop.Hoverable.
func (b *toastButton) MouseIn(ev *desktop.MouseEvent) {
	b.Button.MouseIn(ev)
	b.card.MouseIn(ev)
}

// MouseOut implements desktop.Hoverable.
func (b *toastButton) MouseOut() {
	b.Button.MouseOut()
	b.card.MouseOut()
}

// copyGlyph is a small copy icon, a line of text high: before a toast's ×, after a notification's
// time. A click puts the message on the clipboard as it came, and the icon is a
// checkmark for the shell's copyConfirm. No toast says so: the message is a toast already, and a
// new one would push the toast under the pointer away (D-99).
type copyGlyph struct {
	widget.BaseWidget
	sh      *shell
	text    string
	color   fyne.ThemeColorName
	in, out func() // the pointer came onto the icon, left it; nil: nothing
	done    bool   // a copy just now: the checkmark
	timer   *time.Timer
}

var (
	_ fyne.Tappable      = (*copyGlyph)(nil)
	_ desktop.Hoverable  = (*copyGlyph)(nil)
	_ desktop.Cursorable = (*copyGlyph)(nil)
)

func newCopyGlyph(sh *shell, text string, c fyne.ThemeColorName) *copyGlyph {
	g := &copyGlyph{sh: sh, text: text, color: c}
	g.ExtendBaseWidget(g)
	return g
}

// Tapped implements fyne.Tappable.
func (g *copyGlyph) Tapped(*fyne.PointEvent) {
	g.sh.copyText(g.text, false)
	if g.timer != nil {
		g.timer.Stop()
	}
	g.done = true
	g.Refresh()
	var t *time.Timer
	t = time.AfterFunc(g.sh.copyConfirm, func() {
		g.sh.do(func() {
			if g.timer == t {
				g.timer, g.done = nil, false
				g.Refresh()
			}
		})
	})
	g.timer = t
}

// Cursor implements desktop.Cursorable.
func (g *copyGlyph) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (g *copyGlyph) MouseIn(*desktop.MouseEvent) {
	if g.in != nil {
		g.in()
	}
}

// MouseMoved implements desktop.Hoverable.
func (g *copyGlyph) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (g *copyGlyph) MouseOut() {
	if g.out != nil {
		g.out()
	}
}

// glyphSize is the icon's side: a line of text's height, so that it does not make its line taller.
func glyphSize(th fyne.Theme) float32 {
	return fyne.MeasureText("Mg", th.Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Height
}

// CreateRenderer implements fyne.Widget.
func (g *copyGlyph) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain
	r := &copyGlyphRenderer{g: g, img: img}
	r.Refresh()
	return r
}

type copyGlyphRenderer struct {
	g   *copyGlyph
	img *canvas.Image
}

func (r *copyGlyphRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(glyphSize(r.g.Theme())) }

func (r *copyGlyphRenderer) Layout(size fyne.Size) {
	inset := size.Height / 8 // the icon's own margin is small: keep it from the text's edges
	r.img.Move(fyne.NewPos(inset, inset))
	r.img.Resize(size.SubtractWidthHeight(2*inset, 2*inset))
}

func (r *copyGlyphRenderer) Refresh() {
	if r.g.done {
		r.img.Resource = themedIcon(theme.ConfirmIcon(), colorStatusOK)
	} else {
		r.img.Resource = themedIcon(theme.ContentCopyIcon(), r.g.color)
	}
	r.Layout(r.g.Size())
	r.img.Refresh()
}

func (r *copyGlyphRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *copyGlyphRenderer) Destroy()                     {}

// newToastCopy is a toast's copy icon: its hover counts as the toast's.
func (sh *shell) newToastCopy(card *toastCard, msg string) *copyGlyph {
	g := newCopyGlyph(sh, msg, colorToastText)
	g.in = func() { card.MouseIn(&desktop.MouseEvent{}) }
	g.out = card.MouseOut
	return g
}

// noteRow is a notification in the bell's panel: its copy icon shows while the pointer is over
// it, in a place kept for it, so that the time beside it does not move.
type noteRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	glyph   *copyGlyph
}

var _ desktop.Hoverable = (*noteRow)(nil)

// newNoteRow returns the row content, whose head holds glyph, showing glyph on hover.
func newNoteRow(content fyne.CanvasObject, glyph *copyGlyph) *noteRow {
	r := &noteRow{content: content, glyph: glyph}
	glyph.Hide()
	glyph.in = glyph.Show // from the row onto the icon: the row's MouseOut hid it a moment ago
	glyph.out = glyph.Hide
	r.ExtendBaseWidget(r)
	return r
}

// glyphSlot keeps the icon's room while it is hidden.
func glyphSlot(g *copyGlyph) fyne.CanvasObject {
	room := canvas.NewRectangle(color.Transparent)
	room.SetMinSize(g.MinSize())
	return container.NewStack(room, g)
}

// MouseIn implements desktop.Hoverable.
func (r *noteRow) MouseIn(*desktop.MouseEvent) { r.glyph.Show() }

// MouseMoved implements desktop.Hoverable.
func (r *noteRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *noteRow) MouseOut() { r.glyph.Hide() }

// CreateRenderer implements fyne.Widget.
func (r *noteRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.content) }
