package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Charts from repositories whose content nothing authenticates are marked in the window (H-41,
// owner 2026-10-14 evening, A14): the Charts view's Repository cell, the drawer, the Install tab,
// the install's preview, the Upgrade tab and a confirmation, each in the warning colour, from what
// the engine says (the chart object's origin, the tabs' origin, the preview's and the
// confirmation's warning).

var labWarning = kinds.HelmOriginWarning("lab", kinds.HelmOriginHTTP)

// The Repository cell shows the name, then the mark in the warning colour: "lab (http)",
// "charts.edge (insecure TLS)"; a checked repository's cell is its name alone. The drawer of such
// a chart says why.
func TestChartsViewMarksTheOrigin(t *testing.T) {
	f := chartsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the charts", func() bool { return sh.view.snap != nil && len(sh.view.snap.Rows) == 7 })
	th := sh.app.Settings().Theme()
	warning, plain := themeColor(th, colorStatusWarning), themeColor(th, theme.ColorNameForeground)
	repoCol := firstDataColumn + 5
	for name, want := range map[string][2]string{"podinfo": {"lab", "(http)"}, "edge-agent": {"charts.edge", "(insecure TLS)"}, "nginx": {"bitnami", ""}} {
		row := -1
		for i, r := range sh.view.snap.Rows {
			if r.Cells[1].Text == name {
				row = i
			}
		}
		var c *tableCell
		q.until(t, "the cell on screen", func() bool {
			settle(w)
			for _, x := range sh.view.cells {
				if x.row == row && x.col == repoCol && x.Visible() {
					c = x
				}
			}
			return c != nil
		})
		if c.text.Text != want[0] || c.text.Color != plain || c.full != strings.TrimSpace(want[0]+" "+want[1]) {
			t.Errorf("%s: the cell's name %q in %v (all %q), want %q in the foreground colour", name, c.text.Text, c.text.Color, c.full, want[0])
		}
		mark := ""
		if len(c.more) > 0 && c.more[0].Text != "" { // a narrow column cuts it with an ellipsis; the tooltip shows it whole
			mark = c.more[0].Text
			if c.more[0].Color != warning {
				t.Errorf("%s: the mark %q in %v, not the warning colour", name, mark, c.more[0].Color)
			}
		}
		if (mark == "") != (want[1] == "") || !strings.HasPrefix(want[1], strings.TrimSuffix(mark, "…")) {
			t.Errorf("%s: the mark %q, want %q", name, mark, want[1])
		}
	}
	ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "lab.podinfo"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "the drawer says why", func() bool {
		for _, p := range findAll[*pill](sh.drawer.details) {
			if p.text == labWarning && p.fg == colorStatusWarning {
				return true
			}
		}
		return false
	})
}

// originInstallTab opens an Install tab on lab's podinfo, which came over plain http.
func originInstallTab(t testing.TB, f *fakeEngine) engine.DockTab {
	t.Helper()
	tab := installTabFixture(t, f, true)
	ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "lab.podinfo"}
	tab.Ref, tab.Title = &ref, "Helm Install: podinfo"
	tab.HelmInstall.Repository, tab.HelmInstall.Chart, tab.HelmInstall.Origin = "lab", "podinfo", kinds.HelmOriginHTTP
	tab.HelmInstall.Version, tab.HelmInstall.Versions = "6.7.1", []string{"6.7.1"}
	f.ui.Dock = []engine.DockTab{tab}
	return tab
}

// The Install tab marks the origin its archive came from beside the chart and in a line under it;
// the preview says it above what the install creates. A tab whose archive came checked shows
// neither, and the marks follow the tab when a reload brings another origin.
func TestHelmInstallTabMarksTheOrigin(t *testing.T) {
	f := chartsFake(t)
	tab := originInstallTab(t, f)
	rec := &installRecorder{origin: kinds.HelmOriginHTTP, warning: labWarning, repository: "lab", chart: "podinfo", version: "6.7.1"}
	rec.install(f)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := installTabOf(t, sh, q, tab.ID)
	if !u.originBox.Visible() || u.origin.text != "http" || u.origin.fg != colorStatusWarning {
		t.Errorf("the mark beside the chart: visible %v, %q in %s", u.originBox.Visible(), u.origin.text, u.origin.fg)
	}
	if !u.originRow.Visible() || u.originNote.text != labWarning || u.originNote.color != colorStatusWarning {
		t.Errorf("the line under the chart: visible %v, %q", u.originRow.Visible(), u.originNote.text)
	}
	test.Tap(u.installBtn)
	q.until(t, "the preview", func() bool { return u.preview != nil })
	if w := u.preview.warning; w == nil || w.text != labWarning || w.color != colorStatusWarning {
		t.Errorf("the preview's warning %+v", w)
	}
	test.Tap(u.preview.cancel)
	q.until(t, "the cancel", func() bool { return !u.busy })

	// A reload that came checked (the entry changed in Helm's file) loses the marks.
	f.update(func(ui *engine.UIState) {
		hi := *ui.Dock[0].HelmInstall
		hi.Origin, hi.Digest = "", strings.Repeat("56", 32)
		ui.Dock[0].HelmInstall = &hi
	})
	q.until(t, "the marks to go", func() bool { return !u.originBox.Visible() && !u.originRow.Visible() })
}

// A checked repository's Install tab has no mark, and its preview no warning.
func TestHelmInstallTabWithoutOrigin(t *testing.T) {
	f := chartsFake(t)
	tab := installTabFixture(t, f, true)
	rec := &installRecorder{}
	rec.install(f)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := installTabOf(t, sh, q, tab.ID)
	if u.originBox.Visible() || u.originRow.Visible() {
		t.Error("a checked repository's tab is marked")
	}
	test.Tap(u.installBtn)
	q.until(t, "the preview", func() bool { return u.preview != nil })
	if u.preview.warning != nil {
		t.Errorf("a checked chart's preview warns: %q", u.preview.warning.text)
	}
	test.Tap(u.preview.cancel)
}

