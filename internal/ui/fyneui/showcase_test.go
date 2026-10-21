package fyneui

import (
	"context"
	"crypto/sha256"
	hexenc "encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The showcase renders: the pictures of the README (docs/images/*.png), Cogpit's main features
// drawn offscreen over the test fixtures (the demo-like cluster, the metrics fakes, the Helm, Flux
// and Argo CD fixtures), with the fixtures' fixed clock and the status bar's fixed counters, so
// that a new run draws the same pictures. They assert nothing the testdata renders do not: they
// reuse those scenes and only arrange them for a picture (a tab bar, a wider dock, demo data that
// reads well), and no fixture value that looks like a secret.
//
// Every scene is drawn twice, in the dark and the light theme, into <name>_dark.png and
// <name>_light.png: the README shows the one that matches the reader's GitHub theme through
// <picture> (A110). The connected cluster is the same context in every picture, demo (A114).
//
// They run only when asked: with -showcase they write docs/images; with COGPIT_PNG_DIR they write
// there, to look without touching the tree. Otherwise they are skipped, so that CI's race shards
// draw no pictures nobody looks at. make showcase runs them and then oxipng over docs/images,
// lossless (A112).
//
//	GOFLAGS=-p=4 go test ./internal/ui/fyneui -run TestShowcase -showcase
var showcasePNG = flag.Bool("showcase", false, "write the README's showcase renders into docs/images")

const (
	// showcaseDir is docs/images, from this package's directory.
	showcaseDir = "../../../docs/images"
	// showcaseScale draws the 1440×900 window at twice its size, as a Retina display shows it, so
	// that GitHub's scaled-down picture stays sharp and a click on it shows crisp text.
	showcaseScale = 2
	// showcaseContext is the connected cluster's context in every picture (A114): the fixtures'
	// one, the name of the demo cluster a reader can open in Cogpit, and nothing that names a
	// person, a company or Cogpit's own test clusters. The GitOps scenes, whose fixtures connect
	// "kind-flux" and "argo", show it too (renamedConnector).
	showcaseContext = "demo"
)

// showcaseOnly skips a showcase render unless -showcase or COGPIT_PNG_DIR asks for it.
func showcaseOnly(t *testing.T) {
	t.Helper()
	if !*showcasePNG && os.Getenv("COGPIT_PNG_DIR") == "" {
		t.Skip("a showcase render: run with -showcase (docs/images) or COGPIT_PNG_DIR")
	}
}

// eachTheme draws a scene once per theme, as the subtests dark and light (A110).
func eachTheme(t *testing.T, scene func(t *testing.T, mode ThemeMode)) {
	t.Helper()
	showcaseOnly(t)
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) { scene(t, mode) })
	}
}

// saveShowcase captures the 1440×900 window at showcaseScale into docs/images (or
// COGPIT_PNG_DIR) as <name>_<mode>.png.
func saveShowcase(t *testing.T, w fyne.Window, name string, mode ThemeMode) {
	t.Helper()
	saveShowcaseSized(t, w, name, mode, fyne.NewSize(1440, 900))
}

