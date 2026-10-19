package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Flux kind keys and resources of the tests.
const (
	ksKey  = "kustomizations.kustomize.toolkit.fluxcd.io"
	hrKey  = "helmreleases.helm.toolkit.fluxcd.io"
	alKey  = "alerts.notification.toolkit.fluxcd.io"
	extKey = "externalartifacts.source.toolkit.fluxcd.io"
)

var (
	ksGVR  = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	hrGVR  = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	alGVR  = schema.GroupVersionResource{Group: "notification.toolkit.fluxcd.io", Version: "v1beta3", Resource: "alerts"}
	extGVR = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "externalartifacts"}
)

// fluxWriter records the Flux patches; err, when set, is every patch's answer, or, with failOn,
// the answer to the patches of that resource only.
type fluxWriter struct {
	fakeWriter
	mu      sync.Mutex
	patches []string
	err     error
	failOn  string
	// onPatch plays the object's controller after a patch went through (fakeFluxController).
	onPatch func(gvr schema.GroupVersionResource, namespace, name string)
}

func (w *fluxWriter) PatchFlux(_ context.Context, gvr schema.GroupVersionResource, namespace, name, uid string, req cluster.FluxRequest) error {
	body, err := cluster.FluxPatch(time.Unix(0, 0).UTC(), uid, req)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.err != nil && (w.failOn == "" || w.failOn == gvr.Resource) {
		w.mu.Unlock()
		return w.err
	}
	w.patches = append(w.patches, fmt.Sprintf("%s/%s %s/%s %s", gvr.Resource, gvr.Version, namespace, name, body))
	onPatch := w.onPatch
	w.mu.Unlock()
	if onPatch != nil {
		onPatch(gvr, namespace, name)
	}
	return nil
}

func (w *fluxWriter) Patches() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.patches)
}

// fluxConnector connects "test" with the custom kinds watch on and the Flux writer.
type fluxConnector struct {
	src    *testkit.MemorySource
	writer *fluxWriter
}

func (c fluxConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "test"}}, nil
}

func (c fluxConnector) Connect(context.Context, string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: "test", Namespace: "default", Cache: ch, Profiles: testProfiles(),
		CustomResources: true, Close: ch.Close, Conn: &cluster.Conn{}, Actions: c.writer}, nil
}

// fluxObject builds a Flux object; suspended sets spec.suspend, ready the Ready condition.
func fluxObject(apiVersion, kind, namespace, name string, suspended bool, ready string) *unstructured.Unstructured {
	obj := testkit.Object(apiVersion, kind, namespace, name, map[string]any{"spec": map[string]any{"suspend": suspended}})
	if ready != "" {
		obj.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": ready, "message": "m"}}}
	}
	return obj
}

// fluxSource is a cluster with the Flux definitions and objects: Kustomizations apps (ready),
// infra (failing) and paused (suspended) in flux-system, a Helm release, a static Alert and an
// External Artifact.
func fluxSource() *testkit.MemorySource {
	src := seed()
	for _, d := range []struct {
		gvr  schema.GroupVersionResource
		kind string
	}{{ksGVR, "Kustomization"}, {hrGVR, "HelmRelease"}, {alGVR, "Alert"}, {extGVR, "ExternalArtifact"}} {
		src.Set(fluxCRDsGVR, testkit.CRD(d.gvr.Group, d.gvr.Resource, d.kind, true, testkit.CRDVersion{Name: d.gvr.Version, Served: true, Storage: true}))
	}
	src.Set(ksGVR, fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "apps", false, "True"))
	src.Set(ksGVR, fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "infra", false, "False"))
	src.Set(ksGVR, fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "paused", true, "True"))
	src.Set(hrGVR, fluxObject("helm.toolkit.fluxcd.io/v2", "HelmRelease", "shop", "podinfo", false, "True"))
	src.Set(alGVR, fluxObject("notification.toolkit.fluxcd.io/v1beta3", "Alert", "flux-system", "on-call", false, ""))
	src.Set(extGVR, fluxObject("source.toolkit.fluxcd.io/v1", "ExternalArtifact", "flux-system", "bundle", false, "True"))
	return src
}

var fluxCRDsGVR = testkit.CRDsGVR

