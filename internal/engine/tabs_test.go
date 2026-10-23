package engine

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// tabSummary is the tabs as "<id> <view>@<context> [p]", the active one marked with a star.
func tabSummary(u UIState) []string {
	out := make([]string, 0, len(u.Tabs))
	for _, t := range u.Tabs {
		s := t.ID + " " + t.View + "@" + t.Context
		if t.Preview {
			s += " p"
		}
		if t.ID == u.ActiveTab {
			s += " *"
		}
		out = append(out, s)
	}
	return out
}

func wantTabs(t *testing.T, u UIState, want ...string) {
	t.Helper()
	if got := tabSummary(u); !slices.Equal(got, want) {
		t.Fatalf("tabs %q, want %q", got, want)
	}
}

// tabSession is a session connected to "test" over a small cluster, with the default view in the
// preview tab.
func tabSession(t *testing.T) *Session {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	return s
}

// A connect shows the cluster's default view in the preview tab, titled "<view> - <context>".
func TestTabsFollowTheConnection(t *testing.T) {
	s := tabSession(t)
	u := s.UIState()
	wantTabs(t, u, "tab-1 pods@test p *")
	tab := u.Tabs[0]
	if tab.Title != "Pods - test" || tab.Sort != u.Sort || u.View != "pods" {
		t.Errorf("the connected tab %+v, view %q", tab, u.View)
	}
	if got, ok := u.Tab("tab-1"); !ok || got.ID != "tab-1" {
		t.Errorf("Tab(tab-1) = %+v, %v", got, ok)
	}
}

// Opening in preview replaces the preview tab's view (the old one goes into its history); a fixed
// tab opens right of the active one and stays; a fixed tab already showing a view is activated
// instead; asked for fixed, the preview tab showing the view becomes fixed.
func TestOpenTabPreviewAndFixed(t *testing.T) {
	s := tabSession(t)
	s.NoteViewTitle("deployments", "Deployments")
	u := s.OpenTab("services", false)
	wantTabs(t, u, "tab-1 services@test p *")
	if back := u.Tabs[0].Back; !slices.Equal(back, []string{"pods"}) || u.View != "services" {
		t.Errorf("history %q, view %q", back, u.View)
	}
	u = s.OpenTab("services", true) // the double click's second click: the preview becomes fixed
	wantTabs(t, u, "tab-1 services@test *")
	u = s.OpenTab("pods", false) // no preview tab left: a new one, right of the active tab
	wantTabs(t, u, "tab-1 services@test", "tab-2 pods@test p *")
	s.UpdateUI(func(u *UIState) { u.Filter = "web" }) // a search does not fix the preview tab
	u = s.OpenTab("deployments", true)
	wantTabs(t, u, "tab-1 services@test", "tab-2 pods@test p", "tab-3 deployments@test *")
	if u.Tabs[2].Title != "Deployments - test" || u.Filter != "" {
		t.Errorf("the fixed tab %+v, filter %q", u.Tabs[2], u.Filter)
	}
	u = s.OpenTab("services", false) // a fixed tab shows it: activated, not the preview replaced
	wantTabs(t, u, "tab-1 services@test *", "tab-2 pods@test p", "tab-3 deployments@test")
	if u.View != "services" || u.Tabs[1].Filter != "web" {
		t.Errorf("view %q, the preview tab's search %q", u.View, u.Tabs[1].Filter)
	}
	s.SelectTab(context.Background(), "tab-1") //nolint:errcheck // the active tab: nothing changes
	u = s.OpenTab("pods", false)               // the preview tab shows pods: activated with the view afresh
	wantTabs(t, u, "tab-1 services@test", "tab-2 pods@test p *", "tab-3 deployments@test")
	if u.Filter != "" || len(u.Tabs[1].Back) != 0 {
		t.Errorf("reopened in preview: filter %q, history %q", u.Filter, u.Tabs[1].Back)
	}
	// A double click on the active fixed tab's view keeps its search; ui.openView opens it afresh.
	s.SelectTab(context.Background(), "tab-3") //nolint:errcheck
	s.UpdateUI(func(u *UIState) { u.Filter = "web" })
	if u = s.OpenTab("deployments", true); u.Filter != "web" || len(u.Tabs) != 3 {
		t.Errorf("fixed open of the active fixed tab's view: filter %q, %q", u.Filter, tabSummary(u))
	}
	if u = s.OpenTab("deployments", false); u.Filter != "" || u.ActiveTab != "tab-3" {
		t.Errorf("openView of the active fixed tab's view: filter %q, active %q", u.Filter, u.ActiveTab)
	}
}

