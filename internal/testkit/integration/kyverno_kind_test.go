//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// kyvernoNamespace is the kind check's namespace (make kind-kyverno, hack/kind/kyverno).
const kyvernoNamespace = "cogpit-kyverno-a"

// TestKindKyverno checks the Kyverno integration (D-79) against a real Kyverno on the kind cluster
// (make kind-kyverno): an Audit ClusterPolicy's fail on Deployments made before and after it, an
// Enforce one's fail on a ConfigMap from before it (a background scan), a ValidatingPolicy's on a
// Service, an Audit fail on the Namespace (a cluster report). What Cogpit marks is what the
// reports say: the violations kyverno_violations lists are the fail and warn results of the
// reports, the lists mark those objects and no other, and the drawer says how each was found. It
// only reads.
func TestKindKyverno(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	served, err := conn.ServedResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	polr := schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	cpolr := schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "clusterpolicyreports"}
	if !served[polr] {
		t.Skip("the cluster serves no policy reports: run make kind-kyverno first")
	}
	if _, err := conn.Kube().CoreV1().Namespaces().Get(ctx, kyvernoNamespace, metav1.GetOptions{}); err != nil {
		t.Skipf("no %s: run make kind-kyverno first (%v)", kyvernoNamespace, err)
	}

	// What the reports say, read past Cogpit: every fail and warn of the check's objects.
	want := func() []string {
		var out []string
		read := func(list []map[string]any) {
			for _, r := range list {
				scope, _ := kinds.PolicyReportScope(r)
				if scope.Namespace != kyvernoNamespace && scope.Name != kyvernoNamespace {
					continue
				}
				for _, res := range kinds.PolicyReportResults(r, kinds.IsPolicyViolation) {
					out = append(out, scope.Kind+"/"+scope.Name+":"+res.Policy)
				}
			}
		}
		for _, gvr := range []schema.GroupVersionResource{polr, cpolr} {
			l, err := conn.Dynamic().Resource(gvr).List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			items := make([]map[string]any, len(l.Items))
			for i := range l.Items {
				items[i] = l.Items[i].Object
			}
			read(items)
		}
		sort.Strings(out)
		return out
	}
	expected := []string{
		"ConfigMap/legacy-config:cogpit-require-configmap-owner", "Deployment/late:cogpit-require-team-label",
		"Deployment/web:cogpit-require-team-label", "Namespace/" + kyvernoNamespace + ":cogpit-require-namespace-owner",
		"Service/plain:cogpit-service-team",
	}
	if !testkit.WaitFor(ctx, func() bool { return slices.Equal(want(), expected) }) {
		t.Fatalf("Kyverno's reports say %v, want %v (is make kind-kyverno's background scan done?)", want(), expected)
	}

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
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}
	if !testkit.WaitFor(ctx, func() bool {
		for _, st := range s.IntegrationStatuses(contextName) {
			if st.Name == engine.IntegrationKyverno {
				return st.State == engine.IntegrationFound
			}
		}
		return false
	}) {
		t.Fatalf("Kyverno not found: %+v", s.IntegrationStatuses(contextName))
	}

	// Agents: the violations are the reports' fail and warn results.
	res := dispatch("kyverno.violations", map[string]any{"namespaces": []string{kyvernoNamespace}}).(commands.KyvernoViolationsResult)
	var got []string
	byObject := map[string]engine.PolicyViolation{}
	for _, v := range res.Violations {
		if v.Object.Namespace != kyvernoNamespace && v.Object.Name != kyvernoNamespace {
			continue
		}
		key := map[string]string{"configmaps": "ConfigMap", "deployments": "Deployment", "namespaces": "Namespace", "services": "Service"}[v.Object.Kind]
		got = append(got, key+"/"+v.Object.Name+":"+v.Policy)
		byObject[v.Object.Name] = v
	}
	sort.Strings(got)
	if !slices.Equal(got, expected) {
		t.Errorf("kyverno_violations %v, want %v", got, expected)
	}
	if v := byObject["legacy-config"]; v.Process != "background scan" {
		t.Errorf("the Enforce policy's fail on an object from before it: %+v", v)
	}
	if v := byObject["plain"]; v.Source != "KyvernoValidatingPolicy" || v.PolicyRef() == nil {
		t.Errorf("the ValidatingPolicy's fail: %+v", v)
	}

	// The window: the lists mark exactly those objects.
	dispatch("ui.setNamespace", map[string]any{"namespaces": []string{kyvernoNamespace}})
	for view, marked := range map[string][]string{
		"deployments": {"late", "web"}, "configmaps": {"legacy-config"}, "services": {"plain"}, "namespaces": {kyvernoNamespace},
	} {
		dispatch("ui.openView", map[string]any{"view": view})
		var tbl *engine.TableSnapshot
		if !testkit.WaitFor(ctx, func() bool {
			tbl = s.Table(view)
			return tbl != nil && tbl.Synced && slices.Equal(warned(tbl), marked)
		}) {
			t.Errorf("%s: marked %v, want %v", view, warned(tbl), marked)
		}
	}

	// The drawer: the Enforce policy's fail, found by a background scan.
	m, err := s.DetailWait(ctx, model.ObjectRef{Context: contextName, Kind: "configmaps", Namespace: kyvernoNamespace, Name: "legacy-config"})
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, sec := range m.Sections {
		if sec.Group == "Policy violations" && sec.Title == "cogpit-require-configmap-owner" {
			for _, it := range sec.Items {
				if it.Name == "Found by" {
					found = it.Value
				}
			}
		}
	}
	if found != "Background scan" {
		t.Errorf("legacy-config's drawer: %+v", m.Sections)
	}
	t.Logf("kyverno_violations: %d violations of %d objects", res.Total, res.Objects)
}

// warned returns the names of a table's rows whose Signals cell has Kyverno's signal, sorted.
func warned(tbl *engine.TableSnapshot) []string {
	if tbl == nil {
		return nil
	}
	ci := slices.IndexFunc(tbl.Columns, func(c model.Column) bool { return c.ID == model.SignalsColumnID })
	kyverno := func(s model.Signal) bool { return s.Source == model.SignalKyverno }
	var out []string
	for _, r := range tbl.Rows {
		if ci >= 0 && ci < len(r.Cells) && r.Cells[ci].Signals != nil && slices.ContainsFunc(*r.Cells[ci].Signals, kyverno) {
			out = append(out, r.Ref.Name)
		}
	}
	sort.Strings(out)
	return out
}
