package metrics

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Kind is what a scope measures.
type Kind string

// Kinds.
const (
	KindPods  Kind = "pods"
	KindNodes Kind = "nodes"
)

// Scope names what a poll asks for. Everything that wants the same scope shares one poll.
//
//	{pods, "", ""}      every pod of every namespace (a list view)
//	{pods, "web", ""}   the pods of one namespace
//	{pods, "web", "a"}  one pod (a drawer)
//	{nodes, "", ""}     every node; {nodes, "", "n1"} one node
//	{cluster, "", "m1"} the cluster's totals, m1 its control-plane node (ClusterScope, hubcluster.go)
//	{nodeDisk, "", ""}  every node's root filesystem, from Prometheus (DiskScope, disk.go)
type Scope struct {
	Kind      Kind
	Namespace string
	Name      string
}

// IsList reports whether the scope is a list. The cluster's totals are one object, with a chart.
func (s Scope) IsList() bool { return s.Name == "" && s.Kind != KindCluster }

// Defaults of Options.
const (
	// DefaultListInterval is how often a visible list is polled: Lens's list refresh, 60 seconds.
	// A list can hold thousands of pods, so the response is large; the Metrics Server measures
	// every 15 seconds anyway, and a column of current values does not need every measurement.
	DefaultListInterval = 60 * time.Second
	// DefaultObjectInterval is how often an open drawer polls its object's live values: 15
	// seconds, the Metrics Server's own resolution (its --metric-resolution default), so every
	// measurement reaches the chart and none is asked for twice. The response is one small object.
	DefaultObjectInterval = 15 * time.Second
	// DefaultTTL is how long a result is served as current when nobody polls the scope: the
	// Metrics Server's resolution again. A subscription that starts within the TTL of the last
	// result does not poll at once; a one-off read (the agents' command) within it asks nothing.
	DefaultTTL = 15 * time.Second
	// DefaultSeriesPoints bounds one series of a drawer's chart: a day of the Metrics Server's
	// 15-second measurements. A series also drops what is older than its longest chart's range.
	DefaultSeriesPoints = 5760
	// DefaultSeriesAge is the least a series keeps, back from now: an hour, the default range.
	DefaultSeriesAge = time.Hour
	// DefaultDetectTTL is how long a found provider is trusted before discovery asks again.
	DefaultDetectTTL = 10 * time.Minute
	// DefaultRetryDetectTTL is how long a "not served", "forbidden" or "unavailable" is kept
	// before discovery asks again: a metrics-server installed meanwhile shows up within it.
	DefaultRetryDetectTTL = 2 * time.Minute
	// DefaultProviderRetry is how long a provider that failed a request while in use is left
	// alone while the other one serves its part; then it is asked again, and serves again when it
	// answers. A provider that nothing can stand in for is asked again at the next poll.
	// Cogpit's choice (G12): the list interval, so a list asks the failed one once per poll.
	DefaultProviderRetry = time.Minute
	// DefaultRequestTimeout bounds one poll.
	DefaultRequestTimeout = 20 * time.Second
	// DefaultRangeWindow is the time range of a chart of a provider with history: Lens's default,
	// one hour.
	DefaultRangeWindow = time.Hour
	// DefaultMinStep is the finest step of a range query: a Prometheus rarely scrapes faster.
	DefaultMinStep = 15 * time.Second
	// StepsPerWindow is how many points a chart gets across its time range (window / 120): a
	// chart a few hundred pixels wide gains nothing from more.
	StepsPerWindow = 120
	// DefaultMaxPoints bounds the points of one series however long the longest range is.
	DefaultMaxPoints = 1000
)

// Options tune a Hub; zero fields take the defaults above. Tests shrink the intervals.
type Options struct {
	ListInterval   time.Duration
	ObjectInterval time.Duration
	TTL            time.Duration
	SeriesPoints   int
	SeriesAge      time.Duration
	DetectTTL      time.Duration
	RetryDetectTTL time.Duration
	ProviderRetry  time.Duration
	RequestTimeout time.Duration
	// RangeWindow is the time range a drawer's charts show unless a window was set with
	// Subscription.SetWindows; MinStep the finest step of a range query; MaxRangePoints the most
	// points a range query returns per series.
	RangeWindow    time.Duration
	MinStep        time.Duration
	MaxRangePoints int
	// RetainObjects, RetainAge and RetainPoints bound the charts kept after their drawer closed
	// (retain.go); a negative RetainObjects keeps none.
	RetainObjects int
	RetainAge     time.Duration
	RetainPoints  int
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Logger receives poll failures; nil means slog.Default().
	Logger *slog.Logger
}

