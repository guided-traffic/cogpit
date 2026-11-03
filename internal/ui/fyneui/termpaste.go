package fyneui

import (
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// Copy and paste with the right mouse button in the dock's terminals (ui-parity › Terminal and
// shells, G8; engine/termpaste.go): the Preferences' TerminalRightClickCopyPaste, which the
// window applies to every open terminal, local shells, pod shells and attached sessions alike, as
// soon as it changes, and to each terminal it opens. With it on, a right click copies the
// selection, or pastes the clipboard when nothing is selected; off, a right click does neither.
// The clipboard's text goes to the session as typed input and nowhere else: no log, no event.

// rightClickCopyPaste reports the preference; off when the session keeps no preferences.
func (sh *shell) rightClickCopyPaste() bool {
	p := sh.ui.Preferences
	return p != nil && p.TerminalRightClickCopyPaste
}

// applyTerminalPrefs gives every open terminal the right mouse button the preference says.
// Main goroutine.
func (sh *shell) applyTerminalPrefs() {
	on := sh.rightClickCopyPaste()
	for _, c := range sh.dock.contents {
		if w, ok := c.closer.(*termview.Terminal); ok && !c.closed {
			w.SetRightClickCopyPaste(on)
		}
	}
}

// Wording of the Preferences page's setting (Cogpit's own).
const (
	rightClickTitle = "Right mouse button"
	rightClickCheck = "Copy and paste"
	rightClickHelp  = "A right click in a terminal copies the selection, or pastes the clipboard when nothing is selected."
)

// rightClickSetting is the Terminal section's check for the preference; sync shows the
// preference in it.
func (p *settingsPage) rightClickSetting() (setting *pillSwitch, sync func()) {
	check := newLabelledSwitch(rightClickCheck, func(on bool) { p.setPreference("terminalRightClickCopyPaste", on) })
	return check, func() { check.SetChecked(p.prefs().TerminalRightClickCopyPaste) }
}
