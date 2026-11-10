package kinds

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestNodeWarningsAndConditions pins the Nodes list as Lens shows it (capture, 2026-10-11): a
// cordoned Ready node reads "SchedulingDisabled Ready", only Ready green, with no warning icon and
// no warning row. Which nodes do get the icon is inferred: not Ready, or a problem condition.
func TestNodeWarningsAndConditions(t *testing.T) {
	cordoned := map[string]any{
		"spec": map[string]any{"unschedulable": true},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "MemoryPressure", "status": "False"},
		}},
	}
	s := summariseNode(cordoned)
	c := s.conditionsCell()
	want := []model.CellPart{{Text: "SchedulingDisabled"}, {Text: "Ready", Status: model.StatusOK}}
	if c.Parts == nil || len(*c.Parts) != len(want) || (*c.Parts)[0] != want[0] || (*c.Parts)[1] != want[1] {
		t.Fatalf("parts = %+v, want %+v", c.Parts, want)
	}
	if c.Text != "SchedulingDisabled Ready" || c.Status != model.StatusOK || s.token != model.StatusOK {
		t.Errorf("cordoned: %q %q row %q", c.Text, c.Status, s.token)
	}
	if w := s.warning(); w != (model.Cell{}) {
		t.Errorf("a cordoned Ready node must not warn: %+v", w)
	}

	// A Ready node under pressure is a warning row and warns; the Ready part stays green.
	pressure := map[string]any{"status": map[string]any{"conditions": []any{
		map[string]any{"type": "Ready", "status": "True"},
		map[string]any{"type": "DiskPressure", "status": "True"},
	}}}
	s = summariseNode(pressure)
	if c := s.conditionsCell(); c.Text != "Ready DiskPressure" || (*c.Parts)[0].Status != model.StatusOK || (*c.Parts)[1].Status != model.StatusWarning {
		t.Errorf("pressure cell = %+v %+v", c, c.Parts)
	}
	if s.token != model.StatusWarning || s.warning().Text != "DiskPressure" {
		t.Errorf("pressure: row %q, warning %+v", s.token, s.warning())
	}

	// A condition whose type ends in Ready reads the other way (SystemdUnitsReady on a Gardener
	// cluster of STACKIT, the owner's finding of 2026-10-17): True is healthy, no warning, green in
	// the drawer's chips only; False or Unknown is the problem, named with its status.
	systemd := func(status string) map[string]any {
		return map[string]any{"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "SystemdUnitsReady", "status": status},
			map[string]any{"type": "KernelDeadlock", "status": "False"},
		}}}
	}
	s = summariseNode(systemd("True"))
	if c := s.conditionsCell(); c.Text != "Ready" || s.token != model.StatusOK || s.warning() != (model.Cell{}) {
		t.Errorf("SystemdUnitsReady True: cell %q, row %q, warning %+v", c.Text, s.token, s.warning())
	}
	if got := s.detailParts(); len(got) != 2 || got[1] != (model.CellPart{Text: "SystemdUnitsReady", Status: model.StatusOK}) {
		t.Errorf("SystemdUnitsReady True: drawer chips %+v", got)
	}
	for _, status := range []string{"False", "Unknown"} {
		s = summariseNode(systemd(status))
		want := "SystemdUnitsReady: " + status
		if c := s.conditionsCell(); c.Text != "Ready "+want || s.token != model.StatusWarning || s.warning().Text != want {
			t.Errorf("SystemdUnitsReady %s: cell %q, row %q, warning %+v", status, c.Text, s.token, s.warning())
		}
	}
	// RKE2's and k3s's EtcdIsVoter reads the same way (the owner's finding of 2026-10-20): True on an
	// etcd member that votes is healthy, False (a learner) the problem.
	etcd := func(status string) map[string]any {
		return map[string]any{"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": "EtcdIsVoter", "status": status},
		}}}
	}
	if s = summariseNode(etcd("True")); s.conditionsCell().Text != "Ready" || s.token != model.StatusOK || s.warning() != (model.Cell{}) {
		t.Errorf("EtcdIsVoter True: cell %q, row %q, warning %+v", s.conditionsCell().Text, s.token, s.warning())
	}
	if s = summariseNode(etcd("False")); s.conditionsCell().Text != "Ready EtcdIsVoter: False" || s.token != model.StatusWarning {
		t.Errorf("EtcdIsVoter False: cell %q, row %q", s.conditionsCell().Text, s.token)
	}
	if problem, ok := (nodeConditionRules{}).problem("KernelDeadlock", "True"); !ok || problem != "KernelDeadlock" {
		t.Errorf("node-problem-detector's KernelDeadlock True: %q %v", problem, ok)
	}

	// The node controller's Unknown: the kubelet stopped reporting.
	unknown := map[string]any{"status": map[string]any{"conditions": []any{
		map[string]any{"type": "Ready", "status": "Unknown"},
	}}}
	if w := summariseNode(unknown).warning(); w.Status != model.StatusWarning || w.Num != model.SignalRank(model.StatusWarning, model.GradeNone, 0) {
		t.Errorf("unknown ready = %+v", w)
	}
}

