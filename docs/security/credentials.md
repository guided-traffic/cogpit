# Credentials

Where cluster credentials and Cogpit's own secrets come from, where they live while Cogpit runs,
and where they must never end up. Rules that are built link to their code and test; the others
are the plan. Cluster authentication is client-go's, the code `kubectl` uses
([D-6](../developer/decisions.md#d-6-kubernetes-libraries)), loaded in
[`internal/cluster/cluster.go`](../../internal/cluster/cluster.go). The owner's decision round of
2026-10-14 evening confirms H-33, H-40 and H-66 as they are and decides changes to who sets a
proxy and to what the certificate checks show; each is marked where it applies, beside what is
built.

## Where credentials come from

All cluster credentials come from kubeconfig files the user adds. The kubeconfig format carries
them in these forms ([kubeconfig](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/),
[authentication](https://kubernetes.io/docs/reference/access-authn-authz/authentication/)):

| Form | Lives in | Cogpit's handling |
|---|---|---|
| Client certificate and key (`client-certificate[-data]`, `client-key[-data]`) | the kubeconfig, or a file it names | Read when connecting; held in memory by the TLS stack. Since 2026-10-17 ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists)) the certificate, not the key, also whenever the contexts are listed: of the leaf (the first CERTIFICATE block) Cogpit keeps NotBefore and NotAfter and nothing else, the window and `cluster.contexts` show them; the key (`client-key-data`, `client-key`) is never looked at outside client-go's connect (`TestClientCertificateDates`: a garbage key gives the same dates; `TestListingNeverOpensTheKey`). A file in a place macOS protects is read only after the connect read it ([file access › H-119](file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)). A connection keeps the certificate and key it read at its connect: client-go re-reads only a certificate and a key that are both files (every 5 minutes), never ones in the kubeconfig itself, so a certificate renewed there reaches Cogpit only through a new connection (the owner's F28, 2026-10-17): since then ([D-81](../developer/decisions.md#d-81-reconnect-and-what-log-in-again-does)) Reconnect, Log in again of a context without a credential plugin, and, by itself, a connection whose login the API server rejects once its file holds another, connect anew from the file, with every approval a connect asks for (an unchecked certificate, a token file outside the folder). To tell that the file holds another login, each connection keeps a fingerprint of the context's login as the file held it at the connect: an HMAC-SHA-256 under a key made at random for the process, over the user entry's credentials, its plugin's configuration, the cluster entry it is sent to and the size and modification time of the files they name (stat only, none opened; in a place macOS protects not even that); compared, never logged, never sent (`TestLoginFingerprintNamesNoCredential`). The new connection presents the new certificate to the server the file names; a working connection is not reconnected when its file changes |
| Static bearer token (`token`, `tokenFile`) | the kubeconfig, or a file it names | Read when connecting; held in memory. A `token` renewed in the kubeconfig reaches Cogpit through a new connection as a certificate does (since 2026-10-17, D-81, above); a `tokenFile` client-go re-reads every minute |
| Exec credential plugin (`exec`) | a program the kubeconfig names | Run on connect and on expiry; the returned credential is held in memory only, never written to disk. Since 2026-10-16 ([D-58](../developer/decisions.md#d-58-standby-and-lost-connections)) client-go runs it on a 401 only once after a request that worked (the login guard), Cogpit runs it by itself once after an interruption or a rejected heartbeat (an approved plugin only), and a run that waits longer than 30 s (a browser login) makes the login "expired" until the user logs in again; a run Cogpit stopped waiting for goes on, and the connection is back by itself if it ends with a working login. Since 2026-10-16 night ([D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound), the owner's finding) **Cogpit ends a run**: one that has not returned within 5 minutes, and one the user cancels (Disconnect, the window's Cancel Connecting, of a connect or a connection that waits for it). client-go starts the plugin without a context and offers no handle on the process, so Cogpit notes each run where client-go starts it (the same `interactiveFunc` wrapper) and finds the process among its own child processes as the one that appears then (by its command line, or as the one new child in Cogpit's process group; since 2026-10-19, WF38, followed by its number and start time, so that a wrapper that `exec`s the login program is found too), then sends it SIGTERM, and SIGKILL after 3 s if it still runs ([`loginrun.go`](../../internal/cluster/loginrun.go), [`pluginproc.go`](../../internal/cluster/pluginproc.go); `TestCancelledConnectEndsThePlugin`, `TestLoginTimeoutEndsThePlugin`, `TestCancelEndsAPluginThatExecs`); a process it does not find it only stops waiting for, and says so. Only the plugin's own process, never its process group (Cogpit's own) nor a browser it opened; after that no run of the plugin starts until the user connects or logs in again ([H-118](#h-118-what-a-cancelled-login-leaves-running)). Nothing of it reaches the cluster. The checks of the connection send its credential with `GET /version` every 15 s; the reconnect attempts send none ([H-101](trust-boundaries.md#h-101-a-credential-plugin-runs-again-during-a-connection-without-being-checked-again)). Since 2026-10-16 ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) item 4) a cluster the window does not show, whose setting "Renew the login while the cluster is not shown" is off, **holds its plugin**: client-go's authenticator is kept from starting it (Cogpit wraps the Authenticator's `interactiveFunc`, which client-go asks before it builds the command, [`loginhold.go`](../../internal/cluster/loginhold.go)), so a credential that expires is not renewed and the login lapses until the cluster is shown or the user logs in again; while held, no run happens at all (`TestHeldPluginDoesNotRun`). Two contexts whose plugin runs with the same command, arguments and environment share client-go's one login: it is held only while both are. The plugin's standard output, the credential, goes to a buffer of client-go's own that Cogpit never reads. Its standard error goes to Cogpit's standard error as client-go always sent it, and since 2026-10-16 ([D-66](../developer/decisions.md#d-66-the-connection-log)) a copy of the newest 64 KiB is held in memory: after a connect or a heartbeat that failed because the plugin failed, it is scrubbed, cut to 4 KiB and written to the [connection log](#where-credentials-must-never-end-up); after a request that worked it is dropped ([`pluginstderr.go`](../../internal/cluster/pluginstderr.go), [H-112](#h-112-a-credential-plugins-standard-error-in-the-connection-log)) |
| A proxy's user and password in the cluster's `proxy-url` (`http://user:password@proxy:3128`) | the kubeconfig | Handed to client-go, which sends it to the proxy alone (Basic `Proxy-Authorization` on the CONNECT; in clear for an `http://` proxy); shown nowhere: the plugin approval dialog strips it, and since 2026-10-13 connect errors replace it with `•••` ([`proxyurl.go`](../../internal/cluster/proxyurl.go), `TestKubeconfigProxyPasswordNeverInErrors`) |
| Legacy auth provider `oidc` | the kubeconfig | Refreshed tokens go through a persister Cogpit supplies; see [H-4](#h-4-auth-provider-token-refresh) |
| Legacy auth providers `azure` and `gcp` | the kubeconfig | client-go 0.37.1 rejects `azure` and points to kubelogin; the user replaces them with exec plugins |

Cogpit adds no cluster credential source of its own: no account, no login, no cloud service.

Since 2026-10-13 ([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml))
Cogpit also reads the credentials of Helm chart repositories, from the file the helm CLI keeps
them in:

| Form | Lives in | Cogpit's handling |
|---|---|---|
| A repository's `username` and `password`, or a user and password in its `url` | Helm's `repositories.yaml` (plain text, written by `helm repo add`), resolved as the helm CLI resolves it | Read with the file (every 2 seconds when it changed, at most 1 MiB) into memory only, unexported in `helm.Repository`; sent as basic auth to the repository's own scheme, host and port only, unless the entry says `pass_credentials_all` (below) |
| A client certificate and key (`certFile`, `keyFile`), a CA file (`caFile`) | files the entry names by absolute path | Read at every download, at most 1 MiB each, into that download's TLS configuration; an error names the path, never the content |

Cogpit never writes Helm's file or anything in Helm's folders, and never copies a repository's
credential into its own files.

## Cogpit's own secrets

| Secret | Lives in | Rule |
|---|---|---|
| MCP bearer token | `<user config dir>/Cogpit/mcp-token` (macOS: `~/Library/Application Support/Cogpit/mcp-token`), file 0600 in a 0700 directory, written atomically, never through a symlink, kept across starts and the status bar's switch since 2026-10-15 (until then removed on exit and when switched off); a file that holds no token Cogpit wrote, or that other users may read, is replaced at the next start ([`internal/mcp/token.go`](../../internal/mcp/token.go), `TestTokenFileLifecycle`, `TestTokenKeptAcrossStarts`, `TestTokenFileNotKept`, `TestStartRefusesSymlinks`) | 32 random bytes, new only when the user clicks Regenerate token in the MCP details (the gear in the status bar's MCP item; F6, [D-53](../developer/decisions.md#d-53-the-mcp-details-and-a-token-kept-until-the-user-regenerates-it), `TestRegenerateToken`), until then valid across starts ([agents and MCP › H-98](agents-and-mcp.md#h-98-a-leaked-token-stays-valid-until-the-user-regenerates-it)); shown there masked until the user shows it, copied on request as a Secret's value (kept from clipboard managers on macOS, cleared after 1 minute; `TestMCPDetailsTokenShownAndCopied`), never in a toast or a notification; printed to the terminal that started Cogpit inside the HTTP `claude mcp add` command, at start and after a regenerate, never logged; an app whose bundle carries `cogpit-cli` prints and copies the stdio registration of that `cogpit-cli` instead, which names no token (since 2026-10-14, [D-2](../developer/decisions.md#d-2-agent-interface-scope), `TestMCPControlOffersTheBundledCLI`) (`TestTokenNeverLogged`) ([agents and MCP](agents-and-mcp.md)). Since 2026-10-13 the endpoint's URL is written beside it, `mcp-endpoint`, with the same rules (not a secret: a loopback URL), while the server listens, for the stdio bridge `cogpit-cli mcp`, which reads both before every call and sends the token to that loopback URL only, and nothing while the file is missing (`TestEndpointFileLifecycle`, `TestReadEndpointTakesOnlyWhatCogpitWrites`, `TestBridgeFollowsARestart`); since 2026-10-15 the port is kept beside them, `mcp-port`, with the same rules (not a secret) (`TestPortKeptAcrossStarts`) |
| LLM API key (built-in agent) | `<user config dir>/Cogpit/llm-keys.json` (macOS: `~/Library/Application Support/Cogpit/llm-keys.json`), the connection's id to its key, file 0600 in a 0700 directory, written atomically through the engine's owner-only writer, never through a symlink, read and written anew on every call, on every platform; a file other users may access is made owner-only again before its keys are used, the log naming the file and its mode, never a key ([`agent/keystore.go`](../../internal/agent/keystore.go), `TestKeyFileKeepsTheKey`, `TestKeyFileAccessibleByOthersIsRepaired`, `TestFileKeyStoreRefuses`). Never in a keychain since 2026-10-15 (the owner's decision, [D-10](../developer/decisions.md#d-10-built-in-agent) item 12: every self-built binary made macOS ask for the login password). Until then the macOS login keychain held it (service `io.guided-traffic.cogpit.llm`, label "Cogpit LLM connection"); Cogpit no longer reads that item, not even to move a key out of it, and has no keychain code left (`TestNoKeychainCode`): such a connection shows "no API key" and its turn asks for the key again until the user enters it once more (`TestKeyFromTheKeychainIsAskedForAgain`); the item stays for the user to delete in Keychain Access | never in `llm-connections.json`, which holds whether a key was given (`TestConnectionsKeepTheKeyInTheKeyStore`); never logged; never in an error (a key file that does not decode is named, not quoted), a note, the audit log, the conversations or a request body (`TestTurnErrors`, `TestKeyFileKeepsTheKey`); sent only in the `Authorization` header to the configured endpoint, which no redirect can change ([D-10](../developer/decisions.md#d-10-built-in-agent)), over `https` checked against the system's roots or, since 2026-10-15, a connection's own CA file, which since 2026-10-16 replaces them for that connection (the owner's decision in the audit of 2026-10-16) ([trust boundaries › H-104](trust-boundaries.md#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier)); over plain `http` to another machine the window warns (H-95); removed from the file by Remove Key and with its connection; plain text protected by the file's mode ([H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)) |
| Update public key (Sparkle's EdDSA key) | the app bundle's `Info.plist` (`SUPublicEDKey`), written at build time by [`hack/macos/sparkle.sh`](../../hack/macos/sparkle.sh) from the repository variable `SPARKLE_PUBLIC_ED_KEY`; a build without it has no update key and its updater does not start | not secret; its private counterpart never touches a user's machine ([updates](updates.md)) |

## Where they live inside Cogpit

- **Kubeconfig files stay where they are.** Cogpit stores the paths the user added, not copies
  (built 2026-10-12: `<user config dir>/Cogpit/kubeconfigs.json`, 0600, lists the added files and
  sync folders; Cogpit never writes the files themselves, `TestAddedFilesAreNeverWritten`).
  Since 2026-10-12 Cogpit also reads every regular file of at most 1 MiB in `~/.kube` (a default
  sync folder, dot files and `config` included), each on its own, and the files `$KUBECONFIG` and
  `--kubeconfig` name (D-28: nothing is merged any more); the list holds only the folder's removal,
  the files named for a run are not stored. A credential plugin or an outside token file of any of
  them waits for the user's approval ([H-1](trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders)).
  The MVP switches off client-go's two implicit writes: the migration of a legacy
  `~/.kube/.kubeconfig` into `~/.kube/config`, and the persister that writes refreshed `oidc` tokens
  back into the file ([`cluster.go`](../../internal/cluster/cluster.go), `memoryPersister`).
- **Cogpit does not store kubeconfig text.** Pasting a kubeconfig is not a target
  ([D-21](../developer/decisions.md#d-21-pasting-a-kubeconfig), owner, 2026-10-12): a copy of a
  kubeconfig is a second place for a credential to live, and the files the user adds already
  stay where they are. A build from 2026-10-12 that still had the paste feature wrote the text to
  `<user config dir>/Cogpit/kubeconfigs/pasted-<16 hex>.yaml` (0600 in a 0700 directory) and an
  entry of kind `pasted` to `kubeconfigs.json`. Cogpit no longer lists, reads or deletes them
  (`TestPastedSourcesOfEarlierBuildsAreNotOffered`); the files hold credentials in clear text and
  are the user's to delete or to add as files again. The entry in `kubeconfigs.json` goes with
  the next change of the list of sources.
- **Exec plugin approvals** live in `<user config dir>/Cogpit/exec-approvals.json` (0600): the
  fingerprint of what each allowed plugin runs and where its credential goes (including the content
  digest of a program or CA file inside the kubeconfig's folder, and the path of a token file
  outside it), no credential.
- **Helm repositories allowed over plain http** (2026-10-14, [H-40](#h-40-credentials-over-plain-http))
  live in `<user config dir>/Cogpit/helm-http-approvals.json` (0600): per repository its name, its
  URL as Cogpit shows it (no user, password or query values), a SHA-256 of its name, URL and user
  name, and when; no credential (`TestHelmCredentialsOverHTTPWaitForTheUser`).
- **Token files** (`tokenFile`) of an added or synced kubeconfig are read by client-go when the
  user connects, and only after the user allowed a file outside the kubeconfig's folder
  (since 2026-10-12, D-17, [trust boundaries › H-24](trust-boundaries.md#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)).
- **Helm chart repositories' credentials stay in Helm's file and Cogpit's memory** (built
  2026-10-13, [D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)):
  Cogpit reads the repositories of Helm's own `repositories.yaml` and keeps an entry's user and
  password in a `helm.Repository` whose credentials are unexported; `%v`, `%+v`, `%#v`, slog and
  JSON print its name and redacted URL (`TestReadRepositoryFile`). Agents, the dialog and
  `helm.repo.list` see the name, the URL without user, password or query values, and whether
  credentials, a client certificate, a CA file, `insecure_skip_tls_verify`,
  `pass_credentials_all` or plain http are present, never a credential. Basic auth is added per
  request by a transport that compares the request's scheme, host and port with the repository's,
  so a redirect to another host or port, a subdomain included, and a chart URL the index points
  elsewhere get none, unless the entry says `pass_credentials_all` (helm's own option)
  (`TestCredentialsStayOnTheRepositoryHost`, `TestCredentialsNotSentToAChartElsewhere`). Download
  errors show URLs redacted and are scrubbed of the password and user name as a guard
  (`TestScrubbedErrors`); the list, the install tab, events, toasts and notifications, the audit log
  and the log are checked for them (`TestHelmRepoCredentialsNeverShown`; on kind
  `TestKindHelmInstall`). The indexes Cogpit downloads are written to its own
  `<user cache dir>/Cogpit/helm/indexes/` (0600 in a 0700 folder): an index of a repository behind a
  login is what that login protects, readable to the user's own processes as Helm's cache is. The
  night run's `<user config dir>/Cogpit/helm/repositories.json` (public repositories, no
  credential) is left on disk, unread. Since 2026-10-14 the chart drawer's README is read from the
  chart's archive through the same download, so the same rules apply to it, and kept in
  `<user cache dir>/Cogpit/helm/readmes/` (owner-only) like the indexes; the charts' icons are fetched
  with no credential at all, not the repository's whatever `pass_credentials_all` says, no cookies
  ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them),
  `TestChartIconRefusals`: an icon behind the repository's login gets a 401). What remains is
  [H-29](#h-29-authenticated-helm-repositories) and [H-40](#h-40-credentials-over-plain-http).
- **A proxy's user and password** (built 2026-10-13, G13): a cluster's proxy (Cluster Settings ›
  Proxy) and the proxy of the Helm repositories' and kubectl's downloads (Preferences › Proxy) may each be
  `http://user:password@host:port`, as Lens's fields allow. Cogpit keeps each as typed in one place
  only, `<user config dir>/Cogpit/cluster-settings.json` and `preferences.json` (0600 in a 0700
  directory, replaced atomically), and hands it to the HTTP client that uses it (client-go's
  `rest.Config.Proxy` for the connection, the Helm fetcher's transport for a download), which sends
  it to the proxy alone: as Basic `Proxy-Authorization` on the CONNECT for an http or https
  proxy (in clear on the network for an `http://` proxy, over TLS for an `https://` one), as SOCKS5's
  user and password for a socks5 proxy; never to the API server or the repository. Everywhere else
  the password is `•••` (`engine.RedactProxyURL`): the UI state (so `ui_state` and the window),
  `cluster.settings.get`, `preferences.get` and their setters' results, the audit log's parameters
  (`auditParams`), the connection log ("Through the proxy http://alice:•••@proxy:3128"); a value
  typed in the window with `•••` keeps
  the stored password (`MergeProxyPassword`). No error quotes a proxy value: the checks name the
  problem only, and `cluster.Connect` scrubs the password from any connect error as a guard
  (`TestCheckProxyURL`, `TestClusterProxyKeptPrivately`, `TestClusterProxyCommands`,
  `TestProxyRefusalNeverQuotesThePassword`, `TestConnectThroughTheClusterProxy`). The user sets
  a proxy in the window ([agents and MCP](agents-and-mcp.md)); decided (owner, 2026-10-14 evening,
  A20) and built the same night: an agent may set one as a write the user confirms, whatever the
  allowance of agent writes, its password `•••` in the confirmation, the audit log and every
  result, the same redaction as above; the CLI is still refused (`TestClusterProxyCommands`,
  `TestNonClusterProxyCommands`). The file is plaintext
  ([H-33](#h-33-proxy-passwords-are-plaintext-in-cogpits-settings), confirmed by the owner the same
  evening).
- **Drafts of unsaved editor text** (built 2026-10-13, G9, [D-11](../developer/decisions.md#d-11-crash-stance)):
  the unsaved text of an Edit or Create resource tab is written to
  `<user config dir>/Cogpit/drafts/<32 hex>.json`, 0600 in a 0700 folder, replaced atomically,
  never through a symlink (`writePrivateFile`); with it the tab's context, object, the version the
  text is based on, the time and the writing Cogpit process. It is removed when the text is saved
  again or the tab is closed. **Never a Secret's values** ([D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml)):
  a Secret's Edit tab has no draft, nor has a tab whose text holds a Secret's manifest (a list's
  items and text that does not parse included) or may become one while it is typed (a `kind:` that
  is a beginning of `Secret` or `SecretList`, in any case); a draft whose text turns into one is
  removed, and a draft file found holding one is deleted unread. **Never Helm values**: the Helm
  Upgrade and Install tabs have no drafts ([`drafts.go`](../../internal/engine/drafts.go),
  `TestNoDraftsForSecretsOrHelmValues`, `TestNoDraftsOfSecretTabs`, `TestDraftFilesReadBack`).
  Agents list drafts without their text; a restored tab's text follows `editor.get`'s rules. What
  other kinds' text a user types is kept as typed ([H-38](#h-38-drafts-keep-typed-text-in-plain-text)).
  Since 2026-10-15 (the owner's audit answer, [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer))
  **a Config Map's values edited in the drawer** and not saved are kept the same way: one file per
  Config Map (type `configMapData`) with the edited keys' text, the values they are based on and
  the version, since 2026-10-16 also the keys added with their text and the keys removed (the
  owner's audit decision), removed when the user saves or reverts them
  ([`drafts_drawer.go`](../../internal/engine/drafts_drawer.go), `TestConfigMapDrawerDrafts`). Only
  the window keeps or reads them (`configMap.keepEdits` is GUI-only, `configMap.readData` returns
  them to the window only); agents list such a draft with its keys, never a value. **A Secret's
  values edited in the drawer are never on disk**: the window keeps them in its memory only
  ([D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)).
- **The tabs and the tables' layout** (built 2026-10-13, night build,
  [D-41](../developer/decisions.md#d-41-what-survives-a-restart)): `<user config dir>/Cogpit/workspace.json`,
  0600 in a 0700 directory, replaced atomically through `writePrivateFile`, holds per tab a context
  id, view keys and titles, namespace and API group names, the search text as typed and the sort,
  column ids per view, and since 2026-10-15 the navigator's collapsed source groups (the labels of
  synced folders and added files, A63): names of contexts, kubeconfig files and namespaces, no credential, no
  object's value, no selection and no drawer object
  ([`persist.go`](../../internal/engine/persist.go), `TestWorkspaceRoundTrip`); since 2026-10-16 the
  context ids of the clusters connected and the one shown (`connected`, `shown`,
  [D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) item 8), whatever the
  restore preference says. A context read back from it connects only when the user shows its tab,
  through the same path as a connect from the navigator (its credential plugin still needs the
  approval), and only a context a kubeconfig source still offers keeps its tab. **With Preferences ›
  App › Restore the last session on** (off by default; only the user turns it on, agents and the CLI
  may turn it off, `ErrRestoreSessionGUIOnly`) the start connects the contexts of `connected`
  without a click, the one of `shown` first, the others one after another, through the same connect:
  each one's credential plugin runs if it is approved and unchanged (D-26), one not allowed or
  changed is refused with its reason, a context no source offers any more is not connected
  ([`restore.go`](../../internal/engine/restore.go), `TestRestoreOnConnectsTheLastSession`). So a
  login program, a browser login included, may run at start, one after another, for every cluster
  of the last session: what the user asked for by turning the preference on. Someone who can write
  `workspace.json` can name contexts there, which then connect at the next start: only contexts a
  kubeconfig source offers, only with their approved plugins, and that person holds the user's
  account already (the file is the user's own, 0600).
- **The discovery cache holds no credential** (built 2026-10-16, G3,
  [D-65](../developer/decisions.md#d-65-discovery-cached-on-disk)): `<user cache dir>/Cogpit/discovery/<32 hex digits>.json`,
  0600 in a 0700 directory, written through a temporary file renamed over it, holds a server's URL
  (without user information) and version, when its answer was fetched, a digest of the custom
  resource definitions, and the groups and resources it serves; no object, no token, certificate
  or key. The file's name is a SHA-256 over the server URL, its TLS server name, the kubeconfig
  files, the context's cluster and user entry names and the impersonated user, never over a
  credential (`TestDiscoveryCacheServesTheNextConnection` looks for the token in the name and the
  content). The answer decides which kinds the navigator offers and which resource a manifest's
  kind maps to when it is written; a file is Cogpit's own, so whoever can change it runs as the
  local user, whom [trust boundaries](trust-boundaries.md#roles) trust already.
- **In memory:** credentials are held by the connection that uses them and dropped when it closes.
  Rule for Cogpit's own credential types: they implement `slog.LogValuer` and `fmt.Stringer` as
  `"<redacted>"`, so a stray log statement cannot print them. The first such type is
  `helm.Repository` (2026-10-13), which also implements `fmt.GoStringer` and `json.Marshaler`
  (its name and redacted URL; its user and password in an unexported value that prints as
  `<redacted>`). Cluster credentials stay inside client-go's `rest.Config`, which Cogpit never
  logs, and the MCP token is a plain string that only `cmd/cogpit` prints, inside the
  `claude mcp add` command; the window holds it while the server runs, for the MCP details
  ([`fyneui/mcpdetails.go`](../../internal/ui/fyneui/mcpdetails.go)), and shows or copies it only
  on the user's click. An LLM API key is a plain string too while a turn's requests or a
  connection test run, sent only in their `Authorization` header
  ([H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)).

## The terminal's kubeconfig

A terminal opened for a cluster must make `kubectl` in it talk to that cluster and namespace,
without copying credentials and without a local proxy that serves the user's identity to other
processes. Kubeconfig merging gives this for free: when `KUBECONFIG` lists several files, the
first file that sets a value wins
([merging rules](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/#merging-kubeconfig-files)).
Each terminal therefore gets

```text
KUBECONFIG=<temp file>:<the user's kubeconfig files>
```

where the temp file holds only a `current-context` and one context entry naming the cluster and
user entries that the user's own files define, plus the namespace. The temp file contains no
credential, is owner-only, and is deleted when the terminal closes. Implemented in
[`internal/kubeconfig/terminal.go`](../../internal/kubeconfig/terminal.go) and
[`internal/terminal/local.go`](../../internal/terminal/local.go); `TestStartLocalBindsKubectlToContextAndNamespace`
checks that `kubectl` in the shell sees the context and namespace, that the file is 0600, holds no
token and is gone after close, and that `kubectl config use-context` in the terminal leaves the
user's file byte-identical. A shell profile can still override `KUBECONFIG`
([H-20](#h-20-shell-profiles-can-override-the-terminals-kubeconfig)).

When Cogpit puts a `kubectl` first for the terminal (since 2026-10-13, [D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)),
the terminal also gets a temporary directory of its own (0700, removed with the terminal) holding a
link named `kubectl` and, for zsh, a `.zshenv` that hands over to the user's own: names and a
script of Cogpit's, no credential ([`terminal/kubectl.go`](../../internal/terminal/kubectl.go)).
That `kubectl` reads the same `KUBECONFIG` as any other.

## Where credentials must never end up

| Place | Rule |
|---|---|
| Application log | No tokens, keys, certificates, a credential plugin's output or Secret data, at any log level |
| Connection log | One file per context, `<user config dir>/Cogpit/connection-logs/<first 32 hex digits of the context id's SHA-256>.jsonl`, 0600 in a 0700 directory, never through a symlink, 7 days and 1 MiB per context ([D-66](../developer/decisions.md#d-66-the-connection-log), [`connectlog_files.go`](../../internal/engine/connectlog_files.go)); read back by the Connection Updates dialog and by `cluster.connectLog` for every origin, agents included. Its lines are built from what the configuration and the errors say, never from a credential: a login is named by its method (a client certificate by its subject CN and expiry date, public facts; a token "not shown"; a plugin by its program's name), URLs lose their user information, query and fragment, a proxy's password is `•••`, error texts pass `kubeconfig.SafeError` (`TestConnectTraceRecordsTheSteps`, `TestConnectTraceNamesTheClientCertificate`, `TestConnectTraceErrorPicture`). A credential plugin's standard error is in it only after a failed login, scrubbed (the user entry's token, password, the plugin's environment values and `--flag=value` arguments and the proxy's password replaced; a JWT, a PEM block, a `Bearer` or `Basic` value, a value after a key naming a secret and a run of 32 or more token characters with letters and digits replaced; URL queries dropped) and cut to 4 KiB; its standard output never (`TestPluginStderrOnFailureIsScrubbed`, `TestPluginStderrOfASuccessfulRunIsNotKept`, `TestScrubPluginOutput`; what the scrubber can miss: [H-112](#h-112-a-credential-plugins-standard-error-in-the-connection-log)) |
| Audit log | An agent's parameters are logged (D-2), with Secret values, Helm values and a proxy's password redacted ([`audit.go`](../../internal/engine/audit.go) `auditParams`) |
| Error messages and toasts | Same rule; errors name the context, never the credential. client-go's own errors can echo a kubeconfig's `users` entries; `kubeconfig.SafeError` ([`kubeconfig.go`](../../internal/kubeconfig/kubeconfig.go)) strips them, and connect errors are tested not to contain a known token. client-go's validation also quotes a `proxy-url` it cannot use whole (`invalid 'proxy-url' "http://user:password@…"`, "could not parse: …"): fixed 2026-10-13, `cluster.Connect` replaces the user information of every `proxy-url` of the kubeconfig, as written, as `%q` writes it, and the password alone, with `•••` (`TestKubeconfigProxyPasswordNeverInErrors`, `TestKubeconfigProxySecrets`). A certificate the server presents is checked, not a secret: a failed check names the kubeconfig field to look at (`TestConnectTrustsTheKubeconfigCA`) |
| Drafts of unsaved editor text | Never a Secret's values, never Helm values (2026-10-13, see [above](#where-they-live-inside-cogpit)); the drafts' listing for agents carries no text; a Config Map's drawer values (since 2026-10-15) listed by their keys only |
| Files saved from the window | The Create resource tab's save (2026-10-13) writes the tab's text only to the file the user picks in the system's save panel (on macOS since 2026-10-16, [D-72](../developer/decisions.md#d-72-native-file-panels-and-macos-folder-access); Fyne's dialog elsewhere), created owner-only when new ([file access](file-access.md)), a GUI action no agent can start, and refuses a text that holds a Secret's manifest ([`createtab_save.go`](../../internal/ui/fyneui/createtab_save.go), `TestCreateTabKeepsSecretsOffDisk`) |
| Crash reports | Cogpit has no crash upload. Since 2026-10-12 the Go runtime writes an unhandled panic or fatal error into `<user config dir>/Cogpit/crash.log` (0600) as well as to standard error ([`crash.go`](../../cmd/cogpit/crash.go)); the next start moves it to `crashes/crash-<time>.log`, keeps the newest ten and names it in the window. Its text is the runtime's: the panic's message and the goroutines' stacks (function names, file lines, argument words, no strings' contents); a panic message the code formats from a value could carry it, so the file follows the log rule: local, owner-only, never sent anywhere |
| Helm repositories' credentials | In memory only, sent to the repository's own scheme, host and port unless its entry says `pass_credentials_all` (2026-10-13, [above](#where-they-live-inside-cogpit)); never in a result, error, toast, the audit log or the log |
| Telemetry | Cogpit sends none. Any network call to something other than a cluster, a Helm repository, the configured LLM endpoint, the release channel or the kubectl release host or mirror ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries), a download the user clicked; it sends the version and platform in the URL, no credential) needs a decision entry in [`decisions.md`](../developer/decisions.md). The check of kubectl's signature against sigstore's transparency log is such a call, decided by the owner on 2026-10-14 evening and built 2026-10-15 ([D-50](../developer/decisions.md#d-50-kubectls-signature-check)): during a download the user clicked, one request to `rekor.sigstore.dev` (`POST /api/v1/log/entries/retrieve`) with the release's published SHA-256, signature and certificate, public material and no credential, through the same proxy as the download ([H-35](trust-boundaries.md#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)) |
| The LLM endpoint | Secret values only over a connection the user marked private (decided 2026-10-14 late evening, built in the engine the same night: the private turn reads what the window shows, Secret values, Helm's values, Flux's credential fields and Helm's errors included; an exec plugin's arguments stay withheld from it as from every agent: [H-13](agents-and-mcp.md#h-13-cluster-data-leaves-for-the-llm-endpoint), [H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)) |
| Screenshots taken by automation | Secret values stay masked unless the user revealed them in the GUI |
| Clipboard | Only on an explicit copy action; a copied Secret value is cleared after 60 seconds if the clipboard still holds it ([H-25](#h-25-a-copied-secret-value-reaches-the-clipboard)) |

## Secret objects

Kubernetes Secrets are data the user's identity may read, not Cogpit's own credentials, but they
are handled with the same care: the list holds metadata only, an opened Secret's values are
masked until the user reveals one field, decoded values are never logged, and a Secret is edited or
created from YAML in the GUI only (since 2026-10-12, D-19: the editor shows its values to the user;
agents and the CLI are refused, and no value reaches the audit log, an error or a toast; since
2026-10-13 nor a draft on disk), and since 2026-10-15 its values are edited in the drawer, in the
window only ([below](#secret-values-edited-in-the-drawer)). As built (2026-10-11): when the
drawer closes or moves, the engine zeroes the bytes it kept of the opened Secret; the GET's
response and a revealed value's string wait for the garbage collector, so the wipe is best effort
([privilege footprint › H-8](privilege-footprint.md#h-8-secret-data-arrives-with-the-list)).
A revealed value can be copied from the drawer, by its copy icon or by selecting it (D-25, built
2026-10-12): a masked value has no icon, and the clipboard is emptied 60 seconds after such a copy
if it still holds the value (`shell.copyText`, `TestSecretValueCopyOnlyOnceRevealed`,
`TestSecretClipboardKeepsWhatReplacedIt`). Copying is a GUI behaviour with no command, so no agent
can put a Secret on the clipboard (`TestCopyHasNoCommand`). Since 2026-10-13 a list's cells copy
with the same icon (D-25): the Secrets list shows Name, Namespace, Labels, the names of the keys,
Type and Age, never a value (`TestSecretDetailShowsNoValue`), so a copy from it is a plain one and
carries no value (`TestSecretsListCopiesNoValue`). Since 2026-10-19 a log tab's lines and a toast's
text copy too (by a selection or a copy icon, [D-99](../developer/decisions.md#d-99-selecting-and-copying-a-log-tabs-text-and-a-toasts)),
as plain copies: a Secret's value that an application printed into its log reaches the clipboard
as the user copied it, neither marked concealed nor cleared, as Download writes it into a file;
Cogpit cannot tell such a value from the rest of a line (the filter that replaces the cached
Secrets' values in what agents read, [H-82](agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents),
does not apply to the user's own copy). Since 2026-10-13 the drawer decodes the
certificates a Secret's keys hold into separate fields, for the window and agents alike
([D-30](../developer/decisions.md#d-30-certificates-in-secrets-decoded),
[`secretcerts.go`](../../internal/engine/secretcerts.go)): a certificate is public material; only
certificates are decoded (PEM `CERTIFICATE` blocks, a private key block beside one never handed to
the decoder; DER certificates, a DER private key never handed to the certificate parser), a key
named like a private key (`tls.key`, `*.key`, `*key.pem`, `*key.der`) is never searched for
certificates, and the decoded value is zeroed after parsing as the Data section's are
(`TestParseCertificatesOnlyCertificateBlocks`, `TestDecodeDERCertificates`,
`TestObjectGetDecodesCertificatesForAgents`).

**A private key is parsed in memory, for one check (since 2026-10-13, the owner's answer on
D-30).** The owner asked for the drawer to say whether `tls.key` belongs to `tls.crt`, which
cannot be known without the key: Cogpit decodes the private key of a key that pairs with a
certificate key (`tls.key` with `tls.crt`, and the other unambiguous pairs D-30 lists) from the
cached Secret, at most 64 KiB, parses it (PKCS #1, PKCS #8, SEC 1; RSA, ECDSA, Ed25519, X25519),
compares its public half with the first certificate's public key, and drops it. What keeps it in:
the parsed key lives in one function call ([`model/keymatch.go`](../../internal/model/keymatch.go),
`MatchPrivateKey`) and is never returned, stored or cached, not even for the next rebuild of the
drawer, which parses it again; the outcome is yes, no or a fixed reason (`encrypted`, `unsupported
key type`, `not readable`, `too large`), and no error of a parser leaves the check, so no section,
`object_get` result, error, log line or audit line can quote the key; the decoded value and the PEM
block are zeroed after, an Ed25519 key too, while an RSA or ECDSA key's numbers wait for the
garbage collector (best effort, as for the values: Go cannot wipe them). An encrypted key is not
decrypted (`TestSecretKeyMatchesCertificate`, `TestMatchPrivateKeyReasons`,
`TestObjectGetDecodesCertificatesForAgents`, which searches the result, the audit log, the events
and the log for the key's PEM, base64 and DER).

**Decided (owner, 2026-10-14 evening), two changes to these checks, both built the same night
(A99, R1):**

- **A combined PEM** (a certificate and its private key in one value) gets the key-matches-certificate
  check too ([D-30](../developer/decisions.md#d-30-certificates-in-secrets-decoded),
  `TestSecretCombinedPEMKeyCheck`): the value's key is parsed in memory for the check, under the
  rules of the paragraph above, and only the outcome leaves it; the certificate parser still never
  sees the key block, and finding it reads the blocks' types only, zeroing each block decoded
  (`model.HoldsPrivateKeyBlock`).
- **The chain's anchor** (a new requirement): beside the key match and the chain's order, the
  drawer and agents say whether the chain leads to a public certificate authority or an internal
  (private) one, with the root's name. Built 2026-10-14 night as the lead's design (D-30;
  `TestCertAnchor`, `TestSecretChainAnchor`): a root of
  a public root program that Cogpit carries itself (Mozilla's NSS bundle, independent of the Mac);
  a root this Mac trusts (its trust store, a corporate root included); no known root (internal or
  self-signed). Certificates are
  public material (D-30), so a root's name adds nothing secret about the cluster. What would be new
  for agents, and through the built-in agent for its LLM endpoint
  ([agents and MCP › H-13](agents-and-mcp.md#h-13-cluster-data-leaves-for-the-llm-endpoint)), is
  the second answer: it tells them about this computer's trust settings, not the cluster's.

### Secret values in the Create Secret dialog

Since 2026-10-13 (G9, [D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml))
the Secrets view's "+" opens Create Secret: name, namespace, type, the data as rows of a key and a
value, labels and annotations ([`createdialog_secret.go`](../../internal/ui/fyneui/createdialog_secret.go),
[`commands/secretcreate.go`](../../internal/engine/commands/secretcreate.go)). Where a value goes,
as built:

- **Typed into a masked field.** Each value is a Fyne entry in password mode (one dot per
  character, a line per line), whose eye shows that value only. A masked entry neither copies nor
  cuts; a shown one does, through the clipboard for Secret values the drawer uses (since
  2026-10-14, H-70 closed): marked concealed on macOS, cleared after 60 seconds if the clipboard
  still holds it, a toast that says so; ⌘C, ⌘X and the field's menu take that one path
  ([`createdialog_secret.go`](../../internal/ui/fyneui/createdialog_secret.go) `secretEntry`,
  `TestSecretDialogCopiesAShownValueConcealed`; [H-25](#h-25-a-copied-secret-value-reaches-the-clipboard)
  for what remains of any copy). The widgets,
  and the entry's undo history, hold the text until the dialog is gone and the garbage collector
  takes it, as a revealed value in the drawer does.
- **To the engine, in the GUI's own process.** The dialog dispatches `secret.create` with origin
  `gui`; the user's own commands are not audited, and the command refuses every other origin
  before anything is asked or sent (`engine.ErrSecretCreateGUIOnly`). An agent's refused call is
  audited with the Secret's name, namespace, type, its keys and the annotations' keys, each value
  replaced by `<redacted>`, and only the names of parameters the command does not have
  (`engine.auditSecretCreateParams`, `TestSecretCreateIsGUIOnly`).
- **To the API server, base64-encoded in `data`** (Cogpit's default, not `stringData`: what is sent
  is what the server keeps and returns, as `kubectl create secret` sends it), first in a server-side
  dry run, then the create. The checks before it name keys and positions, never values; an API
  message that quotes a value comes back with it removed, raw, base64 and decoded alike
  (`TestSecretCreateChecksBeforeSending`, `TestSecretCreateErrorsNeverQuoteValues`).
- **Nowhere else.** No draft (a dialog keeps none, and it opens no dock tab whose text would), no
  toast beyond "Created secrets/<namespace>/<name>", no log line.
**Argo CD's repository and cluster Secrets (since 2026-10-13, [D-35](../developer/decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show),
decided by the owner 2026-10-14).** Settings › Repositories and Settings › Clusters of
the Argo CD section read the Secrets Argo CD labels `argocd.argoproj.io/secret-type`, which hold
repository passwords, SSH and TLS keys, GitHub App keys, bearer tokens and whole cluster client
configurations. Of each, Cogpit reads only an allowlist of configuration keys (a repository's
`name`, `url`, `type`, `project`; a cluster's `name`, `server`, `namespaces`, `clusterResources`,
`project`) into objects of its own kind and names the other keys without reading them; user
information in a URL is replaced with `<redacted>`. The window and agents see the same allowlist;
the Secret itself stays a Secret, masked in its own drawer
([`kinds/argocd_settings.go`](../../internal/model/kinds/argocd_settings.go),
`TestArgoSettingsNeverShowCredentials`, `TestArgoSettingsNeverReachAgents`). Where those values
live meanwhile: [H-66](#h-66-argo-cds-credentials-are-in-cogpits-memory-while-its-settings-are-read).

### Secret values edited in the drawer

Since 2026-10-15 (the owner's audit answer, [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer))
a Secret's values are edited in its drawer, as a Config Map's are, in the window only
([`secretdata.go`](../../internal/ui/fyneui/secretdata.go),
[`engine/secretdataedit.go`](../../internal/engine/secretdataedit.go)). Where a value goes, as built:

- **Read whole into an editor, after Show.** Where the user may save the Secret (not the demo
  cluster, `update secrets` allowed, not immutable, no Helm release record), Show runs
  `secret.reveal` with `whole` (GUI-only) and the value goes into a `yamlview` editor below its key;
  elsewhere Show reveals it read-only as before. Hide masks it again; an edited value stays, masked,
  until it is saved or reverted. A Secret that changed (a rotation) masks every unedited value
  revealed at the older version (R-6). ⌘C and ⌘X in the editor go through the clipboard for Secret
  values (concealed, cleared after 60 seconds, [H-25](#h-25-a-copied-secret-value-reaches-the-clipboard);
  yamlview's `OnCopy`, `TestSecretEditorCopiesAsASecret`). Since 2026-10-16 (the owner's choice in
  the audit of 2026-10-16, D-59) the copy icon in the key's line above each editor copies the whole
  value the same way, never through the plain clipboard; an empty value copies nothing, so a click
  does not empty the clipboard (`secData.copyIcon`, `TestSecretEditorCopyIcon`).
- **To the engine, in the GUI's own process.** `secret.reviewData` (a masked diff: the keys that
  change and their sizes, never a value, and the server's dry run) and `secret.saveData` are marked
  GUI-only: `Dispatch` refuses the CLI and every agent before anything is read, asked or sent, and
  `Refuse`, `Run` and the engine refuse again (`ErrSecretDataGUIOnly`); a save refuses a Secret the
  drawer does not show. The window's own calls are not audited, as none of the user's writes are;
  an agent's refused call is, with the Secret, the version and the keys of `data` and `from`, each
  value `<redacted>`, and only the names of other parameters (`auditSecretDataParams`,
  `TestSecretDrawerDataIsGUIOnly`). The review's `from` (the values the edit is based on, for the
  rebase) is compared base64-encoded: nothing is decoded for it.
- **To the API server, base64-encoded in `data`**, in the cached Secret written back whole: a
  server-side dry run, then the update with the version the values are based on as precondition. An
  API message that quotes a value, the old or the new one, comes back with it removed; a conflict
  carries the masked changes only (`TestSecretDrawerDataRefusals`,
  `TestSecretDrawerDataConflictAndRebase`, on kind `TestKindSecretDataSave`). What the update also
  does: [H-103](#h-103-a-secret-saved-from-cogpit-loses-kubectls-last-applied-annotation).
- **Nowhere else.** No draft: edited values the drawer moves away from stay in the window's memory
  until Cogpit quits (`drawer.secretUnsaved`) and come back masked; no toast beyond
  "Secret <namespace>/<name> saved", no status line or review line with a value, no log line
  (`TestSecretDrawerEditAndSave`, `TestSecretDrawerKeepsUnsavedInMemory`). Since 2026-10-17 each
  editor has an undo history (⌘Z, [D-84](../developer/decisions.md#d-84-undo-and-redo-in-the-yaml-editor)),
  which holds every value typed, those deleted again too: in the editor widget's memory only, never
  on disk, in a draft, in `ui_state` or a log, and dropped when the drawer moves to another object
  or closes (`secData.close`); the kept values come back without it
  (`TestSecretDrawerKeepsUnsavedInMemory`).
- **Keys added and removed** (since 2026-10-16, the owner's audit decision,
  [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)). An added key's
  value is typed into the same editor as a revealed one and takes the same ways: the GUI-only
  commands' `add`, base64-encoded in `data` on the update, the window's memory while unsaved (back
  masked), never a draft; the masked diff names the key and its size. A removal sends no value at
  all; its review base (`from`) is the value only when it was shown, else the removal never rebases.
  An agent's refused call is audited with the keys of `add`, each value `<redacted>`, and the keys of
  `remove` (`TestSecretDrawerDataKeysAudit`); the confirmation of a removal names the keys and "The
  values are not shown." (`TestSecretDrawerDataAddsAndRemovesKeys`, `TestSecretDrawerAddsAndRemovesKeys`).

## What this does not cover

### H-66: Argo CD's credentials are in Cogpit's memory while its settings are read

The settings views of the Argo CD section read Argo CD's labelled Secrets through a watch that
holds them in full (D-13's covering scopes: from the Secrets view's watch while that runs, else a
watch of their own with the label selector), so their passwords, private keys, tokens and cluster
configurations are in Cogpit's memory while a settings view, its drawer or an agent's read uses
them and for the cache's warm period after (five minutes), though nothing shows them. RBAC cannot
narrow `list secrets` by label, so the verbs are the Secrets view's. Dropping the other keys'
values in the informer's transform for these scopes would keep them out of memory; it is not built,
because a covering scope serves the same objects to the Secrets view, whose drawer needs the
values.

Confirmed by the owner (2026-10-14 evening): Argo CD's labelled Secrets stay whole in the cache.
The reason given with it: the filter that replaces cached Secret values in what agents read ([H-82](agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents)) is built
from the values the cache holds, so a value dropped from the cache would reach agents unreplaced
where a log or an object prints it. What remains, live while a settings view, its drawer or an
agent's read uses them and for five minutes after: the values are in Cogpit's memory, shown
nowhere.

### H-4: Auth-provider token refresh

client-go's legacy `oidc` auth provider hands refreshed tokens to a persister, which in `kubectl`
writes them back into the kubeconfig file (client-go 0.37.1,
`plugin/pkg/client/auth/oidc`). Cogpit supplies the persister, so it decides: write through
(kubectl-compatible, but writes the user's file) or keep refreshed tokens in memory (no write, but
`kubectl` in a terminal then refreshes on its own). The rule "no implicit kubeconfig writes" in
[conventions](../developer/conventions.md#never-surprise-a-cluster) argues for memory only, and
the MVP's code keeps them in memory; the owner has not decided, so the behaviour may change.

### H-5: Kubeconfig files are plaintext

Credentials embedded in kubeconfig files are plaintext files protected only by file permissions.
Cogpit does not change that and does not move them into the operating system's keychain. Cogpit
does not check or warn about world-readable kubeconfig files.

### H-6: Exec plugins inherit the environment

An exec plugin runs with Cogpit's environment plus the variables the kubeconfig declares. A GUI
app started from the dock or start menu does not see `PATH` entries and variables set in shell
profiles, so plugins that rely on them (cloud CLIs reading `AWS_PROFILE` and similar) can fail.
Lens's technical FAQ tells users that exec-plugin binaries must be on `PATH` or named with an
absolute path ([Lens technical FAQ](https://docs.lenshq.io/faq/technical-faq/)). As a stopgap the
MVP appends the usual tool directories (`/opt/homebrew/bin`, `/usr/local/bin`, `/opt/local/bin`,
`~/.local/bin`, `~/bin`) that exist and are missing from `PATH` at start
(`cluster.EnsureToolPath`); variables such as `AWS_PROFILE` are still missing. Whether Cogpit reads
the login shell's environment, and which variables it then passes to plugins, is not designed yet.

### H-112: A credential plugin's standard error in the connection log

Since 2026-10-16 ([D-66](../developer/decisions.md#d-66-the-connection-log), the owner's "only on
failure, filtered") the standard error of a credential plugin whose login failed is written to the
context's connection log, scrubbed and cut to 4 KiB ([above](#where-credentials-must-never-end-up)).
The scrubber replaces what it knows (the user entry's own values) and what has the shape of a
credential; it cannot know a secret the plugin prints in another shape: a short password, a
one-time code in prose, a token of fewer than 32 characters or of letters only, a value after a
word it does not take for a secret's name. Such a value then stays, live for every context whose
credential plugin fails with it on its standard error:

- in the file, owner-only, for up to 7 days (or until 1 MiB of newer attempts pushes it out);
- in what `cluster.connectLog` returns to agents, external ones and the built-in agent alike,
  and so possibly at the LLM endpoint;
- on the clipboard after the dialog's Copy, as the user chose.

A plugin that prints its secrets on standard error on failure is the case that makes it real; the
plugins' usual messages (the error, a URL to open, a device code's instructions) are what the
filter is for. Since 2026-10-16 night ([D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound))
a run Cogpit ended after its 5-minute bound counts as a failed login: what it wrote is kept the same
way (a browser login's "open this URL" line, its query removed); a run the user cancelled keeps
nothing. A successful plugin's standard error is not kept, and its standard output, the
credential, is never read. The capture also rests on an unexported field of client-go's
Authenticator (`stderr`, client-go v0.37.1): a client-go that renames it leaves the plugin's
standard error on Cogpit's own standard error, as before, and fails the capture's test.

### H-118: What a cancelled login leaves running

Since 2026-10-16 night ([D-73](../developer/decisions.md#d-73-a-login-that-waits-cancel-connecting-and-a-bound))
Cogpit ends a credential plugin's run that the user cancels or that passes its 5-minute bound
([above](#where-credentials-come-from)). client-go gives it no handle on the process, so Cogpit
finds the process itself ([`pluginproc.go`](../../internal/cluster/pluginproc.go)), and what it does
not end stays. Since 2026-10-19 (WF38, the lead's walk W44) the connection log and the error say
"Cogpit ended <program>" only when Cogpit found the process and may signal it; otherwise "Cogpit
stopped waiting for <program>; its process was not found and may run on"
(`LoginEndedError.RunsOn`). Before, they said it was ended while a wrapper's process ran on.

- **Its children run on.** Cogpit signals the plugin's own process only (the plugin shares Cogpit's
  process group, and a browser it starts must never be ended). A helper the plugin started keeps
  running until it ends by itself. If such a child keeps the plugin's output open, client-go's run
  does not end with the plugin, its lock stays taken, and every connection with that plugin
  configuration waits behind it: a new connect of the context waits too, and can be cancelled
  again, with no process left to end. That wait has no bound: the 5 minutes act on a run Cogpit can
  still end, and a connect has no deadline of its own, so it lasts until the stray process ends
  (then the connect goes on: with the credential that run handed out, if it did, else with a run
  of its own, bounded as any) or until the user cancels it. Since
  2026-10-20 its waiting line says so, with no end time: "Waiting behind the login through
  <program>, running since 14:28, which Cogpit stopped waiting for and could not end. It has no end
  time: it lasts until its process ends.", and "Cancel Connecting stops this wait; that login goes
  on" (for agents `waitingForLogin` with `runsOn` and no `until`; [`loginrun.go`](../../internal/cluster/loginrun.go)
  `LoginRun.RunsOn`, `TestAWaitBehindARunCogpitCouldNotEndHasNoEnd`,
  `TestAConnectBehindARunCogpitCouldNotEndHasNoEnd`). Before, a connect that registered behind the
  run said nothing of what it waited for, and a connection that already showed the run kept its
  passed end ("Cogpit ends it at …") after the bound had failed to end it. Since 2026-10-19 the
  requests of such cancelled connects that queued behind the run start no run of their own once it
  ends: client-go's `getCreds` waits for the lock without the request's context,
  and before, each started the plugin anew (a browser tab each) with nobody waiting; now the hold
  admits a run only while an open connection the user did not cancel asks for one
  ([`loginhold.go`](../../internal/cluster/loginhold.go) `admit`, `TestCancelledConnectsStartNoRunAfterAStrayOne`).
  Live where a plugin starts a long-running child; on macOS pinniped and kubelogin open the browser
  through `open`, which hands the URL to the system and leaves no child, so it is dormant there as
  far as the code shows (not tried with a real plugin).
- **Found at the run's begin, not always.** The wrapper of client-go's `interactiveFunc` notes
  Cogpit's children just before client-go starts the plugin; for half a second Cogpit looks at each
  child that is new since, and takes the one seen running the plugin's command line (its arguments
  after its program, or a script's path after its interpreter; a program the kubeconfig names by its
  path only under that path), else the one new child in Cogpit's own process group when no other
  plugin run was under way alongside. From then on the process is the same number with the same
  start time, whatever its command line becomes: a wrapper that replaces itself with the login
  program (`exec`) is found and ended (`TestCancelEndsAPluginThatExecs`), and SIGTERM and SIGKILL go
  to no number the system gave another process meanwhile. A run whose process was not found at its
  begin falls back to a child that is new since and runs the plugin's command line. Not found, and
  so not ended: two runs that begin within half a second of each other (the restore that connects
  several clusters at once) when a wrapper replaced itself before Cogpit saw its command line, or
  when both run the same command line; a process that appears more than half a second after its
  run began and replaces itself; a wrapper that runs the login program as its child (the case
  above). Then the cancel or the bound only stops waiting, and no run of that plugin starts again
  until the user connects or logs in again. Taken for the plugin wrongly, a child of Cogpit's would
  be signalled: only if the plugin's own process did not appear within the half second while exactly
  one other child in Cogpit's process group did. Cogpit starts one other such child, `kubectl version
  --client` ([`kubectl/client.go`](../../internal/kubectl/client.go)), which ends within a second; a
  terminal's shell runs in a session of its own and is never taken, nor counted as a second child in
  the group: a child's group is the one at Cogpit's last look, and the shell leaves Cogpit's group
  right after its fork (fixed 2026-10-20: seen there first, a shell started within the half second
  left a run whose wrapper replaced itself before Cogpit saw its command line without a process,
  `TestSearchCountsTheGroupAChildEndsIn`). Dormant as far as the code
  shows: client-go starts the plugin microseconds after it asked, and a plugin that cannot start
  ends the run at once, before anything is signalled.
- **Windows** finds the plugin by its program's file name among the children that are new since
  the run's begin (another process's command line is not readable without its memory; Windows has
  no `exec` that keeps a process's number), so another new child with the same program would be
  ended in its place; the code compiles for Windows and is not tried there. Systems other than
  macOS, Linux and Windows find no process: Cogpit stops waiting, says so, and the login is not
  used until the user acts.

### H-25: A copied Secret value reaches the clipboard

A Secret's value that the user copies from the drawer, or a value shown in the Create Secret
dialog (since 2026-10-14), goes on the system clipboard, where Cogpit
cannot guard it. It clears the clipboard after 60 seconds if the clipboard still holds the value,
and when the window closes. On macOS (owner's decision, 2026-10-12) the copy is marked with
nspasteboard.org's concealed and transient types, which clipboard managers that honour them
neither show nor record, and is kept to this Mac, out of Universal Clipboard
(`NSPasteboardContentsCurrentHostOnly`; [`clipboard_darwin.m`](../../internal/ui/fyneui/clipboard_darwin.m),
`TestSecretCopyGoesThroughTheConcealedClipboard` for the routing; the AppKit side is checked by
hand, not by a test). What remains: a clipboard manager that ignores those types keeps its own
history of the value; any process of the user can read it while it is there; Cogpit's process
ending within the 60 seconds (a crash, `kill -9`) leaves it on the clipboard; on Linux and Windows
the copy goes through Fyne's clipboard unmarked. Only values the user revealed and copied are
affected; an agent has no way to copy.

### H-29: Authenticated Helm repositories

Built 2026-10-13 ([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)):
Cogpit reads Helm's own `repositories.yaml`, credentials included, keeps them in memory, sends
basic auth only to the repository's own scheme, host and port (unless `pass_credentials_all`),
reads a CA file, client certificate and key at every download, and keeps every credential out of
results, errors, toasts, the audit log and the log ([above](#where-they-live-inside-cogpit)).
Cogpit never writes Helm's file: repositories and their credentials are added with the helm CLI
(owner, 2026-10-13). What remains, per case:

- **Helm's file is plain text.** The password sits in `repositories.yaml` as `helm repo add` wrote
  it, readable to the user's own processes and carried by a backup, as kubeconfig files keep
  their tokens ([H-5](#h-5-kubeconfig-files-are-plaintext)); Cogpit adds no copy but does not
  improve it. Live for every repository added with `--username`/`--password`.
- **In memory for the session.** The user and password are Go strings in `helm.Repository` for as
  long as the entry is in the file, and a client key's bytes for the length of a download (cleared
  after use, best effort: the TLS stack keeps its parsed key until the download's transport is
  dropped). Nothing writes them out; a core dump of the process would hold them.
- **OCI registries.** Credentials of `helm registry login` (Helm's `registry/config.json`) are not
  read; an `oci://` entry is listed as not used (G11).
- **Plain http** sends the credentials readable on the network once the user allowed it for the
  repository (2026-10-14; nothing is sent before): [H-40](#h-40-credentials-over-plain-http).

### H-40: Credentials over plain http

Narrowed 2026-10-14 (owner's answer; [D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)):
a repository in Helm's file whose URL is `http://` and that has a user and password downloads
nothing, neither its index nor a chart nor a README, on an update or in the background, until the
user allowed it once in Cogpit's window: the Charts view (and a Helm Install tab) shows a banner per
such repository, and Allow… says what crosses the network before
`helm.repo.allowHTTPCredentials` records it. The helm package refuses the request by itself
(`helm.ErrCredentialsOverHTTP`, before any connection; its authorization transport never adds the
credentials to a plain http request it was not allowed for), so neither the credentials nor a
request without them leave Cogpit (`TestPlainHTTPRepository`, `TestHTTPBinding`,
`TestHelmCredentialsOverHTTPWaitForTheUser`). The allowance is kept like a credential plugin's,
in `<user config dir>/Cogpit/helm-http-approvals.json` (0600), bound to the repository's name, URL
and user name (a SHA-256, no credential); a changed URL or user name asks again, a changed password
does not. It is the user's only: the command is GUI-only, and an agent or the CLI is refused
whatever it passes, reads why the repository waits in `helm.repo.list` (`httpCredentials`,
`error`) and its refusal is audited (`TestAllowHTTPCredentialsIsTheUsers`). A repository over http
without credentials needs no allowance (only [H-41](trust-boundaries.md#h-41-repositories-whose-content-nothing-authenticates)
applies). What remains, per case:

- **Once allowed, the credentials travel in clear.** Every download of that repository, the
  background refresh while the Charts view or a Helm Install tab is open included, sends the
  basic auth readable to anyone on the network path, a proxy included, as helm sends it; the index
  and charts can be changed on the way (H-41). Live for every repository the user allowed.
- **The allowance does not expire** and binds no password: a rotated password keeps it (the
  owner's binding). A repository removed from Helm's file and added again under the same name, URL
  and user name is allowed again without asking. Confirmed by the owner (2026-10-14 evening): no
  expiry; the allowance stays bound to the name, the URL and the user name. Live for every
  repository the user allowed.
- **The file is the user's own.** A process of the user that writes
  `helm-http-approvals.json` can allow a repository without the window, as it can write
  `exec-approvals.json` or read Helm's file itself
  ([agents and MCP › H-12](agents-and-mcp.md#h-12-every-local-process-of-the-user-can-read-the-token)).
- **Without a configuration directory** the allowances last for the session (in memory).
- **The built-in agent's files** (2026-10-14, [D-10](../developer/decisions.md#d-10-built-in-agent)):
  `<user config dir>/Cogpit/llm-connections.json` (0600 in 0700: each connection's name, kind,
  base URL, model, limits, private mark, whether it sends all tools, its CA file's path (not
  secret, since 2026-10-15), and whether a key was given; a base URL with a user or
  password is refused, so no credential lands there; the keys sit beside it in `llm-keys.json`
  since 2026-10-15, [H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)) and
  `chat-conversations.json`
  (0600: the last 20 conversations per context, every text scrubbed of the cached Secrets' values
  as D-19 keeps them out of drafts; the request bodies sent stay in memory). Both are written
  through the engine's owner-only writer (atomic, symlinks refused). A private connection's turns
  read more than the scrubber knows (Helm's values, Flux's inline credentials, a Secret's value
  under 10 bytes), so a conversation in which such a turn ran is not kept at all (the owner's
  decision of 2026-10-15, built the same day): it lives in memory only and is gone when Cogpit
  quits, and a file of v0.8.0 to v0.10.0 that holds one is rewritten without it at the next start
  ([H-96](agents-and-mcp.md#h-96-a-private-turns-reads-are-kept-where-the-scrubber-does-not-know-them)).

### H-95: An LLM API key over plain http

Since 2026-10-14 (the built-in agent's engine side): a connection whose base URL is `http://` sends
its API key in the clear to whoever is on the path, a LiteLLM or Open WebUI on the local network
for example; and not only the key: the whole conversation, every tool result and every Secret or
other secret the agent reads go over the network unencrypted, so a private connection over plain
http hands the window's Secret values to the path. Cogpit allows it (the lead's decision of
2026-10-14, kept by the owner on 2026-10-15: refusing it would block such a set-up).

**Built 2026-10-15 (the owner's audit answer (6), [D-10](../developer/decisions.md#d-10-built-in-agent)):**
for a base URL that is plain http to another machine (any host but `localhost` and the loopback
addresses, `engine.ChatPlainHTTPRemote`) Preferences › Agent says so in the warning colour under
the base URL while it is typed ("Plain http to another machine: not only the API key but the whole
conversation, and every Secret or other secret the agent reads, travel unencrypted over the
network."), the list marks the connection "plain http", and the connection test's HTTP layer passes
with the same warning ([`agent/conntest.go`](../../internal/agent/conntest.go),
`TestPlainHTTPToAnotherMachineIsWarned`, `TestAgentPreferencesPlainHTTPWarning`,
`TestAgentPreferencesTestWarns`); an http connection on this computer keeps the line "plain http
(the API key travels unencrypted)" for a key. Checked against the owner's LM Studio on another
machine of his network over plain http (`TestLiveEndpoint`, 2026-10-15: the HTTP layer passed with
the warning). What remains, per case:

- **It is still sent.** The warning informs, it does not stop: every turn over such a connection
  sends the key, the conversation and what the agent read in the clear. Live for every
  `http://` connection to another machine; a local LM Studio sends nothing over the network.
- **The chat itself does not repeat it:** the warning is in Preferences › Agent and the test, not
  in the chat column or per turn (the lead's decision, D-10 (6), confirmed by the owner in the
  audit of 2026-10-16); a user who set the connection up
  long ago is not reminded. The message's "Sent … to <host>" names the host only.
- **A name that resolves to this computer** (`*.localhost`, a hosts-file entry) is warned about
  although nothing leaves the computer; the opposite, a name for another machine that the warning
  misses, does not occur: only `localhost` and loopback addresses are exempt.

What would close it: refusing a key or a private connection over `http://` to another machine, or
asking once per connection.

### H-99: LLM API keys are plaintext in Cogpit's settings folder

Since 2026-10-15 (the owner's decision, [D-10](../developer/decisions.md#d-10-built-in-agent) item
12, built the same day): the built-in agent's API keys sit in `llm-keys.json` as the user typed
them, protected by the file's mode only (0600 in a 0700 directory), as kubeconfig files keep their
tokens ([H-5](#h-5-kubeconfig-files-are-plaintext)) and Cogpit's settings the proxy passwords
([H-33](#h-33-proxy-passwords-are-plaintext-in-cogpits-settings)). Until then the macOS login
keychain held them; the owner accepted the trade-off to end the keychain's password prompts
("übertrieben, erzeugt Reibung"). What remains, per case:

- **Any process of the user reads it.** Whatever runs as the user (a script, an editor extension,
  an agent with file access, malware in the user's session) can read the file and every key in it
  without a prompt; the keychain asked before it gave a key to another program. Live for every
  connection with a key, on every platform.
- **A backup of the configuration folder carries it** in plain text, as it carries the proxy
  passwords and the drafts ([H-38](#h-38-drafts-keep-typed-text-in-plain-text)). Live wherever the
  user's backup includes that folder (not checked per backup tool).
- **A key others may have read stays valid.** A key file that other users could access (group or
  other bits set: a copy restored without its mode, a `chmod` by hand) is made owner-only again
  before its keys are used, and the log names the file and its mode and says to replace keys that
  others may have read; Cogpit cannot revoke a key at its provider (the lead's decision, D-10 item
  12: repaired rather than refused, since a key entered again is the same key). Confirmed by the
  owner (2026-10-15): repaired to 0600 and its keys used, with a Warn line in `cogpit.log` and on
  stderr advising to replace the keys, and no notice in the window, chosen over the lead's
  recommendation of the repair plus a bell notification; also weighed: refusing until the keys
  are entered again (the MCP token's rule; friction, and it revokes nothing). What remains: the
  advice reaches only a user who reads the log. Dormant: Cogpit itself always writes 0600; live
  only after a change outside Cogpit, and only on a computer with other users.
- **On Windows** file modes do not express access: the user profile's ACLs protect the file, as
  they protect the MCP token (not checked on Windows; Cogpit does not ship for it yet).
- **The old keychain item** of a version up to 2026-10-15 stays in the login keychain until the
  user deletes it in Keychain Access; Cogpit never reads or removes it (reading it would ask for the
  login password). Confirmed by the owner (2026-10-15): no migration, such a key is entered once
  again and the old item left in the keychain; weighed: migrating once at the first start (one
  prompt per connection, the cgo Security code kept for macOS only), an "Import from Keychain"
  button. Dormant: it holds a key Cogpit no longer uses, as safe as the keychain keeps it.
- **In memory** a key is a Go string while a turn's requests and a connection test run, and in the
  key store's read buffers until they are collected; a core dump of the process would hold it.

What would close it: the file encrypted with a key from the operating system's keychain (a
keychain prompt again whenever the binary's signature changes, which the owner declined), or a
passphrase at every start.

### H-33: Proxy passwords are plaintext in Cogpit's settings

A proxy's password (a cluster's in `cluster-settings.json`, the Helm downloads' in
`preferences.json`) is kept as the user typed it, in a file
protected by its permissions only (0600 in a 0700 directory), as kubeconfig files keep their tokens
([H-5](#h-5-kubeconfig-files-are-plaintext)). Cogpit does not move it into the operating system's
keychain (the LLM keys, there from 2026-10-14, moved into a file of the same kind on 2026-10-15,
[H-99](#h-99-llm-api-keys-are-plaintext-in-cogpits-settings-folder)), and a backup of the
configuration folder carries it.
The password reaches the proxy in clear when the proxy's URL is `http://` (Basic authentication on
the CONNECT, as every HTTP client sends it); Cogpit does not warn about that.

Confirmed by the owner (2026-10-14 evening): proxy passwords stay in the 0600 settings files, and an
`http://` proxy with a password gets no warning. What remains, per case: every proxy with a
password sits in plain text in `cluster-settings.json` or `preferences.json`, readable to the
user's own processes and carried by a backup (live wherever the user typed one); an `http://`
proxy's password crosses the network in clear on every CONNECT (live for each such proxy). Since
an agent may set a proxy (decided the same evening, A20, built the same night,
[above](#where-they-live-inside-cogpit)), the password an agent passes lands in the same file once
the user accepted it.

### H-103: A Secret saved from Cogpit loses kubectl's last-applied annotation

Cogpit's cache drops `kubectl.kubernetes.io/last-applied-configuration` from every Secret, because
it repeats the values in plain text ([privilege footprint › H-8](privilege-footprint.md#h-8-secret-data-arrives-with-the-list)).
A save from the drawer (since 2026-10-15, [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)),
like an apply from a Secret's Edit tab (since 2026-10-12, D-19), writes the cached Secret back whole
with an update, so the annotation is gone from the Secret afterwards (measured on kind:
`TestKindSecretDataSave`). What it means: the next `kubectl apply` of that Secret's manifest finds no
last-applied state, warns, writes it again, and cannot prune a key the manifest dropped meanwhile; a
tool that reads the annotation reads nothing. Nothing of a value leaks by it (it removes a plain
copy of the values); it is a change the user did not make. Live wherever a Secret was applied with
`kubectl apply` and is saved from Cogpit. Closing it needs a write that leaves the annotation alone
(a patch of `data`, or the live object read for the save), which the writer does not offer today.

### H-38: Drafts keep typed text in plain text

A draft of unsaved editor text (2026-10-13) holds what the user typed into an Edit or Create
resource tab of any kind but a Secret, as typed: a password pasted into a ConfigMap, a token in a
Deployment's `env`, a manifest that names a Secret's values under another kind's fields. Since
2026-10-15 a Config Map's values edited in the drawer are kept so too, each edited key's text and
the value it is based on, and since 2026-10-16 an added key's text (live: a value a user keeps a
credential in reaches the file). It sits
in `<user config dir>/Cogpit/drafts/` protected by its permissions only (0600 in a 0700
directory) until the text is saved, the tab closed or the draft discarded, and a crash leaves it
there until the next connect of its context offers it; a backup of the configuration folder
carries it. Before drafts that text lived in memory only. A draft is not encrypted, and the
Secret check reads kinds, not values. Whether drafts are encrypted (a key in the operating
system's keychain), expire, or can be switched off in the Preferences is not decided.

### H-20: Shell profiles can override the terminal's kubeconfig

The terminal starts a login shell, so the user's shell profile runs after Cogpit set `KUBECONFIG`.
A profile that exports `KUBECONFIG` (common with tools that manage several kubeconfig files)
replaces Cogpit's value, and `kubectl` in that terminal then talks to whatever context the profile
selects, not to the cluster whose view opened the terminal. Lens has the same behaviour. Cogpit
does not detect it; a check after the profile ran, or a prompt hook, is not designed.
