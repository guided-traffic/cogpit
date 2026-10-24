package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The log API names a container by its pod's name, never by the pod's UID or the container's
// instance: a log opened again after an interruption, or after the server ended its stream, is the
// log of whatever pod and container instance hold that name now. These tests pin what a log does
// when that is no longer the one it showed (a Stateful Set's pod created again under its name, a
// container restarted) while Cogpit's cache still shows the old one, as it does right after a
// reconnect: no line twice, and a notice where the log no longer goes on from its last line.

// watchedLogsSession is a session with a connection watch the fake prober answers for, connected to
// a cluster with the Stateful Set db and its pod db-0 (UID uid-shop-db-0, container db).
func watchedLogsSession(t *testing.T) (*Session, *testkit.MemorySource, *fakeProber) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "db", nil))
	src.Set(testkit.PodsGVR, dbPod("uid-shop-db-0", "db"))
	sts, ok := kinds.ByKey("statefulsets")
	if !ok {
		t.Fatal("no profile statefulsets")
	}
	p := &fakeProber{}
	fc := &fakeConnector{src: src, profiles: append(testProfiles(), sts), conn: &cluster.Conn{}, health: p}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s, src, p
}

// dbPod is the Stateful Set db's pod db-0 with the given UID and container (logPod's form).
func dbPod(uid, container string) *unstructured.Unstructured {
	pod := owned(logPod("shop", "db-0", container), "apps/v1", "StatefulSet", "db", "uid-shop-db")
	pod.SetUID(types.UID(uid))
	return pod
}

// recreate gives a container's log, by its pod's name, a new content: the pod was created again
// under its name, or the container restarted. The old instance's open streams end with it.
func (f *serverLogs) recreate(key string, lines ...stampedLine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, st := range f.open[key] {
		close(st.lines)
	}
	delete(f.open, key)
	f.log[key] = slices.Clone(lines)
}

// waitCached waits until the cache shows db-0 with the given UID and restart count of its db.
func waitCached(t *testing.T, s *Session, uid string, restarts int64) {
	t.Helper()
	waitUntil(t, "the cache to show db-0 "+uid, func() bool {
		pod, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})
		if err != nil || string(pod.GetUID()) != uid {
			return false
		}
		n, _ := model.NestedInt64(model.NestedSlice(pod, "status", "containerStatuses")[0], "restartCount")
		return n == restarts
	})
}

// countOf counts the lines equal to line.
func countOf(lines []string, line string) int {
	n := 0
	for _, l := range lines {
		if l == line {
			n++
		}
	}
	return n
}

// TestWorkloadLogsAPodCreatedAgainDuringAnInterruption: the Stateful Set's pod db-0 is deleted and
// created again under its name while the connection is down. At the reconnect the cache still
// shows the old pod, so the stream opens again by the name and gets the new pod's log: it says that
// the log no longer goes on from its last line, and when the cache shows the new pod, no second
// stream starts for it. Every line once.
func TestWorkloadLogsAPodCreatedAgainDuringAnInterruption(t *testing.T) {
	s, src, p := watchedLogsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	f.add(key, t0.Add(100*time.Millisecond), "old pod")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "old pod"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	f.drop(key) // the stream ends with the connection
	// Meanwhile db-0 is created again; the cache, whose watch the interruption holds, does not see it.
	f.recreate(key, stampedLine{t0.Add(5 * time.Second), "new pod"}, stampedLine{t0.Add(6 * time.Second), "new pod, second line"})
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	lr.wait(t, line(5000, "new pod"), line(6000, "new pod, second line"))

	// The cache catches up.
	src.Set(testkit.PodsGVR, dbPod("uid-shop-db-0-again", "db"))
	waitCached(t, s, "uid-shop-db-0-again", 0)
	time.Sleep(4 * workloadLogRescan) // rescans that would start a stream of the new pod's own
	waitUntil(t, "a stream to follow", func() bool { return f.opened(key) })
	f.add(key, t0.Add(7*time.Second), "new pod, third line")
	lr.wait(t, line(7000, "new pod, third line"))

	got := lr.all()
	for _, l := range []string{line(100, "old pod"), line(5000, "new pod"), line(6000, "new pod, second line"), line(7000, "new pod, third line")} {
		if n := countOf(got, l); n != 1 {
			t.Errorf("%q %d times, want once; lines %q", l, n, got)
		}
	}
	if len(got) != 6 || !strings.HasPrefix(got[1], NoticePrefix+" db-0/db: "+interruptedMarkerStart) ||
		got[2] != NoticePrefix+" db-0/db: "+notContinuedNotice || got[3] != line(5000, "new pod") {
		t.Errorf("lines %q: want the old pod's line, the marker, the notice, the new pod's lines", got)
	}
	if reqs := f.requestsFor(key); len(reqs) != 2 {
		t.Errorf("%d log requests, want the first and the resumption: %+v", len(reqs), reqs)
	}
}

