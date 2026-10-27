package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var (
	replicaSetsGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	statefulSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
)

// fakeLogs is a log streamer: each container's log is its initial content, and a follow stream
// stays open for more (line) until its context ends.
type fakeLogs struct {
	mu      sync.Mutex
	reqs    []cluster.LogOptions
	content map[string]string // "<pod>/<container>" → what the stream starts with
	fail    map[string]error
	open    map[string]*io.PipeWriter
	ended   map[string]bool // follow streams whose context ended
}

func newFakeLogs() *fakeLogs {
	return &fakeLogs{content: map[string]string{}, fail: map[string]error{}, open: map[string]*io.PipeWriter{}, ended: map[string]bool{}}
}

func (f *fakeLogs) StreamLogs(ctx context.Context, opts cluster.LogOptions) (io.ReadCloser, error) {
	key := opts.Pod + "/" + opts.Container
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, opts)
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	content := f.content[key]
	if opts.Previous {
		content = f.content[key+"#previous"]
	}
	go func() {
		_, _ = io.WriteString(pw, content)
		if !opts.Follow {
			_ = pw.Close()
			return
		}
		<-ctx.Done()
		f.mu.Lock()
		f.ended[key] = true
		f.mu.Unlock()
		_ = pw.CloseWithError(ctx.Err())
	}()
	if opts.Follow {
		f.open[key] = pw
	}
	return pr, nil
}

// line writes one more line into an open follow stream.
func (f *fakeLogs) line(t *testing.T, key, text string) {
	t.Helper()
	f.mu.Lock()
	pw := f.open[key]
	f.mu.Unlock()
	if pw == nil {
		t.Fatalf("no open stream %s", key)
	}
	go func() { _, _ = io.WriteString(pw, text+"\n") }()
}

func (f *fakeLogs) requests() []cluster.LogOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func (f *fakeLogs) requestFor(t *testing.T, pod, container string) cluster.LogOptions {
	t.Helper()
	var found []cluster.LogOptions
	for _, r := range f.requests() {
		if r.Pod == pod && r.Container == container {
			found = append(found, r)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no log request for %s/%s: %+v", pod, container, f.requests())
	}
	return found[len(found)-1]
}

// lineReader collects a stream's lines.
type lineReader struct {
	mu    sync.Mutex
	lines []string
	err   error
	done  chan struct{}
}

func readLines(r io.Reader) *lineReader {
	lr := &lineReader{done: make(chan struct{})}
	go func() {
		defer close(lr.done)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lr.mu.Lock()
			lr.lines = append(lr.lines, sc.Text())
			lr.mu.Unlock()
		}
		lr.mu.Lock()
		lr.err = sc.Err()
		lr.mu.Unlock()
	}()
	return lr
}

func (lr *lineReader) all() []string {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return slices.Clone(lr.lines)
}

func (lr *lineReader) wait(t *testing.T, want ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		got := lr.all()
		for _, w := range want {
			if !slices.Contains(got, w) {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("lines %q, want %q among them", lr.all(), want)
	}
}

func owned(obj *unstructured.Unstructured, apiVersion, kind, name, uid string) *unstructured.Unstructured {
	obj.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid, "controller": true,
	}}
	return obj
}

// logPod is a pod of the given containers, each with its state ("running", "waiting",
// "terminated") and restart count; a container that restarted has a terminated last state.
func logPod(namespace, name string, containers ...string) *unstructured.Unstructured {
	var specs, statuses []any
	for _, c := range containers {
		cname, state, _ := strings.Cut(c, ":")
		if state == "" {
			state = "running"
		}
		state, restarts, _ := strings.Cut(state, "#")
		specs = append(specs, map[string]any{"name": cname, "image": "registry.example/app:1"})
		st := map[string]any{"name": cname, "restartCount": int64(len(restarts)), "state": map[string]any{state: map[string]any{}}}
		if restarts != "" {
			st["lastState"] = map[string]any{"terminated": map[string]any{"exitCode": int64(1)}}
		}
		statuses = append(statuses, st)
	}
	return testkit.Object("v1", "Pod", namespace, name, map[string]any{
		"spec":   map[string]any{"containers": specs},
		"status": map[string]any{"phase": "Running", "containerStatuses": statuses},
	})
}

// logRescan is how soon the logs of these tests look at their pods again after a change: the tests
// wait for what a rescan does, not for the product's 250 ms.
const logRescan = 5 * time.Millisecond

// logsSession is a session connected to a cluster with a deployment web (its replica set web-1
// and two pods), a stateful set db with one pod, and a pod nobody controls.
func logsSession(t *testing.T) (*Session, *testkit.MemorySource) {
	t.Helper()
	return logsSessionWith(t, Options{})
}

