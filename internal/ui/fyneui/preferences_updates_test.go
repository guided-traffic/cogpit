package fyneui

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// uiUpdater stands in for the app's updater (internal/update): a reason it cannot update, or
// none, and the checks it was asked for.
type uiUpdater struct {
	why    error
	checks atomic.Int32
}

func (u *uiUpdater) Unavailable() error      { return u.why }
func (u *uiUpdater) Start(bool)              {}
func (u *uiUpdater) SetAutomaticChecks(bool) {}
func (u *uiUpdater) CheckForUpdates() error {
	u.checks.Add(1)
	return nil
}

// updatesRow returns the App section's update check box and the line under it that says why this
// copy does not update itself.
func updatesRow(t *testing.T, sh *shell) (*widget.Check, *textLine) {
	t.Helper()
	box, ok := sh.settings.controls["checkForUpdates"].(*fyne.Container)
	if !ok || len(box.Objects) != 2 {
		t.Fatalf("the checkForUpdates control: %#v", sh.settings.controls["checkForUpdates"])
	}
	return box.Objects[0].(*widget.Check), box.Objects[1].(*textLine)
}

// Preferences › App: "Check for updates automatically" shows the preference (on by default), a
// click keeps it through preferences.set, and the box follows a change made elsewhere; the line
// under it says why this copy does not update itself; the channel select offers Stable only.
func TestPreferencesUpdateRows(t *testing.T) {
	s, _ := settingsEngine(t)
	s.SetUpdater(&uiUpdater{why: errors.New("this build has no update key")})
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the App section", func() bool {
		_, ok := sh.settings.controls["checkForUpdates"]
		return sh.settings.visible() && ok
	})
	check, why := updatesRow(t, sh)
	q.until(t, "the box checked and the reason shown", func() bool {
		return check.Checked && why.Visible() && why.text == "This build has no update key"
	})
	sel, ok := sh.settings.controls["updateChannel"].(*selectField)
	if !ok || !slices.Equal(sel.Options, []string{"Stable"}) || sel.Selected != "Stable" {
		t.Fatalf("the channel select: %#v", sh.settings.controls["updateChannel"])
	}

	check.SetChecked(false)
	q.until(t, "the checks turned off", func() bool {
		p, _ := s.Preferences()
		return !p.CheckForUpdates && sh.ui.Preferences != nil && !sh.ui.Preferences.CheckForUpdates
	})
	mustDispatch(t, s, "preferences.set", map[string]any{"checkForUpdates": true})
	q.until(t, "the box follows", func() bool { return check.Checked })
}

// helpItem returns Help's Check for Updates… item.
func helpItem(t *testing.T, sh *shell) *fyne.MenuItem {
	t.Helper()
	help := sh.helpMenu()
	i := slices.IndexFunc(help.Items, func(m *fyne.MenuItem) bool { return strings.HasPrefix(m.Label, "Check for Updates…") })
	if i < 0 {
		t.Fatalf("Help has no Check for Updates…: %v", help.Items)
	}
	// Help ends with it on macOS, whose application menu has About Cogpit; elsewhere About follows.
	if mac := sh.helpMenuOn(goosMac); !strings.HasPrefix(mac.Items[len(mac.Items)-1].Label, "Check for Updates…") {
		t.Errorf("Check for Updates… does not end Help on macOS")
	}
	if linux := sh.helpMenuOn("linux"); linux.Items[len(linux.Items)-1].Label != aboutItemLabel ||
		!strings.HasPrefix(linux.Items[len(linux.Items)-2].Label, "Check for Updates…") {
		t.Errorf("Check for Updates… is not right before About Cogpit on Linux")
	}
	return help.Items[i]
}

// Help › Check for Updates… is greyed with the short reason where this Cogpit does not update
// itself, and checks through app.checkForUpdates where it does.
func TestHelpMenuCheckForUpdates(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	if item := helpItem(t, sh); !item.Disabled || item.Label != "Check for Updates… (this Cogpit does not update itself)" {
		t.Errorf("without an updater: %q, disabled %v", item.Label, item.Disabled)
	}
	s.SetUpdater(&uiUpdater{why: errors.New("not installed from the disk image: this Cogpit does not run as an app bundle")})
	if item := helpItem(t, sh); !item.Disabled || item.Label != "Check for Updates… (not installed from the disk image)" {
		t.Errorf("a development build: %q, disabled %v", item.Label, item.Disabled)
	}
	u := &uiUpdater{}
	s.SetUpdater(u)
	item := helpItem(t, sh)
	if item.Disabled || item.Label != "Check for Updates…" {
		t.Fatalf("with an updater: %q, disabled %v", item.Label, item.Disabled)
	}
	item.Action()
	q.until(t, "the check", func() bool { return u.checks.Load() == 1 })
}

// Preferences › App with the update rows on a build without the update key, and Help with its
// greyed Check for Updates… as Fyne draws a menu (macOS shows its own native menu), dark and light:
// the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderPreferencesUpdatesDark(t *testing.T) {
	renderPreferencesUpdates(t, ThemeDark, "preferences_updates_dark.png")
}

func TestRenderPreferencesUpdatesLight(t *testing.T) {
	renderPreferencesUpdates(t, ThemeLight, "preferences_updates_light.png")
}

func renderPreferencesUpdates(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "timezone": "Europe/Berlin"})
	s.SetUpdater(&uiUpdater{why: errors.New("this build has no update key")})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the App section", func() bool {
		_, ok := sh.settings.controls["checkForUpdates"]
		return sh.settings.visible() && ok
	})
	_, why := updatesRow(t, sh)
	q.until(t, "the reason", func() bool { return why.Visible() })
	settle(w)
	widget.ShowPopUpMenuAtPosition(sh.helpMenu(), w.Canvas(), fyne.NewPos(1040, 60))
	q.drain()
	savePNG(t, w, name)
}
