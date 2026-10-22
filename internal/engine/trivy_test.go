package engine

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Trivy integration's fixtures are shaped like what Trivy Operator 0.35.0 wrote on kind
// (2026-10-17, hack/kind/trivy): synthetic names, the vulnerability lists left out.

var vulnReportsGVR = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}

func vulnReportsCRD() *unstructured.Unstructured {
	return testkit.CRD("aquasecurity.github.io", "vulnerabilityreports", "VulnerabilityReport", true,
		testkit.CRDVersion{Name: "v1alpha1", Served: true, Storage: true, Columns: []map[string]any{
			{"name": "Repository", "type": "string", "jsonPath": ".report.artifact.repository"},
			{"name": "Tag", "type": "string", "jsonPath": ".report.artifact.tag"},
			{"name": "Critical", "type": "integer", "jsonPath": ".report.summary.criticalCount", "priority": int64(1)},
		}})
}

// vulnReport is the report of one workload's container: the image as repository and tag, the
// counts critical, high, medium, low, unknown.
func vulnReport(namespace, kind, workload, container, repo, tag string, counts ...int64) *unstructured.Unstructured {
	counts = append(counts, 0, 0, 0, 0, 0)
	name := strings.ToLower(kind) + "-" + workload + "-" + container
	group := map[string]string{"ReplicaSet": "apps/v1", "StatefulSet": "apps/v1", "Job": "batch/v1", "CronJob": "batch/v1", "Pod": "v1"}[kind]
	return testkit.Object("aquasecurity.github.io/v1alpha1", "VulnerabilityReport", namespace, name, map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "trivy-operator", "resource-spec-hash": "85f6f8bfb6",
				model.TrivyLabelContainerName: container, model.TrivyLabelResourceKind: kind,
				model.TrivyLabelResourceName: workload, model.TrivyLabelResourceNamespace: namespace,
			},
			"ownerReferences": []any{map[string]any{"apiVersion": group, "kind": kind, "name": workload, "uid": "u", "controller": true}},
		},
		"report": map[string]any{
			"artifact": map[string]any{"repository": repo, "tag": tag, "digest": "sha256:0272"},
			"registry": map[string]any{"server": "index.docker.io"},
			"scanner":  map[string]any{"name": "Trivy", "vendor": "Aqua Security", "version": "0.75.0"},
			"summary": map[string]any{
				"criticalCount": counts[0], "highCount": counts[1], "mediumCount": counts[2], "lowCount": counts[3],
				"unknownCount": counts[4], "noneCount": int64(0),
			},
			"updateTimestamp": "2026-10-17T09:55:22Z",
			"vulnerabilities": []any{},
		},
	})
}

// trivyPod is a pod of containers "name=image" ("init:" marks an init container), controlled by
// the owner (apiVersion, kind, name) unless that is empty.
func trivyPod(namespace, name string, owner []string, containers ...string) *unstructured.Unstructured {
	var cs, inits []any
	for _, c := range containers {
		init := strings.HasPrefix(c, "init:")
		n, image, _ := strings.Cut(strings.TrimPrefix(c, "init:"), "=")
		entry := map[string]any{"name": n, "image": image}
		if init {
			inits = append(inits, entry)
		} else {
			cs = append(cs, entry)
		}
	}
	spec := map[string]any{"containers": cs}
	if len(inits) > 0 {
		spec["initContainers"] = inits
	}
	pod := testkit.Object("v1", "Pod", namespace, name, map[string]any{"spec": spec, "status": map[string]any{"phase": "Running"}})
	if len(owner) == 3 {
		pod = owned(pod, owner[0], owner[1], owner[2], "uid-"+owner[2])
	}
	return pod
}

// trivySource is a cluster running Trivy Operator: a Deployment's pod of two images and an init
// container, a StatefulSet's pod not scanned yet, a CronJob's Job's pod, a Job's pod and a bare pod.
func trivyFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(crdsGVR, vulnReportsCRD())
	for _, p := range []*unstructured.Unstructured{
		trivyPod("shop", "web-5d9c-a", []string{"apps/v1", "ReplicaSet", "web-5d9c"},
			"init:init=busybox:1.37.0", "app=nginx:1.16.1", "side=busybox:1.37.0"),
		trivyPod("shop", "db-0", []string{"apps/v1", "StatefulSet", "db"}, "db=postgres:16"),
		trivyPod("shop", "nightly-29000000-x", []string{"batch/v1", "Job", "nightly-29000000"}, "report=alpine:3.12"),
		trivyPod("shop", "once-y", []string{"batch/v1", "Job", "once"}, "task=busybox:1.37.0"),
		trivyPod("team", "bare", nil, "shell=docker.io/library/alpine:3.12"),
	} {
		src.Set(testkit.PodsGVR, p)
	}
	for _, r := range []*unstructured.Unstructured{
		vulnReport("shop", "ReplicaSet", "web-5d9c", "init", "library/busybox", "1.37.0"),
		vulnReport("shop", "ReplicaSet", "web-5d9c", "app", "library/nginx", "1.16.1", 39, 123, 147, 23, 31),
		vulnReport("shop", "ReplicaSet", "web-5d9c", "side", "library/busybox", "1.37.0"),
		vulnReport("shop", "CronJob", "nightly", "report", "library/alpine", "3.12", 1),
		vulnReport("shop", "Job", "once", "task", "library/busybox", "1.37.0"),
		vulnReport("team", "Pod", "bare", "shell", "library/alpine", "3.12", 1),
	} {
		src.Set(vulnReportsGVR, r)
	}
	return src
}

