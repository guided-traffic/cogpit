package fyneui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's icon in Cluster Settings › General (the owner's VW5 of 2026-10-23, D-119): the user
// chooses the Internal icon, a colour tile with up to five letters of the user's, or the Custom
// icon, a square part of an image the user places; each is customised in a dialog of its own
// (Customize…). The tile's dialog has the letters and the colours; the image's has a square frame
// the image is moved in by dragging and zoomed in with the mouse wheel (at the pointer) or the
// slider, as the owner's teddycloud-cover-printer does, the image always filling the frame. Both
// dialogs preview the icon at the hotbar's and the navigator's sizes; the image's preview is cut by
// the engine's own CutIcon, so it is the icon that is kept. Lens's "…" menu (Upload Icon, Pick Icon
// Color, Reset Icon) gave way to this by the owner's direction; Reset stays beside Customize….

// The two kinds of icon, as the choice names them, and the setting that switches between them.
const (
	iconKindTile  = "Internal icon"
	iconKindImage = "Custom icon"
	iconTileParam = "iconTile"
)

// fitTextSize is the largest text size, at most base, at which text in bold fits width: a tile's
// letters, up to five of the user's (D-119), drawn smaller where the default size does not fit.
func fitTextSize(text string, base, width float32) float32 {
	w := fyne.MeasureText(text, base, fyne.TextStyle{Bold: true}).Width
	if w <= width || w <= 0 {
		return base
	}
	return max(base*width/w, 1) // a text's width grows with its size
}

// iconSetting is General › Icon: the icon, the choice of its kind, Customize… and Reset.
func (p *settingsPage) iconSetting(ctx string) (fyne.CanvasObject, func()) {
	preview := newClusterBadge()
	preview.side = sizeHotbarTile
	p.controls["iconPreview"] = preview
	kind := widget.NewRadioGroup([]string{iconKindTile, iconKindImage}, nil)
	kind.Horizontal, kind.Required = true, true
	p.controls["iconKind"] = kind
	customize := newPlateButton("Customize…", nil, nil)
	p.controls["iconCustomize"] = customize
	reset := newPlateButton("Reset", nil, func() {
		p.sh.dispatch("cluster.settings.resetIcon", clusterSettingsContext{Context: ctx}, func(_ any, err error) { p.showResult("icon", err) })
	})
	p.controls["iconReset"] = reset
	kind.OnChanged = func(sel string) {
		if p.syncing {
			return
		}
		cs := p.sh.ui.ClusterSettings[ctx]
		switch {
		case sel == iconKindImage && cs.Icon == "":
			p.openImageDialog(ctx) // nothing to show yet: the kind follows once an image is cut
		case sel == iconKindImage:
			p.setIcon(map[string]any{iconTileParam: false})
		case cs.Icon != "" && !cs.IconTile:
			p.setIcon(map[string]any{iconTileParam: true})
		}
	}
	customize.OnTapped = func() {
		if kind.Selected == iconKindImage {
			p.openImageDialog(ctx)
			return
		}
		p.openTileDialog(ctx)
	}
	sync := func() {
		cs := p.sh.ui.ClusterSettings[ctx] // the UI state's, so the choice follows at once
		preview.setIdentity(p.sh.identity(ctx))
		sel := iconKindTile
		if cs.Icon != "" && !cs.IconTile {
			sel = iconKindImage
		}
		kind.SetSelected(sel)
		if cs.Icon == "" && cs.IconColor == "" && cs.IconLetters == "" {
			reset.Disable()
		} else {
			reset.Enable()
		}
	}
	row := container.NewHBox(preview, newHGap(theme.SizeNameInnerPadding), container.NewCenter(kind),
		newHGap(theme.SizeNameInnerPadding), container.NewCenter(customize), container.NewCenter(reset))
	return p.setting("icon", "Icon", "Internal: up to five letters of yours on a colour. Custom: a part of an image you place; "+
		"Cogpit keeps its own copy. Customize… changes the one chosen.", row), sync
}

