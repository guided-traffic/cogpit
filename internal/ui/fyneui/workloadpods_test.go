package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A workload's Pods table in its drawer (D-40): the node drawer's table without its Namespace column
// (the owner's, 2026-10-14: a workload's pods share its namespace) and with the pods' Age after
// Status (the owner's, 2026-10-26), with the pods the workload
// controls, their CPU and Memory and the same sorting, built from the real profiles
// (engine.BuildDetailWith with the fixture's related objects, engine.FillPodTable,
// engine.SortDrawerTables, engine.LimitDrawerTables). PNGs: testdata/deployment_pods_drawer_*.png,
// statefulset_pods_drawer_*.png.

// The Deployment checkout-api mid-rollout: three pods of its new replica set (one not ready), one
// of the old one terminating.
const (
	wpDeployNS, wpDeploy = "checkout", "checkout-api"
	wpNewRS, wpOldRS     = "checkout-api-5c4b8d7f9", "checkout-api-7d9f8c6b5"
	wpSetNS, wpSet       = "orders", "ledger-writer"
)

func workloadPodSpecs() []podSpec {
	day := 24 * time.Hour
	return []podSpec{
		{ns: wpDeployNS, name: wpNewRS + "-bkmqx", ownerKind: "ReplicaSet", ownerName: wpNewRS, node: "worker-a-01", state: podOK, containers: []string{"app", "envoy"}, age: 2 * time.Hour},
		{ns: wpDeployNS, name: wpNewRS + "-clnry", ownerKind: "ReplicaSet", ownerName: wpNewRS, node: "worker-b-01", state: podOK, containers: []string{"app", "envoy"}, age: 2 * time.Hour},
		{ns: wpDeployNS, name: wpNewRS + "-dmpsz", ownerKind: "ReplicaSet", ownerName: wpNewRS, node: "worker-a-02", state: podNotReady, containers: []string{"app", "envoy"}, age: 9 * time.Minute},
		{ns: wpDeployNS, name: wpOldRS + "-fkqtx", ownerKind: "ReplicaSet", ownerName: wpOldRS, node: "worker-c-01", state: podDeleting, containers: []string{"app", "envoy"}, age: 6 * day},
		{ns: wpSetNS, name: wpSet + "-0", ownerKind: "StatefulSet", ownerName: wpSet, node: "worker-b-01", state: podOK, containers: []string{"app"}, age: 12 * day},
		{ns: wpSetNS, name: wpSet + "-1", ownerKind: "StatefulSet", ownerName: wpSet, node: "worker-a-04", state: podCrashLoop, containers: []string{"app"}, age: 12 * day},
		{ns: wpSetNS, name: wpSet + "-2", ownerKind: "StatefulSet", ownerName: wpSet, node: "worker-a-02", state: podOK, containers: []string{"app"}, age: 12 * day},
	}
}

// workloadPodUsage is what the fake Metrics Server measures of the workloads' pods: every pod but
// the terminating one.
func workloadPodUsage() map[string]model.Usage {
	out := map[string]model.Usage{}
	for i, p := range workloadPodSpecs() {
		if p.state != podDeleting {
			out[p.ns+"/"+p.name] = model.Usage{CPUMilli: int64(12 + i*173%800), MemoryBytes: int64(90+i*211%900) << 20}
		}
	}
	return out
}

// fixtureReplicaSet is a replica set of the Deployment, with the UID its pods' owner references
// name ("uid-owner-<name>").
func fixtureReplicaSet(name string, replicas int64, deploy *unstructured.Unstructured) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "ReplicaSet",
		"metadata": map[string]any{"name": name, "namespace": deploy.GetNamespace(), "uid": "uid-owner-" + name, "creationTimestamp": ts(6 * 24 * time.Hour),
			"ownerReferences": []any{map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": deploy.GetName(), "uid": string(deploy.GetUID()), "controller": true}}},
		"spec":   map[string]any{"replicas": replicas},
		"status": map[string]any{"replicas": replicas, "readyReplicas": replicas},
	}}
}

