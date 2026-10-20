package fyneui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › Agent (preferences_agent.go) on the real engine, its chat a scripted agent.

func agentPrefsShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeChat) {
	t.Helper()
	s, _ := settingsEngine(t)
	ch := newFakeChat()
	ch.notify = s.NotifyChat
	ch.conns = []engine.ChatConnection{lmStudio(), litellm()}
	s.SetChatAgent(ch)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": engine.SectionAgent})
	q.until(t, "Preferences › Agent", func() bool { return sh.settings.agent != nil && sh.settings.agent.loaded })
	return sh, w, q, ch
}

// TestAgentPreferencesListsConnections: the section lists each connection with its kind, host,
// model, the default and the private mark, and is the Preferences page's last section.
func TestAgentPreferencesListsConnections(t *testing.T) {
	sh, _, _, _ := agentPrefsShell(t, ThemeDark)
	if b := sh.settings.navItems[engine.SectionAgent]; b == nil || b.Text != "Agent" {
		t.Fatalf("no Agent in the section list: %v", b)
	}
	var texts []string
	for _, l := range findAll[*textLine](sh.settings.agent.list) {
		texts = append(texts, l.text)
	}
	all := strings.Join(texts, "|")
	for _, want := range []string{"LM Studio", "LM Studio · localhost:1234 · qwen3.6-27b · no API key", "Team LiteLLM",
		"LiteLLM · litellm.example.com · gpt-oss-120b"} {
		if !strings.Contains(all, want) {
			t.Errorf("the list lacks %q: %s", want, all)
		}
	}
	var pills []string
	for _, p := range findAll[*pill](sh.settings.agent.list) {
		pills = append(pills, p.text)
	}
	if strings.Join(pills, ",") != "default,private" {
		t.Errorf("marks %v, want LM Studio's default and private", pills)
	}
}

// TestAgentPreferencesKeySemantics: saving with the key field empty keeps the stored key (nil), a
// typed key replaces it, Remove Key removes it (a pointer to ""); a new connection without a key
// sends none. The kind prefills LM Studio's base URL.
func TestAgentPreferencesKeySemantics(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	save := func(n int) fakeSave {
		t.Helper()
		test.Tap(a.form.saveB)
		q.until(t, "the save", func() bool { return len(ch.savesOf()) == n && a.form == nil })
		return ch.savesOf()[n-1]
	}

	a.edit(litellm())
	if !a.form.removeKey.Visible() || !strings.Contains(a.form.keyHelp.text, "Empty keeps it") {
		t.Fatalf("a connection with a key: remove %v, help %q", a.form.removeKey.Visible(), a.form.keyHelp.text)
	}
	if got := save(1); got.key != nil || got.conn.ID != "c2" {
		t.Errorf("empty key field: key %v, want nil (keep)", got.key)
	}

	a.edit(litellm())
	test.Type(a.form.key, "sk-new")
	if got := save(2); got.key == nil || *got.key != "sk-new" {
		t.Errorf("typed key: %v, want sk-new", got.key)
	}

	a.edit(litellm())
	test.Tap(a.form.removeKey)
	if a.form.removeKey.Visible() || !strings.Contains(a.form.keyHelp.text, "removes the stored key") {
		t.Errorf("after Remove Key: %q", a.form.keyHelp.text)
	}
	if got := save(3); got.key == nil || *got.key != "" {
		t.Errorf("removed key: %v, want a pointer to \"\"", got.key)
	}

	test.Tap(a.addB)
	f := a.form
	if f.baseURL.Text != "http://localhost:1234/v1" || f.kind.Selected != "LM Studio" {
		t.Fatalf("a new connection: %q, %q", f.kind.Selected, f.baseURL.Text)
	}
	f.kind.SetSelected("Open WebUI")
	if f.baseURL.Text != "" || !strings.Contains(f.urlHint.text, "/api") {
		t.Errorf("Open WebUI: the URL %q, hint %q", f.baseURL.Text, f.urlHint.text)
	}
	f.kind.SetSelected("LM Studio")
	test.Type(f.name, "Mac Studio")
	f.private.SetChecked(true)
	test.Type(f.limits[1], "40")
	test.Type(f.limits[2], "120,000")
	got := save(4)
	if got.key != nil || got.conn.ID != "" || got.conn.Kind != engine.ChatLMStudio || got.conn.BaseURL != "http://localhost:1234/v1" ||
		!got.conn.Private || got.conn.Limits.MaxToolCalls != 40 || got.conn.Limits.MaxTokens != 120_000 || got.conn.Limits.RepeatStop != 0 {
		t.Errorf("the new connection was saved as %+v with key %v", got.conn, got.key)
	}
}