// logsSessionWith is logsSession with options of the test's own (a time layout, preferences); the
// connector and the snapshot interval are the helper's.
func logsSessionWith(t *testing.T, opts Options) (*Session, *testkit.MemorySource) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", nil))
	src.Set(replicaSetsGVR, owned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-1", nil), "apps/v1", "Deployment", "web", "uid-shop-web"))
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "db", nil))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-a", "app", "proxy"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-b", "app", "proxy:waiting"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db:running#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	src.Set(testkit.PodsGVR, logPod("shop", "loner", "app"))
	profiles := testProfiles()
	for _, key := range []string{"replicasets", "statefulsets", "daemonsets", "jobs"} {
		p, ok := kinds.ByKey(key)
		if !ok {
			t.Fatalf("no profile %s", key)
		}
		profiles = append(profiles, p)
	}
	fc := &fakeConnector{src: src, profiles: profiles, conn: &cluster.Conn{}}
	opts.Connector, opts.SnapshotInterval, opts.LogRescan = fc, 10*time.Millisecond, logRescan
	s := New(opts)
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s, src
}

// openTabLogs opens the logs tab with the fake streamer, as OpenLogs does with the connection.
func openTabLogs(t *testing.T, s *Session, id string, f logStreamer) io.ReadCloser {
	t.Helper()
	tab, b, err := s.streamTab(id, DockLogs)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := s.openLogs(context.Background(), tab, b, f)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = rc.Close()
		if wl, ok := rc.(*workloadLogs); ok {
			wl.wg.Wait() // its streams read the resumptions' timings, which fastResumes puts back after
		}
	})
	return rc
}

// TestWorkloadLogsMergeEveryContainer: a Deployment's logs are those of every running container
// of the pods its replica sets control, each line prefixed with pod and container; a waiting
// container and pods of other owners are left out; the history is shared by the containers.
func TestWorkloadLogsMergeEveryContainer(t *testing.T) {
	s, _ := logsSession(t)
	f := newFakeLogs()
	f.content["web-1-a/app"] = "a1\na2\n"
	f.content["web-1-a/proxy"] = "proxy up\n"
	f.content["web-1-b/app"] = "b1\n"
	f.content["loner/app"] = "never" // not the deployment's
	tab, created, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil || !created || tab.Title != "Logs: Deployment web" || !tab.Logs.ResourceNames || tab.Ref.Kind != "deployments" {
		t.Fatalf("tab %+v created %v: %v", tab, created, err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, "web-1-a/app a1", "web-1-a/app a2", "web-1-a/proxy proxy up", "web-1-b/app b1")
	for _, l := range lr.all() {
		if strings.Contains(l, "never") || strings.HasPrefix(l, "web-1-b/proxy") {
			t.Errorf("a line of a container that is not the deployment's or not running: %q", l)
		}
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("requests %+v", reqs)
	}
	for _, r := range reqs {
		// A follow stream asks for timestamps to resume from; the tab gets them stripped.
		if r.Namespace != "shop" || !r.Follow || r.Previous || !r.Timestamps || r.TailLines != 333 || !r.SinceTime.IsZero() {
			t.Errorf("request %+v: want follow, 1,000 lines shared by three containers", r)
		}
	}
	// More lines while following.
	f.line(t, "web-1-b/app", "b2")
	lr.wait(t, "web-1-b/app b2")
}

// TestWorkloadLogsFollowNewPodsAndRestarts: while the tab follows, a new pod's containers join
// once they run, and a restarted container's new instance gets a stream of its own.
func TestWorkloadLogsFollowNewPodsAndRestarts(t *testing.T) {
	s, src := logsSession(t)
	f := newFakeLogs()
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	wl, ok := openTabLogs(t, s, tab.ID, f).(*workloadLogs)
	if !ok {
		t.Fatal("not a workload's logs")
	}
	lr := readLines(wl)
	waitRequests(t, f, 3)
	// startsNothing checks, once the cache shows what changed, that a rescan has nothing to start
	// and that no stream of pod runs: what the stream's own rescan decides from the same cache.
	startsNothing := func(what, pod string, shown func(*unstructured.Unstructured) bool) {
		t.Helper()
		waitUntil(t, "the cache to show "+what, func() bool {
			obj, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: pod})
			return err == nil && shown(obj)
		})
		if targets := wl.scan(); len(targets) != 0 || slices.ContainsFunc(streamsOf(wl), func(s string) bool { return strings.HasPrefix(s, pod+"/") }) {
			t.Errorf("%s: a rescan starts %+v; streams %q", what, targets, streamsOf(wl))
		}
	}

	// A new pod: waiting first (no request), then running.
	f.mu.Lock()
	f.content["web-1-c/app"] = "c started\n"
	f.mu.Unlock()
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-c", "app:waiting"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	startsNothing("web-1-c waiting", "web-1-c", func(*unstructured.Unstructured) bool { return true })
	if n := len(f.requests()); n != 3 {
		t.Errorf("a waiting container was streamed: %+v", f.requests())
	}
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-c", "app"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	lr.wait(t, "web-1-c/app c started")
	if r := f.requestFor(t, "web-1-c", "app"); r.TailLines != workloadLogTail || !r.Follow {
		t.Errorf("a joining container's request %+v", r)
	}

	// The proxy of web-1-b starts; web-1-a's app restarts.
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-b", "app", "proxy"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-a", "app:running#1", "proxy"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	waitRequests(t, f, 6)
	var appA int
	for _, r := range f.requests() {
		if r.Pod == "web-1-a" && r.Container == "app" {
			appA++
		}
	}
	if appA != 2 {
		t.Errorf("web-1-a/app streamed %d times, want once per instance: %+v", appA, f.requests())
	}
	// Other pods' updates start nothing more.
	src.Set(testkit.PodsGVR, logPod("shop", "loner", "app", "sidecar"))
	startsNothing("loner's sidecar", "loner", func(pod *unstructured.Unstructured) bool {
		return len(model.NestedSlice(pod, "status", "containerStatuses")) == 2
	})
	if n := len(f.requests()); n != 6 {
		t.Errorf("requests after an unrelated change: %+v", f.requests())
	}
}

