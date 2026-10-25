package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// workspaceSession is a session over the fake connector ("test" and "broken") whose workspace is
// kept in dir, written delay after a change, with the preference "Restore the last session" on
// (D-69 item 8: off, no tab comes back); closed at cleanup.
func workspaceSession(t *testing.T, dir string, delay time.Duration) (*Session, *fakeConnector) {
	t.Helper()
	return workspaceSessionWith(t, dir, delay, true)
}

// workspaceSessionWith is workspaceSession with the restore preference as given.
func workspaceSessionWith(t *testing.T, dir string, delay time.Duration, restore bool) (*Session, *fakeConnector) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	s := New(Options{Connector: fc, SnapshotInterval: 5 * time.Millisecond, Workspace: NewWorkspace(dir).WithDelay(delay),
		Preferences: restorePreferences(t, dir, restore)})
	t.Cleanup(s.Close)
	return s, fc
}

// restorePreferences keeps the preferences in dir, the restore of the last session as given; it
// writes them only when dir has none yet, so that a session started again keeps what was set.
func restorePreferences(t *testing.T, dir string, restore bool) *PreferencesStore {
	t.Helper()
	path := filepath.Join(dir, PreferencesFile)
	if _, err := os.Stat(path); err == nil || !restore {
		return NewPreferencesStore(dir)
	}
	if err := writePrivateFile(path, []byte(`{"version": 1, "restoreSession": true}`)); err != nil {
		t.Fatal(err)
	}
	return NewPreferencesStore(dir)
}

// readWorkspace decodes the workspace file in dir.
func readWorkspace(t *testing.T, dir string) workspaceDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, WorkspaceFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc workspaceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return doc
}

func writeWorkspace(t *testing.T, dir string, doc workspaceDoc) {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(dir, WorkspaceFile), data); err != nil {
		t.Fatal(err)
	}
}

func connectionsOf(fc *fakeConnector) int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.connects
}

// The tabs and the column layout written by one session come back in the next: the tabs in their
// order with their views, the preview tab, each tab's namespace and group filters, search and its
// toggles, sort and history, the tab shown last marked, its cluster named but not connected; the
// hidden columns and the column order per view. A selection and the drawer are not kept. Nothing
// connects until a tab is shown; then the tab shows its own state.
func TestWorkspaceRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	a, _ := workspaceSession(t, dir, time.Hour) // written at close only
	connectTest(t, a)                           // tab-1: pods, the preview tab
	a.NoteViewTitle("services", "Services")
	a.OpenTab("services", false) // the preview tab shows services, pods in its history
	a.OpenTab("services", true)  // and is fixed
	a.UpdateUI(func(u *UIState) {
		u.Namespaces, u.Filter, u.FilterOptions = []string{"shop"}, "check", FilterOptions{MatchCase: true}
		u.Sort = SortSpec{Column: "age", Desc: true}
		u.Selected = []model.ObjectRef{{Kind: "services", Namespace: "shop", Name: "checkout"}}
		u.Drawer = &model.ObjectRef{Kind: "services", Namespace: "shop", Name: "checkout"}
		u.HiddenColumns = map[string][]string{"pods": {"ip", "node"}}
		u.ColumnOrder = map[string][]string{"services": {"name", "type", "namespace"}}
	})
	u := a.OpenTab("pods", false) // a new preview tab right of it
	wantTabs(t, u, "tab-1 services@test", "tab-2 pods@test p *")
	if _, err := a.SelectTab(context.Background(), "tab-1"); err != nil { // services is shown last
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, WorkspaceFile)); err == nil {
		t.Fatal("written before the delay or the close")
	}
	a.Close()

	doc := readWorkspace(t, dir)
	if len(doc.Tabs) != 2 || doc.Active != 0 || doc.Tabs[0].ViewTitle != "Services" || doc.Tabs[1].ViewTitle == "" {
		t.Fatalf("file %+v", doc)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, WorkspaceFile))
	if strings.Contains(string(raw), "checkout") || strings.Contains(string(raw), "selected") || strings.Contains(string(raw), "drawer") {
		t.Errorf("the file keeps a selection or the drawer: %s", raw)
	}
	if fi, err := os.Stat(filepath.Join(dir, WorkspaceFile)); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("file mode %v, %v", fi.Mode(), err)
	}

	b, fc := workspaceSession(t, dir, time.Hour)
	u = b.UIState()
	wantTabs(t, u, "tab-1 services@test *", "tab-2 pods@test p")
	if u.Context != "test" || u.Connection.State != Disconnected || !u.Welcome || u.View != "" || u.NavigatorScope != NavScopeLocal {
		t.Errorf("restored: context %q, connection %+v, welcome %v, view %q, scope %q", u.Context, u.Connection, u.Welcome, u.View, u.NavigatorScope)
	}
	svc := u.Tabs[0]
	if svc.Title != "Services - test" || !slices.Equal(svc.Namespaces, []string{"shop"}) || svc.Filter != "check" ||
		!svc.FilterOptions.MatchCase || svc.Sort != (SortSpec{Column: "age", Desc: true}) || svc.Selected != nil ||
		svc.Drawer != nil || !slices.Equal(PlaceViews(svc.Back), []string{"pods"}) {
		t.Errorf("the services tab %+v", svc)
	}
	if pods := u.Tabs[1]; pods.Title != "Pods - test" {
		t.Errorf("the pods tab %+v", pods)
	}
	if !slices.Equal(u.HiddenColumns["pods"], []string{"ip", "node"}) || !slices.Equal(u.ColumnOrder["services"], []string{"name", "type", "namespace"}) {
		t.Errorf("layout: hidden %v, order %v", u.HiddenColumns, u.ColumnOrder)
	}
	if n := connectionsOf(fc); n != 0 {
		t.Fatalf("%d connects at start", n)
	}

	// Showing the marked tab connects its cluster and shows the tab's own state.
	if _, err := b.SelectTab(context.Background(), "tab-1"); err != nil {
		t.Fatal(err)
	}
	u = b.UIState()
	if connectionsOf(fc) != 1 || u.Connection.State != Connected || u.ActiveTab != "tab-1" || u.View != "services" ||
		u.Filter != "check" || !slices.Equal(u.Namespaces, []string{"shop"}) || u.Sort.Column != "age" {
		t.Errorf("after showing the tab: %+v", u)
	}
}

