# File access on the user's computer

What Cogpit reads and writes of the user's own files outside its own folders, how it gets macOS's
permission for the places macOS's privacy settings protect (the Desktop, Documents and Downloads
folders, iCloud Drive, removable and network volumes), and the bookmarks that carry that
permission into later runs ([D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access),
built 2026-10-16 night). Cogpit is not sandboxed: macOS's Files and Folders privacy (TCC) is the
one check between Cogpit and these places; everywhere else the user's file permissions decide.

## What Cogpit reads and writes of the user's files

| Item | What Cogpit does with it | How it is chosen | Access kept for later runs |
|---|---|---|---|
| Kubeconfig sources: files and sync folders | Read at start, a sync folder's files again every 2 s, a file when a cluster connects; never written ([trust boundaries](trust-boundaries.md), [credentials](credentials.md)) | `~/.kube` from the start (removable); `--kubeconfig` or `$KUBECONFIG` for the run; the rest in Sync Files and Folders, on macOS the system's open panel ([`filepicker_choosers.go`](../../internal/ui/fyneui/filepicker_choosers.go)) | A picked file or folder: a read-only security-scoped bookmark, kept while it is a source; none for `~/.kube` (not protected) and the run's files (in a protected folder macOS asks once) |
| Files a kubeconfig names: client certificates, token files, a credential plugin's program | Read when the cluster connects; a plugin's program hashed for its approval and run on connect (H-1, D-17). Since 2026-10-17 a `client-certificate` file also whenever the contexts are listed, for its two dates ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)): not when it lies in a protected place, or a link on the way leads into one (then read right after client-go read it to connect, and remembered for the run), and never its key file ([`cluster/clientcert.go`](../../internal/cluster/clientcert.go), `TestProtectedClientCertificateIsReadAtConnect`, `TestListingNeverOpensTheKey`) | By the kubeconfig, never in a panel | None: in a protected folder macOS asks Cogpit once |
| A chat connection's CA file | Read for the connection's test and its turns ([trust boundaries › H-104](trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)) | Preferences › Agent › Choose…, the system's open panel (or typed) | A picked file: a read-only bookmark |
| A cluster's icon | Read once (at most 1 MiB), checked and copied into `<user config dir>/Cogpit/cluster-icons/`; the window draws the copy ([trust boundaries](trust-boundaries.md#boundaries-and-required-checks)) | Upload Icon…, the system's open panel | None: the copy is Cogpit's own |
| A cluster's terminal working directory | A local shell is started there; the shell and what the user runs in it reach whatever the user does | Cluster Settings › Terminal, the system's folder panel (or typed) | A picked folder: a read-write bookmark |
| The folder for kubectl's binaries | Downloaded releases written there and run in terminals (D-3) | Preferences › Kubernetes, the system's folder panel (or typed) | A picked folder: a read-write bookmark |
| A saved table, manifest or log | Written once, to the file the user named ([credentials](credentials.md#where-credentials-must-never-end-up) for what a save refuses) | The system's save panel, which asks before replacing a file | None. A new file is created owner-only (0600) |
| Helm's `repositories.yaml` | Read every 2 seconds, never written ([architecture §8](../developer/architecture.md#8-helm-internalhelm)) | Helm's own location (`~/Library/Preferences/helm` on macOS) or `$HELM_*` | None needed outside the protected places |
| Cogpit's own files | `<user config dir>/Cogpit` and `<user cache dir>/Cogpit`, owner-only ([usage › Naming conventions](../developer/usage.md#naming-conventions)) | — | — (not protected) |

Where the system has no panel (Windows and Linux until M11, Fyne's test driver) each chooser shows
Fyne's dialog, and Sync Files and Folders Cogpit's own browser, which lists only the folders the
user opens. Fyne's file dialog reads the user's Desktop, Documents, Downloads, Music, Pictures and
Videos folders for its sidebar each time it opens (`fyne.io/fyne/v2/dialog/file.go`,
`loadFavorites`): on macOS that raised up to three privacy prompts before the user chose anything,
which is why no Fyne file dialog opens on macOS since D-72. Windows and Linux have no such prompts.

Only the user picks: adding a kubeconfig source, uploading an icon and the saves are the window's
own actions, the kubectl folder is refused for agents (`ErrKubectlGUIOnly`), and no panel opens
for an agent ([agents and MCP](agents-and-mcp.md)). A cluster's terminal working directory is a
setting agents can set too (`cluster.settings.set`, a path typed, no panel, no bookmark); a
terminal started there runs as one the user started.

## How macOS grants access

Documented by Apple, not tried on a real Mac in the run that built it (D-72 lists the sources and
the walk steps that check it):

- **A pick in the system's panel is granted without a prompt.** Since macOS 10.15 the open and save
  panels run in a separate process, sandboxed app or not, and the chosen item is granted to the app
  ("user intent"). Browsing the panel lists folders outside Cogpit's process and costs no prompt.
  macOS records the grant on the item (the `com.apple.macl` extended attribute, which SIP
  protects); how long it lasts is not documented and is reported differently (until a restart of
  the Mac; until `tccutil reset`). It does not cover iCloud Drive (Howard Oakley, 2026).
- **Anything else in a protected place asks once.** The prompt names Cogpit and says why with the
  bundle's usage descriptions (`NSDesktopFolderUsageDescription`, `NSDocumentsFolderUsageDescription`,
  `NSDownloadsFolderUsageDescription`, `NSRemovableVolumesUsageDescription`,
  `NSNetworkVolumesUsageDescription`, written by `make app`). macOS keeps the answer for the app's
  code signing identity: the release (Developer ID) is asked once, a build of `make app` or
  `make build` (ad hoc) after every build again. The answer is shown and changed in System
  Settings › Privacy & Security › Files and Folders, or reset with `tccutil reset
  SystemPolicyDocumentsFolder io.guided-traffic.cogpit` # example.
- **A security-scoped bookmark** of a pick is made while the panel's grant is fresh, kept, and
  resolved at the next start before anything reads a source, its item accessed for the process's
  life ([`internal/ui/nativedialog`](../../internal/ui/nativedialog/), [`cmd/cogpit/fileaccess.go`](../../cmd/cogpit/fileaccess.go)).
  Apple documents these bookmarks for sandboxed apps; Apple DTS names them among what the
  user-intent check considers. Whether macOS honours them for an app without a sandbox is not
  documented. **Observed 2026-10-19** (the owner's walk, the v0.26.1 release): with Cogpit's
  Downloads grant reset (`tccutil reset SystemPolicyDownloadsFolder io.guided-traffic.cogpit`), a
  source picked in Downloads earlier was read at the next start of Cogpit without a prompt, and no
  grant was recorded afterwards; a log saved to the Desktop through the save panel without a grant
  for the Desktop raised none either. Whether the bookmark or the item's own grant from the pick
  (`com.apple.macl`, above) carried it is not told apart, and a restart of the Mac was not tried
  (D-72). Verified: Cogpit's process makes, resolves and starts accessing them
  (`TestBookmarkHarness`, `TestBookmarkRoundTrip`).

## The bookmarks file

`<user config dir>/Cogpit/bookmarks.json`, macOS only in practice (no other build makes a
bookmark), owner-only: 0600 in a 0700 directory, written whole and atomically through
`engine.WritePrivateFile`, never through a symlink, read through `engine.ReadPrivateJSON` (a
regular file of at most 4 MiB) ([`store.go`](../../internal/ui/nativedialog/store.go),
`TestStoreKeep`, `TestStoreUnreadable`). Each entry holds the item's path, its use (`kubeconfig`,
`folder`, `caFile`), its access (read-only or read-write) and the bookmark's data. It holds no
credential, but it names where the user keeps kubeconfigs and CA files. A bookmark resolves with
its scope only for the app and user that made it (Apple's App Sandbox design guide for
app-scoped bookmarks; not verified here outside the sandbox), so a copied or edited file grants
another program nothing it did not have; Cogpit resolves only what its own window put there, and
an entry someone else wrote is at worst an item Cogpit starts accessing without reading it. Bounds:
a kubeconfig source's bookmark goes with the source at the next start; of the folders and the CA
files the newest 16 each stay; a bookmark that does not resolve (an item gone, a volume not
mounted) stays within those bounds. An unreadable file is replaced by the next pick. Removing the
file removes the kept access; the next pick makes it again.

## What this does not cover

### H-116: Programs Cogpit starts act with Cogpit's folder access

A local terminal's shell, the programs the user runs in it, a credential plugin and the kubectl
Cogpit offers are processes Cogpit starts, and macOS treats Cogpit as the app responsible for them
(Apple DTS, "On File System Permissions": the prompt and the decision belong to the responsible
app). Live today, for every terminal and every credential plugin: once the user has allowed Cogpit
a protected folder (a prompt answered OK, a source picked there), such a program reads that folder
without asking, and a prompt a program raises names Cogpit and Cogpit's reason, not the program.
A credential plugin is run only after the user approved it (H-1), and a terminal runs what the user
types, as Terminal.app's shells do under Terminal's grants; what is not checked is that a program
uses only the folder the user meant. Documented, not verified on a real Mac.

### H-119: A client certificate file is read while the contexts are listed

Since 2026-10-17 ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists))
Cogpit opens a context's `client-certificate` file whenever it lists the contexts (at start, when
a kubeconfig changes, when the section shows), before the user connects, to show how long the
certificate is valid. It keeps the leaf certificate's two dates and nothing else; the key file is
never opened. What remains, per case:

- **A protected place the list does not know.** Cogpit skips a file in the Desktop, Documents and
  Downloads folders, iCloud Drive, `~/Library/CloudStorage`, `~/Library/Containers`,
  `~/Library/Group Containers` and `/Volumes`, and one a link leads into them, checking each
  component before it looks at it. A place macOS protects that is not in that list (a network
  share mounted elsewhere than `/Volumes`, a folder a later macOS adds) is read at listing and can
  raise a prompt there that names Cogpit before the user chose to connect. Dormant unless a
  kubeconfig names a certificate in such a place; the list is Cogpit's reading of Apple's
  documentation, not verified on a real Mac, nor is whether looking at a path's metadata inside a
  protected folder prompts at all (the walk avoids it either way).
- **A file that does not answer.** A certificate on a network mount that hangs holds the listing
  (off the window's goroutine; the section says "Reading the kubeconfigs…") as a kubeconfig on
  such a mount would. Dormant unless a kubeconfig names one.
- **Any file the user can read.** A kubeconfig dropped into a sync folder can name any file as its
  `client-certificate` ([trust boundaries › H-24](trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)):
  Cogpit opens it at listing (regular files of at most 256 KiB only, never a FIFO or a device) and
  parses it as PEM; nothing of it is kept but a CERTIFICATE block's two dates, and a file that is no
  certificate gives a debug log line naming its path and a fixed problem, never its content. Live
  for every listed context whose user names a certificate file.

Also outside this page: the files a kubeconfig names in a protected folder (a certificate, a token
file) are not picked in a panel and ask once; iCloud Drive asks whatever the user picked; macOS's
Privacy & Security settings may not list an access granted by a pick (Oakley, 2026), so revoking it
takes `tccutil reset` and removing `bookmarks.json`; the Local Network privacy prompt for clusters on
the local network is not file access.