// setIcon sets icon fields of the shown cluster's settings at once.
func (p *settingsPage) setIcon(params map[string]any) {
	if p.syncing {
		return
	}
	params[contextParam] = p.shown.Context
	p.sh.dispatchInOrder("cluster.settings.set", params, p.settled("icon"))
}

// iconPreviews are an icon at the hotbar's size and at the navigator's, side by side, each over
// its place's name.
func iconPreviews() (big, small *clusterBadge, row fyne.CanvasObject) {
	big, small = newClusterBadge(), newClusterBadge()
	big.side = sizeHotbarTile
	column := func(b *clusterBadge, place string) fyne.CanvasObject {
		return container.NewVBox(sized(sizeHotbarTile, sizeHotbarTile, container.NewCenter(b)),
			container.NewCenter(newTextLine(place, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})))
	}
	return big, small, container.NewHBox(column(big, "Hotbar"), newHGap(theme.SizeNameInnerPadding), column(small, "Navigator"))
}

// tileDialog is the Internal icon's dialog: the letters and the colour.
type tileDialog struct {
	d        *dialogFrame
	letters  *fieldEntry
	problem  *textLine
	color    string // "" the accent, a name of engine.IconColors, or #rrggbb
	swatches []*swatch
	custom   *swatch
	big      *clusterBadge
	small    *clusterBadge
	save     *textButton
}

// openTileDialog opens the Internal icon's dialog for ctx.
func (p *settingsPage) openTileDialog(ctx string) *tileDialog {
	sh := p.sh
	cs := sh.ui.ClusterSettings[ctx]
	name := engine.ClusterName(ctx, cs)
	auto := nameInitials(name)
	td := &tileDialog{color: cs.IconColor}
	td.letters = newFieldEntry()
	td.letters.SetPlaceHolder(auto)
	td.letters.SetText(cs.IconLetters)
	td.problem = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	td.problem.Hide()
	var previews fyne.CanvasObject
	td.big, td.small, previews = iconPreviews()
	show := func() {
		letters := strings.TrimSpace(td.letters.Text)
		if err := engine.CheckIconLetters(letters); err != nil {
			td.problem.SetText(err.Error())
			td.problem.Show()
			td.save.Disable()
		} else {
			td.problem.Hide()
			td.save.Enable()
		}
		if letters == "" {
			letters = auto
		}
		id := clusterIdentity{name: name, initials: letters, ink: colorAccent}
		if td.color != "" {
			id.ink = identityColorName(td.color)
		}
		td.big.setIdentity(id)
		td.small.setIdentity(id)
		for i, s := range td.swatches {
			s.setSelected(td.color == engine.IconColors[i])
		}
		if engine.IsCustomIconColor(td.color) {
			td.custom.color = td.color
			td.custom.Show()
			td.custom.setSelected(true)
			td.custom.Refresh()
		} else {
			td.custom.Hide()
		}
	}
	setColor := func(c string) {
		td.color = c
		show()
	}
	colors := container.NewHBox()
	for _, c := range engine.IconColors {
		col := c
		s := newSwatch(sh, col, func() { setColor(col) })
		td.swatches = append(td.swatches, s)
		colors.Add(s)
	}
	td.custom = newSwatch(sh, "#000000", func() {})
	td.custom.Hide()
	colors.Add(td.custom)
	colors.Add(newTipButton(sh, theme.ColorPaletteIcon(), "Pick any colour", func() {
		p.pickColor(iconPickerTitle, setColor)
	}))
	td.letters.OnChanged = func(string) { show() }

	cancel := newPlateButton("Cancel", nil, nil)
	td.save = newPlateButton("Save", nil, nil)
	td.save.Importance = widget.HighImportance
	help := newParagraph("Up to five letters, digits or signs; empty: the initials of the cluster's name. "+
		"Longer letters are drawn smaller to fit.", colorMuted, 360)
	help.size = theme.SizeNameCaptionText
	content := container.NewVBox(
		newTextLine("Letters", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}), fixedHeight(6),
		td.letters, fixedHeight(4), help, td.problem, fixedHeight(18),
		newTextLine("Colour", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}), fixedHeight(6), colors,
		fixedHeight(18), previews)
	td.d = newDialogFrame(sh, objectDialogTitle("", "Internal Icon", name, ""))
	td.d.setContent(content)
	td.d.SetButtons([]fyne.CanvasObject{cancel, td.save})
	cancel.OnTapped = td.d.Hide
	td.save.OnTapped = func() {
		letters := strings.TrimSpace(td.letters.Text)
		if engine.CheckIconLetters(letters) != nil {
			return
		}
		td.d.Hide()
		params := map[string]any{"iconLetters": letters, "iconColor": td.color, iconTileParam: true}
		params[contextParam] = ctx
		sh.dispatchInOrder("cluster.settings.set", params, p.settled("icon"))
	}
	td.d.SetOnClosed(p.syncControls) // the choice shows the kept kind again
	show()
	td.d.Resize(fyne.NewSize(440, td.d.MinSize().Height))
	td.d.Show()
	p.tileDialog = td
	return td
}