// A cluster connected from the picker after a restart shows its most recently used tab, as in the
// run before.
func TestWorkspaceKeepsWhichTabWasUsedLast(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: -1, Tabs: []storedTab{
		{Context: "test", View: "pods", Used: 2},
		{Context: "test", View: "services", Used: 3},
		{Context: "test", View: "configmaps", Used: 1},
	}})
	s, _ := workspaceSession(t, dir, time.Hour)
	if u := s.UIState(); u.ActiveTab != "" || u.Context != "" || len(u.Tabs) != 3 {
		t.Fatalf("restored %+v", u)
	}
	connectTest(t, s)
	if u := s.UIState(); u.ActiveTab != "tab-2" || u.View != "services" {
		t.Errorf("connected: active %q, view %q", u.ActiveTab, u.View)
	}
}

// A workspace file keeps one preview tab per cluster (D-117): one written before, with one preview
// tab in all, loads as it was; a second preview tab of a cluster becomes a fixed one.
func TestWorkspaceKeepsOnePreviewTabPerCluster(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: -1, Tabs: []storedTab{
		{Context: "test", View: "pods", Preview: true},
		{Context: "broken", View: "pods"}, // the fake connector offers test and broken
		{Context: "broken", View: "nodes", Preview: true},
		{Context: "test", View: "services", Preview: true},
	}})
	s, _ := workspaceSession(t, dir, time.Hour)
	wantTabs(t, s.UIState(), "tab-1 pods@test p", "tab-2 pods@broken", "tab-3 nodes@broken p", "tab-4 services@test")
}

// The tabs of a context no kubeconfig source offers any more are dropped with a notice, and the
// file is written again without them; the tabs of the offered ones stay.
func TestWorkspaceDropsTabsOfGoneContexts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: 1, Tabs: []storedTab{
		{Context: "test", View: "pods"},
		{Context: "gone", View: "nodes"},
		{Context: "gone", View: "pods", Preview: true},
	}})
	s, fc := workspaceSession(t, dir, 5*time.Millisecond)
	u := s.UIState()
	wantTabs(t, u, "tab-1 pods@test")
	if u.Context != "" || u.ActiveTab != "" {
		t.Errorf("the shown tab was dropped, yet context %q, active %q", u.Context, u.ActiveTab)
	}
	notes := s.Notifications(false).Notifications
	if len(notes) != 1 || notes[0].Message != "2 tabs of the last run were not restored: no kubeconfig source offers gone any more." {
		t.Errorf("notices %+v", notes)
	}
	waitFor(t, "the file without them", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, WorkspaceFile))
		return err == nil && !strings.Contains(string(data), "gone")
	})
	if n := connectionsOf(fc); n != 0 {
		t.Errorf("%d connects", n)
	}
}

