package kinds_test

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A workload's Pods table in its drawer (D-40): the pods its controlling owner references name,
// by UID and in the workload's namespace; a Deployment's through its replica sets, a Cron Job's
// through its jobs. The lists do not read them (DetailNeeds, not Needs).

// controlled returns obj as controlled by owner: an owner reference with the owner's kind, name and
// UID, marked as the controller.
func controlled(obj, owner *unstructured.Unstructured) *unstructured.Unstructured {
	obj.SetOwnerReferences(nil)
	_ = unstructured.SetNestedSlice(obj.Object, []any{map[string]any{
		"apiVersion": owner.GetAPIVersion(), "kind": owner.GetKind(), "name": owner.GetName(),
		"uid": string(owner.GetUID()), "controller": true,
	}}, "metadata", "ownerReferences")
	return obj
}

// named returns a copy of obj in namespace ns, named name, with the UID "uid-<ns>-<name>".
func named(obj *unstructured.Unstructured, ns, name string) *unstructured.Unstructured {
	o := obj.DeepCopy()
	o.SetNamespace(ns)
	o.SetName(name)
	o.SetUID(types.UID("uid-" + ns + "-" + name))
	return o
}

// podsTableNames returns the names in the sections' Pods table, nil without one.
func podsTableNames(sections []model.DetailSection) []string {
	for _, s := range sections {
		if s.Title == "Pods" && s.Table != nil {
			names := make([]string, 0, len(s.Table.Rows))
			for _, r := range s.Table.Rows {
				names = append(names, r[0])
			}
			return names
		}
	}
	return nil
}

func TestControllerKeys(t *testing.T) {
	owner := named(recorded(t, "replicasets", "healthy"), "shop", "web-abc")
	pod := controlled(named(recorded(t, "pods", "healthy"), "shop", "web-abc-1"), owner)
	if got := kinds.PodsByOwner.Keys(pod); !slices.Equal(got, []string{kinds.OwnerKey("shop", "uid-shop-web-abc")}) {
		t.Errorf("a controlled pod's keys %q", got)
	}

	refs := func(refs ...any) *unstructured.Unstructured {
		p := named(recorded(t, "pods", "healthy"), "shop", "p")
		_ = unstructured.SetNestedSlice(p.Object, refs, "metadata", "ownerReferences")
		return p
	}
	for name, c := range map[string]struct {
		pod  *unstructured.Unstructured
		want []string
	}{
		"no owner":                   {refs(), nil},
		"an owner, not a controller": {refs(map[string]any{"kind": "ReplicaSet", "name": "a", "uid": "u-a"}), nil},
		"the controller second": {refs(map[string]any{"kind": "Node", "name": "n", "uid": "u-n"},
			map[string]any{"kind": "ReplicaSet", "name": "b", "uid": "u-b", "controller": true}), []string{"shop/u-b"}},
		"a controller without a UID": {refs(map[string]any{"kind": "ReplicaSet", "name": "c", "controller": true}), nil},
		"a controller flag as text":  {refs(map[string]any{"kind": "ReplicaSet", "name": "d", "uid": "u-d", "controller": "true"}), nil},
		"a reference that is a list": {refs([]any{"x"}, map[string]any{"uid": "u-e", "controller": true}), []string{"shop/u-e"}},
	} {
		if got := kinds.PodsByOwner.Keys(c.pod); !slices.Equal(got, c.want) {
			t.Errorf("%s: keys %q, want %q", name, got, c.want)
		}
	}
}

// The pods a Replica Set, Stateful Set, Daemon Set or Job controls itself: by UID, not by the
// owner's name (a workload created again under its name owns none of the old one's pods), only in
// its namespace, only through the controller reference. The table is the node drawer's without its
// Namespace column (D-40, owner 2026-10-14): the workload's pods share its namespace.
func TestWorkloadDrawerPods(t *testing.T) {
	for _, key := range []string{"replicasets", "statefulsets", "daemonsets", "jobs"} {
		t.Run(key, func(t *testing.T) {
			p, _ := kinds.ByKey(key)
			owner := named(recorded(t, key, "healthy"), "shop", "web")
			older := named(owner, "shop", "web")
			older.SetUID(types.UID("uid-before-it-was-created-again"))
			elsewhere := named(owner, "other", "web")
			pod := func(ns, name string) *unstructured.Unstructured {
				return named(recorded(t, "pods", "healthy"), ns, name)
			}
			notController := pod("shop", "web-adopted")
			_ = unstructured.SetNestedSlice(notController.Object, []any{map[string]any{
				"kind": owner.GetKind(), "name": "web", "uid": string(owner.GetUID()),
			}}, "metadata", "ownerReferences")
			crossNamespace := controlled(pod("other", "web-x"), owner) // its owner reference names shop's UID
			rel := fakeRelated{"pods": {
				controlled(pod("shop", "web-2"), owner),
				controlled(pod("shop", "web-1"), owner),
				controlled(pod("shop", "web-old"), older),
				controlled(pod("other", "web-0"), elsewhere),
				notController, crossNamespace,
			}}
			sections := p.DetailSections(owner, rel)
			if got := podsTableNames(sections); !slices.Equal(got, []string{"web-1", "web-2"}) {
				t.Errorf("the drawer's Pods %q, want web-1 and web-2", got)
			}
			tbl := findTable(t, sections, "Pods")
			if !tbl.Sortable || !slices.Equal(tbl.IDs, []string{"name", "warning", "node", "ready", model.ColumnCPU, model.ColumnMemory, "status", "age"}) {
				t.Errorf("columns %q, ids %q: want the node drawer's without Namespace, with Age", tbl.Columns, tbl.IDs)
			}
			if len(tbl.Cells[0]) != len(tbl.IDs) {
				t.Errorf("a row of %d cells for %d columns", len(tbl.Cells[0]), len(tbl.IDs))
			}
			row := tbl.Cells[0]
			if l := row[0].Link; l == nil || *l != (model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-1"}) {
				t.Errorf("the name links to %+v", l)
			}
			if l := row[2].Link; row[2].Text == "" || l == nil || l.Kind != "nodes" || l.Name != row[2].Text {
				t.Errorf("the node cell %+v does not link to its node", row[2])
			}
			if row[3].Text == "" || row[4] != (model.Cell{}) || row[5] != (model.Cell{}) || row[6].Text == "" {
				t.Errorf("Ready %+v, CPU and Memory %+v %+v (the engine's to fill), Status %+v", row[3], row[4], row[5], row[6])
			}
			if pod := rel["pods"][1]; row[7] != model.AgeCell(pod.GetCreationTimestamp().Time) || row[7].Num == 0 {
				t.Errorf("Age %+v, want the creation time of %s", row[7], pod.GetName())
			}
			if got := podsTableNames(p.DetailSections(owner, nil)); got != nil {
				t.Errorf("without related objects: Pods %q", got)
			}
			if got := podsTableNames(p.DetailSections(named(owner, "shop", "idle"), rel)); got != nil {
				t.Errorf("a workload without pods has a Pods table: %q", got)
			}
		})
	}
}

