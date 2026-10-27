package fyneui

import (
	"context"
	"errors"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// dockTerminal waits for the terminal widget of a dock tab.
func dockTerminal(t *testing.T, sh *shell, q *mainQueue, id string) *termview.Terminal {
	t.Helper()
	var w *termview.Terminal
	q.until(t, "the terminal of "+id, func() bool {
		c := sh.dock.contents[id]
		if c == nil {
			return false
		}
		w, _ = c.closer.(*termview.Terminal)
		return w != nil
	})
	return w
}

// TestTerminalRightClickFollowsThePreference: the dock's terminals start with the Preferences'
// right mouse button (off by default) and follow a change of it at once, local terminals and pod
// shells alike, and terminals opened afterwards start with it.
func TestTerminalRightClickFollowsThePreference(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 20)
	f.quietTerminals = true // the terminals are refreshed while open (quietReader)
	prefs := engine.DefaultPreferences()
	f.ui.Preferences = &prefs
	f.ui.Dock = []engine.DockTab{{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal: demo", Context: "demo"}}
	f.ui.ActiveDock = "terminal-1"
	sh, _, q := newTestShell(t, f, Options{})
	local := dockTerminal(t, sh, q, "terminal-1")
	if local.RightClickCopyPaste() {
		t.Fatal("on by default")
	}

	go f.update(func(u *engine.UIState) {
		p := u.Preferences.Clone()
		p.TerminalRightClickCopyPaste = true
		u.Preferences = &p
	})
	q.until(t, "the open terminal to follow", local.RightClickCopyPaste)

	ref := f.tables["pods"].Rows[0].Ref
	go f.update(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "shell-2", Type: engine.DockShell, Title: "Shell: " + ref.Name, Context: "demo", Ref: &ref, Container: "api"})
		u.ActiveDock = "shell-2"
	})
	if shell := dockTerminal(t, sh, q, "shell-2"); !shell.RightClickCopyPaste() {
		t.Error("a pod shell opened afterwards started without the preference")
	}

	go f.update(func(u *engine.UIState) {
		p := u.Preferences.Clone()
		p.TerminalRightClickCopyPaste = false
		u.Preferences = &p
	})
	q.until(t, "both terminals off", func() bool {
		return !local.RightClickCopyPaste() && !dockTerminal(t, sh, q, "shell-2").RightClickCopyPaste()
	})
}

// TestPreferencesRightClickCheck: the Terminal section's check sets the preference as the user
// (GUI origin), which agents may not; an agent's refused attempt leaves the check as it was.
func TestPreferencesRightClickCheck(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "terminal"})
	q.until(t, "the Terminal section", func() bool { return sh.settings.controls["terminalRightClickCopyPaste"] != nil })
	check := sh.settings.controls["terminalRightClickCopyPaste"].(*widget.Check)
	if check.Checked || check.Text != rightClickCheck {
		t.Fatalf("the check: %q, checked %v", check.Text, check.Checked)
	}
	test.Tap(check)
	q.until(t, "the preference kept", func() bool {
		p, _ := s.Preferences()
		return p.TerminalRightClickCopyPaste
	})

	_, err := s.Dispatch(context.Background(), engine.Invocation{ID: "preferences.set", Origin: engine.OriginExternalAgent,
		Params: mustJSON(t, map[string]any{"terminalRightClickCopyPaste": false})})
	if !errors.Is(err, engine.ErrRightClickGUIOnly) {
		t.Errorf("an agent's change: %v", err)
	}
	q.drain()
	if p, _ := s.Preferences(); !p.TerminalRightClickCopyPaste || !check.Checked {
		t.Errorf("after the refused change: kept %v, checked %v", p.TerminalRightClickCopyPaste, check.Checked)
	}
	test.Tap(check)
	q.until(t, "off again", func() bool {
		p, _ := s.Preferences()
		return !p.TerminalRightClickCopyPaste && !check.Checked
	})
}

// The Preferences page's Terminal section with the right mouse button on, dark and light.
func renderTerminalPreferences(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "terminalRightClickCopyPaste": true})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "terminal"})
	q.until(t, "the Terminal section", func() bool {
		c, ok := sh.settings.controls["terminalRightClickCopyPaste"].(*widget.Check)
		return sh.settings.visible() && ok && c.Checked
	})
	savePNG(t, w, name)
}

func TestRenderTerminalPreferencesDark(t *testing.T) {
	renderTerminalPreferences(t, ThemeDark, "preferences_terminal_dark.png")
}
