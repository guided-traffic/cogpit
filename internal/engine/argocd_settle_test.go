package engine

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestArgoDashboardWaitsForTheDefinitions: an agent's argocd.dashboard right after connecting,
// while the definitions are still being listed, waits for them (bounded) and then counts Argo CD's
// Applications; read before they are listed it says it is not synced, never that the cluster
// serves no Argo CD while synced (seen on a real cluster with many definitions: served false,
// synced true).
func TestArgoDashboardWaitsForTheDefinitions(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(crdsGVR, crd)
	}
	src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("argocd", "drift", "OutOfSync", "Healthy"))
	held, release := testkit.HoldList(src, crdsGVR)
	s := newCRDSession(t, held)
	t.Cleanup(release) // runs before the session closes

	// Bounded by the caller's context while the definitions are held: not synced.
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m, _, err := s.ArgoCDDashboard(short, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Served || m.Synced {
		t.Errorf("before the definitions are listed: served %v, synced %v (%s)", m.Served, m.Synced, m.Error)
	}

	// A read that starts before they are listed waits for them.
	type result struct {
		m   *ArgoCDDashboardModel
		err error
	}
	done := make(chan result, 1)
	go func() {
		m, _, err := s.ArgoCDDashboard(context.Background(), "", nil, false)
		done <- result{m, err}
	}()
	release()
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.m.Served || !r.m.Synced || len(r.m.OutOfSync) != 1 || r.m.OutOfSync[0].Name != "drift" {
		t.Errorf("after the definitions: %+v", r.m)
	}
}
