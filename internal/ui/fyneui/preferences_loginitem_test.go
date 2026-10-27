package fyneui

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// fakeLoginItem stands in for macOS's SMAppService: a status and the calls made.
type fakeLoginItem struct {
	mu     sync.Mutex
	status string
	calls  []string
}

func (f *fakeLoginItem) Status() string { f.mu.Lock(); defer f.mu.Unlock(); return f.status }
func (f *fakeLoginItem) Register() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.status = append(f.calls, "register"), engine.LoginItemOn
	return nil
}
func (f *fakeLoginItem) Unregister() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.status = append(f.calls, "unregister"), engine.LoginItemOff
	return nil
}
func (f *fakeLoginItem) OpenSettings() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "settings")
	return nil
}
func (f *fakeLoginItem) set(status string) { f.mu.Lock(); f.status = status; f.mu.Unlock() }
func (f *fakeLoginItem) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// loginItemEngine is settingsEngine with a login item.
func loginItemEngine(t *testing.T, li engine.LoginItem) *engine.Session {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{}, Preferences: engine.NewPreferencesStore(cfg),
		ClusterSettings: engine.NewClusterSettingsStore(cfg), Hotbar: engine.NewHotbar(cfg), LoginItem: li})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// Preferences › App › Start at login shows what macOS reports, read when the section opens: off by
// default; the check registers the login item and shows it on; a status that waits for the user's
// approval (switched in System Settings meanwhile) shows its line and the button to System
// Settings' Login Items, which opens them; unchecking removes it. Where no login item is wired in
// the row is not there.
func TestPreferencesStartAtLogin(t *testing.T) {
	li := &fakeLoginItem{status: engine.LoginItemOff}
	s := loginItemEngine(t, li)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	status := func() string {
		l, _ := sh.settings.controls["startAtLoginStatus"].(*textLine)
		if l == nil {
			return ""
		}
		return l.text
	}
	q.until(t, "the row with macOS's status", func() bool { return status() == loginItemTexts[engine.LoginItemOff] })
	check := sh.settings.controls["startAtLogin"].(*widget.Check)
	open := sh.settings.controls["startAtLoginOpen"].(*textButton)
	if check.Checked || open.Visible() || check.Text != "Start Cogpit at login" {
		t.Errorf("checked %v, button shown %v, label %q", check.Checked, open.Visible(), check.Text)
	}

	test.Tap(check)
	q.until(t, "on", func() bool { return check.Checked && status() == loginItemTexts[engine.LoginItemOn] })

	li.set(engine.LoginItemNeedsApproval) // the user switched it off in System Settings
	mustDispatch(t, s, "preferences.loginItem", struct{}{})
	q.until(t, "waiting for approval", func() bool {
		return status() == loginItemTexts[engine.LoginItemNeedsApproval] && open.Visible() && check.Checked
	})
	test.Tap(open)
	q.until(t, "System Settings", func() bool { c := li.called(); return len(c) > 0 && c[len(c)-1] == "settings" })

	test.Tap(check)
	q.until(t, "off", func() bool {
		return !check.Checked && status() == loginItemTexts[engine.LoginItemOff] && !open.Visible()
	})
	if c := li.called(); !slices.Equal(c, []string{"register", "settings", "unregister"}) {
		t.Errorf("calls %v", c)
	}

	plain, _ := settingsEngine(t)
	psh, _, pq := newEngineShell(t, plain, Options{})
	mustDispatch(t, plain, "preferences.open", map[string]any{"section": "app"})
	pq.until(t, "the App section", func() bool { return psh.settings.controls["theme"] != nil })
	if psh.settings.controls["startAtLogin"] != nil {
		t.Error("the row without a login item")
	}
}

// Preferences › App with Start at login waiting for the user's approval in System Settings, dark
// and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderPreferencesStartAtLoginDark(t *testing.T) {
	renderPreferencesStartAtLogin(t, ThemeDark, "preferences_start_at_login_dark.png")
}

func renderPreferencesStartAtLogin(t *testing.T, mode ThemeMode, name string) {
	s := loginItemEngine(t, &fakeLoginItem{status: engine.LoginItemNeedsApproval})
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "timezone": "Europe/Berlin"})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool {
		b, ok := sh.settings.controls["startAtLoginOpen"].(*textButton)
		return ok && b.Visible()
	})
	q.drain()
	savePNG(t, w, name)
}
