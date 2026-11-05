package fyneui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// Remove Finalizers on a selection over the real engine: two runner pods being deleted and held by
// a finalizer, and a pod that is not. The selection bar shows the padlock beside Delete while the
// selection holds one being deleted; its confirmation names the two pods, the finalizer once, the
// pod it leaves out and the warning. PNGs: selection_remove_finalizers_dark.png,
// confirm_remove_finalizers_many_dark.png, drawer_remove_finalizers_dark.png.

// runnerPod is a pod of the runners namespace, being deleted and held by finalizers when given.
func runnerPod(name string, fins ...string) *unstructured.Unstructured {
	pod := testkit.Pod("runners", name)
	if len(fins) > 0 {
		pod.SetFinalizers(fins)
		at := metav1.NewTime(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC))
		pod.SetDeletionTimestamp(&at)
	}
	return pod
}

func renderRemoveFinalizersMany(t *testing.T, mode ThemeMode, barPNG, confirmPNG, drawerPNG string) {
	const fin = "actions.example.com/runner-pod"
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "runners", nil))
	src.Set(testkit.PodsGVR, runnerPod("runner-a", fin))
	src.Set(testkit.PodsGVR, runnerPod("runner-b", fin))
	src.Set(testkit.PodsGVR, runnerPod("web"))
	writer := &finalizerWriter{}
	conn := enginekit.MemoryConnector{Context: "kind-cogpit", Src: src, Profiles: kinds.Builtin(), Writer: writer}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond, ClusterSettings: engine.NewClusterSettingsStore("")})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("kind-cogpit", func(cs *engine.ClusterSettings) error {
		cs.SetIntegrations(0, 0, engine.IntAll) // no integrations: nothing asked, nothing looked for
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "pods"})
	q.until(t, "the pods", func() bool { return sh.view.snap != nil && len(sh.view.snap.Rows) == 3 })
	pod := func(name string) model.ObjectRef {
		return model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "runners", Name: name}
	}
	bar := sh.view.check.bar

	// Only the pod not being deleted: no padlock.
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("web")}, "noDrawer": true})
	q.until(t, "the bar", func() bool { return bar.root.Visible() && bar.count.text == "1 selected" })
	settle(w)
	if bar.finPlate.Visible() {
		t.Fatal("Remove Finalizers shown for a pod not being deleted")
	}

	// All three: the padlock beside Delete.
	mustDispatch(t, s, "ui.select", map[string]any{"all": true, "refs": []model.ObjectRef{}, "noDrawer": true})
	q.until(t, "the padlock", func() bool { return bar.count.text == "3 selected" && bar.finPlate.Visible() && !bar.fin.Disabled() })
	savePNG(t, w, barPNG)

	test.Tap(bar.fin)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	savePNG(t, w, confirmPNG)
	test.Tap(sh.confirmer.current.confirm)
	q.until(t, "the removals", func() bool {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return slices.Equal(writer.removed, []string{"runners/runner-a " + fin, "runners/runner-b " + fin})
	})

	// One pod's drawer: the padlock between Edit and Delete.
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{pod("runner-a")}})
	q.until(t, "the drawer's padlock", func() bool {
		return sh.drawer.last != nil && slices.ContainsFunc(sh.drawer.acts, func(a actionState) bool { return a.cmd.ID == engine.RemoveFinalizersCommand })
	})
	savePNG(t, w, drawerPNG)
}

func TestRenderRemoveFinalizersManyDark(t *testing.T) {
	renderRemoveFinalizersMany(t, ThemeDark, "selection_remove_finalizers_dark.png", "confirm_remove_finalizers_many_dark.png", "drawer_remove_finalizers_dark.png")
}