// TestAgentPreferencesKeyHint: the API key's hints say where a key is kept, Cogpit's settings
// folder readable only by the user, never the keychain (the owner's decision of 2026-10-15), each
// whole at the form's width; a saved connection without a key saves "" for an empty field, as its
// hint says ("Empty: no key"), which gives up a key kept in the keychain before.
func TestAgentPreferencesKeyHint(t *testing.T) {
	sh, w, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	whole := func(l *textLine, want string) {
		t.Helper()
		settle(w)
		if l.text != want || strings.Contains(strings.ToLower(l.text), "keychain") {
			t.Errorf("hint %q, want %q", l.text, want)
		}
		if shown := test.WidgetRenderer(l).Objects()[0].(*canvas.Text).Text; shown != want {
			t.Errorf("the hint is shortened to %q", shown)
		}
	}
	var help *paragraph
	for _, l := range findAll[*paragraph](w.Content()) { // a setting's help is wrapped, never cut (preferences_page.go)
		if strings.HasPrefix(l.text, "Each conversation uses one") {
			help = l
		}
	}
	if help == nil {
		t.Fatal("no help under LLM connections")
	}
	settle(w)
	const helpText = "Each conversation uses one; a new conversation the default. API keys stay in Cogpit's settings folder."
	if help.text != helpText || paragraphShown(help) != helpText {
		t.Errorf("help %q, drawn %q, want %q whole", help.text, paragraphShown(help), helpText)
	}

	a.edit(litellm())
	whole(a.form.keyHelp, "The key is kept in Cogpit's settings folder, readable only by you. Empty keeps it.")
	test.Type(a.form.key, "sk-FAKE")
	whole(a.form.keyHelp, "Saving keeps this key in Cogpit's settings folder, readable only by you.")
	a.closeForm()

	a.edit(lmStudio())
	whole(a.form.keyHelp, "Kept in Cogpit's settings folder, readable only by you. Empty: no key (LM Studio needs none).")
	test.Tap(a.form.saveB)
	q.until(t, "the save", func() bool { return len(ch.savesOf()) == 1 && a.form == nil })
	if got := ch.savesOf()[0]; got.key == nil || *got.key != "" || got.conn.ID != "c1" {
		t.Errorf("a saved connection without a key, the field empty: key %v, want a pointer to \"\"", got.key)
	}
}

// TestAgentPreferencesPrivateHint (H-96): under the private connection's mark the form says that
// conversations over a private connection are not saved: a hint in the settings' style (a muted
// caption line under the setting), whole at the form's width, between the mark and the limits.
func TestAgentPreferencesPrivateHint(t *testing.T) {
	sh, w, _, _ := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	a.edit(litellm())
	settle(w)
	var hint *paragraph
	for _, l := range findAll[*paragraph](a.form.root) {
		if l.text == privateNotKept {
			hint = l
		}
	}
	if hint == nil || !hint.Visible() {
		t.Fatalf("the form has no visible hint %q", privateNotKept)
	}
	if hint.color != colorMuted || hint.size != theme.SizeNameCaptionText {
		t.Errorf("the hint is drawn in %s at %s, want the settings' hint style", hint.color, hint.size)
	}
	if shown := paragraphShown(hint); shown != privateNotKept {
		t.Errorf("the hint is shortened to %q", shown)
	}
	d := fyne.CurrentApp().Driver()
	y := d.AbsolutePositionForObject(hint).Y
	if y <= d.AbsolutePositionForObject(a.form.private).Y || y >= d.AbsolutePositionForObject(a.form.limits[0]).Y {
		t.Errorf("the hint at y %v is not between the private mark and the limits", y)
	}
}

