package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Trivy integration (D-78; the owner's I6, 2026-10-17: "an allen Pods das schlechteste
// Image-Rating aller Container, im Pod Drawer das Image Rating pro Image, wenn verfügbar"). Trivy
// Operator scans the images of a cluster's workloads and writes one VulnerabilityReport
// (aquasecurity.github.io/v1alpha1, namespaced) per workload and container, labelled with the
// workload's kind, name and namespace and the container's name (model.TrivyLabel*). Cogpit installs
// nothing: it reads the reports a running Trivy Operator wrote.
//
// While the integration is on for a connected cluster that serves the reports' kind (its custom
// resource definition, through the custom kinds watch), the integration's follower puts its overlay
// on the backend (trivySource, overlay.go) and takes it off when the integration is switched off.
// The overlay holds one handle on the reports, read as their namespace plan says (in all
// namespaces, or in each namespace the user named for a user who may not list them everywhere;
// nothing while the access reviews have not said which), in a trimmed scope (cache.Scope.Trim): the
// cache keeps each report without its vulnerability list (trimVulnerabilityReport), while the
// reports' own view, YAML and editor read the untrimmed scope and keep them whole. The index over
// them is built from the cache on the first lookup after a change and maps a workload and a
// container to the report's summary; a change of the reports tells the overlays, coalesced, which
// wakes the open view and drawer. The overlay gives the Pods list its column "Image Rating" (the
// worst grade of a pod's images, drawn as a shield, model.ImageGrade) and each container section of
// a pod's drawer its image's grade (D-new). Switched off: no overlay, so no column, no grade and no
// read.
//
// A pod's reports are its controller's, as Trivy Operator resolves a pod's workload (its
// ReportOwner, read in its source and seen on kind 2026-10-17): a ReplicaSet's (a Deployment's
// pods), a StatefulSet's, a DaemonSet's, a ReplicationController's, a Job's, the CronJob's of a
// Job a CronJob made, and the pod's own for a bare pod or one another controller made. The
// CronJob of a Job is found by the Job's name, as the CronJob controller names its Jobs
// ("<cronjob>-<scheduled minute>"), after the Job's own reports: the lead's decision (2026-10-17),
// so that a pod costs no read of its Job (the Jobs are not watched for it); a Job made by hand from
// a CronJob under another name has no rating.

// trivyReportsKind is the kind key of Trivy Operator's vulnerability reports.
const trivyReportsKind = "vulnerabilityreports.aquasecurity.github.io"

// trivyNotServed is the Trivy integration's detail on a cluster without the reports' kind.
const trivyNotServed = "Trivy Operator's reports are not served"

// trivyTrim names the trim of the reports' scope (cache.RegisterTrim).
const trivyTrim = "trivy-summary"

// trivyColumnID is the Pods list's column of the worst image grade, its title (for agents and the
// columns menu: the header shows none, as the warning column's), its fixed width and the column it
// shows after. The owner (2026-10-22): no title and a fixed width right after Containers, to save
// room as the warning column does; 32 points hold the 18-point shield and the header's sort arrow
// (the lead's choice).
const (
	trivyColumnID    = "imageRating"
	trivyColumnTitle = "Image Rating"
	trivyColumnWidth = 32
	trivyColumnAfter = specContainers // the Containers column: the images' grade beside their squares
)

// initSuffix marks an init container among the containers that run an image.
const initSuffix = " (init)"

// The workload kinds a report names that a pod's lookup picks itself, and a pod spec's container lists.
const (
	trivyKindPod       = "Pod"
	trivyKindJob       = "Job"
	trivyKindCronJob   = "CronJob"
	specContainers     = "containers"
	specInitContainers = "initContainers"
)

func init() {
	cache.RegisterTrim(trivyTrim, trimVulnerabilityReport)
	integrationProbes[IntegrationTrivy] = probeTrivy
	integrationFollowers = append(integrationFollowers, followTrivy)
}

// trimVulnerabilityReport keeps what the integration reads of a report: everything but its
// vulnerability list (report.vulnerabilities), which is 95 % of a report's weight (D-78, measured
// on kind). Metadata, the artifact, the registry, the summary and the scan's time stay.
func trimVulnerabilityReport(u *unstructured.Unstructured) {
	if report, ok := u.Object["report"].(map[string]any); ok {
		delete(report, "vulnerabilities")
	}
}

// followTrivy is the Trivy integration's follower: it puts the overlay on a connected cluster while
// the integration is on and the cluster serves the reports, puts a new one when the reports' kind
// or namespace plan changes, and takes it off when either ends. It runs while the engine reconciles
// (state.mu held): it does not wait.
func followTrivy(st *state, b *Backend, now UIState) {
	if b == nil || b.Cache == nil {
		return
	}
	cur, _ := overlayOf[*trivySource](b)
	p, served := b.Profile(trivyReportsKind)
	// Read only while the cluster is the one shown, as Kyverno's reports (kyverno.go): its ratings show
	// only in its views, and a cluster connected in the background goes cold (D-69; the lead's
	// decision, 2026-10-17, D-78).
	want := served && p.Namespaced && now.IntegrationOn(b.Context, IntegrationTrivy) && now.Context == b.Context
	key := ""
	if want {
		key = p.GVR().String() + "=" + b.namespacePlan(p).key()
	}
	switch {
	case cur == nil && !want, cur != nil && cur.key == key:
		return
	case want:
		b.overlays.put(IntegrationTrivy, startTrivy(st.s, b, p, key))
	default:
		b.overlays.put(IntegrationTrivy, nil)
	}
	ctx := b.Context
	st.s.goSafe("trivy integration", func() { st.s.integrationsChanged(ctx) })
}

// isPodsKind reports whether a profile is the core Pods'.
func isPodsKind(p *model.KindProfile) bool {
	return p != nil && p.Key == logKindPods && p.Group == ""
}

// trivyWorkload names the workload a report is of: its namespace, kind and name.
type trivyWorkload struct{ namespace, kind, name string }

// trivySource is the integration's read of the reports on one cluster and the index over them; it
// is the backend's Trivy overlay while the integration is on.
type trivySource struct {
	s       *Session
	b       *Backend
	p       *model.KindProfile
	key     string // what it reads (followTrivy): another key puts a new overlay
	plan    nsPlan
	h       *cache.Handle
	unsub   func()
	wake    chan struct{}
	quit    chan struct{}
	stopped atomic.Bool
	gen     atomic.Uint64 // bumped by every event of the reports

	mu    sync.Mutex
	built uint64 // gen that idx was built at
	idx   trivyReportIndex
	count int
}

// trivyReportIndex maps a workload to its reports by container name. Built once, never changed.
type trivyReportIndex map[trivyWorkload]map[string]model.VulnerabilityReport

// newTrivyReportIndex indexes report objects; it returns the index and how many reports it holds.
func newTrivyReportIndex(objs []*unstructured.Unstructured) (trivyReportIndex, int) {
	idx := trivyReportIndex{}
	count := 0
	for _, obj := range objs {
		r, ok := model.ParseVulnerabilityReport(obj)
		if !ok {
			continue
		}
		count++
		w := trivyWorkload{namespace: r.Namespace, kind: r.Kind, name: r.Workload}
		byContainer := idx[w]
		if byContainer == nil {
			byContainer = map[string]model.VulnerabilityReport{}
			idx[w] = byContainer
		}
		if cur, ok := byContainer[r.Container]; !ok || r.Scanned.After(cur.Scanned) {
			byContainer[r.Container] = r // two reports of one container: the newer scan
		}
	}
	return idx, count
}

// startTrivy acquires the reports' trimmed scope as their plan says and starts the loop that tells
// the overlays of changes.
func startTrivy(s *Session, b *Backend, p *model.KindProfile, key string) *trivySource {
	t := &trivySource{s: s, b: b, p: p, key: key, plan: b.namespacePlan(p),
		wake: make(chan struct{}, 1), quit: make(chan struct{})}
	sc := b.Scope(p)
	sc.Trim = trivyTrim
	switch {
	case t.plan.pending: // the access reviews have not said whether the user may list them everywhere
		t.h = cache.Pending(sc)
	case t.plan.perNamespace:
		t.h = b.Cache.AcquireNamespaces(sc, t.plan.watch)
	default:
		t.h = b.Cache.Acquire(sc)
	}
	t.unsub = t.h.Subscribe(func(cache.Event) {
		t.gen.Add(1)
		signal(t.wake)
	})
	s.goSafe("trivy reports "+b.Context, t.run)
	return t
}

// run tells the overlays, at most once per snapshot interval, that the reports changed (the open
// view's ratings and the drawer's sections follow), the settings page when what the integration
// knows changed (the reports listed, a refusal), and the session when a plan that was pending is
// decided (the follower then reads as it says), until the overlay stops.
func (t *trivySource) run() {
	settled, replanned := t.settled(), false
	poll := func() bool {
		return t.settled() != settled || (!replanned && t.planSettled())
	}
	runCoalesced(t.quit, t.wake, t.s.opts.SnapshotInterval, poll, func() {
		if t.stopped.Load() {
			return
		}
		t.b.overlays.changed()
		if now := t.settled(); now != settled {
			settled = now
			t.s.integrationsChanged(t.b.Context)
		}
		if !replanned && t.planSettled() {
			replanned = true
			t.b.ns.wakeReplan()
		}
	})
}

// settled is what the integration's state reads of the handle: synced, and its error.
func (t *trivySource) settled() string {
	return fmt.Sprint(t.h.HasSynced(), errText(t.h.Err()))
}

// planSettled reports whether the read started while its plan was pending and the plan is decided now.
func (t *trivySource) planSettled() bool {
	return t.plan.pending && !t.b.namespacePlan(t.p).pending
}

func (t *trivySource) stop() {
	if !t.stopped.CompareAndSwap(false, true) {
		return
	}
	close(t.quit)
	t.unsub()
	t.h.Release()
}

// synced reports whether the reports' initial list is done.
func (t *trivySource) synced() bool { return t != nil && t.h.HasSynced() }

// waitSynced waits, bounded by ctx, until the reports listed or were refused: an agent's read sees
// what a connect just started to read.
func (t *trivySource) waitSynced(ctx context.Context) {
	if t.plan.pending {
		return
	}
	for !t.h.HasSynced() && !cache.IsRefused(t.h.Err()) && ctx.Err() == nil && !t.stopped.Load() {
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// index returns the index, built from the cache first when the reports changed since.
func (t *trivySource) index() (trivyReportIndex, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	gen := t.gen.Load() // before the list: an event during the build builds it again next time
	if t.idx == nil || t.built != gen {
		t.idx, t.count = newTrivyReportIndex(t.h.List())
		t.built = gen
	}
	return t.idx, t.count
}

// reports returns how many reports the index holds.
func (t *trivySource) reports() int {
	_, n := t.index()
	return n
}

// columns: the Pods list's "Image Rating", shown after Containers, while the overlay is on (also
// while no pod has a grade yet: a column that comes and goes with the reports would move the
// columns after it). Other kinds get none.
func (t *trivySource) columns(p *model.KindProfile) []overlayColumn {
	if !isPodsKind(p) {
		return nil
	}
	return []overlayColumn{{
		Column: model.Column{ID: trivyColumnID, Title: trivyColumnTitle, Type: model.ColumnGrade, Width: trivyColumnWidth},
		After:  trivyColumnAfter,
	}}
}

// cell is a pod's worst image grade, drawn as its shield: its text says the grade and the count
// that decides it ("D · 2 Critical"), its tooltip every severity's count, the end of life of the
// image's OS, and which of several images it is; sorted by the grade, then that count. A pod none
// of whose images has a report is N/A (the owner, 2026-10-22: N/A everywhere, unobtrusive); a pod
// some of whose images have one, the worst of those (D-78). None while the reports list.
func (t *trivySource) cell(p *model.KindProfile, column string, obj *unstructured.Unstructured) (model.Cell, bool) {
	if column != trivyColumnID || !isPodsKind(p) || !t.synced() {
		return model.Cell{}, false
	}
	idx, _ := t.index()
	images := idx.podImages(obj)
	worst, ok := worstImage(images)
	if !ok {
		return model.Cell{
			Text: model.GradeText(model.GradeNA, model.VulnerabilityCounts{}, false), Grade: model.GradeNA,
			Num: model.GradeRank(model.GradeNA, model.VulnerabilityCounts{}, false), Tip: "No Trivy report of this pod's images",
		}, true
	}
	r := worst.report
	g := r.Grade()
	tip := []string{r.Counts.Summary()}
	if r.EOSL {
		tip = append(tip, r.OS()+": end of life")
	}
	if len(images) > 1 {
		tip = append(tip, fmt.Sprintf("The worst of %d images: %s", len(images), worst.image))
	}
	return model.Cell{
		Text: model.GradeText(g, r.Counts, r.EOSL), Grade: g, Num: model.GradeRank(g, r.Counts, r.EOSL),
		Tip: strings.Join(tip, "\n"),
	}, true
}

// worstImage returns the image of the worst grade among those with a report (by grade, then the
// count that decides it); ok is false when none has one.
func worstImage(images []podImage) (podImage, bool) {
	var worst podImage
	found := false
	for _, im := range images {
		if im.report == nil {
			continue
		}
		if !found || reportRank(*im.report) > reportRank(*worst.report) {
			worst, found = im, true
		}
	}
	return worst, found
}

// reportRank is a report's grade as a sort key (model.GradeRank).
func reportRank(r model.VulnerabilityReport) int64 {
	return model.GradeRank(r.Grade(), r.Counts, r.EOSL)
}

// sections: none of its own; the grades go into the pod's container sections (amend).
func (t *trivySource) sections(*model.KindProfile, *unstructured.Unstructured, model.TimeFormat) []model.DetailSection {
	return nil
}

// amend gives each container section of a pod's drawer its image's grade (gradeSections).
func (t *trivySource) amend(p *model.KindProfile, obj *unstructured.Unstructured, sections []model.DetailSection, f model.TimeFormat) []model.DetailSection {
	if !isPodsKind(p) || obj == nil || !t.synced() {
		return sections
	}
	idx, _ := t.index()
	return f.WriteSections(gradeSections(idx.podImages(obj), sections, t.s.displayNow()))
}

// trivyWorkloadsOf returns the workloads whose reports hold a pod's containers, in the order to
// try them (the file's comment).
func trivyWorkloadsOf(pod *unstructured.Unstructured) []trivyWorkload {
	ns := pod.GetNamespace()
	own := []trivyWorkload{{ns, trivyKindPod, pod.GetName()}}
	for _, o := range pod.GetOwnerReferences() {
		if o.Controller == nil || !*o.Controller {
			continue
		}
		group, _, grouped := strings.Cut(o.APIVersion, "/")
		if !grouped {
			group = "" // "v1": the core group
		}
		switch {
		case group == "apps" && (o.Kind == apiKindReplicaSet || o.Kind == apiKindStatefulSet || o.Kind == apiKindDaemonSet),
			group == "" && o.Kind == "ReplicationController":
			return []trivyWorkload{{ns, o.Kind, o.Name}}
		case group == "batch" && o.Kind == trivyKindJob:
			out := []trivyWorkload{{ns, trivyKindJob, o.Name}}
			if cj := cronJobOfJob(o.Name); cj != "" {
				out = append(out, trivyWorkload{ns, trivyKindCronJob, cj})
			}
			return out
		}
		return own // another controller: Trivy Operator scans the pod itself
	}
	return own
}

// cronJobOfJob returns the CronJob a Job's name says made it, "<cronjob>-<scheduled minute>" as
// the CronJob controller names its Jobs; "" for another name.
func cronJobOfJob(job string) string {
	i := strings.LastIndexByte(job, '-')
	if i <= 0 || i == len(job)-1 {
		return ""
	}
	for _, c := range job[i+1:] {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return job[:i]
}

// podImage is one image of a pod: the containers that run it (an init container marked so) and its
// report, nil when it has none (not scanned, or a report of another image not replaced yet).
type podImage struct {
	image      string
	containers []string
	report     *model.VulnerabilityReport
}

// podImages returns a pod's images in the order its init containers and then its containers name
// them, each with its report. Ephemeral containers are left out: Trivy Operator scans workloads'
// pod templates, which have none.
func (idx trivyReportIndex) podImages(pod *unstructured.Unstructured) []podImage {
	var reports map[string]model.VulnerabilityReport
	for _, w := range trivyWorkloadsOf(pod) {
		if reports = idx[w]; len(reports) > 0 {
			break
		}
	}
	var out []podImage
	at := map[string]int{}
	add := func(c map[string]any, init bool) {
		name, image := model.NestedString(c, "name"), model.NestedString(c, "image")
		if name == "" || image == "" {
			return
		}
		i, ok := at[image]
		if !ok {
			i = len(out)
			at[image] = i
			out = append(out, podImage{image: image})
		}
		label := name
		if init {
			label += initSuffix
		}
		out[i].containers = append(out[i].containers, label)
		r, ok := reports[name]
		if !ok || !r.Matches(image) {
			return
		}
		if cur := out[i].report; cur == nil || worseReport(r, *cur) {
			out[i].report = &r // two containers of one image: the worse report, then the newer
		}
	}
	for _, field := range []string{specInitContainers, specContainers} {
		for _, c := range model.NestedSlice(pod.Object, "spec", field) {
			if m, ok := c.(map[string]any); ok {
				add(m, field == specInitContainers)
			}
		}
	}
	return out
}

func worseReport(a, b model.VulnerabilityReport) bool {
	ra, rb := reportRank(a), reportRank(b)
	if ra != rb {
		return ra > rb
	}
	return a.Scanned.After(b.Scanned)
}

// probeTrivy is the Trivy integration's probe: whether the cluster serves the reports' kind (the
// custom kinds), and what the overlay's read of them met. It sends nothing.
func probeTrivy(_ *Session, b *Backend, on bool) (IntegrationState, string, string) {
	if !on {
		return IntegrationOff, "", ""
	}
	p, served := b.Profile(trivyReportsKind)
	if !served {
		set := b.customSet()
		switch {
		case set == nil:
			return IntegrationMissing, "", trivyNotServed
		case set.err != "":
			// Discovery names only the curated kinds (A90): without the definitions Cogpit cannot tell.
			state := IntegrationUnavailable
			if strings.Contains(strings.ToLower(set.err), "forbidden") {
				state = IntegrationForbidden
			}
			return state, "", "Cogpit cannot tell whether Trivy Operator's reports are served: the custom resource definitions cannot be listed (" + set.err + ")"
		case !set.synced:
			return IntegrationChecking, "", ""
		}
		return IntegrationMissing, "", trivyNotServed
	}
	where := p.Group + "/" + p.Version
	t, _ := overlayOf[*trivySource](b)
	if t == nil {
		return IntegrationChecking, where, ""
	}
	err := t.h.Err()
	switch {
	case refusedAll(t.h):
		return IntegrationForbidden, where, "list " + trivyReportsKind
	case t.h.HasSynced():
		n := t.reports()
		if n == 1 {
			return IntegrationFound, where, "1 vulnerability report"
		}
		return IntegrationFound, where, fmt.Sprintf("%d vulnerability reports", n)
	case err != nil:
		return IntegrationUnavailable, where, errText(err)
	case t.plan.pending:
		return IntegrationChecking, where, "waiting for the access reviews"
	}
	return IntegrationChecking, where, ""
}

// The drawer's grades (D-78, D-new; the owner, 2026-10-22: no section of their own, each in its
// container's section, as a symbol and a detail card): every init container's and container's
// section of a pod gets its image's grade card (model.GradeCard): the grade, its text and rule, the
// counts, the report's OS and its end of life, when it was scanned, and the report as a link to its
// object (the reports' custom kind view). An image without a report is N/A, also when none of the
// pod's images has one (the owner's N/A everywhere). Ephemeral containers get none: no workload's
// template has them, so no report does. Nothing while the integration is off, the cluster does not
// serve the reports, or they are still being listed. The grades come through the overlay
// (trivySource.amend), into the kind's own sections.

// PodImageGrades returns a pod's drawer sections with each container's image grade from Trivy
// Operator's report objects, as the overlay builds them from its index, with absolute times in ISO
// 8601 in now's zone (as BuildDetail writes them): the drawer's content without the cache, for tests
// of the UI.
func PodImageGrades(pod *unstructured.Unstructured, reports []*unstructured.Unstructured, sections []model.DetailSection, now time.Time) []model.DetailSection {
	idx, _ := newTrivyReportIndex(reports)
	return model.TimeLayout{}.In(now.Location()).WriteSections(gradeSections(idx.podImages(pod), sections, now))
}

// gradeSections returns sections with a grade card on each init container's and container's
// section; sections it changes are copied, the input is not modified. Absolute times are marks
// (model.MarkTime).
func gradeSections(images []podImage, sections []model.DetailSection, now time.Time) []model.DetailSection {
	byContainer := map[string]podImage{}
	for _, im := range images {
		for _, c := range im.containers {
			byContainer[strings.TrimSuffix(c, initSuffix)] = im
		}
	}
	var out []model.DetailSection
	for i, sec := range sections {
		if sec.Group != model.GroupContainers && sec.Group != model.GroupInitContainers {
			continue
		}
		im, ok := byContainer[sec.Title]
		if !ok {
			continue
		}
		if out == nil {
			out = append([]model.DetailSection(nil), sections...)
		}
		out[i].Grade = gradeCard(im.report, now)
	}
	if out == nil {
		return sections
	}
	return out
}

// gradeCard is one image's grade card; N/A for an image without a report.
func gradeCard(r *model.VulnerabilityReport, now time.Time) *model.GradeCard {
	if r == nil {
		var none model.VulnerabilityCounts
		return &model.GradeCard{
			Grade: model.GradeNA, Text: model.GradeText(model.GradeNA, none, false),
			Reason: model.GradeReason(model.GradeNA, none, false),
		}
	}
	g, counts := r.Grade(), r.Counts
	card := &model.GradeCard{
		Grade: g, Text: model.GradeText(g, counts, r.EOSL), Reason: model.GradeReason(g, counts, r.EOSL),
		Counts: &counts, OS: r.OS(), EOSL: r.EOSL,
		Report: &model.ObjectRef{Kind: trivyReportsKind, Namespace: r.Namespace, Name: r.Name},
	}
	if !r.Scanned.IsZero() {
		card.Scanned = model.FormatAge(now.Sub(r.Scanned)) + " ago (" + model.MarkTime(r.Scanned) + ")"
	}
	return card
}
