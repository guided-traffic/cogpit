# Cogpit's patches to fyne-io/terminal

This directory is `github.com/fyne-io/terminal` at commit `46cd12724ff3` (2026-10-09; moved from
`91063bb29d69` on 2026-10-12, whose two upstream commits since touch none of the patched code),
without its `cmd/` and `img/` directories, used by Cogpit through a `replace` directive in Cogpit's `go.mod`,
whose `require` names the same commit (Renovate leaves the module alone, `.github/renovate.json`:
an upstream update means syncing this copy by hand; e858ee49 had moved the `require` to
`522b7a6e5de3` while the copy stayed, set back 2026-10-15, confirmed by the owner in the audit of 2026-10-16).
Terminal output comes from containers, which anyone who can deploy to a cluster controls; the
upstream widget crashed or froze Cogpit's whole process on some of it, on Fyne's main goroutine,
where nothing can recover. Each patch is marked `Cogpit patch:` in the code. They are candidates
offered upstream (owner's OK, 2026-10-12) as four pull requests from the owner's fork, one commit
and test per fix: [#155](https://github.com/fyne-io/terminal/pull/155) the crashes,
[#156](https://github.com/fyne-io/terminal/pull/156) the hangs,
[#157](https://github.com/fyne-io/terminal/pull/157) OSC 7, and
[#158](https://github.com/fyne-io/terminal/pull/158) the races with the fuzz test (built on the
other three); drop the fork once upstream has equivalent fixes ([D-14](../../docs/developer/decisions.md#d-14-terminal-widget)).

## Crashes and hangs

| File | Patch |
|---|---|
| `output.go` | A tab stops at the right margin and never wraps; unclamped, the tab loop wrapped to column 0 before reaching its stop and never ended |
| `escape.go` | Delete characters (`CSI P`) does nothing when the cursor is past the row's cells; it sliced out of range |
| `escape.go` | Insert characters (`CSI @`) creates the cursor's row and pads it to the cursor first; it indexed a missing row or sliced out of range |
| `escape.go` | Scroll margins (`CSI r`) are clamped to the screen, and a region whose top is not above its bottom resets to the full screen; a bottom of 0 made scrolling index out of range |
| `output.go` | `scrollUp` and `scrollDown` grow the rows to the region and ignore an invalid region |
| `output.go` | `ESC 7`, `ESC 8`, `ESC D` and `ESC M` run on Fyne's main goroutine like the CSI sequences, instead of the reader goroutine; the restored cursor goes through `moveCursor`, which clamps it to the screen |
| `osc.go` | OSC 7 (the shell reporting its directory) records the directory in the configuration instead of changing the whole process's working directory; 2026-10-12: as the path, read with `net/url` (the host of `file://host/path` dropped, the percent-encoding decoded), the form offered upstream (`TestOSC_Directory`) |
| `escape.go` | 2026-10-12: a control sequence's final character is decoded as a rune (`handleEscape`); indexing the runes with the length in bytes panicked on a non-ASCII final (`ESC [ é`) |
| `escape.go` | 2026-10-12: set and reset mode (`CSI h`, `CSI l`) check for the `?` of the private modes (`escapeMode`); without a parameter (`ESC [ h`) they sliced out of range, and with one the ANSI mode lost its first digit, so `CSI 20 h` (new line mode) did nothing and `CSI 125 l` hid the cursor. ANSI mode 20 sets new line mode; `CSI ? 20 h` still does too |
| `escape.go` | 2026-10-12: with debug output on, the device attribute queries without a parameter (`ESC [ > c`, which vim sends) logged `code[1]` and indexed out of range |
| `escape.go` | 2026-10-12: repeat (`CSI b`) skips the whole lines past a screenful and a line, which leave the screen and the cursor as they were (`TestRepeatChar_LargeCount` compares with writing the character that often); a large count looped for as long as the count |
| `escape.go` | 2026-10-12: delete characters (`CSI P`) clamps its count to the cells right of the cursor; a count near `MaxInt` overflowed the cursor's column plus the count and sliced at a negative index (`TestDeleteChars_LargeCount`; found by Cogpit's review of the fork, unreachable through Cogpit's terminal view, which caps every number at 9999) |
| `escape.go` | 2026-10-12: delete lines, insert lines, scroll up and scroll down (`CSI M`, `L`, `S`, `T`) clamp their count to the lines the region has below the cursor or in all; a large count looped for as long as the count (scroll down overflowed `scrollTop+lines`) and blanked lines above the cursor or the region |
| `output.go` | 2026-10-12: the sequence or string being parsed is a byte buffer (`addCode`, `takeCode`) instead of a string grown with `+=`; an unterminated OSC, APC or DCS string, or a long CSI, took time quadratic in its length on the reader goroutine (about a minute for 1 MiB) and memory without a bound. One longer than 1 MiB (`maxCodeLen`) is dropped |
| `output.go` | 2026-10-12: ST (`ESC \`) ends an APC string as well as an OSC one; the APC went on, and all later output was added to it instead of being shown, until a NUL arrived |

## Found on the way

| File | Patch |
|---|---|
| `escape.go` | 2026-10-12: insert lines (`CSI L`) of more than one line blanks them; the copying loop stopped at `cursorRow-rows+1` instead of `cursorRow+rows-1` and copied the lines above the cursor into them (`CSI 2 L` on the second of lines 1 to 5 showed 1, blank, 1, 2, 3). Not a crash: found while bounding the count |

## Additions

| File | Patch |
|---|---|
| `color.go` | 2026-10-12: `SetANSIColors` replaces the sixteen ANSI colours; Cogpit sets Solarized's ([D-22](../../docs/developer/decisions.md#d-22-colour-theme)) from `termview`'s `init`, before any terminal exists. Upstream's colours stay the default |
| `rightclick.go`, `term.go` | 2026-10-13: `SetRightClickAction` chooses what the secondary mouse button does: `RightClickPasteSelection` (the default, upstream's behaviour: paste the text last selected in the terminal), `RightClickNone`, or `RightClickCopyPaste` (copy the selection to the clipboard, else paste the clipboard); `MouseDown` reads the selection before it clears it. A program that asked for mouse events still gets the click. Cogpit sets none or copy and paste from its terminal preference (`TestRightClickAction`) |
| `input.go`, `term.go` | 2026-10-13: off macOS, Ctrl+Insert copies the selection to the clipboard and Shift+Insert pastes the clipboard (the shortcut's, else the app's); both used the terminal's own selection buffer, which nothing outside the terminal reads, so a copy could not be pasted elsewhere. The system is read from a variable, `goos`, so that the tests on macOS cover the keys of Windows and Linux (`TestCopyPasteKeys_WindowsAndLinux`) |

## Found in the owner's gate walk (2026-10-20)

Cogpit's G16 GW7 and GW8, [D-105](../../docs/developer/decisions.md#d-105-reverse-video-as-a-state-and-a-keys-modifiers-from-the-key-itself).
Candidates not offered upstream yet.

| File | Patch |
|---|---|
| `color.go`, `output.go`, `term.go` | Reverse video (SGR 7) is a state, as in xterm (`reverse`, `cellColors`): SGR 7 turns it on however often it comes, SGR 27 and SGR 0 turn it off, and a character written while it is on gets the current colours swapped, the default text colour as its background and `DisabledButton`, upstream's choice, as its text where a colour is the default; a colour set while it is on keeps its meaning (`SGR 7` then `SGR 32` is a green background), and an erase (`CSI X`, `CSI @`) takes the colours unswapped. SGR 7 swapped the current colours each time it came and SGR 27 cleared both: zsh highlights a paste with SGR 7 until Enter, and with oh-my-zsh sends SGR 7 again part way, or before every character when zsh-syntax-highlighting is loaded (zsh 5.9 in a pseudo-terminal), so every other character showed without the highlight, and an SGR 27 dropped the colours set before the SGR 7 (`TestReverseVideo`; `testColor` compares the colours a character gets, `cellColors`, and the `[7m[37m` case expects xterm's meaning) |

## Data races (H-19)

Fyne's desktop driver runs `fyne.Do` on its main goroutine; the widget parses output on the
goroutine that reads the connection. These patches keep each field on one of the two. The rows of
2026-10-15 (`RunLocalShell`, which Cogpit does not call; decision A116) are candidates not offered
upstream yet, like the two input patches of 2026-10-13.

| File | Patch |
|---|---|
| `output.go` | 2026-10-12: the selection is cleared on the main goroutine when output arrives; it was read and cleared on the reader goroutine |
| `output.go` | 2026-10-12: the character set is looked up (`mapCharset`), and designated (`ESC (`, `ESC )`), on the main goroutine, where shift in and shift out switch it; it was read on the reader goroutine |
| `output.go` | 2026-10-12: media copy (`CSI 5 i`, `CSI 4 i`) is handled on the reader goroutine, whose parsing it changes; its printing state was set on the main goroutine, so the reader could parse output meant for the printer as text first |
| `output.go` | 2026-10-12: the bell is cleared on the main goroutine; the timer's goroutine wrote it |
| `term.go` | 2026-10-12: `RunWithConnection` writes the directory and the input, and polls the size (`columns`), on the main goroutine with `fyne.DoAndWait`; they were written and polled on the caller's goroutine, racing `Resize`, `onConfigure` and typing |
| `term.go`, `term_unix.go` | 2026-10-15: `RunLocalShell` does the same: it and `startPTY` write the directory, and it and `open` poll the size (`columns`), on the main goroutine; `open` writes the input and the pseudo-terminal there and sets the pseudo-terminal's size there (`updatePTYSize`). They were written, polled and set on the caller's goroutine, racing `Resize` (which reads the pseudo-terminal), `onConfigure` and typing (`TestRunLocalShell_MainLoop`) |
| `term_unix.go` | 2026-10-15: the check of a local shell's directory compares and records the directory on the main goroutine, where it calls `onConfigure` as before; it read and wrote the configuration on its ticker's goroutine, racing `onConfigure`, `Resize` and OSC 7 (`TestRunLocalShell_MainLoop`) |
| `input.go` | 2026-10-15: the time of the last key (`lastKeyTime`) is an atomic (`keyTyped`, `sinceKeyTyped`); keys wrote it on the main goroutine while that check read it on its ticker's goroutine (`TestRunLocalShell_MissingStartDir` wrote it from the test's goroutine, the race the detector showed first) |
| `term.go` | 2026-10-15: `ExitCode` reads the shell's state under `closeLock`, and only once `run` has waited for the shell (`exited`); it read it at any time, racing `cmd.Wait`, which writes it on the reader goroutine (`TestRunLocalShell_MainLoop`). On Windows, where a goroutine of `startPTY` records the state, the code shows once the output has ended instead of as soon as that goroutine has it |
| `term.go` | 2026-10-15, no change of code: `run` reads `content` on the reader goroutine only after the size was read on the main goroutine (`columns`), and `Resize` creates the renderer, and with it the content, before it sets a size; `CreateRenderer` never replaces the content. Listed as a known race until then; for `RunWithConnection` this holds since 2026-10-12 |

## Scrollback

2026-10-15 ([D-56](../../docs/developer/decisions.md#d-56-the-terminals-scrollback), decided by the
owner): the widget keeps the lines that scroll off the main screen, without a line limit and
bounded at 256 MiB per terminal. Upstream keeps none (`scrollDown` overwrote the top row); its draft
[#147](https://github.com/fyne-io/terminal/pull/147) keeps every row in the grid, which Fyne's
TextGrid then lays out and refreshes on every refresh (measured in D-56: 56 times the cost with
1,000 rows). Here the grid holds the screen only, as upstream's does, and is never drawn; the lines
that scroll off go into a compact store; a second grid, the view, draws copies of the rows in view.
Rows are numbered from the first the scrollback held, the scrollback's then the screen's; a row
keeps its number when it scrolls into the scrollback, so a selection by row numbers stays on its
text. Main goroutine only, like the rest of the widget's state.

| File | Patch |
|---|---|
| `history.go` | New: the scrollback. A line is what the output wrote between two line feeds, its text as UTF-8 and its styles as runs (205 bytes for 80 cells in two colours against 9,267 in the grid, `TestScrollback_Size`); the rows each line takes at a width; numbers kept while a line is held; the oldest lines dropped past `SetScrollbackLimit` (256 MiB by default); a line grown to a quarter of the bound ends there |
| `scrollback.go` | New: the view of the rows from `viewTop` (`refreshView`), the wheel (`Scrolled`: the scrollback, or a full-screen program's mouse buttons 4 and 5 or the arrow keys as `typeCursorKey` sends them), the reflow at a new size of the main screen and the scrollback (`reflow`; the cursor's line keeps its blanks up to the cursor, `padLine`, and the cursor stays on the screen: shortened, the line moved the cursor below the screen and later output landed above older lines), a carriage return on a row of blanks taking back the wrap into it (`unwrapPadding`: zsh's PROMPT_SP pads a line without a line feed to the right margin, and the joined padding pushed the next prompt into the middle of a row at another width), a row of dropped lines above the scrollback, the selection's text over both (`selectedText`), `Lines` and `LineTexts` for readers, `setCell` |
| `scrollbar.go` | New: the scroll bar, thin and wide under the pointer, its thumb dragged, a tap above or below it moving a screen |
| `render.go` | The renderer draws the view, the cursor and the bar; `refreshView` runs on the main goroutine with the cursor's refresh; `Destroy` stops the blink timer |
| `term.go` | `Resize` reflows the main screen at a new size (the alternate screen when it ends); the double click reads its row with `rowCells`; a drag past the view's edge scrolls it, and one that ends outside the terminal takes the focus back (Fyne unfocuses on a release outside the focused widget, and ⌘C went nowhere); `run` waits for the main goroutine at most every `maxRefreshInterval` and queues a refresh in between, where it waited after every read |
| `output.go` | The characters and control characters in a row go to the main goroutine in one function (`flush` in `handleOutput`), where each went in its own: in a window `seq 1 200000` took 24.6 s before the scrollback, 5.1 s with it, 1.9 s with the batches and 0.9 s with `run`'s waits (Terminal.app 0.29 s, the owner's measurement); `handleOutputChar` marks the row the output wraps from (`markWrapped`, for the reflow) and sets its cell with `setCell` (`TextGrid.SetCell` needs a renderer, which the screen's grid never gets); `scrollDown` pushes the row it scrolls off into the scrollback; the screen's grid is no longer refreshed; output clears a selection only where it reaches the screen |
| `escape.go` | Scroll up (`CSI S`) pushes its rows into the scrollback; erase saved lines (`CSI 3 J`) clears it; the alternate screen keeps the main screen's wrap marks and, resized meanwhile, reflows the main screen when it ends |
| `select.go`, `position.go`, `mouse.go` | The selection by row numbers, highlighted in the view's copies; positions are rows of the scrollback and the screen; mouse reports are the screen's (`screenPosition`) |
| `input.go` | Typing returns the view to the screen (`followOutput`), a modifier alone (⌘, Ctrl, Shift) does not; a shortcut that goes to the shell (ctrl+C, paste) does, copying does not (`isCopy`) |
| `internal/widget/termgrid.go` | `StopBlink`; the timer's closure takes the blink state as a value (it read the variable the timer's goroutine went on writing), and a tick after a stop ends the timer |

## Tests and fuzzing

| File | What |
|---|---|
| `escape_test.go` | `TestScrollBack_Tmux` expects the 5 rows of its screen rather than the 46 an unclamped region grew the grid to; 2026-10-12: a test per patch above (`TestHandleEscape_NonASCIIFinal`, `TestSetMode_WithoutPrivateMarker`, `TestDeviceAttributes_Debug`, `TestInsertLines`, `TestLineCounts_Overflow`, `TestRepeatChar_LargeCount`, `TestDeleteChars_LargeCount`) |
| `apc_test.go`, `osc_test.go` | 2026-10-12: `TestAPC_StringTerminator`, `TestOSC_LongStrings` |
| `fuzz_test.go` | 2026-10-12: `FuzzHandleOutput`, output at sizes from 0x0 to 255x255 with a resize part way, debug output on or off; a panic, or an input not done within 30 seconds, fails. Seeds: every input above and real programs' output in `test_data/sessions` (vim 9.1, less 668, macOS `nano` (UW PICO 5.09) and GNU screen 4.00.03 running less, recorded in an 80x24 pseudo-terminal on synthetic files) |
| `mainloop_test.go` | 2026-10-12: the fuzz target runs `fyne.Do` on a stand-in for the desktop driver's main goroutine, so that the race detector sees the two goroutines; Fyne's test driver runs `fyne.Do` on the calling goroutine |
| `term_test.go`, `term_unix_test.go` | 2026-10-14: the tests that start a local shell (`TestExitCode01`, `TestTerminal_Close`) write to it once its terminal is open, read under the widget's lock (`startLocalShell`), and wait for `RunLocalShell` to return within a bound (`shellTimeout`, 60 s) instead of without one. They wrote until a write succeeded, but `New` gives a terminal a writer that discards its input until the terminal is open, so the first write, 50 ms after the start, always succeeded: on a loaded machine it came before the terminal was open, the shell never got its `exit`, and `TestExitCode01` waited until the test binary's 10-minute timeout (CI run 38527183426, Linux); `TestTerminal_Close` read the command while it was still nil and panicked. Reproduced on macOS with a 100 ms delay before the terminal opens and on Linux (`golang:1.27.2`, bash) on a container limited to 0.05 CPUs; upstream's test is the same (`c8f30fa` made it stop at the first write that succeeds) |
| `scrollback_test.go` | 2026-10-15: `TestScrollback_*`: lines kept and numbered, every editing sequence changing the screen only, `CSI S` and `CSI 3 J`, a wrapped line as one, the alternate screen, reflow (narrower, wider, shorter, taller, after the alternate screen), the bound and its row, the size of a line, the view (scrolled back, kept while output arrives, returned by typing, holding the screen's rows only), a selection over the scrollback and the screen, a double click in the scrollback, the wheel in the alternate screen, the bar |
| `scrollback_test.go` | 2026-10-15, after the owner's check in a window: `TestScrollback_PromptPaddingIsNoWrap` (zsh's PROMPT_SP), `TestScrollback_RandomOutputKeepsItsOrder` (100 random runs of output, zsh's padding, resizes and scrolling: lines keep their order, the cursor stays on the screen), `TestScrollback_ShortcutsToTheShellReturnTheView`, `TestScrollback_DragEndingOutsideKeepsTheFocus`, and modifiers and the wheel's arrow keys in `TestScrollback_View` and `TestScrollback_WheelInTheAlternateScreen` |
| `term_test.go` | 2026-10-15: `TestTerminal_Resize_Narrower` expects the line wrapped again at the new width and the cursor after its last character, where upstream moves the cursor to the right edge |
| `fuzz_test.go` | 2026-10-15: each input ends by releasing the terminal's renderers, as a closed tab does, which stops the timer of blinking text; the reflow brought blinking text back onto screens that had dropped it, and timers that outlived their test ran Fyne's test driver on their own goroutines |
| `term_unix_test.go` | 2026-10-15: `TestRunLocalShell_MainLoop` runs a local shell (`/bin/sh`, which does not report its directory by OSC 7 as macOS's zsh does) with `fyne.Do` on the main-loop stand-in: started before the terminal has a size, sized, written to and resized on the main loop, `cd` typed there until the directory check reports the new directory to a listener while the main loop resizes the terminal (which reads the configuration the check writes), the exit code read while the shell runs on a goroutine that synchronises with nothing else and lives until the shell has exited (the race detector can miss what a finished goroutine read), then `exit 3`. Against the code before the rows of 2026-10-15 above the detector reports seven races (the size, the directory twice, the input, the pseudo-terminal, the last key's time, the exit code); with them, none in 20 runs of the module under `-race` on macOS and on Linux (`golang:1.27.2`, bash, `/bin/sh` dash), and in 10 runs of the shell tests at `-cpu 1,2` on macOS. `TestRunLocalShell_MissingStartDir` marks a key as typed through `keyTyped` |

The fuzz seeds and every test run under the race detector in Cogpit's `make test-unit-race` and
CI (`make test-fork`), since 2026-10-15 the tests that start a local shell too (decision A116):
until then they ran without it, because `RunLocalShell` had the races patched above. The tests
themselves no longer read the input and the command while `RunLocalShell` writes them since
2026-10-14 (50 runs under `-race` on macOS, 20 on Linux); run together under `-race` on
2026-10-15, `TestRunLocalShell_MissingStartDir`'s write of the last key's time still raced an
earlier test's directory check (patched above).

## Fuzzing record (2026-10-12, Apple M1 Ultra, shared with other runs)

| Target | Run | Result |
|---|---|---|
| `FuzzHandleOutput` | 11 min 42 s, 1,035,471 execs, 4 workers, after the first five patches above | one timeout, whose minimised input passed alone in 0.1 s: the harness then kept every input's widget (about 650 KB each) and slowed down; harness fixed, input discarded |
| `FuzzHandleOutput` | four shorter runs, 1 to 4 min, up to 224,818 execs, on the main loop | no crash or hang in the widget; workers died or stalled from the harness's memory use (up to 20 GiB a worker: Fyne's test driver never expires renderers, and a bell redraws after the clean-up) and once from the machine running out of memory; harness fixed (`releaseRenderers`, `flush`), not rerun for long |
| `FuzzRawWidget` (termview) | 9 min 48 s, 164,674 execs, 3 workers, before the main loop | no finding before the run was stopped |
| `FuzzHandleOutput` | 2026-10-12, fifth run: 10 min, 443,665 execs, `-parallel 2`, with the fixed harness and the main loop, on an otherwise idle machine | no crash, no hang; 38 new interesting inputs |
| `FuzzRawWidget` (termview) | 2026-10-12, fifth run: 10 min, 72,941 execs, `-parallel 2` | no crash, no hang; 24 new interesting inputs |
| `FuzzHandleOutput` | 2026-10-15, after the batches and the fixes of the owner's check: 2 min, `-parallel 2` | no crash, no hang |
| `FuzzHandleOutput` | 2026-10-15, with the scrollback and its reflow: 3 min, 106,152 execs, and 2 min, 143,070 execs, `-parallel 2` | no crash, no hang; the stretches at 0 execs/s while new inputs are minimised show on the unchanged fork too (1 min, 86,273 execs) |

Crashers and hangs fixed above came from the seeds, the fuzzer's first minutes and reading the
code around them; the string, APC and line-count findings were measured with probes (1 MiB of
unterminated OSC took about 60 s). The ten-minute runs with the fixed harness (fifth run) found nothing; longer ones are welcome.

## Known and not patched

- A DCS string ended by ST swallows the character after it (`ESC P zz ESC \ x` drops the `x`).
- An ESC at byte 5000 of one read is not taken for an escape: `noEscape` is 5000 and doubles as
  the ESC's offset.
- Cursor movement by more than `math.MaxInt` minus the cursor's row or column overflows and moves
  the cursor to the opposite edge (`CSI 9223372036854775807 B` from row 2 goes to the top).
- Media copy buffers everything up to `CSI 4 i` without a bound, by design of the feature.
- `Write` before the terminal is open reports success and discards what it was given (`New` sets a
  discarding input), although its comment promises an error; this is what let the local-shell
  tests hang. Cogpit never calls `Write`; typing before the terminal is open is dropped either way.
  On a goroutine other than Fyne's main one it reads the input that `RunWithConnection` (since
  2026-10-12) and `RunLocalShell` (since 2026-10-15) set on the main goroutine, and nothing public
  lets such a caller wait for the open terminal; on the main goroutine, where typing calls it, it
  does not race.
- On Windows, `startPTY`'s goroutine writes the shell's state and clears the pseudo-terminal
  without synchronisation, racing `ExitCode` and `updatePTYSize`. Nothing runs the widget under the
  race detector on Windows (Cogpit's CI compiles its tests there), and Cogpit does not call
  `RunLocalShell`.
- Blinking text starts a ticker goroutine that runs until no blinking cell is left in view; since
  2026-10-15 it also stops when the terminal's renderer goes (a closed or hidden tab).
- The arrow keys, and the wheel in the alternate screen, send `ESC O` in the alternate screen and
  `ESC [` outside it: the widget does not track the application cursor-key mode (DECCKM) itself.
- While a window is resized with the mouse on macOS, output stops until the mouse is released: Fyne's
  GLFW loop runs queued functions only between its event polls, and `glfwPollEvents` stays in
  Cocoa's resize loop meanwhile (read in Fyne 2.8.1's `loop.go`). Fyne's, not patched.
- Wide characters (CJK) take one cell, also in the scrollback and its reflow.
- A reversed character's default colours are the theme's when it is written: after a switch between
  dark and light, highlighted text written before keeps the old theme's (as upstream's swap did).

`go.mod` requires Fyne v2.8.1 (Cogpit's own module builds it with v2.8.2, the latest release, since
the Renovate updates of 2026-10-15, e858ee49), where upstream's asks for an unreleased Fyne
(`v2.8.2-0.20261009111023-0a528b3a387b`); its indirect requirements are the ones `go mod tidy`
derives from that, so the copy never raises a module in Cogpit's build above what Fyne v2.8.1
needs. The code builds and its tests pass on v2.8.1 (2026-10-12), `TestExitCode*` included, which
hung before upstream's `c8f30fa` and, on a loaded machine, after it until 2026-10-14 (above).
Licence: BSD-3-Clause, see [`LICENSE`](LICENSE).