func (o Options) withDefaults() Options {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&o.ListInterval, DefaultListInterval)
	def(&o.ObjectInterval, DefaultObjectInterval)
	def(&o.TTL, DefaultTTL)
	def(&o.SeriesAge, DefaultSeriesAge)
	def(&o.DetectTTL, DefaultDetectTTL)
	def(&o.RetryDetectTTL, DefaultRetryDetectTTL)
	def(&o.ProviderRetry, DefaultProviderRetry)
	def(&o.RequestTimeout, DefaultRequestTimeout)
	def(&o.RangeWindow, DefaultRangeWindow)
	def(&o.MinStep, DefaultMinStep)
	if o.MaxRangePoints <= 0 {
		o.MaxRangePoints = DefaultMaxPoints
	}
	if o.SeriesPoints <= 0 {
		o.SeriesPoints = DefaultSeriesPoints
	}
	def(&o.RetainAge, DefaultRetainAge)
	if o.RetainObjects == 0 {
		o.RetainObjects = DefaultRetainObjects
	}
	if o.RetainPoints <= 0 {
		o.RetainPoints = DefaultRetainPoints
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Hub polls a cluster's providers for the scopes that are subscribed and keeps the latest results.
//
//   - Nothing is asked while nothing is subscribed: no poll, no detection; but for DetectUnknown,
//     which detects once what nothing looked for yet (the cluster settings' Integrations, D-76).
//   - A scope is polled by one goroutine however many consumers subscribed to it; its interval is
//     the list or the object interval.
//   - A result is cached for the TTL, longer while the scope is polled (twice its interval, so a
//     poll that is late does not blank a column). A subscription that starts on a fresh result
//     waits for it to go stale.
//   - A cluster has up to two providers (NewHubFor): the Metrics Server for the current values and
//     Prometheus for history. Both together is the normal case: a drawer's charts get their history
//     from Prometheus once and continue with the Metrics Server's measurements; the network and the
//     filesystem come from Prometheus while a chart shows them. One alone serves only while the
//     other fails or is not found, and the other takes its part back when it answers again.
//   - While an object scope is subscribed the hub keeps its chart (chart.go). The chart ends with
//     the last subscription.
//
// A Hub is safe for concurrent use.
type Hub struct {
	opts  Options
	slots []*slot // in the order the status line names them: the history provider first

	mu     sync.Mutex // guards the slots' fields below detMu and everything below
	scopes map[Scope]*scopeState
	closed bool
	// detectingUnknown is set while DetectUnknown's detection runs, triedUnknown is when it last
	// tried each provider (detectunknown.go).
	detectingUnknown bool
	triedUnknown     map[*slot]time.Time

	// The charts kept after their drawer closed (retain.go): by scope, the least recently closed
	// first in retainOrder, retainPoints their points in all.
	retained     map[Scope]*retainedChart
	retainOrder  []Scope
	retainPoints int

	// nodeAddrs are the nodes' addresses besides their names, by node (SetNodeAddresses): how a
	// Prometheus whose series name a node by its address finds it (A43).
	nodeAddrs map[string][]string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	gen atomic.Uint64
}

// slotRole is what a provider is there for.
type slotRole int

const (
	// roleAny: a hub over one detector (the demo cluster's, a test's, a chain): what it finds
	// decides what it serves.
	roleAny slotRole = iota
	// roleLive: the current values (the Metrics Server).
	roleLive
	// roleHistory: history and the variants only it has (Prometheus); the current values only
	// while no live provider answers.
	roleHistory
)

// slot is one of a cluster's providers: its detector, what detection found, and how its requests
// went.
type slot struct {
	role      slotRole
	det       Detector
	id, label string     // from the detector when it is Identified
	detMu     sync.Mutex // serialises the slot's detection

	// Guarded by Hub.mu.
	prov       Provider // found; nil when not found, forbidden or not asked yet
	state      State    // detection's answer, or the outcome of the last request
	asked      bool     // detection ran
	detectedAt time.Time
	retryAt    time.Time // a provider that failed is not asked again before (another serves)
	lastOK     time.Time
	lastErr    string
	lastErrAt  time.Time
	off        bool // turned off by the cluster's Metrics source setting (source.go): never detected nor asked
}

func newSlot(role slotRole, det Detector) *slot {
	sl := &slot{role: role, det: det}
	if id, ok := det.(Identified); ok {
		sl.id, sl.label = id.ID(), id.Label()
	}
	return sl
}

// Sources are a cluster's providers by role: Live answers the current values (the Metrics Server),
// History answers range queries (Prometheus). Either may be nil.
type Sources struct {
	Live    Detector
	History Detector
}

// NewHub returns a hub over one detector, which it asks for the provider the first time something
// subscribes or reads: the demo cluster's fixed provider, or a chain.
func NewHub(det Detector, opts Options) *Hub {
	return newHub(opts, newSlot(roleAny, det))
}

// NewHubFor returns a hub over a cluster's two providers: the Metrics Server for the current
// values, Prometheus for history. Prometheus is looked for only when a drawer opens (its history)
// or the Metrics Server fails or is missing, or once by DetectUnknown (the cluster's integrations
// shown, D-76).
func NewHubFor(src Sources, opts Options) *Hub {
	var slots []*slot
	if src.History != nil {
		slots = append(slots, newSlot(roleHistory, src.History))
	}
	if src.Live != nil {
		slots = append(slots, newSlot(roleLive, src.Live))
	}
	return newHub(opts, slots...)
}

func newHub(opts Options, slots ...*slot) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{opts: opts.withDefaults(), slots: slots, scopes: map[Scope]*scopeState{}, ctx: ctx, cancel: cancel}
}

// Generation increases whenever a result or a state changed; a table snapshot built at an older
// generation may be stale.
func (h *Hub) Generation() uint64 { return h.gen.Load() }

// Close stops every poll and waits for them. Subscriptions made afterwards do nothing.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.cancel()
	h.wg.Wait()
}

type scopeState struct {
	scope     Scope
	subs      map[*Subscription]struct{}
	cancel    context.CancelFunc // set while the poll goroutine runs
	data      *dataSet           // the current values
	state     State              // the outcome of the last attempt
	refused   bool               // the last attempt was the namespace's refusal (namespaceRefusal): data is empty, an answer
	attempted time.Time
	inflight  *flight
	chart     *chart        // an object scope's series while it is subscribed
	touched   time.Time     // the last use, for pruning
	kick      chan struct{} // wakes the poll goroutine early: a chart's range or variant changed
}

// dataSet is one result of current values: immutable once stored.
type dataSet struct {
	at    time.Time // when the hub got it
	pods  map[string]model.PodUsage
	nodes map[string]model.NodeUsage
	disks map[string]model.NodeDisk // the disk scope's, by the series' node (disk.go)
}

type flight struct {
	done      chan struct{}
	cancelled atomic.Bool // its loop was cancelled before it answered (fetch)
}

func podKey(namespace, name string) string { return namespace + "/" + name }

// Subscription is one consumer's interest in a scope.
type Subscription struct {
	h        *Hub
	scope    Scope
	onChange func()
	once     sync.Once
	windows  []time.Duration // the time ranges its charts show (guarded by Hub.mu); empty: the default
	wants    Want            // the series beyond CPU and memory its charts show (guarded by Hub.mu)
	noUsage  bool            // none of its charts shows CPU or memory (SetUsage; guarded by Hub.mu)
}

// Subscribe starts polling scope (if nobody polls it yet) until the returned subscription is
// released. onChange, if set, is called after every poll of the scope, on the poll goroutine: it
// must not block. The first poll happens at once unless a result younger than the TTL is held.
func (h *Hub) Subscribe(scope Scope, onChange func()) *Subscription {
	sub := &Subscription{h: h, scope: scope, onChange: onChange}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return sub
	}
	st := h.scopeLocked(scope)
	st.subs[sub] = struct{}{}
	if st.cancel == nil {
		if !scope.IsList() {
			st.chart = h.restoreLocked(scope) // the chart kept since its drawer closed, else a new one (retain.go)
			h.seedLocked(st)
		}
		ctx, cancel := context.WithCancel(h.ctx)
		st.cancel = cancel
		h.wg.Add(1)
		go h.loop(ctx, scope, st.kick, h.firstWaitLocked(st))
	}
	return sub
}

