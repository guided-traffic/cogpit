package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// welcomeNote is the welcome page's message about the connection in a place of its own (the
// owner's RW4 of 2026-10-23: "Der Fehlertext auf dieser Seite geht etwas unter, man kann ihn leicht
// übersehen weil er so dazwischen gequetscht ist. Die Farbe ist es nicht. Also wenn Welcome eine
// Fehlermeldung anzeigt, braucht die meldung auch einen angemessenen platz um als Info, Warning
// oder Error eindeutig erkannt zu werden."; his pick of three rendered looks, variant C): a banner
// between the tagline and the clusters, with room above and below, on a tint of its level's colour
// and outlined in it, the level's icon and a bold title in that colour over the text. The level
// shows in the word, the colour, the icon and the plate at once.
type welcomeNote struct {
	root  *fyne.Container
	plate *notePlate
	icon  *widget.Icon
	title *textLine
	text  *paragraph
	level string
}

// Levels of a welcome note.
const (
	noteInfo    = "info"
	noteWarning = "warning"
	noteError   = "error"
)

func newWelcomeNote() *welcomeNote {
	n := &welcomeNote{icon: widget.NewIcon(nil), plate: newNotePlate()}
	n.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	n.title.truncate = true
	n.text = newParagraph("", theme.ColorNameForeground, welcomeColumn)
	body := container.NewStack(n.plate, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		container.NewBorder(nil, nil, container.NewVBox(n.icon), nil, container.NewVBox(n.title, n.text))))
	n.root = container.NewVBox(newSpace(theme.SizeNameInnerPadding), body, newSpace(theme.SizeNameInnerPadding))
	n.root.Hide()
	return n
}

// noteColor is a level's colour and icon.
func noteColor(level string) (fyne.ThemeColorName, fyne.Resource) {
	switch level {
	case noteError:
		return colorStatusError, theme.ErrorIcon()
	case noteWarning:
		return colorStatusWarning, theme.WarningIcon()
	}
	return colorAccent, theme.InfoIcon()
}

// set shows title and text at level; an empty title hides the note.
func (n *welcomeNote) set(level, title, text string) {
	if title == "" {
		n.root.Hide()
		n.title.SetText("")
		return
	}
	if level != n.level {
		n.level = level
		c, icon := noteColor(level)
		n.icon.SetResource(themedIcon(icon, c))
		n.title.SetColor(c)
		n.plate.color = c
		n.plate.Refresh()
	}
	n.title.SetText(title)
	th := fyne.CurrentApp().Settings().Theme()
	width := welcomeColumn - th.Size(theme.SizeNameInlineIcon) - 3*th.Size(theme.SizeNameInnerPadding) - th.Size(theme.SizeNamePadding)
	if text != n.text.text || width != n.text.width {
		n.text.text, n.text.width = text, width
		n.text.Refresh()
	}
	setVisible(n.text, text != "")
	n.root.Show()
}

// notePlate is a welcome note's plate: a tint of its colour, outlined in it, the dialogs' corners.
type notePlate struct {
	widget.BaseWidget
	color fyne.ThemeColorName
}

func newNotePlate() *notePlate {
	p := &notePlate{color: colorAccent}
	p.ExtendBaseWidget(p)
	return p
}

// CreateRenderer implements fyne.Widget.
func (p *notePlate) CreateRenderer() fyne.WidgetRenderer {
	r := &notePlateRenderer{p: p, rect: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type notePlateRenderer struct {
	p    *notePlate
	rect *canvas.Rectangle
}

func (r *notePlateRenderer) Layout(size fyne.Size) { r.rect.Resize(size) }
func (r *notePlateRenderer) MinSize() fyne.Size    { return fyne.Size{} }

func (r *notePlateRenderer) Refresh() {
	th := r.p.Theme()
	c := themeColor(th, r.p.color)
	r.rect.FillColor = withAlpha(c, 0x24) // the banners' tint (bannerTint)
	r.rect.StrokeColor, r.rect.StrokeWidth = c, th.Size(theme.SizeNameInputBorder)
	r.rect.CornerRadius = th.Size(theme.SizeNameDialogRadius)
	r.rect.Refresh()
}

func (r *notePlateRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect} }
func (r *notePlateRenderer) Destroy()                     {}
