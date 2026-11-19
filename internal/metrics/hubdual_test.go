package metrics_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The owner's evening requirements (G12) on a hub over both providers, with a clock the test moves
// and polls the test starts (metrics.Kick): history from Prometheus once, then the Metrics Server's
// live values; the network and the filesystem only while shown; falling back to one provider only
// while the other fails; the seam without a point twice and the gap filled once; the status line's
// data.

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// liveFake is a Metrics Server: the current values, measured at the clock's time.
type liveFake struct {
	mu    sync.Mutex
	clk   *testClock
	err   error
	gets  int
	lists int
}

func (f *liveFake) ID() string                         { return metrics.ServerID }
func (f *liveFake) Label() string                      { return metrics.ServerLabel }
func (f *liveFake) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }

func (f *liveFake) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *liveFake) counts() (gets, lists int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.lists
}

func (f *liveFake) pod(ns, name string) model.PodUsage {
	return model.PodUsage{Namespace: ns, Name: name, Timestamp: f.clk.Now(), Containers: []model.ContainerUsage{
		{Name: "app", Usage: model.Usage{CPUMilli: 100, MemoryBytes: 64 << 20}},
	}}
}

func (f *liveFake) ListPods(context.Context, string) ([]model.PodUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.err != nil {
		return nil, f.err
	}
	return []model.PodUsage{f.pod("web", "a")}, nil
}

func (f *liveFake) GetPod(_ context.Context, ns, name string) (model.PodUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.err != nil {
		return model.PodUsage{}, f.err
	}
	return f.pod(ns, name), nil
}

func (f *liveFake) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }

func (f *liveFake) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

// histFake is a Prometheus: range queries on the step's grid, instant queries for the lists.
type histFake struct {
	mu       sync.Mutex
	clk      *testClock
	err      error
	queries  []metrics.RangeQuery
	instants int
}

func (f *histFake) ID() string    { return metrics.PrometheusID }
func (f *histFake) Label() string { return "Prometheus (monitoring/prometheus-server)" }
func (f *histFake) Where() string { return "monitoring/prometheus-server:http" }

func (f *histFake) ServiceRef() (namespace, name string) { return "monitoring", "prometheus-server" }
func (f *histFake) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{History: true, Network: true, Filesystem: true}
}

func (f *histFake) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *histFake) ranges() []metrics.RangeQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.queries)
}

func (f *histFake) instantCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instants
}

func (f *histFake) ObjectRange(_ context.Context, q metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if f.err != nil {
		return nil, f.err
	}
	want := q.Want
	if want == 0 {
		want = metrics.WantAll
	}
	out := &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{"": {}, "app": {}}, From: q.From, To: q.To, Step: q.Step}
	for t := q.From; !t.After(q.To); t = t.Add(q.Step) {
		if want.Has(metrics.WantUsage) {
			u := model.Usage{CPUMilli: 300, MemoryBytes: 96 << 20}
			out.Targets[""].Usage = append(out.Targets[""].Usage, model.UsageSample{At: t, Usage: u})
			out.Targets["app"].Usage = append(out.Targets["app"].Usage, model.UsageSample{At: t, Usage: u})
		}
		if want.Has(metrics.WantNetwork) {
			out.Targets[""].Network = append(out.Targets[""].Network, model.NetworkSample{At: t, ReceiveBytesPerSec: 1000, TransmitBytesPerSec: 500})
		}
		if want.Has(metrics.WantFilesystem) {
			out.FilesystemSource = metrics.FilesystemUsage
			out.Targets["app"].Filesystem = append(out.Targets["app"].Filesystem, model.FilesystemSample{At: t, UsageBytes: 4096})
		}
	}
	return out, nil
}

func (f *histFake) ListPods(context.Context, string) ([]model.PodUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instants++
	if f.err != nil {
		return nil, f.err
	}
	return []model.PodUsage{{Namespace: "web", Name: "a", Timestamp: f.clk.Now(), Containers: []model.ContainerUsage{{Name: "app", Usage: model.Usage{CPUMilli: 300, MemoryBytes: 96 << 20}}}}}, nil
}

func (f *histFake) GetPod(ctx context.Context, ns, name string) (model.PodUsage, error) {
	l, err := f.ListPods(ctx, ns)
	if err != nil {
		return model.PodUsage{}, err
	}
	return l[0], nil
}

func (f *histFake) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }

func (f *histFake) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

