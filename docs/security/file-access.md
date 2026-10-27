# File access on the user's computer

What Cogpit reads and writes of the user's files outside its own folders
([usage › Naming conventions](../developer/usage.md#naming-conventions)), how macOS grants it the
places its privacy settings protect (Desktop, Documents, Downloads, iCloud Drive, removable and
network volumes), and the bookmarks that keep the grant
([D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access)). Cogpit is not
sandboxed (signed without entitlements, `check_signature` in
[`release.sh`](../../hack/macos/release.sh)): macOS's Files and Folders privacy (TCC) is the one
check before these places; elsewhere the user's file permissions decide.

## What Cogpit reads and writes of the user's files

On macOS every chooser is the system's panel
([`filepicker_choosers.go`](../../internal/ui/fyneui/filepicker_choosers.go); which one where:
[usage › Choosing files](../developer/usage.md#choosing-files-the-systems-panels)).

| Item | What Cogpit does with it | Access kept for later runs |
|---|---|---|
| Kubeconfig sources: files and sync folders | Checked every 2 s (size, modification time, a folder's listing); read when the contexts are listed and at connect; never written | Picked in Sync Files and Folders: a read-only security-scoped bookmark while it is a source. `~/.kube` (not protected) and the run's `--kubeconfig` or `$KUBECONFIG` files: none |
| Files a kubeconfig names: certificates, keys, token files, a credential plugin's program | Read by client-go at connect; a plugin runs once approved (H-1, D-17). A program or CA file in the kubeconfig's folder is hashed for that approval at every listing and connect (D-26, [`pluginfiles.go`](../../internal/cluster/pluginfiles.go)). A `client-certificate` file is also read at every listing for its dates ([H-119](#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)); the key only by client-go | None; in a protected folder macOS asks once |
| A chat connection's CA file | Read (at most 1 MiB) for every turn, connection test and models list ([trust boundaries › H-104](trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)) | Picked: a read-only bookmark |
| A cluster's icon | Read when the Custom icon's dialog shows it and when it is kept; checked and copied, the square cut from it, into `<user config dir>/Cogpit/cluster-icons/`; only the copies are drawn ([trust boundaries](trust-boundaries.md#boundaries-and-required-checks), D-119) | None |
| A cluster's terminal working directory | A local shell starts there and reaches whatever the user can | Picked: a read-write bookmark |
| The folder for kubectl's binaries | Downloaded releases written there, run in terminals (D-3) | Picked: a read-write bookmark |
| Program folders | Searched for credential plugins' programs and on the `PATH` of every program Cogpit starts, after `PATH` and Cogpit's built-in folders; one every user may write to is refused (D-120, [`programfolders.go`](../../internal/engine/programfolders.go)) | Picked: a read-only bookmark |
| A saved table, manifest, log, diagnostics report or revision text | Written once to the file named in the save panel, which asks before replacing it (what a save refuses: [credentials](credentials.md#where-credentials-must-never-end-up)) | None. A new file is 0600 through the system's panel (`createPicked`), the umask's mode through Fyne's dialog |
| Helm's `repositories.yaml` (`~/Library/Preferences/helm` on macOS, or `$HELM_*`) | Checked every 2 s, read when changed or when the last read failed, never written; an entry's CA file, certificate and key read at each download ([architecture §8](../developer/architecture.md#8-helm-internalhelm)) | None needed outside the protected places |

Where no system panel shows (Windows and Linux, D-72; Fyne's test driver) each chooser is Fyne's
dialog, which reads the user's standard folders for its sidebar whenever it opens (`loadFavorites`
in Fyne's `dialog/file.go`; on macOS that would prompt), and Sync Files and Folders is Cogpit's own
browser ([`syncpicker.go`](../../internal/ui/fyneui/syncpicker.go)).

Only the user picks: no panel opens for an agent. Adding a source, an icon or a program folder and
setting the kubectl folder (`ErrKubectlGUIOnly`) are GUI-only; a chat connection and the saves
exist only in the window ([agents and MCP](agents-and-mcp.md)). Agents can set a cluster's terminal
working directory (`cluster.settings.set`: a typed path, no bookmark); a terminal started there
runs as one the user started.

## How macOS grants access

From Apple's documentation, unless marked observed:

- **A pick in the system's panel needs no prompt.** The panels run outside the app's process and
  grant it the chosen item ("user intent": [NSOpenPanel][nsopenpanel], [WWDC 2019][tsai]);
  browsing them costs none. The grant is recorded in the item's SIP-protected `com.apple.macl`
  attribute ([Apple DTS][dts-intent]); its lifetime is undocumented (reported: until a restart or
  `tccutil reset`). iCloud Drive and File Provider storage ask whatever was picked ([Oakley][oakley]).
- **Anything else in a protected place asks once**, naming Cogpit and the reason `make app` writes
  into `NSDesktopFolderUsageDescription`, `NSDocumentsFolderUsageDescription`,
  `NSDownloadsFolderUsageDescription`, `NSRemovableVolumesUsageDescription` and
  `NSNetworkVolumesUsageDescription` (`TCC_WHAT` in the [`Makefile`](../../Makefile)). The answer
  is kept per code signing identity ([Apple DTS][dts-perms]): once for the release (Developer ID),
  again after every ad hoc `make app` build. It shows in System Settings › Privacy & Security ›
  Files and Folders; `tccutil reset <service> io.guided-traffic.cogpit` clears it.
- **Security-scoped bookmarks** of the picks Cogpit reads again are made while the grant is fresh
  and resolved at the next start, without UI or mounting, before any source is read; their items
  stay accessed for the process's life ([`nativedialog`](../../internal/ui/nativedialog/),
  [`fileaccess.go`](../../cmd/cogpit/fileaccess.go); `TestBookmarkHarness`,
  `TestBookmarkRoundTrip`). Apple documents them for sandboxed apps ([`withSecurityScope`][scope]);
  outside the sandbox their effect is undocumented. **Observed** with a release: with the Downloads
  grant reset, a source picked in Downloads was read at the next start without a prompt, and a log
  saved to the Desktop through the save panel raised none. Not told apart: bookmark or
  `com.apple.macl`. Not tried: across a restart of the Mac.

[nsopenpanel]: https://developer.apple.com/documentation/appkit/nsopenpanel
[tsai]: https://mjtsai.com/blog/2019/12/18/persistent-file-access-via-com-apple-macl-xattr
[dts-intent]: https://developer.apple.com/forums/thread/750962
[dts-perms]: https://developer.apple.com/forums/thread/678819
[oakley]: https://eclecticlight.co/2026/04/21/the-macl-extended-attribute/
[scope]: https://developer.apple.com/documentation/foundation/nsurl/bookmarkcreationoptions/withsecurityscope

## The bookmarks file

`<user config dir>/Cogpit/bookmarks.json` ([`store.go`](../../internal/ui/nativedialog/store.go)),
macOS only, holds per pick its path, use (`kubeconfig`, `folder`, `caFile`), access, bookmark data
and time: no credential, but where kubeconfigs, folders and CA files are. 0600 in a 0700 directory,
written whole and atomically by `engine.WritePrivateFile`, never through a symlink; read by
`engine.ReadPrivateJSON` as a regular file of at most 4 MiB (`TestStoreKeep`,
`TestStoreUnreadable`). A bookmark's scope resolves only for the app and user that made it (Apple's
App Sandbox design guide; not verified outside the sandbox): a copied or edited file grants another
program nothing, and an entry someone else wrote is at worst an item Cogpit starts accessing
without reading it.

- **Bounds:** a kubeconfig bookmark goes at the next start once its path is no source (at most
  256); of the folders (terminal, kubectl and program folders together) and of the CA files the
  newest 16 each stay, unresolvable ones included (`TestStoreBounds`, `TestKeptSource`).
- **Loss and revoking:** an unreadable file is replaced by the next pick; removing it removes the
  kept access. A pick's grant may not show in Privacy & Security ([Oakley][oakley]): revoking it
  takes `tccutil reset` and removing `bookmarks.json`.

## What this does not cover

The Local Network privacy prompt for clusters on the local network is not file access.

### H-116: Programs Cogpit starts act with Cogpit's folder access

A terminal's shell and what the user runs in it, a credential plugin and the kubectl Cogpit runs
and offers are Cogpit's child processes, and macOS makes Cogpit their responsible app ([Apple
DTS][dts-perms]). Once Cogpit is allowed a protected folder (a prompt answered, a source picked
there), such a program reads it unasked, and a prompt it raises names Cogpit and Cogpit's reason.
Live for every terminal and credential plugin. A plugin runs only once approved (H-1), a terminal
what the user types, as Terminal.app's shells do under its grants; nothing checks that a program
keeps to the folder the user meant. Documented, not verified on a real Mac.

### H-119: A client certificate file is read while the contexts are listed

At every listing of the contexts (at start, after a kubeconfig changed, when the navigator's
cluster list shows), before any connect, Cogpit opens each `client-certificate` file and keeps the
first certificate's NotBefore and NotAfter, never opening the key
([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists),
`fillClientCerts` in [`clientcert.go`](../../internal/cluster/clientcert.go),
`TestClientCertificateDates`, `TestListingNeverOpensTheKey`). A file in a protected place is read
only after client-go read it to connect, then remembered for the run
(`TestProtectedClientCertificateIsReadAtConnect`). Open:

- **A protected place the list misses** (dormant unless a kubeconfig names a certificate there).
  Skipped: Desktop, Documents, Downloads, `~/Library/Mobile Documents` (iCloud Drive),
  `~/Library/CloudStorage`, `~/Library/Containers`, `~/Library/Group Containers` and `/Volumes`,
  links into them included, each path component checked before it is looked at (`protectedPlace`,
  `TestProtectedPlaces`). A certificate in another protected place (a share mounted outside
  `/Volumes`, a folder a later macOS protects) can prompt at listing, naming Cogpit. The list
  follows Apple's documentation, not verified on a real Mac; whether a metadata look inside a
  protected folder prompts is unknown (the component check avoids one).
- **A hanging mount** (dormant unless a kubeconfig names a file there): a certificate on it holds
  the listing, off the window's goroutine, as a kubeconfig there would.
- **Any file the user can read** (live for every context naming a certificate file): a kubeconfig
  dropped into a sync folder can name any file
  ([trust boundaries › H-24](trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)).
  The listing opens it (a regular file of at most 256 KiB, never a FIFO or device), keeps only a
  CERTIFICATE block's dates, and logs a file that is none at debug level with its path and a fixed
  problem, never its content (`TestUnreadableClientCertificateFile`).