// TestAgentPreferencesAllTools: the form's "send all tools" (the owner's decision of 2026-10-15) is
// off for a connection without it, says what it costs, is saved with the connection, and the list
// names a connection that sends all tools.
func TestAgentPreferencesAllTools(t *testing.T) {
	sh, w, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	a.edit(litellm())
	settle(w)
	if a.form.allTools.Checked || a.form.allTools.Text != allToolsText {
		t.Fatalf("all tools: %v %q", a.form.allTools.Checked, a.form.allTools.Text)
	}
	var help bool
	for _, p := range findAll[*paragraph](a.form.root) {
		help = help || (p.text == allToolsHelp && p.color == colorMuted)
	}
	if !help {
		t.Error("the setting does not say what it costs")
	}
	a.form.allTools.SetChecked(true)
	test.Tap(a.form.saveB)
	q.until(t, "the save", func() bool { return len(ch.savesOf()) == 1 && a.form == nil })
	if got := ch.savesOf()[0]; !got.conn.AllTools || got.conn.ID != "c2" {
		t.Errorf("saved %+v, want all tools", got.conn)
	}
	q.until(t, "the list", func() bool {
		for _, l := range findAll[*textLine](a.list) {
			if strings.HasSuffix(l.text, "gpt-oss-120b · all tools") {
				return true
			}
		}
		return false
	})
}

// lanStudio is an LM Studio on another machine of the local network, over plain http (the owner's).
func lanStudio() engine.ChatConnection {
	return engine.ChatConnection{ID: "c3", Name: "LM Studio on the LAN", Kind: engine.ChatLMStudio, BaseURL: "http://192.168.1.20:1234/v1",
		Model: "qwen/qwen3.8-27b"}
}

// TestAgentPreferencesPlainHTTPWarning (the owner's decision of 2026-10-15, H-95): while the base
// URL is plain http to another machine the form says, in the warning colour and whole, that the key,
// the conversation and every secret the agent reads travel unencrypted; localhost, 127.0.0.1, ::1
// and https do not warn. The list marks such a connection "plain http".
func TestAgentPreferencesPlainHTTPWarning(t *testing.T) {
	sh, w, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	ch.mu.Lock()
	ch.conns = append(ch.conns, lanStudio())
	ch.mu.Unlock()
	a.load()
	q.until(t, "the LAN connection's mark", func() bool {
		for _, p := range findAll[*pill](a.list) {
			if p.text == "plain http" && p.fg == colorStatusWarning {
				return true
			}
		}
		return false
	})
	if n := len(findAll[*pill](a.list)); n != 3 { // default, private, plain http
		t.Errorf("%d marks in the list, want only the LAN connection's plain http besides LM Studio's two", n)
	}

	a.edit(lanStudio())
	settle(w)
	warn := a.form.plainWarn
	if !warn.Visible() || warn.text != engine.ChatPlainHTTPWarning || warn.color != colorStatusWarning {
		t.Fatalf("the LAN connection: warning shown %v, %q in %s", warn.Visible(), warn.text, warn.color)
	}
	if lines := test.WidgetRenderer(warn).Objects(); !strings.HasSuffix(lines[len(lines)-1].(*canvas.Text).Text, "over the network.") {
		t.Error("the warning is cut")
	}
	for base, shown := range map[string]bool{
		"http://localhost:1234/v1": false, "http://127.0.0.1:1234/v1": false, "http://[::1]:1234/v1": false,
		"https://llm.lan:1234/v1": false, "http://litellm.lan:4000": true,
	} {
		a.form.baseURL.SetText(base)
		if warn.Visible() != shown {
			t.Errorf("%s: warning shown %v, want %v", base, warn.Visible(), shown)
		}
	}
}

