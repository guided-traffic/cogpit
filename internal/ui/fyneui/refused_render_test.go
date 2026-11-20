package fyneui

import (
	"context"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The pages and the Kyverno integration of a user the access reviews say may not list their kinds
// (engine: mayNotList, D-177), with the models the engine builds for that user
// (TestReadersNameKindsTheReviewsDeny): a kind that could not be read shows no count, only "—"
// in the muted colour (no "0", no "No objects"); the table's error line names the refusal once, and
// its placeholder claims nothing ("No … found").

const (
	fluxMayNotList = "Cogpit may not list kustomizations.kustomize.toolkit.fluxcd.io, gitrepositories.source.toolkit.fluxcd.io"
	argoMayNotList = "Cogpit may not list applications.argoproj.io"
)

// refusedTableShown checks the table's error line (text, once) and that its placeholder shows no
// claim.
func refusedTableShown(t *testing.T, sh *shell, q *mainQueue, errLine string) {
	t.Helper()
	q.until(t, "the error line", func() bool { return sh.view.errLine.Visible() && sh.view.errLine.text == errLine })
	if sh.view.placeholder.Visible() {
		t.Errorf("the placeholder says %q under the refusal", sh.view.placeholderText.text)
	}
	if sh.view.count.text != "" || sh.view.syncing.text != "" {
		t.Errorf("the header counts %q %q what could not be read", sh.view.count.text, sh.view.syncing.text)
	}
	n := 0
	for _, s := range visibleTexts(sh.root) {
		if s == errLine {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the refusal is shown %d times, want once", n)
	}
}

// TestRenderFluxDashboardMayNotListDark: the Flux dashboard of a user who may not list the Flux
// kinds names them once, in its error line, with no tile and no placeholder.
func TestRenderFluxDashboardMayNotListDark(t *testing.T) {
	f, publish := fluxReadingFake(t)
	publish(engine.FluxDashboardModel{Kinds: []engine.FluxDashboardKind{}, Synced: true, Error: fluxMayNotList})
	snap := *f.Table(engine.FluxDashboard)
	snap.Error = fluxMayNotList
	f.setTable(engine.FluxDashboard, &snap)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	refusedTableShown(t, sh, q, fluxMayNotList)
	savePNG(t, w, "flux_dashboard_may_not_list_dark.png")
}

// TestFluxTileOfARefusedKind: a Flux kind the API server refused shows "—" in place of its count,
// and neither states nor "No objects"; its section's head too.
func TestFluxTileOfARefusedKind(t *testing.T) {
	const ksKey = "kustomizations.kustomize.toolkit.fluxcd.io"
	f, publish := fluxReadingFake(t)
	k := fluxReadingKind(t, ksKey, false, nil)
	k.Error = "in team-a: kustomizations.kustomize.toolkit.fluxcd.io is forbidden"
	publish(engine.FluxDashboardModel{Kinds: []engine.FluxDashboardKind{k}, Error: "Kustomizations: " + k.Error})
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the refused kind's tile", func() bool { tile := sh.view.flux.tiles[ksKey]; return tile != nil && tile.count.text == noCount })
	if got := visibleTexts(sh.view.flux.tiles[ksKey]); !slices.Equal(got, []string{"Kustomizations", noCount}) {
		t.Errorf("the refused kind's tile shows %q", got)
	}
	var heads []string
	for _, h := range findAll[*bracketLabel](sh.view.flux.box) {
		heads = append(heads, h.text)
	}
	if !slices.Equal(heads, []string{"release & build · " + noCount}) {
		t.Errorf("the section heads %q, want the refused kind's without a count", heads)
	}
}

// TestRenderArgoDashboardMayNotListDark: the Argo CD dashboard of a user who may not list its
// kinds: the strip, its head and the cards show "—", muted; the table's error line names the
// refusal once.
func TestRenderArgoDashboardMayNotListDark(t *testing.T) {
	var ks []engine.OverviewKind
	for _, k := range []struct{ key, title, res string }{
		{kinds.ArgoApplicationsKey, "Applications", "applications.argoproj.io"},
		{"applicationsets.argoproj.io", "Application Sets", "applicationsets.argoproj.io"},
		{"appprojects.argoproj.io", "Projects", "appprojects.argoproj.io"},
		{"argocdrepositories", "Repositories", "secrets"},
		{"argocdclusters", "Clusters", "secrets"},
	} {
		ks = append(ks, engine.OverviewKind{Kind: k.key, Title: k.title, Synced: true, Error: "Cogpit may not list " + k.res})
	}
	m := engine.ArgoCDDashboardModel{Served: true, Synced: true, Kinds: ks, Strip: argoStrip(0, 0, 0, 0, 0, 0, 0), Error: argoMayNotList}
	f, _ := argoDashFake(t, m)
	snap := *f.Table(engine.ArgoCDDashboard)
	snap.Error = argoMayNotList
	snap.Columns = []model.Column{ // the Needs attention table's columns (engine: argoAttentionProfile)
		{ID: "name", Title: "Name", Type: model.ColumnText}, {ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "sync", Title: "Sync", Type: model.ColumnText}, {ID: "health", Title: "Health", Type: model.ColumnText},
		{ID: "message", Title: "Message", Type: model.ColumnText}, {ID: "age", Title: "Age", Type: model.ColumnAge},
	}
	f.setTable(engine.ArgoCDDashboard, &snap)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	p := sh.view.argo
	q.until(t, "the page", func() bool { return p.data != nil && len(p.strip.Objects) == 7 })
	for _, s := range argoStripShows(t, p) {
		if s.count != noCount || s.color != colorMuted {
			t.Errorf("the strip's %s shows %q in %s, want %q muted", s.label, s.count, s.color, noCount)
		}
	}
	if p.stripHead.text != "applications · "+noCount {
		t.Errorf("the strip's head: %q", p.stripHead.text)
	}
	for _, c := range findAll[*overviewTile](p.cards) {
		if c.count.text != noCount || c.count.color != colorMuted {
			t.Errorf("the %s card shows %q in %s", c.link.text, c.count.text, c.count.color)
		}
	}
	if got := visibleTexts(p.activity); len(got) != 0 {
		t.Errorf("the recent activity says %q of Applications that could not be read", got)
	}
	refusedTableShown(t, sh, q, argoMayNotList)
	savePNG(t, w, "argocd_dashboard_may_not_list_dark.png")
}

// TestRenderWorkloadsOverviewMayNotListDark: the Workloads Overview of a user who may list pods and
// Deployments but none of the other workload kinds nor the events: their tiles show "—", the
// events' error line names the refusal once, and no "No events found." under it.
func TestRenderWorkloadsOverviewMayNotListDark(t *testing.T) {
	f := overviewFake(t)
	m := engine.WorkloadsOverviewModel{Synced: true, Kinds: []engine.OverviewKind{
		overviewKind(f, "pods", "Pods", 0), overviewKind(f, "deployments", "Deployments", 0),
	}}
	for _, k := range []struct {
		key, title, res string
		column          int
	}{
		{"daemonsets", "Daemon Sets", "daemonsets.apps", 0}, {"statefulsets", "Stateful Sets", "statefulsets.apps", 0},
		{"replicasets", "Replica Sets", "replicasets.apps", 1}, {"jobs", "Jobs", "jobs.batch", 1}, {"cronjobs", "Cron Jobs", "cronjobs.batch", 1},
	} {
		m.Kinds = append(m.Kinds, engine.OverviewKind{Kind: k.key, Title: k.title, Column: k.column, Synced: true, Error: "Cogpit may not list " + k.res})
	}
	f.handlers["workloads.overview"] = func(context.Context, engine.Invocation) (any, error) { return m, nil }
	events := *f.Table(engine.WorkloadsOverview)
	events.Rows, events.Total, events.Error = nil, 0, "Cogpit may not list events"
	f.setTable(engine.WorkloadsOverview, &events)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	p := sh.view.overview
	q.until(t, "the tiles", func() bool { return len(p.tiles) == 7 && p.tiles["jobs"].count.text == noCount })
	for key, tile := range p.tiles {
		refused := key != "pods" && key != "deployments"
		if (tile.count.text == noCount) != refused {
			t.Errorf("%s shows %q", key, tile.count.text)
		}
	}
	refusedTableShown(t, sh, q, "Cogpit may not list events")
	savePNG(t, w, "workloads_overview_may_not_list_dark.png")
}

// TestRenderKyvernoMayNotListDark: Cluster Settings › Integrations of a user the access reviews
// say may not list Kyverno's policy reports: "No permission", naming the kind.
func TestRenderKyvernoMayNotListDark(t *testing.T) {
	inf := newIntegrationsFake(t)
	for i, st := range inf.statuses {
		if st.Name == engine.IntegrationKyverno {
			inf.statuses[i].Where, inf.statuses[i].Detail = "wgpolicyk8s.io/v1alpha2", "may not list policyreports.wgpolicyk8s.io"
		}
	}
	inf.decide()
	sh, w, q := newTestShell(t, inf.fakeEngine, Options{Theme: ThemeDark})
	inf.update(func(u *engine.UIState) {
		u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: "kind-dev", Section: engine.SectionIntegrations}
	})
	const want = "No permission: wgpolicyk8s.io/v1alpha2 (may not list policyreports.wgpolicyk8s.io)"
	q.until(t, "Kyverno's status", func() bool {
		rows := sh.settings.integrationRows
		return len(rows) == len(engine.Integrations) && integrationRowOf(t, rows, engine.IntegrationKyverno).status.text == want
	})
	savePNG(t, w, "cluster_settings_kyverno_may_not_list_dark.png")
}

// refusedRelated is fixtureRelated whose kinds in refused the user may not list
// (model.RelatedRefusals): their objects are unknown.
type refusedRelated struct {
	fixtureRelated
	refused []string
}

func (r refusedRelated) Refused(kind string) bool { return slices.Contains(r.refused, kind) }

// TestRenderClaimsMayNotListPodsDark: the Persistent Volume Claims of a user who may not list pods:
// every Pods cell is "—" in the muted colour, never empty, and the error line names the pods.
func TestRenderClaimsMayNotListPodsDark(t *testing.T) {
	f := claimsFake(t)
	claims, pods := fixtureClaims()
	snap := kindSnapshotWith(t, "persistentvolumeclaims", claims, refusedRelated{fixtureRelated{"pods": pods}, []string{"pods"}})
	snap.Error = "Cogpit may not list pods"
	f.tables["persistentvolumeclaims"] = snap
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the claims view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "persistentvolumeclaims" && sh.diag.total > 0
	})
	settle(w)
	col := columnOf(t, sh, "pods")
	for _, r := range snap.Rows {
		c := cellAt(t, sh, rowOf(t, f, "persistentvolumeclaims", r.Ref.Name), col)
		if c.full != model.NoCount || c.text.Color != themeColor(c.Theme(), colorMuted) {
			t.Errorf("%s's Pods cell %q in %v, want %q muted", r.Ref.Name, c.full, c.text.Color, model.NoCount)
		}
	}
	savePNG(t, w, "pvc_may_not_list_pods_dark.png")
}
