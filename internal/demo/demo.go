// Package demo is a synthetic cluster for `cogpit --demo`: 10,000 pods, a few hundred services
// with known cluster IPs, deployments, namespaces, nodes and events, the records of three Helm
// releases with their history (its only Secrets and ConfigMaps, helm.go), and a configurable
// churn of watch events (default 1,000 per second) that leaves the release records alone. It
// feeds the real cache through a cache.Source, so the demo exercises the same informers, engine
// and UI as a real cluster.
//
// The cluster is deterministic: the same Seed (and Now) gives the same objects, and the same
// sequence of changes, whatever the timing of the churn.
//
// Watch semantics follow the API server where informers depend on them:
//
//   - List returns a snapshot and its resourceVersion; every change bumps one global
//     resourceVersion.
//   - Watch from a resourceVersion replays what changed since, then streams live changes. The
//     replay is compacted: one event per object changed since that resourceVersion, carrying the
//     object's current state, in resourceVersion order. An informer ends in the same state as with
//     a full replay, and the history keeps no old object versions, so the demo's own memory stays
//     small and does not distort Cogpit's footprint in demo mode (budget P6).
//   - A resourceVersion older than the bounded history gets 410 Gone (an ERROR event, as from the
//     API server's watch cache), and the reflector relists.
//   - A watcher that falls more than a few seconds behind is closed, as the API server closes
//     watchers that do not drain: the generator never blocks on a consumer, and the reflector
//     resumes from its last resourceVersion (or relists after a 410).
//   - Watchers that ask for bookmarks get one a second while the churn runs, so a quiet
//     namespace-scoped watch stays resumable.
//   - Objects handed out are deep copies: informers transform them in place.
//
// Layer L2: it implements a cache.Source.
package demo

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// Defaults for zero Options fields.
const (
	DefaultPods            = 10_000
	DefaultServices        = 300
	DefaultNamespaces      = 20
	DefaultNodes           = 50
	DefaultDeployments     = 500
	DefaultEvents          = 2_000
	DefaultEventsPerSecond = 1_000
	DefaultHistory         = 50_000
)

// The well-known service of the MCP demo: an agent finds it with
// `resources_find {kind: services, field: clusterIP, value: 10.96.100.100}`. The kind seed
// (hack/kind/seed) has a Service with the same ClusterIP.
const (
	MCPNamespace = "demo-mcp"
	MCPService   = "web"
	MCPClusterIP = "10.96.100.100"
)

// Options size the synthetic cluster. Zero fields take the defaults above.
type Options struct {
	Pods            int     // default 10,000
	Services        int     // default 300
	Namespaces      int     // default 20; at least 3 (default, kube-system, demo-mcp)
	Nodes           int     // default 50
	EventsPerSecond float64 // watch events per second across all kinds; default 1,000; 0 with NoChurn
	NoChurn         bool    // a static cluster, for tests
	Seed            int64   // random seed; the same seed gives the same cluster

	// Deployments is the number of Deployments (default 500); they own most of the pods.
	Deployments int
	// Events is the number of core v1 Event objects the cluster keeps (default 2,000); the churn
	// adds new ones and removes the oldest.
	Events int
	// MetricsResolution is how often the demo's Metrics Server measures: the measurements'
	// timestamps are multiples of it (default 15 seconds, the real one's). Tests shorten it.
	MetricsResolution time.Duration
	// History is the number of changes a watch can resume across (default 50,000, about 50
	// seconds at the default rate); an older resourceVersion gets 410 Gone.
	History int
	// Now is the reference time: creation timestamps lie up to 30 days before it. Zero means the
	// time of New. Tests set it to make objects byte-for-byte reproducible.
	Now time.Time
}

