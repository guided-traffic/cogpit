package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Flux and Argo CD dashboards of a user the access reviews say may not list their kinds
// (engine: mayNotList, D-new): the page names the kinds instead of "The cluster serves no Flux
// kinds." or counts of nothing. The models are the engine's for that user
// (TestReadersNameKindsTheReviewsDeny).

const (
	fluxMayNotList = "Cogpit may not list kustomizations.kustomize.toolkit.fluxcd.io, gitrepositories.source.toolkit.fluxcd.io"
	argoMayNotList = "Cogpit may not list applications.argoproj.io"
)

// renderFluxMayNotList shows the Flux dashboard with the model and the table's error line the
// engine builds for the user, checks that the page says error, and saves it as name.
func renderFluxMayNotList(t *testing.T, modelErr, tableErr, name string) {
	f, publish := fluxReadingFake(t)
	publish(engine.FluxDashboardModel{Kinds: []engine.FluxDashboardKind{}, Synced: true, Error: modelErr})
	snap := *f.Table(engine.FluxDashboard)
	snap.Error = tableErr
	f.setTable(engine.FluxDashboard, &snap)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the page's error line", func() bool { return sh.view.errLine.Visible() && sh.view.errLine.text == tableErr })
	want := modelOr(modelErr, "The cluster serves no Flux kinds.")
	q.until(t, "the page's text", func() bool {
		for _, l := range findAll[*widget.Label](sh.view.flux.box) {
			if l.Visible() && l.Text == want {
				return true
			}
		}
		return textsContain(visibleTexts(sh.view.flux.box), want)
	})
	savePNG(t, w, name)
}

// renderArgoMayNotList shows the Argo CD dashboard with the model and the table's error line the
// engine builds for the user, checks the error line, and saves it as name.
func renderArgoMayNotList(t *testing.T, kindErr, name string) {
	var ks []engine.OverviewKind
	for _, k := range []struct{ key, title, res string }{
		{kinds.ArgoApplicationsKey, "Applications", "applications.argoproj.io"},
		{"applicationsets.argoproj.io", "Application Sets", "applicationsets.argoproj.io"},
		{"appprojects.argoproj.io", "Projects", "appprojects.argoproj.io"},
		{"argocdrepositories", "Repositories", "secrets"},
		{"argocdclusters", "Clusters", "secrets"},
	} {
		e := ""
		if kindErr != "" {
			e = "Cogpit may not list " + k.res
		}
		ks = append(ks, engine.OverviewKind{Kind: k.key, Title: k.title, Synced: true, Error: e})
	}
	m := engine.ArgoCDDashboardModel{Served: true, Synced: true, Kinds: ks, Strip: argoStrip(0, 0, 0, 0, 0, 0, 0), Error: kindErr}
	f, _ := argoDashFake(t, m)
	snap := *f.Table(engine.ArgoCDDashboard)
	snap.Error = kindErr
	snap.Columns = []model.Column{ // the Needs attention table's columns (engine: argoAttentionProfile)
		{ID: "name", Title: "Name", Type: model.ColumnText}, {ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "sync", Title: "Sync", Type: model.ColumnText}, {ID: "health", Title: "Health", Type: model.ColumnText},
		{ID: "message", Title: "Message", Type: model.ColumnText}, {ID: "age", Title: "Age", Type: model.ColumnAge},
	}
	f.setTable(engine.ArgoCDDashboard, &snap)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the page", func() bool { return sh.view.argo != nil && len(sh.view.argo.strip.Objects) > 0 })
	q.until(t, "the page's error line", func() bool {
		return sh.view.errLine.Visible() == (kindErr != "") && (kindErr == "" || sh.view.errLine.text == kindErr)
	})
	savePNG(t, w, name)
}

// modelOr is s, or other when s is empty.
func modelOr(s, other string) string {
	if s != "" {
		return s
	}
	return other
}

// textsContain reports whether one of texts contains want.
func textsContain(texts []string, want string) bool {
	for _, s := range texts {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func TestRenderFluxDashboardMayNotListDark(t *testing.T) {
	renderFluxMayNotList(t, fluxMayNotList, fluxMayNotList, "flux_dashboard_may_not_list_dark.png")
}

func TestRenderArgoDashboardMayNotListDark(t *testing.T) {
	renderArgoMayNotList(t, argoMayNotList, "argocd_dashboard_may_not_list_dark.png")
}
