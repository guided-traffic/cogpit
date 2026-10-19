package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The node shell (G7, G8; D-37) against the fake writer: every origin confirms it, the confirmation
// names the namespace, the node, the image and that the pod is privileged; the pod is exactly the
// one privilege-footprint.md's rules ask for; it is deleted when its tab closes and when Cogpit
// quits; its image and pull secret are the user's to set.

func init() {
	wantCommands[engine.NodeShellCommand] = engine.DangerDestructive
	wantCommands["cluster.settings.setNodeShell"] = engine.DangerUI
}

// nodeShellServer is the fake cluster side of the node shells of one fake writer.
type nodeShellServer struct {
	mu      sync.Mutex
	created []*corev1.Pod
	waitErr error
}

var nodeShellServers sync.Map // *fakeWriter → *nodeShellServer

func (w *fakeWriter) shellServer() *nodeShellServer {
	v, _ := nodeShellServers.LoadOrStore(w, &nodeShellServer{})
	return v.(*nodeShellServer)
}

func (w *fakeWriter) CreatePod(_ context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	srv := w.shellServer()
	srv.mu.Lock()
	srv.created = append(srv.created, pod.DeepCopy())
	srv.mu.Unlock()
	w.record("create pod " + pod.Namespace + "/" + pod.Name)
	out := pod.DeepCopy()
	out.UID = types.UID("uid-" + pod.Name)
	return out, nil
}

func (w *fakeWriter) WaitPodRunning(_ context.Context, namespace, name, container string) error {
	w.record("wait " + namespace + "/" + name + " " + container)
	srv := w.shellServer()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.waitErr
}

// createdPods returns the node shell pods the writer created.
func (w *fakeWriter) createdPods() []*corev1.Pod {
	srv := w.shellServer()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return slices.Clone(srv.created)
}

// waitCall waits until the writer recorded call.
func waitCall(t *testing.T, w *fakeWriter, call string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return slices.Contains(w.Calls(), call) }) {
		t.Fatalf("no %q among %v", call, w.Calls())
	}
}

var worker1 = model.ObjectRef{Kind: "nodes", Name: "worker-1"}

// nodeShellHarness is the actions harness with cluster settings kept in memory and every
// confirmation recorded and answered yes.
func nodeShellHarness(t *testing.T) (*harness, *confirmations) {
	t.Helper()
	conf := &confirmations{answer: true}
	h, _ := newActionsHarness(t, nil, engine.Options{Confirmer: conf, ClusterSettings: engine.NewClusterSettingsStore("")})
	return h, conf
}