// The image dialog's sizes: the editor's area and the square frame in its middle, in points.
const (
	cropAreaWidth  = 360
	cropAreaHeight = 300
	cropFrame      = 240
	// cropPreviewDelay is the pause after a move or a zoom before the previews are cut again.
	cropPreviewDelay = 80 * time.Millisecond
)

// imageDialog is the Custom icon's dialog: the image in the square frame, its zoom, the previews.
type imageDialog struct {
	p       *settingsPage
	ctx     string
	d       *dialogFrame
	editor  *cropEditor
	zoom    *fieldSlider
	big     *clusterBadge
	small   *clusterBadge
	save    *textButton
	problem *textLine
	path    string // a file picked in the dialog; "" the source kept
	gen     int    // the previews' newest cut
	timer   *time.Timer
	saved   bool
	sliding bool // the slider follows the editor's zoom: its change is not a zoom of its own
}

// openImageDialog opens the Custom icon's dialog for ctx: the image kept, where it was cut; without
// one the system's file panel opens at once.
func (p *settingsPage) openImageDialog(ctx string) *imageDialog {
	sh := p.sh
	name := engine.ClusterName(ctx, sh.ui.ClusterSettings[ctx])
	id := &imageDialog{p: p, ctx: ctx}
	id.editor = newCropEditor(func(engine.IconCrop) { id.changed() })
	id.zoom = newFieldSlider(1, engine.MaxIconZoom)
	id.zoom.Step = 0.01
	id.zoom.OnChanged = func(z float64) {
		if id.sliding || id.editor.src == nil || math.Abs(z-id.editor.crop.Zoom) < 1e-6 {
			return
		}
		id.editor.zoomAt(z, fyne.Position{})
	}
	id.problem = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	id.problem.Hide()
	var previews fyne.CanvasObject
	id.big, id.small, previews = iconPreviews()
	choose := newPlateButton("Choose Image…", theme.FolderOpenIcon(), func() { id.choose() })
	fit := newPlateButton("Fit", nil, func() { id.editor.setCrop(engine.DefaultIconCrop) })
	id.editor.onEmpty = id.choose

	zoomRow := container.NewBorder(nil, nil, newTextLine("Zoom", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}),
		nil, themedSlider(id.zoom)) // its track drawn on the card, as Scale's (stage_dialog.go)
	help := newParagraph("Drag the image to move it; the mouse wheel or the slider zooms. The square is the icon.", colorMuted, cropAreaWidth)
	help.size = theme.SizeNameCaptionText
	content := container.NewVBox(
		container.NewCenter(id.editor), fixedHeight(6), help, fixedHeight(10), zoomRow,
		container.NewHBox(choose, fit), id.problem, fixedHeight(14), previews)
	cancel := newPlateButton("Cancel", nil, nil)
	id.save = newPlateButton("Save", nil, nil)
	id.save.Importance = widget.HighImportance
	id.save.Disable()
	id.d = newDialogFrame(sh, objectDialogTitle("", "Custom Icon", name, ""))
	id.d.setContent(content)
	id.d.SetButtons([]fyne.CanvasObject{cancel, id.save})
	cancel.OnTapped = id.d.Hide
	id.save.OnTapped = id.saveCut
	id.d.SetOnClosed(func() {
		if id.timer != nil {
			id.timer.Stop()
		}
		id.gen++
		p.syncControls() // a choice of Custom without an image cut goes back
	})
	id.d.Resize(fyne.NewSize(cropAreaWidth+80, id.d.MinSize().Height))
	id.d.Show()
	p.imageDialog = id
	id.load()
	return id
}

