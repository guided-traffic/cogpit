package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// sourceConnector is the test connector whose contexts come from kubeconfig source files: it
// connects like fakeConnector and records the file the connection was made from.
type sourceConnector struct {
	*fakeConnector
	k    *KubeconfigSources
	file map[string]string // context id → its kubeconfig file
}

func (c sourceConnector) KubeconfigSources() *KubeconfigSources { return c.k }

func (c sourceConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if b != nil {
		b.SourceFile = c.file[name]
	}
	return b, err
}

// goneFixture is a session connected to "a", whose kubeconfig lies in a sync folder, with the
// watcher running; toasts are counted.
type goneFixture struct {
	s      *Session
	k      *KubeconfigSources
	folder string
	file   string
	toasts atomic.Int64
	last   atomic.Value // string: the last toast's message
}

func newGoneFixture(t *testing.T, asFolder bool) *goneFixture {
	t.Helper()
	g := &goneFixture{folder: t.TempDir()}
	g.file = filepath.Join(g.folder, "a.yaml")
	writeUserFile(t, g.file, sourceKubeconfig("a"))
	g.k = NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	var err error
	if asFolder {
		_, err = g.k.AddFolder(g.folder)
	} else {
		_, _, err = g.k.AddFile(g.file)
	}
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: sourceConnector{fakeConnector: fc, k: g.k, file: map[string]string{"a": g.file}}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	g.s = s
	t.Cleanup(s.Subscribe(func(ev Event) {
		if ev.Type == EventToast {
			g.toasts.Add(1)
			g.last.Store(ev.Message)
		}
	}))
	stop := s.WatchKubeconfigs(5 * time.Millisecond)
	t.Cleanup(stop)
	if err := s.Connect(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestConnectionClosesWhenItsFileLeavesASyncFolder: the connected cluster's kubeconfig file is
// deleted from a synced folder; the next poll closes the connection, the UI is Disconnected with
// the reason, a warning toast says so, the context's connect log ends in the reason, and the list
// of contexts marks it.
func TestConnectionClosesWhenItsFileLeavesASyncFolder(t *testing.T) {
	g := newGoneFixture(t, true)
	if g.s.Backend() == nil {
		t.Fatal("not connected")
	}
	if err := os.Remove(g.file); err != nil {
		t.Fatal(err)
	}
	// The backend goes first and the UI state follows: wait for what is asserted (testing.md).
	waitFor(t, "the connection to close", func() bool {
		return g.s.Backend() == nil && g.s.UIState().Connection.State == Disconnected
	})
	ui := g.s.UIState()
	if ui.Connection.State != Disconnected || ui.Context != "a" || ui.View != "" ||
		!strings.Contains(ui.Connection.Error, "Disconnected from a") || !strings.Contains(ui.Connection.Error, g.file) {
		t.Errorf("UI state %+v", ui)
	}
	waitFor(t, "the toast", func() bool { return g.toasts.Load() == 1 })
	if msg, _ := g.last.Load().(string); msg != ui.Connection.Error {
		t.Errorf("toast %q, state %q", msg, ui.Connection.Error)
	}
	lines := g.s.ConnectLog("a")
	if n := len(lines); n < 3 || lines[n-1].Level != LogError || lines[n-1].Text != ui.Connection.Error {
		t.Errorf("connect log %+v", lines)
	}
	if !g.s.state.logs.problem("a") {
		t.Error("the context is not marked")
	}
}

// TestConnectionClosesWhenAnAddedFileIsDeleted is the same for a file added on its own.
func TestConnectionClosesWhenAnAddedFileIsDeleted(t *testing.T) {
	g := newGoneFixture(t, false)
	if err := os.Remove(g.file); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the connection to close", func() bool {
		return g.s.Backend() == nil && g.s.UIState().Connection.State == Disconnected
	})
}

