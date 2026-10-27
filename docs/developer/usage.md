# Using Cogpit

What a user sees and does. Contributors start at the [developer documentation](README.md); every
command and MCP tool, with its danger and parameters: [`commands.md`](commands.md), generated from
the registry.

## What Cogpit is and its status

Cogpit is a native Kubernetes IDE for the desktop, written in Go: an open-source, clean-room
reimplementation of [Lens Desktop](https://lenshq.io/) for macOS on Apple silicon, Debian and
Ubuntu (amd64, arm64) and Windows (x64). It reads your kubeconfig files, connects with your own
credentials, shows every view from a cache that watches keep current, and lets agents drive all of
it over MCP, the UI included. It is a desktop client only, never deployed into a cluster.
**Status: alpha**; where it stands: [ROADMAP.md](../../ROADMAP.md); how it is built:
[architecture](architecture.md).

## Features in detail

How far each feature matches Lens: [docs/parity](../parity/README.md). Shortcuts are macOS's; ⌘ is
Ctrl on Windows and Linux.

**Clusters and connections**

- Every context of your kubeconfig files and synced folders (`~/.kube` and what you add) in the
  navigator's All Clusters: `+` adds kubeconfigs, `−` opens Kubeconfig Sources…, `›` enters a
  cluster. Remove from Cogpit unlinks a file you added and never touches it.
- A cluster's short name (`kubernetes-admin@prod-eu` → `prod-eu`, [rule](#naming-conventions));
  tooltips and the navigator's search know the full name.
- The navigator's root: the user the API server confirms at every connect (a SelfSubjectReview),
  the groups in a tooltip; `(kubeconfig)` where the server does not say; the last confirmed user,
  dimmed, while not connected. Below it the Kubernetes version against upstream support
  ([details](#the-kubernetes-version-and-upstream-support)); a gear before the root's dot opens the
  cluster's Settings ([D-92](decisions.md#d-92-a-gear-in-the-navigators-cluster-row)).
- A client certificate's validity under the cluster's name: orange in its last 14 days or the last
  fifth of its lifetime, "expired" in red; it never blocks a connect.
- A hotbar of pinned clusters, scrolling when full: F1–F12 open the first twelve, drag a tile to
  reorder. Removing a kubeconfig source, or Remove from Cogpit, removes its tiles; a tile whose file
  went away by itself stays, marked missing.
- A welcome page whenever no cluster is open (and Help › Welcome): the hotbar's and recent clusters,
  Add Kubeconfigs, the documentation, and a banner naming the level of what it says.
- Several clusters connected at once, one shown: each stays connected until you disconnect it or
  quit, keeps its own namespace filter, search and status filter, and agents may work on any of
  them ([details](#several-clusters-connected)).
- A failed connect shows an error page with each stage's result, Connect Again and Show Connection
  Updates ([details](#several-clusters-connected)).
- A lost connection (sleep, a network or VPN change, a cluster that stops answering) keeps views
  and tabs, disables changes and reconnects by itself ([details](#when-the-connection-breaks)).
- Integrations per cluster: the Metrics Server, Prometheus, Alertmanager, the Trivy Operator,
  Kyverno; a new cluster asks at its first connect
  ([details](#cluster-settings--integrations-and-the-question-at-a-new-clusters-first-connect)).

**The window**

- Command palette ⌘⇧P: every menu action, view and hotbar cluster. ⌘P finds a cluster by name.
  ⌘B, ⌘J and ⌘I toggle the navigator, the dock (a terminal when it has no tab) and the agent chat.
- The tab bar shows the shown cluster's tabs, none until it connects; Mixed environments (foot of
  All Clusters, off by default) shows every connected cluster's. Reorder tabs by dragging or with
  ⌘⌥⇧← and ⌘⌥⇧→ (the dock's tab while you work in the dock); the order is kept with the tabs.
- Back and forward restore each place of the tab's history with its filters, search, drawer and
  scroll position ([D-129](decisions.md#d-129-back-brings-the-place-back)): ⌃- and ⌃⇧- or the title
  bar's arrows ([keys per platform](#zoom)).
- Zoom with ⌘+, ⌘- and ⌘0 ([details](#zoom)); the window reopens where it was
  ([details](#the-windows-place-and-size)).
- Preferences (⌘,) and a cluster's Settings (⌘⇧,) open as a card over the window, closed with ×,
  Escape or a click beside it. A cluster's Settings hold its name, icon (up to five letters on a
  colour, or part of an image), terminal folder and namespace, Prometheus (query format, address,
  empty containers filtered, HTTPS) and integrations; nothing is written to a kubeconfig.
- Times in your time zone and your region's format; on Linux and Windows the format comes from
  `LC_ALL`, `LC_TIME` or `LANG`, else ISO 8601.
- Toasts at the bottom right, kept in the bell's list, their text selectable
  ([details](#selecting-and-copying-a-logs-a-notifications-and-the-agents-text)).
- The mouse wheel over a text field scrolls the page; a multi-line field scrolls itself first, to
  its end ([D-126](decisions.md#d-126-a-text-field-passes-the-wheel-on)).

**Resources**

- Lens's resource views (workloads, config, network, storage, access control, device resources,
  custom resources), each with a word search (match case, regular expression), filters by the rows'
  states and a namespace filter with its own search; a filtered namespace being deleted shows red.
  A click below the rows closes the drawer.
- Ingresses list their hosts in place of Lens's Rules; a click on a host, there or in the drawer's
  rules, opens it in the browser, https where the Ingress has TLS for it. `*` and wildcard hosts
  are text.
- Certificates in Secrets decoded in the drawer (PEM or DER, each of a chain): subject, issuer,
  serial, validity (a warning 30 days before the end), names, key, usages, fingerprint; whether
  `tls.key` belongs to `tls.crt` and the chain is in order. Agents read the fields, never the PEM
  text or the private key.
- YAML editing with a diff and a server-side dry run before every apply; every manifest reads
  apiVersion, kind and metadata first, status last; ⌘Z undoes, ⇧⌘Z or ⌘Y redoes; ⌘F opens a find bar
  (match case, whole word, regular expression) in the editor tabs, the drawer's YAML
  ([D-127](decisions.md#d-127-find-in-the-drawers-yaml)) and a release's values
  ([D-130](decisions.md#d-130-find-in-a-releases-values)). An apply that scales a workload to zero
  or removes a Config Map's or Secret's key asks first.
- A Config Map's values edited in its drawer: keys added, changed, removed, saved with a diff and a
  dry run, rebased onto a version someone else changed meanwhile; unsaved values survive a quit or
  a crash as a draft.
- A Secret's values edited in its drawer, by you alone: Show, copy a value (masked too) or its
  base64 form (the clipboard is cleared after 60 seconds), add and remove keys; Save checks with the
  server first and names keys and sizes, never a value; nothing unsaved touches the disk. Create
  Secret masks values as you type. Agents can neither create a Secret nor read, review or save its
  values.
- Delete, Restart and the Flux and Argo CD actions on checked rows (each view keeps its own): up to
  500 after one confirmation naming them all; more than ten run in a dock tab with Cancel, which
  agents see and may cancel in `ui_state`.
- Scale a Deployment, StatefulSet or ReplicaSet with a slider from 1 to 30 or a typed count; 0 is
  confirmed first.
- Drain a node in a dock tab with Cancel, a pod a PodDisruptionBudget holds retried until the
  timeout. A drain cancelled, failed or cut off leaves the node cordoned, with Finish drain,
  Uncordon and Dismiss; nothing is uncordoned by itself. An agent's drain goes on after its call
  ends.
- A namespace stuck in Terminating names what holds it
  ([details](#a-namespace-stuck-in-terminating)); Kyverno's policy violations mark their objects
  ([details](#kyvernos-policy-violations)).
- Image grades from the Trivy Operator the cluster runs: S nothing found and a supported OS; A only
  Low or Unknown, at most 10; B Medium, or more than 10 Low or Unknown; C High, or an OS at its end
  of life; D 1 to 5 Critical; E more; N/A no report. The Pods list shows each pod's worst grade, a
  pod's drawer each container's with its counts; nothing is read while the integration is off.

**Workloads at run time**

- Logs: follow, filter, timestamps, the previous container, pod and container selectors, older
  lines, a jump to a time.
- Pod shells, attach and a terminal bound to the cluster's context (⌘⇧T); copy and paste by keys or,
  if you choose, the right mouse button. A tab you open gets the keyboard; one an agent opens does
  not take it.
- The terminal's `kubectl` checked against the cluster, a verified matching release one click away
  ([details](#preferences--kubernetes-kubectl-for-terminals)).
- A node shell: a privileged pod in `kube-system` from an Alpine image pinned by digest, confirmed
  every time, deleted when the shell closes.
- Debug containers added from a pod's drawer ([details](#debug-containers)); port forwarding with
  one click to the browser.

**Helm and GitOps**

- The Helm releases in your clusters (Secrets or ConfigMaps driver) through the Helm SDK, no `helm`
  binary: history, values, upgrade, rollback, uninstall; a release's drawer ends with its History,
  a failed revision's error in red ([revisions](#a-releases-revisions)).
- Charts from the repositories in Helm's own `repositories.yaml` (read, never written; a change
  picked up within seconds), installed from a Helm Install tab; a chart's drawer shows the README of
  the version you pick ([chart icons](#preferences--kubernetes-chart-icons)).
- Flux CD: a navigator section (Helm releases, Kustomizations, sources, image automation,
  notifications) with each object's state, revision and message; a dashboard of what is not ready;
  Reconcile, Reconcile with Source (waiting for the source in a dock tab), Reset, Suspend and
  Resume, on a selection too.
- Argo CD: a navigator section (Applications, ApplicationSets, Projects, and repositories and
  clusters without their credentials) and a dashboard of health and sync state; Sync (with prune)
  and Refresh (normal or hard) within the project's sync windows, on a selection too; an
  Application's logs in one tab; Argo CD's own UI through a port forward.

**Metrics and alerts**

- CPU and memory from the Metrics Server and Prometheus in the Pods and Nodes lists, the drawers'
  charts and the cluster Overview; a node's history, network and disks from node-exporter.
- Cogpit installs nothing: it finds the cluster's Prometheus by its Service's name
  (kube-prometheus-stack, the `prometheus` chart, the Prometheus Operator, Thanos's Querier,
  VictoriaMetrics single-node or vmselect), a Prometheus first. Name another in Cluster Settings ›
  Prometheus › Service address (vmselect with the prefix `/select/<tenant>/prometheus`).
- The Alertmanager's alerts: a pill in the title bar and an alert board
  ([details](#alerts-the-pill-and-the-alert-board)).

**Agents, safety, updates**

- MCP control of everything, the UI included ([Drive it with an agent](#drive-it-with-an-agent));
  a built-in agent chat (⌘I) on an OpenAI-compatible endpoint such as LM Studio, LiteLLM or Open
  WebUI ([its connections](#preferences--agent-the-chats-connections)); [`cogpit-cli`](#cogpit-cli).
- Destructive actions are confirmed in a table (Context › Namespace › Object, the effects, who
  asked); nothing is installed into a cluster or written implicitly.
- kubectl's own authentication code (certificates, tokens, exec credential plugins); no local
  proxy, no login to Cogpit. Views read from the cache; switching back to a view costs no API
  request.
- Updates with one click: Sparkle on macOS, apt on Linux, WinSparkle on Windows
  ([updates](#preferences--app-updates)). macOS only: [`cogpit://` links](#deep-links-cogpit),
  [Start at login](#preferences--app-start-at-login).

## Get it and run it

### Download a release

Every [release](https://github.com/guided-traffic/cogpit/releases) holds the complete product
([D-16](decisions.md#d-16-releases-and-what-they-carry)):

| File | For | Contents |
|---|---|---|
| `Cogpit-<version>-macos-arm64.dmg` | macOS 13 or later, Apple silicon | `Cogpit.app` with `cogpit-cli` inside, signed with the Developer ID and notarized ([signing](releasing.md#signing-and-notarization)) |
| `cogpit_<version>_amd64.deb`, `cogpit_<version>_arm64.deb` | Debian 13, Ubuntu 24.04 and later | `cogpit`, `cogpit-cli` ([Install on Linux](#install-on-linux)) |
| `Cogpit-<version>-windows-x64-setup.exe` | Windows 10 1809 or later, Windows 11; x64 | `cogpit.exe`, `cogpit-cli.exe` ([Install on Windows](#install-on-windows)) |
| `SHA256SUMS` | – | the SHA-256 of each file; not signed |

On macOS, open the disk image and drag Cogpit into Applications.

```sh
shasum -a 256 -c --ignore-missing SHA256SUMS
spctl -a -t open --context context:primary-signature -vv Cogpit-<version>-macos-arm64.dmg   # source=Notarized Developer ID
/Applications/Cogpit.app/Contents/MacOS/cogpit-cli version
```

The top bar shows the running version right of the wordmark: `v<version>` for a release, `dev` for
a build from source, its commit in the tooltip (`dev 0123456789ab-dirty` # example); About Cogpit
says the same ([which build reports what](releasing.md#the-version-a-build-reports)). Fyne's thread
check is off in releases, on in builds from source, where each call to Fyne off its main goroutine
shows a toast; the app log names the mode at every start
([D-103](decisions.md#d-103-fynes-thread-check-a-build-parameter-off-in-releases)).

**Not built:** an app for Intel Macs, a Homebrew cask, Linux packages other than the `.deb`, a
Windows setup signed with Authenticode or for ARM64, winget and Scoop.

### Install on Linux

For Debian 13, Ubuntu 24.04 and later, amd64 or arm64, in an X11 or a Wayland session:

```sh
sha256sum -c --ignore-missing SHA256SUMS
sudo apt install ./cogpit_<version>_amd64.deb   # the ./ makes apt read the file; apt fetches the libraries
cogpit-cli version
```

Cogpit is then in the desktop's applications; `cogpit` starts it from a terminal. The package sets
up Cogpit's APT repository `https://guided-traffic.github.io/cogpit/apt/` (suite `stable`, component
`main`, the two newest releases per architecture), so `apt upgrade`, the Software Updater and
Cogpit's Update button offer every release ([updates on Linux](#updates-on-linux)). apt checks it
against its Ed25519 key, inline in the sources file: fingerprint
`BE09 5407 4B36 CBDE 8875  7E52 A9D2 608B 0DC7 6494`, valid until 2031-10-19, public half in
[`packaging/linux/cogpit-archive-keyring.asc`](../../packaging/linux/cogpit-archive-keyring.asc)
([security › updates › Linux](../security/updates.md#linux), [releasing › Linux](releasing.md#linux)).
Nothing upgrades by itself: unattended-upgrades installs Cogpit only when its
`Unattended-Upgrade::Origins-Pattern` names `"origin=Cogpit,suite=stable";` # example.

| File the package installs | What it is |
|---|---|
| `/usr/bin/cogpit`, `/usr/bin/cogpit-cli` | the app and the command-line tool, built as a release ([how](releasing.md#linux)) |
| `/usr/share/applications/io.guided-traffic.cogpit.desktop` | the launcher (`StartupWMClass=Cogpit`; Wayland app id `io.guided-traffic.cogpit`) |
| `/usr/share/icons/hicolor/<n>x<n>/apps/io.guided-traffic.cogpit.png` | the icon, 16 to 512 pixels |
| `/usr/share/metainfo/io.guided-traffic.cogpit.metainfo.xml` | the AppStream entry |
| `/usr/libexec/cogpit/update` | the Update button's helper, run as root through `pkexec`: refreshes Cogpit's repository only, upgrades the package `cogpit` only |
| `/usr/share/polkit-1/actions/io.guided-traffic.cogpit.update.policy` | its polkit action `io.guided-traffic.cogpit.update`: an administrator's password, kept a few minutes |
| `/etc/apt/sources.list.d/cogpit.sources` | the repository with its key; a conffile: deleted, it stays deleted through upgrades |
| `/usr/share/doc/cogpit/` | copyright and changelog |

The package depends on the libraries the app links and loads, libdecor with its GTK plugin
included (the title bar's buttons on a Wayland desktop without XWayland), recommends `pkexec`
(without it only apt and the Software Updater update) and has no maintainer scripts. Removing it
keeps your files ([naming conventions](#naming-conventions)):

```sh
sudo apt remove cogpit                    # the app; the sources file stays
sudo apt purge cogpit                     # also the sources file
rm -rf ~/.config/Cogpit ~/.cache/Cogpit   # your settings, logs and caches
```

**On Linux and Windows** the system's title bar stays and Cogpit's bar below it holds the menus
(Cluster, View, Helm, Window, Help) before the logo
([D-109](decisions.md#d-109-the-menus-in-the-top-bar-on-windows-and-linux)); their keys work from
any focus, Alt does not open them. Not on either: `cogpit://` links, Start at login, the window's
position ([size and full screen come back](#the-windows-place-and-size)), the system's file panels
([Fyne's dialogs instead](#choosing-files-the-systems-panels)).

### Install on Windows

For Windows 10 1809 (build 17763) or later and Windows 11, x64. Compare the setup's digest with
`SHA256SUMS` (lower case there, upper case in PowerShell):

```powershell
(Get-FileHash .\Cogpit-<version>-windows-x64-setup.exe -Algorithm SHA256).Hash
Select-String windows-x64-setup .\SHA256SUMS
```

The setup is not signed with Authenticode
([H-139](../security/updates.md#h-139-the-windows-setup-is-not-signed-with-authenticode)): at its
first start SmartScreen says "Windows protected your PC"; More info, then Run anyway. Updates rest
on Cogpit's update key instead: WinSparkle runs only a setup that key signed
([updates on Windows](#updates-on-windows)).

```text
Cogpit-<version>-windows-x64-setup.exe                         for you, %LOCALAPPDATA%\Programs\Cogpit, no administrator # default
Cogpit-<version>-windows-x64-setup.exe /allusers               for every user, C:\Program Files\Cogpit (UAC asks)
Cogpit-<version>-windows-x64-setup.exe /S /D=D:\Tools\Cogpit   silently, into a folder of your own (/D= last) # example
```

Without `/allusers` or `/currentuser` an existing installation is updated where and as it was
installed. A running Cogpit is waited for (a silent setup up to a minute, then it stops; otherwise
it asks); a running `cogpit-cli.exe` is moved aside, and a later setup or the uninstaller removes
it.

| File the setup installs | What it is |
|---|---|
| `cogpit.exe` | the app, a GUI program, built as a release ([how](releasing.md#windows)) |
| `cogpit-cli.exe` | the command-line tool and MCP stdio bridge; not on your `PATH` |
| `WinSparkle.dll` | the updater, loaded by full path; the release the Makefile pins by its archive's SHA-256, unsigned ([H-140](../security/updates.md#h-140-winsparkledll-is-a-third-party-binary-without-a-signature)) |
| `LICENSE.txt`, `THIRD-PARTY-NOTICES.txt` | the licences |
| `uninstall.exe` | the uninstaller (`/S` silently) |

The setup adds the Start menu entry Cogpit and the uninstall entry `io.guided-traffic.cogpit`.
Uninstalling (Settings › Apps) removes the program, its entries and WinSparkle's state
(`HKCU\Software\guided-traffic\Cogpit\WinSparkle`) and keeps your files
([naming conventions](#naming-conventions)):

```powershell
Remove-Item -Recurse "$env:APPDATA\Cogpit", "$env:LOCALAPPDATA\Cogpit"   # your settings, logs and caches
```

The window follows each monitor's scaling. The menus and what Windows lacks are as on Linux
([above](#install-on-linux)), and `~\` in a path field is not your home folder (only `~/` is).

**The terminal on Windows.** A local terminal runs the shell Preferences › Terminal names, else
PowerShell 7 (`pwsh.exe` on `PATH`), else Windows PowerShell, else `%ComSpec%`; `$SHELL` is not
read. It runs on a pseudo console (ConPTY) with
`KUBECONFIG=<its own file>;<your kubeconfig files>`
([why](../security/credentials.md#the-terminals-kubeconfig)). A `kubectl` Cogpit puts first
([details](#preferences--kubernetes-kubectl-for-terminals)) has its folder put first on `PATH` when
it is named `kubectl.exe`, else gets a hard link or copy named so. Enforce kubectl has no effect
(PowerShell and cmd get no function). Closing the terminal ends every program started from it, the
shell running in a job object of its own.

### Build and run from source

The Makefile sets `GOTOOLCHAIN=auto`, so an older Go fetches the version `go.mod` names.

| System | Needs |
|---|---|
| macOS | Go, the Xcode Command Line Tools (Fyne uses cgo) |
| Debian, Ubuntu | Go, and as CI: `sudo apt install --no-install-recommends gcc libc6-dev make pkgconf libgl-dev libegl-dev libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev libxkbcommon-dev libwayland-dev`; `dpkg-dev` for `make dist-linux` |
| Windows | Go, Git Bash or MSYS2's shell with MSYS2's MinGW-w64 gcc and make, as CI; NSIS 3 for `make dist-windows` ([releasing › Windows](releasing.md#windows)) |
| The kind cluster | Docker, `kind`, `kubectl` |

```sh
git clone https://github.com/guided-traffic/cogpit.git
cd cogpit
make run-demo                     # the synthetic demo cluster: 10,000 pods, 1,000 watch events per second
make kind-up                      # a kind cluster "cogpit", kubeconfig .cache/kind/kubeconfig
make run                          # Cogpit connected to it; make kind-down removes the cluster
make build && bin/cogpit          # your own clusters: ~/.kube and the files $KUBECONFIG names
make app                          # macOS: bin/Cogpit.app (ad-hoc signed)
make dist-linux VERSION=x.y.z     # Linux: the .deb for this machine's architecture in dist/linux
make dist-windows VERSION=x.y.z   # Windows: the setup in dist/windows
```

A build from source offers the demo cluster as the context `demo`; a release leaves it and its
flags out ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)). Nothing writes
`~/.kube/config`. `make kind-up` seeds the namespace `cogpit-demo`
([hack/kind](../../hack/kind/README.md)), the Service `web` with ClusterIP `10.96.100.100` among it.
Every target: [Make targets](#make-targets).

<details>
<summary>Clean up</summary>

```sh
make kind-down && make clean                                            # the kind cluster; bin/ and dist/
rm -rf ~/Library/Caches/Cogpit ~/"Library/Application Support/Cogpit"   # macOS: settings, logs, caches
rm -rf ~/.cache/Cogpit ~/.config/Cogpit                                 # Linux
```

```powershell
Remove-Item -Recurse "$env:LOCALAPPDATA\Cogpit", "$env:APPDATA\Cogpit"   # Windows
```

</details>

### Drive it with an agent

Cogpit's external MCP server, for other programs on this computer, is off until you click the MCP
item at the right end of the status bar; the choice is kept
([D-49](decisions.md#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp)). Its gear
opens the MCP details: the switch, the endpoint, the token (masked until Show), the registration
commands and Regenerate token
([D-53](decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it)). Help ›
Copy MCP command copies the command; Cogpit also prints it to the terminal that started it.

Where Cogpit came with its `cogpit-cli` (the app in Applications, the Linux package, a Windows
release), the details offer the stdio registration, run once
([D-2](decisions.md#d-2-agent-interface-scope)); on Windows in PowerShell's form:

```sh
claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user cogpit -- /Applications/Cogpit.app/Contents/MacOS/cogpit-cli mcp   # example
```

```powershell
claude mcp remove --scope user cogpit *> $null; claude mcp add --scope user cogpit -- 'C:\Program Files\Cogpit\cogpit-cli.exe' mcp   # example
```

Claude Code then starts `cogpit-cli mcp`, which reads the running app's port and token before every
call, so the registration survives restarts, updates and a new token; run it again only after
moving the app. A build from source and the app running from its disk image offer the HTTP
registration:

```sh
claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user --transport http cogpit http://127.0.0.1:<port>/mcp --header "Authorization: Bearer <token>"
```

The port and the token are kept across restarts: run it again after Regenerate token, or when
Cogpit says the kept port was taken. The token stays valid until you regenerate it
([H-98](../security/agents-and-mcp.md#h-98-a-leaked-token-stays-valid-until-the-user-regenerates-it));
a source build's bridge needs none (`claude mcp add --scope user cogpit -- "$PWD/bin/cogpit-cli"
mcp` # example). With `make kind-up`, ask "show me the Service with ClusterIP 10.96.100.100":
Cogpit opens Services, selects `web` and opens its drawer. Agents read and navigate freely;
Restart, Delete and the pod shell wait for your confirmation
([agents and MCP](../security/agents-and-mcp.md)).

## Command line

### `cogpit` flags

| Flag | Default | Meaning |
|---|---|---|
| `--kubeconfig <files>` | the files `$KUBECONFIG` lists # default | kubeconfig files, separated by `:` (`;` on Windows), each a source of its own for this run; `~/.kube` is synced either way |
| `--context <name>` | empty: the welcome page # default | connect at start: an id as the navigator lists it, or a context name only one file defines; one Cogpit refuses is named in a toast and the navigator's search opens |
| `--demo` | `false` # default | connect to the synthetic demo cluster; development builds only ([D-67](decisions.md#d-67-the-demo-cluster-stays-out-of-releases)) |
| `--demo-pods <n>` | `10000` # default | pods in the demo cluster; development builds only |
| `--demo-events <n>` | `1000` # default | watch events per second in the demo cluster; development builds only |
| `--mcp-addr <addr>` | `127.0.0.1:0` # default | MCP listen address, loopback only; port 0 binds the port kept in `mcp-port` (a free one, kept, when there is none or it is taken), another port is used for this run only |
| `--no-mcp` | `false` # default | leave the external MCP server and its status bar item out of this run; the stored switch stays |
| `--perf <duration>` | `0` # default | measure main-loop latency while auto-scrolling the table, print it as JSON and quit |
| `--perf-mode <mode>` | empty # default | `scroll` with `--perf`, or `gate`: start-up, scrolling, every view and typing into 5 MB of YAML ([spike](spike-fyne.md)) |
| `--cpuprofile <file>` | empty # default | write a CPU profile of the first `--cpuprofile-for` to this file |
| `--cpuprofile-for <duration>` | `5s` # default | how long `--cpuprofile` records |
| `--smoke <duration>` | `0`: off # default | a smoke test ([releasing › the smoke test](releasing.md#the-smoke-test)): start in a temporary, empty home of its own, without MCP server, update check, login item or cluster connection (`--kubeconfig` and `--context` have no effect), quit once the window drew its first frame; exit 1 on a logged error, after `<duration>`, or when it does not quit within 15 s of the frame. The home stays; the last line names it |
| `--theme <mode>` | empty: the Preferences' theme, `system` until set # default | `dark`, `light` or `system` for this run ([D-22](decisions.md#d-22-colour-theme)) |
| `-v` | `false` # default | debug logging |

Security note on `--mcp-addr`: the server binds loopback addresses only and checks `Host`,
`Origin` and the bearer token; any reachable listener would let others drive Cogpit with your
cluster identities.

### `cogpit-cli`

The headless command-line tool (pure Go, no window), updated with the app: inside the app on macOS
(`/Applications/Cogpit.app/Contents/MacOS/cogpit-cli`, signed and notarized with it),
`/usr/bin/cogpit-cli` on Linux, `cogpit-cli.exe` beside `cogpit.exe` on Windows (not on `PATH`),
`bin/cogpit-cli` from `make build`. It offers the app's contexts from the same kubeconfig sources,
runs a credential plugin or connects a context with `insecure-skip-tls-verify` only once you allowed
it in Cogpit's window, and never writes Cogpit's sources or approvals. Exit codes: `0` done, `1`
the command failed, `2` the command line is wrong; `cogpit-cli <command> --help` lists a command's
flags.

| Command | Flags | What it does |
|---|---|---|
| `cogpit-cli contexts` | `--kubeconfig <files>`, `-o table` # default (`json`, `yaml`), `-v` | every context as `<context> [<file>]` with NAMESPACE, SERVER, LOGIN (a plugin or token file: `approved`, `required`, `changed`), CERTIFICATE (`not checked` for `insecure-skip-tls-verify`, with your approval's state) and CLIENT CERT (`expires in 23 days` # example; [D-75](decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)); `json`/`yaml`: the `cluster_contexts` result. Reads no key and runs no plugin |
| `cogpit-cli get <kind> [name]` | `--context <context>` (required), `-n`/`--namespace <ns,...>` (every namespace; a named object's: the context's # default), `-o table` # default (`json`, `yaml`), `--filter <text>`, `--timeout 1m` # default, `--kubeconfig <files>`, `-v` | waits for the cache's first list of the kind and prints it as Cogpit's view does; `json`/`yaml`: the `resources_list` result (at most 1,000 rows) or a named object's manifest (managed fields dropped, Secret values redacted). `<kind>`: a kind key (`pods`, `certificates.cert-manager.io`) or a kind or view title naming one (`Pod`). A context without certificate checks gets a warning on stderr |
| `cogpit-cli mcp` | `--headless` (`false` # default), `--kubeconfig <files>` (headless only), `-v` | MCP over stdio: forwarded to the running Cogpit (its confirmations and audit log apply); without it, or with `--headless`, an engine of its own that refuses what needs your confirmation. While the external MCP server is off it only says so. stdout carries the protocol, logs go to stderr |
| `cogpit-cli version` (`--version` too) | – | `cogpit-cli <version> (go1.27.3, darwin/arm64)` # example; a development build `dev` and its commit |

```sh
cogpit-cli get pods -n kube-system --kubeconfig .cache/kind/kubeconfig --context kind-cogpit                       # example
cogpit-cli get deployments web -n cogpit-demo -o yaml --kubeconfig .cache/kind/kubeconfig --context kind-cogpit   # example
cogpit-cli get services --context demo                                                                            # a development build's demo cluster
```

Security note on `mcp`: the bridge sends the token only to the loopback URL in `mcp-endpoint`,
reads both files before every call, follows no redirect and uses no proxy. The switch, the file
`mcp-switch` (`on`, `off`), fails closed: no file, an unreadable one or an unknown configuration
directory is off. A run with `--no-mcp` leaves the switch as it is, so a bridge started meanwhile
serves headless if it says on
([H-28](../security/agents-and-mcp.md#h-28-the-status-bars-switch-does-not-reach-a-headless-bridge)).
Without a window, switch it on by hand (no command does, since an agent could run it):

```sh
mkdir -p -m 700 ~/.config/Cogpit && printf 'on\n' > ~/.config/Cogpit/mcp-switch   # example on Linux; macOS: ~/Library/Application Support/Cogpit, Windows: %APPDATA%\Cogpit
```

Security note on `get` and `contexts`: `get` only reads (origin CLI), prints a Secret's key names
but never a value, and refuses a context whose credential plugin you have not allowed before it
runs. Both escape every text from a kubeconfig or a cluster for your terminal (`\u001b`), since
anyone who can drop a file into a sync folder chooses it. Without a configuration directory (no
`$HOME`) the CLI does not connect: merging the files as kubectl does would run their plugins
unasked.

## Preferences

Preferences (⌘,) are kept in `<user config dir>/Cogpit/preferences.json`, owner-only, and set on
the Preferences page or by an agent with `preferences.set`, except where a field says only you may.

### Preferences › App, Tables, Editor, Terminal, Proxy

| Field | Default | Meaning |
|---|---|---|
| `theme` | `system` # default | `system` (follows the computer), `dark` or `light`; also the title bar's sun and moon; `--theme` overrides it for one run |
| `timezone` | `""`: the computer's # default | the IANA time zone every absolute time is written in (`Europe/Berlin` # example) |
| `tableRowHeight` | `0`: the text's height and 6 points above and below # default | a table row's height, 20 to 48 points |
| `editorFontSize`, `editorLineNumbers`, `editorTabSize` | `13`, `true`, `2` # default | the YAML editor of the Edit, Create, Helm Install and Upgrade tabs: text size (8 to 32 points), line numbers, spaces per Tab (1 to 8) |
| `terminalShell`, `terminalShellArgs` | `""`: `$SHELL`, else `/bin/zsh`, `/bin/bash`, `/bin/sh` ([Windows](#install-on-windows)); `[]` # default | the local terminal's shell (an absolute path) and its arguments; pod shells are not affected. Only you set them |
| `terminalFontSize` | `13` # default | the text size of terminals, pod shells and attached sessions opened afterwards, 8 to 32 points |
| `terminalRightClickCopyPaste` | `false` # default | a right click in a terminal copies the selection, or pastes when nothing is selected. Only you set it |
| `httpProxy` | `""`: the environment's `HTTPS_PROXY` # default | the proxy for traffic that is no cluster's (Helm repositories, `kubectl` downloads, chart icons): `http://`, `https://` or `socks5://`, a password shown redacted. An agent's proxy waits for your acceptance; `cogpit-cli` may only clear it |

### Preferences › App: start at login

macOS only: registers Cogpit as the app's own login item, listed in System Settings › General ›
Login Items too ([D-48](decisions.md#d-48-start-at-login)). It is macOS's state, not a field of
`preferences.json`.

| Status (`preferences_loginItem`, `ui_state`'s `loginItem`) | Meaning |
|---|---|
| `off` # default | not registered |
| `on` | macOS opens Cogpit at login |
| `requiresApproval` | registered; allow it in System Settings › General › Login Items (the row's Open Login Items…) |
| `notFound` | needs the signed `Cogpit.app` in Applications; a `bin/cogpit` or `go run` build reports this without asking macOS |

Security note: on, Cogpit runs after every login unopened, with its MCP server if switched on; so
only you, in Cogpit's window, turn it on, and agents may only turn it off.

### Preferences › App: restore the last session

Off, Cogpit starts on the welcome page without tabs or connections. On, the next start reopens the
last run's tabs and connects the clusters connected at quit: the one shown then first, the others
one by one in the background ([D-69](decisions.md#d-69-several-clusters-connected-at-once)). The
welcome page's checkbox "Restore this session at the next start" is the same preference.

- Login programs run as on any connect; one not allowed yet, or changed since, does not run and the
  cluster says why. `--context` shows its own cluster first.
- A cluster whose kubeconfig is gone is left out with a notice; one whose connect fails shows its
  [error page](#several-clusters-connected) and no tabs.
- The dock's tabs are not restored. Table columns, collapsed navigator groups, whether the navigator
  and the chat show, and the [window's place](#the-windows-place-and-size) come back either way.
- `workspace.json` records the connected and shown clusters either way, so turning the preference
  on restores the session of your last quit.

| Preference (`preferences.set`) | Default | Meaning |
|---|---|---|
| `restoreSession` | `false` # default | on, login programs run without a click, so agents and the CLI may only turn it off |
| `mixedEnvironments` | `false` # default | on: the tab bar shows every connected cluster's tabs; off: only the shown cluster's. The check "Mixed environments" at the foot of the navigator's All Clusters; agents may set it |

### Preferences › App: reduce motion

Off, the navigator slides: a cluster's tree comes in from the right over All Clusters, another
cluster's tree vertically in All Clusters' order
([D-107](decisions.md#d-107-the-navigator-slides-and-reduce-motion)). On, nothing moves: the
navigator switches at once, dragged tiles and tabs jump into place, a reconnecting cluster's dot is
steady at half strength, and the [light strip](#the-light-strip) stands still. macOS's Reduce motion
(System Settings › Accessibility › Display) has the same effect.

| Preference | Default | Meaning |
|---|---|---|
| `reduceMotion` | `false` # default | on: nothing slides or blinks; agents may set it |

### Preferences › App: light strip on battery

| Preference | Default | Meaning |
|---|---|---|
| `lightStripMainsOnly` | `false` # default | "Animate the light strip only on the power adapter": on, the [light strip](#the-light-strip) stands still in its colour while the computer runs on battery; agents may set it |

macOS follows the power source at once; Windows and Linux read it every 30 s (on Linux a
peripheral's battery, such as a wireless mouse's, does not count).

### Zoom

**View › Zoom In**, **Zoom Out** and **Reset Zoom** scale the whole window by 1.2 per step, as VS
Code does and with VS Code's keys, from any focus; the window keeps its size on the screen
([D-134](decisions.md#d-134-the-windows-zoom)).

| | macOS | Windows | Linux |
|---|---|---|---|
| Zoom In | ⌘=, ⌘⇧=, ⌘+ (keypad, German keyboard's +) | Ctrl+=, Ctrl+Shift+=, Ctrl++ | Ctrl+=, Ctrl+Shift+=, Ctrl++ |
| Zoom Out | ⌘-, ⌘⇧- | Ctrl+-, Ctrl+Shift+- | Ctrl+- |
| Reset Zoom (100%) | ⌘0 | Ctrl+0 | Ctrl+0 |
| Go Back / Go Forward | ⌃- / ⌃⇧- | Alt+← / Alt+→ | Ctrl+Alt+- / Ctrl+Shift+- |

The steps are 50, 60, 70, 80, 100, 120, 140, 170 and 210%. Zoom In stops where the content would no
longer fit the window ("the window is too small for 210%: make it larger first"). The status bar
shows −, the percent (a click resets to 100%) and + right of **Diagnostics**; Preferences › App ›
Zoom sets the same value. The zoom multiplies the scale Cogpit was started with (`FYNE_SCALE`, else
Fyne's setting); the local terminal's shell gets `FYNE_SCALE` as Cogpit was started with it.

| Preference | Default | Meaning |
|---|---|---|
| `zoomLevel` | `0` # default (100%) | `-4` to `4` (`1` # example: 120%); `ui.zoom.in`, `ui.zoom.out` and `ui.zoom.reset` step it; agents may set it |

### Preferences › App: revision diffs

Which way a Helm release's Revisions tab compares two revisions: the first is the diff's old side
(−), the second its new side (+) ([D-131](decisions.md#d-131-a-releases-revisions)).

| Preference | Default | Values |
|---|---|---|
| `revisionsDiff` | absent: `comparedToShown` # default | `comparedToShown` (from the compared revision to the shown one), `olderToNewer`, `shownToCompared`; any origin may set it |

### Preferences › App: updates

| Field (`preferences.json`) | Default | Meaning |
|---|---|---|
| `checkForUpdates` | `true` # default | "Check for updates automatically": at every start and every hour, against the platform's update feed ([names](#naming-conventions)), on Linux apt's lists; off: only Help › Check for Updates… checks. Agents and `cogpit-cli` may turn it on, never off |
| `updateChannel` | `"stable"` # default | the only channel; `beta` is refused |

A newer release shows a button at the top bar's right end, "Update to v<version>"
([D-54](decisions.md#d-54-the-top-bar-layout-toggles-the-update-button-the-theme-toggle)). A Cogpit
that cannot update itself says why: a build without the update key (every local build), an app
still on its disk image, on Linux not the package's `/usr/bin/cogpit`, on Windows not the setup's
`cogpit.exe`. `cogpit-cli` has no updater; it comes with the app, the package or the setup.

On macOS Sparkle 2 reads Cogpit's signed appcast ([D-4](decisions.md#d-4-packaging-and-updates)).
The button opens Sparkle's window with the newest release and goes when a check finds nothing newer
or Sparkle's session ends. Sparkle downloads only when you click Install Update, and installs only a
disk image and feed signed with the update key and an app carrying Cogpit's Developer ID
([updates](../security/updates.md)).

#### Updates on Linux

apt updates Cogpit from the repository the package set up and checks its signature
([Install on Linux](#install-on-linux),
[D-108](decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages),
[updates › Linux](../security/updates.md#linux)). Cogpit's check reads `apt-cache policy cogpit`,
without a request or a password.

| apt's lists say | Button | Its click |
|---|---|---|
| a newer release | "Update to v<version>" | `pkexec /usr/libexec/cogpit/update`: after an administrator's password, refreshes only Cogpit's repository and upgrades only the package `cogpit` |
| a newer release is installed under the running Cogpit | "Restart to Update" | restarts `/usr/bin/cogpit` |

Installing never restarts Cogpit by itself. Help › Check for Updates… runs the helper too. On Linux
only your click runs `app.checkForUpdates`; agents read `ui_state`'s `update` (`available`,
`version`, `restart`). The Help item says "not installed from the package", or "no package
repository" when `/etc/apt/sources.list.d/cogpit.sources` is gone.

#### Updates on Windows

WinSparkle updates the `cogpit.exe` the setup installed, in a release build with `WinSparkle.dll`
beside it ([Install on Windows](#install-on-windows),
[D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle),
[updates › Windows](../security/updates.md#windows)). Cogpit's check reads the signed feed; a
newer x64 release not skipped in WinSparkle's window shows the Update button, which installs at
once (`app.installUpdate`): download, signature check, silent setup, restart into the new release.
A per-user installation needs no administrator; an all-users one asks UAC. Help › Check for
Updates… shows WinSparkle's window (Install update, Skip this version, Remind me later). Only your
click runs `app.installUpdate`; `app.checkForUpdates`, open to agents, only shows the window.
Cogpit's own feed read follows `HTTPS_PROXY`, not Windows' proxy settings or Preferences › Proxy:
behind a proxy only Windows knows, the button stays away and Help › Check for Updates… still works.

### Preferences › Kubernetes: program folders

Cogpit started from the Dock, Finder, a launcher or the Start menu lacks the `PATH` and the
variables (`AWS_PROFILE`) your shell's profile sets; credential plugins (kubelogin, krew's
`kubectl oidc-login`) live there
([D-120](decisions.md#d-120-folders-searched-for-credential-plugins)). It looks for a plugin's
program in this order:

1. the folders `PATH` has when Cogpit starts;
2. on macOS and Linux, the built-in folders that exist: `/opt/homebrew/bin`, `/usr/local/bin`,
   `/opt/local/bin`; on Linux `/home/linuxbrew/.linuxbrew/bin`, `/snap/bin`; Nix's
   `/run/current-system/sw/bin`, `/nix/var/nix/profiles/default/bin`,
   `/etc/profiles/per-user/<user>/bin`, `~/.nix-profile/bin`; `~/.local/bin`, `~/bin`;
   `~/.krew/bin` (`$KREW_ROOT/bin`); `~/go/bin` (`$GOBIN`, or `$GOPATH`'s first folder);
   `~/.asdf/shims` (`$ASDF_DATA_DIR/shims`), `~/.local/share/mise/shims` (`$MISE_DATA_DIR/shims`,
   or below `$XDG_DATA_HOME`); `~/.rd/bin` (Rancher Desktop), `~/.docker/bin`;
3. the program folders you add here, on every platform, in the order added.

Add Folder… adds one (on macOS its panel lists hidden folders, so that `~/.krew/bin` can be
chosen); it counts from the next login (Connect Again). A login's program not found is named on the
error page, whose Program Folders… opens this section, and in Connection Updates.

| Field | Default | Meaning |
|---|---|---|
| `programFolders` | `[]` # default | absolute paths, at most 32; also read by `cogpit-cli`. A missing folder is kept and skipped until it exists |

| Command | Who | What it does |
|---|---|---|
| `preferences.addProgramFolder {path}` | you, in Cogpit's window | adds a folder that exists and that not every user may write to |
| `preferences.removeProgramFolder {path}` | anyone, agents included | removes a folder |

Security note: folders are only appended, so nothing your `PATH` finds is shadowed; a program a
login names is found by its name as on `PATH`
([H-6](../security/credentials.md#h-6-exec-plugins-inherit-the-environment)). Only you add folders,
since an agent's folder could hold the program your next login runs; add only folders you alone can
write to (one every user may write to is refused, or skipped once it becomes so).

### Preferences › Kubernetes: `kubectl` for terminals

A terminal compares its `kubectl` with the cluster's version and, more than one minor version off
or with none, offers the matching release ([D-3](decisions.md#d-3-helm-and-kubectl-binaries)).

| Field | Default | Meaning |
|---|---|---|
| `kubectlDownload` | `true` # default | offer the matching release; off, the terminal only says so. A download always waits for your click |
| `kubectlMirror` | `""`: `https://dl.k8s.io/release` # default | an https base URL with `v<version>/bin/<os>/<arch>/kubectl` and its `.sha256`, `.sig`, `.cert` below it (`https://mirror.example.com/kubernetes/kubectl` # example) |
| `kubectlDir` | `""`: `<user config dir>/Cogpit/kubectl` # default | where releases go, as `<dir>/<version>/kubectl` (`kubectl.exe` on Windows); an absolute path |
| `kubectlPath` | `""`: the first `kubectl` on `PATH` # default | the `kubectl` terminals run, an absolute path, checked against the cluster and put first on `PATH` |
| `kubectlEnforce` | `false` # default | off: the chosen `kubectl` goes first on `PATH` after the profile ran (zsh `ZDOTDIR`, bash `--init-file`, fish `-C`); on: zsh, bash and fish also get a function `kubectl`, so a later `PATH` entry or an alias cannot win. Other shells keep the environment's order ([H-36](../security/trust-boundaries.md#h-36-a-shells-profile-can-put-another-kubectl-first)) |

Security note: the last four decide what runs as `kubectl`, so only you, in Cogpit's window, set
them or start a download; agents and `cogpit-cli` are refused. A download is checked against the
release's SHA-256, its sigstore signature and that signature's entry in sigstore's transparency
log, against the trusted root Cogpit embeds, so a mirror needs no trust of its own; one that serves
no `.sig` or `.cert` is refused ([D-50](decisions.md#d-50-kubectls-signature-check)). The entry is
asked from `https://rekor.sigstore.dev` (`POST /api/v1/log/entries/retrieve`, public material only,
30 seconds); without its answer the download is refused, so in a network that cannot reach it put a
`kubectl` on `PATH` or set `kubectlPath`
([H-35](../security/trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)).
Both requests go through Preferences › Proxy's `httpProxy` when set.

### Preferences › Kubernetes: chart icons

| Field | Default | Meaning |
|---|---|---|
| `loadChartIcons` | `true` # default | the Charts view and a chart's drawer fetch the icon a repository's index names, only for what the window shows; off: Cogpit's helm-wheel glyph, nothing fetched ([D-46](decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them)) |

Security note: an icon's host learns that you looked at the chart
([H-87](../security/trust-boundaries.md#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at)).
Icons come over https only, without the repository's credentials, cookies or a referrer, at most
256 KiB, never from your own network unless from a chart repository's host, through `httpProxy`
when set; an SVG only when it refers to nothing outside itself. Agents never fetch icons and may
only turn the switch off.

### Preferences › Kubernetes: Helm storage driver

| Field | Default | Meaning |
|---|---|---|
| `helmDriver` | `""`: Secrets, Helm's default # default | where a new Helm install keeps its release records (Lens's `HELM_DRIVER`): `secret` or `configmap`; `sql` is refused; installed releases keep theirs. Agents and `cogpit-cli` may only set `secret`, since whoever may read ConfigMaps reads `configmap` records |

### Preferences › Agent: the chat's connections

The chat (⌘I) talks to the LLM connections added here ([D-10](decisions.md#d-10-built-in-agent));
each conversation uses one. Only you edit them, in the window; no command does. Kept in
`llm-connections.json`, the keys in `llm-keys.json` ([files](#naming-conventions)).

| Field | Default | Meaning |
|---|---|---|
| Name | – | shown in the chat's connection menu |
| Kind | LM Studio # default | LM Studio, LiteLLM, Open WebUI or another OpenAI-compatible server: sets the paths (`/chat/completions`, `/models`; Open WebUI's under `/api`) |
| Base URL | `http://localhost:1234/v1` # default (LM Studio; LiteLLM `http://localhost:4000`, Open WebUI `http://localhost:3000`, other `https://api.openai.com/v1`) | plain `http://` to another machine than `localhost`, 127.0.0.0/8 or ::1 shows a warning: key, conversation and every Secret read travel unencrypted ([H-95](../security/credentials.md#h-95-an-llm-api-key-over-plain-http)) |
| API key | none # default | sent as `Authorization: Bearer …` to the base URL only; a redirect is refused. Empty keeps the stored key, Remove Key removes it |
| Model | – | typed or from the endpoint's list (↻), `qwen/qwen3.8-27b` # example |
| CA file | none # default | an absolute PEM file for an `https://` server with a private CA (`/Users/you/certs/team-ca.pem` # example); its certificates replace the system's roots for this connection, read at every connect; refused with `http://` ([H-104](../security/trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)) |
| Private connection | off # default | a local model or private cloud: the chat reads what the window shows, Secret values included, and its conversations are not saved ([H-94](../security/agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows), [H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)) |
| Send all tools | off # default | every command the chat may run as its own tool (about 200 tools, some 70,000 tokens per request), for large cloud models; off: 20 core tools plus a search and a run tool (about 8,600 tokens). Same rules either way ([D-2](decisions.md#d-2-agent-interface-scope)) |
| Limits per turn | 3 identical tool calls, 25 tool calls, 300,000 tokens, 10 minutes # default | a turn that reaches one stops with a note |

**Test Connection** checks the form's values, unsaved and without cluster data, layer by layer: DNS
(the addresses; an IP address or `localhost` passes), TCP (5 s for all addresses), HTTP or HTTPS
(the address reached, the TLS version and the verified certificate), Authentication (without, then
with the key), Models (whether yours is listed; fills the list) and Tool use (one harmless tool
call). A failed step shows its request and answer as `curl -v` writes them, the key as
`Bearer •••`; the text is selectable and copyable. Every endpoint error starts with its method and
URL. With `HTTPS_PROXY`, `HTTP_PROXY` or `NO_PROXY` set, DNS and TCP check the proxy. The test and
the model list's ↻ send the stored key also to a typed, unsaved base URL
([H-143](../security/credentials.md#h-143-the-stored-api-key-goes-to-an-edited-unsaved-base-url)).

Under each answer: TTFT (the time to the first streamed part) and OTPS (output tokens per second; ≈
when estimated) ([D-125](decisions.md#d-125-an-answers-speed-ttft-and-otps)). The pencil under your
message edits and resends it: that message and everything after it leave the chat and the kept
conversation (the audit log keeps them); unchanged, it retries the turn. The model is not told what
was cut, so an allowed agent write of the cut part can run again unasked
([H-100](../security/agents-and-mcp.md#h-100-a-cut-parts-tool-calls-leave-the-models-view)).

## Cluster Settings

### Cluster Settings › General: debug, initial streaming

**Debug › Use initial streaming (BETA)** ([D-135](decisions.md#d-135-a-stalled-stream-lists)). Off,
each read lists its objects once and then watches from that list's version; lists have a rate limit
of their own ([rate limits](#connection-diagnostics-what-the-requests-ran-into)). On, from the next
connect, each read gets its objects over the watch it keeps (Kubernetes' WatchListClient, beta),
which may save API server resources. A stream that receives nothing for 10 s (# default) before its
end makes that read and every later read of the connection list instead; Connection Diagnostics
logs it ("streams off, every read lists: for this connection: a stream stalled"), and the next
connect streams again. Switched off, the connected cluster lists at once.

| Setting (`cluster.settings.set`) | Default | Meaning |
|---|---|---|
| `streamInitialLists` (`streaming` in `cluster-settings.json`) | `false` # default | on: stream the initial lists; agents may set it either way |

### Cluster Settings › General: the login while the cluster is not shown

"Renew the login while the cluster is not shown"
([D-69](decisions.md#d-69-several-clusters-connected-at-once)): on, a connected cluster in the
background renews its login like the shown one, so its login program (a browser login included) may
run. Off, the program does not run while the cluster is not shown, and an expiring login
**lapses**:

- The cluster stays connected with a grey ring, not red (`loginLapsed` in `ui_state`'s
  `connections`).
- Its checks pause and changes are disabled ("login lapsed: show the cluster to renew it"). A log
  that has to reopen waits with `[cogpit] the login lapsed; it is renewed when the cluster is shown`;
  port forwards wait; a running drain or Helm action stops as at an interruption; open streams go
  on.
- Showing the cluster runs its login program once (**Login expired** if that fails); **Log in
  again** (`cluster.login`, window only) renews it without showing it. Agents never renew it.

| Setting (`cluster.settings.set`) | Default | Meaning |
|---|---|---|
| `renewLoginInBackground` | `true` # default | off: kept as `holdBackgroundLogin: true` in `cluster-settings.json`. Agents and the CLI may only turn it off |

Two contexts whose login program runs with the same command, arguments and environment share one
login in client-go; it is held only while both are held.

### Cluster Settings › General: the icon

The Icon row chooses the kind ([D-119](decisions.md#d-119-the-cluster-icon-designer));
**Customize…** opens its dialog, **Reset** gives back the initials in the accent colour. The other
kind's settings are kept.

- **Internal icon:** a colour tile with up to five characters, no spaces (empty: the name's
  initials), in one of eight colours or any colour.
- **Custom icon:** a square cut of a PNG, JPEG or SVG image: drag to move it, the wheel or the slider
  to zoom (up to 8×), **Fit** to fill the square from the middle, **Choose Image…** for another file.

Icons have macOS's app-icon corners (radius 22/96 of the side). Sizes: 50 points on the hotbar and
in Cluster Settings' header; up to 50 at the navigator's root, down to 28 where the name or the
version would otherwise be cut; 28 in the navigator's rows and on the welcome page.

| Field (`cluster-settings.json`, per context) | Default | Values |
|---|---|---|
| `iconColor` | none # default: the accent | `cyan`, `blue`, `violet`, `magenta`, `red`, `orange`, `yellow`, `green`, or `#rrggbb` (`"#d33682"` # example) |
| `iconLetters` | none # default: the initials | at most 5 characters without spaces (`"PROD"` # example) |
| `iconTile` | `false` # default | `true`: the tile shows although an image is kept |
| `icon`, `iconSource` | none # default | files in `cluster-icons/`: the 256-pixel icon and its source (at most 1,024 pixels a side; an SVG is drawn into a PNG) |
| `iconCrop` | none # default | `{"zoom": 1.5, "x": 0.4, "y": 0.5}` # example: the zoom (1: the shorter side fills the square) and the source's point at the square's centre, as fractions |

Security note: the image is read once (at most 1 MiB) and checked before anything is kept (PNG or
JPEG decoded under bounds, SVG without scripts or outside references); Cogpit draws only its own
copies.

### Cluster Settings › General: the accent colour

Your mark of which cluster it is (red for production, say), shown by the
[light strip](#the-light-strip) ([D-80](decisions.md#d-80-a-clusters-accent-colour)): None, the
icon's eight colours, or any colour.

| Field (`cluster-settings.json`, per context) | Default | Values |
|---|---|---|
| `accentColor` | none # default | as `iconColor`; set with the row or `cluster.settings.setAccentColor {context?, color}` (empty: none) |

Security note: only you, in the window, change what tells clusters apart (H-125,
[agents and MCP](../security/agents-and-mcp.md)). `cluster.settings.setAccentColor`,
`cluster.settings.setLightStrip`, `cluster.settings.resetIcon`, `cluster.settings.uploadIcon` and
`cluster.settings.cutIcon` are GUI-only; `cluster.settings.set`'s `name`, `iconColor`,
`iconLetters` and `iconTile` are refused to agents and `cogpit-cli`, even where agents' writes are
allowed. Agents read all of them.

The General page also has copy icons by **Cluster name** and by **Kubeconfig** (the file's whole
path, shown wrapped and selectable).

#### The light strip

A strip 4 points high under a view's header, the view's width, in the accent colour of the active
tab's cluster under a frosted-glass grain; frosted glass alone without an accent colour and while
that cluster connects ([D-122](decisions.md#d-122-the-light-strip)). The row **Light strip** under
the accent colour chooses how it moves:

| Light strip | What it does |
|---|---|
| Static # default | the colour, steady |
| Breathing | swells and fades, a breath every 4.6 s, never below 48 % |
| Scanner | a light half the strip wide sweeps end to end, 6 s there and back |
| Flow | a light 46 % of the strip long runs left to right, 5 s a pass |

A change fades the light out and the new one in, 350 ms each; one static colour after another
spreads from the active tab in 700 ms. The strip stands still while Cogpit is not the active app,
while motion is [reduced](#preferences--app-reduce-motion), and on battery with
[Light strip on battery](#preferences--app-light-strip-on-battery) on; a warning under the row says
which. An animation repaints the window up to 60 times a second (about a fifth of a CPU core);
static costs nothing. Without an accent colour the row is disabled ("Set an accent colour first").

| Field (`cluster-settings.json`, per context) | Default | Values |
|---|---|---|
| `lightStrip` | absent # default: static | `breathing`, `scanner`, `flow`; set with the row or `cluster.settings.setLightStrip {context?, animation}` (GUI-only) |

### Cluster Settings › Integrations, and the question at a new cluster's first connect

Integrations are tools the cluster runs that Cogpit finds and reads with your identity, never
installing or writing to them
([D-76](decisions.md#d-76-integrations-per-cluster-and-the-question-at-a-new-clusters-first-connect)):
the Metrics Server (`metrics-server`) and Prometheus (`prometheus`) for metrics, Alertmanager
(`alertmanager`) for the alerts pill and board, the Trivy Operator (`trivy`) for image ratings,
Kyverno (`kyverno`) for policy violations. Nothing is looked for until you choose for the cluster
([D-82](decisions.md#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)), not
even in the background; Alertmanager, Trivy and Kyverno are read only while the cluster is shown.

**Cluster Settings › Integrations**
([D-87](decisions.md#d-87-cluster-settings-the-integrations-under-a-divider-a-section-each-metrics-their-consumer))
lists each integration with its switch and status ("Found automatically:
monitoring/prometheus-server:80" # example, "Needs manual configuration", "Not in this cluster", "No
permission", "Not answering"); each has a section with its details (Prometheus: query format,
service address, HTTPS, path prefix; Alertmanager: service address, HTTPS, path prefix). A switch
applies at once; off, Cogpit sends nothing for it and shows nothing of it. **Ask again** forgets the
cluster's choice and asks again. Prometheus and Alertmanager are read through the API server's
service proxy, the port by number first (`monitoring/prometheus-operated:9090`, as Lens does), then
by name (`…:http-web`), so a role written for Lens that names services in `resourceNames` works;
each refused spelling is a 403 in the cluster's audit log.

**The question:** a cluster without a choice asks once it is connected and shown ("Integrations for
<name>"): **Check all integrations** (the default's, looked for at once; untick what you don't
want), **Choose integrations** (none ticked; a tick looks for that one), or **Don't use
integrations** (after a warning, all off). **Apply** keeps the ticks, with "Save as the default for
new clusters" also as the default; **Later** asks again at the next start and reads nothing
meanwhile. The demo cluster never asks. Preferences › Kubernetes › Integrations of new clusters
holds the default. `cogpit-cli` asks nothing and reads what the default keeps on
([H-120](../security/agents-and-mcp.md#h-120-the-clis-headless-engine-does-not-apply-a-clusters-integration-switches)).

| Setting or command | Default | Meaning |
|---|---|---|
| `cluster.settings.set`'s `integrations`, `{"alertmanager": false}` # example | all off until you choose # default | the cluster's choice; kept as `integrationsOff` in `cluster-settings.json` (absent: no choice yet) |
| `cluster.settings.set`'s `metricsSource` | – | `auto`, `prometheus`, `metrics-server` or `none`: sets both metrics integrations |
| `preferences.set`'s `integrationDefaults`, `{"trivy": false}` # example | all on # default | the default for new clusters and the CLI; kept as `integrationsOff` in `preferences.json` |
| `cluster.integrations.step`, `cluster.integrations.lookFor`, `cluster.integrations.answer`, `cluster.integrations.later`, `cluster.integrations.ask` | – | the question's page, its ticks, Apply, Later and Ask again; `ui_state`'s `integrationsQuestion`, `integrationsStep`, `integrationsLookFor` |

Security note: only you switch integrations and answer the question (H-126, H-133,
[agents and MCP](../security/agents-and-mcp.md)): on reads with your identity, off hides alerts,
ratings or policy marks. Agents and `cogpit-cli` are refused every `cluster.integrations.*` command
but `later` (putting the question off), `cluster.settings.set`'s `integrations` and
`metricsSource`, and `preferences.set`'s `integrationDefaults`, even where agents' writes are
allowed. An agent's `cluster.setPrometheus` or `cluster.setAlertmanager` always waits for your
acceptance.

## The window

### The window's place and size

Cogpit opens where you closed it, whatever Restore the last session says
([D-95](decisions.md#d-95-the-window-comes-back-where-it-was)). On macOS that is the size, place,
display and full screen, saved half a second after each change and at quit; on Windows and Linux
the size and full screen, saved when the window closes (the system places it). On macOS, at the
first start or when its display is gone, the window opens 1440 × 900 # default in the middle of the
main display; on a display arranged elsewhere it keeps its place from the display's top left; one
larger than its display or over its edge is moved in. To start over, quit Cogpit and delete
`window.json` ([files](#naming-conventions)). Agents neither read nor move the window.

On macOS a change of the window's pixel scale (another display, displays off and on around a sleep)
draws every text again once, so none stays soft
([D-121](decisions.md#d-121-texts-drawn-again-when-the-windows-pixel-scale-changes)).

### Quitting

Closing the window, ⌘Q, Quit in the first menu (Windows, Linux), Ctrl+C and SIGTERM quit Cogpit
alike: drafts and tabs are written, the node shell pods it made are deleted, the MCP endpoint file
is removed, and the process exits with code 0. Should Fyne, the window's toolkit, not end its run
within 5 s, Cogpit finishes the same quit itself and logs a warning, `Fyne did not end its run
within the quit bound` with `bound=5s` ([`cogpit.log`](#naming-conventions),
[D-136](decisions.md#d-136-the-quit-bound)).

### A cluster's right-click menu

On a cluster's hotbar tile, its row in All Clusters or on the welcome page, or the navigator's root:
Connect (Show when connected), Settings…, Add to or Remove from Hotbar, **Copy Kubeconfig Path**,
Reconnect, Disconnect or Cancel Connecting, Remove from Cogpit…; a tile adds Move Up and Move Down, a
row whose connect failed Show Connection Updates….

**Copy Kubeconfig Path** copies the path of the cluster's kubeconfig file, connected or not, and a
toast names it. The demo cluster and contexts merged from `$KUBECONFIG` or `~/.kube/config` without
a configuration directory have no such item. Copying is no command
([D-25](decisions.md#d-25-copying-values)); agents read the path in `cluster_contexts` (`file`) and
`cluster_settings_get` (`kubeconfig`).

### Choosing files: the system's panels

On macOS every file chooser is the system's panel, a sheet on Cogpit's window
([D-72](decisions.md#d-72-native-file-panels-and-macos-folder-access)).

| Chooser | Where | Takes |
|---|---|---|
| Sync Files and Folders… | Cluster menu, the navigator's "+", the welcome page, Kubeconfig Sources, Preferences › Kubernetes | kubeconfig files and folders, several at once, hidden files listed; button "Sync"; opens in your home folder; ⌘⇧G types a path |
| Choose Image… | Cluster Settings › General › Icon › Customize… (Custom icon) | one PNG, JPEG or SVG |
| folder buttons | Cluster Settings › Terminal (working directory), Preferences › Kubernetes (directory for binaries) | one folder; button "Choose" |
| Add Folder… | Preferences › Kubernetes › Program folders | one folder, hidden folders listed; button "Add" |
| Choose… | Preferences › Agent, a connection's CA file | one `.pem`, `.crt` or `.cer` |
| save panels | a table's CSV, the Create tab, a log tab's download, Save Report…, the Revisions tab's Save as… | a proposed name; macOS asks before replacing a file; a new file is readable by you only |

**macOS privacy prompts.** Cogpit may read what you pick in these panels without a prompt; anything
else in a protected folder asks once, saying why: "Cogpit reads the kubeconfigs, folders and
certificate files you chose in your Downloads folder. Programs Cogpit starts for you, such as a
terminal or a login plugin, ask through Cogpit too." A terminal or credential plugin reading a
protected folder asks under Cogpit's name and, once Cogpit was allowed the folder, reads it without
asking ([H-116](../security/file-access.md#h-116-programs-cogpit-starts-act-with-cogpits-folder-access)).
The answers are in System Settings › Privacy & Security › Files and Folders
([how macOS grants access](../security/file-access.md#how-macos-grants-access)).

On Linux and Windows the choosers are Fyne's dialogs, in the system's language (the rest of Cogpit
is English); Sync Files and Folders is Cogpit's own browser and takes one file or folder at a time.
On Windows a chosen path shows with backslashes.

### Deep links (`cogpit://`)

macOS only: the app registers the URL scheme `cogpit`, so a link in a runbook, a chat or an alert
opens Cogpit at a cluster, a view or an object ([D-47](decisions.md#d-47-deep-links-cogpit)).

```text
cogpit://open?context=kind-dev%20%5Bconfig%5D&view=pods&namespace=team-a&name=web-0   # example
```

| Parameter | Value | Without it |
|---|---|---|
| `context` | a context id as `cluster_contexts` lists it (`<context> [<file>]`), or a context name only one source file defines | the shown cluster |
| `view` | a kind key (`pods`, `gateways.gateway.networking.k8s.io`) or a page key (`cluster/overview`) | the view the connect shows |
| `namespace` | with `name` the object's namespace, else the namespace filter | the filter as it is |
| `name` | an object of the view's kind, whose drawer opens | no drawer |

At least one parameter, each once, percent-encoded, at most 2,048 bytes. Cogpit comes to the front,
connects the context if needed and opens what the link names; a toast says what it opened or why
not, and the audit log records the link (origin `deeplink`).

Security note: any web page or app can open such a link, so a link only navigates: it never writes,
never runs a command that needs a confirmation, never names a file, and never connects a context
whose login program or token file you have not allowed yet. A malformed link is refused whole. Open
gaps: [H-89](../security/trust-boundaries.md#h-89-a-link-switches-the-cluster-and-the-view-under-the-user),
[H-90](../security/trust-boundaries.md#h-90-a-links-text-reaches-agents-through-the-logs).

### Escape

Escape closes the innermost thing open, and only that; press it again to go one step further out
([D-116](decisions.md#d-116-escape-closes-the-innermost-thing)).

| Where you are | Escape |
|---|---|
| an open dropdown's list | closes it without a choice |
| a dialog (a confirmation, Rollback, Scale, a create dialog, Connection Updates, About, …), whatever field has the keyboard | its Cancel, or Close where it has none; the drawer behind it stays open |
| a pop-up list (namespace filter, status filter, columns, the agent's history), the palette, the bell's list, the MCP details | closes it |
| a dock tab's find bar (its fields or the editor) | closes the bar; the editor gets the keys, the find text stays for the next ⌘F |
| a list's search (⌘F) | clears it; the list gets the keys |
| the logs tab's search, Argo CD's activity filter | clears it |
| Preferences or Cluster Settings, a text field on it included | closes the card, keeping the value just typed; a menu or dialog opened from it closes first |
| Preferences › Agent's connection form | closes the form, as its Cancel does |
| the list or the navigator | closes the drawer |

A selected text (a log, a toast, the agent's answer) drops its selection first; the navigator's
search (⌘P) and the namespace filter's search clear typed text first. Fyne's own widgets keep
Escape: a button reached by Tab, the items of a check or radio group (Argo CD's Sync dialog), the
fields of Fyne's file and colour dialogs.

### Selecting and copying a log's, a notification's and the agent's text

In a **logs tab** ([D-99](decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)):

| You do | It does |
|---|---|
| drag over the lines | selects across lines; beyond the tab's top or bottom the lines scroll, faster the farther |
| ⇧-click | extends the selection to the click |
| double-click | selects the word (the run of characters without a space) |
| ⌘A (Ctrl+A) | selects every line the tab shows (its sources, its filter) |
| ⌘C (Ctrl+C) | copies the selection, one line per line as shown (names and timestamps as toggled, a wrapped line as one, a line cut on screen whole) |
| right-click | Copy, Select All, Copy All Lines Shown (what Download saves) |
| Escape | drops the selection |

**Follow** goes off when you select with the pointer (the selection stays on its lines as new ones
arrive) and when you scroll up five lines or more from the newest (wheel, trackpad, scroll bar):
the log stays where you scrolled. A smaller scroll up keeps following that far behind; scrolling
back down to the newest line turns it on. New lines never turn it on. Ticking Follow jumps to the
newest line.

The selection survives Wrap, a new width, a search and a filter; toggling Resource names or
Timestamps drops it. A tab keeps its newest 10,000 lines; a selection loses its part in dropped
lines. Copying shows no toast. `/` jumps to the search.

In a **toast**, drag or double-click selects and ⌘C copies; the copy icon left of the × copies the
whole message. A toast stays while the pointer is on it, its × or its icon, and while its text is
selected. In the **bell's list** a notification selects the same way, its copy icon showing after
its time on hover. Selecting is the window's alone: agents read a logs tab with `dock_read` and the
notifications with `notifications_list`.

In the **agent's column** each block of a message selects on its own (paragraph, heading, list
item, table cell, code block, note, an opened tool row's arguments and result): drag or
double-click, ⌘C copies the text as written (a wrapped line as one, inline code without its chip),
Escape drops it. A drag stays in its block; the copy icon under an answer copies all of it as
Markdown. Links open with a click.

## Connections

### Several clusters connected

Showing another cluster leaves the one you were on connected
([D-69](decisions.md#d-69-several-clusters-connected-at-once)). A click on a connected cluster's
tile (or its F-key), row or tab shows it at once from the cache; its menu says **Show**, and
**Disconnect** closes it alone. Showing another cluster while one connects lets that connect go on
in the background (**Cancel Connecting** in its menus; when done, a toast "Connected to <cluster> in
the background."); clicking or connecting it again waits for the same connect. A failed connect
leaves the window on its error page: Cogpit never switches clusters by itself
([D-97](decisions.md#d-97-a-connect-goes-on-when-another-cluster-is-shown-a-failed-one-stays)).

**The error page** ([D-114](decisions.md#d-114-a-failed-connects-error-page-and-its-stages)):
"Could not connect to <cluster>", the cause under the stage it failed in, and every stage with a
tick, a cross, a ring (not reached) or a dash (skipped). **Connect Again** retries; **Show Connection
Updates** opens the [connection log](#connection-updates-the-connection-log). Agents read
`ui_state`'s `connection.error` and `connectFailure`.

| Stage | Says |
|---|---|
| Kubeconfig valid and complete | context, file, server and login method; or what is missing or wrong |
| Login by the credential plugin | for a context with a login program: how long it took, or why it failed |
| DNS: the API server's name resolved | the name and its addresses; skipped for an address; behind a proxy the proxy's |
| TCP: the address and port reached | the address and port dialled, or refused or unanswered |
| TLS handshake | the TLS version and what the certificate is checked against; or why it failed |
| API server: the login and the first request | `GET /version`'s answer and version; or the 401, 403 or timeout |

Every dot of a cluster (hotbar tile, navigator, welcome page, status bar) names its state in a
tooltip; a click opens the connection log.

| Dot | Means |
|---|---|
| filled green | live: shown, or in the background with its lists kept (5 minutes after you left it) |
| filled green, blinking | connecting or reconnecting |
| green ring | in the background, lists released: showing it lists again (seconds) |
| grey ring | in the background, login lapsed: showing it or Log in again renews it |
| filled red | needs you: a connect or a login renewal failed |
| filled grey | not connected |

The dock shows the shown cluster's tabs plus the Audit Log and Connection Diagnostics; other
clusters' logs, shells and jobs go on behind and come back as you left them. An agent's tab on
another cluster waits in that cluster's dock.

### When the connection breaks

Cogpit checks each connected cluster every 15 s and when a watch breaks, on macOS also when the Mac
wakes or its network changes; two checks in a row without an answer (one after a wake) make it
**interrupted** ([D-58](decisions.md#d-58-standby-and-lost-connections)).

- Its dots blink slowly green; a banner over the view says which cluster, since when, why and the
  next attempt, with **Reconnect**.
- Views, tabs, drawer and dock stay as of the interruption; a view not read before says so ("The
  pods were not read before the cluster stopped answering; they load once it answers again.").
  Changes are disabled ("connection interrupted"); an agent's change is refused and its reads are
  marked as of then.
- Cogpit retries at once on a wake or network change, else after 1, 2, 4, 8, 16, then every 30 s.
  Back, a notification says how long it was away and the views catch up.
- A followed log resumes after a line `[cogpit] ── no connection 11:00:05 – 11:10:07 CEST (10 min
  2 s) · …` (# example) with what was written meanwhile; when the reopened log lacks its last line
  (pod recreated, container restarted, log rotated) a second line says so. Cogpit's lines are
  orange.
- Port forwards wait and connect again. Pod shells, node shells and attach end, their tab offering
  **Reconnect**; local terminals are not affected.
- A running drain or Helm action stops as after a crash; afterwards its banner offers to finish or
  dismiss it.
- A change unanswered for 30 s ends with "outcome unknown"; the view shows the object once back.

A rejected login is renewed once by the context's login program. If that is rejected too, or takes
longer than 30 s (a browser login), the dot turns red (**Login expired**) and the banner offers
**Log in again** and **Reconnect**; Cogpit does not run the program again by itself. A context
without a login program is renewed in its kubeconfig file: Cogpit reconnects by itself once the
file (checked every 2 s) holds another login. After a reconnect or Log in again the navigator's
root names the renewed login's user
([D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)).

### Reconnect: a new connection from the kubeconfig file

A connection keeps the login it read at connect (a certificate renewed in the file does not reach
it). **Reconnect** connects anew from the kubeconfig file as it is now and keeps the tabs
([D-81](decisions.md#d-81-reconnect-and-what-log-in-again-does)); it is in every cluster menu, the
Cluster menu, the palette, the ↻ on the cluster's row in All Clusters, the cluster Overview, the
connection banner and the Connection Updates dialog.

- The old connection serves until the new one stands. Log tabs reopen their streams; shells and
  attach end with **Reconnect**; local terminals run on; port forwards keep their local ports. A
  failed reconnect leaves the cluster as it was and says why.
- Refused while a drain or a Helm action runs on the cluster.
- **Log in again** reconnects too when the context has no login program, or its login in the file
  changed.
- Agents may run `cluster_reconnect`, except for a cluster whose login expired and whose context uses
  a login program: that is Log in again, in the window.

### A cluster upgraded while connected

Cogpit follows the API server's version from the heartbeat, without extra requests
([D-132](decisions.md#d-132-a-cluster-upgraded-while-connected)). A patch updates the version in the
navigator, its upstream support, the Nodes list's version check and a new terminal's kubectl check.
Another minor also shows a banner, "The API server of prod-eu runs v1.36.1 now · Cogpit connected
to v1.35.3" (# example): the kinds and API versions Cogpit knows are the old version's until
**Reconnect**, which keeps the tabs, views and filters. A version counts once two heartbeats in a row
agree (during a rolling upgrade old and new answer in turn). Both changes go into the connection log.

### A login that waits: Cancel Connecting and the 5-minute bound

A login program that opens your browser (kubelogin, pinniped) waits for you
([D-73](decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound)):

- The connecting workspace and the dot's tooltip say "Waiting for the login through <program> since
  14:28; it may wait for you in your browser. Cogpit ends it at 14:33." (# example).
- **Cancel Connecting** (beside it, in the Cluster menu and in the cluster's menus, for a background
  connect too) ends the connect and the login program; the log says "cancelled". A browser window
  the program opened stays open.
- Cogpit ends a login program after **5 minutes** (fixed): "the login through <program> did not
  finish within 5 minutes; Connect starts it again".
- When Cogpit cannot find the program's process it only stops waiting; a later connect waits behind
  that login, without an end time, until its process ends, and says so.
- A connected cluster's login renewal (Log in again's too) shows **Login expired** after 30 s
  ([above](#when-the-connection-breaks)); a login you finish within the 5 minutes still reconnects.
  Cogpit ends the program after 5 minutes, and **Login expired** then says "the login through
  <program> did not finish within 5 minutes; Log in again starts it again".
- Only Connect or Log in again starts an ended login program again, so browser tabs do not pile up.
- The connect's 15 s limit for the API server's first answer excludes the time spent in the browser.

Agents read `ui_state`'s `connection.waitingForLogin` (`program`, `since`, `until`, `runsOn`).
`cluster_disconnect` cancels a connecting cluster's connect without asking. `cluster_connect` and
`hotbar_openCluster` answer `connecting` when another cluster was shown meanwhile; a cancelled
connect answers the error code `cancelled`.

### Connection Updates: the connection log

Every connect, reconnect, login and disconnect of a context is logged step by step, successful ones
too, kept 7 days and at most 1 MiB per context ([D-66](decisions.md#d-66-the-connection-log); the
file is in the [naming table](#naming-conventions)). **Connection Updates: <context>** opens from a
cluster's dot, the welcome page's warning icon, or Show Connection Updates… in a navigator row's
menu, for any context.

- Newest attempt first: kind, start, outcome, duration; then its lines (time, INFO/WARN/ERROR,
  text, duration). An **Interruption** has its own entry; the reconnect after it is the entry above.
- Your own actions are not errors: Cancel Connecting logs INFO lines and "cancelled"; removing a
  kubeconfig source or Remove from Cogpit logs a disconnect.
- A connect logs the kubeconfig file and context, the server, how you log in (a certificate's name
  and expiry, a token "not shown", a login program and whether you allowed it), the certificate
  check, the proxy, the first answer with the version, discovery, the cache's kinds, the heartbeat
  and the access reviews.
- A failure adds its class, the error chain, the limit a timeout hit, and a failed login program's
  standard error, scrubbed of tokens and passwords, at most 4 KiB.
- Each connect shows its stages, as the [error page](#several-clusters-connected) does.
- The open dialog follows new lines; **Copy** copies every attempt as text.
- Agents read it with `cluster_connectLog`.

### Connection Diagnostics: what the requests ran into

When a cluster loads slowly or rows stay missing, **Connection Diagnostics** says what its requests
ran into: refusals, late or missing answers, waits at Cogpit's own rate limits, initial lists the
server did not stream ([D-133](decisions.md#d-133-connection-diagnostics)). Open it with the status
bar's **Diagnostics** button or Help › Connection Diagnostics. The button is muted while nothing
went wrong since the shown cluster's latest connect, orange with the number of problem lines once
something did ("Diagnostics 4", "99+" beyond 99); its tooltip counts the API requests and informers.

The tab, from the top:

- the shown cluster's sums (API requests, watches, objects and memory, where discovery came from,
  table snapshots built), and a line per connected cluster when there are several;
- the connection (server, Kubernetes version, start of the latest connect), with **Overview** /
  **Report** and the copy and save icons;
- the problems since the latest connect by kind, and how the open watches' initial lists came:
  streamed (a cluster's opt-in), listed (the default), or listed after the server did not stream
  them; "streams off, every read lists" and why, while a connection that began streaming lists;
- Cogpit's rate limits, each with how many requests waited, how long, the longest wait and how many
  wait now; orange while one waits or one waited 1 s or more;
- four columns: the **log** (the line seen last first), the API requests by verb and resource, the
  watches with their load time ("loaded in 1.2 s (streamed)"), the objects and memory per kind.

| Rate limit | Rate | For |
|---|---|---|
| requests | 5/s, burst 10 # default | gets and writes; watches never wait |
| lists | 50/s, burst 300 # default | the cache's initial lists |
| discovery | 50/s, burst 300 # default | discovery |
| access reviews | 10/s, burst 20 # default | access reviews |

A log line is one problem with its count: a request client-go retries counts in its line ("12
times since 08:01:04") instead of pushing other lines out.

<details>
<summary>The log's lines</summary>

| Line | When |
|---|---|
| refused | 403 Forbidden; the message names the user and what was refused |
| unauthorized | 401 Unauthorized |
| too many (429) | the API server's API Priority and Fairness asked to slow down, with its Retry-After |
| cache warming | a watch got 429 "storage is (re)initializing" while the API server builds its cache of the resource (first request for a custom resource, after a restart); client-go asks again. Grey and not counted unless the same request gets it again within a minute |
| server error | a 5xx, a down aggregated API's discovery among them |
| no answer | no answer within the request timeout (30 s # default) |
| network | the request did not reach the server, or its answer broke off |
| slow answer | a request that is not a stream took 5 s or more |
| rate limited | a request waited 1 s or more at a Cogpit rate limit, named by its read ("pods in team-a") |
| not streamed | streaming on: the server did not stream a read's initial list (WatchList), so Cogpit listed it; or a stream stalled and its connection lists every read ([initial streaming](#cluster-settings--general-debug-initial-streaming)): the line says whether the stream got no answer or objects then silence, and how the list in its place answered |
| slow load | a read took 5 s or more to load |
| per namespace | a refused read in all namespaces is read namespace by namespace (grey, not counted: the refusal is) |
| connect | a connect began (grey) |

</details>

The log is kept in memory per context across reconnects and dropped at quit: at most 500 lines per
context (the least recently seen goes first), for the 8 contexts used last. Lines show real values
(server address, namespaces, the API server's message); a refused request on a Secret keeps its
status, not its message.

**Report** shows the plain-text report the copy icon and **Save Report…** put out
(`cogpit-connection-diagnostics-<yyyy-mm-dd-hhmmss>.txt`): connection, problems, rate limits,
initial lists, log, watches, requests and the context's latest three connect attempts.
Endpoints are replaced: a host becomes `host-1`, an address `ip-1` (consistently throughout), ports
go, a URL keeps its path (`https://host-1/api/v1/pods`), file paths become `<path>`, a server in a
context name too (`team-a/api-ocp-example-com:6443/jdoe` → `team-a/host-1/jdoe`). Users, groups,
namespaces, objects and API groups stay readable: read it before you send it. Agents use
`diagnostics_get` (real values) and `diagnostics_report`.

## In a cluster

### Alerts: the pill and the alert board

While the Alertmanager integration is on for a cluster (Cluster Settings › Integrations), Cogpit
reads the cluster's Alertmanager
([D-77](decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)).

- **Source:** a Service named as kube-prometheus-stack, kube-prometheus, the Prometheus Operator,
  VictoriaMetrics' operator or the alertmanager chart name theirs
  (`monitoring/kube-prometheus-alertmanager` # example), asked through the API server's service
  proxy every 30 s while the cluster is shown, for the firing alerts neither silenced nor inhibited.
  Your user needs `get services/proxy` on it. For another name, set **Service address**
  (`namespace/service:port`; empty: found by name), **Use HTTPS** and **Path prefix** in Cluster
  Settings › Alertmanager; the line under the address names the one in use.
- **The pill,** left of the bell, hidden while the integration is off. Severity `none`, Watchdog and
  InfoInhibitor are left out; `major` and any other or missing severity count as warning. The
  tooltip counts by severity. When the Alertmanager stops answering, the pill keeps its last count
  for 5 minutes (the tooltip says since when), then turns grey.

| Pill | Means |
|---|---|
| warning sign in the worst severity's colour (red critical, orange warning, amber info), then per severity a bar (red, orange, blue) wider with more alerts, and its count (`99+` beyond 99) | alerts fire |
| green `0` | the Alertmanager answers with none |
| grey `warn` | nothing answers: none found, not allowed, not answering (the tooltip says which) |
| grey `–` | still looking for it |

- **The board** (a click on the pill, or Alerts in the palette, ⌘⇧P): a line of the severities with
  their counts (a click hides or shows that severity's cards), then cards in as many columns as the
  window holds, the worst severity first
  ([D-new](decisions.md#d-new-the-alert-boards-cards)). A card is one alert name:
  - its head in the severity's colour (a click folds the card; a copy icon after the name on hover)
    under a strip of the last 24 hours, a cell an hour, brighter the more often the alert started
    firing in it (its tooltip says when and how often; outlined while unknown);
  - its summary once, the places where the alerts differ shown as `‹pod›`, `‹value›`;
  - a line with what all its alerts share (the namespace, a click narrows the board; the object or
    the workload that controls their pods, a click opens it), the history in words ("fired 7× in
    24 h", "flapping"), **Details** (the description) and **Runbook**;
  - a table of its alerts: only the columns that tell them apart (namespace, object, container, the
    labels that differ, a number from the text) and how long each fires; ten rows, then **Show
    all**. An alert whose text differs from the card's has it under its row.

  The history needs the Prometheus integration and a Prometheus that evaluates the rules (2 requests
  every 5 minutes while the board is shown); without one the line of severities says why. Search
  and namespace filter work as in the lists; the settings link opens Cluster Settings ›
  Alertmanager. Nothing on the board changes Alertmanager: silences stay in its own UI.

### Kyverno's policy violations

While the Kyverno integration is on (Cluster Settings › Integrations) and the cluster serves
Kyverno's `wgpolicyk8s.io/v1alpha2` PolicyReports and ClusterPolicyReports, Cogpit marks every object
a report names with a violation
([D-79](decisions.md#d-79-kyvernos-policy-violations-on-the-objects)):

- **What counts:** a result `fail` in any policy mode (Audit, or Enforce on an object older than the
  policy, found by a background scan) and `warn` (a failed rule of a policy marked as not scored).
  `error`, `pass` and `skip` mark nothing; the report's drawer lists them.
- **Lists:** the warning icon, tooltip "Kyverno: 2 policy violations (require-team-label,
  disallow-latest-tag)", in the view's own warning column (Pods, Nodes, Cron Jobs, …), else in a
  warning column after Name, added while an object of the view violates a policy (custom resources
  included). Sort by it, or search "Kyverno".
- **Drawer:** "Policy violations", a section each: policy (a link while the cluster serves its kind:
  ClusterPolicy, Policy, ValidatingPolicy), rule, result, message, severity, category, how Kyverno
  found it (background scan, admission review), when.
- **Reports:** Custom Resources › wgpolicyk8s.io › Policy Reports and Cluster Policy Reports, with
  Kyverno's columns (Kind, Object, Pass, Fail, Warn, Error, Skip), every result in the drawer.
- **Agents:** `kyverno_violations {context?, namespaces?, policy?, limit?}` lists the violations with
  their objects; `object_get` carries the drawer's sections. Policy messages reach agents as data
  ([H-11](../security/agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)).
- **Reads:** a user who may list the reports only in some namespaces gets those the cluster's
  settings or the namespace filter name. Off: nothing is read or shown. Not read: Kyverno's
  `openreports.io` reports, other engines'.

### A namespace stuck in Terminating

A deleted namespace stays Terminating while objects in it remain, usually one whose finalizers no
controller removes. Its drawer then shows **Blocking Deletion** under Properties
([D-98](decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)):

- **What holds it:** per kind the namespace controller names (condition
  `NamespaceContentRemaining`), the objects being deleted and held by finalizers, each a link with
  its finalizers and **Remove Finalizers**; the kind's objects without finalizers are counted ("1
  more without finalizers, not gone yet"). At most 100 objects are listed, the rest counted.
- **What you may not see:** a kind you may not list there, or one Cogpit has no view of, is a row
  with the controller's count ("1 remaining; you may not list secrets in team-a").
- **Discovery failed** (`NamespaceDeletionDiscoveryFailure`): the unavailable API services with
  their reasons and a link to the Service behind each.
- **It follows the cluster:** an object whose finalizers go leaves the list; when nothing is left
  the drawer says so and the controller removes the namespace within seconds, without a second
  Delete.
- **Remove Finalizers** (in that list, the row menu and the drawer's toolbar of a deleting object
  with finalizers, between Edit and Delete) is confirmed every time, naming the object, each
  finalizer in red and "Skips the cleanup of the controllers that own them (a cloud disk or a DNS
  record may stay)". It removes exactly those finalizers from exactly that object; if they changed
  meanwhile, nothing is removed and Cogpit says so. Never offered on a namespace, and a namespace's
  own finalizer is never forced.
- **Agents** read the section in `object_get` with each row's `object_removeFinalizers` params;
  their request always waits for your confirmation.
- **Reads** happen only while the drawer shows such a namespace, metadata only.

### Debug containers

**Debug** (the magnifier in a pod's drawer, the row menu after Shell, the palette) adds an ephemeral
container to a running pod, as `kubectl debug -it <pod> --target=<container>` does
([D-102](decisions.md#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)):

- **The dialog** is the confirmation: context, namespace, pod; **Target**, the container whose
  processes it sees (the first preselected; None: no shared processes); **Image**, an editable field
  prefilled from Preferences › Kubernetes › Debug container image; what it sees of the target
  ("app's processes and files"); the image's digest, or that it is a floating tag. "This cannot be
  undone." If the pod was replaced under its name meanwhile, nothing is added.
- **What is added:** `debugger-<5 characters>` with stdin and a terminal, sharing the target's
  processes; an **Attach** tab opens on it ("Waiting for debugger-… to start…" while the image is
  pulled). Closing the tab detaches; **Attach**, **Shell** and **Logs** in the pod's menus reach it
  again; `exit` ends it.
- **Lifetime:** as long as the pod object: it survives restarts of the pod's other containers and
  goes with a deleted or replaced pod. It is never restarted; after `exit` it stays Terminated with
  its log. Kubernetes cannot remove or change it.
- **What you see:** in the Pods list's Containers column a blue square per ephemeral container after
  the others (filled while running or starting, outlined once ended, red while its image cannot be
  pulled), running ones first; a cell draws at most 12 squares, "+N" for the rest; tooltip
  "debugger: ephemeral, running, target app". The ratio ("1/1") counts the pod's containers only,
  as kubectl's READY. The drawer lists them under **Ephemeral Containers** with their **Target**, a
  pod's log tab under "Ephemeral containers"; a controller's logs show its template's containers
  only.
- **Refusals:** greyed on a pod that has ended or is being deleted; "not permitted" without
  `patch pods/ephemeralcontainers` and `create pods/attach`. The cluster's own refusals show as
  worded (a namespace enforcing the restricted Pod Security level refuses netshoot; an admission
  policy may refuse it).
- **Agents** run `pod_debug` with an image and target of their choice; it always waits for your
  confirmation, with the same rows.

**Preferences › Kubernetes › Debug container image** (`debugImage`): `""` # default, meaning
`docker.io/nicolaka/netshoot:v0.16@sha256:b09d9b21381f47a79b3cbcb30da25266dc17186ea00ae65e99fdc51396f48e70`,
which follows Cogpit's releases. Cluster Settings › Node Shell › Image works the same, its default's
short name `alpine:3.24` ([D-37](decisions.md#d-37-the-node-shell)). Only you, in Cogpit's window,
set either image; agents and the CLI may only reset it.

- Best pinned by digest (`repository:tag@sha256:…`); a floating tag works and is named as one. The
  line under the field says whether the image in effect is pinned.
- The field shows the short name (`nicolaka/netshoot:v0.16`: no `docker.io/`, no digest). Left at
  the configured image's short name, that image runs pinned as configured; the default's name or an
  empty field sets the default; a changed name runs exactly as typed.
- The confirmations of a node shell and of an agent's Debug name the whole reference and its digest.

Security note: with a target, the debug container reads that container's processes and through them
its files, mounted Secrets included, as root with netshoot
([privilege footprint › Debug container](../security/privilege-footprint.md#debug-container)).

### A release's revisions

**Revisions** (the list icon beside Rollback in a release's drawer, its row menu, "Open revisions"
at the drawer's History, or a revision's number there) opens the dock tab "Revisions · <release>",
one per release ([D-131](decisions.md#d-131-a-releases-revisions)):

- **Top:** **Revision** and **Compare with** (an older revision is compared with the current one by
  default; "The revision before" as a quick pick; None), the shown revision's description (a failed
  one's in red), **Rollback to N**, copy and **Save as…**.
- **Values:** **User-supplied** (`helm get values --revision N`) or **All values** (`--all`), with a
  find bar. Compared: the keys whose values differ, with both values; **Show the line diff**
  unfolds the YAML diff.
- **Manifest:** **Objects** as `kubectl get` lists them (Kind, Name, Namespace) in Helm's install
  order, the selected one's YAML under its `# Source:` template; compared, each marked + (only in the
  shown revision), − (only in the compared one), ~ (changed) or =, a changed one as its diff.
  **manifest.yaml** is the manifest as Helm stored it (`helm get manifest --revision N`). A
  Secret's values are dots and their size until **Show**.
- **Left:** the revisions, ● shown, ◆ compared; a click shows another.
- **Direction:** [Preferences › App: revision diffs](#preferences--app-revision-diffs); an open tab
  follows at once.
- **Copying:** versions, times, names, changed keys and values, the description, the status line and
  every line of values, manifests and diffs are selectable (drag or double-click, ⌘C / Ctrl+C); a
  click on a list row's text still picks the row. The copy icon copies what the tab shows (values,
  changed keys as "path: from → to", a diff as − and + lines, an object, manifest.yaml); **Save
  as…** writes the same to a file.

The tab reads the release records in Cogpit's cache and asks the cluster nothing. `cogpit --demo`
(development builds) has shop-web in demo-shop with six revisions.

### The Kubernetes version and upstream support

While a cluster is connected, the navigator's root shows its API server's version (`v1.36.1` #
example) under your user; after an upgrade it follows within about half a minute
([A cluster upgraded while connected](#a-cluster-upgraded-while-connected)). Its colour says how
upstream supports the minor, by the Kubernetes project's own dates only
([D-91](decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)):

| Shown | When | Tooltip (examples, a German computer's dates) |
|---|---|---|
| `v1.36.1` muted | supported | Upstream support until 28.06.2027 (253 days) |
| `v1.34.2` amber | maintenance mode: the last two months before the end of life | Upstream support until 27.10.2026 (9 days), maintenance mode since 27.08.2026 |
| `v1.33.9 ⚠` orange | past the end of life | Upstream end of life since 28.06.2026 |

Dates are in your computer's format; the tooltip's second line dates the release table this Cogpit
carries ("Upstream dates as of …"). A minor newer than the table gets an estimate, marked "about …
estimated: this Cogpit's table ends at 1.37": a release every four months after the table's newest,
the end of life on the 28th of the fourteenth month after the release month.

- **Managed clusters:** only upstream's dates count; Cogpit knows no distribution's own dates (EKS,
  GKE, AKS, STACKIT SKE, …).
- **Nodes:** the Nodes list's Version column marks a kubelet the same way, and with ⚠ one outside
  the version skew policy (newer than the API server, or more than three minors behind; two for a
  kubelet older than 1.25), the tooltip saying which; the node drawer shows "Upstream support" and,
  outside the policy, "Version skew".
- **Agents:** `ui_state`'s `clusterVersion` (version, minor, released, maintenanceFrom, endOfLife,
  estimated, state, daysLeft, tableNewest, tableFetched), the `tips` per node in `view_rows` and
  `resources_list`, `object_get`'s drawer items.
- **Offline:** the table is built into each release; Cogpit fetches nothing at run time.
  `make k8s-eol` refreshes it ([releasing](releasing.md#the-kubernetes-release-table)).

## Naming conventions

Paths are macOS's: `<user config dir>` is `~/Library/Application Support`, `<user cache dir>`
`~/Library/Caches`. On Linux `$XDG_CONFIG_HOME` (else `~/.config`) and `$XDG_CACHE_HOME` (else
`~/.cache`), on Windows `%APPDATA%` and `%LOCALAPPDATA%` (Go's rule). Owner-only is 0600 in a 0700
directory; on Windows modes are neither set nor checked: files inherit the user profile's ACLs (the
user, SYSTEM, Administrators), a temporary file those of `%TEMP%`
([D-111](decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)).

<details>
<summary>Binaries, packages and downloads</summary>

| What | Name |
|---|---|
| Go module | `github.com/guided-traffic/cogpit` |
| Binaries | `cogpit` (desktop app), `cogpit-cli` (headless CLI, MCP stdio bridge) |
| App bundle | `Cogpit.app`, bundle id `io.guided-traffic.cogpit`; `Contents/MacOS/cogpit-cli` with signing identifier `io.guided-traffic.cogpit.cli` |
| Linux package | `cogpit`; app id `io.guided-traffic.cogpit` (launcher, icon, AppStream entry, Wayland app id), `WM_CLASS` `Cogpit`, polkit action `io.guided-traffic.cogpit.update` ([files](#install-on-linux)) |
| Windows setup | `%LOCALAPPDATA%\Programs\Cogpit` (for every user `C:\Program Files\Cogpit`); Start menu entry `Cogpit.lnk`; uninstall entry `…\CurrentVersion\Uninstall\io.guided-traffic.cogpit` in HKCU (for every user HKLM, 64-bit view) ([files](#install-on-windows)) |
| Release artifacts | `Cogpit-<version>-macos-<arch>.dmg` (`Cogpit_<version>_darwin_<arch>.zip`, signed ad hoc, from a pipeline without the signing secrets), `cogpit_<version>_<arch>.deb` (`amd64`, `arm64`), `Cogpit-<version>-windows-x64-setup.exe`, `SHA256SUMS` ([download](#download-a-release)) |
| Update feeds | `https://guided-traffic.github.io/cogpit/appcast.xml` # default (the `SUFeedURL` of an app built with the update key); `https://guided-traffic.github.io/cogpit/appcast-windows.xml` # default (built into `cogpit.exe`; items `sparkle:os="windows-x64"`) |
| APT repository | `https://guided-traffic.github.io/cogpit/apt/` # default, named by `/etc/apt/sources.list.d/cogpit.sources`: suite `stable`, component `main`, `Origin` and `Label` `Cogpit` ([its key](#install-on-linux)) |

</details>

<details>
<summary>Names in the code, the cluster and the environment</summary>

| What | Pattern | Example |
|---|---|---|
| Packages | `internal/<area>` | `internal/cache` # example |
| Command ids | `<kind-or-area>.<action>`, lowerCamelCase segments | `deployment.restart`, `ui.navigate` |
| MCP tool names | the command id, dots replaced by underscores | `deployment_restart` |
| Kind keys | built-in kinds: the plural resource name; custom resource kinds: `<plural>.<group>` ([why](architecture.md#45-kind-keys)) | `pods`, `gateways.gateway.networking.k8s.io` |
| Page keys (`ui.openView`'s `view` for a page that is not one kind's list) | `<navigator group>/<page>` | `cluster/overview`, `flux/dashboard` |
| A cluster's name in the window | the display name from Cluster Settings › General, else the context name's part after its last `@` (the whole name when it has none or nothing follows it), without the kubeconfig file; commands and `ui_state` keep the full context id ([D-68](decisions.md#d-68-one-short-name-for-a-cluster-and-the-user-cogpit-is-logged-in-as)) | `kubernetes-admin@prod-eu [dev.yaml]` → `prod-eu` |
| Flux annotations | `reconcile.fluxcd.io/requestedAt` (Reconcile, Resume), `…/forceAt` (forced Helm release), `…/resetAt` (Reset): RFC 3339 with nanoseconds, as the flux CLI writes it | `2026-10-13T22:01:02.345678901Z` |
| Helm chart objects | kind `helmcharts`, named `<repository>.<chart>`, the repository's dots doubled (a repository name: 1 to 63 letters, digits, `.`, `-`, `_`, starting with a letter or digit; others are listed as not used) | `charts..example.nginx` for `charts.example` |
| Node shell pod | `kube-system/cogpit-node-shell-<8 random characters>`, labels `app.kubernetes.io/name=cogpit-node-shell`, `app.kubernetes.io/managed-by=cogpit`, annotation `cogpit.io/node-shell-id`; deleted when the shell closes or, after a crash, at the next connect; Cluster Settings › Node Shell › Maximum duration ends it (2 hours # default, 5 minutes to 24 hours) ([why](../security/privilege-footprint.md#node-shell)) | `kube-system/cogpit-node-shell-k3v9q2mx` |
| Debug container | `debugger-<5 lowercase characters>`, never a name the pod has ([why](../security/privilege-footprint.md#debug-container)) | `debugger-x7k2q` |
| Dock tabs | node shell `nodeShell-<n>`, title `Node shell: <node>`; Attach (Debug's too) `attach-<n>`, title `Attach: <pod>/<container>` | `Attach: web-0/debugger-x7k2q` |
| Deep links (macOS) | `cogpit://open?context=<context id>&view=<view key>&namespace=<namespace>&name=<name>`, registered in the app's `CFBundleURLTypes` (name `io.guided-traffic.cogpit`); audit log entry `deeplink.open`, origin `deeplink` ([deep links](#deep-links-cogpit)) | `cogpit://open?view=pods&namespace=team-a` |
| Demo context | `demo`, the synthetic cluster of development builds; in a release an ordinary context name | |
| Local kind cluster | cluster `cogpit`, context `kind-cogpit`, kubeconfig `.cache/kind/kubeconfig` | |
| Build tags | `nodemo` (release builds: the Makefile's `DIST_TAGS`), `nothreadcheck` (Fyne's thread check off: `FYNE_THREAD_CHECKS=off`, the `dist-*` targets' default) ([releasing › What gets built](releasing.md#what-gets-built)) | `go version -m bin/cogpit` shows `-tags=nodemo,nothreadcheck` for a release |
| Cogpit's `PATH` | `PATH` at start, then the built-in folders and the program folders, appended ([order](#preferences--kubernetes-program-folders)) | `/usr/bin:/bin:/opt/homebrew/bin:/Users/me/.krew/bin` |
| MCP endpoint | `http://127.0.0.1:<port>/mcp`: the port kept in `mcp-port`, a free one (then kept) the first time or when it is taken | `http://127.0.0.1:59449/mcp` |
| Environment variables | `COGPIT_<NAME>` (below) | `COGPIT_LLM_TEST_URL=http://localhost:1234/v1` |

Environment variables:

- **The app:** `COGPIT_SYSTEM_TITLEBAR` (set: macOS's own title bar); in development builds
  `COGPIT_LIGHTSTRIP_FPS` (the [light strip](#the-light-strip)'s frame cap, 1 to 60, else ignored
  with a warning; 60 # default).
- **A terminal whose kubectl Cogpit chose:** `COGPIT_KUBECTL_DIR`, `COGPIT_KUBECTL_ENFORCE`,
  `COGPIT_ZDOTDIR`, `COGPIT_ZDOTDIR_SET`, removed by its shell's start-up.
- **Tests** ([testing](testing.md#tiers)): `COGPIT_KIND_KUBECONFIG`, `COGPIT_KIND_CONTEXT`,
  `COGPIT_KIND_DRAIN_KUBECONFIG`, `COGPIT_KIND_DRAIN_CONTEXT`, `COGPIT_KIND_FALLBACK`,
  `COGPIT_ENVTEST_ASSETS`, `COGPIT_PNG_DIR`, `COGPIT_SCORECARD`, `COGPIT_SIGSTORE_LIVE`,
  `COGPIT_SPARKLE_FRAMEWORK`; `COGPIT_LLM_TEST_URL`, `_MODEL`, `_KIND`, `_KEY` for
  `TestLiveEndpoint` (skipped without them); `COGPIT_REAL_*` for a lent real cluster, never in CI:
  `COGPIT_REAL_KUBECONFIG` (passed to Cogpit alone), `COGPIT_REAL_CONTEXT` (default: the file's
  current context), `COGPIT_REAL_ROUNDS`, `COGPIT_REAL_IDLE`, `COGPIT_REAL_THANOS`, ….

</details>

<details>
<summary>Files and folders Cogpit writes</summary>

In `<user config dir>/Cogpit`, owner-only, unless named otherwise.

| File | Holds |
|---|---|
| `preferences.json` | the Preferences |
| `kubeconfigs.json`, `exec-approvals.json` | the kubeconfig sources you added; the credential plugins you allowed. Cogpit never writes a kubeconfig |
| `cluster-settings.json`, `cluster-icons/` | per context what Cluster Settings sets, at most 1,024 contexts; uploaded icons with their source images. Removing a cluster from Cogpit drops its settings (added again, it is asked its integrations again); a file `$KUBECONFIG` names keeps them |
| `cluster-users.json` | per context the username the API server last confirmed, for the navigator while disconnected |
| `metrics.json` | per context the Prometheus (namespace, service, port, path prefix, `format`, `keepEmptyContainers`, `https`) and the Alertmanager you named; never the kubeconfig |
| `hotbar.json` | the pinned clusters, top to bottom ([D-83](decisions.md#d-83-one-hotbar-and-a-drag-that-carries-the-tile)) |
| `workspace.json` | tabs, column layout, navigator and chat column, the clusters connected and shown: the layout comes back at every start, tabs and connections only with [Restore the last session](#preferences--app-restore-the-last-session) ([D-41](decisions.md#d-41-what-survives-a-restart)) |
| `window.json` | the window's place ([its section](#the-windows-place-and-size)) |
| `bookmarks.json` (macOS) | access to what you picked in the system's panels; deleting it removes that access ([file access](../security/file-access.md)) |
| `drafts/` | unsaved Edit and Create tab text and Config Map drawer edits, offered again at the next connect; never a Secret's or Helm values ([D-11](decisions.md#d-11-crash-stance), [D-55](decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)) |
| `drains.json`, `helm.json`, `nodeshells.json`, `journals.lock`, `owners/` | the drains, Helm actions and node shell pods Cogpit started and has not finished or seen deleted, and which Cogpit process owns each; a node shell pod a crash left is deleted at the next connect ([H-71](../security/privilege-footprint.md#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)) |
| `audit.log` | a JSON line per agent-initiated command, read newest first by Help › Audit Log and `audit_list` (its last 4 MiB) |
| `connection-logs/` | a file per context of its [connect attempts](#connection-updates-the-connection-log), shared with `cogpit-cli`; no credential ([H-112](../security/credentials.md#h-112-a-credential-plugins-standard-error-in-the-connection-log)) |
| `llm-connections.json`, `llm-keys.json` | the chat connections (never a key); their API keys in plain text ([H-99](../security/credentials.md#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)) |
| `chat-conversations.json` | the last 20 conversations per context, Secret values replaced; never one with a turn over a private connection ([H-96](../security/agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)) |
| `helm-http-approvals.json` | the Helm repositories you allowed to send credentials over plain http; no credential ([H-40](../security/credentials.md#h-40-credentials-over-plain-http)) |
| `mcp-token`, `mcp-port`, `mcp-endpoint`, `mcp-switch` | the MCP bearer token, kept until Regenerate token; the port the next start binds; the endpoint URL while the server listens; the switch, `on` or `off`, no file meaning off |
| `kubectl/<version>/kubectl` | kubectl downloaded for terminals, unless `kubectlDir` names another folder ([kubectl](#preferences--kubernetes-kubectl-for-terminals)) |
| `crash.log`, `crashes/` | the crash output; the next start keeps the newest 10 reports in `crashes/` and says where |
| `<user cache dir>/Cogpit/cogpit.log` | the app log (also on stderr) |
| `<user cache dir>/Cogpit/discovery/` | discovery answers per API server and identity, shared with `cogpit-cli`; deleting it costs the next connect its discovery requests ([D-65](decisions.md#d-65-discovery-cached-on-disk)) |
| `<user cache dir>/Cogpit/helm/` | repository indexes, chart READMEs and chart icons; Helm's own cache is not touched |
| `<temp dir>/cogpit-terminal-<random>.kubeconfig`, `<temp dir>/cogpit-terminal-<random>/` | a terminal's kubeconfig with its context only, and the folder that puts Cogpit's kubectl first; removed on close ([why](../security/credentials.md#the-terminals-kubeconfig)) |
| Helm's `repositories.yaml` | read every 2 s, never written: `$HELM_REPOSITORY_CONFIG`, else `$HELM_CONFIG_HOME/repositories.yaml`, else Helm's folder (`~/Library/Preferences/helm/repositories.yaml` # default on macOS); manage it with `helm repo add` and `helm repo remove` ([D-29](decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)) |
| `~/Library/Preferences/io.guided-traffic.cogpit.plist` | Sparkle's state: automatic checks (`SUEnableAutomaticChecks`, set from Cogpit's preference), the last check, a skipped version |
| `HKCU\Software\guided-traffic\Cogpit\WinSparkle` | WinSparkle's state: a skipped release (`SkipThisVersion`), which the Update button leaves out; removed on uninstall |

</details>

<details>
<summary>Identifiers the documentation uses</summary>

| Identifier | Meaning | Where |
|---|---|---|
| `G<n>` | A gap against Lens or a beyond-Lens target | [docs/parity/gaps.md](../parity/gaps.md) |
| `M<n>` | A milestone | [docs/parity/roadmap.md](../parity/roadmap.md) |
| `D-<n>` | A decision | [docs/developer/decisions.md](decisions.md) |
| `H-<n>` | An open security gap | [docs/security/](../security/README.md) |
| `P<n>` | A performance or API-load budget | [docs/developer/testing.md](testing.md#performance-budgets) |
| `S<n>` | A gate criterion of the Fyne spike | [docs/developer/spike-fyne.md](spike-fyne.md#gate-criteria) |

</details>

## Development

Working on Cogpit: [docs/developer/](README.md); what to check before a pull request:
[testing](testing.md#before-a-pull-request); CI and releases: [releasing](releasing.md).

### Make targets

`make help` lists every target; details: [testing](testing.md), [releasing](releasing.md),
[hack/kind](../../hack/kind/README.md).

| Target | What it does |
|---|---|
| `make build` | `bin/cogpit` (cgo, Fyne) and `bin/cogpit-cli` (pure Go), a development build: demo cluster and Fyne's thread check on; `GO_TAGS=nodemo` and `FYNE_THREAD_CHECKS=off` give a release's variant |
| `make run`, `make run-demo` | run against the kind cluster, or the demo cluster |
| `make app` | `bin/Cogpit.app` with Sparkle and `cogpit-cli` inside; `SPARKLE_PUBLIC_ED_KEY=<base64>` for an app that updates itself |
| `make lint`, `make lint-darwin`, `make fmt` | golangci-lint at the Makefile's `GOLANGCI_LINT_VERSION` (built into `bin/`): the team's linters and the `depguard` layering, for this system's build and Windows' (`lint-darwin`: the packages with darwin-only files, macOS only); formatting |
| `make test-unit`, `make test-unit-race` (`make test`) | every test that needs no cluster, without and with the race detector (the latter with `make test-fork`, the patched terminal widget's); `internal/ui/fyneui` in `UI_SHARDS` (eight) shards balanced by the tests' measured time, `UI_JOBS` at once |
| `make test-ui`, `make test-ui-race` | `internal/ui/fyneui` alone, sharded the same way ([D-112](decisions.md#d-112-ci-and-test-time)) |
| `make ui-durations` | measures `internal/ui/fyneui`'s tests with and without the race detector into the files that balance its shards ([releasing › The UI's shards](releasing.md#the-uis-shards)) |
| `make test-kind`, `make test-kind-drain`, `make test-envtest`, `make test-real` | cluster tests against the kind cluster, the drain cluster, envtest's API server, a real cluster you name (`REAL_KUBECONFIG`, `REAL_CONTEXT`; never in CI) |
| `make test-norace-coverage`, `make test-race-ui-build`, `make test-race-ui`, `make test-race-rest`, `make test-darwin`, `make test-windows-unit`, `make test-windows-ui` | CI's test jobs ([releasing › CI](releasing.md#ci)) |
| `make check-nodemo` | the release variant (CI's lint job): `internal/demo` in both binaries without `-tags nodemo` and in neither with it, the `nodemo` files linted with the tag, `TestNoDemoInARelease` |
| `make coverage-report` | merges the profiles in `.cache/coverage` into `combined.out`, `summary.md` and the badge `.github/badges/coverage.json` |
| `make vuln`, `make gosec`, `make gosec-darwin`, `make cyclo`, `make cyclo-report` | govulncheck over the code paths Cogpit calls, gosec over the shipped code (this system's build and Windows'; `gosec-darwin`: the packages with darwin-only files, macOS only), no function above `CYCLO_THRESHOLD` (56 # default), the 20 most complex functions |
| `make parity`, `make scorecard`, `make bench` | the parity checks ([rules](../parity/README.md#make-parity)); the performance budgets measured into `docs/parity/scorecard.md` (not in CI); the benchmarks, the spike's S1 and S2 included |
| `make kind-up`, `make kind-seed`, `make kind-down`, `make kind-drain-up`, `make kind-drain-down` | the local kind cluster (with the Metrics Server) and its seed; the three-node drain cluster `cogpit-drain` (`.cache/kind-drain/kubeconfig`) |
| `make kind-prometheus`, `kind-kps`, `kind-vm`, `kind-alertmanager`, `kind-kyverno`, `kind-trivy` (each with `-down`) | optional stacks in the kind cluster: Prometheus, kube-prometheus-stack, VictoriaMetrics, Alertmanager, Kyverno with test policies, Trivy Operator |
| `make sigstore-root`, `make k8s-eol` | refresh the sigstore trusted root `internal/kubectl` embeds and the Kubernetes release table `internal/model/k8seol` embeds (`K8SEOL_FLAGS=-fallback` keeps the committed one on a failure) |
| `make icon`, `make showcase` | redraw `packaging/icon.png` ([extending](extending.md#changing-the-logo)); render the README's pictures into `docs/images`, dark and light, optimised with oxipng (`OXIPNG_FLAGS` without `--zopfli` for a quick look) |
| `make sparkle`, `make oxipng`, `make winsparkle`, `make mesa` | the pinned release into `.cache/`, SHA-256 checked (oxipng: macOS on Apple silicon and Linux on x86-64; Mesa: the Windows smoke test's software OpenGL) |
| `make smoke` | starts `SMOKE_BIN` (`bin/cogpit` # default) once with `--smoke SMOKE_BOUND` (`2m` # default): its window until the first frame ([releasing › the smoke test](releasing.md#the-smoke-test)) |
| `make dist-app`, `dist-macos`, `dist-macos-sign`, `dist-macos-notarize`, `dist-checksums`, `appcast-draft`, `appcast-sign`, `appcast-verify` | the macOS release and its appcast (`MACOS_SIGN=adhoc` for a local check) ([releasing](releasing.md#running-the-signing-locally)) |
| `make dist-linux VERSION=x.y.z`, `test-linux-package`, `apt-repo`, `apt-verify` | the `.deb` for this machine's architecture (Debian or Ubuntu with `dpkg-dev`), its check (lintian, `hack/linux/apt-test.sh`), the APT repository ([releasing › Linux](releasing.md#linux)) |
| `make dist-windows VERSION=x.y.z`, `test-windows-package`, `appcast-windows-draft`, `appcast-windows-sign`, `appcast-windows-verify` | the Windows setup (on Windows, with MinGW-w64 gcc and NSIS 3), its silent install check (`hack/windows/setup-test.sh`; `WINDOWS_SMOKE=on` starts the installed app once), its appcast ([releasing › Windows](releasing.md#windows)) |
| `make clean` | removes `bin/` and `dist/` |
