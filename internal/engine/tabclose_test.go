package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// Close Others keeps one of the tabs the bar shows and makes it the active one; Close All closes
// them all; a tab of a cluster the bar does not show stays, and cannot be the one kept (D-144).
func TestCloseTabs(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	s.PinTab("") //nolint:errcheck
	s.OpenTab("services", true)
	s.OpenTab("deployments", true)
	if err := s.Connect(ctx, "prod [prod.yaml]"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectTab(ctx, "tab-3"); err != nil {
		t.Fatal(err)
	}
	wantTabs(t, s.UIState(), "tab-1 pods@test", "tab-2 services@test", "tab-3 deployments@test *", "tab-4 pods@prod [prod.yaml] p")

	u, err := s.CloseTabs(ctx, "tab-2") // not the active one
	if err != nil {
		t.Fatal(err)
	}
	wantTabs(t, u, "tab-2 services@test *", "tab-4 pods@prod [prod.yaml] p")
	if u.View != "services" {
		t.Errorf("the tab kept shows %q", u.View)
	}
	if _, err := s.CloseTabs(ctx, "tab-4"); !errors.Is(err, ErrUnknownTab) {
		t.Errorf("keeping a tab the bar does not show: %v", err)
	}

	u, err = s.CloseTabs(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	wantTabs(t, u, "tab-4 pods@prod [prod.yaml] p")
	if u.View != "" || u.ActiveTab != "" {
		t.Errorf("every tab closed: view %q, active %q", u.View, u.ActiveTab)
	}
}

// The dock's Close Others and Close All close the tabs it shows but the one kept and a tab whose job
// still runs; another cluster's tabs stay; the tab kept, else a tab left, is the active one
// (D-144).
func TestCloseDockTabs(t *testing.T) {
	s, _ := poolSession(t, time.Hour, "alpha", "beta")
	mustConnect(t, s, "alpha")
	addDockTab(s, "terminal-1", "alpha")
	addDockTab(s, "audit-2", "")
	s.UpdateUI(func(u *UIState) {
		u.Dock = append(u.Dock,
			DockTab{ID: "job-3", Type: DockJob, Context: "alpha", Job: &JobTab{State: JobRunning}},
			DockTab{ID: "job-4", Type: DockJob, Context: "alpha", Job: &JobTab{State: JobDone}})
	})
	addDockTab(s, "logs-5", "alpha")
	mustConnect(t, s, "beta")
	addDockTab(s, "terminal-6", "beta")
	mustConnect(t, s, "alpha")

	u, err := s.CloseDockTabs("terminal-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := dockIDs(u.Dock); !slices.Equal(got, []string{"terminal-1", "job-3", "terminal-6"}) {
		t.Errorf("Close Others left %q", got)
	}
	if u.ActiveDock != "terminal-1" || !u.DockOpen {
		t.Errorf("the tab kept: active %q, open %v", u.ActiveDock, u.DockOpen)
	}
	if _, err := s.CloseDockTabs("terminal-6"); err == nil {
		t.Error("kept beta's tab, which the dock does not show")
	}

	u, err = s.CloseDockTabs("")
	if err != nil {
		t.Fatal(err)
	}
	if got := dockIDs(u.Dock); !slices.Equal(got, []string{"job-3", "terminal-6"}) {
		t.Errorf("Close All left %q", got)
	}
	if u.ActiveDock != "job-3" {
		t.Errorf("Close All: active %q, want the running job's tab", u.ActiveDock)
	}
}
