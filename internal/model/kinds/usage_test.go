package kinds_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func TestUsageColumns(t *testing.T) {
	var withUsage int
	for _, p := range kinds.Builtin() {
		if p.Usage == "" {
			continue
		}
		withUsage++
		cpu, mem := p.ColumnIndex(model.ColumnCPU), p.ColumnIndex(model.ColumnMemory)
		if cpu < 0 || mem < 0 || p.Columns[cpu].Type != model.ColumnMetric || p.Columns[mem].Type != model.ColumnMetric || p.UsageCells == nil {
			t.Errorf("%s: usage kind %q without CPU and Memory metric columns and UsageCells", p.Key, p.Usage)
		}
	}
	if withUsage != 2 {
		t.Errorf("%d kinds with usage, want pods and nodes", withUsage)
	}
}

func TestPodsListLensOrder(t *testing.T) {
	p := profile(t, "pods")
	var ids []string
	for _, c := range p.Columns[:5] {
		ids = append(ids, c.ID)
	}
	if got := ids; got[0] != "name" || got[2] != "namespace" || got[3] != "cpu" || got[4] != "memory" {
		t.Errorf("first columns = %v, want Name, warning, Namespace, CPU, Memory (Lens)", got)
	}
}

func TestUsageCells(t *testing.T) {
	pods := profile(t, "pods")
	cpu, mem := pods.UsageCells(&unstructured.Unstructured{}, model.Usage{CPUMilli: 12, MemoryBytes: 20_447_232})
	if cpu.Text != "0.012" || cpu.Num != 12 || mem.Text != "19.5 MiB" || mem.Num != 20_447_232 {
		t.Errorf("pod cells = %+v, %+v", cpu, mem)
	}

	nodes := profile(t, "nodes")
	node := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"allocatable": map[string]any{"cpu": "4", "memory": "8Gi"},
	}}}
	cpu, mem = nodes.UsageCells(node, model.Usage{CPUMilli: 300, MemoryBytes: 2 << 30})
	if cpu.Text != "7.5%" || cpu.Num != 75 || mem.Text != "25%" || mem.Num != 250 {
		t.Errorf("node cells = %+v, %+v", cpu, mem)
	}
	// No allocatable amount reported: absolute usage, not a division by zero.
	cpu, _ = nodes.UsageCells(&unstructured.Unstructured{}, model.Usage{CPUMilli: 300})
	if cpu.Text != "0.300" {
		t.Errorf("node without allocatable: cpu = %+v", cpu)
	}
}

// A part the provider did not measure is an empty cell, not "0%" (G12: a Nodes list right after
// connecting showed CPU 0% beside a memory value, when Prometheus's CPU rate had no series yet).
func TestUsageCellsLeaveAMissingPartEmpty(t *testing.T) {
	nodes := profile(t, "nodes")
	node := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"allocatable": map[string]any{"cpu": "4", "memory": "8Gi"},
	}}}
	cpu, mem := nodes.UsageCells(node, model.Usage{MemoryBytes: 2 << 30, Missing: model.UsageCPU})
	if cpu != (model.Cell{}) || mem.Text != "25%" {
		t.Errorf("node without a CPU value: cells %+v, %+v; want an empty CPU cell and 25%%", cpu, mem)
	}
	pods := profile(t, "pods")
	cpu, mem = pods.UsageCells(&unstructured.Unstructured{}, model.Usage{CPUMilli: 12, Missing: model.UsageMemory})
	if cpu.Text != "0.012" || mem != (model.Cell{}) {
		t.Errorf("pod without a memory value: cells %+v, %+v", cpu, mem)
	}
	// A pod's total lacks a part only when every container lacks it.
	p := model.PodUsage{Containers: []model.ContainerUsage{
		{Name: "a", Usage: model.Usage{MemoryBytes: 1, Missing: model.UsageCPU}},
		{Name: "b", Usage: model.Usage{CPUMilli: 5, MemoryBytes: 2}},
	}}
	if tot := p.Total(); !tot.HasCPU() || tot.CPUMilli != 5 || tot.MemoryBytes != 3 {
		t.Errorf("total = %+v", tot)
	}
	p.Containers[1].Missing = model.UsageCPU
	if tot := p.Total(); tot.HasCPU() || !tot.HasMemory() {
		t.Errorf("total of containers without CPU = %+v, want CPU missing", tot)
	}
	if (model.PodUsage{}).Total() != (model.Usage{}) {
		t.Error("the total of no containers is not the zero usage")
	}
}