// TestNodeShellPodAndConfirmation: the user's own node shell is confirmed, naming the node, the
// namespace, the image pinned by digest and the privileged pod with root access; the pod is the one
// the rules ask for; the tab names it.
func TestNodeShellPodAndConfirmation(t *testing.T) {
	h, conf := nodeShellHarness(t)
	var res commands.DockResult
	h.must(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, &res)

	req := conf.last(t)
	if req.Danger != engine.DangerDestructive || req.Context != "test" || len(req.Objects) != 1 || req.Objects[0].Name != "worker-1" {
		t.Errorf("confirmation %+v", req)
	}
	msg := req.Message()
	at := strings.LastIndex(engine.DefaultNodeShellImage, "@") // Renovate moves the default: the test follows it
	for _, want := range []string{"Shell Node worker-1 (context test)?", "Creates: a privileged pod in namespace kube-system on node worker-1.",
		"Access: root on the node", "Image: " + engine.DefaultNodeShellImage[:at] + ". Digest: " + engine.DefaultNodeShellImage[at+1:] + ".",
		"Ends: when the shell closes, by itself after 2 hours."} {
		if !strings.Contains(msg, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, msg)
		}
	}

	pods := h.writer.createdPods()
	if len(pods) != 1 {
		t.Fatalf("%d pods created", len(pods))
	}
	p := pods[0]
	sp := p.Spec
	c := sp.Containers
	switch {
	case p.Namespace != "kube-system" || !strings.HasPrefix(p.Name, "cogpit-node-shell-") || len(p.Name) != len("cogpit-node-shell-")+8:
		t.Errorf("pod %s/%s", p.Namespace, p.Name)
	case p.Labels["app.kubernetes.io/name"] != "cogpit-node-shell" || p.Labels["app.kubernetes.io/managed-by"] != "cogpit":
		t.Errorf("labels %v", p.Labels)
	case sp.NodeName != "worker-1" || !sp.HostPID || sp.HostNetwork || sp.HostIPC || sp.RestartPolicy != corev1.RestartPolicyNever:
		t.Errorf("spec %+v", sp)
	case sp.ActiveDeadlineSeconds == nil || *sp.ActiveDeadlineSeconds != 7200:
		t.Errorf("deadline %v", sp.ActiveDeadlineSeconds)
	case sp.AutomountServiceAccountToken == nil || *sp.AutomountServiceAccountToken:
		t.Error("the pod mounts a service account token")
	case len(sp.Tolerations) != 1 || sp.Tolerations[0] != (corev1.Toleration{Operator: corev1.TolerationOpExists}):
		t.Errorf("tolerations %+v", sp.Tolerations)
	case len(sp.ImagePullSecrets) != 0 || sp.TerminationGracePeriodSeconds == nil || *sp.TerminationGracePeriodSeconds != 1:
		t.Errorf("pull secrets %v, grace %v", sp.ImagePullSecrets, sp.TerminationGracePeriodSeconds)
	case len(c) != 1 || c[0].Name != "shell" || c[0].Image != engine.DefaultNodeShellImage ||
		c[0].SecurityContext == nil || c[0].SecurityContext.Privileged == nil || !*c[0].SecurityContext.Privileged ||
		!slices.Equal(c[0].Command, []string{"sleep", "7200"}):
		t.Errorf("container %+v", c)
	}

	tab := res.Tab
	if tab.Type != engine.DockNodeShell || tab.Title != "Node shell: worker-1" || tab.NodeShell == nil ||
		tab.NodeShell.Pod != p.Name || tab.NodeShell.Namespace != "kube-system" || !tab.NodeShell.Pinned || tab.NodeShell.DeadlineSeconds != 7200 {
		t.Errorf("tab %+v %+v", tab, tab.NodeShell)
	}
	if u := h.s.UIState(); u.ActiveDock != tab.ID {
		t.Errorf("active dock %q, want %q", u.ActiveDock, tab.ID)
	}
	if got := h.s.NodeShellPods()[tab.ID]; got != "kube-system/"+p.Name {
		t.Errorf("recorded pod %q", got)
	}
}

// TestNodeShellNeedsEveryOriginsConfirmation: declined, nothing is created, for the GUI, the CLI
// and agents alike, even with agent writes allowed; without a window to confirm in it is refused.
func TestNodeShellNeedsEveryOriginsConfirmation(t *testing.T) {
	h, conf := nodeShellHarness(t)
	conf.answer = false
	h.s.SetAgentWrites("test", true)
	for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		if err := h.run(t, origin, engine.NodeShellCommand, map[string]any{"ref": worker1}, nil); !errors.Is(err, engine.ErrDeclined) {
			t.Errorf("%s declined: %v", origin, err)
		}
	}
	if n := len(conf.all()); n != 4 {
		t.Errorf("%d confirmations, want one per origin", n)
	}
	if pods := h.writer.createdPods(); len(pods) != 0 {
		t.Errorf("declined node shells created %d pods", len(pods))
	}

	headless, _ := newActionsHarness(t, nil, engine.Options{})
	if err := headless.run(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("without a confirmer: %v", err)
	}
}

// TestNodeShellPodGoesWithItsTab: closing the tab deletes the pod with its UID; Cogpit's quit
// deletes the pods of the tabs still open.
func TestNodeShellPodGoesWithItsTab(t *testing.T) {
	h, _ := nodeShellHarness(t)
	var first, second commands.DockResult
	h.must(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, &first)
	h.must(t, engine.OriginExternalAgent, engine.NodeShellCommand, map[string]any{"ref": worker1}, &second)
	h.must(t, engine.OriginGUI, "dock.close", map[string]any{"id": first.Tab.ID}, nil)
	waitCall(t, h.writer, "delete kube-system/"+first.Tab.NodeShell.Pod)
	if _, open := h.s.NodeShellPods()[first.Tab.ID]; open {
		t.Error("the closed tab's pod is still recorded")
	}
	if slices.Contains(h.writer.Calls(), "delete kube-system/"+second.Tab.NodeShell.Pod) {
		t.Error("the open tab's pod was deleted")
	}
	h.s.Close()
	if !slices.Contains(h.writer.Calls(), "delete kube-system/"+second.Tab.NodeShell.Pod) {
		t.Errorf("quit left the open node shell's pod: %v", h.writer.Calls())
	}
	if n := len(h.s.NodeShellPods()); n != 0 {
		t.Errorf("%d pods recorded after quit", n)
	}
}