// The Upgrade tab marks its repository's origin beside the chart, and while Save downloads from it
// the line under the chart says what that means; on the release's own version (nothing downloaded)
// only the mark stays.
func TestHelmUpgradeTabMarksTheOrigin(t *testing.T) {
	f, _ := releasesFake(t)
	tab, _ := repoTabFixture(t, f, "lab")
	f.ui.Dock[0].Helm.Origin = kinds.HelmOriginHTTP
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := repoUpgradeTabOf(t, sh, q, tab.ID)
	if !u.originBox.Visible() || u.origin.text != "http" || !u.originRow.Visible() || u.originNote.text != labWarning {
		t.Errorf("the marks: pill %v %q, line %v %q", u.originBox.Visible(), u.origin.text, u.originRow.Visible(), u.originNote.text)
	}
	f.update(func(ui *engine.UIState) {
		c := *ui.Dock[0].Helm
		c.Version = c.ReleaseVersion
		ui.Dock[0].Helm = &c
	})
	q.until(t, "the release's own version", func() bool { u.follow(); return u.choice().Version == "4.12.1" })
	if !u.originBox.Visible() || u.originRow.Visible() {
		t.Errorf("on the release's own version: pill %v, line %v", u.originBox.Visible(), u.originRow.Visible())
	}
}

// A confirmation with a warning shows it under its message in the warning colour (an agent's
// install of a chart nothing authenticates); one without shows none.
func TestConfirmationShowsTheWarning(t *testing.T) {
	f := chartsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the charts", func() bool { return sh.view.snap != nil })
	req := engine.ConfirmRequest{Command: "helm.chart.install", Label: "Install", Danger: engine.DangerMutating, Origin: engine.OriginExternalAgent,
		Context: "demo", Namespaces: []string{"web"}, Objects: []model.ObjectRef{{Context: "demo", Kind: helm.ReleaseKind, Namespace: "web", Name: "podinfo"}},
		Effects: []engine.Effect{{Name: "Chart", Value: "lab/podinfo 6.7.1", Tone: engine.EffectNames},
			{Name: "Archive", Value: "checked against the index's digest"}, {Name: "Values", Value: "below"}},
		Warning: labWarning}
	var d *confirmDialog
	sh.do(func() { d = sh.confirmer.show(req, func(bool) {}) })
	q.drain()
	texts := paragraphs(w.Canvas().Overlays().Top())
	if !containsText(texts, labWarning, colorStatusWarning) {
		t.Errorf("the confirmation does not show the warning in the warning colour: %v", texts)
	}
	sh.do(d.d.Hide)
	req.Warning = ""
	sh.do(func() { d = sh.confirmer.show(req, func(bool) {}) })
	q.drain()
	if texts := paragraphs(w.Canvas().Overlays().Top()); containsText(texts, labWarning, colorStatusWarning) {
		t.Errorf("a confirmation without a warning shows one: %v", texts)
	}
	sh.do(d.d.Hide)
	q.drain()
}

// paragraphs lists the paragraphs under o with their colours.
func paragraphs(o fyne.CanvasObject) []*paragraph {
	if o == nil {
		return nil
	}
	return findAll[*paragraph](o)
}

func containsText(ps []*paragraph, text string, c fyne.ThemeColorName) bool {
	for _, p := range ps {
		if p.text == text && p.color == c {
			return true
		}
	}
	return false
}

// renderOriginCharts renders the Charts view with every column on screen (no drawer), the marked
// Repository cells among them, or with the drawer of lab's chart, which says why it is marked.
func renderOriginCharts(t *testing.T, mode ThemeMode, name string, drawer bool) {
	f := chartsFake(t)
	if drawer {
		ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "lab.podinfo"}
		f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the charts", func() bool {
		return sh.view.snap != nil && len(sh.view.snap.Rows) == 7 && sh.diag.total > 0 && (!drawer || sh.drawer.last != nil)
	})
	savePNG(t, w, name)
}

func TestRenderHelmChartsOriginDark(t *testing.T) {
	renderOriginCharts(t, ThemeDark, "helm_charts_origin_dark.png", false)
}
func TestRenderHelmChartDrawerOriginDark(t *testing.T) {
	renderOriginCharts(t, ThemeDark, "helm_chart_drawer_origin_dark.png", true)
}

func renderOriginInstallTab(t *testing.T, mode ThemeMode, name string, preview bool) {
	f := chartsFake(t)
	tab := originInstallTab(t, f)
	rec := &installRecorder{origin: kinds.HelmOriginHTTP, warning: labWarning, repository: "lab", chart: "podinfo", version: "6.7.1"}
	rec.install(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	u := installTabOf(t, sh, q, tab.ID)
	q.until(t, "the view", func() bool { return sh.view.snap != nil && sh.diag.total > 0 })
	if preview {
		test.Tap(u.installBtn)
		q.until(t, "the preview", func() bool { return u.preview != nil })
	}
	savePNG(t, w, name)
}

func TestRenderHelmInstallTabOriginDark(t *testing.T) {
	renderOriginInstallTab(t, ThemeDark, "helm_install_tab_origin_dark.png", false)
}
func TestRenderHelmInstallPreviewOriginDark(t *testing.T) {
	renderOriginInstallTab(t, ThemeDark, "helm_install_preview_origin_dark.png", true)
}
