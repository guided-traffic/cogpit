# Fyne spike

> **Last reviewed:** 2026-10-11 · **Last updated:** 2026-10-21 · **Change:** minor (the Status: S7's re-check with the Windows setup built on CI since 2026-10-21, S5's begun on Windows, D-111); minor (the Status: S7's re-check, the Linux package built on CI since 2026-10-20, D-108); minor (the Status names S7's re-check since CI, half done, D-1); major (the spike ran as the local MVP; results S1–S8 recorded) · **Target:** Lens Desktop 2026.10.51501

The gate of [D-1](decisions.md#d-1-ui-toolkit): before milestone M2 builds the real UI on Fyne, a
spike builds the parts most likely to fail and measures them. If the gate passes, M2 builds on
Fyne. If it fails, the engine stays in Go and the UI becomes a separate Rust/egui process.

**Status: run on 2026-10-11 as the local MVP** (the MVP builds the spike's pieces on the planned
packages, see [Results](#results)). **Gate passed; D-1 decided for Fyne** (owner, 2026-10-11), with
re-checks of S7 and S5 on Windows and Linux ([D-1](decisions.md#d-1-ui-toolkit)). S7's re-check
since CI landed (2026-10-13): CI packages the macOS app, since 2026-10-20 the Linux `.deb` for
amd64 and arm64 (built with `make build` and `hack/linux/deb.sh`, not `fyne package`;
[D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)), and since
2026-10-21 the Windows setup for x64 (`make dist-windows`, the executables built with `go build`
and go-winres's resources, the setup with NSIS, not `fyne package`;
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)), on every push (CI's
Windows Package and Linux Package) and in the release pipeline; packaging passes on every platform
Cogpit ships for, built on native runners rather than cross-built, and the universal macOS app is
the part not done. S5's re-check is begun on Windows: the local shell on ConPTY with its own
`KUBECONFIG`, resize and the end of its programs are built and pass their session tests there
(2026-10-21); pod exec against a cluster from Windows (the kind tests run on Linux), the terminal
widget in a real Windows window (copy and paste, the console's first sequences) and Linux's X11
and Wayland are not looked at yet.
The results below are those of 2026-10-11.

## What the spike builds

A throwaway app on the planned packages (`model`, `cache`, `engine`, `mcp`, `ui/fyneui`) with two
data sources:

- **demo:** a synthetic cluster in memory with 10,000 pods, a few hundred services with known
  cluster IPs, and a configurable churn of watch-like events (default 1,000 per second);
- **kind:** a real `kind` cluster through client-go informers, with its kubeconfig in a separate
  file so that the developer's `~/.kube/config` stays untouched.

And four pieces of UI:

1. the **Pods table** with sortable headers, a namespace filter and a text filter;
2. the **detail drawer** with a **YAML editor** (highlighting, line numbers, typing, copy and
   paste);
3. a **terminal** dock tab: a local shell with a per-terminal kubeconfig, and a shell in a pod;
4. **MCP control:** an external MCP client reads `ui_state`, calls
   `resources_find {kind: services, field: clusterIP, value: …}` and `ui_navigate`, and the GUI
   follows.

## Gate criteria

Measured on the slowest supported platform available, with the GPU renderer (a real window), not
only with Fyne's offscreen test driver.

| # | Criterion | Pass when | Budget |
|---|---|---|---|
| S1 | 10,000-row table under 1,000 events per second | no frame over 16 ms while scrolling and while events arrive | P3, P4 |
| S2 | Sort by a column on 10,000 rows | ≤ 50 ms from click to sorted view | P2 |
| S3 | Column widths | users can resize columns by dragging, or a workaround of comparable cost is shown | – |
| S4 | YAML editor on a 5 MB document | keystroke to repaint ≤ 16 ms with highlighting; memory proportional to the document, not to its cells | P8 |
| S5 | Terminal | local shell with its own `KUBECONFIG`, pod exec, resize, copy and paste; on macOS, Linux (X11 and Wayland) and Windows | – |
| S6 | MCP control | an MCP client navigates the GUI to an object; widget updates happen on Fyne's main goroutine without warnings | – |
| S7 | Packaging | `fyne package` builds the app for macOS, Windows and Linux on CI runners | – |
| S8 | Accessibility (recorded, not gating) | what a screen reader announces for the table and the drawer with `-tags accessibility` | – |

**Pass:** S1–S7 pass. **Fail:** any of S1, S2, S4 or S5 fails without a workaround the spike
demonstrates. S3 and S7 failures are recorded and weighed.

## Known before the spike

Verified on 2026-10-11 by reading the sources of the pinned versions:

| Finding | Source | Consequence for the spike |
|---|---|---|
| `widget.Table` supports header rows (`ShowHeaderRow`, `CreateHeader`, `UpdateHeader`), sticky rows and columns, `SetColumnWidth` | `fyne.io/fyne/v2@v2.8.1/widget/table.go` | sorting by header click is straightforward; whether users can drag column widths was not found in the changelog (criterion S3) |
| `widget.TextGrid` renders only the visible rows, but stores every character as a `TextGridCell` and computes its minimum size by walking all rows | `widget/textgrid.go` | a 5 MB YAML must not be loaded into one TextGrid; the editor keeps its own lines and fills a grid the size of the viewport |
| Fyne's accessibility support is "currently off by default, use `-tags accessibility` to test" | Fyne changelog (v2.8.x) | criterion S8 records the state; no gate |
| Wayland is supported by default and picked at runtime | Fyne changelog | test both X11 and Wayland in S5 |
| `fyne-io/terminal`'s `RunLocalShell` starts the shell with the process's own environment | `term_unix.go` | per-terminal `KUBECONFIG` needs Cogpit's own PTY and `RunWithConnection` |
| `fyne-io/terminal` uses ConPTY on Windows | `term_windows.go` | the same approach is available to Cogpit's own PTY |
| `fyne-io/terminal` has no tagged releases; commit `46cd12724ff3` (2026-10-09) requires an unreleased Fyne, commit `91063bb29d69` (2026-09-27) builds with Fyne 2.8.1 | its `go.mod` history | pin `91063bb29d69`; 2026-10-12: `46cd12724ff3`'s code builds and passes its tests on Fyne 2.8.1 (only its `go.mod` asks for more), and Cogpit's copy moved to it ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)) |
| Widget updates from other goroutines go through `fyne.Do` or `fyne.DoAndWait` | `fyne.io/fyne/v2@v2.8.1/thread.go` | engine events reach the UI only through `fyne.Do` |

## Effort

16–24 agent-hours.

## Results

Run on 2026-10-11 on an **Apple M1 Ultra (20 cores), macOS 26.6.2**, Go 1.27.2, Fyne v2.8.1,
fyne-io/terminal `91063bb29d69`, at commit `4a3401a`. "Offscreen" means Fyne's `test` driver
with its CPU software painter, which re-rasterises every frame and costs about 18 ms for an empty
1440×900 frame on its own, so offscreen frame times say nothing about the GPU renderer the gate
asks for. The GPU measurement is the one real-window run at the end (S1).

| # | Result | Value and evidence |
|---|---|---|
| S1 | **pass with reservation** (owner, 2026-10-11) | Real window with the GPU renderer, 1440×900, `cogpit --demo --perf-mode gate`: the jank meter posts a callback to the main goroutine every 16 ms and records how late it runs (Fyne has no frame callback, so this is a proxy for frame time), from the shell's start, while the table auto-scrolls 3 rows per frame through 10,000 pods under 1,000 watch events per second, then while it switches through all six views. Two runs: from start to the first rows 0.44–0.58 s, with one stall of 420–563 ms while the window opens; one stall of 151–193 ms when the table first fills; after that, scrolling had at most three stalls of 16–25 ms in 9 s, a view switch at most 42 ms (Events, 2,000 rows; the others ≤ 20 ms), and the views phase p99 6.3–7.4 ms. CPU profile of the first 5 s: `gl.painter.Init` (Fyne's per-window GL set-up) takes 0.31 s on the main thread, and creating text textures for the first full table most of the fill stall (inferred from the profile, not isolated per frame). The owner's rule, set before the measurement: a stall once per window start passes with a reservation and counts toward P7; a stall over 100 ms at every view switch would have been a defect blocking M2. The reservation is tracked in [G15](../parity/gaps.md#g15-performance-and-api-load-at-scale) ([`perf.go`](../../internal/ui/fyneui/perf.go)) |
| S2 | **pass** | Sort 10,000 rows by another column: 10.2 ms (`BenchmarkSortChange10k`), and 11.6 ms from the `ui.sort` command to the new snapshot's event on an idle view (`BenchmarkSortCommand10k`), both in [`internal/engine/bench_test.go`](../../internal/engine/bench_test.go); applying a 10,000-row snapshot to the table: 41 µs without paint (`BenchmarkTableRefresh10k` in [`internal/ui/fyneui/bench_test.go`](../../internal/ui/fyneui/bench_test.go)). Budget P2 is 50 ms |
| S3 | **pass** | Fyne 2.8.1's table resizes a column when the user drags the gap between two header cells (`Cursor()` returns the resize cursor there, `Dragged` calls `SetColumnWidth`); `TestColumnResizeByDraggingHeaderSeparator` in [`internal/ui/fyneui/resize_test.go`](../../internal/ui/fyneui/resize_test.go) drags 60 points and the column widens by 60. Caveats: the grab zone is only the 4-point gap, and Fyne has no resize callback (the shell reads the width back) |
| S4 | **pass** | Own line storage and a viewport-sized set of `canvas.Text` runs ([`internal/ui/fyneui/yamlview`](../../internal/ui/fyneui/yamlview/yamlview.go)). Real window with the GPU renderer (`--perf-mode gate`): one character per frame (645 keystrokes in 10 s) into the middle of a 5 MB document with highlighting; the widget's work per keystroke p50 0.09 ms, max 2.1 ms; the main loop kept every frame within 16 ms but one of 19 ms, after the window's own opening stall; `SetText` of 5 MB 5.5 ms. Memory: 0.63 bytes per document byte on top of the text, against 25.8 bytes per byte (129 MB) for a TextGrid holding the same document |
| S5 | **pass on macOS, on a patched widget** | Local shell on Cogpit's own PTY with `KUBECONFIG=<own file>:<the user's files>`: `kubectl config current-context` and the namespace come from the terminal's file, which is 0600, holds no credential and is removed on close (`TestStartLocalBindsKubectlToContextAndNamespace` in [`internal/terminal/localshell_test.go`](../../internal/terminal/localshell_test.go)); pod exec over WebSocket with SPDY fallback against kind (`TestKindPodExec`); resize (`stty size` → 45 123); kubectl in Cogpit's terminal lists the kind cluster's pods (`TestKindEngineWorkflows`); ⌘C/⌘V, double-click and drag selection (offscreen, synthetic events). The pinned fyne-io/terminal froze (a tab at the right margin) or crashed (delete and insert characters, scroll regions) on output a container can send, and changed the process directory on OSC 7; Cogpit builds against a patched copy ([`third_party/fyne-terminal`](../../third_party/fyne-terminal/PATCHES.md)) that survives every known crashing input, with an escape filter ([`termview/sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go)) in front (`TestRawWidgetHazards`, `TestTerminalFilterNeutralisesHazards`); see [H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input). Linux (X11, Wayland) and Windows not run |
| S6 | **pass** | An MCP client over loopback HTTP calls `resources_find` and `ui_navigate`; the engine's UI state and the next table snapshot follow ([`showcase_test.go`](../../internal/testkit/integration/showcase_test.go)), and the Fyne shell over the real engine and demo cluster opens Services, selects the row, scrolls to it and opens the drawer, with all widget updates through `fyne.Do` (`TestRenderDemoShowcase`, [PNG](../../internal/ui/fyneui/testdata/demo_showcase.png)). In a real window, Claude Code (`claude -p` with this server as its only MCP server, `--strict-mcp-config`) answered "show me the Service with ClusterIP 10.96.100.100 in Cogpit" by calling `resources_find` and `ui_navigate` against the kind cluster; Cogpit's UI state then had Services open, `web` selected and its drawer open (read back over MCP; both calls in the audit log) |
| S7 | **partial** | `make app` runs `fyne package` (fyne.io/tools v1.7.3) and produces an ad-hoc signed `Cogpit.app` for arm64 that `codesign --verify` accepts. Not done: a universal (arm64 + x86_64) build, CI runners, Windows and Linux packages (CI was out of scope for the MVP) |
| S8 | **recorded** | With `-tags accessibility` (builds on macOS with AppKit), only `Container`, `Label`, `Button` and `Hyperlink` implement `fyne.Accessible`, and the tree walk does not descend into widget renderers; the table and the detail drawer therefore announce nothing. The tree is rebuilt only on window show and `SetContent` |

