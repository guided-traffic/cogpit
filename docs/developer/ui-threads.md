# The UI's goroutines

> **Last reviewed:** 2026-10-19 · **Last updated:** 2026-10-22 · **Change:** trivial (`dock.showAddMenu`'s row removed: the dock's "+" menu is gone, its terminal chip dispatches on the main goroutine's tap); trivial (a row for the namespace filter's recheck timer, GW9, D-106); new (the audit before
> Fyne's thread check is turned off in releases, [D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases))

Fyne runs every widget on its main goroutine. Code that runs anywhere else (a goroutine, a timer,
a callback another package calls on its own goroutine, a function native code calls) must hand its
widget work to the main goroutine with `fyne.Do` (in Cogpit `sh.do`, which also drops work once the
shell stopped, [`shell.go`](../../internal/ui/fyneui/shell.go)), and must not read widget state
either. Fyne v2.8 checks this at run time while an app has not declared itself migrated to
`fyne.Do`: a canvas refresh from another goroutine is moved to the main goroutine and logged.
Cogpit's releases declare the migration and so turn that check off for speed ([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases));
there a stray call races instead, and a `fyne.DoAndWait` made on the main goroutine never returns.
So every such place is listed here, with why it is safe.

## How this page is kept

- [`TestEveryGoroutineSiteIsAudited`](../../internal/ui/fyneui/threadsites_test.go) scans the
  non-test Go files below `internal/ui` for `go` statements, `time.AfterFunc`, `time.NewTicker`,
  `time.Tick`, `time.NewTimer`, `time.After` and cgo `//export` functions, and fails for every
  place (file, function, kind) without its row here, and for every row of those kinds the code no
  longer has. A new goroutine therefore fails the tests until someone checked it and wrote down why
  it is safe.
- Rows of the kinds `callback` (a function another package calls on its own goroutine) and `wait`
  (a wait for the main goroutine) were found by reading the code; the test cannot find them. A new
  engine subscription, confirmer, reader or native callback gets its row by hand.
- In a development build (the check on) a stray call that slipped through shows as a warning toast
  naming the place, besides the line Fyne writes to the app log
  ([`threadcheck.go`](../../internal/ui/fyneui/threadcheck.go)).

| Class | Means |
|---|---|
| A | Funnels: every Fyne touch, writes and reads, runs through `sh.do`, `fyne.Do` or a `do` the caller guarantees is one, or `fyne.DoAndWait` from a goroutine that is provably not the main one |
| B | No Fyne: touches no Fyne object; what it shares with other goroutines is behind a mutex, an atomic or a channel, or its own |
| C | Was a stray call or a gap, fixed in the audit (the row says how) |
| D | Was a `fyne.DoAndWait` reachable from the main goroutine, fixed in the audit |

## The audit of 2026-10-19

Three agents read every place in three groups of files; the lead checked their rows against the
scan (the same keys as the test) and read the open points. Result: **no stray call and no
`fyne.DoAndWait` reachable from the main goroutine** in Cogpit's code; every place is A or B, but
the funnel's end at quit (C, fixed, below).
Fyne's own warnings agree: the app log (`<user cache dir>/Cogpit/cogpit.log`, where Fyne's
standard-log lines arrive through `slog.SetDefault`) holds none of them from 2026-10-11 to
2026-10-19 on the owner's Mac (checked 2026-10-19: 834 lines, no "Error in Fyne call thread", no
"has not been migrated", which Fyne writes at the first stray call). That is the evidence of the
owner's own use, not a proof: a path he never took logs nothing.

Kinds: `go` a goroutine, `AfterFunc`, `Ticker`, `Timer` and `After` package time's, `cgo` a function
native code calls (`//export`), `callback` a function another package calls on its own goroutine
(the registering API in the last column), `wait` a wait for the main goroutine. A function with two
places of the same kind has one row.

<details>
<summary>The shell, the logs tab, the tables, the pickers and the native callbacks (56 rows)</summary>

