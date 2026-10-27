package fyneui

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The Pod Disruption Budgets list as the real kind profile fills it: the warning column after
// Name (a budget not met, one that allows no disruption, one the controller could not compute),
// "N/A" for a bound a budget does not set, and the healthy counts. PNGs:
// testdata/pdbs_list_{dark,light}.png.

// pdbSpec is one fixture budget: its bounds (one of them usually ""), and its status.
type pdbSpec struct {
	ns, name                  string
	minAvailable, maxUnavail  any // int64, a percentage string, or nil
	current, desired, allowed int64
	expected                  int64
	syncFailed                bool
	age                       time.Duration
}

func fixturePDBObject(p pdbSpec) *unstructured.Unstructured {
	spec := map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": p.name}}}
	if p.minAvailable != nil {
		spec["minAvailable"] = p.minAvailable
	}
	if p.maxUnavail != nil {
		spec["maxUnavailable"] = p.maxUnavail
	}
	cond := map[string]any{"type": "DisruptionAllowed", "status": "True", "reason": "SufficientPods"}
	switch {
	case p.syncFailed:
		cond = map[string]any{"type": "DisruptionAllowed", "status": "False", "reason": "SyncFailed",
			"message": `found no controllers for pod "` + p.name + `-x7k2p"`}
	case p.allowed == 0:
		cond = map[string]any{"type": "DisruptionAllowed", "status": "False", "reason": "InsufficientPods"}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
		"metadata": map[string]any{"name": p.name, "namespace": p.ns, "uid": "uid-pdb-" + p.name, "creationTimestamp": ts(p.age)},
		"spec":     spec,
		"status": map[string]any{
			"currentHealthy": p.current, "desiredHealthy": p.desired, "disruptionsAllowed": p.allowed,
			"expectedPods": p.expected, "conditions": []any{cond},
		},
	}}
}

func fixturePDBObjects() []*unstructured.Unstructured {
	specs := []pdbSpec{
		{ns: "checkout", name: "checkout-api", minAvailable: int64(2), current: 3, desired: 2, allowed: 1, expected: 3, age: 40 * 24 * time.Hour},
		{ns: "payments", name: "payments-worker", minAvailable: int64(2), current: 1, desired: 2, allowed: 0, expected: 2, age: 12 * 24 * time.Hour},
		{ns: "platform", name: "etcd", maxUnavail: int64(0), current: 3, desired: 3, allowed: 0, expected: 3, age: 160 * 24 * time.Hour},
		{ns: "storefront", name: "web-frontend", minAvailable: "50%", current: 10, desired: 5, allowed: 5, expected: 10, age: 9 * 24 * time.Hour},
		{ns: "search", name: "search-indexer", maxUnavail: int64(1), current: 4, desired: 3, allowed: 1, expected: 4, age: 30 * 24 * time.Hour},
		{ns: "monitoring", name: "metrics-collector", maxUnavail: "10%", expected: 4, syncFailed: true, age: 2 * 24 * time.Hour},
		{ns: "qa", name: "image-resizer", minAvailable: int64(1), desired: 1, age: 5 * 24 * time.Hour},
		{ns: "orders", name: "ledger-writer", minAvailable: int64(1), current: 1, desired: 1, allowed: 0, expected: 1, age: 12 * 24 * time.Hour},
		{ns: "identity", name: "auth-gateway", maxUnavail: "25%", current: 8, desired: 6, allowed: 2, expected: 8, age: 70 * 24 * time.Hour},
		{ns: "kube-system", name: "coredns", maxUnavail: int64(1), current: 2, desired: 1, allowed: 1, expected: 2, age: 160 * 24 * time.Hour},
	}
	out := make([]*unstructured.Unstructured, 0, len(specs))
	for _, s := range specs {
		out = append(out, fixturePDBObject(s))
	}
	return out
}

func renderPDBList(t *testing.T, mode ThemeMode, name string) {
	const view = "poddisruptionbudgets"
	f := kindsFake(t, view)
	f.kinds = append(f.kinds, kindInfo{Kind: view, Title: "Pod Disruption Budgets", APIKind: "PodDisruptionBudget",
		Group: "policy", Version: "v1", Namespaced: true})
	f.tables[view] = kindSnapshot(t, view, fixturePDBObjects())
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the "+view+" view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderPDBListDark(t *testing.T) { renderPDBList(t, ThemeDark, "pdbs_list_dark.png") }