// Release ends the subscription (a nil subscription is a no-op); the last one of a scope stops its
// poll and drops its chart. The result stays cached for the TTL. Releasing twice is harmless.
func (s *Subscription) Release() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		h := s.h
		h.mu.Lock()
		defer h.mu.Unlock()
		st := h.scopes[s.scope]
		if st == nil {
			return
		}
		delete(st.subs, s)
		if len(st.subs) > 0 {
			return
		}
		if st.cancel != nil {
			st.cancel()
			st.cancel = nil
		}
		h.retainLocked(s.scope, st.chart) // kept for the next drawer of the object (retain.go)
		st.chart = nil
		st.touched = h.opts.Now()
		h.pruneLocked()
	})
}

func (h *Hub) intervalOf(s Scope) time.Duration {
	if s.IsList() {
		return h.opts.ListInterval
	}
	return h.opts.ObjectInterval
}

// intervalLocked is how often a scope is polled: the list or object interval, and for a drawer
// whose live values come from the provider with history (no Metrics Server answers) the range
// query's step (a new point exists only once per step).
func (h *Hub) intervalLocked(st *scopeState) time.Duration {
	iv := h.intervalOf(st.scope)
	if !st.scope.IsList() {
		if live, hist := h.rolesLocked(h.opts.Now()); live != nil && live == hist {
			iv = max(iv, h.bandsLocked(st)[0].step) // the finest query set's
		}
	}
	return iv
}

// hasHistory reports whether a provider answers range queries.
func hasHistory(p Provider) bool {
	_, ok := p.(RangeProvider)
	return ok && p.Capabilities().History
}

// readyLocked reports whether a slot's provider may be asked now: found and answering, or failed
// and due for another try.
func (h *Hub) readyLocked(sl *slot, now time.Time) bool {
	return sl.prov != nil && (sl.state.Usable() || !now.Before(sl.retryAt))
}

// rolesLocked picks the providers of a poll: hist is the first ready provider with history, live
// the first ready one without, else hist (the fallback). When nothing is ready, a found provider is
// asked again rather than nothing (there is no other to wait for).
func (h *Hub) rolesLocked(now time.Time) (live, hist *slot) {
	for _, sl := range h.slots {
		if !h.readyLocked(sl, now) {
			continue
		}
		if hasHistory(sl.prov) {
			if hist == nil {
				hist = sl
			}
		} else if live == nil {
			live = sl
		}
	}
	if live == nil {
		live = hist
	}
	if live == nil {
		for _, sl := range h.liveOrder() {
			if sl.prov != nil {
				live = sl
				if hasHistory(sl.prov) {
					hist = sl
				}
				break
			}
		}
	}
	return live, hist
}

// servingLocked is what each provider serves now, for the status line: the roles among the
// providers that answer, without retries.
func (h *Hub) servingLocked() (live, hist *slot) {
	for _, sl := range h.slots {
		if sl.prov == nil || !sl.state.Usable() {
			continue
		}
		if hasHistory(sl.prov) {
			if hist == nil {
				hist = sl
			}
		} else if live == nil {
			live = sl
		}
	}
	if live == nil {
		live = hist
	}
	return live, hist
}

// liveOrder is the slots in the order the current values prefer them: the live providers (and a
// single detector's), then the history providers.
func (h *Hub) liveOrder() []*slot {
	out := make([]*slot, 0, len(h.slots))
	for _, sl := range h.slots {
		if sl.role != roleHistory {
			out = append(out, sl)
		}
	}
	for _, sl := range h.slots {
		if sl.role == roleHistory {
			out = append(out, sl)
		}
	}
	return out
}

// band is one query set of an object scope's charts: the window it asks for, at its step.
type band struct{ window, step time.Duration }

// windowsLocked are the time ranges a scope's charts show, ascending and each once: every
// subscription's (SetWindows), the default range for one that set none.
func (h *Hub) windowsLocked(st *scopeState) []time.Duration {
	var ws []time.Duration
	for s := range st.subs {
		n := 0
		for _, w := range s.windows {
			if w > 0 {
				ws, n = append(ws, w), n+1
			}
		}
		if n == 0 {
			ws = append(ws, h.opts.RangeWindow)
		}
	}
	if len(ws) == 0 {
		ws = append(ws, h.opts.RangeWindow)
	}
	slices.Sort(ws)
	return slices.Compact(ws)
}

// stepOf is the step of a query set whose shortest chart shows shortest and whose longest shows
// longest: shortest / 120 (StepsPerWindow points), at least MinStep, and coarse enough that the
// longest stays within MaxRangePoints; whole seconds.
func (h *Hub) stepOf(shortest, longest time.Duration) time.Duration {
	step := max(h.opts.MinStep, shortest/StepsPerWindow, longest/time.Duration(h.opts.MaxRangePoints))
	if step >= time.Second {
		step = (step + time.Second - 1) / time.Second * time.Second
	}
	return step
}

// bandsLocked are the query sets of an object scope's charts, the finest first (A46, owner
// 2026-10-14 evening: "charts of one object whose ranges lie far apart get separate Prometheus
// queries, each at its own step"). The last, the coarsest, is the one set there was before: the
// longest range at the step of the shortest (at most the default hour's), within MaxRangePoints, so
// no chart gets less than it did. A range that would get fewer than half its StepsPerWindow points
// at that step (a container's hour beside the pod's week: 6 points at ten minutes) gets a finer set
// of its own, shared with the ranges near it: those that stay within MaxRangePoints at the step
// their shortest needs. A finer set exists only when its step is finer than the next coarser one's.
// The choices are the lead's for the owner to confirm (night run, D-43's neighbour in
// architecture §9).
func (h *Hub) bandsLocked(st *scopeState) []band {
	ws := h.windowsLocked(st)
	longest := ws[len(ws)-1]
	coarse := band{longest, h.stepOf(min(ws[0], h.opts.RangeWindow), longest)}
	var out []band
	for i := 0; i < len(ws); {
		s := ws[i]
		if s >= coarse.step*StepsPerWindow/2 { // enough points at the coarse step; so has every longer one
			break
		}
		j := i
		for j+1 < len(ws) && ws[j+1]/time.Duration(h.opts.MaxRangePoints) <= max(h.opts.MinStep, s/StepsPerWindow) {
			j++
		}
		b := band{ws[j], h.stepOf(s, ws[j])}
		switch {
		case b.step >= coarse.step: // the coarse set serves it as finely
		case len(out) > 0 && b.step <= out[len(out)-1].step:
			out[len(out)-1].window = b.window
		default:
			out = append(out, b)
		}
		i = j + 1
	}
	return append(out, coarse)
}