// TestWorkloadLogsAPodCreatedAgainSeenBeforeTheResume: when the cache already shows the new pod as
// the old pod's stream is about to open again, that stream ends (after its marker), and the new
// pod's own stream shows its lines, once.
func TestWorkloadLogsAPodCreatedAgainSeenBeforeTheResume(t *testing.T) {
	s, src, p := watchedLogsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	f.add(key, t0.Add(100*time.Millisecond), "old pod")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	wl, ok := openTabLogs(t, s, tab.ID, f).(*workloadLogs)
	if !ok {
		t.Fatal("not a workload's logs")
	}
	lr := readLines(wl)
	lr.wait(t, line(100, "old pod"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	f.drop(key)
	time.Sleep(200 * time.Millisecond) // the old pod's stream waits for the connection (either way, it must not show a line twice)
	f.recreate(key, stampedLine{t0.Add(5 * time.Second), "new pod"})
	// The cache sees the new pod before the old pod's stream opens again (here while the fake
	// connection is down: the memory source is not held), and the scan starts the new pod's stream.
	src.Set(testkit.PodsGVR, dbPod("uid-shop-db-0-again", "db"))
	waitCached(t, s, "uid-shop-db-0-again", 0)
	lr.wait(t, line(5000, "new pod"))
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	waitUntil(t, "the old pod's stream to end", func() bool {
		wl.mu.Lock()
		defer wl.mu.Unlock()
		_, running := wl.streaming["uid-shop-db-0/db/0"]
		return !running
	})
	time.Sleep(4 * workloadLogRescan)
	f.add(key, t0.Add(6*time.Second), "new pod, second line")
	lr.wait(t, line(6000, "new pod, second line"))

	got := lr.all()
	for _, l := range []string{line(100, "old pod"), line(5000, "new pod"), line(6000, "new pod, second line")} {
		if n := countOf(got, l); n != 1 {
			t.Errorf("%q %d times, want once; lines %q", l, n, got)
		}
	}
	if slices.Contains(got, NoticePrefix+" db-0/db: "+notContinuedNotice) {
		t.Errorf("lines %q: the new pod's own stream is no resumption", got)
	}
	reqs := f.requestsFor(key)
	if len(reqs) != 2 || !reqs[1].SinceTime.IsZero() || reqs[1].TailLines != workloadLogTail {
		t.Errorf("log requests %+v: want the old pod's first and the new pod's own", reqs)
	}
}

// TestWorkloadLogsARestartTheCacheShowsLate: the server ends the stream as the container restarts,
// and the cache shows the restart only after the resumption's backoff, so the stream opens again by
// the name and follows the new instance. When the cache shows it, no second stream starts for it
// (until 2026-10-20 the new instance's lines came twice).
func TestWorkloadLogsARestartTheCacheShowsLate(t *testing.T) {
	fastResumes(t, 5)
	s, src, _ := watchedLogsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	f.add(key, t0.Add(100*time.Millisecond), "first instance")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "first instance"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	f.recreate(key, stampedLine{t0.Add(3 * time.Second), "second instance"})
	lr.wait(t, line(3000, "second instance"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db:running#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	waitCached(t, s, "uid-shop-db-0", 1)
	time.Sleep(4 * workloadLogRescan)
	f.add(key, t0.Add(4*time.Second), "second instance, second line")
	lr.wait(t, line(4000, "second instance, second line"))

	got := lr.all()
	want := []string{line(100, "first instance"), NoticePrefix + " db-0/db: " + notContinuedNotice, line(3000, "second instance"), line(4000, "second instance, second line")}
	if !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	if reqs := f.requestsFor(key); len(reqs) != 2 {
		t.Errorf("%d log requests, want the first and the resumption: %+v", len(reqs), reqs)
	}
}

// TestWorkloadLogsAPodCreatedAgainTheCacheShowsLate: the Stateful Set's pod db-0 is deleted and
// created again under its name with a new UID, no interruption: the server ends the old pod's
// stream, and the cache shows the new pod only after the resumption's backoff (the lead's note of
// 2026-10-14: "while the cache lags > 500 ms", the backoff shortened here). The stream opens again
// by the name, says that the log no longer goes on from its last line, and when the cache shows
// the new pod under its new UID, no second stream starts for it: every line once, two requests.
func TestWorkloadLogsAPodCreatedAgainTheCacheShowsLate(t *testing.T) {
	fastResumes(t, 5)
	s, src, _ := watchedLogsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	f.add(key, t0.Add(100*time.Millisecond), "old pod")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "old pod"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	// db-0 is created again: the old pod's stream ends; the cache still shows the old pod.
	f.recreate(key, stampedLine{t0.Add(5 * time.Second), "new pod"}, stampedLine{t0.Add(6 * time.Second), "new pod, second line"})
	lr.wait(t, line(5000, "new pod"), line(6000, "new pod, second line"))

	// The cache catches up: the new pod, its new UID, the same name.
	src.Set(testkit.PodsGVR, dbPod("uid-shop-db-0-again", "db"))
	waitCached(t, s, "uid-shop-db-0-again", 0)
	time.Sleep(4 * workloadLogRescan) // rescans that would start a stream of the new pod's own
	f.add(key, t0.Add(7*time.Second), "new pod, third line")
	lr.wait(t, line(7000, "new pod, third line"))

	got := lr.all()
	want := []string{line(100, "old pod"), NoticePrefix + " db-0/db: " + notContinuedNotice,
		line(5000, "new pod"), line(6000, "new pod, second line"), line(7000, "new pod, third line")}
	if !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	if reqs := f.requestsFor(key); len(reqs) != 2 {
		t.Errorf("%d log requests, want the first and the resumption: %+v", len(reqs), reqs)
	}
}

// TestWorkloadLogsAPodCreatedAgainTheCacheShowsInTime: as above, but the cache shows the new pod
// before the old pod's stream would open again (the backoff as released): that stream ends without
// opening again, and the new pod's own stream shows its lines, once, with no notice.
func TestWorkloadLogsAPodCreatedAgainTheCacheShowsInTime(t *testing.T) {
	s, src, _ := watchedLogsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	f.add(key, t0.Add(100*time.Millisecond), "old pod")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "old pod"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	f.recreate(key, stampedLine{t0.Add(5 * time.Second), "new pod"})
	src.Set(testkit.PodsGVR, dbPod("uid-shop-db-0-again", "db")) // within the 500 ms backoff
	waitCached(t, s, "uid-shop-db-0-again", 0)
	lr.wait(t, line(5000, "new pod"))
	time.Sleep(workloadLogBackoff + 4*workloadLogRescan) // past the old stream's resumption
	f.add(key, t0.Add(6*time.Second), "new pod, second line")
	lr.wait(t, line(6000, "new pod, second line"))

	got := lr.all()
	want := []string{line(100, "old pod"), line(5000, "new pod"), line(6000, "new pod, second line")}
	if !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	reqs := f.requestsFor(key)
	if len(reqs) != 2 || !reqs[1].SinceTime.IsZero() {
		t.Errorf("log requests %+v: want the old pod's first and the new pod's own", reqs)
	}
}

// TestAPodLogCreatedAgainDuringAnInterruption: a pod's tab follows its log by the pod's name; the pod
// is created again under it while the connection is down. After the marker the log says that it no
// longer goes on from its last line, and the new pod's lines follow.
func TestAPodLogCreatedAgainDuringAnInterruption(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	f := newServerLogs()
	const key = "web-1/app"
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return t0.Add(time.Duration(ms)*time.Millisecond).Format(time.RFC3339Nano) + " " + text
	}
	f.add(key, t0.Add(100*time.Millisecond), "old pod")
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "default", Name: "web-1"}
	tab := DockTab{ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "web-1", Context: "test", Ref: &ref, Container: "app"}
	s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, tab) })
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "old pod"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	f.drop(key)
	f.recreate(key, stampedLine{t0.Add(5 * time.Second), "new pod"})
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	lr.wait(t, line(5000, "new pod"))

	got := lr.all()
	if len(got) != 4 || got[0] != line(100, "old pod") || !strings.HasPrefix(got[1], NoticePrefix+" "+interruptedMarkerStart) ||
		got[2] != NoticePrefix+" "+notContinuedNotice || got[3] != line(5000, "new pod") {
		t.Errorf("lines %q: want the old pod's line, the marker, the notice, the new pod's line", got)
	}
}
