package fyneui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// findScene is the section scene drilled into kind-cogpit, with cluster.connect's label and ⌘P from
// the real registry.
func findScene(t *testing.T) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := sectionFake(t)
	if c, ok := realCommands(t).Get(connectCommand); ok {
		probe[probeContext](t, f, c)
	}
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the section's clusters", func() bool { return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 })
	test.Tap(sh.side.sec.rows[secKind])
	q.until(t, "kind-cogpit's tree", func() bool { return sh.side.tree.Visible() })
	return sh, w, q, f
}

// escape presses Escape in the navigator's search field.
func escape(sh *shell) { sh.side.head.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape}) }

// The Cluster menu's first item is Connect to Cluster… with ⌘P (cluster.connect's label and
// shortcut, D-57, I6): it shows the navigator's section with its search open and focused (I4); ↵
// drills into the first match. Escape on the empty search goes back to the cluster's tree it came
// from; with text it clears the text first. From the section itself Escape only closes the search.
func TestConnectToClusterShowsTheSection(t *testing.T) {
	sh, w, q, f := findScene(t)
	h, c := sh.side.head, w.Canvas()
	cluster := sh.mainMenu().Items[0]
	item := cluster.Items[0]
	sc, ok := item.Shortcut.(*desktop.CustomShortcut)
	if cluster.Label != "Cluster" || item.Label != "Connect to Cluster…" || !ok || sc.KeyName != fyne.KeyP ||
		sc.Modifier != fyne.KeyModifierShortcutDefault {
		t.Fatalf("the Cluster menu's first item %q %+v", item.Label, item.Shortcut)
	}
	searchShown := func() bool { return sh.side.sec.Visible() && h.searching() && c.Focused() == h.entry }

	item.Action()
	q.until(t, "the section's search", searchShown)
	if h.entry.PlaceHolder != "Search clusters…" || sh.side.findFrom != secKind || h.back.Visible() || !h.addB.Visible() {
		t.Errorf("placeholder %q, from %q, back %v, + %v", h.entry.PlaceHolder, sh.side.findFrom, h.back.Visible(), h.addB.Visible())
	}
	if got := scopeCalls(t, f); !slices.Equal(got, []string{"cluster:" + secKind, engine.NavScopeLocal}) {
		t.Errorf("scopes %q", got)
	}

	escape(sh) // the empty search: back to kind-cogpit
	q.until(t, "kind-cogpit's tree again", func() bool { return sh.side.tree.Visible() && !h.searching() })
	if sh.side.findFrom != "" || sh.ui.Context != secKind {
		t.Errorf("from %q, context %q", sh.side.findFrom, sh.ui.Context)
	}

	item.Action()
	q.until(t, "the section's search again", searchShown)
	test.Type(h.entry, "pay")
	escape(sh) // a text: cleared, the search stays
	if h.entry.Text != "" || !h.searching() || !sh.side.sec.Visible() {
		t.Fatalf("Escape on a text: %q, search %v", h.entry.Text, h.searching())
	}
	escape(sh)
	q.until(t, "kind-cogpit's tree a third time", func() bool { return sh.side.tree.Visible() && !h.searching() })

	item.Action()
	q.until(t, "the search for prod", searchShown)
	test.Type(h.entry, "payments")
	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "prod's tree", func() bool { return sh.side.tree.Visible() && sh.ui.Context == secProd })
	if sh.side.findFrom != "" || h.searching() {
		t.Errorf("after ↵: from %q, search %v", sh.side.findFrom, h.searching())
	}

	test.Tap(h.back)
	q.until(t, "the section", func() bool { return sh.side.sec.Visible() })
	item.Action()
	q.until(t, "the search at the section", searchShown)
	if sh.side.findFrom != "" {
		t.Errorf("from %q at the section", sh.side.findFrom)
	}
	escape(sh) // came from no cluster: the search closes, the section stays
	q.drain()
	if h.searching() || !sh.side.sec.Visible() {
		t.Errorf("Escape at the section: search %v, section %v", h.searching(), sh.side.sec.Visible())
	}
	want := []string{"cluster:" + secKind, engine.NavScopeLocal, "cluster:" + secKind, engine.NavScopeLocal, "cluster:" + secKind,
		engine.NavScopeLocal, "cluster:" + secProd, engine.NavScopeLocal}
	if got := scopeCalls(t, f); !slices.Equal(got, want) {
		t.Errorf("scopes %q, want %q", got, want)
	}
}

