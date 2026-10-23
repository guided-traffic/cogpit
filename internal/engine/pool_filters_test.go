package engine

import (
	"slices"
	"testing"
	"time"
)

// TestNoFilterCrossesAClusterSwitch (WF20): the namespaces, API groups, status filter, search and
// its toggles of the cluster shown so far never come along when another cluster is shown, by a
// connect or by showing a connected one; a cluster coming back with a tab of its own gets that
// tab's filters again.
func TestNoFilterCrossesAClusterSwitch(t *testing.T) {
	filtered := func(u *UIState) {
		u.Namespaces, u.Groups = []string{"walk-test"}, []string{"apps"}
		u.Filter, u.FilterOptions = "web", FilterOptions{Regex: true}
		u.SetStates([]string{"Running"})
	}
	clean := func(t *testing.T, u UIState, want string) {
		t.Helper()
		if u.Context != want {
			t.Fatalf("shown %q, want %q", u.Context, want)
		}
		if len(u.Namespaces) != 0 || len(u.Groups) != 0 || u.Filter != "" || u.FilterOptions != (FilterOptions{}) || len(u.States) != 0 {
			t.Errorf("%s came with the other cluster's filters: namespaces %q groups %q filter %q options %+v states %q",
				want, u.Namespaces, u.Groups, u.Filter, u.FilterOptions, u.States)
		}
		if i := slices.IndexFunc(u.Tabs, func(tb ViewTab) bool { return tb.ID == u.ActiveTab }); i >= 0 && len(u.Tabs[i].Namespaces) != 0 {
			t.Errorf("%s's tab took the namespaces %q", want, u.Tabs[i].Namespaces)
		}
	}

	t.Run("preview tab", func(t *testing.T) {
		s, _ := poolSession(t, time.Minute, "kind", "stage")
		mustConnect(t, s, "kind")
		s.UpdateUI(filtered)
		mustConnect(t, s, "stage") // a first connect: stage's own preview tab (D-117)
		clean(t, s.UIState(), "stage")
		s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"monitoring"} })
		mustConnect(t, s, "kind") // shown again from the pool (showConnected): its preview tab's filters
		u := s.UIState()
		if u.Context != "kind" || !slices.Equal(u.Namespaces, []string{"walk-test"}) || u.Filter != "web" {
			t.Errorf("kind's preview tab did not bring its filters back: %q %q %q", u.Context, u.Namespaces, u.Filter)
		}
		mustConnect(t, s, "stage")
		if u := s.UIState(); u.Context != "stage" || !slices.Equal(u.Namespaces, []string{"monitoring"}) || u.Filter != "" {
			t.Errorf("stage's preview tab: %q %q %q", u.Context, u.Namespaces, u.Filter)
		}
	})

	t.Run("fixed tab", func(t *testing.T) {
		s, _ := poolSession(t, time.Minute, "kind", "stage")
		mustConnect(t, s, "kind")
		s.OpenTab("pods", true)
		s.UpdateUI(filtered)
		mustConnect(t, s, "stage")
		clean(t, s.UIState(), "stage")
		if err := s.Show("kind"); err != nil {
			t.Fatal(err)
		}
		u := s.UIState()
		if u.Context != "kind" || !slices.Equal(u.Namespaces, []string{"walk-test"}) || u.Filter != "web" {
			t.Errorf("kind's own tab did not bring its filters back: %q %q %q", u.Context, u.Namespaces, u.Filter)
		}
		if err := s.Show("stage"); err != nil {
			t.Fatal(err)
		}
		clean(t, s.UIState(), "stage")
	})
}
