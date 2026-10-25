package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// The Flux section (G23): the navigator's Flux group, the Dashboard page, a Flux kind's view, row
// menu and drawer, over the real engine and commands with a cluster of synthetic Flux objects
// (Cogpit's curated profiles, internal/model/kinds/flux*.go). PNGs: testdata/flux_*.png.

// fluxKindInfos are the Flux kinds as kinds.list reports them with the curated profiles.
func fluxKindInfos() []kindInfo {
	var out []kindInfo
	for _, p := range kinds.Curated() {
		if _, ok := kinds.FluxKindOf(p.Key); ok {
			out = append(out, kindInfo{Kind: p.Key, Title: p.Title, APIKind: p.Kind, Group: p.Group, Version: p.Version, Namespaced: true})
		}
	}
	return out
}

// TestSidebarFluxSection: with Flux installed the navigator shows the Flux group between
// Applications and Nodes as captured (Dashboard, Helm Releases, Kustomizations, the three
// sub-groups), the sub-groups only with kinds the cluster serves, and the toolkit's API groups
// under Custom Resources too; without Flux the group, the Dashboard included, is gone.
func TestSidebarFluxSection(t *testing.T) {
	f := crdFake(t)
	served := slices.DeleteFunc(fluxKindInfos(), func(k kindInfo) bool { return strings.HasPrefix(k.Kind, "imagepolicies.") })
	served = slices.DeleteFunc(served, func(k kindInfo) bool { return strings.Contains(k.Kind, ".notification.") })
	f.kinds = append(append(f.kinds, kindInfo{Kind: "applications", Title: "Applications", APIKind: "Application", Namespaced: true}), served...)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	s := sh.side
	roots := titlesOf(s, s.children[""])
	if len(roots) < 4 || !slices.Equal(roots[:4], []string{"Overview", "Applications", "Flux", "Nodes"}) {
		t.Fatalf("top level %v", roots)
	}
	if got := titlesOf(s, s.children[fluxGroup]); !slices.Equal(got, []string{"Dashboard", "Helm Releases", "Kustomizations", "Image Automation", "Sources"}) {
		t.Errorf("Flux %v", got)
	}
	if got := titlesOf(s, s.children[groupPrefix+"flux/images"]); !slices.Equal(got, []string{"Image Repositories", "Image Update Automations"}) {
		t.Errorf("Image Automation %v", got)
	}
	if got := titlesOf(s, s.children[groupPrefix+"flux/sources"]); !slices.Equal(got,
		[]string{"Git Repositories", "OCI Repositories", "Helm Repositories", "Buckets", "External Artifacts"}) {
		t.Errorf("Sources %v", got)
	}
	if _, ok := s.children[groupPrefix+"flux/notifications"]; ok {
		t.Error("Notifications shown without its kinds")
	}
	if got := s.children[customGroupPrefix+"kustomize.toolkit.fluxcd.io"]; !slices.Equal(got, []string{aliasPrefix + "kustomizations.kustomize.toolkit.fluxcd.io"}) {
		t.Errorf("Custom Resources › kustomize.toolkit.fluxcd.io %v", got)
	}

	// The Dashboard opens the page.
	s.tree.Select(engine.FluxDashboard)
	q.until(t, "ui.openView", func() bool { return len(f.callsOf("ui.openView")) > 0 })
	if p := decodeParams[openViewParams](t, f.callsOf("ui.openView")[0]); p.View != engine.FluxDashboard {
		t.Errorf("the Dashboard opened %q", p.View)
	}

	// Flux uninstalled: the section goes, its Dashboard with it.
	f.mu.Lock()
	f.kinds = slices.DeleteFunc(f.kinds, func(k kindInfo) bool { _, flux := kinds.FluxKindOf(k.Kind); return flux })
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventKinds})
	q.until(t, "the Flux group to go", func() bool { _, ok := s.children[fluxGroup]; return !ok })
	if s.titles[engine.FluxDashboard] != "" || slices.Contains(titlesOf(s, s.children[""]), "Flux") {
		t.Errorf("the Dashboard stayed: %q, top level %v", s.titles[engine.FluxDashboard], titlesOf(s, s.children[""]))
	}
}

