//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// finalizerNamespace is the namespace the Blocking Deletion test owns on the shared kind cluster.
const finalizerNamespace = "cogpit-fin"

// kindFinalizer is the dummy finalizer no controller ever removes.
const kindFinalizer = "example.com/x"

var kindSecrets = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// TestKindTerminatingNamespaceBlockers walks WF43 (D-98) on kind: a namespace with a ConfigMap
// and a Secret carrying a finalizer nobody removes is deleted and stays Terminating; its drawer
// lists both objects, with their finalizers and Remove Finalizers, from the namespace controller's
// conditions and the cache, and so does an agent's object_get; the API server refuses a removal
// that names other finalizers (the patch's test, 422); Remove Finalizers through the engine, each
// confirmed with the finalizers as a row, makes each object vanish from the list, and the
// namespace goes by itself without a second Delete.
func TestKindTerminatingNamespaceBlockers(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var asked []engine.ConfirmRequest
	s.SetConfirmer(engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		mu.Lock()
		asked = append(asked, req)
		mu.Unlock()
		return true, nil
	}))
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	conn := s.Backend().Conn
	dyn := conn.Dynamic()

	if !testkit.WaitFor(ctx, func() bool {
		_, err := dyn.Resource(kindNamespaces).Get(ctx, finalizerNamespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}) {
		t.Fatalf("namespace %s still exists from an earlier run", finalizerNamespace)
	}
	// Whatever happens, the dummy finalizers go and the namespace with them.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		for _, gvr := range []schema.GroupVersionResource{kindConfigMaps, kindSecrets} {
			list, err := dyn.Resource(gvr).Namespace(finalizerNamespace).List(cctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for _, o := range list.Items {
				_, _ = dyn.Resource(gvr).Namespace(finalizerNamespace).Patch(cctx, o.GetName(), "application/merge-patch+json",
					[]byte(`{"metadata":{"finalizers":null}}`), metav1.PatchOptions{})
			}
		}
		_ = conn.DeleteObject(cctx, kindNamespaces, "", finalizerNamespace, "")
	})

	ns := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": finalizerNamespace}}}
	if _, err := dyn.Resource(kindNamespaces).Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	held := func(kind, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": kind,
			"metadata": map[string]any{"name": name, "namespace": finalizerNamespace, "finalizers": []any{kindFinalizer}},
			"data":     map[string]any{"k": "dg=="},
		}}
	}
	if _, err := dyn.Resource(kindConfigMaps).Namespace(finalizerNamespace).Create(ctx, held("ConfigMap", "held-cm"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := dyn.Resource(kindSecrets).Namespace(finalizerNamespace).Create(ctx, held("Secret", "held-secret"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := conn.DeleteObject(ctx, kindNamespaces, "", finalizerNamespace, ""); err != nil {
		t.Fatal(err)
	}

	// The drawer, as the window opens it: the list once the controller has said what remains.
	nsRef := model.ObjectRef{Context: contextName, Kind: "namespaces", Name: finalizerNamespace}
	s.UpdateUI(func(u *engine.UIState) { u.View, u.Drawer = "namespaces", &nsRef })
	blocking := func(m *engine.DetailModel) *model.DetailSection {
		for i, sec := range m.Sections {
			if sec.Title == engine.BlockingTitle {
				return &m.Sections[i]
			}
		}
		return nil
	}
	rows := func(sec *model.DetailSection) map[string]model.DetailItem {
		out := map[string]model.DetailItem{}
		if sec == nil {
			return out
		}
		for _, it := range sec.Items {
			if len(it.Actions) == 1 && it.Actions[0].Command == engine.RemoveFinalizersCommand {
				out[it.Name+" "+it.Value] = it
			}
		}
		return out
	}
	var listed map[string]model.DetailItem
	if !testkit.WaitFor(ctx, func() bool {
		m, err := s.Detail(nsRef)
		if err != nil {
			return false
		}
		listed = rows(blocking(m))
		return len(listed) == 2
	}) {
		m, _ := s.Detail(nsRef)
		t.Fatalf("the drawer never listed both objects: %+v", blocking(m))
	}
	for _, key := range []string{"Config Map held-cm", "Secret held-secret"} {
		it, ok := listed[key]
		if !ok || it.Link == nil || it.Link.Namespace != finalizerNamespace {
			t.Fatalf("row %q: %+v (rows %v)", key, it, listed)
		}
	}
	// An agent's object_get reads the same rows.
	m, err := s.DetailWait(ctx, model.ObjectRef{Kind: "namespaces", Name: finalizerNamespace})
	if err != nil || len(rows(blocking(m))) != 2 {
		t.Fatalf("object_get: %v %+v", err, m)
	}

	// The API server refuses a removal of finalizers the object does not hold exactly.
	cm, err := dyn.Resource(kindConfigMaps).Namespace(finalizerNamespace).Get(ctx, "held-cm", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.RemoveFinalizers(ctx, kindConfigMaps, finalizerNamespace, "held-cm", string(cm.GetUID()), []string{"example.com/other"}); !errors.Is(err, cluster.ErrFinalizersChanged) {
		t.Fatalf("a removal of other finalizers: %v", err)
	}
	if err := conn.RemoveFinalizers(ctx, kindConfigMaps, finalizerNamespace, "held-cm", "not-its-uid", []string{kindFinalizer}); !errors.Is(err, cluster.ErrFinalizersChanged) {
		t.Fatalf("a removal from another object: %v", err)
	}

	// Remove Finalizers from each row, through the engine with the row's params, confirmed.
	for _, key := range []string{"Config Map held-cm", "Secret held-secret"} {
		it := listed[key]
		res, err := s.Dispatch(ctx, engine.Invocation{ID: engine.RemoveFinalizersCommand, Params: it.Actions[0].Params, Origin: engine.OriginGUI})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		var r engine.RemoveFinalizersResult
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, &r); err != nil || !slices.Equal(r.Removed, []string{kindFinalizer}) {
			t.Fatalf("%s: %s", key, b)
		}
		name := strings.TrimPrefix(strings.TrimPrefix(key, "Config Map "), "Secret ")
		if !testkit.WaitFor(ctx, func() bool {
			m, err := s.Detail(nsRef)
			if err != nil {
				return false
			}
			for k := range rows(blocking(m)) {
				if strings.HasSuffix(k, " "+name) {
					return false
				}
			}
			return true
		}) {
			t.Fatalf("%s did not vanish from the list", key)
		}
	}
	mu.Lock()
	if len(asked) != 2 || asked[0].Danger != engine.DangerDestructive || len(asked[0].Effects) == 0 ||
		asked[0].Effects[0].Name != engine.EffectRemovesFinalizers || asked[0].Effects[0].Value != kindFinalizer {
		t.Errorf("confirmations %+v", asked)
	}
	mu.Unlock()

	// The namespace goes by itself: no second Delete.
	start := time.Now()
	if !testkit.WaitFor(ctx, func() bool {
		_, err := dyn.Resource(kindNamespaces).Get(ctx, finalizerNamespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}) {
		t.Fatal("the namespace did not go after its last finalizer")
	}
	t.Logf("the namespace went %s after the last finalizer", time.Since(start).Round(100*time.Millisecond))
}
