package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › App › Start at login (D-48; engine/loginitem.go). Lens has "Automatic start-up"
// among its application preferences (inferred from its documentation, not captured); Cogpit's row
// is a check, "Start Cogpit at login", with a line under it saying what macOS reports, and, while
// macOS waits for the user's approval, a button to System Settings' Login Items. The check shows
// macOS's state, not a stored wish: it reads the status when the section opens
// (preferences.loginItem) and after every change, so a switch in System Settings shows here too.
// Where the platform offers no login item (UIState.LoginItem nil: Windows, Linux) the row is not
// built.

// loginItemTexts are the line under the check, by status.
var loginItemTexts = map[string]string{
	engine.LoginItemUnknown:       "Asking macOS…",
	engine.LoginItemOff:           "Off: Cogpit starts when you open it.",
	engine.LoginItemOn:            "macOS opens Cogpit when you log in; System Settings › General › Login Items lists it.",
	engine.LoginItemNeedsApproval: "macOS waits for you to allow Cogpit in System Settings › General › Login Items.",
	engine.LoginItemNotFound:      "macOS finds no login item for this Cogpit: it needs the signed Cogpit.app in Applications.",
}

// loginItemSetting returns the row and the function that shows the state in it; nil where the
// platform offers no start at login.
func (p *settingsPage) loginItemSetting() (fyne.CanvasObject, func()) {
	if p.sh.ui.LoginItem == nil {
		return nil, func() {}
	}
	const name = "startAtLogin"
	check := widget.NewCheck("Start Cogpit at login", func(on bool) {
		if p.syncing {
			return
		}
		p.sh.dispatch("preferences.setStartAtLogin", map[string]any{"enabled": on}, func(_ any, err error) {
			p.showResult(name, err)
			p.syncControls() // a refusal puts the check back where macOS says it is
		})
	})
	status := newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	status.truncate = true
	open := newPlateButton("Open Login Items…", nil, func() {
		p.sh.dispatch("preferences.openLoginItems", struct{}{}, func(_ any, err error) { p.showResult(name, err) })
	})
	openRow := container.NewHBox(open)
	open.Hide()
	openRow.Hide()
	errLine := newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	errLine.Hide()
	p.errs[name], p.controls[name], p.controls[name+"Status"], p.controls[name+"Open"] = errLine, check, status, open

	sync := func() {
		st := engine.LoginItemState{}
		if p.sh.ui.LoginItem != nil {
			st = *p.sh.ui.LoginItem
		}
		check.SetChecked(st.Status == engine.LoginItemOn || st.Status == engine.LoginItemNeedsApproval)
		text, ok := loginItemTexts[st.Status]
		if !ok {
			text = loginItemTexts[engine.LoginItemNotFound]
		}
		status.SetText(text)
		if st.Status == engine.LoginItemNeedsApproval {
			open.Show()
			openRow.Show()
		} else {
			open.Hide()
			openRow.Hide()
		}
	}
	p.sh.dispatch("preferences.loginItem", struct{}{}, nil) // macOS's status now: a switch in System Settings shows
	return container.NewVBox(heading("Start at login"), check, status, openRow, errLine, newSpace(theme.SizeNameInnerPadding)), sync
}