// fluxFixture is a cluster running Flux: every toolkit definition at the version Flux serves (the
// HelmCharts helm-controller makes too), and objects in every state.
func fluxFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	for _, ns := range []string{"flux-system", "shop", "platform"} {
		src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", ns, nil))
	}
	for _, p := range kinds.Curated() {
		if _, ok := kinds.FluxKindOf(p.Key); ok {
			src.Set(testkit.CRDsGVR, testkit.CRD(p.Group, p.Resource, p.Kind, true, testkit.CRDVersion{Name: p.Version, Served: true, Storage: true}))
		}
	}
	src.Set(testkit.CRDsGVR, testkit.CRD("source.toolkit.fluxcd.io", "helmcharts", "HelmChart", true, testkit.CRDVersion{Name: "v1", Served: true, Storage: true}))
	src.Set(schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmcharts"},
		testkit.Object("source.toolkit.fluxcd.io/v1", "HelmChart", "flux-system", "shop-podinfo", map[string]any{"spec": map[string]any{
			"chart": "podinfo", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "podinfo"}}}))
	gvr := func(key string) schema.GroupVersionResource {
		for _, p := range kinds.Curated() {
			if p.Key == key {
				return p.GVR()
			}
		}
		panic(key)
	}
	add := func(key, apiVersion, kind, ns, name string, spec, status map[string]any) {
		obj := testkit.Object(apiVersion, kind, ns, name, map[string]any{"spec": spec})
		if status != nil {
			obj.Object["status"] = status
		}
		src.Set(gvr(key), obj)
	}
	ready := func(msg, since string, extra map[string]any) map[string]any {
		st := map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded", "message": msg, "lastTransitionTime": since}}}
		for k, v := range extra {
			st[k] = v
		}
		return st
	}
	failing := func(reason, msg, since string) map[string]any {
		return map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False", "reason": reason, "message": msg, "lastTransitionTime": since}}}
	}
	const ks, hr = "kustomizations.kustomize.toolkit.fluxcd.io", "helmreleases.helm.toolkit.fluxcd.io"
	ksAPI, hrAPI := "kustomize.toolkit.fluxcd.io/v1", "helm.toolkit.fluxcd.io/v2"
	gitSrc := map[string]any{"kind": "GitRepository", "name": "flux-system"}
	add(ks, ksAPI, "Kustomization", "flux-system", "flux-system", map[string]any{"path": "./clusters/prod", "prune": true, "interval": "10m", "sourceRef": gitSrc},
		ready("Applied revision: main@sha1:6f2c1b9e", "2026-10-11T14:20:00Z", map[string]any{"lastAppliedRevision": "main@sha1:6f2c1b9e"}))
	add(ks, ksAPI, "Kustomization", "flux-system", "apps", map[string]any{"path": "./apps/prod", "prune": true, "interval": "5m", "sourceRef": gitSrc,
		"dependsOn": []any{map[string]any{"name": "infra"}}},
		ready("Applied revision: main@sha1:6f2c1b9e", "2026-10-11T14:21:00Z", map[string]any{"lastAppliedRevision": "main@sha1:6f2c1b9e"}))
	add(ks, ksAPI, "Kustomization", "flux-system", "infra", map[string]any{"path": "./infrastructure", "interval": "1h",
		"sourceRef": map[string]any{"kind": "OCIRepository", "name": "platform", "namespace": "platform"}},
		failing("BuildFailed", "kustomize build failed: accumulating resources: missing Resource metadata", "2026-10-11T13:10:00Z"))
	add(ks, ksAPI, "Kustomization", "flux-system", "monitoring", map[string]any{"path": "./monitoring", "suspend": true, "sourceRef": gitSrc},
		ready("Applied revision: main@sha1:1234abcd", "2026-10-09T08:00:00Z", map[string]any{"lastAppliedRevision": "main@sha1:1234abcd"}))
	add(ks, ksAPI, "Kustomization", "flux-system", "tenants", map[string]any{"path": "./tenants", "sourceRef": gitSrc},
		map[string]any{"conditions": []any{
			map[string]any{"type": "Reconciling", "status": "True", "reason": "Progressing", "message": "Reconciliation in progress"},
			map[string]any{"type": "Ready", "status": "Unknown", "reason": "Progressing", "message": "Reconciliation in progress", "lastTransitionTime": "2026-10-11T14:29:00Z"},
		}})
	add(hr, hrAPI, "HelmRelease", "shop", "podinfo", map[string]any{"interval": "30m", "timeout": "5m", "releaseName": "podinfo", "targetNamespace": "shop",
		"chart":      map[string]any{"spec": map[string]any{"chart": "podinfo", "version": "6.x", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "podinfo", "namespace": "flux-system"}}},
		"valuesFrom": []any{map[string]any{"kind": "ConfigMap", "name": "podinfo-values"}, map[string]any{"kind": "Secret", "name": "podinfo-secret", "valuesKey": "values.yaml"}},
		"values":     map[string]any{"password": "never-shown"}},
		ready("Helm upgrade succeeded for release shop/podinfo.v4 with chart podinfo@6.7.1", "2026-10-11T12:00:06Z", map[string]any{
			"lastAttemptedRevision": "6.7.1", "lastAttemptedReleaseAction": "upgrade", "helmChart": "flux-system/shop-podinfo",
			"history": []any{
				map[string]any{"chartName": "podinfo", "chartVersion": "6.7.1", "appVersion": "6.7.1", "version": int64(4), "status": "deployed", "lastDeployed": "2026-10-11T12:00:05Z"},
				map[string]any{"chartName": "podinfo", "chartVersion": "6.7.0", "appVersion": "6.7.0", "version": int64(3), "status": "superseded", "lastDeployed": "2026-10-05T09:00:00Z"},
			}}))
	add(hr, hrAPI, "HelmRelease", "shop", "frontend", map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": "frontend-chart"}},
		failing("UpgradeFailed", "Helm upgrade failed for release shop/frontend with chart frontend@1.2.0: context deadline exceeded", "2026-10-11T14:00:00Z"))
	add(hr, hrAPI, "HelmRelease", "platform", "redis", map[string]any{"suspend": true,
		"chart": map[string]any{"spec": map[string]any{"chart": "redis", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "bitnami", "namespace": "flux-system"}}}},
		ready("Helm install succeeded", "2026-10-02T09:00:00Z", nil))
	srcAPI := "source.toolkit.fluxcd.io/v1"
	add("gitrepositories.source.toolkit.fluxcd.io", srcAPI, "GitRepository", "flux-system", "flux-system",
		map[string]any{"url": "https://github.com/acme/k8s-flux-base", "ref": map[string]any{"branch": "main"}, "interval": "1m"},
		ready("stored artifact for revision 'main@sha1:6f2c1b9e'", "2026-10-11T14:19:00Z", map[string]any{"artifact": map[string]any{"revision": "main@sha1:6f2c1b9e"}}))
	add("ocirepositories.source.toolkit.fluxcd.io", srcAPI, "OCIRepository", "platform", "platform",
		map[string]any{"url": "oci://ghcr.io/acme/platform", "ref": map[string]any{"semver": "1.x"}},
		ready("stored artifact for digest '1.5.0@sha256:9f8e7d'", "2026-10-11T14:00:00Z", map[string]any{"artifact": map[string]any{"revision": "1.5.0@sha256:9f8e7d"}}))
	add("helmrepositories.source.toolkit.fluxcd.io", srcAPI, "HelmRepository", "flux-system", "podinfo",
		map[string]any{"url": "https://stefanprodan.github.io/podinfo"}, ready("stored artifact: revision 'sha256:4f2b'", "2026-10-11T13:00:00Z", nil))
	add("helmrepositories.source.toolkit.fluxcd.io", srcAPI, "HelmRepository", "flux-system", "bitnami",
		map[string]any{"type": "oci", "url": "oci://registry-1.docker.io/bitnamicharts"}, ready("Helm repository is Ready", "2026-10-11T13:00:00Z", nil))
	imgAPI := "image.toolkit.fluxcd.io/v1"
	add("imagerepositories.image.toolkit.fluxcd.io", imgAPI, "ImageRepository", "flux-system", "podinfo",
		map[string]any{"image": "ghcr.io/stefanprodan/podinfo"}, ready("successful scan: found 42 tags", "2026-10-11T14:25:00Z",
			map[string]any{"lastScanResult": map[string]any{"tagCount": int64(42), "scanTime": "2026-10-11T14:25:00Z"}}))
	add("imagepolicies.image.toolkit.fluxcd.io", imgAPI, "ImagePolicy", "flux-system", "podinfo",
		map[string]any{"imageRepositoryRef": map[string]any{"name": "podinfo"}, "policy": map[string]any{"semver": map[string]any{"range": "6.x"}}},
		ready("Latest image tag resolved to 6.7.1", "2026-10-11T14:25:00Z", map[string]any{"latestRef": map[string]any{"name": "ghcr.io/stefanprodan/podinfo", "tag": "6.7.1"}}))
	ntfAPI := "notification.toolkit.fluxcd.io/v1beta3"
	add("alerts.notification.toolkit.fluxcd.io", ntfAPI, "Alert", "flux-system", "on-call",
		map[string]any{"providerRef": map[string]any{"name": "slack"}, "eventSeverity": "error"}, nil)
	add("providers.notification.toolkit.fluxcd.io", ntfAPI, "Provider", "flux-system", "slack",
		map[string]any{"type": "slack", "channel": "ops-alerts", "address": "https://hooks.slack.com/services/never-shown"}, nil)
	return src
}

