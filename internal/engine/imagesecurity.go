package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Image Security page (D-178): the Trivy integration's page under the navigator's heading
// Integrations, shown while the integration is on for the cluster and the cluster serves Trivy
// Operator's VulnerabilityReports. While it is open its worker holds two handles: the reports in a
// scope trimmed to the fields the page shows (trivyFindingsTrim; the integration's own read keeps
// them without their vulnerability lists, trivy.go) and the pods, for the images that run. Both
// are released when the page closes. The model and the table are imagesecurity_model.go's.

// ImageSecurity is the page key of the Image Security page with its CVEs table, one row per CVE;
// ImageSecurityFindings the same page with its findings table, one row per CVE, workload and
// container. Both are one page: the same header, cards and reading; the key says which table it
// shows, so that each table keeps its own sort and columns and Back returns from one to the other.
const (
	ImageSecurity         = "trivy/image-security"
	ImageSecurityFindings = "trivy/image-security/findings"
)

// IsImageSecurity reports whether a view is the Image Security page, with either table.
func IsImageSecurity(view string) bool {
	return view == ImageSecurity || view == ImageSecurityFindings
}

// ImageSecurityTitle is the page's title in its header, its tab and the navigator.
const ImageSecurityTitle = "Image Security"

// ImageSecurityDefaultFilter is the search the page opens with: unfiltered, its findings run to
// tens of thousands of rows on a large cluster, beyond the lists' budget of 10,000.
const ImageSecurityDefaultFilter = tokenSevAtLeast + "high"

// TrivyReportsKind is the kind key of Trivy Operator's vulnerability reports, the kind the page
// needs the cluster to serve.
const TrivyReportsKind = trivyReportsKind

// trivyFindingsTrim names the trim of the page's scope of the reports (cache.RegisterTrim).
const trivyFindingsTrim = "trivy-findings"

// findingFields are the fields of a vulnerability the page shows; the trim drops the others (the
// title, the description, links, CVSS vectors, the target, the package's path and PURL).
var findingFields = map[string]bool{
	"vulnerabilityID": true, "resource": true, "installedVersion": true, "fixedVersion": true, "severity": true, "score": true,
}

// pageFilters are the searches pages open with (UIState.openTab); a page not named opens without.
var pageFilters = map[string]string{}

func init() {
	pageTitles[ImageSecurity] = ImageSecurityTitle
	pageTitles[ImageSecurityFindings] = ImageSecurityTitle
	pageFilters[ImageSecurity] = ImageSecurityDefaultFilter
	pageFilters[ImageSecurityFindings] = ImageSecurityDefaultFilter
	cache.RegisterTrim(trivyFindingsTrim, trimVulnerabilityFindings)
}

// trimVulnerabilityFindings keeps of each vulnerability what the page shows (findingFields), in a
// map of its own: a map does not shrink when keys are deleted, and the kept six fit the smallest
// (on synthetic reports shaped as a real cluster's, 23,059 findings: 26.9 MB decoded, 23.5 MB with
// nine fields deleted in place, 12.0 MB with six kept in their own maps; D-178). The rest of the
// report stays as it is.
func trimVulnerabilityFindings(u *unstructured.Unstructured) {
	report, ok := u.Object["report"].(map[string]any)
	if !ok {
		return
	}
	list, _ := report["vulnerabilities"].([]any)
	for i, v := range list {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		kept := make(map[string]any, len(findingFields))
		for k := range findingFields {
			if x, ok := m[k]; ok {
				kept[k] = x
			}
		}
		list[i] = kept
	}
}

// ErrTrivyOff says that the Trivy integration is off for the cluster.
var ErrTrivyOff = errors.New("the Trivy integration is off for this cluster: switch it on in Cluster Settings › Integrations")

// trivyReportsWhat names the reports in the text of a page that cannot tell whether the cluster
// serves them.
const trivyReportsWhat = "Trivy Operator's reports"

// imageSecReason says why the page reads nothing on a backend: the integration is off, the
// cluster does not serve the reports, or Cogpit cannot tell; "" when it reads.
func imageSecReason(b *Backend, u *UIState) string {
	if !u.IntegrationOn(b.Context, IntegrationTrivy) {
		return ErrTrivyOff.Error() + "."
	}
	if p, ok := b.Profile(trivyReportsKind); ok && p.Namespaced {
		return ""
	}
	if why := b.customKindsUnknown(trivyReportsWhat); why != "" {
		return why
	}
	return trivyNotServed + "."
}

