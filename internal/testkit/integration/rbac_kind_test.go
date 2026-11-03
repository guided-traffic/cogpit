//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindAccessControlViews fills the Access Control views from watches on the kind cluster's
// own RBAC objects and checks what kubeadm and kind create: the bindings' subjects, the bootstrap
// policy's system:node binding without a warning, an aggregated cluster role with its rules, links
// from a binding to its cluster role and service account. A binding that names a cluster role that
// does not exist is built against the real cluster roles without being created. It writes nothing
// to the cluster.
func TestKindAccessControlViews(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params any) any {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}

	for _, view := range []string{"serviceaccounts", "clusterroles", "roles", "clusterrolebindings", "rolebindings"} {
		dispatch("ui.openView", map[string]any{"view": view})
		if !testkit.WaitFor(ctx, func() bool {
			tbl := s.Table(view)
			return tbl != nil && tbl.Synced && tbl.Total > 0
		}) {
			t.Fatalf("view %s never filled: %+v", view, s.Table(view))
		}
		t.Logf("view %s: %d rows", view, s.Table(view).Total)
	}

	// Every binding as the view builds it once the cluster roles are listed: kind's bindings all
	// name cluster roles that exist, and system:node has no subjects on purpose.
	b, p, err := s.Kind("clusterrolebindings")
	if err != nil {
		t.Fatal(err)
	}
	objs, err := s.Objects(ctx, "clusterrolebindings", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
		"metadata": map[string]any{"name": "cogpit-b-not-created"},
		"roleRef":  map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cogpit-b-no-such-role"},
		"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": "default", "namespace": "cogpit-b"}},
	}}
	rows := s.BuildRows(ctx, b, p, append(append([]*unstructured.Unstructured(nil), objs...), missing))
	signals, bindings := p.ColumnIndex(model.SignalsColumnID), p.ColumnIndex("bindings")
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Ref.Name] = true
		text := r.Cells[signals].Text
		switch r.Ref.Name {
		case "cogpit-b-not-created":
			if text != "Cluster role cogpit-b-no-such-role not found: grants nothing" {
				t.Errorf("a binding to a cluster role that does not exist reads %q", text)
			}
		case "cluster-admin":
			if text != "" || r.Cells[bindings].Text != "system:masters" {
				t.Errorf("cluster-admin: signal %q, bindings %q", text, r.Cells[bindings].Text)
			}
		default:
			// Only the bindings kind and kubeadm create: other tests on the shared cluster may
			// leave their own.
			own := strings.HasPrefix(r.Ref.Name, "system:") || strings.HasPrefix(r.Ref.Name, "kubeadm:") ||
				r.Ref.Name == "kindnet" || r.Ref.Name == "local-path-provisioner-bind"
			if own && text != "" {
				t.Errorf("%s warns: %q", r.Ref.Name, text)
			}
		}
	}
	if !seen["system:node"] || !seen["kubeadm:node-proxier"] {
		t.Fatalf("kind's bindings are missing: %v", seen)
	}

	proxier, err := s.DetailWait(ctx, model.ObjectRef{Kind: "clusterrolebindings", Name: "kubeadm:node-proxier"})
	if err != nil {
		t.Fatal(err)
	}
	var role *model.ObjectRef
	var subjects *model.DetailTable
	for _, sec := range proxier.Sections {
		for _, it := range sec.Items {
			if it.Name == "Role" {
				role = it.Link
			}
		}
		if sec.Title == "Subjects" {
			subjects = sec.Table
		}
	}
	if role == nil || *role != (model.ObjectRef{Kind: "clusterroles", Name: "system:node-proxier"}) {
		t.Errorf("role link %+v", role)
	}
	if subjects == nil || subjects.Link(0, 1) == nil || *subjects.Link(0, 1) != (model.ObjectRef{Kind: "serviceaccounts", Namespace: "kube-system", Name: "kube-proxy"}) {
		t.Errorf("subjects %+v", subjects)
	}

	// The aggregated admin role: no warning, its aggregation rule and the rules the controller
	// filled in.
	admin, err := s.DetailWait(ctx, model.ObjectRef{Kind: "clusterroles", Name: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	var aggregation string
	rules := 0
	for _, sec := range admin.Sections {
		for _, it := range sec.Items {
			switch it.Name {
			case "Aggregation Rule":
				aggregation = it.Value
			case "Warning":
				t.Errorf("admin warns: %q", it.Value)
			}
		}
		if sec.Title == "Rules" && sec.Table != nil {
			rules = len(sec.Table.Rows)
		}
	}
	if !strings.Contains(aggregation, "aggregate-to-admin=true") || rules == 0 {
		t.Errorf("admin: aggregation %q, %d rules", aggregation, rules)
	}

	// A service account kind creates: no automount setting of its own.
	coredns, err := s.DetailWait(ctx, model.ObjectRef{Kind: "serviceaccounts", Namespace: "kube-system", Name: "coredns"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range coredns.Sections[0].Items {
		if it.Name == "Automount Token" {
			found = it.Value == "true (default)"
		}
	}
	if !found {
		t.Errorf("coredns service account: %+v", coredns.Sections[0].Items)
	}
}
