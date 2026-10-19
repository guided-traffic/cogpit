package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The debug container (D-102) against the fake writer: every origin confirms it (the owner:
// destructive), the confirmation names the place, the target and the image; the container is
// kubectl debug's; the Attach tab opens on it once the pod's watch brought it; the image is the
// Preferences', which only the user sets.

func init() {
	wantCommands[engine.DebugCommand] = engine.DangerDestructive // the owner, 2026-10-19
}

// debugServer is the fake cluster side of the debug containers of one fake writer: it adds the
// container to the pod in the memory source, as the API server would, so that the watch brings it.
type debugServer struct {
	mu    sync.Mutex
	src   *testkit.MemorySource
	added []corev1.EphemeralContainer
	uids  []string
	err   error // the API server's refusal
}

var debugServers sync.Map // *fakeWriter → *debugServer

func (w *fakeWriter) debugServer() *debugServer {
	v, _ := debugServers.LoadOrStore(w, &debugServer{})
	return v.(*debugServer)
}

func (w *fakeWriter) AddEphemeralContainer(_ context.Context, namespace, pod, uid string, ec corev1.EphemeralContainer) error {
	srv := w.debugServer()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	w.record("debug " + namespace + "/" + pod + " " + ec.Name)
	if srv.err != nil {
		return srv.err
	}
	srv.added = append(srv.added, ec)
	srv.uids = append(srv.uids, uid)
	if srv.src == nil {
		return nil
	}
	obj := testkit.Pod(namespace, pod)
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ec)
	if err != nil {
		return err
	}
	spec := obj.Object["spec"].(map[string]any)
	spec["ephemeralContainers"] = []any{raw}
	srv.src.Set(testkit.PodsGVR, obj)
	return nil
}

// debugHarness is a harness whose writer's debug server adds to its source, with the preferences
// in memory and every confirmation recorded and answered yes.
func debugHarness(t *testing.T, src *testkit.MemorySource) (*harness, *confirmations) {
	t.Helper()
	if src == nil {
		src = seed()
	}
	conf := &confirmations{answer: true}
	h := newHarness(t, src, engine.Options{Confirmer: conf, Preferences: engine.NewPreferencesStore("")})
	h.writer.debugServer().src = src
	if _, err := h.s.Objects(context.Background(), "pods", nil); err != nil { // warm: Enabled does not wait
		t.Fatal(err)
	}
	return h, conf
}

var debugWeb0 = model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}

// TestDebugAddsTheConfirmedContainer: the user's Debug is confirmed with the rows Context ›
// Namespace › Pod, Target and what the container sees of it, Image and its digest, and "This
// cannot be undone." (not undoable); the container is kubectl debug's, bound to the pod's UID;
// the Attach tab opens on it.
func TestDebugAddsTheConfirmedContainer(t *testing.T) {
	h, conf := debugHarness(t, nil)
	var res commands.DebugResult
	h.must(t, engine.OriginGUI, engine.DebugCommand, map[string]any{"ref": debugWeb0, "target": "app"}, &res)

	req := conf.last(t)
	if req.Danger != engine.DangerDestructive || req.Undoable || req.Context != "test" || len(req.Objects) != 1 ||
		req.Objects[0].Name != "web-0" || !slices.Equal(req.Namespaces, []string{"shop"}) || !slices.Equal(req.UIDs, []string{"uid-shop-web-0"}) {
		t.Errorf("confirmation %+v", req)
	}
	at := strings.LastIndex(engine.DefaultDebugImage, "@") // Renovate moves the default: the test follows it
	want := []engine.Effect{
		{Name: "Target", Value: "app", Tone: engine.EffectNames},
		{Name: "Sees", Value: "app's processes and files", Tone: engine.EffectWarning},
		{Name: "Image", Value: engine.DefaultDebugImage[:at], Tone: engine.EffectNames},
		{Name: "Digest", Value: engine.DefaultDebugImage[at+1:]},
	}
	if !slices.Equal(req.Effects, want) {
		t.Errorf("effects %+v,\nwant %+v", req.Effects, want)
	}
	if req.Detail != engine.DebugDetail("app", engine.DefaultDebugImage) {
		t.Errorf("detail %q", req.Detail)
	}

	srv := h.writer.debugServer()
	if len(srv.added) != 1 || !slices.Equal(srv.uids, []string{"uid-shop-web-0"}) {
		t.Fatalf("added %+v with %v", srv.added, srv.uids)
	}
	ec := srv.added[0]
	if !strings.HasPrefix(ec.Name, engine.DebugContainerPrefix) || len(ec.Name) != len(engine.DebugContainerPrefix)+5 ||
		ec.Name != strings.ToLower(ec.Name) || ec.Image != engine.DefaultDebugImage || !ec.Stdin || !ec.TTY || ec.StdinOnce ||
		ec.TargetContainerName != "app" || ec.TerminationMessagePolicy != corev1.TerminationMessageReadFile || len(ec.Command) != 0 {
		t.Errorf("container %+v", ec)
	}
	if res.Container != ec.Name || res.Image != engine.DefaultDebugImage || res.Target != "app" || res.Tab == nil {
		t.Fatalf("result %+v", res)
	}
	if tab := *res.Tab; tab.Type != engine.DockAttach || tab.Container != ec.Name || tab.Ref == nil || !tab.Ref.Same(debugWeb0) ||
		tab.Title != "Attach: web-0/"+ec.Name || h.s.UIState().ActiveDock != tab.ID {
		t.Errorf("tab %+v", tab)
	}
	cmd, _ := h.s.Registry().Get(engine.DebugCommand)
	if want := []engine.Verb{{Verb: "patch", Resource: "pods", Subresource: "ephemeralcontainers"},
		{Verb: "create", Resource: "pods", Subresource: "attach"}}; !slices.Equal(cmd.Verbs, want) {
		t.Errorf("verbs %+v", cmd.Verbs)
	}
}

