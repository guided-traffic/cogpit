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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// selectionNamespace is the namespace of TestKindDeleteSelection: it creates it, acts only inside
// it, and deletes it.
const selectionNamespace = "cogpit-w-selection"

// TestKindDeleteSelection selects three ConfigMaps of its own namespace through the engine as the
// header's checkbox does (ui.select with all, after the namespace filter and a search that leaves
// out the namespace's kube-root-ca.crt) and deletes them with
// object.deleteMany: one confirmation names all three with their namespace, the context and the
// UIDs the API server gave them, and each is gone afterwards. Then two more, one of them deleted
// and created again under its name while the confirmation is open: that one is refused by its UID
// precondition on the API server and reported, the other is deleted, nothing is retried.
func TestKindDeleteSelection(t *testing.T) {
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
	kube, ns := conn.Kube(), selectionNamespace
	freshNamespace(ctx, t, kube, ns)
	create := func(name string) string {
		t.Helper()
		cm, err := kube.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Data: map[string]string{"a": "1"},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return string(cm.UID)
	}
	uids := map[string]string{}
	for _, name := range []string{"sel-one", "sel-two", "sel-three"} {
		uids[name] = create(name)
	}

	var mu sync.Mutex
	var asked []engine.ConfirmRequest
	var during func()
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			mu.Lock()
			asked = append(asked, req)
			d := during
			mu.Unlock()
			if d != nil {
				d()
			}
			return true, nil
		}),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	run := func(id string, params any) (any, error) {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		return s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI})
	}
	must := func(id string, params any) any {
		t.Helper()
		res, err := run(id, params)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}
	// selectAll selects every ConfigMap the view shows once it shows n of them, as the header's
	// checkbox does.
	// selectAll waits until the view shows exactly the named ConfigMaps (a count alone matched a
	// snapshot that still held a deleted one beside a created one: CI), then checks all.
	selectAll := func(names ...string) []model.ObjectRef {
		t.Helper()
		n := len(names)
		wctx, wcancel := context.WithTimeout(ctx, time.Minute)
		defer wcancel()
		var last *engine.TableSnapshot
		if !testkit.WaitFor(wctx, func() bool {
			last = s.Table("configmaps")
			if last == nil || !last.Synced || len(last.Rows) != n {
				return false
			}
			for _, r := range last.Rows {
				if !slices.Contains(names, r.Ref.Name) {
					return false
				}
			}
			return true
		}) {
			u := s.UIState()
			t.Fatalf("the view never showed %v: view %q namespaces %v, last snapshot %+v", names, u.View, u.Namespaces, last)
		}
		must("ui.select", map[string]any{"all": true, "noDrawer": true})
		sel := s.UIState().Selected
		if len(sel) != n {
			t.Fatalf("selected %v, want %d", sel, n)
		}
		return sel
	}

	must("ui.openView", map[string]any{"view": "configmaps"})
	must("ui.setNamespace", map[string]any{"namespaces": []string{ns}})
	must("ui.setFilter", map[string]any{"filter": "sel-"}) // not the namespace's kube-root-ca.crt
	sel := selectAll("sel-one", "sel-two", "sel-three")
	res := must("object.deleteMany", map[string]any{"refs": sel}).(commands.DeleteManyResult)
	if len(res.Deleted) != 3 {
		t.Fatalf("deleted %v", res.Deleted)
	}
	mu.Lock()
	req := asked[0]
	n := len(asked)
	mu.Unlock()
	if n != 1 || len(req.Objects) != 3 || !slices.Equal(req.Namespaces, []string{ns}) || req.Context != s.UIState().Context || req.Danger != engine.DangerDestructive {
		t.Fatalf("%d confirmations, the first %+v", n, req)
	}
	for i, o := range req.Objects {
		if req.UIDs[i] != uids[o.Name] {
			t.Errorf("%s confirmed with UID %q, the server's is %q", o.Name, req.UIDs[i], uids[o.Name])
		}
	}
	if !strings.HasPrefix(req.Message(), "Delete 3 objects: Config Map ") {
		t.Errorf("message %q", req.Message())
	}
	for _, name := range []string{"sel-one", "sel-two", "sel-three"} {
		if _, err := kube.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Errorf("configmap %s after the delete: %v", name, err)
		}
	}
	if len(s.UIState().Selected) != 0 {
		t.Errorf("the deleted objects are still selected: %v", s.UIState().Selected)
	}

	// A ConfigMap created again under its name while the dialog is open keeps its life.
	create("sel-four")
	create("sel-five")
	sel = selectAll("sel-four", "sel-five")
	var again string
	mu.Lock()
	during = func() {
		if err := kube.CoreV1().ConfigMaps(ns).Delete(ctx, "sel-five", metav1.DeleteOptions{}); err != nil {
			t.Errorf("delete five: %v", err)
		}
		again = create("sel-five")
	}
	mu.Unlock()
	_, err = run("object.deleteMany", map[string]any{"refs": sel})
	var de *commands.DeleteManyError
	if !errors.As(err, &de) || len(de.Deleted) != 1 || de.Deleted[0].Name != "sel-four" || len(de.Failed) != 1 || de.Failed[0].Ref.Name != "sel-five" {
		t.Fatalf("a replaced object in the selection: %v", err)
	}
	cm, err := kube.CoreV1().ConfigMaps(ns).Get(ctx, "sel-five", metav1.GetOptions{})
	if err != nil || string(cm.UID) != again {
		t.Errorf("the replacement of five: %v (uid %v, want %s)", err, cm.GetUID(), again)
	}
	if _, err := kube.CoreV1().ConfigMaps(ns).Get(ctx, "sel-four", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("configmap four after the delete: %v", err)
	}
}
