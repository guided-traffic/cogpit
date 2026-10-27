//go:build kind

package terminal

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// kindTestNamespace is the namespace this package's cluster tests own; it is deleted afterwards.
const kindTestNamespace = "cogpit-test-f"

// kindConfig connects to the test cluster named by COGPIT_KIND_KUBECONFIG and
// COGPIT_KIND_CONTEXT (make test-kind sets both); never to the developer's kubeconfig.
func kindConfig(t *testing.T) *rest.Config {
	t.Helper()
	path, ctxName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if path == "" || ctxName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT not set")
	}
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules,
		&clientcmd.ConfigOverrides{CurrentContext: ctxName}).ClientConfig()
	if err != nil {
		t.Fatalf("kind kubeconfig: %v", err)
	}
	return cfg
}

func TestKindPodExec(t *testing.T) {
	cfg := kindConfig(t)
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: kindTestNamespace}}
	if _, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating namespace: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.CoreV1().Namespaces().Delete(ctx, kindTestNamespace, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("deleting namespace %s: %v", kindTestNamespace, err)
		}
	})

	zero := int64(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "shell", Namespace: kindTestNamespace},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &zero,
			Containers: []corev1.Container{{
				Name:    "busybox",
				Image:   "busybox:1.37",
				Command: []string{"sleep", "3600"},
			}},
		},
	}
	if _, err := client.CoreV1().Pods(kindTestNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating pod: %v", err)
	}
	err = wait.PollUntilContextTimeout(ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		p, err := client.CoreV1().Pods(kindTestNamespace).Get(ctx, "shell", metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return p.Status.Phase == corev1.PodRunning, nil
	})
	if err != nil {
		t.Fatalf("pod not running: %v", err)
	}

	sess, err := StartPodExec(ctx, cfg, client, ExecOptions{Namespace: kindTestNamespace, Pod: "shell", Container: "busybox"})
	if err != nil {
		t.Fatalf("StartPodExec: %v", err)
	}
	defer sess.Close()
	out := record(sess)
	if err := sess.Resize(80, 24); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	start := time.Now()
	if _, err := sess.Write([]byte("echo hello-cogpit\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The TTY echoes the typed line; the command's own output is a line of its own.
	out.waitFor(t, "\nhello-cogpit\r", 30*time.Second)
	t.Logf("echo round trip (including connect): %v", time.Since(start))

	if err := sess.Resize(100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// The resize and the typed line travel on streams of their own: ask again, once the last
	// answer came, until the container's TTY has the new size.
	sizes := regexp.MustCompile(`\n(\d+ \d+)\r`)
	deadline := time.Now().Add(15 * time.Second)
	answer := func(n int) string { // stty's n-th answer, once it came
		t.Helper()
		for {
			if m := sizes.FindAllStringSubmatch(out.String(), -1); len(m) >= n {
				return m[n-1][1]
			}
			if time.Now().After(deadline) {
				t.Fatalf("stty size did not answer; output:\n%s", out.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	for asked := 1; ; asked++ {
		if _, err := sess.Write([]byte("stty size\n")); err != nil {
			t.Fatal(err)
		}
		if answer(asked) == "30 100" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the TTY kept its old size; output:\n%s", out.String())
		}
		time.Sleep(100 * time.Millisecond) // a pause between asks, not their order
	}

	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	waitClosed(t, sess.Done(), 10*time.Second, "Done")
	_ = out.waitEnd(t, 10*time.Second)
	if strings.Contains(out.String(), "[cogpit] shell ended") {
		t.Errorf("Close printed an error:\n%s", out.String())
	}
	t.Logf("output:\n%q", out.String())
}

func TestKindPodExecShowsErrors(t *testing.T) {
	cfg := kindConfig(t)
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sess, err := StartPodExec(ctx, cfg, client, ExecOptions{Namespace: kindTestNamespace, Pod: "no-such-pod"})
	if err != nil {
		t.Fatalf("StartPodExec: %v", err)
	}
	defer sess.Close()
	out := record(sess)
	_ = out.waitEnd(t, 30*time.Second)
	if !strings.Contains(out.String(), "[cogpit] shell ended: ") {
		t.Errorf("output = %q, want the error", out.String())
	}
	t.Logf("output: %q", out.String())
}
