package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// userCtx is a source context named as kubeadm names its admin's: "<user>@<cluster>".
const userCtx = "kubernetes-admin@prod-eu [dev.yaml]"

var userCtxInfo = engine.ContextInfo{Name: userCtx, Cluster: "prod-eu", Server: "https://api.prod-eu.example.com:6443",
	Source: "dev.yaml", KubeContext: "kubernetes-admin@prod-eu", File: "/Users/dev/dev.yaml"}

// confirmedUser is the API server's answer of the scenes.
var confirmedUser = &engine.ClusterUser{Context: userCtx, Username: "alice@example.com",
	Groups: []string{"team-a", "system:authenticated"}, Source: engine.UserFromAPIServer}

// userFake is the user's context in a state, the given user in the UI state, its tab and its tile.
func userFake(t testing.TB, u *engine.ClusterUser, state engine.ConnectionState) *fakeEngine {
	f := crdFake(t)
	f.ui.Context = userCtx
	f.ui.Connection = engine.Connection{State: state}
	f.ui.User = u
	f.contexts = append(f.contexts, userCtxInfo)
	f.ui.Tabs = []engine.ViewTab{{ID: "tab-1", Context: userCtx, View: kindPods, Title: "Pods - prod-eu", Preview: true, Sort: engine.DefaultSort}}
	f.ui.ActiveTab = "tab-1"
	return f
}

func userScene(t *testing.T, f *fakeEngine, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the root", func() bool {
		return sh.side.name.text == "prod-eu" && (len(sh.kinds) > 0 || !f.ui.Connection.State.Live())
	})
	settle(w)
	return sh, w, q
}

// Connected: the root names the cluster by its short name and the user the API server confirmed,
// a box beside the username lists the groups in its tooltip (only the groups), right after the name;
// the rest of the row keeps the row's tooltip, the context's full id.
func TestRootRowShowsTheConfirmedUser(t *testing.T) {
	sh, w, q := userScene(t, userFake(t, confirmedUser, engine.Connected), ThemeDark)
	s := sh.side
	if s.subtitle.text != "alice@example.com" || s.subtitle.color != colorMuted || !s.groups.Visible() ||
		s.groups.tip() != "team-a\nsystem:authenticated" || s.userTip != "" {
		t.Fatalf("subtitle %q (%s), groups %v %q, tip %q", s.subtitle.text, s.subtitle.color, s.groups.Visible(), s.groups.tip(), s.userTip)
	}
	if got := s.rootRow.tipText(); got != userCtx+" - Connected" {
		t.Errorf("the row's tooltip %q", got)
	}
	if got := s.rootRow.tipAt(middleOf(s.groups)); got != "team-a\nsystem:authenticated" {
		t.Errorf("the box's tooltip %q", got)
	}
	if got := s.rootRow.tipAt(middleOf(s.subtitle)); got != s.rootRow.tipText() {
		t.Errorf("a confirmed user's tooltip %q, want the row's", got)
	}
	gap := s.groups.Position().X - s.subtitle.Size().Width
	natural := fyne.MeasureText(s.subtitle.text, s.subtitle.Theme().Size(s.subtitle.size), s.subtitle.style).Width
	if gap <= 0 || gap > 8 || s.subtitle.Size().Width > natural+0.5 {
		t.Errorf("the box at %v after a text %v wide (natural %v)", s.groups.Position().X, s.subtitle.Size().Width, natural)
	}
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(s.groups))
	q.until(t, "the groups' tooltip", func() bool { return sh.tip.visibleText() == "team-a\nsystem:authenticated" })
	if !s.rootRow.hovered {
		t.Error("the row lost its hover over the box")
	}

	// No groups: no box.
	f := userFake(t, &engine.ClusterUser{Context: userCtx, Username: "system:serviceaccount:ci:deployer", Source: engine.UserFromAPIServer}, engine.Connected)
	sh, _, _ = userScene(t, f, ThemeDark)
	if sh.side.subtitle.text != "system:serviceaccount:ci:deployer" || sh.side.groups.Visible() {
		t.Errorf("without groups: %q, box %v", sh.side.subtitle.text, sh.side.groups.Visible())
	}
}

