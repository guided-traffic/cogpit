# Cogpit vs Lens Desktop: target-app parity

> **Last reviewed:** 2026-10-16 · **Last updated:** 2026-10-22 · **Change:** trivial (Logs: Follow switched by scrolling built, AW14, D-99; no estimate moves); minor (Stage: alpha, declared by the owner 2026-10-22 after the gate's re-walk and the readiness recount; the stage texts brought to it); minor (the cloud providers' credential plugins and cloud account integrations not a target (owner, 2026-10-22: won't build, D-9): the three plugin rows not a target, authentication 7 of 10, the paid features 3 of 10 (the cloud account integrations out of the premium list's count), weighted breadth 87.8%, breadth ~88%; the interface's two paid cloud rows not a target; the cloud-plugin discount removed: mainstream ~59%, essentials ~69%; the scope table, the connectivity row and the human steps); minor (the readiness recount after the owner's re-walk of the gate, the lead's estimate of 2026-10-22: ready for real work ~47% (42–52), mainstream ~42% with stability ×0.85, essentials ~48%, the full Lens target ~31%; every dimension's, area's and feature's evidence; the Interface, Connectivity and Helm rows' hours brought to the gap entries; the stage texts: the gate passed, both alpha conditions hold, alpha on the owner's word); trivial (Interface fidelity: 8 of the free rows done, was 9, the dock's "New tab" menu replaced by two chips at the owner's request; the weighted shares unchanged); trivial (A10 made (2026-10-22): the tag ruleset, the App its only bypass, a probe tag refused; `release`'s admin bypass off; no number moves); trivial (Needs a human: the owner's approval of each release's appcast jobs dropped, they sign without it since 2026-10-22 (his choice, H-141); no number moves); minor (Cogpit for Windows, D-111: the platforms 9 of 13 package rows, 86.0% weighted, breadth ~86%; G22 20–45 h, the full Lens target ~360–900 h; the scope's Windows and Linux row; the human steps); minor (Cogpit for Linux, D-108: the platforms 8 of 13 package rows, breadth ~86%, 85.6% weighted; G22 30–65 h, the full Lens target ~370–920 h; the scope's Windows and Linux row; the human steps); minor (the interface: 119 of 122 free rows, a log tab's text selected and copied a new row, the owner's WF36, D-99; weighted 84.8% unchanged); minor (the interface: 118 of 121 free rows, the window opening where it was closed a new row, D-95; weighted 84.8% unchanged); minor (v0.20.0 and v0.21.0 released in the night of 2026-10-16/17, their appcasts waiting for the owner's approval); minor (the platform row: v0.13.0 to v0.19.0 with files, the signed feed and the first update, 2026-10-16); minor (the interface row Lens Metrics not a target, Cogpit Metrics withdrawn by the owner 2026-10-16, D-61: 117 of 120 free rows, weighted breadth 84.8%; G12 2–7 h, the hours within their rounding); minor (Platforms: v0.14.0 and v0.15.0 notarized with their disk images on 2026-10-16); minor (hours recounted after the night run of 2026-10-15/16: alpha ~50–205, beta ~220–600, free parity ~220–620, the full Lens target ~380–940 agent-hours); minor (Foundation 3–15 h, was 7–23: G3's discovery cache on disk and memory per kind built 2026-10-16; totals not recounted); minor (the foundation row and How to re-measure: the generated checklist page, `make scorecard` and the envtest tier built 2026-10-16; no number moves); minor (the interface row Lens Metrics partial, Cogpit Metrics, G12, D-61: 118 of 121 free rows, weighted breadth 84.8%; G12 3–10 h, beta ~235–640, full parity ~240–655, the full Lens target ~395–980 h, 2026-10-16 night); minor (the Essentials user's standby and lost connections built 2026-10-16, D-58; numbers not moved); minor (Resource actions 42 / 42: the Secrets row names the drawer's Save, secret.saveData, D-59; breadth not moved); minor (beyond Lens ~2–8 h: G21 2–6 h, the owner's audit answers for the chat built in the night run); minor (AGENTS.md rule 10: checked against the code and gaps.md at HEAD; the remaining hours re-estimated from gaps.md's entries of 2026-10-15 (the lead's estimate, PhotoCraft's calibration): alpha ~65–240 h, beta ~240–650 h, free parity ~240–665 h, the full Lens target ~400–990 h, beyond Lens ~6–20 h, every dimension's hours, the audiences' hours and the feature areas' share of G6 and G7; the evidence text of the readiness, audience and area tables brought to what is built; 10 counted rows done, not 9; the human prerequisites current; verifying the cloud credential plugins out of scope until long after parity (owner, 2026-10-15); the readiness percentages and the ×0.7 discounts not changed, a decision pending with the owner); minor (Feature breadth's opening sentence brought to the table's 84.6% (it still said ~57% of 2026-10-12); beyond Lens ~3–12 h as G21's entry and ROADMAP say; no number re-measured); minor (Resource actions 41 / 41: the Config Maps row names the drawer's Save, `configMap.saveData`, the owner's F3; breadth not moved); minor (Sparkle 2 and the signed appcast built, no update key yet, D-4: interface 114 of 120, platforms and packages 6 of 13, weighted 84.2%, breadth ~84%); minor (paid features 3 of 11 with the Flux CD and Argo CD sections, partial: weighted 82.4%, breadth ~82%; the node shell and Cluster Settings › Node Shell partial: actions 39 of 39, interface 113 of 120, weighted 80.6%, breadth ~81%; Create Secret partial: actions 38 of 39; G12: the cluster's Prometheus setting, partial: interface 56 of 120, breadth ~57%; earlier: fourth measurement, by `make parity`: breadth ~56%, ready ~26%; beyond Lens re-estimated at ~20–42 h after the built-in agent's engine side) · **Target:** Lens Desktop 2026.10.51501

The authoritative answer to "how close is Cogpit to Lens, and how much work is left". The
one-page summary is [`ROADMAP.md`](../../ROADMAP.md); the ranked work list is
[`gaps.md`](gaps.md); the checklists are [`ui-parity.md`](ui-parity.md),
[`resource-parity.md`](resource-parity.md) and [`platform-parity.md`](platform-parity.md); what
Cogpit adds beyond Lens is in [`beyond-lens.md`](beyond-lens.md). Method and vocabulary:
[`README.md`](README.md).

## Headline

| Number | Value | Kind |
|---|---|---|
| **Feature breadth** (does each Lens feature exist?) | **~88%** (was ~86%) | measured by `make parity`: [breadth](#feature-breadth); 10 rows done, the rest partial |
| **Ready for real work** (could a daily Lens user on macOS switch?) | **~47%** (was ~26%) | estimated 2026-10-22 by the lead after the gate's re-walk, range 42–52: [weights](#ready-for-real-work) |
| **Mainstream practitioner** (daily user of Lens's free features) | **~59%** (was ~25%) | estimated 2026-10-22: depth ~70%, stability ×0.85 ([discounts](#mainstream-practitioner)) |
| **Essentials user** (connect, pods, logs, shell, edit) | **~69%** (was ~34%) | estimated 2026-10-22: depth ~81%, the same stability discount |
| **Stage** | **alpha** (declared by the owner 2026-10-22) | the [alpha gate](roadmap.md#alpha-gate) passed: the owner walked every workflow end to end on real clusters (2026-10-17 to 2026-10-20) and re-walked every fix that touches it, each passed (2026-10-22; findings AW1–AW19 none blocking, [G16](gaps.md#g16-stability-and-the-user-bug-backlog), [D-114](../developer/decisions.md#d-114-the-alpha-walks-decisions-a-failed-connects-page-and-its-stages)); ready for real work, ~47% (estimated), is inside the ~40–75% band. Both alpha conditions hold; the owner declared alpha on 2026-10-22. |
| Remaining to **alpha** on macOS | **none by the stage rule** (2026-10-22) | both alpha conditions hold since 2026-10-22 (the gate passed, ready for real work in the band); alpha reached, declared by the owner the same day. The ~50–200 agent-hours estimated 2026-10-15 from [gaps](gaps.md#summary) for the depth of the gaps marked *blocks alpha* count toward beta from here; [calibration](#remaining-effort-and-calibration) |
| Remaining to **beta** on macOS (~75% ready, GitOps views, notarized, updates, budgets met) | **~220–600 agent-hours** | estimated, re-estimated 2026-10-15 (G12 again 2026-10-16) (was ~425–995) |
| Remaining to **full parity** with Lens's free features on macOS | **~220–620 agent-hours** | estimated, re-estimated 2026-10-15 (G12 again 2026-10-16) (was ~420–1,040), the sum of G1–G16 |
| Remaining to the **full Lens target** (paid features, Windows and Linux) | **~360–900 agent-hours** | estimated, re-estimated 2026-10-21 (G22 after the Windows setup, D-111; ~370–920 from 2026-10-20 night after the Linux package, D-108; was ~380–940 from 2026-10-15 and G12's of 2026-10-16, ~620–1,450 before); several parts need owner decisions |
| Cogpit's targets **beyond Lens** (MCP control, built-in agent) | **~2–8 agent-hours** | estimated (2026-10-15 night: G21 2–6 h, the owner's audit answers for the chat built; 6–18 h with them to build; was ~3–12 after the chat column of 2026-10-14 night, ~20–42 before it, ~50–110 before its engine side); not part of the parity numbers |

Breadth is counted by `make parity` at every change (last moved 2026-10-22, the cloud providers' three credential-plugin rows not a target); the three readiness
numbers were re-estimated on 2026-10-22 by the lead after the owner's re-walk of the gate, by
counting the gate and the checklists, each dimension, area and feature with its evidence below
(the lead's estimate, open to revision, D-74; before it the estimate of 2026-10-12 and ROADMAP's
rough ~30% of 2026-10-13); the remaining hours were re-estimated on 2026-10-15 from the entries of
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
| Full Lens target | ~31% | 360–900 | Everything below, plus paid-plan features (G18, G23), the rest of Windows and Linux (G22) |
| Mainstream practitioner (macOS) | ~59% | 220–620 | Stability and what real use finds (G16: 100–200), the interface (G5, G13: 39–137), the depth of the views and actions (G6, G7: 18–91), performance (G15: 28–66) |
| Essentials user (macOS) | ~69% | 45–150, plus what the walks find (G16) | What the walks found and real use finds (G16: the alpha walk's AW1–AW19, D-114), the depth of logs, shells and the editor (G8, G9) and of the core views, actions and shell (a third of G5 to G7), the foundation left (G1, G3, G4); the flows walked end to end on real clusters (2026-10-17 to 2026-10-22), standby and lost connections (G2, D-58) checked by the owner after real sleeps (2026-10-19, 2026-10-22) |

Each tier is a subset of the one above. The hours were re-estimated on 2026-10-15 from
[gaps](gaps.md#summary) (were 620–1,450, 420–1,040 and 90–280); the percentages are the lead's
estimate of 2026-10-22 (were ~17%, ~25% and ~34%, the estimate of 2026-10-12; the mainstream and
essentials ones without the cloud-plugin discount since the owner made the cloud providers' plugins
not a target the same day, ~42% and ~48% with it). The full Lens
target has no formula of its own: the ratio of 2026-10-12 (~17% of ~26%, about 0.65) applied to
~47% gives ~31%, estimated.

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
| Paid features: Security Center, cost monitoring | full Lens target only, after the beta | Need data sources; not the mainstream audience's daily work |
| EKS, AKS and GKE: the paid account integrations, and verifying or supporting the cloud providers' credential plugins (`aws eks get-token`, `gke-gcloud-auth-plugin`, `kubelogin`) | nothing | Not a target (owner, 2026-10-22: won't build, [D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Extensions installed from outside (paid in Lens) | nothing | Integrations are built into Cogpit's code ([D-5](../developer/decisions.md#d-5-extensions)) |
| Paid features: Hotbar, editor diff | full Lens target; built with the interface and the editor | Cheap |
| Paid Ask AI and MCP server | superseded | Cogpit's built-in agent and MCP control go further ([beyond Lens](beyond-lens.md)) |
| Vendor features: Lens ID activation, subscriptions, telemetry, surveys | nothing | They serve the vendor, not the user ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) |
| Teamwork and Cluster Connect | nothing, unless the owner decides | Needs a hosted relay service (G20) |
| Windows and Linux | full Lens target | macOS comes first ([D-7](../developer/decisions.md#d-7-language-and-platforms)); Linux's deb and Cogpit's APT repository brought ahead of the beta by the owner and built 2026-10-20 ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)), the Windows setup and WinSparkle on 2026-10-21, on the branch `windows-release` ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); the rest of Windows (Authenticode, ARM64, winget and Scoop) and the other Linux formats with M11 |

## Feature breadth

**~88% (weighted 87.8%, measured by `make parity`, last moved 2026-10-22 when the cloud providers' three credential-plugin rows became not a target and the cloud account integrations left the paid features' count; 86.0% from 2026-10-21 with the Windows setup; 85.6% from 2026-10-20 with the Linux package; 84.8% from 2026-10-16; ~57% on 2026-10-12 after the fourth implementation run).**
Denominators count Lens Desktop's free features; paid features have their own component. A row
counts when it exists in some form; ten counted rows are done (one view, nine interface rows), the
rest partial.

| Component | Weight | Ours / Lens | % | Kind |
|---|---:|---|---:|---|
| Resource views | 30% | 64 / 64 | 100.0% | measured ([resource-parity](resource-parity.md)); ten of them the Gateway API views, generic until captured |
| Resource actions | 20% | 42 / 42 | 100.0% | measured ([resource-parity](resource-parity.md)) |
| Interface: layout, navigation, tables, drawer, dock, editor, logs, terminal, palette, menus, shortcuts, preferences | 25% | 119 / 122 | 97.5% | measured ([ui-parity](ui-parity.md), free rows) |
| Cluster authentication and connectivity | 10% | 7 / 10 | 70.0% | measured ([platform-parity](platform-parity.md#cluster-authentication-and-connectivity)); the cloud providers' three plugin rows not a target since 2026-10-22 (D-9) |
| Platforms and packages | 5% | 9 / 13 | 69.2% | measured ([platform-parity](platform-parity.md#operating-systems-and-packages)); the Linux deb and Cogpit's APT repository since 2026-10-20 (D-108); the Windows setup since 2026-10-21, on the branch `windows-release` (D-111) |
| Paid Lens Desktop features | 10% | 3 / 10 | 30.0% | measured ([premium list](https://docs.lenshq.io/k8slens/premium-features/) without extensions, D-5, and since 2026-10-22 without the cloud account integrations, D-9, owner: won't build; 3 / 11 before): the editor's diff, the Flux CD and Argo CD sections (2026-10-14, G23), partial |
| **Weighted** | 100% | | **87.8%** | |

## Ready for real work

**~47% (weighted, range 42–52, estimated 2026-10-22 by the lead after the owner's re-walk of the gate; was ~26%, estimated 2026-10-12 before any walk; the lead's estimate, open to revision, D-74).** Weights reflect what a daily Lens user depends on. Connectivity weighs as much
as the whole interface because a user who cannot authenticate to their cluster cannot use anything
else. Since 2026-10-22 a row counts what a person walked on a real cluster above what was tested
only on kind or offscreen, and texts inferred rather than captured from Lens count less; the
conservative value of every row gives 42%, the optimistic 52%.

| Dimension | Weight | % ready | Remaining (agent-h) | Evidence | Detail |
|---|---:|---:|---:|---|---|
| Resource views and actions in depth | 28% | 45% (was 29%) | 18–91 | 64 of 64 views (one done) and 42 of 42 actions, the rest partial (`make parity`; 55 of 64 and 29 of 39 on 2026-10-12); every kind's cells checked by agents against the API server's own tables on the staging cluster (D-62, 2026-10-16); walked by the owner on real clusters 2026-10-17 to 2026-10-22: Pods, Deployments, Nodes, Services, Config Maps, Secrets, Namespaces and Helm releases, and the gate's actions (scale, restart, delete, cordon, drain, uncordon); the rarer views (Device Resources, the Gateway API's generic views, access control, storage) checked by agents or on kind only; most texts and rules inferred, not checked against a capture | [resource-parity](resource-parity.md), G6, G7 |
| Interface fidelity and feel | 15% | 45% (was 23%) | 42–148 | 119 of 122 free rows (8 done; 54 of 120 on 2026-10-12); about half of them seen by the owner in a real window in his walks (WF1–WF45, GW1–GW14, AW1–AW19), the rest offscreen only; Cogpit's own title bar in his daily use since 2026-10-16; Dark Cockpit (D-110, 2026-10-21) leaves Lens's look by the owner's choice; open from the alpha walk (G16): Escape and the focus of new dock tabs inconsistent (AW2–AW5, AW8, AW19), ⌘F in the Edit tab after about 3 s (AW6), a click below the rows after about 1 s (AW12) | [ui-parity](ui-parity.md), G5, G9, G13 |
| Cluster connectivity and authentication | 15% | 55% (was 28%) | 8–26 | 7 of 10 rows partial (4 of 13 on 2026-10-12); the hang after sleep fixed (D-58) and checked by the owner after real sleeps (2026-10-19; the log's resume re-walked 2026-10-22); walked on real clusters: kubeconfig files and a synced folder, client certificates expiring and expired (D-75), the owner's Pinniped credential plugins, several clusters at once (D-69), Reconnect and Log in again (D-81), a login that waits, cancellable and bounded (D-73), a connect going on in the background and a failed one staying (D-97, re-walked 2026-10-22); open: connect outcomes as error toasts and a failed connect's page (AW15–AW17, D-114) | [platform-parity](platform-parity.md#cluster-authentication-and-connectivity), G2 |
| Logs, terminal, shells, port forwarding | 10% | 65% (was 47%) | 9–31 | walked by the owner on the staging cluster: a followed log, a previous container's log (the gate walk), its download, its resume after a real sleep (re-walked 2026-10-22), its text selected and copied (D-99); a pod shell opened again after a reconnect; kubectl in a cluster terminal (the gate walk), its pasting and keys re-walked 2026-10-22 (GW7, GW8); a Service's port forwarded into the browser (the gate walk); debug containers checked by agents on the staging cluster only (D-102); open: a new terminal tab without the keyboard focus (AW8); Follow switched by scrolling built 2026-10-22 on a branch, not walked yet (AW14, D-99) | [ui-parity](ui-parity.md#logs), G8, G10 |
| Performance and API load | 10% | 40% (was 25%) | 28–66 | `make scorecard` (2026-10-16): 13 of 16 budgets measured, all met, offscreen against fakes (P3, P6, P7 not measured there); fast scrolling of the Pods list at 10,000 px/s from 6.3 to 60.3 frames per second (D-103, Fyne's thread check off in releases since v0.30.0), "Deutlich besseres Ansprechverhalten" (the owner, 2026-10-20); no comparison with Lens, no real cluster's round trips; Fyne draws at 60 Hz on 120 Hz displays; open: ⌘F after about 3 s (AW6), a click below the rows after about 1 s (AW12) | [testing](../developer/testing.md#performance-budgets-proposed-not-measured), G15 |
| Stability | 10% | 30% (was 11%) | 100–200 | the owner on real clusters 2026-10-17 to 2026-10-22 (the staging cluster, his own Pinniped clusters): 78 findings in his walks (WF1–WF45, GW1–GW14, AW1–AW19), none blocking a workflow; the gate passed and every fix touching it re-walked; against it: a crash on a tooltip's hover (GW14, 2026-10-20), two real defects found in his use on 2026-10-22 (a narrowed list drew no row; a view kept "Loading…" after an expired login; both fixed, not seen in a real window yet), one tester, Fyne's thread check off in releases (D-103); race-clean tests and CI on every push | [gaps](gaps.md#g16-stability-and-the-user-bug-backlog), G16 |
| Helm releases and metrics | 5% | 55% (was 25%) | 3–12 | walked by the owner in the gate walk on the staging cluster: a release's values, an upgrade from the Helm Upgrade tab, a rollback, the cluster's history agreeing; the rollback's dropdown and ⌘F re-walked 2026-10-22; install and uninstall checked by agents there only (`TestRealHelm`); metrics from the Metrics Server and Prometheus, Thanos or VictoriaMetrics, their sources, charts and a node's disks walked on the staging cluster (2026-10-18, 2026-10-19) | G11, G12 |
| Platforms and distribution | 5% | 60% (was 5%) | 32–75 + human | macOS: the signed, notarized disk image in every release since v0.13.0, updates through Sparkle's signed feed since 2026-10-16, used by the owner (0.29.3 to 0.30.0 at the gate walk's start); missing: the Intel and universal app, the Homebrew cask, macOS 13 not tried; Linux (the deb with Cogpit's APT repository, D-108) and Windows (the setup with WinSparkle in the releases since v0.37.0, D-111) not in the %: it measures macOS | [platform-parity](platform-parity.md#operating-systems-and-packages), G14, G22 |
| Paid-plan integrations | 2% | 10% (was 5%) | 118–242 + human | 3 of 10 partial (3 of 11 until the cloud account integrations left the premium list's count, 2026-10-22; the editor's diff, the Flux CD and Argo CD sections); Flux CD and Argo CD checked by agents on real clusters (`TestRealFlux`, `TestRealArgoCD`), neither walked by a person | G18, G23 |
| Foundation: workspace, cache, engine (enables every row) | – | – | 0–9 | the cache with scoped, covering, metadata-only, local and virtual kinds, live related-objects indexes, access reviews, background jobs (2026-10-15), CI, `make parity`, the generated checklist page, `make scorecard`, the envtest tier, the discovery cache on disk and memory per kind (2026-10-16); left: the definitions' schemas held in memory (G3) and what the walks find in the engine (G4); G1 0–2, G3 0–4, G4 0–3 h | G1, G3, G4 |
| **Weighted** | 100% | **~47%** (42–52; was ~26%) | **358–900** | | |

## Mainstream practitioner

**~59% (estimated 2026-10-22 by the lead; was ~25%, estimated 2026-10-12).** A daily user of Lens's free features on macOS. Method as in PhotoCraft:
weighted depth of the areas used every week, times discounts for what stops real work.

| Area | Weight | Depth |
|---|---:|---:|
| Connect to and switch between clusters | 15% | 70% (was 45%) |
| Workload views and actions | 25% | 70% (was 55%) |
| Logs | 15% | 75% (was 55%) |
| Shell into containers | 10% | 75% (was 65%) |
| Edit and apply YAML | 10% | 65% (was 55%) |
| Config, network and storage views | 10% | 60% (was 55%) |
| Port forwarding | 5% | 80% (was 60%) |
| Helm releases in the cluster | 5% | 70% (was 50%) |
| Metrics at a glance | 5% | 60% (was 0%) |
| **Weighted depth** | 100% | **~70%** (was ~52%) |

Discount: stability on real clusters ×0.85 (was ×0.7, unproven) gives **~59%**. The stability
discount is the lead's re-assessment of 2026-10-22: the owner's walks proved every gate workflow on
real clusters over six days, sleep and wake included, but with one tester and new defects still
found in his use (a crash on 2026-10-20, two defects on 2026-10-22) half of the penalty stays. The
cloud providers' credential plugins carry no discount: they are not a target (owner, 2026-10-22:
won't build, [D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)); until then a ×0.7 for them gave ~42%. Speed on large clusters is not
discounted: the demo cluster's 10,000 pods meet the budgets measured so far.

## Essentials user

**~69% (estimated 2026-10-22 by the lead; was ~34%, estimated 2026-10-12).** Someone who opens Lens to look at their pods and fix one.

| Core feature | Weight | Depth |
|---|---:|---:|
| Add a kubeconfig and connect | 20% | 80% (was 60%) |
| Pods list with status | 15% | 85% (was 78%) |
| Logs of a pod | 20% | 85% (was 70%) |
| Shell into a pod | 15% | 80% (was 72%) |
| Details and events of an object | 10% | 80% (was 75%) |
| Edit YAML and apply | 10% | 70% (was 55%) |
| Delete, scale, restart | 10% | 85% (was 75%) |
| **Weighted depth** | 100% | **~81%** (was ~69%) |

With the same discount as above (stability ×0.85): **~69%** (~48% until 2026-10-22 with the cloud
plugins' ×0.7, removed when the owner made them not a target).

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
exist; accounts on EKS, GKE and AKS, since the cloud providers' credential plugins and account
integrations are not a target (owner, 2026-10-22: won't build).

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
| 2026-10-22 | trivial | Logs, terminal, shells, port forwarding: Follow switched by scrolling built on a branch (AW14, D-99), not walked yet; no estimate moves |
| 2026-10-22 | minor | Stage: alpha, declared by the owner 2026-10-22 ("Ja, dann sind wir jetzt Alpha") after the gate's re-walk and the readiness recount; the stage texts brought to it |
| 2026-10-22 | minor | The cloud providers' credential plugins and cloud account integrations not a target (owner, 2026-10-22: won't build, D-9): the EKS, GKE and AKS plugin rows of the platform checklist not a target, authentication 7 of 10 (was 7 of 13), 70.0%; the paid features 3 of 10 (was 3 of 11), 30.0%, the cloud account integrations out of the premium list's count (D-9), and the readiness row Paid-plan integrations 3 of 10; weighted breadth 87.8% (was 86.0%), breadth ~88%; the interface's paid rows Preferences › Integrations and the navigator's cloud sections not a target (8 of 10 paid rows); the cloud-plugin discount ×0.7 removed: mainstream practitioner ~59% (was ~42%), essentials user ~69% (was ~48%); ready for real work ~47% and the full Lens target ~31% unchanged; the scope table, the connectivity row's evidence and the human steps |
| 2026-10-22 | minor | Readiness re-estimated by the lead after the owner's re-walk of the gate (2026-10-22), by counting the gate and the checklists; estimated, open to revision (D-74). Ready for real work ~47% (range 42–52; was ~26%): resource views and actions 45%, interface 45%, connectivity 55%, logs and terminal 65%, performance 40%, stability 30%, Helm and metrics 55%, platforms (macOS) 60%, paid integrations 10%, a row walked by a person counting above one tested on kind or offscreen, inferred texts below captured ones. Mainstream practitioner ~42% (was ~25%): depth ~70%, the stability discount ×0.85 (was ×0.7, the lead's re-assessment after the walks), the cloud-plugin discount ×0.7 kept, its removal the owner's pending decision (~59% without it). Essentials user ~48% (was ~34%): depth ~81%, ~69% without the cloud-plugin discount. Full Lens target ~31% (was ~17%; the ratio of 2026-10-12 applied). The Interface, Connectivity and Helm rows' hours brought to the gap entries (42–148, 8–26, 3–12; the total 358–900 already was their sum). The stage stays pre-alpha: the gate passed and ~47% is inside the ~40–75% band, so alpha is declared on the owner's word |
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
