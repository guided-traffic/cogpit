package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestRenderDemoNodeDisks runs the shell over the real engine and the demo cluster, whose metrics
// provider makes up the nodes' disks (D-90): the Nodes list fills its Disk column with the fullest
// filesystem, and a worker's drawer shows a usage bar per filesystem. The PNG goes to
// COGPIT_PNG_DIR when that is set (else it is only captured), never to testdata.
func TestRenderDemoNodeDisks(t *testing.T) {
	s, sh, w, until := demoShell(t)
	until("the demo connected", func() bool { tbl := s.Table("pods"); return tbl != nil && tbl.Synced && len(sh.kinds) > 0 })
	s.UpdateUI(func(u *engine.UIState) { u.View = "nodes" })
	var ref model.ObjectRef
	until("a node with several filesystems", func() bool {
		tbl := s.Table("nodes")
		if tbl == nil || !tbl.Synced || len(sh.kinds) == 0 {
			return false
		}
		disk := -1
		for i, c := range tbl.Columns {
			if c.ID == model.ColumnDisk {
				disk = i
			}
		}
		for _, r := range tbl.Rows {
			if disk < 0 || r.Cells[disk].Bar == nil {
				continue
			}
			if m, err := s.ObjectMetrics(r.Ref); err == nil && m != nil && m.Disk != nil && len(m.Disk.Filesystems) > 1 {
				ref = r.Ref
				return true
			}
		}
		return false
	})
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref; u.Selected = []model.ObjectRef{ref} })
	until("the drawer's disks", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && p.data != nil && p.disk.Visible() && len(p.disk.fs) > 1
	})
	t.Logf("%s: %d filesystems", ref.Name, len(sh.drawer.kept[metricsKey].(*metricsPanel).disk.fs))
	demoPNG(t, w, "demo_node_disks.png")
}