**Gate reading.** S1 passes with a reservation (owner, 2026-10-11): the window's GL set-up and the
first fill of a 10,000-row table stall the main thread once per window start; view switches and
steady work keep the frame budget. S2, S3, S4, S5 and S6 pass; S7 is partial (local macOS build
only); S8 is recorded. The owner decided D-1 for Fyne on 2026-10-11, with re-checks of S7 (when
M0's CI lands) and S5 (Windows and Linux, at M11). S5 holds only because Cogpit builds against a patched copy of fyne-io/terminal
(`third_party/fyne-terminal`): the pinned upstream commit froze or crashed on container output.

## Revision history

| Date | Change | Summary |
|---|---|---|
| 2026-10-21 | minor | The Status: S7's re-check, CI and the release pipeline package the Windows setup since 2026-10-21 (D-111, on the branch `windows-release`), so packaging is checked on every platform Cogpit ships for, the universal macOS app aside; S5's re-check begun on Windows (the local shell on ConPTY built and tested, the widget in a real window not looked at); the results stay those of 2026-10-11 |
| 2026-10-20 | minor | The Status: S7's re-check, CI packages the Linux `.deb` for amd64 and arm64 since 2026-10-20 (D-108), Windows still nothing; the results stay those of 2026-10-11 |
| 2026-10-15 | minor | The Status names S7's re-check since CI (the macOS app packaged on every push, Windows compiled and linked, no Windows or Linux package) and S5's at M11; the results stay those of 2026-10-11 |
| 2026-10-11 | minor | S1 and S4 measured again in a real window (gate mode with view switches, typing, a CPU profile); S1 passes with a reservation (owner), S4 passes |
| 2026-10-11 | major | Results S1–S8 from the local MVP on an M1 Ultra |
| 2026-10-11 | major | First version: plan, gate criteria, findings from the sources |
