package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer's Copy (object.copyName) sits right after the object's name in the header, not among
// the actions at the header's right end (the owner's F4). The command and its MCP tool are
// unchanged; only where the window draws it moved. Always visible, as before (the lead's default:
// the lists' copy icon shows on hover, the drawer's name has room for one icon).

// copyNameID is the command the name's copy icon runs.
const copyNameID = "object.copyName"

// showNameCopy shows the name's copy icon for the drawer's action a.
func (d *drawer) showNameCopy(ref model.ObjectRef, a actionState) {
	b := d.nameCopy
	b.tip = actionTip(a)
	b.OnTapped = func() { d.run(ref, a, b) }
	if a.reason != "" {
		b.Disable()
	} else {
		b.Enable()
	}
	b.Show()
	d.titleBox.Refresh()
}

// setTitle shows the object's name and lays the copy icon out after it again.
func (d *drawer) setTitle(name string) {
	d.title.SetText(name)
	d.titleBox.Refresh()
}

// titleCopyLayout lays out the header's title and the copy icon after it: the title as wide as its
// text, shortened with an ellipsis only when the icon would not fit beside it, and the icon right
// after the text, centred on the line.
type titleCopyLayout struct{ title *textLine }

func (l *titleCopyLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 2 {
		return
	}
	title, icon := objs[0], objs[1]
	iconSize, gap := fyne.NewSize(0, 0), float32(0)
	if icon.Visible() {
		iconSize = icon.MinSize()
		gap = theme.Padding()
	}
	tw := min(l.textWidth(), max(size.Width-iconSize.Width-gap, 0))
	title.Move(fyne.NewPos(0, 0))
	title.Resize(fyne.NewSize(tw, size.Height))
	icon.Move(fyne.NewPos(tw+gap, (size.Height-iconSize.Height)/2))
	icon.Resize(iconSize)
}

func (l *titleCopyLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		if o.Visible() {
			h = max(h, o.MinSize().Height)
		}
	}
	return fyne.NewSize(0, h)
}

// textWidth is the title's text drawn whole.
func (l *titleCopyLayout) textWidth() float32 {
	th := l.title.Theme()
	return fyne.MeasureText(l.title.text, th.Size(l.title.size), l.title.style).Width
}
