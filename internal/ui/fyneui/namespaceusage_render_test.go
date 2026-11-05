package fyneui

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Namespaces list over the demo cluster (D-154): each namespace's pods' CPU and Memory, and
// what they request, the limits muted after them, a "+" after a limit some pod runs without,
// orange for memory, muted with the limits for CPU. The "+" follows the number directly. PNG
// (COGPIT_PNG_DIR only): demo_namespaces.png.
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
			if mi < len(r.Cells) && openLimit(r.Cells[mi].Parts) && ci < len(r.Cells) && r.Cells[ci].Text != "" {
				return true
			}
		}
		return false
	})
	settle(w)
	vis := columnOf(t, sh, "memoryRequests")
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.col == vis && c.Visible() && openLimit(c.content.parts) {
			plus = c
			break
		}
	}
	if plus == nil {
		t.Fatal("no shown memory cell with an open limit")
	}
	if got := plus.run(1); !strings.HasPrefix(got.Text, "/ ") || got.Color != themeColor(plus.Theme(), colorMuted) {
		t.Errorf("the limits: %q in %v, want them muted", got.Text, got.Color)
	}
	if got := plus.run(2); got.Text != "+" || got.Color != themeColor(plus.Theme(), statusColorName(model.StatusWarning)) {
		t.Errorf("the +: %q in %v", got.Text, got.Color)
	}
	if x0, x1 := plus.runX[2], plus.runX[1]+plus.run(1).MinSize().Width; x0-x1 > 1 {
		t.Errorf("a gap of %.1f before the +", x0-x1)
	}
	demoPNG(t, w, "demo_namespaces.png")
}

// openLimit reports whether an allocation cell's parts end in a "+": requests, limits, the +.
func openLimit(parts *[]model.CellPart) bool {
	return parts != nil && len(*parts) == 3 && (*parts)[2].Text == "+"
}
