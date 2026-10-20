package fyneui

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Debug dialog (D-102) over the real engine: it is the confirmation (the engine's own does not
// ask again), its image an ordinary entry prefilled with the Preferences' image, its target the
// pod's containers with the first preselected; a pod replaced while it is open is refused; the
// pod's menus offer Debug after Shell and its ephemeral containers to Logs, Shell and Attach.
// PNGs: debug_dialog_{dark,light}.png.

// debugWriter records the debug containers added and adds them to the source, as the API server
// would, so that the pod's watch brings them.
type debugWriter struct {
	quietWriter
	src   *testkit.MemorySource
	mu    sync.Mutex
	pods  map[string]*unstructured.Unstructured // the source's pods as this writer last set them
	added []corev1.EphemeralContainer
	uids  []string
}

func (w *debugWriter) AddEphemeralContainer(_ context.Context, _, pod, uid string, ec corev1.EphemeralContainer) error {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ec)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.added = append(w.added, ec)
	w.uids = append(w.uids, uid)
	obj := w.pods[pod].DeepCopy()
	spec := obj.Object["spec"].(map[string]any)
	specs, _ := spec["ephemeralContainers"].([]any)
	spec["ephemeralContainers"] = append(specs, raw)
	w.pods[pod] = obj
	w.src.Set(testkit.PodsGVR, obj)
	return nil
}

func (w *debugWriter) calls() ([]corev1.EphemeralContainer, []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.added), slices.Clone(w.uids)
}

var debugWeb1 = model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "shop", Name: "web-1"}

// openDebug opens the Debug dialog on web-1 as its drawer's toolbar or row menu does (runAction
// with the evaluated action and its Prefill) and waits for it.
func openDebug(t *testing.T, sh *shell, w fyne.Window, q *mainQueue) {
	t.Helper()
	for _, a := range sh.evaluateActions(debugWeb1, sh.containersFor(debugWeb1)) {
		if a.cmd.ID == engine.DebugCommand {
			if a.reason != "" {
				t.Fatalf("Debug is greyed: %s", a.reason)
			}
			sh.runAction(a, debugWeb1, "")
			q.until(t, "the Debug dialog", func() bool { return findButton(w, "Debug") != nil })
			return
		}
	}
	t.Fatal("no Debug among the pod's actions")
}

// dialogEntry and dialogSelect are the dialog's image entry and target select.
func dialogEntry(t *testing.T, w fyne.Window) *widget.Entry {
	t.Helper()
	es := findAll[*widget.Entry](w.Canvas().Overlays().Top())
	if len(es) != 1 {
		t.Fatalf("%d entries in the dialog", len(es))
	}
	return es[0]
}

func dialogSelect(t *testing.T, w fyne.Window) *selectField {
	t.Helper()
	ss := findAll[*selectField](w.Canvas().Overlays().Top())
	if len(ss) != 1 {
		t.Fatalf("%d selects in the dialog", len(ss))
	}
	return ss[0]
}

func debugShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *engine.Session, *debugWriter, *testkit.MemorySource) {
	t.Helper()
	src := ephemeralFixture()
	writer := &debugWriter{src: src, pods: ephemeralPods()}
	s := ephemeralEngine(t, src, writer)
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "pods"})
	q.until(t, "the pods", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" && len(sh.view.snap.Rows) == 5 })
	return sh, w, q, s, writer, src
}

func renderDebugDialog(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _, _ := debugShell(t, mode)
	mustDispatch(t, sh.sess, "ui.select", map[string]any{"refs": []model.ObjectRef{debugWeb1}})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref.Name == "web-1" })
	openDebug(t, sh, w, q)
	savePNG(t, w, name)
}

func TestRenderDebugDialogDark(t *testing.T) {
	renderDebugDialog(t, ThemeDark, "debug_dialog_dark.png")
}
func TestRenderDebugDialogLight(t *testing.T) {
	renderDebugDialog(t, ThemeLight, "debug_dialog_light.png")
}

// TestRenderDebugAgentConfirmation: an agent's Debug asks with the engine's confirmation, the same
// rows as the window's dialog, and adds nothing when the user cancels. PNG:
// confirm_debug_agent_dark.png.
func TestRenderDebugAgentConfirmation(t *testing.T) {
	sh, w, q, s, writer, _ := debugShell(t, ThemeDark)
	done := make(chan error, 1)
	go func() {
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: engine.DebugCommand,
			Params: mustJSON(t, map[string]any{"ref": debugWeb1, "target": "app"}), Origin: engine.OriginExternalAgent})
		done <- err
	}()
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	savePNG(t, w, "confirm_debug_agent_dark.png")
	test.Tap(sh.confirmer.current.cancel)
	if err := <-done; err == nil {
		t.Error("a cancelled agent's Debug ran")
	}
	if added, _ := writer.calls(); len(added) != 0 {
		t.Errorf("a cancelled Debug added %+v", added)
	}
}