// wantsLocked is what the scope's charts show beyond CPU and memory.
func (h *Hub) wantsLocked(st *scopeState) Want {
	var w Want
	for s := range st.subs {
		w |= s.wants
	}
	return w &^ WantUsage
}

// SetWindows tells the hub which time ranges the subscriber's charts show, every chart's (one that
// shows the default range too); with a provider that has history (Prometheus) the next poll asks it
// for the longest at the step of the shortest, and for ranges far shorter than the longest a finer
// query set of their own (bandsLocked, A46). A subscription that never calls it, or calls it with
// none, shows the default range. A change makes the scope poll at once.
func (s *Subscription) SetWindows(windows ...time.Duration) {
	if s == nil {
		return
	}
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if slices.Equal(s.windows, windows) {
		return
	}
	s.windows = slices.Clone(windows)
	h.kickLocked(s.scope)
}

// SetWants tells the hub which series beyond CPU and memory the subscriber's charts show: the
// network and the filesystem, which only Prometheus has. They are asked for only while a chart
// shows them (G12: proposed by the lead, not decided by the owner): once over the chart's range
// when one starts showing them, then at the range's step. A change makes the scope poll at once.
func (s *Subscription) SetWants(w Want) {
	if s == nil {
		return
	}
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	w &^= WantUsage
	if s.wants == w {
		return
	}
	s.wants = w
	h.kickLocked(s.scope)
}

// SetUsage tells the hub whether the subscriber's charts show CPU or memory; they do until it is
// told otherwise. While no subscription of an object scope shows them (a drawer whose charts all
// show the network or the filesystem, or whose CPU and memory are hidden), its poll asks for
// neither: no live value of the Metrics Server, no CPU and memory history, no gap to fill
// (2026-10-16 night; before, every object poll asked for them). The cluster's scope always shows
// them (the Overview holds it only while its CPU or Memory chart shows). Shown again, the
// history is asked for over the range, and the live values go on. A change makes the scope poll
// at once.
func (s *Subscription) SetUsage(shown bool) {
	if s == nil {
		return
	}
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.noUsage == !shown {
		return
	}
	s.noUsage = !shown
	h.kickLocked(s.scope)
}

// usageLocked reports whether an object scope's charts show CPU or memory: the cluster's always,
// a drawer's when any subscription's does (and with none, as before). Charts that show the network
// or the filesystem while no provider has them (Prometheus went away) fall back to CPU, in the
// engine and the window: then CPU and memory are asked again.
func (h *Hub) usageLocked(st *scopeState) bool {
	if st.scope.Kind == KindCluster || len(st.subs) == 0 {
		return true
	}
	for s := range st.subs {
		if !s.noUsage {
			return true
		}
	}
	w, c := h.wantsLocked(st), h.capsLocked()
	return w == 0 || w.Has(WantNetwork) && !c.Network || w.Has(WantFilesystem) && !c.Filesystem
}

func (h *Hub) kickLocked(sc Scope) {
	if st := h.scopes[sc]; st != nil && st.cancel != nil && !sc.IsList() {
		select {
		case st.kick <- struct{}{}:
		default:
		}
	}
}

// Capabilities are the found providers': which chart variants they can draw. A provider that
// fails for a moment keeps its variants (the status line says why they do not move); one that is
// not found any more takes them away. Zero until a provider was found.
func (h *Hub) Capabilities() Capabilities {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.capsLocked()
}

// capsLocked is Capabilities with h.mu held.
func (h *Hub) capsLocked() Capabilities {
	var c Capabilities
	for _, sl := range h.slots {
		if sl.prov == nil {
			continue
		}
		pc := sl.prov.Capabilities()
		c.History = c.History || pc.History
		c.Network = c.Network || pc.Network
		c.Filesystem = c.Filesystem || pc.Filesystem
	}
	return c
}

// Providers is the status line's data: each of the cluster's providers, what Cogpit knows of it and
// what it serves now. It sends no request.
func (h *Hub) Providers() []ProviderStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	live, hist := h.servingLocked()
	out := make([]ProviderStatus, 0, len(h.slots))
	for _, sl := range h.slots {
		if sl.off {
			continue // the cluster's settings turn it off: not on the status line
		}
		ps := ProviderStatus{
			Provider: sl.id, Label: sl.label, Status: StatusUnknown,
			LastOK: sl.lastOK, LastError: sl.lastErr, LastErrorAt: sl.lastErrAt,
		}
		if sl.prov != nil {
			if ps.Provider == "" {
				ps.Provider = sl.prov.ID()
			}
			if ps.Label == "" {
				ps.Label = sl.prov.Label()
			}
			if l, ok := sl.prov.(Located); ok {
				ps.Service = l.Where()
			}
			if l, ok := sl.prov.(ServiceLocated); ok {
				ps.Namespace, ps.ServiceName = l.ServiceRef()
			}
			if n, ok := sl.prov.(Named); ok && n.Name() != "" {
				ps.Label = n.Name() // a Thanos or a VictoriaMetrics found by the Prometheus detector
			}
		}
		if sl.asked && sl.state.Status != "" {
			ps.Status = sl.state.Status
		}
		if sl == hist && sl.prov != nil {
			ps.Serves = append(ps.Serves, ServesHistory)
		}
		if sl == live && sl.prov != nil {
			ps.Serves = append(ps.Serves, ServesLive)
		}
		if sl == hist && sl.prov != nil {
			if c := sl.prov.Capabilities(); c.Network {
				ps.Serves = append(ps.Serves, ServesNetwork)
			}
			if c := sl.prov.Capabilities(); c.Filesystem {
				ps.Serves = append(ps.Serves, ServesFilesystem)
			}
		}
		out = append(out, ps)
	}
	return out
}

// DefaultWindow is the time range a chart shows unless told otherwise.
func (h *Hub) DefaultWindow() time.Duration { return h.opts.RangeWindow }

