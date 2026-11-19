package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// podCluster is a cluster's pods in kube-system as the node shell and its sweep see them: the
// create gives a pod a UID, the delete honours the UID precondition as the API server does (409
// Conflict on another UID, 404 on none), a name can be made forbidden, and every call is recorded.
type podCluster struct {
	mu        sync.Mutex
	pods      map[string]*corev1.Pod // by name
	forbidden map[string]bool        // names whose delete is forbidden
	calls     []string
	created   []*corev1.Pod
	createErr error
	onCreate  func(*corev1.Pod) // runs before the create is stored
	next      int
}

func newPodCluster() *podCluster {
	return &podCluster{pods: map[string]*corev1.Pod{}, forbidden: map[string]bool{}}
}

var podsResource = schema.GroupResource{Resource: "pods"}

func (c *podCluster) RestartDeployment(context.Context, string, string, string) error {
	return errors.New("not in this test")
}

func (c *podCluster) put(name, uid string, phase corev1.PodPhase, annotations map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pods[name] = &corev1.Pod{}
	c.pods[name].Name, c.pods[name].Namespace, c.pods[name].UID = name, NodeShellNamespace, types.UID(uid)
	c.pods[name].Annotations = annotations
	c.pods[name].Status.Phase = phase
}

func (c *podCluster) CreatePod(_ context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	c.mu.Lock()
	onCreate, err := c.onCreate, c.createErr
	c.mu.Unlock()
	if onCreate != nil {
		onCreate(pod)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "create "+pod.Name)
	if err != nil {
		return nil, err
	}
	c.next++
	out := pod.DeepCopy()
	out.UID = types.UID(fmt.Sprintf("uid-created-%d", c.next))
	c.pods[out.Name] = out
	c.created = append(c.created, out.DeepCopy())
	return out, nil
}

func (c *podCluster) WaitPodRunning(context.Context, string, string, string) error { return nil }

func (c *podCluster) GetPod(_ context.Context, namespace, name string) (*corev1.Pod, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "get "+name)
	p, ok := c.pods[name]
	if !ok || namespace != NodeShellNamespace {
		return nil, apierrors.NewNotFound(podsResource, name)
	}
	return p.DeepCopy(), nil
}

func (c *podCluster) DeletePod(_ context.Context, namespace, name, uid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "delete "+name+" uid "+uid)
	if c.forbidden[name] {
		return apierrors.NewForbidden(podsResource, name, errors.New(`User "dev" cannot delete resource "pods" in the namespace "kube-system"`))
	}
	p, ok := c.pods[name]
	if !ok || namespace != NodeShellNamespace {
		return apierrors.NewNotFound(podsResource, name)
	}
	if uid != "" && string(p.UID) != uid {
		return apierrors.NewConflict(podsResource, name, fmt.Errorf("Precondition failed: UID in precondition: %s, UID in object meta: %s", uid, p.UID))
	}
	delete(c.pods, name)
	return nil
}

func (c *podCluster) has(name string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pods[name]
	if !ok {
		return "", false
	}
	return string(p.UID), true
}

func (c *podCluster) callList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

var testNodes = &model.KindProfile{
	Key: "nodes", Version: "v1", Resource: "nodes", Kind: "Node", Title: "Nodes",
	Columns: []model.Column{{ID: "name", Title: "Name", Type: model.ColumnText}},
}

// shellSweepSession is a session over the pods of c with journal j, its toasts and audit lines
// recorded; it is not connected yet.
type shellSweepSession struct {
	s      *Session
	toasts *toastLog
	audit  *auditBuffer
}

type toastLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *toastLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.msgs)
}

type auditBuffer struct {
	mu    sync.Mutex
	lines []AuditEntry
}

func (b *auditBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range strings.Split(strings.TrimSpace(string(p)), "\n") {
		var e AuditEntry
		if err := json.Unmarshal([]byte(l), &e); err == nil {
			b.lines = append(b.lines, e)
		}
	}
	return len(p), nil
}

func (b *auditBuffer) of(command string) []AuditEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []AuditEntry
	for _, e := range b.lines {
		if e.Command == command {
			out = append(out, e)
		}
	}
	return out
}

