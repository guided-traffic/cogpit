package fyneui

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// helmInstallDispatches are the Helm install and repository commands and parameters the shell
// dispatches, which TestDispatchedParamsMatchTheRegistry checks against the real registry.
func helmInstallDispatches() []struct {
	id     string
	params any
} {
	chart := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "bitnami.nginx"}
	return []struct {
		id     string
		params any
	}{
		{"helm.chart.installTab", installTabParams{refParams: refParams{Ref: chart}, Version: "18.1.4"}},
		{"helm.chart.preview", helmChartInstallParams{Tab: "helmInstall-1", Namespace: "web", Name: "nginx-x2k4q"}},
		{"helm.chart.install", helmChartInstallParams{Tab: "helmInstall-1", TextRevision: 2, Namespace: "web", Name: "nginx-x2k4q", CreateNamespace: true}},
		{"helm.repo.list", struct{}{}},
		{"helm.repo.update", struct{}{}},
		{"editor.setText", editorTextParams{ID: "helmInstall-1", YAML: "a: 1\n"}},
		{"ui.setChartVersion", chartVersionParams{refParams: refParams{Ref: chart}, Version: "18.1.4"}}, // the drawer's version picker
	}
}

// fixtureCharts are the Charts view's rows: six repositories' charts, one deprecated, two from
// repositories whose content nothing authenticates.
func fixtureCharts() []*unstructured.Unstructured {
	at := func(d int) time.Time { return fixedNow.Add(-time.Duration(d) * 24 * time.Hour) }
	charts := []kinds.HelmChart{
		{Repository: "bitnami", Name: "nginx", Version: "18.2.0", AppVersion: "1.27.1", Description: "NGINX Open Source is a web server that can be also used as a reverse proxy, load balancer, and HTTP cache.",
			Home: "https://example.com/charts/nginx", Icon: fixtureIconNginx, Keywords: []string{"nginx", "http", "web"}, Maintainers: []string{"Example Maintainers"}, Digest: strings.Repeat("ab", 32), Created: at(3),
			Versions: []kinds.HelmChartVersion{{Version: "18.3.0-rc.1", AppVersion: "1.27.2", Created: at(1)}, {Version: "18.2.0", AppVersion: "1.27.1", Created: at(3)}, {Version: "18.1.4", AppVersion: "1.27.0", Created: at(30)}}},
		{Repository: "bitnami", Name: "redis", Version: "20.1.3", AppVersion: "7.4.0", Description: "Redis is an open source, in-memory data structure store.", Digest: strings.Repeat("cd", 32), Created: at(5),
			Icon:     fixtureIconRedis,
			Versions: []kinds.HelmChartVersion{{Version: "20.1.3", AppVersion: "7.4.0", Created: at(5)}}},
		{Repository: "jetstack", Name: "cert-manager", Version: "v1.16.1", AppVersion: "v1.16.1", Description: "A Helm chart for cert-manager", Digest: strings.Repeat("ef", 32), Created: at(9),
			Versions: []kinds.HelmChartVersion{{Version: "v1.16.1", AppVersion: "v1.16.1", Created: at(9)}}},
		{Repository: "prometheus-community", Name: "kube-prometheus-stack", Version: "65.1.0", AppVersion: "v0.77.1", Icon: fixtureIconFailing,
			Description: "kube-prometheus-stack collects Kubernetes manifests, Grafana dashboards, and Prometheus rules.", Created: at(2),
			Versions: []kinds.HelmChartVersion{{Version: "65.1.0", AppVersion: "v0.77.1", Created: at(2)}}},
		{Repository: "stable", Name: "old-app", Version: "0.9.0", Description: "An application whose chart is deprecated", Deprecated: true,
			Versions: []kinds.HelmChartVersion{{Version: "0.9.0"}}},
		// Repositories nothing authenticates (H-41, A14): one over plain http, one with a dot in its
		// name (A36) whose certificate is not checked.
		{Repository: "lab", Name: "podinfo", Version: "6.7.1", AppVersion: "6.7.1", Description: "Podinfo Helm chart for Kubernetes",
			Digest: strings.Repeat("12", 32), Created: at(12), Origin: kinds.HelmOriginHTTP,
			Versions: []kinds.HelmChartVersion{{Version: "6.7.1", AppVersion: "6.7.1", Created: at(12)}}},
		{Repository: "charts.edge", Name: "edge-agent", Version: "2.4.0", Description: "The edge site agent",
			Digest: strings.Repeat("34", 32), Created: at(20), Origin: kinds.HelmOriginInsecureTLS,
			Versions: []kinds.HelmChartVersion{{Version: "2.4.0", Created: at(20)}}},
	}
	out := make([]*unstructured.Unstructured, 0, len(charts))
	for i, c := range charts {
		out = append(out, kinds.HelmChartObject(c, "uid-"+c.Name, string(rune('1'+i))))
	}
	return out
}

