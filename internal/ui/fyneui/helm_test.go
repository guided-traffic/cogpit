package fyneui

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Releases view and drawer as the engine serves them: release Secrets through the real
// virtual kind (helm.NewReleaseProfile), and the drawer the real drawer model. Every name is made up.

// fixtureRelease is one revision of a release.
type fixtureRelease struct {
	ns, name, chart, version, app, status string
	rev                                   int
	ago                                   time.Duration
}

// fixtureReleaseSecret encodes a revision the way Helm's Secrets driver stores it.
func fixtureReleaseSecret(t testing.TB, r fixtureRelease) *unstructured.Unstructured {
	t.Helper()
	deployed := fixedNow.Add(-r.ago)
	rls := map[string]any{
		"name": r.name, "namespace": r.ns, "version": r.rev,
		"info": map[string]any{"status": r.status, "first_deployed": fixedNow.Add(-90 * 24 * time.Hour).Format(time.RFC3339),
			"last_deployed": deployed.Format(time.RFC3339), "description": releaseDescription(r),
			"notes": "The " + r.chart + " controller has been installed.\nGet the admin password with:\n  kubectl get secret " + r.name + "-auth -n " + r.ns + " -o jsonpath='{.data.admin-password}'"},
		"chart": map[string]any{"metadata": map[string]any{"name": r.chart, "version": r.version, "appVersion": r.app},
			"values": map[string]any{"controller": map[string]any{"replicaCount": 1, "image": map[string]any{"tag": "v" + r.app}}}},
		"config": map[string]any{
			"controller": map[string]any{"replicaCount": 2, "service": map[string]any{"type": "LoadBalancer"}},
			"auth":       map[string]any{"adminPassword": "correct-horse-battery-staple"},
		},
		"manifest": "---\n# Source: " + r.chart + "/templates/serviceaccount.yaml\napiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: " + r.name +
			"\n  namespace: " + r.ns + "\n---\n# Source: " + r.chart + "/templates/secret.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: " +
			r.name + "-auth\n  namespace: " + r.ns + "\ntype: Opaque\ndata:\n  admin-password: Y29ycmVjdC1ob3JzZQ==\n---\n# Source: " + r.chart +
			"/templates/service.yaml\napiVersion: v1\nkind: Service\nmetadata:\n  name: " + r.name + "\nspec:\n  type: LoadBalancer\n  ports:\n  - port: 443\n" +
			"---\n# Source: " + r.chart + "/templates/deployment.yaml\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + r.name + "-controller\n" +
			"---\n# Source: " + r.chart + "/templates/clusterrole.yaml\napiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: " + r.name + "\n",
	}
	raw, err := json.Marshal(rls)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("sh.helm.release.v1.%s.v%d", r.name, r.rev)
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "helm.sh/release.v1",
		"metadata": map[string]any{
			"name": name, "namespace": r.ns, "uid": "uid-" + r.ns + "-" + name, "resourceVersion": fmt.Sprint(1000 + r.rev),
			"creationTimestamp": deployed.Format(time.RFC3339),
			"labels":            map[string]any{"owner": "helm", "name": r.name, "version": fmt.Sprint(r.rev), "status": r.status},
		},
		"data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(buf.Bytes())))},
	}}
}

func releaseDescription(r fixtureRelease) string {
	switch {
	case r.status == "failed":
		return `Upgrade "` + r.name + `" failed: client rate limiter Wait returned an error: context deadline exceeded`
	case r.status == "pending-upgrade":
		return "Preparing upgrade"
	case r.rev == 1:
		return "Install complete"
	}
	return "Upgrade complete"
}

