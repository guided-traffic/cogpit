package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Reconcile with Source and Reset (D-33, owner 2026-10-14).

// The source kinds of the tests.
const (
	gitKey   = "gitrepositories.source.toolkit.fluxcd.io"
	chartKey = "helmcharts.source.toolkit.fluxcd.io"
)

var (
	gitGVR      = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	helmRepoGVR = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmrepositories"}
	chartGVR    = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmcharts"}
	ociGVR      = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "ocirepositories"}
)

// fluxSourcesSource is fluxSource with Flux's sources: Kustomizations from a Git repository (apps
// and flux-system share it), an OCI repository in another namespace (infra), an External
// Artifact (bundled), a missing repository (orphan) and a suspended one (held); Helm releases from
// a chart template of a Helm repository (podinfo), of an OCI Helm repository (redis, which no
// controller reconciles), from a chartRef to an OCI repository (frontend), and a suspended one
// (stopped).
func fluxSourcesSource() *testkit.MemorySource {
	src := fluxSource()
	for _, d := range []struct {
		gvr  schema.GroupVersionResource
		kind string
	}{{gitGVR, "GitRepository"}, {helmRepoGVR, "HelmRepository"}, {chartGVR, "HelmChart"}, {ociGVR, "OCIRepository"}} {
		src.Set(fluxCRDsGVR, testkit.CRD(d.gvr.Group, d.gvr.Resource, d.kind, true, testkit.CRDVersion{Name: d.gvr.Version, Served: true, Storage: true}))
	}
	withSpec := func(obj *unstructured.Unstructured, spec map[string]any) *unstructured.Unstructured {
		for k, v := range spec {
			obj.Object["spec"].(map[string]any)[k] = v
		}
		return obj
	}
	ks := func(name string, suspended bool, sourceRef map[string]any) {
		src.Set(ksGVR, withSpec(fluxObject("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", name, suspended, "True"), map[string]any{"sourceRef": sourceRef}))
	}
	git := map[string]any{"kind": "GitRepository", "name": "flux-system"}
	ks("apps", false, git)
	ks("flux-system", false, git)
	ks("paused", true, git)
	ks("infra", false, map[string]any{"kind": "OCIRepository", "name": "platform", "namespace": "platform"})
	ks("bundled", false, map[string]any{"kind": "ExternalArtifact", "name": "bundle"})
	ks("orphan", false, map[string]any{"kind": "GitRepository", "name": "missing"})
	ks("held", false, map[string]any{"kind": "GitRepository", "name": "held"})
	const srcAPI = "source.toolkit.fluxcd.io/v1"
	src.Set(gitGVR, fluxObject(srcAPI, "GitRepository", "flux-system", "flux-system", false, "True"))
	src.Set(gitGVR, fluxObject(srcAPI, "GitRepository", "flux-system", "held", true, "True"))
	src.Set(ociGVR, fluxObject(srcAPI, "OCIRepository", "platform", "platform", false, "True"))
	src.Set(ociGVR, fluxObject(srcAPI, "OCIRepository", "shop", "frontend-chart", false, "True"))
	src.Set(helmRepoGVR, fluxObject(srcAPI, "HelmRepository", "flux-system", "podinfo", false, "True"))
	src.Set(helmRepoGVR, withSpec(fluxObject(srcAPI, "HelmRepository", "flux-system", "bitnami", false, ""), map[string]any{"type": "oci"}))
	chart := func(name, repo string) {
		src.Set(chartGVR, withSpec(fluxObject(srcAPI, "HelmChart", "flux-system", name, false, "True"),
			map[string]any{"chart": name, "sourceRef": map[string]any{"kind": "HelmRepository", "name": repo}}))
	}
	chart("shop-podinfo", "podinfo")
	chart("shop-redis", "bitnami")
	hr := func(name string, suspended bool, spec map[string]any) {
		src.Set(hrGVR, withSpec(fluxObject("helm.toolkit.fluxcd.io/v2", "HelmRelease", "shop", name, suspended, "True"), spec))
	}
	template := func(repo string) map[string]any {
		return map[string]any{"chart": map[string]any{"spec": map[string]any{"chart": "c",
			"sourceRef": map[string]any{"kind": "HelmRepository", "name": repo, "namespace": "flux-system"}}}}
	}
	hr("podinfo", false, template("podinfo"))
	hr("redis", false, template("bitnami"))
	hr("stopped", true, template("podinfo"))
	hr("frontend", false, map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": "frontend-chart"}})
	return src
}

func hrNamed(name string) model.ObjectRef {
	return model.ObjectRef{Kind: hrKey, Namespace: "shop", Name: name}
}

// reconcileAt is the flux CLI's reconcile request at the writer's time, bound to uid.
func reconcileAt(uid string) string {
	return `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"1970-01-01T00:00:00Z"},"uid":"` + uid + `"}}`
}

// fluxCommandState says how a command shows for one object: enabled, hidden or greyed with why.
func fluxCommandState(t *testing.T, h *harness, id string, params any) string {
	t.Helper()
	cmd, ok := h.s.Registry().Get(id)
	if !ok {
		t.Fatalf("no command %s", id)
	}
	raw, _ := json.Marshal(params)
	switch err := cmd.Enabled(h.s, raw); {
	case err == nil:
		return "enabled"
	case errors.Is(err, engine.ErrNotApplicable):
		return "hidden"
	default:
		return "greyed: " + err.Error()
	}
}

// TestFluxReconcileWithSource: the source first, then the object, as flux reconcile
// --with-source does: a Kustomization's Git or OCI repository (in another namespace too), a Helm
// release's Helm repository and HelmChart (an OCI Helm repository skipped) or its chartRef; each
// patch bound to its object's UID, in a job that waits for each controller (A91). A suspended
// object or source, a missing source, an External Artifact as the source refuse it with nothing
// patched and no job; a failure in the middle shows in the job's steps.
func TestFluxReconcileWithSource(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	newFakeFluxController(h, w)
	gui := engine.OriginGUI
	var res commands.FluxSourceJobResult
	h.must(t, gui, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, &res)
	if res.Action != "reconcile with source started" || len(res.Targets) != 2 || res.Targets[0].Kind != gitKey || res.Targets[1].Name != "apps" {
		t.Errorf("result %+v", res)
	}
	if j := waitJob(t, h, res.Job.ID); j.State != engine.JobDone {
		t.Errorf("job %+v", j)
	}
	for _, ref := range []model.ObjectRef{ksRef("infra"), hrRef(), hrNamed("redis"), hrNamed("frontend")} {
		var r commands.FluxSourceJobResult
		h.must(t, gui, "flux.reconcileWithSource", map[string]any{"ref": ref}, &r)
		if j := waitJob(t, h, r.Job.ID); j.State != engine.JobDone {
			t.Errorf("%s: job %+v", ref.Name, j)
		}
	}
	want := []string{
		"gitrepositories/v1 flux-system/flux-system " + reconcileAt("uid-flux-system-flux-system"),
		"kustomizations/v1 flux-system/apps " + reconcileAt("uid-flux-system-apps"),
		"ocirepositories/v1 platform/platform " + reconcileAt("uid-platform-platform"),
		"kustomizations/v1 flux-system/infra " + reconcileAt("uid-flux-system-infra"),
		"helmrepositories/v1 flux-system/podinfo " + reconcileAt("uid-flux-system-podinfo"),
		"helmcharts/v1 flux-system/shop-podinfo " + reconcileAt("uid-flux-system-shop-podinfo"),
		"helmreleases/v2 shop/podinfo " + reconcileAt("uid-shop-podinfo"),
		"helmcharts/v1 flux-system/shop-redis " + reconcileAt("uid-flux-system-shop-redis"),
		"helmreleases/v2 shop/redis " + reconcileAt("uid-shop-redis"),
		"ocirepositories/v1 shop/frontend-chart " + reconcileAt("uid-shop-frontend-chart"),
		"helmreleases/v2 shop/frontend " + reconcileAt("uid-shop-frontend"),
	}
	if got := w.Patches(); !slices.Equal(got, want) {
		t.Errorf("patches\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Refused with nothing patched.
	for name, want := range map[string]string{"held": "flux-system/held is suspended", "orphan": "not found"} {
		err := h.run(t, gui, "flux.reconcileWithSource", map[string]any{"ref": ksRef(name)}, nil)
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "nothing was done") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, tc := range []struct {
		ref  model.ObjectRef
		want string
	}{
		{ksRef("bundled"), "greyed: the source of bundled is of kind ExternalArtifact, which Flux does not reconcile on request"},
		{ksRef("paused"), "greyed: paused is suspended: resume it to reconcile it"},
		{hrNamed("stopped"), "greyed: stopped is suspended: resume it to reconcile it"},
		{alRef(), "hidden"},
		{model.ObjectRef{Kind: gitKey, Namespace: "flux-system", Name: "flux-system"}, "hidden"},
	} {
		if got := fluxCommandState(t, h, "flux.reconcileWithSource", map[string]any{"ref": tc.ref}); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.ref.Name, got, tc.want)
		}
	}
	if n := len(w.Patches()); n != len(want) {
		t.Errorf("a refused reconcile patched: %v", w.Patches()[len(want):])
	}

	// The object's patch fails after its source's: the job's steps say which was done.
	w.mu.Lock()
	w.err, w.failOn = errors.New("the API server is away"), "kustomizations"
	w.mu.Unlock()
	var failing commands.FluxSourceJobResult
	h.must(t, gui, "flux.reconcileWithSource", map[string]any{"ref": ksRef("flux-system")}, &failing)
	j := waitJob(t, h, failing.Job.ID)
	if got := stepStates(j); j.State != engine.JobFailed || got[0] != "done GitRepository flux-system/flux-system: Ready: Applied revision: main@sha1:new" ||
		got[1] != "failed Kustomization flux-system/flux-system: Could not be asked to reconcile: the API server is away" {
		t.Errorf("a failure after the source: %q, %+v", got, j)
	}

	// The access reviews say the user may not patch the source: greyed with why.
	h.access.set(denyAccess{"patch gitrepositories flux-system": true})
	if got := fluxCommandState(t, h, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}); !strings.Contains(got, "may not patch gitrepositories.source.toolkit.fluxcd.io in flux-system, the source of apps") {
		t.Errorf("denied the source: %s", got)
	}
	if got := fluxCommandState(t, h, "flux.reconcileWithSource", map[string]any{"ref": ksRef("infra")}); got != "enabled" {
		t.Errorf("another source: %s", got)
	}
}

