# Using Cogpit: running, flags, files and agents

The reference that used to be in the root [README](../../README.md), moved here on 2026-10-14
(the owner's call: the README is the showcase page). For whoever builds and runs Cogpit from
source, sets up an agent, or needs the exact flags, preferences, files and names; contributors
start at the [developer documentation](README.md). Every command and MCP tool is listed in
[`commands.md`](commands.md).

## What Cogpit is and its status

Cogpit is a native Kubernetes IDE for the desktop, written in Go: an open-source, clean-room
reimplementation of the current [Lens Desktop](https://lenshq.io/). It runs on your Mac, from
the release after v0.32.0 on Debian and Ubuntu, and from the first release after the branch
`windows-release` is merged on Windows 10 and 11 (built 2026-10-21,
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)). It reads your kubeconfig files, connects to your clusters with your
own credentials, and gives you the views Lens users know: workloads, logs, shells, YAML editing,
port forwarding and the Helm releases in your clusters. Every view reads from a cache that watches
keep current, so it is fast for you and light on the API server. And agents can drive all of it
over MCP, the UI included. Cogpit is a desktop client: it is never a web UI and is never deployed
into a cluster. **Status: alpha** (declared by the owner 2026-10-22): every view and every action on Lens's
list exists at least in part (64 of 64 views, 42 of 42 actions, counted by `make parity`), with
the Flux CD and Argo CD sections, Helm releases and charts, metrics, logs, a terminal, pod and node
shells, the built-in agent's chat and MCP control of all of it; every core workflow of the alpha
gate was walked end to end on real clusters (2026-10-17 to 2026-10-22). The releases are builds for
Apple silicon, signed and notarized, updated through Sparkle; Linux packages for amd64 and arm64
with Cogpit's APT repository ([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages));
and since v0.37.0 a Windows setup for x64 that WinSparkle keeps up to date
([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle))
([Download a release](#download-a-release), [Install on Linux](#install-on-linux),
[Install on Windows](#install-on-windows), [Run the MVP](#run-the-mvp)); where Cogpit stands:
[roadmap](../../ROADMAP.md).

How data flows from the kubeconfig files to the views and agents:
[developer documentation › Data flow](README.md#data-flow).

## Features in detail

What Cogpit does and is meant to do, feature by feature. Everything below exists at least in part
unless it is marked *(planned)*; how far each one goes is measured row by row in
[docs/parity](../parity/README.md) and summed up in [ROADMAP.md](../../ROADMAP.md).

- 🖥️ A native macOS app; since 2026-10-20 night a Linux package too, a `.deb` for amd64 and arm64 (Debian 13, Ubuntu 24.04 and later) that sets up Cogpit's signed APT repository ([Install on Linux](#install-on-linux), [D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)); since 2026-10-21 a Windows setup for Windows 10 (1809) and 11 on x64, installed for your user without an administrator, on the branch `windows-release` and in the first release after its merge ([Install on Windows](#install-on-windows), [D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); on Linux and Windows no `cogpit://` links and no Start at login yet.
- 🗂️ Every cluster from your kubeconfig files and synced folders, side by side.
- 🧭 Lens's navigator levels: All Clusters (Local Kubeconfigs) lists every cluster, the one list to choose a cluster from, with a + to add kubeconfigs; › drills into one cluster's resources, ‹ All Clusters goes back up. The tab bar shows the tabs of the cluster you are in, the one the status bar and the navigator name, at either level; **Mixed environments** at the foot of All Clusters (off by default, its ? says what it does) shows every connected cluster's tabs side by side; a cluster without a connection (connecting, a failed connect, disconnected) shows no tabs until it connects (since 2026-10-20). Going into a cluster slides its tree in from the right over All Clusters, ‹ All Clusters slides it out again; showing another cluster from a cluster's tree (a tab of another cluster, the hotbar) slides vertically, the cluster listed higher in All Clusters coming in from above (since 2026-10-20; [Reduce motion](#preferences--app-reduce-motion) switches at once).
- ⏳ How long a cluster's client certificate is still valid, where you choose the cluster (since 2026-10-17): for a context that logs in with a certificate in its kubeconfig, the navigator's list says "cert expires in 23 days" under its name, in orange in its last 14 days (or the last fifth of a short-lived one's life), "expired" alone (the owner, 2026-10-17) in red once it ended; the welcome page and the tooltips say it too, the tooltips with the date. It never stops you from connecting. A certificate file in Documents, Downloads or on the Desktop shows after your first connect to it, so that listing your clusters never makes macOS ask for those folders.
- 🔌 Integrations per cluster (since 2026-10-17): the Metrics Server, Prometheus, Alertmanager, the Trivy Operator and Kyverno, each switched on or off in Cluster Settings, under the list's divider (Integrations for all five, and a section of its own each with its details), with whether Cogpit found it or it needs its address; a new cluster asks at its first connect, nothing looked for before you choose (Check all, Choose, Don't use), with a checkbox to keep the answer as the default and Ask again in its settings; the default checks everything.
- 📌 A hotbar of pinned clusters at the window's edge, one list that scrolls when it is full: F1–F12 open the first twelve; drag a tile to reorder, the others make room as it moves. Removing a kubeconfig source, or Remove from Cogpit, takes its clusters' tiles off too; a tile whose file went away by itself stays, marked missing, in case it comes back (since 2026-10-19).
- ➖ The "−" at the right of the navigator's Local Kubeconfigs row opens Kubeconfig Sources…, where you remove synced files and folders (since 2026-10-19; also in that row's menu and the Cluster menu).
- 🔀 Several clusters connected at once (since 2026-10-16): switching to another cluster leaves the one you were on connected, its logs, shells, port forwards and running jobs going on; switching back within five minutes shows it at once without asking the cluster again, later its lists load anew. A cluster's namespace filter, search and status filter stay with it and its tabs: the next cluster never comes with them. The top bar's three toggles show and hide the navigator (⌘B), the dock (⌘J) and the agent chat (⌘I); with no tab in the dock, ⌘J opens a terminal there. A cluster stays connected until you disconnect it or quit; agents may work on any connected cluster. Every dot of a cluster says how it is: filled while live, a ring while it rests in the background, blinking while it reconnects, red when it needs you; the dock shows the tabs of the cluster you look at ([Several clusters connected](#several-clusters-connected)). A cluster's Settings can keep its login program from running while you look at another cluster (its login then waits, renewed when you show it), and Preferences › App can bring the last session back at start: its tabs and its clusters, connected again.
- 🪪 One short name per cluster wherever the window names it: the name you gave it in its Settings, else its context's name after the last "@" ("kubernetes-admin@prod-eu" shows as "prod-eu"); the full name in every tooltip, and the navigator's search finds it by either. The navigator's root says who you are logged in as there, as the API server confirms it at every connect (`kubectl auth whoami`'s question), your groups in the tooltip of the small box beside it; "(kubeconfig)" marks the kubeconfig's user name where the API server does not say, and while the cluster connects the last user it confirmed shows dimmed, with when it was last seen ([D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)).
- 🏷️ The cluster's Kubernetes version under your user in the navigator's root, ⚠ when upstream's support for its minor has ended, in amber during its last two months (maintenance mode); its tooltip says until when, with the days left. The Nodes list marks a kubelet the same way, and one newer than the API server or more than three minors behind it ([The Kubernetes version and upstream support](#the-kubernetes-version-and-upstream-support)). A gear beside the root's dot opens the cluster's Settings.
- ⌨️ A command palette on ⌘⇧P (Ctrl+Shift+P): every action the menus offer, Switch to any view, the hotbar's clusters; ⌘P (Cluster › Connect to Cluster…) to search a cluster by name in the navigator's list, ↵ to connect, Escape to go back.
- 👋 A welcome page whenever no cluster is open (and from Help › Welcome): the clusters of your hotbar and the recent ones one click away (on a first start the clusters your kubeconfigs offer; All Clusters beside it lists every one), Add Kubeconfigs, the documentation; what there is to say about the connection (closed by Cogpit, a failed connect, the cluster connected under it) in a banner whose colour, icon and title name its level. Disconnect a cluster, or remove it from Cogpit, which unlinks the kubeconfig file you added and never touches the file.
- 🩺 A connect that fails shows its own error page (since 2026-10-22): why, in a sentence, and the connect stage by stage (the kubeconfig, a login program, DNS with the addresses, TCP to the address and port, TLS, the API server's answer), with Connect Again and Show Connection Updates.
- 🪟 The window opens where you closed it (since 2026-10-18): as large, on the same display, in full screen if it was, whatever Restore the last session says; a display that is gone gives 1440 × 900 in the middle of the main one ([details](#the-windows-place-and-size); Windows and Linux: the size and full screen).
- 🗂️ Tabs you arrange (since 2026-10-20): drag a tab along the tab bar, or the dock's, to another place, the others making room as it moves; ⌘⌥⇧← and ⌘⌥⇧→ move the shown tab one place, the dock's while you work in the dock (since 2026-10-22); the tab bar's order comes back with your tabs at the next start.
- ⚙️ Preferences on ⌘, and a cluster's Settings on ⌘⇧,, each a card over the whole window with Cogpit dimmed behind it (since 2026-10-23; a cluster's titled "Cluster Settings: <name>" since 2026-10-24; close it with its × at the top right, Escape or a click beside it; a value you just typed is kept): the theme switched live (also with the sun and moon left of the bell in the title bar, since 2026-10-21), the time zone, the editor's and the terminal's settings, a cluster's name and icon (up to five letters of yours on a colour, or a part of your image you place), its terminals' folder and namespace, its Prometheus and how it is asked (a query format, empty containers filtered, HTTPS); never written to a kubeconfig.
- 🕐 Every timestamp in your time zone and your region's format, as your Mac's region settings write it ("10.09.2026 17:41:01" in Germany): the drawer, events, logs, notifications, charts.
- 🔐 Your credentials stay yours: kubectl's own authentication code (certificates, tokens, exec credential plugins); no local proxy, no login.
- ⚡ Cache first: views read from a watch-fed cache; switching back to a view costs no API request.
- 📋 Lens's resource views: workloads, config, network, storage, access control, device resources (with each node's devices in its drawer), custom resources; each searchable by words, with match case or a regular expression, and filtered by the states its rows carry (Running, Pending, Not Ready, Degraded…), several at once, cleared with one click; the namespace filter's list has a search field to find your namespaces by typing, a click on a name shows only that namespace, its box adds it to the others. A namespace you filter by that is deleted, or being deleted, reads red there with "deleted" or "terminating", and the filter's button turns red within a second of the deletion, whether or not anything in the list changed (since 2026-10-22); untick a deleted one and it leaves the list. A click on the empty area below a list's rows closes the drawer; the rows you checked stay checked.
- 🔏 The certificates in your Secrets decoded in the drawer, PEM or DER, a section per certificate of a chain: subject, issuer, serial, validity (a warning 30 days before it ends), names, key, usages, fingerprint, each field copyable; whether `tls.key` belongs to `tls.crt` and the chain is in order; agents read the same fields, never the PEM text or the private key.
- 📜 Logs with follow, filter, timestamps, the previous container, pod and container selectors, older lines and a jump to a time; select their text across lines and copy it (since 2026-10-19: a drag, ⇧-click, a double click, ⌘A, ⌘C, a right-click's Copy All Lines Shown; [details](#selecting-and-copying-a-logs-a-notifications-and-the-agents-text)).
- 🔔 Notifications as toasts at the bottom right, and the session's in the bell's list: a toast stays while the pointer is on it or you select in it; drag over its text and ⌘C copies, its copy icon left of the × copies the whole message, an error included (since 2026-10-19).
- 💻 Pod shells, attach and a terminal bound to the cluster's context; copy and paste by keys or, if you choose, the right mouse button. A shell, terminal, log or editor you open (⌘⇧T, ⋮ › Edit, Logs, a click on its tab) has the keys at once, a shell once it is connected (since 2026-10-22); one an agent opens leaves them where you type.
- 🖥️ A node shell, as Lens opens it: a privileged pod in `kube-system` from an image pinned by digest, confirmed every time, deleted when the shell closes.
- 🔍 Debug a running pod: Debug in its drawer adds an ephemeral container (by default nicolaka/netshoot:v0.16, pinned by digest, your choice in Preferences) that sees the processes of the container you pick and opens a shell in it; confirmed every time, since it stays in the pod; the Pods list shows such containers as blue squares, outlined once they ended, and Logs, Shell and Attach reach them ([details](#debug-containers)).
- 🔐 Create Secret from a dialog, values masked as you type, for you alone: agents can neither create a Secret nor read its values.
- 🗝️ A Secret's values and keys edited in its drawer, for you alone: Show puts a value into an editor, whose copy icon copies the whole value (the clipboard is cleared again after 60 seconds); each key's line ends with Show (amber, with an eye: it exposes the value), the copy icon and the bin icon, always shown; a masked value's copy icon copies it without showing it, straight into a password field elsewhere (since 2026-10-22), Add Key adds a key, each key's bin icon removes it, pills beside Revert and Save count what you edited, added and removed; Save shows which keys change, are added or are removed and their sizes, never a value, checks with the server first and, for a removed key, is your confirmation; agents can neither save nor review a Secret's values, and unsaved ones never touch the disk.
- 🧰 The terminal's `kubectl` checked against the cluster: more than one minor version off, or none, and one click downloads the matching release from `dl.k8s.io`, verified against its SHA-256, the Kubernetes release's signature and that signature's entry in sigstore's transparency log, for Cogpit's terminals only.
- ✏️ YAML editing with a diff and a server-side dry-run before every apply; every object's YAML reads as the Kubernetes docs write a manifest, apiVersion, kind and metadata (name and namespace first) on top, status last; ⌘Z undoes and ⇧⌘Z or ⌘Y redo your typing, pastes and indents in every YAML editor, the drawer's values included, and ⌘Z right after Revert to Original brings your text back; ⌘F opens the find bar of the editor tab you work in (Edit, Create, Helm Install and Upgrade), with the keys in its field; an apply that scales a workload to zero or removes a key of a Config Map or a Secret asks you first, naming the keys.
- 📝 A Config Map's values edited right in its drawer, each key above its editor, keys added with Add Key and removed with their bin icon, what you edited, added and removed counted in coloured pills beside Revert and Save, saved with a diff and a dry run (a removal named in red and confirmed by that review, an agent's always asked); when someone else changed other keys meanwhile, Save moves your edits onto the newer version and shows you the diff again; unsaved values survive a quit or a crash as a draft.
- ☑️ Delete, Restart and the Flux and Argo CD actions on the rows you checked (each view keeps its own checked rows and has them again when you come back to it in the same tab, without objects deleted meanwhile), up to 500 at once, after one confirmation that names them all (every confirmation is a table, Context › Namespace › Objects, what it does and who asked; "This cannot be undone." in red where something is lost); more than ten run in the background in a dock tab of their own, which shows each object as it is done or fails, counts them, and has Cancel to stop what is not done yet (nothing done is undone); a toast says how it ended. Agents follow the same tab in `ui_state` and may cancel it too.
- 🚧 Drain a node in the background, after its confirmation: a dock tab shows the cordon and each pod as it is evicted, which one a PodDisruptionBudget holds and why (the drain retries it until its timeout), and has Cancel. A drain that is cancelled, fails or is cut off by a lost connection or a crash leaves the node cordoned and partly drained, and a banner offers Finish drain, Uncordon and Dismiss; nothing is uncordoned by itself. An agent's drain goes on when the agent's call ends.
- 🎚️ Scale a Deployment, Stateful Set or Replica Set with a slider from 1 to 30, or type any other count beside it (0 stops every pod and is confirmed first); in Scale's and Drain's dialogs each field is as wide as what it holds, and the "?" after its name shows what it does when you point at the name or click it; every dropdown opens over its field with the current choice checked where the field is, as on macOS (since 2026-10-20).
- 🔌 Port forwarding, one click to the browser.
- ⎈ The Helm releases in your clusters (Lens's Helm view), kept in Secrets or ConfigMaps: history, values, upgrade, rollback, through the Helm SDK, no `helm` binary needed.
- 📦 Charts from the repositories in Helm's own `repositories.yaml` (add them with `helm repo add`; Cogpit picks a change up within seconds, logins and CA files included, and never writes the file), installed from a Helm Install tab; a chart's drawer shows the README of the version you pick, and charts show their icons (switch them off in Preferences).
- 🔁 Flux CD: its section in the navigator (Helm releases, Kustomizations, sources, image automation, notifications) with each object's state, revision and message, a dashboard of what is not ready whose states open the kind's list filtered to them, and Reconcile, Reconcile with Source (which waits for the source, as the flux CLI does, in a dock tab of its own), Reset, Suspend and Resume, on a selection too.
- 🐙 Argo CD: its section in the navigator (Applications, Application Sets, Projects, and the repositories and clusters without their credentials), a dashboard of the Applications' health and sync state, Sync (with prune) and Refresh (normal or hard) through the Kubernetes API, on a selection too and within the project's sync windows, an Application's logs in one tab, and Argo CD's own UI through a port forward.
- 🧪 Your images' vulnerability grades from the Trivy Operator your cluster runs (since 2026-10-17, as grades since 2026-10-22): a shield from S to E. S: nothing found and an OS that still gets security updates; A: only Low or Unknown, at most 10; B: Medium, or more Low; C: High, or an OS at its end of life; D: 1 to 5 Critical; E: more; a grey N/A where an image has no report. The Pods list shows each pod's worst grade in the narrow column after Containers (sort by it to find the worst first; hover it for the counts), and each container in a pod's drawer shows its image's grade after its name and, under its details, a card with the rule, the counts per severity as a bar, the OS, when it was scanned and a link to the report. With the cluster's Trivy integration switched off (D-76's switch per cluster, on by default), nothing of it is read or shown. Cogpit installs no scanner.
- 📈 CPU and memory from the cluster's metrics-server and its Prometheus: columns in the Pods and Nodes lists, charts with requests and limits in the pod and node drawers, and the cluster Overview: CPU, Memory and Pods of the master or worker nodes over a time range, with the Issues (pods, nodes and workloads in trouble, warning events) below. A node's history, its network and its filesystem come from node-exporter where it runs, and so do its disks: a small usage bar per filesystem it mounts in its drawer (the root first, then a data disk such as `/data`; the numbers in each bar's tooltip), the fullest of them in the Nodes list's Disk column. Cogpit installs nothing into your cluster for this: it finds the Prometheus and the exporters the cluster runs (your own charts, kube-prometheus-stack, the `prometheus` chart under any release name, and since 2026-10-16 night Thanos's Querier and VictoriaMetrics, single-node or the cluster's vmselect, by their usual service names) and reads them; where it finds no provider, the charts say so. A cluster that runs a Prometheus and a Thanos or VictoriaMetrics reads the Prometheus; name the other in Cluster Settings › Prometheus › Service address (vmselect with the path prefix `/select/<tenant>/prometheus`). Under the charts one row says where the numbers come from, "Prometheus ● 30 s ago" left of the row's centre and "Metrics Server ● now" right of it: the dot green while it answers, red while it does not or may not be read, grey while it is not found, a ring while nothing has asked it yet; its tooltip has the details. Only while one is not reachable does the row link to its settings ("Set its address…", "Settings…"); a cluster whose integrations are not chosen yet offers "Choose integrations…".
- 🚨 The cluster's alerts from its Alertmanager (since 2026-10-17): a pill in the title bar, red from a critical alert, orange from a warning, yellow from an info alert, with the number of every active alert; a click opens the alert board, the alerts grouped by severity and alert name, each with its summary, since when it fires, its runbook and the object it is about one click away. Found by its service's name, or set in the cluster's Settings; read only.
- 🛂 Kyverno's policy violations on your objects (since 2026-10-17): while the Kyverno integration is on for a cluster, every object a Kyverno policy report names with a failed rule, of an Audit policy or of an Enforce policy on an object from before it, shows the warning icon in its list ("Kyverno: 2 policy violations (require-team-label, …)") and a "Policy violations" group in its drawer; agents list them all with `kyverno_violations`. Cogpit reads the reports Kyverno writes and installs nothing ([Kyverno's policy violations](#kyvernos-policy-violations)).
- 🧹 A namespace stuck in Terminating says why (since 2026-10-19): its drawer lists the objects whose finalizers hold it, each a link with its finalizers, as Kubernetes' namespace controller names their kinds; kinds you may not list there are counted, an unavailable API service is named with its Service; an object vanishes from the list once its finalizers go, and the namespace then goes by itself. Remove Finalizers on one object, confirmed, naming the finalizers and the cleanup it skips ([below](#a-namespace-stuck-in-terminating)).
- 🛡️ Never surprises a cluster: destructive actions confirm, nothing is installed or written implicitly; every confirmation is a table you read at a glance, the place as Context › Namespace › Object, the object as "Config Map → walk-cm", what the command does a row each ("Removes  level" in red), never a sentence.
- 😴 Sleep and lost connections: when the Mac wakes, the network or a VPN changes, or the cluster stops answering, Cogpit notices within seconds, blinks the cluster's dot and shows a banner, keeps your views and tabs as they were, disables changes, and reconnects by itself; a login that expired asks you once ([below](#when-the-connection-breaks)).
- 🤖 Fully controllable over MCP, the UI included: an agent sees what you see and navigates for you.
- ⌨️ `cogpit-cli`, inside the app: your contexts and any kind's objects on the command line, read-only, and an MCP stdio bridge you register once.
- 💬 A built-in agent in a chat beside your views (⌘I), on any OpenAI-compatible endpoint such as LM Studio, LiteLLM or Open WebUI.
- 📦 Installs the Mac way: open the disk image, drag Cogpit into Applications, signed and notarized (since v0.6.0); *(planned)* a Homebrew cask for shell users.
- ⬆️ Updates with one click in the macOS app (Sparkle), verified against a signed update feed: the releases carry the update key since v0.11.0, Pages serves the signed feed since 2026-10-16 and the first installed copy updated through it that day. On Linux (since 2026-10-20 night, D-108) apt updates Cogpit from its signed APT repository: the Software Updater and `apt upgrade` offer each release, and Cogpit's Update button installs it after an administrator's password, then reads Restart to Update; whether updates install by themselves is the system's setting, never Cogpit's ([updates on Linux](#updates-on-linux)). On Windows (from the first release after the branch `windows-release` is merged, D-111) WinSparkle updates the setup's installation from a feed signed with the same update key: a click on the Update button downloads the setup, checks its signature, installs it and restarts Cogpit ([updates on Windows](#updates-on-windows)).

## Get it and run it

### Download a release

Every [release](https://github.com/guided-traffic/cogpit/releases) carries the complete product
only ([D-16](decisions.md#d-16-the-first-real-release)): Cogpit for Apple silicon as
`Cogpit-<version>-macos-arm64.dmg`, from the release after v0.32.0 on the Linux packages
`cogpit_<version>_amd64.deb` and `cogpit_<version>_arm64.deb` beside it, each with both binaries
([Install on Linux](#install-on-linux), [D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)),
from the first release after the branch `windows-release` is merged the Windows setup
`Cogpit-<version>-windows-x64-setup.exe` with both executables
([Install on Windows](#install-on-windows), [D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)),
and one `SHA256SUMS` over all of them. Open the disk image and drag Cogpit into
Applications; `cogpit-cli` comes inside the app ([`cogpit-cli`](#cogpit-cli)). v0.6.0, v0.7.0 and
v0.8.0 carry these files; v0.5.0 and v0.9.0 to v0.12.0 have none, their signing or notarization
having not finished ([releasing](releasing.md#when-signing-or-notarization-fails)), and v0.13.0's
pipeline was still waiting for Apple's notarization on 2026-10-15. The app with its
`cogpit-cli` and the disk image are signed with the project's Developer ID and notarized by Apple,
so macOS starts them without a warning
([releasing › signing and notarization](releasing.md#signing-and-notarization)). v0.1.0 to v0.4.0
carry `Cogpit_<version>_darwin_arm64.zip` instead, **unsigned**: macOS blocks that app's first
start until you allow it under System Settings › Privacy & Security › Open Anyway. Releases up to
v0.6.0 also carry `cogpit-cli` archives for Linux, macOS and Windows, those for Linux and Windows
not signed ([updates](../security/updates.md#what-a-release-holds-today)); later releases carry
none: from the release after v0.32.0 on, Linux gets `cogpit-cli` in its package with the app, and
from the first release after the branch `windows-release` is merged Windows gets `cogpit-cli.exe`
from the setup, beside `cogpit.exe`.

```sh
shasum -a 256 -c SHA256SUMS
spctl -a -t open --context context:primary-signature -vv Cogpit-<version>-macos-arm64.dmg   # source=Notarized Developer ID
/Applications/Cogpit.app/Contents/MacOS/cogpit-cli version   # once the app is in Applications
```

The window says which version runs: right of the wordmark "Cogpit" in the top bar, smaller, on its line,
a release shows `v0.16.0` (# example), a build from source `dev`, its commit in the tooltip
(`dev 0691fb32abcd` # example, `-dirty` when the tree had changes; since 2026-10-16, the owner's
F12). About Cogpit says the same. A release is built with Fyne's thread check off (since v0.30.0;
fast scrolling stays at 60 frames per second), a build from source with it on ([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)):
About adds "Fyne thread check on" to a release built with it and "Fyne thread check off" to a build
from source without it, and the app log names the mode at every start
(`cogpit started version=… fyneThreadCheck="off (speed)"` # example). A build from source with the
check on shows each place where the window's code called Fyne off its main goroutine once, as a
warning toast naming the file and line; a release keeps such lines in the app log only.

### Install on Linux

Since 2026-10-20 night ([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages),
the owner's request of that evening), the release pipeline builds Cogpit for Linux as a Debian
package, for amd64 and arm64; the first release that carries it is the one after v0.32.0. It runs
on Debian 13 and Ubuntu 24.04 and later (it is built on Ubuntu 24.04 and needs glibc 2.38), in an
X11 or a Wayland session. Download the package for your machine's architecture
(`dpkg --print-architecture`) from the [release](https://github.com/guided-traffic/cogpit/releases)
and install it with apt, which fetches the libraries it needs:

```sh
sha256sum --ignore-missing -c SHA256SUMS                 # the release's checksums, the downloaded files only
sudo apt install ./cogpit_<version>_amd64.deb            # the ./ makes apt read the file
cogpit-cli version                                       # cogpit-cli <version> (go1.27.3, linux/amd64)
```

Cogpit is then in the desktop's list of applications (its launcher
`io.guided-traffic.cogpit.desktop`), and `cogpit` starts it from a terminal. The package also sets
up Cogpit's own APT repository, `https://guided-traffic.github.io/cogpit/apt/` (suite `stable`,
component `main`), so after `sudo apt update` apt knows Cogpit's releases: `apt upgrade` and the
Software Updater offer each new one with the system's other updates, and Cogpit's Update button
installs it ([updates on Linux](#updates-on-linux)). apt checks every file of the
repository against its signing key, which the sources file carries inline (`Signed-By`): Ed25519,
fingerprint `BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`, valid until 2031-10-19, its public
half also in the repository at
[`packaging/linux/cogpit-archive-keyring.asc`](../../packaging/linux/cogpit-archive-keyring.asc)
(`gpg --show-keys` on it prints the fingerprint). The repository holds the two newest releases of
each architecture. Trust chain: [security › updates › Linux](../security/updates.md#linux); the
pipeline: [releasing › Linux](releasing.md#linux).

**Updates without asking are your setting, not Cogpit's** (the owner, 2026-10-20: Cogpit serves
users in general and he does not decide for them). The package installs nothing that upgrades by
itself. Ubuntu's Software Updater shows Cogpit's releases as it shows every repository's;
unattended-upgrades installs only the origins its `Unattended-Upgrade::Origins-Pattern` names, and
Cogpit's repository says `Origin: Cogpit`, `Suite: stable` (`"origin=Cogpit,suite=stable";` # example,
not tried). An upgrade replaces the files under a running Cogpit, so its next check offers
Restart to Update.

| File the package installs | What it is |
|---|---|
| `/usr/bin/cogpit`, `/usr/bin/cogpit-cli` | the app and the command-line tool, built as a release builds them: the tag `nodemo` (no demo cluster), Fyne's thread check off, position-independent |
| `/usr/share/applications/io.guided-traffic.cogpit.desktop` | the launcher (`StartupWMClass=Cogpit`; on Wayland the window's app id is `io.guided-traffic.cogpit`) |
| `/usr/share/icons/hicolor/<n>x<n>/apps/io.guided-traffic.cogpit.png` | the icon, 16 to 512 pixels |
| `/usr/share/metainfo/io.guided-traffic.cogpit.metainfo.xml` | the AppStream entry software centres read |
| `/usr/libexec/cogpit/update` | the update helper the Update button runs as root through `pkexec`: it refreshes the list of Cogpit's repository only and upgrades the package `cogpit` only, keeping a sources file you changed |
| `/usr/share/polkit-1/actions/io.guided-traffic.cogpit.update.policy` | the helper's polkit action `io.guided-traffic.cogpit.update`: an administrator's password, kept for a few minutes |
| `/etc/apt/sources.list.d/cogpit.sources` | Cogpit's APT repository with its key inline; a configuration file of the package: delete it and upgrades keep it deleted (apt then knows no new release, and Cogpit's Help › Check for Updates… is greyed "no package repository"); `apt purge` removes it |
| `/usr/share/doc/cogpit/` | the copyright and a changelog |

It depends on the libraries the app links, the X11 and Wayland libraries its window loads at run
time, and libdecor with its GTK plugin, which draws the title bar with minimize, maximize and close
on a Wayland desktop that draws none and runs no XWayland; on GNOME with XWayland (Ubuntu's
default) Cogpit's window is an X11 one and GNOME draws its title bar. It recommends `pkexec`
(without it the Update button cannot install, apt and the Software Updater still can) and has no
maintainer scripts. Your settings live in `~/.config/Cogpit` and the logs and
caches in `~/.cache/Cogpit` ([naming conventions](#naming-conventions)).

```sh
sudo apt remove cogpit     # removes the app; keeps the sources file, so apt update still reads the repository
sudo apt purge cogpit      # also removes /etc/apt/sources.list.d/cogpit.sources
rm -rf ~/.config/Cogpit ~/.cache/Cogpit   # your settings, logs and caches, if you want them gone too
```

The window has two bars: the desktop's title bar with its buttons, and Cogpit's bar with the menus
(Cluster, View, Helm, Window, Help) before the logo, since the release after v0.34.1 (the owner,
[D-109](decisions.md#d-109-the-menus-in-the-top-bar-on-windows-and-linux); before, Fyne's menu bar
was a row of its own between them). The menus' keys work from any focus, a text field's too; Alt
does not open the menus, the command palette (Ctrl+Shift+P) reaches every command.

Not on Linux yet: `cogpit://` links, Start at login, the window's place (its size and full screen
come back, D-95) and the system's file panels (Fyne's dialogs instead). The MCP details offer the
stdio registration of `/usr/bin/cogpit-cli` since the release after v0.33.0 (0.33.0 offers the HTTP
one; `claude mcp add --scope user cogpit -- /usr/bin/cogpit-cli mcp` registers the bridge by hand). No
rpm, AppImage, Flatpak or tarball;
not tried yet by a person on a desktop: polkit's password dialog, the restart, an update through
the Software Updater, the arm64 package beyond GitHub's ARM runner.

### Install on Windows

Since 2026-10-21 ([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle),
the owner's request of that day), Cogpit for Windows is a setup,
`Cogpit-<version>-windows-x64-setup.exe`, built on the branch `windows-release`; the first release
that carries it is the first after that branch is merged. It runs on Windows 10 1809 (build 17763)
and later and on Windows 11, x64; Windows 11 on ARM runs it emulated. Download it and
`SHA256SUMS` from the [release](https://github.com/guided-traffic/cogpit/releases) and compare the
digests in PowerShell (the file writes it in lower case, PowerShell in upper case):

```powershell
(Get-FileHash .\Cogpit-<version>-windows-x64-setup.exe -Algorithm SHA256).Hash
Select-String windows-x64-setup .\SHA256SUMS
```

The setup is **not signed with Authenticode yet**
([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)), so
Windows SmartScreen says "Windows protected your PC" when you first open it: More info, then Run
anyway. Updates are vouched for by Cogpit's update key instead: WinSparkle runs only a setup that
key signed ([updates on Windows](#updates-on-windows)).

```text
Cogpit-<version>-windows-x64-setup.exe                         for you only, %LOCALAPPDATA%\Programs\Cogpit, no administrator # default
Cogpit-<version>-windows-x64-setup.exe /allusers               for every user, C:\Program Files\Cogpit (UAC asks)
Cogpit-<version>-windows-x64-setup.exe /S /D=D:\Tools\Cogpit   silently, into a folder of your own (/D= comes last) # example
```

An installation is updated where it is and as it was installed (for you or for every user). The
setup waits for a running Cogpit to quit (a minute silently, then it asks), and moves a running
`cogpit-cli.exe` aside, an agent's MCP bridge that may run for hours; a later setup or the
uninstaller removes it.

| File the setup installs into its folder | What it is |
|---|---|
| `cogpit.exe` | the app, a GUI program (no console window opens), built as a release builds it: the tag `nodemo` (no demo cluster), Fyne's thread check off, the Windows feed's URL and the update key's public half; its icon and version in Explorer's Properties |
| `cogpit-cli.exe` | the command-line tool and MCP stdio bridge ([`cogpit-cli`](#cogpit-cli)); not put on your `PATH` |
| `WinSparkle.dll` | WinSparkle 0.9.4, the updater `cogpit.exe` loads from its own folder by its full path; pinned by the SHA-256 of its release archive, unsigned as WinSparkle ships it ([H-140](../security/updates.md#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)) |
| `LICENSE.txt`, `THIRD-PARTY-NOTICES.txt` | Cogpit's licence; the third-party code in the binaries, WinSparkle's and expat's licences |
| `uninstall.exe` | the uninstaller (`/S` silently) |

and the Start menu entry Cogpit and the entry Settings › Apps lists (its uninstall entry
`io.guided-traffic.cogpit`, in your registry or, for every user, the machine's). Your settings live
in `%APPDATA%\Cogpit` and the logs and caches in `%LOCALAPPDATA%\Cogpit`
([naming conventions](#naming-conventions)). Uninstalling, from Settings › Apps, removes the program, its entries and WinSparkle's state
(`HKCU\Software\guided-traffic\Cogpit\WinSparkle`), and keeps your files:

```powershell
Remove-Item -Recurse "$env:APPDATA\Cogpit", "$env:LOCALAPPDATA\Cogpit"   # your settings, logs and caches, if you want them gone too
```

The window has Windows' own title bar with its three buttons, and below it Cogpit's bar with the
menus (Cluster, View, Helm, Window, Help) before the logo
([D-109](decisions.md#d-109-the-menus-in-the-top-bar-on-windows-and-linux)); it follows each
monitor's scaling. In a release the MCP details offer the stdio registration of the
`cogpit-cli.exe` beside `cogpit.exe`, written for PowerShell ([Drive it with an agent](#run-the-mvp)).

**The terminal on Windows** (built 2026-10-21, D-111). A local terminal runs the shell Preferences
› Terminal names, else PowerShell 7 (`pwsh.exe` on `PATH`), else Windows PowerShell
(`%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe`), else `%ComSpec%` (cmd.exe); the
field's placeholder shows which one. `$SHELL` is not read on Windows (Git Bash sets a POSIX path
there that Windows cannot start), and no login shell is asked for: PowerShell and cmd read their
profiles at every start. The shell runs on Windows' pseudo console (ConPTY). Its own kubeconfig binds
the cluster's context and namespace as on macOS, `KUBECONFIG=<temp file>;<your kubeconfig files>`
joined with `;` ([why](../security/credentials.md#the-terminals-kubeconfig)). When Cogpit puts a
`kubectl` first ([kubectl for terminals](#preferences--kubernetes-kubectl-for-terminals)), a file
named `kubectl.exe` has its folder put first on the terminal's `PATH`, no file written (any other
program in that folder comes first too; Cogpit's download folders hold kubectl alone); a kubectl by
another name gets a hard link named `kubectl.exe` in the terminal's own folder, else a copy.
**Enforce kubectl does nothing on Windows yet**: its shell function exists for zsh, bash and fish,
not for PowerShell or cmd, and the switch still shows. **Closing the terminal ends every program
started from it**, windowed ones too (a Notepad, the first `code .`), as closing a console window
does: the shell and what it starts run in a job of their own; a program that asks to leave the job
may, as a program on macOS or Linux may leave the terminal's session. Tested by the session's tests
on Windows; not looked at yet: a terminal tab in the real window (the console's first output
carries `ESC[?9001h`, `ESC[?1004h` and OSC 0 titles, which the widget has not been seen to handle
there), PowerShell 7, Windows 10 and ARM.

**Not on Windows yet:** the Authenticode signature (H-139); a setup for ARM64; winget and Scoop;
`cogpit://` links and Start at login; the system's file dialogs (the choosers are Fyne's, which
speak Windows' language, "Speichern" on a German Windows, while Cogpit speaks English, and the
folder chooser lists no drives); the region's date and time format (Windows' region settings are
not read: Cogpit writes ISO 8601 unless `LC_ALL`, `LC_TIME` or `LANG` names a locale); the window's place (its size and full screen come back,
[D-95](decisions.md#d-95-the-window-comes-back-where-it-was)); `~\` in a path field (a terminal's
folder, a CA file: only `~/` stands for your home folder). Not tried yet: an installation for every
user through UAC, Windows 10, an ARM machine, a release through the pipeline.

### Run the MVP

The MVP is a local build for macOS (tested on Apple silicon, macOS 26). It needs Go (1.26 or
later; the build fetches Go 1.27.3 itself), the Xcode Command Line Tools (Fyne uses cgo), and for
the kind cluster Docker, `kind` and `kubectl`. On Debian or Ubuntu a C compiler and the GL, X11
and Wayland headers take the Command Line Tools' place, as CI installs them (`sudo apt install
--no-install-recommends gcc libc6-dev make pkgconf libgl-dev libegl-dev libx11-dev libxcursor-dev
libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev libxkbcommon-dev libwayland-dev`, D-112), and
`make dist-linux VERSION=x.y.z` builds the Debian package for the machine's architecture in
`dist/linux` (with `dpkg-dev`; since 2026-10-20 night, D-108); `make app` is macOS only. On
Windows, Git Bash or MSYS2's shell with MSYS2's MinGW-w64 gcc and make (as CI has them) builds and
tests Cogpit (`make build`, `make test-windows-unit`, `make test-windows-ui`), and
`make dist-windows VERSION=x.y.z` builds the setup in `dist/windows` with NSIS 3 (since 2026-10-21,
D-111; [releasing › Windows](releasing.md#windows)).

```sh
git clone https://github.com/guided-traffic/cogpit.git
cd Cogpit

make run-demo                 # synthetic cluster: 10,000 pods, 1,000 watch events per second (development builds only)

make kind-up                  # a kind cluster "cogpit" with its own kubeconfig (.cache/kind/kubeconfig)
make run                      # Cogpit connected to it; make kind-down removes it

make build && bin/cogpit      # your own clusters: syncs ~/.kube (config included) and offers the files $KUBECONFIG names, each file on its own
make app                      # bin/Cogpit.app (ad-hoc signed), starts by double-click
```

The demo cluster is for development: a build from source (`make build`, `make app`, `go run
./cmd/cogpit`) offers it as the context `demo` on the welcome page and in the navigator, a release
leaves it out. The release pipeline builds with the tag `nodemo` (the Makefile's `DIST_TAGS`), so
the release's binaries contain neither the demo cluster nor its flags; `make build
GO_TAGS=nodemo` builds the same variant from source (owner, 2026-10-16,
[D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)).

`make kind-up` seeds the namespace `cogpit-demo` ([hack/kind](../../hack/kind/README.md)): deployments
`web` and `api`, a crash-looping pod, a completed job, a `toolbox` pod with a shell, and the Service
`web` with the ClusterIP `10.96.100.100`. Nothing writes `~/.kube/config`; `make run` lists your `~/.kube` contexts too (Kubeconfig Sources removes the folder from the list, never from disk).

**Drive it with an agent.** Cogpit's external MCP server, the endpoint other programs on this
computer use, is **opt-in: off until you switch it on**, on a new installation too (the owner's
decision of 2026-10-21, [D-49](decisions.md#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)).
Click the MCP item at the right end of the status bar, muted while the server is off, to switch it
on; an installation where you already switched it on keeps it on, and one where you never touched
the item has it off since that change. The gear beside the item opens the MCP details: the
server's switch, its endpoint, its token (masked until you click Show), the registration commands
with a copy button each, and Regenerate token
([D-53](decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it)); the
endpoint, the token and the commands appear once the server runs.
Help › Copy MCP command copies the command and opens them too (it is disabled while the server is
off), and Cogpit prints it to the terminal that started it whenever the server starts. The
installed app offers the registration of the `cogpit-cli` inside it, which you run once
([D-2](decisions.md#d-2-agent-interface-scope)):

```sh
claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user cogpit -- /Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp   # example
```

On Windows the commands are written for PowerShell, which Windows Terminal opens (since 2026-10-21,
D-111): the output goes to `*> $null`, the commands are joined with `;` (Windows PowerShell 5.1
knows no `&&`), and a path that needs quoting is put in single quotes, a single quote in it doubled
(the typographic ones too, which PowerShell reads as quotes). A release offers the stdio
registration of the `cogpit-cli.exe` the setup installed beside `cogpit.exe`; a build from source
(`bin\cogpit.exe`, which has a `cogpit-cli.exe` beside it too) offers the HTTP one:

```powershell
claude mcp remove --scope user cogpit *> $null; claude mcp add --scope user cogpit -- C:\Users\you\AppData\Local\Programs\Cogpit\cogpit-cli.exe mcp   # example
claude mcp remove --scope user cogpit *> $null; claude mcp add --scope user cogpit -- 'C:\Program Files\Cogpit\cogpit-cli.exe' mcp                  # example: for every user
```

Claude Code then starts that `cogpit-cli mcp`, which reads the running app's port and token
itself before every call, so the registration follows every restart and update of Cogpit and a
regenerated token; run it again only if you move `Cogpit.app`. Beside it, and alone in a build
from source (`bin/cogpit`) or the app while it still runs from its disk image, the details offer
the HTTP registration, with the URL and the token:

```sh
claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user --transport http cogpit http://127.0.0.1:<port>/mcp --header "Authorization: Bearer <token>"
claude mcp remove --scope user cogpit *> $null; claude mcp add --scope user --transport http cogpit http://127.0.0.1:<port>/mcp --header "Authorization: Bearer <token>"   # the same on Windows, in PowerShell
```

Cogpit keeps the port and the token across restarts and the switch, so you run it once, and again
after you regenerate the token or when Cogpit had to move to another port because the kept one was
taken (it says so). Its token sits in Claude Code's configuration and stays valid until you
regenerate it ([H-98](../security/agents-and-mcp.md#h-98-a-leaked-token-stays-valid-until-the-user-regenerates-it));
a source build's bridge holds none:
`claude mcp add --scope user cogpit -- "$PWD/bin/cogpit-cli" mcp` # example. Then ask Claude
Code "show me the Service with ClusterIP 10.96.100.100": Cogpit opens Services, selects `web` and
opens its drawer. Agents may read and navigate freely; Restart, Delete and the pod shell wait for
your confirmation in Cogpit's window ([agents and MCP](../security/agents-and-mcp.md)).

**Verify.** `make test-unit-race` runs every test without a cluster; with the kind cluster up,
`make test-kind` also runs the cluster tests (views, logs, pod shell, `kubectl` in the terminal).

**Not built yet (2026-10-21):** the Windows setup's Authenticode signature, a setup for Windows on
ARM, winget and Scoop ([Install on Windows](#install-on-windows)), Linux formats besides the `.deb`
(rpm, AppImage, Flatpak, a tarball), the app for Intel Macs, the Homebrew cask. Updates work on
macOS: the first installed copy updated from 0.15.0 to 0.16.0 on 2026-10-16; on Linux through apt,
from 0.33.0 to 0.34.0 on the owner's workstation (2026-10-20 night); on Windows through WinSparkle,
end to end with a throwaway key and a local feed (2026-10-21), not yet through a release
([Preferences › App: updates](#preferences--app-updates)). What exists and what is missing, row by
row: [docs/parity](../parity/README.md).

<details>
<summary>Clean up</summary>

```sh
make kind-down     # deletes the kind cluster and its kubeconfig file
make clean         # removes bin/ and dist/
rm -rf ~/Library/Caches/Cogpit "~/Library/Application Support/Cogpit"   # logs, the discovery and Helm caches, audit and connection logs, token, downloaded kubectl releases
rm -rf ~/.cache/Cogpit ~/.config/Cogpit                                 # the same on Linux
```

```powershell
Remove-Item -Recurse "$env:LOCALAPPDATA\Cogpit", "$env:APPDATA\Cogpit"   # the same on Windows
```

</details>

## Reference

### `cogpit` flags

| Flag | Default | Meaning |
|---|---|---|
| `--kubeconfig <files>` | empty: the files `$KUBECONFIG` lists # default | kubeconfig files, separated by `:`, offered as sources of their own for this run, each read alone and removable until the next start; `~/.kube` is synced either way (a bare `--context` name resolves to the one file that defines it) |
| `--context <name>` | empty: start on the welcome page, under the tabs of the last run, nothing connected # default | connect to this context at start; one Cogpit refuses (no such context) says why in a toast and shows the navigator's clusters with the search focused, as ⌘P does; a connect that fails says why on the welcome page |
| `--demo` | `false` # default | connect to the synthetic demo cluster; a development build's only, a release has no demo cluster and refuses the flag ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)) |
| `--demo-pods <n>` | `10000` # default | pods in the demo cluster; development builds only |
| `--demo-events <n>` | `1000` # default | watch events per second in the demo cluster; development builds only |
| `--mcp-addr <addr>` | `127.0.0.1:0` # default | MCP listen address; anything but a loopback address is refused. Port 0 binds the port Cogpit kept in `mcp-port` (a free one the first time, or when the kept one is taken, which it then keeps); a port given here binds that port for this run and is not kept |
| `--no-mcp` | `false` # default | leave the external MCP server and its status bar item out of this run; without it, the server is off until you switch it on by clicking the MCP item in the status bar (opt-in, D-49; the choice is remembered, and `--no-mcp` wins over it), or its gear for the MCP details |
| `--perf <duration>` | `0` # default | measure main-loop latency while auto-scrolling the table, print it as JSON and quit |
| `--perf-mode <mode>` | empty # default | with `--perf`: `scroll`; or `gate`, alone too: start-up, scrolling, every view and typing into 5 MB of YAML in one run (the [spike](spike-fyne.md)'s measurements) |
| `--cpuprofile <file>` | empty # default | write a CPU profile of the first `--cpuprofile-for` to this file |
| `--cpuprofile-for <duration>` | `5s` # default | how long `--cpuprofile` records |
| `--theme <mode>` | `dark` # example | `dark` (Dark Cockpit's night stage), `light` (its day stage) or `system`, for this run; without it the theme of Preferences › App (⌘,) applies, System (follow macOS) until changed (since 2026-10-14, [D-22](decisions.md#d-22-colour-theme)) |
| `-v` | `false` # default | debug logging |

Security note on `--mcp-addr`: the server checks `Host`, `Origin` and the bearer token, and binds
loopback addresses only, because any reachable listener would let others drive Cogpit with your
cluster identities.

### `cogpit-cli`

The headless command-line tool (pure Go, no window). It comes inside the app, beside the app's own
executable, from the first release after v0.6.0 on
([D-16](decisions.md#d-16-the-first-real-release)), and in the Linux package as
`/usr/bin/cogpit-cli`, on your `PATH`, from the release after v0.32.0 on
([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)), and from the
Windows setup as `cogpit-cli.exe` beside `cogpit.exe`, not on your `PATH`, from the first release
after the branch `windows-release` is merged
([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)); a source checkout builds it as
`bin/cogpit-cli` (`make build`):

```sh
/Applications/Cogpit.app/Contents/MacOS/cogpit-cli contexts                  # example: the installed app's
ln -s /Applications/Cogpit.app/Contents/MacOS/cogpit-cli ~/bin/cogpit-cli    # example: as cogpit-cli on your PATH; updates follow
```

It is signed and notarized with the app and replaced with it by an update; on Linux apt replaces it
with the package, on Windows the setup an update runs. Releases no longer carry it alone for any
system; until the first release with the Windows setup, build it from source there:
`CGO_ENABLED=0 go build ./cmd/cogpit-cli`. On Linux the MCP details offer the stdio registration of
the package's `/usr/bin/cogpit-cli` since the release after v0.33.0, as the macOS app offers its
bundle's, and on Windows a release's details offer the `cogpit-cli.exe` beside `cogpit.exe`, in
PowerShell's form ([Drive it with an agent](#run-the-mvp)). It offers the same contexts as the app, from
the same kubeconfig sources (`~/.kube`, the files `--kubeconfig` or `$KUBECONFIG` names, the files
and sync folders added in Cogpit), and runs a context's credential plugin, or connects a context
whose cluster sets `insecure-skip-tls-verify` (A28, since 2026-10-14), only once you allowed it in
Cogpit's window; it never asks itself and never writes Cogpit's sources or approvals. Exit
codes: `0` done, `1` the command failed, `2` the command line is wrong. `cogpit-cli <command>
--help` prints a command's flags.

| Command | Flags | What it does |
|---|---|---|
| `cogpit-cli contexts` | `--kubeconfig <files>` (empty: the files `$KUBECONFIG` lists # default), `-o table` # default (`json`, `yaml`), `-v` (`false` # default) | lists every context as `<context> [<file>]`, with its namespace, server and whether its login (a credential plugin, or a token file outside its folder) is `approved`, `required` or `changed`, and `not checked` under CERTIFICATE for a context whose cluster sets `insecure-skip-tls-verify` (Cogpit's default), followed for a source's context by whether you allowed connecting to it so (`not checked, required` # example; `approved`, `required` or `changed`, A28); since 2026-10-17 the last column, CLIENT CERT, says how long the client certificate the context's user logs in with is valid (`expires in 23 days`, `expired 3 days ago` # example; empty without one, [D-75](decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)); `json` and `yaml` are the `cluster_contexts` tool's result (`insecure: true`; `clientCert` with `notBefore`, `notAfter` and `state`); reads the kubeconfig files and those certificates only, never a key |
| `cogpit-cli get <kind> [name]` | `--context <context>` (required; an id as `contexts` lists it, `demo` in a development build too, or a context name only one file defines), `-n`/`--namespace <ns,...>` (empty: every namespace, for a named object the context's namespace # default), `-o table` # default (`json`, `yaml`), `--filter <text>` (empty # default), `--timeout 1m` # default, `--kubeconfig <files>`, `-v` | connects through a headless engine, waits for the cache's first list of the kind and prints it as Cogpit's view does (its columns, those empty in every row left out), then exits; `json` and `yaml` print the `resources_list` result for a list (at most 1,000 rows; stderr says when there are more) and the object's manifest for a named object (managed fields dropped, Secret values redacted). `<kind>` is a kind key (`pods`, `certificates.cert-manager.io`), or a Kubernetes kind or view title that names one kind (`Pod`). Connected to a context that skips the certificate check (`insecure-skip-tls-verify`, allowed once in Cogpit's window), it writes one line to stderr, `cogpit-cli: warning: TLS not checked for <context>. Certificate checks are off for this context …` (A29), and stdout stays the result |
| `cogpit-cli mcp` | `--headless` (`false` # default), `--kubeconfig <files>` (the headless engine only), `-v` | serves Cogpit's MCP tools over stdin and stdout for agents that speak only stdio: with Cogpit running, every call is forwarded to it (its confirmations and audit log apply; a restart of Cogpit, a new port and a regenerated token are followed); without it, or with `--headless`, an engine of its own serves them, and what needs your confirmation is refused. The external MCP server is opt-in (D-49): until you switched it on in Cogpit's window, and while you have it switched off, the bridge serves only a note that the server is off, `--headless` included. stdout carries the protocol only; logs go to stderr |
| `cogpit-cli version` | – | prints the version, the Go version and the platform (`--version` too): a release's `cogpit-cli 0.7.0 (go1.27.2, darwin/arm64)` # example, a development build's `dev` and its commit, `cogpit-cli dev 0691fb3277bb-dirty (…)` # example ([which build reports what](releasing.md#the-version-a-build-reports)) |

```sh
cogpit-cli contexts                                         # every context Cogpit offers
cogpit-cli get pods -n kube-system --kubeconfig .cache/kind/kubeconfig --context kind-cogpit   # example: kind's system pods
cogpit-cli get deployments web -n cogpit-demo -o yaml --kubeconfig .cache/kind/kubeconfig --context kind-cogpit   # example: one manifest
cogpit-cli get services --context demo                      # the synthetic demo cluster (a development build's bin/cogpit-cli)
```

Security note on `mcp`: the bridge sends the token from Cogpit's token file only to the loopback
URL in `mcp-endpoint`, reads both again before every call and sends nothing while there is no
endpoint file, follows no redirect and uses no proxy. Switching the MCP server off in the
status bar stops a bridge that forwards to the app, and a bridge started while it is off, or
before it was ever switched on (it is opt-in), serves no engine, headless included: the agent is
told the server is off (`mcp-switch`, D-49). A computer where the configuration directory is
unknown counts as off too. A run with `--no-mcp` leaves the switch as it is
([H-28](../security/agents-and-mcp.md#h-28-the-status-bars-switch-does-not-reach-a-headless-bridge)).
Without a window, the only way to switch the server on is by hand, since a command for it would be
one an agent could run (not built, the owner's call):

```sh
mkdir -p -m 700 ~/.config/Cogpit && printf 'on\n' > ~/.config/Cogpit/mcp-switch   # example on Linux; macOS: "~/Library/Application Support/Cogpit/mcp-switch"; Windows: %APPDATA%\Cogpit\mcp-switch
```

Security note on `get`: it only reads (`resources_list` and `object_get` through the cache, origin
CLI), prints a Secret's key names and sizes but never a value, and refuses a context whose
credential plugin you have not allowed in Cogpit's window before the plugin runs. Like the app's
views it watches a kind in every namespace and filters by `-n` afterwards, so a user who may list
a kind only in some namespaces gets the API server's refusal.

Security note on `contexts`: context names, file names and every other text from a kubeconfig or a
cluster are escaped before they reach your terminal (`\u001b` for an escape character), since
anyone who can drop a file into a sync folder chooses them. Without a configuration directory
(no `$HOME`) the CLI refuses to connect rather than merging the files as kubectl does, which
would run their plugins unasked.

### Preferences › Kubernetes: program folders

A kubeconfig's credential plugin (kubelogin, a cloud's plugin, `kubectl oidc-login` and the kubectl
plugin it runs) is looked up on `PATH`, and Cogpit started from the Dock, Finder or a launcher does
not get the `PATH` your shell's profile sets (since 2026-10-24,
[D-120](decisions.md#d-120-credential-plugins-found-in-more-folders)). Cogpit therefore looks in
more folders, always after your `PATH`'s own:

1. the folders `PATH` has when Cogpit starts;
2. on macOS and Linux, its built-in folders, each only while it exists: `/opt/homebrew/bin`,
   `/usr/local/bin`, `/opt/local/bin`; on Linux `/home/linuxbrew/.linuxbrew/bin` and `/snap/bin`;
   `/run/current-system/sw/bin`, `/nix/var/nix/profiles/default/bin`,
   `/etc/profiles/per-user/<user>/bin`, `~/.nix-profile/bin`; `~/.local/bin`, `~/bin`;
   krew's `~/.krew/bin` (`$KREW_ROOT/bin` when Cogpit sees `KREW_ROOT`); go install's `~/go/bin`
   (`$GOBIN`, or the first folder of `$GOPATH`, when set); `~/.asdf/shims`,
   `~/.local/share/mise/shims`; `~/.rd/bin` (Rancher Desktop), `~/.docker/bin` (Docker Desktop);
3. the program folders you add in Preferences › Kubernetes › Program folders, in the order added.

Add Folder… opens the system's folder panel with hidden folders listed (⌘⇧. toggles them), so
`~/.krew/bin` can be chosen; each folder is a row with a remove button. A folder counts from the
next login on, without a restart: Connect Again after adding it. When a login fails because its
program is not found, the error page and Connection Updates say which program and where to add its
folder, and the error page's Program Folders… opens this section.

```json
{
  "programFolders": ["/Users/me/tools/bin"]
}
```

| Field | Default | Meaning |
|---|---|---|
| `programFolders` | `[]` # default | the folders, absolute paths, Cogpit looks in after `PATH` and its built-in folders, at most 32; kept in `preferences.json` and read by `cogpit-cli` too. A folder that is missing is kept and skipped until it exists |

| Command | Who | What it does |
|---|---|---|
| `preferences.addProgramFolder {path}` | your click only (GUI only) | adds a folder that exists, is a folder and that not every user of the computer may write to |
| `preferences.removeProgramFolder {path}` | anyone, agents included | removes a folder |

Security note: a folder is only ever appended, so a program your `PATH` finds keeps running and
nothing is shadowed; a program a login names without a folder that your `PATH` lacks is found in
these folders instead, bound by its name as a program on `PATH`
([H-6](../security/credentials.md#h-6-exec-plugins-inherit-the-environment)). Only you can add a
folder, in Cogpit's window: a folder an agent added could hold the program your next login runs.
Add only folders that you alone can write to: a folder others write to (a shared or synced folder)
lets them choose what runs for a name your `PATH` lacks; a folder every user may write to is
refused. Variables a profile sets (`AWS_PROFILE`) still do not reach Cogpit.

### Preferences › Kubernetes: `kubectl` for terminals

Kept in `<user config dir>/Cogpit/preferences.json` (owner-only), set on the Preferences page (⌘,)
or with `preferences_set`. When a terminal opens, its `kubectl` is compared with the cluster's
version; more than one minor version off, or none, and the terminal offers the matching release
([D-3](decisions.md#d-3-helm-and-kubectl-binaries)).

```json
{
  "kubectlDownload": true,
  "kubectlMirror": "",
  "kubectlDir": "",
  "kubectlPath": "",
  "kubectlEnforce": false
}
```

| Field | Default | Meaning |
|---|---|---|
| `kubectlDownload` | `true` # default | offer the matching release in a terminal whose `kubectl` does not suit the cluster; off, the terminal only says so. The download itself always waits for your click |
| `kubectlMirror` | `""`: `https://dl.k8s.io/release` # default | the https base URL under which `v<version>/bin/<os>/<arch>/kubectl` and its `.sha256`, `.sig` and `.cert` lie, such as `https://mirror.example.com/kubernetes/kubectl` # example |
| `kubectlDir` | `""`: `<user config dir>/Cogpit/kubectl` # default | the directory for binaries, an absolute path; releases go into `<dir>/<version>/kubectl` (`kubectl.exe` on Windows, where the default is `%APPDATA%\Cogpit\kubectl`) |
| `kubectlPath` | `""`: the first `kubectl` on `PATH` # default | the `kubectl` your terminals run, an absolute path; Cogpit checks it against the cluster as it would check `PATH`'s and puts it first on the terminal's `PATH` |
| `kubectlEnforce` | `false` # default | off: the chosen `kubectl` goes first on the terminal's `PATH` once the shell's profile ran (zsh through `ZDOTDIR`, bash through `--init-file`, fish through `-C`); on: zsh, bash and fish also get a function `kubectl` that runs it, so that a folder the profile puts first later, or an alias, does not win (A27, since 2026-10-14). Other shells keep the environment's `PATH` order ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first)); on Windows PowerShell and cmd get no function yet, so the switch does nothing there ([the terminal on Windows](#install-on-windows), D-111) |

Security note: the last four decide which program your terminals run as `kubectl`, so only you, in
Cogpit's window, can set them, and only you can start a download; agents and `cogpit-cli` are
refused. A mirror needs no trust of its own: every download is checked against the Kubernetes
release's sigstore signature (its certificate chained to sigstore's authority for the release's
identity) and that signature's entry in sigstore's transparency log, all against the sigstore
trusted root Cogpit embeds, and refused when the mirror serves no `.sig` or `.cert`. The download
goes through the HTTP proxy for non-cluster traffic (`httpProxy`, Preferences › Proxy) when one is
set.

The download makes one more outgoing request (since 2026-10-15,
[D-50](decisions.md#d-50-kubectls-signature-check)): before the binary is fetched, Cogpit asks
sigstore's transparency log at `https://rekor.sigstore.dev` (`POST /api/v1/log/entries/retrieve`)
for its entry of the release's signature, through the same proxy, with the published SHA-256, the
signature and the certificate (public material, no credential), within 30 seconds. Whatever the
mirror, this host must be reachable: without its answer the download is refused ("sigstore's
transparency log could not be asked"), and there is no fallback to a check without it. In a network
that cannot reach it, put a `kubectl` on `PATH` or set `kubectlPath`
([H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).

### Preferences › Kubernetes: chart icons

The Charts view and a chart's drawer show the icon a chart repository's index names
([D-46](decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them)), fetched only for what the window shows.

```json
{
  "loadChartIcons": true
}
```

| Field | Default | Meaning |
|---|---|---|
| `loadChartIcons` | `true` # default | fetch each chart's icon from the host its URL names; off, Cogpit's own helm-wheel glyph and nothing fetched |

Security note: an icon's URL may point at any host, which then learns that your computer looked at
the chart ([H-87](../security/trust-boundaries.md#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at)).
Cogpit fetches it over https only, without the repository's credentials, cookies or a referrer, at
most 256 KiB, never from an address in your own network unless it is a chart repository's own host,
and through the HTTP proxy for non-cluster traffic when one is set; a PNG or JPEG is decoded under
bounds and kept as a small PNG, an SVG only when it refers to nothing outside itself. Agents never
fetch icons, and only you, in Cogpit's window, can turn the switch on; agents may turn it off.

### Preferences › App: start at login

macOS only (13 or later): Preferences › App › Start at login makes macOS open Cogpit when you log
in, as the app's own login item, which System Settings › General › Login Items lists too
([D-48](decisions.md#d-48-start-at-login)). It is macOS's state, not a field of `preferences.json`:
the row shows what macOS reports, read when the section opens.

| Status (`preferences_loginItem`, `ui_state`'s `loginItem`) | Meaning |
|---|---|
| `off` # default | not registered: Cogpit starts when you open it |
| `on` | registered: macOS opens Cogpit at login |
| `requiresApproval` | registered, but macOS waits for you to allow it in System Settings › General › Login Items (the row's Open Login Items… button goes there) |
| `notFound` | macOS finds no login item for this Cogpit: it needs the signed `Cogpit.app` in Applications; a `bin/cogpit` or `go run` build reports this without asking macOS |

Security note: with it on, Cogpit runs after every login without being opened, its MCP endpoint
included if you switched that on (it is off until you do); so only you, in Cogpit's window, can turn it on, and agents
may only turn it off. Not offered on Windows and Linux yet. What macOS does with the releases' app,
signed with the Developer ID since v0.6.0, is not verified: no signed app installed in Applications
has been tried ([D-48](decisions.md#d-48-start-at-login)).

### Preferences › App: restore the last session

Since 2026-10-16 ([D-69](decisions.md#d-69-several-clusters-connected-at-once) item 8, the owner's
decision): **off by default**, Cogpit starts without tabs and without connections, on the welcome
page. On, the next start reopens the tab bar's tabs of the last run and connects again to the
clusters that were connected when Cogpit quit: the one you were looking at first, shown as on any
connect with its most recently used tab, then the others one after another in the background
(`ui_state`'s `connections` lists each as `connecting` meanwhile). Their login programs run as on any connect; one you have not
allowed yet, or one whose program changed since, is not run, and the cluster says why (allow it in
the navigator). When the one you were looking at fails to connect, the window shows its error page
with the connect's stages (since 2026-10-22, [below](#several-clusters-connected)), not the welcome
page. `--context` shows its own cluster, and the restore connects the others behind it. A
cluster whose kubeconfig is gone is left out with a notice. The dock's tabs (logs, terminals,
editors) are not reopened. The tables' column choice and order, the navigator's collapsed groups,
whether the navigator and the chat are shown, and the window's place and size
([below](#the-windows-place-and-size)) come back whatever this says. The welcome page has
the same preference as a checkbox under "Add Kubeconfigs…", "Restore this session at the next start:
its tabs and connected clusters" (since 2026-10-17 evening, the owner's walk, WF1): ticked there, it
is on in the Preferences too. Every cluster gets its tabs back; the tab bar shows those of the
cluster shown first once it is connected, the others' come with their cluster when you go to it
(since 2026-10-20, the owner's GW11), unless Mixed environments is on (below); a cluster whose
connect fails (an expired certificate) shows no tabs (GW13).

| Preference (`preferences.set`, `ui_state`'s `preferences`) | Default | Meaning |
|---|---|---|
| `restoreSession` | `false` # default | on: the next start restores the last session's tabs and connections |
| `mixedEnvironments` | `false` # default | on: the tab bar shows the open tabs of every cluster side by side; off: only the shown cluster's. Set at the foot of the navigator's All Clusters (its ? explains it); agents may set it either way, it changes only what the bar shows |

`workspace.json` keeps the clusters connected and the one shown whether it is on or off, so turning
it on restores the session of your last quit. Security note: on, the next start connects clusters
and runs their login programs without a click, so only you, in Cogpit's window, can turn it on;
agents and the CLI may only turn it off.

### Preferences › App: reduce motion

Since 2026-10-20 ([D-107](decisions.md#d-107-the-navigator-slides-and-reduce-motion), the owner's
request): **off by default**, the navigator slides. Going into a cluster moves its tree in from the
right over All Clusters, ‹ All Clusters moves it out to the right again; showing another cluster
while a cluster's tree shows (a tab of another cluster with Mixed environments on, a hotbar tile, an
agent) slides vertically: a cluster listed above the shown one in All Clusters comes in from above
and pushes the old one down, one listed below comes in from below. Every change of the navigator's
scope slides, an agent's too; the first view at start does not. On, nothing in Cogpit moves: the
navigator switches at once, a dragged hotbar tile or tab jumps to its place instead of gliding, and
a reconnecting cluster's dot stays steady at half strength instead of blinking. macOS's own Reduce
motion (System Settings › Accessibility › Display) does the same, whatever the preference says.

| Preference (`preferences.set`, `ui_state`'s `preferences`) | Default | Meaning |
|---|---|---|
| `reduceMotion` | `false` # default | on: the window switches at once where it would slide or blink; agents may set it either way, it changes only how the window moves |

### The window's place and size

Since 2026-10-18 ([D-95](decisions.md#d-95-the-window-comes-back-where-it-was), the owner's
WF11): Cogpit opens where you closed it, at every start and whatever Restore the last session says.

| Platform | What comes back | When it is kept |
|---|---|---|
| macOS | the size, the place, the display, full screen | half a second after you move or resize the window, take it to another display or into or out of full screen, and at quit |
| Windows, Linux | the size and full screen; the place is the system's | when the window closes |

| Case (macOS) | Where the window opens |
|---|---|
| First start, or no `window.json` | 1440 × 900 in the middle of the main display # default |
| Its display is gone (unplugged) | 1440 × 900 in the middle of the main display; plugged in again, the window goes back to it |
| Its display is arranged elsewhere | on that display, at the same place from its top left |
| Larger than its display, or over an edge | clipped to the display's area without the menu bar and the Dock, moved in |
| In full screen at quit | at its window frame, then in full screen once Cogpit is the active app |

To start over, quit Cogpit and delete `window.json` ([files](#naming-conventions)). Agents neither
read nor move the window: it is not part of `ui_state`.

On macOS, when the window's pixel scale changes (it goes to a display at another scale, or the
displays go off and come back around a sleep), Cogpit draws its texts again once, as a theme switch
does, so that none stays drawn at the old resolution, soft
([D-121](decisions.md#d-121-texts-drawn-again-when-the-windows-pixel-scale-changes)). Should a
text still look soft, switching the theme twice (the top bar's theme button) draws it again. On
Windows and Linux Fyne draws the texts again itself.

### Cluster Settings › General: the login while the cluster is not shown

Since 2026-10-16 ([D-69](decisions.md#d-69-several-clusters-connected-at-once) item 4). **On by
default:** a cluster that stays connected while you look at another one renews its login as the
shown one does, so its login program may run (a browser login included) when the login expires.
Off ("Renew the login while the cluster is not shown" unchecked), its login program does not run
while another cluster is shown: when its login expires, the cluster's login **lapses**. It stays
connected and is not shown in red (whether a renewal would work is not known); `ui_state`'s
`connections[].loginLapsed` says so, with since and why (the window shows it as a grey ring on the
cluster's mark, D-69 stage 2). Meanwhile its checks pause, changes to
it are disabled ("login lapsed: show the cluster to renew it"), a log that has to open again waits
with the line `[cogpit] the login lapsed; it is renewed when the cluster is shown`, port forwards
wait, and a drain or a Helm action that was running stops as at an interruption. A stream that is
open goes on. Show the cluster and its login is renewed as the shown cluster's is (its login program
runs once; **Login expired** in red if that does not help); **Log in again** (`cluster.login` with
the cluster's context, the window's only) renews it without showing it. An agent never renews it: its reads come from the cache with a note, and what
needs the cluster is refused.

| Setting (`cluster.settings.set`, `cluster.settings.get`) | Default | Meaning |
|---|---|---|
| `renewLoginInBackground` | `true` # default | off: the login program does not run while the cluster is not shown; kept as `holdBackgroundLogin: true` in `cluster-settings.json` |

Security note: on lets a login program run while you work with another cluster, so only you, in
Cogpit's window, can turn it on; agents and the CLI may only turn it off. Two contexts whose login
program runs with the same command, arguments and environment share one login in client-go: it is
held only while both are held.

### Cluster Settings › General: the icon

A cluster's icon is one of two kinds, chosen in the Icon row (since 2026-10-23, the owner's VW5,
[D-119](decisions.md#d-119-the-cluster-icon-designer)); **Customize…** opens the chosen kind's
dialog, **Reset** gives back the initials in the accent colour:

- **Internal icon:** a colour tile with letters. Its dialog takes up to five letters, digits or
  signs (no spaces; empty: the initials of the cluster's name) and one of eight colours or any
  colour. Longer letters are drawn smaller to fit, so the dialog previews the icon at the hotbar's
  size and at the navigator's.
- **Custom icon:** a square part of an image. Its dialog shows the image in the icon's square frame,
  the rest dimmed: drag to move it, zoom with the mouse wheel (at the pointer) or the slider (up to
  8×), **Fit** to fill the square from the middle again, **Choose Image…** for another file. The
  image always fills the square. Opened again, it shows where you left it.

Choosing one keeps the other: back on the Internal icon, the image stays for when you choose Custom
again. Both kinds are drawn with rounded corners as macOS draws an app icon, Cogpit's own included:
the radius is 22/96 of the side wherever the icon shows, the hotbar, the navigator, the welcome page,
the tabs and the settings (since 2026-10-24, the owner's VW10; before, an image was drawn square).
The kept image itself stays square; the rounding is drawn.

| Field (in `cluster-settings.json`, per context) | Default | Values |
|---|---|---|
| `iconColor` | none # default: the accent | one of `cyan`, `blue`, `violet`, `magenta`, `red`, `orange`, `yellow`, `green`, or `#rrggbb` (`"#d33682"` # example) |
| `iconLetters` | none # default: the name's initials | at most 5 characters without spaces (`"PROD"` # example) |
| `iconTile` | `false` # default | `true` while the tile shows although an image is kept |
| `icon`, `iconSource` | none # default | file names in `cluster-icons/`: the 256-pixel icon and the image it was cut from (at most 1,024 pixels a side; an SVG is drawn into a PNG) |
| `iconCrop` | none # default | `{"zoom": 1.5, "x": 0.4, "y": 0.5}` # example: the zoom (1: the image's shorter side fills the square) and the image's point at the square's centre, as fractions of its width and height |

Security note: the image file is read once, at most 1 MiB, and checked before anything is kept (a
PNG or JPEG decoded under bounds, an SVG without scripts or outside references); Cogpit keeps its
own copies and draws only those. Every change of the icon is yours alone, in Cogpit's window
([H-125](../security/agents-and-mcp.md#h-125-until-the-accent-colour-shows-a-clusters-name-and-icon-colour-are-agents-to-change)).

### Cluster Settings › General: the accent colour

Since 2026-10-17 ([D-80](decisions.md#d-80-a-clusters-accent-colour), the owner's F26). Beside its
icon every cluster can carry an **accent colour** you pick, your mark of which cluster it is (red for
production, say), meant to mark parts of the window so that you see at a glance where you act. **The
window does not use it anywhere yet:** where it shows is still open, so today you see it only in
its own row. The row "Accent colour" sits below the icon: None (the crossed square, the default),
the eight colours of the icon, a colour of your own from the palette button. A click applies it.

| Setting | Default | Meaning |
|---|---|---|
| `accentColor` in `cluster-settings.json`, per context | none # default | one of `cyan`, `blue`, `violet`, `magenta`, `red`, `orange`, `yellow`, `green`, or `#rrggbb` (e.g. `"#d33682"` # example); absent: none. Set with the row, or `cluster.settings.setAccentColor {context?, color}` from the window (empty `color`: none) |

Security note: only you set or clear it, in Cogpit's window. `cluster.settings.setAccentColor` is
GUI-only: agents (over MCP and the built-in one) and `cogpit-cli` are refused, also where you allowed
agents' writes, so no agent can make a production cluster look like a development one. Agents read
it (`accentColor` in `ui_state` and `cluster_settings_get`). Since 2026-10-19 the cluster's **name
and icon** are yours alone too ([H-125](../security/agents-and-mcp.md#h-125-until-the-accent-colour-shows-a-clusters-name-and-icon-colour-are-agents-to-change)):
`cluster.settings.set`'s `name`, `iconColor`, `iconLetters` and `iconTile` are refused to agents and
`cogpit-cli`, whether they set or clear them, and Reset (`cluster.settings.resetIcon`), the image's
upload (`cluster.settings.uploadIcon`) and its cut (`cluster.settings.cutIcon`) are GUI-only. Agents
read the name, the icon colour and the letters; the other fields of `cluster.settings.set` (the terminal's
directory and namespace, hidden metrics, accessible namespaces) stay theirs.

The same page has copy icons (since 2026-10-17, F27): the one by **Cluster name** copies what the
field shows (your display name, else the context's name in its kubeconfig), the one by
**Kubeconfig** the file's whole path; a context merged from the default files has none there. The
path itself is shown whole, wrapped where it is long, and selectable (since 2026-10-18, the owner's
WF28): drag over it, double-click a word, ⌘A for all of it, ⌘C to copy.

### Cluster Settings › Integrations, and the question at a new cluster's first connect

Since 2026-10-17 ([D-76](decisions.md#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect), the owner's words; the pre-choice and Ask again since that
evening, [D-82](decisions.md#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)). An integration is a tool
the cluster runs that Cogpit finds and reads to show more: the Kubernetes Metrics Server and
Prometheus (the metrics), Alertmanager (the alerts pill and the alert board), the Trivy Operator
(the pods' image ratings) and Kyverno (policy violations on the objects); the last three are
D-77 to D-79. Cogpit installs none of them. Each is looked for and read with your identity, and
Cogpit never writes to it. **Nothing of a cluster's integrations is looked for until you choose**:
a cluster you have not chosen for reads none of them, not while its question is put off, not while
it is connected in the background. The default for new clusters (every integration) is what the
question's first choice looks for. The Alertmanager's polls and the Trivy Operator's and Kyverno's
reports are read only while the cluster is the one shown; a cluster connected in the background
reads none of them.

In Cluster Settings the integrations stand under a divider below Agents (since 2026-10-18,
[D-87](decisions.md#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer)):
the heading **Integrations**, which opens a section for all five, then **Metrics Server**,
**Prometheus**, **Alertmanager**, **Trivy Operator** and **Kyverno**, a section each.
Integrations has a row per integration: its switch, what Cogpit found ("Found automatically:
monitoring/prometheus-server:80", "Needs manual configuration: none found by name", "Not in this
cluster", "No permission", "Not answering", "Checking…", "Off"), what it adds, and a link to its own
section ("Details…", "Set its address…" while Prometheus or Alertmanager needs one). An
integration's own section has its switch ("On for this cluster"), the same status (where a found
Prometheus or Alertmanager runs, its namespace and service as links that close the settings and
open them), what it adds, and its details: Prometheus's query format, service address, HTTPS and
path prefix; Alertmanager's service address, HTTPS and path prefix. They can be set while the
integration is off; setting them sends nothing, and switched on, Cogpit asks only that address. A
switch applies to the connected cluster at once; off, Cogpit sends the cluster nothing for that
integration and shows nothing of it. A cluster not chosen for yet says so ("Not chosen for this
cluster yet: nothing is looked for until you tick one here or answer its question"), its switches
off; ticking one is a choice: that one is on, the others stay off. **Ask again** (on Integrations,
not on the demo cluster) forgets the cluster's choice and asks the question again, from its first
page, while the cluster is shown and connected; until you answer, nothing is read. Cluster Settings
› Metrics names the two integrations it reads, the Metrics Server and Prometheus, each with its
state and a link to its section, and keeps Hide metrics; before you chose it says that no metrics
are asked until you choose. Cogpit reads Prometheus and Alertmanager through the API server's
service proxy and spells the port by its number first (`monitoring/prometheus-operated:9090`), as
Lens does, then by its name (`…:http-web`) when the number is refused: a role that grants
`get services/proxy` only for named services (`resourceNames`) is matched as written, so a role
written for Lens works; each refused spelling is a 403 in the cluster's audit log.

**What you see at a cluster's first connect:** a cluster you have not chosen integrations for asks,
once it is connected and shown, in a dialog over the window "Integrations for <name>" (the cluster's
views load behind it, its resources as always). It opens on a pre-choice: why it asks (Cogpit looks
for these tools with your identity and never writes to them; nothing is looked for until you
choose), and three choices:

- **Check all integrations** ("Check the default integrations" while the default for new clusters
  switches some off, naming those it looks for): the rows, every integration of the default ticked
  and looked for at once, their statuses filling in. Untick what you don't want.
- **Choose integrations**: the rows, none ticked ("Not looked for: tick it to look for it"); a tick
  looks for that one at once, an untick asks it nothing more.
- **Don't use integrations**: a last warning, "Use <name> without integrations? Cogpit then shows no
  metrics, alerts, image ratings or policy marks for <name>; its resources still show. Cluster
  Settings › Integrations turns them on later." Yes keeps every integration off as the cluster's
  choice; No goes back to the three choices.

The rows have the checkbox "Save as the default for new clusters" (off until you tick it), Back (to
the three choices, looking for nothing), Later and Apply. Apply keeps your ticks as the cluster's
choice, and with the checkbox as the default for new clusters. While the question is open, what is
ticked is read as if on (its looking is its reading), behind the dialog. Later asks no more this run
and again at the next start; meanwhile nothing is read. Clusters you used before this version ask
once too; the demo cluster never asks and keeps the default. Preferences › Kubernetes › Integrations
of new clusters shows and changes the default. `cogpit-cli`, which keeps no cluster settings, asks
nothing and reads every integration the default keeps on (H-120).

| Setting | Default | Meaning |
|---|---|---|
| `cluster.settings.set`'s `integrations`, e.g. `{"alertmanager": false}` | every one off until you choose # default | switches integrations on (`true`) or off (`false`) for the cluster; the result is its own choice, the ones left out as they apply now (for a cluster not chosen for: off). Kept as `integrationsOff` (the names switched off) in `cluster-settings.json`; absent: no choice yet, nothing read and the window asks. Agents and the CLI: refused, `true` and `false` alike (since 2026-10-24, H-133) |
| `preferences.set`'s `integrationDefaults`, e.g. `{"trivy": false}` | every one on # default | what the question's Check all looks for, and what the CLI's session reads; kept as `integrationsOff` in `preferences.json`. Agents and the CLI: refused, `true` and `false` alike (since 2026-10-24, H-133) |
| `cluster.integrations.step` `{context?, step}` | `preChoice` # default | the question's page: `preChoice`, `checkAll`, `choose`, `dontUse`; `cluster.integrations.lookFor` `{context?, integrations}` ticks the rows (`{"prometheus": true}`); both GUI-only |
| `cluster.integrations.answer` `{context?, integrations?, saveAsDefault?}` | | the dialog's Apply, and the warning's Yes with every one `false` (GUI-only); `cluster.integrations.later` `{context?}` its Later (open to agents); `cluster.integrations.ask` `{context?}` Ask again (GUI-only since 2026-10-24: it forgets your choice); `ui_state`'s `integrationsQuestion`, `integrationsStep` and `integrationsLookFor` show the question |

A `metricsSource` an older Cogpit kept in `cluster-settings.json` is moved into the two metrics
switches once (`prometheus`: the Metrics Server off; `metrics-server`: Prometheus off; `none`: both
off). Security note: only you switch an integration, on (since 2026-10-19,
[H-126](../security/agents-and-mcp.md#h-126-an-agent-can-answer-the-integrations-question-for-the-user))
and off (since 2026-10-24, [H-133](../security/agents-and-mcp.md#h-133-an-agent-can-switch-an-integration-off-and-so-hide-alerts-or-policy-marks)),
and only you answer the question: switching one on lets Cogpit look for and read it with your
identity, and switching one off takes its alerts, image grades or policy marks out of the window
without the window saying who did it. Agents (over MCP and the built-in one) and `cogpit-cli` are
refused `cluster.integrations.step`, `.lookFor`, `.answer` and `.ask` (GUI-only; Ask again forgets
your choice, which switches every integration of the cluster off until you answer), `cluster.settings.set`'s
`integrations` (`true` or `false`) and `metricsSource` (any value, `none` included), and
`preferences.set`'s `integrationDefaults` (`true` or `false`), also where you allowed agents' writes;
the other fields of those commands stay theirs. They may put the question off
(`cluster.integrations.later`): it only defers a question you have not answered, whose cluster reads
nothing meanwhile. Where an integration is
read from when it is not found by name stays yours to set ([agents and MCP](../security/agents-and-mcp.md)).
`cogpit-cli` does not apply these switches ([H-120](../security/agents-and-mcp.md#h-120-the-clis-headless-engine-does-not-apply-a-clusters-integration-switches)).

### Deep links (`cogpit://`)

macOS only: the app registers the URL scheme `cogpit`, so a link in a runbook, a chat or an alert
opens Cogpit at a cluster, a view or an object ([D-47](decisions.md#d-47-deep-links-cogpit)).

```text
cogpit://open?context=kind-dev%20%5Bconfig%5D&view=pods&namespace=team-a&name=web-0   # example
```

| Parameter | Value | Without it |
|---|---|---|
| `context` | a context id as `cluster_contexts` lists it (`<context> [<file>]`), or a kubeconfig context name that one source file alone defines; in a development build `demo` for the demo cluster | the cluster the window shows |
| `view` | a kind key (`pods`, `deployments`, `gateways.gateway.networking.k8s.io`) or a page key (`cluster/overview`) | the view the connect shows |
| `namespace` | a namespace: with `name` the object's; without it the namespace filter | the filter as it is |
| `name` | an object of the view's kind: its drawer opens; needs `view` to be a kind, not a page | no drawer |

At least one parameter; each at most once, percent-encoded, at most 2,048 bytes in all. Cogpit
comes to the front, connects the context unless it is connected, and opens what the link names;
a toast says what it opened or why not, and the audit log records the link (origin `deeplink`).

Security note: any web page or app can open such a link, so a link only navigates: it never
writes to a cluster, never runs a command with a confirmation, never names a file, and never
connects a context whose login program or token file you have not allowed yet (connect from the
navigator's clusters, ⌘P, to review it). A malformed link is refused whole. What remains is in
[H-89](../security/trust-boundaries.md#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)
and [H-90](../security/trust-boundaries.md#h-90-a-links-text-reaches-agents-through-the-logs).
Not on Windows and Linux yet.

### Preferences › App: updates

The macOS app checks Cogpit's signed appcast with Sparkle 2 and offers a newer release in Sparkle's
window, installed on your click ([D-4](decisions.md#d-4-packaging-and-updates)); Help › Check for
Updates… checks now. While "Check for updates automatically" is on (its line reads "At every start
and every hour; a new release installs with one click"), Cogpit checks at every start and then
every hour. A release such a check finds shows as a button at the top bar's right
end, left of the alerts pill, the bell and the layout toggles, not as Sparkle's window: "Update to v0.16.0" (# example: the
version the feed names), "Update" when it names none or the bar has no room for the version (a
narrow window beside a long context name); the tooltip names the version, and a click opens
Sparkle's window with the release. The button stays until Sparkle's update session ends (the
release installed or skipped, or put off with "Remind Me Later" until the next scheduled check)
([D-54](decisions.md#d-54-the-layout-toggles-and-the-update-button)). Kept in
`<user config dir>/Cogpit/preferences.json` with the other preferences. On Linux apt does the
updating, with the same preference and button ([below](#updates-on-linux)); on Windows WinSparkle,
whose button installs at once ([below](#updates-on-windows)).

```json
{
  "checkForUpdates": true,
  "updateChannel": "stable"
}
```

| Field | Default | Meaning |
|---|---|---|
| `checkForUpdates` | `true` # default | Sparkle checks `https://guided-traffic.github.io/cogpit/appcast.xml` at every start and every hour (since 2026-10-16; once a day before); on Linux Cogpit reads apt's lists then instead, on Windows `https://guided-traffic.github.io/cogpit/appcast-windows.xml`; off, no check at start or on a schedule, only Help › Check for Updates… checks (on Linux apt and the Software Updater still offer Cogpit's releases: they are the system's) |
| `updateChannel` | `"stable"` # default | the only channel until Cogpit publishes prereleases; `beta` is refused for now |

Security note: an agent or `cogpit-cli` may turn `checkForUpdates` on, never off; only you can, in
Cogpit's window ([agents and MCP](../security/agents-and-mcp.md)). A build without the update key
(every local build, and every release up to v0.10.0; the releases carry the key since v0.11.0), an app still on its disk
image, a Cogpit on Linux that is not the installed package's and a Cogpit on Windows that is not
the setup's `cogpit.exe` (a build of your own, a copy elsewhere) do not update themselves, and the
row says why. `cogpit-cli`
has no updater of its own: the one inside the app is replaced with the app (on Linux with the
package, on Windows by the setup), one built from source is not.
Sparkle installs an update only when its disk image and the feed carry the update key's signature
and the new app carries Cogpit's Developer ID ([updates](../security/updates.md)).

#### Updates on Linux

Since 2026-10-20 night (from the release after v0.32.0 on,
[D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) apt updates
Cogpit, from the APT repository the package set up ([Install on Linux](#install-on-linux)), and
apt checks the repository's signature before it installs anything
([updates › Linux](../security/updates.md#linux)). While `checkForUpdates` is on, Cogpit reads
`apt-cache policy cogpit` at every start and every hour: what apt's package lists, which the system
refreshes by itself every day, say is installed and newest. That read sends no request and needs
no password.

| What apt's lists say | The top bar's button | Its click |
|---|---|---|
| a newer release than the installed one | "Update to v0.34.0" # example ("Update" where the bar has no room) | `pkexec /usr/libexec/cogpit/update`: polkit asks for an administrator's password, then the package's helper refreshes the list of Cogpit's repository only and upgrades the package `cogpit` only; a notification says "Cogpit 0.34.0 is installed: click Restart to Update to use it" # example, or why not (the dialog dismissed, not allowed, the newest already) |
| a newer release installed under the running Cogpit (by the button, `apt upgrade`, the Software Updater or unattended-upgrades) | "Restart to Update" | Cogpit quits and starts again as the installed release: a detached shell waits for this process to end and starts `/usr/bin/cogpit` |
| nothing newer | none | – |

Installing never restarts Cogpit by itself: the restart is your second click. Help › Check for
Updates… runs the helper too, password included, since only root can fetch a fresh list. On Linux
only your own click in the window runs `app.checkForUpdates`: an agent or `cogpit-cli` is refused
(it would put a password prompt or a restart in front of you unasked) and reads `ui_state`'s
`update` (`available`, `version`, `restart`) instead. Where this Cogpit is not `/usr/bin/cogpit` of
the installed package (a build of your own) the Help item is greyed "not installed from the
package"; where `/etc/apt/sources.list.d/cogpit.sources` was deleted, "no package repository".
`updateChannel`'s `stable` is the repository's suite `stable`, its only one.

#### Updates on Windows

From the first release after the branch `windows-release` is merged (built 2026-10-21,
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)) WinSparkle, Sparkle's
Windows port, updates Cogpit for Windows: the `cogpit.exe` the setup installed (in the folder its
uninstall entry names), in a release build, with `WinSparkle.dll` beside it
([Install on Windows](#install-on-windows)). While `checkForUpdates` is on, Cogpit reads the signed
feed `https://guided-traffic.github.io/cogpit/appcast-windows.xml` itself at every start and every
hour and checks its signature with the update key the release carries; a newer release for Windows
x64 than the running one, unless you skipped it in WinSparkle's window, shows the Update button
([updates › Windows](../security/updates.md#windows)).

| What you click | What happens |
|---|---|
| The Update button, "Update to v0.35.0" # example (tooltip: "… is available: install it now; Cogpit restarts into it") | installs at once (`app.installUpdate`; the owner's choice of 2026-10-21): WinSparkle's window shows the download, WinSparkle checks the setup's signature with the update key, runs it silently (`/S /relaunch`) and asks Cogpit to quit; the setup waits for Cogpit to end, installs, and starts the new release. No second question: your click is the decision. An installation for you needs no administrator; one for every user asks UAC, since its setup runs elevated |
| Help › Check for Updates… | WinSparkle's window: whether a newer release exists, its notes (the release's page), Install update, Skip this version, Remind me later; Install update then does what the button does |

Only your click in the window runs `app.installUpdate`: an agent or `cogpit-cli` is refused, since it
would install and restart Cogpit without asking again; `app.checkForUpdates`, which agents may run,
shows WinSparkle's window, which installs nothing without your click there. Cogpit's own reading of
the feed goes through Go's HTTP client, which follows `HTTPS_PROXY` but neither Windows' proxy
settings nor Preferences › Proxy; WinSparkle's own requests (Help › Check for Updates…, the
download) follow Windows' settings. Behind a proxy only Windows knows, the Update button stays
away, and Help › Check for Updates… still finds the release. Checked end to end on 2026-10-21 with a
throwaway key and a feed on `127.0.0.1` (the owner's Windows 11): 0.0.1 showed "Update to v0.0.2",
the click had 0.0.2 running 4.6 s later; not yet through a release.

### Preferences › Agent: the chat's connections

The built-in agent's chat (⌘I) talks to the LLM connections you add here
([D-10](decisions.md#d-10-built-in-agent)); each conversation uses one, a new one the default.
Only you edit them, in the window: none of it is a command or an MCP tool. A connection's fields,
kept in `llm-connections.json` (its key apart in `llm-keys.json`, [naming](#naming-conventions)):

| Field | Default | Meaning |
|---|---|---|
| Name | – | how the chat's connection menu names it, `LM Studio on this Mac` # example |
| Kind | LM Studio # default | LM Studio, LiteLLM, Open WebUI or another OpenAI-compatible server: sets the paths (`/chat/completions` and `/models`, Open WebUI's under `/api`) |
| Base URL | `http://localhost:1234/v1` # default for LM Studio | where the API is; `https://litellm.example.com` # example. Plain `http://` to another machine (anything but `localhost`, 127.0.0.0/8 and ::1) is allowed, and the form, the list ("plain http") and the connection test warn in the warning colour that not only the key but the whole conversation and every Secret or other secret the agent reads travel unencrypted ([H-95](../security/credentials.md#h-95-an-llm-api-key-over-plain-http)) |
| API key | none # default | sent as `Authorization: Bearer …` to the base URL only (no redirects are followed); empty keeps a stored key, Remove Key removes it |
| Model | – | from the endpoint's list or typed, `qwen/qwen3.8-27b` # example |
| CA file | none # default | for an `https://` server with a private CA: an absolute path (`~/` is your home folder; Choose… opens the system's file panel, Fyne's dialog on Windows and Linux) to a PEM file of its CA certificates, `/Users/you/certs/team-ca.pem` # example; for this connection its certificates replace the system's roots (on macOS its trust store), as a kubeconfig's `certificate-authority` does for kubectl (the owner's decision in the audit of 2026-10-16): only they vouch for the server, so a server that moves to a public certificate fails with "this connection trusts only its CA file" until the field is cleared; read whenever the chat connects, so a renewed file counts at once; refused with an `http://` base URL. Without it, a certificate macOS does not trust fails the test's HTTP and TLS layer with a hint at this field ([H-104](../security/trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)) |
| Private connection | off # default | a local model or a private cloud: the chat then reads what the window shows, Secret values included; its conversations are not saved ([H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows), [H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)) |
| Send all tools | off # default | every Cogpit command the chat may run as a tool of its own with each request (179 tools, about 56,000 tokens, measured 2026-10-15), for large cloud models; off, 20 core tools and a search that finds the rest (about 7,600 tokens). The same rules either way: what the agent reads follows the private mark, every write asks as D-2 says |
| Limits per turn | 3 identical tool calls, 25 tool calls, 300,000 tokens, 10 minutes # default | a turn that reaches one stops with a note |

Test Connection checks the form's values layer by layer (DNS, TCP, HTTP and TLS, authentication,
tool use) without saving them and without cluster data. DNS lists the addresses the endpoint's name
resolves to, IPv4 and IPv6, the first four and a count of the rest; it does not say which server
answered (the system's resolver does not tell). An IP address or `localhost` passes at once
("nothing to resolve"). TCP then dials those addresses in that order, 5 s for all of them, and names
the one that answered and any refused before it. With a proxy set for the endpoint in the
environment (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`; never for localhost), DNS and TCP check the
proxy, which resolves the endpoint's name itself; the proxy's user and password are never shown
([D-10](decisions.md#d-10-built-in-agent) item 8).

In the chat column, the pencil at the end of the line under each of your messages opens it for
editing (since 2026-10-15, F9): Enter or Send sends it again, and that message and everything after
it are gone from the chat, from what the model is sent and from the kept conversation (the audit
log keeps it); sent unchanged, it retries a failed or stopped turn. The resent message attaches the
view and selection the input's chip shows then. While a turn runs the pencil is dimmed: stop the
turn first. The model is not told what was cut, so where you allowed agent writes for the cluster,
a write of the cut part can run again unasked
([H-100](../security/agents-and-mcp.md#h-100-a-cut-parts-tool-calls-leave-the-models-view)).

### A cluster's right-click menu

Right-click a cluster's hotbar tile, its row in All Clusters or on the welcome page, or the
navigator's root (the shown cluster). The menu holds what applies to that cluster: Connect (Show for
a connected one; not on the root), Settings…, Add to Hotbar or Remove from Hotbar, **Copy Kubeconfig
Path**, then Reconnect, Disconnect or Cancel Connecting, and Remove from Cogpit…; a tile's menu ends
with Move Up, Move Down and Remove from Hotbar, a row whose connect failed with Show Connection
Updates….

**Copy Kubeconfig Path** (since 2026-10-24, the owner's VW13,
[D-25](decisions.md#d-25-copying-values)) puts the whole path of the kubeconfig file the cluster
comes from on the clipboard, connected or not, and a toast names it ("Copied the kubeconfig path
/Users/…/.kube/config"). The demo cluster and a cluster without a file of its own (without a
configuration directory, the contexts merged from `$KUBECONFIG` or `~/.kube/config`) have no such
item. It is no command: agents read the path in `cluster_contexts` (`file`) and
`cluster_settings_get` (`kubeconfig`). Cluster Settings › General copies it too, by the path's copy
icon.

### Several clusters connected

Since 2026-10-16 ([D-69](decisions.md#d-69-several-clusters-connected-at-once)). Showing another
cluster leaves the one you were on connected. Click a connected cluster's hotbar tile (or press its
F-key), its row in All Clusters or on the welcome page, or one of its tabs (in the tab bar with
Mixed environments on; off, the bar shows the shown cluster's tabs only): it shows at once, from
Cogpit's cache, without asking the cluster or showing its login program again. A cluster that is not
connected connects and shows; the others stay connected. A connected cluster's menu (right-click on
its tile or row) says **Show** instead of Connect, and **Disconnect** there closes that cluster
alone. A connect that fails leaves the window on that cluster (since 2026-10-19, [D-97](decisions.md#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays)),
its dot red; Cogpit never shows another cluster by itself, so you never come back to a window that
shows a cluster you did not choose. The cluster shown before stays connected; click it to go back.

**A failed connect's error page** (since 2026-10-22, [D-114](decisions.md#d-114-the-alpha-walks-decisions-a-failed-connects-page-and-its-stages)):
whether you clicked the cluster or the start restored it, and whether or not one of its tabs was
open, the workspace shows the cluster's error page in the welcome page's look: "Could not connect
to <cluster>", that this is an error page, the cause in a sentence ("The client certificate file
/Users/…/client.crt does not exist") under the stage it failed in, then the connect stage by stage,
each with a tick, a cross, a ring for not reached or a dash for skipped:

| Stage | Says |
|---|---|
| Kubeconfig valid and complete | the context, its file, the server and how you log in; or which file is missing or what is wrong |
| Login by the credential plugin | only for a context with a login program: how long it took, or why it failed (it runs before anything is sent) |
| DNS: the API server's name resolved | the name and the addresses it gave; skipped for a server named by its address; behind a proxy the proxy's name, the proxy resolving the server's |
| TCP: the address and port reached | the address and port dialled (or the proxy's), or that it refused or did not answer within the limit |
| TLS handshake | the TLS version and against what the server's certificate is checked; or why it failed (an authority this context does not trust, another name) |
| API server: the login and the first request | `GET /version`'s answer with the server's version, the login accepted; or the 401, 403 or the time it waited |

**Connect Again** tries again (a login program you have not allowed yet is shown first);
**Show Connection Updates** opens the cluster's connection log with the whole error line by line.
Help › Welcome leaves the failed cluster and shows the welcome page. Agents read the same in
`ui_state`: `connection.error` is the cause, `connectFailure` holds the stages.

Showing another cluster while one connects (it waits for your login in a browser, say) does not end
that connect (since 2026-10-19, D-97): it goes on in the background, its dot blinking, its tooltip
saying what it waits for, **Cancel Connecting** in its menus. Nothing else says it when you leave it
(since 2026-10-22, the owner's AW16: before, an error toast "connect <cluster>: another cluster was
shown; the connect goes on in the background"). When it is done, a toast says
"Connected to <cluster> in the background." (or why it failed) and the window stays where you are;
click the cluster to show it. Clicking a cluster that connects in the background shows it connecting
and waits for that same connect, without starting the login a second time; connecting the cluster the
window waits for again (from the navigator's section, the palette or an agent) waits for the same
connect too and says nothing (since 2026-10-22; before, the first connect's wait ended with the
error toast "another cluster was shown").

Every dot of a cluster (its hotbar tile, its row in the navigator and on the welcome page, and the
status bar for the one shown; the top bar has none since 2026-10-21, D-54) says how it is; its
tooltip names the state, a click on it opens the cluster's connection updates:

| Dot | Means |
|---|---|
| filled green | live: the cluster shown, or one in the background whose lists are still kept (5 minutes after you left it): showing it is instant |
| filled green, blinking | connecting or reconnecting, in the background too |
| green ring | connected in the background, its lists released: showing it lists them again (seconds) |
| grey ring | connected in the background, its login lapsed and was not renewed: showing it or Log in again renews it |
| filled red | needs you: a connect or a login renewal failed |
| filled grey | not connected |

The dock shows the tabs of the cluster you look at, and the Audit Log and the Diagnostics, which
belong to no cluster. Another cluster's logs, shells and jobs go on behind it; showing that cluster
again shows its tabs with what they printed meanwhile, the tab that was active and the dock open
or collapsed as you left it. A tab an agent opens on a cluster you do not look at waits in that
cluster's dock. With more than one cluster connected, the Diagnostics tab (Help › Diagnostics) lists
what each one costs: its API requests, watches, objects and memory.

### When the connection breaks

Since 2026-10-16 ([D-58](decisions.md#d-58-standby-and-lost-connections)). Cogpit checks each
connected cluster when the Mac wakes, when its network changes (Wi-Fi, a VPN), when a watch breaks,
and every 15 seconds, the ones in the background too (D-69); two checks without an answer (one
after a wake) and the connection is **interrupted**. The banner is the shown cluster's; an
interrupted cluster in the background disables the changes to it only, and its state is in
`ui_state`'s `connections`:

- The cluster's dot blinks green, slowly (half a second on, half a second dimmed), everywhere it
  shows: the status bar, its hotbar tile, the navigator, the welcome page. No word
  stands beside it; its tooltip says **Reconnecting…**, since when, why and the next attempt, and a
  click on it opens the cluster's connection updates. Under Reduce motion (Cogpit's [preference](#preferences--app-reduce-motion)
  or macOS's) it stays a steady, paler green. A banner over the view says which cluster, since when, why, and when Cogpit tries
  next; **Reconnect** connects anew at once ([below](#reconnect-a-new-connection-from-the-kubeconfig-file);
  since 2026-10-16, the owner's F13: dots, never words).
- The views, the tabs, the drawer and the dock stay as they were, showing the cluster as of the
  interruption. A view whose objects were not read before (opened since, or a cluster shown again
  after its lists went cold) loads nothing until the cluster is back and says so: "The pods were
  not read before the cluster stopped answering; they load once it answers again." (after an
  expired login: "…before the login expired; they load once it is renewed."), without an item
  count, "syncing…" or the download button; the namespace filter offers only that cluster's
  namespaces, none while they were not read (since 2026-10-22). Everything that would change the cluster is disabled, its menus saying
  "connection interrupted" and the selection bar's greyed Delete "Delete — connection interrupted"
  in its tooltip; an agent's change is refused the same way, and its reads are marked as
  the cluster's state of then.
- Cogpit reconnects by itself: at once on a wake or a network change, otherwise after 1, 2, 4, 8, 16,
  then every 30 seconds, for as long as the window is open. Back, the dot stops blinking (after
  three blinks at least, so that a quick reconnect is seen too) and stays green, a notification says
  how long it was away, and the views catch up. Connecting to a cluster blinks the same way.
- A followed log goes on by itself after a line `[cogpit] ── no connection 11:00:05 – 11:10:07 CEST
  (10 min 2 s) · the log was read again from its last line: what was written meanwhile follows ──`,
  from where it stopped: the times are when the last line arrived and when the connection was back,
  with seconds and your time zone (the log's own lines may print another), and the lines the
  container wrote meanwhile come after it. If the log no longer holds its last line (the pod was
  deleted and created again under its name, as a Stateful Set's pods are, the container restarted,
  or the kubelet rotated the log), a second line says so, `[cogpit] the log read again does not
  hold its last line: …`, and what follows is the log as it is now; in a workload's tab each line
  still comes once. Cogpit's own lines in a log (this one, a login that
  lapsed, a log that could not be opened again) are orange, apart from the container's. A port forward waits and connects again at once. A pod shell, a node shell or an attach
  ends (a process cannot be resumed): its tab says so and offers **Reconnect**, which opens a new
  one in the same tab once the cluster answers. A local terminal is not affected.
- A drain or a Helm install, upgrade or rollback that was running stops, as after a crash (a drain's
  tab says so); after the reconnect its banner offers to finish or dismiss it.
- A change you made just as the connection broke may have reached the cluster or not: after 30
  seconds without an answer it ends with "outcome unknown", and the view shows the object's state
  once Cogpit is back.

When the cluster rejects your login (it expired while the Mac slept) Cogpit lets the context's
login program renew it once by itself; if that does not help, or the program waits for you (a
browser login), the dot turns red at once (its tooltip: **Login expired**), and the banner's **Log in again**
runs the login program, browser included, keeping your views; **Reconnect** beside it connects anew
from the kubeconfig file. Cogpit never runs the program again on its own meanwhile. A login program
that changed since you allowed it is shown to you first, as at a connect, and the context then
reconnects with its tabs kept. A context without a login program (a client certificate or a token
in the kubeconfig) can only be renewed in its kubeconfig file: the banner says so and offers
**Reconnect**, and once the file holds another login Cogpit reconnects by itself
([below](#reconnect-a-new-connection-from-the-kubeconfig-file)).
After a reconnect and after Log in again Cogpit asks the API server once more who you are, so the
navigator's root names the user the renewed login belongs to (D-68). A login program that waits is
ended after 5 minutes ([below](#a-login-that-waits-cancel-connecting-and-the-5-minute-bound)).

### Reconnect: a new connection from the kubeconfig file

Since 2026-10-17 ([D-81](decisions.md#d-81-reconnect-and-what-log-in-again-does), the owner's F28 and
F29). A connection keeps the login it read from the kubeconfig when it connected: a client
certificate you renewed in the file reaches it only through a new connection. **Reconnect** makes
one, from the kubeconfig file as it is now, without closing the cluster's tabs:

- Where: the ↻ on a connected cluster's row in the list of all clusters (the navigator's All
  Clusters), the **Reconnect** button above the cluster Overview's charts, the Cluster menu and every
  menu of a cluster beside Disconnect (its row, the navigator's root, its hotbar tile, its row on the
  welcome page), the connection banner, the Connection Updates dialog and the command palette.
- What happens: Cogpit connects anew as at a connect: the file read again, an expired token of a
  login program renewed by the program (one you allowed; a changed one is shown to you first). The
  old connection goes on until the new one stands: the cluster's dots blink meanwhile and its items
  say **Reconnecting…**. Then the tabs, the view, the filter, the selection and the drawer stay and
  the views read the new connection (a moment of loading); a log tab opens its stream again; a shell,
  attach or node shell ends, its tab saying so with **Reconnect** for a new session; a local
  terminal runs on; port forwards listen on the same local ports again. A reconnect that fails leaves
  the cluster as it was and says why (a notification and the connection updates).
- Not while a job (a drain) or a Helm action runs on the cluster: it would stop it; Reconnect says so.
- **Log in again** reconnects too when the context has no login program, or when its login in the
  kubeconfig file changed since the connect.
- A login the cluster rejects, of a context without a login program, reconnects by itself once its
  kubeconfig file holds another one (Cogpit looks at the file every 2 seconds): renew the certificate
  in the file and the cluster comes back. A connection that still works is left alone when its file
  changes; its new login is used when the old one ends.
- Agents may reconnect a cluster too (`cluster_reconnect`), except one whose login expired while its
  context logs in through a login program: that is Log in again, yours, in the window.

### A login that waits: Cancel Connecting and the 5-minute bound

Since 2026-10-16 night ([D-73](decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound),
the owner's finding: a browser login he did not finish kept the cluster blinking with no way out).
A context whose login program (an OIDC plugin such as kubelogin or pinniped) opens your browser
waits for you to finish there. While it waits:

- The connecting workspace says so under "Connecting to …": **Waiting for the login through
  <program> since 14:28; it may wait for you in your browser. Cogpit ends it at 14:33.**, with a
  **Cancel Connecting** button. The cluster's dot keeps blinking; its tooltip says the same.
- **Cancel Connecting** is also in the window's Cluster menu and in every menu of the cluster (its
  navigator rows, its hotbar tile, its welcome-page row) in place of Disconnect while it connects,
  a cluster connecting in the background (restoring the last session, or one you left to show
  another cluster, D-97) included. It ends the
  connect at once and the login program Cogpit started for it; the cluster is not connected, and
  its connection updates say "cancelled". No toast says it: you cancelled it yourself (since
  2026-10-22, the owner's AW15; before, an error toast "connect <cluster>: connecting cancelled").
  **Connect** starts the login again, a new browser tab
  included. A browser window the program opened stays open: close it yourself. A login program
  that is a wrapper script handing over to the real program (`exec`) is ended too (since
  2026-10-19). When Cogpit cannot find the program's process (two logins that start at the same
  moment, a helper the program left behind that keeps its output open), it only stops waiting,
  and the connection updates say so: "Login cancelled: Cogpit stopped waiting for <program>; its
  process was not found and may run on" (before 2026-10-19 they said "Cogpit ended <program>"
  either way). A connect you start then waits behind that login until its process ends, with no
  end time, and says so (since 2026-10-20): **Waiting behind the login through <program>, running
  since 14:28, which Cogpit stopped waiting for and could not end. It has no end time: it lasts
  until its process ends. Cancel Connecting stops this wait; that login goes on.** The same shows
  when a login passes its 5 minutes and Cogpit cannot find its process: the connect then waits on
  the same way instead of failing.
- A login program that has not finished after **5 minutes** is ended by Cogpit: the connect fails
  with "the login through <program> did not finish within 5 minutes; Connect starts it again" (the
  dot red). The 5 minutes are fixed.
- A connected cluster whose login is renewed (D-58) waits the same way: its tooltip says what it
  waits for, Disconnect ends the program, and after 5 minutes the login is **Login expired** with
  "…; Log in again starts it again".
- After Cogpit ended a login program, it does not start it again by itself: only Connect or Log in
  again does, so no browser tabs pile up. A connect you cancelled starts no login program later
  either, when the login it waited behind ends (since 2026-10-19; before, each cancelled connect
  started one more).
- The connect's own limit (15 s for the API server's first answer) no longer counts the time you
  take in the browser.

Agents see it in `ui_state`: `connection.waitingForLogin` (`program`, `since`, `until`) while a login
program runs, and while connecting `connection.reason` "waiting for the login through <program>; it
may wait for you to log in, in a browser". Behind a login Cogpit stopped waiting for and could not
end, `waitingForLogin` has `runsOn: true` and no `until`, and the reason is "waiting behind the
login through <program>, which Cogpit stopped waiting for and could not end; it has no end time". `cluster_disconnect` of a connecting cluster (its
`context` for one connecting in the background) cancels the connect as the window's Cancel
Connecting does; agents may run it without asking you (it stops only what your connect started).
`cluster_connect` and `hotbar_openCluster` answer `connecting` when another cluster was shown while
they waited, `ui_tab_select` and `ui_setNavigatorScope` the UI state without an error (since
2026-10-22; before, those three an error): the connect goes on in the background, and its end is in
`ui_state`'s `connections` and its connection updates. `cluster_connect` of the cluster the window
waits for already waits for that connect and answers with its end. A connect cancelled while it waits (Cancel
Connecting, `cluster_disconnect`) answers an error with the code `cancelled` that says so, not
`failed`, and asks the agent not to connect it again unless you ask.

### Connection Updates: the connection log

Since 2026-10-16 ([D-66](decisions.md#d-66-the-connection-log)). Every connect, reconnect, login
and disconnect of a context is written down step by step, the ones that worked too, and kept for
7 days (at most 1 MiB per context; the file is in the [naming table](#naming-conventions)).
**Connection Updates: <context>** shows it: a click on a cluster's status dot opens it (and, as
before, the warning icon on the welcome page and Show Connection Updates… in a navigator row's
menu), for any context, connected or not; one with nothing recorded says so.

- The newest attempt comes first, with its kind, start, outcome and duration (green when it
  worked, red when it failed, "running…" while it runs, "cancelled" after Cancel Connecting); its
  lines follow in the order they happened: time of day, INFO, WARN or ERROR, the text and how long
  the step took. A login program that waits writes when it began and when Cogpit ends it, then how
  long it took or that Cogpit ended it (D-73).
- A connection that was lost has an entry of its own, **Interruption**, with what went wrong (in
  orange: "the connection was lost"); the **Reconnect** after it is the entry above, with its own
  steps, ending in "Reconnected to … after 55 ms" when it worked (since 2026-10-19, the owner's
  WF37: before, the reconnect's entry began with the interruption's lines and read as if it ended
  in them). Durations are written one way everywhere: "55 ms", "2.4 s", "10 min 2 s".
- What you did yourself is not an error: Cancel Connecting writes INFO lines and "cancelled",
  removing a kubeconfig source or Remove from Cogpit a disconnect (since 2026-10-19).
- A connect lists the kubeconfig file and context, the API server, how you log in (a client
  certificate by its name and expiry, a token "not shown", a login program by its name and
  whether you allowed it), how the server's certificate is checked, the proxy, the first answer
  with the server's version, discovery (from the disk cache or the server), the kinds the cache
  offers, the heartbeat, and the access reviews' first answer.
- A failure adds what went wrong in detail: its class (DNS, a refused or timed-out connection, the
  server's certificate with its subject, issuer, validity and names, the login program, a 401 or
  403 with the API server's message), the error chain, the limit a timeout ran into, and, when a
  login program failed, what it wrote to its standard error, scrubbed of tokens and passwords and
  cut to 4 KiB.
- While the dialog is open it follows the log: a connect or reconnect that runs adds its lines as
  they come. **Copy** puts every attempt shown on the clipboard as plain text, for a bug report.
- Each connect shows its stages above its lines (since 2026-10-22, D-114): the same table as on a
  failed connect's error page ([above](#several-clusters-connected)), for the ones that worked too;
  Copy writes them under "Stages:". Cogpit measures them on the connect's own first request: it
  opens no connection or lookup of its own for them.
- Agents read the same log with `cluster_connectLog`.

### Choosing files: the system's panels

Every file chooser of the window is the operating system's own on macOS (since 2026-10-16,
[D-72](decisions.md#d-72-native-file-panels-and-macos-folder-access)): a sheet slides out of the top of Cogpit's window.

| Chooser | Where | What it takes |
|---|---|---|
| Sync Files and Folders… | Cluster menu, the navigator's "+", the welcome page, Kubeconfig Sources | kubeconfig files and folders together, several at once (⌘-click), hidden files listed (`.kube`); the button "Sync"; opens in your home folder; ⌘⇧G types a path |
| Choose Image… | Cluster Settings › General › Icon, Custom icon › Customize… | one PNG, JPEG or SVG file |
| The folder buttons | Cluster Settings › Terminal (working directory), Preferences › Kubernetes (directory for binaries) | one folder, the button "Choose" |
| Add Folder… | Preferences › Kubernetes › Program folders | one folder, hidden folders listed, the button "Add"; a read-only bookmark |
| Choose… | Preferences › Agent, a connection's CA file | one `.pem`, `.crt` or `.cer` file |
| The downloads | a table's CSV, the Create tab's save, a log tab's download | a file name, proposed; macOS asks before it replaces a file; a new file is readable by you only |

**macOS's privacy prompts.** What you pick in these panels Cogpit may read without macOS asking.
Cogpit keeps a bookmark of the kubeconfig files and folders, folders and CA files you picked
(`bookmarks.json`, below), so that the next start can open them again; whether that keeps macOS
from asking after a restart is not known yet. A prompt that still comes ("Cogpit would like to
access files in your Downloads folder") names the reason: "Cogpit reads the kubeconfigs, folders
and certificate files you chose in your Downloads folder. Programs Cogpit starts for you, such as
a terminal or a login plugin, ask through Cogpit too." It comes once for the release; a build of
`make app` or `make build` is a new app to macOS after every build and asks again. A terminal or a
credential plugin that reads a protected folder asks under Cogpit's name, and once you allowed
Cogpit a folder they read it without asking ([H-116](../security/file-access.md#h-116-programs-cogpit-starts-act-with-cogpits-folder-access)).
Your answers are in System Settings › Privacy & Security › Files and Folders; `tccutil reset
SystemPolicyDownloadsFolder io.guided-traffic.cogpit` # example asks again.

On Linux and Windows the choosers are Fyne's dialogs (the system's panels there are not built), and
Sync Files and Folders is Cogpit's own browser for files and folders together, one at a time. On
Windows Fyne's dialogs speak the system's language ("Speichern" on a German Windows) while the rest
of Cogpit speaks English, the folder chooser lists no drives to go to, and a chosen path shows with
backslashes (since 2026-10-21, D-111).

### Alerts: the pill and the alert board

The cluster's Alertmanager, when the Alertmanager integration is on for it (Check all in its question turns it on), is read
by Cogpit ([D-77](decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)); Cogpit
installs none.

- **Where it comes from.** Cogpit looks among the cluster's services for one named as
  kube-prometheus-stack, kube-prometheus, the Prometheus Operator or the alertmanager chart name
  their Alertmanager (`monitoring/kube-prometheus-alertmanager` # example), and asks it once every
  30 seconds, through the API server, for the alerts that fire and are neither silenced nor
  inhibited; only while the cluster is the one shown. Your user needs `get services/proxy` on that
  service. When it runs under another name, set **Alertmanager service address**
  (`namespace/service:port`), **Use HTTPS** and **Path prefix** in the cluster's Settings ›
  Alertmanager (its own section since 2026-10-18); empty finds it by name again. The line under the
  address says which one is in use. The board's settings link opens that section.
- **The pill,** left of the bell, a warning sign the bell's size and the number in the lists' text size: red while an
  alert is critical, orange while one is a warning (also `major` and any other or missing
  severity), yellow while one is info, green with `0` while the Alertmanager answers with none; its
  number is every alert, `99+` beyond 99, and the tooltip counts them by severity. Alerts of
  severity `none` and the meta alerts Watchdog and InfoInhibitor are left out. Grey with `warn`
  means nothing answers: none found, not allowed or not answering (the tooltip says which); grey
  with a dash while Cogpit still looks for it. When the Alertmanager stops answering, the pill keeps
  its last count for 5 minutes and its tooltip says since when, then turns grey. The pill is not
  there while the integration is off for the cluster shown.
- **The board,** a click on the pill or Alerts in the command palette (⌘⇧P): a fold per severity, worst first, in each a
  card per alert name with its count (the pointer on a card's head shows a copy icon right after its name);
  per alert its summary (a click shows the description), how
  long it has fired and a Runbook link, and under it the namespace (a click narrows the board to
  it) and the object it is about (a click opens it). The search and the namespace filter work as
  in the lists. Nothing on the board changes Alertmanager: silences stay in Alertmanager's own UI.

### Kyverno's policy violations

While the Kyverno integration is on for a cluster (Check all in its question turns it on; Cluster Settings › Integrations)
and the cluster serves Kyverno's policy reports (`wgpolicyk8s.io/v1alpha2` PolicyReports and
ClusterPolicyReports, which Kyverno 1.10 and later writes for every object its policies check),
Cogpit marks every object a report names with a violation
([D-79](decisions.md#d-79-kyvernos-policy-violations-on-the-objects)):

- **What counts:** a rule's result `fail`, whatever the policy's mode: an Audit policy's, and an
  Enforce policy's on an object that existed before it (Kyverno finds those in its background
  scans); and `warn`, a failed rule of a policy its author marked as not scored. An `error` (Kyverno
  could not apply the rule), `pass` and `skip` mark nothing; the report's own drawer lists them.
- **In the lists:** the warning icon in the warning column, its tooltip "Kyverno: 2 policy
  violations (require-team-label, disallow-latest-tag)". Pods, Nodes, Cron Jobs and the other views
  with a warning column of their own show it there, the reasons joined; every other view, custom
  resources included, gets the warning column after Name while one of its objects violates a
  policy. Sort by it to put the violations first, or search "Kyverno".
- **In the drawer:** "Policy violations", a section per violation: the policy (a link to it while
  the cluster serves its kind: a ClusterPolicy, a Policy, a ValidatingPolicy), the rule, the
  result, the message, severity, category, how Kyverno found it (background scan, admission review)
  and when.
- **The reports themselves:** Custom Resources › wgpolicyk8s.io › Policy Reports and Cluster
  Policy Reports, with Kyverno's columns (Kind, Object, Pass, Fail, Warn, Error, Skip) and every
  result in the drawer.
- **Agents:** `kyverno_violations {context?, namespaces?, policy?, limit?}` lists every violation
  with its object's reference; `object_get` carries the drawer's sections. Policy messages are
  text from the cluster: agents get them as data
  ([agents and MCP › H-11](../security/agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)).
- **Off:** nothing is read and nothing shows. A user who may list the reports only in some
  namespaces gets them for the namespaces the cluster's settings or the namespace filter name.
- **Not read:** Kyverno's `openreports.io` reports (a Kyverno set up to write them in place of
  wgpolicyk8s.io marks nothing), other policy engines' reports.

Cogpit reads with one watch per report kind and keeps only the violations of each report in
memory. `make kind-kyverno` installs a Kyverno with test policies into the kind cluster to try it
([hack/kind](../../hack/kind/README.md#kyverno)).

### A namespace stuck in Terminating

A namespace you delete stays Terminating for as long as objects in it remain; usually an object
whose finalizers no controller removes (its controller is gone, or cannot reach what it cleans up).
Its drawer then shows **Blocking Deletion** right under Properties
([D-98](decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)):

- **What it lists:** for each kind Kubernetes' namespace controller names on the namespace (its
  condition `NamespaceContentRemaining`), the objects of that kind there that are being deleted and
  held by finalizers, each as "Config Map  walk-cm" (a link to the object), a **Remove Finalizers**
  button, and its finalizers under it. Objects of the kind without finalizers are counted ("1 more
  without finalizers, not gone yet").
- **What you may not see:** a kind you may not list in that namespace is a row from the
  controller's own count, "1 remaining; you may not list secrets in walk-fin": the namespace object
  names kinds and counts to anyone who may read it, never the objects. A kind Cogpit has no view of
  is counted the same way.
- **Discovery failed:** when the controller cannot list every kind the server serves
  (`NamespaceDeletionDiscoveryFailure`), the API services that are not available are named with
  their reason and the Service that serves each, a link (usually a metrics or webhook backend that
  is down).
- **It follows the cluster:** an object whose finalizers go vanishes from the list by itself; when
  nothing is left, the drawer says so, and the namespace controller removes the namespace by itself
  within seconds. No second Delete is needed.
- **Remove Finalizers** (in the list, the row menu and the drawer's toolbar of an object being
  deleted that holds finalizers, between Edit and Delete): confirmed every time, naming the object,
  each finalizer it removes in red and "Skips the cleanup of the controllers that own them (a cloud
  disk or a DNS record may stay)". It removes exactly the finalizers the dialog named, from exactly
  that object: if one was added or removed meanwhile, nothing is removed and Cogpit says so. It is
  never offered on a namespace, and Cogpit never forces a namespace's own finalizer.
- **Agents** read the same section in `object_get`, each row's `object_removeFinalizers` action
  with its params; their request always waits for your confirmation.
- **Reads:** while the drawer shows such a namespace, each kind named is watched in that namespace
  only, names and finalizers only (no values); nothing when it closes.

### Debug containers

**Debug** (the magnifier in a pod's drawer, after Shell in its row menu, or the command palette)
adds an ephemeral container to a running pod, as `kubectl debug -it <pod> --target=<container>`
does ([D-102](decisions.md#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)):

- **The dialog** names the context, the namespace and the pod, and asks two things: **Target**, the
  container whose processes the debug container sees (the pod's first preselected; None for no
  shared processes), and **Image**, an ordinary field you can edit, select, copy and paste into,
  filled with the image Preferences › Kubernetes › Debug container image names, shown by its short
  name (`nicolaka/netshoot:v0.16`: no `docker.io/`, no digest). Under them it says what the
  container sees of the target ("app's processes and files") and the image's digest, or that the
  image is a floating tag. While the field shows the configured image's short name, that image runs
  as configured, pinned by the digest the Digest row shows; a name you change runs exactly as typed,
  and without `@sha256:…` it is a floating tag (since 2026-10-24, the owner's R30). The dialog is the confirmation: **Debug** adds the container,
  and "This cannot be undone." means what it says (below). If the pod was replaced by another of
  its name while the dialog was open, nothing is added.
- **What is added:** a container `debugger-<5 characters>` from that image with stdin and a
  terminal, sharing the target's processes. Then an **Attach** tab opens on it, which says
  "Waiting for debugger-… to start…" while the image is pulled and is its shell once it runs
  (netshoot's zsh). Closing the tab detaches; the shell runs on, and **Attach**, **Shell** and
  **Logs** in the pod's menus reach it again. `exit` in the shell ends it.
- **How long it lives:** as long as the pod object. A crash or liveness restart of the pod's other
  containers keeps it; a deleted or replaced pod (a rollout, an eviction, a lost node) takes it
  away. It is never restarted itself: once its shell exits it stays Terminated, with its log, and
  Kubernetes offers no way to remove or change it.
- **What you see:** in the Pods list's Containers column a blue square per ephemeral container after
  the others, filled while it runs or starts, a blue outline once it ended, red while its image
  cannot be pulled; the ones still running first; at most 12 squares, "+N" for the rest. The
  tooltip says "debugger: ephemeral, running, target app". The ratio ("1/1") counts only the pod's
  containers, as kubectl's READY. The drawer lists them under **Ephemeral Containers**, each with
  its **Target**. A pod's log tab lists them under "Ephemeral containers"; the logs of a Deployment
  or another controller show the template's containers only.
- **Refusals:** Debug is greyed on a pod that has ended or is being deleted, and "not permitted"
  when you may not `patch pods/ephemeralcontainers` or `create pods/attach`. The cluster's own
  refusals are shown as it words them: a namespace that enforces the restricted Pod Security level
  refuses netshoot ("violates PodSecurity …"), an admission policy (Kyverno) may refuse it too.
- **Agents** run `pod_debug` with an image and a target of their choice; it always waits for your
  confirmation, with the same rows.

**Preferences › Kubernetes › Debug container image:**

```json
{
  "debugImage": ""
}
```

| Field | Default | Meaning |
|---|---|---|
| `debugImage` | `""` # default: `docker.io/nicolaka/netshoot:v0.16@sha256:b09d9b21381f47a79b3cbcb30da25266dc17186ea00ae65e99fdc51396f48e70` | the image Debug offers; best pinned by digest (`repository:tag@sha256:…`); a floating tag works and is named as one |

The field shows an image by its short name, the digest behind it: the default's,
`nicolaka/netshoot:v0.16`, as its placeholder, a set image without `docker.io/` and its digest. While
the text is that name the setting keeps the image as it is, digest included; the default's name typed,
or an empty field, sets the default (which follows Cogpit's releases); a changed name is kept exactly
as typed. The line under the field says whether the image in effect is pinned or a floating tag.
Cluster Settings › Node Shell › Image does the same with the node shell's image, `alpine:3.24` the
default's name ([D-37](decisions.md#d-37-the-node-shell)); the confirmations of a node shell and of an
agent's Debug name the whole reference and its digest.

Security note: a debug container with a target reads that container's processes and, through
them, its files, the Secrets it mounts included, as root with netshoot
([privilege footprint › Debug container](../security/privilege-footprint.md#debug-container)). Only
you, in Cogpit's window, set the image; agents and the CLI may only reset it.

### The Kubernetes version and upstream support

The navigator's root shows the cluster's Kubernetes version, the API server's as its connect read
it (`v1.36.1` # example), on a line under your user, while the cluster is connected
([D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)). Its colour says how
upstream supports its minor, by the Kubernetes project's own release dates only:

| Shown | When | Tooltip (examples, a German computer's dates) |
|---|---|---|
| `v1.36.1` muted | supported | Upstream support until 28.06.2027 (253 days) |
| `v1.34.2` amber | in maintenance mode, the last two months before the end of life | Upstream support until 27.10.2026 (9 days), maintenance mode since 27.08.2026 |
| `v1.33.9 ⚠` orange | past the end of life | Upstream end of life since 28.06.2026 |

Dates are in your computer's format; the tooltip's second line says the day of the release table
this Cogpit carries ("Upstream dates as of …"). A minor newer than that table gets an estimate,
marked "about … estimated: this Cogpit's table ends at 1.37": its end of life on the 28th of the
month fourteen months after its release month, the release four months per minor after the
table's newest (accurate to weeks; checked against every minor since 1.19).

- **Managed clusters:** only upstream's dates count. EKS, GKE, AKS, STACKIT SKE and others support
  a minor longer or shorter than upstream; Cogpit does not know their dates and does not tell one
  distribution from another.
- **Nodes:** the Nodes list's Version column marks a kubelet the same way (⚠ past its end of life,
  amber in maintenance mode), and ⚠ a kubelet outside the version skew policy against the API
  server: newer than it, or more than three minors behind (two for a kubelet older than 1.25). The
  cell's tooltip says which; the node drawer shows "Upstream support" and, outside the policy,
  "Version skew" under the Kubelet version.
- **Agents** read the same: `ui_state`'s `clusterVersion` (version, minor, released,
  maintenanceFrom, endOfLife, estimated, state, daysLeft, the table's newest minor and day),
  `view_rows`' and `resources_list`'s `tips` per node, `object_get`'s drawer items.
- **No connection for it:** the table is built into each release from upstream's files on the
  release's day; Cogpit fetches nothing at run time. `make k8s-eol` refreshes the committed table
  ([releasing](releasing.md#the-kubernetes-release-table)).

The gear before the root's dot opens the cluster's Settings, as ⌘⇧, does
([D-92](decisions.md#d-92-a-gear-in-the-navigators-cluster-row)).

### Escape

Since 2026-10-22 (the owner's alpha walk, [D-116](decisions.md#d-116-escape-closes-the-innermost-thing)),
Escape closes the innermost thing open, and only that; press it again to go one step further out.

| Where you are | Escape |
|---|---|
| An open dropdown's list | closes it without a choice; the field is no longer highlighted |
| A dialog (a confirmation, Rollback, Scale, a create dialog, Connection Updates, About, …), whatever field has the keyboard | is its Cancel, or its Close where it has none; the drawer behind it stays open |
| A pop-up list (the namespace filter, the status filter, the columns, the agent's history) or the palette, the bell's list, the MCP details | closes it |
| A dock tab's find bar (its fields or the editor) | closes the bar; the editor gets the keys, the find text stays for the next ⌘F |
| A list's search (⌘F) | clears it; the list gets the keys |
| The logs tab's search, Argo CD's activity filter | clears it |
| A settings page (Preferences, Cluster Settings), a card over the window | closes the card (a text field on it keeps Escape; a menu or dialog opened from it closes first) |
| The list or the navigator | closes the drawer |

A selected text (a log, a toast, the agent's answer) drops its selection first, and the
navigator's search opened by ⌘P and the namespace filter's search clear a typed text first, as
before.

### Selecting and copying a log's, a notification's and the agent's text

Since 2026-10-19 (the owner's WF36 and WF41,
[D-99](decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)). In a **logs tab**:

| You do | It does |
|---|---|
| Drag over the lines | selects from where you pressed to the pointer, across lines; beyond the tab's top or bottom the lines scroll, faster the farther you go |
| ⇧-click | extends the selection from where it began to the click |
| Double-click | selects the word: the run of characters without a space |
| ⌘A (Ctrl+A) | selects every line the tab shows: of the pods and containers its selectors show, only the matching ones while Filter is on |
| ⌘C (Ctrl+C) | copies the selection: one line per line, as the tab shows them (names and timestamps as toggled), a wrapped line as one, a line cut on screen whole |
| Right-click | Copy, Select All, and Copy All Lines Shown (what Download would save) |
| Escape | drops the selection |

Selecting with the pointer turns **Follow** off, so the lines stay where you see them; turn it on
again with its check, and the selection stays on its lines as new ones arrive. Scrolling switches it
too (since 2026-10-22): five lines or more up from the newest line, with the wheel, the trackpad or
the scroll bar, turn Follow off and the log stays where you scrolled; scroll back down to the newest
line and Follow is on again. A smaller scroll up keeps following, that far behind the newest line.
New lines never turn Follow on by themselves. The Follow check works as before: off, the log stays
where it is; on, it jumps to the newest line and follows. The selection keeps
its lines through Wrap, a new width, a search and a filter; turning Resource names or Timestamps on
or off drops it. A tab keeps its newest 10,000 lines: when older ones go, the part of the selection
in them goes too. Copying shows no toast. `/` still jumps to the search.

In a **toast**, drag over its text or double-click a word and ⌘C copies; the copy icon left of the
× copies the whole message as it came, and is a checkmark for a moment. A toast stays while the
pointer is on it, its × or its icon, and while its text is selected; its full time runs again once
neither holds. In the **bell's list** a notification's text selects the same way, and its copy icon
shows after its time while the pointer is on it. Selecting is the window's alone: agents read a
logs tab's lines with `dock.read` and the notifications with `notifications.list`.

In the **agent's column** (since 2026-10-21) every block of a message selects on its own: a
paragraph, a heading, a list item, a table cell, a code block, a note, an opened tool row's
arguments and result. Drag over it or double-click a word, and ⌘C copies the text as written (a
wrapped line as one, inline code without its chip); Escape drops the selection. A drag stays in its
block; the copy icon under an answer copies all of it as Markdown. Links still open with a click.

### Commands and MCP tools

Every command is an MCP tool. The table of all of them, with each one's danger and parameters,
is generated from the registry: [`commands.md`](commands.md).

## Naming conventions

Names Cogpit generates or expects. Paths are macOS paths; `<user config dir>` is
`~/Library/Application Support`, `<user cache dir>` is `~/Library/Caches`. On Linux they are
`$XDG_CONFIG_HOME` (else `~/.config`) and `$XDG_CACHE_HOME` (else `~/.cache`), on Windows
`%APPDATA%` and `%LOCALAPPDATA%` (so `%APPDATA%\Cogpit` and `%LOCALAPPDATA%\Cogpit`), Go's own rule.
On Windows the "owner-only" modes below are neither set nor checked: the files live in the user's
profile, whose inherited ACLs give the user, SYSTEM and the Administrators access, and a temporary
file in `%TEMP%` inherits the same (the lead's policy, 2026-10-21,
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).

<details>
<summary>Names, paths and files</summary>

| What | Pattern | Example |
|---|---|---|
| Go module | `github.com/guided-traffic/cogpit` | |
| Binaries | `cogpit` (desktop app), `cogpit-cli` (headless CLI and MCP stdio bridge) | |
| App bundle | `Cogpit.app`, bundle id `io.guided-traffic.cogpit` | `bin/Cogpit.app` # example |
| Linux package | `cogpit`, the app id `io.guided-traffic.cogpit` (its launcher, icon, AppStream entry and Wayland app id), the window's `WM_CLASS` `Cogpit`, the polkit action `io.guided-traffic.cogpit.update`, since 2026-10-20 night ([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages); its files: [Install on Linux](#install-on-linux)) | `/usr/share/applications/io.guided-traffic.cogpit.desktop` # example |
| Windows setup | `Cogpit-<version>-windows-x64-setup.exe`, installing `cogpit.exe`, `cogpit-cli.exe` and `WinSparkle.dll` into `%LOCALAPPDATA%\Programs\Cogpit` (for every user `C:\Program Files\Cogpit`), the Start menu entry `Cogpit.lnk`, the uninstall entry `…\CurrentVersion\Uninstall\io.guided-traffic.cogpit` (HKCU, or HKLM's 64-bit view for every user), since 2026-10-21 on the branch `windows-release` ([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle); its files: [Install on Windows](#install-on-windows)) | `C:\Users\you\AppData\Local\Programs\Cogpit\cogpit.exe` # example |
| `cogpit-cli` in the app | `Cogpit.app/Contents/MacOS/cogpit-cli`, code-signing identifier `io.guided-traffic.cogpit.cli` ([D-16](decisions.md#d-16-the-first-real-release)) | `/Applications/Cogpit.app/Contents/MacOS/cogpit-cli` # example |
| Packages | `internal/<area>` | `internal/cache` # example |
| Command ids | `<kind-or-area>.<action>`, lowerCamelCase segments | `deployment.restart`, `ui.navigate` |
| MCP tool names | the command id with dots replaced by underscores | `deployment_restart`, `ui_navigate` |
| Kind keys (views, `kind` parameters) | the plural resource name for built-in kinds; `<plural>.<group>` for kinds from custom resource definitions ([why](architecture.md#45-kind-keys)) | `pods`, `services`, `gateways.gateway.networking.k8s.io` |
| Page keys (views that are not one kind's list, `ui.openView`'s `view`) | `<navigator group>/<page>` | `cluster/overview`, `workloads/overview`, `flux/dashboard` |
| Annotations the Flux actions set | `reconcile.fluxcd.io/requestedAt` (Reconcile, Resume) and, for a forced Helm release, `reconcile.fluxcd.io/forceAt`: the time in RFC 3339 with nanoseconds, as the flux CLI writes it | `2026-10-13T22:01:02.345678901Z` # example |
| Environment variables | `COGPIT_<NAME>`; the app reads one, `COGPIT_SYSTEM_TITLEBAR=1` (macOS's own title bar instead of Cogpit's); the others are the tests': `COGPIT_KIND_KUBECONFIG`, `COGPIT_KIND_CONTEXT`, `COGPIT_KIND_DRAIN_KUBECONFIG`, `COGPIT_KIND_DRAIN_CONTEXT`, `COGPIT_KIND_FALLBACK`, `COGPIT_PNG_DIR`, `COGPIT_SIGSTORE_LIVE`, `COGPIT_SPARKLE_FRAMEWORK`, and for a real cluster someone lends for a test run `COGPIT_REAL_KUBECONFIG` (its kubeconfig, passed to Cogpit alone) and `COGPIT_REAL_CONTEXT` (default: the file's current context), never set in CI (and a few that tests set for their own child processes); for measurements on a real cluster (read-only, skipped without them, never in CI) `COGPIT_REAL_KUBECONFIG`, `COGPIT_REAL_CONTEXT`, `COGPIT_REAL_ROUNDS`, `COGPIT_REAL_IDLE`, `COGPIT_REAL_THANOS` ([testing](testing.md#tiers)); for the built-in agent's local tests `COGPIT_LLM_TEST_URL`, `COGPIT_LLM_TEST_MODEL`, `COGPIT_LLM_TEST_KIND`, `COGPIT_LLM_TEST_KEY` (`TestLiveEndpoint` against a real endpoint over the demo cluster; skipped without them) | `COGPIT_LLM_TEST_URL=http://localhost:1234/v1` # example |
| Cogpit's `PATH` (where a credential plugin's programs are found) | the `PATH` Cogpit started with, then the built-in folders that exist (macOS and Linux), then Preferences › Kubernetes › Program folders (`programFolders`), each appended, never first ([program folders](#preferences--kubernetes-program-folders), D-120) | `/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/Users/me/.krew/bin:/Users/me/tools/bin` # example |
| MCP endpoint | `http://127.0.0.1:<port>/mcp`, a free port the first time, kept in `mcp-port` for every later start; another one, kept from then on, when the kept port is taken | `http://127.0.0.1:59449/mcp` # example |
| MCP bearer token | `<user config dir>/Cogpit/mcp-token`, owner-only (0600, directory 0700), kept across starts and the switch until you click Regenerate token in the MCP details (D-53); a file that holds no token Cogpit wrote, or that other users may read, is replaced at the next start | |
| MCP port file | `<user config dir>/Cogpit/mcp-port`, beside the token and with its rules: the port the next start binds (not with `--mcp-addr` and a port) | `59449` # example |
| MCP endpoint file | `<user config dir>/Cogpit/mcp-endpoint`, beside the token and with its rules: the endpoint's URL, for `cogpit-cli mcp`, written while the server listens and removed when it stops (on exit and when you switch the external MCP server off) | `http://127.0.0.1:59449/mcp` # example |
| MCP switch | `<user config dir>/Cogpit/mcp-switch`, beside the token and with its rules: `on` or `off`, the status bar's MCP switch, written only by a click in the window (or by hand, without one); the server is opt-in, so no file means off, and `cogpit-cli mcp` serves nothing but a note that the server is off unless it says `on` (D-49) | `on` # example; no file means off |
| Audit log | `<user config dir>/Cogpit/audit.log`, owner-only, one JSON line per agent-initiated command; read back newest first in Help › Audit Log and by `audit_list` (its last 4 MiB) | |
| Connection log | `<user config dir>/Cogpit/connection-logs/<32 hex digits>.jsonl`, owner-only (0600, directory 0700), shared by the app and `cogpit-cli` (since 2026-10-16, [D-66](decisions.md#d-66-the-connection-log)): one file per context, named by the first 32 hex digits of the SHA-256 of the context id (the id itself is inside); one JSON line per record (an attempt's begin with its kind and context, its lines, its end with the outcome and duration), appended as it happens. Attempts older than 7 days, and the oldest beyond 1 MiB per context, are dropped; a file nobody wrote for 7 days is deleted at a run's first connect. Read back by the Connection Updates dialog and `cluster_connectLog`; no credential in it, a credential plugin's standard error only after a failed login, scrubbed ([H-112](../security/credentials.md#h-112-a-credential-plugins-standard-error-in-the-connection-log)) | `~/Library/Application Support/Cogpit/connection-logs/4f1c0a9e7b2d6c35e8a1f0b9d2c47e61.jsonl` # example |
| Agent chat connections | `<user config dir>/Cogpit/llm-connections.json`, owner-only (0600, directory 0700): each LLM connection's id, name, kind (`lmstudio`, `litellm`, `openwebui`, `openai`), base URL, model, limits, private mark, whether it sends all tools (`allTools`, since 2026-10-15), its CA file's path (`caFile`, since 2026-10-15), whether a key was given and which one is the default; never a key ([D-10](decisions.md#d-10-built-in-agent)) | |
| Agent chat API keys | `<user config dir>/Cogpit/llm-keys.json`, owner-only (0600, directory 0700), on every platform: the connection's id to its API key, in plain text ([H-99](../security/credentials.md#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)); written when you save a key, a key removed with Remove Key or its connection; a file other users could access is made owner-only again at the next read, and the log says so. Never in a keychain since 2026-10-15 (the owner's decision): a key an earlier version kept in the macOS login keychain is not read, so its connection shows "no API key" until you enter the key once again; that keychain item (named `Cogpit LLM connection`, service `io.guided-traffic.cogpit.llm`) is left alone, and you can delete it in Keychain Access | `{"version":1,"keys":{"3f9a…":"sk-…"}}` # example |
| Agent chat conversations | `<user config dir>/Cogpit/chat-conversations.json`, owner-only (0600, directory 0700): the last 20 conversations per context with their items and the history the model is sent, every text with the cached Secrets' values replaced (`«secret <ns>/<name>/<key>»`), a text that could not be scrubbed `<withheld: …>`; the request bodies sent are never written, and never a conversation in which a turn ran over a private connection: it lives in memory only and is gone when Cogpit quits, and one an earlier version (v0.8.0 to v0.10.0) wrote is removed from the file at the next start ([H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)) | |
| Drain journal | `<user config dir>/Cogpit/drains.json`, owner-only (0600, directory 0700): the drains Cogpit started and has not finished, offered again on the next connect | |
| Node shell journal | `<user config dir>/Cogpit/nodeshells.json`, owner-only (0600, directory 0700): the node shell pods Cogpit created and has not seen deleted (context, namespace, name, UID, the id in the pod's `cogpit.io/node-shell-id` annotation); one a crash left is deleted at the next connect to its context ([H-71](../security/privilege-footprint.md#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)) | |
| Hotbar | `<user config dir>/Cogpit/hotbar.json`, owner-only (0600, directory 0700): the pinned clusters by context id, top to bottom, `{"version": 2, "clusters": [{"context": "kind-dev [.kube/config]"}]}` (example); an unreadable one is moved to `hotbar.json.unreadable` at the first change; a file of an earlier Cogpit with banks (format 1) is merged into one list at the start (the first bank's tiles, then each further bank's, each context once) and kept as `hotbar.json.v1` ([D-83](decisions.md#d-83-one-hotbar-and-a-drag-that-carries-the-tile)) | |
| A cluster's name in the window | the display name set in its Cluster Settings › General, else the part of its context's name after the last `@` (the whole name without an `@`, or with nothing after it), without the kubeconfig file; commands and `ui_state` keep the full context id ([D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)) | `kubernetes-admin@prod-eu [dev.yaml]` → `prod-eu` # example |
| Cluster settings | `<user config dir>/Cogpit/cluster-settings.json`, owner-only (0600, directory 0700): per context id what Cluster Settings sets (name, icon, accent colour, terminal, metrics, integrations, proxy, accessible namespaces, node shell, agent allowance, login renewal), at most 1,024 contexts; uploaded icons in `cluster-icons/`, since 2026-10-23 each with the image it was cut from (at most 1,024 pixels a side, D-119). A cluster removed from Cogpit (Remove from Cogpit, or its kubeconfig source removed) loses its settings, so added again it is a new cluster and is asked its integrations again; a file `$KUBECONFIG` names keeps them (since 2026-10-17 evening, the owner's walk, WF4) | |
| Last known users | `<user config dir>/Cogpit/cluster-users.json`, owner-only (0600, directory 0700): per context id the username the API server last confirmed and when Cogpit last saw it so, for the navigator's root while the cluster is not connected; never the groups, never the kubeconfig's user name; an unreadable one is moved to `cluster-users.json.unreadable` at the next write ([D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)) | `{"version": 1, "users": {"kubernetes-admin@prod-eu [dev.yaml]": {"username": "alice@example.com", "seen": "2026-10-16T09:12:00Z"}}}` # example |
| Bookmarks of picked files | `<user config dir>/Cogpit/bookmarks.json`, owner-only (0600, directory 0700), macOS (since 2026-10-16, [D-72](decisions.md#d-72-native-file-panels-and-macos-folder-access)): a security-scoped bookmark of each kubeconfig file and sync folder, folder and CA file you picked in the system's panels, with its path, its use (`kubeconfig`, `folder`, `caFile`) and its access; resolved at every start before the sources are read; a source's goes with the source, the newest 16 folders and CA files stay; removing the file removes the kept access ([file access](../security/file-access.md)) | |
| Tabs and column layout | `<user config dir>/Cogpit/workspace.json`, owner-only (0600, directory 0700): the tab bar's tabs (context id, view, filters, search, sort, history), each view's hidden columns and column order, and since 2026-10-15 the navigator's collapsed source groups (their folders' and files' labels, A63) and whether the navigator is hidden and the chat column shown (F7, [D-54](decisions.md#d-54-the-layout-toggles-and-the-update-button)), and since 2026-10-16 the clusters connected (`connected`, context ids) and the one shown (`shown`) ([D-41](decisions.md#d-41-what-survives-a-restart)); the layout comes back at every start, the tabs and the connections only while Preferences › App › Restore the last session is on ([D-69](decisions.md#d-69-several-clusters-connected-at-once) item 8); format version 1, a file of v0.17.0 loads; an unreadable one is moved to `workspace.json.unreadable` at the first change | `{"version": 1, "tabs": […], "active": 0, "connected": ["kind-dev", "prod-eu"], "shown": "prod-eu"}` # example |
| Window place | `<user config dir>/Cogpit/window.json`, owner-only (0600, directory 0700), since 2026-10-18 ([D-95](decisions.md#d-95-the-window-comes-back-where-it-was)): on macOS the window's frame in points (origin at the main display's bottom left), the display it was on (id, name, frame) and whether it was in full screen, written half a second after a change and at quit; on Windows and Linux its size and full screen, written when it closes; read at every start, whatever Restore the last session says; an unreadable one is replaced by the next change; deleting it gives the first start's window | `{"version": 1, "frame": {"x": 300, "y": 200, "width": 1000, "height": 700}, "display": {"id": 1, "name": "Built-in Retina Display", "frame": {…}}}` # example |
| Drafts of unsaved text | `<user config dir>/Cogpit/drafts/<32 hex digits>.json`, owner-only (0600, directory 0700): an Edit or Create resource tab's unsaved text, and since 2026-10-15 a Config Map's values edited in the drawer (type `configMapData`; since 2026-10-16 its keys added and removed too), removed when saved, reverted or discarded, offered again at the next connect of their context ([D-11](decisions.md#d-11-crash-stance), [D-55](decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)); never a Secret's values or Helm values | `drafts/3f9a…c2.json` # example |
| Helm chart repositories | Helm's own `repositories.yaml`, as the helm CLI resolves it: `$HELM_REPOSITORY_CONFIG`, else `$HELM_CONFIG_HOME/repositories.yaml`, else Helm's folder; read every 2 seconds, never written (add and remove repositories with `helm repo add` and `helm repo remove`, [D-29](decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)); credentials stay in memory | `~/Library/Preferences/helm/repositories.yaml` # default on macOS |
| Helm repositories allowed over http | `<user config dir>/Cogpit/helm-http-approvals.json`, owner-only (0600, directory 0700): the repositories whose user and password you let Cogpit send over plain http, each by its name, its URL without user, password or query values, a SHA-256 of its name, URL and user name and when; no credential ([H-40](../security/credentials.md#h-40-credentials-over-plain-http)) | |
| Helm repository indexes | `<user cache dir>/Cogpit/helm/indexes/<key>-index.yaml`, owner-only (0600, directory 0700); the key a hash of the repository's name and URL. Nothing goes into Helm's own cache | `~/Library/Caches/Cogpit/helm/indexes/3f2a9c61d0b4e8a7c5f19e2d4b6a8c0e-index.yaml` # example |
| Helm chart READMEs | `<user cache dir>/Cogpit/helm/readmes/<key>.json`, owner-only (0600, directory 0700): the README of a chart version whose archive the index's digest verified, as the chart's drawer showed it (at most 256 KiB), the last 256 kept; the key a hash of the repository's name and URL, the chart, the version and the digest | `~/Library/Caches/Cogpit/helm/readmes/5d0c3f9e2a7b41c8e6f0a19b3d7e2c45.json` # example |
| Helm chart icons | `<user cache dir>/Cogpit/helm/icons/<key>.png` or `.svg`, owner-only (0600, directory 0700): a chart's icon as Cogpit checked it (a PNG of at most 256 pixels a side, or an SVG that refers to nothing outside itself), used for 7 days, the last 2,048 kept; the key a hash of the icon's URL ([D-46](decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them)) | `~/Library/Caches/Cogpit/helm/icons/0b7e5d2c9a14f3e8b6c1d0a9e7f42c51.png` # example |
| Helm chart objects | kind `helmcharts`, named by the chart's id: `<repository>.<chart>`, every dot of the repository's name doubled, so that the first single dot separates the two (a repository's name: 1 to 63 letters, digits, `.`, `-` or `_`, the first a letter or a digit; another is listed as not used) | `bitnami.nginx`, `charts..example.nginx` for repository `charts.example` # example |
| App log | `<user cache dir>/Cogpit/cogpit.log`, and stderr | |
| Discovery cache | `<user cache dir>/Cogpit/discovery/<32 hex digits>.json`, owner-only (0600, directory 0700), shared by the app and `cogpit-cli` (since 2026-10-16, [D-65](decisions.md#d-65-discovery-cached-on-disk)): one file per API server and identity, named by a SHA-256 over the server URL, its TLS server name, the kubeconfig files, the context's cluster and user entry names and the user it impersonates (never a credential); it holds the server's URL and version, when the answer was fetched, the digest of the custom resource definitions it was checked against, and the groups and resources the server serves, no object. Reused for the same server version only, for 6 hours once the definitions confirmed it, else 10 minutes; dropped when the definitions change. About 140 KiB for a cluster with 230 custom resource definitions (measured 2026-10-16); deleting the folder costs the next connect its discovery requests, nothing else | `~/Library/Caches/Cogpit/discovery/9c1e4b7a2d5f8e0c3b6a9d2e5f8c1b4a.json` # example |
| Terminal kubeconfig | a temporary file per terminal holding only a context, `<temp dir>/cogpit-terminal-<random>.kubeconfig`, owner-only (on Windows in `%TEMP%`, whose ACL it inherits), deleted on close (on Windows retried for up to 2 seconds while a program the terminal started still holds it) ([why](../security/credentials.md#the-terminals-kubeconfig)) | |
| Downloaded `kubectl` | `<user config dir>/Cogpit/kubectl/<version>/kubectl` (0755, directories 0700; on Windows `%APPDATA%\Cogpit\kubectl\<version>\kubectl.exe`), or the directory for binaries set in Preferences › Kubernetes ([why](decisions.md#d-3-helm-and-kubectl-binaries)) | `~/Library/Application Support/Cogpit/kubectl/1.36.1/kubectl` # example |
| A terminal's own directory | `<temp dir>/cogpit-terminal-<random>/`, owner-only, removed on close, only when Cogpit puts a `kubectl` first: `bin/kubectl` (a link), `zsh/.zshenv` (zsh only), `bash/rc` (bash only, since 2026-10-14); on Windows only for a kubectl not named `kubectl.exe`: `bin\kubectl.exe`, a hard link or else a copy (a `kubectl.exe` has its own folder put first on `PATH` instead; since 2026-10-21, D-111) | |
| Node shell pod | `kube-system/cogpit-node-shell-<8 random characters>`, labels `app.kubernetes.io/name=cogpit-node-shell` and `app.kubernetes.io/managed-by=cogpit`, annotation `cogpit.io/node-shell-id=<random id>` (its journal entry's), deleted when the shell closes, else at the next connect to its context after a crash, else ended by its deadline, Cluster Settings › Node Shell › Maximum duration (2 hours unless the user sets 5 minutes to 24 hours; [why](../security/privilege-footprint.md#node-shell)) | `kube-system/cogpit-node-shell-k3v9q2mx` # example |
| Node shell dock tab | id `nodeShell-<n>`, title `Node shell: <node>` | `Node shell: worker-1` # example |
| Debug container | an ephemeral container `debugger-<5 lowercase characters>` in the pod, as kubectl debug names one, never a name the pod has; it stays as long as the pod ([why](../security/privilege-footprint.md#debug-container)) | `debugger-x7k2q` # example |
| Debug's Attach tab | id `attach-<n>`, title `Attach: <pod>/<container>`, as Attach's | `Attach: web-0/debugger-x7k2q` # example |
| Prometheus and Alertmanager settings | `<user config dir>/Cogpit/metrics.json`, owner-only (0600): per context the Prometheus you named (namespace, service, port, path prefix) and, since 2026-10-16, how it is asked when not the defaults (`format`: `prometheus-operator` or `cadvisor`; `keepEmptyContainers`; `https`); since 2026-10-17 the Alertmanager you named (`alertmanager`: namespace, service, port, path prefix, `https`, D-77); never the kubeconfig | `{"prometheus":{},"query":{"kind-dev":{"https":true}},"alertmanager":{"kind-dev":{"namespace":"monitoring","service":"alertmanager-operated","port":"web"}}}` # example |
| Demo context | `demo` (the synthetic cluster), development builds only; in a release `demo` is a kubeconfig context name like any other ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)) | |
| Release build tag | `nodemo` (the Makefile's `DIST_TAGS`; `make dist-app`, `make dist-linux` and `make dist-windows` check both binaries recorded it, `go version -m <binary>` shows `-tags=nodemo`) | |
| Fyne's thread check | the Makefile's `FYNE_THREAD_CHECKS=on\|off`: off adds Cogpit's build tag `nothreadcheck`, with which the app declares itself migrated to `fyne.Do` (`go version -m <binary>` shows `-tags=nodemo,nothreadcheck` for a release); empty: off for `make dist-app`, `make dist-linux` and `make dist-windows`, on for every other target; the repository variable `FYNE_THREAD_CHECKS` sets it for the release pipeline ([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)) | `make app FYNE_THREAD_CHECKS=off` # example |
| Local kind cluster | cluster `cogpit`, context `kind-cogpit`, kubeconfig `.cache/kind/kubeconfig` | |
| Release artifacts (planned) | `Cogpit-<version>-macos-universal.dmg`; a Windows setup for ARM64 | `Cogpit-0.1.0-macos-universal.dmg` # example |
| Deep links (macOS) | `cogpit://open?context=<context id>&view=<view key>&namespace=<namespace>&name=<name>`, the scheme registered in the app's `CFBundleURLTypes` (name `io.guided-traffic.cogpit`); audited as `deeplink.open` with origin `deeplink` | `cogpit://open?view=pods&namespace=team-a` # example |
| Release artifacts | `Cogpit-<version>-macos-<arch>.dmg` (signed releases; `universal` once the app is built for Intel too, planned), `Cogpit_<version>_darwin_<arch>.zip` (v0.4.0 and earlier, and builds without the signing secrets), `SHA256SUMS`; from the release after v0.32.0 on `cogpit_<version>_<arch>.deb` for Linux, `<arch>` `amd64` and `arm64` (D-108), each with both binaries; from the first release after the branch `windows-release` is merged `Cogpit-<version>-windows-x64-setup.exe` (D-111) with both executables and WinSparkle; `SHA256SUMS` over every file; nothing else since D-16's revision (2026-10-14): `cogpit-cli_<version>_<os>_<arch>.tar.gz` (`.zip` for Windows) only up to v0.6.0 | `Cogpit-0.5.0-macos-arm64.dmg`, `cogpit_0.34.0_amd64.deb` # example |
| Deep links | `cogpit://` ([D-47](decisions.md#d-47-deep-links-cogpit)) | |
| Update feed | the appcast every macOS app with the update key reads (`SUFeedURL`, [D-4](decisions.md#d-4-packaging-and-updates)) | `https://guided-traffic.github.io/cogpit/appcast.xml` # default |
| Sparkle's own state | the app's user defaults, written by Sparkle: whether it checks automatically (`SUEnableAutomaticChecks`, which Cogpit sets from its preference), the last check's time, a skipped version | `~/Library/Preferences/io.guided-traffic.cogpit.plist` # default |
| Update feed (Windows) | the appcast the Windows setup's `cogpit.exe` reads, built into it by `make dist-windows` (D-111); its items `sparkle:os="windows-x64"` | `https://guided-traffic.github.io/cogpit/appcast-windows.xml` # default |
| WinSparkle's own state | the registry key WinSparkle writes (a release you skipped, `SkipThisVersion`, which Cogpit reads to leave that release off the button; its own scheduled checks are off); the uninstaller removes it | `HKCU\Software\guided-traffic\Cogpit\WinSparkle` # default |
| APT repository (Linux) | the repository the package's `/etc/apt/sources.list.d/cogpit.sources` names, suite `stable`, component `main`, `Origin` and `Label` `Cogpit`, signed with Cogpit's APT key (Ed25519, fingerprint `BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`), the two newest releases of each architecture ([D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) | `https://guided-traffic.github.io/cogpit/apt/` # default |

</details>

Identifiers the documentation uses:

<details>
<summary>Identifiers</summary>

| Identifier | Meaning | Where |
|---|---|---|
| `G<n>` | A gap against Lens or a beyond-Lens target | [docs/parity/gaps.md](../parity/gaps.md) |
| `M<n>` | A milestone | [docs/parity/roadmap.md](../parity/roadmap.md) |
| `D-<n>` | A decision | [docs/developer/decisions.md](decisions.md) |
| `H-<n>` | An open security gap | [docs/security/](../security/README.md) |
| `P<n>` | A performance or API-load budget | [docs/developer/testing.md](testing.md#performance-budgets-proposed-not-measured) |
| `S<n>` | A gate criterion of the Fyne spike | [docs/developer/spike-fyne.md](spike-fyne.md#gate-criteria) |

</details>

## Make targets

| Target | What it does |
|---|---|
| `make build` | `bin/cogpit` (cgo, Fyne) and `bin/cogpit-cli` (pure Go), a development build with the demo cluster and Fyne's thread check on; `GO_TAGS=nodemo` builds the release's variant without the demo, `FYNE_THREAD_CHECKS=off` without the check, as a release scrolls ([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)) |
| `make run`, `make run-demo` | run against the kind cluster, or the demo cluster (development builds only) |
| `make check-nodemo` | the release build's variant (CI's lint job): `internal/demo` in neither binary with `-tags nodemo` and in both without, the packages with `nodemo` files linted with the tag, and their `TestNoDemo*` tests ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)) |
| `make test-unit-race`, `make test` | every test without a cluster, race detector on, the patched terminal widget's (`make test-fork`) first; the UI's package in eight shards at once (`make test-ui-race`) |
| `make test-unit` | every test without a cluster, without the race detector; the UI's package in eight shards at once (`make test-ui`) |
| `make test-ui`, `make test-ui-race` | `internal/ui/fyneui`'s tests, compiled once and run as eight shards (each a `go test` run of its own, so Go's test cache keeps its result), `UI_JOBS` at a time (unless set, half the CPUs and no more than three quarters of the memory holds: about 3 GB a shard, 8 GB under the race detector), without and with the race detector: 68 s and about 6 minutes on an M1 Ultra instead of 344 s and about 18 minutes in one process ([releasing › the UI's tests in parallel shards](releasing.md#the-uis-tests-in-parallel-shards), D-112) |
| `make test-kind` | cluster tests against the kind cluster (build tag `kind`) |
| `make test-norace-coverage`, `make test-race-ui RACE_SHARD=n RACE_SHARDS=m`, `make test-race-rest`, `make test-darwin` | CI's test jobs: what the race detector keeps from running, without it, with coverage profiles; one shard of the UI's race tests; every other package's race tests; the macOS-only tests ([releasing › CI](releasing.md#ci)) |
| `make coverage-report` | the profiles in `.cache/coverage` merged into `combined.out`, the badge `.github/badges/coverage.json` and `summary.md` (CI keeps the badge current on main) |
| `make vuln`, `make gosec`, `make cyclo`, `make cyclo-report` | govulncheck over the code paths Cogpit calls; gosec over the shipped code; no function above `CYCLO_THRESHOLD`; the 20 most complex functions |
| `make parity` | the parity checklists and the numbers the progress pages report ([rules](../parity/README.md#make-parity)) |
| `make kind-drain-up`, `make test-kind-drain`, `make kind-drain-down` | a separate three-node kind cluster (`cogpit-drain`, own kubeconfig `.cache/kind-drain/kubeconfig`) for the cordon, uncordon and drain tests |
| `make bench` | the gate benchmarks (S1, S2) |
| `make lint`, `make fmt` | golangci-lint v2.15.0 (installed into `bin/`) with the team's linters and the `depguard` layering |
| `make kind-up`, `make kind-seed`, `make kind-down` | the local kind cluster and its seed |
| `make kind-prometheus`, `make kind-prometheus-down` | a real Prometheus in the kind cluster (`monitoring/prometheus-server`, the `prometheus` chart) for the drawer's charts ([hack/kind](../../hack/kind/README.md#prometheus)) |
| `make kind-kps`, `make kind-kps-down` | a minimal kube-prometheus-stack in the kind cluster (namespace `cogpit-kps`) |
| `make kind-alertmanager`, `make kind-alertmanager-down` | one Alertmanager in the kind cluster (namespace `cogpit-alerts`, its Service `cogpit-alertmanager`, port `http`) for `TestKindAlertmanager`, which posts its own alerts and resolves them ([hack/kind](../../hack/kind/README.md#alertmanager)) |
| `make kind-kyverno`, `make kind-kyverno-down` | Kyverno (admission and reports controllers, chart 3.9.1) in the kind cluster's namespace `cogpit-kyverno`, its webhooks limited to namespaces labelled `cogpit-kyverno=true`, test policies (Audit and Enforce) and violating objects in `cogpit-kyverno-a`; the down target removes all of it, Kyverno's CRDs and webhook configurations included ([hack/kind](../../hack/kind/README.md#kyverno)) |
| `make kind-vm`, `make kind-vm-down` | VictoriaMetrics single-node and a small cluster in the kind cluster (namespace `cogpit-vm`), fed from the kind Prometheus ([hack/kind](../../hack/kind/README.md#victoriametrics)) |
| `make kind-trivy`, `make kind-trivy-down` | Trivy Operator in the kind cluster (namespace `cogpit-trivy`), scanning only the test workloads in `cogpit-trivy-a` and `cogpit-trivy-b`, for `TestKindTrivy` ([hack/kind](../../hack/kind/README.md#trivy-operator)) |
| `make sigstore-root` | refresh the sigstore trusted root `internal/kubectl` embeds ([D-50](decisions.md#d-50-kubectls-signature-check)) |
| `make k8s-eol` | refresh the Kubernetes release table `internal/model/k8seol` embeds from upstream's `schedule.yaml` and `eol.yaml`, checked; `K8SEOL_FLAGS=-fallback` keeps the committed table on a failure, as the release pipeline does ([D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)) |
| `make icon` | redraw `packaging/icon.png` from the current logo ([extending › Changing the logo](extending.md#changing-the-logo)) |
| `make app` | `bin/Cogpit.app` via `fyne package`, with Sparkle in `Contents/Frameworks` (`SPARKLE_PUBLIC_ED_KEY=<base64>` for an app that updates itself) and `cogpit-cli` in `Contents/MacOS` |
| `make sparkle` | the pinned Sparkle release in `.cache/sparkle/<version>`, its SHA-256 checked |
| `make showcase` | the README's pictures in `docs/images`, each in the dark and the light theme, then the pinned oxipng over them, lossless (`OXIPNG_FLAGS` without `--zopfli` for a quick look) |
| `make oxipng` | the pinned oxipng release in `.cache/oxipng/<version>`, its SHA-256 checked (macOS on Apple silicon and Linux on x86-64) |
| `make dist-app`, `make dist-macos`, `make dist-macos-sign`, `make dist-macos-notarize`, `make dist-checksums` | the release pipeline's macOS download and `SHA256SUMS` in `dist/`; `MACOS_SIGN=adhoc` for a local check ([releasing › Running the signing locally](releasing.md#running-the-signing-locally)) |
| `make appcast-draft`, `make appcast-sign`, `make appcast-verify` | the release pipeline's appcast jobs ([releasing › the appcast](releasing.md#the-appcast)) |
| `make dist-linux VERSION=x.y.z` | the Debian package `dist/linux/cogpit_<version>_<arch>.deb` for this machine's architecture (cgo does not cross-compile), its binaries built as a release builds them; Debian or Ubuntu with `dpkg-dev` (since 2026-10-20 night, [D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) |
| `make test-linux-package` | lintian on the package `dist-linux` built and the APT repository's scripts end to end with a throwaway key (`hack/linux/apt-test.sh`); CI's job Linux Package |
| `make dist-windows VERSION=x.y.z` | the Windows setup `dist/windows/Cogpit-<version>-windows-x64-setup.exe`, its executables built as a release builds them, with their icon, manifest and version information (go-winres) and WinSparkle (`make winsparkle`, pinned by SHA-256); on Windows only, with MinGW-w64 gcc and NSIS 3 (since 2026-10-21, [D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle); [releasing › Windows](releasing.md#windows)) |
| `make test-windows-package` | installs the setup `dist-windows` built silently for this user into a folder of its own, checks its files, both executables' version, the uninstall and Start menu entries and that `cogpit.exe` is a GUI program, installs it again over itself and uninstalls it (`hack/windows/setup-test.sh`); CI's job Windows Package |
| `make test-windows-unit`, `make test-windows-ui` | CI's Windows test jobs: `go vet` and every package's tests but `internal/ui/fyneui`'s; `internal/ui/fyneui`'s tests in four shards, two at a time (D-112); on Windows, without the race detector ([testing › On Windows](testing.md#on-windows)) |
| `make appcast-windows-draft`, `make appcast-windows-sign`, `make appcast-windows-verify` | the release pipeline's Windows appcast jobs: `appcast-windows.xml` drafted, signed with the update key (on macOS, Sparkle's `sign_update`) and checked ([releasing › Windows](releasing.md#windows)) |
| `make apt-repo`, `make apt-verify` | the release pipeline's APT repository in `dist/apt`: built from the packages in `dist/linux` and the repository Pages serves, signed with `APT_SIGNING_KEY` when it is set; checked with the packages' public key ([releasing › the APT repository](releasing.md#the-apt-repository)) |
| `make clean` | removes `bin/` and `dist/`; `make help` lists every target |

## Development

Through the `Makefile`, as in the team's other Go repositories:

```sh
make build
make lint
make test-unit-race
make test-kind      # needs make kind-up
```

Locally, check what a change touches and leave the full gate to CI
([testing › Before a pull request](testing.md#before-a-pull-request)). CI runs each check as a job of its own with its own make target (`make gosec`, `make cyclo`,
`make test-race-ui RACE_SHARD=n`, …), and semantic-release publishes every `feat:` and `fix:` on
`main` once CI is green ([releasing](releasing.md)). Everything else:
[docs/developer/](README.md).
