package fyneui

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// fixtureReadme is a chart's README as charts write them (Cogpit's own text): headings, a badge
// image in a link, a code block, a list, a table, links absolute and relative, and an image.
const fixtureReadme = "# NGINX\n\n" +
	"NGINX Open Source is a web server that can also be used as a reverse proxy, load balancer and HTTP cache. " +
	"This chart runs it on a Kubernetes cluster with the **Helm** package manager.\n\n" +
	"[![Build status](https://ci.example.com/badge.svg)](https://ci.example.com/nginx)\n\n" +
	"## TL;DR\n\n```console\nhelm install my-release example/nginx\n```\n\n" +
	"## Prerequisites\n\n- Kubernetes 1.23+\n- Helm 3.8.0+\n- A *LoadBalancer* for the `service.type` of the same name\n\n" +
	"## Parameters\n\n| Name | Description | Value |\n|------|-------------|-------|\n" +
	"| `replicaCount` | Number of replicas | `1` |\n| `service.type` | Service type | `LoadBalancer` |\n\n" +
	"See the [upgrade notes](https://example.com/nginx/upgrade) and the [values file](values.yaml).\n\n" +
	"![Architecture diagram](docs/architecture.png)\n"

// chartDrawerFake is the charts fake with bitnami.nginx's drawer open, its README loaded.
func chartDrawerFake(t testing.TB, version string) (*fakeEngine, model.ObjectRef) {
	f := chartsFake(t)
	ref := model.ObjectRef{Context: "demo", Kind: helmChartsKind, Name: "bitnami.nginx"}
	m := f.details[ref.String()]
	engine.AddHelmChartDrawer(m, engine.HelmChartDrawer{Repository: "bitnami", Chart: "nginx", Version: version, Listed: "18.2.0",
		Versions: []string{"18.3.0-rc.1", "18.2.0", "18.1.4"},
		Readme:   engine.ChartReadme{State: engine.ReadmeLoaded, File: "README.md", Size: len(fixtureReadme), Verified: true}}, fixtureReadme)
	f.ui.Selected = []model.ObjectRef{ref}
	f.ui.Drawer = &ref
	return f, ref
}

// segmentsText is the text of rich text segments, nested lists, paragraphs, code and tables
// included, one segment after the other.
func segmentsText(segs []widget.RichTextSegment) string {
	var b strings.Builder
	for _, s := range segs {
		switch x := s.(type) {
		case *widget.ParagraphSegment:
			b.WriteString(segmentsText(x.Texts))
		case *widget.ListSegment:
			b.WriteString(segmentsText(x.Items))
		case *widget.TableSegment:
			for _, row := range append(slices.Clone(x.Rows), x.Headers) {
				for _, cell := range row {
					b.WriteString(segmentsText(cell) + " ")
				}
			}
		default:
			b.WriteString(s.Textual())
		}
	}
	return b.String()
}

// drawerParagraphs are the texts of the drawer's wrapped paragraphs.
func drawerParagraphs(sh *shell) []string {
	var out []string
	for _, p := range findAll[*paragraph](sh.drawer.details) {
		out = append(out, p.text)
	}
	return out
}

// readmeOf returns the drawer's rendered README.
func readmeOf(sh *shell) *widget.RichText {
	if rts := findAll[*widget.RichText](sh.drawer.details); len(rts) > 0 {
		return rts[0]
	}
	return nil
}