// fluxEngineShell runs the shell over the real engine and commands, connected to fluxFixture.
func fluxEngineShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *engine.Session, *enginekit.FluxRecorder) {
	t.Helper()
	sh, w, q, s, rec, _ := fluxEngineShellOn(t, mode)
	return sh, w, q, s, rec
}

// fluxEngineShellOn is fluxEngineShell with the cluster's source, for a test that plays Flux's
// controllers in it (playFluxControllers).
func fluxEngineShellOn(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *engine.Session, *enginekit.FluxRecorder, *testkit.MemorySource) {
	t.Helper()
	rec := &enginekit.FluxRecorder{At: fixedNow}
	src := fluxFixture()
	conn := enginekit.MemoryConnector{Context: "kind-flux", Src: src, Profiles: kinds.Builtin(), CustomResources: true, Writer: rec}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: "kind-flux", Theme: mode}, q.do)
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
	q.until(t, "the Flux kinds", func() bool {
		_, ok := sh.side.children[groupPrefix+"flux/notifications"]
		return sh.ui.Connection.State == engine.Connected && ok
	})
	return sh, w, q, s, rec, src
}

// playFluxControllers makes rec's patches handled by Flux's controllers at once: the object gets
// status.lastHandledReconcileAt, the time it is handled, and Ready True, as Reconcile with Source
// waits for it (A91).
func playFluxControllers(s *engine.Session, rec *enginekit.FluxRecorder, src *testkit.MemorySource) {
	rec.OnPatch = func(gvr schema.GroupVersionResource, namespace, name string) {
		obj, err := s.Lookup(model.ObjectRef{Kind: gvr.Resource + "." + gvr.Group, Namespace: namespace, Name: name})
		if err != nil {
			return
		}
		obj = obj.DeepCopy()
		obj.Object["status"] = map[string]any{
			"lastHandledReconcileAt": time.Now().UTC().Format(time.RFC3339Nano),
			"conditions":             []any{map[string]any{"type": "Ready", "status": "True", "message": "handled"}},
		}
		src.Set(gvr, obj)
	}
}

