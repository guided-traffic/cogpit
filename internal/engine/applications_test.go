package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var (
	appsStatefulSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	appsJobsGVR         = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	appsCronJobsGVR     = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
)

// appLabels are the recommended labels of a workload of an application instance.
func appLabels(instance, version string) map[string]any {
	return map[string]any{"labels": map[string]any{
		kinds.LabelAppInstance: instance, kinds.LabelAppName: "storefront", kinds.LabelAppVersion: version,
		kinds.LabelAppManagedBy: "Helm",
	}}
}

func appDeployment(ns, name, instance, version string, ready int64) *unstructured.Unstructured {
	return testkit.Object("apps/v1", "Deployment", ns, name, map[string]any{
		"metadata": appLabels(instance, version),
		"spec":     map[string]any{"replicas": int64(1)},
		"status": map[string]any{
			"replicas": int64(1), "updatedReplicas": int64(1), "readyReplicas": ready, "availableReplicas": ready,
			"conditions": []any{
				map[string]any{"type": "Available", "status": "True", "reason": "MinimumReplicasAvailable"},
				map[string]any{"type": "Progressing", "status": "True", "reason": "NewReplicaSetAvailable"},
			},
		},
	})
}

func appStatefulSet(ready int64) *unstructured.Unstructured {
	return testkit.Object("apps/v1", "StatefulSet", "shop", "db", map[string]any{
		"metadata": appLabels("storefront", "1.5"),
		"spec":     map[string]any{"replicas": int64(2)},
		"status":   map[string]any{"replicas": int64(2), "readyReplicas": ready, "currentReplicas": int64(2), "updatedReplicas": int64(2)},
	})
}

