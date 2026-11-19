package demo

import (
	"context"
	"hash/fnv"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Metrics is the demo cluster's Metrics Server: a metrics.Provider that makes up the usage of
// every running pod and ready node, so that `cogpit --demo` shows CPU and Memory columns and live
// charts without a cluster. The numbers are deterministic for a pod and a moment of the demo's
// clock (Options.Now): each container has a steady level between 15 % and 85 % of its CPU and
// memory limit (else request, else a default), a slow wave of its own on top and a little noise per
// measurement. A measurement is taken every 15 seconds like the real Metrics Server does, so a
// chart gains a point per 15 s; its timestamp is the computer's time of that moment, as a real
// Metrics Server's. A pod that is not running has no measurement (metrics.ErrNoData), as in a real
// cluster.
type Metrics struct {
	c          *Cluster
	now        func() time.Time // the demo's clock
	offset     time.Duration    // the demo's clock ahead of the computer's (Cluster.offset)
	resolution time.Duration
	requests   atomic.Int64
}

// Requests counts the list and get calls the provider has served: what the API server would have
// seen from a real metrics-server.
func (m *Metrics) Requests() int64 { return m.requests.Load() }

// Metrics returns the demo cluster's metrics provider.
func (c *Cluster) Metrics() *Metrics { return c.metrics }

// defaultResolution is the demo's measurement interval, the real Metrics Server's default.
const defaultResolution = 15 * time.Second

// Defaults for a container that sets neither a limit nor a request.
const (
	defaultCPUMilli = 200
	defaultMemBytes = 256 << 20
)

// ID implements metrics.Provider.
func (*Metrics) ID() string { return metrics.ServerID }

// Label implements metrics.Provider.
func (*Metrics) Label() string { return metrics.ServerLabel }

// Capabilities implements metrics.Provider: current CPU and memory only, as the Metrics Server.
func (*Metrics) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }

// ListPods implements metrics.Provider.
func (m *Metrics) ListPods(_ context.Context, namespace string) ([]model.PodUsage, error) {
	m.requests.Add(1)
	m.c.mu.Lock()
	pods := make([]*unstructured.Unstructured, 0, len(m.c.store[kindPods]))
	for _, p := range m.c.store[kindPods] {
		if namespace == "" || p.GetNamespace() == namespace {
			pods = append(pods, p)
		}
	}
	m.c.mu.Unlock()
	// Published objects are immutable, so the measurements are made outside the lock.
	at, stamp := m.measuredAt()
	out := make([]model.PodUsage, 0, len(pods))
	for _, p := range pods {
		if u, ok := podUsageAt(p, at, stamp, m.resolution); ok {
			out = append(out, u)
		}
	}
	return out, nil
}

// GetPod implements metrics.Provider.
func (m *Metrics) GetPod(_ context.Context, namespace, name string) (model.PodUsage, error) {
	m.requests.Add(1)
	m.c.mu.Lock()
	p := m.c.store[kindPods][objKey(namespace, name)]
	m.c.mu.Unlock()
	if p != nil {
		at, stamp := m.measuredAt()
		if u, ok := podUsageAt(p, at, stamp, m.resolution); ok {
			return u, nil
		}
	}
	return model.PodUsage{}, metrics.ErrNoData
}

// ListNodes implements metrics.Provider.
func (m *Metrics) ListNodes(_ context.Context) ([]model.NodeUsage, error) {
	m.requests.Add(1)
	m.c.mu.Lock()
	nodes := make([]*unstructured.Unstructured, 0, len(m.c.store[kindNodes]))
	for _, n := range m.c.store[kindNodes] {
		nodes = append(nodes, n)
	}
	m.c.mu.Unlock()
	at, stamp := m.measuredAt()
	out := make([]model.NodeUsage, 0, len(nodes))
	for _, n := range nodes {
		if u, ok := nodeUsageAt(n, at, stamp); ok {
			out = append(out, u)
		}
	}
	return out, nil
}

// GetNode implements metrics.Provider.
func (m *Metrics) GetNode(_ context.Context, name string) (model.NodeUsage, error) {
	m.requests.Add(1)
	m.c.mu.Lock()
	n := m.c.store[kindNodes][objKey("", name)]
	m.c.mu.Unlock()
	if n != nil {
		at, stamp := m.measuredAt()
		if u, ok := nodeUsageAt(n, at, stamp); ok {
			return u, nil
		}
	}
	return model.NodeUsage{}, metrics.ErrNoData
}

// measuredAt is the newest measurement: at, the demo's clock rounded down to the resolution, which
// the numbers follow, and stamp, the computer's time of that moment, its timestamp.
func (m *Metrics) measuredAt() (at, stamp time.Time) {
	at = m.now().Truncate(m.resolution)
	return at, at.Add(-m.offset)
}