func newShellSweepSession(t *testing.T, c *podCluster, j NodeShellJournal) *shellSweepSession {
	t.Helper()
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: append(testProfiles(), testNodes), actions: c}
	ss := &shellSweepSession{toasts: &toastLog{}, audit: &auditBuffer{}}
	ss.s = New(Options{Connector: fc, NodeShells: j, Audit: ss.audit, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(ss.s.Close)
	t.Cleanup(ss.s.Subscribe(func(ev Event) {
		if ev.Type == EventToast {
			ss.toasts.mu.Lock()
			ss.toasts.msgs = append(ss.toasts.msgs, ev.Message)
			ss.toasts.mu.Unlock()
		}
	}))
	return ss
}

func journalNames(t *testing.T, j NodeShellJournal) []string {
	t.Helper()
	entries, err := j.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Context+"/"+e.Name)
	}
	slices.Sort(out)
	return out
}

func shellEntry(ctxName, name, uid, id string) NodeShellEntry {
	return NodeShellEntry{Context: ctxName, Namespace: NodeShellNamespace, Name: name, UID: uid, ID: id, Node: "worker-1",
		Created: time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)}
}

// The pods an earlier run left (H-71, owner): on the connect to their context Cogpit deletes its
// own, Running or Failed, by UID; a pod of the name with another UID, or recorded without a UID and
// carrying another id, is not Cogpit's and stays; a pod that is gone clears its entry; a forbidden
// delete keeps the entry and says so; another context's entries and another live Cogpit process's
// are untouched. The deletes are in the audit log; the notification names the pods deleted, and a
// warning those kept.
func TestNodeShellSweepDeletesOwnPodsOnly(t *testing.T) {
	dir := t.TempDir()
	j := NewFileNodeShellJournal(dir)
	c := newPodCluster()
	c.put("ns-running", "uid-running", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "id-running"})
	c.put("ns-failed", "uid-failed", corev1.PodFailed, map[string]string{NodeShellIDAnnotation: "id-failed"})
	c.put("ns-replaced", "uid-someone-elses", corev1.PodRunning, nil)
	c.put("ns-forbidden", "uid-forbidden", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "id-forbidden"})
	c.put("ns-nouid", "uid-nouid", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "id-nouid"})
	c.put("ns-nouid-other", "uid-other-machine", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "another-id"})
	c.put("ns-other-context", "uid-other-context", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "id-oc"})
	c.put("ns-elsewhere", "uid-elsewhere", corev1.PodRunning, map[string]string{NodeShellIDAnnotation: "id-elsewhere"})
	c.forbidden["ns-forbidden"] = true
	for _, e := range []NodeShellEntry{
		shellEntry("test", "ns-running", "uid-running", "id-running"),
		shellEntry("test", "ns-failed", "uid-failed", "id-failed"),
		shellEntry("test", "ns-replaced", "uid-mine-was-deleted", "id-replaced"),
		shellEntry("test", "ns-gone", "uid-gone", "id-gone"),
		shellEntry("test", "ns-forbidden", "uid-forbidden", "id-forbidden"),
		shellEntry("test", "ns-nouid", "", "id-nouid"),
		shellEntry("test", "ns-nouid-other", "", "id-nouid-other"),
		shellEntry("other", "ns-other-context", "uid-other-context", "id-oc"),
	} {
		if err := j.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	// Another Cogpit process of the user, alive, recorded its own node shell in this context.
	elsewhere := newFileNodeShellJournal(newJournalDir(dir))
	if err := elsewhere.Put(shellEntry("test", "ns-elsewhere", "uid-elsewhere", "id-elsewhere")); err != nil {
		t.Fatal(err)
	}
	defer runtime.KeepAlive(elsewhere) // its owner's lock lives until the test ends (TestDrainsOfAnotherProcess)

	ss := newShellSweepSession(t, c, j)
	if err := ss.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	want := []string{"other/ns-other-context", "test/ns-elsewhere", "test/ns-forbidden"}
	waitFor(t, "the sweep", func() bool { return slices.Equal(journalNames(t, j), want) })
	waitFor(t, "the notifications", func() bool { return len(ss.toasts.all()) == 2 })

	for name, uid := range map[string]string{"ns-running": "", "ns-failed": "", "ns-nouid": "",
		"ns-replaced": "uid-someone-elses", "ns-forbidden": "uid-forbidden", "ns-nouid-other": "uid-other-machine",
		"ns-other-context": "uid-other-context", "ns-elsewhere": "uid-elsewhere"} {
		got, ok := c.has(name)
		if uid == "" && ok {
			t.Errorf("%s was not deleted", name)
		}
		if uid != "" && got != uid {
			t.Errorf("%s: uid %q (present %v), want %q untouched", name, got, ok, uid)
		}
	}
	calls := c.callList()
	for _, call := range []string{"delete ns-running uid uid-running", "delete ns-failed uid uid-failed",
		"delete ns-replaced uid uid-mine-was-deleted", "delete ns-gone uid uid-gone", "delete ns-forbidden uid uid-forbidden",
		"get ns-nouid", "delete ns-nouid uid uid-nouid", "get ns-nouid-other"} {
		if !slices.Contains(calls, call) {
			t.Errorf("no %q among %v", call, calls)
		}
	}
	for _, call := range calls {
		if strings.Contains(call, "ns-other-context") || strings.Contains(call, "ns-elsewhere") || call == "delete ns-nouid-other uid uid-other-machine" ||
			(strings.HasPrefix(call, "delete ") && strings.HasSuffix(call, " uid ")) {
			t.Errorf("a call it must not make: %q", call)
		}
	}

	toasts := ss.toasts.all()
	slices.Sort(toasts)
	if !strings.HasPrefix(toasts[0], "Cogpit could not delete a node shell pod it had left in context test") ||
		!strings.Contains(toasts[0], "kube-system/ns-forbidden (node worker-1): you may not delete pods in kube-system") ||
		!strings.Contains(toasts[0], "tries again at the next connect") {
		t.Errorf("warning %q", toasts[0])
	}
	if !strings.HasPrefix(toasts[1], "Cogpit deleted 3 node shell pods it had left in context test") ||
		!strings.Contains(toasts[1], "kube-system/ns-running (node worker-1)") || !strings.Contains(toasts[1], "kube-system/ns-failed") ||
		!strings.Contains(toasts[1], "kube-system/ns-nouid") || strings.Contains(toasts[1], "ns-replaced") {
		t.Errorf("notice %q", toasts[1])
	}

	audited := ss.audit.of(NodeShellCleanupAudit)
	if len(audited) != 6 { // running, failed, replaced, gone, forbidden, nouid: every delete sent
		t.Fatalf("audit %+v", audited)
	}
	for _, e := range audited {
		var p map[string]string
		_ = json.Unmarshal(e.Params, &p)
		if e.Origin != OriginGUI || e.Context != "test" || p["namespace"] != NodeShellNamespace || p["name"] == "" || p["uid"] == "" {
			t.Errorf("audit line %+v %s", e, e.Params)
		}
		// The line says what the API server answered: deleted, or not found, another UID, forbidden.
		if deleted := slices.Contains([]string{"ns-running", "ns-failed", "ns-nouid"}, p["name"]); deleted != e.OK {
			t.Errorf("audit line of %s: ok %v (%s)", p["name"], e.OK, e.Error)
		}
	}

	// The next connect tries the forbidden one again; allowed now, it goes.
	c.mu.Lock()
	c.forbidden = map[string]bool{}
	c.mu.Unlock()
	if err := ss.s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := ss.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	retried := func() bool {
		return slices.Equal(journalNames(t, j), []string{"other/ns-other-context", "test/ns-elsewhere"})
	}
	wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(wctx, retried) {
		t.Fatalf("the retry: journal %v, calls %v, toasts %q, state %+v", journalNames(t, j), c.callList(), ss.toasts.all(), ss.s.UIState().Connection)
	}
	if _, ok := c.has("ns-forbidden"); ok {
		t.Error("the retried pod is still there")
	}
}

