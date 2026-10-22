package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// phasePod is a pod in a phase (testPods' Status column shows it).
func phasePod(namespace, name, phase string) *unstructured.Unstructured {
	p := testkit.Pod(namespace, name)
	p.Object["status"].(map[string]any)["phase"] = phase
	return p
}

// choicesText renders a snapshot's state choices as "State n" by column, "Column/State n" when the
// column is named.
func choicesText(t *TableSnapshot) string {
	parts := make([]string, 0, len(t.StateChoices))
	for _, c := range t.StateChoices {
		s := c.State + " " + fmt.Sprint(c.Count)
		if c.Status != model.StatusNone {
			s += " " + string(c.Status)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// statesSession is a session connected to "test" with four pods in three phases in two namespaces.
func statesSession(t *testing.T) (*Session, *testkit.MemorySource) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, phasePod("shop", "web-0", "Running"))
	src.Set(testkit.PodsGVR, phasePod("shop", "web-1", "Pending"))
	src.Set(testkit.PodsGVR, phasePod("infra", "db-0", "Running"))
	src.Set(testkit.PodsGVR, phasePod("infra", "job-0", "Failed"))
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	s, _ := newTestSession(t, src, 5*time.Millisecond)
	connectTest(t, s)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 4 })
	return s, src
}

// setStates sets the open view's status filter as ui.setStatusFilter does.
func setStates(s *Session, states ...string) UIState {
	return s.UpdateUI(func(u *UIState) { u.SetStates(states) })
}

// TestStatusFilterSnapshot: the status filter keeps the rows that carry a selected state; the
// snapshot offers the states of the rows the namespace filter and the search keep, with counts and
// colours, and the selected ones no row carries; it follows the objects; changing it reads nothing
// from the API server again.
func TestStatusFilterSnapshot(t *testing.T) {
	s, src := statesSession(t)
	snap := s.Table("pods")
	if got := choicesText(snap); got != "Failed 1 pending, Pending 1 pending, Running 2 ok" {
		t.Errorf("choices %s", got)
	}
	if snap.StateChoices[0].Column != "Status" || snap.States != nil {
		t.Errorf("choice %+v, states %v", snap.StateChoices[0], snap.States)
	}
	lists := src.Lists()

	u := setStates(s, "Running")
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if got := rowKeys(snap); !slices.Equal(got, []string{"infra/db-0", "shop/web-0"}) || snap.Total != 4 {
		t.Errorf("Running: rows %v of %d", got, snap.Total)
	}
	if got := choicesText(snap); got != "Failed 1 pending, Pending 1 pending, Running 2 ok" {
		t.Errorf("choices under the filter %s", got)
	}
	u = setStates(s, "Running", "Failed")
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if got := rowKeys(snap); !slices.Equal(got, []string{"infra/db-0", "infra/job-0", "shop/web-0"}) {
		t.Errorf("Running or Failed: %v", got)
	}

	// The namespace filter and the search narrow what is offered; a selected state no row
	// carries stays offered, with no rows.
	u = s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"shop"} })
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	// The namespace filter has the engine watch the namespaces (nswatch.go, AW11): one list.
	waitNamespacesWatched(t, src)
	lists++
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/web-0"}) || choicesText(snap) != "Failed 0, Pending 1 pending, Running 1 ok" {
		t.Errorf("shop: rows %v, choices %s", rowKeys(snap), choicesText(snap))
	}
	u = s.UpdateUI(func(u *UIState) { u.Filter = "web-1" })
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if len(snap.Rows) != 0 || choicesText(snap) != "Failed 0, Pending 1 pending, Running 0" {
		t.Errorf("searched: rows %v, choices %s", rowKeys(snap), choicesText(snap))
	}

	// An object changes its state: the filtered view follows.
	src.Set(testkit.PodsGVR, phasePod("shop", "web-1", "Running"))
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return slices.Equal(rowKeys(t), []string{"shop/web-1"}) })
	if n := src.Lists(); n != lists {
		t.Errorf("the status filter listed again: %d lists, %d before", n, lists)
	}

	// Cleared: every row the other filters keep.
	u = s.UpdateUI(func(u *UIState) { u.Namespaces, u.Filter = nil, ""; u.SetStates(nil) })
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if len(snap.Rows) != 4 || snap.States != nil || u.States != nil {
		t.Errorf("cleared: %s, states %v", summary(snap), u.States)
	}
}