// openFluxView opens a view through the navigator, as a click does, and waits for its rows.
func openFluxView(t *testing.T, sh *shell, q *mainQueue, view string, rows int) {
	t.Helper()
	sh.side.activate(view)
	q.until(t, view, func() bool {
		return sh.ui.View == view && sh.view.snap != nil && sh.view.snap.View == view && sh.view.snap.Synced && len(sh.view.snap.Rows) == rows
	})
}

// TestFluxDashboardInTheWindow: the Dashboard shows a tile per served kind under its sub-group,
// with its count and states; a state opens its kind's view filtered to that state (the views'
// status filter, D-34); the table lists what is not ready, of several kinds, and a selection of it
// takes the Flux actions.
func TestFluxDashboardInTheWindow(t *testing.T) {
	sh, _, q, s, rec := fluxEngineShell(t, ThemeDark)
	openFluxView(t, sh, q, engine.FluxDashboard, 5)
	tv := sh.view
	q.until(t, "the tiles", func() bool {
		k := tv.flux.tiles["kustomizations.kustomize.toolkit.fluxcd.io"]
		return len(tv.flux.tiles) == 13 && k != nil && k.count.text == "5"
	})
	if tv.title.text != "Flux Dashboard" || tv.eventsTitle.text != "Not Ready" || tv.search.PlaceHolder != "Search Flux objects…" {
		t.Errorf("page %q, table %q, search %q", tv.title.text, tv.eventsTitle.text, tv.search.PlaceHolder)
	}
	ks := tv.flux.tiles["kustomizations.kustomize.toolkit.fluxcd.io"]
	if ks.link.text != "Kustomizations" || ks.count.text != "5" || ks.bar.tipText() != "2 Ready · 1 Not Ready · 1 Reconciling · 1 Suspended" {
		t.Fatalf("Kustomizations tile %q %q %q", ks.link.text, ks.count.text, ks.bar.tipText())
	}
	var names []string
	for _, r := range tv.snap.Rows {
		names = append(names, r.Ref.Name)
	}
	if !slices.Equal(names, []string{"tenants", "frontend", "infra", "monitoring", "redis"}) {
		t.Errorf("not ready rows %v", names)
	}

	// The table's rows are of several kinds: select them all, the selection menu has the Flux
	// actions; Resume resumes the two suspended ones.
	sh.dispatch("ui.select", selectModeParams{All: true, NoDrawer: true}, nil)
	q.until(t, "the selection", func() bool { return len(sh.ui.Selected) == 5 })
	refs := slices.Clone(sh.ui.Selected)
	acts := sh.evaluateSelection(refs)
	m := tv.selectionMenu(refs, acts)
	if got := labels(m); !slices.Contains(got, "Reconcile") || !slices.Contains(got, "Suspend") || !slices.Contains(got, "Resume") || !strings.Contains(strings.Join(got, "|"), "|Delete") {
		t.Errorf("selection menu %q", got)
	}
	item(t, m, "Resume").Action()
	q.until(t, "the resumes", func() bool { return len(rec.Patches()) == 2 })
	for _, p := range rec.Patches() {
		if !strings.Contains(p, `"suspend":false`) || !strings.Contains(p, "reconcile.fluxcd.io/requestedAt") {
			t.Errorf("resume patch %s", p)
		}
	}

	// A state opens its kind's view filtered to it.
	notReady := ks.links["notReady"]
	if notReady == nil || len(ks.links) != 4 {
		t.Fatalf("state links %v", ks.links)
	}
	test.Tap(notReady)
	const ksKey = "kustomizations.kustomize.toolkit.fluxcd.io"
	q.until(t, "the filtered Kustomizations view", func() bool {
		snap := tv.snap
		return sh.ui.View == ksKey && slices.Equal(sh.ui.States, []string{"Not Ready"}) && snap != nil && snap.View == ksKey &&
			snap.Synced && slices.Equal(snap.States, []string{"Not Ready"})
	})
	if len(tv.snap.Rows) != 1 || tv.snap.Rows[0].Ref.Name != "infra" || sh.ui.Filter != "" || s.UIState().View != sh.ui.View ||
		tv.states.button.Text != "Not Ready" || !tv.states.clear.Visible() {
		t.Errorf("rows %v, filter %q, states button %q", tv.snap.Rows, sh.ui.Filter, tv.states.button.Text)
	}
}

