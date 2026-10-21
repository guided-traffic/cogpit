package fyneui

import (
	"context"
	"slices"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Argo CD Dashboard's states that argocd_test.go's fixture does not have once it is listed
// (argocd_dashboard.go): the strip while Argo CD's kinds are still being listed, the Applications
// card's buttons with nothing to sync or refresh, and the recent activity before any operation.
// Each comes from a model the fake engine answers argocd.dashboard with. Every name is made up.

// argoDashFake is connectedFake on the Argo CD Dashboard, answering argocd.dashboard with the model
// put last, as the engine answers with the open page's latest build; put announces a new build
// with the page's table event, as the engine's worker does after each rebuild.
func argoDashFake(t *testing.T, first engine.ArgoCDDashboardModel) (f *fakeEngine, put func(engine.ArgoCDDashboardModel)) {
	f = connectedFake(t, 1)
	var mu sync.Mutex
	current := first
	f.handlers["argocd.dashboard"] = func(context.Context, engine.Invocation) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	f.tables[engine.ArgoCDDashboard] = &engine.TableSnapshot{View: engine.ArgoCDDashboard, Context: "demo", Title: "Dashboard",
		Namespaced: true, Rows: []model.Row{}, Synced: first.Synced, Revision: 1}
	f.ui.View = engine.ArgoCDDashboard
	put = func(m engine.ArgoCDDashboardModel) {
		mu.Lock()
		current = m
		mu.Unlock()
		snap := *f.Table(engine.ArgoCDDashboard)
		snap.Synced, snap.Revision = m.Synced, snap.Revision+1
		f.setTable(engine.ArgoCDDashboard, &snap)
	}
	return f, put
}

// argoStrip is the engine's health strip (argoApplicationsSummary) for the counts of Applications,
// Synced, Healthy, Out of sync, Degraded, Missing and Progressing.
func argoStrip(apps, synced, healthy, outOfSync, degraded, missing, progressing int) []engine.ArgoTile {
	return []engine.ArgoTile{
		{Label: "Applications", Count: apps},
		{Label: "Synced", Count: synced, Status: model.StatusOK, State: "Sync: Synced"},
		{Label: "Healthy", Count: healthy, Status: model.StatusOK, State: "Health: Healthy"},
		{Label: "Out of sync", Count: outOfSync, Status: model.StatusWarning, State: "Sync: OutOfSync"},
		{Label: "Degraded", Count: degraded, Status: model.StatusError, State: "Health: Degraded"},
		{Label: "Missing", Count: missing, Status: model.StatusWarning, State: "Health: Missing"},
		{Label: "Progressing", Count: progressing, Status: model.StatusPending, State: "Health: Progressing"},
	}
}

// argoAppRef is an Application of the fake's cluster.
func argoAppRef(name string) model.ObjectRef {
	return model.ObjectRef{Context: "demo", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: name}
}

// argoTileShows is what a tile of the strip shows: its label, its count's text and colour, and the
// colour of the spine at its edge ("" for none).
type argoTileShows struct {
	label, count string
	color, spine fyne.ThemeColorName
}

// argoStripShows reads the strip's tiles as they are drawn.
func argoStripShows(t *testing.T, p *argoPanel) []argoTileShows {
	t.Helper()
	out := make([]argoTileShows, 0, len(p.strip.Objects))
	for _, tile := range p.strip.Objects {
		lines, links := findAll[*textLine](tile), findAll[*linkText](tile)
		if len(lines) != 1 || len(links) != 1 {
			t.Fatalf("a tile with %d texts and %d links", len(lines), len(links))
		}
		s := argoTileShows{label: links[0].text, count: lines[0].text, color: lines[0].color}
		for _, sf := range findAll[*surface](tile) {
			if sf.fill != theme.ColorNameInputBackground { // not the tile's plate
				s.spine = sf.fill
			}
		}
		out = append(out, s)
	}
	return out
}

// TestArgoDashboardStripWhileListing: while Argo CD's kinds are still being listed, every count of
// the strip ends in " …", as it may still grow; a zero is muted, a healthy count is in the text
// colour, and a count of something that needs a look is in its status colour with a spine at the
// tile's edge. Once listed, the counts lose the " …" and keep their colours.
func TestArgoDashboardStripWhileListing(t *testing.T) {
	m := engine.ArgoCDDashboardModel{Served: true, Strip: argoStrip(1204, 1180, 1190, 24, 2, 0, 12)}
	f, put := argoDashFake(t, m)
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.argo
	q.until(t, "the strip while listing", func() bool { return p.data != nil && len(p.strip.Objects) == 7 })
	listed := []argoTileShows{
		{"Applications", "1,204", theme.ColorNameForeground, ""},
		{"Synced", "1,180", theme.ColorNameForeground, ""},
		{"Healthy", "1,190", theme.ColorNameForeground, ""},
		{"Out of sync", "24", colorStatusWarning, colorStatusWarning},
		{"Degraded", "2", colorStatusError, colorStatusError},
		{"Missing", "0", colorMuted, ""},
		{"Progressing", "12", colorStatusPending, colorStatusPending},
	}
	listing := make([]argoTileShows, 0, len(listed))
	for _, s := range listed {
		s.count += " …"
		listing = append(listing, s)
	}
	if got := argoStripShows(t, p); !slices.Equal(got, listing) {
		t.Errorf("while listing, the strip shows\n%v\nwant\n%v", got, listing)
	}

	m.Synced = true
	put(m)
	q.until(t, "the listed strip", func() bool { return p.data.Synced })
	if got := argoStripShows(t, p); !slices.Equal(got, listed) {
		t.Errorf("listed, the strip shows\n%v\nwant\n%v", got, listed)
	}
}

// TestArgoDashboardButtonsFollowTheApplications: the Applications card's Sync Out of Sync… is
// enabled only while an Application is out of sync, and Refresh All… only while there are
// Applications: the build after the last out-of-sync Application synced greys Sync again, and the
// one after the Applications were deleted greys Refresh too.
func TestArgoDashboardButtonsFollowTheApplications(t *testing.T) {
	docs, shop := argoAppRef("docs"), argoAppRef("shop")
	m := engine.ArgoCDDashboardModel{Served: true, Synced: true, Strip: argoStrip(2, 1, 2, 1, 0, 0, 0),
		All: []model.ObjectRef{docs, shop}, OutOfSync: []model.ObjectRef{shop}}
	f, put := argoDashFake(t, m)
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.argo
	q.until(t, "the dashboard", func() bool { return p.data != nil })
	if p.sync.Disabled() || p.refresh.Disabled() {
		t.Fatalf("shop out of sync: Sync disabled %v, Refresh disabled %v", p.sync.Disabled(), p.refresh.Disabled())
	}

	m.Strip, m.OutOfSync = argoStrip(2, 2, 2, 0, 0, 0, 0), nil
	put(m)
	q.until(t, "the build with shop synced", func() bool { return len(p.data.OutOfSync) == 0 })
	if !p.sync.Disabled() {
		t.Error("Sync Out of Sync… stays enabled with no Application out of sync")
	}
	if p.refresh.Disabled() {
		t.Error("Refresh All… is greyed with two Applications")
	}

	m.Strip, m.All = argoStrip(0, 0, 0, 0, 0, 0, 0), nil
	put(m)
	q.until(t, "the build without Applications", func() bool { return len(p.data.All) == 0 })
	if !p.sync.Disabled() || !p.refresh.Disabled() {
		t.Errorf("without Applications: Sync disabled %v, Refresh disabled %v", p.sync.Disabled(), p.refresh.Disabled())
	}
}

// TestArgoDashboardWithoutOperations: before any Application ran an operation, the recent activity
// says so, muted; with a filter typed it says instead that nothing matches the filter, a filter of
// spaces filters nothing, and once the filter is cleared it says again that there are none.
func TestArgoDashboardWithoutOperations(t *testing.T) {
	m := engine.ArgoCDDashboardModel{Served: true, Synced: true, Strip: argoStrip(1, 1, 1, 0, 0, 0, 0),
		All: []model.ObjectRef{argoAppRef("docs")}}
	f, _ := argoDashFake(t, m)
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.argo
	q.until(t, "the dashboard", func() bool { return p.data != nil })
	for _, step := range []struct{ filter, says string }{
		{"", "No operations yet."}, // the filter empty from the start: what the page drew
		{"   ", "No operations yet."},
		{"docs", "No activity matches the filter."},
		{"", "No operations yet."},
	} {
		p.filter.SetText(step.filter)
		objs := p.activity.Objects
		if len(objs) != 1 {
			t.Fatalf("filter %q: the activity has %d lines, want 1", step.filter, len(objs))
		}
		l, ok := objs[0].(*textLine)
		if !ok {
			t.Fatalf("filter %q: the activity shows a %T, want the text %q", step.filter, objs[0], step.says)
		}
		if l.text != step.says || l.color != colorMuted {
			t.Errorf("filter %q: the activity says %q in %s, want %q in %s", step.filter, l.text, l.color, step.says, colorMuted)
		}
	}
}