// saveShowcaseSized captures the window, which must be at least the size least, at showcaseScale.
func saveShowcaseSized(t *testing.T, w fyne.Window, name string, mode ThemeMode, least fyne.Size) {
	t.Helper()
	file := name + "_" + string(mode) + ".png"
	// The README is the showcase page: no build's version beside the wordmark, which a test build
	// would show as "dev" (F12, the lead's decision).
	for _, v := range findAll[*buildLabel](w.Content()) {
		v.conceal()
	}
	// Nor the alerts pill (D-54, the lead's decision; its source since 2026-10-17 the cluster's
	// Alertmanager, D-77, which the showcase's clusters do not run).
	for _, a := range findAll[*alertsButton](w.Content()) {
		a.conceal()
	}
	settle(w)
	c, ok := w.Canvas().(interface{ SetScale(float32) })
	if !ok {
		t.Fatalf("the test window's canvas %T cannot scale", w.Canvas())
	}
	c.SetScale(showcaseScale)
	defer c.SetScale(1)
	img := w.Canvas().Capture()
	if b := img.Bounds(); b.Dx() < int(least.Width)*showcaseScale || b.Dy() < int(least.Height)*showcaseScale {
		t.Fatalf("%s: captured %v, want at least %v×%v", file, b, least.Width*showcaseScale, least.Height*showcaseScale)
	}
	dir := os.Getenv("COGPIT_PNG_DIR")
	if *showcasePNG {
		dir = showcaseDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// scrollRowToTop scrolls the view's list so that row is its first visible row, whole: down to the
// last of n rows first, then up to row, which a table shows at its top edge, instead of a row
// half under the header as a scroll down to a row leaves it.
func scrollRowToTop(sh *shell, n, row int) {
	sh.view.scrollTo(n - 1)
	sh.view.scrollTo(max(0, row))
}

// showcaseCounters shows the fake's fixed status-bar counters in a scene on the real engine, whose
// in-memory connector sends no request: a picture of zeros reads like a broken session. Call it
// after the scene's last drain, right before saveShowcase: a fetch still on its way lands only on
// the next drain, after the capture.
func showcaseCounters(sh *shell, context string) {
	d := fixtureDiagnostics()
	d.Context, d.Demo, d.DemoStats = context, false, nil
	sh.diag = newDiagView(d)
	sh.status.update()
}

// showcaseContexts are the kubeconfig contexts the showcase's navigator and hotbar show: the
// connected showcaseContext and four invented ones that read like a team's clusters, none of them
// a real cluster's, a person's or Cogpit's own test cluster's (kind-cogpit; A114).
func showcaseContexts() []engine.ContextInfo {
	return []engine.ContextInfo{
		{Name: showcaseContext, Demo: true},
		{Name: "prod-eu-central", Cluster: "prod-eu-central", Server: "https://api.prod-eu-central.example.com:6443", Namespace: "platform"},
		{Name: "staging-us-east", Cluster: "staging-us-east", Server: "https://api.staging-us-east.example.com:6443"},
		{Name: "kind-local", Cluster: "kind-local", User: "kind-local", Server: "https://127.0.0.1:52341", Current: true},
		{Name: "dev-us-west", Cluster: "dev-us-west", Server: "https://api.dev-us-west.example.com:6443", Namespace: "default"},
	}
}

// showcaseTitles are the views' titles in the tab bar.
var showcaseTitles = map[string]string{
	engine.ClusterOverview: "Overview", "pods": "Pods", "nodes": "Nodes", "deployments": "Deployments",
	"services": "Services", "secrets": "Secrets", "events": "Events", helm.ReleaseKind: "Releases",
	helmChartsKind: "Charts",
}

// showcaseTabs gives a scene the tab bar a working session has: a few views of the connected
// cluster, the first one active.
func showcaseTabs(f *fakeEngine, views ...string) {
	ctx := f.ui.Context
	for i, v := range views {
		id := fmt.Sprintf("tab-%d", i+1)
		f.ui.Tabs = append(f.ui.Tabs, engine.ViewTab{ID: id, Context: ctx, View: v,
			Title: showcaseTitles[v] + " - " + engine.ContextLabel(ctx), Sort: engine.DefaultSort})
	}
	f.ui.ActiveTab = "tab-1"
}

// 1. The hero: the navigator, the Pods list with CPU and Memory, a pod's drawer with its charts
// from Prometheus (history, requests and limits, a chart per container).
func TestShowcaseHero(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		p := showcasePod(t)
		f := kindsFake(t, "pods")
		withUsageCells(t, f, "pods")
		ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
		m := podMetricsFixture(ref)
		withEnvoy(m)
		withPrometheus(m)
		f.metrics = map[string]*engine.ObjectMetrics{ref.String(): m}
		withRowUsage(t, f, "pods", p.name, *m.Targets[0].Current)
		f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
		withRangeCommand(f)
		withVariantCommand(f)
		showcaseTabs(f, "pods", engine.ClusterOverview, "deployments")
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the metrics", func() bool {
			pm, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
			return ok && pm.data != nil && sh.drawer.last != nil && sh.diag.total > 0 && len(sh.tabs.shown) == 3
		})
		settle(w)
		// The selected row in the middle of the list, not at its edge where the drawer's opening
		// scrolled it.
		scrollRowToTop(sh, len(f.tables["pods"].Rows), rowOf(t, f, "pods", p.name)-13)
		saveShowcase(t, w, "hero", mode)
	})
}

// showcasePod is a running fixture pod with the containers app and envoy in a namespace that
// reads as an application's.
func showcasePod(t *testing.T) podSpec {
	t.Helper()
	for _, p := range fixturePodSpecs() {
		if p.state == podOK && slices.Equal(p.containers, []string{"app", "envoy"}) && p.ns == "inventory" {
			return p
		}
	}
	return twoContainerPod(t)
}

// withRowUsage puts a usage into one row's CPU and Memory cells, as the engine's row cache does.
func withRowUsage(t *testing.T, f *fakeEngine, key, name string, u model.Usage) {
	t.Helper()
	p, _ := kinds.ByKey(key)
	snap := *f.tables[key]
	snap.Rows = slices.Clone(snap.Rows)
	cpu, mem := p.ColumnIndex(model.ColumnCPU), p.ColumnIndex(model.ColumnMemory)
	for i, r := range snap.Rows {
		if r.Ref.Name != name {
			continue
		}
		var obj *unstructured.Unstructured
		for _, o := range fixturePodObjects() {
			if o.GetName() == name {
				obj = o
			}
		}
		cells := slices.Clone(r.Cells)
		cells[cpu], cells[mem] = p.UsageCells(obj, u)
		snap.Rows[i].Cells = cells
	}
	f.tables[key] = &snap
}

// 2. The cluster Overview: CPU, Memory and Pods of the worker nodes over the last hour (the Pods'
// history from kube-state-metrics), and the Issues below.
func TestShowcaseClusterOverview(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f := clusterFakeWithPodsHistory(t, metrics.PodsFromKubeStateMetrics)
		showcaseTabs(f, engine.ClusterOverview, "pods", "nodes")
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the page", func() bool {
			return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.cluster.data != nil && sh.diag.total > 0
		})
		saveShowcase(t, w, "cluster_overview", mode)
	})
}

