package fyneui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Argo CD section in the window (G23) against the real engine and commands over an in-memory
// cluster with Argo CD's definitions (no Argo CD runs): the navigator's group following the
// definitions, the Dashboard's panel and its actions, the Sync and Refresh dialogs, and renders of
// the Dashboard, the Applications list and an Application's drawer. Every name is made up.

// argoGUIConnector connects "argo" to a cache over src with the custom kinds watch on; its writes
// go to the recorder.
type argoGUIConnector struct {
	src    *testkit.MemorySource
	writer *argoGUIWriter
}

func (c *argoGUIConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "argo", Namespace: "argocd", Current: true}}, nil
}

func (c *argoGUIConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := testkit.CacheOver(c.src)
	var profiles []*model.KindProfile
	for _, key := range []string{"pods", "deployments", "services", "secrets", "namespaces"} {
		p, _ := kinds.ByKey(key)
		profiles = append(profiles, p)
	}
	// No connection: the writer is the backend's Actions, so the commands can write; nothing dials.
	return &engine.Backend{Context: "argo", Namespace: "argocd", Cache: ch, Close: ch.Close, CustomResources: true,
		Profiles: profiles, Actions: c.writer}, nil
}

// argoGUIWriter records Argo CD's writes.
type argoGUIWriter struct {
	mu     sync.Mutex
	writes []string
}

func (w *argoGUIWriter) record(s string) {
	w.mu.Lock()
	w.writes = append(w.writes, s)
	w.mu.Unlock()
}

func (w *argoGUIWriter) Writes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.writes)
}

func (w *argoGUIWriter) RestartDeployment(context.Context, string, string, string) error { return nil }
func (w *argoGUIWriter) DeletePod(context.Context, string, string, string) error         { return nil }

func (w *argoGUIWriter) RefreshArgoApplication(_ context.Context, _, ns, name, _ string, hard bool) error {
	mode := "normal"
	if hard {
		mode = "hard"
	}
	w.record("refresh " + ns + "/" + name + " " + mode)
	return nil
}

func (w *argoGUIWriter) SyncArgoApplication(_ context.Context, _, ns, name, _, _ string, op map[string]any) error {
	prune, _ := op["sync"].(map[string]any)["prune"].(bool)
	s := "sync " + ns + "/" + name
	if prune {
		s += " prune"
	}
	w.record(s)
	return nil
}

// argoTS is a time before the tests' clock, as an object writes it.
func argoTS(ago time.Duration) string { return fixedNow.Add(-ago).UTC().Format(time.RFC3339) }

// argoApp builds an Application of the fixture.
func argoApp(name, project, path, sync, health string, created time.Duration, status map[string]any) *unstructured.Unstructured {
	st := map[string]any{
		"sync":   map[string]any{"status": sync, "revision": "4f2c9e1b7a6d5c3b2a1f0e9d8c7b6a5f4e3d2c1b"},
		"health": map[string]any{"status": health},
	}
	for k, v := range status {
		st[k] = v
	}
	if sync == "" {
		st = status
	}
	fields := map[string]any{
		"metadata": map[string]any{"creationTimestamp": argoTS(created)},
		"spec": map[string]any{
			"project": project,
			"source": map[string]any{
				"repoURL": "https://git.example.com/platform/k8s-base-argo.git", "targetRevision": "main", "path": path,
			},
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": name},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true"},
			},
		},
	}
	if st != nil {
		fields["status"] = st
	}
	return testkit.Object("argoproj.io/v1alpha1", "Application", "argocd", name, fields)
}