// fixtureStatefulSet is the stateful set ledger-writer, two of its three pods ready.
func fixtureStatefulSet() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "StatefulSet",
		"metadata": map[string]any{"name": wpSet, "namespace": wpSetNS, "uid": "uid-owner-" + wpSet, "generation": int64(3), "creationTimestamp": ts(12 * 24 * time.Hour)},
		"spec": map[string]any{
			"replicas": int64(3), "serviceName": wpSet, "podManagementPolicy": "OrderedReady",
			"selector":       map[string]any{"matchLabels": map[string]any{"app": wpSet}},
			"updateStrategy": map[string]any{"type": "RollingUpdate"},
			"volumeClaimTemplates": []any{map[string]any{"metadata": map[string]any{"name": "data"},
				"spec": map[string]any{"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "standard", "resources": map[string]any{"requests": map[string]any{"storage": "10Gi"}}}}},
		},
		"status": map[string]any{"observedGeneration": int64(3), "replicas": int64(3), "readyReplicas": int64(2), "availableReplicas": int64(2),
			"updatedReplicas": int64(3), "currentRevision": wpSet + "-6c9d7f8b5", "updateRevision": wpSet + "-6c9d7f8b5"},
	}}
}

// workloadPodsFake is kindsFake over the Deployments with the drawer open on the Deployment (or over
// the Stateful Sets on the stateful set, with set): its model as the engine builds it, the Pods filled from
// workloadPodUsage, sorted as sorts says and cut at engine.MaxDrawerTableRows; ui.sortDrawerTable
// answered as the engine does.
func workloadPodsFake(t testing.TB, set bool, sorts map[string]engine.SortSpec) (*fakeEngine, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "deployments")
	var deploy *unstructured.Unstructured
	for _, d := range fixtureDeploymentObjects() {
		if d.GetName() == wpDeploy {
			deploy = d
		}
	}
	var pods []*unstructured.Unstructured
	for _, s := range workloadPodSpecs() {
		pods = append(pods, fixturePodObject(s))
	}
	rel := fixtureRelated{"pods": pods, "replicasets": {fixtureReplicaSet(wpNewRS, 3, deploy), fixtureReplicaSet(wpOldRS, 0, deploy)}}
	key, obj := "deployments", deploy
	if set {
		key, obj = "statefulsets", fixtureStatefulSet()
		f.tables[key] = kindSnapshot(t, key, []*unstructured.Unstructured{obj})
		f.ui.View = key
	}
	ref := model.ObjectRef{Context: "demo", Kind: key, Namespace: obj.GetNamespace(), Name: obj.GetName()}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	f.ui.DrawerSort = sorts
	p, _ := kinds.ByKey(key)
	podsProfile, _ := kinds.ByKey("pods")
	usage := workloadPodUsage()
	build := func(sorts map[string]engine.SortSpec) *engine.DetailModel {
		m, err := engine.BuildDetailWith(p, ref, obj, nil, fixedNow, rel)
		if err != nil {
			t.Fatal(err)
		}
		for _, sec := range m.Sections {
			if sec.Table != nil && sec.Table.Sortable {
				engine.FillPodTable(sec.Table, func(ns, name string) (model.Usage, bool) {
					u, ok := usage[ns+"/"+name]
					return u, ok
				}, func(u model.Usage) (model.Cell, model.Cell) { return podsProfile.UsageCells(nil, u) })
			}
		}
		engine.SortDrawerTables(m, sorts)
		engine.LimitDrawerTables(m, engine.MaxDrawerTableRows)
		return m
	}
	f.details[ref.String()] = build(sorts)
	f.handlers["ui.sortDrawerTable"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var sp drawerSortParams
		if err := json.Unmarshal(inv.Params, &sp); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.DrawerSort = map[string]engine.SortSpec{sp.Section: {Column: sp.Column, Desc: sp.Desc}}
		})
		f.mu.Lock()
		f.details[ref.String()] = build(f.ui.DrawerSort)
		f.mu.Unlock()
		f.emit(engine.Event{Type: engine.EventDetail, View: ref.Kind})
		return f.UIState(), nil
	}
	return f, ref
}

