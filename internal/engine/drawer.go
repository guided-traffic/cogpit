package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// eventsKind is the kind key of core v1 events, which the drawer shows for its object.
const eventsKind = "events"

// maxDetailEvents caps the events a detail model carries.
const maxDetailEvents = 50

// drawerWatch keeps the drawer current: while the drawer is open it holds a handle on the
// object's kind and one on the events in all namespaces, and emits EventDetail, coalesced to at
// most one per SnapshotInterval, when the object or its events change.
type drawerWatch struct {
	backend *Backend
	ref     model.ObjectRef
	profile *model.KindProfile
	obj     *cache.Handle
	events  *cache.Handle            // nil when the cluster serves no events
	related *relatedSet              // the indexes of the profile's Needs; nil without
	usage   *metrics.Subscription    // the pod's or node's metrics poll (metrics.go); nil without or while its metrics are hidden. Guarded by wmu
	pods    []*metrics.Subscription  // the polls of the metrics of the pods its table lists (drawerpods.go); nil while it shows none. Guarded by wmu
	disk    *metrics.Subscription    // a node's poll of the disks (drawerdisk.go); nil without or while hidden. Guarded by wmu
	wmu     sync.Mutex               // guards windows, variants, usage, pods and hidden
	windows map[string]time.Duration // the time range of each chart, by target name (metrics.go)
	unsubs  []func()
	wake    chan struct{}
	quit    chan struct{}
	stopped atomic.Bool
	planKey string // how its kind and the events were read when it started (accessiblens_plan.go); set under state.mu

	// variants is what each chart shows, by target name; absent: CPU (metrics.go). Guarded by wmu.
	variants map[string]string
	// hidden are the metrics the cluster's settings hide (hidemetrics.go); hiddenSet once known.
	// Guarded by wmu.
	hidden    MetricSet
	hiddenSet bool
}

func (s *Session) startDrawerWatch(b *Backend, p *model.KindProfile, ref model.ObjectRef, hidden MetricSet) *drawerWatch {
	ns := ref.Namespace
	if !p.Namespaced {
		ns = ""
	}
	d := &drawerWatch{
		backend: b, ref: ref, profile: p,
		obj:  b.acquireNow(p, ns), // in all namespaces, or the object's alone (accessiblens_follow.go)
		wake: make(chan struct{}, 1),
		quit: make(chan struct{}),
		// The hidden metrics as the drawer opens: its poll starts only for what shows.
		hidden: hidden, hiddenSet: true,
	}
	d.unsubs = append(d.unsubs, d.obj.Subscribe(func(ev cache.Event) {
		// A virtual kind's cache holds what it derives from, not its objects (a Helm release's
		// revisions are Secrets named sh.helm.release.v1.<name>.v<n>): any change in the namespace
		// may change the object, and the drawer derives it again (coalesced like every wake).
		if ev.Object.GetNamespace() == d.namespace() && (p.Derive != nil || ev.Object.GetName() == ref.Name) {
			signal(d.wake)
		}
	}))
	if ep, ok := b.Profile(eventsKind); ok && p.APIObject() { // no event involves a virtual or local kind's object
		d.events = b.acquireNow(ep, ns)
		d.unsubs = append(d.unsubs, d.events.Subscribe(func(ev cache.Event) {
			if d.involves(ev.Object) {
				signal(d.wake)
			}
		}))
	}
	d.unsubs = append(d.unsubs, b.overlays.subscribe(func() { signal(d.wake) })) // the integrations' sections (overlay.go)
	// Woken only by changes to the keys the drawer's model looked up (a node's pods, not every
	// pod); Detail reads through this set while the drawer shows ref (drawerRelated). The drawer's
	// own indexes (DetailNeeds: a workload's pods) live only as long as it does.
	d.related = newRelatedSetWith(b, drawerNeeds(b, p), func() { signal(d.wake) }, true, func(rp *model.KindProfile) *cache.Handle { return b.acquireNow(rp, ns) })
	d.followHidden(s, hidden)
	objSynced, evSynced := d.synced()
	poll := func() bool {
		o, e := d.synced()
		changed := o != objSynced || e != evSynced
		objSynced, evSynced = o, e
		return changed
	}
	s.goSafe("detail drawer", func() {
		runCoalesced(d.quit, d.wake, s.opts.SnapshotInterval, poll, func() {
			if !d.stopped.Load() {
				s.emit(Event{Type: EventDetail, View: ref.Kind})
			}
		})
	})
	return d
}

func (d *drawerWatch) namespace() string {
	if d.profile.Namespaced {
		return d.ref.Namespace
	}
	return ""
}

func (d *drawerWatch) synced() (obj, events bool) {
	obj = d.obj.HasSynced() && d.related.synced()
	events = d.events == nil || d.events.HasSynced()
	return obj, events
}