func TestNodeDetailTables(t *testing.T) {
	obj := map[string]any{"status": map[string]any{
		"capacity": map[string]any{"pods": "110", "nvidia.com/gpu": "2", "cpu": "8", "memory": "32Gi", "hugepages-2Mi": "0"},
	}}
	tbl := resourceTable(obj, "capacity")
	if tbl == nil {
		t.Fatal("no table")
	}
	wantCols := []string{"CPU", "Memory", "Hugepages 2Mi", "Pods", "nvidia.com/gpu"}
	wantRow := []string{"8", "32.0 GiB", "0 B", "110", "2"} // bytes in binary units (D-32), the rest as written
	if len(tbl.Columns) != len(wantCols) || len(tbl.Rows) != 1 {
		t.Fatalf("table = %+v", tbl)
	}
	for i := range wantCols {
		if tbl.Columns[i] != wantCols[i] || tbl.Rows[0][i] != wantRow[i] {
			t.Errorf("column %d = %s %s, want %s %s", i, tbl.Columns[i], tbl.Rows[0][i], wantCols[i], wantRow[i])
		}
	}
	if resourceTable(map[string]any{}, "allocatable") != nil {
		t.Error("a node without allocatable resources should have no table")
	}
}

// TestNodeDrawerBytesInBinaryUnits is the owner's Node drawer of 2026-10-13 (D-32): Capacity's
// memory and ephemeral storage in Ki, Allocatable's in plain bytes, all shown in binary units with
// one decimal; CPU and pods as the API writes them.
func TestNodeDrawerBytesInBinaryUnits(t *testing.T) {
	node := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"capacity": map[string]any{
			"cpu": "8", "memory": "16360780Ki", "ephemeral-storage": "119842348Ki",
			"hugepages-1Gi": "0", "hugepages-2Mi": "0", "pods": "110",
		},
		"allocatable": map[string]any{
			"cpu": "7800m", "memory": "13856852378", "ephemeral-storage": "119400000000",
			"hugepages-1Gi": "0", "hugepages-2Mi": "0", "pods": "110",
		},
	}}}
	want := map[string][]string{
		"Capacity":    {"8", "15.6 GiB", "114.3 GiB", "0 B", "0 B", "110"},
		"Allocatable": {"7800m", "12.9 GiB", "111.2 GiB", "0 B", "0 B", "110"},
	}
	cols := []string{"CPU", "Memory", "Ephemeral Storage", "Hugepages 1Gi", "Hugepages 2Mi", "Pods"}
	seen := 0
	for _, s := range nodeDetail(node) {
		row, ok := want[s.Title]
		if !ok {
			continue
		}
		seen++
		if s.Table == nil || len(s.Table.Rows) != 1 || len(s.Table.Columns) != len(cols) {
			t.Fatalf("%s: %+v", s.Title, s.Table)
		}
		for i := range cols {
			if s.Table.Columns[i] != cols[i] || s.Table.Rows[0][i] != row[i] {
				t.Errorf("%s column %d = %s %q, want %s %q", s.Title, i, s.Table.Columns[i], s.Table.Rows[0][i], cols[i], row[i])
			}
		}
	}
	if seen != 2 {
		t.Errorf("Capacity and Allocatable: %d of 2 sections", seen)
	}
}

