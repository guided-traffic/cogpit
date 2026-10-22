package engine

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The welcome page shows while the workspace has nothing else to show: before the first connect,
// after closing the last tab; not while a connect runs, nor while a tab is active, nor after the
// shown cluster's connect failed (its error page, D-114). Help › Welcome leaves the active tab,
// which keeps its state and brings it back.
func TestWelcomeFollowsTheConnectionAndTheTabs(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	if u := s.UIState(); !u.Welcome || u.Connection.State != Disconnected {
		t.Fatalf("a new session: welcome %v, %+v", u.Welcome, u.Connection)
	}
	var connecting []bool // the welcome flag of every Connecting state a connect shows
	cancel := s.Subscribe(func(ev Event) {
		if ev.Type == EventUIState && ev.UIState.Connection.State == Connecting {
			connecting = append(connecting, ev.UIState.Welcome)
		}
	})
	connectTest(t, s)
	cancel()
	if slices.Contains(connecting, true) || len(connecting) == 0 {
		t.Errorf("while connecting: welcome %v", connecting)
	}
	if u := s.UIState(); u.Welcome || u.ActiveTab == "" {
		t.Fatalf("connected: welcome %v, active tab %q", u.Welcome, u.ActiveTab)
	}

	// Help › Welcome over the connected cluster: no tab active, no view; the tab keeps its state.
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	s.UpdateUI(func(u *UIState) { u.Filter, u.Selected, u.Drawer = "web", []model.ObjectRef{pod}, &pod })
	s.UpdateUI(func(u *UIState) { u.Palette = true })
	u, err := s.OpenWelcome()
	if err != nil {
		t.Fatal(err)
	}
	if !u.Welcome || u.ActiveTab != "" || u.View != "" || u.Drawer != nil || u.Palette || len(u.Tabs) != 1 {
		t.Fatalf("welcome over a cluster: %+v", u)
	}
	if tab := u.Tabs[0]; tab.View != "pods" || tab.Filter != "web" || tab.Drawer == nil || *tab.Drawer != pod {
		t.Errorf("the tab left: %+v", tab)
	}
	u, err = s.SelectTab(context.Background(), u.Tabs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Welcome || u.View != "pods" || u.Filter != "web" || u.Drawer == nil {
		t.Errorf("the tab back: %+v", u)
	}

	// Opening a view ends the page too, and closing the last tab shows it again.
	s.OpenWelcome() //nolint:errcheck
	if u = s.OpenTab("services", false); u.Welcome || u.View != "services" {
		t.Errorf("a view opened from the welcome page: %+v", u)
	}
	if u, _ = s.CloseTab(""); !u.Welcome || len(u.Tabs) != 0 {
		t.Errorf("the last tab closed: welcome %v, tabs %v", u.Welcome, tabSummary(u))
	}

	// A failed connect: its error page, not the welcome page (D-114), with the connection's error
	// and its stages for the window to show.
	if err := s.Connect(context.Background(), "broken"); err == nil {
		t.Fatal("broken connected")
	}
	if u = s.UIState(); u.Welcome || u.Connection.State != ConnectError || u.Connection.Error == "" ||
		u.ConnectFailure == nil || u.ConnectFailure.Context != "broken" {
		t.Errorf("a failed connect: %+v", u)
	}
}

// ui.welcome.open is refused while a connect runs: the connect decides what the workspace shows.
func TestWelcomeRefusedWhileConnecting(t *testing.T) {
	s, _ := newTestSession(t, testkit.NewMemorySource(), 10*time.Millisecond)
	s.UpdateUI(func(u *UIState) { u.Context, u.Connection = "slow", Connection{State: Connecting} })
	if err := s.CheckOpenWelcome(); !errors.Is(err, errWelcomeConnecting) {
		t.Errorf("check while connecting: %v", err)
	}
	if u, err := s.OpenWelcome(); !errors.Is(err, errWelcomeConnecting) || u.Welcome {
		t.Errorf("open while connecting: %v, welcome %v", err, u.Welcome)
	}
}

// Disconnect closes the connection without a word to the cluster: Disconnected without an error,
// no backend, the welcome page, and the cluster's tabs closed (the owner's A61), another cluster's
// tab kept; connecting again shows the cluster's default view in a new preview tab. Without a
// connection it is refused; during a connect it ends the connect.
func TestDisconnect(t *testing.T) {
	s := tabSession(t)
	s.OpenTab("services", true)
	waitTable(t, s, "services", func(ts *TableSnapshot) bool { return ts.Synced })
	s.UpdateUI(func(u *UIState) { // a tab of another cluster, as a restart leaves one
		u.Tabs = append(u.Tabs, ViewTab{ID: "tab-9", Context: "other", View: "nodes", Title: "Nodes - other"})
	})
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	if u.Connection != (Connection{State: Disconnected}) || u.Context != "test" || u.View != "" || !u.Welcome || s.Backend() != nil {
		t.Fatalf("after disconnect: %+v, backend %v", u, s.Backend())
	}
	wantTabs(t, u, "tab-9 nodes@other")
	if u.ActiveTab != "" {
		t.Errorf("a tab stays active: %q", u.ActiveTab)
	}
	if lines := s.ConnectLog("test"); len(lines) == 0 || !strings.Contains(lines[len(lines)-1].Text, "Disconnected from test") ||
		lines[len(lines)-1].Level != LogInfo {
		t.Errorf("connect log %+v", lines)
	}
	if err := s.Disconnect(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("disconnect without a connection: %v", err)
	}
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	u = s.UIState()
	if u.Connection.State != Connected || u.View != "pods" || u.Welcome {
		t.Errorf("connected again: %+v", u)
	}
	wantTabs(t, u, "tab-9 nodes@other", "tab-3 pods@test p *")

	// A connect under way: Disconnect ends it, and the connect leaves the UI alone.
	done := make(chan error, 1)
	go func() { done <- s.Connect(context.Background(), "slow") }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == Connecting }) {
		t.Fatal("the slow connect never started")
	}
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Error("the slow connect succeeded")
	}
	if u = s.UIState(); u.Connection != (Connection{State: Disconnected}) || u.Context != "slow" || !u.Welcome {
		t.Errorf("after ending a connect: %+v", u)
	}
}