// TestNodeShellSettings: the user sets the image and a pull secret, and the confirmation says the
// image is a floating tag and names the secret, which the pod pulls with; agents and the CLI may
// only reset them; what cannot be an image or a Secret's name is refused.
func TestNodeShellSettings(t *testing.T) {
	h, conf := nodeShellHarness(t)
	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.get", nil, &view)
	if view.NodeShellDefaultImage != engine.DefaultNodeShellImage || view.NodeShellImage != "" {
		t.Errorf("defaults %+v", view)
	}
	set := map[string]any{"image": "registry.example.com/tools/shell:1.2", "imagePullSecret": "registry-pull"}
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		if err := h.run(t, origin, "cluster.settings.setNodeShell", set, nil); !errors.Is(err, engine.ErrNodeShellGUIOnly) {
			t.Errorf("%s set the node shell: %v", origin, err)
		}
	}
	h.must(t, engine.OriginGUI, "cluster.settings.setNodeShell", set, &view)
	if view.NodeShellImage != "registry.example.com/tools/shell:1.2" || view.NodeShellPullSecret != "registry-pull" {
		t.Errorf("after set %+v", view)
	}
	h.must(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, nil)
	msg := conf.last(t).Message()
	for _, want := range []string{"Image: registry.example.com/tools/shell:1.2. Digest: none: a floating tag",
		"Pull secret: registry-pull."} {
		if !strings.Contains(msg, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, msg)
		}
	}
	pod := h.writer.createdPods()[0]
	if pod.Spec.Containers[0].Image != "registry.example.com/tools/shell:1.2" ||
		len(pod.Spec.ImagePullSecrets) != 1 || pod.Spec.ImagePullSecrets[0].Name != "registry-pull" {
		t.Errorf("pod image %s, pull secrets %v", pod.Spec.Containers[0].Image, pod.Spec.ImagePullSecrets)
	}

	for _, bad := range []map[string]any{{"image": "alpine 3.24"}, {"image": "https://example.com/x"}, {"imagePullSecret": "Not_A_Name"},
		{"maxDuration": "4m"}, {"maxDuration": "25h"}, {"maxDuration": "2 hours"}, {"maxDuration": "10m30s"}} {
		if err := h.run(t, engine.OriginGUI, "cluster.settings.setNodeShell", bad, nil); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	var reset engine.ClusterSettingsView
	h.must(t, engine.OriginExternalAgent, "cluster.settings.setNodeShell", map[string]any{"image": "", "imagePullSecret": " "}, &reset)
	if reset.NodeShellImage != "" || reset.NodeShellPullSecret != "" {
		t.Errorf("an agent's reset %+v", reset)
	}
}

