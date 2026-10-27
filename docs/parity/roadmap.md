# Roadmap

> **Target:** Lens Desktop 2026.10.51501

The plan: the alpha gate, the milestones, the current focus and what's next, with estimates. Where
Cogpit stands is in [`target-app-parity.md`](target-app-parity.md); the ranked work list is
[`gaps.md`](gaps.md); the one-page summary is [`ROADMAP.md`](../../ROADMAP.md).

Status: ✅ done · 🟡 in progress · ⬜ not started. Hours are Opus 5.5 agent-hours for one agent
working sequentially ([calibration](target-app-parity.md#remaining-effort-and-calibration)).

**Stage: alpha** ([stages](README.md#stages)): the [alpha gate](#alpha-gate) passes on macOS, and
ready for real work, ~47% (estimated, [target-app-parity](target-app-parity.md#ready-for-real-work)),
is inside the ~40–75% band alpha needs. Next is beta. Cogpit is a desktop client for macOS first;
Linux has a `.deb` for amd64 and arm64 with Cogpit's APT repository
([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)),
Windows an x64 setup that updates through WinSparkle
([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle);
[D-7](../developer/decisions.md#d-7-language-and-platforms)).

## Alpha gate

The core workflows a daily Lens user runs, each checked end to end by a person in Cogpit on macOS
against a real cluster; the tests named cover the same steps, most on a kind cluster. A **no**, or
a **partial** that blocks the workflow, means pre-alpha.

| Core workflow | Works end to end? | Needs |
|---|---|---|
| **Connect:** add a kubeconfig (a file or a synced folder; pasted text is not a target, [D-21](../developer/decisions.md#d-21-pasting-a-kubeconfig)); connect with a client certificate, a token or an exec plugin (the EKS, GKE and AKS plugins are not a target, [D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)); switch between clusters | yes: an added source's credential plugin runs only once allowed (`TestKindAddedKubeconfig`, `TestKindExecPluginAfterApproval`); several clusters at once ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once)); sleep, wake and lost connections ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections)); Lens's dialogs not captured ([G2](gaps.md#g2-cluster-connection-and-authentication)) | M1, M2 |
| **Triage a failing pod:** Pods list with status, the drawer with containers, conditions and events, logs (follow, previous container), a shell in the container | yes: logs with follow, timestamps, the previous container and a workload's pods; the pod shell (`TestKindEngineWorkflows`, `TestKindWorkloadLogsAttachRestartScale`) | M2, M3 |
| **Change a workload:** edit YAML and apply it after a diff and a dry-run; scale and restart a Deployment; delete a pod after a confirmation | yes: edit, diff, dry run, apply, a conflict and delete (`TestKindEditCreateConflictDelete`); scale and restart (`TestKindWorkloadLogsAttachRestartScale`, `TestKindNodeAndWorkloadActions`) | M4 |
| **Reach a service:** forward a pod or service port and open it in the browser | yes: pod and service forwards on 127.0.0.1 that follow a replaced pod (`TestKindPortForward`) | M5 |
| **Manage Helm releases in the cluster** (Lens's Helm view): list releases, read history and values, upgrade, roll back | yes: upgrade with the release's own chart or a repository's version, rollback, crash-safe through a journal (`TestKindHelmReleases`, `TestKindHelmUpgrade`, `TestKindHelmUpgradeFromRepository`); parts of Lens's behaviour not captured ([G11](gaps.md#g11-helm)) | M6 |
| **Maintain a node:** cordon, drain and uncordon with confirmations; node details | yes: a crash-safe drain as a background job (`TestKindDrainCrashFinishUncordon`, [D-63](../developer/decisions.md#d-63-long-commands-as-background-jobs)); the node drawer's metrics and its history from node-exporter (`TestNodeDrawerMetricsLines`, `TestNodeDrawerHistoryFromNodeExporterByAddress`) | M4 |
| **Use kubectl:** a terminal bound to the cluster's context and namespace | yes (`TestKindEngineWorkflows`); a shell profile that sets `KUBECONFIG` overrides it ([H-20](../security/credentials.md#h-20-shell-profiles-can-override-the-terminals-kubeconfig)) | M3 |
| A downloadable macOS build (unsigned is enough) | yes for Apple silicon: a release carries a signed, notarized disk image and `SHA256SUMS` ([releasing › the release pipeline](../developer/releasing.md#the-release-pipeline)); no build for Intel Macs; the minimum, macOS 13, untested ([G14](gaps.md#g14-packaging-signing-and-updates-macos)) | G14 |

**Result:** every workflow passes. What use in the app finds goes to
[G16](gaps.md#g16-stability-and-the-user-bug-backlog); none of its open items blocks a workflow.

## Milestones

| M | Scope | Definition of done | Status | Left |
|---|---|---|---|---|
| **M0** Skeleton | `go.mod`, `Makefile`, golangci-lint with `depguard` layering, CI ([releasing › CI](../developer/releasing.md#ci)), semantic-release, the parity and scorecard generators | `make lint test` green in CI; a forbidden import fails the lint; the generated parity pages match the hand-written checklists | 🟡 the scorecard runs by hand, not regularly on a fixed runner ([D-15](../developer/decisions.md#d-15-progress-generators)) | [G1](gaps.md#g1-workspace-ci-and-progress-generators): 0–2 h |
| **Spike** Fyne gate | Table, YAML editor, terminal and MCP control on Fyne ([spike](../developer/spike-fyne.md)) | Gate criteria S1–S7 recorded; [D-1](../developer/decisions.md#d-1-ui-toolkit) confirmed or the fallback chosen | ✅ Fyne; S5, the terminal, is re-checked on Windows and Linux with M11 | – |
| **M1** Connect | `kubeconfig` and `cluster`: discovery, merge, every authentication form, cached discovery, access reviews; `cogpit-cli contexts` and `cogpit-cli get <kind>` | Connects to kind and envtest in CI with certificate and token auth; exec plugins run once approved; fuzzed kubeconfig parsing never panics; a connection lost to sleep or the network is shown and rebuilt | 🟡 lost connections handled ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections)); missing: `kubectl oidc-login`, interactive exec plugins, the legacy `oidc` provider ([platform parity](platform-parity.md#cluster-authentication-and-connectivity)) | [G2](gaps.md#g2-cluster-connection-and-authentication): 8–26 h |
| **M2** Browse | The cache, the engine and registry with UI state, the Fyne shell, the core views, MCP `ui_state`, `resources_find`, `ui_navigate` | A 10,000-pod list meets P2–P4; an idle view costs no list requests (P9); every built-in kind the cluster serves appears; an MCP client navigates the GUI to a Service by its cluster IP | 🟡 P3 not measured ([G15](gaps.md#g15-performance-and-api-load-at-scale)); every Lens view exists, 63 of 64 partial | [G3](gaps.md#g3-cache-and-the-resource-model), [G4](gaps.md#g4-engine-and-command-registry), [G6](gaps.md#g6-resource-views): 10–63 h |
| **M3** Inspect | Logs, terminal sessions, pod shell, attach, node shell | P5 met; a terminal's `kubectl` talks to its own cluster; the node shell meets the rules in [privilege footprint](../security/privilege-footprint.md#node-shell) | 🟡 P5 met offscreen, its GPU frames not measured (G15); the terminals have no theme or font family of their own | [G8](gaps.md#g8-logs-terminal-and-shells): 7–25 h |
| **M4** Change | Editor with diff and server-side dry run, create from templates, delete with confirmation, workload and node actions | Every Lens Desktop action on workloads and nodes; the command fuzz target (`FuzzDispatch`) green; confirmation enforced for the CLI and agents too; an interrupted drain detected on the next start | 🟡 all 42 actions exist; Lens's dialogs not captured; the create dialogs lack fields | [G7](gaps.md#g7-resource-actions), [G9](gaps.md#g9-yaml-editor-and-create-resource): 13–52 h |
| **M5** Reach | Port forwarding: manager, dialog, view | Forward, open in the browser, stop; listeners on `127.0.0.1` only | 🟡 Lens's status texts and Edit dialog not captured; `portforward.edit` runs no access review | [G10](gaps.md#g10-port-forwarding): 2–6 h |
| **M6** Helm | Helm SDK in-process: releases from the cache; install, upgrade, rollback, uninstall as crash-safe jobs; repositories and charts | The gate's Helm workflow passes on kind; a release left pending by a crash is offered for completion or rollback | 🟡 no OCI registries; Lens's chart views and Install and Upgrade tabs not captured | [G11](gaps.md#g11-helm): 2–8 h |
| **Alpha (macOS)** | M0–M6 to the depth the gate needs, a downloadable macOS build | The [alpha gate](#alpha-gate) passes and ready for real work is in the ~40–75% band | ✅ | – |
| **M7** Metrics | Provider detection, the cluster Overview, charts | Metrics on kind with metrics-server and with Prometheus; no provider is a state, not an error; polling stops when nothing visible needs it | 🟡 the drawer's metrics ⋮ menu; a namespace-limited user's pod history ([D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)); Lens's Overview not captured | [G12](gaps.md#g12-metrics-and-cluster-overview): 1–4 h |
| **M8** Interface parity | Clusters, hotbar, palette, menus, shortcuts, preferences, cluster settings, themes, deep links | Every free Lens Desktop row in [ui-parity](ui-parity.md) done | 🟡 119 of 122 free rows exist, most partial; missing: several windows, Reload, untrusted certificate authorities | [G5](gaps.md#g5-app-shell-navigation-tables-and-drawer), [G13](gaps.md#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences): 37–131 h |
| **M9** MCP control | Every command an MCP tool, dock tools, the GUI confirmation flow, the audit log, the stdio bridge `cogpit-cli mcp` | An external agent completes the triage and change workflows through MCP, every mutating step confirmed in the GUI | ✅ [`TestKindAgentTriageAndChangeOverMCP`](../../internal/testkit/integration/mcpworkflows_kind_test.go) | – |
| **M10** macOS distribution | Signed universal app in a notarized disk image, Homebrew cask, Sparkle 2 with an EdDSA-signed appcast, update notice and one-click update ([D-4](../developer/decisions.md#d-4-packaging-and-updates)) | Installs from the disk image and updates itself on a clean Mac without warnings | 🟡 Apple silicon only; no Homebrew cask | [G14](gaps.md#g14-packaging-signing-and-updates-macos): 12–30 h + human |
| **GitOps** Flux CD and Argo CD | Kind profiles, dashboards, the Flux actions, Argo CD's Sync and Refresh ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) | A Flux and an Argo CD installation on kind show their objects with status; Sync and Refresh work with confirmation | 🟡 on kind with the definitions and no controllers (`TestKindFlux`, `TestKindArgoCD`), Argo CD also against a live controller (`TestRealArgoCD`); Lens's views not captured | [G23](gaps.md#g23-flux-cd-and-argo-cd-views): 8–22 h |
| **Beta (macOS)** | M7–M10, the GitOps views, ~75% of views and actions, budgets P1–P11 met ([G15](gaps.md#g15-performance-and-api-load-at-scale)), the first user bug backlog ([G16](gaps.md#g16-stability-and-the-user-bug-backlog)) | Ready for real work ≥ ~75% on macOS; no gap marked *blocks beta* | ⬜ | ~220–600 h in all + testers |
| **M11** Windows and Linux | ConPTY, X11 and Wayland, packages and updates per format, Ubuntu first | The alpha gate passes on Windows and on Ubuntu | 🟡 Linux: `.deb` for amd64 and arm64, Cogpit's APT repository; Windows: x64 setup, WinSparkle, the terminal on ConPTY; missing: deep links, Start at login, Authenticode, other packages | [G22](gaps.md#g22-windows-and-linux): 20–45 h + human |
| **M12** Built-in agent | Chat column, OpenAI-compatible endpoints (LM Studio, LiteLLM, Open WebUI), the tool loop with [D-2](../developer/decisions.md#d-2-agent-interface-scope)'s confirmation rules | The agent answers questions about the selected object, navigates the UI and runs a confirmed action, against a LiteLLM and an Open WebUI endpoint | 🟡 no live endpoint in CI (`TestLiveEndpoint` skips); Open WebUI tested against the fake endpoint only | [G21](gaps.md#g21-built-in-agent): 2–6 h + human |
| **M13** Paid-plan features | Security Center, Cost Monitoring ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features), [D-5](../developer/decisions.md#d-5-extensions)); the cloud account integrations are not a target | Each matches its Lens Desktop rows | ⬜ | [G18](gaps.md#g18-paid-lens-desktop-features): 110–220 h + human |

M11 and M12 are independent of each other.

## Current focus

Ranked toward beta; each item links to its gap. Pick from the top unless an issue is assigned to
you. *Human* marks what an agent cannot finish alone.

1. **Depth of the core gaps** ([G1](gaps.md#g1-workspace-ci-and-progress-generators) to
   [G4](gaps.md#g4-engine-and-command-registry), [G6](gaps.md#g6-resource-views) to
   [G11](gaps.md#g11-helm)): their open items, and each partial row checked against Lens and in a
   real window.
2. **Interface parity** (M8: [G5](gaps.md#g5-app-shell-navigation-tables-and-drawer),
   [G13](gaps.md#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences)): the partial rows
   checked against Lens's captures and in a real window; the editor's minimap and font family.
3. **Performance at scale** ([G15](gaps.md#g15-performance-and-api-load-at-scale)): the budgets
   that need a real window (P3 and P5–P8 on the GPU renderer, P1 with a real cluster and the
   window's paint), each beside Lens on the same machine; the scorecard regularly on a fixed runner
   ([G1](gaps.md#g1-workspace-ci-and-progress-generators),
   [D-15](../developer/decisions.md#d-15-progress-generators)).
4. **Stability** ([G16](gaps.md#g16-stability-and-the-user-bug-backlog)): testers besides the owner
   on their own clusters, their reports worked off (human, calendar time); G16's known items.
5. **Metrics** (M7, [G12](gaps.md#g12-metrics-and-cluster-overview)): the drawer's metrics ⋮ menu;
   a namespace-limited user's pod history
   ([D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)).
6. **Flux CD and Argo CD** ([G23](gaps.md#g23-flux-cd-and-argo-cd-views)): checked in the app on
   clusters that run them and against Lens's views; G23's open items.
7. **macOS distribution** (M10, [G14](gaps.md#g14-packaging-signing-and-updates-macos)): the
   universal app for Intel Macs, the Homebrew cask, a clean-Mac install and update without
   warnings, macOS 13 tried (human).
8. **Captures of Lens** (human): the rows marked inferred, first the create dialogs, the navigator's
   root, the Helm Upgrade tab with a repository's chart, where Lens sets `HELM_DRIVER`, the hotbar's
   slots, a workload drawer's pods, the Flux CD and Argo CD views.

Beside the path to beta:

9. **Built-in agent** (M12, [G21](gaps.md#g21-built-in-agent)): a LiteLLM virtual key as a GitHub
   secret for an opt-in CI run of `TestLiveEndpoint` (GitHub settings change only with the owner's
   OK); a real Open WebUI to test against (human).
10. **Linux** ([G22](gaps.md#g22-windows-and-linux),
    [D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)):
    a person's check on a Linux desktop (polkit's password dialog, Restart to Update, an update
    through the Software Updater, X11 and Wayland, the arm64 package on arm64 hardware); `cogpit://`
    links and Start at login.
11. **Windows** ([G22](gaps.md#g22-windows-and-linux),
    [D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)): a
    released setup tried on Windows; Authenticode once signing material exists
    ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)); an
    install for every user (`/allusers`, through UAC); the terminal in a real window (D-1's S5);
    Windows 10, ARM64.
12. **Upstream** ([G8](gaps.md#g8-logs-terminal-and-shells),
    [D-14](../developer/decisions.md#d-14-terminal-widget),
    [D-112](../developer/decisions.md#d-112-ci-and-test-time)):
    [fyne-io/terminal#147](https://github.com/fyne-io/terminal/pull/147) (scroll support) and
    Cogpit's scrollback fixes for it ([andydotxyz/terminal#2](https://github.com/andydotxyz/terminal/pull/2))
    wait for the maintainer, then Cogpit's copy moves to upstream's master; the patches not offered
    upstream follow after the app ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)).
    [fyne#6620](https://github.com/fyne-io/fyne/pull/6620), the fix for the fonts a font cache clear
    leaves alive ([fyne#6619](https://github.com/fyne-io/fyne/issues/6619)), waits for review; once a
    Fyne release carries it, the UI test shards are measured again.
13. **Postponed and deferred:** several windows
    ([D-52](../developer/decisions.md#d-52-several-windows): model (a) is the direction, the design
    questions are open). Until the feature is taken up: OCI registries and `helm registry login`
    ([H-29](../security/credentials.md#h-29-authenticated-helm-repositories)); an offline preview of
    an agent's Helm upgrade ([H-30](../security/trust-boundaries.md#h-30-chart-provenance-is-not-verified),
    [H-31](../security/privilege-footprint.md#h-31-an-agents-helm-values-can-copy-what-lookup-reads));
    a preference allowing untrusted certificate authorities for traffic outside the cluster; the
    terminal's kubectl through the cluster's proxy; kubectl mirror presets; Sparkle's Beta channel;
    [D-103](../developer/decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)'s
    further scrolling work (frames at the display's rate), only if fast scrolling still looks uneven.

## What's next, with estimates

| Next | Scope | Estimate |
|---|---|---:|
| Beta (macOS) | M7–M10, the GitOps views in depth, the depth of the core gaps (G1–G11, G13), performance (G15), stability (G16: testers besides the owner, calendar time), the generators and the captures of Lens | ~220–600 h + testers |
| After the macOS beta | The rest of M11 ([G22](gaps.md#g22-windows-and-linux): 20–45 h), M13's paid-plan features ([G18](gaps.md#g18-paid-lens-desktop-features): 110–220 h); Teamwork only if a relay service is wanted ([G20](gaps.md#g20-teamwork-and-remote-access)) | 130–265 h, Teamwork 150–300 h |
