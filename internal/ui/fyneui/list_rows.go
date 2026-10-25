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
// edge, a status cell as a pill, and an error row a faint tint. Every object is made the first time
// a cell needs it and reused by Fyne's cell reuse, so the common case (a healthy, unmarked row)
// costs not one object more than before.
//
// A checked row and the drawer's row are two kinds of mark, not two shades of blue (the owner's
// VW16 of 2026-10-25, his pick (a): "keinen Unterschied ... zwischen selektierten Einträgen und dem
// aktuell markierten Eintrag"), as VS Code's lists mark a selection with a fill and the focus with
// an outline: a checked row has the selection's fill; the drawer's row has an outline round the
// whole row in the glow blue and the glow blue bar at its left, no fill; a row that is both has the
// fill, the outline and the bar. The text takes the selection's colour only on the fill.

// Geometry of the marks, in points.
const (
	spineWidth   = 3   // a status spine
	selBarWidth  = 2   // the drawer's row's bar
	outlineWidth = 1.5 // the drawer's row's outline: heavier than the keyboard's ring (1), so the two read apart
	markGap      = 1   // between the outline, the spine and the bar at a row's left end
	pillDotSide  = 6   // the dot in a status cell
	pillPadX     = 8   // inside a pill, before the dot and after the word
	pillDotGap   = 6   // between the dot and the word
	pillAlpha    = 36  // a pill's fill: the status colour at about 14 %
	errorTint    = 15  // an error row's tint: about 6 %
	maxPillText  = 24  // the longest text drawn as a status pill; longer is a sentence
	markRadius   = 2.5 // a container square's corner
	markGapWidth = 2   // between two squares

	quietDotSide  = 4    // a healthy status word's dot
	quietDotAlpha = 0x8c // and its alpha: about 55 %
	quietMarkA    = 0x66 // a running container's square: the Running green at 40 %, so a column of them stays calm
	quietStrokeA  = 0x8c // a stopped container's outline: about 55 %
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

// applyRowMarks paints the row's spine and the drawer's bar (the first cell's), the cell's part of
// the drawer's outline, and the cell's pill and dot, making the objects the first time they are
// needed.
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
	if c.col == checkColumn && c.drawer {
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
	c.applyOutline(th)
	c.layoutLead(c.Size())
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
			c.pill.FillColor = withAlpha(col, pillFill(th))
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

// rowPlate is the row's plate: the selection's fill on a checked row, the hover, else nothing, or
// for an error row the faint tint of its status. The drawer's row has no fill of its own: its
// outline marks it (VW16).
func (c *tableCell) rowPlate(th fyne.Theme) color.Color {
	switch {
	case c.checked:
		return themeColor(th, colorRowSelected)
	case c.hover:
		return themeColor(th, colorRowHover)
	case c.content.row == model.StatusError:
		return withAlpha(themeColor(th, colorStatusError), errorTintFor(th))
	}
	return color.Transparent
}

// applyOutline shows the cell's part of the drawer's outline (VW16, rowoutline.go), or hides it on
// another row.
func (c *tableCell) applyOutline(th fyne.Theme) {
	if !c.drawer {
		if c.outline != nil && c.outline.box.Visible() {
			c.outline.box.Hide()
		}
		return
	}
	if c.outline == nil {
		c.outline = newRowOutline()
		c.objs++
	}
	c.outline.paint(themeColor(th, theme.ColorNamePrimary))
	c.layoutOutline(c.Size(), th) // the outline may be new, or the cell's column another
	if !c.outline.box.Visible() {
		c.outline.box.Show()
	}
}

// layoutOutline places the cell's part of the drawer's outline in a cell of size.
func (c *tableCell) layoutOutline(size fyne.Size, th fyne.Theme) {
	if c.outline == nil || !c.drawer || c.tv == nil {
		return
	}
	c.outline.layout(size, th.Size(theme.SizeNamePadding), th.Size(theme.SizeNameSelectionRadius),
		c.col == checkColumn, c.col == c.tv.menuColumn())
}

// layoutLead places the spine and the drawer's bar at the row's left end in a cell of size: right of
// the drawer's outline on its row (VW16), so that the spine still shows. apply calls it too, since a
// row that becomes the drawer's moves them without a layout.
func (c *tableCell) layoutLead(size fyne.Size) {
	inset := float32(6)
	x := float32(0)
	if c.drawer {
		x = outlineWidth + markGap
	}
	if c.spine != nil {
		c.spine.Move(fyne.NewPos(x, inset))
		c.spine.Resize(fyne.NewSize(spineWidth, max(0, size.Height-2*inset)))
		if c.spine.Visible() {
			x += spineWidth + markGap
		}
	}
	if c.selBar != nil {
		c.selBar.Move(fyne.NewPos(x, inset))
		c.selBar.Resize(fyne.NewSize(selBarWidth, max(0, size.Height-2*inset)))
	}
}

// layoutRowMarks places the spine, the drawer's bar, the pill and the dot in a cell of size whose
// text has size ts and starts at x.
func (c *tableCell) layoutRowMarks(size fyne.Size, ts fyne.Size, pad float32) {
	c.layoutLead(size)
	if c.pill != nil && c.pillShows() {
		h := min(size.Height-8, ts.Height+6)
		y := (size.Height - h) / 2
		c.pill.CornerRadius = min(9, h/2)
		c.pill.Move(fyne.NewPos(pad, y))
		c.pill.Resize(fyne.NewSize(min(size.Width-pad, pillPadX+pillDotSide+pillDotGap+ts.Width+pillPadX), h))
		side := float32(pillDotSide)
		if !abnormalStatus(c.content.status) {
			side = quietDotSide // the slot stays pillDotSide wide, so the words of a column stay aligned
		}
		c.dot.Move(fyne.NewPos(pad+pillPadX+(pillDotSide-side)/2, (size.Height-side)/2))
		c.dot.Resize(fyne.NewSize(side, side))
	}
}

// errorTintFor is the alpha of an error row's tint: 6 % on the dark theme and 3.5 % on the light
// one, where the same tint spreads across a whole white row and shouts.
func errorTintFor(th fyne.Theme) uint8 {
	if pillFill(th) == pillAlpha {
		return errorTint
	}
	return 9
}

// markColor is a container square's paint: its colour, the running ones and the stopped ones
// quieter than the rest so that a column of healthy squares stays calm and a failing one lights.
func markColor(th fyne.Theme, s model.Status) color.Color {
	mc := themeColor(th, markColorName(s))
	switch s {
	case model.StatusOK:
		return withAlpha(mc, quietMarkA)
	case model.StatusStopped:
		return withAlpha(mc, quietStrokeA)
	}
	return mc
}