// A Deployment's drawer ends with its Pods, the node drawer's table without Namespace and with Age:
// the pods of both replica sets during the rollout, their CPU and Memory as the Pods list writes
// them, the node as a link, each pod's age; a click on CPU sorts by it through the engine's state.
func TestDeploymentDrawerPodsTable(t *testing.T) {
	f, _ := workloadPodsFake(t, false, nil)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerHeaders(sh)) > 0 })
	settle(w)

	headers := drawerHeaders(sh)
	for _, title := range []string{"Name", "Node", "Ready", "CPU", "Memory", "Status", "Age"} {
		if headers[title] == nil {
			t.Errorf("no sortable title %s among %v", title, keys(headers))
		}
	}
	if headers["Namespace"] != nil {
		t.Errorf("a Namespace column in a workload's Pods: %v", keys(headers))
	}
	names := podsColumn(t, sh, "Name")
	want := []string{wpNewRS + "-bkmqx", wpNewRS + "-clnry", wpNewRS + "-dmpsz", wpOldRS + "-fkqtx"}
	if !slices.Equal(names, want) {
		t.Errorf("the Deployment's Pods %q, want %q", names, want)
	}
	texts := drawerTexts(sh)
	pods, _ := kinds.ByKey("pods")
	for key, u := range workloadPodUsage() {
		if key[:len(wpDeployNS)] != wpDeployNS {
			continue
		}
		cpu, mem := pods.UsageCells(nil, u)
		if !slices.Contains(texts, cpu.Text) || !slices.Contains(texts, mem.Text) {
			t.Errorf("%s: %s and %s not in the drawer", key, cpu.Text, mem.Text)
		}
	}
	if l := drawerLinks(sh)["worker-b-01"]; l == nil {
		t.Errorf("the node is not a link: %v", keys(drawerLinks(sh)))
	}
	if ages := podsColumn(t, sh, "Age"); !slices.Equal(ages, []string{"2h", "2h", "9m", "6d"}) { // against the engine's clock
		t.Errorf("the pods' ages %q", ages)
	}

	test.Tap(drawerHeaders(sh)["CPU"])
	q.until(t, "ui.sortDrawerTable", func() bool { return len(f.callsOf("ui.sortDrawerTable")) == 1 })
	if p := decodeParams[drawerSortParams](t, f.callsOf("ui.sortDrawerTable")[0]); p.Section != "Pods" || p.Column != model.ColumnCPU || p.Desc {
		t.Fatalf("a click on CPU sent %+v", p)
	}
	q.until(t, "the table sorted by CPU", func() bool { h := drawerHeaders(sh)["CPU"]; return h != nil && h.sort == 1 })
	cpu := map[string]int64{}
	for key, u := range workloadPodUsage() {
		cpu[key[len(wpDeployNS)+1:]] = u.CPUMilli
	}
	sorted := podsColumn(t, sh, "Name")
	if sorted[len(sorted)-1] != wpOldRS+"-fkqtx" { // no measurement: last
		t.Errorf("the pod without a measurement is not last: %q", sorted)
	}
	if !slices.IsSortedFunc(sorted[:3], func(a, b string) int { return int(cpu[a] - cpu[b]) }) {
		t.Errorf("not sorted by CPU: %q", sorted)
	}
}

// A table the engine cut says how many rows it left out, under its rows.
func TestDrawerTableShowsOmittedRows(t *testing.T) {
	f, ref := workloadPodsFake(t, true, nil)
	m := f.details[ref.String()]
	for i := range m.Sections {
		if m.Sections[i].Title == "Pods" {
			tbl := *m.Sections[i].Table
			tbl.Omitted = 1234
			m.Sections[i].Table = &tbl
		}
	}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerHeaders(sh)) > 0 })
	if !slices.Contains(drawerTexts(sh), "1234 more pods not shown") {
		t.Errorf("no count of the omitted rows in %q", drawerTexts(sh))
	}
}

// renderWorkloadPods opens the Deployment's (or the stateful set's) drawer, its Pods sorted by
// Memory, descending, and scrolls it to the table.
func renderWorkloadPods(t *testing.T, set bool, mode ThemeMode, name string) {
	f, _ := workloadPodsFake(t, set, map[string]engine.SortSpec{"Pods": {Column: model.ColumnMemory, Desc: true}})
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(drawerHeaders(sh)) > 0 && sh.diag.total > 0
	})
	settle(w)
	scrollDrawerTo(t, sh, "Pods")
	settle(w)
	savePNG(t, w, name)
}

func TestRenderDeploymentPodsDark(t *testing.T) {
	renderWorkloadPods(t, false, ThemeDark, "deployment_pods_drawer_dark.png")
}
