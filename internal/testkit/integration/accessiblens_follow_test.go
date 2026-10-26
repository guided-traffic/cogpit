package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// A namespace-limited user against a fake API server that enforces RBAC, measured at the HTTP
// level (G3, G12; architecture §5 and §9): a user whose one RoleBinding is in the context's
// namespace team-a, and a cluster-admin, through the same steps: the Pods view with its CPU and
// Memory, an agent's object_get of a Deployment, resources_list of the Deployments, metrics.get of
// every pod. The limited user's reads end in team-a after one refused read per kind in all
// namespaces, and the pods' metrics are listed in team-a alone; the cluster-admin's counts are the
// ones of before: one read per kind in all namespaces and one list of every pod's metrics.

var podMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}

func podMetrics(ns, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetrics",
		"metadata":  map[string]any{"namespace": ns, "name": name},
		"timestamp": time.Now().UTC().Format(time.RFC3339), "window": "15s",
		"containers": []any{map[string]any{"name": "app", "usage": map[string]any{"cpu": "250m", "memory": "64Mi"}}},
	}}
}

// connectLimited connects an engine to a fake API server that serves pods, deployments, events and
// the Metrics API, enforcing access's rules, with team-a as the context's namespace. team-a holds
// three pods and the Deployment web, team-b two pods; every pod is measured. accessible, when
// given, are the cluster's accessible namespaces (Cluster Settings), set before connecting.
func connectLimited(ctx context.Context, t *testing.T, access *testkit.FakeAccess, accessible ...string) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.EventsGVR, Kind: "Event", Namespaced: true},
		testkit.FakeResource{GVR: podMetricsGVR, Kind: "PodMetrics", Namespaced: true})
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "limited-token", Access: access, Resources: resources})
	t.Cleanup(srv.Close)
	for ns, n := range map[string]int{"team-a": 3, "team-b": 2} {
		for i := range n {
			name := fmt.Sprintf("web-%d", i)
			srv.Set(testkit.PodsGVR, testkit.Pod(ns, name))
			srv.Set(podMetricsGVR, podMetrics(ns, name))
		}
	}
	srv.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "team-a", "web", nil))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if len(accessible) > 0 {
		if _, err := s.ChangeClusterSettings("fake", func(c *engine.ClusterSettings) error { c.AccessibleNamespaces = accessible; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	decideIntegrations(t, s, "fake") // its metrics read (D-82)
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	return s, srv
}

// readsOf are the server's reads of one resource, by "verb resource [in namespace]", refused ones
// included.
func readsOf(srv *testkit.FakeAPIServer, resource string) map[string]int64 {
	out := map[string]int64{}
	for k, v := range srv.RequestsIn() {
		if verb, rest, _ := strings.Cut(k, " "); verb != "" && (rest == resource || strings.HasPrefix(rest, resource+" in ")) {
			out[k] = v
		}
	}
	return out
}

func agentDispatch(ctx context.Context, t *testing.T, s *engine.Session, id string, params any) any {
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

// limitedSteps runs the user's steps: the Pods view until every row shows its CPU, an agent's
// object_get of the Deployment, resources_list of the Deployments, metrics.get of every pod.
func limitedSteps(ctx context.Context, t *testing.T, s *engine.Session, rows int) {
	t.Helper()
	if !testkit.WaitFor(ctx, func() bool {
		ts := s.Table("pods")
		if ts == nil || !ts.Synced || len(ts.Rows) != rows {
			return false
		}
		ci := -1
		for i, c := range ts.Columns {
			if c.ID == model.ColumnCPU {
				ci = i
			}
		}
		for _, r := range ts.Rows {
			if ci < 0 || ci >= len(r.Cells) || r.Cells[ci].Text == "" {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("the Pods view never showed %d rows with their CPU: %+v", rows, s.Table("pods"))
	}
	start := time.Now()
	ref := model.ObjectRef{Kind: "deployments", Namespace: "team-a", Name: "web"}
	m, ok := agentDispatch(ctx, t, s, "object.get", map[string]any{"ref": ref}).(*engine.DetailModel)
	if !ok || m.Missing || m.Loading {
		t.Fatalf("object.get: %+v", m)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("object.get took %s", d)
	}
	list, ok := agentDispatch(ctx, t, s, "resources.list", map[string]any{"kind": "deployments"}).(commands.ListResult)
	if !ok || list.Total != 1 {
		t.Fatalf("resources.list: %+v", list)
	}
	res, ok := agentDispatch(ctx, t, s, "metrics.get", map[string]any{"kind": "pods"}).(*engine.MetricsResult)
	if !ok || res.Status != "available" || res.Total != rows {
		t.Fatalf("metrics.get: %+v", res)
	}
}

// The Releases view of a user who may read Secrets and ConfigMaps in team-a alone (a RoleBinding
// there): the release records are read in all namespaces first, refused, then in team-a, and the
// view lists team-a's release without an error. Before (2026-10-13) the releases were left out of
// the namespace plans: the view kept the refused watches of both drivers and showed no release.
func TestHelmReleasesOfANamespaceLimitedUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rule := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: []string{"secrets", "configmaps", "pods"}}
	access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {rule}}}
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true},
		testkit.FakeResource{GVR: configMapsGVR, Kind: "ConfigMap", Namespaced: true})
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "limited-token", Access: access, Resources: resources})
	t.Cleanup(srv.Close)
	srv.Set(testkit.SecretsGVR, helmSecret(t, "team-a", "web", 1, "deployed"))
	srv.Set(testkit.SecretsGVR, helmSecret(t, "team-b", "db", 1, "deployed"))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *engine.UIState) { u.View = helm.ReleaseKind })
	var ts *engine.TableSnapshot
	if !testkit.WaitFor(ctx, func() bool {
		ts = s.Table(helm.ReleaseKind)
		return ts != nil && ts.Synced && len(ts.Rows) == 1 && ts.Error == ""
	}) {
		t.Fatalf("the Releases view never listed team-a's release: %+v (reads %v)", ts, srv.RequestsIn())
	}
	if r := ts.Rows[0].Ref; r.Namespace != "team-a" || r.Name != "web" {
		t.Errorf("row %v", r)
	}
	for _, res := range []string{"secrets with owner=helm", "configmaps with owner=helm"} {
		reads := readsOf(srv, res)
		wide := reads["list "+res]
		if wide > 2 || reads["list "+res+" in team-a"] != 1 || reads["watch "+res+" in team-a"] != 1 || len(reads) > 3 {
			t.Errorf("%s: reads %v, want at most two refused lists in all namespaces, then team-a's list and watch", res, reads)
		}
	}
}