// ⌘P over a navigator ⌘B hid shows it first (ui.toggleNavigator, open), then the section with its
// search focused; the keyboard reaches the search only once the navigator shows.
func TestConnectToClusterShowsAHiddenNavigator(t *testing.T) {
	f := sectionFake(t)
	f.ui.NavigatorHidden = true
	f.ui.NavigatorScope = engine.NavScopeCluster(secKind)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the hidden navigator", func() bool { return sh.uiApplied && sh.ui.NavigatorHidden && len(sh.kinds) > 0 })
	sh.findCluster()
	h := sh.side.head
	q.until(t, "the section's search", func() bool {
		return !sh.ui.NavigatorHidden && sh.side.sec.Visible() && h.searching() && w.Canvas().Focused() == h.entry
	})
	calls := f.callsOf("ui.toggleNavigator")
	if len(calls) != 1 {
		t.Fatalf("%d ui.toggleNavigator", len(calls))
	}
	if p := decodeParams[layoutToggleParams](t, calls[0]); p.Open == nil || !*p.Open {
		t.Errorf("ui.toggleNavigator %+v, want open", p)
	}
	if got := scopeCalls(t, f); !slices.Equal(got, []string{engine.NavScopeLocal}) {
		t.Errorf("scopes %q", got)
	}
}

// ⌘P over the open palette takes its place.
func TestConnectToClusterOverThePalette(t *testing.T) {
	sh, w, q, _, _ := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	sh.connectMenuItem().Action()
	h := sh.side.head
	q.until(t, "the section's search", func() bool {
		return !sh.palette.visible() && !sh.ui.Palette && sh.side.sec.Visible() && w.Canvas().Focused() == h.entry
	})
}

// ⌘P over a settings page closes it first (the page takes the navigator's place), as Escape does.
func TestConnectToClusterClosesASettingsPage(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", struct{}{})
	q.until(t, "the Preferences", func() bool { return sh.settings.visible() })
	sh.connectMenuItem().Action()
	h := sh.side.head
	q.until(t, "the section's search", func() bool {
		return !sh.settings.visible() && sh.side.sec.Visible() && h.searching() && w.Canvas().Focused() == h.entry
	})
	if u := s.UIState(); u.Settings != nil {
		t.Errorf("the engine still shows %+v", u.Settings)
	}
}

// welcomeButtons returns the labels of the welcome page's buttons with a label (a cluster row's
// warning button has none).
func welcomeButtons(w *welcomePage) []string {
	var out []string
	for _, b := range findAll[*widget.Button](w.root) {
		if b.Text != "" {
			out = append(out, b.Text)
		}
	}
	return out
}

// The welcome page has no Connect to Cluster… (the owner, 2026-10-16 evening, N122): Add
// Kubeconfigs… is its one button; the workspace's button while no cluster is connected stays
// Connect to Cluster… (it read All Clusters…, D-57) and does what ⌘P does.
func TestConnectToClusterButtons(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	sh, q := welcomeOnFake(t, f, ThemeDark)
	w := sh.tabs.welcome
	if got := welcomeButtons(w); !slices.Equal(got, []string{"Add Kubeconfigs…"}) {
		t.Fatalf("the welcome page's buttons %q, want Add Kubeconfigs… alone", got)
	}
	if sh.view.connectButton.Text != "Connect to Cluster…" {
		t.Fatalf("the workspace's button %q", sh.view.connectButton.Text)
	}
	h := sh.side.head
	test.Tap(sh.view.connectButton)
	q.until(t, "the section's search", func() bool { return h.searching() && sh.win.Canvas().Focused() == h.entry })
}

// Over the real engine: connected to the demo cluster with the navigator hidden (⌘B), ⌘P shows the
// navigator at the section with its search focused, and Escape on the empty search goes back to the
// demo's tree, still connected.
func TestConnectToClusterOverTheEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	empty := filepath.Join(t.TempDir(), "config") // a kubeconfig without contexts: never the developer's
	if err := os.WriteFile(empty, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &engine.ClusterConnector{IncludeDemo: true, KubeconfigPaths: []string{empty}}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 3
	s := newTestEngine(engine.Options{Connector: conn})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newEngineShell(t, s, Options{})
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: scope %q, %s %q", what, sh.ui.NavigatorScope, sh.ui.Connection.State, sh.ui.Context)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	n := sh.side.sec
	until("the section", func() bool { return n.Visible() && n.rows[engine.DemoContext] != nil })
	test.Tap(n.rows[engine.DemoContext])
	until("the demo's tree", func() bool { return sh.ui.Connection.State == engine.Connected && sh.side.tree.Visible() })
	mustDispatch(t, s, "ui.toggleNavigator", map[string]any{"open": false})
	until("the navigator hidden", func() bool { return sh.ui.NavigatorHidden })

	sh.connectMenuItem().Action()
	h := sh.side.head
	until("the section's search", func() bool {
		return !sh.ui.NavigatorHidden && n.Visible() && h.searching() && w.Canvas().Focused() == h.entry
	})
	if u := s.UIState(); u.NavigatorScope != engine.NavScopeLocal || u.NavigatorHidden || u.Connection.State != engine.Connected {
		t.Errorf("the engine: scope %q, hidden %v, %s", u.NavigatorScope, u.NavigatorHidden, u.Connection.State)
	}
	escape(sh)
	until("the demo's tree again", func() bool { return sh.side.tree.Visible() && !h.searching() })
	if u := s.UIState(); u.NavigatorScope != engine.NavScopeCluster(engine.DemoContext) || u.Connection.State != engine.Connected {
		t.Errorf("back: scope %q, %s", u.NavigatorScope, u.Connection.State)
	}
}