// appsSession connects to a source with the four workload kinds Cogpit has profiles for and the
// Applications view the connector adds to them.
func appsSession(t *testing.T, src *testkit.MemorySource) (*Session, *Backend) {
	t.Helper()
	var profiles []*model.KindProfile
	for _, key := range []string{"pods", "deployments", "statefulsets", "jobs", "cronjobs"} {
		p, _ := kinds.ByKey(key)
		profiles = append(profiles, p)
	}
	fc := &fakeConnector{src: src, profiles: withApplications(profiles)}
	s := New(Options{Connector: fc, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s, s.Backend()
}

// TestApplicationsView groups the cached workloads by namespace and app.kubernetes.io/instance:
// a row per application with its managers and versions, its worst workload's status and the
// workloads in its drawer as links; a Job a Cron Job controls and a workload without the label
// belong to none. The workloads are watched only once the view is open, and the rows follow them.
func TestApplicationsView(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.DeploymentsGVR, appDeployment("shop", "web", "storefront", "1.4", 1))
	src.Set(testkit.DeploymentsGVR, appDeployment("payments", "api", "storefront", "2.0", 1))
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "orphan", nil))
	src.Set(appsStatefulSetsGVR, appStatefulSet(0))
	src.Set(appsCronJobsGVR, testkit.Object("batch/v1", "CronJob", "shop", "nightly", map[string]any{
		"metadata": appLabels("storefront", "1.4"), "spec": map[string]any{"schedule": "0 3 * * *"},
	}))
	run := testkit.Object("batch/v1", "Job", "shop", "nightly-29311440", map[string]any{"metadata": appLabels("storefront", "1.4")})
	run.SetOwnerReferences(nil)
	run.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": "batch/v1", "kind": "CronJob", "name": "nightly", "uid": "uid-shop-nightly", "controller": true,
	}}
	src.Set(appsJobsGVR, run)
	src.Set(appsJobsGVR, testkit.Object("batch/v1", "Job", "shop", "seed", map[string]any{
		"metadata": appLabels("storefront", "1.4"),
		"status":   map[string]any{"succeeded": int64(1), "conditions": []any{map[string]any{"type": "Complete", "status": "True"}}},
	}))
	s, b := appsSession(t, src)

	apps, ok := b.Profile(kinds.ApplicationsKey)
	if !ok || apps.Derive == nil || apps.APIObject() {
		t.Fatalf("the Applications profile %+v: want a virtual kind", apps)
	}
	deployments, _ := b.Profile("deployments")
	if b.Cached(apps) || b.Cache.Running(b.Scope(deployments)) {
		t.Fatal("the workloads are watched before anything reads the applications")
	}

	openView(s, kinds.ApplicationsKey)
	snap := waitTable(t, s, kinds.ApplicationsKey, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 2 })
	if !b.Cache.Running(b.Scope(deployments)) || !b.Cached(apps) {
		t.Error("the open view does not hold the workloads' watches")
	}
	col := func(id string) int { return apps.ColumnIndex(id) }
	shop, payments := -1, -1
	for i, r := range snap.Rows {
		switch r.Ref.Namespace + "/" + r.Ref.Name {
		case "shop/storefront":
			shop = i
		case "payments/storefront":
			payments = i
		}
	}
	if shop < 0 || payments < 0 {
		t.Fatalf("rows %v", rowKeys(snap))
	}
	row := snap.Rows[shop]
	stsProfile, _ := b.Profile("statefulsets")
	stsStatus := model.BuildRow(stsProfile, "test", appStatefulSet(0)).Status
	if row.Cells[col("managedBy")].Text != "Helm" || row.Cells[col("version")].Text != "1.4, 1.5" ||
		row.Cells[col("workloads")].Num != 4 || row.Status != stsStatus {
		t.Errorf("shop/storefront: %+v (status %q, want the stateful set's %q)", row.Cells, row.Status, stsStatus)
	}
	if v := row.Cells[col("violations")].Num; (stsStatus == model.StatusWarning) != (v == 1) {
		t.Errorf("violations %d with the stateful set %q", v, stsStatus)
	}
	if r := snap.Rows[payments]; r.Cells[col("version")].Text != "2.0" || r.Status != model.StatusOK || r.Cells[col("status")].Text != "Healthy" {
		t.Errorf("payments/storefront: %+v %q", r.Cells, r.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := s.DetailWait(ctx, model.ObjectRef{Kind: kinds.ApplicationsKey, Namespace: "shop", Name: "storefront"})
	if err != nil {
		t.Fatal(err)
	}
	var workloads *model.DetailTable
	for _, sec := range d.Sections {
		if sec.Title == "Workloads" {
			workloads = sec.Table
		}
	}
	if workloads == nil || len(workloads.Rows) != 4 {
		t.Fatalf("drawer %+v", d.Sections)
	}
	var names []string
	for _, r := range workloads.Rows {
		names = append(names, r[0]+" "+r[1])
	}
	if got := strings.Join(names, ", "); got != "Deployment web, StatefulSet db, CronJob nightly, Job seed" {
		t.Errorf("workloads %s", got)
	}
	if l := workloads.Link(0, 1); l == nil || *l != (model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}) {
		t.Errorf("web's link %+v", l)
	}
	if !d.Plain || !d.NoEvents {
		t.Error("an application's drawer has tabs or events; it is a virtual kind's")
	}

	// The rows follow the workloads: the stateful set ready, a new instance, an instance gone.
	src.Set(appsStatefulSetsGVR, appStatefulSet(2))
	src.Set(testkit.DeploymentsGVR, appDeployment("shop", "search", "finder", "0.9", 1))
	src.Delete(testkit.DeploymentsGVR, "payments", "api")
	waitTable(t, s, kinds.ApplicationsKey, func(t *TableSnapshot) bool {
		if len(t.Rows) != 2 {
			return false
		}
		for _, r := range t.Rows {
			if r.Ref.Name == "storefront" && (r.Status != model.StatusOK || r.Cells[col("violations")].Num != 0) {
				return false
			}
		}
		return t.IndexOf(model.ObjectRef{Kind: kinds.ApplicationsKey, Namespace: "shop", Name: "finder"}) >= 0
	})
}