// SetNodeAddresses tells the hub the addresses a node has besides its name (its status.addresses:
// InternalIP, ExternalIP, Hostname). A node's range queries, and the cluster's for its control-plane
// nodes, match the node's series by them too: kube-prometheus-stack's node-exporter is scraped
// under its host-network address and carries no node label (A43; the disks are matched the same
// way, Hub.NodeDisk). The engine sets them from the cache before it subscribes a node's or the
// cluster's scope, and again whenever it reads the node; a change makes an open chart of the node
// poll at once and ask for its history with the new addresses (planLocked), since it may now be
// found. It sends nothing itself.
func (h *Hub) SetNodeAddresses(name string, addrs []string) {
	addrs = slices.Clone(addrs)
	slices.Sort(addrs)
	addrs = slices.DeleteFunc(slices.Compact(addrs), func(a string) bool { return a == "" || a == name })
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.nodeAddrs[name]; ok && slices.Equal(old, addrs) || !ok && len(addrs) == 0 {
		return
	}
	if h.nodeAddrs == nil {
		h.nodeAddrs = map[string][]string{}
	}
	h.nodeAddrs[name] = addrs
	for sc, st := range h.scopes {
		if st.chart != nil && (sc.Kind == KindNodes && sc.Name == name || sc.Kind == KindCluster && slices.Contains(ClusterMasters(sc.Name), name)) {
			h.kickLocked(sc)
		}
	}
}

// queryLocked is the range query of an object scope without its range: the object, and for a node
// (or the cluster's control plane) the addresses the engine gave (SetNodeAddresses).
func (h *Hub) queryLocked(sc Scope) RangeQuery {
	q := RangeQuery{Kind: sc.Kind, Namespace: sc.Namespace, Name: sc.Name}
	switch sc.Kind {
	case KindNodes:
		q.Addresses = h.nodeAddrs[sc.Name]
	case KindCluster:
		for _, m := range ClusterMasters(sc.Name) {
			q.Addresses = append(q.Addresses, h.nodeAddrs[m]...)
		}
	}
	return q
}

// Forget makes the next poll detect every provider again: the user changed the cluster's metrics
// settings (the Prometheus override), or knows a provider was installed.
func (h *Hub) Forget() {
	h.mu.Lock()
	for _, sl := range h.slots {
		sl.asked = false
		sl.retryAt = time.Time{}
	}
	for sc, st := range h.scopes {
		if st.chart != nil { // the history may be another Prometheus's: ask again
			st.chart.hist, st.chart.net, st.chart.fsys, st.chart.podsCov, st.chart.coarse = coverage{}, coverage{}, coverage{}, coverage{}, nil
			h.kickLocked(sc)
		}
	}
	h.mu.Unlock()
	h.gen.Add(1)
}

func (h *Hub) scopeLocked(s Scope) *scopeState {
	st := h.scopes[s]
	if st == nil {
		st = &scopeState{scope: s, subs: map[*Subscription]struct{}{}, kick: make(chan struct{}, 1)}
		h.scopes[s] = st
	}
	st.touched = h.opts.Now()
	return st
}

// pruneLocked forgets results nobody polls and nobody can still use.
func (h *Hub) pruneLocked() {
	now := h.opts.Now()
	for k, st := range h.scopes {
		if len(st.subs) == 0 && st.inflight == nil && now.Sub(st.touched) > 4*h.opts.TTL {
			delete(h.scopes, k)
		}
	}
}

// firstWaitLocked is how long a new poll goroutine waits before its first poll: until the held
// result goes stale, or not at all when there is none. A drawer polls at once when a provider with
// history is known: its charts need their history.
func (h *Hub) firstWaitLocked(st *scopeState) time.Duration {
	if st.data == nil {
		return 0
	}
	if !st.scope.IsList() {
		for _, sl := range h.slots {
			if sl.prov != nil && hasHistory(sl.prov) {
				return 0
			}
		}
	}
	return max(h.opts.TTL-h.opts.Now().Sub(st.data.at), 0)
}

// seedLocked starts an object's chart with the measurement a fresh list result holds for it.
func (h *Hub) seedLocked(st *scopeState) {
	sc := st.scope
	switch sc.Kind {
	case KindPods:
		for _, c := range []Scope{{KindPods, "", ""}, {KindPods, sc.Namespace, ""}} {
			if ls := h.scopes[c]; ls != nil && h.validLocked(ls) {
				if p, ok := ls.data.pods[podKey(sc.Namespace, sc.Name)]; ok {
					h.addPodLocked(st.chart, p, 0)
					return
				}
			}
		}
	case KindNodes:
		if ls := h.scopes[Scope{Kind: KindNodes}]; ls != nil && h.validLocked(ls) {
			if n, ok := ls.data.nodes[sc.Name]; ok {
				h.addNodeLocked(st.chart, n, 0)
			}
		}
	case KindCluster:
		h.seedClusterLocked(st)
	}
}

// validLocked reports whether a scope's result may still be served as current.
func (h *Hub) validLocked(st *scopeState) bool {
	if st.data == nil {
		return false
	}
	limit := h.opts.TTL
	if st.cancel != nil {
		limit = max(limit, 2*h.intervalLocked(st))
	}
	return h.opts.Now().Sub(st.data.at) <= limit
}

// loop is a scope's poll goroutine.
func (h *Hub) loop(ctx context.Context, sc Scope, kick <-chan struct{}, first time.Duration) {
	defer h.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			h.opts.Logger.Error("metrics: a poll panicked", "scope", fmt.Sprintf("%+v", sc), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		pctx, cancel := context.WithTimeout(ctx, h.opts.RequestTimeout)
		h.fetch(pctx, sc, !sc.IsList())
		cancel()
		if ctx.Err() != nil {
			return
		}
		h.notify(sc)
		timer.Reset(h.pollInterval(sc))
	}
}

// pollInterval is how long a scope waits after a poll.
func (h *Hub) pollInterval(sc Scope) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.scopes[sc]; st != nil {
		return h.intervalLocked(st)
	}
	return h.intervalOf(sc)
}

// notify calls the subscribers of a scope.
func (h *Hub) notify(sc Scope) {
	h.mu.Lock()
	var cbs []func()
	if st := h.scopes[sc]; st != nil {
		for s := range st.subs {
			if s.onChange != nil {
				cbs = append(cbs, s.onChange)
			}
		}
	}
	h.mu.Unlock()
	for _, cb := range cbs {
		func() {
			defer func() { _ = recover() }()
			cb()
		}()
	}
}

