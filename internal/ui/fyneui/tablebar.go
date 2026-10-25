package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A usage bar in a table cell (G12; resource-parity › Nodes: "CPU, Memory, Disk (usage bars)"): a
// cell whose model.Cell has a Bar shows its share ("7.5%") right-aligned in a slot as wide as
// "100%", then a track to the cell's right edge filled to the share, in the accent colour below the
// warning share and in the status colours from it (model.UsageBarStatus; the thresholds are
// Cogpit's defaults). The bars of a column line up because every share's slot is as wide. A cell too
// narrow for a bar of barMinWidth beside its share shows the share alone. The track and the fill
// are two rectangles a cell makes the first time it shows a bar and reuses afterwards, so a list of
// 10,000 nodes costs no widget per cell (the table reuses its cells).

// cellBar is a cell's usage bar.
type cellBar struct {
	track, fill *canvas.Rectangle
	shown       bool    // the bar fits beside the share (set by fit)
	slot        float32 // the share's room, the text right-aligned in it
}

// barMinWidth is the narrowest bar drawn; a narrower room shows the share alone.
const barMinWidth = 16

// barShareSlot is the text the share's slot is measured on: the widest share drawn in line.
const barShareSlot = "100%"

// barHeight is a bar's thickness: under half the text size, at least four units.
func barHeight(th fyne.Theme) float32 {
	return max(4, float32(int(th.Size(theme.SizeNameText)*0.45+0.5)))
}

// barColorName is a bar's fill colour: the accent below the warning share, else the status colour.
func barColorName(s model.Status) fyne.ThemeColorName {
	if s == model.StatusNone {
		return colorAccent
	}
	return statusColorName(s)
}

// barShows reports whether the cell's content is a usage bar.
func (c *tableCell) barShows() bool {
	return c.content.bar != nil && c.content.kind == cellText && c.content.marks == nil
}

// applyBar colours the cell's bar, making it the first time the cell shows one.
func (c *tableCell) applyBar(th fyne.Theme) {
	if !c.barShows() {
		if c.bar != nil {
			c.bar.shown = false
		}
		return
	}
	if c.bar == nil {
		c.bar = &cellBar{track: canvas.NewRectangle(color.Transparent), fill: canvas.NewRectangle(color.Transparent)}
		c.objs++
	}
	track := theme.ColorNameSeparator
	if c.checked {
		track = colorRowHover // the separator's colour sinks into the selection's fill in the light theme
	}
	c.bar.track.FillColor = themeColor(th, track)
	c.bar.fill.FillColor = themeColor(th, barColorName(c.content.bar.Status))
}

// refreshBar redraws the bar's rectangles.
func (c *tableCell) refreshBar() {
	if c.bar != nil {
		c.bar.track.Refresh()
		c.bar.fill.Refresh()
	}
}

// fitBar decides whether the bar fits in avail beside the share and returns the room left for the
// text: the share's slot when it does, else avail.
func (c *tableCell) fitBar(th fyne.Theme, avail float32) float32 {
	if c.bar == nil {
		return avail
	}
	if !c.barShows() {
		c.bar.shown = false
		return avail
	}
	size := th.Size(theme.SizeNameText)
	slot := fyne.MeasureText(barShareSlot, size, fyne.TextStyle{}).Width
	slot = max(slot, fyne.MeasureText(c.full, size, fyne.TextStyle{}).Width) // "105%": a node above its allocatable
	c.bar.slot = slot
	c.bar.shown = avail-slot-th.Size(theme.SizeNamePadding) >= barMinWidth
	if !c.bar.shown {
		return avail
	}
	return slot
}

// layoutBar places the bar in a cell of size and returns the x of the text: right-aligned in the
// share's slot when the bar shows, else the cell's padding. The cell's layout calls it, also when
// a cell the table keeps on screen gets new content (tableCell.set): the share's x and the fill's
// width follow the content.
func (c *tableCell) layoutBar(th fyne.Theme, size fyne.Size, pad, textWidth float32) float32 {
	if c.bar == nil {
		return pad
	}
	if !c.bar.shown || !c.barShows() {
		c.bar.track.Hide()
		c.bar.fill.Hide()
		return pad
	}
	x := pad + c.bar.slot + th.Size(theme.SizeNamePadding)
	w := size.Width - pad - x
	if w < barMinWidth {
		c.bar.track.Hide()
		c.bar.fill.Hide()
		return pad
	}
	h := barHeight(th)
	y := (size.Height - h) / 2
	c.bar.track.CornerRadius = h / 2
	c.bar.track.Move(fyne.NewPos(x, y))
	c.bar.track.Resize(fyne.NewSize(w, h))
	c.bar.track.Show()
	share := min(max(c.content.bar.Share, 0), 1)
	fw := w * float32(share)
	if share > 0 {
		fw = max(fw, 2) // a share that rounds to nothing still shows it is not zero
	}
	if fw <= 0 {
		c.bar.fill.Hide()
	} else {
		c.bar.fill.CornerRadius = min(h, fw) / 2
		c.bar.fill.Move(fyne.NewPos(x, y))
		c.bar.fill.Resize(fyne.NewSize(fw, h))
		c.bar.fill.Show()
	}
	return pad + c.bar.slot - textWidth
}
