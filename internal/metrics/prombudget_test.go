package metrics_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// promHub is a hub over the detector chain of a fake cluster that has the fake Prometheus.
func promHub(t *testing.T, c *promCluster, opts metrics.Options) *metrics.Hub {
	t.Helper()
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	det, _, err := metrics.ChainForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHub(det, opts)
	t.Cleanup(h.Close)
	return h
}

var webPodA = metrics.Scope{Kind: metrics.KindPods, Namespace: "web", Name: "a"}

// pollCounter returns a callback that counts polls and a channel that gets each count. It never
// blocks the hub: a test that stops reading (a failure) must not hang the hub's Close.
func pollCounter() (func(), chan int) {
	ch := make(chan int, 64)
	n := 0
	return func() {
		n++
		select {
		case ch <- n:
		default:
		}
	}, ch
}

func waitPoll(t *testing.T, ch chan int, n int) {
	t.Helper()
	for {
		select {
		case got := <-ch:
			if got >= n {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("poll %d never came", n)
		}
	}
}

// podDataRel is podData with the points at the end of whatever range is asked for, as a real
// Prometheus answers: the hub keeps only what lies in the range it asked for.
func podDataRel(p *fakeProm) {
	p.onRel("container_cpu_usage_seconds_total", s(lc("app"), -0.3, 0.25, 0, 0.5), s(lc("sidecar"), -0.3, 0.01, 0, 0.02))
	p.onRel("container_memory_working_set_bytes", s(lc("app"), -0.3, 100*mib, 0, 110*mib), s(lc("sidecar"), -0.3, 10*mib, 0, 10*mib))
	p.onRel("container_network_receive_bytes_total", s(nil, -0.3, 1000, 0, 2000))
	p.onRel("container_network_transmit_bytes_total", s(nil, -0.3, 500, 0, 700))
	p.onRel("container_fs_usage_bytes", s(lc("app"), -0.3, 5000, 0, 6000))
}

// P13, Prometheus alone (no Metrics Server): a drawer costs one query set over its range when it
// opens, CPU and memory, then the newest points of the two at each poll (the live values), however
// many consumers share it; the network and the filesystem only while a chart shows them; nothing
// when nothing is open.
func TestPrometheusBudget(t *testing.T) {
	c := newPromCluster(t, false)
	podDataRel(c.prom)
	h := promHub(t, c, metrics.Options{ObjectInterval: 300 * time.Millisecond, MinStep: 5 * time.Millisecond, RangeWindow: 600 * time.Millisecond})

	idle := c.conn.Counter().Snapshot().Total
	time.Sleep(120 * time.Millisecond)
	if got := c.conn.Counter().Snapshot().Total; got != idle || c.proxyCount() != 0 {
		t.Fatalf("an idle hub sent %d requests (%d through the proxy)", got-idle, c.proxyCount())
	}

	onChange, polls := pollCounter()
	a := h.Subscribe(webPodA, onChange)
	b := h.Subscribe(webPodA, nil) // a second consumer of the same drawer
	waitPoll(t, polls, 3)

	// Right after poll 3 (the next one is 300 ms away): the detection probe, the history (cpu and
	// memory over the range), then the newest cpu and memory points at polls 2 and 3.
	if got := c.proxyCount(); got != 1+2+2*2 {
		t.Errorf("%d proxy requests after 3 polls, want 7 (1 probe + 2 + 2 x 2)", got)
	}
	for m, want := range map[string]int{"container_cpu_usage_seconds_total": 3, "container_memory_working_set_bytes": 3, "container_network_": 0, "container_fs_": 0} {
		if n := c.prom.queryCount(m); n != want {
			t.Errorf("%s asked for %d times in 3 polls, want %d", m, n, want)
		}
	}
	snap := c.conn.Counter().Snapshot()
	if snap.ByVerb["watch"] != 0 || snap.ByVerbResource["list services"] != 0 {
		t.Errorf("unexpected requests: %v", snap.ByVerbResource)
	}
	var spans []string
	for _, r := range c.prom.requests() {
		if strings.Contains(r.path, "query_range") && strings.Contains(r.query, "cpu") {
			if r.step != "0.005" || r.end == "" || r.start == "" {
				t.Errorf("range request %+v", r)
			}
			spans = append(spans, r.start+".."+r.end)
		}
	}
	// The history spans the range; the newest points start at the last point the chart has.
	if len(spans) == 3 {
		f0, t0, _ := strings.Cut(spans[0], "..")
		f1, _, _ := strings.Cut(spans[1], "..")
		if f0 >= f1 || f1 > t0 {
			t.Errorf("query spans %v: the second must start at the first's last point", spans)
		}
	}

	if st := h.State(webPodA); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server)" {
		t.Errorf("state = %+v", st)
	}
	hist := h.History(webPodA)
	if hist == nil || len(hist.Targets) != 3 || len(hist.Targets[""].Network) != 0 || hist.FilesystemSource != "" {
		t.Fatalf("history = %+v", hist)
	}
	for name, tg := range hist.Targets {
		for i := 1; i < len(tg.Usage); i++ {
			if !tg.Usage[i].At.After(tg.Usage[i-1].At) {
				t.Fatalf("target %q has points out of order or twice: %v", name, tg.Usage)
			}
		}
	}
	if p, ok := h.Pod("web", "a"); !ok || p.Total().CPUMilli != 520 || len(p.Containers) != 2 {
		t.Errorf("pod = %+v (%v): the current values come from the last point", p, ok)
	}

	// A chart shows the network and another the filesystem: each is asked for once over the range
	// (at once), then from its last point at each poll.
	b.SetWants(metrics.WantNetwork | metrics.WantFilesystem)
	waitPoll(t, polls, 5)
	if rx, fs := c.prom.queryCount("container_network_receive"), c.prom.queryCount("container_fs_usage"); rx != 2 || fs != 2 {
		t.Errorf("after two polls with the network and the filesystem shown: %d receive and %d filesystem queries, want 2 each", rx, fs)
	}
	if hist = h.History(webPodA); len(hist.Targets[""].Network) == 0 || hist.FilesystemSource != "usage" || len(hist.Targets["app"].Filesystem) == 0 {
		t.Errorf("history with the network and the filesystem = %+v", hist)
	}
	// No chart shows them any more: they are not asked for again, and their points are gone.
	b.SetWants(0)
	waitPoll(t, polls, 6)
	rx := c.prom.queryCount("container_network_receive")
	waitPoll(t, polls, 7)
	if n := c.prom.queryCount("container_network_receive"); n != rx {
		t.Errorf("the network was asked for %d more times while no chart showed it", n-rx)
	}
	if hist = h.History(webPodA); len(hist.Targets[""].Network) != 0 {
		t.Errorf("the network's points stayed: %d", len(hist.Targets[""].Network))
	}

	a.Release()
	b.Release()
	settled := c.conn.Counter().Snapshot().Total
	time.Sleep(500 * time.Millisecond)
	if got := c.conn.Counter().Snapshot().Total; got != settled {
		t.Errorf("%d requests after everything was released", got-settled)
	}
}

