package fyneui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// A logs tab's single keys, k9s's in its log view, so that k9s users find them where they expect
// them. They act while the tab's lines have the keyboard (the surface over them, which opening the
// tab or a click into the lines gives it, dockfocus.go), never in the tab's search or time field,
// and never in a terminal tab, whose widget takes every key itself. Each does what the tab's own
// control does, so a change of the tab's state goes through the engine as a click's does:
//
//	t      the Timestamps check (logs.setOptions timestamps)
//	w      the Wrap check (logs.setOptions wrap)
//	2–6    a jump to 1, 5, 15, 30 minutes or an hour ago (logs.jumpTo, as typed into the time field)
//	0      the Follow check ticked: the newest line, following (the view's own state, as the check's)
//	/      the search field (termview's SetOnSlash)
//	⌘S     the Download button (Ctrl+S on Windows and Linux)
//
// ⌘S is a shortcut of the focused surface, not a main-menu item: Fyne asks the main menu before the
// focused widget, so a menu item would take the key from every text field and terminal too (a
// terminal sends Ctrl+S on to its shell). Keys the tab does not take go on to the window
// (termview's logSurface).

// logJumpKeys are the keys that jump to a time before now, as logs.jumpTo reads it.
var logJumpKeys = map[rune]string{'2': "1m ago", '3': "5m ago", '4': "15m ago", '5': "30m ago", '6': "1h ago"}

// logSaveKey is the download's shortcut as the registry writes shortcuts (palette.go
// shortcutText): ⌘S on macOS, Ctrl+S elsewhere.
const logSaveKey = "Cmd+S"

// logTabKeyHints are the keys a logs tab's lines take while they have the keyboard, for the key
// overview (keyhints.go); mac writes the download's ⌘S, else Ctrl+S.
func logTabKeyHints(mac bool) []keyHint {
	return []keyHint{
		{Keys: "t", Label: "Timestamps on or off"},
		{Keys: "w", Label: "Wrap on or off"},
		{Keys: "2–6", Label: "Jump to 1m, 5m, 15m, 30m or 1h ago"},
		{Keys: "0", Label: "Follow the newest line"},
		{Keys: "/", Label: "Search the lines"},
		{Keys: shortcutText(logSaveKey, mac), Label: "Download the lines shown"},
	}
}

// typedRune acts on a rune typed while the tab's lines have the keyboard and reports whether it
// took it.
func (lt *logTab) typedRune(r rune) bool {
	switch r {
	case 't':
		lt.timestamps.SetChecked(!lt.timestamps.Checked) // its OnChanged dispatches, as a click's
	case 'w':
		lt.wrap.SetChecked(!lt.wrap.Checked)
	case '0':
		if lt.view == nil {
			return false
		}
		lt.view.SetFollow(true)
	default:
		at, ok := logJumpKeys[r]
		if !ok {
			return false
		}
		lt.requestJump(at)
	}
	return true
}

// typedShortcut acts on a shortcut typed while the tab's lines have the keyboard (⌘S, Ctrl+S
// elsewhere) and reports whether it took it.
func (lt *logTab) typedShortcut(sc fyne.Shortcut) bool {
	s, ok := sc.(*desktop.CustomShortcut)
	if !ok || s.KeyName != fyne.KeyS || s.Modifier != fyne.KeyModifierShortcutDefault {
		return false
	}
	lt.saveLines()
	return true
}

// downloadTip is the Download button's tooltip, with its key.
func downloadTip() string {
	return "Download the lines this tab shows (" + shortcutText(logSaveKey, runtime.GOOS == goosMac) + ")"
}
