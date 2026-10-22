package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// insecureFake lists the fixture's contexts with dev-hfi's cluster skipping the certificate check.
func insecureFake(t *testing.T) *fakeEngine {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	for i := range f.contexts {
		if f.contexts[i].Name == "dev-hfi" {
			f.contexts[i].Insecure = true
		}
	}
	return f
}

// welcomeRowOf returns the welcome page's row of the context whose title is title.
func welcomeRowOf(t *testing.T, sh *shell, title string) *clusterRow {
	t.Helper()
	for _, r := range sh.tabs.welcome.rows {
		if r.title.text == title {
			return r
		}
	}
	t.Fatalf("no welcome row %q", title)
	return nil
}

// hasPill reports whether the row's badges hold a pill with this text.
func hasPill(r *clusterRow, text string) bool {
	for _, o := range r.badges.Objects {
		if p, ok := o.(*pill); ok && p.text == text {
			return true
		}
	}
	return false
}

// welcomeOnFake shows the welcome page over a fake engine and waits for every context's row.
func welcomeOnFake(t *testing.T, f *fakeEngine, mode ThemeMode) (*shell, *mainQueue) {
	t.Helper()
	f.ui.Welcome = true // the engine's state without a cluster (engine/welcome.go)
	sh, _, q := newTestShell(t, f, Options{Theme: mode})
	w := welcomeShown(t, sh, q)
	q.until(t, "every row", func() bool { return len(w.rows) == len(f.contexts) })
	return sh, q
}

// A context whose kubeconfig skips the certificate check carries a warning pill and a warning line
// that names the flag and the server its credential goes to on the welcome page, and the line in
// its row's tooltip in the navigator's section (the context picker's rows had the pill and the
// line, D-57); the other rows have neither.
func TestClusterRowsMarkAnInsecureContext(t *testing.T) {
	f := insecureFake(t)
	sh, q := welcomeOnFake(t, f, ThemeDark)

	row := welcomeRowOf(t, sh, "dev-hfi")
	if !hasPill(row, "certificate not checked") {
		t.Error("the insecure context has no warning pill")
	}
	note := "insecure-skip-tls-verify: the credential goes to whoever answers at https://192.168.49.2:8443"
	if row.note == nil || row.note.color != colorStatusWarning || row.note.text != note {
		t.Errorf("the warning line: %+v", row.note)
	}
	if other := welcomeRowOf(t, sh, "kind-cogpit"); hasPill(other, "certificate not checked") || other.note != nil {
		t.Error("a checked context is marked")
	}
	n := sh.side.sec
	q.until(t, "the section's rows", func() bool { return n.loaded && n.rows["dev-hfi"] != nil })
	if tip := n.rows["dev-hfi"].tipText(); !strings.HasSuffix(tip, "\n"+note) {
		t.Errorf("the section's tooltip %q", tip)
	}
	if tip := n.rows["kind-cogpit"].tipText(); strings.Contains(tip, "insecure") {
		t.Errorf("a checked context's tooltip %q", tip)
	}
	if got := insecureNote(engine.ContextInfo{Insecure: true}); !strings.HasSuffix(got, "its server") {
		t.Errorf("without a server: %q", got)
	}
}

// The welcome page's rows of such contexts, light and dark: the "certificate not checked" pill and
// line, beside a checked one, and the pill saying an unchecked connection waits for the user.
func TestRenderClusterRowsInsecure(t *testing.T) {
	for _, tc := range []struct {
		mode ThemeMode
		name string
	}{{ThemeDark, "welcome_insecure_dark.png"}, {ThemeLight, "welcome_insecure_light.png"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := insecureFake(t)
			f.contexts = append([]engine.ContextInfo{labContext}, f.contexts...)
			sh, q := welcomeOnFake(t, f, tc.mode)
			if !hasPill(welcomeRowOf(t, sh, "lab"), "unchecked connection not allowed yet") {
				t.Error("the row does not say the unchecked connection waits")
			}
			q.drain()
			savePNG(t, sh.win, tc.name)
		})
	}
}