func newFluxHarness(t *testing.T, opts engine.Options) (*harness, *fluxWriter) {
	t.Helper()
	return newFluxHarnessOn(t, opts, fluxSource())
}

// newFluxHarnessOn connects a session to src, a cluster with fluxSource's objects at least.
func newFluxHarnessOn(t *testing.T, opts engine.Options, src *testkit.MemorySource) (*harness, *fluxWriter) {
	t.Helper()
	w := &fluxWriter{}
	h := &harness{src: src, writer: &w.fakeWriter}
	opts.Connector = fluxConnector{src: src, writer: w}
	opts.SnapshotInterval = 5 * time.Millisecond
	h.s = engine.New(opts)
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		for _, r := range []model.ObjectRef{ksRef("apps"), ksRef("paused"), hrRef(), alRef(), extRef()} {
			if _, err := h.s.Lookup(r); err != nil {
				return false
			}
		}
		return true
	}) {
		t.Fatal("the Flux objects never reached the cache")
	}
	return h, w
}

func ksRef(name string) model.ObjectRef {
	return model.ObjectRef{Kind: ksKey, Namespace: "flux-system", Name: name}
}
func hrRef() model.ObjectRef { return model.ObjectRef{Kind: hrKey, Namespace: "shop", Name: "podinfo"} }
func alRef() model.ObjectRef {
	return model.ObjectRef{Kind: alKey, Namespace: "flux-system", Name: "on-call"}
}
func extRef() model.ObjectRef {
	return model.ObjectRef{Kind: extKey, Namespace: "flux-system", Name: "bundle"}
}