func chartsProfile(t testing.TB) *model.KindProfile {
	t.Helper()
	for _, p := range kinds.Local() {
		if p.Key == helmChartsKind {
			return p
		}
	}
	t.Fatal("no helmcharts profile")
	return nil
}

// chartsFake is the Releases fake with the Charts view open and its rows and drawers.
func chartsFake(t testing.TB) *fakeEngine {
	f, _ := releasesFake(t)
	f.kinds = append(f.kinds, kindInfo{Kind: helmChartsKind, Title: "Charts", APIKind: "HelmChart", Group: model.LocalGroup, Version: "v1", Local: true})
	p := chartsProfile(t)
	objs := fixtureCharts()
	rows := make([]model.Row, 0, len(objs))
	for _, o := range objs {
		rows = append(rows, model.BuildRow(p, "demo", o))
		ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: o.GetName()}
		m, err := engine.BuildDetail(p, ref, o, nil, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		f.details[ref.String()] = m
	}
	slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Cells[1].Text, b.Cells[1].Text) })
	f.tables[helmChartsKind] = &engine.TableSnapshot{View: helmChartsKind, Context: "demo", Title: p.Title, Columns: p.Columns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true}
	f.ui.View = helmChartsKind
	f.ui.Drawer, f.ui.Selected = nil, nil // the Releases fake opens a release's drawer
	f.handlers["helm.repo.list"] = func(context.Context, engine.Invocation) (any, error) {
		return fixtureHelmRepos(), nil
	}
	return f
}

// fixtureHelmRepos is Helm's repositories file as helm.repo.list reports it: public repositories,
// one behind a login whose download failed, one over plain http, one with an unchecked
// certificate and a client certificate, one Cogpit does not use.
func fixtureHelmRepos() engine.HelmReposList {
	return engine.HelmReposList{File: "/Users/me/Library/Preferences/helm/repositories.yaml", Status: engine.HelmFileOK,
		Repositories: []engine.HelmRepoInfo{
			{Name: "bitnami", URL: "https://charts.example.com/bitnami", Charts: 2, Updated: fixedNow.Add(-4 * time.Minute)},
			{Name: "jetstack", URL: "https://charts.example.com/jetstack", Charts: 1, Updated: fixedNow.Add(-4 * time.Minute)},
			{Name: "private", URL: "https://charts.internal.example/private", HasCredentials: true,
				Error: "downloading the index: https://charts.internal.example/private/index.yaml: 401 Unauthorized"},
			{Name: "lab", URL: "http://charts.lab.example:8080/stable", HasCredentials: true, PlainHTTP: true, Charts: 7, Updated: fixedNow.Add(-2 * time.Hour)},
			{Name: "charts.edge", URL: "https://charts.edge.example", HasCredentials: true, ClientCertificate: true, CAFile: true, InsecureSkipTLSVerify: true, Charts: 3, Updating: true},
			{Name: "my repo", URL: "https://charts.example.com/spaced", Unused: true,
				Error: "a repository Cogpit uses has a name of 1 to 63 letters, digits, '.', '-' or '_', the first a letter or a digit; remove it with helm repo remove and add it again under such a name"},
		}}
}

// installTabFixture is an open Helm Install tab on bitnami's nginx in the charts fake.
func installTabFixture(t testing.TB, f *fakeEngine, verified bool) engine.DockTab {
	t.Helper()
	ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "bitnami.nginx"}
	tab := engine.DockTab{ID: "helmInstall-1", Type: engine.DockHelmInstall, Title: "Helm Install: nginx", Context: "demo", Ref: &ref,
		HelmInstall: &engine.HelmInstallTab{Repository: "bitnami", Chart: "nginx", Version: "18.2.0", AppVersion: "1.27.1",
			Versions: []string{"18.3.0-rc.1", "18.2.0", "18.1.4"}, Digest: strings.Repeat("ab", 32), Verified: verified, Namespace: "web", ReleaseName: "nginx-x2k4q"}}
	ed := newEditorFake(f)
	ed.texts[tab.ID] = "## @param replicaCount Number of NGINX replicas to deploy\nreplicaCount: 1\n\nimage:\n  registry: registry.example\n  repository: nginx\n  tag: 1.27.1\n\nservice:\n  ## @param service.type Service type\n  type: LoadBalancer\n  ports:\n    http: 80\n"
	ed.revs[tab.ID] = 1
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
	return tab
}