// imageSecSource is what the page reads: the reports, trimmed, and the pods, each as its namespace
// plan says; nothing when the page reads nothing (reason).
type imageSecSource struct {
	reports, pods   *cache.Handle // nil when not read
	reportsP, podsP *model.KindProfile
	unsubs          []func()
	reason          string
	denied          string        // the reports the access reviews say the user may not list (mayNotList); not read
	gen             atomic.Uint64 // bumped by every event of either
	onChange        func()
}

// newImageSecSource acquires the page's handles: the reports as plan says (trimmed), the pods
// through acquire (the open page's acquireForPage, an agent's read's Acquire once the plans are
// decided), the pods only when the user may list them as far as the access reviews know.
func newImageSecSource(b *Backend, reason string, acquire func(*model.KindProfile) *cache.Handle, onChange func()) *imageSecSource {
	src := &imageSecSource{reason: reason, onChange: onChange}
	if reason != "" {
		return src
	}
	src.reportsP, _ = b.Profile(trivyReportsKind)
	if b.deniedKind(src.reportsP) {
		src.denied = mayNotList(src.reportsP) // a read the API server would refuse (D-177)
	} else {
		src.reports = b.acquireTrimmed(src.reportsP, trivyFindingsTrim)
		src.unsubs = append(src.unsubs, src.reports.Subscribe(src.changed))
	}
	if p, ok := b.Profile(logKindPods); ok {
		if listable, known := b.Listable(p, nil); !known || listable {
			src.podsP = p
			src.pods = acquire(p)
			src.unsubs = append(src.unsubs, src.pods.Subscribe(src.changed))
		}
	}
	return src
}

func (src *imageSecSource) changed(cache.Event) {
	src.gen.Add(1)
	if src.onChange != nil {
		src.onChange()
	}
}

// acquireTrimmed acquires a kind's scope with a trim, as its namespace plan says: in all
// namespaces, in each namespace the user named for a user who may not list it everywhere (a read
// in all of them refused once included), nothing while the access reviews have not said which.
func (b *Backend) acquireTrimmed(p *model.KindProfile, trim string) *cache.Handle {
	sc := b.Scope(p)
	sc.Trim = trim
	plan := b.namespacePlan(p)
	switch {
	case plan.pending:
		return cache.Pending(sc)
	case plan.perNamespace:
		return b.Cache.AcquireNamespaces(sc, plan.watch)
	}
	b.ns.planMu.RLock()
	defer b.ns.planMu.RUnlock()
	if b.ns.isRefused(p.GVR()) {
		if now := b.namespacePlan(p); now.perNamespace {
			return b.Cache.AcquireNamespaces(sc, now.watch)
		}
	}
	return b.Cache.Acquire(sc)
}

// kindHandles are the source's handles, for openFollowing.
func (src *imageSecSource) kindHandles() []kindHandle {
	var out []kindHandle
	if src.reports != nil {
		out = append(out, kindHandle{src.reportsP, src.reports})
	}
	if src.pods != nil {
		out = append(out, kindHandle{src.podsP, src.pods})
	}
	return out
}

func (src *imageSecSource) release() {
	for _, u := range src.unsubs {
		u()
	}
	for _, k := range src.kindHandles() {
		k.h.Release()
	}
}

func (src *imageSecSource) synced() bool {
	for _, k := range src.kindHandles() {
		if !k.h.HasSynced() {
			return false
		}
	}
	return true
}

func (src *imageSecSource) waitSynced(ctx context.Context) {
	for _, k := range src.kindHandles() {
		k.h.WaitForSync(ctx)
	}
}

func (src *imageSecSource) errText() string {
	var parts []string
	if src.denied != "" {
		parts = append(parts, src.denied)
	}
	if src.reports != nil && src.reports.Err() != nil {
		parts = append(parts, "Vulnerability reports: "+src.reports.Err().Error())
	}
	if src.pods != nil && src.pods.Err() != nil {
		parts = append(parts, "Pods: "+src.pods.Err().Error())
	}
	return strings.Join(parts, "; ")
}

// build reads the cache.
func (src *imageSecSource) build(context string) *imageSecBuild {
	if src.reason != "" {
		return &imageSecBuild{synced: true, reason: src.reason}
	}
	synced := src.synced() // before List, as views do
	// Pods the API server refused count nothing: the header leaves the running images out (D-177).
	podsRead := src.pods != nil && !refusedAll(src.pods)
	var pods, reports []*unstructured.Unstructured
	if podsRead {
		pods = KindList(src.pods, src.podsP)
	}
	if src.reports != nil {
		reports = src.reports.List()
	}
	fb := newImageSecBuild(context, reports, pods, podsRead)
	fb.synced, fb.errText = synced, src.errText()
	return fb
}