// With kubeconfig sources, a tab is dropped only on a plain fact: its file is gone, its source was
// removed, or its file parses and no longer defines it. A file that does not parse keeps its tabs.
func TestWorkspaceDropsOnlyWhatIsCertainlyGone(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "synced")
	writeUserFile(t, filepath.Join(folder, "kept.yaml"), sourceKubeconfig("kept"))
	writeUserFile(t, filepath.Join(folder, "broken.yaml"), sourceKubeconfig("broken"))
	added := filepath.Join(root, "team.yaml")
	writeUserFile(t, added, sourceKubeconfig("team-a"))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, err := k.AddFolder(folder); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.AddFile(added); err != nil {
		t.Fatal(err)
	}
	// After the last run: broken.yaml is half written, a folder file is gone, team.yaml lost team-b.
	writeUserFile(t, filepath.Join(folder, "broken.yaml"), "apiVersion: v1\nkind: Config\nclusters: [")
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: 0, Tabs: []storedTab{
		{Context: "kept [synced/kept.yaml]", View: "pods"},
		{Context: "broken [synced/broken.yaml]", View: "pods"},
		{Context: "old [synced/old.yaml]", View: "pods"},
		{Context: "team-a [team.yaml]", View: "pods"},
		{Context: "team-b [team.yaml]", View: "pods"},
		{Context: "x [removed.yaml]", View: "pods"},
		{Context: "x [elsewhere/y.yaml]", View: "pods"},
	}})
	s := New(Options{Connector: &ClusterConnector{Sources: k}, Workspace: NewWorkspace(dir).WithDelay(time.Hour),
		Preferences: restorePreferences(t, dir, true)})
	t.Cleanup(s.Close)
	var got []string
	for _, tab := range s.UIState().Tabs {
		got = append(got, tab.Context)
	}
	if want := []string{"kept [synced/kept.yaml]", "broken [synced/broken.yaml]", "team-a [team.yaml]"}; !slices.Equal(got, want) {
		t.Errorf("restored %q, want %q", got, want)
	}
	if u := s.UIState(); u.Context != "kept [synced/kept.yaml]" || u.ActiveTab != "tab-1" {
		t.Errorf("context %q, active %q", u.Context, u.ActiveTab)
	}
	notes := s.Notifications(false).Notifications
	if len(notes) != 1 || !strings.HasPrefix(notes[0].Message, "4 tabs of the last run were not restored: no kubeconfig source offers old [synced/old.yaml], team-b [team.yaml], x [removed.yaml] and 1 more") {
		t.Errorf("notices %+v", notes)
	}
}