// TestStatusFilterBelongsToTheView: opening another view clears the filter, as it clears the
// search; a fixed tab keeps its own and shows it again; ui.openView with states opens a view
// filtered; a view changed by any command ends the filter set for the old one.
func TestStatusFilterBelongsToTheView(t *testing.T) {
	s, _ := statesSession(t)
	setStates(s, "Pending")
	s.PinTab("") //nolint:errcheck // tab-1: pods, fixed
	u := s.OpenTab("services", true)
	if u.View != "services" || u.States != nil {
		t.Fatalf("a new view kept the filter: %+v", u.States)
	}
	if u.Tabs[0].States == nil || u.Tabs[0].States[0] != "Pending" {
		t.Errorf("tab-1 lost its filter: %+v", u.Tabs[0])
	}
	u, err := s.SelectTab(context.Background(), "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(u.States, []string{"Pending"}) {
		t.Fatalf("tab-1 shows %v", u.States)
	}
	snap := waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/web-1"}) {
		t.Errorf("tab-1's rows %v", got)
	}

	// Going back in a tab's history opens the view afresh, without the filter.
	if _, err := s.SelectTab(context.Background(), "tab-2"); err != nil {
		t.Fatal(err)
	}
	u = s.OpenTabFiltered("pods", []string{"Running"}) // the fixed pods tab, with the states
	if u.ActiveTab != "tab-1" || !slices.Equal(u.States, []string{"Running"}) {
		t.Fatalf("opened filtered: tab %s, states %v", u.ActiveTab, u.States)
	}
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) && len(t.Rows) == 2 })
	u = s.UpdateUI(func(u *UIState) { u.View = "services" }) // as ui.navigate does
	if u.States != nil || u.Tabs[0].States != nil {
		t.Errorf("a view change kept the filter: %v, tab %v", u.States, u.Tabs[0].States)
	}

	// ui.openView of the view the tab shows opens it afresh: no filter, as no search.
	if _, err := s.SelectTab(context.Background(), "tab-1"); err != nil {
		t.Fatal(err)
	}
	setStates(s, "Pending") // tab-1 shows services since the view change above
	if u = s.OpenTab("services", false); u.ActiveTab != "tab-1" || u.States != nil {
		t.Errorf("services opened afresh: tab %s, states %v", u.ActiveTab, u.States)
	}

	// A view opened filtered in the preview tab.
	s.CloseTab("tab-2") //nolint:errcheck
	u = s.OpenTabFiltered("services", []string{"Gone"})
	if u.View != "services" || !slices.Equal(u.States, []string{"Gone"}) {
		t.Fatalf("services filtered: %+v", u)
	}
	snap = waitTable(t, s, "services", func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if len(snap.Rows) != 0 || choicesText(snap) != "Gone 0" {
		t.Errorf("services without states: rows %v, choices %s", rowKeys(snap), choicesText(snap))
	}
}

// TestCleanStates: states are trimmed, empty and repeated ones dropped, long ones refused, at most
// MaxStates kept.
func TestCleanStates(t *testing.T) {
	long := strings.Repeat("x", MaxStateLength+1)
	if got := CleanStates([]string{" Running ", "", "Running", long, "Pending"}); !slices.Equal(got, []string{"Running", "Pending"}) {
		t.Errorf("cleaned %v", got)
	}
	many := make([]string, MaxStates+5)
	for i := range many {
		many[i] = fmt.Sprint("s", i)
	}
	if got := CleanStates(many); len(got) != MaxStates {
		t.Errorf("%d states kept", len(got))
	}
	if CleanStates(nil) != nil || CleanStates([]string{" "}) != nil {
		t.Error("no states is not nil")
	}
}