// detect asks a slot's detector when its answer is older than the detection TTL (shorter while
// there is no provider). Cancelled, it says nothing about the cluster.
func (h *Hub) detect(ctx context.Context, sl *slot) {
	sl.detMu.Lock()
	defer sl.detMu.Unlock()
	h.mu.Lock()
	ttl := h.opts.DetectTTL
	if sl.prov == nil {
		ttl = h.opts.RetryDetectTTL
	}
	if sl.off || sl.asked && h.opts.Now().Sub(sl.detectedAt) < ttl {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	var (
		p Provider
		s State
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				p, s = nil, State{Status: StatusUnavailable, Message: fmt.Sprint("detecting metrics failed: ", r), Reason: "detection failed"}
			}
		}()
		p, s = sl.det.Detect(ctx)
	}()
	if ctx.Err() != nil {
		return
	}
	if p != nil && !s.Usable() {
		p = nil
	}
	h.mu.Lock()
	if sl.off { // turned off while it was detected
		h.mu.Unlock()
		return
	}
	now := h.opts.Now()
	sl.prov, sl.state, sl.asked, sl.detectedAt = p, s, true, now
	if p != nil {
		sl.lastOK, sl.retryAt = now, time.Time{}
	} else if s.Reason != "" {
		sl.lastErr, sl.lastErrAt = s.Reason, now
	}
	h.mu.Unlock()
	h.gen.Add(1)
}

// detectAll runs the detection of every slot whose answer is due, at once.
func (h *Hub) detectAll(ctx context.Context) {
	fns := make([]func(), len(h.slots))
	for i, sl := range h.slots {
		fns[i] = func() { h.detect(ctx, sl) }
	}
	runParallel(fns...)
}

// detectLive runs what the current values need: the live providers' detection, and the history
// providers' only when no live provider was found (the fallback).
func (h *Hub) detectLive(ctx context.Context) {
	for _, sl := range h.slots {
		if sl.role != roleHistory {
			h.detect(ctx, sl)
		}
	}
	h.mu.Lock()
	found := false
	for _, sl := range h.slots {
		found = found || sl.role != roleHistory && h.readyLocked(sl, h.opts.Now())
	}
	h.mu.Unlock()
	if !found {
		h.detectHistory(ctx)
	}
}

func (h *Hub) detectHistory(ctx context.Context) {
	for _, sl := range h.slots {
		if sl.role == roleHistory {
			h.detect(ctx, sl)
		}
	}
}

// okLocked records that a slot's provider answered.
func (h *Hub) okLocked(sl *slot) {
	now := h.opts.Now()
	sl.lastOK, sl.retryAt = now, time.Time{}
	if !sl.state.Usable() && sl.prov != nil {
		sl.state = State{Status: StatusAvailable, Provider: sl.prov.ID(), Label: sl.prov.Label()}
	}
}

// failLocked turns a provider's error into the slot's state and its last error. An error that
// carries its state (StateError) is taken as it is; a not-served state makes detection run again.
// A cancelled request says nothing about the provider.
func (h *Hub) failLocked(ctx context.Context, sl *slot, sc Scope, err error) State {
	var s State
	var se *StateError
	if errors.As(err, &se) {
		s = se.State
	} else {
		s = StateFromError(err, "the cluster serves no metrics.k8s.io API (no metrics-server installed)")
	}
	if s.Reason == "" && s.Status != StatusNotServed {
		s.Reason = errorClass(err)
	}
	if ctx.Err() != nil {
		return State{Status: StatusUnavailable, Message: msgCancelled}
	}
	now := h.opts.Now()
	sl.state, sl.retryAt = s, now.Add(h.opts.ProviderRetry)
	sl.lastErr, sl.lastErrAt = cmp.Or(s.Reason, "not found"), now
	if s.Status == StatusNotServed {
		sl.prov, sl.asked = nil, false
	}
	id := sl.id
	if id == "" && sl.prov != nil {
		id = sl.prov.ID()
	}
	h.opts.Logger.Debug("metrics: a request failed", "provider", id, "kind", sc.Kind, "namespace", sc.Namespace, "name", sc.Name, "status", s.Status, "class", sl.lastErr)
	return s
}

// namespaceRefusal reports whether err is the Metrics API's refusal of one namespace's pods, and
// returns that scope's state (D-39, owner 2026-10-14). RBAC grants pods.metrics.k8s.io per
// namespace, apart from the pods themselves: a 403 for a namespace's pods says nothing about the
// provider's other scopes. So the scope's pods get no values and the state forbidden for that
// namespace, while the provider keeps its status, the other namespaces keep its values, and no
// other provider stands in for the refused one. A refusal of every pod (no namespace), of the
// nodes, or by a provider with history (a Prometheus behind a refused service proxy) is the
// provider's, as before (failLocked).
func namespaceRefusal(sc Scope, prov Provider, err error) (State, bool) {
	if sc.Kind != KindPods || sc.Namespace == "" || prov == nil || hasHistory(prov) || !apierrors.IsForbidden(err) {
		return State{}, false
	}
	return State{
		Status:  StatusForbidden,
		Message: "the current user may not read the pods' metrics in " + sc.Namespace + ": " + errorText(err),
		Reason:  errorClass(err),
	}, true
}

// worstLocked is the most telling state when no provider answered: forbidden before unavailable
// before not served, so a cluster with a Prometheus the user may not read says so instead of "no
// metrics-server"; on a tie the live provider's.
func (h *Hub) worstLocked() State {
	if st, ok := h.offStateLocked(); ok {
		return st
	}
	var best State
	for _, sl := range h.liveOrder() {
		if !sl.asked || sl.state.Status == "" {
			continue
		}
		if best.Status == "" || severity(sl.state.Status) > severity(best.Status) {
			best = sl.state
		}
	}
	if best.Status == "" {
		best = State{Status: StatusUnknown}
	}
	return best
}

