package kinds_test

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// stuckNamespace is a namespace as the namespace controller left walk-fin on kind v1.36.1
// (2026-10-19): a ConfigMap and a Secret with finalizers, the messages verbatim.
func stuckNamespace(conds ...map[string]any) *unstructured.Unstructured {
	list := make([]any, len(conds))
	for i, c := range conds {
		list[i] = c
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": "walk-fin", "deletionTimestamp": "2026-10-19T10:00:00Z"},
		"spec":     map[string]any{"finalizers": []any{"kubernetes"}},
		"status":   map[string]any{"phase": "Terminating", "conditions": list},
	}}
}

func cond(typ, status, msg string) map[string]any {
	return map[string]any{"type": typ, "status": status, "message": msg}
}

func TestNamespaceDeletionFromTheControllersConditions(t *testing.T) {
	ns := stuckNamespace(
		cond(kinds.CondDiscoveryFailure, "False", "All resources successfully discovered"),
		cond(kinds.CondParsingFailure, "False", "All legacy kube types successfully parsed"),
		cond(kinds.CondContentFailure, "False", "All content successfully deleted, may be waiting on finalization"),
		cond(kinds.CondContentRemaining, "True", "Some resources are remaining: configmaps. has 1 resource instances, secrets. has 1 resource instances, widgets.example.com has 12 resource instances"),
		cond(kinds.CondFinalizersRemaining, "True", "Some content in the namespace has finalizers remaining: example.com/other in 1 resource instances, example.com/walk-research in 2 resource instances"),
	)
	d := kinds.NamespaceDeletionOf(ns)
	if !d.Terminating || !d.Reported || !d.Blocked() {
		t.Fatalf("state %+v", d)
	}
	want := []kinds.RemainingResource{
		{GroupResource: schema.GroupResource{Resource: "configmaps"}, Count: 1},
		{GroupResource: schema.GroupResource{Resource: "secrets"}, Count: 1},
		{GroupResource: schema.GroupResource{Group: "example.com", Resource: "widgets"}, Count: 12},
	}
	if !reflect.DeepEqual(d.Remaining, want) {
		t.Errorf("remaining %+v", d.Remaining)
	}
	if d.Remaining[2].String() != "widgets.example.com" || d.Remaining[0].String() != "configmaps" {
		t.Errorf("names %s %s", d.Remaining[2], d.Remaining[0])
	}
	wantFin := []kinds.RemainingFinalizer{{Name: "example.com/other", Count: 1}, {Name: "example.com/walk-research", Count: 2}}
	if !reflect.DeepEqual(d.Finalizers, wantFin) {
		t.Errorf("finalizers %+v", d.Finalizers)
	}
	if d.DiscoveryFailure != "" || len(d.Failures) != 0 {
		t.Errorf("failures %+v", d)
	}
}

func TestNamespaceDeletionFailuresAndEdges(t *testing.T) {
	ns := stuckNamespace(
		cond(kinds.CondDiscoveryFailure, "True", "Discovery failed for some groups, 1 failing: unable to retrieve the complete list of server APIs: metrics.k8s.io/v1beta1: stale GroupVersion discovery: metrics.k8s.io/v1beta1"),
		cond(kinds.CondContentFailure, "True", "Failed to delete all resource types, 1 remaining: x"),
		cond(kinds.CondContentRemaining, "False", "All content successfully removed"),
	)
	d := kinds.NamespaceDeletionOf(ns)
	if d.DiscoveryFailure == "" || len(d.Failures) != 1 || d.Failures[0].Type != kinds.CondContentFailure || len(d.Remaining) != 0 {
		t.Errorf("failures %+v", d)
	}
	// Nothing reported yet, an active namespace, and messages that do not parse.
	if d := kinds.NamespaceDeletionOf(stuckNamespace()); !d.Terminating || d.Reported || d.Blocked() {
		t.Errorf("unreported %+v", d)
	}
	active := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "a"}, "status": map[string]any{"phase": "Active"}}}
	if d := kinds.NamespaceDeletionOf(active); d.Terminating || d.Blocked() {
		t.Errorf("active %+v", d)
	}
	garbled := stuckNamespace(
		cond(kinds.CondContentRemaining, "True", "Some resources are remaining: , has x, configmaps. has -3 resource instances, bad entry"),
		cond(kinds.CondFinalizersRemaining, "True", "nothing to split"),
	)
	d = kinds.NamespaceDeletionOf(garbled)
	if len(d.Remaining) != 1 || d.Remaining[0].Resource != "configmaps" || d.Remaining[0].Count != 0 || d.Finalizers != nil {
		t.Errorf("garbled %+v", d)
	}
	if d := kinds.NamespaceDeletionOf(nil); d.Terminating {
		t.Error("nil")
	}
}

func FuzzNamespaceDeletion(f *testing.F) {
	f.Add("Some resources are remaining: configmaps. has 1 resource instances", "Some content in the namespace has finalizers remaining: a/b in 2 resource instances")
	f.Fuzz(func(_ *testing.T, content, fins string) {
		kinds.NamespaceDeletionOf(stuckNamespace(cond(kinds.CondContentRemaining, "True", content), cond(kinds.CondFinalizersRemaining, "True", fins)))
	})
}
