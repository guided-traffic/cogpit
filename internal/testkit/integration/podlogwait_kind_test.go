//go:build kind

package integration_test

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestKindPodLogWaitsForInit opens a pod's log during its init container against a real kubelet:
// the tab says the container waits and shows the log once it runs, in its own namespace cogpit-w.
func TestKindPodLogWaitsForInit(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const ns = "cogpit-w"
	kube := conn.Kube()
	freshNamespace(ctx, t, kube, ns)

	spec := podTemplate("init-wait", nil).Spec
	gate := sleeper("gate")
	gate.Command = []string{"sh", "-c", "until [ -e /tmp/go ]; do sleep 0.2; done"}
	spec.InitContainers = []corev1.Container{gate}
	spec.Containers = []corev1.Container{talker("app")}
	if _, err := kube.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "init-wait"}, Spec: spec}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}}, Confirmer: &recordingConfirmer{}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	d := dispatcher{t: t, ctx: ctx, s: s}
	ref := model.ObjectRef{Kind: "pods", Namespace: ns, Name: "init-wait"}
	d.cached(ref, nil)
	waitFor(ctx, t, "the init container running", func() bool {
		pod, err := kube.CoreV1().Pods(ns).Get(ctx, "init-wait", metav1.GetOptions{})
		return err == nil && len(pod.Status.InitContainerStatuses) == 1 && pod.Status.InitContainerStatuses[0].State.Running != nil
	})

	var tab commands.DockResult
	d.must("pod.logs", map[string]any{"ref": ref, "container": "app"}, &tab)
	stream, err := s.OpenLogs(ctx, tab.Tab.ID)
	if err != nil {
		t.Fatalf("the log of a container that waits for its init container: %v", err)
	}
	defer func() { _ = stream.Close() }()
	log := collect(stream)
	log.waitLines(ctx, t, "the notice", func(got []string) bool {
		return slices.ContainsFunc(got, func(l string) bool {
			return strings.HasPrefix(l, engine.NoticePrefix) && strings.Contains(l, "is waiting to start")
		})
	})

	req := kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name("init-wait").SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{Container: "gate", Command: []string{"touch", "/tmp/go"}, Stdout: true}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(conn.RESTConfig(), "POST", req.URL())
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: io.Discard}); err != nil {
		t.Fatalf("opening the init container's gate: %v", err)
	}
	log.waitLines(ctx, t, "the container's lines", func(got []string) bool {
		return slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, "from init-wait") })
	})
}
