//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const debugNamespace = "cogpit-debug"

// ephemeralState is the state of a pod's ephemeral container as the API server reports it, nil
// without one.
func ephemeralState(ctx context.Context, kube kubernetes.Interface, pod, name string) *corev1.ContainerState {
	p, err := kube.CoreV1().Pods(debugNamespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	for _, st := range p.Status.EphemeralContainerStatuses {
		if st.Name == name {
			return &st.State
		}
	}
	return nil
}

// TestKindDebugContainer adds a debug container to a running pod through the engine (D-102): the
// confirmation names the place, the target and Cogpit's default image (nicolaka/netshoot pinned by
// digest, which Renovate keeps current, so CI runs every new one here); the API server takes the
// patch on the ephemeralcontainers subresource; the Attach tab waits for the container to start
// and is its shell, which sees the target's processes; once the shell exits the container stays
// Terminated and its log is read through Logs; a patch with another pod's UID is refused
// (cluster.ErrReplaced). Everything is in the namespace cogpit-debug, deleted at the end.
func TestKindDebugContainer(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	var (
		mu    sync.Mutex
		asked []engine.ConfirmRequest
	)
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, req)
			return true, nil
		}),
		Preferences: engine.NewPreferencesStore(""),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	kube := s.Backend().Conn.Kube()
	kindNamespace(ctx, t, kube, debugNamespace)

	// The target: a pod whose one container sleeps, from the node shell's pinned Alpine (on the node
	// already when TestKindNodeShell ran).
	sleeper := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: debugNamespace},
		Spec: corev1.PodSpec{
			Containers:                    []corev1.Container{{Name: "main", Image: engine.DefaultNodeShellImage, Command: []string{"sleep", "3600"}}},
			TerminationGracePeriodSeconds: new(int64),
		},
	}
	if _, err := kube.CoreV1().Pods(debugNamespace).Create(ctx, sleeper, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Backend().Conn.WaitPodRunning(ctx, debugNamespace, "target", "main"); err != nil {
		t.Fatalf("the target pod: %v", err)
	}
	if _, err := s.Objects(ctx, "pods", nil); err != nil { // the Pods view's cache, which Enabled reads
		t.Fatal(err)
	}
	ref := model.ObjectRef{Kind: "pods", Namespace: debugNamespace, Name: "target"}
	if !testkit.WaitFor(ctx, func() bool { o, err := s.Lookup(ref); return err == nil && engine.PodEnded(o) == "" && o != nil }) {
		t.Fatal("the target is not in the cache")
	}

	start := time.Now()
	raw, _ := json.Marshal(map[string]any{"ref": ref, "target": "main"})
	res, err := s.Dispatch(ctx, engine.Invocation{ID: engine.DebugCommand, Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("pod.debug: %v", err)
	}
	debug := res.(commands.DebugResult)
	name := debug.Container
	mu.Lock()
	if len(asked) != 1 || asked[0].Danger != engine.DangerDestructive ||
		!strings.Contains(asked[0].Message(), "Target: main. Sees: main's processes and files. Image: docker.io/nicolaka/netshoot:") {
		t.Errorf("confirmation %+v", asked)
	}
	mu.Unlock()
	if debug.Tab == nil || debug.Tab.Container != name || debug.Image != engine.DefaultDebugImage {
		t.Fatalf("result %+v", debug)
	}
	p, err := kube.CoreV1().Pods(debugNamespace).Get(ctx, "target", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.Spec.EphemeralContainers); n != 1 {
		t.Fatalf("%d ephemeral containers", n)
	}
	if ec := p.Spec.EphemeralContainers[0]; ec.Name != name || ec.Image != engine.DefaultDebugImage || !ec.Stdin || !ec.TTY ||
		ec.TargetContainerName != "main" || ec.TerminationMessagePolicy != corev1.TerminationMessageReadFile {
		t.Errorf("the API server holds %+v", ec)
	}

	// The Attach tab: it waits while the image is pulled, then is the debug container's shell, which
	// sees the target's sleep.
	sess, err := s.OpenTerminal(ctx, debug.Tab.ID)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	out := collectShell(sess)
	if !testkit.WaitFor(ctx, func() bool { return strings.Contains(out.String(), "[cogpit] attached to target/"+name) }) {
		t.Fatalf("not attached: %q", out.String())
	}
	t.Logf("attached %s after the debug's dispatch", time.Since(start).Round(time.Second))
	// Enter until the prompt answers: the shell may not have read the first keystrokes yet.
	answered := func(want string) bool {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return testkit.WaitFor(wctx, func() bool { return strings.Contains(out.String(), want) })
	}
	for i := 0; ; i++ {
		if _, err := io.WriteString(sess, "echo DEBUG-$((40+2))-OK PROC-$(ps -o args | grep -c '[s]leep 3600')-END\r"); err != nil {
			t.Fatal(err)
		}
		if answered("DEBUG-42-OK PROC-1-END") {
			break
		}
		if i == 5 {
			t.Fatalf("the debug shell did not answer, or does not see the target's process:\n%q", out.String())
		}
	}
	if _, err := io.WriteString(sess, "exit\r"); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		st := ephemeralState(ctx, kube, "target", name)
		return st != nil && st.Terminated != nil
	}) {
		t.Fatalf("the debug container did not end after exit")
	}
	_ = sess.Close()
	st := ephemeralState(ctx, kube, "target", name)
	t.Logf("after exit: %+v", st.Terminated)

	// The finished container's log, through Logs: what its shell printed.
	raw, _ = json.Marshal(map[string]any{"ref": ref, "container": name})
	lres, err := s.Dispatch(ctx, engine.Invocation{ID: "pod.logs", Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("pod.logs: %v", err)
	}
	logs, err := s.OpenLogs(ctx, lres.(commands.DockResult).Tab.ID)
	if err != nil {
		t.Fatalf("OpenLogs: %v", err)
	}
	logOut := collectShell(logs)
	if !testkit.WaitFor(ctx, func() bool { return strings.Contains(logOut.String(), "DEBUG-42-OK PROC-1-END") }) {
		t.Errorf("the finished debug container's log lacks its output:\n%q", logOut.String())
	}
	_ = logs.Close()

	// The finished container stays; the pod and its other container run on.
	if p, err = kube.CoreV1().Pods(debugNamespace).Get(ctx, "target", metav1.GetOptions{}); err != nil ||
		len(p.Spec.EphemeralContainers) != 1 || p.Status.Phase != corev1.PodRunning {
		t.Errorf("after the debug: %v, %+v", err, p.Status)
	}

	// Another pod's UID: the API server refuses the patch (metadata.uid immutable).
	err = s.Backend().Conn.AddEphemeralContainer(ctx, debugNamespace, "target", "00000000-0000-0000-0000-000000000000",
		engine.DebugContainer("debugger-wrong", engine.DefaultNodeShellImage, ""))
	if !errors.Is(err, cluster.ErrReplaced) {
		t.Errorf("a patch with another UID: %v", err)
	}

	// Pod Security admission checks an ephemeral container too: in a namespace that enforces
	// restricted, the user's Debug is refused with the API server's message and nothing is added.
	nsObj, err := kube.CoreV1().Namespaces().Get(ctx, debugNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nsObj.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
	if _, err := kube.CoreV1().Namespaces().Update(ctx, nsObj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"ref": ref, "target": "main"})
	_, err = s.Dispatch(ctx, engine.Invocation{ID: engine.DebugCommand, Params: raw, Origin: engine.OriginGUI})
	if err == nil || !strings.Contains(err.Error(), "violates PodSecurity") {
		t.Errorf("under Pod Security restricted: %v", err)
	}
	t.Logf("under Pod Security restricted: %v", err)
	if p, err = kube.CoreV1().Pods(debugNamespace).Get(ctx, "target", metav1.GetOptions{}); err != nil || len(p.Spec.EphemeralContainers) != 1 {
		t.Errorf("a refused debug added a container: %v, %d", err, len(p.Spec.EphemeralContainers))
	}
	if nsObj, err = kube.CoreV1().Namespaces().Get(ctx, debugNamespace, metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	delete(nsObj.Labels, "pod-security.kubernetes.io/enforce")
	if _, err := kube.CoreV1().Namespaces().Update(ctx, nsObj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	// A pod that has ended: Debug is greyed with the reason; what the API server itself says to the
	// patch is logged for the docs.
	finished := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "finished", Namespace: debugNamespace},
		Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{Name: "main", Image: engine.DefaultNodeShellImage, Command: []string{"true"}}}},
	}
	created, err := kube.CoreV1().Pods(debugNamespace).Create(ctx, finished, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fref := model.ObjectRef{Kind: "pods", Namespace: debugNamespace, Name: "finished"}
	if !testkit.WaitFor(ctx, func() bool { o, err := s.Lookup(fref); return err == nil && engine.PodEnded(o) != "" }) {
		t.Fatal("the finished pod did not end in the cache")
	}
	cmd, _ := s.Registry().Get(engine.DebugCommand)
	fraw, _ := json.Marshal(map[string]any{"ref": fref})
	if err := cmd.Enabled(s, fraw); err == nil || !strings.Contains(err.Error(), "the pod has ended (succeeded)") {
		t.Errorf("Debug on a finished pod: %v", err)
	}
	err = s.Backend().Conn.AddEphemeralContainer(ctx, debugNamespace, "finished", string(created.UID),
		engine.DebugContainer("debugger-late", engine.DefaultNodeShellImage, ""))
	t.Logf("the API server on a finished pod: %v", err)
	t.Logf("output:\n%q", out.String())
}
