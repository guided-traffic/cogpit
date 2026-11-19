package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestBudgetKyvernoRefusedClusterReports measures, at the HTTP level against a fake API server that
// enforces RBAC, what the Kyverno integration costs a user who may list the policy reports in all
// namespaces but not the cluster policy reports: the refused cluster reports are a state the
// integration shows, not a read client-go's backoff keeps sending while the cluster is shown.
func TestBudgetKyvernoRefusedClusterReports(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	polr := schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	cpolr := schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "clusterpolicyreports"}
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: polr, Kind: "PolicyReport", Namespaced: true},
		testkit.FakeResource{GVR: cpolr, Kind: "ClusterPolicyReport"})
	reads := testkit.FakeRule{Verbs: []string{"get", "list", "watch"},
		APIGroups: []string{"", "apps", "apiextensions.k8s.io", kinds.PolicyReportGroup},
		Resources: []string{"pods", "namespaces", "deployments", "customresourcedefinitions", "policyreports"}}
	access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {reads}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "kyverno-token", Access: access, Resources: resources})
	t.Cleanup(srv.Close)
	v := testkit.CRDVersion{Name: "v1alpha2", Served: true, Storage: true}
	srv.Set(testkit.CRDsGVR, testkit.CRD(kinds.PolicyReportGroup, "policyreports", "PolicyReport", true, v))
	srv.Set(testkit.CRDsGVR, testkit.CRD(kinds.PolicyReportGroup, "clusterpolicyreports", "ClusterPolicyReport", false, v))

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		ClusterSettings:  engine.NewClusterSettingsStore(t.TempDir()),
		SnapshotInterval: 10 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("fake", func(c *engine.ClusterSettings) error {
		c.SetIntegrations(engine.IntAll, engine.IntKyverno, 0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	status := func() engine.IntegrationStatus {
		for _, st := range s.IntegrationStatuses("fake") {
			if st.Name == engine.IntegrationKyverno {
				return st
			}
		}
		return engine.IntegrationStatus{}
	}
	if !testkit.WaitFor(ctx, func() bool {
		st := status()
		return st.State == engine.IntegrationFound && strings.Contains(st.Detail, kinds.ClusterPolicyReportsKey)
	}) {
		t.Fatalf("the status never named the refused cluster reports: %+v", status())
	}
	cluster := func() int64 {
		var n int64
		for k, c := range srv.RequestsIn() {
			if strings.Contains(k, " clusterpolicyreports.") {
				n += c
			}
		}
		return n
	}
	// client-go's backoff would retry a held refused scope within a second or two.
	const window = 3 * time.Second
	settled := func(want int64) {
		t.Helper()
		time.Sleep(window)
		if got := cluster(); got != want {
			t.Errorf("the refused cluster policy reports were read %d times, want %d (%v)", got, want, srv.RequestsIn())
		}
		if st := status(); st.State != engine.IntegrationFound || !strings.Contains(st.Detail, kinds.ClusterPolicyReportsKey) {
			t.Errorf("the status lost the refusal: %+v", st)
		}
	}
	settled(1)

	// Refreshing the access reviews reads them once again.
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.refreshAccess", Params: []byte(`{}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return cluster() == 2 }) {
		t.Fatalf("cluster.refreshAccess did not read the cluster policy reports again: %v", srv.RequestsIn())
	}
	settled(2)
}
