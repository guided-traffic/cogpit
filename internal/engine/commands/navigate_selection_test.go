package commands_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestNavigateLeavesTheSelection: selecting and opening a drawer are separate actions (owner):
// ui.navigate opens the drawer and leaves the checked rows of the same view alone; a view it
// changes to shows its own checked rows, none for a view never checked in, and the old view's come
// back with it (owner, audit).
func TestNavigateLeavesTheSelection(t *testing.T) {
	src := testkit.NewMemorySource()
	claims := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	src.Set(claims, testkit.Object("v1", "PersistentVolumeClaim", "apps", "data", map[string]any{"status": map[string]any{"phase": "Bound"}}))
	src.Set(claims, testkit.Object("v1", "PersistentVolumeClaim", "apps", "logs", map[string]any{"status": map[string]any{"phase": "Bound"}}))
	src.Set(testkit.PodsGVR, testkit.Pod("apps", "db-0"))

	s := engine.New(engine.Options{Connector: &claimsConnector{src: src}, SnapshotInterval: 10 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.openView", map[string]any{"view": "persistentvolumeclaims"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("persistentvolumeclaims")
		return tbl != nil && tbl.Synced && len(tbl.Rows) == 2
	}) {
		t.Fatalf("the claims view: %+v", s.Table("persistentvolumeclaims"))
	}
	data := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "apps", Name: "data"}
	logs := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "apps", Name: "logs"}
	h.must(t, agent, "ui.select", map[string]any{"refs": []model.ObjectRef{data}, "noDrawer": true}, nil)

	h.must(t, agent, "ui.navigate", map[string]any{"ref": logs}, nil)
	st := s.UIState()
	if len(st.Selected) != 1 || !st.Selected[0].Same(data) {
		t.Errorf("ui.navigate changed the checked rows: %v", st.Selected)
	}
	if st.Drawer == nil || !st.Drawer.Same(logs) {
		t.Errorf("ui.navigate did not open the drawer: %+v", st.Drawer)
	}

	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "apps", Name: "db-0"}
	h.must(t, agent, "ui.navigate", map[string]any{"ref": pod}, nil)
	if st := s.UIState(); st.View != "pods" || len(st.Selected) != 0 || st.Drawer == nil || !st.Drawer.Same(pod) {
		t.Errorf("another view: view %s, selected %v, drawer %+v", st.View, st.Selected, st.Drawer)
	}
	// The selection bar's Delete takes the shown view's selection: the claims' stays with their view.
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "object.deleteMany", Origin: engine.OriginGUI,
		Params: engine.SelectionParams(s.UIState().Selected)}); err == nil || !strings.Contains(err.Error(), "no objects are selected") {
		t.Errorf("a Delete of the pods view's selection: %v, want nothing selected", err)
	}

	// Each view keeps its checked rows (N111, the owner's decision in the audit): the claims' come
	// back with their view.
	h.must(t, agent, "ui.navigate", map[string]any{"ref": logs}, nil)
	if st := s.UIState(); st.View != "persistentvolumeclaims" || len(st.Selected) != 1 || !st.Selected[0].Same(data) {
		t.Errorf("back in the claims: view %s, selected %v", st.View, st.Selected)
	}
}