// trivyOn returns the backend's Trivy overlay, nil while there is none.
func trivyOn(b *Backend) *trivySource {
	t, _ := overlayOf[*trivySource](b)
	return t
}

// gradedPods is the test pods' profile with a section per init container and container, grouped
// and titled as the real profile's (kinds/pods.go): the sections the Trivy integration grades.
var gradedPods = func() *model.KindProfile {
	p := *testPods
	p.Detail = func(obj *unstructured.Unstructured) []model.DetailSection {
		out := testPods.Detail(obj)
		for _, g := range [][2]string{{specInitContainers, model.GroupInitContainers}, {specContainers, model.GroupContainers}} {
			for _, c := range model.NestedSlice(obj.Object, "spec", g[0]) {
				out = append(out, model.DetailSection{Title: model.NestedString(c, "name"), Group: g[1]})
			}
		}
		return out
	}
	return &p
}()

// gradeCards returns a drawer model's image grades by the container sections that carry them, in
// the sections' order ("init", "app").
func gradeCards(m *DetailModel) (names []string, cards map[string]*model.GradeCard) {
	cards = map[string]*model.GradeCard{}
	for _, sec := range m.Sections {
		if sec.Grade != nil {
			names = append(names, sec.Title)
			cards[sec.Title] = sec.Grade
		}
	}
	return names, cards
}

func trivyStatus(s *Session) IntegrationStatus {
	for _, st := range s.IntegrationStatuses("test") {
		if st.Name == IntegrationTrivy {
			return st
		}
	}
	return IntegrationStatus{}
}

func setTrivy(s *Session, on bool) {
	s.UpdateUI(func(u *UIState) {
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]ClusterSettings{}
		}
		cs := u.ClusterSettings["test"]
		if on {
			cs.SetIntegrations(0, IntTrivy, 0)
		} else {
			cs.SetIntegrations(0, 0, IntTrivy)
		}
		u.ClusterSettings["test"] = cs
	})
}

func detailOf(t *testing.T, s *Session, ref model.ObjectRef) *DetailModel {
	t.Helper()
	m, err := s.Detail(ref)
	if err != nil {
		t.Fatalf("detail %s: %v", ref, err)
	}
	return m
}

