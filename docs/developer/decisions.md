# Decisions

Why Cogpit is built as it is: one record per decision, `## D-<n>: <title>` in id order, with the
decision as it holds now, its reason, the options rejected where they explain the choice, and the
code and tests that carry it. **Status** is `built`, `partly built`, `decided, not built`, `open`
or `withdrawn`. **By** is who decided the record's core: `owner` (direction, scope, security
posture, anything irreversible or outward-facing) or `lead (open to revision)` (the details below
that). The lead's details within an owner's record are open to revision as well; they stand until
use or feedback changes them ([D-74](#d-74-how-the-leads-decisions-are-reviewed),
[conventions](conventions.md#decisions)). Git holds each record's history.

| Id | Decision | Status |
|---|---|---|
| [D-1](#d-1-ui-toolkit) | UI toolkit | built: Fyne; S5 terminal re-check open on Linux, Windows |
| [D-2](#d-2-agent-interface-scope) | Agent interface scope | built: full MCP control; agents' changes wait for the user |
| [D-3](#d-3-helm-and-kubectl-binaries) | Helm and kubectl binaries | built: Helm in-process; kubectl from PATH or verified download |
| [D-4](#d-4-packaging-and-updates) | Packaging and updates | partly built: Sparkle, signed disk image; no universal bundle or cask |
| [D-5](#d-5-extensions) | Extensions | built: integrations in Cogpit's code, no external extensions |
| [D-6](#d-6-kubernetes-libraries) | Kubernetes libraries | built: client-go, kubectl's drain, metrics, Helm v4 SDK |
| [D-7](#d-7-language-and-platforms) | Language and platforms | built: Go; macOS first, Linux and Windows ship |
| [D-8](#d-8-licence) | Licence | built: Apache-2.0 |
| [D-9](#d-9-scope-against-lenss-paid-and-vendor-features) | Scope against Lens's paid and vendor features | partly built: GitOps views in; Security Center, cost monitoring later |
| [D-10](#d-10-built-in-agent) | Built-in agent | built: chat column, OpenAI-compatible endpoints, private connections |
| [D-11](#d-11-crash-stance) | Crash stance | built: operation journals, editor drafts, local crash reports |
| [D-12](#d-12-parity-sources) | Parity sources | built: current Lens Desktop only, clean-room |
| [D-13](#d-13-cache) | Cache | built: watched, in memory only, Secrets watched in full |
| [D-14](#d-14-terminal-widget) | Terminal widget | built: patched fyne-io/terminal copy, fuzzed, offered upstream |
| [D-15](#d-15-progress-generators) | Progress generators | built: make parity in CI; scorecard by hand |
| [D-16](#d-16-releases-and-what-they-carry) | Releases and what they carry | built: semantic-release on green main; complete product, CLI inside |
| [D-17](#d-17-token-files-outside-the-kubeconfigs-folder) | Token files outside the kubeconfig's folder | built: asked for in the plugin approval dialog |
| [D-18](#d-18-secret-annotations-seen-by-agents) | Secret annotations seen by agents | built: shown to agents except kubectl's last-applied; risk accepted |
| [D-19](#d-19-editing-and-creating-secrets-from-yaml) | Editing and creating Secrets from YAML | built: GUI only |
| [D-20](#d-20-a-port-forward-that-cannot-connect) | A port forward that cannot connect | built: 404/403/401 fail at once, others retried |
| [D-21](#d-21-pasting-a-kubeconfig) | Pasting a kubeconfig | built: no pasted kubeconfigs |
| [D-22](#d-22-colour-theme) | Colour theme | built: Solarized terminal colours, theme preference, cluster colours |
| [D-23](#d-23-the-status-bars-owner-link-and-mcp-switch) | The status bar's owner link and MCP switch | built: GUI-only link and MCP server switch |
| [D-24](#d-24-logo) | Logo | built: glowing C helm, every version kept drawable |
| [D-25](#d-25-copying-values) | Copying values | built: copy icons in drawer, lists, settings; Secret clipboard rules |
| [D-26](#d-26-binding-a-credential-plugins-approval-to-its-program) | Binding a credential plugin's approval to its program | built: content in the folder, command elsewhere, Cogpit's proxy |
| [D-27](#d-27-an-agents-helm-values-in-the-confirmation) | An agent's Helm values in the confirmation | built: shown in the dialog, never serialised |
| [D-28](#d-28-the-default-kubeconfig-as-sources-as-lens-has-it) | The default kubeconfig as sources, as Lens has it | built: every file a source, one dialog per file and plugin |
| [D-29](#d-29-helm-chart-repositories-from-helms-repositoriesyaml) | Helm chart repositories from Helm's repositories.yaml | built: Helm's file read-only, http credentials allowed once |
| [D-30](#d-30-certificates-in-secrets-decoded) | Certificates in Secrets, decoded | built: decoded fields, key match, chain order and anchor |
| [D-31](#d-31-every-timestamp-in-the-local-time-zone-and-format) | Every timestamp in the local time zone and format | built: OS region format; no format setting |
| [D-32](#d-32-every-byte-amount-in-binary-units-with-one-decimal) | Every byte amount in binary units with one decimal | built: one formatter, model/bytes.go |
| [D-33](#d-33-the-flux-actions) | The Flux actions | built: flux CLI patches, Reconcile with Source as a job |
| [D-34](#d-34-the-flux-dashboard) | The Flux dashboard | built: state tiles, Not Ready table, status filter in every view |
| [D-35](#d-35-what-argo-cds-repository-and-cluster-secrets-show) | What Argo CD's repository and cluster Secrets show | built: allowlist of configuration keys |
| [D-36](#d-36-how-argo-cds-sync-writes-the-operation) | How Argo CD's Sync writes the operation | built: operation via Kubernetes API, sync windows checked |
| [D-37](#d-37-the-node-shell) | The node shell | built: Lens's privileged pod with nsenter, pinned Alpine, 2 h cap |
| [D-38](#d-38-who-sets-the-node-shells-image) | Who sets the node shell's image | built: the user only; agents and the CLI may reset |
| [D-39](#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only) | Pod metrics for a user who may list pods in some namespaces only | built: one list per namespace read; a refusal stays in its namespace |
| [D-40](#d-40-a-pods-table-in-the-workloads-drawers) | A Pods table in the workloads' drawers | built: node drawer's table without Namespace, with Age, six kinds |
| [D-41](#d-41-what-survives-a-restart) | What survives a restart | built: workspace.json; tabs and connections only with Restore the last session |
| [D-42](#d-42-the-start-screen) | The start screen | built: welcome page, no picker; last session per preference |
| [D-43](#d-43-where-the-nodes-disk-comes-from) | Where the nodes' disk comes from | built: node-exporter through Prometheus, never nodes/proxy |
| [D-44](#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs) | What agents read of the dock's logs and terminal tabs | built: logs freely, terminals by the user's switch; Secret values replaced |
| [D-46](#d-46-chart-icons-from-the-hosts-that-serve-them) | Chart icons from the hosts that serve them | built: fetched when shown, https only, bounded; switch on by default |
| [D-47](#d-47-deep-links-cogpit) | Deep links (`cogpit://`) | built (macOS only): navigation only, asks before every connect |
| [D-48](#d-48-start-at-login) | Start at login | built (macOS only): the app's login item, off, user turns on |
| [D-49](#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp) | The external MCP server's switch, honoured by `cogpit-cli mcp` | built: opt-in; off refuses `cogpit-cli mcp`, headless too |
| [D-50](#d-50-kubectls-signature-check) | kubectl's signature check | built: sigstore-go with Rekor's log proof, no fallback |
| [D-51](#d-51-the-navigators-and-the-command-palettes-state) | The navigator's and the command palette's state | built: navigator and palette text are engine state |
| [D-52](#d-52-several-windows) | Several windows | open: one session per app chosen, rest open; not built |
| [D-53](#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it) | The MCP details, and a token kept until the user regenerates it | built: token and port kept until Regenerate token |
| [D-54](#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle) | The top bar: layout toggles, the Update button, the theme toggle | built: layout toggles, Update button, theme toggle; no cluster name |
| [D-55](#d-55-a-config-maps-values-edited-in-the-drawer) | A Config Map's values edited in the drawer | built: values and keys, dry-run review, rebase, drafts |
| [D-56](#d-56-the-terminals-scrollback) | The terminal's scrollback | built: no line limit, 256 MiB per terminal |
| [D-57](#d-57-choosing-a-cluster) | Choosing a cluster | built: the navigator's section is the one cluster list |
| [D-58](#d-58-standby-and-lost-connections) | Standby and lost connections | built: detect, show and reconnect lost connections, writes disabled meanwhile |
| [D-59](#d-59-a-secrets-values-edited-in-the-drawer) | A Secret's values edited in the drawer | built: GUI-only, masked diff, keys added and removed |
| [D-61](#d-61-cogpit-metrics-the-bundled-metrics-stack) | Cogpit Metrics, the bundled metrics stack | withdrawn |
| [D-62](#d-62-checks-against-a-real-cluster) | Checks against a real cluster | built: opt-in TestReal* tests, never in CI |
| [D-63](#d-63-long-commands-as-background-jobs) | Long commands as background jobs | built: selections above ten objects, Reconcile with Source, drains |
| [D-64](#d-64-envtest-a-real-api-server-in-ci-without-docker) | envtest, a real API server in CI without Docker | built: kube-apiserver 1.37.0, frozen with SIGSTOP |
| [D-65](#d-65-discovery-cached-on-disk) | Discovery cached on disk | built: per server and identity, checked by version and definitions |
| [D-66](#d-66-the-connection-log) | The connection log | built: every attempt kept on disk, 7 days, 1 MiB per context |
| [D-67](#d-67-the-demo-cluster-stays-out-of-releases) | The demo cluster stays out of releases | built: build tag nodemo in every release target |
| [D-68](#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as) | One short name for a cluster, and the user Cogpit is logged in as | built: short name everywhere, user confirmed by SelfSubjectReview |
| [D-69](#d-69-several-clusters-connected-at-once) | Several clusters connected at once | built: a pool of connections, one cluster shown per window |
| [D-70](#d-70-the-disk-images-window) | The disk image's window | built: dmgbuild template, signed app added in the signing job |
| [D-71](#d-71-thanos-and-victoriametrics-read-as-prometheus) | Thanos and VictoriaMetrics, read as Prometheus | built: found by Service name, ranked after Prometheus |
| [D-72](#d-72-native-file-panels-and-macos-folder-access) | Native file panels and macOS folder access | partly built: macOS panels and bookmarks; Windows, Linux use Fyne's dialogs |
| [D-73](#d-73-a-login-that-waits-cancel-connecting-and-a-bound) | A login that waits: Cancel Connecting and a bound | built: Cancel Connecting ends the plugin; every run bounded at 5 minutes |
| [D-74](#d-74-how-the-leads-decisions-are-reviewed) | How the lead's decisions are reviewed | built: lead decides details and records them, open to revision |
| [D-75](#d-75-a-client-certificates-validity-in-the-cluster-lists) | A client certificate's validity in the cluster lists | built: time left or expired shown; never blocks a connect |
| [D-76](#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect) | Integrations per cluster, and the question at a new cluster's first connect | built: five per-cluster switches; only the user switches them |
| [D-77](#d-77-alertmanager-the-alerts-pill-and-the-alert-board) | Alertmanager, the alerts pill and the alert board | built: shown cluster polled every 30 s, read only |
| [D-78](#d-78-trivy-operators-image-ratings) | Trivy Operator's image ratings | built: reads and placement; the grade itself is D-113 |
| [D-79](#d-79-kyvernos-policy-violations-on-the-objects) | Kyverno's policy violations on the objects | built: warning column and drawer marks from policy reports |
| [D-80](#d-80-a-clusters-accent-colour) | A cluster's accent colour | built: accent colour per cluster, set only in the window |
| [D-81](#d-81-reconnect-and-what-log-in-again-does) | Reconnect, and what Log in again does | built: Reconnect from the kubeconfig file, tabs kept |
| [D-82](#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again) | A pre-choice before any integration is looked for, and Ask again | built: nothing looked for before the user's choice; Ask again |
| [D-83](#d-83-one-hotbar-and-a-drag-that-carries-the-tile) | One hotbar, and a drag that carries the tile | built: one hotbar without limit, tiles carried by drag |
| [D-84](#d-84-undo-and-redo-in-the-yaml-editor) | Undo and redo in the YAML editor | built: undo and redo history in the YAML editor |
| [D-85](#d-85-dialogs-as-tables-and-an-objects-state-without-its-resourceversion) | Dialogs as tables and an object's state without its resourceVersion | built: dialogs as rows, no resourceVersion in window texts |
| [D-86](#d-86-the-order-of-an-objects-yaml) | The order of an object's YAML | built: apiVersion, kind, metadata first, status last |
| [D-87](#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer) | Cluster Settings: the integrations under a divider, a section each, Metrics their consumer | built: integrations under a divider, one section each |
| [D-88](#d-88-the-metrics-source-row) | The metrics' source row | built: one row, a dot and an age per provider |
| [D-89](#d-89-how-a-charts-line-is-drawn) | How a chart's line is drawn | built: one rasterised path per run of points |
| [D-90](#d-90-a-nodes-disks-a-usage-bar-per-real-filesystem) | A node's disks: a usage bar per real filesystem | built: a bar per real filesystem; Nodes list shows the fullest |
| [D-91](#d-91-the-kubernetes-version-and-upstream-end-of-life) | The Kubernetes version and upstream end of life | built: release table fetched at build time, estimate, ⚠ marks |
| [D-92](#d-92-a-gear-in-the-navigators-cluster-row) | A gear in the navigator's cluster row | built |
| [D-93](#d-93-room-in-the-data-footer-and-the-layout-bar-and-a-full-row-back-button) | Room in the data footer and the layout bar, and a full-row back button | built |
| [D-94](#d-94-the-empty-docks-toggle-opens-a-terminal-and-alert-names-copy) | The empty dock's toggle opens a terminal, and alert names copy | built |
| [D-95](#d-95-the-window-comes-back-where-it-was) | The window comes back where it was | built: macOS frame and display; size elsewhere |
| [D-96](#d-96-the-service-proxys-port-spelled-by-its-number-first) | The service proxy's port spelled by its number first | built: number first, then name |
| [D-97](#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays) | A connect goes on when another cluster is shown; a failed one stays | built: connects run on in the background; a failure stays shown |
| [D-98](#d-98-what-holds-a-terminating-namespace-and-remove-finalizers) | What holds a Terminating namespace, and Remove finalizers | built: Blocking Deletion list and guarded Remove Finalizers |
| [D-99](#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts) | Selecting and copying a log tab's text and a toast's | built: log, toast and chat text selectable; Follow by scrolling |
| [D-100](#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save) | Objects and effects in one form, and a drawer's edited state beside Save | built: Kind → name in dialogs, effects as rows, edit pills |
| [D-101](#d-101-the-kubeconfig-sources-at-hand-and-a-removed-cluster-leaves-the-hotbar) | The kubeconfig sources at hand, and a removed cluster leaves the hotbar | built: sources' "−"; a removed source's clusters leave the hotbar |
| [D-102](#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach) | Debug containers added from the drawer, shown in blue, reached by Logs, Shell and Attach | built: pod.debug, Destructive, netshoot pinned by digest |
| [D-103](#d-103-fynes-thread-check-a-build-parameter-off-in-releases) | Fyne's thread check, a build parameter, off in releases | built: FYNE_THREAD_CHECKS, off in releases, on in development builds |
| [D-104](#d-104-dialog-fields-hints-dropdowns-and-button-outlines) | Dialog fields, hints, dropdowns and button outlines | built: hint tooltips, sliders, dropdowns over the field, outlines |
| [D-105](#d-105-reverse-video-as-a-state-and-a-keys-modifiers-from-the-key-itself) | Reverse video as a state, and a key's modifiers from the key itself | built: fork patches for SGR 7, key modifiers, xterm sequences |
| [D-106](#d-106-a-click-below-the-rows-f-in-editor-tabs-moving-tabs-a-deleted-namespace) | A click below the rows, ⌘F in editor tabs, moving tabs, a deleted namespace | built: drawer close, editor-tab ⌘F, tab moves, namespace marks |
| [D-107](#d-107-the-navigator-slides-and-reduce-motion) | The navigator slides, and Reduce motion | built: navigator slides; Reduce motion preference and macOS setting |
| [D-108](#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages) | Cogpit for Linux: a .deb and an APT repository on Pages | built: .deb, signed APT repository on Pages, apt-based updater |
| [D-109](#d-109-the-menus-in-the-top-bar-on-windows-and-linux) | The menus in the top bar on Windows and Linux | built: menu titles in Cogpit's top bar, native title bar |
| [D-110](#d-110-dark-cockpit-cogpits-design-language) | Dark Cockpit, Cogpit's design language | built: Dark Cockpit replaces Solarized; first outlook topic open |
| [D-111](#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle) | Cogpit for Windows: an NSIS setup and WinSparkle | built: per-user NSIS setup, WinSparkle updates, x64 |
| [D-112](#d-112-ci-and-test-time) | CI and test time | built: main-only caches, UI tests in shards balanced by measured time, lint and GoSec per system, the shipped app's smoke test; one item open |
| [D-113](#d-113-image-grades-shields-from-trivys-reports) | Image grades, shields from Trivy's reports | built: S–E and N/A shields; D/E thresholds provisional |
| [D-114](#d-114-a-failed-connects-error-page-and-its-stages) | A failed connect's error page and its stages | built: error page with the connect's stages |
| [D-115](#d-115-a-dock-tab-the-user-opens-takes-the-keys) | A dock tab the user opens takes the keys | built: user-opened dock tabs take the keyboard |
| [D-116](#d-116-escape-closes-the-innermost-thing) | Escape closes the innermost thing | built: one Escape chain, innermost first |
| [D-117](#d-117-one-preview-tab-per-cluster) | One preview tab per cluster | built: a preview tab per cluster; tabs keep their place |
| [D-118](#d-118-settings-as-a-card-over-the-window) | Settings as a card over the window | built: modal card overlay, window keys held |
| [D-119](#d-119-the-cluster-icon-designer) | The cluster icon designer | built: Internal letter tile or Custom cut image |
| [D-120](#d-120-folders-searched-for-credential-plugins) | Folders searched for credential plugins | built: built-in and user program folders appended after PATH |
| [D-121](#d-121-texts-drawn-again-when-the-windows-pixel-scale-changes) | Texts drawn again when the window's pixel scale changes | built: theme re-applied on a macOS density change |
| [D-122](#d-122-the-light-strip) | The light strip | built: accent colour strip, static or three animations |
| [D-123](#d-123-a-drag-starts-where-the-button-went-down) | A drag starts where the button went down | built: dragGuard on every Cogpit draggable; dock edge Cogpit's own |
| [D-124](#d-124-checked-rows-the-drawers-row-and-the-keyboards-cell) | Checked rows, the drawer's row and the keyboard's cell | built: fill for checked, bar for the drawer's row |
| [D-125](#d-125-an-answers-speed-ttft-and-otps) | An answer's speed: TTFT and OTPS | built: TTFT and OTPS under each chat answer |
| [D-126](#d-126-a-text-field-passes-the-wheel-on) | A text field passes the wheel on | built: wheelPass over every Cogpit text field |
| [D-127](#d-127-find-in-the-drawers-yaml) | Find in the drawer's YAML | built: permanent find bar, ⌘F from drawer or table |
| [D-128](#d-128-a-releases-history-in-its-drawer) | A release's history in its drawer | built: History and Description in a release's drawer |
| [D-129](#d-129-back-brings-the-place-back) | Back brings the place back | built: back/forward restore view, filters, drawer and scroll |
| [D-130](#d-130-find-in-a-releases-values) | Find in a release's values | built: find bar over a release's values in the drawer |
| [D-131](#d-131-a-releases-revisions) | A release's revisions | built: Revisions dock tab, values and manifests compared |
| [D-132](#d-132-a-cluster-upgraded-while-connected) | A cluster upgraded while connected | built: heartbeat follows version; Reconnect offered on new minor |
| [D-133](#d-133-connection-diagnostics) | Connection Diagnostics | built: one diagnostics tab, in-memory log, anonymised report |
| [D-134](#d-134-the-windows-zoom) | The window's zoom | built: VS Code's zoom via Fyne's canvas scale, 50-210% |
| [D-135](#d-135-a-stalled-stream-lists) | A stalled stream lists | built: lists by default; streaming opt-in beta, 10 s stall bound |

## D-1: UI toolkit

**Status:** built · **By:** owner

The UI is Fyne (`fyne.io/fyne/v2`, BSD-3-Clause), chosen after the spike gate
([spike-fyne.md](spike-fyne.md)), which ran on macOS. Two criteria are re-checked per platform:

| Criterion | Re-check |
|---|---|
| S7, packaging | Met: every CI run packages the macOS app, the Linux `.deb` and the Windows setup and runs the unit and UI tests on Windows ([releasing › CI](releasing.md#ci)). The universal macOS app is not built. |
| S5, terminal | Open (M11): the local shell on ConPTY passes its session tests on Windows (own `KUBECONFIG`, resize, exit); the widget in a real window, copy and paste included, is not re-checked on Windows or Linux (X11, Wayland). |

A re-check that fails without a workaround reopens D-1 for that platform only: its UI moves to a
separate Rust process with egui, and the engine stays in Go behind the serializable engine/UI seam
([architecture §4](architecture.md#4-the-ui-seam-the-most-important-interface)). Accessibility
(S8: the table and the drawer announce nothing to a screen reader) is a known Fyne gap; it does not
gate.

**Why:** the team writes Go; Fyne is stable and has tables with header rows and a terminal widget
on all three platforms.

**Rejected:**

- Gio, Cogent Core — pre-1.0: too low-level (Gio), too young (Cogent Core).
- GTK4 through gotk4 — weak on macOS and Windows.
- Wails — Lens's web stack in a webview, TypeScript as a second language, three browser engines.
- Rust with egui — the team writes no Rust; kept as the fallback UI process.

**Code:** [`internal/ui/fyneui`](../../internal/ui/fyneui/) · 
`TestStartLocalWindowsBindsKubectlToContextAndNamespace`, `TestStartLocalWindowsResize`,
`TestStartLocalWindowsShellExitEndsSession`

## D-2: Agent interface scope

**Status:** built · **By:** owner

Agents control all of Cogpit over MCP: they read and drive the UI state, find objects and navigate
to them (`resources.find`, `ui.navigate`), and run every engine command with the GUI's verbs and
danger levels. The engine enforces the rules for every origin:

| An agent's command | Runs |
|---|---|
| read, UI | without asking |
| mutating | after the user accepts, unless the user allowed agent writes for its context (Cluster Settings › Agents, off by default, H-14) |
| declaring `AgentsAsk` | after the user accepts, whatever that allowance |
| destructive | after the user confirms objects, namespace and context, as for every origin |
| GUI-only | never: refused before anything is asked, read or written |

Every agent command, refused ones included, is audited (`audit.list`). Without the window
(`cogpit-cli`, the stdio bridge without a running app) whatever needs an answer is refused.

- **`AgentsAsk`** ([`agentsask.go`](../../internal/engine/agentsask.go)): `role.create`,
  `clusterRole.create`, `roleBinding.create`, and `object.create` or `object.apply` holding any
  RBAC kind (H-27); `helm.chart.install` and `helm.release.upgrade` (H-31); `cluster.setPrometheus`
  (H-26) and `cluster.setAlertmanager`; setting a proxy (its password shown as `•••`; the CLI
  cannot). UI commands ask rather than becoming mutating: Cogpit's own settings are outside the
  allowance, and their MCP tools keep the read-only hint.
- **GUI-only** (`Spec.GUIOnly`, [`guionly.go`](../../internal/engine/guionly.go)), still listed as
  MCP tools, among them: running a program or reading a local file, adding a credential, showing or
  editing a Secret's value, installing an update, deciding what agents may do or read; a cluster's
  name, icon and accent colour (H-125); every integration switch and the integrations question but
  Later (H-126, H-133); turning update checks off ([D-4](#d-4-packaging-and-updates)); marking
  notifications read or clearing them (H-32; agents read them as text, window-only ones filtered:
  [architecture §4.3](architecture.md#43-ui-state)).
- **Confirmations:** a grant says "immediately, cluster-wide" or "immediately, in namespace <ns>";
  "This cannot be undone." is left out for grants, rollbacks and the node shell (`Spec.Undoable`).
- **The external server** is opt-in, switched only in the status bar, `cogpit-cli mcp` included
  ([D-49](#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)): MCP Streamable HTTP
  on `127.0.0.1`, its bearer token in the owner-only `<user config dir>/Cogpit/mcp-token` until the
  user regenerates it, plus the stdio bridge. Help › Copy MCP command copies the stdio registration
  of the installed `cogpit-cli` ([`bundledcli.go`](../../cmd/cogpit/bundledcli.go)), which follows
  a new token or port by itself, else the HTTP one, which writes the token into Claude Code's
  configuration ([`register.go`](../../internal/mcp/register.go)).

**Why:** full control is Cogpit's difference; Lens Desktop's MCP server is read-only and paid
([Lens MCP server](https://docs.lenshq.io/k8slens/mcp-server/)). The rules sit in the engine so no
origin bypasses them.

**Code:** [`engine.go`](../../internal/engine/engine.go) `NeedsConfirmation` · 
`TestAgentsAskDispatch`, `TestTextHoldsRBAC`, `TestGUIOnlyCommandsRefuseOtherOrigins`

## D-3: Helm and kubectl binaries

**Status:** built · **By:** owner

Helm runs in-process through its Go SDK ([D-6](#d-6-kubernetes-libraries)); no `helm` binary is
bundled, downloaded or required.

A cluster's terminal gets a `kubectl` within one minor version of the API server
([skew policy](https://kubernetes.io/releases/version-skew-policy/#kubectl)):

- Cogpit compares the client version (read once per file) of the Preferences' path to kubectl,
  else of the first `kubectl` on its own `PATH`, with the version the API server reported at
  connect.
- Missing or too far off: a suitable release downloaded earlier goes first without asking; without
  one, the tab offers the server's major.minor.patch (EKS `v1.36.1-eks-…` → 1.36.1) from
  `https://dl.k8s.io/release` or an https mirror into `<user config dir>/Cogpit/kubectl/<version>/`,
  checked against its `.sha256` and its signature with Rekor
  ([D-50](#d-50-kubectls-signature-check)). Terminals opened afterwards use it.
- "First" is a link in the terminal's own directory, first on its `PATH` and put first again after
  the profile ran: zsh through Cogpit's `ZDOTDIR`, bash with `--init-file` (it reads a login
  bash's files but is no login shell: no `~/.bash_logout`), fish with `-C`; other shells keep the
  environment's order (H-36). Enforce kubectl adds a function `kubectl` in these three.
- Windows: a `kubectl.exe`'s own folder goes first on `PATH`, one by another name gets a hard link
  or a copy named `kubectl.exe`; PowerShell and cmd keep the order and get no enforcement.

Preferences › Kubernetes, as Lens's: the download offer (on by default), the mirror, the directory
for binaries, the path to kubectl; plus Enforce kubectl (off by default). Only the user, in the
window, downloads and sets all but the offer (`kubectl.download`, `ErrKubectlGUIOnly`).

**Why:** a working setup stays as the user made it, a cluster outside its range still gets a
verified, matching `kubectl`, and nothing changes outside Cogpit's terminals.

**Rejected:**

- `PATH` only — fails without `kubectl` or across the skew.
- Always downloading, as Lens does — about 60 MB per minor version; overrides deliberate setups.
- Bundling one — 60 MB larger, still skewed for other clusters, every fix forces a release.
- bash `--rcfile` (a login bash ignores it), `PROMPT_COMMAND` (a profile can overwrite it), fish
  `conf.d` (runs before `config.fish`).

**Code:** [`internal/kubectl`](../../internal/kubectl/),
[`engine/kubectl.go`](../../internal/engine/kubectl.go),
[`terminal/kubectl.go`](../../internal/terminal/kubectl.go) · **Tests:** `TestCheckKubectl`,
`TestDownloadVerifiesAndInstalls`, `TestBashKubectlFirstAfterTheProfile`, `TestZshKubectlEnforced`,
`TestStartLocalWindowsKubectlByAnotherName`

## D-4: Packaging and updates

**Status:** partly built · **By:** owner

Cogpit shows when an update is available and installs it with one click, as Lens does: macOS
through Sparkle 2, Linux through apt ([D-108](#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)),
Windows through WinSparkle ([D-111](#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).
AppImage and tar.gz are not offered; winget, Flathub and rpm are open. The macOS download,
`Cogpit-<v>-macos-arm64.dmg`, is signed, notarized and stapled ([D-16](#d-16-releases-and-what-they-carry));
a failed signature or notarization leaves a release without it, never with an unsigned one. Not
built: the universal (Intel) bundle and a Homebrew cask of the disk image that leaves updates to
Sparkle (`auto_updates true`, [G14](../parity/gaps.md#g14-packaging-signing-and-updates-macos)).

- **Sparkle 2** runs from macOS 12 (Cogpit needs 13.0); the [`Makefile`](../../Makefile) pins it by
  version and SHA-256 (Renovate writes both, never merging them itself). It ships without its XPC
  services (no entitlements), verifies the disk image's EdDSA signature before extracting and
  accepts only a signed feed.
- **Checks:** Sparkle's own scheduled checks are off. While Preferences › App › Check for updates
  automatically is on (the default, as in Lens), Cogpit runs Sparkle's probing check at every start
  and every hour: it feeds the Update button ([D-54](#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle))
  and holds nothing, so the button, Help › Check for Updates… and `app.checkForUpdates` (any origin)
  read the feed afresh in Sparkle's window. Agents and the CLI may only turn the checks on. The
  channel is Stable; Beta is refused until Cogpit publishes prereleases.
- **The appcast**, `https://guided-traffic.github.io/cogpit/appcast.xml` on GitHub Pages, is the
  `SUFeedURL` every copy keeps, signed as a whole with the update key in a job that runs only
  Sparkle's `sign_update`; a draft builds only on a served feed the key signed, and the deploy
  refuses to take back a newer release ([releasing › the appcast](releasing.md#the-appcast)).
- **Release rights** and Sparkle's 20-day fallback:
  [H-62](../security/updates.md#h-62-every-push-to-main-that-passes-the-gates-is-released),
  [H-91](../security/updates.md#h-91-after-20-days-of-unverifiable-feeds-sparkle-presents-items-again).
  A PUT on an environment must repeat its `deployment_branch_policy`, or its tag policy is lost.

**Why:** Sparkle handles signatures, atomic install, `/Applications` permissions, quarantine and
relaunch inside a non-Cocoa app, and its own scheduled check holds a found release until the user
acts, checking nothing more. Turned-off checks stop security fixes; every copy keeps the feed URL.

**Rejected:** an updater in Go or `go-selfupdate` — Sparkle's hard cases as own security-critical
code; the feed as a release asset — `releases/latest` omits prereleases; the enclosures' signatures
alone — whoever controls Pages could change notes and links.

**Code:** [`sparkle_darwin.go`](../../internal/update/sparkle_darwin.go) `scheduledChecks`,
[`engine/updates.go`](../../internal/engine/updates.go) · **Tests:** `TestUpdatePreferences`,
`TestSparkleScheduledChecksFindTheNewestRelease`, `TestVerifyKeepsTheServedFeed`

## D-5: Extensions

**Status:** built · **By:** owner

What Lens ships as an extension (a custom resource's view, an integration such as Flux CD or Argo
CD) is part of Cogpit's own code: kind profiles, commands and views. There is no mechanism for
external extensions; adding one needs a new decision. Lens's extension features (install from the
file system, auto-update, the registry, safe mode) are not targets: Lens Desktop's extensions are
npm packages, installing one needs a paid plan and running one a JavaScript runtime Cogpit does not
have ([Lens extensions](https://docs.lenshq.io/k8slens/extensions/)).

**Why:** every integration is reviewed and released with Cogpit.

**Rejected:** kind profiles as data files — a format to maintain; WebAssembly plug-ins with
wazero — a large API and review effort; external programs as plug-ins — no sandbox;
Lens-compatible JavaScript extensions — impossible without a JavaScript runtime.

## D-6: Kubernetes libraries

**Status:** built · **By:** owner

Cogpit uses the libraries `kubectl` and `helm` are built from; versions in [`go.mod`](../../go.mod).

| Need | Module | Where |
|---|---|---|
| API access, informers, exec, port-forward | `k8s.io/client-go` | `internal/cluster`, `internal/cache`, `internal/terminal` |
| Drain | `k8s.io/kubectl/pkg/drain` | [`internal/cluster/nodes.go`](../../internal/cluster/nodes.go) |
| Metrics API | `k8s.io/metrics` | [`internal/metrics`](../../internal/metrics/) |
| Helm | `helm.sh/helm/v4` | [`internal/helm`](../../internal/helm/) |

**Why:** credential plugins, drain semantics and Helm release handling behave as in the reference
tools instead of being reimplemented.

**Rejected:** kube-rs (Rust) — not the code `kubectl` and `helm` run, and its typed API lagged
Kubernetes' releases.

## D-7: Language and platforms

**Status:** built · **By:** owner

Cogpit is written in Go. It is a desktop application installed on the user's machine, never a web
UI and never deployed into a cluster ("Helm" in these documents is Lens's feature for managing Helm
releases in the user's clusters). macOS comes first: the alpha and the beta are macOS milestones.
Every release also ships Linux (a .deb for amd64 and arm64,
[D-108](#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) and Windows (an x64 setup,
[D-111](#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); their remaining work is M11
([roadmap](../parity/roadmap.md#milestones)). Platform code sits in per-OS files, and CI runs the
tests on Linux, macOS and Windows ([releasing › CI](releasing.md#ci)).

**Why:** the Kubernetes ecosystem ([D-6](#d-6-kubernetes-libraries)) is Go, and the team writes Go.
Accepted costs: weaker native UI toolkits than Rust ([D-1](#d-1-ui-toolkit)) and fewer crash
guarantees from the compiler ([D-11](#d-11-crash-stance)).

**Code:** [`localpty_windows.go`](../../internal/terminal/localpty_windows.go),
[`internal/ui/nativedialog`](../../internal/ui/nativedialog/),
[`internal/update`](../../internal/update/)

## D-8: Licence

**Status:** built · **By:** owner

Apache-2.0 ([`LICENSE`](../../LICENSE)).

## D-9: Scope against Lens's paid and vendor features

**Status:** partly built · **By:** owner

Lens Desktop gates twelve features behind paid plans
([premium features](https://docs.lenshq.io/k8slens/premium-features/)) and carries vendor features.

| Lens feature | In Cogpit |
|---|---|
| Flux CD and Argo CD views | in the beta: curated kind profiles, both dashboards, Argo CD's Sync and Refresh ([G23](../parity/gaps.md#g23-flux-cd-and-argo-cd-views)) |
| Hotbar, the editor's diff | built: cheap with the interface and the editor |
| Security Center, cost monitoring | after the beta, for the full target only ([G18](../parity/gaps.md#g18-paid-lens-desktop-features)) |
| Extensions | not a target ([D-5](#d-5-extensions)) |
| EKS, AKS and GKE: the account integrations and the credential plugins (`aws eks get-token`, `aws-iam-authenticator`, `gke-gcloud-auth-plugin`, `kubelogin`) | not a target, won't build |
| MCP server, Ask AI | superseded by [D-2](#d-2-agent-interface-scope) and [D-10](#d-10-built-in-agent) |
| Teamwork | out of scope: it needs a hosted relay, unless the owner decides to run one |
| Lens ID activation, subscriptions, telemetry, surveys | never |

Cogpit runs any exec credential plugin a kubeconfig names once the user approved it
([D-26](#d-26-binding-a-credential-plugins-approval-to-its-program)), but nothing is built, tested
or promised for the cloud providers' plugins; their checklist rows are `not a target`
([platform](../parity/platform-parity.md#cluster-authentication-and-connectivity),
[interface](../parity/ui-parity.md#preferences)). Feature breadth's paid component counts the
premium list without extensions and the cloud account integrations: 10 features
([target-app parity](../parity/target-app-parity.md#feature-breadth)).

**Why:** the owner's organisation keeps GitOps repositories for Flux CD and Argo CD, so their views
are daily work, not a paid extra. Security Center and cost monitoring need data sources and are not
the mainstream audience's daily work. Vendor features serve the vendor, not the user.

**Rejected:** all paid features after the beta — the GitOps views late although used daily; all
paid features in the beta — 150–320 h more before it, with cloud accounts and data sources;
none — Cogpit behind Lens Pro, GitOps included.

**Code:** [`kinds/flux.go`](../../internal/model/kinds/flux.go),
[`kinds/argocd.go`](../../internal/model/kinds/argocd.go) · **Tests:** `TestKindFlux`,
`TestKindArgoCD`

## D-10: Built-in agent

**Status:** built · **By:** owner

A chat column at the window's right edge (⌘I, Ctrl+I; `chat.toggle`) runs an agent that answers
about what the user sees and acts for the user. It dispatches the registry's commands in-process
with origin `builtInAgent`, so D-2's confirmations, the audit log and the agents' read rules apply
and the external MCP server's switch ([D-49](#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)) does not touch it. The chat is no command and no
MCP tool. A conversation keeps the context it began on.

- **Endpoints:** OpenAI's Chat Completions API with streaming and tool calling, on plain
  `net/http`; the kind (LM Studio, LiteLLM, Open WebUI, another compatible server) sets the paths.
  Several connections, one the default; no redirects are followed, so a key reaches only its host.
- **Tools:** 20 core commands plus `search_commands` and `run_command` (22 tools, under 40 KiB),
  or, set per connection, every command (about 200 tools, 70,000 tokens) without meta-tools; never
  GUI-only commands or `chat.toggle`. Each message starts with a compact `ui_state`; a tool result
  goes back framed as data and cut at 32 KiB; a model's thinking is dropped.
- **Limits per turn,** set per connection: the third identical tool call, 25 calls, 300,000
  tokens, 10 minutes. Stop, Escape in the input and ⌘. from any focus end a turn; so does showing
  another cluster, before the next tool call.
- **Private connection** (the user's mark, off by default): the turn reads what the window shows,
  Secret values included ([H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)); write rules, terminal
  switches and exec plugin arguments stay as for every agent. Unmarked, it reads as external agents do ([H-13](../security/agents-and-mcp.md#h-13-cluster-data-leaves-for-the-llm-endpoint)).
- **Conversations:** the last 20 per context in an owner-only file, each piece scrubbed by the H-82
  filter as it becomes final (`<withheld: …>` where that fails); never one with a private turn
  ([H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)). Editing a sent message cuts it and all after it, unknown to the model ([H-100](../security/agents-and-mcp.md#h-100-a-cut-parts-tool-calls-leave-the-models-view)).
- **Keys and transport:** keys in the owner-only `llm-keys.json`, no keychain ([H-99](../security/credentials.md#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)); plain
  `http://` to another machine is warned about ([H-95](../security/credentials.md#h-95-an-llm-api-key-over-plain-http)); a CA file replaces the system's roots
  ([H-104](../security/trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)); the test and the model list send the form's values ([H-143](../security/credentials.md#h-143-the-stored-api-key-goes-to-an-edited-unsaved-base-url)).
- **Connection test:** DNS, TCP, HTTP or HTTPS, Authentication, Models, Tool use, each bounded and
  without cluster data; a failed layer shows its request and answer as `curl -v` does, key masked.
  `TestLiveEndpoint` needs a real endpoint and runs locally; a LiteLLM key for CI: decided, not built.

**Why:** an agent beside the views as in VS Code, local models first, that operates Cogpit itself
(Lens Desktop's Ask AI launches an external AI tool). An external agent driving the chat could read
through a private connection what it may not read itself. The API subset is small and the servers'
quirks need the raw events; every command as a tool fills a local model's context. A kill switch
must work while the user looks elsewhere, and a turn must not act on a cluster not its own. The
keychain asked for the login password at every new code signature.

**Rejected:** the chat as a dock tab — it vanishes when its agent opens a logs or terminal tab; a
question per cluster and connection before sending — stricter than an external agent.

**Code:** [`internal/agent`](../../internal/agent/), [`engine/chat.go`](../../internal/engine/chat.go) · 
`TestToolsetSize`, `TestConnectionTestShowsTheExchange`, `TestPrivateConversationIsNotKept`

## D-11: Crash stance

**Status:** built · **By:** owner

A Cogpit crash is a bug, not a catastrophe: the cluster's state lives in the cluster. A panic in the
UI is fixed like any bug and blocks no release on its own; a goroutine doing cluster I/O recovers
from a panic at its boundary and reports an error, so that one panicking watch does not close every
cluster. An operation of several API calls is crash-safe: recorded before it starts, its
half-finished state offered for a decision later.

- **Drain and Helm install, upgrade, rollback and uninstall** are journalled (`drains.json`,
  `helm.json`, owner-only, locked between Cogpit processes,
  [H-22](../security/privilege-footprint.md#h-22-two-cogpit-processes-share-the-journals)). An entry
  left by a crash, a lost connection ([D-58](#d-58-standby-and-lost-connections)) or a stopped drain
  job ([D-63](#d-63-long-commands-as-background-jobs)) shows in its context's banner
  (`node.pendingDrains`, `helm.pendingOperations`), offered against the cluster as it is then: a
  drain to finish, uncordon or dismiss; a rollback or uninstall to finish or dismiss; an upgrade left
  `pending-upgrade` as a rollback to its starting revision (the Helm SDK refuses to upgrade a pending
  release); an install only with its release's state, Show release and Dismiss, the clean-up being
  the user's in the Helm Releases view. Nothing continues or is undone by itself.
- **Unsaved editor text** of an Edit or Create tab is a draft (`drafts/<id>.json`, owner-only),
  written 2 s after the text stops changing (at least every 10 s) and at quit, removed on save or
  close, offered when its context connects (`editor.drafts`), kept when its context leaves Cogpit.
  Never a Secret's text or Helm values ([D-19](#d-19-editing-and-creating-secrets-from-yaml)).
- **Crash reports:** the Go runtime also writes a crash into `crash.log` (0600,
  `runtime/debug.SetCrashOutput`), which a clean exit removes; the next start moves it to `crashes/`
  (the newest ten kept) and says where. Nothing is uploaded.

**Why:** PhotoCraft's "never crash" priority protects work held in the app; Cogpit's work is the
cluster's state, which a crash endangers only halfway through an operation of several API calls.
An install is journalled although nothing is offered for it: the entry keeps a second Cogpit
process off the release and tells the user after a crash what was left.

**Code:** [`engine/drainjournal.go`](../../internal/engine/drainjournal.go),
[`engine/helmjournal.go`](../../internal/engine/helmjournal.go),
[`engine/drafts.go`](../../internal/engine/drafts.go), [`cmd/cogpit/crash.go`](../../cmd/cogpit/crash.go) ·
**Tests:** `TestDrainJobCancelledLeavesAPendingDrain`, `TestHelmBannerOffersUpgradeRollback`,
`TestHelmBannerOffersNothingAfterAnInstall`, `TestNoDraftsForSecretsOrHelmValues`,
`TestCrashReportSurvivesAPanic`

## D-12: Parity sources

**Status:** built · **By:** owner

The target is the current Lens Desktop. Checklist rows come from its documentation and release
notes and, where those are silent, from a capture of the running app. OpenLens and its fork
Freelens are not sources. No code, text or asset from Lens Desktop, OpenLens or Freelens is used.

**Why:** OpenLens's source was retired in January 2024; it and Freelens describe an older Lens.

## D-13: Cache

**Status:** built · **By:** owner

Cogpit caches so that the UI is fast and the API server carries as little load as possible while
the view stays current: watches write every change into the cache, the UI and agents read only
from it, and what cannot be watched is polled only while something visible needs it. The cache
lives in memory only; of a cluster only the discovery answer is kept on disk
([D-65](#d-65-discovery-cached-on-disk)). The mechanisms: [architecture §5](architecture.md#5-cache).

Secrets are listed and watched in full, so that the Secrets view fills its Keys and Type columns as
Lens does. The drawer reads the opened Secret from the cache and masks its values until the user
reveals one (`secret.reveal`, GUI only); agents get them redacted
([H-8](../security/privilege-footprint.md#h-8-secret-data-arrives-with-the-list)). Helm reads its
release Secrets from that watch while it runs, else from its own `owner=helm` watch, so that the
Releases view alone never holds every Secret. The cache drops the
`kubectl.kubernetes.io/last-applied-configuration` annotation from every Secret: it repeats the
values.

**Why:** in memory, no stale state is shown as current and no cluster data rests on disk; the price
is a list at every start. Revisit only if the measured cold start (budget P7) is too slow.

**Rejected:** persisting every kind — Secrets at rest, stale state until the first relist;
persisting non-sensitive kinds marked stale — complexity, and which kinds are sensitive is the
user's call; Secrets as metadata only, with a GET per drawer — empty Keys and Type columns; no
cache for Secrets — a request per view, against cache-first.

**Code:** [`internal/cache`](../../internal/cache/cache.go) · 
`TestBudgetSecretsViewAndDrawer`, `TestBudgetSecretsAndReleasesShareOneWatch`,
`TestSecretsLoseTheLastAppliedAnnotation`, `TestAgentsReadSecretsRedacted`

## D-14: Terminal widget

**Status:** built · **By:** owner

Cogpit uses a patched copy of `fyne-io/terminal` in `third_party/fyne-terminal` (a `replace` in
`go.mod`), fuzzes the widget on its own, without Cogpit's escape filter in front of it
([H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input)), and offers its
patches upstream; the copy goes once upstream has equivalent fixes. Each patch is marked
`Cogpit patch:` and listed in [PATCHES.md](../../third_party/fyne-terminal/PATCHES.md). Patches go
upstream as grouped pull requests, one commit and test per fix, opened under the owner's account
after he has looked at the series; those not offered yet wait to go together at the end.

- What the right mouse button does is the user's terminal preference alone
  (`SetRightClickAction`; agents and the CLI get `ErrRightClickGUIOnly`).
- Off macOS, Ctrl+C always goes to the shell; Ctrl+Shift+C and Ctrl+Insert copy.
- The copy's tests and fuzz seeds run under the race detector (`make test-fork`); in CI only in the
  Linux job Race Tests (other packages).
- On Windows Cogpit starts a local shell itself on a pseudo console (ConPTY, through
  `golang.org/x/sys/windows`) and connects the widget with `RunWithConnection`, as on macOS and
  Linux ([`localpty_windows.go`](../../internal/terminal/localpty_windows.go)).

**Why:** container output, which anyone who can deploy controls, crashed or hung the upstream
widget on Fyne's main goroutine, taking the whole process with it. Fuzzing the widget alone finds
such inputs at small cost, and the patches are small and general.

**Rejected:** the copy untested — unknown crashing inputs stay; Cogpit's own widget on a maintained
VT emulator — replaces tested code, the step only if fuzzing shows the parser cannot be kept;
upstream without the patches — the known crashes come back; `make test-fork` in the macOS job too —
more time there for one macOS-only file (`term_darwin.go`); on Windows the widget's `RunLocalShell`
and its ConPTY package (`termtest/conpty`) — that package ends the process when a pipe cannot be
made and starts the shell before it can be put in a job object.

**Tests:** `FuzzHandleOutput`, `FuzzRawWidget`, `TestRunLocalShell_MainLoop`

## D-15: Progress generators

**Status:** built · **By:** owner

`make parity` ([`hack/parity`](../../hack/parity/main.go), CI's Parity job) checks the
hand-written checklists and counts what they report. It refuses a row that appears twice, a row
marked done or partial without evidence, a command id the registry does not know, a link to a
missing file and an unknown test name, and compares its counts with the numbers ROADMAP.md and the
progress pages report. A plain `go run ./hack/parity` also writes the generated
[`parity-checklist.md`](../parity/parity-checklist.md) (every checklist row with its status and
section, and the counts); under `-check`, as `make parity` runs it, a page that differs fails.

`make scorecard` ([`hack/scorecard`](../../hack/scorecard/main.go)) writes
[`scorecard.md`](../parity/scorecard.md) from the budget tests and benchmarks of
[testing › Performance budgets](testing.md#performance-budgets), run without
the race detector: per budget its proposed target, every measurement with its limit and result,
the date, the machine (GOOS/GOARCH, CPU model and count, OS version, Go; no host name) and the
budgets nothing measures. It runs by hand and the page is committed; a run in CI on a fixed
runner, once budgets are measured there regularly, is decided, not built.

- A budget test reports through `scorecard.Record`
  ([`testkit/scorecard`](../../internal/testkit/scorecard/scorecard.go)): one JSON line per
  measurement, with its limit, when `COGPIT_SCORECARD` names a directory. The package imports
  nothing of Cogpit's, so the window's tests may call it.
- Benchmarks are read from their `ns/op`. A line from a race build fails the run, and so does a
  listed test that records nothing, which would otherwise drop its budget silently.
- P8 is the editor widget's work per keystroke against 16 ms; the full software paint is context
  only, since the software painter rasterises every text object on every capture.
- The cluster tier's measurements (kind) are not part of a run; testing.md records them.

**Why:** the checker keeps merged branches from listing a row twice and makes the counts
reproducible; shared CI runners would measure their own load.

**Rejected:** parsing the tests' `t.Logf` lines — their wording changes with every edit; the
generated page written only on a flag — every checklist edit needs the flag remembered; generating
it in CI only — the page would not be on GitHub to read.

**Tests:** `TestRowChecks`, `TestChecklistPageWrittenOrRefused`, `TestCollectRefusesASilentTest`,
`TestRecordWritesOneLinePerMeasurement`

## D-16: Releases and what they carry

**Status:** built · **By:** owner

Once every gate on `main` is green, semantic-release tags and publishes the release with the
organisation's GitHub App token, and the release pipeline
([`build.yml`](../../.github/workflows/build.yml)) builds and attaches the artifacts
([releasing › Release flow](releasing.md#release-flow)). A breaking change makes a minor release
until the owner decides on 1.0 (`releaseRules` in [`.releaserc.json`](../../.releaserc.json)).
Renovate's minor, patch, digest and pin updates are `build(deps):` commits, which make no release
of their own ([releasing › Renovate](releasing.md#renovate)).

A release carries the complete product only, each package with both binaries: the macOS disk image
(the unsigned app zip without the Apple signing material, as in a fork), the Linux `.deb` packages
([D-108](#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)), the Windows setup
([D-111](#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) and `SHA256SUMS`; there are no
separate `cogpit-cli` archives. In the app, `cogpit-cli` is `Contents/MacOS/cogpit-cli`, the
binary `make build` makes, with the app's linker flags, architecture and minimum macOS, signed
inside-out with the app as `io.guided-traffic.cogpit.cli` and covered by its notarization and
Sparkle's updates.
The app offers the stdio registration of the `cogpit-cli` installed with it
([D-2](#d-2-agent-interface-scope)). The app job checks the bundled `cogpit-cli version`; the
signing job builds and runs nothing, so no Cogpit code runs beside the signing identity. Both
binaries report one version, `internal/buildinfo`'s `version`, set by the release build's
`DIST_LDFLAGS`
([releasing › The version a build reports](releasing.md#the-version-a-build-reports)).

**Why:** the team's Go repositories release the same way, and 1.0 is the owner's call. One product
per platform is installed, signed, notarized and updated as a whole, the CLI with it.

**Rejected:** `Contents/Helpers` — a second code location the signing script and the app would both
have to know. Building the bundled CLI again with `-trimpath` — every package compiled twice in a
release job without a build cache. A `main.version` per binary — two defaults to keep alike;
`git describe` — only `make` builds would carry it.

**Code:** [`Makefile`](../../Makefile) `app`, `DIST_LDFLAGS`;
[`release.sh`](../../hack/macos/release.sh); [`buildinfo.go`](../../internal/buildinfo/buildinfo.go)
· **Tests:** `TestMakefileSetsTheVersion`

## D-17: Token files outside the kubeconfig's folder

**Status:** built · **By:** owner

A context whose user names a `tokenFile` outside its kubeconfig's own folder connects only after
the user allowed it, in the credential plugin's approval dialog ("Allow this file as the login
token?") and approval store, which only the GUI writes. The dialog names the file, the server its
content goes to and how that server is reached and trusted; a context that also runs a plugin gets
one dialog and one approval for both. The approval binds the file's path, the server and its
trust, not the file's content, so a rotated token does not ask again. The policy is asked before
client-go reads the file. A token file inside the kubeconfig's folder comes from the same source
and needs no approval. Every source counts, `~/.kube/config` and the files `$KUBECONFIG` names
included ([D-28](#d-28-the-default-kubeconfig-as-sources-as-lens-has-it)).

**Why:** closes
[H-24](../security/trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token),
a dropped kubeconfig sending any file the user can read as its token, without breaking legitimate
setups, with the one mechanism for everything a kubeconfig runs or reads on the user's computer
([H-1](../security/trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)).

**Rejected:** refusing token files of synced files only — a legitimate synced setup breaks without
recourse, and added files stay open; refusing every file a kubeconfig names — breaks the many
kubeconfigs that name certificate files, which send nothing that matters.

**Code:** [`sources.go`](../../internal/cluster/sources.go) `checkTokenFilePolicy`,
[`pluginfiles.go`](../../internal/cluster/pluginfiles.go) `BindPluginFiles`,
[`exec.go`](../../internal/kubeconfig/exec.go) `AsksForTokenFile` · 
`TestTokenFileOutsideTheFolderIsAskedFor`, `TestTokenFileOutsideTheFolderNeedsApproval`,
`TestApprovalDialogAsksForATokenFile`

## D-18: Secret annotations seen by agents

**Status:** built · **By:** owner

Agents see a Secret's annotations in full through `object.get`, except kubectl's
`last-applied-configuration`, which the cache drops from every core Secret because it repeats the
values; `resources.list`, `resources.find` and `table.exportCsv` show no annotations. A tool that
copies a Secret's manifest, values included, into another annotation reaches agents with those
values: the risk is accepted
([H-23](../security/agents-and-mcp.md#h-23-secret-annotations-other-than-kubectls-reach-agents)).

**Why:** agents see what the user sees in the drawer, and no rule over annotations is complete
without knowing every tool that writes them.

**Rejected:** withholding every Secret annotation's value — agents lose harmless values (a
certificate's issuer); an allowlist of harmless annotations — a list to maintain; a denylist of
known manifest copies — an unknown tool slips through.

**Code:** [`cache.go`](../../internal/cache/cache.go) `stripManagedFields` · 
`TestSecretsLoseTheLastAppliedAnnotation`

## D-19: Editing and creating Secrets from YAML

**Status:** built · **By:** owner

Secrets are edited and created from YAML in the GUI only. An Edit or Create resource tab holding a
Secret shows the user its values (base64 in `data`, plain text in `stringData`) and works like any
editor tab (diff, server-side dry run, the resourceVersion precondition). Agents and the CLI can
neither open such a tab, read its text, nor check, apply or create a Secret (`ErrSecretGUIOnly`);
the same holds for a Helm release record kept in a ConfigMap. Values never reach the audit log, an
error or a toast, and such a tab keeps no draft on disk. `secret.create` (the Create Secret dialog)
refuses every origin but the GUI before anything is asked or sent; the audit line of a refused call
names the Secret, its type and its keys, never a value. The dialog sends the values base64-encoded
in `data`: what is sent is what the API server stores and returns, as `kubectl create secret`
sends it, and the scrubbing of an API message knows each value in both forms. A Secret's values are
also edited in its drawer after Show, under the same rule
([D-59](#d-59-a-secrets-values-edited-in-the-drawer)).

**Why:** the user gets Lens's Edit while values never reach an agent, the pattern of
`secret.reveal` and the Helm Upgrade tab.

**Rejected:** agents writing values they bring without reading the existing ones — keeping
withheld values on save is complex, and an agent could overwrite credentials; metadata only — half
of Lens's Edit.

**Code:** [`secretedit.go`](../../internal/engine/secretedit.go),
[`commands/secretcreate.go`](../../internal/engine/commands/secretcreate.go),
[`drafts.go`](../../internal/engine/drafts.go) · **Tests:** `TestSecretEditAndCreateAreGUIOnly`,
`TestSecretCreateIsGUIOnly`, `TestNoDraftsForSecretsOrHelmValues`

## D-20: A port forward that cannot connect

**Status:** built · **By:** owner

A forward whose dial to the pod fails with an error that waiting does not cure turns Failed at
once, frees its local port and names the cause; any other error is retried with the backoff.
Permanent are the API server's answers to the portforward request 404 (the pod or its namespace is
gone), 403 (no `create` on `pods/portforward`) and 401 (credentials rejected after client-go's
refresh). Retried: network errors, dial timeouts, 5xx, 429, 400 (the answer while a pod is not
running yet) and errors without a status. client-go's SPDY dialer drops the status, so Cogpit
records the status of the dialer's last response (`DialRefusedError`). A refused connection to the
pod's port is no dial error and keeps reconnecting (the application may be starting); a pod or
namespace gone from the cache keeps the forward Waiting, since the cache cannot tell a deleted pod
from one a controller recreates.

**Why:** a clear state, no pointless requests and the port freed, while passing errors are still
bridged.

**Rejected:** failing after N attempts that never connected — a slow pod start kills the forward;
retrying for ever in a "Failing" state — the load and the occupied port stay.

**Code:** [`portforward_errors.go`](../../internal/cluster/portforward_errors.go)
`IsPermanentForwardError`, [`portforward_run.go`](../../internal/engine/portforward_run.go) ·
**Tests:** `TestPortForwardFailsAtOncePermanentErrors`, `TestDialErrorsAreClassified`

## D-21: Pasting a kubeconfig

**Status:** built · **By:** owner

Cogpit does not add kubeconfigs from pasted text, though Lens's "Add Kubeconfigs" menu offers
"Paste from clipboard": it reads the files and sync folders the user adds, where they are. An entry
of kind `pasted` in `kubeconfigs.json` is skipped and dropped at the list's next save; its file
under `<user config dir>/Cogpit/kubeconfigs/` stays, being the user's.

**Why:** a stored copy of a kubeconfig is a second place for a credential to live
([credentials](../security/credentials.md)).

**Code:** [`kubeconfigs.go`](../../internal/engine/kubeconfigs.go) `legacyPastedKind` · 
`TestPastedSourcesOfEarlierBuildsAreNotOffered`

## D-22: Colour theme

**Status:** built · **By:** owner

The terminal's sixteen ANSI colours are Solarized's, VS Code's `terminal.ansi*` of Solarized Dark
and Light (the same in both), copied once and set through a patch to the terminal widget
([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md), [ATTRIBUTION.md](../../ATTRIBUTION.md));
the session-ended notice is bright yellow, since bright black does not read on the dark background.
The window's palettes are Dark Cockpit's ([D-110](#d-110-dark-cockpit-cogpits-design-language)).

The theme is a preference (Preferences › App › Theme: Sync with computer, the default, Dark or
Light), applied at once; `--theme dark|light|system` overrides it for one run, and the title bar's
toggle sets it ([D-54](#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle)).

A cluster's icon colour is one of eight named colours (`engine.IconColors`), each in a shade per
theme that reads at 3:1 where identities are drawn (`identityColors`), or any colour from Fyne's
picker, stored as `#rrggbb`: it keeps its hue and saturation, and its lightness moves (up in the
dark theme, down in the light one) just far enough for 3:1 on every identity surface
(`legibleShade`). Only the user sets it; agents and the CLI are refused
([agents and MCP](../security/agents-and-mcp.md)). The accent colour
([D-80](#d-80-a-clusters-accent-colour)) offers the same choices. The connection dot is the Running
containers' green (`colorContainerRunning`) while Connected or (re)connecting, the status red for
Login expired and Connection failed, grey for Not connected
([D-58](#d-58-standby-and-lost-connections)).

**Why:** Solarized is the owner's working theme; copying its values once keeps Cogpit independent
of VS Code's files. A picked colour has to stay legible in both themes.

**Rejected:** drawing a picked colour as picked, with a contrast warning — initials that may not
read in one theme; refusing colours that fail the contrast — the picker would reject most pale and
dark colours.

**Code:** [`termview.go`](../../internal/ui/fyneui/termview/termview.go),
[`theme.go`](../../internal/ui/fyneui/theme.go) `identityColors`,
[`identitycolor.go`](../../internal/ui/fyneui/identitycolor.go) `legibleShade` · 
`TestSetANSIColors`, `TestIdentityColorsContrast`, `TestCustomIdentityColorContrast`

## D-23: The status bar's owner link and MCP switch

**Status:** built · **By:** owner

The status bar always shows "Need a Kubernetes Expert? Visit: hans-fischer.com", before connecting
and on the demo cluster too, at its right end directly left of the MCP item (ending the bar under
`--no-mcp`). A click opens https://hans-fischer.com in the default browser; nothing is sent before.
The link takes the room the status items leave: shortened with "…", and hidden with its divider
when narrower than "Need a…" (`statusLayout`).

A click on the status bar's MCP pill switches the external MCP server, the loopback HTTP endpoint
other programs reach with the bearer token, off and on. Off ends every agent connection and pending
confirmation (the command does not run), removes the endpoint file, strikes the pill and disables
Help › Copy MCP command; on starts the server with the token and port it kept. The gear beside the
pill opens the MCP details
([D-53](#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it)). The engine and the
built-in agent keep working while it is off. Each switch is one audit line (origin `gui`, name
`gui.mcpServer`, `{"enabled": …}`). The choice is stored (Fyne's preference `mcp.enabled` and a
file beside the token file, which `cogpit-cli mcp` honours,
[D-49](#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)); with none stored the
server is off. `--no-mcp` leaves server and item out, `--mcp-addr` sets the address; no
environment variable does either. A failed start at launch shows the item off with the reason in
its tooltip and keeps the stored choice.

**Why:** both are GUI controls, not commands or MCP tools, so no agent opens the page or switches
the server on: a documented exception to "everything is a command".

**Code:** [`bars.go`](../../internal/ui/fyneui/bars.go) `statusLayout`,
[`mcp.go`](../../internal/ui/fyneui/mcp.go), [`mcpcontrol.go`](../../cmd/cogpit/mcpcontrol.go) ·
**Tests:** `TestMCPSwitchIsNoCommand`

## D-24: Logo

**Status:** built · **By:** owner

Cogpit's logo is the glowing C helm (`helm-glow`): a blue C with a white core glowing over a ship's
wheel of eight spokes and a hexagon hub, white handles and outer ring, on a petrol plate darkening
downwards; its settings are [`helm-glow.json`](assets/logo/helm-glow.json). The same tile is the
app icon, the header's mark (20 points), the welcome page and About, and the README's banner and
social preview, in both themes. One pure-Go rasteriser,
[`internal/ui/logo`](../../internal/ui/logo/logo.go), draws it for the GUI and for `hack/icon`
(the glow a Gaussian blur, as the lab SVG's `feGaussianBlur`). `make icon` writes
[`packaging/icon.png`](../../packaging/icon.png) with its own plate, transparent corners and
macOS's margin (100 of 1024 pixels), so `make app` passes `--use-raw-icon`. Every version stays
drawable: `Versions` in [`versions.go`](../../internal/ui/logo/versions.go) holds `gauge`, `helm`
(the first C helm, on Solarized base03) and `helm-glow`; `Current` is the one shown;
`go run ./hack/icon -version <name>` draws any; [`assets/logo/`](assets/logo/) holds each one's
picture and reference. Changing it:
[extending › Changing the logo](extending.md#changing-the-logo).

**Why:** a bright, saturated tile with heavy lines stays visible among other icons in the macOS
Dock at 64 points, where a dark, unsaturated tile with thin spokes gets lost.

**Rejected:** the plate-less mark in the glow's colours — its pale lines need darker inks on the
light theme; the tile on the welcome page only.

**Code:** [`internal/ui/logo`](../../internal/ui/logo/versions.go),
[`hack/icon`](../../hack/icon/main.go), [`bars.go`](../../internal/ui/fyneui/bars.go) `newLogo` ·
**Tests:** `TestEveryVersionDrawsItsPicture`, `TestPackagedIconIsCurrent`,
`TestLabLooksMatchTheirReferenceSVGs`

## D-25: Copying values

**Status:** built · **By:** owner

- **Drawer.** Every text is selectable (`selLine`: drag, double click, ⌘C); links, chips and pills
  are not, their copy icon covers them. Every value shows the copy icon on hover, a padding after
  what it draws; a line or link without room is shortened with "…" while it shows, and a value
  with action buttons (a port's Forward…) gets it over its end. One entry of a list value copies
  alone, `key=value` as `key: value` (`yamlEntry`).
- **Lists.** One icon per table follows the cell under the pointer, after the text or over the
  cell's visible end, never shortening the text; a click copies the full text. Numbers, times,
  metrics, counts, booleans and short state words have none, by a blacklist of column types and
  ids (`noCopyTypes`, `noCopyColumns`), so custom resource columns need no marking. A Secret's
  list holds no values.
- **Clusters.** Cluster Settings › General has the icon beside the name and the kubeconfig's whole
  path; a cluster's right-click menus have Copy Kubeconfig Path, read from the contexts the window
  holds. A context without a file of its own (the demo cluster, a merged one) has neither.
- **Secret values**
  ([H-25](../security/credentials.md#h-25-a-copied-secret-value-reaches-the-clipboard)): concealed
  and kept to this Mac on macOS, cleared after 60 s if still on the clipboard and when the window
  closes. A key's line has a copy and a b64 icon: both copy the whole value unseen through the
  GUI-only `secret.reveal` (`whole`, `base64`), the toast naming the key only; a value in the
  drawer's editor ([D-59](#d-59-a-secrets-values-edited-in-the-drawer)) copies as typed; a binary
  value has b64 only. An empty value, or one not read whole, copies nothing and says so.

After a copy the icon is a checkmark for 1.5 s and a toast confirms it. Log tabs, toasts and alert
names copy too ([D-94](#d-94-the-empty-docks-toggle-opens-a-terminal-and-alert-names-copy),
[D-99](#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)). No copy is a command, an MCP
tool or an audit line; `object.copyName` is the one copy command.

**Why:** agents read values through `object.get`; a copy command would only add a way to put a
Secret on the user's clipboard. A value pasted unseen must be whole, and base64 is the one form a
binary value survives a text clipboard in.

**Code:** [`copy.go`](../../internal/ui/fyneui/copy.go),
[`tablecopy.go`](../../internal/ui/fyneui/tablecopy.go),
[`secretcopy.go`](../../internal/ui/fyneui/secretcopy.go),
[`clipboard_darwin.m`](../../internal/ui/fyneui/clipboard_darwin.m) · 
`TestCopyHasNoCommand`, `TestListCopyIconByColumn`, `TestSecretMaskedValueCopiedWithoutShowing`,
`TestSecretB64OnABinaryValue`, `TestConcealedClipboardMarkedForThisMacOnly`,
`TestCopyKubeconfigPathInEveryClusterMenu`

## D-26: Binding a credential plugin's approval to its program

**Status:** built · **By:** owner

The approval of a kubeconfig's credential plugin binds the program by its content (SHA-256) when
it lies inside the kubeconfig's folder or the sync folder it came from, where a writer of that
folder could replace it
([H-1](../security/trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)); a replaced
program asks again. A CA file there is bound the same way, a symlink there by its target, a file
over 32 MiB by its size and modification time. A program elsewhere is bound by the command as the
kubeconfig names it (a name looked up on `PATH`, or a path), so a tool update does not ask again.
Cogpit appends its tool folders and the user's program folders after the `PATH` it started with
([D-120](#d-120-folders-searched-for-credential-plugins)), so a name `PATH` finds keeps resolving
to the same program. On Windows the bound file is the one `os/exec` runs: a path without a
`PATHEXT` extension binds the `plug.exe` that runs, not a `plug` beside it.

The approval also shows and binds the proxy Cogpit sets for the cluster (Cluster Settings ›
Proxy), the kubeconfig's own `proxy-url` beside it marked as not used; a changed proxy asks again
([H-34](../security/trust-boundaries.md#login-approval)).
It is bound as shown, its password as `•••`: another scheme, user, host or port asks again, a new
password alone does not, and `exec-approvals.json` holds no hash over the password. Without a
proxy of Cogpit's the field is left out of the fingerprint. A proxy change sends `EventContexts`,
so the context lists show the approval state at once.

**Why:** closes H-1's own threat, a program swapped through a synced folder, without asking again
after every tool update; whoever can write a `PATH` folder controls the user's account anyway.

**Rejected:** hashing every program — every plugin update (brew, gcloud components) asks again.
Binding the proxy with its password — a rotated proxy password asks again for every plugin of the
cluster; host and port only — a changed proxy user would not ask.

**Code:** [`pluginfiles.go`](../../internal/cluster/pluginfiles.go) `BindPluginFiles`,
[`exec.go`](../../internal/kubeconfig/exec.go) `Fingerprint`, `SetClusterProxy`,
[`proxy.go`](../../internal/engine/proxy.go) `proxyBoundPolicy`, `RedactProxyURL` · 
`TestProgramIsBoundByTheFileThatRuns`, `TestPluginApprovalBindsTheClusterProxy`,
`TestFingerprintBindsCogpitsProxy`, `TestApprovalDialogShowsCogpitsProxy`

## D-27: An agent's Helm values in the confirmation

**Status:** built · **By:** owner

When an agent upgrades a release with values passed inline (`helm.release.upgrade` with
`values`), the confirmation dialog shows those values, monospace, selectable and scrolling, before
the user answers, and the upgrade runs with exactly that text. It travels in
`ConfirmRequest.Review`, which is never serialised: no audit line, log or MCP result holds it. An
upgrade from a Helm Upgrade tab shows no values in the dialog: the tab is what the user reads, and
the upgrade is bound to its text revision.

**Why:** the user accepts only values they saw, and the agent still needs one call.

**Rejected:** refusing inline values for agents — two calls, and the user must open the tab to
read them.

**Code:** [`confirm.go`](../../internal/engine/confirm.go) `ConfirmRequest.Review`,
[`helmupgrade.go`](../../internal/engine/commands/helmupgrade.go) · 
`TestConfirmationShowsTheReviewText`, `TestHelmUpgradeAgents`

## D-28: The default kubeconfig as sources, as Lens has it

**Status:** built · **By:** owner

Every context comes from a source file read on its own; nothing is merged. `~/.kube` is the
default sync folder: `~/.kube/config` is an ordinary file of it, and removing the folder hides its
contexts. The files `$KUBECONFIG` (or `--kubeconfig`) names are session sources, each read alone,
removable for the session and back at the next start with its approvals kept; one directly in
`~/.kube` belongs to the folder. A bare context name (`--context`, `cluster.connect`) resolves to
the one file that defines it, refused with the ids when several do. A plugin in `config` needs the
user's approval like any other
([H-1](../security/trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)). Without
Cogpit's configuration directory there are no sources: the files are merged as kubectl does, and
nothing asks.

The approval dialog asks once per file and identical login (`ExecPlugin.Identity`: command,
arguments, environment, API version, interactive mode, `provideClusterInfo`, the program's binding
of [D-26](#d-26-binding-a-credential-plugins-approval-to-its-program), or the token file of
[D-17](#d-17-token-files-outside-the-kubeconfigs-folder)), listing every context of the file that
uses it and is not allowed yet with its server and trust; one click allows them all. A context
that waits only because it skips the certificate check is asked on its own. Approvals stay one per
context, each with its own fingerprint; `kubeconfig.approveExec` is GUI-only and refuses the whole
list, naming the contexts, if any changed or left after the dialog showed them.

Remove from Cogpit on a context of a sync folder unlinks the whole folder, `~/.kube` included; its
dialog names the folder, what stays on disk and every context that leaves, and the removal is
bound to those contexts (`cluster.remove`'s `contexts`): refused with the reason if they changed.

**Why:** the user sees, approves and removes each file's contexts, and a `KUBECONFIG` set for a
run keeps working. One dialog for ten contexts with the same plugin spares ten that invite
approving without reading, while each approval still binds its server.

**Rejected:** one approval per file and plugin whatever the context — a changed server would not
ask; unlinking what the folder holds at the click — a file synced in meanwhile would leave unseen.

**Code:** [`kubeconfigs.go`](../../internal/engine/kubeconfigs.go) `WithDefaultFolder`,
`WithSessionFiles`; [`kubeconfigs_connect.go`](../../internal/engine/kubeconfigs_connect.go)
`ResolveContext` · **Tests:** `TestDefaultKubeconfigIsOneFileOfTheSyncedFolder`,
`TestSessionFilesAreSourcesOfTheirOwn`, `TestPendingApprovalsGroupContextsByFileAndPlugin`,
`TestApprovalIsRefusedAsAWholeWhenOneContextChanged`, `TestRemoveFromCogpitUnlinksTheFolder`

## D-29: Helm chart repositories from Helm's repositories.yaml

**Status:** built · **By:** owner

Cogpit uses the repositories of Helm's own `repositories.yaml` (resolved as the helm CLI resolves
it, `helm.RepositoryConfigPath`) and never writes Helm's file or folders: repositories are added
and removed with the helm CLI. The file is looked at every 2 s and read once it held still for
250 ms, so a change shows within 5 s; a file that does not parse keeps the last good list.
`helm.repo.list` returns the file, its status (`ok`, `missing`, `error`) and its repositories.

- **Indexes** go to Cogpit's own cache, keyed by name and URL: on `helm.repo.update` and, while the
  Charts view or a Helm Install tab is open, when older than 10 minutes or new or changed.
- **Credentials** stay in memory and never print (`helm.Repository`). Basic auth goes only to the
  repository's own scheme, host and port, per redirect hop, unless `pass_credentials_all`; an https
  repository never downloads over http. Entries Cogpit cannot use (OCI, a name a chart id cannot
  hold, a second entry of a name, a relative file path, past 256) are listed with the reason.
- **Basic auth over plain http** waits until the user allows it once per repository in the window
  (a banner; `helm.repo.allowHTTPCredentials` is GUI-only). The allowance does not expire and binds
  the name, URL and user name, not the password; an unreadable allowances file allows nothing
  ([H-40](../security/credentials.md#h-40-credentials-over-plain-http)).
- **Plain http and `insecure_skip_tls_verify`** are marked, not refused: "(http)", "(insecure TLS)"
  in the Charts view, a warning (`kinds.HelmOriginWarning`) in the drawer, the Install and Upgrade
  tabs, the preview and an agent's confirmation; an install or upgrade whose origin changed after
  the user read it is refused ([H-41](../security/trust-boundaries.md#h-41-repositories-whose-content-nothing-authenticates)).
- **Chart ids** are `<repository>.<chart>`, the repository's dots doubled (`kinds.HelmChartName`).
- **Releases:** Install shows the preview first; an upgrade to the release's own version uses the
  stored chart; a name both storage drivers hold in one namespace shows once, the one deployed
  last, its drawer noting the other; no sql driver; only the window chooses the ConfigMaps driver.

**Why:** the helm CLI stays the one place repositories are kept. Cogpit's own index cache never
shows an index fetched for an old URL, at the cost of each index being downloaded twice.

**Rejected:** a list of Cogpit's own; credentials over http refused (an internal http repository
would be unusable) or sent after a warning (the first download has sent them already); chart ids
`<repository>/<chart>` (`/` splits cache keys and `cogpit://` names) or `<repository>:<chart>`
(every id changes).

**Code:** [`helm/repofile.go`](../../internal/helm/repofile.go), [`helm/repo.go`](../../internal/helm/repo.go),
[`engine/helmrepos.go`](../../internal/engine/helmrepos.go), [`engine/helmhttp.go`](../../internal/engine/helmhttp.go)
· **Tests:** `TestHelmRepoFileChangesWithinFiveSeconds`, `TestNoCommandWritesHelmsFile`,
`TestCredentialsStayOnTheRepositoryHost`, `TestHelmCredentialsOverHTTPWaitForTheUser`

## D-30: Certificates in Secrets, decoded

**Status:** built · **By:** owner

Every key of a Secret, of any type, whose value holds PEM `CERTIFICATE` blocks or DER certificates
gets a drawer group "Certificates · <key>" after Data (`tls.crt` and `ca.crt` first): a Checks
section, then one section per certificate in the value's order, each field its own copyable item
(subject, issuer, serial, validity, subject alternative names, public key, signature algorithm,
CA, key usages, self-signed, SHA-256 fingerprint, key ids). Not After is a pill: OK with more than
30 days left, warning at 30 or fewer, error when expired or not yet valid. Agents, the built-in
agent's LLM endpoint included, read the same in `object_get` (`certificates`, times in RFC 3339
UTC), never the PEM text, a value or a private key. Lens has no such section.

- **Only certificates are decoded:** the PEM parser sees only `CERTIFICATE` blocks, the DER reader
  only elements shaped like a certificate, and a key named like a private key (`tls.key`, `*.key`,
  `*key.pem`, `*key.der`) is never searched.
- **Checks:** "Key Matches" compares the first certificate's public key with its private key
  (`<base>.key` or `<base>-key.pem`; else, with one certificate key in the Secret, its one key named
  like a private key; or a key in the same value), parsed in memory inside one function and zeroed
  where Go allows; an encrypted key is a warning ("TLS and Ingress cannot use it"). "Chain In
  Order" checks that each certificate is signed by the next. "Chain Leads To" names a public CA
  (Mozilla's NSS roots, embedded from `golang.org/x/crypto/x509roots/fallback/bundle`), a CA the
  computer trusts, or an internal or self-signed one, judged at the middle of the leaf's validity.
  Both chain checks run for a leaf or a value with its key, not for a CA bundle; revocation is not
  checked.
- **Bounds:** a value over 1 MiB is not decoded; at most 16 certificates per key and 64 per Secret;
  a private key over 64 KiB is not read; texts are cut at 1 KiB and name lists at 100, control
  characters and bidirectional overrides escaped.

**Why:** a certificate is public material every TLS client is sent anyway, so it is the one
exception to "Secret values never reach an agent" ([agents and MCP](../security/agents-and-mcp.md#rules));
the key check is the only place Cogpit parses a private key
([credentials](../security/credentials.md#secret-objects)).

**Rejected:** for the anchor, the computer's trust store alone — a corporate root would read as
public; the public bundle alone — no answer for a corporate root.

**Code:** [`model/certs.go`](../../internal/model/certs.go), [`model/keymatch.go`](../../internal/model/keymatch.go),
[`model/certanchor.go`](../../internal/model/certanchor.go), [`engine/secretcerts.go`](../../internal/engine/secretcerts.go)
· **Tests:** `TestObjectGetDecodesCertificatesForAgents`, `TestSecretKeyMatchesCertificate`,
`TestSecretCombinedPEMKeyCheck`, `TestCertAnchor`, `TestKindSecretCertificates`

## D-31: Every timestamp in the local time zone and format

**Status:** built · **By:** owner

Every absolute time Cogpit shows is written in the Preferences' time zone (the computer's by
default, summer and winter time from the zone database) and in the date and time format of the
computer's region: "10.09.2026 17:41:01" in Germany. There is no format setting; every writer takes
a `model.TimeLayout`, so a setting could replace the computer's.

- **Source:** on macOS (with cgo) CoreFoundation's short date and medium time of the current
  locale, which honours the region, the user's own formats and the 24-hour switch. Linux, Windows
  and `cogpit-cli` read the POSIX locale (`LC_ALL`, else `LC_TIME`, else `LANG`) against a built-in
  table of CLDR patterns (42 territories, 23 languages); C, POSIX, an unknown locale or none give
  ISO 8601. Windows' region settings are not read.
- **Conversion** ([`locale.GoLayout`](../../internal/locale/icu.go)): a year gets four digits,
  24-hour hours two, names and AM/PM are English; a pattern Go cannot write (an era, a quarter, a
  week, the day period `B`, over 128 bytes) falls back to ISO 8601 for its part.
- **One formatter:** each binary looks the layout up once at start (`engine.Options.TimeLayout`);
  the engine writes with `Session.TimeFormat`, the window with `UIState.TimeFormat` in the same
  zone. A kind writes a drawer time as `model.MarkTime`, replaced by `TimeFormat.WriteSections`.
- **Kept as they are:** ages and countdowns; what Cogpit writes for machines (its files, an
  object's YAML, structured times in MCP results, a credential plugin file's fingerprint note that
  an approval compares) stays RFC 3339.
- **Agents** read drawn texts (`view_rows`, `resources_list`, `object_get` sections) in the user's
  format, and `ui_state`'s `timeFormat` names the layouts. A `time` column has no cell text, so the
  list's search does not find a time.

**Why:** a person reads times in their own zone and notation. macOS's region settings are the
source because a Finder-started app has no `$LANG`.

**Code:** [`internal/locale`](../../internal/locale/), [`model/timefmt.go`](../../internal/model/timefmt.go)
· **Tests:** `TestGoLayout`, `TestEnvironment`, `TestTimeFormatZoneAndSummerTime`,
`TestMarksWrittenInTheFormat`, `TestAgentsReadTimesAsDrawn`,
`TestTheWindowWritesTimesInTheUsersFormat`

## D-32: Every byte amount in binary units with one decimal

**Status:** built · **By:** owner

Every byte amount a person or an agent reads as drawn is written in binary units: whole bytes up to
1023 ("512 B"), then KiB, MiB, GiB, TiB, PiB and EiB with one decimal ("15.6 GiB"), a space before
the unit, a decimal point whatever the region. The unit follows the rounded text, so 1 MiB less one
byte reads "1.0 MiB", never "1024.0 KiB"; an amount that need not be whole (a rate, a chart value)
is rounded to whole bytes below 1 KiB. Lens's own notation is not captured.

- **One formatter** in L0 for the engine, the window and the CLI: `model.FormatBytes`,
  `FormatByteAmount`, `FormatByteRate` ("…/s"), `FormatByteQuantity` ("16360780Ki" → "15.6 GiB";
  decimal suffixes too: "1G" → "953.7 MiB") and `FormatResource` (bytes for `memory`, `storage`,
  `ephemeral-storage` and `hugepages-*`, also as quotas name them; CPU, counts and extended
  resources as written). Milli-units ("500m"), fractions of a byte, text that is no quantity and
  amounts beyond what Kubernetes holds stay as written.
- **Applies to** lists, drawers, charts (axis and hover panel alike), value sizes, download
  progress and size messages. A message about a bound names the bound only ("larger than the
  4.0 KiB Cogpit accepts"): with one decimal a size just over it would read like it.
- **Sorting** stays by bytes (`Cell.Num`); structured results stay raw (`metrics.get`'s
  `memoryBytes`, a revealed value's `size`, an object's YAML).
- **Kept as written:** the YAML view and the editors; huge pages' resource names ("Hugepages
  2Mi"); a DRA device capacity not named `memory` (no unit declared); the audit log's exact byte
  counts; a credential plugin file's fingerprint note (part of its approval); limits on typed text
  (lengths, not amounts); command descriptions and parameter schemas; the performance harness.

**Why:** raw quantities such as `16360780Ki` or `13856852378` are unreadable; one formatter keeps
the window, the CLI and agents in agreement.

**Code:** [`model/bytes.go`](../../internal/model/bytes.go) · **Tests:** `TestFormatBytes`,
`TestFormatByteAmount`, `TestFormatByteQuantity`, `TestByteResource`,
`TestNodeDrawerBytesInBinaryUnits`, `TestPersistentVolumeClaimSizeSorts`

## D-33: The Flux actions

**Status:** built · **By:** owner

Reconcile, Reconcile with Source, Reset, Suspend and Resume act on one Flux object or a selection of
at most 500 (`flux.reconcile`, `flux.reconcileWithSource`, `flux.reset`, `flux.suspend`,
`flux.resume`, each with a `…Many` form), each one JSON merge patch per object as the flux CLI
writes it, bound to the object's UID (`cluster.FluxPatch`). Lens's Flux actions are not captured.

| Action | Writes | Applies to |
|---|---|---|
| Reconcile | `reconcile.fluxcd.io/requestedAt` (now, RFC 3339 with nanoseconds); agents' `force` adds `forceAt` (Helm releases) | every kind but External Artifacts; an Alert or Provider only with a status |
| Reconcile with Source | the same on the source, then on the object | Kustomizations, Helm releases |
| Reset | `resetAt` and `requestedAt`, one token | Helm releases |
| Suspend, Resume | `spec.suspend: true`; `false` with a reconcile request | every kind but Image Policies and External Artifacts |

- On a suspended object Reconcile, Reconcile with Source and Reset are greyed; Suspend and Resume
  each show where the other does not. All are mutating; the confirmation names whose rights the
  controller uses, not the user's ([H-64](../security/privilege-footprint.md#h-64-a-flux-action-is-carried-out-with-the-controllers-identity)).
- **Reconcile with Source** asks first for what `flux reconcile … --with-source` asks: a
  Kustomization's `spec.sourceRef`; a Helm release's `spec.chartRef`, else the HelmChart made of its
  `spec.chart`; a HelmChart's own source before it. Every object of the chain is checked before the
  first patch, so a refusal leaves nothing half done. A background job with a dock tab
  (`DockTab.Job`) asks the deepest source first and waits through the cache until
  `status.lastHandledReconcileAt` answers and the object is settled and Ready, at most 5 minutes
  per object; a failed source stops what depends on it, and closing the tab stops the job. No
  journal: a crash leaves at most sources asked to reconcile, which is harmless (D-11).

**Why:** these are the flux CLI's everyday commands; its patches are what the controllers and the
CLI agree on, touch no other field, need only `patch` and replace nothing the controller would not
on its next interval. A selection serves "reconcile everything that failed" from the dashboard
(D-34).

**Rejected:** Reconcile only; Force as a menu entry; server-side apply; a destructive danger level;
annotating source and object at once (the flux CLI waits); waiting for the source only (the tab
would end without the object's verdict); checking each source in turn (a refusal would leave
sources annotated).

**Code:** [`commands/flux.go`](../../internal/engine/commands/flux.go), [`commands/fluxsourcejob.go`](../../internal/engine/commands/fluxsourcejob.go),
[`engine/fluxwait.go`](../../internal/engine/fluxwait.go) · **Tests:** `TestFluxActionsPatch`,
`TestFluxReconcileWithSource`, `TestFluxReset`, `TestFluxReconcileWithSourceWaitsForTheSource`,
`TestFluxConfirmationNamesTheIdentity`, `TestKindFlux`

## D-34: The Flux dashboard

**Status:** built · **By:** owner

The Flux section's Dashboard shows a tile per Flux kind the cluster serves, its objects counted by
state, and a table "Not Ready" of every Flux object that is not ready, the latest change of its
Ready condition first; its selection takes the Flux actions (D-33). A click on a state opens the
kind's view filtered to that state.

- **A Flux object's state** follows kstatus as Flux documents it: Suspended (`spec.suspend`)
  first, then Not Ready (Ready False or Stalled True), then Reconciling (Reconciling True, Ready
  Unknown, or a generation not yet observed), then Ready (Ready True), else Unknown. An object that
  reports no conditions by design (notification v1beta3's Alerts and Providers, an OCI
  HelmRepository) counts as Ready.
- **Every view has a status filter**, a tab's own state (`UIState.States`, `ui.setStatusFilter`;
  `ui.openView` and `resources.list` take `states`), applied to the snapshot like the search, so the
  table, `view.rows`, select all and the CSV export agree. A row's states are the texts of its
  kind's state columns (`KindProfile.States`, else `model.DefaultStateColumns`: Status, State,
  Phase, Sync and Health whole, Conditions word by word), else its status tokens by name. With
  several state columns each state is qualified ("Sync: OutOfSync"): AND between columns, OR
  within one.
- The filter belongs to its view and tab and survives a restart with the tab (D-41); it lists the
  states the rows carry with counts and colours, is drawn in the accent colour while set, and holds
  at most 100 states of 512 bytes. Filtering costs no request. Lens's views have no status filter.

**Why:** the dashboard exists for "reconcile everything that failed". The Ready condition alone
would show a suspended or freshly changed object as ready; qualified states tell an Argo CD
Application's Sync from its Health.

**Rejected:**
- Counts only, or a table of every Flux object; a click opening the view unfiltered or with the
  search set to the state's word.
- A row's states from status tokens only — Running and Ready merge; from the Status column only —
  Conditions, Sync and Health are left out; one list of texts across columns — both of an Argo CD
  Application's columns have Unknown.

**Code:** [`engine/fluxdashboard.go`](../../internal/engine/fluxdashboard.go), [`model/states.go`](../../internal/model/states.go),
[`engine/statusfilter.go`](../../internal/engine/statusfilter.go) · **Tests:** `TestFluxDashboardPage`,
`TestFluxStatusTexts`, `TestStateMatcher`, `TestStatusFilterSnapshot`,
`TestWorkspaceKeepsTheStatusFilter`, `BenchmarkSnapshot10kStates`

## D-35: What Argo CD's repository and cluster Secrets show

**Status:** built · **By:** owner

Argo CD keeps its repositories, credential templates and clusters as Secrets labelled
`argocd.argoproj.io/secret-type` (`repository`, `repo-creds`, `cluster`). Settings › Repositories
and Settings › Clusters (Lens's columns: Name, Type, URL, Project, Age; Name, Server, Age) show the
window and agents alike only an allowlist of configuration keys: a repository's `name`, `url`,
`type`, `project`; a cluster's `name`, `server`, `namespaces`, `clusterResources`, `project`
(`kinds.ArgoRepositoryKeys`, `kinds.ArgoClusterKeys`).

- The views are virtual kinds whose objects `argoDerive` builds from each labelled Secret: name,
  namespace, UID, resourceVersion, creation time, labels, the Argo CD secret type, the allowed
  values (decoded, at most 2 KiB, control characters dropped) and the other keys' names. No other
  value and no annotation (kubectl's last-applied one repeats the values) is copied, so no row,
  drawer, YAML, `object_get`, search, CSV, audit or log line can carry one.
- User information in `url` or `server` becomes `<redacted>` (`kinds.RedactURLUserinfo`), except an
  SSH user without a password (`ssh://git@host`, `git@host:org/repo`).
- The other keys are listed by name ("Values Not Shown"); a repository's Authentication names the
  method its key names imply (SSH key, username and password, GitHub App, …) without reading a
  value. The drawer links to the Secret, whose drawer masks its values as every Secret's does.
- The cache holds these Secrets whole while a settings view, its drawer or an agent's read uses
  them, as it holds every Secret it watches
  ([H-66](../security/credentials.md#h-66-argo-cds-credentials-are-in-cogpits-memory-while-its-settings-are-read));
  the filter of cached Secret values in what agents read
  ([D-44](#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs)) uses their values.

**Why:** the allowed values are configuration that Applications and kubeconfigs carry in the clear
anyway, and with them an agent can tell which repository an Application pulls from; every
credential stays where the Secrets view keeps it.

**Rejected:**
- Names only — not Lens's views: no URL, type, project or server.
- The allowlist for the window, nothing for agents — agents would lose what the Applications'
  specs already tell them.
- Parsing a cluster's `config` for its authentication method — it would read the credentials.

**Code:** [`kinds/argocd_settings.go`](../../internal/model/kinds/argocd_settings.go) · 
`TestArgoSettingsNeverShowCredentials`, `TestRedactURLUserinfo`, `TestArgoSettingsNeverReachAgents`,
`TestKindArgoCD`

## D-36: How Argo CD's Sync writes the operation

**Status:** built · **By:** owner

Cogpit talks to the Kubernetes API only, never to Argo CD's API server. Sync
(`argocd.application.sync`, `argocd.application.syncMany`) writes the Application's `operation`
as `argocd app sync` without flags does, and Argo CD's controller runs it: each source's target
revision, a copy of the confirmed `source` or `sources`, `prune` as asked, the spec's sync options
and retry, and `initiatedBy: cogpit` (`kinds.ArgoSyncOperation`).

- The merge patch is bound to the UID and the resourceVersion Cogpit checked; on a 409 Cogpit waits
  up to 3 s for its cache to catch up, checks again and resends, at most three times in all.
  Refresh sets `argocd.argoproj.io/refresh`, which the controller removes, bound to the UID only.
- Refused, and greyed with the reason, while an operation is requested (`operation` set), running
  or terminating; a selection skips and reports such an Application. Sync with prune asks twice in
  the window: the Sync dialog, then the destructive confirmation naming what Argo CD would prune
  (Lens asks once).
- **Sync windows** block as Argo CD blocks a manual sync: an open deny window blocks unless every
  open deny window allows manual syncs; else an open allow window lets it through; else matching
  allow windows, all closed, block unless every one allows manual syncs. Matching uses Argo CD's
  glob, whose `*` matches `/` (`kinds.ArgoGlobMatch`), and `andOperator`; schedules are five cron
  fields in the window's time zone. A window Cogpit cannot read (`@daily` included) blocks; a
  project it may not read blocks nothing. The project is looked up in `status.controllerNamespace`,
  else in the Application's namespace.
- Argo CD's RBAC is not emulated: the user needs `patch` on the Application and the sync runs with
  Argo CD's rights ([H-67](../security/privilege-footprint.md#h-67-a-sync-acts-with-argo-cds-rights-not-the-users)).
  Cogpit neither clears the last `operationState` nor resolves a branch to a commit; the controller
  does both.

**Why:** it is the sync the CLI and Argo CD's UI start, without a second credential and endpoint
per cluster; the resourceVersion binding never replaces an operation Cogpit has not seen. Going
around Argo CD's API server skips its sync-window check, so Cogpit checks the windows itself.

**Rejected:** the minimal operation bound to the UID only (drops sync options and retry, and can
replace an operation started meanwhile); Argo CD's API server (a second credential and endpoint
per cluster).

**Code:** [`kinds/argocd_ops.go`](../../internal/model/kinds/argocd_ops.go), [`kinds/argocd_syncwindow.go`](../../internal/model/kinds/argocd_syncwindow.go),
[`commands/argocd.go`](../../internal/engine/commands/argocd.go) · **Tests:** `TestArgoSyncOperation`,
`TestArgoSyncConflicts`, `TestArgoSyncRefusedWhileAnOperationRuns`, `TestArgoGlobMatch`,
`TestArgoSyncRespectsSyncWindows`, `TestKindArgoCD`; `TestRealArgoCD` against a live Argo CD, never
in CI ([testing](testing.md))

## D-37: The node shell

**Status:** built · **By:** owner

`node.shell` opens a root shell on a node as Lens does: a privileged pod in `kube-system` on the
node, sharing the host's PID namespace, whose shell enters PID 1's namespaces with `nsenter` and
runs the node's bash, else its sh. It is `Destructive`: every origin, the GUI included, confirms it,
and the start is bound to what the confirmation named; access is checked in `kube-system`
(`Verb.Namespace`). The pod, its end and what `kube-system` must allow:
[privilege footprint › Node shell](../security/privilege-footprint.md#node-shell).

- **Image:** a cluster setting with a pull secret, set only by the user
  ([D-38](#d-38-who-sets-the-node-shells-image)). The default, `DefaultNodeShellImage`, is the
  newest supported Alpine release pinned by its index digest; Renovate's minor and digest updates
  merge once CI's kind job has opened a node shell with them
  ([H-72](../security/privilege-footprint.md#h-72-the-default-node-shell-image-ages-with-the-build-that-pins-it)).
  A floating tag is accepted, as Lens accepts any image, and named as one in the confirmation and
  under the field, which shows the short name
  ([D-102](#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)).
- **Maximum duration:** a cluster setting, 2 hours by default, 5 minutes to 24 hours typed as
  `90m`, `2h` or `1h30m` (a kept value out of bounds reads as the default): the pod's
  `activeDeadlineSeconds` and `sleep`, so it ends even if Cogpit dies.
- **Crash cleanup:** each pod is recorded in `nodeshells.json` before its create is sent; the next
  connect to its context deletes, by UID, what a crash left
  ([H-71](../security/privilege-footprint.md#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)).
  An interruption keeps the pod for the tab's Reconnect ([D-58](#d-58-standby-and-lost-connections)).

**Why:** Lens parity, bounded by a confirmation per start and a capped pod life. A pinned, supported
Alpine keeps a tag from changing what runs as root (Lens's tag `alpine:3.19` is out of support).
Five minutes allow the image pull; a day covers a long session, never an idle root pod for days.

**Rejected:**
- No node shell — loses parity.
- A `kubectl debug node/…`-style pod without the privileged flag as the default — not Lens's;
  possible later as a second profile ([H-7](../security/privilege-footprint.md#h-7-node-shell-is-root-on-the-node)).
- Recording the pod only after its create — a crash between the two leaves it unrecorded.

**Code:** [`engine/nodeshell.go`](../../internal/engine/nodeshell.go),
[`engine/nodeshellsweep.go`](../../internal/engine/nodeshellsweep.go) · 
`TestNodeShellBoundToWhatWasConfirmed`, `TestNodeShellSweepDeletesOwnPodsOnly`, `TestKindNodeShell`

## D-38: Who sets the node shell's image

**Status:** built · **By:** owner

Only the user, in Cogpit's window, sets the node shell's image, image pull secret and maximum
duration; agents and the CLI may only reset them to the defaults. `cluster.settings.setNodeShell`
refuses any other value from every origin but the GUI with `ErrNodeShellGUIOnly`, before anything
is written.

**Why:** the image is what runs as root on the node at the user's next node shell, and the maximum
duration how long that root pod lives. An agent that could set the image and then ask for a node
shell would leave the user one careful reading of the confirmation away from running the agent's
image privileged.

**Rejected:**
- Agents set them like the other cluster settings — the risk above.
- No setting, the pinned default only — Lens has the setting.

**Code:** [`commands/nodeshell.go`](../../internal/engine/commands/nodeshell.go)
`refuseNodeShellSettings` · **Tests:** `TestNodeShellSettings`, `TestNodeShellMaxDurationSetting`

## D-39: Pod metrics for a user who may list pods in some namespaces only

**Status:** built · **By:** owner

The pods' metrics are listed as the Pods view lists the pods (`usageListScopes`): one
`list pods.metrics.k8s.io` in all namespaces for a user who may list pods there, else one per
namespace the view's plan reads (`nsPlan`: the accessible namespaces, the namespace filter's, else
the context's own, less those whose rules deny pods), none while the plan is pending. One poll per
scope serves the Pods list, a node's Pods table and the drawers while they show; `metrics.get` of
every pod reads the same scopes, at most four at once.

A 403 for one namespace's pod metrics affects only that namespace (`namespaceRefusal`): its pods
get empty cells and the state `forbidden` naming the namespace and the API server's reason; the
other namespaces keep their values, the Metrics Server's status stays, and Prometheus does not
stand in. A pod's drawer there shows no live values but Prometheus's history. The refusal is cached
for the TTL like values, and `metrics.get` names such namespaces in `forbidden`. A refusal of every
pod or of the nodes, a 401 and a Prometheus refusal stay the provider's.

Prometheus is not limited by namespace: Cogpit's queries name the plan's namespaces, but they pass
the service proxy, which RBAC grants or not as a whole. The Overview's Pods history counts the whole
cluster for every user ([`promclusterpods.go`](../../internal/metrics/promclusterpods.go)), so for a
namespace-limited user the chart drops where the cache's counts, which hold only the plan's
namespaces, take over. Not built: continuing that history from kube-state-metrics for such a user
(one range query per step while the page is open), which would remove the drop.

**Why:** one request per namespace and poll instead of one per pod (300 pods: 300 requests a
minute), the columns fill for every row the user sees, and a user who may list pods everywhere
keeps the one list (budget P12). RBAC grants the Metrics API per namespace apart from the pods, so
one namespace's 403 says nothing about the provider.

**Rejected:**
- The one list in all namespaces for everyone — RBAC refuses it to such a user.
- A `get` of each visible pod's metrics per poll — one request per pod.
- The Overview's Pods history by the plan's namespaces — it sets the user's pods against node
  capacity that pods they cannot see share.

**Code:** [`engine/metrics.go`](../../internal/engine/metrics.go) `usageListScopes`,
[`metrics/hub.go`](../../internal/metrics/hub.go) `namespaceRefusal` · 
`TestPodMetricsPerNamespaceOfThePlan`, `TestPodMetricsRefusalStaysInItsNamespace`,
`TestBudgetNamespaceLimitedUser`

## D-40: A Pods table in the workloads' drawers

**Status:** built · **By:** owner

The drawers of Deployments, Replica Sets, Stateful Sets, Daemon Sets, Jobs and Cron Jobs show the
node drawer's sortable Pods table after the kind's own sections, before the Events: Name, the
warning icon, Node (a link), Ready, CPU, Memory, Status, Age. A workload's pods share its
namespace, so its table has no Namespace column; the node drawer's has Namespace and no Age. One
builder, one set of column ids and one sort state ("Pods") serve both. A workload without pods, or
whose pods are still listing, has no Pods section.

- **Which pods:** those whose controller reference names the workload's UID in its namespace
  (`controllerUID`, as Kubernetes's garbage collector reads owner references); a Deployment's
  through every replica set it controls, old ones during a rollout included; a Cron Job's through
  its jobs. Replication Controllers have none, as `related: true` leaves them out.
- **Age:** the cell holds the creation time, and `engine.BuildDetailIn` writes its text against the
  engine's clock (`model.WriteAges`), so agents read the same text in `object_get`; the drawer is
  rebuilt on every metrics poll and pod change.
- **Indexes:** the pods-by-owner indexes are the drawer's (`KindProfile.DetailNeeds`), held only
  while it is open, together with a watch on the pods and the replica sets or jobs.
- **Bound:** at most 500 rows (`engine.MaxDrawerTableRows`) in any sortable drawer table, cut after
  the sort, so only a large workload's table is cut (a node runs 110 pods by default); the window
  says "N more pods not shown". `related: true` lists the table's pods first, in its order, the cut
  ones after the replica sets or jobs.

**Why:** what a Lens user expects from a workload's details (inferred: Lens's workload drawers are
not captured); the node drawer's table, metrics and sorting existed, so the cost is an index only
an open drawer holds.

**Rejected:**
- The indexes in the profiles' `Needs` — every workload list rebuilds its rows on every pod change
  (eleven Deployments snapshots for ten pod changes and one Deployment change, against one).
- The window writing the age when it draws the cell — agents would read another text.

**Code:** [`kinds/workloadpods.go`](../../internal/model/kinds/workloadpods.go),
[`kinds/nodes.go`](../../internal/model/kinds/nodes.go) `workloadPodColumns`,
[`engine/drawerlimit.go`](../../internal/engine/drawerlimit.go) · 
`TestWorkloadDrawerPodsPerKind`, `TestWorkloadPodsIndexOnlyWithTheDrawer`,
`TestDeploymentDrawerPodsTable`, `TestKindWorkloadDrawerPods`

## D-41: What survives a restart

**Status:** built · **By:** owner

`<user config dir>/Cogpit/workspace.json` (0600 in a 0700 directory, replaced atomically) keeps
the window's arrangement and the last session, written 1 s after they stop changing (at least every
5 s while they keep changing) and at quit, read once at start.

- **Restored at every start:** the tables' hidden columns and column order per view key, the
  navigator's collapsed source groups ([D-51](#d-51-the-navigators-and-the-command-palettes-state)),
  the navigator hidden or shown, the chat column; the window's place and size come from
  `window.json` ([D-95](#d-95-the-window-comes-back-where-it-was)).
- **Only with Preferences › App › Restore the last session** (off by default, turned on only by the
  user, [D-69](#d-69-several-clusters-connected-at-once) item 8): the tabs, and the clusters
  connected at quit, the one shown then first. The file keeps `connected` and `shown` either way.
- **A tab keeps:** its cluster, view and title, preview or fixed, its place in the bar, when it was
  last shown, its namespace and API group filters, its search and toggles, its status filter, its
  sort and its history (view keys only, [D-129](#d-129-back-brings-the-place-back)).
- **Never kept:** a selection or the drawer's object, the dock's tabs (an editor's unsaved text has
  its draft, [D-11](#d-11-crash-stance)), column widths.
- **Tabs of contexts that are gone** are dropped with a notice, only on a plain fact
  (`goneContexts`): the file or its source is gone, or the context is no longer defined or offered.
- **A file read back** keeps at most 200 tabs, 1,000 entries a list, keys of 512 bytes, a search of
  1,000 bytes and titles of 256 bytes; one that does not load is moved to
  `workspace.json.unreadable` before the first write. Of two Cogpit processes the last write wins.

**Why:** what the user arranged survives, and nothing reaches a cluster at start unless the user
turned the restore on. Column widths wait until a drag's end is its own event: the table records a
width whenever a header's laid-out width differs from the one it set, which a relayout does too.

**Rejected:**
- A tab's selection and drawer — they name objects that may be gone, and a restored drawer reads
  an object the user did not ask for.
- The dock's tabs — a stream or a terminal would open at start.
- The column layout per cluster — a view's columns are its kind's, the same on every cluster.

**Code:** [`engine/persist.go`](../../internal/engine/persist.go),
[`engine/restore.go`](../../internal/engine/restore.go) · **Tests:** `TestWorkspaceRoundTrip`,
`TestWorkspaceDropsOnlyWhatIsCertainlyGone`, `TestWorkspaceUnreadableFileIsKeptAside`,
`TestRestoreOffStartsWithoutTabsAndConnections`, `TestRestoreOnConnectsTheLastSession`

## D-42: The start screen

**Status:** built · **By:** owner

Cogpit starts on the welcome page with its cluster groups, the navigator showing its clusters, and
nothing opens over it. Without Preferences › App › Restore the last session nothing connects by
itself; with it ([D-41](#d-41-what-survives-a-restart)), the last run's tabs come back and the
cluster shown at quit connects and shows as on any connect, with its most recently used tab.

- `--context` connects its cluster and shows it; with the restore on, the other clusters connect
  behind it. A `--context` the engine refuses says why in a toast and shows the navigator's
  clusters with the search focused, where a login not allowed yet can be allowed
  ([D-57](#d-57-choosing-a-cluster)); a connect that fails says why on the welcome page.
- `--demo` exists in development builds only ([D-67](#d-67-the-demo-cluster-stays-out-of-releases)).
- Notices the engine gave before the window was up (tabs not restored, an unreadable
  `workspace.json`) show as toasts once it is up, with the crash notice.

**Why:** the welcome page offers every cluster with one click; Lens starts on its own page
(inferred: its welcome page is not captured).

**Rejected:**
- A context picker opening at start whenever nothing is connected — it hid the welcome page's
  cluster rows; Cogpit has no picker ([D-57](#d-57-choosing-a-cluster)).
- The picker only when no tab was restored — the first screen would depend on a file the user does
  not see.

**Code:** [`fyneui/shell.go`](../../internal/ui/fyneui/shell.go) `start`, `autoConnected`,
`showStartNotices` · **Tests:** `TestStartShowsRestoredTabsWithoutConnecting`,
`TestStartWithoutTheRestoreShowsNoTabs`, `TestRestoreBehindAnExplicitContext`

## D-43: Where the nodes' disk comes from

**Status:** built · **By:** owner

A node's disk comes from node-exporter's filesystem gauges through the cluster's Prometheus: the
Nodes list's Disk column shows the fullest real filesystem, a node's drawer a bar per real
filesystem ([D-90](#d-90-a-nodes-disks-a-usage-bar-per-real-filesystem)). Without Prometheus and
node-exporter the column stays empty and its tooltip says why. Used is the size less the available
bytes (`df`'s Avail), so 100% is where workloads can no longer write. A series names its node by
the node label, else by its instance's host as the node's name or one of its addresses
(kube-prometheus-stack's node-exporter carries only its host-network address). One instant query
per poll (60 s) serves every node, only while the Disk column or a node's drawer shows the disk.

A node's charts (its drawer, the cluster Overview) prefer node-exporter too, else cAdvisor's root
cgroup, which kube-prometheus-stack drops; network and filesystem join both by `or` in one query
each. Memory is `MemTotal − MemFree − Inactive(file)`, the kubelet's working set on cgroup v2, so
history and live values mean the same (inferred from the kubelet's accounting, not measured). The
network leaves out the loopback and the virtual interfaces of runtimes, network plugins and the
kernel. Queries, matching, budget: [architecture §9](architecture.md#9-metrics-internalmetrics).
The lists' fallback while the Metrics Server fails reads the root cgroup, so on
kube-prometheus-stack the Nodes list then has no CPU and Memory.

**Why:** the Metrics Server measures no disk.

**Rejected:**
- The kubelet's summary API (`/api/v1/nodes/<node>/proxy/stats/summary`) — it needs
  `get nodes/proxy`, which reaches the kubelet's whole API, running commands in the node's pods
  included: a grant Cogpit must not ask for a column.
- cAdvisor's root-cgroup filesystem series — they sum every block device, not the filesystem a
  Disk bar means, and kube-prometheus-stack drops them.
- `MemTotal − MemAvailable` — counts active file cache as free; the chart steps at the live values.
- Every interface but the loopback — counts the pods' traffic several times (34 interfaces on one
  node with Cilium and WireGuard).
- Network and filesystem tried source by source like CPU and memory — they are asked at every
  step, so the discovery would cost queries the window shows nothing of.

**Code:** [`metrics/disk.go`](../../internal/metrics/disk.go),
[`metrics/promrange.go`](../../internal/metrics/promrange.go) `nodePlan` · 
`TestNodeDisksFromNodeExporter`, `TestKindNodeExporterByAddress`,
`TestNodeHistoryOnKubePrometheusStack`, `TestNodeNetworkAndFilesystemFromNodeExporter`

## D-44: What agents read of the dock's logs and terminal tabs

**Status:** built · **By:** owner

Agents and the CLI read a logs tab's lines freely (`dock_read`), a terminal tab's (a local
terminal, a pod shell, attach, a node shell; scrollback too) only while its "Agents may read"
switch is on. Only the window sets it (`dock.allowAgentRead`); it is off whenever a tab opens, its
marker flashes at each read it allows, and it turns itself off after 15 minutes without one, at the
latest 1 hour after it went on. The built-in agent needs it too, over a private connection as well
([D-10](#d-10-built-in-agent)). In what an agent reads of dock tabs, objects, drawers and editors,
the values of the Secrets Cogpit has cached, from 10 bytes, become `«secret <ns>/<name>/<key>»`,
stored (base64), decoded and printed forms alike; the window and the CLI get the text as it is. A
chart's README reaches agents framed as its author's text, a window-only notification through the
same filter. Rules and gaps: [agents and MCP](../security/agents-and-mcp.md#rules), H-82, H-83.

- **Name keys are left out** (a user, a host, a database, by the key's last word): their values
  stand in every line about the application, and replacing them caught no leak more.
- **The Secrets are taken for the read:** as the Secrets view reads them, waited for at most 10 s;
  a read still without them is refused, not sent unfiltered (`ErrAgentSecretsPending`).
- **No write-back:** an agent's text carrying a placeholder is refused (`ErrAgentMarkerWrite`).
- **No dialog for the switch** (it exposes one tab, not every write): turning it on is ignored for
  the confirmations' 750 ms after the strip began to show another tab, which an agent can activate.

**Why:** a logs tab shows cluster data the user's access governs, an agent's to read like events
and YAML ([D-2](#d-2-agent-interface-scope)); a terminal is the user's own workspace, whose output,
credentials included, no rule of Cogpit's can see. A copy of a Secret's manifest carries the
stored form; the 10-byte minimum keeps common words whole (H-82's measurement).

**Rejected:**
- Terminal output read freely — it would reach every agent and the built-in agent's endpoint.
- Never terminal output — loses "read the error in my terminal", leaving copy and paste.
- Only the Secrets the cache holds anyway — nothing is replaced until the Secrets view opened;
  watching them whenever the MCP server runs — held without any agent read.
- Putting the values back into an agent's write — it could place a value it never read.

**Code:** [`engine/dockoutput.go`](../../internal/engine/dockoutput.go),
[`engine/secretscrub.go`](../../internal/engine/secretscrub.go),
[`engine/agentsecrets.go`](../../internal/engine/agentsecrets.go) · 
`TestDockReadTerminalNeedsTheUsersSwitch`, `TestAgentReadSwitchEndsAfterAnHour`,
`TestAgentViewReplacesCachedSecretValues`, `TestSecretScrubMeasure`

## D-46: Chart icons from the hosts that serve them

**Status:** built · **By:** owner

The Charts view's first column and a chart's drawer header show the icon a repository's index
names, fetched from its URL when the window draws it; Cogpit's helm-wheel glyph shows while it
loads, when there is none or it failed, and while Preferences › Kubernetes › Load chart icons (on
by default) is off. Agents and the CLI never fetch icons (`object_get` and the rows carry the URL)
and may only turn the switch off (`ErrChartIconsGUIOnly`).

- **How:** https only (a redirect too, at most 5), no credentials whatever `pass_credentials_all`
  says, no cookies or referrer, at most 256 KiB and 20 s, through the Preferences' proxy for
  non-cluster traffic; at most 4 downloads at once, one per URL.
- **Where to:** a host at a non-public address (loopback, private, link-local, carrier-grade NAT)
  is refused unless it is a chart repository's own host; each request dials the address it
  checked ([H-87](../security/trust-boundaries.md#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at)).
- **What:** checked as an uploaded cluster icon (`CheckIcon`): a PNG or JPEG decoded under bounds
  and kept as a PNG of at most 256 pixels a side; an SVG only as one `<svg>` document without a
  DOCTYPE, scripts, event handlers or outside references, rasterised by the window off the main
  goroutine, never by Fyne's renderer
  ([H-88](../security/trust-boundaries.md#h-88-chart-icons-are-decoded-in-cogpits-process));
  anything else refused.
- **Kept:** `<user cache dir>/Cogpit/helm/icons/<hash of the URL>.png` or `.svg`, owner-only, for 7
  days, the newest 2,048; 512 in memory; a failure is remembered for an hour.

**Why:** the icon is how a user finds a chart in a long list, and Lens shows icons (inferred: not
captured). The cost is a request per icon shown, to a host the chart's author chose, carrying
nothing but the URL; the switch turns it off.

**Rejected:**
- No icons — Lens's look not met.
- Icons only from the repository's own host — most public repositories name icons elsewhere, so
  most charts would show none, for a privacy gain only against hosts the chart's author chose.

**Code:** [`helm/icon.go`](../../internal/helm/icon.go),
[`engine/helmicons.go`](../../internal/engine/helmicons.go),
[`fyneui/helmcharticons.go`](../../internal/ui/fyneui/helmcharticons.go) · 
`TestFetchIconRefusesPrivateAddresses`, `TestChartIconRefusals`, `TestChartIconsPreference`,
`TestChartIconsInTheWindow`

## D-47: Deep links (`cogpit://`)

**Status:** built · **By:** owner

The macOS app registers the scheme `cogpit` (`CFBundleURLTypes`, the [`Makefile`](../../Makefile)'s
`app` target), so any page, mail or app can hand Cogpit a link: untrusted input that only
navigates (connect a context of the user's kubeconfig sources, open a view, set the namespace
filter, open a drawer; [usage › Deep links](usage.md#deep-links-cogpit)), parsed strictly and
refused whole otherwise ([trust boundaries](../security/trust-boundaries.md#boundaries-and-required-checks)).

- **Origin `deeplink`:** `Dispatch` refuses it every command but `DeepLinkCommands`
  (`settings.close`, `cluster.connect`, `ui.setNamespace`, `ui.openView`, `ui.navigate`) and any
  invocation that is not a UI or read command, before decoding and again after `Escalate`, so no
  write is reachable whatever `OpenDeepLink` dispatches.
- **A connect asks:** a link that would connect or show another cluster than the shown one, or
  arrives while none is connected, asks once, naming the context and what it opens
  (`askLinkConnect`); navigation within the shown cluster is immediate.
- **Contexts:** a kubeconfig source's context or a development build's demo cluster, never one a
  connector without sources offers, never a file path; a login that waits for the user's approval
  stops the link.
- **Seen:** the window comes to the front, a toast says what the link opened or why not, and the
  audit log records `deeplink.open` with origin `deeplink`.
- **Windows and Linux** register no scheme: a link there starts a second process, which needs a
  hand-over to the running Cogpit that does not exist (G22).

**Why:** links serve runbooks, chat messages and alerts that point at an object, at the cost of
nothing the user's identity can write. Asking before every connect turns a page that opens links
in a loop into questions, not connections
([H-89](../security/trust-boundaries.md#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)).

**Rejected:**
- Links that run commands with a confirmation — every web page becomes a prompt the user must read
  with care before a click acts with their identity (consent phishing).
- No deep links — loses Lens's `lens://` parity.
- The login approval dialog raised by a link — a page could raise it at a moment of its choosing.

**Code:** [`engine/deeplink.go`](../../internal/engine/deeplink.go),
[`cmd/cogpit/deeplink_darwin.m`](../../cmd/cogpit/deeplink_darwin.m) · 
`TestParseDeepLinkRefusals`, `FuzzParseDeepLink`, `TestDeepLinkCommandsOnlyNavigate`,
`TestDeepLinkGuardsAnAlteredCommand`, `TestDeepLinkAsksBeforeItConnects`, `TestDeepLinkQueue`

## D-48: Start at login

**Status:** built · **By:** owner

Preferences › App › "Start Cogpit at login" registers the macOS app's own login item
(`SMAppService.mainAppService`, macOS 13 and later), which System Settings › General › Login Items
lists and switches too. Cogpit keeps no copy: the row shows what macOS reports (`UIState.LoginItem`:
off, on, waiting for approval with a button to Login Items, not found), read when the section opens
and after every change. Off by default; `preferences.setStartAtLogin` turns it on for the GUI only,
agents and the CLI may turn it off; `preferences.openLoginItems` is GUI-only. A Cogpit that does
not run from its app bundle (`bin/cogpit`, `go run`) reports not found without asking macOS.
Windows and Linux show no row (G22) rather than a switch that does nothing.

**Why:** with it on, Cogpit runs after every login without being opened, its MCP server included
once the user switched that on
([D-49](#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)), so turning it on is the
user's own act. Lens has the preference ("Automatic start-up", inferred from its documentation).

**Rejected:**
- A LaunchAgent property list in `~/Library/LaunchAgents` — a file outside Cogpit's folders, shown
  as a background item, and not Apple's way since macOS 13.
- A preference in `preferences.json` applied at every start — it would undo the user's switch in
  System Settings.

**Not verified:** what macOS does with the signed release app or an ad-hoc signed local build
(`SMAppService` requires a code-signed app); the row shows macOS's status or its error.

**Code:** [`engine/loginitem.go`](../../internal/engine/loginitem.go),
[`commands/loginitem.go`](../../internal/engine/commands/loginitem.go),
[`cmd/cogpit/loginitem_darwin.m`](../../cmd/cogpit/loginitem_darwin.m) · 
`TestStartAtLogin`, `TestStartAtLoginOutcomes`, `TestLoginItemOutsideTheAppBundle`

## D-49: The external MCP server's switch, honoured by `cogpit-cli mcp`

**Status:** built · **By:** owner

The external MCP server is opt-in, and the status bar's MCP item is its one switch for every way an
outside agent reaches Cogpit: the app's loopback endpoint and `cogpit-cli mcp`, forwarding or
headless.

- The choice is the file `mcp-switch` (`on` or `off`) beside `mcp-token`, owner-only, written
  atomically and only by the window (the item and the MCP details' switch; one audit line,
  `gui.mcpServer`, per click). No file, an unreadable file, a symlink or other content is off. A
  stored Fyne preference `mcp.enabled` is copied into a missing file once at launch.
- While off, `cogpit-cli mcp` serves no engine, only an MCP server whose instructions and one tool
  (`cogpit_mcp_off`, an error) say that the server is off and opt-in (`SwitchedOffMessage`); an
  unknown configuration directory counts as off. A forwarding bridge fails its next call once the
  user switches off and never falls back to a headless engine.
- `--no-mcp` leaves the server out of one run and the stored switch as it is. No `cogpit-cli`
  command switches the server on (an agent could run it); without a window the user writes `on`
  into the file ([usage](usage.md#cogpit-cli)). The built-in agent needs no switch
  ([D-10](#d-10-built-in-agent)).

**Why:** "Off" must mean off for every agent host, the one that starts the bridge included, and a
server for agents, which reads clusters with the user's identity, is opt-in. One function
(`mcp.ReadSwitch`) decides for the window and the bridge. Closes H-28
([agents and MCP](../security/agents-and-mcp.md)).

**Rejected:**
- The switch stopping the app's endpoint only — a bridge an agent host starts still reaches the
  clusters.
- Fyne's preferences or the engine's `preferences.json` as the store — `cogpit-cli` has no Fyne,
  and `preferences.set` would let agents switch the server.
- Exiting with an error on stderr — an agent host shows only "server failed".

**Code:** [`mcp/switch.go`](../../internal/mcp/switch.go),
[`cogpit-cli/mcp.go`](../../cmd/cogpit-cli/mcp.go), [`fyneui/mcp.go`](../../internal/ui/fyneui/mcp.go)
`storedMCP` · **Tests:** `TestSwitchFile`, `TestServeSwitchedOff`, `TestMCPSwitchedOffRefuses`,
`TestMCPFreshInstallationIsOff`, `TestMCPLegacyPreference`

## D-50: kubectl's signature check

**Status:** built · **By:** owner

A kubectl downloaded for a terminal ([D-3](#d-3-helm-and-kubectl-binaries)) is installed only when
sigstore-go verifies its signature, Rekor's transparency-log proof included, against the trusted
root Cogpit embeds. The steps:
[architecture › kubectl in terminals](architecture.md#kubectl-in-terminals); what the check rests
on: [trust boundaries › H-35](../security/trust-boundaries.md).

- The log entry is found by its content (`POST /api/v1/log/entries/retrieve` on
  `rekor.sigstore.dev`: the hashedrekord of the published digest, signature and certificate), as
  `cosign verify-blob` searches; `dl.k8s.io` publishes no sigstore bundle.
- The trusted root is embedded
  ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json)); a developer
  refreshes it by hand with `make sigstore-root` (through sigstore's TUF repository; Renovate cannot
  see the file) before a release and when a download says the root does not know a certificate or
  a log.
- No fallback: a log that does not answer or answers 429 or 5xx (`ErrLogUnreachable`), has no
  entry (`ErrNotLogged`) or holds one that does not verify refuses the download. Without access to
  the log, the user puts a kubectl on `PATH` or sets the path to kubectl.
- Required: one signed certificate timestamp of a CT log the trusted root names, and the inclusion
  proof besides the signed entry timestamp. The offline check (`ParseSigned`) runs first, so a
  signature that does not fit the digest is refused before any request.
- Bounds: 30 s, 64 KiB and four entries per answer; the log's reason at most 200 bytes and
  sigstore-go's 300, each on one line. The log's URL is the constant `DefaultRekor`, no preference
  (the embedded root knows no private sigstore); tests set `Downloader.Rekor`.
- sigstore-go updates like any Go module, gated by the offline tests with real releases' files.

**Why:** A check without the log accepts a signature made with a leaked key or under a certificate
Fulcio issued by mistake; sigstore-go is sigstore's own verification, the log included.

**Rejected:**
- Go's standard library — it cannot consult the transparency log.
- A search by digest, then a request per entry — more requests, and anyone can log entries under
  kubectl's digest.
- The trusted root from TUF at run time — a second host and a cache on the user's disk. The price
  of embedding: after a sigstore key rotation, new releases download only once a Cogpit release
  carries the new root (fails closed).

**Cost:** sigstore-go's dependency tree (Rekor, TUF, protobuf, gRPC, OpenTelemetry; no cloud SDK or
KMS client) in both binaries; one request to `rekor.sigstore.dev` per download.

**Code:** [`kubectl/verify.go`](../../internal/kubectl/verify.go),
[`kubectl/rekor.go`](../../internal/kubectl/rekor.go), the tests' own sigstore
[`testkit/sigstoretest`](../../internal/testkit/sigstoretest/sigstoretest.go) · 
`TestRealReleaseSignature`, `TestRealReleaseRefused`, `TestDownloadWithoutTheLog`,
`TestLiveRelease` (opt-in, `COGPIT_SIGSTORE_LIVE=1`)

## D-51: The navigator's and the command palette's state

**Status:** built · **By:** owner

The navigator's search, keyboard row and collapsed source groups (`UIState.Navigator`) and the
command palette's text with who typed it last (`PaletteText`, `PaletteTextOrigin`) are engine
state, set by the UI commands `ui.setNavigatorSearch`, `ui.setNavigatorRow`,
`ui.setNavigatorGroup` and `ui.palette.setText` ([D-2](#d-2-agent-interface-scope)).

- The window reports a change 150 ms after the last and shows the engine's value unless a report
  of its own is pending, so an older echo never overwrites the user's typing. An agent's value, or
  a palette an agent opens, never takes the keyboard focus.
- The row names a row of the level shown: `root`; at a cluster a view key, `group:<path>` or
  `alias:<kind key>`; at the section `context:<context id>` or `source:<label>`. A row the window
  does not show leaves the keyboard where it is. A change of level clears the search; every
  connect drills the navigator into its cluster as it starts, whoever starts it.
- Bounds: the search and the palette's text one line of at most 1,000 bytes; a row at most a
  context id's 4,096 bytes plus its prefix; at most 1,000 collapsed groups. One state per window,
  not per tab; `workspace.json` keeps the collapsed groups only
  ([D-41](#d-41-what-survives-a-restart)).
- On a palette text another origin typed last, ↵ runs only a read or a UI entry; a change runs
  only after the user picks it with an arrow key or a click, as the hint line says. Typing makes
  the text the user's.
- The focus ring shows on keyboard navigation only. A source group's heading collapses and opens
  with a click, ↵, Space, ← and →; a collapsed heading counts its clusters; a search opens every
  group.

**Why:** UI state is engine state (golden rule 2) without an exception. The ↵ rule keeps an agent
from choosing the change the user's next ↵ runs, while it can still show the user what to run.

**Rejected:**
- The row as a view key only — the keyboard stands on roots, groups and clusters as often.
- ↵ running nothing on an agent's text — an agent's "Switch to Pods" would take two steps.

**Code:** [`engine/navigator.go`](../../internal/engine/navigator.go),
[`engine/palettetext.go`](../../internal/engine/palettetext.go),
[`fyneui/palette.go`](../../internal/ui/fyneui/palette.go) `enter` · 
`TestNavigatorAndPaletteStateCommands`, `TestNavigatorReportsAndShowsItsState`,
`TestCommandPaletteAgentTextNeverRunsAChange`, `TestNavigatorSectionGroupsCollapse`

## D-52: Several windows

**Status:** open · **By:** owner

The requirement: dragging a hotbar tile out of the window opens another Cogpit window with that
cluster. Lens has several windows ([ui-parity](../parity/ui-parity.md)); the drag out is beyond
Lens ([beyond Lens](../parity/beyond-lens.md)). Not built: a tile dropped outside the hotbar moves
nothing ([`hotbardrag.go`](../../internal/ui/fyneui/hotbardrag.go)).

**Decided (the direction):** model (a), one process and one session with several windows, each
with its own window state and connection. `UIState` splits into the app's part (hotbar, stores,
notification log, columns) and a window's part (context and connection, view, filters, selection,
drawer, tabs, dock, navigator, palette, chat column); windows on one cluster share its backend in
the pool ([D-69](#d-69-several-clusters-connected-at-once)). Stages: the window's state in the
engine; `window.open {context?}`, `window.close`, `window.focus`, `ui_state`'s `windows`, a shell
per window and a Window menu; the drag; `workspace.json` with windows. Estimated 40–80 h.

**Open, each with the lead's recommendation:**

1. A connection per window (a tab of another cluster switches the window's cluster), not per tab.
2. An optional `window` on window-scoped commands, by default the window the user focused last.
3. Agents open and close windows as UI commands, never the last one.
4. A restart brings back every window with its tabs and frame, none connected.
5. A tile dropped beyond the window's edge by a margin (24 points) opens the window, at the
   pointer on macOS; "Open in New Window" in the tile's menu covers full screen and the keyboard.
6. A tile of a cluster another window shows opens a new window that shares the connection.
7. Which window a `cogpit://` link opens in ([D-47](#d-47-deep-links-cogpit)).

**Why (a):** one hotbar, one set of stores, one MCP server and one audit log for every window.

**Rejected:**
- A process per window (est. 20–40 h) — the processes share the configuration directory without
  coordination: the last writer wins for the workspace, hotbar, preferences, cluster settings and
  the chat's conversations, one start removes the other's crash report, each runs its own MCP
  endpoint and switch, and an agent sees two Cogpits.
- A new tab instead (est. 3–6 h) — not the requirement.

**Constraints:** Fyne 2.8.2 draws every window in one loop on the main goroutine, so opening a
window stalls every open one ([spike](spike-fyne.md)). `fyneui.Run`, the engine's confirmer and
dock reader, and `aboutShell` assume one window. `DragEnd` has no position, but `Dragged` reports
positions beyond the window while the button is held, so a drop outside is detectable; nothing
outside is a drop target. Fyne cannot read a window's position and `RequestPosition` may be
ignored (Wayland): the pointer's screen position is known on macOS only (`[NSEvent mouseLocation]`).

**Risks:** live today, Cogpit started twice (there is no single-instance guard) meets the shared
files of the process model. Under (a), an agent's write that names no context goes to the focused
window's cluster; its confirmation names the context, and `CheckConfirmedContext` refuses it on
another.

## D-53: The MCP details, and a token kept until the user regenerates it

**Status:** built · **By:** owner

- The gear beside the status bar's MCP item opens the MCP details above the status bar: the switch
  (D-49), the endpoint, the bearer token, the stdio and HTTP registrations with copy buttons, and
  Regenerate token. A click on the item itself still switches the server. Help › Copy MCP command
  copies the registration and opens the details; the top bar has no MCP button.
- The token (`mcp-token`) and the port (`mcp-port`, 0600, atomic, never through a symlink) stay
  across restarts and switches; `mcp-endpoint` exists only while the server listens. A token file
  without a token Cogpit wrote, or readable by other users, is replaced at the next start.
- When the kept port is taken at start, the server listens on another and keeps it; a notice names
  both. `--mcp-addr` with a port keeps nothing; the default `127.0.0.1:0` keeps the port.
- Regenerate token (the window's alone, also while the server is off) warns first, its button armed
  after the confirmations' delay. The running server keeps its port, refuses the old token at once
  and ends every request it admitted, a call waiting for a confirmation included; one audit line
  (`gui.mcpToken`), never the token.
- The token and the HTTP registration show masked until the user shows them and copy as a
  credential ([D-25](#d-25-copying-values)); the endpoint and the stdio registration copy as plain
  text. A note at the panel's foot confirms a copy (the panel covers the toasts' corner).
- `cogpit-cli mcp` reads the endpoint and token files before every call and reconnects when they
  changed, so it follows a restart, a new port and a new token and never sends the token to a port
  Cogpit left.

**Why:** A client registered once keeps working across restarts and switches; Regenerate token is
the one way to cut every client off. What a stable token costs:
[agents and MCP › H-98](../security/agents-and-mcp.md).

**Rejected:**
- A new token at every switch-on — every switch would break the registration.
- The endpoint file kept across stops — it would no longer say that Cogpit listens, and the bridge
  would send the token to whatever holds the port.
- Trying the old port again at the next start — the registration shown meanwhile breaks.
- Stopping and restarting the server to regenerate — a moment with nothing listening, in which
  another program can take the port.

**Code:** [`mcp/server.go`](../../internal/mcp/server.go) `RegenerateToken`, `keepPort`;
[`mcp/token.go`](../../internal/mcp/token.go) `loadToken`; [`mcp/bridge.go`](../../internal/mcp/bridge.go)
`followed`; [`fyneui/mcpdetails.go`](../../internal/ui/fyneui/mcpdetails.go) · 
`TestTokenKeptAcrossStarts`, `TestPortTakenMovesAndKeepsTheNewOne`, `TestBridgeFollowsARegenerate`,
`TestRegenerateEndsPendingConfirmation`, `TestMCPRegenerateWarnsFirst`

## D-54: The top bar: layout toggles, the Update button, the theme toggle

**Status:** built · **By:** owner

The top bar's right part, left to right: the Update button while an update is available, the alerts
pill (one number, every alert shown, in the colour of the worst; its source: D-77), the light and
dark toggle, the bell, a short divider and the three layout toggles (their spacing: D-93). The left
part ends with the history arrows: the top bar names no cluster; the status bar and the navigator
do, and each connection dot opens the connection log ([D-66](#d-66-the-connection-log)).

- **Layout toggles** in VS Code's order: the navigator (`ui.toggleNavigator`, ⌘B), the dock
  (`ui.toggleDock`, ⌘J) and the chat column (`chat.toggle`, ⌘I); UI commands with an optional
  `open`, also in the View menu; state `UIState.NavigatorHidden`, `DockOpen`, `Chat`.
  `workspace.json` keeps the navigator's and the chat column's, not the dock's (its tabs are not
  restored). The icons are Cogpit's own 16-point drawings, the part filled while open (D-12). A
  hidden navigator gives its room to the views and the dock; the hotbar stays.
- **The dock opens by itself** when a tab is added or another tab is made active while the previous
  one still exists (`UIState.SyncDock`, run after every change), not when the active tab closes; a
  command that shows an open tab, the active one included, opens it (`UIState.ShowDockTab`). On an
  empty dock the toggle opens a terminal (D-94).
- **The Update button** shows while the last check found a newer release, until a check finds none
  or the update session of the user's check ends (installed, skipped, "Remind Me Later", failed).
  Label "Update to v<version>" (the appcast's `displayVersionString`, at most 64 bytes of letters,
  digits, spaces and `.+-_()`), else "Update", also where the bar has no room. A click runs
  `app.installUpdate`, the window's only: on macOS Sparkle's window with the release (Linux: D-108,
  Windows: D-111). Scheduled checks never open Sparkle's window ([D-4](#d-4-packaging-and-updates)).
- **The light and dark toggle** shows the look a click goes to (a sun in the dark, a moon in the
  light) and sets Preferences › App › Theme through `preferences.set`, the explicit look also when
  the theme follows the computer; greyed in a run started with `--theme`.

**Why:** An update window popping up over a production cluster's terminal while the user types is a
surprise; the update waits behind a button, as in VS Code. The dock is its tabs, as Lens's is. The
status bar and the navigator already name the cluster. `preferences.set` names the result, so an
agent sets a look without guessing what the window shows.

**Rejected:**
- Sparkle's window for scheduled finds — it can pop up while the user types.
- A `ui.theme.toggle` command, or changing the computer's appearance — a toggle depends on what the
  window shows, and Cogpit never changes the system's settings.

**Code:** [`engine/layout.go`](../../internal/engine/layout.go),
[`fyneui/layoutbar.go`](../../internal/ui/fyneui/layoutbar.go),
[`fyneui/bars.go`](../../internal/ui/fyneui/bars.go),
[`fyneui/themetoggle.go`](../../internal/ui/fyneui/themetoggle.go),
[`update/sparkle_darwin.m`](../../internal/update/sparkle_darwin.m) · **Tests:** `TestSyncDock`,
`TestShowDockTab`, `TestLayoutTogglesThroughTheEngine`, `TestUpdateButtonShowsOnlyWithAnUpdate`,
`TestSparkleScheduledChecksFindTheNewestRelease`, `TestTopBarNamesNoCluster`

## D-55: A Config Map's values edited in the drawer

**Status:** built · **By:** owner

The Data section shows each key on its own line above its value, in a `yamlview` editor 3 to 12
lines tall with a resize grip. A Secret's Data ([D-59](#d-59-a-secrets-values-edited-in-the-drawer))
and Binary Data share this layout, and other drawer names wrap at `-` `.` `_` `/`. One Save writes:

- `configMap.readData` returns the first 100 keys with whole values; `configMap.reviewData` diffs
  the edit against the cache and dry-runs it; `configMap.saveData` (mutating) writes as
  `object.apply` does, with the base resourceVersion as precondition. A changed Config Map is never
  overwritten silently (`DataConflictError`): only a second call naming the newer version writes
  over it. With `from`, a review rebases onto a newer version where none of the edit's keys changed.
- `add` (checked by `CheckDataKey`, the API server's rule) and `remove` go in the same update. A
  removal is destructive for every origin, also for an agent with agent writes allowed
  ([D-2](#d-2-agent-interface-scope)). In the window the review is the confirmation: it arms after
  the confirmation delay and is bound to the object's UID and keys (`checkRemovalTarget`).
- Refused before sending: a `data` key the data lacks, binary data, a Helm release record, an
  immutable Config Map, and an agent's value holding a Secret placeholder
  ([H-82](../security/agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents)).
- `configMap.keepEdits` (GUI-only) keeps unsaved edits 300 ms after typing stops. Two seconds later
  they are written as an owner-only draft in `drafts/`, which `editor.restoreDraft` restores.
- `object.apply` is destructive too when its text drops a data key the cache or the edit's base
  holds (`EditRemovesDataKeys`). Confirmations name keys, never values.

**Why:** long keys ran under their values in the name column, and Lens's drawer edits in place. A
removed key loses data a workload reads; an added one changes nothing until something refers to it.

**Rejected:** a Save per key — each would conflict with the version the last one wrote; keys added
through `data` — a typo would add one; agents reading kept edits — they could plant text.

**Limits:** a write to an edited key before Save shows the conflict again; a crash within about
2.3 s of the last keystroke loses it; drafts are plain text ([H-38](../security/credentials.md#h-38-drafts-keep-typed-text-in-plain-text));
only the review refuses an added name matching a key beyond the first 100; Fyne 2.8.2's hit test
can let a pointer over the drawer's header reach an editor scrolled under it (read in its source).

**Code:** [`engine/configmapdata.go`](../../internal/engine/configmapdata.go), [`datakeys.go`](../../internal/engine/datakeys.go), [`drafts_drawer.go`](../../internal/engine/drafts_drawer.go),
[`editdatakeys.go`](../../internal/engine/editdatakeys.go), [`fyneui/configmapdata.go`](../../internal/ui/fyneui/configmapdata.go) · 
`TestConfigMapDataNeverOverwritesUnasked`, `TestConfigMapDataRebases`, `TestKindConfigMapDataSave`,
`TestConfigMapDataAgentRemovalAsks`, `TestConfigMapDrawerRemovalArms`

## D-56: The terminal's scrollback

**Status:** built · **By:** owner

The terminal keeps the lines that scroll off the main screen, with no line limit. Each terminal is
bounded at 256 MiB; past that the oldest lines go, and a row above the scrollback counts them.

- A view scrolled back stays put while output arrives, as in Terminal.app, iTerm2 and VTE (xterm
  jumps to the bottom). Typing or a shortcut sent to the shell returns the view to the screen.
- A scroll bar shows while there is scrollback: thin, wider under the pointer, with a draggable
  thumb, and a tap above or below the thumb moves one screen. There is no keyboard scrolling.
- In the alternate screen (less, man, vim, k9s) the wheel goes to the program: mouse buttons 4 and
  5 if the program asked for the mouse, else the arrow keys as typed (`ESC O A` under mode 1049).
- A selection spans the scrollback and survives new output there. A drag past the view's edge
  scrolls, and a wrapped line copies as one line.
- A new width rewraps the scrollback and the screen; the cursor stays on its character. `clear`
  (`CSI 3 J`) empties the scrollback. Rows enter it only from a scroll region at the screen's top.
- The reader hands output to the main goroutine in batches and waits for it at most every 17 ms
  (`maxRefreshInterval`), which keeps the queue bounded under a flood.
- Agents read the scrollback when the tab's switch is on
  ([D-44](#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs)), by stable line numbers
  (`dock.read`'s `before`, `after` and `limit`).

Upstream: the fixes for draft #147 are offered as andydotxyz/terminal#2 (open). Cogpit keeps its
own copy of the widget meanwhile ([D-14](#d-14-terminal-widget)).

**Why:** upstream keeps no scrollback. Draft #147 keeps every row in Fyne's TextGrid with
scrolling off, so each refresh redoes a row object per row held (56 times the cost at 1,000 rows,
measured). A scrollback line takes under 400 bytes per 80 cells (`TestScrollback_Size`).
Terminal.app has no line limit by default; the bound stops container output that never ends.

**Limits:** no search in the scrollback; wide characters take one cell; on macOS, output pauses
while the window is resized with the mouse ([PATCHES.md › Known and not patched](../../third_party/fyne-terminal/PATCHES.md#known-and-not-patched)).

**Code:** [`history.go`](../../third_party/fyne-terminal/history.go), [`scrollback.go`](../../third_party/fyne-terminal/scrollback.go),
[`scrollbar.go`](../../third_party/fyne-terminal/scrollbar.go) ([PATCHES.md › Scrollback](../../third_party/fyne-terminal/PATCHES.md#scrollback)),
[`termview/output.go`](../../internal/ui/fyneui/termview/output.go) `Terminal.Output` · 
[`scrollback_test.go`](../../third_party/fyne-terminal/scrollback_test.go), `TestOutputNumbersStayWithTheirLines`

## D-57: Choosing a cluster

**Status:** built · **By:** owner

The navigator's section is the one list that chooses a cluster. There is no context picker, and
the title bar has no cluster switcher.

- These show the section with its search focused (`findCluster`), after closing a settings page
  and showing a hidden navigator: ⌘P, Cluster › Connect to Cluster…, the palette's Connect to
  Cluster (`cluster.connect`), the workspace's button while the shown cluster is not connected,
  and a `--context` refused at start. ↵ drills into the first match.
- Escape on ⌘P's search first clears the text. On the empty search it returns to the cluster ⌘P
  came from while that cluster is shown and connected or connecting, else closes the search. On
  any other search, Escape clears and closes it.
- At a cluster the header shows "‹ All Clusters", the one visible way up (← on the tree's root
  is the keyboard's). At the section it shows "All Clusters", a "+" (Add Kubeconfigs…) and the
  search; the root row keeps Lens's name, Local Kubeconfigs. There is no scope dropdown.
- A row's tooltip says why the last connect failed (Show Connection Updates… in its menu opens the
  log), whether a login program, token file or unchecked connection is not allowed yet or changed,
  and which server an `insecure-skip-tls-verify` context's credential goes to (also a warning icon).
- A `--context` refused before connecting also shows a toast "Could not connect to <name>: …". A
  failed connect shows its error page ([D-114](#d-114-a-failed-connects-error-page-and-its-stages)).
  The welcome page lists the hotbar's and recent clusters and has no connect button.
- The section, the navigator's visibility and the search text are engine state
  (`ui.setNavigatorScope`, `ui.toggleNavigator`, `ui.setNavigatorSearch`). The search field's focus
  and ⌘P's origin are the window's. Agents connect by name with `cluster.connect`. Every cluster
  and kubeconfig command's menu path is under `Cluster/`.

**Why:** the picker and the section listed the same contexts with the same connect and menu, so
every change was made twice, and users had to learn which of eight ways led where.

**Rejected:** a ⌘P quick switcher beside the section — two lists to keep in step; Connect to
Cluster dropped from the palette — the palette lists every command.

**Code:** [`fyneui/findcluster.go`](../../internal/ui/fyneui/findcluster.go) `findCluster`, `showSection`;
[`navigator_header.go`](../../internal/ui/fyneui/navigator_header.go) `navHeader.escape`;
[`navigator_sections.go`](../../internal/ui/fyneui/navigator_sections.go) `navContextRow.tipText`;
[`engine/navscope.go`](../../internal/engine/navscope.go) `syncNavScope` · **Tests:** `TestConnectToClusterShowsTheSection`,
`TestConnectToClusterShowsAHiddenNavigator`, `TestAutoConnectRefusedShowsTheSection`,
`TestNavigatorSectionTooltipLines`, `TestClusterCommandsUnderTheClusterMenu`

## D-58: Standby and lost connections

**Status:** built · **By:** owner

Cogpit notices a connected cluster that stops answering (sleep, network, VPN) or rejects its login,
says so, and reconnects by itself; the window never blocks
([usage](usage.md#when-the-connection-breaks), [architecture](architecture.md#d-58-the-connections-states)).

- **Detection:** macOS's wake and network-path signals and a broken watch check at once; a heartbeat
  runs every 15 s with 5 s to answer. Two failed checks 2 s apart interrupt (one lost packet is no
  interruption); after a wake one does. Windows and Linux have no wake or network signal.
- **While interrupted:** the views keep the cache's rows; every write is disabled for every origin,
  so none with an unknown outcome starts; agents' reads carry the data's time; lists and watches
  wait at a gate in front of the credential plugin, other requests fail at once.
- **Reconnecting** never gives up: at once on a wake or network change, else after 1, 2, 4, 8, 16,
  then every 30 s, each try without credentials so that no plugin runs in the backoff.
- **Login expired:** the first 401 after a working request lets client-go run the approved plugin
  once; a second 401, or a plugin silent for 30 s, makes it `loginExpired` with Log in again
  ([D-81](#d-81-reconnect-and-what-log-in-again-does)); until then a login guard and no probes keep
  the plugin from running again. A cluster the window does not show renews per its setting
  ([D-69](#d-69-several-clusters-connected-at-once) item 4).
- **Streams by kind:** logs resume from their last line after a `[cogpit]` marker; port forwards
  dial again; shells end and `dock.reconnect` opens a new session in the tab. Drains and Helm
  actions stop and are offered afterwards as after a crash ([D-11](#d-11-crash-stance)).
- **A dot, never a word:** green connected; blinking that green (500 ms full, 500 ms at 30 % alpha)
  while connecting or interrupted, at least 3 s (the window's minimum: agents read the true state);
  red for a failed connect or an expired login; grey not connected; steady at half alpha under
  Reduce motion ([D-107](#d-107-the-navigator-slides-and-reduce-motion)). A click opens the
  connection log ([D-66](#d-66-the-connection-log)).
- A view not read before says why it is empty; another cluster's namespaces go at a switch; a log
  reopened by name that does not continue its last line says so, a workload's stream adopting the
  cache's next instance instead of opening a second one.

**Why:** a sleeping computer drops every connection, and a plugin waiting for a browser login holds
every request behind client-go's exec authenticator; unnoticed, the views stand still or empty.

**Rejected:** a modal dialog or the status bar alone; queuing or sending writes; giving up; the
plugin at every 401 (client-go's default: a pile of browser tabs) or never; resuming an operation
after the reconnect; refusing the held lists (client-go's informers back off up to a minute);
rewriting client-go's exec authenticator; reading a pod's UID before a log reopens (past the cache).

**Code:** [`cluster/health.go`](../../internal/cluster/health.go), [`engine/connwatch.go`](../../internal/engine/connwatch.go),
[`fyneui/conndot.go`](../../internal/ui/fyneui/conndot.go) · **Tests:** `TestTwoFailedHeartbeatsInterruptTheConnection`,
`TestAPluginThatWaitsExpiresTheLogin`, `TestAPodLogResumesAfterTheInterruption`, `TestDotsBlinkWhileReconnecting`;
behind `testkit.FreezableProxy` `TestStandbyFrozenControlPlane`, `TestStandbyWindowStaysUsable`; by hand: tag `standby`

**Not covered:** [H-101](../security/trust-boundaries.md#h-101-a-credential-plugin-runs-again-during-a-connection-without-being-checked-again),
[H-102](../security/agents-and-mcp.md#h-102-an-agent-may-act-after-the-reconnect-on-what-it-read-during-the-interruption).

## D-59: A Secret's values edited in the drawer

**Status:** built · **By:** owner

A Secret's values and keys are edited in its drawer as a Config Map's are
([D-55](#d-55-a-config-maps-values-edited-in-the-drawer)), in Cogpit's window only. Show reads the
whole value into an editor (`secret.reveal` with `whole`); Add Key and each key's remove button add
and remove keys. Save reviews a masked diff (each key changed, added or removed with its sizes,
never a value) and the server's dry run, then updates with the base version as precondition: a
newer version none of the edited keys changed in is rebased onto, otherwise the conflict shows and
"Overwrite" writes over it. A save that removes a key is destructive; the review confirms it.
`secret.reviewData` and `secret.saveData` are GUI-only: refused for every other origin in
`Dispatch`, again in `Refuse`, `Run` and the engine. Where a value goes:
[credentials](../security/credentials.md#secret-values-edited-in-the-drawer).

- Unsaved values and keys stay in the window's memory until Cogpit quits, never on disk, and come
  back masked; Hide masks an edited value and keeps the edit. An unedited value revealed at an older
  version is dropped when the Secret changes, so a rotated credential's old value cannot be copied.
- Removing a key needs no Show; a binary value can be removed, not edited; a removal whose value was
  never shown never rebases (any change of the Secret is the conflict).
- Refused before sending: a Secret the drawer does not show, a key the data lacks, a binary value,
  an immutable Secret, a Helm release record. Where Save cannot work (also the demo cluster, no
  `update secrets`) Show reveals read-only, the reason under the data.
- Each key's line ends "Show [copy] [b64] [remove]" (or Hide) in every state. Show is marked as
  exposing (word, outline and eye in the warning amber), Hide green with the eye struck through,
  one width; every Show of a secret looks so, the MCP token's too. Copies follow the clipboard rule
  for Secret values ([D-25](#d-25-copying-values)); an empty value copies nothing.
- The window's own save writes no audit line (the audit log is agents',
  [D-2](#d-2-agent-interface-scope)); an agent's refused call is audited with the Secret and keys.

**Why:** no path an agent, the CLI or an MCP client reaches may write a Secret's value; the masked
diff lets the user check a save with no value on screen (and no hash: a short password's hash can
be guessed offline).

**Rejected:** the window calling the engine without a command (the one write outside the registry);
the Edit tab (leaves the drawer); a read-only editor where Save cannot work; keys added or removed
by agents (an added key carries a value, a removal is a loss); a filled warning plate on Show.

**Code:** [`engine/secretdataedit.go`](../../internal/engine/secretdataedit.go), [`fyneui/secretdata.go`](../../internal/ui/fyneui/secretdata.go)
· **Tests:** `TestSecretDrawerDataIsGUIOnly`, `TestSecretDrawerDataConflictAndRebase`,
`TestSecretDrawerDataAddsAndRemovesKeys`, `TestKindSecretDataSave`,
`TestSecretDrawerKeepsUnsavedInMemory`, `TestSecretShowMarkedAsExposing`

**Not covered:** a save drops kubectl's last-applied annotation
([H-103](../security/credentials.md#h-103-a-secret-saved-from-cogpit-loses-kubectls-last-applied-annotation));
typed and revealed values are Go strings that stay in memory until collected; a crash loses unsaved
values, by design; a pointer over the drawer's header may reach an editor scrolled under it (D-55).

## D-61: Cogpit Metrics, the bundled metrics stack

**Status:** withdrawn · **By:** owner

Withdrawn: Cogpit installs nothing into a cluster for its own features; it finds and reads the
metrics stack the cluster runs
([privilege footprint](../security/privilege-footprint.md#nothing-installed-for-cogpits-own-features)).

## D-62: Checks against a real cluster

**Status:** built · **By:** owner

Besides kind, Cogpit is checked against a real cluster someone names: the `TestReal*` tests in
[`real_kind_test.go`](../../internal/testkit/integration/real_kind_test.go), tagged `kind` so that
CI compiles them without running them (`make test-kind` runs `^TestKind`), skipped without
`COGPIT_REAL_KUBECONFIG` and `COGPIT_REAL_CONTEXT`, run by `make test-real` with a HOME of their own.
Their writes go through an engine with the GUI origin and a confirming hook, only into their own
`cogpit-real-*` namespaces, to a node, a Helm repository or Flux objects only when a variable names
them, and are undone whatever happens. Nothing of the cluster enters the repository. Settled by the
checks:

- Budget P11 counts reads of objects: a drawer's first `SelfSubjectRulesReview` per namespace and
  session is the access reviews' own cost (P15).
- A Flux OCI HelmRepository without a status is Ready unless suspended, its Message empty, as
  `flux get` shows it: no controller reconciles one since Flux 2.1.
- A reference to a custom resource (an event's involved object, Controlled By in the Pods list and
  in every drawer) links to its kind through `CustomKindsByGroupKind`, the definitions by group and
  Kind, at no request; for a user who may not list the definitions it stays plain text.
- An unfinished drain says what it left: "this drain evicted no pod", or "n of m pods evicted".
- A confirmation names each object by its kind, as its Objects row does ("Drain Node worker-1").

**Why:** a real cluster (hundreds of custom resource definitions, Flux, Kyverno) shows what kind
does not, and it may be someone's production.

**Rejected:** writes through `cogpit-cli` — it confirms nothing, so destructive commands are
refused (D-2); a build tag of their own — no CI job would compile them; prefetching every
namespace's rules at connect for P11 — a review per namespace for drawers that may never open.

**Open:** the unfinished-drains banner says "partly drained" also when no pod was evicted (the drain
journal keeps no count); custom owners are linked by inference, as Lens's capture shows such links
in the Events list only; none of this is compared with a running Lens.

**Code:** [`kinds/related.go`](../../internal/model/kinds/related.go) `RefToWith`,
[`commands/nodes.go`](../../internal/engine/commands/nodes.go) `drainLeft` · 
`TestFluxStatusTexts`, `TestEventInvolvedCustomResourceLinks`, `TestDrawerLinksCustomOwners`,
`TestKindNamespaceLimitedUser`, `TestDrainLeft`, `TestConfirmMessageNamesKinds`

## D-63: Long commands as background jobs

**Status:** built · **By:** lead (open to revision)

An action on a selection that sends one request per object runs as a background job when it acts on
more than ten objects (`engine.DirectSelection`), within its call otherwise: `object.deleteMany`,
`workload.restartMany`, `flux.reconcileMany`, `flux.suspendMany`, `flux.resumeMany`,
`flux.resetMany`, `argocd.application.syncMany`, `argocd.application.refreshMany`. Reconcile with
Source ([D-33](#d-33-the-flux-actions)) and `node.drain` always run as jobs. The confirmation is
asked in `Dispatch` as for any command; the command then returns at once with the job's dock tab.
The selection bound stays 500 (`MaxSelection`): the confirmation lists every object.

- The job keeps the command's context values (`JobSpec.Parent`: the origin, the confirmed UIDs), so
  each step acts on exactly the confirmed object. A failed step does not stop the others; nothing
  is retried. The outcome counts done, failed and not attempted; a toast says it when the job ends
  by itself; an agent's job gets a third audit line, `ended`.
- `job.cancel` (a UI command: any origin, any job) and the tab's Cancel stop a job and keep its tab;
  closing the tab stops it too. A request under way finishes; later steps say "Not attempted:
  <why>". `job.cancel` returns the tab once it shows the end, at most 10 s later.
- A job stops when its cluster's connection is interrupted, its login expires or lapses, or the
  cluster is disconnected, and does not go on after a reconnect
  ([D-58](#d-58-standby-and-lost-connections)); showing another cluster does not stop it. A crash
  ends a job like a stop: each object's action is one request, so objects are done or not attempted.
- **Drain:** the journal entry is written before the job starts; the steps are the cordon, then one
  per pod (the cache's pods on the node until the drain's own list replaces them). The job outlives
  the MCP call that started it. A stop cuts the evictions under way and leaves the node cordoned and
  the drain `stopped` in its journal; the drain banner offers Finish drain, Uncordon and Dismiss as
  after a crash ([D-11](#d-11-crash-stance)) and has no row for a drain this window runs.

**Why:** 500 deletes take about 100 s at the connection's 5 requests per second, with nothing shown
and no way to stop them. Ten is the connection's burst (`cluster.DefaultBurst`): up to ten requests
end within about a second, where a dock tab per action would only pile up.

**Rejected:** a job for every size (tabs pile up); agents cancelling only their own jobs (closing a
tab stops any job already); cutting a selection step's request (what was done would be uncertain);
a small drain within the call (a drain waits minutes for grace periods and budgets); Cancel
uncordoning the node (a write an agent's UI command must not make).

**Code:** [`engine/jobs.go`](../../internal/engine/jobs.go), [`commands/manyjob.go`](../../internal/engine/commands/manyjob.go),
[`commands/drainjob.go`](../../internal/engine/commands/drainjob.go) · **Tests:** `TestJobCancelKeepsItsTab`,
`TestJobStopsWhenInterrupted`, `TestDeleteManyAsAJob`, `TestDrainJobOutlivesItsCall`,
`TestDrainJobCancelledLeavesAPendingDrain`, `TestKindDeleteSelectionAsAJob`

**Not covered:** an agent can stop a drain the user confirmed
([H-111](../security/agents-and-mcp.md#h-111-an-agent-can-stop-a-drain-the-user-confirmed));
a multi-document Apply or Create and Helm's actions run within their call (Helm waits up to 5
minutes for hooks, without Cancel); no test interrupts a real connection during a drain; a job tab
of 500 steps (a row per step, no recycling list) is not measured.

## D-64: envtest, a real API server in CI without Docker

**Status:** built · **By:** lead (open to revision)

Tests tagged `envtest` (`TestEnvtest*`) run against a real etcd and kube-apiserver started by
controller-runtime's envtest ([`envtest.go`](../../internal/testkit/envtest/envtest.go): `Start`,
`FreezeAPIServer`, `ThawAPIServer`), without Docker or a kubelet. The binaries are envtest's release
archive of Kubernetes 1.37.0 for darwin-arm64 and linux-amd64, pinned by version and SHA-512 in the
`Makefile`; `make envtest-bin` ([`fetch.sh`](../../hack/envtest/fetch.sh)) fetches them into
`.cache/envtest/1.37.0`, `make test-envtest` runs the tests with the race detector, and CI runs
them as the job Integration Tests (envtest) ([testing](testing.md)).

- `FreezeAPIServer` stops the API server with SIGSTOP (its PID recorded by a wrapper script that
  execs the binary), `ThawAPIServer` resumes it with SIGCONT: the port accepts, nothing answers.
- Without `COGPIT_ENVTEST_ASSETS` the tests skip; a folder without the binaries fails them.
- Unix only: controller-runtime v0.25.2's envtest does not compile for Windows, and the freeze needs
  SIGSTOP.
- The standby test shortens the connection watch's intervals (heartbeat 1.5 s, 1 s to answer,
  second check after 0.2 s, backoff 0.1 to 0.8 s).

**Why:** D-58's standby case needs a real API server whose process can be stopped; `docker pause`
of a kind node works only on a cluster of its own, and CI's kind job shares one cluster.
controller-runtime is in the module graph through Helm already.

**Rejected:** a starter of our own around the binaries (certificates, login and flags to follow at
every release); setup-envtest or envtest's own download (they check an archive against an index
that changes with it); `testkit.FreezableProxy` (already the fake API server's test); `docker
pause` (needs Docker).

**Code:** [`envtest.go`](../../internal/testkit/envtest/envtest.go) · 
`TestEnvtestStandbyAPIServerStopped`

**Not covered:** pods do not run (no kubelet, scheduler or controller manager), so logs, exec,
attach and port forwards stay the kind tests'; one Kubernetes version, which Renovate does not see
([H-18](../security/updates.md#h-18-dependencies)); a revoked login (envtest's client certificate
cannot be revoked; `TestStandbyRevokedLogin` covers it against the fake API server).

## D-65: Discovery cached on disk

**Status:** built · **By:** owner

A connection keeps the API server's discovery answer on disk and reuses it at the next connect, in
the app and in `cogpit-cli`:

- One owner-only file per server and identity in `<user cache dir>/Cogpit/discovery`, named by a
  SHA-256 over the server URL, its TLS server name, the kubeconfig files, the context's cluster and
  user entry names and the impersonated user, never by a credential. It holds no object
  ([D-13](#d-13-cache)).
- Used only for the same server version (`GET /version`, sent by every connect): 6 hours once the
  custom resource definitions confirmed it, else 10 minutes; a partial answer a minute, unwritten.
- The confirmation is a SHA-256 over each definition's name, generation, established and
  being-deleted state, taken at every rebuild of the custom kinds. An answer checked against another
  digest, or read from an unchecked file, is dropped from memory and disk; one fetched in this
  connection takes the digest. A kind the REST mapper does not know resets it (`MapKind`).

A stale answer, until a check drops it, shows a view's refusal for an API group switched off without
a new version, or misses for up to six hours an aggregated API that appeared without one. Custom
kinds come from the definitions' watch, not from discovery.

**Why:** otherwise every connect and every `cogpit-cli` run asks discovery again: two requests with
aggregated discovery, a large answer on a cluster with hundreds of definitions.

**Rejected:** client-go's `disk.NewCachedDiscoveryClientForConfig` (kubectl's) — a client and
connection pool of its own beside the connection's, files 0660, no check of the server version or
the definitions; the server URL alone as the name — a proxy that serves clusters under one URL by
identity (Teleport) would share one file.

**Open:** a definition served a moment after its watch event can be missing from an answer stamped
with that digest until the next change or the time-to-live; the folder is never cleaned (deleting it
is harmless).

**Code:** [`discocache.go`](../../internal/cluster/discocache.go),
[`customkinds_digest.go`](../../internal/engine/customkinds_digest.go) · 
`TestDiscoveryCacheFileIsCheckedBeforeUse`, `TestDiscoveryCacheIsPerIdentity`,
`TestDefinitionsConfirmOrDropTheDiscoveryCache`, `TestDiscoveryCacheAcrossConnects`

## D-66: The connection log

**Status:** built · **By:** owner

Every connect, reconnect, login, disconnect and interruption of a context is an attempt in its
connection log, successful ones too: kind, start, steps with times, outcome, duration, a connect's
stages ([D-114](#d-114-a-failed-connects-error-page-and-its-stages)), and for a
failure the error picture (error chain, HTTP status and message, a class such as DNS, TCP, TLS or
forbidden). No line holds a credential: a login is named by its method, a proxy password as •••.

- **Dialog** "Connection Updates: <context>" (every status dot of a cluster, the error page, the
  navigator's menu): the newest 50 attempts newest first, followed live, copied as text; an
  interruption is its own entry, so each entry ends in its outcome. Agents: `cluster.connectLog`.
- **Files** in `<user config dir>/Cogpit/connection-logs`: one owner-only JSON-lines file per
  context, named by the first 32 hex digits of the context id's SHA-256, shared with `cogpit-cli`;
  one append per record survives a force-quit, a cut last line is skipped. Attempts go after 7 days
  and, oldest first, beyond 1 MiB per context; an attempt holds at most 200 lines of 600 bytes. A
  file that cannot be written costs no connection: one toast per run, the log kept in memory.
- **Levels:** ERROR for a failed connect and a connection Cogpit closed (both mark the context),
  WARN for an interruption, its tries and an expired login, INFO for what the user did. A
  reconnect writes a try only when its reason changes.
- **A credential plugin's standard error** is kept only after a failed login, cut to 4 KiB and
  scrubbed (the user entry's secrets, URLs' user information and queries, credential shapes such as
  a JWT, a PEM block or a value after `Bearer`); its standard output is never captured. Cogpit sets
  the unexported `stderr` of client-go's own exec Authenticator; the output still reaches stderr.

**Why:** every connection set-up is traceable, a failure with enough detail to act on it.

**Rejected:** the cache directory — a record cannot be fetched again; replacing `os.Stderr` (the
plugin's runs not told apart), a wrapper program (it changes the command D-26 approved), an exec
authenticator of Cogpit's own (a plugin's client certificates need client-go's TLS callback).

**Open:** the scrubber can miss a secret a plugin prints, and the file keeps it for 7 days
([H-112](../security/credentials.md#h-112-a-credential-plugins-standard-error-in-the-connection-log));
a record another Cogpit process appends during a compaction can be lost.

**Code:** [`connectlog_files.go`](../../internal/engine/connectlog_files.go),
[`pluginstderr.go`](../../internal/cluster/pluginstderr.go) · **Tests:** `TestErrorPictureClasses`,
`TestPluginStderrOnFailureIsScrubbed`, `TestConnectLogRotation`, `TestConnectLogDialogFollowsTheLog`

## D-67: The demo cluster stays out of releases

**Status:** built · **By:** owner

Release builds leave the synthetic demo cluster out with the build tag `nodemo`, which only the
release targets set ([Makefile](../../Makefile) `DIST_TAGS`: `dist-app`, `dist-linux`,
`dist-windows`): no `internal/demo`, no flags `--demo`, `--demo-pods` and `--demo-events`, no context
`demo`. Development builds, `make run-demo`, the tests and `make showcase` keep it.

- The tag cuts in two places: [`connector_demo.go`](../../internal/engine/connector_demo.go) against
  [`connector_nodemo.go`](../../internal/engine/connector_nodemo.go) (`DemoBuilt`, the demo's
  backend) and [`cmd/cogpit/demo.go`](../../cmd/cogpit/demo.go) against
  [`nodemo.go`](../../cmd/cogpit/nodemo.go) (the flags); the rest follows from a connector that
  offers no demo. A kubeconfig context named `demo` is one like any other there.
- Tests that need the demo carry `//go:build !nodemo`.
- A tag no file knows is ignored silently, so: the dist targets check `go version -m` for the tag in
  both binaries; `TestReleaseBuildsLeaveTheDemoOut` checks that they pass `DIST_TAGS` and that it
  names `connector_nodemo.go`'s tag; CI's lint job runs `make check-nodemo` (`internal/demo` in
  neither binary with the tag and in both without it; lint and `TestNoDemoInARelease` with it).

**Why:** the demo is a development fixture, and a release ships none.

**Rejected:** a tag `release` — it would invite release-only code that no development build, test
or CI job runs; a positive `demo` tag — every untagged build (`go test`, gopls, golangci-lint) would
lose the demo; tagged file pairs in every package that names the demo — more cut points to keep.

**Code:** [`connector.go`](../../internal/engine/connector.go) `IsDemoContext` · 
`TestReleaseBuildsLeaveTheDemoOut`, `TestNoDemoInARelease`

## D-68: One short name for a cluster, and the user Cogpit is logged in as

**Status:** built · **By:** owner

- **One short name.** The window names a cluster everywhere (navigator rows, hotbar tiles, status
  bar, tabs, command palette, welcome page) by its display name from Cluster Settings › General,
  else the part of the context's name after the last "@" ("kubernetes-admin@prod-eu" → "prod-eu";
  nothing but blanks after it keeps the whole name). The full context name stays in the tooltips,
  in Cluster Settings, in the welcome row's second line and as the palette entry's category, and
  the searches find it; agents, logs, toasts and confirmations use the full context id. Only the
  user sets the display name and the icon (D-80).
- **The user, as the API server sees it.** Every connect, a Reconnect included, sends one
  `SelfSubjectReview` (`authentication.k8s.io/v1`, `v1beta1` on a 404) before the connection is
  published, bounded by 5 s and never failing the connect; it is sent again in the background after
  an interruption or Log in again. The navigator's root row shows the username and a box whose
  tooltip lists the groups; the review's UID and extra fields are dropped. Where the server does
  not serve the review (404), refuses it (403) or fails, the row shows the kubeconfig user entry's
  name with "(kubeconfig)", and the reason goes to the connection log and `ui_state`.
- **Not connected:** the last username the API server confirmed, dimmed, its tooltip "Last seen
  <date> <time>" (D-31), from `cluster-users.json` (owner-only; per context the username and when
  Cogpit last saw it confirmed, never the groups or the fallback's name).
- **Agents** read `ui_state`'s `user`: `username`, `groups`, `source`, `reason`, `lastSeen`
  ([H-113](../security/agents-and-mcp.md#h-113-the-users-name-and-groups-reach-every-agent-that-reads-ui_state)).

**Why:** context names are often "<user>@<cluster>" (kubeadm, kind, cloud CLIs), so the part that
tells clusters apart sits at the end of a name the navigator cuts. The kubeconfig's user entry is a
label that says nothing of whom the API server authenticates.

**Rejected:** the review after Connected — the row and `ui_state` would show a connected cluster
without its user; `v1alpha1` (Kubernetes 1.26) — behind a feature gate that is off by default;
asking again only after Log in again — a token renewed while the connection was away may be another
identity's; the last users in `cluster-settings.json` — they are no setting, and
`cluster.settings.get` would change at every connect.

**Code:** [`displayname.go`](../../internal/engine/displayname.go) `ShortContextName`,
[`engine/whoami.go`](../../internal/engine/whoami.go),
[`whoami_store.go`](../../internal/engine/whoami_store.go),
[`cluster/whoami.go`](../../internal/cluster/whoami.go) `Conn.ReviewUser`,
[`fyneui/clusteruser.go`](../../internal/ui/fyneui/clusteruser.go) · 
`TestShortContextName`, `TestShortClusterNameEverywhere`, `TestConnectLearnsTheUser`,
`TestUserFallsBackToTheKubeconfig`, `TestLoginRenewsTheUser`, `TestKindWhoAmI`

## D-69: Several clusters connected at once

**Status:** built · **By:** owner

Several clusters stay connected and the window shows one: the engine keeps one backend per
connected context in a pool, the shown one in the window's view state (D-52's windows: not built).

- **Switching** (hotbar, navigator, welcome page, a tab, a menu's Show) only shows a connected
  cluster from its cache, unless its login program changed since it was allowed (D-26). The cluster
  left stays connected, its views warm for 5 minutes, then cold and listed again when shown; its
  connection watch, streams and jobs go on until `cluster.disconnect {context}` or quit.
- **Commands** act on a reference's context, a dock tab's or the optional `context`
  (`engine.OnCluster`), else the shown cluster; an unconnected context is an error, never a connect,
  and objects in two clusters are refused. Blocked writes, access reviews, D-2's agent allowance and
  the confirmation follow that cluster. `ui_state` lists every connection.
- **Marks** on a cluster's dots: filled green live, blinking (re)connecting, green ring cold, grey
  ring a lapsed login, red a failed connect or renewal, grey not connected.
- **The window:** a switch clears the filters; the tab bar shows the shown cluster's tabs (every
  connected cluster's with the preference Mixed environments, off by default), never one of a
  cluster without a live connection; the dock shows the shown cluster's tabs and those of no
  cluster. A failed connect stays on its cluster (D-97).
- **A background cluster's login** is renewed as the shown one's unless its Cluster Settings'
  "Renew the login while the cluster is not shown" (on by default) is off: then Cogpit wraps the
  `interactiveFunc` of client-go's exec Authenticator, so no plugin starts; requests fail with
  `cluster.ErrLoginLapsed`, writes are disabled, a drain or Helm action stops. Showing the cluster,
  Log in again or turning the setting on renews the login.
- **Restore the last session** (Preferences › App, off by default) connects at start the cluster
  shown at quit, then the others one by one in the background. Agents may turn it and the renewal
  off only: on runs credential plugins unasked.

**Why:** an administrator works on several clusters at once; a switch must not reconnect.

**Rejected:** refusing a background cluster's requests instead of holding its plugin — it would stop
working streams; restoring all clusters at once — two browser logins could ask together.

**Code:** [`pool.go`](../../internal/engine/pool.go), [`target.go`](../../internal/engine/target.go),
[`loginhold.go`](../../internal/cluster/loginhold.go), [`restore.go`](../../internal/engine/restore.go)
· **Tests:** `TestSwitchingClustersOnARealStack`, `TestCommandOnABackgroundClusterActsThere`,
`TestTabsShownMixedEnvironments`, `TestHeldPluginOnARealStack`

## D-70: The disk image's window

**Status:** built · **By:** owner

The macOS disk image opens as a designed Finder window: 600 × 340 points on the app's plate (the
logo's navy gradient and halo), Cogpit's icon left and the Applications folder right at 124 points,
a light-blue arrow between them, Cogpit's own helm wheel faint behind the folder, the names on grey
pills that read in light and dark Finder. [`lab.json`](../../packaging/dmg/lab.json) holds the
settings and links the lab that draws them; the background is the lab's drawing at 1× and 2×,
joined into one TIFF at build time. The look is decided with the owner in a visual review.

The app's job, which holds no secret, builds the window as a template with dmgbuild (pinned by
version and hash); the signing job checks the template's entries and fills in the signed app with
Apple's tools only ([releasing](releasing.md#the-disk-images-window),
[H-18](../security/updates.md#h-18-dependencies)). No Finder flag hides `.app`: `codesign --verify
--strict` refuses the attribute, and Finder hides the extension anyway. Renovate never merges a
dmgbuild update on its own: its output ships in every disk image.

**Why:** Finder's default window (small icons on white) does not show what to do, and no code from
PyPI runs beside the signing identity.

**Rejected:** the Kubernetes logo — the Linux Foundation's trademark guidelines allow no use in
one's own design without written permission, and a seven-spoke look-alike is confusingly similar;
`create-dmg` — it drives Finder through AppleScript and needs a login session; a `.DS_Store` made
by hand — it breaks with the volume's name, which carries the version; dmgbuild in the signing job —
third-party code beside the identity; a job building the whole image — a job more per release.

**Code:** [`dmg-template.sh`](../../hack/macos/dmg-template.sh),
[`dmg_settings.py`](../../hack/macos/dmg_settings.py), [`release.sh`](../../hack/macos/release.sh)
`build_dmg`

## D-71: Thanos and VictoriaMetrics, read as Prometheus

**Status:** built · **By:** lead (open to revision)

Thanos's Querier and query frontend and VictoriaMetrics (single-node and the cluster's vmselect)
speak the Prometheus HTTP API, so the Prometheus provider reads them with the same probe and
queries. Detection finds them by their Services' default names among the cached Services and ranks
them after every Prometheus name; it probes at most three candidates
([architecture §9](architecture.md#9-metrics-internalmetrics) lists names and ranks). vmselect is
read under the default tenant, `/select/0/prometheus`; another tenant is the path prefix set in
Cluster Settings › Prometheus. The window names each "Thanos" or "VictoriaMetrics", a service the
user names too.

**Why:** a Prometheus by its own names holds the cluster's own series; a Querier or a
VictoriaMetrics may hold several clusters', and Cogpit's queries carry no cluster label. Matching
names costs no new request.

**Rejected:** a buildinfo probe of every Service on a likely port — requests with the user's
identity to services that are none of these; the operators' custom resources (VMSingle, VMCluster)
— they need list and watch rights and miss the charts' installs; ranking them before Prometheus — a
cluster that runs both would show another store's series; trying several vmselect tenants — a probe
per guess.

**Limits:** a store found or named that holds several clusters counts them all in the Overview's
sums and the pod counts, and a drawer's object may match another cluster's object of the same name.
Not verified: detection on a real cluster without a Prometheus (fakes only); VictoriaMetrics'
charts point by point against Prometheus (MetricsQL's `rate` does not extrapolate).

**Code:** [`promflavor.go`](../../internal/metrics/promflavor.go),
[`promdetect.go`](../../internal/metrics/promdetect.go) · 
`TestCandidatesThanosAndVictoriaMetrics`, `TestDetectThanosAndVictoriaMetrics`,
`TestKindVictoriaMetrics` (`make kind-vm`), `TestRealThanos` (`COGPIT_REAL_THANOS`)

## D-72: Native file panels and macOS folder access

**Status:** partly built · **By:** owner

Every file chooser is the operating system's own, and Cogpit raises no macOS privacy prompt by
listing the user's folders. Built on macOS; Windows and Linux show Fyne's dialogs and, for Sync
Files and Folders, Cogpit's own browser (Fyne's dialogs choose a file or a folder, never both).
Not built: the system dialogs of Windows and Linux (M11).

- **Panels.** [`nativedialog`](../../internal/ui/nativedialog/) shows NSOpenPanel and NSSavePanel
  through cgo as a sheet on the window; a request while one is up is dropped. Every chooser goes
  through the shell's `filePicker` and falls back to its Fyne dialog where no panel is shown: Sync
  Files and Folders (files and folders, several, synced in the order chosen, hidden files listed,
  from the home folder), a cluster icon's image (PNG, JPEG, SVG), the terminal's and kubectl's
  folders, an agent connection's CA file (`.pem`, `.crt`, `.cer`), and the saves of a table's CSV,
  the Create tab's YAML and a log tab's lines (a new file owner-only).
- **Bookmarks.** What Cogpit reads again later keeps a security-scoped bookmark made at the pick
  (kubeconfig sources and CA files read-only, folders read-write; none for an icon, copied at once,
  or a saved file), kept in an owner-only file, no keychain. At start, before any source is read,
  `cmd/cogpit` resolves them and accesses their items for the process's life
  ([file access](../security/file-access.md)).
- **Usage descriptions.** `make app` writes the five Files and Folders keys into Info.plist, each
  saying that programs Cogpit starts ask through it
  ([H-116](../security/file-access.md#h-116-programs-cogpit-starts-act-with-cogpits-folder-access)).

**Why:** Fyne's file dialog lists `~/Desktop`, `~/Documents` and `~/Downloads` for its sidebar each
time it opens, which raises macOS's prompts before the user chose anything. macOS grants what the
user picks in its own panel without a prompt, and a security-scoped bookmark is how Apple carries
that grant into a later run.

**Rejected:** the system's panels for the open choosers only — Fyne's save dialog reads the same
folders; an app-modal panel (`runModal`) — it holds the main thread that Fyne's loop and GLFW share;
copies of picked files — they freeze a kubeconfig or CA bundle the user edits, and a sync folder
cannot be copied.

**Code:** [`panel_darwin.m`](../../internal/ui/nativedialog/panel_darwin.m),
[`store.go`](../../internal/ui/nativedialog/store.go),
[`filepicker_choosers.go`](../../internal/ui/fyneui/filepicker_choosers.go),
[`cmd/cogpit/fileaccess.go`](../../cmd/cogpit/fileaccess.go) · 
`TestPanelConfiguration`, `TestBookmarkHarness`, `TestStoreRestore`, `TestSyncPanelSyncsEveryPick`,
`TestSyncFallsBackToCogpitsPicker`, `TestKeptSource`

## D-73: A login that waits: Cancel Connecting and a bound

**Status:** built · **By:** lead (open to revision)

- **Cancel Connecting.** `cluster.disconnect` of a cluster that connects (shown or in the
  background) cancels the connect at once and ends the credential plugin's run it waits for; the
  window labels it Cancel Connecting in the cluster's menus and on a button in the connecting
  workspace. The cluster ends disconnected without an error, its attempt `cancelled`.
- **A bound.** Each run of a plugin (a connect's, a renewal's, Log in again's) is ended after 5
  minutes (`DefaultLoginTimeout`): the connect fails, or a renewal leaves the login expired. The
  plugin's standard error is kept as after a failed login (D-66), but dropped after a cancel. The
  connect's 15 s timeout bounds its first request below the plugin, not the login.
- **Ending a run.** Cogpit finds the plugin's process among its own children at the run's begin and
  follows it by pid and start time, so a wrapper that `exec`s is ended too (macOS, Linux, Windows;
  no cgo). It signals that process alone: SIGTERM, then SIGKILL after 3 s. It says "Cogpit ended
  <program>" only when it found the process and may signal it, else "Cogpit stopped waiting for
  <program>; its process was not found and may run on"
  ([H-118](../security/credentials.md#h-118-what-a-cancelled-login-leaves-running)).
- **No run nobody asked for.** After Cogpit ended a run, none starts until the user connects or
  logs in again (`ArmLogin`), and a run starts only while an open connection the user did not
  cancel asks for one (`admit`). A run several contexts share (D-69) ends on a cancel only when no
  other connection still uses it; the bound ends it regardless. Log in again joins a run under way.
- **What it says.** The state stays as it was (`connecting`, `connected` or `loginExpired`); the
  connection's `waitingForLogin` (`program`, `since`, `until`) names the run for the window and
  agents ([usage](usage.md#a-login-that-waits-cancel-connecting-and-the-5-minute-bound)). Behind a
  run Cogpit could not end, `until` is left out and `runsOn` is true: the wait has no end time, and
  Cancel Connecting stops only the connect's wait.

**Why:** client-go's exec authenticator runs the plugin without a context under the
Authenticator's lock, every request that needs the credential waits for that lock whatever its own
context says, and it gives no handle on the process: a login nobody finishes would hold the
connect, and Disconnect, indefinitely. The bound is one fixed value: a browser login with a second
factor takes a minute or two, and Cogpit has no terminal, so `interactiveMode` does not tell a
browser login from a silent one.

**Rejected:** a cancel command of its own — `cluster.disconnect` already ends a connect under way;
running the plugin in Cogpit — certificate plugins need client-go's TLS callback (D-58); a wrapper
in the plugin's place — it changes the approved command (D-26); abandoning only the waiting request
— the plugin keeps the lock and the next connect waits behind it; signalling the process group —
the plugin runs in Cogpit's own group; a connection state of its own — the dots, the menus and
agents' checks of the state keep working when it stays.

**Code:** [`loginrun.go`](../../internal/cluster/loginrun.go),
[`loginhold.go`](../../internal/cluster/loginhold.go) `admit`,
[`pluginproc.go`](../../internal/cluster/pluginproc.go),
[`engine/loginwait.go`](../../internal/engine/loginwait.go) · 
`TestCancelledConnectEndsThePlugin`, `TestLoginTimeoutEndsThePlugin`,
`TestEndedLoginDoesNotRunAgainUntilLogInAgain`, `TestCancelEndsAPluginThatExecs`,
`TestCancelledConnectsStartNoRunAfterAStrayOne`, `TestCancelConnectingFromTheWindowAndAnAgent`

## D-74: How the lead's decisions are reviewed

**Status:** built · **By:** owner

The owner decides product direction, scope, security posture and whatever is irreversible or
outward-facing, one question at a time, and judges what the window shows in walks. Details below
that are the lead's: each is recorded where it lives (the question, the options weighed, the choice
and why) as the lead's decision, open to revision, without a confirmation queue. A choice that only
a report, a chat or a commit message names is written into its page.

**Why:** the owner cannot answer every small detail, some need time and others' feedback, and a
detail is cheap to change once a walk or a user finds it.

**Rejected:** every detail "for the owner to confirm" — a queue that grows by dozens a day; a weekly
short list of costly or visible details — still the owner's time every week; walks only, nothing
recorded — invisible details, security-relevant defaults among them, lose their review path.

**Recorded in:** this page's introduction, [conventions › Decisions](conventions.md#decisions) and
the rules in [AGENTS.md](../../AGENTS.md).

## D-75: A client certificate's validity in the cluster lists

**Status:** built · **By:** owner

For a context whose user logs in with a client certificate (`client-certificate-data` or a
`client-certificate` file; not a user with an `exec` plugin or an auth provider), Cogpit shows how
long it is valid where the user chooses a cluster, and never blocks, asks or warns before a connect.

- **Where:** a second line in the navigator's section, the cluster picker ("cert expires in 23
  days", "expired"), a pill on the welcome page's row, a line with the date in the section row's
  and the hotbar tile's tooltips; `clientCert` (`notBefore`, `notAfter`, `state`) in
  `cluster.contexts` for agents, a CLIENT CERT column in `cogpit-cli contexts`.
- **States:** muted while valid, the warning colour while expiring (within 14 days, or within the
  last fifth of the lifetime when that is shorter) or not yet valid, the error colour once expired.
  The shell's ten-second tick recomputes the texts without reading a file or asking a cluster.
- **What is read:** NotBefore and NotAfter of the leaf (the first CERTIFICATE block); never the PEM,
  the subject or the key. An unreadable certificate shows nothing and is logged once at debug level.
- **A file in a place macOS protects**, or behind a link into one, is read only after client-go read
  it to connect, and remembered for the run; any other file at every listing
  ([H-119](../security/file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)).
- **At a connect** of an expired or not-yet-valid certificate, the connection log's Login line,
  which names the dates (D-66), is a warning.
- **After notAfter** Cogpit keeps an established connection: refusing an expired certificate is the
  server's decision
  ([H-142](../security/credentials.md#h-142-a-connection-outlives-its-client-certificate-where-a-proxy-checks-it-at-the-handshake-only)).

**Why:** the time left helps where a cluster is chosen but must not stand in the way of a connect,
even one that will fail. A fixed window would keep a certificate issued for hours in the warning
colour all its life. Listing the contexts must not raise a macOS prompt the user did not ask for.

**Rejected:** a suffix such as "23d" — it takes the name's width in the 216-point navigator; the
tooltip alone — the time left must stand in the row; a log line of its own — it would repeat the
Login line's dates; ending the connection at notAfter, or a warning only — the server decides.

**Code:** [`kubeconfig/clientcert.go`](../../internal/kubeconfig/clientcert.go) `ParseCertValidity`,
[`cluster/clientcert.go`](../../internal/cluster/clientcert.go),
[`fyneui/clientcert.go`](../../internal/ui/fyneui/clientcert.go) · 
`TestClientCertificateState`, `TestProtectedClientCertificateIsReadAtConnect`,
`TestListingNeverOpensTheKey`, `TestContextsCarryTheClientCertificate`,
`TestAnExpiredClientCertificateDoesNotHoldTheConnect`, `TestAnExpiredClientCertificateWarnsInTheLog`

## D-76: Integrations per cluster, and the question at a new cluster's first connect

**Status:** built · **By:** owner

Cogpit reads five tools a cluster may run, each switched on or off per cluster: the Metrics Server,
Prometheus, Alertmanager (D-77), Trivy Operator (D-78) and Kyverno (D-79). Lens has none of this.
[Usage](usage.md#cluster-settings--integrations-and-the-question-at-a-new-clusters-first-connect)
has what the user sees, [architecture](architecture.md) §4.7 the switch, probes and followers.

- A cluster's choice is the set it switched off, absent until the user chooses. The default for new
  clusters has every integration on; the question's "Save as the default for new clusters" (off
  until ticked) and Preferences › Kubernetes set it. Off, an integration sends nothing and shows
  nothing. Lens's Metrics source is the two metrics switches.
- Cluster Settings › Integrations shows at once whether each was found automatically or needs a
  manual configuration (layout: D-87).
- While the shown cluster is connected and has no choice, a dialog asks; nothing waits for it, and
  nothing is read before the answer (D-82). Later, or closing it, puts it off for the run; the next
  start asks again. Never asked: the demo cluster, and the CLI, which keeps no cluster settings and
  reads every integration
  ([H-120](../security/agents-and-mcp.md#h-120-the-clis-headless-engine-does-not-apply-a-clusters-integration-switches)).
- While the question or the cluster's Integrations, Metrics, Metrics Server or Prometheus section
  shows a connected cluster, its metrics providers that are on are detected once, bounded as D-71's
  detection; one the request timeout cut short waits for the retry TTL (2 minutes).
- Only the user, in the window, switches an integration, on or off: `cluster.settings.set`'s
  `integrations` and `metricsSource` and `preferences.set`'s `integrationDefaults` are refused to
  agents and the CLI whenever named, also where agent writes are allowed (`ErrIntegrationsGUIOnly`);
  `cluster.integrations.step`, `cluster.integrations.lookFor`, `cluster.integrations.answer` and
  `cluster.integrations.ask` are GUI-only; `cluster.integrations.later` is open to agents.

**Why:** an absent choice tells "not chosen" from "all on", and an integration added later is on
for clusters chosen before it. Switching one on reads with the user's identity, so it is the user's
consent; switching one off hides alerts, grades or marks without the window saying who did it
(H-126, H-133: [agents and MCP](../security/agents-and-mcp.md)). The default is what the next Check
all looks for, and Ask again turns every integration off until answered, so both are switches; Later
only delays a question whose cluster reads nothing meanwhile.

**Rejected:** detection at every connect — probes clusters nobody looks at; detection only when a
drawer opens — "found" would not show at once; a toast instead of the dialog — easy to miss.

**Code:** [`engine/integrations.go`](../../internal/engine/integrations.go),
[`engine/integrationsask.go`](../../internal/engine/integrationsask.go),
[`engine/integrationsguionly.go`](../../internal/engine/integrationsguionly.go),
[`fyneui/integrationsask.go`](../../internal/ui/fyneui/integrationsask.go) · 
`TestIntegrationSettings`, `TestIntegrationsQuestionLifecycle`, `TestIntegrationsOnAreTheUsers`,
`TestIntegrationsShownDetectOnce`, `TestIntegrationsSection`, `TestKindIntegrationsQuestion`

## D-77: Alertmanager, the alerts pill and the alert board

**Status:** built · **By:** owner

While its integration
([D-76](#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect)) is on,
Cogpit reads the shown cluster's active alerts and shows them in a pill in the title bar and on an
alert board, a page like karma's, tidier; Lens shows no alerts. What the user sees:
[usage](usage.md#alerts-the-pill-and-the-alert-board); detection, polling and bounds:
[architecture](architecture.md) §9.

- `internal/alerting` (L2, its own `depguard` rule) finds the Alertmanager among the cached
  Services by well-known names, at most 3 probed with the alerts request itself, or uses the address
  the user set (in `metrics.json`; an agent's `cluster.setAlertmanager` always asks).
- One `GET /api/v2/alerts?active=true&silenced=false&inhibited=false` through the service proxy
  every 30 s while the cluster is shown and connected; failing, the polls back off to 5 minutes and
  the last answer stands for 5 minutes. Read only: no silences, no acknowledgements.
- `critical` is red, `info` yellow, `warning`, `major` and any other or missing label orange;
  `none`, Watchdog and InfoInhibitor are neither counted nor shown.
- The pill stays while the integration is on, grey with `warn` while nothing answers, and leads to
  the board, which says how to set the address. `ui_state`'s `alerts` holds counts and state, not
  the answer's time.
- The board is a page with a view's header (namespace filter, search), its folds engine state
  (`alerts.fold`); per alert only the summary, since when, the runbook (an absolute http or https
  URL without user info) and the namespace and object it names. A card shows 50, counts the rest.

**Why:** `internal/metrics` is made for polled series, not a list of alerts. Probing every Service
sends requests to services that are none (as for Prometheus, D-71); `GET /api/v2/status` would hand
Cogpit Alertmanager's whole configuration, and `/-/ready` is answered by Prometheus too. 30 s is
karma's and Alertmanager's own refresh (budget P17, [testing](testing.md)). The pill shows the shown
cluster, and a background one whose login is held (D-69) answers 401. The kept answer stops a
restarting Alertmanager from reading as "no alerts"; its limit stops an old count from standing. A
label not marked informational counts as a warning. A poll does not change the UI state's revision.

**Rejected:** the Prometheus Operator's `Alertmanager` resources — need list rights and miss the
charts' installs; a table under an Overview-like page — not karma's groups and cards; silences —
a write to Alertmanager.

**Code:** [`alerting/detect.go`](../../internal/alerting/detect.go),
[`alerting/poller.go`](../../internal/alerting/poller.go),
[`engine/alerts.go`](../../internal/engine/alerts.go),
[`fyneui/alertboard.go`](../../internal/ui/fyneui/alertboard.go) · **Tests:** `TestClassify`,
`TestCandidates`, `TestPollerBackoffAndKeep`, `TestAlertmanagerOnlyTheShownCluster`,
`TestAlertsPill`, `TestAlertBoard`, `TestKindAlertmanager`, `TestRealAlertmanager`

### What it does not cover

- **Mimir's and Cortex's multi-tenant Alertmanager** (a tenant header) and one behind an
  authenticating proxy (OpenShift's port 9095: "not answering"). VMAlertmanager is found by name;
  vmalert and Thanos Ruler are not Alertmanagers. None verified.
- **A background cluster's alerts** are its last answer until it is shown again.
- **Alert texts** reach agents and the chat's LLM endpoint
  ([H-11](../security/agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)); a lookalike
  found by name writes them
  ([H-121](../security/privilege-footprint.md#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)).

## D-78: Trivy Operator's image ratings

**Status:** built; the grade and its look are
[D-113](#d-113-image-grades-shields-from-trivys-reports) · **By:** owner

While its integration (D-76) is on, Cogpit grades the images of the shown cluster's pods from the
`VulnerabilityReport`s a running Trivy Operator writes (`aquasecurity.github.io/v1alpha1`, one per
workload and container, read by their `trivy-operator.*` labels): the Pods list's column after
Containers holds each pod's worst grade, each container section of the drawer its image's. No other
kind gets a column; agents read the grades in `object_get`, `view_rows` and `resources_list`. Lens
shows these reports only in its paid Security Center.

- A pod's reports are its controller's, as Trivy Operator resolves the owner (ReplicaSet,
  StatefulSet, DaemonSet, ReplicationController, Job; for a Job's pod also the CronJob its name
  gives, `<cronjob>-<digits>`); a bare pod's, or one another controller made, its own.
- A report of another image (no new scan yet) is left out: the repository (without registry and
  `library/`) and the tag must match where both name one; the digest is not compared.
- Init containers count, marked "(init)"; ephemeral containers do not. A pod some of whose images
  have no report takes the worst of the others.
- One cache handle per shown cluster, by the reports' namespace plan, in a trimmed scope
  (`trivy-summary`) without `report.vulnerabilities`; the reports' own view and editor read them
  whole. `ClusterVulnerabilityReport`s are not read.

**Why:** a report is made for the workload, and Trivy Operator picks the same owner; a report of the
same image elsewhere would grade the pod by another workload's scan. The CronJob from the Job's name
spares a watch of every Job. The column needs every pod's grade at once, so the reports are read per
cluster, not per drawer. The vulnerability lists are about 95 % of a report's weight. A background
read would keep a cluster's cache from going cold (D-69).

**Rejected:** a curated profile of the reports' kind, for users who may not list definitions — it
changes the reports' own view's columns; without the definitions the state says Cogpit cannot tell.

**Limits:** with the reports' own view open they are watched twice, trimmed and whole; a workload
name too long for a label (read from the annotation) is tested on a synthetic report only.

**Code:** [`engine/trivy.go`](../../internal/engine/trivy.go) `followTrivy`, `trivyWorkloadsOf`;
[`model/imagerating.go`](../../internal/model/imagerating.go) `ParseVulnerabilityReport`,
`VulnerabilityReport.Matches` · **Tests:** `TestParseVulnerabilityReport`, `TestImageMatches`,
`TestTrivyImageGrades`, `TestTrivyWorkloadsOf`, `TestTrivyNamespaceLimited`,
`TestTrivyReadsTrimmedReports`, `TestKindTrivy`

## D-79: Kyverno's policy violations on the objects

**Status:** built · **By:** owner

While its integration (D-76) is on, Cogpit marks every object of the shown cluster that a Kyverno
policy report names with a `fail`, or a `warn` (a failed rule of a policy marked as not scored),
whatever the policy's mode: an Enforce policy's fail on an object from before it counts as an Audit
one's. Marks: the lists' warning column, a "Policy violations" group in the drawer, and
`kyverno.violations` for agents; Lens shows none. What the user sees:
[usage](usage.md#kyvernos-policy-violations); the integration overlay and trimmed scopes, made for
this and used by D-78 too: [architecture](architecture.md) §4.4 and §5.

- The `wgpolicyk8s.io/v1alpha2` PolicyReports and ClusterPolicyReports come through the cache, as
  each kind's namespace plan says; only Kyverno's (managed-by `kyverno`, or a result source
  starting with "kyverno"), in both shapes (`scope`, or each result's `resources`), trimmed to their
  fail and warn results (`kyverno-violations`); indexed by the object's UID, else by its group,
  Kind, namespace and name.
- A kind with its own warning column gets the reason joined and the worse colour; any other kind,
  custom ones included, gets the column after Name while one of its objects violates a policy. Every
  violation is the warning colour.
- The drawer links each violation's policy while the cluster serves its kind: ClusterPolicy, Policy
  ("<namespace>/<name>") or ValidatingPolicy.
- Not read: the policies (the mode is not shown). `error`, `pass` and `skip` mark nothing; a
  violation enters neither the status filter nor the Overview's issues.
- Curated profiles of both report kinds let API discovery offer them without the definitions.

**Why:** the reports are Kubernetes objects Kyverno keeps current, so a watch holds them and nothing
is polled. Every fail counts, so the mode would only label, at four more watches of large policies.
Other engines write the same API. The trim cut a real cluster's reports by 60 %, mostly passes. A
replaced object (same name, new UID) never inherits a mark. An empty column on sixty views changes
them for nothing. A violation does not break the object, and a severity is the policy author's
label. A namespace-limited user rarely may list definitions. A background read would keep a
cluster's cache from going cold (D-69), so `kyverno.violations` refuses a cluster not shown.

**Rejected:** Kyverno's policy-reporter API or reports-server through the service proxy — a
component the cluster may not run, and polled; evaluating the policies in Cogpit — reimplements
Kyverno; a badge for kinds without a warning column — a new cell kind that does not sort.

**Code:** [`engine/kyverno.go`](../../internal/engine/kyverno.go) `followKyverno`,
`trimPolicyReport`; [`model/kinds/policyreports.go`](../../internal/model/kinds/policyreports.go)
`KyvernoReport`, `PolicyRef` · **Tests:** `TestKyvernoMarksViolations`,
`TestKyvernoNamespaceLimitedUser`, `TestViolationIndex`, `TestKyvernoViolationsCommand`,
`TestPolicyRef`, `TestKindKyverno`, `TestRealKyverno`

### What it does not cover

- **`openreports.io` Reports** are not read: a Kyverno writing them instead marks nothing, and while
  the cluster serves the wgpolicyk8s.io kinds the state says "found".
- **Kyverno's reports-server** (an aggregated API): whether it serves watch is not verified.
- **Kyverno's other CEL policies** and a ValidatingAdmissionPolicy's results: marked, their policy
  not linked.
- **Policy messages** reach agents and the chat's LLM endpoint and may quote the object's own fields
  ([H-11](../security/agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)).

## D-80: A cluster's accent colour

**Status:** built · **By:** owner

Every cluster can carry an accent colour, the user's mark of which cluster it is (red for
production, say), so that nobody acts in production believing to be in development. Lens has none
([beyond Lens › Everyday use](../parity/beyond-lens.md#everyday-use)).

- `ClusterSettings.AccentColor` (`accentColor` in `cluster-settings.json`), none by default: one of
  the icon colour's eight names (`IconColors`) or a free `#rrggbb`, drawn in a shade legible on
  both themes; another value is refused, and dropped when read from the file. It is independent of
  the icon colour.
- Set in Cluster Settings › General below the icon (None, the eight swatches, the free colour, the
  picker); a click applies it without a question.
- Shown only by the light strip under a view's header ([D-122](#d-122-the-light-strip)), for the
  shown tab's cluster.
- Only the user sets or clears it: `cluster.settings.setAccentColor` is GUI-only
  (`ErrAccentColorGUIOnly`), refused to agents and the CLI before anything is read, written or
  asked, also where agent writes are allowed. Agents read it as `accentColor`
  (`cluster.settings.get`, `ui_state`'s `clusterSettings`).
- The display name and the icon are the user's alone too (H-125): `cluster.settings.set` refuses
  `name`, `iconColor`, `iconLetters` and `iconTile`, set or cleared, to every origin but the window
  (`RefuseClusterIdentity`); `cluster.settings.resetIcon`, `cluster.settings.uploadIcon` and
  `cluster.settings.cutIcon` are GUI-only.

**Why:** An agent that could change the colour, the name or the icon could make a production
cluster look like a development one. Agents read all three, so they can tell which cluster the
user marked as what.

**Rejected:**
- agents may clear but not set it — clearing a production cluster's red does the same harm as
  painting it green;
- named meanings (dev, staging, prod) — the meaning is the user's;
- a default colour — an unset colour must not look like a mark.

**Code:** [`engine/accentcolor.go`](../../internal/engine/accentcolor.go),
[`engine/clusteridentity.go`](../../internal/engine/clusteridentity.go),
[`commands/accentcolor.go`](../../internal/engine/commands/accentcolor.go),
[`fyneui/clustersettings_accent.go`](../../internal/ui/fyneui/clustersettings_accent.go) ·
**Tests:** `TestAccentColorKept`, `TestAccentColorIsTheUsers`, `TestClusterIdentityIsTheUsers`,
`TestClusterSettingsAccentColor`

## D-81: Reconnect, and what Log in again does

**Status:** built · **By:** owner

A connection keeps the credentials it read at its connect (client-go re-reads only certificate,
key and token files, never what the kubeconfig embeds), so a login renewed in the file needs a new
connection.

- **Reconnect** (`cluster.reconnect`, a UI command that only reads) connects a cluster anew from its
  kubeconfig file as it is now, with every approval a connect asks for
  ([D-26](#d-26-binding-a-credential-plugins-approval-to-its-program)). The new connection is made
  first and replaces the old one in the pool ([D-69](#d-69-several-clusters-connected-at-once))
  once it stands; a failed reconnect leaves the old one. Tabs, view, filter, selection, drawer and
  dock stay, and the views list anew. Logs tabs reopen their streams, port forwards listen on the
  same local ports again, shells, attach and node shells end with a Reconnect of their own
  (`dock.reconnect`, which asks), a local terminal runs on, and the integrations restart without
  asking their question again. A background job (a drain) or a Helm action running on the cluster
  refuses it.
- **Log in again** (`cluster.login`, GUI-only) runs the context's credential plugin once on the
  connection, keeping cache, streams and forwards; a context without a plugin, or whose login in
  the file changed since the connect, reconnects instead.
- **By itself:** a connection whose login the API server rejects reconnects when its context runs
  no credential plugin and the file holds another login (`Conn.LoginInFile` compares a per-process
  HMAC of the login's kubeconfig entries and the files they name): at the expiry, and when the
  kubeconfig watch (every 2 s) sees the file change; each version of the file is tried once.
- Agents and the CLI may reconnect, except a cluster whose plugin login expired or lapsed
  (`ErrReconnectLoginGUIOnly`): that runs the login program, which may open a browser.
- The window offers it on every connected cluster: the ↻ on its row in the list of all clusters,
  above the Overview's charts, beside Disconnect in the cluster menus, in the connection banner and
  in the palette. Lens has none
  ([beyond Lens](../parity/beyond-lens.md#standby-and-lost-connections)).

**Why:** Without a plugin only the file can renew a login. Making the new connection first keeps a
failed reconnect from disconnecting the cluster; renewing a plugin login in place keeps what a
reconnect relists or ends.

**Rejected:**
- a TLS callback that reads the certificate at each handshake — it covers certificates only, not a
  token, another server or another CA;
- ending the old connection first — a failed reconnect would leave the cluster disconnected;
- a "Reconnect now" item beside Reconnect — two items the user would have to tell apart.

**Code:** [`engine/reconnect.go`](../../internal/engine/reconnect.go),
[`cluster/loginfile.go`](../../internal/cluster/loginfile.go),
[`commands/connection.go`](../../internal/engine/commands/connection.go),
[`fyneui/reconnect.go`](../../internal/ui/fyneui/reconnect.go) · 
`TestReconnectKeepsTheUIStateAndSwapsTheBackend`, `TestReconnectThatFailsKeepsTheConnection`,
`TestReconnectRestartsLogsAndEndsShells`, `TestLogInAgainChoosesHowToRenew`,
`TestARenewedCertificateReconnectsByItself`, `TestReconnectRefusedToAgentsForAnExpiredPluginLogin`,
`TestKindReconnectWithARenewedCertificate`

## D-82: A pre-choice before any integration is looked for, and Ask again

**Status:** built · **By:** owner

Looking for an integration is already a read with the user's identity (the Metrics Server's
discovery, Prometheus's and Alertmanager's probes through the service proxy, the Trivy and Kyverno
report lists). So in the window's session a cluster without a choice of integrations has every one
off (`integrationsOffOf` in [`engine/integrations.go`](../../internal/engine/integrations.go)):
nothing is looked for before the user answers its question
([D-76](#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect)), while
the question is put off with Later (until the next start), or while the cluster is connected in the
background. Only what the question ticks is read, while it asks.

- The question opens on a pre-choice that looks for nothing: **Check all integrations** ticks and
  looks for what the default for new clusters keeps on; **Choose integrations** ticks none and
  looks for each one ticked; **Don't use integrations** warns what goes (metrics, alerts, image
  ratings, policy marks; the resources still show), and its Yes answers with every integration off.
  The rows lead Back to the pre-choice.
- **Ask again** (Cluster Settings › Integrations, `cluster.integrations.ask`) forgets the cluster's
  choice and a Later of this run; nothing is read until the window asks again.
- The metrics line of a cluster without a choice, in the drawers and the Overview, reads "No
  metrics" with a link, Choose integrations…, that asks again.
- Only the user consents: `cluster.integrations.step`, `cluster.integrations.lookFor`,
  `cluster.integrations.answer` and `cluster.integrations.ask` are GUI-only (H-126, H-133); agents
  may run `cluster.integrations.later`.
- `cogpit-cli` keeps no cluster settings: it asks nothing and reads what the default keeps on
  ([H-120](../security/agents-and-mcp.md#h-120-the-clis-headless-engine-does-not-apply-a-clusters-integration-switches)).
  The demo cluster is never asked.

**Why:** Consent has to come before the first look, because the look already reads.

**Rejected:**
- detecting before the answer and reading after it — the detection is itself the read;
- a detection-only mode for a ticked integration — for Alertmanager, Trivy and Kyverno the looking
  is the reading, so it would be a second code path per integration;
- a warning that no resources show without integrations — untrue, and it would push the user to
  Check all;
- Ask again keeping the old choice until the new answer — the reads would go on behind the
  question.

**Code:** [`engine/integrationsconsent.go`](../../internal/engine/integrationsconsent.go),
[`engine/integrationsguionly.go`](../../internal/engine/integrationsguionly.go),
[`commands/integrations.go`](../../internal/engine/commands/integrations.go),
[`fyneui/integrationsask.go`](../../internal/ui/fyneui/integrationsask.go) · 
`TestIntegrationsOffUntilAnswered`, `TestIntegrationsQuestionSteps`,
`TestIntegrationsOnAreTheUsers`, `TestIntegrationsQuestionPreChoice`,
`TestIntegrationsQuestionDontUse`, `TestKindIntegrationsQuestion`

## D-83: One hotbar, and a drag that carries the tile

**Status:** built · **By:** owner

Cogpit has one hotbar, without Lens's banks of twelve and without a limit
([ui-parity › Hotbar](../parity/ui-parity.md#hotbar)): it holds every cluster the user pins and
scrolls when the tiles do not fit. F1–F12 open the first twelve tiles; a shifted F-key is not the
hotbar's. A tile is a 50-point square in a 76-point bar.

- `HotbarState` is one list, and `hotbar.json` has format 2. A format-1 file (banks) is merged
  when read, the first bank's tiles first, then each further bank's, a context once where it first
  appears, and the old file is kept as `hotbar.json.v1`.
- **The drag carries the tile:** past 8 points a lifted copy (1.08 times the tile, with a drop
  shadow, in a layer over the window) follows the pointer, an outline marks the slot a drop gives
  (nearest the copy's top, by the tiles' fixed pitch), and the other tiles slide there in 150 ms.
  The drop is `hotbar.moveCluster`; the dropped order shows until the engine's hotbar arrives, and
  a failed move slides back. A drop off the tiles' area (half a tile right of the bar still
  counts), on the tile's own place or after the bar changed under the drag moves nothing. The bar
  scrolls by itself near its top and bottom edges. With Reduce motion
  ([D-107](#d-107-the-navigator-slides-and-reduce-motion)) nothing animates.

**Why:** A bar filled at will needs no switcher between banks, and carrying the tile shows where it
lands while the others make room.

**Rejected:**
- a limit (12 for the F-keys, 144 for every banks file) — one tile per offered context bounds it;
- ⇧F1–⇧F12 for tiles 13 to 24 — a shifted F-key should not open a cluster unexpectedly;
- moving the tile itself inside the bar — the bar's scroll clips it at its edge;
- taking the slot from the tiles' drawn places — it flips back and forth while they slide.

**Code:** [`engine/hotbar.go`](../../internal/engine/hotbar.go) `mergeBanks`,
[`commands/hotbar.go`](../../internal/engine/commands/hotbar.go),
[`fyneui/hotbardrag.go`](../../internal/ui/fyneui/hotbardrag.go) · 
`TestHotbarMergesBanks`, `TestHotbarCommands`, `TestHotbarFunctionKeys`,
`TestHotbarPastTwelveScrolls`, `TestHotbarDragCarriesTheTile`, `TestHotbarTileDragCancels`,
`TestHotbarDragScrollsAtTheEdge`, `TestHotbarTilesFifty`

## D-84: Undo and redo in the YAML editor

**Status:** built · **By:** owner

The YAML editor ([`yamlview.View`](../../internal/ui/fyneui/yamlview/yamlview.go): the Edit,
Create, Helm Install and Upgrade tabs, a release's values, a Config Map's and a Secret's values in
the drawer) keeps an undo history: ⌘Z undoes, ⇧⌘Z and ⌘Y redo (Ctrl+Z, Ctrl+Shift+Z and Ctrl+Y on
Windows and Linux; Fyne delivers ⇧⌘Z as a `desktop.CustomShortcut`).

- A step is one replacement with the caret and the selection before and after it, which undo and
  redo restore. A run of typing is one step, ended by a pause of 1 s, a caret move, the focus
  leaving, a new line, another kind of edit or 4 KiB; consecutive Backspaces are one step, and so
  are consecutive forward Deletes; a deleted selection, a paste, a cut, an indent change and the
  find bar's Replace or Replace all are a step each.
- It keeps the newest 1,000 steps within 32 MiB of text, the newest step always.
- Text set from outside (an object loaded or applied, Revert, Undo Revert, a draft restored, a
  template, an agent's text, a newer value in an unedited drawer editor) clears it. In an Edit tab ⌘Z with
  nothing to undo is Undo Revert while the tab offers it; the drawer's Revert has no undo.
- It lives in the widget's memory only; a Secret value's history goes when the drawer moves away or
  closes ([credentials](../security/credentials.md#secret-values-edited-in-the-drawer)).
- No command or MCP tool: an undo is an edit like typing (`OnChanged` follows), and agents set a
  text whole (`editor.setText`).
- Fyne's own text fields keep their own undo for ⌘Z and ⌘Y; ⇧⌘Z does nothing there.

**Why:** One ⌘Z takes back what a macOS text view takes back, a typing run, and the pause keeps it
from taking back minutes of typing. A history kept across a text set from outside would let ⌘Z
rebuild an old text on a newer base, and Save or Apply would then overwrite another party's change
without the conflict warning.

**Rejected:**
- a step per character or word — it splits one act into many;
- undo and redo as engine commands — the engine has the text only after a typing pause;
- an Edit menu — Cogpit's menu bar has none, and the keys reach the focused widget without one;
- a wrapper type for every Fyne text field to add ⇧⌘Z — ⌘Y already redoes there.

**Code:** [`yamlview/history.go`](../../internal/ui/fyneui/yamlview/history.go),
[`fyneui/editortab_revert.go`](../../internal/ui/fyneui/editortab_revert.go) · 
`TestUndoTypingRun`, `TestTypingRunBreaks`, `TestUndoRestoresTheSelection`,
`TestTextFromOutsideClearsTheHistory`, `TestHistoryBounds`, `TestUndoRedoRoundTrip`,
`TestEditTabUndoShortcuts`, `TestSecretDrawerKeepsUnsavedInMemory`

## D-85: Dialogs as tables and an object's state without its resourceVersion

**Status:** built · **By:** owner

A dialog that says what is about to happen is a table of rows, and no sentence repeats them
([conventions › Dialogs, reviews and status lines](conventions.md#dialogs-reviews-and-status-lines)).

- A confirmation: Context, Namespace(s), Objects, a row per effect
  ([D-100](#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save)), then
  Requested by; a warning (a chart nothing authenticates) under the rows.
  `ConfirmRequest.Message()` is the engine's one-line text of a request; only tests read it.
- A Config Map's or Secret's save review: Context, Namespace, Object, Changes, Removes (red, bold),
  Adds, and "This cannot be undone." in red under a removal, without explaining what a removed key
  does to a workload; a Secret's Changes row ends "· no value shown".
- Every dialog's Context row shows the context id as it is, its source label in brackets in the
  muted colour (`placeRows`, `contextValue`).
- No window text shows a raw `metadata.resourceVersion`: an edit's state reads as counts ("1 key
  removed"), times ("Saved 1 value at 14:02:05", "loaded at 14:02:05") or "Changed on the cluster
  since your edits began", and the save over a newer version is **Overwrite**. The number stays in
  the engine's results (`baseResourceVersion`) for agents, the CLI, the audit log and drafts. Helm
  revisions keep their numbers, the release's own numbering.
- A suggested file name names the cluster as the window does
  ([D-68](#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)), never its
  kubeconfig file: `<view>-<cluster>.csv` for a table, `<what>-<cluster>.log` for a log, every run
  of other characters one `_`.

**Why:** A table says at a glance what is about to happen, and a place always reads Context ›
Namespace › Object. A `resourceVersion` is one etcd counter for the whole cluster, large and
jumping; it tells a person only whether the object moved since the edit began, which the texts say.

**Rejected:** the number in a tooltip — a status line has none, and nobody reads the number.

**Code:** [`fyneui/confirm.go`](../../internal/ui/fyneui/confirm.go),
[`fyneui/datakeys.go`](../../internal/ui/fyneui/datakeys.go) `reviewHead`,
[`fyneui/objectnames.go`](../../internal/ui/fyneui/objectnames.go) `placeRows`,
[`commands/tables.go`](../../internal/engine/commands/tables.go) `csvFilename`,
[`fyneui/logtab.go`](../../internal/ui/fyneui/logtab.go) `logFileName` · 
`TestSelectionMenu`, `TestContextRowMutesTheSourceLabel`, `TestCSVFilenameNamesTheCluster`

## D-86: The order of an object's YAML

**Status:** built · **By:** owner

Cogpit writes an object's YAML in the order the Kubernetes docs write a manifest
([`model.ObjectYAML`](../../internal/model/objectyaml.go)): `apiVersion`, `kind`, `metadata`
(`name`, `generateName`, `namespace`, `labels`, `annotations`, then the rest sorted), then the
body's fields sorted and `status` last; deeper maps stay sorted. The order is for display only: the
text parses to the same object. It holds wherever an object shows as YAML: the Edit tab
(`EditableYAML`), the drawer's YAML tab, a Secret's YAML, what agents read of a manifest, the audit
log's manifests and a Helm revision's rendered objects; the Create tab's templates start the same
way. Helm values are data, not objects, and keep YAML's sorted order. Drafts, the edited state and
Revert compare texts that `EditableYAML` makes on both sides.

**Why:** A person reads what an object is and where it lives before what the server set, and
`status` last as `kubectl` users read it. A field order per kind would have to know every custom
resource.

**Rejected:** yaml.v3's node API — it writes YAML 1.2, so an unquoted "on" or "yes" in an
annotation would become a boolean when the text is parsed and applied again; instead each
top-level field (and each of `metadata`'s) is marshalled on its own by `sigs.k8s.io/yaml`, quoting
exactly as a whole-object marshal does.

**Code:** [`model/objectyaml.go`](../../internal/model/objectyaml.go),
[`engine/manifest.go`](../../internal/engine/manifest.go) `EditableYAML` · 
`TestObjectYAMLOrder`, `TestObjectYAMLRoundTrip`

## D-87: Cluster Settings: the integrations under a divider, a section each, Metrics their consumer

**Status:** built · **By:** owner

Cluster Settings lists General, Proxy, Terminal, Namespaces, Metrics, Node Shell and Agents, then,
under a divider, the heading "integrations" (itself a section) and a section per integration:
Metrics Server, Prometheus, Alertmanager, Trivy Operator, Kyverno
(`engine.ClusterSettingsSections`).

- An integration's section id is its name (`metrics-server`, `prometheus`, `alertmanager`, `trivy`,
  `kyverno`; `engine.IntegrationSection`), as in `cluster.settings.set`'s `integrations` and
  `cluster.integrations.lookFor`; the heading's section is `integrations`.
- The heading's section holds what concerns all five: the note while the cluster has no choice
  ([D-82](#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)), Ask again, and
  each integration's switch and status with a link to its section.
- An integration's section has the switch "On for this cluster", its status (a found or configured
  Prometheus or Alertmanager names its namespace and service as links that open them) and its
  details: Prometheus's query format, service address, HTTPS and path prefix; Alertmanager's
  service address, HTTPS and path prefix. Details stay editable while the integration is off;
  setting them sends nothing.
- Metrics shows the two integrations it consumes, each with its state's dot, a link to its section
  and its status, then Hide metrics. It has no "Metrics source" dropdown; `cluster.settings.set`'s
  `metricsSource` still sets the two switches, from the window only.
- General shows the kubeconfig's path whole: monospace, wrapped at any character, selectable (⌘A,
  ⌘C).

**Why:** The integrations are Cogpit's own, and each provider's details belong under its
integration; Metrics reads the Metrics Server and Prometheus but does not configure them. An
address set while an integration is off is the only service Cogpit asks once it is on, instead of
probing services by name.

**Rejected:**
- Lens's "Metrics source" dropdown beside the sections — a third control for the same two switches
  ([ui-parity › Cluster settings](../parity/ui-parity.md#cluster-settings));
- greying an off integration's details — it would have to be on, and probing, before its address
  can be set.

**Code:** [`engine/preferences.go`](../../internal/engine/preferences.go)
`ClusterSettingsSections`,
[`fyneui/clustersettings_integrations.go`](../../internal/ui/fyneui/clustersettings_integrations.go),
[`fyneui/clustersettings_integration.go`](../../internal/ui/fyneui/clustersettings_integration.go),
[`fyneui/clustersettings_metrics.go`](../../internal/ui/fyneui/clustersettings_metrics.go) ·
**Tests:** `TestSettingsSections`, `TestIntegrationSections`, `TestIntegrationsSection`,
`TestMetricsSourceFromTheSettings`, `TestKubeconfigPathSelectable`

## D-88: The metrics' source row

**Status:** built · **By:** owner

Under the charts of a pod's and a node's drawer and of the cluster Overview, one row names the
metrics providers, each as "<name> ● <when>": Prometheus (a Thanos or VictoriaMetrics by its name,
[D-71](#d-71-thanos-and-victoriametrics-read-as-prometheus)) left of the centre, "Metrics Server"
right of it, any other provider after them; where they do not fit side by side they stack.

- **Dot and word**, in the cluster dots' colours: green and the age of the last answer as the
  charts' hover writes ages ("now", "30 s ago"); red and "not answering" or "no permission"; grey
  and "not found"; a grey ring and "waiting" while not asked yet. Cluster Settings' integration dots
  ([D-87](#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer))
  use the same colours.
- **Tooltip:** where it runs, its state, what it serves, the last good answer and the last failure
  (a class, never a response body).
- **A link only while it is unreachable:** "Set its address…" after a Prometheus not found by name,
  "Settings…" after any other failure; both open the integration's section of Cluster Settings.
- A provider switched off for the cluster is not on the row; the caption above it only says why
  there are no charts ("Loading metrics…", "No metrics: …"). A cluster whose integrations are not
  chosen yet has the row "Choose integrations…", which runs `cluster.integrations.ask`
  ([D-82](#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)).
- Agents read the same states as `providers` in `metrics.get` and `drawerMetricsProviders` in
  `ui.state`.

**Why:** The least text that says which provider serves the charts and why one does not; a
provider's settings matter only while it fails.

**Rejected:** Lens's prose source line and a permanent "Prometheus…" button — prose that fills the
drawer while everything works; the state word itself as the link — it reads as a state, not an
action.

**Code:** [`metricsstatus.go`](../../internal/ui/fyneui/metricsstatus.go) `sourceRowLayout`,
`providerDot`, `providerLink`; [`hub.go`](../../internal/metrics/hub.go) `Hub.Providers` ·
**Tests:** `TestSourceRow`, `TestSourceChipStates`, `TestSourceRowLinksToTheSettings`,
`TestSourceRowUndecided`

## D-89: How a chart's line is drawn

**Status:** built · **By:** lead (open to revision)

Each run of a series' points (a line between gaps, `seriesRuns`) is one stroke, 1.75 units wide
with round joins and caps, rasterised with rasterx into a `canvas.Raster` at the window's pixel
size; a point alone is a dot. The area under a run is the same path closed along zero, filled
antialiased and fading towards zero, so its edge lies exactly under the line. A chart keeps three
such layers (`chartLayer`), drawn as: fills, a faint glow stroke
([D-110](#d-110-dark-cockpit-cogpits-design-language)), event markers and reference lines, lines.
Each layer sits at whole units and redraws into the same image while its size stays. The hover
refreshes only its overlay's objects (`refreshOverlay`): a refresh of the chart would drop the
layers' textures.

**Why:** Separate `canvas.Line` segments are antialiased each on its own without a join, so the
line's apparent width changes with its slope and where the joints fall; one path keeps it constant
at 1× and 2×. rasterx is Fyne's own rasteriser, already a dependency. A reused image means a poll
allocates nothing of the plot's size.

**Rejected:** an SVG path in a `canvas.Image` — a new SVG text parsed on the main goroutine at every
poll, cached by Fyne under its resource's name; Fyne's `ArbitraryPolygon` (16 points at most) or
`BezierCurve` (one segment) — the joints stay; a dot at each joint — it thickens the joints at 1×;
miter or bevel joins — a miter spikes past a sharp peak, a bevel flattens it.

**Not covered:** at a fractional scale (1.25, 1.5) whole units are not whole pixels, so a layer may
lie up to half a pixel off the pixel grid.

**Code:** [`chartpath.go`](../../internal/ui/fyneui/chartpath.go) `chartLayer`, `seriesPaths`;
[`chart.go`](../../internal/ui/fyneui/chart.go);
[`chart_hover.go`](../../internal/ui/fyneui/chart_hover.go) `refreshOverlay` · 
`TestChartLineKeepsItsWidth`, `TestChartLayerReusesItsImage`, `TestChartFillsUnderTheSeries`

## D-90: A node's disks: a usage bar per real filesystem

**Status:** built · **By:** owner

A node's drawer shows under its chart a row per real filesystem node-exporter reports through
Prometheus, the root first, then by mountpoint: the mountpoint, a small usage bar in the lists' bar
colours (warning from 80 %, error from 90 %, `model.UsageBarStatus`) and the share; the tooltip
holds device, type, used, size and free. The Nodes list's Disk column shows the fullest filesystem,
its mountpoint in the bar's tooltip.

- **Real** excludes filesystems in memory or of the kernel (tmpfs, ramfs, devtmpfs, proc, sysfs,
  cgroup, …), container layers (overlay) and images (squashfs, erofs, iso9660); mounts under
  `/boot`, `/dev`, `/proc`, `/run`, `/sys`, `/snap`; files under `/etc`; and anything below
  `/var/lib/{kubelet,docker,containerd,containers,rancher}/` (a disk mounted at one of those
  directories itself counts). A device mounted twice counts once: at `/` when it is the root, else
  at its shortest mountpoint.
- The rules run in the query's selector and again in Go on the answer, from the same text: the
  answer stays small, and a store that matches expressions differently shows the same filesystems.
- One instant query for every node per poll at the lists' 60-second interval, shared by an open
  Nodes list and a node's drawer. Agents read the fullest filesystem in the Disk cell through
  `view.rows`; the drawer's per-filesystem rows reach no agent tool.

**Why:** A bar reads faster than numbers, and a node's data disk (a `/data` beside the root) is what
runs full. The Disk column answers which node runs out of disk; sorting by it finds the fullest.

**Rejected:** every series node-exporter reports — `/run` everywhere and, by default, a tmpfs per
pod's volume; block devices only (`device` under `/dev/`) — loses network filesystems and LVM or ZFS
devices elsewhere; `/boot` and `/boot/efi` — sized by the installer, nothing about the cluster's
data; the root in the Disk column ([D-43](#d-43-where-the-nodes-disk-comes-from), Lens) — a `/data`
at 95 % under a root at 40 % stays unseen.

**Code:** [`diskfs.go`](../../internal/metrics/diskfs.go) `realFilesystem`, `diskQuery`;
[`usage.go`](../../internal/model/usage.go) `NewNodeDisk`;
[`metricsdisk.go`](../../internal/ui/fyneui/metricsdisk.go);
[`drawerdisk.go`](../../internal/engine/drawerdisk.go) · **Tests:** `TestNodeDiskFilesystems`,
`TestNodeDisksFromNodeExporter`, `TestNodeDrawerShowsTheDisk`, `TestNodesListDiskBudget`,
`TestDemoDisks`

## D-91: The Kubernetes version and upstream end of life

**Status:** built · **By:** owner

Cogpit marks the shown cluster's API server version and its nodes' kubelet versions by upstream
Kubernetes' support dates: muted while supported, the pending colour in the minor's maintenance
mode, the warning colour with "⚠" past its end of life; a kubelet outside the version skew policy
gets "⚠" too. Only upstream's dates count: no managed service's dates, no distribution detected.
What users and agents see: [usage](usage.md#the-kubernetes-version-and-upstream-support).

- **The table** ([`k8seol`](../../internal/model/k8seol/k8seol.go), L0, no I/O) is embedded
  ([`releases.json`](../../internal/model/k8seol/releases.json)) and fetched at build time, never at
  run time: every release job in [`build.yml`](../../.github/workflows/build.yml) runs
  `make k8s-eol K8SEOL_FLAGS=-fallback` ([`hack/k8seol`](../../hack/k8seol/main.go)), which checks
  upstream's files and keeps the committed table when a download or check fails
  ([releasing](releasing.md#the-kubernetes-release-table)). Unreviewed upstream data in a release is
  [H-130](../security/updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed),
  an accepted risk.
- **A minor newer than the table** gets an estimate (`EstimateFrom`): released four months per minor
  after the table's newest, end of life on the 28th of the month 14 months later.
- **The day** is now's in now's zone (`StateAt`); the end-of-life day itself is still supported.
- **The engine** ([`k8sversion.go`](../../internal/engine/k8sversion.go)) sends no request: the API
  server's version is the connect's `GET /version`, then the heartbeat's
  ([D-132](#d-132-a-cluster-upgraded-while-connected)); a kubelet's is its cached Node's.
  `learnVersion` puts Cogpit's own overlay on each connection (`overlays.putOwn`, the seam of
  [D-79](#d-79-kyvernos-policy-violations-on-the-objects)) that marks the Nodes list's Version cell
  (colour, "⚠", a `model.Cell.Tip` tooltip); `MarkKubeletVersion` marks the node drawer;
  `UIState.ClusterVersion` feeds the navigator root's third line
  ([`clusterversion.go`](../../internal/ui/fyneui/clusterversion.go)).

**Why:** A minor past its end of life gets no fixes, and a kubelet outside the skew policy is
unsupported. A table built into each release needs no connection from the app; the estimate covers a
build older than a new minor. Upstream's maintenance mode is the boundary of the softer mark, not a
number of Cogpit's.

**Rejected:**
- fetching at run time — a new outward connection; endoflife.date — a third-party mirror;
- a scheduled workflow committing the table to `main` — a bot pushing to `main`, while every release
  fetches its own;
- failing a release on a failed fetch — a GitHub hiccup must not stop a release;
- the kubelet's mark in the warning column — that column is the node's health;
- the version beside the user or the name — the navigator's default width cuts either.

**Code:** [`k8seol.go`](../../internal/model/k8seol/k8seol.go) `SupportOf`, `KubeletSkew`;
[`k8sversion.go`](../../internal/engine/k8sversion.go) `learnVersion`, `versionCheck` · 
`TestSupportOf`, `TestStateAtTheEdges`, `TestKubeletSkew`, `TestEstimateRuleAgainstUpstream`,
`TestCompare`, `TestNodesVersionCheck`, `TestKindClusterVersion`

## D-92: A gear in the navigator's cluster row

**Status:** built · **By:** owner

The navigator's root row carries the theme's settings icon before its connection dot, where a
section row has its ↻: a click opens the shown cluster's Cluster Settings (`cluster.settings.open`),
its tooltip "Cluster Settings (⌘⇧,)" (Ctrl+Shift+, on Windows and Linux). It shows with a cluster
and hides without one. Its room cuts a long username at the navigator's default width; the cut
name's tooltip holds it whole (`subtitleTip`). The All Clusters section's rows get no gear.

**Why:** The settings of the cluster the user works in, one visible click away.

**Rejected:** a gear on every All Clusters row — repeats each row's Settings… menu item down a long
list and narrows every name; a gear on hover only — an icon must be seen to be found.

**Code:** [`clusterversion.go`](../../internal/ui/fyneui/clusterversion.go) `newRootSettings`;
[`navigator.go`](../../internal/ui/fyneui/navigator.go) `newNavRootRow` · 
`TestRootRowGearOpensClusterSettings`

## D-93: Room in the data footer and the layout bar, and a full-row back button

**Status:** built · **By:** owner

- A Config Map's or Secret's data section has the theme's inner padding between its last key and
  its Add Key row and footer (`dataGap`), and the same under an open Add Key row; a padding
  separates the new key's name field from Add.
- The three layout toggles at the top bar's right stand a padding apart.
- At a cluster, "‹ All Clusters" fills the navigator header's row up to the search button: its
  hover, click and tooltip cover the row; ‹ and the label stay at the left.

**Why:** Space, not a line, separates the footer from the last value editor; toggles a padding apart
read as a group yet are hit one by one; a click anywhere left of the search button means "up".

**Rejected:** a divider above the data footer — a line competing with the value editors' frames.

**Code:** [`datakeys.go`](../../internal/ui/fyneui/datakeys.go) `dataGap`, `newKeyAdder`;
[`layoutbar.go`](../../internal/ui/fyneui/layoutbar.go);
[`navigator_header.go`](../../internal/ui/fyneui/navigator_header.go) · 
`TestRenderConfigMapDrawerFooterDark`, `TestRenderNavigatorBackDark`
(asserts the button ends at the search button)

## D-94: The empty dock's toggle opens a terminal, and alert names copy

**Status:** built · **By:** owner

- `ui.toggleDock` (⌘J, View › Dock, the dock's layout toggle) on a dock without a tab opens a
  terminal tab as `terminal.open` does, the dock opening with it; the toggle's tooltip then reads
  "Open a Terminal". It is disabled only where no terminal can open (no cluster connected, the demo
  cluster). The rule is the same for every origin.
- An alert card's alertname on the alert board has the lists' copy icon
  ([D-25](#d-25-copying-values)) right after the name, shown while the pointer is on the card's head
  or the icon, its room kept so the head does not move. An answer that shows what the board shows
  rebuilds nothing (`sameBoard`), and a board rebuilt under the pointer shows the icon of the card
  under it at once (`alertBoard.hoverKey`), since a head rebuilt under a still pointer gets no
  MouseIn. Only the alertname copies: the summary is a tooltip, the namespace and object link to
  drawers that copy them.

**Why:** A toggle that does nothing on an empty dock reads as broken. `terminal.open` is a UI
command agents run freely already ([D-2](#d-2-agent-interface-scope)), so the toggle gives them no
new power. An icon always shown on every card would add a column of noise.

**Rejected:** splitting the toggle by origin — `ui.state` and the window would disagree on what ⌘J
does.

**Code:** [`commands/layout.go`](../../internal/engine/commands/layout.go);
[`alertboard.go`](../../internal/ui/fyneui/alertboard.go) `cardCopy`, `sameBoard` · 
`TestLayoutToggles`, `TestDockToggleOpensATerminal`, `TestAlertNameCopy`,
`TestAlertNameCopyAcrossARebuild`

## D-95: The window comes back where it was

**Status:** built · **By:** owner

The window opens with its last size, on its last display at its last place, and in full screen if
it was, whatever Preferences › App › Restore the last session says. What the user sees per case:
[usage](usage.md#the-windows-place-and-size).

- **The file:** `window.json` in Cogpit's configuration directory, owner-only and replaced
  atomically (`engine.WritePrivateFile`). On macOS it holds the frame in AppKit's screen
  coordinates, the display (id, name, frame) and full screen with the frame it leaves full screen
  to, written half a second after the last change and at quit. A file that does not load, or a newer
  version's, gives the first start's window and is replaced at the next change; a frame under
  320 × 200, not finite or beyond a million is ignored.
- **No jump at start (macOS):** the kept frame is fitted to the displays (`placeFrame`: the display
  by id, else by a name only one display has, else the one the frame overlaps most) and given to
  Fyne as the content's size and to GLFW as its position (`glfwPlacement`) before the window exists;
  once Cogpit's title bar took its room, the frame is set exactly and followed
  ([`windowplace_darwin.m`](../../internal/ui/fyneui/windowplace_darwin.m)). Full screen waits until
  Cogpit is the active app, since AppKit abandons an inactive app's transition.
- **Windows and Linux** keep the canvas's size and full screen when the window closes
  ([`windowplace_other.go`](../../internal/ui/fyneui/windowplace_other.go)): Fyne cannot read a
  window's position there.
- Not engine state: no command, no `ui.state` field. A `--perf` run opens at 1440 × 900 and keeps
  nothing, so its measurements compare.

**Why:** macOS apps come back where they were. The frame names no cluster and sends no request, so
the restore preference's reason (never surprise a cluster) does not apply. Where the window sits is
the user's desktop, not what Cogpit shows: an agent moving it would be a surprise.

**Rejected:** a field in `workspace.json` ([D-41](#d-41-what-survives-a-restart)) — that file
follows the restore preference, and the engine would carry a field it never reads; AppKit's frame
autosave — no full screen, no rule for a missing display, nothing on Windows and Linux, and tests
would write the developer's defaults; writing at quit only — a crash loses the run's place; setting
the frame only after the window shows — it opens centred and jumps.

**Code:** [`windowplace.go`](../../internal/ui/fyneui/windowplace.go) `placeFrame`, `keptFrameOn`,
`glfwPlacement`, `openWindowPlace` · **Tests:** `TestPlaceFrame`, `TestGLFWPlacement`,
`TestWindowPlaceKeeper`, `TestWindowPlaceKeeperUnreadable`, `TestWindowPlaceFollowsTheWindow`
(macOS, a native harness in a process of its own)

## D-96: The service proxy's port spelled by its number first

**Status:** built · **By:** owner

RBAC grants `services/proxy` by `resourceNames` matching the proxy's name
`[scheme:]<service>[:<port>]` as the URL writes it. Prometheus' and the Alertmanager's detection ask
each candidate Service under its port's number first, then its name (`Candidate.Ports`):

- a 401 or 403 tries the next spelling, then the next candidate; any other answer ends that
  candidate, since the other spelling reaches the same server; at most three candidates, so at most
  six probes;
- without a Prometheus found, a refusal makes the state forbidden, naming every refused target
  ("…:9090 nor …:http-web");
- the target found keeps the spelling that answered; an address the user set is asked as set.

The test fake's service proxy resolves a port's number and name to the same handler, as the API
server does (`FakeAPIServer.proxyBySpelling`).

**Why:** Roles written for Lens grant the port's number, which Lens writes; spelled by name, the
same Prometheus would be refused.

**Rejected:** the number only — breaks roles that grant only the name; leaving the code and having
the user set the address — every cluster with such a role would need it.

**Not covered:** each refused spelling is a 403 in the cluster's audit log, at every detection while
nothing is granted.

**Code:** [`promdetect.go`](../../internal/metrics/promdetect.go) `portForms`, `PrometheusDetector`;
[`alerting/poller.go`](../../internal/alerting/poller.go);
[`fakeapi.go`](../../internal/testkit/fakeapi.go) · 
`TestDetectTriesThePortsNumberThenName`, `TestDetectARefusedCandidateDoesNotStopTheNext`,
`TestDetectForbiddenIsAState`, `TestPollerTriesThePortsNumberThenName`

## D-97: A connect goes on when another cluster is shown; a failed one stays

**Status:** built · **By:** owner (a failed connect stays); lead (open to revision) for the rest

What the user sees: [usage](usage.md#several-clusters-connected).

- **A failed connect stays:** the window stays on the failed cluster (`UIState.Context` names it,
  `Connection` is `error`), its error page
  ([D-114](#d-114-a-failed-connects-error-page-and-its-stages)) over the
  workspace; the cluster shown before stays connected behind it. The window never shows another
  cluster by itself.
- **Every connect is a run of its own** (`connectRun`): the window's connect starts one, or joins
  the one under way for that context, and waits; the restore's starts one nobody waits for. Showing
  or connecting another cluster ends only the wait: the run goes on in the background, `connecting`
  in `Connections`, cancellable. A run the window stopped waiting for ends with a toast: connected,
  it joins the pool as a cluster the window left ([D-69](#d-69-several-clusters-connected-at-once));
  failed, its entry goes; a cancel says nothing. The caller's own context ending (an MCP request
  given up) ends the run.
- **The same cluster again** joins: a connect of a context whose run is under way joins the run, so
  no second login races the first; a connect of the cluster the window's connect waits for joins
  that wait (`windowConnect`) and gets its outcome.
- **Outcomes that are no failure** are typed: `ErrConnectInBackground`, `ErrConnectCancelled` (also
  when the run ended at the moment of the cancel), `ErrConnectSuperseded` (a newer connect or show
  came before this one began). `engine.ConnectNotFailed` matches them, and the window raises no
  toast for them (`showError`). `cluster.connect` and `hotbar.openCluster` answer a background
  connect with `{context, state: connecting}` (`connectAnswer`); `ui.tab.select`, `ui.tab.next`,
  `ui.tab.previous` and `ui.setNavigatorScope` with the UI state.
- **Agents** get a cancel as MCP's error code `cancelled`, telling them not to connect again unless
  the user asks; a connect superseded before it began is `cancelled` too, saying nothing failed. An
  agent's `cluster.disconnect` of a waiting connect counts as the user's Cancel Connecting.

**Why:** Ending a browser login or an MFA push the user is in the middle of costs the login, and a
question at every switch is noise. A window that jumps to another cluster after a failure lets the
user act on the wrong cluster unnoticed. The cluster's dot and its connection log already say that a
connect was cancelled or left.

**Rejected:** ending the waiting connect on a switch — loses the login; asking first — noise at
every switch; an error toast for a cancel or a background connect — repeats the dot; a cancel
reported to agents as `failed` — invites a retry of what the user stopped.

**Code:** [`connect_run.go`](../../internal/engine/connect_run.go) `connectRun`, `awaitRun`,
`ConnectNotFailed`; [`connect_join.go`](../../internal/engine/connect_join.go) `windowConnect`;
[`commands/cluster.go`](../../internal/engine/commands/cluster.go) `connectAnswer`;
[`mcp/tools.go`](../../internal/mcp/tools.go) · 
`TestShowingAnotherClusterLeavesTheConnectRunning`, `TestAFailedConnectStaysOnItsCluster`,
`TestACancelledConnectIsNoFailureOnEveryPath`, `TestAConnectOfTheSameClusterJoinsTheWait`,
`TestConnectOutcomesAreNoFailures`, `TestConnectOutcomesThatAreNoFailureRaiseNoToast`

## D-98: What holds a Terminating namespace, and Remove finalizers

**Status:** built · **By:** owner

A Terminating namespace's drawer lists what holds its deletion ("Blocking Deletion"), and a
destructive **Remove Finalizers** (`object.removeFinalizers`) frees one object. Neither touches a
namespace's own finalizer or calls `/finalize`. What the user sees:
[usage](usage.md#a-namespace-stuck-in-terminating).

- **Source:** the namespace controller's conditions on the Namespace name kinds and finalizers with
  counts, never objects; anyone who may read the namespace reads them (`kinds.NamespaceDeletionOf`).
- **Reads:** per named kind, one metadata-only watch of that kind in that namespace, held while the
  drawer shows the namespace and the condition names the kind (`drawerWatch.blockers`); `object.get`
  reads for the call. A kind the namespace's rules review denies is a counted row without a request.
  Rows: the objects with a deletionTimestamp and finalizers, at most 100.
- **Remove Finalizers** applies only to an object being deleted that holds finalizers, never to a
  namespace or a custom resource definition; Destructive for every origin, verb `patch`. The write
  is one JSON patch (`cluster.FinalizersPatch`): `test /metadata/uid`, `test /metadata/finalizers`
  equal to the finalizers the confirmation showed (`engine.ConfirmedFinalizers`),
  `remove /metadata/finalizers`; a failed test is `ErrTargetsChanged`.

**Why:** A metadata-only watch per named kind keeps Secret values off the wire, never lists a kind
cluster-wide for a drawer, and lets an object whose finalizers go vanish by itself. The JSON patch
refuses exactly what the user did not see (another object under the name, a finalizer added or
removed since) and nothing else. A definition's `customresourcecleanup` finalizer is the API
server's and guards its custom resources in etcd, as a namespace's own guards its content.

**Rejected:** the Namespace profile's `DetailWith` — its `DetailNeeds` are fixed per profile, while
the kinds come from the condition at run time; a merge patch `finalizers: null` with the
resourceVersion — a controller's status update meanwhile would refuse it.

**Not covered:** what a finalizer guards, which Cogpit cannot know
([H-9](../security/privilege-footprint.md#h-9-force-finalize-skips-cleanup)). Removing from the
list with no view of the kind open binds the UID through the kind's own cache, as Delete does,
which lists the kind as its view would.

**Code:** [`nsblockers.go`](../../internal/engine/nsblockers.go);
[`engine/finalizers.go`](../../internal/engine/finalizers.go) `RemoveFinalizers`;
[`cluster/finalizers.go`](../../internal/cluster/finalizers.go) `FinalizersPatch` · 
`TestNamespaceDeletionFromTheControllersConditions`, `TestTerminatingNamespaceListsWhatBlocksIt`,
`TestRemoveFinalizersSendsTheGuardedPatch`, `TestRemoveFinalizersConfirmsWhatItRemoves`,
`TestRemoveFinalizersRefusals`, `TestBudgetTerminatingNamespaceBlockers`,
`TestKindTerminatingNamespaceBlockers`

## D-99: Selecting and copying a log tab's text and a toast's

**Status:** built · **By:** owner

Log tabs, toasts, the bell's notifications and the agent's column select and copy text as the
drawer does ([D-25](#d-25-copying-values)). A selection is the window's alone: no command, no
audit line; agents read these texts through `dock.read` and `notifications.list`. Copies are plain
copies through `shell.copyText`, which cancel a pending clearing of a copied Secret value.

- **Log tab:** a range in the lines (a line's number and a byte offset into its shown text), taken
  by `logSurface` over the list. Drag, ⇧-click, double click, ⌘A, ⌘C, Escape, and Copy, Select All
  and Copy All Lines Shown in the context menu. A copy is the lines as the tab shows them, as
  Download saves them. The selection stays on its lines through new lines, Wrap, a search and a
  filter; lines the scrollback drops take their part; toggling names, timestamps or the time
  format drops it. A pointer selection turns Follow off; unused keys go on to the window; search
  matches use `findColor` (the warning hue, translucent), not the selection colour.
- **Toasts and notifications:** the text is a `selParagraph`, selected toast text in its own token
  `cogpit.toastSelection`. A copy icon left of a toast's × (on a notification row while hovered)
  copies the message as it came and shows a checkmark for 1.5 s. A toast stays while the pointer
  is on it or its icons, or while its text is selected.
- **Agent's column:** block by block (a paragraph, heading, quote, list item, table cell, code
  block, note, a tool row's arguments, result or error); a click on a link opens it.
- **Follow by scrolling:** a user's scroll up (wheel, trackpad, scroll bar; not the view's own,
  `logList.quietly`) to 5 rows or more above the newest line (`followOffRows`) turns Follow off;
  less keeps following that far behind (`Logs.under`). A scroll down that ends with the last row
  at least half in view turns it on; new lines never do.

**Why:** The list recycles its rows as it scrolls, so a selection held by a row would move with
it. Snapping back after a small scroll up would fight the user (a macOS wheel notch is half a
row); Follow ticked always means a moving view.

**Not covered:** ⌘A in a toast's or the drawer's text (Fyne's desktop driver hands a disabled
selectable text only Copy); a selection ends when the tab's stream opens again.

**Code:** [`logselect.go`](../../internal/ui/fyneui/termview/logselect.go),
[`logsfollow.go`](../../internal/ui/fyneui/termview/logsfollow.go),
[`toastcopy.go`](../../internal/ui/fyneui/toastcopy.go),
[`chat_select.go`](../../internal/ui/fyneui/chat_select.go) · 
`TestLogSelectionKeepsItsLines`, `TestLogSelectionKeysGoOn`, `TestToastTextSelectable`,
`TestNotificationRowCopy`, `TestChatTextSelectsBlockByBlock`, `TestLogsFollowOffByScrollingUp`

## D-100: Objects and effects in one form, and a drawer's edited state beside Save

**Status:** built · **By:** owner

- Every dialog names an object with kind and name apart: the kind muted with an arrow, the name
  bold, each in its own column (`objectNames`, `objectsRow`); plain text such as a title reads
  "Config Map → app-config" (`objectText`). Several objects are grouped by kind, the kind once per
  group; the row reads "Object" or "Objects (12)". `placeRows` adds Context › Namespace › Object.
- A confirmation states what a command does beyond its objects as rows, never as a sentence:
  `Spec.Effects` returns `engine.Effect` rows (a short name, a terse value and a tone: plain;
  names bold; lost bold in the error colour; warning in the warning colour), such as a save's
  Removes, Adds and Changes, a rollback's To revision, a drain's Loses and Evicts.
  `ConfirmRequest.Detail` is their text (`EffectsText`), to which the Rollback dialog and the
  drawer's save review bind the answer; a request with a Detail and no Effects shows it in a row
  "Effect".
- A Config Map's or Secret's drawer shows its edits as tinted pills between Add Key and Revert:
  "2 values edited" amber, "1 key added" green, "1 key removed" red, at the buttons' text size, on
  a line of their own when the drawer is narrow (`editCounts`, `dataFooterLayout`); the line under
  the footer says the rest (read-only, a newer version, saved).

The dialog rule:
[conventions › Dialogs, reviews and status lines](conventions.md#dialogs-reviews-and-status-lines).

**Why:** A key in a cell of its own never reads as a word of a sentence; colour and weight set the
kind apart where the arrow is not read. The pills sit where the eye goes before Save, in the
colours a diff marks lines with.

**Rejected:** a chip per object — twelve pods would be twelve badges; a count in Save's label —
hides which kind of change.

**Not covered:** the port-forward dialog's title and the create dialogs name no kind; the Edit
tab's title and the drawer's head keep their own form.

**Code:** [`objectnames.go`](../../internal/ui/fyneui/objectnames.go),
[`confirm.go`](../../internal/engine/confirm.go) `Effect`,
[`datakeys.go`](../../internal/ui/fyneui/datakeys.go) `editCounts` · 
`TestObjectNamesSetTheKindApart`, `TestConfirmationEffectsAreRows`,
`TestConfirmationShowsADetailWithoutEffects`, `TestDataFooterLayout`, `TestDataKeysDetail`

## D-101: The kubeconfig sources at hand, and a removed cluster leaves the hotbar

**Status:** built · **By:** owner

- The navigator's Local Kubeconfigs row has a "−" at the right of its title line that opens
  Kubeconfig Sources…, where each source is removed after a confirmation
  (`navSectionRootRow.sources`); the row's context menu has Add Kubeconfigs… and Kubeconfig
  Sources….
- Removing a source (`kubeconfig.remove`) or a cluster (`cluster.remove`) takes the tiles of the
  contexts no source offers any more off the hotbar (`Session.dropHotbarTilesOfSource`), as it
  closes their tabs and forgets their settings; a session source (`$KUBECONFIG`) too. A kubeconfig
  file that disappears by itself keeps its tile, marked missing.

**Why:** Removing a source is the user's act, meant to take its clusters; a file that vanished may
come back (a sync tool, an editor's save by rename, an unmounted volume), and its tile keeps its
place. A source can hold several clusters, so the "−" opens the dialog instead of removing one.

**Code:** [`navigator_sections.go`](../../internal/ui/fyneui/navigator_sections.go),
[`contextlink.go`](../../internal/engine/contextlink.go) `RemoveKubeconfigSource` · 
`TestRemovedSourceLeavesTheHotbar`, `TestSectionRootMinusOpensTheSources`,
`TestNavigatorSectionRowMenu`

## D-102: Debug containers added from the drawer, shown in blue, reached by Logs, Shell and Attach

**Status:** built · **By:** owner

`pod.debug` (Debug, after Shell in a pod's menus) adds an ephemeral container to a running pod as
`kubectl debug -it --target` does and opens the pod's Attach tab on it, for every origin.

- **Destructive for every origin:** an ephemeral container cannot be removed or changed and lives
  as long as the pod object; with a target it sees that container's processes and files
  ([H-134](../security/privilege-footprint.md#h-134-a-debug-container-stays-in-the-pod-and-sees-its-target)).
  Greyed with the reason on a pod that has ended or is being deleted.
- **One dialog, the confirmation** ([`debugdialog.go`](../../internal/ui/fyneui/debugdialog.go)):
  Context › Namespace › Pod, Target (the pod's containers, the first preselected, or None), Sees,
  Image (editable), Digest, "This cannot be undone."; the answer is bound to the pod's UID, the
  target and the image. Agents and the CLI get the engine's confirmation with the same rows.
- **The write:** `debugger-<5 lowercase characters>` with stdin, a TTY, the image's own command and
  `targetContainerName` (`engine.DebugContainer`), a strategic merge patch on the
  `ephemeralcontainers` subresource with the pod's UID as `metadata.uid`, so a pod created again
  under the name is refused (`cluster.ErrReplaced`). Debug waits up to 30 s for the pod's watch;
  the Attach tab waits through the cache up to 3 minutes for the container to start.
- **The image:** app-wide (Preferences › Kubernetes), set by the user only; agents and the CLI may
  reset it (`ErrDebugImageGUIOnly`, as for the node shell, D-38). Default: `nicolaka/netshoot:v0.16`
  pinned by digest (`engine.DefaultDebugImage`, moved by Renovate). A field shows the short name
  (`engine.ShortImage`), which stands for the configured image with its digest; other text runs as
  typed, a tag without a digest marked floating (`engine.ImageFromField`).
- **The Pods list:** ephemeral containers' squares follow the regular ones, live first: blue
  (`model.StatusEphemeral`) while running or starting, red while waiting on an error, a blue
  outline once finished (`CellMark.Hollow`); at most 12 squares a cell, the rest "+N". They count
  in neither READY, RESTARTS nor STATUS, as in kubectl. A pod's log tab lists them apart.

**Why:** One dialog that is the confirmation shows each value once (D-85). A finished debug
shell's exit code says how its last command ended, not that the pod is unwell. A field left at the
default stores empty, so it follows the pinned default as Renovate moves it.

**Not covered:** a command of its own (kubectl's `-- <command>`); stopping it when its tab closes.

**Code:** [`engine/debugcontainer.go`](../../internal/engine/debugcontainer.go),
[`cluster/debugcontainer.go`](../../internal/cluster/debugcontainer.go) · 
`TestDebugNeedsEveryOriginsConfirmation`, `TestAddEphemeralContainerSendsTheGuardedPatch`,
`TestImageFromField`, `TestEphemeralContainerMarks`, `TestKindDebugContainer`

## D-103: Fyne's thread check, a build parameter, off in releases

**Status:** built · **By:** owner

`FYNE_THREAD_CHECKS=on|off` in the [`Makefile`](../../Makefile) chooses safety or speed per build;
empty is the target's default: `on` for `build`, `app`, `run`, `run-demo`; `off` for `dist-app`,
`dist-linux` and `dist-windows`, so every release. `off` adds the build tag `nothreadcheck`, and
`fyneui.Run` then declares the app migrated to `fyne.Do` in its metadata (`appMetadata`), which
turns Fyne's main-goroutine check off and keeps Fyne's caches thread-safe.

- The releases' way back is the repository variable `FYNE_THREAD_CHECKS=on`, which
  [`build.yml`](../../.github/workflows/build.yml) passes to the dist targets. The dist targets
  fail unless both binaries recorded `nothreadcheck` exactly when the check is off.
- The app log names the mode at every start, About when a build is not its kind's default. A
  development build with the check on shows each place Fyne reported off the main goroutine once,
  as a warning toast ([`threadcheck.go`](../../internal/ui/fyneui/threadcheck.go)).
- With the check off, a Fyne call from another goroutine races and a `fyne.DoAndWait` on the main
  goroutine never returns. Every place below `internal/ui` that runs off the main goroutine is
  audited in [ui-threads.md](ui-threads.md); `TestEveryGoroutineSiteIsAudited` fails on an
  unaudited `go` statement, timer or cgo export, and the main-loop stand-ins of `termview` and the
  terminal fork fail on a `fyne.DoAndWait` made on the loop.
- The window's close (the close button, `a.Quit`, ⌘Q) stops the shell's posts and the dock's
  terminals (`termview.Terminal.Stop`) before Fyne drains its queue, after which a `fyne.Do` would
  run at once on the caller's goroutine.

**Why:** Fyne asks on every canvas refresh which goroutine it runs on (a `runtime.Stack` read)
unless the app declares the migration. Scrolling the demo cluster's Pods list at 10,000 px/s drew 6
frames a second with the check (a frame's work p50 184 ms), 60 without (7.9 ms).

**Rejected:** Fyne's own tag `migrated_fynedo` — it also makes Fyne's caches and queues
single-threaded, so a stray call could end the app; a run-time flag — the mode is chosen per build.

**Not covered:** Fyne draws on a fixed 60 Hz timer, not at the display's rate
([G16](../parity/gaps.md#g16-stability-and-the-user-bug-backlog)); a SIGINT or SIGTERM that Fyne's
own handler takes quits without the window's close.

**Code:** [`fyneui.go`](../../internal/ui/fyneui/fyneui.go) `appMetadata`,
[`buildinfo.go`](../../internal/buildinfo/buildinfo.go) · **Tests:** `TestTheThreadCheckTag` (the
Makefile's tag against `internal/buildinfo`'s, and Fyne still reading the `fyneDo` migration),
`TestEveryGoroutineSiteIsAudited`

## D-104: Dialog fields, hints, dropdowns and button outlines

**Status:** built · **By:** owner

- **Hints:** a muted "?" after a field's name; its name cell shows the hint as a tooltip after
  450 ms, a click at once, in a layer inside the dialog (`fieldName`, `newDialogTooltip`). A hint
  is a person's text in the command (`DialogField.Hint`), else the schema's description;
  `TestDialogHintsSpeakToAPerson` refuses "--", "kubectl", "default" and "required to run" in it.
- **Sliders:** `DialogField.SliderMin` and `SliderMax` give an integer parameter a slider inside
  the schema's bounds. Scale's replicas: 1 to 30, bound to a five-digit count field that takes any
  value; 0 is only typed and is confirmed as destructive.
- **The frame:** a generated dialog is a `dialogFrame` (Fyne's dialog rebuilt), its rows a table
  (D-85), a number field as wide as its largest value. A title naming an object is two texts,
  "Scale Deployment →" and " gate-web" (`objectDialogTitle`).
- **Dropdowns:** `selectField` embeds Fyne's Select and opens its own list over the field, the
  current entry at the field's place with a check. Escape closes the list and takes the keyboard
  off the field. Editable dropdowns keep Fyne's list below the field.
- **Outlines:** dropdowns, text buttons (`textButton`), a content surface's icon buttons
  (`tipButton`) and neutral plated buttons (`newPlateButton`) draw the entry's outline
  (`theme.ColorNameInputBorder`, at least 2:1 on every surface), primary while focused; coloured
  plates, the chrome's icon buttons and the search switches stay bare (`newBareTipButton`). A
  button with a word keeps 4 points beside it (`textButtonSide`).
- **Secret values:** Show (amber, an eye) and Hide (green, the eye struck through) are one widget
  as wide as the wider (`setRevealLook`), keeping the pointer's hover when it turns; a key line's
  buttons, b64 included ([D-25](#d-25-copying-values)), are one height.
- **Carets:** a button that opens a list shows `chevronDownIcon`, at the right edge of the list
  filters (`textButton.iconEnd`); switches side by side (Aa, ab, .*) are one width (`oneWidth`).

**Why:** The schema speaks to agents; a person's hint in the command keeps the window data-driven.
Fyne's Select shows the current entry twice; Fyne shapes a title as one text, drawing the space
after the symbol font's arrow as a wide missing glyph; the shell's tooltips lie under overlays.

**Not covered:** the "?" takes no keyboard focus; a single text with a space after an arrow keeps
the gap; buttons in Fyne's own dialogs keep Fyne's plate.

**Code:** [`paramdialog.go`](../../internal/ui/fyneui/paramdialog.go),
[`selectfield.go`](../../internal/ui/fyneui/selectfield.go),
[`textbutton.go`](../../internal/ui/fyneui/textbutton.go) · **Tests:** `TestScaleSliderAndCount`,
`TestParamHintIsATooltip`, `TestSelectOpensOverTheField`, `TestEscapeOnAnOpenDropdown`,
`TestButtonOutlineAndRoom`, `TestSecretShowTurnsIntoHideUnderThePointer`

## D-105: Reverse video as a state, and a key's modifiers from the key itself

**Status:** built · **By:** lead (open to revision)

Three patches of Cogpit's copy of fyne-io/terminal
([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)):

- **Reverse video is a state**, as in ECMA-48 and xterm: SGR 7 sets it, SGR 27 and SGR 0 clear it,
  and a character written while it is on takes the current colours swapped (`cellColors`); a
  default colour reversed is upstream's pair (the theme's text colour as the background,
  `DisabledButton` as the text). An erase takes the current colours unswapped, as xterm erases.
- **A key's modifiers come from its own event**: the desktop driver's `CurrentKeyModifiers`, which
  Fyne sets from the event before it hands the key on (`keyModifiers`). The widget keeps no
  modifier state; a drag's block selection takes ⌥ from its press (`pressAlt`).
- **⇧ and Ctrl with a named key send xterm's sequences**, as the terminfo entry Cogpit sets
  (`TERM=xterm-256color`) names them (`typeModifiedKey`): `CSI 1;m A`–`D`, `H`, `F`, `P`–`S` and
  `CSI n;m ~` (m = 2 with ⇧, 5 with Ctrl, 6 with both), `CSI Z` for ⇧Tab. A key without such a
  sequence goes as without ⇧; with Ctrl, Return and Enter send CR and Backspace its usual byte,
  Tab and Escape nothing. A named key with ⌥ sends nothing.

**Why:** zsh marks a paste with SGR 7 and repeats it part way or before every character
(oh-my-zsh, zsh-syntax-highlighting), so a toggle draws every other character plain. A modifier
state the widget tracks itself stays held when the key's release reaches another window or
overlay. Upstream's Ctrl with a named key sends the control character of its name's first letter
(Ctrl+↓ is `^D`, which ends a pod shell at an empty prompt).

**Rejected:** ignoring a second SGR 7 — SGR 27 still drops the colours set before it; resetting
tracked modifiers on a focus change — nothing tells the widget that a key went up elsewhere;
upstream's `ESC [ D ; 2` — no program reads it as one key, a shell types `;2`.

**Not covered:** a default colour drawn reversed keeps the theme's colour of the moment it was
written, across a switch between dark and light.

**Code:** [`color.go`](../../third_party/fyne-terminal/color.go) `cellColors`,
[`input.go`](../../third_party/fyne-terminal/input.go) `keyModifiers`, `typeModifiedKey`,
`TypedShortcut` · **Tests:** `TestReverseVideo`, `TestRenderTerminalPasteHighlightDark`,
`TestTypedKey_ShiftReleaseNeverSeen`, `TestKeyModifiers_FromTheDesktopDriver`,
`TestDragged_BlockFromThePress`, `TestTerminal_TypedKey`, `TestTerminal_TypedShortcut`

## D-106: A click below the rows, ⌘F in editor tabs, moving tabs, a deleted namespace

**Status:** built · **By:** owner

- **A click below a list's rows** closes the drawer (`ui.closeDrawer`) and keeps the checked rows,
  which are built up one by one for a bulk action. The table is no `fyne.DoubleTappable`, so Fyne
  does not hold its clicks for the double-click interval; it counts the header separators' double
  click itself. The welcome page's cluster rows take clicks only, and `welcomePage.connect` skips a
  cluster whose connect is under way, so a double click connects once.
- **⌘F in the dock's editor tabs** (Edit, Create, Helm Upgrade, Helm Install) opens the shown
  tab's find bar, never closes it, when the keys are in that tab or nowhere, as IDEs route find;
  else the list's search or the drawer's YAML find ([D-127](#d-127-find-in-the-drawers-yaml)).
  macOS claims it before the focused widget (`windowKey.claim`); Windows and Linux take the
  editor's `OnFind`, else the window's shortcut (`shell.searchKey`).
- **Tabs move** by a drag, by `ui.tab.move` and `dock.move` (an index among the tabs the bar shows;
  hidden tabs keep their places), and by `ui.tab.moveLeft` and `ui.tab.moveRight` (⌘⌥⇧←, ⌘⌥⇧→;
  `dock: true` while the keys are in the dock, `shell.keysInDock`): the order is UI state, so every
  move is a command agents see. The drop slot comes from the other tabs' widths, not from where
  they are drawn; a drop off the strip or after the tabs changed moves nothing. The tab bar's order
  is kept with its tabs ([D-41](#d-41-what-survives-a-restart)); dock tabs are not.
- **A deleted or Terminating namespace** shows in the namespace filter's list in the error colour
  with "· deleted" or "· terminating" (not colour alone), the filter's button red while it names
  one; unticked, a deleted one leaves the list. The marks come from the cache (`resources.list`,
  paged by `offset`) when a cluster is shown, when the filter opens and on `EventNamespaces`: while
  the filter names namespaces the user may list (`Backend.Listable`), the engine watches them and
  sends it, at most every 500 ms, when one comes, goes, or begins or ends terminating. Nothing is
  marked while the user may not list namespaces or before they are read.

**Rejected:** a ghost card for a dragged tab (a tab bar is one row); `dock.moveLeft` and
`dock.moveRight` (two menu items on one key); re-reading namespaces when the view's rows change
(misses a namespace without objects of the view's kind); a namespace watch on every connection.

**Code:** [`tableclick.go`](../../internal/ui/fyneui/tableclick.go),
[`findkeys.go`](../../internal/ui/fyneui/findkeys.go),
[`tabmove.go`](../../internal/engine/tabmove.go), [`nsgone.go`](../../internal/ui/fyneui/nsgone.go),
[`nswatch.go`](../../internal/engine/nswatch.go) · 
`TestClickBelowTheRowsClosesTheDrawer`, `TestListClicksAreNotHeld`, `TestWelcomeRowConnectsOnce`,
`TestCmdFOpensTheEditorTabsFindBar`, `TestTabMoveCommands`, `TestTabDragMovesTheTab`,
`TestNamespaceDeletedWhileFiltered`

## D-107: The navigator slides, and Reduce motion

**Status:** built · **By:** owner

- Drilling into a cluster slides its tree in from the right over All Clusters' list, which stays in
  place; ‹ All Clusters slides it out. The header does not move.
- Showing another cluster while a cluster's tree shows slides vertically in All Clusters' order
  (unfiltered, collapsed groups counted, `listedAbove`): a cluster listed above comes in from above;
  one not listed counts as below every other.
- Every change of the scope slides, whoever made it (a click, a key, ⌘P, the hotbar, a tab,
  `ui.setNavigatorScope`); the window's first scope does not. 250 ms, easing out, a 12-point shadow
  on the tree's edge; a slide turned back returns from where it is.
- The leaving cluster is a picture (`Canvas.Capture` of the frame on screen, cut to the navigator):
  the tree is one widget drawn from the shown cluster's state.
- **Reduce motion** (Preferences › App, `reduceMotion`, off by default; agents and the CLI may set
  it either way) and macOS's Reduce motion each stop Cogpit's animations (`shell.reduceMotion`):
  these slides, a dragged hotbar tile's or tab's slide and glide, the reconnecting dot's blink, the
  light strip's animation ([D-122](#d-122-the-light-strip)) and the agent chat's thinking wave.
  Linux and Windows have the preference only.

**Why:** the scope is engine state ([D-57](#d-57-choosing-a-cluster)), so how the window shows a
change must not depend on who made it; easing out answers a click at once. A Reduce motion that
left some motion running, or a system setting that stopped only part of it, could not be trusted.

**Not covered:** the Helm Upgrade tab's loading spinner (Fyne's `widget.Activity`) turns under
Reduce motion.

**Code:** [`navigator_slide.go`](../../internal/ui/fyneui/navigator_slide.go) (`newNavSlide`
extends the clip's base widget, without which Fyne's focus walk does not find the tree),
[`sidebar.go`](../../internal/ui/fyneui/sidebar.go) `scopeChanged`,
[`preferences_motion.go`](../../internal/ui/fyneui/preferences_motion.go) `shell.reduceMotion` ·
**Tests:** `TestNavigatorSlideSettles`, `TestNavigatorSlideBetweenClusters`,
`TestNavigatorSlideReduceMotion`, `TestReduceMotionIsThePreferenceOrTheSystems`,
`TestPreferencesReduceMotion`, `TestPreferencesCommands`

## D-108: Cogpit for Linux: a .deb and an APT repository on Pages

**Status:** built · **By:** owner

Cogpit for Linux is a Debian package for amd64 and arm64 on each release that sets up Cogpit's
signed APT repository on GitHub Pages, from which apt, the Software Updater and the Update button
update it; unattended installs are each user's own system setting, never the package's. Pipeline:
[releasing › Linux](releasing.md#linux); trust: [updates › Linux](../security/updates.md#linux).

- **Pages, under `/apt` beside the appcasts:** the two newest releases per architecture, with
  Acquire-By-Hash and the previous generation kept for clients reading a cached `InRelease`. Pages
  changes only when a signed repository that checked is deployed, where one in the releases would
  serve nothing until a release's Linux upload. Three Pages deploys (the repository, the macOS and
  the Windows appcast) each carry the others' parts over, so no platform waits for another.
- **Signing in a job of its own** in the environment `apt` (tags `v*` only), on a GitHub-hosted
  runner that runs no Go and nothing of the packages, as Sparkle's signing
  ([D-4](#d-4-packaging-and-updates)); the key is Ed25519, expiring 2031-10-19 to bound a lost key.
- **The sources file is a conffile with the key inline** (deb822 `Signed-By`), with no maintainer
  scripts: a deleted conffile stays deleted through upgrades, and after `apt remove` the file still
  carries its key, where a separate keyring would be gone and break `apt update`.
- **Built on GitHub's hosted Ubuntu 24.04 runners**, for Debian 13, Ubuntu 24.04 and later,
  position-independent as Debian builds Go programs. `Depends` adds the X11 and Wayland libraries
  GLFW loads with dlopen and requires libdecor with a plugin, so the window always has its buttons.
- **The updater reads apt's state** (`apt-cache policy cogpit`: no network, no root) at start and
  hourly while the automatic checks are on. The Update button and Help › Check for Updates… (a
  fresh list needs root) run the package's helper through `pkexec` (polkit `auth_admin_keep`); it
  takes no argument and upgrades only `cogpit` from Cogpit's repository. Then the button reads
  Restart to Update; nothing restarts unasked.
  `app.checkForUpdates` runs from the window only (`ErrUpdateCheckGUIOnly`): from an agent it would
  put a password prompt in front of the user.
- **`cogpit://` links are not registered:** a link would start a second process, and no hand-over
  to the running Cogpit exists ([G22](../parity/gaps.md#g22-windows-and-linux)).

**Code:** [`deb.sh`](../../hack/linux/deb.sh), [`apt.sh`](../../hack/linux/apt.sh),
[`packaging/linux`](../../packaging/linux), [`apt_linux.go`](../../internal/update/apt_linux.go),
[`engine/updates.go`](../../internal/engine/updates.go) `UpdateActor` · 
`TestAptScheduledChecks`, `TestAptUpdateThenRestart`, `TestAptInstallFailures`,
`TestCheckForUpdatesThatActs`; [`apt-test.sh`](../../hack/linux/apt-test.sh) and lintian in
`make test-linux-package`

## D-109: The menus in the top bar on Windows and Linux

**Status:** built · **By:** owner

On Windows and Linux the system's title bar stays, with its buttons and everything the window
manager does with it, and the menus' titles sit at the left of Cogpit's top bar, before the logo
and a divider: two bars, no menu bar of Fyne's. On macOS the menus are the system's menu bar and
Cogpit's bar is the title bar.

- The window keeps a main menu for the menus' keys only: set once while empty, so that Fyne builds
  no menu bar for it (`glCanvas.buildMenu`), then filled, and refilled in place after each rebuild
  of the menus (`installMenuKeys`); before the window has started, the keys wait. Fyne's log line
  about the empty menu is kept out of Cogpit's log.
- An open menu is Fyne's `widget.Menu` in a layer of Cogpit's own under its title; the titles stay
  live in the layer, so the pointer on another title switches menus, and a click elsewhere closes
  it. ↑ ↓ ↵ as in Fyne's menu, Escape closes, ← and → go to the neighbouring menu (round) where no
  submenu takes them; Alt opens nothing (⌃⇧P reaches every command).
- Quit ends the first menu, after a separator, without a key. A title is the bar's text with a
  pill behind it under the pointer (hover colour) and while its menu is open (pressed colour).

**Why:** Fyne looks a typed shortcut up in the main menu before the focused widget is offered it
(`window.triggerMainMenuShortcut`), so ⌃⇧P, ⌃P and the other menu keys work while a text field has
the keys; as canvas shortcuts, every field would swallow them. Fyne's pop-up menu takes every event
while open, so the pointer could not move on to the next menu.

**Not covered:** a Fyne update that changes `glCanvas.buildMenu` or `triggerMainMenuShortcut` brings
Fyne's menu bar back or takes the keys' precedence away; the tests below show part of that.

**Code:** [`menubar.go`](../../internal/ui/fyneui/menubar.go) `shell.setMainMenu`,
`installMenuKeys`, [`bars.go`](../../internal/ui/fyneui/bars.go) · 
`TestMenusInTheTopBar`, `TestMenuOpensUnderItsTitle`, `TestMenuItemRunsAndCloses`,
`TestMenuKeysWaitForTheWindow`, `TestRenderMenuBarDark`

## D-110: Dark Cockpit, Cogpit's design language

**Status:** built · **By:** owner

Cogpit's window follows Dark Cockpit ([design-language.md](design-language.md)): quiet when healthy
and lit where it matters; a navy stage whose surfaces step up in luminance; three lights with one
meaning each (glow blue for Cogpit and the user, cyan for the cluster's data, violet for the
agent). Its palettes in [`theme.go`](../../internal/ui/fyneui/theme.go) replace D-22's Solarized in
the window, which is not offered as a palette; the terminal keeps Solarized's sixteen colours
([ATTRIBUTION.md](../../ATTRIBUTION.md)). Dark, light and the system's setting remain
([D-54](#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle)).

Decided with it:

- violet marks the agent and no status (Terminating is rose); charts draw their second series in it;
- lists are quiet: a status spine and a pill only for what deviates, Running plain with a dot;
- the grid floor only where there is no data (welcome, About);
- a cluster's integrations show in the Overview's head and in Cluster Settings;
- Fyne's typeface stays;
- a cluster's identity colour stays the hue its user picked: a red tile is often the user's own
  warning for production;
- the agent's input keeps Fyne's input border width with transparent colours (`flatEntryTheme`),
  since Fyne draws the text cursor as wide as that border.

**Why:** Cogpit's look sets it apart from Lens while staying intuitive; lights with one meaning
each make what deviates stand out against a quiet rest.

**Open (owner):** which of the design review's outlook topics comes first (a change set with a
receipt, two lanes, ambient triage, semantic zoom); none is built.

**Code:** [`theme.go`](../../internal/ui/fyneui/theme.go),
[`chat_input.go`](../../internal/ui/fyneui/chat_input.go) `flatEntryTheme` · 
`TestPaletteContrast`, `TestStatusColoursAreDistinct`, `TestChartColoursAreDistinct`,
`TestIdentityColorsContrast`, `TestBearingStates`,
`TestOverviewHealthHeader`, `TestAlertsPillSegments`

## D-111: Cogpit for Windows: an NSIS setup and WinSparkle

**Status:** built · **By:** owner

Cogpit ships for Windows 10 1809 and later (build 17763 brought ConPTY), x64, as an NSIS 3 setup
updated by WinSparkle from a feed of its own, signed with Sparkle's update key. Build, files, feed
and release jobs: [releasing › Windows](releasing.md#windows).

- **Per user by default** (`/allusers`: Program Files, through UAC): no install or update needs an
  administrator, as with VS Code's user setup; Lens's switches `/S`, `/allusers` and `/D=`. A Start
  menu entry only, nothing on PATH: the MCP details give `cogpit-cli.exe`'s full path.
- **Running copies:** the setup waits for `cogpit.exe`, which WinSparkle asks to quit once the setup
  runs, moves a running `cogpit-cli.exe` (an agent's MCP bridge) aside so it never blocks an update,
  and restarts Cogpit itself (`/relaunch`): only the setup knows when the files are in place.
- **Updater:** WinSparkle, loaded by its full path from the app's folder (no cgo, never the DLL
  search order), only in a release build running from the folder an uninstall entry names. Its
  scheduled checks are off, as they show its window unasked and tell the app nothing: Cogpit reads
  the signed feed for the Update button, whose click (`app.installUpdate`, GUI only) installs at once
  through `win_sparkle_check_update_with_ui_and_install`, the click being the decision. Help › Check
  for Updates… opens WinSparkle's window. On macOS and Linux `app.installUpdate` does what
  `app.checkForUpdates` does.
- **A feed of its own** (`appcast-windows.xml`): a Windows release never waits for Apple's
  notarization, and WinSparkle gets HTML notes (the release's page), not Sparkle 2's Markdown.
- **In the app** ([architecture](architecture.md)): a ConPTY terminal whose shell runs in a job
  object, so closing it ends every program started from it; PowerShell as the default shell;
  kubectl's folder first on PATH; file renames and removals retried while another process holds the
  file; plugin approvals bound to the file that runs (`plug.exe` for `plug`); MCP registration
  commands for PowerShell. Every text file is checked out with LF
  ([`.gitattributes`](../../.gitattributes)): golden files and `//go:build` lines fail in CRLF.

**Why:** Lens's Windows setup is an NSIS one; WinSparkle checks the same EdDSA signatures with the
same tools as Sparkle, so one update key signs both platforms' feeds.

**Rejected:** WiX (MSI) — v6 and later come under the Open Source Maintenance Fee's EULA; Inno Setup
— asks commercial users for a licence; MSIX — installs only with a trusted signature; winget only —
no one-click update; ActiveState's `termtest/conpty` — ends the process when a pipe fails and starts
the shell before it can join a job; a symlink for kubectl — needs Developer Mode or an administrator.

**Not covered:** no Authenticode signature, so SmartScreen warns at the first start
([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)); no arm64
setup (Fyne's OpenGL on Windows on ARM is untried; Windows 11 on ARM runs x64 emulated); Fyne's file
dialogs follow the system's language while Cogpit's text is English, and its folder chooser lists no
drives.

**Code:** [`cogpit.nsi`](../../packaging/windows/cogpit.nsi),
[`winsparkle_windows.go`](../../internal/update/winsparkle_windows.go),
[`localpty_windows.go`](../../internal/terminal/localpty_windows.go) · 
`TestWindowsUpdaterLoadsWinSparkle`, `TestWindowsScheduledCheckShowsTheButton`, `TestAddWindowsFeed`,
`TestStartLocalWindowsCloseEndsTheTree`, `TestProgramIsBoundByTheFileThatRuns`

## D-112: CI and test time

**Status:** built · **By:** lead (open to revision)

CI and local test runs are kept short at the same coverage.

- **Build cache:** saved by main's pushes only
  ([`go-setup`](../../.github/actions/go-setup/action.yml)); a pull request restores main's newest
  cache of its kind and saves none, so pull requests (Renovate's, each with its own `go.sum`) cannot
  evict main's caches from the repository's 10 GB. A pull request that changes `go.mod` or `go.sum`
  compiles the changed modules in each run, so every job's limit is 30 minutes.
- **Fyne's Linux build dependencies:** the 14 packages cgo and GLFW 3.4 need, without
  recommendations, installed in the background while Go, the cache and the modules download; dpkg
  runs without fsync ([`apt-install.sh`](../../hack/ci/apt-install.sh)).
- **What runs under the race detector:** on Linux every test but the UI's render tests
  (`TestRender*`): they paint on one goroutine and took two thirds of the race shards' test time, so
  they run without it in the no-race job (`make test-norace-coverage`, `UI_RENDER_SHARDS` shards,
  `hack/shard -run`/`-skip`), beside what the race detector keeps from running: `internal/ui/logo`
  and the tests that read `raceEnabled` or compile only without the race tag (`hack/shard -norace`).
  States that only timing reached are reached on purpose by the `*_states_test.go` files. The no-race
  coverage uses `-covermode=set`, which `hack/coverage` merges.
- **The kind job** compiles no UI package, and skips the add-on stacks' tests by name (they need
  stacks CI does not install; `KIND_ADDONS=1` runs them by hand).
- **macOS** runs, besides the darwin files, the tests of `internal/cluster` and `internal/terminal`
  and the login waits (the PTY, the shells, the credential plugin's process search), split into the
  jobs macOS Tests and macOS Lint and App.
- **A missing prerequisite fails CI:** a test takes what it needs of the machine through
  [`testkit/prereq`](../../internal/testkit/prereq/prereq.go), which skips without it and fails
  where `CI` is set: CI runs `go test` without `-v`, which reports a skip as its package's `ok`.
  CI installs what its runners lack: zsh and fish in Race Tests (other packages), from apt without
  recommendations (about 18 MB instead of 30; no other Linux job runs a shell test), and fish in
  macOS Tests from Homebrew rather than a skip there, since fish on macOS orders `PATH` from
  `/etc/paths` as `path_helper` does
  ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first));
  each system's own fish, unpinned, as its users get it. `prereq` imports only the standard
  library, so that every package's tests can use it (`testkit` depends on `cache`, `kubectl` and
  `model`). A prerequisite no job can promise stays a plain skip with its reason: PowerShell off
  Windows, an IPv6 loopback in the Linux runners' pods.
- **Tests without value of their own are removed:** a render's light twin of a dark render of the
  same layout (one light render per major surface stays), a test whose behaviour another asserts as
  strongly, a change detector of a literal, a measurement nothing reads; each removal checked by a
  second, adversarial review.
- **The UI's tests in parallel shards:** `internal/ui/fyneui`'s tests cannot share a process (one
  Fyne app at a time); `hack/shard -exec` compiles them once and runs the shards as processes of their
  own, `UI_JOBS` at once (default: half the CPUs, at most what three quarters of the memory holds at
  about 3 GB a shard, 8 GB under the race detector), `UI_SHARDS` (eight) of them for `make test-ui`,
  `test-ui-race`, `test-unit` and `test-unit-race`. On Windows four shards, two at once (`make
  test-windows-ui`; 4 CPUs, 16 GB).
- **Shards balanced by measured time:** every split of the UI's tests (CI's race shards, the render
  tests' shards, Windows', the local targets) reads a durations file of
  [`hack/shard/durations`](../../hack/shard/durations/), each test's seconds under the race detector
  with coverage (`fyneui-race.txt`) or without it (`fyneui.txt`), as `make ui-durations` measures
  them. The tests go longest first to the shard with the least time so far, each counting at least
  a hundredth of a second; a test the file lacks goes by its name's hash, so a new test needs no
  refresh.
- **Five race shards on CI** (`RACE_SHARDS`, the race-ui matrix), each a fifth of the race tests'
  time.
- **No job that builds the race test binary once** for the shards: each shard compiles its own.
- **Render tests** encode a PNG only for `-update` or `COGPIT_PNG_DIR`; the capture, which paints
  every renderer, is the check (`writePNG`).
- **The logo's tile** is drawn once per pixel size and shared (`logoTile`).
- **`GOGC`** stays at Go's default.
- **Fyne as released**, no `replace`: since Fyne 2.8.0 each `ClearFontCache` (every test app, every
  `SetTheme`) leaves the fonts parsed before it alive, so a process's heap grows with every test;
  reported as [fyne#6619](https://github.com/fyne-io/fyne/issues/6619), with the fix in
  [fyne#6620](https://github.com/fyne-io/fyne/pull/6620) (both open).
- **Linux runners:** the organisation's self-hosted ones; the repository variable `LINUX_RUNNER`
  names others (GitHub's hosted runners take load off the shared machine but do not shorten a run).
- **Lint and GoSec per system:** each sees one system's build, so `make lint` and `make gosec` add
  a pass for Windows without cgo, and the macOS job runs `make lint-darwin` and `make gosec-darwin`;
  the Makefile finds their packages by the files' names, so a new system file is analysed without a
  list to keep ([releasing › Lint and GoSec per system](releasing.md#lint-and-gosec-per-system)).
  `internal/ui/fyneui`'s Windows file stays out: its package needs cgo for Fyne, and a lint on the
  Windows runners would build golangci-lint there and analyse Fyne with MinGW for one system call;
  moving the power read into a package without Fyne would bring it in. A run lists every finding.
- **The smoke test** ([releasing › the smoke test](releasing.md#the-smoke-test)): CI starts each
  platform's shipped binary once with `--smoke <bound>`, so that a release variant that panics at
  start, misses a library or fails in the GL driver fails CI. Each choice against its alternative:
  - a flag, not an environment variable such as `COGPIT_SMOKE=1`: it shows in the command and in
    `-h`, as `--perf` does, and no exported variable turns every start into a smoke run;
  - the binary makes its own home (`smokeEnv`, before the crash capture reads the configuration
    folder) rather than relying on the caller's variables, so that a run by hand cannot touch the
    user's files either; it leaves the home, since Fyne's settings watcher makes its folder again
    and logs an error while the process lives and Windows keeps the open log; `hack/smoke.sh`
    removes it once the app ended;
  - a drawn frame is read back from the front buffer, more than one colour, rather than inferred
    from a callback: it is what the driver presented;
  - an error is a record at level ERROR or a line of Fyne's errors, which reach the log at INFO
    through the standard log package; Linux therefore runs in a D-Bus session, without which Fyne's
    theme watch logs one;
  - the bound is the flag's value (`2m` in `make smoke`), a watchdog's `os.Exit` whatever the main
    goroutine does;
  - macOS Lint and App builds the bundle as releases do (`make dist-app VERSION=0.0.1`), so that
    macOS smokes the release variant as Linux and Windows do; the development variant of
    `cmd/cogpit` compiles in macOS Tests;
  - Windows draws with Mesa's llvmpipe beside the installed `cogpit.exe`, in CI only: the release
    pipeline's Windows job, which ships the setup it builds, runs no third-party binary Renovate
    merges on its own.

**Open:** a runner image that carries Fyne's build dependencies, and with it one job building the
UI's race test binary for all shards.

**Why:** Job set-up (checkout, Go, a slow cache restore, apt) and a full second pass without the race
detector, which covered about 0.3 % of statements no race run covered, took most of a run. In one
process the UI tests' garbage collection dominated as the heap grew; shards in processes of their own
keep heaps small. A higher `GOGC` bought little time for much more memory. Measured times split the
race tests evenly where a hash of the names did not (66 to 163 s of tests across eight shards on
CI), so fewer shards need not make the longest one longer. A race-ui job spends about three minutes
before its first test (checkout, Go, the 500 MB build cache, the modules, the compile), and every
shard fewer saves that: five balanced shards hold about 155 s of tests each, a fifth of the 760 to
775 s the race tests take without the render tests (an M1 Ultra, 815 s on CI; one shard alone took
158 s, none more than 4.1 GB), about the 163 s of the longest hashed shard on CI; four would hold
about 190 s each, six would spend three minutes more on set-up. A job building the race test binary
once for the shards (about 95 MB compressed) would spare each shard Go, the cache and the compile,
but not the libraries the cgo binary links (OpenGL, X11, Wayland, xkbcommon), which apt installs
while Go sets up: each shard would wait for apt instead, after a second wait for a runner behind the
build job. With a runner image that carries the libraries, a shard's set-up would shrink to the
checkout and the download. A pass for Windows takes
about 1.5 s with a warm lint cache and, with an empty one, 3 s (GoSec) and 30 s (lint);
`make gosec-darwin` 2 to 4 s (Apple M1 Ultra, four workers). golangci-lint's defaults show three
findings of a text and one per line, so a run's log would not list all a change has to fix.

**Code:** [`release.yml`](../../.github/workflows/release.yml), [`Makefile`](../../Makefile),
[`.golangci.yml`](../../.golangci.yml) `issues`,
[`hack/shard`](../../hack/shard/main.go), [`durations.go`](../../hack/shard/durations.go) `split`,
[`render_test.go`](../../internal/ui/fyneui/render_test.go) `writePNG`,
[`bars.go`](../../internal/ui/fyneui/bars.go) `logoTile`,
[`prereq.go`](../../internal/testkit/prereq/prereq.go),
[`hack/smoke.sh`](../../hack/smoke.sh), [`cmd/cogpit/smoke.go`](../../cmd/cogpit/smoke.go),
[`fyneui/smoke.go`](../../internal/ui/fyneui/smoke.go), [`mesa.sh`](../../hack/windows/mesa.sh) · `TestSplitIsStableAndComplete`,
`TestSplitBalancesByDurations`, `TestDurationsFile`, `TestReadTestJSON`, `TestSelection`,
`TestRaceKept`, `TestExecShardsRunsEveryShard`, `TestExecShardsRecordsTimes`, `TestDefaultJobs`, `TestNeed`,
`TestLookPath`,
`TestSmokeReportsTheFirstDrawnFrame`, `TestSmokeFrameHoldsMoreThanOneColour`, `TestSmokeWindowWithoutASurface`,
`TestSmokeEnvLeavesTheUsersHome`, `TestSmokeVerdict`, `TestSmokeCountsLoggedErrors`, `TestSmokeWatchdogEndsAStuckRun`

## D-113: Image grades, shields from Trivy's reports

**Status:** built · **By:** owner

The Pods list shows each pod's worst image grade as a shield, in a 32-point column without a header
title right after Containers. A pod's drawer shows each container's grade as a 16-point shield after
its name and, under its items, a card: a 32-point shield, the grade's text and rule, a bar of the
counts with each severity as wide as its share, and a line with the image's OS, when it was scanned
and a link to the report. A grade comes from the image's Trivy Operator VulnerabilityReport as
[D-78](#d-78-trivy-operators-image-ratings) reads them, from `report.summary` and `report.os.eosl`
only; Unknown counts as Low:

| Grade | Rule |
|---|---|
| S | no vulnerability, and the OS is not at its end of life |
| A | at most 10 Low and Unknown (`GradeAMaxLow`), nothing worse |
| B | Medium, or more than 10 Low and Unknown |
| C | High; the best grade of an OS at its end of life |
| D | 1 to 5 Critical (`GradeDMaxCritical`); provisional |
| E | more than 5 Critical; provisional |
| N/A | no report of the image |

- **Text** behind a shield (search, CSV, agents, tooltip): the grade and the count that decides it,
  "D · 2 Critical", "C · OS end of life", "S · no known vulnerabilities", "N/A · not scanned"
  (`GradeText`). The list's tooltip adds every severity's count, the OS's end of life and, for a pod
  of several images, "The worst of N images: …".
- **Sorting** (`GradeRank`): by the grade, then by the deciding count; N/A below S.
- **Look:** a crest filled from 25 % lighter at its top to 25 % darker at its bottom in Docker
  Scout's colours, the same in both themes, the letter in the theme's bold, dark or white by
  contrast; S has a gold rim and a mint letter; N/A is grey at 1.5:1 to the list's background, its
  text at 4:1, per theme. The crest is rasterised with rasterx at the screen's pixel size and cached
  per grade, size and colours, so the list's cells share the images.
- **Agents** read the card as `grade` on the container's section in `object_get`
  (`model.GradeCard`). The overlay adds it to the kind's own sections through `sectionAmender`,
  copying what it changes.
- Ephemeral containers get no grade: no workload's template has them, so no report does.

**Why:** A symbol saves room in the Pods list, and the text keeps the deciding count, so a search for
"Critical" still finds the pods. S cannot require a distroless image: Trivy reads distroless and
Debian slim images alike ("debian 12.15"). An OS at its end of life is C at best because Trivy warns
that its detection may be insufficient there.

**Not covered:** an image Trivy cannot see into (busybox, without a package database) grades S, like
one with nothing found; "N/A" in the list's 18-point shield is about 5.4 points tall, legible only on
a 2× screen.

**Code:** [`imagerating.go`](../../internal/model/imagerating.go) `VulnerabilityCounts.Grade`,
`GradeText`, `GradeRank`; [`trivy.go`](../../internal/engine/trivy.go);
[`overlay.go`](../../internal/engine/overlay.go) `sectionAmender`;
[`gradebadge.go`](../../internal/ui/fyneui/gradebadge.go);
[`drawergrade.go`](../../internal/ui/fyneui/drawergrade.go) · **Tests:** `TestImageGrades`,
`TestParseVulnerabilityReport`, `TestTrivyPodsColumn`, `TestTrivyImageGrades`,
`TestTrivyWorstImage`, `TestRenderTrivyPodsDark`, `TestRenderPodImageGradesDark`, `TestKindTrivy`

## D-114: A failed connect's error page and its stages

**Status:** built · **By:** owner

- **An error page, not the welcome page:** when the shown cluster's connect fails (a click or a
  restore), the workspace shows that cluster's error page whether or not a tab is open: the welcome
  page's look, the cause in a sentence, the connect's stages, Connect Again and Show Connection
  Updates ([usage](usage.md)). Only a failure without a context to name shows the welcome page,
  with its error. The view under the page says "Not connected." only.
- **Every connect in stages:** kubeconfig, login (a credential plugin's run, only for a context with
  one), dns, tcp, tls and api (`GET /version`, the login accepted or not), each with its result (ok,
  failed, not reached, skipped, cancelled), a sentence and its time; after the failed stage every one
  is not reached. The connection log keeps them per attempt, and Connection Updates shows them under
  each connect's header ([architecture](architecture.md)).
- **Measured on client-go's own first request** (`checkReachable` gives `GET /version` an
  `httptrace.ClientTrace`): the stages are what client-go did (its resolver, the address it chose,
  its timeouts), and behind a proxy they name the proxy.
- **The login stage is second:** client-go runs the plugin before it dials, so a failed plugin leaves
  DNS and TCP not reached. After the first answer the connect waits up to 1 s (`runEndSettle`) for
  the plugin run's end to be noted, so a login that answered never reads as still running.
- **The error line** (`Connection.Error`) is the failed stage's sentence; the whole chain is the
  attempt's last ERROR line in the connection log ([D-66](#d-66-the-connection-log)). An invalid
  kubeconfig names each missing or unreadable file by its role and path, at most three problems.
- **No secrets:** a stage names files, hosts, addresses, the certificate check and a plugin's
  program, never a token, key, password or a plugin's output; error texts pass the URL sanitiser,
  and proxy passwords read •••.
- **Help › Welcome** (`ui.welcome.open`) over the error page shows the welcome page and leaves the
  cluster `Disconnected`, its context, connection log and red dot kept.
- **The welcome page** has no "Other clusters" group, as the navigator beside it lists every cluster;
  only on a first start (no hotbar tile, no recent cluster) it lists up to eight as "Your clusters",
  the kubeconfig's current one first. Its message about the connection is a banner naming its level.
- A dialog's Context row shows the kubeconfig's source label in the kind word's grey
  ([D-85](#d-85-dialogs-as-tables-and-an-objects-state-without-its-resourceversion)); a log tab's
  Follow turns off on a scroll up and on at the newest line
  ([D-99](#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)).

**Why:** The stages tell at a glance which step of a connect failed; a raw error chain on the page is
noise while the connection log holds it.

**Rejected:** a probe of Cogpit's own (`net.Resolver`, `net.Dialer`) — behind a proxy it would
bypass the proxy (a direct attempt the network may not expect, the server's name sent to the local
resolver) or be skipped; Help › Welcome doing nothing over the error page — reads as broken.

**Code:** [`connectstages.go`](../../internal/cluster/connectstages.go),
[`connectfailure.go`](../../internal/engine/connectfailure.go),
[`errorpage.go`](../../internal/ui/fyneui/errorpage.go),
[`welcome.go`](../../internal/ui/fyneui/welcome.go) `groups` · 
`TestConnectStagesOfAFailure`, `TestConnectStagesBehindAProxy`,
`TestALoginThatAnsweredIsNotStillRunning`, `TestARestoreThatFailsShowsTheErrorPage`,
`TestTheWelcomePageHasNoOtherClusters`, `TestWelcomeNoteNamesItsLevel`

## D-115: A dock tab the user opens takes the keys

**Status:** built · **By:** lead (open to revision)

A dock tab the user opens or shows from the window takes the keyboard on its main widget: the YAML
editor of an Edit, Create, Helm Upgrade or Helm Install tab; the terminal of a terminal, shell,
attach or node shell tab once its session runs; a logs tab's lines once its stream opened. An Edit
tab's review, the Audit Log, the Diagnostics and a job's progress take nothing.

- **Whose tab:** `shell.run`, through which every command of the window goes, asks the keys for the
  tab its answer names (`OpenedTab()` of `DockResult`, `EditResult` and `DebugResult`); a click on a
  tab in the dock's strip (the shown one too) and Restore on the drafts banner ask directly.
- **Agents:** a tab an agent (MCP, the built-in agent) or the CLI opens or shows never takes the keys
  ([agents and MCP](../security/agents-and-mcp.md#rules)).
- **A widget still opening** takes the keys once it is ready, unless the keys went to another widget
  meanwhile, the tab closed or a newer state shows a third tab; a click on a button, which leaves no
  widget focused, does not count as a move.
- Keys already inside the tab (its find bar, a version just picked) stay there; ⌘J, the layout
  toggle and the strip's arrow move no keys.
- **The find bar** opens through the window's canvas (`findBar.open`): Fyne's `Container.Show` asks
  for no repaint and the driver knows no canvas for an object no frame has drawn, so a bar shown
  otherwise appears only at the next repaint, with the keys left in the editor. Fyne's test driver
  always answers the window's canvas and does not show this.

**Why:** Whoever opens a tab wants to type in it. Keys must never land in a shell an agent opened,
where they run on a cluster with the user's identity, and the window cannot tell typing from a
pause. The command's answer covers every path of the window (row menu, palette, menus) in one place.

**Rejected:** every tab that appears or becomes active — hands an agent's tab the keys; an agent's
tab only while nothing has the keys — after a click on a button nothing has them; the engine
recording who opened each tab — needs the origin at a dozen sites through `UpdateUI`, which takes no
context; each call site asking — misses the generic paths (row menu, palette, menus).

**Code:** [`dockfocus.go`](../../internal/ui/fyneui/dockfocus.go) `focusOpened`, `takeFocus`;
[`commands/dock.go`](../../internal/engine/commands/dock.go) `DockResult.OpenedTab`;
[`findbar.go`](../../internal/ui/fyneui/findbar.go) `findBar.open` · 
`TestRowMenuEditGivesTheEditorTheKeys`, `TestNewTerminalGetsTheKeys`,
`TestStripTabClickGivesTheTabTheKeys`, `TestATerminalStillOpeningLeavesKeysMovedMeanwhile`,
`TestAnAgentsTabLeavesTheKeys`, `TestFindBarOpensThroughTheWindowsCanvas`

## D-116: Escape closes the innermost thing

**Status:** built · **By:** lead (open to revision)

Escape closes the innermost thing open, and only that, outermost last:

1. what has the keyboard, where it closes something of its own: an open dropdown's list (its field
   then gives the keyboard up), a find bar, a search, the command palette's filter, a menu;
2. the window's top overlay: a dialog as its Cancel (Close where it has none), a pop-up, the
   palette, the notifications, the MCP details, the settings card
   ([D-118](#d-118-settings-as-a-card-over-the-window));
3. the drawer.

- **One mechanism:** each overlay registers what Escape does over it as it shows (`onEscape`,
  `onShown`; Fyne dialogs through `sh.showDialog`, Escape being their `Dismiss`), and the window's
  `escape` runs the top overlay's. An overlay without a registration takes Escape and closes nothing
  behind it. `TestEveryDialogTakesEscape` reads the sources and fails on a Fyne dialog shown
  otherwise or a pop-up shown without `onShown`.
- **Escape over a dialog is Cancel** for every way it closes (a confirmation answers no, a file
  dialog nil, the integrations question Later, "No clusters found" Keep Syncing), also where Cancel
  drops what was typed; nothing reaches a cluster without the confirm button.
- **Passing it on:** Fyne gives a key to the widget that has the keyboard and calls the window's
  handler only while none has it, so Cogpit's own widgets hand an unused Escape on (`passEscape`):
  `fieldEntry`, `fieldSelectEntry`, `fieldCheck` and `fieldSlider` in dialogs and in every text field
  of Preferences and Cluster Settings, a Secret's value (`secretEntry`), the dropdown (`selectField`).
- **Searches** (`searchEntry`): the list's search clears (sending the empty filter) and gives the
  table the keys, so the next Escape closes the drawer; the logs tab's search and Argo CD's activity
  filter clear and leave the keys nowhere. The editor tabs' find bar closes from its fields and from
  the editor, before the editor's selection goes, and keeps its text for the next ⌘F.
- **Settings:** Escape in a text field closes the card, the value typed set first (`flushTyping`);
  in Preferences › Agent's connection form it closes the form as its Cancel (`agentForm.escape`),
  and the next Escape the card. Text waiting for Enter or Add (the accessible namespaces) is dropped.

**Why:** One place decides what Escape does; Fyne's dialogs cannot take a key handler of their own.

**Rejected:** Escape only while nothing was typed — its effect would hang on a state the dialog does
not show; asking before dropping typed fields — a dialog over a dialog; the list's search keeping its
filter, or a first Escape clearing and a second leaving — two steps for one intent.

**Not covered:** a button focused by Tab, the items of a check or radio group (Argo CD's Sync and
Refresh dialogs), the fields of Fyne's file and colour dialogs and a selectable label in a
confirmation's review keep Escape; the guard reads assignments as written, so a Fyne dialog shown
under another name than it was assigned to passes it.

**Code:** [`escape.go`](../../internal/ui/fyneui/escape.go),
[`searchkeys.go`](../../internal/ui/fyneui/searchkeys.go) `searchEntry`,
[`findbar.go`](../../internal/ui/fyneui/findbar.go) · 
`TestEscapeCancelsTheRollbackDialog`, `TestEscapeCancelsEveryDialog`,
`TestEscapeClosesTheConnectionUpdates`, `TestEscapeClosesTheFindBar`,
`TestEscapeLeavesTheListSearch`, `TestEveryDialogTakesEscape`,
`TestSettingsEscapeInATextFieldClosesTheCard`, `TestSettingsEscapeInTheAgentFormClosesTheForm`

## D-117: One preview tab per cluster

**Status:** built · **By:** owner

Every cluster has at most one preview tab of its own. A view opened in preview replaces only the
shown cluster's preview tab (`previewIndex(context)`); a cluster without one gets it right of the
active tab, or at the end of the bar when no tab is active. Another cluster's tabs never change, and
a tab's cluster never does.

- **A tab switch changes no other tab:** `SelectTab` of another cluster's tab keeps the shown
  cluster's tab active until the other cluster shows (it clears it only for another tab of the
  clicked tab's own cluster: the one shown last before a restart,
  [D-41](#d-41-what-survives-a-restart)); `syncTabs` moves the live view into the preview tab only on
  a connect or when the view changed.
- The workspace file keeps one preview tab per cluster (`workspaceDoc.normalize`: a further one of a
  cluster loads as fixed). `ui.state` lists every tab with its context and `preview`.
- **A tab comes back as it was left:** the engine keeps its filters, search with its toggles, status
  filter, sort, selection and drawer; the window keeps where each place of the tab scrolled its list
  ([`tabscroll.go`](../../internal/ui/fyneui/tabscroll.go), by `ViewTab.Place`), for this run only.
  A tab shown again (a tab or cluster switch, ⌘1–9) and a place brought back by back or forward
  ([D-129](#d-129-back-brings-the-place-back)) scroll back there, as far as the rows reach with each
  snapshot until the list is synced; a scroll of the user's own ends the wait. A view the tab opens
  anew is a new place and starts at its top; places no tab holds any more are forgotten
  (`forgetGone`). Only a list that is its tab's own (the tab's cluster and view as the engine has
  them) is kept or brought back.

**Why:** With clusters side by side ([D-69](#d-69-several-clusters-connected-at-once)), one preview
tab for all lets a click in one cluster drop another cluster's view. Only the window knows its
pixels, and agents drive filters and selection, not a scroll offset; a view opened anew comes with
new filters, so an old offset would point elsewhere.

**Rejected:** one preview tab for all — a click in the navigator after a switch drops the other
cluster's view silently; one for all, made fixed when its cluster is left — a fixed tab the user
never asked for after every switch; offsets in the engine's tab state — only the window knows them;
offsets across a restart — the workspace file keeps no selection either.

**Not covered:** Lens's behaviour across clusters is not captured, so the rule is inferred; with
mixed environments on, the preview tabs differ only by their cluster's icon and name; a page's
scroll (the Overview's) and the drawer's are not kept.

**Code:** [`tabs.go`](../../internal/engine/tabs.go) `SelectTab`, `syncTabs`, `showInPreview`;
[`persist_restore.go`](../../internal/engine/persist_restore.go) `normalize`;
[`tabscroll.go`](../../internal/ui/fyneui/tabscroll.go) · 
`TestSelectingAnotherClustersTabChangesNoTab`, `TestThreeClustersKeepTheirPreviewTabs`,
`TestWorkspaceKeepsOnePreviewTabPerCluster`, `TestATabComesBackAsItWasLeft`,
`TestATabComesBackWhereItWasLeft`, `TestATabsViewOpenedAgainStartsAtTheTop`,
`TestAScrollEndsTheWaitForATabsRows`

## D-118: Settings as a card over the window

**Status:** built · **By:** owner

Preferences and Cluster Settings show as a modal card over the whole window: the hotbar, the bars,
the views, the dock and the agent's chat are dimmed behind it and unreachable until it closes; a
page an agent opens covers its chat until the user closes it. Lens shows both as a page.

- **Mechanism:** an overlay of the window's canvas (`settingsOverlay`), as the palette's; while it
  shows, the window's tooltip layer and toasts are lifted into it (`lift`), so they stay above the
  card.
- **Size:** inset by 5 % of the window from each edge (at least 24 points), at most 1,120 points
  wide, as high as the window allows, never smaller than the page needs nor larger than the window.
  One size for every section, so the card does not jump between them.
- **Look:** the window's background, rounded 14 points, a hairline edge (`colorBracket`) and Fyne's
  pop-up shadow. The header spans the card with 5 points of room on every side
  (`settingsHeaderRoom`): the cluster's icon at the hotbar tile's size and
  "Cluster Settings: <name>" (or "Preferences") at the left, an outlined × with the tooltip
  "Close (Esc)" at the right; the section list below it on the sidebar's colour.
- **Closing:** the ×, Escape, or a click or right-click on the shade; a click on the card's own
  background does nothing. A value typed within the 600 ms typing pause is set first
  (`flushTyping`), also for an agent's `settings.close` and a change of section; a refusal after the
  close shows as a toast. A section shown anew, and the card opened again, start at the top, so a
  section never shows empty at an old scroll offset.
- **Keys:** the card's close is the top overlay's Escape
  ([D-116](#d-116-escape-closes-the-innermost-thing)), so a dialog, pop-up or menu opened from the
  page closes first; Escape in a page's text field closes the card (the agent connection form's
  fields close the form). The window's own keys are held (`heldUnderSettings`): ⌘F, ⌘⇧T, F1–F12
  and "/" do nothing; the main menu's items act (⌘, and ⌘⇧, switch the page).

**Why:** Fyne hands the pointer to the top overlay alone and gives each overlay its own focus
manager, so nothing behind is clicked, hovered or scrolled and Tab stays in the card. A window key
would act on what lies behind the card and move the keyboard there.

**Rejected:** a layer of the window's content made modal by hand — Fyne's focus manager walks the
whole content, so Tab would reach the dimmed fields behind.

**Not covered:** a card opened while a dialog shows lies above that dialog until it closes (the
newest overlay is on top); where Tab starts inside the card is Fyne's.

**Code:** [`settingsoverlay.go`](../../internal/ui/fyneui/settingsoverlay.go),
[`preferences_page.go`](../../internal/ui/fyneui/preferences_page.go) `flushTyping`,
[`menukeys.go`](../../internal/ui/fyneui/menukeys.go) `heldUnderSettings` · 
`TestSettingsOverlayKeepsThePointerFromTheWindow`, `TestSettingsOverlayKeepsTabInTheCard`,
`TestSettingsOverlayTypedValueIsSetOnClose`, `TestSettingsOverlayShowsToastsAndTooltipsAboveIt`,
`TestSettingsOverlayHoldsTheWindowKeys`, `TestSettingsCardHeader`,
`TestASettingsSectionStartsAtItsTop`

## D-119: The cluster icon designer

**Status:** built · **By:** owner

Cluster Settings › General › Icon offers two kinds of icon, chosen by the radio pair "Internal icon"
and "Custom icon" and changed in their own dialog through Customize…; Reset drops the image, its
source, the colour and the letters.

- **Internal icon:** a colour tile with up to 5 characters, no spaces or control characters
  (`CheckIconLetters`); empty means the name's initials. The letters grow with the square
  (`initialsSize`: side × 16/38, at least the text size 13) and shrink to fit (`fitTextSize`).
- **Custom icon:** a part of an image (a picked file of at most 1 MiB) placed in a 240-point square
  of a 360 × 300 editor, the rest dimmed: dragged 1:1, zoomed by the wheel at the pointer (×1.1 per
  notch) or by a slider (1 to 8) at the centre, Fit to fill and centre, clamped so the square never
  leaves the image; square only, no rotation. Previews at the hotbar's and the navigator's sizes,
  cut 80 ms after a move stops. Choose Image… opens the system's file panel (Fyne's dialog where
  there is none, [D-72](#d-72-native-file-panels-and-macos-folder-access)); Custom without a kept
  image opens the dialog.
- **What is kept**, in `cluster-icons/` under each file's SHA-256: the source (a PNG or JPEG made a
  PNG of at most 1,024 pixels a side; an SVG checked, then drawn into one with `srwiley/oksvg`), the
  cut (`iconCrop`: zoom 1 to 8 and the source's point at the square's centre, as fractions) and the
  icon (a PNG of 256 pixels a side). A file no context uses is removed. `CutIcon` (Catmull-Rom from
  `golang.org/x/image/draw`) makes both the previews and the icon. Both kinds stay kept (`iconTile`
  says which shows); setting an icon colour shows the tile. An icon without a kept source serves as
  its own source in the dialog.
- **Rounding:** every cluster icon, tile or image, is rounded at every size by its side × 22/96
  (`iconCorner`, `logo.CornerRatio`: the app icon's plate) through Fyne's
  `canvas.Image.CornerRadius`; an image on a hotbar tile by its own side. The crop square keeps the
  fields' 7 points.
- **Sizes:** hotbar tiles and "+" are 50 points, as are the settings header's badge, the Icon row
  and the Hotbar preview. The navigator root's badge is as tall as its lines (50 beside three) and
  narrows, down to 28, before it cuts the name or the version further than a 28-point badge would,
  or a user's line that was whole beside 28
  ([`navigator_rootbadge.go`](../../internal/ui/fyneui/navigator_rootbadge.go)). Navigator rows,
  the welcome page's rows and the Navigator preview stay 28.
- **Agents:** `cluster.settings.set`'s `iconLetters` and `iconTile` are refused to agents and the
  CLI (`ErrClusterIdentityGUIOnly`); `cluster.settings.uploadIcon`, `cluster.settings.cutIcon` and
  `cluster.settings.resetIcon` are GUI-only (H-125,
  [agents and MCP](../security/agents-and-mcp.md)).

**Why:** The icon is the user's mark of which cluster this is. One cut function makes the preview
the icon kept, keeps image bytes out of command parameters and is testable without a window; a cut
in fractions lets the dialog open where it was left. Fyne's corner radius is drawn at the screen's
pixels by both painters, so no rounded copy per size and scale is needed.

**Rejected:** a circle frame — the icon shows in a rounded square everywhere; rounding by masking
pixels or a rounded copy — bakes one size and screen scale into each copy; the root badge sized by
the navigator's width alone — cuts a typical cluster name at the default width.

**Not covered:** the editor's cursor stays the arrow (Fyne has no grab cursor); corners are circular
arcs, not Apple's continuous curve.

**Code:** [`clustersettings_iconcut.go`](../../internal/engine/clustersettings_iconcut.go)
`CutIcon`, `CheckIconLetters`; [`clustericon.go`](../../internal/ui/fyneui/clustericon.go);
[`clusteridentity.go`](../../internal/ui/fyneui/clusteridentity.go) `iconCorner`, `initialsSize` ·
**Tests:** `TestCutIcon`, `TestIconSource`, `TestClusterIconCutAgain`, `TestIconImageDialog`,
`TestCropEditorZoomsAtThePointer`, `TestClusterIconCornersAsMacOS`, `TestRootBadgeFillsItsRoom`

## D-120: Folders searched for credential plugins

**Status:** built · **By:** owner

Cogpit appends folders to its own `PATH`, after every folder `PATH` has, so that kubectl, a
credential plugin or a kubectl plugin a package manager installed is found by an app started without
a shell profile:

1. **Built-in folders**, on macOS and Linux, each while it exists (`cluster.BuiltinToolFolders`):
   Homebrew's (`/opt/homebrew/bin`, `/usr/local/bin`; on Linux `/home/linuxbrew/.linuxbrew/bin`),
   MacPorts' `/opt/local/bin`, `/snap/bin` (Linux), Nix's profiles, `~/.local/bin`, `~/bin`,
   krew's `${KREW_ROOT:-~/.krew}/bin`, go install's (`$GOBIN`, else the first folder of `$GOPATH`
   or `~/go`, then `bin`), asdf's and mise's shims, Rancher Desktop's `~/.rd/bin`, Docker Desktop's
   `~/.docker/bin`. A variable counts only where the app sees it. None on Windows, and no cloud
   provider's installer folder ([D-9](#d-9-scope-against-lenss-paid-and-vendor-features)).
2. **The user's program folders**, on every platform, in the order added: Preferences ›
   Kubernetes › Program folders (`programFolders` in `preferences.json`, at most 32), each a row
   with a remove button; Add Folder… opens the system's folder panel (hidden folders listed, a
   read-only bookmark; Fyne's folder dialog where there is none). `preferences.addProgramFolder`
   is GUI-only (`ErrProgramFoldersGUIOnly`); `preferences.removeProgramFolder` is open to agents.

A folder counts when it is an absolute path, exists, is a folder (a link to one counts) and, on
macOS and Linux, is not writable by every user (`ProgramFolderProblem`); adding refuses the others,
a kept folder is skipped while it fails. `PATH` is composed again from the start-up `PATH` when the
folders change and at every connect (`cluster.RefreshToolPath`), so a folder that appeared counts at
the next login without a restart. `cogpit-cli` reads the same folders (`engine.ReadProgramFolders`).
A login that fails for a missing program (client-go's `exec: executable X not found`, or kubectl's
`unknown command "Y" for "kubectl"` on the plugin's standard error, read as `kubectl-Y`;
`cluster.MissingProgram`) says where to add its folder, in the stage's text and in agents'
`missingProgram`; the error page offers Program Folders….

**Why:** Appended, never prepended: a program `PATH` finds keeps running, so no folder shadows a
system program or the program an approval binds by name
([D-26](#d-26-binding-a-credential-plugins-approval-to-its-program)). A folder every user may write
to would let any local account plant a plugin. A folder an agent added could hold a program of its
choice for the user's next login; removing one only makes fewer programs found.

**Rejected:** the built-in list alone — a team's folder or a `$KREW_ROOT` set in a profile stays
unfound; the login shell's `PATH` read at start — runs the user's profile at every start
([H-6](../security/credentials.md#h-6-exec-plugins-inherit-the-environment)); a setting alone —
every krew user sets it by hand.

**Not covered:** the shell profile's variables (`AWS_PROFILE` and the like, H-6); a kubectl plugin
is bound by no approval (D-26); whether kubelogin's device-code login completes is not checked (it
prints its URL and code on standard error, which the connection log shows only after a failed
login, [D-66](#d-66-the-connection-log)).

**Code:** [`toolpath.go`](../../internal/cluster/toolpath.go),
[`programfolders.go`](../../internal/engine/programfolders.go),
[`preferences_programs.go`](../../internal/ui/fyneui/preferences_programs.go) · 
`TestComposePathAppendsWithoutShadowing`, `TestProgramFolderProblem`, `TestBuiltinToolFolders`,
`TestConnectNamesTheMissingProgram`, `TestProgramFoldersAddRemoveAndKeep`,
`TestGUIOnlyCommandsRefuseOtherOrigins`, `TestErrorPageOffersProgramFolders`

## D-121: Texts drawn again when the window's pixel scale changes

**Status:** built · **By:** lead (open to revision)

On macOS, when the window's framebuffer density changes (a display at another scale, displays
switched off and on around a sleep), Cogpit applies the current theme again (`reapplyTheme`:
`SetTheme(Theme())`, the same object), so every text is drawn again at the new resolution, and the
logo at the new pixel count (`pixelsFor`).

- An observer of `NSWindowDidChangeBackingPropertiesNotification` on the window
  ([`pixelscale_darwin.m`](../../internal/ui/fyneui/pixelscale_darwin.m)) hands each report on in
  the main queue's next pass; the check then runs through `sh.do`, after GLFW gave Fyne the new
  framebuffer size.
- The check compares Fyne's own density (`canvasDensity`: the pixels of 10,000 points over Fyne's
  scale, i.e. its `texScale`) with the last reading and re-applies once per change of 0.01 or more;
  a report for a colour space alone, the same scale again or a density of 0 applies nothing.
- Windows and Linux watch nothing
  ([`pixelscale_other.go`](../../internal/ui/fyneui/pixelscale_other.go)): Fyne changes the
  framebuffer density only on macOS and Wayland (Cogpit's Linux build is X11), and a change of
  Fyne's own scale goes through its `reloadScale`, which drops the texts already.

**Why:** Fyne 2.8.2 caches a text's texture by text, size, style, font, colour and canvas, not by
resolution; on macOS a backing-scale change only sets `texScale` and refreshes the content
(`processFrameSized`), so texts drawn before stay soft. Only `RescaleContext`
(`DeleteTextTexturesFor`) drops them, reached through the settings listener that every `SetTheme`
notifies. A re-apply refreshes every widget and clears Fyne's font cache (about 1.5 MB each in Fyne
2.8.2, [fyne#6619](https://github.com/fyne-io/fyne/issues/6619)), hence once per change. The watch
can go once Fyne drops text textures when `texScale` changes
([fyne#5323](https://github.com/fyne-io/fyne/issues/5323)).

**Rejected:** Fyne's content refresh as the signal — it never reaches Cogpit's code; comparing the
density on a resize, a move or a tick — the displays can change without either, and a tick polls;
calling `DeleteTextTexturesFor` — Fyne-internal.

**Not covered:** texts that go soft without a change of the density are not drawn again (switching
the theme still does); the Objective-C side runs in no test.

**Code:** [`pixelscale.go`](../../internal/ui/fyneui/pixelscale.go) `densityWatch`, `reapplyTheme`,
[`pixelscale_darwin.go`](../../internal/ui/fyneui/pixelscale_darwin.go) · 
`TestThemeAppliedAgainWhenThePixelScaleChanges`, `TestCanvasDensity`; the cgo export's row in
[ui-threads.md](ui-threads.md)

## D-122: The light strip

**Status:** built · **By:** owner

A 4-point strip of frosted glass right under a view's header (above the page's part and the table's
header, the view's width; every tab of the main tab bar has it) shows the accent colour
([D-80](#d-80-a-clusters-accent-colour)) of the cluster whose tab is active in the main tab bar
(`UIState.TabsShown`, never the dock's). It is glass (off) without an accent colour, on the welcome
page, without an active tab and while the active tab's cluster connects. The tab bar is always
shown, a tab high when empty, so the window does not jump. Lens has no such strip.

- **Per cluster:** `ClusterSettings.LightStrip` (`lightStrip` in `cluster-settings.json`): static
  (the default, kept as none), `breathing`, `scanner` or `flow`; an unknown value is refused, and
  dropped from a file. Set by `cluster.settings.setLightStrip`, GUI-only (`ErrLightStripGUIOnly`),
  disabled without an accent colour (`ErrLightStripNoAccent`); clearing the colour keeps the choice.
  Agents read `lightStrip` (`cluster_settings_get`, `ui_state`'s `clusterSettings`).
- **The row:** Cluster Settings › General › Light strip, under Accent colour: Static, then
  Breathing, Scanner and Flow in a frame headed "Animations ⓘ", whose tooltip states the CPU cost.
  A warning line says why a chosen animation stands still when the user's setting causes it.
- **Look** (constants in [`lightstrip_model.go`](../../internal/ui/fyneui/lightstrip_model.go)):
  glass `colorLightStrip` (dark `#1b2027`, light `#eff1f4`) at density 32 %; grain 10 %, one device
  pixel per speck (a fixed 256 × 64 texture, seed 7, tiled by a `canvas.Raster` made again only on
  a resize, its strength in the specks' alpha because Fyne's software painter ignores a raster's
  translucency); full glow; breathing 4.6 s down to 48 %; scanner 6 s there and back, its light half
  the strip; flow 5 s per pass, its light 46 % of the width with a soft front a quarter of that. The
  light is the accent in the theme's legible shade (`identityColors`), as its swatch shows it.
- **Transitions** (a function of the clock, smoothstep): every change goes through glass, 350 ms
  out, then the new light fades in from its animation's beginning (scanner and flow enter from
  outside). Static to static spreads from the active tab's middle over 700 ms (soft edge 18 %), or
  crossfades in sRGB while motion is reduced. The same colour and animation: no transition.
- **Standing still:** static at full glow while Cogpit is not the active app, while motion is
  reduced (Preferences › App › Reduce motion or macOS's,
  [D-107](#d-107-the-navigator-slides-and-reduce-motion)), and on battery when Preferences › App ›
  Light strip on battery is checked (`Preferences.LightStripMainsOnly`, off by default, open to
  `preferences.set`). Going to stand and back is a change through glass.
- **Power source:** macOS follows IOKit's notification, acting only when adapter and battery swap;
  Windows (`GetSystemPowerStatus`) and Linux (`/sys/class/power_supply`: on battery only when no
  Mains or USB supply is online and a system battery, not a device's, discharges) are polled every
  30 s. Unknown counts as the adapter.
- **Clock and drawing:** a ticker at 60 frames a second posts one frame at a time through `sh.do`
  and runs only while a light fades or an animation shows. A frame moves gradients (Fyne keeps their
  textures) or recolours a rectangle, clipped to the strip, at most three lights at once. A
  development build reads `COGPIT_LIGHTSTRIP_FPS` (1 to 60); a release (`nodemo`) has no such
  variable.

**Why:** The accent colour tells a production cluster from a development one at a glance, and an
agent that could calm a production strip down would undo the user's warning (H-125). Fyne 2.8.2
repaints the whole window when anything changed, so an animation costs about a fifth of a core
(Apple M1 Ultra, 60 frames a second) while off and static cost nothing: hence the CPU note and the
battery switch. Windows gives Cogpit no power message through GLFW, and UPower would be a
dependency for one bit: hence the poll.

**Rejected:** the strip under the tab bar — the active tab's underline clashed with the lit strip;
blinking — distracting and flashing (WCAG 2.3.1); rainbow and RGB pulse — they hide the colour that
tells clusters apart.

**Not covered:** the load is measured on one Mac at 1× only (not a Retina display, a laptop,
Windows or Linux, nor the GPU's and WindowServer's share); a strip that costs nothing while it moves
would need a native layer or a Fyne that redraws a region; the ticker is not in step with Fyne's
draw loop; Reduce motion is read on macOS only; the Objective-C observers run in no test; a Cogpit
that starts without becoming the active app counts as active until the first change of focus; the
light theme's lit glass shows hardly any grain.

**Code:** [`engine/lightstrip.go`](../../internal/engine/lightstrip.go),
[`commands/lightstrip.go`](../../internal/engine/commands/lightstrip.go),
[`fyneui/lightstrip.go`](../../internal/ui/fyneui/lightstrip.go),
[`clustersettings_lightstrip.go`](../../internal/ui/fyneui/clustersettings_lightstrip.go),
[`preferences_power.go`](../../internal/ui/fyneui/preferences_power.go),
[`power.go`](../../internal/ui/fyneui/power.go) · **Tests:** `TestLightStripKept`,
`TestLightStripIsTheUsers`, `TestLightStripStaticToStaticSpreadsOrCrossfades`,
`TestLightStripFollowsTheShownTab`, `TestLightStripAnimatesAndChangesThroughGlass`,
`TestLightStripTickerStopsWhenStill`, `TestRenderLightStrip`

## D-123: A drag starts where the button went down

**Status:** built · **By:** lead (open to revision)

A drag belongs to the object the mouse button went down on. Every draggable of Cogpit's carries a
`dragGuard`: on the drag's first event it takes the press point (`AbsolutePosition − Dragged`) and
passes the drag only if the point lies on the object (`pointerOn`, Fyne's hit rule: left and top
edges on, right and bottom off); the answer holds until `DragEnd`, which for an ignored drag does
nothing (no move, drop, click or command). An event without an absolute position (Fyne's test
driver) passes. Guarded: hotbar tiles, the tab bar's and the dock's tabs, the panels' edges and the
dock's top edge, the table's column dividers and headers (no stray `ui.setColumnOrder`), a Config
Map's row grip, the icon designer's crop, and the selectable texts of the drawer, Secret values,
toasts and the chat.

The dock's height is Cogpit's own edge (`dockLayout` with a vertical `resizeHandle`, the side
panels' look: a 5-point band with a 1-point line): the dock takes 0.34 of the height at first and
keeps its share when the window resizes; at least 100 points or its tab's minimum, the workspace at
least 120; not stored.

**Why:** Fyne 2.8.2's GLFW driver (`processMouseMoved`) gives a drag to the draggable under the
pointer's previous position once the pointer is 2 points from the press, wherever the press was,
and the first event carries the whole way from the press, so a tile could jump out of the window.
The first event decides because a tile, a tab or an edge moves under its own drag. Fyne's
`container.Split` divider cannot carry a guard.

**Not covered:** Fyne's own draggables (a list's scroll bar, an Entry's text selection, a slider);
the fix for them is Fyne's driver looking up the draggable at the press point.

**Code:** [`dragguard.go`](../../internal/ui/fyneui/dragguard.go),
[`panelwidths.go`](../../internal/ui/fyneui/panelwidths.go) `dockLayout` · 
`TestDragGuardDecidesOnTheFirstEvent`, `TestStrayDragLeavesTheHotbarAlone`,
`TestStrayDragLeavesTheColumnsAlone`, `TestStrayDragLeavesTheDockEdgeAlone`,
`TestStrayDragSelectsNothing`, `TestDockHeightByDraggingItsEdge`

## D-124: Checked rows, the drawer's row and the keyboard's cell

**Status:** built · **By:** owner

| Row | Mark |
|---|---|
| Checked | the selection's fill, text in the selected text colour; no bar |
| The drawer's | a 2-point bar in the primary blue at the left edge, as high as the row's plate (the cell and half the gap above and below); no fill |
| Both | the fill and the bar |

A status spine stands right of the bar, 1 point apart. The keyboard's cell shows a ring (1 point,
1.5 points inside the cell so the scroller does not cut it, radius 4.5) only while the keyboard is
used, as the web's `:focus-visible`: a click on any cell (checkbox, ⌘- and ⇧-click, a link and ⋮
included) moves the keyboard there without the ring, so ↑↓ continue from the row clicked; the next
key the table takes (not F1–F12, the hotbar's) or a focus that comes by Tab shows it. The ring
never shows on the drawer's row, which its bar marks.

**Why:** A fill and a bar are two kinds of mark, not two shades of one blue (as VS Code's lists mark
the selection and the focused item). The ring answers where the keys act, which matters only while
the keyboard is used.

**Rejected:** a weak and a strong fill — two shades of one blue read alike; an outline round the
drawer's row — three marks on one row where the bar suffices.

**Not covered:** Tab between the drawer row's columns shows no cell.

**Code:** [`list_rows.go`](../../internal/ui/fyneui/list_rows.go) `rowPlate`, `layoutLead`,
[`tablekeys.go`](../../internal/ui/fyneui/tablekeys.go),
[`tableclick.go`](../../internal/ui/fyneui/tableclick.go) · 
`TestRowMarksTellCheckedFromTheDrawer`, `TestTableClickMovesTheKeyboardWithoutTheRing`,
`TestTableFocusByKeyboardShowsTheRing`, `TestRenderRowMarksDark`

## D-125: An answer's speed: TTFT and OTPS

**Status:** built · **By:** owner

Under each answer of the agent chat, beside its copy icon, muted and small: "TTFT 840 ms · OTPS
42.1" (`chatTimingText`).

- **TTFT:** from the request sent to its first streamed part (content, thinking or a tool call).
- **OTPS:** output tokens over the time from the first part to the last. The tokens are the
  endpoint's `usage.completion_tokens` (asked for with `stream_options.include_usage`; an endpoint
  that refuses the option gets the request again without it), else about four bytes a token, marked
  "≈". An answer in one part shows TTFT only.
- Kept with the conversation's item (`ChatItem.Timing`, `engine.ChatTiming`).

**Why:** TTFT is the wait the user feels, thinking included; OTPS measures generation alone, since
TTFT holds the wait.

**Rejected:** TTFT to the first visible text — a thinking model's first visible word can come much
later.

**Code:** [`agent/client.go`](../../internal/agent/client.go),
[`agent/turn.go`](../../internal/agent/turn.go) `timingOf`,
[`chat_items.go`](../../internal/ui/fyneui/chat_items.go) `chatTimingText` · 
`TestAnswerTiming`, `TestChatTimingText`

## D-126: A text field passes the wheel on

**Status:** built · **By:** owner

The wheel never sticks in a text field: a single-line field hands a vertical wheel to the scroller
around it and keeps a sideways swipe; a multi-line field scrolls itself and hands the wheel on once
its offset stops moving. Every Cogpit text field (settings, dialogs, the chat, the searches, the
Secret drawer) draws a transparent `wheelPass` over itself that takes only the wheel; clicks, hover,
drags, the context menu and the cursor stay the field's. Where a plain Fyne Entry would do, Cogpit
uses `wheelEntry`. The scroller it hands the wheel to is the innermost of Cogpit's own scrollers
whose bounds hold the pointer and the field: a new scroller must be made with `newVScroll`,
`newScroll` or `newHScroll` (registered, held weakly), or a field inside it keeps the wheel.

**Why:** Fyne 2.8's Entry keeps its text in a scroller of its own whenever its wrapping is not off,
the driver hands the wheel to the topmost Scrollable under the pointer, and a scroller always takes
it, at its end too (a single line's turns a vertical wheel sideways).

**Rejected:** a fork of Fyne's scroller — one more fork to carry through every Fyne update; walking
the window's tree for the outer scroller — Fyne's tree walk is internal.

**Not covered:** fields inside Fyne's own dialogs (their scroller is not Cogpit's); the YAML editor
(its own widget).

**Code:** [`wheelpass.go`](../../internal/ui/fyneui/wheelpass.go) · 
`TestATextFieldPassesTheWheelOn`, `TestEveryTextFieldPassesTheWheel`

## D-127: Find in the drawer's YAML

**Status:** built · **By:** owner

A find bar stands over the drawer's read-only YAML at all times (`newViewerFindBar`): the editor's
find bar without its replace row and its ×: the field, Aa, ab, .*, the count and the chevrons; the
count follows the object's updates. ⌘F focuses it while the drawer shows its YAML and the keyboard
is in the drawer, in the list's table or nowhere (`drawerFinds`); a text field elsewhere keeps its
own ⌘F, and with the keyboard nowhere the drawer goes before the dock's editor. Escape in the field
clears it and gives the YAML the keys; the next Escape closes the drawer
([D-116](#d-116-escape-closes-the-innermost-thing)). The same bar stands over a Helm release's
values ([D-130](#d-130-find-in-a-releases-values)) and revisions
([D-131](#d-131-a-releases-revisions)).

**Why:** In a read-only view finding is the main action, so the bar stays shown instead of waiting
behind ⌘F; a click on a row leaves the keyboard in the table, so ⌘F from there must reach the
drawer.

**Code:** [`findbar.go`](../../internal/ui/fyneui/findbar.go) `newViewerFindBar`,
[`findkeys.go`](../../internal/ui/fyneui/findkeys.go) `drawerFinds`,
[`drawer.go`](../../internal/ui/fyneui/drawer.go) · **Tests:** `TestDrawerYAMLFind`

## D-128: A release's history in its drawer

**Status:** built · **By:** owner

A release's drawer shows every revision Helm's records keep.

- Properties has Description after Status: the newest revision's line, wrapped
  (`DetailItem.Wrap`).
- History, the drawer's last section: Revision, Status (a failed one in the error colour), Chart,
  App Version and Updated ([D-31](#d-31-every-timestamp-in-the-local-time-zone-and-format)), newest
  first, at most 100 revisions (`historyRows`). Each row's description lies under it on a plate of
  the input background, a failed one's in the error colour (`DetailTable.LastUnder`). A revision's
  number opens the Revisions tab ([D-131](#d-131-a-releases-revisions)).
- A description is scrubbed of its revision's values (`helm.ScrubValuesExcept`) except what the
  release object shows anyway (the release's name and namespace, the chart's name and version, the
  app version); a value that contains one of those is still replaced whole. Agents read the History
  in `object.get`.

**Why:** Helm's record keeps one description line per revision (`release.Info`) and no hook output,
and the Releases list shows only the newest revision, so a failed upgrade that a rollback covered
shows nowhere else. The description goes under its row because a sixth column would leave each text
about 100 of the drawer's 600 points; wrapped in Properties, a failed newest revision's error shows
without scrolling.

**Not covered:** a mark in the Releases list for a release rolled back after a failure; hook output,
which Helm does not keep.

**Code:** [`helm/detail.go`](../../internal/helm/detail.go) `historySection`,
[`helm/values.go`](../../internal/helm/values.go) `ScrubValuesExcept`,
[`fyneui/drawertable.go`](../../internal/ui/fyneui/drawertable.go) `underTable` · 
`TestReleaseHistoryShowsAFailedUpgrade`, `TestScrubValuesExcept`, `TestDeriveFromConfigMaps`,
`TestReleaseHistoryShowsTheDescriptionUnderItsRow`

## D-129: Back brings the place back

**Status:** built · **By:** owner

A tab's history holds places (`TabPlace`): a view with its namespace and API group filters, search
text, status filter and drawer. Go Back and Go Forward (`ui.tab.back`, `ui.tab.forward`) bring a
place back as the tab left it; the place left goes the other way.

- `ui.navigate` is a step (`MarkStep`) even within the shown view; a row click (`ui.select`) is not.
- Checked rows come back from the tab's memory per view; search toggles and sort stay as they are.
- The window keeps each place's scroll offset by its name (`ViewTab.Place`, not in `ui.state`) and
  restores it without scrolling the drawer's row into view; a view opened anew starts at its top.
- `ui.state` lists a history entry as an object (`view`, `namespaces`, `groups`, `filter`,
  `states`, `drawer`). The workspace file keeps only the places' views
  ([D-41](#d-41-what-survives-a-restart)).

**Why:** Back means the previous point, drawer, filters and scroll included; `ui.navigate` clears a
namespace filter that hides its object, so back must restore it. A followed link is what back
undoes (an Argo CD app of apps, an owner of the same kind); browsing a list is not navigating.
Toggles and sort brought back would undo what the user set since.

**Not covered:** the drawer's own state (its tab, its scroll); a page's scroll (the Overview, a
dashboard); a place's filters and drawer across a restart.

**Code:** [`engine/tabs.go`](../../internal/engine/tabs.go) `TabPlace`, `stepHistory`, `MarkStep`;
[`fyneui/tabscroll.go`](../../internal/ui/fyneui/tabscroll.go) · 
`TestTabHistoryBringsThePlaceBack`, `TestBackComesBackToTheDrawer`,
`TestBackBringsTheListBackWhereItWasLeft`, `TestBackWithinTheViewBringsTheListBack`

## D-130: Find in a release's values

**Status:** built · **By:** owner

The Values editor of a release's drawer has [D-127](#d-127-find-in-the-drawers-yaml)'s find bar over
it for good, between the "User-supplied values only" checkbox and the editor, without a replace
row. ⌘F in the editor puts the keys in its field; ⌘F from the list, or with the keys nowhere, stays
the list's search.

**Why:** The drawer is where a release's values are read first, and a bar shown for good is found
without knowing ⌘F. Replacing belongs to the Helm Upgrade tab's bar, which rewrites the values. The
editor lies below Properties, often out of view, so ⌘F from the list keeps its meaning.

**Not covered:** a search in the release's Notes (selectable text, not an editor).

**Code:** [`fyneui/helmdrawer.go`](../../internal/ui/fyneui/helmdrawer.go) `releaseValues`,
[`fyneui/findbar.go`](../../internal/ui/fyneui/findbar.go) `newViewerFindBar` · 
`TestReleaseValuesFind`

## D-131: A release's revisions

**Status:** built · **By:** owner

A release's revisions, each one's values and rendered manifest compared with another revision, are
read in a dock tab "Revisions · <release>", one per release (`ui.helm.revisions`; its state
`DockTab.HelmRevisions`, set by `ui.helm.revisions.set`; [usage](usage.md#a-releases-revisions)).

- Each revision is decoded on demand from its Helm record in the cache
  (`sh.helm.release.v1.<release>.v<N>`, both drivers) as untrusted input (`maxPayload`), at most
  500 manifest objects; nothing asks the API server. `helm.release.revision` reads one revision,
  `helm.release.diff` compares two.
- "Current" is the newest revision, as `helm get` means it; an older revision opens compared with
  it. A comparison reads from the compared revision to the shown one by default, so against the
  current revision it shows what a rollback to the shown one would change; Preferences › App ›
  Revision diffs (`Preferences.RevisionsDiff`) can make it older to newer or shown to compared.
- Compared values are the changed key paths (lists by element, `ingress.hosts[1]`), at most 1,000,
  the line diff folded under them. Objects are listed as `kubectl get` lists them, in Helm's install
  order (`releaseutil.InstallOrder`); an object's YAML is in the Kubernetes order
  ([D-86](#d-86-the-order-of-an-objects-yaml)) under its `# Source:` line, its diff compacted.
- A rendered Secret's values are masked (dots and size) until Show, which is the window's alone and
  ends with the tab; copy and Save as… put out what is shown. Agents read values and rendered
  Secrets withheld and compared values as key paths only
  ([H-144](../security/agents-and-mcp.md#h-144-a-changed-values-key-path-reaches-agents)).
- Texts worth copying are selectable ([D-25](#d-25-copying-values)); numbers, statuses, kinds and
  titles are plain, and a click on a row's selectable text still picks the row.

**Why:** Values and rendered objects compared across revisions show what an upgrade changed, which
is too much for the drawer. Masking parses the Secret, which finds every value where a line match
might not. Show stays in the window so that an agent never unmasks a value on the user's screen;
key paths tell an agent which keys an upgrade changed, not to what.

**Not covered:** drift against the live object; a revision's notes and hooks; revisions Helm pruned;
the tab does not follow a revision added while it is open.

**Code:** [`helm/revisions.go`](../../internal/helm/revisions.go),
[`engine/helmrevisions.go`](../../internal/engine/helmrevisions.go),
[`fyneui/helmrevisions.go`](../../internal/ui/fyneui/helmrevisions.go) · 
`TestRevisionOf`, `TestValueChanges`, `TestReleaseRevisionWithheldFromAgents`,
`TestRevisionsDiffSides`, `TestRevisionsTabFromTheDrawer`, `TestRevisionsRowTextsPassTheirClick`

## D-132: A cluster upgraded while connected

**Status:** built · **By:** owner

Cogpit follows the API server's version through the connection watch's heartbeat, which sends
`GET /version` every 15 s anyway ([D-58](#d-58-standby-and-lost-connections)); `cluster.Conn`
keeps the answer's `gitVersion` (`SeenVersion`).

- A different version is taken once two heartbeats in a row read it (`noteVersion`): the
  navigator's version and its upstream support
  ([D-91](#d-91-the-kubernetes-version-and-upstream-end-of-life)), the Nodes list's version check
  and a newly opened terminal's kubectl skew follow; the connection log notes it.
- Another minor than the connect's sets `Connection.Upgraded` {from, to, since} until a reconnect
  (a version back in the connect's minor clears it). The banner over the view names both versions,
  says the kinds and API versions are the old one's, and offers Reconnect (`cluster.reconnect`,
  which keeps the tabs, [D-81](#d-81-reconnect-and-what-log-in-again-does)); it has no dismiss.

**Why:** Following the version costs no request. A patch changes no API; a minor may add kinds and
stop serving beta versions, which only a new discovery sees
([D-65](#d-65-discovery-cached-on-disk)). During a rolling upgrade of several API servers behind one
address the answers alternate for minutes, so one read would make the version flap.

**Rejected:** rebuilding the discovery and the informers in place — a new, costly mechanism for an
event of a few times a year; the version alone — new kinds would be missing without a word.

**Not covered:** a kind the server stopped serving shows the server's refusal in its view until the
reconnect; a cluster whose heartbeat pauses (a lapsed login) is not followed meanwhile.

**Code:** [`engine/connwatch_version.go`](../../internal/engine/connwatch_version.go) `noteVersion`;
[`fyneui/connbanner.go`](../../internal/ui/fyneui/connbanner.go) `fillUpgraded` · 
`TestTheServerVersionFollowsTheHeartbeat`, `TestAVersionThatAlternatesIsNotTaken`,
`TestHeartbeatAndReachable`, `TestConnectionBannerSaysTheClusterWasUpgraded`

## D-133: Connection Diagnostics

**Status:** built · **By:** owner

One dock tab, Connection Diagnostics (Help › Connection Diagnostics, `diagnostics.open`), holds the
shown cluster's connection, its problems since the latest connect, the rate limits, the diagnostics
log, the requests, the watches, and the objects and memory
([usage](usage.md#connection-diagnostics-what-the-requests-ran-into)). The status bar keeps one
pill, "Diagnostics": muted, or the warning colour and the count of the log's problem lines since
the latest connect.

- **Log** ([`cluster/diaglog.go`](../../internal/cluster/diaglog.go)): per context, in memory only,
  across the context's connections in the run, for the 8 contexts used last; at most 500 lines,
  each text at most 1,000 bytes. A repeated problem (same kind, method, path, selectors, status)
  folds into its line with a count; the line seen least recently goes first. A connect, a kind read
  namespace by namespace after a refusal, and a cache warming up (a 429 "storage is
  (re)initializing") are grey and not counted, the last unless the same request gets it again
  within a minute (`cluster.WarmingRepeatWithin`).
- **Sources**, which send nothing: a round-tripper above the request timeout and the gates
  ([`diagtransport.go`](../../internal/cluster/diagtransport.go)), the token buckets
  (`Conn.Budgets`), the cache's load times and fallbacks (`Cache.OnDiagnostic`). A Secret's error
  answer keeps only its status
  ([H-8](../security/privilege-footprint.md#h-8-secret-data-arrives-with-the-list)).
- **Report** (`diagnostics.report`, [`engine/diagscrub.go`](../../internal/engine/diagscrub.go)):
  plain text, as Report shows it and Copy and Save Report… put it out. Hosts and addresses become
  `host-1`, `ip-1` throughout, ports go, file paths become `<path>`; URL paths, users, groups,
  namespaces, objects and API groups stay. `diagnostics.get` gives agents the real values.

**Why:** A slow or refused connection must be traceable by the user and shareable as a log a
colleague can work with: resource names kept, endpoints not; one place for the numbers instead of
the status bar. A retried refusal is one problem, not a hundred. An API server answers a watch 429
while it builds its cache of a resource, and client-go asks again: no fault unless it repeats.

**Not covered:** a view waiting for the access reviews has no line of its own; the anonymous
reachability probe's requests; QPS and burst are fixed, no preference.

**Code:** [`engine/diaglogs.go`](../../internal/engine/diaglogs.go),
[`fyneui/diagnostics.go`](../../internal/ui/fyneui/diagnostics.go) · 
`TestDiagLogFoldsTheSameProblem`, `TestDiagLogIsBounded`, `TestDiagRoundTripperNotesProblems`,
`TestEndpointScrubber`, `TestConnectionDiagnosticsOfALimitedUser`, `TestCacheWarmingIsNoProblem`

## D-134: The window's zoom

**Status:** built · **By:** owner

Zoom In, Zoom Out and Reset Zoom (View menu and the status bar's − percent +; `ui.zoom.in`,
`ui.zoom.out`, `ui.zoom.reset`, from any focus and for agents) work as VS Code's: the whole window,
text, icons, room and panels alike, scales by 1.2 per level and keeps its size on the screen. The
level is one preference, `Preferences.ZoomLevel`, kept across starts ([usage](usage.md#zoom)).

- **Mechanism** ([`fyneui/zoom.go`](../../internal/ui/fyneui/zoom.go)): Fyne's canvas scale, which
  Fyne reads from `FYNE_SCALE` on every recalculation and has no call for. Cogpit sets `FYNE_SCALE`
  in its own environment to the start scale times the factor, applies the theme again and gives the
  window back its size in the same pass; the local terminal's shell gets `FYNE_SCALE` as Cogpit was
  started with it (`terminal.baseEnv`).
- **Range** −4 to 4: 50% to 210%. Zoom In stops where the content would not fit the window (the +
  greyed with the reason, an agent's call refused with `ErrZoomFit`); the window reports its
  largest fitting level (`SetZoomCeiling`) when its size or the zoom changes.
- **Keys:** VS Code's on each platform. The registry carries a command's keys per platform and its
  further keys (`Spec.PlatformKeys`, `Spec.MoreKeys`,
  [`engine/keys.go`](../../internal/engine/keys.go)), so Go Back and Go Forward move to VS Code's
  keys where Ctrl+- zooms. Further keys ride on separators of a menu of their own at the main
  menu's end ([`menukeys.go`](../../internal/ui/fyneui/menukeys.go)): a menu item has one shortcut,
  and Fyne looks a typed shortcut up in the main menu, separators included, before the focused
  widget.

**Why:** VS Code's zoom works on every display and in presentations because it scales the whole
window on top of the display's own scale; Fyne's canvas scale is the same mechanism.

**Rejected:** a factor on the theme's sizes — Cogpit's fixed sizes would not scale; letting Fyne
widen the window to its content's minimum size — VS Code's window never grows when it zooms; VS
Code's −8 to 8 — above 210% the content does not fit a laptop screen.

**Not covered:** content that grows after the window last changed its size (a drawer opened at 170%)
is checked again only at the next Zoom In, and an agent's `ui.zoom.in` uses that last fit.

**Code:** [`engine/zoom.go`](../../internal/engine/zoom.go),
[`commands/zoom.go`](../../internal/engine/commands/zoom.go) · **Tests:** `TestZoom`,
`TestZoomKeys`, `TestKeysDoNotCollideOnAnyPlatform`, `TestZoomFitsWindow`, `TestWindowZoomScale`

## D-135: A stalled stream lists

**Status:** built · **By:** owner

- **Lists by default** ([`engine/initiallists.go`](../../internal/engine/initiallists.go)): each
  read lists its objects once (`resourceVersion=0`, from the API server's watch cache) and then
  watches from that list's version (`cache.Options.Lists`: the list-watcher reports WatchList
  unsupported).
- **The lists' budget:** the cache's dynamic and metadata clients (`Conn.CacheDynamic`,
  `CacheMetadata`) share the connection's HTTP client with a token bucket of their own, 50 a second,
  burst 300 (`listQPS`, `listBurst`); gets and writes stay at 5 and 10 (`DefaultQPS`,
  `DefaultBurst`).
- **Streaming, an opt-in beta** per cluster (Cluster Settings › General › Debug, "Use initial
  streaming (BETA)"; `cluster.settings.set` `streamInitialLists`): on applies from the next connect,
  off at once (`Cache.ListFromNowOn`).
- **The safety net** ([`cache/stall.go`](../../internal/cache/stall.go)): a streamed initial list
  that receives nothing for 10 s before its end (`cache.DefaultStreamStall`; silence, since a large
  list streams long) is ended; client-go lists in its place, every read of the connection lists
  from then on, and the streams still running end at once. Connection Diagnostics writes a "not
  streamed" line; the setting stays, and the next connect streams again. Client-go's own fallback
  does the listing, no fork: an error event (Status 504, reason `Timeout`), or the request's context
  cancelled with the stall as cause while no answer came.

**Why:** Streaming came with client-go's informers (`WatchListClient`, beta, on by default), not
from a choice of Cogpit's. It is not faster on a healthy server, and client-go waits without a bound
for the bookmark that ends a stream: where a cluster's streams go silent before their end, a read
waits minutes while a list answers at once. Lists from the watch cache are cheap for the API server;
at the shared 5 a second, a connect that reads many kinds would wait seconds.

**Rejected:** a 3 s bound — trips on a server that waits before it answers (its watch cache, its
priority queues); a stall switching streaming off in the settings — the user switched it on on
purpose; the shared limit raised — it guards the cluster against a burst of gets and writes.

**Not covered:** if something on the path holds streams back, plain watches deliver changes late
too, unseen by Cogpit; with streaming on, a cluster whose streams stall waits 10 s at every connect.

**Code:** [`cluster/cluster.go`](../../internal/cluster/cluster.go),
[`fyneui/clustersettings_streams.go`](../../internal/ui/fyneui/clustersettings_streams.go) ·
**Tests:** `TestStalledStreamLists`, `TestStallEndsTheOtherStreams`, `TestListsWithoutStreams`,
`TestStalledStreamsList`, `TestStreamingOptIn`