// 3. The Nodes list with Lens's usage bars, and a node's drawer: its charts, its Pods with CPU and
// Memory.
func TestShowcaseNodes(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f, ref := nodePodsSortFake(t, map[string]engine.SortSpec{"Pods": {Column: model.ColumnCPU, Desc: true}})
		withNodeBars(t, f)
		registerProbes(t, f)
		showcaseTabs(f, "nodes", engine.ClusterOverview)
		fakeTabCommands(t, f)
		u := barNodeUsage[ref.Name]
		f.metrics = map[string]*engine.ObjectMetrics{ref.String(): {
			Ref: ref, Status: metrics.StatusAvailable, Source: "Prometheus (monitoring/prometheus-server) and Kubernetes Metrics Server",
			Capabilities: metrics.Capabilities{History: true},
			Targets: []engine.MetricsTarget{{
				Current: &u, At: fixedNow, Samples: nodeSeries(u),
				Allocatable: model.Usage{CPUMilli: 7800, MemoryBytes: 30 << 30}, Capacity: model.Usage{CPUMilli: 8000, MemoryBytes: 32 << 30},
			}},
			Disk: &model.NodeDisk{Name: ref.Name, UsedBytes: 172 << 30, SizeBytes: 200 << 30},
		}}
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the drawer", func() bool {
			p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
			return ok && p.data != nil && len(sh.kinds) > 0 && sh.drawer.last != nil && sh.diag.total > 0 && len(sh.tabs.shown) == 2
		})
		settle(w)
		scrollDrawerTo(t, sh, "Allocatable")
		saveShowcase(t, w, "nodes", mode)
	})
}

// nodeSeries is an hour of a node's usage, ending at u.
func nodeSeries(u model.Usage) []model.UsageSample {
	const n = 240
	return metricsSeries(n, func(i int) model.Usage {
		f := 0.75 + 0.25*float64(i)/n
		wave := 0.04 * float64((i*7)%13-6) / 6
		return model.Usage{CPUMilli: int64(float64(u.CPUMilli) * (f + wave)), MemoryBytes: int64(float64(u.MemoryBytes) * (0.9 + 0.1*float64(i)/n))}
	})
}

// 4. Logs: a Deployment's pods followed in one tab, each line with its pod and container, a search
// highlighted; a pod shell and a terminal in the dock beside it.
func TestShowcaseLogs(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f := kindsFake(t, "deployments")
		f.tables["deployments"] = kindSnapshot(t, "deployments", showcaseDeployments())
		f.quietTerminals = true
		pod := model.ObjectRef{Context: "demo", Kind: kindPods, Namespace: "checkout", Name: "checkout-api-7d9f8b6c4-x2k9p"}
		dep := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
		f.ui.Selected = []model.ObjectRef{dep}
		withLogCommands(t, f, dep)
		f.ui.Dock = []engine.DockTab{
			{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: Deployment checkout-api", Context: "demo", Ref: &dep,
				Logs: engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "error"}},
			{ID: "shell-2", Type: engine.DockShell, Title: "Shell: " + pod.Name + "/api", Context: "demo", Ref: &pod, Container: "api"},
			{ID: "terminal-3", Type: engine.DockTerminal, Title: "Terminal: " + showcaseContext, Context: "demo"},
		}
		f.ui.ActiveDock = "logs-1"
		showcaseTabs(f, "deployments", "pods")
		fakeTabCommands(t, f)
		sh, w, q := startShell(t, newLogsEngine(f), f, Options{Theme: mode})
		sh.vsplit.SetOffset(0.5)
		q.until(t, "the log lines", func() bool {
			lt := logTabOf(sh)
			return len(sh.kinds) > 0 && lt != nil && lt.view != nil && len(lt.view.ShownLines()) == len(demoMerged) && sh.diag.total > 0
		})
		saveShowcase(t, w, "logs", mode)
	})
}

// showcaseShellOutput is a session in a search-indexer pod's app container.
func showcaseShellOutput(pod string) string {
	return "/app $ hostname\r\n" + pod + "\r\n" +
		"/app $ wget -qO- localhost:8080/healthz\r\n{\"status\":\"ok\",\"index\":\"products\",\"documents\":184213}\r\n" +
		"/app $ env | grep ^SEARCH_\r\nSEARCH_BACKEND_URL=http://opensearch.search.svc:9200\r\nSEARCH_LOG_LEVEL=info\r\n" +
		"/app $ ls /etc/search-indexer\r\nconfig.yaml  synonyms.txt\r\n/app $ "
}

// showcaseShellEngine is the fake engine whose shell tabs stream showcaseShellOutput.
type showcaseShellEngine struct {
	*fakeEngine
	pod  string
	last *atomic.Pointer[openStream]
}