// TestAgentPreferencesCAFile: the CA file is saved with an https connection by its absolute path
// ("~/" the home folder), refused with an http base URL or a relative path; the list says a
// connection has one.
func TestAgentPreferencesCAFile(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	a.edit(litellm())
	a.form.caFile.SetText("certs/ca.pem")
	test.Tap(a.form.saveB)
	if !a.form.errLine.Visible() || !strings.Contains(a.form.errLine.text, "absolute path") {
		t.Errorf("a relative CA file: %q", a.form.errLine.text)
	}
	a.form.caFile.SetText("~/certs/ca.pem")
	a.form.baseURL.SetText("http://litellm.lan:4000")
	test.Tap(a.form.saveB)
	if !strings.Contains(a.form.errLine.text, "https") {
		t.Errorf("a CA file over http: %q", a.form.errLine.text)
	}
	q.drain()
	if n := len(ch.savesOf()); n != 0 {
		t.Fatalf("%d saves of invalid forms", n)
	}
	a.form.baseURL.SetText("https://litellm.example.com")
	test.Tap(a.form.saveB)
	q.until(t, "the save", func() bool { return len(ch.savesOf()) == 1 && a.form == nil })
	home, _ := os.UserHomeDir()
	if got := ch.savesOf()[0].conn.CAFile; got != filepath.Join(home, "certs", "ca.pem") {
		t.Errorf("saved CA file %q", got)
	}
	q.until(t, "the list", func() bool {
		for _, l := range findAll[*textLine](a.list) {
			if strings.HasSuffix(l.text, "gpt-oss-120b · CA file") {
				return true
			}
		}
		return false
	})
	conns, _ := ch.Connections()
	a.edit(conns[1])
	if a.form.caFile.Text != filepath.Join(home, "certs", "ca.pem") {
		t.Errorf("the form shows the CA file %q", a.form.caFile.Text)
	}
}

// plainSteps is a connection test of the LAN connection: every layer passed, the HTTP layer with
// the plain http warning.
func plainSteps() [][]engine.ChatTestStep {
	return [][]engine.ChatTestStep{{
		{Layer: engine.ChatTestTCP, State: engine.ChatTestPass, Detail: "192.168.1.20:1234 answers", Duration: 2 * time.Millisecond},
		{Layer: engine.ChatTestHTTP, State: engine.ChatTestPass, Detail: "HTTP 200, plain http to another machine",
			Warning: engine.ChatPlainHTTPWarning, Duration: 9 * time.Millisecond},
		{Layer: engine.ChatTestAuth, State: engine.ChatTestPass, Detail: "7 models, no key needed", Duration: 11 * time.Millisecond},
		{Layer: engine.ChatTestTools, State: engine.ChatTestPass, Detail: "qwen/qwen3.8-27b called get_time", Duration: 3200 * time.Millisecond},
	}}
}

// TestAgentPreferencesTestWarns: a layer that passed with a warning shows the warning icon and
// colour, and the warning under the test's rows.
func TestAgentPreferencesTestWarns(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	ch.testSteps = plainSteps()
	a.edit(lanStudio())
	test.Tap(a.form.testB)
	q.until(t, "the test's result", func() bool {
		r := a.form.stepRows[engine.ChatTestTools]
		return r != nil && r.step.State == engine.ChatTestPass && !a.form.testB.Disabled()
	})
	if r := a.form.stepRows[engine.ChatTestHTTP]; r.detail.color != colorStatusWarning || r.detail.text != "Passed: HTTP 200, plain http to another machine" {
		t.Errorf("HTTP: %q in %s", r.detail.text, r.detail.color)
	}
	if r := a.form.stepRows[engine.ChatTestAuth]; r.detail.color != colorStatusOK {
		t.Errorf("Auth drawn in %s", r.detail.color)
	}
	if !a.form.testWarn.Visible() || a.form.testWarn.text != engine.ChatPlainHTTPWarning || a.form.testWarn.color != colorStatusWarning {
		t.Errorf("the test's warning: shown %v, %q", a.form.testWarn.Visible(), a.form.testWarn.text)
	}
	ch.testSteps = testSteps(true)
	test.Tap(a.form.testB)
	q.until(t, "the second test", func() bool {
		return a.form.stepRows[engine.ChatTestAuth].step.State == engine.ChatTestFail && !a.form.testB.Disabled()
	})
	if a.form.testWarn.Visible() {
		t.Error("a test without a warning keeps the earlier one")
	}
}