// TestFluxKustomizationsInTheWindow: a Flux kind's view shows the curated columns, its row menu
// Reconcile, Reconcile with Source and Suspend (Resume on a suspended object, Reconcile greyed
// there), and Reconcile patches the object.
func TestFluxKustomizationsInTheWindow(t *testing.T) {
	sh, _, q, _, rec := fluxEngineShell(t, ThemeDark)
	const key = "kustomizations.kustomize.toolkit.fluxcd.io"
	openFluxView(t, sh, q, key, 5)
	var cols []string
	for _, i := range sh.view.cols {
		cols = append(cols, sh.view.snap.Columns[i].Title)
	}
	if !slices.Equal(cols, []string{"Name", "Namespace", "Source", "Path", "Revision", "Suspended", "Age", "Status", "Message"}) {
		t.Errorf("columns %v", cols)
	}
	row := func(name string) int {
		for i, r := range sh.view.snap.Rows {
			if r.Ref.Name == name {
				return i
			}
		}
		t.Fatalf("no row %s", name)
		return -1
	}
	m := openMenu(t, sh, q, row("apps"))
	if got := strings.Join(labels(m), "|"); !strings.HasPrefix(got, "Show Details|Reconcile|Reconcile with Source|Suspend|Edit") || !strings.Contains(got, "|Delete") {
		t.Errorf("menu %s", got)
	}
	item(t, m, "Reconcile").Action()
	q.until(t, "the reconcile", func() bool { return len(rec.Patches()) == 1 })
	if p := rec.Patches()[0]; !strings.HasPrefix(p, "kustomizations flux-system/apps ") || !strings.Contains(p, `"uid":"uid-flux-system-apps"`) {
		t.Errorf("patch %s", p)
	}
	m = openMenu(t, sh, q, row("monitoring"))
	if got := strings.Join(labels(m), "|"); !strings.Contains(got, "Reconcile — monitoring is suspended") || !strings.Contains(got, "|Resume|") || strings.Contains(got, "|Suspend|") {
		t.Errorf("suspended menu %s", got)
	}
}