func (e showcaseShellEngine) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	if !strings.HasPrefix(tabID, string(engine.DockShell)+"-") {
		return e.fakeEngine.OpenTerminal(ctx, tabID)
	}
	e.mu.Lock()
	e.opened[tabID]++
	e.mu.Unlock()
	s := newGatedStream(e.fakeEngine, tabID, showcaseShellOutput(e.pod))
	e.last.Store(s)
	return s, nil
}

// 5. A pod shell in the dock, under the Pods list with the pod selected.
func TestShowcaseShell(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		p := showcasePod(t)
		f := kindsFake(t, "pods")
		withUsageCells(t, f, "pods")
		pod := model.ObjectRef{Context: "demo", Kind: kindPods, Namespace: p.ns, Name: p.name}
		f.ui.Selected = []model.ObjectRef{pod}
		f.ui.Dock = []engine.DockTab{
			{ID: "shell-1", Type: engine.DockShell, Title: "Shell: " + pod.Name + "/app", Context: "demo", Ref: &pod, Container: "app"},
		}
		f.ui.ActiveDock = "shell-1"
		showcaseTabs(f, "pods", "deployments")
		fakeTabCommands(t, f)
		eng := showcaseShellEngine{fakeEngine: f, pod: pod.Name, last: &atomic.Pointer[openStream]{}}
		sh, w, q := startShell(t, eng, f, Options{Theme: mode})
		sh.vsplit.SetOffset(0.52)
		q.until(t, "the terminal to read", func() bool {
			s := eng.last.Load()
			return len(sh.kinds) > 0 && sh.view.snap != nil && s != nil && s.reading.Load() && sh.diag.total > 0
		})
		q.drain()
		s := eng.last.Load()
		close(s.gate)
		deadline := time.Now().Add(5 * time.Second)
		for !s.idle.Load() {
			if time.Now().After(deadline) {
				t.Fatal("the terminal did not take the shell's output")
			}
			time.Sleep(time.Millisecond)
		}
		q.drain()
		settle(w)
		scrollRowToTop(sh, len(f.tables["pods"].Rows), rowOf(t, f, "pods", pod.Name)-6)
		saveShowcase(t, w, "shell", mode)
	})
}

// 6. Editing YAML: an Edit tab reviewing its change as a diff after the server's dry run accepted
// it, under the Services list.
func TestShowcaseEditDiff(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f := kindsFake(t, "services")
		f.kinds = append(f.kinds, kindInfo{Kind: "services", Title: "Services", APIKind: "Service", Version: "v1", Namespaced: true})
		svcs := append(fixtureServiceObjects(), &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]any{"name": "checkout", "namespace": "storefront", "uid": "uid-svc-checkout", "creationTimestamp": ts(40 * 24 * time.Hour)},
			"spec": map[string]any{"type": "ClusterIP", "clusterIP": "10.96.100.100", "selector": map[string]any{"app": "checkout"},
				"ports": []any{map[string]any{"name": "http", "port": int64(80), "targetPort": int64(8080), "protocol": "TCP"}}},
		}})
		f.tables["services"] = kindSnapshot(t, "services", svcs)
		f.ui.View = "services"
		ed := newEditorFake(f)
		r := checkoutRef
		tab := engine.DockTab{ID: "edit-1", Type: engine.DockEdit, Title: "Service checkout", Context: "demo", Ref: &r}
		f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
		f.ui.Selected = []model.ObjectRef{r}
		ed.setText(tab.ID, serviceYAML)
		showcaseTabs(f, "services", "pods")
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		var et *editorTab
		q.until(t, "the editor to load", func() bool {
			if c, ok := sh.dock.contents[tab.ID]; ok {
				et, _ = c.editor.(*editorTab)
			}
			return et != nil && et.pane.view.Text() == serviceYAML && len(sh.kinds) > 0 && sh.diag.total > 0
		})
		sh.vsplit.SetOffset(renderDockOffset)
		typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
		et.pane.view.SetCursorPosition(13, 1<<20)
		for range 4 {
			et.pane.view.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
		}
		test.Type(et.pane.view, "9090")
		test.Tap(et.saveBtn)
		q.until(t, "the review", func() bool { return et.reviewing && !et.applyBtn.Disabled() })
		saveShowcase(t, w, "edit_diff", mode)
	})
}