// TestDebugDialogIsTheConfirmation: the dialog starts from the Preferences' image and the pod's
// first container; the user edits the image and picks the other container; Debug adds exactly that
// to the pod the dialog showed, without a second dialog, and the Attach tab opens on it.
func TestDebugDialogIsTheConfirmation(t *testing.T) {
	sh, w, q, s, writer, _ := debugShell(t, ThemeDark)
	openDebug(t, sh, w, q)
	image, target := dialogEntry(t, w), dialogSelect(t, w)
	if image.Text != engine.DefaultDebugImage || target.Selected != "app" || !slices.Equal(target.Options, []string{"app", "envoy", debugNoTarget}) {
		t.Fatalf("starts with %q, target %q of %v", image.Text, target.Selected, target.Options)
	}
	if b := findButton(w, "Debug"); b == nil || b.Disabled() {
		t.Fatal("Debug is not armed")
	}
	image.SetText("registry.example/tools:2")
	target.SetSelected("envoy")
	q.drain()
	test.Tap(findButton(w, "Debug"))
	q.until(t, "the container", func() bool { added, _ := writer.calls(); return len(added) == 1 })
	added, uids := writer.calls()
	if ec := added[0]; ec.Image != "registry.example/tools:2" || ec.TargetContainerName != "envoy" || !ec.Stdin || !ec.TTY ||
		!slices.Equal(uids, []string{"uid-shop-web-1"}) {
		t.Errorf("added %+v to %v", ec, uids)
	}
	if sh.confirmer.current != nil {
		t.Error("the engine's confirmation asked again")
	}
	q.until(t, "the Attach tab", func() bool {
		u := s.UIState()
		tab, ok := u.DockTab(u.ActiveDock)
		return ok && tab.Type == engine.DockAttach && tab.Container == added[0].Name
	})
}

// TestDebugDialogEmptyImageAndNoTarget: Debug stays disabled while the image is empty; None adds
// a container without a target.
func TestDebugDialogEmptyImageAndNoTarget(t *testing.T) {
	sh, w, q, _, writer, _ := debugShell(t, ThemeLight)
	openDebug(t, sh, w, q)
	image := dialogEntry(t, w)
	image.SetText("  ")
	q.drain()
	if !findButton(w, "Debug").Disabled() {
		t.Error("Debug is enabled without an image")
	}
	image.SetText("busybox:1.37")
	dialogSelect(t, w).SetSelected(debugNoTarget)
	q.drain()
	test.Tap(findButton(w, "Debug"))
	q.until(t, "the container", func() bool { added, _ := writer.calls(); return len(added) == 1 })
	if added, _ := writer.calls(); added[0].TargetContainerName != "" || added[0].Image != "busybox:1.37" {
		t.Errorf("added %+v", added[0])
	}
}

// TestDebugDialogRefusesAReplacedPod: a pod created again under the name while the dialog was open
// is not debugged; the refusal says why.
func TestDebugDialogRefusesAReplacedPod(t *testing.T) {
	sh, w, q, _, writer, src := debugShell(t, ThemeDark)
	openDebug(t, sh, w, q)
	again := ephemeralPods()["web-1"]
	again.SetUID(types.UID("uid-shop-web-1-again"))
	src.Set(testkit.PodsGVR, again)
	q.until(t, "the new pod in the cache", func() bool {
		o, err := sh.sess.Lookup(debugWeb1)
		return err == nil && o.GetUID() == "uid-shop-web-1-again"
	})
	test.Tap(findButton(w, "Debug"))
	q.until(t, "the refusal", func() bool { return strings.Contains(toastTexts(sh), errPodReplacedWhileOpen.Error()) })
	if added, _ := writer.calls(); len(added) != 0 {
		t.Errorf("a replaced pod was debugged: %+v", added)
	}
	if sh.confirmer.current != nil {
		t.Error("the engine asked again after the refusal")
	}
}

// TestPodMenusOfferDebugAndEphemeralContainers: Debug sits after Shell in a pod's row menu; Logs,
// Shell and Attach offer its ephemeral containers after the others.
func TestPodMenusOfferDebugAndEphemeralContainers(t *testing.T) {
	sh, _, _, _, _, _ := debugShell(t, ThemeDark)
	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "shop", Name: "web-0"}
	acts := sh.evaluateActions(ref, sh.containersFor(ref))
	var ids []string
	for _, a := range acts {
		ids = append(ids, a.cmd.ID)
	}
	if i, j := slices.Index(ids, "pod.shell"), slices.Index(ids, engine.DebugCommand); i < 0 || j != i+1 {
		t.Errorf("actions %v: Debug is not right after Shell", ids)
	}
	want := []string{"app", "envoy", "dnscheck", "debugger", "debugger-x7k2q"}
	for _, a := range acts {
		if a.cmd.TakesContainer() && !slices.Equal(a.containers, want) {
			t.Errorf("%s offers %v, want %v", a.cmd.ID, a.containers, want)
		}
	}
	items := sh.actionItems(ref, acts)
	for _, it := range items {
		if it.Label == "Logs" && (it.ChildMenu == nil || len(it.ChildMenu.Items) != len(want)) {
			t.Errorf("Logs' submenu %+v", it.ChildMenu)
		}
	}
}
