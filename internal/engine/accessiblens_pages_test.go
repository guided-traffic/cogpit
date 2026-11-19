package engine

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// podsHandles counts the handles on pods in all namespaces and on pods of single namespaces.
func podsHandles(s *Session) (clusterWide, namespaced int) {
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR != testkit.PodsGVR {
			continue
		}
		if st.Scope.Namespace == "" {
			clusterWide += st.Handles
		} else {
			namespaced += st.Handles
		}
	}
	return clusterWide, namespaced
}

// For a user whose RoleBinding is in the context's namespace alone, the Overviews read their kinds
// in all namespaces first and are refused; no view has met the refusals of the Deployments and
// Replica Sets. An agent's read of either Overview (no page open) waits for the plans, follows the
// refusals and answers from team-a at once, instead of waiting 15 seconds on refused reads; the
// open page reads them in team-a too (the refusals count whoever met them, cache.OnRefused), rather
// than keeping the refused watches.
func TestPagesFollowARefusal(t *testing.T) {
	src, rev := contextOnly(t)
	s := contextOnlySession(t, src, rev, nil)
	s.UpdateUI(func(u *UIState) { u.View = "services" }) // no view of pods, deployments or replica sets
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	counted := func(m *WorkloadsOverviewModel) bool {
		n := 0
		for _, k := range m.Kinds {
			if k.Error != "" {
				return false
			}
			if (k.Kind == "deployments" || k.Kind == "replicasets") && k.Count == 1 {
				n++
			}
		}
		return m.Synced && n == 2
	}
	m, _, err := s.WorkloadsOverview(ctx, "", nil, false)
	if err != nil || !counted(m) {
		t.Fatalf("an agent's Workloads Overview: %+v, %v", m, err)
	}
	cm, err := s.ClusterOverview(ctx, "", nil, false)
	if err != nil || !cm.Synced || cm.Error != "" {
		t.Fatalf("an agent's cluster Overview: %+v, %v", cm, err)
	}

	s.UpdateUI(func(u *UIState) { u.View, u.Sort = WorkloadsOverview, s.ViewSort(WorkloadsOverview) })
	waitFor(t, "the page's kinds read in team-a, counted without an error", func() bool {
		m, _, err := s.WorkloadsOverview(ctx, "", nil, false)
		return err == nil && counted(m)
	})
	waitFor(t, "no read in all namespaces left", func() bool {
		for _, st := range s.Backend().Cache.Stats() {
			// Events and namespaces are not refused in all namespaces here (the source serves them).
			if st.Scope.Namespace == "" && st.Scope.GVR.Resource != "namespaces" && st.Scope.GVR.Resource != "events" {
				return false
			}
		}
		return true
	})
	for _, res := range []string{"deployments", "replicasets"} {
		if calls := callsOf(src, res); calls["list "+res+".apps"] > 2 {
			t.Errorf("%s calls %v: the refused read in all namespaces was tried again", res, calls)
		}
	}
}

// A page opened while the access reviews are still out holds nothing of its kinds, as the Flux and
// Argo CD dashboards do (A98, owner 2026-10-14 evening; before, it read them in all namespaces
// first and was refused). Once the reviews say the user may not list pods there, the Workloads
// Overview and the cluster Overview read pods namespace by namespace, as the Pods view and the
// drawer do: no cluster-wide pods handle at any time, and the Workloads Overview counts the
// accessible namespaces' pods without an error.
func TestPagesFollowTheAccessReviews(t *testing.T) {
	for _, page := range []string{WorkloadsOverview, ClusterOverview} {
		t.Run(page, func(t *testing.T) {
			_, src, rev := namespacedOnly(t)
			rev.set(func(f *fakeReviewer) { f.gate = make(chan struct{}) })
			s := nsSession(t, src, rev, "team-a", "team-a", "team-b")
			connectTest(t, s)
			s.UpdateUI(func(u *UIState) { u.View, u.Sort = page, s.ViewSort(page) })
			waitFor(t, "the page waiting for the reviews", func() bool {
				s.state.mu.Lock()
				defer s.state.mu.Unlock()
				return (page == WorkloadsOverview && s.state.page != nil) || (page == ClusterOverview && s.state.cluster != nil)
			})
			if all, ns := podsHandles(s); all != 0 || ns != 0 {
				t.Fatalf("while the reviews are out the page holds pods: %d cluster-wide, %d per namespace", all, ns)
			}

			rev.set(func(f *fakeReviewer) { close(f.gate); f.gate = nil })
			waitFor(t, "the page reading pods namespace by namespace", func() bool {
				all, ns := podsHandles(s)
				return all == 0 && ns > 0
			})
			if page != WorkloadsOverview {
				return
			}
			waitFor(t, "the accessible namespaces' pods counted", func() bool {
				m, _, err := s.WorkloadsOverview(context.Background(), "", nil, false)
				if err != nil {
					t.Fatal(err)
				}
				return m.Synced && len(m.Kinds) > 0 && m.Kinds[0].Kind == "pods" && m.Kinds[0].Count == 2 && m.Kinds[0].Error == ""
			})
		})
	}
}
