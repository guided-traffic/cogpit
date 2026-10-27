# Credentials

Where cluster credentials and Cogpit's own secrets come from, where they live while Cogpit runs,
and where they must never end up. Cluster authentication is client-go's, as `kubectl`'s
([D-6](../developer/decisions.md#d-6-kubernetes-libraries)), loaded in
[`internal/cluster/cluster.go`](../../internal/cluster/cluster.go).

## Where credentials come from

Cluster credentials come only from kubeconfig files the user adds; Cogpit has no account, login or
cloud service of its own.

| Form | Lives in | Cogpit's handling |
|---|---|---|
| Client certificate and key (`client-certificate[-data]`, `client-key[-data]`) | the kubeconfig, or a file it names | Read at connect, held by the TLS stack |
| Bearer token (`token`, `tokenFile`) | the kubeconfig, or a file it names | Read at connect, held in memory; a `tokenFile` outside the kubeconfig's folder only once the user allowed it ([D-17](../developer/decisions.md#d-17-token-files-outside-the-kubeconfigs-folder), [trust boundaries › H-24](trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)) |
| Exec credential plugin (`exec`) | a program the kubeconfig names | Runs only once approved ([trust boundaries › H-1](trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)); its credential stays in memory, never on disk |
| User and password in the cluster's `proxy-url` (`http://user:password@proxy:3128`) | the kubeconfig | client-go sends them to the proxy alone (Basic `Proxy-Authorization` on the CONNECT, in clear for `http://`); the approval dialog strips them; connect errors show the user information, as written and as `%q` quotes it, and the password as `•••` ([`proxyurl.go`](../../internal/cluster/proxyurl.go), `TestKubeconfigProxyPasswordNeverInErrors`, `TestKubeconfigProxySecrets`) |
| Legacy `auth-provider` (`oidc`, `azure`, `gcp`) | the kubeconfig | Not linked: the connect fails ([H-4](#h-4-auth-provider-token-refresh)) |

**Client certificates and renewed logins**
([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists),
[D-81](../developer/decisions.md#d-81-reconnect-and-what-log-in-again-does)):

- Listing the contexts reads a `client-certificate` file's two dates for the window and
  `cluster.contexts`, never the key
  ([file access › H-119](file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)).
- A connection keeps what it read at its connect; client-go re-reads only a certificate and key that
  are both files (every 5 minutes) and a `tokenFile` (every minute). A certificate or token renewed
  in the kubeconfig itself reaches Cogpit through a new connection made from the file, with every
  approval a connect asks for: Reconnect; Log in again when the context runs no credential plugin or
  its login in the file changed; by itself for a context without a plugin whose login the API server
  rejects while the file holds another ([`reconnect.go`](../../internal/engine/reconnect.go)
  `loginByReconnect`, `renewFromFile`; on kind `TestKindReconnectWithARenewedCertificate`). A
  working connection is neither reconnected when its file changes nor ended at notAfter
  ([H-142](#h-142-a-connection-outlives-its-client-certificate-where-a-proxy-checks-it-at-the-handshake-only)).
- "Holds another login" compares a fingerprint taken at the connect: an HMAC-SHA-256 under a random
  per-process key over the user entry's credentials and plugin configuration, the cluster entry, and
  the size and modification time of the files they name (stat only; in a place macOS protects not
  even that); never logged or sent ([`loginfile.go`](../../internal/cluster/loginfile.go),
  `TestLoginFingerprintNamesNoCredential`).

**Credential plugins** ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections),
[D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound),
[D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) item 4):

- client-go runs the plugin at connect and when the credential expires; on a 401 only once after a
  request that worked (the login guard, [`health.go`](../../internal/cluster/health.go)). Cogpit runs
  an approved plugin itself once after an interruption or a rejected heartbeat. A run unanswered
  after 30 s (a browser login) marks the login expired until the user logs in again; a later working
  login brings the connection back ([`connwatch.go`](../../internal/engine/connwatch.go)). The
  heartbeat (`GET /version`, every 15 s) sends the credential; reconnect attempts send none
  ([trust boundaries › H-101](trust-boundaries.md#h-101-a-credential-plugin-runs-again-during-a-connection-without-being-checked-again)).
- Cogpit ends a run after 5 minutes or when the user cancels it (Disconnect, Cancel Connecting): it
  finds the plugin's process among its children where client-go starts it (a wrapper of the
  Authenticator's `interactiveFunc`) and sends that process alone SIGTERM, SIGKILL 3 s later
  (TerminateProcess on Windows), never to Cogpit's process group or a browser the plugin opened
  ([`loginrun.go`](../../internal/cluster/loginrun.go),
  [`pluginproc.go`](../../internal/cluster/pluginproc.go); `TestCancelledConnectEndsThePlugin` and
  `TestLoginTimeoutEndsThePlugin` on macOS, Linux and Windows, `TestCancelEndsAPluginThatExecs` on
  macOS and Linux). No run starts again until the user connects or logs in again; what stays
  running: [H-118](#h-118-what-a-cancelled-login-leaves-running).
- A cluster not shown whose setting "Renew the login while the cluster is not shown" is off holds its
  plugin: the wrapper refuses before client-go builds the command, and an expiring login lapses until
  the cluster is shown or the user logs in again
  ([`loginhold.go`](../../internal/cluster/loginhold.go), `TestHeldPluginDoesNotRun`). Contexts with
  the same plugin command, arguments and environment share one client-go login, held only while all
  of them hold it.
- Standard output, the credential, goes to a client-go buffer Cogpit never reads; standard error to
  Cogpit's, and after a failed connect or heartbeat also to the
  [connection log](#where-credentials-must-never-end-up), scrubbed
  ([`pluginstderr.go`](../../internal/cluster/pluginstderr.go),
  [H-112](#h-112-a-credential-plugins-standard-error-in-the-connection-log)).

**Where plugin programs are found**
([D-120](../developer/decisions.md#d-120-folders-searched-for-credential-plugins),
[`toolpath.go`](../../internal/cluster/toolpath.go)). After every folder already on Cogpit's `PATH`
come the folders of package managers and installers that rely on a shell profile (macOS and Linux,
each while it exists, `cluster.BuiltinToolFolders`: Homebrew, MacPorts, snap, Nix, `~/.local/bin`,
`~/bin`, krew's `${KREW_ROOT:-~/.krew}/bin`, go install's, asdf's and mise's shims, Rancher
Desktop's, Docker Desktop's), then the user's program folders on every platform (Preferences ›
Kubernetes › Program folders, `programFolders` in `preferences.json`). `PATH` is composed again when
they change and at every connect; `cogpit-cli` reads the same folders.

- Appended, never first: no folder shadows a system program, a plugin the user's `PATH` finds, or a
  program an approval names on `PATH`
  ([D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program),
  `TestComposePathAppendsWithoutShadowing`).
- Only the user adds a folder (`preferences.addProgramFolder` is GUI-only,
  `ErrProgramFoldersGUIOnly`); any origin may remove one (`TestProgramFoldersAddRemoveAndKeep`,
  `TestGUIOnlyCommandsRefuseOtherOrigins`).
- A folder every user may write to is refused when added and skipped if it becomes so (not on
  Windows, where ACLs are not read; `TestProgramFolderProblem`).

**Helm chart repositories** take their credentials from the helm CLI's own `repositories.yaml`
([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)), found
as the helm CLI finds it; Cogpit never writes Helm's files nor copies a credential into its own.

| Form | Lives in | Cogpit's handling |
|---|---|---|
| `username` and `password`, or a user and password in the `url` | `repositories.yaml` (plain text) | Read when it changed (looked at every 2 s, at most 1 MiB), held as [below](#where-they-live-inside-cogpit); sent as basic auth only to the repository's own scheme, host and port unless the entry says `pass_credentials_all` |
| `certFile`, `keyFile`, `caFile` | files the entry names by absolute path | Read at every download (at most 1 MiB each) into its TLS configuration; errors name the path, never the content |

## Cogpit's own secrets

| Secret | Lives in | Rule |
|---|---|---|
| MCP bearer token | `<user config dir>/Cogpit/mcp-token` (macOS: `~/Library/Application Support/Cogpit/`) | 32 random bytes, valid across starts until Regenerate token ([D-53](../developer/decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it), [agents and MCP › H-98](agents-and-mcp.md#h-98-a-leaked-token-stays-valid-until-the-user-regenerates-it)) |
| LLM API keys (built-in agent) | `<user config dir>/Cogpit/llm-keys.json`, connection id to key | Plain text under the file's mode ([H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)) |
| Update public key (Sparkle's EdDSA key) | `Info.plist`'s `SUPublicEDKey` ([`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh)); linked into `cogpit.exe` (`make dist-windows`) | Not secret, from the repository variable `SPARKLE_PUBLIC_ED_KEY`; without it no updater; the private key never reaches a user's machine ([updates](updates.md)) |

- **MCP token** ([`internal/mcp/token.go`](../../internal/mcp/token.go)): 0600 in a 0700 directory,
  written atomically, never through a symlink, at the server's first start (it is opt-in,
  [D-49](../developer/decisions.md#d-49-the-external-mcp-servers-switch-honoured-by-cogpit-cli-mcp))
  or on Regenerate token ([agents and MCP](agents-and-mcp.md#the-mcp-endpoint-its-token-and-the-stdio-bridge));
  a file Cogpit did not write, or that others may read, is replaced at the next start
  (`TestTokenFileLifecycle`, `TestTokenKeptAcrossStarts`, `TestTokenFileNotKept`,
  `TestStartRefusesSymlinks`). The MCP details show it masked until Show and copy it as a Secret's
  value (`TestMCPDetailsTokenShownAndCopied`). It is printed to the terminal that started Cogpit
  inside the HTTP `claude mcp add` command, unless the installed app has its own `cogpit-cli`, whose
  stdio registration names no token ([D-2](../developer/decisions.md#d-2-agent-interface-scope),
  `TestMCPControlOffersTheBundledCLI`); never in a toast, a notification or a log
  (`TestTokenNeverLogged`). Beside it, under the same rules and not secret: `mcp-endpoint` while the
  server listens, for the stdio bridge (`TestEndpointFileLifecycle`,
  `TestReadEndpointTakesOnlyWhatCogpitWrites`, `TestBridgeFollowsARestart`), and `mcp-port`, kept
  across starts (`TestPortKeptAcrossStarts`).
- **LLM API keys** ([`agent/keystore.go`](../../internal/agent/keystore.go)): 0600 in a 0700
  directory through the engine's owner-only writer, never through a symlink, read and written anew on
  every call; a file others may access is made owner-only before its keys are used, the log naming
  file and mode, never a key (`TestKeyFileKeepsTheKey`, `TestKeyFileAccessibleByOthersIsRepaired`,
  `TestFileKeyStoreRefuses`). Never in a keychain ([D-10](../developer/decisions.md#d-10-built-in-agent)
  item 12, `TestNoKeychainCode`) or `llm-connections.json`
  (`TestConnectionsKeepTheKeyInTheKeyStore`); never logged, nor in an error (an undecodable key file
  is named, not quoted), a note, the audit log, a conversation or a request body (`TestTurnErrors`).
  Sent only in the `Authorization` header to the configured endpoint, no redirect followed, over
  `https` checked against the system's roots or a connection's own CA file, which replaces them
  ([trust boundaries › H-104](trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier));
  plain `http` to another machine is warned about ([H-95](#h-95-an-llm-api-key-over-plain-http)).
  Removed by Remove key and with its connection. The connection test's record of a failed request
  ([`agent/exchange.go`](../../internal/agent/exchange.go)) writes the header as `Bearer •••`, removes
  the key from other headers and the answer, withholds `Set-Cookie` and `Cookie`, skips a 401's or
  403's body (it may quote part of the key), and stays in Preferences › Agent, out of every command's
  result.

**On Windows** ([D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle))
the modes on this page are neither set nor checked: Cogpit's files sit in `%APPDATA%\Cogpit` and
`%LOCALAPPDATA%\Cogpit`, and the terminal's kubeconfig in `%TEMP%`, whose inherited ACLs admit the
user, SYSTEM and the Administrators. Cogpit does not check those ACLs: a widened profile exposes the
token, the keys and the drafts as a widened mode would
([H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)). A rename or removal blocked by
another process's open handle (another Cogpit, an editor, a virus scanner) is retried for up to a
second ([`privatefile.go`](../../internal/engine/privatefile.go)).

## Where they live inside Cogpit

Files sit in `<user config dir>/Cogpit` unless a path says otherwise, each 0600 in a 0700 directory
and replaced atomically.

- **Kubeconfig files stay where they are.** `kubeconfigs.json` lists the added files' and sync
  folders' paths, never copies; Cogpit never writes the files (`TestAddedFilesAreNeverWritten`).
  `~/.kube` is a default sync folder: each regular file of at most 1 MiB, dot files included, is
  read on its own, as are the files `$KUBECONFIG` and `--kubeconfig` name (nothing is merged,
  [D-28](../developer/decisions.md#d-28-the-default-kubeconfig-as-sources-as-lens-has-it)); the
  list holds `~/.kube` only by its removal and the files named for a run not at all. client-go's
  implicit writes are off: the legacy `~/.kube/.kubeconfig` migration and the auth-provider token
  persister ([`cluster.go`](../../internal/cluster/cluster.go), `memoryPersister`). Pasting a
  kubeconfig is not a target ([D-21](../developer/decisions.md#d-21-pasting-a-kubeconfig)): no
  second copy of a credential.
- **Exec plugin approvals** (`exec-approvals.json`): the fingerprint of what each allowed plugin runs
  and where its credential goes (with the digest of a program or CA file inside the kubeconfig's
  folder and the path of a token file outside it); no credential.
- **Helm repositories' credentials** sit unexported in `helm.Repository`, whose `%v`, `%+v`, `%#v`,
  slog and JSON forms are its name and redacted URL (`TestReadRepositoryFile`); agents, the dialog
  and `helm.repo.list` see only whether credentials, a client certificate, a CA file,
  `insecure_skip_tls_verify`, `pass_credentials_all` or plain http are present. The transport adds
  basic auth only when a request's scheme, host and port are the repository's, so a redirect
  elsewhere (a subdomain included) and a chart URL on another host get none unless
  `pass_credentials_all` (`TestCredentialsStayOnTheRepositoryHost`,
  `TestCredentialsNotSentToAChartElsewhere`). Errors show URLs redacted and are scrubbed of password
  and user name (`TestScrubbedErrors`); the list, the install tab, events, toasts, notifications, the
  audit log and the log are checked (`TestHelmRepoCredentialsNeverShown`, on kind
  `TestKindHelmInstall`). Indexes and chart READMEs, fetched under the same rules, are cached in
  `<user cache dir>/Cogpit/helm/indexes/` and `helm/readmes/`, as readable to the user's processes
  as Helm's own cache. Chart icons go without any credential or cookie
  ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them),
  `TestChartIconRefusals`). Open: [H-29](#h-29-authenticated-helm-repositories).
- **Helm repositories allowed over plain http** (`helm-http-approvals.json`,
  [H-40](#h-40-credentials-over-plain-http)): per repository its name, its URL as shown (no user,
  password or query values), a SHA-256 of name, URL and user name, and when; no credential.
- **Proxy passwords.** A cluster's proxy (Cluster Settings › Proxy) and the downloads' proxy
  (Preferences › Proxy) may carry `user:password`, kept as typed only in `cluster-settings.json` or
  `preferences.json` and handed to the client that uses it (`rest.Config.Proxy`, the Helm fetcher's
  transport), which sends it to the proxy alone: Basic `Proxy-Authorization` on the CONNECT (in
  clear for `http://`), SOCKS5's user and password for `socks5://`. Everywhere else the password is
  `•••` (`engine.RedactProxyURL`): the UI state (`ui_state`, the window), `cluster.settings.get`,
  `preferences.get` and the setters' results, the audit log (`auditParams`), the connection log; a
  value typed with `•••` keeps the stored password (`MergeProxyPassword`). No error quotes a proxy
  value (`TestCheckProxyURL`, `TestClusterProxyKeptPrivately`,
  `TestProxyRefusalNeverQuotesThePassword`, `TestConnectThroughTheClusterProxy`). Who may set one:
  [agents and MCP](agents-and-mcp.md#only-the-users-own-action). Plain text:
  [H-33](#h-33-proxy-passwords-are-plaintext-in-cogpits-settings).
- **Drafts** ([D-11](../developer/decisions.md#d-11-crash-stance)): an Edit or Create tab's unsaved
  text goes to `drafts/<32 hex>.json` (never through a symlink, `writePrivateFile`) with its context,
  object, base version, time and writing process, removed on save or close. Never a Secret's values
  ([D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml)): no draft for a
  Secret's Edit tab or for text that holds, or while typed may become, a Secret's manifest (a `kind:`
  beginning `Secret` or `SecretList` in any case; list items and unparsable text included); such a
  draft is removed, such a file deleted unread. Never Helm values: the Helm Upgrade and Install tabs
  keep none ([`drafts.go`](../../internal/engine/drafts.go), `TestNoDraftsForSecretsOrHelmValues`,
  `TestNoDraftsOfSecretTabs`, `TestDraftFilesReadBack`). A Config Map's unsaved drawer values
  ([D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)) are kept
  likewise (type `configMapData`: edited and added keys' text, base values and version, removed keys;
  [`drafts_drawer.go`](../../internal/engine/drafts_drawer.go), `TestConfigMapDrawerDrafts`), for the
  window only (`configMap.keepEdits` is GUI-only, `configMap.readData` returns them to the window
  only). Agents list drafts without text and see a Config Map draft's keys; a restored tab follows
  `editor.get`'s rules. Other kinds' text is kept as typed
  ([H-38](#h-38-drafts-keep-typed-text-in-plain-text)). A Secret's drawer edits are never on disk
  ([below](#secret-values-edited-in-the-drawer)).
- **Workspace** ([D-41](../developer/decisions.md#d-41-what-survives-a-restart)): `workspace.json`
  holds names and ids only, among them the clusters connected and shown (`connected`, `shown`, D-69
  item 8); no credential, value, selection or drawer object
  ([`persist.go`](../../internal/engine/persist.go), `TestWorkspaceRoundTrip`; contents:
  [usage › Naming conventions](../developer/usage.md#naming-conventions)). Preferences › App ›
  Restore the last session is off by default: Cogpit starts without tabs or connections. Only the
  user turns it on; agents and the CLI only off (`ErrRestoreSessionGUIOnly`). On, the start connects
  `connected`, `shown` first, one after another, as a click would: an approved, unchanged plugin runs
  (D-26), one not allowed or changed is refused, a context no source offers is skipped; another
  restored tab connects when shown ([`restore.go`](../../internal/engine/restore.go),
  `TestRestoreOnConnectsTheLastSession`). So login programs, browser logins included, may run at
  start: what the user turned on. Whoever writes `workspace.json` can only name contexts a source
  offers, with their approved plugins, and holds the user's account already.
- **Discovery cache** ([D-65](../developer/decisions.md#d-65-discovery-cached-on-disk)):
  `<user cache dir>/Cogpit/discovery/<32 hex>.json` holds a server's URL (no user information),
  version and the groups and resources it serves; no object, token, certificate or key. Its name is a
  SHA-256 over an identity (server URL, TLS server name, kubeconfig files, cluster and user entry
  names, impersonated user), never a credential (`TestDiscoveryCacheServesTheNextConnection` searches
  name and content for the token). It decides the kinds offered and how a manifest's kind maps when
  written; whoever can change it runs as the user already
  ([trust boundaries](trust-boundaries.md#roles)).
- **The built-in agent's files** (D-10), through the
  engine's owner-only writer: `llm-connections.json` holds each connection's name, kind, base URL,
  model, limits, private mark, whether it sends all tools, its CA file's path and whether a key was
  given; a base URL with a user or password is refused (`parseBaseURL` in
  [`agent/client.go`](../../internal/agent/client.go)), so no credential lands there.
  `chat-conversations.json` keeps the last 20 conversations per context, scrubbed of the cached
  Secrets' values, and none in which a private turn ran
  ([agents and MCP › H-96](agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)).
- **In memory** credentials live with the connection that uses them and go when it closes; cluster
  credentials stay in client-go's `rest.Config`, which Cogpit never logs. Cogpit's own credential
  types print as `<redacted>` through `String`, `GoString`, `LogValue` and `MarshalJSON`, so a stray
  log statement cannot print them (`helm.Repository`: its name and redacted URL). The MCP token and
  the LLM keys are plain strings: the window holds the token for the MCP details
  ([`fyneui/mcpdetails.go`](../../internal/ui/fyneui/mcpdetails.go)), a key lives while a turn's
  requests or a connection test run.

## The terminal's kubeconfig

A cluster's terminal points `kubectl` at that cluster and namespace without copying credentials or
running a local proxy that would serve the user's identity to other processes. Since kubeconfig
merging takes each value from the first file that sets it
([merging rules](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/#merging-kubeconfig-files)),
each terminal gets

```text
KUBECONFIG=<temp file>:<the user's kubeconfig files>
```

where the temp file holds only a `current-context` and one context entry naming the cluster and user
entries of the user's own files, plus the namespace: no credential, owner-only, deleted at close
([`internal/kubeconfig/terminal.go`](../../internal/kubeconfig/terminal.go),
[`internal/terminal/local.go`](../../internal/terminal/local.go);
`TestStartLocalBindsKubectlToContextAndNamespace` checks context and namespace, mode 0600, no token,
the removal, and that `kubectl config use-context` leaves the user's file byte-identical). A shell
profile can override `KUBECONFIG` ([H-20](#h-20-shell-profiles-can-override-the-terminals-kubeconfig)).

On Windows the list is joined with `;`; the file (`%TEMP%\cogpit-terminal-<random>.kubeconfig`)
inherits `%TEMP%`'s ACL, and its removal is retried for up to 2 s after the shell's job object ended
the programs that hold it ([`localpty_windows.go`](../../internal/terminal/localpty_windows.go),
`TestStartLocalWindowsBindsKubectlToContextAndNamespace`, `TestStartLocalWindowsRemovesHeldFiles`).

When Cogpit puts a `kubectl` first ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)),
the terminal also gets a 0700 directory, removed with it, holding a `kubectl` link and, for zsh and
bash, a start-up file (`.zshenv`, `rc`) that hands over to the user's own; no credential
([`terminal/kubectl.go`](../../internal/terminal/kubectl.go)). On Windows the directory exists only
for a kubectl not named `kubectl.exe` (a hard link or copy named so); otherwise that kubectl's folder
goes first on `PATH`.

## Where credentials must never end up

| Place | Rule |
|---|---|
| Application log | No token, key, certificate, credential plugin output or Secret data, at any level |
| Connection log | `<user config dir>/Cogpit/connection-logs/<first 32 hex digits of the context id's SHA-256>.jsonl`, 0600 in 0700, never through a symlink, 7 days and 1 MiB per context ([D-66](../developer/decisions.md#d-66-the-connection-log), [`connectlog_files.go`](../../internal/engine/connectlog_files.go)); read by Connection Updates and `cluster.connectLog` for every origin, agents included; built from the configuration and the errors, never from a credential (below) |
| Connection Diagnostics ([D-133](../developer/decisions.md#d-133-connection-diagnostics)) | The log ([`diaglog.go`](../../internal/cluster/diaglog.go)): in memory only, at most 500 lines per context for the 8 used last; real values (the server's address, namespaces, API messages), no header or body beyond an error's message; URLs without user information, errors through `kubeconfig.SafeError`, a refused or failed request on Secrets by its status only, never the message a webhook may fill (`TestDiagRoundTripperNotesProblems`); agents read it with `diagnostics.get`. The report (`diagnostics.report`, Report, its copy icon, Save Report…) replaces hosts, addresses, ports, URL servers and file paths ([`diagscrub.go`](../../internal/engine/diagscrub.go), `TestEndpointScrubber`); what stays: [H-145](#h-145-a-diagnostics-report-names-users-namespaces-and-objects) |
| Audit log | Agents' parameters (D-2), Secret values, Helm values and proxy passwords redacted ([agents and MCP › The audit log](agents-and-mcp.md#the-audit-log)) |
| Errors and toasts | Name the context, never the credential. `kubeconfig.SafeError` ([`kubeconfig.go`](../../internal/kubeconfig/kubeconfig.go)) drops the list client-go's duplicate-name error prints (tokens included) and the value go-yaml quotes; connect errors are tested against a known token; a `proxy-url`'s user information: [above](#where-credentials-come-from). A failed check of the server's certificate names the kubeconfig field to look at (`TestConnectTrustsTheKubeconfigCA`) |
| Drafts | No Secret or Helm values; agents see no text, a Config Map draft's keys only ([above](#where-they-live-inside-cogpit)) |
| Saved files | The Create tab's save writes only to the file the user picks ([D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access)), is GUI-only, and refuses a Secret's manifest ([`createtab_save.go`](../../internal/ui/fyneui/createtab_save.go), `TestCreateTabKeepsSecretsOffDisk`); a new file's mode: [file access](file-access.md#what-cogpit-reads-and-writes-of-the-users-files) |
| Crash reports | No upload. The runtime writes a panic or fatal error to `<user config dir>/Cogpit/crash.log` (0600) and to standard error ([`crash.go`](../../cmd/cogpit/crash.go)); the next start moves it to `crashes/crash-<time>.log`, keeps ten and names it in the window. Messages and stacks only (function names, file lines, argument words, no strings' contents), but a message formatted from a value could carry it: local, owner-only, never sent |
| Telemetry | None. A network call beyond a cluster, a Helm repository, the LLM endpoint, the release channel and the kubectl release host or mirror (D-3: a download the user clicked, version and platform in the URL) needs a decision in [`decisions.md`](../developer/decisions.md): the chart icons' hosts (D-46); sigstore's log ([D-50](../developer/decisions.md#d-50-kubectls-signature-check): per kubectl download one `POST /api/v1/log/entries/retrieve` to `rekor.sigstore.dev` with the release's published SHA-256, signature and certificate, through the download's proxy; [trust boundaries › H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)) |
| The LLM endpoint | Secret values only over a connection the user marked private, whose turn reads what the window shows (Helm values, Flux credential fields and Helm errors included); exec plugin arguments never ([agents and MCP › H-13](agents-and-mcp.md#h-13-cluster-data-leaves-for-the-llm-endpoint), [H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)) |
| Screenshots taken by automation | Secret values masked unless the user revealed them in the GUI |
| Clipboard | Only the user's explicit copy of a Secret value, cleared after 60 s ([below](#secret-objects), [H-25](#h-25-a-copied-secret-value-reaches-the-clipboard)) |

**What the connection log names.** A login by its method: a client certificate by subject CN and
expiry (public facts), a token as "not shown", a plugin by its program's name. URLs lose user
information, query and fragment; proxy passwords are `•••`; errors pass `kubeconfig.SafeError`
(`TestConnectTraceRecordsTheSteps`, `TestConnectTraceNamesTheClientCertificate`,
`TestConnectTraceErrorPicture`). A connect's stages
([D-114](../developer/decisions.md#d-114-a-failed-connects-error-page-and-its-stages),
[`connectstages.go`](../../internal/cluster/connectstages.go)), also in `ui_state`'s
`connectFailure` and the error page, name file paths, the API server's and a proxy's host names and
resolved addresses, the address and port dialled, the TLS version and certificate check, a plugin's
program; never a token, key, password or plugin output (`TestConnectStagesOfAFailure`,
`TestConnectStagesBehindAProxy`). They are measured on client-go's own first request
(`net/http/httptrace`), with no lookup or connection of Cogpit's own, and behind a proxy none past
it. A plugin's standard error (its newest 64 KiB, held in memory until the request ends) appears
only after a failed login, scrubbed (the user entry's token and password, the plugin's environment
values and `=` argument values, the proxy password; JWTs, PEM blocks, `Bearer` and `Basic` values,
values after a key naming a secret, runs of 32 or more token characters mixing letters and digits;
URL queries dropped) and cut to 4 KiB; its standard output never
(`TestPluginStderrOnFailureIsScrubbed`, `TestPluginStderrOfASuccessfulRunIsNotKept`,
`TestScrubPluginOutput`; what it misses:
[H-112](#h-112-a-credential-plugins-standard-error-in-the-connection-log)).

## Secret objects

Handled with the care of Cogpit's own credentials
([`engine/secretdata.go`](../../internal/engine/secretdata.go)):

- **Drawer.** Reads the cached Secret (no request) and lists its keys masked, with sizes; a value
  shows only on the user's Show (`secret.reveal`, GUI-only). Decoded values are never logged and are
  zeroed after use; the cached base64 text and a revealed string wait for the garbage collector
  (best effort, [privilege footprint › H-8](privilege-footprint.md#h-8-secret-data-arrives-with-the-list)).
- **YAML.** A Secret is edited or created from YAML in the GUI only (D-19); no value reaches the
  audit log, an error, a toast or a draft. Drawer editing:
  [below](#secret-values-edited-in-the-drawer).
- **Helm revisions** ([D-131](../developer/decisions.md#d-131-a-releases-revisions)): a revision's
  manifest, as objects or as the stored `manifest.yaml`, shows each rendered Secret's values as dots
  and size until Show; agents get them withheld (H-8).
- **Copying** ([D-25](../developer/decisions.md#d-25-copying-values);
  [`copy.go`](../../internal/ui/fyneui/copy.go) `shell.copyText`,
  [`secretcopy.go`](../../internal/ui/fyneui/secretcopy.go)). Only the user's own copy (a copy icon,
  a selection) puts a value on the clipboard, revealed or masked, whole or, by the b64 button,
  base64-encoded (a binary value only so); a masked one is read through the GUI-only `secret.reveal`
  (`whole`, `base64`), entering no widget, the toast naming the key only
  (`TestSecretRevealedValueCopied`, `TestSecretCutRevealedValueCopiedWhole`,
  `TestSecretMaskedValueCopiedWithoutShowing`, `TestSecretMaskedCopyRefusals`,
  `TestSecretB64CopiesAMaskedValueEncoded`, `TestSecretB64OnABinaryValue`,
  `TestSecretB64OfAValueInAnEditor`, `TestSecretValueBase64IsWhole`). Such a copy goes through the
  clipboard for Secret values: cleared after 60 s if the clipboard still holds it (a b64 copy: the
  encoded text) and when the window closes (`TestSecretClipboardKeepsWhatReplacedIt`); on macOS
  marked with nspasteboard.org's concealed and transient types, which clipboard managers that honour
  them neither show nor record, and kept on this Mac, out of Universal Clipboard
  (`NSPasteboardContentsCurrentHostOnly`, [`clipboard_darwin.m`](../../internal/ui/fyneui/clipboard_darwin.m);
  `TestSecretCopyGoesThroughTheConcealedClipboard`; the AppKit call recorded by a harness,
  `TestConcealedClipboardMarkedForThisMacOnly`). Copying has no command (`TestCopyHasNoCommand`); a
  copy or Show writes no audit line; an agent's `secret.reveal`, the "Copy as base64" action
  `object_get` lists included, is refused and audited without a value (`TestRevealIsGUIOnly`). What
  remains: [H-25](#h-25-a-copied-secret-value-reaches-the-clipboard).
- **Lists and logs.** The Secrets list shows Name, Namespace, Labels, the keys' names, Type and Age,
  never a value (`TestSecretDetailShowsNoValue`), so its cells copy none
  (`TestSecretsListCopiesNoValue`). A log tab's lines and a toast's text copy plainly
  ([D-99](../developer/decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)): a
  value an application logged reaches the clipboard unconcealed and uncleared, as Download writes it;
  the agents' value filter
  ([agents and MCP › H-82](agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents))
  does not apply to the user's copy.
- **Certificates** ([D-30](../developer/decisions.md#d-30-certificates-in-secrets-decoded),
  [`secretcerts.go`](../../internal/engine/secretcerts.go)) are decoded into fields for the window and
  agents: public material. Only PEM `CERTIFICATE` blocks and DER certificates reach a parser, never a
  private key; a key named like one (`tls.key`, `*.key`, `*key.pem`, `*key.der`) is never searched;
  the decoded bytes are zeroed (`TestParseCertificatesOnlyCertificateBlocks`,
  `TestDecodeDERCertificates`, `TestObjectGetDecodesCertificatesForAgents`).
- **Key match.** Whether `tls.key` (or another pair D-30 lists, or a combined PEM's key, found by
  block types only, `model.HoldsPrivateKeyBlock`) belongs to the certificate: the key, at most
  64 KiB, is parsed in memory (PKCS #1, PKCS #8, SEC 1; RSA, ECDSA, Ed25519, X25519) within one call
  ([`model/keymatch.go`](../../internal/model/keymatch.go) `MatchPrivateKey`), compared by its public
  half and dropped, never returned, stored or cached. The outcome is yes, no or a fixed reason
  (`encrypted`, `unsupported key type`, `not readable`, `too large`); no parser error escapes, so no
  result, error or log line can quote the key. PEM blocks and Ed25519 keys are zeroed; RSA and ECDSA
  numbers wait for the garbage collector; an encrypted key is not decrypted
  (`TestSecretKeyMatchesCertificate`, `TestMatchPrivateKeyReasons`, `TestSecretCombinedPEMKeyCheck`;
  `TestObjectGetDecodesCertificatesForAgents` searches the result, the audit log, the events and the
  log for the key's PEM, base64 and DER).
- **Chain anchor:** a public certificate authority (a root of Mozilla's NSS program, which Cogpit
  carries), a root this computer's trust store holds (a corporate one included), or none (internal or
  self-signed), with the root's name (`TestCertAnchor`, `TestSecretChainAnchor`). Nothing secret
  about the cluster; the second answer tells agents, and the built-in agent's LLM endpoint
  ([agents and MCP › H-13](agents-and-mcp.md#h-13-cluster-data-leaves-for-the-llm-endpoint)), about
  this computer's trust settings.
- **Argo CD's repository and cluster Secrets**
  ([D-35](../developer/decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show)): the
  Argo CD settings views read only an allowlist (a repository's `name`, `url`, `type`, `project`; a
  cluster's `name`, `server`, `namespaces`, `clusterResources`, `project`) from the Secrets labelled
  `argocd.argoproj.io/secret-type`, name the other keys unread and replace a URL's user information
  with `<redacted>`, for the window and agents alike; the Secret stays masked in its own drawer
  ([`kinds/argocd_settings.go`](../../internal/model/kinds/argocd_settings.go),
  `TestArgoSettingsNeverShowCredentials`, `TestArgoSettingsNeverReachAgents`). In memory meanwhile:
  [H-66](#h-66-argo-cds-credentials-are-in-cogpits-memory-while-its-settings-are-read).

### Secret values in the Create Secret dialog

Name, namespace, type, the data as key and value rows, labels and annotations (D-19;
[`createdialog_secret.go`](../../internal/ui/fyneui/createdialog_secret.go),
[`commands/secretcreate.go`](../../internal/engine/commands/secretcreate.go)):

- **Masked fields** (password entries, an eye per value) neither copy nor cut; a shown one copies
  through the clipboard for Secret values ([above](#secret-objects), H-70), a toast saying so; ⌘C,
  ⌘X and the field's menu alike (`secretEntry`, `TestSecretDialogCopiesAShownValueConcealed`). The
  widgets and the undo history hold the text until the dialog is gone and collected.
- **Engine:** `secret.create` runs for origin `gui` only and refuses every other origin before
  anything is asked or sent (`engine.ErrSecretCreateGUIOnly`). The user's call is not audited; an
  agent's refused one is, with name, namespace, type, labels and the keys of data and annotations,
  each value `<redacted>`, other parameters by name only (`engine.auditSecretCreateParams`,
  `TestSecretCreateIsGUIOnly`).
- **API server:** base64 in `data` (not `stringData`: what is sent is what the server keeps, as
  `kubectl create secret` sends it), a server-side dry run, then the create. The checks name keys and
  positions, never values; an API message loses any value it quotes, raw, base64 or decoded
  (`TestSecretCreateChecksBeforeSending`, `TestSecretCreateErrorsNeverQuoteValues`).
- **Nowhere else:** no draft or dock tab, no toast beyond "Created secrets/<namespace>/<name>", no
  log line.

### Secret values edited in the drawer

In the window only (D-59; [`secretdata.go`](../../internal/ui/fyneui/secretdata.go),
[`engine/secretdataedit.go`](../../internal/engine/secretdataedit.go)):

- **Show** reads the whole value (`secret.reveal` with `whole`) into a `yamlview` editor, editable
  where the user may save (not the demo cluster, `update secrets` allowed, not immutable, no Helm
  release record), read-only elsewhere. Hide masks it; an edit stays, masked, until saved or
  reverted. A Secret that changed masks every unedited value revealed at the older version
  (`TestRevealedValueDroppedWhenSecretChanges`). ⌘C, ⌘X and the key's copy icon copy the whole value
  through the clipboard for Secret values, an empty one nothing (yamlview's `OnCopy`,
  `secData.copyIcon`; `TestSecretEditorCopiesAsASecret`, `TestSecretEditorCopyIcon`); a masked value,
  or an edited one hidden again, copies without a reveal
  (`TestSecretEditorMaskedValueCopiedWithoutShowing`); the b64 button likewise.
- **Engine:** `secret.reviewData` (a masked diff of the keys that change and their sizes, and the
  server's dry run) and `secret.saveData` are GUI-only: `Dispatch` refuses the CLI and every agent
  before anything is read, asked or sent, and `Refuse`, `Run` and the engine check again
  (`ErrSecretDataGUIOnly`); a save refuses a Secret the drawer does not show. An agent's refused call
  is audited with the Secret, the version and the keys of `data` and `from`, each value `<redacted>`,
  other parameters by name only (`auditSecretDataParams`, `TestSecretDrawerDataIsGUIOnly`). `from`,
  the rebase's base, is compared base64-encoded, never decoded.
- **API server:** base64 in `data` of the cached Secret written back whole, a dry run, then the
  update with the base version as precondition; an API message loses any value it quotes, old or
  new, and a conflict carries masked changes only (`TestSecretDrawerDataRefusals`,
  `TestSecretDrawerDataConflictAndRebase`, on kind `TestKindSecretDataSave`). Side effect:
  [H-103](#h-103-a-secret-saved-from-cogpit-loses-kubectls-last-applied-annotation).
- **Nowhere else:** unsaved edits stay in the window's memory until Cogpit quits
  (`drawer.secretUnsaved`) and come back masked, never in a draft; no toast beyond
  "Secret <namespace>/<name> saved", no value in a status line, review line or log
  (`TestSecretDrawerEditAndSave`, `TestSecretDrawerKeepsUnsavedInMemory`). An editor's undo history
  ([D-84](../developer/decisions.md#d-84-undo-and-redo-in-the-yaml-editor)), deleted text included,
  lives in the widget's memory only and goes when the drawer moves or closes (`secData.close`).
- **Keys added and removed:** an added value takes the same ways (the commands' `add`, base64 in
  `data`, memory while unsaved, never a draft; the diff names key and size). A removal sends no
  value; its `from` is the value only when it was shown, else it never rebases. An agent's refused
  call is audited with the keys of `add` (each value `<redacted>`) and of `remove`
  (`TestSecretDrawerDataKeysAudit`); a removal's confirmation lists the keys and that the values are
  not shown (`TestSecretDrawerDataAddsAndRemovesKeys`, `TestSecretDrawerAddsAndRemovesKeys`).

## What this does not cover

### H-145: A diagnostics report names users, namespaces and objects

The report (D-133) replaces endpoints only. A report pasted into a ticket or a chat still shows the
user's name and groups as the API server's refusals name them ("User \"jdoe\" cannot list …"), the
context's name less the server's, namespaces, object and release names, API groups (a company's own,
`billing.example.com`), the Kubernetes, Cogpit and operating system versions, and a host name that
appears only bare (an API server's or a webhook's message could carry one). Live for every report;
it is made only on the user's act, and Report shows the very text the copy icon and Save Report…
put out. Agents get it with `diagnostics_report`, the log with its endpoints with `diagnostics_get`.
Accepted.

### H-66: Argo CD's credentials are in Cogpit's memory while its settings are read

The Argo CD settings views read the labelled Secrets through a watch that holds them whole
([D-13](../developer/decisions.md#d-13-cache)'s covering scopes: the Secrets view's watch while it
runs, else one with the label selector): passwords, keys, tokens and cluster configurations sit in
Cogpit's memory, shown nowhere, while a settings view, its drawer or an agent's read uses them and
for the cache's five-minute warm period after. RBAC cannot narrow `list secrets` by label. Dropping
the other keys' values in the informer's transform would keep them out; not built, since a covering
scope serves the Secrets view's drawer too, and the agents' value filter (H-82) is built from the
values the cache holds. Accepted.

### H-4: Auth-provider token refresh

Neither binary links client-go's `oidc`, `azure` or `gcp` auth provider: a context whose user entry
names an `auth-provider` fails at connect (`no Auth Provider found for name "oidc"`); the persister
Cogpit sets (`memoryPersister`) would keep refreshed tokens in memory only. Live for every
kubeconfig with a legacy provider. Not decided: link client-go's `oidc` provider (refresh in memory;
`kubectl` in a terminal refreshes and writes the file on its own; writing through would break
[no implicit kubeconfig writes](../developer/conventions.md#never-surprise-a-cluster)), or keep
legacy providers unsupported with an error that points to exec plugins.

### H-5: Kubeconfig files are plaintext

Credentials embedded in kubeconfig files are plain text under file permissions only. Cogpit neither
moves them into the operating system's keychain nor checks or warns about files others can read.
Live for every kubeconfig with embedded credentials.

### H-6: Exec plugins inherit the environment

A plugin runs with Cogpit's environment plus the kubeconfig's variables; an app started from the
Dock, a launcher or the Start menu lacks the shell profile's `PATH` and variables. The program
folders ([above](#where-credentials-come-from)) cover `PATH`. What remains, live wherever it applies:

- **Names found anew.** A program named without a folder, or a kubectl plugin, that the user's
  `PATH` lacks is found in a built-in or program folder and bound by its name, so whoever writes
  there chooses what runs. Built-in folders are the user's own or root's, whose writer owns the
  account already (D-26); a program folder a group may write to (a synced kubeconfig folder, a shared
  drive) hands that group the choice: only a folder every user may write to is refused.
- **A kubectl plugin is bound by nothing:** kubectl, run as the credential plugin, looks its plugin
  up on Cogpit's `PATH`; no approval names or hashes it (D-26 binds kubectl only).
- **Terminals** inherit the composed `PATH`; a profile that sets `PATH` anew keeps Cogpit's folders
  behind its own.
- **Profile variables** (`KREW_ROOT`, a profile name a plugin reads) reach neither Cogpit nor its
  plugins; reading the login shell's environment is not designed (D-120 chose the folders).

### H-112: A credential plugin's standard error in the connection log

Kept from a failed login only ([above](#where-credentials-must-never-end-up)); a run Cogpit ended
at its 5-minute bound counts as failed (a browser login's URL line stays, its query removed), a
cancelled or successful run keeps nothing. The scrubber misses a secret of another shape: a short
password, a one-time code in prose, a token of fewer than 32 characters or of letters only, a value
after a word it does not take for a secret's name. Such a value stays in the file (owner-only, up to
7 days or 1 MiB), in what `cluster.connectLog` returns to agents, external and built-in (so possibly
at the LLM endpoint), and on the clipboard after the dialog's Copy. Live for every context whose
plugin prints one on failure. The capture uses client-go's unexported Authenticator field `stderr`:
a rename leaves the output on Cogpit's standard error and fails the capture's test.

### H-118: What a cancelled login leaves running

A run the user cancels, or one past its 5-minute bound, ends only where Cogpit finds its process
among its own children ([above](#where-credentials-come-from), D-73,
[`pluginproc.go`](../../internal/cluster/pluginproc.go)). The error and the connection log say
"Cogpit ended <program>" only when Cogpit found the process and may signal it, otherwise that it
stopped waiting and the process may run on (`LoginEndedError.RunsOn`). What is not ended:

- **The plugin's children.** Cogpit signals the plugin's own process only (it shares Cogpit's
  process group; a browser it opened must not be ended). A child that keeps the plugin's output
  open keeps client-go's run and its lock going: every connection with that plugin configuration
  waits behind it, unbounded (the 5 minutes act on a run Cogpit can still end; a connect's own
  limit does not count a login), until that process ends or the user cancels. Then a connect behind
  it goes on with the credential the run handed out or a bounded run of its own, while a request of
  a cancelled connect, which client-go's `getCreds` kept waiting for the lock without its context,
  starts none ([`loginhold.go`](../../internal/cluster/loginhold.go) `admit`,
  `TestCancelledConnectsStartNoRunAfterAStrayOne`). Where Cogpit did not find the run's process, the
  waiting line says it has no end time
  ([usage](../developer/usage.md#a-login-that-waits-cancel-connecting-and-the-5-minute-bound);
  agents: `waitingForLogin` with `runsOn`, no `until`;
  [`loginrun.go`](../../internal/cluster/loginrun.go) `LoginRun.RunsOn`,
  `TestAWaitBehindARunCogpitCouldNotEndHasNoEnd`,
  `TestAConnectBehindARunCogpitCouldNotEndHasNoEnd`,
  `TestARunPastItsBoundThatCogpitCouldNotEndHasNoEnd`). Where Cogpit ended the plugin but a child of
  it holds the output (a wrapper script running the login program as its child), the error says
  "Cogpit ended <program>", the run goes on, and a connection waiting behind it shows "Cogpit ends
  it at <time>", a time at which nothing happens. Live where a plugin starts a long-running child;
  dormant on macOS for a plugin that opens the browser through `open`, which leaves no child (not
  tried with a real plugin).
- **A process not found.** The `interactiveFunc` wrapper notes Cogpit's children just before
  client-go starts the plugin; for half a second after, Cogpit takes the new child seen running the
  plugin's command line (its arguments after its program, or a script's path after its interpreter;
  a program named by its path only under that path), else the one new child in Cogpit's process
  group when no other plugin run began within half a second of it, and follows it by number and
  start time: a wrapper that `exec`s the login program is ended (`TestCancelEndsAPluginThatExecs`),
  and no signal reaches a number the system gave another process. Without such a process, a cancel
  or the bound takes the one new child that runs the plugin's command line. Not found: two runs that
  begin within half a second (two connects or renewals at once) when a wrapper replaced itself
  before Cogpit saw its command line or both run the same command line; a process that appears after
  the half second and replaces itself.
- **Another child taken for the plugin's**, only if the plugin's own process did not appear within
  the half second while exactly one other child in Cogpit's group did. Cogpit's other children in
  its group: `kubectl version --client` ([`kubectl/client.go`](../../internal/kubectl/client.go)),
  `open` or `xdg-open` for a link or folder the user opens (Fyne's `OpenURL`), and in the Linux
  package apt's queries and an update's `pkexec`
  ([`update/apt_linux.go`](../../internal/update/apt_linux.go)). A terminal's shell runs in a
  session of its own and is never taken: a child counts by its group at Cogpit's last look
  (`TestSearchCountsTheGroupAChildEndsIn`). Dormant: client-go starts the plugin microseconds after
  it asked, and a plugin that cannot start ends the run at once.
- **Windows** (D-111) matches new children by their program's file name only (another process's
  command line is not readable; no `exec` keeps a process's number), so a new child with the same
  program name can be taken for the plugin's (`TestWindowsChildIsFoundAndEnded`); a `.bat` or
  `.cmd` plugin runs as `cmd.exe` and is never found. Other systems find no process: Cogpit stops
  waiting and says so.

### H-25: A copied Secret value reaches the clipboard

A Secret value the user copies ([above](#secret-objects): from the drawer, revealed or masked,
plain or base64; a value shown in the Create Secret dialog;
[D-25](../developer/decisions.md#d-25-copying-values)) sits on the system clipboard, where Cogpit
cannot guard it; no agent can copy. What remains, for every copied value:

- a clipboard manager that ignores the concealed and transient types keeps the value in its own
  history;
- any process of the user can read it while it is there;
- Cogpit's end within the 60 seconds (a crash, `kill -9`) leaves it there;
- on Linux and Windows the copy goes through Fyne's clipboard unmarked;
- a masked value is copied by one click on its icon, shown only by the checkmark and the toast.

### H-29: Authenticated Helm repositories

What remains of the Helm repositories' credentials Cogpit reads and keeps out of every output
([above](#where-they-live-inside-cogpit)):

- **Helm's file is plain text**, as `helm repo add` wrote it: readable to the user's processes and
  carried by a backup, as kubeconfig files keep their tokens
  ([H-5](#h-5-kubeconfig-files-are-plaintext)); Cogpit adds no copy. Live for every repository
  added with `--username`/`--password`.
- **In memory for the session.** The user and password are Go strings in `helm.Repository` while
  the entry is in the file; a client key's bytes last one download and are cleared after use (best
  effort: the TLS stack keeps its parsed key until the download's transport is dropped;
  [`helm/repo.go`](../../internal/helm/repo.go)). A core dump of the process would hold them.
- **OCI registries.** The credentials of `helm registry login` (Helm's `registry/config.json`) are
  not read; an `oci://` entry is listed as not used (G11).
- **Plain http**: [H-40](#h-40-credentials-over-plain-http).

### H-40: Credentials over plain http

A repository whose URL is `http://` and that has a user and password downloads nothing (no index,
chart or README, on an update or in the background) until the user allowed it once in Cogpit's
window (D-29). The Charts view and a Helm Install tab show a row per such repository; its Allow…
says what crosses the network before `helm.repo.allowHTTPCredentials` records it. The helm package
refuses the request before any connection (`helm.ErrCredentialsOverHTTP`), and its auth transport
adds no credentials to a plain http request it was not allowed for, so neither the credentials nor
a request without them leave Cogpit (`TestPlainHTTPRepository`, `TestHTTPBinding`,
`TestHelmCredentialsOverHTTPWaitForTheUser`). The allowance is kept in `helm-http-approvals.json`
([above](#where-they-live-inside-cogpit); for the session only without a configuration directory),
bound to the repository's name, URL and user name: a changed URL or user name asks again
([`engine/helmhttp.go`](../../internal/engine/helmhttp.go)). The command is GUI-only: an agent or
the CLI is refused and audited, and reads why a repository waits in `helm.repo.list`
(`httpCredentials`, `error`; `TestAllowHTTPCredentialsIsTheUsers`). A repository over http without
credentials needs no allowance
([H-41](trust-boundaries.md#h-41-repositories-whose-content-nothing-authenticates) applies).
Accepted. What remains:

- **Once allowed, the credentials travel in clear** on every download of that repository, the
  background refresh while the Charts view or a Helm Install tab is open included: readable on the
  network path, a proxy included, and the index and charts can be changed on the way (H-41). Live
  for every allowed repository.
- **The allowance does not expire and binds no password**: a rotated password keeps it, and a
  repository removed from Helm's file and added again under the same name, URL and user name is
  allowed without asking.
- **The file is the user's own.** A process of the user that writes `helm-http-approvals.json` can
  allow a repository without the window, as it can write `exec-approvals.json` or read Helm's file
  ([agents and MCP › H-12](agents-and-mcp.md#h-12-every-local-process-of-the-user-can-read-the-token)).

### H-95: An LLM API key over plain http

A connection whose base URL is `http://` sends its API key in clear to whoever is on the path, and
with it the whole conversation, every tool result and every Secret the agent reads: a private
connection over plain http hands the window's Secret values to the path. Accepted: refusing it
would block a LiteLLM or Open WebUI on the local network (D-10 item 10). For plain http to another
machine (any host but `localhost` and the loopback addresses, `engine.ChatPlainHTTPRemote`)
Preferences › Agent warns in the warning colour under the base URL while it is typed
(`engine.ChatPlainHTTPWarning`), the list marks the connection "plain http", and the connection
test's HTTP layer passes with the same warning ([`agent/conntest.go`](../../internal/agent/conntest.go),
`TestPlainHTTPToAnotherMachineIsWarned`, `TestAgentPreferencesPlainHTTPWarning`,
`TestAgentPreferencesTestWarns`; against a real endpoint, local only, `TestLiveEndpoint`). Plain
http on this computer adds "(the API key travels unencrypted)" to that layer's line when a key is
set. What remains:

- **It is still sent:** the warning informs, it does not stop. Live for every `http://` connection
  to another machine.
- **The chat does not repeat it**: no warning in the chat column or per turn, so a connection set
  up long ago reminds no one; a message's "Sent … to <host>" names the host only.
- **A name that resolves to this computer** (`*.localhost`, a hosts-file entry) is warned about
  although nothing leaves it; since only `localhost` and loopback addresses are exempt, no name of
  another machine escapes the warning.

What would close it: refusing a key or a private connection over `http://` to another machine, or
asking once per connection.

### H-99: LLM API keys are plaintext in Cogpit's settings folder

The built-in agent's API keys sit in `llm-keys.json` as typed, protected by the file's mode only
([above](#cogpits-own-secrets)), as kubeconfig files keep their tokens
([H-5](#h-5-kubeconfig-files-are-plaintext)) and Cogpit's settings the proxy passwords
([H-33](#h-33-proxy-passwords-are-plaintext-in-cogpits-settings)); the other agent files hold no
key. Accepted: a keychain would ask for the login password whenever the binary's signature changes
(D-10 item 12). What remains:

- **Any process of the user reads it** without a prompt: a script, an editor extension, an agent
  with file access, malware in the user's session. Live for every connection with a key, on every
  platform.
- **A backup of the configuration folder carries it** in plain text, as it carries the proxy
  passwords and the drafts ([H-38](#h-38-drafts-keep-typed-text-in-plain-text)). Live wherever the
  backup includes that folder.
- **A key others may have read stays valid.** A key file with group or other bits set (a copy
  restored without its mode, a `chmod` by hand) is made owner-only again before its keys are used;
  the advice to replace the keys at their providers is a Warn line in `cogpit.log` and on stderr, no
  notice in the window, and Cogpit cannot revoke a key. Dormant: Cogpit itself writes 0600; live
  after a change outside Cogpit on a computer with other users.
- **On Windows** the repair does not happen: the profile's ACLs protect the file, and Cogpit checks
  neither them nor a mode. Live where the profile's ACLs were widened.
- **In memory** a key is a Go string while a turn's requests or a connection test run, and in the
  key store's read buffers until they are collected; a core dump of the process would hold it.

What would close it: the file encrypted with a key from the operating system's keychain (its
prompts again) or a passphrase at every start.

### H-33: Proxy passwords are plaintext in Cogpit's settings

A proxy's password (a cluster's in `cluster-settings.json`, that of the Helm and kubectl downloads
in `preferences.json`) is kept as typed, protected by the file's mode only, as kubeconfig files keep
their tokens ([H-5](#h-5-kubeconfig-files-are-plaintext)); everywhere else it is `•••`
([above](#where-they-live-inside-cogpit)). Cogpit gives no warning for an `http://` proxy with a
password. Accepted. What remains:

- the password sits in plain text in those files, readable to the user's processes and carried by a
  backup; live wherever the user typed one;
- an `http://` proxy's password crosses the network in clear on every CONNECT (Basic
  `Proxy-Authorization`); live for each such proxy;
- a proxy an agent sets lands in the same file with its password once the user accepted it.

### H-103: A Secret saved from Cogpit loses kubectl's last-applied annotation

The cache drops `kubectl.kubernetes.io/last-applied-configuration` from every Secret, because it
repeats the values in plain text ([`cache.go`](../../internal/cache/cache.go) `stripManagedFields`,
[privilege footprint › H-8](privilege-footprint.md#h-8-secret-data-arrives-with-the-list)). A save
from the drawer (D-59) and an apply from a Secret's Edit tab (D-19) write the cached Secret back
whole with an update, so the annotation is gone afterwards (on kind: `TestKindSecretDataSave`). The
next `kubectl apply` of that Secret's manifest finds no last-applied state, warns, writes it again,
and cannot prune a key the manifest dropped meanwhile; a tool that reads the annotation reads
nothing. No value leaks; it is a change the user did not make. Live wherever a Secret applied with
`kubectl apply` is saved from Cogpit. What would close it: a write that leaves the annotation alone
(a patch of `data`, or the live object read for the save); not built.

### H-38: Drafts keep typed text in plain text

A draft holds what the user typed into an Edit or Create resource tab of any kind but a Secret, as
typed (a password pasted into a ConfigMap, a token in a Deployment's `env`, a manifest that names a
Secret's values under another kind's fields), and a Config Map's values edited or added in the
drawer ([above](#where-they-live-inside-cogpit)). It sits unencrypted in
`<user config dir>/Cogpit/drafts/` until the text is saved, the tab closed or the draft discarded;
after a crash, until the next connect of its context offers it; a backup of the configuration
folder carries it. The Secret check reads kinds, not values. Live for every unsaved text that holds
a credential. Not decided: encrypting drafts (a key in the operating system's keychain), an expiry,
or a preference that switches them off.

### H-20: Shell profiles can override the terminal's kubeconfig

The terminal's shell reads the user's profile after Cogpit set `KUBECONFIG`: a login shell on macOS
and Linux ([`localpty_unix.go`](../../internal/terminal/localpty_unix.go)), PowerShell's profile or
cmd's AutoRun on Windows. A profile that exports `KUBECONFIG` (common with tools that manage several
kubeconfig files) replaces Cogpit's value, and `kubectl` in that terminal talks to whatever context
the profile selects, not to the cluster whose view opened the terminal. Cogpit does not detect it.
What would close it: a check after the profile ran, or a prompt hook; not designed.

### H-142: A connection outlives its client certificate where a proxy checks it at the handshake only

Cogpit presents a context's client certificate in its connection's TLS handshake and keeps that
connection while it works (client-go holds one HTTP/2 connection per cluster); it does not end it
at notAfter. Whether requests on it are served after notAfter is the server's decision:

- **kube-apiserver checks the certificate on every request** (`k8s.io/apiserver` v0.37.1,
  `pkg/authentication/request/x509`, `AuthenticateRequest`): after notAfter the next request gets a
  401, within two heartbeats the connection is "Login expired" and writes stop (D-58); a watch
  opened before runs on until client-go's reflector opens it again (after 5 to 10 minutes) and is
  refused then.
- **A proxy that terminates TLS in front of it may check the certificate at the handshake only.**
  Gardener's `IstioTLSTermination` (beta, on by default from Gardener v1.144,
  [feature gates](https://github.com/gardener/gardener/blob/master/docs/deployment/feature_gates.md))
  does: its Istio gateway checks the client certificate when the connection is made, and an Envoy
  Lua filter sets kube-apiserver's authenticating-proxy headers from its subject on every request
  without looking at its validity. An established connection is then served past notAfter, reads
  and writes alike, while a new one fails at its handshake (`remote error: tls: expired
  certificate`): Reconnect, a restart of Cogpit and `kubectl` in a terminal fail while the open
  window goes on. Gardener v1.154.0 and the patch releases v1.153.2, v1.152.2 and v1.151.4 check
  notBefore and notAfter in the filter on every request and end long-running requests such as
  watches at notAfter ([gardener/gardener#16055](https://github.com/gardener/gardener/pull/16055)).

Live for a context that logs in with a client certificate whose API server sits behind such a proxy
(a Gardener landscape on an earlier version), for writes as well as reads: an action the user
confirms after notAfter runs with the expired identity. Dormant against an API server that checks
every request, and for a token or a credential plugin's credential. Meanwhile the navigator and the
welcome page show "expired" (D-75); the connection log warns only at the next connect. Accepted:
Cogpit keeps its connection, and refusing an expired certificate is the server's business (D-75).

### H-143: The stored API key goes to an edited, unsaved base URL

Test Connection and the model list's ↻ in Preferences › Agent run with the form's values: the base
URL as typed, and the key as typed or, when the key field is empty, the connection's stored key
(`agentForm.apiKey`, [`agent/conntest.go`](../../internal/agent/conntest.go) `testKey`,
`Agent.ModelsOf`). So an edited, unsaved base URL (a typo, another server) gets the stored key with
that request. It is the user's own action, no redirect is followed, and the key goes only to a URL
the user typed. Accepted (D-10 item 10). What limits it: leaving a changed base URL or key field
lists the models again by itself, but not while only the stored key would go to a base URL other
than the saved one; then only ↻ or Test Connection send it (`agentForm.modelsAfterEdit`,
`TestAgentPreferencesModelsFromTheForm`). Live for every connection with a stored key whose base URL
is edited in the form.
