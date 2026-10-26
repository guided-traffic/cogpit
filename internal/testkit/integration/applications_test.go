package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestBudgetApplicationsView measures the Applications view against a real client-go stack: no
// workload is watched before the view opens; opening it costs one initial request per workload
// kind the cluster serves and nothing else (no request for the applications themselves, which no
// API server serves); its drawer costs no request.
func TestBudgetApplicationsView(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sts := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	jobs := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{
		Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
			testkit.FakeResource{GVR: sts, Kind: "StatefulSet", Namespaced: true},
			testkit.FakeResource{GVR: jobs, Kind: "Job", Namespaced: true}),
	})
	defer srv.Close()
	labels := map[string]any{"labels": map[string]any{kinds.LabelAppInstance: "storefront", kinds.LabelAppManagedBy: "Helm"}}
	srv.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", map[string]any{"metadata": labels}))
	srv.Set(sts, testkit.Object("apps/v1", "StatefulSet", "shop", "db", map[string]any{"metadata": labels}))
	srv.Set(jobs, testkit.Object("batch/v1", "Job", "shop", "seed", map[string]any{"metadata": labels}))

	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		SnapshotInterval: 10 * time.Millisecond,
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Backend().Profile(kinds.ApplicationsKey); !ok {
		t.Fatal("no Applications on a cluster that serves workloads")
	}
	workloadRequests := func() map[string]int64 {
		out := map[string]int64{}
		for k, n := range srv.Requests() {
			for _, r := range []string{"deployments", "statefulsets", "jobs", "applications"} {
				if strings.Contains(k, r) && n > 0 {
					out[k] = n
				}
			}
		}
		return out
	}
	if before := workloadRequests(); len(before) != 0 {
		t.Fatalf("workload requests before the view opened: %v", before)
	}

	raw, _ := json.Marshal(map[string]any{"view": kinds.ApplicationsKey})
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		ts := s.Table(kinds.ApplicationsKey)
		return ts != nil && ts.Synced && len(ts.Rows) == 1
	}) {
		t.Fatalf("no applications view: %+v", s.Table(kinds.ApplicationsKey))
	}
	m, err := s.DetailWait(ctx, model.ObjectRef{Context: "fake", Kind: kinds.ApplicationsKey, Namespace: "shop", Name: "storefront"})
	if err != nil || m.Missing {
		t.Fatalf("drawer %+v, %v", m, err)
	}
	req := workloadRequests()
	for _, r := range []string{"deployments.apps", "statefulsets.apps", "jobs.batch"} {
		if req["list "+r] != 1 || req["watch "+r] != 1 || len(req) != 6 {
			t.Errorf("%s: want one list and one watch: %v", r, req)
		}
	}
	for k := range req {
		if strings.Contains(k, "applications") {
			t.Errorf("a request for the applications: %s", k)
		}
	}
	t.Logf("requests: %v", req)
}
