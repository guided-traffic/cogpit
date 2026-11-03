package engine

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// rbacProfiles are the built-in profiles of the cluster role bindings and the cluster roles they
// read through kinds.ClusterRolesByName.
func rbacProfiles(t *testing.T) (crbs, crs *model.KindProfile) {
	t.Helper()
	crbs, ok1 := kinds.ByKey("clusterrolebindings")
	crs, ok2 := kinds.ByKey("clusterroles")
	if !ok1 || !ok2 {
		t.Fatal("no RBAC profiles")
	}
	return crbs, crs
}

func testClusterRole(name string) *unstructured.Unstructured {
	return testkit.Object("rbac.authorization.k8s.io/v1", "ClusterRole", "", name, map[string]any{
		"rules": []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"pods"}, "verbs": []any{"get"}}},
	})
}

// TestClusterRoleBindingFollowsItsClusterRole: the Cluster Role Bindings view warns about a binding
// whose cluster role does not exist, from the cached cluster roles, and stops when the role is
// created, although the binding itself did not change; the drawer links the role once it exists.
func TestClusterRoleBindingFollowsItsClusterRole(t *testing.T) {
	crbs, crs := rbacProfiles(t)
	src := testkit.NewMemorySource()
	src.Set(crs.GVR(), testClusterRole("view"))
	src.Set(crbs.GVR(), testkit.Object("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", "deployers", map[string]any{
		"roleRef":  map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "deployer"},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "ci", "namespace": "ci"}},
	}))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), crbs, crs)
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = crbs.Key })

	warning := func(ts *TableSnapshot) string {
		if len(ts.Rows) != 1 {
			return "?"
		}
		return ts.Rows[0].Cells[crbs.ColumnIndex("signals")].Text
	}
	const notFound = "Cluster role deployer not found: grants nothing"
	waitTable(t, s, crbs.Key, func(ts *TableSnapshot) bool { return ts.Synced && warning(ts) == notFound })
	src.Set(crs.GVR(), testClusterRole("deployer"))
	ts := waitTable(t, s, crbs.Key, func(ts *TableSnapshot) bool { return warning(ts) == "" })
	if ts.Rows[0].Status != model.StatusNone || ts.Rows[0].Cells[crbs.ColumnIndex("bindings")].Text != "ci" {
		t.Errorf("row %+v", ts.Rows[0])
	}

	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: crbs.Key, Name: "deployers"})
	if err != nil {
		t.Fatal(err)
	}
	var role *model.DetailItem
	for i, it := range m.Sections[0].Items {
		if it.Name == "Role" {
			role = &m.Sections[0].Items[i]
		}
	}
	if role == nil || role.Link == nil || *role.Link != (model.ObjectRef{Kind: "clusterroles", Name: "deployer"}) {
		t.Errorf("role item %+v", role)
	}
}