// fixtureReleaseSecrets are the release Secrets of a cluster: ingress-nginx with four revisions,
// the others with one or two.
func fixtureReleaseSecrets(t testing.TB) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, r := range []fixtureRelease{
		{"ingress-nginx", "ingress-nginx", "ingress-nginx", "4.11.2", "1.11.2", "superseded", 1, 60 * 24 * time.Hour},
		{"ingress-nginx", "ingress-nginx", "ingress-nginx", "4.11.3", "1.11.3", "superseded", 2, 32 * 24 * time.Hour},
		{"ingress-nginx", "ingress-nginx", "ingress-nginx", "4.12.0", "1.12.0", "superseded", 3, 9 * 24 * time.Hour},
		{"ingress-nginx", "ingress-nginx", "ingress-nginx", "4.12.1", "1.12.1", "deployed", 4, 26 * time.Hour},
		{"cert-manager", "cert-manager", "cert-manager", "v1.16.2", "v1.16.2", "deployed", 1, 45 * 24 * time.Hour},
		{"monitoring", "kube-prometheus-stack", "kube-prometheus-stack", "66.2.1", "v0.78.2", "superseded", 11, 12 * 24 * time.Hour},
		{"monitoring", "kube-prometheus-stack", "kube-prometheus-stack", "66.3.0", "v0.79.0", "pending-upgrade", 12, 7 * time.Minute},
		{"checkout", "checkout-api", "checkout-api", "2.14.0", "2.14.0", "deployed", 4, 3 * 24 * time.Hour},
		{"checkout", "checkout-api", "checkout-api", "2.15.0", "2.15.0", "failed", 5, 2 * time.Hour},
		{"data-pipeline", "kafka", "kafka", "31.1.0", "3.9.0", "uninstalling", 2, 3 * time.Minute},
		{"logging", "loki", "loki", "6.24.0", "3.3.2", "deployed", 1, 20 * 24 * time.Hour},
	} {
		out = append(out, fixtureReleaseSecret(t, r))
	}
	return out
}

