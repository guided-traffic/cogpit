package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Data sections with each key on a line of its own and its value below it at the details' full
// width (the owner's F3, decided, after Lens's ConfigMap drawer, captured): a key such as
// "agent-not-ready-taint-key" no longer runs under its value in the 128 pt name column. A Config
// Map's Data puts its values in editors (configmapdata.go); a Secret's Data keeps its masked values
// with Show, into an editor where the user may save them (secretdata.go), and a Config Map's Binary
// Data its sizes.

func init() {
	drawerSections["configmaps\x00Binary Data"] = keyedSection
}

// keyedSection renders a section's items as keys above their values; an item without a name (a
// note that more keys are not listed) is its value alone.
func keyedSection(d *drawer, ref model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, 2*len(sec.Items))
	for _, it := range sec.Items {
		objs = append(objs, d.keyName(it.Name), d.itemValue(ref, sec, it))
	}
	return box(keyedLayout{}, objs...)
}

// keyName is a data key on a line of its own, muted as the details' names, wrapped at the details'
// width by wrapName when it is wider; nothing for an item without a name.
func (d *drawer) keyName(text string) fyne.CanvasObject {
	if text == "" {
		return container.NewWithoutLayout()
	}
	th := d.sh.app.Settings().Theme()
	if w := d.contentWidth(); fyne.MeasureText(text, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width > w {
		return newNameParagraph(text, colorMuted, w)
	}
	return d.line(text, colorMuted, theme.SizeNameText, fyne.TextStyle{}, false, "")
}

// keyedLayout lays out keys and values, objects alternating key, value: each key at the full
// width, its value right below it at the full width, a wider gap before the next key.
type keyedLayout struct{}

func (keyedLayout) gaps() (inner, between float32) {
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
	return pad / 2, pad * 2
}

func (l keyedLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	inner, between := l.gaps()
	var w, h float32
	for i := 0; i+1 < len(objs); i += 2 {
		k, v := objs[i].MinSize(), objs[i+1].MinSize()
		if i > 0 {
			h += between
		}
		h += k.Height + inner + v.Height
		w = max(w, k.Width, v.Width)
	}
	return fyne.NewSize(w, h)
}

func (l keyedLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	inner, between := l.gaps()
	y := float32(0)
	for i := 0; i+1 < len(objs); i += 2 {
		if i > 0 {
			y += between
		}
		k, v := objs[i].MinSize(), objs[i+1].MinSize()
		objs[i].Move(fyne.NewPos(0, y))
		objs[i].Resize(fyne.NewSize(size.Width, k.Height))
		y += k.Height + inner
		objs[i+1].Move(fyne.NewPos(0, y))
		objs[i+1].Resize(fyne.NewSize(size.Width, v.Height))
		y += v.Height
	}
}