// A context's link: an added file is removable, with every context the file defines; a file of the
// default ~/.kube or of another synced folder is removable with its whole folder, every context of
// the folder's files leaving with it (A97), and only against the list the confirmation named; a
// file $KUBECONFIG names is refused with the reason, and so is the demo cluster. Removing unlinks
// the file or folder only: the files stay on disk unchanged.
func TestContextLinksAndRemove(t *testing.T) {
	su := newSessionSetup(t)
	added := filepath.Join(t.TempDir(), "team.yaml")
	writeUserFile(t, added, twoContextKubeconfig("team-a", "team-b"))
	folder := filepath.Join(t.TempDir(), "synced")
	writeUserFile(t, filepath.Join(folder, "edge.yaml"), sourceKubeconfig("edge"))
	before := treeState(t, filepath.Dir(added))
	k := su.open()
	if _, _, err := k.AddFile(added); err != nil {
		t.Fatal(err)
	}
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Connector: &ClusterConnector{Sources: k}})
	t.Cleanup(s.Close)

	links, err := s.ContextLinks()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ContextLink{}
	for _, l := range links {
		got[l.Context] = l
	}
	for id, want := range map[string]string{
		"main [.kube/config]":     "folder " + su.kube,
		"alpha [a.yaml]":          "named by $KUBECONFIG or --kubeconfig for this run",
		"edge [synced/edge.yaml]": "folder " + folder,
		"team-a [team.yaml]":      "",
		"team-b [team.yaml]":      "",
		"beta [b.yaml]":           "named by $KUBECONFIG",
	} {
		l, ok := got[id]
		folderOf, isFolder := strings.CutPrefix(want, "folder ")
		switch {
		case !ok:
			t.Errorf("no link for %s in %+v", id, links)
		case want == "" && (!l.Removable || l.Reason != "" || l.Folder != "" || l.File != added || !slices.Equal(l.Contexts, []string{"team-a [team.yaml]", "team-b [team.yaml]"})):
			t.Errorf("%s: %+v, want removable with both contexts of %s", id, l, added)
		case isFolder && (!l.Removable || l.Reason != "" || l.Folder != folderOf || !slices.Contains(l.Contexts, id)):
			t.Errorf("%s: %+v, want removable with its folder %s", id, l, folderOf)
		case want != "" && !isFolder && (l.Removable || !strings.HasPrefix(l.Reason, want)):
			t.Errorf("%s: %+v, want refused: %q…", id, l, want)
		}
		if one, err := s.ContextLink(id); err != nil || one.Context != id || one.Removable != l.Removable || !slices.Equal(one.Contexts, l.Contexts) {
			t.Errorf("ContextLink(%s) = %+v, %v", id, one, err)
		}
		if err := s.CheckRemoveContext(id); (err == nil) != (want == "" || isFolder) {
			t.Errorf("CheckRemoveContext(%s) = %v", id, err)
		}
	}
	if l, err := s.ContextLink(DemoContext); err != nil || l.Removable || !strings.Contains(l.Reason, "demo") {
		t.Errorf("the demo cluster's link: %+v, %v", l, err)
	}
	if _, err := s.ContextLink("nowhere [x.yaml]"); err == nil {
		t.Error("a context no source offers has a link")
	}
	if _, err := s.RemoveContext("alpha [a.yaml]", nil); err == nil {
		t.Error("removed a context $KUBECONFIG names")
	}

	l, err := s.RemoveContext("team-b [team.yaml]", nil)
	if err != nil || l.Source.Path != added {
		t.Fatalf("RemoveContext: %+v, %v", l, err)
	}
	names := contextNames(t, k)
	if slices.Contains(names, "team-a [team.yaml]") || slices.Contains(names, "team-b [team.yaml]") || !slices.Contains(names, "edge [synced/edge.yaml]") {
		t.Errorf("contexts after the removal: %v", names)
	}
	sameTree(t, before, treeState(t, filepath.Dir(added)))

	// A folder: only against the contexts the confirmation named. A file that joins the folder
	// after the confirmation refuses the removal; asked again, it goes with the rest.
	shown := got["edge [synced/edge.yaml]"].Contexts
	if _, err := s.RemoveContext("edge [synced/edge.yaml]", nil); !errors.Is(err, ErrLinkChanged) {
		t.Errorf("a folder removed without the confirmed list: %v", err)
	}
	writeUserFile(t, filepath.Join(folder, "late.yaml"), sourceKubeconfig("late"))
	beforeFolder := treeState(t, filepath.Dir(folder))
	if _, err := s.RemoveContext("edge [synced/edge.yaml]", shown); !errors.Is(err, ErrLinkChanged) || !strings.Contains(err.Error(), "late") {
		t.Errorf("a folder removed although a context joined it: %v", err)
	}
	now, err := s.ContextLink("edge [synced/edge.yaml]")
	if err != nil || !slices.Equal(now.Contexts, []string{"edge [synced/edge.yaml]", "late [synced/late.yaml]"}) {
		t.Fatalf("the folder's link: %+v, %v", now, err)
	}
	if l, err := s.RemoveContext("edge [synced/edge.yaml]", []string{"late [synced/late.yaml]", "edge [synced/edge.yaml]"}); err != nil || l.Folder != folder {
		t.Fatalf("RemoveContext of the folder: %+v, %v", l, err)
	}
	names = contextNames(t, k)
	if slices.Contains(names, "edge [synced/edge.yaml]") || slices.Contains(names, "late [synced/late.yaml]") || !slices.Contains(names, "main [.kube/config]") {
		t.Errorf("contexts after the folder's removal: %v", names)
	}
	sameTree(t, beforeFolder, treeState(t, filepath.Dir(folder)))

	// The default ~/.kube goes the same way, its files untouched.
	main, err := s.ContextLink("main [.kube/config]")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveContext("main [.kube/config]", main.Contexts); err != nil {
		t.Fatalf("RemoveContext of the default folder: %v", err)
	}
	if names := contextNames(t, k); slices.Contains(names, "main [.kube/config]") {
		t.Errorf("contexts after the default folder's removal: %v", names)
	}
	su.untouched(t)

	bare := New(Options{})
	defer bare.Close()
	if _, err := bare.ContextLinks(); err == nil {
		t.Error("links without sources")
	}
}

// twoContextKubeconfig is a kubeconfig file with two contexts on one cluster.
func twoContextKubeconfig(a, b string) string {
	return `apiVersion: v1
kind: Config
clusters:
- {name: c, cluster: {server: "https://127.0.0.1:1"}}
users:
- {name: u, user: {token: t}}
contexts:
- {name: ` + a + `, context: {cluster: c, user: u}}
- {name: ` + b + `, context: {cluster: c, user: u}}
current-context: ` + a + `
`
}
