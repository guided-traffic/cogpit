package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// metricsListInterval is metricsSession's list interval.
const metricsListInterval = 150 * time.Millisecond

// metricsSession connects the small demo cluster with short poll intervals.
func metricsSession(t *testing.T) (*engine.Session, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{
		IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 300, NoChurn: true, Seed: 7, MetricsResolution: 30 * time.Millisecond},
		MetricsOptions: metrics.Options{
			ListInterval: metricsListInterval, ObjectInterval: 40 * time.Millisecond, TTL: 30 * time.Millisecond,
		},
	}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, engine.DemoContext); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func demoRequests(t *testing.T, s *engine.Session) int64 {
	t.Helper()
	stats, ok := s.Backend().DemoStats().(demo.Stats)
	if !ok {
		t.Fatal("no demo stats")
	}
	return stats.MetricsRequests
}

// quietRequests waits until the demo's Metrics Server served no request for a list interval and
// returns its count: a poll under way when the last scope was released may still land.
func quietRequests(ctx context.Context, t *testing.T, s *engine.Session) int64 {
	t.Helper()
	var n int64
	if !testkit.WaitFor(ctx, func() bool {
		before := demoRequests(t, s)
		time.Sleep(metricsListInterval)
		n = demoRequests(t, s)
		return n == before
	}) {
		t.Fatal("the metrics requests never stopped")
	}
	return n
}

func cellOf(tbl *engine.TableSnapshot, row int, col string) model.Cell {
	for i, c := range tbl.Columns {
		if c.ID == col {
			return tbl.Rows[row].Cells[i]
		}
	}
	return model.Cell{}
}

// The Pods list shows CPU and Memory while it is open, polls only then, and sorts by them.
func TestPodsListMetricsFollowTheOpenView(t *testing.T) {
	s, ctx := metricsSession(t)
	pods := func() *engine.TableSnapshot { return s.Table("pods") }

	usage := func(tbl *engine.TableSnapshot) (withCPU, withMem int) {
		for i := range tbl.Rows {
			if cellOf(tbl, i, "cpu").Text != "" {
				withCPU++
			}
			if strings.HasSuffix(cellOf(tbl, i, "memory").Text, "iB") {
				withMem++
			}
		}
		return withCPU, withMem
	}
	// Only running pods have a measurement; the demo has plenty of them. The usage reaches the
	// rows with the next snapshot after the poll, so wait for the state asserted, not for a first
	// cell.
	var tbl *engine.TableSnapshot
	withCPU, withMem := 0, 0
	if !testkit.WaitFor(ctx, func() bool {
		tbl = pods()
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 {
			return false
		}
		withCPU, withMem = usage(tbl)
		return withCPU >= len(tbl.Rows)/2 && withMem == withCPU
	}) {
		t.Fatal("the Pods list never showed CPU")
	}
	if withCPU < len(tbl.Rows)/2 || withMem != withCPU {
		t.Errorf("%d of %d pods show CPU, %d show memory", withCPU, len(tbl.Rows), withMem)
	}
	t.Logf("%d of %d pods show usage; first: cpu %q memory %q", withCPU, len(tbl.Rows), cellOf(tbl, 0, "cpu").Text, cellOf(tbl, 0, "memory").Text)

	// Sorting by CPU, descending, puts the biggest consumer first and the pods without a measurement last.
	s.UpdateUI(func(u *engine.UIState) { u.Sort = engine.SortSpec{Column: "cpu", Desc: true} })
	if !testkit.WaitFor(ctx, func() bool {
		t := pods()
		return t != nil && t.Sort.Column == "cpu" && len(t.Rows) > 2 && cellOf(t, 0, "cpu").Num > 0
	}) {
		t.Fatal("the list never sorted by CPU")
	}
	tbl = pods()
	for i := 1; i < len(tbl.Rows); i++ {
		a, b := cellOf(tbl, i-1, "cpu"), cellOf(tbl, i, "cpu")
		if b.Text != "" && (a.Text == "" || a.Num < b.Num) {
			t.Fatalf("row %d (cpu %q) sorts before row %d (cpu %q)", i-1, a.Text, i, b.Text)
		}
	}

	// Away from the metrics views the polls stop: no request in several list intervals.
	s.UpdateUI(func(u *engine.UIState) { u.View = "services"; u.Sort = engine.DefaultSort })
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().Metrics.Subscribed() == 0 }) {
		t.Fatalf("%d scopes still polled after leaving the Pods view", s.Backend().Metrics.Subscribed())
	}
	before := quietRequests(ctx, t, s)
	time.Sleep(500 * time.Millisecond) // more than three list intervals
	if after := demoRequests(t, s); after != before {
		t.Errorf("%d metrics requests while no view showed metrics", after-before)
	}
}