// A node shell's pod is recorded before its create is sent, carrying the entry's id, and gets its
// UID after; a failed create, and the pod's normal delete, drop the entry; a journal that cannot
// be written refuses the node shell before anything is created. The sweep leaves a node shell this
// session holds alone.
func TestNodeShellJournalBeforeTheCreate(t *testing.T) {
	j := NewMemoryNodeShellJournal()
	c := newPodCluster()
	ss := newShellSweepSession(t, c, j)
	s := ss.s
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	var atCreate []NodeShellEntry
	c.onCreate = func(pod *corev1.Pod) {
		atCreate, _ = j.List()
		if len(atCreate) != 1 || atCreate[0].Name != pod.Name || atCreate[0].UID != "" || atCreate[0].ID == "" ||
			pod.Annotations[NodeShellIDAnnotation] != atCreate[0].ID {
			t.Errorf("at the create: journal %+v, pod %s annotations %v", atCreate, pod.Name, pod.Annotations)
		}
	}
	node := model.ObjectRef{Kind: "nodes", Name: "worker-1"}
	tab, err := s.StartNodeShell(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := j.List()
	if len(entries) != 1 || entries[0].UID != "uid-created-1" || entries[0].Context != "test" || entries[0].Node != "worker-1" ||
		entries[0].Namespace != NodeShellNamespace || entries[0].Name != tab.NodeShell.Pod {
		t.Fatalf("after the create: %+v", entries)
	}

	// The sweep leaves the open shell's pod alone.
	s.sweepNodeShells("test")
	if _, ok := c.has(tab.NodeShell.Pod); !ok {
		t.Fatal("the sweep deleted the open node shell's pod")
	}

	// Closing the tab deletes the pod and drops the entry.
	s.UpdateUI(func(u *UIState) { u.Dock = slices.DeleteFunc(u.Dock, func(d DockTab) bool { return d.ID == tab.ID }) })
	waitFor(t, "the entry dropped", func() bool { e, _ := j.List(); return len(e) == 0 })
	if _, ok := c.has(tab.NodeShell.Pod); ok {
		t.Error("the closed tab's pod is still there")
	}

	// A failed create drops the entry it made.
	c.mu.Lock()
	c.onCreate, c.createErr = nil, apierrors.NewForbidden(podsResource, "x", errors.New("no"))
	c.mu.Unlock()
	if _, err := s.StartNodeShell(context.Background(), node); err == nil {
		t.Fatal("the create did not fail")
	}
	if e, _ := j.List(); len(e) != 0 {
		t.Errorf("a failed create left %+v", e)
	}

	// A journal that cannot be written: nothing is created.
	c.mu.Lock()
	c.createErr = nil
	n := len(c.created)
	c.mu.Unlock()
	broken := newShellSweepSession(t, c, failingShellJournal{})
	if err := broken.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.s.StartNodeShell(context.Background(), node); err == nil || !strings.Contains(err.Error(), "nothing was created") {
		t.Errorf("without a journal: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.created) != n {
		t.Error("a pod was created that the journal could not record")
	}
}

// failingShellJournal cannot be written.
type failingShellJournal struct{}

func (failingShellJournal) List() ([]NodeShellEntry, error) { return nil, nil }
func (failingShellJournal) Put(NodeShellEntry) error        { return errors.New("disk full") }
func (failingShellJournal) Remove(string, string, string) error {
	return errors.New("disk full")
}

// The file journal keeps its entries owner-only across processes, replaces an entry of the same
// pod, and reports an unreadable file instead of overwriting it.
func TestFileNodeShellJournal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	j := NewFileNodeShellJournal(dir)
	if e, err := j.List(); err != nil || len(e) != 0 {
		t.Fatalf("empty: %v, %v", e, err)
	}
	a := shellEntry("test", "ns-a", "", "id-a")
	if err := j.Put(a); err != nil {
		t.Fatal(err)
	}
	a.UID = "uid-a"
	if err := j.Put(a); err != nil {
		t.Fatal(err)
	}
	if err := j.Put(shellEntry("test", "ns-b", "uid-b", "id-b")); err != nil {
		t.Fatal(err)
	}
	if err := j.Remove("test", "default", "ns-b"); err != nil { // another namespace: nothing
		t.Fatal(err)
	}
	if e, _ := j.List(); len(e) != 2 {
		t.Fatalf("a remove in another namespace removed: %+v", e)
	}
	if err := j.Remove("test", NodeShellNamespace, "ns-b"); err != nil {
		t.Fatal(err)
	}
	// A second process reads what the first wrote; the entry's owner is alive elsewhere for it.
	other := newFileNodeShellJournal(newJournalDir(dir))
	defer runtime.KeepAlive(j)
	entries, err := other.List()
	if err != nil || len(entries) != 1 || entries[0].UID != "uid-a" || entries[0].Owner == nil || !entries[0].OwnedElsewhere() {
		t.Fatalf("the other process's list: %+v, %v", entries, err)
	}
	if own, _ := j.List(); len(own) != 1 || own[0].OwnedElsewhere() {
		t.Errorf("the writer's own list: %+v", own)
	}
	if info, err := os.Stat(j.Path()); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("the journal file: %v, %v", info.Mode(), err)
	}
	if info, err := os.Stat(dir); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Errorf("the journal's directory: %v, %v", info.Mode(), err)
	}

	if err := os.WriteFile(j.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.List(); err == nil {
		t.Error("an unreadable journal was read")
	}
	if err := j.Put(a); err == nil {
		t.Error("an unreadable journal was overwritten")
	}
	if b, _ := os.ReadFile(j.Path()); string(b) != "{not json" {
		t.Errorf("the unreadable journal was changed: %q", b)
	}
}