// involves reports whether a core v1 event is about the drawer's object.
func (d *drawerWatch) involves(ev *unstructured.Unstructured) bool {
	var uid string
	if o, ok := KindGet(d.obj, d.profile, d.namespace(), d.ref.Name); ok {
		uid = string(o.GetUID())
	}
	return eventInvolves(ev, d.profile.Kind, d.namespace(), d.ref.Name, uid)
}

func (d *drawerWatch) stop() {
	if !d.stopped.CompareAndSwap(false, true) {
		return
	}
	close(d.quit)
	for _, u := range d.unsubs {
		u()
	}
	d.obj.Release()
	if d.events != nil {
		d.events.Release()
	}
	d.related.release()
	d.usageSub().Release()
	d.diskSub().Release()
	releaseAll(d.podsSubs())
}

// eventInvolves matches an event's involvedObject by UID when both are known, else by kind,
// namespace and name. Nodes match by kind and name: the kubelet reports node events with the
// node's name as the involved object's UID, not the Node object's UID.
func eventInvolves(ev *unstructured.Unstructured, kind, namespace, name, uid string) bool {
	inv, ok := ev.Object["involvedObject"].(map[string]any)
	if !ok {
		return false
	}
	if kind == "Node" {
		k, _ := inv["kind"].(string)
		n, _ := inv["name"].(string)
		return k == kind && n == name
	}
	if evUID, _ := inv["uid"].(string); evUID != "" && uid != "" {
		return evUID == uid
	}
	k, _ := inv["kind"].(string)
	ns, _ := inv["namespace"].(string)
	n, _ := inv["name"].(string)
	return k == kind && ns == namespace && n == name
}

// Detail builds the drawer model for ref from the cache, without waiting: an object whose kind is
// still being listed comes back with Loading set, one that is not in the cache with Missing set.
func (s *Session) Detail(ref model.ObjectRef) (*DetailModel, error) {
	return s.detail(context.Background(), ref, false)
}

// DetailWait is Detail after waiting, bounded by ctx and 15 seconds, for the object's kind and
// the events to finish their initial list. Agents and the CLI use it; the GUI uses Detail.
func (s *Session) DetailWait(ctx context.Context, ref model.ObjectRef) (*DetailModel, error) {
	return s.detail(ctx, ref, true)
}

func (s *Session) detail(ctx context.Context, ref model.ObjectRef, wait bool) (*DetailModel, error) {
	b, ref, p, err := s.resolve(ref)
	if err != nil {
		return nil, err
	}
	// The object's namespace for an agent's read, once the kind's plan is decided, following a
	// refusal in all namespaces; the GUI's as its drawer reads (accessiblens_follow.go).
	h, evh, rs := s.detailReads(ctx, b, p, ref, wait)
	defer h.Release()
	if evh != nil {
		defer evh.Release()
	}
	defer rs.release()

	obj, ok := KindGet(h, p, ref.Namespace, ref.Name)
	if !ok {
		m := &DetailModel{Ref: ref, Title: detailTitle(p, ref), Sections: []model.DetailSection{}, Events: []EventItem{}, NoEvents: !p.APIObject(), Plain: p.Derive != nil}
		if h.HasSynced() {
			m.Missing = true
		} else {
			m.Loading = true
		}
		return m, nil
	}
	var events []*unstructured.Unstructured
	if evh != nil {
		events = evh.List()
	}
	rel, _ := rs.view()
	m, perr := BuildDetailIn(p, ref, obj, events, s.displayNow(), rel, s.TimeFormat()) // absolute times in the user's zone and format (D-31)
	if perr != nil {
		s.log.Error("engine: kind profile detail panicked", "kind", p.Key, "panic", perr.Error())
	}
	m = s.helmChartDrawer(ctx, p, ref, obj, m, wait) // a chart's version and README (helmchartdrawer.go)
	if wait {
		b.overlays.waitSynced(ctx) // an agent's read sees the integrations' reads listed
	}
	m.Sections = append(m.Sections, b.overlays.sections(p, obj, s.TimeFormat())...) // the integrations' (overlay.go)
	if m.Metrics && len(s.hiddenMetrics(b.Context).drawerVariants(p.Usage)) == 0 {
		m.Metrics = false // the cluster's settings hide every chart of the kind (hidemetrics.go)
	}
	s.drawerPods(b, ref, m)                  // the CPU and Memory of the pods a table lists, and their poll (drawerpods.go)
	SortDrawerTables(m, s.drawerSorts())     // the sortable tables in the drawer's order (drawersort.go)
	LimitDrawerTables(m, MaxDrawerTableRows) // the first rows of a long one, the rest counted (drawerlimit.go)
	switch {
	case ReadsAsAgent(ctx):
		m.Sections = model.WithholdPrivate(m.Sections) // object_get: agents never read private values
	case PrivateTurn(ctx):
		readLikeUser(p, obj, m) // a Secret's values as Show reveals them (secretdata.go, H-94)
	}
	if m.YAML == "" {
		s.log.Warn("engine: object not representable as YAML", "ref", ref.String())
	}
	return m, nil
}

