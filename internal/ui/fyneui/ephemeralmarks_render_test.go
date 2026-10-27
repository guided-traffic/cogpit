package fyneui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// Ephemeral (debug) containers in the Pods list and a pod's drawer (D-102) over the real engine:
// web-0 debugged three times (one running with a target, one finished well, one ended with 130),
// api-0 debugged fifteen times (the cell's count), worker-0's waiting for its image and batch-0's
// that cannot pull it. PNGs: ephemeral_marks_pods_{dark,light}.png (the list, the tooltip of
// web-0's Containers cell), ephemeral_containers_drawer_{dark,light}.png (web-0's drawer at its
// Ephemeral Containers).

// quietWriter answers the writes of a render that writes nothing.
type quietWriter struct{}

func (quietWriter) RestartDeployment(context.Context, string, string, string) error { return nil }
func (quietWriter) DeletePod(context.Context, string, string, string) error         { return nil }

// ephemeralSpec and ephemeralStatus build an ephemeral container and its status.
func ephemeralSpec(name, target string) map[string]any {
	c := map[string]any{"name": name, "image": "docker.io/nicolaka/netshoot:v0.16", "stdin": true, "tty": true,
		"terminationMessagePolicy": "File"}
	if target != "" {
		c["targetContainerName"] = target
	}
	return c
}

func ephemeralStatus(name string, state map[string]any) map[string]any {
	return map[string]any{"name": name, "image": "docker.io/nicolaka/netshoot:v0.16", "state": state}
}

var (
	ephRunning = map[string]any{"running": map[string]any{"startedAt": "2026-10-11T14:20:00Z"}}
	ephDone    = map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0),
		"startedAt": "2026-10-11T13:00:00Z", "finishedAt": "2026-10-11T13:00:04Z"}}
	ephFailed = map[string]any{"terminated": map[string]any{"reason": "Error", "exitCode": int64(130),
		"startedAt": "2026-10-11T13:10:00Z", "finishedAt": "2026-10-11T13:12:00Z"}}
)

// debuggedPod is a running pod of the given containers with ephemeral containers specs and their
// statuses.
func debuggedPod(name string, containers []string, specs, statuses []any) map[string]any {
	cs, sts := make([]any, 0, len(containers)), make([]any, 0, len(containers))
	for _, c := range containers {
		cs = append(cs, map[string]any{"name": c, "image": "registry.example/" + c + ":1.0"})
		sts = append(sts, map[string]any{"name": c, "ready": true, "restartCount": int64(0),
			"state": map[string]any{"running": map[string]any{"startedAt": "2026-10-11T12:00:00Z"}}})
	}
	return map[string]any{
		"spec": map[string]any{"nodeName": "node-1", "containers": cs, "ephemeralContainers": specs},
		"status": map[string]any{"phase": "Running", "podIP": "10.244.0.10", "qosClass": "BestEffort",
			"containerStatuses": sts, "ephemeralContainerStatuses": statuses},
	}
}

// ephemeralFixture is a source of the shop's pods (ephemeralPods).
func ephemeralFixture() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	for _, p := range ephemeralPods() {
		src.Set(testkit.PodsGVR, p)
	}
	return src
}

// ephemeralPods are the shop's pods by name.
func ephemeralPods() map[string]*unstructured.Unstructured {
	out := map[string]*unstructured.Unstructured{}
	pod := func(name string, fields map[string]any) {
		out[name] = testkit.Object("v1", "Pod", "shop", name, fields)
	}
	pod("web-0", debuggedPod("web-0", []string{"app", "envoy"},
		[]any{ephemeralSpec("dnscheck", ""), ephemeralSpec("debugger", "app"), ephemeralSpec("debugger-x7k2q", "app")},
		[]any{ephemeralStatus("dnscheck", ephDone), ephemeralStatus("debugger", ephRunning), ephemeralStatus("debugger-x7k2q", ephFailed)}))
	var specs, statuses []any
	for i := range 14 {
		n := fmt.Sprintf("debugger-%05d", i)
		specs, statuses = append(specs, ephemeralSpec(n, "api")), append(statuses, ephemeralStatus(n, ephDone))
	}
	specs, statuses = append(specs, ephemeralSpec("debugger-live", "api")), append(statuses, ephemeralStatus("debugger-live", ephRunning))
	pod("api-0", debuggedPod("api-0", []string{"api"}, specs, statuses))
	pod("worker-0", debuggedPod("worker-0", []string{"worker"}, []any{ephemeralSpec("debugger-pull", "worker")},
		[]any{ephemeralStatus("debugger-pull", map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}})}))
	pod("batch-0", debuggedPod("batch-0", []string{"batch"}, []any{ephemeralSpec("debugger-bad", "")},
		[]any{ephemeralStatus("debugger-bad", map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff",
			"message": "Back-off pulling image"}})}))
	pod("web-1", debuggedPod("web-1", []string{"app", "envoy"}, nil, nil))
	return out
}

// ephemeralEngine is the real engine on src, without integrations.
func ephemeralEngine(t *testing.T, src *testkit.MemorySource, writer engine.Actions) *engine.Session {
	t.Helper()
	conn := enginekit.MemoryConnector{Context: "kind-cogpit", Src: src, Profiles: kinds.Builtin(), Writer: writer}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond,
		ClusterSettings: engine.NewClusterSettingsStore(""), Preferences: engine.NewPreferencesStore("")})
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
	return s
}

func renderEphemeralMarks(t *testing.T, mode ThemeMode, listPNG, drawerPNG string) {
	s := ephemeralEngine(t, ephemeralFixture(), quietWriter{})
	sh, w, q := newEngineShell(t, s, Options{AutoConnect: "kind-cogpit", Theme: mode})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	mustDispatch(t, s, "ui.openView", map[string]any{"view": "pods"})
	q.until(t, "the pods", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" && len(sh.view.snap.Rows) == 5 })
	settle(w)
	col := columnOf(t, sh, "containers")
	row := -1
	for i, r := range sh.view.snap.Rows {
		if r.Ref.Name == "web-0" {
			row = i
		}
	}
	sh.tip.delay = time.Millisecond
	hoverTip(t, sh, w, q, "the Containers cell's tooltip", middleOf(cellAt(t, sh, row, col)),
		middleOf(cellAt(t, sh, row, columnOf(t, sh, "name"))))
	savePNG(t, w, listPNG)
	sh.tip.hide()

	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "shop", Name: "web-0"}
	mustDispatch(t, s, "ui.select", map[string]any{"refs": []model.ObjectRef{ref}})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref.Name == "web-0" })
	settle(w)
	sh.drawer.scroll.ScrollToBottom()
	savePNG(t, w, drawerPNG)
}

func TestRenderEphemeralMarksDark(t *testing.T) {
	renderEphemeralMarks(t, ThemeDark, "ephemeral_marks_pods_dark.png", "ephemeral_containers_drawer_dark.png")
}