// load shows the image kept for the cluster, read off the main goroutine; without one the file
// panel opens.
func (id *imageDialog) load() {
	sh := id.p.sh
	if sh.sess == nil {
		id.choose()
		return
	}
	go func() {
		defer sh.recoverTo("cluster icon source")
		src, crop, err := sh.sess.ClusterIconSource(id.ctx)
		sh.do(func() {
			switch {
			case err != nil:
				id.showProblem(err)
			case src == nil:
				id.choose()
			default:
				id.show(src, crop, "")
			}
		})
	}()
}

// choose asks for an image file and shows it, filling the frame, its middle in the square.
func (id *imageDialog) choose() {
	if !id.d.shown {
		return
	}
	id.p.pickIcon(func(path string) {
		sh := id.p.sh
		go func() {
			defer sh.recoverTo("cluster icon source")
			src, _, err := engine.ReadIconSource(path)
			sh.do(func() {
				if err != nil {
					id.showProblem(err)
					return
				}
				id.show(src, engine.DefaultIconCrop, path)
			})
		}()
	})
}

// show puts an image in the editor at a cut.
func (id *imageDialog) show(src image.Image, crop engine.IconCrop, path string) {
	if !id.d.shown {
		return
	}
	id.path = path
	id.problem.Hide()
	id.editor.setSource(src, crop)
	id.save.Enable()
}

func (id *imageDialog) showProblem(err error) {
	id.problem.SetText(err.Error())
	id.problem.Show()
	id.d.fit()
}

// changed follows a move or a zoom: the slider at once, the previews after a pause.
func (id *imageDialog) changed() {
	crop := id.editor.crop
	if math.Abs(id.zoom.Value-crop.Zoom) > 1e-6 {
		id.sliding = true
		id.zoom.SetValue(crop.Zoom)
		id.sliding = false
	}
	id.gen++
	gen, src := id.gen, id.editor.src
	if id.timer != nil {
		id.timer.Stop()
	}
	sh := id.p.sh
	id.timer = time.AfterFunc(cropPreviewDelay, func() {
		defer sh.recoverTo("cluster icon preview")
		cut := engine.CutIcon(src, crop, 128)
		sh.do(func() {
			if gen != id.gen {
				return
			}
			res := pngResource(fmt.Sprintf("icon-preview-%d.png", gen), cut)
			preview := clusterIdentity{initials: "", ink: colorAccent, image: res}
			id.big.setIdentity(preview)
			id.small.setIdentity(preview)
		})
	})
}

// saveCut keeps the cut: a new image picked here is uploaded with it, else the kept one is cut
// again.
func (id *imageDialog) saveCut() {
	if id.editor.src == nil {
		return
	}
	crop := id.editor.crop
	sh := id.p.sh
	id.saved = true
	id.d.Hide()
	done := func(_ any, err error) {
		id.p.showResult("icon", err)
		if err == nil {
			id.p.stateChanged()
		}
	}
	if id.path != "" {
		sh.dispatch("cluster.settings.uploadIcon", clusterIconCutParams{Context: id.ctx, Path: id.path,
			Zoom: crop.Zoom, X: crop.X, Y: crop.Y}, done)
		return
	}
	sh.dispatch("cluster.settings.cutIcon", clusterIconCutParams{Context: id.ctx, Zoom: crop.Zoom, X: crop.X, Y: crop.Y}, done)
}

// pngResource is an image as a PNG resource named name; nil when it cannot be encoded.
func pngResource(name string, img image.Image) fyne.Resource {
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		return nil
	}
	return fyne.NewStaticResource(name, b.Bytes())
}

