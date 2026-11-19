package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestBudgetRefusedClusterScopedList measures, against a fake API server that enforces RBAC, a list
// of a cluster-scoped kind the user may not list (the window's namespace filter reads the
// Namespaces at connect, a namespace-limited user may not): the refusal is the answer at once, one
// list, not a wait of the read's 15 seconds while client-go's backoff lists again and again, and
// Connection Diagnostics shows the one refused line once.
func TestBudgetRefusedClusterScopedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	podsRule := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: []string{"pods"}}
	access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": {podsRule}}}
	s, srv := connectNamed(ctx, t, access, "team-a", nil)
	lists := func() int64 { return srv.RequestsIn()["list namespaces"] }

	start := time.Now()
	_, err := s.Dispatch(ctx, engine.Invocation{ID: "resources.list", Params: []byte(`{"kind":"namespaces","limit":500}`), Origin: engine.OriginGUI})
	took := time.Since(start)
	if !apierrors.IsForbidden(err) {
		t.Fatalf("resources.list of the namespaces: %v, want the API server's refusal", err)
	}
	if took > 5*time.Second || lists() != 1 {
		t.Errorf("the refusal took %s and %d lists, want it at once after one list", took.Round(time.Millisecond), lists())
	}
	time.Sleep(3 * time.Second) // client-go's backoff would have listed again within it
	if n := lists(); n != 1 {
		t.Errorf("the refused namespaces were listed %d times, want once", n)
	}
	r, err := s.DiagnosticsReportOf("fake")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range r.Log {
		if l.Kind == "refused" && strings.Contains(l.What, "namespaces") && l.Count != 1 {
			t.Errorf("Connection Diagnostics: %q %d times, want once", l.What, l.Count)
		}
	}
}
