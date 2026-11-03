package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var groupKsGVR = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}

const groupKsKey = "kustomizations.kustomize.toolkit.fluxcd.io"

// inventoried is a Kustomization in flux-system whose inventory lists the given workloads
// ("<namespace>_<name>_<group>_<kind>") and a Service, which no logs tab shows.
func inventoried(ids ...string) *unstructured.Unstructured {
	entries := []any{map[string]any{"id": "shop_web_apps_Service", "v": "v1"}}
	for _, id := range ids {
		entries = append(entries, map[string]any{"id": id, "v": "v1"})
	}
	obj := testkit.Object("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps", map[string]any{"spec": map[string]any{}})
	obj.Object["status"] = map[string]any{"inventory": map[string]any{"entries": entries}}
	return obj
}

// groupLogsSession is logsSession's cluster (a Deployment web with pods web-1-a and web-1-b, a
// StatefulSet db with db-0) with Flux's Kustomizations and one, apps, that applied both.
func groupLogsSession(t *testing.T) (*Session, *testkit.MemorySource) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", nil))
	src.Set(replicaSetsGVR, owned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-1", nil), "apps/v1", "Deployment", "web", "uid-shop-web"))
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "db", nil))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-a", "app", "proxy"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-b", "app", "proxy:waiting"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	src.Set(groupKsGVR, inventoried("shop_web_apps_Deployment", "shop_db_apps_StatefulSet"))
	profiles := testProfiles()
	for _, key := range []string{"replicasets", "statefulsets", "daemonsets", "jobs"} {
		p, _ := kinds.ByKey(key)
		profiles = append(profiles, p)
	}
	for _, p := range kinds.Curated() {
		if p.Key == groupKsKey {
			profiles = append(profiles, p)
		}
	}
	fc := &fakeConnector{src: src, profiles: profiles, conn: &cluster.Conn{}}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s, src
}

var groupRef = model.ObjectRef{Kind: groupKsKey, Namespace: "flux-system", Name: "apps"}

// TestGroupLogsMergeEveryWorkload (A81): a group's tab merges every running container of every
// workload the group names, each line starting with <workload>/<pod>/<container>; a Service of the
// inventory and a waiting container are left out; the history is shared by the workloads.
func TestGroupLogsMergeEveryWorkload(t *testing.T) {
	s, _ := groupLogsSession(t)
	f := newFakeLogs()
	f.content["web-1-a/app"] = "a1\n"
	f.content["web-1-a/proxy"] = "proxy up\n"
	f.content["web-1-b/app"] = "b1\n"
	f.content["db-0/db"] = "ready\n"
	tab, created, err := s.OpenGroupLogsTab(context.Background(), groupRef)
	if err != nil || !created || tab.Title != "Logs: Kustomization apps" || !tab.Logs.ResourceNames || tab.Ref.Kind != groupKsKey {
		t.Fatalf("tab %+v created %v: %v", tab, created, err)
	}
	if again, created, _ := s.OpenGroupLogsTab(context.Background(), groupRef); created || again.ID != tab.ID {
		t.Errorf("a second open made %+v", again)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, "web/web-1-a/app a1", "web/web-1-a/proxy proxy up", "web/web-1-b/app b1", "db/db-0/db ready")
	for _, r := range f.requests() {
		// 1,000 lines shared by the two workloads: db's one container 500, web's three 166 each.
		want := int64(166)
		if r.Pod == "db-0" {
			want = 500
		}
		if r.Namespace != "shop" || !r.Follow || r.TailLines != want {
			t.Errorf("request %+v: want follow in shop with %d lines", r, want)
		}
	}
	f.line(t, "db-0/db", "query 1")
	lr.wait(t, "db/db-0/db query 1")

	src, err := s.LogSources(context.Background(), tab.ID)
	if err != nil || !slices.Equal(src.Workloads, []string{"web", "db"}) || !src.AllPods || !slices.Equal(src.Pods, []string{"web-1-a", "web-1-b", "db-0"}) {
		t.Errorf("sources %+v, %v", src, err)
	}
}

