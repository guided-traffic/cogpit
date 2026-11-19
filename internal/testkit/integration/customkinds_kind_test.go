//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// crdNamespace is the namespace the custom resource kind test works in; the CRD is its own test
// group's (widgets.e2e.cogpit.test), created and deleted by the test.
const crdNamespace = "cogpit-crd"

// TestKindCustomResources applies a test CRD and a custom resource on the kind cluster and follows
// them through the engine: the kind appears once the CRD is established, its view shows the
// object with its printer columns, and deleting the CRD takes the kind away while the open view
// says so. It writes only its CRD, its namespace and its object.
func TestKindCustomResources(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	crds := conn.Dynamic().Resource(testkit.CRDsGVR)
	deleteCRD := func() {
		err := crds.Delete(context.Background(), widgetsKey, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting the test CRD: %v", err)
		}
	}
	// A run that died halfway may have left the CRD: start from none.
	deleteCRD()
	if !testkit.WaitFor(ctx, func() bool {
		_, err := crds.Get(ctx, widgetsKey, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}) {
		t.Fatal("an old test CRD is still being deleted")
	}
	defer deleteCRD()
	if _, err := conn.Kube().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: crdNamespace}}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("namespace: %v", err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params any) any {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}

	crd := testkit.CRD("e2e.cogpit.test", "widgets", "Widget", true, widgetCRD()...)
	crd.SetUID("")
	crd.SetCreationTimestamp(metav1.Time{})
	crd.Object["status"] = nil
	if _, err := crds.Create(ctx, crd, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating the test CRD: %v", err)
	}
	if !testkit.WaitFor(ctx, func() bool { _, ok := s.Backend().Profile(widgetsKey); return ok }) {
		t.Fatal("the Widget kind never appeared")
	}
	w := testkit.Object("e2e.cogpit.test/v1", "Widget", crdNamespace, "sprocket", widgetFields("teal", 7))
	w.SetUID("")
	w.SetCreationTimestamp(metav1.Time{})
	if _, err := conn.Dynamic().Resource(widgetsGVR).Namespace(crdNamespace).Create(ctx, w, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating the widget: %v", err)
	}

	dispatch("ui.openView", map[string]any{"view": widgetsKey})
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table(widgetsKey); return tbl != nil && tbl.Synced && tbl.Total == 1 }) {
		t.Fatalf("the Widget view never showed the widget: %+v", s.Table(widgetsKey))
	}
	list := dispatch("resources.list", map[string]any{"kind": widgetsKey}).(commands.ListResult)
	if len(list.Rows) != 1 || list.Cell(0, "Color") != "teal" || list.Cell(0, "Size") != "7" || list.RefAt(0).Namespace != crdNamespace {
		t.Fatalf("resources.list: %+v", list)
	}
	found := dispatch("resources.find", map[string]any{"kind": widgetsKey, "field": "color", "value": "teal"}).(commands.FindResult)
	if found.Count != 1 {
		t.Fatalf("resources.find: %+v", found)
	}
	t.Logf("Widget view: %+v", list.Rows[0])

	deleteCRD()
	if !testkit.WaitFor(ctx, func() bool { _, ok := s.Backend().Profile(widgetsKey); return !ok }) {
		t.Fatal("the Widget kind outlived its CRD")
	}
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table(widgetsKey); return tbl != nil && tbl.Error != "" }) {
		t.Fatalf("the open view does not say the kind is gone: %+v", s.Table(widgetsKey))
	}
	if msg := s.Table(widgetsKey).Error; !strings.Contains(msg, "no longer serves Widget") {
		t.Errorf("vanished view: %q", msg)
	}
}
