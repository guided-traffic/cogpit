package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// D-69 item 8: Preferences › App › Restore the last session (restore.go).

// v0170Workspace is a workspace file as Cogpit v0.17.0 wrote it: tabs and a column layout, no
// connections.
const v0170Workspace = `{
  "version": 1,
  "tabs": [
    {"context": "test", "view": "pods", "viewTitle": "Pods", "namespaces": ["shop"], "used": 2},
    {"context": "test", "view": "services", "viewTitle": "Services", "preview": true, "used": 1}
  ],
  "active": 0,
  "columnOrder": {"pods": ["name", "namespace", "status"]},
  "collapsedSources": ["~/.kube"],
  "navigatorHidden": true
}
`

// workspaceOnDisk decodes the workspace file in dir; false while there is none.
func workspaceOnDisk(dir string) (workspaceDoc, bool) {
	var doc workspaceDoc
	data, err := os.ReadFile(filepath.Join(dir, WorkspaceFile))
	if err != nil || json.Unmarshal(data, &doc) != nil {
		return doc, false
	}
	return doc, true
}

func writeWorkspaceText(t *testing.T, dir, text string) {
	t.Helper()
	if err := writePrivateFile(filepath.Join(dir, WorkspaceFile), []byte(text)); err != nil {
		t.Fatal(err)
	}
}

// The preference is off by default.
func TestRestoreSessionIsOffByDefault(t *testing.T) {
	if DefaultPreferences().RestoreSession {
		t.Fatal("restoring the last session is on by default")
	}
	s := New(Options{Connector: newPoolConnector(time.Minute), Preferences: NewPreferencesStore(t.TempDir())})
	t.Cleanup(s.Close)
	if p, _ := s.Preferences(); p.RestoreSession {
		t.Fatal("a first start restores the last session")
	}
}

// Off (the default), a start shows no tab and connects nothing, whatever the file holds; the
// window's arrangement (the column layout, the collapsed groups, the navigator hidden) comes back
// all the same. The file then keeps what this run has open.
func TestRestoreOffStartsWithoutTabsAndConnections(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceText(t, dir, strings.Replace(v0170Workspace, `"navigatorHidden": true`,
		`"navigatorHidden": true, "connected": ["test"], "shown": "test"`, 1))
	s, fc := workspaceSessionWith(t, dir, time.Millisecond, false)
	u := s.UIState()
	if len(u.Tabs) != 0 || u.ActiveTab != "" || u.Context != "" {
		t.Fatalf("tabs %+v, context %q at a start without the restore", u.Tabs, u.Context)
	}
	if !slices.Equal(u.ColumnOrder["pods"], []string{"name", "namespace", "status"}) || !u.NavigatorHidden ||
		!slices.Equal(u.Navigator.Collapsed, []string{"~/.kube"}) {
		t.Errorf("the window's arrangement was not restored: %v %v %v", u.ColumnOrder, u.NavigatorHidden, u.Navigator.Collapsed)
	}
	s.RestoreLastSession("")
	if n := connectionsOf(fc); n != 0 {
		t.Fatalf("%d connects at a start without the restore", n)
	}
	waitUntil(t, "the file without the last run's tabs", func() bool {
		doc, ok := workspaceOnDisk(dir)
		return ok && len(doc.Tabs) == 0 && len(doc.Connected) == 0
	})
}

// A workspace file of v0.17.0 (no connections) loads: with the preference on its tabs come back as
// before, nothing connects, and the file written back keeps them.
func TestWorkspaceOfV0170Loads(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceText(t, dir, v0170Workspace)
	s, fc := workspaceSession(t, dir, time.Millisecond)
	u := s.UIState()
	if len(u.Tabs) != 2 || u.ActiveTab != "tab-1" || u.Context != "test" || u.Connection.State != Disconnected || !u.NavigatorHidden {
		t.Fatalf("restored %+v", u)
	}
	s.RestoreLastSession("")
	if n := connectionsOf(fc); n != 0 {
		t.Fatalf("%d connects for a file without connections", n)
	}
	if notes := s.Notifications(false).Notifications; len(notes) != 0 {
		t.Errorf("notices %+v", notes)
	}
}