// imageSecWorker keeps the page current while it is open: its source's handles, its build rebuilt
// at most once per SnapshotInterval after a change, and the table again when only the search, the
// namespaces or the sort changed.
type imageSecWorker struct {
	s       *Session
	backend *Backend
	src     *imageSecSource
	planKey string       // what the open page reads and how (imageSecPlanKey); the state's lock
	refused refusedReads // the reads given back as refused (pagerefusals.go)

	mu       sync.Mutex
	params   viewParams
	view     string // the table shown: ImageSecurity (the CVEs) or ImageSecurityFindings
	builtGen uint64 // the source's gen the build was made at; the loop's alone

	snap     atomic.Pointer[TableSnapshot]
	build    atomic.Pointer[imageSecBuild]
	ready    chan struct{} // closed once the first build is stored
	readyNow sync.Once
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool
}

// startImageSecurity starts the page's worker; the first build runs at once.
func (s *Session) startImageSecurity(b *Backend, reason, view string, params viewParams) *imageSecWorker {
	w := &imageSecWorker{s: s, backend: b, params: params, view: view, ready: make(chan struct{}), wake: make(chan struct{}, 1), quit: make(chan struct{})}
	w.refused.startAt(b.ns.refusalsGen())
	wake := w.wake
	w.src = newImageSecSource(b, reason, b.acquireForPage, func() { signal(wake) }) // nothing while a plan is pending
	signal(w.wake)
	s.goSafe("image security", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *imageSecWorker) setParams(view string, p viewParams) {
	w.mu.Lock()
	changed := !w.params.equal(p) || w.view != view
	w.params, w.view = p, view
	w.mu.Unlock()
	if changed {
		signal(w.wake)
	}
}

// stop ends the loop and releases the handles; the informers stay warm.
func (w *imageSecWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.src.release()
}

// unsettled reports whether a sync state or an error changed since the last build: neither comes
// with a watch event. A read the API server refused wholly goes back first, its refusal kept
// (pagerefusals.go).
func (w *imageSecWorker) unsettled() bool {
	gave := w.refused.giveBack(w.src.reports, w.src.pods)
	last := w.build.Load()
	return last == nil || gave || (w.src.reason == "" && (last.synced != w.src.synced() || last.errText != w.src.errText()))
}

// rebuild reads the cache again after a change of the reports or the pods, builds the table and
// announces it; a panic is reported once and does not end the loop.
func (w *imageSecWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the image security page failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the Image Security page failed to update: internal error"})
			}
		}
	}()
	w.mu.Lock()
	p, view := w.params, w.view
	w.mu.Unlock()
	fb := w.build.Load()
	if gen := w.src.gen.Load(); fb == nil || gen != w.builtGen || w.unsettled() {
		w.builtGen = gen // before the list: an event during the build builds it again
		fb = w.src.build(w.backend.Context)
	}
	snap := imageSecTable(w.backend.Context, view, fb, p)
	snap.Revision = w.s.state.snapshots.Add(1)
	if w.stopped.Load() {
		return
	}
	w.build.Store(fb)
	w.snap.Store(snap)
	w.readyNow.Do(func() { close(w.ready) })
	w.s.emit(Event{Type: EventTable, View: view, Revision: snap.Revision})
}

// ImageSecurity returns the page's model for the given namespaces (nil: the namespace filter the
// user has set) and, with table (ImageSecurity for the CVEs, ImageSecurityFindings for the
// findings; "" none), that table for a search (nil: the page's default,
// ImageSecurityDefaultFilter) in its default order. While the page is open on the cluster it is built from the page's
// latest reading; otherwise from a reading of its own: the page's handles are held for the call,
// which waits (bounded by ctx and 15 seconds) for their initial lists, and released after. It
// fails while the Trivy integration is off for the cluster or the cluster does not serve the
// reports.
func (s *Session) ImageSecurity(ctx context.Context, contextID string, namespaces []string, filter *string, table string) (*ImageSecurityModel, *TableSnapshot, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, nil, err
	}
	u := s.UIState()
	if !u.IntegrationOn(b.Context, IntegrationTrivy) {
		return nil, nil, ErrTrivyOff
	}
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	params := viewParams{namespaces: slices.Clone(namespaces), filter: ImageSecurityDefaultFilter, sort: imageSecSort}
	if table == ImageSecurity {
		params.sort = imageSecCVESort
	}
	if filter != nil {
		params.filter = *filter
	}
	fb, err := s.imageSecReading(ctx, b, &u)
	if err != nil {
		return nil, nil, err
	}
	var snap *TableSnapshot
	if IsImageSecurity(table) {
		snap = imageSecTable(b.Context, table, fb, params)
	}
	m := imageSecModel(fb, namespaces)
	m.Trend = s.imageSecurityTrend(b, u)
	return m, snap, nil
}

