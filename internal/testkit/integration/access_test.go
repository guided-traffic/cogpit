package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The request keys of the access reviews in cluster.RequestStats.ByVerbResource.
const (
	rulesReviews  = "create selfsubjectrulesreviews.authorization.k8s.io"
	accessReviews = "create selfsubjectaccessreviews.authorization.k8s.io"
)

func reviews(st cluster.RequestStats) (rules, access int64) {
	return st.ByVerbResource[rulesReviews], st.ByVerbResource[accessReviews]
}

// connectWithAccess connects an engine to a fake API server that answers access reviews from
// access, with namespace as the kubeconfig context's default namespace.
func connectWithAccess(ctx context.Context, t *testing.T, access *testkit.FakeAccess, namespace string, seed func(*testkit.FakeAPIServer)) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "access-token", Access: access})
	t.Cleanup(srv.Close)
	seed(srv)
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", namespace)}}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	return s, srv
}

// settledKinds asks kinds.list as the sidebar does (again after each answer) until every kind
// in want has a known answer, and returns the answers.
func settledKinds(ctx context.Context, t *testing.T, s *engine.Session, want ...string) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	if !testkit.WaitFor(ctx, func() bool {
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "kinds.list", Origin: engine.OriginGUI})
		if err != nil {
			t.Fatal(err)
		}
		clear(got)
		for _, k := range res.([]commands.KindInfo) {
			if k.Listable != nil && k.Group != model.LocalGroup { // Cogpit's own kinds are always listable
				got[k.Kind] = *k.Listable
			}
		}
		for _, k := range want {
			if _, ok := got[k]; !ok {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("kinds.list never answered for %v: %v", want, got)
	}
	return got
}

// TestBudgetAccessReviews measures what access reviews cost on a real client-go stack against
// the fake API server: connecting costs one rules review of the default namespace; the sidebar's
// answers cost the reviews of the namespaces the user can see and one access review (may the user
// list namespaces?); browsing, menus and the sidebar asking again cost nothing once answered; a
// refresh asks again. The client's counter and the server agree on every review.
func TestBudgetAccessReviews(t *testing.T) {
	podsAndNamespaces := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: []string{"pods", "namespaces"}}

	t.Run("a user limited to their namespace", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Like Kubernetes' view role bound in team-a: it names namespaces, which a RoleBinding
		// cannot grant; an access review tells.
		access := &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"team-a": {podsAndNamespaces}}}
		s, srv := connectWithAccess(ctx, t, access, "team-a", func(srv *testkit.FakeAPIServer) {
			for i := range 3 {
				srv.Set(testkit.PodsGVR, testkit.Pod("team-a", fmt.Sprintf("web-%d", i)))
			}
		})
		conn := s.Backend().Conn
		if !testkit.WaitFor(ctx, func() bool { r, _ := reviews(conn.Counter().Snapshot()); return r == 1 }) {
			t.Fatalf("connecting did not review the default namespace: %v", conn.Counter().Snapshot().ByVerbResource)
		}

		got := settledKinds(ctx, t, s, "pods", "deployments", "services", "namespaces")
		want := map[string]bool{"pods": true, "deployments": false, "services": false, "namespaces": false}
		if !maps.Equal(got, want) {
			t.Errorf("listable %v, want %v", got, want)
		}
		settled := conn.Counter().Snapshot()
		if r, a := reviews(settled); r != 1 || a != 1 {
			t.Errorf("reviews to settle the sidebar: %d rules, %d access; want 1 and 1", r, a)
		}

		// Browsing: the sidebar again, the pods' row menus, a denied delete; no review.
		ref := model.ObjectRef{Kind: "pods", Namespace: "team-a", Name: "web-1"}
		if !testkit.WaitFor(ctx, func() bool { _, err := s.Lookup(ref); return err == nil }) {
			t.Fatal("the pod never reached the cache")
		}
		// The pods' informer, started with the connect's view, watches after its list on a goroutine
		// of its own: its watch is the listing's cost, not browsing's, so browsing counts from there.
		if !testkit.WaitFor(ctx, func() bool {
			b := conn.Counter().Snapshot().ByVerbResource
			return b["watch pods"]+b["watchlist pods"] > 0
		}) {
			t.Fatalf("the pods' watch never came: %v", conn.Counter().Snapshot().ByVerbResource)
		}
		browsing := conn.Counter().Snapshot()
		for range 5 {
			settledKinds(ctx, t, s, "pods")
			for _, cmd := range s.Registry().Actions("pods") {
				_ = cmd.Enabled(s, cmd.ObjectParams(ref, "web"))
			}
		}
		del, _ := s.Registry().Get("object.delete")
		if err := del.Enabled(s, del.ObjectParams(ref, "")); !errors.Is(err, engine.ErrNotPermitted) ||
			err.Error() != "not permitted: you may not delete pods in team-a" {
			t.Errorf("Delete: %v, want not permitted", err)
		}
		// A discovery request still on its way from the connect is not browsing (see
		// TestBudgetWithoutAccessReviews), so discovery is left out of the count.
		if after := conn.Counter().Snapshot(); len(withoutDiscovery(diff(browsing, after))) > 0 {
			t.Errorf("browsing cost %d requests: %v", after.Total-browsing.Total, diff(browsing, after))
		}

		// A refresh asks again: the default namespace at once, the access review when the
		// sidebar asks.
		raw, _ := json.Marshal(struct{}{})
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.refreshAccess", Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatal(err)
		}
		if r := res.(commands.AccessResult); r.Context != "fake" || len(r.Namespaces) != 1 || r.Namespaces[0] != "team-a" {
			t.Errorf("refresh result %+v", r)
		}
		settledKinds(ctx, t, s, "pods", "deployments", "services", "namespaces")
		if r, a := reviews(conn.Counter().Snapshot()); r != 2 || a != 2 {
			t.Errorf("after the refresh: %d rules, %d access reviews; want 2 and 2", r, a)
		}
		if final := conn.Counter().Snapshot(); !reviewsAgree(final, srv) {
			t.Errorf("client counted %v, server served %v", final.ByVerbResource, srv.Requests())
		}
		t.Logf("requests: %v", conn.Counter().Snapshot().ByVerbResource)
	})

	t.Run("a user who may list namespaces", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Pods and namespaces everywhere, Services only in ns-2, Deployments nowhere.
		access := &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{
			"":     {podsAndNamespaces},
			"ns-2": {{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"services"}}},
		}}
		s, srv := connectWithAccess(ctx, t, access, "", func(srv *testkit.FakeAPIServer) {
			for i := range 4 {
				srv.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", fmt.Sprintf("ns-%d", i), nil))
			}
		})
		got := settledKinds(ctx, t, s, "pods", "deployments", "services", "namespaces")
		want := map[string]bool{"pods": true, "deployments": false, "services": true, "namespaces": true}
		if !maps.Equal(got, want) {
			t.Errorf("listable %v, want %v", got, want)
		}
		conn := s.Backend().Conn
		settled := conn.Counter().Snapshot()
		// default and ns-0 … ns-3; one access review for list namespaces.
		if r, a := reviews(settled); r != 5 || a != 1 {
			t.Errorf("reviews to settle the sidebar: %d rules, %d access; want 5 and 1", r, a)
		}
		for range 5 {
			settledKinds(ctx, t, s, "deployments")
		}
		// Discovery from the connect may still arrive (seen in CI, 2026-10-14); it is not asking.
		if after := conn.Counter().Snapshot(); len(withoutDiscovery(diff(settled, after))) > 0 {
			t.Errorf("asking again cost %d requests: %v", after.Total-settled.Total, diff(settled, after))
		}
		if !reviewsAgree(settled, srv) {
			t.Errorf("client counted %v, server served %v", settled.ByVerbResource, srv.Requests())
		}
		t.Logf("requests: %v", settled.ByVerbResource)
	})
}