// releasesFake is a fake whose Releases view and drawers come from the release Secrets through
// the real profile, with the rollback and uninstall commands in its registry.
func releasesFake(t testing.TB) (*fakeEngine, model.ObjectRef) {
	f := kindsFake(t, helm.ReleaseKind)
	f.kinds = append(f.kinds,
		kindInfo{Kind: "secrets", Title: "Secrets", APIKind: "Secret", Version: "v1", Namespaced: true},
		kindInfo{Kind: helm.ReleaseKind, Title: "Releases", APIKind: "HelmRelease", Group: model.LocalGroup, Version: "v1", Namespaced: true, Virtual: true})
	p := helm.NewReleaseProfile()
	secrets := fixtureReleaseSecrets(t)
	objs := p.Derive(secrets)
	rows := make([]model.Row, 0, len(objs))
	byRef := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		rows = append(rows, model.BuildRow(p, "demo", o))
		ref := model.ObjectRef{Context: "demo", Kind: helm.ReleaseKind, Namespace: o.GetNamespace(), Name: o.GetName()}
		m, err := engine.BuildDetail(p, ref, o, nil, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		for i, it := range m.Sections[0].Items {
			if it.Name == "Updated" { // the drawer's age against the tests' clock, in UTC
				m.Sections[0].Items[i].Value = model.TimeFormat{Zone: time.UTC}.WriteMarks(helm.UpdatedText(model.NestedTime(o, "updated"), fixedNow))
			}
		}
		f.details[ref.String()] = m
		byRef[ref.Namespace+"/"+ref.Name] = o
	}
	secretOf := func(o *unstructured.Unstructured) *unstructured.Unstructured {
		for _, s := range secrets {
			if s.GetNamespace() == o.GetNamespace() && s.GetName() == helm.ReleaseSecret(o) {
				return s
			}
		}
		return nil
	}
	params := func(inv engine.Invocation, p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	f.handlers["helm.release.values"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p helmValuesParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		o := byRef[p.Ref.Namespace+"/"+p.Ref.Name]
		if o == nil {
			return nil, engine.ErrNotFound
		}
		return helm.ReleaseValues(secretOf(o), p.All)
	}
	f.handlers["helm.release.history"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p refParams
		if err := params(inv, &p); err != nil {
			return nil, err
		}
		o := byRef[p.Ref.Namespace+"/"+p.Ref.Name]
		if o == nil {
			return nil, engine.ErrNotFound
		}
		return releaseHistory{Ref: p.Ref, Revision: helm.ReleaseRevision(o), UID: string(o.GetUID()), History: helm.ReleaseHistory(o)}, nil
	}
	f.tables[helm.ReleaseKind] = &engine.TableSnapshot{
		View: helm.ReleaseKind, Context: "demo", Title: p.Title, Namespaced: true, Columns: p.Columns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true,
	}
	targets := func(_ *engine.Session, p refParams) ([]model.ObjectRef, error) { return []model.ObjectRef{p.Ref}, nil }
	run := func(context.Context, *engine.Session, refParams) (any, error) { return nil, nil }
	for _, c := range []engine.Spec[refParams]{
		{ID: "helm.release.rollback", Label: "Rollback", Kinds: []string{helm.ReleaseKind}, Order: 200, Danger: engine.DangerDestructive, Targets: targets, Run: run},
		{ID: helm.UpgradeTabCommand, Label: "Upgrade", Kinds: []string{helm.ReleaseKind}, Order: 300, Danger: engine.DangerUI, Run: run},
		{ID: "helm.release.uninstall", Label: "Delete", Kinds: []string{helm.ReleaseKind}, Order: engine.OrderDelete, Danger: engine.DangerDestructive, Targets: targets, Run: run},
	} {
		if err := engine.Register(f.reg, c); err != nil {
			t.Fatal(err)
		}
	}
	f.disabled["object.delete"] = notApplicable // not to a virtual kind: helm.release.uninstall takes its place
	ref := model.ObjectRef{Context: "demo", Kind: helm.ReleaseKind, Namespace: "ingress-nginx", Name: "ingress-nginx"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	return f, ref
}

// The Releases view and drawer: one row per release with its newest revision and Lens's status
// text, the sidebar's Helm group, and the drawer as Lens's (captured 2026-10-12): "Release" and
// the name, the icons Rollback, Upgrade, Delete, Properties with an Upgrade button beside Chart,
// the values in an editor, the notes, the resources by kind, no events.
func TestReleasesViewAndDrawer(t *testing.T) {
	f, _ := releasesFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the releases and drawer", func() bool {
		rv, _ := sh.drawer.kept[releaseValuesKey].(*releaseValues)
		return sh.view.snap != nil && sh.drawer.last != nil && len(sh.kinds) > 0 && len(sh.drawer.actions.Objects) == 3 && rv != nil && rv.revision > 0
	})
	if got := len(sh.view.snap.Rows); got != 6 {
		t.Errorf("%d releases, want 6", got)
	}
	if kids := sh.side.children[groupPrefix+"helm"]; len(kids) != 1 || kids[0] != helm.ReleaseKind || sh.side.titles[kids[0]] != "Releases" {
		t.Errorf("Helm group %v", kids)
	}
	if sh.drawer.title.text != "Release: ingress-nginx" || sh.drawer.kind.Visible() {
		t.Errorf("drawer title %q %q", sh.drawer.kind.text, sh.drawer.title.text)
	}
	var tips []string
	for _, o := range sh.drawer.actions.Objects {
		tips = append(tips, o.(*tipButton).tip)
	}
	if strings.Join(tips, ",") != "Rollback,Upgrade,Delete" {
		t.Errorf("icons %v", tips)
	}
	texts := releaseDrawerText(sh.drawer)
	for _, s := range []string{"4.12.1", "Deployed", "26h ago (2026-10-10 12:30:00)", "ingress-nginx-controller", "has been installed", "Resources", "ClusterRole"} {
		if !strings.Contains(texts, s) {
			t.Errorf("the drawer lacks %q", s)
		}
	}
	if strings.Contains(texts, "Events") || strings.Contains(texts, "Y29ycmVjdC1ob3JzZQ==") {
		t.Error("the release drawer shows events or a Secret's data")
	}
	if b := drawerButtons(sh.drawer)["Upgrade"]; len(b) != 1 {
		t.Errorf("Upgrade buttons beside Chart: %d", len(b))
	}
	rv := sh.drawer.kept[releaseValuesKey].(*releaseValues)
	if got := rv.view.Text(); !strings.Contains(got, "adminPassword: correct-horse-battery-staple") || !strings.Contains(got, "image:") {
		t.Errorf("values editor %q", got)
	}
}

