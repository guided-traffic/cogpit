# Fyne spike

The gate of [D-1](decisions.md#d-1-ui-toolkit): the parts of the UI most likely to fail on Fyne,
each with a pass condition and its result. It ran on macOS and passed, and D-1 chose Fyne; the
re-checks per platform (S5, S7) and the fallback are D-1's.

## Gate criteria

| # | Criterion | Pass when | Budget |
|---|---|---|---|
| S1 | 10,000-row table under 1,000 watch events per second | no frame over 16 ms while scrolling and while events arrive | P3, P4 |
| S2 | Sort 10,000 rows by a column | ≤ 50 ms from click to sorted view | P2 |
| S3 | Column widths | users drag a column wider, or a workaround of comparable cost | – |
| S4 | YAML editor on a 5 MB document | keystroke to repaint ≤ 16 ms with highlighting; memory proportional to the document | P8 |
| S5 | Terminal | local shell with its own `KUBECONFIG`, pod exec, resize, copy and paste; on macOS, Linux (X11 and Wayland, which Fyne picks at runtime) and Windows | – |
| S6 | MCP control | an MCP client navigates the GUI to an object; widget updates on Fyne's main goroutine | – |
| S7 | Packaging | the app packaged for macOS, Windows and Linux on CI runners | – |
| S8 | Accessibility (recorded, not gating) | what a screen reader announces for the table and the drawer with `-tags accessibility` | – |

The gate fails when S1, S2, S4 or S5 fails without a demonstrated workaround; S3 and S7 failures
are weighed. Frames count in a real window with the GPU renderer: Fyne's `test` driver paints in
software, about 18 ms for an empty 1440×900 frame.

## Results

Measured on an Apple M1 Ultra (macOS) with Fyne v2.8.1, on the demo cluster (10,000 pods, 1,000
watch events per second); frames in a real window with `cogpit --demo --perf-mode gate` (a
development build). Fyne has no frame callback, so the jank meter posts a function to the main
goroutine every 16 ms and records how late it runs ([`perf.go`](../../internal/ui/fyneui/perf.go)).
The numbers are the budgets' in [testing](testing.md#performance-budgets-proposed-not-measured).

| # | Result | Evidence |
|---|---|---|
| S1 | pass with a reservation: each window's GL set-up and a 10,000-row table's first fill stall the main thread once ([G15](../parity/gaps.md#g15-performance-and-api-load-at-scale)); after it, scrolling and view switches pass | P3, P4 |
| S2 | pass | `BenchmarkSortChange10k`, `BenchmarkSortCommand10k`, `BenchmarkTableRefresh10k` |
| S3 | pass: Fyne's table resizes a column dragged at the gap between two header cells, which is only the theme's padding (4 points) wide; Fyne reports no resize, so the shell reads the width back from the header | `TestColumnResizeByDraggingHeaderSeparator` |
| S4 | pass: `widget.TextGrid` keeps a cell per character and walks every row for its minimum size, so the editor keeps its own lines and draws the viewport's lines as `canvas.Text` runs ([`yamlview`](../../internal/ui/fyneui/yamlview/yamlview.go)): 0.63 bytes per document byte on top of the text, a `TextGrid` 25.8 | P8, `TestMemoryProportionalToDocument` |
| S5 | pass on macOS: fyne-io/terminal's `RunLocalShell` starts the shell with the process's environment, so Cogpit runs its own PTY (ConPTY on Windows) and attaches it with `RunWithConnection` ([`termview.go`](../../internal/ui/fyneui/termview/termview.go)); the widget is Cogpit's patched copy ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)) behind an escape filter ([H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input)) | `TestStartLocalBindsKubectlToContextAndNamespace`, `TestKindPodExec`, `TestKindEngineWorkflows`, `TestRawWidgetHazards`, `TestTerminalFilterNeutralisesHazards` |
| S6 | pass: `resources_find` and `ui_navigate` over MCP move the window to the object; widget updates from other goroutines go through `fyne.Do` ([conventions › UI](conventions.md#ui), [ui-threads.md](ui-threads.md)) | `TestShowcaseServiceByClusterIP`, `TestRenderDemoShowcase` |
| S7 | met on every platform (D-1): `fyne package` builds the macOS app (`make app`); the `.deb` (`make dist-linux`) and the Windows setup (`make dist-windows`) are built without it ([releasing](releasing.md)) | – |
| S8 | recorded: only `Container`, `Label`, `Button` and `Hyperlink` implement `fyne.Accessible`, and Fyne walks the tree through containers, not widget renderers, so the table and the drawer announce nothing | – |