// withDefaults fills zero fields.
func (o Options) withDefaults() Options {
	if o.Pods <= 0 {
		o.Pods = DefaultPods
	}
	if o.Services <= 0 {
		o.Services = DefaultServices
	}
	if o.Namespaces <= 0 {
		o.Namespaces = DefaultNamespaces
	}
	o.Namespaces = max(o.Namespaces, 3)
	if o.Nodes <= 0 {
		o.Nodes = DefaultNodes
	}
	if o.Deployments <= 0 {
		o.Deployments = DefaultDeployments
	}
	if o.Events <= 0 {
		o.Events = DefaultEvents
	}
	if o.History <= 0 {
		o.History = DefaultHistory
	}
	if o.EventsPerSecond <= 0 {
		o.EventsPerSecond = DefaultEventsPerSecond
	}
	if o.NoChurn {
		o.EventsPerSecond = 0
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.MetricsResolution <= 0 {
		o.MetricsResolution = defaultResolution
	}
	return o
}

// Stats describe the synthetic cluster for the status bar.
type Stats struct {
	Objects      int     `json:"objects"`
	EventsSent   int64   `json:"eventsSent"`
	EventsPerSec float64 `json:"eventsPerSec"`

	Lists   int64 `json:"lists"`   // List calls served
	Watches int64 `json:"watches"` // Watch calls served
	Expired int64 `json:"expired"` // watches answered with 410 Gone
	Dropped int64 `json:"dropped"` // watchers closed because they fell behind

	// MetricsRequests counts the list and get calls the demo's Metrics Server has served.
	MetricsRequests int64 `json:"metricsRequests"`
}

// Cluster is a running synthetic cluster. It is safe for concurrent use.
type Cluster struct {
	opts Options

	mu    sync.Mutex
	rng   *rand.Rand       // guarded by mu
	clock func() time.Time // the churn's clock; tests replace it before Start
	w     world            // the typed state objects are rendered from; guarded by mu

	rv        int64                                           // the global resourceVersion; guarded by mu
	compacted int64                                           // newest resourceVersion no longer in the history; guarded by mu
	store     [numKinds]map[string]*unstructured.Unstructured // current objects by key; published objects are immutable
	history   history                                         // guarded by mu
	watchers  [numKinds]map[*watcher]struct{}                 // guarded by mu
	meter     meter                                           // guarded by mu

	// queueLimit is how many events a watcher may have pending before it is closed.
	queueLimit int

	sent    atomic.Int64
	lists   atomic.Int64
	watches atomic.Int64
	expired atomic.Int64
	dropped atomic.Int64

	runMu sync.Mutex
	run   *runState // guarded by runMu

	metrics *Metrics // the synthetic Metrics Server (metrics.go)
}

// runState is one run of the churn.
type runState struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds the synthetic cluster's initial objects. Nothing changes until Start.
func New(opts Options) *Cluster {
	opts = opts.withDefaults()
	c := &Cluster{
		opts:       opts,
		rng:        rand.New(rand.NewPCG(uint64(opts.Seed), 0x636f67706974)), // "cogpit"
		clock:      time.Now,
		history:    newHistory(opts.History),
		queueLimit: defaultQueueLimit,
	}
	c.metrics = &Metrics{c: c, now: time.Now, resolution: opts.MetricsResolution}
	for k := range numKinds {
		c.store[k] = map[string]*unstructured.Unstructured{}
		c.watchers[k] = map[*watcher]struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.build()
	// The initial objects are not in the history: a watch from before them gets 410 Gone.
	c.compacted = c.rv
	return c
}

// Source returns the cache.Source the cache builds its informers from.
func (c *Cluster) Source() cache.Source { return source{c: c} }

// Resources returns the resources the synthetic cluster serves; its Secrets and ConfigMaps are
// Helm's release records. Other resources answer 404 Not Found, as a cluster without them would.
func Resources() []schema.GroupVersionResource {
	out := make([]schema.GroupVersionResource, 0, numKinds)
	for k := range numKinds {
		out = append(out, kindInfo[k].gvr)
	}
	return out
}

// Start starts the churn in its own goroutine and returns. The churn runs until ctx ends or Stop
// is called; Start while it runs does nothing, and Start after Stop resumes it. With NoChurn,
// Start does nothing.
func (c *Cluster) Start(ctx context.Context) {
	if c.opts.EventsPerSecond <= 0 {
		return
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.run != nil {
		select {
		case <-c.run.done: // ended with its context; start a new run
		default:
			return
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &runState{cancel: cancel, done: make(chan struct{})}
	c.run = r
	go c.generate(ctx, r.done)
}

// Stop ends the churn and waits for the generator to finish. It is idempotent. Open watches stay
// open; the cluster is static until the next Start.
func (c *Cluster) Stop() {
	c.runMu.Lock()
	r := c.run
	c.run = nil
	c.runMu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// Stats returns counts for the status bar.
func (c *Cluster) Stats() Stats {
	c.mu.Lock()
	objects := 0
	for k := range numKinds {
		objects += len(c.store[k])
	}
	rate := c.meter.rate(time.Now())
	c.mu.Unlock()
	return Stats{
		Objects:      objects,
		EventsSent:   c.sent.Load(),
		EventsPerSec: rate,
		Lists:        c.lists.Load(),
		Watches:      c.watches.Load(),
		Expired:      c.expired.Load(),
		Dropped:      c.dropped.Load(),

		MetricsRequests: c.metrics.Requests(),
	}
}

// Churn pacing.
const (
	tickInterval = 10 * time.Millisecond // 100 batches a second
	// maxBurst is how much stalled churn the generator catches up on: a busy machine does not
	// lower the rate, and a long pause (a laptop asleep) does not end in a flood.
	maxBurst         = time.Second
	bookmarkInterval = time.Second
)

// generate is the churn goroutine: a token bucket filled at EventsPerSecond, emptied every tick.
func (c *Cluster) generate(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("demo: churn stopped: internal error", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	rate := c.opts.EventsPerSecond
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	last := time.Now()
	lastBookmark := last
	budget := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			budget = min(budget+rate*now.Sub(last).Seconds(), rate*maxBurst.Seconds())
			last = now
			bookmarks := now.Sub(lastBookmark) >= bookmarkInterval
			if bookmarks {
				lastBookmark = now
			}
			budget -= float64(c.tick(now, int(budget), bookmarks))
		}
	}
}

// tick produces at least n watch events (an operation may produce two) and returns how many it
// produced. The deferred unlock keeps the cluster usable if an operation panics.
func (c *Cluster) tick(now time.Time, n int, bookmarks bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	produced := 0
	if n > 0 {
		produced = c.step(n)
		c.meter.add(now, produced)
	}
	if bookmarks {
		c.sendBookmarks()
	}
	return produced
}

// meter counts events per tick for the events-per-second figure.
type meter struct {
	buckets [128]meterBucket
	next    int
}

type meterBucket struct {
	t time.Time
	n int
}

func (m *meter) add(t time.Time, n int) {
	m.buckets[m.next] = meterBucket{t: t, n: n}
	m.next = (m.next + 1) % len(m.buckets)
}

// rate returns the events of the last second.
func (m *meter) rate(now time.Time) float64 {
	sum := 0
	for _, b := range m.buckets {
		if !b.t.IsZero() && now.Sub(b.t) < time.Second {
			sum += b.n
		}
	}
	return float64(sum)
}