// GKE's SysctlChanged, accepted on every cluster: True does not warn and shows uncoloured in the
// drawer only; False shows nowhere. A cluster's own accepted type reads the same whatever its status.
func TestAcceptedNodeConditions(t *testing.T) {
	node := func(typ, status string) map[string]any {
		return map[string]any{"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": "True"},
			map[string]any{"type": typ, "status": status},
		}}}
	}
	s := summariseNode(node("SysctlChanged", "True"))
	if c := s.conditionsCell(); c.Text != "Ready" || s.token != model.StatusOK || s.warning() != (model.Cell{}) {
		t.Errorf("SysctlChanged True: cell %q, row %q, warning %+v", c.Text, s.token, s.warning())
	}
	if got := s.detailParts(); len(got) != 2 || got[1] != (model.CellPart{Text: "SysctlChanged"}) {
		t.Errorf("SysctlChanged True: drawer chips %+v", got)
	}
	if got := summariseNode(node("SysctlChanged", "False")).detailParts(); len(got) != 1 {
		t.Errorf("SysctlChanged False: drawer chips %+v", got)
	}

	r := nodeConditionRules{accepted: []string{"FooProblem", "BarReady"}}
	for _, c := range []struct{ typ, status, cell string }{
		{"FooProblem", "True", "Ready"},
		{"FooProblem", "False", "Ready"},
		{"BarReady", "False", "Ready"},
		{"BarReady", "Unknown", "Ready"},
		{"BazProblem", "True", "Ready BazProblem"},
	} {
		s := r.summarise(node(c.typ, c.status))
		if got := s.conditionsCell().Text; got != c.cell {
			t.Errorf("%s %s: cell %q, want %q", c.typ, c.status, got, c.cell)
		}
	}
	// A positive type stays green when True, accepted or not.
	if got := r.summarise(node("BarReady", "True")).detailParts(); len(got) != 2 || got[1] != (model.CellPart{Text: "BarReady", Status: model.StatusOK}) {
		t.Errorf("accepted BarReady True: drawer chips %+v", got)
	}
	// Ready is never accepted, whatever the list says.
	notReady := map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}}}
	if s := (nodeConditionRules{accepted: []string{"Ready"}}).summarise(notReady); s.base != "NotReady" || s.token != model.StatusError {
		t.Errorf("Ready accepted: %+v", s)
	}
	if !(nodeConditionRules{accepted: []string{"Ready"}}).isProblem("Ready", "False") {
		t.Error("isProblem took Ready as accepted")
	}

	if NodesAccepting(nodes, nil) != nodes {
		t.Error("no accepted types made a copy")
	}
	own := NodesAccepting(nodes, []string{"FooProblem"})
	if own == nodes || own.ConditionProblem("FooProblem", "True") || !nodes.ConditionProblem("FooProblem", "True") {
		t.Error("the copy's rule")
	}
}

// ConditionProblem is the one reading of a condition: the list's, Ready included.
func TestConditionProblem(t *testing.T) {
	for _, c := range []struct {
		typ, status string
		want        bool
	}{
		{"Ready", "True", false},
		{"Ready", "False", true},
		{"Ready", "Unknown", true},
		{"SystemdUnitsReady", "True", false},
		{"SystemdUnitsReady", "False", true},
		{"SystemdUnitsReady", "Unknown", true},
		{"EtcdIsVoter", "True", false},
		{"EtcdIsVoter", "False", true},
		{"MemoryPressure", "True", true},
		{"MemoryPressure", "False", false},
		{"KernelDeadlock", "True", true},
		{"NetworkUnavailable", "False", false},
		{"ContainerRuntimeReady", "False", true}, // EKS's node monitoring agent
		{"ContainerRuntimeReady", "True", false},
		{"SysctlChanged", "True", false},
		{"SysctlChanged", "False", false},
	} {
		if got := nodes.ConditionProblem(c.typ, c.status); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.typ, c.status, got, c.want)
		}
	}
}