// argoFixture is a cluster with Argo CD: eight Applications in every state, two Application Sets,
// three projects, two repositories and a cluster.
func argoFixture(src *testkit.MemorySource) {
	for _, crd := range testkit.ArgoCRDs() {
		src.Set(testkit.CRDsGVR, crd)
	}
	resources := []any{
		map[string]any{"group": "apps", "version": "v1", "kind": "Deployment", "namespace": "shop", "name": "web", "status": "Synced", "health": map[string]any{"status": "Healthy"}},
		map[string]any{"version": "v1", "kind": "Service", "namespace": "shop", "name": "web", "status": "Synced", "health": map[string]any{"status": "Healthy"}},
		map[string]any{"version": "v1", "kind": "ConfigMap", "namespace": "shop", "name": "web-config", "status": "Synced"},
		map[string]any{"group": "networking.k8s.io", "version": "v1", "kind": "Ingress", "namespace": "shop", "name": "web", "status": "Synced", "health": map[string]any{"status": "Healthy"}},
	}
	history := []any{
		map[string]any{"id": int64(11), "revision": "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567", "deployedAt": argoTS(26 * time.Hour),
			"initiatedBy": map[string]any{"username": "alice"}, "source": map[string]any{"repoURL": "https://git.example.com/platform/k8s-base-argo.git"}},
		map[string]any{"id": int64(12), "revision": "4f2c9e1b7a6d5c3b2a1f0e9d8c7b6a5f4e3d2c1b", "deployedAt": argoTS(40 * time.Minute),
			"initiatedBy": map[string]any{"automated": true}, "source": map[string]any{"repoURL": "https://git.example.com/platform/k8s-base-argo.git"}},
	}
	shop := argoApp("shop", "team-shop", "apps/shop", "Synced", "Healthy", 30*24*time.Hour, map[string]any{
		"reconciledAt": argoTS(2 * time.Minute), "sourceType": "Kustomize", "resources": resources, "history": history,
		"summary": map[string]any{"images": []any{"registry.example.com/shop/web:1.4.2"}, "externalURLs": []any{"https://shop.example.com"}},
		"operationState": map[string]any{"phase": "Succeeded", "message": "successfully synced (all tasks run)",
			"startedAt": argoTS(41 * time.Minute), "finishedAt": argoTS(40 * time.Minute),
			"operation":  map[string]any{"initiatedBy": map[string]any{"automated": true}, "sync": map[string]any{"revision": "main", "prune": true}},
			"syncResult": map[string]any{"revision": "4f2c9e1b7a6d5c3b2a1f0e9d8c7b6a5f4e3d2c1b"}},
	})
	shop.SetOwnerReferences(nil)
	apps := []*unstructured.Unstructured{
		shop,
		argoApp("payments", "team-pay", "apps/payments", "OutOfSync", "Degraded", 12*24*time.Hour, map[string]any{
			"conditions": []any{map[string]any{"type": "SyncError", "message": "one or more objects failed to apply: admission webhook denied the request"}},
			"operationState": map[string]any{"phase": "Failed", "message": "one or more objects failed to apply",
				"startedAt": argoTS(15 * time.Minute), "finishedAt": argoTS(14 * time.Minute),
				"operation": map[string]any{"initiatedBy": map[string]any{"username": "bob"}}},
		}),
		argoApp("ingress", "default", "infra/ingress", "OutOfSync", "Healthy", 60*24*time.Hour, nil),
		argoApp("monitoring", "platform", "infra/monitoring", "Synced", "Progressing", 9*24*time.Hour, map[string]any{
			"operationState": map[string]any{"phase": "Running", "message": "waiting for healthy state of apps/Deployment/grafana",
				"startedAt": argoTS(3 * time.Minute), "operation": map[string]any{"initiatedBy": map[string]any{"username": "carol"}}},
		}),
		argoApp("docs", "default", "apps/docs", "Synced", "Healthy", 90*24*time.Hour, nil),
		argoApp("cache", "team-shop", "apps/cache", "Synced", "Missing", 2*24*time.Hour, nil),
		argoApp("legacy", "default", "apps/legacy", "Synced", "Suspended", 200*24*time.Hour, nil),
		argoApp("fresh", "default", "apps/fresh", "", "", 2*time.Minute, nil),
	}
	for _, a := range apps {
		src.Set(testkit.ArgoApplicationsGVR, a)
	}
	src.Set(testkit.ArgoAppSetsGVR, testkit.Object("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "shops", map[string]any{
		"spec":   map[string]any{"generators": []any{map[string]any{"git": map[string]any{}}}, "template": map[string]any{"spec": map[string]any{"project": "team-shop"}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "ResourcesUpToDate", "status": "True"}}},
	}))
	src.Set(testkit.ArgoAppSetsGVR, testkit.Object("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "previews", map[string]any{
		"spec":   map[string]any{"generators": []any{map[string]any{"pullRequest": map[string]any{}}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "ErrorOccurred", "status": "True", "message": "API rate limit exceeded"}}},
	}))
	for _, p := range []string{"default", "team-shop", "team-pay", "platform"} {
		src.Set(testkit.ArgoProjectsGVR, testkit.Object("argoproj.io/v1alpha1", "AppProject", "argocd", p, map[string]any{
			"spec": map[string]any{"sourceRepos": []any{"*"}},
		}))
	}
	src.Set(testkit.SecretsGVR, testkit.ArgoRepoSecret("argocd", "repo-k8s-base-argo", "https://git.example.com/platform/k8s-base-argo.git", "fake-gui-password"))
	src.Set(testkit.SecretsGVR, testkit.ArgoClusterSecret("argocd", "prod-eu", "https://prod-eu.k8s.example.com:6443", "fake-gui-token"))
}

