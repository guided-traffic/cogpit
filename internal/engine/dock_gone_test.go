package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The dock's tabs of a context that leaves Cogpit (G5, G13). A removed source's or a vanished
// file's cluster tabs close (UIState.dropContexts); the dock's tabs of those contexts (logs,
// terminals, a node shell, editors) close with them and are counted in the same notice; a node
// shell's pod is deleted as its tab's close deletes it (D-37); an editor's unsaved text is kept as
// a draft, offered when the cluster connects again (D-11), where closing a tab discards it.

// deletesSeen is NodeShellActions that records the pods deleted.
type deletesSeen struct {
	mu      sync.Mutex
	deleted []string
}

func (d *deletesSeen) CreatePod(context.Context, *corev1.Pod) (*corev1.Pod, error) {
	return nil, errors.New("not in this test")
}

func (d *deletesSeen) WaitPodRunning(context.Context, string, string, string) error { return nil }

func (d *deletesSeen) DeletePod(_ context.Context, namespace, name, uid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, namespace+"/"+name+" uid "+uid)
	return nil
}

func (d *deletesSeen) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.deleted)
}

// goneDock is a session over kubeconfig sources with drafts in its own directory (written only
// when kept: their delay is an hour), and the dock of two contexts: for ida a logs tab, a terminal,
// a node shell (its pod recorded), an Edit tab with unsaved text and a Create resource tab with
// none; for idb a terminal. ida's terminal is the dock's active tab.
type goneDock struct {
	s       *Session
	drafts  string // the drafts' directory
	shell   *deletesSeen
	notices *atomic.Value
	edit    string // ida's Edit tab
	other   string // idb's terminal
}

const goneDockText = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: shop\n  resourceVersion: \"41\"\ndata:\n  mode: slow\n"

func newGoneDock(t *testing.T, k *KubeconfigSources, files map[string]string, ida, idb string) *goneDock {
	t.Helper()
	g := &goneDock{drafts: t.TempDir(), shell: &deletesSeen{}, notices: &atomic.Value{}}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}
	d := newDrafts(g.drafts, newJournalDir(g.drafts)).WithDelay(time.Hour)
	g.s = New(Options{Connector: sourceConnector{fakeConnector: fc, k: k, file: files}, Drafts: d, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(g.s.Close)
	t.Cleanup(g.s.Subscribe(func(ev Event) {
		if ev.Type == EventToast {
			g.notices.Store(ev.Message)
		}
	}))
	s := g.s
	ref := model.ObjectRef{Context: ida, Kind: "pods", Namespace: "shop", Name: "web-0"}
	logs := DockTab{ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "Logs: web-0", Context: ida, Ref: &ref, Container: "app"}
	term := DockTab{ID: s.NewDockID(DockTerminal), Type: DockTerminal, Title: "Terminal 1", Context: ida}
	shell := DockTab{ID: s.NewDockID(DockNodeShell), Type: DockNodeShell, Title: "Node shell: n1", Context: ida,
		NodeShell: &NodeShellTab{Node: "n1", Pod: "cogpit-node-shell-abcdefgh", Image: DefaultNodeShellImage}}
	other := DockTab{ID: s.NewDockID(DockTerminal), Type: DockTerminal, Title: "Terminal 2", Context: idb}
	s.shells.add(shell.ID, &nodeShellPod{namespace: "kube-system", name: "cogpit-node-shell-abcdefgh", uid: "uid-shell", actions: g.shell})
	s.UpdateUI(func(u *UIState) {
		u.Tabs = []ViewTab{{ID: "tab-1", Context: ida, View: "pods"}, {ID: "tab-2", Context: idb, View: "pods"}}
		u.Dock = append(u.Dock, logs, term, shell, other)
		u.ActiveDock = term.ID
	})
	target := ObjectTarget{
		Ref:        model.ObjectRef{Context: ida, Kind: "configmaps", Namespace: "shop", Name: "settings"},
		APIVersion: "v1", Kind: "ConfigMap", Namespaced: true,
	}
	g.edit = goneDockEditor(t, s, DockEdit, ida, &target)
	goneDockEditor(t, s, DockCreate, ida, nil)
	if _, err := s.SetEditorText(g.edit, strings.Replace(goneDockText, "slow", "fast", 1)); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *UIState) { u.ActiveDock = term.ID })
	g.other = other.ID
	return g
}