// showcaseCharts are the charts of a few public repositories, as their indexes list them (the
// descriptions Cogpit's own words); each has an icon of Cogpit's drawing, no project's logo.
var showcaseCharts = []struct {
	repo, name, version, app, desc, color, glyph string
}{
	{"bitnami", "nginx", "18.2.0", "1.27.1", "NGINX Open Source is a web server that can be also used as a reverse proxy, load balancer, and HTTP cache.", "#2aa198", "square"},
	{"bitnami", "postgresql", "16.2.1", "17.2.0", "An object-relational database with a focus on standards and extensibility.", "#268bd2", "disc"},
	{"bitnami", "rabbitmq", "15.1.0", "4.0.4", "A message broker for queues, streams and publish-subscribe.", "#cb4b16", "bars"},
	{"bitnami", "redis", "20.1.3", "7.4.0", "Redis is an open source, in-memory data structure store.", "#dc322f", "diamond"},
	{"grafana", "grafana", "8.6.4", "11.3.1", "Dashboards and alerting for metrics, logs and traces.", "#b58900", "ring"},
	{"grafana", "loki", "6.24.0", "3.3.2", "Log aggregation that indexes labels, not the lines.", "#d33682", "lines"},
	{"ingress-nginx", "ingress-nginx", "4.12.1", "1.12.1", "An ingress controller that uses NGINX as a reverse proxy and load balancer.", "#859900", "arrow"},
	{"jetstack", "cert-manager", "v1.16.2", "v1.16.2", "Issues and renews TLS certificates from ACME, Vault and private CAs.", "#6c71c4", "shield"},
	{"prometheus-community", "kube-prometheus-stack", "66.3.0", "v0.79.0", "Prometheus, Alertmanager and their rules and dashboards for a whole cluster.", "#cb4b16", "flame"},
	{"prometheus-community", "prometheus-node-exporter", "4.42.0", "1.8.2", "Hardware and kernel metrics of each node.", "#93a1a1", "disc"},
}

// showcaseIcon draws a chart's icon: a rounded tile in its colour with a white glyph.
func showcaseIcon(color, glyph string) string {
	shapes := map[string]string{
		"square":  `<rect x="7" y="7" width="10" height="10" rx="1.5" fill="#fff"/>`,
		"disc":    `<circle cx="12" cy="12" r="5" fill="#fff"/>`,
		"ring":    `<circle cx="12" cy="12" r="5" fill="none" stroke="#fff" stroke-width="2.4"/>`,
		"diamond": `<path d="M12 5.5l6.5 6.5-6.5 6.5-6.5-6.5z" fill="#fff"/>`,
		"bars":    `<path d="M6.5 15h3v3h-3zM10.5 10h3v8h-3zM14.5 6h3v12h-3z" fill="#fff"/>`,
		"lines":   `<path d="M6 7.5h12v2H6zM6 11h9v2H6zM6 14.5h12v2H6z" fill="#fff"/>`,
		"arrow":   `<path d="M6 11h8V7.5l5 4.5-5 4.5V13H6z" fill="#fff"/>`,
		"shield":  `<path d="M12 5l6 2.4v4.4c0 3.6-2.6 6.2-6 7.2-3.4-1-6-3.6-6-7.2V7.4z" fill="#fff"/>`,
		"flame":   `<path d="M12 5c1 3 4.5 4.4 4.5 8.2A4.5 4.5 0 0 1 7.5 13.2c0-2 1-3.2 2.2-4.2 0 1.6.8 2.6 1.8 2.6C11.5 9.6 11 7.4 12 5z" fill="#fff"/>`,
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">` +
		`<rect width="24" height="24" rx="5" fill="` + color + `"/>` + shapes[glyph] + `</svg>`
}

// showcaseIconEngine serves the showcase charts' icons.
type showcaseIconEngine struct{ *fakeEngine }

func (e showcaseIconEngine) ChartIcon(_ context.Context, iconURL string) ([]byte, string, error) {
	for _, c := range showcaseCharts {
		if iconURL == showcaseIconURL(c.name) {
			return []byte(showcaseIcon(c.color, c.glyph)), "svg", nil
		}
	}
	return nil, "", errors.New(iconURL + ": 404 Not Found")
}

func showcaseIconURL(chart string) string {
	return "https://icons.example.com/charts/" + chart + ".svg"
}

// showcaseDigest is a chart archive's digest as an index lists it.
func showcaseDigest(repo, name, version string) string {
	sum := sha256.Sum256([]byte(repo + "/" + name + "-" + version + ".tgz"))
	return hexenc.EncodeToString(sum[:])
}

// 7. Helm charts: the charts of Helm's own repositories with their icons, and a chart's drawer
// with the README of the version picked.
func TestShowcaseHelmCharts(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f, ref := chartDrawerFake(t, "18.2.0")
		p := chartsProfile(t)
		at := func(d int) time.Time { return fixedNow.Add(-time.Duration(d) * 24 * time.Hour) }
		rows := make([]model.Row, 0, len(showcaseCharts))
		for i, c := range showcaseCharts {
			ch := kinds.HelmChart{Repository: c.repo, Name: c.name, Version: c.version, AppVersion: c.app, Description: c.desc,
				Icon: showcaseIconURL(c.name), Digest: showcaseDigest(c.repo, c.name, c.version), Created: at(2 + i*3),
				Versions: []kinds.HelmChartVersion{{Version: c.version, AppVersion: c.app, Created: at(2 + i*3)}}}
			rows = append(rows, model.BuildRow(p, "demo", kinds.HelmChartObject(ch, "uid-"+c.name, fmt.Sprint(i+1))))
		}
		slices.SortFunc(rows, func(a, b model.Row) int { return strings.Compare(a.Cells[1].Text, b.Cells[1].Text) })
		f.tables[helmChartsKind] = &engine.TableSnapshot{View: helmChartsKind, Context: "demo", Title: p.Title, Columns: p.Columns, Rows: rows,
			Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true}
		m := f.details[ref.String()]
		m.HelmChart.Icon = showcaseIconURL("nginx")
		for i := range m.Sections {
			for j, it := range m.Sections[i].Items {
				switch it.Name {
				case "Digest":
					m.Sections[i].Items[j].Value = showcaseDigest("bitnami", "nginx", "18.2.0")
				case "Home":
					m.Sections[i].Items[j].Value = "https://charts.example.com/nginx"
				}
			}
		}
		showcaseTabs(f, helmChartsKind, helm.ReleaseKind)
		fakeTabCommands(t, f)
		sh, w, q := startShell(t, showcaseIconEngine{f}, f, Options{Theme: mode})
		q.until(t, "the icons and the README", func() bool {
			cells := iconCells(sh)
			return sh.view.snap != nil && len(cells) == len(showcaseCharts) && !slices.Contains(mapResources(cells), sh.chartGlyph()) &&
				readmeOf(sh) != nil && sh.diag.total > 0 && sh.drawer.lead.Visible()
		})
		settle(w)
		sh.drawer.scroll.ScrollToOffset(fyne.NewPos(0, readmeOf(sh).Position().Y-60))
		saveShowcase(t, w, "helm_charts", mode)
	})
}