// The Nodes list draws its CPU and Memory as usage bars (Lens's Nodes list, captured: "CPU,
// Memory, Disk (usage bars)"): the share as the text and the sort key, the bar coloured at the
// thresholds, the amounts as its detail; a node without allocatable amounts has no bar.
func TestNodeUsageBars(t *testing.T) {
	nodes := profile(t, "nodes")
	for _, id := range []string{model.ColumnCPU, model.ColumnMemory} {
		if c := nodes.Columns[nodes.ColumnIndex(id)]; c.Width <= 64 {
			t.Errorf("the Nodes list's %s column is %v wide: no room for a bar", id, c.Width)
		}
	}
	node := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"allocatable": map[string]any{"cpu": "4", "memory": "8Gi"},
	}}}
	cpu, mem := nodes.UsageCells(node, model.Usage{CPUMilli: 3700, MemoryBytes: 7 << 30})
	if cpu.Bar == nil || cpu.Bar.Status != model.StatusError || cpu.Bar.Detail != "3.700 of 4.000 cores" || cpu.Text != "92%" {
		t.Errorf("cpu cell = %+v, bar %+v", cpu, cpu.Bar)
	}
	if mem.Bar == nil || mem.Bar.Status != model.StatusWarning || mem.Bar.Detail != "7.0 GiB of 8.0 GiB" || mem.Text != "88%" {
		t.Errorf("memory cell = %+v, bar %+v", mem, mem.Bar)
	}
	cpu, _ = nodes.UsageCells(node, model.Usage{CPUMilli: 300, MemoryBytes: 1})
	if cpu.Bar == nil || cpu.Bar.Status != model.StatusNone {
		t.Errorf("a node at 7.5%% = %+v, want a bar without a status colour", cpu.Bar)
	}
	if cpu, _ = nodes.UsageCells(&unstructured.Unstructured{}, model.Usage{CPUMilli: 300}); cpu.Bar != nil {
		t.Errorf("a node without allocatable amounts has a bar: %+v", cpu)
	}
	// The Pods list keeps its numbers.
	if cpu, _ := profile(t, "pods").UsageCells(&unstructured.Unstructured{}, model.Usage{CPUMilli: 300}); cpu.Bar != nil {
		t.Errorf("a pod's cell has a bar: %+v", cpu)
	}
}

func profile(t *testing.T, key string) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Builtin() {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no profile %q", key)
	return nil
}

// The Nodes list's Disk cell is the node's fullest filesystem, its mountpoint in the bar's detail
// (D-90); a disk without a size is an empty cell.
func TestNodeDiskCell(t *testing.T) {
	p := profile(t, "nodes")
	c := p.DiskCell(nil, model.NodeDisk{Mountpoint: "/data", UsedBytes: 92 << 30, SizeBytes: 100 << 30})
	if c.Text != "92%" || c.Bar == nil || c.Bar.Status != model.StatusError || c.Bar.Detail != "/data · 92.0 GiB of 100.0 GiB" {
		t.Errorf("disk cell = %+v, bar %+v", c, c.Bar)
	}
	if c := p.DiskCell(nil, model.NodeDisk{}); c.Text != "" || c.Bar != nil {
		t.Errorf("a disk without a size = %+v", c)
	}
}