func podUsageAt(p *unstructured.Unstructured, at, stamp time.Time, res time.Duration) (model.PodUsage, bool) {
	if phase, _, _ := unstructured.NestedString(p.Object, "status", "phase"); phase != "Running" || p.GetDeletionTimestamp() != nil {
		return model.PodUsage{}, false
	}
	out := model.PodUsage{Namespace: p.GetNamespace(), Name: p.GetName(), Timestamp: stamp}
	for _, c := range model.NestedSlice(p.Object, "spec", "containers") {
		name := model.NestedString(c, "name")
		if name == "" {
			continue
		}
		cpuRef, memRef := usageReference(c)
		key := p.GetNamespace() + "/" + p.GetName() + "/" + name
		out.Containers = append(out.Containers, model.ContainerUsage{Name: name, Usage: model.Usage{
			CPUMilli:    max(int64(float64(cpuRef)*level(key+"/cpu", at, res)), 1),
			MemoryBytes: int64(float64(memRef) * level(key+"/mem", at, res) * memoryCurve(at, key)),
		}})
	}
	return out, len(out.Containers) > 0
}

func nodeUsageAt(n *unstructured.Unstructured, at, stamp time.Time) (model.NodeUsage, bool) {
	ready := false
	for _, c := range model.NestedSlice(n.Object, "status", "conditions") {
		if model.NestedString(c, "type") == "Ready" && model.NestedString(c, "status") == "True" {
			ready = true
		}
	}
	if !ready {
		return model.NodeUsage{}, false
	}
	alloc, _ := model.NodeCapacity(n)
	key := n.GetName()
	return model.NodeUsage{Name: n.GetName(), Timestamp: stamp, Usage: model.Usage{
		CPUMilli:    int64(float64(alloc.CPUMilli) * (0.1 + 0.55*wave(key+"/cpu", at, 40*time.Minute))),
		MemoryBytes: int64(float64(alloc.MemoryBytes) * (0.25 + 0.5*wave(key+"/mem", at, 3*time.Hour))),
	}}, true
}

// usageReference is what a container's usage is a share of: its limit, else its request, else a
// default; CPU in millicores, memory in bytes.
func usageReference(c any) (cpuMilli, memBytes int64) {
	cpuMilli, memBytes = defaultCPUMilli, defaultMemBytes
	req := model.UsageOf(c, "resources", "requests")
	lim := model.UsageOf(c, "resources", "limits")
	switch {
	case lim.CPUMilli > 0:
		cpuMilli = lim.CPUMilli
	case req.CPUMilli > 0:
		cpuMilli = req.CPUMilli
	}
	switch {
	case lim.MemoryBytes > 0:
		memBytes = lim.MemoryBytes
	case req.MemoryBytes > 0:
		memBytes = req.MemoryBytes
	}
	return cpuMilli, memBytes
}

// level is a container's share of its reference at a moment: a steady level of its own between 15 %
// and 85 %, a slow wave on a period of 3 to 12 minutes, a faster one of about a minute and a half
// and noise of ±3 % that changes with every measurement.
func level(key string, at time.Time, res time.Duration) float64 {
	steady := 0.15 + 0.7*unit(key, 0)
	period := time.Duration(3+int(9*unit(key, 1))) * time.Minute
	noise := (unit(key, at.UnixNano()/int64(res)+2) - 0.5) * 0.06
	fast := 0.12 * steady * math.Sin(phase(key+"/fast", at, 80*time.Second+time.Duration(unit(key, 3)*float64(30*time.Second))))
	return min(max(steady*0.7+0.3*steady*(1+math.Sin(phase(key, at, period)))+fast+noise, 0.02), 1.15)
}

// memoryCurve makes memory move slower than CPU and only climb within a period (a leak that a
// restart resets), so the memory chart does not just mirror the CPU one.
func memoryCurve(at time.Time, key string) float64 {
	const period = 20 * time.Minute
	offset := time.Duration(unit(key, 7) * float64(period))
	frac := float64((at.Sub(time.Unix(0, 0))+offset)%period) / float64(period)
	return 0.85 + 0.15*frac
}

// wave is a smooth value in [0, 1] for key on a period.
func wave(key string, at time.Time, period time.Duration) float64 {
	return 0.5 + 0.5*math.Sin(phase(key, at, period))
}

func phase(key string, at time.Time, period time.Duration) float64 {
	return 2*math.Pi*float64(at.UnixNano()%int64(period))/float64(period) + 2*math.Pi*unit(key, 99)
}

// unit hashes a key and a salt to [0, 1).
func unit(key string, salt int64) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key + "#" + strconv.FormatInt(salt, 10)))
	return float64(h.Sum64()>>11) / float64(1<<53)
}
