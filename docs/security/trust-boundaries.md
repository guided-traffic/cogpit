# Trust boundaries

Who and what Cogpit trusts, and the points where untrusted data crosses into trusted state. The
boundaries below exist in the problem itself; each row names the check the implementation must
have and the package that owns it, with a link to the code and its test where it is built. The
owner's decision round of 2026-10-14 evening changes H-34, H-35, H-36, H-37, H-41 and H-89 and
confirms H-42 as it is; each change is marked beside what was built before it. H-34's, H-36's,
H-37's and H-89's changes are built since 2026-10-14 night (H-37's status bar since 2026-10-15),
H-41's since 2026-10-15 (the same night run), and H-35's since 2026-10-15 (kubectl's signature
with sigstore-go and Rekor, D-50); the owner confirmed the rest of D-47 (navigation only) on
2026-10-15.

## Roles

| Role | Trust | Why |
|---|---|---|
| Local user | trusted | Owns the desktop session, the kubeconfig files and the cluster identities Cogpit uses |
| Kubeconfig file | trusted only as much as its origin | A kubeconfig can name a program to run (`users[].user.exec`). Kubernetes warns: "Only use kubeconfig files from trusted sources. Using a specially-crafted kubeconfig file could result in malicious code execution or file exposure." ([kubeconfig docs](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)) |
| Credential plugin | runs with the user's privileges | It is whatever program the kubeconfig names ([client-go credential plugins](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins)) |
| Kubernetes API server | authenticated, data untrusted | TLS proves which server answers, checked against the kubeconfig's CA (or the system's) and `tls-server-name` by client-go, unless the kubeconfig switches the check off ([H-37](#h-37-a-context-that-skips-the-certificate-check)); it does not make object names, annotations, events or logs safe to render, execute or hand to a model |
| Workload output | untrusted | Logs, exec and attach streams are written by containers, which anyone who can deploy to the cluster controls |
| Alertmanager (2026-10-17, [D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)) | untrusted: the alerting rules' authors write its texts, workloads' metrics fill their labels, and the one Cogpit reads is found by its Service's name ([H-121](privilege-footprint.md#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)) | Reached through the API server's service proxy with the user's identity; its answer is the title bar's pill, the alert board and what agents read |
| A proxy the user names (a cluster's, and the Helm repositories' and kubectl's downloads', 2026-10-13; the chart icons', 2026-10-14) | trusted to forward, not to read | The user picks it in the window; an agent may set one too, as a write the user confirms (decided by the owner, 2026-10-14 evening, A20, built the same night: `TestClusterProxyCommands`, [agents and MCP](agents-and-mcp.md#rules)). Where the server's TLS is verified it sees the server's name and the traffic's timing and size; where it is not (`insecure-skip-tls-verify`) it reads everything, credentials included ([H-34](#h-34-the-plugin-approval-shows-the-kubeconfigs-proxy-not-cogpits)); to a plain-http server client-go sends no kubeconfig credential at all (it attaches them to https servers only, verified 2026-10-13, `TestPlainHTTPServerGetsNoCredential`), so there a proxy reads the traffic but no token. The Helm downloads follow Helm's own file (D-29, 2026-10-13): https with the certificate checked, unless an entry is plain http or says `insecure_skip_tls_verify`, where the proxy reads and can change them, a plain-http entry's credentials included ([H-41](#h-41-repositories-whose-content-nothing-authenticates)); Lens's "Allow untrusted certificate authorities" is not built (a question for the owner) |
| Helm's `repositories.yaml` and the CA, certificate and key files it names (2026-10-13) | trusted as the user's own configuration, like a kubeconfig file the user keeps | The helm CLI writes it (`helm repo add`); Cogpit reads it, never writes it, and takes its changes over within seconds without asking, from whoever wrote them ([H-42](#h-42-a-repository-another-program-writes-into-helms-file-is-used-without-asking)). Its passwords are the user's ([credentials](credentials.md#where-credentials-come-from)) |
| Helm repositories and charts | untrusted | Remote content; rendering a chart runs its templates in Cogpit's process through the Helm SDK |
| Hosts that serve chart icons (2026-10-14) | untrusted, and told what the user looks at | A repository's index names each chart's icon as a URL on any host; Cogpit fetches it when the window shows the chart, while Preferences › Load chart icons is on ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them), [H-87](#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at)) |
| A `cogpit://` link (2026-10-14) | untrusted: any web page, mail, chat or app can open one, whether Cogpit runs or not | macOS hands it to Cogpit's app, which only navigates with it, never writes ([D-47](../developer/decisions.md#d-47-deep-links-cogpit), [H-89](#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)); decided (owner, 2026-10-14 evening) and built 2026-10-14: a link to another cluster than the connected one asks the user first; navigation only, as built, confirmed by the owner on 2026-10-15 |
| External agents over MCP | as trusted as the local user who connected them; decisions are a model's | [agents and MCP](agents-and-mcp.md) |
| Built-in agent and its LLM endpoint | the endpoint's output is untrusted | [agents and MCP](agents-and-mcp.md) |
| Update feed (GitHub Pages) and release host | trusted after signature verification: the feed and the disk image only with the update key's EdDSA signatures, the app inside only with Cogpit's Developer ID (2026-10-14; the key set up 2026-10-15, no signed feed served yet) | [updates](updates.md) |
| Cogpit's APT repository (GitHub Pages, 2026-10-20, [D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) | trusted after apt's signature check: the index only with the repository key the package's sources file carries, every package only by the hash the index names; read by apt, never by Cogpit, which reads apt's state and runs the package's helper through pkexec on the user's click | [updates › Linux](updates.md#linux) |
| kubectl release host (`dl.k8s.io`) or the mirror set in Preferences | untrusted; a binary is trusted after the Kubernetes release's signature on it and that signature's entry in sigstore's transparency log verified | A downloaded `kubectl` runs whenever the user types `kubectl` in a terminal of Cogpit's, with the user's privileges and credentials ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)) |
| sigstore's transparency log (`rekor.sigstore.dev`, 2026-10-15) | untrusted transport; its answer counts only once the log's signatures in it verify against the log key of the trusted root Cogpit embeds | It vouches that a kubectl release's signature was published while its certificate was valid ([D-50](../developer/decisions.md#d-50-kubectls-signature-check), [H-35](#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)) |
| The Kubernetes project's release schedule, `kubernetes/website`'s `schedule.yaml` and `eol.yaml` (2026-10-18, [D-91](../developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)) | data at build time only: the release pipeline fetches it, the app never contacts its host | Upstream's end-of-life dates built into each release, checked and bounded, never code; a changed file shifts the dates the window marks versions by ([updates › H-130](updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)) |

```text
UNTRUSTED                                   BOUNDARY                          TRUSTED STATE
  kubeconfig files (exec, auth-provider) ─► parse + exec policy ────────────► connection settings
  proxy URL the user types (window only) ─► URL check, password ────────────► the connection's proxy
                                             redacted outside the file
  API responses, watch events            ─► schema-tolerant decoding, ───────► cache, view-models
                                             size limits, bounded walks
  log lines, exec/attach output          ─► terminal emulator with ─────────► terminal and log views
                                             filtered escape sequences
  YAML typed by the user                 ─► parse, diff, dry-run ───────────► apply request
  image file picked as a cluster icon    ─► bounded decode, re-encode, ──────► kept icon copy
                                             SVG checked
  Helm's repositories.yaml, CA/cert/key  ─► bounded read, SDK types, ───────► repositories in memory,
                                             never written                      credentials unexported
  Helm repository index and charts       ─► Helm SDK: no plugins, ──────────► release operations
                                             no post-renderers
  chart README (from the archive)        ─► 256 KiB, cleaned, Markdown ─────► drawer, object_get
                                             without images or HTML
  chart icon (any https host)            ─► no credentials, public ─────────► kept icon copy
                                             address, bounded decode
  cogpit:// link (any page or app)       ─► strict grammar, sources' ───────► navigation only:
                                             contexts, no approval asked        five ui.* commands
  MCP requests, LLM tool calls           ─► token, origin, schema, ─────────► dispatched commands
                                             access review, confirmation
  appcast and disk image (Sparkle)       ─► EdDSA over feed and image, ──────► installed app
                                             Developer ID, newer version only
  APT repository (Linux, apt as root)    ─► OpenPGP over the index, ─────────► installed package
                                             by-hash, SHA-256, newer only        (the user's password)
  kubectl release (dl.k8s.io or mirror)  ─► GUI click, https, size bound, ──► first on PATH in
                                             SHA-256, sigstore signature        Cogpit's terminals
  Rekor's entry (rekor.sigstore.dev)     ─► its proofs against the
                                             embedded trusted root (sigstore-go)
```

## Boundaries and required checks

| Boundary | Package | Required checks |
|---|---|---|
| Kubeconfig to connection | `internal/kubeconfig`, `internal/cluster` | Parse without executing; run an exec plugin only when the user connects to that context, with an argument vector; show the command before the first run of a plugin from a newly added file; never follow `exec` from a kubeconfig the user did not add. Built 2026-10-12 for added and synced kubeconfigs ([H-1](#h-1-kubeconfig-files-added-by-sync-folders)): each source file is read on its own, never merged with another (a dropped file cannot point a context at the user's real credentials); a context whose user has an `exec` section, or names a `tokenFile` outside the kubeconfig's folder ([H-24](#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)), connects only after the user allowed exactly that plugin or file in the GUI, checked by `cluster.Connect` before any client exists, from the very `rest.Config` the client would run ([`exec.go`](../../internal/kubeconfig/exec.go), `TestConnectAsksTheExecPolicyBeforeThePluginRuns`, `TestExecPluginRunsOnlyOnceAllowed`, `TestKindExecPluginAfterApproval`); the default kubeconfig (`--kubeconfig`, `$KUBECONFIG`, `~/.kube/config`) keeps kubectl's behaviour. The server's certificate is checked by client-go as the context's cluster entry says (`certificate-authority(-data)`, `tls-server-name`; verified against a test CA, `TestConnectTrustsTheKubeconfigCA`, `TestConnectWithTLSServerName`); a failed check names the kubeconfig field to look at, never the credential; a cluster with `insecure-skip-tls-verify` connects unchecked and is marked ([H-37](#h-37-a-context-that-skips-the-certificate-check)); since 2026-10-14 (the owner's A28) a source's such context connects only after the user allowed it once in the GUI, through the same policy and store as a plugin, bound to its server and how it is reached (`TestInsecureContextAsksOnceBeforeItsFirstConnect`, `TestConnectAsksThePolicyAboutAnUncheckedCertificate`), and while connected the status bar says "TLS not checked" from the engine's `connection.tlsUnchecked` (A29); a `proxy-url`'s user information never appears in a connect error (2026-10-13, [credentials](credentials.md#where-credentials-must-never-end-up)) |
| Proxy setting to connection | `internal/engine`, `internal/cluster` | Built 2026-10-13 ([`engine/proxy.go`](../../internal/engine/proxy.go), [`cluster/proxy.go`](../../internal/cluster/proxy.go)): a proxy is set only by the user in the window (`ErrProxyGUIOnly` for agents and the CLI, who may clear it); it must be an `http`, `https` or `socks5` URL with a host and nothing but an optional port and `user:password@`; it applies to its own context's connection only, at the next connect, through `rest.Config.Proxy`; its password stays in the settings file and is `•••` everywhere else ([credentials](credentials.md#where-they-live-inside-cogpit)). Since 2026-10-14 (owner's A23) a credential plugin's approval shows and binds it (as shown, the password `•••`), so a changed proxy asks for the approval again before the next connect ([H-34](#h-34-the-plugin-approval-shows-the-kubeconfigs-proxy-not-cogpits)). Decided (owner, 2026-10-14 evening, A20): an agent may set a proxy as a write the user confirms, its password `•••` in the confirmation ([agents and MCP](agents-and-mcp.md#rules)) |
| API response to cache | `internal/cache`, `internal/model` | Tolerate unknown and missing fields; cap object and list sizes; bound owner-reference walks. Built in part: the kind profiles read unstructured content and never panic on an unexpected shape (fuzzed per kind, `FuzzPods`, `FuzzNodes` and the others in [`model/kinds`](../../internal/model/kinds/fuzz_test.go)); a related-objects walk follows a fixed chain of kinds per workload, never an object's own references (`controlled` in [`engine/relatedrefs.go`](../../internal/engine/relatedrefs.go)). Not built: a cap on an object's or a list's size |
| Cluster text to UI | `internal/model`, `internal/ui/fyneui` | Object names, labels, annotations and events are text, never markup; strip control characters before display. Not built as one rule: the window draws cluster text with Fyne's text widgets, which render no markup (Markdown only for a chart's README and the built-in agent's answers, images never loaded: [`helmchartdrawer.go`](../../internal/ui/fyneui/helmchartdrawer.go), [`chat_markdown.go`](../../internal/ui/fyneui/chat_markdown.go)), and handles control characters only where a row of this table or another page names it (the terminal and log views, a certificate's texts, the plugin approval dialog, links; a Secret's value that holds one is shown by its size, [`engine/secretdata.go`](../../internal/engine/secretdata.go)), not in a list's cells |
| Container output to terminal | `internal/terminal`, `internal/ui/fyneui/termview` | Drop or confirm OSC 52 (clipboard write), window-title and window-manipulation sequences ([xterm control sequences](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)); cap scrollback; cap line length. The MVP drops every OSC, DCS, APC, PM and SOS string, media copy and window operations, clamps large counts, and strips escapes from log lines, which are capped at 64 KiB per line and 10,000 lines ([`termview/sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go), fuzzed; [`termview/logs.go`](../../internal/ui/fyneui/termview/logs.go)). Since 2026-10-15 a terminal keeps a scrollback, at most 256 MiB per tab, its oldest lines dropped past it ([D-56](../developer/decisions.md#d-56-the-terminals-scrollback), `history.go` in [the widget's copy](../../third_party/fyne-terminal/PATCHES.md#scrollback), `TestScrollback_Limit`); see [H-19](#h-19-the-terminal-widget-trusts-its-input) |
| Editor to cluster | `internal/engine` | Show a diff and a server-side dry-run before apply; reject documents whose kind, name or namespace changed without saying so. Built 2026-10-11 ([`engine/editor.go`](../../internal/engine/editor.go)): the window's Save shows the diff and the API server's dry-run verdict before Apply (`TestEditTabSaveReviewsThenApplies`); `object.apply` sends the update with `dryRun=All` first, then for real, with the resourceVersion the tab started from as precondition ([privilege footprint](privilege-footprint.md#actions-that-write)); a text whose apiVersion, kind, name or namespace differs from the tab's object is refused, never applied as another object (`editedObject`, `TestEditRefusesWhatItMustNot`) |
| Port-forward to local socket | `internal/cluster` | Bind `127.0.0.1` only; show which local port maps to which pod. Built 2026-10-11: `ListenLoopback` binds `tcp4` 127.0.0.1 and refuses any listener that ends up elsewhere ([`portforward.go`](../../internal/cluster/portforward.go), `TestListenLoopbackBindsIPv4LoopbackOnly`; the engine's tests try `::1` and every non-loopback interface address and expect refusal); the Port Forwarding view and a forward's drawer show the address, the pod and its port; a pod's error text is cut to one line of at most 512 bytes without control characters before it reaches the GUI or an agent; the forward's URL names `127.0.0.1`, not `localhost`, which macOS resolves to `::1` first, where another process may listen (`TestForwardURLReachesTheForward`), and the GUI opens only such URLs; a forward whose connection fails for a reason waiting does not cure (404, 403, 401) fails at once and closes its listener, so no port stays bound for a forward that cannot work (D-20, `TestPortForwardFailsAtOncePermanentErrors`) |
| Image file to cluster icon | `internal/engine` | Built 2026-10-13 ([`clustersettings_icon.go`](../../internal/engine/clustersettings_icon.go), `TestCheckIcon`, `TestClusterIconUpload`): only the user's upload in the window reads a file (agents and the CLI are refused, [agents and MCP](agents-and-mcp.md)); at most 1 MiB read; a PNG or JPEG's size is checked from its header before any pixel is decoded (at most 8,192 pixels a side and 16 megapixels: a small file cannot expand into a huge image), then decoded with Go's own decoders and kept as a PNG of at most 256 pixels a side that Cogpit wrote; an SVG (at most 256 KiB) must be one well-formed `<svg>` document of bounded depth and token count, without a DOCTYPE or other directive (no entities), processing instructions, `<script>`, `<foreignObject>` or animation elements, event handler attributes, or references to anything but a fragment of itself; the copy lives in `<user config dir>/Cogpit/cluster-icons/` under its SHA-256, owner-only, and is checked again before the GUI draws it, so a file someone else put there is never decoded as is. Since 2026-10-23 ([D-119](../developer/decisions.md#d-119-the-cluster-icon-designer), [`clustersettings_iconcut.go`](../../internal/engine/clustersettings_iconcut.go), `TestIconSource`, `TestClusterIconCutAgain`) the picked image is also kept as the icon's source, a PNG of at most 1,024 pixels a side that Cogpit wrote (an SVG passes the same check and is drawn into one by the engine, a renderer panic failing that image only), and the icon is the square cut from it; a kept source is read back bounded (a regular file, at most 4 MiB, a PNG of at most 1,024 pixels a side) before it is decoded again |
| Helm's repositories file to repositories | `internal/helm`, `internal/engine` | Built 2026-10-13 ([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml), [`repofile.go`](../../internal/helm/repofile.go)): the path is the helm CLI's, resolved in `cmd/cogpit` only; the file is read when its size, modification time or mode changed and stayed so for 250 ms, at most 1 MiB, a regular file (a link to one followed), parsed with the Helm SDK's `repo.File`; a file that does not parse keeps the last good list and its error names a line, never the content (`TestReadRepositoryFileRefusals`, `TestHelmRepoFileErrors`); an entry is used only with a name Cogpit can list, an http or https URL with a host, absolute CA, certificate and key paths, and once per name (`TestReadRepositoryFile`); the CA, certificate and key files are read at each download, at most 1 MiB, regular files, their errors naming the path only (`TestClientCertificateRepository`); Cogpit writes nothing into the file or Helm's folders (`TestNoCommandWritesHelmsFile`, `TestHelmReposFromHelmsFile`). Not checked: who wrote the file ([H-42](#h-42-a-repository-another-program-writes-into-helms-file-is-used-without-asking)) |
| Chart to release | `internal/helm` | The Helm SDK renders in-process; Helm plugins and post-renderers (which run external programs) are not supported. Built 2026-10-13 ([architecture §8](../developer/architecture.md#8-helm-internalhelm)): repositories are Helm's file's (D-29): https with the certificate checked against the system's roots or the entry's CA file, unless the entry says `insecure_skip_tls_verify`, or plain http as the entry says, either marked wherever its charts show or install since 2026-10-15 ([H-41](#h-41-repositories-whose-content-nothing-authenticates)); an https repository's downloads never go to http, by a redirect or a chart URL of its index (`TestRepositoryRefusesWhatDoesNotFit`, `TestPlainHTTPRepository`); a repository's credentials go to its own scheme, host and port only, unless `pass_credentials_all` (`TestCredentialsStayOnTheRepositoryHost`); the downloads go through the Preferences' proxy, else the environment's; an index (32 MiB) and a chart archive (16 MiB) are bounded and parsed by the SDK (`FuzzLoadIndex`); an archive must match the SHA-256 digest its index lists, when it lists one, and hold the chart and version the index names (`TestRepositoryRefusesWhatDoesNotFit`); a library chart and a chart missing its dependencies are refused; DNS lookups in templates stay off; the install is previewed with the SDK's server-side dry run, which writes nothing (`TestKindHelmInstall`). An upgrade to a version a repository lists (2026-10-13) downloads the chart through the same checks, before anything is recorded, and refuses a chart whose name is not the release's (`TestUpgradeToARepositoryChart`, `TestHelmUpgradeRepositoryRefusals`); it has no preview. Provenance files are not checked ([H-30](#h-30-chart-provenance-is-not-verified)) |
| Chart README to the drawer and agents | `internal/helm`, `internal/engine`, `internal/ui/fyneui` | Built 2026-10-14 (night build HELM2, [architecture §8](../developer/architecture.md#8-helm-internalhelm)): read from the version's archive through the install's path above, only when the drawer shows the chart or an agent reads it; at most 256 KiB kept, invalid UTF-8 replaced, control characters but tab and line feed and the bidirectional embeddings, overrides and isolates dropped (`FuzzCleanReadme`); a copy in Cogpit's cache only when the index's digest verified the archive, checked again when read back (`TestReadmeStoreBounds`). The window renders it as Markdown with Fyne's RichText: images never loaded (their alt text instead, so the README names no host Cogpit fetches from), raw HTML dropped, a link opened only on the user's click and only for an absolute http or https URL (`TestChartDrawerReadme`). Agents read it framed as the chart author's text, not instructions (decided by the owner on 2026-10-14 evening, A31, built the same night: `ReadmeFrameStart` in [`engine/agentview.go`](../../internal/engine/agentview.go), `TestHelmChartReadmeForAgents`; unmarked before, [H-86](agents-and-mcp.md#h-86-a-charts-readme-reaches-agents)) |
| Chart icon to the window | `internal/helm`, `internal/engine`, `internal/ui/fyneui` | Built 2026-10-14 ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them), [`helm/icon.go`](../../internal/helm/icon.go), [`engine/helmicons.go`](../../internal/engine/helmicons.go)): only for a row or drawer the window draws and while Preferences › Load chart icons is on (only the user turns it on; agents fetch none); https only, a redirect too; no credentials, cookies or referrer; at most 256 KiB and 20 s; a host at a loopback, private, link-local or other non-public address refused unless it is a chart repository's own, the checked address dialled (`TestFetchIconRefusesPrivateAddresses`); through the Preferences' proxy; the bytes checked as an uploaded cluster icon is (`CheckIcon`: a PNG or JPEG decoded under bounds and written again as a PNG of at most 256 pixels a side, an SVG kept only when it refers to nothing outside itself, anything else refused) and checked again when read from Cogpit's cache (`TestChartIconRefusals`) |
| Alertmanager's answer to the pill, the board and agents | `internal/alerting`, `internal/engine`, `internal/ui/fyneui` | Built 2026-10-17 ([D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)): only `GET`, the answer bounded (16 MiB, 10,000 alerts, an annotation 4 KiB, 64 labels of 1 KiB) and read as Alertmanager's JSON list or refused (`TestDecodeAlerts`); a failure a class or the API server's own message, never the service's body (`TestErrorText`); its texts shown as text, never markup; an object a label names opens only Cogpit's own views, resolved against the kinds the cluster serves; a runbook URL kept only when it is absolute http or https without user information (`TestNoAlertmanager`'s `webURL` cases) and opened in the browser only on the user's click, checked again in the window (`alertboard.go`'s `openRunbook`); the alerts reach agents as data ([H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)) |
| Request to command | `internal/engine` | Params validated against the schema; `enabled` and access review before `run`; confirmation by origin ([agents and MCP](agents-and-mcp.md)). Built: the schema check refuses unknown fields, then `Command.Enabled` asks the connected user's cached access reviews about every declared verb whenever the parameters name an object, and a known denial refuses the command for every origin ("not permitted: you may not …"), an unknown answer passes and the API server decides; then the confirmation ([`engine.go`](../../internal/engine/engine.go) `Dispatch`, [`access.go`](../../internal/engine/access.go)); see [H-21](#h-21-access-reviews-guide-they-do-not-guard) |
| Link to navigation | `internal/engine`, `cmd/cogpit`, `internal/ui/fyneui` | Built 2026-10-14 ([D-47](../developer/decisions.md#d-47-deep-links-cogpit), [`engine/deeplink.go`](../../internal/engine/deeplink.go)): only the app bundle registers the scheme; the Apple event handler hands at most 2,049 bytes of a link to a queue that holds at most 8 and drops a longer link (`TestDeepLinkAppleEvents`, `TestDeepLinkQueue`); a link is parsed strictly or refused whole: `cogpit://open` with the parameters `context`, `view`, `namespace` and `name` only, each once, not empty, without control or format characters, in its form and bounds (`TestParseDeepLinkRefusals`, `FuzzParseDeepLink`); its context must be one of the kubeconfig sources' (or a development build's demo cluster; a release has none since 2026-10-16, [D-67](../developer/decisions.md#d-67-the-demo-cluster-stays-out-of-releases)), never a file, and one whose credential plugin or token file waits for the user's approval is not connected: the approval stays the user's, in the window (the navigator's clusters, ⌘P; `TestDeepLinkRefusals`); what it asks for runs as five navigation commands with origin `deeplink`, and `Dispatch` refuses that origin every other command and any invocation that is not a UI or read command, before the parameters decode and again after `Escalate`, so no write is reachable from a link (`TestDeepLinkCommandsOnlyNavigate` over the whole registry, `TestDeepLinkGuardsAnAlteredCommand`); every link is audited (origin `deeplink`) and said in a toast, the window raised. Decided (owner, 2026-10-14 evening) and built 2026-10-14: a link that names another cluster than the connected one asks the user once, naming the context, before it switches; within the connected cluster it navigates at once (`askLinkConnect`, `TestDeepLinkAsksBeforeItConnects`; [H-89](#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)). Confirmed by the owner (2026-10-15): navigation only, as built, with these defaults |
| MCP request to engine | `internal/mcp` | Loopback only, bearer token, `Host` and `Origin` checks: [`internal/mcp/http.go`](../../internal/mcp/http.go), `TestHTTPGuard`, `TestNewRefusesNonLoopbackAddresses` |
| Cluster data to the LLM endpoint | `internal/agent` | Secret values withheld by default; what is sent is shown. Built 2026-10-14 (the engine) and in the night run of 2026-10-14/15 (the chat column, [D-10](../developer/decisions.md#d-10-built-in-agent)): a turn's tool results pass the filter that replaces the cached Secrets' values, as every agent's reads do, unless the user marked the connection private ([agents and MCP › H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows); `TestTurnKeepsSecretValuesFromTheEndpoint`); each user message in the chat shows what its turn sent, requests, bytes, tokens and the endpoint's host, and opens the last request body as sent (`SentPayload`, `TestChatSentPayload`) |
| The LLM endpoint's certificate to the chat | `internal/agent`, `internal/ui/fyneui` | Built 2026-10-15 (the owner's decision, [D-10](../developer/decisions.md#d-10-built-in-agent) (6)): an `https://` endpoint's certificate is verified against the system's roots (on macOS its trust store, through the platform verifier); a connection's CA file, which only the user names in Preferences › Agent, replaces them for that connection alone, as a kubeconfig's `certificate-authority` does for kubectl (the owner's decision in the audit of 2026-10-16, N141): only the file's CAs vouch for its endpoint, read whenever a request is sent ([`agent/cafile.go`](../../internal/agent/cafile.go); `TestCAFileTrustsAPrivateCA`, `TestCAFileReplacesTheSystemRoots`, `TestCAFileRefusesAPublicServer`, `TestCAFileErrorsAreNamed`); no redirect is followed. A plain `http://` endpoint on another machine is allowed and warned about in Preferences › Agent and the connection test ([credentials › H-95](credentials.md#h-95-an-llm-api-key-over-plain-http)). What the CA file leaves open: [H-104](#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier) |
| Download to installed package (Linux) | apt; `internal/update` (`apt_linux.go`) | Built 2026-10-20 ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)): apt checks the repository index's OpenPGP signature with the key in `/etc/apt/sources.list.d/cogpit.sources`, the index files by hash and every package's SHA-256, and upgrades only to a newer version; Cogpit reads `apt-cache policy` and, on the user's click in the window only, runs `/usr/libexec/cogpit/update` through pkexec, which polkit gates with an administrator's password ([updates › Linux](updates.md#linux)) |
| Download to installed app | `internal/update` (Sparkle 2) | Built 2026-10-14, not proven by a release ([D-4](../developer/decisions.md#d-4-packaging-and-updates)): Sparkle checks the feed's and the disk image's EdDSA signatures with the key in the app's `Info.plist`, before extracting anything, then the new app's Developer ID against the running one's, and offers only a newer version ([updates](updates.md)); the app loads Sparkle only from its own installed bundle (`TestSparkleLoadsFromTheBundle`) |
| kubectl download to a terminal's `PATH` | `internal/kubectl`, `internal/engine`, `internal/terminal` | Built 2026-10-13 ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)): only the user's click in the window downloads (`kubectl.download` refuses agents and the CLI before anything is fetched, `TestKubectlDownloadCommand`), and only the user sets the path to kubectl, the directory for binaries and the mirror (`ErrKubectlGUIOnly`); the mirror is an https URL without user information, query or fragment, and redirects are followed to https only (`TestCheckMirror`, `TestDownloadHTTPSOnly`); the `.sha256`, `.sig` and `.cert` files are read first (16 KiB each at most), and before the binary is asked for the signature is checked (since 2026-10-15 with sigstore-go, [D-50](../developer/decisions.md#d-50-kubectls-signature-check)): offline, the signature must have been made over the published digest by the certificate's key (`ParseSigned`); then sigstore's transparency log (`rekor.sigstore.dev`, through the same client and proxy, 30 s and 64 KiB at most) is asked for its entry of exactly that digest, signature and certificate ([`rekor.go`](../../internal/kubectl/rekor.go)), and sigstore-go checks, against the trusted root Cogpit embeds ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json)), the entry's inclusion proof up to a checkpoint the log signed and its signed entry timestamp, that timestamp inside the certificate's ten minutes, the certificate's chain to Fulcio at that time and one signed certificate timestamp of a CT log, the identity `krel-staging@k8s-releng-prod.iam.gserviceaccount.com` with the OIDC issuer `https://accounts.google.com`, and the signature over the digest ([`verify.go`](../../internal/kubectl/verify.go); `TestRealReleaseSignature` verifies kubectl 1.36.1's and 1.37.1's published signatures and Rekor entries offline, `TestRealReleaseRefused`, `TestVerifierRefuses`, `TestDownloadRefusesBadSignatures`); no entry, no answer from the log or an entry that does not verify refuses the download, with no fallback to a check without the log (`TestDownloadWithoutTheLog`); the binary (at most 128 MiB, `TestDownloadSizeBound`) goes into a temporary file beside its place, must have the published digest (`TestDownloadRefusesTamperedBinary`), and is made 0755 and renamed into `<directory for binaries>/<version>/kubectl` (directories 0700); nothing is left on failure. It goes first on `PATH` in Cogpit's terminals only, as a link in the terminal's own temporary directory, never in a shell profile or a global location (`TestStartLocalKubectlFirst`, `TestZshKubectlFirstAfterTheProfile`). What the check rests on, an embedded trusted root and Rekor v1's search: [H-35](#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search); a profile can still reorder `PATH` ([H-36](#h-36-a-shells-profile-can-put-another-kubectl-first); decided the same evening and built the same night, A26 and A27: the hand-over for bash and fish too, and Preferences › Kubernetes › Enforce kubectl, a shell function that enforces Cogpit's kubectl; `TestBashKubectlFirstAfterTheProfile`, `TestFishKubectlFirstAfterTheConfig`, `TestZshKubectlEnforced`) |

## What this does not cover

### H-104: A connection's CA file replaces the system's roots, checked by Go's verifier

Since 2026-10-15 (the owner's decision, [D-10](../developer/decisions.md#d-10-built-in-agent) (6)):
an LLM connection may name a CA file for an `https://` server with a private CA. Since 2026-10-16
(the owner's decision in the audit of 2026-10-16, N141) its certificates are that connection's only
roots, as a kubeconfig's `certificate-authority` is for kubectl (`x509.NewCertPool` plus
`AppendCertsFromPEM`, [`agent/cafile.go`](../../internal/agent/cafile.go)); until then they were
added to the system's roots. In Go 1.27.2 (read in its `crypto/x509` `verify.go`), only a pool
marked as the system's goes to macOS's platform verifier, so for such a connection Go's own
verifier checks every chain against the file's certificates alone. What remains, per case:

- **What the file vouches for:** every certificate its CAs signed, for any host name; Cogpit sends
  this connection's requests to its base URL's host only (no redirects), so the reach is that host.
  A broad CA in the file (a company's TLS-inspection CA, a whole bundle) vouches for whatever it
  signed. The user's choice; live only for connections with a CA file.
- **Public CAs no longer vouch for the endpoint:** a mis-issued or stolen certificate from a public
  CA cannot answer as the endpoint of a connection with a CA file, which the night run's addition
  to the system's roots allowed. The other side: a server that moves to a public certificate fails
  the connection test and every turn with "this connection trusts only its CA file …; for a server
  with a public certificate, clear the CA file" until the user clears the field
  (`TestCAFileRefusesAPublicServer`). An availability matter, not a gap.
- **Go's verifier, not macOS's, for every chain of the connection:** no revocation check (OCSP,
  CRL), no Certificate Transparency, and none of the keychain's trust settings: a CA the user marked
  "Never Trust" in Keychain Access is still trusted through the file. Go checks the validity
  period, the host name, the key usage and name constraints. Live for every connection with a CA
  file; before 2026-10-16 only for one whose server macOS refused.
- **The file is the user's own:** a process of the user that writes the file puts a CA of its own
  in its place or beside it; a machine on the network path could then answer as the endpoint for
  that connection and read the key, the conversation and what the agent reads. It needs both,
  write access to the file and a place on the path; Cogpit does not notice a changed file (it reads
  the file anew for each request, by design: a renewed CA counts at once). Dormant unless both hold,
  as for a kubeconfig's `certificate-authority` file.
- **`SSL_CERT_FILE` and `SSL_CERT_DIR`:** since Go 1.27, either one in Cogpit's environment makes
  macOS's Go code load the system's roots from those files instead of asking the platform verifier
  (unless `GODEBUG=x509sslcertoverrideplatform=0`), for every TLS connection of the process that
  uses the system's roots: clusters, Helm repositories, a chat connection without a CA file. A
  connection with a CA file uses its own pool and is not affected (Go 1.27.2's `verify.go` uses a
  pool it is given as it is). An app started from the Finder or the Dock does not inherit a shell's
  variables; one started from a terminal does. Not checked how Cogpit behaves then beyond Go's
  documented rule.
- **The path is not secret** and sits in `llm-connections.json` (0600) beside the base URL.

### H-89: A link switches the cluster and the view under the user

Live since 2026-10-14 on macOS ([D-47](../developer/decisions.md#d-47-deep-links-cogpit)). Any web
page, mail or app can open a `cogpit://` link, and Cogpit follows it without asking: it connects
another context of the user's kubeconfig sources and opens a view or an object's drawer there.
Browsers usually ask before they hand a link to an app ("Open Cogpit?"); a mail or chat app may not
(neither verified with Cogpit, nor per app). Per case:

- **The wrong cluster.** A link switches the connection while the user works on another cluster; a
  user who misses the toast may act in the new one. Bounded by: the window comes to the front and a
  toast names the context; a write the user confirmed for the other cluster refuses to run
  (`CheckConfirmedContext`); the other cluster's tabs stay; nothing is written by the link itself.
- **The cost of a connect.** Each link to another context connects it: discovery, the access
  reviews and the lists of the view it opens, with the user's identity. A page that opens links in
  a loop makes Cogpit connect again and again (the queue holds at most 8, and a browser asks per
  link). Bounded since 2026-10-14 by the question below.
- **An approved credential plugin runs.** A context whose plugin the user allowed is connected,
  so the plugin runs (an OIDC login may open a browser). A plugin not allowed, or changed since,
  never runs from a link.

Decided (owner, 2026-10-14 evening) and built the same night: a link that names another cluster
than the connected one asks the user once, naming the context and what it opens, before it
connects (also while none is connected: the lead's night decision, confirmed by the owner on
2026-10-15, [D-47](../developer/decisions.md#d-47-deep-links-cogpit)); navigation within the connected cluster
stays immediate (`askLinkConnect`, `TestDeepLinkAsksBeforeItConnects`). Per case now: **the wrong
cluster** needs the user's yes, which names the context; **the cost of a connect** is a question per link instead of a connect,
and a page that opens links in a loop raises questions rather than connections (the queue still
holds at most 8); **an approved credential plugin** runs only after that yes. Confirmed by the
owner (2026-10-15): D-47 as a whole, navigation only as built, with the build's defaults (a login
waiting for approval stops the link, no approval dialog from a link; the window raised and a toast
for every link; one audit line per link; the ask before every connect); weighed: commands with a
confirmation (consent phishing), no deep links (losing Lens's `lens://` parity). What remains: a
link within the connected cluster still changes the view and opens a drawer under the user without
asking, and a yes to a question a page raised switches as today. Not built and not decided: a
setting that turns links off.

### H-90: A link's text reaches agents through the logs

Live since 2026-10-14. The audit log keeps every link: the parameters of one that parses (a
context of up to 512 bytes of any printable text, refused or not, a view key, a namespace, an
object name), or at most 256 bytes of one that does not, escaped. The notification log keeps its
toast, which quotes up to 200 bytes of a context that names none of the user's and up to 64 of
another refused value. Agents read both (`audit_list`, `notifications_list`), so a web page can put
a short text of its choice where an agent reads: a channel for the instructions of
[H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data), live whenever links reach
Cogpit and an agent reads those logs. Bounded by: control and format characters are escaped or
refused; the view, the namespace and the name have narrow forms (`[a-z0-9.-/]`, a DNS label,
`[A-Za-z0-9._:@+-]`). Not built: leaving a refused link's text out of what agents read.

### H-1: Kubeconfig files added by sync folders

Lens watches `~/.kube/` and any files and folders listed under Preferences › Kubernetes ›
Kubeconfig Syncs, and adds every kubeconfig that appears there; captured 2026-10-12, it runs a
synced file's credential plugin on connect without asking
([add a local cluster](https://docs.lenshq.io/k8slens/getting-started/add-clusters/add-local-cluster/),
[preferences](https://docs.lenshq.io/k8slens/using-lens/preferences/)). Cogpit adds kubeconfig
files and sync folders since 2026-10-12 (and `~/.kube` by default, below), and a file dropped into a synced
folder (a download, a shared drive) becomes a set of contexts; hidden files too, as in Lens. The check of the boundary table is
built as follows:

- **When.** For a context of an added or synced kubeconfig whose user has an `exec`
  section (or a `tokenFile` outside the folder, H-24), the window (the navigator's section, the hotbar, the welcome page; the context picker until [D-57](../developer/decisions.md#d-57-choosing-a-cluster)) shows the plugin before it connects, when there is no approval or the
  approval no longer matches. The engine refuses the connect for every origin on its own
  ("the kubeconfig X runs the program Y to log in, which you have not allowed yet"); a `--context`
  at start-up lands on that refusal, which the welcome page shows with the way to ⌘P.
- **What is shown.** The program as client-go will run it (a relative path resolved against the
  kubeconfig's folder), where it is found on `PATH` now or that it is not found, each argument on
  its own line, the environment variables by name only (a warning when they set `PATH`, `LD_*` or
  `DYLD_*`), the server the credential goes to, **how that server is reached and trusted** (since
  2026-10-12, review of the fourth run, R-11: certificate checks off, a proxy URL without user
  information, a certificate authority the kubeconfig brings, embedded or as a file, and a TLS
  server name; each of the first three is a line in the warning colour above the rows, and a plain
  connection with the system's trust adds nothing), whether the program or the CA file lies inside
  the kubeconfig's folder with the content digest the approval binds, the kubeconfig file, the
  context and its source, and, for a changed plugin, that it changed since it was allowed. All of
  it is escaped for control characters, bidirectional overrides and invalid UTF-8, and capped in
  length (`TestApprovalDialogShowsProxyAndInsecure`, `TestApprovalDialogEscapesUntrustedText`).
  "Allow and Connect" arms after 750 ms; nothing has focus by default.
- **What is remembered.** One approval per file and context in `<user config dir>/Cogpit/exec-approvals.json`
  (0600, written atomically, never through a symlink), bound to a SHA-256 fingerprint (version 2
  since 2026-10-12; every approval of version 1 asks again once) of the file, the context, the
  user, the server URL, its CA data and file, the insecure flag, the TLS server name, the proxy
  URL, the plugin's API version, command, arguments and environment (names and values, in order),
  its interactive mode and `provideClusterInfo`, the token file when it needs approval
  ([H-24](#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)), and, **for a program or
  a CA file that resolves to a file inside the kubeconfig's own folder (the place a writer of a
  synced folder can write), the SHA-256 of its content** (review of the fourth run, R-5, owner
  decision, [D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program), "programs in the folder only"): replacing `./login.sh` or the CA file
  after the approval asks again, at the next listing and at connect time, where the files are read
  again right before the policy decides (`TestExecApprovalRefusedAfterPluginBinaryChanged`,
  `TestCAFileSwapAsksAgain`). A link in the folder is bound by its resolved target as well, so
  repointing it asks again; a program resolved elsewhere (`PATH`, `/usr/local/bin`, `/opt/homebrew`:
  `kubelogin`, `aws`, `gke-gcloud-auth-plugin`) stays bound by its path, so a tool update does
  not ask again (`TestProgramOutsideTheFolderIsBoundByPathOnly`). On Windows the program bound is
  the file `os/exec` runs (since 2026-10-21, [D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle),
  found by the first test run there): a command path without one of `PATHEXT`'s extensions runs as
  the first of `plug.com`, `plug.exe`, `plug.bat`, … beside it, so a `plug` in the kubeconfig binds
  `plug.exe`, and a decoy `plug` beside a `plug.exe` swapped at will no longer keeps the approval
  (`TestProgramIsBoundByTheFileThatRuns`). The hashing is bounded: regular
  files only, at most 32 MiB for a program and 1 MiB for a CA file; a larger or unreadable file
  inside the folder is bound by a note (its size and modification time for a large one), so a
  change still asks again. The approval names the fingerprint the dialog showed and is refused
  if the file changed meanwhile; an unreadable approvals file allows nothing; removing a source
  forgets its approvals.
  The GUI asks before removing one, naming the path and saying that only Cogpit's link goes and
  the files stay on disk, and a connection made from the source closes.
- **Asked once per file and plugin** (owner, 2026-10-12, [D-28](../developer/decisions.md#d-28-the-default-kubeconfig-as-sources-as-lens-has-it);
  only the asking is bundled). Contexts of one file whose logins are the same plugin (the same
  command, arguments, environment, API version, interactive mode, `provideClusterInfo`, program
  binding and, for D-17, token file: `kubeconfig.ExecPlugin.Identity`) are one dialog, which lists
  **every context that is not allowed yet, each with its own server and how that server is reached
  and trusted** (the R-11 warning lines per context, under its name), and one click allows them all.
  Nothing in what an approval binds got looser: the engine still stores **one approval per context
  with that context's own fingerprint**, which binds the server, its CA, proxy, TLS server name and
  insecure flag, so a changed server in one context asks again for that context only, even when
  its siblings are allowed. The approving command takes the `(context, fingerprint)` list the
  dialog showed, checks each against the file as it is now, and refuses the **whole** list, naming
  the contexts, if any changed or left meanwhile; nothing is stored then
  (`TestApprovalIsRefusedAsAWholeWhenOneContextChanged`). A context added to the file later is not
  in an earlier approval and asks on its own, or with the other new ones
  (`TestAContextAddedLaterAsksOnItsOwn`). What one click means is wider than before, and that is the
  point of the owner's decision: the user allows every listed context, each shown with its server
  and trust, in one step; with many contexts the dialog scrolls, and its buttons stay visible
  without the user having scrolled to the end of the list. Agents and the CLI still cannot approve,
  and read the grouped listing (`kubeconfig.pendingApprovals`) without arguments and environment
  values (`TestPendingApprovalsAreBundledAndApprovedByTheGUIOnly`).
- **Who.** Adding a file or a folder, removing a source and allowing a plugin are GUI
  only: the engine refuses them for the CLI, external agents and the built-in agent, whose
  confirmation dialog could not show the user an agent-chosen path for review
  (`TestKubeconfigCommandsAreGUIOnly`).
- **`cogpit-cli`** (built 2026-10-13) offers the same sources and honours the same approvals: both
  binaries build them in [`internal/app`](../../internal/app/app.go), and the CLI connects through
  the same `ExecPolicy`, so a plugin the user has not allowed in the window is refused before it
  runs (`TestGetRunsNoPluginTheUserHasNotAllowed`). It writes neither the sources nor the
  approvals, and without a configuration directory it refuses to connect, where the app falls back
  to merging the files as kubectl does (`TestNoConfigDirNoConnection`; Cogpit's default, not an
  owner decision). Context names and file names are escaped before they reach the terminal
  (`TestContextsListsTheSourcesWithoutSecrets`).

**`~/.kube` is synced by default, and so is everything else** (2026-10-12; Lens does the same,
captured; [D-28](../developer/decisions.md#d-28-the-default-kubeconfig-as-sources-as-lens-has-it), built
the same day). The folder is the user's own and Cogpit neither writes nor runs anything in it
unasked, but a folder like that collects files nobody chose to add (a kubeconfig saved or generated
into it by the user or a tool). Every file of it is therefore handled as the file of any sync
folder, **`config` included**: read on its own, never merged; a credential plugin runs only after the
user allowed it (the approval above, per file and context, forgotten when the folder is removed).
There is no longer a "default kubeconfig" under kubectl's rules: the files `$KUBECONFIG` lists and
Cogpit's `--kubeconfig` paths are sources of their own for the run, under the same approval, and
removable for the session only (their approvals are kept; a file that lies directly in `~/.kube` is
the folder's, so removing the folder hides it). Per case: a process that can write into `~/.kube`
could replace `config` and have kubectl run its plugin; in Cogpit it gets a dialog instead
(dormant, and narrower than before, when `config`'s plugin ran unasked); a kubeconfig that arrives in
the folder by accident becomes contexts, and its plugin waits for the user (the approval holds); its
`tokenFile` is H-24 below, live for these files as for every file. The cost, accepted by the owner:
a `config` with a credential plugin asks, as any other file does, and again
when the plugin changes (once per file and plugin, listing its contexts, since the owner's decision of the same day, above). The user can remove the folder (Kubeconfig Sources), and the engine takes
the folder and the session files as options, so tests never read the real ones
(`TestNoDefaultFolderWithoutTheOption`, `TestConfigWithAPluginAsksForApprovalOnce`,
`TestDefaultKubeconfigIsOneFileOfTheSyncedFolder`).

What remains: kubectl in a terminal of such a context reads the source file again, so a
plugin changed after it was allowed runs there without being asked (the user's own kubectl
command); and a context without a plugin connects without asking only when it names no token file
outside its folder ([H-24](#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)).
Content binding is narrower than "everything the plugin runs", by decision (owner, 2026-10-12):

- a program found outside the kubeconfig's folder is bound by its path, not its content: whoever
  can write to a `PATH` directory or to `/usr/local/bin` already controls the user's account, and
  binding them would ask again after every tool update;
- only the program file itself is hashed: a script in the folder that sources or imports a sibling
  file, or an interpreter that loads libraries, is bound through the script only, and so is a
  binary's own configuration;
- "the folder" is the directory of the kubeconfig file, its subdirectories included; for a file
  the user added from a broad directory (the home directory, Downloads) that is every file below
  it, not only what a sync writer controls;
- the files are read right before the policy decides and client-go starts the program a moment
  later: a writer who swaps the program inside that window, and swaps it back, is not caught (a
  file is not locked; the window is microseconds and needs write access to the folder at that
  moment);
- the client certificate and key files a kubeconfig names are bound by path only (the key is never
  sent; the certificate only in the TLS handshake).

A link in the folder that points to a file outside is bound by its target path; a tool manager that
repoints it (a Homebrew upgrade of a linked `aws`) asks once again.

### H-101: A credential plugin runs again during a connection without being checked again

Live since credential plugins run in Cogpit (2026-10-11), found and narrowed while building
[D-58](../developer/decisions.md#d-58-standby-and-lost-connections) (2026-10-15/16). The approval
binds a plugin's program ([D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program))
and is checked when a connection is made: the files are read again right before client-go starts
the program (H-1). During the connection, client-go's exec authenticator runs the program again by
itself, by its path as it is then: when the credential it returned expires (its
`expirationTimestamp`; a cloud token after an hour, a login after a night's sleep), and on a 401
Unauthorized. None of these runs is checked against the approval: a program inside the kubeconfig's
folder that a writer of a synced folder replaced after the connect runs at the next renewal without
the dialog a new connect would show. What stands in the way since D-58: Cogpit's own renewals check
the approval first and run no changed plugin (the check at wake, the reconnect after an
interruption, Log in again; they make the login expired and ask for a new connect instead, which
shows the plugin, `Connection.NeedsApproval`); the login
guard lets client-go answer only the first 401 after a request that worked with a run, where before
every 401 could start one ([`cluster/health.go`](../../internal/cluster/health.go)); and the
replacement needs write access to the kubeconfig's folder while the user is connected. Since
2026-10-16 ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once) item 4) a
cluster the window does not show whose setting "Renew the login while the cluster is not shown" is
off runs none of client-go's renewals while it is held ([`cluster/loginhold.go`](../../internal/cluster/loginhold.go),
`TestHeldPluginDoesNotRun`), and the renewal when it is shown again is Cogpit's own, which checks the
approval first. Not covered: client-go's run at a credential's expiry and at that first 401, while
connected, for the shown cluster and for a cluster not shown whose setting is on (the default).
Closing it needs Cogpit to run the plugin itself (client-go's exec authenticator re-done for tokens
and client certificates) or a check of the program before each of client-go's runs. Client-go
offers no hook for that; the hold wraps one of its unexported fields (`interactiveFunc`, asked right
before each run) by reflection, so the same wrapper could check the approval before every run, at
the cost of depending on client-go's internals as the hold and D-66's capture do. Not built.

### H-34: The plugin approval shows the kubeconfig's proxy, not Cogpit's

The approval of a credential plugin binds, and its dialog shows, the kubeconfig's own `proxy-url`
(R-11). Since 2026-10-13 a cluster's settings in Cogpit can name a proxy of their own, which replaces
the kubeconfig's for Cogpit's connections ([credentials](credentials.md#where-they-live-inside-cogpit)):
the credential the approved plugin returns then travels through a proxy the dialog did not name, and
changing that proxy does not ask for the approval again. What keeps it narrow today: only the user
sets a proxy, in the window (agents and the CLI are refused, [agents and MCP](agents-and-mcp.md));
with the server's TLS verified a proxy sees the server's name and the traffic's timing and size, not
the token. It is live for a context with a plugin whose user also set a proxy, and real where the
server's certificate is not verified (`insecure-skip-tls-verify`): there the proxy reads the bearer
token. A plain-http server gets no credential from client-go (`TestPlainHTTPServerGetsNoCredential`;
corrected 2026-10-13, this page said before that a proxy reads the token there too).

Decided (owner, 2026-10-14 evening, A23) and built the same night
([D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program)): a
credential plugin's approval shows and binds Cogpit's own proxy for the cluster too, and a changed
proxy asks for the approval again. The dialog names the proxy the connection goes through in the
warning colour (with the kubeconfig's `proxy-url` beside it as not used), the fingerprint binds it,
and the connect-time check binds the proxy from the same read of the setting that the connection
uses, so a proxy set after the approval stops the plugin before it runs
(`TestPluginApprovalBindsTheClusterProxy`). The case above is closed for contexts with a plugin.
What remains, per case:

- the proxy is bound as it is shown, its password `•••` (the lead's night decision): a new
  password for the same proxy user, host and port does not ask again; it does not change who
  receives the traffic;
- a context without a plugin or a foreign token file (an inline token, a client certificate, a
  token file inside the kubeconfig's folder) has no approval, so a changed proxy asks nothing there.
  With the server's certificate checked the proxy sees only the server's name, timing and size; an
  agent can set such a proxy only with the user's acceptance (A20). Under `insecure-skip-tls-verify`,
  where the proxy would read the token, such a context has an approval since the same night (A28,
  [H-37](#h-37-a-context-that-skips-the-certificate-check)) that binds the proxy too, so the case
  that made this real is closed;
- the environment's `HTTPS_PROXY`, which client-go uses when neither the kubeconfig nor Cogpit names
  a proxy, is not shown or bound: it is the user's own session's.

### H-37: A context that skips the certificate check

A kubeconfig cluster with `insecure-skip-tls-verify: true` makes client-go connect without checking
the API server's certificate, as kubectl does. Whoever answers at the server's address then gets
the context's credential: a machine on the network path, a spoofed DNS answer, a proxy on the way
(Cogpit's or the kubeconfig's). Verified: the token of such a context reaches a server whose
certificate no CA vouches for (`TestInsecureSkipTLSVerifyConnectsToAnUntrustedServer`). What is at
stake per credential form: a bearer token (inline, a token file, an exec plugin's or the legacy
`oidc` provider's) is handed over in full and works against the real cluster until it expires, and
an impostor relaying to the real server reads and can change everything in between, the user's
writes included; a client certificate's private key never leaves Cogpit, so an impostor can neither
reuse it nor relay as the user, only show made-up cluster data and swallow the user's writes.

What Cogpit did first (built 2026-10-13; Cogpit's default, not the owner's decision, Lens documents
nothing about such contexts): it connects, as kubectl does, and says so in two places. The context
picker marked the context with "certificate not checked" and a line naming the server the
credential goes to (`ContextInfo.Insecure`, from the kubeconfig); since the picker went (2026-10-15,
[D-57](../developer/decisions.md#d-57-choosing-a-cluster)) the welcome page's row carries that pill and line
([`clusterrow.go`](../../internal/ui/fyneui/clusterrow.go), `TestClusterRowsMarkAnInsecureContext`)
and the navigator section's row a warning icon before its connection dot, with the line in its
tooltip (`navContextRow.insecure`, `navContextRow.tipText`, `TestNavigatorSectionTooltipLines`;
the icon added 2026-10-16 so that a context the welcome page does not list is marked before its
connect without the pointer on it; the lead's decision, open to revision); the connect log gets a WARN line once the connection
stands, from the connection's own configuration (`Conn.Insecure`; [`engine/tlswarning.go`](../../internal/engine/tlswarning.go),
`TestInsecureContextIsMarkedAndLogged`). Agents see `insecure: true` in `cluster.contexts`, and
`cogpit-cli contexts` says `not checked` in its table's CERTIFICATE column (2026-10-13,
`TestContextsMarksAnInsecureContext`). The
plugin approval dialog of a kubeconfig source already showed the flag (R-11). Live for every
context whose cluster sets the flag, common for test clusters and for servers reached by an address
their certificate does not name (`tls-server-name` is the fix there, and the connect error now
points at it).

Decided (owner, 2026-10-14 evening): Cogpit asks once per context before the first connect of an
`insecure-skip-tls-verify` context and keeps the answer like a plugin approval; while such a
context is connected, the status bar shows "TLS not checked" in the warning colour beside the
context; `cogpit-cli get` writes a warning line to standard error.

**Asked once, built 2026-10-14 (A28).** A source's context whose cluster skips the check is a login
that needs the user's approval (`kubeconfig.LoginInsecure`), kept in `exec-approvals.json` and
checked by the same policy as a plugin, at connect time from the very `rest.Config` the client is
built from, before any request leaves (`checkExecPolicy`; `TestConnectAsksThePolicyAboutAnUncheckedCertificate`,
`TestInsecureContextAsksOnceBeforeItsFirstConnect`). Every window path that connects (the
navigator's section, the hotbar, the welcome page; the context picker until D-57) shows the dialog "Connect without checking the certificate?" with
the server and how it is reached, and the user's yes is kept across restarts; a context of a file a
sync folder picked up is asked like any other ([H-1](#h-1-kubeconfig-files-added-by-sync-folders)).
**The lead's night decisions, open to revision** (★ chosen):

- *What the answer binds:* ★ the plugin approval's fingerprint: the file, context, user entry's name,
  server URL, CA, the flag, TLS server name, the kubeconfig's `proxy-url` and Cogpit's own proxy
  (A23), so another server or proxy asks again; the credential itself (token, certificate) is not
  bound. Options: the server and the flag only (a proxy added later would not ask); the
  credential's digest too (every token rotation would ask).
- *A context that also runs a plugin or reads a foreign token file:* ★ one question, the plugin's
  dialog, which already warns that the check is off; its one approval covers both. Option: two
  approvals and two dialogs.
- *Asking per context or per file:* ★ per context, never bundled with the file's other unchecked
  contexts (the decision's "once per context"; a check that is off concerns one server). Option:
  one dialog per file as D-28 bundles plugins.
- *Agents, the CLI and links:* ★ refused until the user allowed it in the window, as a plugin not
  allowed yet is today: an agent's or the CLI's `cluster_connect` ends in state error naming the
  flag and the window (`TestInsecureContextConnectsPerOrigin`, `TestGetRefusesAnUncheckedContextUntilAllowed`),
  a `cogpit://` link is refused without asking (`TestDeepLinkRefusals`), and allowing stays GUI
  only. Option: an agent's call waits while the window asks (a security question an agent can
  raise at a moment of its choosing).
- *Earlier approvals:* a plugin approval given before for a context that skips the check still
  stands (its fingerprint bound the flag, its dialog warned); a context without a plugin asks once
  after the upgrade.

**Marked while connected, built 2026-10-15 (A29, the same night run).** The engine's UI state carries
`connection.tlsUnchecked` while such a connection stands, from the connection's own configuration
(`Conn.Insecure`), so the window and agents read the same fact (`ui_state`, and `cluster_connect`'s
and `hotbar_openCluster`'s results); the status bar shows "TLS not checked" in the warning colour
right beside the context (`TestStatusBarSaysTLSNotChecked`, PNG
[dark](../../internal/ui/fyneui/testdata/statusbar_tls_unchecked_dark.png),
[light](../../internal/ui/fyneui/testdata/statusbar_tls_unchecked_light.png)); `cogpit-cli get`
writes one line to standard error, "cogpit-cli: warning: TLS not checked for <context>." followed by
the connect log's sentence naming the server, and leaves standard output as it was
(`TestGetWarnsAboutAnUncheckedCertificate`). Cogpit's details, not the owner's decision: the item
shows only once connected (not while connecting), has no tooltip and no click, and the navigator
does not mark the cluster.

What remains: the check stays off, so after the user's yes every connect hands the credential to
whoever answers at the address, as before; refusing such contexts is not decided; a context of a
connector without sources (the merged kubeconfig of a start without a configuration directory,
tests) is not asked, as its plugins are not; `cogpit-cli mcp` and agents learn it from the state
and results, not from a warning of their own.

### H-30: Chart provenance is not verified

The digest Cogpit checks a chart archive against comes from the repository's own `index.yaml`,
served by the same host as the archive (2026-10-13). It catches a corrupted download or an index
and archive that do not belong together, not a repository that serves a malicious chart with a
matching digest. Helm's provenance files (`<chart>-<version>.tgz.prov`, an OpenPGP signature over
the chart's digest, `helm install --verify`) are not downloaded or checked, and Cogpit keeps no
keyring. A chart then runs as templates in Cogpit's process (no plugins, post-renderers or DNS
lookups, but `lookup` reads what the user may read) and its objects are created with the user's
identity after the preview and, for an agent, the user's acceptance; an upgrade to a repository's
version (2026-10-13) changes a release's objects to such a chart the same way, without a preview.
Which keys to trust and where
they would live is not designed.

### H-87: Chart icons tell their hosts what the user looks at

Live since 2026-10-14 while Preferences › Kubernetes › Load chart icons is on, which it is by
default ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them), decided
by the owner 2026-10-14). Per case:

- **Who learns what.** Every host an index names as an icon's URL gets a request from the user's
  address (or the proxy's) when the Charts view shows the chart's row or its drawer opens, at most
  once a week per URL (the cache): that this computer runs Cogpit (the user agent), when, and which
  chart's icon. Scrolling the Charts view of a large repository asks the hosts of every icon the
  rows show. A public repository's icons mostly live on the project's own site, GitHub or a CDN.
- **What a hostile index can do with it.** Name a URL of its own per user to track who opens which
  chart (a tracking pixel); name a host inside the user's network: refused when it resolves to a
  loopback, private, link-local or other non-public address, unless it is a chart repository's own
  host (any repository in Helm's file), which an index of that repository could already reach.
  The address is checked when the request dials it, so a name that resolves elsewhere a moment
  later does not slip through.
- **Through a proxy** (Preferences › Proxy or `HTTPS_PROXY`) the proxy resolves the host, and the
  address check does not apply: a proxy that reaches the user's network carries a request to any
  host there an index names (dormant without a proxy; live with one that forwards to internal
  hosts). Only GET, without credentials, cookies or a body, and the response is read only as an
  image.
- **Bounded by:** the switch, which turns all of it off and which agents cannot turn on; no
  credentials, cookies or referrer with any icon request; https only.

Not built: a list of hosts the user trusts for icons, icons from the repository's own host only
(D-46's option (c)), resolving the host before handing a request to a proxy.

### H-88: Chart icons are decoded in Cogpit's process

Live since 2026-10-14 with chart icons on (default): an icon's bytes come from any host its index
names. A PNG or JPEG is decoded by Go's image packages after its header said it has at most 16
megapixels and 8,192 pixels a side, and is written again as a PNG of at most 256 pixels a side, which
is what Cogpit's cache keeps and the window draws; an SVG is parsed as XML (at most 256 KiB, 20,000
tokens, 64 levels deep, no DOCTYPE, no scripts, event handlers or references outside itself). Since
2026-10-14 (narrowed) the window never hands an SVG to Fyne's renderer: the goroutine that fetched
it parses and rasterises it with `fyne-io/oksvg` into a PNG of 128 pixels a side, under a recover,
and a failure shows the glyph ([`helmcharticons_svg.go`](../../internal/ui/fyneui/helmcharticons_svg.go);
`TestRasteriseSVGDrawsTheIcon`, `TestRasteriseSVGIgnoresAHugeViewBox`,
`TestRasteriseSVGFailsWithoutTheWindow`). Fyne's renderer sized its rasteriser by the icon's
viewBox, a number the icon's host chooses (a viewBox of a billion units a side asked for a buffer no
recover catches), and recovered around the drawing only, not the parsing; Cogpit's rasteriser has
the icon's own size whatever the viewBox says (measured 2026-10-14: 1.2 MiB allocated for a viewBox of 10⁹
units). What remains: a bug in Go's PNG or JPEG decoder, or in oksvg's handling of the paths,
gradients and transforms an SVG may hold, is still reachable from any icon host; a panic fails that
icon only, but one that does not panic (a loop within the 20,000 tokens, a wrong drawing) is not
caught. The same parsers already read the cluster icons the user uploads (a file the user picked).

### H-41: Repositories whose content nothing authenticates

Helm's file may name a repository over plain `http://`, or one with `insecure_skip_tls_verify`
(2026-10-13, D-29: both used as helm uses them, Cogpit's defaults, not the owner's decisions). For
such a repository nothing proves who answers: anyone on the path (a network, a proxy) can serve
another index and charts that match its digests, since the digest Cogpit checks a chart against
comes from that same index ([H-30](#h-30-chart-provenance-is-not-verified)), and for plain http
read the basic auth once the user allowed it for that repository (2026-10-14; before, Cogpit
downloads nothing from one with credentials: [credentials › H-40](credentials.md#h-40-credentials-over-plain-http)). A
chart from it renders in Cogpit's process and is installed with the user's identity after the
preview and, for an agent, the user's acceptance, as any chart is. Live only for entries the user
wrote that way.

Decided (owner, 2026-10-14 evening), built 2026-10-15: an http or insecure-TLS origin is marked
wherever a chart from such a repository is shown or installed, besides the repositories dialog
("http", "insecure TLS") and `helm.repo.list` (`plainHTTP`, `insecureSkipTLSVerify`): the Charts
view's Repository cell and the chart's drawer, the Install tab, the install's preview (the window's
own install is confirmed there), the Upgrade tab (the window's own upgrade has no other
confirmation) and the confirmation of an agent's install or upgrade, which always asks
([D-2](../developer/decisions.md#d-2-agent-interface-scope), A2), in the warning colour, with the
sentence "nothing authenticates the chart, and anyone on the network path can change it"; agents
read the same marks (`spec.origin`, the tabs' `origin`, the preview's `warning`). An agent's install
or upgrade whose repository's entry changed while the user read the confirmation is refused, and so
is a window's upgrade whose tab showed another origin than the entry now has
([architecture §8](../developer/architecture.md#8-helm-internalhelm)). What remains: a mark, not a
refusal and not a question (neither is decided): nothing authenticates such a repository's
content, and a user, or an agent with the user's acceptance, installs its charts as before. The
mark depends on the user reading it; the window's install from a tab shows it in the tab and the
preview, but nothing stops a click past it.

### H-42: A repository another program writes into Helm's file is used without asking

Cogpit takes over every change of Helm's `repositories.yaml` within seconds (owner, D-29), from
whoever made it: `helm repo add`, an editor, or any program running as the user. A repository
added that way is listed at once, and while the Charts view or a Helm Install tab is open its index
is downloaded at once: Cogpit then sends a request, with the credentials the entry names, to a host
the user did not choose in Cogpit, and the entry's CA file decides whom Cogpit trusts there; an
entry may also name any file the user can read as its client certificate and key, which Cogpit then
presents to that host (the certificate is sent, the key never leaves the process). Its charts
appear in the Charts view beside the others and install only when the user, or an agent with the
user's acceptance, installs one. This is the same trust a kubeconfig the user keeps gets, but
without H-1's question: nothing is run, so Cogpit does not ask. Live whenever another program
writes the file.

Confirmed by the owner (2026-10-14 evening): a repository that appears in Helm's file while Cogpit
runs is downloaded at once, without a question. What remains, live whenever another program
writes the file while the Charts view or a Helm Install tab is open: the request with the entry's
credentials, CA and client certificate to a host the user did not choose in Cogpit, as above.
Bounded where [H-40](credentials.md#h-40-credentials-over-plain-http) applies: an `http://` entry
with credentials downloads nothing until the user allowed it in the window.

### H-35: kubectl's signature check rests on an embedded trusted root and Rekor v1's search

The Kubernetes release signs kubectl with sigstore's keyless signing: a key made for one signing,
certified by sigstore's certificate authority (Fulcio) for ten minutes for the release's identity,
and the signature recorded in sigstore's public transparency log (Rekor). Until 2026-10-15 Cogpit
checked the certificate's chain, the identity, the issuer and the signature over the published
SHA-256 with the standard library, but not the log. Since 2026-10-15 (the owner's decision of
2026-10-14 evening, [D-50](../developer/decisions.md#d-50-kubectls-signature-check), built in the
night run) sigstore-go checks all of it, the log included ([`verify.go`](../../internal/kubectl/verify.go),
[`rekor.go`](../../internal/kubectl/rekor.go)). The `.sig` and `.cert` files `dl.k8s.io` publishes
carry no log entry, and it publishes no sigstore bundle (verified for kubectl 1.37.1 on
2026-10-15), so each download asks `rekor.sigstore.dev` for the entry that records exactly the
published SHA-256, the signature and the certificate, as `cosign verify-blob` does; a download is
refused when the log has no such entry, cannot be reached, or its entry does not verify against the
trusted root Cogpit embeds ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json)).
Per case, what the check covers:

- **A compromised mirror or CDN** (the `dl.k8s.io` redirect, or a mirror set in Preferences) cannot
  forge the signature or its log entry: covered, since every file it serves is checked against the
  release identity and the log.
- **A leaked signing key:** closed. A signature made later with such a key needs a log entry whose
  time the log signed inside the certificate's ten minutes, which the log does not give for a later
  upload (`TestRealReleaseRefused`: a changed integrated time, a changed signed entry timestamp,
  another release's entry; `TestVerifierRefuses`: another signature's entry).
- **A misissued certificate:** narrowed. The certificate must carry a signed certificate timestamp
  of a certificate transparency log the trusted root names, so a misissued one is public where the
  release's owners can watch for it; Cogpit still accepts it when its signature is in Rekor.
  Dormant, and it needs a compromise of Fulcio or of the Google service account.

What remains, per case:

- **The embedded trusted root goes stale.** It is refreshed by `make sigstore-root` through
  sigstore's TUF repository, by hand: Renovate cannot see it. When sigstore rotates a key that a
  new release is certified, logged or timestamped under (a new Fulcio intermediate, a new CT log key,
  a new Rekor key), downloads of such releases are refused until a Cogpit release carries the new
  root: fails closed, nothing runs. Fulcio's root and intermediate in it are valid until 2031-10-05.
- **Rekor v2.** The trusted root already names sigstore's Rekor v2 log (`log2025-1.rekor.sigstore.dev`),
  which has no search: a release whose signature is logged only there cannot be found from a `.sig`
  and a `.cert`, and Cogpit refuses it ("sigstore's transparency log has no entry for it") until it
  reads the sigstore bundle such a release would have to publish. Dormant: kubectl 1.36.1's and
  1.37.1's entries are in Rekor v1 (2026-10-15); when the Kubernetes release moves is not verified.
- **No consistency check of the log.** Cogpit verifies the entry's inclusion in the tree the
  checkpoint names and the checkpoint's signature, but keeps no earlier checkpoint and asks no
  witness, so a log that shows Cogpit another tree than everyone else (a split view) is not
  detected. Dormant: it needs Rekor's signing key.
- **The lookup tells the log what the user downloads.** The digest names the release and the
  platform, and the request comes from the user's address or proxy; it carries only what the
  release publishes, no credential ([credentials](credentials.md#where-credentials-must-never-end-up)).
- **A network without access to `rekor.sigstore.dev`**, a mirror inside a closed network for
  example, can no longer download through Cogpit (before 2026-10-15 the check was offline); the
  user puts a kubectl on `PATH` or sets Preferences › Path to kubectl instead. Not a weakness of the
  check, a cost of it, recorded with D-50's decisions, open to revision.

### H-36: A shell's profile can put another kubectl first

Cogpit puts the downloaded kubectl first on the terminal's `PATH` in the environment it starts the
login shell with, and the profile runs after that. Verified 2026-10-13 on macOS 26.6:
`/usr/libexec/path_helper`, which `/etc/zprofile` and `/etc/profile` run, moves an inherited entry
behind `/etc/paths` and `/etc/paths.d` (a directory put first came out last). Per shell:

- **zsh** (macOS's default): Cogpit starts it with a `ZDOTDIR` of its own whose `.zshenv` gives zsh
  the user's `ZDOTDIR` back, reads the user's `.zshenv` and puts the directory first again before the
  first prompt, after every profile ran (`TestZshKubectlFirstAfterTheProfile`, with a `.zprofile` that
  prepends another kubectl). What remains: a `.zshrc` that empties `precmd_functions`, and a tool that
  changes `PATH` at a later prompt (direnv, asdf, a typed `export`).
- **bash** (since 2026-10-14, A26): started with `--init-file`, its rc reading the files a login
  bash reads and then putting the directory first (`TestBashKubectlFirstAfterTheProfile`). What
  remains: as for zsh, a tool that changes `PATH` at a later prompt; a Preferences shell argument
  `-l`/`--login`, which makes bash ignore the init file (then only the environment's order).
- **fish** (since 2026-10-14, A26): `-C`, which fish runs after its configuration
  (`TestFishKubectlFirstAfterTheConfig`, run where fish is installed; CI has none).
- **sh and other shells**: only the environment's order. Live on macOS whenever `/etc/paths` or a
  profile's directory holds a kubectl (Docker Desktop's `/usr/local/bin/kubectl`, Homebrew's), and
  on Linux distributions whose `/etc/profile` sets `PATH` anew. The terminal's line names the
  version Cogpit put first; `kubectl version --client` in the terminal shows which one runs.

**Enforcement** (A27, built 2026-10-14): Preferences › Kubernetes › Enforce kubectl also defines a
shell function `kubectl` (zsh, bash, fish) that runs Cogpit's choice, after removing an alias of
that name (`TestZshKubectlEnforced`, `TestBashKubectlFirstAfterTheProfile`). Only the user sets it
(the lead's night decision, [D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)). What it
leaves: a function `kubectl` a later hook defines after Cogpit's, and a call by full path.

### H-2: Local processes and forwarded ports

A forwarded port on `127.0.0.1` is reachable by every process of every local user on the machine,
with the pod's identity behind it. Binding to loopback limits it to the host, not to Cogpit. No
per-connection authentication is possible for arbitrary TCP protocols. As built (2026-10-11): a
forward lives from Start until Stop, Delete, switching context or quitting, and is not restored on
the next start; agents need the user's acceptance to start or edit one (`Mutating`, D-2) unless
agent writes are allowed for the context, and may open, stop and delete forwards freely (`UI`);
an agent that learns the URL can connect like any other local process. The URL names `127.0.0.1`
because `localhost` may resolve to `::1`, where Cogpit does not listen and another process may.
Whether another process can bind the same port on Windows (`SO_EXCLUSIVEADDRUSE`) is not
verified; kubectl has the same exposure.

### H-19: The terminal widget trusts its input

The terminal widget, `github.com/fyne-io/terminal` (Cogpit's patched copy on upstream
`46cd12724ff3` since 2026-10-12, [`third_party/fyne-terminal`](../../third_party/fyne-terminal/PATCHES.md)),
froze or crashed the whole app on output a container can send, on the main goroutine where nothing
recovers, and changed Cogpit's working directory on OSC 7; the escape filter stays in front of it.
Fuzzed on its own as decided ([D-14](../developer/decisions.md#d-14-terminal-widget), 2026-10-12):
`FuzzHandleOutput` in the copy and `FuzzRawWidget` through the public API in termview, each failing
on a panic or on input not done within 30 s, seeded with every known hazard and recordings of vim,
less, pico and screen. Found and fixed, each with a regression test (PATCHES.md lists them): a CSI
whose final character is not ASCII and a set or reset mode without `?` (both panicked; ANSI mode
20 is new-line mode again); a debug-mode panic on `ESC [ > c`; repeat, delete, insert and scroll
counts that looped once per count (a hang at the largest count); insert lines copying instead of
inserting; OSC, APC and DCS strings and long CSI parameters built quadratically (1 MiB took about
60 s on the reading goroutine; now linear and capped at 1 MiB); an APC ended by ST that swallowed
all later output; and five data races moved onto one goroutine (start-up configuration and the
input writer, the selection, the character sets, media copy, the bell), since 2026-10-15 seven
more in `RunLocalShell`, which Cogpit does not call, its directory check and `ExitCode`, with its
tests under the race detector in CI (A116). Since 2026-10-15 the copy also keeps a scrollback
of its own, at most 256 MiB per terminal, and wraps it again at a new width
([D-56](../developer/decisions.md#d-56-the-terminals-scrollback); new code on the output's path:
`history.go`, `scrollback.go`, PATCHES.md's
[Scrollback](../../third_party/fyne-terminal/PATCHES.md#scrollback)), and blinking text's ticker
stops when the terminal's renderer goes. The runs were short: about 1.0 million executions in 12
minutes on the copy and 165,000 in 10 minutes through termview on 2026-10-12, and with the
scrollback and its reflow two runs of 3 and 2 minutes (about 250,000 executions) on 2026-10-15,
with no finding left; longer runs are open. What remains: inputs the short runs did not reach;
known and unpatched (a DCS ended by ST swallows the next character, an ESC at byte 5000 of a read
is not recognised, cursor movement overflows at the largest counts, media copy buffers without a
bound, unsynchronised writes of `RunLocalShell`'s Windows start-up goroutine); its ANSI colours are
fixed, not theme tokens; and the copy must be kept in step with upstream until upstream takes the
patches: the crash, hang, OSC 7 and race fixes of 2026-10-12 are offered as four pull requests
([#155](https://github.com/fyne-io/terminal/pull/155) to
[#158](https://github.com/fyne-io/terminal/pull/158)), of which the crash, hang and OSC 7 fixes are
merged (2026-10-14 to 2026-10-17) and the race fixes wait for the maintainer's review of the
changes he asked for (pushed 2026-10-22); fixes to upstream's own scrolling ([#147](https://github.com/fyne-io/terminal/pull/147))
are offered as [andydotxyz/terminal#2](https://github.com/andydotxyz/terminal/pull/2) and Cogpit's
scrollback model as a counter-proposal on #147 (2026-10-15, D-56); the input patches of 2026-10-13
and the race fixes of 2026-10-15 are not offered.

### H-21: Access reviews guide, they do not guard

Cogpit greys and refuses what the connected user's access reviews deny, and hides the kinds they
may not list. The reviews are guidance for what Cogpit shows and offers, not an authorization
boundary: their answers are kept until a refresh or the next connect, so a role changed meanwhile
is not seen; a rules review can over-report a cluster-scoped kind that only a RoleBinding names,
which is why Cogpit confirms such answers with an access review; and Kubernetes' documentation
advises against rules reviews for authorization decisions. An unknown answer offers the command,
and the API server stays the authority for every request.

### H-24: A dropped kubeconfig can send a local file as its token

A kubeconfig names files as well as programs: `tokenFile` (and a user's `client-certificate`,
`client-key` and the cluster's `certificate-authority`). A kubeconfig dropped into a synced folder
or added by the user can pair `tokenFile: <any file the user can read>` with a server of its own;
when the user connects to that context, client-go reads the file and sends its content to that
server as a bearer token. Kubernetes' documentation names this ("file exposure",
[kubeconfig docs](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)).

**Built 2026-10-12** ([D-17](../developer/decisions.md#d-17-token-files-named-by-added-kubeconfigs),
owner, option a): a context of any kubeconfig file (since D-28, 2026-10-12, `config` and the
files `$KUBECONFIG` names included: there is no unasked default kubeconfig left) whose user names a `tokenFile`
outside the kubeconfig's folder connects only after the user allowed it, in the dialog of the
credential plugin ([H-1](#h-1-kubeconfig-files-added-by-sync-folders)), with the same approval
store and the same GUI-only rule. The dialog is titled "Allow this file as the login token?" and
names the file, the server its content goes to, how that server is reached and trusted, the
kubeconfig and the context; a context that also runs a plugin shows both in one dialog and one
approval. The approval's fingerprint binds the token file's path, the server and its trust (a
changed path or a changed server asks again, `TestTokenFileOutsideTheFolderIsAskedFor`,
`TestTokenFileOutsideTheFolderNeedsApproval`); the file's content is not bound, so a rotated token
does not ask again. `cluster.Connect` asks the policy **before** client-go builds its configuration,
because that is what reads the token file: nothing is read from a file outside the folder before
the user allowed it. A token file resolved inside the kubeconfig's folder comes from the same
source as the kubeconfig and needs no approval; a link in the folder that points outside counts as
outside (`TestTokenFileLinkedOutOfTheFolderNeedsApproval`), and the engine's refusal names the
file and the server, never the content. Since D-28 the gap no longer has a dormant corner: it
holds for `~/.kube/config` and the `$KUBECONFIG` files as for every file.

What remains, per case:

- **A broad folder.** "The kubeconfig's folder" is the directory of the file, subdirectories
  included. For a file the user added from the home directory, Downloads or a shared drive's root,
  a `tokenFile` anywhere below that directory (`~/.ssh/id_ed25519` for a kubeconfig in `~`) counts
  as inside and is not asked for. Live for added files in such directories, and for a sync folder
  only as far as its own writer can already write.
- **A link repointed after the connect.** client-go re-reads a token file about once a minute for
  the life of the connection. A link in the folder that resolved inside when the user connected and
  is repointed outside afterwards is read at the next refresh without a new question; the next
  connect asks. Live while a connection to such a context is open and the folder is writable by
  someone else.
- **The window between the check and the read.** The policy decides, then client-go opens the
  path; a swap to a link outside in that window is not caught (microseconds, write access to the
  folder at that moment).
- **Other files.** A user's `client-certificate` and `client-key` and the cluster's
  `certificate-authority` are bound by path only: the key is never sent, a certificate only in the
  TLS handshake. A CA file inside the folder is bound by content ([H-1](#h-1-kubeconfig-files-added-by-sync-folders)).
  Since 2026-10-17 Cogpit itself opens a `client-certificate` file whenever it lists the contexts,
  before any connect, without asking, and keeps only a certificate's two dates ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists);
  what that leaves: [file access › H-119](file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed)).
  The key file is still read by client-go alone, at connect.