// dual is a hub over both fakes with the test's clock; its drawer polls only when the test kicks it.
type dual struct {
	clk        *testClock
	live       *liveFake
	hist       *histFake
	liveDet    *countingDetector
	histDet    *countingDetector
	h          *metrics.Hub
	polls      chan int
	n          int
	sub        *metrics.Subscription
	pollsSoFar int
}

// identified is a detector that names its provider before it found one, as the Metrics Server and
// PrometheusDetector do.
type identified struct {
	*countingDetector
	id, label string
}

func (d identified) ID() string    { return d.id }
func (d identified) Label() string { return d.label }

func newDual(t *testing.T) *dual {
	t.Helper()
	clk := &testClock{t: time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC)}
	d := &dual{clk: clk, live: &liveFake{clk: clk}, hist: &histFake{clk: clk}, polls: make(chan int, 64)}
	d.liveDet, d.histDet = detectorFor(d.live), detectorFor(d.hist)
	live := identified{d.liveDet, metrics.ServerID, metrics.ServerLabel}
	hist := identified{d.histDet, metrics.PrometheusID, metrics.PrometheusLabel}
	d.h = metrics.NewHubFor(metrics.Sources{Live: live, History: hist}, metrics.Options{
		ObjectInterval: time.Hour, ListInterval: time.Hour, Now: clk.Now,
	})
	t.Cleanup(d.h.Close)
	return d
}

// open subscribes to pod web/a's drawer and waits for its first poll.
func (d *dual) open(t *testing.T) {
	t.Helper()
	d.sub = d.h.Subscribe(podA, func() { d.n++; d.polls <- d.n })
	t.Cleanup(d.sub.Release)
	d.wait(t)
}

// wait waits for the next poll.
func (d *dual) wait(t *testing.T) {
	t.Helper()
	d.pollsSoFar++
	waitPoll(t, d.polls, d.pollsSoFar)
}

// next moves the clock and polls once.
func (d *dual) next(t *testing.T, dt time.Duration) {
	t.Helper()
	d.clk.Advance(dt)
	metrics.Kick(d.h, podA)
	d.wait(t)
}

func (d *dual) chart(t *testing.T) []model.UsageSample {
	t.Helper()
	hist := d.h.History(podA)
	if hist == nil || hist.Targets[""] == nil {
		t.Fatalf("no chart: %+v", hist)
	}
	pts := hist.Targets[""].Usage
	for i := 1; i < len(pts); i++ {
		if !pts[i].At.After(pts[i-1].At) {
			t.Fatalf("points %d and %d are not in time order (a point twice?): %v, %v", i-1, i, pts[i-1].At, pts[i].At)
		}
	}
	return pts
}

func provider(ps []metrics.ProviderStatus, id string) metrics.ProviderStatus {
	for _, p := range ps {
		if p.Provider == id {
			return p
		}
	}
	return metrics.ProviderStatus{}
}

func unavailable() error {
	return apierrors.NewServiceUnavailable("the server is currently unable to handle the request")
}