// The API server did not confirm the user: the kubeconfig's user entry's name, marked, its tooltip
// saying so; no groups.
func TestRootRowMarksTheKubeconfigFallback(t *testing.T) {
	u := &engine.ClusterUser{Context: userCtx, Username: "admin-entry", Source: engine.UserFromKubeconfig,
		Reason: "the API server refused the SelfSubjectReview (403 Forbidden)"}
	sh, _, _ := userScene(t, userFake(t, u, engine.Connected), ThemeDark)
	s := sh.side
	if s.subtitle.text != "admin-entry (kubeconfig)" || s.groups.Visible() || s.subtitle.color != colorMuted {
		t.Errorf("subtitle %q, groups %v", s.subtitle.text, s.groups.Visible())
	}
	if got := s.rootRow.tipAt(middleOf(s.subtitle)); got != notConfirmedTip || !strings.HasPrefix(got, "Not confirmed by the API server") {
		t.Errorf("the fallback's tooltip %q", got)
	}
}

// Not connected: the last known user, dimmed, its tooltip saying when it was last seen in the
// user's format; a context never confirmed shows its kubeconfig file, as before.
func TestRootRowShowsTheLastKnownUser(t *testing.T) {
	seen := time.Date(2026, 10, 16, 9, 12, 30, 0, time.UTC)
	u := &engine.ClusterUser{Context: userCtx, Username: "alice@example.com", Source: engine.UserLastSeen, LastSeen: seen}
	for _, state := range []engine.ConnectionState{engine.Disconnected, engine.Connecting, engine.ConnectError} {
		sh, _, _ := userScene(t, userFake(t, u, state), ThemeDark)
		s := sh.side
		tf := sh.timeFormat()
		want := "Last seen " + tf.FormatDate(seen) + " " + tf.FormatClock(seen)
		if s.subtitle.text != "alice@example.com" || s.subtitle.color != theme.ColorNameDisabled || s.groups.Visible() {
			t.Errorf("%s: subtitle %q (%s), groups %v", state, s.subtitle.text, s.subtitle.color, s.groups.Visible())
		}
		if got := s.rootRow.tipAt(middleOf(s.subtitle)); !strings.HasPrefix(got, want+"\n") {
			t.Errorf("%s: the tooltip %q, want it to start with %q", state, got, want)
		}
	}

	sh, _, _ := userScene(t, userFake(t, nil, engine.Disconnected), ThemeDark)
	if s := sh.side; s.subtitle.text != "dev.yaml" || s.subtitle.color != colorMuted || s.userTip != "" || s.groups.Visible() {
		t.Errorf("never confirmed: %q (%s), tip %q", s.subtitle.text, s.subtitle.color, s.userTip)
	}
	// Another context's user is never shown.
	other := *confirmedUser
	other.Context = "kind-cogpit"
	sh, _, _ = userScene(t, userFake(t, &other, engine.Connected), ThemeDark)
	if sh.side.subtitle.text != "dev.yaml" {
		t.Errorf("another context's user: %q", sh.side.subtitle.text)
	}
}

