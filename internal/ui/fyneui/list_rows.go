package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The table row's marks in the Dark Cockpit design (docs/developer/design-language.md): a list is
// quiet while everything is healthy and lights only where something needs the operator. A healthy
// row draws nothing beyond its text; a row in an abnormal state gets a status spine at its left
// edge, a status cell as a pill, and an error row a faint tint. The selected row gets the glow
// blue bar. Every object is made the first time a cell needs it and reused by Fyne's cell reuse,
// so the common case (a healthy, unselected row) costs not one object more than before.

// Geometry of the marks, in points.
const (
	spineWidth   = 3   // a status spine
	selBarWidth  = 2   // the selected row's bar
	pillDotSide  = 6   // the dot in a status cell
	pillPadX     = 8   // inside a pill, before the dot and after the word
	pillDotGap   = 6   // between the dot and the word
	pillAlpha    = 36  // a pill's fill: the status colour at about 14 %
	errorTint    = 15  // an error row's tint: about 6 %
	markRadius   = 2.5 // a container square's corner
	markGapWidth = 2   // between two squares
)

// abnormalStatus reports whether a state asks for attention: the ones a spine and a pill light.
// Running, Ready and Completed are quiet, and so are a stopped or an ephemeral container.
func abnormalStatus(s model.Status) bool {
	switch s {
	case model.StatusPending, model.StatusWarning, model.StatusError, model.StatusTerminating, model.StatusUnknown:
		return true
	}
	return false
}

// pillShows reports whether the cell is a status cell: one plain word in a status colour. Cells in
// parts, squares, a bar or a link keep their own drawing.
func (c *tableCell) pillShows() bool {
	k := c.content
	return k.pill && k.kind == cellText && k.status != model.StatusNone && k.parts == nil && k.marks == nil &&
		k.bar == nil && k.link == nil && k.text != ""
}

// pillLead is the room before a status cell's word: the pill's padding, the dot and its gap.
func (c *tableCell) pillLead() float32 {
	if !c.pillShows() {
		return 0
	}
	return pillPadX + pillDotSide + pillDotGap
}

// pillRoom is the room a status cell's word loses to the pill: the lead and the pill's end padding.
func (c *tableCell) pillRoom() float32 {
	if !c.pillShows() {
		return 0
	}
	return c.pillLead() + pillPadX
}

// applyRowMarks paints the row's spine and selection bar (the first cell's) and the cell's pill and
// dot, making the objects the first time they are needed, and the row's error tint into bg.
func (c *tableCell) applyRowMarks(th fyne.Theme) {
	row := c.content.row
	if c.col == checkColumn && abnormalStatus(row) {
		if c.spine == nil {
			c.spine = canvas.NewRectangle(color.Transparent)
			c.spine.CornerRadius = spineWidth / 2
			c.objs++
		}
		c.spine.FillColor = themeColor(th, statusColorName(row))
		c.spine.Show()
	} else if c.spine != nil {
		c.spine.Hide()
	}
	if c.col == checkColumn && c.selected {
		if c.selBar == nil {
			c.selBar = canvas.NewRectangle(color.Transparent)
			c.selBar.CornerRadius = selBarWidth / 2
			c.objs++
		}
		c.selBar.FillColor = themeColor(th, theme.ColorNamePrimary)
		c.selBar.Show()
	} else if c.selBar != nil {
		c.selBar.Hide()
	}
	if c.pillShows() {
		if c.pill == nil {
			c.pill = canvas.NewRectangle(color.Transparent)
			c.dot = canvas.NewCircle(color.Transparent)
			c.objs++
		}
		col := themeColor(th, statusColorName(c.content.status))
		c.dot.FillColor = col
		c.dot.Show()
		c.pill.FillColor = color.Transparent
		if abnormalStatus(c.content.status) {
			c.pill.FillColor = withAlpha(col, pillAlpha)
		}
		c.pill.Show()
	} else if c.pill != nil {
		c.pill.Hide()
		c.dot.Hide()
	}
	if c.spine != nil {
		c.spine.Refresh()
	}
	if c.selBar != nil {
		c.selBar.Refresh()
	}
	if c.pill != nil {
		c.pill.Refresh()
		c.dot.Refresh()
	}
}

// bgColorName is the row's plate: the selection, the hover, else nothing, or for an error row the
// faint tint of its status. It returns the colour to paint.
func (c *tableCell) rowPlate(th fyne.Theme) color.Color {
	switch {
	case c.selected:
		return themeColor(th, colorRowSelected)
	case c.hover:
		return themeColor(th, colorRowHover)
	case c.content.row == model.StatusError:
		return withAlpha(themeColor(th, colorStatusError), errorTint)
	}
	return color.Transparent
}

// layoutRowMarks places the spine, the selection bar, the pill and the dot in a cell of size whose
// text has size ts and starts at x.
func (c *tableCell) layoutRowMarks(size fyne.Size, ts fyne.Size, pad float32) {
	inset := float32(6)
	if c.spine != nil {
		c.spine.Move(fyne.NewPos(0, inset))
		c.spine.Resize(fyne.NewSize(spineWidth, max(0, size.Height-2*inset)))
	}
	if c.selBar != nil {
		x := float32(0)
		if c.spine != nil && c.spine.Visible() {
			x = spineWidth + 1
		}
		c.selBar.Move(fyne.NewPos(x, inset))
		c.selBar.Resize(fyne.NewSize(selBarWidth, max(0, size.Height-2*inset)))
	}
	if c.pill != nil && c.pillShows() {
		h := min(size.Height-8, ts.Height+6)
		y := (size.Height - h) / 2
		c.pill.CornerRadius = min(9, h/2)
		c.pill.Move(fyne.NewPos(pad, y))
		c.pill.Resize(fyne.NewSize(min(size.Width-pad, pillPadX+pillDotSide+pillDotGap+ts.Width+pillPadX), h))
		c.dot.Move(fyne.NewPos(pad+pillPadX, (size.Height-pillDotSide)/2))
		c.dot.Resize(fyne.NewSize(pillDotSide, pillDotSide))
	}
}
