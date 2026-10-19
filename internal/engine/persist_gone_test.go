package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// sourcesSession is a session over the fake connector whose contexts come from kubeconfig sources;
// a connection records the file it was made from, as the desktop app's does. Toasts are recorded.
func sourcesSession(t *testing.T, k *KubeconfigSources, files map[string]string) (*Session, *atomic.Value) {
	t.Helper()
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: sourceConnector{fakeConnector: fc, k: k, file: files}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	var last atomic.Value
	t.Cleanup(s.Subscribe(func(ev Event) {
		if ev.Type == EventToast {
			last.Store(ev.Message)
		}
	}))
	return s, &last
}

func tabContexts(u UIState) []string {
	out := make([]string, 0, len(u.Tabs))
	for _, t := range u.Tabs {
		out = append(out, t.Context)
	}
	return out
}

// Removing a cluster from Cogpit forgets UIState.Context too when it named that cluster: the
// connection made from its file closes, its tabs close, and nothing points at the context any more
// (the navigator shows its section, the welcome page the reason). A failed connect's context goes
// the same way; a context another source still offers stays.
func TestRemovedSourceForgetsTheContext(t *testing.T) {
	root := t.TempDir()
	fileA, fileB := filepath.Join(root, "a.yaml"), filepath.Join(root, "b.yaml")
	writeUserFile(t, fileA, sourceKubeconfig("a"))
	writeUserFile(t, fileB, sourceKubeconfig("b"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	srcA, _, err := k.AddFile(fileA)
	if err != nil {
		t.Fatal(err)
	}
	srcB, _, err := k.AddFile(fileB)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := sourcesSession(t, k, map[string]string{"a [a.yaml]": fileA, "b [b.yaml]": fileB})
	if err := s.Connect(context.Background(), "a [a.yaml]"); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *UIState) {
		u.Tabs = append(u.Tabs, ViewTab{ID: "tab-9", Context: "b [b.yaml]", View: "nodes"})
	})
	if _, err := s.RemoveKubeconfigSource(srcA.ID); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	if u.Context != "" || u.Connection.State != Disconnected || !strings.Contains(u.Connection.Error, "its kubeconfig source was removed") ||
		u.ActiveTab != "" || u.NavigatorScope != NavScopeLocal || !u.Welcome || s.Backend() != nil {
		t.Errorf("after removing a.yaml: %+v", u)
	}
	if got := tabContexts(u); !slices.Equal(got, []string{"b [b.yaml]"}) {
		t.Errorf("tabs %q", got)
	}

	// A connect to b failed (the state names b with the error); removing b forgets both.
	s.UpdateUI(func(u *UIState) {
		u.Context, u.Connection = "b [b.yaml]", Connection{State: ConnectError, Error: "refused"}
	})
	if _, err := s.RemoveKubeconfigSource(srcB.ID); err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.Context != "" || u.Connection != (Connection{State: Disconnected}) || len(u.Tabs) != 0 {
		t.Errorf("after removing b.yaml: %+v", u)
	}
}

// A context whose kubeconfig file leaves a synced folder loses its tabs with a notice, and
// UIState.Context forgets it; a file that is only rewritten, even into one that does not parse,
// keeps its tabs, as it keeps its connection.
func TestTabsCloseWhenTheirFileLeavesASyncFolder(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "synced")
	fileA, fileB := filepath.Join(folder, "a.yaml"), filepath.Join(folder, "b.yaml")
	writeUserFile(t, fileA, sourceKubeconfig("a"))
	writeUserFile(t, fileB, sourceKubeconfig("b"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	ida, idb := "a [synced/a.yaml]", "b [synced/b.yaml]"
	s, last := sourcesSession(t, k, map[string]string{ida: fileA, idb: fileB})
	s.UpdateUI(func(u *UIState) { // a was connected, then disconnected: its tab is still the marked one
		u.Context = ida
		u.Tabs = []ViewTab{
			{ID: "tab-1", Context: ida, View: "pods"}, {ID: "tab-2", Context: idb, View: "pods"},
			{ID: "tab-3", Context: ida, View: "nodes", Preview: true},
		}
		u.ActiveTab = "tab-1"
	})
	stop := s.WatchKubeconfigs(time.Millisecond)
	t.Cleanup(stop)

	writeUserFile(t, fileB, "apiVersion: v1\nkind: Config\nclusters: [") // rewritten, not gone
	if err := os.Remove(fileA); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a's tabs to close", func() bool { return len(s.UIState().Tabs) == 1 })
	u := s.UIState()
	if got := tabContexts(u); !slices.Equal(got, []string{idb}) || u.Context != "" || u.ActiveTab != "" || !u.Welcome {
		t.Errorf("after a.yaml went: tabs %q, context %q, active %q", got, u.Context, u.ActiveTab)
	}
	waitFor(t, "the notice", func() bool {
		msg, _ := last.Load().(string)
		return msg == "Closed 2 tabs of a [synced/a.yaml]: its kubeconfig file is gone."
	})
}

// Removing a source, as Remove from Cogpit does, takes its clusters off the hotbar (the owner's
// WF45 of 2026-10-19); a tile of a context another source still offers, and one whose file went
// away by itself, stay (the latter marked missing).
func TestRemovedSourceLeavesTheHotbar(t *testing.T) {
	root := t.TempDir()
	fileA, fileB := filepath.Join(root, "a.yaml"), filepath.Join(root, "b.yaml")
	writeUserFile(t, fileA, sourceKubeconfig("a"))
	writeUserFile(t, fileB, sourceKubeconfig("b"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	srcA, _, err := k.AddFile(fileA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.AddFile(fileB); err != nil {
		t.Fatal(err)
	}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	s := New(Options{Connector: sourceConnector{fakeConnector: fc, k: k, file: map[string]string{"a [a.yaml]": fileA, "b [b.yaml]": fileB}},
		Hotbar: NewHotbar(t.TempDir()), SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if _, err := s.ChangeHotbar(func(h *HotbarState) error {
		h.Clusters = []HotbarTile{{Context: "a [a.yaml]"}, {Context: "b [b.yaml]"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveKubeconfigSource(srcA.ID); err != nil {
		t.Fatal(err)
	}
	st, _ := s.HotbarState()
	if got := tiles(st); got != "b [b.yaml]" {
		t.Errorf("tiles after removing a.yaml: %q", got)
	}
	if h := s.UIState().Hotbar; h == nil || len(h.Clusters) != 1 {
		t.Errorf("the UI's hotbar %+v", h)
	}
	// b's file goes by itself: its tile stays, marked missing.
	if err := os.Remove(fileB); err != nil {
		t.Fatal(err)
	}
	view, _ := s.HotbarView()
	if len(view.Clusters) != 1 || !view.Clusters[0].Missing {
		t.Errorf("the gone file's tile %+v", view.Clusters)
	}
}