// TestGroupLogsNarrowToAWorkload (A81): narrowing a group's tab to one workload ends the other
// workloads' streams and keeps that one's; a pod of another workload is dropped; a workload the
// group does not name is refused, and a workload's own tab takes none.
func TestGroupLogsNarrowToAWorkload(t *testing.T) {
	s, _ := groupLogsSession(t)
	f := newFakeLogs()
	tab, _, err := s.OpenGroupLogsTab(context.Background(), groupRef)
	if err != nil {
		t.Fatal(err)
	}
	readLines(openTabLogs(t, s, tab.ID, f))
	waitRequests(t, f, 3)
	ctx := context.Background()
	out, err := s.SetGroupLogSource(ctx, tab.ID, strp("db"), nil, nil)
	if err != nil || out.Logs.Workload != "db" || out.Logs.Pod != "" {
		t.Fatalf("narrowed to db: %+v, %v", out.Logs, err)
	}
	waitFor(t, "web's streams to end", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.ended["web-1-a/app"] && f.ended["web-1-a/proxy"] && f.ended["web-1-b/app"]
	})
	f.mu.Lock()
	dbEnded := f.ended["db-0/db"]
	f.mu.Unlock()
	if dbEnded {
		t.Error("db's stream ended with the narrowing to db")
	}
	src, err := s.LogSources(ctx, tab.ID)
	if err != nil || !slices.Equal(src.Pods, []string{"db-0"}) || !slices.Equal(src.Containers, []string{"db"}) || len(src.Workloads) != 2 {
		t.Errorf("sources of db %+v, %v", src, err)
	}
	if _, err := s.SetGroupLogSource(ctx, tab.ID, strp("cache"), nil, nil); err == nil || !strings.Contains(err.Error(), "its workloads: web, db") {
		t.Errorf("an unknown workload: %v", err)
	}
	// A pod of web, then the workload db: the pod is dropped.
	if _, err := s.SetGroupLogSource(ctx, tab.ID, strp(""), strp("web-1-a"), nil); err != nil {
		t.Fatal(err)
	}
	if out, err := s.SetGroupLogSource(ctx, tab.ID, strp("db"), nil, nil); err != nil || out.Logs.Pod != "" || out.Logs.Workload != "db" {
		t.Errorf("web's pod with the workload db: %+v, %v", out.Logs, err)
	}
	wt, _, err := s.OpenWorkloadLogsTab(ctx, model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupLogSource(ctx, wt.ID, strp("web"), nil, nil); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("a workload's tab narrowed to a workload: %v", err)
	}
}

// TestGroupLogsFollowTheGroup (A81): a workload that leaves the group ends with a line that says
// so; one that joins starts streaming.
func TestGroupLogsFollowTheGroup(t *testing.T) {
	s, src := groupLogsSession(t)
	f := newFakeLogs()
	f.content["db-0/db"] = "ready\n"
	tab, _, err := s.OpenGroupLogsTab(context.Background(), groupRef)
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	waitRequests(t, f, 3)
	src.Set(groupKsGVR, inventoried("shop_web_apps_Deployment"))
	lr.wait(t, "[cogpit] StatefulSet db left Kustomization apps; its logs end here")
	waitFor(t, "db's stream to end", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.ended["db-0/db"] })
	src.Set(groupKsGVR, inventoried("shop_web_apps_Deployment", "shop_db_apps_StatefulSet"))
	waitFor(t, "db again", func() bool {
		n := 0
		for _, r := range f.requests() {
			if r.Pod == "db-0" {
				n++
			}
		}
		return n == 2
	})
}

