package fyneui

import (
	"errors"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// createButton is the "+" that creates an object in the open list (Lens's was a round button over
// the rows, captured 2026-10-11; here it is a 28 point chip in the dock's strip, so that it never
// covers a cell):
// it opens a Create resource tab in the dock, starting from the template of the view's kind when
// Cogpit has one (Cogpit's choice; what Lens prefills is not captured), else the example Pod. On
// the views whose kind has a create dialog (Namespaces, Service Accounts, the four RBAC kinds,
// Resource Quotas: createdialog.go) it opens that dialog instead, as Lens does (inferred).
type createButton struct {
	widget.BaseWidget
	sh      *shell
	hovered bool

	plate *canvas.Rectangle
	icon  *iconImage
}

func newCreateButton(sh *shell) *createButton {
	b := &createButton{sh: sh, plate: canvas.NewRectangle(color.Transparent), icon: newIconImage()}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

// floating places the button at the bottom right of the area it covers. The container handles no
// input itself, so the table below it keeps every click outside the button.
func (b *createButton) floating() *fyne.Container { return box(floatLayout{}, b) }

// update shows the button while a cluster is connected and a view is open, except on the views
// of local and virtual kinds (Port Forwarding, Helm Releases): no manifest creates their objects,
// so a Create resource tab would only offer to create something else (owner, 2026-10-12).
func (b *createButton) update() {
	ui := b.sh.ui
	if ui.Connection.State.Live() && ui.View != "" && !b.sh.cogpitKind(ui.View) {
		b.Show()
	} else {
		b.Hide()
	}
}

// cogpitKind reports whether kinds.list marks a kind key local or virtual.
func (sh *shell) cogpitKind(key string) bool {
	for _, k := range sh.kinds {
		if k.Kind == key {
			return k.Local || k.Virtual
		}
	}
	return false
}

// Tapped implements fyne.Tappable.
func (b *createButton) Tapped(*fyne.PointEvent) {
	if !b.sh.openCreateDialog(b.sh.ui.View) {
		b.sh.openCreateTab()
	}
}

// Cursor implements desktop.Cursorable.
func (b *createButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (b *createButton) MouseIn(*desktop.MouseEvent) { b.hovered = true; b.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (b *createButton) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (b *createButton) MouseOut() { b.hovered = false; b.Refresh() }

// CreateRenderer implements fyne.Widget.
func (b *createButton) CreateRenderer() fyne.WidgetRenderer {
	r := &createButtonRenderer{b: b}
	r.Refresh()
	return r
}

type createButtonRenderer struct{ b *createButton }

// createButtonSide is the chip's side in points.
const createButtonSide = 28

func (r *createButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(createButtonSide, createButtonSide)
}

func (r *createButtonRenderer) Layout(s fyne.Size) {
	b := r.b
	d := fyne.Min(s.Width, s.Height)
	b.plate.Move(fyne.NewPos(0, 0))
	b.plate.Resize(fyne.NewSize(d, d))
	icon := b.Theme().Size(theme.SizeNameInlineIcon)
	b.icon.img.Move(fyne.NewPos((d-icon)/2, (d-icon)/2))
	b.icon.img.Resize(fyne.NewSize(icon, icon))
}

func (r *createButtonRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	b.plate.CornerRadius = th.Size(theme.SizeNameInputRadius)
	b.plate.FillColor = themeColor(th, colorChip)
	b.plate.StrokeColor, b.plate.StrokeWidth = themeColor(th, colorBracket), 1
	glyph := fyne.ThemeColorName(colorAccent)
	if b.hovered {
		b.plate.FillColor = themeColor(th, theme.ColorNameHover)
		b.plate.StrokeColor = withAlpha(themeColor(th, colorAccent), 0xa0)
	}
	b.icon.set(th, theme.ContentAddIcon(), glyph)
	r.Layout(b.Size())
	b.plate.Refresh()
}

func (r *createButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.plate, r.b.icon.img}
}

func (r *createButtonRenderer) Destroy() {}

// floatLayout puts its objects at their minimum size in the bottom-right corner, inset by the
// theme's padding.
type floatLayout struct{}

func (floatLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (floatLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding) * 5
	for _, o := range objs {
		ms := o.MinSize()
		o.Move(fyne.NewPos(size.Width-ms.Width-pad, size.Height-ms.Height-pad))
		o.Resize(ms)
	}
}

// openCreateTab opens a Create resource tab, from the template of the open view's kind when
// Cogpit has one.
func (sh *shell) openCreateTab() {
	template := ""
	for _, k := range sh.kinds {
		if k.Kind == sh.ui.View {
			template = k.APIKind
		}
	}
	sh.dispatch("resource.createTab", createTabParams{Template: template}, func(_ any, err error) {
		var dis *engine.DisabledError
		switch {
		case err == nil:
		case template != "" && !errors.As(err, &dis):
			sh.dispatch("resource.createTab", createTabParams{}, nil) // no template for this kind
		default:
			sh.showError(err)
		}
	})
}