// newArgoShell starts a shell on the real engine connected to the Argo CD fixture.
func newArgoShell(t *testing.T, mode ThemeMode, seed bool) (*shell, fyne.Window, *mainQueue, *engine.Session, *testkit.MemorySource, *argoGUIWriter) {
	t.Helper()
	src := testkit.NewMemorySource()
	if seed {
		argoFixture(src)
	}
	writer := &argoGUIWriter{}
	s := newTestEngine(engine.Options{Connector: &argoGUIConnector{src: src, writer: writer}, SnapshotInterval: 5 * time.Millisecond,
		Now: func() time.Time { return fixedNow }})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{Theme: mode, AutoConnect: "argo"}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.confirmer.armDelay = 0
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.Close()
	})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected })
	if seed {
		q.until(t, "Argo CD's kinds", func() bool {
			return slices.ContainsFunc(sh.kinds, func(k kindInfo) bool { return k.Kind == kinds.ArgoClustersKey })
		})
	}
	return sh, w, q, s, src, writer
}

// TestArgoNavigatorGroup: the Argo CD group appears with Argo CD's definitions, right after
// Applications, with the Dashboard, the three kinds and Settings › Repositories and Clusters; the
// argoproj.io group stays under Custom Resources; the group leaves with the definitions.
func TestArgoNavigatorGroup(t *testing.T) {
	sh, _, q, _, src, _ := newArgoShell(t, ThemeDark, false)
	s := sh.side
	q.until(t, "the kinds", func() bool { return len(sh.kinds) > 0 })
	if _, ok := s.children[groupPrefix+argoGroup]; ok {
		t.Fatal("the Argo CD group shows without Argo CD")
	}
	argoFixture(src)
	q.until(t, "the Argo CD group", func() bool {
		return len(s.children[groupPrefix+argoGroup]) == 5 && len(s.children[groupPrefix+argoGroup+"/settings"]) == 2
	})
	if got := titlesOf(s, s.children[groupPrefix+argoGroup]); !slices.Equal(got, []string{"Dashboard", "Applications", "Application Sets", "Projects", "Settings"}) {
		t.Errorf("group %v", got)
	}
	if got := titlesOf(s, s.children[groupPrefix+argoGroup+"/settings"]); !slices.Equal(got, []string{"Repositories", "Clusters"}) {
		t.Errorf("settings %v", got)
	}
	// After Applications (which this fixture does not offer) and Flux CD, before Nodes and Workloads.
	roots := s.children[""]
	if i := slices.Index(roots, groupPrefix+argoGroup); i < 1 || roots[i-1] != engine.ClusterOverview || roots[i+1] != groupPrefix+"workloads" {
		t.Errorf("the group's place: %v", roots)
	}
	custom := s.children[customGroupPrefix+kinds.ArgoCDGroup]
	if !slices.Contains(custom, aliasPrefix+kinds.ArgoApplicationsKey) {
		t.Errorf("argoproj.io under Custom Resources: %v", custom)
	}
	src.Delete(testkit.CRDsGVR, "", kinds.ArgoApplicationsKey)
	q.until(t, "the dashboard and settings to leave", func() bool {
		kids := s.children[groupPrefix+argoGroup]
		return len(kids) == 2 && !slices.Contains(kids, engine.ArgoCDDashboard)
	})
}

