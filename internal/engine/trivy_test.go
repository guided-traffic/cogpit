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

// imageSections returns a drawer model's image rating sections.
func imageSections(m *DetailModel) []model.DetailSection {
	var out []model.DetailSection
	for _, sec := range m.Sections {
		if sec.Group == groupImageRatings {
			out = append(out, sec)
		}
	}
	return out
}

// item returns a section's item by name.
func item(sec model.DetailSection, name string) (model.DetailItem, bool) {
	i := slices.IndexFunc(sec.Items, func(it model.DetailItem) bool { return it.Name == name })
	if i < 0 {
		return model.DetailItem{}, false
	}
	return sec.Items[i], true
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

// The pod drawer's image ratings: per image of the pod (its init containers' too) the rating with
// its count, the counts, the containers, when it was scanned and the report's link; the reports of
// a Deployment's pod are its ReplicaSet's, of a CronJob's Job's pod the CronJob's, of a bare pod
// its own; a pod without any report has no section. A changed report reaches the open drawer.
func TestTrivyImageRatings(t *testing.T) {
	src := trivyFixture()
	s := newCRDSession(t, src)
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
	secs := imageSections(detailOf(t, s, web))
	if len(secs) != 2 || secs[0].Title != "busybox:1.37.0" || secs[1].Title != "nginx:1.16.1" {
		t.Fatalf("sections %+v", secs)
	}
	clean, bad := secs[0], secs[1]
	if it, _ := item(clean, "Rating"); it.Value != "Clean" || it.Status != model.StatusOK {
		t.Errorf("busybox rating %+v", it)
	}
	if it, _ := item(clean, "Containers"); it.Value != "init (init), side" {
		t.Errorf("busybox containers %+v", it)
	}
	if _, ok := item(clean, "Vulnerabilities"); ok {
		t.Error("a clean image lists vulnerabilities")
	}
	if it, _ := item(bad, "Rating"); it.Value != "Critical · 39" || it.Status != model.StatusError {
		t.Errorf("nginx rating %+v", it)
	}
	if it, _ := item(bad, "Vulnerabilities"); len(it.Chips) != 5 || it.Chips[0].Text != "39 Critical" || it.Chips[1].Status != model.StatusWarning {
		t.Errorf("nginx counts %+v", it)
	}
	if it, _ := item(bad, "Container"); it.Value != "app" {
		t.Errorf("nginx container %+v", it)
	}
	if it, _ := item(bad, "Scanned"); !strings.HasSuffix(it.Value, ")") || strings.ContainsRune(it.Value, '￹') {
		t.Errorf("scanned %+v: a time, written in the user's format", it)
	}
	if it, _ := item(bad, "Report"); it.Link == nil || *it.Link != (model.ObjectRef{Kind: trivyReportsKind, Namespace: "shop", Name: "replicaset-web-5d9c-app"}) {
		t.Errorf("report %+v", it)
	}

	// A changed report reaches the open drawer.
	before := rec.count(EventDetail, "pods")
	src.Set(vulnReportsGVR, vulnReport("shop", "ReplicaSet", "web-5d9c", "app", "library/nginx", "1.16.1", 0, 0, 2))
	waitFor(t, "the new rating in the drawer", func() bool {
		secs := imageSections(detailOf(t, s, web))
		it, _ := item(secs[len(secs)-1], "Rating")
		return it.Value == "Medium · 2" && it.Status == model.StatusPending
	})
	waitFor(t, "an EventDetail for the changed report", func() bool { return rec.count(EventDetail, "pods") > before })

	for ref, want := range map[model.ObjectRef]string{
		{Kind: "pods", Namespace: "shop", Name: "nightly-29000000-x"}: "Critical · 1", // the CronJob's
		{Kind: "pods", Namespace: "shop", Name: "once-y"}:             "Clean",        // the Job's
		{Kind: "pods", Namespace: "team", Name: "bare"}:               "Critical · 1", // its own
	} {
		secs := imageSections(detailOf(t, s, ref))
		if len(secs) != 1 {
			t.Errorf("%s: %+v", ref, secs)
			continue
		}
		if it, _ := item(secs[0], "Rating"); it.Value != want {
			t.Errorf("%s: %+v", ref, it)
		}
	}
	if secs := imageSections(detailOf(t, s, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})); len(secs) != 0 {
		t.Errorf("a pod without reports: %+v", secs)
	}

	// Switched off: no read of the reports, no section, the state says so; on again, they come back.
	setTrivy(s, false)
	waitFor(t, "the index stopped", func() bool { return trivyOn(b) == nil })
	if secs := imageSections(detailOf(t, s, web)); len(secs) != 0 {
		t.Errorf("off, the drawer shows %+v", secs)
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
	waitFor(t, "the ratings back", func() bool { return len(imageSections(detailOf(t, s, web))) == 2 })
}

// A cluster without Trivy Operator: the integration says the reports are not served, and the
// drawer has no section; one whose reports the user may not list says which permission is missing.
func TestTrivyNotServedOrForbidden(t *testing.T) {
	src := trivyFixture()
	src.Delete(crdsGVR, "", "vulnerabilityreports.aquasecurity.github.io")
	s := newCRDSession(t, src)
	b := s.Backend()
	waitFor(t, "the definitions listed", func() bool { set := b.customSet(); return set != nil && set.synced })
	waitFor(t, "the state", func() bool { return trivyStatus(s).State == IntegrationMissing })
	if st := trivyStatus(s); st.Detail != trivyNotServed {
		t.Errorf("not served: %+v", st)
	}
	if trivyOn(b) != nil {
		t.Error("an index without the reports' kind")
	}
	if secs := imageSections(detailOf(t, s, model.ObjectRef{Kind: "pods", Namespace: "team", Name: "bare"})); len(secs) != 0 {
		t.Errorf("not served: %+v", secs)
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
// of the read in all namespaces, and the pod there has its rating.
func TestTrivyNamespaceLimited(t *testing.T) {
	mem := trivyFixture()
	mem.Set(testkit.PodsGVR, trivyPod("team-a", "bare", nil, "shell=alpine:3.12"))
	mem.Set(vulnReportsGVR, vulnReport("team-a", "Pod", "bare", "shell", "library/alpine", "3.12", 0, 4))
	src := &testkit.NamespacedOnlySource{Inner: mem, Denied: []string{"team-b"}, GVRs: []schema.GroupVersionResource{testkit.PodsGVR, vulnReportsGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {
		rule("get,list,watch", "", "pods"), rule("get,list,watch", "aquasecurity.github.io", "vulnerabilityreports"),
	}})
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: testProfiles(), namespace: "team-a", custom: true},
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
	waitFor(t, "the rating", func() bool {
		secs := imageSections(detailOf(t, s, ref))
		if len(secs) != 1 {
			return false
		}
		it, _ := item(secs[0], "Rating")
		return it.Value == "High · 4"
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
	// not the container's rating; none of the pod's images rated: no section.
	images := []podImage{{image: "nginx:1.17", containers: []string{"app"}}}
	if secs := imageRatingSections(images, time.Now()); secs != nil {
		t.Errorf("no report: %+v", secs)
	}
}
