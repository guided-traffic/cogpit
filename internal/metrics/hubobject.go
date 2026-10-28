package metrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The poll of an open drawer (G12, owner 2026-10-12, evening). Both providers together is the
// normal case:
//
//   - History from Prometheus: when the drawer opens, Prometheus is asked once for the charts'
//     range (CPU and memory: two range queries), and once more when a range changes.
//   - Then the live values from the Metrics Server: one get of the object per poll (15 s), shared
//     with everything else that watches it. Prometheus is not asked again while only CPU or Memory
//     is shown.
//   - Network and Filesystem exist only in Prometheus: asked for only while a chart shows them,
//     over the chart's range when one starts showing them, then from their last point at the
//     range's step.
//   - The seam: a live value not newer than the history's last point is left out (no point twice);
//     a live value that comes long after the last point leaves a gap that Prometheus fills once.
//
// Falling back (owner): a Metrics Server that fails hands the live values to Prometheus in the same
// poll (from the last point on, at the step), until it answers again (asked again after
// ProviderRetry); a Prometheus that fails or is gone leaves the Metrics Server's live values
// without new history, and when it answers again the history is asked for again where it is
// missing.

// objectPlan is what one poll of a drawer asks for.
type objectPlan struct {
	full, inc, gap *RangeQuery // the history provider's range queries; nil when not due
	gapSpan        [2]time.Time
	live           Provider // the live provider when it is not the history provider
	hist           RangeProvider
	liveSlot       *slot
	histSlot       *slot
	// window is the longest range (the coarsest query set's), what the series keep; fine and step
	// are the finest set's range and step, which the live values, the incremental and the gap's
	// queries and the fallback follow.
	window, fine, step time.Duration
	addrs              []string // the node's (the control plane's) addresses for the range queries
	// coarse are the coarser query sets due (A46), each answer put into the chart up to its until:
	// where the next finer set begins.
	coarse []coarseQuery
}

// coarseQuery is a coarser query set's range query.
type coarseQuery struct {
	q     RangeQuery
	band  band
	until time.Time
}

// objectResults are the answers of one poll.
type objectResults struct {
	full, inc, gap, fallback *ObjectSeries
	fullErr, incErr, gapErr  error
	fallbackErr              error
	fallbackQuery            *RangeQuery
	coarse                   []*ObjectSeries
	coarseErr                []error
	live                     *dataSet
	liveErr                  error
}