// drawerRelated returns the open drawer's related set, held, when the drawer shows ref through p
// on b, else nil. A fetch of the drawer's model reads the indexes the drawer keeps current instead
// of building its own, and its lookups tell the drawer which keys to wake for.
func (s *Session) drawerRelated(b *Backend, p *model.KindProfile, ref model.ObjectRef) *relatedSet {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	d := st.drawer
	if d == nil || d.backend != b || d.profile != p || !d.ref.Same(ref) || !d.related.hold() {
		return nil
	}
	return d.related
}

// BuildDetail assembles the drawer model of one object from its profile, the object and the
// cached events (of all objects; the object's own are picked out), with ages against now. It is
// the drawer's content without the cache, for the engine and for tests of the UI.
//
// The sections are Properties (the generic metadata, then the items of the kind's own Properties
// section) and the kind's other sections; the events follow, newest first. A profile whose Detail
// panics yields only the generic sections, and the panic as the error. Absolute times are written
// in ISO 8601 in now's zone; the session writes them in the user's format (BuildDetailIn).
func BuildDetail(p *model.KindProfile, ref model.ObjectRef, obj *unstructured.Unstructured, events []*unstructured.Unstructured, now time.Time) (*DetailModel, error) {
	return BuildDetailWith(p, ref, obj, events, now, nil)
}

// BuildDetailWith is BuildDetail with the related objects of the profile's Needs; nil means none.
func BuildDetailWith(p *model.KindProfile, ref model.ObjectRef, obj *unstructured.Unstructured, events []*unstructured.Unstructured, now time.Time, rel model.Related) (*DetailModel, error) {
	return BuildDetailIn(p, ref, obj, events, now, rel, model.TimeLayout{}.In(now.Location()))
}

// BuildDetailIn is BuildDetailWith with the absolute times written in f (D-31): the kinds' and
// the certificates' times are marks (model.MarkTime) until then.
func BuildDetailIn(p *model.KindProfile, ref model.ObjectRef, obj *unstructured.Unstructured, events []*unstructured.Unstructured, now time.Time, rel model.Related, f model.TimeFormat) (*DetailModel, error) {
	m, err := buildDetail(p, ref, obj, events, now, rel)
	m.Sections = f.WriteSections(m.Sections)
	return m, err
}

func buildDetail(p *model.KindProfile, ref model.ObjectRef, obj *unstructured.Unstructured, events []*unstructured.Unstructured, now time.Time, rel model.Related) (*DetailModel, error) {
	m := &DetailModel{
		Ref: ref, Title: detailTitle(p, ref),
		Sections: []model.DetailSection{}, Events: []EventItem{},
	}
	m.Status = model.BuildRowWith(p, ref.Context, obj, rel).Status
	m.Metrics = p.Usage != ""
	m.NoEvents = p.Local // no event involves an object Cogpit makes itself (port forwards, Helm charts)
	kind, err := profileSections(p, obj, rel)
	if p.Derive != nil { // a virtual kind's drawer is the profile's alone (model.KindProfile.Detail)
		m.NoEvents, m.Plain = true, true
		if kind != nil {
			m.Sections = kind
		}
	} else {
		meta := obj
		if isSecrets(p) {
			// The cache drops a Secret's last-applied annotation, which repeats its values; should
			// one arrive another way, its Annotations show it redacted.
			meta = model.Redact(obj)
		}
		owners := func(apiVersion, kind, namespace, name string) *model.ObjectRef { // custom owners too (drawerNeeds)
			return kinds.RefToWith(rel, apiVersion, kind, namespace, name)
		}
		m.Sections = model.MergeProperties(model.PropertiesSectionLinked(meta, now, owners), kind)
		if isSecrets(p) {
			m.Sections = append(m.Sections, secretDataSection(ref, obj)) // keys and sizes, values masked
			// The certificates its values hold, decoded (D-30): never a value, PEM text or key.
			certs, cerr := secretCertificates(obj, now)
			m.Certificates = certs
			m.Sections = append(m.Sections, certificateSections(certs, now)...)
			err = errors.Join(err, cerr)
		}
	}
	if len(events) > 0 && !m.NoEvents {
		m.Events = objectEvents(events, p.Kind, ref.Namespace, ref.Name, string(obj.GetUID()))
	}
	if y, yerr := model.ObjectYAML(model.Redact(obj).Object); yerr == nil { // the Kubernetes order (D-86)
		m.YAML = string(y)
	}
	if p.Key == "pods" && p.Group == "" {
		m.Containers = ContainerNames(obj, false)
		m.InitContainers = containerNames(obj, "initContainers")
	}
	return m, err
}