func mapResources(m map[int]fyne.Resource) []fyne.Resource {
	out := make([]fyne.Resource, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

// 8. Helm releases: a release's drawer and its Helm Upgrade tab with the repository's versions.
func TestShowcaseHelmUpgrade(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f, _ := releasesFake(t)
		f.kinds = append(f.kinds, kindInfo{Kind: helmChartsKind, Title: "Charts", APIKind: "HelmChart", Group: model.LocalGroup, Version: "v1", Local: true})
		values := f.handlers["helm.release.values"]
		f.handlers["helm.release.values"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
			res, err := values(ctx, inv)
			if v, ok := res.(helm.Values); ok { // the fixture's password: no value that looks like a secret
				var keep []string
				for _, l := range strings.SplitAfter(v.YAML, "\n") {
					if !strings.HasPrefix(l, "auth:") && !strings.Contains(l, "adminPassword") {
						keep = append(keep, l)
					}
				}
				v.YAML = strings.Join(keep, "")
				return v, err
			}
			return res, err
		}
		tab, _ := repoTabFixture(t, f, "ingress-nginx")
		showcaseTabs(f, helm.ReleaseKind, helmChartsKind)
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		u := repoUpgradeTabOf(t, sh, q, tab.ID)
		q.until(t, "the drawer", func() bool {
			rv, _ := sh.drawer.kept[releaseValuesKey].(*releaseValues)
			return sh.drawer.last != nil && sh.diag.total > 0 && rv != nil && rv.revision == 4
		})
		settle(w)
		test.Tap(u.version)
		if len(w.Canvas().Overlays().List()) == 0 {
			t.Fatal("the picker did not open")
		}
		saveShowcase(t, w, "helm_upgrade", mode)
	})
}

// renamedConnector shows a fixture connector's one context under the showcase's context name
// (A114): the GitOps fixtures connect "kind-flux" and "argo", the other scenes "demo".
type renamedConnector struct {
	engine.Connector
	name string
}

// Contexts implements engine.Connector.
func (c renamedConnector) Contexts() ([]engine.ContextInfo, error) {
	cs, err := c.Connector.Contexts()
	for i := range cs {
		cs[i].Name = c.name
	}
	return cs, err
}

// Connect implements engine.Connector: the fixture's context, its backend named c.name.
func (c renamedConnector) Connect(ctx context.Context, _ string) (*engine.Backend, error) {
	var inner string
	if cs, err := c.Connector.Contexts(); err == nil && len(cs) > 0 {
		inner = cs[0].Name
	}
	b, err := c.Connector.Connect(ctx, inner)
	if b != nil {
		b.Context = c.name
	}
	return b, err
}

// showcaseEngineShell starts the shell on the real engine and commands over conn, as the GitOps
// tests' shells do (fluxEngineShell, newArgoShell), connected to showcaseContext.
func showcaseEngineShell(t *testing.T, conn engine.Connector, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	s := engine.New(engine.Options{Connector: renamedConnector{Connector: conn, name: showcaseContext},
		SnapshotInterval: 5 * time.Millisecond, Now: func() time.Time { return fixedNow }})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{Theme: mode, AutoConnect: showcaseContext}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir
	sh.confirmer.armDelay = 0
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected })
	return sh, w, q
}

// 9. Flux CD: the dashboard of every Flux kind's states, and what is not ready.
func TestShowcaseFlux(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		conn := enginekit.MemoryConnector{Context: "kind-flux", Src: fluxFixture(), Profiles: kinds.Builtin(), CustomResources: true,
			Writer: &enginekit.FluxRecorder{At: fixedNow}}
		sh, w, q := showcaseEngineShell(t, conn, mode)
		q.until(t, "the Flux kinds", func() bool {
			_, ok := sh.side.children[groupPrefix+"flux/notifications"]
			return ok
		})
		openFluxView(t, sh, q, engine.FluxDashboard, 5)
		q.until(t, "the tiles", func() bool {
			k := sh.view.flux.tiles["kustomizations.kustomize.toolkit.fluxcd.io"]
			return len(sh.view.flux.tiles) == 13 && k != nil && k.count.text == "5"
		})
		for _, g := range []string{fluxGroup, groupPrefix + "flux/sources"} {
			sh.side.setOpen(g, true)
		}
		q.drain()
		showcaseCounters(sh, showcaseContext)
		saveShowcase(t, w, "flux", mode)
	})
}

