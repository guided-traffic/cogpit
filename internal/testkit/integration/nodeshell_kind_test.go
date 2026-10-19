//go:build kind

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// shellOutput collects a session's output for waiting on.
type shellOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	done chan struct{}
}

func collectShell(r io.Reader) *shellOutput {
	o := &shellOutput{done: make(chan struct{})}
	go func() {
		defer close(o.done)
		p := make([]byte, 4096)
		for {
			n, err := r.Read(p)
			o.mu.Lock()
			o.buf.Write(p[:n])
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

func (o *shellOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// TestKindNodeShell opens a node shell on the kind cluster's node through the engine (G7, G8;
// D-37): node.shell, confirmed, creates the privileged pod in kube-system from Cogpit's default
// image (the pinned Alpine release Renovate keeps current, so CI runs every new one here) with the
// maximum duration the user set (30 minutes: activeDeadlineSeconds and the container's sleep);
// the tab's session waits for it to run and enters the node with nsenter, so `hostname` answers
// the node's name (a kind node is a container whose PID 1 is its systemd); closing the session
// deletes the pod. The pod is the one thing it creates, in kube-system by design, and it is
// deleted in any case; the settings are kept in memory.
func TestKindNodeShell(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	var asked []engine.ConfirmRequest
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
			asked = append(asked, req)
			return true, nil
		}),
		ClusterSettings: engine.NewClusterSettingsStore(""),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	kube := s.Backend().Conn.Kube()
	nodes, err := s.Objects(ctx, "nodes", nil) // the Nodes view's cache, which Enabled reads
	if err != nil || len(nodes) == 0 {
		t.Fatalf("nodes: %v, %d", err, len(nodes))
	}
	node := nodes[0].GetName()
	setting, _ := json.Marshal(map[string]any{"context": contextName, "maxDuration": "30m"})
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.settings.setNodeShell", Params: setting, Origin: engine.OriginGUI}); err != nil {
		t.Fatalf("the maximum duration: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: "nodes", Name: node}})
	res, err := s.Dispatch(ctx, engine.Invocation{ID: engine.NodeShellCommand, Params: raw, Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("node.shell: %v", err)
	}
	tab := res.(commands.DockResult).Tab
	pod := tab.NodeShell.Pod
	t.Cleanup(func() { // whatever happened: the pod goes
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = kube.CoreV1().Pods(engine.NodeShellNamespace).Delete(cctx, pod, metav1.DeleteOptions{})
	})
	if len(asked) != 1 || !strings.Contains(asked[0].Message(), "privileged pod in namespace kube-system on node "+node) ||
		!strings.Contains(asked[0].Message(), "Ends: when the shell closes, by itself after 30 minutes.") {
		t.Errorf("confirmation %+v", asked)
	}
	created, err := kube.CoreV1().Pods(engine.NodeShellNamespace).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the node shell's pod: %v", err)
	}
	if created.Spec.NodeName != node || !created.Spec.HostPID || created.Spec.ActiveDeadlineSeconds == nil ||
		*created.Spec.ActiveDeadlineSeconds != 1800 || created.Labels["app.kubernetes.io/managed-by"] != "cogpit" {
		t.Errorf("pod %+v", created.Spec)
	}
	if c := created.Spec.Containers[0]; c.Image != engine.DefaultNodeShellImage || strings.Join(c.Command, " ") != "sleep 1800" {
		t.Errorf("container: image %s, command %v", c.Image, c.Command)
	}

	sess, err := s.OpenTerminal(ctx, tab.ID)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	out := collectShell(sess)
	if !testkit.WaitFor(ctx, func() bool { return strings.Contains(out.String(), "Waiting for the pod to run") }) {
		t.Fatalf("no notice: %q", out.String())
	}
	if _, err := io.WriteString(sess, "echo ON-$(hostname)-DONE\r"); err != nil {
		t.Fatal(err)
	}
	want := "ON-" + node + "-DONE"
	wctx, wcancel := context.WithTimeout(ctx, 3*time.Minute) // the image pull on a fresh node
	defer wcancel()
	if !testkit.WaitFor(wctx, func() bool {
		return strings.Count(out.String(), want) > 0 && !strings.Contains(out.String(), "node shell ended")
	}) {
		t.Fatalf("the node shell did not answer with the node's name %s:\n%q", node, out.String())
	}
	_ = sess.Close()
	gone := testkit.WaitFor(ctx, func() bool {
		_, err := kube.CoreV1().Pods(engine.NodeShellNamespace).Get(ctx, pod, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	})
	if !gone {
		t.Errorf("the pod %s is still there after the shell closed", pod)
	}
	if n := len(s.NodeShellPods()); n != 0 {
		t.Errorf("%d node shells recorded after close", n)
	}
	t.Logf("output:\n%q", out.String())
}