// Every tab keeps its namespace filter, search, sort, selection and drawer; switching tabs brings
// them back.
func TestTabStateIsRestored(t *testing.T) {
	s := tabSession(t)
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	s.UpdateUI(func(u *UIState) {
		u.Namespaces, u.Filter, u.Sort = []string{"shop"}, "web", SortSpec{Column: "name", Desc: true}
		u.Selected, u.Drawer = []model.ObjectRef{pod}, &pod
	})
	s.OpenTab("services", true)
	u := s.UIState()
	if u.View != "services" || u.Filter != "" || u.Drawer != nil || !slices.Equal(u.Namespaces, []string{"shop"}) {
		t.Fatalf("the new tab: %+v", u)
	}
	s.UpdateUI(func(u *UIState) { u.Namespaces, u.Filter = nil, "check" })
	u, err := s.SelectTab(context.Background(), "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.View != "pods" || u.Filter != "web" || !u.Sort.Desc || !slices.Equal(u.Namespaces, []string{"shop"}) ||
		len(u.Selected) != 1 || u.Drawer == nil || *u.Drawer != pod {
		t.Errorf("tab-1 restored as %+v", u)
	}
	u, _ = s.SelectTab(context.Background(), "tab-2")
	if u.View != "services" || u.Filter != "check" || u.Namespaces != nil || u.Drawer != nil {
		t.Errorf("tab-2 restored as %+v", u)
	}
	waitTable(t, s, "services", func(ts *TableSnapshot) bool { return ts.Filter == "check" })
}

// Closing the active tab shows the nearest tab of its cluster, right first; the last one leaves an
// empty workspace, where opening a view makes a preview tab again.
func TestCloseTab(t *testing.T) {
	s := tabSession(t)
	s.PinTab("") //nolint:errcheck
	s.OpenTab("services", true)
	s.OpenTab("deployments", true)
	s.OpenTab("events", false)
	wantTabs(t, s.UIState(), "tab-1 pods@test", "tab-2 services@test", "tab-3 deployments@test", "tab-4 events@test p *")
	if _, err := s.SelectTab(context.Background(), "tab-2"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.CloseTab("")
	wantTabs(t, u, "tab-1 pods@test", "tab-3 deployments@test *", "tab-4 events@test p")
	u, _ = s.CloseTab("tab-1") // not the active one
	wantTabs(t, u, "tab-3 deployments@test *", "tab-4 events@test p")
	if _, err := s.CloseTab("tab-4"); err != nil {
		t.Fatal(err)
	}
	u, _ = s.CloseTab("tab-3")
	wantTabs(t, u)
	if u.View != "" || u.ActiveTab != "" || u.Drawer != nil {
		t.Errorf("an empty workspace: %+v", u)
	}
	if _, err := s.CloseTab(""); !errors.Is(err, ErrNoTab) {
		t.Errorf("closing without tabs: %v", err)
	}
	if _, err := s.CloseTab("tab-9"); !errors.Is(err, ErrUnknownTab) {
		t.Errorf("closing an unknown tab: %v", err)
	}
	u = s.OpenTab("pods", false)
	wantTabs(t, u, "tab-5 pods@test p *")
	// A view a command opens without tabs (ui.navigate) lands in the preview tab too.
	s.CloseTab("") //nolint:errcheck
	u = s.UpdateUI(func(u *UIState) { u.View = "services" })
	wantTabs(t, u, "tab-6 services@test p *")
}

// Next and previous wrap around; ⌘n is the n-th tab, ⌘9 the last.
func TestCycleAndSelectTabs(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	s.OpenTab("services", true)
	s.OpenTab("deployments", true)
	step := func(u UIState, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return u.ActiveTab + " " + u.View
	}
	if got := step(s.CycleTab(ctx, 1)); got != "tab-1 pods" {
		t.Errorf("next after the last: %s", got)
	}
	if got := step(s.CycleTab(ctx, -1)); got != "tab-3 deployments" {
		t.Errorf("previous before the first: %s", got)
	}
	if got := step(s.CycleTab(ctx, -1)); got != "tab-2 services" {
		t.Errorf("previous: %s", got)
	}
	if got := step(s.SelectTabAt(ctx, 1, false)); got != "tab-1 pods" {
		t.Errorf("tab 1: %s", got)
	}
	if got := step(s.SelectTabAt(ctx, 0, true)); got != "tab-3 deployments" {
		t.Errorf("the last tab: %s", got)
	}
	if _, err := s.SelectTabAt(ctx, 4, false); !errors.Is(err, ErrUnknownTab) {
		t.Errorf("tab 4 of 3: %v", err)
	}
	if _, err := s.SelectTab(ctx, "tab-7"); !errors.Is(err, ErrUnknownTab) {
		t.Errorf("an unknown tab: %v", err)
	}
}

// A view changed within a tab by any command goes into its history; back and forward walk it,
// each a view opened afresh.
func TestTabHistory(t *testing.T) {
	s := tabSession(t)
	if err := s.CheckTabHistory(true); err == nil || err.Error() != "no earlier view in this tab" {
		t.Errorf("back without history: %v", err)
	}
	s.UpdateUI(func(u *UIState) { u.View = "services" }) // as ui.navigate changes it
	s.OpenTab("deployments", false)
	if back := s.UIState().Tabs[0].Back; !slices.Equal(back, []string{"pods", "services"}) {
		t.Fatalf("history %q", back)
	}
	s.UpdateUI(func(u *UIState) { u.Filter = "web" })
	u, err := s.TabHistory(true)
	if err != nil || u.View != "services" || u.Filter != "" {
		t.Fatalf("back: %v, view %q filter %q", err, u.View, u.Filter)
	}
	u, _ = s.TabHistory(true)
	tab := u.Tabs[0]
	if u.View != "pods" || len(tab.Back) != 0 || !slices.Equal(tab.Forward, []string{"deployments", "services"}) {
		t.Errorf("back twice: view %q, back %q, forward %q", u.View, tab.Back, tab.Forward)
	}
	if _, err := s.TabHistory(true); err == nil {
		t.Error("back past the start")
	}
	u, _ = s.TabHistory(false)
	if u.View != "services" || !slices.Equal(u.Tabs[0].Forward, []string{"deployments"}) {
		t.Errorf("forward: view %q, forward %q", u.View, u.Tabs[0].Forward)
	}
	s.OpenTab("events", false) // a new view drops what lay ahead
	if tab := s.UIState().Tabs[0]; len(tab.Forward) != 0 || !slices.Equal(tab.Back, []string{"pods", "services"}) {
		t.Errorf("after a new view: back %q, forward %q", tab.Back, tab.Forward)
	}
	if err := s.CheckTabHistory(false); err == nil {
		t.Error("forward without a later view")
	}
}

func TestTabHistoryIsBounded(t *testing.T) {
	var h []string
	for i := range maxTabHistory + 10 {
		h = pushHistory(h, strconv.Itoa(i))
	}
	if len(h) != maxTabHistory || h[0] != "10" || h[len(h)-1] != strconv.Itoa(maxTabHistory+9) {
		t.Errorf("history of %d: %q … %q", len(h), h[0], h[len(h)-1])
	}
}

// The tabs of another cluster stay; selecting one shows its cluster (connecting it when it is not)
// and its view again. A cluster connected from the picker shows its last used tab, else its default
// view in a preview tab of its own (D-117); a reconnect keeps the tab's view.
func TestTabsOfAnotherCluster(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	s.OpenTab("services", true)
	s.UpdateUI(func(u *UIState) { u.Filter = "check" })
	if err := s.Connect(ctx, "prod [prod.yaml]"); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	wantTabs(t, u, "tab-1 pods@test p", "tab-2 services@test", "tab-3 pods@prod [prod.yaml] p *")
	if u.Tabs[2].Title != "Pods - prod" || len(u.Tabs[2].Back) != 0 || len(u.Tabs[0].Back) != 0 {
		t.Errorf("prod's preview tab %+v, test's %+v", u.Tabs[2], u.Tabs[0])
	}
	u, err := s.SelectTab(ctx, "tab-2")
	if err != nil {
		t.Fatal(err)
	}
	if u.Context != "test" || u.Connection.State != Connected || u.ActiveTab != "tab-2" || u.View != "services" || u.Filter != "check" {
		t.Fatalf("back on test: %+v", u)
	}
	waitTable(t, s, "services", func(ts *TableSnapshot) bool { return ts.Filter == "check" && ts.Synced })

	// From the picker: prod's last used tab, then test's again; a reconnect keeps the view.
	if err := s.Connect(ctx, "prod [prod.yaml]"); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.ActiveTab != "tab-3" || u.View != "pods" {
		t.Errorf("prod from the picker: %+v", tabSummary(u))
	}
	if err := s.Connect(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.ActiveTab != "tab-2" || u.View != "services" || u.Filter != "check" {
		t.Errorf("test from the picker: %q, view %q", tabSummary(u), u.View)
	}
	if err := s.Connect(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.ActiveTab != "tab-2" || u.View != "services" {
		t.Errorf("a reconnect: %q, view %q", tabSummary(u), u.View)
	}

	// A failed connect leaves no tab active and every tab in place; selecting the tab again retries.
	if err := s.Connect(ctx, "broken"); err == nil {
		t.Fatal("broken connected")
	}
	u = s.UIState()
	wantTabs(t, u, "tab-1 pods@test p", "tab-2 services@test", "tab-3 pods@prod [prod.yaml] p")
	u, err = s.SelectTab(ctx, "tab-2")
	if err != nil || u.ActiveTab != "tab-2" || u.View != "services" {
		t.Errorf("selecting after a failed connect: %v, %q", err, tabSummary(u))
	}
}

// Selecting another cluster's tab changes no other tab (D-117, the owner's bug of 2026-10-23): the
// cluster left keeps the view its preview tab shows, though a fixed tab of it was the active one.
func TestSelectingAnotherClustersTabChangesNoTab(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	if _, err := s.PinTab(""); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "prod [prod.yaml]"); err != nil {
		t.Fatal(err)
	}
	s.OpenTab("services", true)
	wantTabs(t, s.UIState(), "tab-1 pods@test", "tab-2 pods@prod [prod.yaml] p", "tab-3 services@prod [prod.yaml] *")
	u, err := s.SelectTab(ctx, "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	wantTabs(t, u, "tab-1 pods@test *", "tab-2 pods@prod [prod.yaml] p", "tab-3 services@prod [prod.yaml]")
	if u.Context != "test" || u.View != "pods" || len(u.Tabs[1].Back) != 0 {
		t.Errorf("test shown: %q view %q, prod's preview history %q", u.Context, u.View, u.Tabs[1].Back)
	}
	u, err = s.SelectTab(ctx, "tab-3")
	if err != nil {
		t.Fatal(err)
	}
	wantTabs(t, u, "tab-1 pods@test", "tab-2 pods@prod [prod.yaml] p", "tab-3 services@prod [prod.yaml] *")
	if u.Context != "prod [prod.yaml]" || u.View != "services" {
		t.Errorf("prod shown again: %q view %q", u.Context, u.View)
	}
}

// The owner's case (2026-10-23): a view on three clusters, two in fixed tabs and the third in its
// preview tab; selecting the first tab, then the second, leaves the third's preview tab as it was,
// and every cluster keeps a preview tab of its own.
func TestThreeClustersKeepTheirPreviewTabs(t *testing.T) {
	s := tabSession(t)
	ctx := context.Background()
	for _, name := range []string{"main", "stage"} {
		if _, err := s.PinTab(""); err != nil {
			t.Fatal(err)
		}
		if err := s.Connect(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	wantTabs(t, s.UIState(), "tab-1 pods@test", "tab-2 pods@main", "tab-3 pods@stage p *")
	for _, step := range []struct{ id, context string }{{"tab-1", "test"}, {"tab-2", "main"}, {"tab-3", "stage"}} {
		u, err := s.SelectTab(ctx, step.id)
		if err != nil {
			t.Fatal(err)
		}
		if u.Context != step.context || u.ActiveTab != step.id {
			t.Fatalf("selecting %s: shown %q, %q", step.id, u.Context, tabSummary(u))
		}
		if tab, _ := u.Tab("tab-3"); tab.Context != "stage" || tab.View != "pods" || !tab.Preview || len(tab.Back) != 0 {
			t.Fatalf("after selecting %s, stage's preview tab is %+v", step.id, tab)
		}
	}

	// A view opened in preview on main goes to main's own preview tab, stage's stays.
	if _, err := s.SelectTab(ctx, "tab-2"); err != nil {
		t.Fatal(err)
	}
	u := s.OpenTab("services", false)
	wantTabs(t, u, "tab-1 pods@test", "tab-2 pods@main", "tab-4 services@main p *", "tab-3 pods@stage p")
}

// A preview tab in a closed connection keeps its view; while disconnected the tab's state does not
// follow the emptied workspace.
func TestTabKeepsItsViewWhileDisconnected(t *testing.T) {
	s := tabSession(t)
	s.OpenTab("services", false)
	s.UpdateUI(func(u *UIState) {
		u.Connection, u.View, u.Filter = Connection{State: Disconnected, Error: "gone"}, "", ""
	})
	u := s.UIState()
	if u.ActiveTab != "tab-1" || u.Tabs[0].View != "services" {
		t.Errorf("disconnected: %q", tabSummary(u))
	}
	if _, err := s.TabHistory(true); err == nil {
		t.Error("history while disconnected")
	}
}

// The titles: a page's, a remembered kind's, else the key with a capital letter; the cluster by the
// name it shows everywhere (D-68): its display name, else its short name without the kubeconfig file.
func TestTabTitles(t *testing.T) {
	s := New(Options{})
	s.NoteViewTitle("configmaps", "Config Maps")
	s.NoteViewTitle("", "nothing")
	settings := map[string]ClusterSettings{"named@ctx [b.yaml]": {Name: "Staging"}}
	cases := map[string]ViewTab{
		"Config Maps - kind-dev": {View: "configmaps", Context: "kind-dev [config]"},
		"Overview - demo":        {View: WorkloadsOverview, Context: "demo"},
		"Widgets - demo":         {View: "widgets", Context: "demo"},
		"demo":                   {Context: "demo"},
		"Pods - prod-eu":         {View: "pods", Context: "kubernetes-admin@prod-eu [a.yaml]"},
		"Staging":                {Context: "named@ctx [b.yaml]"},
	}
	for want, tab := range cases {
		if got := tabTitle(s.viewTitle, tab, settings); got != want {
			t.Errorf("title of %+v: %q, want %q", tab, got, want)
		}
	}
}

// Cloning the UI state copies the tabs deeply.
func TestCloneCopiesTabs(t *testing.T) {
	ref := model.ObjectRef{Kind: "pods", Name: "a"}
	u := UIState{Tabs: []ViewTab{{ID: "tab-1", Namespaces: []string{"a"}, Back: []string{"pods"}, Drawer: &ref}}}
	c := u.Clone()
	c.Tabs[0].Namespaces[0], c.Tabs[0].Back[0], c.Tabs[0].Drawer.Name = "b", "services", "b"
	if u.Tabs[0].Namespaces[0] != "a" || u.Tabs[0].Back[0] != "pods" || u.Tabs[0].Drawer.Name != "a" {
		t.Errorf("the clone shares the tabs: %+v", u.Tabs[0])
	}
	if (UIState{}).Clone().Tabs != nil {
		t.Error("no tabs cloned into an empty slice")
	}
}

// Pinning makes a preview tab fixed; the next view in preview opens a new preview tab.
func TestPinTab(t *testing.T) {
	s := tabSession(t)
	u, err := s.PinTab("")
	if err != nil {
		t.Fatal(err)
	}
	wantTabs(t, u, "tab-1 pods@test *")
	u = s.OpenTab("services", false)
	wantTabs(t, u, "tab-1 pods@test", "tab-2 services@test p *")
	if _, err := s.PinTab("tab-9"); !errors.Is(err, ErrUnknownTab) {
		t.Errorf("pin an unknown tab: %v", err)
	}
}