func TestHistoryThenLiveValues(t *testing.T) {
	d := newDual(t)
	d.open(t)
	qs := d.hist.ranges()
	now := d.clk.Now()
	if len(qs) != 1 || qs[0].Want != metrics.WantUsage || qs[0].Step != 30*time.Second || !qs[0].To.Equal(now) || !qs[0].From.Equal(now.Add(-time.Hour)) {
		t.Fatalf("the drawer's first poll asked Prometheus %+v, want the hour's CPU and memory once", qs)
	}
	if gets, _ := d.live.counts(); gets != 1 {
		t.Errorf("%d Metrics Server gets at the first poll", gets)
	}
	pts := d.chart(t)
	if len(pts) != 121 || pts[len(pts)-1].CPUMilli != 300 {
		t.Errorf("the chart after the first poll: %d points ending at %+v, want the history's 121", len(pts), pts[len(pts)-1])
	}

	// The next polls: the Metrics Server's measurements continue the chart; Prometheus is not asked.
	for range 3 {
		d.next(t, 15*time.Second)
	}
	if n := len(d.hist.ranges()); n != 1 {
		t.Errorf("Prometheus was asked %d times in 4 polls showing CPU, want once", n)
	}
	if gets, _ := d.live.counts(); gets != 4 {
		t.Errorf("%d Metrics Server gets in 4 polls", gets)
	}
	pts = d.chart(t)
	tail := pts[len(pts)-3:]
	if tail[0].CPUMilli != 100 || tail[1].CPUMilli != 100 || tail[2].CPUMilli != 100 || !tail[2].At.Equal(d.clk.Now()) || pts[len(pts)-4].CPUMilli != 300 {
		t.Errorf("the chart ends %+v, want the history, then 3 live values", pts[len(pts)-4:])
	}
	// An hour back from the newest point, no more: the history's two oldest points are gone.
	if len(pts) != 122 || pts[0].At.Before(d.clk.Now().Add(-time.Hour)) {
		t.Errorf("the chart has %d points from %v", len(pts), pts[0].At)
	}
	if p, ok := d.h.Pod("web", "a"); !ok || p.Total().CPUMilli != 100 {
		t.Errorf("current values = %+v (%v), want the Metrics Server's", p, ok)
	}
	if st := d.h.State(podA); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server) and Kubernetes Metrics Server" {
		t.Errorf("state = %+v", st)
	}
	ps := d.h.Providers()
	if len(ps) != 2 || ps[0].Provider != metrics.PrometheusID || ps[1].Provider != metrics.ServerID {
		t.Fatalf("providers = %+v, want Prometheus, then the Metrics Server", ps)
	}
	prom, ms := ps[0], ps[1]
	if prom.Status != metrics.StatusAvailable || prom.Service != "monitoring/prometheus-server:http" || prom.Label != "Prometheus" ||
		prom.Namespace != "monitoring" || prom.ServiceName != "prometheus-server" || // the source line's links
		!slices.Equal(prom.Serves, []string{metrics.ServesHistory, metrics.ServesNetwork, metrics.ServesFilesystem}) || prom.LastOK.IsZero() {
		t.Errorf("Prometheus = %+v", prom)
	}
	if ms.Status != metrics.StatusAvailable || ms.Label != metrics.ServerLabel || !slices.Equal(ms.Serves, []string{metrics.ServesLive}) || !ms.LastOK.Equal(d.clk.Now()) || ms.LastError != "" {
		t.Errorf("Metrics Server = %+v", ms)
	}

	// A range change asks Prometheus once more, at the new step; the same range again does not.
	d.sub.SetWindows(5*time.Minute, time.Hour) // the pod's chart at five minutes, a container's at the default hour
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 2 || qs[1].Step != 15*time.Second || qs[1].Want != metrics.WantUsage || !qs[1].From.Equal(d.clk.Now().Add(-time.Hour).Truncate(15*time.Second)) {
		t.Errorf("after the range change Prometheus was asked %+v, want the hour once more at 15 s", qs[1:])
	}
	d.sub.SetWindows(5*time.Minute, time.Hour)
	d.next(t, 15*time.Second)
	if n := len(d.hist.ranges()); n != 2 {
		t.Errorf("an unchanged range asked Prometheus again: %d queries", n)
	}
	d.chart(t)
}

func TestNetworkAndFilesystemOnlyWhileShown(t *testing.T) {
	d := newDual(t)
	d.open(t)
	d.sub.SetWants(metrics.WantNetwork)
	d.wait(t)
	qs := d.hist.ranges()
	if len(qs) != 2 || qs[1].Want != metrics.WantNetwork || !qs[1].From.Equal(d.clk.Now().Add(-time.Hour).Truncate(30*time.Second)) {
		t.Fatalf("showing the network asked %+v, want its hour once", qs[1:])
	}
	// 15 s later the step (30 s) has not passed: nothing; 30 s later: from the last point on.
	d.next(t, 15*time.Second)
	if n := len(d.hist.ranges()); n != 2 {
		t.Errorf("the network was asked for within its step: %d queries", n)
	}
	last := d.h.History(podA).Targets[""].Network
	d.next(t, 15*time.Second)
	qs = d.hist.ranges()
	if len(qs) != 3 || qs[2].Want != metrics.WantNetwork || !qs[2].From.Equal(last[len(last)-1].At) {
		t.Errorf("after a step the network was asked %+v, want from its last point %v", qs[2:], last[len(last)-1].At)
	}
	net := d.h.History(podA).Targets[""].Network
	for i := 1; i < len(net); i++ {
		if !net[i].At.After(net[i-1].At) {
			t.Fatalf("network points out of order: %v %v", net[i-1].At, net[i].At)
		}
	}
	// The filesystem too, and then neither: nothing more, and their points are gone.
	d.sub.SetWants(metrics.WantNetwork | metrics.WantFilesystem)
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 4 || qs[3].Want != metrics.WantFilesystem {
		t.Errorf("showing the filesystem asked %+v", qs[3:])
	}
	if h := d.h.History(podA); h.FilesystemSource != metrics.FilesystemUsage || len(h.Targets["app"].Filesystem) == 0 {
		t.Errorf("filesystem = %q %d points", h.FilesystemSource, len(h.Targets["app"].Filesystem))
	}
	d.sub.SetWants(0)
	d.wait(t)
	d.next(t, time.Minute)
	if n := len(d.hist.ranges()); n != 4 {
		t.Errorf("Prometheus was asked %d times after no chart showed the network or the filesystem, want 4", n)
	}
	if h := d.h.History(podA); len(h.Targets[""].Network) != 0 || h.FilesystemSource != "" {
		t.Errorf("the network's or the filesystem's points stayed: %+v", h.Targets[""])
	}
}

