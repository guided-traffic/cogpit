package kinds_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// object builds an object of a kind in a namespace ("" for a cluster-scoped one) from its fields
// beside metadata, with labels.
func object(kind, ns, name string, labels map[string]any, fields map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if ns != "" {
		meta["namespace"] = ns
	}
	if labels != nil {
		meta["labels"] = labels
	}
	o := map[string]any{"kind": kind, "metadata": meta}
	for k, v := range fields {
		o[k] = v
	}
	return &unstructured.Unstructured{Object: o}
}

// backRefRows returns the section's rows as text, and fails unless the kind has the section.
func backRefRows(t *testing.T, kind string, obj *unstructured.Unstructured, rel model.Related) (model.DetailSection, [][]string) {
	t.Helper()
	sec, ok := kinds.BackRefSection(kind, obj, rel)
	if !ok {
		t.Fatalf("%s: no back references section", kind)
	}
	if sec.Title != kinds.TitleReferencedBy {
		t.Fatalf("title %q", sec.Title)
	}
	if sec.Table == nil {
		return sec, nil
	}
	return sec, sec.Table.Rows
}

func TestBackRefsOfAPod(t *testing.T) {
	pod := object("Pod", "shop", "web-0", map[string]any{"app": "web", "tier": "front"}, nil)
	rel := fakeRelated{
		"services": {
			object("Service", "shop", "web", nil, map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web"}}}),
			object("Service", "shop", "web-front", nil, map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web", "tier": "front"}}}),
			object("Service", "shop", "web-back", nil, map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web", "tier": "back"}}}),
			object("Service", "other", "web", nil, map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web"}}}),
			object("Service", "shop", "manual", nil, map[string]any{"spec": map[string]any{}}),
		},
		"networkpolicies": {
			object("NetworkPolicy", "shop", "default-deny", nil, map[string]any{"spec": map[string]any{"podSelector": map[string]any{}}}),
			object("NetworkPolicy", "shop", "front-only", nil, map[string]any{"spec": map[string]any{"podSelector": map[string]any{
				"matchExpressions": []any{map[string]any{"key": "tier", "operator": "In", "values": []any{"front", "edge"}}}}}}),
			object("NetworkPolicy", "shop", "not-back", nil, map[string]any{"spec": map[string]any{"podSelector": map[string]any{
				"matchExpressions": []any{map[string]any{"key": "tier", "operator": "NotIn", "values": []any{"back"}}}}}}),
			object("NetworkPolicy", "shop", "no-canary", nil, map[string]any{"spec": map[string]any{"podSelector": map[string]any{
				"matchExpressions": []any{map[string]any{"key": "canary", "operator": "Exists"}}}}}),
			object("NetworkPolicy", "shop", "odd", nil, map[string]any{"spec": map[string]any{"podSelector": map[string]any{
				"matchExpressions": []any{map[string]any{"key": "tier", "operator": "Gt", "values": []any{"1"}}}}}}),
		},
		"poddisruptionbudgets": {
			object("PodDisruptionBudget", "shop", "web-pdb", nil, map[string]any{"spec": map[string]any{"selector": map[string]any{
				"matchLabels": map[string]any{"app": "web"}, "matchExpressions": []any{map[string]any{"key": "tier", "operator": "Exists"}}}}}),
			object("PodDisruptionBudget", "shop", "none", nil, map[string]any{"spec": map[string]any{}}),
		},
	}
	sec, rows := backRefRows(t, "pods", pod, rel)
	want := [][]string{
		{"Service", "web", "spec.selector"},
		{"Service", "web-front", "spec.selector"},
		{"NetworkPolicy", "default-deny", "spec.podSelector (all pods)"},
		{"NetworkPolicy", "front-only", "spec.podSelector"},
		{"NetworkPolicy", "not-back", "spec.podSelector"},
		{"PodDisruptionBudget", "web-pdb", "spec.selector"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows\n got %q\nwant %q", rows, want)
	}
	if got := sec.Table.Columns; !slices.Equal(got, []string{"Kind", "Name", "Via"}) {
		t.Fatalf("columns %q", got)
	}
	if !sec.Table.Sortable {
		t.Fatal("not sortable")
	}
	if link := sec.Table.Link(0, 1); link == nil || *link != (model.ObjectRef{Kind: "services", Namespace: "shop", Name: "web"}) {
		t.Fatalf("link %v", link)
	}
	if len(sec.Items) != 0 {
		t.Fatalf("items %v", sec.Items)
	}
}

func TestBackRefsByName(t *testing.T) {
	pod := object("Pod", "shop", "web-0", nil, map[string]any{"spec": map[string]any{
		"serviceAccountName": "web",
		"volumes": []any{
			map[string]any{"name": "config", "configMap": map[string]any{"name": "web-config"}},
			map[string]any{"name": "all", "projected": map[string]any{"sources": []any{
				map[string]any{"configMap": map[string]any{"name": "web-config"}},
				map[string]any{"secret": map[string]any{"name": "web-tls"}},
			}}},
			map[string]any{"name": "creds", "secret": map[string]any{"secretName": "db"}},
		},
		"imagePullSecrets": []any{map[string]any{"name": "registry"}},
		"containers": []any{map[string]any{"name": "web",
			"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "web-config"}}},
			"env": []any{
				map[string]any{"name": "LEVEL", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "web-config", "key": "level"}}},
				map[string]any{"name": "MODE", "valueFrom": map[string]any{"configMapKeyRef": map[string]any{"name": "web-config", "key": "mode"}}},
				map[string]any{"name": "PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "db", "key": "password"}}},
			}}},
	}})
	cases := []struct {
		kind string
		obj  *unstructured.Unstructured
		rel  fakeRelated
		want [][]string
	}{
		{"configmaps", object("ConfigMap", "shop", "web-config", nil, nil), fakeRelated{"pods": {pod}},
			[][]string{{"Pod", "web-0", "volume config\nvolume all (projected)\nenvFrom web\nenv web: LEVEL, MODE"}}},
		{"secrets", object("Secret", "shop", "db", nil, nil), fakeRelated{"pods": {pod}, "ingresses": {}, "serviceaccounts": {}},
			[][]string{{"Pod", "web-0", "volume creds\nenv web: PASSWORD"}}},
		{"secrets", object("Secret", "shop", "web-tls", nil, nil), fakeRelated{"pods": {pod}, "serviceaccounts": {}, "ingresses": {
			object("Ingress", "shop", "web", nil, map[string]any{"spec": map[string]any{"tls": []any{
				map[string]any{"hosts": []any{"shop.example", "www.shop.example"}, "secretName": "web-tls"}}}}),
		}}, [][]string{{"Pod", "web-0", "volume all (projected)"}, {"Ingress", "web", "tls: shop.example, www.shop.example"}}},
		{"secrets", object("Secret", "shop", "registry", nil, nil), fakeRelated{"pods": {pod}, "ingresses": {}, "serviceaccounts": {
			object("ServiceAccount", "shop", "web", nil, map[string]any{"imagePullSecrets": []any{map[string]any{"name": "registry"}}}),
		}}, [][]string{{"Pod", "web-0", "imagePullSecrets"}, {"ServiceAccount", "web", "imagePullSecrets"}}},
		{"serviceaccounts", object("ServiceAccount", "shop", "web", nil, nil), fakeRelated{"pods": {pod},
			"rolebindings": {
				object("RoleBinding", "shop", "web-read", nil, map[string]any{"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "web"}}}),
				object("RoleBinding", "shop", "other", nil, map[string]any{"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "web", "namespace": "other"}}}),
			},
			"clusterrolebindings": {
				object("ClusterRoleBinding", "", "web-view", nil, map[string]any{"subjects": []any{
					map[string]any{"kind": "User", "name": "web"},
					map[string]any{"kind": "ServiceAccount", "name": "web", "namespace": "shop"}}}),
			}},
			[][]string{{"Pod", "web-0", "spec.serviceAccountName"}, {"RoleBinding", "web-read", "subjects"}, {"ClusterRoleBinding", "web-view", "subjects"}}},
		{"services", object("Service", "shop", "web", nil, nil), fakeRelated{"ingresses": {
			object("Ingress", "shop", "web", nil, map[string]any{"spec": map[string]any{
				"defaultBackend": map[string]any{"service": map[string]any{"name": "web"}},
				"rules": []any{
					map[string]any{"host": "shop.example", "http": map[string]any{"paths": []any{
						map[string]any{"path": "/api", "backend": map[string]any{"service": map[string]any{"name": "web"}}},
						map[string]any{"path": "/img", "backend": map[string]any{"service": map[string]any{"name": "img"}}}}}},
					map[string]any{"http": map[string]any{"paths": []any{
						map[string]any{"path": "/", "backend": map[string]any{"service": map[string]any{"name": "web"}}}}}},
				}}}),
		}}, [][]string{{"Ingress", "web", "defaultBackend\nshop.example/api\n*/"}}},
		{"roles", object("Role", "shop", "reader", nil, nil), fakeRelated{"rolebindings": {
			object("RoleBinding", "shop", "read", nil, map[string]any{"roleRef": map[string]any{"kind": "Role", "name": "reader"}}),
			object("RoleBinding", "other", "read", nil, map[string]any{"roleRef": map[string]any{"kind": "Role", "name": "reader"}}),
			object("RoleBinding", "shop", "view", nil, map[string]any{"roleRef": map[string]any{"kind": "ClusterRole", "name": "reader"}}),
		}}, [][]string{{"RoleBinding", "read", "roleRef"}}},
	}
	for _, c := range cases {
		_, rows := backRefRows(t, c.kind, c.obj, c.rel)
		if !reflect.DeepEqual(rows, c.want) {
			t.Errorf("%s %s\n got %q\nwant %q", c.kind, c.obj.GetName(), rows, c.want)
		}
	}
}

// A cluster-scoped object's referrers come from every namespace: the table names theirs.
func TestBackRefsOfClusterScoped(t *testing.T) {
	role := object("ClusterRole", "", "view", nil, nil)
	rel := fakeRelated{
		"rolebindings": {
			object("RoleBinding", "shop", "view", nil, map[string]any{"roleRef": map[string]any{"kind": "ClusterRole", "name": "view"}}),
			object("RoleBinding", "blog", "view", nil, map[string]any{"roleRef": map[string]any{"kind": "ClusterRole", "name": "view"}}),
			object("RoleBinding", "blog", "role", nil, map[string]any{"roleRef": map[string]any{"kind": "Role", "name": "view"}}),
		},
		"clusterrolebindings": {
			object("ClusterRoleBinding", "", "view-all", nil, map[string]any{"roleRef": map[string]any{"kind": "ClusterRole", "name": "view"}}),
		},
	}
	sec, rows := backRefRows(t, "clusterroles", role, rel)
	want := [][]string{{"RoleBinding", "view", "blog", "roleRef"}, {"RoleBinding", "view", "shop", "roleRef"}, {"ClusterRoleBinding", "view-all", "", "roleRef"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows\n got %q\nwant %q", rows, want)
	}
	if got := sec.Table.Columns; !slices.Equal(got, []string{"Kind", "Name", "Namespace", "Via"}) {
		t.Fatalf("columns %q", got)
	}
	if link := sec.Table.Link(0, 1); link == nil || *link != (model.ObjectRef{Kind: "rolebindings", Namespace: "blog", Name: "view"}) {
		t.Fatalf("link %v", link)
	}

	class := object("StorageClass", "", "fast", nil, nil)
	_, rows = backRefRows(t, "storageclasses", class, fakeRelated{
		"persistentvolumeclaims": {
			object("PersistentVolumeClaim", "shop", "data", nil, map[string]any{"spec": map[string]any{"storageClassName": "fast"}}),
			object("PersistentVolumeClaim", "shop", "old", nil, nil),
			object("PersistentVolumeClaim", "shop", "none", nil, map[string]any{"spec": map[string]any{"storageClassName": ""}}),
		},
		"persistentvolumes": {object("PersistentVolume", "", "pv-1", nil, map[string]any{"spec": map[string]any{"storageClassName": "fast"}})},
	})
	want = [][]string{{"PersistentVolumeClaim", "data", "shop", "spec.storageClassName"}, {"PersistentVolume", "pv-1", "", "spec.storageClassName"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("storage class rows\n got %q\nwant %q", rows, want)
	}
}

// Without referrers the section names the kinds it found none in, and those it could not read.
func TestBackRefsNoneAndNotRead(t *testing.T) {
	pod := object("Pod", "shop", "web-0", map[string]any{"app": "web"}, nil)
	sec, rows := backRefRows(t, "pods", pod, fakeRelated{"services": {}})
	if rows != nil {
		t.Fatalf("rows %q", rows)
	}
	want := []model.DetailItem{
		{Name: "Not read", Value: "Network Policies, Pod Disruption Budgets"},
		{Name: "Found", Value: "None in Services"},
	}
	if !reflect.DeepEqual(sec.Items, want) {
		t.Fatalf("items %+v", sec.Items)
	}
	sec, _ = backRefRows(t, "pods", pod, nil)
	if len(sec.Items) != 1 || sec.Items[0].Name != "Not read" || !strings.Contains(sec.Items[0].Value, "Services") {
		t.Fatalf("nothing read: %+v", sec.Items)
	}
	if _, ok := kinds.BackRefSection("deployments", object("Deployment", "shop", "web", nil, nil), nil); ok {
		t.Fatal("a kind without back references has the section")
	}
	if kinds.BackRefNeeds("deployments") != nil || len(kinds.BackRefNeeds("pods")) != 3 {
		t.Fatalf("needs: %v, %v", kinds.BackRefNeeds("deployments"), kinds.BackRefNeeds("pods"))
	}
}

// TestBackRefsTypeConfusion feeds every recorded object of a referring kind, with every field
// replaced by each hostile value in turn, to the section of each kind it may point at.
func TestBackRefsTypeConfusion(t *testing.T) {
	if raceEnabled {
		t.Skip("one goroutine: the build without the race detector runs it (make test-norace-coverage)")
	}
	targets := map[string]*unstructured.Unstructured{
		"pods": recorded(t, "pods", "healthy"), "configmaps": recorded(t, "configmaps", "healthy"),
		"secrets": recorded(t, "secrets", "healthy"), "serviceaccounts": recorded(t, "serviceaccounts", "healthy"),
		"services": recorded(t, "services", "healthy"), "roles": recorded(t, "roles", "healthy"),
		"clusterroles": recorded(t, "clusterroles", "healthy"), "storageclasses": recorded(t, "storageclasses", "healthy"),
	}
	for kind, target := range targets {
		for _, ix := range kinds.BackRefNeeds(kind) {
			for _, c := range loadCases(t, ix.Kind) {
				confuse(t, kind+" referenced by "+ix.Kind+"/"+c.name, c.obj, func([]any, any) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s by %s %v: panic: %v", kind, ix.Kind, c.obj.Object, r)
						}
					}()
					_ = ix.Keys(c.obj)
					_, _ = kinds.BackRefSection(kind, target, allKeys{fakeRelated: fakeRelated{ix.Kind: {c.obj}}})
				})
			}
		}
	}
}