// The README renders as Markdown in its section, before the Versions table, without images: a
// badge and a diagram are their alt texts; a link opens in the browser only for http and https;
// the version picker under the title lists the versions, shows the drawer's, and a pick asks the
// engine (ui.setChartVersion), not a sync from the model.
func TestChartDrawerReadme(t *testing.T) {
	browser := recordBrowser(t)
	f, ref := chartDrawerFake(t, "18.2.0")
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the README", func() bool { return sh.drawer.last != nil && readmeOf(sh) != nil })
	rt := readmeOf(sh)
	var images, links []string
	var walk func([]widget.RichTextSegment)
	walk = func(segs []widget.RichTextSegment) {
		for _, s := range segs {
			switch x := s.(type) {
			case *widget.ImageSegment:
				images = append(images, x.Source.String())
			case *widget.HyperlinkSegment:
				links = append(links, x.Text)
				if x.OnTapped == nil {
					t.Errorf("the link %q opens without Cogpit's rule", x.Text)
				}
			case *widget.ParagraphSegment:
				walk(x.Texts)
			case *widget.ListSegment:
				walk(x.Items)
			}
		}
	}
	walk(rt.Segments)
	text := segmentsText(rt.Segments)
	if len(images) != 0 || !strings.Contains(text, "(image: Architecture diagram)") || !slices.Contains(links, "(image: Build status)") {
		t.Errorf("images %v, links %v, text %q", images, links, text)
	}
	for _, want := range []string{"NGINX", "TL;DR", "helm install my-release example/nginx", "Kubernetes 1.23+", "replicaCount"} {
		if !strings.Contains(text, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
	var titles []string
	for _, s := range sh.drawer.last.Sections {
		titles = append(titles, s.Title)
	}
	if !slices.Equal(titles, []string{model.TitleProperties, "Description", engine.ReadmeSection, "Versions"}) {
		t.Errorf("sections %v", titles)
	}

	toasts := len(sh.toasts.Objects)
	sh.openReadmeLink(&url.URL{Path: "values.yaml"})
	sh.openReadmeLink(&url.URL{Scheme: "file", Path: "/etc/passwd"})
	sh.openReadmeLink(&url.URL{Scheme: "https", Host: "example.com", Path: "/nginx/upgrade"})
	q.until(t, "the refusals", func() bool { return len(sh.toasts.Objects) >= toasts+2 })
	if got := browser.opened(); !slices.Equal(got, []string{"https://example.com/nginx/upgrade"}) {
		t.Errorf("opened %v", got)
	}

	p, _ := sh.drawer.kept[chartVersionKey].(*chartVersionPicker)
	if p == nil || !slices.Equal(p.sel.Options, []string{"18.3.0-rc.1", "18.2.0", "18.1.4"}) || p.sel.Selected != "18.2.0" || !sh.drawer.extra.Visible() {
		t.Fatalf("the version picker %+v", p)
	}
	if n := len(f.callsOf("ui.setChartVersion")); n != 0 {
		t.Errorf("showing the version dispatched %d times", n)
	}
	p.sel.SetSelected("18.1.4")
	q.until(t, "the command", func() bool { return len(f.callsOf("ui.setChartVersion")) == 1 })
	var params chartVersionParams
	if err := json.Unmarshal(normalize(f.callsOf("ui.setChartVersion")[0].Params), &params); err != nil || params.Version != "18.1.4" || !params.Ref.Same(ref) {
		t.Errorf("ui.setChartVersion %+v (%v)", params, err)
	}

	// Another kind's drawer has no picker.
	pod := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "web", Name: "web-0"}
	f.details[pod.String()] = &engine.DetailModel{Ref: pod, Title: "Pod: web-0", Sections: []model.DetailSection{}}
	f.update(func(u *engine.UIState) { u.Drawer = &pod })
	q.until(t, "the pod's drawer", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref == pod })
	if sh.drawer.extra.Visible() || sh.drawer.lead.Visible() {
		t.Error("a pod's drawer shows the chart's header parts")
	}
}

// A README that is loading, missing or failed shows what its section says; a cut one says so
// above it.
func TestChartDrawerReadmeStates(t *testing.T) {
	f, ref := chartDrawerFake(t, "18.2.0")
	m := f.details[ref.String()]
	m.HelmChart.Readme = engine.ChartReadme{State: engine.ReadmeFailed, Error: "chart nginx 18.2.0: https://charts.example.com/nginx-18.2.0.tgz: 401 Unauthorized"}
	for i, s := range m.Sections {
		if s.Title == engine.ReadmeSection {
			m.Sections[i].Items = []model.DetailItem{{Value: "The README could not be loaded: " + m.HelmChart.Readme.Error}}
		}
	}
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil })
	if readmeOf(sh) != nil || !slices.ContainsFunc(drawerParagraphs(sh), func(s string) bool { return strings.Contains(s, "401 Unauthorized") }) {
		t.Errorf("a failed README: %v", drawerParagraphs(sh))
	}

	cut := engine.ChartReadme{State: engine.ReadmeLoaded, File: "README.md", Size: 3 << 20, Truncated: true}
	next := *m
	next.Sections = slices.Clone(m.Sections)
	next.HelmChart = &engine.HelmChartDrawer{Repository: "bitnami", Chart: "nginx", Version: "18.2.0", Listed: "18.2.0", Versions: m.HelmChart.Versions, Readme: cut}
	for i, s := range next.Sections {
		if s.Title == engine.ReadmeSection {
			next.Sections[i].Items = []model.DetailItem{{Value: fixtureReadme}}
		}
	}
	f.details[ref.String()] = &next
	f.emit(engine.Event{Type: engine.EventDetail, View: helmChartsKind})
	q.until(t, "the cut README", func() bool { return readmeOf(sh) != nil })
	if !slices.ContainsFunc(drawerParagraphs(sh), func(s string) bool { return strings.Contains(s, "of 3.0 MiB") }) {
		t.Errorf("no note on the cut README: %v", drawerParagraphs(sh))
	}
}

func renderChartDrawer(t *testing.T, mode ThemeMode, name string) {
	f, _ := chartDrawerFake(t, "18.2.0")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the charts and the README", func() bool {
		return sh.view.snap != nil && len(sh.view.snap.Rows) == 7 && readmeOf(sh) != nil && sh.diag.total > 0
	})
	settle(w)
	// The README from its title on: the version picker stays in the header above it.
	sh.drawer.scroll.ScrollToOffset(fyne.NewPos(0, readmeOf(sh).Position().Y-60))
	savePNG(t, w, name)
}

func TestRenderHelmChartDrawerDark(t *testing.T) {
	renderChartDrawer(t, ThemeDark, "helm_chart_drawer_dark.png")
}
