package commands_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// crashingJournal stands for the disk of a process that dies: once dead, nothing the process
// writes reaches the journal any more.
type crashingJournal struct {
	engine.DrainJournal
	dead atomic.Bool
}

func (c *crashingJournal) Put(p engine.PendingDrain) error {
	if c.dead.Load() {
		return errors.New("the process is gone")
	}
	return c.DrainJournal.Put(p)
}

func (c *crashingJournal) Remove(context, node string) error {
	if c.dead.Load() {
		return errors.New("the process is gone")
	}
	return c.DrainJournal.Remove(context, node)
}

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// reconnect connects "test" again with an event log attached first, as a fresh start of Cogpit
// does: the session's first connect happened before a test could subscribe.
func reconnect(t *testing.T, h *harness) *eventLog {
	t.Helper()
	events := recordEvents(h.s)
	if err := h.s.Disconnect(); err != nil { // a connect again would only show the open connection (D-69)
		t.Fatal(err)
	}
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	warmActionKinds(t, h.s)
	return events
}

func pendingOf(t *testing.T, h *harness) []engine.PendingDrainInfo {
	t.Helper()
	var out []engine.PendingDrainInfo
	h.must(t, engine.OriginExternalAgent, "node.pendingDrains", nil, &out)
	return out
}

// TestCrashMidDrainIsOfferedOnTheNextStart is D-11 for drains: Cogpit dies between cordoning the
// node and evicting its pods. The journal on disk still holds the drain; the next start of Cogpit
// offers it when the context is connected, and finishing, uncordoning or dismissing clears it.
func TestCrashMidDrainIsOfferedOnTheNextStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit") // does not exist yet: the journal creates it
	src := seedActions()

	// The first run of Cogpit: the drain cordons worker-1, then the process dies.
	crash := &crashingJournal{DrainJournal: engine.NewFileDrainJournal(dir)}
	first, w1 := newActionsHarness(t, src, engine.Options{Drains: engine.NewDrains(crash), Confirmer: &confirmations{answer: true}, Logger: quietLogger})
	w1.setDrainHook(func(context.Context, string, func(cluster.DrainProgress)) error {
		crash.dead.Store(true)
		panic("Cogpit crashed")
	})
	var started commands.DrainResult
	first.must(t, engine.OriginGUI, "node.drain", actionNodeRef("worker-1"), &started)
	if job := waitJob(t, first, started.Job.ID); job.State != engine.JobFailed {
		t.Fatalf("the crashed drain's job: %+v", job)
	}
	if got := w1.Calls(); !slices.Equal(got, []string{"drain worker-1", "cordon worker-1"}) {
		t.Fatalf("first run's writes %v", got)
	}

	// What the crash left on disk: the entry as running, owner-only.
	path := filepath.Join(dir, engine.DrainJournalFile)
	assertMode(t, path, 0o600)
	assertMode(t, dir, 0o700)
	onDisk, err := engine.NewFileDrainJournal(dir).List()
	if err != nil || len(onDisk) != 1 || onDisk[0].State != engine.DrainRunning || onDisk[0].Node != "worker-1" || onDisk[0].StartedBy != engine.OriginGUI {
		t.Fatalf("journal after the crash: %+v, %v", onDisk, err)
	}

	// The next start of Cogpit, on the same journal and cluster.
	second, w2 := newActionsHarness(t, src, engine.Options{Drains: engine.NewDrains(engine.NewFileDrainJournal(dir)), Confirmer: &confirmations{answer: true}})
	events := reconnect(t, second)
	events.waitEvent(t, engine.EventDrains, "The drain of node worker-1 in context test did not finish")
	pending := pendingOf(t, second)
	if len(pending) != 1 || pending[0].Status != engine.DrainStatusInterrupted || pending[0].NodeState != engine.DrainNodeCordoned ||
		pending[0].Settings.TimeoutSeconds != 300 {
		t.Fatalf("pending after the restart: %+v", pending)
	}

	// Finish the drain: node.drain again, confirmed as usual.
	second.must(t, engine.OriginGUI, "node.drain", actionNodeRef("worker-1"), &started)
	if job := waitJob(t, second, started.Job.ID); job.State != engine.JobDone {
		t.Errorf("finish: %+v", job)
	}
	if len(pendingOf(t, second)) != 0 {
		t.Error("the finished drain is still pending")
	}

	// Uncordon instead: the drain is given up and forgotten.
	j := engine.NewFileDrainJournal(dir)
	if err := j.Put(onDisk[0]); err != nil {
		t.Fatal(err)
	}
	waitCordoned(t, second, "worker-1", true)
	second.must(t, engine.OriginGUI, "node.uncordon", actionNodeRef("worker-1"), nil)
	if len(pendingOf(t, second)) != 0 {
		t.Error("uncordon left the drain pending")
	}

	// Dismiss: forgotten, the cluster untouched.
	waitCordoned(t, second, "worker-1", false)
	second.must(t, engine.OriginGUI, "node.cordon", actionNodeRef("worker-1"), nil)
	if err := j.Put(onDisk[0]); err != nil {
		t.Fatal(err)
	}
	before := len(w2.Calls())
	var res struct {
		Ref    model.ObjectRef `json:"ref"`
		Action string          `json:"action"`
	}
	second.must(t, engine.OriginExternalAgent, "node.dismissDrain", actionNodeRef("worker-1"), &res)
	if res.Action != "dismissed" || res.Ref.Context != "test" || len(pendingOf(t, second)) != 0 || len(w2.Calls()) != before {
		t.Errorf("dismiss: %+v, pending %v, writes %v", res, pendingOf(t, second), w2.Calls()[before:])
	}
	if err := second.run(t, engine.OriginGUI, "node.dismissDrain", actionNodeRef("worker-1"), nil); err == nil || !strings.Contains(err.Error(), "no unfinished drain") {
		t.Errorf("dismiss twice: %v", err)
	}
	assertMode(t, path, 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s: mode %o, want %o", path, got, want)
	}
}

