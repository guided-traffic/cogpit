package fyneui

import (
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A pod's Containers cell draws one square per container in the container's status colour, the
// text ("1/2") gives way to them, and the tooltip names each container and its state (owner,
// 2026-10-12, from Lens).
func TestContainersCellDrawsSquares(t *testing.T) {
	f := kindsFake(t, "pods")
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the pods", func() bool { return sh.view.snap != nil && len(sh.view.snap.Rows) > 0 })
	settle(sh.win)
	col := columnOf(t, sh, "containers")
	found := false
	for row, r := range sh.view.snap.Rows {
		ci := sh.view.cols[col-firstDataColumn]
		if ci >= len(r.Cells) || r.Cells[ci].Marks == nil {
			continue
		}
		marks := *r.Cells[ci].Marks
		c := cellAt(t, sh, row, col)
		if c.text.Text != "" || c.shown != len(marks) {
			t.Fatalf("row %d: text %q, %d of %d squares shown", row, c.text.Text, c.shown, len(marks))
		}
		th := c.Theme()
		for i, m := range marks {
			if !c.marks[i].Visible() || c.marks[i].FillColor != markColor(th, m.Status) {
				t.Errorf("row %d square %d: visible %v, colour %v for %s", row, i, c.marks[i].Visible(), c.marks[i].FillColor, m.Status)
			}
			if !strings.Contains(c.tipText(), m.Text) {
				t.Errorf("row %d: tooltip %q lacks %q", row, c.tipText(), m.Text)
			}
		}
		found = true
		break
	}
	if !found {
		t.Fatal("no pod row with container marks in the fixture")
	}
}

// A pod with more containers than its cell has room for shows the squares that fit and "+N" for
// the others, coloured by their gravest state, so that a running container behind finished init
// containers is not mistaken for none (the owner's F24 of 2026-10-17); a wider cell shows them all.
func TestContainersCellCountsWhatDoesNotFit(t *testing.T) {
	test.NewTempApp(t)
	marks := []model.CellMark{
		{Text: "a: terminated", Status: model.StatusStopped}, {Text: "b: terminated", Status: model.StatusStopped},
		{Text: "c: terminated", Status: model.StatusStopped}, {Text: "d: terminated", Status: model.StatusStopped},
		{Text: "e: terminated", Status: model.StatusStopped}, {Text: "f: running", Status: model.StatusOK},
	}
	c := newTableCell(&tableView{})
	c.set(0, 0, cellContent{text: "1/1", marks: &marks}, rowMarks{}, false, false)
	th := c.Theme()
	side, gap := markSize(th)
	pad := th.Size(sizeCellPadding)

	c.Resize(fyne.NewSize(2*pad+4*(side+gap), 30)) // room for four squares, not six
	test.WidgetRenderer(c).Layout(c.Size())
	if c.shown >= len(marks) || c.shown == 0 || c.text.Text != "+"+strconv.Itoa(len(marks)-c.shown) {
		t.Fatalf("narrow: %d squares shown, text %q", c.shown, c.text.Text)
	}
	if c.text.Color != themeColor(th, markColorName(model.StatusOK)) {
		t.Errorf("the count is not in the hidden running container's colour: %v", c.text.Color)
	}
	if end := pad + float32(c.shown)*(side+gap) + c.text.MinSize().Width; end > c.Size().Width-pad+0.5 {
		t.Errorf("the squares and the count overflow the cell: %.1f of %.1f", end, c.Size().Width-pad)
	}
	for i, r := range c.marks {
		if r.Visible() != (i < c.shown) {
			t.Errorf("square %d visible %v with %d shown", i, r.Visible(), c.shown)
		}
	}

	c.Resize(fyne.NewSize(2*pad+float32(len(marks))*(side+gap)+side, 30))
	test.WidgetRenderer(c).Layout(c.Size())
	if c.shown != len(marks) || c.text.Text != "" {
		t.Errorf("wide: %d of %d squares shown, text %q", c.shown, len(marks), c.text.Text)
	}
}