// A pod's drawer polls its pod (and only while open), shows requests and limits, and gathers the
// series for the chart; the list's poll is shared and the drawer adds one scope.
func TestPodDrawerMetrics(t *testing.T) {
	s, ctx := metricsSession(t)
	var ref model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for i := range tbl.Rows {
			if cellOf(tbl, i, "cpu").Text != "" {
				ref = tbl.Rows[i].Ref
				return true
			}
		}
		return false
	}) {
		t.Fatal("no pod with metrics")
	}

	if m, err := s.ObjectMetrics(model.ObjectRef{Kind: "services", Namespace: ref.Namespace, Name: "x"}); err != nil || m != nil {
		// A kind without metrics has no section; an unknown service is an error or nil, never a panic.
		t.Logf("services: %v, %v", m, err)
	}

	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	var m *engine.ObjectMetrics
	if !testkit.WaitFor(ctx, func() bool {
		var err error
		m, err = s.ObjectMetrics(ref)
		return err == nil && m != nil && len(m.Targets) > 0 && len(m.Targets[0].Samples) >= 4
	}) {
		t.Fatalf("the drawer's series never grew: %+v", m)
	}
	if m.Status != metrics.StatusAvailable || m.Source != "Kubernetes Metrics Server" || m.Message != "" {
		t.Errorf("status %q source %q message %q", m.Status, m.Source, m.Message)
	}
	pod := m.Targets[0]
	if pod.Name != "" || pod.Current == nil || pod.Current.CPUMilli <= 0 || pod.Current.MemoryBytes <= 0 {
		t.Fatalf("pod target = %+v", pod)
	}
	if len(m.Targets) < 2 {
		t.Fatalf("no container targets: %+v", m.Targets)
	}
	for _, c := range m.Targets[1:] {
		if c.Name == "" || c.Current == nil || len(c.Samples) == 0 {
			t.Errorf("container target %+v", c)
		}
	}
	t.Logf("pod %s: %d samples, current %+v, requests %+v, limits %+v", ref.Name, len(pod.Samples), *pod.Current, pod.Requests, pod.Limits)

	// Closing the drawer ends its poll; its series is not shown while it is closed.
	subscribed := s.Backend().Metrics.Subscribed()
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = nil })
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().Metrics.Subscribed() == subscribed-1 }) {
		t.Fatalf("%d scopes polled after closing the drawer, was %d", s.Backend().Metrics.Subscribed(), subscribed)
	}
	if m, _ := s.ObjectMetrics(ref); m != nil && len(m.Targets) > 0 && len(m.Targets[0].Samples) != 0 {
		t.Errorf("the series shows with the drawer closed: %d samples", len(m.Targets[0].Samples))
	}
	if charts, _ := s.Backend().Metrics.Retained(); charts != 1 {
		t.Errorf("%d charts kept after the drawer closed, want 1", charts)
	}

	// Opened again: the chart starts with what it measured before (G12, history that outlives a
	// closed drawer): its first point is the old one, not a new measurement.
	first := pod.Samples[0].At
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) > 0 && len(m.Targets[0].Samples) > len(pod.Samples)
	}) {
		t.Fatalf("the reopened drawer's series: %+v", m)
	}
	if got := m.Targets[0].Samples[0].At; !got.Equal(first) {
		t.Errorf("the reopened drawer's chart starts at %v, want the kept first point %v", got, first)
	}
}