// TestFluxReset: Reset writes requestedAt and resetAt with one token, as flux reconcile
// helmrelease --reset does, on Helm releases only and not on a suspended one; on a selection it
// skips the suspended ones.
func TestFluxReset(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	gui := engine.OriginGUI
	h.must(t, gui, "flux.reset", map[string]any{"ref": hrRef()}, nil)
	reset := `helmreleases/v2 shop/podinfo {"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"1970-01-01T00:00:00Z","reconcile.fluxcd.io/resetAt":"1970-01-01T00:00:00Z"},"uid":"uid-shop-podinfo"}}`
	if got := w.Patches(); !slices.Equal(got, []string{reset}) {
		t.Errorf("patches %v", got)
	}
	for _, tc := range []struct {
		ref  model.ObjectRef
		want string
	}{
		{hrNamed("frontend"), "enabled"},
		{hrNamed("stopped"), "greyed: stopped is suspended: resume it to reset it"},
		{ksRef("apps"), "hidden"},
	} {
		if got := fluxCommandState(t, h, "flux.reset", map[string]any{"ref": tc.ref}); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.ref.Name, got, tc.want)
		}
	}
	var res commands.FluxManyResult
	h.must(t, gui, "flux.resetMany", map[string]any{"refs": []model.ObjectRef{hrRef(), hrNamed("frontend"), hrNamed("stopped")}}, &res)
	if res.Action != "reset requested" || len(res.Done) != 2 || res.Done[1].Name != "frontend" || len(w.Patches()) != 3 {
		t.Errorf("reset of a selection %+v, patches %v", res, w.Patches())
	}
	if p := w.Patches()[2]; !strings.Contains(p, "shop/frontend") || !strings.Contains(p, "resetAt") {
		t.Errorf("patch %s", p)
	}
}