// TestPendingDrainOfAMissingNodeIsDropped: an entry whose node the cluster no longer has is
// dropped with a note on connect; entries of other contexts stay.
func TestPendingDrainOfAMissingNodeIsDropped(t *testing.T) {
	j := engine.NewMemoryDrainJournal()
	h, _ := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(j), Logger: quietLogger})
	started := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	for _, p := range []engine.PendingDrain{
		{Context: "test", Node: "gone", Started: started, State: engine.DrainRunning},
		{Context: "test", Node: "worker-2", Started: started.Add(time.Minute), State: engine.DrainFailed, Error: "timed out"},
		{Context: "other", Node: "elsewhere", Started: started, State: engine.DrainRunning},
	} {
		if err := j.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	events := reconnect(t, h)
	events.waitEvent(t, engine.EventToast, "dropped the unfinished drain of node gone (context test): the node no longer exists")
	events.waitEvent(t, engine.EventDrains, "The drain of node worker-2 in context test did not finish")
	entries, _ := j.List()
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Context+"/"+e.Node)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"other/elsewhere", "test/worker-2"}) {
		t.Errorf("journal %v", keys)
	}
	pending := pendingOf(t, h)
	if len(pending) != 1 || pending[0].Node != "worker-2" || pending[0].Status != engine.DrainStatusFailed || pending[0].NodeState != engine.DrainNodeCordoned {
		t.Errorf("pending %+v", pending)
	}
	var all []engine.PendingDrainInfo
	h.must(t, engine.OriginGUI, "node.pendingDrains", map[string]any{"all": true}, &all)
	if len(all) != 2 || all[0].Context != "other" || all[0].NodeState != engine.DrainNodeUnknown {
		t.Errorf("all pending %+v", all)
	}
}

// TestCorruptJournalIsReportedNotOverwritten: an unreadable journal is reported on connect and by
// node.pendingDrains, a drain is refused rather than started without a record, nothing panics,
// and the file is left as it was.
func TestCorruptJournalIsReportedNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, engine.DrainJournalFile)
	garbage := []byte(`{"version":1,"drains":[{"context":"test","node":`)
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	h, w := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(engine.NewFileDrainJournal(dir)), Confirmer: &confirmations{answer: true}, Logger: quietLogger})
	events := reconnect(t, h)
	events.waitEvent(t, engine.EventToast, "Unfinished drains cannot be offered: the drain journal "+path+" is unreadable")

	err := h.run(t, engine.OriginGUI, "node.pendingDrains", nil, nil)
	var jerr *engine.JournalError
	if !errors.As(err, &jerr) || jerr.Path != path {
		t.Errorf("pending drains from a corrupt journal: %v", err)
	}
	err = h.run(t, engine.OriginGUI, "node.drain", actionNodeRef("worker-1"), nil)
	if !errors.As(err, &jerr) || !strings.Contains(err.Error(), "could not record it first") {
		t.Errorf("drain with a corrupt journal: %v", err)
	}
	if len(w.Calls()) != 0 {
		t.Errorf("the drain ran without a record: %v", w.Calls())
	}
	if got, _ := os.ReadFile(path); string(got) != string(garbage) {
		t.Errorf("the corrupt journal was changed: %q", got)
	}
}

// TestDemoClusterHasNoNodeOrWorkloadWrites: the demo cluster cannot be changed; the commands say
// so, and which of Cordon and Uncordon applies is still known.
func TestDemoClusterHasNoNodeOrWorkloadWrites(t *testing.T) {
	s := engine.New(engine.Options{Connector: &actionsConnector{src: seedActions(), demo: true},
		SnapshotInterval: 10 * time.Millisecond, Drains: engine.NewDrains(engine.NewMemoryDrainJournal())})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	warmActionKinds(t, s)
	for id, params := range map[string]any{
		"node.cordon":       actionNodeRef("worker-1"),
		"node.uncordon":     actionNodeRef("worker-2"),
		"node.drain":        actionNodeRef("worker-1"),
		"deployment.scale":  map[string]any{"ref": model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}, "replicas": 2},
		"statefulset.scale": map[string]any{"ref": model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"}, "replicas": 2},
		"cronjob.trigger":   map[string]any{"ref": model.ObjectRef{Kind: "cronjobs", Namespace: "ops", Name: "backup"}},
		"cronjob.suspend":   map[string]any{"ref": model.ObjectRef{Kind: "cronjobs", Namespace: "ops", Name: "backup"}},
		"pod.evict":         map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}},
	} {
		_, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: mustJSON(t, params), Origin: engine.OriginGUI})
		var dis *engine.DisabledError
		if !errors.As(err, &dis) || !strings.Contains(dis.Reason, "demo") {
			t.Errorf("%s on the demo cluster: %v", id, err)
		}
	}
	cordon, _ := s.Registry().Get("node.cordon")
	if cordon.Applicable(s, mustJSON(t, actionNodeRef("worker-2"))) {
		t.Error("Cordon applies to a cordoned node on the demo cluster")
	}
}