// imageSecReading is the page's latest reading while it is open on the backend's cluster;
// otherwise a reading of its own: the page's handles held for the call, which waits (bounded by
// ctx and 15 seconds) for their initial lists, and released after. It fails where the page reads
// nothing (the reports not served).
func (s *Session) imageSecReading(ctx context.Context, b *Backend, u *UIState) (*imageSecBuild, error) {
	s.state.mu.Lock()
	page := s.state.imageSec
	s.state.mu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	var fb *imageSecBuild
	if page != nil && page.backend == b {
		// A page just started builds at once: its build, not a reading of the call's own.
		select {
		case <-page.ready:
		case <-page.quit:
		case <-wctx.Done():
		}
		fb = page.build.Load()
	}
	if fb == nil {
		waitCustomKindsSettled(wctx, b) // before, the reports' kind may not be known yet
		reason := imageSecReason(b, u)
		src := openFollowing(wctx, b, []string{trivyReportsKind, logKindPods}, func() (*imageSecSource, []kindHandle) {
			src := newImageSecSource(b, reason, b.Acquire, nil)
			return src, src.kindHandles()
		}, (*imageSecSource).release)
		defer src.release()
		src.waitSynced(wctx)
		fb = src.build(b.Context)
	}
	if fb.reason != "" {
		return nil, fmt.Errorf("%w: %s", ErrNotApplicable, fb.reason)
	}
	return fb, nil
}

// imageSecPlanKey is how the open page reads: why it reads nothing, else the reports' version
// and how the reports and the pods are read (pageReadKey: a pending plan, all namespaces, or the
// namespaces).
func (b *Backend) imageSecPlanKey(reason string) string {
	if reason != "" {
		return "none: " + reason
	}
	p, _ := b.Profile(trivyReportsKind)
	return p.Version + "|" + b.pageReadKey([]string{trivyReportsKind, logKindPods})
}

// reconcileImageSecPageLocked runs the page's worker: started when the page opens, given the UI's
// namespaces, search, sort and columns, stopped when another view opens or the backend changes,
// and started again when the integration is switched, the reports' kind comes, goes or moves to
// another version, the access reviews change how the page reads, or cluster.refreshAccess follows a
// read given back as refused. The page's sort is remembered
// as a view's is.
func (st *state) reconcileImageSecPageLocked(b *Backend, now UIState) {
	want := b != nil && IsImageSecurity(now.View)
	var key, reason string
	if want {
		reason = imageSecReason(b, &now)
		key = b.imageSecPlanKey(reason)
	}
	if st.imageSec != nil && (!want || st.imageSec.backend != b || st.imageSec.planKey != key || st.imageSec.refused.retake(b)) {
		st.imageSec.stop()
		st.imageSec = nil
	}
	switch {
	case want && st.imageSec == nil:
		st.imageSec = st.s.startImageSecurity(b, reason, now.View, paramsOf(now))
		st.imageSec.planKey = key
	case st.imageSec != nil:
		st.imageSec.setParams(now.View, paramsOf(now))
	}
	if st.imageSec != nil && imageSecProfileOf(now.View).ColumnIndex(now.Sort.Column) >= 0 {
		st.sorts[now.View] = now.Sort
	}
}

// detachImageSecLocked stops the page's worker.
func (st *state) detachImageSecLocked() {
	if st.imageSec != nil {
		st.imageSec.stop()
		st.imageSec = nil
	}
}

// imageSecTableOf returns the open page's latest snapshot of the view's table, or nil.
func (st *state) imageSecTableOf(view string) *TableSnapshot {
	st.mu.Lock()
	w := st.imageSec
	st.mu.Unlock()
	if w == nil {
		return nil
	}
	if snap := w.snap.Load(); snap != nil && snap.View == view {
		return snap
	}
	return nil
}