// TestFluxHelmReleaseActionsInTheWindow (D-33, owner 2026-10-14): a Helm release's row menu has
// Reconcile, Reconcile with Source and Reset before Suspend; Reconcile with Source patches the
// chart's Helm repository, the release's HelmChart and the release, in that order; Reset writes
// resetAt; on a suspended release both are greyed with why.
func TestFluxHelmReleaseActionsInTheWindow(t *testing.T) {
	sh, _, q, s, rec, src := fluxEngineShellOn(t, ThemeDark)
	playFluxControllers(s, rec, src) // Reconcile with Source waits for each (A91)
	openFluxView(t, sh, q, "helmreleases.helm.toolkit.fluxcd.io", 3)
	row := func(name string) int {
		for i, r := range sh.view.snap.Rows {
			if r.Ref.Name == name {
				return i
			}
		}
		t.Fatalf("no row %s", name)
		return -1
	}
	m := openMenu(t, sh, q, row("podinfo"))
	if got := strings.Join(labels(m), "|"); !strings.HasPrefix(got, "Show Details|Reconcile|Reconcile with Source|Reset|Suspend|Edit") {
		t.Errorf("menu %s", got)
	}
	item(t, m, "Reconcile with Source").Action()
	q.until(t, "the three patches", func() bool { return len(rec.Patches()) == 3 })
	var got []string
	for _, p := range rec.Patches() {
		got = append(got, strings.SplitN(p, " {", 2)[0])
	}
	if want := []string{"helmrepositories flux-system/podinfo", "helmcharts flux-system/shop-podinfo", "helmreleases shop/podinfo"}; !slices.Equal(got, want) {
		t.Errorf("patches %v, want %v", got, want)
	}
	m = openMenu(t, sh, q, row("podinfo"))
	item(t, m, "Reset").Action()
	q.until(t, "the reset", func() bool { return len(rec.Patches()) == 4 })
	if p := rec.Patches()[3]; !strings.HasPrefix(p, "helmreleases shop/podinfo ") || !strings.Contains(p, "reconcile.fluxcd.io/resetAt") {
		t.Errorf("reset %s", p)
	}
	m = openMenu(t, sh, q, row("redis"))
	if got := strings.Join(labels(m), "|"); !strings.Contains(got, "Reconcile with Source — redis is suspended") || !strings.Contains(got, "Reset — redis is suspended: resume it to reset it") {
		t.Errorf("suspended menu %s", got)
	}
}