// helmEngine is the fake engine with the commands the Helm banner dispatches: it answers
// helm.pendingOperations with canned actions and records the others.
type helmEngine struct {
	*fakeEngine
	mu      sync.Mutex
	pending []engine.PendingHelmOpInfo
	actions []engine.Invocation
}

func (h *helmEngine) Dispatch(ctx context.Context, inv engine.Invocation) (any, error) {
	switch inv.ID {
	case "helm.pendingOperations":
		h.mu.Lock()
		defer h.mu.Unlock()
		return append([]engine.PendingHelmOpInfo(nil), h.pending...), nil
	case "helm.release.rollback", "helm.release.uninstall", "helm.dismissOperation", "ui.navigate":
		h.mu.Lock()
		h.actions = append(h.actions, inv)
		h.mu.Unlock()
		return map[string]any{"action": "ok"}, nil
	}
	return h.fakeEngine.Dispatch(ctx, inv)
}

func (h *helmEngine) set(list []engine.PendingHelmOpInfo) {
	h.mu.Lock()
	h.pending = list
	h.mu.Unlock()
	h.emit(engine.Event{Type: engine.EventHelm})
}

func (h *helmEngine) recorded() []engine.Invocation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]engine.Invocation(nil), h.actions...)
}

func newHelmTestShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *helmEngine) {
	t.Helper()
	f, _ := releasesFake(t)
	f.ui.Context = "kind-cogpit"
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.ui.Selected, f.ui.Drawer = nil, nil
	f.diag.Context, f.diag.Demo, f.diag.DemoStats = "kind-cogpit", false, nil
	h := &helmEngine{fakeEngine: f}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(h, a, w, Options{Theme: mode}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.confirmer.armDelay = 0
	h.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	return sh, w, q, h
}

// fixtureHelmOps: a rollback a crash interrupted (the release left pending-rollback), and an
// uninstall that failed.
func fixtureHelmOps() []engine.PendingHelmOpInfo {
	started := time.Date(2026, 10, 11, 13, 41, 0, 0, time.UTC)
	return fixtureHelmOpsAt(started)
}

// fixtureHelmInstallOp is an install a crash interrupted, its release left pending-install.
func fixtureHelmInstallOp() engine.PendingHelmOpInfo {
	started := time.Date(2026, 10, 11, 13, 52, 0, 0, time.UTC)
	return engine.PendingHelmOpInfo{
		PendingHelmOp: engine.PendingHelmOp{Context: "kind-cogpit", Namespace: "web", Release: "nginx-x2k4q",
			Action: engine.HelmInstall, Chart: "bitnami/nginx", ChartVersion: "18.2.0", Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
		Status: engine.HelmOpStatusInterrupted, ReleaseStatus: "pending-install", ReleaseRevision: 1,
	}
}

func fixtureHelmOpsAt(started time.Time) []engine.PendingHelmOpInfo {
	return []engine.PendingHelmOpInfo{
		{
			PendingHelmOp: engine.PendingHelmOp{Context: "kind-cogpit", Namespace: "ingress-nginx", Release: "ingress-nginx",
				Action: engine.HelmRollback, Revision: 3, FromRevision: 4, Started: started, StartedBy: engine.OriginGUI, State: engine.HelmOpRunning},
			Status: engine.HelmOpStatusInterrupted, ReleaseStatus: "pending-rollback", ReleaseRevision: 5,
		},
		{
			PendingHelmOp: engine.PendingHelmOp{Context: "kind-cogpit", Namespace: "data-pipeline", Release: "kafka",
				Action: engine.HelmUninstall, FromRevision: 2, Started: started.Add(6 * time.Minute), StartedBy: engine.OriginExternalAgent,
				State: engine.HelmOpFailed, Error: `helm uninstall data-pipeline/kafka: failed to delete release: kafka`},
			Status: engine.HelmOpStatusFailed, ReleaseStatus: "uninstalling", ReleaseRevision: 2,
		},
	}
}

func helmBannerButtons(b *helmBanner) map[string][]*widget.Button {
	out := map[string][]*widget.Button{}
	for _, x := range findAll[*widget.Button](b.root) {
		out[x.Text] = append(out[x.Text], x)
	}
	return out
}

func TestHelmBannerShowsAndActs(t *testing.T) {
	sh, _, q, h := newHelmTestShell(t, ThemeDark)
	if sh.helm.root.Visible() {
		t.Fatal("banner visible without pending actions")
	}
	h.set(fixtureHelmOps())
	q.until(t, "the banner", func() bool { return sh.helm.root.Visible() && len(sh.helm.ops) == 2 })
	b := helmBannerButtons(sh.helm)
	if len(b["Finish rollback"]) != 1 || len(b["Finish uninstall"]) != 1 || len(b["Dismiss"]) != 2 {
		t.Fatalf("buttons %v", b)
	}
	// Each button dispatches on a goroutine of its own: wait for each action before the next, so
	// that the recorded order is the order of the taps.
	for i, tap := range []*widget.Button{b["Finish rollback"][0], b["Finish uninstall"][0], b["Dismiss"][1]} {
		test.Tap(tap)
		q.until(t, fmt.Sprintf("action %d", i+1), func() bool { return len(h.recorded()) == i+1 })
	}
	want := []struct{ id, params string }{
		{"helm.release.rollback", `{"ref":{"context":"kind-cogpit","kind":"helmreleases","namespace":"ingress-nginx","name":"ingress-nginx"},"revision":3}`},
		{"helm.release.uninstall", `{"ref":{"context":"kind-cogpit","kind":"helmreleases","namespace":"data-pipeline","name":"kafka"}}`},
		{"helm.dismissOperation", `{"ref":{"context":"kind-cogpit","kind":"helmreleases","namespace":"data-pipeline","name":"kafka"}}`},
	}
	for i, w := range want {
		got := h.recorded()[i]
		if got.ID != w.id || string(got.Params) != w.params || got.Origin != engine.OriginGUI {
			t.Errorf("action %d: %s %s, want %s %s", i, got.ID, got.Params, w.id, w.params)
		}
	}
	if got := helmOpDetail(fixtureHelmOps()[0], germanTime); !strings.Contains(got, "the release is pending-rollback at revision 5 · Cogpit ended before the action did") {
		t.Errorf("detail %q", got)
	}
	// An action another live Cogpit process runs says so, as a drain does (H-22; review of the
	// fourth run, R-9); one this window runs does not.
	elsewhere := fixtureHelmOps()[0]
	elsewhere.Status, elsewhere.Elsewhere, elsewhere.Owner = engine.HelmOpStatusRunning, true, &engine.JournalOwner{ID: "abc", PID: 4242}
	if got := helmOpDetail(elsewhere, germanTime); !strings.Contains(got, "in another Cogpit window (process 4242)") {
		t.Errorf("running elsewhere: %q", got)
	}
	elsewhere.Elsewhere = false
	if got := helmOpDetail(elsewhere, germanTime); strings.Contains(got, "another Cogpit window") {
		t.Errorf("running here: %q", got)
	}
	running := fixtureHelmOps()[:1]
	running[0].Status = engine.HelmOpStatusRunning
	h.set(running)
	q.until(t, "the running action", func() bool {
		return len(sh.helm.ops) == 1 && sh.helm.ops[0].Status == engine.HelmOpStatusRunning
	})
	if b := helmBannerButtons(sh.helm); len(b) != 0 {
		t.Errorf("buttons on a running action: %v", b)
	}
	if got := helmOpTitle(sh.helm.ops[0]); got != "Rolling back release ingress-nginx to revision 3" {
		t.Errorf("title %q", got)
	}
	h.set(nil)
	q.until(t, "the banner to go", func() bool { return !sh.helm.root.Visible() })
}

// An install a crash interrupted is offered nothing (owner, 2026-10-14 evening, A17): its row says
// what happened and the release's state, and offers Show release, which opens the release in the
// Helm Releases view (ui.navigate), and Dismiss; no Uninstall, no other action on the release. A
// release that no longer exists has nothing to show: Dismiss alone.
func TestHelmBannerOffersNothingAfterAnInstall(t *testing.T) {
	sh, _, q, h := newHelmTestShell(t, ThemeDark)
	op := fixtureHelmInstallOp()
	h.set([]engine.PendingHelmOpInfo{op})
	q.until(t, "the banner", func() bool { return sh.helm.root.Visible() && len(sh.helm.ops) == 1 })
	b := helmBannerButtons(sh.helm)
	if len(b) != 2 || len(b[showReleaseLabel]) != 1 || len(b["Dismiss"]) != 1 {
		t.Fatalf("buttons %v: want Show release and Dismiss only", b)
	}
	if got := helmOpTitle(op); got != "The install of release nginx-x2k4q from bitnami/nginx 18.2.0 did not finish" {
		t.Errorf("title %q", got)
	}
	if got := helmOpDetail(op, germanTime); !strings.Contains(got, "the release is pending-install at revision 1 · Cogpit ended before the action did") {
		t.Errorf("detail %q", got)
	}
	test.Tap(b[showReleaseLabel][0])
	q.until(t, "the navigation", func() bool { return len(h.recorded()) == 1 })
	got := h.recorded()[0]
	if got.ID != "ui.navigate" || string(got.Params) != `{"ref":{"context":"kind-cogpit","kind":"helmreleases","namespace":"web","name":"nginx-x2k4q"}}` {
		t.Errorf("Show release dispatched %s %s", got.ID, got.Params)
	}

	failed := op
	failed.Status, failed.State, failed.Error = engine.HelmOpStatusFailed, engine.HelmOpFailed, "helm install web/nginx-x2k4q: timed out waiting for the condition"
	failed.ReleaseStatus = "failed"
	missing := op
	missing.Release, missing.ReleaseStatus, missing.ReleaseRevision = "gone", engine.HelmReleaseMissing, 0
	h.set([]engine.PendingHelmOpInfo{failed, missing})
	q.until(t, "the failed and the missing install", func() bool { return len(sh.helm.ops) == 2 && sh.helm.ops[0].Status == engine.HelmOpStatusFailed })
	b = helmBannerButtons(sh.helm)
	if len(b) != 2 || len(b[showReleaseLabel]) != 1 || len(b["Dismiss"]) != 2 {
		t.Errorf("buttons %v: a failed install shows its release, a missing one only Dismiss", b)
	}
	if got := helmOpDetail(failed, germanTime); !strings.Contains(got, "the release is failed at revision 1 · helm install web/nginx-x2k4q: timed out") {
		t.Errorf("failed detail %q", got)
	}
	for _, inv := range h.recorded() {
		if inv.ID != "ui.navigate" {
			t.Errorf("an install's row ran %s", inv.ID)
		}
	}
}

func renderHelmBanner(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, h := newHelmTestShell(t, mode)
	h.set(append(fixtureHelmOps(), fixtureHelmInstallOp()))
	q.until(t, "the banner", func() bool { return sh.helm.root.Visible() && len(sh.helm.ops) == 3 && sh.view.snap != nil })
	savePNG(t, w, name)
}

func TestRenderHelmBannerDark(t *testing.T) { renderHelmBanner(t, ThemeDark, "helm_banner_dark.png") }
