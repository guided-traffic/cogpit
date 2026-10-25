package fyneui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// restoredWorkspace is a workspace file of the last run: three tabs of the kubeconfig's
// contexts, kind-dev's Pods shown last, and one of a context the kubeconfig no longer has.
const restoredWorkspace = `{"version": 1, "active": 0, "tabs": [
  {"context": "kind-dev", "view": "pods", "viewTitle": "Pods", "namespaces": ["team-a"], "used": 3},
  {"context": "kind-dev", "view": "deployments", "viewTitle": "Deployments", "used": 1},
  {"context": "staging", "view": "services", "viewTitle": "Services", "preview": true, "used": 2},
  {"context": "old-cluster", "view": "pods", "viewTitle": "Pods"}
], "columnOrder": {"pods": ["name", "namespace", "status"]}}`

// restoredEngine is a real engine whose kubeconfig's contexts all point at server (empty: the
// welcome page's renders' unreachable address, so that the PNGs do not change from run to run), and
// whose Cogpit folder holds restoredWorkspace (a file as v0.17.0 wrote it, without connections) and
// the preference "Restore the last session" on (D-69 item 8: off, no tab comes back).
func restoredEngine(t *testing.T, server string) *engine.Session {
	t.Helper()
	return restoredEngineWith(t, server, true)
}

// restoredEngineWith is restoredEngine with the restore preference as given.
func restoredEngineWith(t *testing.T, server string, restore bool) *engine.Session {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, engine.WorkspaceFile), []byte(restoredWorkspace), 0o600); err != nil {
		t.Fatal(err)
	}
	if restore {
		if err := os.WriteFile(filepath.Join(cfg, engine.PreferencesFile), []byte(`{"version": 1, "restoreSession": true}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	kc := filepath.Join(t.TempDir(), "config")
	config := settingsKubeconfig
	if server != "" {
		config = strings.ReplaceAll(config, "https://127.0.0.1:1", server)
	}
	if err := os.WriteFile(kc, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kc}},
		Hotbar: engine.NewHotbar(cfg), Preferences: engine.NewPreferencesStore(cfg),
		ClusterSettings: engine.NewClusterSettingsStore(cfg), Workspace: engine.NewWorkspace(cfg)})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// countingServer is an API server that answers nothing and counts what it was asked.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func tabTitles(sh *shell) []string {
	out := make([]string, 0, len(sh.tabs.shown))
	for _, t := range sh.tabs.shown {
		out = append(out, t.tab.Title)
	}
	return out
}

// A start with the tabs of the last run (D-41, the preference Restore the last session on, D-69
// item 8) shows the tab bar at once, the tab shown last marked, and the welcome page, the
// navigator's search closed (D-42, D-57) and without a request to any cluster: the file, as
// v0.17.0 wrote it, names no connection to restore, and the window itself connects nothing (the
// app's RestoreLastSession does, engine/restore.go). The tabs of a context that is gone are not
// restored and a toast says so once the window is up. Showing a tab is what connects its cluster.
func TestStartShowsRestoredTabsWithoutConnecting(t *testing.T) {
	srv, requests := countingServer(t)
	s := restoredEngine(t, srv.URL)
	sh, _, q := newEngineShell(t, s, Options{})
	welcomeShown(t, sh, q)
	// Every tab is restored, in the UI state; the bar shows none while no cluster is connected (the
	// owner's GW13; GW11: then the shown cluster's, the others' with their cluster), the bar itself
	// empty (LS11).
	q.until(t, "the restored tabs", func() bool { return len(sh.ui.Tabs) == 3 })
	q.drain()
	if len(sh.tabs.shown) != 0 || !sh.tabs.root.Visible() || sh.ui.Connection.State != engine.Disconnected {
		t.Errorf("the bar shows %d tabs before a connect, or connected: %+v", len(sh.tabs.shown), sh.ui.Connection)
	}
	if sh.ui.ActiveTab != sh.ui.Tabs[0].ID {
		t.Errorf("the tab shown last is not the active one: %q", sh.ui.ActiveTab)
	}
	if sh.side.head.searching() {
		t.Error("the navigator's search opened by itself at start")
	}

	sh.showStartNotices() // what Run does once the window is up
	cards := toastCards(sh)
	if len(cards) != 1 {
		t.Fatalf("%d toasts at start, want the notice", len(cards))
	}
	notice := "1 tab of the last run was not restored: no kubeconfig source offers old-cluster any more."
	if texts := findAll[*paragraph](cards[0]); len(texts) != 1 || texts[0].text != notice {
		t.Errorf("the toast says %+v", texts)
	}
	q.drain()
	if n := requests.Load(); n != 0 || s.Backend() != nil {
		t.Fatalf("%d requests to the cluster before a tab was shown", n)
	}

	// No tab shows before a connect (GW13): the active tab selected by its id, as the hotbar's
	// tile or an agent does, connects its cluster.
	sh.dispatch("ui.tab.select", map[string]string{"id": sh.ui.ActiveTab}, nil)
	q.until(t, "the tab's connect to ask its cluster", func() bool { return requests.Load() > 0 })
}

// Without the preference (the owner's default, D-69 item 8) the start shows no tab of the last run
// and says nothing about them: the welcome page alone, nothing connected.
func TestStartWithoutTheRestoreShowsNoTabs(t *testing.T) {
	srv, requests := countingServer(t)
	s := restoredEngineWith(t, srv.URL, false)
	sh, _, q := newEngineShell(t, s, Options{})
	welcomeShown(t, sh, q)
	q.drain()
	if len(sh.tabs.shown) != 0 || len(s.UIState().Tabs) != 0 {
		t.Errorf("tabs at a start without the restore: %q", tabTitles(sh))
	}
	sh.showStartNotices()
	if cards := toastCards(sh); len(cards) != 0 {
		t.Errorf("%d toasts at start", len(cards))
	}
	if n := requests.Load(); n != 0 || s.Backend() != nil {
		t.Fatalf("%d requests at a start without the restore", n)
	}
}

// The start screen with the tabs of the last run, before a connect: the welcome page, no tab bar
// (GW13).
func TestRenderStartRestoredDark(t *testing.T)  { renderStartRestored(t, ThemeDark) }
func TestRenderStartRestoredLight(t *testing.T) { renderStartRestored(t, ThemeLight) }

func renderStartRestored(t *testing.T, mode ThemeMode) {
	s := restoredEngine(t, "")
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	welcomeShown(t, sh, q)
	q.until(t, "the restored tabs", func() bool { return len(sh.ui.Tabs) == 3 })
	q.drain()
	savePNG(t, w, "start_restored_"+string(mode)+".png")
	if s.Backend() != nil || sh.side.head.searching() {
		t.Error("connected, or the navigator's search opened, while rendering the start")
	}
}
