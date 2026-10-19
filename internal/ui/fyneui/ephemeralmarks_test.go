package fyneui

import (
	"image/color"
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// An ephemeral container's square is blue (D-102): filled while it runs, an outline once it has
// finished, as large as a filled one; a reused cell paints a filled mark filled again. However wide
// the column, at most maxCellMarks squares show and the rest are counted, the count in the hidden
// ones' colour (finished ephemeral containers: blue).
func TestContainersCellDrawsEphemeralMarks(t *testing.T) {
	test.NewTempApp(t)
	marks := []model.CellMark{
		{Text: "app: running", Status: model.StatusOK},
		{Text: "debugger: ephemeral, running", Status: model.StatusEphemeral},
		{Text: "dnscheck: ephemeral, terminated", Status: model.StatusEphemeral, Hollow: true},
	}
	c := newTableCell(&tableView{})
	c.set(0, 0, cellContent{text: "1/1", marks: &marks}, rowMarks{}, false, false)
	th := c.Theme()
	side, gap := markSize(th)
	pad := th.Size(sizeCellPadding)
	c.Resize(fyne.NewSize(2*pad+10*(side+gap), 30))
	test.WidgetRenderer(c).Layout(c.Size())
	blue := themeColor(th, colorStatusEphemeral)
	if c.marks[1].FillColor != blue || c.marks[1].StrokeWidth != 0 {
		t.Errorf("a running ephemeral container: fill %v, stroke %v", c.marks[1].FillColor, c.marks[1].StrokeWidth)
	}
	hollow := c.marks[2]
	if hollow.FillColor != color.Transparent || hollow.StrokeColor != blue || hollow.StrokeWidth != markStroke {
		t.Errorf("a finished ephemeral container: fill %v, stroke %v %v", hollow.FillColor, hollow.StrokeColor, hollow.StrokeWidth)
	}
	if got, want := hollow.Size().Width+hollow.StrokeWidth, c.marks[1].Size().Width; got != want {
		t.Errorf("the outline's outer side %.1f, a filled square's %.1f", got, want)
	}

	filled := []model.CellMark{{Text: "a", Status: model.StatusOK}, {Text: "b", Status: model.StatusOK}, {Text: "c", Status: model.StatusOK}}
	c.set(0, 0, cellContent{text: "3/3", marks: &filled}, rowMarks{}, false, false)
	if c.marks[2].StrokeWidth != 0 || c.marks[2].FillColor == color.Transparent {
		t.Errorf("a reused outline stays hollow: fill %v, stroke %v", c.marks[2].FillColor, c.marks[2].StrokeWidth)
	}

	many := []model.CellMark{{Text: "app: running", Status: model.StatusOK}}
	for range 19 {
		many = append(many, model.CellMark{Text: "x: ephemeral, terminated", Status: model.StatusEphemeral, Hollow: true})
	}
	c.set(0, 0, cellContent{text: "1/1", marks: &many}, rowMarks{}, false, false)
	c.Resize(fyne.NewSize(2*pad+40*(side+gap), 30)) // room for all twenty
	test.WidgetRenderer(c).Layout(c.Size())
	if c.shown != maxCellMarks || c.text.Text != "+"+strconv.Itoa(len(many)-maxCellMarks) || c.text.Color != blue {
		t.Errorf("wide: %d shown, text %q in %v", c.shown, c.text.Text, c.text.Color)
	}
}