// What the file keeps for the restore: the clusters connected, the shown one; a disconnect takes
// its cluster out.
func TestWorkspaceKeepsTheConnections(t *testing.T) {
	dir := t.TempDir()
	pc := newPoolConnector(time.Minute, "alpha", "beta")
	s := New(Options{Connector: pc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch,
		Workspace: NewWorkspace(dir).WithDelay(time.Millisecond)})
	t.Cleanup(s.Close)
	mustConnect(t, s, "alpha")
	mustConnect(t, s, "beta")
	waitUntil(t, "both kept", func() bool {
		doc, ok := workspaceOnDisk(dir)
		return ok && slices.Equal(doc.Connected, []string{"alpha", "beta"}) && doc.Shown == "beta"
	})
	if err := s.DisconnectContext("alpha"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "alpha gone from the file", func() bool {
		doc, ok := workspaceOnDisk(dir)
		return ok && slices.Equal(doc.Connected, []string{"beta"}) && doc.Shown == "beta"
	})
}

// restoreStack is a session on client-go against two fake API servers, alpha and beta, whose
// Cogpit folder holds a workspace of the last session and the restore preference as given.
func restoreStack(t *testing.T, restore bool, workspace string) (*Session, *testkit.FakeAPIServer, *testkit.FakeAPIServer) {
	t.Helper()
	alpha := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(alpha.Close)
	beta := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(beta.Close)
	alpha.Set(testkit.PodsGVR, testkit.Pod("default", "alpha-web"))
	beta.Set(testkit.PodsGVR, testkit.Pod("default", "beta-web"))
	dir := filepath.Join(t.TempDir(), "Cogpit")
	writeWorkspaceText(t, dir, workspace)
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: []string{fakeClusterKubeconfig(t, alpha, "alpha"), fakeClusterKubeconfig(t, beta, "beta")}},
		SnapshotInterval: 10 * time.Millisecond, Workspace: NewWorkspace(dir).WithDelay(time.Millisecond),
		Preferences: restorePreferences(t, dir, restore),
	})
	t.Cleanup(s.Close)
	return s, alpha, beta
}

const lastSession = `{"version": 1, "active": 1, "tabs": [
  {"context": "alpha", "view": "pods", "viewTitle": "Pods", "used": 1},
  {"context": "beta", "view": "pods", "viewTitle": "Pods", "used": 2}
], "connected": ["alpha", "beta", "gone"], "shown": "beta"}`

// On, the start restores the tabs and connects the clusters of the last session: the one shown at
// quit is shown again with its tab, the other connects in the background and is not shown; a
// context no source offers any more is left out with a notice.
func TestRestoreOnConnectsTheLastSession(t *testing.T) {
	s, alpha, beta := restoreStack(t, true, lastSession)
	u := s.UIState()
	if len(u.Tabs) != 2 || u.Context != "beta" || u.Connection.State != Disconnected {
		t.Fatalf("before the restore's connects: %+v", u)
	}
	if len(alpha.Requests())+len(beta.Requests()) != 0 {
		t.Fatalf("requests before RestoreLastSession: %v %v", alpha.Requests(), beta.Requests())
	}
	s.RestoreLastSession("")
	waitTable(t, s, "pods", func(tb *TableSnapshot) bool { return tb.Synced && tb.Context == "beta" && len(tb.Rows) == 1 })
	waitUntil(t, "alpha connected in the background", func() bool {
		c, ok := s.UIState().ConnectionOf("alpha")
		return ok && c.Connection.State == Connected && !c.Shown
	})
	u = s.UIState()
	if u.Context != "beta" || u.Connection.State != Connected || u.ActiveTab != "tab-2" {
		t.Errorf("shown %q (%s), active tab %q", u.Context, u.Connection.State, u.ActiveTab)
	}
	if len(u.Tabs) != 2 {
		t.Errorf("tabs %+v", u.Tabs)
	}
	if podReads(alpha) != 0 {
		t.Errorf("alpha's pods were listed though it is not shown: %v", alpha.Requests())
	}
	var notice string
	for _, n := range s.Notifications(false).Notifications {
		if strings.Contains(n.Message, "gone") {
			notice = n.Message
		}
	}
	if notice != "Not connected again from the last session: no kubeconfig source offers gone any more." {
		t.Errorf("the notice for gone: %q", notice)
	}
}

