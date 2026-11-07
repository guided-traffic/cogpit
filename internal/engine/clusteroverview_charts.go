package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's charts: CPU, Memory and Pods for the chosen node group, each with the
// lines Lens's documentation lists (CPU and Memory: usage, requests, limits, allocatable capacity,
// capacity; Pods: usage, allocatable capacity, capacity). The lines come from the cache. The usage
// of CPU and Memory comes from the metrics hub's cluster scope (metrics.ClusterScope), subscribed
// while the page is open: Prometheus's history once for the range, then the Metrics Server's node
// list summed, the same roles and fallback as a drawer's charts (architecture §9). The Pods' usage
// comes from the cache: a point per sample while the page is open, no history before it opened.

// The charts' metrics (OverviewChart.Metric).
const (
	OverviewCPU    = "cpu"
	OverviewMemory = "memory"
	OverviewPods   = "pods"
)

// OverviewChart is one chart of the Overview, in its own unit: cores, bytes or pods.
type OverviewChart struct {
	Metric string `json:"metric"` // "cpu", "memory", "pods"
	Title  string `json:"title"`  // "CPU", "Memory", "Pods"
	Unit   string `json:"unit"`   // "cores", "bytes", "pods"
	// Current is the newest usage and At its time; nil without one.
	Current *float64  `json:"current,omitempty"`
	At      time.Time `json:"at,omitzero"`
	// Requests and Limits are the sums over the pods that have not finished on the group's nodes
	// (a limit counts where a container sets one, as kubectl's "Allocated resources" does); the
	// Pods chart has neither. Allocatable and Capacity are the nodes' sums.
	Requests    *float64 `json:"requests,omitempty"`
	Limits      *float64 `json:"limits,omitempty"`
	Allocatable float64  `json:"allocatable"`
	Capacity    float64  `json:"capacity"`
	// Samples is the usage over the chart's range, oldest first; only with series.
	Samples []OverviewSample `json:"samples,omitempty"`
	// Hidden says the cluster's settings hide this chart (hidemetrics.go): it carries no values
	// and the window leaves it out.
	Hidden bool `json:"hidden,omitempty"`
}

// OverviewSample is one point of a chart.
type OverviewSample struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// The charts' units (OverviewChart.Unit).
const (
	unitCores = "cores"
	unitBytes = "bytes"
	unitPods  = OverviewPods
)

// msgNoMetrics is the state's message of a cluster without a metrics hub.
const msgNoMetrics = "this cluster has no metrics provider"

// overviewSampleEvery is how often the open page samples its pods count without a metrics poll
// (the Metrics Server's resolution, as the drawer's polls); a test shortens it.
var overviewSampleEvery = 15 * time.Second

// overviewMaxPoints bounds a pods series: a day of samples.
const overviewMaxPoints = 5760

// overviewCharts is the open page's chart data: its subscription to the cluster's metrics, and the
// pods series of each node group, sampled from the cache while the page is open.
type overviewCharts struct {
	s *Session
	b *Backend

	mu     sync.Mutex
	choice OverviewChoice
	last   *overviewBuild
	pods   map[string][]OverviewSample // by node group
	sub    *metrics.Subscription       // nil without a metrics hub, or while the CPU and Memory charts are hidden
	scope  metrics.Scope
	hidden MetricSet // the metrics the cluster's settings hide (hidemetrics.go)
	closed bool
	quit   chan struct{}
}

func newOverviewCharts(s *Session, b *Backend, choice OverviewChoice) *overviewCharts {
	c := &overviewCharts{s: s, b: b, choice: choice, pods: map[string][]OverviewSample{}, quit: make(chan struct{})}
	s.goSafe("cluster overview samples", c.sampleLoop)
	return c
}

// sampleLoop records the pods count every overviewSampleEvery; without a metrics poll to announce
// it, it tells the window itself.
func (c *overviewCharts) sampleLoop() {
	t := time.NewTicker(overviewSampleEvery)
	defer t.Stop()
	for {
		select {
		case <-c.quit:
			return
		case now := <-t.C:
			c.mu.Lock()
			if c.last != nil && c.last.synced {
				c.sampleLocked(c.last, now)
			}
			polled := c.sub != nil
			c.mu.Unlock()
			if !polled {
				c.s.emit(Event{Type: EventMetrics, View: ClusterOverview})
			}
		}
	}
}

// polled follows a poll of the cluster's metrics: a pods point, and the window redraws.
func (c *overviewCharts) polled() {
	c.mu.Lock()
	if c.last != nil && c.last.synced && !c.closed {
		c.sampleLocked(c.last, time.Now())
	}
	c.mu.Unlock()
	c.s.emit(Event{Type: EventMetrics, View: ClusterOverview})
}