// The connect log shows the engine's WARN line in the warning colour.
func TestRenderConnectionUpdatesInsecure(t *testing.T) {
	f := insecureFake(t)
	at := time.Date(2026, 10, 13, 2, 3, 9, 0, time.UTC)
	f.handlers["cluster.connectLog"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"context": "dev-hfi", "lines": []map[string]any{
			{"time": at, "level": engine.LogInfo, "text": "Connecting to dev-hfi"},
			{"time": at.Add(time.Second), "level": engine.LogWarn, "text": "Certificate checks are off for this context " +
				"(insecure-skip-tls-verify in its kubeconfig): Cogpit cannot tell the cluster at https://192.168.49.2:8443 " +
				"from anyone else who answers there, and sends the context's credential to whoever does"},
			{"time": at.Add(time.Second), "level": engine.LogInfo, "text": "Connected to dev-hfi"},
		}}, nil
	}
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the section's rows", func() bool { return sh.side.sec.loaded && len(sh.side.sec.rows) == len(f.contexts) })
	sh.kc.showConnectLog("dev-hfi")
	q.until(t, "the log dialog", func() bool { return sh.kc.connectLog != nil })
	if n := len(sh.kc.connectLog.view.Lines); n != 3 {
		t.Errorf("%d lines", n)
	}
	savePNG(t, w, "connection_updates_insecure_dark.png")
}

// A cluster row on the welcome page names its kubeconfig file only when another row shows the same
// title (the owner, 2026-10-21): two "staging" contexts from two files are told apart by their
// files, a cluster whose name is unique shows none.
func TestWelcomeNamesTheFileOnlyForSharedNames(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = []engine.ContextInfo{
		{Name: "staging [a.yaml]", KubeContext: "staging", Source: "a.yaml", Cluster: "staging-a", Server: "https://a.example:6443"},
		{Name: "staging [b.yaml]", KubeContext: "staging", Source: "b.yaml", Cluster: "staging-b", Server: "https://b.example:6443"},
		{Name: "prod [c.yaml]", KubeContext: "prod", Source: "c.yaml", Cluster: "prod", Server: "https://c.example:6443"},
	}
	sh, _ := welcomeOnFake(t, f, ThemeDark)
	files := map[string]bool{}
	for _, r := range sh.tabs.welcome.rows {
		for _, file := range []string{"a.yaml", "b.yaml", "c.yaml"} {
			if hasPill(r, file) {
				files[file] = true
			}
		}
	}
	if !files["a.yaml"] || !files["b.yaml"] {
		t.Errorf("the two staging rows name their files: %v", files)
	}
	if files["c.yaml"] {
		t.Error("the prod row, unique on the page, names its file")
	}
}

