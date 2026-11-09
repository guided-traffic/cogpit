package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

const startsNotice = `; its log shows here when it starts`

// initializingPod is a pod whose container app waits for its init container.
func initializingPod(name, reason string) *unstructured.Unstructured {
	pod := logPod("shop", name, "app:waiting")
	st := model.NestedSlice(pod, "status", "containerStatuses")[0].(map[string]any)
	st["state"] = map[string]any{"waiting": map[string]any{"reason": reason}}
	return pod
}

// openPodTab adds a logs tab on a pod's container, as pod.logs does, and returns its id.
func openPodTab(s *Session, pod, container string, opts LogOptions) string {
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: pod}
	tab := DockTab{ID: s.NewDockID(DockLogs), Type: DockLogs, Context: "test", Ref: &ref, Container: container, Logs: opts}
	s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, tab) })
	return tab.ID
}

// waitEnd waits until a stream has ended.
func waitEnd(t *testing.T, lr *lineReader) {
	t.Helper()
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the stream did not end; lines %q", lr.all())
	}
}

// TestPodLogWaitsForItsContainer: a tab opened during the pod's initialization says so, asks the
// API server for nothing, and shows the log once the cache shows the container started. Each
// reason the container waits for shows once.
func TestPodLogWaitsForItsContainer(t *testing.T) {
	s, src := logsSession(t)
	src.Set(testkit.PodsGVR, initializingPod("init-0", "PodInitializing"))
	f := newFakeLogs()
	f.content["init-0/app"] = "first line\n"
	tabID := openPodTab(s, "init-0", "app", LogOptions{})
	waitPodCached(t, s, "init-0")
	lr := readLines(openTabLogs(t, s, tabID, f))
	first := NoticePrefix + ` container "app" is waiting to start: PodInitializing` + startsNotice
	lr.wait(t, first)

	src.Set(testkit.PodsGVR, initializingPod("init-0", "ContainerCreating"))
	second := NoticePrefix + ` container "app" is waiting to start: ContainerCreating` + startsNotice
	lr.wait(t, second)
	src.Set(testkit.PodsGVR, initializingPod("init-0", "PodInitializing"))
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d log requests while the container waits: %+v", n, f.requests())
	}

	src.Set(testkit.PodsGVR, logPod("shop", "init-0", "app"))
	lr.wait(t, "first line")
	if got := lr.all(); !slices.Equal(got, []string{first, second, "first line"}) {
		t.Errorf("lines %q", got)
	}
	f.line(t, "init-0/app", "second line")
	lr.wait(t, "second line")
}

// waitPodCached waits until the cache holds the pod, as a Pods view that shows it does.
func waitPodCached(t *testing.T, s *Session, pod string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		_, err := s.Lookup(model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: pod})
		return err == nil
	}) {
		t.Fatalf("the cache does not show pod %s", pod)
	}
}

// TestPodLogWaitsWhenTheServerSaysSo: a cache that still shows the container running does not end
// the tab: the API server's "is waiting to start" counts, and the log is asked for again.
func TestPodLogWaitsWhenTheServerSaysSo(t *testing.T) {
	fastResumes(t, 100)
	s, _ := logsSession(t)
	f := newFakeLogs()
	f.mu.Lock()
	f.fail["loner/app"] = errors.New(`logs of shop/loner: container "app" in pod "loner" is waiting to start: PodInitializing`)
	f.content["loner/app"] = "started\n"
	f.mu.Unlock()
	tabID := openPodTab(s, "loner", "app", LogOptions{})
	lr := readLines(openTabLogs(t, s, tabID, f))
	lr.wait(t, NoticePrefix+` container "app" is waiting to start`+startsNotice)
	f.mu.Lock()
	delete(f.fail, "loner/app")
	f.mu.Unlock()
	lr.wait(t, "started")
}

// TestPodLogWaitGivesUp: the server's answer ends the tab when it does not go on saying the
// container is about to start, or after a few tries when the cache says it has.
func TestPodLogWaitGivesUp(t *testing.T) {
	fastResumes(t, 2)
	s, _ := logsSession(t)
	f := newFakeLogs()
	waiting := `logs of shop/loner: container "app" in pod "loner" is waiting to start: PodInitializing`
	f.mu.Lock()
	f.fail["loner/app"] = errors.New(waiting)
	f.mu.Unlock()
	lr := readLines(openTabLogs(t, s, openPodTab(s, "loner", "app", LogOptions{}), f))
	waitEnd(t, lr)
	got := lr.all()
	if want := NoticePrefix + " " + couldNotOpenLogs + waiting; len(got) != 2 || got[1] != want {
		t.Errorf("lines %q, want the notice and %q", got, want)
	}
}