// A file that does not load is ignored, never a crash: the session starts without tabs, says so,
// and keeps the file aside as workspace.json.unreadable at the first write.
func TestWorkspaceUnreadableFileIsKeptAside(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":      "{not json",
		"newer version": `{"version": 99, "tabs": [{"context": "test", "view": "pods"}]}`,
		"wrong types":   `{"version": 1, "tabs": "pods"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "Cogpit")
			if err := writePrivateFile(filepath.Join(dir, WorkspaceFile), []byte(content)); err != nil {
				t.Fatal(err)
			}
			s, _ := workspaceSession(t, dir, 5*time.Millisecond)
			if u := s.UIState(); len(u.Tabs) != 0 || u.Context != "" {
				t.Fatalf("restored from an unreadable file: %+v", u.Tabs)
			}
			notes := s.Notifications(false).Notifications
			if len(notes) != 1 || notes[0].Level != LevelWarning || !strings.Contains(notes[0].Message, "were not restored") {
				t.Errorf("notices %+v", notes)
			}
			if data, _ := os.ReadFile(filepath.Join(dir, WorkspaceFile)); string(data) != content {
				t.Fatal("the unreadable file was touched before a change")
			}
			connectTest(t, s)
			waitFor(t, "the new file", func() bool {
				data, err := os.ReadFile(filepath.Join(dir, WorkspaceFile))
				return err == nil && string(data) != content && strings.Contains(string(data), `"context": "test"`)
			})
			if aside, err := os.ReadFile(filepath.Join(dir, WorkspaceFile+".unreadable")); err != nil || string(aside) != content {
				t.Errorf("kept aside %q, %v", aside, err)
			}
		})
	}
}

// Changes are written once they stop: many changes within the delay write nothing yet, and the
// close writes the last of them once.
func TestWorkspaceWritesAfterTheChangesStop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := workspaceSession(t, dir, time.Hour)
	connectTest(t, s)
	for i := range 20 {
		s.UpdateUI(func(u *UIState) { u.Filter = "web-" + string(rune('a'+i)) })
	}
	w := s.Workspace()
	w.mu.Lock()
	pending := w.pending != nil
	w.mu.Unlock()
	if _, err := os.Stat(filepath.Join(dir, WorkspaceFile)); err == nil || !pending {
		t.Fatalf("written before the delay (pending %v, %v)", pending, err)
	}
	s.Close()
	if doc := readWorkspace(t, dir); len(doc.Tabs) != 1 || doc.Tabs[0].Filter != "web-t" {
		t.Errorf("file %+v", doc)
	}
}

// A change written after the delay; a change undone before it writes nothing; a session without a
// directory keeps nothing.
func TestWorkspaceTimer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := workspaceSession(t, dir, 5*time.Millisecond)
	connectTest(t, s)
	waitFor(t, "the first write", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, WorkspaceFile))
		return err == nil && strings.Contains(string(data), `"view": "pods"`)
	})
	s.UpdateUI(func(u *UIState) { u.ColumnOrder = map[string][]string{"pods": {"name", "age"}} })
	waitFor(t, "the column order", func() bool {
		doc := readWorkspace(t, dir)
		return slices.Equal(doc.ColumnOrder["pods"], []string{"name", "age"})
	})

	none := New(Options{Connector: &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles()}, Workspace: NewWorkspace("")})
	connectTest(t, none)
	none.Close()
	if none.Workspace().Path() != "" {
		t.Error("a workspace without a directory has a file")
	}
}

// The workspace's writer runs beside the UI's changes: run with -race.
func TestWorkspaceWriterRace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := workspaceSession(t, dir, time.Millisecond)
	connectTest(t, s)
	done := make(chan struct{})
	for g := range 4 {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range 50 {
				s.UpdateUI(func(u *UIState) { u.Filter = strings.Repeat("x", g+i%3) })
				if i%10 == 0 {
					s.OpenTab("services", i%20 == 0)
				}
			}
		}()
	}
	for range 4 {
		<-done
	}
	s.Close()
	if doc := readWorkspace(t, dir); len(doc.Tabs) == 0 {
		t.Errorf("file %+v", doc)
	}
}

// Nothing at start sends a request to a cluster: a session restoring tabs of a real context (its
// server counts requests) starts, lists its contexts and reads its UI state without one; showing a
// tab is what connects.
func TestWorkspaceNoRequestAtStart(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	kc := filepath.Join(t.TempDir(), "dev.yaml")
	writeUserFile(t, kc, strings.Replace(sourceKubeconfig("dev"), "https://dev.example:6443", srv.URL, 1))
	k := NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"))
	if _, _, err := k.AddFile(kc); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: 1, Tabs: []storedTab{
		{Context: "dev [dev.yaml]", View: "pods"}, {Context: "dev [dev.yaml]", View: "nodes", Preview: true},
	}})
	s := New(Options{Connector: &ClusterConnector{Sources: k}, Workspace: NewWorkspace(dir).WithDelay(time.Millisecond),
		Preferences: restorePreferences(t, dir, true)})
	t.Cleanup(s.Close)
	stop := s.WatchKubeconfigs(time.Millisecond)
	t.Cleanup(stop)
	if _, err := s.Contexts(); err != nil {
		t.Fatal(err)
	}
	u := s.UIState()
	if len(u.Tabs) != 2 || u.ActiveTab != "tab-2" || u.Context != "dev [dev.yaml]" || u.Connection.State != Disconnected {
		t.Fatalf("restored %+v", u)
	}
	s.UpdateUI(func(u *UIState) { u.Palette = true })
	if n := requests.Load(); n != 0 || s.Backend() != nil {
		t.Fatalf("%d requests before a tab was shown", n)
	}
	if _, err := s.SelectTab(context.Background(), "tab-2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the tab's connect to ask the cluster", func() bool { return requests.Load() > 0 })
}

// After a restart, the tab the user clicks is the one shown once its cluster connects, not the
// one shown last before the restart (marked active, its cluster named but not connected, D-41).
func TestWorkspaceClickedTabWinsOverTheRestoredRank(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspace(t, dir, workspaceDoc{Version: workspaceVersion, Active: 1, Tabs: []storedTab{
		{Context: "test", View: "pods", Used: 2},
		{Context: "test", View: "services", Used: 3},
		{Context: "test", View: "configmaps", Used: 1},
	}})
	s, _ := workspaceSession(t, dir, time.Hour)
	if _, err := s.SelectTab(context.Background(), "tab-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "connected", func() bool { u := s.UIState(); return u.connected() })
	if u := s.UIState(); u.ActiveTab != "tab-1" || u.View != "pods" {
		t.Errorf("clicked tab-1 (pods), shown: active %q, view %q", u.ActiveTab, u.View)
	}
}