func waitRequests(t *testing.T, f *fakeLogs, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return len(f.requests()) >= n }) {
		t.Fatalf("%d log requests, want %d: %+v", len(f.requests()), n, f.requests())
	}
}

// TestWorkloadLogsPreviousAndTimestamps: with Previous the stream holds the previous instances of
// the containers that have one, does not follow, and ends; Timestamps reaches every request.
func TestWorkloadLogsPreviousAndTimestamps(t *testing.T) {
	s, _ := logsSession(t)
	f := newFakeLogs()
	f.content["db-0/db#previous"] = "2026-10-11T12:00:00.000000001Z panic: disk full" // no line feed at the end
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLogOptions(tab.ID, func(o *LogOptions) { o.Previous, o.Timestamps = true, true }); err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("a previous-instance stream did not end")
	}
	if got := lr.all(); !slices.Equal(got, []string{"db-0/db 2026-10-11T12:00:00.000000001Z panic: disk full"}) {
		t.Errorf("lines %q", got)
	}
	if r := f.requestFor(t, "db-0", "db"); !r.Previous || r.Follow || !r.Timestamps {
		t.Errorf("request %+v", r)
	}

	// The deployment's containers never restarted: the stream says so and ends.
	dep, _, _ := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	_, _ = s.SetLogOptions(dep.ID, func(o *LogOptions) { o.Previous = true })
	lr = readLines(openTabLogs(t, s, dep.ID, f))
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("a stream without previous instances did not end")
	}
	if got := lr.all(); len(got) != 1 || !strings.HasPrefix(got[0], NoticePrefix+" no container of") {
		t.Errorf("lines %q", got)
	}
}

// TestWorkloadLogsErrorsAndEmpty: a container whose log the server refuses says so in a line of
// Cogpit's own and the others go on; a resource without running containers says it waits.
func TestWorkloadLogsErrorsAndEmpty(t *testing.T) {
	s, src := logsSession(t)
	f := newFakeLogs()
	f.fail["web-1-a/proxy"] = errors.New("pods \"web-1-a\" is forbidden:\nno\x1b[31m")
	f.content["web-1-a/app"] = "fine\n"
	tab, _, _ := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, "web-1-a/app fine", NoticePrefix+" web-1-a/proxy: pods \"web-1-a\" is forbidden: no [31m")

	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "empty", nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	testkit.WaitFor(ctx, func() bool {
		_, err := s.Lookup(model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "empty"})
		return err == nil
	})
	empty, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	lr = readLines(openTabLogs(t, s, empty.ID, f))
	lr.wait(t, NoticePrefix+" StatefulSet empty has no running containers yet; their logs show here when they start")
}