// TestFluxActionsWithSourceInTheMenus: the row menus of Kustomizations and Helm releases in their
// order (Reconcile, Reconcile with Source, Reset for Helm releases, Suspend and Resume, Edit,
// Delete); only Reconcile takes force.
func TestFluxActionsWithSourceInTheMenus(t *testing.T) {
	reg := engine.NewRegistry()
	if err := commands.RegisterAll(reg); err != nil {
		t.Fatal(err)
	}
	menu := func(kind string) []string {
		var ids []string
		for _, c := range reg.Actions(kind) {
			if !c.DrawerOnly {
				ids = append(ids, c.ID)
			}
		}
		return ids
	}
	if got, want := menu(ksKey), []string{"flux.reconcile", "flux.reconcileWithSource", "flux.resume", "flux.suspend", "flux.logs", "object.edit", engine.RemoveFinalizersCommand, "object.delete"}; !slices.Equal(got, want) {
		t.Errorf("Kustomizations %v, want %v", got, want)
	}
	if got, want := menu(hrKey), []string{"flux.reconcile", "flux.reconcileWithSource", "flux.reset", "flux.resume", "flux.suspend", "object.edit", engine.RemoveFinalizersCommand, "object.delete"}; !slices.Equal(got, want) {
		t.Errorf("Helm releases %v, want %v", got, want)
	}
	if got := menu(gitKey); slices.Contains(got, "flux.reconcileWithSource") || slices.Contains(got, "flux.reset") {
		t.Errorf("Git repositories %v", got)
	}
	for _, id := range []string{"flux.reconcileWithSource", "flux.reconcileWithSourceMany", "flux.reset", "flux.resetMany"} {
		if cmd, _ := reg.Get(id); cmd.Schema.Properties["force"] != nil || cmd.Danger != engine.DangerMutating {
			t.Errorf("%s: force %v, danger %s", id, cmd.Schema.Properties["force"] != nil, cmd.Danger)
		}
	}
}