// 10. Argo CD: the dashboard of the Applications' sync and health, the recent activity and what
// needs attention.
func TestShowcaseArgoCD(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		src := testkit.NewMemorySource()
		argoFixture(src)
		sh, w, q := showcaseEngineShell(t, &argoGUIConnector{src: src, writer: &argoGUIWriter{}}, mode)
		q.until(t, "Argo CD's kinds", func() bool {
			return slices.ContainsFunc(sh.kinds, func(k kindInfo) bool { return k.Kind == kinds.ArgoClustersKey })
		})
		argoOpenDashboard(t, sh, q)
		q.drain()
		showcaseCounters(sh, showcaseContext)
		saveShowcase(t, w, "argocd", mode)
	})
}

// 11. Many clusters: the hotbar, the navigator listing every cluster, tabs of several clusters and
// the command palette. The connected cluster is showcaseContext; the others have invented names
// (showcaseContexts).
func TestShowcaseClusters(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f, _ := paletteFake(t)
		f.contexts = showcaseContexts()
		f.ui.Selected, f.ui.Drawer = nil, nil
		withHotbar(f, showcaseContext, "prod-eu-central", "staging-us-east", "kind-local", "dev-us-west")
		f.ui.ClusterSettings = map[string]engine.ClusterSettings{
			"prod-eu-central": {IconColor: "red"}, "staging-us-east": {IconColor: "yellow"}, "kind-local": {IconColor: "violet"},
			"dev-us-west": {IconColor: "green"},
		}
		f.ui.NavigatorScope = engine.NavScopeLocal
		f.ui.Tabs = []engine.ViewTab{
			{ID: "tab-1", Context: showcaseContext, View: "pods", Title: "Pods - " + showcaseContext, Sort: engine.DefaultSort},
			{ID: "tab-2", Context: "prod-eu-central", View: "deployments", Title: "Deployments - prod-eu-central", Sort: engine.DefaultSort},
			{ID: "tab-3", Context: "staging-us-east", View: "nodes", Title: "Nodes - staging-us-east", Sort: engine.DefaultSort},
			{ID: "tab-4", Context: "kind-local", View: engine.ClusterOverview, Title: "Overview - kind-local", Sort: engine.DefaultSort},
		}
		f.ui.ActiveTab = "tab-1"
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
		// The tab bar shows the shown cluster's tabs only (GW11, D-69), and no tab of a cluster
		// without a connection (GW13): demo's one of the four.
		q.until(t, "the shell", func() bool {
			return len(sh.kinds) > 0 && sh.hot.root.Visible() && len(tilesOf(sh.hot)) == 5 && len(sh.tabs.shown) == 1 && sh.diag.total > 0
		})
		openPalette(t, sh, q)
		typeInPalette(t, sh, q, "open")
		saveShowcase(t, w, "clusters", mode)
	})
}

// 12. Certificates in a TLS Secret decoded in its drawer: each certificate of the chain, the key
// checked against it, the chain's order, the validity.
func TestShowcaseCertificates(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f, ref, _ := certsSecretsFake(t)
		renameRow(f, "secrets", "stripe-webhook", "payment-webhook")
		showcaseTabs(f, "secrets", "pods")
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the drawer", func() bool {
			return len(sh.kinds) > 0 && sh.view.snap != nil && sh.drawer.last != nil && sh.drawer.last.Ref == ref && sh.diag.total > 0
		})
		settle(w)
		scrollDrawerTo(t, sh, "Certificates · tls.crt")
		saveShowcase(t, w, "certificates", mode)
	})
}

// 13. An agent at work, from the user's side: the agent's Restart waits for the user's answer in
// a dialog that names the object, the namespace and the context; the Audit Log below lists what
// agents did before.
func TestShowcaseAgents(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		f := kindsFake(t, "deployments")
		f.tables["deployments"] = kindSnapshot(t, "deployments", showcaseDeployments())
		dep := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
		f.ui.Selected = []model.ObjectRef{dep}
		f.ui.Dock = []engine.DockTab{{ID: "audit-1", Type: engine.DockAudit, Title: "Audit Log"}}
		f.ui.ActiveDock = "audit-1"
		audit := showcaseAudit(realCommands(t), dep)
		f.handlers["audit.list"] = func(context.Context, engine.Invocation) (any, error) {
			return audit, nil
		}
		showcaseTabs(f, "deployments", "pods")
		fakeTabCommands(t, f)
		sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
		sh.zone = time.UTC
		sh.vsplit.SetOffset(0.5)
		q.until(t, "the Audit Log", func() bool {
			a := auditTabOf(sh)
			return len(sh.kinds) > 0 && a != nil && len(a.entries) == len(audit.Entries) && sh.diag.total > 0
		})
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{
				Command: "deployment.restart", Label: "Restart", Danger: engine.DangerMutating,
				Origin: engine.OriginExternalAgent, Context: "demo",
				Namespaces: []string{dep.Namespace}, Objects: []model.ObjectRef{dep}, Kinds: []string{"Deployment"},
			})
		}()
		q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
		saveShowcase(t, w, "agents", mode)
		test.Tap(sh.confirmer.current.cancel)
		<-done
	})
}