// TestGroupLogsOlderSources (A81): a group's older lines are asked per container of its workloads
// in their namespaces, each named by its workload, pod and container, within the tab's narrowing.
func TestGroupLogsOlderSources(t *testing.T) {
	s, _ := groupLogsSession(t)
	tab, _, err := s.OpenGroupLogsTab(context.Background(), groupRef)
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := s.olderSources(context.Background(), tab)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range srcs {
		got = append(got, o.namespace+" "+o.prefix)
	}
	if want := []string{"shop web/web-1-a/app", "shop web/web-1-a/proxy", "shop web/web-1-b/app", "shop db/db-0/db"}; !slices.Equal(got, want) {
		t.Errorf("sources %q, want %q", got, want)
	}
	tab, _ = s.SetGroupLogSource(context.Background(), tab.ID, strp("db"), nil, nil)
	if srcs, _ := s.olderSources(context.Background(), tab); len(srcs) != 1 || srcs[0].prefix != "db/db-0/db" {
		t.Errorf("narrowed to db: %+v", srcs)
	}
}

// TestGroupLogsPrevious (A81): with the previous containers, every workload's previous instances
// are written and the stream ends.
func TestGroupLogsPrevious(t *testing.T) {
	s, src := groupLogsSession(t)
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db:running#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	waitFor(t, "the restarted db", func() bool {
		obj, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})
		return err == nil && len(model.NestedSlice(obj, "status", "containerStatuses")) == 1 &&
			model.NestedMap(model.NestedSlice(obj, "status", "containerStatuses")[0], "lastState") != nil
	})
	f := newFakeLogs()
	f.content["db-0/db#previous"] = "crashed\n"
	tab, _, err := s.OpenGroupLogsTab(context.Background(), groupRef)
	if err != nil {
		t.Fatal(err)
	}
	if tab, err = s.SetLogOptions(tab.ID, func(o *LogOptions) { o.Previous = true }); err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the stream did not end: %q", lr.all())
	}
	if got := lr.all(); !slices.Contains(got, "db/db-0/db crashed") || !slices.Contains(got, "[cogpit] no container of Deployment web has a previous instance") {
		t.Errorf("lines %q", got)
	}
}

// TestGroupWorkloadsOfEachGroup (A81): what each group names: an Argo CD Application's
// status.resources in the cluster it runs in (none for another cluster), a Kustomization's
// inventory; only the kinds a logs tab shows.
func TestGroupWorkloadsOfEachGroup(t *testing.T) {
	argo := testkit.ArgoApplication("argocd", "shop", "Synced", "Healthy")
	argo.Object["status"].(map[string]any)["resources"] = []any{
		map[string]any{"group": "apps", "kind": "Deployment", "namespace": "shop", "name": "web"},
		map[string]any{"group": "", "kind": "Service", "namespace": "shop", "name": "web"},
		map[string]any{"group": "batch", "kind": "Job", "namespace": "shop", "name": "migrate"},
		map[string]any{"group": "batch", "kind": "CronJob", "namespace": "shop", "name": "backup"},
		map[string]any{"group": "apps", "kind": "Deployment", "namespace": "shop", "name": "web"},
	}
	ref := model.ObjectRef{Context: "c", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: "shop"}
	got := LogGroupWorkloads(ref, argo)
	want := []model.ObjectRef{{Context: "c", Kind: "deployments", Namespace: "shop", Name: "web"}, {Context: "c", Kind: "jobs", Namespace: "shop", Name: "migrate"}}
	if !slices.Equal(got, want) {
		t.Errorf("Argo CD: %+v", got)
	}
	argo.Object["spec"].(map[string]any)["destination"] = map[string]any{"server": "https://prod.example.com", "namespace": "shop"}
	if got := LogGroupWorkloads(ref, argo); len(got) != 0 || kinds.ArgoAppElsewhere(argo) != "https://prod.example.com" {
		t.Errorf("another cluster: %+v", got)
	}
	ks := inventoried("shop_web_apps_Deployment", "shop_db_apps_StatefulSet", "broken", "shop_x_apps_Unknown")
	if got := LogGroupWorkloads(groupRef, ks); len(got) != 2 || got[1].Kind != "statefulsets" || got[1].Name != "db" {
		t.Errorf("Kustomization: %+v", got)
	}
	if IsLogGroup("deployments") || !IsLogGroup(kinds.ArgoApplicationsKey) || LogGroupWorkloads(model.ObjectRef{Kind: "pods"}, ks) != nil {
		t.Error("IsLogGroup")
	}
}