// TestApplicationsReleaseTheWorkloads: the applications hold handles on the workloads only while
// their own informer runs; once the view closed and its warm period passed, the workloads' handles
// go back (their informers stay warm in the cluster's cache), and opening the view again takes them
// anew.
func TestApplicationsReleaseTheWorkloads(t *testing.T) {
	idle, warm := appsIdle, appsWarmPeriod
	t.Cleanup(func() { appsIdle, appsWarmPeriod = idle, warm })
	appsIdle, appsWarmPeriod = 10*time.Millisecond, 30*time.Millisecond

	src := testkit.NewMemorySource()
	src.Set(testkit.DeploymentsGVR, appDeployment("shop", "web", "storefront", "1.4", 1))
	s, b := appsSession(t, src)
	deployments, _ := b.Profile("deployments")
	handles := func() int {
		for _, st := range b.Cache.Stats() {
			if st.Scope == b.Scope(deployments) {
				return st.Handles
			}
		}
		return 0
	}
	for round := range 2 {
		openView(s, kinds.ApplicationsKey)
		waitTable(t, s, kinds.ApplicationsKey, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
		if handles() == 0 {
			t.Fatalf("round %d: the open view holds no handle on the deployments", round)
		}
		openView(s, "pods")
		waitFor(t, "the deployments' handles to go back", func() bool { return handles() == 0 })
	}
}

// TestApplicationsOfferedWithWorkloads: the connector offers Applications on a cluster that serves
// a workload kind, once, and not on one that serves none (the demo cluster's:
// TestDemoOffersApplicationsOnce).
func TestApplicationsOfferedWithWorkloads(t *testing.T) {
	pods, _ := kinds.ByKey("pods")
	jobs, _ := kinds.ByKey("jobs")
	if got := withApplications([]*model.KindProfile{pods}); len(got) != 1 {
		t.Errorf("without workloads: %d profiles", len(got))
	}
	got := withApplications([]*model.KindProfile{pods, jobs})
	if len(got) != 3 || got[2] != kinds.Applications() {
		t.Errorf("with jobs: %+v", got)
	}
}

// listAccess answers list reviews from a table of resources: allowed, denied, or unknown when
// absent.
type listAccess map[string]bool

func (a listAccess) Allowed(_ string, v Verb) (allowed, known bool) {
	allowed, known = a[v.Resource]
	return allowed, known
}

// TestApplicationsListable: Applications are listable when the user may list a workload kind,
// hidden when the reviews deny every one, and shown while one is not known yet.
func TestApplicationsListable(t *testing.T) {
	src := testkit.NewMemorySource()
	_, b := appsSession(t, src)
	apps, _ := b.Profile(kinds.ApplicationsKey)
	if l, known := b.Listable(apps, nil); !l || !known {
		t.Errorf("without access reviews: listable %v known %v", l, known)
	}
	denied := listAccess{"deployments": false, "statefulsets": false, "jobs": false, "cronjobs": false}
	tests := []struct {
		name          string
		access        listAccess
		listable, kno bool
	}{
		{"one workload kind allowed", listAccess{"deployments": false, "statefulsets": false, "jobs": true, "cronjobs": false}, true, true},
		// Denied everywhere, a namespaced kind may still be listable in some namespace: its own view
		// stays shown (Backend.Listable), and so do the applications.
		{"every workload kind denied everywhere", denied, true, false},
		{"one not known yet", listAccess{"deployments": false, "statefulsets": false, "jobs": false}, true, false},
	}
	for _, tt := range tests {
		d := &Backend{Profiles: b.Profiles, Access: tt.access} // detached: the session's goroutines read the live backend's Access
		if l, known := d.Listable(apps, nil); l != tt.listable || known != tt.kno {
			t.Errorf("%s: listable %v known %v, want %v %v", tt.name, l, known, tt.listable, tt.kno)
		}
	}
}