// fetch polls a scope once and stores the outcome: the current values of a list or a one-off
// read, or the chart of a watched object. Concurrent fetches of one scope share one poll.
func (h *Hub) fetch(ctx context.Context, sc Scope, object bool) State {
	h.mu.Lock()
	st := h.scopeLocked(sc)
	for st.inflight != nil {
		f := st.inflight
		h.mu.Unlock()
		select {
		case <-f.done:
		case <-ctx.Done():
		}
		h.mu.Lock()
		// A fetch of a loop that was cancelled meanwhile (its last subscriber left and a new one
		// came at once: a view started again with its namespace plan) answered nothing: fetch
		// again rather than wait a whole interval with no values (review 2026-10-13).
		if ctx.Err() != nil || !f.cancelled.Load() {
			defer h.mu.Unlock()
			return st.state
		}
		if st.inflight == f {
			st.inflight = nil
		}
	}
	f := &flight{done: make(chan struct{})}
	st.inflight = f
	h.mu.Unlock()

	var state State
	if object {
		state = h.pollObject(ctx, sc)
	} else {
		var data *dataSet
		if sc.Kind == KindNodeDisk {
			data, state = h.pollDisks(ctx, sc) // Prometheus only (disk.go)
		} else {
			data, state = h.pollCurrent(ctx, sc)
		}
		h.mu.Lock()
		if ctx.Err() == nil || data != nil {
			st.attempted, st.state = h.opts.Now(), state
			st.refused = data != nil && state.Status == StatusForbidden
			if data != nil {
				st.data = data
			}
		}
		h.mu.Unlock()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	st.inflight = nil
	f.cancelled.Store(ctx.Err() != nil)
	close(f.done)
	h.gen.Add(1)
	h.pruneLocked()
	return state
}

// pollCurrent asks for a scope's current values: the live provider (the Metrics Server), and
// when it fails or is missing the provider with history (Prometheus's instant queries), in the
// same poll. A failure is a state and leaves the previous result alone (data nil).
func (h *Hub) pollCurrent(ctx context.Context, sc Scope) (*dataSet, State) {
	h.detectLive(ctx)
	tried := map[*slot]bool{}
	historyDetected := false
	for {
		h.mu.Lock()
		sl := h.nextLiveLocked(tried)
		var prov Provider
		if sl != nil {
			prov = sl.prov
		}
		h.mu.Unlock()
		if sl == nil {
			if !historyDetected && ctx.Err() == nil {
				historyDetected = true
				h.detectHistory(ctx) // the fallback: looked for only now
				continue
			}
			break
		}
		tried[sl] = true
		ds, err := h.instant(ctx, prov, sc)
		h.mu.Lock()
		if err == nil || errors.Is(err, ErrNoData) {
			h.okLocked(sl)
			h.mu.Unlock()
			return ds, State{Status: StatusAvailable, Provider: prov.ID(), Label: prov.Label()}
		}
		if s, refused := namespaceRefusal(sc, prov, err); refused {
			// The namespace's pods have no values; the provider and the other scopes are untouched.
			ds := &dataSet{at: h.opts.Now(), pods: map[string]model.PodUsage{}}
			h.mu.Unlock()
			return ds, s
		}
		s := h.failLocked(ctx, sl, sc, err)
		h.mu.Unlock()
		if ctx.Err() != nil {
			return nil, s
		}
	}
	if ctx.Err() != nil {
		return nil, State{Status: StatusUnavailable, Message: msgCancelled}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return nil, h.worstLocked()
}

// nextLiveLocked is the next provider to ask for current values: a ready live provider, then a
// ready one with history, then (nothing answered yet) any found one, each at most once per poll.
func (h *Hub) nextLiveLocked(tried map[*slot]bool) *slot {
	now := h.opts.Now()
	order := h.liveOrder()
	for _, pass := range []func(*slot) bool{
		func(sl *slot) bool { return h.readyLocked(sl, now) && !hasHistory(sl.prov) },
		func(sl *slot) bool { return h.readyLocked(sl, now) },
		func(sl *slot) bool { return sl.prov != nil },
	} {
		for _, sl := range order {
			if !tried[sl] && sl.prov != nil && pass(sl) {
				return sl
			}
		}
	}
	return nil
}

// instant makes the request of a scope's current values to one provider.
func (h *Hub) instant(ctx context.Context, prov Provider, sc Scope) (ds *dataSet, err error) {
	defer func() {
		if r := recover(); r != nil {
			ds, err = nil, fmt.Errorf("provider panicked: %v", r)
		}
	}()
	ds = &dataSet{at: h.opts.Now()}
	switch sc.Kind {
	case KindPods:
		ds.pods = map[string]model.PodUsage{}
		if sc.IsList() {
			var l []model.PodUsage
			if l, err = prov.ListPods(ctx, sc.Namespace); err == nil {
				for _, p := range l {
					ds.pods[podKey(p.Namespace, p.Name)] = p
				}
			}
		} else {
			var p model.PodUsage
			if p, err = prov.GetPod(ctx, sc.Namespace, sc.Name); err == nil {
				if p.Namespace == "" {
					p.Namespace = sc.Namespace
				}
				ds.pods[podKey(p.Namespace, p.Name)] = p
			}
		}
	case KindNodes:
		ds.nodes = map[string]model.NodeUsage{}
		if sc.IsList() {
			var l []model.NodeUsage
			if l, err = prov.ListNodes(ctx); err == nil {
				for _, n := range l {
					ds.nodes[n.Name] = n
				}
			}
		} else {
			var n model.NodeUsage
			if n, err = prov.GetNode(ctx, sc.Name); err == nil {
				ds.nodes[n.Name] = n
			}
		}
	case KindCluster:
		err = clusterInstant(ctx, prov, ds)
	default:
		err = fmt.Errorf("unknown scope kind %q", sc.Kind)
	}
	if errors.Is(err, ErrNoData) {
		return ds, err // an empty result: the object has no measurement yet
	}
	if err != nil {
		return nil, err
	}
	return ds, nil
}

// alignDown rounds t down to a multiple of step since the Unix epoch, so that the grid of
// successive range queries is the same and a point does not move between polls.
func alignDown(t time.Time, step time.Duration) time.Time {
	if step < time.Second {
		return t
	}
	s := int64(step / time.Second)
	return time.Unix(t.Unix()/s*s, 0)
}

// History returns the chart of an object scope that is polled, as of the last poll, or nil: lists,
// a scope nobody watches, a result that went stale. It holds what every provider gave: the history
// of a provider with history and the live values after it, the network and the filesystem while a
// chart shows them; with the Metrics Server alone the measurements since the drawer opened.
func (h *Hub) History(sc Scope) *ObjectSeries {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.scopes[sc]
	if st == nil || st.chart == nil || st.cancel == nil || st.chart.at.IsZero() {
		return nil
	}
	return st.chart.series()
}

// Fetch returns the current result of a scope without subscribing: from a fresh result (the scope's
// own, or a list that covers it), else after one request. It is for one-off reads (the agents'
// command): nothing keeps polling afterwards.
func (h *Hub) Fetch(ctx context.Context, sc Scope) Result {
	if r, ok := h.cached(sc); ok {
		return r
	}
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.opts.RequestTimeout)
		defer cancel()
	}
	state := h.fetch(ctx, sc, false)
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.scopes[sc]
	r := Result{State: state}
	if st != nil && st.data != nil {
		r.At, r.Pods, r.Nodes = st.data.at, sortedPods(st.data.pods, sc), sortedNodes(st.data.nodes)
	}
	return r
}

