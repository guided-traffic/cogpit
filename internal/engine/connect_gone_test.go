package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A connect still running when its context leaves Cogpit (G5, G13; FIXUP 2026-10-14). The watcher
// closes the tabs of a context whose kubeconfig file is gone, and a removed source's, but leaves
// UIState.Context alone while a connect holds it (Connecting). Before, the connect then failed and
// left Context naming a context that is gone (ConnectError), or succeeded and left a connection
// made from a file that is gone, until the next change of the sources. Now the connect's end
// forgets the context, and a connection that succeeded is closed, with the reason.

// gatedConnector holds every connect until gate closes, then fails it or connects as
// sourceConnector does.
type gatedConnector struct {
	sourceConnector
	gate chan struct{}
	fail bool
}

func (c gatedConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	select {
	case <-c.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if c.fail {
		return nil, errors.New("dial tcp 10.0.0.1:6443: i/o timeout")
	}
	return c.sourceConnector.Connect(ctx, name)
}

// connectWhileGone connects ida, whose file is in a sync folder, and while the connect waits,
// removes the file (or, with removeSource, the folder's source) and lets the watcher close ida's
// tab; then the connect ends, failing or not. It returns the session after the connect returned.
func connectWhileGone(t *testing.T, fail, removeSource bool) (*Session, error) {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "synced")
	file := filepath.Join(folder, "a.yaml")
	writeUserFile(t, file, sourceKubeconfig("a"))
	writeUserFile(t, filepath.Join(folder, "b.yaml"), sourceKubeconfig("b"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	src, err := k.AddFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	ida := "a [synced/a.yaml]"
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	gc := gatedConnector{sourceConnector: sourceConnector{fakeConnector: fc, k: k, file: map[string]string{ida: file}}, gate: make(chan struct{}), fail: fail}
	s := New(Options{Connector: gc, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	s.UpdateUI(func(u *UIState) { u.Tabs = []ViewTab{{ID: "tab-1", Context: ida, View: "pods"}} })
	stop := s.WatchKubeconfigs(time.Millisecond)
	t.Cleanup(stop)

	done := make(chan error, 1)
	go func() { done <- s.Connect(context.Background(), ida) }()
	waitFor(t, "the connect to start", func() bool { u := s.UIState(); return u.Connection.State == Connecting && u.Context == ida })
	if removeSource {
		if _, err := s.RemoveKubeconfigSource(src.ID); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the watcher to close ida's tab", func() bool { return len(s.UIState().Tabs) == 0 })
	if u := s.UIState(); u.Context != ida || u.Connection.State != Connecting {
		t.Fatalf("while connecting: context %q, %+v", u.Context, u.Connection)
	}
	close(gc.gate)
	return s, <-done
}

func TestConnectEndsWhenItsContextLeftCogpit(t *testing.T) {
	for _, tc := range []struct {
		name               string
		fail, removeSource bool
		reason             string
	}{
		{"a failed connect, its file gone", true, false, "Could not connect to a [synced/a.yaml]: its kubeconfig file "},
		{"a connect that succeeded, its file gone", false, false, "Disconnected from a [synced/a.yaml]: its kubeconfig file "},
		{"a failed connect, its source removed", true, true, "Could not connect to a [synced/a.yaml]: its kubeconfig source was removed"},
		{"a connect that succeeded, its source removed", false, true, "Disconnected from a [synced/a.yaml]: its kubeconfig source was removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := connectWhileGone(t, tc.fail, tc.removeSource)
			if (err != nil) != tc.fail {
				t.Errorf("connect: %v", err)
			}
			u := s.UIState()
			if u.Context != "" || u.Connection.State != Disconnected || !strings.HasPrefix(u.Connection.Error, tc.reason) || s.Backend() != nil {
				t.Errorf("after the connect: context %q, %+v, backend %v; want no context, Disconnected with %q", u.Context, u.Connection, s.Backend() != nil, tc.reason)
			}
			if len(u.Tabs) != 0 || u.ActiveTab != "" {
				t.Errorf("tabs %+v, active %q", u.Tabs, u.ActiveTab)
			}
		})
	}
}

// The window's connect whose backend the kubeconfig watcher closed between its publishing and its
// Connected, the watcher's change of the UI still on its way, forgets the context that left
// Cogpit (settleClosedConnect): the run's own settle found the window Connecting and left it, and
// a race-detector run on CI (2026-10-23) saw the connect return with the window "connecting".
func TestAClosedConnectForgetsItsGoneContext(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "synced")
	file := filepath.Join(folder, "a.yaml")
	writeUserFile(t, file, sourceKubeconfig("a"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	src, err := k.AddFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	ida := "a [synced/a.yaml]"
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: sourceConnector{fakeConnector: fc, k: k, file: map[string]string{ida: file}}})
	t.Cleanup(s.Close)
	s.UpdateUI(func(u *UIState) {
		u.Context, u.Connection = ida, Connection{State: Connecting}
		u.Tabs = []ViewTab{{ID: "tab-1", Context: ida, View: "pods"}}
	})
	if _, err := k.Remove(src.ID); err != nil {
		t.Fatal(err)
	}
	s.forgetGoneContext(ida, "the run's settle", true, false) // as the run's settle found it
	if u := s.UIState(); u.Context != ida || u.Connection.State != Connecting {
		t.Fatalf("the run's settle changed the window: %q %+v", u.Context, u.Connection)
	}
	s.settleClosedConnect(ida)
	u := s.UIState()
	want := "Disconnected from a [synced/a.yaml]: its kubeconfig source was removed"
	if u.Context != "" || u.Connection.State != Disconnected || !strings.HasPrefix(u.Connection.Error, want) || len(u.Tabs) != 0 {
		t.Errorf("after the window's connect ended: context %q, %+v, tabs %d; want no context, Disconnected with %q", u.Context, u.Connection, len(u.Tabs), want)
	}
}

// A connect whose context stays is left alone: a failed one keeps its context and its error, as
// before.
func TestFailedConnectOfAContextThatStaysKeepsIt(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "synced")
	file := filepath.Join(folder, "a.yaml")
	writeUserFile(t, file, sourceKubeconfig("a"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	ida := "a [synced/a.yaml]"
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	gate := make(chan struct{})
	close(gate)
	s := New(Options{Connector: gatedConnector{sourceConnector: sourceConnector{fakeConnector: fc, k: k, file: map[string]string{ida: file}}, gate: gate, fail: true},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), ida); err == nil {
		t.Fatal("the connect did not fail")
	}
	if u := s.UIState(); u.Context != ida || u.Connection.State != ConnectError || !strings.Contains(u.Connection.Error, "i/o timeout") {
		t.Errorf("context %q, %+v", u.Context, u.Connection)
	}
}