// installRecorder answers helm.chart.preview and helm.chart.install for a fake and records them.
type installRecorder struct {
	mu       sync.Mutex
	previews []helmChartInstallParams
	installs []helmChartInstallParams
	// origin and warning are what the preview says of the chart's origin (H-41); empty: checked.
	origin, warning string
	// repository, chart and version name the previewed chart; empty: bitnami/nginx 18.2.0.
	repository, chart, version string
}

func (r *installRecorder) install(f *fakeEngine) {
	f.handlers["helm.chart.preview"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p helmChartInstallParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.previews = append(r.previews, p)
		origin, warning := r.origin, r.warning
		repository, chart, version := cmp.Or(r.repository, "bitnami"), cmp.Or(r.chart, "nginx"), cmp.Or(r.version, "18.2.0")
		r.mu.Unlock()
		return installPreview{Release: model.ObjectRef{Context: "demo", Kind: helm.ReleaseKind, Namespace: p.Namespace, Name: p.Name},
			Repository: repository, Chart: chart, Version: version, Verified: true, Origin: origin, Warning: warning,
			Objects: []helm.RenderedObject{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: p.Namespace, Name: p.Name + "-server-block"},
				{APIVersion: "apps/v1", Kind: "Deployment", Namespace: p.Namespace, Name: p.Name},
				{APIVersion: "v1", Kind: "Service", Namespace: p.Namespace, Name: p.Name},
				{APIVersion: "v1", Kind: "ServiceAccount", Namespace: p.Namespace, Name: p.Name},
			},
			Hooks: []helm.RenderedObject{{APIVersion: "v1", Kind: "Pod", Namespace: p.Namespace, Name: p.Name + "-test"}}}, nil
	}
	f.handlers["helm.chart.install"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p helmChartInstallParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.installs = append(r.installs, p)
		r.mu.Unlock()
		return map[string]any{"action": "installed", "revision": 1}, nil
	}
}

func (r *installRecorder) recorded() ([]helmChartInstallParams, []helmChartInstallParams) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.previews), slices.Clone(r.installs)
}

func installTabOf(t testing.TB, sh *shell, q *mainQueue, id string) *helmInstallTab {
	t.Helper()
	var u *helmInstallTab
	q.until(t, "the install tab", func() bool {
		if c := sh.dock.contents[id]; c != nil {
			u, _ = c.editor.(*helmInstallTab)
		}
		return u != nil && !u.busy && strings.Contains(u.pane.view.Text(), "replicaCount")
	})
	return u
}

// The Helm group lists Charts before Releases; the Charts view shows Lens's columns.
func TestChartsView(t *testing.T) {
	f := chartsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	// The sidebar's groups come with kinds.list, which can answer after the view's rows (CI on
	// Windows, 2026-10-22).
	q.until(t, "the charts", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.snap.Rows) == 7 })
	if kids := sh.side.children[groupPrefix+"helm"]; !slices.Equal(kids, []string{helmChartsKind, helm.ReleaseKind}) {
		t.Errorf("Helm group %v", kids)
	}
	var titles []string
	for _, c := range sh.view.snap.Columns {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, ",") != "Icon,Name,Description,Version,App Version,Repository" {
		t.Errorf("columns %v", titles)
	}
	if menu := sh.helmMenu(); len(menu.Items) != 3 || menu.Items[0].Label != "Repositories…" {
		t.Errorf("Helm menu %+v", menu.Items)
	}
}

