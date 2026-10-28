# Where Cogpit falls short of Lens

> **Target:** Lens Desktop 2026.10.51501

Every open shortfall against Lens Desktop 2026.10.51501 and against Cogpit's own targets beyond
Lens, ranked by impact. Take the highest-ranked item nobody works on; when you close one, remove it
here, adjust the estimate and update the checklist row it points to
([rules](README.md#rules-for-every-progress-page)).

## Summary

| # | Gap | Kind | Impact | Estimate | Parity doc |
|---|---|---|---|---:|---|
| G1 | Workspace, CI and progress generators | foundation | blocks alpha | 0–2 h | [roadmap](roadmap.md#milestones) |
| G2 | Cluster connection and authentication | connectivity | blocks alpha | 8–26 h | [platform-parity](platform-parity.md#cluster-authentication-and-connectivity) |
| G3 | Cache and the resource model | foundation | blocks alpha | 0–4 h | [architecture §5](../developer/architecture.md#5-cache) |
| G4 | Engine and command registry | foundation | blocks alpha | 0–3 h | [architecture §4](../developer/architecture.md#4-the-ui-seam-the-most-important-interface) |
| G5 | App shell, navigation, tables and drawer | UI | blocks alpha | 9–35 h | [ui-parity](ui-parity.md#layout) |
| G6 | Resource views | features | blocks alpha (core kinds) | 10–56 h | [resource-parity](resource-parity.md) |
| G7 | Resource actions | features | blocks alpha (core actions) | 8–35 h | [resource-parity](resource-parity.md) |
| G8 | Logs, terminal and shells | features | blocks alpha | 7–25 h | [ui-parity](ui-parity.md#logs) |
| G9 | YAML editor and create resource | features | blocks alpha | 5–17 h | [ui-parity](ui-parity.md#editor-and-create-resource) |
| G10 | Port forwarding | features | blocks alpha | 2–6 h | [ui-parity](ui-parity.md#port-forwarding) |
| G11 | Helm | features | blocks alpha | 2–8 h | [resource-parity](resource-parity.md#helm) |
| G12 | Metrics and cluster overview | features | blocks beta | 1–4 h | [ui-parity](ui-parity.md#cluster-overview-and-charts) |
| G13 | Clusters, hotbar, palette, menus, shortcuts and preferences | UI | partly alpha, rest beta | 28–96 h | [ui-parity](ui-parity.md#clusters) |
| G14 | Packaging, signing and updates (macOS) | platforms | blocks beta (M10) | 12–30 h | [platform-parity](platform-parity.md#operating-systems-and-packages) |
| G15 | Performance and API load at scale | performance | blocks beta | 28–66 h | [testing](../developer/testing.md#performance-budgets) |
| G16 | Stability and the user bug backlog | stability | blocks beta | 100–200 h | this page |
| G17 | MCP control | beyond Lens | nothing open (M9 done) | 0 h | [beyond-lens](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| G18 | Paid Lens Desktop features | features | low-medium | 110–220 h + human | [resource-parity](resource-parity.md#paid-lens-desktop-views) |
| G19 | External extensions | ecosystem | not a target ([D-5](../developer/decisions.md#d-5-extensions)) | – | [decisions D-5](../developer/decisions.md#d-5-extensions) |
| G20 | Teamwork and remote access | ecosystem | not a target by default | 150–300 h + hosted service | [decisions D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features) |
| G21 | Built-in agent | beyond Lens | M12 (in progress) | 2–6 h + human | [beyond-lens](beyond-lens.md#built-in-agent) |
| G22 | Windows and Linux | platforms | rest after the macOS beta (M11) | 20–45 h + human | [platform-parity](platform-parity.md#operating-systems-and-packages) |
| G23 | Flux CD and Argo CD views | features | blocks beta ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) | 8–22 h | [resource-parity](resource-parity.md#paid-lens-desktop-views) |

Estimates are agent-hours (one agent, sequential, tests included) on PhotoCraft's calibration:
2–9 h per checklist item, 8–20 h per subsystem, 0.25–1 h per partial row (its check against a
capture or a real window, and the fixes). **Human** marks what an agent cannot finish alone. Row
counts are those of the checklist a section links.

Totals, the sums of the table: Lens's free feature set on macOS (G1–G16) **~220–620 h**; the macOS
beta also needs the Flux CD and Argo CD views (G23); with Lens's paid features, Windows and Linux
(G18, G22, G23) the full Lens target is **~360–900 h**. Cogpit's own targets beyond Lens (G17,
G21) add **~2–6 h**. G19 and G20 are not targets and not counted.

## Foundation gaps

### G1. Workspace, CI and progress generators

- **Scorecard:** the performance budgets are measured by hand (`make scorecard` on a quiet machine,
  [`Makefile`](../../Makefile)), not regularly on a fixed runner as
  [D-15](../developer/decisions.md#d-15-progress-generators) wants; CI's shared runners would
  measure their own load.
- **Estimate:** 0–2 h.

### G2. Cluster connection and authentication

- **Shell variables:** variables only a shell profile sets (`AWS_PROFILE`, `KREW_ROOT`) do not
  reach credential plugins; Cogpit appends program folders to `PATH` only
  ([`toolpath.go`](../../internal/cluster/toolpath.go) `EnsureToolPath`,
  [H-6](../security/credentials.md#h-6-exec-plugins-inherit-the-environment)). 2–6 h.
- **File dialogs and the ↻:** on Windows and Linux, Sync uses Cogpit's own file browser (one item
  at a time) and other choosers Fyne's dialogs, not the system's
  ([D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access), M11); Lens's
  ↻ beside a failed connect's ⚠ is missing ([ui parity › Clusters](ui-parity.md#clusters)). 1–4 h.
- **Lost connections ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections)):** off
  macOS there is no wake or network-change signal; the heartbeat and broken streams alone find an
  interruption ([`connhints_other.go`](../../internal/ui/fyneui/connhints_other.go)). No end-to-end
  test loses the connection during a drain or a Helm action (unit level:
  `TestAnOperationStopsAtTheInterruption`, `TestDrainJobStopsWhenInterrupted`).
- **Renewed certificate files:** a rejected login reconnects by itself once its kubeconfig holds
  another ([D-81](../developer/decisions.md#d-81-reconnect-and-what-log-in-again-does)), but a
  certificate file in a place macOS protects counts by its path only (`stampOf` in
  [`loginfile.go`](../../internal/cluster/loginfile.go)): its renewal needs Reconnect
  (`cluster.reconnect`) or Log In Again (`cluster.login`).
- **Cancelled logins on Windows:** the plugin's process is found by its program's file name
  ([`pluginproc_windows.go`](../../internal/cluster/pluginproc_windows.go),
  [D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound)); a `.bat`
  or `.cmd` plugin runs as `cmd.exe`, is not found, and a cancel only stops waiting for it.
- **Not checked against the real thing:** a forward proxy and a cloud cluster's own CA (1–3 h); an
  OIDC login program with its browser, cancelled and failing (the tests use a program that waits).
- **Not captured:** Lens's dialogs for adding kubeconfigs; Cogpit's are inferred.
- **Security gaps:** [H-4](../security/credentials.md#h-4-auth-provider-token-refresh) (the legacy
  `oidc` provider's refreshed tokens stay in memory, `memoryPersister`; not decided),
  [H-101](../security/trust-boundaries.md#h-101-a-credential-plugin-runs-again-during-a-connection-without-being-checked-again)
  (client-go renews a credential without the approval being checked again),
  [H-118](../security/credentials.md#h-118-what-a-cancelled-login-leaves-running) (a cancelled
  plugin's children run on).
- **Rows:** [platform parity](platform-parity.md#cluster-authentication-and-connectivity): 7
  partial (2–7 h), 3 missing (`kubectl oidc-login`, interactive plugins, the legacy `oidc`
  provider).
- **Estimate:** 8–26 h.

### G3. Cache and the resource model

- **Definitions held whole:** the custom kinds' watch keeps every custom resource definition with
  its OpenAPI schema ([`customkinds.go`](../../internal/engine/customkinds.go)), though the custom
  kinds need only names, versions, scope and printer columns. Candidate, not decided: drop the
  schemas in a trimmed scope ([`trim.go`](../../internal/cache/trim.go)), the Definitions view
  reading whole objects. 2–4 h.
- **Estimate:** 0–4 h (none if the schemas stay).

### G4. Engine and command registry

- **Long Create:** a Create of several documents (`object.create`) writes them within its call,
  not as a background job: a long manifest shows nothing until it is done
  ([D-63](../developer/decisions.md#d-63-long-commands-as-background-jobs)).
- **Estimate:** 0–3 h, for engine fixes found in use (the rest is
  [G16](#g16-stability-and-the-user-bug-backlog)'s).

## Feature gaps

### G6. Resource views

- **Depth:** every view exists, 63 of 64 partial ([resource parity](resource-parity.md) marks the
  inferred parts per row). Missing: metrics columns, and texts, status rules and drawers checked
  against a Lens capture, also how Lens shows a custom resource as an event's object or an owner.
- **Not captured:** whether and where Lens's workload drawers list their pods, as Cogpit's do
  ([D-40](../developer/decisions.md#d-40-a-pods-table-in-the-workloads-drawers)).
- **Estimate:** 10–56 h.

### G7. Resource actions

No action is missing: 42 of 42 exist ([resource parity](resource-parity.md)).

- **Lens not captured:** its drain, scale and confirmation dialogs and its eight create dialogs.
  Cogpit's fields, texts and drain defaults are inferred and unchecked, as is which actions take a
  selection (Delete, Restart, the Flux CD and Argo CD actions, at most 500 objects; Scale and the
  rest one object).
- **Create dialogs lack fields:** labels and annotations (all but the Secret's), a cluster role's
  aggregation rule, resource names in the window's rule rows, a quota's scopes
  ([resource parity › Create dialogs](resource-parity.md#create-dialogs)).
- **Unfinished-drains banner:** says "cordoned and partly drained" also when the drain evicted no
  pod: the drain journal keeps no eviction count
  ([`drains.go`](../../internal/ui/fyneui/drains.go) `drainDetail`,
  [`drainjournal.go`](../../internal/engine/drainjournal.go) `PendingDrain`).
- **Node shell:** Lens's privileged profile only; an unprivileged, `kubectl debug node/…`-style
  profile is a later option of [D-37](../developer/decisions.md#d-37-the-node-shell). Not built.
- **Estimate:** 8–35 h.

### G8. Logs, terminal and shells

[Logs](ui-parity.md#logs) and [Terminal and shells](ui-parity.md#terminal-and-shells): 18 rows, 17
partial; the scrollback is done ([D-56](../developer/decisions.md#d-56-the-terminals-scrollback)).
Triaging a failing pod (logs, then a shell) is the most common Lens workflow.

- **Terminal theme and font family:** the terminals have none of their own; their colours follow
  the app's theme (2–6 h).
- **A log stream's first request:** it names the pod, not the container instance, so a rescan from
  a cache that replays an interruption's restarts one by one could show a newer instance's lines
  twice; not reproduced ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections);
  1–2 h).
- **The 17 partial rows:** each checked against Lens and in a real window (0.25–1 h each).
- **Upstream:** the terminal fork's patches not offered upstream
  ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)) go last, after the app
  ([D-14](../developer/decisions.md#d-14-terminal-widget); under 1 h).
- **Estimate:** 7–25 h.

### G9. YAML editor and create resource

[Editor and create resource](ui-parity.md#editor-and-create-resource): 5 rows, all partial.

- **Lens not captured:** its diff view (paid), its Secret drawer and what its Create resource tab's
  save and search buttons do; Cogpit's versions are inferred (the create dialogs:
  [G7](#g7-resource-actions)).
- **The 5 partial rows:** each checked against Lens (0.25–1 h each).
- **Estimate:** 5–17 h.

### G10. Port forwarding

[Port forwarding](ui-parity.md#port-forwarding): 3 rows, 2 done, 1 partial. The Port Forwarding
view and its actions count in G6 and G7.

- **Lens not captured:** the status texts of its Port Forwarding view, and its Edit dialog;
  Cogpit's are inferred.
- **Edit without an access review:** `portforward.edit`
  ([`commands/portforward.go`](../../internal/engine/commands/portforward.go)) checks that the
  forward exists and its cluster can forward, not that the user may still forward to the pod
  behind it.
- **Real window:** that a port link opens the browser is tested offscreen only, up to Fyne's
  `OpenURL`.
- **Estimate:** 2–6 h.

### G11. Helm

Helm's views and actions count in G6 and G7.

- **OCI registries:** `oci://` repositories and `helm registry login` are not supported; such an
  entry in Helm's `repositories.yaml` is listed as not used
  ([`repofile.go`](../../internal/helm/repofile.go) `newRepository`,
  [ui parity › Preferences](ui-parity.md#preferences); 2–6 h).
- **Lens not captured** (up to 2 h of fixes after the captures):
  - the Charts view (icon column, search hint, order), a chart's drawer (README, version picker,
    icon) and the Helm Install tab (fields, default release name and namespace, what Install does
    first);
  - where its Preferences show `HELM_DRIVER`, and its Helm charts settings;
  - the Upgrade tab of a release whose chart a repository holds (its labels, the picker's order
    and first version, prereleases, several repositories);
  - a release's drawer and Rollback dialog: what Rollback does with the current revision
    selected, what Save does with "User-supplied values only" cleared, where that checkbox sits,
    the texts of states other than Deployed and Failed, what the values editor's Replace does with
    a regular expression's groups.
- **Not targets:** a list of public repositories to pick (Cogpit adds no repository,
  [D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml));
  Helm's SQL storage driver (its releases live outside the cluster; the preference refuses it).
- **Estimate:** 2–8 h.

### G12. Metrics and cluster overview

- **The drawer's metrics ⋮ menu:** not built; Lens's entries are not captured (0.5–1 h once
  captured).
- **The Overview's Pods chart drops for a namespace-limited user** where the cache's counts take
  over: the history from kube-state-metrics counts the whole cluster
  ([`promclusterpods.go`](../../internal/metrics/promclusterpods.go)), the cache only the namespaces
  the user reads. Not built: continuing the history from kube-state-metrics for such a user, which
  removes the drop (1–2 h;
  [D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)).
- **Inferred, to check against a capture of Lens:** the Overview's layout, its Master/Worker toggle
  and default group, the Issues' rule and look; the charts' range values beyond 1h and their Network
  and Filesystem series; the entries of Cluster Settings › Metrics › Hide metrics from the UI, check
  rows where Lens has a multi-select; whether Lens's claim and volume drawers chart the volume's
  usage ([D-new](../developer/decisions.md#d-new-a-volumes-usage-in-its-claims-and-its-own-drawer)).
- **Short charts are smoothed:** every `rate()` spans at least four minutes, sized for a one-minute
  scrape (`rateWindow` in [`promql.go`](../../internal/metrics/promql.go)); reading the scrape
  interval from Prometheus would sharpen the 5–15 minute charts on a faster scrape.
- **A Thanos or VictoriaMetrics store of several clusters** is read unfiltered: the queries carry no
  cluster label, so the Overview's sums and pod counts take in every cluster
  ([D-71](../developer/decisions.md#d-71-thanos-and-victoriametrics-read-as-prometheus)).
  Auto-detection on a cluster without a Prometheus and Services an operator made are not checked
  against a real installation.
- **Alertmanager** (beyond Lens, not in the estimate; [beyond Lens › Alerts](beyond-lens.md#alerts)):
  Mimir's and Cortex's multi-tenant Alertmanager (a tenant header) and one behind an authenticating
  proxy are not read; a higher-ranked lookalike Service wins over the cluster's own
  ([H-121](../security/privilege-footprint.md#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)).
- **Not targets:** chart ranges beyond 30 days (Lens's Overview: 2 months); installing a metrics
  stack (Lens Metrics,
  [D-61](../developer/decisions.md#d-61-cogpit-metrics-the-bundled-metrics-stack)); Alertmanager
  silences, a write
  ([D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)).
- **Rows:** [ui-parity › Cluster overview and charts](ui-parity.md#cluster-overview-and-charts);
  [ui-parity › Cluster settings](ui-parity.md#cluster-settings), Metrics provider and Hide metrics
  from the UI.
- **Estimate:** 1–4 h.

### G23. Flux CD and Argo CD views

- **Flux CD's views are inferred:** their columns, drawers, dashboard and actions are not checked
  against Lens's, which are not captured (2–6 h after a capture).
- **The Flux dashboard shows no events** of the Flux objects
  ([`fluxdashboard.go`](../../internal/engine/fluxdashboard.go)) (1–4 h).
- **Flux's Helm Charts** (`helmcharts.source.toolkit.fluxcd.io`, which Helm releases make) appear
  only as generic custom resources (`fluxRefKey` in [`flux.go`](../../internal/model/kinds/flux.go));
  a curated profile: 2–4 h.
- **Argo CD's views are not checked against Lens's,** which are not captured; their columns follow
  Lens's documentation.
- **Argo CD's Sync with prune asks twice:** the Sync dialog, then the destructive confirmation
  (`openArgoSyncDialog` in [`argocd.go`](../../internal/ui/fyneui/argocd.go)); Lens's Sync dialog
  asks once. Argo CD's two items: 3–8 h.
- **Rows:** [resource-parity › Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views),
  Flux CD and Argo CD.
- **Estimate:** 8–22 h.

## UI gaps

### G5. App shell, navigation, tables and drawer

[Layout](ui-parity.md#layout), [Cluster sidebar](ui-parity.md#cluster-sidebar),
[Tables](ui-parity.md#tables), [Detail drawer](ui-parity.md#detail-drawer) and
[Dock](ui-parity.md#dock): 41 rows, 4 done, 37 partial (one paid: the hotbar), none missing.

- **The 37 partial rows:** each checked against Lens's captures and in a real window; each row's
  Status names what differs from Lens or is inferred (0.25–1 h each).
- **Estimate:** 9–35 h.

### G13. Clusters, hotbar, palette, menus, shortcuts and preferences

[Clusters](ui-parity.md#clusters), [Hotbar](ui-parity.md#hotbar),
[Command palette](ui-parity.md#command-palette), [Application menu](ui-parity.md#application-menu),
[Keyboard shortcuts](ui-parity.md#keyboard-shortcuts), [Preferences](ui-parity.md#preferences),
[Cluster settings](ui-parity.md#cluster-settings) and
[Appearance and application](ui-parity.md#appearance-and-application): 60 rows, 1 done, 54 partial,
5 missing; 12 more are not a target.

- **Several windows** (missing): more than one Cogpit window, and a hotbar tile dragged out of the
  window opening its cluster in a new one
  ([beyond Lens › Everyday use](beyond-lens.md#everyday-use)). Postponed: the direction is
  chosen, the design questions are open
  ([D-52](../developer/decisions.md#d-52-several-windows)).
- **Untrusted certificate authorities for non-cluster traffic** (missing): not built; whether to
  offer it is open, as it turns off the certificate check of the Helm repository and kubectl
  downloads (0–2 h).
- **Reload** (⌘R, missing): not bound. The views are watch-fed, and a reload could only list
  again, which the cache-first rule forbids.
- **Paid, missing:** the Lens CLI switch (Preferences); the cost source (Cluster settings, with
  [G18](#g18-paid-lens-desktop-features)'s Cost Monitoring).
- **Zoom:** Lens's steps are not captured; Cogpit's follow VS Code's
  ([D-134](../developer/decisions.md#d-134-the-windows-zoom)).
- **Editor preferences:** no minimap and no font family (2–8 h).
- **Update channel:** Stable only; Beta once Cogpit publishes prereleases.
- **A cluster's proxy** (Cluster Settings › Proxy) does not reach the local terminal's kubectl:
  the terminal's kubeconfig ([`TerminalConfig`](../../internal/kubeconfig/terminal.go)) and
  environment ([`localEnv`](../../internal/terminal/local.go)) carry no proxy.
- **Elsewhere:** the terminal's theme and font family are [G8](#g8-logs-terminal-and-shells)'s;
  Start at login, `cogpit://` links and Minimize on Windows and Linux are
  [G22](#g22-windows-and-linux)'s; Cluster settings' two metrics rows are
  [G12](#g12-metrics-and-cluster-overview)'s.
- **The 52 partial rows** (without the two metrics rows): each checked against Lens's captures and
  in a real window (0.25–1 h each).
- **Estimate:** 28–96 h.

## Platform gaps

### G14. Packaging, signing and updates (macOS)

[platform-parity](platform-parity.md#operating-systems-and-packages), the macOS and update rows.

- **Missing** ([D-4](../developer/decisions.md#d-4-packaging-and-updates)): a build for Intel Macs
  (a release carries `Cogpit-<version>-macos-arm64.dmg` only), a Homebrew cask, an SBOM, and a
  signature on `SHA256SUMS` ([H-61](../security/updates.md#h-61-sha256sums-is-not-signed)).
- **macOS 13 untested:** it is the declared minimum
  ([releasing › the minimum macOS](../developer/releasing.md#the-minimum-macos)); CI's macOS job
  runs on `macos-latest`.
- **Estimate:** 12–30 h.

### G22. Windows and Linux

[platform-parity](platform-parity.md#operating-systems-and-packages), the Windows, Linux, APT and
update rows.

- **Deep links and Start at login** (both): macOS only
  ([D-47](../developer/decisions.md#d-47-deep-links-cogpit),
  [D-48](../developer/decisions.md#d-48-start-at-login)); on Linux a link starts a second process,
  which needs a hand-over to the running Cogpit
  ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)).
- **Window place** (both): only the size and full screen come back; Fyne has no window position
  ([D-95](../developer/decisions.md#d-95-the-window-comes-back-where-it-was)).
- **Minimize** (both): missing from the Window menu; Fyne's window has no minimise call
  ([Application menu](ui-parity.md#application-menu)).
- **Next and Previous Tab** (both): Ctrl+Alt+→ and Ctrl+Alt+←, not Lens's Ctrl+PageUp and
  Ctrl+PageDown ([Keyboard shortcuts](ui-parity.md#keyboard-shortcuts)).
- **File dialogs** (both, [G2](#g2-cluster-connection-and-authentication)): Fyne's dialogs label
  their buttons in the system's language while Cogpit is English; on Windows the Sync Files and
  Folders browser lists no other drive (a typed path reaches one).
- **Terminal in a real window** (both): D-1's S5 re-check, the terminal widget with copy and paste
  on Windows and on Linux (X11, Wayland) ([D-1](../developer/decisions.md#d-1-ui-toolkit)).
- **Linux packages:** `.deb` only; no AppImage, rpm, tar.xz or Flatpak, no RPM repository, no
  Flathub.
- **Linux desktop, untried:** polkit's password dialog, Restart to Update, an update through the
  Software Updater, X11 and Wayland sessions, the arm64 package on arm64 hardware.
- **Windows signatures:** the setup, `cogpit.exe` and `cogpit-cli.exe` carry no Authenticode
  signature
  ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode));
  `WinSparkle.dll` is its maintainer's unsigned build
  ([H-140](../security/updates.md#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)).
- **Windows packages:** the x64 setup only (`make dist-windows` builds for the machine it runs on,
  the release job's is x64); no winget, no Scoop.
- **Windows date and time:** the region settings are not read; ISO 8601 unless `LC_ALL`,
  `LC_TIME` or `LANG` names a locale ([`system_other.go`](../../internal/locale/system_other.go)).
- **Windows terminal:** Enforce kubectl (Preferences › Kubernetes) has no effect in PowerShell and
  cmd ([`kubectl.go`](../../internal/terminal/kubectl.go)).
- **Windows paths:** `~\` is not the home folder in the agent's CA file field
  ([`caFilePath`](../../internal/ui/fyneui/preferences_agent.go)) or a cluster's terminal working
  directory ([`checkTerminalDir`](../../internal/engine/clustersettings.go)); only `~/` is.
- **Windows proxy:** the Update button reads the feed through Go's HTTP client, which follows
  `HTTPS_PROXY`, not Windows' proxy settings
  ([`winsparkle_windows.go`](../../internal/update/winsparkle_windows.go)).
- **Windows lint:** golangci-lint and GoSec never see `internal/ui/fyneui`'s Windows file
  ([`power_windows.go`](../../internal/ui/fyneui/power_windows.go)): its package needs cgo, which
  their Windows pass runs without ([releasing › Lint and GoSec per system](../developer/releasing.md#lint-and-gosec-per-system)).
- **Windows, untried:** an install for all users through UAC, PowerShell 7, Windows 10, ARM64.
- **Estimate:** 20–45 h. **Human:** Authenticode signing material; a person's check of the Linux
  package on a desktop.

## Quality gaps

### G15. Performance and API load at scale

- **Against Lens:** no budget is measured beside Lens on the same machine
  ([testing › budgets](../developer/testing.md#performance-budgets)).
- **Not measured:** the window's paint in P1, GPU frames in P5, Cogpit's resident memory with three
  clusters (P6), and the time before the window in P7 (logger, engine, MCP). P3, P6 and P7 are
  outside `make scorecard` (`notMeasured` in [`main.go`](../../hack/scorecard/main.go)); its
  regular run on a fixed runner is [G1](#g1-workspace-ci-and-progress-generators)'s.
- **Start-up stall** (gate S1's reservation): on an Apple M1 Ultra each window's GL set-up stalls
  the main thread 0.42–0.56 s, and a 10,000-row table's first fill 151–193 ms (text textures).
  To try: paint the first frame before the rows arrive; warm the first screen's text textures off
  the critical frame. Each further window
  ([D-52](../developer/decisions.md#d-52-several-windows)) stalls again.
- **Impact:** large clusters are where users notice an IDE first and where it costs the API server
  most.
- **Estimate:** 28–66 h.

### G16. Stability and the user bug backlog

- **Missing:** a reserve for the bugs testers and users report on the way to beta. PhotoCraft's
  first public week brought hundreds of issues on features its tests counted as done. The known
  items below fall within it.
- **A test fails under load:** `TestConnectCommandsAnswerABackgroundConnect` fails rarely under a
  parallel test load and has not failed when run alone; cause not found.
- **`services` answering 401:** on one real cluster `services` alone answered 401 every 5 minutes
  for two hours while every other kind worked; cause not found.
- **Stray drags reach Fyne's own draggables:** a drag pressed elsewhere and moved onto a list's
  scroll bar, a text field's selection or a slider (Scale's count, the icon designer's zoom) acts on
  it, because Fyne's driver gives the drag to the draggable under the pointer, not to the one at the
  press. Cogpit's own draggables carry a guard
  ([D-123](../developer/decisions.md#d-123-a-drag-starts-where-the-button-went-down)); the fix for
  Fyne's belongs in its driver.
- **Escape does nothing** while the keyboard is on a button reached by Tab, an item of a check or
  radio group, a field of Fyne's file dialog (Windows, Linux) or colour dialog, or a selectable text
  after a click on it ([D-116](../developer/decisions.md#d-116-escape-closes-the-innermost-thing)).
- **A space after an arrow is drawn wide** in a single text (an upgrade's Chart row, a Secret's
  changed size in its save review): Fyne's symbol font has the arrow and no space, and the shaper
  keeps the space in the arrow's font. Dialog titles avoid it with two texts (`objectDialogTitle`,
  [D-104](../developer/decisions.md#d-104-dialog-fields-hints-dropdowns-and-button-outlines)).
- **Fast scrolling at 60 Hz:** Fyne draws on a fixed 60 Hz timer, not at the display's rate, so on
  a 120 Hz display the frames carry an uneven number of scroll events. Not built, and taken up only
  if fast scrolling still looks uneven: frames at the display's rate and smoothed input (a change
  to Fyne, a fork the fallback), one separator line per row instead of one per cell
  ([D-103](../developer/decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)).
- **Undo:** the drawer's Revert of a Config Map's or a Secret's values has no undo (an Edit tab's
  Revert has Undo Revert); there is no Edit menu; Fyne's own text fields redo with ⌘Y, not ⇧⌘Z
  ([D-84](../developer/decisions.md#d-84-undo-and-redo-in-the-yaml-editor)).
- **A selection dragged out of a text field loses the keyboard:** in Fyne's own text fields (the
  searches, dialog and settings fields) a drag released over another widget unfocuses the field,
  since Fyne's driver applies its unfocus rule on the release as on the press. The YAML editor takes
  the keyboard back (`yamlview.View.DragEnd`); each of Fyne's fields would need a wrapper type or a
  Fyne fix.
- **A quit can wait for Fyne or log an error:** Fyne 2.8.2's stop races itself. Its lifecycle
  queue's last event, the stop hook with the preferences' save, can miss the main loop's last drain
  and leave Fyne waiting in `Lifecycle.WaitForEvents` with the window gone; Cogpit's quit bound ends
  such a quit 5 s after Cogpit handed it to Fyne, with Cogpit's own shutdown run
  ([D-136](../developer/decisions.md#d-136-the-quit-bound)). The save also races the preferences'
  file watch, whose reload can read the file emptied for the write and log `Preferences load error:`
  (EOF). Under load in the smoke test's container 240 quits hung 3 times and logged it 27 times; the
  smoke run names both ([D-112](../developer/decisions.md#d-112-ci-and-test-time)). The fixes are
  Fyne's, filed and open: [fyne#6633](https://github.com/fyne-io/fyne/pull/6633) for the save,
  [fyne#6635](https://github.com/fyne-io/fyne/pull/6635) for the stop hook.
- **Estimate:** 100–200 h to beta.

## Beyond Lens

### G17. MCP control

Nothing open.

### G21. Built-in agent

- **No live run in CI:** [`TestLiveEndpoint`](../../internal/agent/live_test.go) skips unless
  `COGPIT_LLM_TEST_URL` is set, and no CI job sets it; a LiteLLM key for CI is decided, not built
  ([D-10](../developer/decisions.md#d-10-built-in-agent)).
- **Open WebUI:** its routes (`/api/chat/completions`, `/api/models`) are tested only against the
  fake endpoint ([`fakellm.go`](../../internal/testkit/fakellm/fakellm.go)), not a real Open WebUI
  ([checklist](beyond-lens.md#built-in-agent)).
- **Estimate:** 2–6 h. **Human:** a LiteLLM virtual key for CI; an Open WebUI instance to test
  against.

## Ecosystem gaps

### G18. Paid Lens Desktop features

- **Missing:** Security Center and Cost Monitoring
  ([checklist](resource-parity.md#paid-lens-desktop-views)); Lens's Flux CD and Argo CD views are
  [G23](#g23-flux-cd-and-argo-cd-views), its Ask AI and MCP server
  [G21](#g21-built-in-agent) and [G17](#g17-mcp-control). Cogpit reads Trivy Operator's reports
  only for pods' image ratings
  ([D-78](../developer/decisions.md#d-78-trivy-operators-image-ratings)), the index a Security
  Center page would build on.
- **Impact:** high for users of Lens's paid plans, none for the mainstream audience; after the
  beta, counted toward the full target only
  ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)).
- **Estimate:** 110–220 h, an upper bound: it covers more than D-9 keeps in scope. **Human:** a
  cost data source, the owner's choice.

### G19. External extensions

- **Not a target** ([D-5](../developer/decisions.md#d-5-extensions)): Lens installs extensions as
  npm packages; Cogpit's integrations are part of its own code, and it has no JavaScript runtime to
  run Lens's. Not counted.

### G20. Teamwork and remote access

- **Not a target** unless the owner runs a relay service
  ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)): Lens's
  Teamwork spaces and Cluster Connect relay.
- **Estimate:** 150–300 h plus a hosted service; not counted.