// TestDebugNeedsEveryOriginsConfirmation: declined, nothing is added, for the GUI, the CLI and
// agents alike, even with agent writes allowed (D-2: destructive always asks).
func TestDebugNeedsEveryOriginsConfirmation(t *testing.T) {
	h, conf := debugHarness(t, nil)
	conf.answer = false
	h.s.SetAgentWrites("test", true)
	for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		if err := h.run(t, origin, engine.DebugCommand, map[string]any{"ref": debugWeb0}, nil); !errors.Is(err, engine.ErrDeclined) {
			t.Errorf("%s declined: %v", origin, err)
		}
	}
	if n := len(conf.all()); n != 4 {
		t.Errorf("%d confirmations, want one per origin", n)
	}
	if added := h.writer.debugServer().added; len(added) != 0 {
		t.Errorf("declined debugs added %+v", added)
	}
	// Without a target the confirmation says so, and names no "Sees".
	if req := conf.last(t); len(req.Effects) == 0 || req.Effects[0] != (engine.Effect{Name: "Target", Value: "none"}) ||
		slices.ContainsFunc(req.Effects, func(e engine.Effect) bool { return e.Name == "Sees" }) {
		t.Errorf("effects without a target %+v", req.Effects)
	}
}

// TestDebugRefusals: a pod that has ended or is being deleted is greyed with the reason; a target
// that is none of its containers, an image that is no reference and a UID of another pod are
// refused before anything is written; the API server's refusal (Pod Security admission, a policy
// engine) reaches the caller with its message.
func TestDebugRefusals(t *testing.T) {
	src := seed()
	done := testkit.Pod("shop", "batch-1")
	done.Object["status"].(map[string]any)["phase"] = "Succeeded"
	src.Set(testkit.PodsGVR, done)
	going := testkit.Pod("shop", "web-9")
	created := going.GetCreationTimestamp()
	going.SetDeletionTimestamp(&created)
	src.Set(testkit.PodsGVR, going)
	h, _ := debugHarness(t, src)

	for name, c := range map[string]struct {
		params map[string]any
		want   string
	}{
		"ended":      {map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "batch-1"}}, "the pod has ended (succeeded)"},
		"deleting":   {map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-9"}}, "the pod is being deleted"},
		"target":     {map[string]any{"ref": debugWeb0, "target": "sidecar"}, `pod web-0 has no container "sidecar" to target; its containers: app`},
		"image":      {map[string]any{"ref": debugWeb0, "image": "nicolaka/netshoot v0.16"}, "a reference without spaces"},
		"url":        {map[string]any{"ref": debugWeb0, "image": "https://example.com/netshoot"}, "not a URL"},
		"other uid":  {map[string]any{"ref": debugWeb0, "uid": "uid-of-another"}, "is not the one read"},
		"other kind": {map[string]any{"ref": model.ObjectRef{Kind: "services", Namespace: "shop", Name: "checkout"}}, "applies to pods"},
	} {
		err := h.run(t, engine.OriginGUI, engine.DebugCommand, c.params, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if added := h.writer.debugServer().added; len(added) != 0 {
		t.Errorf("refused debugs added %+v", added)
	}

	h.writer.debugServer().err = errors.New(`pods "web-0" is forbidden: violates PodSecurity "restricted:latest": allowPrivilegeEscalation != false`)
	err := h.run(t, engine.OriginGUI, engine.DebugCommand, map[string]any{"ref": debugWeb0}, nil)
	if err == nil || !strings.Contains(err.Error(), "violates PodSecurity") {
		t.Errorf("the API server's refusal: %v", err)
	}
}

// TestDebugPrefill: the dialog starts from the Preferences' image (the user's own, else Cogpit's
// default), the pod's first container as the target and the pod's UID, its reference with its
// context named as the confirmation names it.
func TestDebugPrefill(t *testing.T) {
	h, _ := debugHarness(t, nil)
	cmd, _ := h.s.Registry().Get(engine.DebugCommand)
	prefill := func() map[string]any {
		raw, err := cmd.Prefill(h.s, cmd.ObjectParams(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}, ""))
		if err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		return p
	}
	p := prefill()
	if p["image"] != engine.DefaultDebugImage || p["target"] != "app" || p["uid"] != "uid-shop-web-0" ||
		p["ref"].(map[string]any)["context"] != "test" {
		t.Errorf("prefill %v", p)
	}
	h.must(t, engine.OriginGUI, "preferences.set", map[string]any{"debugImage": "registry.example/tools:1"}, nil)
	if p := prefill(); p["image"] != "registry.example/tools:1" {
		t.Errorf("prefill with the user's image %v", p)
	}
	if fields := cmd.DialogFields(); !slices.Equal(fields, []string{"image", "target", "uid"}) {
		t.Errorf("dialog fields %v", fields)
	}
}