// --context names the cluster the start shows: the restore connects the others, the one shown at
// quit among them, in the background.
func TestRestoreBehindAnExplicitContext(t *testing.T) {
	s, _, _ := restoreStack(t, true, lastSession)
	mustConnect(t, s, "alpha") // what the window does for --context alpha
	s.RestoreLastSession("alpha")
	waitUntil(t, "beta connected in the background", func() bool {
		c, ok := s.UIState().ConnectionOf("beta")
		return ok && c.Connection.State == Connected && !c.Shown
	})
	if u := s.UIState(); u.Context != "alpha" {
		t.Errorf("the restore showed %q over the explicit alpha", u.Context)
	}
}

// A tab on a view Cogpit does not have (retiredViews: the Applications view) is not restored, nor
// an entry of a tab's history or a column layout of one. The shown cluster's tab was that one, so
// its cluster comes back on its default view, Pods, which loads; the other cluster's tab stays.
func TestRestoreLeavesOutARetiredView(t *testing.T) {
	s, _, _ := restoreStack(t, true, `{"version": 1, "active": 1, "tabs": [
  {"context": "alpha", "view": "pods", "viewTitle": "Pods", "used": 1, "back": ["applications", "services"]},
  {"context": "beta", "view": "applications", "viewTitle": "Applications", "used": 2, "back": ["pods"]}
], "hiddenColumns": {"applications": ["managedBy"], "pods": ["qos"]}, "connected": ["alpha", "beta"], "shown": "beta"}`)
	u := s.UIState()
	if len(u.Tabs) != 1 || u.Tabs[0].Context != "alpha" || PlaceViews(u.Tabs[0].Back)[0] != "services" || len(u.Tabs[0].Back) != 1 || u.ActiveTab != "" {
		t.Fatalf("restored tabs %+v, active %q", u.Tabs, u.ActiveTab)
	}
	if _, ok := u.HiddenColumns["applications"]; ok || !slices.Equal(u.HiddenColumns["pods"], []string{"qos"}) {
		t.Errorf("hidden columns %v", u.HiddenColumns)
	}
	s.RestoreLastSession("")
	waitTable(t, s, "pods", func(tb *TableSnapshot) bool { return tb.Synced && tb.Context == "beta" && len(tb.Rows) == 1 })
	if u = s.UIState(); u.Context != "beta" || u.View != "pods" || slices.ContainsFunc(u.Tabs, func(t ViewTab) bool { return t.View == "applications" }) {
		t.Errorf("shown %q on %q, tabs %+v", u.Context, u.View, u.Tabs)
	}
}

// Off, nothing of the last session connects, and the start shows no tab.
func TestRestoreOffOnARealStack(t *testing.T) {
	s, alpha, beta := restoreStack(t, false, lastSession)
	s.RestoreLastSession("")
	if u := s.UIState(); len(u.Tabs) != 0 || len(u.Connections) != 0 || u.Context != "" {
		t.Fatalf("a start without the restore: %+v", u)
	}
	if len(alpha.Requests())+len(beta.Requests()) != 0 {
		t.Errorf("requests: %v %v", alpha.Requests(), beta.Requests())
	}
}

// A connect the user starts for a cluster the restore connects in the background shows that
// connection rather than opening a second one.
func TestConnectWaitsForTheBackgroundConnect(t *testing.T) {
	pc := newPoolConnector(time.Minute, "alpha", "beta")
	s := New(Options{Connector: pc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	mustConnect(t, s, "beta")
	if err := s.ConnectBackground(t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if c, ok := s.UIState().ConnectionOf("alpha"); !ok || c.Shown || c.Connection.State != Connected {
		t.Fatalf("alpha after the background connect: %+v", c)
	}
	waitUntil(t, "the background alpha cold", func() bool { c, _ := s.UIState().ConnectionOf("alpha"); return c.Cold })
	mustConnect(t, s, "alpha")
	if n := pc.connectCount("alpha"); n != 1 {
		t.Errorf("alpha connected %d times", n)
	}
	if u := s.UIState(); u.Context != "alpha" || u.Connection.State != Connected {
		t.Errorf("alpha not shown: %+v", u.Connection)
	}
	if err := s.ConnectBackground(t.Context(), "alpha"); err == nil {
		t.Error("a background connect of a connected context did not say so")
	}
}