// pollObject polls a drawer's object once and stores the outcome in its chart and current values.
func (h *Hub) pollObject(ctx context.Context, sc Scope) State {
	if sc.Kind == KindClaims {
		h.detectNotLive(ctx) // a volume's usage is Prometheus's alone (volume.go)
	} else {
		h.detectAll(ctx)
	}
	if ctx.Err() != nil {
		return State{Status: StatusUnavailable, Message: msgCancelled}
	}
	h.mu.Lock()
	st := h.scopes[sc]
	if st == nil || st.chart == nil {
		h.mu.Unlock()
		return State{Status: StatusUnknown}
	}
	ch := st.chart
	p := h.planLocked(st, sc, h.opts.Now())
	h.mu.Unlock()

	var r objectResults
	var jobs []func()
	rangeCall := func(q *RangeQuery, out **ObjectSeries, errp *error) {
		if q != nil {
			jobs = append(jobs, func() { *out, *errp = callRange(ctx, p.hist, *q) })
		}
	}
	rangeCall(p.full, &r.full, &r.fullErr)
	rangeCall(p.inc, &r.inc, &r.incErr)
	rangeCall(p.gap, &r.gap, &r.gapErr)
	r.coarse, r.coarseErr = make([]*ObjectSeries, len(p.coarse)), make([]error, len(p.coarse))
	for i := range p.coarse {
		rangeCall(&p.coarse[i].q, &r.coarse[i], &r.coarseErr[i])
	}
	if p.live != nil {
		jobs = append(jobs, func() { r.live, r.liveErr = h.instant(ctx, p.live, sc) })
	}
	runParallel(jobs...)

	// The Metrics Server failed: Prometheus takes the live values over in this poll, from the
	// object's last point on, unless this poll already asked it for them. A refusal of the pod's
	// namespace is not a failure of the Metrics Server: nothing stands in (namespaceRefusal).
	_, refused := namespaceRefusal(sc, p.live, r.liveErr)
	if p.live != nil && r.liveErr != nil && !errors.Is(r.liveErr, ErrNoData) && !refused && ctx.Err() == nil && p.hist != nil &&
		(p.full == nil || !p.full.Want.Has(WantUsage)) && (p.inc == nil || !p.inc.Want.Has(WantUsage)) {
		h.mu.Lock()
		now := h.opts.Now()
		from := alignDown(maxTime(ch.lastUsage(), now.Add(-p.fine)), p.step)
		h.mu.Unlock()
		if !now.After(from) {
			from = alignDown(now.Add(-p.step), p.step)
		}
		q := RangeQuery{Kind: sc.Kind, Namespace: sc.Namespace, Name: sc.Name, Addresses: p.addrs, From: from, To: now, Step: p.step, Want: WantUsage}
		r.fallbackQuery = &q
		r.fallback, r.fallbackErr = callRange(ctx, p.hist, q)
	}
	if ctx.Err() != nil {
		return State{Status: StatusUnavailable, Message: msgCancelled}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if st.chart != ch { // released (and perhaps subscribed again) meanwhile
		return st.state
	}
	if len(jobs) == 0 && p.histSlot != nil && st.state.Status != "" {
		// Nothing was due from the provider there is (a variant changed to one already held, a
		// poll before the step): what the last poll found still holds. Without any provider the
		// state below says why.
		return st.state
	}
	return h.applyObjectLocked(ctx, st, sc, p, &r)
}

// planLocked decides what a poll of a drawer asks for.
func (h *Hub) planLocked(st *scopeState, sc Scope, now time.Time) objectPlan {
	ch := st.chart
	bands := h.bandsLocked(st)
	window, step := bands[0].window, bands[0].step // the finest set's: the rest of the plan is about it
	wants := h.wantsLocked(st)
	// A series no chart shows any more is dropped: shown again, it is asked for over the range.
	if !wants.Has(WantNetwork) && ch.net != (coverage{}) {
		ch.drop(WantNetwork)
	}
	if !wants.Has(WantFilesystem) && ch.fsys != (coverage{}) {
		ch.drop(WantFilesystem)
	}
	if !wants.Has(WantPods) && ch.podsCov != (coverage{}) {
		ch.drop(WantPods)
	}
	usage := h.usageLocked(st) // a chart shows CPU or memory: else neither is asked (SetUsage)
	live, hist := h.rolesLocked(now)
	p := objectPlan{window: bands[len(bands)-1].window, fine: window, step: step, liveSlot: live, histSlot: hist}
	if live != nil && live != hist && usage {
		p.live = live.prov
	}
	if hist == nil {
		return p
	}
	p.hist = hist.prov.(RangeProvider)
	base := h.queryLocked(sc)
	base.Step = step
	p.addrs = base.Addresses
	from := alignDown(now.Add(-window), step)
	addrsChanged := !slices.Equal(ch.histAddrs, base.Addresses)
	if addrsChanged {
		ch.coarse = nil
	} else {
		ch.carry(bands)
	}
	h.planCoarseLocked(ch, sc, &p, bands, wants, base, now, usage)
	var full, inc Want
	switch {
	case !usage:
	case !ch.hist.covers(window, step) || addrsChanged:
		// No history at this range yet, or it was asked with other addresses of the node (the
		// cache knew them only later: SetNodeAddresses), which may find what the last did not.
		full |= WantUsage
	case live == hist && now.Sub(ch.hist.asked) >= step:
		inc |= WantUsage // no Metrics Server answers: the live values are Prometheus's newest points
	}
	for _, x := range []struct {
		w Want
		c coverage
	}{{WantNetwork, ch.net}, {WantFilesystem, ch.fsys}} {
		switch {
		case !wants.Has(x.w):
		case !x.c.covers(window, step):
			full |= x.w
		case now.Sub(x.c.asked) >= step:
			inc |= x.w
		}
	}
	// The cluster's pod counts: over the range once (the history before the page opened), never
	// incrementally, since the cache counts the pods while the page is open (promcluster.go).
	if sc.Kind == KindCluster && wants.Has(WantPods) && !ch.podsCov.covers(window, step) {
		full |= WantPods
	}
	if full != 0 {
		p.full = &RangeQuery{Kind: base.Kind, Namespace: base.Namespace, Name: base.Name, Addresses: base.Addresses, From: from, To: now, Step: step, Want: full}
	}
	if inc != 0 {
		start := ch.lastOf(inc)
		if start.Before(from) {
			start = from
		}
		start = alignDown(start, step)
		if !now.After(start) {
			start = alignDown(now.Add(-step), step)
		}
		p.inc = &RangeQuery{Kind: base.Kind, Namespace: base.Namespace, Name: base.Name, Addresses: base.Addresses, From: start, To: now, Step: step, Want: inc}
	}
	if usage && ch.gap != nil && !full.Has(WantUsage) {
		q := RangeQuery{Kind: base.Kind, Namespace: base.Namespace, Name: base.Name, Addresses: base.Addresses, From: alignDown(ch.gap[0], step), To: ch.gap[1], Step: step, Want: WantUsage}
		if q.To.After(q.From) {
			p.gap, p.gapSpan = &q, *ch.gap
		} else {
			ch.gap = nil
		}
	}
	return p
}

// planCoarseLocked plans the coarser query sets (A46, bandsLocked): each asks once, over its
// range at its step, for the series the charts show that it was not asked for yet (CPU and memory
// always; the network, the filesystem and the cluster's pod counts while shown), never
// incrementally: the time after the next finer set's start is that set's.
func (h *Hub) planCoarseLocked(ch *chart, sc Scope, p *objectPlan, bands []band, wants Want, base RangeQuery, now time.Time, usage bool) {
	ch.keepCoarse(bands[1:])
	want := wants & (WantNetwork | WantFilesystem)
	if usage {
		want |= WantUsage
	}
	if sc.Kind == KindCluster {
		want |= wants & WantPods
	}
	for i := 1; i < len(bands); i++ {
		b, finer := bands[i], bands[i-1]
		missing := ch.coarseMissing(b, want)
		if missing == 0 {
			continue
		}
		until := alignDown(now.Add(-finer.window), finer.step)
		q := base
		q.From, q.To, q.Step, q.Want = alignDown(now.Add(-b.window), b.step), until, b.step, missing
		p.coarse = append(p.coarse, coarseQuery{q: q, band: b, until: until})
	}
}

// callRange asks a history provider, recovering a panic at the boundary.
func callRange(ctx context.Context, rp RangeProvider, q RangeQuery) (s *ObjectSeries, err error) {
	defer func() {
		if r := recover(); r != nil {
			s, err = nil, fmt.Errorf("provider panicked: %v", r)
		}
	}()
	return rp.ObjectRange(ctx, q)
}

// applyObjectLocked stores a drawer poll's answers: the history first, then the live values (which
// so continue it), then the bounds; and the scope's current values and state.
func (h *Hub) applyObjectLocked(ctx context.Context, st *scopeState, sc Scope, p objectPlan, r *objectResults) State {
	ch := st.chart
	now := h.opts.Now()
	served := false
	var histLabel, liveLabel string
	if p.histSlot != nil && p.histSlot.prov != nil {
		histLabel = p.histSlot.prov.Label()
	}
	if p.liveSlot != nil && p.liveSlot.prov != nil {
		liveLabel = p.liveSlot.prov.Label()
	}
	histOK := func(err error) bool {
		if err == nil {
			h.okLocked(p.histSlot)
			return true
		}
		h.failLocked(ctx, p.histSlot, sc, err)
		return false
	}
	// The coarser sets first, each up to where the next finer set begins (A46).
	for i, cq := range p.coarse {
		if histOK(r.coarseErr[i]) {
			ch.applyRange(r.coarse[i], cq.q.Want, cq.q.From, cq.until)
			ch.coverCoarse(cq.band, cq.q.Want)
			ch.foundUsage(r.coarse[i])
			served = true
		}
	}
	if p.full != nil && histOK(r.fullErr) {
		ch.applyRange(r.full, p.full.Want, p.full.From, farFuture)
		cov := coverage{window: p.fine, step: p.step, asked: now} // the finest set's
		if p.full.Want.Has(WantUsage) {
			ch.hist, ch.histAddrs = cov, p.full.Addresses
			ch.histLabel = histLabel
			if r.full != nil {
				ch.usageSource = r.full.UsageSource
			}
			if p.live == nil {
				ch.liveLabel = histLabel
			}
		}
		if p.full.Want.Has(WantNetwork) {
			ch.net = cov
		}
		if p.full.Want.Has(WantFilesystem) {
			ch.fsys = cov
		}
		if p.full.Want.Has(WantPods) {
			ch.podsCov = cov
		}
		served = true
	}
	if p.inc != nil && histOK(r.incErr) {
		ch.applyRange(r.inc, p.inc.Want, p.inc.From, farFuture)
		if p.inc.Want.Has(WantUsage) {
			ch.foundUsage(r.inc)
			ch.liveLabel, ch.hist.asked = histLabel, now
			if ch.histLabel == "" {
				ch.histLabel = histLabel
			}
		}
		if p.inc.Want.Has(WantNetwork) {
			ch.net.asked = now
		}
		if p.inc.Want.Has(WantFilesystem) {
			ch.fsys.asked = now
		}
		served = true
	}
	if served && ch.histLabel == "" {
		ch.histLabel = histLabel // a chart of the network or the filesystem alone: Prometheus's
	}
	if p.gap != nil && histOK(r.gapErr) {
		ch.applyRange(r.gap, WantUsage, p.gapSpan[0].Add(time.Nanosecond), p.gapSpan[1])
		ch.foundUsage(r.gap)
		if ch.gap != nil && *ch.gap == p.gapSpan {
			ch.gap = nil
		}
	}

	// A live value more than two of the Metrics Server's resolutions and a step after the last
	// point leaves a gap.
	gapAfter := 2*min(h.opts.ObjectInterval, DefaultObjectInterval) + p.step
	fromLive := false // the current values are the live provider's measurement
	refusal, refused := namespaceRefusal(sc, p.live, r.liveErr)
	if p.live != nil {
		switch {
		case refused:
			// The pod's namespace is refused (D-39): no live values, the provider untouched.
		case r.liveErr == nil || errors.Is(r.liveErr, ErrNoData):
			h.okLocked(p.liveSlot)
			served = true
			if r.live != nil && hasObject(r.live, sc) {
				st.data, fromLive = r.live, true
				h.addLiveLocked(ch, sc, r.live, gapAfter)
				ch.liveLabel = liveLabel
			}
		default:
			h.failLocked(ctx, p.liveSlot, sc, r.liveErr)
			if r.fallbackQuery != nil && histOK(r.fallbackErr) {
				ch.applyRange(r.fallback, WantUsage, r.fallbackQuery.From, farFuture)
				ch.foundUsage(r.fallback)
				ch.liveLabel, ch.hist.asked = histLabel, now
				served = true
			}
		}
	}

	ch.trim(max(p.window, h.opts.SeriesAge), h.opts.SeriesPoints)
	ch.at, ch.step = now, p.step

	// The current values: the live provider's measurement, else the chart's newest point.
	if !fromLive {
		if ds := h.chartCurrentLocked(ch, sc, now); ds != nil {
			st.data = ds
		}
	}

	var state State
	if served {
		id := ""
		switch {
		case p.live != nil && r.liveErr == nil:
			id = p.live.ID()
		case p.histSlot != nil && p.histSlot.prov != nil:
			id = p.histSlot.prov.ID()
		}
		state = State{Status: StatusAvailable, Provider: id, Label: ch.source()}
	} else if refused {
		state = refusal
	} else if sc.Kind == KindClaims {
		state = h.volumeMissingLocked() // Prometheus's state, never the Metrics Server's
	} else {
		state = h.worstLocked()
	}
	st.state, st.attempted = state, now
	return state
}

// addLiveLocked adds a live result of the scope's object to its chart; it reports whether the
// object's series got a new point.
func (h *Hub) addLiveLocked(ch *chart, sc Scope, ds *dataSet, gapAfter time.Duration) bool {
	switch sc.Kind {
	case KindPods:
		if pu, ok := ds.pods[podKey(sc.Namespace, sc.Name)]; ok {
			return h.addPodLocked(ch, pu, gapAfter)
		}
	case KindNodes:
		if nu, ok := ds.nodes[sc.Name]; ok {
			return h.addNodeLocked(ch, nu, gapAfter)
		}
	case KindCluster:
		return h.addClusterLocked(ch, sc, ds, gapAfter)
	}
	return false
}

// chartCurrentLocked is the chart's newest point as a result of current values; nil without one.
func (h *Hub) chartCurrentLocked(ch *chart, sc Scope, now time.Time) *dataSet {
	switch sc.Kind {
	case KindPods:
		if pu, ok := ch.latestPod(sc); ok {
			return &dataSet{at: now, pods: map[string]model.PodUsage{podKey(sc.Namespace, sc.Name): pu}}
		}
	case KindNodes:
		if nu, ok := ch.latestNode(sc.Name); ok {
			return &dataSet{at: now, nodes: map[string]model.NodeUsage{sc.Name: nu}}
		}
	}
	return nil
}

func hasObject(ds *dataSet, sc Scope) bool {
	if sc.Kind == KindCluster {
		return len(ds.nodes) > 0
	}
	if sc.Kind == KindNodes {
		_, ok := ds.nodes[sc.Name]
		return ok
	}
	_, ok := ds.pods[podKey(sc.Namespace, sc.Name)]
	return ok
}

// source is the providers the chart's numbers come from, as the source line names them: the
// history's, then the live values' when another provider gives them.
func (c *chart) source() string {
	switch {
	case c.histLabel == "":
		return c.liveLabel
	case c.liveLabel == "" || c.liveLabel == c.histLabel:
		return c.histLabel
	}
	return c.histLabel + " and " + c.liveLabel
}