// A Deployment's pods are those of every replica set it controls: during a rollout the old and the
// new replica set's; not another Deployment's, nor a replica set's that names it without being
// controlled by it.
func TestDeploymentDrawerPodsThroughReplicaSets(t *testing.T) {
	p, _ := kinds.ByKey("deployments")
	deploy := named(recorded(t, "deployments", "healthy"), "shop", "web")
	other := named(deploy, "shop", "api")
	rs := func(name string, owner *unstructured.Unstructured) *unstructured.Unstructured {
		return controlled(named(recorded(t, "replicasets", "healthy"), "shop", name), owner)
	}
	oldRS, newRS, apiRS := rs("web-old", deploy), rs("web-new", deploy), rs("api-1", other)
	pod := func(name string, owner *unstructured.Unstructured) *unstructured.Unstructured {
		return controlled(named(recorded(t, "pods", "healthy"), "shop", name), owner)
	}
	rel := fakeRelated{
		"replicasets": {oldRS, newRS, apiRS},
		"pods":        {pod("web-new-b", newRS), pod("web-old-a", oldRS), pod("web-new-a", newRS), pod("api-1-a", apiRS)},
	}
	if got := podsTableNames(p.DetailSections(deploy, rel)); !slices.Equal(got, []string{"web-new-a", "web-new-b", "web-old-a"}) {
		t.Errorf("the Deployment's Pods %q, want both replica sets' pods", got)
	}
	// The replica sets' kind not served (or still listing): no Pods.
	if got := podsTableNames(p.DetailSections(deploy, fakeRelated{"pods": rel["pods"]})); got != nil {
		t.Errorf("without replica sets: Pods %q", got)
	}
}

// A Cron Job's pods are those of the jobs it controls.
func TestCronJobDrawerPodsThroughJobs(t *testing.T) {
	p, _ := kinds.ByKey("cronjobs")
	cj := named(recorded(t, "cronjobs", "healthy"), "batch", "report")
	job := func(name string) *unstructured.Unstructured {
		return controlled(named(recorded(t, "jobs", "healthy"), "batch", name), cj)
	}
	j1, j2 := job("report-1"), job("report-2")
	manual := named(recorded(t, "jobs", "healthy"), "batch", "report-manual") // no owner
	pod := func(name string, owner *unstructured.Unstructured) *unstructured.Unstructured {
		return controlled(named(recorded(t, "pods", "completed-job"), "batch", name), owner)
	}
	rel := fakeRelated{"jobs": {j1, j2, manual}, "pods": {pod("report-2-x", j2), pod("report-1-x", j1), pod("report-manual-x", manual)}}
	if got := podsTableNames(p.DetailSections(cj, rel)); !slices.Equal(got, []string{"report-1-x", "report-2-x"}) {
		t.Errorf("the Cron Job's Pods %q", got)
	}
}

// The lists of the workloads read no related objects: the pods' indexes are the drawer's alone
// (DetailNeeds), so a pod's change rebuilds no workload row (G6).
func TestWorkloadListsDoNotNeedPods(t *testing.T) {
	want := map[string][]string{
		"deployments": {"replicasets/byOwner", "pods/byOwner"}, "cronjobs": {"jobs/byOwner", "pods/byOwner"},
		"replicasets": {"pods/byOwner"}, "statefulsets": {"pods/byOwner"}, "daemonsets": {"pods/byOwner"}, "jobs": {"pods/byOwner"},
	}
	for key, ids := range want {
		p, _ := kinds.ByKey(key)
		if len(p.Needs) != 0 || p.CellsWith != nil {
			t.Errorf("%s: the list reads related objects (%v)", key, p.Needs)
		}
		var got []string
		for _, ix := range p.DrawerNeeds() {
			got = append(got, ix.ID())
		}
		if !slices.Equal(got, ids) {
			t.Errorf("%s: the drawer reads %q, want %q", key, got, ids)
		}
	}
}
