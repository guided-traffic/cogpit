# Cogpit vs Lens Desktop: target-app parity

> **Last reviewed:** 2026-10-16 · **Last updated:** 2026-10-22 · **Change:** trivial (Interface fidelity: 8 of the free rows done, was 9, the dock's "New tab" menu replaced by two chips at the owner's request; the weighted shares unchanged); trivial (A10 made (2026-10-22): the tag ruleset, the App its only bypass, a probe tag refused; `release`'s admin bypass off; no number moves); trivial (Needs a human: the owner's approval of each release's appcast jobs dropped, they sign without it since 2026-10-22 (his choice, H-141); no number moves); minor (Cogpit for Windows, D-111: the platforms 9 of 13 package rows, 86.0% weighted, breadth ~86%; G22 20–45 h, the full Lens target ~360–900 h; the scope's Windows and Linux row; the human steps); minor (Cogpit for Linux, D-108: the platforms 8 of 13 package rows, breadth ~86%, 85.6% weighted; G22 30–65 h, the full Lens target ~370–920 h; the scope's Windows and Linux row; the human steps); minor (the interface: 119 of 122 free rows, a log tab's text selected and copied a new row, the owner's WF36, D-99; weighted 84.8% unchanged); minor (the interface: 118 of 121 free rows, the window opening where it was closed a new row, D-95; weighted 84.8% unchanged); minor (v0.20.0 and v0.21.0 released in the night of 2026-10-16/17, their appcasts waiting for the owner's approval); minor (the platform row: v0.13.0 to v0.19.0 with files, the signed feed and the first update, 2026-10-16); minor (the interface row Lens Metrics not a target, Cogpit Metrics withdrawn by the owner 2026-10-16, D-61: 117 of 120 free rows, weighted breadth 84.8%; G12 2–7 h, the hours within their rounding); minor (Platforms: v0.14.0 and v0.15.0 notarized with their disk images on 2026-10-16); minor (hours recounted after the night run of 2026-10-15/16: alpha ~50–205, beta ~220–600, free parity ~220–620, the full Lens target ~380–940 agent-hours); minor (Foundation 3–15 h, was 7–23: G3's discovery cache on disk and memory per kind built 2026-10-16; totals not recounted); minor (the foundation row and How to re-measure: the generated checklist page, `make scorecard` and the envtest tier built 2026-10-16; no number moves); minor (the interface row Lens Metrics partial, Cogpit Metrics, G12, D-61: 118 of 121 free rows, weighted breadth 84.8%; G12 3–10 h, beta ~235–640, full parity ~240–655, the full Lens target ~395–980 h, 2026-10-16 night); minor (the Essentials user's standby and lost connections built 2026-10-16, D-58; numbers not moved); minor (Resource actions 42 / 42: the Secrets row names the drawer's Save, secret.saveData, D-59; breadth not moved); minor (beyond Lens ~2–8 h: G21 2–6 h, the owner's audit answers for the chat built in the night run); minor (AGENTS.md rule 10: checked against the code and gaps.md at HEAD; the remaining hours re-estimated from gaps.md's entries of 2026-10-15 (the lead's estimate, PhotoCraft's calibration): alpha ~65–240 h, beta ~240–650 h, free parity ~240–665 h, the full Lens target ~400–990 h, beyond Lens ~6–20 h, every dimension's hours, the audiences' hours and the feature areas' share of G6 and G7; the evidence text of the readiness, audience and area tables brought to what is built; 10 counted rows done, not 9; the human prerequisites current; verifying the cloud credential plugins out of scope until long after parity (owner, 2026-10-15); the readiness percentages and the ×0.7 discounts not changed, a decision pending with the owner); minor (Feature breadth's opening sentence brought to the table's 84.6% (it still said ~57% of 2026-10-12); beyond Lens ~3–12 h as G21's entry and ROADMAP say; no number re-measured); minor (Resource actions 41 / 41: the Config Maps row names the drawer's Save, `configMap.saveData`, the owner's F3; breadth not moved); minor (Sparkle 2 and the signed appcast built, no update key yet, D-4: interface 114 of 120, platforms and packages 6 of 13, weighted 84.2%, breadth ~84%); minor (paid features 3 of 11 with the Flux CD and Argo CD sections, partial: weighted 82.4%, breadth ~82%; the node shell and Cluster Settings › Node Shell partial: actions 39 of 39, interface 113 of 120, weighted 80.6%, breadth ~81%; Create Secret partial: actions 38 of 39; G12: the cluster's Prometheus setting, partial: interface 56 of 120, breadth ~57%; earlier: fourth measurement, by `make parity`: breadth ~56%, ready ~26%; beyond Lens re-estimated at ~20–42 h after the built-in agent's engine side) · **Target:** Lens Desktop 2026.10.51501

The authoritative answer to "how close is Cogpit to Lens, and how much work is left". The
one-page summary is [`ROADMAP.md`](../../ROADMAP.md); the ranked work list is
[`gaps.md`](gaps.md); the checklists are [`ui-parity.md`](ui-parity.md),
[`resource-parity.md`](resource-parity.md) and [`platform-parity.md`](platform-parity.md); what
Cogpit adds beyond Lens is in [`beyond-lens.md`](beyond-lens.md). Method and vocabulary:
[`README.md`](README.md).

## Headline

| Number | Value | Kind |
|---|---|---|
| **Feature breadth** (does each Lens feature exist?) | **~86%** (was ~85%) | measured by `make parity`: [breadth](#feature-breadth); 10 rows done, the rest partial |
| **Ready for real work** (could a daily Lens user on macOS switch?) | **~26%** (was ~23%) | estimated 2026-10-12: [weights](#ready-for-real-work) |
| **Mainstream practitioner** (daily user of Lens's free features) | **~25%** (was ~24%) | estimated 2026-10-12 |
| **Essentials user** (connect, pods, logs, shell, edit) | **~34%** (was ~32%) | estimated 2026-10-12 |
| **Stage** | **pre-alpha** | the [alpha gate](roadmap.md#alpha-gate) fails: every workflow exists, but none is checked end to end in the GUI on a real cluster; on the owner's real cluster the window hung after the Mac slept (2026-10-15 evening; decided, not built, [G2](gaps.md#g2-cluster-connection-and-authentication)). Verifying the cloud credential plugins is out of scope until long after parity (owner, 2026-10-15) |
| Remaining to **alpha** on macOS | **~50–200 agent-hours** | estimated, re-estimated 2026-10-15 from [gaps](gaps.md#summary) (was ~135–370 on 2026-10-12; ~110–320 in ROADMAP's rough re-estimate of 2026-10-13), plus what the owner's walks find (G16); [calibration](#remaining-effort-and-calibration) |
| Remaining to **beta** on macOS (~75% ready, GitOps views, notarized, updates, budgets met) | **~220–600 agent-hours** | estimated, re-estimated 2026-10-15 (G12 again 2026-10-16) (was ~425–995) |
| Remaining to **full parity** with Lens's free features on macOS | **~220–620 agent-hours** | estimated, re-estimated 2026-10-15 (G12 again 2026-10-16) (was ~420–1,040), the sum of G1–G16 |
| Remaining to the **full Lens target** (paid features, Windows and Linux) | **~360–900 agent-hours** | estimated, re-estimated 2026-10-21 (G22 after the Windows setup, D-111; ~370–920 from 2026-10-20 night after the Linux package, D-108; was ~380–940 from 2026-10-15 and G12's of 2026-10-16, ~620–1,450 before); several parts need owner decisions |
| Cogpit's targets **beyond Lens** (MCP control, built-in agent) | **~2–8 agent-hours** | estimated (2026-10-15 night: G21 2–6 h, the owner's audit answers for the chat built; 6–18 h with them to build; was ~3–12 after the chat column of 2026-10-14 night, ~20–42 before it, ~50–110 before its engine side); not part of the parity numbers |

Breadth is counted by `make parity` at every change (last moved 2026-10-20, the Linux package); the three readiness
numbers are the estimate of 2026-10-12, not re-assessed since (ROADMAP carries the lead's rough
~30% of 2026-10-13, which this page did not take up; a re-estimate is a decision pending with the
owner); the remaining hours were re-estimated on 2026-10-15 from the entries of
[gaps](gaps.md), which are now mostly the depth of rows that exist, decided work the owner added on
2026-10-15 and the reservation for what real use finds (G16). The fourth measurement (2026-10-12),
the first counted by `make parity` rather than by hand, moved breadth little (one action, three
interface rows, one authentication row): that run built depth the alpha gate needs rather than new
rows: Helm upgrade with Lens's drawer and dialogs, adding, pasting and syncing kubeconfigs with
credential plugins run only once allowed, the Secrets view on a full watch, the journals shared
safely between Cogpit processes, the terminal widget fuzzed on its own with twelve fixes, and ten
flaky tests whose causes included four product bugs. Readiness moved then for connectivity, Helm
and stability.

### Readiness by audience

| Audience | Ready % | Agent-hours to ~95% | Work that dominates |
|---|---:|---:|---|
| Full Lens target | ~17% | 360–900 | Everything below, plus paid-plan features (G18, G23), the rest of Windows and Linux (G22) |
| Mainstream practitioner (macOS) | ~25% | 220–620 | Stability and what real use finds (G16: 100–200), the interface (G5, G13: 39–137), the depth of the views and actions (G6, G7: 18–91), performance (G15: 28–66) |
| Essentials user (macOS) | ~34% | 45–150, plus what the walks find (G16) | Checking the flows end to end on real clusters (the owner's walks), standby and lost connections (G2, built 2026-10-16, D-58; the owner's check in a real window left), the depth of logs, shells and the editor (G8, G9) and of the core views, actions and shell (a third of G5 to G7), the foundation left (G1, G3, G4); verifying the cloud credential plugins against EKS, GKE and AKS is out of scope until long after parity (owner, 2026-10-15) |

Each tier is a subset of the one above. The hours were re-estimated on 2026-10-15 from
[gaps](gaps.md#summary) (were 620–1,450, 420–1,040 and 90–280); the percentages are the estimate
of 2026-10-12.

## Target measured

- **Lens Desktop 2026.10.51501**, the stable release in Lens's update feeds on 2026-10-11 (feed
  date 2026-10-05; the release notes, dated 2026-10-08, say it rolls out gradually); Electron 44,
  Chromium 152. Lens's documentation lives at [docs.lenshq.io](https://docs.lenshq.io/)
  (`docs.k8slens.dev` redirects there) and calls the product "Lens K8S IDE". Measured from that
  documentation, the release notes from 2025-10 to 2026-10, the update feeds and the pricing page.
- **Captured, not run by agents.** Rows marked *capture* in the checklists come from screenshots
  of a running Lens Desktop (2026-10-11 and 2026-10-12, the latter with the owner on a real
  cluster, [gaps › G11](gaps.md#g11-helm), [G12](gaps.md#g12-metrics-and-cluster-overview)). Lens
  Desktop requires a Lens ID at first launch; rows still marked *capture needed* need a look at an
  activated install (human).
- **Only Lens Desktop** ([D-12](../developer/decisions.md#d-12-parity-sources)). OpenLens, whose
  source was retired in January 2024, and its fork Freelens describe an older Lens and are not
  sources.
- **Plans.** Lens Desktop Personal is free for individuals and organizations under $10M revenue
  or funding; Plus and Pro cost $25 per seat per month billed annually, Enterprise $50
  ([pricing](https://lenshq.io/pricing)). Twelve features are paid
  ([premium features](https://docs.lenshq.io/k8slens/premium-features/)).

## Scope

| Lens feature group | Counts toward | Why |
|---|---|---|
| Free (Personal plan) features | every number | What a typical Lens user uses daily |
| Paid features: Flux CD and Argo CD views | full Lens target; built for the beta (both sections partial since 2026-10-13, G23) | Daily work for the owner's team ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Paid features: EKS, AKS, GKE integrations, Security Center, cost monitoring | full Lens target only, after the beta | Need cloud accounts and data sources; not the mainstream audience's daily work |
| Extensions installed from outside (paid in Lens) | nothing | Integrations are built into Cogpit's code ([D-5](../developer/decisions.md#d-5-extensions)) |
| Paid features: Hotbar, editor diff | full Lens target; built with the interface and the editor | Cheap |
| Paid Ask AI and MCP server | superseded | Cogpit's built-in agent and MCP control go further ([beyond Lens](beyond-lens.md)) |
| Vendor features: Lens ID activation, subscriptions, telemetry, surveys | nothing | They serve the vendor, not the user ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Teamwork and Cluster Connect | nothing, unless the owner decides | Needs a hosted relay service (G20) |
| Windows and Linux | full Lens target | macOS comes first ([D-7](../developer/decisions.md#d-7-language-and-platforms)); Linux's deb and Cogpit's APT repository brought ahead of the beta by the owner and built 2026-10-20 ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)), the Windows setup and WinSparkle on 2026-10-21, on the branch `windows-release` ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); the rest of Windows (Authenticode, ARM64, winget and Scoop) and the other Linux formats with M11 |

## Feature breadth

**~86% (weighted 86.0%, measured by `make parity`, last moved 2026-10-21 with the Windows setup; 85.6% from 2026-10-20 with the Linux package; 84.8% from 2026-10-16; ~57% on 2026-10-12 after the fourth implementation run).**
Denominators count Lens Desktop's free features; paid features have their own component. A row
counts when it exists in some form; ten counted rows are done (one view, nine interface rows), the
rest partial.

| Component | Weight | Ours / Lens | % | Kind |
|---|---:|---|---:|---|
| Resource views | 30% | 64 / 64 | 100.0% | measured ([resource-parity](resource-parity.md)); ten of them the Gateway API views, generic until captured |
| Resource actions | 20% | 42 / 42 | 100.0% | measured ([resource-parity](resource-parity.md)) |
| Interface: layout, navigation, tables, drawer, dock, editor, logs, terminal, palette, menus, shortcuts, preferences | 25% | 119 / 122 | 97.5% | measured ([ui-parity](ui-parity.md), free rows) |
| Cluster authentication and connectivity | 10% | 7 / 13 | 53.8% | measured ([platform-parity](platform-parity.md#cluster-authentication-and-connectivity)) |
| Platforms and packages | 5% | 9 / 13 | 69.2% | measured ([platform-parity](platform-parity.md#operating-systems-and-packages)); the Linux deb and Cogpit's APT repository since 2026-10-20 (D-108); the Windows setup since 2026-10-21, on the branch `windows-release` (D-111) |
| Paid Lens Desktop features | 10% | 3 / 11 | 27.3% | measured ([premium list](https://docs.lenshq.io/k8slens/premium-features/) without extensions, D-5): the editor's diff, the Flux CD and Argo CD sections (2026-10-14, G23), partial |
| **Weighted** | 100% | | **86.0%** | |

## Ready for real work

**~26% (weighted, estimated 2026-10-12, after the fourth implementation run; was ~23%; not re-assessed since, the hours and the evidence brought current on 2026-10-15).** Weights reflect what a daily Lens user depends on. Connectivity weighs as much
as the whole interface because a user who cannot authenticate to their cluster cannot use anything
else.

| Dimension | Weight | % ready | Remaining (agent-h) | Evidence | Detail |
|---|---:|---:|---:|---|---|
| Resource views and actions in depth | 28% | 29% | 18–91 | 64 of 64 views (one done) and 42 of 42 actions, the rest partial (`make parity`, 2026-10-15); Secrets' Keys and Type filled from a full watch; most texts and rules inferred, not checked against a capture | [resource-parity](resource-parity.md), G6, G7 |
| Interface fidelity and feel | 15% | 23% | 44–154 | 119 of 122 free rows (8 done, 9 until the dock's "New tab" menu became two chips on 2026-10-22; the window's place a new row since 2026-10-18, D-95, a log tab's text selected and copied since 2026-10-19, D-99; 54 of 120, 8 done, on 2026-10-12; Lens Metrics not a target since 2026-10-16); the tab bar, navigator, palette, Preferences and Cluster settings, hotbar, welcome page, editor drafts; Cogpit's own title bar on macOS not yet seen in a real window | [ui-parity](ui-parity.md), G5, G9, G13 |
| Cluster connectivity and authentication | 15% | 28% | 15–43 | 7 of 13 rows partial; kubeconfig files and sync folders (`~/.kube` by default), each read on its own; credential plugins run only once the user allowed them; TLS and proxy fields verified against test servers; on the owner's real cluster the connections lost while the Mac slept went undetected (2026-10-15, decided, not built); no cloud plugin verified (out of scope until long after parity, owner 2026-10-15) | [platform-parity](platform-parity.md#cluster-authentication-and-connectivity), G2 |
| Logs, terminal, shells, port forwarding | 10% | 47% | 9–31 | log options, logs of a workload's pods that resume a dropped stream, an application's logs in one tab, attach, port forwarding with reconnects (on kind); the terminal widget fuzzed on its own, twelve crashes, hangs and races fixed; the node shell since 2026-10-13 (D-37); kubectl downloaded and verified (D-3, D-50); the scrollback (D-56) | [ui-parity](ui-parity.md#logs), G8, G10 |
| Performance and API load | 10% | 25% | 28–66 | P2, P3, P4, P7, P8 and P9–P11 measured (2026-10-11, one Mac, no Lens comparison), P1's engine share (2026-10-14); frames on the GPU only once; the perf gate no longer hangs on a slow runner | [testing](../developer/testing.md#performance-budgets-proposed-not-measured), G15 |
| Stability | 10% | 11% | 100–200 | race-clean tests and CI on every push; ten flaky tests fixed, four of them product bugs (a UI state overwritten, rows missing related objects, a failed forward reported as started, a nil colour); journals safe between Cogpit processes; the owner's first use in a real window and on real clusters (2026-10-14 and 2026-10-15) found the About dialog's version, the missing node history, long keys in a Config Map's drawer and the hang after sleep | [gaps](gaps.md#g16-stability-and-the-user-bug-backlog), G16 |
| Helm releases and metrics | 5% | 25% | 9–29 | releases from the cache with Lens's drawer, rollback with Lens's dialog, uninstall and upgrade, both storage drivers, crash-safe on kind; charts and install from Helm's own `repositories.yaml` (2026-10-13); metrics from the Metrics Server and Prometheus together, the cluster Overview (2026-10-12 to 2026-10-14) | G11, G12 |
| Platforms and distribution | 5% | 5% | 32–75 + human | releases for Apple silicon; the signed, notarized disk image in v0.6.0 to v0.8.0 and again in v0.14.0 and v0.15.0 (built 2026-10-14; v0.9.0 to v0.13.0 got no files, notarization in a job of its own since 163664f2; v0.13.0 to v0.20.0 notarized by 2026-10-16 (v0.21.0 released that night), the signed feed served since that morning, with 0.19.0 to 0.15.0 by the evening, and the first update through it); Sparkle built and proven by that update; since 2026-10-20 night the Linux deb for amd64 and arm64 with Cogpit's APT repository and updates through apt (D-108), not in a release yet; since 2026-10-21 the Windows setup with WinSparkle on the branch `windows-release` (D-111), released after its merge; the % not re-assessed, it measures macOS | [platform-parity](platform-parity.md#operating-systems-and-packages), G14, G22 |
| Paid-plan integrations | 2% | 5% | 118–242 + human | 3 of 11 partial (the editor's diff, the Flux CD and Argo CD sections) | G18, G23 |
| Foundation: workspace, cache, engine (enables every row) | – | – | 0–9 | the cache with scoped, covering, metadata-only, local and virtual kinds, live related-objects indexes, access reviews, background jobs (2026-10-15), CI, `make parity`, the generated checklist page, `make scorecard`, the envtest tier, the discovery cache on disk and memory per kind (2026-10-16); left: the definitions' schemas held in memory (G3) and what the walks find in the engine (G4); G1 0–2, G3 0–4, G4 0–3 h | G1, G3, G4 |
| **Weighted** | 100% | **~26%** | **358–900** | | |

## Mainstream practitioner

**~25% (estimated 2026-10-12; was ~24%).** A daily user of Lens's free features on macOS. Method as in PhotoCraft:
weighted depth of the areas used every week, times discounts for what stops real work.

| Area | Weight | Depth |
|---|---:|---:|
| Connect to and switch between clusters | 15% | 45% |
| Workload views and actions | 25% | 55% |
| Logs | 15% | 55% |
| Shell into containers | 10% | 65% |
| Edit and apply YAML | 10% | 55% |
| Config, network and storage views | 10% | 55% |
| Port forwarding | 5% | 60% |
| Helm releases in the cluster | 5% | 50% |
| Metrics at a glance | 5% | 0% |
| **Weighted depth** | 100% | **~52%** |

Discounts: stability on real clusters (unproven: ×0.7) and authentication against the cloud
providers' credential plugins (unverified: ×0.7) give **~25%**. Verifying those plugins is out of
scope until long after parity (owner, 2026-10-15); the discount stays as it is until the owner
decides on the pending re-estimate. Speed on large clusters is not discounted: the demo cluster's
10,000 pods meet the budgets measured so far.

## Essentials user

**~34% (estimated 2026-10-12; was ~32%).** Someone who opens Lens to look at their pods and fix one.

| Core feature | Weight | Depth |
|---|---:|---:|
| Add a kubeconfig and connect | 20% | 60% |
| Pods list with status | 15% | 78% |
| Logs of a pod | 20% | 70% |
| Shell into a pod | 15% | 72% |
| Details and events of an object | 10% | 75% |
| Edit YAML and apply | 10% | 55% |
| Delete, scale, restart | 10% | 75% |
| **Weighted depth** | 100% | **~69%** |

With the same two discounts as above (×0.7 each): **~34%**.

## Feature areas

Percent is ready-for-real-work for the area, the estimate of 2026-10-12 (not re-assessed since);
hours are the area's share of G6 and G7 (18–91 h for 105 views and actions, about 0.2–0.9 h each:
every view and action exists, so what is left is depth; re-estimated 2026-10-15, the lead's
estimate, PhotoCraft's calibration; 2–6 h per view or action to build before), plus the named
subsystem where one exists.

| Area | Views | Actions | % | Hours |
|---|---:|---:|---:|---:|
| Cluster: overview, applications, nodes, namespaces, events | 5 | 6 | ~25% (every view partial: the cluster Overview and Applications since 2026-10-13, Nodes with its Pods table, Namespaces, Events; Cordon, Uncordon, Drain partial; the node shell partial, 2026-10-13; Create Namespace partial, 2026-10-12; an application's logs, 2026-10-15) | 2–10, plus metrics (G12) |
| Workloads | 9 | 16 | ~55% (every view, the Overview page included; all 15 actions that are targets exist, partial; Share is not a target) | 4–21 |
| Config | 16 | 3 | ~42% (16 of 16 views partial, Secrets on a full watch with Keys and Type; the Resource Quota create dialog partial, 2026-10-12; the Secret's, 2026-10-13; a Config Map's values saved in the drawer, 2026-10-15) | 3–16 |
| Network, Gateway API included | 17 | 5 | ~45% (every view; port forwarding and its four actions; the Gateway API views generic) | 4–19 |
| Storage | 3 | – | ~40% (every view partial, a claim's pods) | 1–3 |
| Device resources | 4 | – | 0% (the four views partial since 2026-10-13; the percentage of 2026-10-12, not re-assessed) | 1–3 |
| Helm releases and charts | 2 | 4 | ~45% (Releases with Lens's drawer, values and notes; rollback with Lens's dialog, uninstall, upgrade from the Helm Upgrade tab; Charts and Install from Helm's own `repositories.yaml` since 2026-10-13, partial) | 1–5, plus Helm (G11) |
| Access control | 6 | 5 | ~35% (6 of 6 views partial, Pod Security Policies since 2026-10-13; the create dialogs of Service Accounts and the four RBAC kinds partial, 2026-10-12) | 2–10 |
| Custom resources (Definitions and the custom resource views) | 2 | – | ~65% (custom resource views done, Definitions with its group filter partial) | 0–2 |
| Actions on every kind (edit, delete, copy name) | – | 3 | ~55% (all three, partial; Edit and Delete greyed by access reviews) | 1–3 |

## Remaining effort and calibration

**Calibration (borrowed).** Cogpit has no history yet. Estimates use PhotoCraft's calibration from
its first 1,004 merged pull requests (2026-09-30 to 2026-10-10, median open-to-merge 3.4 h):
2–9 agent-hours per checklist item of tool or UI depth and 8–20 h per format or subsystem, with
about 2–4 agent-hours per merged pull request. A Kubernetes resource view or action is smaller
than a Photoshop tool behaviour, so this page uses 2–6 h for those and 1–4 h for small interface
rows. Building on client-go, the kubectl libraries and the Helm SDK makes authentication, actions
and Helm cheaper than in the earlier Rust plan; the cache requirement and Cogpit's own YAML editor
make the cache and the editor more expensive. Replace the calibration with Cogpit's own after about
100 merged pull requests.

| Milestone | Scope | Opus 5.5 agent-hours |
|---|---|---:|
| **Alpha (macOS)** | The gaps marked *blocks alpha* in [gaps](gaps.md#summary): the foundation, connections, the shell, logs, shells, the editor, port forwarding and Helm (G1 to G5, G8 to G11), the core half of the views and actions (G6, G7) and G13's share (the one list of clusters, the Clusters, Application menu and shortcut rows); the seven gate workflows walked by a person (human) | **~50–200** (re-estimated 2026-10-16 night: G1 to G5 and G8 to G11, 33–126; half of G6 and G7, 9–46; G13's share, 8–29; 2026-10-15: ~65–240 with G1 to G5 and G8 to G11 at 49–163; plus what the walks find, from G16, not split before them. Was ~150–400 here and ~135–370 in the headline, both 2026-10-12, as the gate's rows plus the foundation; ~110–320 in ROADMAP's rough re-estimate of 2026-10-13) |
| **Beta (macOS)** | Alpha plus metrics, most of the interface, MCP control, the Flux CD and Argo CD views, ~75% of views and actions, a notarized app with one-click updates, performance budgets, the first bug backlog | **~220–600** (re-estimated 2026-10-16 night: G1 to G16, G17 and G23, 230–643, less the same 12–43; ~235–640 after G12 earlier that night; re-estimated 2026-10-15: G1 to G16, G17 and G23, 250–691, less a quarter of G6 and G7 and the several windows the owner postponed, 12–43; was ~440–1,020, before that ~580–1,360) |
| **Full parity (free features, macOS)** | G1–G16 | **~220–620** (re-estimated 2026-10-16 night after the night run, 222–619; G12 earlier that night, 238–656; re-estimated 2026-10-15, 242–667; was ~420–1,040 on 2026-10-12, ~430–1,060 after the third run) |
| **Full Lens target** | plus G18, G22, G23 | **~360–900** (re-estimated 2026-10-21, 358–900: G22 20–45 after the Windows setup, D-111; 2026-10-20 night, 368–920: G22 30–65 after the Linux package, D-108; 378–935 in the night of 2026-10-16/17; re-estimated 2026-10-16 night, 380–941; G12 earlier that night, 396–978; re-estimated 2026-10-15, 400–989; was ~620–1,450 on 2026-10-12, ~640–1,480 after the third run) |
| **Beyond Lens** | G17, G21 | **~2–8** (2026-10-15 night: G21 2–6, the owner's audit answers for the chat built; 6–18 with them to build; was ~3–12 after G21's re-estimate of 2026-10-14 night, ~20–42 before, ~50–110 before that) |

**Cogpit's first data point (2026-10-11), not yet a calibration.** The MVP was built in one session:
a serial skeleton by the lead, then eight agents in parallel for 18 to 45 minutes each (about
4.7 agent-hours together), then integration, review and documentation. Against the borrowed
calibration that work retired roughly 150–250 estimated agent-hours (the drop of the alpha
estimate plus the spike), but it leaves every row partial: it is the first, shallow pass over each
row, which is where agents are fastest. One session does not replace PhotoCraft's calibration;
it says the remaining estimates may be high for first passes and says nothing yet about the depth
work.

**Second data point (2026-10-11).** The second run: a serial phase by the lead (contracts, kind,
dependencies), then seven agents in parallel for 22 to 72 minutes each (about 4.8 agent-hours
together), then merging, CI, a review and documentation. It retired about 100–250 estimated
agent-hours of the free-parity total (G1, G3, G5, G6, G7, G9) and 40–180 of the alpha estimate.
As in the first run, most rows it touched are partial: the ratio says first passes are cheap, not
that depth is.

**Third data point (2026-10-11).** The third run: a serial Phase 0 by the lead (the shared
contracts: scoped, metadata-only, local and virtual kinds; the related-objects index; access
reviews in `Enabled`; item actions in the drawer; the Helm SDK in `go.mod`), then eight agents in
parallel for 36 to 86 minutes each (about 7.8 agent-hours together), then merging one branch at a
time, CI, a review and documentation. It retired about 140–340 estimated agent-hours of the
free-parity total (G2–G11, G15) and 60–120 of the alpha estimate. Merging cost more than in the
earlier runs: eight branches touched the same tables, constants and parity rows, and four tests
that passed on every branch alone turned out to be timing-dependent on CI's slower runners.

**Parallelism.** The foundation and the spike were serial: everything else builds on them. The spike
has run (D-1) and the foundation left is small (G1, G3, G4: 7–23 h, 2026-10-15), so about 80% of
what remains parallelizes, as in PhotoCraft: one kind profile or command per agent. With about ten
agents, alpha's ~50–200 agent-hours are roughly 15–60 wall-clock hours (a fifth serial, the rest
spread), plus the human steps: the owner's walks and each release's approvals. (On 2026-10-12, with
the foundation at 125–250 h, this paragraph said 220–460 wall-clock hours.)

**Needs a human (2026-10-15).** The owner's walks in a real window and of the alpha gate's
workflows on kind and a real cluster ([roadmap › Current focus](roadmap.md#current-focus) items 5
and 6); nothing of A10 (made 2026-10-22; the appcast jobs in `sparkle` sign without his approval since 2026-10-22, [H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval))
(a GitHub setting made only with his OK; [gaps › G14](gaps.md#g14-packaging-signing-and-updates-macos));
the owner's go-ahead to offer the terminal fork's patches upstream, at the end
([D-14](../developer/decisions.md#d-14-terminal-widget), [H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input));
D-4 decided for Linux (2026-10-20, [D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) and Windows (2026-10-21, [D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) ([decisions](../developer/decisions.md)); a person's look at the Linux package on a desktop (polkit's password dialog, Restart to Update, an update through the Software Updater); the owner's approval of the pull request that merges the branch `windows-release`, and Windows code-signing material for Authenticode ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode));
an activated Lens Desktop to capture the rows marked *capture needed*; Lens Desktop on the same Mac
as Cogpit to set the performance budgets; a LiteLLM or Open WebUI endpoint for the built-in agent.
No longer needed: Apple's signing material (2026-10-14) and Sparkle's update key (2026-10-15)
exist; EKS, GKE and AKS accounts only once verifying their credential plugins is in scope (out of
scope until long after parity, owner 2026-10-15).

## How to re-measure

1. `make parity` counts views, actions and interface rows from the checklists and fails when a
   number on this page, `ROADMAP.md` or a checklist's summary differs from its count
   ([rules](README.md#make-parity)); `make scorecard` (since 2026-10-16) measures the performance
   and API-load budgets and writes [`scorecard.md`](scorecard.md), run by hand on a quiet machine
   (G1, [D-15](../developer/decisions.md#d-15-progress-generators)).
2. Views and actions: compare the kind-profile and command registries with the tables in
   [`resource-parity.md`](resource-parity.md); `make parity` counts an action as existing when the
   registry has its id.
3. Interface: walk the rows of [`ui-parity.md`](ui-parity.md) in the running app, with a screenshot
   as evidence for visual rows.
4. Lens side: for every Lens Desktop release after the target, read its release notes, update the
   rows they touch and move the `Target` of every progress page in one change.
5. Move an estimated number only with new evidence, and add a revision-history row.

## Revision history

| Date | Change | Summary |
|---|---|---|
| 2026-10-22 | trivial | Interface fidelity: 8 of the free rows done, was 9: the dock's "New tab" menu partial since the owner replaced it with a terminal chip and the create chip; the weighted shares unchanged |
| 2026-10-22 | trivial | A10 made (2026-10-22): the tag ruleset, the App its only bypass, a probe tag refused; `release`'s admin bypass off; no number moves |
| 2026-10-22 | trivial | Needs a human: the owner's approval of each release's appcast jobs dropped, they sign without it since 2026-10-22 (his choice, H-141); no number moves |
| 2026-10-21 | minor | Cogpit for Windows (D-111, built 2026-10-21 on the branch `windows-release`): the platforms and packages 9 of 13 (the Windows row partial), 69.2% of the dimension, weighted breadth 86.0% (was 85.6%), the headline ~86% unchanged; G22 re-estimated 20–45 h (the lead's estimate: the setup, WinSparkle, the terminal on ConPTY and the first run of every test on Windows took about one subsystem, 8–20 h at the borrowed calibration; was 30–65), so the full Lens target ~360–900 h (358–900) and the platforms' readiness row 32–75 h; the scope's Windows and Linux row; D-4 decided for Windows among the human steps, the merge's approval and Authenticode's material added |
| 2026-10-20 | minor | Cogpit for Linux (D-108, built 2026-10-20 night): the platforms and packages 8 of 13 (the Linux row and the APT and RPM repositories' partial), 61.5% of the dimension, weighted breadth 85.6% (was 84.8%), the headline ~86%; G22 re-estimated 30–65 h (the lead's estimate: the deb, Cogpit's APT repository and the updates through apt built, one subsystem at the borrowed 8–20 h, less what no desktop has shown yet; was 40–80), so the full Lens target ~370–920 h (368–920), the platforms dimension 42–95 h and the readiness table's total the full target's sum, 368–920 (it said 400–989, the sum of 2026-10-15); the scope's Windows and Linux row; the human steps; readiness not re-assessed (it measures macOS) |
| 2026-10-19 | minor | The interface's new free row, a log tab's text selected and copied (partial, the owner's WF36, D-99): 119 of 122 free rows, 97.5% of the dimension, weighted breadth 84.8% unchanged |
| 2026-10-18 | minor | The interface's new free row, the window opening where it was closed (partial, the owner's WF11, D-95): 118 of 121 free rows, 97.5% of the dimension, weighted breadth 84.8% unchanged |
| 2026-10-16 | minor | The interface row Lens Metrics not a target (the owner withdrew Cogpit Metrics, D-61): 117 of 120 free rows, weighted breadth 84.8%; G12 re-estimated to 2–7 h, the headline hours unchanged within their rounding |
| 2026-10-16 | minor | Hours recounted after the night run of 2026-10-15/16 (the lead's estimate): alpha ~50–200 (G1 to G5 and G8 to G11 33–126, half of G6 and G7 9–46, G13's share 8–29), beta ~220–600, free parity ~220–620, the full Lens target ~380–940 agent-hours; readiness not moved |
| 2026-10-16 | minor | The foundation row and How to re-measure: the generated checklist page, `make scorecard` (D-15) and the envtest tier (D-64) built; no number moves |
| 2026-10-15 | minor | Beyond Lens ~2–8 h (was ~6–20 h): G21 2–6 h, the owner's audit answers for the chat (send all tools, the `http://` warning and a CA file, F9) built in the night run |
| 2026-10-15 | minor | Resource actions 42 / 42 (was 41 / 41): the Secrets row names the drawer's Save, `secret.saveData` (the owner's audit answer, D-59); breadth not moved |
| 2026-10-16 | minor | The Essentials user's standby and lost connections built 2026-10-16 (D-58); no number moves |
| 2026-10-16 | minor | The interface row Lens Metrics partial (Cogpit Metrics, D-61, G12): 118 of 121 free rows, weighted breadth 84.8%; G12 re-estimated to 3–10 h: beta ~235–640 h, full parity ~240–655 h, the full Lens target ~395–980 h |
| 2026-10-16 | minor | Foundation 3–15 h (was 7–23): G3's discovery cache on disk and memory per kind built in the night run, G3 0–4 h; totals not recounted |
| 2026-10-15 | minor | AGENTS.md rule 10, checked against the code and gaps.md at HEAD: the remaining hours re-estimated from gaps.md's entries of 2026-10-15 (the lead's estimate, PhotoCraft's calibration): alpha ~65–240 h (was ~135–370 in the headline and ~150–400 in the effort table, both 2026-10-12; now the gaps marked blocks alpha, half of G6 and G7 and G13's share, plus what the walks find), beta ~240–650 (~425–995), free parity ~240–665 (~420–1,040), the full Lens target ~400–990 (~620–1,450), beyond Lens ~6–20 (~3–12); every dimension's hours as the sums of its gaps (400–989 in all), the audiences' hours, the feature areas' hours as their share of G6 and G7; the evidence of the readiness, audience and area tables brought to what is built (64 of 64 views, 41 of 41 actions, 117 of 121 interface rows, Helm charts and install, metrics, the signed disk image, the Flux CD and Argo CD sections, jobs, no pasted kubeconfigs); 10 counted rows done, not 9; the headline's heading without its date; Lens's captures described as they are; the human prerequisites current (Apple's material and Sparkle's key exist); verifying the cloud credential plugins out of scope until long after parity (owner, 2026-10-15). The readiness percentages and the ×0.7 discounts not changed, a decision pending with the owner |
| 2026-10-15 | minor | Feature breadth's opening sentence said ~57% (2026-10-12) under a table that adds up to 84.6%: brought to the table; beyond Lens ~3–12 h in the headline and the effort table, as G21's entry (3–10 h since the night run) and ROADMAP say. Nothing re-measured; the readiness figures here (~26%) and in ROADMAP (~30%) and the remaining hours (alpha ~135–370 h here, of 2026-10-12; ~110–320 h in ROADMAP, the lead's rough re-estimate of 2026-10-13, which this page did not take up) still differ, left for the next re-estimate with its evidence per dimension |
| 2026-10-15 | minor | Resource actions 41 / 41 (was 40 / 40): the Config Maps row names the drawer's Save, `configMap.saveData` (the owner's F3, D-55); breadth not moved |
| 2026-10-15 | minor | Resource actions 40 / 40 (was 39 / 39): the Applications row names its own command, `application.logs` (A81); breadth not moved |
| 2026-10-14 | minor | Sparkle 2 and the signed appcast built, no update key yet (D-4): interface 114 of 120 (the update channel), platforms and packages 6 of 13 (the update notice, one-click update, channels, signed manifest), weighted 84.2%, breadth ~84% |
| 2026-10-14 | minor | Paid features 3 of 11 (the Flux CD and Argo CD sections partial, night build, G23): weighted 82.4%, breadth ~82% |
| 2026-10-13 | minor | The node shell (D-37) and Cluster Settings › Node Shell partial (night build, G7, G8, G13): resource actions 39 of 39, interface 113 of 120 free rows, weighted 80.6%, breadth ~81% |
| 2026-10-13 | minor | Create Secret partial (night build, G9): resource actions 38 of 39, weighted 79.9%, breadth ~80% |
| 2026-10-12 | minor | G12 (fourth slice): the cluster's Prometheus setting partial (a dialog from the drawer's status line), the interface 56 of 120 free rows, weighted 56.7%, breadth ~57% |
| 2026-10-12 | minor | Recount after the fifth run (lead): the interface 55 of 120 free rows (CPU and Memory columns from metrics-server), weighted 56.5% |
| 2026-10-12 | major | Fourth measurement after the fourth implementation run, counted by `make parity`: breadth ~56% (55 of 64 views, 29 of 39 actions, 54 of 120 interface rows after D-21, 4 of 13 connectivity rows), ready ~26%, mainstream ~25%, essentials ~34% (estimated); alpha ~135–370 h, free parity ~420–1,040 h |
| 2026-10-12 | minor | `make parity` reproduces the breadth table (55 / 64, 28 / 39, 52 / 121, 3 / 13, 1 / 13; 54.5% weighted) and guards it in CI; how to re-measure |
| 2026-10-11 | major | Third measurement after the third implementation run: breadth ~54% (measured: 55 of 64 views, 28 of 39 actions, 52 of 121 interface rows, 3 of 13 connectivity rows), ready ~23%, mainstream ~24%, essentials ~32% (estimated); alpha ~150–400 h, free parity ~430–1,060 h |
| 2026-10-11 | major | Second measurement after the second implementation run: breadth ~30% (measured), ready ~14%, mainstream ~14%, essentials ~28% (estimated); remaining hours re-estimated from the gaps |
| 2026-10-11 | major | First measurement after the local MVP: breadth ~12% (measured), ready ~10%, mainstream ~8%, essentials ~23% (estimated); remaining hours re-estimated from the gaps |
| 2026-10-11 | major | Re-estimated for Go, client-go, the kubectl libraries and the Helm SDK; macOS first; sources narrowed to Lens Desktop; beyond-Lens targets separated |
| 2026-10-11 | major | First version: target, scope, weights, audiences and a zero baseline before any code |
