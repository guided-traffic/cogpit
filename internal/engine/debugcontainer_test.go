package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// debugPod is shop/web-0 with an ephemeral container "debugger" in the given status (nil: none
// reported yet).
func debugPod(status map[string]any) *unstructured.Unstructured {
	pod := testkit.Pod("shop", "web-0")
	pod.Object["spec"].(map[string]any)["ephemeralContainers"] = []any{
		map[string]any{"name": "debugger", "image": DefaultDebugImage, "stdin": true, "tty": true, "targetContainerName": "app"},
	}
	if status != nil {
		st := pod.Object["status"].(map[string]any)
		st["ephemeralContainerStatuses"] = []any{map[string]any{"name": "debugger", "state": status}}
	}
	return pod
}

// TestAttachOptionsOfAnEphemeralContainer: an ephemeral container's stdin and TTY come from
// spec.ephemeralContainers, as kubectl debug -it set them (D-102).
func TestAttachOptionsOfAnEphemeralContainer(t *testing.T) {
	got := AttachOptions(debugPod(nil), "debugger")
	want := terminal.AttachOptions{Namespace: "shop", Pod: "web-0", Container: "debugger", Stdin: true, TTY: true}
	if got != want {
		t.Errorf("%+v, want %+v", got, want)
	}
	if names := ContainerNames(debugPod(nil), true); strings.Join(names, ",") != "app,debugger" {
		t.Errorf("every container %v", names)
	}
	if names := ContainerNames(debugPod(nil), false); strings.Join(names, ",") != "app" {
		t.Errorf("the containers %v", names)
	}
}

// TestContainerStarting: an attach waits for a container without a status or waiting, not for one
// that runs or ended, nor for a name the pod does not have.
func TestContainerStarting(t *testing.T) {
	for name, c := range map[string]struct {
		pod       *unstructured.Unstructured
		container string
		want      bool
	}{
		"no status":  {debugPod(nil), "debugger", true},
		"waiting":    {debugPod(map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}), "debugger", true},
		"running":    {debugPod(map[string]any{"running": map[string]any{}}), "debugger", false},
		"terminated": {debugPod(map[string]any{"terminated": map[string]any{"exitCode": int64(0)}}), "debugger", false},
		"regular":    {debugPod(nil), "app", false},
		"unknown":    {debugPod(nil), "gone", false},
	} {
		if got := containerStarting(c.pod, c.container); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// TestWaitContainerStarted: the attach's wait follows the pod's watch to the container's start; an
// image that cannot be pulled, a container that ended and a pod deleted end it at once with the
// reason.
func TestWaitContainerStarted(t *testing.T) {
	ref := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	run := func(t *testing.T, next map[string]any, deleted bool) error {
		t.Helper()
		src := testkit.NewMemorySource()
		src.Set(testkit.PodsGVR, debugPod(map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}))
		s, _ := newTestSession(t, src, 5*time.Millisecond)
		connectTest(t, s)
		done := make(chan error, 1)
		go func() { done <- s.waitContainerStarted(context.Background(), ref, "uid-shop-web-0", "debugger") }()
		time.Sleep(50 * time.Millisecond) // the wait is on the watch now
		select {
		case err := <-done:
			t.Fatalf("returned before the container started: %v", err)
		default:
		}
		if deleted {
			src.Delete(testkit.PodsGVR, "shop", "web-0")
		} else {
			src.Set(testkit.PodsGVR, debugPod(next))
		}
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("the wait did not follow the watch")
		}
		return nil
	}
	if err := run(t, map[string]any{"running": map[string]any{}}, false); err != nil {
		t.Errorf("started: %v", err)
	}
	err := run(t, map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff", "message": "Back-off pulling image"}}, false)
	if err == nil || err.Error() != "debugger does not start: ImagePullBackOff: Back-off pulling image" {
		t.Errorf("image pull: %v", err)
	}
	if err := run(t, map[string]any{"terminated": map[string]any{"exitCode": int64(1)}}, false); err == nil ||
		!strings.Contains(err.Error(), "ended before it could be attached to") {
		t.Errorf("ended: %v", err)
	}
	if err := run(t, nil, true); !errors.Is(err, errPodGone) {
		t.Errorf("deleted: %v", err)
	}
}

// TestDebugContainerNamesAndEffects: a name as kubectl debug makes one, never one taken; the
// confirmation's rows in the owner's order, a floating tag named as such.
func TestDebugContainerNamesAndEffects(t *testing.T) {
	taken := []string{"app"}
	for range 50 {
		n := DebugContainerName(taken)
		if !strings.HasPrefix(n, "debugger-") || len(n) != len("debugger-")+5 || n != strings.ToLower(n) {
			t.Fatalf("name %q", n)
		}
		for _, o := range taken {
			if o == n {
				t.Fatalf("name %q taken", n)
			}
		}
		taken = append(taken, n)
	}
	if DefaultDebugImageName != "nicolaka/netshoot:v0.16" || !ImagePinned(DefaultDebugImage) {
		t.Errorf("default %q, pinned %v", DefaultDebugImageName, ImagePinned(DefaultDebugImage))
	}
	got := DebugEffects("", "busybox:1.37")
	want := []Effect{{Name: "Target", Value: "none"}, {Name: "Image", Value: "busybox:1.37", Tone: EffectNames},
		{Name: "Digest", Value: "none: a floating tag, what it runs can change without Cogpit knowing", Tone: EffectWarning}}
	if len(got) != len(want) {
		t.Fatalf("effects %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("effect %d %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, image := range []string{"a b", "https://x/y", strings.Repeat("x", 513), "x\ty"} {
		if CheckDebugImage(image) == nil {
			t.Errorf("%q passed", image)
		}
	}
	if err := CheckDebugImage(""); err != nil {
		t.Errorf("empty (the default): %v", err)
	}
}

// TestPodEnded: a pod that ended or is being deleted is no pod to debug.
func TestPodEnded(t *testing.T) {
	pod := testkit.Pod("shop", "web-0")
	if why := PodEnded(pod); why != "" {
		t.Errorf("running: %q", why)
	}
	pod.Object["status"].(map[string]any)["phase"] = "Failed"
	if why := PodEnded(pod); why != "the pod has ended (failed)" {
		t.Errorf("failed: %q", why)
	}
	created := pod.GetCreationTimestamp()
	pod.SetDeletionTimestamp(&created)
	if why := PodEnded(pod); why != "the pod is being deleted" {
		t.Errorf("deleting: %q", why)
	}
}
