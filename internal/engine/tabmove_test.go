package engine

import (
	"errors"
	"slices"
	"testing"
)

// tabIDs are the tabs' ids in order.
func tabIDs(tabs []ViewTab) []string {
	out := make([]string, len(tabs))
	for i, t := range tabs {
		out[i] = t.ID
	}
	return out
}

// TestMoveTab (the owner's GW4): a tab moves to a place among the tabs the bar shows; the shown
// cluster's (GW11), the other clusters' keeping their places among the rest; every tab while no
// cluster is shown; "" is the active tab; a place outside the bar, an unknown tab and a hidden one
// are refused and change nothing.
func TestMoveTab(t *testing.T) {
	base := UIState{Context: "a", NavigatorScope: NavScopeLocal, ActiveTab: "a2", Tabs: []ViewTab{
		{ID: "a1", Context: "a"}, {ID: "b1", Context: "b"}, {ID: "a2", Context: "a"}, {ID: "b2", Context: "b"}, {ID: "a3", Context: "a"},
	}}
	cases := []struct {
		name  string
		shown string
		id    string
		index int
		want  []string
	}{
		{"no cluster shown: right", "", "b1", 4, []string{"a1", "a2", "b2", "b1", "a3"}},
		{"no cluster shown: to the first", "", "a3", 1, []string{"a3", "a1", "b1", "a2", "b2"}},
		{"no cluster shown: to the last", "", "a1", 5, []string{"b1", "a2", "b2", "a3", "a1"}},
		{"no cluster shown: its own place", "", "a2", 3, []string{"a1", "b1", "a2", "b2", "a3"}},
		{"the active tab", "", "", 1, []string{"a2", "a1", "b1", "b2", "a3"}},
		{"cluster a shown: to the first", "a", "a3", 1, []string{"a3", "a1", "b1", "a2", "b2"}},
		{"cluster a shown: to the last", "a", "a1", 3, []string{"b1", "a2", "b2", "a3", "a1"}},
		{"cluster a shown: one right, before the shown tab after it", "a", "a1", 2, []string{"b1", "a2", "b2", "a1", "a3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := base.Clone()
			u.Context = c.shown
			if err := u.moveTab(c.id, c.index); err != nil {
				t.Fatal(err)
			}
			if got := tabIDs(u.Tabs); !slices.Equal(got, c.want) {
				t.Fatalf("%v, want %v", got, c.want)
			}
		})
	}

	for _, c := range []struct {
		name  string
		shown string
		id    string
		index int
		is    error
	}{
		{"an unknown tab", "", "c9", 1, ErrUnknownTab},
		{"another cluster's tab", "a", "b1", 1, errNotInBar},
		{"place 0", "", "a1", 0, nil},
		{"past the last", "a", "a1", 4, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := base.Clone()
			u.Context = c.shown
			err := u.moveTab(c.id, c.index)
			if err == nil || (c.is != nil && !errors.Is(err, c.is)) {
				t.Fatalf("error %v, want %v", err, c.is)
			}
			if got := tabIDs(u.Tabs); !slices.Equal(got, tabIDs(base.Tabs)) {
				t.Fatalf("a refused move changed the order to %v", got)
			}
		})
	}
	u := base.Clone()
	u.ActiveTab = ""
	if err := u.moveTab("", 1); !errors.Is(err, ErrNoTab) {
		t.Fatalf("the active tab without one: %v", err)
	}
}

// TestTabStep: Move Tab Left and Right step the active tab among the tabs the bar shows, and say
// why not at the bar's ends.
func TestTabStep(t *testing.T) {
	u := UIState{Context: "a", NavigatorScope: NavScopeCluster("a"), ActiveTab: "a1", Tabs: []ViewTab{
		{ID: "b1", Context: "b"}, {ID: "a1", Context: "a"}, {ID: "a2", Context: "a"},
	}}
	if _, err := u.tabStep(-1); err == nil {
		t.Fatal("the first tab steps left")
	}
	to, err := u.tabStep(1)
	if err != nil || to != 2 {
		t.Fatalf("right: %d, %v", to, err)
	}
	if err := u.moveTab(u.ActiveTab, to); err != nil {
		t.Fatal(err)
	}
	if got := tabIDs(u.Tabs); !slices.Equal(got, []string{"b1", "a2", "a1"}) {
		t.Fatalf("after the step %v", got)
	}
	if _, err := u.tabStep(1); err == nil {
		t.Fatal("the last tab steps right")
	}
	u.ActiveTab = ""
	if _, err := u.tabStep(1); !errors.Is(err, ErrNoTab) {
		t.Fatalf("without an active tab: %v", err)
	}
}

// TestMoveDockTab: a dock tab moves among the tabs the dock shows (the shown cluster's and those of
// no cluster); another cluster's hidden tab keeps its place and cannot be moved.
func TestMoveDockTab(t *testing.T) {
	u := UIState{Context: "a", Dock: []DockTab{
		{ID: "logs-1", Context: "a"}, {ID: "terminal-1", Context: "b"}, {ID: "audit-1"}, {ID: "edit-1", Context: "a"},
	}}
	if err := u.moveDockTab("edit-1", 1); err != nil {
		t.Fatal(err)
	}
	ids := func() []string {
		var out []string
		for _, d := range u.Dock {
			out = append(out, d.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []string{"edit-1", "logs-1", "terminal-1", "audit-1"}) {
		t.Fatalf("after the move %v", got)
	}
	if err := u.moveDockTab("terminal-1", 1); !errors.Is(err, errNotInBar) {
		t.Fatalf("a hidden cluster's tab: %v", err)
	}
	if err := u.moveDockTab("logs-9", 1); !errors.Is(err, ErrUnknownTab) {
		t.Fatalf("an unknown tab: %v", err)
	}
}

// TestMovedTabsSurviveARestart: the workspace keeps the tabs in the order a move gave them.
func TestMovedTabsSurviveARestart(t *testing.T) {
	s := tabSession(t)
	if _, err := s.MoveTab("", 1); err != nil { // one tab: its own place
		t.Fatal(err)
	}
	s.OpenTab("services", true)
	wantTabs(t, s.UIState(), "tab-1 pods@test p", "tab-2 services@test *")
	if _, err := s.MoveTab("tab-2", 1); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	wantTabs(t, u, "tab-2 services@test *", "tab-1 pods@test p")
	doc := workspaceDocOf(&u, s.viewTitle)
	if len(doc.Tabs) != 2 || doc.Tabs[0].View != "services" || doc.Tabs[1].View != "pods" || doc.Active != 0 {
		t.Fatalf("the workspace keeps %+v, active %d", doc.Tabs, doc.Active)
	}
}
