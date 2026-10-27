# Architecture

How Cogpit is built, for a developer about to change it: what runs where, the packages and their
layers, the engine/UI seam, the cache, cluster connections and each subsystem. Why it is built so is
in [decisions](decisions.md); what a user sees is in [usage](usage.md).

## What runs where

- **`cogpit`** (Fyne, cgo: Fyne renders through OpenGL) runs the engine, the window, the MCP server
  and the built-in agent in one process ([§12](#12-runtime-model)). The engine keeps one backend per
  connected context, each with its own cache, connection watch, discovery, access reviews, port
  forwards and dock streams; the window shows one ([§6](#d-69-the-pool-of-backends)). A command acts
  on the cluster its `context` parameter, its dock tab or its object references name, else on the
  shown one; a context that is not connected is an error (`NotConnectedError`): nothing connects by
  itself ([§4.3](#d-69-several-clusters-connected)).
- **`cogpit-cli`** (pure Go, `CGO_ENABLED=0`, no display needed) runs a headless engine over the
  app's kubeconfig sources, credential plugin approvals, cluster settings (read only), discovery
  cache and connection log, without a confirmer (a command that needs a confirmation is refused),
  journals or hotbar ([`env.go`](../../cmd/cogpit-cli/env.go)). Without Cogpit's configuration
  directory it does not connect; the app then merges the kubeconfig files as kubectl does
  ([§6](#kubeconfig-sources)). `cogpit-cli mcp` forwards to the running app's MCP endpoint, else
  serves an engine of its own ([§7](#7-mcp-and-the-built-in-agent)).
- **Every package ships both binaries** ([D-16](decisions.md#d-16-releases-and-what-they-carry):
  inside `Cogpit.app/Contents/MacOS/`;
  [D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages): `/usr/bin/`;
  [D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle): one folder). The app
  offers agents the stdio registration of the `cogpit-cli` it came with (`bundledCLI` in
  [`bundledcli.go`](../../cmd/cogpit/bundledcli.go): never from a translocated or read-only bundle,
  on Windows only in a release), else the HTTP registration ([§7](#7-mcp-and-the-built-in-agent)).
- **Release builds** carry the build tags `nodemo` and `nothreadcheck` (the Makefile's `DIST_TAGS`
  and `FYNE_THREAD_CHECKS`). `nodemo` leaves [`internal/demo`](../../internal/demo/) out of both
  binaries; its only importer is
  [`engine/connector_demo.go`](../../internal/engine/connector_demo.go)
  ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)), and `make check-nodemo` lints
  and tests that variant. `nothreadcheck` turns Fyne's thread check off ([§12](#12-runtime-model)).
- **Process-wide** ([`cmd/cogpit/main.go`](../../cmd/cogpit/main.go)): `utilruntime.ReallyCrash` is
  off, so a panic in a client-go goroutine is logged and the cache reports the stopped informer;
  crash output goes to `crash.log` (`runtime/debug.SetCrashOutput`) and is kept in `crashes/` at the
  next start ([`crash.go`](../../cmd/cogpit/crash.go), [D-11](decisions.md#d-11-crash-stance));
  `time/tzdata` is linked in for Cron Jobs' `spec.timeZone` on Windows.

### Platforms

| | macOS | Linux | Windows |
|---|---|---|---|
| Package ([releasing](releasing.md)) | a signed, notarized disk image | a `.deb` for amd64 and arm64 ([`hack/linux/deb.sh`](../../hack/linux/deb.sh), [`packaging/linux`](../../packaging/linux)) and Cogpit's APT repository, signed by [`hack/linux/apt.sh`](../../hack/linux/apt.sh) | an NSIS setup per user ([`hack/windows/setup.sh`](../../hack/windows/setup.sh), [`packaging/windows/cogpit.nsi`](../../packaging/windows/cogpit.nsi)), not signed with Authenticode ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)) |
| Updates ([§11](#11-updates-internalupdate)) | Sparkle 2 | apt | WinSparkle |
| Local terminal ([§10](#10-terminal-internalterminal)) | PTY | PTY | ConPTY, the shell in a job object |
| `kubectl` first on a terminal's `PATH` ([§10](#kubectl-in-terminals)) | zsh, bash and fish put it first again after the profile ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first)) | as macOS | its folder first (a link or copy named `kubectl.exe` for another name); no enforcing function |
| Title bar, menus | Cogpit's own title bar through a cgo bridge to the `NSWindow` ([`titlebar_darwin.go`](../../internal/ui/fyneui/titlebar_darwin.go); `COGPIT_SYSTEM_TITLEBAR=1` keeps the system's) | menus in the top bar | menus in the top bar |
| File choosers ([`filepicker.go`](../../internal/ui/fyneui/filepicker.go)) | the system's panels, their grants kept as bookmarks ([D-72](decisions.md#d-72-native-file-panels-and-macos-folder-access)) | Fyne's dialogs; kubeconfigs in Cogpit's own Sync browser ([`syncpicker.go`](../../internal/ui/fyneui/syncpicker.go)) | as Linux |
| Window place ([`windowplace.go`](../../internal/ui/fyneui/windowplace.go), [D-95](decisions.md#d-95-the-window-comes-back-where-it-was)) | frame, display, full screen ([`windowplace_darwin.m`](../../internal/ui/fyneui/windowplace_darwin.m)) | size, full screen | size, full screen |
| Date and time format ([`internal/locale`](../../internal/locale/), [D-31](decisions.md#d-31-every-timestamp-in-the-local-time-zone-and-format)) | the region, through CoreFoundation | `LC_ALL`, `LC_TIME`, `LANG` | the same variables; the region settings are not read |
| Wake and network-change signals ([D-58](decisions.md#d-58-standby-and-lost-connections)) | yes ([`connhints_darwin.m`](../../internal/ui/fyneui/connhints_darwin.m)) | none: the heartbeat and broken streams find a lost connection | as Linux |
| A copied Secret value ([H-25](../security/credentials.md#h-25-a-copied-secret-value-reaches-the-clipboard)) | marked concealed and for this Mac only; cleared after 60 s | cleared after 60 s | cleared after 60 s |
| `cogpit://` links ([D-47](decisions.md#d-47-deep-links-cogpit)), start at login ([D-48](decisions.md#d-48-start-at-login)) | yes | no | no |
| Cogpit's private files | owner-only modes | owner-only modes | the profile's inherited ACLs; a rename or removal retried for up to 1 s while another process holds the file (`retryHeld`, [`privatefile.go`](../../internal/engine/privatefile.go)) |

## 1. Goals and principles

**Goals:** a native, open-source reimplementation of the current Lens Desktop for macOS, Linux and
Windows ([parity](../parity/README.md), [D-12](decisions.md#d-12-parity-sources)), in Go
([D-7](decisions.md#d-7-language-and-platforms)) on the kubectl and Helm libraries
([D-6](decisions.md#d-6-kubernetes-libraries)); a UI that reads only from a watched cache
([§5](#5-cache)); everything controllable over MCP, the UI included
([D-2](decisions.md#d-2-agent-interface-scope)), the built-in agent on the same tools
([D-10](decisions.md#d-10-built-in-agent)); a UI toolkit replaceable without touching the engine
(Fyne, with a Rust/egui process as the fallback, [D-1](decisions.md#d-1-ui-toolkit)).

**Principles**

1. **Engine-first:** every action works without the GUI, from tests, the CLI and agents.
2. **Everything is a command** with a stable id and typed JSON parameters; menus, the palette,
   shortcuts, the CLI, MCP and the built-in agent dispatch the same commands.
3. **UI state is engine state** (§4.3): the toolkit renders it, MCP reads and sets it.
4. **Data describes the UI:** lists are table snapshots, details section models, menus the registry
   filtered by kind and state.
5. **Cache first:** only the cache lists and watches (§5).
6. **Discover, don't assume:** kinds, versions, permissions and metrics providers come from the
   cluster at runtime.
7. **Cluster-safe:** every mutating command declares its verbs and danger; the engine confirms for
   every origin.
8. **Crash-tolerant UI, crash-safe cluster operations** ([D-11](decisions.md#d-11-crash-stance)).
9. **Libraries over binaries:** client-go, the kubectl libraries and the Helm SDK run in-process;
   external programs only for credential plugins, the terminal's shell and its `kubectl`
   ([D-3](decisions.md#d-3-helm-and-kubectl-binaries)) and Linux updates (§11).
10. **Clean-room** against the current Lens Desktop ([conventions](conventions.md#clean-room)).

### 1.1 Structural rules

| Property | Rule | Where |
|---|---|---|
| **API versions** | Kinds resolve through discovery per cluster: curated profiles for built-in kinds, a generic one from discovery and `additionalPrinterColumns` for every other kind. Objects stay unstructured, so fields newer than the client libraries survive an edit | `model`, `cluster` |
| **API server load** | One watch per (cluster, kind, namespace scope), shared by every consumer; nothing that can be watched is polled | `cache` |
| **Scale** | Virtualized lists; sort and filter off the UI goroutine; a view's table snapshot at most every `SnapshotInterval` (100 ms) | `engine`, `ui` |
| **Clusters** | One connection and one cache per kubeconfig context; nothing global assumes one cluster | `engine` |
| **Credentials** | Stay in the process that uses them; no local proxy serves the user's identity to other processes | [credentials](../security/credentials.md) |

A new kind, auth method, agent or UI toolkit touches a leaf package, not the foundation.

## 2. Repository layout

```text
cogpit/
├─ go.mod, Makefile        module github.com/guided-traffic/cogpit; targets: make help
├─ .golangci.yml           linters and the depguard rules of the layering (§3)
├─ .github/workflows/      release.yml (CI), build.yml (release pipeline), renovate.yml
├─ cmd/cogpit/             desktop app (Fyne, cgo)
├─ cmd/cogpit-cli/         CLI and MCP stdio bridge (pure Go)
├─ internal/
│  ├─ model/          L0  object refs, kind profiles, rows, detail sections; pure
│  │  ├─ kinds/       L0  the built-in kind profiles, one file per kind
│  │  └─ k8seol/      L0  Kubernetes' release table, built into each release
│  ├─ kubeconfig/     L0  kubeconfig parsing from bytes, the per-terminal kubeconfig
│  ├─ buildinfo/      L0  the version both binaries report
│  ├─ cluster/        L1  connections, auth, discovery, access reviews, direct calls (§6)
│  ├─ cache/          L1  the only place that lists and watches (§5)
│  ├─ terminal/       L1  local shell on a PTY (ConPTY on Windows), pod and node shells (§10)
│  ├─ kubectl/        L1  the terminals' kubectl: version, skew, verified download
│  ├─ locale/         L1  the computer's date and time format
│  ├─ helm/           L2  Helm SDK in-process (§8)
│  ├─ metrics/        L2  metrics providers (§9)
│  ├─ alerting/       L2  the cluster's Alertmanager (§9)
│  ├─ demo/           L2  the synthetic demo cluster; not in releases (-tags nodemo)
│  ├─ engine/         L3  Session, registry, UI state, jobs, events, confirmation, audit log
│  │  └─ commands/    L3  the commands, one file per area
│  ├─ mcp/            L4  MCP server (Streamable HTTP on loopback) and stdio bridge (§7)
│  ├─ agent/          L4  the built-in agent (§7)
│  ├─ update/         L4  Sparkle 2, apt, WinSparkle (§11)
│  ├─ app/            L4  shared wiring: directories, kubeconfig sources, audit log
│  ├─ ui/fyneui/      L4  the Fyne shell, with termview/ and yamlview/
│  ├─ ui/logo/        L4  the logo, rasterised; pure Go
│  ├─ ui/nativedialog/ L4 the system's file panels (cgo on macOS)
│  └─ testkit/        –   fixtures, fake API server, fake LLM; integration/: fake-server, kind tests
├─ third_party/fyne-terminal/  Cogpit's patched fyne-io/terminal (a go.mod replace)
├─ hack/                   parity, commands page, scorecard, shards, kind, packaging scripts
├─ packaging/              icon and the dmg/, linux/ and windows/ packaging files
└─ docs/
```

### Packages

Where each package's mechanisms are described, and what only this table states:

| Package | Mechanisms |
|---|---|
| [`model`](../../internal/model/), [`model/kinds`](../../internal/model/kinds/), [`model/k8seol`](../../internal/model/k8seol/) | Pure values for [§4.4](#44-tables-details-editors-and-streams) and [§4.5](#45-kind-keys): rows and cells, status tokens, panic-free accessors, Secret redaction, an object's YAML in the Kubernetes order (`ObjectYAML`, [D-86](decisions.md#d-86-the-order-of-an-objects-yaml)), byte amounts in binary units ([`bytes.go`](../../internal/model/bytes.go), [D-32](decisions.md#d-32-every-byte-amount-in-binary-units-with-one-decimal)), usage values (`PodUsage`, `NodeUsage` in [`usage.go`](../../internal/model/usage.go)), image grades ([`imagerating.go`](../../internal/model/imagerating.go)). `kinds`: one file per profile; a custom kind's generic profile comes from its definition's printer columns ([`customresources.go`](../../internal/model/kinds/customresources.go)); a kind without a profile (a built-in kind without one, an aggregated API's) has no view. `k8seol`: upstream's release table, embedded and refreshed by `make k8s-eol` at release build; nothing is fetched at run time ([H-130](../security/updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)). |
| [`kubeconfig`](../../internal/kubeconfig/) | No I/O: contexts from bytes, errors without credentials (`SafeError`), which files of a sync folder are read and how large (`SyncCandidate`, `MaxFileSize` in [`source.go`](../../internal/kubeconfig/source.go)), the per-terminal kubeconfig, a credential plugin's fingerprint ([`exec.go`](../../internal/kubeconfig/exec.go)), a client certificate's dates, its key never read ([`clientcert.go`](../../internal/kubeconfig/clientcert.go)). |
| [`buildinfo`](../../internal/buildinfo/) | The version both binaries report ([`buildinfo.go`](../../internal/buildinfo/buildinfo.go)): the release's from the linker, else `dev` and the commit; the window shows it beside the wordmark ([`buildlabel.go`](../../internal/ui/fyneui/buildlabel.go)). |
| [`cluster`](../../internal/cluster/) | [§6](#6-cluster-connections-internalcluster). Client-go's loading rules without implicit kubeconfig writes: no migration rules, no persisted auth configuration ([`cluster.go`](../../internal/cluster/cluster.go)). One kubeconfig file read on its own (`InspectFile`), sync folder listings and the credential plugin policy ([`sources.go`](../../internal/cluster/sources.go)). Writes through the dynamic client (field manager `cogpit`): update with a `resourceVersion` precondition, create (both optionally a server-side dry run), delete with a UID precondition ([`objects.go`](../../internal/cluster/objects.go)). Patches for Argo CD's Sync and Refresh ([`argocd.go`](../../internal/cluster/argocd.go), [D-36](decisions.md#d-36-how-argo-cds-sync-writes-the-operation)), for a removal of finalizers (`FinalizersPatch`: tests the UID and the confirmed finalizers; never a namespace's, never `/finalize`) and for a debug container (`AddEphemeralContainer`: the pod's `ephemeralcontainers` subresource, bound to its UID). Port-forward tunnels on loopback listeners Cogpit owns across reconnects ([`portforward.go`](../../internal/cluster/portforward.go)). |
| [`cache`](../../internal/cache/) | The only place that lists and watches: [§5](#5-cache). |
| [`terminal`](../../internal/terminal/) | [§10](#10-terminal-internalterminal). Pod shells over exec, WebSocket with a SPDY fallback ([`exec.go`](../../internal/terminal/exec.go)); attach waits through the cache for a starting container (`StartPodAttachAfter`); a node shell runs `nsenter` through a pod's exec ([`nodeshell.go`](../../internal/terminal/nodeshell.go), [D-37](decisions.md#d-37-the-node-shell)). |
| [`kubectl`](../../internal/kubectl/) | [kubectl in terminals](#kubectl-in-terminals). |
| [`locale`](../../internal/locale/) | The computer's date and time format ([`system_darwin.go`](../../internal/locale/system_darwin.go), else the environment), converted to Go layouts (`GoLayout`); ISO 8601 where a pattern does not convert. |
| [`helm`](../../internal/helm/), [`metrics`](../../internal/metrics/), [`alerting`](../../internal/alerting/) | [§8](#8-helm-internalhelm), [§9](#9-metrics-internalmetrics), [Alertmanager](#d-77-alertmanager-internalalerting). |
| [`demo`](../../internal/demo/) | Development builds only: a synthetic cluster behind a `cache.Source` (by default 10,000 pods, 300 services, 1,000 watch events per second; watch resume and 410 Gone), a synthetic Metrics Server and node disks ([`disks.go`](../../internal/demo/disks.go)), Helm release records. |
| [`engine`](../../internal/engine/), [`engine/commands`](../../internal/engine/commands/) | [§4](#4-the-ui-seam-the-most-important-interface) (`Session`, registry, UI state, tables, jobs, drafts), the pool and connect runs ([§6](#d-69-the-pool-of-backends)), integrations ([§4.7](#47-integrations-d-76-d-82)), the port-forward manager ([`portforward.go`](../../internal/engine/portforward.go)). `object.create` runs in its dispatch call, without a job. Debug containers ([`debugcontainer.go`](../../internal/engine/debugcontainer.go), [D-102](decisions.md#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)): destructive for every origin; the new container reaches Cogpit through the pod's watch, then Attach opens on it. |
| [`mcp`](../../internal/mcp/), [`agent`](../../internal/agent/) | [§7](#7-mcp-and-the-built-in-agent). The MCP server is stateless: each request is its own session. |
| [`update`](../../internal/update/) | [§11](#11-updates-internalupdate). No automated test installs an update end to end. |
| [`app`](../../internal/app/) | What both binaries share: Cogpit's directories (`UserDirs`), the kubeconfig sources, the audit log, the discovery cache and connection log folders. |
| [`ui/fyneui`](../../internal/ui/fyneui/), [`yamlview`](../../internal/ui/fyneui/yamlview/), [`termview`](../../internal/ui/fyneui/termview/) | The window: it draws engine state and changes it through `ui.*` commands; window-only state is in [§4.3](#layout-panels-and-window-only-state). Row menus, the drawer's toolbar and the palette come from the registry, parameter dialogs from a command's schema and `Spec.Dialog` ([`paramdialog.go`](../../internal/ui/fyneui/paramdialog.go)), each dialog that names an object a `dialogFrame` ([`dialogframe.go`](../../internal/ui/fyneui/dialogframe.go)), each dropdown a `selectField` ([`selectfield.go`](../../internal/ui/fyneui/selectfield.go)); Cogpit draws its own tooltips (Fyne has none). Escape closes the innermost thing open ([`escape.go`](../../internal/ui/fyneui/escape.go), [D-116](decisions.md#d-116-escape-closes-the-innermost-thing)). The terminal widget is a patched copy of fyne-io/terminal ([`third_party/fyne-terminal`](../../third_party/fyne-terminal/PATCHES.md)) behind an escape filter ([`sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go)). Theme: theme and status tokens ([design-language.md](design-language.md), [D-110](decisions.md#d-110-dark-cockpit-cogpits-design-language); the terminal's sixteen colours, [D-22](decisions.md#d-22-colour-theme)); rules for dialogs and Escape: [conventions › UI](conventions.md#ui). |
| [`ui/nativedialog`](../../internal/ui/nativedialog/) | macOS's open and save panels through cgo, shown as a sheet, answered on the main thread ([`panel_darwin.m`](../../internal/ui/nativedialog/panel_darwin.m)); `ErrUnsupported` elsewhere. Security-scoped bookmarks of the picked items ([`store.go`](../../internal/ui/nativedialog/store.go)), restored before any kubeconfig source is read ([`fileaccess.go`](../../cmd/cogpit/fileaccess.go)). |
| [`ui/logo`](../../internal/ui/logo/) | The logo ([`logo.go`](../../internal/ui/logo/logo.go)), every version kept drawable ([`versions.go`](../../internal/ui/logo/versions.go)), rasterised for the app icon (`hack/icon`) and the window ([D-24](decisions.md#d-24-logo)). |
| [`cmd/cogpit`](../../cmd/cogpit/) | Wires the packages and Cogpit's files ([usage › Naming conventions](usage.md#naming-conventions)), installs the built-in agent (`installChat` in [`chat.go`](../../cmd/cogpit/chat.go)) and restores the last session before the window runs (`Session.RestoreLastSession`); on macOS the login item ([`loginitem_darwin.m`](../../cmd/cogpit/loginitem_darwin.m)) and the deep links' Apple events ([`deeplink_darwin.m`](../../cmd/cogpit/deeplink_darwin.m)). `--smoke` runs it in a home of its own until the window's first frame ([`smoke.go`](../../cmd/cogpit/smoke.go), [releasing › the smoke test](releasing.md#the-smoke-test)). |
| [`cmd/cogpit-cli`](../../cmd/cogpit-cli/) | `contexts`, `get`, `mcp` ([`mcp.go`](../../cmd/cogpit-cli/mcp.go)) and `version`, dispatched with origin `cli` ([usage › cogpit-cli](usage.md#cogpit-cli)). |
| [`testkit`](../../internal/testkit/) | Test-only (`depguard`'s `no-testkit-in-product`; [testing › Fixtures](testing.md#fixtures)): an in-memory source, the fake API server over HTTPS, a forward proxy ([`proxy.go`](../../internal/testkit/proxy.go)) and one that goes silent (`FreezableProxy`), a kubectl release host; [`enginekit`](../../internal/testkit/enginekit/) (the real engine for the window's tests), [`fakellm`](../../internal/testkit/fakellm/) (an OpenAI-compatible endpoint), [`sigstoretest`](../../internal/testkit/sigstoretest/) (a sigstore of the tests' own), [`envtest`](../../internal/testkit/envtest/envtest.go) (a real kube-apiserver and etcd, Unix only, [D-64](decisions.md#d-64-envtest-a-real-api-server-in-ci-without-docker)), [`prereq`](../../internal/testkit/prereq/prereq.go) (what a test needs of the machine: skipped without it, failed in CI; [testing › Prerequisites](testing.md#prerequisites)), [`integration`](../../internal/testkit/integration/) (cross-package and kind tests). |

## 3. Dependency layering (enforced)

```text
 L5  cmd/*
 L4  ui/fyneui · ui/logo · ui/nativedialog · mcp · agent · update · app
 L3  engine (and engine/commands)
 L2  helm · metrics · alerting · demo
 L1  cluster · cache · terminal · kubectl · locale
 L0  model · kubeconfig · buildinfo
```

`make lint` enforces it with the `depguard` rules in [`.golangci.yml`](../../.golangci.yml):

1. No package imports a higher layer; L1 packages do not import each other, nor do `ui`, `mcp`,
   `agent`, `update` and `app`.
2. **Only `internal/ui/...` imports Fyne.**
3. **`model`, `kubeconfig` and `buildinfo` do no I/O** (no client-go clients, `net/http`, `os` or
   `os/exec`): bytes and unstructured content in, values out, which keeps them fuzzable.
4. **Only `cluster`, `cache`, `terminal`, `helm`, `metrics` and `alerting` talk to the API server.**
   The UI uses `helm`'s, `metrics`' and `terminal`'s types but never `cluster`, `cache`, `demo`,
   `kubectl` or client-go; `mcp` and `agent` only dispatch engine commands.
5. Per package: `kubectl` talks only to `dl.k8s.io` (or a mirror) and `rekor.sigstore.dev`, no
   client-go ([D-50](decisions.md#d-50-kubectls-signature-check),
   [H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search));
   `locale` imports only `model`
   ([D-31](decisions.md#d-31-every-timestamp-in-the-local-time-zone-and-format)); `alerting` imports
   no Cogpit package (the engine hands it the cached Services as `alerting.Service`); `update`
   imports only `buildinfo` (on Linux and Windows, to compare the running version with apt's or the
   feed's); `testkit` never enters a binary.
6. A new package gets its layer and its `depguard` rule in the same change.

## 4. The UI seam (the most important interface)

Everything that crosses the seam is JSON-serializable: command parameters and results, table
snapshots, detail models, UI state and events. MCP and the CLI use the same seam, and it would let
the fallback UI of [D-1](decisions.md#d-1-ui-toolkit) run as a separate process.

### 4.1 `Session`: commands in, events out

[`engine.Session`](../../internal/engine/engine.go) is one process's engine, safe for concurrent
use: `Dispatch(ctx, Invocation)` runs a command (§4.2), `Registry()` lists them, `Table(view)`
returns an immutable `TableSnapshot` (replaced, never mutated), `Detail(ref)` the drawer's
`DetailModel`, `UIState()` the UI state (§4.3), and `Subscribe` delivers events by type (`uiState`,
`table`, `detail`, `toast`, `kinds`, ...; [`events.go`](../../internal/engine/events.go)). An
`Invocation` holds the command `ID`, its `Params` (raw JSON) and its `Origin`: `gui`, `cli`,
`externalAgent`, `builtInAgent` or `deeplink` (§4.6). A confirmation is no event: `Dispatch` blocks
on the `Confirmer` the window sets (`SetConfirmer`,
[`fyneui/confirm.go`](../../internal/ui/fyneui/confirm.go)), so it must not run on the GUI's main
goroutine; without one (headless), a command that needs it fails (`ErrConfirmationUnavailable`).

### 4.2 The command registry

A command is an [`engine.Spec[P]`](../../internal/engine/registry.go) registered from
`internal/engine/commands`; `P` is a struct whose JSON schema (`github.com/google/jsonschema-go`)
agents read.

| Field | Meaning |
|---|---|
| `ID` | `<kind-or-area>.<action>`, lowerCamelCase segments |
| `Label`, `MenuPath`, `Description` | menu label; menu (empty: none); MCP tool description |
| `Shortcut`, `MoreKeys`, `PlatformKeys` | "Cmd" is ⌘ on macOS, Ctrl elsewhere; `NewRegistryFor` resolves the keys for its platform ([`keys.go`](../../internal/engine/keys.go)); no menu shows `MoreKeys` |
| `Kinds`, `Order`, `DrawerOnly` | kinds (empty: any); place in the row menu and the drawer's toolbar (0: neither; a kind's actions below Edit at `OrderEdit` 900, Delete at `OrderDelete` 1000); drawer only (Copy, which the window draws after the object's name: [`drawertitlecopy.go`](../../internal/ui/fyneui/drawertitlecopy.go)) |
| `Danger`, `Verbs` | `read`, `ui`, `mutating`, `destructive`; API verbs for access reviews and the [privilege footprint](../security/privilege-footprint.md) (`Verb.Namespace`: a fixed namespace) |
| `Enabled` | why it cannot run now; an error wrapping `ErrNotApplicable` hides it instead (on a cordoned node, Uncordon takes Cordon's place) |
| `Targets` | the objects a write acts on; required for mutating and destructive commands |
| `Escalate` | destructive for these parameters: Scale to zero, removing a Config Map's or Secret's data key (a drawer's save, an Edit tab's apply: [`editdatakeys.go`](../../internal/engine/editdatakeys.go)), an Argo CD sync with prune, `dock.reconnect` of a node shell |
| `AgentsAsk` | an agent's call waits for the user even where agent writes are allowed ([`agentsask.go`](../../internal/engine/agentsask.go)): the RBAC creates, Helm install and upgrade, RBAC text in an apply or create ([H-27](../security/privilege-footprint.md#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)), setting a proxy, Prometheus or Alertmanager ([agents and MCP](../security/agents-and-mcp.md)) |
| `Refuse` | refuses some parameters to an origin before anyone is asked (a Secret in `object.apply` from anywhere but the window, [D-19](decisions.md#d-19-editing-and-creating-secrets-from-yaml)) |
| `GUIOnly` | only the window runs it (running a program, adding a credential, showing a Secret's value, deciding what agents may do); other origins get a `GUIOnlyError`, MCP the code `guiOnly` ([`guionly.go`](../../internal/engine/guionly.go), `TestGUIOnlyCommandsRefuseOtherOrigins`; listed in [commands.md](commands.md)) |
| `Prefill`, `Dialog` | a generated dialog's start values, from the cache; per field a hint and a slider (`DialogField`) |
| `Effects`, `Warning`, `Review`, `Undoable` | the confirmation's rows beside its objects (`Effect`, [D-100](decisions.md#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save)), a warning shown apart, a text read before answering (never serialized), no "This cannot be undone." |
| `Run` | the work; reads from the cache; returns a JSON-serializable result |

`Register` refuses a spec that breaks these rules (an id's form, a write without `Targets`,
`Escalate` off a mutating command, `AgentsAsk` on a read or destructive one, `DrawerOnly` without
`Order`, a `Dialog` field outside the schema). Every command is an MCP tool (`ui.navigate` →
`ui_navigate`, §7); nothing reaches agents that the registry does not describe.

#### Dispatch

[`Session.Dispatch`](../../internal/engine/engine.go) takes every origin through the same steps:

1. Unknown ids are refused; a deep link runs only `DeepLinkCommands`, never a write
   ([`deeplink.go`](../../internal/engine/deeplink.go)).
2. Parameters are validated against the schema, unknown fields refused (`ParamsError`); a `GUIOnly`
   command refuses every origin but `gui` before `Enabled` can reveal anything.
3. References to several connected clusters are refused
   ([D-69](decisions.md#d-69-several-clusters-connected-at-once)).
4. `Enabled`: `ErrNotApplicable`, a write on a connection that does not answer
   ([D-58](decisions.md#d-58-standby-and-lost-connections)), an access-review denial, then the
   command's own reason; then `Refuse`.
5. `NeedsConfirmation` on the invocation's danger (after `Escalate`): destructive always; mutating
   for an agent without the context's allowance of agent writes or with `AgentsAsk`; UI for an agent
   with `AgentsAsk`. The `ConfirmRequest` names context, namespaces and objects (each by its kind as
   the user reads it, `Kinds`) and records their UIDs; after a yes the connection is checked again.
6. `Run` with the origin (`OriginOf`) and the confirmed request (`Confirmed`) in its context; a
   panic becomes an error. An agent's call is audited (a write also before it runs) and its result
   filtered (Secret values redacted).

#### What a confirmation binds

A write checks when it runs that it acts on what the user confirmed
([`confirm.go`](../../internal/engine/confirm.go)):

- **Context and objects:** `CheckConfirmedContext`, `CheckConfirmedTargets` (a create whose
  namespace came from the filter refuses when the filter moved meanwhile).
- **UIDs:** `ActUID` gives the write the UID confirmed (else the cache's). Delete and Evict send it
  as a precondition; Restart's template patch and every Scale carry `metadata.uid`, which the API
  server refuses on an object created again under the name (`cluster.ErrReplaced`, reported as
  `ErrTargetsChanged`; `TestKindPatchesBoundToTheUID`, `TestRestartAndScaleCarryTheUID`). Restart
  and Scale refuse first when the cache already holds another UID.
- **Text:** `object.apply` and `helm.release.upgrade` from a dock tab name its `textRevision`; an
  edit since refuses (`TestApplyFromTabBoundToTheReviewedText`,
  `TestHelmUpgradeTabBoundToTheReviewedText`).
- **Effects:** Helm install refuses when the chart's origin warning changed, upgrade also when its
  effects did; Remove finalizers removes exactly the confirmed ones (`ConfirmedFinalizers`,
  [D-98](decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)).

#### Access reviews

`Enabled` asks the backend's `Access` ([`access.go`](../../internal/engine/access.go)) for every
declared verb on each object of `ref` or `refs`, `*` resolved to the object's kind. A known denial
wraps `ErrNotPermitted` ("you may not patch deployments.apps in team-a"); unknown answers, local
kinds, `*` on a virtual kind and commands without `ref` pass, and the API server decides. The demo
cluster has no `Access`.

A real cluster's `Access` ([`accessreviews.go`](../../internal/engine/accessreviews.go)) never
blocks or sends a request from `Allowed`: a missing answer is unknown, and its review starts in the
background (deduplicated, at most four in flight, panic-safe). A namespace's
`SelfSubjectRulesReview` answers its verbs, the default namespace's the cluster-wide ones (every
namespace's rules carry the cluster-wide bindings); incomplete rules, a rule naming a cluster-wide
verb, a wildcard question and a failed rules review go to a `SelfSubjectAccessReview`, and after one
fails no more are sent. Answers (at most 512 namespaces, 2,048 questions) stay until
`cluster.refreshAccess` or a reconnect; nothing is polled. Connecting prefetches the rules of the
default, the accessible and the filter's namespaces. `kinds.list` marks a namespaced kind not
listable, which the sidebar hides, only when every namespace the user can see (up to 32 listed) was
reviewed completely without naming it (`TestBudgetAccessReviews`, `TestBudgetWithoutAccessReviews`).

#### Menus, dialogs and selections

`Registry.Actions(kind)` ([`menu.go`](../../internal/engine/menu.go)) is the drawer's toolbar and,
without `DrawerOnly` commands, the row menu: commands with an `Order` whose `Kinds` hold the kind or
are empty, by `Order`, then id. A `container` parameter opens a submenu of the pod's containers; a
schema requiring more than `ref` and `container`, or a `Prefill`, opens a dialog generated from
those fields ([`paramdialog.go`](../../internal/ui/fyneui/paramdialog.go)). `ObjectParams` fills
required fields with zero values so that `Enabled` and `Prefill` can be asked first; they read the
cache, so the UI calls them off its main goroutine.

A command with a required `refs` array and an `Order` acts on a selection
([`selection.go`](../../internal/engine/selection.go), `Registry.SelectionActions`; the commands in
[`commands/selection.go`](../../internal/engine/commands/selection.go) and
[`restartmany.go`](../../internal/engine/commands/restartmany.go)): `object.deleteMany`,
`workload.restartMany`, the five `flux.*Many`, `argocd.application.syncMany` and `refreshMany`, at
most `MaxSelection` (500) objects each. One confirmation names every object and records each UID;
`runMany` ([`manyjob.go`](../../internal/engine/commands/manyjob.go)) then acts on them one by one,
up to `DirectSelection` (10) within the call, more as a background job
([D-63](decisions.md#d-63-long-commands-as-background-jobs), §4.4). A failure stays that object's
and nothing is retried (`TestDeleteManyPartialFailure`).

#### The command palette

`Registry.Palette` ([`palette.go`](../../internal/engine/palette.go); the rules in its header)
builds the palette from registry metadata, so a command needs no palette code (the window's list:
[`fyneui/palette.go`](../../internal/ui/fyneui/palette.go)): the palette object's actions, the open
view's kind commands, commands with a `MenuPath` the window can run (no `ref` without an `Order`,
reads only where it presents the result, required parameters only where it has a dialog,
`PaletteInput.Runs`), and an entry per view and hotbar tile. An entry dispatches with origin `gui`
like its menu item. `Palette` asks every entry's `Enabled`: call it off the UI goroutine.

#### Keys

A main-menu item's shortcut takes its key from any focus (Fyne asks the main menu first; on macOS it
is the item's key equivalent). ⌘F (`ui.setFilter`) and ⌘⇧T (`terminal.open`) stay window shortcuts,
which a focused editor, terminal or text field keeps: their View menu items show the keys through a
`shownShortcut` no typed key matches, and on macOS the item asks AppKit whether a key is being
dispatched and then passes it on (`typeWindowShortcut`;
[`menukeys.go`](../../internal/ui/fyneui/menukeys.go), `TestWindowKeysInTheViewMenu`). ⌘F opens the
find bar of the dock's editor tab when the keys are in it
([D-106](decisions.md#d-106-a-click-below-the-rows-f-in-editor-tabs-moving-tabs-a-deleted-namespace)),
of the drawer's YAML ([D-127](decisions.md#d-127-find-in-the-drawers-yaml)) or a release's values
([D-130](decisions.md#d-130-find-in-a-releases-values)) when they are in the drawer or the table
(with the keys nowhere, the drawer's first), else the list's search
([`findkeys.go`](../../internal/ui/fyneui/findkeys.go) `claimFind`); a find bar shows through the
window's canvas, since Fyne's `Show` asks for no repaint. `MoreKeys` ride on separators of a menu of
their own (`moreKeysMenu`), which Fyne's key lookup runs and no menu bar draws
(`TestZoomMenuAndKeys`). The keys per platform: [usage › Zoom](usage.md#zoom).

#### The window's zoom

The zoom is Fyne's canvas scale ([D-134](decisions.md#d-134-the-windows-zoom)). `applyZoom`
([`fyneui/zoom.go`](../../internal/ui/fyneui/zoom.go)) sets `FYNE_SCALE` in Cogpit's own environment
to the start's scale times the factor of `Preferences.ZoomLevel` (−4 to 4, ×1.2 per level,
[`zoom.go`](../../internal/engine/zoom.go)), applies the theme again so that Fyne reloads every
window's scale, and resizes the window back to its size on the screen. `fyneui.Run` sets the level
before the window exists. `zoomFitLayout` gives the session the largest level whose content fits the
window (`SetZoomCeiling`); `ui.zoom.in` refuses beyond it (`ErrZoomFit`). The local terminal's shell
gets `FYNE_SCALE` as Cogpit started with it ([`local.go`](../../internal/terminal/local.go)
`baseEnv`).

### 4.3 UI state

`UIState` ([`uistate.go`](../../internal/engine/uistate.go)) is the window's state, one per session
(one window; several windows, [D-52](decisions.md#d-52-several-windows), are not built). The GUI
changes it only through commands, so the user's change and an agent's are the same event; agents
read it with `ui.state` (MCP `ui_state`), which is how the built-in agent knows what "this pod" is.
`View`, `Namespaces`, `Groups`, `Filter`, `FilterOptions`, `States`, `Sort`, `Selected` and `Drawer`
are the active tab's live copy ([tabs](#tabs-and-the-workspace-file)). `Welcome`, `User` (D-68),
`ClusterVersion` (D-91), `Alerts` (D-77) and `IntegrationsQuestion` (D-76) are only derived;
`ConnectFailure` is written by a failed connect run and cleared by `syncConnectFailure`.

**Every change goes through `Session.UpdateUI(fn)`**
([`engine.go`](../../internal/engine/engine.go)): under `Session.mu` it applies `fn` and derives, in
this order, `syncConnections`, `syncStates`, `syncTabs`, `syncConnectFailure`, `syncWelcome`,
`syncNavScope`, `syncNavigator`, `syncIntegrationsQuestion`, `syncDrawerChartVersion`, `SyncDock`,
`syncUser`, `syncAlerts`, `syncClusterVersion`; then it bumps `Revision`, releases the lock, lets
the view workers reconcile (`state.uiChanged`, which skips an older revision) and emits
`EventUIState`. Rules:

- A derived field gets a `sync*` step there, never a setter. The order matters: `syncStates` ends a
  status filter with its view before `syncTabs` saves the tab; `syncWelcome` reads what `syncTabs`
  left.
- The sync steps run under `Session.mu`: what they read besides the UI state has a leaf lock of its
  own (the alert book, the user book, a `sync.Map` of tab titles), never `state.mu`.
- Lock order: `state.mu` before `Session.mu` ([`views.go`](../../internal/engine/views.go)).
- A view's snapshot depends on `viewParams` (`paramsOf`): a new field that changes a view's rows
  goes there, or the view does not rebuild.

#### D-69: Several clusters connected

`Context` and `Connection` are the shown cluster; `Connections`
([`connections.go`](../../internal/engine/connections.go)) lists every connected cluster with its
`Connection`, `Shown`, `Cold` ([§6](#d-69-the-pool-of-backends)) and `LoginLapsed`, which every dot
of the cluster in the window draws ([`clustermarks.go`](../../internal/ui/fyneui/clustermarks.go)).
Each cluster's connection watch writes its own state, bound to its connect by a generation
(`UIState.connGen`, `ConnectedCluster.gen`), so an old watch never overwrites a newer connect.

- **Connect runs** ([`connect_run.go`](../../internal/engine/connect_run.go),
  [D-97](decisions.md#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays)):
  each connect is a `connectRun` in `state.runs`. `Session.Connect` starts or joins one and waits
  under `state.connMu`; `ConnectBackground` starts one nobody waits for (generations from
  `bgGenBase`). A later connect or show ends only the wait (`errConnectSuperseded`); the run then
  joins the pool as a cluster the window left, and a toast says how it ended.
- **The shown cluster never changes by itself:** a failed connect leaves `Context` on its cluster in
  `ConnectError`, its stages in `ConnectFailure`
  ([`connectfailure.go`](../../internal/engine/connectfailure.go), D-114).

**A command's cluster** ([`target.go`](../../internal/engine/target.go)) is its `engine.OnCluster`
`context` parameter (commands without an object: `resources.list`, `metrics.get`, ...), else its
dock tab's (`engine.DockTargeted`), else its references', else the shown cluster. `Dispatch` checks
`writeBlocked`, the access reviews (`permitted`), the allowance of agent writes (D-2) and the
confirmation's context against it, and refuses references into two clusters (`oneCluster`). A
context not connected is an `engine.NotConnectedError` (`ErrNotConnected`); nothing connects
implicitly.

**The dock per cluster** ([`dockscope.go`](../../internal/engine/dockscope.go)): the dock shows the
shown cluster's tabs and those of no cluster (`UIState.DockInScope`); hidden tabs stay in `Dock`
with their streams running, so `dock.read` finds them. `ActiveDock` and `DockOpen` are the shown
cluster's, the others' kept in `dockMemo`. `SyncDock`
([`layout.go`](../../internal/engine/layout.go)) closes a dock without tabs (`ErrNoDockTabs`) and
opens it for a tab added or activated (`ShowDockTab` for one already open); `dock.activate` of a
hidden cluster's tab shows that cluster first. The window keeps the dock's height (`dockLayout`,
[`panelwidths.go`](../../internal/ui/fyneui/panelwidths.go), not stored) and the keyboard focus
([`dockfocus.go`](../../internal/ui/fyneui/dockfocus.go),
[D-115](decisions.md#d-115-a-dock-tab-the-user-opens-takes-the-keys)): only a tab the window's own
command answered with (`OpenedTab()`), clicked or restored from a draft takes the keys; an agent's
never does.

#### Selections, columns and filters

`ui.select` ([`selection.go`](../../internal/engine/selection.go)) takes a `mode`: `replace`
(default; moves the drawer unless `noDrawer`), `add`, `toggle`, `remove`, or `open` (one object:
`Drawer` moves, `Selected` stays, a plain click; `TestSelectOpenKeepsTheCheckedRows`); `all` takes
the rows the view shows after all its filters. Each view keeps its selection
([`selectionmemory.go`](../../internal/engine/selectionmemory.go)): its tab remembers it by cluster
and view (`ViewTab.selections`, not in `ui.state`) until the tab closes or the cluster goes
(`forgetSelections`); `DropGoneSelected` removes objects the cache no longer holds when it shows
again, waiting at most 2 s per kind (`TestRememberedSelectionDropsGoneObjects`).

In the window a checked row has the fill and the drawer's row a bar at its left edge (`layoutLead`
in [`list_rows.go`](../../internal/ui/fyneui/list_rows.go),
[D-124](decisions.md#d-124-checked-rows-the-drawers-row-and-the-keyboards-cell)); ↵ and Space open a
row through `ui.select` as a click does ([`tablekeys.go`](../../internal/ui/fyneui/tablekeys.go)); a
click below the rows sends `ui.closeDrawer` and leaves `Selected`
([`tableclick.go`](../../internal/ui/fyneui/tableclick.go),
[D-106](decisions.md#d-106-a-click-below-the-rows-f-in-editor-tabs-moving-tabs-a-deleted-namespace)).
The widgets' traps: [§4.4](#44-tables-details-editors-and-streams).

**Columns:** `HiddenColumns` and `ColumnOrder` are keyed by view, the same for every cluster. A
`TableSnapshot` ([`table.go`](../../internal/engine/table.go)) keeps the kind's `Columns` and
indexes with `Hidden` set and the user's `Order` as indexes; the table, its column list
([`columnorder.go`](../../internal/ui/fyneui/columnorder.go)) and `WriteCSV` follow
`DisplayOrder()`. `DrawerSort` ([`drawersort.go`](../../internal/engine/drawersort.go)) orders
sortable drawer tables by section title, and every drawer model comes sorted, so `object.get` reads
rows as shown.

**Search:** `FilterOptions` (`matchCase`, `regex`) belong to the tab. One `RowMatcher`
([`filteroptions.go`](../../internal/engine/filteroptions.go)) serves the view snapshots and the
Overviews' tables: terms, or one RE2 pattern of at most 1,000 bytes matched per cell; a pattern that
does not compile filters nothing and sets `FilterError`.

**Status filter** ([`statusfilter.go`](../../internal/engine/statusfilter.go),
[`model/states.go`](../../internal/model/states.go)): a row's states are its kind's state columns'
texts (`KindProfile.States`, else `model.DefaultStateColumns`) or its status token's name, kept with
the row (`rowEntry.states`), so a snapshot counts (`StateChoices`) and matches without building.
`syncStates` clears `States` whenever the view changes; `ui.navigate` clears a filter that hides its
object (`MatchesStates`); `resources.list` applies the same rule (`FilterRowsByStates`).

#### Pages

A view key with a slash is a page (`engine.IsPage`,
[`overview.go`](../../internal/engine/overview.go)): `workloads/overview`, `cluster/overview`
([`clusteroverview.go`](../../internal/engine/clusteroverview.go)), `flux/dashboard`
([`fluxdashboard.go`](../../internal/engine/fluxdashboard.go),
[D-34](decisions.md#d-34-the-flux-dashboard)), `argocd/dashboard`
([`argocd_dashboard.go`](../../internal/engine/argocd_dashboard.go)), `alerts/board`. An open page
runs its own worker in place of a view worker (`state.page`, `state.cluster`, `state.flux`,
`state.argo`), with cache handles on the kinds it reads. Its table's rows keep their kind keys, so
selection, row menus and the drawer work (`engine.MixedKinds` where kinds mix), and
`Session.ViewProfile` gives `ui.sort`, `ui.setColumns` and `table.exportCsv` its profile.

#### Hotbar

`UIState.Hotbar` ([`engine/hotbar.go`](../../internal/engine/hotbar.go), the commands in
[`commands/hotbar.go`](../../internal/engine/commands/hotbar.go),
[D-83](decisions.md#d-83-one-hotbar-and-a-drag-that-carries-the-tile)) is one list of context ids,
nil in a headless session (`ErrNoHotbar`); `Session.HotbarView` (`hotbar.list`) derives what a tile
shows. `Session.ChangeHotbar` applies a pure `HotbarState` operation (its `Check*` twins are the
commands' `Enabled`), writes `hotbar.json`, then shows it; a format-1 file is merged on load and
kept as `hotbar.json.v1`. F1–F12 reach the window, the table and every `searchEntry`
([`searchkeys.go`](../../internal/ui/fyneui/searchkeys.go)), which hand them to the hotbar
(`TestSearchFieldsHandFKeysToTheHotbar`). Drags of tiles and tabs
([`hotbardrag.go`](../../internal/ui/fyneui/hotbardrag.go),
[`stripdrag.go`](../../internal/ui/fyneui/stripdrag.go)) stay in the window until the drop, which
dispatches one command (`hotbar.moveCluster`, `ui.tab.move`, `dock.move`). The window's hotbar:
[`fyneui/hotbar.go`](../../internal/ui/fyneui/hotbar.go).

#### Preferences, cluster settings and start at login

Cogpit's files ([usage › Naming conventions](usage.md#naming-conventions)) share one store pattern
([`preferences.go`](../../internal/engine/preferences.go),
[`clustersettings.go`](../../internal/engine/clustersettings.go), hotbar, workspace): read once;
written whole by `writePrivateFile` ([`privatefile.go`](../../internal/engine/privatefile.go):
temporary file, sync, rename; 0600 in a 0700 directory, the profile's ACLs on Windows; symlinks
refused); an unreadable file moved to `<file>.unreadable` before the first write; a field out of
bounds dropped alone. `ChangePreferences`, `ChangeClusterSettings` and `ChangeHotbar` check, write,
then show through `UpdateUI`; the commands are in
[`commands/preferences.go`](../../internal/engine/commands/preferences.go) and
[`commands/clustersettings.go`](../../internal/engine/commands/clustersettings.go). Compare
`ClusterSettings` with `Equal` (`TestClusterSettingsEqualComparesEveryField`).

- The UI state carries proxies redacted (`RedactProxyURL`, `MergeProxyPassword`;
  [credentials](../security/credentials.md)).
- Cluster settings with files of their own: the accessible namespaces
  ([`accessiblens.go`](../../internal/engine/accessiblens.go): DNS-1123 labels, at most 100;
  [§5](#scopes-local-and-virtual-kinds)); the allowance of agent writes
  ([`allowance.go`](../../internal/engine/allowance.go),
  [`clustersettings_agents.go`](../../internal/ui/fyneui/clustersettings_agents.go)); the accent
  colour ([`accentcolor.go`](../../internal/engine/accentcolor.go),
  [D-80](decisions.md#d-80-a-clusters-accent-colour),
  [`clustersettings_accent.go`](../../internal/ui/fyneui/clustersettings_accent.go)) and its light
  strip ([`engine/lightstrip.go`](../../internal/engine/lightstrip.go), refused without an accent
  colour, `ErrLightStripNoAccent`;
  [`clustersettings_lightstrip.go`](../../internal/ui/fyneui/clustersettings_lightstrip.go)); the
  cluster's identity marks, its display name and icon
  ([D-119](decisions.md#d-119-the-cluster-icon-designer),
  [`engine/clusteridentity.go`](../../internal/engine/clusteridentity.go),
  [`fyneui/clusteridentity.go`](../../internal/ui/fyneui/clusteridentity.go)).
- Settings that run a program, read a file, mark a cluster's identity
  ([H-125](../security/agents-and-mcp.md#h-125-the-accent-colour-shows-only-in-a-clusters-view))
  or allow agent writes are refused to every origin but the GUI, a field through its command's
  `Refuse` (`Err*GUIOnly`), a command through `Spec.GUIOnly`
  ([`guionly.go`](../../internal/engine/guionly.go)); the full rules, the one-way and the asking
  ones included: [agents and MCP › Rules](../security/agents-and-mcp.md#rules).
- The stored allowance of agent writes counts only in a session with a confirmer
  (`agentWritesLocked`), so a headless engine over the same file honours none.
- The window applies `UIState.Preferences` whoever changed it
  ([`fyneui/preferences.go`](../../internal/ui/fyneui/preferences.go); the pages in
  [`preferences_page.go`](../../internal/ui/fyneui/preferences_page.go) and
  [`clustersettings.go`](../../internal/ui/fyneui/clustersettings.go)); the settings page is an
  overlay card holding the window's keys but Escape
  ([`settingsoverlay.go`](../../internal/ui/fyneui/settingsoverlay.go), D-118).
- Start at login is the operating system's state
  ([`loginitem.go`](../../internal/engine/loginitem.go), the commands in
  [`commands/loginitem.go`](../../internal/engine/commands/loginitem.go), the window's switch in
  [`preferences_loginitem.go`](../../internal/ui/fyneui/preferences_loginitem.go)):
  `UIState.LoginItem` is what macOS's `SMAppService` last reported
  ([`loginitem_darwin.go`](../../cmd/cogpit/loginitem_darwin.go)), nil on Windows and Linux.

#### Tabs and the workspace file

`Tabs` ([`tabs.go`](../../internal/engine/tabs.go), the commands in
[`commands/tabs.go`](../../internal/engine/commands/tabs.go)) are `ViewTab`s: a fixed cluster, a
view, its own filters, sort, selection and drawer, and a history of `TabPlace`s (at most 50 each
way, D-129; the top bar's arrows send `ui.tab.back` and `ui.tab.forward`,
[`titlehistory.go`](../../internal/ui/fyneui/titlehistory.go)). `syncTabs` keeps the active tab and
the live fields in step, so commands that know nothing of tabs work unchanged, and pushes the place
a view change left onto the history (`MarkStep` for `ui.navigate` within a view). Each cluster has
its own preview tab (D-117); `ui.openView` is `OpenTab` in preview. `Place` (not in `ui.state`) keys
the window's scroll positions. The bar shows `UIState.TabsShown`
([`navscope.go`](../../internal/engine/navscope.go)): tabs of live clusters only, the shown
cluster's unless `mixedEnvironments` is on or none is shown; the tab keys count those. A tab's title
names its cluster by `engine.ClusterName` (the display name, else the context's name after the last
"@"; [`displayname.go`](../../internal/engine/displayname.go),
[D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)). A move
(`ui.tab.move`, `ui.tab.moveLeft`, `ui.tab.moveRight`;
[`tabmovekeys.go`](../../internal/ui/fyneui/tabmovekeys.go),
[D-106](decisions.md#d-106-a-click-below-the-rows-f-in-editor-tabs-moving-tabs-a-deleted-namespace))
places the tab among the tabs the bar shows, the other clusters' keeping their places
([`tabmove.go`](../../internal/engine/tabmove.go)); `dock.move` does the same in the dock.

**Leaving a cluster clears its filters:** namespaces, API groups, status filter, search and its
toggles never cross to the next cluster, which brings its own tab's or none (`UIState.leaveCluster`
in [`tabs.go`](../../internal/engine/tabs.go)).

**`workspace.json`** ([`persist.go`](../../internal/engine/persist.go),
[D-41](decisions.md#d-41-what-survives-a-restart)) follows `EventUIState` and is written 1 s after
changes stop, at least every 5 s, and at `Session.Close`; never a selection, the drawer or a dock
tab; of two processes the last write wins. `restoreWorkspace`
([`persist_restore.go`](../../internal/engine/persist_restore.go)) restores the layout, and with
`Preferences.RestoreSession` the tabs, the last shown tab's cluster `Disconnected`, asking no
cluster (`TestWorkspaceNoRequestAtStart`); tabs of contexts certainly gone are dropped
(`goneContexts` reads the kubeconfig sources, never a cluster). `Session.RestoreLastSession`
([`restore.go`](../../internal/engine/restore.go)) then connects the cluster shown at quit through
`Connect` and the others through `ConnectBackground`
([`connect_background.go`](../../internal/engine/connect_background.go)), each the user's own
connect with plugin approvals (D-26).

#### Welcome page and error page

`syncWelcome` ([`welcome.go`](../../internal/engine/welcome.go),
[D-42](decisions.md#d-42-the-start-screen)) shows the welcome page without a live or connecting
cluster and over a live one without an active tab, never for a chosen cluster's failed connect,
whose error page shows from `ConnectFailure` (its stages drawn by
[`connectstages.go`](../../internal/ui/fyneui/connectstages.go), which Connection Updates uses too).
`ui.welcome.open` ([`commands/welcome.go`](../../internal/engine/commands/welcome.go)) clears
`ActiveTab` (the tab keeps its state) and leaves a failed cluster `Disconnected`. Both pages cover
the workspace, not the dock ([`welcome.go`](../../internal/ui/fyneui/welcome.go),
[`errorpage.go`](../../internal/ui/fyneui/errorpage.go)); a click on a cluster's row
([`clusterrow.go`](../../internal/ui/fyneui/clusterrow.go)) dispatches `cluster.connect`.
Documentation and About are window actions outside the registry, as the status bar's link is
([D-23](decisions.md#d-23-the-status-bars-owner-link-and-mcp-switch)): no agent opens a browser. On
macOS About is GLFW's application-menu item, tagged below 5000 so Fyne's menu rebuilds keep it
([`appmenu_darwin.m`](../../internal/ui/fyneui/appmenu_darwin.m)); no main-menu item may be labelled
exactly "About".

#### Navigator and palette

- **Scope** ([`navscope.go`](../../internal/engine/navscope.go), the commands in
  [`commands/navscope.go`](../../internal/engine/commands/navscope.go)): `section:local` (the one
  list that chooses a cluster, [D-57](decisions.md#d-57-choosing-a-cluster); Lens's other sections
  are not targets, [D-5](decisions.md#d-5-extensions),
  [D-9](decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) or `cluster:<context id>`;
  `syncNavScope` drills into a cluster when its connect starts, whoever started it. The window draws
  the section ([`navigator_sections.go`](../../internal/ui/fyneui/navigator_sections.go)) under the
  navigator's header ([`navigator_header.go`](../../internal/ui/fyneui/navigator_header.go)).
- **State** ([`navigator.go`](../../internal/engine/navigator.go),
  [D-51](decisions.md#d-51-the-navigators-and-the-command-palettes-state)): `Navigator.Search`,
  `Row` and `Collapsed`, set by `ui.setNavigatorSearch`, `ui.setNavigatorRow` and
  `ui.setNavigatorGroup`. The window reports its changes after 150 ms (`navReport`,
  [`navigatorstate.go`](../../internal/ui/fyneui/navigatorstate.go)) and ignores the engine's echo
  while a report is on its way (`TestNavigatorTypingWinsOverAnEcho`); an agent's value never takes
  the focus. Open tree groups and the focus stay the window's.
- **Palette text** ([`palettetext.go`](../../internal/engine/palettetext.go)): on a text another
  origin typed (`PaletteTextOrigin`), ↵ runs only a read or a UI entry
  (`TestCommandPaletteAgentTextNeverRunsAChange`).
- **Choosing a cluster** ([`findcluster.go`](../../internal/ui/fyneui/findcluster.go)) dispatches
  `settings.close`, `ui.toggleNavigator` and `ui.setNavigatorScope` as needed, then opens the search
  once the window shows the answer (the engine emits its state before a dispatch returns).
- **The namespace filter's list** ([`nsfilter.go`](../../internal/ui/fyneui/nsfilter.go)) reads
  every namespace from the cache at each opening, 1,000 a page, noting the terminating ones
  ([`nsgone.go`](../../internal/ui/fyneui/nsgone.go),
  [D-106](decisions.md#d-106-a-click-below-the-rows-f-in-editor-tabs-moving-tabs-a-deleted-namespace)),
  and on `EventNamespaces` ([`nswatch.go`](../../internal/engine/nswatch.go)) while the filter names
  namespaces: reads on the user's action or an event, not polls.
- The tree is Cogpit's own rows (`navTree`,
  [`navigator.go`](../../internal/ui/fyneui/navigator.go)), since `widget.Tree` keeps hover and
  keyboard highlight private; level changes slide unless Reduce motion is on
  ([`navigator_slide.go`](../../internal/ui/fyneui/navigator_slide.go), D-107).

#### Layout, panels and window-only state

- **Layout toggles** ([`layout.go`](../../internal/engine/layout.go), the commands in
  [`commands/layout.go`](../../internal/engine/commands/layout.go), the buttons in
  [`layoutbar.go`](../../internal/ui/fyneui/layoutbar.go); D-54) are engine state: `NavigatorHidden`
  (`ui.toggleNavigator`), `DockOpen` (`ui.toggleDock`, which on a dock without a tab opens a
  terminal tab as `terminal.open` does,
  [D-94](decisions.md#d-94-the-empty-docks-toggle-opens-a-terminal-and-alert-names-copy)), `Chat`
  (`chat.toggle`).
- **Panel widths** ([`panelwidths.go`](../../internal/ui/fyneui/panelwidths.go)) are Fyne
  preferences, never engine state: navigator 160–480 points (default 216), drawer 320–1000 (default
  600), chat 300–760 (default 400); the table and the workspace keep 480.
- **The drawer's width never follows its content:** a panel's root is a `lockedWidth`, and drawer
  widgets build to `drawer.contentWidth` (`TestDrawerWidthNeverFollowsItsContent`).
- **The light strip** ([`lightstrip.go`](../../internal/ui/fyneui/lightstrip.go), its maths in
  [`lightstrip_model.go`](../../internal/ui/fyneui/lightstrip_model.go),
  [D-122](decisions.md#d-122-the-light-strip)) adds no state: after every `applyUIState`, `follow`
  reads the active tab's cluster's `AccentColor` and `LightStrip`; its ticker (at most 60 fps) runs
  only while the light moves; static in the background, under Reduce motion, or on battery with
  `lightStripMainsOnly` (IOKit on macOS, a 30 s poll elsewhere,
  [`power.go`](../../internal/ui/fyneui/power.go),
  [`preferences_power.go`](../../internal/ui/fyneui/preferences_power.go); Reduce motion through
  [`reducemotion_darwin.go`](../../internal/ui/fyneui/reducemotion_darwin.go)).
- **Toasts** last 6, 9 or 14 s (info, warning, error), held while hovered or selected; their text is
  a `selParagraph` ([`selparagraph.go`](../../internal/ui/fyneui/selparagraph.go)) and their copy
  icon copies the whole message ([`toastcopy.go`](../../internal/ui/fyneui/toastcopy.go),
  [D-99](decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)).

#### Notifications and alerts

The notification log ([`notifications.go`](../../internal/engine/notifications.go); the bell's list:
[`fyneui/notifications.go`](../../internal/ui/fyneui/notifications.go)) keeps the newest 100 toasts
in memory; `Session.emit` records an `EventToast` before subscribers see it, the window adds its own
(`RecordWindowToast`). A change emits `EventUIState` directly, not through `UpdateUI`: toasts come
from view workers and recover handlers that must not wait for the views. Marking read and clearing
are GUI-only. A toast quoting what the cluster returned to a Helm action must go through
`EmitWindowToast`, which other origins read without its inline values and with cached Secret values
replaced
([H-31](../security/privilege-footprint.md#h-31-an-agents-helm-values-can-copy-what-lookup-reads)).
`syncAlerts` ([`alerts.go`](../../internal/engine/alerts.go), D-77) derives `UIState.Alerts` from
the poller's `alertBook` ([§9 › Alertmanager](#d-77-alertmanager-internalalerting)); only an answer
that changes the counts calls `UpdateUI`.

#### D-58: The connection's states

`Connection.State` is `disconnected`, `connecting`, `connected`, `error`, `interrupted` or
`loginExpired`. `Live()` holds for the last three: backend, cache, tabs, views, drawer and dock stay
(`TestUIStateStaysAcrossAnInterruption`); only `connected` lets a write start. `Connection` also
carries `since`, `nextAttempt`, `reason` (never a credential), `needsApproval`, `upgraded` and
`waitingForLogin` (`program`, `since`, `until`, or `runsOn` for a plugin run Cogpit could not end,
H-118; [`loginwait.go`](../../internal/engine/loginwait.go), D-73). Disconnect of a connect under
way cancels it with `errConnectCancelled`; the cluster layer ends the plugin's run.

**The connection watch** ([`connwatch.go`](../../internal/engine/connwatch.go)) is one goroutine per
backend of a real cluster, made by the connect run, stopped with the backend; it alone writes its
cluster's state. It asks through `HealthProber` (`*cluster.Conn`;
[§6](#6-cluster-connections-internalcluster)) with `ConnWatchOptions`: a heartbeat (`GET /version`)
every 15 s with 5 s to answer, a second check 2 s after a failure, 30 s for a credential plugin,
attempts after 1, 2, 4, 8, 16 s, then every 30 s. Wake and network signals (`NoteWake`,
`NoteNetworkChange`; macOS only, [Platforms](#platforms)) kick every watch. A version read twice in
a row becomes `Backend.ServerVersion`; another minor sets `Connection.Upgraded` until a reconnect
([`connwatch_version.go`](../../internal/engine/connwatch_version.go), D-132). A cluster not shown
whose `ClusterSettings.HoldBackgroundLogin` is set (Cluster Settings' "Renew the login while the
cluster is not shown" off) holds its plugin (`applyLoginHolds`,
[`connwatch_lapse.go`](../../internal/engine/connwatch_lapse.go)); its lapsed login shows as
`connected` with `LoginLapsed`.

| From | Trigger | Check | To |
|---|---|---|---|
| connected | heartbeat tick; wake; network change; `cache.OnStreamError` ([`streamerr.go`](../../internal/cache/streamerr.go)) | `Heartbeat`; not back after 6 s: waits for its plugin, at most 30 s | two failures in a row (one at wake): **interrupted**; two 401s in a row or a plugin silent 30 s: **loginExpired**; at wake, a plugin whose program changed (D-26): **loginExpired** without running it |
| interrupted | backoff; wake or network change at once | `Reachable` (no credentials), then `Heartbeat` | **connected**; **loginExpired**; next attempt |
| loginExpired | `cluster.login` (GUI-only) | one `Heartbeat` as long as the user takes | **connected**; **interrupted**; **loginExpired** |
| a plugin run under way | its 5-minute bound (`cluster.LoginEndedError`) | none: the waiting heartbeat's answer stands whenever it comes (`pluginRunEnded`) | **loginExpired** with the bound's reason |
| plugin held | a check that needed it (`cluster.ErrLoginLapsed`) | none | **lapsed**: requests held or refused, heartbeat paused |
| lapsed | hold ends (shown, setting on); `cluster.login` | one `Heartbeat` | **connected**; **interrupted**; **loginExpired** |

While a cluster is not `connected`
([`connwatch_streams.go`](../../internal/engine/connwatch_streams.go); the user's view:
[usage](usage.md#when-the-connection-breaks)):

- `writeBlocked` refuses its mutating and destructive commands in `enabled` and again in `Dispatch`
  after a confirmation; it never blocks writes to another cluster.
- Views keep the cache's rows without an informer error (`shownErr`); agents' reads carry
  `StaleNoteFor`.
- Shells, attach and node shells end (`trackSession`); `dock.reconnect` opens a new session.
- Followed logs wait and reopen from their last line
  ([`logsresume.go`](../../internal/engine/logsresume.go)); port forwards dial once connected.
- Helm actions (`OperationContext`, `ErrConnectionLost`) and drains (`errJobInterrupted`, D-63) stop
  and stay in their journals ([D-11](decisions.md#d-11-crash-stance)). The drafts banner and the
  node shell sweep do not count an interruption's end as a connect.

The window draws the states: every dot of the cluster
([`conndot.go`](../../internal/ui/fyneui/conndot.go), blinking while it connects), the banner over
the view with Reconnect or Log in again ([`connbanner.go`](../../internal/ui/fyneui/connbanner.go))
and a shell tab's Reconnect row ([`dockreconnect.go`](../../internal/ui/fyneui/dockreconnect.go)).

### 4.4 Tables, details, editors and streams

- **Tables:** `TableSnapshot` ([`table.go`](../../internal/engine/table.go)) holds a view's columns
  (in the kind's order; `DisplayOrder` applies the user's), rows, sort, filters, `Total` and
  `Revision`. A `model.Cell` ([`row.go`](../../internal/model/row.go)) carries text, a sort number
  and a status token the toolkit maps to a theme colour. Columns of type `age`, `until` and `time`
  hold a Unix time written when drawn (`model.CellText`), so rows never go stale.
- **Links** (`followLink`, [`links.go`](../../internal/ui/fyneui/links.go)): a link in a list's
  Namespace column runs `ui.setNamespace`, one without a name `ui.openView`, any other
  `ui.navigate`.
- **`view.rows`** ([`viewrows.go`](../../internal/engine/viewrows.go),
  [`commands/viewrows.go`](../../internal/engine/commands/viewrows.go)): a page of the open view's
  snapshot as the table draws it, after waiting up to 2 s for the snapshot to follow the UI state
  (`TableSnapshot.CaughtUp`); 100 rows by default, at most 500. No API request.

**Fyne traps in lists and tables.** Table tests run the GL driver's minimum-size pass (`glPass` in
[`tablereuse_test.go`](../../internal/ui/fyneui/tablereuse_test.go)), not `settle`, which lays out
every widget and hides them.

- Fyne gives a reused cell new content without a layout while its size is unchanged. `tableCell.set`
  ([`tableview.go`](../../internal/ui/fyneui/tableview.go)) lays out every cell whose content
  changed, so nothing `Layout` places (container squares, a usage bar) stays from the previous
  content; the drawer's header does the same after loading (`drawer.layoutHeader` in
  [`drawer.go`](../../internal/ui/fyneui/drawer.go); `TestDrawerHeaderAfterAnotherPod` in
  [`drawerheader_test.go`](../../internal/ui/fyneui/drawerheader_test.go)).
- The table and its scroller keep separate offsets: set both with the table's `ScrollToOffset`. A
  snapshot with fewer rows clamps the offset (`keepScrollInRows`), a view entered starts at its top
  (`scrollToTop`), and each tab place keeps its offset in the window
  ([`tabscroll.go`](../../internal/ui/fyneui/tabscroll.go), `ViewTab.Place`,
  [D-129](decisions.md#d-129-back-brings-the-place-back)). Tests:
  [`tablescroll_test.go`](../../internal/ui/fyneui/tablescroll_test.go).
- A click takes ⌘ and ⇧ from its own mouse press (`tableCell.MouseDown`, `clickModifiers` in
  [`tablecheck.go`](../../internal/ui/fyneui/tablecheck.go)), never from the driver's
  `CurrentKeyModifiers`, which Fyne updates only on key events and so can report a key let go
  outside the window as held. macOS reports ⌘ in a mouse event as Control; `clickModifiers` maps it
  back. Tests: `TestClickTakesItsOwnKeys`, `TestClickModifiers`
  ([`tableclick_test.go`](../../internal/ui/fyneui/tableclick_test.go)).
- The resource table, the tab bar and the navigator are no `fyne.DoubleTappable` (Fyne would hold
  every tap for the double-click interval); they count double clicks themselves (`doubleClickTime`,
  500 ms, [`tabbar.go`](../../internal/ui/fyneui/tabbar.go); `TestListClicksAreNotHeld`).
- Every `fyne.Draggable` keeps a `dragGuard`
  ([`dragguard.go`](../../internal/ui/fyneui/dragguard.go), D-123): a drag counts only where the
  button went down.
- Every text field hands the wheel on through a `wheelPass`
  ([`wheelpass.go`](../../internal/ui/fyneui/wheelpass.go), D-126) to a scroller made by
  `newVScroll`.

**Details.** `DetailModel` ([`detail.go`](../../internal/engine/detail.go); also `object_get`'s
result, so its JSON shape only grows) is built by `BuildDetailIn`
([`engine/drawer.go`](../../internal/engine/drawer.go)) in Lens's order: Metrics (pods, nodes;
`Session.ObjectMetrics`), Properties (metadata without UID, then the profile's own "Properties"
section), the kind's sections, the integrations' sections, then events (newest 50). A virtual kind's
drawer is its profile's sections alone.

- Items ([`model/detail.go`](../../internal/model/detail.go)): `value` is always plain text; `link`,
  `chips`, `fold` and `actions` add to it. A `model.CellTable` adds typed cells beside the plain
  `rows` agents read; a sortable table keeps 500 rows and counts the rest (`LimitDrawerTables`,
  [`drawerlimit.go`](../../internal/engine/drawerlimit.go)).
- A `Private` item (Helm's user-supplied values) reads `model.Withheld` for agents
  (`model.WithholdPrivate`).
- An `ItemAction` is a command with the item's parameters ("Forward…" on a container port): agents
  dispatch it from `object_get`, the GUI runs it as the row menu does or through a dialog registered
  in `itemActionDialogs` ([`itemactions.go`](../../internal/ui/fyneui/itemactions.go)). The GUI may
  also draw a whole section itself (`drawerSections`, by kind and section title).
- Config Map values ([D-55](decisions.md#d-55-a-config-maps-values-edited-in-the-drawer),
  [`configmapdata.go`](../../internal/engine/configmapdata.go); the editors in
  [`fyneui/configmapdata.go`](../../internal/ui/fyneui/configmapdata.go), each key on a line of its
  own, [`drawerdata.go`](../../internal/ui/fyneui/drawerdata.go), long names wrapped by
  [`wrapname.go`](../../internal/ui/fyneui/wrapname.go)): `configMap.reviewData` diffs against the
  cache and asks for a server dry run; `configMap.saveData` writes the cached object with the values
  set through the `ObjectWriter`, the base resourceVersion as precondition. A newer version is a
  `DataConflictError`; a review rebases onto it when it changed none of the edited keys. Keys added
  or removed go in the same write (`CheckDataKey`;
  [`datakeys.go`](../../internal/engine/datakeys.go),
  [`fyneui/datakeys.go`](../../internal/ui/fyneui/datakeys.go)); a removal makes the save
  destructive. Unsaved values are kept by `configMap.keepEdits` and as drafts
  ([`drafts_drawer.go`](../../internal/engine/drafts_drawer.go)).
- Secret values ([D-59](decisions.md#d-59-a-secrets-values-edited-in-the-drawer),
  [`secretdataedit.go`](../../internal/engine/secretdataedit.go),
  [`fyneui/secretdata.go`](../../internal/ui/fyneui/secretdata.go)): `secret.reveal`,
  `secret.reviewData` (masked diff) and `secret.saveData` are GUI-only. Values stay in the window's
  memory, never in a draft, and are copied only through `shell.copyText` as a Secret (cleared after
  60 s; per platform: [Platforms](#platforms)); a masked value is copied whole without being shown
  (`secret.reveal` with `whole`, [`secretcopy.go`](../../internal/ui/fyneui/secretcopy.go)).
- Copying is the window's, no command ([D-25](decisions.md#d-25-copying-values),
  `TestCopyHasNoCommand`): the drawer's texts are selectable and one copy icon sits on the value
  under the pointer ([`selection.go`](../../internal/ui/fyneui/selection.go),
  [`copy.go`](../../internal/ui/fyneui/copy.go)), a list's cells likewise
  ([`tablecopy.go`](../../internal/ui/fyneui/tablecopy.go)).
- Every button the shell makes is a `textButton`
  ([`textbutton.go`](../../internal/ui/fyneui/textbutton.go),
  [D-104](decisions.md#d-104-dialog-fields-hints-dropdowns-and-button-outlines)).

**Dock tabs.** Editors, logs, terminals, chats and jobs are engine sessions, so agents open, read
and close them too.

- **Background jobs** ([`jobs.go`](../../internal/engine/jobs.go),
  [D-63](decisions.md#d-63-long-commands-as-background-jobs)): `Session.StartJob` returns at once
  with a `job` tab ([`jobtab.go`](../../internal/ui/fyneui/jobtab.go)) whose steps, counts, state
  and outcome are engine state (`DockTab.Job`); the job runs under `goSafe`. It ends with its work,
  on `job.cancel`, when its tab closes, when its cluster is interrupted, its login lapses or it
  disconnects (`followJobs`; showing another cluster ends none), or with the session. Jobs keep no
  journal: each command documents why a crash midway is harmless
  ([D-11](decisions.md#d-11-crash-stance)). `JobSpec.Parent` passes the command's origin and
  confirmation, not its cancellation. Jobs run Flux's Reconcile with Source
  ([D-33](decisions.md#d-33-the-flux-actions)), every action on more than `DirectSelection` (10)
  objects (`runMany`), and `node.drain`, journaled before its job starts
  ([`drainjob.go`](../../internal/engine/commands/drainjob.go)).
- **Drafts** ([`drafts.go`](../../internal/engine/drafts.go)): an Edit or Create tab's unsaved text
  is written owner-only to `<user config dir>/Cogpit/drafts/<id>.json` 2 s after its last change (at
  least every 10 s); `wmu` orders writes, removals and restores. A draft names its process
  (`JournalOwner`, H-22); at connect, drafts no open tab and no live Cogpit owns are offered
  (`EventDrafts`; the window's banner, [`fyneui/drafts.go`](../../internal/ui/fyneui/drafts.go),
  lists them with `editor.drafts`, which never returns their text). `editor.restoreDraft`
  ([`drafts_restore.go`](../../internal/engine/drafts_restore.go),
  [`editor.go`](../../internal/engine/editor.go)) bases a restored Edit tab on the draft's
  resourceVersion, so that an apply over a newer one meets a conflict. Secrets and Helm values tabs
  keep none (`textMayHoldSecret`, [D-19](decisions.md#d-19-editing-and-creating-secrets-from-yaml)),
  nor does `cogpit-cli`.
- **Editors:** an Edit or Create tab's text is engine state (`editor.get`, `editor.setText`); the
  undo and redo history lives in the widget only and a text set from outside clears it
  ([`yamlview/history.go`](../../internal/ui/fyneui/yamlview/history.go),
  [D-84](decisions.md#d-84-undo-and-redo-in-the-yaml-editor)); the find bar
  ([`findbar.go`](../../internal/ui/fyneui/findbar.go)) edits through the view (`SelectRange`,
  `ReplaceSelection`), each replacement one undo step.
- **Revert** ([`editrevert.go`](../../internal/engine/editrevert.go), the window's button in
  [`editortab_revert.go`](../../internal/ui/fyneui/editortab_revert.go)): `editor.revert` sets an
  Edit tab's text and base to the cached object; `editor.undoRevert` undoes it, one step deep.
- **Logs** ([`logs.go`](../../internal/engine/logs.go); the window's tab in
  [`logtab.go`](../../internal/ui/fyneui/logtab.go)): options and source are engine state on the tab
  (`logs.setOptions`, `logs.setSource`; `logs.sources` offers pods and containers from the cache,
  [`logsources.go`](../../internal/engine/logsources.go)); every stream asks for timestamps, the GUI
  hides them unless shown. `workload.logs`
  ([`workloadlogs.go`](../../internal/engine/workloadlogs.go)) merges one stream per running
  container of the resource's pods from the cache (lines start `<pod>/<container>`, at most 40
  containers). A follow stream the server ends while the container runs reopens from `sinceTime`
  (backoff 0.5 s to 8 s, at most 5 times without a new line) and follows the container by name,
  adopting its next instance. Group logs (`application.logs`, `argocd.logs`, `flux.logs`;
  [`grouplogs.go`](../../internal/engine/grouplogs.go)) run one such stream per workload the cached
  group object names ([`workloadsof.go`](../../internal/model/kinds/workloadsof.go)), sharing the
  bounds, at most 20 workloads.
- **Older log lines** ([`logsolder.go`](../../internal/engine/logsolder.go)): the lines live in the
  GUI's view (at most 10,000), so `logs.loadOlder` and `logs.jumpTo` record a request on the tab
  that the window carries out through `Session.OlderLogs`
  ([`logtab_older.go`](../../internal/ui/fyneui/logtab_older.go),
  [`logtab_jump.go`](../../internal/ui/fyneui/logtab_jump.go)); the engine parses the jump's time
  (`ParseLogTimeIn`, [`logsjump.go`](../../internal/engine/logsjump.go)). The API has no "before"
  cursor: the engine asks each tail again with 1,000 more lines than held (at most 8 requests, 32
  MiB) and keeps the older ones. Agents never receive lines. A view's line numbers go below zero
  once lines are prepended: never use -1 as "none" (`TestLogsEvictPrependedLines`). Text selection
  is the window's, not engine state
  ([`logselect.go`](../../internal/ui/fyneui/termview/logselect.go),
  [D-99](decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)).

**Absolute times** ([D-31](decisions.md#d-31-every-timestamp-in-the-local-time-zone-and-format),
[`timefmt.go`](../../internal/model/timefmt.go)): one formatter, `model.TimeFormat`. The binaries
read the computer's layout once (`locale.System`, `locale.Environment`); `Session.TimeFormat` puts
it in the Preferences' zone. A kind's `Detail` has no clock, so it writes a time as a mark
(`model.MarkTime`) that `BuildDetailIn` replaces. Texts drawn for the user (the window, `view_rows`,
`object_get`'s sections, the CSV) use the user's format; structured times stay RFC 3339
(`TestAgentsReadTimesAsDrawn`).

**Related objects** ([`model/related.go`](../../internal/model/related.go),
[`engine/related.go`](../../internal/engine/related.go)):

- A cell or drawer section that shows objects of another kind reads an index over that kind's cached
  objects (`model.RelatedIndex`, declared in
  [`kinds/related.go`](../../internal/model/kinds/related.go): `PodsByNode`, `PodsByClaim`, ...),
  never a list of its own. A profile names its indexes in `Needs` (rows and drawer) or `DetailNeeds`
  (drawer only: the workloads' Pods tables, `PodsByOwner`, `ReplicaSetsByOwner`, `JobsByOwner`,
  [`workloadpods.go`](../../internal/model/kinds/workloadpods.go)) and reads them with
  `rel.Objects(index, key)` in `CellsWith` and `DetailWith`.
- An index is built on its first lookup and kept current by its kind's events. A view wakes on every
  change of an indexed kind (its row cache keys on the related generation); a drawer only for the
  keys it looked up (`TestWorkloadDrawerWakesOnlyForItsPods`).
- A lookup on a kind still listing returns nothing: a rule that warns about a missing object checks
  `rel.Listed(index)` first. Rows built outside a view use `Session.BuildRows`.
- References to custom resources link through `CustomKindsByGroupKind` (`kinds.RefToWith`).
- `object.get` with `related: true` ([`relatedrefs.go`](../../internal/engine/relatedrefs.go)) lists
  what the drawer links to and what a workload controls, at most 200 (`TestObjectGetRelated`).

### 4.5 Kind keys

A kind key names a kind in views, object references, `ui.*` commands and MCP tools.

- **Built-in kinds:** the bare plural (`pods`). **Custom resource kinds:** `<plural>.<group>`
  (`certificates.cert-manager.io`), since plurals are not unique across groups. A CRD's group always
  contains a dot, so the two never collide.
- **Virtual and local kinds:** Cogpit's own bare plural (`helmreleases`, `argocdrepositories`),
  apiVersion `cogpit.io/v1`, never a built-in plural; `KindKeyFor` skips them.
- **No version in the key:** the profile's preferred version; a built-in profile lists older ones of
  the same shape as `FallbackVersions` (`servedProfiles` in
  [`connector.go`](../../internal/engine/connector.go)).
- **References carry a Kind:** `model.BuiltinKindKey`
  ([`kindkey.go`](../../internal/model/kindkey.go)) maps every built-in view's `group/Kind`
  (`TestReferencesLinkToEveryBuiltinKind`); custom kinds link through `kinds.RefToWith`.
- **Curated custom kinds** (`kinds.Curated()`: Vertical Pod Autoscalers, the thirteen Flux kinds,
  Argo CD's, the policy reports) keep the `<plural>.<group>` key; a family appends to `curated` from
  its own file's `init`
  ([`verticalpodautoscalers.go`](../../internal/model/kinds/verticalpodautoscalers.go),
  [`flux.go`](../../internal/model/kinds/flux.go),
  [`argocd.go`](../../internal/model/kinds/argocd.go)), and `kinds.ProfileFor` takes the curated
  profile while scope, Kind and version match. `kinds.FluxKinds` lists the Flux kinds with the
  actions each takes ([D-33](decisions.md#d-33-the-flux-actions)), `kinds.FluxStateOf` reads an
  object's state ([D-34](decisions.md#d-34-the-flux-dashboard)).
- **Argo CD's Repositories and Clusters** are virtual kinds over the Secrets Argo CD labels
  ([`argocd_settings.go`](../../internal/model/kinds/argocd_settings.go),
  [D-35](decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show)), present while
  `applications.argoproj.io` and Secrets are served (`addArgoCompanions` in
  [`engine/argocd.go`](../../internal/engine/argocd.go)); their `Derive` shows allowlisted keys
  only.

**Litmus test:** `cogpit-cli` and MCP can do everything the GUI does, except draw it.

### 4.6 Deep links (`cogpit://`)

A link is the least trusted input: any web page or app can open one
([D-47](decisions.md#d-47-deep-links-cogpit), [`deeplink.go`](../../internal/engine/deeplink.go)).

- **Delivery, macOS only:** the bundle's `CFBundleURLTypes` (the [`Makefile`](../../Makefile)'s
  `app`) registers the scheme; the Apple event handler
  ([`deeplink_darwin.m`](../../cmd/cogpit/deeplink_darwin.m)) only queues the link (`DeepLinkQueue`
  in [`deeplink_queue.go`](../../internal/engine/deeplink_queue.go): never blocks, at most 8
  waiting), and the window ([`fyneui/deeplink.go`](../../internal/ui/fyneui/deeplink.go)) hands each
  to `Session.OpenDeepLink`.
- **Parsing:** `ParseDeepLink` accepts only `cogpit://open?context=&view=&namespace=&name=`
  ([usage](usage.md#deep-links-cogpit)).
- **Navigation:** dispatches with `OriginDeepLink`: `settings.close`; `cluster.connect` after asking
  the user (`askLinkConnect`) unless the context is the shown, connected one; then `ui.navigate`, or
  `ui.setNamespace` and `ui.openView`. One toast and one audit line (`deeplink.open`).
- **The walls:** `Dispatch` refuses a link any command outside `DeepLinkCommands`, or one that is
  not a UI or read command, may escalate, is GUI-only or declares a verb beyond get, list and watch
  (`checkDeepLinkCommand`), and any invocation whose danger after escalation is above read
  (`checkDeepLinkDanger`). A `Refuse` that admits only `OriginGUI` treats a link as not the user.

### 4.7 Integrations (D-76, D-82)

An integration is a tool a cluster runs that Cogpit finds and reads: the Metrics Server and
Prometheus ([§9](#9-metrics-internalmetrics)), Alertmanager, the Trivy Operator, Kyverno. Cogpit
installs none
([privilege footprint](../security/privilege-footprint.md#nothing-installed-for-cogpits-own-features),
[D-61](decisions.md#d-61-cogpit-metrics-the-bundled-metrics-stack);
[`integrations.go`](../../internal/engine/integrations.go),
[D-76](decisions.md#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect),
[D-82](decisions.md#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)).

- **The switch:** `ClusterSettings.IntegrationsOff` per cluster (nil until the user chooses),
  `Preferences.IntegrationsOff` for new clusters (all on). `(*UIState).IntegrationOn` is the one
  switch every integration reads; off means no request and nothing in the window or `ui_state`.
- **Consent before looking**
  ([`integrationsconsent.go`](../../internal/engine/integrationsconsent.go)): in the window's
  session (`UIState.IntegrationsAskFirst`) an undecided cluster reads nothing but what its question
  ticks (`UIState.IntegrationsLookFor`); the CLI and the demo cluster use the default.
- **A probe and a follower per integration**, registered from its own file: the probe
  (`integrationProbes`) reports `found`, `missing`, `forbidden` and the like from what Cogpit
  already holds, sending nothing; the follower (`integrationFollowers`) runs on every UI state
  change for every connected cluster, starts or stops the reads, and must not block.
- **The question** ([`integrationsask.go`](../../internal/engine/integrationsask.go)): `UpdateUI`
  derives `UIState.IntegrationsQuestion` for the shown, connected, undecided, non-demo cluster not
  put off this run; `cluster.integrations.step`, `.lookFor`, `.answer`, `.later` and `.ask` drive
  it.
- **Only the user switches**
  ([`integrationsguionly.go`](../../internal/engine/integrationsguionly.go),
  [agents and MCP](../security/agents-and-mcp.md),
  [H-126](../security/agents-and-mcp.md#integrations-and-a-clusters-identity),
  [H-133](../security/agents-and-mcp.md#integrations-and-a-clusters-identity)):
  the question's commands but `.later` are GUI-only, and `cluster.settings.set` and
  `preferences.set` refuse any switch from another origin (`RefuseIntegrationSwitches`).
- **The window** reads the statuses with `cluster.settings.get`, again on `EventIntegrations`;
  Cluster Settings has a section per integration
  ([`clustersettings_integrations.go`](../../internal/ui/fyneui/clustersettings_integrations.go),
  listed by [`clustersettings_nav.go`](../../internal/ui/fyneui/clustersettings_nav.go)), whose
  details its own file adds through `integrationDetails`
  ([`clustersettings_integration.go`](../../internal/ui/fyneui/clustersettings_integration.go),
  [D-87](decisions.md#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer));
  the question's dialog is [`integrationsask.go`](../../internal/ui/fyneui/integrationsask.go), the
  default for new clusters
  [`preferences_integrations.go`](../../internal/ui/fyneui/preferences_integrations.go).

#### D-79: The integration overlay

An integration that marks objects (Kyverno's policy violations,
[D-79](decisions.md#d-79-kyvernos-policy-violations-on-the-objects),
[`kyverno.go`](../../internal/engine/kyverno.go); Trivy's image grades,
[D-78](decisions.md#d-78-trivy-operators-image-ratings),
[D-113](decisions.md#d-113-image-grades-shields-from-trivys-reports),
[`trivy.go`](../../internal/engine/trivy.go), their shields drawn by
[`gradebadge.go`](../../internal/ui/fyneui/gradebadge.go)) adds cells and drawer sections without a
kind profile knowing it ([`overlay.go`](../../internal/engine/overlay.go)). Its follower puts a
`rowOverlay` on the backend while the integration is on (`overlays.put`) and takes it off, ending
its reads, when it is off. An overlay reads only its own cached data and reports changes
(`overlays.changed`), which wakes the open view and drawer.

- A cell in a column the kind has is merged (`mergeCell`: texts joined, the worse status); a column
  the kind lacks is appended, so row indexes hold, and shown after `overlayColumn.After`.
- The row cache keeps an overlaid copy per row; search, sort, the CSV, `view.rows` and
  `resources.list` read it.
- An overlay's sections follow the kind's; a `sectionAmender` adds to the kind's own. An agent's
  read waits for the overlays' reads (`overlays.waitSynced`, at most 15 s).
- Cogpit's own overlay (`overlays.putOwn`) is the Nodes list's version check
  ([`k8sversion.go`](../../internal/engine/k8sversion.go),
  [D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)); it reads nothing of
  the cluster.

## 5. Cache

Cluster state flows one way; nothing in the UI waits for the API server:

```text
 API server ──list + watch──► cache: one informer per (cluster, scope)
                                 │  transform: drop managedFields (Secrets: also last-applied)
                                 ▼
                         engine snapshots (coalesced, ≤ 10 per second per view)
                                 ▼
                    UI views · detail drawer · MCP tools · built-in agent
```

Informers run only in [`internal/cache`](../../internal/cache/cache.go), and every object read
(views, drawer, editor, `ui.state`, `resources.find`, agents) is served from it.
`Cache.Acquire(scope)` returns a `Handle`; the first one starts a client-go shared informer over
`cache.DynamicSource` (the connection's dynamic client, or its metadata client for a metadata-only
scope), unless a cover serves the scope (below). After the last `Release` the informer stays warm
for `DefaultWarmPeriod` (5 minutes, not configurable), then stops. A failing list or watch is a
state: client-go retries, and `Handle.Err` reports it.

| Mechanism | Effect | Where |
|---|---|---|
| No resync, watch bookmarks, relist only on `410 Gone` | Only watch events after the initial list | informer options, client-go's reflector |
| Initial lists | Each read lists once (`resourceVersion=0`), then watches from that version | `cache.Options.Lists` |
| Streamed initial lists, opt-in beta ([D-135](decisions.md#d-135-a-stalled-stream-lists)) | `ClusterSettings.Streaming` streams initial lists over the watch (WatchList) from the next connect. A stream silent for `DefaultStreamStall` (10 s) before its bookmark is ended, and the connection lists every read from then on | [`stall.go`](../../internal/cache/stall.go), [`initiallists.go`](../../internal/engine/initiallists.go) |
| Per-namespace informers | Only for a user who may not list a kind cluster-wide (below); otherwise a namespace filter filters the one cluster-wide watch | `Cache.AcquireNamespaces` |
| Metadata-only scopes | Kinds that show only metadata transfer only metadata | `Scope.MetadataOnly` |
| Discovery on disk | No discovery request when connecting again (below) | [`discocache.go`](../../internal/cluster/discocache.go) |
| Access reviews cached ([§4.2](#access-reviews)) | Until `cluster.refreshAccess` or the next connect; never polled | [`accessreviews.go`](../../internal/engine/accessreviews.go) |
| Rate limits per connection | Other requests 5/s, burst 10 (`cluster.DefaultQPS`, `DefaultBurst`); the cache's lists 50/300; discovery 50/300; access reviews 10/20; each its own bucket. Watches are never throttled | [`cluster.go`](../../internal/cluster/cluster.go), [`access.go`](../../internal/cluster/access.go) |

A view's snapshot is rebuilt when its kind or a kind of its profile's `Needs` changes, at most once
per `SnapshotInterval` (100 ms). A drawer's `DetailNeeds` are held only while that drawer is open
([§4.4](#44-tables-details-editors-and-streams)). The transform also drops the
`last-applied-configuration` annotation from core Secrets, since it holds their values in plain text
(`TestSecretsLoseTheLastAppliedAnnotation`). The cache lives in memory only, so no stale state shows
after a restart and no cluster data, Secrets included, lands on disk
([D-13](decisions.md#d-13-cache)).

**Diagnostics** ([`diagnostics_view.go`](../../internal/engine/diagnostics_view.go)).
`diagnostics.get` and the Connection Diagnostics tab (`diagnostics.open`,
[`fyneui/diagnostics.go`](../../internal/ui/fyneui/diagnostics.go)) read counters and send nothing:
requests by verb and resource ([`counter.go`](../../internal/cluster/counter.go)); each informer as
open, warm or covered, with its load time; objects and estimated memory per kind
(`Cache.StatsWithMemory`, [`size.go`](../../internal/cache/size.go): the heap layout of each object,
memoized by UID and resourceVersion, at most 1,000 objects walked per scope and read); a sum per
connected cluster. The `TestBudget*` tests pin request counts at the HTTP level (budgets P9–P11 in
[testing](testing.md#performance-budgets)).

**Connection Diagnostics** ([D-133](decisions.md#d-133-connection-diagnostics)). Each context has an
in-memory log ([`diaglog.go`](../../internal/cluster/diaglog.go): at most 500 lines, a repeated
problem folded into one line with a count); the engine keeps the logs of the 8 contexts used last
([`diaglogs.go`](../../internal/engine/diaglogs.go)). Its sources only observe: a round-tripper
above the request timeout and the gates
([`diagtransport.go`](../../internal/cluster/diagtransport.go): 401, 403, 429, 5xx, no answer, a
broken request, a request that is not a stream and took 5 s or more; a Secret's error body is never
read); a `measuredLimiter` per token bucket (waits of 1 s or more, named by the read through
`Cache.LabelRequests`); and the cache's `OnDiagnostic` and `OnStall`
([`cache/diag.go`](../../internal/cache/diag.go): fallbacks, stalls, loads of 5 s or more).
`diagnostics.report` ([`diagnostics_report.go`](../../internal/engine/diagnostics_report.go))
replaces hosts, addresses and file paths before its text leaves Cogpit
([`diagscrub.go`](../../internal/engine/diagscrub.go)).

**Discovery on disk** ([D-65](decisions.md#d-65-discovery-cached-on-disk)). A layer under
client-go's memory cache reads `<user cache dir>/Cogpit/discovery/<hash>.json`, or asks the server
and writes the answer back owner-only. The hash covers the server URL, TLS server name, kubeconfig
files, cluster and user entry names and impersonation, never a credential. An answer counts only for
the server version `GET /version` reports: 6 h once the definitions watch confirmed it
(`Conn.DefinitionsSeen`), else 10 minutes; one checked against other definitions is dropped from
memory and disk.

**A Terminating namespace's drawer**
([D-98](decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers),
[`nsblockers.go`](../../internal/engine/nsblockers.go)) reads each kind its
`NamespaceContentRemaining` condition names through a metadata-only scope in that namespace, never
the kind's cluster-wide view scope, and only once the namespace's rules review allows it or cannot
tell (`TestBudgetTerminatingNamespaceBlockers`).

### Scopes, local and virtual kinds

A `cache.Scope` is a GVR, a namespace (empty: all), a label selector (normalised by `Acquire`), a
metadata-only flag and a trim; each distinct scope is one informer and one watch unless a cover
serves it. Every `Source` honours the whole scope; a metadata-only object is a
`PartialObjectMetadata` as `Unstructured` (`cache.MetadataObject`). A kind's profile decides its
scope, and the reads of a kind (view, drawer, `Lookup`, agents) go through
`Backend.Acquire(profile)` and `KindList`/`KindGet`
([`kindscope.go`](../../internal/engine/kindscope.go)), so they share one informer; a scope built by
hand is a separate read on purpose (the trims, a Terminating namespace's kinds). `resources.find`
without a kind searches virtual kinds, custom kinds and kinds holding Secret values
(`HoldsSecretValues`) only when already cached (`Backend.Cached`): a search starts no watch of every
Secret.

**Secrets** ([`secretdata.go`](../../internal/engine/secretdata.go),
[H-8](../security/privilege-footprint.md#h-8-secret-data-arrives-with-the-list)). The Secrets view
watches every Secret in full; the drawer reads its Secret from the cache, lists keys with masked
values and sizes, and zeroes what it decodes. `secret.reveal` serves one value of the Secret the
drawer shows, to the GUI origin only, bound to the `resourceVersion` the drawer listed. Agents read
every result redacted (`TestAgentsReadSecretsRedacted`), except the built-in agent on a connection
the user marked private (`readLikeUser`,
[H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)).
Certificates in Secrets ([D-30](decisions.md#d-30-certificates-in-secrets-decoded)) are parsed by
[`model/certs.go`](../../internal/model/certs.go), which decodes only `CERTIFICATE` blocks; the one
private-key parse is `DecodedValue.MatchPrivateKey`
([`keymatch.go`](../../internal/model/keymatch.go)). The drawer's certificate groups come from
[`secretcerts.go`](../../internal/engine/secretcerts.go), the checks (a paired key matches, the
chain in order, `ChainInOrder`) from
[`secretcerts_checks.go`](../../internal/engine/secretcerts_checks.go); nothing parsed is cached.

**Peeking** ([`peek.go`](../../internal/cache/peek.go)). `Cache.Peek` and `PeekVersion` read the
running scopes without a handle: nothing starts, nothing is held, no warm period is extended. The
matcher that keeps cached Secret values out of agents' results reads through it
([§7](#7-mcp-and-the-built-in-agent),
[H-82](../security/agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents),
[`agentsecrets.go`](../../internal/engine/agentsecrets.go)).

**Covering scopes** ([`cover.go`](../../internal/cache/cover.go), [D-13](decisions.md#d-13-cache)).
While the unselected scope of the same resource, namespace and flags runs, a label-selected scope
reads a filtered view of its informer and starts no watch. A covered scope never keeps its cover
alive: when the cover's warm period ends with covered handles open, the selected scope starts its
own informer, and the cover runs until that one has synced, at most `HandoverTimeout` (1 minute). A
handover sends subscribers the difference as events; with comparable resourceVersions (KEP-5504) no
event repeats or goes back in version (`TestCoverHandoversUnderChurn`). Helm's releases use it: with
the Secrets view open they cost no watch of their own (`TestBudgetSecretsAndReleasesShareOneWatch`).

- **Local kinds** (`KindProfile.Local`, group `cogpit.io`: port forwards, Helm charts): a
  `cache.Memory` per backend, written through `Backend.PutLocal` and `DeleteLocal`; no API server.
- **Virtual kinds** (`KindProfile.Derive`): derived from cached objects on every snapshot; a derived
  object carries a UID and a resourceVersion that changes with its content (the row cache and the
  confirmation's UID binding rely on it). Helm's releases join (`cache.Join`,
  [`join.go`](../../internal/cache/join.go)) the Secrets and the ConfigMaps labelled `owner=helm`
  ([`helmdriver.go`](../../internal/engine/helmdriver.go)); a joined handle costs exactly its parts.
  The Applications ([`applications.go`](../../internal/engine/applications.go)) group workloads by
  `app.kubernetes.io/instance` (`kinds.GroupApplications`,
  [`kinds/applications.go`](../../internal/model/kinds/applications.go)) in a cache of their own
  over `cache.Held`, which holds the workload kinds only while its informer runs.
- `object.edit` and `object.delete` do not apply to either (`KindProfile.APIObject`); `kinds.list`
  marks them `local` or `virtual`, and their views have no Create button.

**Custom resource kinds** ([`customkinds.go`](../../internal/engine/customkinds.go)). One handle on
`customresourcedefinitions` per connected cluster; its events, coalesced, rebuild the profiles
(`kinds.ParseCRD`, or a curated one) into an immutable set behind an atomic pointer, and a change
emits `EventKinds`. A refused list ends the watch until `cluster.refreshAccess` or the next connect;
meanwhile each rebuild asks API discovery and gives every curated kind the cluster serves
(`kinds.Curated`: Flux, Argo CD, Vertical Pod Autoscalers) its profile
([`customkinds_discovery.go`](../../internal/engine/customkinds_discovery.go)). Generic kinds stay
hidden, and so do Flux's HelmCharts, which have no curated profile. The namespaces watch
([`nswatch.go`](../../internal/engine/nswatch.go)) ends at a refusal the same way; it runs only
while the namespace filter names namespaces, and emits `EventNamespaces`.

**Namespaced watches** ([`nsscope.go`](../../internal/cache/nsscope.go),
[`accessiblens_plan.go`](../../internal/engine/accessiblens_plan.go)). A user who may not list a
namespaced kind cluster-wide reads it per namespace (`Cache.AcquireNamespaces`: one scope each,
merged into one handle that syncs once every namespace listed or was refused) in the accessible
namespaces (`ClusterSettings.AccessibleNamespaces`, kept in Cluster Settings › Namespaces,
[`clustersettings_namespaces.go`](../../internal/ui/fyneui/clustersettings_namespaces.go), and
offered by the namespace filter,
[`clustersettings_namespaces_filter.go`](../../internal/ui/fyneui/clustersettings_namespaces_filter.go))
and the filter's, else the context's namespace; denied namespaces are named in
`TableSnapshot.Denied`. Whether the user may comes from answers held already: a 403 a cluster-wide
read got, an access review, the context namespace's rules review. Until it is decided, views and
pages hold `cache.Pending` (nothing listed; a page through `acquireForPage`,
[`accessiblens_pages.go`](../../internal/engine/accessiblens_pages.go)), the GUI drawer and `Lookup`
hold nothing (`acquireNow`), and reads that wait anyway wait up to 10 s (`acquireSettled`); a read
of one object, its logs or a port forward then takes the target's namespace alone (`acquireIn`).
`DetailWait` reads an object's events through its namespace's events informer, shared with the
Events view, because `cache.Scope` selects by label only (`detailReads`). A refused scope is not
kept warm.

**A refusal counts, whoever met it**
([`accessiblens_follow.go`](../../internal/engine/accessiblens_follow.go)). `Cache.OnRefused`
reports every refused list or watch before the handle's error shows; the session keeps a
cluster-wide refusal and replans what is open, and reads that wait retake the kind per namespace at
once (`settleReads`). `acquirePlanned` holds `nsState.planMu`, so a plan made before a refusal never
sends the refused read again (`TestBudgetAccessibleNamespaces`,
`TestTargetReadsFollowARefusedClusterWideList`, `TestKindNamespaceLimitedUser`).

### D-79: Trimmed scopes

A scope may name a trim (`Scope.Trim`, `cache.RegisterTrim`,
[`trim.go`](../../internal/cache/trim.go)) that the transform applies before the store:
`kyverno-violations` keeps a policy report's violations, `trivy-summary` drops a vulnerability
report's list. A trimmed scope is its own informer beside the kind's whole objects, so views, the
editor and agents never see a trimmed object; a trim never touches metadata (`TestTrimmedScope`).

## 6. Cluster connections (`internal/cluster`)

- One `cluster.Conn` ([`cluster.go`](../../internal/cluster/cluster.go)) per open kubeconfig
  context, several at once (D-69, below): a `rest.Config` from `clientcmd` and one HTTP client (one
  connection pool) shared by its typed, dynamic, metadata and discovery clients. The engine's
  `Backend` adds the cache (§5) and the access reviews.
- **Auth** is client-go's: client certificates, tokens, exec credential plugins. Only client-go's
  `exec` auth plugin is linked, so a legacy `auth-provider` (oidc, azure, gcp) fails with "no Auth
  Provider found for name"; `memoryPersister` never writes the kubeconfig
  ([H-4](../security/credentials.md#h-4-auth-provider-token-refresh)). Credentials go to https
  servers only (`TestPlainHTTPServerGetsNoCredential`).
- **Trust** comes from the kubeconfig as clientcmd reads it
  ([`tls.go`](../../internal/cluster/tls.go)); a failed certificate check names the kubeconfig field
  to check. A connection without the check (`Conn.Insecure`) gets a WARN line in the connect log and
  `Connection.TLSUnchecked` in the UI state, shown in the status bar
  ([`tlswarning.go`](../../internal/engine/tlswarning.go),
  [`bars.go`](../../internal/ui/fyneui/bars.go),
  [H-37](../security/trust-boundaries.md#h-37-a-context-that-skips-the-certificate-check)).
- **Proxy**: the kubeconfig's `proxy-url`, or the cluster's proxy in Cogpit's settings
  (`Options.Proxy`, [`proxy.go`](../../internal/cluster/proxy.go)), which replaces it and
  `HTTPS_PROXY` for every client of the connection, the Helm SDK and the exec, attach and
  port-forward dialers included; the local terminal's kubectl does not use it. `Connect`'s errors
  carry no proxy password ([`proxyurl.go`](../../internal/cluster/proxyurl.go)). Tests go through a
  forward proxy that tunnels CONNECT and checks Basic credentials
  ([`testkit/proxy.go`](../../internal/testkit/proxy.go)).
- **Streams**: logs (`StreamLogs`) and port forwards (`DialPortForward`, listening on `127.0.0.1`
  only); exec and attach are in `internal/terminal` (§10).
- **Health** ([`health.go`](../../internal/cluster/health.go),
  [D-58](decisions.md#d-58-standby-and-lost-connections)): a 30 s timeout on every request that is
  not a stream (`ErrNoAnswer`; a write that may have landed ends as an `OutcomeUnknownError`); a
  gate that, while the engine marks the connection interrupted or its login expired
  (`Conn.SetHealth`), holds lists and watches and refuses every other request; a login guard that
  hands only the first 401 after a working request to the credential plugin; the login hold
  (`Conn.HoldLogin`, [`loginhold.go`](../../internal/cluster/loginhold.go)), which wraps the exec
  Authenticator's `interactiveFunc` so the plugin of a cluster that is not shown does not start
  ([D-69](decisions.md#d-69-several-clusters-connected-at-once)); `ResetConnections`, which closes
  in-use connections too. Probes: `Heartbeat` with credentials, `Reachable` without (never runs a
  plugin). Tests freeze the fake API server with `testkit.FreezableProxy`.
- **A login that waits** ([`loginrun.go`](../../internal/cluster/loginrun.go),
  [D-73](decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound)): client-go runs a
  plugin without a context, under a lock every request of that plugin waits for. Cogpit notes each
  run and ends it after `DefaultLoginTimeout` (5 minutes) or on a cancel by signalling the plugin's
  process, found among its own children at the run's begin
  ([`pluginproc.go`](../../internal/cluster/pluginproc.go); `proc_info` on macOS, `/proc` on Linux,
  a Toolhelp snapshot on Windows:
  [`pluginproc_darwin.go`](../../internal/cluster/pluginproc_darwin.go),
  [`pluginproc_linux.go`](../../internal/cluster/pluginproc_linux.go),
  [`pluginproc_windows.go`](../../internal/cluster/pluginproc_windows.go)); after that it starts the
  plugin again only on a new connect or Log in again
  ([H-118](../security/credentials.md#h-118-what-a-cancelled-login-leaves-running)).
- **The logged-in user** ([`whoami.go`](../../internal/cluster/whoami.go),
  [D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)): one
  `SelfSubjectReview` at every connect and reconnect, bounded by 5 s, keeping username and groups
  only; on failure the kubeconfig user entry's name, unconfirmed. It never fails a connect. The
  engine asks before a connection is published (`Session.learnUser`) and again after every reconnect
  (`renewUser`; [`engine/whoami.go`](../../internal/engine/whoami.go)), keeps a confirmed username
  in `cluster-users.json` ([`whoami_store.go`](../../internal/engine/whoami_store.go)), and the
  navigator's root shows it ([`clusteruser.go`](../../internal/ui/fyneui/clusteruser.go)).
- **API server warnings** ([`warnings.go`](../../internal/cluster/warnings.go)) are logged once per
  distinct text and connection; at most 64 are kept (`Conn.Warnings`).

### D-69: The pool of backends

`state.pool` ([`pool.go`](../../internal/engine/pool.go)) holds one `Backend` per connected context;
`state.shown` is the one the window shows (the window side: [§4.3](#43-ui-state)).
`Session.Backend()` returns the shown backend, `BackendOf(context)` any connected one,
`TargetBackend(context)` the same or a `NotConnectedError`: no command connects a cluster by itself.

- `Session.Connect` ([`views.go`](../../internal/engine/views.go)) of a pooled context only shows it
  from its cache (`showLocked`), unless its login needs a new connection (D-26); any other context
  connects while the shown one stays pooled. The left cluster's views stop when the connect starts
  (`leaveLocked`), so `UIState.Context` always names the shown cluster. Connect runs:
  [§4.3](#d-69-several-clusters-connected).
- Leaving a cluster releases only the window's view handles; its connection watch, discovery, access
  reviews, port forwards, dock streams and jobs go on. A timer at the end of its informers' warm
  period (`Cache.WarmUntil`, `cache.DefaultWarmPeriod`: 5 minutes; `armColdLocked`) marks it `Cold`;
  shown again before then, it sends no request (`TestSwitchingBackWithinTheWarmPeriodListsNothing`,
  `TestSwitchingClustersOnARealStack`).
- **Disconnect** (`cluster.disconnect`, `Session.DisconnectContext`,
  [`disconnect.go`](../../internal/engine/disconnect.go)) sends nothing: the cluster's informers
  stop, its port forwards, logs, shells and jobs end, its tab-bar tabs close and its dock tabs stay
  with their streams ended. For the shown cluster or a connect under way (cancelled at once) it
  waits for `connMu` and leaves `Disconnected` with `Context` still naming the cluster; for another
  only its `Connections` entry goes. The connection log notes it; a connect started after it wins
  (`TestDisconnect`).
- Per cluster: the connection watch (D-58), `writeBlocked`, `OperationContext`, the access reviews,
  the agent-write allowance and the journals. The agent filter scrubs the cached Secrets of every
  connected cluster.
- `ConnectBackground` pools a cluster without showing it (the session restore): its connection
  watch, discovery, access reviews and login hold start ([§4.3](#d-58-the-connections-states)), its
  views list nothing until it is shown.

### D-81: Reconnect from the kubeconfig file

A connection keeps the credentials it read at its connect (client-go re-reads only a certificate and
key that are both files, and a token file). `cluster.Connect` keeps an HMAC fingerprint of the
context's login ([`loginfile.go`](../../internal/cluster/loginfile.go)), and `Conn.LoginInFile`
tells whether the file now holds another. `Session.Reconnect`
([`reconnect.go`](../../internal/engine/reconnect.go), `cluster.reconnect`) builds the new
connection while the old backend stays pooled, then swaps them (`swapBackend`); a failure leaves the
old one. The tabs stay: log streams reopen, shell tabs are marked `Interrupted` and `Reconnected`,
port forwards move to the new backend. A running job or Helm action refuses a reconnect.
`cluster.login` reconnects a context without a credential plugin or whose login in the file changed,
and otherwise runs the plugin again. `renewFromFile` reconnects by itself a connection without a
plugin whose login expired while its file holds another, once per version of the file.

### Kubeconfig sources

Every context comes from one source file read on its own, never merged, so a dropped file cannot
borrow the user's credentials (D-28). `KubeconfigSources`
([`engine/kubeconfigs.go`](../../internal/engine/kubeconfigs.go), the commands in
[`commands/kubeconfigs.go`](../../internal/engine/commands/kubeconfigs.go), the window's dialogs in
[`fyneui/kubeconfigs.go`](../../internal/ui/fyneui/kubeconfigs.go)) keeps the list in
`<user config dir>/Cogpit/kubeconfigs.json` and never writes a source file. Both binaries build it
in `app.Dirs.KubeconfigSources` ([`internal/app/app.go`](../../internal/app/app.go)).

| Source | Comes from | Stored |
|---|---|---|
| `~/.kube`, synced by default | `engine.WithDefaultFolder` | only its removal |
| This run's files | `--kubeconfig`, else `$KUBECONFIG` (`engine.WithSessionFiles`) | no |
| A kubeconfig file | `kubeconfig.addFile` | yes |
| A sync folder | `kubeconfig.addFolder`: its regular files, not its subfolders, at most 500 of at most 1 MiB | yes |

- The engine never reads the home directory or the environment; the caller passes them, so tests
  never touch a real `~/.kube` (`TestNoDefaultFolderWithoutTheOption`).
- A file is checked to be regular before it is opened (opening a FIFO blocks,
  `TestSyncFolderNeverOpensAFIFO`), and no message quotes a file's content
  (`SourceStatus.SkippedFiles` carries fixed reasons).
- Files and folders are picked by `pickFiles`
  ([`filepicker_choosers.go`](../../internal/ui/fyneui/filepicker_choosers.go); per platform:
  [Platforms](#platforms)).
- `Session.WatchKubeconfigs` polls the files' sizes and modification times every 2 s; on a change it
  closes connections of gone files, renews changed logins and emits `EventContexts`.
- A context's id is `<context> [<label>]` (`SourceContextID`; the label names the file, or the
  folder and file). Confirmations, audit lines and journals use it.
  `ClusterConnector.ResolveContext` maps a bare `--context` name to the one file that defines it and
  refuses a name several files define (`TestRunFlagConnectsABareContextName`). Without `Sources`
  (tests, and the app without a configuration directory) the connector merges `KubeconfigPaths` as
  kubectl does.
- A client certificate's dates
  ([D-75](decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)) come from the
  kubeconfig or the certificate file (`fillClientCerts` in
  [`clientcert.go`](../../internal/cluster/clientcert.go)); a file in a place macOS protects is read
  only after the connect read it, so listing never raises a privacy prompt
  ([H-119](../security/file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)).
- Pasted sources ([D-21](decisions.md#d-21-pasting-a-kubeconfig)) are skipped when the list is read
  (`loadDoc`); their files stay on disk.

**Credential plugins run only after the user allowed them**
([trust boundaries › H-1](../security/trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)
says what an approval binds). `cluster.Connect` asks `Options.ExecPolicy`
(`KubeconfigSources.Policy`) about the plugin described from `rest.Config.ExecProvider`, before any
client exists, so a file changed after listing cannot slip another command past it. A token file
outside the kubeconfig's folder is asked about before client-go reads it (`checkTokenFilePolicy` in
[`sources.go`](../../internal/cluster/sources.go),
[D-17](decisions.md#d-17-token-files-outside-the-kubeconfigs-folder),
[H-24](../security/trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)),
and a cluster that skips the certificate check goes through the same policy
(`kubeconfig.LoginInsecure`). Approvals (`exec-approvals.json`) bind `ExecPlugin.Fingerprint` per
file and context; `cluster.BindPluginFiles`
([`pluginfiles.go`](../../internal/cluster/pluginfiles.go)) hashes a program or CA file inside the
folder, and `proxyBoundPolicy` binds the cluster's proxy from Cluster Settings.
`Session.PendingExecApprovals` groups the waiting logins by file and `ExecPlugin.Identity` (the
dialog: [`kubeconfigs_approval.go`](../../internal/ui/fyneui/kubeconfigs_approval.go));
`Session.ApproveExecAll` stores one approval per context, or none when any changed since the dialog
showed them ([`kubeconfigs_approvals.go`](../../internal/engine/kubeconfigs_approvals.go)).

Adding or removing a source, allowing a plugin and `cluster.remove` are GUI-only: `Spec.GUIOnly`
makes `Dispatch` refuse every other origin before `Enabled` runs
([`guionly.go`](../../internal/engine/guionly.go), `TestKubeconfigCommandsAreGUIOnly`).

**A connection ends with its file.** `Session.CloseGoneSourceConnection` closes a backend only on a
plain fact: its file is gone, or no source offers it under the context's label
(`KubeconfigSources.FateOf`). A file being rewritten, an unreadable source list or a permission
error leave it open (`TestConnectionStaysWhileItsFileIsOnlyRewritten`). It closes only the backend
it looked at, never a connect started meanwhile.

**The connection log** ([D-66](decisions.md#d-66-the-connection-log),
[`connectlog.go`](../../internal/engine/connectlog.go)): every connect, reconnect, login and
disconnect is an attempt of lines, kept in memory and in one owner-only JSON-lines file per context
under `<user config dir>/Cogpit/connection-logs` (7 days, 1 MiB;
[`connectlog_files.go`](../../internal/engine/connectlog_files.go)). `cluster.Connect` reports its
steps through `Options.Trace` ([`connecttrace.go`](../../internal/cluster/connecttrace.go)) and its
stages from an `httptrace.ClientTrace` on client-go's own `GET /version`, opening no connection of
its own ([`connectstages.go`](../../internal/cluster/connectstages.go), D-114).

- A credential plugin's standard error is logged only after a failed login, scrubbed and cut to 4
  KiB. Cogpit captures it by setting client-go's unexported Authenticator field through `reflect`
  and `unsafe` ([`pluginstderr.go`](../../internal/cluster/pluginstderr.go)): a client-go update
  that renames it makes `TestPluginStderrOnFailureIsScrubbed` fail.
- [`toolpath.go`](../../internal/cluster/toolpath.go) appends the built-in and the user's program
  folders to `PATH`, never in front of it, because an approval binds a program on `PATH` by name
  (D-120); folders others may write to are skipped, and `PATH` is composed again at every connect.

**Remove from Cogpit** ([`contextlink.go`](../../internal/engine/contextlink.go), the commands in
[`commands/clustermenu.go`](../../internal/engine/commands/clustermenu.go)): `cluster.links` tells
what `cluster.remove` would unlink. `Session.RemoveKubeconfigSource`, behind `cluster.remove` and
`kubeconfig.remove`, drops the source and its approvals, closes its connections, and drops the tabs,
dock tabs (an editor's unsaved text kept as a draft;
[`dock_gone.go`](../../internal/engine/dock_gone.go)), cluster settings and hotbar tiles
([D-101](decisions.md#d-101-the-kubeconfig-sources-at-hand-and-a-removed-cluster-leaves-the-hotbar))
of the contexts no other source offers; the files stay on disk. A sync folder's context unlinks the
whole folder, and `cluster.remove` refuses with `ErrLinkChanged` when the folder's contexts changed
since the confirmation. A connect running when its context leaves settles at its end
(`settleGoneConnect` in [`connect_gone.go`](../../internal/engine/connect_gone.go)).

## 7. MCP and the built-in agent

**MCP server** (`internal/mcp`, `github.com/modelcontextprotocol/go-sdk` v1.8.0):

- One tool per registry command ([`tools.go`](../../internal/mcp/tools.go)), with the command's
  schema and annotations: `readOnlyHint` for `Read` and `UI`, `destructiveHint` for `Destructive`
  and commands that `Escalates`.
- Streamable HTTP on a loopback address, path `/mcp`, a bearer token, `Host` and `Origin` checked
  against DNS rebinding ([`http.go`](../../internal/mcp/http.go), `TestHTTPGuard`).
- Opt-in ([D-49](decisions.md#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)): the
  switch is the status bar's MCP item
  ([D-23](decisions.md#d-23-the-status-bars-owner-link-and-mcp-switch)), kept in the file
  `mcp-switch` ([`mcp/switch.go`](../../internal/mcp/switch.go)), off when missing. The window
  drives the server through `fyneui.MCPControl`, implemented in
  [`cmd/cogpit/mcpcontrol.go`](../../cmd/cogpit/mcpcontrol.go) because `internal/ui` may not import
  `internal/mcp`. The switch stops only the listener; the built-in agent dispatches in-process.
- The token and port are kept in `mcp-token` and `mcp-port`
  ([D-53](decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it)).
  `RegenerateToken`, the window's alone (the MCP details,
  [`mcpdetails.go`](../../internal/ui/fyneui/mcpdetails.go)), ends every request the old token
  admitted, a call waiting for a confirmation included.
- `cogpit-cli mcp` ([`bridge.go`](../../internal/mcp/bridge.go),
  [`cogpit-cli/mcp.go`](../../cmd/cogpit-cli/mcp.go)) reads the endpoint file and the token before
  every call and forwards it to the running app (`OriginExternalAgent`); only a call that never
  reached the app is sent again. Without a running app, or with `--headless`, it serves an engine of
  its own without a confirmer
  ([H-28](../security/agents-and-mcp.md#h-28-the-status-bars-switch-does-not-reach-a-headless-bridge));
  with the switch off, only the tool `cogpit_mcp_off` (`ServeSwitchedOff`).

Example (`TestShowcaseServiceByClusterIP`): "show me the Service with ClusterIP 1.2.3.4" becomes
`resources_find {kind: services, field: clusterIP, value: 1.2.3.4}` and `ui_navigate {ref}`, all
from the cache.

**`dock.read`** ([D-44](decisions.md#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs)):
the text of logs and terminal tabs lives in the window's widgets. The window installs an
`engine.DockOutputReader` ([`fyneui/dockoutput.go`](../../internal/ui/fyneui/dockoutput.go),
[`termview/output.go`](../../internal/ui/fyneui/termview/output.go)), which pages a tab on the main
goroutine; `Session.ReadDockOutput` waits for it at most 2 s
([`dockoutput.go`](../../internal/engine/dockoutput.go)). A headless engine has no reader. Logs tabs
are readable by every origin. Agents read a terminal tab only while its `DockTab.AgentRead` switch,
set by the GUI alone, is on; it is checked before and after the read, and turns itself off after 15
minutes without a read or one hour after it went on
([`agentread.go`](../../internal/engine/agentread.go)); each read it allowed is recorded
(`EventDockRead`) and flashes the tab's marker
([`dockagentread.go`](../../internal/ui/fyneui/dockagentread.go),
[H-83](../security/agents-and-mcp.md#h-83-an-allowed-terminal-tab-gives-agents-everything-it-shows-from-then-on)).

**What an agent reads of cluster text**
([agents and MCP › H-82](../security/agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents)
has the rules). `Dispatch` first takes the read cluster's Secrets (`Backend.agentSecrets`, at most
10 s, else the read is refused), then passes the result through `Session.forAgent`
([`agentview.go`](../../internal/engine/agentview.go)). The `AgentView` replaces the cached Secrets'
values (at least 10 bytes; [`secretscrub.go`](../../internal/engine/secretscrub.go) has the
exceptions) by `«secret <ns>/<name>/<key>»` and withholds Flux's credential fields
([H-63](../security/agents-and-mcp.md#h-63-credentials-in-flux-objects-reach-agents-through-their-yaml));
its matcher is rebuilt only when the cached Secrets change. The window's notifications reach agents
through the same view. A panic in the filter fails the command, and `GuardAgentMarkers` refuses an
agent's text that writes a placeholder back. A new result type that carries cluster text implements
`engine.AgentResult` ([extending](extending.md)).

**The private turn** ([D-10](decisions.md#d-10-built-in-agent),
[`readslikeuser.go`](../../internal/engine/readslikeuser.go)): over a connection the user marked
private, the built-in agent sets `Invocation.ReadsLikeUser`, which no MCP or CLI request can set.
Read paths ask `engine.ReadsLikeUser(ctx)` or `engine.ReadsAsAgent(ctx)`, never the origin alone;
write rules judge by origin. An error only the user may read is an `engine.UserOnlyError`, whose
`Withheld` text the audit log records. The sites are listed in
[agents and MCP › H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows).

`kinds.list` ([`kindslist.go`](../../internal/engine/commands/kindslist.go)) gives an agent each
kind's profile and the tools of its row menu, drawer and selection menu, all from the registry.

**Built-in agent** (`internal/agent`, D-10): a chat column toggled with ⌘I (`chat.toggle`). Each
turn sends the conversation, a compact `ui_state` and tools to an OpenAI-compatible Chat Completions
endpoint, streams the answer and runs the tool calls through `Session.Dispatch` with
`OriginBuiltInAgent`, so D-2's confirmations and the audit log apply.

<details>
<summary>The agent's files</summary>

| File | Responsibility |
|---|---|
| [`agent.go`](../../internal/agent/agent.go) | `Agent` (implements `engine.ChatAgent`), the connections |
| [`client.go`](../../internal/agent/client.go), [`think.go`](../../internal/agent/think.go) | requests, the streamed answer and its timing (TTFT, OTPS: `ChatTiming`, [D-125](decisions.md#d-125-an-answers-speed-ttft-and-otps)), `HTTPError`; reasoning cut from the content |
| [`tools.go`](../../internal/agent/tools.go) | 20 core commands plus `search_commands` and `run_command`, or every command for `ChatConnection.AllTools`; GUI-only commands left out |
| [`turn.go`](../../internal/agent/turn.go), [`prompt.go`](../../internal/agent/prompt.go), [`failure.go`](../../internal/agent/failure.go) | `Send`, `Resend`, the turn loop and its limits; the system prompt, tool results framed as data; a `Dispatch` error in the model's words |
| [`conversation.go`](../../internal/agent/conversation.go), [`store.go`](../../internal/agent/store.go) | the conversations, `Stop`; the files |
| [`conntest.go`](../../internal/agent/conntest.go), [`exchange.go`](../../internal/agent/exchange.go) | the connection test, layer by layer; a failed layer's request and answer |
| [`cafile.go`](../../internal/agent/cafile.go), [`keystore.go`](../../internal/agent/keystore.go) | a connection's CA file as its only roots; the API keys |
| [`engine/toolschema.go`](../../internal/engine/toolschema.go), [`chatconfirm.go`](../../internal/engine/chatconfirm.go), [`chatscrub.go`](../../internal/engine/chatscrub.go), [`chatfiles.go`](../../internal/engine/chatfiles.go) | the engine's side: tool schemas and descriptions shared with MCP; whether `Dispatch` will ask (`Session.AsksUser`); `Session.ScrubSecretValues`; the owner-only files |
| [`turn_test.go`](../../internal/agent/turn_test.go), [`conntest_test.go`](../../internal/agent/conntest_test.go), [`connections_test.go`](../../internal/agent/connections_test.go), [`keyfile_test.go`](../../internal/agent/keyfile_test.go), [`client_test.go`](../../internal/agent/client_test.go), [`private_test.go`](../../internal/agent/private_test.go), [`live_test.go`](../../internal/agent/live_test.go) | tests against [`testkit/fakellm`](../../internal/testkit/fakellm/fakellm.go) and a real engine session; `live_test.go` against a real endpoint, local only |

</details>

- A turn runs on its own goroutine under the connection's time limit. Each tool call is checked
  against the connection's limits (`engine.DefaultChatLimits` by default) and Stop first; a call
  that fails them, and every call after it, is answered "not run", so the history stays one the next
  request can carry. `Stop` cancels the stream, a running `Dispatch` and its pending confirmation.
- Kept files, owner-only: `llm-connections.json` (never a key), `llm-keys.json`
  ([credentials › H-99](../security/credentials.md#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder))
  and `chat-conversations.json`, scrubbed by `Session.ScrubSecretValues`, which fails closed. A
  conversation in which a private turn ran is never written
  ([H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)).
- The seam is `engine.ChatAgent` ([`chat.go`](../../internal/engine/chat.go)), installed with
  `Session.SetChatAgent`. The chat is no command and so no MCP tool: an external agent driving the
  built-in one could read through a private connection what it may not read itself. depguard keeps
  `internal/ui` from importing `internal/agent`.
- The window's column ([`fyneui/chat.go`](../../internal/ui/fyneui/chat.go); its items, Markdown and
  input in [`chat_items.go`](../../internal/ui/fyneui/chat_items.go),
  [`chat_markdown.go`](../../internal/ui/fyneui/chat_markdown.go),
  [`chat_input.go`](../../internal/ui/fyneui/chat_input.go), a message edited and sent again in
  [`chat_edit.go`](../../internal/ui/fyneui/chat_edit.go)) calls the agent off the main goroutine
  and reads a conversation again on `EventChat` with one reader at a time, so a fast stream never
  queues reads; a streamed delta lays out only the answer's last Markdown block
  (`TestChatStreamingStaysCheap`). The request body opens in a dialog, not a dock tab, because
  agents read dock tabs; Preferences › Agent
  ([`preferences_agent.go`](../../internal/ui/fyneui/preferences_agent.go)) calls the agent
  directly, so no agent can mark a connection private.

**Confirmations** follow [§4.2 › Dispatch](#dispatch).

**The audit log** ([`auditlist.go`](../../internal/engine/auditlist.go), the commands in
[`commands/audit.go`](../../internal/engine/commands/audit.go), the dock tab in
[`fyneui/auditlog.go`](../../internal/ui/fyneui/auditlog.go)): `Dispatch` appends a line for every
agent command to `<user config dir>/Cogpit/audit.log` (`Options.Audit`, shared by the app and
`cogpit-cli`); the CLI's own commands are not audited. `audit.list` reads the newest entries of its
last 4 MiB, and `audit.open` shows them in a dock tab, which reads again on `EventAudit`.

## 8. Helm (`internal/helm`)

The Helm SDK (`helm.sh/helm/v4`) runs in Cogpit's process
([D-3](decisions.md#d-3-helm-and-kubectl-binaries)); no `helm` binary is used. Releases are read
from the cached release records. Install, upgrade, rollback and uninstall run the SDK on the
connection's own client configuration (`helm.Actions`), journaled before they start
([D-11](decisions.md#d-11-crash-stance), §8.6). Chart repositories are those of Helm's own
`repositories.yaml`, which Cogpit never writes
([D-29](decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)).

### 8.1 Releases

- **Records.** One per revision, `sh.helm.release.v1.<release>.v<revision>`, labelled `owner=helm`:
  a Secret, or a ConfigMap with `HELM_DRIVER=configmap`. `Backend.Acquire` joins both drivers'
  scopes ([§5](#5-cache)); `helm.DecodeRecord` decodes either (a ConfigMap's payload:
  [`helm/configmaps.go`](../../internal/helm/configmaps.go)).
- **`helmreleases`** is a virtual kind ([`releases.go`](../../internal/helm/releases.go)
  `NewReleaseProfile`). `Derive` groups the records by namespace, name and driver. The object
  carries the newest record's UID and resourceVersion (confirmations bind to them), its driver
  (`storage`) and the history, never values, notes or the manifest. Of a name held in both drivers
  the release deployed last is shown and the other marked (`hiddenStorage`).
- **Memo.** Decoded records are memoized by UID and resourceVersion (at most 4,096); values, notes
  and resources only for each shown release's newest revision. The memo is purged 5 minutes after
  the last `Derive` (`memoIdle`), so decoded values leave memory with the records' watch.
- **Drawer** ([`detail.go`](../../internal/helm/detail.go)): `Plain`, as every virtual kind's (no
  Details and YAML tabs, no events). Values and Notes are `Private` (agents read `model.Withheld`)
  and cut to 16 KiB or 200 lines; the History comes last
  ([D-128](decisions.md#d-128-a-releases-history-in-its-drawer), `DetailTable.LastUnder`, drawn by
  `underTable` in [`drawertable.go`](../../internal/ui/fyneui/drawertable.go)). The GUI's values
  editor ([`helmdrawer.go`](../../internal/ui/fyneui/helmdrawer.go)) reads `helm.release.values`;
  its Save is `helm.release.upgrade` with `values` and the `revision` read.
- **Revisions tab** ([D-131](decisions.md#d-131-a-releases-revisions),
  [`engine/helmrevisions.go`](../../internal/engine/helmrevisions.go); `ui.helm.revisions`,
  `helm.release.revision`, `helm.release.diff`): a revision is decoded from its cached record
  (`helm.RevisionOf`; manifest objects in Helm's install order, each Secret with its masked form)
  and two are compared (`ReleaseRevisionDiff`); `revisionRecord` reads from the part of the handle
  of the release's driver. Agents read values and Secrets withheld. In the GUI
  ([`fyneui/helmrevisions.go`](../../internal/ui/fyneui/helmrevisions.go)) a copyable text in a list
  row passes a plain click to its row (`selLine.passClick`): Fyne gives the click to the text.

### 8.2 Release actions

- **Seam.** `engine.HelmWriter` and `engine.HelmInstaller`
  ([`helmreleases.go`](../../internal/engine/helmreleases.go),
  [`helminstall.go`](../../internal/engine/helminstall.go)); tests put a fake in `Backend.Actions`;
  the demo cluster has none.
- **SDK settings** ([`actions.go`](../../internal/helm/actions.go),
  [`upgrade.go`](../../internal/helm/upgrade.go), [`install.go`](../../internal/helm/install.go)):
  the helm CLI's defaults (hooks waited for, 5 minutes, ten revisions kept, server-side apply, no
  `--atomic`). Dependencies are never fetched: a library chart or one missing a dependency in
  `charts/` is refused. An upgrade replaces the user-supplied values whole (`ResetValues`). The
  SDK's own reads (release history, discovery) are part of the action, not cache reads.
- **No cancellation.** Rollback and uninstall take no context; upgrade and install get
  `context.WithoutCancel`, because the SDK marks a release failed when its context ends mid-write.
- **Run path** (`runHelmOp` in [`commands/helm.go`](../../internal/engine/commands/helm.go)):
  `HelmOps.Begin` records the entry, the SDK runs behind a panic boundary, and `HelmOps.End` removes
  the entry on success or on `helm.ErrUnchanged` (refused before a revision was made), else keeps it
  as failed. Without a configuration directory there is no journal and no action.
- **Driver.** An action uses its release's driver (`helm.NewActionConfigFor`), an install
  `Preferences.HelmDriver`; `HELM_DRIVER` is never read. Verbs are declared on `secrets` and
  `configmaps`; the access check keeps the release's driver's (`Session.releaseVerbs`).
- **Binding.** Rollback and uninstall (Destructive) bind to the newest record's UID; an upgrade to
  the revision its values are based on and, from a tab, its `textRevision` (`ErrTargetsChanged`).
  Because the cache can lag, the SDK side of an upgrade or rollback re-checks the server's newest
  revision (`helm.ErrReleaseMoved`) before anything changes.
- **Agents.** Install and upgrade always ask the user (`AgentsAsk`), showing inline values
  ([D-27](decisions.md#d-27-an-agents-helm-values-in-the-confirmation)). At run time the origin
  warning, and for an upgrade the confirmation's rows, are rebuilt; a difference refuses it.
- **Rollback dialog** ([`helmrollback.go`](../../internal/ui/fyneui/helmrollback.go)): the engine's
  confirmation of exactly that command, release, context and revision within 10 seconds takes the
  dialog's answer (`guiConfirmer.confirmedByDialog`); an answer not taken is dropped.
- **Values stay in the window** ([`values.go`](../../internal/helm/values.go)). `helm.ScrubValues`
  removes values of 4 or more characters, also base64-encoded and quoted, from errors, toasts, the
  journal and the SDK's log lines; an install scrubs those that differ from the chart's defaults.
  Both binaries wrap their log handler in `helm.RedactSDKLog`. The audit log keeps Helm values by
  size only. Install and upgrade errors reach agents and the CLI without their text (`forCaller`),
  because a template can quote what `lookup` read; a private built-in agent turn reads them as the
  window does.

### 8.3 Upgrade and install

- **Upgrade tab** (`helm.release.upgradeTab`;
  [`helmupgrade.go`](../../internal/engine/helmupgrade.go),
  [`commands/helmupgrade.go`](../../internal/engine/commands/helmupgrade.go),
  [`helmupgrade_repos.go`](../../internal/engine/helmupgrade_repos.go)): the user-supplied values in
  the editor (agents: withheld) and the chart's versions from a repository whose index lists it by
  name (the first in file order that lists the release's own version, else the first), starting on
  its newest non-prerelease when that is newer. The own version upgrades with the stored chart;
  another is downloaded (`HelmRepos.FetchChart`) before the journal records anything. No preview.
- **Upgrade tab GUI** ([`helmupgradetab.go`](../../internal/ui/fyneui/helmupgradetab.go)): the dock
  syncs only when tabs come, go or move (`dockEqual` in
  [`shell.go`](../../internal/ui/fyneui/shell.go)), so the engine updates the UI state before the
  tab's editor event (`setHelmUpgradeChoice`). Save sends `editor.setText`, then upgrades at the
  text revision it returned.
- **Install tab** (`helm.chart.installTab`,
  [`commands/helminstall.go`](../../internal/engine/commands/helminstall.go)): downloads the
  archive, checked against the index's digest when it lists one, and keeps it in memory with the
  tab, so the install installs what the tab showed.
- **Preview** (`helm.chart.preview`): a dry run naming objects, hooks and CRDs, never content.
  Against the cluster (`DryRunServer`) only for a caller that reads like the user; agents and the
  CLI render offline (`DryRunClient`), since values reach `lookup` through `tpl`
  (`TestKindHelmInstall`). The window installs only from its preview.
- **Install** (`helm.chart.install`): refused when the cache or the server holds the release name;
  an error before revision 1 is `helm.ErrUnchanged` (CRDs and a created namespace stay).

### 8.4 Repositories and charts

- **Helm's file** ([`repofile.go`](../../internal/helm/repofile.go),
  [`helmrepos.go`](../../internal/engine/helmrepos.go),
  [`commands/helmrepos.go`](../../internal/engine/commands/helmrepos.go)): resolved by
  `helm.RepositoryConfigPath` in `cmd/cogpit` only, so no test reads the developer's file.
  `HelmRepos.Sync` stats it every 2 seconds and reads it once unchanged for 250 ms, keeping the list
  only if still unchanged after the read (`helm repo add` rewrites in place); a file that does not
  parse keeps the last good list. Used entries: valid names, http or https (no OCI), the first of a
  name, absolute CA and certificate paths, at most 256. The Repositories dialog
  ([`helmcharts.go`](../../internal/ui/fyneui/helmcharts.go),
  [render](../../internal/ui/fyneui/testdata/helm_repos_dialog_dark.png)) is read-only: no Add or
  Remove (D-29).
- **Chart id** (`kinds.HelmChartName`): `<repository>.<chart>` with the repository's dots doubled;
  the first single dot splits (`SplitHelmChartName`). It names the `helmcharts` objects
  ([`kinds/helmcharts.go`](../../internal/model/kinds/helmcharts.go)), which `HelmRepos.publish`
  writes into the shown cluster's local store.
- **Indexes** in `<user cache dir>/Cogpit/helm/indexes/`, keyed by a hash of name and URL, are
  downloaded on `helm.repo.update` and, only while the Charts view or a Helm Install tab is open,
  when older than 10 minutes, new or changed (`TestHelmIndexRefreshedOnlyWhileShown`); one at a
  time, an index at most 32 MiB, an archive 16 MiB, 2 minutes each.
- **Credentials** ([credentials](../security/credentials.md)) are unexported in `helm.Repository`.
  `Fetcher.clientFor` ([`repo.go`](../../internal/helm/repo.go)) builds each download's transport
  from the proxy, the entry's TLS files and an `authTransport` that sends basic auth only to the
  repository's own origin (any with `pass_credentials_all`); https never downgrades to http.
  Credentials over plain http wait for the GUI-only `helm.repo.allowHTTPCredentials`
  ([`helmhttp.go`](../../internal/engine/helmhttp.go), the window's banner in
  [`fyneui/helmhttp.go`](../../internal/ui/fyneui/helmhttp.go),
  [H-40](../security/credentials.md#h-40-credentials-over-plain-http)).
- **Origins.** `Repository.Origin` (`http`, `insecure-tls`) is marked wherever its charts show and
  in agents' confirmations (`kinds.HelmOriginWarning`). An upgrade that downloads from a repository
  whose origin changed since its tab showed it is refused
  ([H-41](../security/trust-boundaries.md#h-41-repositories-whose-content-nothing-authenticates)).
- **Proxy.** `helm.Fetcher.Proxy` is `Session.NonClusterProxy`
  ([`proxy.go`](../../internal/engine/proxy.go)): `Preferences.HTTPProxy`, else the environment's.
  It carries the repository downloads, chart icons and kubectl downloads; the LLM endpoint uses the
  environment's proxy only ([`agent/client.go`](../../internal/agent/client.go)).

### 8.5 A chart's drawer and icons

- **Version.** `ui.setChartVersion` sets `UIState.DrawerChartVersion`, which ends with the drawer
  (`syncDrawerChartVersion`); Install from the drawer takes that version.
- **README** ([`readme.go`](../../internal/helm/readme.go),
  [`helmchartdrawer.go`](../../internal/engine/helmchartdrawer.go),
  [`commands/helmchartdrawer.go`](../../internal/engine/commands/helmchartdrawer.go); the window's
  [`fyneui/helmchartdrawer.go`](../../internal/ui/fyneui/helmchartdrawer.go),
  [render](../../internal/ui/fyneui/testdata/helm_chart_drawer_dark.png)): the archive's root
  README, downloaded through `FetchChart`, cut at 256 KiB and cleaned (`helm.CleanReadme`); loaded
  only for the shown chart's `Detail` or an agent's `object_get` (`DetailWait`, at most 30 s); kept
  on disk only when digest-verified. Agents read it as untrusted text
  ([H-86](../security/agents-and-mcp.md#h-86-a-charts-readme-reaches-agents)). The GUI never fetches
  its images and opens only absolute http(s) links, on a click.
- **Icons** ([D-46](decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them),
  [`icon.go`](../../internal/helm/icon.go), [`helmicons.go`](../../internal/engine/helmicons.go),
  [`helmcharticons.go`](../../internal/ui/fyneui/helmcharticons.go)): only the window fetches
  (`Session.ChartIcon`), while `Preferences.LoadChartIcons` is on. `helm.IconFetcher` is https only,
  without credentials, bounded (256 KiB, 20 s), and dials only public addresses unless the host is a
  repository's own; `CheckIcon` admits PNG, JPEG and a script-free SVG.

### 8.6 Journals

`helm.json`, `drains.json` and `nodeshells.json` in `<user config dir>/Cogpit/` (0600) are shared by
every Cogpit process of the user. A change replaces the file atomically (`writeFileAtomic`, fsynced:
about 13 ms a write on macOS); a file that does not parse is reported, never overwritten.

- **Helm journal** ([`helmjournal.go`](../../internal/engine/helmjournal.go),
  [`helmops.go`](../../internal/engine/helmops.go)): per release the action, `FromRevision`, chart
  and version, never values. On connect entries the cache shows finished are dropped
  (`helmOpFinished`) and the rest offered (`helm.pendingOperations`, the banner): a rollback or
  uninstall to run again; an upgrade as a rollback to `FromRevision`, since the SDK's upgrade
  refuses a `pending-upgrade` release; an install with nothing to finish.
- **Lock** ([`journallock.go`](../../internal/engine/journallock.go)): every access holds an
  exclusive lock on `<dir>/journals.lock` (`flock`; `LockFileEx` on Windows, where an owner file is
  created sharing its deletion: `createOwnerFile` in
  [`journallock_windows.go`](../../internal/engine/journallock_windows.go)), waited for at most 10
  seconds (`ErrJournalBusy`); the kernel releases it with the process.
- **Owners.** A writing process locks `<dir>/owners/<random id>.lock` for its life and its entries
  name it; a PID is never trusted, and the files are opened close-on-exec. A running entry of
  another live owner is not offered, replaced or removed (`ErrRunsElsewhere`)
  ([H-22](../security/privilege-footprint.md#h-22-two-cogpit-processes-share-the-journals);
  `TestJournalsKeepEveryProcessesEntries`, `TestJournalEntryOfAKilledProcess`).
- **Node shells** ([`nodeshelljournal.go`](../../internal/engine/nodeshelljournal.go),
  [`nodeshellsweep.go`](../../internal/engine/nodeshellsweep.go)): `StartNodeShell` records the pod
  before its create, with a random id in the annotation `cogpit.io/node-shell-id`, then its UID. On
  each connect `followNodeShellSweep` deletes the context's recorded pods with their UID as
  precondition (without one: by name, after comparing the id), skipping those of another live
  process or still in use, and audits each delete; a crash leaves the pod until that connect
  ([H-71](../security/privilege-footprint.md#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)).

### 8.7 Storage driver

- `Preferences.HelmDriver` is Secrets (empty) or `configmap`, chosen only in the window
  (`ErrHelmDriverGUIOnly`) since ConfigMaps are often more widely readable; `sql` is refused
  (`helm.ErrSQLDriver`).
- A release ConfigMap's payload is redacted like a Secret's (`model.IsHelmRecord`, `model.Redact`,
  `RedactManifests`), and agents cannot edit or create one (`ErrSecretGUIOnly`).

## 9. Metrics (`internal/metrics`)

Metrics cannot be watched, so they are polled ([§5](#5-cache)), and only while a view, a drawer or
the Overview shows them; their request budgets are P13 (a Pods list and a pod's drawer) and P14 (the
Overview) in [testing](testing.md#performance-budgets). No provider, a refusal
and a provider that does not answer are a `State` the UI shows, never an error. A cluster has two
providers: the Kubernetes Metrics Server for current values, and a Prometheus (or Thanos,
VictoriaMetrics) the cluster runs, read through the API server's service proxy, for history,
network, filesystem and disks. Cogpit installs nothing.

| File | Responsibility |
|---|---|
| [`provider.go`](../../internal/metrics/provider.go) | The seam: `Provider`, `RangeProvider`, `Detector`, `State`, `Capabilities`, `RangeQuery`, `Want` |
| [`server.go`](../../internal/metrics/server.go), [`conn.go`](../../internal/metrics/conn.go) | The Metrics Server's provider and detector; `SourcesForConn` builds both providers on the connection's clients |
| [`hub.go`](../../internal/metrics/hub.go), [`hubobject.go`](../../internal/metrics/hubobject.go), [`chart.go`](../../internal/metrics/chart.go), [`retain.go`](../../internal/metrics/retain.go) | `Hub`: subscriptions, polls, the result cache, provider roles; an object's poll plan and series; series kept after a drawer closed |
| [`hubcluster.go`](../../internal/metrics/hubcluster.go), [`promcluster.go`](../../internal/metrics/promcluster.go), [`promclusterpods.go`](../../internal/metrics/promclusterpods.go) | The cluster's totals (the Overview) |
| [`disk.go`](../../internal/metrics/disk.go), [`diskfs.go`](../../internal/metrics/diskfs.go) | The nodes' filesystems |
| [`prom.go`](../../internal/metrics/prom.go), [`promrange.go`](../../internal/metrics/promrange.go), [`promql.go`](../../internal/metrics/promql.go) | The Prometheus provider: proxy requests, range queries, every PromQL string |
| [`promdetect.go`](../../internal/metrics/promdetect.go), [`promflavor.go`](../../internal/metrics/promflavor.go), [`promquery.go`](../../internal/metrics/promquery.go) | Detection by Service name; Thanos and VictoriaMetrics; the per-cluster query settings |
| [`source.go`](../../internal/metrics/source.go), [`detectunknown.go`](../../internal/metrics/detectunknown.go), [`status.go`](../../internal/metrics/status.go) | Providers switched off per cluster; one detection for the integrations' state; the source row's data |
| [`chain.go`](../../internal/metrics/chain.go) | `isBuildInfo`, `severity`; `Chain`, one detector over both providers (tests only) |

### The hub

A `Scope` is `{Kind, Namespace, Name}`: `pods` and `nodes` (a list when `Name` is empty, else one
object), `cluster` (an object) and `nodeDisk` (a list). `Hub.Subscribe(scope, onChange)` starts the
scope's one poll goroutine, shared by every subscriber; the last `Release` stops it. Nothing
subscribed polls nothing, not even discovery (`TestNothingOpenNothingRequested`); only `Hub.Fetch`
and `Hub.DetectUnknown` ask outside a subscription.

| What | Value | Constant |
|---|---|---|
| List poll | 60 s | `DefaultListInterval` |
| Object poll | 15 s; at least the finest query set's step while Prometheus gives the live values | `DefaultObjectInterval` |
| Result served as current | 15 s, or twice the interval while polled | `DefaultTTL` |
| Detection kept | 10 min found, 2 min not | `DefaultDetectTTL`, `DefaultRetryDetectTTL` |
| Failing provider left alone | 1 min | `DefaultProviderRetry` |
| One poll | 20 s | `DefaultRequestTimeout` |

A subscription on a result younger than the TTL waits for it to go stale
(`TestSubscribeOnAFreshResultWaitsForItToGoStale`), except an object scope while a provider with
history is known. `Hub.Fetch` is the one-off read (`metrics.get`, `cluster.overview` without the
page): a fresh result of the scope or of a list covering it, else one request, no poll
(`TestFetchCachesForTheTTL`). Concurrent fetches of a scope share one request; a panic in a poll or
a provider call is recovered there (`TestProviderPanicIsContained`).

**Roles.** `NewHubFor(Sources{Live, History})` gives each provider a slot with its own detection,
state, last answer and last failure; `NewHub(det)` has one (the demo's `metrics.Fixed`). Each poll
(`rolesLocked`) gives the history role to the first ready provider with range queries (Prometheus),
the live role to the first without (the Metrics Server), else to the history provider.
`Server.Detect` is one discovery request of `metrics.k8s.io/v1beta1`; an object without a
measurement yet is `ErrNoData`, not a state.

- Prometheus is looked for only by an object scope's poll, a read of current values whose Metrics
  Server fails or is missing, the disks' poll, and `Hub.DetectUnknown` while Cluster Settings or the
  integrations question shows the cluster ([§4.7](#47-integrations-d-76-d-82)); CPU and Memory
  columns send it nothing while the Metrics Server answers (`TestBothProvidersBudget`).
- A provider that fails while in use takes the failure's state and is left alone for `ProviderRetry`
  while the other serves its part, from the same poll on
  (`TestMetricsServerFailsPrometheusTakesOver`, `TestPrometheusGoneLeavesTheLiveValues`); one
  nothing can replace is asked at every poll; a 404 drops it and detection runs again. With none
  answering, the state is forbidden before unavailable before notServed (`worstLocked`).
- The Metrics Server's 403 for one namespace's pods is that scope's answer (empty, `forbidden`,
  cached for the TTL), not the provider's failure (`namespaceRefusal`,
  [D-39](decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)).
- A found provider that fails keeps its chart variants (`Hub.Capabilities`); one not found any more
  loses them.
- `Hub.Providers` (`ProviderStatus`) is the source row's data: status, Service, last good answer,
  last error as a class (`errorClass`), what it serves; no probe keeps it current. Messages quote
  only the API server's Status text, never a proxied body (`errorText`, `parseResult`;
  `TestErrorClassesCarryNoBodies`).

### An object's poll and chart

A pod's or node's drawer and the Overview subscribe an object scope; its `chart` lives while
subscribed and starts from a fresh list's measurement (`seedLocked`). `pollObject` sends what
`planLocked` asks for, in parallel:

| Request | When |
|---|---|
| History: CPU and memory over the finest set's range (2 range queries) | none held at that range and step; a range or the node's addresses changed; after `Hub.Forget` |
| Live value: 1 `get` (the nodes' list for the cluster) | every poll |
| Network (2), filesystem (1–3 range queries) | while a chart shows them (`Subscription.SetWants`): the range once, then from the last point at each step |
| CPU and memory from Prometheus | the Metrics Server failed: in the same poll, then at each step |
| The gap | once, for live points more than two object intervals and a step apart |

- **Grid and query sets.** `stepOf(shortest, longest) = max(15 s, shortest / 120, longest / 1000)`
  in whole seconds, aligned to the epoch (`alignDown`). The coarsest set (`bandsLocked`) is the
  longest range at the step of the shortest one (of an hour when every range is longer); a range
  that would get fewer than 60 points there gets a finer set, shared with the longer ranges that
  stay within 1,000 points at its step. The finest set is followed incrementally, a coarser one
  asked once up to where the next finer begins (`TestQuerySetsForFarApartRanges`,
  `TestFarApartRangesGetTheirOwnQuerySets`). The engine passes every visible chart's range
  (`drawerWatch.windowListLocked`), the default for the containers of a pod the cache does not hold
  yet.
- **Seam and bounds.** A range answer replaces the chart's points in its interval (`splice`); a live
  value is added only when newer than the last point. A series keeps its longest range, at least an
  hour, back from its own newest point (the cluster's clock), at most 5,760 points (`chart.trim`).
- **CPU and memory only while shown** (`Subscription.SetUsage`): a scope whose charts show neither
  asks for neither (`TestUsageNotAskedWhileNoChartShowsIt`).
- **Node addresses** (`Hub.SetNodeAddresses`): the engine passes `status.addresses` from the cache;
  new addresses ask for the history again (`TestNodeDrawerHistoryFromNodeExporterByAddress`).
- **History across drawers** (`retainLocked`, `restoreLocked`): the last release keeps the CPU and
  memory series, the next subscription within `RetainAge` starts with them; at most 50 charts, 1 h,
  50,000 points, least recently closed dropped first (`TestRetainedHistoryIsBounded`).

### Prometheus

**Requests.** client-go's `Services(ns).ProxyGet` on the connection's typed client: counted,
authenticated as the user, on the requests' budget (`cluster.DefaultQPS`), `GET` only. A body over
16 MiB, a query over 15 s or a series over 11,000 points fails the request; NaN and infinite samples
are dropped. A failure is a `StateError`: 401/403 forbidden, 404 notServed, 503 and timeouts
unavailable. Queries carry label values only.

**Detection** (`PrometheusDetector`). Candidates are the cache's Services (`Backend.promServices` in
[`engine/metrics.go`](../../internal/engine/metrics.go): the Services informer, waiting up to 3 s
for the access reviews and 8 s for the first list), ranked by name (`Candidates`):
kube-prometheus-stack's first (100), then kube-prometheus, `prometheus-operated`, any
`*prometheus-server`, Thanos's Querier and frontend, VictoriaMetrics' single-node and vmselect
(under `/select/0/prometheus`), and `prometheus` (70 in `monitoring`, `prometheus` or
`lens-metrics`, else 40); no other Service is probed. The port is the first with a known web name,
else 9090, else 80, else the first, tried by number, then by name
([D-96](decisions.md#d-96-the-service-proxys-port-spelled-by-its-number-first)). At most `MaxProbes`
(3) candidates are probed with `GET /api/v1/status/buildinfo`, best first; the first buildinfo
answer wins. A 401 or 403 moves on to the next spelling and candidate; when nothing answers, a
refusal makes the state forbidden, a failure other than 404 or a foreign answer unavailable, else
notServed. Thanos and VictoriaMetrics get the same queries; `Target.Flavor` only names them, and
they rank below a Prometheus because Cogpit's queries do not narrow by a cluster label
([D-71](decisions.md#d-71-thanos-and-victoriametrics-read-as-prometheus)).

**The user's setting** ([`engine/metricssettings.go`](../../internal/engine/metricssettings.go)).
`cluster.setPrometheus`
([`commands/metricsprometheus.go`](../../internal/engine/commands/metricsprometheus.go)) sets a
context's whole setting: the address `namespace/service:port` (`CheckPrometheusOverride`), the path
prefix, the query format (where a node's series come from), the empty-container filter and HTTPS
(the proxy's `https:` scheme; the API server makes the TLS connection). It is kept in
`<user config dir>/Cogpit/metrics.json` (owner-only; invalid entries are skipped on read), applied
with `PrometheusDetector.SetOverride`, `SetQuery` and `Hub.Forget`, and by the connector before
anything is detected; with an override only that Service is probed. `DangerUI`, and an agent's call
always waits for the user (`AgentsAsk`,
[H-26](../security/privilege-footprint.md#h-26-prometheus-is-found-by-name)). `cluster.prometheus`
reads it with the Prometheus in use. The window's Cluster Settings › Prometheus
([`clustersettings_prometheus.go`](../../internal/ui/fyneui/clustersettings_prometheus.go)) sends
the whole setting at every change and follows `cluster.prometheus` after every metrics event
(`TestPrometheusOverrideFromTheSettings`, `TestPrometheusSettingsOnThePage`).

**Queries** ([`promql.go`](../../internal/metrics/promql.go)). Label values go through `quoteLabel`
(`FuzzQuoteLabel`), node names and addresses in a regular expression also through
`regexp.QuoteMeta`. Rates use 4 min for instant queries and `max(4m, step + 1m)` for ranges
(`rateWindow`, `rangeRate`).

- A pod: cAdvisor per container (`container!="",container!="POD"`, or `container!="POD"` with the
  filter off, when the pod's own cgroup is its line); the network per pod, which a container's chart
  carries (`NetworkOfPod`); the filesystem's usage, else its read and write rates
  (`FilesystemSource`, found by trying, remembered per kind for the provider's life).
- A node and the cluster: node-exporter's CPU, memory (`MemTotal − MemFree − Inactive_file`),
  interfaces (`nodeInterfaces`) and block devices, else cAdvisor's root cgroup (`id="/"`). CPU and
  memory try node-exporter first and `Prometheus.nodeSource` remembers the answer for the provider's
  life (`ObjectSeries.UsageSource`); network and filesystem join both by `or`. Series match by the
  `node` label, else by `instance` as the name or an address; each exporter counts once. Why:
  [D-43](decisions.md#d-43-where-the-nodes-disk-comes-from).
- The lists' instant queries read cAdvisor only (a node: its root cgroup), so on
  kube-prometheus-stack, which drops the root cgroup, the Nodes list has no CPU and Memory while the
  Metrics Server is down.
- Only CPU and memory must succeed. A part without a value is `model.Usage.Missing`: an empty cell
  sorted last, a point left out (`TestUsageCellsLeaveAMissingPartEmpty`).

### The cluster's totals and the disks

`ClusterScope(masters)` names the control-plane nodes (from the cache), so another control plane is
another scope; it is polled like a drawer's object with the targets every node, `master` and
`worker` (every node less the masters, so no query names workers). Live values: the nodes' list
summed (`clusterSums`); history: `clusterRange`, the node charts' series summed; pods: the cache's
count, preceded once by kube-state-metrics' (`podCountQuery`, `addPodsHistory` in
[`clusteroverview_pods.go`](../../internal/engine/clusteroverview_pods.go)). The engine
([`clusteroverview_charts.go`](../../internal/engine/clusteroverview_charts.go)) subscribes while
the page is open and CPU or Memory shows. Not guarded: a Prometheus holding other clusters' series,
or scraping cAdvisor twice, inflates the every-node sum.

`DiskScope` is a list scope: `pollDisks` sends one instant query (`diskQuery`: size and available
bytes of every node's filesystems, told apart by a `part` label). `diskUsages` filters the answer
again with `realFilesystem` (no memory, kernel, container or image filesystem; no system or runtime
mountpoint) and keeps one mountpoint per device; `model.NewNodeDisk` makes the fullest the summary
([D-90](decisions.md#d-90-a-nodes-disks-a-usage-bar-per-real-filesystem); a node's drawer holds the
scope through [`drawerdisk.go`](../../internal/engine/drawerdisk.go)). `Hub.NodeDisk(keys…)` finds a
node by name, then address. Without node-exporter's series the scope's state says why (the Disk
header's tooltip, `diskColumnTip`, drawn by
[`tableheadertip.go`](../../internal/ui/fyneui/tableheadertip.go); the drawer's `DiskNote`, its rows
drawn by [`metricsdisk.go`](../../internal/ui/fyneui/metricsdisk.go)). Nothing subscribes unless
`Hub.DisksPossible` (a Prometheus slot, or the demo's provider). The kubelet's summary API is never
used: it needs `nodes/proxy`.

### Settings that narrow what is asked

- **Metrics source** (`Hub.SetSource`): the two metrics integrations' switches (§4.7) turn slots
  off, applied by `state.followMetricsSource`
  ([`metricssource.go`](../../internal/engine/metricssource.go)) at every reconcile. An off slot is
  never detected nor asked; a change drops results and kept charts and calls `Forget`, so no chart
  mixes providers (`TestMetricsSourceNone`, `TestMetricsSourceSetting`).
- **Hidden metrics** ([`engine/hidemetrics.go`](../../internal/engine/hidemetrics.go)): a context's
  `HiddenMetrics` gate the subscribers, not the hub: a list's feed (`viewWorker.followHidden`), a
  drawer's scope (`drawerWatch.followHidden`), the Overview's (`overviewCharts.setHidden`) exist
  only while something they show is visible (`TestHiddenMetricsAreNotPolled`). `metrics.get` still
  answers.

### The engine's side

[`engine/metrics.go`](../../internal/engine/metrics.go). `Backend.Metrics` is the connector's
`NewHubFor` over `SourcesForConn` (tuned by `ClusterConnector.MetricsOptions`) or the demo's
`NewHub`; it closes with the backend.

| Subscriber | Scope | Code |
|---|---|---|
| A Pods or Nodes view, once its kind has listed | the nodes; the pods in all namespaces, or per namespace the view reads (D-39) | `startListFeed`, `usageListScopes` |
| The open pod or node drawer | the object; each poll emits `EventMetrics` | `startDrawerUsage` |
| A drawer's pod table (a node's, a workload's, [D-40](decisions.md#d-40-a-pods-table-in-the-workloads-drawers)) | the pods' namespace scopes; each poll wakes the drawer | [`drawerpods.go`](../../internal/engine/drawerpods.go) |
| The Nodes list's Disk column, a node's drawer | `DiskScope` | `startListFeed`, `drawerWatch.followDisk` |
| `metrics.get` | `Hub.Fetch`; every pod: the Pods view's scopes, at most four at once | `MetricsGet`, `fetchPodLists` |

- **Cells.** The profile's `UsageCells` and `DiskCell` fill CPU, Memory and Disk; `Cells` leaves
  them empty. The row cache keeps the profile's row as `base` and patches those cells when the
  feed's generation moves (`rowCache.buildWith`): a measurement copies cells, never rebuilds rows,
  and the sort sees the patched cells. Pod tables in drawers take the same cells from `Hub.Pod`.
- **The drawer's data.** `Session.ObjectMetrics(ref)` reads the hub and the cache, sends nothing:
  status, providers, capabilities, variants, history source, a node's disk, and one `MetricsTarget`
  per chart (the object, then each container) with requests and limits (`model.PodTotals`) or a
  node's allocatable and capacity, and its series cut to the chart's range.
- **Range and variant are engine state** (rule 2), per chart in `drawerWatch`: `ui.setMetricsRange`
  (1 min to 30 d, [`commands/metricsrange.go`](../../internal/engine/commands/metricsrange.go)) and
  `ui.setMetricsVariant` (network and filesystem only with a provider that has them,
  [`commands/metricsvariant.go`](../../internal/engine/commands/metricsvariant.go)) are `DangerUI`,
  refuse another object (`ErrDrawerNotOpen`), update the subscription (`SetWindows`, `SetWants`,
  `SetUsage`) and emit `EventMetrics`; `ui.state` reports `drawerCharts` and
  `drawerMetricsProviders` (`TestMetricsRangeCommand`).

### The window

- **Panel** ([`metricsview.go`](../../internal/ui/fyneui/metricsview.go)): a `metricsPanel` kept in
  `drawer.kept`; on each `EventMetrics` it reads `ObjectMetrics` off the main goroutine and updates
  in place (`TestMetricsEventUpdatesInPlace`). Each chart's `chartCtl` dispatches range and variant
  and then follows the engine; `chartCtl.pending` holds an asked range until the engine answers
  (`TestMetricsRangeIsEngineState`).
- **Chart** ([`chart.go`](../../internal/ui/fyneui/chart.go)): series, dashed reference lines and
  event markers in theme tokens, the area under each run of points filled
  ([`chartfill.go`](../../internal/ui/fyneui/chartfill.go)). Each run of points is one path
  rasterised by rasterx into a `canvas.Raster` layer (`chartLayer`) that keeps its image across
  polls ([`chartpath.go`](../../internal/ui/fyneui/chartpath.go),
  [D-89](decisions.md#d-89-how-a-charts-line-is-drawn); `TestChartLayerReusesItsImage`). A step
  longer than four times the median step and than two minutes breaks the line
  ([`chartgap.go`](../../internal/ui/fyneui/chartgap.go) `gapLimit`). Hover
  ([`chart_hover.go`](../../internal/ui/fyneui/chart_hover.go)) implements only `desktop.Hoverable`,
  so clicks and scrolls pass through, and repaints its overlay alone (`refreshOverlay`).
- **Source row** ([`metricsstatus.go`](../../internal/ui/fyneui/metricsstatus.go),
  [D-88](decisions.md#d-88-the-metrics-source-row)): a chip per `ProviderStatus`; an unreachable
  provider links to its Cluster Settings section (`cluster.settings.open`), an undecided cluster to
  `cluster.integrations.ask` (`TestSourceRowLinksToTheSettings`). The caption above it is a line
  whose pieces may link (`sourceLine`,
  [`fyneui/metricssource.go`](../../internal/ui/fyneui/metricssource.go)).
- **Table bars** ([`tablebar.go`](../../internal/ui/fyneui/tablebar.go)): two rectangles a cell
  makes once and reuses; `layoutBar` runs again on new content, since Fyne's table updates a cell
  without laying it out.

Kind tests ([`prometheus_kind_test.go`](../../internal/testkit/integration/prometheus_kind_test.go),
[`clusteroverview_kind_test.go`](../../internal/testkit/integration/clusteroverview_kind_test.go),
[`metricsview_kind_test.go`](../../internal/ui/fyneui/metricsview_kind_test.go)) read the stacks
`make kind-prometheus`, `kind-kps` and `kind-vm` install
([hack/kind](../../hack/kind/README.md#prometheus)) and skip without them; `TestKindMetricsFallback`
needs `COGPIT_KIND_FALLBACK=1`, `TestRealThanos` `COGPIT_REAL_THANOS`.

### D-77: Alertmanager (`internal/alerting`)

Alerts cannot be watched, so a poller asks for them
([D-77](decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)). Every request is a GET
through the API server's service proxy on the connection's counted clients
([`client.go`](../../internal/alerting/client.go); budget
[P17](testing.md#performance-budgets)).

- **Detection** ([`detect.go`](../../internal/alerting/detect.go)): `Candidates` ranks the cached
  Services by the names charts and operators give Alertmanager, the port by name, else 9093, else 80.
  At most `MaxProbes` (3) are asked, each with the alerts request itself (the port by number, by
  name if the proxy refuses the number); `/api/v2/status` is never asked, so Alertmanager's
  configuration never reaches Cogpit. An address set with `cluster.setAlertmanager` (kept in
  `metrics.json`) replaces detection.
- **Poller** ([`poller.go`](../../internal/alerting/poller.go)): one per backend while the
  integration is on (`Backend.alerts`, run by `followAlertmanager`), active only while its cluster
  is shown and connected ([D-69](decisions.md#d-69-several-clusters-connected-at-once)).
  `GET /api/v2/alerts?active=true&silenced=false&inhibited=false` every 30 s, failures backing off
  to 5 minutes, the last answer kept for 5 minutes of failures; with nothing found, detection reads
  the cache again every 5 minutes. Bounds: 15 s a request, 16 MiB an answer, 10,000 alerts, 4 KiB an
  annotation, 64 labels of 1 KiB; `Classify` drops severity `none`, Watchdog and InfoInhibitor. An
  error text is the API server's message or the failure's class, never the service's answer. A poll
  recovers from a panic on the poller's goroutine; `Stop` does not wait, since the engine stops a
  poller while holding locks its `OnChange` may wait for; `Close` waits.
- **Engine** ([`alerts.go`](../../internal/engine/alerts.go)): `OnChange` stores the snapshot in
  `alertBook`, whose own lock lets `UpdateUI` read it without the state's ([§4.3](#43-ui-state)),
  then, on a goroutine of its own, refreshes the pill (`UIState.syncAlerts`) and emits `EventAlerts`
  and, on a status change, `EventIntegrations`. `alerts.board` (grouped, with the UI state's search
  and namespace filter) and `alerts.list` (flat, its own parameters) are built from the snapshot
  without a request ([`alerts_board.go`](../../internal/engine/alerts_board.go)).
- **Not supported:** a tenant header (Mimir, Cortex) and credentials for an authenticating proxy in
  front of Alertmanager, whose 401 or 403 reads as unavailable.

## 10. Terminal (`internal/terminal`)

Cogpit starts a local shell itself on a PTY (`creack/pty` on macOS and Linux, ConPTY on Windows)
with the terminal's own kubeconfig
([credentials](../security/credentials.md#the-terminals-kubeconfig)) and connects the widget,
`github.com/fyne-io/terminal`, with `RunWithConnection`, as it does pod exec, attach and node shell
sessions ([`termview.go`](../../internal/ui/fyneui/termview/termview.go)). The widget's own
`RunLocalShell` is not used: it gives the shell the process's environment. On macOS and Linux
closing sends the shell's process group SIGHUP, then SIGKILL after 2 s
([`localpty_unix.go`](../../internal/terminal/localpty_unix.go)).

- **Output is untrusted:** `escFilter`
  ([`sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go)) passes plain text, the needed C0
  controls and a known set of sequences, re-emitted with numbers clamped to 9999; it drops every
  OSC, DCS, APC, PM and SOS string (titles, OSC 7, OSC 52), media copy and window operations
  ([trust boundaries](../security/trust-boundaries.md)).
- **The widget is a patched copy**
  ([`third_party/fyne-terminal`](../../third_party/fyne-terminal/PATCHES.md) through a `replace`,
  [D-14](decisions.md#d-14-terminal-widget)): it parses on the goroutine that reads the connection
  and changes screen, configuration, selection and character sets only on Fyne's main goroutine;
  media copy, which changes how later output parses, stays on the reader. Fuzzed without the filter:
  `FuzzHandleOutput`, `FuzzRawWidget`.
- **Scrollback** ([D-56](decisions.md#d-56-the-terminals-scrollback)): the grid holds the screen
  only; lines that scroll off go into a compact store
  ([`history.go`](../../third_party/fyne-terminal/history.go)) bounded at 256 MiB per terminal, the
  oldest dropped first, and a second grid draws the rows in view
  ([`scrollback.go`](../../third_party/fyne-terminal/scrollback.go); the scroll bar:
  [`scrollbar.go`](../../third_party/fyne-terminal/scrollbar.go); tests:
  [`scrollback_test.go`](../../third_party/fyne-terminal/scrollback_test.go)). Rows keep their
  numbers, so a selection stays on its text and agents page with `dock_read`'s `before`, `after` and
  `limit` once `dock.allowAgentRead` allows it
  ([D-44](decisions.md#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs)). A new width
  reflows scrollback and screen; the wheel in a full-screen program goes to the program.
- **Keys**
  ([D-105](decisions.md#d-105-reverse-video-as-a-state-and-a-keys-modifiers-from-the-key-itself)):
  no modifier state of the widget's own; a key takes its modifiers from its event (`keyModifiers` in
  [`input.go`](../../third_party/fyne-terminal/input.go)), so a release the window never sees cannot
  leave ⇧ held, and a block selection takes ⌥ from the drag's press. ⇧ and Ctrl with a named key
  send xterm's sequences (`typeModifiedKey`). Reverse video (SGR 7) is a state applied when a
  character is written (`cellColors` in [`color.go`](../../third_party/fyne-terminal/color.go);
  renders in [`termhighlight_test.go`](../../internal/ui/fyneui/termhighlight_test.go)).

**On Windows** ([`localpty_windows.go`](../../internal/terminal/localpty_windows.go),
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) the shell runs on a
pseudo console (ConPTY, Windows 10 1809 and later) through `golang.org/x/sys/windows`, not through
ActiveState's `termtest/conpty` the widget uses, which ends the process when a pipe cannot be made
and starts the shell before it could join a job. With no process groups and no hangup, the shell
starts suspended, joins a job object of its own (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, breakaway
allowed) and only then runs, so ending the session ends every program started from it. The default
shell (`terminal.DefaultShell`) is `pwsh.exe` on `PATH`, else Windows PowerShell, else `%ComSpec%`;
`$SHELL` is not read. Environment names compare without case (`envBlock`), and `KUBECONFIG` joins
with `;`. After the shell exits, output is read for 500 ms more and the session's files are removed
with retries for up to 2 s, since Windows deletes no file a process still holds. Tests:
[`localpty_windows_test.go`](../../internal/terminal/localpty_windows_test.go).

### kubectl in terminals

A terminal runs the `kubectl` on `PATH`; when it is missing or more than one minor version from the
cluster, Cogpit puts a matching release first, downloaded and verified on the user's click
([D-3](decisions.md#d-3-helm-and-kubectl-binaries); settings:
[usage](usage.md#preferences--kubernetes-kubectl-for-terminals)).

1. **Check** (`Session.checkKubectl` in [`engine/kubectl.go`](../../internal/engine/kubectl.go),
   when a local terminal opens): the cluster's version is `Backend.ServerVersion` (no request of its
   own). The kubectl found is the Preferences' path, else the first on Cogpit's `PATH`; its client
   version comes from `kubectl version --client -o json`, cached per file by size and modification
   time (`kubectl.Prober`). A Preferences' path whose version reads goes first. When the found
   kubectl is not within one minor version (`kubectl.Supported`), the newest installed release of
   the closest supported minor goes first (`kubectl.Best`); without one, the tab offers the server's
   version. The outcome is the tab's `DockTab.Kubectl`, which agents read in `ui_state` and the
   window shows above the terminal ([`kubectloffer.go`](../../internal/ui/fyneui/kubectloffer.go)).
2. **Download** (`kubectl.download`, GUI only; `kubectl.Downloader` in
   [`download.go`](../../internal/kubectl/download.go),
   [D-50](decisions.md#d-50-kubectls-signature-check)): from `https://dl.k8s.io/release` or the
   Preferences' https mirror into `<directory for binaries>/<version>/kubectl`. The `.sha256`,
   `.sig` and `.cert` come first; `lookupRekor` ([`rekor.go`](../../internal/kubectl/rekor.go)) asks
   `rekor.sigstore.dev` for the signature's hashedrekord entry; `Verifier.Verify`
   ([`verify.go`](../../internal/kubectl/verify.go)) has sigstore-go check signature, certificate
   and entry against the embedded trusted root
   ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json),
   `make sigstore-root`). Only then is the binary fetched, its digest compared and renamed into
   place. An unreachable log (`ErrLogUnreachable`) or a missing entry (`ErrNotLogged`) refuses the
   download, without a fallback. One download per release and directory runs at a time; every tab
   offering it shows its progress. It goes through the proxy for non-cluster traffic
   (`Session.NonClusterProxy`). Tests verify against a sigstore of their own
   ([`testkit/sigstoretest`](../../internal/testkit/sigstoretest/sigstoretest.go)).
3. **`PATH`** ([`terminal/kubectl.go`](../../internal/terminal/kubectl.go)): the session's own
   directory holds a link named `kubectl`, first on the shell's `PATH`. Since a login profile can
   reorder `PATH`
   ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first)),
   zsh gets a `ZDOTDIR` of Cogpit's whose `.zshenv` hands over to the user's and puts the directory
   first again before the first prompt, bash an `--init-file`, fish `-C`; other shells keep the
   environment's order. `LocalOptions.EnforceKubectl` also defines a `kubectl` function in those
   three. A running terminal keeps its `PATH`. On Windows (`kubectlOnPath`) a `kubectl.exe` has its
   own folder put first; one by another name gets a hard link `kubectl.exe`, else a copy.

## 11. Updates (`internal/update`)

`cmd/cogpit` hands the platform's updater (`update.New()`) to `Session.SetUpdater`
([`engine/updates.go`](../../internal/engine/updates.go)), which starts it with the preference
`checkForUpdates` and passes changes to `SetAutomaticChecks`. Policy:
[D-4](decisions.md#d-4-packaging-and-updates); trust chain: [updates](../security/updates.md);
bundle and feeds: [releasing › the appcast](releasing.md#the-appcast).

| | macOS | Linux | Windows |
|---|---|---|---|
| Updater | Sparkle 2 in the app bundle, loaded through cgo ([`sparkle_darwin.m`](../../internal/update/sparkle_darwin.m)) | apt and Cogpit's signed APT repository ([`apt_linux.go`](../../internal/update/apt_linux.go)) | WinSparkle beside `cogpit.exe`, loaded without cgo ([`winsparkle_windows.go`](../../internal/update/winsparkle_windows.go)) |
| Present when | an installed bundle (not its disk image, not translocated) with `Sparkle.framework`, `SUFeedURL`, `SUPublicEDKey` | a release build at `/usr/bin/cogpit` of the installed package, `cogpit.sources` present | a release build in the folder of Cogpit's uninstall entry, feed URL and key linked in |
| Scheduled check | Sparkle's probing check (`cogpitSparkleProbe`) | `apt-cache policy cogpit` | Cogpit reads the signed feed ([`appcast.go`](../../internal/update/appcast.go)) |
| `app.installUpdate` (the Update button) | Sparkle's window with the release | the helper through `pkexec`, or the restart into a release apt installed | WinSparkle installs and Cogpit quits |
| `app.checkForUpdates` (Help menu) | Sparkle's window | as the button | WinSparkle's window |

- **Loaded at run time:** neither Sparkle nor WinSparkle is linked, so builds, tests and CI need
  neither. Without an updater, `Updater.Unavailable` says why
  ([`update.go`](../../internal/update/update.go)) and the engine disables `app.checkForUpdates`
  with it; `cogpit-cli` has none (`ErrNoUpdater`).
- **Cogpit schedules the checks** on every platform: at once and every hour while the preference is
  on (`scheduledChecks` in each updater, such as
  [`sparkle_darwin.go`](../../internal/update/sparkle_darwin.go)). The updaters' own schedules stay
  off: a release Sparkle's own check finds is held in an update session during which no check runs,
  and WinSparkle's opens its window without telling Cogpit. Channel: stable only.
- **The Update button**
  ([D-54](decisions.md#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle)): an
  `engine.UpdateNotifier` feeds `UIState.Update` (`{available, version, restart}`);
  `updateNoticeChanged` re-reads the updater under its own lock, so notices out of order leave the
  newest state, and keeps the version only as up to 64 bytes of letters, digits, spaces and
  `.+-_()`.
- **GUI only:** `app.installUpdate` refuses other origins (`ErrInstallUpdateGUIOnly`), since on
  Windows it installs and restarts without asking again; on Linux `app.checkForUpdates` too
  (`engine.UpdateActor`), since it asks for a password or restarts.
- **macOS:** every Sparkle call is queued onto the main dispatch queue, which GLFW's event loop
  drains, so nothing blocks a goroutine; the calls go through protocols mirroring Sparkle 2.10's
  headers. `Info.plist` has `SUEnableAutomaticChecks` and `SUAllowsAutomaticUpdates` NO
  ([`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh)); `startOnMain` also turns
  `automaticallyChecksForUpdates` off in the user defaults. One delegate, kept for the process's
  life (Sparkle holds it weakly), reports found, none found and the end of the user's update session
  through `cogpitSparkleUpdateChanged`, which only stores the report. A probe leaves out the release
  the user skipped and does nothing while the user's check runs; the user's check waits for a
  running probe.

### Linux: apt

[D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages). The package sets
up `/etc/apt/sources.list.d/cogpit.sources` with its key inline; apt verifies every file, and Cogpit
only reads apt's state and asks it to act.

- **Check:** `apt-cache policy cogpit` in the C locale (no network, no root): `Installed` newer than
  this process means a restart is pending, a newer `Candidate` an update.
- **Install:** the click runs [`/usr/libexec/cogpit/update`](../../packaging/linux/update) through
  `pkexec` (polkit action `io.guided-traffic.cogpit.update`, `auth_admin_keep`), one at a time, 20
  minutes at most. The helper takes no argument and nothing from the caller's environment, refreshes
  Cogpit's repository only and upgrades `cogpit` only (`--force-confold`). The outcome is a toast
  (`engine.UpdateReporter`).
- **Restart:** with a newer release installed under the running app, `UIState.Update.Restart` is
  set; the next click starts a detached `/bin/sh` that waits for this process to end and starts
  `/usr/bin/cogpit`, and the app sends itself SIGTERM. Never a restart without that click.

### Windows: WinSparkle

[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle). WinSparkle downloads a
release's setup, checks its EdDSA signature with the update key and runs it (`/S /relaunch`); the
setup waits for `cogpit.exe` to end, installs and starts the new release.

- **Loading:** a release build with the feed URL and the key's public half linked in
  (`-X …/internal/update.windowsFeedURL`, `.edPublicKey`), in the folder of Cogpit's uninstall entry
  (HKCU, or HKLM's 64-bit view); `WinSparkle.dll` by its full path, never the DLL search order.
- **The button** comes from Cogpit's own reading of the signed feed (Go's HTTP client): the newest
  item for this system, newer than the running release, not WinSparkle's `SkipThisVersion`; a feed
  the key does not verify offers nothing. Go's client follows `HTTPS_PROXY`, not Windows' proxy
  settings, which WinSparkle follows: behind such a proxy only the Help menu finds a release.
- **The click** runs `win_sparkle_check_update_with_ui_and_install`, without WinSparkle's Install
  questions; the Help menu runs `win_sparkle_check_update_with_ui`. A session that ends without an
  install clears the button. WinSparkle's shutdown request runs `Updater.OnShutdownRequest`'s
  functions; `cmd/cogpit` signals fyneui's `Options.Quit`.

## 12. Runtime model

```text
 GUI ── a goroutine per command ──Dispatch (origin GUI)──► engine ──► cache, cluster, helm,
  ▲                                                          │        metrics, alerting
  └──────────── fyne.Do ◄──── events (Subscribe) ◄───────────┘
 MCP server goroutines ──Dispatch (origin ExternalAgent)──► engine
 built-in agent goroutine ──Dispatch (origin BuiltInAgent)──► engine
 link queue goroutine ──Dispatch (origin DeepLink: navigation only, §4.6)──► engine
```

- Fyne's main goroutine never waits on the network: the window dispatches each command on a
  goroutine of its own (`shell.dispatch`). Event subscribers run on engine goroutines and must not
  block; the shell applies events and results with `fyne.Do`.
- Once the window closes (`SetOnClosed`), nothing more is posted to Fyne's queue and the dock's
  streams end: Fyne then drains its queue and runs a `fyne.Do` at once on the caller's goroutine.
- Fyne's thread check is a build parameter, `FYNE_THREAD_CHECKS`
  ([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)): on in
  development builds, which show a call off the main goroutine as a toast
  ([`threadcheck.go`](../../internal/ui/fyneui/threadcheck.go)); off in releases, whose tag
  `nothreadcheck` declares the `fyneDo` migration in the app's metadata (not Fyne's
  `migrated_fynedo`, which would make Fyne's caches single-threaded). Every place the GUI runs off
  the main goroutine is audited in [ui-threads.md](ui-threads.md); `TestEveryGoroutineSiteIsAudited`
  fails on a new one.
- Every goroutine that does cluster I/O recovers from a panic at its boundary and reports it as an
  error (`Session.goSafe`, [D-11](decisions.md#d-11-crash-stance)).
- Work that outlives its command runs as a background job
  ([§4.4](#44-tables-details-editors-and-streams)).

## 13. Testing and quality

Tiers, fixtures and budgets: [`testing.md`](testing.md). `make scorecard`, run by hand and not in
CI, writes the measured budgets to the [scorecard](../parity/scorecard.md)
([D-15](decisions.md#d-15-progress-generators)).

## 14. Key dependencies

[`go.mod`](../../go.mod) pins the modules and its `go` line the toolchain; the Makefile pins tools
and frameworks; Renovate updates both ([releasing › Renovate](releasing.md#renovate)).

| Need | Module | Note |
|---|---|---|
| UI toolkit | `fyne.io/fyne/v2` | [D-1](decisions.md#d-1-ui-toolkit) |
| Terminal widget | `github.com/fyne-io/terminal` | upstream commit `46cd12724ff3`, patched in `third_party/fyne-terminal`; its `go.mod` requires Fyne v2.8.1 ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)) |
| Packaging (tool) | `fyne.io/tools` | `fyne package`, the Makefile's `FYNE_TOOLS` |
| Kubernetes API | `k8s.io/client-go`, `k8s.io/apimachinery`, `k8s.io/api` | |
| Exec, attach, port-forward | `k8s.io/streaming` | |
| Node drain | `k8s.io/kubectl` | `pkg/drain` only |
| Metrics API | `k8s.io/metrics` | |
| Helm | `helm.sh/helm/v4` | in-process ([D-3](decisions.md#d-3-helm-and-kubectl-binaries)) |
| MCP | `github.com/modelcontextprotocol/go-sdk` | the 2026-07-28 protocol and earlier ones, stateless over HTTP |
| LLM client | none: `net/http` | OpenAI's Chat Completions API, the subset the agent uses ([`agent/client.go`](../../internal/agent/client.go), [D-10](decisions.md#d-10-built-in-agent)) |
| kubectl's signature | `github.com/sigstore/sigstore-go` | [D-50](decisions.md#d-50-kubectls-signature-check) |
| PTY (macOS, Linux) | `github.com/creack/pty` | |
| Pseudo console (Windows) | `golang.org/x/sys/windows` | ConPTY, the job object |
| YAML | `sigs.k8s.io/yaml` | |
| envtest tier | `sigs.k8s.io/controller-runtime` | `internal/testkit/envtest` only |
| Updates (macOS) | Sparkle framework | the Makefile's `SPARKLE_VERSION` ([D-4](decisions.md#d-4-packaging-and-updates)) |
| Updates (Linux) | apt, `dpkg-query`, `pkexec` | the system's programs ([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) |
| Updates (Windows) | WinSparkle DLL | the Makefile's `WINSPARKLE_TAG`, pinned by SHA-256 ([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) |
| Windows resources (tool) | `github.com/tc-hib/go-winres` | icon, manifest and version information; the Makefile's `GO_WINRES_VERSION` |

## 15. Open decisions

Open decisions and their status: [`decisions.md`](decisions.md).
