# The UI's goroutines

Every place below `internal/ui` where code runs off Fyne's main goroutine (a goroutine, a timer, a
callback another package calls on its own goroutine, a function native code calls), and why it is
safe. Such code hands its widget work, reads included, to the main goroutine with `fyne.Do` (in the
shell `sh.do`, which drops work once the shell stopped: `newShell` in
[`shell.go`](../../internal/ui/fyneui/shell.go)). Releases turn Fyne's thread check off
([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)): there a stray
call races, and a `fyne.DoAndWait` made on the main goroutine never returns.

## How this page is kept

- [`TestEveryGoroutineSiteIsAudited`](../../internal/ui/fyneui/threadsites_test.go) scans the
  non-test Go files below `internal/ui` for `go` statements, `time.AfterFunc`, `time.NewTicker`,
  `time.Tick`, `time.NewTimer`, `time.After` and cgo `//export` functions. It fails for every place
  (file, function, kind) without its row here, and for every row of those kinds that names no place
  in the code. It parses a row's first four cells (`| file | function | kind | class |`, the file
  relative to `internal/ui`), so keep their form. A goroutine started any other way
  (`sync.WaitGroup.Go`, an errgroup's `Go`) escapes the scan.
- Rows of the kinds `callback` and `wait` come from reading the code; the test cannot find them. A
  new engine subscription, confirmer, reader or native callback gets its row by hand.
- A stray call that slipped through shows in a development build as a warning toast naming the
  place, besides Fyne's line in the app log
  ([`threadcheck.go`](../../internal/ui/fyneui/threadcheck.go); the log's file: [usage.md](usage.md)).

## The audit

No place makes a stray call or a `fyne.DoAndWait` reachable from the main goroutine: every place is
A or B, but for the `do` funnel's end at quit (C).

Kinds: `go` a goroutine; `AfterFunc`, `Ticker`, `Timer` and `After` package time's; `cgo` a function
native code calls (`//export`); `callback` a function another package calls on its own goroutine
(the registering API in the last column); `wait` a wait for the main goroutine. A function with two
places of the same kind has one row.

| Class | Means |
|---|---|
| A | Funnels: every Fyne touch, reads included, runs through `sh.do`, `fyne.Do` or a `do` the caller guarantees is one, or `fyne.DoAndWait` from a goroutine that is provably not the main one |
| B | No Fyne: touches no Fyne object; what it shares with other goroutines is behind a mutex, an atomic or a channel, or its own |
| C | Safe only with a guard beyond the funnel; the row names it |
| D | A `fyne.DoAndWait` reachable from the main goroutine, a deadlock in a release; no row has it |

<details>
<summary>The shell, the logs tab, the tables, the pickers and the native callbacks</summary>

| File | Function | Kind | Class | What runs off main and why it is safe |
|---|---|---|---|---|
| `fyneui/shell.go` | `shell.start` | go | A | `go sh.tick()`: the ticker posts all widget work through `sh.do`. |
| `fyneui/shell.go` | `shell.tick` | Ticker | A | Every 10 s posts `refreshAges` and `certsTick` through `sh.do`; otherwise reads only `stopTickers`. |
| `fyneui/shell.go` | `shell.requestDiagnostics` | go | A | The engine's `Diagnostics` into pure data (`newDiagView`); `sh.diag` and the status bar set inside `sh.do`. |
| `fyneui/shell.go` | `shell.requestDiagnostics` | After | A | Only paces that goroutine's select; runs nothing itself. |
| `fyneui/shell.go` | `shell.dispatch` | go | A | `sh.run`: the engine's `Dispatch`, then `done`, the error, the focus and `recoverTo`'s toast through `sh.do`. |
| `fyneui/shell.go` | `shell.dispatchInOrder` | go | A | Waits for the previous dispatch or `sh.ctx` (`orderTail` under `orderMu`), then `sh.run`. |
| `fyneui/shell.go` | `toastCard.arm` | AfterFunc | A | `card.expire` is `sh.do(dismissToast)`; timer and hover state under `card.mu`. |
| `fyneui/shell.go` | `shell.onEvent` | callback | A | The engine's `Subscribe` (`shell.start`): coalesces under atomics and `pendingMu`; widget work through `sh.do`. |
| `fyneui/fyneui.go` | `Run` | go | A | The signal wait: `do(a.Quit)`, the `fyne.Do` funnel; it ends with `Run`. |
| `fyneui/fyneui.go` | `Run` | callback | B | `Lifecycle.SetOnStopped`, on Fyne's lifecycle goroutine: an atomic store only. |
| `fyneui/fyneui.go` | `Run` | callback | A | `Lifecycle.SetOnStarted`, on main at the loop's start: queued `fyne.Do` calls and native watches registered, no Fyne. |
| `fyneui/fyneui.go` | `Run` | callback | C | The `do` funnel: after Fyne's drain `fyne.Do` runs at once on the caller's goroutine; the window's close sets `stopped` first and ends the dock's streams. |
| `fyneui/logtab.go` | `logTab.open` | go | A | `OpenLogs` with values fixed on main; widget steps inside `sh.do`; the inner goroutines only `Close` a stream. |
| `fyneui/logtab.go` | `logTab.open` | callback | A | `termview.NewLogsOn(stream, sh.do)` hands termview the funnel; termview's goroutines have rows of their own. |
| `fyneui/logtab.go` | `logTab.searchChanged` | AfterFunc | A | `sh.dispatch` with id and text copied; `done` runs through `sh.do`. |
| `fyneui/logtab.go` | `logTab.checkController` | go | A | `commandEnabled`, then the button's tooltip and state inside `sh.do`. |
| `fyneui/logtab.go` | `logTab.saveDialog` | go | A | Writes lines copied on main to Fyne's file writer (storage, no canvas); the toast inside `sh.do`. |
| `fyneui/logtab_older.go` | `logTab.loadOlder` | go | A | `OlderLogs` with a request built on main; `Prepend`, head row and toast inside `sh.do`. |
| `fyneui/perf.go` | `shell.startPerf` | go | A | `perfYAML` builds a string into a buffered channel, no Fyne; `go m.run()` (`perfMeter.run`). |
| `fyneui/perf.go` | `perfMeter.run` | Ticker | A | Every frame posts `m.sample` through `sh.do`; reads only the stop channels. |
| `fyneui/smoke.go` | `shell.startSmoke` | go | A | A smoke run's reads of the window's frame: each `Capture`, the report and `Quit` inside `sh.do`; off main it reads an atomic and `sh.ctx` only. |
| `fyneui/smoke.go` | `shell.startSmoke` | Ticker | A | Paces those reads every `smokePoll`; runs nothing itself. |
| `fyneui/syncpicker.go` | `syncPicker.goTo` | go | A | `os.ReadDir`; entries, rows and status set inside `sh.do`. |
| `fyneui/syncpicker.go` | `syncPicker.goToTyped` | go | A | `os.Stat` of a path expanded on main; status or `goTo` inside `sh.do`. |
| `fyneui/syncpicker.go` | `syncPicker.sync` | go | A | `os.Stat` of the path read on main; `Hide` and `done` inside `sh.do`. |
| `fyneui/welcome.go` | `welcomePage.load` | go | A | `Contexts` and `clusterLinks.read` (dispatches `cluster.links`); state and render inside `sh.do`. |
| `fyneui/welcome.go` | `welcomePage.subscribe` | go | B | Waits for `sh.ctx`, then cancels the engine subscription. |
| `fyneui/welcome.go` | `welcomePage.subscribe` | callback | A | The engine's `Subscribe`: `eventSig` under `sigMu`; `w.shown`, render and load only inside `sh.do`. |
| `fyneui/tablecheck.go` | `tableView.openSelectionMenu` | go | A | `evaluateSelection` on references cloned on main; the menu built and shown inside `sh.do`. |
| `fyneui/tablecheck.go` | `selectionBar.askReason` | go | A | The registry's label and `commandEnabled` on cloned references; the tooltip inside `sh.do`. |
| `fyneui/tableview.go` | `tableView.searchChanged` | AfterFunc | A | `sh.dispatch(ui.setFilter)` with the text copied; `done` runs through `sh.do`. |
| `fyneui/palette.go` | `cmdPalette.load` | go | A | `Registry.Palette` on a cloned state (`Runs` reads package maps) and `containersFor`; results inside `sh.do`. |
| `fyneui/palette.go` | `shell.runPaletteAction` | go | A | `evaluateActions` and `containersFor`, engine only; `runAction` or the toast inside `sh.do`. |
| `fyneui/navigator_sections.go` | `navSections.load` | go | A | `Contexts`; render and the hotbar's marks inside `sh.do`. |
| `fyneui/navigator_sections.go` | `navSections.subscribe` | go | B | Waits for `sh.ctx`, then cancels the engine subscription. |
| `fyneui/navigator_sections.go` | `navSections.subscribe` | callback | A | The engine's `Subscribe`: on `EventContexts` only `sh.do(n.load)`. |
| `fyneui/navigatorstate.go` | `navReport.send` | AfterFunc | A | `sh.dispatch` with parameters built on main; busy and idle only in `done`, through `sh.do`. |
| `fyneui/menus_cluster.go` | `clusterLinks.refresh` | go | A | `read()` dispatches `cluster.links`; the loading flag and the menu's rebuild inside `sh.do`. |
| `fyneui/menus_cluster.go` | `shell.confirmRemoveCluster` | go | A | Dispatches `cluster.links`; the dialog built and shown inside `sh.do`. |
| `fyneui/hotbardrag.go` | `tileDrag.scrollAt` | go | A | A ticker loop until stop or `sh.ctx`; each tick posts `d.scrollStep` through `sh.do`. |
| `fyneui/hotbardrag.go` | `tileDrag.scrollAt` | Ticker | A | Only feeds that goroutine; nothing runs on the timer. |
| `fyneui/filepicker.go` | `shell.keepAccess` | go | B | `nativedialog.Store.Keep` (under its mutex) and log lines; no Fyne. |
| `fyneui/filepicker.go` | `systemPanels.open` | callback | A | `nativedialog.Open`'s `done`, on the main thread; `done(picks)` inside `sh.do`. |
| `fyneui/filepicker.go` | `systemPanels.save` | callback | A | `nativedialog.Save`'s `done`, on the main thread; `done(path)` inside `sh.do`. |
| `fyneui/filepicker_choosers.go` | `logTab.saveLines` | go | A | Writes the picked file with lines copied on main; the toast inside `sh.do`. |
| `fyneui/filepicker_choosers.go` | `shell.writePicked` | go | A | The file's write, then `done(err)` inside `sh.do`. |
| `fyneui/windowplace.go` | `windowPlaceKeeper.changed` | AfterFunc | B | `flush` writes the pending place under `k.mu`; no Fyne. |
| `fyneui/windowplace_darwin.go` | `cogpitWindowPlaceChanged` | cgo | B | AppKit's window notifications and `dispatch_after` on the main queue; a `sync.Map` load, then `keeper.changed` under its mutex. |
| `fyneui/tooltip.go` | `tooltip.hover` | AfterFunc | A | Posts `show` through `sh.do` behind a generation check; `t.sh` never changes. |
| `fyneui/toastcopy.go` | `copyGlyph.Tapped` | AfterFunc | A | Posts the checkmark's reset through `sh.do`; `g.timer` compared on main. |
| `fyneui/connhints_darwin.go` | `cogpitConnectionHint` | cgo | B | Wake on the main thread, network on a global queue: `hintFn` under `hintMu`, then non-blocking `NoteWake` or `NoteNetworkChange`. |
| `fyneui/connhints.go` | `shell.startConnectionHints` | callback | B | The `watchConnectionHints` closure the cgo export calls: only `NoteWake` and `NoteNetworkChange`. |
| `fyneui/pixelscale_darwin.go` | `cogpitBackingScaleChanged` | cgo | A | `dispatch_async` on the main queue; calls `backingFn`, read under `backingMu`: the closure below (D-121). |
| `fyneui/pixelscale.go` | `shell.startDensityWatch` | callback | A | The `watchBackingScale` closure the cgo export calls: only posts `densityWatch.check` through `sh.do`. |
| `fyneui/reducemotion_darwin.go` | `cogpitReduceMotionChanged` | cgo | A | AppKit's workspace notification on the main thread; calls `motionFn` (under `motionMu`): the closure below (D-122). |
| `fyneui/fyneui.go` | `Run` | callback | A | The `watchReduceMotion` closure: only posts `sh.systemMotionChanged` (the strip's `follow`) through `sh.do` (D-122). |
| `fyneui/fyneui.go` | `Run` | callback | A | `Lifecycle.SetOnEnteredForeground` and `SetOnExitedForeground`, GLFW's focus callback on main: each posts `lightStrip.setActive` through `sh.do` (D-122). |
| `fyneui/power_darwin.go` | `cogpitPowerChanged` | cgo | A | IOKit's source on the main run loop: under `powerMu`, calls `powerFn` (the closure below) only on an adapter-battery switch. |
| `fyneui/fyneui.go` | `Run` | callback | A | The `watchPower` closure, from the cgo export or `pollPower`'s goroutine: only posts `sh.systemMotionChanged` through `sh.do`. |
| `fyneui/titlebar_darwin.go` | `cogpitTitlebarChanged` | cgo | A | AppKit's window notifications, on the main thread; posts `tb.update` through `sh.do`. |
| `fyneui/appmenu_darwin.go` | `cogpitAboutChosen` | cgo | A | An AppKit menu action on the main thread; posts `showAbout` through `sh.do`. |
| `nativedialog/panel_darwin.go` | `cogpitPanelPicked` | cgo | B | The sheet's completion handler on the main thread; appends to the request under `requestsMu`; no Fyne. |
| `nativedialog/panel_darwin.go` | `cogpitPanelDone` | cgo | A | The main thread in GLFW's event poll; calls `done`, which `systemPanels`, its only caller, wraps in `sh.do`. |
| `fyneui/threadcheck.go` | `threadCheckHandler.Handle` | callback | A | The app log's handler (`cmd/cogpit`) on any logging goroutine: pairs lines under `strayCallLog.mu`, toasts through `sh.do`. |

</details>

<details>
<summary>The chat, the agent preferences, MCP, the dock and the terminal and log views</summary>

| File | Function | Kind | Class | What runs off main and why it is safe |
|---|---|---|---|---|
| `fyneui/chat.go` | `chatPanel.pickNewest` | go | A | `Conversations(ctx)` returns copies taken under the agent's mutex; the pick and `setConversation` inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.requestRead` | go | A | `readLoop`: a deep copy, parsed under `parseMu` into plain blocks (an unshown `RichText`, no driver call); `applyRead` inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.readLoop` | wait | A | Waits for its own `sh.do` or `sh.ctx` on `requestRead`'s goroutine, never on main. |
| `fyneui/chat.go` | `chatPanel.requestLists` | go | A | `Connections()`, cloned under the agent's mutex, and the atomic `listsQ`; every update inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.send` | go | A | `NewConversation` and `Send` with values copied on main; the sending flag, input and scroll inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.stop` | go | B | `agent.Stop(id)` with the id copied on main; no Fyne. |
| `fyneui/chat.go` | `chatPanel.showHistory` | go | A | Only `Conversations(ctx)`; the popup built and shown inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.deleteConversation` | go | A | `DeleteConversation(id)`; toast, `setConversation` and `showHistory` inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.confirmClear` | go | A | `ClearConversations(ctx)` from the dialog's callback; toast and `setConversation` inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.showPayload` | go | A | `SentPayload(id, idx)` with endpoint and private flag read on main; the dialog built inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.event` | callback | B | `EventChat` through `shell.onEvent`: reads the fixed `c.agent` and the atomic `shownID`, starts the requests; no Fyne. |
| `fyneui/chat_edit.go` | `chatPanel.resend` | go | A | `agent.Resend` with id, index, text and attachment read on main; editor, input and scroll inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.pickConnection` | go | A | `Connections()` and `modelsFor` with the menu's place read on main; the menu built and shown inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.setConnection` | go | A | `SetConversationConnection(conv, id)`; toast and `requestRead` inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.setModel` | go | A | `SaveConnection` on a copied connection; toast and `requestLists` inside `c.sh.do`. |
| `fyneui/chat_items.go` | `chatAnswerView.scheduleThink` | AfterFunc | A | Reads only the atomic `sh.stopped`; the generation check, `SetText` and the next schedule inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.load` | go | A | `Connections()` on the agent captured on main; the sequence check and `render` inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.confirmDelete` | go | A | `DeleteConnection(c.ID)` from the confirmation; `closeForm`, `load` and `requestLists` inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.makeDefault` | go | A | `SetDefaultConnection(id)`; `load` and `requestLists` inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.save` | go | A | The saves with values and key copied on main; buttons and form inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.loadModels` | go | A | `modelsOf` with the endpoint and key read on main; model list, note and button inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.test` | go | A | `agent.TestConnection` with values, key and context made on main; the final steps and the button inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.test` | callback | A | `ChatAgent.TestConnection`'s progress, its steps cloned by the agent ([`conntest.go`](../../internal/agent/conntest.go)); shown inside `sh.do` behind a sequence check. |
| `fyneui/mcp.go` | `shell.toggleMCP` | go | A | `MCPControl`'s `Start` or `Stop` and `StoreMCP` (`cmd/cogpit`, no Fyne); `applyMCP`, toasts and the preference inside `sh.do`. |
| `fyneui/mcpdetails.go` | `shell.askRegenerateMCP` | AfterFunc | A | `sh.do(w.confirm.Enable)`; `w.confirm` is set before the timer and never reassigned. |
| `fyneui/mcpdetails.go` | `shell.regenerateMCP` | go | A | `MCPControl.Regenerate` under its own recover; `regeneratedMCP` inside `sh.do`. |
| `fyneui/dock.go` | `dockContent.close` | go | B | `Close` of a termview `Terminal` or `Logs`: a once, a mutex, closed channels and session or stream; no Fyne object, fyne-terminal's `Close` included. |
| `fyneui/dockagentread.go` | `dock.flashAgentRead` | AfterFunc | A | The flash's end: map, button and `Refresh` inside `d.sh.do`; the closure captures values only. |
| `fyneui/dockoutput.go` | `dockOutputReader.ReadDockOutput` | callback | A | `Session.SetDockOutputReader`; `dock.read` calls it off main; `dock.output` inside `r.sh.do`, answered into a buffered channel. |
| `fyneui/dockoutput.go` | `dockOutputReader.ReadDockOutput` | wait | A | Waits for its `sh.do` or its context (`dockReadWait`); never on main, since every caller of `Dispatch` is off main. |
| `fyneui/dockreconnect.go` | `dock.startTerminal` | go | A | `OpenTerminal`, then the widget made inside `d.sh.do`; the inner `go` only calls `sess.Close`. |
| `fyneui/dockreconnect.go` | `dock.followSession` | go | B | Closes the old termview `Terminal`, as `dockContent.close` does. |
| `fyneui/termview/logs.go` | `Logs.startReading` | go | A | `read` splits the stream under `l.mu`, no Fyne; `flush` hands batches to main through `onMain`. |
| `fyneui/termview/logs.go` | `Logs.flush` | Ticker | A | The loop drains `l.pending` under `l.mu`; `apply` runs only inside `onMain`. |
| `fyneui/termview/logs.go` | `Logs.onMain` | wait | A | Waits for `l.do` (the shell's `sh.do`, `logtab.go`) or `Close` on `flush`'s goroutine only; `fyne.DoAndWait` only in tests (`NewLogs`). |
| `fyneui/termview/logselect.go` | `logSurface.startAutoScroll` | go | A | Stop, done and interval captured on main; each step through `l.post`, `autoStop` checked on main. |
| `fyneui/termview/logselect.go` | `logSurface.startAutoScroll` | Ticker | A | The same ticker; it only posts work to main. |
| `fyneui/termview/termview.go` | `Terminal.start` | go | A | `forwardSizes` (to `sess.Resize`, no Fyne) and `run` (fyne-terminal's `RunWithConnection`: `fyne.Do` or `DoAndWait` from that goroutine). |
| `fyneui/termview/termview.go` | `endNotice.Read` | callback | B | fyne-terminal's reader loop on `Terminal.run`'s goroutine: reads the session and owns its state; no Fyne. |
| `fyneui/termview/termview.go` | `stopGate.Read` | callback | B | The same reader goroutine: `Terminal.Stop`'s atomic flag ends the output with `io.EOF`; no Fyne. |
| `fyneui/termview/sanitize.go` | `escFilter.Read` | callback | B | The same reader goroutine; its state is its own and the file imports no Fyne. |

</details>

<details>
<summary>Dialogs, the drawer, editors, Helm, metrics, settings and the rest</summary>

| File | Function | Kind | Class | What runs off main and why it is safe |
|---|---|---|---|---|
| `fyneui/clustericon.go` | `imageDialog.changed` | AfterFunc | A | `engine.CutIcon` on copies taken before the timer (the image is never written); the badges inside `sh.do` for the newest `gen`. |
| `fyneui/clustericon.go` | `imageDialog.choose` | go | A | `engine.ReadIconSource` of a copied path; the editor set inside `sh.do`. |
| `fyneui/clustericon.go` | `imageDialog.load` | go | A | `ClusterIconSource` for the dialog's context, a string set before; editor or file panel inside `sh.do`. |
| `fyneui/clusteridentity.go` | `shell.icon` | go | A | `ClusterIcon` with local copies; the cache maps and `identitiesChanged` inside `sh.do`. |
| `fyneui/clustersettings_agents.go` | `settingsPage.confirmAgentWrites` | AfterFunc | A | The arm delay: only `sh.do(cd.confirm.Enable)`, a pointer set before the timer. |
| `fyneui/configmapdata.go` | `cmData.keepSoon` | AfterFunc | A | Reads `cd.d.sh`, set once; the `closed` check and `keepNow` inside `sh.do`. |
| `fyneui/confirm.go` | `guiConfirmer.show` | AfterFunc | A | The arm delay: only `c.sh.do(cd.confirm.Enable)`. |
| `fyneui/confirm.go` | `guiConfirmer.Confirm` | callback | A | `engine.Session.SetConfirmer` (`fyneui.go`), on the dispatching goroutine; the dialog built, hidden and read only inside `c.sh.do`. |
| `fyneui/confirm.go` | `guiConfirmer.Confirm` | wait | A | Waits for the answer off main (every `Dispatch` runs off main); main only sends once into a buffered channel. |
| `fyneui/helmrollback.go` | `guiConfirmer.takeDialogAnswer` | callback | B | Inside `Confirm`, off main: only `dialogAnswers` under its mutex, which main's `confirmedByDialog` and `dropDialogAnswer` take too. |
| `fyneui/conndot.go` | `blinker.runTicker` | go | A | Reads its local `stop` and `sh.stopTickers` only; each tick is `b.sh.do(b.tick)`. |
| `fyneui/conndot.go` | `blinker.runTicker` | Ticker | A | Made and stopped inside that goroutine; `b.stop` is written on main only. |
| `fyneui/lightstrip.go` | `lightStrip.runTicker` | go | A | The strip's clock (D-122): locals and the atomic `pending`; each frame's `s.sh.do` checks `s.stop`, one frame waiting at most. |
| `fyneui/lightstrip.go` | `lightStrip.runTicker` | Ticker | A | Made and stopped inside that goroutine; `s.stop` is written and read on main only. |
| `fyneui/power.go` | `pollPower` | go | A | Windows' and Linux's power poll: locals and `read` (no Fyne); `fn` posts through `sh.do`; a panic ends it with a log line. |
| `fyneui/power.go` | `pollPower` | Ticker | B | Made and stopped inside that goroutine. |
| `fyneui/connbanner.go` | `connBanner.startTicker` | go | A | Reads the locals `t`, `stop` and `sh.stopTickers`; the `Visible` check and `fill` inside `sh.do`. |
| `fyneui/connbanner.go` | `connBanner.startTicker` | Ticker | A | Made on main and read by that goroutine; `Stop` and the field writes on main (`hide`). |
| `fyneui/copy.go` | `shell.copyText` | AfterFunc | A | A Secret's clipboard time-out: `sh.do(sh.clearSecretClipboard)`; clipboard and `clipTimer` touched on main only. |
| `fyneui/copy.go` | `copyButton.confirm` | AfterFunc | A | The timer comparison and `setDone` inside `b.sh.do`. |
| `fyneui/createdialog.go` | `shell.createDialog` | go | A | `shell.prefill` (`rowmenu.go`, engine only) on local parameters; `setNamespace` and `show` inside `sh.do`. |
| `fyneui/createdialog.go` | `createForm.check` | go | A | `commandEnabled` on parameters marshalled on main; the sequence check, `denied` and `refresh` inside `f.sh.do`. |
| `fyneui/datakeys.go` | `armSave` | AfterFunc | A | `ready()` and `b.Enable` inside `sh.do`. |
| `fyneui/debugdialog.go` | `openDebugDialog` | go | A | The engine's `Detail` on a copied reference; `showDebugDialog` inside `sh.do`. |
| `fyneui/debugdialog.go` | `shell.showDebugDialog` | AfterFunc | A | `armed` and `enable()` touched only inside `sh.do`. |
| `fyneui/deeplink.go` | `shell.attachDeepLinks` | callback | A | `engine.DeepLinkQueue.Attach`, on the queue's goroutine: raises the window through `sh.do`; `OpenDeepLink` is the engine's. |
| `fyneui/drains.go` | `drainBanner.request` | callback | A | `EventDrains` through `shell.onEvent`: a compare-and-swap, then the dispatch inside `sh.do`. |
| `fyneui/helmbanner.go` | `helmBanner.request` | callback | A | `EventHelm` through `shell.onEvent`: a compare-and-swap, then the dispatch inside `sh.do`. |
| `fyneui/helmhttp.go` | `helmHTTPBanner.request` | callback | A | `EventHelmRepos` through `shell.onEvent`: a compare-and-swap; `sh.ui` read and the dispatch made inside `sh.do`. |
| `fyneui/drafts.go` | `draftsBanner.request` | callback | A | `EventDrafts` through `shell.onEvent`: a compare-and-swap, then the dispatch inside `sh.do`. |
| `fyneui/drawer.go` | `drawer.request` | go | A | `Detail` and `evaluateActions` (engine only) on a copied reference; `busy`, `dirty` and `show` inside `sh.do`. |
| `fyneui/editortab.go` | `editorPane.changed` | AfterFunc | A | `p.sh.do(p.push)`; `push` reads the editor's text on main. |
| `fyneui/helmcharticons.go` | `shell.chartIconResource` | go | A | `ChartIcon` and `rasteriseSVG` (oksvg and rasterx, no Fyne) on locals; the cache maps and the redraw inside `sh.do`. |
| `fyneui/helmcharts.go` | `shell.showHelmRepos` | callback | A | `engine.Session.Subscribe`: a compare-and-swap, then the `sh.helmRepos` check and `rd.load` inside `sh.do`. |
| `fyneui/helmrollback.go` | `shell.showRollbackDialog` | AfterFunc | A | `armed` and `enable()` touched only inside `sh.do`. |
| `fyneui/kubeconfigs_approval.go` | `kubeconfigUI.showApproval` | AfterFunc | A | Only `k.sh.do(a.allow.Enable)`. |
| `fyneui/metricsview.go` | `metricsPanel.refresh` | go | A | `ObjectMetrics` on values copied on main; the `closed` and `loads` check and `apply` inside `sh.do`. |
| `fyneui/preferences_page.go` | `settingsPage.typed` | AfterFunc | A | The `timers` map, the current-pause check and `set()` inside `p.sh.do`. |
| `fyneui/preferences_updates.go` | `settingsPage.updatesSettings` | go | A | Only `commandEnabled`; `why.SetText` and `Show` inside `p.sh.do`. |
| `fyneui/rowmenu.go` | `tableView.openRowMenu` | go | A | `evaluateActions` and `containersFor` on a copied reference and view; the snapshot check and the popup inside `sh.do`. |

</details>

## Findings beside the classes

- **Quit.** Once Fyne's loop ends it drains its queue, and from then on runs a `fyne.Do` at once on
  the caller's goroutine. The window's close runs on the main goroutine a frame before that: it sets
  `stopped` (the funnel drops all later work), makes each terminal end its output at its next read
  (`termview.Terminal.Stop`) and ends the dock's streams (`Run` in
  [`fyneui.go`](../../internal/ui/fyneui/fyneui.go), `dock.closeAll` in
  [`dock.go`](../../internal/ui/fyneui/dock.go)). That keeps the terminal widget, which calls
  `fyne.Do` itself, from running widget code on its reading goroutines side by side at quit. Open: a
  chunk of terminal output being drawn when the window closes is drawn to its end, so a race at quit
  remains possible; a SIGINT or SIGTERM can reach Fyne's own handler first, which quits without
  closing the window. Both end a process that is ending, and Fyne's caches keep their locks in
  releases (D-103).
- **Goroutines without a recover of their own:** the ticker loops, the waits for `sh.ctx` and the
  signal wait in `Run`, the goroutines that only `Close` a stream or a session, and `startPerf`'s
  YAML builder. `StoreMCP`, a file write, runs after the recover in `shell.toggleMCP`, which covers
  `Start` and `Stop` only.
- **`shell.dispatch` off main:** a parameter that does not marshal becomes a toast at once, a
  main-only call. The timers that dispatch (`logTab.searchChanged`, `tableView.searchChanged`,
  `navReport.send`) pass structs of strings, which always marshal.
- **`MCPControl` makes no Fyne call:** the window calls it on goroutines of its own and, for the
  first `Start`, on the main goroutine before Fyne's loop (`shell.startMCP`).
- **Not verified:** that nothing writes into the state an `EventUIState` carries: `Session.UpdateUI`
  emits a fresh clone but returns to its caller a copy that shares its maps and slices (`onEvent`
  clones the state, `welcomeStateSig` reads it directly); that macOS posts the wake and the Reduce
  motion notifications on the main thread (both paths are safe either way); the terminal fork's own
  parsing on its reading goroutine, trusted to its race patches
  ([`PATCHES.md`](../../third_party/fyne-terminal/PATCHES.md), H-19).