// TestFluxReconcileWithSourceAgents (D-2): an agent's Reconcile with Source asks the user, naming
// the source and the object with their UIDs; a source replaced while the user was asked is not
// patched, nor the object after it.
func TestFluxReconcileWithSourceAgents(t *testing.T) {
	var asked []engine.ConfirmRequest
	var during func()
	h, w := newFluxHarnessOn(t, engine.Options{Audit: &syncBuffer{}, Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		if during != nil {
			during()
		}
		return true, nil
	})}, fluxSourcesSource())
	newFakeFluxController(h, w)
	agent := engine.OriginExternalAgent
	var first commands.FluxSourceJobResult
	h.must(t, agent, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, &first)
	waitJob(t, h, first.Job.ID)
	req := asked[0]
	if req.Danger != engine.DangerMutating || len(req.Objects) != 2 || req.Objects[0].Kind != gitKey || req.Objects[1].Name != "apps" ||
		!slices.Equal(req.UIDs, []string{"uid-flux-system-flux-system", "uid-flux-system-apps"}) || !slices.Equal(req.Namespaces, []string{"flux-system"}) {
		t.Errorf("confirmation %+v", req)
	}
	if n := len(w.Patches()); n != 2 {
		t.Errorf("patches %v", w.Patches())
	}

	// A selection: the shared source named once.
	var many commands.FluxSourceJobResult
	h.must(t, agent, "flux.reconcileWithSourceMany", map[string]any{"refs": []model.ObjectRef{ksRef("apps"), ksRef("flux-system"), hrRef()}}, &many)
	waitJob(t, h, many.Job.ID)
	var names []string
	for _, o := range asked[1].Objects {
		names = append(names, o.Name)
	}
	if !slices.Equal(names, []string{"flux-system", "apps", "flux-system", "podinfo", "shop-podinfo", "podinfo"}) || asked[1].Objects[2].Kind != ksKey {
		t.Errorf("selection's confirmation %v", asked[1].Objects)
	}

	// The source replaced while the user was asked.
	before := len(w.Patches())
	during = func() {
		obj := fluxObject("source.toolkit.fluxcd.io/v1", "GitRepository", "flux-system", "flux-system", false, "True")
		obj.SetUID("uid-new")
		h.src.Set(gitGVR, obj)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if !testkit.WaitFor(ctx, func() bool {
			o, err := h.s.Lookup(model.ObjectRef{Kind: gitKey, Namespace: "flux-system", Name: "flux-system"})
			return err == nil && o.GetUID() == "uid-new"
		}) {
			t.Error("the cache never showed the replaced source")
		}
	}
	if err := h.run(t, agent, "flux.reconcileWithSource", map[string]any{"ref": ksRef("apps")}, nil); !errors.Is(err, engine.ErrTargetsChanged) {
		t.Errorf("a source replaced while asked: %v", err)
	}
	if n := len(w.Patches()); n != before {
		t.Errorf("patched after a replaced source: %v", w.Patches()[before:])
	}
}