// TestBudgetWithoutAccessReviews: a server that answers no access reviews (404) costs one failing
// rules review and at most a burst of failing access reviews (those in flight when the first
// fails) for a whole session of sidebar reloads and menus; every kind is shown and every command
// offered, and the API server decides.
func TestBudgetWithoutAccessReviews(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("default", "web"))
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	conn := s.Backend().Conn
	ref := model.ObjectRef{Kind: "pods", Namespace: "default", Name: "web"}
	if !testkit.WaitFor(ctx, func() bool {
		for _, cmd := range s.Registry().Actions("pods") {
			_ = cmd.Enabled(s, cmd.ObjectParams(ref, "web"))
		}
		_, err := s.Dispatch(ctx, engine.Invocation{ID: "kinds.list", Origin: engine.OriginGUI})
		_, a := reviews(conn.Counter().Snapshot())
		return err == nil && a >= 1
	}) {
		t.Fatalf("no failing access review: %v", conn.Counter().Snapshot().ByVerbResource)
	}
	time.Sleep(100 * time.Millisecond)
	failed := conn.Counter().Snapshot()
	for range 5 {
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "kinds.list", Origin: engine.OriginGUI})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range res.([]commands.KindInfo) {
			if k.Listable != nil && k.Group != model.LocalGroup {
				t.Errorf("%s: listable %v from failing reviews", k.Kind, *k.Listable)
			}
		}
		for _, cmd := range s.Registry().Actions("pods") {
			if err := cmd.Enabled(s, cmd.ObjectParams(ref, "web")); errors.Is(err, engine.ErrNotPermitted) {
				t.Errorf("%s greyed by failing reviews: %v", cmd.ID, err)
			}
		}
	}
	st := conn.Counter().Snapshot()
	if r, a := reviews(st); r != 1 || a > 4 {
		t.Errorf("reviews: %d rules, %d access; want 1 and at most the 4 in flight", r, a)
	}
	// Asking again costs nothing; a discovery request still on its way from the connect (seen on a
	// loaded machine) is not one of the questions, so discovery is left out of the count.
	if d := diff(failed, st); len(withoutDiscovery(d)) > 0 {
		t.Errorf("asking again after the failure cost requests: %v", d)
	}
	if !reviewsAgree(st, srv) {
		t.Errorf("client counted %v, server served %v", st.ByVerbResource, srv.Requests())
	}
	t.Logf("requests: %v", st.ByVerbResource)
}

// reviewsAgree reports whether the client's counter and the server count the same reviews.
func reviewsAgree(st cluster.RequestStats, srv *testkit.FakeAPIServer) bool {
	served := srv.Requests()
	return st.ByVerbResource[rulesReviews] == served[rulesReviews] && st.ByVerbResource[accessReviews] == served[accessReviews]
}

// withoutDiscovery is a request diff without the discovery verb.
func withoutDiscovery(d map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range d {
		if k != cluster.VerbDiscovery && v != 0 {
			out[k] = v
		}
	}
	return out
}