// drawerNeeds are the related-objects indexes a kind's drawer reads: its profile's, and on a
// cluster with custom kinds the definitions by group and Kind, through which Controlled By links an
// owner that is a custom resource (a StatefulSet's Prometheus, a Deployment's CephCluster, seen on a
// real cluster 2026-10-16). Their scope is the custom kinds' own watch, so the index costs no
// request; a virtual or local kind's drawer has no owners to link.
func drawerNeeds(b *Backend, p *model.KindProfile) []model.RelatedIndex {
	needs := p.DrawerNeeds()
	if b == nil || !b.CustomResources || p.Derive != nil || p.Local {
		return needs
	}
	for _, ix := range needs {
		if ix.ID() == kinds.CustomKindsByGroupKind.ID() {
			return needs
		}
	}
	return append(append(make([]model.RelatedIndex, 0, len(needs)+1), needs...), kinds.CustomKindsByGroupKind)
}

// detailTitle is the drawer's title: "Pod: web-0", "Release: web" (a profile's DetailTitle).
func detailTitle(p *model.KindProfile, ref model.ObjectRef) string {
	if p.DetailTitle != "" {
		return p.DetailTitle + ": " + ref.Name
	}
	return p.Kind + ": " + ref.Name
}

// profileSections returns the kind's own drawer sections; a panicking profile yields none and
// the panic as an error.
func profileSections(p *model.KindProfile, obj *unstructured.Unstructured, rel model.Related) (out []model.DetailSection, err error) {
	if p.Detail == nil && p.DetailWith == nil {
		return nil, nil
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("%v", r)
		}
	}()
	return p.DetailSections(obj, rel), nil
}

// objectEvents returns the events about one object, newest first, at most maxDetailEvents.
func objectEvents(all []*unstructured.Unstructured, kind, namespace, name, uid string) []EventItem {
	out := []EventItem{}
	for _, ev := range all {
		if eventInvolves(ev, kind, namespace, name, uid) {
			out = append(out, eventItem(ev))
		}
	}
	slices.SortStableFunc(out, func(a, b EventItem) int {
		if a.LastSeen != b.LastSeen {
			if a.LastSeen > b.LastSeen {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	if len(out) > maxDetailEvents {
		out = out[:maxDetailEvents]
	}
	return out
}

func eventItem(ev *unstructured.Unstructured) EventItem {
	it := EventItem{
		Type:    nestedString(ev.Object, "type"),
		Reason:  nestedString(ev.Object, "reason"),
		Message: nestedString(ev.Object, "message"),
	}
	src := nestedString(ev.Object, "source", "component")
	if src == "" {
		src = nestedString(ev.Object, "reportingComponent")
	}
	if host := nestedString(ev.Object, "source", "host"); host != "" && src != "" {
		src += " " + host
	}
	it.Source = src
	it.Count = nestedInt(ev.Object, "count")
	if it.Count == 0 {
		it.Count = nestedInt(ev.Object, "series", "count")
	}
	for _, path := range [][]string{
		{"series", "lastObservedTime"}, {"lastTimestamp"}, {"eventTime"}, {"firstTimestamp"},
		{"metadata", "creationTimestamp"},
	} {
		if t, err := time.Parse(time.RFC3339Nano, nestedString(ev.Object, path...)); err == nil {
			it.LastSeen = t.Unix()
			break
		}
	}
	switch it.Type {
	case "Warning":
		it.Status = model.StatusWarning
	case "Normal":
		it.Status = model.StatusOK
	}
	return it
}

// ContainerNames returns the container names of a pod: spec.containers, then, with init, the
// init containers. Other shapes yield none.
func ContainerNames(obj *unstructured.Unstructured, init bool) []string {
	out := containerNames(obj, "containers")
	if init {
		out = append(out, containerNames(obj, "initContainers")...)
	}
	return out
}

// containerNames returns the names in a pod's spec.<field> list.
func containerNames(obj *unstructured.Unstructured, field string) []string {
	v, _, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", field)
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, c := range list {
		if m, ok := c.(map[string]any); ok {
			if n, _ := m["name"].(string); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// nestedString reads a string field without copying and without failing on other shapes.
func nestedString(obj map[string]any, fields ...string) string {
	v, _, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	s, _ := v.(string)
	return s
}

// nestedInt reads an integer field that JSON decoding may have produced as int64 or float64.
func nestedInt(obj map[string]any, fields ...string) int64 {
	v, _, _ := unstructured.NestedFieldNoCopy(obj, fields...)
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