// TestFluxActionsPatch: Reconcile, Suspend and Resume send the flux CLI's patches at the served
// version, bound to the cached UID; force only for Helm releases.
func TestFluxActionsPatch(t *testing.T) {
	h, w := newFluxHarness(t, engine.Options{})
	gui := engine.OriginGUI
	h.must(t, gui, "flux.reconcile", map[string]any{"ref": ksRef("apps")}, nil)
	h.must(t, gui, "flux.suspend", map[string]any{"ref": ksRef("apps")}, nil)
	h.must(t, gui, "flux.resume", map[string]any{"ref": ksRef("paused")}, nil)
	h.must(t, gui, "flux.reconcile", map[string]any{"ref": hrRef(), "force": true}, nil)
	stamp := `"1970-01-01T00:00:00Z"`
	want := []string{
		`kustomizations/v1 flux-system/apps {"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":` + stamp + `},"uid":"uid-flux-system-apps"}}`,
		`kustomizations/v1 flux-system/apps {"metadata":{"uid":"uid-flux-system-apps"},"spec":{"suspend":true}}`,
		`kustomizations/v1 flux-system/paused {"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":` + stamp + `},"uid":"uid-flux-system-paused"},"spec":{"suspend":false}}`,
		`helmreleases/v2 shop/podinfo {"metadata":{"annotations":{"reconcile.fluxcd.io/forceAt":` + stamp + `,"reconcile.fluxcd.io/requestedAt":` + stamp + `},"uid":"uid-shop-podinfo"}}`,
	}
	if got := w.Patches(); !slices.Equal(got, want) {
		t.Errorf("patches\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var disabled *engine.DisabledError
	if err := h.run(t, gui, "flux.reconcile", map[string]any{"ref": ksRef("apps"), "force": true}, nil); !errors.As(err, &disabled) || !strings.Contains(err.Error(), "Helm releases only") {
		t.Errorf("force on a Kustomization: %v", err)
	}

	// The API server refused the UID: another object holds the name now.
	w.mu.Lock()
	w.err = fmt.Errorf("patch: %w", cluster.ErrReplaced)
	w.mu.Unlock()
	if err := h.run(t, gui, "flux.reconcile", map[string]any{"ref": ksRef("infra")}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("a replaced object: %v", err)
	}
}

// TestFluxActionsApply: each action shows where it applies (Suspend or Resume by the object's
// state; Reconcile greyed on a suspended object, hidden on a static Alert and an External
// Artifact), in the kind's row menu before Edit and Delete, greyed by the access reviews.
func TestFluxActionsApply(t *testing.T) {
	h, _ := newFluxHarness(t, engine.Options{})
	reg := h.s.Registry()
	state := func(id string, ref model.ObjectRef) string {
		cmd, ok := reg.Get(id)
		if !ok {
			t.Fatalf("no command %s", id)
		}
		raw, _ := json.Marshal(map[string]any{"ref": ref})
		switch err := cmd.Enabled(h.s, raw); {
		case err == nil:
			return "enabled"
		case errors.Is(err, engine.ErrNotApplicable):
			return "hidden"
		default:
			return "greyed: " + err.Error()
		}
	}
	for _, tc := range []struct {
		id   string
		ref  model.ObjectRef
		want string
	}{
		{"flux.reconcile", ksRef("apps"), "enabled"},
		{"flux.reconcile", ksRef("paused"), "greyed: paused is suspended: resume it to reconcile it"},
		{"flux.reconcile", alRef(), "hidden"},
		{"flux.reconcile", extRef(), "hidden"},
		{"flux.suspend", ksRef("apps"), "enabled"},
		{"flux.suspend", ksRef("paused"), "hidden"},
		{"flux.resume", ksRef("apps"), "hidden"},
		{"flux.resume", ksRef("paused"), "enabled"},
		{"flux.suspend", alRef(), "enabled"},
		{"flux.suspend", extRef(), "hidden"},
		{"flux.suspend", model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}, "hidden"},
	} {
		if got := state(tc.id, tc.ref); got != tc.want {
			t.Errorf("%s on %s: %s, want %s", tc.id, tc.ref.Name, got, tc.want)
		}
	}
	var ids []string
	for _, c := range reg.Actions(ksKey) {
		if !c.DrawerOnly {
			ids = append(ids, c.ID)
		}
	}
	if want := []string{"flux.reconcile", "flux.reconcileWithSource", "flux.resume", "flux.suspend", "flux.logs", "object.edit", engine.RemoveFinalizersCommand, "object.delete"}; !slices.Equal(ids, want) {
		t.Errorf("row menu %v, want %v", ids, want)
	}
	// Only Reconcile takes force (Helm releases).
	for id, want := range map[string]bool{"flux.reconcile": true, "flux.reconcileMany": true, "flux.suspend": false, "flux.resume": false, "flux.suspendMany": false, "flux.resumeMany": false} {
		cmd, _ := reg.Get(id)
		if got := cmd.Schema.Properties["force"] != nil; got != want {
			t.Errorf("%s takes force: %v", id, got)
		}
	}
	h.s.Backend().Access = denyAccess{"patch kustomizations flux-system": true}
	if got := state("flux.reconcile", ksRef("apps")); !strings.Contains(got, "not permitted") || !strings.Contains(got, "patch") {
		t.Errorf("denied: %s", got)
	}
	if got := state("flux.reconcile", hrRef()); got != "enabled" {
		t.Errorf("another kind: %s", got)
	}
}

// TestFluxActionsAgentsNeedAcceptance (D-2): an agent's Suspend asks the user, naming the object,
// its namespace and the context; declined, nothing is patched; accepted, the patch carries the UID
// the confirmation recorded; both are in the audit log. An object replaced under its name while the
// user was asked is not patched.
func TestFluxActionsAgentsNeedAcceptance(t *testing.T) {
	audit := &syncBuffer{}
	var asked []engine.ConfirmRequest
	answer := false
	var during func()
	h, w := newFluxHarness(t, engine.Options{Audit: audit, Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		if during != nil {
			during()
		}
		return answer, nil
	})})
	agent := engine.OriginExternalAgent
	if err := h.run(t, agent, "flux.suspend", map[string]any{"ref": ksRef("apps")}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Fatalf("declined: %v", err)
	}
	if len(w.Patches()) != 0 {
		t.Fatalf("a declined suspend patched %v", w.Patches())
	}
	req := asked[0]
	if req.Danger != engine.DangerMutating || req.Context != "test" || !slices.Equal(req.Namespaces, []string{"flux-system"}) ||
		len(req.Objects) != 1 || req.Objects[0].Name != "apps" || !slices.Equal(req.UIDs, []string{"uid-flux-system-apps"}) {
		t.Errorf("confirmation %+v", req)
	}
	answer = true
	h.must(t, agent, "flux.suspend", map[string]any{"ref": ksRef("apps")}, nil)
	if p := w.Patches(); len(p) != 1 || !strings.Contains(p[0], `"uid":"uid-flux-system-apps"`) {
		t.Errorf("patches %v", p)
	}
	lines := audit.Lines()
	if n := len(lines); n < 3 || lines[n-1].Command != "flux.suspend" || !lines[n-1].OK || lines[n-2].Phase != "requested" {
		t.Errorf("audit %+v", lines)
	}

	// Replaced while the user was asked: the cache holds another UID when the command runs.
	during = func() {
		obj := fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "infra", false, "False")
		obj.SetUID("uid-new")
		h.src.Set(ksGVR, obj)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		testkit.WaitFor(ctx, func() bool { o, err := h.s.Lookup(ksRef("infra")); return err == nil && o.GetUID() == "uid-new" })
	}
	if err := h.run(t, agent, "flux.reconcile", map[string]any{"ref": ksRef("infra")}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("replaced while asked: %v", err)
	}
	if len(w.Patches()) != 1 {
		t.Errorf("a replaced object was patched: %v", w.Patches())
	}
}