// P12 and P13 with both providers, the normal case (owner, 2026-10-12): a drawer that opens asks
// Prometheus once for its history (CPU and memory) and then nothing while only CPU or Memory is
// shown; the Metrics Server's get serves the live values at every poll. A Pods list costs the
// Metrics Server's list and nothing of Prometheus, which is not even looked for until a drawer
// opens.
func TestBothProvidersBudget(t *testing.T) {
	c := newPromCluster(t, true)
	c.srv.Set(podMetricsGVR, podObj("web", "a"))
	podDataRel(c.prom)
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, metrics.Options{ListInterval: 200 * time.Millisecond, ObjectInterval: 200 * time.Millisecond, MinStep: 5 * time.Millisecond, RangeWindow: 600 * time.Millisecond})
	t.Cleanup(h.Close)
	count := func(key string) int64 { return c.conn.Counter().Snapshot().ByVerbResource[key] }

	listChange, listPolls := pollCounter()
	list := h.Subscribe(allPods, listChange)
	waitPoll(t, listPolls, 2)
	if n := c.proxyCount(); n != 0 {
		t.Errorf("the Pods list sent %d requests to Prometheus, want none (the Metrics Server answers)", n)
	}
	if n := count("list pods.metrics.k8s.io"); n != 2 {
		t.Errorf("%d lists in 2 list polls", n)
	}
	if ps := h.Providers(); len(ps) != 2 || ps[0].Provider != "prometheus" || ps[0].Status != metrics.StatusUnknown || ps[1].Provider != "metrics-server" || ps[1].Status != metrics.StatusAvailable || !slices.Equal(ps[1].Serves, []string{metrics.ServesLive}) {
		t.Errorf("providers with the list open = %+v", ps)
	}
	list.Release()

	onChange, polls := pollCounter()
	drawer := h.Subscribe(webPodA, onChange)
	waitPoll(t, polls, 4)
	// One probe and one history (cpu, memory) at the drawer's first poll, then nothing of
	// Prometheus; one get of the Metrics Server per poll.
	if n := c.proxyCount(); n != 1+2 {
		t.Errorf("%d Prometheus requests after 4 drawer polls, want 3 (1 probe + cpu and memory once)", n)
	}
	if n := count("get pods.metrics.k8s.io"); n < 4 {
		t.Errorf("%d Metrics Server gets in 4 drawer polls", n)
	}
	ps := h.Providers()
	if ps[0].Status != metrics.StatusAvailable || ps[0].Service != "monitoring/prometheus-server:80" || !slices.Contains(ps[0].Serves, metrics.ServesHistory) || slices.Contains(ps[0].Serves, metrics.ServesLive) {
		t.Errorf("Prometheus's status = %+v", ps[0])
	}
	if !slices.Equal(ps[1].Serves, []string{metrics.ServesLive}) || ps[1].LastOK.IsZero() || ps[1].LastError != "" {
		t.Errorf("the Metrics Server's status = %+v", ps[1])
	}
	if st := h.State(webPodA); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server) and Kubernetes Metrics Server" {
		t.Errorf("state = %+v", st)
	}

	// The network shown: once over the range, then at the step (the polls are slower than it).
	drawer.SetWants(metrics.WantNetwork)
	waitPoll(t, polls, 6)
	if n := c.prom.queryCount("container_network_receive"); n != 2 {
		t.Errorf("%d receive queries in two polls with the network shown, want 2", n)
	}
	if n := c.prom.queryCount("container_cpu"); n != 1 {
		t.Errorf("cpu asked for %d times: the Metrics Server serves the live values", n)
	}
	drawer.Release()
	settled := c.conn.Counter().Snapshot().Total
	time.Sleep(300 * time.Millisecond)
	if got := c.conn.Counter().Snapshot().Total; got != settled {
		t.Errorf("%d requests after everything was released", got-settled)
	}
}

