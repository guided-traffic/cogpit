package fyneui

import "fyne.io/fyne/v2"

// iconTerminal is a shell window drawn for Cogpit: a frame with a title bar, a prompt's chevron
// and its cursor, as one even-odd path so that the prompt in the frame's hole is filled again. It
// spans 18 by 14 of its 24 units, so that it weighs as much as the create chip's "+" beside it.
var iconTerminal = fyne.NewStaticResource("cogpit-terminal.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<path fill-rule="evenodd" d="M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Z`+
	`M5 8v9h14V8Z`+
	`M6.6 10.7 7.8 9.5 10.8 12.5 7.8 15.5 6.6 14.3 8.4 12.5Z`+
	`M11.8 13.6h4.8v1.9h-4.8Z"/></svg>`))

// newTerminalButton is the chip at the left end of the dock's strip, after the tabs: it opens a
// terminal tab (terminal.open, ⌘⇧T) on the shown cluster. It replaced the strip's "+" menu, whose
// other item, Create resource, the create chip at the right end already offers (owner,
// 2026-10-22). It shows while the shown cluster is connected, as the create chip does; where
// terminal.open is disabled (the demo cluster) a tap says why in a toast, as ⌘⇧T does.
func newTerminalButton(sh *shell) *stripChip {
	return newStripChip(func() fyne.Resource { return iconTerminal },
		func() bool { return sh.ui.Connection.State.Live() },
		func() { sh.dispatch("terminal.open", struct{}{}, nil) })
}
