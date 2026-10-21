//go:build kind

package integration_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fluxNamespace is TestKindFlux's namespace; fluxKsKey the Kustomizations' kind key.
const (
	fluxNamespace = "cogpit-flux-actions"
	fluxKsKey     = "kustomizations.kustomize.toolkit.fluxcd.io"
)

var fluxKsGVR = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}

// TestKindFlux installs minimal Flux definitions (testdata/flux/crds.yaml: Kustomization,
// GitRepository, HelmRelease at the versions Flux serves, with the status subresource; no
// controllers), creates a Kustomization and writes the status a controller would through the
// status subresource, and checks the Flux section over the engine: the curated view's row, the
// dashboard's counts, and Reconcile, Suspend and Resume patching the object on the API server, a
// patch bound to another UID refused; Reconcile with Source annotating the GitRepository and the
// Kustomization, Reset a Helm release. A cluster that already has Flux is left alone (the test
// skips); the definitions are removed at the end.
func TestKindFlux(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	installFluxCRDs(ctx, t, conn)
	freshNamespace(ctx, t, conn.Kube(), fluxNamespace)

	// A Kustomization, and the status kustomize-controller would write.
	kss := conn.Dynamic().Resource(fluxKsGVR).Namespace(fluxNamespace)
	ks := testkit.Object("kustomize.toolkit.fluxcd.io/v1", "Kustomization", fluxNamespace, "apps", map[string]any{
		"spec": map[string]any{"interval": "10m", "path": "./apps", "prune": true,
			"sourceRef": map[string]any{"kind": "GitRepository", "name": "flux-system"}},
	})
	ks.SetUID("")
	ks.SetCreationTimestamp(metav1.Time{})
	created, err := kss.Create(ctx, ks, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating the Kustomization: %v", err)
	}
	created.Object["status"] = map[string]any{
		"observedGeneration":  created.GetGeneration(),
		"lastAppliedRevision": "main@sha1:6f2c1b9e",
		"conditions": []any{map[string]any{"type": "Ready", "status": "True", "reason": "ReconciliationSucceeded",
			"message": "Applied revision: main@sha1:6f2c1b9e", "lastTransitionTime": "2026-10-13T20:00:00Z"}},
	}
	if _, err := kss.UpdateStatus(ctx, created, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("writing the status: %v", err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	waitFor(ctx, t, "the Kustomizations kind", func() bool { _, ok := s.Backend().Profile(fluxKsKey); return ok })
	if p, _ := s.Backend().Profile(fluxKsKey); p.Title != "Kustomizations" || p.ColumnIndex("source") != 2 {
		t.Fatalf("profile %q with columns %+v; want the curated one", p.Title, p.Columns)
	}

	d := dispatcher{t: t, ctx: ctx, s: s}
	d.must("ui.openView", map[string]any{"view": fluxKsKey}, nil)
	ref := model.ObjectRef{Kind: fluxKsKey, Namespace: fluxNamespace, Name: "apps"}
	row := func(want string) {
		t.Helper()
		waitFor(ctx, t, "the row to read "+want, func() bool {
			tbl := s.Table(fluxKsKey)
			if tbl == nil || !tbl.Synced {
				return false
			}
			i := tbl.IndexOf(ref)
			if i < 0 {
				return false
			}
			cells := map[string]model.Cell{}
			for j, c := range tbl.Columns {
				cells[c.ID] = tbl.Rows[i].Cells[j]
			}
			return cells["status"].Text+"|"+cells["source"].Text+"|"+cells["revision"].Text+"|"+cells["suspended"].Text == want
		})
	}
	row("Ready|GitRepository/flux-system|main@sha1:6f2c1b9e|false")

	var dash commands.FluxDashboardResult
	d.must("flux.dashboard", map[string]any{"namespaces": []string{fluxNamespace}, "table": true}, &dash)
	if len(dash.Kinds) < 3 || dash.Kinds[1].Kind != fluxKsKey || dash.Kinds[1].Count != 1 || len(dash.Kinds[1].States) != 1 ||
		dash.Kinds[1].States[0].State != "ready" || dash.NotReady == nil || len(dash.NotReady.Rows) != 0 {
		t.Errorf("dashboard %+v", dash)
	}

	annotation := func() string {
		obj, err := kss.Get(ctx, "apps", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return obj.GetAnnotations()[cluster.FluxReconcileAnnotation]
	}
	suspended := func() bool {
		obj, err := kss.Get(ctx, "apps", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		v, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
		return v
	}

	d.must("flux.reconcile", map[string]any{"ref": ref}, nil)
	first := annotation()
	if _, err := time.Parse(time.RFC3339Nano, first); err != nil {
		t.Fatalf("reconcile.fluxcd.io/requestedAt %q: %v", first, err)
	}
	d.must("flux.suspend", map[string]any{"ref": ref}, nil)
	if !suspended() {
		t.Fatal("spec.suspend is not true after Suspend")
	}
	row("Suspended|GitRepository/flux-system|main@sha1:6f2c1b9e|true")
	if err := d.run("flux.reconcile", map[string]any{"ref": ref}, nil); err == nil || !strings.Contains(err.Error(), "is suspended") {
		t.Errorf("Reconcile of a suspended object: %v", err)
	}
	d.must("flux.resume", map[string]any{"ref": ref}, nil)
	if suspended() || annotation() == first {
		t.Errorf("after Resume: suspended %v, requestedAt %q (was %q)", suspended(), annotation(), first)
	}
	// spec.suspend is part of the spec: the generation moved past the one the status says the
	// controller observed, so the object reads Reconciling until a controller catches up (none runs).
	row("Reconciling|GitRepository/flux-system|main@sha1:6f2c1b9e|false")

	// A patch bound to another UID: the API server refuses it, as it does for built-in kinds.
	yes := true
	err = conn.PatchFlux(ctx, fluxKsGVR, fluxNamespace, "apps", "00000000-0000-0000-0000-000000000000", cluster.FluxRequest{Suspend: &yes})
	if !errors.Is(err, cluster.ErrReplaced) {
		t.Errorf("a patch bound to another UID: %v", err)
	}
	t.Logf("the API server's answer to a patch bound to another UID: %v", err)
	if suspended() {
		t.Error("the refused patch suspended the object")
	}

	kindFluxWithSourceAndReset(ctx, t, conn, s, d, ref, annotation)
}

// kindFluxWithSourceAndReset (D-33, owner 2026-10-14): Reconcile with Source annotates the
// Kustomization's GitRepository and then the Kustomization on the API server; Reset writes resetAt
// and requestedAt with one token on a Helm release.
func kindFluxWithSourceAndReset(ctx context.Context, t *testing.T, conn *cluster.Conn, s *engine.Session, d dispatcher, ks model.ObjectRef, annotation func() string) {
	t.Helper()
	gitGVR := schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	hrGVR := schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	create := func(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
		obj.SetUID("")
		obj.SetCreationTimestamp(metav1.Time{})
		if _, err := conn.Dynamic().Resource(gvr).Namespace(fluxNamespace).Create(ctx, obj, metav1.CreateOptions{}); err != nil {
			t.Fatalf("creating %s: %v", obj.GetName(), err)
		}
	}
	get := func(gvr schema.GroupVersionResource, name string) map[string]string {
		obj, err := conn.Dynamic().Resource(gvr).Namespace(fluxNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return obj.GetAnnotations()
	}
	create(gitGVR, testkit.Object("source.toolkit.fluxcd.io/v1", "GitRepository", fluxNamespace, "flux-system", map[string]any{
		"spec": map[string]any{"url": "https://example.invalid/fleet", "interval": "1m"}}))
	// The command reads the source from the cache, which the watch fills a moment after the create
	// (CI, 2026-10-21: "not found in the cache").
	gitRef := model.ObjectRef{Kind: "gitrepositories.source.toolkit.fluxcd.io", Namespace: fluxNamespace, Name: "flux-system"}
	waitFor(ctx, t, "the GitRepository in the cache", func() bool { _, err := s.Lookup(gitRef); return err == nil })
	before := annotation()
	var res commands.FluxSourceJobResult
	d.must("flux.reconcileWithSource", map[string]any{"ref": ks}, &res)
	if len(res.Targets) != 2 || res.Targets[0].Name != "flux-system" || res.Targets[1].Name != ks.Name {
		t.Errorf("reconcile with source targets %+v", res.Targets)
	}
	// A91: the job asks the GitRepository and waits for source-controller before it asks the
	// Kustomization. No controller runs here: the test plays each, through the status subresource,
	// with the token the request carried.
	handle := func(gvr schema.GroupVersionResource, name, token string) {
		obj, err := conn.Dynamic().Resource(gvr).Namespace(fluxNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		obj.Object["status"] = map[string]any{
			"lastHandledReconcileAt": token, "observedGeneration": obj.GetGeneration(),
			"conditions": []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded",
				"message": "stored artifact for revision 'main@sha1:7a1e'", "lastTransitionTime": time.Now().UTC().Format(time.RFC3339)}},
		}
		if _, err := conn.Dynamic().Resource(gvr).Namespace(fluxNamespace).UpdateStatus(ctx, obj, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("the status of %s: %v", name, err)
		}
	}
	var git string
	waitFor(ctx, t, "the GitRepository asked", func() bool { git = get(gitGVR, "flux-system")[cluster.FluxReconcileAnnotation]; return git != "" })
	time.Sleep(time.Second) // the job must not ask the Kustomization meanwhile
	if annotation() != before {
		t.Errorf("the Kustomization was asked before its source was ready")
	}
	handle(gitGVR, "flux-system", git)
	waitFor(ctx, t, "the Kustomization asked", func() bool { return annotation() != before })
	handle(fluxKsGVR, ks.Name, annotation())
	var job *engine.JobTab
	waitFor(ctx, t, "the job's end", func() bool {
		tab, ok := s.UIState().DockTab(res.Job.ID)
		job = tab.Job
		return ok && job != nil && job.State.Ended()
	})
	if job.State != engine.JobDone || job.Steps[0].Detail != "Ready: stored artifact for revision 'main@sha1:7a1e'" {
		t.Errorf("the job %+v", job)
	}

	create(hrGVR, testkit.Object("helm.toolkit.fluxcd.io/v2", "HelmRelease", fluxNamespace, "podinfo", map[string]any{
		"spec": map[string]any{"interval": "10m", "chart": map[string]any{"spec": map[string]any{
			"chart": "podinfo", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "podinfo"}}}}}))
	hr := model.ObjectRef{Kind: "helmreleases.helm.toolkit.fluxcd.io", Namespace: fluxNamespace, Name: "podinfo"}
	waitFor(ctx, t, "the Helm release in the cache", func() bool { _, err := s.Lookup(hr); return err == nil })
	d.must("flux.reset", map[string]any{"ref": hr}, nil)
	ann := get(hrGVR, "podinfo")
	if ann[cluster.FluxResetAnnotation] == "" || ann[cluster.FluxResetAnnotation] != ann[cluster.FluxReconcileAnnotation] {
		t.Errorf("after Reset: %v", ann)
	}
}

// installFluxCRDs creates the definitions of testdata/flux/crds.yaml and waits until they are
// established; the test's cleanup deletes them. A cluster that has one of them already skips the
// test.
func installFluxCRDs(ctx context.Context, t *testing.T, conn *cluster.Conn) {
	t.Helper()
	f, err := os.Open("testdata/flux/crds.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var defs []*unstructured.Unstructured
	dec := utilyaml.NewYAMLOrJSONDecoder(f, 4096)
	for {
		var m map[string]any
		if err := dec.Decode(&m); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if m != nil {
			defs = append(defs, &unstructured.Unstructured{Object: m})
		}
	}
	crds := conn.Dynamic().Resource(testkit.CRDsGVR)
	for _, def := range defs {
		if _, err := crds.Get(ctx, def.GetName(), metav1.GetOptions{}); err == nil {
			t.Skipf("the cluster has %s already (Flux installed?); the test installs its own definitions only", def.GetName())
		} else if !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dcancel()
		for _, def := range defs {
			if err := crds.Delete(dctx, def.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("deleting %s: %v", def.GetName(), err)
			}
		}
	})
	for _, def := range defs {
		if _, err := crds.Create(ctx, def, metav1.CreateOptions{}); err != nil {
			t.Fatalf("creating %s: %v", def.GetName(), err)
		}
	}
	for _, def := range defs {
		name := def.GetName()
		waitFor(ctx, t, name+" to be established", func() bool {
			got, err := crds.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			for _, c := range model.NestedSlice(got, "status", "conditions") {
				if model.NestedString(c, "type") == "Established" && model.NestedString(c, "status") == "True" {
					return true
				}
			}
			return false
		})
	}
}
