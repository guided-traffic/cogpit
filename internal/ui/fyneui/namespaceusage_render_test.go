package fyneui

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Namespaces list over the demo cluster (D-154): each namespace's pods' CPU and Memory, and
// what they request with their limits, a "+" after a limit some pod runs without, orange for
// memory, in the text's colour for CPU. The "+" follows the number directly. PNG (COGPIT_PNG_DIR
// only): demo_namespaces.png.
func TestNamespacesListUsageOverTheDemo(t *testing.T) {
	s, sh, w, until := demoShell(t)
	until("the demo cluster", func() bool { return sh.ui.Connection.State == engine.Connected })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "namespaces"})
	col := func(id string) int {
		return slices.IndexFunc(sh.view.snap.Columns, func(c model.Column) bool { return c.ID == id })
	}
	var plus *tableCell
	until("the namespaces' usage and an open memory limit", func() bool {
		snap := sh.view.snap
		if snap == nil || snap.View != "namespaces" || len(snap.Rows) == 0 || col("cpuRequests") < 0 {
			return false
		}
		mi, ci := col("memoryRequests"), col(model.ColumnCPU)
		for _, r := range snap.Rows {
			if mi < len(r.Cells) && r.Cells[mi].Parts != nil && ci < len(r.Cells) && r.Cells[ci].Text != "" {
				return true
			}
		}
		return false
	})
	settle(w)
	vis := columnOf(t, sh, "memoryRequests")
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.col == vis && c.Visible() && c.content.parts != nil {
			plus = c
			break
		}
	}
	if plus == nil {
		t.Fatal("no shown memory cell with an open limit")
	}
	if got := plus.run(1); got.Text != "+" || got.Color != themeColor(plus.Theme(), statusColorName(model.StatusWarning)) {
		t.Errorf("the +: %q in %v", got.Text, got.Color)
	}
	if x0, x1 := plus.runX[1], plus.runX[0]+plus.run(0).MinSize().Width; x0-x1 > 1 {
		t.Errorf("a gap of %.1f before the +", x0-x1)
	}
	demoPNG(t, w, "demo_namespaces.png")
}