// The pod drawer's image grades (D-new): each container's section (its init containers' too) carries
// its image's grade card: the grade, its text and rule, the counts, when it was scanned and the
// report's link; the reports of a Deployment's pod are its ReplicaSet's, of a CronJob's Job's pod the
// CronJob's, of a bare pod its own; a pod without any report is N/A in each container. A changed
// report reaches the open drawer.
func TestTrivyImageGrades(t *testing.T) {
	src := trivyFixture()
	s := newCRDSession(t, src, gradedPods)
	rec := record(s)
	b := s.Backend()
	waitFor(t, "the reports listed", func() bool { return trivyOn(b).synced() })

	st := trivyStatus(s)
	if !st.On || st.State != IntegrationFound || st.Where != "aquasecurity.github.io/v1alpha1" || st.Detail != "6 vulnerability reports" {
		t.Errorf("status %+v", st)
	}
	// The source announces the settled detection from its own loop, after the reports are listed:
	// wait for the announcement, not for the listing that comes before it (CI, 2026-10-21).
	waitFor(t, "the settled detection announced", func() bool { return rec.count(EventIntegrations, "test") > 0 })

	web := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-5d9c-a"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &web })
	m := detailOf(t, s, web)
	names, cards := gradeCards(m)
	if !slices.Equal(names, []string{"init", "app", "side"}) {
		t.Fatalf("containers with a grade %v", names)
	}
	for _, sec := range m.Sections {
		if sec.Group == "Image Ratings" {
			t.Errorf("a section of its own: %+v", sec)
		}
	}
	for _, name := range []string{"init", "side"} {
		if c := cards[name]; c.Grade != model.GradeS || c.Text != "S · no known vulnerabilities" || c.Counts == nil || c.Counts.Total() != 0 {
			t.Errorf("%s %+v", name, c)
		}
	}
	app := cards["app"]
	if app.Grade != model.GradeE || app.Text != "E · 39 Critical" || app.Reason != "More than 5 Critical" ||
		*app.Counts != (model.VulnerabilityCounts{Critical: 39, High: 123, Medium: 147, Low: 23, Unknown: 31}) {
		t.Errorf("nginx %+v", app)
	}
	if !strings.HasSuffix(app.Scanned, ")") || strings.ContainsRune(app.Scanned, '￹') {
		t.Errorf("scanned %q: a time, written in the user's format", app.Scanned)
	}
	if app.Report == nil || *app.Report != (model.ObjectRef{Kind: trivyReportsKind, Namespace: "shop", Name: "replicaset-web-5d9c-app"}) {
		t.Errorf("report %+v", app.Report)
	}

	// A changed report reaches the open drawer.
	before := rec.count(EventDetail, "pods")
	src.Set(vulnReportsGVR, vulnReport("shop", "ReplicaSet", "web-5d9c", "app", "library/nginx", "1.16.1", 0, 0, 2))
	waitFor(t, "the new grade in the drawer", func() bool {
		_, cards := gradeCards(detailOf(t, s, web))
		return cards["app"] != nil && cards["app"].Text == "B · 2 Medium" && cards["app"].Grade == model.GradeB
	})
	waitFor(t, "an EventDetail for the changed report", func() bool { return rec.count(EventDetail, "pods") > before })

	for ref, want := range map[model.ObjectRef][2]string{
		{Kind: "pods", Namespace: "shop", Name: "nightly-29000000-x"}: {"report", "D · 1 Critical"},             // the CronJob's
		{Kind: "pods", Namespace: "shop", Name: "once-y"}:             {"task", "S · no known vulnerabilities"}, // the Job's
		{Kind: "pods", Namespace: "team", Name: "bare"}:               {"shell", "D · 1 Critical"},              // its own
		{Kind: "pods", Namespace: "shop", Name: "db-0"}:               {"db", "N/A · not scanned"},              // none: N/A
	} {
		names, cards := gradeCards(detailOf(t, s, ref))
		if len(names) != 1 || names[0] != want[0] || cards[want[0]].Text != want[1] {
			t.Errorf("%s: %v %+v", ref, names, cards[want[0]])
		}
	}
	if _, cards := gradeCards(detailOf(t, s, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})); cards["db"].Grade != model.GradeNA || cards["db"].Counts != nil {
		t.Errorf("N/A %+v", cards["db"])
	}

	// Switched off: no read of the reports, no grade, the state says so; on again, they come back.
	setTrivy(s, false)
	waitFor(t, "the index stopped", func() bool { return trivyOn(b) == nil })
	if names, _ := gradeCards(detailOf(t, s, web)); len(names) != 0 {
		t.Errorf("off, the drawer shows grades on %v", names)
	}
	if st := trivyStatus(s); st.On || st.State != IntegrationOff {
		t.Errorf("off: %+v", st)
	}
	for _, is := range b.Cache.Stats() {
		if is.Scope.GVR == vulnReportsGVR && is.Handles > 0 {
			t.Errorf("off, the reports are still held: %+v", is)
		}
	}
	setTrivy(s, true)
	waitFor(t, "the grades back", func() bool { names, _ := gradeCards(detailOf(t, s, web)); return len(names) == 3 })
}

// A cluster without Trivy Operator: the integration says the reports are not served, and the
// drawer has no grade; one whose reports the user may not list says which permission is missing.
func TestTrivyNotServedOrForbidden(t *testing.T) {
	src := trivyFixture()
	src.Delete(crdsGVR, "", "vulnerabilityreports.aquasecurity.github.io")
	s := newCRDSession(t, src, gradedPods)
	b := s.Backend()
	waitFor(t, "the definitions listed", func() bool { set := b.customSet(); return set != nil && set.synced })
	waitFor(t, "the state", func() bool { return trivyStatus(s).State == IntegrationMissing })
	if st := trivyStatus(s); st.Detail != trivyNotServed {
		t.Errorf("not served: %+v", st)
	}
	if trivyOn(b) != nil {
		t.Error("an index without the reports' kind")
	}
	if names, _ := gradeCards(detailOf(t, s, model.ObjectRef{Kind: "pods", Namespace: "team", Name: "bare"})); len(names) != 0 {
		t.Errorf("not served: grades on %v", names)
	}

	refused := testkit.RefusingSource{Inner: trivyFixture(), Refuse: map[schema.GroupVersionResource]error{
		vulnReportsGVR: apierrors.NewForbidden(schema.GroupResource{Group: vulnReportsGVR.Group, Resource: vulnReportsGVR.Resource}, "", errors.New("no")),
	}}
	s2 := newCRDSession(t, refused)
	waitFor(t, "forbidden", func() bool { return trivyStatus(s2).State == IntegrationForbidden })
	if st := trivyStatus(s2); st.Detail != "list "+trivyReportsKind {
		t.Errorf("forbidden: %+v", st)
	}
}