// showcaseAudit is what an external agent did before it asked for the Restart: it read the UI's
// state and the Deployment's logs, showed the Deployment, scaled another one with the user's
// acceptance, and was refused a Delete.
func showcaseAudit(reg *engine.Registry, dep model.ObjectRef) engine.AuditList {
	at := func(m, s int) time.Time {
		return fixedNow.Add(-time.Duration(m)*time.Minute - time.Duration(s)*time.Second)
	}
	worker := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "payments", Name: "payments-worker"}
	pod := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "payments", Name: "payments-worker-6f8b9c7d4-eknsy"}
	items := []engine.AuditItem{
		{Time: at(0, 12), Origin: engine.OriginExternalAgent, Command: "ui.navigate", Context: "demo", Phase: "done", OK: true, Objects: []model.ObjectRef{dep}, ObjectCount: 1},
		{Time: at(0, 31), Origin: engine.OriginExternalAgent, Command: "workload.logs", Context: "demo", Phase: "done", OK: true, Objects: []model.ObjectRef{dep}, ObjectCount: 1},
		{Time: at(0, 48), Origin: engine.OriginExternalAgent, Command: "resources.list", Context: "demo", Phase: "done", OK: true},
		{Time: at(1, 5), Origin: engine.OriginExternalAgent, Command: "ui.state", Context: "demo", Phase: "done", OK: true},
		{Time: at(7, 58), Origin: engine.OriginExternalAgent, Command: "deployment.scale", Context: "demo", Phase: "done", OK: true, Objects: []model.ObjectRef{worker}, ObjectCount: 1},
		{Time: at(8, 0), Origin: engine.OriginExternalAgent, Command: "deployment.scale", Context: "demo", Phase: "requested", OK: true, Objects: []model.ObjectRef{worker}, ObjectCount: 1},
		{Time: at(9, 30), Origin: engine.OriginExternalAgent, Command: "object.delete", Context: "demo", Phase: "done",
			Error: "command object.delete: the user declined the command", Objects: []model.ObjectRef{pod}, ObjectCount: 1},
		{Time: at(12, 0), Origin: engine.OriginGUI, Command: "mcp.switch", Phase: "done", OK: true},
	}
	for i := range items { // the labels as the engine adds them from its registry
		if c, ok := reg.Get(items[i].Command); ok {
			items[i].Label = c.Label
		}
	}
	return engine.AuditList{Matched: len(items), Returned: len(items), Entries: items}
}

// showcaseDeployments are the Deployments of a small shop, one of them short of a ready pod.
func showcaseDeployments() []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for i, d := range []struct {
		ns, name        string
		replicas, ready int64
	}{
		{"checkout", "checkout-api", 3, 3}, {"checkout", "cart-service", 2, 2}, {"payments", "payments-worker", 2, 1},
		{"payments", "fraud-detector", 2, 2}, {"storefront", "web-frontend", 4, 4}, {"storefront", "catalog-api", 3, 3},
		{"orders", "order-processor", 2, 2}, {"orders", "shipping-quotes", 1, 1}, {"search", "search-indexer", 2, 2},
		{"ml-serving", "recommendation-engine", 2, 2}, {"notifications", "notification-dispatcher", 1, 1},
		{"identity", "auth-gateway", 3, 3}, {"qa", "image-resizer", 0, 0},
	} {
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]any{"name": d.name, "namespace": d.ns, "uid": "uid-dep-" + d.name, "generation": int64(2),
				"creationTimestamp": ts(time.Duration(20+i*3) * 24 * time.Hour)},
			"spec": map[string]any{"replicas": d.replicas, "selector": map[string]any{"matchLabels": map[string]any{"app": d.name}}},
			"status": map[string]any{"observedGeneration": int64(2), "replicas": d.replicas, "readyReplicas": d.ready,
				"availableReplicas": d.ready, "updatedReplicas": d.replicas},
		}})
	}
	return out
}

// renameRow renames an object of a fixture view, in its ref and its cells.
func renameRow(f *fakeEngine, view, from, to string) {
	snap := *f.tables[view]
	snap.Rows = slices.Clone(snap.Rows)
	for i, r := range snap.Rows {
		if r.Ref.Name != from {
			continue
		}
		r.Ref.Name = to
		r.Cells = slices.Clone(r.Cells)
		for j := range r.Cells {
			r.Cells[j].Text = strings.ReplaceAll(r.Cells[j].Text, from, to)
		}
		snap.Rows[i] = r
	}
	f.tables[view] = &snap
}