// The short name everywhere the window names the cluster, the full name in each tooltip: the top
// bar, the status bar, the hotbar's tile, the tab, the navigator's section row and the welcome
// page's row; the navigator's search still finds the context by its full name.
func TestShortClusterNameEverywhere(t *testing.T) {
	f := userFake(t, confirmedUser, engine.Connected)
	withHotbar(f, userCtx)
	sh, _, q := userScene(t, f, ThemeDark)

	if sh.top.context.text != "prod-eu" || sh.top.context.tip != "kubernetes-admin@prod-eu" || sh.top.file.text != "dev.yaml" {
		t.Errorf("top bar %q (tip %q) / %q", sh.top.context.text, sh.top.context.tip, sh.top.file.text)
	}
	if sh.status.context.text != "prod-eu" || sh.status.context.tip != userCtx {
		t.Errorf("status bar %q (tip %q)", sh.status.context.text, sh.status.context.tip)
	}
	q.until(t, "the tile", func() bool { return len(tilesOf(sh.hot)) == 1 })
	tile := tilesOf(sh.hot)[0]
	if r := test.WidgetRenderer(tile).(*hotbarTileRenderer); r.id.name != "prod-eu" || r.id.initials != "PE" {
		t.Errorf("tile %q %q", r.id.name, r.id.initials)
	}
	if !strings.HasPrefix(tile.tipText(), userCtx+"\n") {
		t.Errorf("tile tooltip %q", tile.tipText())
	}
	q.until(t, "the tab", func() bool { return len(sh.tabs.shown) == 1 })
	if got := sh.tabs.shown[0].tipText(); got != "Pods - prod-eu\n"+userCtx {
		t.Errorf("tab tooltip %q", got)
	}

	n := sh.side.sec
	n.load()
	q.until(t, "the section's rows", func() bool { return n.loaded && n.rows[userCtx] != nil })
	row := n.rows[userCtx]
	if row.title.text != "prod-eu" || !strings.HasPrefix(row.tipText(), "kubernetes-admin@prod-eu - dev.yaml") {
		t.Errorf("section row %q, tooltip %q", row.title.text, row.tipText())
	}
	for _, typed := range []string{"kubernetes-admin@prod", "KUBERNETES-ADMIN", "prod-eu", "dev.yaml"} {
		if !n.matches(userCtxInfo, typed) {
			t.Errorf("the search %q does not find the context", typed)
		}
	}
	if n.matches(userCtxInfo, "staging") {
		t.Error("the search finds what the context does not hold")
	}

	r := sh.tabs.welcome.row(userCtxInfo)
	if r.title.text != "prod-eu" || !strings.HasPrefix(r.sub.text, "context kubernetes-admin@prod-eu · ") {
		t.Errorf("welcome row %q / %q", r.title.text, r.sub.text)
	}
	// A display name the user set still wins, the full name in the second line.
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{userCtx: {Name: "Production"}}
	})
	q.until(t, "the display name", func() bool { return sh.side.name.text == "Production" })
	if sh.top.context.text != "Production" || sh.top.context.tip != "kubernetes-admin@prod-eu" {
		t.Errorf("top bar with a display name %q (tip %q)", sh.top.context.text, sh.top.context.tip)
	}
	if r := sh.tabs.welcome.row(userCtxInfo); r.title.text != "Production" || !strings.HasPrefix(r.sub.text, "context kubernetes-admin@prod-eu · ") {
		t.Errorf("welcome row with a display name %q / %q", r.title.text, r.sub.text)
	}
}

func TestRenderNavigatorUserDark(t *testing.T) { renderNavigatorUser(t, ThemeDark, "dark") }

func TestRenderNavigatorUserLight(t *testing.T) { renderNavigatorUser(t, ThemeLight, "light") }

// renderNavigatorUser: the root row in its three states, each with its tooltip shown: connected (the
// pointer on the groups' box), the kubeconfig fallback and not connected (the pointer on the name).
func renderNavigatorUser(t *testing.T, mode ThemeMode, suffix string) {
	seen := time.Date(2026, 10, 16, 9, 12, 0, 0, time.UTC)
	for _, sc := range []struct {
		name  string
		user  *engine.ClusterUser
		state engine.ConnectionState
		on    func(s *sidebar) fyne.CanvasObject
	}{
		{"navigator_user", confirmedUser, engine.Connected, func(s *sidebar) fyne.CanvasObject { return s.groups }},
		{"navigator_user_kubeconfig", &engine.ClusterUser{Context: userCtx, Username: "admin-entry", Source: engine.UserFromKubeconfig,
			Reason: "the API server refused the SelfSubjectReview (403 Forbidden)"}, engine.Connected,
			func(s *sidebar) fyne.CanvasObject { return s.subtitle }},
		{"navigator_user_lastseen", &engine.ClusterUser{Context: userCtx, Username: "alice@example.com", Source: engine.UserLastSeen,
			LastSeen: seen}, engine.Connecting, func(s *sidebar) fyne.CanvasObject { return s.subtitle }},
	} {
		f := userFake(t, sc.user, engine.Disconnected)
		withSettingsCommand(t, f) // the gear before the dot, as every window has it (D-92)
		sh, w, q := userScene(t, f, mode)
		clk := fakeBlinkClock(sh) // a connecting cluster's dot blinks: drawn in its full phase
		f.update(func(u *engine.UIState) {
			u.Connection, u.NavigatorScope = engine.Connection{State: sc.state}, engine.NavScopeCluster(userCtx)
			if sc.state.Live() {
				u.ClusterVersion = clusterVersionOf("v1.37.1") // its version, supported (clusterversion.go, D-91)
			}
		})
		q.until(t, "the cluster's tree", func() bool { return sh.side.tree.Visible() && sh.ui.Connection.State == sc.state })
		clk.after(0)
		q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
		settle(w)
		sh.tip.delay = time.Millisecond
		test.MoveMouse(w.Canvas(), middleOf(sc.on(sh.side)))
		q.until(t, sc.name+"'s tooltip", func() bool { return sh.tip.visibleText() != "" })
		savePNG(t, w, sc.name+"_"+suffix+".png")
	}
}
