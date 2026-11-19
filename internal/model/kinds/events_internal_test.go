package kinds

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

func asUnstructured(obj map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: obj}
}

// definitions serves CustomKindsByGroupKind from definitions, as the engine's index would.
type definitions []*unstructured.Unstructured

func (d definitions) Listed(ix model.RelatedIndex) bool {
	return ix.Kind == KeyCustomResourceDefinitions
}

func (d definitions) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, o := range d {
		for _, k := range ix.Keys(o) {
			if k == key {
				out = append(out, o)
			}
		}
	}
	return out
}

// definition is a synthetic custom resource definition, established unless told otherwise.
func definition(group, plural, kind, scope string, established bool) *unstructured.Unstructured {
	status := "True"
	if !established {
		status = "False"
	}
	return asUnstructured(map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]any{"name": plural + "." + group},
		"spec": map[string]any{
			"group": group, "scope": scope,
			"names":    map[string]any{"plural": plural, "kind": kind},
			"versions": []any{map[string]any{"name": "v1", "served": true, "storage": true}},
		},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Established", "status": status}}},
	})
}

// TestEventInvolvedCustomResourceLinks: an event about a custom resource links to its kind's view
// once the definitions are known, as Lens's do (capture). On a real cluster the owner provides for
// tests, 174 of 188 events were about Flux's and cert-manager's kinds, all plain before; a
// cluster-scoped kind drops the namespace; a definition that is not established yet, an unknown
// group or no definitions leave the link out; built-in kinds link as before. The event's drawer
// links the same.
func TestEventInvolvedCustomResourceLinks(t *testing.T) {
	defs := definitions{
		definition("kustomize.toolkit.fluxcd.io", "kustomizations", "Kustomization", "Namespaced", true),
		definition("trust.cert-manager.io", "bundles", "Bundle", "Cluster", true),
		definition("shop.example.com", "widgets", "Widget", "Namespaced", false),
	}
	ev := func(apiVersion, kind, name string) *unstructured.Unstructured {
		return asUnstructured(map[string]any{
			"metadata":       map[string]any{"name": "e", "namespace": "flux-system"},
			"involvedObject": map[string]any{"apiVersion": apiVersion, "kind": kind, "name": name},
		})
	}
	tests := []struct {
		obj  *unstructured.Unstructured
		rel  model.Related
		want *model.ObjectRef
	}{
		{ev("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "apps"), defs,
			&model.ObjectRef{Kind: "kustomizations.kustomize.toolkit.fluxcd.io", Namespace: "flux-system", Name: "apps"}},
		{ev("trust.cert-manager.io/v1alpha1", "Bundle", "truststore"), defs,
			&model.ObjectRef{Kind: "bundles.trust.cert-manager.io", Name: "truststore"}},
		{ev("shop.example.com/v1", "Widget", "w"), defs, nil},
		{ev("source.toolkit.fluxcd.io/v1", "GitRepository", "flux-system"), defs, nil},
		{ev("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "apps"), model.NoRelated, nil},
		{ev("v1", "Pod", "web-0"), defs, &model.ObjectRef{Kind: "pods", Namespace: "flux-system", Name: "web-0"}},
	}
	p := events
	col := p.ColumnIndex("involvedObject")
	for _, tt := range tests {
		row := model.BuildRowWith(p, "test", tt.obj, tt.rel)
		got := row.Cells[col].Link
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%v: link %+v, want %+v", tt.obj.Object["involvedObject"], got, tt.want)
		}
		var item *model.ObjectRef
		for _, s := range p.DetailSections(tt.obj, tt.rel) {
			for _, it := range s.Items {
				if it.Name == "Involved Object" {
					item = it.Link
				}
			}
		}
		if (item == nil) != (tt.want == nil) || (item != nil && *item != *tt.want) {
			t.Errorf("%v: drawer link %+v, want %+v", tt.obj.Object["involvedObject"], item, tt.want)
		}
	}
	if len(p.Needs) != 1 || p.Needs[0].ID() != CustomKindsByGroupKind.ID() {
		t.Errorf("the Events view reads %v, want the definitions by group and Kind", p.Needs)
	}
}

// TestPodControlledByCustomResource: a pod whose controller is a custom resource (cert-manager's
// Challenge, the logging operator's Logging; seen on a real cluster) links to it in the Pods list
// once the definitions are known, and stays plain without them; the Pods view reads the definitions
// by group and Kind.
func TestPodControlledByCustomResource(t *testing.T) {
	defs := definitions{definition("acme.cert-manager.io", "challenges", "Challenge", "Namespaced", true)}
	obj := asUnstructured(pod(map[string]any{"containers": []any{map[string]any{"name": "solver"}}}, map[string]any{"phase": "Running"},
		map[string]any{"name": "cm-acme-http-solver-x", "namespace": "iam", "ownerReferences": []any{map[string]any{
			"apiVersion": "acme.cert-manager.io/v1", "kind": "Challenge", "name": "dex-tls-1", "controller": true}}}))
	col := pods.ColumnIndex("controlledBy")
	row := model.BuildRowWith(pods, "test", obj, defs)
	want := model.ObjectRef{Kind: "challenges.acme.cert-manager.io", Namespace: "iam", Name: "dex-tls-1"}
	if c := row.Cells[col]; c.Text != "Challenge" || c.Link == nil || *c.Link != want {
		t.Errorf("Controlled By %+v, want Challenge linked to %+v", c, want)
	}
	if c := model.BuildRow(pods, "test", obj).Cells[col]; c.Text != "Challenge" || c.Link != nil {
		t.Errorf("Controlled By without the definitions %+v, want plain", c)
	}
	if len(pods.Needs) != 1 || pods.Needs[0].ID() != CustomKindsByGroupKind.ID() {
		t.Errorf("the Pods view reads %v", pods.Needs)
	}
}

// TestEventInvolvedObjectLinks pins the Involved Object link of the Events list: built-in kinds
// link by their key; a custom kind stays unlinked without its definition, because its key needs
// the plural (TestEventInvolvedCustomResourceLinks: with it).
func TestEventInvolvedObjectLinks(t *testing.T) {
	ev := func(ref map[string]any) map[string]any {
		return map[string]any{"metadata": map[string]any{"name": "e", "namespace": "shop"}, "involvedObject": ref}
	}
	tests := []struct {
		obj  map[string]any
		want *model.ObjectRef
	}{
		{ev(map[string]any{"apiVersion": "v1", "kind": "Pod", "namespace": "shop", "name": "web-0"}), &model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}},
		{ev(map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "web"}), &model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}},
		{ev(map[string]any{"kind": "Node", "name": "worker-1"}), &model.ObjectRef{Kind: "nodes", Name: "worker-1"}},
		{ev(map[string]any{"apiVersion": "cert-manager.io/v1", "kind": "Certificate", "name": "tls"}), nil},
		{map[string]any{"regarding": map[string]any{"apiVersion": "batch/v1", "kind": "Job", "namespace": "ci", "name": "build"}}, &model.ObjectRef{Kind: "jobs", Namespace: "ci", Name: "build"}},
	}
	col := events.ColumnIndex("involvedObject")
	for _, tt := range tests {
		cells, _ := eventCells(asUnstructured(tt.obj))
		got := cells[col].Link
		if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
			t.Errorf("%v: link %+v, want %+v", tt.obj, got, tt.want)
		}
	}
}