// TestStatusFilterFluxPage: the Flux dashboard's table filters by its Status column too.
func TestStatusFilterFluxPage(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	src.Set(crdsGVR, testkit.CRD(fluxKsGVR.Group, fluxKsGVR.Resource, "Kustomization", true))
	ks := func(ns, name, ready string, suspended bool) *unstructured.Unstructured {
		return fluxObj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", ns, name, ready, "2026-10-13T10:00:00Z", suspended)
	}
	src.Set(fluxKsGVR, ks("flux-system", "apps", "True", false))
	src.Set(fluxKsGVR, ks("flux-system", "broken", "False", false))
	src.Set(fluxKsGVR, ks("flux-system", "fresh", "Unknown", false))
	src.Set(fluxKsGVR, ks("team-a", "paused", "True", true))
	s := newCRDSession(t, src)
	waitFor(t, "the Flux kinds", func() bool { return hasKind(s, fluxKsKey) })
	s.OpenTab(FluxDashboard, false)
	snap := waitTable(t, s, FluxDashboard, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 3 })
	if got := choicesText(snap); got != "Not Ready 1 error, Reconciling 1 pending, Suspended 1 stopped" {
		t.Errorf("choices %s", got)
	}
	u := setStates(s, "Suspended", "Not Ready")
	snap = waitTable(t, s, FluxDashboard, func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if rowNames(snap) != "broken paused" {
		t.Errorf("filtered %s", summary(snap))
	}

	// A dashboard state opens its kind's view filtered to it.
	u = s.OpenTabFiltered(fluxKsKey, []string{kinds.FluxStateText(kinds.FluxSuspended)})
	snap = waitTable(t, s, fluxKsKey, func(t *TableSnapshot) bool { return t.Synced && t.CaughtUp(u) })
	if rowNames(snap) != "paused" || choicesText(snap) != "Not Ready 1 error, Ready 1 ok, Reconciling 1 pending, Suspended 1 stopped" {
		t.Errorf("Kustomizations: %s, choices %s", summary(snap), choicesText(snap))
	}
}

// TestArgoTilesNameTheStatusFilter: the Argo CD Dashboard's tiles name the Applications view's
// states, qualified by Sync and Health; opening the view with one shows the tile's Applications.
func TestArgoTilesNameTheStatusFilter(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(crdsGVR, crd)
	}
	src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("argocd", "good", "Synced", "Healthy"))
	src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("argocd", "drift", "OutOfSync", "Healthy"))
	src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("argocd", "broken", "Synced", "Degraded"))
	src.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("team", "fresh", "", ""))
	s := newCRDSession(t, src)
	waitFor(t, "Argo CD's kinds", func() bool { return hasKind(s, kinds.ArgoApplicationsKey) })
	m, _, err := s.ArgoCDDashboard(context.Background(), "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, tile := range append(append(slices.Clone(m.Strip), m.Health...), m.Sync...) {
		states[tile.Label] = tile.State
	}
	want := map[string]string{
		"Applications": "", "Synced": "Sync: Synced", "Healthy": "Health: Healthy", "Out of sync": "Sync: OutOfSync",
		"Degraded": "Health: Degraded", "Missing": "Health: Missing", "Progressing": "Health: Progressing", "Not assessed": "",
	}
	for label, st := range want {
		if got, ok := states[label]; !ok || got != st {
			t.Errorf("tile %s: state %q (%v), want %q", label, got, ok, st)
		}
	}
	u := s.OpenTabFiltered(kinds.ArgoApplicationsKey, []string{states["Out of sync"]})
	snap := waitTable(t, s, kinds.ArgoApplicationsKey, func(t *TableSnapshot) bool { return t.Synced && t.CaughtUp(u) })
	if rowNames(snap) != "drift" {
		t.Errorf("out of sync: %s", summary(snap))
	}
	cols := map[string]string{}
	for _, c := range snap.StateChoices {
		cols[c.State] = c.Column
	}
	if cols["Sync: OutOfSync"] != "Sync" || cols["Health: Degraded"] != "Health" {
		t.Errorf("choices %+v", snap.StateChoices)
	}
	u = setStates(s, "Sync: Synced", "Health: Degraded")
	snap = waitTable(t, s, kinds.ArgoApplicationsKey, func(t *TableSnapshot) bool { return t.CaughtUp(u) })
	if rowNames(snap) != "broken" {
		t.Errorf("synced and degraded: %s", summary(snap))
	}
}