// TestConnectionStaysWhileItsFileIsOnlyRewritten: truncating, rewriting with garbage and rewriting
// again must not close a connection built from the file; only its absence does. A sibling file
// leaving the folder does not touch it either.
func TestConnectionStaysWhileItsFileIsOnlyRewritten(t *testing.T) {
	g := newGoneFixture(t, true)
	other := filepath.Join(g.folder, "other.yaml")
	writeUserFile(t, other, sourceKubeconfig("other"))
	var events atomic.Int64
	defer g.s.Subscribe(func(ev Event) {
		if ev.Type == EventContexts {
			events.Add(1)
		}
	})()
	for _, content := range []string{"", "{{ not yaml", sourceKubeconfig("a")} {
		before := events.Load()
		writeUserFile(t, g.file, content+"\n# "+content)
		waitFor(t, "a poll to see the change", func() bool { return events.Load() > before })
	}
	before := events.Load()
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a poll to see the sibling go", func() bool { return events.Load() > before })
	if g.s.Backend() == nil || g.s.UIState().Connection.State != Connected || g.toasts.Load() != 0 {
		t.Errorf("the connection closed: %+v", g.s.UIState().Connection)
	}
}

// TestConnectionClosesWhenItsSourceIsRemoved: removing the source (the link) closes a connection
// made from its files, though the file is still on disk; nothing else does.
func TestConnectionClosesWhenItsSourceIsRemoved(t *testing.T) {
	g := newGoneFixture(t, true)
	if g.s.CloseGoneSourceConnection() {
		t.Fatal("closed with the source in place")
	}
	if _, err := g.k.Remove(filepath.Base(g.folder) + "/"); err != nil {
		t.Fatal(err)
	}
	// The watcher (5 ms) may close it first; either way it is closed, exactly once.
	g.s.CloseGoneSourceConnection()
	// The backend is detached before the UI state says Disconnected (disconnectBackend closes it in
	// between): wait for both, or the check below reads the state in that gap (a CI flake).
	waitFor(t, "the connection to close after the source was removed", func() bool {
		return g.s.Backend() == nil && g.s.UIState().Connection.State == Disconnected
	})
	ui := g.s.UIState()
	if ui.Connection.State != Disconnected || !strings.Contains(ui.Connection.Error, "source was removed") ||
		!strings.Contains(ui.Connection.Error, "untouched") {
		t.Errorf("UI state %+v", ui.Connection)
	}
	if _, err := os.Stat(g.file); err != nil {
		t.Errorf("the file is gone: %v", err)
	}
	if g.s.CloseGoneSourceConnection() {
		t.Error("closed twice")
	}
	// The user's own act: an INFO line and a disconnect, not an error (the walk of 2026-10-19).
	got, _ := g.s.ConnectAttempts("a", 1)
	if len(got) != 1 || got[0].Kind != AttemptDisconnect || got[0].Outcome != OutcomeOK || strings.Contains(attemptText(got[0]), "ERROR") {
		t.Errorf("the source's removal in the log: %+v", got)
	}
}

// TestGoneFileDoesNotTouchANewerConnect: a connection to another context started after the file
// went is left alone; the gone file's connection, connected in the background since (D-69), closes
// without changing what the window shows, and only once.
func TestGoneFileDoesNotTouchANewerConnect(t *testing.T) {
	g := newGoneFixture(t, true)
	b := g.s.Backend()
	if err := g.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if !g.s.disconnectBackend(b, "late", false) {
		t.Error("the background connection of the gone file stayed")
	}
	if g.s.disconnectBackend(b, "late", false) {
		t.Error("closed twice")
	}
	ui := g.s.UIState()
	if ui.Context != "test" || ui.Connection.State != Connected {
		t.Errorf("UI state %+v", ui)
	}
	if _, ok := ui.ConnectionOf(b.Context); ok || len(ui.Connections) != 1 {
		t.Errorf("connections %+v", ui.Connections)
	}
}
