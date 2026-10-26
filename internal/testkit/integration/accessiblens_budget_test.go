package integration_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// podReads are the server's reads of pods, by "verb pods [in namespace]".
func podReads(srv *testkit.FakeAPIServer) map[string]int64 {
	out := map[string]int64{}
	for k, v := range srv.RequestsIn() {
		if strings.Contains(k, " pods") {
			out[k] = v
		}
	}
	return out
}

// connectNamed connects an engine to a fake API server that enforces access's rules, with the
// context's default namespace and the cluster's accessible namespaces set before connecting.
func connectNamed(ctx context.Context, t *testing.T, access *testkit.FakeAccess, namespace string, accessible []string) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "budget-token", Access: access})
	t.Cleanup(srv.Close)
	for ns, n := range map[string]int{"team-a": 3, "team-b": 2, "team-c": 1} {
		for i := range n {
			srv.Set(testkit.PodsGVR, testkit.Pod(ns, fmt.Sprintf("web-%d", i)))
		}
	}
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", namespace)}},
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
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	return s, srv
}

// waitPods waits for the Pods view's snapshot to be synced with n rows.
func waitPods(ctx context.Context, t *testing.T, s *engine.Session, n int) *engine.TableSnapshot {
	t.Helper()
	var ts *engine.TableSnapshot
	if !testkit.WaitFor(ctx, func() bool {
		ts = s.Table("pods")
		return ts != nil && ts.Synced && len(ts.Rows) == n
	}) {
		t.Fatalf("the Pods view never showed %d rows: %+v", n, ts)
	}
	return ts
}

// TestBudgetAccessibleNamespaces measures, on a real client-go stack against a fake API server
// that enforces RBAC, what the Pods view costs a user who may list pods only in team-a and team-b
// and named them as accessible namespaces: one streaming watch per namespace, no cluster-wide list
// or watch ever tried (the access reviews said no before the view read anything), and nothing
// while idle. A cluster-admin keeps the one cluster-wide watch and the one rules review of
// connecting: the namespaced watches cost them nothing.
func TestBudgetAccessibleNamespaces(t *testing.T) {
	podsRule := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: []string{"pods"}}

	t.Run("a user limited to two namespaces", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {podsRule}, "team-b": {podsRule}}}
		s, srv := connectNamed(ctx, t, access, "team-a", []string{"team-a", "team-b"})
		ts := waitPods(ctx, t, s, 5)
		if !slices.Equal(ts.Watched, []string{"team-a", "team-b"}) || len(ts.Denied) != 0 || ts.Error != "" {
			t.Errorf("watched %q, denied %q, error %q", ts.Watched, ts.Denied, ts.Error)
		}
		if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatchesBy()["pods"] == 2 }) {
			t.Fatalf("open pod watches: %v, want 2", srv.OpenWatchesBy())
		}
		want := map[string]int64{"list pods in team-a": 1, "watch pods in team-a": 1, "list pods in team-b": 1, "watch pods in team-b": 1}
		settled := podReads(srv)
		if !maps.Equal(settled, want) {
			t.Errorf("pod reads %v, want %v (one list and one watch per namespace, nothing in all namespaces)", settled, want)
		}
		conn := s.Backend().Conn
		r, a := reviews(conn.Counter().Snapshot())
		t.Logf("pod reads %v; access reviews: %d rules, %d access", settled, r, a)
		if r != 2 || a != 1 {
			t.Errorf("reviews: %d rules (team-a, team-b), %d access (list pods in all namespaces); want 2 and 1", r, a)
		}

		var reads int64
		for _, n := range settled {
			reads += n
		}
		how := "the engine against the fake API server, which enforces RBAC and counts reads per namespace"
		scorecard.Record(t, scorecard.Measurement{Budget: "P15", Metric: "pod reads of a user limited to two named namespaces (one list and one watch each, none cluster-wide)",
			Value: float64(reads), Unit: "requests", Limit: 4, How: how})

		total := settledTotal(ctx, t, conn.Counter().Snapshot)
		time.Sleep(idle)
		scorecard.Record(t, scorecard.Measurement{Budget: "P15", Metric: fmt.Sprintf("requests of that user's open Pods view idle for %s", idle),
			Value: float64(conn.Counter().Snapshot().Total - total), Unit: "requests", How: how})
		if after := conn.Counter().Snapshot(); after.Total != total || !maps.Equal(podReads(srv), want) {
			t.Errorf("idle cost %d requests, pod reads %v", after.Total-total, podReads(srv))
		}
	})

	t.Run("a cluster-admin", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		all := testkit.FakeRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}
		access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {all}}}
		s, srv := connectNamed(ctx, t, access, "", nil)
		waitPods(ctx, t, s, 6)
		if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatchesBy()["pods"] == 1 }) {
			t.Fatalf("open pod watches: %v, want 1", srv.OpenWatchesBy())
		}
		if got, want := podReads(srv), map[string]int64{"list pods": 1, "watch pods": 1}; !maps.Equal(got, want) {
			t.Errorf("pod reads %v, want %v", got, want)
		}
		if r, a := reviews(s.Backend().Conn.Counter().Snapshot()); r != 1 || a != 0 {
			t.Errorf("reviews: %d rules, %d access; want the default namespace's rules alone, as before", r, a)
		}
	})
}