// TestWorkspaceKeepsTheStatusFilter: a tab's status filter survives a restart and shows again when
// the tab does; a file's states are cleaned as ui.setStatusFilter cleans them.
func TestWorkspaceKeepsTheStatusFilter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	a, _ := workspaceSession(t, dir, time.Hour)
	connectTest(t, a)
	setStates(a, "Running")
	a.OpenTab("services", true)
	if _, err := a.SelectTab(context.Background(), "tab-1"); err != nil { // pods is shown last
		t.Fatal(err)
	}
	a.Close()
	if doc := readWorkspace(t, dir); len(doc.Tabs) != 2 || !slices.Equal(doc.Tabs[0].States, []string{"Running"}) || doc.Tabs[1].States != nil {
		t.Fatalf("file %+v", doc)
	}

	b, _ := workspaceSession(t, dir, time.Hour)
	if tab := b.UIState().Tabs[0]; !slices.Equal(tab.States, []string{"Running"}) {
		t.Fatalf("restored tab %+v", tab)
	}
	u, err := b.SelectTab(context.Background(), "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if u.View != "pods" || !slices.Equal(u.States, []string{"Running"}) {
		t.Fatalf("shown: view %q states %v", u.View, u.States)
	}
	waitTable(t, b, "pods", func(t *TableSnapshot) bool { return t.CaughtUp(u) && len(t.Rows) == 1 })
	b.Close()

	// A file with states that ui.setStatusFilter would not take.
	doc := readWorkspace(t, dir)
	doc.Tabs[0].States = []string{" Pending ", "", "Pending", strings.Repeat("x", MaxStateLength+1)}
	writeWorkspace(t, dir, doc)
	c, _ := workspaceSession(t, dir, time.Hour)
	if tab := c.UIState().Tabs[0]; !slices.Equal(tab.States, []string{"Pending"}) {
		t.Errorf("cleaned states %q", tab.States)
	}
}

// BenchmarkSnapshot10kStates measures a 10,000-pod snapshot with the status filter set, from a
// warm row cache: the states are counted and matched on the snapshot, nothing listed again.
func BenchmarkSnapshot10kStates(b *testing.B) {
	objs := benchPods(10_000)
	for i, o := range objs {
		if i%10 == 0 {
			o.Object["status"].(map[string]any)["phase"] = "Pending"
		}
	}
	for _, c := range []struct {
		name   string
		states []string
	}{{"none", nil}, {"pending", []string{"Pending"}}, {"two", []string{"Pending", "Running"}}} {
		b.Run(c.name, func(b *testing.B) {
			rc := &rowCache{}
			params := viewParams{sort: SortSpec{Column: "name"}, states: c.states}
			rc.build(testPods, "bench", objs, params, nil, 0)
			b.ReportAllocs()
			for b.Loop() {
				if snap := rc.build(testPods, "bench", objs, params, nil, 0); snap.Total != len(objs) {
					b.Fatal("short snapshot")
				}
			}
		})
	}
}
