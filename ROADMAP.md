# Cogpit roadmap

> **Target:** Lens Desktop 2026.10.51501

How close Cogpit, a native Kubernetes IDE in Go, is to Lens Desktop 2026.10.51501, on one page.
Cogpit ships for macOS first, for Linux as a `.deb` for amd64 and arm64 from its APT repository
([D-108](docs/developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages))
and for Windows as an NSIS setup for x64 with WinSparkle's updates
([D-111](docs/developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).
Detail: [target-app parity](docs/parity/target-app-parity.md) (scope, weights, evidence),
[gaps](docs/parity/gaps.md) (the ranked work list), [roadmap](docs/parity/roadmap.md) (alpha gate,
milestones, current focus), the checklists for the [interface](docs/parity/ui-parity.md),
[resources](docs/parity/resource-parity.md), [platforms](docs/parity/platform-parity.md) and
[beyond Lens](docs/parity/beyond-lens.md), the [decisions](docs/developer/decisions.md) and the
[architecture](docs/developer/architecture.md).

**Stage: alpha.** The seven workflows of the [alpha gate](docs/parity/roadmap.md#alpha-gate)
(connect, triage a failing pod, change a workload, reach a service, manage Helm releases, maintain
a node, use kubectl) pass end to end on macOS, walked by a person on real clusters, and ready for
real work is inside the ~40–75% band alpha needs. Beta needs ready for real work ≥ ~75% and no gap
marked *blocks beta* ([stages](docs/parity/README.md#stages)).

Hours are Opus 5.5 agent wall-clock hours for one agent working sequentially, estimated with
PhotoCraft's calibration until Cogpit has its own
([calibration](docs/parity/target-app-parity.md#remaining-effort-and-calibration)); after the
serial foundation, about 80% of the work parallelizes.

## Headline numbers

| | Value | Kind |
|---|---|---|
| **Feature breadth** | **~88%** | measured by `make parity` ([components and weights](docs/parity/target-app-parity.md#feature-breadth)): 64 of 64 views (ten of them the Gateway API views, generic), 42 of 42 actions, 119 of 122 interface rows, 9 of 13 package rows, 7 of 10 authentication rows, 3 of 10 paid features (the editor's diff, Flux CD, Argo CD); 9 rows done (one view, eight interface rows), the rest partial. Not a target: pasting a kubeconfig ([D-21](docs/developer/decisions.md#d-21-pasting-a-kubeconfig)), the cloud providers' credential plugins and account integrations on EKS, GKE and AKS ([D-9](docs/developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| **Ready for real work** | **~47%** | estimated, range 42–52, by counting the alpha gate and the checklists ([weights and evidence](docs/parity/target-app-parity.md#ready-for-real-work)) |
| Remaining to **beta** (macOS, with the Flux CD and Argo CD views) | **~220–600 h** | estimated from the gap entries; plus the testers' calendar time (G16), which agent-hours do not measure |
| Remaining to **full parity** (free features, macOS) | **~220–620 h** | estimated: the sum of G1–G16 ([gaps](docs/parity/gaps.md#summary)) |
| Remaining to the **full Lens target** (paid features, Windows and Linux) | **~360–900 h** | estimated: G1–G16 with G18, G22 and G23 |
| **Beyond Lens** (MCP control of UI and cluster, built-in agent) | **~2–6 h** | estimated (G21; MCP control, G17, has nothing left); not part of the parity numbers; 65 of 77 rows done, 11 partial ([beyond Lens](docs/parity/beyond-lens.md)) |

### Readiness by audience

| Audience | Ready % | Agent-hours to ~95% | Work that dominates |
|---|---:|---:|---|
| Full Lens target | ~31% | 360–900 | Everything below, plus the paid-plan features (G18, G23) and the rest of Windows and Linux (G22) |
| Mainstream practitioner (macOS) | ~59% (depth ~70%, stability ×0.85) | 220–620 | Stability (G16), the interface (G5, G13), the depth of views and actions (G6, G7), performance (G15), metrics and Helm (G11, G12) |
| Essentials user (macOS) | ~69% (depth ~81%, stability ×0.85) | 45–150 plus what real use finds | What real use finds (G16), the depth of logs, shells and the editor (G8, G9) and of the core views |

Each audience is a subset of the one above; the percentages are estimates
([method](docs/parity/target-app-parity.md#readiness-by-audience)).

## By dimension

| Dimension | % ready | Hours to full | Doc |
|---|---:|---:|---|
| Resource views and actions | 45% (the core kinds checked by a person on real clusters, the rest by agents or on kind; most texts inferred, not captured from Lens) | 18–91 | [resource-parity](docs/parity/resource-parity.md) |
| Interface fidelity and feel | 45% (about half the rows seen in a real window, the rest offscreen only) | 42–148 | [ui-parity](docs/parity/ui-parity.md) |
| Cluster connectivity and authentication | 55% (sleep and wake, exec plugins and several clusters at once tried on real clusters) | 8–26 | [platform-parity](docs/parity/platform-parity.md#cluster-authentication-and-connectivity) |
| Logs, terminal, shells, port forwarding | 65% (logs, pod shells, kubectl and a port forward tried on a real cluster) | 9–31 | [ui-parity](docs/parity/ui-parity.md#logs) |
| Performance and API load | 40% (13 of 16 budgets measured offscreen against fakes, all met; fast scrolling at 60 frames per second; no comparison with Lens) | 28–66 | [testing](docs/developer/testing.md#performance-budgets) |
| Stability | 30% (one tester on real clusters, whose use still finds defects) | 100–200 | [gaps](docs/parity/gaps.md#g16-stability-and-the-user-bug-backlog) |
| Helm releases and metrics | 55% (upgrade, rollback and the metrics tried on a real cluster) | 3–12 | [gaps](docs/parity/gaps.md#g11-helm) |
| Platforms and distribution | 60% (measures macOS: the signed, notarized disk image and Sparkle's updates; no Intel app, no Homebrew cask) | 32–75 + human | [platform-parity](docs/parity/platform-parity.md#operating-systems-and-packages) |
| Paid-plan integrations (Flux CD and Argo CD in the beta) | 10% (3 of 10 partial: the editor's diff, Flux CD, Argo CD) | 118–242 + human | [resource-parity](docs/parity/resource-parity.md#paid-lens-desktop-views) |
| Foundation: workspace, cache, engine, CI | built in part | 0–9 | [architecture](docs/developer/architecture.md) |

## Upcoming

To beta: the [Beta milestone](docs/parity/roadmap.md#milestones) (M7–M10, the Flux CD and Argo CD
views, ~75% of views and actions, the budgets P1–P11 met, the first user bug backlog). Estimates
are the gap entries'; the ranked list is the [current focus](docs/parity/roadmap.md#current-focus).

- **Stability** (G16; 100–200 h + testers): testers using Cogpit on their own clusters and the
  first user bug backlog they find; calendar time rather than agent-hours.
- **The depth of the gaps marked *blocks alpha*** (G1–G11, part of G13; ~50–200 h + human):
  Lens's dialogs and texts captured to check the rows marked inferred (human: an activated Lens),
  the rarer views and actions tried on real clusters.
- **Interface parity** (G5, G13, M8; 37–131 h): every free Lens row of
  [ui-parity](docs/parity/ui-parity.md) done: navigation, tables, drawer, hotbar, palette, menus,
  shortcuts, preferences, cluster settings, themes, deep links.
- **Performance at scale** (G15; 28–66 h): the budgets P1–P11 met on a fixed runner and a real
  cluster (10,000 rows, 1,000 events/s, API request budgets); P3 (scrolling), P6 (memory) and P7
  (cold start) not measured, the rest measured offscreen against fakes
  ([scorecard](docs/parity/scorecard.md)); no comparison with Lens on the same Mac.
- **Metrics and the cluster Overview** (G12, M7; 1–4 h): the rest of the depth from what the
  cluster runs; Cogpit installs nothing for it
  ([D-61](docs/developer/decisions.md#d-61-cogpit-metrics-the-bundled-metrics-stack)).
- **Flux CD and Argo CD** (G23; 8–22 h): the inferred columns and actions checked against Lens's
  views, the Flux objects' events on the dashboard, a sync with prune in one dialog; neither
  section tried by a person on a cluster that runs it.
- **macOS distribution** (G14, M10; 12–30 h + human): the universal (Intel) app, the Homebrew
  cask, an SBOM and signed checksums; the minimum, macOS 13, not tried.
- **Windows and Linux** (G22, M11; 20–45 h + human; after the beta): on Linux a person's look on a
  desktop (polkit's password dialog, Restart to Update, the Software Updater, the arm64 package on
  a real machine), `cogpit://` links, Start at login, other package formats; on Windows
  Authenticode once signing material exists
  ([H-139](docs/security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)), an
  install for every user through UAC, ARM64, winget and Scoop, Windows 10.
- **Not needed for beta either:** the paid Lens features (G18, M13; 110–220 h: Security Center,
  cost monitoring); several windows
  ([D-52](docs/developer/decisions.md#d-52-several-windows): open, postponed, nothing built); the
  built-in agent run against a real LiteLLM and Open WebUI endpoint (G21; 2–6 h; human: a LiteLLM
  key for CI).
- **Upstream** (G8, [D-14](docs/developer/decisions.md#d-14-terminal-widget),
  [D-112](docs/developer/decisions.md#d-112-ci-and-test-time)): Cogpit's terminal scrollback
  offered to upstream's scroll support
  ([fyne-io/terminal#147](https://github.com/fyne-io/terminal/pull/147), through
  [andydotxyz/terminal#2](https://github.com/andydotxyz/terminal/pull/2)) and Fyne's font cache
  fix ([fyne#6620](https://github.com/fyne-io/fyne/pull/6620)), all open.
