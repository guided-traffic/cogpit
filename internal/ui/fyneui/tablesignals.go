package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The Signals column (D-150; the owner, 2026-11-03: every mark an object carries in one column
// after Name, titled "Signals"): a cell draws one icon per signal side by side, each in its slot
// (model.Signal.Slot) so that a source's icon keeps its place in the view: the object's own state
// as the triangle, Kyverno's violations as its glyph, both in the signal's status colour, and
// Trivy's worst image grade as its shield (an N/A one dimmed). The pointer over an icon shows that
// signal's text and what is behind it; a click on one selects the row and opens the drawer at the
// signal's section (drawer.openAt). A row that shows a status pill and a spine (statusLit) leaves
// out its state's triangle, which would say the same a third time; the integrations' icons stay.
// The column keeps a fixed width: its slots, or its title and the sort arrow, whichever is wider.

// signalGap is the room between two icons of a Signals cell, in points.
const signalGap = 4

// signalStep is the width one slot of a Signals cell takes: an icon (a shield is as wide) and the gap.
func signalStep(th fyne.Theme) float32 {
	return max(th.Size(sizeCellIcon), gradeTableSide) + signalGap
}

// signalsWidth is a Signals column's fixed width: its slots' icons with the cell's padding, or its
// title with the sort arrow as the header lays them out (headerCellRenderer), whichever is wider.
func signalsWidth(th fyne.Theme, col model.Column) float32 {
	pad, gap := th.Size(sizeCellPadding), th.Size(theme.SizeNamePadding)
	icons := 2*pad + float32(max(col.Slots, 1))*signalStep(th) - signalGap
	title := pad + fyne.MeasureText(col.Title, th.Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Width +
		gap + th.Size(sizeSortIcon)
	return float32(int(max(icons, title) + 1)) // whole points: a point of slack against rounding
}

// shownSignals are the signals a cell draws: all of them, but the state's on a lit row.
func (c *tableCell) shownSignals() []model.Signal {
	if c.content.kind != cellSignals || c.content.signals == nil {
		return nil
	}
	all := *c.content.signals
	if !c.content.lit {
		return all
	}
	out := make([]model.Signal, 0, len(all))
	for _, s := range all {
		if s.Source != model.SignalState {
			out = append(out, s)
		}
	}
	return out
}

// setSignals shows a Signals cell's icons and shield, or hides those of a cell that shows something
// else. Icons are made on demand and kept; the themed resources are shared (tableView.signalIcon).
func (c *tableCell) setSignals() {
	shown := c.shownSignals()
	n := 0
	for _, s := range shown {
		if s.Grade != model.GradeNone {
			continue // the shield (setGrade)
		}
		if n == len(c.sigIcons) {
			img := canvas.NewImageFromResource(nil)
			img.FillMode = canvas.ImageFillContain
			c.sigIcons = append(c.sigIcons, img)
			c.sigRes = append(c.sigRes, nil)
			c.objs++
		}
		if res := c.tv.signalIcon(s); res != c.sigRes[n] {
			c.sigRes[n] = res
			c.sigIcons[n].Resource = res
			c.sigIcons[n].Refresh()
		}
		c.sigIcons[n].Show()
		n++
	}
	for _, img := range c.sigIcons[n:] {
		img.Hide()
	}
}

// signalGrade is the grade of the cell's shield: a Signals cell's grade signal, else a grade cell's.
func (c *tableCell) signalGrade() model.ImageGrade {
	if c.content.kind == cellGrade {
		return c.content.grade
	}
	for _, s := range c.shownSignals() {
		if s.Grade != model.GradeNone {
			return s.Grade
		}
	}
	return model.GradeNone
}

// layoutSignals places a Signals cell's icons in their slots.
func (c *tableCell) layoutSignals(th fyne.Theme, size fyne.Size) {
	if c.content.kind != cellSignals {
		return
	}
	pad, step, is := th.Size(sizeCellPadding), signalStep(th), th.Size(sizeCellIcon)
	n := 0
	for _, s := range c.shownSignals() {
		x := pad + float32(s.Slot)*step
		if s.Grade != model.GradeNone {
			if c.badge != nil {
				c.badge.Move(fyne.NewPos(x, (size.Height-gradeTableSide)/2))
				c.badge.Resize(fyne.NewSize(gradeTableSide, gradeTableSide))
			}
			continue
		}
		if n < len(c.sigIcons) {
			c.sigIcons[n].Move(fyne.NewPos(x+(step-signalGap-is)/2, (size.Height-is)/2))
			c.sigIcons[n].Resize(fyne.NewSize(is, is))
		}
		n++
	}
}

// signalAt is the signal whose icon is at p in the cell, or nil.
func (c *tableCell) signalAt(p fyne.Position) *model.Signal {
	shown := c.shownSignals()
	if len(shown) == 0 {
		return nil
	}
	th := c.Theme()
	pad, step := th.Size(sizeCellPadding), signalStep(th)
	for i := range shown {
		x := pad + float32(shown[i].Slot)*step
		if p.X >= x-signalGap/2 && p.X < x+step-signalGap/2 {
			return &shown[i]
		}
	}
	return nil
}

// signalTip is the tooltip of the icon the pointer is on: its text, then what is behind it.
func (c *tableCell) signalTip() string {
	if c.sigHover < 0 {
		return ""
	}
	for _, s := range c.shownSignals() {
		if s.Slot != c.sigHover {
			continue
		}
		if s.Tip != "" {
			return s.Text + "\n" + s.Tip
		}
		return s.Text
	}
	return ""
}

// pointSignal follows the pointer over a Signals cell: the icon under it gets the tooltip.
func (tv *tableView) pointSignal() {
	c := tv.hoverCell
	if c == nil || c.content.kind != cellSignals {
		return
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(c)
	slot := -1
	if s := c.signalAt(tv.pointer.Subtract(at)); s != nil {
		slot = s.Slot
	}
	if slot != c.sigHover {
		c.sigHover = slot
		tv.requestTip()
	}
}

// signalIcon is a state's or an integration's icon in its signal's status colour, made once per
// source and colour.
func (tv *tableView) signalIcon(s model.Signal) fyne.Resource {
	key := signalIconKey{s.Source, statusColorName(s.Status)}
	if res, ok := tv.sigIcons[key]; ok {
		return res
	}
	glyph := theme.WarningIcon()
	if s.Source == model.SignalKyverno {
		glyph = glyphKyverno
	}
	res := themedIcon(glyph, key.color)
	if tv.sigIcons == nil {
		tv.sigIcons = map[signalIconKey]fyne.Resource{}
	}
	tv.sigIcons[key] = res
	return res
}

// signalIconKey names a signal's icon in a colour.
type signalIconKey struct {
	source model.SignalSource
	color  fyne.ThemeColorName
}

// signalTapped opens the drawer at the section of the signal clicked, if the click was on one: the
// row is selected as by any click, which opens the drawer.
func (tv *tableView) signalTapped(c *tableCell, p fyne.Position) {
	if s := c.signalAt(p); s != nil {
		tv.sh.drawer.openAt(tv.snap.Rows[c.row].Ref, s.Section)
	}
}

// signalIcons are a Signals cell in a drawer's table (a node's pods): each signal's icon with its
// text as the tooltip, side by side.
func (d *drawer) signalIcons(signals []model.Signal) fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, len(signals))
	for _, s := range signals {
		if s.Grade != model.GradeNone {
			badge := newGradeBadge(gradeTableSide, s.Grade)
			objs = append(objs, container.New(layout.NewGridWrapLayout(fyne.NewSquareSize(gradeTableSide)), badge))
			continue
		}
		glyph := theme.WarningIcon()
		if s.Source == model.SignalKyverno {
			glyph = glyphKyverno
		}
		objs = append(objs, newTipIcon(d.sh, glyph, statusColorName(s.Status), s.Text))
	}
	return container.NewHBox(objs...)
}