// clusterIconCutParams mirror cluster.settings.uploadIcon's and cluster.settings.cutIcon's.
type clusterIconCutParams struct {
	Context string  `json:"context,omitempty"`
	Path    string  `json:"path,omitempty"`
	Zoom    float64 `json:"zoom"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

// cropEditor shows an image in the square frame it is cut by, the rest of it dimmed, and moves it
// by dragging and zooms it with the mouse wheel at the pointer (the owner's crop tool, VW5). The
// image always fills the frame (engine.IconCrop.Clamp). A scrollable widget, it clips what it draws
// to its own size.
type cropEditor struct {
	widget.BaseWidget
	src      image.Image
	srcW     float64
	srcH     float64
	crop     engine.IconCrop
	img      *canvas.Image
	onChange func(engine.IconCrop)
	onEmpty  func() // a tap while no image is shown
}

var (
	_ fyne.Draggable  = (*cropEditor)(nil)
	_ fyne.Scrollable = (*cropEditor)(nil)
	_ fyne.Tappable   = (*cropEditor)(nil)
)

func newCropEditor(onChange func(engine.IconCrop)) *cropEditor {
	e := &cropEditor{crop: engine.DefaultIconCrop, onChange: onChange,
		img: &canvas.Image{FillMode: canvas.ImageFillStretch, ScaleMode: canvas.ImageScaleSmooth}}
	e.ExtendBaseWidget(e)
	return e
}

// setSource shows src at crop.
func (e *cropEditor) setSource(src image.Image, crop engine.IconCrop) {
	e.src = src
	b := src.Bounds()
	e.srcW, e.srcH = float64(b.Dx()), float64(b.Dy())
	e.img.Image = src
	e.img.Refresh()
	e.setCrop(crop)
}

// setCrop moves the image to crop, clamped.
func (e *cropEditor) setCrop(crop engine.IconCrop) {
	if e.src == nil {
		return
	}
	e.crop = crop.Clamp(int(e.srcW), int(e.srcH))
	e.Refresh()
	if e.onChange != nil {
		e.onChange(e.crop)
	}
}

// scale is the image's points per source pixel at the current zoom.
func (e *cropEditor) scale() float64 {
	return cropFrame / math.Min(e.srcW, e.srcH) * e.crop.Zoom
}

// frameRect is the square frame's place in the editor.
func (e *cropEditor) frameRect() (fyne.Position, fyne.Size) {
	size := e.Size()
	return fyne.NewPos((size.Width-cropFrame)/2, (size.Height-cropFrame)/2), fyne.NewSize(cropFrame, cropFrame)
}

// Dragged implements fyne.Draggable: the image moves with the pointer.
func (e *cropEditor) Dragged(ev *fyne.DragEvent) {
	if e.src == nil {
		return
	}
	s := e.scale()
	c := e.crop
	c.X -= float64(ev.Dragged.DX) / (e.srcW * s)
	c.Y -= float64(ev.Dragged.DY) / (e.srcH * s)
	e.setCrop(c)
}

// DragEnd implements fyne.Draggable.
func (e *cropEditor) DragEnd() {}

// Scrolled implements fyne.Scrollable: the wheel zooms at the pointer, a tenth per notch, as the
// owner's tool does; a trackpad's small steps zoom by as much less.
func (e *cropEditor) Scrolled(ev *fyne.ScrollEvent) {
	if e.src == nil || ev.Scrolled.DY == 0 {
		return
	}
	pos, size := e.frameRect()
	at := ev.Position.Subtract(pos.AddXY(size.Width/2, size.Height/2)) // from the frame's centre
	e.zoomAt(e.crop.Zoom*math.Pow(1.1, float64(ev.Scrolled.DY)/10), at)
}

// zoomAt zooms to z keeping the image's point at at (from the frame's centre) where it is.
func (e *cropEditor) zoomAt(z float64, at fyne.Position) {
	if e.src == nil {
		return
	}
	s := e.scale()
	qx := e.crop.X*e.srcW + float64(at.X)/s // the source point under at
	qy := e.crop.Y*e.srcH + float64(at.Y)/s
	c := e.crop
	c.Zoom = min(max(z, 1), engine.MaxIconZoom)
	s2 := cropFrame / math.Min(e.srcW, e.srcH) * c.Zoom
	c.X = (qx - float64(at.X)/s2) / e.srcW
	c.Y = (qy - float64(at.Y)/s2) / e.srcH
	e.setCrop(c)
}

// Tapped implements fyne.Tappable: without an image a tap asks for one.
func (e *cropEditor) Tapped(*fyne.PointEvent) {
	if e.src == nil && e.onEmpty != nil {
		e.onEmpty()
	}
}

// Cursor implements desktop.Cursorable.
func (e *cropEditor) Cursor() desktop.Cursor {
	if e.src == nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// CreateRenderer implements fyne.Widget.
func (e *cropEditor) CreateRenderer() fyne.WidgetRenderer {
	r := &cropEditorRenderer{e: e, bg: canvas.NewRectangle(color.Transparent), frame: canvas.NewRectangle(color.Transparent),
		empty: newTextLine("Choose an image…", colorMuted, theme.SizeNameText, fyne.TextStyle{})}
	for i := range r.shade {
		r.shade[i] = canvas.NewRectangle(color.Transparent)
	}
	r.Refresh()
	return r
}

type cropEditorRenderer struct {
	e     *cropEditor
	bg    *canvas.Rectangle
	shade [4]*canvas.Rectangle // the dimmed image around the frame: above, below, left, right
	frame *canvas.Rectangle
	empty *textLine
}

func (r *cropEditorRenderer) MinSize() fyne.Size { return fyne.NewSize(cropAreaWidth, cropAreaHeight) }

func (r *cropEditorRenderer) Layout(size fyne.Size) {
	e := r.e
	r.bg.Resize(size)
	pos, fs := e.frameRect()
	r.frame.Move(pos)
	r.frame.Resize(fs)
	r.shade[0].Move(fyne.Position{})
	r.shade[0].Resize(fyne.NewSize(size.Width, pos.Y))
	r.shade[1].Move(fyne.NewPos(0, pos.Y+fs.Height))
	r.shade[1].Resize(fyne.NewSize(size.Width, size.Height-pos.Y-fs.Height))
	r.shade[2].Move(fyne.NewPos(0, pos.Y))
	r.shade[2].Resize(fyne.NewSize(pos.X, fs.Height))
	r.shade[3].Move(fyne.NewPos(pos.X+fs.Width, pos.Y))
	r.shade[3].Resize(fyne.NewSize(size.Width-pos.X-fs.Width, fs.Height))
	es := r.empty.MinSize()
	r.empty.Move(fyne.NewPos((size.Width-es.Width)/2, (size.Height-es.Height)/2))
	r.empty.Resize(es)
	if e.src == nil {
		return
	}
	s := e.scale()
	w, h := float32(e.srcW*s), float32(e.srcH*s)
	cx, cy := pos.X+fs.Width/2, pos.Y+fs.Height/2
	e.img.Move(fyne.NewPos(cx-float32(e.crop.X*e.srcW*s), cy-float32(e.crop.Y*e.srcH*s)))
	e.img.Resize(fyne.NewSize(w, h))
}

func (r *cropEditorRenderer) Refresh() {
	e := r.e
	th := e.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	r.bg.FillColor = th.Color(theme.ColorNameInputBackground, v)
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	for _, s := range r.shade {
		s.FillColor = th.Color(theme.ColorNameShadow, v)
		s.Refresh()
	}
	r.frame.FillColor = color.Transparent
	r.frame.StrokeColor = th.Color(theme.ColorNameForeground, v)
	r.frame.StrokeWidth = th.Size(theme.SizeNameInputBorder)
	r.frame.CornerRadius = th.Size(theme.SizeNameInputRadius)
	if e.src == nil {
		e.img.Hide()
		r.empty.Show()
	} else {
		e.img.Show()
		r.empty.Hide()
	}
	r.bg.Refresh()
	r.frame.Refresh()
	r.Layout(e.Size())
	canvas.Refresh(e.img)
}

func (r *cropEditorRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg, r.e.img}
	for _, s := range r.shade {
		objs = append(objs, s)
	}
	return append(objs, r.frame, r.empty)
}

func (r *cropEditorRenderer) Destroy() {}