// TestFluxReconcileWithSourceOnASelection: each object's chain, a source two objects share
// patched once; an object whose source is suspended or missing is skipped with why, the others go
// on; the suspended object and the one from an External Artifact are left out. A selection none of
// whose objects can be reconciled starts no job.
func TestFluxReconcileWithSourceOnASelection(t *testing.T) {
	h, w := newFluxHarnessOn(t, engine.Options{}, fluxSourcesSource())
	newFakeFluxController(h, w)
	refs := []model.ObjectRef{ksRef("apps"), ksRef("flux-system"), ksRef("held"), ksRef("orphan"), ksRef("bundled"), ksRef("paused"), ksRef("infra")}
	var ids []string
	for _, c := range h.s.Registry().SelectionActions(engine.SelectionKinds(append(refs, hrRef()))) {
		ids = append(ids, c.ID)
	}
	if !slices.Contains(ids, "flux.reconcileWithSourceMany") || slices.Contains(ids, "flux.resetMany") {
		t.Errorf("selection actions %v", ids)
	}
	var res commands.FluxSourceJobResult
	h.must(t, engine.OriginGUI, "flux.reconcileWithSourceMany", map[string]any{"refs": refs}, &res)
	var targets, skipped []string
	for _, r := range res.Targets {
		targets = append(targets, r.Name)
	}
	for _, f := range res.Skipped {
		skipped = append(skipped, f.Ref.Name+": "+f.Error)
	}
	if !slices.Equal(targets, []string{"flux-system", "apps", "flux-system", "platform", "infra"}) {
		t.Errorf("targets %v", targets)
	}
	if len(skipped) != 2 || !strings.Contains(skipped[0], "held: its source") || !strings.Contains(skipped[0], "is suspended") ||
		!strings.Contains(skipped[1], "orphan: its source") || !strings.Contains(skipped[1], "not found") {
		t.Errorf("skipped %v", skipped)
	}
	if j := waitJob(t, h, res.Job.ID); j.State != engine.JobFailed || j.Outcome != "5 of 7 objects reconciled and ready; the steps say why the others are not." {
		t.Errorf("job %+v", j)
	}
	git := 0
	for _, p := range w.Patches() {
		if strings.HasPrefix(p, "gitrepositories/") {
			git++
		}
	}
	if git != 1 || len(w.Patches()) != 5 {
		t.Errorf("patches %v", w.Patches())
	}

	// The shared source fails: both objects that need it fail, the other goes on.
	w.mu.Lock()
	w.err, w.failOn = errors.New("the API server is away"), "gitrepositories"
	w.mu.Unlock()
	h.must(t, engine.OriginGUI, "flux.reconcileWithSourceMany", map[string]any{"refs": []model.ObjectRef{ksRef("apps"), ksRef("flux-system"), ksRef("infra")}}, &res)
	j := waitJob(t, h, res.Job.ID)
	got := stepStates(j)
	want := []string{
		"failed GitRepository flux-system/flux-system: Could not be asked to reconcile: the API server is away",
		"failed Kustomization flux-system/apps: Not asked to reconcile: its source GitRepository flux-system/flux-system could not be asked to reconcile: the API server is away",
		"failed Kustomization flux-system/flux-system: Not asked to reconcile: its source GitRepository flux-system/flux-system could not be asked to reconcile: the API server is away",
		"done OCIRepository platform/platform: Ready: Applied revision: main@sha1:new",
		"done Kustomization flux-system/infra: Ready: Applied revision: main@sha1:new",
	}
	if !slices.Equal(got, want) {
		t.Errorf("a failing shared source:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Nothing to reconcile: an error, no job.
	before := len(h.s.UIState().Dock)
	err := h.run(t, engine.OriginGUI, "flux.reconcileWithSourceMany", map[string]any{"refs": []model.ObjectRef{ksRef("held"), ksRef("orphan")}}, nil)
	var none *commands.FluxManyError
	if !errors.As(err, &none) || len(none.Failed) != 2 || len(h.s.UIState().Dock) != before {
		t.Errorf("nothing to reconcile: %v, %d tabs (was %d)", err, len(h.s.UIState().Dock), before)
	}
}
