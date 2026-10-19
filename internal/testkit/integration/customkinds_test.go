package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var widgetsGVR = schema.GroupVersionResource{Group: "e2e.cogpit.test", Version: "v1", Resource: "widgets"}

const widgetsKey = "widgets.e2e.cogpit.test"

func widgetCRD() []testkit.CRDVersion {
	return []testkit.CRDVersion{{Name: "v1", Served: true, Storage: true, Columns: []map[string]any{
		{"name": "Color", "type": "string", "jsonPath": ".spec.color"},
		{"name": "Size", "type": "integer", "jsonPath": ".spec.size"},
	}}}
}

func widgetFields(color string, size int64) map[string]any {
	return map[string]any{"spec": map[string]any{"color": color, "size": size}}
}

// TestBudgetCustomResourceView measures what custom resources cost on a real client-go stack
// against the fake API server: one watch on the definitions for the whole connection, one per
// opened custom resource kind, nothing while idle, and a new definition or object arrives through
// the open watches without a request (cache first, no polling).
func TestBudgetCustomResourceView(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: widgetsGVR, Kind: "Widget", Namespaced: true},
	)})
	defer srv.Close()
	srv.Set(testkit.CRDsGVR, testkit.CRD("e2e.cogpit.test", "widgets", "Widget", true, widgetCRD()...))
	for i := range 20 {
		srv.Set(widgetsGVR, testkit.Object("e2e.cogpit.test/v1", "Widget", "cogpit-crd", fmt.Sprintf("w-%02d", i),
			widgetFields("blue", int64(i))))
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { _, ok := s.Backend().Profile(widgetsKey); return ok }) {
		t.Fatal("the Widget kind never appeared")
	}
	// The Pods view the connection opens reads its pods only once the access reviews decided its
	// plan; the Services are read from the cache by the detection of Prometheus (the Pods view's CPU
	// and Memory columns) and of the Alertmanager (while the cluster is shown). The Widget kind can
	// come first: opened before the reviews' answer, its view ends the Pods view before it watched,
	// and the count below stays at three (2026-10-17; reviews held 1.5 s by the fake: the pods'
	// watch missing three times in three). Leave the Pods view once both watches are open.
	if !testkit.WaitFor(ctx, func() bool { w := srv.OpenWatchesBy(); return w["pods"] == 1 && w["services"] == 1 }) {
		t.Fatalf("the Pods view's watches: %v, want pods and services", srv.OpenWatchesBy())
	}
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: json.RawMessage(`{"view":"` + widgetsKey + `"}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table(widgetsKey); return tbl != nil && tbl.Synced && tbl.Total == 20 }) {
		t.Fatalf("the Widget view never filled: %+v", s.Table(widgetsKey))
	}
	// The definitions, the Pods view the connection opened first (warm), the Widget view, and the
	// Services (above).
	if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatches() == 4 }) {
		t.Fatalf("open watches: %v, want 4", srv.OpenWatchesBy())
	}
	conn := s.Backend().Conn
	synced := conn.Counter().Snapshot()
	for res, want := range map[string]perResource{
		"customresourcedefinitions.apiextensions.k8s.io": {watchlist: 1},
		widgetsKey: {watchlist: 1},
	} {
		if got := countFor(synced, res); got != want {
			t.Errorf("initial cost for %s: %+v, want %+v", res, got, want)
		}
	}

	time.Sleep(idle)
	// A new object and a new definition arrive through the open watches.
	srv.Set(widgetsGVR, testkit.Object("e2e.cogpit.test/v1", "Widget", "cogpit-crd", "late", widgetFields("red", 1)))
	srv.Set(testkit.CRDsGVR, testkit.CRD("e2e.cogpit.test", "gadgets", "Gadget", false))
	if !testkit.WaitFor(ctx, func() bool {
		_, ok := s.Backend().Profile("gadgets.e2e.cogpit.test")
		tbl := s.Table(widgetsKey)
		return ok && tbl != nil && tbl.Total == 21
	}) {
		t.Fatal("the new widget or the new definition did not arrive through the watches")
	}
	if after := conn.Counter().Snapshot(); after.Total != synced.Total {
		t.Errorf("idle and watch events cost %d requests: %v", after.Total-synced.Total, diff(synced, after))
	}
	t.Logf("requests: %v", conn.Counter().Snapshot().ByVerbResource)
}