// TestAgentPreferencesValidation: a form without a name or with a base URL that is no http(s)
// address is not saved and says why.
func TestAgentPreferencesValidation(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	test.Tap(a.addB)
	test.Tap(a.form.saveB)
	if !a.form.errLine.Visible() || !strings.Contains(a.form.errLine.text, "name") {
		t.Errorf("no name: %q", a.form.errLine.text)
	}
	test.Type(a.form.name, "x")
	a.form.baseURL.SetText("localhost:1234")
	test.Tap(a.form.saveB)
	if !strings.Contains(a.form.errLine.text, "http or https") {
		t.Errorf("bad URL: %q", a.form.errLine.text)
	}
	test.Type(a.form.limits[0], "three")
	a.form.baseURL.SetText("http://localhost:1234/v1")
	test.Tap(a.form.saveB)
	if !strings.Contains(a.form.errLine.text, "whole number") {
		t.Errorf("bad limit: %q", a.form.errLine.text)
	}
	q.drain()
	if n := len(ch.savesOf()); n != 0 {
		t.Errorf("%d saves of invalid forms", n)
	}
}

// TestAgentPreferencesDefault: "use as default" saves and makes it the default; Make Default on a
// row too.
func TestAgentPreferencesDefault(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	a.edit(litellm())
	a.form.def.SetChecked(true)
	test.Tap(a.form.saveB)
	q.until(t, "the default", func() bool {
		ch.mu.Lock()
		defer ch.mu.Unlock()
		return len(ch.defaults) == 1 && ch.defaults[0] == "c2"
	})
	q.until(t, "the list again", func() bool {
		for _, p := range findAll[*pill](a.list) {
			if p.text == "default" {
				return true
			}
		}
		return false
	})
}

func testSteps(final bool) [][]engine.ChatTestStep {
	running := []engine.ChatTestStep{
		{Layer: engine.ChatTestTCP, State: engine.ChatTestPass, Detail: "litellm.example.com:443 answers", Duration: 3 * time.Millisecond},
		{Layer: engine.ChatTestHTTP, State: engine.ChatTestRunning},
		{Layer: engine.ChatTestAuth, State: engine.ChatTestPending},
		{Layer: engine.ChatTestTools, State: engine.ChatTestPending},
	}
	done := []engine.ChatTestStep{
		{Layer: engine.ChatTestTCP, State: engine.ChatTestPass, Detail: "litellm.example.com:443 answers", Duration: 3 * time.Millisecond},
		{Layer: engine.ChatTestHTTP, State: engine.ChatTestPass, Detail: "HTTP 200, TLS 1.3", Duration: 41 * time.Millisecond},
		{Layer: engine.ChatTestAuth, State: engine.ChatTestFail, Detail: "HTTP 401: the API key was refused", Duration: 12 * time.Millisecond},
		{Layer: engine.ChatTestTools, State: engine.ChatTestNotReached},
	}
	if !final {
		return [][]engine.ChatTestStep{running}
	}
	return [][]engine.ChatTestStep{running, done}
}