// TestWorkloadLogsBoundsStreams: at most workloadLogStreams containers stream at once; the stream
// says so, and one left out starts when another ends.
func TestWorkloadLogsBoundsStreams(t *testing.T) {
	s, src := logsSession(t)
	for i := range workloadLogStreams + 1 {
		src.Set(testkit.PodsGVR, owned(logPod("shop", fmt.Sprintf("db-%02d", i+1), "db"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	testkit.WaitFor(ctx, func() bool {
		_, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-41"})
		return err == nil
	})
	f := newFakeLogs()
	tab, _, _ := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	rc := openTabLogs(t, s, tab.ID, f)
	lr := readLines(rc)
	lr.wait(t, fmt.Sprintf("%s showing %d containers' logs at once; the others join as these end", NoticePrefix, workloadLogStreams))
	// The streams open on their own goroutines; the notice may come before the last has asked.
	wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wcancel()
	testkit.WaitFor(wctx, func() bool { return len(f.requests()) >= workloadLogStreams })
	if n := len(f.requests()); n != workloadLogStreams {
		t.Errorf("%d streams, want %d", n, workloadLogStreams)
	}
	for _, r := range f.requests() {
		if r.TailLines != workloadLogTail/workloadLogStreams && r.TailLines != workloadLogMinTail {
			t.Errorf("tail %d", r.TailLines)
		}
	}
	// One ends with its container: the one left out starts. (A stream the server ends while the
	// cache shows its container running is resumed instead, and keeps its place.)
	streamed := map[string]bool{}
	for _, r := range f.requests() {
		streamed[r.Pod] = true
	}
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db:terminated#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	waitUntil(t, "the cache to show db-0's container ended", func() bool {
		pod, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})
		return err == nil && model.NestedMap(model.NestedSlice(pod, "status", "containerStatuses")[0], "state", "terminated") != nil
	})
	f.mu.Lock()
	pw := f.open["db-0/db"]
	f.mu.Unlock()
	_ = pw.Close()
	waitRequests(t, f, workloadLogStreams+1)
	if r := f.requests()[workloadLogStreams]; streamed[r.Pod] {
		t.Errorf("the next stream is %s's, which streamed already; want one left out", r.Pod)
	}

	// Closing the stream ends every container's stream.
	_ = rc.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if !testkit.WaitFor(ctx2, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.ended) >= workloadLogStreams
	}) {
		t.Errorf("%d streams ended after Close", len(f.ended))
	}
}

// TestWorkloadLogsPreviousBeyondTheBound: the previous instances of more containers than stream at
// once all show: the ones left out start as the first end, as the notice says, and the stream ends
// after the last.
func TestWorkloadLogsPreviousBeyondTheBound(t *testing.T) {
	s, src := logsSession(t)
	f := newFakeLogs()
	f.content["db-0/db#previous"] = "old\n"
	n := workloadLogStreams + 5
	for i := range n {
		name := fmt.Sprintf("db-%02d", i+1)
		src.Set(testkit.PodsGVR, owned(logPod("shop", name, "db:running#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
		f.content[name+"/db#previous"] = "old\n"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	testkit.WaitFor(ctx, func() bool {
		_, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: fmt.Sprintf("db-%02d", n)})
		return err == nil
	})
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLogOptions(tab.ID, func(o *LogOptions) { o.Previous = true }); err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the previous instances' stream did not end")
	}
	seen := map[string]int{}
	for _, l := range lr.all() {
		if prefix, ok := strings.CutSuffix(l, " old"); ok {
			seen[prefix]++
		}
	}
	if len(seen) != n+1 { // db-0 of logsSession has a previous instance too
		t.Errorf("%d containers' previous logs, want %d", len(seen), n+1)
	}
	for prefix, c := range seen {
		if c != 1 {
			t.Errorf("%s's previous log shows %d times", prefix, c)
		}
	}
}

// TestPodLogOptions: a pod's tab asks for its container's log with the tab's options, always with
// the server's timestamps (the GUI hides them unless the tab shows them).
func TestPodLogOptions(t *testing.T) {
	ref := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	got := podLogOptions(DockTab{Ref: &ref, Container: "app"})
	if got != (cluster.LogOptions{Namespace: "shop", Pod: "web-0", Container: "app", Follow: true, Timestamps: true, TailLines: logTailLines}) {
		t.Errorf("default %+v", got)
	}
	got = podLogOptions(DockTab{Ref: &ref, Container: "app", Logs: LogOptions{Previous: true, Timestamps: true, Wrap: true, Search: "x"}})
	if !got.Previous || got.Follow || !got.Timestamps || got.TailLines != logTailLines {
		t.Errorf("previous with timestamps %+v", got)
	}
}

// TestControllingWorkload: a pod's tab moves to its Deployment through the replica set, to its
// stateful set directly; a pod without a controller has none.
func TestControllingWorkload(t *testing.T) {
	s, _ := logsSession(t)
	ctx := context.Background()
	for pod, want := range map[string]string{"web-1-a": "deployments shop/web", "db-0": "statefulsets shop/db"} {
		got, err := s.ControllingWorkload(ctx, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: pod})
		if err != nil || got.Kind+" "+got.Namespace+"/"+got.Name != want || got.Context != "test" {
			t.Errorf("%s: %+v, %v; want %s", pod, got, err, want)
		}
	}
	if _, err := s.ControllingWorkload(ctx, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "loner"}); !errors.Is(err, ErrNoController) {
		t.Errorf("loner: %v", err)
	}
}
