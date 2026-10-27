package cluster

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

// shellPod is a node-shell pod in the given phase, its container in state.
func shellPod(phase corev1.PodPhase, state corev1.ContainerState) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cogpit-node-shell-x", ResourceVersion: "7"},
		Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", State: state}}},
	}
}

var (
	waitingCreating = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}
	runningState    = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
)

// watchedConn is a fake Conn whose pod watches are handed out from watchers, in order, and whose
// watch requests are recorded.
func watchedConn(t *testing.T, pod *corev1.Pod, watchers ...*watch.FakeWatcher) (*Conn, func() []k8stesting.WatchActionImpl) {
	t.Helper()
	c, cs := fakeConn(pod)
	var mu sync.Mutex
	var seen []k8stesting.WatchActionImpl
	cs.PrependWatchReactor("pods", func(a k8stesting.Action) (bool, watch.Interface, error) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, a.(k8stesting.WatchActionImpl))
		if len(seen) > len(watchers) {
			return true, watch.NewEmptyWatch(), nil
		}
		return true, watchers[len(seen)-1], nil
	})
	return c, func() []k8stesting.WatchActionImpl {
		mu.Lock()
		defer mu.Unlock()
		return append([]k8stesting.WatchActionImpl(nil), seen...)
	}
}

// waitAsync runs WaitPodRunning and returns its outcome on a channel.
func waitAsync(ctx context.Context, c *Conn) <-chan error {
	out := make(chan error, 1)
	go func() { out <- c.WaitPodRunning(ctx, "kube-system", "cogpit-node-shell-x", "shell") }()
	return out
}

func waitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("WaitPodRunning did not return")
		return nil
	}
}

// TestWaitPodRunningWatchesThePod: the wait reads the pod once, then watches that pod alone from
// the version it read, and returns once its container runs.
func TestWaitPodRunningWatchesThePod(t *testing.T) {
	w := watch.NewFakeWithChanSize(4, false)
	c, seen := watchedConn(t, shellPod(corev1.PodPending, waitingCreating), w)
	ch := waitAsync(context.Background(), c)
	w.Modify(shellPod(corev1.PodPending, waitingCreating))
	w.Modify(shellPod(corev1.PodRunning, runningState))
	if err := waitResult(t, ch); err != nil {
		t.Fatalf("WaitPodRunning: %v", err)
	}
	got := seen()
	if len(got) != 1 {
		t.Fatalf("%d watches, want 1", len(got))
	}
	r := got[0].GetWatchRestrictions()
	if r.Fields.String() != "metadata.name=cogpit-node-shell-x" || r.ResourceVersion != "7" || got[0].GetNamespace() != "kube-system" {
		t.Errorf("watch %q from %q in %s", r.Fields.String(), r.ResourceVersion, got[0].GetNamespace())
	}
}

// TestWaitPodRunningSaysWhyNot: an image that cannot be pulled, a pod that failed and a deleted pod
// end the wait with the reason; a running pod needs no watch.
func TestWaitPodRunningSaysWhyNot(t *testing.T) {
	pullFailed := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "Back-off pulling image"}}
	failed := shellPod(corev1.PodFailed, waitingCreating)
	failed.Status.Reason = "DeadlineExceeded"
	for _, tt := range []struct {
		what string
		send func(*watch.FakeWatcher)
		want string
	}{
		{"pull", func(w *watch.FakeWatcher) { w.Modify(shellPod(corev1.PodPending, pullFailed)) },
			"the pod did not start: its container is waiting: ImagePullBackOff (Back-off pulling image)"},
		{"failed", func(w *watch.FakeWatcher) { w.Modify(failed) }, "the pod did not start: it is Failed: DeadlineExceeded"},
		{"deleted", func(w *watch.FakeWatcher) { w.Delete(shellPod(corev1.PodPending, waitingCreating)) }, "the pod did not start: it was deleted"},
	} {
		w := watch.NewFakeWithChanSize(2, false)
		c, _ := watchedConn(t, shellPod(corev1.PodPending, waitingCreating), w)
		ch := waitAsync(context.Background(), c)
		tt.send(w)
		err := waitResult(t, ch)
		if !errors.Is(err, ErrPodNotStarted) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", tt.what, err, tt.want)
		}
	}

	c, seen := watchedConn(t, shellPod(corev1.PodRunning, runningState))
	if err := c.WaitPodRunning(context.Background(), "kube-system", "cogpit-node-shell-x", "shell"); err != nil || len(seen()) != 0 {
		t.Errorf("a running pod: %v, %d watches", err, len(seen()))
	}
}

// TestWaitPodRunningReadsAgainAfterTheWatchEnds: a watch the server closes is followed by another
// get and watch; the context ends the wait.
func TestWaitPodRunningReadsAgainAfterTheWatchEnds(t *testing.T) {
	first := watch.NewFakeWithChanSize(1, false)
	pod := shellPod(corev1.PodPending, waitingCreating)
	c, seen := watchedConn(t, pod, first, watch.NewFake())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := waitAsync(ctx, c)
	first.Modify(shellPod(corev1.PodPending, waitingCreating))
	first.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for len(seen()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(seen()); n != 2 {
		t.Fatalf("%d watches after the first ended, want 2", n)
	}
	cancel()
	if err := waitResult(t, ch); !errors.Is(err, context.Canceled) {
		t.Errorf("after cancel: %v", err)
	}
}

// TestGetPodReadsOnePod: the node shell journal's sweep reads one pod by name (H-71); a pod that is
// not there is a NotFound the caller can tell.
func TestGetPodReadsOnePod(t *testing.T) {
	pod := shellPod(corev1.PodRunning, runningState)
	pod.UID, pod.Annotations = "uid-shell", map[string]string{"cogpit.io/node-shell-id": "id-1"}
	c, cs := fakeConn(pod)
	got, err := c.GetPod(context.Background(), "kube-system", "cogpit-node-shell-x")
	if err != nil || got.UID != "uid-shell" || got.Annotations["cogpit.io/node-shell-id"] != "id-1" {
		t.Fatalf("GetPod: %v, %v", got, err)
	}
	for _, a := range cs.Actions() {
		if a.GetVerb() != "get" || a.GetNamespace() != "kube-system" {
			t.Errorf("request %s %s in %s", a.GetVerb(), a.GetResource().Resource, a.GetNamespace())
		}
	}
	if _, err := c.GetPod(context.Background(), "kube-system", "gone"); !apierrors.IsNotFound(err) {
		t.Errorf("a missing pod: %v", err)
	}
}