func TestMetricsServerFailsPrometheusTakesOver(t *testing.T) {
	d := newDual(t)
	d.open(t)
	d.next(t, 15*time.Second)
	d.live.setErr(unavailable())

	// The poll the Metrics Server fails in: Prometheus gives the live values at once, from the
	// chart's last point.
	lastLive := d.chart(t)[len(d.chart(t))-1].At
	d.next(t, 15*time.Second)
	qs := d.hist.ranges()
	if len(qs) != 2 || qs[1].Want != metrics.WantUsage || !qs[1].From.Equal(lastLive.Truncate(30*time.Second)) || !qs[1].To.Equal(d.clk.Now()) {
		t.Fatalf("the fallback asked %+v, want CPU and memory from %v", qs[1:], lastLive)
	}
	pts := d.chart(t)
	if !pts[len(pts)-1].At.Equal(d.clk.Now()) || pts[len(pts)-1].CPUMilli != 300 {
		t.Errorf("the chart's newest point = %+v, want Prometheus's at now", pts[len(pts)-1])
	}
	ps := d.h.Providers()
	ms, prom := provider(ps, metrics.ServerID), provider(ps, metrics.PrometheusID)
	if ms.Status != metrics.StatusUnavailable || ms.LastError != "unavailable (HTTP 503)" || !ms.LastErrorAt.Equal(d.clk.Now()) || len(ms.Serves) != 0 {
		t.Errorf("Metrics Server = %+v", ms)
	}
	if !slices.Contains(prom.Serves, metrics.ServesLive) || !slices.Contains(prom.Serves, metrics.ServesHistory) {
		t.Errorf("Prometheus = %+v, want it to serve the live values too", prom)
	}
	if st := d.h.State(podA); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server)" {
		t.Errorf("state = %+v", st)
	}

	// While the Metrics Server is left alone (a minute), Prometheus serves the live values.
	gets, _ := d.live.counts()
	d.next(t, 30*time.Second)
	if g, _ := d.live.counts(); g != gets {
		t.Errorf("the failed Metrics Server was asked again within a minute")
	}
	if n := len(d.hist.ranges()); n != 3 {
		t.Errorf("%d Prometheus queries, want one more for the live values", n)
	}

	// It answers again: the normal case is back, and Prometheus is not asked for live values.
	d.live.setErr(nil)
	d.next(t, time.Minute)
	if g, _ := d.live.counts(); g != gets+1 {
		t.Errorf("the Metrics Server was not asked again after its minute")
	}
	n := len(d.hist.ranges())
	d.next(t, 15*time.Second)
	if m := len(d.hist.ranges()); m != n {
		t.Errorf("Prometheus was asked for live values after the Metrics Server came back")
	}
	ps = d.h.Providers()
	if ms = provider(ps, metrics.ServerID); ms.Status != metrics.StatusAvailable || !slices.Equal(ms.Serves, []string{metrics.ServesLive}) || ms.LastError == "" {
		t.Errorf("Metrics Server after it came back = %+v (the last error stays for the status line)", ms)
	}
	pts = d.chart(t)
	for i := 1; i < len(pts); i++ {
		if gap := pts[i].At.Sub(pts[i-1].At); gap > time.Minute {
			t.Errorf("a gap of %s in the chart at %v", gap, pts[i].At)
		}
	}
}