// TestFluxActionsOnASelection: the selection's menu offers the Flux actions for Flux kinds only;
// each acts on the selected objects it applies to, one after the other, and skips the others.
func TestFluxActionsOnASelection(t *testing.T) {
	h, w := newFluxHarness(t, engine.Options{})
	refs := []model.ObjectRef{ksRef("apps"), ksRef("infra"), ksRef("paused"), hrRef()}
	var ids []string
	for _, c := range h.s.Registry().SelectionActions(engine.SelectionKinds(refs)) {
		ids = append(ids, c.ID)
	}
	if want := []string{"flux.reconcileMany", "flux.reconcileWithSourceMany", "flux.resumeMany", "flux.suspendMany", "object.deleteMany"}; !slices.Equal(ids, want) {
		t.Errorf("selection actions %v, want %v", ids, want)
	}
	ids = nil
	for _, c := range h.s.Registry().SelectionActions(engine.SelectionKinds(append(refs, extRef()))) {
		ids = append(ids, c.ID)
	}
	if slices.Contains(ids, "flux.reconcileMany") || slices.Contains(ids, "flux.suspendMany") {
		t.Errorf("an External Artifact in the selection: %v", ids)
	}

	gui := engine.OriginGUI
	var res commands.FluxManyResult
	h.must(t, gui, "flux.reconcileMany", map[string]any{"refs": refs}, &res)
	if len(res.Done) != 3 || res.Done[2].Name != "podinfo" {
		t.Errorf("reconcile done %+v", res.Done)
	}
	h.must(t, gui, "flux.resumeMany", map[string]any{"refs": refs}, &res)
	if len(res.Done) != 1 || res.Done[0].Name != "paused" {
		t.Errorf("resume done %+v", res.Done)
	}
	if n := len(w.Patches()); n != 4 {
		t.Errorf("%d patches: %v", n, w.Patches())
	}
	only := []model.ObjectRef{ksRef("paused")}
	if err := h.run(t, gui, "flux.suspendMany", map[string]any{"refs": only}, nil); !strings.Contains(fmt.Sprint(err), "no selected object to suspend") {
		t.Errorf("suspend of a suspended selection: %v", err)
	}
	if err := h.run(t, gui, "flux.reconcileMany", map[string]any{"refs": refs, "force": true}, nil); !strings.Contains(fmt.Sprint(err), "Helm releases only") {
		t.Errorf("force on Kustomizations: %v", err)
	}

	// A failure does not stop the others; the error names both.
	w.mu.Lock()
	w.err = errors.New("the API server is away")
	w.mu.Unlock()
	err := h.run(t, gui, "flux.suspendMany", map[string]any{"refs": refs}, nil)
	if err == nil || !strings.Contains(err.Error(), "suspended: 0 of 3 objects") || !strings.Contains(err.Error(), "the API server is away") {
		t.Errorf("failures: %v", err)
	}
}