// TestDebugImagePreference: only the user sets the debug image; agents and the CLI may reset it;
// what is no image reference is refused; an image that is not pinned shows as a floating tag in
// the confirmation.
func TestDebugImagePreference(t *testing.T) {
	h, conf := debugHarness(t, nil)
	h.must(t, engine.OriginGUI, "preferences.set", map[string]any{"debugImage": " registry.example/tools:1 "}, nil)
	if got := h.s.DebugImage(); got != "registry.example/tools:1" {
		t.Errorf("debug image %q", got)
	}
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		if err := h.run(t, origin, "preferences.set", map[string]any{"debugImage": "evil.example/x:1"}, nil); !errors.Is(err, engine.ErrDebugImageGUIOnly) {
			t.Errorf("%s set the image: %v", origin, err)
		}
	}
	if err := h.run(t, engine.OriginGUI, "preferences.set", map[string]any{"debugImage": "a b"}, nil); err == nil {
		t.Error("an image with a space was kept")
	}
	h.must(t, engine.OriginGUI, engine.DebugCommand, map[string]any{"ref": debugWeb0}, nil)
	if req := conf.last(t); !slices.Contains(req.Effects, engine.Effect{Name: "Image", Value: "registry.example/tools:1", Tone: engine.EffectNames}) ||
		!slices.ContainsFunc(req.Effects, func(e engine.Effect) bool { return e.Name == "Digest" && e.Tone == engine.EffectWarning }) {
		t.Errorf("effects of a floating tag %+v", req.Effects)
	}
	h.must(t, engine.OriginExternalAgent, "preferences.set", map[string]any{"debugImage": ""}, nil) // a reset is allowed
	if got := h.s.DebugImage(); got != engine.DefaultDebugImage {
		t.Errorf("after the reset %q", got)
	}
}

// TestDebugContainerReachedByLogsShellAndAttach: a pod's ephemeral container is in the logs tab's
// container list apart, and Logs, Shell and Attach accept it (the owner's "Fix bauen").
func TestDebugContainerReachedByLogsShellAndAttach(t *testing.T) {
	h, _ := debugHarness(t, nil)
	var res commands.DebugResult
	h.must(t, engine.OriginGUI, engine.DebugCommand, map[string]any{"ref": debugWeb0, "target": "app"}, &res)
	name := res.Container
	var logs commands.DockResult
	h.must(t, engine.OriginGUI, "pod.logs", map[string]any{"ref": debugWeb0, "container": name}, &logs)
	if logs.Tab.Container != name {
		t.Errorf("logs tab %+v", logs.Tab)
	}
	var src engine.LogSources
	h.must(t, engine.OriginGUI, "logs.sources", map[string]any{"id": logs.Tab.ID}, &src)
	if !slices.Equal(src.Containers, []string{"app"}) || !slices.Equal(src.EphemeralContainers, []string{name}) {
		t.Errorf("sources %+v", src)
	}
	for _, id := range []string{"pod.shell", "pod.attach"} {
		var tab commands.DockResult
		h.must(t, engine.OriginGUI, id, map[string]any{"ref": debugWeb0, "container": name}, &tab)
		if tab.Tab.Container != name {
			t.Errorf("%s tab %+v", id, tab.Tab)
		}
	}
	m, err := h.s.Detail(debugWeb0)
	if err != nil || !slices.Equal(m.EphemeralContainers, []string{name}) || !slices.Equal(m.Containers, []string{"app"}) {
		t.Errorf("drawer model %v: %+v", err, m)
	}
}