// Install sends the text, previews the install and shows what it creates; the dialog's Install
// installs at the text revision the send made, with the namespace, name and checkbox of the tab.
// Cancel installs nothing.
func TestHelmInstallTabInstalls(t *testing.T) {
	f := chartsFake(t)
	tab := installTabFixture(t, f, true)
	rec := &installRecorder{}
	rec.install(f)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := installTabOf(t, sh, q, tab.ID)
	if u.namespace.Text != "web" || u.release.Text != "nginx-x2k4q" || u.version.Selected != "18.2.0" || len(u.version.Options) != 3 ||
		u.digest.text != "digest verified" || u.createNS.Checked {
		t.Errorf("fields: %q %q %q %v %q", u.namespace.Text, u.release.Text, u.version.Selected, u.version.Options, u.digest.text)
	}

	u.pane.view.SetText("replicaCount: 2\n")
	test.Tap(u.installBtn)
	q.until(t, "the preview", func() bool { return u.preview != nil })
	if previews, installs := rec.recorded(); len(previews) != 1 || len(installs) != 0 || previews[0].Tab != tab.ID || previews[0].Name != "nginx-x2k4q" {
		t.Fatalf("previews %+v installs %+v", previews, installs)
	}
	if len(u.preview.lines) != 5 || u.preview.lines[0] != "ConfigMap → web/nginx-x2k4q-server-block" || u.preview.lines[4] != "Pod (hook) → web/nginx-x2k4q-test" {
		t.Errorf("preview lines %q", u.preview.lines)
	}
	test.Tap(u.preview.cancel)
	q.until(t, "the cancel", func() bool { return !u.busy })
	if _, installs := rec.recorded(); len(installs) != 0 || u.status.text != "Not installed." {
		t.Errorf("cancel installed %+v (%q)", installs, u.status.text)
	}

	u.release.SetText("my-nginx")
	u.createNS.SetChecked(true)
	test.Tap(u.installBtn)
	q.until(t, "the preview", func() bool { return u.preview != nil })
	test.Tap(u.preview.install)
	q.until(t, "the install", func() bool { _, ins := rec.recorded(); return len(ins) == 1 && !u.busy })
	_, installs := rec.recorded()
	in := installs[0]
	if in.Tab != tab.ID || in.Name != "my-nginx" || in.Namespace != "web" || !in.CreateNamespace || in.TextRevision == 0 {
		t.Errorf("install %+v", in)
	}
	var order []string
	for _, c := range f.calls {
		if c.ID == "editor.setText" || strings.HasPrefix(c.ID, "helm.chart.") {
			order = append(order, c.ID)
		}
	}
	if strings.Join(order, ",") != "editor.setText,helm.chart.preview,editor.setText,helm.chart.preview,helm.chart.install" {
		t.Errorf("order %v", order)
	}
	if !strings.Contains(u.status.text, "Installed release my-nginx in namespace web (revision 1)") {
		t.Errorf("status %q", u.status.text)
	}
}

// The repositories dialog lists Helm's file's repositories read-only: each with its marks, its
// charts and the age of its index or its error, the file above; Update All sends helm.repo.update;
// EventHelmRepos reloads the open dialog; a missing file shows the hint. No Add, no Remove.
func TestHelmReposDialog(t *testing.T) {
	f := chartsFake(t)
	var mu sync.Mutex
	lists, updates := 0, 0
	list := fixtureHelmRepos()
	f.handlers["helm.repo.list"] = func(context.Context, engine.Invocation) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		lists++
		return list, nil
	}
	f.handlers["helm.repo.update"] = func(context.Context, engine.Invocation) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		updates++
		return []engine.HelmRepoInfo{list.Repositories[0], list.Repositories[2]}, nil
	}
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.showHelmRepos()
	rd := sh.helmRepos
	q.until(t, "the repositories", func() bool { return len(rd.rows) == 6 })
	if s := repoSummary(rd.rows[0], fixedNow); s != "2 charts · index downloaded 4m ago" {
		t.Errorf("summary %q", s)
	}
	if s := repoSummary(rd.rows[4], fixedNow); s != "3 charts · updating…" {
		t.Errorf("summary %q", s)
	}
	marks := func(i int) string {
		var out []string
		for _, m := range repoMarks(rd.rows[i]) {
			out = append(out, m.text)
		}
		return strings.Join(out, ",")
	}
	for i, want := range map[int]string{0: "", 2: "credentials", 3: "credentials,http", 4: "client certificate,insecure TLS", 5: "not used"} {
		if got := marks(i); got != want {
			t.Errorf("%s: marks %q, want %q", rd.rows[i].Name, got, want)
		}
	}
	if !strings.Contains(rd.file.text, "repositories.yaml") {
		t.Errorf("file line %q", rd.file.text)
	}
	for _, label := range []string{"Add", "Remove"} {
		if findButton(w, label) != nil {
			t.Errorf("a %s button", label)
		}
	}
	test.Tap(rd.updateB)
	q.until(t, "the update", func() bool { mu.Lock(); defer mu.Unlock(); return updates == 1 })
	q.until(t, "the update's status", func() bool { return strings.Contains(rd.status.text, "1 of 2 repositories failed") })

	mu.Lock()
	before := lists
	list = engine.HelmReposList{File: list.File, Status: engine.HelmFileMissing, Hint: "Add repositories with helm repo add <name> <url>; Cogpit lists them within seconds."}
	mu.Unlock()
	f.emit(engine.Event{Type: engine.EventHelmRepos})
	q.until(t, "the reload", func() bool { return len(rd.rows) == 0 })
	mu.Lock()
	if lists <= before {
		t.Errorf("EventHelmRepos did not reload (%d lists)", lists)
	}
	mu.Unlock()
	if rd.updateB.Disabled() == false {
		t.Error("Update All is enabled without repositories")
	}
	if len(rd.fileMsg.Objects) != 1 {
		t.Errorf("the missing file's message: %d objects", len(rd.fileMsg.Objects))
	}
}