// TestFluxDashboardCommand: agents read the dashboard (counts by state, the table of objects that
// are not ready); the user opens it with ui.openView, selects every row of it (objects of several
// Flux kinds) and reconciles them from the selection.
func TestFluxDashboardCommand(t *testing.T) {
	h, w := newFluxHarness(t, engine.Options{})
	agent := engine.OriginExternalAgent
	var res commands.FluxDashboardResult
	h.must(t, agent, "flux.dashboard", map[string]any{"table": true}, &res)
	counts := map[string]string{}
	for _, k := range res.Kinds {
		var states []string
		for _, st := range k.States {
			states = append(states, fmt.Sprintf("%s=%d", st.State, st.Count))
		}
		counts[k.Title] = strings.Join(states, " ")
	}
	if counts["Kustomizations"] != "ready=1 notReady=1 suspended=1" || counts["Helm Releases"] != "ready=1" ||
		counts["Alerts"] != "ready=1" || counts["External Artifacts"] != "ready=1" || len(res.Kinds) != 4 {
		t.Errorf("counts %v", counts)
	}
	if res.NotReady == nil || len(res.NotReady.Rows) != 2 {
		t.Fatalf("not ready %+v", res.NotReady)
	}

	h.must(t, engine.OriginGUI, "ui.openView", map[string]any{"view": engine.FluxDashboard}, nil)
	waitTable(t, h.s, engine.FluxDashboard, func(t *engine.TableSnapshot) bool { return t.Synced && len(t.Rows) == 2 })
	h.must(t, engine.OriginGUI, "ui.select", map[string]any{"all": true, "noDrawer": true}, nil)
	sel := h.s.UIState().Selected
	if len(sel) != 2 {
		t.Fatalf("selected %v", sel)
	}
	var ids []string
	for _, c := range h.s.Registry().SelectionActions(engine.SelectionKinds(sel)) {
		ids = append(ids, c.ID)
	}
	if !slices.Contains(ids, "flux.reconcileMany") || !slices.Contains(ids, "flux.resumeMany") {
		t.Errorf("selection actions %v", ids)
	}
	var done commands.FluxManyResult
	h.must(t, engine.OriginGUI, "flux.reconcileMany", map[string]any{"refs": sel}, &done)
	if len(done.Done) != 1 || done.Done[0].Name != "infra" || len(w.Patches()) != 1 {
		t.Errorf("reconciled %+v, patches %v", done.Done, w.Patches())
	}
}

// TestFluxConfirmationNamesTheIdentity (A34, owner 2026-10-14 evening; H-64): the confirmation of a
// Flux action says whose rights the reconciliation uses: the object's spec.serviceAccountName, or
// the controller's own service account without one; a selection names each identity once.
func TestFluxConfirmationNamesTheIdentity(t *testing.T) {
	src := fluxSource()
	hr := fluxObject("helm.toolkit.fluxcd.io/v2", "HelmRelease", "shop", "podinfo", false, "True")
	if err := unstructured.SetNestedField(hr.Object, "shop-deployer", "spec", "serviceAccountName"); err != nil {
		t.Fatal(err)
	}
	src.Set(hrGVR, hr)
	var asked []engine.ConfirmRequest
	h, _ := newFluxHarnessOn(t, engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return false, nil
	})}, src)
	agent := engine.OriginExternalAgent
	_ = h.run(t, agent, "flux.reconcile", map[string]any{"ref": ksRef("apps")}, nil)
	hrRef := model.ObjectRef{Kind: "helmreleases.helm.toolkit.fluxcd.io", Namespace: "shop", Name: "podinfo"}
	_ = h.run(t, agent, "flux.reconcile", map[string]any{"ref": hrRef}, nil)
	_ = h.run(t, agent, "flux.reconcileMany", map[string]any{"refs": []model.ObjectRef{ksRef("apps"), ksRef("infra"), hrRef}}, nil)
	if len(asked) != 3 {
		t.Fatalf("asked %d times", len(asked))
	}
	if d := asked[0].Detail; !strings.Contains(d, "Runs as: the kustomize-controller's own service account.") ||
		!strings.Contains(d, "Your rights: not used") {
		t.Errorf("a Kustomization without a service account: %q", d)
	}
	if d := asked[1].Detail; !strings.Contains(d, "ServiceAccount → shop/shop-deployer (spec.serviceAccountName, through the helm-controller)") {
		t.Errorf("a HelmRelease with one: %q", d)
	}
	if d := asked[2].Detail; strings.Count(d, "kustomize-controller") != 1 || !strings.Contains(d, "shop/shop-deployer") {
		t.Errorf("a selection: %q", d)
	}
}