// goneDockEditor puts an editor tab of context into the dock as the engine's openers do: an Edit
// tab on target at version 41, or a Create resource tab holding its starting text.
func goneDockEditor(t *testing.T, s *Session, typ DockType, ctxName string, target *ObjectTarget) string {
	t.Helper()
	e := &editor{tab: DockTab{ID: s.NewDockID(typ), Type: typ, Title: createTabTitle, Context: ctxName}, text: goneDockText, rev: 1}
	if target != nil {
		r := target.Ref
		e.tab.Ref, e.tab.Title = &r, target.Kind+" "+target.Ref.Name
		e.target, e.baseRV, e.baseUID, e.baseText = *target, "41", "uid-1", goneDockText
	} else {
		e.startText = goneDockText
	}
	s.putEditor(e)
	ui := s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, e.tab) })
	s.settleEditor(e.tab.ID, true, ui.Revision)
	return e.tab.ID
}

// check waits for ida's dock tabs to close and checks what is left: idb's terminal, shown; the node
// shell's pod deleted; the Edit tab's unsaved text kept as a draft of ida, offered.
func (g *goneDock) check(t *testing.T, ida string) {
	t.Helper()
	waitFor(t, "ida's dock tabs to close", func() bool { return len(g.s.UIState().Dock) == 1 })
	u := g.s.UIState()
	if u.Dock[0].ID != g.other || u.ActiveDock != g.other {
		t.Errorf("dock %+v, active %q; want idb's terminal alone, shown", u.Dock, u.ActiveDock)
	}
	waitFor(t, "the node shell's pod deleted", func() bool {
		return slices.Equal(g.shell.list(), []string{"kube-system/cogpit-node-shell-abcdefgh uid uid-shell"})
	})
	drafts := waitDrafts(t, g.drafts, "the Edit tab's draft kept", func(d []Draft) bool { return len(d) == 1 })
	if dr := drafts[0]; dr.Context != ida || dr.Type != DockEdit || !strings.Contains(dr.Text, "mode: fast") || dr.BaseResourceVersion != "41" {
		t.Errorf("draft %+v", dr)
	}
	list, err := g.s.EditorDrafts(true)
	if err != nil || len(list) != 1 || list[0].Context != ida {
		t.Errorf("offered drafts %+v, %v", list, err)
	}
	time.Sleep(3 * draftDelay) // a removal of the kept draft would be under way by now
	if got := draftFiles(t, g.drafts); len(got) != 1 {
		t.Errorf("the kept draft went: %+v", got)
	}
}

// A kubeconfig file leaves its sync folder: its context's dock tabs close with its cluster tab,
// counted in the same notice, which says where the unsaved text went.
func TestDockTabsCloseWhenTheirFileGoes(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "synced")
	fileA, fileB := filepath.Join(folder, "a.yaml"), filepath.Join(folder, "b.yaml")
	writeUserFile(t, fileA, sourceKubeconfig("a"))
	writeUserFile(t, fileB, sourceKubeconfig("b"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	ida, idb := "a [synced/a.yaml]", "b [synced/b.yaml]"
	g := newGoneDock(t, k, map[string]string{ida: fileA, idb: fileB}, ida, idb)
	stop := g.s.WatchKubeconfigs(time.Millisecond)
	t.Cleanup(stop)
	if err := os.Remove(fileA); err != nil {
		t.Fatal(err)
	}
	g.check(t, ida)
	if got := tabContexts(g.s.UIState()); !slices.Equal(got, []string{idb}) {
		t.Errorf("cluster tabs %q", got)
	}
	waitFor(t, "the notice", func() bool {
		msg, _ := g.notices.Load().(string)
		return msg == "Closed 6 tabs of a [synced/a.yaml]: its kubeconfig file is gone. "+
			"The unsaved text of 1 editor tab is kept as a draft, offered when its cluster connects again."
	})
}

// A kubeconfig source is removed: its contexts' dock tabs close with their cluster tabs, without a
// notice for the tabs (the user confirmed the removal), but with one for the unsaved text kept.
func TestDockTabsCloseWhenTheirSourceIsRemoved(t *testing.T) {
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
	ida, idb := "a [a.yaml]", "b [b.yaml]"
	g := newGoneDock(t, k, map[string]string{ida: fileA, idb: fileB}, ida, idb)
	if _, err := g.s.RemoveKubeconfigSource(srcA.ID); err != nil {
		t.Fatal(err)
	}
	g.check(t, ida)
	waitFor(t, "the drafts' notice", func() bool {
		msg, _ := g.notices.Load().(string)
		return msg == "The unsaved text of 1 editor tab of a [a.yaml] is kept as a draft, offered when its cluster connects again."
	})
}
