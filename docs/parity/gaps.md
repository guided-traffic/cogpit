# Where Cogpit falls short of Lens

> **Last reviewed:** 2026-10-15 · **Last updated:** 2026-10-23 · **Change:** patch (G16: the owner's finding of 2026-10-23, a view entered drew no row until a minimal scroll, reproduced and fixed on a branch: a view entered starts at its top; no estimate moves); patch (G16: the owner's bug of 2026-10-23, another cluster's tab selected took the one preview tab over and lost its view, fixed on a branch with one preview tab per cluster, D-117; G5's inferred defaults brought to it; no estimate moves); trivial (G16: a further cluster name of the owner's replaced by a placeholder, acme-d-pinniped; the repository is public); minor (G16: the owner's re-walk of RW1–RW4 and the logo on v0.40.1, 2026-10-23, each passed in a real window; his findings CW1–CW4 open; the estimate checked, not moved); trivial (G16: RW1–RW4 and the logo merged and released in v0.40.1, PRs #86 and #87; the owner's re-walk planned for his next session); patch (G16: a race in a connect whose kubeconfig source is removed meanwhile, found in CI 2026-10-23, open); patch (G16: a colleague's blurry navigator, the owner's question of 2026-10-23: the logos drawn at the display's pixels, built; Fyne's text textures kept across a change of density, a candidate, open until the colleague's test; the estimate checked, not moved); minor (G16: RW1–RW4 built 2026-10-23 on a branch: a masked key's buttons on its name line, the field's outline on every text button, a Secret's Show in amber with an eye, the welcome page's message in a banner naming its level; the owner's re-walk open; the estimate checked, not moved); minor (G16: the owner's re-walk of AW1–AW19 on v0.40.0, 2026-10-23, each passed in a real window; his findings RW1–RW4 on a Secret's drawer and RW4 on the welcome page's message recorded, open; the estimate checked, not moved); minor (G16: the alpha walk's findings AW1–AW19 all built 2026-10-22, PRs #74 to #82, the owner's re-walk open; the estimate checked, not moved); patch (G16: AW14 built on a branch, Follow switched by scrolling, D-99; no estimate moves); patch (G16: AW11 built on a branch, a deleted namespace the filter names read on the namespaces' own changes, D-106; no estimate moves); minor (G16: AW7, AW9 and AW10 of the owner's alpha walk built 2026-10-22 on a branch, the dialogs' Context label grey, the minimised dock opened by a tab shown again, ⌘⌥⇧← and ⌘⌥⇧→ in the dock; no estimate moves); minor (G16: the owner's AW15 and AW16 of 2026-10-22 built, a cancelled connect and one left in the background raise no toast on any path; no estimate moves); minor (G16: AW13, AW17 and AW18 built on a branch, D-114: a failed connect's error page with the connect's stages, the welcome page without "Other clusters", the stray "Could not" fixed); minor (G16: AW12's welcome page rows no longer hold a click for a double click's, a second click on a cluster connecting connects nothing more; no estimate moves); minor (G16: AW12 built on a branch, a click below a list's rows closes the drawer at once, the table no `fyne.DoubleTappable`; no estimate moves); minor (G16: the alpha walk's AW1–AW4 and AW19 built on a branch, Escape closes the innermost thing and a closed dropdown is outlined, D-116 and D-104); minor (G16: the alpha walk's focus findings AW5, AW6 and AW8 built on a branch: a dock tab the user opens takes the keys, the find bar's first ⌘F repaints and focuses its field, D-115; no estimate moves); minor (the cloud providers' credential plugins and cloud account integrations not a target (owner, 2026-10-22: won't build, D-9): G2's Missing, Evidence and Human, G18's Missing, Evidence and Human, the cloud sections in G5 and G13; no estimate moves); minor (G16: the owner's alpha walk of 2026-10-22 on v0.38.0, the gate's re-walk passed, findings AW1–AW19 none blocking, his decisions in D-114; no estimate moves); minor (G16: the owner's finding of 2026-10-22 on v0.37.3 fixed, a list narrowed while scrolled down drew no row; no estimate moves); minor (G16: the owner's findings of 2026-10-22 on v0.37.0 fixed, a view not read before an expired login or an interruption and the namespaces of the cluster shown before, the drawer's header after another pod; no estimate moves); trivial (A10 made (2026-10-22): the tag ruleset, the App its only bypass, a probe tag refused; `release`'s admin bypass off; no number moves); trivial (G14's Human: the appcast jobs in `sparkle` sign without the owner's approval since 2026-10-22 (his choice, H-141); no number moves); trivial (G8: the terminal widget's upstream pull requests' state, #155 to #157 merged, #158's requested changes pushed, waiting for review); minor (G22: Cogpit for Windows built on the branch `windows-release`, D-111; what is left on Windows; re-estimated 20–45 h, the full Lens target ~360–900 h; G1, G2 and G14's Windows lines); patch (the external MCP server opt-in, D-49, noted beside A15's build; no gap moves); minor (G22: the Linux deb, Cogpit's APT repository and the updates through apt built 2026-10-20 night, D-108; what is left on Linux and Windows; re-estimated 30–65 h, the full Lens target ~370–920 h; G14's Linux line); minor (G16: GW13, no tab of a cluster without a connection; GW14, the crash on the foot's ?); minor (G16: GW1–GW12 built 2026-10-20, GW10 to GW12 from the owner's use the same day; the namespace filter's 100-row limit real and fixed); minor (G16: the owner's gate walk of 2026-10-20, findings GW1–GW9, not built; no estimate moves until the readiness recount); minor (G16: fast scrolling's proposal 1 in releases since v0.30.0; no estimate moves); minor (G16: fast scrolling's proposal 1 merged after the owner's look of 2026-10-20, in releases from the next one on; no estimate moves); minor (G16: fast scrolling's proposal 1 built on the branch `fast-scroll`, D-103: Fyne's thread check off in releases by a build parameter, the audit; no estimate moves); patch (G8: a log opened again under a name another pod or container instance holds now, checked and fixed: no line twice, a `[cogpit]` line where the log does not go on from its last line; no estimate moves); minor (G16: the owner's fast scrolling that jumps, measured: Fyne's thread check and its 60 Hz timer; his decisions on the proposals, not built); minor (G16: the owner's clicks on a pod that only checked it, as if ⌘ were held, fixed: a click takes ⌘ and ⇧ from its own mouse event; no estimate moves); minor (G16: a colleague's pod with one container square too many after the namespace filter, fixed: a list's cell with new content laid out again; no estimate moves); minor (G16: WF43 built, a Terminating namespace's blocking objects and Remove finalizers, D-98; no estimate moves); minor (G16: WF36 and WF41 built, D-99: a log tab's and a toast's text selected and copied; no estimate moves); minor (G16: the owner's walk of 2026-10-19 on v0.26.1, his findings WF31–WF45, not built yet; no estimate moves); minor (G16: the owner's acme-mgmt, the service proxy's port by its number first (D-96), fixed); minor (G12: the source row centred, the owner's correction of WF24); minor (G12: a node's disks as a bar per real filesystem, the Nodes list's Disk the fullest (WF26, D-90); the charts' line as one path (WF23, D-89)); minor (G12: the source row under the charts with links to the settings only when needed, the provider settings on the integrations' own sections of Cluster Settings, D-87 and D-88; estimate not moved); minor (G16: WF15, an object's YAML in the Kubernetes order, built); minor (G16: WF13, WF14, WF16, WF19, the dialogs as tables, no raw resourceVersion, the CSV's name, built); minor (G16: WF20, a namespace filter carried to another cluster, fixed); minor (G13: the scope dropdown's bank note marked gone (D-57, D-83)); minor (G12: a cluster without a choice of integrations asks no metrics until its question's pre-choice is answered, the owner's WF5 of 2026-10-17, D-82; no estimate moves); minor (G13: one hotbar and the drag that carries the tile, the owner's WF7 and WF6 of 2026-10-17, D-83; the banks' two paid rows not a target; no estimate moves); minor (G16: the owner's WF9 of 2026-10-17, undo and redo in the YAML editor, built, D-84; no estimate moves); minor (G16: the owner's WF8 of 2026-10-17, a selection dragged out of the YAML editor lost the keyboard, its cause in Fyne's driver, fixed in the editor; Fyne's own text fields left as they are, the lead's decision; no estimate moves); minor (G2 and G16: the owner's F28 and F29 of 2026-10-17, a renewed client certificate Log in again could not use, its cause, and Reconnect from the kubeconfig file with the tabs kept, built, D-81; no estimate moves); minor (G13 and G16: the owner's F25, F26 and F27 of 2026-10-17, a cluster's uploaded icon on its tabs, a cluster's accent colour (the setting, D-80, H-125) and copy icons by its name and kubeconfig path, built; no estimate moves); minor (G18: Kyverno's policy violations on the objects, D-79, built 2026-10-17, are Cogpit's own, not Lens's Security Center, which reads Trivy's reports; no estimate moves); minor (G12: the cluster's alerts from its Alertmanager, the title bar's pill and the alert board, built 2026-10-17, D-77, beyond Lens and not counted in the gap; what is left of them; no estimate moves); minor (G12: the Metrics source kept as the two metrics integrations' switches, D-76, the owner's words of 2026-10-17; Cluster Settings › Integrations and the question at a new cluster's first connect, rows of beyond-lens; estimate not moved); minor (G16: the owner's F23 and F24 of 2026-10-17, a running container that is not ready orange in the Pods list and "+N" for the squares a narrow Containers cell cannot show, fixed; no estimate moves); minor (G18: Trivy Operator's reports read for a pod's image ratings, the owner's I6 of 2026-10-17, D-78; the gap stands); minor (v0.22.0 released 2026-10-17 08:14 UTC, its appcast waiting for the owner's approval with v0.20.0's and v0.21.0's); minor (G13 and G16: the owner's F22 of 2026-10-17, a context's client certificate, the time left or "expired", in the cluster lists, built, D-75; no estimate moves); minor (G5 and G16: the namespace filter's search field, a click on a name filtering to it alone and a click on a box adding it, the owner's F21 of 2026-10-17, built; estimate not moved); minor (G16: the owner's finding F20 of 2026-10-17, a node condition ending in Ready read as a problem when True, fixed; estimate not moved); minor (v0.20.0 and v0.21.0 released in the night of 2026-10-16/17, their appcasts waiting for the owner's approval); minor (G2: a login that waits built 2026-10-16 night, D-73, the owner's finding: Cancel Connecting ends a connect under way and its credential plugin, every plugin run bounded by 5 minutes, `waitingForLogin`, the connect's limit below the plugin; what is missing; H-118); minor (G2 and G9: the system's file panels on macOS, D-72; estimates not moved); minor (G12, night run 2026-10-16/17: the area under every chart's series filled, the five Overview rows saying what keeps them partial; G12 re-estimated 1–4 h (was 2–7), the totals within their rounding); minor (G12, night run 2026-10-16/17: a drawer that shows only the network or the filesystem asks for no CPU and memory, built, measured on a real cluster; estimate not moved); minor (G12, night run 2026-10-16/17: the namespace-limited user's Pods history by the plan's namespaces not built, the owner's A47 standing; the Missing entry corrected; estimate not moved); minor (G12, night run 2026-10-16/17: Thanos and VictoriaMetrics found by name and named so, D-71, built; Left of Prometheus done; estimate not moved); minor (G12, night run 2026-10-16/17: the Prometheus's namespace and service as links in the source line, built; estimate not moved); minor (G12, night run 2026-10-16/17: the Prometheus setting's fields on Cluster Settings › Metrics in place of the dialog, built; estimate not moved); minor (G17 closed: every cluster command names its cluster to agents, the connection log, the user, the drawer keys and the session settings checked, M9's triage and change workflows over MCP on kind, 2026-10-16 night; beyond Lens ~2–6 h); minor (G13: the welcome page without its "Connect to Cluster…" button, the workspace's kept, the owner's answer of 2026-10-16 evening (N122), built 2026-10-16 night; estimate not moved); minor (G5: a lone checked row's menu ends with Clear Selection, the owner's answer of 2026-10-16 evening (N112), built 2026-10-16 night; estimate not moved); minor (G5 and G13: the owner's answers of 2026-10-16 evening: a lone checked row's menu gains Clear Selection; the welcome page without its "Connect to Cluster…" button, the workspace's kept; not built yet); minor (G14: v0.17.0 to v0.19.0 released 2026-10-16, the feed holds 0.19.0 to 0.15.0); minor (G5: the selection bar's greyed Delete names the reason in its tooltip, the owner's audit decision of 2026-10-16, built; estimate not moved); minor (G9: the copy icon over a Secret's editor in the drawer, the owner's audit decision of 2026-10-16, built; estimate not moved); minor (G9: keys added and removed in a Config Map's and a Secret's drawer, the owner's audit decision of 2026-10-16, built; estimate not moved); minor (G21: a chat connection's CA file replaces the system's roots, the owner's decision in the audit of 2026-10-16, N141; estimate not moved); minor (G5: each view keeps its checked rows, per tab and cluster, the owner's decision in the audit of 2026-10-16, N111, built the same day; estimate not moved); minor (G1, G2, G3, G4, G5, G9, G13: the owner's audit of the night decisions (2026-10-16) recorded, confirmed where he chose the built option; G5 and G9 gain two items he decided and that are not built, the greyed Delete's tooltip and a copy icon over a Secret's editor; G5's lone checked row's menu and G13's welcome page button to be changed, how not said yet; estimates not moved); minor (G12: Cogpit Metrics withdrawn by the owner 2026-10-16 and removed, D-61: metrics only from what the cluster runs, Lens Metrics not a target; G12 re-estimated 2–7 h (was 3–10), the totals within their rounding; G13 7 of 64 rows); minor (G5: D-69 stage 3 built 2026-10-16, a background cluster's login held while its setting says so and the last session restored at start, the tabs only with the preference on; estimate not moved); minor (G5: the window's part of several connected clusters built 2026-10-16, D-69 stage 2: the marks, switching without reconnecting, the dock per cluster; estimate not moved); minor (G5: several clusters connected at once as Lens keeps them, the engine part built 2026-10-16, D-69 stage 1; the window's part, stage 2, not built; estimate not moved); minor (G14: v0.13.0 has its disk image since a re-run); minor (G14: the disk image's window, D-70, 2026-10-16); minor (G2: the connection log built, D-66, 2026-10-16: every attempt on disk with its steps and error picture, a failed credential plugin's standard error scrubbed; the plugin's standard error no longer open, H-112; estimate not moved); minor (G2's standby entry: the states as dots, no word, blinking green while reconnecting, the owner's F13 2026-10-16; estimate not moved); minor (G14: the first update through the feed, 2026-10-16, the signed manifest done for macOS); minor (G14: the signed feed served since 2026-10-16 with 0.16.0 and 0.15.0); minor (G14: v0.14.0 and v0.15.0 notarized with files on 2026-10-16, their appcasts waiting for the owner; v0.13.0 not notarized in 330 minutes); minor (G23: Argo CD checked against a live Argo CD controller on a real cluster, 2026-10-16, night run, five findings fixed, estimate not moved); minor (the night run of 2026-10-15/16 recounted: G2 8–26 h (standby built), the free parity ~220–620 h, the full Lens target ~380–940 h; alpha ~50–200 h and beta ~220–600 h in the roadmap); minor (G4 and G7: Drain as a background job built 2026-10-16, night run, D-63: the cordon and a step per pod, the budget's wait, Cancel leaving the drain `stopped` in its journal, an agent's drain outliving its call); minor (G3: discovery cached on disk, D-65, and memory per kind and per cluster in the Diagnostics view built in the night run of 2026-10-16, measured on a real cluster; the definitions' schemas found holding most of the cache's memory, a candidate; G3 0–4 h (was 4–12), totals not recounted); minor (G6: an owner that is a custom resource links to it in the drawers and the Pods list, 2026-10-16; estimates not moved); minor (G6, G7: what a real cluster showed, 2026-10-16; estimates not moved); minor (G7: a confirmation names its objects by kind, not kind key, 2026-10-16; estimates not moved); minor (G7: a failed drain that evicted nothing no longer says "partly drained", found on a real cluster 2026-10-16; estimates not moved); minor (G6: an event's involved custom resource links to its view, found on a real cluster 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G6, G7: what a real cluster showed, 2026-10-16; estimates not moved); minor (G7: a confirmation names its objects by kind, not kind key, 2026-10-16; estimates not moved); minor (G7: a failed drain that evicted nothing no longer says "partly drained", found on a real cluster 2026-10-16; estimates not moved); minor (G6: an event's involved custom resource links to its view, found on a real cluster 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G7: a confirmation names its objects by kind, not kind key, 2026-10-16; estimates not moved); minor (G7: a failed drain that evicted nothing no longer says "partly drained", found on a real cluster 2026-10-16; estimates not moved); minor (G6: an event's involved custom resource links to its view, found on a real cluster 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G7: a failed drain that evicted nothing no longer says "partly drained", found on a real cluster 2026-10-16; estimates not moved); minor (G6: an event's involved custom resource links to its view, found on a real cluster 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G6: an event's involved custom resource links to its view, found on a real cluster 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G23: an OCI Helm repository without a status Ready as the flux CLI shows it, the dashboard's counts checked against `flux get` on a real cluster, 2026-10-16; estimates not moved); minor (G1: `make scorecard` built 2026-10-16 in the night run, D-15, the first scorecard committed, removed from Missing; G1 0–2 h (was 3–8), the totals not recounted; G15: the scorecard as a way to measure, estimate not moved); minor (G1: the envtest tier and its CI job built 2026-10-16 in the night run, D-64, removed from Missing; estimate not moved); minor (G1: the envtest tier and its CI job built 2026-10-16 in the night run, D-64, removed from Missing; estimate not moved); minor (G12: Lens Metrics as Cogpit Metrics (D-61), the provider's query format, empty-container filter and HTTPS, a node's Network and Filesystem from node-exporter, built 2026-10-16 night and tried on kind and a real cluster, P13 measured there; G12 3–10 h (was 7–21), the totals recounted with it: free parity ~240–655 h, the full Lens target ~395–980 h); minor (G4: long commands as background jobs built 2026-10-16, night run, D-63: an action on a selection of more than ten objects as a job with progress in the dock and Cancel for every origin; G7, G9 say so; estimates not moved); minor (G2: standby and lost connections built 2026-10-16, D-58, what is left named, estimate not moved); minor (G1: parity-checklist.md generated by hack/parity, refused when stale in CI; estimate not moved); minor (G9, G7, G5: of the owner's audit answers the drawer's built, night run: a Config Map's Save rebasing by itself and its drafts, a Secret's values in the drawer, D-59; 42 of 42 actions); minor (G9: of the owner's audit answers, a Config Map's drawer Save rebasing by itself and its unsaved values kept as drafts built, night run); minor (G21: the owner's audit answers of 2026-10-15 for the chat built in the night run: send all tools per connection, the `http://` warning and a CA file per connection, F9; the estimate); minor (G21: send all tools per connection, the `http://` warning and a CA file per connection built in the night run, the owner's audit answers (4) and (6) of 2026-10-15); minor (G21: send all tools per connection built in the night run, the owner's audit answer (4) of 2026-10-15); minor (D-57 built: G5's navigator and welcome page, G13's choosing a cluster, G2's connect log and insecure mark follow the context picker's removal; estimates not moved); minor (G5: the selection bar from one checked row and `ui.navigate` leaving the checked rows alone, the owner's audit answers (7) and (8), built 2026-10-15 night; G5 9–35 h (was 11–41), the totals recounted at the night run's end); minor (AGENTS.md rule 10: every entry checked against the code at HEAD; the Missing lists rewritten to what is missing today (G2, G3, G4, G5, G6, G7, G8, G9, G13, G17, G23), the "Decided … not built" lines of G1, G5, G6, G8, G9, G11, G12, G13 and G23 brought to what is built, stale "still missing" and "yet" sentences corrected; the owner's decisions of 2026-10-15 added where they open work (standby and lost connections in G2, one list of clusters in G13, the owner's audit answers in G5, G9 and G21; G16's findings), verifying the cloud credential plugins out of scope until long after parity (owner, 2026-10-15); re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration): G2 15–43, G3 4–12, G4 0–3, G5 11–41, G8 7–25, G9 5–17, G12 7–21, G13 28–96, G21 6–18, G23 8–22 h; free parity ~240–665 h, the full Lens target ~400–990 h, beyond Lens ~6–20 h); minor (G14: Sparkle's update key set up by the owner 2026-10-15, in the releases since v0.11.0, no signed appcast yet; the signed disk image proven by v0.6.0 to v0.8.0; notarization in a job of its own since 163664f2; G21's summary row and the beyond-Lens total brought to the entry's 3–10 h; estimates not moved); minor (G21: the API keys in an owner-only file on every platform, no keychain (the owner, 2026-10-15), its keychain item for Windows and Linux gone; G22 without the keychain backends; estimates not moved); minor (G5, G9, G16: the owner's F3 built, D-53: data keys above their values, a Config Map's values in editors saved in the drawer through `configMap.saveData`, names wrapping at - . _ /; estimates not moved); minor (G5: the title bar's Update button built, the owner's finding F7; estimate not moved); minor (G5: a copy's confirmation not in the notification log (F5); estimates not moved); minor (G21: H-96 built, a conversation in which a turn ran over a private connection is not kept, files of v0.8.0 to v0.10.0 cleaned at the next start; estimate not moved); minor (G21: H-96 decided, a private connection's conversations not kept, not built; G13: R3 postponed by the owner; estimates not moved); minor (G13: R2 built, hotbar tiles reordered by drag and drop, the paid row partial, the lead's decisions for the owner to confirm; estimate not moved); minor (G13: several windows designed, D-52, the owner's decision pending, estimate not moved); minor (G5: A73, A69 and A71 built, a plain click keeps the checked rows, the copy icon over a list cell's end and over a port's end with Forward… right after it; estimate not moved); minor (G1, G8: the terminal fork's four shell tests under the race detector in CI, A116 built, its races patched; estimates not moved); minor (G1: the README's pictures dark and light, its banner, lossless oxipng and one context name, A110, A111, A112, A114 built); minor (G13: About Cogpit in macOS's application menu, the dialog laid out anew and showing the version the build reports, decision A60 and the owner's finding F2 built; G16: that finding fixed; estimates not moved); minor (the owner's decision round of 2026-10-14 evening: a "Decided" bullet in G1, G2, G5 to G9, G11 to G14, G17 and G23 with what he confirmed and what he decided, not built; his findings in a real window in G12 (no node history from Prometheus, the next implementation's first item), G13 (About shows 0.0.1) and G16; G11: the SQL driver out of scope; G12: ranges beyond 30 days not to be built, the per-range query step answered; G13: the hotbar's drag to reorder and a tile dragged out to another window required; estimates not moved); minor (G14: D-16 revised by the owner, `cogpit-cli` inside the macOS app and the disk image the release's only download besides `SHA256SUMS`, built by agent CLIBUNDLE, not yet proven by a release, estimate not moved; G22: no `cogpit-cli` archives for Windows and Linux any more); minor (G14: Sparkle 2 and the signed appcast built, no update key yet; G15: P1 measured on the engine side; G13: deep links partial on macOS, D-47, estimate not moved; G13: start at login partial on macOS, D-48; G17: the cached Secrets' values and Flux's credential fields kept out of what agents read, H-82 and H-63 (owner), built; G11: credentials over plain http asked once per repository, H-40 (owner), built; G8: a node shell pod a crash left deleted at the next connect, H-71, owner; G7: an agent's role binding always waits for the user, H-27, owner; G14: the signed, notarized disk image built in the release pipeline, no tag run yet; , estimate not moved; G14: the Apple signing material exists, 2026-10-14, no longer a human prerequisite; G6: the workloads' drawers' Pods tables without a Namespace column, D-40 decided by the owner, estimate not moved; G23: Reconcile with Source and Reset built (D-33, decided by the owner), removed from Left, estimate not moved; G23: the views' status filter built, a dashboard's state opens its view filtered (owner, D-34), removed from Left (Flux CD), estimate not moved; G7, G13: D-37 decided by the owner, the node shell's default image the newest supported Alpine release kept current by Renovate and its Maximum duration built, estimates not moved; G12: the Metrics source setting built, removed from Missing, estimate not moved; G12: the Overview's Pods history from kube-state-metrics built (a night decision for the owner to confirm), removed from Missing, estimate down to 8–20 h; G12: history that outlives a closed drawer built (a night decision for the owner to confirm), removed from Missing, estimate not moved; G12: the Nodes list's Disk built (D-43, a night decision for the owner to confirm), removed from Missing, estimate not moved; G12: the Nodes list's usage bars built, removed from Missing, estimate not moved; G11: chart icons built, D-46, removed from Missing, estimate 2–8 h (was 3–10), totals not recounted; G11: the README in the chart's drawer built with a version picker, night build HELM2, estimate not moved; G5, G13: a context that leaves Cogpit takes its dock tabs, a connect under way forgets it, fixed, estimates not moved; G3, G23: the Flux and Argo CD dashboards follow a namespace-limited user's plans and refusals and say when the definitions cannot be listed, a refused label-selected scope not kept warm, fixed, estimates not moved; G17: `kinds_list` completed, every MCP row done, estimate 0–2 h, totals not recounted; G17: `dock_read` built (D-44); G17: every command a tool over the whole registry, GUI-only commands refused under one mark, estimate not moved; G5: no context picker by itself at start (D-42); G5, G13: the tabs and the tables' layout kept across a restart without connecting anything (D-41), a removed cluster's context forgotten, a vanished kubeconfig file's tabs closed with a notice; G6: a Pods table in the workloads' drawers built (D-40, a night decision for the owner to confirm), removed from Missing, from indexes only the open drawer holds, estimate not moved; G12: Prometheus detection follows a namespace-limited user's refusal, fixed; G3: a refused list of the custom resource definitions not retried until a refresh, and the namespace-limited user checked on kind, fixed; G3: views' indexes, an agent's Overviews, `Lookup` and write targets, Helm's releases and the Applications follow a namespace-limited user's plan and refusals, fixed; G12: pod metrics per namespace for a namespace-limited user, D-39, a night decision, built, estimate not moved; G3: one object's read, `object_get`, `cogpit-cli get` and the drawer, takes the object's namespace, fixed; G3: a refused read in all namespaces counts whoever met it, logs, port forwards, one object and `resources_list` follow it at once, fixed; G23: Argo CD built, partial, night build (D-35, D-36), its remainder 3–8 h, the gap's estimate for the lead; G23: Argo CD built, partial, night build (D-35, D-36), its remainder 4–10 h, the gap's estimate for the lead; G23: the Flux CD section built, partial: its views, dashboard and actions, D-33, D-34, Flux's remainder 6–15 h, the table's estimate for the lead; G7, G8, G13: the node shell (D-37) and Cluster Settings › Node Shell partial, 39 of 39 actions, estimates of G7 and G8 down; G9, G7: Create Secret partial, the eighth create dialog, the user's only (D-19), 38 of 39 actions; G1: CI rebuilt as parallel jobs of at most ten minutes, GoSec and cyclomatic complexity added, semantic-release live; G14: each release carries the unsigned app, `cogpit-cli` and `SHA256SUMS`, estimate not moved; G12, G6: a node drawer's Pods table with CPU and Memory and sortable, owner, removed from Missing, estimate not moved; G12: every byte amount in binary units with one decimal, D-32, estimate not moved; G8, G13: every timestamp in the zone and the region's format, D-31, estimates not moved; G6: the Secrets drawer decodes certificates, beyond Lens (D-30), estimate not moved; G5: the lists' copy icon outside a blacklist of columns and right after the text (owner, D-25), estimate not moved; G11: D-29 built, authenticated repositories from Helm's file, estimate 3–10 h; G3: workload logs, port forwards and one object's read follow the access reviews' answers, fixed; G5: copy icons in the lists built (D-25), estimate not moved; G3: the Overviews follow the access reviews' answers, fixed; G6: references to Pod Security Policies, admission policies and Device Resources link, fixed; G2: `cogpit-cli contexts` marks an insecure context; G5, G13: a removed cluster's tabs close, fixed; G13: Cluster › Disconnect follows a reconnect, fixed; G11, G5: the owner's requirements of the morning after the night run (D-29, D-25); G17: `view_rows`, `object_get`'s related objects, the agent writes allowance and the audit log in the GUI built; G17: `view_rows`, `object_get`'s related objects and the agent writes allowance in the GUI built; G17: `view_rows` and `object_get`'s related objects built; G17: `view_rows` built; G9: the Create resource tab's save and search buttons built, Revert to original built, drafts of unsaved editor text built, estimate not moved; G9: Revert to original built, drafts of unsaved editor text built, estimate not moved; G9: drafts of unsaved editor text built, estimate not moved; G3: namespace-scoped informers for users who may not list a kind in all namespaces built, estimate not moved; G11: the ConfigMaps storage driver built, the SQL one left missing with its reason, estimate not moved; G2: the kubeconfig's `proxy-url`, CA, `tls-server-name` and `insecure-skip-tls-verify` verified against TLS test servers, a `proxy-url` password kept out of connect errors, insecure contexts marked, estimate 8–25 h; G13: the kubectl Preferences built; G8: kubectl for terminals built, D-3; G6: the node drawer's Devices section built, estimate not moved; G8: copy and paste with the right mouse button partial, the Windows and Linux copy and paste keys, estimate not moved; G5: the search field's "Aa" and ".*" toggles built, estimate not moved; G13: the non-cluster proxy built, untrusted CAs held back; G13: a cluster's proxy built; G12: hidden metrics built; G5, G13: the navigator's section level, Local Kubeconfigs, and its scope as engine state built, the tab bar showing the scope's tabs, two rows from missing to partial, estimates not moved; G5: the title bar's history arrows, notifications and context menu built; G5: column reorder by dragging built; G5: row checkboxes and the selection's menu built; G9, G7: Delete of a selection built in the engine (`object.deleteMany`), estimates not moved; G5, G13: the welcome page, the Help and Window menus, Disconnect and Remove from Cogpit built, four rows partial, estimates not moved; G8: the log tab's pod and container selectors, older lines and jump to a time partial; G8: the log tab's pod and container selectors and older lines partial; G8: the log tab's pod and container selectors partial; G11: a release upgraded to a version from the user's repositories; G5, G13: the tab bar of cluster views built, its five shortcut rows, the navigator's ⇧↵ fixed tab, estimates not moved; G13: Preferences and Cluster settings built, night run, 11 rows partial, G8's terminal rows 2, estimates not moved; G12: the cluster Overview built, the remainder 15–35 h; G6: Applications, Vertical Pod Autoscalers and the four Device Resources views partial; G6: Vertical Pod Autoscalers and the four Device Resources views partial; G6: the four Device Resources views partial; G11: chart repositories, the Charts view and the install built; G12: the owner's evening requirements built (both providers together, the status line, the fallback, a missing value as an empty cell; measured on kind); G13: the command palette and its shortcuts built, estimate 50–140 h, the summary table not recounted; G2: `cogpit-cli contexts` and `get` built; G5, G13: the navigator's header, root, tooltips and keyboard built; G12: the owner's evening requirements (provider status line, Prometheus history then Metrics Server live) and the open step question; G16: the quit crash fixed; G12: Prometheus in the window, checked against a real Prometheus on kind; G12: Prometheus, backend; G2: Lens's Kubeconfig Syncs captures followed, scope C2; totals not recounted; G21: the built-in agent's engine side built, re-estimated at 20–40 h, the beyond-Lens total ~20–42 h; G12, G1, G16: node history from node-exporter built in the night run (F1, A43, A115); G12: query sets for ranges far apart built (A46); G21: the built-in agent's window half built (the chat column, Preferences › Agent), night run, six rows partial, estimate not moved; G21: the private connection's reads built in the engine (M12 (c)), estimate not moved; G8: Download saves what the logs tab shows (A84) and the Jump to time field's cut placeholder fixed (#169), estimate not moved; G23: Flux and Argo CD from API discovery without the definitions (A90), estimate not moved; G23: Reconcile with Source waits as the flux CLI does (A91), estimate not moved; G8: an application's logs in one merged tab (A81), estimate not moved; G8: kubectl's signature with sigstore-go and its Rekor entry, D-50 built, estimate not moved; G5, G13: the owner's A51–A55, A61, A63, A64, A67 and A113 built 2026-10-15 (the navigator's and the palette's state as engine state, D-51 with night decisions for the owner to confirm; the ring on keyboard navigation only; the dropdown of the hotbar's clusters; collapsible source groups; Disconnect closing the tabs; F1–F12 in the search fields; ⌘F and ⌘⇧T in the View menu; the welcome page's Recent and links), estimates not moved; G2, G13: the plugin approval binds Cogpit's proxy, A23; G2: an unchecked certificate asked once, A28; G2: "TLS not checked" while connected, A29; G11: A39 built; G11: A17 built; G11: A36 built; G11: A14 built 2026-10-15) · **Target:** Lens Desktop 2026.10.51501

Every known shortfall against Lens Desktop 2026.10.51501, and against Cogpit's own targets beyond
Lens, one entry each, ranked by what stands between Cogpit and alpha, then beta. This is the work
list: take the highest-ranked entry nobody is working on, and when you close part of one, update its
evidence and estimate here and the row in the parity page it points to
([rules](README.md#rules-for-every-progress-page)).

On 2026-10-11 the local MVP closed parts of G1–G9, G14, G15 and G17; their entries say what is
left, with evidence. Entries without code are the whole of their area. Numbers come from
[`target-app-parity.md`](target-app-parity.md) and the checklists in
[`ui-parity.md`](ui-parity.md), [`resource-parity.md`](resource-parity.md),
[`platform-parity.md`](platform-parity.md) and [`beyond-lens.md`](beyond-lens.md).

Hours are Opus 5.5 agent-hours, one agent working sequentially, including tests, estimated with
PhotoCraft's calibration (2–9 h per checklist item, 8–20 h per subsystem) until Cogpit has its
own. Since 2026-10-15 a checklist row that exists but is partial counts at a quarter of a small
interface row's 1–4 h, 0.25–1 h: the check against a capture or a real window and the fixes it
finds. **Human** marks what an agent can't finish alone.

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
| G14 | Packaging, signing and updates (macOS) | platforms | signing and updates block beta; the download alpha needs exists (since 2026-10-13) | 12–30 h + human | [platform-parity](platform-parity.md#operating-systems-and-packages) |
| G15 | Performance and API load at scale | performance | blocks beta | 28–66 h | [testing](../developer/testing.md#performance-budgets-proposed-not-measured) |
| G16 | Stability and the user bug backlog | stability | blocks beta | 100–200 h | this page |
| G17 | MCP control | beyond Lens | closed 2026-10-16 night (M9 done) | 0 h | [beyond-lens](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| G18 | Paid Lens Desktop features | features | low-medium | 110–220 h + human | [resource-parity](resource-parity.md#paid-lens-desktop-views) |
| G19 | External extensions | ecosystem | not a target ([D-5](../developer/decisions.md#d-5-extensions)) | – | [decisions D-5](../developer/decisions.md#d-5-extensions) |
| G20 | Teamwork and remote access | ecosystem | not a target by default | 150–300 h + hosted service | [decisions D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features) |
| G21 | Built-in agent | beyond Lens | pulled forward (owner, 2026-10-14) | 2–6 h + human | [beyond-lens](beyond-lens.md#built-in-agent) |
| G22 | Windows and Linux | platforms | Linux's deb, APT repository and updates built 2026-10-20 (D-108); the Windows setup, WinSparkle and the terminal on ConPTY built 2026-10-21 on the branch `windows-release` (D-111); the rest after the macOS beta | 20–45 h + human | [platform-parity](platform-parity.md#operating-systems-and-packages) |
| G23 | Flux CD and Argo CD views | features | blocks beta ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)) | 8–22 h | [resource-parity](resource-parity.md#paid-lens-desktop-views) |

Totals (re-estimated 2026-10-15, the sums of the table): Lens's free feature set on macOS
(G1–G16) is **~220–620 h** (220–613 in the night of 2026-10-16/17 with G12 1–4; 221–616 on 2026-10-16 afternoon with G12 2–7, Cogpit Metrics withdrawn; 222–619 on 2026-10-16 night after the night run: G1 0–2, G2 8–26, G3 0–4 and G5 9–35 re-estimated, G12 3–10; 238–656 after G12's re-estimate earlier that night; 242–667 on 2026-10-15; was ~420–1,040 on 2026-10-12 after the fourth run, and
~360–920 in ROADMAP's rough re-estimate of 2026-10-13); the beta adds the Flux CD and Argo CD
views (G23); with Lens's other paid features, Windows and Linux (G18, G22, G23), the full Lens
target is **~360–900 h** (358–900 on 2026-10-21, G22 20–45 after the Windows setup; 368–920 on 2026-10-20 night, G22 30–65 after the Linux package; 378–935 in the night of 2026-10-16/17; 379–938 on 2026-10-16 afternoon; 380–941 on 2026-10-16 night; 396–978 earlier that night; 400–989 on 2026-10-15; was ~620–1,450). On 2026-10-15 G2, G3, G4, G5, G8, G9, G12,
G13, G21 and G23 were re-estimated from what is built and from the owner's decisions of that day
(standby and lost connections, one list of clusters, the owner's audit answers), with the same borrowed
calibration and the partial rows counted as above; G1, G6, G7, G10, G11, G14, G15, G16, G17, G18
and G22 were checked against the code and not moved. Cogpit's own targets beyond Lens (G17, G21)
add **~2–6 h** (2026-10-16 night: G17 closed, 0 h; 2026-10-15 night: G21 2–6 h, the owner's audit answers for the chat built; 6–18 h
with them to build; was ~3–12 h
after the chat column of 2026-10-14 night, ~20–42 h before it, and ~50–110 h before its engine
side).
G19 and G20 are not targets and not counted. The Fyne spike before M2 ran as part of the MVP and
D-1 is decided for Fyne ([results](../developer/spike-fyne.md#results)).

## Foundation gaps

### G1. Workspace, CI and progress generators
- **Decided (owner, 2026-10-14 evening, [decisions](../developer/decisions.md); all of it built
  2026-10-14 night and 2026-10-15):** CI and tests: node-exporter and kube-state-metrics on in the kind cluster's
  Prometheus values (`hack/kind`), with kind tests of node and pod history from Prometheus
  ([G12](#g12-metrics-and-cluster-overview); built 2026-10-14 night: `TestKindPrometheus`,
  `TestKindNodeExporterByAddress`, `TestKindClusterOverview`, which skip in CI, whose kind cluster
  has no Prometheus); the terminal fork's four shell tests under the race
  detector in CI ([G8](#g8-logs-terminal-and-shells); built 2026-10-15); confirmed: renders of the workload drawers'
  Pods for Deployments and Stateful Sets only. The README stays the showcase page, and no further
  Markdown files go into the repository root (owner); decided for it: images through `<picture>`,
  light renders for GitHub's light theme and dark ones for dark; a banner (the logo, the name, one
  sentence) in both themes; the images optimised losslessly (oxipng, zopfli); one neutral demo
  context name in every showcase image (these four built 2026-10-15: `make showcase`, the lead's
  decisions in [decisions](../developer/decisions.md#the-readmes-pictures-a110-a111-a112-a114)).
  Moot: the CLI archive's README (no CLI archives any more).
  Estimate not moved: these were additions, and the list below is unchanged (checked 2026-10-15).
- **Built 2026-10-16 (night run):** the generated page [`parity-checklist.md`](parity-checklist.md)
  from [`hack/parity`](../../hack/parity/checklist.go): every checklist row with its status and
  section and the counts, written by a plain `go run ./hack/parity`, refused by `make parity` (CI,
  `-check`) when it no longer matches the checklists (`TestChecklistPageWrittenOrRefused`).
- **Built 2026-10-16 (night run, [D-64](../developer/decisions.md#d-64-envtest-a-real-api-server-in-ci-without-docker),
  the lead's decisions, confirmed by the owner in the audit of 2026-10-16 where it asked):** an envtest tier and its CI job, Integration
  Tests (envtest): `make test-envtest` runs the tests tagged `envtest` with the race detector
  against envtest's etcd and kube-apiserver of Kubernetes 1.37.0, fetched by `make envtest-bin` and
  checked against their pinned SHA-512 ([`hack/envtest/fetch.sh`](../../hack/envtest/fetch.sh)),
  started by [`testkit/envtest`](../../internal/testkit/envtest/envtest.go) through
  controller-runtime's envtest v0.25.2 (no k8s.io module moved). The first test that needs it,
  [`TestEnvtestStandbyAPIServerStopped`](../../internal/testkit/integration/standby_envtest_test.go),
  stops the API server with SIGSTOP: interrupted 3.68 s after it (the window 5.7 s), writes refused
  at once, connected again 0.40 s after SIGCONT and the view following the server (measured
  locally, five runs; the CI job not observed yet).
- **Built 2026-10-16 (night run, [D-15](../developer/decisions.md#d-15-progress-generators), the
  lead's choices, the main ones confirmed by the owner in the audit of 2026-10-16):** `make scorecard` ([`hack/scorecard`](../../hack/scorecard/main.go))
  runs the budget tests and benchmarks without the race detector and writes
  [`scorecard.md`](scorecard.md); the tests record each measurement with
  [`scorecard.Record`](../../internal/testkit/scorecard/scorecard.go). The first run, committed: 13
  budgets measured, all within their limits (32 measurements); P3, P6 and P7 not measured (a real
  window, Lens beside Cogpit). By hand, not in CI, as D-15 decided.
- **Missing:** the scorecard run regularly on a fixed runner, in CI (D-15's "later"); the envtest
  job observed in CI (it has not run there yet).
- **Evidence:** built on 2026-10-11: [`go.mod`](../../go.mod) (Go 1.27.2, pinned versions), the
  [`Makefile`](../../Makefile) with local targets, [`.golangci.yml`](../../.golangci.yml) with the
  team's linters and `depguard` rules for every layer (a forbidden import fails `make lint`;
  verified with probe imports). golangci-lint is pinned (v2.15.0 since 2026-10-22), at least v2.14.0 because v2.13
  cannot read Go 1.27.2 export data. CI as workflow files (2026-10-11,
  [releasing › CI](../developer/releasing.md#ci)): [`release.yml`](../../.github/workflows/release.yml)
  calls [`build.yml`](../../.github/workflows/build.yml) on every push and pull request: macOS runs
  `make lint`, the race tests with coverage and `make build`; Linux and Windows compile and link
  every package and test with cgo and Fyne's dependencies; a Linux job runs `make test-kind` on a
  kind cluster; govulncheck. On `main` it commits the coverage badge (84.5 % measured locally) and
  runs semantic-release as a dry run ([`.releaserc.json`](../../.releaserc.json), the
  `conventionalcommits` preset as in the team's other repositories); Renovate runs from
  [`renovate.yml`](../../.github/workflows/renovate.yml). The README carries the build, coverage
  and licence badges (the Go Report Card's left on 2026-10-14: the service is retired, owner). Verified locally: the macOS job's `make` targets pass,
  actionlint, the Renovate config validator, the dry run against a stand-in for the GitHub API.
  Observed on GitHub (2026-10-11): the first run failed Compile (linux) and the vulnerability
  check (GLFW 3.4 needs the Wayland headers; fixed in the workflow) and the kind job (a flaky
  test, fixed in the cache); from then on every job is green on `main`, including the kind job on
  Kubernetes 1.37.0, govulncheck (after `golang.org/x/net` v0.60.0 and `golang.org/x/image`
  v0.43.0), the coverage badge commit and the semantic-release dry run, which created no tag and
  no release.
  Built on 2026-10-12: `make parity` ([`hack/parity`](../../hack/parity/main.go), D-15), a CI
  step after the lint: it refuses duplicated rows, rows marked done or partial without evidence,
  command ids the registry does not know, links to missing files and unknown test names, and
  compares its counts with the numbers ROADMAP and the progress pages report
  ([rules](README.md#make-parity)); its first run found six partial rows without evidence and a
  platform summary that counted 7 partial rows instead of 8, and reproduced the 121 free
  interface rows (the 123 counted the two paid shortcuts). The dependency backlog of 2026-10-12
  (44 Renovate pull requests) was held by flaky tests, not by the updates
  ([releasing › Renovate](../developer/releasing.md#renovate)).
  Rebuilt on 2026-10-13 (owner, [releasing › CI](../developer/releasing.md#ci)): every check a job
  of its own on the self-hosted runners, as in `valkey-operator`, none longer than ten minutes:
  lint, parity, GoSec (new; its 43 findings triaged, one fixed in `servicesOf`, the rest excluded
  with their reasons), govulncheck, cyclomatic complexity (new, threshold 56), unit tests without
  the race detector, the UI's race tests in eight shards ([`hack/shard`](../../hack/shard/main.go)),
  the other race tests, the kind and drain cluster tests (the drain cluster in CI for the first
  time; the Metrics Server in `make kind-up`), the macOS-only tests and app, the Windows compile,
  and a combined coverage report ([`hack/coverage`](../../hack/coverage/main.go)). Semantic
  Release is live ([D-16](../developer/decisions.md#d-16-the-first-real-release) revised), and
  [`build.yml`](../../.github/workflows/build.yml) attaches `cogpit-cli`, the unsigned app and
  `SHA256SUMS` to each release. Since 2026-10-21 ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle))
  the Windows compile is two jobs that run every test there, Unit Tests (windows) and UI Tests
  (windows), beside the job Windows Package; not observed on GitHub yet.
- **Impact:** CI guards `main`; `make parity` keeps the hand-written checklists honest and the
  checklist page generated; `make scorecard` measures the budgets on demand.
- **Estimate:** 0–2 h (was 3–8 until 2026-10-16: the scorecard and the envtest job built; was 6–14
  before `make parity`, 2026-10-12); the totals not recounted.

### G2. Cluster connection and authentication
- **Built 2026-10-17 (the owner's F28 and F29, [G16](#g16-stability-and-the-user-bug-backlog),
  [D-81](../developer/decisions.md#d-81-reconnect-and-what-log-in-again-does)):** a connection keeps
  the login it read at its connect, so a client certificate renewed in the kubeconfig file never
  reached it and Log in again, which renewed only a credential plugin's login, could not help (the
  cause, verified in the code and on the owner's running Cogpit, reproduced on kind). Now
  `cluster.reconnect` connects a connected cluster anew from its file as it is now, the new
  connection first and the tabs kept; Log in again reconnects a login without a plugin or one
  changed in the file; a login the API server rejects reconnects by itself once the file holds
  another (no plugin, each version of the file once); agents may reconnect except an expired plugin
  login. **Still open:** not tried in the owner's window or on his cluster; a renewed certificate
  *file* in a place macOS protects is not seen as changed by itself (its stamp is not read there),
  Reconnect and Log in again use it.
- **Decided (owner, 2026-10-14 evening):** a context that skips the certificate check
  (`insecure-skip-tls-verify`) asks once before its first connect, the answer kept like a plugin
  approval (**built 2026-10-14**, A28: through the plugins' policy and dialog, per context; agents,
  the CLI and links refused until the user allowed it; the lead's night decisions in H-37); while it is connected the status bar says "TLS not checked" in the warning colour beside
  the context, and `cogpit-cli get` writes a warning line to standard error (**built 2026-10-15**,
  A29, from the engine's `connection.tlsUnchecked`, which agents read too)
  ([H-37](../security/trust-boundaries.md#h-37-a-context-that-skips-the-certificate-check)); a
  credential plugin's approval shows and binds Cogpit's own proxy for the cluster too, and a changed
  proxy asks for the approval again
  ([H-34](../security/trust-boundaries.md#h-34-the-plugin-approval-shows-the-kubeconfigs-proxy-not-cogpits);
  **built 2026-10-14**, A23: the proxy bound as shown, its password `•••`, the lead's night decision
  in [D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program)).
  Confirmed: a single object's reads (`object_get`, `cogpit-cli get`, logs, forwards) read the
  target's namespace. Rows: [platform parity](platform-parity.md#cluster-authentication-and-connectivity),
  [ui parity › Layout](ui-parity.md#layout). Estimate not moved.
- **Lens's captures of 2026-10-12 followed** ([ui parity › Clusters](ui-parity.md#clusters), built 2026-10-12, scope C2): dot files in sync
  folders read (regular files only, at most 1 MiB, a FIFO never opened: `readBounded` also looks before it opens now); a connected
  cluster closed when its file is deleted or its source removed; one dialog, "Sync Files and Folders", for files and folders;
  `~/.kube` a default synced folder (removable, the removal remembered); a connect's log ("Connection Updates") behind a
  warning icon in the picker (since [D-57](../developer/decisions.md#d-57-choosing-a-cluster) the welcome page's icon and the section row's Show Connection Updates…); owner requirement: removing a synced item asks, naming the path, that only the link is removed.
  **Still open:** the ↻ beside the ⚠; Lens's system file dialog instead of Cogpit's own
  browser on Windows and Linux (M11; on macOS built 2026-10-16 night, one `NSOpenPanel` for files and
  folders, several at once, [D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access); estimate not moved, for the lead). Since D-28 (built 2026-10-12, C3) `config` is an
  ordinary file of `~/.kube` and every context comes from one file read alone, so a deleted `config` closes its connections as any
  synced file does. A plugin shared by several contexts of one file is approved in one dialog that lists
  every context with its server (owner, 2026-10-12, built by C4), stored per context.
- **Built 2026-10-16 (the connection log, [D-66](../developer/decisions.md#d-66-the-connection-log), the owner's decision of
  the day):** "Connection Updates: <context>" for every context, opened from a cluster's status dot too, live, with Copy;
  every connect, reconnect, login and disconnect kept on disk per context (7 days, 1 MiB) with its steps, times and durations;
  a failure's error picture (class, HTTP status, the presented certificate's public facts, the error chain); a credential
  plugin's standard error after a failed login, captured from client-go's Authenticator, scrubbed and cut to 4 KiB
  ([H-112](../security/credentials.md#h-112-a-credential-plugins-standard-error-in-the-connection-log)). Lens shows INFO and
  ERROR lines of a failed connect only; the rest is Cogpit's own ([beyond Lens](beyond-lens.md#standby-and-lost-connections)).
  **Still open:** not tried with a real login program's failure.
- **Decided (owner, 2026-10-12, [D-17](../developer/decisions.md#d-17-token-files-named-by-added-kubeconfigs)):**
  a `tokenFile` of an added or synced kubeconfig outside its folder is asked for in the
  plugin approval dialog (H-24); **built 2026-10-12** (`TestTokenFileOutsideTheFolderIsAskedFor`;
  what remains is in H-24). The same run bound a program or CA file inside the kubeconfig's folder
  by content (review R-5) and made the dialog show the proxy, insecure and own-CA settings (R-11).
- **Decided (owner, 2026-10-12, [D-21](../developer/decisions.md#d-21-pasting-a-kubeconfig)):**
  no pasted kubeconfigs; the paste feature built on 2026-10-12 is removed (done 2026-10-12;
  sources of kind `pasted` from earlier builds are no longer listed, their files stay).
- **Built 2026-10-16 (standby and lost connections, [D-58](../developer/decisions.md#d-58-standby-and-lost-connections)):**
  the owner's finding on a real cluster (2026-10-15 evening: after the Mac slept every connection
  was gone and the window hung), decided in an interview the same evening. S1 reproduced it on a
  kind cluster of its own: no hang of the window offscreen with the control plane frozen, and a
  login program that waits after its credential expired stalls every request of the connection
  without a word. Built: a heartbeat, the wake, a network change and a broken watch check the
  connection; "Reconnecting…" and "Login expired" with a banner (since 2026-10-16 as dots, no word:
  blinking green and red, the owner's F13); the views kept, writes disabled for
  the window and agents; a reconnect with backoff without end; one renewal of a login, then Log in
  again; logs and port forwards resuming, shells ending with Reconnect, drains and Helm actions
  stopping as after a crash; a 30 s timeout on every request that is not a stream
  ([beyond Lens › Standby and lost connections](beyond-lens.md#standby-and-lost-connections): 5 of 8
  rows done, 3 partial). **Still open:** the owner's check in a real window after a real sleep (Fyne
  and GLFW after sleep could not be reproduced offscreen); a real login program with its browser;
  an interrupted drain or Helm action end to end; the wake and network signals on Windows and Linux;
  [H-101](../security/trust-boundaries.md#h-101-a-credential-plugin-runs-again-during-a-connection-without-being-checked-again)
  (client-go renews a credential without the approval being checked again). Estimate not moved,
  for the lead to re-estimate.
- **Built 2026-10-16 night (a login that waits, [D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound),
  the owner's finding of that night):** a connect waiting for an OIDC login in the browser that he
  had not finished blinked for ten minutes, without a timeout, and Disconnect waited behind it
  (client-go runs the plugin without a context). Built: Disconnect (the window's **Cancel
  Connecting**, in the cluster's menus and on the connecting workspace) cancels a connect under way
  at once, a restore's background connect too, and ends the credential plugin's process it waits
  for, found among Cogpit's child processes when the run began (since 2026-10-19, WF38: followed by
  its number, so a wrapper that `exec`s is ended too; one not found is said to run on, and a
  cancelled connect starts no run later); every run of a plugin (a connect's,
  a renewal's, Log in again's) is ended after 5 minutes, and none starts again until the user
  connects or logs in again; the connection carries `waitingForLogin` (program, since, until),
  shown in the workspace and the dot's tooltip, read by agents in `ui_state`; the connect's 15 s
  limit no longer counts the login (a browser login longer than it failed the connect after it had
  worked). **Missing:** a real OIDC plugin with a browser in a real window (the tests use shell
  scripts that wait); the process lookup on Windows beyond its test (`TestWindowsChildIsFoundAndEnded`
  since 2026-10-21, D-111; a `.bat` or `.cmd` plugin runs as `cmd.exe`, which its name does not
  find, so a cancel ends in "may run on"); the plugin's children run on,
  and two runs that begin together are not always told apart
  ([H-118](../security/credentials.md#h-118-what-a-cancelled-login-leaves-running));
  whether Log in again should start a waiting renewal over (the owner's question in D-73).
- **Missing (2026-10-15):** the exec plugins' environment beyond the `PATH` stopgap
  (`cluster.EnsureToolPath` in [`cluster.go`](../../internal/cluster/cluster.go);
  [H-6](../security/credentials.md#h-6-exec-plugins-inherit-the-environment)); the ↻ beside the ⚠
  and, on Windows and Linux, the system's file dialog (Lens's
  captures, above; macOS's built 2026-10-16 night, D-72); a check against a real proxy and a cloud cluster's own CA; Lens's dialogs for
  adding kubeconfigs captured (Cogpit's are inferred). Built since this
  list first named them (Evidence): kubeconfig discovery, merging and validation, connections
  through client-go with every authentication form, the legacy `oidc` provider's refreshed tokens
  kept in memory rather than written back (`memoryPersister`; the owner's decision open,
  [H-4](../security/credentials.md#h-4-auth-provider-token-refresh)), access reviews,
  credential-free connect errors with a connect log, and the per-terminal kubeconfig
  ([security › credentials](../security/credentials.md#the-terminals-kubeconfig)).
- **Evidence:** [platform-parity](platform-parity.md#cluster-authentication-and-connectivity),
  7 of 10 rows partial (7 of 13 on 2026-10-13; 2 after the MVP: certificates on kind, tokens on a fake
  server), 3 missing; the EKS, GKE and AKS plugin rows and the cloud-account row not a target since
  the owner's decision of 2026-10-22 (won't build, [D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)). Built: contexts from the merged kubeconfig files, connections
  through client-go's loading rules with credential-free errors, implicit kubeconfig writes
  switched off, cached discovery, the per-terminal kubeconfig ([`internal/cluster`](../../internal/cluster/),
  [`internal/kubeconfig`](../../internal/kubeconfig/)). Access reviews built on 2026-10-11
  ([`cluster/access.go`](../../internal/cluster/access.go),
  [`engine/accessreviews.go`](../../internal/engine/accessreviews.go)): `SelfSubjectRulesReview` per
  namespace and `SelfSubjectAccessReview` where rules cannot answer, with their own request budget,
  cached per connection until refreshed or reconnected; measured, connecting costs one rules review
  and settling the sidebar one more access review for a user limited to a namespace
  (`TestBudgetAccessReviews`; on kind with a ServiceAccount token, `TestKindAccessReviews`). Built
  on 2026-10-12 (fourth run): kubeconfig files and sync folders added in Cogpit, each file read on its own and offered as
  `<context> [<source>]`, sync folders watched by polling; a source's exec credential plugin runs
  only after the user allowed it in the GUI, bound to a fingerprint of the command, arguments,
  environment and server; adding, removing and allowing refused for agents and the CLI
  ([`engine/kubeconfigs.go`](../../internal/engine/kubeconfigs.go); `TestExecPluginRunsOnlyOnceAllowed`,
  `TestSourcesFileIsOwnerOnly`, `TestAddedFilesAreNeverWritten`, `TestSyncFolderPicksUpAndDropsFiles`;
  on kind `TestKindAddedKubeconfig`, `TestKindExecPluginAfterApproval`); exec plugins partial in
  [platform-parity](platform-parity.md#cluster-authentication-and-connectivity). Then still missing:
  the exec-plugin environment (only a `PATH` stopgap, still so); Lens's own dialogs for adding
  kubeconfigs are not captured (the built ones are inferred, still so); `~/.kube/` watched without
  setup (a folder synced by default since 2026-10-12, scope C2 and D-28, above). Built 2026-10-13 (milestone 0.1): `cogpit-cli contexts` and `cogpit-cli get` over the
  same sources and approvals as the app (shared through [`internal/app`](../../internal/app/app.go)),
  read-only through a headless engine, a plugin the user has not allowed refused before it runs
  (`TestContextsListsTheSourcesWithoutSecrets`, `TestGetRunsNoPluginTheUserHasNotAllowed`,
  `TestGetKeepsSecretsSecret`, on kind `TestKindCLIContextsAndGet`).
  Night run (2026-10-13, V2): the kubeconfig's TLS and proxy fields verified through the cluster
  layer against the fake API server presenting a test CA's certificate
  ([`testkit/tlsca.go`](../../internal/testkit/tlsca.go)): `certificate-authority(-data)`, a
  certificate for another host with and without `tls-server-name`, `insecure-skip-tls-verify`, and
  `proxy-url` through the test forward proxy with Cogpit's cluster proxy replacing it
  (`TestConnectTrustsTheKubeconfigCA`, `TestConnectWithTLSServerName`,
  `TestInsecureSkipTLSVerifyConnectsToAnUntrustedServer`, `TestKubeconfigProxyURLIsHonoured`); a
  certificate that fails the check now says which kubeconfig field to look at
  ([`cluster/tls.go`](../../internal/cluster/tls.go)). **Fixed:** client-go quotes a `proxy-url` it
  cannot use whole in its error, password included; `cluster.Connect` replaces the user
  information of every `proxy-url` of the kubeconfig in its errors
  ([`cluster/proxyurl.go`](../../internal/cluster/proxyurl.go), `TestKubeconfigProxyPasswordNeverInErrors`).
  A context with `insecure-skip-tls-verify` is marked on the welcome page and in the section row's
  tooltip (the context picker's row until D-57) and warned about in its
  connect log (Cogpit's default, [H-37](../security/trust-boundaries.md#h-37-a-context-that-skips-the-certificate-check);
  `TestInsecureContextIsMarkedAndLogged`, `TestClusterRowsMarkAnInsecureContext`), and since 2026-10-13
  in `cogpit-cli contexts`' table too (CERTIFICATE `not checked`, `TestContextsMarksAnInsecureContext`). Two rows partial
  in [platform-parity](platform-parity.md#cluster-authentication-and-connectivity); not verified
  against a real proxy or a cloud cluster's own CA.
- **Impact:** a user who cannot connect cannot use anything; exec plugins are how most cloud
  clusters authenticate. client-go carries the authentication itself, which is why this is smaller
  than it was in the Rust plan.
- **Estimate:** 8–26 h, re-estimated 2026-10-16 night (the lead's estimate): standby and lost
  connections built (D-58), its 8–20 h out, 1–3 h in for its check in a real window after a real
  sleep and the owner's confirmations (Log in again renewing on the open connection; given in the
  audit of 2026-10-16). The plugin's
  standard error in the connect log built since (D-66, 2026-10-16), its 1–3 h of the breakdown
  below done; not re-estimated, for the lead. Was 15–43 h,
  re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration):
  standby and lost connections a subsystem, 8–20 h (decided 2026-10-15 evening, new work); the
  exec plugins' environment 2–6 h; the plugin's standard error in the connect log 1–3 h; the ↻ and
  the system's file dialog 1–4 h; the real proxy and CA checks 1–3 h; the 7 partial rows at
  0.25–1 h, 2–7 h. Was 8–25 (the TLS and proxy fields verified, night run 2026-10-13), 10–28 before,
  14–34 before the fourth run's kubeconfig sources and exec plugins. **Human:** none (the cloud
  providers' credential plugins are not a target, owner 2026-10-22: won't build).

### G3. Cache and the resource model
- **Missing (2026-10-16):** nothing this list named on 2026-10-15: discovery cached on disk and
  memory per kind and per cluster in the Diagnostics view are built (2026-10-16, Evidence). The
  lead's night decision on a drawer's events (the namespace's events, below) and D-65's decisions
  wait for the owner. Found while measuring on a real cluster (2026-10-16), not decided: the
  custom resource definitions' watch holds them in full, their OpenAPI schemas included, 61 MiB of
  the 93 MiB the cache held with eight views open on a cluster with 232 definitions, while the
  custom kinds need only their names, versions, scope and printer columns; dropping the schemas in
  the cache's transform, read on demand for the definitions' own view as managed fields are, is a
  candidate (2–4 h). By design, not missing: a namespace filter on a kind the user may list everywhere filters one
  cluster-wide watch client-side. Built since this list first named them (Evidence): the shared,
  reference-counted informers per cluster, kind and namespace scope, the warm period, no resync,
  bookmarks and streaming lists, metadata-only informers, the `managedFields` transform, access
  reviews cached, polling only while visible, the request counters and the diagnostics view, the
  kind-profile framework, the generic profile for custom resources, status tokens and coalesced
  snapshots.
- **Evidence:** built on 2026-10-11 ([`internal/cache`](../../internal/cache/), [`internal/model`](../../internal/model/)):
  shared, reference-counted informers, the warm period, no resync, streaming initial lists, the
  `managedFields` transform, request counters, the kind-profile framework with status tokens, and
  coalesced snapshots (≤ 10 per second per view). P9–P11 hold in tests
  ([testing](../developer/testing.md#performance-budgets-proposed-not-measured)). Then still
  missing: namespace-scoped informers (built 2026-10-13, below), metadata-only informers (built in
  the third run, 2026-10-11, the scopes' metadata flag), discovery cached on disk (still missing),
  polling sources (the metrics hub and the Helm indexes poll only while shown, 2026-10-12 and
  2026-10-13, G11, G12), the diagnostics view (built 2026-10-13, below); access reviews are cached
  since 2026-10-11, G2.
  Built in the night run (2026-10-13): namespace-scoped informers for a user who may not list a
  kind in all namespaces: one informer per named namespace (the cluster's accessible namespaces,
  the namespace filter's, else the context's), shared and reference-counted, merged by one handle
  (`Cache.AcquireNamespaces`), no cluster-wide list once the access reviews said no, refused
  scopes not kept warm ([architecture §5](../developer/architecture.md#5-cache), budget P15,
  `TestBudgetAccessibleNamespaces`, `TestNamespacedWatchesWhenClusterWideListIsDenied`,
  `TestKindAccessibleNamespaces`). Still filtered client-side: a namespace filter on a kind the
  user may list everywhere (one cluster-wide watch, as before). Fixed 2026-10-13: the Workloads and
  cluster Overviews opened before the access reviews answered kept the cluster-wide handles they took
  (a watch the API server refused, retried while open); they now start again when the answers change
  how their kinds are read (`Backend.pagePlanKey`, `TestPagesFollowTheAccessReviews`). Fixed the
  same day: a workload's logs tab and a port forward (a permanent one starts at connect) opened
  before the answers took a cluster-wide pods handle that, for a user who may not list pods
  everywhere, never synced and failed after 15 s. They, and the read of one object (`object_get`,
  `cogpit-cli get`), now wait for the answers, then such a user reads the target's namespace alone,
  even when the namespaces the plan names miss it (Cogpit's default, confirmed by the owner
  2026-10-14 evening); a
  namespace refused too ends them with the refusal (`Backend.acquireIn`,
  `TestPortForwardWaitsForThePlanAndReadsItsNamespace`, `TestWorkloadLogsWaitForThePlanAndReadTheirNamespace`;
  [architecture §5](../developer/architecture.md#5-cache)). Fixed the same night: for a user whose
  one RoleBinding is in the context's namespace (its rules allow the kind there, so the plan reads
  all namespaces first and the API server refuses), a workload's logs, a port forward, the read of
  one object and `resources_list` failed after 15 s unless a view had met the refusal first, and a
  page or an index kept a refused watch while open; every reader's refusal now counts
  (`cache.Cache.OnRefused`), and the reads that wait take the target's namespace at once after one
  refused list (`readFollowing`, `TestTargetReadsFollowARefusedClusterWideList`). Fixed the same
  night: an agent's `object_get` and `cogpit-cli get` of one object (`DetailWait`) and the GUI's
  drawer read the plan's namespaces, which can miss the object's (it showed as missing); they read
  the object's namespace, its events there and a namespaced object's related objects there
  (**night decision, the lead's, open to revision:** the namespace's events, not a
  field-selected scope per object; [architecture §5](../developer/architecture.md#5-cache)),
  following a refusal (`TestDetailWaitReadsTheObjectsNamespace`, `TestDetailWaitFollowsARefusal`,
  `TestDrawerReadsTheObjectsNamespace`). Fixed the same night, found while checking the rest of the
  class as a namespace-limited user: a view whose related-objects index was refused (the Services
  view's endpoint slices) stayed loading; an agent's Workloads or cluster Overview with no page open
  waited 15 s on refused reads; `Lookup` and the target of Edit, Revert and Delete read the plan's
  namespaces; Helm's releases were never read per namespace (no release shown); the Applications
  kept refused reads until their informer retried; each now follows the plan and refusals
  ([architecture §5](../developer/architecture.md#5-cache): `TestViewIndexFollowsARefusal`,
  `TestPagesFollowARefusal`, `TestLookupsAndWriteTargetsReadTheTargetsNamespace`,
  `TestHelmReleasesOfANamespaceLimitedUser`, `TestApplicationsFollowARefusal`). Checked, nothing to
  fix: `view_rows`, `resources_find`, the palette. Fixed the same night: a user who may not list the
  custom resource definitions was asked for them again on every backoff step for as long as the
  cluster was connected; the refused list now ends the watch until a refresh of the access reviews
  (`TestCustomKindsStopAfterARefusal`). Fixed 2026-10-14 (the night build's seams): the Flux and
  Argo CD dashboards, built before these fixes, read their kinds in all namespaces while the access
  reviews were out, and an agent's read with no page open waited 15 s on refused reads; the page
  now waits for the plans as a view does and an agent's read follows refusals, measured at the HTTP
  level for a RoleBinding in the context's namespace, for named accessible namespaces (no read in
  all namespaces) and for a cluster-admin (unchanged); a refused label-selected scope (Argo CD's
  Repositories and Clusters) is no longer kept warm and retried
  ([architecture §5](../developer/architecture.md#5-cache), `TestGitOpsDashboardsOfANamespaceLimitedUser`,
  `TestRefusedSelectedScopeIsNotKeptWarm`). **Checked on kind** (`TestKindNamespaceLimitedUser`: a
  ServiceAccount whose one RoleBinding is in its context's namespace): the Pods view with CPU and
  Memory, `object_get`, a Deployment's logs and `metrics.get` work, each kind read in all namespaces
  once (refused), then in the namespace.
  Built in the second run
  (2026-10-11): the generic custom-resource profile from a definition and its printer columns,
  kept current through a watch on the cluster's definitions
  ([`customresources.go`](../../internal/model/kinds/customresources.go),
  [`customkinds.go`](../../internal/engine/customkinds.go), `TestBudgetCustomResourceView`); a
  handle reports synced only once the initial list reached its subscribers
  (`TestSyncWaitsForTheInitialListToReachSubscribers`). Built in the fourth run (2026-10-12):
  covering scopes ([architecture §5](../developer/architecture.md#scopes-local-and-virtual-kinds),
  [`cover.go`](../../internal/cache/cover.go)): a label-selected scope reads from the running
  unselected scope of the same resource, namespace and metadata flag instead of a watch of its own,
  with handovers both ways whose events stay exact under churn (`TestCoverHandoversUnderChurn`,
  `TestBudgetSecretScopes`, `TestKindSecretScopes`).
  Built in the night run (2026-10-16): **discovery cached on disk**
  ([D-65](../developer/decisions.md#d-65-discovery-cached-on-disk), the lead's decisions, confirmed
  by the owner in the audit of 2026-10-16 where it asked; [`discocache.go`](../../internal/cluster/discocache.go)): one owner-only file
  per server and identity under `<user cache dir>/Cogpit/discovery`, shared by the app and
  `cogpit-cli`, used for the same server version, 6 hours once the definitions' digest confirmed
  it, else 10 minutes; a change of the definitions drops it from memory and disk
  (`TestDiscoveryCacheServesTheNextConnection`, `TestDiscoveryCacheFileIsCheckedBeforeUse`,
  `TestDiscoveryCacheIsPerIdentity`, `TestDefinitionsConfirmOrDropTheDiscoveryCache`,
  `TestPartialDiscoveryIsNotWritten`, `TestMapKindAsksAgainForAKindTheCacheDoesNotKnow`, through the
  engine `TestDiscoveryCacheAcrossConnects`: 8 discovery requests at connect without it, 2 with it).
  Measured on a real cluster the owner provides for tests (232 definitions, aggregated discovery,
  `TestRealDiscoveryCacheConnectTime`): the engine's connect 17.8 ms from the server, 11.2 ms from
  the file, 14.6 ms without a cache directory (medians of 7), 3 discovery requests, then 1; a run of
  `cogpit-cli get pods` 223 ms against 212 ms, within the runs' spread. **Memory per kind and per
  cluster** in the Diagnostics tab and `diagnostics_get`
  ([`cache/size.go`](../../internal/cache/size.go), `TestDiagnosticsReport`,
  `TestDiagnosticsCommands`, `TestDiagnosticsTab`): each object's heap as Go lays out its decoded
  map, remembered by UID and resourceVersion so a read walks only what changed (at most 1,000 per
  scope, the rest at the mean, `TestSizeMemo`), no API request; within 0.93–0.99 of the heap Go
  reports for the same objects per kind on the built-in kinds' fixtures (`TestMemoryEstimateAgainstTheHeap`)
  and 0.96–1.00 on the real cluster's objects of seven kinds (`TestRealMemoryEstimateAgainstTheHeap`).
  On the real cluster with eight views open (`TestRealBudgetsAndMemory`): 1,912 objects, 93.2 MiB
  estimated while the process's heap grew by 105.9 MiB; a pod 43.5 KiB, a definition 267 KiB; a
  read with the estimate 0.3 ms. Budgets P9 to P11 measured on the same cluster ([testing](../developer/testing.md#performance-budgets-proposed-not-measured)): 10 minutes idle without a list of a cached kind, a view reopened within its warm period without a request (its rows after 6 ms), an agent's `resources_find`, `ui_navigate` and `ui_state` on a warm cache without a request.
- **Impact:** every view, every MCP read and the agent depend on it; it decides budgets P1–P4 and
  P9–P11 and the load Cogpit puts on API servers.
  Fourth run (2026-10-12): the related-objects indexes are kept current by their kind's events
  instead of being rebuilt per lookup, and a drawer shares its indexes with its fetches and wakes
  only for the keys it shows: a node drawer's fetch on the demo cluster fell from 13–33 ms to
  2.6 ms (p50), its fetches from 14–43 % to 2.3–2.5 % of a core
  ([architecture §4.4](../developer/architecture.md#44-tables-details-editors-and-streams),
  `TestNodeDrawerUnderDemoChurn`, `TestLiveIndexBuiltDuringChurn`).
  Night run (2026-10-13): the diagnostics view, a Diagnostics dock tab (Help › Diagnostics,
  `diagnostics_get` for agents) with the connected cluster's requests by verb and resource, its
  watches open, warm or covered, and the objects per kind, from the counters, no request
  ([`diagnostics_view.go`](../../internal/engine/diagnostics_view.go), `TestDiagnosticsCommands`,
  `TestDiagnosticsTab`); memory per kind and per cluster since 2026-10-16 (Evidence).
- **Estimate:** 0–4 h, re-estimated 2026-10-16 (the lead's estimate): the two items of 2026-10-15
  are built; left is the definitions' schemas in the cache, 2–4 h if the owner wants them dropped,
  none otherwise. Was 4–12 h (2026-10-15, PhotoCraft's calibration: the discovery cache on disk and
  memory per kind, 2–6 h each; the namespace-scoped informers, the diagnostics view and the
  namespace-limited user's fixes of 2026-10-13 and 2026-10-14 had not moved the estimate), 16–38 (was 22–50; scopes with label
  selectors and metadata only, local and virtual kinds and the related-objects index built, third
  run).

### G4. Engine and command registry
- **Built 2026-10-16 (night run, [D-63](../developer/decisions.md#d-63-long-commands-as-background-jobs)):**
  long commands as background jobs for every origin. An action on a selection of more than ten
  objects (`object.deleteMany`, `workload.restartMany`, `flux.reconcileMany`, `flux.suspendMany`,
  `flux.resumeMany`, `flux.resetMany`, `argocd.application.syncMany`,
  `argocd.application.refreshMany`) asks for its confirmation as before, then returns at once with a
  job's dock tab whose steps are the objects: each object's progress, a failure kept to its object
  while the others go on, the counts, the outcome saying what was done, what failed and what was not
  attempted. `job.cancel` and the tab's Cancel button stop it, the request under way finishing; an
  agent's job gets an `ended` audit line ([`commands/manyjob.go`](../../internal/engine/commands/manyjob.go),
  [`engine/jobs.go`](../../internal/engine/jobs.go), [`fyneui/jobtab.go`](../../internal/ui/fyneui/jobtab.go);
  `TestDeleteManyAsAJob`, `TestDeleteManyJobCancelled`, `TestJobCancelKeepsItsTab`,
  `TestJobKeepsItsCommandsContext`, `TestJobTabCancel`, [PNG](../../internal/ui/fyneui/testdata/job_tab_delete_dark.png);
  on kind `TestKindDeleteSelectionAsAJob`). The lead's decisions, confirmed by the owner in the
  audit of 2026-10-16: ten and fewer within the call, any origin cancels any job, the bound of 500 kept. Estimate not moved.
- **Built 2026-10-16 (night run, later; [D-63](../developer/decisions.md#drain-as-a-job-built-2026-10-16-night-run)):**
  every drain is a background job too: `node.drain` asks for its confirmation as before, records the
  drain in its journal and returns at once with the job's tab; the steps are the cordon and one per
  pod (the cache's pods at the start, the drain's own list once it listed), a PodDisruptionBudget's
  wait in the pod's step; Cancel, the tab closed, another cluster or an interrupted connection stop
  it and leave the drain `stopped` in its journal, offered as after a crash; nothing is uncordoned by
  itself; an MCP client that gives up no longer stops it ([`commands/drainjob.go`](../../internal/engine/commands/drainjob.go);
  `TestDrainJobBudgetBlocksThenAllows`, `TestDrainJobCancelledLeavesAPendingDrain`,
  `TestDrainJobOutlivesItsCall`, `TestDrainJobStopsWhenInterrupted`; on kind 1.36.1
  `TestKindDrainCrashFinishUncordon` with a Cancel). Estimate not moved.
- **Missing (2026-10-16):** of the list this entry started with nothing: `Session`, the command
  registry with schemas, verbs, danger levels and disabled reasons, UI state in the engine,
  events, the confirmation policy per origin, the audit log and the command fuzz target
  (2026-10-11), `cogpit-cli`'s `contexts`, `get` and `mcp` (2026-10-13), and background jobs with
  cancellation (2026-10-15, A91: a job runs in a dock tab of its own that agents read, closing
  the tab or leaving the cluster stops it; [`engine/jobs.go`](../../internal/engine/jobs.go),
  `TestJobRunsInItsTab`, `TestJobStops`; since 2026-10-16 every action on a selection of more than
  ten objects, with Cancel, above). Jobs are not journaled: a crash ends one, each job's
  command saying why that is harmless (D-11); a drain keeps its own journal as before. Left long
  within its call: a multi-document Apply or Create (D-63, open); D-58's interrupted connection
  stops a job (decided at the merge, 2026-10-16). What new commands need comes with the gaps that
  add them.
- **Evidence:** built on 2026-10-11 ([`internal/engine`](../../internal/engine/)): `Session`, the
  registry with JSON schemas, verbs, danger levels and disabled reasons, UI state, events, the
  confirmation policy per origin (`TestDispatchConfirmationPolicy`), the audit log, `FuzzDispatch`,
  21 commands (41 after the second run, with the confirmation bound to its objects and their
  UIDs). Access reviews in `Enabled` built on 2026-10-11: a command whose verbs the connected
  user's reviews deny is greyed "not permitted: you may not …" in every menu and refused for every
  origin; answers never block a menu and arrive as events (`TestAccessReviewsAnswerWithoutBlocking`,
  `TestAccessReviewsGreyCommands`); `cluster.refreshAccess` asks again (the Cluster menu's Refresh Access Reviews, 2026-10-12), and the View menu, like the sidebar, leaves out the kinds the user may not list (2026-10-12). Then still missing: jobs and
  cancellation (built 2026-10-15, A91, above), `cogpit-cli` commands (built 2026-10-13, G2 and
  G17).
- **Impact:** every action, menu, shortcut and MCP tool dispatches through it.
- **Estimate:** 0–3 h, re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration):
  every item of the entry exists; up to 3 h for what the walks find in the engine itself (the rest
  of what they find is G16's). Was 10–28 (was 15–35; access reviews in `Enabled`, drawer-only
  commands, third run).

## Feature gaps

### G6. Resource views
- **Fixed 2026-10-16 (checked against a real cluster the owner provides for tests):** an event's
  Involved Object links to custom kinds too, as Lens's capture shows, through the definitions
  (`CustomKindsByGroupKind`, on the scope the custom kinds already watch); there 174 of 188 events
  were about Flux's and cert-manager's kinds, all unlinked before (`TestEventInvolvedCustomResourceLinks`).
  Estimate not moved.
- **Decided (owner, 2026-10-14 evening; built 2026-10-15):** the pod drawer's Resource Claims with their
  state, the pod's namespace's claims watched while the drawer is open (A105); a node device names the claim
  that holds it, as a link (A108; both in the Evidence below); an Application's logs in one merged
  tab ([G8](#g8-logs-terminal-and-shells), A81).
  Confirmed, now the owner's decisions: Applications' Violations (the workloads in a warning or error
  state), a VPA's Mode "Auto" when unset, no Pods table in Replication Controllers' drawers, Pods as
  the view a connect opens, the Overview's Issues rule. Beyond Lens, not counted in this gap
  ([D-30](../developer/decisions.md#d-30-certificates-in-secrets-decoded), the Secrets drawer's
  certificates): required, the kind of CA a certificate's chain leads to (a public one, one this Mac
  trusts, or an internal or self-signed one, with the root's name; its design settled when built);
  built 2026-10-14 (A99, A100), a combined PEM's key check and an encrypted key in the warning colour
  ([beyond Lens › Everyday use](beyond-lens.md#everyday-use)). Rows: [resource parity](resource-parity.md).
  Estimate not moved.
- **Checked against a real cluster (2026-10-16, [D-62](../developer/decisions.md#d-62-checks-against-a-real-cluster)):**
  a real cluster the owner provides for tests (Kubernetes 1.35, four nodes, Rook-Ceph, Flux, 232
  custom resource definitions): every served kind listed through `cogpit-cli get` without an
  error; the built-in kinds' cells equal the API server's own table in Lens's formats (one
  difference by design: an evicted pod's Status "Evicted"); the 55 custom kinds with objects equal
  it cell for cell; drawers and related objects read through `object_get`; P1 and P9–P11 measured
  ([testing](../developer/testing.md#performance-budgets-proposed-not-measured)). Found and fixed:
  events about custom resources unlinked (above), an OCI Helm repository Unknown (G23), owners
  that are custom resources unlinked in every drawer's Controlled By and the Pods list's (linked
  since 2026-10-16, `TestDrawerLinksCustomOwners`, `TestPodControlledByCustomResource`). Open:
  Lens's behaviour on such data is not captured. Estimate not moved.
- **Missing (2026-10-15):** no view: 64 of 64 exist, 1 done and 63 partial (`make parity`;
  [resource-parity](resource-parity.md); Pod Security Policies, the cluster Overview, Helm Charts,
  Applications, Vertical Pod Autoscalers and the four Device Resources views the last, partial
  since 2026-10-13). Left: the depth of the partial ones (metrics columns, texts and rules checked
  against a capture); whether Lens's workload drawers list their pods as Cogpit's do since
  2026-10-13, and where (not captured;
  [D-40](../developer/decisions.md#d-40-a-pods-table-in-the-workloads-drawers), below).
- **Evidence:** resource-parity, 27 of 64 (2026-10-11): Nodes, Pods, Deployments, Services,
  Namespaces, Events (the MVP); Daemon Sets, Stateful Sets, Replica Sets, Jobs, Cron Jobs
  ([`kinds/`](../../internal/model/kinds/) `daemonsets.go` … `cronjobs.go`; Cron Jobs with the
  captured columns and texts, Next execution from the schedule); Ingresses, Ingress Classes (the
  captured columns), Config Maps, Persistent Volume Claims; Definitions (no group filter) and the
  custom resource views (done: titled by Kind, grouped by API group, printer columns of priority
  0, following the definitions live); the ten Gateway API views, shown as generic custom resource
  views. Golden, status-text and fuzz tests per kind
  (`TestGolden` and `TestCustomGolden` over 119 synthetic objects), views filled from watches on kind
  (`TestKindEngineWorkflows`, `TestKindCustomResources`). Inferred and marked per row: most status
  rules, the Cron Jobs warning rule, the drawers of the new kinds. Third implementation run
  (2026-10-11): the 13 Config views beyond Config Maps and Secrets, all partial: Resource Quotas,
  Limit Ranges, Horizontal Pod Autoscalers, Pod Disruption Budgets (the captured columns with the
  warning icon and "N/A"), Priority Classes with the pod counts of 2026.9 (the related-objects
  index `PodsByPriorityClass`), Runtime Classes, Leases, Mutating and Validating Webhook
  Configurations, and the four admission policy views, the validating ones falling back to v1beta1
  and v1alpha1 through the profiles' `FallbackVersions`, the mutating ones at v1 only
  ([`kinds/`](../../internal/model/kinds/) `resourcequotas.go` … `validatingadmissionpolicybindings.go`;
  golden, status-text and fuzz tests per kind; `TestBuiltinAdmissionPoliciesFollowTheServedVersion`;
  the PDB list rendered offscreen). On kind 1.36.1 (`TestKindConfigViews`): a budget, a quota, a
  limit range and an autoscaler created in `cogpit-c` fill their views with what the controllers
  report, the priority classes count the cluster's pods, a node's lease links its node, and the
  runtime class, webhook and admission policy views sync (the test writes no cluster-scoped
  objects, so those show no rows). Inferred and marked per row: the status and warning rules, the
  HPA Metrics and Status forms, the Pods column's place, the drawers; Vertical Pod Autoscalers (a
  CRD) are not covered.
  Access Control (2026-10-11, third run): Service Accounts, Cluster Roles, Roles, Cluster Role Bindings and Role Bindings
  partial, with the captured columns ([`kinds/rbac.go`](../../internal/model/kinds/rbac.go) and
  one file per kind); the warning rules (a role or binding that grants nothing; a binding's
  missing cluster role read through the related-objects index `ClusterRolesByName`) and the
  drawers (rules and subjects as tables, links to roles and service accounts) inferred; 24
  synthetic objects in the goldens, the views filled on kind (`TestKindAccessControlViews`);
  the Create dialogs then still missing (built 2026-10-12, [G7](#g7-resource-actions); Pod Security
  Policies built 2026-10-13, below).
  Network and storage (2026-10-11, third run): Endpoint Slices, Endpoints, Network Policies, Persistent
  Volumes, Storage Classes and Replication Controllers partial (the documented columns; cell
  formats, status rules and drawers inferred; [`kinds/`](../../internal/model/kinds/)
  `endpointslices.go` … `replicationcontrollers.go`, golden, status-text and fuzz tests per kind);
  the related-objects index's first users: a claim's Pods column and drawer item
  (`TestClaimPodsColumnFollowsPods`), a node's Pods table as captured (`TestNodeDrawerPodsFollowPods`,
  under the demo's churn `TestNodeDrawerUnderDemoChurn`; its CPU and Memory from the metrics and
  every column sortable since 2026-10-13, [G12](#g12-metrics-and-cluster-overview)),
  a Service's endpoint slices in its drawer (`TestServiceEndpointSlices`); links to storage
  classes, volumes, claims and pods; on kind 1.36.1, a claim provisioned by the default class and
  mounted by a pod fills the Pods column, its volume, the node's Pods table and the Service's
  endpoint slices (`TestKindStorageAndNetworkViews`).
  Secrets and Helm (2026-10-11, third run): Secrets partial, then from a metadata-only watch (Keys
  and Type empty), the drawer's one GET per open, values masked and revealed in the GUI only;
  Helm Releases partial, a virtual kind over the `owner=helm` Secrets
  ([`releases.go`](../../internal/helm/releases.go), G11). Fourth run (2026-10-12), as the owner
  revised [D-13](../developer/decisions.md#d-13-cache): the Secrets view on a full watch, Keys and
  Type filled as in Lens, the drawer read from the cache without a GET, values still masked and
  revealed in the GUI only, and redacted in every result an agent reads
  ([`secrets.go`](../../internal/model/kinds/secrets.go), [`secretdata.go`](../../internal/engine/secretdata.go),
  `TestBudgetSecretsViewAndDrawer`, `TestAgentsReadSecretsRedacted`, `TestKindSecretsView`); Helm's
  releases read from that watch while it runs and from their own `owner=helm` watch otherwise
  (covering scopes, G3, `TestBudgetSecretsAndReleasesShareOneWatch`). Beyond Lens (2026-10-13, the
  owner's [D-30](../developer/decisions.md#d-30-certificates-in-secrets-decoded), not counted in
  this gap): the Secrets drawer decodes the certificates its keys hold, a group per key and a
  section per certificate with each field its own value, and agents read the same fields in
  `object_get` ([`secretcerts.go`](../../internal/engine/secretcerts.go),
  `TestSecretCertificateSections`, `TestObjectGetDecodesCertificatesForAgents`).
  Device Resources (2026-10-13): Resource Claims, Resource Claim Templates, Device Classes and
  Resource Slices partial, at `resource.k8s.io/v1` only as Lens reads them, with the columns, the
  claim's State texts and the warning icon's tooltip of Lens's documentation (no capture;
  [`resourceclaims.go`](../../internal/model/kinds/resourceclaims.go) and one file per kind; 21
  synthetic objects in the goldens, `TestDeviceResourcesColumns`, `TestResourceClaimsStatusTexts`);
  Resource Slices' Allocated from the related-objects index `ClaimsByPool` (`TestResourceSliceAllocated`);
  the pod drawer's Resource Claims section, with each claim's state since 2026-10-15 (A105,
  `TestPodResourceClaimsSection`, `TestPodResourceClaimStates`), and a node's devices with the claim
  that holds each (A108, `TestNodeDeviceClaims`);
  offered at v1 only (`TestBuiltinDeviceResourcesFollowTheServedVersion`), filled on kind 1.36.1
  (`TestKindDeviceResources`).
  Inferred and marked per row: which device conditions warn, the colours, the drawers. The node
  drawer's Devices section (night run, 2026-10-13): one section per resource slice that names the
  node, from the related-objects index `SlicesByNode`, then without the claim holding each device
  (with it since 2026-10-15, A108, above)
  ([`nodedevices.go`](../../internal/model/kinds/nodedevices.go), `TestNodeDevicesSection`,
  `TestNodeDrawerDevicesFollowSlices`, on kind `TestKindNodeDevices`; [resource-parity › Nodes](resource-parity.md#cluster)).
  Vertical Pod Autoscalers (2026-10-13): partial, Cogpit's first curated profile of a custom
  resource kind (`kinds.ProfileFor`, chosen by the custom kinds watch while the definition serves
  v1 or v1beta2): the documented default columns, the status rule, hidden Target, CPU and Memory
  columns and the drawer inferred ([`verticalpodautoscalers.go`](../../internal/model/kinds/verticalpodautoscalers.go),
  `TestVerticalPodAutoscalersFollowTheirDefinition`; on kind with the definition only,
  `TestKindVerticalPodAutoscalers`). The demo cluster serves no definitions, so it shows none.
  Applications (2026-10-13): partial, a virtual kind made of several kinds: the cached workloads
  grouped per namespace by `app.kubernetes.io/instance` ([`applications.go`](../../internal/model/kinds/applications.go)),
  served from a cache of its own whose source holds the workloads' handles only while something
  reads the applications ([`engine/applications.go`](../../internal/engine/applications.go);
  one watch per workload kind and no other request, `TestBudgetApplicationsView`; the handles go
  back after the view closes, `TestApplicationsReleaseTheWorkloads`); Logs as `application.logs`,
  a `workload.logs` tab per workload (`TestApplicationLogsOpensATabPerWorkload`); the demo's
  deployments carry the recommended labels. Inferred and marked: Status, Violations, which
  workloads count, the drawer.
  Pod Security Policies (2026-10-13, night run): partial, `policy/v1beta1` with the documented
  columns (Name, Privileged, Volume, Age), offered only where discovery finds the resource, so on
  clusters before 1.25 alone (`TestPodSecurityPoliciesOnlyWhereServed`, through client-go's
  discovery of a fake API server); the Privileged and Volume texts, no status token and the drawer
  inferred ([`podsecuritypolicies.go`](../../internal/model/kinds/podsecuritypolicies.go), five
  synthetic objects in the goldens, `TestPodSecurityPolicyDetail`, `FuzzPodSecurityPolicies`). Not
  on kind (1.36 serves none) and not checked against Lens on such a cluster. Fixed 2026-10-13: a
  reference to a policy (an event's involved object, a Helm release's resource) had no link, nor
  one to an admission policy, a binding or a Device Resources object: their kinds were missing from
  `model.BuiltinKindKey`, now checked against every built-in view (`TestReferencesLinkToEveryBuiltinKind`).
  A Pods table in the workloads' drawers (2026-10-13, night build,
  [D-40](../developer/decisions.md#d-40-a-pods-table-in-the-workloads-drawers), decided by the
  owner 2026-10-14): the drawers of Deployments, Replica Sets, Stateful Sets, Daemon Sets,
  Jobs and Cron Jobs end with the node drawer's Pods table, built by the same code, without its
  Namespace column since 2026-10-14 (the owner's; Name, warning icon, Node, Ready, CPU, Memory,
  Status), the pods the workload controls by the
  controller reference's UID in its namespace (a Deployment's through every replica set it
  controls, a Cron Job's through its jobs), CPU and Memory from the metrics and the sorting as a
  node's ([`workloadpods.go`](../../internal/model/kinds/workloadpods.go); `TestWorkloadDrawerPods`,
  `TestWorkloadDrawerPodsPerKind`, `TestWorkloadDrawerPodsFollowPods`, `TestWorkloadDrawerPodsMetrics`,
  `TestWorkloadDrawerPodsSort`, `TestDeploymentDrawerPodsTable`; on kind `TestKindWorkloadDrawerPods`).
  The cost decided first: its indexes (`PodsByOwner`, `ReplicaSetsByOwner`, `JobsByOwner`) are the
  profiles' `DetailNeeds`, not `Needs`, so only an open drawer holds them and the workload lists do
  not rebuild on a pod change (measured: one Deployments snapshot for ten pod changes and one
  Deployment change, eleven with the indexes in `Needs`; `TestWorkloadPodsIndexOnlyWithTheDrawer`).
  Every sortable drawer table, a node's Pods included, shows at most 500 rows in its order and
  counts the rest (`engine.MaxDrawerTableRows`, Cogpit's default).
- **Estimate:** 10–56 h (was 12–60; Pod Security Policies built; the rest is depth: captures, metrics columns; not moved by the workloads' Pods, which the estimate did not count). Checked 2026-10-15, not moved: A105 and A108 were the owner's additions of 2026-10-14 evening, outside the depth the range spans.

### G7. Resource actions
- **Fixed 2026-10-16 (seen on a real cluster the owner provides for tests):** every action's
  confirmation named its objects by kind key, "Drain nodes worker-1", "Delete helmreleases web",
  above an Objects row that said "Node worker-1"; it names them by kind now, "Drain Node worker-1"
  (`TestConfirmMessageNamesKinds`, [PNG](../../internal/ui/fyneui/testdata/confirm.png);
  [D-62](../developer/decisions.md#d-62-checks-against-a-real-cluster)). Lens's dialogs are still not
  captured. Estimate not moved.
- **Fixed 2026-10-16 (checked against a real cluster the owner provides for tests):** the default
  Drain refused a node for its pods with emptyDir data before evicting any, and Cogpit's toast and
  error said the node stays "cordoned and partly drained"; they now say "stays cordoned; no pod was
  evicted", or the count of pods evicted (`TestFailedDrainSaysWhatItLeft`). The unfinished-drains
  banner still says "partly drained" (the journal keeps no count). Estimate not moved.
- **Built 2026-10-14 (A1):** an agent's create or change of any RBAC kind (Role, Cluster Role, Role
  Binding, Cluster Role Binding) always waits for the user, also where agent writes are allowed
  ([H-27](../security/privilege-footprint.md#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)).
- **Built 2026-10-14 (A74):** Restart on a selection of workloads (`workload.restartMany`), Scale
  staying on one object ([resource parity › Create dialogs](resource-parity.md#create-dialogs)).
  Built 2026-10-14 (A101): a grant's confirmation says "Grants these rights immediately,
  cluster-wide" or "in <namespace>", and "This cannot be undone." stays only where something is lost.
  Confirmed: the node shell's bounds, 5 minutes to 24 hours, default 2 hours. Estimate not moved.
- **Missing (2026-10-15):** no action: 42 of 42 exist (`make parity`, every id in the registry;
- **Checked against a real cluster (2026-10-16, [D-62](../developer/decisions.md#d-62-checks-against-a-real-cluster)):**
  through the engine with a confirming hook: Scale, Restart, scale to zero, port forwards to a
  Service and a pod, Logs, Attach, Shell and Delete (`TestRealWorkloadActions`); Cordon and
  Uncordon (`TestRealNodeCordon`); Drain, refused for emptyDir pods, then 20 pods evicted in 39 s,
  no eviction blocked by Rook-Ceph's budgets (`TestRealNodeDrain`); the node shell, its pod gone
  after (`TestRealNodeShell`); a public chart's install, upgrade, rollback and uninstall
  (`TestRealHelm`); Flux's Reconcile, Suspend and Resume (`TestRealFlux`); 27 kind tests ran
  unchanged against it (create, edit, conflict and delete among them). Found and fixed: a failed
  drain's wording, confirmations naming kind keys (above). Estimate not moved.
  Share is not a target, owner 2026-10-11). Left: depth, Lens's dialogs captured (drain, scale,
  the eight create dialogs; Cogpit's are inferred) and the inferred rows checked; the node shell's
  second, unprivileged profile if the owner wants it. On a selection: Delete, Restart and the Flux
  CD and Argo CD actions, more than ten objects as a background job with each object's progress in
  the dock and Cancel since 2026-10-16 ([G4](#g4-engine-and-command-registry)); Scale and the others
  stay on one object (Cogpit's default). Drain runs as a background job with a step per pod and
  Cancel since 2026-10-16 ([G4](#g4-engine-and-command-registry)).
- **Evidence:** resource-parity: Logs, Shell, Restart (Deployment), Delete (pods only) partial (the
  MVP); Cordon, Uncordon, Drain, Scale (Deployment, Stateful Set), Trigger, Suspend, Resume (Cron
  Job) and Evict (Pod) partial (2026-10-11), as engine commands with the captured places and
  applicability, tested against client-go's fake clientset and the kind cluster
  ([`commands/nodes.go`](../../internal/engine/commands/nodes.go),
  [`commands/workloads.go`](../../internal/engine/commands/workloads.go)). Restart (Daemon Set,
  Stateful Set) and Scale (Replica Set) partial (third run, 2026-10-11): kubectl rollout restart's
  patch, compared with the one kubectl computes (`TestRestartWorkloadSendsKubectlsPatch`),
  confirmed for every origin ([`commands/workloadactions.go`](../../internal/engine/commands/workloadactions.go)). Drain uses kubectl's
  drain library and is crash-safe ([D-11](../developer/decisions.md#d-11-crash-stance)): a journal
  records it before the node is cordoned, and the next connect of its context offers to finish,
  uncordon or dismiss it ([`drains.go`](../../internal/engine/drains.go)). The row menus and the
  drawer's icons show them from the registry (G5, `TestRegistryMenusAsLens`); Scale and Drain
  open a dialog generated from their schemas, prefilled (the replicas from the cache, the drain
  options with their defaults). Still open for these: Lens's drain and scale dialogs (not
  captured; drain defaults inferred). The drain test passed on a three-node kind cluster
  (2026-10-11, `make test-kind-drain`, `TestKindDrainCrashFinishUncordon`: a drain blocked by a
  PodDisruptionBudget and a simulated crash leave the journal entry, the next session offers it,
  finishing and uncordoning clear it).
  Fourth run (2026-10-12, [`journallock.go`](../../internal/engine/journallock.go)): the drain and
  Helm journals are shared by every Cogpit process of the user under a file lock, and each entry
  names its process, so a drain or Helm action running in another live Cogpit is reported running
  there, not offered as interrupted, and cannot be finished or dismissed from here
  (`TestJournalsKeepEveryProcessesEntries`, `TestJournalEntryOfAKilledProcess`; [architecture §8](../developer/architecture.md#8-helm-internalhelm)).
  Restart and Scale send the UID they act on in their patches (2026-10-12), which the API server
  checks: a workload (Deployment, Daemon Set, Stateful Set, Replica Set) created again under the name is never restarted
  or scaled, whatever the origin, also when Cogpit's cache has not seen it yet
  (`TestKindPatchesBoundToTheUID` on Kubernetes 1.36.1, `TestRestartAndScaleCarryTheUID`;
  [architecture §4.2](../developer/architecture.md#42-the-command-registry)).
  Create dialogs (2026-10-12, [resource-parity › Create dialogs](resource-parity.md#create-dialogs)):
  Namespace, Service Account, Role, Cluster Role, Role Binding, Cluster Role Binding and Resource
  Quota partial, behind their views' "+", as engine commands that check the fields, dry-run and
  create ([`commands/createdialogs.go`](../../internal/engine/commands/createdialogs.go),
  [`createdialog.go`](../../internal/ui/fyneui/createdialog.go); `TestKindCreateDialogs` on
  Kubernetes 1.36.1); their fields are inferred, Lens's dialogs not captured. Bindings to
  cluster-admin and cluster-wide ones are confirmed for every origin (the lead's default, not the
  owner's decision), and every binding an agent asks for waits for the user, also where agent
  writes are allowed (the owner's decision, 2026-10-14;
  [H-27](../security/privilege-footprint.md#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)).
  Create Secret (2026-10-13, night build; G9): the eighth dialog, `secret.create`, the user's
  only (D-19) ([`commands/secretcreate.go`](../../internal/engine/commands/secretcreate.go),
  [`createdialog_secret.go`](../../internal/ui/fyneui/createdialog_secret.go)).
  The node shell (2026-10-13, night build; [D-37](../developer/decisions.md#d-37-the-node-shell),
  decided by the owner 2026-10-14; G8): `node.shell`, Lens's privileged pod in `kube-system`
  with nsenter, confirmed for every origin naming the namespace, the node and the image, first in
  the node's row menu and drawer as captured, the pod deleted with its tab and at quit
  ([`commands/nodeshell.go`](../../internal/engine/commands/nodeshell.go); `TestKindNodeShell`).
  Every one of the 39 actions of that day existed then; 42 of 42 since 2026-10-15, with
  `application.logs` (A81, [G8](#g8-logs-terminal-and-shells)), `configMap.saveData` (D-55,
  [G9](#g9-yaml-editor-and-create-resource)) and `secret.saveData` (D-59, the night run). Still open: captures of Lens's dialogs.
  Delete acts on the table's selection since 2026-10-13 (`object.deleteMany`, one confirmation
  naming every object; G9, G5), and more than ten objects run as a background job since
  2026-10-16 (G4); the other actions stay on one object (Cogpit's default, inferred).
- **Estimate:** 8–35 h, estimated (was 12–45; no action missing since 2026-10-13: Helm install, the Secret's dialog and the node shell built; what is left is depth (Lens's dialogs captured, the inferred rows checked) and the node shell's second, unprivileged profile if the owner wants it). Checked 2026-10-15, not moved: A1, A74 and A101 were the owner's additions of 2026-10-14 evening, outside the depth the range spans.

### G8. Logs, terminal and shells
- **Checked and fixed 2026-10-20:** a log opened again names its pod and container, never the pod's
  UID or the container's instance. After an interruption a workload's container stream opened again
  at once, by the name, while the cache still showed the pods as of the interruption; a Stateful
  Set's pod created again under its name meanwhile, or a container that restarted meanwhile, was
  then followed by that stream and, once the cache showed it, by a stream of its own: its lines came
  twice, every new line too. After the server ended a stream the same happened when the cache
  showed a restart only after the backoff. A pod's tab showed the new pod's log after the marker as
  if it went on from its last line. Reproduced in engine tests, not seen in a real window. Now a
  stream opens again only while the cache shows its instance running, and the rescan hands it the
  instance the cache shows next instead of starting a second stream; a log opened again whose first
  line is newer than its last says so in a `[cogpit]` line
  ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections), the lead's decisions, open
  to revision; `TestWorkloadLogsAPodCreatedAgainDuringAnInterruption`,
  `TestAPodLogCreatedAgainDuringAnInterruption`). Left: a stream's first request names the pod too
  (a rescan from a cache that replays an interruption's restarts one by one), not reproduced.
  Estimate not moved.
- **Built 2026-10-15 (night run, A25, [D-50](../developer/decisions.md#d-50-kubectls-signature-check)):**
  a kubectl download's signature, certificate and Rekor entry checked with sigstore-go v1.3.0
  against an embedded sigstore trusted root (`make sigstore-root`), the entry asked for at
  `rekor.sigstore.dev` before the binary, no fallback when the log cannot be asked
  ([`verify.go`](../../internal/kubectl/verify.go), [`rekor.go`](../../internal/kubectl/rekor.go);
  `TestRealReleaseSignature`, `TestRealReleaseRefused`, `TestDownloadWithoutTheLog`); the lead's
  decisions, open to revision in D-50. Estimate not moved.
- **Built 2026-10-15 (A81):** an application's logs in one merged tab, each line
  `<workload>/<pod>/<container>`, with a Workload selector beside the Pod and Container ones
  (`application.logs`; `argocd.logs` and `flux.logs` for Argo CD Applications and Flux
  Kustomizations, the lead's decision, open to revision,
  [architecture §4.4](../developer/architecture.md#44-tables-details-editors-and-streams)). Estimate
  not moved.
- **Built 2026-10-14 (A84, #169):** a logs tab's Download saves what the tab shows, its filter
  applied while Filter is on (`TestLogDownloadSavesWhatTheTabShows`); the Jump to time field, whose
  placeholder was cut after "10m" at a window width of 1440 points, is as wide as the placeholder
  needs (`TestLogJumpFieldShowsItsPlaceholderWhole`). Estimate not moved.
- **Built 2026-10-14 (A26, A27):** the hand-over that puts the verified kubectl first for bash
  (`--init-file`) and fish (`-C`) as for zsh, and Preferences › Kubernetes › Enforce kubectl, a
  shell function `kubectl` in zsh, bash and fish
  ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first)).
- **Decided (owner, 2026-10-14 evening; built 2026-10-14 and 2026-10-15 but the upstream offer,
  which comes last):** an Application's logs in one
  merged tab, each line `<workload>/<pod>/<container>`, with a filter per source (built 2026-10-15,
  above); a logs tab's
  Download saves what it shows, its filter applied (built 2026-10-14, above); kubectl's signature checked with sigstore-go, the
  Rekor transparency log's proof included (a D-entry,
  [H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search);
  built 2026-10-15, above);
  the fork's four shell tests under the race detector in CI (built 2026-10-15: seven races of the
  fork's `RunLocalShell`, its directory check and `ExitCode` patched, upstream candidates not
  offered, [D-14](../developer/decisions.md#d-14-terminal-widget)); the two
  newer terminal-fork patches offered upstream later, at the end, the app first. Confirmed, now the
  owner's decisions: older log lines only through the row or `logs.loadOlder`; init containers
  listed apart; ⌃C to the shell on Windows and Linux; the right-click copy and paste preference the
  user's only; a verified, matching kubectl downloaded earlier first without asking; the cluster's
  terminal namespace beating the filter's; the node shell's bounds. To check, a defect: whether the
  logs' Jump to time field is cut at a window width of 1440 points (it was; fixed 2026-10-14,
  above). Noted from reading on
  2026-10-14: a reopened log stream names the pod and container, not the pod's UID, so a Stateful
  Set pod recreated under the same name while the cache lags more than 500 ms may show its lines
  twice; checked 2026-10-20, real, fixed (above). Rows: [ui parity › Logs](ui-parity.md#logs),
  [› Terminal and shells](ui-parity.md#terminal-and-shells). Estimate not moved.
- **Missing (2026-10-15):** no row: the 17 rows of [Logs](ui-parity.md#logs) and
  [Terminal and shells](ui-parity.md#terminal-and-shells) exist, 1 done (the scrollback, D-56) and
  16 partial. Left: the terminal's own theme and font family (the rows' missing parts); the
  partial rows checked against Lens and in a real window; the fork's patches offered upstream, at
  the end (the owner, 2026-10-14 evening). Built since this list first named them (Evidence): the
  terminal tab on `fyne-io/terminal` over Cogpit's own PTY with its own kubeconfig, exec and
  attach streams, the node shell with the safeguards of
  [privilege footprint](../security/privilege-footprint.md#node-shell), the log viewer (follow,
  filter, timestamps, previous container, download, several pods).
- **Evidence:** [ui-parity](ui-parity.md#logs), 17 rows across logs and terminal on 2026-10-15, 16
  partial and the scrollback done (13 of 16 partial on 2026-10-13; 10 before, 4 before the third
  run): the terminal tab on Cogpit's own PTY with
  its own kubeconfig, the pod shell over exec, the log view with follow ([`internal/terminal`](../../internal/terminal/),
  [`termview`](../../internal/ui/fyneui/termview/)); from the third run (2026-10-11): attach to a
  container's main process ([`terminal/attach.go`](../../internal/terminal/attach.go)), and the
  log options as engine state of the tab (`logs.setOptions`): timestamps, previous container, word
  wrap, search with a filter and `/`, download, and the logs of a controlling resource
  (`workload.logs`, `logs.controller`: every pod's containers from the cache, joining while
  following, each line named) ([`engine/logs.go`](../../internal/engine/logs.go),
  [`engine/workloadlogs.go`](../../internal/engine/workloadlogs.go),
  [`logtab.go`](../../internal/ui/fyneui/logtab.go)). Night run (2026-10-13): the log tab's pod
  and container selectors (`logs.sources`, `logs.setSource`): a controlling resource's merged
  stream narrows without opening the streams that stay again, and every stream carries the
  server's timestamps ([`engine/logsources.go`](../../internal/engine/logsources.go),
  [`logtab_sources.go`](../../internal/ui/fyneui/logtab_sources.go)); older lines on an
  explicit "Load older lines" row (`logs.loadOlder`), asked as a longer tail and put on top
  without repeating a line ([`engine/logsolder.go`](../../internal/engine/logsolder.go),
  [`logtab_older.go`](../../internal/ui/fyneui/logtab_older.go)); a jump to a time from a text
  field (`logs.jumpTo`, in the user's zone and region format since D-31, as the timestamps show), searching the held lines first and asking `sinceTime` only for what is
  not held ([`engine/logsjump.go`](../../internal/engine/logsjump.go),
  [`logtab_jump.go`](../../internal/ui/fyneui/logtab_jump.go)); every Logs row now exists,
  checked on kind by `TestKindLogSourcesOlderAndJump`. The node shell since 2026-10-13 (night
  build, [D-37](../developer/decisions.md#d-37-the-node-shell)): a dock tab whose terminal waits
  for the privileged pod with a watch and enters the node with nsenter through the pod shell's
  exec stream ([`terminal/nodeshell.go`](../../internal/terminal/nodeshell.go),
  [`engine/nodeshell.go`](../../internal/engine/nodeshell.go); `TestNodeShellWaitsThenStreams`,
  `TestKindNodeShell`, [PNG](../../internal/ui/fyneui/testdata/node_shell_dark.png)); the dock row
  updated. Since 2026-10-14 its pod is journaled before the create and one a crash left is deleted
  at the next connect to its context (the owner's decision,
  [H-71](../security/privilege-footprint.md#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)). The terminal
  widget (2026-10-12, fourth run): the patched copy survives every input found by fuzzing it
  alone, without the escape filter (`FuzzHandleOutput`, `FuzzRawWidget`; the crash, hang and
  race patches in [PATCHES.md](../../third_party/fyne-terminal/PATCHES.md)), and its tests run
  in CI (`make test-fork`); its patches offered upstream since 2026-10-12 as #155 to #158, of which
  #157, #155 and #156 are merged (2026-10-14 to 2026-10-17); #158's changes the maintainer asked for
  on 2026-10-19 are pushed (2026-10-22), waiting for his review
  ([H-19](../security/trust-boundaries.md#h-19-the-terminal-widget-trusts-its-input)).
  Fourth run (2026-10-12): a workload's logs resume a follow stream the server ended while its
  container runs, from the newest line's timestamp without repeating lines, and forget ended pods
  and container instances (`TestWorkloadLogsResumeADroppedStream`,
  `TestWorkloadLogsForgetEndedInstances`; [architecture §4.4](../developer/architecture.md#44-tables-details-editors-and-streams)).
  Since 2026-10-13 (G13's Preferences and Cluster settings): the local terminal's shell, its
  arguments and the terminals' font size are preferences, and a cluster's terminals start in its
  working directory with its namespace (`TestLocalTerminalOptionsFromTheSettings`); two rows
  partial (font size, shell path and arguments), one updated.
  Night run (2026-10-13): copy and paste with the right mouse button, a preference off by default
  (Preferences › Terminal, `terminalRightClickCopyPaste`) that only the user may set, applied to
  every open terminal at once; ⌃Ins and ⇧Ins copy to and paste from the clipboard on Windows and
  Linux; both are patches of the fork, upstream candidates ([PATCHES.md](../../third_party/fyne-terminal/PATCHES.md))
  ([`termpaste.go`](../../internal/ui/fyneui/termpaste.go), `TestTerminalRightClickFollowsThePreference`,
  `TestRightClickAction`, `TestCopyPasteKeys_WindowsAndLinux`). One row from missing to partial, one
  updated; estimate not moved then (re-estimated 2026-10-15, below).
  Night run (2026-10-13, D-3 built): a terminal checks its `kubectl` against the cluster's version
  when it opens and offers the matching release, downloaded on the user's click from `dl.k8s.io` or
  a mirror and verified against the published SHA-256 and the release's sigstore signature, first
  on the terminal's `PATH` in the terminals opened afterwards ([`internal/kubectl`](../../internal/kubectl/kubectl.go),
  [`engine/kubectl.go`](../../internal/engine/kubectl.go); `TestCheckKubectl`,
  `TestDownloadKubectl`, `TestZshKubectlFirstAfterTheProfile`); the terminal row updated, the
  platform page's `kubectl` row (not counted) and G13's Preferences row partial.
- **Impact:** triaging a failing pod (logs, then a shell) is the most common Lens workflow.
- **Decided (owner, 2026-10-12, [D-14](../developer/decisions.md#d-14-terminal-widget)):** the
  patched terminal widget stays; its patches are offered upstream and it is fuzzed on its own,
  the two inputs that still make it panic alone patched first (3–8 h). Built 2026-10-12 except
  the offer, which the owner put at the end (2026-10-14 evening: the app first).
- **Estimate:** 7–25 h, re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration):
  the terminal's theme and font family 2–6 h; the reopened stream's check 1–2 h; the 16 partial
  rows at 0.25–1 h, 4–16 h; sending the upstream series under 1 h. The right-click copy and paste,
  the kubectl download and its signature check, the merged application logs, the download of what
  a tab shows and the scrollback had not moved the estimate. Was 13–34 (was 16–40; the node shell
  built 2026-10-13, about 3–6 h of it); the terminal widget's fuzzing and patches done (fourth
  run).

### G9. YAML editor and create resource
- **Built 2026-10-15 (the owner's F3, [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)):**
  a Config Map's values edited in the drawer, each key above its editor, Save at the end of the data: a review
  of the diff and a server dry run (`configMap.reviewData`), then `configMap.saveData` with the
  resourceVersion precondition, the Edit tab's write path; a conflict is shown, never overwritten unasked.
  Estimate not moved.
- **Built 2026-10-15 night (the owner's audit answers, [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)):**
  Save rebases a Config Map's edited values onto a newer version none of the edited keys changed in,
  the diff and the dry run against it shown before it writes (`configMap.reviewData`'s `from`), the
  conflict as built otherwise; the drawer's unsaved values kept in memory and as a draft (0600)
  offered at the next connect (`configMap.keepEdits`, GUI-only). The same night a Secret's values
  edited in the drawer after Show, in the window only ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)):
  `secret.reviewData` (a masked diff, the dry run, the rebase) and `secret.saveData`, GUI-only, the
  unsaved values in the window's memory only. Estimate not moved.
- **Built 2026-10-16 (the owner's audit decision, N152 (b), [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer),
  [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)):** a Config Map's and a
  Secret's drawer add keys (Add Key, the name checked as the API server checks it) and remove them
  (a bin icon per key, Undo), saved with the values in one write (`add` and `remove` on the review
  and save commands); a save that removes a key is destructive, the review naming the keys removed
  with the object, its namespace and its context and serving as the confirmation; the rebase counts
  an added key that appeared and a removed key that changed as a conflict. Estimate not moved.
- **Built 2026-10-16 (the owner's audit decision of 2026-10-16):** a copy icon over a Secret's editor
  in the drawer, in the key's line between Hide and the remove button, that copies the whole value
  as typed through the clipboard rule for Secret values (concealed, cleared after 60 seconds),
  beside ⌘C and ⌘X; an added key's editor has one too, a Config Map's editors none
  ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer),
  [D-25](../developer/decisions.md#d-25-copying-values); `TestSecretEditorCopyIcon`,
  `TestConfigMapEditorsHaveNoCopyIcon`). Estimate not moved.
- **Decided (owner, 2026-10-14 evening; built 2026-10-14):** after a create from a dialog the new object's
  drawer opens and the list scrolls to its row (A102, [G5](#g5-app-shell-navigation-tables-and-drawer));
  the grants' confirmation wording (A101, [G7](#g7-resource-actions)). Confirmed: Delete of a
  selection at most 500 objects. Estimate not moved.
- **Missing (2026-10-15):** no row: the 5 rows of [Editor and create resource](ui-parity.md#editor-and-create-resource)
  exist, all partial, the create dialogs among them (the Secret's since 2026-10-13, the other seven
  since 2026-10-12, G7) with their YAML fallback, a Secrets path (built 2026-10-12,
  `TestSecretEditAndCreateAreGUIOnly`; [D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml):
  Secrets editable and creatable from YAML in the GUI only, agents refused; since 2026-10-15 night
  a Secret's values in its drawer, D-59). Left: Lens's dialogs, diff view and Secret drawer
  captured, the partial rows checked.
- **Evidence:** [ui-parity](ui-parity.md#editor-and-create-resource): 5 rows partial on 2026-10-15 (4 partial and 1 missing before 2026-10-12);
  [resource-parity](resource-parity.md#every-kind): Edit and Delete partial. Built on 2026-10-11:
  an Edit tab per object on the editable [`yamlview`](../../internal/ui/fyneui/yamlview/), filled
  from the cache; Save shows the line diff against the object as the cache holds it, then a
  server-side dry run, and Apply writes it as an update with the resourceVersion the tab started
  from, so a concurrent change comes back as a conflict with the new diff and is never overwritten
  ([`engine/editor.go`](../../internal/engine/editor.go)); Create resource tabs with Cogpit's own
  templates and multi-document create, each object dry-run first
  ([`engine/create.go`](../../internal/engine/create.go)); Delete for every kind with the cached UID
  as precondition; all of it as commands agents drive too, tested against fakes, fuzzed, and on kind
  (`TestKindEditCreateConflictDelete`). Edit and Delete are greyed "not permitted" when the access
  reviews deny `update` or `delete` on the object (2026-10-11, `TestEditAndDeleteFollowAccessReviews`,
  `TestKindAccessReviews`).
- **Delete of a selection (built 2026-10-13, night run):** `object.deleteMany` deletes up to 500
  objects of any kinds after one confirmation for every origin that lists every object (its message
  the first ten and the count), their namespaces and the context; each object is deleted with its
  own confirmed UID as the precondition, one failure does not stop the others and is reported with
  its reason, nothing is retried; the deleted objects leave the selection, which `ui.select` sets
  with modes for agents and the window alike ([architecture §4.2](../developer/architecture.md#42-the-command-registry),
  [`commands/selection.go`](../../internal/engine/commands/selection.go); `TestDeleteManyOneConfirmation`,
  `TestDeleteManyPartialFailure`, `TestDeleteManyNeedsTheAgentsAcceptance`). Inferred, Cogpit's
  defaults: the bound of 500, the wording, the other actions staying on one object. In the window
  the row checkboxes, the header's selection bar and the selection's row menu (G5,
  `TestSelectionMenu`, [PNG](../../internal/ui/fyneui/testdata/delete_selection_dark.png)); on
  kind 1.36.1 `TestKindDeleteSelection` (the UID precondition refuses an object created again
  while the dialog was open, the others are deleted). Estimate not moved then (re-estimated 2026-10-15, below).
  Since 2026-10-16 ([G4](#g4-engine-and-command-registry), D-63) more than ten objects are deleted
  by a background job: the command returns at once with the job's dock tab, which shows each
  object's step, and Cancel stops what is not attempted yet (`TestDeleteManyAsAJob`;
  `TestKindDeleteSelectionAsAJob` on kind).
- **Drafts of unsaved editor text (built 2026-10-13, night run; [D-11](../developer/decisions.md#d-11-crash-stance)):**
  an Edit or Create resource tab's unsaved text is written 2 seconds after it stops changing (at
  least every 10 seconds while it keeps changing) to `<user config dir>/Cogpit/drafts/<id>.json`,
  0600 in a 0700 folder, atomically; it goes when the text is saved again (applied, or created in
  full) or the tab is closed, and a quit writes what still waits. On the next connect of its
  context a banner offers each draft with Restore and Discard (`editor.drafts`,
  `editor.restoreDraft`, `editor.discardDraft`; agents list them without their text); a restored
  Edit tab is based on the version the draft was based on, so a change on the cluster meanwhile
  comes back as a conflict before anything is overwritten. Never for a Secret or Helm values
  (D-19) ([`engine/drafts.go`](../../internal/engine/drafts.go),
  [`fyneui/drafts.go`](../../internal/ui/fyneui/drafts.go); `TestDraftWrittenAfterTypingStops`,
  `TestDraftRemovedWhenSavedOrClosed`, `TestNoDraftsForSecretsOrHelmValues`,
  `TestDraftsOfAnotherProcess`, `TestDraftRestoredAfterARestart`, `TestCreateTabDraft`,
  `TestDraftsBannerOffersAndActs`, PNGs [dark](../../internal/ui/fyneui/testdata/drafts_banner_dark.png),
  [light](../../internal/ui/fyneui/testdata/drafts_banner_light.png)). Defaults of the night run, not the owner's decision:
  the delays, closing a tab discarding its text without asking, the offer on connect rather than
  at start, Discard without a confirmation. Estimate not moved then (re-estimated 2026-10-15, below).
- **Revert to original (built 2026-10-13, night run):** "Revert to Original" in an Edit tab's
  footer and in the review a Save shows puts the object as the cache holds it now into the tab and
  bases the edit on that version (`editor.revert`); "Undo Revert" brings the replaced text back
  with the version it was based on, so an apply of it still meets the conflict, until the user
  types or saves (`editor.undoRevert`, `canUndoRevert` in `editor.get`)
  ([`engine/editrevert.go`](../../internal/engine/editrevert.go),
  [`fyneui/editortab_revert.go`](../../internal/ui/fyneui/editortab_revert.go); `TestRevertToOriginal`,
  `TestEditTabRevertsAndUndoes`, `TestReviewRevertsToTheEditor`, PNGs
  [dark](../../internal/ui/fyneui/testdata/edit_revert_dark.png),
  [light](../../internal/ui/fyneui/testdata/edit_revert_light.png)). Inferred (Lens's paid diff
  view is not captured): the cluster's version as the original rather than the one the tab opened
  with, an undo instead of a question, the buttons' place and look. A Secret's tab reverts in the
  window only (D-19). Estimate not moved.
- **The Create resource tab's save and search buttons (built 2026-10-13, night run):** before the
  template picker, as captured. What Lens's do is not captured; Cogpit's reading (inferred): save
  writes the tab's text to a YAML file the user picks in the system's save panel (macOS since 2026-10-16, D-72; Fyne's dialog elsewhere), suggesting
  `<kind>-<name>.yaml`, a GUI action without a command as the CSV download is, and refuses a text
  that holds a Secret's manifest (Cogpit's default, D-19); search opens the editor's find widget
  ([`findbar.go`](../../internal/ui/fyneui/findbar.go)) below the editor
  ([`createtab_save.go`](../../internal/ui/fyneui/createtab_save.go); `TestCreateTabSavesToAFile`,
  `TestCreateTabKeepsSecretsOffDisk`, `TestCreateTabSearchOpensTheFindWidget`, PNGs
  [dark](../../internal/ui/fyneui/testdata/create_tools_dark.png),
  [light](../../internal/ui/fyneui/testdata/create_tools_light.png)). Estimate not moved.
- **Create Secret (built 2026-10-13, night build; [D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml)):**
  the Secrets view's "+" opens the eighth create dialog: name, namespace, type (Kubernetes' eight
  built-in types), the data as rows of a key and a masked, multi-line value with an eye that shows
  it, labels and annotations; a type fills in its keys (Cogpit's default). `secret.create` checks
  the name, the keys, the type's required keys and the sizes before anything is sent, dry-runs and
  creates with the values base64-encoded in `data` (Cogpit's default); it refuses agents and the CLI
  before anything is asked or sent, and no value reaches the audit log, an error, a toast or a draft
  ([`commands/secretcreate.go`](../../internal/engine/commands/secretcreate.go),
  [`createdialog_secret.go`](../../internal/ui/fyneui/createdialog_secret.go);
  `TestSecretCreateFromTheGUI`, `TestSecretCreateIsGUIOnly`, `TestSecretCreateChecksBeforeSending`,
  `TestSecretDialogSendsTheSecret`, PNGs [dark](../../internal/ui/fyneui/testdata/create_secret_dialog_dark.png),
  [light](../../internal/ui/fyneui/testdata/create_secret_dialog_light.png)). Inferred: the fields
  and their order (Lens's dialog is not captured). A shown value copies through the clipboard for
  Secret values, concealed and cleared after 60 seconds (2026-10-14, H-70 closed,
  `TestSecretDialogCopiesAShownValueConcealed`).
  Estimate not moved then (re-estimated 2026-10-15, below).
- **Estimate:** 5–17 h, re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration):
  a Secret's values in the drawer 2–6 h, Save rebasing 1–3 h and a Config Map's drawer drafts 1–3 h
  (the owner's audit answers, new work); the 5 partial rows at 0.25–1 h, 1–5 h. The create
  dialogs, Delete of a selection, drafts, Revert to Original and a Config Map's Save had not moved
  the estimate. Was 8–22 (was 10–25; Edit and Delete follow the access reviews, third run).

### G10. Port forwarding
- **Decided (owner, 2026-10-12, [D-20](../developer/decisions.md#d-20-a-port-forward-that-cannot-connect)):**
  a forward that cannot connect for a permanent reason (forbidden, unauthorized, the namespace
  gone) turns Failed at once and frees its port; passing errors keep the backoff; **built 2026-10-12**
  (404, 403 and 401 from the dial; `TestPortForwardFailsAtOncePermanentErrors`, `TestDialErrorsAreClassified`).
- **Missing:** Lens's status texts in the Port Forwarding view and its Edit dialog (capture
  needed; Cogpit's are inferred), the access review of the pod behind a forward before Edit offers
  it, a check in a real window that the browser opens (offscreen tests stop at Fyne's `OpenURL`).
  The Port Forwarding view and its actions count in G6 and G7.
- **Evidence:** [ui-parity](ui-parity.md#port-forwarding): 2 rows done, 1 partial. Built on
  2026-10-11: a forward manager in the engine ([`engine/portforward.go`](../../internal/engine/portforward.go),
  [`portforward_run.go`](../../internal/engine/portforward_run.go)) that listens on `127.0.0.1` only
  ([`cluster/portforward.go`](../../internal/cluster/portforward.go) `ListenLoopback`) and carries
  each local connection over a port-forward connection (WebSocket first, SPDY as the fallback, as
  kubectl 0.37); forwards are objects of the local kind `portforwards`
  ([`kinds/portforwards.go`](../../internal/model/kinds/portforwards.go)), so the view, the drawer,
  row menus and agents read them like cluster objects; reconnects come from the cache's events (a
  pod forward while its pod runs, a service forward to another ready pod of its selector) and from
  dial retries after 1 s doubling to 30 s; every forward of a context ends when Cogpit leaves it;
  commands `pod.portForward`, `service.portForward`, `portforward.open`, `portforward.edit`,
  `portforward.stop`, `portforward.remove`
  ([`commands/portforward.go`](../../internal/engine/commands/portforward.go)); the
  captured dialog and the port links ([`fyneui/portforward.go`](../../internal/ui/fyneui/portforward.go)).
  Tested against a fake forwarder and a fake kubelet (SPDY), and on kind: HTTP through forwards to
  the seed's service and pod, and a forward that reaches the replacement of a deleted pod
  (`TestKindPortForward`).
- **Estimate:** 2–6 h (was 10–20; built in the third run, left: Lens's status texts and Edit dialog by capture, a check in a real window). Checked 2026-10-15, not moved: the Missing list above still holds (`portforward.edit` checks the forward and the connection only, [`commands/portforward.go`](../../internal/engine/commands/portforward.go)).

### G11. Helm
- **Built 2026-10-14 (A2):** an agent's install or upgrade always waits for the user, whatever the
  chart holds, also where agent writes are allowed.
- **Decided (owner, 2026-10-14 evening; all of it built 2026-10-14 and 2026-10-15):** repositories with a dot in
  their name supported through an unambiguous chart id inside Cogpit (Helm itself writes
  repo/chart), carried through references, deep links and agents' parameters, saved ids migrated
  once, nothing changed upstream (built 2026-10-14, A36: the id doubles the repository's dots,
  `charts..example.nginx`; no file Cogpit writes held an id, so nothing was migrated;
  `TestHelmRepositoryWithADot`); an http or insecure-TLS origin marked in the Charts view's
  repository cell, the Install tab and the install and upgrade confirmations
  ([H-41](../security/trust-boundaries.md#h-41-repositories-whose-content-nothing-authenticates);
  built 2026-10-15, A14: the cell and the drawer, both tabs, the preview and an agent's
  confirmation, from the engine, so agents read it; `TestHelmChartOriginMarked`,
  `TestChartsViewMarksTheOrigin`);
  a chart's README
  reaches agents framed as text of the chart's author, not instructions (built 2026-10-14)
  ([H-86](../security/agents-and-mcp.md#h-86-a-charts-readme-reaches-agents)); nothing is offered
  after an interrupted install: Cogpit shows the release's state and the user cleans up in the Helm
  Releases view (built 2026-10-14, A17: the uninstall offered at the next connect gone, Show release
  in its place; D-11 for install); a release name in
  both storage drivers shown once, the one deployed last, with a drawer note "also stored in the
  <other> driver (older)" (built 2026-10-14, A39: the wording finished, `TestDeriveBothDriversNamesTheOlder`). Confirmed: a repository that appears in Helm's file while Cogpit runs is
  downloaded at once (H-42); an http repository's approval does not expire (H-40); the preview
  before Install; the stored chart for an upgrade to the release's own version; only the window
  chooses the configmap driver; the SQL driver out of scope. Rows:
  [resource parity › Helm](resource-parity.md#helm), [ui parity › Preferences](ui-parity.md#preferences),
  [beyond Lens › Crash recovery](beyond-lens.md#crash-recovery). Estimate not moved.
- **Required (owner, 2026-10-13, [D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)):**
  Cogpit reads Helm's own `repositories.yaml` regularly and takes over its changes by itself, within
  5 seconds, repositories with credentials included when their credentials are available.
  Repositories are added with the helm CLI, not in Cogpit, which writes nothing into Helm's file.
  Replaces the night run's list of Cogpit's own (public https only, Helm's file never read, added
  and removed in Cogpit). **Built 2026-10-13** (evidence below).
- **Followed from Lens's captures of 2026-10-12** ([resource parity › Helm](resource-parity.md#helm),
  built 2026-10-12, was 2–5 h): the drawer's title on one line without Details and YAML tabs, Chart
  as "<chart>-<version>", Namespace as text, a grey status pill, "User-supplied values only"
  unchecked at first, Save as a full-width bar; Rollback enabled with the current revision (a new
  revision equal to it, as helm does; what Lens does is not captured); the Upgrade tab's separate
  version picker, its info line with links, and the editor's find widget (`findbar.go`). Cogpit
  keeps upgrading with the release's stored chart where Lens needs a repository (deliberate), so
  the picker offers that version only and the info line's spinner lasts only while the values load.
- **Missing:** OCI registries (`oci://`, `helm registry login`; an OCI entry in Helm's file is
  listed as not used); a list of public repositories to pick (Cogpit adds none by design, D-29); the SQL storage driver
  (out of scope, the owner's decision of 2026-10-14 evening: Helm keeps such releases in a database outside the cluster, which Cogpit neither reads nor
  writes; the preference refuses it with that reason); captures of Lens's Charts view, chart drawer
  and Helm Install tab (Cogpit's are inferred), and of where Lens's Preferences show `HELM_DRIVER`.
  Views and actions count in G6 and G7.
- **Evidence (2026-10-14, night build HELM2, the charts' icons, [D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them),
  decided by the owner 2026-10-14):** the Charts view's first column (no title, no sort)
  and a chart drawer's header draw the icon the index names (inferred: Lens draws icons; not
  captured), fetched only for what the window shows while Preferences › Kubernetes › Load chart
  icons is on (on by default; only the user turns it on, agents may turn it off and never fetch
  one): https only, no credentials, cookies or referrer, at most 256 KiB, a host at a non-public
  address refused unless it is a repository's own, checked as an uploaded cluster icon is (PNG and
  JPEG kept as a PNG of at most 256 pixels, an SVG only when it refers to nothing outside itself),
  kept a week in `<user cache dir>/Cogpit/helm/icons/`; Cogpit's own helm-wheel glyph otherwise
  ([architecture §8](../developer/architecture.md#8-helm-internalhelm);
  [H-87](../security/trust-boundaries.md#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at),
  [H-88](../security/trust-boundaries.md#h-88-chart-icons-are-decoded-in-cogpits-process);
  `TestFetchIcon`, `TestFetchIconRefusesPrivateAddresses`, `TestFetchIconThroughTheProxy`,
  `TestChartIcons`, `TestChartIconRefusals`, `TestChartIconsPreference`; GUI
  `TestChartIconsInTheWindow`, `TestPreferencesLoadChartIcons`, PNGs
  `helm_chart_icons_{dark,light}.png`, `preferences_helm_driver_{dark,light}.png`).
- **Evidence (2026-10-14, night build HELM2, the chart's drawer):** the drawer shows one version
  of the chart, the listed one or the one picked in the "Version" select under its title
  (`ui.setChartVersion`, engine state that ends with the drawer; Install from the drawer installs
  it), with that version's properties from the index and its README (inferred from Lens's
  documentation of the Charts view; not captured): read from the version's archive through the
  install's path (the repository's credentials only for its own host unless
  `pass_credentials_all`, 16 MiB, checked against the index's digest) only when the drawer shows
  the chart or an agent's `object_get` asks, never for the list; cut at 256 KiB with a note,
  control characters and bidirectional overrides dropped; kept in memory and, when the digest
  verified the archive, in `<user cache dir>/Cogpit/helm/readmes/` (owner-only, the last 256);
  rendered with Fyne's RichText from Markdown, images never loaded (their alt text instead), raw
  HTML dropped, tables as lists of rows, links opened on a click for http and https only; agents
  read the text (Cogpit's default, [H-86](../security/agents-and-mcp.md#h-86-a-charts-readme-reaches-agents))
  ([architecture §8](../developer/architecture.md#8-helm-internalhelm); `TestChartReadme`,
  `TestCleanReadme`, `FuzzCleanReadme`, `TestReadmeStoreLoads`, `TestReadmeStoreBounds`,
  `TestHelmChartDrawerVersion`, `TestHelmChartDrawerInstallsItsVersion`,
  `TestHelmChartReadmeForAgents`, `TestHelmChartReadmeBounds`, `TestHelmChartReadmeCredentials`;
  GUI `TestChartDrawerReadme`, `TestChartDrawerReadmeStates`, PNGs
  `helm_chart_drawer_{dark,light}.png`).
- **Evidence (2026-10-13, D-29 built):** the repositories are Helm's own `repositories.yaml`,
  resolved as the helm CLI does in one place (`helm.RepositoryConfigPath`, on this Mac
  `~/Library/Preferences/helm/repositories.yaml`, the path printed, the file not opened), watched
  every 2 seconds by size, modification time and mode, read bounded and parsed with the SDK's
  `repo.File` once settled; a change reaches the Charts view in about 2 seconds, a broken file keeps
  the last good list with its reason, a missing one is an empty list with a hint. Credentials (user
  and password, also from the URL; client certificate and key; CA file; `insecure_skip_tls_verify`;
  `pass_credentials_all`) stay in memory and go only to the repository's own scheme, host and port,
  on redirects and for chart URLs elsewhere too, unless `pass_credentials_all`; plain http
  repositories used as helm uses them and marked, one with credentials only once the user allowed it
  in the window (owner, 2026-10-14, H-40); indexes in Cogpit's own
  cache keyed by name and URL (decided at the build); `helm.repo.add` and `helm.repo.remove` removed,
  `helm.repo.list` with the file and its status, the Repositories dialog read-only. Install and
  upgrade use these repositories unchanged, authenticated ones included
  ([architecture §8](../developer/architecture.md#8-helm-internalhelm), [D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml);
  `TestRepositoryConfigPath`, `TestReadRepositoryFile`, `TestReadRepositoryFileRefusals`,
  `TestAuthenticatedTLSRepository`, `TestCredentialsStayOnTheRepositoryHost`,
  `TestCredentialsNotSentToAChartElsewhere`, `TestPlainHTTPRepository`, `TestClientCertificateRepository`,
  `TestInsecureSkipTLSVerify`, `TestScrubbedErrors`, `TestHelmRepoChangesTakenOver`,
  `TestHelmRepoFileReadWhenSettled`, `TestHelmRepoRemovedWhileDownloading`, `TestHelmReposFromHelmsFile`,
  `TestHelmRepoFileChangesWithinFiveSeconds`, `TestHelmRepoFileErrors`, `TestHelmRepoCredentialsNeverShown`,
  `TestNoCommandWritesHelmsFile`; GUI `TestHelmReposDialog`, PNGs `helm_repos_dialog_{dark,light}.png`;
  on kind `TestKindHelmInstall`: an install from a repository behind a login, trusted through a CA
  file, both named in a temporary Helm file).
- **Evidence (2026-10-13, night run, the ConfigMaps storage driver):** releases Helm keeps in
  ConfigMaps (`HELM_DRIVER=configmap`) are read beside those kept in Secrets, a second
  label-selected watch joined to the first (`cache.Join`, covered by the Config Maps view's full
  watch while that runs), listed, drawn with their storage named (inferred), and rolled back,
  upgraded and uninstalled through their own driver; the verbs checked on the release's own
  records; new installs go where Preferences › Kubernetes › Helm storage driver says (only the
  user chooses configmap: Cogpit's default); a release ConfigMap's payload kept from agents as a
  release Secret's values are ([architecture §8](../developer/architecture.md#8-helm-internalhelm);
  `TestDeriveFromConfigMaps`, `TestDeriveMixedDrivers`, `TestActionsUseTheReleasesDriver`,
  `TestHelmConfigMapReleaseActions`, `TestHelmVerbsFollowTheReleasesDriver`,
  `TestHelmDriverPreference`, `TestHelmConfigMapPayloadsKeptFromAgents`,
  `TestBudgetHelmReleasesWithConfigMaps`, GUI `TestPreferencesHelmDriverSelect`; on kind
  `TestKindHelmConfigMapDriver`).
- **Evidence (2026-10-13, night run, the upgrade from repositories):** a release whose chart a
  repository added to Cogpit lists (by name) is upgraded to a version of it: the Upgrade tab's
  version picker with the repository's versions and a repository picker when several list the
  chart (inferred: Lens with a repository is not captured), the engine's choice (`helm.release.upgradeTab` with `repository`, `version`: the repository's versions newest
  first, prereleases after, the release's own marked; a new tab on the newest when it is newer,
  Cogpit's defaults), `helm.release.upgrade` with `version` and `repository`, the chart downloaded
  and checked against the index's digest before the journal records anything, the confirmation
  naming the chart version from and to and refusing when that changed while the user was asked
  ([architecture §8](../developer/architecture.md#8-helm-internalhelm);
  `TestHelmUpgradeTabOffersTheRepositorysVersions`, `TestHelmUpgradeFromARepository`,
  `TestHelmUpgradeFromARepositoryAgents`, `TestHelmUpgradeRepositoryRefusals`,
  `TestHelmUpgradeRepositoryCrashJournal`, `TestUpgradeToARepositoryChart`; GUI
  `TestHelmUpgradeTabRepositoryVersions`, PNGs `helm_upgrade_picker_{dark,light}.png`; on kind
  `TestKindHelmUpgradeFromRepository`: 0.1.0 installed, upgraded to 0.2.0 from the tab, an agent's
  upgrade back to 0.1.0, uninstalled).
- **Evidence (2026-10-13, night run; its repositories replaced by Helm's file the same day, D-29):**
  Charts partial and Install partial: public https chart repositories the user adds (`helm.repo.add`,
  `remove`, `update`, `list`; Helm › Repositories…), kept in Cogpit's own folders
  (`<user config dir>/Cogpit/helm/repositories.json`, the indexes in `<user cache dir>/Cogpit/helm/`;
  the lead's default, answered by the owner with D-29), indexes
  downloaded on add, on update and while the Charts view or an Install tab is open past a
  10-minute TTL, bounded and parsed with the SDK; the charts as the local kind `helmcharts`
  (Helm › Charts); the "Helm Install: <chart>" dock tab with the chart's values.yaml, its versions
  and the digest check; `helm.chart.preview` (the SDK's server-side dry run) and
  `helm.chart.install` (mutating, crash-safe, values withheld from agents and scrubbed from errors)
  ([resource-parity](resource-parity.md#helm), [architecture §8](../developer/architecture.md#8-helm-internalhelm);
  `TestHelmReposAddListRemove` (replaced by `TestHelmReposFromHelmsFile` with D-29), `TestHelmIndexRefreshedOnlyWhileShown`, `TestHelmInstallFromTab`,
  `TestHelmInstallCrashJournal`, `TestHelmInstallTabInstalls`; on kind `TestKindHelmInstall`).
- **Evidence (2026-10-12, fourth run):** Upgrade partial: Lens's "Helm Upgrade: <name>" dock tab
  and `helm.release.upgrade` through the Helm SDK (the release's own chart, values replacing the
  user-supplied ones, ten revisions kept), mutating, bound to the revision its values were read
  from, crash-safe (an interrupted upgrade is offered as a rollback to the revision it started
  from), values withheld from agents, the audit log, errors and logs; Lens's release drawer (the
  values in an editor with Save, notes, resources by kind) and Rollback dialog, which is the
  rollback's confirmation in the GUI ([resource-parity](resource-parity.md#helm),
  [architecture §8](../developer/architecture.md#8-helm-internalhelm); `TestHelmUpgradeTab`,
  `TestHelmUpgradeCrashJournal`, `TestUpgradeThroughTheSDK`, `TestRollbackDialogConfirms`; on kind
  `TestKindHelmUpgrade`).
- **Evidence:** [resource-parity](resource-parity.md#helm), 2026-10-11 (third run): Releases partial,
  Rollback and Delete (uninstall) partial; Charts, Install and Upgrade missing. Built: the release
  Secrets of the `owner=helm` watch decoded into the virtual kind `helmreleases` (memoized per
  Secret UID and resourceVersion, bounded; hostile payloads fuzzed), the drawer's history, values
  (withheld from agents) and manifest (Secret data redacted), and `helm.release.rollback` and
  `helm.release.uninstall` through the Helm SDK in-process, destructive, bound to the revision the
  user confirmed, crash-safe through a journal offered on the next connect
  ([`internal/helm`](../../internal/helm/), [`commands/helm.go`](../../internal/engine/commands/helm.go),
  [`helmops.go`](../../internal/engine/helmops.go); `TestHelmCrashJournalOffer`; on kind
  `TestKindHelmReleases`: a local chart installed and upgraded with the SDK, then listed, read, rolled
  back and uninstalled through the engine, and a simulated crash offered and finished). The
  drawer's history and manifest gave way to Lens's drawer in the fourth run (above).
- **Captured (2026-10-12, Lens Desktop on a real cluster):** the release drawer (Chart with an
  Upgrade button, Updated, Namespace, Version, Status; editable values with "User-supplied values
  only" and Save; Notes; Resources by kind), its icons (Rollback, Upgrade, Delete), the Rollback
  dialog (every revision, newest first, the current one selected), the Delete dialog, the Helm
  Upgrade dock tab, and a rollback keeping 10 revisions ([resource-parity](resource-parity.md#helm)).
- **Estimate:** 2–8 h (was 3–10; the chart drawer's README and the charts' icons built, D-46, 2026-10-14; before that 4–12, authenticated repositories from Helm's file built, D-29, 2026-10-13; the ConfigMaps driver built in the night run, 2026-10-13, estimate not moved). Checked 2026-10-15 (the lead's estimate, PhotoCraft's calibration), not moved: left are OCI registries, one checklist item, 2–6 h, and up to 2 h of fixes after the captures of Lens's Charts view, Install tab and Upgrade tab with a repository; the SQL storage driver is out of scope (the owner, 2026-10-14 evening) and no longer counted.
- **Still to capture in Lens** (night run, 2026-10-13): the Charts view (its icon column, search hint and order), a chart's drawer (its README, version picker and icon, built from the documentation 2026-10-14), the Helm Install tab (its fields, the default release name and namespace, what Install does first), the Helm Charts preferences, and the Upgrade tab of a release whose chart a repository holds (what "/<chart>" and "(no repository)" become, the picker's order, which version it starts on, prereleases, several repositories).
- **Still to capture in Lens** (the staging cluster's release with five revisions, one failed): what the Rollback dialog's button does with the current revision selected, what Save does with the box cleared, where the checkbox sits relative to the editor, the texts of states other than Deployed and Failed, what the find widget's Replace does with a regular expression's groups.

### G12. Metrics and cluster overview
- **Alerts, built 2026-10-17** (the owner's I4 and I5, [D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board); beyond Lens, not
  counted in this gap: [beyond Lens › Alerts](beyond-lens.md#alerts)): the cluster's Alertmanager,
  found by name among the cached Services or set on Cluster Settings, read through the service
  proxy every 30 s while the cluster is shown; the title bar's pill by the worst active alert and
  the alert board. Left: the pill and the board in a real window (the owner's walk); the address
  fields on Alertmanager's own section of Cluster Settings since 2026-10-18 ([D-87](../developer/decisions.md#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer)); an Alertmanager behind
  Mimir, Cortex or an authenticating proxy; silences are not planned (a write to Alertmanager, out of
  the ticket's scope); a lookalike found by name ([H-121](../security/privilege-footprint.md#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)).
- **Found in the owner's real-window walk (2026-10-14 evening):** no past data is shown for a node.
  The owner: take the past data from Prometheus where it is available. **Built 2026-10-14 night**
  (F1 with A43): the cause, verified in the code, was that a node's drawer and the cluster
  Overview read their CPU and memory history from cAdvisor's root cgroup alone, which
  kube-prometheus-stack drops (seen on kind 2026-10-12); now node-exporter's series where the
  cluster's Prometheus has them, matched by the node label or the node's name or addresses
  (kube-prometheus-stack's exporter carries only its host-network address), else the root cgroup,
  and the drawer and the Overview say when Prometheus has neither. Every chart was checked: the
  pods' and containers' history and the Overview's Pods history already came from Prometheus; the
  lists and the Pods tables have no charts; a node's Network and Filesystem still read the root
  cgroup and stayed empty on kube-prometheus-stack (built 2026-10-16 night: node-exporter's, below). Verified against fakes shaped like
  kube-prometheus-stack and the prometheus chart, and on kind with the prometheus chart's
  node-exporter; not against a real kube-prometheus-stack with node-exporter, nor the owner's
  cluster (since 2026-10-16 against a real Prometheus Operator installation on a cluster the owner
  provides for tests, below) ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics), the choices
  as the lead's decisions, open to revision in
  [D-43](../developer/decisions.md#d-43-where-the-nodes-disk-comes-from);
  [ui parity › Cluster overview and charts](ui-parity.md#cluster-overview-and-charts)).
- **Decided (owner, 2026-10-14 evening; all of it built 2026-10-14 night):** node CPU and memory history
  from node-exporter where it runs (kube-prometheus-stack), from cAdvisor otherwise (built
  2026-10-14 night, above); charts of one
  object whose ranges lie far apart get separate queries, each at its own step (built 2026-10-14 night; the question under
  "Waiting for the owner" below); an agent's `cluster.setPrometheus` becomes a write with
  confirmation, the window's as today ([H-26](../security/privilege-footprint.md#h-26-prometheus-is-found-by-name);
  built 2026-10-14 night, A16); the Overviews wait for the access reviews as the dashboards do
  (built 2026-10-14 night, A98); node-exporter and
  kube-state-metrics on in the kind cluster's Prometheus values, with kind tests of node and pod
  history from Prometheus ([G1](#g1-workspace-ci-and-progress-generators); built 2026-10-14
  night). Confirmed: the network
  and filesystem series asked for only while their chart is shown; a failed provider retried after
  one minute; a provider's status from its last answers, without a probe; ranges up to 30 days (the
  ranges beyond leave Missing below); the Overview's Pods history over the whole cluster for a
  namespace-limited user too; Prometheus history in a pod drawer of a refused namespace. Estimate
  not moved.
- **Required next (owner, 2026-10-12: "do not forget"):**
  - **metrics-server** (Kubernetes Metrics API, `metrics.k8s.io`): the pods' and nodes' current
    CPU and memory in the lists and the drawer, and as the source of the drawer's charts when no
    Prometheus is found (Lens: "Displaying metrics from Kubernetes Metrics Server").
  - **Prometheus** (through the API server's service proxy, detected per cluster): the drawer's
    resource graphs as Lens shows them, with a time range (1h by default), one graph for the whole
    pod and one per container, each in the variants **CPU, Memory, Network and Filesystem**; the
    containers' **requests and limits drawn as lines** in the graphs; **events as yellow markers**
    (vertical lines with a diamond, toggled by a flag button) at the time they happened.
  - **Interactive charts (owner, 2026-10-12):** hovering a chart shows the exact time and value
    under the pointer (each series' value, the request and limit, an event marker's event).
    **Built 2026-10-12:** a crosshair on the nearest data point, a dot per series and a panel with
    the exact time, the values, the request and limit and the event near a marker, on every chart
    and variant ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  - Captured from Lens with the owner (screenshot, 2026-10-12): the Metrics panel's range dropdown,
    the four variant buttons and the event-marker toggle, the source line, the time axis, a legend.
- **Built (2026-10-12, first slice):** the Metrics Server end to end. Provider detection per cluster
  through discovery, with "not served", "forbidden" and "unavailable" as states
  ([`internal/metrics`](../../internal/metrics/)); polling only while a view or drawer that shows
  metrics is open (a list every 60 s, an open drawer's object every 15 s, shared by every consumer
  of a scope, cached for a 15 s TTL, no request when nothing is open: P12 in
  [testing](../developer/testing.md#performance-budgets-proposed-not-measured)); CPU and Memory
  columns in the Pods and Nodes lists; the drawer's Metrics section for a pod (per container too) and a
  node with a chart, requests and limits as lines and event markers, every chart (the pod's and each container's) with its own range, variant buttons and events flag; `metrics.get` for agents; the
  demo cluster's synthetic Metrics Server. Design and numbers: [architecture §9](../developer/architecture.md#9-metrics-internalmetrics).
- **Built (2026-10-12, second slice, backend only):** Prometheus through the API server's service proxy
  ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)). Detection among the
  cached Services by the charts' and Lens's well-known names (kube-prometheus-stack, `prometheus-k8s`,
  `prometheus-operated`, the `prometheus` chart's `prometheus-server`, `prometheus` in `monitoring`,
  `prometheus` or `lens-metrics`) with one buildinfo probe per candidate (at most 3), cached like the
  Metrics Server (10 minutes found, 2 not), forbidden/unavailable/not found as states, an override
  (namespace, service, port, prefix) on the detector; the chain Prometheus, then Metrics Server; range
  queries for the pod and each container in CPU, Memory, Network (per pod) and Filesystem (usage, else
  read and write rates), requests and limits from the pod spec as before, the source line
  `Prometheus (<namespace>/<service>)`, per-chart ranges (`Session.SetMetricsRange`, 1 minute to 30
  days), node charts from cAdvisor's root cgroup; budget P13, tested against a fake Prometheus behind
  the fake API server's service proxy. **Not verified against a real Prometheus.**
- **Built (2026-10-12, third slice, the window):** the Network and Filesystem buttons enabled from
  `ObjectMetrics.Capabilities` (icons, names as tooltips; disabled with "needs Prometheus" without
  them); Network drawn as Receive and Transmit in bytes per second, a container's chart showing the
  pod's with the line "Network is per pod" (Lens has no network tab on a container: Cogpit's, by the
  owner's "each in the variants"); Filesystem as Usage in bytes or Read and Write per second, as the
  cluster's Prometheus has it; requests and limits only on CPU and Memory; a second series colour
  token held to contrast and colour-difference tests in both themes; byte axes in round KiB, MiB, GiB
  steps, dates on the time axis from a day's range; the hover naming every new series with its unit;
  ranges up to 30 days with Prometheus (5m, 15m, 30m, 1h, 3h, 6h, 12h, 1d, 2d, 7d, 14d, 30d; up to 1h
  with the Metrics Server), **each chart's range held by the engine** (`ui.setMetricsRange` for the
  GUI and agents, `ui.state`'s `drawerCharts`), the source line "Displaying metrics from Prometheus
  (<namespace>/<service>)" without the Metrics Server's "keeps no history" note
  ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)). **Captured:** the default
  range "1h", the four variant icons, the source line's wording up to the provider. **Inferred:** every
  range value but 1h, the Network and Filesystem series' names and units, the per-pod line.
- **Changed (2026-10-13, owner, [D-32](../developer/decisions.md#d-32-every-byte-amount-in-binary-units-with-one-decimal)):** every byte amount in binary units with one
  decimal: the Pods list's Memory ("19.5 MiB"), the charts' byte axes and hover panels alike
  ("192.0 MiB", "48.0 KiB/s"), the Overview's Memory headline; estimate not moved.
- **Checked against a real Prometheus (2026-10-12):** the `prometheus` chart on the kind cluster
  (`make kind-prometheus`, Prometheus 3.15.0, one-minute scrape): detection, a node's and a pod's
  CPU, memory, network and filesystem from range queries, the window's variants, ranges and hover,
  `ui.setMetricsRange`, `metrics.get` and budget P13, with memory equal to the Metrics Server's to
  the byte ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics) has the numbers).
  It found three faults the fake did not show, fixed: the filesystem metric per kind, a node's
  usage over its disks only, rate windows that fit a one-minute scrape. **Only against the fake:**
  kube-prometheus-stack, containers' filesystem usage, a refused service proxy, a route prefix,
  history longer than an hour.
- **Required (owner, 2026-10-12, evening):**
  - **A status line per provider under the charts:** whether Cogpit reached Prometheus and the
    Metrics Server and whether they answered as expected (found, forbidden, unavailable, the last
    good answer's age, the last error), so the user sees why a chart is empty or stale.
  - **History from Prometheus, then live values from the Metrics Server:** when a drawer opens,
    Prometheus is asked once for the chart's history; the chart then continues with the Metrics
    Server's measurements, to spare Prometheus. The Metrics Server has CPU and Memory only, so
    Network and Filesystem still come from Prometheus: proposed (lead, not decided) that they are
    asked for only while their chart is shown. A range change asks Prometheus once for that range.
  - **Falling back (owner, 2026-10-12):** both together is the normal case; Cogpit falls back to one
    of the two only when the other fails, cannot be reached or is not found, also while a drawer is
    open: a Metrics Server that stops answering hands the live values to Prometheus, a Prometheus
    that is gone leaves the Metrics Server's live values without history. The status line says
    which provider serves what.
- **Built (2026-10-12, fourth slice: the evening's requirements):** the hub holds both providers
  apart (`metrics.NewHubFor`, [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  A drawer that opens asks Prometheus once for its charts' range (CPU and memory: two range queries;
  once more on a range change), then the live values come from the Metrics Server's get every 15 s;
  history and live values meet without a point twice, and a hole after both were away is filled
  from Prometheus once. The network and the filesystem are asked of Prometheus only while a chart
  shows them (**the lead's proposal, confirmed by the owner 2026-10-14 evening**); a chart's variant is engine state
  for that (`ui.setMetricsVariant`, `ui.state`'s `drawerCharts`). Falling back as required, also
  while a drawer is open; a failed provider is asked again after **a minute (Cogpit's default, confirmed by the owner 2026-10-14 evening)** and
  takes its part back when it answers. The lists and `metrics.get` ask the Metrics Server and
  Prometheus only when it fails or is missing; Prometheus is looked for only then or when a drawer
  opens. **The status line** under the charts: a row per provider (Prometheus with
  namespace/service:port, the Metrics Server), its state (answering, not answering, not allowed, not
  found, not looked for yet) as an icon in a status token with the whole state as its tooltip, what
  it serves (history, live values, network, filesystem), the last good answer's age and the last
  failure's class (`unavailable (HTTP 503)`: never a response body); the same in `metrics.get`'s
  `providers` and `ui.state`'s `drawerMetricsProviders`. The wording and icons are Cogpit's
  (**inferred**); Lens shows a source line only, which now names both providers. **A missing value
  is an empty cell, not 0%:** the Nodes list's CPU at 0% beside a memory value came from a
  Prometheus instant query with memory but no CPU series for the node; such a part is now
  `Usage.Missing`, an empty cell, and a point left out of the chart. Tests against fakes:
  `TestHistoryThenLiveValues`, `TestNetworkAndFilesystemOnlyWhileShown`,
  `TestMetricsServerFailsPrometheusTakesOver`, `TestPrometheusGoneLeavesTheLiveValues`,
  `TestGapIsFilledOnceFromPrometheus`, `TestListsUseTheMetricsServerAndFallBack`,
  `TestBothProvidersBudget`, `TestPrometheusInTheDrawer`, `TestSourceRow` (`TestProviderStatusLine` until 2026-10-18),
  `TestMetricsVariantIsEngineState`, `TestUsageCellsLeaveAMissingPartEmpty`. Budget P13 per open
  drawer at 1 h showing CPU or Memory: was a query set of five or six every 30 s (600–720 an hour),
  now two queries when it opens. **Measured on kind** (2026-10-12, the cluster's metrics-server and
  the `prometheus` chart): 90 s with the Pods list and a pod's drawer showing CPU cost 0 Prometheus
  requests (was 16), 6 Metrics Server gets and 2 lists; with the network shown 6 Prometheus requests.
  The fallback watched live (`TestKindMetricsFallback`, both deployments scaled to zero and back,
  then restored): the Metrics Server's absence seen at the next poll (15 s) with Prometheus's values
  in the same poll, its return served 9 s after it was ready; Prometheus's absence seen at its next
  request (`unavailable (HTTP 503)`) while the live values went on, its return served 28 s after it
  was ready ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
- **Waiting for the owner (2026-10-12; answered 2026-10-14 evening: separate queries, each range at
  its own step; built 2026-10-14 night, the hour beside the week at 30 s and the week at its old ten minutes, a finer query set only where a range would get fewer than 60 points; the lead's choices in [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)):** charts of one drawer whose ranges lie far apart (a 7-day
  pod chart beside 1-hour container charts) share one query step, set by the longest range, so the
  short charts get about six points. Recommended: a query set of its own per range once the ranges
  differ by more than ten times (more requests only while ranges are mixed); the alternative, one
  range per drawer, gives up the per-chart ranges. Also open, no decision needed yet: the rate
  window of four minutes smooths charts of 5 to 15 minutes; revisit after kube-prometheus-stack (30 s
  scrape), possibly reading the scrape interval from Prometheus.
- **Built (2026-10-12): the cluster's Prometheus, set by the user:** `cluster.setPrometheus`
  (`namespace/service:port`, a path prefix, or `auto`) and `cluster.prometheus`, kept per context in
  Cogpit's own preferences (`metrics.json`, never the kubeconfig), applied at once (the open drawer
  asks the named service) and at the next connection before anything is detected; a dialog
  "Prometheus for <context>" from a button under the drawer's status line (Cogpit's placement:
  Lens's cluster settings page was not built; since 2026-10-13 Cluster Settings › Metrics has a
  Prometheus… button that opens this dialog; since the night of 2026-10-16/17 the fields are on
  that page, the dialog is gone and the drawer's button opens the page, below). **Cogpit's default, not the owner's:** agents may set
  it (`DangerUI`; it writes no cluster object; since 2026-10-14 an agent's change always waits for
  the user, A16, above). Lens's "Metrics source" (built 2026-10-14, below), query format, empty
  container filter and HTTPS (built 2026-10-16 night, below) ([ui parity](ui-parity.md), the Metrics provider row).
- **Checked against a real kube-prometheus-stack (2026-10-12):** chart 92.3.0 in its own namespace
  on kind (`make kind-kps`, `TestKindKubePrometheusStack`, then `make kind-kps-down`): detected as
  `cogpit-kps/kps-kube-prometheus-stack-prometheus:http-web` (a short release keeps "-stack" in the
  name: the name pattern missed it, fixed), a pod's history at the 30 s scrape with network and
  filesystem. **A node had no history there (until 2026-10-14 night):** the chart's kubelet ServiceMonitor drops cAdvisor's
  series without a pod, the root cgroup the node charts read; the node's chart shows the Metrics
  Server's live values only. Node history on kube-prometheus-stack from node-exporter, matched by
  the node's address: built 2026-10-14 night (A43, above).
- **Left of Prometheus:** nothing since the night of 2026-10-16/17 (the settings' fields on the
  page, the links, Thanos and VictoriaMetrics, below). A provider nothing asked since its last answer (Prometheus while only CPU and
  Memory are shown) is shown as of that answer, with its age: no probe keeps the status line current
  (confirmed by the owner 2026-10-14 evening, A44).
- **Built (2026-10-13, night run, M7): the cluster Overview** (`cluster/overview`, the navigator's first
  item; [ui parity](ui-parity.md#cluster-overview-and-charts)). The CPU, Memory and Pods charts of the
  master or the worker nodes over the drawer's ranges, with requests, limits, allocatable capacity and
  capacity as lines; CPU and Memory through a cluster scope of the hub (`metrics.ClusterScope`: the
  same loop, roles and fallback as a drawer, Prometheus's history once as sums over the nodes' root
  cgroups, then the Metrics Server's node list summed; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)),
  the lines and the Pods chart from the cache (since 2026-10-14 the Pods' history from kube-state-metrics, below; before, **Pods had no history from before the page opened:
  Cogpit's default**); the status line per provider under the charts; the Issues from the cache:
  pods and nodes with their views' warning, workloads short of ready pods, warning events of the last
  hour (**the rule is Cogpit's: Lens documents "system messages and warnings" only**); `cluster.overview`,
  `ui.setOverviewRange`, `ui.setOverviewNodes`; budget P14 ([testing](../developer/testing.md#performance-budgets-proposed-not-measured)).
  Inferred: the page's layout, colours, wording ("No issues found."), the default group (the workers),
  the newest-first order of the Issues. Differs: ranges stop at 30 days (Lens: 2 months). **Checked on
  kind** (2026-10-13, `TestKindClusterOverview`, the cluster's metrics-server and the `prometheus` chart):
  the master's hour from Prometheus (120 points at 30 s), the Metrics Server's sums after it (0.204 cores
  against metrics.get's 206m), 5 Prometheus requests when the page opened and none after, 3 issues
  (a crash-looping pod, its Deployment, its BackOff event).
- **Built (2026-10-13, night run): hidden metrics** (Cluster Settings › Metrics › Hide metrics from the
  UI, [ui parity](ui-parity.md#cluster-settings); [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  Per context, places (Cluster, Nodes, Pods, Containers) and measures (CPU, Memory, Network,
  Filesystem) as checks, "Hide all metrics" and "Reset"; hidden metrics leave the lists' columns,
  the drawer's variants and containers' charts and the Overview, and **nothing is polled for
  them** (no hub subscription for a list, drawer or Overview whose metrics are all hidden; counted
  at the demo's Metrics Server, `TestHiddenMetricsAreNotPolled`). **Cogpit's, inferred:** the
  entries (Lens's list is not captured) and the check rows in place of a multi-select. Left: a drawer
  that shows only the network or the filesystem still polls the live CPU and memory with them
  (built 2026-10-16 night: no longer, below).
- **Built (2026-10-13): CPU and Memory in a node drawer's Pods table, and the table sorts** (the
  owner's requirement after a look at a real window: "CPU and Memory are empty. They should come from
  the Metrics Server and be shown. And this list must be sortable: by Name, Namespace, CPU, Memory and
  Status"; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics) and §4.3). The
  table's CPU and Memory are the Pods list's columns and cells (the Pods profile's `UsageCells`, from
  the hub's latest measurement; a pod without one empty), polled while the drawer shows the table: one
  `list` of every namespace's pod metrics every 60 s, the Pods list's own scope (shared when both are
  open), released when the drawer closes or shows another object; Cluster Settings › Metrics hides
  the columns (Pods, CPU, Memory) and with both hidden nothing is polled. A click on a column's
  title sorts (again: descending) with the lists' header look and arrow; the order is engine state
  (`UIState.DrawerSort`, `ui.sortDrawerTable`, in `ui_state`), numbers and metrics by value, a pod
  without a measurement last both ways. **Cogpit's defaults, not the owner's:** every column sorts
  (the owner named five); the order is kept by the table's title for the session and in every tab;
  unsorted, the table comes by name with the arrow on Name, as a list. Workloads' drawers listed no
  pods then, so only a node's drawer had the table (the workloads' drawers have it since the night
  build of 2026-10-13, D-40, [G6](#g6-resource-views), filled and polled the same way). Tests: `TestNodeDrawerPodsShowMetrics`, `TestNodeDrawerPodsHiddenMetrics`,
  `TestNodeDrawerPodsPollWakesTheDrawer`, `TestSortDetailTable`, `TestDrawerSortIsEngineState`,
  `TestSortDrawerTable`, `TestNodeDrawerPodsSortByHeader`; PNGs sorted by Memory
  ([dark](../../internal/ui/fyneui/testdata/node_pods_metrics_dark.png),
  [light](../../internal/ui/fyneui/testdata/node_pods_metrics_light.png)). **Checked on kind**
  (2026-10-13, `TestKindNodeDrawerPods`, the cluster's metrics-server, the list polled every 5 s
  for the test): the control-plane node's 19 pods, 17 with CPU and Memory equal to `metrics.get`'s,
  the two without a measurement (a crash-looping pod, a finished Job's pod) last when sorted by
  memory, two pod-metrics lists while the drawer was open and none in the 12 s after it closed.
- **Built (2026-10-13, night): pod metrics for a user who may list pods in some namespaces only**
  ([D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only),
  a night decision of the lead's, decided by the owner 2026-10-14; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  The Pods list, a node's Pods table and `metrics.get` asked the Metrics API for every pod in all
  namespaces, which RBAC refuses such a user, so CPU and Memory stayed empty; they now list
  `pods.metrics.k8s.io` once per namespace the Pods view reads pods in, shared between them, only
  while they show, and the list of every pod stays for a user who may list pods everywhere. Measured
  at the HTTP level against a fake API server that enforces RBAC (`TestBudgetNamespaceLimitedUser`): 1
  list in `team-a` and none in all namespaces for a user whose one RoleBinding is there; 1 list in
  all namespaces for a cluster-admin, as before (budget P12). Left: the Metrics API's RBAC is not the
  pods', and a namespace that refuses its metrics list has the Metrics Server counted as failing for
  a minute; Prometheus, where it stands in, is not limited by namespace (D-39). Fixed the same
  night: Prometheus detection read the cached Services in all namespaces, which such a user is
  refused, waited 8 s and was not tried again for 2 minutes; it reads them per namespace and follows
  a refusal at once, so a Prometheus in the user's namespace is found
  (`TestPrometheusDetectionFollowsARefusal`).
  **Not tried in a real window** (offscreen only).
- **Built (2026-10-14): the Nodes list's usage bars** ([resource parity](resource-parity.md#cluster), the
  Nodes row; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)). CPU and Memory
  draw the share right-aligned in a slot as wide as "100%" and a bar filled to it, so a column's
  bars line up; the bar's colour is the accent, the warning colour from 80% and the error colour
  from 90% (**Cogpit's thresholds, not the owner's decision**; Lens's are not captured), the amounts
  behind it are the cell's tooltip ("7.410 of 7.800 cores (95%)"), a missing value an empty cell
  without a bar. The model carries the bar (`model.Cell.Bar`, `model.ShareCell`), so agents' rows
  have it too; the window draws it with two rectangles a reused cell makes once
  ([`tablebar.go`](../../internal/ui/fyneui/tablebar.go)). Tests: `TestUsageBars`,
  `TestNodeUsageBars`, `TestNodesListDrawsUsageBars`; PNGs
  ([dark](../../internal/ui/fyneui/testdata/nodes_usage_bars_dark.png),
  [light](../../internal/ui/fyneui/testdata/nodes_usage_bars_light.png)).
- **Built (2026-10-14): the Nodes list's Disk and a node drawer's disk**
  ([D-43](../developer/decisions.md#d-43-where-the-nodes-disk-comes-from), a night decision of the
  lead's, decided by the owner 2026-10-14; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  The root filesystem (`/`) as node-exporter reports it, through the cluster's Prometheus: one
  instant query for every node per poll (the list's 60 s) while the column shows, used as the size
  less the available bytes, a node matched by its node label, its instance as its name or as one of
  its addresses; never the kubelet's summary API (`nodes/proxy`, [privilege
  footprint](../security/privilege-footprint.md)). The same usage bar as CPU and Memory; without
  Prometheus, or with one without node-exporter, an empty column whose header's tooltip says why
  (a header tooltip is new: `TableSnapshot.ColumnTips`). A node's drawer polls the disks while
  open and shows "Disk (/): 172.0 GiB of 200.0 GiB used (86%)" under its chart (since 2026-10-18 a
  usage bar per real filesystem, the root first, the numbers in each row's tooltip, and the column
  the fullest filesystem, the owner's WF26,
  [D-90](../developer/decisions.md#d-90-a-nodes-disks-a-usage-bar-per-real-filesystem)). Hidden with the nodes
  or the filesystem (Cluster Settings › Metrics); not polled while the user puts the column away.
  **Measured** at the HTTP level against a fake API server and a fake Prometheus
  (`TestNodesListDiskBudget`, budget P13): a Nodes list with Prometheus costs one detection probe,
  the cache's one list and watch of Services, and one disk query per list poll beside the Metrics
  Server's node list; nothing after it closes. **Checked against a real node-exporter since
  2026-10-14 night** (A115, the kind cluster's Prometheus with node-exporter): its series carry the
  node label and the node's InternalIP as the instance, which the matching reads
  (`TestKindNodeExporterByAddress`); the column stayed empty on kind, whose node's `/` is an
  overlay node-exporter leaves out (no `mountpoint="/"` series; verified by the query through the
  service proxy), and shows its `/var` since D-90 (verified by the new query, 2026-10-18).
- **Built (2026-10-14): history that outlives a closed drawer** ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  When a drawer closes, the hub keeps its chart's CPU and memory series (the object's and each
  container's; the cluster Overview's sums too) and the next drawer of the object starts with them:
  reopening a pod's drawer shows its last minutes again with the Metrics Server alone, then a gap,
  then the new measurements; where Prometheus is found it fills the gap once and its history is
  asked for again as before. The window's lines break at a gap (a step longer than four times the
  series' usual step and two minutes), so no line crosses the time nobody measured, and the note
  under a chart without history reads "The Metrics Server keeps no history: Cogpit keeps what it
  measured, the last hour at most." **Night decision (the lead's, 2026-10-14, open to revision):** options (a) keep nothing, as before; (b) keep every closed chart for the session; (c)
  keep closed charts bounded by age, count and points, least recently closed dropped first; chosen
  (c), because (b) grows with every object a user clicks through and (a) throws away what the
  Metrics Server cannot give again. **Cogpit's defaults:** the last hour of each series (also how
  long a kept chart is good for), at most 50 charts and 50,000 points in all (`DefaultRetainObjects`,
  `DefaultRetainAge`, `DefaultRetainPoints`); the network and the filesystem are not kept (only
  Prometheus has them, and it is asked again). **Measured** (`TestRetainedHistoryIsBounded`): 1,000
  drawers closed with an hour of four series each leave 50 charts and 48,200 points, the heap 2.4 MB
  larger (the samples alone 2.3 MB at 48 bytes each). Tests: `TestHistoryOutlivesAClosedDrawer`,
  `TestRetainedHistoryExpires`, `TestRetainedHistoryLRU`, `TestSeriesRunsBreakAtAGap`,
  `TestReopenedDrawerChartBreaksAtTheGap`, `TestPodDrawerMetrics` (the demo cluster: the reopened
  drawer's chart starts at its kept first point); PNG
  ([dark](../../internal/ui/fyneui/testdata/pod_metrics_reopened_dark.png),
  [light](../../internal/ui/fyneui/testdata/pod_metrics_reopened_light.png)). Not kept across a restart.
- **Built (2026-10-14): the Overview's Pods history from Prometheus** ([ui parity](ui-parity.md#cluster-overview-and-charts);
  [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)). **Night decision (the
  lead's, 2026-10-14, open to revision):** options (a) the cache's counts since the page
  opened, as before; (b) kube-state-metrics' `kube_pod_info` (the pod's node) and
  `kube_pod_status_phase` through Prometheus, counted by the cache's rule (scheduled to a node of the
  group, not Succeeded or Failed); (c) cAdvisor's series, a pod counted while it has a container's
  memory series, which needs no kube-state-metrics but counts only running containers and has no
  node label on every chart; chosen (b), because it counts what the cache counts, so the history
  and the live counts meet without a jump, and kube-state-metrics comes with kube-prometheus-stack
  and the `prometheus` chart by default. One query for every node and one for the masters, with the
  CPU and Memory history (once per range, never incrementally: the cache counts after that), the
  workers every node less the masters; the cache's counts continue it from the page's first sample;
  without kube-state-metrics the chart is the cache's alone and the note under the charts says
  "Prometheus has no kube-state-metrics series". Budget P14: 2 range queries more when the page
  opens and per range change (1 without masters), measured against the fake Prometheus
  (`TestClusterPodsHistoryBudget`). Tests: `TestClusterPodCountQueries`,
  `TestClusterPodCountsWithoutKubeStateMetrics`, `TestClusterOverviewPodsHistory`,
  `TestClusterOverviewPodsHistoryNote`; PNG
  ([dark](../../internal/ui/fyneui/testdata/cluster_overview_pods_history_dark.png),
  [light](../../internal/ui/fyneui/testdata/cluster_overview_pods_history_light.png)). **Checked
  against a real kube-state-metrics since 2026-10-14 night** (A115, the kind cluster's Prometheus
  with it): `TestKindClusterOverview` asserts the Pods history's source, `kube-state-metrics`, 2
  pod-count queries when the page opens. Left: for a
  user who may list pods in some namespaces only, the history counts every namespace's pods and the
  cache's counts only theirs, so the chart drops at the page's opening (Prometheus is not limited by
  namespace, D-39; asking it for the plan's namespaces only was named the fix, not built). The owner
  confirmed the whole cluster's history as built (A47, 2026-10-14 evening, D-39), so the night run
  of 2026-10-16/17 did not build the namespaces' query; the drop stays, and how to close it is a
  question for the owner (the lead's decision, [D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)).
- **Built (2026-10-14): the Metrics source setting** (Cluster Settings › Metrics, [ui parity](ui-parity.md#cluster-settings),
  the Metrics provider row; [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  Lens's captured dropdown, Automatic, Prometheus, Kubernetes Metrics Server, No metrics, and a line
  naming the providers in use. **Cogpit's defaults, not the owner's decision:** Automatic is both
  providers, each for its part (the owner's requirement of 2026-10-12); a provider the setting turns
  off is never detected nor asked, leaves the status line and its results and the open charts start
  over; No metrics asks nothing (a state "this cluster's Metrics source setting turns metrics off"
  in place of the numbers); kept per context in the cluster's settings and set by agents with
  `cluster.settings.set` (it only narrows what Cogpit asks). Tests: `TestMetricsSourcePrometheusAlone`,
  `TestMetricsSourceMetricsServerAlone`, `TestMetricsSourceNone`, `TestMetricsSourceSetting`,
  `TestMetricsSourceFromTheSettings` (the demo cluster: No metrics empties the Pods list's CPU and
  `metrics.get` answers not served; Automatic brings them back; no request with metrics off is
  counted at the hub in `TestMetricsSourceNone`). Since 2026-10-17 kept as the two metrics
  integrations' switches ([D-76](../developer/decisions.md#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect)): an older `metricsSource` moved into
  them once, the dropdown showing and setting them; Cluster Settings › Integrations and the question
  at a new cluster's first connect show whether each provider was found, and detect the providers
  once when they show a cluster nothing looked at yet ([beyond Lens › Cluster
  integrations](beyond-lens.md#cluster-integrations)). Since the evening of 2026-10-17 a cluster
  without a choice of integrations asks no metrics at all until its question's pre-choice is
  answered ([D-82](../developer/decisions.md#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)): its
  Metrics source reads No metrics with the reason, and Check all or a tick looks for a provider.
- **Metrics come only from what the cluster runs (the owner, 2026-10-16):** Cogpit installs
  nothing into a cluster for its own features; it finds the Metrics Server, the Prometheus and the
  exporters the cluster has and reads them, and where it finds no provider the charts and the
  status line say so, as before. Lens Metrics (installing a bundled stack) is not a target: its
  counterpart Cluster Settings › Cogpit Metrics, built in the night of 2026-10-15/16, was withdrawn
  by the owner and removed the same day ([D-61](../developer/decisions.md#d-61-cogpit-metrics-the-bundled-metrics-stack));
  detection finds the prometheus chart's server under any release's name since then.
- **Built (2026-10-16 night, the lead's decisions, open to revision):** the Metrics provider
  row's rest: a query format select (Cogpit's presets: auto, Prometheus Operator, cAdvisor only),
  the empty-container filter and HTTPS, in the Prometheus dialog (on the settings page since the
  night of 2026-10-16/17, below) and `cluster.setPrometheus`,
  kept in `metrics.json`. A node's Network and Filesystem from node-exporter where it runs (its own
  interfaces, its block devices; the root cgroup's after `or` in the same query), checked against a
  real Prometheus Operator installation (kube-prometheus, whose kubelet ServiceMonitor drops the
  root cgroup as kube-prometheus-stack's does); budget P13 measured there: 1 Prometheus request in 90 s with a node's
  drawer showing CPU, 8 with the network
  ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics),
  [D-43](../developer/decisions.md#d-43-where-the-nodes-disk-comes-from)).
- **Built (2026-10-16 night, the lead's decisions, open to revision): the Prometheus setting's
  fields on Cluster Settings › Metrics** as Lens shows them (captured: the "Prometheus" dropdown,
  the empty-container filter, the auto-detected provider and path, HTTPS, the path prefix), in
  place of the dialog: the query format and "Filter empty containers" under PROMETHEUS; the
  service address (empty: found by name), a line naming the Prometheus in use ("Auto detect: in
  use Prometheus monitoring/prometheus-k8s:web.", from the hub's last answers, no request) and
  "Use HTTPS" under PROMETHEUS SERVICE ADDRESS; the path prefix, for an address only. A select or
  check sets the whole setting at once, a text field after the typing pause, through
  `cluster.setPrometheus` (an agent's still waits for the user, A16); a refusal shows under the
  address; the fields follow an agent's change; `cluster.prometheus` names the one in use
  (`inUse`, `inUseStatus`). The Prometheus… button under the drawer's and the Overview's status
  line opened this page until 2026-10-18; the dialog is gone. Since 2026-10-18 the fields are on
  Prometheus's own section (below). Tests: `TestPrometheusSettingsOnThePage`,
  `TestPrometheusSettingsNeedAConnection`, `TestPrometheusInUseLine`,
  `TestPrometheusOverrideFromTheSettings` (the one in use);
  PNGs ([dark](../../internal/ui/fyneui/testdata/metrics_prometheus_settings_dark.png),
  [light](../../internal/ui/fyneui/testdata/metrics_prometheus_settings_light.png); not connected:
  [dark](../../internal/ui/fyneui/testdata/cluster_settings_metrics_dark.png),
  [light](../../internal/ui/fyneui/testdata/cluster_settings_metrics_light.png)). The lead's
  decisions, which change what the owner sees: [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)
  ("The cluster's Prometheus, set by the user"). **Not tried in a real window:** the owner's walk.
- **Built (2026-10-16 night; replaced 2026-10-18 by the source row, below): the Prometheus's
  namespace and service as links** in the source line of the drawer and the Overview (Lens's, captured on a node's drawer: "Displaying metrics
  from Prometheus: <namespace>/<service>:<port>", the namespace and the service links): in
  "Displaying metrics from Prometheus (monitoring/prometheus-k8s) and Kubernetes Metrics Server"
  the namespace opens the Namespace, the service the Service, through `ui.navigate` as every
  drawer link does, in the drawer's cluster; a hand pointer and an underline over a link only;
  the line cut to its width as before. The provider status carries the place
  (`ProviderStatus.Namespace`, `ServiceName`; `metrics.get`'s and `ui.state`'s providers too).
  Cogpit's wording stays ("(namespace/service)", the port on the status line); the links' look is
  the drawer's links' (inferred). Tests (then): `TestSourceLineLinksThePrometheus`, `TestSourceSegments`,
  `TestClusterOverviewPage` (the Overview's links), `TestHistoryThenLiveValues` (the place); PNGs of
  the Overview ([dark](../../internal/ui/fyneui/testdata/cluster_overview_dark.png),
  [light](../../internal/ui/fyneui/testdata/cluster_overview_light.png)). Not tried in a real window.
- **Built (2026-10-16 night, the lead's decisions, open to revision, [D-71](../developer/decisions.md#d-71-thanos-and-victoriametrics-read-as-prometheus)):
  Thanos and VictoriaMetrics under their own names.** Detection among the cached Services, at most
  the three probes as before, finds Thanos's Querier and query frontend and VictoriaMetrics
  single-node and the cluster's vmselect (under `/select/0/prometheus`) by the names their charts
  and operators give them, ranked after a Prometheus by its own names; the source row and its
  tooltip (the source line and the status line until 2026-10-18) and Cluster Settings › Prometheus
  name them "Thanos" and "VictoriaMetrics", and a service the
  user names is named by the same rules. **Checked** against a real Thanos 0.40 Querier and query
  frontend on a cluster the owner provides for tests (named in the setting: a node's and a pod's
  history, 118 of 120 points from before the drawer opened, and the network) and against
  VictoriaMetrics single-node and a cluster on kind (`make kind-vm`, `TestKindVictoriaMetrics`);
  **not** checked: auto-detection picking one on a cluster without a Prometheus, Services an
  operator made, a store of several clusters ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics)).
  The owner sees it in the window only on a cluster whose Prometheus is a Thanos or a
  VictoriaMetrics: where both run, as on the test cluster, the Prometheus is found as before.
- **Built (2026-10-16 night): a drawer that shows only the network or the filesystem asks for no
  CPU and memory.** While no visible chart of a drawer shows CPU or Memory (every chart set to the
  network or the filesystem, or CPU and Memory hidden by Cluster Settings › Metrics), its poll asks
  the Metrics Server nothing and Prometheus no CPU and memory history, no gap and no fallback; the
  network and the filesystem go on at their step; a chart back at CPU or Memory gets its live
  values again and the time between filled once from Prometheus. A chart whose network or
  filesystem no provider has any more (Prometheus gone) falls back to CPU, and CPU and memory are
  asked again. `metrics.Subscription.SetUsage`, `drawerWatch.usageShownLocked`; the drawer's note
  "No metrics for this … yet." only under a chart of CPU or Memory. **Measured on a real cluster**
  (`TestRealClusterNodeDrawer`, a node's drawer at 1 h): 0 Metrics Server gets in 90 s with the
  network shown, 6 with CPU shown (before: 6 with either), the Prometheus requests as before (8 and
  1). Tests: `TestUsageNotAskedWhileNoChartShowsIt`, `TestUsageAskedWhenTheNetworkHasNoProvider`,
  `TestDrawerUsageShownByItsCharts`, `TestNetworkOnlyDrawerAsksNoLiveUsage` (through the engine and
  a fake API server: 0 live gets in 20 polls with the network shown and with CPU and Memory hidden,
  20 before the change), `TestNetworkOnlyChartNeedsNoCurrentUsage`, `TestPrometheusInTheDrawer`
  (11 requests after its range change, was 13: no chart there shows CPU or memory).
- **Built (2026-10-16 night, the lead's decisions, open to revision): the Overview's five
  partial rows as far as they go without a real window** ([ui-parity](ui-parity.md#cluster-overview-and-charts)).
  The area under every chart's series is filled as Lens's charts are (captured: "the chart's
  fill", missing since 2026-10-12): each run of points down to zero in the series' colour at a
  fifth, in the drawer's and the Overview's charts (`chartFill`; `TestChartFillsUnderTheSeries`,
  `TestLineAt`; the drawer's and the Overview's renders in testdata, e.g.
  [dark](../../internal/ui/fyneui/testdata/cluster_overview_dark.png),
  [light](../../internal/ui/fyneui/testdata/cluster_overview_light.png)); the Overview's source line
  links its Prometheus (above). Each of the five rows now says what keeps it partial: a capture of
  Lens's Overview (the look, the toggle, the Issues' rule) and of the drawer's ⋮ menu (its entries,
  not captured, so not built), a real window, and the 30-day range the owner kept. The fill's
  alpha and filling every series are the lead's decisions ([architecture §9](../developer/architecture.md#9-metrics-internalmetrics));
  the owner sees them in every chart.
- **Built (2026-10-18, the owner's WF24, WF25 and WF22, the lead's decisions, open to revision,
  [D-88](../developer/decisions.md#d-88-the-metrics-source-row-and-links-to-the-settings-only-when-needed), [D-87](../developer/decisions.md#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer)): the source row, and
  the provider settings on the integrations' sections.** Under the drawers' and the Overview's
  charts one row in place of the source line, the status lines and the Prometheus… button:
  "Prometheus ● 30 s ago" left of the row's centre, "Metrics Server ● now" right of it, the dot in the cluster
  dots' colours, the details in each one's tooltip; a link to the integration's section of the
  cluster's settings only while it is not reachable ("Set its address…", "Settings…"); a cluster
  whose integrations are not chosen offers "Choose integrations…". Where a found Prometheus runs is
  on its section of Cluster Settings, its namespace and service as links that close the page and
  open them. Cluster Settings: the integrations under a divider below Agents, a section each;
  Prometheus's fields on its section; Metrics names the two it reads, Lens's "Metrics source"
  dropdown gone from the page (a departure; `metricsSource` kept for agents). Tests:
  `TestSourceRow`, `TestSourceChipStates`, `TestSourceRowLinksToTheSettings`,
  `TestSourceRowUndecided`, `TestIntegrationSections`, `TestPrometheusSectionLinksItsService`,
  `TestMetricsSourceFromTheSettings`; PNGs of the Overview, the node drawer's history and the
  sections. Not tried in a real window.
- **Missing (2026-10-16):** the provider settings, the source row and the charts' fill tried in a real window (the
  owner's walk); the drawer's ⋮ menu (a capture first). Not to be built: the Overview's ranges
  beyond 30 days (the owner kept 30 days, 2026-10-14 evening). **Not built, by the owner's A47:** the
  Overview's Pods history of a namespace-limited user asked for the plan's namespaces only (listed
  here since 2026-10-15; the owner confirmed the whole cluster's count on 2026-10-14 evening). The
  drop at the page's opening it would have closed stays; the question is open
  ([D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)).
- **Evidence:** [ui-parity](ui-parity.md#cluster-overview-and-charts), its five rows partial (the four
  Overview rows since 2026-10-13, the drawer charts), plus the metrics rows of cluster settings (Hide
  metrics from the UI partial since 2026-10-13; the Metrics provider row partial, its Metrics source
  since 2026-10-14 (off the page since 2026-10-18), its query format, filter and HTTPS since
  2026-10-16, its fields on the page, Thanos and VictoriaMetrics since the night of 2026-10-16/17,
  the fields on Prometheus's own section since 2026-10-18; Lens Metrics not a target
  since 2026-10-16, D-61).
- **Estimate:** 1–4 h, re-estimated in the night run of 2026-10-16/17 (the lead's estimate,
  PhotoCraft's calibration), the figure the Summary table carries: the real-window check of the
  provider settings, the links and the fill 0.5–1 h; the namespace-limited user's Pods history
  0–2 h (none if the owner keeps A47 as built, 1–2 h for option (c) of D-39); the drawer's ⋮ menu
  once captured 0.5–1 h. The totals stay within their rounding (G1–G16 220–613 h, "~220–620").
  Before: 2–7 h, re-estimated 2026-10-16 afternoon when the owner withdrew Cogpit Metrics
  (the lead's estimate, PhotoCraft's calibration), the figure the Summary table carries: the
  provider settings' check in a real window 0.5–1 h; the settings' fields, links, other
  providers' names, the namespace-limited Pods history and the network-only drawer's poll 1–3 h
  together; the Overview's five partial rows 1–3 h. 3–10 h earlier on 2026-10-16, with Cogpit
  Metrics' node-exporter on a cluster without one, the bundle's upgrade path and the real window's
  checks at 1–4 h. Until 2026-10-16 7–21 h, re-estimated 2026-10-15
  (it said 40–80 until then): the Lens Metrics install
  2–6 h (assuming it installs its bundle through the Helm install that exists); the query format, the
  empty-container filter and HTTPS 2–4 h; a node's Network and Filesystem on kube-prometheus-stack
  1–3 h; the settings' fields, links, other providers' names, the namespace-limited Pods history
  and the network-only drawer's poll 1–3 h together; the Overview's five partial rows at 0.25–1 h,
  1–5 h. History: 40–80 h before the first slice; after the Overview (2026-10-13) the remainder
  (the settings rows, the Lens Metrics install, the Nodes list's bars and Disk, node history on
  kube-prometheus-stack, the Overview's Pods history) was about 15–35 h, and after 2026-10-14 (the
  bars, Disk, the kept charts, the Pods history) about 8–20 h (the settings rows, the Lens Metrics
  install, node history on kube-prometheus-stack, ranges beyond 30 days, checks against a real
  node-exporter and kube-state-metrics), neither applied to the Summary table then. Since: ranges
  beyond 30 days dropped by the owner, node history from node-exporter and the checks against a
  real node-exporter and kube-state-metrics built (2026-10-14 night).

### G23. Flux CD and Argo CD views
- **Fixed 2026-10-16 (checked against a real cluster):** an OCI Helm repository has no status since
  Flux 2.1 (no controller reconciles one); Cogpit showed six of them Unknown in the view and the
  dashboard, the flux CLI Ready. Now Ready unless suspended (`TestFluxStatusTexts`,
  `TestFluxSourceOf`). The dashboard's counts then matched `flux get` for every kind the cluster
  holds: Helm releases 46 ready and 2 not, Kustomizations 45 ready, 18 not and 1 reconciling,
  Helm repositories 53, one Git and one OCI repository. Estimate not moved.
- **Built 2026-10-15 (A91, D-33):** Reconcile with Source waits as the flux CLI does: a
  background job with a dock tab of its own asks each object of the chain in turn and waits for its
  controller through the cache (at most 5 minutes each), a source that ends not ready stopping the
  object; its steps and outcome are engine state that agents read with `ui_state`; closing the tab
  stops it (`TestFluxReconcileWithSourceWaitsForTheSource`, `TestKindFlux`). The lead's decisions, open to revision are in D-33. Estimate not moved.
- **Built 2026-10-14 (A90):** a user who may not list the custom resource definitions gets Flux's
  and Argo CD's kinds from API discovery with their curated profiles (every curated profile, the
  Vertical Pod Autoscalers' too; generic custom resources stay invisible), and both dashboards count
  what the user may read (`TestGitOpsDashboardsWithoutTheDefinitions`,
  [architecture §5](../developer/architecture.md#5-cache)). Estimate not moved.
- **Built 2026-10-14 (A34, A33):** the Flux actions' confirmations name the identity the controller
  acts as (its service account, or the object's spec.serviceAccountName;
  [H-64](../security/privilege-footprint.md#h-64-a-flux-action-is-carried-out-with-the-controllers-identity));
  Argo CD's Sync respects the AppProject's sync windows, refused or greyed with the reason in a deny
  window, Argo CD's RBAC not emulated
  ([H-67](../security/privilege-footprint.md#h-67-a-sync-acts-with-argo-cds-rights-not-the-users)).
- **Decided (owner, 2026-10-14 evening; both built, 2026-10-14 and 2026-10-15):** without the right to list
  the custom resource definitions, Flux CD and Argo CD are recognised from API discovery and shown
  with their curated profiles (generic custom resources stay invisible without the definitions;
  built 2026-10-14, above); Reconcile with Source waits as the flux CLI does: a background job
  waits, with a timeout, for the source to be ready, then annotates the object, its progress in the
  dock (D-33; built 2026-10-15, above).
  Confirmed: Argo CD's labelled Secrets stay whole in the cache
  ([H-66](../security/credentials.md#h-66-argo-cds-credentials-are-in-cogpits-memory-while-its-settings-are-read)).
  Rows: [resource parity › Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views).
  Estimate not moved.
- **Missing (2026-10-15):** no view, dashboard or action of either section: Flux CD since
  2026-10-13, Argo CD since the night build of 2026-10-13/14 (below; both rows partial in
  [resource-parity](resource-parity.md#paid-lens-desktop-views)). Left: "Left (Flux CD)" below
  (Lens's views captured to check against, the Flux objects' events on the dashboard, a curated
  profile of Flux's Helm Charts) and Argo CD's left in its bullet (a running Argo CD and Lens's
  views to check against, one dialog for a sync with prune). Built since this list first named
  them: Argo CD's profiles of Applications, Application Sets and Projects with columns, status and
  searchable fields, Repositories and Clusters as virtual kinds, its dashboard, Sync with Prune and
  Refresh Normal or Hard on one Application or a selection
  ([D-35](../developer/decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show),
  [D-36](../developer/decisions.md#d-36-how-argo-cds-sync-writes-the-operation); `TestKindArgoCD`).
- **Built (Flux CD, 2026-10-13), partial:** the navigator's Flux section as captured, left out
  without Flux; curated profiles of the thirteen Flux kinds (GitRepository, OCIRepository,
  HelmRepository, Bucket, ExternalArtifact, Kustomization, HelmRelease, ImageRepository,
  ImagePolicy, ImageUpdateAutomation, Alert, Provider, Receiver) at the versions Flux serves, older
  ones as fallbacks: columns, the Status of [D-34](../developer/decisions.md#d-34-the-flux-dashboard),
  searchable fields, drawers, links to sources, charts, providers and image repositories; the Flux
  dashboard (D-34); Reconcile, Suspend and Resume on objects and selections
  ([D-33](../developer/decisions.md#d-33-the-flux-actions)); MCP: `flux.dashboard` and the six
  actions. Columns, drawers, the dashboard and the actions are inferred (Lens's are not captured).
  Since 2026-10-14 (D-33, decided by the owner): Reconcile with Source (Kustomizations, Helm
  releases) and Reset (Helm releases) as the flux CLI does them, on objects and selections, four
  more MCP actions; Force stays a parameter for agents (the owner's decision).
- **Left (Flux CD):** a capture of Lens's Flux views, dashboard and menus to check the inferred
  columns and actions against; the events of the Flux objects on the dashboard; the Helm Charts that Helm releases
  make (`helmcharts.source.toolkit.fluxcd.io`, only under Custom Resources, generic).
- **Evidence:** [resource-parity](resource-parity.md#paid-lens-desktop-views), Flux CD and Argo CD
  rows.
- **Argo CD (night build, 2026-10-13):** built, partial: the navigator group (after Flux CD,
  inferred), the five lists with the columns of Lens's documentation (read 2026-10-13): curated
  profiles of Applications, Application Sets and Projects, and Repositories and Clusters as
  virtual kinds over Argo CD's labelled Secrets that show an allowlist of non-credential keys
  ([D-35](../developer/decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show)); the
  Dashboard (`argocd/dashboard`: the documented strip, cards, Needs attention, recent activity;
  `argocd.dashboard` for agents); Sync with Prune and Refresh Normal or Hard on one Application or a
  selection, through the Kubernetes API ([D-36](../developer/decisions.md#d-36-how-argo-cds-sync-writes-the-operation)).
  Evidence: [resource-parity](resource-parity.md#paid-lens-desktop-views), the Argo CD row; on kind
  `TestKindArgoCD`; "Open Argo UI" as `argocd.openUI`, a port forward to `argocd-server`. Left for
  Argo CD: a check against Lens's views (not captured), a single dialog for a sync with prune
  (Cogpit asks twice); about 3–8 h of the estimate below (not moved). `TestKindArgoCD` runs against
  hand-written stand-ins of Argo CD's definitions; the check against a running Argo CD is done
  (below).
- **Checked against a live Argo CD controller (2026-10-16, night run):** Argo CD 3.5.5 from its
  public Helm chart on a real cluster the owner provides for tests, Applications whose source is a
  public Helm repository (no Git) in every state, an Application Set, a project in a deny window,
  an Application in another namespace, settings Secrets with fake credentials; read with
  `cogpit-cli get` and its MCP bridge, the actions through the engine (`TestRealArgoCD`, opt-in,
  never in CI). What held: the columns, the status tokens, the Dashboard's counts against
  `kubectl`, the related objects, the settings without credentials (D-35), the merged logs (A81),
  Refresh, Sync and Sync with prune on one and a selection run by the controller, Open Argo CD UI.
  Found and fixed (the lead's decisions, open to revision, in
  [D-36](../developer/decisions.md#d-36-how-argo-cds-sync-writes-the-operation) and the
  [Argo CD row](resource-parity.md#paid-lens-desktop-views)): the Resources' empty Health column
  (Argo CD 3 keeps it in its resource tree), the project and sync windows of an Application in
  another namespace, the sync windows' check against Argo CD 3.5.5's, the operation's confirmed
  source, an agent's Dashboard read right after connecting. Estimate not moved.
- **A namespace-limited user (2026-10-14):** both dashboards follow the user's plans and refusals
  (G3), and a user who may not list the custom resource definitions, so has no Flux or Argo CD
  kinds, reads that Cogpit cannot tell whether the cluster serves them, with the server's reason,
  where both said the cluster serves none. Such a user's Flux and Argo CD views stayed out of the
  navigator, their kinds coming from the definitions only; since A90 (built 2026-10-14, above) API
  discovery recognises them, and the dashboards count what the user may read. Estimate not moved.
- **Impact:** GitOps is daily work for the owner's team; in Lens it needs a paid plan.
- **Estimate:** 8–22 h, re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration),
  the figure the Summary table carries since (it said 55–115 until then): Flux CD 5–14 h (the
  checks after a capture of its views, dashboard and menus 2–6 h, the objects' events on the
  dashboard 1–4 h, a curated profile of the Helm Charts 2–4 h); Argo CD 3–8 h (its bullet above).
  Built into the code like every integration ([D-5](../developer/decisions.md#d-5-extensions)).
  Was 55–115 h before the sections (2–4 h per kind profile, the dashboards, two actions); after
  the Flux section (2026-10-13) Flux CD's remainder was about 6–15 h, and Reconcile with Source,
  Reset and the views' status filter came off it since.

## UI gaps

### G5. App shell, navigation, tables and drawer
- **Built 2026-10-17 (the owner's F21, [G16](#g16-stability-and-the-user-bug-backlog)):** the
  namespace filter's list (on Definitions the API group filter's) has a search field on top that has
  the keyboard as soon as it opens: typing narrows the namespaces to those containing the text,
  ignoring case; ↑↓ move a highlight, ↵ applies it, Space toggles its box, Escape clears the text,
  then closes. A click on a name filters to that namespace alone and closes the list; a click on a
  box adds or removes it and keeps the list open. The text is the window's own; the lead's decisions
  (open to revision) are in [ui-parity › Tables](ui-parity.md#tables), the namespace filter's row
  ([`nsfilter.go`](../../internal/ui/fyneui/nsfilter.go)). Not tried in a real window. Estimate not
  moved.
- **Built 2026-10-15 (the owner's F3, [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)):**
  a name wider than the drawer's name column wraps at its spaces, then after - . _ /, then by characters;
  a Config Map's and a Secret's Data put each key on a line of its own above its value. Estimate not moved.
- **Built 2026-10-15 (the owner's decisions of 2026-10-14 evening):** the navigator's search text
  and keyboard row are engine state, visible and settable for agents (the palette's typed text
  too, [G13](#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences);
  [D-51](../developer/decisions.md#d-51-the-navigators-and-the-command-palettes-state), with the
  lead's night decisions, open to revision); the navigator's focus ring on keyboard
  navigation only; the welcome page's "This session" became "Recent", and its links name the README
  for what Cogpit does and [the command list](../developer/commands.md) for the commands.
  Estimate not moved.
- **Decided (owner, 2026-10-14 evening; all of it built 2026-10-14 and 2026-10-15):** a plain click on a row opens
  its drawer and keeps the checked rows (A73, built 2026-10-15: `ui.select`'s mode `open`, the
  drawer's row highlighted with its checkbox as it was, [architecture §4.3](../developer/architecture.md#43-ui-state)); Restart on a
  selection of workloads ([G7](#g7-resource-actions), built 2026-10-14, A74); D-25's places revised: the lists' copy icon
  lies over the cell's end, the text no longer shortened for it (A69, built 2026-10-15), and the
  drawer's Forward… sits right after the value with the copy icon over the value's end on hover
  (A71, built 2026-10-15; [D-25](../developer/decisions.md#d-25-copying-values) lists the lead's
  readings, open to revision) (the status filter's button in
  the accent colour while a filter is active: built 2026-10-14, A80); after a create the new object's drawer opens and the
  list scrolls to its row (built 2026-10-14, A102; [G9](#g9-yaml-editor-and-create-resource)). Confirmed, now the owner's
  decisions: the checkmark and toast on a list copy; the copy icons' exclusion list, for custom
  resource columns too; Delete of at most 500 objects; the name column's 140-point minimum; links
  and chips not drag-selectable (the icon copies them); the status filter's rules (D-34: the state
  columns with the tokens as fallback, every named column matching, the filter per view and tab).
  Rows: [ui parity › Layout](ui-parity.md#layout), [› Clusters](ui-parity.md#clusters),
  [› Tables](ui-parity.md#tables), [beyond Lens › Everyday use](beyond-lens.md#everyday-use).
  Estimate not moved then (re-estimated 2026-10-15, below).
- **Required (owner, 2026-10-13, [D-25](../developer/decisions.md#d-25-copying-values)):** names in
  the lists (Pods, Deployments and every other view) are copyable with the drawer's copy icon on
  hover, at least in the Name column ([beyond Lens](beyond-lens.md)). **Built 2026-10-13**
  ([`tablecopy.go`](../../internal/ui/fyneui/tablecopy.go)): one icon per list, the drawer's, on
  the text cell under the pointer; a click copies the full text with the drawer's checkmark and
  toast and never selects the row. **Changed (owner, 2026-10-13, after seeing it):** no icon on
  Age, CPU, Memory, Restarts and the like, by a blacklist so that custom resource columns keep it,
  and the icon right after the text; built: the blacklist by column type and id in one place
  (`copyColumn`, its ids listed in D-25 as Cogpit's reading of the owner's principle), the icon a
  padding after the text, at the cell's end when the text leaves no room (the text shortened
  before it until A69 on 2026-10-15, since then the icon lies over the cell's end). Cogpit's defaults (not the owner's decision): the id list (confirmed by the owner
  2026-10-14 evening), none on cells without text; a Secret's list holds no values (`TestListCopyIconByColumn`, `TestCopyColumns`,
  `TestListCopyIconAfterTheText`, `TestListCopyIconOnHover`, `TestListCopyIconCopiesTheFullText`,
  `TestListCopyIconFollowsTheCells`, `TestSecretsListCopiesNoValue`). Not tried in a real window.
  Estimate not moved (a beyond-Lens row).
- **Decided (owner, 2026-10-12):** the Solarized colours ([D-22](../developer/decisions.md#d-22-colour-theme)), the
  status bar link ([D-23](../developer/decisions.md#d-23-a-link-to-the-owner-in-the-status-bar)), the C helm logo
  ([D-24](../developer/decisions.md#d-24-logo)) and copying values from the drawer
  ([D-25](../developer/decisions.md#d-25-copying-values)); built 2026-10-12 (fifth run).
- **Required (owner, 2026-10-12, seen in the demo cluster):** the detail drawer's width never
  follows its content: a long value (a container's `Waiting: CrashLoopBackOff (back-off …)` status,
  which the demo changes every few seconds) widened the drawer and made the whole layout jump. The
  user sets the width by dragging the drawer's edge, and it stays; long values wrap or are cut.
  The user can also set the width of the primary sidebar (the navigator) by dragging its edge.
  Both widths persist. **Built 2026-10-12** ([`panelwidths.go`](../../internal/ui/fyneui/panelwidths.go),
  [architecture §4.3](../developer/architecture.md#43-ui-state)): the drawer and the sidebar are a fixed
  number of points wide, set by dragging their edge (drawer 320 to 1000, default 600; sidebar 160 to 480,
  default 216; the table keeps 480 beside the drawer), stored in Fyne's preferences when the drag ends
  and restored at the next start; GUI-only, not engine state. The cause was the `container.Split`
  clamping its offset by the drawer's content-driven minimum width. Long values now wrap (statuses,
  properties, chips) or are cut (title bar, event header, links, lines), the full text stays in the copy
  icon and the selection (`TestDrawerWidthNeverFollowsItsContent`, `TestDrawerWidthByDraggingItsEdge`,
  `TestSidebarWidthByDraggingItsEdge`, `TestPanelWidthsSurviveARestart`, `TestDrawerRebuildsToItsWidth`;
  render `TestRenderDrawerLongStatus`). Not verified: dragging with a real pointer in a window (the tests
  drive the handle's `Dragged` events), Fyne's preference file on disk across a real restart.
- **Navigator (built 2026-10-13, night run):** the captured header (a scope dropdown and a search
  button), the cluster as the tree's root (its initials badge, name, kubeconfig, a connection dot, ›
  and ‹), "<item> - <context>" on every item, and the keyboard of Lens's documentation (↑↓, → and ←,
  Tab and ⇧Tab, ↵ through `ui.openView`), the tree now Cogpit's own rows instead of Fyne's
  ([`navigator.go`](../../internal/ui/fyneui/navigator.go), `TestNavigatorKeyboard`,
  `TestNavigatorSearch`, [ui-parity › Layout](ui-parity.md#layout), [› Clusters](ui-parity.md#clusters)).
  Differs from Lens: the › on the root does nothing (the tree is drilled into its cluster
  already); inferred, Cogpit's defaults:
  the search's matching rules, ↵ on a group, Space, Home and End, a letter typed in the tree starting
  a search. Not tried in a real window. Estimate not moved then (re-estimated 2026-10-15, below). Since the tab bar
  (below) ↵ and a click open the preview tab, ⇧↵ and a double click a fixed tab; since the section
  level (below) the dropdown and the ‹ reached it; since 2026-10-15 ([D-57](../developer/decisions.md#d-57-choosing-a-cluster)) the header's
  "‹ All Clusters" does, the dropdown, the root's ‹ and the context picker gone.
- **Navigator section level and scope (built 2026-10-13, night run):** the level above a cluster,
  Kubernetes Clusters › Local Kubeconfigs: every context the kubeconfig sources offer, grouped by
  the folder or file they come from, each with its identity's badge, display name, connection dot
  and ›, the source as the tooltip; the › (a click, → or ↵) drills into the cluster, connecting it
  through the engine's connect after the login program's approval, the header's ‹ All Clusters (or ←
  on the root) goes back up; since D-57 the one list that chooses a cluster, ⌘P showing it with its
  search focused.
  The scope is engine state (`UIState.NavigatorScope`, `ui.setNavigatorScope` and its MCP tool),
  kept in step with the connection (every connect drills in, no connection is the section), and the
  tab bar shows the scope's tabs, ⌘n, ⌘⌥→ and ⌘⌥← counting those
  ([`fyneui/navigator_sections.go`](../../internal/ui/fyneui/navigator_sections.go),
  [`engine/navscope.go`](../../internal/engine/navscope.go), `TestNavigatorSectionDrillsInAndBack`,
  `TestNavigatorSectionOverTheEngine`, `TestTabsShown`,
  [PNG](../../internal/ui/fyneui/testdata/navigator_section_dark.png),
  [ui-parity › Clusters](ui-parity.md#clusters)). Not built: the cloud sections (paid, not a target
  since 2026-10-22, D-9), Teamwork (D-9) and Extensions (D-5), so nothing is above the section. Inferred, Cogpit's
  defaults: the grouping, the dot's colours, every connect
  drilling in (to the cluster it shows), the search filtering the clusters at the section, the keyboard
  following the level. Not tried in a real window. Estimate not moved then (re-estimated 2026-10-15, below).
- **Tab bar (built 2026-10-13, night run):** the tabs of cluster views above the workspace as
  captured: "<view> - <context>" with the cluster's badge, the preview tab italic, a close button,
  the full title as tooltip; the navigator's click opens the preview tab, its double click and ⇧↵
  a fixed tab, a double click on the preview tab fixes it; ⌘W, ⌘⌥→ and ⌘⌥←, ⌘1–⌘9, ⌃- and ⌃⇧- as
  View menu items from any focus. The tabs are engine state (`UIState.Tabs`, eight `ui.tab.*`
  commands and MCP tools; `ui.openView` opens in the preview tab), each with its own namespace
  filter, search, sort, selection, drawer and history, kept in step by one function that
  `UpdateUI` runs ([`engine/tabs.go`](../../internal/engine/tabs.go),
  [`fyneui/tabbar.go`](../../internal/ui/fyneui/tabbar.go), `TestTabsOfAnotherCluster`,
  `TestTabBarOverTheEngine`, [PNG](../../internal/ui/fyneui/testdata/tabbar_dark.png),
  [ui-parity › Layout](ui-parity.md#layout)). Differs from Lens: initials badges instead of the
  cluster's icon (cluster identity missing); until 2026-10-16 one connection at a time, so showing a
  tab of another cluster connected to it: since then several clusters stay connected as Lens keeps
  them ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) stage 1, the
  engine: a pool of backends, a tab of a connected cluster shown from its cache, every command on
  its own cluster, `UIState.Connections`), and since stage 2 the same day the window's part: the
  marks of every connected cluster on its dots, switching without reconnecting, a tab muted only
  without a connection, the dock per cluster as engine state ([`dockscope.go`](../../internal/engine/dockscope.go),
  `TestDockShowsTheShownClustersTabs`, `TestDotsShowEachClustersMark`); stage 3 built 2026-10-16: a cluster's
  setting holds its credential plugin while it is not shown (its login lapsing, `loginLapsed`), and
  Preferences › App › Restore the last session (off by default) brings back the tabs and the
  clusters of the last quit (both Cogpit's own rows, [beyond Lens](beyond-lens.md)); Windows and Linux get the Ctrl forms of the macOS keys, not Lens's
  Ctrl+PageUp/PageDown and Alt+←/→ (G22). Inferred, Cogpit's defaults: the
  neighbour shown on close, the history's contents and bound, ⌘9 the last tab, wrapping next and
  previous; one preview tab per cluster since 2026-10-23, the owner's pick ([D-117](../developer/decisions.md#d-117-one-preview-tab-per-cluster); one in all before, the lead's inference). The tab's badge takes the cluster's identity (2026-10-13); since 2026-10-13 (night run)
  the bar shows the navigator's scope's tabs (the section's: every tab; a cluster's: its own). Not
  tried in a real window.
  Estimate not moved then (re-estimated 2026-10-15, below). The title bar's history arrows came the same night (below).
  **Since 2026-10-13 (night build) the tabs and the tables' layout survive a restart**
  ([D-41](../developer/decisions.md#d-41-what-survives-a-restart), night decisions, decided by the
  owner 2026-10-14; [`engine/persist.go`](../../internal/engine/persist.go)): Cogpit's own `workspace.json`
  keeps each tab's cluster, view, preview or fixed, place, when last shown, namespace and group
  filters, search and its toggles, sort and history, which tab was shown last, and the hidden
  columns and column order per view key; at start the tabs show at once and nothing connects (the
  tab shown last marked, its cluster disconnected, the welcome page; `TestWorkspaceNoRequestAtStart`
  counts the requests), a tab connects its cluster when shown; the tabs of a context certainly gone
  are dropped with a notice; a file that does not load is kept aside. Since 2026-10-16 (the owner's
  decision, D-69 item 8) the tabs come back only while Preferences › App › Restore the last session
  is on, the clusters connected at quit then connecting again; off (the default), the start shows no
  tab. Not kept: a selection, the
  drawer, the dock's tabs (an editor's text has its draft, D-11) and the column widths (not kept,
  the owner's decision, D-41, 2026-10-14). Estimate not moved then (re-estimated 2026-10-15, below). Fixed 2026-10-14 (the night build's
  seams): a removed source's or a gone file's context closed its cluster tabs but left its dock tabs
  (logs, terminals, a node shell, editors) open; they close with it, counted in the same notice, a
  node shell's pod deleted (D-37), an editor's unsaved text kept as a draft (Cogpit's default,
  [architecture §6](../developer/architecture.md#6-cluster-connections-internalcluster),
  `TestDockTabsCloseWhenTheirFileGoes`, `TestDockTabsCloseWhenTheirSourceIsRemoved`); and a connect
  under way when its context left Cogpit kept `UIState.Context` after it failed, or its connection
  after it succeeded; its end now forgets the context (`TestConnectEndsWhenItsContextLeftCogpit`).
- **Title bar (built 2026-10-13, night run):** the captured back and forward arrows, after Cogpit's
  logo (Lens has them after the window buttons, where the owner wants Cogpit's logo), enabled from
  the active tab's history and dispatching `ui.tab.back` and `ui.tab.forward`, tooltips with ⌃- and
  ⌃⇧-; the captured notifications bell at the bar's right end with an unread badge and a panel of
  the session's toasts newest first ("Clear all", read on opening, a toast's × reads it); a
  right-click on the bar opens a context menu with Preferences…. The notifications are engine
  state: a bounded log of every toast, the engine's and the window's own, but a copy's
  confirmation (shown only, not recorded: the owner's F5, 2026-10-15)
  ([`engine/notifications.go`](../../internal/engine/notifications.go), `notifications.list`,
  `notifications.markRead`, `notifications.clear`, `UIState.Notifications`), which agents read with
  the window's own texts and a Helm action's error filtered since 2026-10-14 (A19: the cached
  Secrets' values replaced, the action's inline values removed; withheld before; H-31,
  `TestHelmErrorToastReachesAgentsFiltered`); marking read and clearing the window's only (A18). Clicks on the new widgets never reach
  the drag area (`TestBarClicksReachTheButtons`). Inferred, Cogpit's defaults (not the owner's
  decision): the panel and its look, what counts as read, the badge's colour, 100 kept per session,
  Preferences… as the menu's only item, the arrows' place
  ([`titlehistory.go`](../../internal/ui/fyneui/titlehistory.go),
  [`notifications.go`](../../internal/ui/fyneui/notifications.go),
  [ui-parity › Layout](ui-parity.md#layout), [› Appearance](ui-parity.md#appearance-and-application)).
  The "Update" button built 2026-10-15 (the owner's finding F7, [D-54](../developer/decisions.md#d-54-the-layout-toggles-and-the-update-button)):
  shown at the bar's right end while Sparkle holds an update, left of Cogpit's own layout toggles
  (the navigator ⌘B, the dock ⌘J, the chat ⌘I); a click opens Sparkle's window. Not tried in a real
  window. Estimate not moved then (re-estimated 2026-10-15, below).
- **Welcome page (built 2026-10-13, night run):** the workspace shows Cogpit's welcome page while
  it has nothing else to show (no cluster connected, or no tab active) and from Help › Welcome:
  the logo, a line about Cogpit, the clusters to connect with one click (the hotbar's bank, since D-83 the hotbar's, this
  session's, then up to eight others), Connect to Cluster… (All Clusters… until D-57; gone from the page since 2026-10-16 night, N122), Add Kubeconfigs… and the README on
  GitHub. Engine state: `UIState.Welcome`, derived after every change, and `ui.welcome.open`, which
  leaves the active tab keeping its state ([`fyneui/welcome.go`](../../internal/ui/fyneui/welcome.go),
  [`engine/welcome.go`](../../internal/engine/welcome.go), `TestWelcomePageOverTheEngine`,
  [PNG](../../internal/ui/fyneui/testdata/welcome_dark.png), [ui-parity › Layout](ui-parity.md#layout)).
  Lens's page is not captured: its contents, wording and layout are Cogpit's (inferred, Cogpit's
  defaults). Since 2026-10-13 (night build, [D-42](../developer/decisions.md#d-42-the-start-screen),
  decided by the owner 2026-10-14) the context picker no longer opens by itself at
  start: the window starts on this page, under the tabs of the last run (D-41; since 2026-10-16
  only with Restore the last session on, D-69 item 8), and the notices
  the engine gave before the window was up show as toasts (`TestStartShowsRestoredTabsWithoutConnecting`,
  PNG [dark](../../internal/ui/fyneui/testdata/start_restored_dark.png)). Not tried in a real
  window. Estimate not moved then (re-estimated 2026-10-15, below).
- **Row checkboxes and actions on a selection (built 2026-10-13, night run):** the checkbox column
  and the header's checkbox as captured; the selection is engine state set through `ui.select` with
  modes (replace, add, toggle, remove, all shown rows), each tab its own; ⌘- and ⇧-click; from two
  selected objects (one since 2026-10-15, below) the header's "N selected", Delete and a clear button, and the row menu of a
  selected row acts on the selection (Delete, Clear Selection); Delete of a selection is one
  confirmation listing every object (G9). Inferred, Cogpit's defaults: what a click, ⌘ and ⇧ do,
  the header's checkbox on the rows shown, the bar from two objects (from one since 2026-10-15, the owner's decision), Delete the only action on a
  selection, the name column giving up width before the table scrolls sideways
  ([`tablecheck.go`](../../internal/ui/fyneui/tablecheck.go), `TestRowCheckboxes`,
  `TestHeaderCheckbox`, `TestSelectionMenu`, [PNG](../../internal/ui/fyneui/testdata/selection_menu_dark.png),
  [ui-parity › Tables](ui-parity.md#tables)). Not tried in a real window. Estimate not moved then
  (re-estimated 2026-10-15, below).
- **Column reorder (built 2026-10-13, night run):** a header dragged into another gap moves its
  column; the order is engine state per view (`ui.setColumnOrder`, `UIState.ColumnOrder`), kept for
  the session, followed by the table, the column list and the CSV export, reset from the header's
  ⋮. Inferred, Cogpit's look: a chip under the pointer and an accent line in the gap
  ([`columnorder.go`](../../internal/ui/fyneui/columnorder.go), `TestDragHeaderReordersColumns`,
  `TestSetColumnOrder`, [PNG](../../internal/ui/fyneui/testdata/column_drag_dark.png)). Not tried
  with a real pointer. Estimate not moved then (re-estimated 2026-10-15, below).
- **Search toggles (built 2026-10-13, night run):** the search field's "Aa" (match case) and ".*"
  (regular expression) as captured, engine state per tab beside the text (`UIState.FilterOptions`,
  `ui.setFilter` with `matchCase` and `regex`), applied by one row matcher to every view and page
  table; a regular expression is RE2 (linear time), at most 1,000 bytes, and one that does not
  compile filters nothing out while the field shows the error. Inferred, Cogpit's defaults: the
  rules, the error state, the toggles staying when the view changes
  ([`engine/filteroptions.go`](../../internal/engine/filteroptions.go), [`searchtoggles.go`](../../internal/ui/fyneui/searchtoggles.go),
  `TestSetFilterOptions`, `TestSearchToggles`, [PNG](../../internal/ui/fyneui/testdata/search_toggles_dark.png),
  [ui-parity › Tables](ui-parity.md#tables)). Not tried in a real window. Estimate not moved then
  (re-estimated 2026-10-15, below).
- **Missing (2026-10-15):** no row: the 40 rows of Layout, Cluster sidebar, Tables, Detail drawer
  and Dock exist, 5 done and 35 partial (one of them paid). Left: the owner's audit answers of
  2026-10-15 that touch this gap are built (below); the partial rows
  checked against Lens's captures and in a real window, Cogpit's own title bar on macOS included.
  Built since this list first named them (Evidence): the Fyne window layout, the cluster sidebar,
  the table, the detail drawer, the dock frame and Cogpit's theme; the [spike](../developer/spike-fyne.md)
  passed (D-1).
- **Evidence:** [ui-parity](ui-parity.md), 40 rows across Layout, Cluster sidebar, Tables, Detail
  drawer and Dock, all existing on 2026-10-15, 5 done (18 partial on 2026-10-11)
  ([`internal/ui/fyneui`](../../internal/ui/fyneui/), Cogpit's theme with status tokens). The
  spike's gate results are recorded, and D-1 is decided for Fyne
  ([spike](../developer/spike-fyne.md#results)). Since 2026-10-11 the captured differences in the
  existing views are closed or narrowed: row menus (⋮ and right-click) built from the registry
  with container submenus and schema-generated dialogs, the drawer's toolbar as icons with
  tooltips, the warning column (Pods, Nodes; rules inferred), links in cells, tooltips on cut-off
  cells, the drawer's Properties section with folded lists, links and the events at the end, a
  cordoned node's Conditions; cell tooltips and right-click are done. Then still missing, all built
  since: keyboard navigation, CSV export, hiding columns and Cogpit's own title bar on macOS (the
  third run, 2026-10-11, below; owner, 2026-10-11: no standard title bar, the logo beside the
  window buttons; [ui-parity](ui-parity.md#layout)), the drawer's Copy (D-25, 2026-10-12), the
  port's Forward… (2026-10-11, [G10](#g10-port-forwarding)), the Pods view's container squares
  (`TestContainersCellDrawsSquares`), the title bar's history, the welcome page, column reorder,
  row checkboxes and multi-selection in row menus (2026-10-13, above).
- **Evidence (2026-10-11, third run, tables and window):** cluster-scoped views hide the namespace
  filter; show and hide columns from the header's ⋮ (`ui.setColumns`, per view in the UI state);
  the Definitions view's group filter (`ui.setGroups`); keyboard navigation (done); a double click
  on a column separator fits the column; "18 items" with a CSV download (`table.exportCsv`, written
  in the engine); the Workloads Overview as a page (`workloads/overview`, `workloads.overview`);
  Cogpit's own title bar on macOS, built but not yet seen in a real window (smoke launch). Then
  still missing from this list, all built since: history in the title bar (2026-10-13), the
  drawer's Copy and Forward…, container squares, the welcome page, row checkboxes, multi-selection
  and column reorder (2026-10-13).
- **Built 2026-10-15 night (the owner's audit answers (7) and (8)):** the selection bar from one
  checked row (was two; the row menu of a lone checked row still acts on that one object, the
  lead's default, which the owner changed in the audit of 2026-10-16: at one checked row the object's menu gains Clear Selection after a separator, decided the same evening (N112), built 2026-10-16 night, `TestSelectionMenu`, [PNG](../../internal/ui/fyneui/testdata/selection_one_menu_dark.png)); `ui.navigate` opens the view and the drawer and leaves the checked rows alone;
  the checked rows change only through `ui.select`
  ([`tablecheck.go`](../../internal/ui/fyneui/tablecheck.go), [`commands/ui.go`](../../internal/engine/commands/ui.go),
  `TestRowCheckboxes`, `TestNavigateLeavesTheSelection`). Not tried in a real window.
- **Built 2026-10-16 (N111, the owner's decision in the audit of 2026-10-16; it replaces the
  night's default that a view changed to started without the old view's checked rows):** each
  view keeps its checked rows. The tab remembers a view's selection, per cluster, when it leaves
  the view and gives it back when it shows the view again (`ui.openView`, `ui.navigate`, a link,
  back and forward); the selection bar and its Delete act on the shown view's; a selection shown
  again drops the objects deleted meanwhile. The lead's decisions on it: per tab and per cluster,
  `ui.openView` of the shown view keeps its checked rows, `ui_state` lists the shown view's only,
  a remembered selection ends with its tab, its cluster's disconnect and the session
  ([`selectionmemory.go`](../../internal/engine/selectionmemory.go), `TestSelectionRememberedPerView`,
  `TestRememberedSelectionDropsGoneObjects`, `TestSelectionBarIsTheShownViews`). Estimate not moved.
- **Built 2026-10-16 (the owner's audit decision of 2026-10-16):** the selection bar's Delete, greyed
  while the connection is not connected (built 2026-10-16 night), names the engine's reason in its
  tooltip as the row menus do, "Delete — connection interrupted" (the login expired, a cluster not
  connected and a lapsed login alike), and says "Delete the selected objects…" again with the
  connection back ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections);
  [`tablecheck.go`](../../internal/ui/fyneui/tablecheck.go) `selectionBar.followConnection`,
  `TestSelectionBarDeleteNamesTheReason`). Estimate not moved.
- **Estimate:** 9–35 h, re-estimated 2026-10-15 night (was 11–41: the selection bar and
  `ui.navigate` built): the 35 partial rows at 0.25–1 h, 9–35 h. The tab bar, the
  navigator's section level, the title bar's arrows and notifications, the welcome page, row
  checkboxes, column reorder, the search toggles, the copy icons and the panel widths had not moved
  the estimate. Was 35–110 (was 45–130; title bar, keyboard, columns, CSV and the Workloads
  Overview built, third run).

### G13. Clusters, hotbar, palette, menus, shortcuts and preferences
- **Found in the owner's real-window walk (2026-10-14 evening):** the About dialog shows version
  0.0.1 and is laid out confusingly. **Fixed 2026-10-14:** the dialog read Fyne's app metadata, whose
  version is Fyne's default 0.0.1 in every Cogpit build, a release's too (`fyne package
  --executable` injects none; read in Fyne 2.8.1's `app/meta.go`), so the bundle's version and the
  binary's never reached it. It now shows the version the binary reports
  ([`internal/buildinfo`](../../internal/buildinfo/buildinfo.go), one variable the release build sets
  for both binaries; a development build says "Development build" and its commit), laid out as
  logo, name, version, one line, the licence and two links
  ([`fyneui/about.go`](../../internal/ui/fyneui/about.go), [PNG](../../internal/ui/fyneui/testdata/about_dark.png);
  [ui parity › Application menu](ui-parity.md#application-menu)). Not tried in a real window.
- **Built 2026-10-14 (decision A60):** one About entry, Cogpit › About Cogpit on macOS: GLFW's own
  item in the application menu shows Cogpit's dialog and survives Fyne's rebuilds of the menu bar
  ([`fyneui/appmenu_darwin.m`](../../internal/ui/fyneui/appmenu_darwin.m), `TestAppMenuAbout`); Help's
  About Cogpit gone there, kept at the end of Help on Windows and Linux (Cogpit's default: no
  application menu there). Not tried in a real window.
- **Decided (owner, 2026-10-14 evening; built 2026-10-14 and 2026-10-15 but the other window, R3,
  postponed):** the hotbar's tiles reorder by
  drag and drop (a paid row of its own, built 2026-10-15, R2, below); dragging a tile out of the window and dropping it
  opens another Cogpit window with that cluster (a beyond-Lens row, missing), designed 2026-10-15,
  postponed by the owner until the rest is done ([D-52](../developer/decisions.md#d-52-several-windows)): one UI state per window, the window list, which window agents drive, the
  workspace, Fyne's start-up stall per window (gate S1's reservation, [G15](#g15-performance-and-api-load-at-scale));
  built 2026-10-15: F1–F12 work as the hotbar's in
  plain search fields, the terminal and the editor keeping their F-keys; the navigator's scope
  dropdown lists the hotbar's clusters and the connected one only, every other context through ⌘P
  (so no cap; the shown bank's, a night decision, D-51; the dropdown went the same night with D-57,
  the banks on 2026-10-17 with D-83); the Local Kubeconfigs section's source
  groups collapse, kept across restarts; Disconnect closes the cluster's tabs; Remove from Cogpit on a synced folder's context offers to
  unlink the whole folder after a confirmation naming every context it takes (A97, built 2026-10-15,
  [`engine/contextlink.go`](../../internal/engine/contextlink.go), `TestRemoveFromCogpitUnlinksTheFolder`); one About entry, in
  the application menu (Cogpit › About Cogpit), Cogpit's own dialog, Help's going (built
  2026-10-14, above); built 2026-10-15: ⌘F and ⌘⇧T as
  main-menu items showing their keys, which stay window shortcuts (the editor keeps its ⌘F; View ›
  Search and New Terminal); the palette's typed text as engine state, an agent's text never running
  a change on ↵ (a night decision, D-51).
  Built 2026-10-15: a cluster's colour from the eight swatches or a free colour picker (A93), drawn
  in a legible shade of its hue on each theme ([D-22](../developer/decisions.md#d-22-colour-theme)).
  Built 2026-10-14: Sync with computer the default theme (A94); a `cogpit://` link naming another cluster than the connected one asks once,
  naming the context ([D-47](../developer/decisions.md#d-47-deep-links-cogpit)); agents may set a proxy, Preferences' and a cluster's, as a write the user always
  confirms; an agent removing a bank that holds clusters waits for the user (moot since D-83, no banks); marking notifications
  read and clearing them the window's only, the window-only toasts reaching agents as text through
  the Secret filter
  ([H-32](../security/agents-and-mcp.md#h-32-an-agent-can-clear-the-users-notifications), H-31). Confirmed, now
  the owner's decisions: the session's leftovers below (a removed cluster's context forgotten, a
  file leaving a synced folder closing its tabs); every connect drilling into its cluster; closing
  a tab showing the nearest of its cluster; the "+" below the tiles; "New Bank…" (moot since D-83); no onboarding
  wizard; start at login off by default (D-48); the light accent #1F7F78; proxy passwords in the
  0600 files and no warning for an http proxy with a password
  ([H-33](../security/credentials.md#h-33-proxy-passwords-are-plaintext-in-cogpits-settings));
  only the window choosing the Helm configmap driver; the cluster's terminal namespace. Verified by
  the owner in a real window: GLFW's Window menu. Rows: [ui parity › Clusters](ui-parity.md#clusters),
  [› Hotbar](ui-parity.md#hotbar), [› Application menu](ui-parity.md#application-menu),
  [› Preferences](ui-parity.md#preferences), [› Appearance](ui-parity.md#appearance-and-application),
  [beyond Lens › Everyday use](beyond-lens.md#everyday-use). Estimate not moved then (re-estimated 2026-10-15, below).
- **Required next (owner, 2026-10-12: "still missing, do not forget"):** the hotbar (Lens's
  vertical bar at the window's left edge with the pinned clusters' icons). **Built 2026-10-12**
  (night run, [`engine/hotbar.go`](../../internal/engine/hotbar.go),
  [`fyneui/hotbar.go`](../../internal/ui/fyneui/hotbar.go)): banks of up to 12 pinned clusters as engine
  state (`UIState.Hotbar`, nine `hotbar.*` commands and MCP tools that write only
  `<user config dir>/Cogpit/hotbar.json`), the bar left of the navigator with initials tiles, a
  connection dot, the context as tooltip and the "‹ 1 ›" switcher, a click or F1–F12 to connect,
  ⇧F1–⇧F12 to switch banks, Add to Hotbar in the row menus (the navigator section's since D-57) and the Cluster menu
  (`TestHotbarCommands`, `TestHotbarFunctionKeys`, [PNG](../../internal/ui/fyneui/testdata/hotbar_tiles_dark.png), since D-83 without the switcher).
  Differs from Lens: Move Up and
  Down instead of dragging (dragging built 2026-10-15, R2, below), the F-keys do nothing while a text field or a
  terminal has the focus, not tried in a real window; inferred: missing contexts kept as marked
  tiles, at most 12 banks, the arrows wrap, the confirmation before removing a bank with clusters
  ([ui-parity › Hotbar](ui-parity.md#hotbar)).
- **Built 2026-10-15 (night run, R2):** hotbar tiles reordered by drag and drop: a tile dropped
  into the gap between two tiles moves there through `hotbar.moveCluster`, Move Up and Move Down
  kept ([`fyneui/hotbardrag.go`](../../internal/ui/fyneui/hotbardrag.go), `TestHotbarTileDragCancels`). The
  lead's decisions, open to revision: an accent line in the gap and an outline where the
  tile was (replaced on 2026-10-17, below); a drop on the "+" puts it last (no empty cells); a drop outside the bar does nothing
  (R3's seam, [D-52](../developer/decisions.md#d-52-several-windows)); a drag under eight points opens
  the cluster. Not tried with a real
  pointer. The paid row from missing to partial; estimate not moved then (re-estimated 2026-10-15, below).
- **Built 2026-10-17 evening (the owner's WF7 and WF6 of his walk that day, [D-83](../developer/decisions.md#d-83-one-hotbar-and-a-drag-that-carries-the-tile)):**
  one hotbar. The owner decided that Cogpit needs no banks ("Es gibt nur eine Hotbar bei uns"): the
  switcher, ⇧F1–⇧F12 and the four bank commands with their MCP tools are gone, the hotbar holds as
  many tiles as the user pins and scrolls, F1–F12 open the first twelve, a file of banks is merged
  into one list at the start (no tile lost, the old file kept as `hotbar.json.v1`); the banks' row
  and the palette's bank commands are not a target, a deliberate deviation (two paid rows fewer).
  The drag carries the tile: a lifted copy under the pointer, its empty slot travelling with it, the
  other tiles sliding into place while the button is held, the bar scrolling at its edges, Reduce
  motion honoured; R2's line and outline gone (`TestHotbarMergesBanks`, `TestHotbarPastTwelveScrolls`,
  `TestHotbarDragCarriesTheTile`, `TestHotbarDragSlides`, `TestHotbarDragScrollsAtTheEdge`,
  [PNG](../../internal/ui/fyneui/testdata/hotbar_drag_dark.png)). Not tried in a real window. No
  estimate moves.
- **Built 2026-10-13 (night run):** the command palette, ⌘⇧P
  ([`engine/palette.go`](../../internal/engine/palette.go), [`fyneui/palette.go`](../../internal/ui/fyneui/palette.go)):
  an overlay with a filter over what the menus offer in the current state, listed from the
  registry's metadata (the drawer object's actions, menu commands only, no tool meant for agents,
  Switch to *view* per navigator view, the hotbar's clusters (and its banks until D-83), commands with required
  parameters only where the window has a dialog for them), run as their menu items run them
  (confirmation, dialogs, audit); its open state is UI state (`ui.palette.open`, `ui.palette.close`).
  Shortcuts through the registry: ⌘⇧P and ⌘P (the navigator's section since D-57) on main-menu items, so they work from
  any focus; ⌘F and ⌘⇧T taken from the registry; ⌘Q left to the platform (GLFW's app menu), ⌘R
  not bound (nothing to reload in a watch-fed view). Inferred: the list's order and contents beyond
  the captured rows, the greyed rows, the ranking; not tried in a real window
  (`TestCommandPaletteTypeAndRun`, `TestPaletteListsWhatTheMenusOffer`,
  [PNG](../../internal/ui/fyneui/testdata/command_palette_dark.png)).
- **Built 2026-10-13 (night run):** Preferences (⌘,) and a cluster's Settings (⌘⇧,), as engine
  state and commands ([`engine/preferences.go`](../../internal/engine/preferences.go),
  [`engine/clustersettings.go`](../../internal/engine/clustersettings.go): `UIState.Preferences`,
  `UIState.ClusterSettings`, the open page in `UIState.Settings`; `preferences.*`,
  `cluster.settings.*`, `settings.close`, each an MCP tool) kept in Cogpit's own
  `preferences.json`, `cluster-settings.json` and `cluster-icons/`, never in a kubeconfig. The
  pages ([`fyneui/preferences_page.go`](../../internal/ui/fyneui/preferences_page.go)) take the
  views' place until closed (inferred). Preferences: the theme (switched at once;
  `--theme` overrides it for a run), the time zone (since D-31, 2026-10-13, every timestamp Cogpit shows, in the computer's region format), the
  tables' row height, the editor's font size, line numbers and tab size, the terminal's font
  size, shell and arguments (the shell GUI-only). Cluster settings: the display name and the
  identity (the theme's accent colours or an uploaded image, checked and kept as Cogpit's copy)
  that the hotbar, the navigator and the welcome page show; the kubeconfig's path with its folder in
  the file manager; the terminals' working directory and namespace; the Prometheus dialog linked.
  Settings… in the menus of the navigator's rows and root, the hotbar's tiles and the welcome page's rows; Preferences… moves
  into macOS's application menu (`TestPreferencesOpenWithTheShortcutAndSwitchTheTheme`,
  `TestClusterSettingsFromTheSectionSetIdentityAndTerminal`, `TestClusterSettingsCommandsNeverTouchTheKubeconfig`,
  [PNG](../../internal/ui/fyneui/testdata/cluster_settings_dark.png)). Inferred or Cogpit's
  defaults: the page in place of the views, the sections' grouping, the swatches, the bounds, the
  cluster namespace replacing the filter's, Dark as the default theme (Sync with computer since
  2026-10-14, A94); not tried in a real window.
- **Built 2026-10-13 (night run):** the Help and Window menus and a cluster's Disconnect and
  Remove from Cogpit ([`fyneui/menus.go`](../../internal/ui/fyneui/menus.go),
  [`fyneui/menus_cluster.go`](../../internal/ui/fyneui/menus_cluster.go)). Help: Welcome
  (`ui.welcome.open`), Documentation (the README on GitHub), Copy MCP command, About Cogpit (the
  build's version; since 2026-10-14 in the application menu on macOS, above); "Open Onboarding Wizard..." not a target (the lead's default, confirmed by the owner 2026-10-14
  evening). Window: on macOS
  GLFW's own menu stays (Minimize ⌘M, Zoom, Bring All to Front, Enter Full Screen ⌃⌘F, the windows;
  verified in GLFW 3.4's and Fyne 2.8.1's source, Cogpit adds none); on Windows and Linux Close
  Window and Toggle Full Screen, Minimize missing (Fyne has no call for it). Disconnect
  (`cluster.disconnect`) closes the connection without a request to the cluster; Remove from Cogpit
  (`cluster.remove`, GUI only) removes Cogpit's link to a kubeconfig file the user added after a
  confirmation naming the path and the contexts that leave, never the file; a context of a synced
  folder (the default `~/.kube` too) or of a `$KUBECONFIG` file is greyed with the reason
  (`cluster.links`). Both in the Cluster menu and the clusters' menus (picker, hotbar tile, navigator
  root, welcome page). Zoom stays missing: Fyne has no runtime scale, ⌘- is the tab history's Back on
  Windows and Linux, and the level would need a Preferences field. Inferred or Cogpit's defaults:
  the labels, which contexts can be removed, the Window menu on Windows and Linux; not tried in a
  real window (`TestRemoveFromCogpitAsksFirst`, `TestHelpAndWindowMenus`, `TestClusterRemove`,
  `TestDisconnect`; [ui-parity › Application menu](ui-parity.md#application-menu)). Estimate not
  moved then (re-estimated 2026-10-15, below). Fixed 2026-10-13: the window's Cluster › Disconnect stayed greyed while
  the same cluster reconnected (the window's menu was rebuilt only when the connection left
  Connected, and again once the cluster's kinds came); it is now rebuilt on every change of the
  connection (`TestClusterMenuDisconnectFollowsAReconnect`). Fixed 2026-10-13: the tabs of a cluster
  removed with `cluster.remove` or `kubeconfig.remove` stayed in the tab bar; the engine now closes
  the tabs of every context that leaves with the source (`Session.RemoveKubeconfigSource`), so agents'
  `ui_state` loses them too; a context still offered keeps its tabs. Cogpit's default, not the owner's
  decision (`TestRemovedClustersLeaveTheTabBar`, `TestRemovedClusterLeavesTheTabBar`).
  **The session's leftovers, decided 2026-10-13 (night build; the lead's, confirmed by the owner
  2026-10-14 evening):** removing a cluster (`cluster.remove`, `kubeconfig.remove`) also forgets
  `UIState.Context` when it named one of the clusters that leave and no connection holds it (options
  (a) keep naming it, as after a disconnect, (b) forget it; chosen (b), so that nothing points at a
  context that no longer exists: the navigator shows its section, a failed connect's error goes with
  its context; the connection made from the file is closed first, as before); and a context whose
  kubeconfig file leaves a synced folder (or an added file that is deleted) loses its tabs, with a
  notice naming the clusters, and is forgotten the same way (options (a) keep the tabs, failing when
  shown, (b) close them as a removed cluster's; chosen (b), only on the plain fact that the file
  does not exist, as the connection closes; a file only rewritten keeps them). Removing a cluster
  gives no notice: the user confirmed a dialog naming its contexts (Cogpit's default)
  ([`engine/persist_restore.go`](../../internal/engine/persist_restore.go);
  `TestRemovedSourceForgetsTheContext`, `TestTabsCloseWhenTheirFileLeavesASyncFolder`).
- **Built 2026-10-13 (night run): a cluster's proxy** (Settings › Proxy,
  [ui parity](ui-parity.md#cluster-settings), [`engine/proxy.go`](../../internal/engine/proxy.go)): an
  http, https or socks5 URL per context in `cluster-settings.json`, applied at the next connect through
  `rest.Config.Proxy` (`cluster.Options.Proxy`), so every request and stream of the connection goes
  through it; tested against a fake API server behind a test proxy (`TestConnectGoesThroughTheProxy`).
  Its password is a credential: shown as `user:•••@host` in the window, `ui_state`, the commands'
  results, the audit log and the connection log, never in an error
  ([credentials](../security/credentials.md)). **Cogpit's defaults, not the owner's decisions:** only the
  user set a proxy at first (agents and the CLI clear it and read it redacted; the lead proposed letting agents
  set it), and it replaces the kubeconfig's `proxy-url` for Cogpit only. **Decided (owner, 2026-10-14
  evening; built 2026-10-14):** agents may set it, as a write the user always confirms (the password redacted in the
  confirmation, the audit log and the results; A20, `TestClusterProxyCommands`), and the plugin approval binds this proxy too (built
  2026-10-14, A23: the dialog shows it, a changed proxy asks again,
  [H-34](../security/trust-boundaries.md#h-34-the-plugin-approval-shows-the-kubeconfigs-proxy-not-cogpits)).
  Left: the local terminal's kubectl does not use it.
- **Built 2026-10-13 (night run): the HTTP proxy for non-cluster traffic** (Preferences › Proxy,
  [architecture §8](../developer/architecture.md#8-helm-internalhelm)): the Helm repositories'
  downloads, and since the kubectl download of the same night (D-3) that one too, go through it, else
  through the environment's `HTTPS_PROXY` as before; same redaction and the same rule as the
  cluster's proxy, an agent's waiting for the user since 2026-10-14 (A20; `TestDownloadsGoThroughTheProxy`,
  `TestNonClusterProxyCarriesHelmDownloads`, `TestNonClusterProxyCommands`). **Held
  back (the lead's default, a question for the owner):** "Allow untrusted certificate authorities for
  non-cluster traffic", which would switch off the certificate check of those downloads.
- **Built 2026-10-13 (night run, D-3):** Preferences › Kubernetes holds kubectl for terminals:
  Download kubectl (on by default), the download mirror (`dl.k8s.io` or a custom https base, where
  Lens offers Google or Azure), the directory for binaries and the path to kubectl, the last three
  GUI-only ([`kubectloffer_prefs.go`](../../internal/ui/fyneui/kubectloffer_prefs.go);
  `TestKubectlPreferencesFields`, `TestKubectlPreferencesAreTheUsers`); the Preferences row partial
  (a free row from missing; counted in the re-estimate of 2026-10-15, below). The terminal's check and download are G8's; the
  download goes through the HTTP proxy for non-cluster traffic too.
- **Built 2026-10-13 (night run): a cluster's accessible namespaces** (Settings › Namespaces,
  [ui parity](ui-parity.md#cluster-settings), [`engine/accessiblens.go`](../../internal/engine/accessiblens.go)):
  a field that adds one namespace at a time and a list that removes them, kept per context in
  `cluster-settings.json`, set by agents with `cluster.settings.set` (a UI command); the namespace
  filter offers them while Namespaces cannot be listed, and a kind the user may not list in all
  namespaces is read in each of them ([G3](#g3-cache-and-the-resource-model)). Cogpit's defaults,
  not the owner's decisions: sorted, at most 100, and without any, the filter's namespaces, else the
  context's, are read (`TestClusterNamespacesField`, `TestNamespaceFilterOffersAccessibleNamespaces`).
- **Built 2026-10-17 (the owner's F22, [D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)):**
  the cluster lists say how long a context's client certificate is valid: the navigator's section a
  second line under the name ("cert expires in 23 days", "expired" alone (the owner, 2026-10-17); muted, the
  warning colour within 14 days or the last fifth of its lifetime, the error colour once expired),
  the welcome page a pill, the row's and the hotbar tile's tooltips the date; `cluster.contexts`'
  `clientCert` and `cogpit-cli contexts`' CLIENT CERT. Only the leaf's two dates, never the key; a
  `client-certificate` file in a place macOS protects read only after a connect read it
  ([file access › H-119](../security/file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)).
  Never blocks a connect. Cogpit's own, no parity row's status moves
  ([ui-parity › Clusters](ui-parity.md#clusters), [beyond Lens › Everyday use](beyond-lens.md#everyday-use));
  not tried in a real window. The estimate does not move.
- **Built 2026-10-17 (the owner's F25, F26 and F27, [G16](#g16-stability-and-the-user-bug-backlog)):**
  a cluster's uploaded icon on its tabs, as the navigator and the hotbar show it (F25); a cluster's
  accent colour, the user's alone, on Cluster Settings › General, read by agents and shown nowhere
  else yet ([D-80](../developer/decisions.md#d-80-a-clusters-accent-colour), F26); copy icons by the
  cluster's name and its kubeconfig's path on the same page (F27). The tab icon row and the General
  row stay partial (not tried in a real window), the accent colour is a row of
  [beyond Lens › Everyday use](beyond-lens.md#everyday-use) (partial). The estimate does not move.
- **Missing (2026-10-15; 2026-10-16: the Lens Metrics row partial, G12, then not a target, D-61):** 7 of the 64 rows of these sections. Free: Reload (⌘R, not bound by
  Cogpit's reading: the views are watch-fed, so a reload could only list again, which the
  cache-first rule forbids); Allow untrusted certificate authorities for non-cluster traffic (held
  back, a question for the owner); several windows (the owner's requirement of 2026-10-14 evening,
  a hotbar tile dragged out of the window opening another window; designed 2026-10-15, postponed by
  the owner until the rest is done, [D-52](../developer/decisions.md#d-52-several-windows)). Paid: Cost
  ([G18](#g18-paid-lens-desktop-features)), the Lens CLI switch; the cloud sections and the cloud
  integrations are not a target (owner, 2026-10-22: won't build, D-9).
  Decided 2026-10-15 evening and built that night ([D-57](../developer/decisions.md#d-57-choosing-a-cluster), [roadmap › Current
  focus](roadmap.md#current-focus) item 3): choosing a cluster from one list, the navigator's
  section; the context picker, the top bar's "Switch context ▾" and the scope dropdown gone, ⌘P,
  the Cluster menu's renamed "Connect to Cluster…", the palette's entry and the workspace's button
  (the welcome page's too until 2026-10-16 night) show the section with its search focused, Escape on that empty search goes back; the lead's
  decisions in D-57 confirmed by the owner in the audit of 2026-10-16, but the menu path, which he
  changed, and the welcome page's button, which he changed (2026-10-16 evening: the welcome page without it, the workspace's kept; built 2026-10-16 night, `TestConnectToClusterButtons`); not tried in a real window. Missing parts of partial rows: zoom (Fyne has
  no runtime scale), the editor's minimap and font family, the terminal's theme and font family
  ([G8](#g8-logs-terminal-and-shells)), start at login, deep links and Minimize on Windows and Linux
  ([G22](#g22-windows-and-linux)), the update channel's Beta once Cogpit publishes prereleases.
  Built since this list first named them: kubeconfig syncs (2026-10-12), the shortcuts of the tabs
  (2026-10-13) and Ask AI's ⌘I opening Cogpit's chat (2026-10-14 night,
  [G21](#g21-built-in-agent)), Preferences' Helm repositories (read from Helm's own file, D-29) and
  `HELM_DRIVER`, the update channel (Stable only, D-4), the terminal's right-click copy and paste,
  the theme (Sync with computer the default), start at login on macOS
  ([D-48](../developer/decisions.md#d-48-start-at-login)), `cogpit://` links on macOS, navigation
  only ([D-47](../developer/decisions.md#d-47-deep-links-cogpit)), and the cluster settings' Node
  Shell page (D-38, D-37).
- **Evidence:** [ui-parity](ui-parity.md), 65 rows across those sections (7 more are marked not a
  target), 57 existing on 2026-10-15, 1 done and 56 partial; 7 of them partial since 2026-10-12 (the hotbar's six and the Cluster menu), the
  navigator keyboard since 2026-10-13 ([G5](#g5-app-shell-navigation-tables-and-drawer)'s navigator; ⇧↵ opens a fixed tab since the tab bar),
  and 7 more since 2026-10-13 (the palette's two free rows and five shortcut rows; the paid bank
  commands' row updated); 11 more since 2026-10-13 (Preferences and Cluster settings: the two
  shortcut rows, the application menu's Preferences, Cluster identity, Theme, Locale timezone,
  Editor, Tables, Terminal, Cluster settings › General and Terminal; the Cluster menu, Kubeconfig
  syncs, Metrics provider and Appearance's theme row updated); and 5 more since 2026-10-13 (the tab
  bar's shortcut rows: close the tab, back and forward, next, previous, tab *n*; G5's tab bar); and
  3 more since 2026-10-13 (the Window menu, Help › Welcome, zoom and full screen; the Cluster menu
  updated; G5's welcome page is the fourth); and 2 more since 2026-10-13 (night run: Navigator
  sections and Drill into a section or cluster, partial; Search clusters by name and the navigator's
  keyboard updated; G5's navigator section level and scope); and 1 more since 2026-10-15 (the
  hotbar's drag to reorder, R2, a paid row). Since 2026-10-17 two of those rows are not a target,
  the hotbar's banks and the palette's bank commands (the owner's one hotbar, a deliberate
  deviation, D-83): 63 rows, 9 more not a target.
- **Estimate:** 28–96 h, re-estimated 2026-10-15 (the lead's estimate, PhotoCraft's calibration):
  several windows a subsystem, 8–20 h (postponed by the owner, still in the target); the one list
  of clusters 2–6 h (decided 2026-10-15, new work); zoom 2–6 h; the editor's minimap and font
  family 2–8 h; the untrusted authorities 0–2 h if the owner wants them; the 54 partial rows that
  are this entry's (the two metrics rows of Cluster settings are G12's) at 0.25–1 h, 14–54 h. The
  Preferences and Cluster settings, the menus, the navigator's sections, the hotbar's drag and the
  F-keys in search fields had not moved the estimate. Was 50–140 (was 58–150; the palette and five
  shortcut rows built, night run; 1–2.5 h per row).

## Platform gaps

### G14. Packaging, signing and updates (macOS)
- **Decided (owner, 2026-10-14 evening):** confirmed: a failed signature or notarization fails the
  job, no files until it runs again, notarization waited for up to 45 minutes (since 2026-10-15,
  commit 163664f2, in a job of its own for up to 330 minutes, after Apple kept the submissions of
  v0.9.0 to v0.12.0 beyond 45 minutes and those releases got no files); Sparkle's 20-day
  failsafe kept; agents and the CLI may turn the automatic update check on, never off, the user both
  ways; the other choices of the Sparkle build stand (not asked). To do with the owner's go-ahead,
  a change to GitHub's settings: a repository ruleset that lets only the semantic-release GitHub
  App create `v*` tags (its only bypass, decided further on 2026-10-15), made 2026-10-22, a probe tag
  from the owner's account refused; the administrators' bypass of the environment `release` off,
  made the same day. Moot by the owner's decision of the same evening (only the disk image ships, the
  CLI inside the app): the disk image replacing the zip, and a `cogpit-cli` notarized but not
  stapled. Estimate not moved.
- **Missing:** the universal macOS app bundle (Intel), the Homebrew
  cask, an SBOM and signed checksums ([D-4](../developer/decisions.md#d-4-packaging-and-updates)).
  **Human:** Sparkle's update key and the rules of the environments `sparkle` and `github-pages`
  set up on 2026-10-15 ([releasing › setting up Sparkle](../developer/releasing.md#setting-up-sparkle));
  the appcast jobs in `sparkle` sign without the owner's approval since 2026-10-22 (his choice,
  [H-141](../security/updates.md#h-141-the-update-key-signs-without-an-approval)); the first release with
  the key that passes notarization starts the signed feed, the one after it proves the update. The app's minimum macOS is 13.0
  since 2026-10-14 (it said the build runner's 26.0 before; [the minimum macOS](../developer/releasing.md#the-minimum-macos)),
  not tried on macOS 13.
- **Built (2026-10-14, agent REL), proven by v0.6.0 to v0.8.0** (each carries the signed,
  notarized disk image; v0.5.0's "Sign and notarize" job failed): the Developer ID signature
  (hardened runtime, no entitlements), the disk image `Cogpit-<v>-macos-arm64.dmg` and the
  notarization with the ticket stapled, `cogpit-cli` for macOS signed and notarized, in a job of
  its own in the environment `release` ([`hack/macos/release.sh`](../../hack/macos/release.sh),
  [`build.yml`](../../.github/workflows/build.yml),
  [releasing › signing and notarization](../developer/releasing.md#signing-and-notarization));
  checked locally signed ad-hoc. A failed signature
  or notarization fails the macOS job and leaves the release without files until it is re-run (the
  lead's decision, confirmed by the owner 2026-10-14 evening, above; D-4). Estimate not moved: the
  proof took nothing off the list under Missing.
- **Built (2026-10-14, agent CLIBUNDLE; the owner's revision of D-16), proven by v0.7.0 and
  v0.8.0** (the disk image and `SHA256SUMS` their only files): `cogpit-cli` inside `Cogpit.app` (`Contents/MacOS`, identifier
  `io.guided-traffic.cogpit.cli`), signed inside-out with the app and covered by the disk image's
  one submission instead of a bare binary notarized on its own; a release carries the disk image
  and `SHA256SUMS` only, no `cogpit-cli` archives for any platform; the installed app's Copy MCP
  command registers its own `cogpit-cli` over stdio, once (D-2). Checked locally signed ad-hoc
  (`cogpit-cli version` from the mounted disk image). Estimate not moved: it replaced work, it
  added none to what remains.
- **Built (2026-10-14, agent SPARKLE), not yet proven:** Sparkle 2.10.0 in the app, pinned by
  version and SHA-256, loaded at run time ([`internal/update`](../../internal/update/)),
  `CFBundleVersion` the release's version; Preferences › App › Check for updates automatically and
  Update channel (Stable), Help › Check for Updates…; Sparkle's parts signed inside-out with the
  Developer ID; the appcast drafted, signed (Sparkle 2.9's signed feeds) in the environment
  `sparkle` and deployed to Pages in three jobs ([releasing › the appcast](../developer/releasing.md#the-appcast)).
  Checked locally with a throwaway key; the releases carry the owner's key since v0.11.0; v0.14.0 and
  v0.15.0 passed notarization on 2026-10-16 and carry their disk images (v0.13.0 too: Apple kept
  it "In Progress" beyond the 330-minute wait, and a re-run attached it at 2026-10-15 23:52 UTC), and since that morning Pages serves the signed feed with
  0.16.0 and 0.15.0 (approved by the owner; v0.17.0 to v0.19.0 followed the same day with their disk images, each appcast approved in release order, so by the evening the feed holds 0.19.0, 0.18.0, 0.17.0, 0.16.0 and 0.15.0; v0.20.0 (2026-10-16 19:21 UTC) and v0.21.0 (23:39 UTC) followed in the night and v0.22.0 (08:14 UTC) and v0.22.1 (09:32 UTC) on 2026-10-17, their appcasts waiting for the owner's approval). The same morning the first installed copy updated
  through it: the owner's MacBook from 0.15.0 to 0.16.0, the launch check's Update button clicked
  and Sparkle's install (a person's observation, not a test), so the signed manifest is done for
  macOS (Linux updates through apt since 2026-10-20 night, and Windows through WinSparkle since
  2026-10-21 on the branch `windows-release`, its button installing with one click, D-111, not yet
  through a release: [G22](#g22-windows-and-linux)). Not observed: a scheduled check
  of a copy that has a last check time. The disk image opens in the window the owner chose on 2026-10-16
  ([D-70](../developer/decisions.md#d-70-the-disk-images-window)): built, checked locally, its jobs
  not run on CI yet. Estimate 12–30 h (was 22–45): the
  universal app, the cask, an SBOM and signed checksums, and what the first signed updates show.
- **Evidence:** [platform-parity](platform-parity.md#operating-systems-and-packages), the macOS and
  update rows.
- **Impact:** an unsigned build is enough for alpha testers; beta users need a notarized app and
  updates.
- **Evidence (2026-10-11):** `make app` builds an ad-hoc signed `Cogpit.app` for arm64 locally.
- **Decided (owner, 2026-10-12, [D-16](../developer/decisions.md#d-16-the-first-real-release)):**
  releases stay dry runs until the alpha; the job that builds the unsigned app as a download
  belongs to the alpha's work and is switched on with the first real release. **Revised (owner,
  2026-10-13):** releases are live; every release carries `Cogpit_<v>_darwin_arm64.zip` (Apple
  silicon, ad-hoc signed, not notarized), `cogpit-cli` for six platforms and `SHA256SUMS`, built by
  [`build.yml`](../../.github/workflows/build.yml) ([releasing › the release pipeline](../developer/releasing.md#the-release-pipeline));
  estimate not moved. **Revised (owner, 2026-10-14):** only the complete product, `cogpit-cli`
  inside the app (above).
- **Estimate:** 12–30 h (was 22–45, before that 25–50). **Human:** none of A10 left (the tag ruleset and
  `release`'s admin bypass off made 2026-10-22; the appcast jobs in `sparkle` sign without his
  approval since 2026-10-22, H-141) (the update key and the environments' rules exist since 2026-10-15; the Apple
  Developer ID certificate and a notarization API key since 2026-10-14, in the environment
  `release`).

### G22. Windows and Linux
- **Built (2026-10-20 night, [D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages);
  the owner's request of 2026-10-20 evening, Linux brought ahead of the macOS beta, the details the
  lead's, open to revision):** the Linux package, `cogpit_<version>_<arch>.deb` for amd64 and arm64
  (`make dist-linux`, [`hack/linux/deb.sh`](../../hack/linux/deb.sh)) with both binaries, the
  launcher, icons, an AppStream entry, the update helper and its polkit action, and the sources file
  of Cogpit's signed APT repository on GitHub Pages ([`hack/linux/apt.sh`](../../hack/linux/apt.sh),
  the key in the environment `apt`); for Debian 13 and Ubuntu 24.04 and later; the release
  pipeline's Linux jobs and CI's Linux Package on every push; the app's updater on Linux reading
  apt's state, installing through `pkexec` and the helper on the user's click, then Restart to Update
  ([`apt_linux.go`](../../internal/update/apt_linux.go)); silent updates are each user's own system
  setting, never the package's (the owner). Checked in Ubuntu 24.04 containers: installed, upgraded
  0.0.2 to 0.0.3 through the helper from a repository signed with a test key, the window open under
  Xvfb, `apt remove` and `apt purge`.
- **Built (2026-10-21, on the branch `windows-release`,
  [D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle); the
  owner's request of that day, Windows brought ahead of the macOS beta, the owner's choices of the
  setup, the updater, the first release unsigned and the button's one click, the details the
  lead's, open to revision):** the Windows setup `Cogpit-<version>-windows-x64-setup.exe` (`make
  dist-windows`, [`hack/windows/setup.sh`](../../hack/windows/setup.sh),
  [`packaging/windows/cogpit.nsi`](../../packaging/windows/cogpit.nsi)), NSIS, per user without an
  administrator, `/allusers`, `/S`, `/D=`, with `cogpit.exe` (a GUI program), `cogpit-cli.exe` and
  WinSparkle 0.9.4; the release pipeline's Windows jobs (the setup, its attach job, the Windows
  appcast drafted, signed with the update key in `sparkle` and deployed to Pages) and CI's Windows
  Package; the updater ([`winsparkle_windows.go`](../../internal/update/winsparkle_windows.go)):
  Cogpit reads the signed `appcast-windows.xml` for the Update button, whose click installs at once
  (`app.installUpdate`), Help › Check for Updates… shows WinSparkle's window; the local terminal on
  ConPTY in a job object, PowerShell by default, kubectl first by its folder or a hard link
  ([`localpty_windows.go`](../../internal/terminal/localpty_windows.go)); the MCP registration
  commands in PowerShell's form and the stdio registration of the setup's `cogpit-cli.exe`. Every
  test of the repository ran on Windows for the first time (91 failures in 15 packages, most the
  tests' own; the real faults fixed: renames and removals of files another process holds, a log's
  resume under the coarse clock, the program a login's approval binds, Winsock's error numbers,
  file URLs and paths in the window), and CI runs them there since (Unit Tests (windows), UI Tests
  (windows)). Checked on the owner's Windows 11 Pro: the setup built, installed, installed again
  and uninstalled; the release build's window; an update end to end with a throwaway key and a
  local feed, 0.0.1 to 0.0.2 by the button's click.
- **Missing:** on Linux, the first release with the packages (the one after v0.32.0) and the
  repository served from Pages; a person's look on a desktop (polkit's password dialog, the
  restart, an update through the Software Updater, X11 and Wayland in a real session, the arm64
  package beyond GitHub's ARM runner); `cogpit://` links (a link starts a second process, which needs
  a hand-over to the running Cogpit, D-108), Start at login, the window's place beyond its size and
  full screen (D-95), the system's file panels; AppImage, rpm, tar.xz and Flatpak, an RPM repository and Flathub. On Windows:
  the first release with the setup (the first after the branch's merge, which the owner approves),
  Authenticode for the setup and both executables ([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode);
  WinSparkle.dll stays its maintainer's unsigned build, [H-140](../security/updates.md#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)),
  an ARM64 setup, winget and Scoop, `cogpit://` links and Start at login, the system's file dialogs
  (Fyne's speak the system's language while Cogpit speaks English, and the folder chooser lists no
  drives), the region's date and time format, the window's place beyond its size and full screen,
  Enforce kubectl for PowerShell and cmd, `~\` in path fields, Windows' own proxy settings for the
  Update button's reading of the feed, lint and GoSec over the files built only for Windows (CI
  lints on Linux); not tried yet: an installation for every user through UAC, a terminal tab in the
  real window on ConPTY, PowerShell 7, Windows 10, ARM. Releases up to v0.6.0 carried `cogpit-cli`
  alone for both, later releases nothing for Windows until the setup
  ([D-16](../developer/decisions.md#d-16-the-first-real-release), owner, 2026-10-14).
- **Evidence:** [platform-parity](platform-parity.md#operating-systems-and-packages), the Windows,
  Linux, APT and update rows.
- **Estimate:** 20–45 h (the lead's, 2026-10-21: the Windows setup, WinSparkle, the terminal on
  ConPTY and the first run of every test on Windows took about one subsystem, 8–20 h at the
  borrowed calibration, less what no release and no Windows 10 has shown yet; was 30–65 after the
  Linux package, 40–80 before it). **Human:** Windows code-signing material (Authenticode); the
  owner's approval of the merge of `windows-release`; a look at the Linux package on a desktop.

## Quality gaps

### G15. Performance and API load at scale
- **Missing:** budgets P3, P5–P8 on the GPU renderer, P1 with a real cluster's round trips and the
  window's paint, and all of them against Lens on the same machine
  ([testing](../developer/testing.md#performance-budgets-proposed-not-measured)), the nightly run on a
  fixed runner, request counts for the scenarios not covered yet. Measured on 2026-10-11: P2,
  P3, P4, P7, P8, P9–P11 (GPU numbers on one Mac, no Lens comparison); on 2026-10-14 P1's
  engine share (`TestBudgetFirstRows`: 111–123 ms to 1,000 rows, 394–400 ms to 10,000); `make bench` and
  `cogpit --perf-mode gate` run the measurements.
- **Built 2026-10-16 (night run, [D-15](../developer/decisions.md#d-15-progress-generators)):**
  `make scorecard` measures P1, P2, P4, P5, P8 and P9 to P16 in one run without the race detector
  and writes [`scorecard.md`](scorecard.md) (by hand, not in CI); the first run on an Apple M1 Ultra:
  all 13 within their limits. What it measures is Cogpit's share offscreen and against fakes, so the
  Missing items above stand; estimate not moved.
- **Start-up reservation of gate S1** (owner, 2026-10-11): every window stalls its main thread for
  0.3–0.56 s while Fyne sets up GL, and the first fill of a 10,000-row table for 151–193 ms (text
  textures). To try: render the first frame before the rows arrive, warm the text textures of the
  first screen off the critical frame, and avoid second windows ([spike S1](../developer/spike-fyne.md#results)).
- **Evidence (2026-10-11, the gate's offscreen test):** `TestPerfGateRunsEveryPhase` hung once
  until go test's 10-minute timeout on the macOS CI runner (7 s locally). Cause, reproduced with
  a main queue slower than one callback per frame (the race detector and coverage on a loaded
  runner): the jank meter kept posting a callback every 16 ms after it had reported, so the test's
  drain never emptied the queue and never reached its deadline; and a sample still queued at the
  report ended the measurement a second time, closing the YAML window twice, which panics inside
  Fyne's test driver while it holds its window lock, so the cleanup's next driver call blocked for
  good. Fixed in the meter ([`perf.go`](../../internal/ui/fyneui/perf.go)): it stops its ticker
  when it reports, samples after the report do nothing, it never waits on the main goroutine for
  the YAML document, and a view switch waits (at most 10 s) for the view's rows instead of
  switching away from a slow view; the test drains in batches and checks its deadline between
  them. `TestPerfGateFinishesOnASlowMainGoroutine` runs the gate with every callback slower than
  a frame (it hangs on the old meter, passes on the new).
- **Impact:** large clusters are where users notice an IDE first, and where an IDE costs the API
  server most.
- **Estimate:** 28–66 h (was 30–70; the perf gate no longer hangs on a slow runner, third run).

### G16. Stability and the user bug backlog
- **Found in the owner's use (2026-10-23, v0.40.1); fixed 2026-10-23 on the branch
  `fix/list-empty-on-entry`** ([architecture › §4.4](../developer/architecture.md#44-tables-details-editors-and-streams)).
  The owner: "Manchmal passiert es, dass ich beim betreten einer Maske keine Objekte sehe. Ein
  minimaler Scroll nach unten bringt dann alle Objekte auf die Ansicht" (Pods, "197 items", the
  header and no row). Reproduced offscreen with tests that failed before the fix: a view entered
  after a longer list was scrolled down, its first snapshots none and then one without rows, kept
  that list's offset past its own end and drew no row; with its rows at once, its first row stayed
  under the header; the same view of another cluster started at the other's offset. A view entered
  now starts at its top. Not seen in a real window yet.
- **Found in the owner's use (2026-10-23 evening, v0.40.1); fixed 2026-10-23 on the branch
  `fix/preview-tab-per-cluster`** ([D-117](../developer/decisions.md#d-117-one-preview-tab-per-cluster)).
  A view on three clusters, the first two in fixed tabs, the third in the preview tab: a click on the
  first tab, then on the second, turned the preview tab into the first cluster's view, and the third
  cluster's was lost (the owner: "mir ist jetzt eine Ansicht verloren gegangen"). Cause (verified
  with tests that failed before the fix): `SelectTab` of another cluster's tab cleared the active
  tab while the cluster left was still shown, and `syncTabs` took the shown view for one just
  opened and moved it into the one preview tab in all. Fixed: a tab switch changes no other tab, and
  every cluster has a preview tab of its own, the owner's pick of three
  (`TestSelectingAnotherClustersTabChangesNoTab`, `TestThreeClustersKeepTheirPreviewTabs`,
  `TestWorkspaceKeepsOnePreviewTabPerCluster`). Not seen in a real window yet.
- **Found in the owner's re-walk of RW1–RW4 (2026-10-23, v0.40.1, the staging cluster); open, not
  built** ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer),
  [D-104](../developer/decisions.md#d-104-dialog-fields-their-hints-and-dropdowns-the-gate-walks-gw2-gw5-gw6)).
  RW1–RW4 and the logo passed in a real window (below); four findings on the buttons, none blocking
  a workflow. **CW1, Hide as wide as Show** (the owner: "damit man mit der Maus nicht erneut auf
  hide ziehen muss weil man an der "falschen" Stelle von Show geklickt hat"): Show carries RW3's eye,
  Hide only its word, and a key's line ends at the right, so Hide starts further right than Show and
  a click on Show's eye misses it (`revealButton`, [`secretreveal.go`](../../internal/ui/fyneui/secretreveal.go)).
  **CW2, Hide green with a closed eye** (the owner: "Hide könnte doch ein grünes Button sein, weil
  es die visuelle sicherheit wiederherstellt, vielleicht haben wir auch ein geschlossenes Auge?"):
  Fyne 2.8.2 has the eye struck through (`theme.VisibilityOffIcon`), the green is `colorStatusOK`;
  with an icon Hide comes near Show's width, equal only with a shared minimum width. **CW3, Undo one
  height with the other buttons** of a removed key's line (the copy and remove icons), the family of
  the owner's finding of the same morning: Show, the copy icon and the remove icon one height, and
  every button outlined, icon-only ones included (recorded only). **CW4, the Preferences' agent Edit
  wider**: tight around its word (a short word in Fyne's button padding, `textButton`,
  [`textbutton.go`](../../internal/ui/fyneui/textbutton.go)); whether this Edit alone or every short
  text button gets a minimum width is open.
- **Found 2026-10-23 in CI (a race-detector run of PR #87, run 39923914751; open, not built).**
  `TestConnectEndsWhenItsContextLeftCogpit` ("a connect that succeeded, its source removed") failed
  once: after the connect returned, the UI state still said `connecting` for the removed context,
  with no backend. Read in the code, not reproduced: `settleGoneConnect`
  ([`connect_gone.go`](../../internal/engine/connect_gone.go)) runs at the end of a successful
  connect (`connect_run.go`), before the window's own connect publishes Connected; its
  `forgetGoneContext` leaves a context alone while the UI says `Connecting`, so when the watcher's
  close of the connection and the window's update fall the other way, nothing forgets the context
  and the window can stay "connecting" for a cluster whose kubeconfig source was removed during its
  connect. Rare (the same commit passed on `main`); the engine untouched by that pull request.
- **Found 2026-10-23 (the owner's question: a colleague on a sharp display finds the navigator's
  text blurry); the logo built 2026-10-23 (PR #87, v0.40.1), the text open.** Read in the code, not reproduced (no
  second display with another density at hand, the app not started). The navigator draws nothing
  its own way: its rows are Fyne's `canvas.Text` and SVG icons, which Fyne's painter rasterises at
  the display's pixels and places on whole pixels. **Built:** the two pictures Cogpit draws itself,
  the logo in the header and the welcome page's, took their pixel size from the canvas scale, which
  Fyne keeps at 1 on a Retina display on macOS (the factor 2 is the framebuffer's), so they were
  drawn at half the display's pixels and stretched; now at the display's (`pixelsFor`,
  [`bars.go`](../../internal/ui/fyneui/bars.go); `TestLogoDrawnAtTheDisplaysPixels`); the owner on
  his MacBook's display in the re-walk of v0.40.1 (2026-10-23): no great difference to before, it
  always looked good to him, accepted. **Open, a
  candidate for the navigator:** Fyne 2.8.2 keeps a text's rasterised texture by its text, size,
  style and colour, not by the density; when a window changes density on macOS (moved between a
  Retina and a 1× display, or its first frames drawn on the other one) its driver sets the new
  factor (`processFrameSized`) without dropping those textures, which only a scale or theme change
  does (`RescaleContext`). Text that never changes, as the navigator's, would keep the old density
  while the views' text, drawn after the connect, is sharp. The owner's call (2026-10-23): the
  workaround (re-apply the theme when the density changes) only once the colleague's test confirms
  it (switch the theme and back: sharp afterwards means this cause; still soft at 1× means Fyne's
  text at 1×, grayscale and unhinted); an issue at Fyne agreed with him first.
- **Found in the owner's re-walk (2026-10-23, v0.40.0, the staging cluster); built 2026-10-23 (PR
  #86), released in v0.40.1 the same day** ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer),
  [D-104](../developer/decisions.md#d-104-dialog-fields-their-hints-and-dropdowns-the-gate-walks-gw2-gw5-gw6),
  [D-114](../developer/decisions.md#d-114-the-alpha-walks-decisions-a-failed-connects-page-and-its-stages)); tests failing before
  the change, offscreen renders dark and light; passed the owner's re-walk of v0.40.1 (2026-10-23, the
  staging cluster), each in a real window, dark and light (his findings CW1–CW4 there: above). The re-walk of AW1–AW19 and
  of the Secret copy (D-25) passed, every fix in a real window; three findings on a Secret's drawer
  and one on the welcome page after it, none blocking a workflow. **RW1, a masked key's buttons**
  (the owner: "Im Zugeklappten Zustand sollen die Button genau so angeordnet sein, wie es es schon
  für den geöffneten Zustand umgesetzt hast. Show Copy-Symbol Delete-Symbol"): a masked key's name
  line ends with Show, the copy icon and the remove icon, always shown, as a revealed key's "Hide
  [copy] [remove]"; the value line under it holds the mask and the size only, without a copy icon of
  its own (`revealLines`, `buttonsLine`, [`secretdata.go`](../../internal/ui/fyneui/secretdata.go)).
  The same for an edited value hidden again, an added key hidden again, and a Secret the user may not
  save ("Show [copy]", no remove icon; Show revealing read-only turns into Hide in the same place);
  a binary value keeps its remove icon alone (`TestSecretMaskedKeyButtonsOnNameLine`,
  `TestSecretReadOnlyKeyButtonsOnNameLine`). **RW2, text buttons hard to see** (the owner: "dass man
  den Rahmen der Buttons weder im Dunklen noch im Hellen Theme richtig erkennen kann. Buttons auf
  dem Drawer müssen wie das Dropdown vorhin besser erkennbar werden."): every button with a word and
  no plate of its own, in the whole window (the owner's choice of the scope, 2026-10-23), draws the
  field's outline (`theme.ColorNameInputBorder`, as the dropdown since AW1), the primary colour while
  it has the keyboard, the disabled colour while disabled (`textButton`,
  [`textbutton.go`](../../internal/ui/fyneui/textbutton.go); `TestTextButtonsHaveTheFieldOutline`):
  the drawer's Show, Hide, Add Key, Undo and item actions (Forward…), the banners' Dismiss and
  Discard, Clear all, Revert to Original, Reset Column Order, the log tab's pickers, a job's Cancel,
  the Preferences' agent rows and the MCP details. Icon-only buttons (copy, remove, close) and the
  find bar's toggles stay bare. **RW3, Show marked as exposing** (the owner: "Vielleicht rot, weil es
  löscht ja nichts, aber ein warnendes Orange? Dir fällt was ein."; his pick A of three rendered
  looks): a Secret's Show has its word, its outline and an eye icon in the status warning's amber;
  Hide stays plain (`revealButton`, [`secretreveal.go`](../../internal/ui/fyneui/secretreveal.go);
  `TestSecretShowMarkedAsExposing`); the MCP token's Show the same (the lead's detail).
  **RW4, the welcome page's message squeezed** (the owner: "Der Fehlertext auf dieser Seite geht
  etwas unter … braucht die meldung auch einen angemessenen platz um als Info, Warning oder Error
  eindeutig erkannt zu werden."; his pick C of three rendered looks): the message stands in a
  banner of its own between the tagline and the clusters, with room above and below: a plate tinted
  and outlined in its level's colour, the level's icon and a bold title in that colour ("Connection
  closed", "Could not connect", "Connected to …") over the message
  ([`welcomenote.go`](../../internal/ui/fyneui/welcomenote.go); `TestWelcomeNoteNamesItsLevel`).
- **Found in the owner's alpha walk (2026-10-22, v0.38.0): Escape and the dropdown field (AW1–AW4,
  AW19); built 2026-10-22 on a branch** ([D-116](../developer/decisions.md#d-116-escape-closes-the-innermost-thing),
  [D-104](../developer/decisions.md#d-104-dialog-fields-their-hints-and-dropdowns-the-gate-walks-gw2-gw5-gw6));
  tests failing before the change and offscreen renders; passed in the owner's re-walk of v0.40.0
  (2026-10-23). Escape
  closes the innermost thing open: over a dialog it is the dialog's Cancel and the drawer behind it
  stays (AW3; every Fyne dialog and framed dialog, a field of it with the keyboard included,
  `TestEscapeCancelsEveryDialog`, `TestEscapeCancelsTheRollbackDialog`); Connection Updates closes
  (AW19, `TestEscapeClosesTheConnectionUpdates`); a list's search is cleared and the table gets the
  keys, the find bar closes from its fields and from the editor (AW4, `TestEscapeLeavesTheListSearch`,
  `TestEscapeClosesTheFindBar`; D-57's "any other search" had been the navigator's only); Escape on
  an open dropdown's list takes the keyboard off the field, so the next Escape goes on (AW2,
  `TestEscapeOnAnOpenDropdown`); a closed dropdown has the outline an entry has, and the outline of
  every field reads at 2:1 on every surface, both themes (AW1, `TestClosedDropdownHasAnOutline`,
  `TestPaletteContrast`; renders `rollback_dialog_*.png`, `preferences_*.png`). Escape still stops
  in a settings page's text field, a button reached by Tab, a check group's or a radio group's item
  and inside Fyne's file and colour dialogs (D-116's "What it does not cover").
- **Found in the owner's alpha walk (2026-10-22, v0.38.0, the staging cluster); all nineteen built
  2026-10-22 (PRs #74 to #82, each marked here or in the bullets beside it), re-walked by the owner
  on v0.40.0 on 2026-10-23, each passed.** The
  re-walk of every fix that touches the [alpha gate](roadmap.md#alpha-gate) passed; these findings
  block no workflow. His decisions on AW7, AW13, AW14 and AW17 are
  [D-114](../developer/decisions.md#d-114-the-alpha-walks-decisions-a-failed-connects-page-and-its-stages).
  **Escape** (AW2, AW3, AW4, AW19): built, the bullet above. **Focus (built 2026-10-22, the next
  bullet):** ⋮ › Edit (AW5) and ⌘⇧T (AW8) opened their dock tab without the keyboard focus in it; ⌘F
  in the Edit tab opened its find bar after about 3 s (AW6). **Toasts that are no errors** (built 2026-10-22 on a branch,
  [D-97](../developer/decisions.md#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays);
  engine, command, MCP and window tests each failing before the change; passed in the owner's
  re-walk, 2026-10-23): the user's own Cancel Connecting (AW15) and a connect going on in the background when another
  cluster is shown (AW16) each raised an error toast in the Go error's form ("connect <context>:
  connecting cancelled", or "superseded by a newer connect" when the run ended at that moment;
  "…: another cluster was shown; the connect goes on in the background"), because a hotbar tile, a
  tab and the navigator passed the connect's error on where `cluster.connect` answered `connecting`.
  Now neither raises a toast on any path, typed outcomes (`ConnectNotFailed`); a failed connect still
  does; agents read a cancel as `cancelled`, not `failed`. Found on the way (another agent, the same
  day): a second connect of the cluster the window waits for ended the first one's wait the same way
  ("another cluster was shown"); it now joins that wait. **Each on its own:** a closed dropdown field with
  the background's colour (AW1, Dark Cockpit) built, the bullet above; Edit on an object whose tab is open sometimes does
  not open the minimised dock again, not narrowed down (AW9, built: below); ⌘⌥⇧← and ⌘⌥⇧→ move the top tab bar's
  tab only, not the dock's (AW10, built: below); a namespace deleted while the list is filtered to it turns the
  filter red only at the next change of the view's rows or when the filter opens, so a namespace
  without pods is noticed late (AW11, `recheckNamespaces`; **built 2026-10-22 on a branch**: the
  engine watches the namespaces while the filter names some and its `EventNamespaces` reads them
  again, the rows no longer do, [D-106](../developer/decisions.md#d-106-the-gate-walks-gw1-gw3-gw4-and-gw9-the-drawer-f-moving-tabs-a-deleted-namespace);
  tests failing before, a user who may not list namespaces reads nothing; passed in the owner's
  re-walk on the staging cluster, 2026-10-23); a click below a list's rows closed the
  drawer after about 1 s, × and Escape at once (AW12; built 2026-10-22 on a branch, passed in the
  owner's re-walk, 2026-10-23: Fyne's driver holds every tap on a `fyne.DoubleTappable` for the double-click
  interval, 0.5 s on macOS by default, and the list's table was one for its header separators'
  double click; it counts that double click itself since, as the tab bar does, so the click closes
  the drawer at its release, [D-106](../developer/decisions.md#d-106-the-gate-walks-gw1-gw3-gw4-and-gw9-the-drawer-f-moving-tabs-a-deleted-namespace);
  `TestListClicksAreNotHeld` fails before the change; clicks on a row or a header were not held;
  the welcome page's cluster rows held theirs too, their double click doing what a click does, and
  connect at the release since, a second click while the connect is under way, a double click's
  or the login program's review included, connecting nothing more, `TestWelcomeRowConnectsOnce`); a clipped "Could not" drawn at the
  navigator's right edge after a failed connect (AW18; **fixed 2026-10-22**: reproduced in a test
  first, the view's placeholder line "Could not connect to <cluster>: <error>" centred at its full
  width from x 128 to 1601 of a view from 289 to 1440, over the navigator drawn before it; the view
  no longer says the failure, its error page does, and every placeholder line fits the view with an
  ellipsis, `TestThePlaceholderStaysInsideTheView`). **Decided (D-114):** a failed connect's error
  page in the welcome page's look with the connect as stages, also in Connection Updates (AW17,
  **built 2026-10-22**: the page whether or not a tab is open, the restore's failure included; the
  stages kubeconfig, a credential plugin's login, DNS with its addresses, TCP with the address and
  port or the proxy's, TLS and the API server's answer, measured on client-go's own first request;
  the error line its cause, the whole chain in the log; tests failing before the change and
  offscreen renders; passed in the owner's re-walk, 2026-10-23, with a client-certificate file
  missing at a restore: the kubeconfig stage failed, the others not reached; the other stages'
  failures not seen in a real window); the dialogs' Context label grey (AW7, built: below); the
  welcome page without "Other clusters" (AW13, **built 2026-10-22**: the hotbar and Recent only,
  "Your clusters" on a first start alone); Follow switched by scrolling (AW14; **built 2026-10-22 on a branch**,
  [D-99](../developer/decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts): five rows
  up off, back at the newest line on, the scroll bar too; tests failing before; passed in the
  owner's re-walk with a real trackpad, 2026-10-23).
- **The alpha walk's focus findings AW5, AW6 and AW8 built 2026-10-22 on the branch
  `fix/dock-tab-focus`** ([D-115](../developer/decisions.md#d-115-a-dock-tab-the-user-opens-takes-the-keys),
  the lead's decisions, open to revision); tests failing before the change, causes verified in the
  code and in Fyne 2.8.2's source; passed in the owner's re-walk, 2026-10-23. A dock tab the user opens or shows
  from the window takes the keyboard on its main widget: ⋮ › Edit's editor (AW5), the terminal of
  ⌘⇧T and of the terminal chip once its session runs (AW8), the Create and Helm tabs' editors, a logs
  tab's lines, a restored draft's editor, a tab clicked in the dock's strip; a tab an agent opens
  leaves the keys where they are, and a terminal still connecting leaves keys the user moved
  meanwhile. AW6 had two causes: the keys stayed in the list after ⋮ › Edit, so ⌘F, routed by where
  the keys are (D-106), focused the list's search; and in a real window the find bar's first opening
  asked for no repaint and could not focus its field never drawn (Fyne's `Container.Show`,
  `CanvasForObject` nil), so the bar showed at the next repaint something else asked for; it now
  repaints and focuses through the window's canvas. Tests: `TestRowMenuEditGivesTheEditorTheKeys`,
  `TestRowMenuLogsGiveTheLogTheKeys`, `TestNewTerminalGetsTheKeys`, `TestStripTabClickGivesTheTabTheKeys`,
  `TestRestoredDraftGetsTheKeys`, `TestATerminalStillOpeningLeavesKeysMovedMeanwhile`,
  `TestAnAgentsTabLeavesTheKeys`, `TestFindBarOpensThroughTheWindowsCanvas`.
- **Built 2026-10-22 on a branch, from the owner's alpha walk**; tests failing before each change
  and offscreen renders; passed in the owner's re-walk, 2026-10-23. **AW7:** a dialog's Context row draws the
  context's source label ("[kubernetes_configs/walk-kind.yaml]", D-28) in the grey of the Object
  row's kind word, the id as it was and wrapped as before, in every dialog that names a place
  (`contextValue`, [`objectnames.go`](../../internal/ui/fyneui/objectnames.go);
  `TestContextRowMutesTheSourceLabel`; render
  [`confirm_apply_removes_dark.png`](../../internal/ui/fyneui/testdata/confirm_apply_removes_dark.png);
  [D-85](../developer/decisions.md#d-85-dialogs-as-tables-and-an-objects-state-without-its-resourceversion)).
  **AW9:** reproduced by a test, the cause verified: a command that shows a dock tab open already
  (Edit, logs, Helm Upgrade and Install, the Audit Log, the Diagnostics, `dock.activate`) set the
  active tab to the one already active, which left the minimised dock collapsed; it worked when
  another tab was active, hence "sometimes". Such a command now opens the dock
  (`UIState.ShowDockTab`, [`engine/layout.go`](../../internal/engine/layout.go);
  `TestShowDockTab`, `TestShowingAnOpenDockTabOpensTheDock`, `TestEditOnTheShownEditTabOpensTheDock`;
  [D-54](../developer/decisions.md#d-54-the-layout-toggles-and-the-update-button)).
  **AW10:** ⌘⌥⇧← and ⌘⌥⇧→ move the active tab of the bar the keyboard works in, the dock's while
  a widget in it has the keys or, with none focused, a dock tab was clicked last
  (`ui.tab.moveLeft` and `ui.tab.moveRight` with `dock`, [`tabmovekeys.go`](../../internal/ui/fyneui/tabmovekeys.go);
  `TestDockStep`, `TestTabMoveCommands`, `TestMoveTabKeysMoveTheDocksTab`;
  [D-106](../developer/decisions.md#d-106-the-gate-walks-gw1-gw3-gw4-and-gw9-the-drawer-f-moving-tabs-a-deleted-namespace)).
- **Found in the owner's use (2026-10-22, v0.37.3); fixed the same day on a branch**
  ([architecture › §4.4](../developer/architecture.md#44-tables-details-editors-and-streams)); tests
  failing before the change and offscreen renders; not seen in a real window yet. acme-d-pinniped's
  Pods list, scrolled down and narrowed by the namespace filter to two pods, read "2 of 200 items"
  and drew no row, where Lens showed both: Fyne's table kept the longer list's scroll offset. A list
  that gets shorter now brings its offset back within its rows, to the top when they all fit.
- **Found in the owner's use (2026-10-22, v0.37.0); fixed the same day on a branch**
  ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections), D-69,
  [architecture › §4.4](../developer/architecture.md#44-tables-details-editors-and-streams)); offscreen
  renders and tests, each failing before the change; not seen in a real window yet. A cluster shown
  while its login was expired (acme-p-pinniped through pinniped), its pods never read: the list said
  "Loading pods…", "0 items" and "syncing…" while nothing loaded, and the namespace filter offered
  the namespaces of the cluster shown before; now the view says the pods were not read before the
  login expired and load once it is renewed (after an interruption: once the cluster answers
  again), without count, "syncing…" or download, and the namespaces of the cluster shown before
  are dropped at the switch. A pod's drawer, moved now and then to another pod, drew its action
  icons from the close button on past the drawer's edge and its status under the tabs: a header
  laid out while the pod loaded and filled before the next frame kept that layout; the drawer now
  lays its header out whenever it changes it.
- **Found in the owner's gate walk (2026-10-20, v0.30.0, the staging cluster) and his use the same
  day (GW10 to GW14); built the same day** ([D-104](../developer/decisions.md#d-104-dialog-fields-their-hints-and-dropdowns-the-gate-walks-gw2-gw5-gw6),
  [D-105](../developer/decisions.md#d-105-reverse-video-as-a-state-and-a-keys-modifiers-from-the-key-itself),
  [D-106](../developer/decisions.md#d-106-the-gate-walks-gw1-gw3-gw4-and-gw9-the-drawer-f-moving-tabs-a-deleted-namespace),
  D-77, D-69); every workflow passed ([roadmap › Alpha gate](roadmap.md#alpha-gate)) and none of
  these blocked one; none seen in a real window yet (offscreen renders, Fyne's test driver, the
  terminal against a real zsh). GW1: a click below a list's rows closes the drawer
  (`ui.closeDrawer`); the checked rows stay. GW2: Scale's count is a slider from 1 to 30 with the
  count typed beside it, five digits wide; 0 is only typed and confirmed as destructive; the title
  reads "Scale Deployment → name" without the gap, which came from Fyne's symbol font having no
  space (every dialog whose title names an object drawn the same way; a single text with "→ " still
  shows the gap elsewhere: a forward's toast, an upgrade's Chart row, a Secret's sizes). GW3: ⌘F
  opens the find bar of the dock's editor tab (Edit, Create, Helm Upgrade, Helm Install) while the
  keys are in it or nowhere; the Edit tab has a find bar. GW4: tabs move by dragging in the tab bar
  and the dock (`ui.tab.move`, `dock.move`), View › Move Tab Left and Right (⌘⌥⇧← and ⌘⌥⇧→); the
  tab bar's order survives a restart. GW5: all 19 dropdowns open their list over the field, the
  current entry checked at the field's place and scrolled to in long lists. GW6: every generated
  dialog (Scale, Drain) sizes its fields to what they hold and puts each hint, in a person's words,
  behind a "?" after the field's name (hover over the name or a click); the "?" takes no keyboard
  focus. GW7: the terminal took each SGR 7 as a colour swap, and zsh's paste highlight repeats it
  under oh-my-zsh (before every character with zsh-syntax-highlighting), so every other cell lost
  it; reverse video is a state as in xterm. GW8: the terminal kept ⇧ from the KeyDown and KeyUp it
  saw; ⇧⌘4's screenshot overlay (the owner's shortcut) took ⇧'s release, and with ⇧ "held" ← and →
  went out as `ESC [ D ; 2`, `ESC [ C ; 2` (after `head -3` exactly `head -;23;2`) and Return as
  nothing until a focus change; a key now takes its modifiers from its own event, ⇧ and Ctrl keys
  send xterm's sequences (found on the way: Ctrl+↓ and Ctrl+Delete sent `^D`, which ends a pod
  shell); the overlay itself is inferred, not seen. GW9, as the owner chose it: a deleted namespace
  the filter names stays red in its list ("· deleted") until unticked, a Terminating one too ("·
  terminating"), the button red; it shows at the filter's next opening when no row of the view
  changes. The side finding was real: the filter offered the first 100 namespaces; it reads them
  all since (`resources.list`'s new `offset`). GW10, the owner's look at the alerts pill: the sign
  the bell's size, a padding before the number, both inside the pill (the number first as tall as
  the sign, then, at his "Die Zahl doch etwas kleiner", the lists' text size); green with
  "0" without alerts, grey with "warn" while nothing answers. GW11, the owner's screenshot of a
  Deployments tab of one cluster beside the shown cluster's Alerts tab at the section ("Ich glaube
  das ist ein Bug"): the tab bar showed every cluster's tabs at the section (D-69 as built); it
  shows the shown cluster's tabs wherever the navigator stands since (`TabsShown`), a restored
  session's other clusters keeping theirs hidden until shown, and **Mixed environments** at the
  foot of All Clusters (the owner's switch, off by default, a "?" with its explanation) shows
  every cluster's. GW12, the owner's RKE2 cluster: the server nodes' EtcdIsVoter (True on an etcd
  member that votes) showed red with the warning icon; a healthy condition since
  (`positiveConditions`; False, a learner, stays a warning). GW13, on v0.31.0 after the update's
  restart: acme-p's connect failed (its certificate expired) and its Pods tab stood over the welcome
  page, a click flashing an empty Pods list; the bar shows no tab of a cluster without a
  connection since, its tabs kept and shown again once it connects (D-69). GW14, a crash on
  v0.31.0: a hover over the "?" of Mixed environments ended Cogpit (`(*tooltip).hover` on nil):
  the navigator is built before the shell's tooltip, and the hint kept the nil it saw then; a
  hint asks for its tooltip at the pointer's event since (`fieldName.tipOf`), and
  `TestMixedEnvironmentsHintHover` hovers and clicks it. The dialogs' "?" were not affected (their
  tooltip exists before their fields); no other widget keeps a tooltip from its construction.
- **Found by the owner (2026-10-19 late evening): fast scrolling jumps, less fluid than Lens's.**
  Proposal 1 built (2026-10-19 night on the branch `fast-scroll`, [D-103](../developer/decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)),
  and merged after the owner scrolled a release build of it on 2026-10-20 ("Deutlich besseres
  Ansprechverhalten"): merged (PR #64), in releases since v0.30.0. Proposals 2–4 stay open (his eyes judge
  whether the 60 Hz timer's unevenness still bothers him). Measured with an instrumented local copy of Fyne v2.8.2 and a scroll
  harness on the demo cluster (scroll events through the driver's own path at 120 per second; a
  2560×1409 canvas at scale 1 on a 120 Hz display; nothing committed). Main cause: Fyne's
  thread-transition check, which Cogpit leaves on (no `fyneDo` migration in the app's metadata):
  every `canvas.Refresh`, so every `Resize` of a rectangle or a text in a cell's layout, takes a
  stack trace to find its goroutine (`internal/async/goroutine.go`), and each scroll event refreshes
  the table on its own. At 10,000 px/s the Pods list drew 6.2 frames per second (tick work p50
  189 ms); with the check off, 60.3 (p50 7.9 ms); at 1,500 and 4,000 px/s 5% and 23% of the ticks
  overran 16.7 ms, none with it off. The YAML view's scroll is the same case. Second cause: Fyne
  draws on a fixed 60 Hz timer (`internal/driver/glfw/loop.go`), not at the display's rate, so
  against 120 Hz input a frame moves one, two or three events' distance. Decided by the owner the
  same evening, not built: an audit of the UI's goroutines first (a Fyne call off the main goroutine
  would race instead of being moved once the check is off), then a build parameter choosing speed
  or safety: release builds the check off (Fyne's own tag `migrated_fynedo` compiles it out; built instead with the app's metadata declaring the `fyneDo` migration, see below),
  development builds and tests keeping it with its warning made visible, each overridable per
  build; on a branch of its own until he has looked at it; then he scrolls a
  release himself, and only if it still jumps for him come frames at the display's rate and smoothed
  input (an upstream Fyne issue first, a fork as the fallback) with one separator line per row
  (−1.2 ms per frame). Built on the branch: `FYNE_THREAD_CHECKS=on|off` in the Makefile, off by
  default in `make dist-app` and so in releases, on in every other build, the repository variable
  `FYNE_THREAD_CHECKS=on` the releases' way back; off means Cogpit's tag `nothreadcheck` and the
  app's metadata declaring the `fyneDo` migration, not Fyne's tag `migrated_fynedo`, which also
  makes Fyne's caches single-threaded (found at build; 60 frames per second at 10,000 px/s either
  way, p50 7.9 ms against 7.0 ms); the audit of every place the GUI's code runs off
  the main goroutine ([ui-threads.md](../developer/ui-threads.md), no stray call found) and a test
  that fails on a new one until it is audited; a development build shows a stray call as a toast.
  Not verified: how smooth it looks, the real trackpad's event rate, Retina scale.
- **Found by the owner (2026-10-19 evening), showing a pod to a colleague over a shared window:**
  every click on a pod only checked it instead of opening its drawer, as if ⌘ were held; one press
  of ⌘ ended it, and it could not be made again on purpose. Cause (verified in Fyne v2.8.2's and
  GLFW's source and with Fyne's test driver, not in a real window): a list's ⌘-click and ⇧-click
  took the keys from the desktop driver's `CurrentKeyModifiers`, the modifiers of the last key
  event it delivered, which nothing in Fyne resets; GLFW clears them on a focus loss only for keys
  it saw go down, and drops the release of one it did not, so a ⌘ that went down outside the window
  and was then used with a key in Cogpit stays "held" until the next key event. What made it stale
  in his window is not known. Fixed 2026-10-19: a click takes ⌘ and ⇧ from its own mouse event, the
  row's and the checkbox's (⇧-click) alike; Space or ↵ on a checkbox takes ⇧ from its key event
  ([architecture §4.4](../developer/architecture.md#44-tables-details-editors-and-streams), the
  lead's choice with options; `TestClickTakesItsOwnKeys`, `TestCheckboxClickTakesItsOwnShift`,
  `TestClickModifiers`, which failed on the old code). Not verified: a real window (his walk step
  R27).
- **Found by a colleague of the owner (2026-10-19 evening), a screenshot:** the Pods list, narrowed
  by the namespace filter to a DaemonSet's thirteen pods, showed three container squares in its
  first row and two in the others; a reconnect made it go away, and it could not be made again on
  purpose. Cause (verified in the code and with Fyne's test driver, not on his cluster): Fyne's
  table gives a cell it keeps on screen new content without a layout when the cell's size is
  unchanged, and the GL driver lays a cell out only when one of its objects' minimum size changed,
  which a Containers cell whose squares all fit never does; the squares were shown, hidden and
  placed only in the cell's layout, so the cell kept the third square of the pod it showed before
  the filter, and a cell that had hidden a third square showed a three-container pod with two. A
  sort, a search or a pod that comes or goes above did the same; a scroll did not (the driver lays
  the moved cells out again). Fixed 2026-10-19: a cell whose content changed is laid out again at
  its size, the squares, a usage bar and the count after the squares alike
  ([architecture §4.4](../developer/architecture.md#44-tables-details-editors-and-streams);
  `TestContainersCellReusedByAFilter`, `TestContainersCellReusedForMoreContainers`,
  `TestContainersCellRowsMove`, `TestUsageBarCellRowsMove`, renders
  `containers_reused_{dark,light}.png`). Not verified: that his pod had two containers (the chart's
  usual shape) and which pod the first row showed before the filter narrowed the list.
- **Found in the owner's walk (2026-10-19, v0.26.1), all built 2026-10-19; re-walked 2026-10-22
  where they touch the gate (WF31–WF33, WF35, WF38–WF40, passed), the others not re-walked:** WF31 a drawer's edited state ("Edited: 1 value.") was easy to miss: a coloured pill per
  count beside Revert and Save ([D-100](../developer/decisions.md#d-100-objects-and-effects-in-one-form-and-a-drawers-edited-state-beside-save)); WF32 a
  confirmation's Object(s) row did not set the kind apart from the name: "Config Map → walk-cm",
  kind muted, name bold, in every dialog (D-100); WF33 the Effect row was a sentence ("Removes key
  added."): a row per effect, "Removes  added" bold and red (D-100); WF34 the alert board's copy
  icon showed late and flakily, at the card's far right: the board rebuilt every card at each
  answer, so a head under a resting pointer got no hover; now nothing is rebuilt for the same
  answer, a rebuilt board keeps the hovered card's icon, the icon after the name
  ([D-94](../developer/decisions.md#d-94-the-empty-docks-toggle-opens-a-terminal-and-alert-names-copy)); WF35 a followed log's resume marker after a
  sleep ("interrupted 11:10–11:10"): the gap from the last line that arrived to the reconnect, with
  seconds, the zone and its length, Cogpit's own lines in the warning colour
  ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections) S7); WF36 a log's text could not be selected or copied: a selection across
  the lines, ⌘A, ⌘C and a right-click's Copy ([D-99](../developer/decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)); WF37 a succeeded
  Reconnect's entry read as ending in "Connection interrupted": the interruption an entry of its
  own, the reconnect ending in its success, one way to write a duration, the user's own cancel and
  removal INFO ([D-66](../developer/decisions.md#d-66-the-connection-log)); WF39 showing another cluster ended a connect that waited for its
  login without a word: the connect goes on in the background, a toast at its end; **WF40 a failed
  connect showed the cluster shown before again** (a safety matter): the window stays on the failed
  cluster with Connect Again and Show Connection Updates ([D-97](../developer/decisions.md#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays)); WF41 a
  toast's text could not be selected or copied: selectable, a copy icon left of its ×, the bell's
  list the same (D-99); WF43 a Terminating namespace: the drawer's Blocking Deletion list through
  metadata-only watches in the namespace, kinds the user may not list counted, Remove finalizers a
  JSON patch testing the UID and the confirmed finalizers, never on a namespace or a custom
  resource definition ([D-98](../developer/decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)); WF44 the kubeconfig sources dialog
  had no button in the navigator: a "−" beside Local Kubeconfigs; WF45 a removed cluster's hotbar
  tile stayed: removing a source or Remove from Cogpit takes it off, a file gone by itself keeps it
  marked missing ([D-101](../developer/decisions.md#d-101-the-kubeconfig-sources-at-hand-and-a-removed-cluster-leaves-the-hotbar)). WF42 the disk image's name pills sat too low and Cogpit's was too narrow for "Cogpit.app":
  Finder's label measured in a ruler disk image on the owner's Mac (76–87 pt under the icon's centre,
  black in both modes), the pills centred on it and 102 pt wide, chosen by the owner in the DMG
  Lab ([D-70](../developer/decisions.md#d-70-the-disk-images-window)). Estimate not moved.
- **Found in the same walk (2026-10-19), WF38 (the lead's, W44):** a credential plugin that is a
  wrapper and `exec`s was left running after Cancel Connecting while the log said Cogpit ended it,
  and connects cancelled behind it started new runs later. Built 2026-10-19
  ([D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound)): the
  plugin's process is found at the run's begin and ended by its number and start time, a wrapper
  that `exec`s included; the log and the error say "Cogpit ended" only then, otherwise "stopped
  waiting … may run on"; a request of a cancelled connect starts no run
  (`TestCancelEndsAPluginThatExecs`, `TestCancelledConnectsStartNoRunAfterAStrayOne`). What stays:
  [H-118](../security/credentials.md#h-118-what-a-cancelled-login-leaves-running).
- **Found by the owner (2026-10-18 evening), acme-mgmt through Pinniped:** Lens showed the cluster's
  Prometheus, Cogpit did not. Cause (verified on the cluster, read only): the user's role grants the
  service proxy only for `prometheus-operated:9090` and `kube-prometheus-prometheus:9090`; Cogpit
  asked `…:http-web` and stopped at the first 403. Fixed 2026-10-18
  ([D-96](../developer/decisions.md#d-96-the-service-proxys-port-spelled-by-its-number-first)): the
  port's number first, then its name, a refusal no longer ends the search, for Prometheus and
  Alertmanager (`TestDetectTriesThePortsNumberThenName`).
- **Found in the owner's walk (2026-10-18), WF15:** the editor's YAML was sorted alphabetically
  (`apiVersion`, `data`, `kind`, `metadata` with `creationTimestamp` before `name`). Built 2026-10-18
  ([D-86](../developer/decisions.md#d-86-the-order-of-an-objects-yaml)): apiVersion, kind, metadata
  (name, namespace first), the body, status last, wherever Cogpit shows an object's YAML, the quoting
  unchanged (`TestObjectYAMLOrder`, `TestObjectYAMLRoundTrip`).
- **Found in the owner's walk (2026-10-18), WF13, WF14, WF16, WF19:** the confirmation said its
  rows again in a sentence and named the place Objects, Namespace, Context; the save review
  explained what a removed key does to workloads; the drawers and the Edit tab showed the raw
  `resourceVersion` ("based on version 805827"); the CSV's name carried the kubeconfig file
  (`configmaps-kind-cogpit__walk-kind.yaml_.csv`). Built 2026-10-18
  ([D-85](../developer/decisions.md#d-85-dialogs-as-tables-and-an-objects-state-without-its-resourceversion),
  the rule in [conventions](../developer/conventions.md#dialogs-reviews-and-status-lines)): rows
  Context, Namespace, Objects, Effect, Requested by and no sentence; the review's head as rows with
  "This cannot be undone." only; "since your edits began", "its current state" and times instead of
  the number; Overwrite; `configmaps-kind-cogpit.csv` (`TestSelectionMenu`,
  `TestCSVFilenameNamesTheCluster`).
- **Found in the owner's walk (2026-10-18), WF20:** kind showed Config Maps filtered on
  `walk-test`; switched to the stage cluster by its tile, the stage's views came with the same
  filter: "Es sollen keine Filter von Cluster zu Cluster übernommen werden." Cause (verified with a
  test that failed before the fix): the switch (a connect, showing a connected cluster) cleared the
  search and the selection but kept the namespaces, groups, status filter and search toggles, and a
  cluster without a tab of its own took over the one preview tab with them. Fixed 2026-10-18: no
  filter crosses a switch; a cluster's own tab brings its filters back, else it starts with none
  ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) stage 1;
  `TestNoFilterCrossesAClusterSwitch`).
- **Found in the owner's walk (2026-10-17), WF9:** "Die Funktion cmd+z funktioniert nicht. Ich
  [kann] versehentliche Vorgänge nicht rückgängig machen wie ich es gewohnt bin." Cause (verified):
  Cogpit's YAML editor had no undo; Fyne's driver hands ⌘Z, ⌘Y and ⇧⌘Z to the focused editor,
  which dropped them, and the Edit tab's Undo Revert undoes a Revert only. Built the same evening
  ([D-84](../developer/decisions.md#d-84-undo-and-redo-in-the-yaml-editor), the lead's decisions,
  open to revision): an undo and redo history in every YAML editor (the Edit, Create resource and
  Helm tabs, a Config Map's and a Secret's values in the drawer), ⌘Z, ⇧⌘Z and ⌘Y, a run of typing
  one step until a pause of a second, a caret move or a new line, a paste, a cut, a deleted
  selection and an indentation a step each, the caret and the selection restored, the newest 1,000
  steps and 32 MiB; a text set from outside clears it, ⌘Z after a Revert is Undo Revert; a Secret's
  editors drop theirs when the drawer moves away (`TestUndoRedoRoundTrip`,
  `TestEditTabUndoShortcuts`, `TestSecretDrawerKeepsUnsavedInMemory`;
  [ui-parity › Editor](ui-parity.md#editor-and-create-resource)). Fyne's own text fields
  (`widget.Entry`) undo with ⌘Z and redo with ⌘Y already, ⇧⌘Z not (Fyne's; left, D-84). No Edit
  menu (Cogpit's main menu has none, and Fyne adds none). Not tried in a real window. Left open:
  the drawer's Revert of a Config Map's or Secret's values has no undo of its own.
- **Found in the owner's walk (2026-10-17), WF8:** "Ich habe den Text markiert und außerhalb vom
  Editor losgelassen, ich konnte den Text dann mit backspace nicht mehr löschen, ich musste ihn neu
  markieren und durfte beim Loslassen des Drag&Drop nicht außerhalb des Editors landen." Seen in the
  Create tab, and again in a Config Map's value editor in the drawer. Cause (verified by reading Fyne
  2.8.2's desktop driver, `internal/driver/glfw/window.go`, `processMouseClicked`, and reproduced
  with its order of calls on Fyne's test driver): the driver runs its "a click outside the focused
  widget unfocuses it" rule on the button's release as well as on its press, so a drag that began in
  the editor and ended over another widget unfocused the editor; the selection stayed, the keyboard
  went nowhere. Fixed the same day in Cogpit's YAML editor, one fix for the Edit, Create resource and
  Helm tabs and a Config Map's and a Secret's values in the drawer: the editor takes the focus back
  when its drag ends, a selection's or a scroll bar's (`yamlview.View.DragEnd`;
  `TestDragReleasedOutsideKeepsTheKeyboard`, `TestScrollBarDragReleasedOutsideKeepsTheKeyboard`,
  which fail without the fix; [ui-parity › Editor](ui-parity.md#editor-and-create-resource)). Not
  tried in a real window. **The lead's decisions (2026-10-17 evening), open to revision:** *where the
  fix sits*: a patch of Fyne's driver (Cogpit carries no Fyne fork, only the terminal's, and a
  patched driver would have to follow every Fyne release), a container around the editor that
  swallows the release (the driver delivers it to the object under the pointer, not to the editor's
  parent), or the editor taking the focus back when the drag ends: the last, a few lines in the
  editor, and a press in the editor always focuses it, so it only undoes the release's unfocus.
  *Fyne's own text fields* (`widget.Entry`: the search fields, the namespace filter's search,
  dialogs' and Preferences' fields, a drawer's new key name) behave the same, verified with the same
  replay: not fixed, because each would need a wrapper type of its own or a patched Fyne, and their
  text is one line, where a drag that ends outside the field is rare; an upstream report to Fyne is
  the owner's call.
- **Found in the owner's real window (2026-10-17), F28 and F29:** "Nach einem Verbindungsabbruch zu
  einem Cluster aufgrund eines abgelaufenen Zertifikates, kann kein Reconnect erfolgen. Das
  Zertifikat auf Dateiebene ist bereits ausgetausch, auch ein Login Again führt nicht zum
  gewünschten Ergebniss. Außerdem fehlt mir ein Button in der Clusterübersicht, für einen
  Reconnect, […] ohne die Tabs zu schließen. Die Tabs werden nach dem neuaufbau aktualisiert." A
  context that logs in with a client certificate in its kubeconfig: once the certificate expired,
  the connection stayed "Login expired" although the file held the renewed one, and Log in again
  was rejected again. Cause (F28): the connection keeps the certificate it read at its connect, and
  Log in again only let a credential plugin renew the login on the same connection, which this
  context has none of. Fixed the same day
  ([D-81](../developer/decisions.md#d-81-reconnect-and-what-log-in-again-does),
  [G2](#g2-cluster-connection-and-authentication)): Reconnect (F29) builds a new connection from the
  file as it is now, the tabs kept, from a ↻ on each connected cluster in the list of all clusters,
  above the cluster Overview, in the cluster menus, the banner, the connection log and the palette;
  Log in again reconnects such a context; and a renewed file reconnects a rejected login by itself
  (`TestLogInAgainUsesTheRenewedCertificate`, `TestARenewedCertificateReconnectsByItself`,
  `TestKindReconnectWithARenewedCertificate`; renders `reconnect_section_*.png`,
  `reconnect_banner_*.png`; [beyond Lens › Standby and lost connections](beyond-lens.md#standby-and-lost-connections)).
  The lead's decisions, open to revision. Not tried in the owner's window.
- **Found in the owner's real window (2026-10-17), F25:** a cluster with an uploaded icon (Cluster
  Settings › General › Icon) showed the image in the navigator, but its tab in the tab bar
  ("Pods - acme-main") showed the initials badge "AM" ("Eine gesetztes Icon am Cluster muss auch in
  den Tabs verwendet werden."). Fixed the same day: a tab shows its cluster's icon as the navigator
  and the hotbar do, the uploaded image, else the initials badge in the icon colour, read from the
  same identity (`shell.identity`) and redrawn when the image arrives after the tab drew the
  initials; faded while its cluster is not connected, as the initials are muted then (the lead's
  decision, open to revision) (`TestTabShowsTheClustersImage`, renders
  [dark](../../internal/ui/fyneui/testdata/tabbar_cluster_icons_dark.png) and
  [light](../../internal/ui/fyneui/testdata/tabbar_cluster_icons_light.png);
  [ui-parity › Layout](ui-parity.md#layout), [Dock](ui-parity.md#dock)). Not tried in a real window.
- **The owner's requirement (2026-10-17), F26:** "Zusätzlich zum Icon braucht ein jeder Cluster
  auch eine Akzentfarbe, die man konfigurieren kann, damit wollen wir später noch verschiedene Teile
  der GUI markieren um während der Arbeit schnell sichtbar zu machen ob wir uns in einer Dev oder
  Prod Umgebung aufhalten. Um zu vermeiden, dass ein User aus versehen Aktionen in einer Prod
  umgebung durchführt, die er dort eigentlich nicht machen sollte, weil er unaufmerksam ist, und
  annimmt in einer Dev umgebung zu sein. Wo wir die Akzentfarbe einsetzen, bleibt erstmal offen."
  The setting built the same day
  ([D-80](../developer/decisions.md#d-80-a-clusters-accent-colour)): a row "Accent colour" on Cluster
  Settings › General (None, the icon colour's eight and a free colour), kept as `accentColor`, read
  by agents, set and cleared only in the window (`cluster.settings.setAccentColor` is GUI-only).
  Where the window shows it is open, so it marks nothing yet
  ([H-125](../security/agents-and-mcp.md#h-125-until-the-accent-colour-shows-a-clusters-name-and-icon-colour-are-agents-to-change);
  [beyond Lens › Everyday use](beyond-lens.md#everyday-use), partial).
- **Found in the owner's real window (2026-10-17), F27:** "Clustername und Pfad zur kube-config muss
  kopierbar sein." (Cluster Settings › General: the CLUSTER NAME field, "kubernetes-admin@acme-main",
  and the KUBECONFIG path with its folder button.) Built the same day
  ([D-25](../developer/decisions.md#d-25-copying-values)): both have the drawer's and the lists' copy
  icon, always shown, with its checkmark and toast; the name's copies what the field shows (the name
  typed, else the context's name it shows as its placeholder), the path's the whole path, and a
  context without a file of its own has none by its sentence (the lead's decisions, open to
  revision; `TestClusterSettingsCopyNameAndPath`, `TestClusterSettingsNoPathNoCopy`;
  [ui-parity › Cluster settings](ui-parity.md#cluster-settings)). Not tried in a real window.
- **Found in the owner's real window (2026-10-17), F23 and F24:** in the Pods list a running
  container that was not ready showed in the Running green (F23: "Container die nicht ready sind,
  sollen nicht grün sondern orange sein"), and a pod with more containers than the Containers
  column is wide showed only the first squares, so a Cilium pod's five finished init containers
  looked like a pod whose containers had all ended (F24: "Aus der Darstellung muss klar werden,
  dass nicht alle Container angezeigt werden"). Fixed the same day: a running container that is
  not ready is orange (a plain init container, never ready while it runs, stays green; a sidecar
  turns orange), and a narrow cell shows the squares that fit and "+N" for the others, coloured by
  their gravest state (`TestContainerMarks`, `TestContainersCellCountsWhatDoesNotFit`, renders dark
  and light; [resource-parity › Pods](resource-parity.md#workloads)). The lead's decisions, open to
  revision.
- **Found in the owner's use (2026-10-17), F22:** a context that logs in with a client certificate
  in its kubeconfig gave no hint how long the certificate was valid, nor that it had expired; the
  owner asked for the time left or "expired" in the cluster picker, without blocking a connect
  ("auch wenn er fehlschlagen wird"). Built the same day
  ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists),
  [G13](#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences)); checked against the kind
  cluster's certificate with `openssl`. The lead's decisions, open to revision.
- **Found in the owner's real window (2026-10-17), F20:** on a Gardener cluster of STACKIT every node
  showed the warning icon and a red "SystemdUnitsReady" in its conditions, although the condition
  was True, its healthy state: Cogpit read every condition but Ready as a problem when True, the
  rule of the kubelet's pressures and node-problem-detector's conditions. Fixed the same morning:
  a condition whose type ends in Ready reads True as healthy and False or Unknown as the problem
  ("SystemdUnitsReady: False"); the list shows only the problems, the drawer's chips also the
  healthy ones in green; the Overview's Issues date a node's problem by the same rule
  (`TestNodeWarningsAndConditions`, `TestNodeConditionProblem`). The rule is Cogpit's, inferred
  (the API does not say which way a condition reads); the lead's decision, open to revision.
- **Found in the owner's smoke test (2026-10-12):**
  - **Every quit on macOS crashed** (window closed, Cmd+Q or SIGTERM): the shell stopped Cogpit's
    title bar after the app's run ended by asking GLFW for the window, which GLFW had terminated
    ("NotInitialized"). Fixed (f480186, `TestTitlebarStopAfterTheRunEnded`, which fails on the old
    code); checked on a real launch ended by SIGTERM and by the owner closing the window: exit 0, no
    crash report. Crash reports are kept since 7ac22e0
    ([D-11](../developer/decisions.md#d-11-crash-stance)).
- **Found in the owner's real-window walk (2026-10-14 evening):** no node history from Prometheus
  ([G12](#g12-metrics-and-cluster-overview), built 2026-10-14 night); the About dialog's version 0.0.1 and its layout
  ([G13](#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences); fixed 2026-10-14). Both were the next
  implementation's first items; both are done.
- **Found in the owner's real window (2026-10-15), F3:** in a Config Map's drawer (`cilium-config` of a real
  cluster) long keys without spaces ran under their values. Decided and built the same day
  ([D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)): the key on a line of
  its own and the value below in an editor, as Lens's ConfigMap drawer shows them, the values saved in
  the drawer through `configMap.saveData` ([G9](#g9-yaml-editor-and-create-resource)); names without
  spaces wrap at - . _ / in the name column ([G5](#g5-app-shell-navigation-tables-and-drawer)); a
  Secret's Data keys above their values. A Secret's values edited in the drawer, in the window only,
  decided by the owner in the owner's audit answers of 2026-10-15, built the same night
  ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer), [G9](#g9-yaml-editor-and-create-resource));
  the lead's decisions D-55 and D-59 list wait for the owner.
- **Found on the owner's real cluster (2026-10-15 evening;
  [roadmap › Current focus](roadmap.md#current-focus) item 2):** after the Mac slept every connection
  was gone and the window hung on waking, nothing detected or reconnected (decided in an interview
  the same evening, not built: [G2](#g2-cluster-connection-and-authentication)); in the owner's log,
  `services` alone answered 401 every 5 minutes for two hours while other kinds worked, cause not
  known.
- **Found in the owner's real window (2026-10-17), F21:** the namespace filter's list could not be
  searched by typing, and a click on a namespace only added it to the others. Built the same day
  ([G5](#g5-app-shell-navigation-tables-and-drawer)): a search field on top of the list, focused when
  it opens, narrowing it as the user types (↑↓, ↵, Space, Escape); a click on a name filters to that
  namespace alone and closes the list, a click on its box adds or removes it as before
  (`TestNamespaceFilterTypeToFilter`, `TestNamespaceFilterKeys`,
  `TestNamespaceFilterClickOnNameSelectsOnly`, `TestNamespaceFilterCheckboxToggles`,
  `TestNamespaceFilterKeepsTextWhenNamesArrive`, `TestSearchFieldsHandFKeysToTheHotbar`; renders
  `namespace_filter_search_dark.png` and `_light.png`). Not tried in a real window yet.
- **Missing:** PhotoCraft's first public week produced hundreds of issues on features its tests
  counted as done; this entry reserves the time for the same after Cogpit's first release.
- **Estimate:** 100–200 h to beta. Checked 2026-10-15, not moved; checked again 2026-10-22 after the
  alpha walk's findings AW1–AW19 were built (about 25 agent-hours, inside this reservation), not
  moved, and 2026-10-23 after their re-walk and its findings RW1–RW4 (small, inside it), not moved, and after
  RW1–RW4's re-walk and its findings CW1–CW4 the same day (small, inside it), not moved: what testers besides the owner find is still ahead. The hang after sleep is counted in G2,
  the 401s and what the owner's walks find in this reservation.

## Beyond Lens

### G17. MCP control
- **Built 2026-10-14 (A5, A3, A1, A2):** the replaced Secret values' minimum length 10 bytes
  instead of 8 ([H-82](../security/agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents));
  a terminal tab's agent switch ends at the latest one hour after it was turned on, whatever the
  reads, the tab saying so
  ([H-83](../security/agents-and-mcp.md#h-83-an-allowed-terminal-tab-gives-agents-everything-it-shows-from-then-on));
  agents' RBAC writes and Helm installs and upgrades always waiting for the user; window-only
  notifications reaching agents as text through that filter, inline Helm values removed, instead
  of withheld; marking notifications read and clearing them the window's only; a chart's README
  framed as text of the chart's author; `cluster.setPrometheus` by an agent always waiting for the
  user (A16).
- **Built 2026-10-14 (A15, D-49):** the MCP switch persisted and honoured by `cogpit-cli mcp`,
  which with MCP off refuses to serve, headless too, telling the agent why
  ([H-28](../security/agents-and-mcp.md#h-28-the-status-bars-switch-does-not-reach-a-headless-bridge)).
- **Built 2026-10-21 (D-49, the owner's security posture):** the external MCP server is opt-in: off
  on a new installation and for `cogpit-cli mcp` until the user switches it on, a stored "on" or the
  preference `mcp.enabled` stored true kept; no gap moves.
- **Confirmed (owner, 2026-10-14 evening)** ([G7](#g7-resource-actions), [G11](#g11-helm),
  [G12](#g12-metrics-and-cluster-overview), [G13](#g13-clusters-hotbar-palette-menus-shortcuts-and-preferences)): values under
  naming keys (user, host, port, database, namespace) not replaced; GUI-only commands listed,
  described and refused; `cogpit-cli get` printing Cogpit's view of a list and one object's manifest
  redacted. They confirm what is built; the estimate below is checked against them (2026-10-15).
- **Closed 2026-10-16 night (the lead, checked against what came since 2026-10-15):** every
  command that reads or acts on a cluster names its cluster to agents (D-69): a `context`
  parameter on the 17 that name no object, else the object's reference, the dock tab or `all`
  (the journals of every context), the window's `ui.*` commands on the shown one and
  `table.exportCsv` on the shown view by design; `TestEveryClusterCommandNamesItsCluster`
  ([`target_every_test.go`](../../internal/engine/commands/target_every_test.go)) fails a command
  added without one. The connection log (D-66) is readable by agents (`cluster_connectLog`, a
  read, D-66 item 5), its scrubbed credential plugin's standard error with it, as
  [H-112](../security/credentials.md#h-112-a-credential-plugins-standard-error-in-the-connection-log)
  says; the logged-in user is `ui_state`'s `user` (D-68,
  [H-113](../security/agents-and-mcp.md#h-113-the-users-name-and-groups-reach-every-agent-that-reads-ui_state));
  the drawer's key commands are tools for a Config Map (`configMap_readData`, `configMap_reviewData`,
  `configMap_saveData`; `configMap_keepEdits` GUI-only, D-55) and GUI-only for a Secret
  (`secret_reveal`, `secret_reviewData`, `secret_saveData`, D-59); Restore the last session
  (`preferences_set`'s `restoreSession`) and the per-cluster login renewal
  (`cluster_settings_set`'s `renewLoginInBackground`) are tools agents may only turn off (D-69
  stage 3); `TestEveryCommandIsATool` passes over the 202 commands. M9's measure is met: an
  external agent completes the triage and change workflows over MCP on kind, every mutating step
  confirmed with the object, its namespace and its context, [`TestKindAgentTriageAndChangeOverMCP`](../../internal/testkit/integration/mcpworkflows_kind_test.go) (in `cogpit-mcpflow`: a
  crashing pod's status in `resources_list`, its drawer through `ui_navigate`, its containers,
  conditions and events from `object_get`, the previous container's log through `dock_read`, a
  shell opened after the user's acceptance and withheld from the agent until the user's switch,
  an edit applied after a dry run, a Deployment scaled and restarted, a pod deleted; the
  window's part of `dock_read` stood in by a reader over `Session.OpenLogs`, as the logs view
  streams). Not done in a real window by a person: the owner's walk.
- **Missing (2026-10-16 night):** nothing. Until then (2026-10-15): none of the 17 MCP rows of
  [beyond-lens](beyond-lens.md#mcp-control-of-ui-and-cluster), all done since 2026-10-13: the MCP
  server over the registry with the UI tools, Streamable HTTP with a bearer token, the stdio bridge,
  the GUI side of the confirmation flow, the audit log view
  ([D-2](../developer/decisions.md#d-2-agent-interface-scope); Evidence). The actions later gaps add
  become tools on registration (`TestEveryCommandIsATool`).
- **Evidence:** beyond-lens, all 17 MCP rows done (2026-10-13; was 14 done and 3 partial): tools from the registry,
  Streamable HTTP on loopback with a bearer token and `Host`/`Origin` checks, `ui_state`,
  `resources_find`, `ui_navigate` and the other UI tools, the GUI confirmation for agents, the audit
  log file ([`internal/mcp`](../../internal/mcp/), `TestShowcaseServiceByClusterIP`); since
  2026-10-13 the stdio bridge `cogpit-cli mcp`, so a registration survives restarts (it forwards to
  the running app and follows its restarts, else serves a headless engine that refuses what needs a
  confirmation; [`bridge.go`](../../internal/mcp/bridge.go), `TestBridgeFollowsARestart`,
  `TestMCPHeadlessServesTheRegistry`); since 2026-10-13 `view_rows`, the rows the user sees in the
  active tab's view from its snapshot ([`viewrows.go`](../../internal/engine/commands/viewrows.go),
  `TestViewRowsAreTheTablesRows`), and `object_get`'s related objects (`related: true`: the
  drawer's links and the objects a workload controls, from the cache;
  [`relatedrefs.go`](../../internal/engine/relatedrefs.go), `TestObjectGetRelated`), and the
  per-context allowance of agent writes in Cluster Settings › Agents, a setting only the user
  changes ([`allowance.go`](../../internal/engine/allowance.go), `TestAgentWritesAllowance`,
  `TestClusterSettingsAgentsSection`), and the audit log read back newest first in a dock tab
  (Help › Audit Log) and by agents (`audit_list`; [`auditlist.go`](../../internal/engine/auditlist.go),
  `TestAuditLogTab`); since 2026-10-13 (night build) every command a tool checked over Cogpit's
  whole registry, and GUI-only commands under one mark the engine enforces, refused to agents and
  the CLI with their reason before anything is asked or read
  ([`guionly.go`](../../internal/engine/guionly.go), `TestEveryCommandIsATool`,
  `TestGUIOnlyCommandsRefuseOtherOrigins`); since 2026-10-13 (night build) `dock_read`, what a logs
  tab shows freely and a terminal tab's screen once the user turned on its switch
  ([D-44](../developer/decisions.md#d-44-what-agents-read-of-the-docks-logs-and-terminal-tabs),
  [`dockoutput.go`](../../internal/engine/dockoutput.go), `TestDockReadTerminalNeedsTheUsersSwitch`,
  `TestDockOutputOfALogsTab`), and `kinds_list` completed: each kind's actions, selection actions
  and other tools, whether Cogpit's cache holds it, and with `all` the built-in kinds the cluster
  does not serve ([`kindslist.go`](../../internal/engine/commands/kindslist.go),
  `TestKindsListActionsCacheAndServed`). Nothing of the rows is missing.
- **Estimate:** 0 h, closed 2026-10-16 night (was 0–2 h; 4–12 before 2026-10-13, night build, after `dock_read`, `kinds_list` and the check of every command over the whole registry): the mitigation [H-83](../security/agents-and-mcp.md#h-83-an-allowed-terminal-tab-gives-agents-everything-it-shows-from-then-on) named, an expiry of the terminal switch, is built (2026-10-14, A3: off 15 minutes after the last read, at the latest an hour after it went on; `TestAgentReadSwitchEndsAfterAnHour`), so nothing of the rows is left (checked 2026-10-15); D-44 is decided (owner, 2026-10-14), and the scrub [H-82](../security/agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents) named is built (2026-10-14: the cached Secrets' values replaced by `«secret <ns>/<name>/<key>»` in what agents read, with the lead's choices, open to revision in D-44). The actions G7 adds later become tools on registration, checked by `TestEveryCommandIsATool`.

### G21. Built-in agent
- **Required (owner, 2026-10-12, later, after the alpha):** a VS Code-like chat in Cogpit (as in
  Claude Code for VS Code), switchable on and off; an OpenAI-compatible endpoint (Open WebUI,
  LiteLLM) so that local models work; the agent gets Cogpit's tools to operate the UI; it keeps
  working while the external MCP server is switched off (in-process dispatch, D-10).
- **Built (2026-10-14 late evening, the engine side, M12 (a)):** `internal/agent` implements
  `engine.ChatAgent`: connections with their keys in the macOS keychain (an owner-only file since
  2026-10-15, below), the OpenAI-compatible
  client (LM Studio, LiteLLM, Open WebUI, others; plain `net/http`), the connection test by layer,
  the turn loop over 20 core commands and two meta-tools with origin `builtInAgent`, the owner's
  limits and Stop, conversations kept scrubbed; checked against the owner's LM Studio with
  `qwen/qwen3.8-27b` ([D-10](../developer/decisions.md#d-10-built-in-agent), with the lead's
  decisions of the night run, open to revision).
- **Built 2026-10-14 night, the window's half (M12 (b)), against the engine's contract
  (`engine.ChatAgent`) and a scripted agent:** the chat column at the right edge (⌘I, its width
  kept, beside a settings page too), the conversation drawn as the owner's design direction has it
  (the pinned user's card with its chip, Markdown answers with tables, inline code chips and links,
  tool rows in every state, notes, what each turn sent and its request body), streaming that lays
  out an answer's last block only, the input (Enter, Shift+Enter, the view and selection's chip,
  Stop by the button, Escape and ⌘.), the connection and model under it, the cluster's agent
  writes setting, and Preferences › Agent with the connection test by layer
  ([`fyneui/chat.go`](../../internal/ui/fyneui/chat.go),
  [`fyneui/preferences_agent.go`](../../internal/ui/fyneui/preferences_agent.go)); the lead's
  decisions, open to revision are in [D-10](../developer/decisions.md#d-10-built-in-agent).
- **Built (2026-10-14 night, M12 (c)):** the private connection's reads like the user's:
  `Invocation.ReadsLikeUser` honoured by the engine for the built-in agent only, every read path
  that withheld from agents asks it, no write rule does, the audit lines marked
  ([agents and MCP › H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows),
  six decisions of the lead, open to revision in D-10).
- **Built 2026-10-15 (the owner's decision of the same day, H-96):** a conversation in which a turn
  ran over a private connection is not kept: memory only, gone when Cogpit quits, from that turn's
  start and for good (also after a switch to a connection not marked private); a file of v0.8.0 to
  v0.10.0 is rewritten without such conversations at the next start; a hint under Preferences ›
  Agent's private mark and "not saved" in the chat's history
  ([agents and MCP › H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them),
  the rule in [`agent/conversation.go`](../../internal/agent/conversation.go)).
- **Built 2026-10-15 (the owner's decision of the same day: no keychain, "übertrieben, erzeugt
  Reibung", each self-built binary made macOS ask for the login password):** the API keys in
  `llm-keys.json` beside the connections, owner-only, on every platform; the keychain code removed;
  a key kept in the keychain before is not read and is entered once again
  ([D-10](../developer/decisions.md#d-10-built-in-agent) item 12,
  [credentials › H-99](../security/credentials.md#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder),
  [`agent/keystore.go`](../../internal/agent/keystore.go)). Windows and Linux keep keys too, which
  was this gap's keychain item.
- **Missing (2026-10-15):** a run against a real LiteLLM and Open WebUI; the window and the agent tried together
  in a real window (both are wired: `cmd/cogpit/chat.go`; tested apart, renders against a scripted
  agent).
- **Built 2026-10-15 (night run, the owner's audit answer (4)):** "send all tools" per connection,
  off by default: every command the chat may run as a tool of its own (179 tools, about 56,000
  tokens a request, measured), for large cloud models; the same rules for every tool
  ([D-10](../developer/decisions.md#d-10-built-in-agent), `TestAllToolsSendsEveryCommand`).
- **Built 2026-10-15 (night run, the owner's audit answer (6)):** plain `http://` to another
  machine warned about in Preferences › Agent and the connection test
  ([H-95](../security/credentials.md#h-95-an-llm-api-key-over-plain-http)); a CA file per
  connection for an `https://` server with a private CA, replacing the system's roots for that
  connection since the owner's decision in the audit of 2026-10-16 (N141; the night run added it to
  them) ([H-104](../security/trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)).
- **Built 2026-10-15 (night run, the owner's F9):** every message of the user's edited and sent
  again, everything after it dropped from the window, the model's history and the kept file (the
  audit log keeps it); a failed or stopped turn retried the same way
  ([agents and MCP › H-100](../security/agents-and-mcp.md#h-100-a-cut-parts-tool-calls-leave-the-models-view)).
- **Evidence:** beyond-lens, 10 of 10 rows done (2026-10-15).
- **Estimate:** 2–6 h, re-estimated 2026-10-15 night (the lead's estimate, PhotoCraft's
  calibration): the live runs against LiteLLM and Open WebUI and the real window; the owner's audit
  answers (send all tools, the `http://` warning and the CA file, F9) built in the night run. Was
  6–18 h with them (was 3–10 h, was 5–15 h). **Human:** a LiteLLM virtual key for CI; an
  Open WebUI instance to test against.

## Ecosystem gaps

### G18. Paid Lens Desktop features
- **Missing:** Security Center (Trivy Operator; since 2026-10-17 Cogpit reads its reports for a
  pod's image ratings, D-78, the base for a page of images, not the page); cost monitoring. Flux CD and Argo CD are G23, in the beta; the Hotbar and the editor's
  diff are built with G13 and G9; Lens's Ask AI and MCP server are superseded by G21 and G17
  ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)). The EKS, AKS
  and GKE account integrations are not a target (owner, 2026-10-22: won't build, D-9).
- **Evidence:** [resource-parity](resource-parity.md#paid-lens-desktop-views), Security Center and
  Cost Monitoring.
  Kyverno's policy violations on the objects (2026-10-17, D-79) are not part of it: Security
  Center reads Trivy Operator's reports, and the Kyverno integration is Cogpit's own
  ([beyond Lens › Integrations](beyond-lens.md#integrations)).
- **Impact:** high for users of Lens's paid plans, none for the mainstream audience.
- **Estimate:** 110–220 h, estimated with the EKS, AKS and GKE account integrations in it; not
  re-estimated since they are not a target (2026-10-22), the entry gives no share for them.
  **Human:** a cost data source (owner decision).

### G19. External extensions
- **Missing:** a way to install extensions from outside the project, as Lens does with npm
  packages.
- **Impact:** not a target ([D-5](../developer/decisions.md#d-5-extensions)): integrations are built
  into Cogpit's code; Lens's JavaScript extensions could not run in Cogpit anyway.
- **Estimate:** not counted.

### G20. Teamwork and remote access
- **Missing:** Lens's Teamwork spaces and Cluster Connect relay.
- **Impact:** not a target unless the owner decides to run a relay service
  ([D-9](../developer/decisions.md#d-9-scope-against-lenss-paid-and-vendor-features)).
- **Estimate:** 150–300 h plus a hosted service; not counted in the totals.

## Revision history

| Date | Change | Summary |
|---|---|---|
| 2026-10-23 | patch | G16: the owner's finding of 2026-10-23 (a view entered drew no row until a minimal scroll) reproduced offscreen and fixed on the branch `fix/list-empty-on-entry`: a view entered, the same view of another cluster too, starts at its top; no estimate moves |
| 2026-10-23 | patch | G16: the owner's bug of 2026-10-23 evening (another cluster's tab selected took the one preview tab over, a view lost) fixed on the branch `fix/preview-tab-per-cluster`: a tab switch changes no other tab, one preview tab per cluster (D-117); G5's inferred defaults without "one preview tab in all"; no estimate moves |
| 2026-10-23 | trivial | G16: a further cluster name of the owner's replaced by a placeholder, acme-d-pinniped; the repository is public |
| 2026-10-23 | minor | G16: the owner's re-walk of RW1–RW4 and the logo on v0.40.1 (2026-10-23, the staging cluster), each passed in a real window; his findings CW1 (Hide as wide as Show), CW2 (Hide green with a closed eye), CW3 (Undo one height), CW4 (the agent's Edit wider) open, not built; the estimate checked, not moved |
| 2026-10-23 | patch | G16: a connect that succeeded while its kubeconfig source was removed can leave the UI "connecting" (`settleGoneConnect` before the window's Connected), found in a CI race run, open |
| 2026-10-23 | patch | G16: a colleague's blurry navigator (the owner's question): the header's and the welcome page's logo drawn at the display's pixels, not the canvas scale's, built; Fyne's text textures kept across a change of density recorded as a candidate, open until the colleague's test |
| 2026-10-23 | minor | G16: RW1–RW4 built (PR #86, released in v0.40.1): a Secret key's buttons on its name line (RW1), the field's outline on every text button of the window (RW2), a Secret's Show in amber with an eye (RW3), the welcome page's message in a banner naming its level (RW4); the owner's re-walk open; the estimate checked, not moved |
| 2026-10-23 | trivial | G16: the owner's cluster names in the findings replaced by placeholders (acme-main, acme-stage, acme-p, acme-mgmt); the repository is public |
| 2026-10-23 | minor | G16: the owner's re-walk of AW1–AW19 and of the Secret copy on v0.40.0 (2026-10-23, the staging cluster), each passed in a real window, the bullets' "not seen in a real window yet" replaced; his findings RW1 (a masked key's buttons), RW2 (text buttons' outline), RW3 (a Secret's Show marked as exposing) and RW4 (the welcome page's message squeezed) recorded, open, the lead's proposals open to revision; the estimate checked, not moved |
| 2026-10-22 | minor | G16: the alpha walk's findings AW1–AW19 all built 2026-10-22 (PRs #74 to #82), the owner's re-walk open; the estimate checked after them, not moved (about 25 agent-hours inside the reservation) |
| 2026-10-22 | minor | G16: the alpha walk's focus findings built on the branch `fix/dock-tab-focus` (D-115): a dock tab the user opens or shows from the window takes the keys on its editor, terminal or log (AW5, AW8), an agent's leaves them; AW6's two causes, the keys left in the list and the find bar's first opening without a repaint in a real window, fixed; no estimate moves |
| 2026-10-22 | minor | G16: the alpha walk's AW1–AW4 and AW19 built on a branch: Escape closes the innermost thing (a dialog as its Cancel, the drawer behind untouched; Connection Updates; a list's search and the find bar; an open dropdown's list lets the next Escape go on), a closed dropdown outlined as an entry, every field's outline 2:1 on every surface (D-116, D-104) |
| 2026-10-22 | minor | G16, AW12's follow-up: the welcome page's cluster rows were a `fyne.DoubleTappable` too, their click held for the double-click interval; they connect at the release, and the page connects a cluster whose connect is under way no second time (D-106); no estimate moves |
| 2026-10-22 | minor | G16: AW12 built on a branch: a click below a list's rows closed the drawer only after the double-click interval, Fyne's driver holding every tap on the table, a `fyne.DoubleTappable` for its header separators' double click; the table counts that double click itself (D-106); no estimate moves |
| 2026-10-22 | minor | G16: AW13, AW17 and AW18 built on the branch `feat/failed-connect-page` (D-114): a failed connect's error page with the connect's stages, also in Connection Updates; the welcome page without "Other clusters"; the stray "Could not" at the navigator's edge reproduced and fixed; no estimate moves |
| 2026-10-22 | minor | G16: the owner's AW15 and AW16 of the alpha walk built (D-97): the user's own Cancel Connecting and a connect going on in the background raise no error toast on any path (a hotbar tile, a tab, the navigator, `cluster.connect`), a failed connect still does; agents read a cancel as `cancelled`; no estimate moves |
| 2026-10-22 | minor | G16: AW10 of the owner's alpha walk built on a branch: ⌘⌥⇧← and ⌘⌥⇧→ move the dock's active tab where the keyboard works in the dock (D-106); no estimate moves |
| 2026-10-22 | minor | G16: AW9 of the owner's alpha walk built on a branch: a command that shows a dock tab open already opens the minimised dock also when that tab is the active one (D-54); no estimate moves |
| 2026-10-22 | minor | G16: AW7 of the owner's alpha walk built on a branch: the dialogs' Context row draws the source label in the kind word's grey (D-85, D-114); no estimate moves |
| 2026-10-22 | minor | The cloud providers' credential plugins and cloud account integrations not a target (owner, 2026-10-22: won't build, D-9; out of scope until long after parity since 2026-10-15): G2's Missing without them, its Evidence 7 of 10 rows partial, its Human none; G18's Missing without the EKS, AKS and GKE account integrations, its Evidence without the platform checklist, its Human without cloud accounts; the navigator's cloud sections not a target in G5 and G13; no estimate moves (G2 counted no hours for the plugins, G18's 110–220 h gives no share for the integrations) |
| 2026-10-22 | patch | G16: AW14 built on a branch: Follow switched off by a scroll five rows up, on again at the newest line (D-99); no estimate moves |
| 2026-10-22 | patch | G16: AW11 built on a branch: the namespaces' own changes (the engine's `EventNamespaces`) read the filter's namespaces again, not the view's rows (D-106); no estimate moves |
| 2026-10-22 | minor | G16: the owner's alpha walk of 2026-10-22 on v0.38.0: the re-walk of every fix that touches the alpha gate passed; findings AW1–AW19 (Escape, focus of new dock tabs, connect outcomes as error toasts, and nine on their own), none blocking; AW7, AW13, AW14 and AW17 decided by him (D-114), not built; no estimate moves |
| 2026-10-22 | minor | G16: the owner's finding of 2026-10-22 on v0.37.3 fixed: a list narrowed while scrolled down ("2 of 200 items", no row drawn) brings its scroll offset back within its rows; no estimate moves |
| 2026-10-22 | minor | G16: the owner's findings of 2026-10-22 on v0.37.0 fixed: a view not read before an expired login or an interruption says so (no "Loading…", count, "syncing…" or download), the namespaces of the cluster shown before dropped at a switch, the drawer's header laid out after another pod; no estimate moves |
| 2026-10-22 | trivial | A10 made (2026-10-22): the tag ruleset, the App its only bypass, a probe tag refused; `release`'s admin bypass off; no number moves |
| 2026-10-22 | trivial | G14's Human: the appcast jobs in `sparkle` sign without the owner's approval since 2026-10-22 (his choice, H-141); no number moves |
| 2026-10-22 | trivial | G8: the terminal widget's upstream series no longer "prepared, not sent": offered as #155 to #158 since 2026-10-12, #157, #155 and #156 merged (2026-10-14 to 2026-10-17), #158's changes the maintainer asked for on 2026-10-19 pushed 2026-10-22, waiting for his review; no number moves |
| 2026-10-21 | minor | G22: Cogpit for Windows built on the branch `windows-release` (D-111): the NSIS setup, WinSparkle with the update key and the button's one click, the terminal on ConPTY, the release jobs, CI's Windows Package and every test on Windows; what is left on Windows (the first release, Authenticode, ARM64, winget and Scoop, links, Start at login, the system's dialogs, the region's format, the window's place, Enforce kubectl, `~\`, the proxy settings, lint on Windows-only files); re-estimated 20–45 h (was 30–65), the full Lens target ~360–900 h; G14's one-click update line, G1's Windows test jobs, G2's process lookup on Windows tried |
| 2026-10-21 | patch | The external MCP server is opt-in (D-49, the owner's security posture), noted beside A15's build in the 2026-10-14 decisions; no gap and no number moves |
| 2026-10-20 | minor | G22: Cogpit for Linux built (night, D-108): the deb for amd64 and arm64, Cogpit's signed APT repository on Pages, the updates through apt, the release jobs and CI's Linux Package; what is left on Linux (the first release, a desktop's look, links, Start at login, the other formats) and on Windows; re-estimated 30–65 h (was 40–80; the lead's estimate), the full Lens target ~370–920 h (368–920); G14: the one-click update partial for Windows, Linux through apt |
| 2026-10-20 | minor | G16: GW1–GW9 of the gate walk built (D-104, D-105, D-106), GW10 the alerts pill (D-77), GW11 the tab bar and Mixed environments (D-69), GW12 EtcdIsVoter healthy, from the owner's use the same day |
| 2026-10-20 | minor | G16: the owner's gate walk on the staging cluster, every workflow passed; findings GW1–GW9 recorded, not built; GW9 as the owner chose it |
| 2026-10-20 | patch | G8: the reopened log stream noted from reading on 2026-10-14 checked: real after an interruption (a Stateful Set's pod created again under its name, a restarted container: its lines twice once the cache showed it) and after a late cache; fixed, the pod's tab says where its log does not go on from its last line (D-58) |
| 2026-10-19 | minor | G16: the owner's report, every click on a pod only checked it as if ⌘ were held, fixed (a click takes ⌘ and ⇧ from its own mouse event, not from the driver's last key event) |
| 2026-10-19 | minor | G16: a colleague's report, one container square too many after the namespace filter, fixed (a list's cell with new content laid out again) |
| 2026-10-19 | minor | G16: WF42 built, the owner's pick in the DMG Lab (D-70); WF31–WF45 all built |
| 2026-10-19 | minor | G16: the owner's findings WF31–WF45 of 2026-10-19 built the same day but WF42 (D-94, D-58, D-66, D-97 to D-101), each with how; WF42 open for a visual review with the owner; no estimate moves |
| 2026-10-19 | minor | G16: WF36 and WF41 built (D-99): a log tab's text selected across its lines and copied, a toast's and a notification's text selectable with a copy icon; no estimate moves |
| 2026-10-19 | minor | G16: WF43 built (a Terminating namespace's drawer lists what holds it; Remove finalizers on one object), D-98; no estimate moves |
| 2026-10-19 | minor | G16: WF31 (the drawer's edited state as pills beside Revert and Save), WF32 (an object named "Config Map → walk-cm" in every dialog) and WF33 (a confirmation's effects a row each) built, D-100; no estimate moves |
| 2026-10-19 | minor | G16: the owner's walk of 2026-10-19 on v0.26.1, his findings WF31–WF45, not built yet; no estimate moves |
| 2026-10-18 | minor | G16: the owner's acme-mgmt finding, Prometheus refused under its port's name, fixed (D-96) |
| 2026-10-18 | minor | G12: the metrics source row in the middle (the owner's correction of WF24, D-88) |
| 2026-10-18 | minor | G16: WF15, the YAML in the Kubernetes order, built (D-86) |
| 2026-10-18 | minor | G16: WF13, WF14, WF16 and WF19 (dialogs as tables, no raw resourceVersion, the CSV's name), built (D-85) |
| 2026-10-18 | minor | G16: WF20, the namespace filter of one cluster came along to the next, fixed |
| 2026-10-18 | minor | G12: a node's disks as a usage bar per real filesystem and the Nodes list's Disk the fullest (the owner's WF26, D-90); no gap closes or opens |
| 2026-10-18 | minor | G12 (the owner's WF22, WF24, WF25 and WF28, D-87 and D-88): the source row under the drawers' and the Overview's charts in place of the source line, the status lines and the Prometheus… button, links to the settings only when one is not reachable; the provider settings on the integrations' own sections of Cluster Settings, the Metrics source dropdown off the page, the Alertmanager's address on its section; estimate not moved |
| 2026-10-17 | minor | G12: a cluster without a choice of integrations asks no metrics until its question's pre-choice is answered (the owner's WF5, D-82), its Metrics source No metrics with the reason; estimate not moved |
| 2026-10-17 | minor | G16: WF9, ⌘Z did nothing in the YAML editor, an undo and redo history built (D-84, the lead's decisions); no estimate moves |
| 2026-10-17 | minor | G16: WF8, a selection dragged out of the YAML editor and released over another widget lost the keyboard (Fyne's driver unfocuses on the release too), fixed in `yamlview`; Fyne's `widget.Entry` fields behave the same and stay so, the lead's decision; no estimate moves |
| 2026-10-17 | minor | G2 and G16: F28 and F29, a certificate renewed in the kubeconfig that Log in again could not use (the connection keeps what it read at its connect) and Reconnect with the tabs kept, built the same day (D-81); no estimate moves |
| 2026-10-17 | minor | G13 and G16: F25, a cluster's uploaded icon on its tabs; F26, a cluster's accent colour, the setting built (D-80), shown nowhere else yet (H-125); F27, copy icons by the cluster's name and kubeconfig path on Cluster Settings › General; built the same day, the lead's decisions, open to revision; no estimate moves |
| 2026-10-17 | minor | G16: F23 and F24, a running container that is not ready orange in the Pods list's Containers column and "+N" for the squares a narrow cell cannot show, fixed the same day; no estimate moves |
| 2026-10-17 | minor | G18: Cogpit reads Trivy Operator's reports since 2026-10-17 for a pod's image ratings (the owner's I6, D-78), the base for Security Center's page of images; the gap stands, no estimate moves |
| 2026-10-17 | minor | G12: the Metrics source kept as the two metrics integrations' switches (D-76), Cluster Settings › Integrations and the question at a new cluster's first connect detecting the providers once when they show a cluster; estimate not moved |
| 2026-10-17 | minor | G18: the Kyverno integration (D-79: policy violations marked in the lists and the drawer, `kyverno.violations`) built and recorded as Cogpit's own, beyond Lens; Lens's Security Center reads Trivy's reports, so G18 does not move |
| 2026-10-17 | minor | G13 and G16: F22, a context's client certificate's time left or "expired" in the navigator's section, on the welcome page and in the tooltips, for agents and `cogpit-cli contexts`, never blocking a connect; built the same day, D-75; no estimate moves |
| 2026-10-17 | minor | G5 and G16 (the owner's F21): the namespace filter's search field, focused when the list opens, typing narrowing it; a click on a name filters to it alone and closes the list, a click on a box adds or removes it; built, not tried in a real window; estimate not moved |
| 2026-10-17 | minor | G16: F20, a node condition whose type ends in Ready (SystemdUnitsReady on STACKIT) healthy when True, fixed the same morning |
| 2026-10-16 | minor | G2: a login that waits (D-73, the owner's finding of 2026-10-16 night) built: Cancel Connecting ends a connect under way and the credential plugin's process it waits for, every plugin run ended after 5 minutes, `waitingForLogin` in the UI state, the connect's limit below the plugin; Missing: a real OIDC plugin in a real window, Windows, H-118; estimate not moved |
| 2026-10-16 | minor | G2: Sync Files and Folders on the system's open panel on macOS, files and folders, several at once (D-72, night run); still open on Windows and Linux; G9: the Create tab's save on the system's save panel; estimates not moved |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): the area under every chart's series filled, the five Overview rows saying what keeps them partial; G12 re-estimated 1–4 h (was 2–7), the totals within their rounding (G1–G16 220–613, the full target 378–935) |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): a drawer that shows only the network or the filesystem asks for no CPU and memory, built, measured on a real cluster (0 Metrics Server gets in 90 s, was 6); estimate not moved |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): the namespace-limited user's Pods history by the plan's namespaces not built, the owner's A47 (the whole cluster, 2026-10-14 evening) standing; the Missing entry corrected, the drop's question open in D-39; estimate not moved |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): Thanos and VictoriaMetrics found by name and shown under their own names (D-71), built, checked against a real Thanos and VictoriaMetrics on kind; "Left of Prometheus" done; estimate not moved |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): the Prometheus's namespace and service as links in the source line, built; estimate not moved |
| 2026-10-16 | minor | G12 (night run 2026-10-16/17): the Prometheus setting's fields on Cluster Settings › Metrics in place of the dialog, built (the lead's decisions in architecture §9); estimate not moved |
| 2026-10-16 | minor | G17 closed (the lead, 2026-10-16 night): checked against D-55, D-59, D-66, D-68 and D-69; `TestEveryClusterCommandNamesItsCluster` and `TestKindAgentTriageAndChangeOverMCP` (M9's measure); estimate 0 h; beyond Lens ~2–6 h (was ~2–8) |
| 2026-10-16 | minor | G13 and G5: the welcome page without its "Connect to Cluster…" button, the workspace's kept, the owner's answer of 2026-10-16 evening (N122), built 2026-10-16 night (`TestConnectToClusterButtons`); estimate not moved |
| 2026-10-16 | minor | G5: a lone checked row's menu ends with Clear Selection after a separator, the owner's answer of 2026-10-16 evening (N112), built 2026-10-16 night (`TestSelectionMenu`); estimate not moved |
| 2026-10-16 | minor | G5 and G13: the owner's answers of 2026-10-16 evening: a lone checked row's menu gains Clear Selection; the welcome page without its "Connect to Cluster…" button, the workspace's kept; not built yet |
| 2026-10-16 | minor | G5: the selection bar's greyed Delete names the engine's reason in its tooltip, "Delete — connection interrupted" (the owner's audit decision of 2026-10-16, D-58), built, decided, not built before; estimate not moved |
| 2026-10-16 | minor | G9: the copy icon over a Secret's editor in the drawer (the owner's audit decision of 2026-10-16, D-59) built, decided, not built before; estimate not moved |
| 2026-10-16 | minor | G12: Cogpit Metrics withdrawn by the owner and removed (D-61): Cogpit installs nothing into a cluster for its own features, metrics only from what the cluster runs; Lens Metrics not a target; the stack's items left Missing; G12 2–7 h (was 3–10), G1–G16 221–616 and the full target 379–938, the headline figures unchanged; G13's Missing 7 of 64 rows |
| 2026-10-16 | minor | The owner's audit of the night decisions (2026-10-16): G1 (D-64, D-15), G2 (Log in again), G3 (D-65), G4 (D-63) and G13 (D-57) confirmed where asked; G5: a lone checked row's menu to be changed, how not said yet, and the greyed Delete's tooltip to name the reason (decided, not built); G9: a copy icon over a Secret's editor (decided, not built); G13: the welcome page's button to be changed; estimates not moved |
| 2026-10-16 | minor | G21: a chat connection's CA file replaces the system's roots for that connection instead of adding to them (N141, the owner's decision in the audit of 2026-10-16, built the same day); estimate not moved |
| 2026-10-16 | minor | G5: each view keeps its checked rows, per tab and cluster, and a selection shown again drops the objects gone meanwhile (N111, the owner's decision in the audit of 2026-10-16, built the same day); estimate not moved |
| 2026-10-16 | minor | G9: keys added and removed in a Config Map's and a Secret's drawer (the owner's audit decision of 2026-10-16, D-55 and D-59), a removal destructive with the review as its confirmation; estimate not moved |
| 2026-10-16 | minor | G5: the window's part of several connected clusters built (D-69 stage 2): the marks, switching without reconnecting, the dock per cluster; estimate not moved |
| 2026-10-16 | minor | G5: D-69 stage 3 built (a background cluster's credential plugin held while its setting says so, the login lapsing until shown; Preferences › App › Restore the last session, off by default, the tabs of the last run only with it on); estimate not moved |
| 2026-10-16 | minor | The standby entry: "Reconnecting…" and "Login expired" as dots, no word, the reconnect blinking green (the owner's F13, D-58's S3 amended); estimate not moved |
| 2026-10-16 | minor | G2: the connection log built (D-66, the owner's decision of the day): every connect, reconnect, login and disconnect on disk per context with its steps and the error picture, the dialog for every context, live, Copy; a failed credential plugin's standard error scrubbed and capped (H-112); removed from Still open and Missing; estimate not moved, for the lead |
| 2026-10-16 | minor | The night run of 2026-10-15/16 recounted (the lead's estimate): G2 8–26 h (standby and lost connections built, D-58); the free parity ~220–620 h (the sum 222–619), the full Lens target ~380–940 h (380–941) |
| 2026-10-16 | minor | G1: `make scorecard` built in the night run (D-15): `hack/scorecard`, `testkit/scorecard`, the first `scorecard.md` (13 budgets measured, all within their limits); removed from Missing; G1 0–2 h (was 3–8), totals not recounted. G15: the scorecard noted, estimate not moved |
| 2026-10-16 | minor | G1: the envtest tier and its CI job built in the night run (D-64): `make test-envtest`, `testkit/envtest`, `TestEnvtestStandbyAPIServerStopped` with the API server stopped by SIGSTOP; removed from Missing; estimate not moved |
| 2026-10-15 | minor | G13, G5, G2: choosing a cluster (D-57) built in the night run: the context picker, "Switch context" and the scope dropdown gone, ⌘P and Connect to Cluster… show the navigator's section with its search focused, "‹ All Clusters" at a cluster, a "+" at the section; the connect log and the insecure mark now on the welcome page and in the section's row; estimates not moved |
| 2026-10-15 | minor | G21: F9 built in the night run (a sent message edited and sent again), out of its Missing list; the estimate's audit-answer part done |
| 2026-10-15 | minor | G21: the `http://` warning and a CA file per connection built in the night run (the owner's audit answer (6) of 2026-10-15), out of its Missing list |
| 2026-10-15 | minor | G21: send all tools per connection built in the night run (the owner's audit answer (4) of 2026-10-15), out of its Missing list |
| 2026-10-15 | minor | G7, G9, G5 (night run): a Secret's values edited in the drawer, the window's only (D-59), built; 42 of 42 actions; estimates not moved |
| 2026-10-15 | minor | G9 (night run): of the owner's audit answers, Save rebasing a Config Map's drawer values by itself (shown to the user) and its unsaved drawer values kept as drafts built; a Secret's values in the drawer still to build; estimate not moved |
| 2026-10-16 | minor | G3: discovery cached on disk (D-65, the lead's decisions for the owner to confirm) and memory per kind and per cluster in the Diagnostics view built in the night run, measured on a real cluster (connect times, the estimate against the heap, P9–P11); the definitions' schemas found holding most of the cache's memory, a candidate; G3 0–4 h (was 4–12), totals not recounted |
| 2026-10-16 | minor | G2: standby and lost connections built 2026-10-16 (D-58), what is left named (the owner's check in a real window, a real login program, an interrupted drain end to end, Windows and Linux signals, H-101); estimate not moved |
| 2026-10-16 | minor | G4 and G7: Drain as a background job built (night run, D-63): the cordon and a step per pod, the PodDisruptionBudget's wait in the pod's step, Cancel and every other stop leaving the drain `stopped` in its journal and offered as after a crash, an MCP client that gives up no longer stopping it; G4's "left long within its call" no longer names Drain; estimates not moved |
| 2026-10-16 | minor | G4: long commands as background jobs built (night run, D-63): an action on a selection of more than ten objects runs as a job with each object's progress in the dock and Cancel for every origin (`job.cancel`), the `ended` audit line; G7 and G9 say so for Delete, Restart and the Flux and Argo CD actions on a selection; Drain and a multi-document Apply left in Missing; estimates not moved |
| 2026-10-16 | minor | G12 (night run): Lens Metrics as Cluster Settings › Cogpit Metrics (D-61, the lead's design for the owner to confirm), the Metrics provider row's query format, empty-container filter and HTTPS, a node's Network and Filesystem from node-exporter; tried on kind and a real cluster, P13 measured there; the Missing list rewritten, G12 3–10 h (was 7–21), the totals recounted with it |
| 2026-10-16 | minor | G6: an owner that is a custom resource links to it, in every drawer's Controlled By and the Pods list's; estimates not moved |
| 2026-10-16 | minor | G6, G7: the views and actions checked against a real cluster the owner provides for tests (D-62), what it showed and what stays open; estimates not moved |
| 2026-10-16 | minor | G7: a confirmation names its objects by kind ("Drain Node worker-1"), not by kind key; estimates not moved |
| 2026-10-16 | minor | G7: a failed drain says what it left of the node, "no pod was evicted" or the count (found on a real cluster the owner provides for tests); estimates not moved |
| 2026-10-16 | minor | G6: an event's Involved Object links to custom kinds through their definitions, as Lens's capture shows (found on a real cluster the owner provides for tests); estimates not moved |
| 2026-10-16 | minor | G23: an OCI Helm repository without a status is Ready, as the flux CLI shows it (found on a real cluster the owner provides for tests); the Flux dashboard's counts checked against `flux get` there; estimates not moved |
| 2026-10-16 | minor | G23: Argo CD checked against a live Argo CD 3.5.5 controller on a real cluster (night run, `TestRealArgoCD`); five findings fixed (the Resources' Health column, an Application in another namespace's project and windows, the windows' check, the operation's source, an agent's Dashboard right after connecting); "a check against a running Argo CD" removed from Left; estimate not moved |
| 2026-10-15 | minor | AGENTS.md rule 10, checked against the code at HEAD: the Missing lists rewritten to what is missing today, G2 (standby and lost connections, decided 2026-10-15 evening; the exec plugins' environment; the `oidc` persister and `~/.kube` built), G3 (discovery on disk, memory per kind), G4 (nothing: jobs and cancellation built 2026-10-15, A91), G5 (40 of 40 rows; the audit answers), G6 (no view), G7 (41 of 41 actions), G8 (17 rows; the terminal's theme and font family), G9 (5 of 5 rows; the audit answers), G12 (the Lens Metrics install, the provider row's parts), G13 (8 of 65 rows; one list of clusters), G17 (no MCP row), G21 (the audit answers), G23 (Argo CD built in the night build of 2026-10-13/14); the "Decided (2026-10-14 evening) … not built" lines of G1, G5, G6, G8, G9, G11, G12, G13 and G23 marked built; dated "still missing" lists in G2 to G6 corrected; G13: agents' proxies (A20) and A97 built; D-39, D-41 and D-43 decided by the owner; G17: H-83's expiry built; G16: the owner's findings of 2026-10-15 evening; verifying the cloud credential plugins out of scope until long after parity (owner, 2026-10-15). Re-estimated (the lead's estimate, PhotoCraft's calibration, a partial row at 0.25–1 h): G2 15–43 h (was 8–25), G3 4–12 (16–38), G4 0–3 (10–28), G5 11–41 (35–110), G8 7–25 (13–34), G9 5–17 (8–22), G12 7–21 (the table's 40–80, the entry's 8–20), G13 28–96 (50–140), G21 6–18 (3–10), G23 8–22 (55–115); free parity ~240–665 h (was ~420–1,040), the full Lens target ~400–990 h (~620–1,450), beyond Lens ~6–20 h (~3–12) |
| 2026-10-15 | minor | G14: Sparkle's update key and the environments' rules set up by the owner (2026-10-15), in the releases since v0.11.0, no signed appcast served yet (no release with the key has passed notarization); the signed disk image proven by v0.6.0 to v0.8.0 and `cogpit-cli` inside the app by v0.7.0 and v0.8.0; notarization in a job of its own since 163664f2; A10 decided further (the semantic-release App the only bypass), not set up. G21's summary row said 20–40 h while its entry says 3–10 h since the night run: brought to the entry, and the beyond-Lens total to ~3–12 h as in ROADMAP. Estimates not moved |
| 2026-10-15 | minor | G21: the owner decided on 2026-10-15 that Cogpit keeps no API key in a keychain; built the same day: the keys in an owner-only file (`llm-keys.json`) on every platform, the keychain code removed, a key kept there before entered once again (D-10 item 12, H-99); G21's "a keychain on Windows and Linux" gone, beyond-lens 10 of 10 rows done; G22's keychain backends gone; estimates not moved |
| 2026-10-15 | minor | G5, G9, G16: the owner's finding F3 (long keys under their values in a Config Map's drawer) built the same day (D-55): each key above its value, a Config Map's values in editors with Save at the end through `configMap.reviewData` and `configMap.saveData`, names without spaces wrapping at - . _ /; a Secret's values in the drawer open for the owner; estimates not moved |
| 2026-10-15 | minor | G5: the title bar's "Update" button built (the owner's finding F7, D-54), shown while Sparkle holds an update, beside Cogpit's own layout toggles; estimate not moved |
| 2026-10-15 | minor | G5: a copy's confirmation shown as a toast only, not in the notification log (the owner's F5); estimates not moved |
| 2026-10-15 | minor | G21: H-96 built, a conversation in which a turn ran over a private connection is not kept (memory only, for good), files of v0.8.0 to v0.10.0 rewritten without such conversations at the next start, the window's hint and "not saved" in the history; no longer under Missing; estimate not moved |
| 2026-10-15 | minor | G21: H-96 decided by the owner (a private connection's conversations not kept), not built; G13: several windows (R3) postponed by the owner until the rest is done; estimates not moved |
| 2026-10-15 | minor | G13: R2 built (night run), hotbar tiles reordered by drag and drop through `hotbar.moveCluster` (`fyneui/hotbardrag.go`); the lead's decisions for the owner to confirm (the line in the gap and the outline, a drop on the "+" puts the tile last, a drop outside the bar does nothing as R3's seam, a drag under eight points opens the cluster); dragging on the hotbar no longer missing; the paid row partial; estimate not moved |
| 2026-10-15 | minor | G13: several windows (R3) designed in the night run, the owner's decision pending ([D-52](../developer/decisions.md#d-52-several-windows)); its two mentions no longer say "needs a design"; nothing built, estimate not moved |
| 2026-10-15 | minor | G5: A73 (a plain click keeps the checked rows), A69 (the lists' copy icon over the cell's end) and A71 (the drawer's Forward… right after the port, the icon over its end) built; estimate not moved |
| 2026-10-15 | minor | G1, G8: A116 built, the terminal fork's four shell tests under the race detector in CI, after seven races of its `RunLocalShell`, directory check and `ExitCode` were patched (D-14); estimates not moved |
| 2026-10-15 | minor | G1 (A110, A111, A112, A114), night run: the README's pictures rendered in the dark and the light theme and shown through `<picture>`, a banner, `make showcase` with the pinned oxipng (lossless), `demo` the connected context in every picture; estimate not moved |
| 2026-10-15 | minor | G8: an application's logs in one merged tab (A81, built; Argo CD Applications and Flux Kustomizations too, the lead's decision for the owner to confirm); estimate not moved |
| 2026-10-15 | minor | G23: Reconcile with Source waits as the flux CLI does, a background job in a dock tab of its own (A91, built; the lead's decisions for the owner to confirm in D-33); estimate not moved |
| 2026-10-14 | minor | G23: Flux and Argo CD recognised from API discovery without the right to list the definitions, with their curated profiles (A90, built; every curated profile, the lead's decision for the owner to confirm); estimate not moved |
| 2026-10-14 | minor | G8: Download saves what the logs tab shows, its filter applied (A84, built); the Jump to time field's cut placeholder fixed (#169); estimate not moved |
| 2026-10-15 | minor | G8: kubectl's signature checked with sigstore-go and its Rekor entry (D-50 built, night run, A25); estimate not moved |
| 2026-10-15 | minor | G5, G13: the owner's A51–A55, A61, A63, A64, A67 and A113 built (the navigator's search and keyboard row and the palette's text as engine state, D-51 with the lead's night decisions for the owner to confirm; the ring on keyboard navigation only; the scope dropdown of the hotbar's clusters; collapsible source groups kept across restarts; Disconnect closing the cluster's tabs; F1–F12 in the search fields; ⌘F and ⌘⇧T as View menu items; the welcome page's Recent and its two links); estimates not moved |
| 2026-10-15 | minor | G11: A14, A17, A36 and A39 built; estimate not moved |
| 2026-10-15 | minor | G2 (A29, H-37), night run: "TLS not checked" in the status bar while such a context is connected, from the engine's state, and `cogpit-cli get`'s warning line on stderr; estimate not moved |
| 2026-10-14 | minor | G2 (A28, H-37), night run: an `insecure-skip-tls-verify` context asks once before its first connect, kept like a plugin approval; agents, the CLI and links refused until the user allowed it; estimate not moved |
| 2026-10-14 | minor | G2, G13 (A23, H-34), night run: a credential plugin's approval shows and binds Cogpit's own proxy for the cluster, a changed proxy asks again; estimates not moved |
| 2026-10-14 | minor | G21: the window's half of the built-in agent built (M12 (b), night run, against the engine's contract and a scripted agent): the chat column and Preferences › Agent; the agent itself (`internal/agent`) named as what is missing; the lead's decisions for the owner to confirm in D-10; estimate not moved |
| 2026-10-14 | minor | G13: the owner's finding F2 fixed (About read Fyne's default version 0.0.1 in every build; now the binary's own from `internal/buildinfo`, a development build saying so, the dialog laid out anew) and decision A60 built (About Cogpit in macOS's application menu, Help's gone there); G16: the finding marked fixed; estimates not moved |
| 2026-10-14 | minor | G21: the private connection's reads like the user's built in the engine (M12 (c)); H-96 named under Missing; estimate not moved |
| 2026-10-14 | minor | G21: the built-in agent's engine side built (M12 (a)): connections and keychain, the client, the connection test, the turn loop with the limits and Stop, conversations kept scrubbed; re-estimated 20–40 h (was 40–80 h); the beyond-Lens total ~20–42 h |
| 2026-10-14 | minor | G12 (A46), night run: separate query sets for ranges far apart built; the question "Waiting for the owner" marked built |
| 2026-10-14 | minor | G12, G1, G16 (F1, A43, A115), night run: node CPU and memory history from node-exporter where the Prometheus has it (the cause of F1 recorded), node-exporter and kube-state-metrics in the kind cluster's Prometheus values with kind tests of node and pod history; a node's Network and Filesystem on kube-prometheus-stack added to Missing; estimate not moved |
| 2026-10-14 | minor | The owner's decision round of 2026-10-14 evening: a "Decided" bullet in G1 (CI, tests, the README showcase), G2, G5, G6, G7, G8, G9, G11, G12, G13, G14, G17 and G23, each naming what he confirmed (the defaults marked "Cogpit's default" now his decisions) and what he decided, not built; his findings in a real window: no node history from Prometheus (G12, first item of the next implementation) and the About dialog's 0.0.1 (G13), both in G16 too; G11 Missing: the SQL driver out of scope; G12: Waiting for the owner answered (a query set per range), Missing without the ranges beyond 30 days (not to be built); G13: the session's leftovers confirmed, agents may set a proxy, the hotbar's drag and several windows required; G23: discovery recognises Flux and Argo CD without the definitions; estimates not moved |
| 2026-10-14 | minor | G14 and G22: D-16 revised by the owner, built by agent CLIBUNDLE: `cogpit-cli` inside `Cogpit.app`, signed and notarized with it; a release carries the disk image and `SHA256SUMS` only, no `cogpit-cli` archives for Windows and Linux any more; not yet proven by a release, estimates not moved |
| 2026-10-14 | minor | G14: Sparkle 2 and the signed appcast built (agent SPARKLE), not yet proven by a release with the owner's key; removed from Missing: the signed update manifest, the update notice and the one-click update; estimate 12–30 h (was 22–45); totals not recounted |
| 2026-10-14 | minor | G13: `cogpit://` deep links partial on macOS (navigation only, D-47), a free row; estimate not moved |
| 2026-10-14 | minor | G13: start at login partial on macOS (the app's login item, D-48), a free row; estimate not moved |
| 2026-10-14 | minor | G17: the cached Secrets' values replaced in what agents read (H-82, D-44) and Flux's credential fields withheld from agents' YAML (H-63), both the owner's answers, built; G11: a Helm repository's credentials over plain http asked once per repository (H-40, D-29), built |
| 2026-10-14 | minor | G8: the node shell's pod journaled before its create, and one a crash left deleted by UID at the next connect to its context (H-71, the owner's decision); estimate not moved |
| 2026-10-14 | minor | G7: every role binding an agent asks for waits for the user, also where agent writes are allowed (H-27, the owner's decision); estimate not moved |
| 2026-10-14 | minor | G15: P1 measured on the engine side (`TestBudgetFirstRows`, a fake API server over HTTPS): 111–123 ms from connect to 1,000 rows; estimate not moved |
| 2026-10-14 | minor | G14: the app's minimum macOS set to 13.0 (Go 1.27's), in the binary and the Info.plist; v0.4.0's said 26.0 |
| 2026-10-14 | minor | G14: the Developer ID signature, the disk image `Cogpit-<v>-macos-arm64.dmg` and the notarization built in the release pipeline, `cogpit-cli` for macOS signed and notarized (agent REL); no tag run yet, estimate not moved; the app's minimum macOS (26.0, the runner's) added to Missing |
| 2026-10-14 | minor | G9: Create Secret's shown value copies through the clipboard for Secret values, concealed and cleared after 60 seconds (H-70 closed); estimate not moved |
| 2026-10-14 | minor | G14: the Apple Developer ID certificate and the notarization API key exist (environment `release`); only the update signing key is left as a human prerequisite |
| 2026-10-14 | minor | G6: the workloads' drawers' Pods tables without a Namespace column (D-40, decided by the owner); estimate not moved |
| 2026-10-14 | minor | G23: Reconcile with Source and Reset built as the flux CLI does them (D-33, decided by the owner; Force stays an agent's parameter), removed from Left; estimate not moved |
| 2026-10-14 | minor | G23: the views' status filter built (owner, D-34): a Flux or Argo CD dashboard's state opens its kind's view filtered to it; removed from Left (Flux CD); estimate not moved |
| 2026-10-14 | minor | G7, G13: D-37 and D-38 decided by the owner; the node shell's default image the newest supported Alpine release (3.24) pinned by digest and kept current by Renovate, Cluster Settings › Node Shell › Maximum duration (default 2 hours, 5 minutes to 24 hours) built; estimates not moved |
| 2026-10-14 | minor | G12: Cluster Settings › Metrics › Metrics source built (Automatic, Prometheus, Kubernetes Metrics Server, No metrics; Cogpit's defaults), removed from Missing; estimate not moved |
| 2026-10-14 | minor | G12: the cluster Overview's Pods history from kube-state-metrics through Prometheus built (a night decision for the owner to confirm), removed from Missing; with the usage bars, Disk and the kept charts the remainder re-estimated 8–20 h (the lead's estimate to apply to the table) |
| 2026-10-14 | minor | G12: a closed drawer's chart kept (the last hour, 50 charts, 50,000 points, LRU; a night decision for the owner to confirm), the lines break at a gap, the memory measured; removed from Missing; estimate not moved |
| 2026-10-14 | minor | G12: the Nodes list's Disk and a node drawer's disk from node-exporter through Prometheus built (D-43, night decision), the budget measured against fakes, removed from Missing; estimate not moved |
| 2026-10-14 | minor | G12: the Nodes list's CPU and Memory drawn as usage bars (thresholds 80% and 90%, Cogpit's), removed from Missing; estimate not moved |
| 2026-10-14 | minor | G11 (night build HELM2): chart icons built (D-46, a night decision for the owner to confirm), removed from Missing; what is left is OCI registries and the captures; estimate 2–8 h (was 3–10), the totals not recounted (the lead recounts) |
| 2026-10-14 | minor | G11 (night build HELM2): the README in the chart's drawer built with a version picker (`ui.setChartVersion`), read from the version's archive when the drawer or an agent asks, cached when verified; removed from Missing; the estimate not moved until the icons land |
| 2026-10-14 | minor | G5, G13 (the night build's seams): a removed source's or a gone file's context takes its dock tabs with its cluster tabs (a node shell's pod deleted, an editor's unsaved text kept as a draft); a connect under way when its context leaves Cogpit forgets it at its end; estimates not moved |
| 2026-10-14 | minor | G3, G23 (the night build's seams): the Flux and Argo CD dashboards wait for a namespace-limited user's plans and follow refusals, as pages and as agents' reads; they say when the definitions cannot be listed; a refused label-selected scope is not kept warm; estimates not moved |
| 2026-10-13 | minor | G17 (night build): `kinds_list` completed; all 17 MCP rows done; estimate 0–2 h (was 4–12), the totals not recounted (the lead recounts) |
| 2026-10-13 | minor | G17 (night build): `dock_read`, the output of logs tabs and, once the user allowed it per tab, terminal tabs (D-44); removed from Still missing; estimate not moved yet |
| 2026-10-13 | minor | G17 (night build): every command an MCP tool checked over Cogpit's whole registry; GUI-only commands under one mark the engine enforces (`Spec.GUIOnly`), refused to agents and the CLI before anything is asked or read; estimate not moved |
| 2026-10-13 | minor | G5: no context picker by itself at start, the welcome page under the restored tabs (D-42, a night decision for the owner to confirm); estimate not moved |
| 2026-10-13 | minor | G5, G13: the tabs and the tables' hidden columns and column order kept across a restart and restored without connecting anything (D-41, night decisions for the owner to confirm); a removed cluster's context forgotten and a vanished kubeconfig file's tabs closed with a notice (the lead's, for the owner to confirm); estimates not moved |
| 2026-10-13 | minor | G6: a Pods table in the drawers of Deployments, Replica Sets, Stateful Sets, Daemon Sets, Jobs and Cron Jobs built (night build, D-40, a night decision for the owner to confirm), removed from Missing; its indexes the drawer's own (`DetailNeeds`), so the workload lists do not rebuild on a pod change (measured); every sortable drawer table at most 500 rows; estimate not moved |
| 2026-10-13 | minor | G12: Prometheus detection reads the Services per namespace for a namespace-limited user and follows a refusal (fixed); estimate not moved |
| 2026-10-13 | minor | G3: a refused list of the custom resource definitions ends their watch until a refresh (fixed); a namespace-limited ServiceAccount checked on kind (`TestKindNamespaceLimitedUser`); estimate not moved |
| 2026-10-13 | minor | G3: a namespace-limited user's views with a refused index, an agent's Overviews, `Lookup` and write targets, Helm's releases and the Applications follow the plan and refusals (fixed); `view_rows`, `resources_find`, the palette checked; estimate not moved |
| 2026-10-13 | minor | G12: the pods' metrics listed per namespace of the Pods view's plan for a user who may not list pods everywhere (D-39, night decision, built); estimate not moved |
| 2026-10-13 | minor | G3: `object_get`, `cogpit-cli get` and the GUI's drawer read the object's namespace and its events there for a user who may not list the kind everywhere (fixed; night decision on the events' scope); estimate not moved |
| 2026-10-13 | minor | G3: a user whose one RoleBinding is in the context's namespace: logs, port forwards, one object's read and `resources_list` follow the refused read in all namespaces at once, and every reader's refusal counts (fixed); estimate not moved |
| 2026-10-13 | minor | G23 (night build): Argo CD built, partial: the navigator group, five lists, the Dashboard, Sync with Prune and Refresh Normal or Hard (D-35, D-36); what is left for Argo CD about 3–8 h; the gap's estimate for the lead to redo with Flux CD's |
| 2026-10-13 | minor | G23 (night build): Argo CD built, partial: the navigator group, five lists, the Dashboard, Sync with Prune and Refresh Normal or Hard (D-35, D-36); what is left for Argo CD about 4–10 h; the gap's estimate for the lead to redo with Flux CD's |
| 2026-10-13 | minor | G23: the Flux CD section built, partial: the navigator's Flux group, curated profiles of the thirteen Flux kinds, the Flux dashboard (D-34), Reconcile, Suspend and Resume on objects and selections (D-33); Flux's remainder 6–15 h, the summary table's estimate left to the lead |
| 2026-10-13 | minor | G7, G8, G13: the node shell partial (night build, D-37, a night decision for the owner to confirm) and Cluster Settings › Node Shell (D-38); every one of the 39 actions exists; G7 8–35 h (was 12–45), G8 13–34 h (was 16–40); the totals not recounted (the lead recounts) |
| 2026-10-13 | minor | G9, G7: Create Secret partial (night build): `secret.create` and its dialog, the user's only (D-19), the values base64-encoded in `data`; 38 of 39 actions exist; H-70 opened; estimates not moved |
| 2026-10-13 | minor | G1: CI rebuilt as parallel jobs of at most ten minutes (GoSec, cyclomatic complexity, unit tests, eight race shards, the kind and drain clusters, macOS-only tests, combined coverage), semantic-release live (D-16 revised); G14: each release carries the unsigned app, `cogpit-cli` and `SHA256SUMS`; estimates not moved |
| 2026-10-13 | minor | G12 and G6 (owner): a node drawer's Pods table shows its pods' CPU and Memory from the metrics, polled while it shows, and sorts by any column as engine state; removed from G12's Missing; estimate not moved |
| 2026-10-13 | minor | G12 evidence: the metrics' bytes in binary units with one decimal, the Pods list, the charts and the Overview (owner, D-32); estimate not moved |
| 2026-10-13 | minor | G8 and G13 evidence: every timestamp in the time zone preference and the computer's region format (owner, D-31), logs and their time field included; estimates not moved |
| 2026-10-13 | minor | G6 evidence: the Secrets drawer decodes the certificates in its keys, for agents too (D-30, beyond Lens, not counted in G6); estimate not moved |
| 2026-10-13 | minor | G11: D-29 built: Helm's repositories.yaml read every 2 s, authenticated and plain-http repositories included, credentials in memory only and to the repository's own host, indexes in Cogpit's own cache, add and remove removed; removed from Missing: authenticated and plain-http repositories; estimate 3–10 h (was 4–12), totals not recounted (the lead recounts) |
| 2026-10-13 | minor | G3: workload logs, port forwards and the read of one object wait for the access reviews, then a namespace-limited user reads the target's namespace (fixed); estimate not moved |
| 2026-10-13 | minor | G5: copy icons in the lists built (D-25; every text cell and the placement Cogpit's defaults); estimate not moved |
| 2026-10-13 | minor | G3: the Workloads and cluster Overviews follow the access reviews' answers (fixed); workload logs and port forwards opened before them still fail for a namespace-limited user; estimate not moved |
| 2026-10-13 | minor | G6: a reference to a Pod Security Policy, an admission policy or binding, or a Device Resources object links to it (fixed); estimate not moved |
| 2026-10-13 | minor | G2: `cogpit-cli contexts` marks a context with `insecure-skip-tls-verify` in its table (H-37); estimate not moved |
| 2026-10-13 | minor | G5, G13: the tabs of a cluster removed with `cluster.remove` or `kubeconfig.remove` close in the engine (fixed); estimates not moved |
| 2026-10-13 | minor | G13: the window's Cluster › Disconnect follows a reconnect of the same cluster (fixed); estimate not moved |
| 2026-10-13 | minor | G11: Helm's repositories.yaml as the repositories' source (owner, D-29); G5: copy icons in the lists (owner, D-25); not built |
| 2026-10-13 | minor | G17 (night run): `view_rows`, `object_get`'s related objects, the allowance of agent writes in Cluster Settings › Agents and the audit log in the window (Help › Audit Log, `audit_list`) built; 14 done and 3 partial of the 17 MCP rows; estimate 4–12 h (was 12–30; the totals not recounted, the lead recounts) |
| 2026-10-13 | minor | G9 evidence (night run): the Create resource tab's save and search buttons; estimate not moved |
| 2026-10-13 | minor | G9 evidence (night run): an Edit tab's Revert to Original with an undo; estimate not moved |
| 2026-10-13 | minor | G9 evidence (night run): drafts of unsaved editor text kept, offered on connect, restored and discarded, never a Secret's or Helm values; estimate not moved |
| 2026-10-13 | minor | G11 (night run): the ConfigMaps storage driver built (releases kept in ConfigMaps read and acted on with their own driver, the `HELM_DRIVER` preference for new installs), removed from Missing; the SQL driver stays with its reason; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G13 (night run, D-3): Preferences › Kubernetes holds the four kubectl settings; kubectl download leaves the Missing list; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G8 (night run, D-3 built): a terminal checks its kubectl against the cluster's version and offers the matching release, downloaded on the user's click and verified (SHA-256 and the release's sigstore signature); estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G6 evidence (night run): the node drawer's Devices from the resource slices that name the node (`SlicesByNode`); estimate not moved |
| 2026-10-13 | minor | G8 evidence (night run): copy and paste with the right mouse button (a preference, the user's alone) and the Windows and Linux copy and paste keys, two patches of the fork; estimate not moved |
| 2026-10-13 | minor | G5 (night run): the search field's match case and regular expression toggles built as engine state per tab; estimate not moved |
| 2026-10-13 | minor | G13 (night run): the HTTP proxy for non-cluster traffic built, removed from Missing; untrusted CAs held back as a question for the owner; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G13 (night run): a cluster's proxy built (Settings › Proxy, the password redacted, only the user sets it: Cogpit's default), removed from Missing; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G12 (night run): hidden metrics built (Cluster Settings › Metrics, nothing polled for what is hidden), removed from Missing; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G5, G13: the navigator's section level, Local Kubeconfigs, built (night run): the clusters of the kubeconfig sources grouped by source, the › drilling in through `ui.setNavigatorScope` (`UIState.NavigatorScope`, kept in step with the connection), the ‹ going up, the tab bar showing the scope's tabs; Navigator sections and Drill into a section or cluster partial (the cloud sections, Teamwork and Extensions not built: D-9, D-5); estimates not moved (the lead recounts) |
| 2026-10-13 | minor | G5 (night run): the title bar's history arrows, the notifications' bell and panel (a log of every toast as engine state, three `notifications.*` commands) and its context menu built; Notifications in the title bar partial; what is inferred; estimate not moved (the lead recounts) |
| 2026-10-13 | minor | G5, G13: the welcome page (`UIState.Welcome`, `ui.welcome.open`), the Help and Window menus (macOS keeps GLFW's Window menu, verified in source), Disconnect and Remove from Cogpit (`cluster.disconnect`, `cluster.links`, `cluster.remove`) built (night run); four free rows partial; zoom missing with its reasons; estimates not moved (the lead recounts) |
| 2026-10-13 | minor | G8 evidence (night run): jumping to a time partial, every Logs row exists; estimate not moved |
| 2026-10-13 | minor | G8 evidence (night run): loading older lines partial; estimate not moved |
| 2026-10-13 | minor | G8 evidence (night run): the log tab's pod and container selectors partial; estimate not moved |
| 2026-10-13 | minor | G11: a release upgraded to a chart version from the user's repositories (the Upgrade tab's choice, `helm.release.upgrade` with `version` and `repository`, the chart downloaded and checked first, the confirmation's from and to bound to what runs; night run) |
| 2026-10-13 | minor | G5, G13: the tab bar of cluster views built (night run): engine state with eight `ui.tab.*` commands, the bar, the navigator's preview and fixed tabs, five shortcut rows; what differs from Lens and what is inferred; estimates not moved (the lead recounts) |
| 2026-10-13 | minor | G13: Preferences and Cluster settings built (night run): the two pages and their shortcuts, cluster identity, the theme, time zone, editor, tables and terminal preferences, the cluster's terminal settings; G8: the terminal's font size, shell and arguments; what is inferred and Cogpit's defaults named; estimates not moved (the lead recounts) |
| 2026-10-13 | minor | G12: the cluster Overview built (night run, M7): charts of the master or worker nodes through a cluster scope of the hub, lines and Pods from the cache, the Issues from the cache, three commands, budget P14; the remainder re-estimated 15–35 h (the summary table not recounted) |
| 2026-10-13 | minor | G6: Applications partial (a virtual kind over the cached workloads, Logs per workload); 3 views missing, estimate 12–60 h |
| 2026-10-13 | minor | G6: Vertical Pod Autoscalers partial (a curated profile of the CRD kind); 4 views missing, estimate 16–70 h |
| 2026-10-13 | minor | G6: Resource Claims, Resource Claim Templates, Device Classes and Resource Slices partial (Lens's documented columns), the pod drawer's Resource Claims section; 5 views missing, estimate 20–80 h |
| 2026-10-13 | minor | G11: chart repositories in Cogpit's own folders, the Charts view, the Helm Install tab and the install as a crash-safe job (night run); what is left and what to capture |
| 2026-10-13 | minor | G17: the `cogpit-cli mcp` stdio bridge built, evidence added; estimate unchanged |
| 2026-10-13 | minor | G2: `cogpit-cli contexts` and `cogpit-cli get` built (milestone 0.1), evidence added; estimate unchanged |
| 2026-10-13 | minor | G5, G13: the navigator built (night run): header, the cluster as the tree's root, item tooltips, keyboard; what differs from Lens and what is inferred; estimates not moved (the lead recounts) |
| 2026-10-12 | minor | G12: the evening's requirements built (fourth slice): both providers together (Prometheus's history once, the Metrics Server's live values, the network and the filesystem only while shown: the lead's proposal), the fallback, a status line per provider (also in `metrics.get` and `ui.state`), a chart's variant as engine state, a missing value as an empty cell; budget P13 measured on kind; no count moved |
| 2026-10-12 | minor | G7: seven create dialogs partial (Namespace, Service Account, the four RBAC kinds, Resource Quota); 3 of 39 actions missing; estimate 12–45 h (was 20–70); totals not recounted |
| 2026-10-12 | minor | G13: the hotbar built (night run), what differs from Lens and what is inferred; estimate 58–150 h (the totals not recounted) |
| 2026-10-12 | minor | G12: the owner's evening requirements (a status line per provider; history from Prometheus, then live values from the Metrics Server; one of the two alone only when the other fails or is missing) and the step of mixed ranges waiting for the owner; G16: the quit crash fixed |
| 2026-10-12 | minor | G12: checked against a real Prometheus on kind (the `prometheus` chart); three faults it found fixed; only kube-prometheus-stack and the override left unchecked; estimate not moved |
| 2026-10-12 | minor | G12: the window draws Prometheus (Network, Filesystem, ranges to 30 days held by the engine through `ui.setMetricsRange`); range values beyond 1h inferred; estimate not moved |
| 2026-10-12 | minor | G12: Prometheus through the service proxy, backend (detection, range queries for CPU, Memory, Network and Filesystem, per-chart ranges, budget P13); the window's Network and Filesystem buttons left; estimate not moved |
| 2026-10-12 | minor | G12: the owner's hover requirement built (crosshair, dots and a panel with time, values, request and limit, events); estimate not moved |
| 2026-10-12 | minor | G12: every drawer chart has its own controls (owner's smoke launch: "no main button"); estimate not moved |
| 2026-10-12 | minor | G2 (scope C2): the Kubeconfig Syncs captures followed: dot files, a closed connection, one dialog, `~/.kube` by default, the connect log, the Remove confirmation; estimate not moved (the lead recounts) |
| 2026-10-12 | major | Totals after the fourth run: free parity ~420–1,040 h, full target ~620–1,450 h |
| 2026-10-12 | minor | G8: the terminal widget fuzzed alone, its crashes, hangs and races patched, the upstream series prepared (D-14, fourth run); estimate of that part done |
| 2026-10-12 | minor | Fourth run, the third run's leftovers: G7 evidence (the journals shared by Cogpit processes); G4 evidence (Refresh Access Reviews in the menu, the View menu hides what the sidebar hides); G3 evidence (live related-objects indexes, measured); G8 evidence (workload log streams resumed and pruned); G7 evidence (Restart and Scale bound to the UID in their patches); estimates not moved |
| 2026-10-12 | minor | G2 (fourth run, agent D): kubeconfig files, sync folders and pasted kubeconfigs, the approval of their credential plugins; estimate 10–28 h |
| 2026-10-12 | minor | G11 (C1): the release drawer, Rollback dialog and Upgrade tab follow the second capture; estimate 10–26 h (the totals not recounted) |
| 2026-10-12 | minor | G11 (fourth run): the Helm upgrade and its dock tab, Lens's release drawer and Rollback dialog built; estimate 12–30 h (the totals not recounted) |
| 2026-10-12 | minor | G3 evidence: covering scopes (fourth run); G6 evidence: Secrets on a full watch, Keys and Type filled, the drawer from the cache (D-13 revised, built) |
| 2026-10-12 | minor | G1: `make parity` built (D-15); its first run's findings; estimate 3–8 h |
| 2026-10-12 | minor | Owner's decisions: G1 the parity checker first (D-15); G6 Secrets as a full watch (D-13 revised); G8 the terminal widget kept, upstreamed and fuzzed (D-14); G11 Lens's Helm release drawer, dialogs and upgrade tab captured; G14 the download job belongs to the alpha (D-16) |
| 2026-10-11 | minor | G5: evidence of the third run's tables and window work; G15: why the gate's offscreen test hung in CI, and the fix |
| 2026-10-11 | minor | G6 and G11 evidence: Secrets and Helm Releases partial, Helm rollback and uninstall partial (third run) |
| 2026-10-11 | minor | G10: port forwarding built (manager, local kind, commands, dialog, view); what is left: Lens's status texts and Edit dialog, access review of the forward's pod, a real-window browser check |
| 2026-10-11 | minor | Access reviews built: G2 and G4 evidence, G3 and G9 no longer list them as missing; estimates not moved |
| 2026-10-11 | minor | G6 evidence (third run, agent D): Endpoint Slices, Endpoints, Network Policies, Persistent Volumes, Storage Classes and Replication Controllers views partial; the related-objects index's first users (a claim's pods, a node's Pods table, a Service's endpoint slices) |
| 2026-10-11 | minor | G6 Evidence: the Access Control views (five of them partial) |
| 2026-10-11 | minor | G6 Evidence: the 13 Config views (third implementation run), admission policies by served version |
| 2026-10-11 | major | Third implementation run: G2–G11 and G15 re-estimated from what landed; totals ~430–1,060 h free parity, ~640–1,480 h full target |
| 2026-10-11 | minor | G11 unblocked by the H-8 decision; G5: Cogpit's own title bar on macOS (owner) |
| 2026-10-11 | major | Second implementation run: G1 (CI green), G3, G5, G6 (27 of 64 views), G7 (14 of 39 actions, drain tested on three nodes) and G9 re-estimated; totals ~570–1,400 h free parity, ~780–1,820 h full target |
| 2026-10-11 | minor | G5: row menus, drawer toolbar, warning column, links, tooltips and the drawer's Properties built; what remains |
| 2026-10-11 | minor | G9: editing, create from templates and Delete for every kind built; what is left listed |
| 2026-10-11 | minor | G7: node and workload actions partial (cordon, uncordon, crash-safe drain, scale, trigger, suspend, resume, evict) |
| 2026-10-11 | minor | G1: CI and the semantic-release dry run exist as workflow files, not yet observed green; generators, the envtest job and a passing `make vuln` remain; estimate not moved |
| 2026-10-11 | major | The MVP: G1–G9, G14, G15 and G17 re-estimated with evidence; new terminal-widget risk in G8 |
| 2026-10-11 | minor | D-5: G19 (external extensions) is not a target and leaves the totals |
| 2026-10-11 | minor | D-9: Flux CD and Argo CD views split from G18 into G23, in the beta |
| 2026-10-11 | minor | macOS first (D-7): G14 covers macOS, G22 Windows and Linux after the macOS beta |
| 2026-10-11 | major | Re-estimated for Go (G2, G7, G11, G18 smaller; G3 larger for the cache; G9 own editor); G17 is MCP control of UI and cluster; G21 built-in agent added; G14 includes one-click updates; Freelens rows removed |
| 2026-10-11 | major | First version: G1–G20 ranked, before any code |