// TestWelcomeRowConnectsOnce (the lead's follow-up to AW12): a click on a welcome page's cluster row
// connects at its release, as Fyne's driver delivers a tap to an object that is no
// fyne.DoubleTappable (driverTap); the row was one, for a double click that connected as one click
// does, so the driver held every click for the double-click interval. A second click (a double
// click's, or an impatient one) while that cluster's connect is under way connects no second time:
// before the engine says so (the page's own pending connect), on the row rebuilt once the window
// shows the cluster connecting, on a cluster the window shows connecting from elsewhere (the
// navigator, an agent), and through the login program's review; once the connect returned, a click
// connects again. Each "no second time" is checked after a later step the test waits for,
// so that a second dispatch, which runs on a goroutine of its own, has been seen.
//
// The fake engine derives the welcome page as the engine does (fake_test.go, D-114): it hides while
// the shown cluster connects, so a click on a row of that cluster can only come from an event the
// page's hiding overtook. keepPage puts the page back so that such a click is checked all the same;
// the test's last step checks that the page hides. Every cluster is on the hotbar, so that each keeps
// its row whichever one is recent: the page has no "Other clusters" (AW13).
func TestWelcomeRowConnectsOnce(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	names := make([]string, 0, len(f.contexts))
	for i := range f.contexts {
		if f.contexts[i].Name == "dev-hfi" {
			f.contexts[i].Exec = engine.ExecRequired // its login program not allowed yet: reviewed first
		}
		names = append(names, f.contexts[i].Name)
	}
	withHotbar(f, names...)
	shown, done := make(chan struct{}), make(chan struct{})
	f.handlers["cluster.connect"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
		var p contextParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		for _, step := range []chan struct{}{shown, done} {
			select {
			case <-step:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if step == shown { // the engine shows the cluster connecting; the page kept up (keepPage)
				f.update(func(u *engine.UIState) {
					u.Context, u.Connection = p.Context, engine.Connection{State: engine.Connecting}
				})
				f.update(func(u *engine.UIState) { u.Welcome = true })
			}
		}
		return nil, nil
	}
	review := make(chan struct{})
	f.handlers["kubeconfig.pendingApprovals"] = func(ctx context.Context, _ engine.Invocation) (any, error) {
		select {
		case <-review:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return map[string]any{"groups": []any{}}, nil // allowed meanwhile: the connect follows
	}
	sh, q := welcomeOnFake(t, f, ThemeDark)
	connects := func() int { return len(f.callsOf("cluster.connect")) }
	connectsOf := func(name string) int {
		n := 0
		for _, inv := range f.callsOf("cluster.connect") {
			if decodeParams[contextParams](t, inv).Context == name {
				n++
			}
		}
		return n
	}
	settle(sh.win) // the page laid out: a click finds what it shows

	row := welcomeRowOf(t, sh, "kind-cogpit")
	if got := driverTap(t, sh.win, centre(row)); got != row {
		t.Fatalf("a click on kind-cogpit's row reached %T, not the row", got)
	}
	q.until(t, "the connect", func() bool { return connects() == 1 })
	driverTap(t, sh.win, centre(row)) // the double click's second click: the engine has not answered
	close(shown)
	q.until(t, "kind-cogpit connecting, its row rebuilt", func() bool {
		return sh.ui.Context == "kind-cogpit" && sh.ui.Connection.State == engine.Connecting && welcomeRowOf(t, sh, "kind-cogpit") != row
	})
	if n := connects(); n != 1 {
		t.Fatalf("a second click before the engine answered connected %d times", n)
	}
	settle(sh.win)
	driverTap(t, sh.win, centre(welcomeRowOf(t, sh, "kind-cogpit"))) // an impatient click on the new row
	close(done)
	q.until(t, "the connect returned", func() bool { return sh.tabs.welcome.pending == "" })
	if n := connects(); n != 1 {
		t.Fatalf("a click while connecting connected %d times", n)
	}

	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{} })
	q.until(t, "no cluster shown", func() bool { return sh.ui.Context == "" })
	settle(sh.win)
	driverTap(t, sh.win, centre(welcomeRowOf(t, sh, "kind-cogpit")))
	q.until(t, "a connect again", func() bool { return connects() == 2 })
	q.until(t, "that connect returned", func() bool { return sh.tabs.welcome.pending == "" })

	// A connect the window shows, begun elsewhere: a click on its row connects nothing; one on
	// another cluster's row does.
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = "prod-eu-central", engine.Connection{State: engine.Connecting}
	})
	f.update(func(u *engine.UIState) { u.Welcome = true }) // keepPage
	q.until(t, "prod-eu-central connecting", func() bool { return sh.ui.Context == "prod-eu-central" && sh.tabs.welcome.shown })
	settle(sh.win)
	driverTap(t, sh.win, centre(welcomeRowOf(t, sh, "prod-eu-central")))
	driverTap(t, sh.win, centre(welcomeRowOf(t, sh, "staging-us-east")))
	q.until(t, "staging-us-east's connect", func() bool { return connectsOf("staging-us-east") == 1 })
	q.until(t, "its connect returned", func() bool { return sh.tabs.welcome.pending == "" })
	if n := connectsOf("prod-eu-central"); n != 0 {
		t.Fatalf("a click on a cluster the window shows connecting connected it %d times", n)
	}

	// The login program's review: one for two clicks, then one connect.
	dev := welcomeRowOf(t, sh, "dev-hfi")
	driverTap(t, sh.win, centre(dev))
	q.until(t, "the review", func() bool { return len(f.callsOf("kubeconfig.pendingApprovals")) == 1 })
	driverTap(t, sh.win, centre(dev))
	close(review)
	q.until(t, "dev-hfi's connect", func() bool { return connectsOf("dev-hfi") == 1 })
	q.until(t, "dev-hfi's connect returned", func() bool { return sh.tabs.welcome.pending == "" })
	if n, m := len(f.callsOf("kubeconfig.pendingApprovals")), connectsOf("dev-hfi"); n != 1 || m != 1 {
		t.Fatalf("two clicks on a row whose login program is reviewed: %d reviews, %d connects", n, m)
	}

	// Without the page kept up, it hides while the shown cluster connects, as the engine has it.
	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{} })
	q.until(t, "the page", func() bool { return sh.tabs.welcome.shown })
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = "kind-cogpit", engine.Connection{State: engine.Connecting}
	})
	q.until(t, "the page hidden while kind-cogpit connects", func() bool { return !sh.tabs.welcome.shown })
}