// setChoice takes the user's range and node group: the history is asked for the new range.
func (c *overviewCharts) setChoice(choice OverviewChoice) {
	c.mu.Lock()
	c.choice = choice
	sub := c.sub
	c.mu.Unlock()
	sub.SetWindows(chartWindowOf(choice))
	c.s.emit(Event{Type: EventMetrics, View: ClusterOverview})
}

func (c *overviewCharts) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.quit)
	c.sub.Release()
	c.sub = nil
}

// follow takes a new build: the pods series gets a point when the count changed, and the metrics
// subscription follows the control plane (another set of masters is another scope).
func (c *overviewCharts) follow(ob *overviewBuild) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	changed := c.last == nil || !c.last.synced
	for _, g := range []string{NodesMaster, NodesWorker} {
		if c.last != nil && c.last.groups[g].pods != ob.groups[g].pods {
			changed = true
		}
	}
	c.last = ob
	if changed && ob.synced { // a count of a partial list is not the cluster's
		c.sampleLocked(ob, ob.at)
	}
	if c.b.Metrics == nil || !ob.nodesSynced {
		return // the nodes are still listing: subscribe once the control plane is known, not to a scope about to change
	}
	c.subscribeLocked(ob)
}

// subscribeLocked subscribes to the cluster scope of a build's control plane, unless the cluster's
// settings hide the CPU and the Memory chart: then nothing is polled for the page. c.mu is held.
func (c *overviewCharts) subscribeLocked(ob *overviewBuild) {
	if cpu, memory, _ := c.hidden.overviewShown(); !cpu && !memory {
		return
	}
	// The masters' addresses, which their history may be found by (A43): before the first poll,
	// and again with every build (unchanged ones cost nothing; a change asks for the history again).
	// Every node with its addresses: the history sums these nodes' series only (D-157).
	for _, m := range ob.masters {
		c.b.Metrics.SetNodeAddresses(m, ob.nodeAddrs[m])
	}
	c.b.Metrics.SetClusterNodes(ob.nodeAddrs)
	sc := metrics.ClusterScope(ob.masters)
	if c.sub != nil && c.scope == sc {
		return
	}
	old := c.sub
	c.scope = sc
	c.sub = c.b.Metrics.Subscribe(sc, c.polled)
	c.sub.SetWindows(chartWindowOf(c.choice))
	// The Pods chart's history from Prometheus (kube-state-metrics), once over the range; it shows
	// whenever the subscription does (the cluster's place hides all three charts at once).
	c.sub.SetWants(metrics.WantPods)
	old.Release()
}

// sampleLocked adds a pods point per group at, bounded to the longest range and a day of samples.
func (c *overviewCharts) sampleLocked(ob *overviewBuild, at time.Time) {
	keep := max(chartWindowOf(c.choice), DefaultOverviewWindow)
	for _, g := range []string{NodesMaster, NodesWorker} {
		pts := c.pods[g]
		if n := len(pts); n > 0 && !at.After(pts[n-1].At) {
			continue
		}
		pts = append(pts, OverviewSample{At: at, Value: float64(ob.groups[g].pods)})
		cut := 0
		for cut < len(pts) && pts[cut].At.Before(at.Add(-keep)) {
			cut++
		}
		if over := len(pts) - cut - overviewMaxPoints; over > 0 {
			cut += over
		}
		c.pods[g] = pts[cut:]
	}
}

// fill adds the charts of the open page to m: the lines, the metrics' state and providers, the
// current values, and with series the samples within the range.
func (c *overviewCharts) fill(m *ClusterOverviewModel, ob *overviewBuild, series bool) {
	m.Charts = chartLines(ob.groups[m.Nodes])
	c.mu.Lock()
	pods := c.pods[m.Nodes]
	sc, subscribed := c.scope, c.sub != nil
	hidden := c.hidden
	c.mu.Unlock()
	if markHiddenCharts(m, hidden) {
		return // nothing of the charts shows: no metrics are read for them
	}
	from := time.Now().Add(-m.Window)
	if n := len(pods); n > 0 {
		last := pods[n-1]
		v := last.Value
		m.Charts[2].Current, m.Charts[2].At = &v, last.At
		if series {
			m.Charts[2].Samples = samplesWithin(pods, from)
		}
	}
	hub := c.b.Metrics
	if hub == nil {
		m.Status, m.Message = metrics.StatusNotServed, msgNoMetrics
		return
	}
	m.Providers = hub.Providers()
	if cpu, memory, _ := hidden.overviewShown(); !cpu && !memory {
		m.Status, m.Message = StatusHidden, msgHidden // the Pods chart alone: from the cache
		return
	}
	if !subscribed {
		m.Status, m.Message = metrics.StatusUnknown, "the cluster's nodes are not listed yet"
		return
	}
	st := hub.State(sc)
	m.Status, m.Source, m.Message = st.Status, st.Label, st.Message
	if st.Usable() {
		m.Message = ""
		m.History = hub.Capabilities().History
	}
	hist := hub.History(sc)
	if hist == nil {
		return
	}
	if !hist.To.IsZero() {
		from = hist.To.Add(-m.Window)
	}
	target := metrics.TargetWorker
	if m.Nodes == NodesMaster {
		target = metrics.TargetMaster
	}
	ts := hist.Targets[target]
	m.PodsHistory = hist.PodsSource
	if m.History {
		m.UsageHistory, m.ForeignMachines = hist.UsageSource, hist.ForeignMachines
	}
	if ts == nil {
		return
	}
	if len(ts.Pods) > 0 && !m.Charts[2].Hidden {
		addPodsHistory(&m.Charts[2], ts.Pods, pods, from, series) // clusteroverview_pods.go
	}
	for i, metric := range []string{OverviewCPU, OverviewMemory} {
		ch := &m.Charts[i]
		if ch.Hidden {
			continue
		}
		for _, sm := range ts.Usage {
			v, ok := usageValue(metric, sm.Usage)
			if !ok {
				continue // a part the provider had no value for: left out, not 0
			}
			ch.Current, ch.At = &v, sm.At
			if series && !sm.At.Before(from) {
				ch.Samples = append(ch.Samples, OverviewSample{At: sm.At, Value: v})
			}
		}
	}
}

