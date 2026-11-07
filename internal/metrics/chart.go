package metrics

import (
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// chart is an object scope's series while a drawer shows the object (G12, owner 2026-10-12,
// evening): the history a provider with history (Prometheus) gave when the drawer opened or a
// range changed, continued by the live values (the Metrics Server's measurements, or Prometheus's
// newest points when the Metrics Server does not answer). Per target: "" is the pod or node, then
// a pod's containers. The network is the object's only. Points are in time order, at most one per
// time; a slice handed out is never written again (new points go to new or appended slices).
type chart struct {
	usage    map[string][]model.UsageSample
	network  []model.NetworkSample
	fs       map[string][]model.FilesystemSample
	fsSource string
	// pods are the cluster scope's pod counts by target (WantPods), podsSource where they come
	// from once asked ("kube-state-metrics" or "none").
	pods       map[string][]model.CountSample
	podsSource string
	// usageSource is where the history of the CPU and memory came from at its last full query
	// (ObjectSeries.UsageSource): node-exporter, cAdvisor, or none; "" before one answered.
	// foreign is the cluster's machines beyond its nodes that history counts
	// (ObjectSeries.ForeignMachines).
	usageSource string
	foreign     int

	// The history each kind of series holds: the window and step it was asked for (zero: none,
	// asked for at the next poll a history provider answers), and when it was last asked for.
	hist, net, fsys, podsCov coverage
	// histAddrs are the node's addresses the CPU and memory history was asked with
	// (RangeQuery.Addresses): other addresses ask for it again.
	histAddrs []string
	// coarse is what each coarser query set (A46, bandsLocked) was asked for: hist, net, fsys and
	// podsCov are the finest set's.
	coarse []bandCoverage

	// gap is a hole in the usage between two points (both live values came, the history provider
	// was away between them) that the history provider fills once when it answers; nil without.
	gap *[2]time.Time

	// histLabel and liveLabel name the providers whose numbers the chart holds: the source line.
	histLabel, liveLabel string

	at   time.Time     // the last poll's time: the series end here
	step time.Duration // the history's step
}

// bandCoverage is the series a coarser query set was asked for.
type bandCoverage struct {
	band
	want Want
}

// coarseMissing is what of want the coarser set b was not asked for yet.
func (c *chart) coarseMissing(b band, want Want) Want {
	for _, bc := range c.coarse {
		if bc.band == b {
			return want &^ bc.want
		}
	}
	return want
}

// coverCoarse records that the coarser set b was asked for want.
func (c *chart) coverCoarse(b band, want Want) {
	for i := range c.coarse {
		if c.coarse[i].band == b {
			c.coarse[i].want |= want
			return
		}
	}
	c.coarse = append(c.coarse, bandCoverage{b, want})
}

// carry keeps what a query set was asked for when a range change moves it between the finest and
// the coarser ones (A46): the week that becomes the only range, or the only range that gets a
// shorter one beside it, is not asked for again. A set that becomes the finest is followed
// incrementally from its last points at once (asked zero).
func (c *chart) carry(bands []band) {
	covs := []struct {
		w Want
		c *coverage
	}{{WantUsage, &c.hist}, {WantNetwork, &c.net}, {WantFilesystem, &c.fsys}, {WantPods, &c.podsCov}}
	for _, b := range bands[1:] {
		for _, x := range covs {
			if x.c.covers(b.window, b.step) {
				c.coverCoarse(b, x.w)
			}
		}
	}
	fine := bands[0]
	for _, bc := range c.coarse {
		if bc.band != fine {
			continue
		}
		for _, x := range covs {
			if bc.want.Has(x.w) && !x.c.covers(fine.window, fine.step) {
				*x.c = coverage{window: fine.window, step: fine.step}
			}
		}
	}
}

// keepCoarse forgets the coarser sets' coverage that is not among bands any more.
func (c *chart) keepCoarse(bands []band) {
	c.coarse = slices.DeleteFunc(c.coarse, func(bc bandCoverage) bool { return !slices.Contains(bands, bc.band) })
}

// coverage is what one kind of series holds of a provider with history.
type coverage struct {
	window, step time.Duration
	asked        time.Time
}

// covers reports whether the history was asked for at window and step.
func (c coverage) covers(window, step time.Duration) bool {
	return c.window == window && c.step == step
}

func newChart() *chart {
	return &chart{usage: map[string][]model.UsageSample{}, fs: map[string][]model.FilesystemSample{}, pods: map[string][]model.CountSample{}}
}

func usageAt(s model.UsageSample) time.Time           { return s.At }
func networkAt(s model.NetworkSample) time.Time       { return s.At }
func filesystemAt(s model.FilesystemSample) time.Time { return s.At }
func countAt(s model.CountSample) time.Time           { return s.At }

// splice returns old with the points at or after from and before to replaced by add's points in
// that interval: the answer of a range query over [from, to) takes the place of what the chart
// held there. The result is a new slice; old is not modified.
func splice[T any](old, add []T, from, to time.Time, at func(T) time.Time) []T {
	out := make([]T, 0, len(old)+len(add))
	i := 0
	for ; i < len(old) && at(old[i]).Before(from); i++ {
		out = append(out, old[i])
	}
	for _, p := range add {
		if t := at(p); !t.Before(from) && t.Before(to) && (len(out) == 0 || t.After(at(out[len(out)-1]))) {
			out = append(out, p)
		}
	}
	for ; i < len(old); i++ {
		if t := at(old[i]); !t.Before(to) && (len(out) == 0 || t.After(at(out[len(out)-1]))) {
			out = append(out, old[i])
		}
	}
	return out
}

// farFuture is the open end of a splice that replaces a series' tail.
var farFuture = time.Unix(1<<62, 0)

// namesOf is the union of a chart's and an answer's target names.
func namesOf[V any](have map[string]V, res *ObjectSeries) []string {
	m := make(map[string]struct{}, len(have)+len(res.Targets))
	for k := range have {
		m[k] = struct{}{}
	}
	for k := range res.Targets {
		m[k] = struct{}{}
	}
	return sortedKeys(m)
}

// applyRange puts a range query's answer into the chart: for each series it asked for (want), the
// points from from on are the answer's. full marks the history as covering window at step.
func (c *chart) applyRange(res *ObjectSeries, want Want, from, to time.Time) {
	if res == nil {
		return
	}
	if want.Has(WantUsage) {
		for _, name := range namesOf(c.usage, res) {
			var add []model.UsageSample
			if t := res.Targets[name]; t != nil {
				add = t.Usage
			}
			c.usage[name] = splice(c.usage[name], add, from, to, usageAt)
		}
	}
	if want.Has(WantNetwork) {
		var add []model.NetworkSample
		if t := res.Targets[""]; t != nil {
			add = t.Network
		}
		c.network = splice(c.network, add, from, to, networkAt)
	}
	if want.Has(WantFilesystem) {
		if res.FilesystemSource != "" && res.FilesystemSource != c.fsSource {
			c.fs = map[string][]model.FilesystemSample{} // another metric: the old points are another unit
			c.fsSource = res.FilesystemSource
		}
		for _, name := range namesOf(c.fs, res) {
			var add []model.FilesystemSample
			if t := res.Targets[name]; t != nil {
				add = t.Filesystem
			}
			c.fs[name] = splice(c.fs[name], add, from, to, filesystemAt)
		}
	}
	if want.Has(WantPods) {
		if res.PodsSource != "" {
			c.podsSource = res.PodsSource
		}
		for _, name := range namesOf(c.pods, res) {
			var add []model.CountSample
			if t := res.Targets[name]; t != nil {
				add = t.Pods
			}
			c.pods[name] = splice(c.pods[name], add, from, to, countAt)
		}
	}
}

// foundUsage takes the source of a later range answer's CPU and memory (an incremental query, a
// gap, the fallback) when it found them: a chart whose history had none may find them later.
func (c *chart) foundUsage(res *ObjectSeries) {
	if res != nil && res.UsageSource != "" && res.UsageSource != UsageNone {
		c.usageSource, c.foreign = res.UsageSource, res.ForeignMachines
	}
}

// addLive appends a live measurement of a pod (its total and each container) or a node to the
// usage. A point that is not newer than the target's last is left out: the history holds that
// time already. A point that comes long after the last one leaves a gap for the history provider
// to fill. It reports whether the object's own series got the point.
func (c *chart) addLive(whole model.UsageSample, containers map[string]model.UsageSample, gapAfter time.Duration) bool {
	added := c.addUsage("", whole, gapAfter)
	for name, s := range containers {
		c.addUsage(name, s, 0)
	}
	return added
}

func (c *chart) addUsage(target string, s model.UsageSample, gapAfter time.Duration) bool {
	pts := c.usage[target]
	if n := len(pts); n > 0 {
		last := pts[n-1].At
		if !s.At.After(last) {
			return false
		}
		if gapAfter > 0 && s.At.Sub(last) > gapAfter {
			if c.gap == nil {
				c.gap = &[2]time.Time{last, s.At}
			} else {
				c.gap = &[2]time.Time{minTime(c.gap[0], last), maxTime(c.gap[1], s.At)}
			}
		}
	}
	c.usage[target] = append(pts, s)
	return true
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// lastUsage is the time of the object's newest usage point; zero without.
func (c *chart) lastUsage() time.Time {
	if pts := c.usage[""]; len(pts) > 0 {
		return pts[len(pts)-1].At
	}
	return time.Time{}
}

// lastOf is the oldest of the newest points of the series in want: where an incremental query
// starts so that every one of them continues without a hole. Zero when one of them is empty.
func (c *chart) lastOf(want Want) time.Time {
	var last time.Time
	take := func(t time.Time) {
		if last.IsZero() || t.Before(last) {
			last = t
		}
	}
	if want.Has(WantUsage) {
		t := c.lastUsage()
		if t.IsZero() {
			return t
		}
		take(t)
	}
	if want.Has(WantNetwork) {
		if len(c.network) == 0 {
			return time.Time{}
		}
		take(c.network[len(c.network)-1].At)
	}
	if want.Has(WantFilesystem) {
		pts := c.fs[""]
		if len(pts) == 0 {
			return time.Time{}
		}
		take(pts[len(pts)-1].At)
	}
	return last
}

// drop forgets a kind of series that no chart shows any more, and its history: shown again, it is
// asked for over the whole range.
func (c *chart) drop(want Want) {
	if want.Has(WantNetwork) {
		c.network, c.net = nil, coverage{}
	}
	if want.Has(WantFilesystem) {
		c.fs, c.fsys, c.fsSource = map[string][]model.FilesystemSample{}, coverage{}, ""
	}
	if want.Has(WantPods) {
		c.pods, c.podsCov, c.podsSource = map[string][]model.CountSample{}, coverage{}, ""
	}
	for i := range c.coarse {
		c.coarse[i].want &^= want
	}
}

// trim keeps each series' points no more than age older than its newest, and at most maxPoints of
// them, the newest. The age counts from the series' own newest point, not from the desktop's
// clock: a cluster whose clock is off by an hour still has a chart. A gap older than the age is
// forgotten.
func (c *chart) trim(age time.Duration, maxPoints int) {
	for k, pts := range c.usage {
		c.usage[k] = bound(pts, age, maxPoints, usageAt)
	}
	c.network = bound(c.network, age, maxPoints, networkAt)
	for k, pts := range c.fs {
		c.fs[k] = bound(pts, age, maxPoints, filesystemAt)
	}
	for k, pts := range c.pods {
		c.pods[k] = bound(pts, age, maxPoints, countAt)
	}
	if last := c.lastUsage(); c.gap != nil && c.gap[1].Before(last.Add(-age)) {
		c.gap = nil
	}
}

func bound[T any](pts []T, age time.Duration, maxPoints int, at func(T) time.Time) []T {
	if len(pts) == 0 {
		return pts
	}
	cutoff := at(pts[len(pts)-1]).Add(-age)
	i := 0
	for i < len(pts) && at(pts[i]).Before(cutoff) {
		i++
	}
	if over := len(pts) - i - maxPoints; maxPoints > 0 && over > 0 {
		i += over
	}
	return pts[i:]
}

// series returns the chart as an ObjectSeries for the engine: the points are shared and must not
// be modified.
func (c *chart) series() *ObjectSeries {
	out := &ObjectSeries{Targets: map[string]*TargetSeries{}, FilesystemSource: c.fsSource, PodsSource: c.podsSource, UsageSource: c.usageSource, ForeignMachines: c.foreign, To: c.at, Step: c.step}
	target := func(name string) *TargetSeries {
		t := out.Targets[name]
		if t == nil {
			t = &TargetSeries{}
			out.Targets[name] = t
		}
		return t
	}
	for name, pts := range c.usage {
		if len(pts) > 0 {
			target(name).Usage = slices.Clip(pts)
		}
	}
	if len(c.network) > 0 {
		target("").Network = slices.Clip(c.network)
	}
	for name, pts := range c.fs {
		if len(pts) > 0 {
			target(name).Filesystem = slices.Clip(pts)
		}
	}
	for name, pts := range c.pods {
		if len(pts) > 0 {
			target(name).Pods = slices.Clip(pts)
		}
	}
	if len(out.FilesystemSource) > 0 && len(c.fs) == 0 {
		out.FilesystemSource = ""
	}
	return out
}

// latestPod is the chart's newest usage as a pod's measurement: the object's newest point and each
// container's at that time or before.
func (c *chart) latestPod(sc Scope) (model.PodUsage, bool) {
	whole := c.usage[""]
	if len(whole) == 0 {
		return model.PodUsage{}, false
	}
	at := whole[len(whole)-1].At
	p := model.PodUsage{Namespace: sc.Namespace, Name: sc.Name, Timestamp: at}
	for _, name := range sortedKeys(c.usage) {
		pts := c.usage[name]
		if name == "" || len(pts) == 0 {
			continue
		}
		p.Containers = append(p.Containers, model.ContainerUsage{Name: name, Usage: pts[len(pts)-1].Usage})
	}
	if len(p.Containers) == 0 { // a provider that has the pod's total only
		p.Containers = []model.ContainerUsage{{Usage: whole[len(whole)-1].Usage}}
	}
	return p, true
}

// latestNode is the chart's newest usage as a node's measurement.
func (c *chart) latestNode(name string) (model.NodeUsage, bool) {
	pts := c.usage[""]
	if len(pts) == 0 {
		return model.NodeUsage{}, false
	}
	last := pts[len(pts)-1]
	return model.NodeUsage{Name: name, Timestamp: last.At, Usage: last.Usage}, true
}