// Prometheus in the namespace of a user whose one RoleBinding is there (team-a), on a cluster
// without a Metrics Server: detection reads the cached Services, which are refused in all
// namespaces and then read in team-a, and finds the Prometheus there at once. Before (2026-10-13)
// the refused read waited its 8 seconds and failed, and detection was not tried again for two
// minutes.
func TestPrometheusDetectionFollowsARefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rule := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: []string{"pods", "services", "services/proxy"}}
	access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {rule}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "limited-token", Access: access})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "a"))
	srv.Set(testkit.ServicesGVR, testkit.Service("team-a", "prometheus-server", "10.0.0.5"))
	srv.SetProxy("team-a", "prometheus-server:80", http.HandlerFunc(promHandler))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, "fake") // Prometheus looked for (D-82)
	start := time.Now()
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	// The Pods list asks Prometheus for its pods' usage once it has found it (the Metrics Server is
	// not served): a probe, then the queries, through team-a's service proxy.
	if !testkit.WaitFor(ctx, func() bool { return srv.Requests()["get services/proxy"] >= 2 }) {
		t.Fatalf("Prometheus never asked; reads %v", srv.RequestsIn())
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("Prometheus found after %s", d)
	}
	reads := readsOf(srv, "services")
	if wide := reads["list services"]; wide > 2 || reads["list services in team-a"] != 1 || reads["watch services in team-a"] != 1 {
		t.Errorf("service reads %v, want at most two refused lists in all namespaces, then team-a's list and watch", reads)
	}
}