func TestNodesListAndDrawerMetrics(t *testing.T) {
	s, ctx := metricsSession(t)
	s.UpdateUI(func(u *engine.UIState) { u.View = "nodes" })
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("nodes")
		return tbl != nil && tbl.Synced && len(tbl.Rows) > 0 && strings.HasSuffix(cellOf(tbl, 0, "cpu").Text, "%")
	}) {
		t.Fatalf("the Nodes list never showed CPU as a share: %+v", s.Table("nodes"))
	}
	tbl := s.Table("nodes")
	var ref model.ObjectRef
	for i := range tbl.Rows {
		if cellOf(tbl, i, "cpu").Text != "" {
			ref = tbl.Rows[i].Ref
			break
		}
	}
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	var m *engine.ObjectMetrics
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 1 && m.Targets[0].Current != nil && len(m.Targets[0].Samples) >= 2
	}) {
		t.Fatalf("node metrics: %+v", m)
	}
	n := m.Targets[0]
	if n.Allocatable.CPUMilli <= 0 || n.Allocatable.MemoryBytes <= 0 || n.Capacity.CPUMilli < n.Allocatable.CPUMilli {
		t.Errorf("node allocatable %+v capacity %+v", n.Allocatable, n.Capacity)
	}
}

// The agents' command: one request for a fresh read, none for the next, no polling afterwards, a
// state instead of an error when a kind has no metrics.
func TestMetricsGetCommand(t *testing.T) {
	s, ctx := metricsSession(t)
	s.UpdateUI(func(u *engine.UIState) { u.View = "services" }) // nothing visible polls
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().Metrics.Subscribed() == 0 }) {
		t.Fatal("polls still running")
	}
	base := quietRequests(ctx, t, s)

	call := func(params string) *engine.MetricsResult {
		t.Helper()
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "metrics.get", Params: json.RawMessage(params), Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("metrics.get %s: %v", params, err)
		}
		return res.(*engine.MetricsResult)
	}
	r := call(`{"kind":"nodes"}`)
	if r.Status != metrics.StatusAvailable || len(r.Nodes) == 0 || r.Nodes[0].CPUMilli <= 0 || r.Source != "Kubernetes Metrics Server" {
		t.Fatalf("nodes: %+v", r)
	}
	if got := demoRequests(t, s) - base; got != 1 {
		t.Errorf("%d requests for the first read, want 1", got)
	}
	call(`{"kind":"nodes"}`) // within the TTL... the test's TTL is 30 ms: allow it to have passed
	r = call(`{"kind":"pods","namespace":"default","limit":3}`)
	if r.Status != metrics.StatusAvailable || len(r.Pods) > 3 || (r.Total > 3 && !r.Truncated) {
		t.Errorf("pods: total %d, %d items, truncated %v, status %q", r.Total, len(r.Pods), r.Truncated, r.Status)
	}
	for _, p := range r.Pods {
		if p.Namespace != "default" || p.CPUMilli <= 0 || len(p.Containers) == 0 {
			t.Errorf("pod item %+v", p)
		}
	}
	if s.Backend().Metrics.Subscribed() != 0 {
		t.Error("a one-off read started a poll")
	}
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "metrics.get", Params: json.RawMessage(`{"kind":"services"}`), Origin: engine.OriginExternalAgent}); err == nil {
		t.Error("a kind without metrics was accepted")
	}
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "metrics.get", Params: json.RawMessage(`{"kind":"pods","name":"x"}`), Origin: engine.OriginExternalAgent}); err == nil {
		t.Error("a pod without a namespace was accepted")
	}
}