// TestPodLogWaitEnds: a pod that goes away, or fails before its container started, ends the wait
// with a line that says so; closing the tab ends it too.
func TestPodLogWaitEnds(t *testing.T) {
	s, src := logsSession(t)
	for _, name := range []string{"gone", "failed", "closed"} {
		src.Set(testkit.PodsGVR, initializingPod(name, "PodInitializing"))
		waitPodCached(t, s, name)
	}
	waitingLine := NoticePrefix + ` container "app" is waiting to start: PodInitializing` + startsNotice
	f := newFakeLogs()

	lr := readLines(openTabLogs(t, s, openPodTab(s, "gone", "app", LogOptions{}), f))
	lr.wait(t, waitingLine)
	src.Delete(testkit.PodsGVR, "shop", "gone")
	lr.wait(t, NoticePrefix+" the pod no longer exists; its log ends here")
	waitEnd(t, lr)

	refused := `container "app" in pod "failed" is waiting to start: PodInitializing`
	f.mu.Lock()
	f.fail["failed/app"] = errors.New(refused)
	f.mu.Unlock()
	lr = readLines(openTabLogs(t, s, openPodTab(s, "failed", "app", LogOptions{}), f))
	lr.wait(t, waitingLine)
	pod := initializingPod("failed", "PodInitializing")
	pod.Object["status"].(map[string]any)["phase"] = "Failed"
	src.Set(testkit.PodsGVR, pod)
	lr.wait(t, NoticePrefix+" "+couldNotOpenLogs+refused)
	waitEnd(t, lr)

	rc := openTabLogs(t, s, openPodTab(s, "closed", "app", LogOptions{}), f)
	lr = readLines(rc)
	lr.wait(t, waitingLine)
	_ = rc.Close()
	waitEnd(t, lr)
}

// TestPreviousPodLogDoesNotWait: the previous container's log is there or it is not, and its error
// shows as before.
func TestPreviousPodLogDoesNotWait(t *testing.T) {
	s, src := logsSession(t)
	src.Set(testkit.PodsGVR, initializingPod("init-0", "PodInitializing"))
	waitPodCached(t, s, "init-0")
	f := newFakeLogs()
	refused := errors.New(`previous terminated container "app" in pod "init-0" not found`)
	f.mu.Lock()
	f.fail["init-0/app"] = refused
	f.mu.Unlock()
	tab, b, err := s.streamTab(openPodTab(s, "init-0", "app", LogOptions{Previous: true}), DockLogs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.openLogs(context.Background(), tab, b, f); !errors.Is(err, refused) {
		t.Errorf("error %v, want %v", err, refused)
	}
}

// TestPodContainerWaiting: which states of a cached pod have no log yet.
func TestPodContainerWaiting(t *testing.T) {
	withStatus := func(list string, status map[string]any) *unstructured.Unstructured {
		pod := logPod("shop", "p")
		pod.Object["spec"] = map[string]any{"initContainers": []any{map[string]any{"name": "app"}}}
		pod.Object["status"] = map[string]any{"phase": "Pending", list: []any{status}}
		return pod
	}
	waitingState := map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}}
	unscheduled := logPod("shop", "p")
	unscheduled.Object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "app"}}}
	unscheduled.Object["status"] = map[string]any{"phase": "Pending"}
	failed := withStatus("initContainerStatuses", map[string]any{"name": "app", "state": waitingState})
	failed.Object["status"].(map[string]any)["phase"] = "Failed"
	for _, c := range []struct {
		name, container, reason string
		pod                     *unstructured.Unstructured
		waiting                 bool
	}{
		{"waiting with a reason", "app", "ContainerCreating", withStatus("initContainerStatuses", map[string]any{"name": "app", "state": waitingState}), true},
		{"no status yet", "app", "", unscheduled, true},
		{"running", "app", "", logPod("shop", "p", "app"), false},
		{"terminated", "app", "", logPod("shop", "p", "app:terminated"), false},
		{"a crash loop has its last instance's log", "app", "", logPod("shop", "p", "app:waiting#1"), false},
		{"the pod ended", "app", "", failed, false},
		{"no such container", "other", "", logPod("shop", "p", "app"), false},
		{"no container named", "", "", logPod("shop", "p", "app:waiting"), false},
	} {
		reason, waiting := podContainerWaiting(c.pod, c.container)
		if reason != c.reason || waiting != c.waiting {
			t.Errorf("%s: %q, %v; want %q, %v", c.name, reason, waiting, c.reason, c.waiting)
		}
	}
}

// A reason from the cluster is one line whatever it holds.
func TestPodLogWaitReasonIsOneLine(t *testing.T) {
	s, src := logsSession(t)
	src.Set(testkit.PodsGVR, initializingPod("odd", "Bad\nReason"))
	waitPodCached(t, s, "odd")
	lr := readLines(openTabLogs(t, s, openPodTab(s, "odd", "app", LogOptions{}), newFakeLogs()))
	lr.wait(t, NoticePrefix+` container "app" is waiting to start: Bad Reason`+startsNotice)
	for _, l := range lr.all() {
		if strings.Contains(l, "\n") {
			t.Errorf("line %q", l)
		}
	}
}
