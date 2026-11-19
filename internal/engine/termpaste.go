package engine

import "errors"

// Copy and paste with the right mouse button in the dock's terminals (ui-parity › Terminal and
// shells, G8): the Preferences' TerminalRightClickCopyPaste, off by default (Cogpit's default,
// inferred: Lens's default is not documented). With it on, a right click in a terminal copies the
// selection to the clipboard, or pastes the clipboard when nothing is selected; with it off, a
// right click neither copies nor pastes.
//
// A paste is a write to the session's input: in a pod shell, the container's stdin, where a pasted
// line ending in a newline runs as typed. It is the user's own input, as typing is, so no
// confirmation asks; and no command pastes, so an agent cannot type the clipboard into a shell. The
// preference changes what the user's click does in a shell, so only the user may set it, in
// Cogpit's window: an agent that turned it on would make a right click the user expects to do
// nothing run what the clipboard holds (Cogpit's default, not the owner's decision).

// ErrRightClickGUIOnly refuses a change of the terminals' right mouse button from anywhere but the
// user's own action in Cogpit's window.
var ErrRightClickGUIOnly = errors.New("only the user, in Cogpit's own window, can set what the right mouse button does in " +
	"a terminal; agents and the CLI are refused (it decides whether a click pastes into a shell)")