// Result is a scope's measurements and the state they came with.
type Result struct {
	State State
	// At is when the hub got the measurements; zero when there are none.
	At    time.Time
	Pods  []model.PodUsage
	Nodes []model.NodeUsage
}

// cached answers a scope from a fresh result: its own, or the all-namespaces list for a pod
// scope, or the node list for one node.
func (h *Hub) cached(sc Scope) (Result, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cands := []Scope{sc}
	switch sc.Kind {
	case KindPods:
		if sc.Namespace != "" {
			cands = append(cands, Scope{Kind: KindPods, Namespace: sc.Namespace}, Scope{Kind: KindPods})
		}
	case KindNodes:
		if sc.Name != "" {
			cands = append(cands, Scope{Kind: KindNodes})
		}
	}
	for _, c := range cands {
		st := h.scopes[c]
		// A namespace's refusal is an answer too: served for the TTL like values (D-39).
		if st == nil || !h.validLocked(st) || !st.state.Usable() && !st.refused {
			continue
		}
		r := Result{State: st.state, At: st.data.at}
		switch sc.Kind {
		case KindPods:
			r.Pods = sortedPods(st.data.pods, sc)
		case KindNodes:
			for _, n := range sortedNodes(st.data.nodes) {
				if sc.Name == "" || n.Name == sc.Name {
					r.Nodes = append(r.Nodes, n)
				}
			}
		}
		if c != sc && sc.Name != "" && len(r.Pods)+len(r.Nodes) == 0 {
			continue // the covering list does not hold the object: ask for it
		}
		return r, true
	}
	return Result{}, false
}

// sortedPods returns the pods a scope asks for (a namespace's, or one pod) sorted by namespace and
// name.
func sortedPods(m map[string]model.PodUsage, sc Scope) []model.PodUsage {
	out := make([]model.PodUsage, 0, len(m))
	for _, p := range m {
		if (sc.Namespace == "" || p.Namespace == sc.Namespace) && (sc.Name == "" || p.Name == sc.Name) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b model.PodUsage) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	return out
}

func sortedNodes(m map[string]model.NodeUsage) []model.NodeUsage {
	out := make([]model.NodeUsage, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b model.NodeUsage) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Pod returns the latest fresh measurement of a pod from any scope that holds it.
func (h *Hub) Pod(namespace, name string) (model.PodUsage, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best model.PodUsage
	found := false
	for _, c := range []Scope{{KindPods, "", ""}, {KindPods, namespace, ""}, {KindPods, namespace, name}} {
		st := h.scopes[c]
		if st == nil || !h.validLocked(st) {
			continue
		}
		if p, ok := st.data.pods[podKey(namespace, name)]; ok && (!found || p.Timestamp.After(best.Timestamp)) {
			best, found = p, true
		}
	}
	return best, found
}

// Node returns the latest fresh measurement of a node from any scope that holds it.
func (h *Hub) Node(name string) (model.NodeUsage, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best model.NodeUsage
	found := false
	for _, c := range []Scope{{Kind: KindNodes}, {Kind: KindNodes, Name: name}} {
		st := h.scopes[c]
		if st == nil || !h.validLocked(st) {
			continue
		}
		if n, ok := st.data.nodes[name]; ok && (!found || n.Timestamp.After(best.Timestamp)) {
			best, found = n, true
		}
	}
	return best, found
}

// State is what is known about the cluster's metrics for a scope: the outcome of its last poll,
// else what detection found, else unknown.
func (h *Hub) State(sc Scope) State {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.scopes[sc]; st != nil && st.state.Status != "" {
		return st.state
	}
	for _, sl := range h.liveOrder() {
		if sl.prov != nil && sl.state.Usable() {
			return sl.state
		}
	}
	return h.worstLocked()
}

// Series is one line of a chart: the whole object (Container ""), or one container of a pod.
type Series struct {
	Container string
	Samples   []model.UsageSample
}

// Series returns the CPU and memory series of an object scope that is subscribed, oldest sample
// first: the object's own first, then its containers by name. It is empty for lists and for a
// scope nobody watches. History has the whole chart.
func (h *Hub) Series(sc Scope) []Series {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.scopes[sc]
	if st == nil || st.chart == nil {
		return nil
	}
	var out []Series
	for _, n := range sortedKeys(st.chart.usage) { // "" sorts first
		if pts := st.chart.usage[n]; len(pts) > 0 {
			out = append(out, Series{Container: n, Samples: slices.Clone(pts)})
		}
	}
	return out
}

// Subscribed reports how many scopes are being polled: zero when nothing visible shows metrics.
func (h *Hub) Subscribed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, st := range h.scopes {
		if st.cancel != nil {
			n++
		}
	}
	return n
}

// addPodLocked adds a live measurement of a pod to a chart.
func (h *Hub) addPodLocked(c *chart, p model.PodUsage, gapAfter time.Duration) bool {
	at := p.Timestamp
	if at.IsZero() {
		at = h.opts.Now()
	}
	cont := make(map[string]model.UsageSample, len(p.Containers))
	for _, ct := range p.Containers {
		cont[ct.Name] = model.UsageSample{At: at, Usage: ct.Usage}
	}
	return c.addLive(model.UsageSample{At: at, Usage: p.Total()}, cont, gapAfter)
}

// addNodeLocked adds a live measurement of a node to a chart.
func (h *Hub) addNodeLocked(c *chart, n model.NodeUsage, gapAfter time.Duration) bool {
	at := n.Timestamp
	if at.IsZero() {
		at = h.opts.Now()
	}
	return c.addLive(model.UsageSample{At: at, Usage: n.Usage}, nil, gapAfter)
}
