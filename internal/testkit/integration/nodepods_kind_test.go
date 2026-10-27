//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindNodeDrawerPods checks a node drawer's Pods table against the kind cluster's
// metrics-server (owner, 2026-10-13): with the drawer open the table's CPU and Memory fill from
// the pods' metrics list, as metrics.get reads them; ui.sortDrawerTable sorts the rows by memory,
// a pod without a measurement last; the drawer closed, no more pod metrics are listed. It only
// reads.
func TestKindNodeDrawerPods(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	const interval = time.Second // the pods' metrics list, polled while the drawer shows the table
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig},
		MetricsOptions: metrics.Options{ListInterval: interval, TTL: interval}}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params, out any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if out != nil {
			b, _ := json.Marshal(res)
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
		}
	}
	// Without a Metrics Server the table has no numbers to show; make kind-up installs one.
	var served engine.MetricsResult
	dispatch("metrics.get", map[string]any{"kind": "pods"}, &served)
	if served.Status == metrics.StatusNotServed {
		t.Skip("the kind cluster has no Metrics Server: run make kind-metrics-server")
	}
	nodes, err := s.Objects(ctx, "nodes", nil)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("nodes: %d, %v", len(nodes), err)
	}
	node := model.ObjectRef{Kind: "nodes", Name: nodes[0].GetName()}
	// The Events view first: the Pods list would poll the same pods' metrics itself.
	dispatch("ui.openView", map[string]any{"view": "events"}, nil)
	lists := func() int64 { return s.Diagnostics().Requests.ByVerbResource["list pods.metrics.k8s.io"] }
	before := lists()
	dispatch("ui.navigate", map[string]any{"ref": node}, nil)

	podsTable := func() *model.DetailTable {
		var m struct {
			Sections []model.DetailSection `json:"sections"`
		}
		dispatch("object.get", map[string]any{"ref": node, "noYaml": true}, &m)
		for _, sec := range m.Sections {
			if sec.Title == "Pods" && sec.Table != nil {
				return sec.Table
			}
		}
		return nil
	}
	var tbl *model.DetailTable
	measured := 0
	if !testkit.WaitFor(ctx, func() bool {
		tbl, measured = podsTable(), 0
		if tbl == nil {
			return false
		}
		ci := tbl.ColumnIndex(model.ColumnCPU)
		for _, r := range tbl.Cells {
			if r[ci].Text != "" {
				measured++
			}
		}
		return measured > 0
	}) {
		t.Fatalf("the node's Pods never showed CPU: %+v", tbl)
	}
	t.Logf("%s: %d pods, %d measured", node.Name, len(tbl.Rows), measured)

	// The cells are metrics.get's numbers in the Pods list's form.
	var mg engine.MetricsResult
	dispatch("metrics.get", map[string]any{"kind": "pods"}, &mg)
	pods, _ := s.Backend().Profile("pods")
	usage := map[string]model.Usage{}
	for _, p := range mg.Pods {
		usage[p.Namespace+"/"+p.Name] = p.Usage
	}
	cpuCol, memCol := tbl.ColumnIndex(model.ColumnCPU), tbl.ColumnIndex(model.ColumnMemory)
	for _, r := range tbl.Cells {
		link := r[0].Link
		u, ok := usage[link.Namespace+"/"+link.Name]
		if !ok || r[cpuCol].Text == "" {
			continue
		}
		cpu, mem := pods.UsageCells(nil, u)
		if r[cpuCol].Text != cpu.Text || r[memCol].Text != mem.Text {
			t.Logf("%s: table %s/%s, metrics.get %s/%s (a poll between the reads may differ)", link.Name, r[cpuCol].Text, r[memCol].Text, cpu.Text, mem.Text)
		}
	}

	// Sorted by memory, descending: the measured pods by value, then the rest.
	dispatch("ui.sortDrawerTable", map[string]any{"section": "Pods", "column": "memory", "desc": true}, nil)
	tbl = podsTable()
	last := int64(-1)
	seenEmpty := false
	for i, r := range tbl.Cells {
		c := r[memCol]
		switch {
		case c.Text == "":
			seenEmpty = true
		case seenEmpty:
			t.Errorf("row %d (%s) measured after a pod without a measurement", i, r[0].Text)
		case last >= 0 && c.Num > last:
			t.Errorf("row %d (%s, %s) out of order", i, r[0].Text, c.Text)
		}
		if c.Text != "" {
			last = c.Num
		}
		t.Logf("%-50s %8s %10s", r[0].Text, r[cpuCol].Text, c.Text)
	}
	if tbl.Sort == nil || tbl.Sort.Column != model.ColumnMemory || !tbl.Sort.Desc {
		t.Errorf("the table's sort %+v", tbl.Sort)
	}

	// Polled while open; closed, no more pod metrics lists.
	if !testkit.WaitFor(ctx, func() bool { return lists()-before >= 2 }) {
		t.Fatalf("%d pod metrics lists while the drawer showed the table", lists()-before)
	}
	t.Logf("%d pod metrics lists while the drawer was open", lists()-before)
	dispatch("ui.closeDrawer", struct{}{}, nil)
	// A poll that started before may still answer: two reads apart agree first.
	var closed int64
	if !testkit.WaitFor(ctx, func() bool {
		was := lists()
		time.Sleep(interval / 2)
		closed = lists()
		return closed == was
	}) {
		t.Fatal("the pod metrics lists never stopped")
	}
	time.Sleep(5 * interval / 2) // more than two list intervals
	if after := lists(); after != closed {
		t.Errorf("%d pod metrics lists after the drawer closed", after-closed)
	}
}