func renderCharts(t *testing.T, mode ThemeMode, name string) {
	f := chartsFake(t)
	ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "bitnami.nginx"}
	f.ui.Selected = []model.ObjectRef{ref}
	f.ui.Drawer = &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the charts and the drawer", func() bool {
		return sh.view.snap != nil && len(sh.view.snap.Rows) == 7 && sh.drawer.last != nil && sh.diag.total > 0
	})
	savePNG(t, w, name)
}

func TestRenderHelmChartsDark(t *testing.T)  { renderCharts(t, ThemeDark, "helm_charts_dark.png") }
func TestRenderHelmChartsLight(t *testing.T) { renderCharts(t, ThemeLight, "helm_charts_light.png") }

func renderInstallTab(t *testing.T, mode ThemeMode, name string, preview bool) {
	f := chartsFake(t)
	tab := installTabFixture(t, f, true)
	rec := &installRecorder{}
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

func TestRenderHelmInstallTabDark(t *testing.T) {
	renderInstallTab(t, ThemeDark, "helm_install_tab_dark.png", false)
}
func TestRenderHelmInstallTabLight(t *testing.T) {
	renderInstallTab(t, ThemeLight, "helm_install_tab_light.png", false)
}
func TestRenderHelmInstallPreviewDark(t *testing.T) {
	renderInstallTab(t, ThemeDark, "helm_install_preview_dark.png", true)
}

func renderHelmReposDialog(t *testing.T, mode ThemeMode, name string) {
	f := chartsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the charts", func() bool { return sh.view.snap != nil && sh.diag.total > 0 })
	sh.showHelmRepos()
	q.until(t, "the repositories", func() bool { return sh.helmRepos != nil && len(sh.helmRepos.rows) == 6 })
	savePNG(t, w, name)
}

func TestRenderHelmReposDialog(t *testing.T) {
	renderHelmReposDialog(t, ThemeDark, "helm_repos_dialog_dark.png")
}
func TestRenderHelmReposDialogLight(t *testing.T) {
	renderHelmReposDialog(t, ThemeLight, "helm_repos_dialog_light.png")
}

// A version picked for the Install tab reaches the window when the engine has loaded its chart:
// the picker and the digest line follow the tab's state (dockEqual compared neither, review
// 2026-10-13).
func TestHelmInstallTabFollowsItsVersion(t *testing.T) {
	f := chartsFake(t)
	tab := installTabFixture(t, f, true)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := installTabOf(t, sh, q, tab.ID)
	f.update(func(ui *engine.UIState) {
		for i := range ui.Dock {
			if ui.Dock[i].ID == tab.ID {
				hi := *ui.Dock[i].HelmInstall
				hi.Version, hi.Digest, hi.Verified = hi.Versions[len(hi.Versions)-1], "", false
				ui.Dock[i].HelmInstall = &hi
			}
		}
	})
	want := tab.HelmInstall.Versions[len(tab.HelmInstall.Versions)-1]
	q.until(t, "the tab's version", func() bool { return u.version.Selected == want })
	if u.digest.text == "digest verified" {
		t.Errorf("digest line %q after a version without a verified digest", u.digest.text)
	}
}

// An open Install tab's editor follows the Preferences' editor settings, as the Edit, Create
// resource and Upgrade tabs' do (editorViews listed the three, not the Install tab, merged before
// the Preferences page).
func TestHelmInstallTabFollowsTheEditorPreferences(t *testing.T) {
	f := chartsFake(t)
	tab := installTabFixture(t, f, true)
	prefs := engine.DefaultPreferences()
	f.ui.Preferences = &prefs
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	u := installTabOf(t, sh, q, tab.ID)
	if v := u.pane.view; v.TextSize() != engine.DefaultFontSize || !v.LineNumbers() || v.TabSize() != engine.DefaultTabSize {
		t.Fatalf("opened with size %v, numbers %v, tab %d", v.TextSize(), v.LineNumbers(), v.TabSize())
	}
	f.update(func(ui *engine.UIState) {
		p := engine.DefaultPreferences()
		p.EditorFontSize, p.EditorLineNumbers, p.EditorTabSize = 18, false, 4
		ui.Preferences = &p
	})
	q.until(t, "the editor's preferences", func() bool {
		v := u.pane.view
		return v.TextSize() == 18 && !v.LineNumbers() && v.TabSize() == 4
	})
}