// samplesWithin returns the points at or after from; shared, not to be modified.
func samplesWithin(pts []OverviewSample, from time.Time) []OverviewSample {
	return tail(pts, from, func(p OverviewSample) time.Time { return p.At })
}

// currentCharts fills the charts of a model built without the page: the lines and the pods count
// from the cache, CPU and Memory from one read of the nodes' current values (a fresh one is served
// without a request; else one list of the Metrics Server, or Prometheus's instant queries while it
// fails). Nothing keeps polling.
func currentCharts(ctx context.Context, b *Backend, m *ClusterOverviewModel, ob *overviewBuild, hidden MetricSet) {
	m.Charts = chartLines(ob.groups[m.Nodes])
	if markHiddenCharts(m, hidden) {
		return
	}
	v := float64(ob.groups[m.Nodes].pods)
	m.Charts[2].Current, m.Charts[2].At = &v, ob.at
	if b.Metrics == nil {
		m.Status, m.Message = metrics.StatusNotServed, msgNoMetrics
		return
	}
	if cpu, memory, _ := hidden.overviewShown(); !cpu && !memory {
		m.Status, m.Message = StatusHidden, msgHidden // no read of the nodes' metrics for hidden charts
		return
	}
	r := b.Metrics.Fetch(ctx, metrics.Scope{Kind: metrics.KindNodes})
	m.Status, m.Source, m.Message, m.Providers = r.State.Status, r.State.Label, r.State.Message, b.Metrics.Providers()
	if !r.State.Usable() {
		return
	}
	m.Message = ""
	var sum model.Usage
	first := true
	for _, n := range r.Nodes {
		if slices.Contains(ob.masters, n.Name) != (m.Nodes == NodesMaster) {
			continue
		}
		if first {
			sum, first = n.Usage, false
		} else {
			sum = sum.Add(n.Usage)
		}
	}
	if first {
		return
	}
	for i, metric := range []string{OverviewCPU, OverviewMemory} {
		if v, ok := usageValue(metric, sum); ok && !m.Charts[i].Hidden {
			m.Charts[i].Current, m.Charts[i].At = &v, r.At
		}
	}
}

// chartLines are a node group's three charts with their lines from the cache.
func chartLines(t groupTotals) []OverviewChart {
	cores := func(milli int64) float64 { return float64(milli) / 1000 }
	f := func(v float64) *float64 { return &v }
	return []OverviewChart{
		{
			Metric: OverviewCPU, Title: "CPU", Unit: unitCores,
			Requests: f(cores(t.requests.CPUMilli)), Limits: f(cores(t.limits.CPUMilli)),
			Allocatable: cores(t.allocatable.CPUMilli), Capacity: cores(t.capacity.CPUMilli),
		},
		{
			Metric: OverviewMemory, Title: "Memory", Unit: unitBytes,
			Requests: f(float64(t.requests.MemoryBytes)), Limits: f(float64(t.limits.MemoryBytes)),
			Allocatable: float64(t.allocatable.MemoryBytes), Capacity: float64(t.capacity.MemoryBytes),
		},
		{Metric: OverviewPods, Title: "Pods", Unit: unitPods, Allocatable: float64(t.allocPods), Capacity: float64(t.capaPods)},
	}
}

// usageValue is a chart's value of a usage: cores or bytes, and whether the provider measured it.
func usageValue(metric string, u model.Usage) (float64, bool) {
	if metric == OverviewCPU {
		return float64(u.CPUMilli) / 1000, u.HasCPU()
	}
	return float64(u.MemoryBytes), u.HasMemory()
}