// argoOpenDashboard opens the Dashboard and waits for its panel and table.
func argoOpenDashboard(t *testing.T, sh *shell, q *mainQueue) {
	t.Helper()
	sh.dispatch("ui.openView", openViewParams{View: engine.ArgoCDDashboard}, nil)
	q.until(t, "the dashboard", func() bool {
		snap := sh.view.snap
		return sh.view.argo.data != nil && sh.view.argo.data.Synced && snap != nil && snap.View == engine.ArgoCDDashboard && snap.Synced &&
			len(snap.Rows) == 6 && len(sh.view.argo.activity.Objects) == 4
	})
}

// TestArgoDashboardPanel: the strip counts the fixture's Applications; a tile opens the
// Applications view searched for its state; the card's Sync takes the out-of-sync Applications,
// its Refresh all of them, each through its dialog.
func TestArgoDashboardPanel(t *testing.T) {
	sh, w, q, _, _, writer := newArgoShell(t, ThemeDark, true)
	argoOpenDashboard(t, sh, q)
	strip := map[string]int{}
	for _, tile := range sh.view.argo.data.Strip {
		strip[tile.Label] = tile.Count
	}
	if strip["Applications"] != 8 || strip["Synced"] != 5 || strip["Healthy"] != 3 || strip["Out of sync"] != 2 || strip["Degraded"] != 1 ||
		strip["Missing"] != 1 || strip["Progressing"] != 1 {
		t.Errorf("strip %v", strip)
	}
	if got := sh.view.eventsTitle.text; got != "Needs Attention" {
		t.Errorf("table title %q", got)
	}

	// The Sync button: the out-of-sync Applications, through the dialog, without prune.
	test.Tap(sh.view.argo.sync)
	q.until(t, "the sync dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	argoTapButton(t, w, "Sync")
	q.until(t, "the syncs", func() bool { return len(writer.Writes()) == 2 })
	if got := strings.Join(writer.Writes(), ","); got != "sync argocd/ingress,sync argocd/payments" {
		t.Errorf("writes %s", got)
	}
	test.Tap(sh.view.argo.refresh)
	q.until(t, "the refresh dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	for _, r := range findAll[*widget.RadioGroup](w.Canvas().Overlays().Top()) {
		r.SetSelected(r.Options[1]) // Hard
	}
	argoTapButton(t, w, "Refresh")
	q.until(t, "the refreshes", func() bool { return len(writer.Writes()) == 10 })
	for _, wr := range writer.Writes()[2:] {
		if !strings.HasPrefix(wr, "refresh argocd/") || !strings.HasSuffix(wr, " hard") {
			t.Errorf("refresh %s", wr)
		}
	}

	// The dialog preselects every Application; one unchecked is left out.
	test.Tap(sh.view.argo.sync)
	q.until(t, "the sync dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	groups := findAll[*widget.CheckGroup](w.Canvas().Overlays().Top())
	if len(groups) != 1 || len(groups[0].Selected) != 2 {
		t.Fatalf("the dialog's Applications %+v", groups)
	}
	groups[0].SetSelected([]string{"argocd/payments"})
	argoTapButton(t, w, "Sync")
	q.until(t, "the sync of the one left", func() bool { return len(writer.Writes()) == 11 })
	if got := writer.Writes()[10]; got != "sync argocd/payments" {
		t.Errorf("write %s", got)
	}

	// The activity's filter.
	sh.view.argo.filter.SetText("PAYMENTS failed")
	if n := len(sh.view.argo.activity.Objects); n != 1 {
		t.Errorf("%d activities match", n)
	}
	sh.view.argo.filter.SetText("nothing like it")
	if l, ok := sh.view.argo.activity.Objects[0].(*textLine); !ok || l.text != "No activity matches the filter." {
		t.Errorf("no match shows %#v", sh.view.argo.activity.Objects[0])
	}
	sh.view.argo.filter.SetText("")

	// A tile opens the Applications view filtered to its state (the status filter, D-34), the search
	// left alone.
	for _, l := range findAll[*linkText](sh.view.argo.strip) {
		if l.text == "Out of sync" {
			test.Tap(l)
		}
	}
	q.until(t, "the filtered Applications", func() bool {
		snap := sh.view.snap
		return sh.ui.View == kinds.ArgoApplicationsKey && slices.Equal(sh.ui.States, []string{"Sync: OutOfSync"}) && sh.ui.Filter == "" &&
			snap != nil && snap.View == kinds.ArgoApplicationsKey && slices.Equal(snap.States, []string{"Sync: OutOfSync"}) && len(snap.Rows) == 2
	})
	if sh.view.states.button.Text != "Sync: OutOfSync" {
		t.Errorf("states button %q", sh.view.states.button.Text)
	}
}

// argoTapButton taps the button with the label in the topmost overlay.
func argoTapButton(t *testing.T, w fyne.Window, label string) {
	t.Helper()
	for _, b := range findAll[*widget.Button](w.Canvas().Overlays().Top()) {
		if b.Text == label {
			test.Tap(b)
			return
		}
	}
	t.Fatalf("no %q button", label)
}

// TestArgoSyncWithPruneConfirms: the row menu's Sync opens the dialog; with prune on, the engine's
// confirmation follows and names what prune deletes; confirmed, the sync runs with prune.
func TestArgoSyncWithPruneConfirms(t *testing.T) {
	sh, w, q, s, _, writer := newArgoShell(t, ThemeDark, true)
	sh.dispatch("ui.openView", openViewParams{View: kinds.ArgoApplicationsKey}, nil)
	q.until(t, "the Applications", func() bool { return sh.view.snap != nil && sh.view.snap.Synced && len(sh.view.snap.Rows) == 8 })
	ref := model.ObjectRef{Context: "argo", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: "ingress"}
	acts := sh.evaluateActions(ref, nil)
	var labels []string
	for _, a := range acts {
		labels = append(labels, a.cmd.Label)
	}
	// Logs (A81, argocd.logs) after Sync and Refresh, greyed or hidden as the Application allows.
	if got := strings.Join(labels, ","); got != "Sync,Refresh,Edit,Delete" && got != "Copy,Sync,Refresh,Edit,Delete" && got != "Copy,Sync,Refresh,Logs,Edit,Delete" {
		t.Errorf("actions %v", labels)
	}
	for _, a := range acts {
		if a.cmd.ID == argoSyncCommand {
			sh.runAction(a, ref, "")
		}
	}
	q.until(t, "the sync dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	for _, c := range checksIn(w.Canvas().Overlays().Top()) {
		c.SetChecked(true)
	}
	argoTapButton(t, w, "Sync")
	q.until(t, "the confirmation", func() bool {
		top := w.Canvas().Overlays().Top()
		if top == nil {
			return false
		}
		for _, p := range findAll[*paragraph](top) {
			if strings.Contains(p.text, "no longer in Git are deleted") {
				return true
			}
		}
		return false
	})
	for _, b := range findAll[*widget.Button](w.Canvas().Overlays().Top()) {
		if b.Importance == widget.DangerImportance {
			test.Tap(b)
		}
	}
	q.until(t, "the sync", func() bool { return len(writer.Writes()) == 1 })
	if got := writer.Writes()[0]; got != "sync argocd/ingress prune" {
		t.Errorf("write %s", got)
	}
	_ = s
}

func argoRender(t *testing.T, mode ThemeMode, name string, show func(*shell, *mainQueue)) {
	sh, w, q, _, _, _ := newArgoShell(t, mode, true)
	show(sh, q)
	savePNG(t, w, name)
}

func renderArgoDashboard(t *testing.T, mode ThemeMode, name string) {
	argoRender(t, mode, name, func(sh *shell, q *mainQueue) { argoOpenDashboard(t, sh, q) })
}

func TestRenderArgoDashboardDark(t *testing.T) {
	renderArgoDashboard(t, ThemeDark, "argocd_dashboard_dark.png")
}

func TestRenderArgoDashboardLight(t *testing.T) {
	renderArgoDashboard(t, ThemeLight, "argocd_dashboard_light.png")
}

func renderArgoApplications(t *testing.T, mode ThemeMode, name string, drawer bool) {
	argoRender(t, mode, name, func(sh *shell, q *mainQueue) {
		sh.dispatch("ui.openView", openViewParams{View: kinds.ArgoApplicationsKey}, nil)
		q.until(t, "the Applications", func() bool { return sh.view.snap != nil && sh.view.snap.Synced && len(sh.view.snap.Rows) == 8 })
		if drawer {
			ref := model.ObjectRef{Context: "argo", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: "shop"}
			sh.dispatch("ui.select", selectParams{Refs: []model.ObjectRef{ref}}, nil)
			q.until(t, "the drawer", func() bool {
				return sh.drawer.last != nil && sh.drawer.last.Ref.Name == "shop" && !sh.drawer.last.Loading
			})
		}
	})
}

func TestRenderArgoApplicationsDark(t *testing.T) {
	renderArgoApplications(t, ThemeDark, "argocd_applications_dark.png", false)
}

func TestRenderArgoApplicationsLight(t *testing.T) {
	renderArgoApplications(t, ThemeLight, "argocd_applications_light.png", false)
}

func TestRenderArgoApplicationDrawerDark(t *testing.T) {
	renderArgoApplications(t, ThemeDark, "argocd_application_drawer_dark.png", true)
}

func TestRenderArgoApplicationDrawerLight(t *testing.T) {
	renderArgoApplications(t, ThemeLight, "argocd_application_drawer_light.png", true)
}

// TestArgoOpenUIOpensTheForward: the Dashboard's Open Argo CD UI runs argocd.openUI and opens the
// forward's URL in the browser.
func TestArgoOpenUIOpensTheForward(t *testing.T) {
	f := connectedFake(t, 10)
	browser := recordBrowser(t)
	f.handlers["argocd.openUI"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"url": "https://127.0.0.1:43123", "spec": map[string]any{"openInBrowser": true}}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	test.Tap(sh.view.argo.openUI)
	q.until(t, "the browser", func() bool { return len(browser.opened()) == 1 })
	if got := browser.opened()[0]; got != "https://127.0.0.1:43123" {
		t.Errorf("opened %s", got)
	}
}

// TestRenderArgoSyncDialogDark shows the Sync dialog of the Dashboard's out-of-sync Applications.
func TestRenderArgoSyncDialogDark(t *testing.T) {
	sh, w, q, _, _, _ := newArgoShell(t, ThemeDark, true)
	argoOpenDashboard(t, sh, q)
	test.Tap(sh.view.argo.sync)
	q.until(t, "the sync dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	savePNG(t, w, "argocd_sync_dialog_dark.png")
}

// TestRenderArgoRefreshDialogLight shows the Refresh dialog of one Application.
func TestRenderArgoRefreshDialogLight(t *testing.T) {
	sh, w, q, _, _, _ := newArgoShell(t, ThemeLight, true)
	sh.dispatch("ui.openView", openViewParams{View: kinds.ArgoApplicationsKey}, nil)
	q.until(t, "the Applications", func() bool { return sh.view.snap != nil && sh.view.snap.Synced && len(sh.view.snap.Rows) == 8 })
	sh.openArgoRefreshDialog([]model.ObjectRef{{Context: "argo", Kind: kinds.ArgoApplicationsKey, Namespace: "argocd", Name: "payments"}})
	q.until(t, "the refresh dialog", func() bool { return w.Canvas().Overlays().Top() != nil })
	savePNG(t, w, "argocd_refresh_dialog_light.png")
}