// TestNodeShellMaxDurationSetting: the maximum duration is the user's (owner, 2026-10-14, D-37):
// unset it is 2 hours; set to 90 minutes in the window, the confirmation names it and the pod's
// activeDeadlineSeconds, its container's sleep and the tab follow it; the CLI and agents are
// refused a value but may reset it; the bounds are 5 minutes and 24 hours.
func TestNodeShellMaxDurationSetting(t *testing.T) {
	h, conf := nodeShellHarness(t)
	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.get", nil, &view)
	if view.NodeShellMaxDuration != "" || view.NodeShellDefaultMaxDuration != "2h" {
		t.Errorf("defaults %+v", view)
	}
	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		err := h.run(t, origin, "cluster.settings.setNodeShell", map[string]any{"maxDuration": "24h"}, nil)
		if !errors.Is(err, engine.ErrNodeShellGUIOnly) || !strings.Contains(err.Error(), "maximum duration") {
			t.Errorf("%s set the maximum duration: %v", origin, err)
		}
	}
	if cs, _ := h.s.ClusterSettingsOf("test"); cs.NodeShellMaxMinutes != 0 {
		t.Fatalf("a refused origin's maximum duration was kept: %d minutes", cs.NodeShellMaxMinutes)
	}
	h.must(t, engine.OriginGUI, "cluster.settings.setNodeShell", map[string]any{"maxDuration": "90m"}, &view)
	if view.NodeShellMaxDuration != "1h30m" {
		t.Errorf("after set %+v", view)
	}
	if cs := h.s.UIState().ClusterSettings["test"]; cs.NodeShellMaxMinutes != 90 {
		t.Errorf("the UI state's settings %+v", cs)
	}

	var res commands.DockResult
	h.must(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, &res)
	if msg := conf.last(t).Message(); !strings.Contains(msg, "Ends: when the shell closes, by itself after 1 hour 30 minutes.") {
		t.Errorf("the confirmation does not name the maximum duration:\n%s", msg)
	}
	pod := h.writer.createdPods()[0]
	if d := pod.Spec.ActiveDeadlineSeconds; d == nil || *d != 5400 || !slices.Equal(pod.Spec.Containers[0].Command, []string{"sleep", "5400"}) {
		t.Errorf("deadline %v, command %v", d, pod.Spec.Containers[0].Command)
	}
	if res.Tab.NodeShell == nil || res.Tab.NodeShell.DeadlineSeconds != 5400 {
		t.Errorf("tab %+v", res.Tab.NodeShell)
	}

	for text, minutes := range map[string]int{"5m": 5, "24h": 1440, "1h": 60} {
		h.must(t, engine.OriginGUI, "cluster.settings.setNodeShell", map[string]any{"maxDuration": text}, nil)
		if cs, _ := h.s.ClusterSettingsOf("test"); cs.NodeShellMaxMinutes != minutes {
			t.Errorf("%s kept as %d minutes", text, cs.NodeShellMaxMinutes)
		}
	}
	var reset engine.ClusterSettingsView
	h.must(t, engine.OriginExternalAgent, "cluster.settings.setNodeShell", map[string]any{"maxDuration": " "}, &reset)
	if cs, _ := h.s.ClusterSettingsOf("test"); cs.NodeShellMaxMinutes != 0 || reset.NodeShellMaxDuration != "" {
		t.Errorf("an agent's reset: %d minutes, view %q", cs.NodeShellMaxMinutes, reset.NodeShellMaxDuration)
	}
}

// TestNodeShellBoundToWhatWasConfirmed: an image or a maximum duration changed while the
// confirmation was open refuses the node shell; nothing is created.
func TestNodeShellBoundToWhatWasConfirmed(t *testing.T) {
	for name, change := range map[string]func(*engine.ClusterSettings){
		"image":            func(cs *engine.ClusterSettings) { cs.NodeShellImage = "evil.example.com/shell:latest" },
		"maximum duration": func(cs *engine.ClusterSettings) { cs.NodeShellMaxMinutes = 1440 },
	} {
		h, conf := nodeShellHarness(t)
		conf.during = func(context.Context) {
			_, err := h.s.ChangeClusterSettings("test", func(cs *engine.ClusterSettings) error {
				change(cs)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}
		err := h.run(t, engine.OriginGUI, engine.NodeShellCommand, map[string]any{"ref": worker1}, nil)
		if !errors.Is(err, engine.ErrTargetsChanged) {
			t.Errorf("the %s changed under the confirmation: %v", name, err)
		}
		if pods := h.writer.createdPods(); len(pods) != 0 {
			t.Errorf("the %s changed: %d pods created", name, len(pods))
		}
	}
}

// TestNodeShellFollowsAccessReviews: a user who may not create pods in kube-system sees Shell
// greyed with the reason; a pod is not a node.
func TestNodeShellFollowsAccessReviews(t *testing.T) {
	h, _ := nodeShellHarness(t)
	h.s.Backend().Access = denyAccess{"create pods kube-system": true}
	cmd, _ := h.s.Registry().Get(engine.NodeShellCommand)
	err := cmd.Enabled(h.s, []byte(`{"ref":{"kind":"nodes","name":"worker-1"}}`))
	if !errors.Is(err, engine.ErrNotPermitted) || !strings.Contains(err.Error(), "you may not create pods in kube-system") {
		t.Errorf("denied: %v", err)
	}
	h.s.Backend().Access = denyAccess{"create pods ": true} // cluster-wide only: kube-system is what counts
	if err := cmd.Enabled(h.s, []byte(`{"ref":{"kind":"nodes","name":"worker-1"}}`)); err != nil {
		t.Errorf("allowed in kube-system: %v", err)
	}
	if !cmd.Applicable(h.s, []byte(`{"ref":{"kind":"nodes","name":"worker-1"}}`)) ||
		cmd.Applicable(h.s, []byte(`{"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}`)) {
		t.Error("node.shell applies to nodes only")
	}
}