// A user who may list the reports in team-a alone: the index reads them there, after the refusal
// of the read in all namespaces, and the pod there has its grade.
func TestTrivyNamespaceLimited(t *testing.T) {
	mem := trivyFixture()
	mem.Set(testkit.PodsGVR, trivyPod("team-a", "bare", nil, "shell=alpine:3.12"))
	mem.Set(vulnReportsGVR, vulnReport("team-a", "Pod", "bare", "shell", "library/alpine", "3.12", 0, 4))
	src := &testkit.NamespacedOnlySource{Inner: mem, Denied: []string{"team-b"}, GVRs: []schema.GroupVersionResource{testkit.PodsGVR, vulnReportsGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {
		rule("get,list,watch", "", "pods"), rule("get,list,watch", "aquasecurity.github.io", "vulnerabilityreports"),
	}})
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: withProfiles(testProfiles(), gradedPods), namespace: "team-a", custom: true},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	waitFor(t, "the reports read in team-a", func() bool {
		ix := trivyOn(b)
		return ix.synced() && slices.Equal(ix.h.Namespaces(), []string{"team-a"})
	})
	ref := model.ObjectRef{Kind: "pods", Namespace: "team-a", Name: "bare"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	waitFor(t, "the grade", func() bool {
		_, cards := gradeCards(detailOf(t, s, ref))
		return cards["shell"] != nil && cards["shell"].Text == "C · 4 High"
	})
	if st := trivyStatus(s); st.State != IntegrationFound || st.Detail != "1 vulnerability report" {
		t.Errorf("status %+v", st)
	}
	waitFor(t, "no read of the reports in all namespaces", func() bool {
		for _, is := range b.Cache.Stats() {
			if is.Scope.GVR == vulnReportsGVR && is.Scope.Namespace == "" && is.Handles > 0 {
				return false
			}
		}
		return true
	})
}

func TestTrivyWorkloadsOf(t *testing.T) {
	for _, c := range []struct {
		owner []string
		want  []trivyWorkload
	}{
		{nil, []trivyWorkload{{"ns", "Pod", "p"}}},
		{[]string{"apps/v1", "ReplicaSet", "web-1"}, []trivyWorkload{{"ns", "ReplicaSet", "web-1"}}},
		{[]string{"apps/v1", "DaemonSet", "agent"}, []trivyWorkload{{"ns", "DaemonSet", "agent"}}},
		{[]string{"v1", "ReplicationController", "rc"}, []trivyWorkload{{"ns", "ReplicationController", "rc"}}},
		{[]string{"batch/v1", "Job", "backup-29360520"}, []trivyWorkload{{"ns", "Job", "backup-29360520"}, {"ns", "CronJob", "backup"}}},
		{[]string{"batch/v1", "Job", "migrate"}, []trivyWorkload{{"ns", "Job", "migrate"}}},
		{[]string{"postgresql.cnpg.io/v1", "Cluster", "pg"}, []trivyWorkload{{"ns", "Pod", "p"}}},
		{[]string{"example.com/v1", "ReplicaSet", "x"}, []trivyWorkload{{"ns", "Pod", "p"}}},
	} {
		got := trivyWorkloadsOf(trivyPod("ns", "p", c.owner, "a=b"))
		if !slices.Equal(got, c.want) {
			t.Errorf("%v: %v", c.owner, got)
		}
	}
	for job, want := range map[string]string{"a-1": "a", "a-b-29360520": "a-b", "a-": "", "-1": "", "a-1x": "", "a": ""} {
		if got := cronJobOfJob(job); got != want {
			t.Errorf("%s: %q", job, got)
		}
	}
	// A report of another image than the container's (the workload changed, not scanned again) is
	// not the container's grade: N/A. Sections of other groups and the input stay as they were.
	images := []podImage{{image: "nginx:1.17", containers: []string{"app"}}, {image: "busybox", containers: []string{"prep (init)"}}}
	in := []model.DetailSection{{Title: "Properties"}, {Title: "prep", Group: model.GroupInitContainers}, {Title: "app", Group: model.GroupContainers},
		{Title: "debug", Group: model.GroupEphemeralContainers}}
	out := gradeSections(images, in, time.Now())
	if out[0].Grade != nil || out[3].Grade != nil || out[1].Grade == nil || out[1].Grade.Grade != model.GradeNA || out[2].Grade.Grade != model.GradeNA {
		t.Errorf("no report: %+v", out)
	}
	if in[2].Grade != nil {
		t.Error("the input was changed")
	}
}