func TestPrometheusGoneLeavesTheLiveValues(t *testing.T) {
	d := newDual(t)
	d.hist.setErr(unavailable())
	d.open(t)
	if st := d.h.State(podA); !st.Usable() || st.Label != metrics.ServerLabel {
		t.Errorf("state without Prometheus = %+v", st)
	}
	prom := provider(d.h.Providers(), metrics.PrometheusID)
	if prom.Status != metrics.StatusUnavailable || prom.LastError != "unavailable (HTTP 503)" || len(prom.Serves) != 0 {
		t.Errorf("Prometheus = %+v", prom)
	}
	if pts := d.chart(t); len(pts) != 1 || pts[0].CPUMilli != 100 {
		t.Errorf("chart = %+v, want the Metrics Server's measurement", pts)
	}
	// Left alone for a minute; then asked for the history again, and it answers.
	d.next(t, 15*time.Second)
	if n := len(d.hist.ranges()); n != 1 {
		t.Errorf("the failed Prometheus was asked %d times within its minute", n)
	}
	d.hist.setErr(nil)
	d.next(t, time.Minute)
	qs := d.hist.ranges()
	if len(qs) != 2 || qs[1].Want != metrics.WantUsage || !qs[1].From.Equal(d.clk.Now().Add(-time.Hour).Truncate(30*time.Second)) {
		t.Errorf("Prometheus back: asked %+v, want the hour's history", qs[1:])
	}
	if pts := d.chart(t); len(pts) < 100 {
		t.Errorf("the chart has %d points after Prometheus came back, want its history", len(pts))
	}
	if prom = provider(d.h.Providers(), metrics.PrometheusID); !slices.Contains(prom.Serves, metrics.ServesHistory) {
		t.Errorf("Prometheus back = %+v", prom)
	}
}

func TestGapIsFilledOnceFromPrometheus(t *testing.T) {
	d := newDual(t)
	d.open(t)
	before := d.chart(t)[len(d.chart(t))-1].At
	// Both fail for two minutes.
	d.live.setErr(unavailable())
	d.hist.setErr(unavailable())
	d.next(t, 15*time.Second)
	if st := d.h.State(podA); st.Status != metrics.StatusUnavailable {
		t.Errorf("state with both failing = %+v", st)
	}
	d.next(t, time.Minute)
	d.next(t, time.Minute)
	// The Metrics Server comes back first: its measurement leaves a gap.
	d.live.setErr(nil)
	d.next(t, time.Minute)
	n := len(d.hist.ranges())
	// Prometheus comes back: the gap is filled once, CPU and memory between the two points.
	d.hist.setErr(nil)
	d.next(t, time.Minute)
	qs := d.hist.ranges()
	if len(qs) != n+1 || qs[n].Want != metrics.WantUsage || !qs[n].From.Equal(before.Truncate(30*time.Second)) {
		t.Fatalf("Prometheus back: asked %+v, want the gap from %v once", qs[n:], before)
	}
	pts := d.chart(t)
	var inGap int
	for _, p := range pts {
		if p.At.After(before) && p.CPUMilli == 300 {
			inGap++
		}
	}
	if inGap < 5 {
		t.Errorf("%d of Prometheus's points in the gap, want the gap filled", inGap)
	}
	d.next(t, 15*time.Second)
	if m := len(d.hist.ranges()); m != n+1 {
		t.Errorf("the gap was asked for again: %d queries", m-n)
	}
}

func TestListsUseTheMetricsServerAndFallBack(t *testing.T) {
	d := newDual(t)
	ctx := context.Background()
	r := d.h.Fetch(ctx, allPods)
	if !r.State.Usable() || r.State.Provider != metrics.ServerID || len(r.Pods) != 1 {
		t.Fatalf("list = %+v", r)
	}
	if n := d.histDet.n.Load(); n != 0 {
		t.Errorf("Prometheus was looked for %d times while the Metrics Server answers the list", n)
	}
	if p := provider(d.h.Providers(), metrics.PrometheusID); p.Status != metrics.StatusUnknown {
		t.Errorf("Prometheus not looked for = %+v, want unknown", p)
	}
	// The Metrics Server fails: Prometheus is looked for and answers in the same read.
	d.live.setErr(unavailable())
	d.clk.Advance(time.Minute)
	r = d.h.Fetch(ctx, allPods)
	if !r.State.Usable() || r.State.Provider != metrics.PrometheusID || len(r.Pods) != 1 || r.Pods[0].Total().CPUMilli != 300 {
		t.Errorf("list with the Metrics Server failing = %+v", r)
	}
	if d.hist.instantCount() != 1 {
		t.Errorf("%d instant queries", d.hist.instantCount())
	}
	// Within its minute the Metrics Server is not asked; then it is, and serves again.
	d.live.setErr(nil)
	d.clk.Advance(20 * time.Second)
	_, lists := d.live.counts()
	d.h.Fetch(ctx, allPods)
	if _, l := d.live.counts(); l != lists || d.hist.instantCount() != 2 {
		t.Errorf("within the minute: %d Metrics Server lists, %d instant queries", l-lists, d.hist.instantCount())
	}
	d.clk.Advance(time.Minute)
	r = d.h.Fetch(ctx, allPods)
	if r.State.Provider != metrics.ServerID || d.hist.instantCount() != 2 {
		t.Errorf("after the minute: %+v, %d instant queries", r.State, d.hist.instantCount())
	}
}