func TestBudgetNamespaceLimitedUser(t *testing.T) {
	t.Run("a RoleBinding in the context's namespace alone", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		rule := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps", "metrics.k8s.io"},
			Resources: []string{"pods", "deployments", "events"}}
		access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {rule}}}
		s, srv := connectLimited(ctx, t, access)
		limitedSteps(ctx, t, s, 3)

		// The first read of each kind in all namespaces is refused (a list, which client-go may
		// retry once before the engine reads the kind namespace by namespace: one or two requests),
		// then team-a's list and watch alone; the pods' metrics are listed in team-a alone, never in
		// all namespaces.
		for _, res := range []string{"pods", "deployments.apps", "events"} {
			reads := readsOf(srv, res)
			wide := reads["list "+res]
			if wide < 1 || wide > 2 || reads["list "+res+" in team-a"] != 1 || reads["watch "+res+" in team-a"] != 1 || len(reads) > 3 {
				t.Errorf("%s: reads %v, want one or two refused lists in all namespaces, then team-a's list and watch", res, reads)
			}
		}
		if got, want := readsOf(srv, "pods.metrics.k8s.io"), map[string]int64{"list pods.metrics.k8s.io in team-a": 1}; !maps.Equal(got, want) {
			t.Errorf("pod metrics: reads %v, want %v", got, want)
		}
		var metricsLists int64
		for _, n := range readsOf(srv, "pods.metrics.k8s.io") {
			metricsLists += n
		}
		scorecard.Record(t, scorecard.Measurement{Budget: "P12", Metric: "pod metrics lists of a namespace-limited user's Pods view in its first list interval (in its namespace, none in all)",
			Value: float64(metricsLists), Unit: "requests", Limit: 1,
			How: "the engine and the metrics hub against the fake API server, which enforces RBAC and counts reads per namespace"})
		total := s.Backend().Conn.Counter().Snapshot().Total
		time.Sleep(idle)
		if after := s.Backend().Conn.Counter().Snapshot().Total; after != total {
			t.Errorf("idle cost %d requests: %v", after-total, srv.RequestsIn())
		}
	})

	// D-39 (owner, 2026-10-14): a user who may list pods in team-a and team-b but their metrics in
	// team-a only. team-b's refused list of pods.metrics.k8s.io affects team-b alone: its rows stay
	// empty and metrics.get says forbidden for team-b, team-a's rows have their values, the Metrics
	// Server's status stays available, and nothing looks for a Prometheus in its place (no read of
	// services).
	t.Run("pods in two namespaces, their metrics in one", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		pods := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps"}, Resources: []string{"pods", "deployments", "events"}}
		podMetricsRule := testkit.FakeRule{Verbs: []string{"get", "list"}, APIGroups: []string{"metrics.k8s.io"}, Resources: []string{"pods"}}
		access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {pods, podMetricsRule}, "team-b": {pods}}}
		s, srv := connectLimited(ctx, t, access, "team-a", "team-b")
		cpu := func(ts *engine.TableSnapshot) map[string]string {
			ci := slices.IndexFunc(ts.Columns, func(c model.Column) bool { return c.ID == model.ColumnCPU })
			out := map[string]string{}
			for _, r := range ts.Rows {
				if ci >= 0 && ci < len(r.Cells) {
					out[r.Ref.Namespace+"/"+r.Ref.Name] = r.Cells[ci].Text
				}
			}
			return out
		}
		var got map[string]string
		if !testkit.WaitFor(ctx, func() bool {
			ts := s.Table("pods")
			if ts == nil || !ts.Synced || len(ts.Rows) != 5 {
				return false
			}
			got = cpu(ts)
			return got["team-a/web-0"] != "" && got["team-a/web-1"] != "" && got["team-a/web-2"] != "" &&
				readsOf(srv, "pods.metrics.k8s.io")["list pods.metrics.k8s.io in team-b"] == 1
		}) {
			t.Fatalf("the Pods view never showed team-a's CPU after team-b's refusal: %v, reads %v", got, srv.RequestsIn())
		}
		if got["team-b/web-0"] != "" || got["team-b/web-1"] != "" {
			t.Errorf("team-b's CPU %q %q, want empty", got["team-b/web-0"], got["team-b/web-1"])
		}
		inB, ok := agentDispatch(ctx, t, s, "metrics.get", map[string]any{"kind": "pods", "namespace": "team-b"}).(*engine.MetricsResult)
		if !ok || inB.Status != "forbidden" || inB.Total != 0 || !strings.Contains(inB.Message, "team-b") {
			t.Errorf("metrics.get in team-b: %+v", inB)
		}
		all, ok := agentDispatch(ctx, t, s, "metrics.get", map[string]any{"kind": "pods"}).(*engine.MetricsResult)
		if !ok || all.Status != "available" || all.Total != 3 || !slices.Equal(all.Forbidden, []string{"team-b"}) {
			t.Errorf("metrics.get of every pod: %+v", all)
		}
		for _, p := range all.Providers {
			if p.Provider == "metrics-server" && (p.Status != "available" || p.LastError != "") {
				t.Errorf("the Metrics Server's status %+v, want available without a failure", p)
			}
		}
		reads := readsOf(srv, "pods.metrics.k8s.io")
		t.Logf("pod metrics reads: %v", reads)
		if want := map[string]int64{"list pods.metrics.k8s.io in team-a": 1, "list pods.metrics.k8s.io in team-b": 1}; !maps.Equal(reads, want) {
			t.Errorf("pod metrics: reads %v, want %v (the refusal kept for the TTL, as an answer)", reads, want)
		}
		if svc := readsOf(srv, "services"); len(svc) != 0 {
			t.Errorf("services read %v: something looked for a Prometheus in the Metrics Server's place", svc)
		}
	})

	t.Run("a cluster-admin", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		all := testkit.FakeRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}
		s, srv := connectLimited(ctx, t, &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {all}}})
		limitedSteps(ctx, t, s, 5)
		for res, want := range map[string]map[string]int64{
			"pods":                {"list pods": 1, "watch pods": 1},
			"deployments.apps":    {"list deployments.apps": 1, "watch deployments.apps": 1},
			"events":              {"list events": 1, "watch events": 1},
			"pods.metrics.k8s.io": {"list pods.metrics.k8s.io": 1},
		} {
			if got := readsOf(srv, res); !maps.Equal(got, want) {
				t.Errorf("%s: reads %v, want %v (as before)", res, got, want)
			}
		}
	})
}