func TestPrometheusRangeFollowsTheChartsWindows(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	h := promHub(t, c, metrics.Options{}) // the defaults: one hour at 30 s
	onChange, polls := pollCounter()
	sub := h.Subscribe(webPodA, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)

	// A chart switched to five minutes, the others at the hour: one poll at once, at the finer step, still covering the hour.
	sub.SetWindows(5*time.Minute, time.Hour) // every chart's range, as the engine passes them
	waitPoll(t, polls, 2)
	var steps, spans []string
	for _, r := range c.prom.requests() {
		if strings.Contains(r.query, "container_cpu") {
			steps = append(steps, r.step)
			spans = append(spans, r.start+".."+r.end)
		}
	}
	if len(steps) != 2 || steps[0] != "30" || steps[1] != "15" {
		t.Errorf("steps = %v, want 30 then 15", steps)
	}
	for _, sp := range spans {
		from, to, _ := strings.Cut(sp, "..")
		if from == "" || to == "" || from >= to {
			t.Errorf("span %s", sp)
		}
	}
	// The same windows again change nothing.
	sub.SetWindows(5*time.Minute, time.Hour)
	time.Sleep(60 * time.Millisecond)
	if n := c.prom.queryCount("container_cpu"); n != 2 {
		t.Errorf("%d cpu queries; an unchanged window must not poll", n)
	}
}

func TestPrometheusForbiddenIsTheScopesState(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	c.prom.status = http.StatusForbidden
	h := promHub(t, c, metrics.Options{ObjectInterval: 300 * time.Millisecond})
	onChange, polls := pollCounter()
	sub := h.Subscribe(webPodA, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	if st := h.State(webPodA); st.Status != metrics.StatusForbidden || !strings.Contains(st.Message, "services/proxy") {
		t.Errorf("state = %+v", st)
	}
	if n := c.proxyCount(); n != 2 {
		t.Errorf("%d proxy requests: a forbidden proxy is probed once per spelling of its port (80, http), not queried", n)
	}
}

// A Prometheus that goes away while a drawer is open is a state; detection runs again.
func TestPrometheusGoneIsAStateAndDetectedAgain(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	h := promHub(t, c, metrics.Options{ObjectInterval: 40 * time.Millisecond, MinStep: 5 * time.Millisecond, RangeWindow: 100 * time.Millisecond, RetryDetectTTL: 10 * time.Millisecond})
	onChange, polls := pollCounter()
	sub := h.Subscribe(webPodA, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	c.prom.mu.Lock()
	c.prom.status = http.StatusNotFound
	c.prom.mu.Unlock()
	until := func(what string, ok func(metrics.State) bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !ok(h.State(webPodA)) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: state = %+v", what, h.State(webPodA))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	until("Prometheus answers 404", func(st metrics.State) bool { return st.Status == metrics.StatusNotServed })
	c.prom.mu.Lock()
	c.prom.status = 0
	c.prom.mu.Unlock()
	until("Prometheus came back", func(st metrics.State) bool { return st.Usable() })
}