| File | Function | Kind | Class | What runs off main and why it is safe (or the fix) |
|---|---|---|---|---|
| `fyneui/shell.go` | `shell.start` | go | A | `go sh.tick()`; the ticking goroutine posts all widget work through `sh.do`. |
| `fyneui/shell.go` | `shell.tick` | Ticker | A | Every 10 s posts `refreshAges` and `certsTick` through `sh.do`; otherwise reads only the `stopTickers` channel. |
| `fyneui/shell.go` | `shell.requestDiagnostics` | go | A | Reads the engine's Diagnostics, atomics and `sh.ctx`; `newDiagView` is pure data; `sh.diag` and the status bar are set inside `sh.do`. |
| `fyneui/shell.go` | `shell.requestDiagnostics` | After | A | Only paces the same goroutine's select before the fetch; runs nothing itself. |
| `fyneui/shell.go` | `shell.dispatch` | go | A | `sh.run`: the engine's Dispatch (which may wait in the confirmer, off main), then `done` or the error inside `sh.do`; `recoverTo` toasts through `sh.do`. |
| `fyneui/shell.go` | `shell.dispatchInOrder` | go | A | Waits for the previous dispatch's channel or `sh.ctx`, then `sh.run`; `orderTail` is under `orderMu`. |
| `fyneui/shell.go` | `toastCard.arm` | AfterFunc | A | Calls `card.expire`, which is `sh.do(dismissToast)`; timer and hover state under `card.mu`. |
| `fyneui/shell.go` | `shell.onEvent` | callback | A | The engine's Subscribe (`shell.start`): coalesces with atomics and `pendingMu`; widget work through `sh.do`; the banners' `request` and `chatPanel.event` do a compare-and-swap or atomics, then `sh.do`. |
| `fyneui/fyneui.go` | `Run` | go | A | The signal goroutine calls `do(a.Quit)`, the `fyne.Do` funnel; otherwise it ends with Run. |
| `fyneui/fyneui.go` | `Run` | callback | B | `Lifecycle.SetOnStopped`: an atomic store only, on Fyne's lifecycle goroutine. |
| `fyneui/fyneui.go` | `Run` | callback | A | `Lifecycle.SetOnStarted`: on main inside the loop; only `fyne.Do` (queued, no wait). |
| `fyneui/fyneui.go` | `Run` | callback | C | The `do` funnel: once Fyne has drained its queue, `fyne.Do` runs at once on the caller's goroutine or panics. Fixed: the window's close sets `stopped` on main first and ends the dock's streams (the terminal widget calls `fyne.Do` itself). |
| `fyneui/logtab.go` | `logTab.open` | go | A | `OpenLogs` with id, sequence and context fixed on main; every widget step inside `sh.do`; the two inner goroutines close an unused engine stream or the replaced `termview.Logs`, no Fyne. |
| `fyneui/logtab.go` | `logTab.open` | callback | A | `termview.NewLogsOn(stream, sh.do)` hands termview the `fyne.Do`-backed funnel; termview's own goroutines are audited with termview. |
| `fyneui/logtab.go` | `logTab.searchChanged` | AfterFunc | A | `sh.dispatch` with the id and text copied; `done` runs through `sh.do`. |
| `fyneui/logtab.go` | `logTab.checkController` | go | A | `commandEnabled` (the registry's Enabled), then the button's tooltip, Hide, Enable or Disable inside `sh.do`. |
| `fyneui/logtab.go` | `logTab.saveDialog` | go | A | Writes lines copied on main to Fyne's file writer (storage, no canvas state); toast and the URI inside `sh.do`. |
| `fyneui/logtab_older.go` | `logTab.loadOlder` | go | A | `OlderLogs` with a request built on main; Prepend, head row and toast inside `sh.do`. |
| `fyneui/perf.go` | `shell.startPerf` | go | A | `perfYAML` builds a string into a buffered channel (no Fyne); `go m.run()` (see `perfMeter.run`). |
| `fyneui/perf.go` | `perfMeter.run` | Ticker | A | Every frame posts `m.sample` through `sh.do`; reads only the stop channels. |
| `fyneui/syncpicker.go` | `syncPicker.goTo` | go | A | `os.ReadDir`; entries, rows and status set inside `sh.do`. |
| `fyneui/syncpicker.go` | `syncPicker.goToTyped` | go | A | `os.Stat` of a path expanded on main; status or `goTo` inside `sh.do`. |
| `fyneui/syncpicker.go` | `syncPicker.sync` | go | A | `os.Stat` of the typed path, read from the entry on main; Hide and `done` inside `sh.do`. |
| `fyneui/welcome.go` | `welcomePage.load` | go | A | `Contexts` and `clusterLinks.read` (dispatches `cluster.links`); state and render inside `sh.do`. |
| `fyneui/welcome.go` | `welcomePage.subscribe` | go | B | Waits for `sh.ctx`, then cancels the engine subscription. |
| `fyneui/welcome.go` | `welcomePage.subscribe` | callback | A | The engine's Subscribe: `eventSig` under `sigMu`; `w.shown`, render and load only inside `sh.do`. |
| `fyneui/tablecheck.go` | `tableView.openSelectionMenu` | go | A | `evaluateSelection` (registry, `commandEnabled`) on references cloned on main; the menu built and shown inside `sh.do`. |
| `fyneui/tablecheck.go` | `selectionBar.askReason` | go | A | The registry's label and `commandEnabled` on cloned references; the tooltip set inside `sh.do`. |
| `fyneui/tableview.go` | `tableView.searchChanged` | AfterFunc | A | `sh.dispatch(ui.setFilter)` with the text copied; `done` (`filterBusy`, `syncSearch`) runs through `sh.do`. |
| `fyneui/nsgone.go` | `shell.recheckNamespaces` | AfterFunc | A | The timer's function is only `sh.do(...)`: clearing `nsRecheck` and the next read run on the main goroutine (GW9, D-106). |
| `fyneui/palette.go` | `cmdPalette.load` | go | A | `Registry.Palette` over a cloned state; `containersFor` reads the engine; results applied inside `sh.do`. |
| `fyneui/palette.go` | `shell.runPaletteAction` | go | A | `evaluateActions` and `containersFor` touch the engine only; `runAction` or the toast inside `sh.do`. |
| `fyneui/navigator_sections.go` | `navSections.load` | go | A | `Contexts`; render and the hotbar's marks inside `sh.do`. |
| `fyneui/navigator_sections.go` | `navSections.subscribe` | go | B | Waits for `sh.ctx`, then cancels the engine subscription. |
| `fyneui/navigator_sections.go` | `navSections.subscribe` | callback | A | The engine's Subscribe: on EventContexts `sh.do(n.load)`, nothing else. |
| `fyneui/navigatorstate.go` | `navReport.send` | AfterFunc | A | `sh.dispatch` with parameters built on main; busy and idle touched only in `done`, through `sh.do`. |
| `fyneui/menus_cluster.go` | `clusterLinks.refresh` | go | A | `read()` dispatches `cluster.links` and decodes; the loading flag and the menu's rebuild inside `sh.do`. |
| `fyneui/menus_cluster.go` | `shell.confirmRemoveCluster` | go | A | Dispatches `cluster.links`; the dialog built and shown inside `sh.do`. |
| `fyneui/hotbardrag.go` | `tileDrag.scrollAt` | go | A | A ticker's loop until stop or `sh.ctx`; each tick posts `d.scrollStep` through `sh.do`. |
| `fyneui/hotbardrag.go` | `tileDrag.scrollAt` | Ticker | A | Only feeds that goroutine; nothing runs on the timer. |
| `fyneui/filepicker.go` | `shell.keepAccess` | go | B | `nativedialog.Store.Keep` (under its mutex) and the log per pick; no Fyne. |
| `fyneui/filepicker.go` | `systemPanels.open` | callback | A | `nativedialog.Open`'s `done` arrives on the main thread; `done(picks)` runs inside `sh.do`. |
| `fyneui/filepicker.go` | `systemPanels.save` | callback | A | `nativedialog.Save`'s `done` arrives on the main thread; `done(path)` runs inside `sh.do`. |
| `fyneui/filepicker_choosers.go` | `logTab.saveLines` | go | A | Creates and writes the picked file with lines copied on main; the toast inside `sh.do`. |
| `fyneui/filepicker_choosers.go` | `shell.writePicked` | go | A | The file's write, then `done(err)` inside `sh.do`. |
| `fyneui/windowplace.go` | `windowPlaceKeeper.changed` | AfterFunc | B | `flush` writes the pending place under `k.mu`; no Fyne. |
| `fyneui/windowplace_darwin.go` | `cogpitWindowPlaceChanged` | cgo | B | AppKit's window notifications and `dispatch_after` on the main queue, so the main goroutine; a `sync.Map` load and `keeper.changed` under a mutex. |
| `fyneui/tooltip.go` | `tooltip.hover` | AfterFunc | A | Posts `show` through `sh.do`, guarded by a generation; `t.sh` never changes. |
| `fyneui/toastcopy.go` | `copyGlyph.Tapped` | AfterFunc | A | Posts the checkmark's reset through `sh.do`; `g.timer` compared on main. |
| `fyneui/connhints_darwin.go` | `cogpitConnectionHint` | cgo | B | A wake arrives on the main thread, a network change on a global dispatch queue (not main): reads `hintFn` under `hintMu`, then the engine's non-blocking `NoteWake` or `NoteNetworkChange`. |
| `fyneui/connhints.go` | `shell.startConnectionHints` | callback | B | The `watchConnectionHints` closure that cgo export calls; only `NoteWake` and `NoteNetworkChange`. |
| `fyneui/titlebar_darwin.go` | `cogpitTitlebarChanged` | cgo | A | Called from `dispatch_async` on the main queue, so the main goroutine; posts `tb.update` through `sh.do`. |
| `fyneui/appmenu_darwin.go` | `cogpitAboutChosen` | cgo | A | An AppKit menu action on the main thread; posts `showAbout` through `sh.do`. |
| `nativedialog/panel_darwin.go` | `cogpitPanelPicked` | cgo | B | The sheet's completion handler on the main thread; appends to the request under `requestsMu`; no Fyne. |
| `nativedialog/panel_darwin.go` | `cogpitPanelDone` | cgo | A | The main thread inside GLFW's event poll (goroutine 1); calls `done`, which `systemPanels`, its only caller, wraps in `sh.do`. |
| `fyneui/threadcheck.go` | `threadCheckHandler.Handle` | callback | A | The app log's handler (`cmd/cogpit`), called on whichever goroutine logs: pairs Fyne's lines under `strayCallLog.mu`; the toast through `sh.do`. |

</details>

<details>
<summary>The chat, the agent preferences, MCP, the dock and the terminal and log views (42 rows)</summary>

| File | Function | Kind | Class | What runs off main and why it is safe (or the fix) |
|---|---|---|---|---|
| `fyneui/chat.go` | `chatPanel.pickNewest` | go | A | Calls the agent's `Conversations(ctx)`, which returns copies taken under its mutex; picking a conversation and `setConversation` run inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.requestRead` | go | A | `readLoop`: `Conversation` (a deep copy), then Markdown parsed into plain blocks (a RichText never shown, no driver call; `parsed` under `parseMu`); `applyRead` runs inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.readLoop` | wait | A | Waits for its own `sh.do` or `sh.ctx`, only on `requestRead`'s goroutine, never on main; `fyne.Do` does not wait, so the tag changes nothing. |
| `fyneui/chat.go` | `chatPanel.requestLists` | go | A | `Connections()` (cloned under the agent's mutex) and the atomic `listsQ`; every field and widget update is inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.send` | go | A | `NewConversation`/`Send` with values copied on main; the sending flag, input and scroll change inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.stop` | go | B | `agent.Stop(id)` with the id copied on main; touches no Fyne object. |
| `fyneui/chat.go` | `chatPanel.showHistory` | go | A | Only `Conversations(ctx)`; the popup is built and shown inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.deleteConversation` | go | A | `DeleteConversation(id)`; toast, `setConversation` and `showHistory` run inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.confirmClear` | go | A | Started from the dialog's callback: `ClearConversations(ctx)`; toast and `setConversation` run inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.showPayload` | go | A | `SentPayload(id, idx)`; endpoint and private flag read on main first; the dialog is built inside `c.sh.do`. |
| `fyneui/chat.go` | `chatPanel.event` | callback | B | Engine `Subscribe` through `shell.onEvent` (EventChat), on an engine goroutine: reads the fixed `c.agent` and the atomic `shownID`, starts the requests; no Fyne. |
| `fyneui/chat_edit.go` | `chatPanel.resend` | go | A | `agent.Resend` with id, index, text and attachment read on main; editor, input and scroll change inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.pickConnection` | go | A | `Connections()` and `modelsFor` (the agent, the fixed `sh.ctx`); the menu's place read on main first; the menu is built and shown inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.setConnection` | go | A | `SetConversationConnection(conv, id)`; toast and `requestRead` run inside `c.sh.do`. |
| `fyneui/chat_input.go` | `chatInput.setModel` | go | A | `SaveConnection` on a copied connection; toast and `requestLists` run inside `c.sh.do`. |
| `fyneui/chat_items.go` | `chatAnswerView.scheduleThink` | AfterFunc | A | The timer's goroutine only reads the atomic `sh.stopped`; the generation check, `SetText` and the next schedule run inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.load` | go | A | `Connections()` on the agent captured on main; the sequence check and `render` run inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.confirmDelete` | go | A | Started from the confirmation: `DeleteConnection(c.ID)`; `closeForm`, `load` and `requestLists` run inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentPrefs.makeDefault` | go | A | `SetDefaultConnection(id)`; `load` and `requestLists` run inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.save` | go | A | The form's values and key copied on main; the saves run off main; buttons and form change inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.loadModels` | go | A | `modelsFor(id)` off main; the model list, note and button change inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.test` | go | A | `TestConnection` with values, key and context made on main; the final steps and the button change inside `sh.do`. |
| `fyneui/preferences_agent.go` | `agentForm.test` | callback | A | `ChatAgent.TestConnection`'s progress, on the test's goroutine: the agent clones the steps (agent/conntest.go); shown inside `sh.do` behind a sequence check. |
| `fyneui/mcp.go` | `shell.toggleMCP` | go | A | `MCPControl` Start/Stop and `StoreMCP` (cmd/cogpit, no Fyne); `applyMCP`, toasts and Preferences run inside `sh.do`. |
| `fyneui/mcpdetails.go` | `shell.askRegenerateMCP` | AfterFunc | A | The arm delay: `sh.do(w.confirm.Enable)`; `w.confirm` is set before the timer and never reassigned. |
| `fyneui/mcpdetails.go` | `shell.regenerateMCP` | go | A | `MCPControl.Regenerate` under its own recover; `regeneratedMCP` runs inside `sh.do`. |
| `fyneui/dock.go` | `dockContent.close` | go | B | `Close` of a termview Terminal or Logs: once, a mutex, a channel closed, the session or stream closed; no Fyne object (fyne-terminal's Close only takes `closeLock`). |
| `fyneui/dockagentread.go` | `dock.flashAgentRead` | AfterFunc | A | The flash's end: map, button field and Refresh run inside `d.sh.do`; the closure only captures values. |
| `fyneui/dockoutput.go` | `dockOutputReader.ReadDockOutput` | callback | A | Installed with `Session.SetDockOutputReader`; `dock.read` calls it on MCP, agent and `sh.run` goroutines; `dock.output` runs inside `r.sh.do` and answers over a buffered channel. |
| `fyneui/dockoutput.go` | `dockOutputReader.ReadDockOutput` | wait | A | Waits for its `sh.do` or its context (`dockReadWait`); never on main, because every caller of Dispatch is off main; `fyne.Do` does not wait, so the tag changes nothing. |
| `fyneui/dockreconnect.go` | `dock.startTerminal` | go | A | Two sites: `OpenTerminal`, then the widget is made inside `d.sh.do`; the inner `go` only calls `sess.Close`, no Fyne. |
| `fyneui/dockreconnect.go` | `dock.followSession` | go | B | Closes the old termview Terminal: no Fyne object, as `dockContent.close`. |
| `fyneui/termview/logs.go` | `Logs.startReading` | go | A | Two sites: `read` (splits the stream; pending, received and ended under `l.mu`; no Fyne) and `flush` (hands batches to main through `onMain`). |
| `fyneui/termview/logs.go` | `Logs.flush` | Ticker | A | The ticker's loop drains `l.pending` under `l.mu`; `apply` (widgets, scrollback) runs only inside `onMain`. |
| `fyneui/termview/logs.go` | `Logs.onMain` | wait | A | Waits for `l.do` (the shell's `sh.do`, logtab.go) or Close; `fyne.DoAndWait` only for `NewLogs`, which only tests use; called only from flush's goroutine. |
| `fyneui/termview/logselect.go` | `logSurface.startAutoScroll` | go | A | Auto-scroll while dragging: stop, done and every captured on main; each step goes through `l.post` (`l.do` or `fyne.Do`), `autoStop` checked on main. |
| `fyneui/termview/logselect.go` | `logSurface.startAutoScroll` | Ticker | A | The same ticker; it only posts work to main. |
| `fyneui/termview/termview.go` | `Terminal.start` | go | A | Two sites: `forwardSizes` (a channel to `sess.Resize`, no Fyne) and `run` (fyne-terminal's `RunWithConnection`, whose widget work goes through `fyne.Do`/`DoAndWait` from this goroutine). |
| `fyneui/termview/termview.go` | `endNotice.Read` | callback | B | fyne-terminal's reader loop on `Terminal.run`'s goroutine: reads the session and owns its state; no Fyne. |
| `fyneui/termview/termview.go` | `stopGate.Read` | callback | B | The same reader goroutine: an atomic flag `Terminal.Stop` sets on main at the window's close; ends the output with io.EOF, no Fyne. |
| `fyneui/termview/sanitize.go` | `escFilter.Read` | callback | B | The same reader goroutine; the filter's state is its own, and the file imports no Fyne. |

</details>

<details>
<summary>Dialogs, the drawer, editors, Helm, metrics, settings and the rest (33 rows)</summary>

| File | Function | Kind | Class | What runs off main and why it is safe (or the fix) |
|---|---|---|---|---|
| `fyneui/clusteridentity.go` | `shell.icon` | go | A | Calls the engine's `ClusterIcon` with local copies; the cache maps and `identitiesChanged` run inside `sh.do`. |
| `fyneui/clustersettings_agents.go` | `settingsPage.confirmAgentWrites` | AfterFunc | A | The arm delay is only `sh.do(cd.confirm.Enable)`; the method value reads a pointer set before the timer started. |
| `fyneui/configmapdata.go` | `cmData.keepSoon` | AfterFunc | A | Reads `cd.d.sh`, set once at construction; the `closed` check and `keepNow` run inside `sh.do`. |
| `fyneui/confirm.go` | `guiConfirmer.show` | AfterFunc | A | The arm delay is only `c.sh.do(cd.confirm.Enable)`. |
| `fyneui/confirm.go` | `guiConfirmer.Confirm` | callback | A | `engine.Session.SetConfirmer` (fyneui.go); runs on the dispatching goroutine. The dialog is built, hidden and read only inside `c.sh.do`. |
| `fyneui/confirm.go` | `guiConfirmer.Confirm` | wait | A | Waits for the answer, never on main: every GUI `Dispatch` runs on a goroutine of its own; main only sends once into a buffered channel. |
| `fyneui/helmrollback.go` | `guiConfirmer.takeDialogAnswer` | callback | B | Runs inside `Confirm` on the engine's goroutine and touches only `dialogAnswers` under its mutex, which main's `confirmedByDialog` and `dropDialogAnswer` take too. |
| `fyneui/conndot.go` | `blinker.runTicker` | go | A | Reads its local `stop` and `sh.stopTickers` only; each tick is `b.sh.do(b.tick)`. |
| `fyneui/conndot.go` | `blinker.runTicker` | Ticker | A | Made and stopped inside that goroutine; `b.stop` is written on main only. |
| `fyneui/connbanner.go` | `connBanner.startTicker` | go | A | Reads the locals `t`, `stop` and `sh.stopTickers`; the `Visible` check and `fill` run inside `sh.do`. |
| `fyneui/connbanner.go` | `connBanner.startTicker` | Ticker | A | Made on main and read by the goroutine above; `Stop` and the field writes happen on main (`hide`). |
| `fyneui/copy.go` | `shell.copyText` | AfterFunc | A | A Secret's clipboard time-out: `sh.do(sh.clearSecretClipboard)`; the clipboard and `clipTimer` are touched only on main. |
| `fyneui/copy.go` | `copyButton.confirm` | AfterFunc | A | The timer comparison and `setDone` run inside `b.sh.do`. |
| `fyneui/createdialog.go` | `shell.createDialog` | go | A | The prefill through `shell.prefill` (rowmenu.go, engine only) on local parameters; `setNamespace` and `show` run inside `sh.do`. |
| `fyneui/createdialog.go` | `createForm.check` | go | A | `commandEnabled` on parameters marshalled on main; the sequence check, `denied` and `refresh` run inside `f.sh.do`. |
| `fyneui/datakeys.go` | `armSave` | AfterFunc | A | `ready()` and `b.Enable` run inside `sh.do`. |
| `fyneui/debugdialog.go` | `openDebugDialog` | go | A | The engine's `Detail` on a copied reference; `showDebugDialog` runs inside `sh.do`. |
| `fyneui/debugdialog.go` | `shell.showDebugDialog` | AfterFunc | A | `armed` and `enable()` are touched only inside `sh.do`. |
| `fyneui/deeplink.go` | `shell.attachDeepLinks` | callback | A | `engine.DeepLinkQueue.Attach`, on the queue's own goroutine: raising the window goes through `sh.do`; `OpenDeepLink` is the engine's only. |
| `fyneui/drains.go` | `drainBanner.request` | callback | A | The engine's event through `shell.onEvent` (EventDrains): an atomic flag, then the dispatch inside `sh.do`. |
| `fyneui/helmbanner.go` | `helmBanner.request` | callback | A | The engine's event through `shell.onEvent` (EventHelm): an atomic flag, then the dispatch inside `sh.do`. |
| `fyneui/helmhttp.go` | `helmHTTPBanner.request` | callback | A | The engine's event through `shell.onEvent` (EventHelmRepos): an atomic flag; `sh.ui` is read and the dispatch made inside `sh.do`. |
| `fyneui/drafts.go` | `draftsBanner.request` | callback | A | The engine's event through `shell.onEvent` (EventDrafts): an atomic flag, then the dispatch inside `sh.do`. |
| `fyneui/drawer.go` | `drawer.request` | go | A | `Detail` and `evaluateActions` (the engine's only) on a copied reference; `busy`, `dirty` and `show` run inside `sh.do`. |
| `fyneui/editortab.go` | `editorPane.changed` | AfterFunc | A | `p.sh.do(p.push)`; `push` reads the editor's text on main. |
| `fyneui/helmcharticons.go` | `shell.chartIconResource` | go | A | `ChartIcon` and `rasteriseSVG` (oksvg and rasterx, no Fyne) on locals; the cache maps and the redraw run inside `sh.do`. |
| `fyneui/helmcharts.go` | `shell.showHelmRepos` | callback | A | `engine.Session.Subscribe`: an atomic compare-and-swap, then the `sh.helmRepos` check and `rd.load` inside `sh.do`. |
| `fyneui/helmrollback.go` | `shell.showRollbackDialog` | AfterFunc | A | `armed` and `enable()` are touched only inside `sh.do`. |
| `fyneui/kubeconfigs_approval.go` | `kubeconfigUI.showApproval` | AfterFunc | A | Only `k.sh.do(a.allow.Enable)`. |
| `fyneui/metricsview.go` | `metricsPanel.refresh` | go | A | `ObjectMetrics` on values copied on main; the `closed` and `loads` check and `apply` run inside `sh.do`. |
| `fyneui/preferences_page.go` | `settingsPage.typed` | AfterFunc | A | The `timers` map, the `built` check and `set()` run inside `p.sh.do`. |
| `fyneui/preferences_updates.go` | `settingsPage.updatesSettings` | go | A | Only `commandEnabled`; `why.SetText` and `Show` run inside `p.sh.do`. |
| `fyneui/rowmenu.go` | `tableView.openRowMenu` | go | A | `evaluateActions` and `containersFor` on a copied reference and view; the snapshot check and the popup run inside `sh.do`. |

</details>

## Findings beside the classes

- **The window's close and Fyne's drain (fixed 2026-10-19, true before D-103 too).** Once Fyne's
  loop has ended it drains its queue, and from then on runs a `fyne.Do` at once on the caller's
  goroutine. The shell's funnel stopped only after that; the terminal widget calls `fyne.Do` itself.
  The `-race` run of D-103 found the terminal's reading goroutines running widget code side by
  side at quit (18 reports, none while the window ran). Now the window's close, on the main
  goroutine a frame before the drain, stops the funnel, makes each terminal end its output at its
  next read (`termview.Terminal.Stop`) and ends the dock's streams
  ([`fyneui.go`](../../internal/ui/fyneui/fyneui.go), [`dock.go`](../../internal/ui/fyneui/dock.go)).
  Left: a chunk of terminal output being drawn when the window closes is drawn to its end, so a
  race at quit remains possible, and a SIGINT or
  SIGTERM reaches Fyne's own handler, which quits without closing the window (the lead accepted
  both: they end a process that is going anyway, and Fyne's caches stay thread-safe in Cogpit's
  releases, D-103).
- **A goroutine without its recover:** the update preference's (`settingsPage.updatesSettings`)
  had none and cut an error's text by its first letter; fixed 2026-10-19. `StoreMCP` runs outside
  `toggleMCP`'s recover and `dock.showAddMenu`'s goroutine had none (`commandEnabled` recovers
  itself): left, no race; the menu and its goroutine are gone since 2026-10-22 (the dock's
  terminal chip dispatches instead).
- **`MCPControl`'s comment** said the window never calls it on the main goroutine; the first
  `Start` runs there before Fyne's loop. Corrected; it makes no Fyne call.
- **Not verified:** that the engine hands every subscriber a `UIState` it never changes afterwards
  (`onEvent` clones it, `welcomeStateSig` reads it directly); that macOS posts the wake
  notification on the main thread (the path is B either way); the terminal fork's own parsing on
  its reading goroutine, trusted to its race patches ([`PATCHES.md`](../../third_party/fyne-terminal/PATCHES.md), H-19).
