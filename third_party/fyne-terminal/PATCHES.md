# Cogpit's patches to fyne-io/terminal

This directory is a patched copy of `github.com/fyne-io/terminal`, the widget Cogpit's terminal
tabs are built on ([D-14](../../docs/developer/decisions.md#d-14-terminal-widget)). Terminal output
comes from containers, which anyone who can deploy to a cluster controls, and the widget acts on it
on Fyne's main goroutine, where a panic ends the app and a loop freezes it; most patches close such
holes, the rest add what Cogpit needs. Each patch is marked `Cogpit patch` in a comment.

## Base and upstream

- **Base:** upstream commit `46cd12724ff3`, without its `cmd/` and `img/` directories. Cogpit's
  [`go.mod`](../../go.mod) requires that commit and replaces the module with this directory;
  Renovate leaves the module alone ([`.github/renovate.json`](../../.github/renovate.json)), so
  moving onto a newer upstream commit is a sync by hand.
- **Upstream:** the crash, hang, OSC 7 and race fixes are merged upstream as
  [#155](https://github.com/fyne-io/terminal/pull/155),
  [#156](https://github.com/fyne-io/terminal/pull/156),
  [#157](https://github.com/fyne-io/terminal/pull/157) and
  [#158](https://github.com/fyne-io/terminal/pull/158), in the form the review asked for, which
  differs from this copy's in places (upstream trims the `?` where this copy has `escapeMode`, and
  keeps fewer comments); upstream's master also has commits of its own this copy lacks. The other
  patches are not offered. The tables name each patch's pull request. The copy goes once upstream
  has equivalent fixes for all of them (D-14).
- **`go.mod`:** Fyne v2.8.1, where upstream's base asks for the unreleased
  `v2.8.2-0.20261009111023-0a528b3a387b`; the indirect requirements are Fyne v2.8.1's own, so the
  copy never raises a module in Cogpit's build above what Fyne v2.8.1 needs. Cogpit builds the
  widget with its own Fyne, v2.8.2; the copy's tests, run in its own module, use v2.8.1.
- **Licence:** BSD-3-Clause, [`LICENSE`](LICENSE).

## Crashes and hangs

| File | Patch | Prevents | Upstream |
|---|---|---|---|
| `output.go` | A tab stops at the right margin and never wraps (`handleOutputTab`) | an endless loop | #156 |
| `escape.go` | Delete characters (`CSI P`) does nothing when the cursor is past the row's cells, and clamps its count to the cells right of the cursor (`TestDeleteChars_LargeCount`) | a slice out of range, also at a count near `MaxInt` | #155 |
| `escape.go` | Insert characters (`CSI @`) creates the cursor's row and pads it to the cursor first | an index out of range on a fresh screen | #155 |
| `escape.go` | Scroll margins (`CSI r`) are clamped to the screen; a region whose top is not above its bottom resets to the full screen | scrolling indexing out of range (a bottom of 0) | #155 |
| `output.go` | `scrollUp` and `scrollDown` grow the rows to the region and ignore an invalid region | an index out of range | #155 |
| `output.go` | `ESC 7`, `ESC 8`, `ESC D` and `ESC M` run on the main goroutine like the CSI sequences; the restored cursor goes through `moveCursor`, which clamps it to the screen | a panic on the reader goroutine (`ESC M` before any row exists) and races with the main goroutine | #158 |
| `escape.go` | A control sequence's final character is decoded as a rune (`handleEscape`, `TestHandleEscape_NonASCIIFinal`) | a panic on a non-ASCII final (`ESC [ é`) | #155 |
| `escape.go` | Set and reset mode (`CSI h`, `CSI l`) check for the private marker `?` (`escapeMode`); ANSI mode 20 sets new line mode, as `CSI ? 20 h` does (`TestSetMode_WithoutPrivateMarker`) | a panic without a parameter (`ESC [ h`), and an ANSI mode read as a private one without its first digit | #155 |
| `escape.go` | With debug output on, the device attribute queries without a parameter (`ESC [ > c`, which vim sends) log without indexing `code[1]` (`TestDeviceAttributes_Debug`) | a panic | #155 |
| `escape.go` | Repeat (`CSI b`) skips the whole lines past a screenful and a line, which leave the screen and the cursor as they were (`TestRepeatChar_LargeCount`) | a loop as long as the count | #156 |
| `escape.go` | Delete lines, insert lines, scroll up and scroll down (`CSI M`, `L`, `S`, `T`) clamp their count to the lines the region has below the cursor or in all (`TestLineCounts_Overflow`) | a loop as long as the count, and lines above the cursor or the region blanked | #156 |
| `output.go` | The sequence or string being parsed is a byte buffer (`addCode`, `takeCode`); one longer than 1 MiB (`maxCodeLen`) is dropped (`TestOSC_LongStrings`) | quadratic time on the reader goroutine, and memory without a bound, for an unterminated OSC, APC or DCS string or a long CSI | #156 |
| `output.go` | ST (`ESC \`) ends an APC string as well as an OSC one (`TestAPC_StringTerminator`) | all later output going into the APC until a NUL | #156 |
| `osc.go` | OSC 7 (the shell reporting its directory) records the path of its `file://` URL in the configuration and tells the listeners (`setDirectory`, `directoryOf` with `net/url`: the host dropped, the percent-encoding decoded; `TestOSC_Directory`) | output changing the whole process's working directory | #157 |

## Output and keys

The reverse-video and key patches are
[D-105](../../docs/developer/decisions.md#d-105-reverse-video-as-a-state-and-a-keys-modifiers-from-the-key-itself).

| File | Patch | Prevents | Upstream |
|---|---|---|---|
| `escape.go` | Insert lines (`CSI L`) of more than one line blanks them: the copying stops at `cursorRow+rows-1` (`TestInsertLines`) | the lines above the cursor copied into the inserted ones | #156 |
| `color.go`, `output.go`, `term.go` | Reverse video (SGR 7) is a state, as in xterm (`reverse`, `cellColors`): SGR 7 turns it on however often it comes, SGR 27 and SGR 0 turn it off; a character written while it is on gets the current colours swapped (where a colour is the default: the theme's text colour as background, `ColorNameDisabledButton`, upstream's choice, as text); a colour set while it is on keeps its meaning (`SGR 7` then `SGR 32` is a green background); an erase (`CSI X`, `CSI @`) takes the colours unswapped (`TestReverseVideo`, with `testColor`) | a repeated SGR 7 (zsh sends it again while it highlights a paste) undoing the highlight, and SGR 27 dropping the colours set before SGR 7 | not offered |
| `input.go`, `term.go` | The widget keeps no modifier keys of its own: a key takes ⇧ from its own event (`keyModifiers`: the desktop driver's `CurrentKeyModifiers`), and a drag selects a block when ⌥ was held at the primary button's press (`pressAlt`, from `MouseDown`'s event); `KeyDown` and `KeyUp` do nothing (`TestTypedKey_ShiftReleaseNeverSeen`, `TestKeyModifiers_FromTheDesktopDriver`, `TestDragged_BlockFromThePress`; `TestTypedShortcut_ScreenshotKeysSendNothing` and `TestPaste_BracketedWithBothMarkers` cover ⇧⌘4 and a multi-line paste) | a modifier whose release never reached the widget (⇧⌘4's screenshot overlay takes the keys) staying held | not offered |
| `input.go` | Keys typed with ⇧ send xterm's sequences, as the terminfo entry `xterm-256color` names them (`typeModifiedKey`, `modifiedKeys`): `CSI 1;2 A`–`D` for the arrows, also in application cursor mode, `CSI 1;2 P`–`S` for ⇧F1–⇧F4, `CSI Z` for ⇧Tab; a key without a form of its own (Return, Enter, Backspace, Escape) goes as without ⇧ (`TestTerminal_TypedKey`'s ⇧ rows) | sequences no program reads as one key (`ESC [ A ; 2`), and ⇧Return or ⇧Tab sending nothing | not offered |
| `input.go` | Ctrl, or Ctrl and ⇧, with a named key (`TypedShortcut`) sends xterm's sequence with the modifier parameter 5 or 6 (`CSI 1;5 B` for Ctrl+↓, `CSI 3;5 ~` for Ctrl+Delete, `CSI 1;6 C` for Ctrl+⇧+→); Ctrl with Return, Enter or Backspace goes as without Ctrl, as in xterm; Ctrl with Tab or Escape sends nothing (`TestTerminal_TypedShortcut`'s Ctrl rows) | the key name's first letter going out as a control character (Ctrl+↓ as `^D`, which ends a shell at an empty prompt) | not offered |

## Additions

| File | Patch | Why | Upstream |
|---|---|---|---|
| `color.go` | `SetANSIColors` replaces the sixteen ANSI colours; upstream's stay the default (`TestSetANSIColors`) | Cogpit sets Solarized's ([D-22](../../docs/developer/decisions.md#d-22-colour-theme)) from `termview`'s `init`, before any terminal exists | not offered |
| `rightclick.go`, `term.go` | `SetRightClickAction` chooses what the secondary mouse button does: `RightClickPasteSelection` (the default, upstream's behaviour: paste the text last selected in the terminal), `RightClickNone`, or `RightClickCopyPaste` (copy the selection to the clipboard, else paste the clipboard); `MouseDown` reads the selection before it clears it; a program that asked for mouse events still gets the click (`TestRightClickAction`) | Cogpit sets `RightClickNone`, or `RightClickCopyPaste` when its terminal preference is on | not offered |
| `input.go`, `term.go` | Off macOS, Ctrl+Insert copies the selection to the clipboard and Shift+Insert pastes the clipboard (the shortcut's, else the app's: `clipboardOf`). The system is read from a variable, `goos`, so that the tests on macOS cover the keys of Windows and Linux (`TestCopyPasteKeys_WindowsAndLinux`) | both used the terminal's own selection buffer, which nothing outside the terminal reads | not offered |

## Data races (H-19)

Fyne's desktop driver runs `fyne.Do` on its main goroutine; the widget parses output on the
goroutine that reads the connection. These patches keep each field on one of the two
([H-19](../../docs/security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input)). Cogpit
starts its sessions with `RunWithConnection`; the `RunLocalShell` rows matter to the widget's own
tests.

| File | Patch | Upstream |
|---|---|---|
| `output.go` | The selection is cleared on the main goroutine when output arrives | #158 |
| `output.go` | The character set is looked up (`mapCharset`), and designated (`ESC (`, `ESC )`), on the main goroutine, where shift in and shift out switch it | #158 |
| `output.go` | Media copy (`CSI 5 i`, `CSI 4 i`) is handled on the reader goroutine, whose parsing it changes, so the reader cannot parse output meant for the printer as text | #158 |
| `output.go` | The bell is cleared on the main goroutine (`ringBell`) | #158 |
| `output.go` | The bell's clear leaves a terminal alone once `Close` has run (`isStopping`): its refresh would make the renderers a closed tab released again (`FuzzHandleOutput`, termview's `TestRawWidgetThreading`) | not offered |
| `term.go` | `RunWithConnection` writes the directory and the input, and polls the size (`columns`), on the main goroutine with `fyne.DoAndWait`, so they do not race `Resize`, `onConfigure` and typing | #158 |
| `term.go`, `term_unix.go` | `RunLocalShell` does the same: it and `startPTY` write the directory, it and `open` poll the size (`columns`), and `open` writes the input and the pseudo-terminal and sets the pseudo-terminal's size (`updatePTYSize`) on the main goroutine (`TestRunLocalShell_MainLoop`) | not offered |
| `term_unix.go` | A local shell's directory check compares and records the directory on the main goroutine, where it calls `onConfigure`, so it does not race `onConfigure`, `Resize` and OSC 7 (`TestRunLocalShell_MainLoop`) | not offered |
| `input.go` | The time of the last key (`lastKeyTime`) is an atomic (`keyTyped`, `sinceKeyTyped`): keys write it on the main goroutine, the directory check reads it on its ticker's goroutine | not offered |
| `term.go` | `ExitCode` reads the shell's state under `closeLock`, and only once `run` has waited for the shell (`exited`), so it does not race `cmd.Wait` on the reader goroutine (`TestRunLocalShell_MainLoop`). On Windows, where a goroutine of `startPTY` records the state, the code shows once the output has ended | not offered |

Without a patch: `run` reads `content` on the reader goroutine only after `columns` has read a size
on the main goroutine, `Resize` creates the renderer, and with it the content, before it sets a
size, and `CreateRenderer` never replaces the content.

## Scrollback

The widget keeps the lines that scroll off the main screen, without a line limit and bounded at
256 MiB per terminal ([D-56](../../docs/developer/decisions.md#d-56-the-terminals-scrollback)).
The screen's grid (`content`) holds the screen only and is never drawn; the lines that scroll off go
into a compact store; a second grid, the view, draws copies of the rows in view. Rows are numbered
from the first the scrollback held, the scrollback's then the screen's; a row keeps its number when
it scrolls into the scrollback, so a selection by row numbers stays on its text. Main goroutine
only, like the rest of the widget's state. Not offered as a pull request: the model is described in
a comment on upstream's open [#147](https://github.com/fyne-io/terminal/pull/147), which keeps every
row in the grid, and fixes to #147's own scrolling are open as
[andydotxyz/terminal#2](https://github.com/andydotxyz/terminal/pull/2).

| File | Patch |
|---|---|
| `history.go` | New: the scrollback. A line is what the output wrote between two line feeds, its text as UTF-8 and its styles as runs (a line of 80 cells in two colours under 400 bytes, `TestScrollback_Size`); the rows each line takes at a width; numbers kept while a line is held; the oldest lines dropped past `SetScrollbackLimit` (256 MiB by default, `TestScrollback_Limit`); a line grown to a quarter of the bound ends there |
| `scrollback.go` | New: the view of the rows from `viewTop` (`refreshView`); the wheel (`Scrolled`: a program that asked for the mouse gets buttons 4 and 5, the alternate screen the arrow keys as `typeCursorKey` sends them, otherwise the view scrolls); the reflow of the main screen and the scrollback at a new size (`reflow`: the cursor's line keeps its blanks up to the cursor, `padLine`, and the cursor stays on the screen, so later output cannot land above older lines); a carriage return on a row of blanks taking back the wrap into it (`unwrapPadding`: zsh's PROMPT_SP padding, joined, would push the next prompt into the middle of a row at another width); a row noting the dropped lines above the scrollback (`droppedNotice`); the selection's text over both (`selectedText`); `Lines` and `LineTexts`, which Cogpit's terminal view reads; `setCell` |
| `scrollbar.go` | New: the scroll bar, thin and wide under the pointer, its thumb dragged, a tap above or below it moving a screen |
| `render.go` | The renderer draws the view, the cursor and the bar; `refreshView` runs on the main goroutine with the cursor's refresh; `Destroy` stops the blink timer |
| `term.go` | `Resize` reflows the main screen at a new size (the alternate screen's program draws itself again, and the main screen reflows when it ends); the double click reads its row with `rowCells`; a drag past the view's edge scrolls it, and one that ends outside the terminal takes the focus back (Fyne unfocuses on a release outside the focused widget, and ⌘C would go nowhere); `run` waits for the main goroutine at most every `maxRefreshInterval` (17 ms) and queues one refresh in between, rather than a round trip after every read |
| `output.go` | The characters and control characters in a row go to the main goroutine in one function (`flush` in `handleOutput`), rather than a hand-over per character; `handleOutputChar` marks the row the output wraps from (`markWrapped`, for the reflow) and sets its cell with `setCell` (`TextGrid.SetCell` needs a renderer, which the screen's grid never gets); `scrollDown` pushes the row it scrolls off into the scrollback (`pushToHistory`: on the main screen, with the region at its top); output clears a selection only where it reaches the screen (`selectionOnScreen`) |
| `escape.go` | Scroll up (`CSI S`) pushes its rows into the scrollback; erase saved lines (`CSI 3 J`) clears it (`clearHistory`); the alternate screen keeps the main screen's wrap marks and, resized meanwhile, reflows the main screen when it ends |
| `select.go`, `position.go`, `mouse.go` | The selection by row numbers, highlighted in the view's copies; positions are rows of the scrollback and the screen; mouse reports are the screen's (`screenPosition`) |
| `input.go` | Typing returns the view to the screen (`followOutput`), a modifier alone (⌘, Ctrl, ⇧) does not (`isModifier`); a shortcut that goes to the shell (Ctrl+C, paste) does, copying does not (`isCopy`) |
| `internal/widget/termgrid.go` | `StopBlink`; the timer's closure takes the blink state as a value, not the variable the timer's goroutine goes on writing, and a tick after a stop ends the timer |

## Tests and fuzzing

The copy is a module of its own, which `go test ./...` from Cogpit's root leaves out:
`make test-fork` ([`Makefile`](../../Makefile)) runs its tests and fuzz seeds under the race
detector, and `make test-unit-race` and CI's job Race Tests (other packages) (`make test-race-rest`,
on Linux) run it first. Nothing runs the copy's tests on Windows. Cogpit's terminal view also fuzzes
the widget through its public API (`FuzzRawWidget`). `fuzz_test.go` and `mainloop_test.go` went
upstream with #158 in an earlier form (upstream also has a `race_test.go`, which this copy does
not); the other test changes are not offered.

| File | What |
|---|---|
| `fuzz_test.go` | `FuzzHandleOutput`: output at sizes from 0x0 to 255x255, with a resize part way, debug output on or off, parsed on the test's goroutine while the widget changes on the main-loop stand-in; a panic on either goroutine, or an input not done within 30 s (`fuzzTimeout`), fails. Seeds: `fuzzSeeds` (the sequences the patches above fix, top's and tmux's output) and the recordings in `test_data/sessions` (vim, less, pico (UW PICO 5.09), and GNU screen running less). Each input ends by closing its terminal and releasing its renderers (`releaseRenderers`), as a closed tab does, which stops the timer of blinking text and leaves a later bell's clear nothing to redraw |
| `mainloop_test.go` | `useMainLoop` stands in for the desktop driver's main goroutine: `fyne.Do` and `fyne.DoAndWait` run their functions on one goroutine, so the race detector sees it apart from the reader (Fyne's test driver runs `fyne.Do` on the calling goroutine). A `fyne.DoAndWait` made on the main loop fails the test: in a release build, with Fyne's thread check off ([D-103](../../docs/developer/decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)), it never returns |
| `term_test.go`, `term_unix_test.go` | The tests that start a local shell (`TestExitCode01`, `TestTerminal_Close`) write to it once its terminal is open (`startLocalShell`, which reads the connection under the widget's lock) and bound every wait by `shellTimeout` (60 s): `New` gives a terminal a writer that discards its input until it is open, so an earlier write succeeds and is lost. `TestRunLocalShell_MainLoop` runs `/bin/sh` on the main-loop stand-in as a window would: started before the terminal has a size, then sized, typed into and resized, `cd` reported by the directory check during resizes, the exit code read on a goroutine that synchronises with nothing else, `exit 3`. `TestRunLocalShell_MissingStartDir` marks a key as typed through `keyTyped` |
| `scrollback_test.go` | `TestScrollback_*`: lines kept and numbered, editing sequences changing the screen only, `CSI S` and `CSI 3 J`, a wrapped line as one, the alternate screen, reflow, the bound, the view (`TestScrollback_View`), a selection and a double click over the scrollback, the wheel in the alternate screen (`TestScrollback_WheelInTheAlternateScreen`), the bar, zsh's PROMPT_SP (`TestScrollback_PromptPaddingIsNoWrap`), random runs of output, padding, resizes and scrolling keeping the lines' order and the cursor on the screen (`TestScrollback_RandomOutputKeepsItsOrder`), `TestScrollback_ShortcutsToTheShellReturnTheView`, `TestScrollback_DragEndingOutsideKeepsTheFocus` |
| `escape_test.go`, `term_test.go` | Upstream tests whose expectations the copy changes, besides the key tests' ⇧ and Ctrl rows above: `TestScrollBack_Tmux` expects the 5 rows of its screen rather than the 46 an unclamped region grew the grid to; `TestTerminal_Resize_Narrower` expects the line wrapped again at the new width and the cursor after its last character, where upstream moves the cursor to the right edge; `TestTerminal_AddListener` waits up to 5 s for the configure callback, where upstream's 100 ms can pass while the race detector or a loaded runner holds up the callback's goroutine |

## Known and not patched

Cogpit's escape filter ([`termview/sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go))
stays in front of the widget: it drops OSC, DCS, APC, PM and SOS strings and media copy, and clamps
numbers to 9999.

- A DCS string ended by ST swallows the character after it (`ESC P zz ESC \ x` drops the `x`).
  Cogpit's filter drops DCS strings.
- An ESC at byte 5000 of one read is not taken for an escape: `noEscape` is 5000 and doubles as
  the ESC's offset.
- Cursor movement by more than `math.MaxInt` minus the cursor's row or column overflows and moves
  the cursor to the opposite edge (`CSI 9223372036854775807 B` from row 2 goes to the top).
  Cogpit's filter clamps the count.
- Media copy buffers everything up to `CSI 4 i` without a bound, by design of the feature. Cogpit's
  filter drops media copy.
- `Write` before the terminal is open reports success and discards what it was given (`New` sets a
  discarding input), although its comment promises an error. Called on a goroutine other than
  Fyne's main one, it reads the input that `RunWithConnection` and `RunLocalShell` set on the main
  goroutine, and nothing public lets such a caller wait for the open terminal; the widget's own
  calls (mouse reports) run on the main goroutine. Cogpit never calls `Write`.
- On Windows, `startPTY`'s goroutine writes the shell's state and clears the pseudo-terminal
  without synchronisation, racing `ExitCode` and `updatePTYSize`. Cogpit does not call
  `RunLocalShell`.
- Blinking text starts a ticker goroutine that runs until no blinking cell is left in view or the
  terminal's renderer goes (a closed or hidden tab).
- The arrow keys, and the wheel in the alternate screen, send `ESC O` while mode 1049 (the alternate
  screen) is set and `ESC [` otherwise: the widget ignores the application cursor-key mode (DECCKM,
  `CSI ? 1 h`).
- While a window is resized with the mouse on macOS, output stops until the mouse is released:
  Fyne's GLFW loop (`internal/driver/glfw/loop.go`) runs queued functions only between its event
  polls, and `glfw.PollEvents` blocks while the window is resized. Fyne's, not patched.
- Wide characters (CJK) take one cell, also in the scrollback and its reflow.
- A reversed character's default colours are the theme's when it is written: after a switch between
  dark and light, highlighted text written before keeps the old theme's.
- ⌥ (Alt) with a named key sends nothing, where xterm sends `CSI 1;3 D` and the like (what a macOS
  user expects of ⌥← in a shell, word movement, is not settled); Ctrl with Tab or Escape sends
  nothing either.
