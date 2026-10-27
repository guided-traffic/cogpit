# Cogpit vs Lens Desktop: target-app parity

> **Target:** Lens Desktop 2026.10.51501

How close Cogpit is to Lens Desktop and how much work is left. The one-page summary is
[`ROADMAP.md`](../../ROADMAP.md), the work list [`gaps.md`](gaps.md), the checklists
[`ui-parity.md`](ui-parity.md), [`resource-parity.md`](resource-parity.md) and
[`platform-parity.md`](platform-parity.md), what Cogpit adds [`beyond-lens.md`](beyond-lens.md);
method and vocabulary: [`README.md`](README.md).

## Headline

| Number | Value | Kind |
|---|---|---|
| **Feature breadth** (does each Lens feature exist?) | **~88%** | measured by `make parity`: [breadth](#feature-breadth) |
| **Ready for real work** (could a daily Lens user on macOS switch?) | **~47%** | estimated, range 42–52: [weights](#ready-for-real-work) |
| **Mainstream practitioner** (daily user of Lens's free features) | **~59%** | estimated: depth ~70%, stability ×0.85 ([discounts](#mainstream-practitioner)) |
| **Essentials user** (connect, pods, logs, shell, edit) | **~69%** | estimated: depth ~81%, the same stability discount ([depth](#essentials-user)) |
| **Stage** | **alpha** | both [alpha conditions](README.md#stages) hold: the [alpha gate](roadmap.md#alpha-gate) passed (every gate workflow walked end to end on real clusters, no finding blocking one, [G16](gaps.md#g16-stability-and-the-user-bug-backlog)), and ready for real work is in the ~40–75% band |
| Remaining to **beta** on macOS (~75% ready, GitOps views, notarized, updates, budgets met) | **~220–600 agent-hours** | estimated: G1–G17 and G23, less a quarter of G6 and G7 and several windows, postponed ([D-52](../developer/decisions.md#d-52-several-windows)) |
| Remaining to **full parity** with Lens's free features on macOS | **~220–620 agent-hours** | estimated, the sum of G1–G16 ([gaps](gaps.md#summary)) |
| Remaining to the **full Lens target** (paid features, Windows and Linux) | **~360–900 agent-hours** | estimated: full parity plus G18, G22, G23; parts wait for decisions (G18) |
| Cogpit's targets **beyond Lens** (MCP control, built-in agent) | **~2–6 agent-hours** | estimated: G17 closed, G21 2–6 h; not part of the parity numbers |

Breadth is counted by `make parity` at every change. The readiness numbers are estimates from the
gate, the checklists and the evidence per dimension, area and feature below. The remaining hours
are sums of the [gap](gaps.md) entries: mostly the depth of rows that exist, decided work, and a
reservation for what real use finds (G16).

### Readiness by audience

| Audience | Ready % | Agent-hours to ~95% | Work that dominates |
|---|---:|---:|---|
| Full Lens target | ~31% | 360–900 | Everything below, plus paid-plan features (G18, G23), the rest of Windows and Linux (G22) |
| Mainstream practitioner (macOS) | ~59% | 220–620 | Stability and what real use finds (G16: 100–200), the interface (G5, G13: 37–131), the depth of the views and actions (G6, G7: 18–91), performance (G15: 28–66) |
| Essentials user (macOS) | ~69% | 45–150, plus what real use finds (G16) | The depth of logs, shells and the editor (G8, G9) and of the core views, actions and shell (a third of G5 to G7), the foundation left (G1, G3, G4) |

Each tier is a subset of the one above. The full Lens target has no formula of its own: ready for
real work (~47%) times 0.65, the ratio of the two at their last separate estimate, gives ~31%.

## Target measured

- **Lens Desktop 2026.10.51501** (Electron 44, Chromium 152), the stable release in Lens's update
  feeds when measured (feed date 2026-10-05). Sources: Lens's documentation at
  [docs.lenshq.io](https://docs.lenshq.io/) (`docs.k8slens.dev` redirects there), the release notes
  from 2025-10 to 2026-10, the update feeds and the pricing page.
- **Captured, not run by agents.** Rows marked *capture* come from screenshots of a running Lens
  Desktop ([G11](gaps.md#g11-helm), [G12](gaps.md#g12-metrics-and-cluster-overview)). Lens Desktop
  requires a Lens ID at first launch, so rows marked *capture needed* need a person with an
  activated install.
- **Only Lens Desktop** ([D-12](../developer/decisions.md#d-12-parity-sources)). OpenLens (source
  retired January 2024) and its fork Freelens describe an older Lens and are not sources.
- **Plans.** Lens Desktop Personal is free for individuals and organizations under $10M revenue or
  funding; Plus and Pro cost $25 per seat per month billed annually, Enterprise $50
  ([pricing](https://lenshq.io/pricing)). Twelve features are paid
  ([premium features](https://docs.lenshq.io/k8slens/premium-features/)).

## Scope

| Lens feature group | Counts toward | Why |
|---|---|---|
| Free (Personal plan) features | every number | What a typical Lens user uses daily |
| Paid features: Flux CD and Argo CD views | full Lens target; in the beta (G23) | Daily work of Cogpit's first users ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Paid features: Security Center, cost monitoring | full Lens target only, after the beta | Need data sources; not the mainstream audience's daily work |
| EKS, AKS and GKE: the paid account integrations and the cloud providers' credential plugins (`aws eks get-token`, `gke-gcloud-auth-plugin`, `kubelogin`) | nothing | Not a target ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Extensions installed from outside (paid in Lens) | nothing | Integrations are built into Cogpit's code ([D-5](../developer/decisions.md#d-5-extensions)) |
| Paid features: Hotbar, editor diff | full Lens target; built with the interface and the editor | Cheap |
| Paid Ask AI and MCP server | superseded | Cogpit's built-in agent and MCP control go further ([beyond Lens](beyond-lens.md)) |
| Vendor features: Lens ID activation, subscriptions, telemetry, surveys | nothing | They serve the vendor, not the user ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Teamwork and Cluster Connect | nothing | Needs a hosted relay service; not a target unless one is decided (G20) |
| Windows and Linux | full Lens target | macOS first ([D-7](../developer/decisions.md#d-7-language-and-platforms)); the Linux deb with its APT repository ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) and the Windows setup ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) are built, the rest with M11 (G22) |

## Feature breadth

**~88% (weighted 87.8%, measured by `make parity`).** Denominators count Lens Desktop's free
features; paid features have their own component. A row counts when it exists in some form; nine
counted rows are done (one view, eight interface rows), the rest partial.

| Component | Weight | Ours / Lens | % | Kind |
|---|---:|---|---:|---|
| Resource views | 30% | 64 / 64 | 100.0% | measured ([resource-parity](resource-parity.md)); ten of them the Gateway API views, generic until captured |
| Resource actions | 20% | 42 / 42 | 100.0% | measured ([resource-parity](resource-parity.md)) |
| Interface: layout, navigation, tables, drawer, dock, editor, logs, terminal, palette, menus, shortcuts, preferences | 25% | 119 / 122 | 97.5% | measured ([ui-parity](ui-parity.md), free rows) |
| Cluster authentication and connectivity | 10% | 7 / 10 | 70.0% | measured ([platform-parity](platform-parity.md#cluster-authentication-and-connectivity)); the cloud providers' three plugin rows not a target (D-9) |
| Platforms and packages | 5% | 9 / 13 | 69.2% | measured ([platform-parity](platform-parity.md#operating-systems-and-packages)), the Linux deb (D-108) and the Windows setup (D-111) among them |
| Paid Lens Desktop features | 10% | 3 / 10 | 30.0% | counted by hand from the [premium list](https://docs.lenshq.io/k8slens/premium-features/) without extensions (D-5) and the cloud account integrations (D-9): the editor's diff, the Flux CD and Argo CD sections (G23), partial |
| **Weighted** | 100% | | **87.8%** | |

## Ready for real work

**~47% (weighted, range 42–52, estimated).** Weights reflect what a daily Lens user depends on;
connectivity weighs as much as the whole interface because a user who cannot authenticate to
their cluster cannot use anything else. A row counts what a person walked on a real cluster above
what was tested only on kind or offscreen, and texts inferred rather than captured from Lens count
less; the conservative value of every row gives 42%, the optimistic 52%.

| Dimension | Weight | % ready | Remaining (agent-h) | Evidence | Detail |
|---|---:|---:|---:|---|---|
| Resource views and actions in depth | 28% | 45% | 18–91 | every view and action exists, one view done; cells checked against the API server's tables (D-62); core views and the gate's actions walked on real clusters, rarer views on kind only; texts mostly inferred | [resource-parity](resource-parity.md), G6, G7 |
| Interface fidelity and feel | 15% | 45% | 42–148 | 119 / 122 free rows (8 done); about half seen in a real window, the rest offscreen; Dark Cockpit departs from Lens's look by design (D-110) | [ui-parity](ui-parity.md), G5, G9, G13 |
| Cluster connectivity and authentication | 15% | 55% | 8–26 | 7 / 10 rows partial; walked on real clusters: kubeconfig files and folders, client certificates (D-75), Pinniped plugins, several clusters (D-69), Reconnect and Log in again (D-81), a waiting login (D-73), background and failed connects (D-97, D-114), sleep and wake (D-58) | [platform-parity](platform-parity.md#cluster-authentication-and-connectivity), G2 |
| Logs, terminal, shells, port forwarding | 10% | 65% | 9–31 | walked on a real cluster: follow, previous container, download, resume after sleep, selection and Follow by scrolling (D-99), pod shells, kubectl in a cluster terminal, a port forward in the browser; debug containers by agents only (D-102) | [ui-parity](ui-parity.md#logs), G8, G10 |
| Performance and API load | 10% | 40% | 28–66 | 13 of 16 budgets met offscreen against fakes (`make scorecard`; P3, P6, P7 not measured); fast scrolling at 60 frames per second, Fyne's thread check off in releases (D-103); Fyne's 60 Hz on 120 Hz displays; no comparison with Lens | [testing](../developer/testing.md#performance-budgets), G15 |
| Stability | 10% | 30% | 100–200 | every gate workflow walked on real clusters by one tester; 86 walk findings, none blocking; defects, a crash among them, still found in daily use; the thread check off in releases (D-103); race-clean tests in CI | [gaps](gaps.md#g16-stability-and-the-user-bug-backlog), G16 |
| Helm releases and metrics | 5% | 55% | 3–12 | values, upgrade, rollback and history walked on a real cluster; install and uninstall by agents only (`TestRealHelm`); charts from the Metrics Server and Prometheus, Thanos or VictoriaMetrics walked | G11, G12 |
| Platforms and distribution | 5% | 60% | 32–75 + human | macOS: a signed, notarized Apple-silicon disk image, Sparkle updates; missing: an Intel or universal app, a Homebrew cask, a try on macOS 13 (the minimum); Linux (D-108) and Windows (D-111) not in the % | [platform-parity](platform-parity.md#operating-systems-and-packages), G14, G22 |
| Paid-plan integrations | 2% | 10% | 118–242 + human | 3 / 10 partial: the editor's diff, Flux CD, Argo CD; the GitOps sections checked by agents on real clusters (`TestRealFlux`, `TestRealArgoCD`), not walked | G18, G23 |
| Foundation: workspace, cache, engine (enables every row) | – | – | 0–9 | built: the cache's kinds (scoped, covering, metadata-only, local, virtual) and indexes, access reviews, jobs, discovery on disk, CI, `make parity`, `make scorecard`, envtest; left: the definitions' schemas in memory (G3 0–4 h), the engine's walk findings (G4 0–3 h), G1 0–2 h | G1, G3, G4 |
| **Weighted** | 100% | **~47%** (42–52) | **358–900** | | |

## Mainstream practitioner

**~59% (estimated).** A daily user of Lens's free features on macOS: the weighted depth of the
areas used every week, times discounts for what stops real work.

| Area | Weight | Depth |
|---|---:|---:|
| Connect to and switch between clusters | 15% | 70% |
| Workload views and actions | 25% | 70% |
| Logs | 15% | 75% |
| Shell into containers | 10% | 75% |
| Edit and apply YAML | 10% | 65% |
| Config, network and storage views | 10% | 60% |
| Port forwarding | 5% | 80% |
| Helm releases in the cluster | 5% | 70% |
| Metrics at a glance | 5% | 60% |
| **Weighted depth** | 100% | **~70%** |

Discount: stability on real clusters ×0.85 gives **~59%**: every gate workflow was walked on real
clusters, sleep and wake included, but by one tester, and defects are still found in daily use. The
cloud providers' credential plugins carry no discount: they are not a target
([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)). Speed on large
clusters is not discounted: the demo cluster's 10,000 pods meet the budgets measured so far.

## Essentials user

**~69% (estimated).** Someone who opens Lens to look at their pods and fix one.

| Core feature | Weight | Depth |
|---|---:|---:|
| Add a kubeconfig and connect | 20% | 80% |
| Pods list with status | 15% | 85% |
| Logs of a pod | 20% | 85% |
| Shell into a pod | 15% | 80% |
| Details and events of an object | 10% | 80% |
| Edit YAML and apply | 10% | 70% |
| Delete, scale, restart | 10% | 85% |
| **Weighted depth** | 100% | **~81%** |

With the same stability discount (×0.85): **~69%**.

## Feature areas

Percent is ready for real work for the area, an estimate older than the dimensions above and not
re-assessed with them. Hours are the area's share of G6 and G7 (18–91 h for 106 views and actions,
about 0.2–0.9 h each: every view and action exists, what is left is depth), plus the named
subsystem where one exists.

| Area | Views | Actions | % | Hours |
|---|---:|---:|---:|---:|
| Cluster: overview, applications, nodes, namespaces, events | 5 | 6 | ~25% (every view and action partial) | 2–10, plus metrics (G12) |
| Workloads | 9 | 15 | ~55% (every view and target action partial; Share is not a target) | 4–21 |
| Config | 16 | 4 | ~42% (every view partial; the Resource Quota and Secret create dialogs; Config Map and Secret values saved in the drawer) | 3–16 |
| Network, Gateway API included | 17 | 5 | ~45% (every view; port forwarding with its four actions; the Gateway API views generic) | 4–19 |
| Storage | 3 | – | ~40% (every view partial) | 1–3 |
| Device resources | 4 | – | 0% (the four views partial; not re-assessed) | 1–3 |
| Helm releases and charts | 2 | 4 | ~45% (Releases with Lens's drawer, rollback, uninstall, upgrade; Charts and Install from Helm's own `repositories.yaml`; partial) | 1–5, plus Helm (G11) |
| Access control | 6 | 5 | ~35% (every view and the five create dialogs partial) | 2–10 |
| Custom resources (Definitions and the custom resource views) | 2 | – | ~65% (custom resource views done, Definitions partial) | 0–2 |
| Actions on every kind (edit, delete, copy name) | – | 3 | ~55% (all three partial; Edit and Delete follow the access reviews) | 1–3 |

## Remaining effort and calibration

**Calibration (borrowed).** The hours on this page are Opus 5.5 agent-hours. Until Cogpit has
about 100 merged pull requests of its own, estimates use PhotoCraft's calibration, from its first
1,004 (median open-to-merge 3.4 h): 2–9 agent-hours per checklist item of tool or UI depth, 8–20 h
per format or subsystem, 2–4 h per merged pull request. Here a resource view or action takes 2–6 h
to build and a small interface row 1–4 h; client-go, the kubectl libraries and the Helm SDK make
authentication, actions and Helm cheaper, the cache requirement and Cogpit's own YAML editor make
those two dearer.

**Parallelism.** About 80% of what remains parallelizes, one kind profile or command per agent;
the foundation left (G1, G3, G4: 0–9 h) is small. With about ten agents, wall-clock hours come to
roughly 0.3 times the agent-hours (a fifth serial, the rest spread), plus the human steps below; a
release needs none ([H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval)).

**Needs a human:**

- A person's walks in a real window ([roadmap › Current focus](roadmap.md#current-focus)).
- Windows code-signing material for Authenticode
  ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)).
- A look at the Linux package on a desktop: polkit's password dialog, Restart to Update, an update
  through the Software Updater.
- The go-ahead to offer the terminal fork's remaining patches upstream, after the app's own work
  ([D-14](../developer/decisions.md#d-14-terminal-widget),
  [H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input)).
- An activated Lens Desktop to capture the rows marked *capture needed*, and Lens Desktop on the
  same Mac as Cogpit to set the performance budgets.
- A LiteLLM or Open WebUI endpoint for the built-in agent (G21).

## How to re-measure

1. `make parity` counts views, actions and interface rows from the checklists and fails when a
   number on this page, `ROADMAP.md` or a checklist's summary differs from its count
   ([rules](README.md#make-parity)). `make scorecard` measures the performance and API-load
   budgets into [`scorecard.md`](scorecard.md), run by hand on a quiet machine (G1,
   [D-15](../developer/decisions.md#d-15-progress-generators)).
2. Views and actions: compare the kind-profile and command registries with
   [`resource-parity.md`](resource-parity.md); `make parity` counts an action as existing when the
   registry has its id.
3. Interface: walk the rows of [`ui-parity.md`](ui-parity.md) in the running app, with a screenshot
   as evidence for visual rows.
4. Lens side: for every Lens Desktop release after the target, read its release notes, update the
   rows they touch and move the `Target` of every progress page in one change.
5. Move an estimated number only with new evidence.
