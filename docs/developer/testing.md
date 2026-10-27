# Testing

The test tiers, where each runs, the fixtures, the rules that keep tests deterministic on every
platform CI runs, and the performance budgets. What each CI job runs and how long it takes:
[releasing › CI](releasing.md#ci).

## Tiers

| Tier | Runs where | Command | What it proves |
|---|---|---|---|
| Unit | everywhere | `make test-unit` (the UI's package in parallel shards, `make test-ui`) | logic in isolation; `model` and `kubeconfig` are fully testable without I/O |
| Race | Linux CI, locally on demand | `make test-race-ui` (five shards of one build, balanced by the tests' measured time, the render tests left out), `make test-race-rest`, `make test-unit-race` | no data races; a concurrent map write would end the process |
| No race | Linux CI | `make test-norace-coverage` | what the race build skips (tests that read `raceEnabled`, `internal/ui/logo`) and the UI's render tests, with coverage |
| macOS | CI's macOS Tests job, locally | `make test-darwin` | every `*_darwin_test.go` (title bar, window place, concealed clipboard, updater and file-panel harnesses, locale), `internal/cluster` and `internal/terminal` (PTY, shells, the credential plugin's process search, login runs and waits), the login-wait tests, and the tests whose expectations branch on macOS, under the race detector |
| Windows | CI's Unit Tests (windows) and UI Tests (windows), locally in MSYS2's MINGW64 shell | `make test-windows-unit`, `make test-windows-ui` | every package's tests on Windows, without the race detector; PowerShell for the terminal's and the registration's tests |
| Smoke | CI's Linux Package and Windows Package, locally (macOS by hand only: GitHub's macOS VMs offer GLFW no accelerated pixel format) | `make smoke` (`SMOKE_BIN`, `SMOKE_BOUND`), `make test-windows-package WINDOWS_SMOKE=on` | the binary a release ships starts, its window draws a frame with the real GL driver (on X11 read from the X server), in a home of its own, without a logged error, and it quits ([releasing › the smoke test](releasing.md#the-smoke-test)) |
| Kind-profile goldens | everywhere | part of Unit | an object becomes the row and detail model Lens would show ([`kinds/golden_test.go`](../../internal/model/kinds/golden_test.go) `TestGolden`, `TestLocalGolden`) |
| Cache | everywhere | part of Unit and Race | watches, warm period, error states, transforms, stalls, and the API requests each scenario costs (against an in-memory source and testkit's fake API server over HTTPS) |
| Engine | everywhere | part of Unit and Race | commands, snapshots, the drawer, confirmation per origin and what it binds (context, objects, UIDs), the audit log's redaction, custom kinds, the drain journal across a simulated crash |
| Command fuzzing | locally | `go test -fuzz FuzzDispatch ./internal/engine/commands` | every command, fed adversarial parameters, returns an error instead of panicking |
| MCP | everywhere | part of Unit and Race | tools match the registry, the HTTP endpoint refuses missing tokens, the stdio bridge in a fresh process of the CLI's test binary (`TestMCPOverRealStdio`) |
| Agent | everywhere | part of Unit and Race | the tool loop, its limits, redaction of what is sent, against a fake OpenAI-compatible server ([`testkit/fakellm`](../../internal/testkit/fakellm/)) |
| Cluster | Linux CI (Integration Tests (kind)), locally | `make kind-up`, `make test-kind` | views, logs, exec and attach, `kubectl` in the terminal, edits, custom resources, workload actions against a real `kind` cluster; the add-on stacks' tests run by hand (`KIND_ADDONS=1`, [hack/kind](../../hack/kind/README.md)) |
| Drain cluster | Linux CI (Integration Tests (kind drain)), locally | `make kind-drain-up`, `make test-kind-drain` | cordon, a drain blocked by a PodDisruptionBudget, a simulated crash and the finish after it, on a three-node cluster of its own |
| Envtest | Linux CI (Integration Tests (envtest)), locally (Unix) | `make test-envtest` | the real API server's path behind client-go, and a control plane frozen with `SIGSTOP` for standby; its binaries pinned by SHA-512 (`make envtest-bin`) |
| UI | everywhere with cgo | part of Unit | views render from snapshots offscreen (Fyne's `test` driver); `-update` writes the pictures to `testdata/`, `make showcase` the README's to [`docs/images/`](../images/) |
| Fuzz | locally | `go test -fuzz <Fuzz…>` | kubeconfig parsing (`FuzzContexts`, `FuzzExecPlugins`), kind profiles, the YAML tokenizer and edits, the terminal's escape filter, manifests (`FuzzParseManifests`, `FuzzEditYAML`), `cogpit://` links (`FuzzParseDeepLink`); seeds run with the unit tests |
| Real cluster | locally, opt-in, never in CI | `make test-real REAL_KUBECONFIG=<file> REAL_CONTEXT=<context>` (`COGPIT_REAL_KUBECONFIG`, `COGPIT_REAL_CONTEXT`) | discovery and the budgets P9–P11 against a real API server; node, Helm and Flux tests each need a variable naming what they may act on |
| Live Argo CD | locally, opt-in | `COGPIT_REAL_KUBECONFIG` and `COGPIT_REAL_ARGOCD_NAMESPACE` | the Argo CD section against a live Argo CD |
| Live sigstore | locally, opt-in | `COGPIT_SIGSTORE_LIVE=1` (or `download`) | the current kubectl release's signature and Rekor entry verified as a download verifies them |
| Performance | locally | `make bench`, `make scorecard` | the [budgets](#performance-budgets) |

## Fixtures

- **Recorded objects** are real API responses with names, namespaces, IPs and UIDs replaced by
  synthetic values. Never commit objects from a real cluster unscrubbed.
- **Clusters** are created by the tests (`kind`, envtest), never borrowed from a developer's
  kubeconfig. Tests never read `~/.kube`: the default folder and the files `$KUBECONFIG` names are
  injected (`engine.WithDefaultFolder`, `engine.WithSessionFiles`), and `kind` writes its kubeconfig
  to a file of its own. `make`'s `TEST_ENV` sets `KUBECONFIG=/dev/null` and removes the in-cluster
  variables CI's runner pods carry.
- **Signing material:** the real `.sha256`, `.sig`, `.cert` and Rekor answers of two kubectl
  releases in [`kubectl/testdata`](../../internal/kubectl/testdata/), verified offline against the
  embedded trusted root; everything else is signed by a sigstore of the tests' own
  ([`testkit/sigstoretest`](../../internal/testkit/sigstoretest/sigstoretest.go)).
- **API request counting:** tests wrap the client's transport in a counting round-tripper and
  assert the requests a scenario may cost.
- **The real engine under the window:** the window's tests may not import the cache or the cluster
  layer (depguard), so a test that runs the real engine under the shell takes its connector from
  [`testkit/enginekit`](../../internal/testkit/enginekit/enginekit.go) (`MemoryConnector`).
- **Credential plugins:** [`testkit.Plugin`](../../internal/testkit/plugin.go) is the test binary
  itself: client-go starts it as the program a kubeconfig's exec section names, and
  `testkit.Main`, which the package's `TestMain` calls, runs it as the plugin when the section's
  environment sets `COGPIT_TEST_PLUGIN` to the plugin's folder. The files there set what a run
  does: hand out a credential, print and exit with a code, wait until ended (a browser login),
  replace itself with `/bin/sleep` (macOS, Linux), leave a child that holds its output (H-118),
  answer late; every run notes itself and its process number. The program carries the plugin's
  name: a symbolic link on macOS and Linux; on Windows, whose process search goes by the program's
  file name, a copy, one per name and test process (or of its own where a test places it), started
  once when it is made so that the malware scan of a new program falls on no test's timing. It
  starts in about 0.3 s under the race detector (the packages' `init`s), so a test that waits for
  a run's bound gives it 3 s. A run notes its stray child before it exits, and the binary may take
  long to exit on a loaded runner: a test that needs a run whose process Cogpit cannot find cancels
  once that process ended (`strayed` in
  [`loginrun_test.go`](../../internal/cluster/loginrun_test.go), `ProcessGone` in the engine's
  `loginwait_test.go`), and one that needs a request queued behind a run waits until the request
  is at client-go's lock (`queuedAtPluginLock`, read from a goroutine dump). On Windows one copy
  serves every test of a name, so a test reads its plugin's hold through its connection's watch,
  never by the program's path.
  [`testkit.LoginPlugin`](../../internal/testkit/loginplugin.go) adds a kubeconfig that runs it.
  Shell scripts stay where a test needs one: a `#!` script's command line and a wrapper that execs,
  for the process search (`pluginproc_unix_test.go`). The plugin a binary swap replaces
  (`TestExecApprovalRefusedAfterPluginBinaryChanged`) is small, so that it is bound by its content
  (`MaxBoundProgramSize`): a script, on Windows a copy of the system's `whoami.exe`. The test
  binary is the plugin (the lead's decision, open to revision) rather than a script per system, as
  a `.cmd` runs as `cmd.exe`, which the search does not take for the plugin, or a helper program
  the tests build, which needs a Go toolchain and a build per package.

## Prerequisites

A test takes what it needs of the machine (a program, a window server, the Sparkle.framework or
WinSparkle.dll a make target fetches) through
[`testkit/prereq`](../../internal/testkit/prereq/prereq.go): `prereq.LookPath(t, "zsh")` for a
program, `prereq.Need(t, ok, what)` for anything else. Without it the test skips on a developer's
machine and fails where `CI` is set, since CI runs `go test` without `-v` and a skip there would
read as a pass. The CI job that runs the test provides it ([releasing › CI](releasing.md#ci)).
What no job can promise stays a plain `t.Skip` with its reason beside it: PowerShell off Windows,
an IPv6 loopback in the Linux runners' pods.

## Asynchronous results

CI runs on loaded machines (self-hosted runner pods sharing one machine, GitHub's small macOS and
Windows runners) and under the race detector, which slows a test five to ten times. A test that
depends on timing passes locally and fails there. The rules:

- **Wait for the state you assert, never for time.** Poll the asserted condition with a deadline
  (`testkit.WaitFor`, `waitTable`, a UI test's `q.until`); the wait's predicate holds every check
  that follows it. A sleep is neither a wait nor a proof that something did not happen.
- **A timer the code arms runs on a test clock:** in a `testing/synctest` bubble, whose clock moves
  only when every goroutine in it waits (`TestNotifyWhileATimerIsPending`, the cache's stall tests,
  `TestChurnKeepsItsPace`), or through the clock the code takes (`Options.Now`, the shell's
  `sh.now`, `fixedNow`), or ended by the test (`fireTimer`). A UI test never races a real tooltip,
  arming or copy-confirmation timer.
- **Wall-clock budgets are not asserted under the race detector or in CI's loaded jobs.** The
  no-race job and `make scorecard` measure them; elsewhere the test checks the structural part
  (work per event, requests per scenario).
- **Open a counting window on a settled system and close it with a sentinel.** On a `MemorySource`
  wait for `testkit.Settled(cache, src)` (every informer synced and its watch open) before reading
  counters, and again before comparing; on the fake API server wait for `OpenWatches`. Count events
  up to a trailing sentinel change the test waits to see. Allow client-go's legitimate retries by
  asserting the end state, not a retry count. An extra request after a settled baseline is a
  finding.
- **A recorded call is not its effect.** The UI fake records a dispatch as it begins: wait for the
  state change it causes. Dispatch counts use `mark` and `callsSince`, which leave out the window's
  own reports (the navigator's `ui.setNavigatorRow`, the palette's), and compare the specific state
  meant, never the engine's global revision read once.
- **A time the window shows comes from the shell's clock** (`sh.now`), and a test's engine runs on
  the tests' clock (`newTestEngine` starts at `fixedNow`), so renders do not change with the day.
- **Pass dependencies before connect.** A test never assigns a field of a live `Backend` that the
  engine's goroutines read (`Access`, connectors): it gives them to the connector, or uses a locked
  setter.
- **`select` picks at random among ready cases.** Code that honours a cancelled context checks
  `ctx.Err()` itself; a test forces the interleaving (`TestDialPortForwardCancelledWhileDialling`).
- **Wait for what the test started.** Processes, shells and goroutines end before the test returns
  (register the wait with `t.Cleanup` after `t.TempDir`, so it runs first); a simulated second
  process stays alive with `runtime.KeepAlive`.
- **The machine's environment is not the test's.** A test that needs a shell or a namespace names
  it; a test never asserts that a freed port stays free. A test never reads or changes the user's
  clipboard: the concealed-copy harness hands `clipboard_darwin.m` a pasteboard of its own in the
  general pasteboard's place (`TestConcealedClipboardMarkedForThisMacOnly`). Saving and restoring
  the user's clipboard instead would read other apps' contents, which macOS can ask the user about
  or deny (`NSPasteboard.accessBehavior`), lose what it could not read, and reach clipboard managers
  as a new copy.
- **Reproduce before fixing.** Run the test many times under load (`go test -race -count=50 -cpu
  1,2` while other packages run), or widen the suspected window on purpose (a delay in a fake's List,
  Watch or handler). A race-job failure is a product race until a forced interleaving shows
  otherwise; a product race gets a regression test that forces it (a gated source, a held mutex).

## On Windows

- **LF checkouts:** [`.gitattributes`](../../.gitattributes) checks every text file out with LF.
- **No Unix modes taken for granted:** on Windows Cogpit neither sets nor checks 0600 and 0700 (its
  files live under `%APPDATA%\Cogpit` and `%LOCALAPPDATA%\Cogpit`, protected by their inherited
  ACLs); a test that checks a mode, an executable bit or a symlink skips that check there; fixture
  paths come from `filepath.Join` and `t.TempDir()`, and file names are valid on Windows.
- **A temporary home is three variables:** `os.UserConfigDir` and `os.UserCacheDir` read `APPDATA`
  and `LOCALAPPDATA`, not `HOME`; a test that gives Cogpit a home sets all three.
- **Fyne's dialog labels follow the system's language:** a test finds a button by Fyne's text
  through `lang.L`, never by the English word.
- **The clock moves in steps** of 0.5 to 16 ms: two `time.Now()` a moment apart can be equal and a
  measured duration can be 0. Assert durations `>= 0`, never use 0 as a marker value, never rely on
  a later event having a strictly later time.
- **A file held open cannot be renamed over or removed, and one held unshared cannot be read:**
  Cogpit retries such a rename or removal, and a read of Helm's `repositories.yaml`, for up to a
  second (`retryHeld` in [`privatefile.go`](../../internal/engine/privatefile.go);
  `testkit.HelmRepoFile` retries its rename the same way), a terminal's files for up to two seconds
  after its shell ended; a test that reads a file the product rewrites retries its read too.
- **A program is named without `.exe`:** os/exec adds it as `PATHEXT` says, so a kubeconfig names
  `testkit.Plugin.Command` and a test that compares the file found compares `Path`.
- **Expectations that differ per platform branch on `runtime.GOOS`** (shortcut glyphs, keys).

Not run on Windows: the race detector, the kind, drain and envtest tiers, the macOS harnesses, and
the process search's tests of what Windows lacks (command lines, process groups, exec). Lint and
GoSec analyse the Windows build on Linux, without cgo, so `internal/ui/fyneui`'s Windows file is
vetted (`go vet`) but not linted ([releasing › Lint and GoSec per system](releasing.md#lint-and-gosec-per-system)).

## Performance budgets

Targets to validate against Lens Desktop on the same machine before they are enforced. "Measured"
holds Cogpit's numbers (Apple M1 Ultra; the cluster tier on kind and a real cluster), without a Lens
comparison yet. `make scorecard` ([`hack/scorecard`](../../hack/scorecard/main.go),
[D-15](decisions.md#d-15-progress-generators)) runs the budget tests and benchmarks without the race
detector and writes [`docs/parity/scorecard.md`](../parity/scorecard.md): per budget the target, the
values, pass or fail, the date and the machine. A budget test records each measurement with
`scorecard.Record` ([`testkit/scorecard`](../../internal/testkit/scorecard/scorecard.go)), which
writes a JSON line when `COGPIT_SCORECARD` names a directory. P3, P6 and P7 need a real window or
Lens beside Cogpit and are measured by hand. Not in CI: run `make scorecard` on a quiet machine and
commit the page.

| Id | Scenario | Proposed target | Measured |
|---|---|---|---|
| P1 | Open a context to the first rows of the Pods list, 1,000-pod cluster | ≤ 1 s | Cogpit's share: all 1,000 rows 111–141 ms after `Connect`, 10,000 pods all rows after ~400 ms, the user's `SelfSubjectReview` included (`TestBudgetFirstRows`, real client-go stack against the fake API server) |
| P2 | Sort a 10,000-pod list by a column | ≤ 50 ms | 10.2 ms sort, 11.6 ms command to new snapshot (`BenchmarkSortChange10k`, `BenchmarkSortCommand10k`) |
| P3 | Scroll a 10,000-pod list | every frame ≤ 16 ms | real window: after the first fill at most 3 stalls of 16–25 ms in 9 s of scrolling; the first fill stalls 151–193 ms once per window |
| P4 | Apply a burst of 1,000 watch events per second to an open list | no frame over 16 ms | a 10,000-row snapshot in 2.1–4.6 ms (`BenchmarkSnapshot10k`); view switches under the churn at most 42 ms, p99 6.3–7.4 ms |
| P5 | Follow a log stream at 10,000 lines per second | no frame over 16 ms; memory bounded by the scrollback limit | offscreen 0.15 ms median, 2.3 ms max of work per 100 ms batch at ~9,800 lines/s; 1.5 MiB for 10,000 kept lines (`TestLogsP5`) |
| P6 | Resident memory with three clusters open on their Pods lists | track only until measured against Lens | the demo cluster with 10,000 pods keeps ~98 MB of its own heap; Cogpit's not measured |
| P7 | Cold start to an interactive window | ≤ 1 s | 0.44–0.58 s from the shell's start to the demo cluster's first rows, 0.42–0.56 s of it the window's GL set-up |
| P8 | Type into a 5 MB YAML document in the editor, highlighting on | keystroke to repaint ≤ 16 ms | real window: widget work p50 0.09 ms, max 2.1 ms (`BenchmarkKeystroke5MB`, `BenchmarkKeystrokeLayout5MB`) |
| P9 | A view stays open on an idle cluster for 10 minutes | 0 list requests; only the view's watches and the connection's heartbeat, one `get` of `/version` every 15 s (D-58) | 0 lists idle on the fake API server and on kind; on a real cluster 10 minutes cost 0 lists and the watches client-go renews (`TestBudgetIdleViewAndWarmReopen`, `TestKindIdleViewsCostNoLists`, `TestRealDiscoveryAndBudgets`) |
| P10 | Reopen a view within the warm period | 0 list requests; rows shown ≤ 100 ms | 0 requests, rows in ~90 µs in the engine; 48–51 ms to rows on screen in the real window; a real cluster's 222 pods after 5–6 ms |
| P11 | An agent asks `ui_state`, `resources_find` and `ui_navigate` for an object in a warm cache | 0 API requests | 0 lists and 0 watches (`TestShowcaseServiceByClusterIP`); on a real cluster one rules review for a namespace not yet reviewed, the access reviews' own cost ([D-62](decisions.md#d-62-checks-against-a-real-cluster)) |
| P12 | Metrics: a Pods list is open on a cluster with a metrics-server; a pod's drawer is open; nothing is open | Pods list: 1 `list pods.metrics.k8s.io` per 60 s; drawer: 1 `get pods.metrics.k8s.io` per 15 s; discovery once per 10 minutes; nothing open: 0 requests (one detection while a cluster's integrations are asked about or shown in its settings, D-76, D-87); never a watch | as the target, through the counting transport (`TestHubOverTheCountingTransport`, `TestDetectUnknownOnce`, `TestIntegrationsShownDetectOnce`) |
| P13 | Metrics with a Prometheus behind the API server's service proxy and a Metrics Server: a Pods list is open; a pod's drawer is open (charts of the default hour) showing CPU or Memory; a chart shows the network or the filesystem; a chart's range changes; the Metrics Server fails; nothing is open | Pods list: 0 Prometheus requests while the Metrics Server answers; detection only when a drawer opens or the Metrics Server fails; drawer: 2 range queries when it opens, then none while only CPU or Memory is shown; network: 2, then 2 per step; filesystem: 1; a range change: the shown charts' queries once; nothing open: 0 | as the target (`TestPrometheusBudget`, `TestPrometheusRangeFollowsTheChartsWindows`) |
| P14 | The cluster Overview is open on a cluster with a Metrics Server and a Prometheus; its range changes; it closes | open: 1 Prometheus probe (when not detected yet) and one history of range queries, then 1 `list nodes.metrics.k8s.io` per 15 s and 0 Prometheus requests while the Metrics Server answers; a range change: the history's queries once; closed: 0 | as the target (`TestClusterBudget`, `TestClusterPodsHistoryBudget`) |
| P15 | A user who may list pods only in two namespaces, named as the cluster's accessible namespaces, opens the Pods view; a cluster-admin opens it | the restricted user: one list and one watch per namespace (one streaming watch each with streaming switched on, D-135), no cluster-wide read once the access reviews said no, nothing while idle; the cluster-admin: one cluster-wide list and watch, no review beyond the one rules review of connecting | as the target (`TestBudgetAccessibleNamespaces`) |
| P16 | An agent reads an object or a log for the first time in a session, then again (H-82) | the first read: the Secrets' one list and one watch (a namespace-limited user's per namespace), nothing else; again within the warm period: 0 requests; the window's reads: no Secrets request | as the target (`TestBudgetAgentReadTakesTheSecrets`) |
| P17 | The Alertmanager integration (D-77) is on; the cluster is shown; it is connected in the background or the integration is off; the Alertmanager fails | shown: 1 `get services/proxy` (`GET /api/v2/alerts`) per 30 s, detection's probes at most 3 once per connection and address; background or off: 0 requests; failing: backoff from 30 s to 5 minutes; never a write | as the target (`TestPollerThroughTheServiceProxy`, `TestAlertmanagerOnlyTheShownCluster`, `TestAlertmanagerOff`) |

## Before a pull request

Locally, check only what the change touches; CI is the full gate
([`AGENTS.md`](../../AGENTS.md#5-before-you-finish-a-task)):

```sh
GOFLAGS=-p=4 go build ./...
GOFLAGS=-p=4 go vet ./internal/<pkg>/...    # the packages you touched
GOFLAGS=-p=4 go test ./internal/<pkg>/...   # their tests, without -race
make test-ui                                 # internal/ui/fyneui's tests in parallel shards (about two minutes)
go test -race ./internal/<pkg>/...          # only when the change is about concurrency
make test-darwin                             # macOS: the platform tests CI's macOS job runs
make test-kind                               # the cluster tier before CI; needs make kind-up
make test-kind-drain                         # cordon and drain; needs make kind-drain-up
make test-envtest                            # the envtest tier; fetches its pinned binaries first
make scorecard                               # only when a change moves a budget; then commit docs/parity/scorecard.md
make test-windows-unit test-windows-ui       # on Windows (MSYS2 MINGW64): CI's two Windows test jobs
```