// TestAgentPreferencesConnectionTest: Test Connection runs the agent's test with the form's values
// (the typed key) and shows each layer: passed with its time, failed with the reason, not reached.
func TestAgentPreferencesConnectionTest(t *testing.T) {
	sh, _, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	ch.testSteps = testSteps(true)
	a.edit(litellm())
	a.form.name.SetText("Team LiteLLM (edited)")
	test.Type(a.form.key, "sk-try")
	test.Tap(a.form.testB)
	q.until(t, "the test's result", func() bool {
		r := a.form.stepRows[engine.ChatTestTools]
		return r != nil && r.step.State == engine.ChatTestNotReached && !a.form.testB.Disabled()
	})
	ch.mu.Lock()
	tried := ch.tests[0]
	ch.mu.Unlock()
	if tried.conn.Name != "Team LiteLLM (edited)" || tried.key == nil || *tried.key != "sk-try" {
		t.Errorf("tested %+v with key %v, want the form's values", tried.conn, tried.key)
	}
	rows := a.form.stepRows
	if r := rows[engine.ChatTestHTTP]; r.detail.text != "Passed: HTTP 200, TLS 1.3" || r.took.text != "41 ms" {
		t.Errorf("HTTP: %q %q", r.detail.text, r.took.text)
	}
	if r := rows[engine.ChatTestAuth]; !strings.HasPrefix(r.detail.text, "Failed: HTTP 401") || r.detail.color != colorStatusError {
		t.Errorf("Auth: %q", r.detail.text)
	}
	if r := rows[engine.ChatTestTools]; !strings.HasPrefix(r.detail.text, "Not reached") {
		t.Errorf("Tools: %q", r.detail.text)
	}
	if len(ch.savesOf()) != 0 {
		t.Error("the test saved the connection")
	}
}

// TestAgentPreferencesNotACommand: nothing on the page is a registry command: an agent cannot set
// a connection, its key or its private mark.
func TestAgentPreferencesNotACommand(t *testing.T) {
	sh, _, _, _ := agentPrefsShell(t, ThemeDark)
	for _, c := range sh.eng.Registry().Commands() {
		id := strings.ToLower(c.ID)
		if strings.Contains(id, "connection") || strings.Contains(id, "llm") || (strings.HasPrefix(id, "chat.") && id != "chat.toggle") {
			t.Errorf("command %s reaches the chat's connections", c.ID)
		}
	}
}

func renderAgentPreferences(t *testing.T, mode ThemeMode, name string, conn engine.ChatConnection, steps [][]engine.ChatTestStep) {
	sh, w, q, ch := agentPrefsShell(t, mode)
	ch.mu.Lock()
	ch.conns = append(ch.conns, lanStudio())
	ch.testSteps = steps
	ch.mu.Unlock()
	a := sh.settings.agent
	a.load()
	q.until(t, "the list", func() bool { return len(a.conns) == 3 })
	a.edit(conn)
	test.Tap(a.form.testB)
	q.until(t, "the test's result", func() bool { return !a.form.testB.Disabled() && len(a.form.steps.Objects) > 0 })
	w.Canvas().Unfocus()
	w.Resize(fyne.NewSize(1440, 1640)) // the list, the whole form and the test without scrolling
	savePNG(t, w, name+"_"+string(mode)+".png")
}

// TestRenderAgentPreferences: the connections (the LAN one marked plain http), the form of an
// https one with its CA file, and a finished test whose third layer failed and whose fourth was
// not reached.
func TestRenderAgentPreferences(t *testing.T) {
	conn := litellm()
	conn.CAFile = "/Users/you/certs/team-ca.pem"
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) { renderAgentPreferences(t, mode, "preferences_agent", conn, testSteps(true)) })
	}
}

// TestRenderAgentPreferencesPlainHTTP: the form of an LM Studio on another machine over plain http
// with the warning under its base URL, and a passed test whose HTTP layer warns (H-95).
func TestRenderAgentPreferencesPlainHTTP(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			renderAgentPreferences(t, mode, "preferences_agent_plainhttp", lanStudio(), plainSteps())
		})
	}
}

// paragraphShown is the text a paragraph draws, its lines joined: the whole of it when nothing is cut.
func paragraphShown(p *paragraph) string {
	var lines []string
	for _, o := range test.WidgetRenderer(p).Objects() {
		lines = append(lines, o.(*canvas.Text).Text)
	}
	return strings.Join(lines, " ")
}