// Prometheus alone serves the live values at its step; a poll within the step (a chart's variant
// changed to one already held) asks nothing and keeps what the last poll found (it once turned the
// state into the Metrics Server's "not served" and blanked the charts).
func TestPrometheusAloneKeepsItsStateBetweenSteps(t *testing.T) {
	d := newDual(t)
	d.liveDet.set(nil, metrics.State{Status: metrics.StatusNotServed, Message: "no metrics.k8s.io"})
	d.open(t)
	d.next(t, 5*time.Second)
	if n := len(d.hist.ranges()); n != 1 {
		t.Errorf("%d queries within the step, want the history only", n)
	}
	if st := d.h.State(podA); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server)" {
		t.Errorf("state after a poll with nothing due = %+v", st)
	}
	d.next(t, 30*time.Second)
	qs := d.hist.ranges()
	if len(qs) != 2 || qs[1].Want != metrics.WantUsage || !qs[1].To.Equal(d.clk.Now()) {
		t.Errorf("after the step: %+v, want the newest CPU and memory", qs[1:])
	}
	if p := provider(d.h.Providers(), metrics.PrometheusID); !slices.Equal(p.Serves, []string{metrics.ServesHistory, metrics.ServesLive, metrics.ServesNetwork, metrics.ServesFilesystem}) {
		t.Errorf("Prometheus alone = %+v", p)
	}
	d.chart(t)
}

// Both missing: the most telling state; a Prometheus the user may not read is said so.
func TestNeitherProviderIsAState(t *testing.T) {
	d := newDual(t)
	d.liveDet.set(nil, metrics.State{Status: metrics.StatusNotServed, Message: "no metrics.k8s.io"})
	d.histDet.set(nil, metrics.State{Status: metrics.StatusForbidden, Message: "may not use the service proxy", Reason: "forbidden (HTTP 403)"})
	r := d.h.Fetch(context.Background(), allPods)
	if r.State.Status != metrics.StatusForbidden {
		t.Errorf("state = %+v, want forbidden", r.State)
	}
	ps := d.h.Providers()
	if p := provider(ps, metrics.PrometheusID); p.Status != metrics.StatusForbidden || p.LastError != "forbidden (HTTP 403)" {
		t.Errorf("Prometheus = %+v", p)
	}
	if p := provider(ps, metrics.ServerID); p.Status != metrics.StatusNotServed || p.LastError != "" {
		t.Errorf("Metrics Server = %+v", p)
	}
	if c := d.h.Capabilities(); c != (metrics.Capabilities{}) {
		t.Errorf("capabilities without a provider = %+v", c)
	}
}

// The status line's last error is a class: a body the service sent never reaches it.
func TestErrorClassesCarryNoBodies(t *testing.T) {
	gr := schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{apierrors.NewGenericServerResponse(500, "get", gr, "", "token=s3cr3t in a body", 0, true), "server error (HTTP 500)"},
		{apierrors.NewGenericServerResponse(403, "get", gr, "", "token=s3cr3t in a body", 0, true), "forbidden (HTTP 403)"},
		{apierrors.NewServiceUnavailable("no endpoints available for service \"prometheus-server\""), "unavailable (HTTP 503)"},
		{apierrors.NewNotFound(gr, "a"), "not found (HTTP 404)"},
		{context.DeadlineExceeded, "timeout"},
		{errors.New("token=s3cr3t"), "request failed"},
	} {
		if got := metrics.ErrorClass(tc.err); got != tc.want {
			t.Errorf("class of %v = %q, want %q", tc.err, got, tc.want)
		}
		st := metrics.StateFromError(tc.err, "none")
		if strings.Contains(st.Message, "s3cr3t") || strings.Contains(st.Reason, "s3cr3t") {
			t.Errorf("a body reached the state: %+v", st)
		}
	}
}