func renderFluxDashboard(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := fluxEngineShell(t, mode)
	openFluxView(t, sh, q, engine.FluxDashboard, 5)
	q.until(t, "the tiles", func() bool {
		k := sh.view.flux.tiles["kustomizations.kustomize.toolkit.fluxcd.io"]
		return len(sh.view.flux.tiles) == 13 && k != nil && k.count.text == "5"
	})
	for _, g := range []string{fluxGroup, groupPrefix + "flux/sources"} {
		sh.side.setOpen(g, true)
	}
	q.drain()
	settle(w)
	savePNG(t, w, name)
}

func TestRenderFluxDashboardDark(t *testing.T) {
	renderFluxDashboard(t, ThemeDark, "flux_dashboard_dark.png")
}
func TestRenderFluxDashboardLight(t *testing.T) {
	renderFluxDashboard(t, ThemeLight, "flux_dashboard_light.png")
}

func renderFluxKustomizations(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := fluxEngineShell(t, mode)
	openFluxView(t, sh, q, "kustomizations.kustomize.toolkit.fluxcd.io", 5)
	q.drain()
	settle(w)
	savePNG(t, w, name)
}

func TestRenderFluxKustomizationsDark(t *testing.T) {
	renderFluxKustomizations(t, ThemeDark, "flux_kustomizations_dark.png")
}

func TestRenderFluxKustomizationsLight(t *testing.T) {
	renderFluxKustomizations(t, ThemeLight, "flux_kustomizations_light.png")
}

func renderFluxHelmReleaseDrawer(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := fluxEngineShell(t, mode)
	const key = "helmreleases.helm.toolkit.fluxcd.io"
	openFluxView(t, sh, q, key, 3)
	ref := model.ObjectRef{Context: "kind-flux", Kind: key, Namespace: "shop", Name: "podinfo"}
	sh.dispatch("ui.select", selectModeParams{Refs: []model.ObjectRef{ref}}, nil)
	q.until(t, "the drawer", func() bool {
		return sh.ui.Drawer != nil && sh.drawer.last != nil && sh.drawer.last.Ref.Name == "podinfo" && len(sh.drawer.actions.Objects) > 0
	})
	q.drain()
	settle(w)
	savePNG(t, w, name)
}

func TestRenderFluxHelmReleaseDrawerDark(t *testing.T) {
	renderFluxHelmReleaseDrawer(t, ThemeDark, "flux_helmrelease_drawer_dark.png")
}

func TestRenderFluxHelmReleaseDrawerLight(t *testing.T) {
	renderFluxHelmReleaseDrawer(t, ThemeLight, "flux_helmrelease_drawer_light.png")
}
