# Trust boundaries

Who and what Cogpit trusts, and where untrusted data crosses into trusted state: each boundary with
its checks, the package that owns them, and the code and tests that hold them.

## Roles

| Role | Trust | Why |
|---|---|---|
| Local user | trusted | Owns the desktop session, the kubeconfig files and the cluster identities Cogpit uses |
| Kubeconfig file | trusted only as much as its origin | It can name a program to run (`users[].user.exec`). Kubernetes warns: "Only use kubeconfig files from trusted sources. Using a specially-crafted kubeconfig file could result in malicious code execution or file exposure." ([kubeconfig docs](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)) |
| Credential plugin | runs with the user's privileges | It is whatever program the kubeconfig names ([client-go credential plugins](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins)) |
| Kubernetes API server | authenticated, data untrusted | client-go checks its certificate (the kubeconfig's CA or the system's, `tls-server-name`) unless the kubeconfig switches the check off ([H-37](#h-37-a-context-that-skips-the-certificate-check)); that makes no object name, annotation, event or log safe to render, execute or hand to a model |
| Workload output | untrusted | Logs, exec and attach streams come from containers, which anyone who can deploy to the cluster controls |
| Alertmanager ([D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)) | untrusted | The alerting rules' authors write its texts, workloads' metrics fill its labels, and Cogpit finds it by its Service's name ([H-121](privilege-footprint.md#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)); reached through the API server's service proxy with the user's identity, its answer fills the alerts pill, the alert board and what agents read |
| A proxy the user names (a cluster's; the Preferences' one for Helm repositories, kubectl downloads and chart icons) | trusted to forward, not to read | With the server's TLS verified it sees the server's name and the traffic's timing and size; without (`insecure-skip-tls-verify`; a plain-http or `insecure_skip_tls_verify` Helm entry, [H-41](#h-41-repositories-whose-content-nothing-authenticates)) it reads and can change everything, credentials included; client-go sends a plain-http API server no kubeconfig credential (`TestPlainHTTPServerGetsNoCredential`) |
| Helm's `repositories.yaml` and the CA, certificate and key files it names | trusted as the user's own configuration, like a kubeconfig the user keeps | The helm CLI writes it (`helm repo add`); Cogpit reads it, never writes it, and takes its changes over within seconds, from whoever made them ([H-42](#h-42-a-repository-another-program-writes-into-helms-file-is-used-without-asking)); its passwords are the user's ([credentials](credentials.md#where-credentials-come-from)) |
| Helm repositories and charts | untrusted | Remote content; a chart's templates run in Cogpit's process through the Helm SDK |
| Hosts that serve chart icons | untrusted, and told what the user looks at | An index names each chart's icon as a URL on any host; Cogpit fetches it when the window shows the chart, while Preferences › Kubernetes › Load chart icons is on ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them), [H-87](#h-87-chart-icons-tell-their-hosts-what-the-user-looks-at)) |
| A `cogpit://` link | untrusted: any web page, mail, chat or app can open one, whether Cogpit runs or not | macOS hands it to Cogpit, which only navigates with it and asks before it connects another cluster ([D-47](../developer/decisions.md#d-47-deep-links-cogpit), [H-89](#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)) |
| External agents over MCP | as trusted as the local user who connected them; decisions are a model's | [agents and MCP](agents-and-mcp.md) |
| Built-in agent and its LLM endpoint | the endpoint's output is untrusted | [agents and MCP](agents-and-mcp.md) |
| Update feed (GitHub Pages) and release host | trusted after signature verification | The feed and each update count only with the update key's EdDSA signatures, the macOS app also only with Cogpit's Developer ID ([updates](updates.md)) |
| Cogpit's APT repository (GitHub Pages, [D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)) | trusted after apt's signature check | Read by apt, never by Cogpit ([updates › Linux](updates.md#linux)) |
| kubectl release host (`dl.k8s.io`) or the mirror set in Preferences | untrusted; a binary counts once the release's signature on it and that signature's entry in sigstore's transparency log verify | A downloaded `kubectl` runs whenever the user types `kubectl` in a Cogpit terminal, with the user's privileges and credentials ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)) |
| sigstore's transparency log (`rekor.sigstore.dev`) | untrusted transport; its answer counts once its signatures verify against the log key in the trusted root Cogpit embeds | It vouches that a kubectl release's signature was published while its certificate was valid ([D-50](../developer/decisions.md#d-50-kubectls-signature-check), [H-35](#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search)) |
| The Kubernetes project's release schedule (`kubernetes/website`'s `schedule.yaml` and `eol.yaml`, [D-91](../developer/decisions.md#d-91-the-kubernetes-version-and-upstream-end-of-life)) | data at build time only | The release pipeline builds its end-of-life dates into each release, checked and bounded, never code; the app never contacts its host; a changed file shifts the dates the window marks versions by ([updates › H-130](updates.md#h-130-the-releases-kubernetes-release-table-is-fetched-unreviewed)) |

```text
UNTRUSTED                                   BOUNDARY                          TRUSTED STATE
  kubeconfig files (exec, auth-provider) ─► parse + login approval ─────────► connection settings
  proxy URL (user; agent if confirmed)   ─► URL check, password ────────────► the connection's proxy
                                            redacted outside the file
  API responses, watch events            ─► schema-tolerant decoding, ──────► cache, view-models
                                            bounded walks
  log lines, exec/attach output          ─► terminal emulator with ─────────► terminal and log views
                                            filtered escape sequences
  YAML typed by the user                 ─► parse, diff, dry-run ───────────► apply request
  image file picked as a cluster icon    ─► bounded decode, re-encode, ─────► kept icon copy
                                            SVG checked and drawn
  Helm's repositories.yaml, CA/cert/key  ─► bounded read, SDK types, ───────► repositories in memory,
                                            never written                     credentials unexported
  Helm repository index and charts       ─► Helm SDK: no plugins, ──────────► release operations
                                            no post-renderers
  chart README (from the archive)        ─► 256 KiB, cleaned, Markdown ─────► drawer, object_get
                                            without images or HTML
  chart icon (any https host)            ─► no credentials, public ─────────► kept icon copy
                                            address, bounded decode
  cogpit:// link (any page or app)       ─► strict grammar, sources' ───────► navigation only:
                                            contexts, a yes per new cluster   five commands
  MCP requests, LLM tool calls           ─► token, origin, schema, ─────────► dispatched commands
                                            access review, confirmation
  appcast, disk image or setup           ─► EdDSA over feed and download, ──► installed app
                                            Developer ID (macOS), newer only
  APT repository (Linux, apt as root)    ─► OpenPGP over the index, ────────► installed package
                                            by-hash, SHA-256, newer only      (the user's password)
  kubectl release (dl.k8s.io or mirror)  ─► GUI click, https, size bound, ──► first on PATH in
                                            SHA-256, sigstore signature       Cogpit's terminals
  Rekor's entry (rekor.sigstore.dev)     ─► its proofs against the
                                            embedded trusted root (sigstore-go)
```

## Boundaries and required checks

| Boundary | Package | Required checks |
|---|---|---|
| Kubeconfig to connection | `internal/kubeconfig`, `internal/cluster` | Parse without executing; read each source file on its own, never merged, so a dropped file cannot point a context at the user's real credentials. A login that needs the user's approval ([login approval](#login-approval)) is checked by `cluster.Connect` before any client exists, from the very `rest.Config` the client runs (`checkExecPolicy` in [`sources.go`](../../internal/cluster/sources.go); `TestConnectAsksTheExecPolicyBeforeThePluginRuns`, `TestExecPluginRunsOnlyOnceAllowed`, `TestKindExecPluginAfterApproval`). client-go checks the server's certificate as the cluster entry says (`certificate-authority(-data)`, `tls-server-name`: `TestConnectTrustsTheKubeconfigCA`, `TestConnectWithTLSServerName`); a failed check names the kubeconfig field to look at, never a value ([`tls.go`](../../internal/cluster/tls.go)); a `proxy-url`'s user information never reaches a connect error (`TestKubeconfigProxyPasswordNeverInErrors`, [credentials](credentials.md#where-credentials-must-never-end-up)) |
| Proxy setting to connection | `internal/engine`, `internal/cluster` | Set by the user in the window or by an agent's write the user always confirms, its password `•••` ([agents and MCP](agents-and-mcp.md#rules)); the CLI is refused (`ErrProxyGUIOnly`) and may only clear it ([`engine/proxy.go`](../../internal/engine/proxy.go), `refuseProxy` in [`commands/proxy.go`](../../internal/engine/commands/proxy.go), `TestClusterProxyCommands`). An `http`, `https` or `socks5` URL with a host, an optional port and `user:password@`, nothing else (`CheckProxyURL`); a cluster's proxy applies to its own context at the next connect, through `rest.Config.Proxy` ([`cluster/proxy.go`](../../internal/cluster/proxy.go)); its password stays in the settings file, `•••` everywhere else ([credentials](credentials.md#where-they-live-inside-cogpit)); a credential plugin's approval binds it ([login approval](#login-approval)) |
| API response to cache | `internal/cache`, `internal/model` | The kind profiles read unstructured content and never panic on an unknown or missing field (fuzzed per kind: `FuzzPods`, `FuzzNodes` and the others in [`model/kinds`](../../internal/model/kinds/fuzz_test.go)); a related-objects walk follows a fixed chain of kinds per workload kind, never an object's own references (`controlled` in [`engine/relatedrefs.go`](../../internal/engine/relatedrefs.go)). Not built: a cap on an object's or a list's size |
| Cluster text to UI | `internal/model`, `internal/ui/fyneui` | Fyne's text widgets draw cluster text and render no markup; Markdown only for a chart's README and the built-in agent's answers, images never loaded ([`helmchartdrawer.go`](../../internal/ui/fyneui/helmchartdrawer.go), [`chat_markdown.go`](../../internal/ui/fyneui/chat_markdown.go)). Control characters are handled where a row here or another page names it (the terminal and log views, a certificate's texts, the approval dialog, links; a Secret value holding one is shown by its size: [`engine/secretdata.go`](../../internal/engine/secretdata.go)). Not built: stripping them from a list's cells |
| Container output to terminal | `internal/terminal`, `internal/ui/fyneui/termview` | The escape filter drops every OSC, DCS, APC, PM and SOS string (OSC 52's clipboard write and window titles among them), media copy and window operations, and clamps large counts ([`termview/sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go), fuzzed); log lines lose escapes and control characters, capped at 64 KiB a line, 10,000 lines and 32 MiB ([`termview/logs.go`](../../internal/ui/fyneui/termview/logs.go)); a terminal's scrollback holds at most 256 MiB per tab ([D-56](../developer/decisions.md#d-56-the-terminals-scrollback), `history.go` in [the widget's copy](../../third_party/fyne-terminal/PATCHES.md#scrollback), `TestScrollback_Limit`). The widget behind the filter: [H-19](#h-19-the-terminal-widget-trusts-its-input) |
| Editor to cluster | `internal/engine` | Save shows the diff and the API server's dry-run verdict before Apply (`TestEditTabSaveReviewsThenApplies`); `object.apply` sends the update with `dryRun=All` first, then for real, with the tab's base resourceVersion as precondition ([`engine/editor.go`](../../internal/engine/editor.go), [privilege footprint](privilege-footprint.md#actions-that-write)); a text whose apiVersion, kind, name or namespace differs from the tab's object is refused, never applied as another object (`editedObject`, `TestEditRefusesWhatItMustNot`) |
| Port-forward to local socket | `internal/cluster` | `ListenLoopback` binds `tcp4` 127.0.0.1 and refuses a listener that ends up elsewhere ([`portforward.go`](../../internal/cluster/portforward.go), `TestListenLoopbackBindsIPv4LoopbackOnly`); a forward is shown with its address, pod and port; a pod's error text reaches the GUI or an agent as one line of at most 512 bytes without control characters; the URL names `127.0.0.1`, not `localhost`, which macOS resolves to `::1` first, where another process may listen (`TestForwardURLReachesTheForward`), and the GUI opens only such URLs; a 404, 403 or 401 fails the forward at once and closes its listener ([D-20](../developer/decisions.md#d-20-a-port-forward-that-cannot-connect), `TestPortForwardFailsAtOncePermanentErrors`). What loopback leaves open: [H-2](#h-2-local-processes-and-forwarded-ports) |
| Image file to cluster icon | `internal/engine` | Only the user's pick in the window reads a file (`ErrIconGUIOnly`), at most 1 MiB; a PNG or JPEG's size is checked from its header (at most 8,192 pixels a side and 16 megapixels) before Go's decoders read a pixel; an SVG (at most 256 KiB) must pass `checkSVG` (one well-formed `<svg>` document of bounded depth and token count; no DOCTYPE or other directive, no processing instruction but the XML declaration, no `<script>`, `<foreignObject>` or animation element, no event handler attribute, no reference but to a fragment of itself) and is drawn into a PNG, a renderer panic failing that image only ([`clustersettings_icon.go`](../../internal/engine/clustersettings_icon.go), `TestCheckIcon`, `TestClusterIconUpload`). Kept are the picked image as the icon's source, a PNG of at most 1,024 pixels a side, and the icon cut from it, a 256-pixel PNG ([D-119](../developer/decisions.md#d-119-the-cluster-icon-designer), [`clustersettings_iconcut.go`](../../internal/engine/clustersettings_iconcut.go), `TestIconSource`, `TestClusterIconCutAgain`), owner-only in `<user config dir>/Cogpit/cluster-icons/` under their SHA-256 and checked again when read back (a regular file of at most 1 MiB for the icon and 4 MiB for the source, a PNG within its side), so a file someone else put there is never decoded as is |
| Helm's repositories file to repositories | `internal/helm`, `internal/engine` | The helm CLI's path ([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml), `RepositoryConfigPath` in [`repofile.go`](../../internal/helm/repofile.go)), resolved in `cmd/cogpit` only; read once its size, modification time or mode changed and stayed so for 250 ms: a regular file (a link to one followed) of at most 1 MiB, parsed with the Helm SDK's `repo.File`; a file that does not parse keeps the last good list, its error naming a line, never the content (`TestReadRepositoryFileRefusals`, `TestHelmRepoFileErrors`); an entry counts only with a name Cogpit can list, an http or https URL with a host and absolute CA, certificate and key paths, once per name, among the first 256 (`TestReadRepositoryFile`); those files are read at each download, regular files of at most 1 MiB, their errors naming the path only (`TestClientCertificateRepository`); Cogpit writes nothing into the file or Helm's folders (`TestNoCommandWritesHelmsFile`, `TestHelmReposFromHelmsFile`). Not checked: who wrote the file ([H-42](#h-42-a-repository-another-program-writes-into-helms-file-is-used-without-asking)) |
| Chart to release | `internal/helm` | The Helm SDK renders in-process, without Helm plugins, post-renderers (both run external programs) or DNS lookups ([architecture §8](../developer/architecture.md#8-helm-internalhelm)). A repository is https with its certificate checked against the system's roots or its entry's CA file, unless the entry says `insecure_skip_tls_verify` or plain http, marked wherever its charts show or install ([H-41](#h-41-repositories-whose-content-nothing-authenticates)); an https repository's downloads never go to http, by a redirect or an index's chart URL (`TestRepositoryRefusesWhatDoesNotFit`, `TestPlainHTTPRepository`); its credentials go to its own scheme, host and port only, unless `pass_credentials_all` (`TestCredentialsStayOnTheRepositoryHost`); downloads use the Preferences' proxy, else the environment's; an index (32 MiB) and a chart archive (16 MiB) are bounded and parsed by the SDK (`FuzzLoadIndex`); an archive must match the SHA-256 its index lists, if it lists one, and hold the chart and version the index names; a library chart and a chart missing its dependencies are refused; an install is previewed with the SDK's server-side dry run, which writes nothing (`TestKindHelmInstall`). An upgrade to a version a repository lists downloads through the same checks before anything is recorded, refuses a chart whose name is not the release's (`TestUpgradeToARepositoryChart`, `TestHelmUpgradeRepositoryRefusals`) and has no preview. Provenance is not checked ([H-30](#h-30-chart-provenance-is-not-verified)) |
| Chart README to the drawer and agents | `internal/helm`, `internal/engine`, `internal/ui/fyneui` | Read from the version's archive as the install reads it, only when the drawer shows the chart or an agent reads it; at most 256 KiB kept, invalid UTF-8 replaced, control characters but tab and line feed and the bidirectional embeddings, overrides and isolates dropped (`CleanReadme`, `FuzzCleanReadme`); cached only when the index's digest verified the archive, checked again when read back (`TestReadmeStoreBounds`). The window renders it as Markdown without images (their alt text instead, so no README names a host Cogpit fetches from) or raw HTML, a link opening only on the user's click and only for an absolute http or https URL (`TestChartDrawerReadme`). Agents read it framed as the chart author's text, not instructions (`ReadmeFrameStart` in [`engine/agentview.go`](../../internal/engine/agentview.go), `TestHelmChartReadmeForAgents`, [H-86](agents-and-mcp.md#h-86-a-charts-readme-reaches-agents)) |
| Chart icon to the window | `internal/helm`, `internal/engine`, `internal/ui/fyneui` | Fetched only for a row or drawer the window draws, while Preferences › Kubernetes › Load chart icons is on, which agents and the CLI may only turn off; agents fetch none ([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them), [`helm/icon.go`](../../internal/helm/icon.go), [`engine/helmicons.go`](../../internal/engine/helmicons.go)); https only, a redirect too; no credentials, cookies or referrer; at most 256 KiB and 20 s; a host at a loopback, private, link-local or other non-public address is refused unless it is a chart repository's own, and the checked address is dialled (`TestFetchIconRefusesPrivateAddresses`); through the Preferences' proxy; the bytes are checked by `CheckIcon` (a PNG or JPEG written again as a PNG of at most 256 pixels a side, an SVG kept only when it refers to nothing outside itself, anything else refused) and again when read from Cogpit's cache (`TestChartIconRefusals`); an SVG is drawn as [H-88](#h-88-chart-icons-are-decoded-in-cogpits-process) says |
| Alertmanager's answer to the pill, the board and agents | `internal/alerting`, `internal/engine`, `internal/ui/fyneui` | `GET` only ([D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)); the answer bounded (16 MiB, 10,000 alerts, an annotation 4 KiB, 64 labels of 1 KiB) and read as Alertmanager's JSON list or refused (`TestDecodeAlerts`); a failure is a class or the API server's own message, never the service's body (`TestErrorText`); its texts are shown as text, never markup; an object a label names opens only Cogpit's own views, resolved against the kinds the cluster serves; a runbook URL is kept only when it is absolute http or https without user information (`webURL` in [`engine/alerts_board.go`](../../internal/engine/alerts_board.go), `TestNoAlertmanager`) and opened in the browser only on the user's click, checked again in the window (`openRunbook` in [`alertboard.go`](../../internal/ui/fyneui/alertboard.go)); the alerts reach agents as data ([H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)) |
| Request to command | `internal/engine` | `Dispatch` ([`engine.go`](../../internal/engine/engine.go)) validates the parameters against the schema and refuses unknown fields, refuses a GUI-only command to every other origin, then `Command.Enabled` asks the connected user's cached access reviews about every declared verb whenever the parameters name an object: a known denial refuses the command for every origin ("not permitted: you may not …"), an unknown answer passes and the API server decides ([`access.go`](../../internal/engine/access.go)); then the confirmation by origin ([agents and MCP](agents-and-mcp.md)). The reviews guide, they do not guard: [H-21](#h-21-access-reviews-guide-they-do-not-guard) |
| Link to navigation | `internal/engine`, `cmd/cogpit`, `internal/ui/fyneui` | Only the macOS app registers the scheme ([D-47](../developer/decisions.md#d-47-deep-links-cogpit), [`engine/deeplink.go`](../../internal/engine/deeplink.go)); the Apple event handler passes at most 2,049 bytes of a link to a queue of at most 8, which drops a link over 2,048 bytes (`TestDeepLinkAppleEvents`, `TestDeepLinkQueue`); a link is parsed strictly or refused whole: `cogpit://open` with the parameters `context`, `view`, `namespace` and `name` only, each once, not empty, without control or format characters, in its form and bounds (`TestParseDeepLinkRefusals`, `FuzzParseDeepLink`); its context must be a kubeconfig source's (or a development build's demo cluster, [D-67](../developer/decisions.md#d-67-the-demo-cluster-stays-out-of-releases)), never a file, and one whose login waits for the user's approval is refused (`TestDeepLinkRefusals`); before it connects another cluster than the shown one, or any while none is shown, it asks the user once, naming the context and what it opens (`askLinkConnect`, `TestDeepLinkAsksBeforeItConnects`, [H-89](#h-89-a-link-switches-the-cluster-and-the-view-under-the-user)); what it asks for runs as the navigation commands of `DeepLinkCommands` (`settings.close`, `cluster.connect`, `ui.setNamespace`, `ui.openView`, `ui.navigate`) with origin `deeplink`, and `Dispatch` refuses that origin every other command and any invocation that is not a UI or read command, before the parameters decode and again after `Escalate`, so no write is reachable from a link (`TestDeepLinkCommandsOnlyNavigate` over the whole registry, `TestDeepLinkGuardsAnAlteredCommand`); every link is audited (origin `deeplink`) and said in a toast, the window raised |
| MCP request to engine | `internal/mcp` | Loopback only, bearer token, `Host` and `Origin` checks: [`internal/mcp/http.go`](../../internal/mcp/http.go), `TestHTTPGuard`, `TestNewRefusesNonLoopbackAddresses` |
| Cluster data to the LLM endpoint | `internal/agent` | A turn's tool results pass the filter that replaces the cached Secrets' values, as every agent's reads do, unless the user marked the connection private ([D-10](../developer/decisions.md#d-10-built-in-agent), [agents and MCP › H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows), `TestTurnKeepsSecretValuesFromTheEndpoint`); each user message in the chat shows what its turn sent (requests, bytes, tokens, the endpoint's host) and opens the last request body as sent (`SentPayload`, `TestChatSentPayload`) |
| The LLM endpoint's certificate to the chat | `internal/agent`, `internal/ui/fyneui` | An `https://` endpoint's certificate is verified against the system's roots (on macOS and Windows through the platform verifier) or, when the user named a CA file for the connection in Preferences › Agent, against that file alone, read whenever a request is sent ([D-10](../developer/decisions.md#d-10-built-in-agent), [`agent/cafile.go`](../../internal/agent/cafile.go); `TestCAFileTrustsAPrivateCA`, `TestCAFileReplacesTheSystemRoots`, `TestCAFileRefusesAPublicServer`, `TestCAFileErrorsAreNamed`); no redirect is followed. A plain `http://` endpoint on another machine is allowed and warned about in Preferences › Agent and the connection test ([credentials › H-95](credentials.md#h-95-an-llm-api-key-over-plain-http)). What the CA file leaves open: [H-104](#h-104-a-connections-ca-file-replaces-the-systems-roots-checked-by-gos-verifier) |
| Download to installed package (Linux) | apt; `internal/update` (`apt_linux.go`) | apt checks the repository index's OpenPGP signature with the key in `/etc/apt/sources.list.d/cogpit.sources`, the index files by hash and every package's SHA-256, and upgrades only to a newer version ([D-108](../developer/decisions.md#d-108-cogpit-for-linux-a-deb-and-an-apt-repository-on-pages)); Cogpit reads `apt-cache policy` and, on the user's click in the window, runs `/usr/libexec/cogpit/update` through pkexec, which polkit gates with an administrator's password ([updates › Linux](updates.md#linux)) |
| Download to installed app (macOS, Windows) | `internal/update` (Sparkle 2, WinSparkle) | Sparkle checks the feed's and the disk image's EdDSA signatures with the key in the app's `Info.plist` before extracting anything, then the new app's Developer ID against the running one's, and offers only a newer version ([D-4](../developer/decisions.md#d-4-packaging-and-updates)); the app loads Sparkle only from its own installed bundle (`TestSparkleLoadsFromTheBundle`). On Windows Cogpit checks the feed's signature and WinSparkle the setup's, with the same key ([updates › Windows](updates.md#windows)) |
| kubectl download to a terminal's `PATH` | `internal/kubectl`, `internal/engine`, `internal/terminal` | Only the user's click in the window downloads (`kubectl.download` refuses agents and the CLI before anything is fetched, `TestKubectlDownloadCommand`), and only the user sets the path to kubectl, the directory for binaries and the mirror (`ErrKubectlGUIOnly`, [D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries)); the mirror is an https URL without user information, query or fragment, and redirects are followed to https only (`TestCheckMirror`, `TestDownloadHTTPSOnly`). The `.sha256`, `.sig` and `.cert` files (16 KiB each at most) come first, and before the binary is fetched sigstore-go ([D-50](../developer/decisions.md#d-50-kubectls-signature-check), [`verify.go`](../../internal/kubectl/verify.go)) checks offline that the certificate's key signed the published digest (`ParseSigned`), fetches from `rekor.sigstore.dev` (same client and proxy, 30 s, 64 KiB) the entry of exactly that digest, signature and certificate ([`rekor.go`](../../internal/kubectl/rekor.go)), and verifies against the embedded trusted root ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json)) the entry's inclusion proof up to a signed checkpoint, its signed entry timestamp inside the certificate's ten minutes, the certificate's chain to Fulcio with one CT log's signed certificate timestamp, and the identity `krel-staging@k8s-releng-prod.iam.gserviceaccount.com` with the OIDC issuer `https://accounts.google.com` (`TestRealReleaseSignature` on kubectl 1.36.1's and 1.37.1's published files, `TestRealReleaseRefused`, `TestVerifierRefuses`, `TestDownloadRefusesBadSignatures`); a missing entry, no answer or a failed check refuses the download, with no fallback (`TestDownloadWithoutTheLog`). The binary (at most 128 MiB, `TestDownloadSizeBound`) goes into a temporary file beside its place, must have the published digest (`TestDownloadRefusesTamperedBinary`), and is made 0755 and renamed into `<directory for binaries>/<version>/kubectl` (directories 0700); nothing is left on failure. It goes first on `PATH` in Cogpit's terminals only, as a link in the terminal's own temporary directory (on Windows by its own folder), never in a shell profile or a global location (`TestStartLocalKubectlFirst`). What the check rests on: [H-35](#h-35-kubectls-signature-check-rests-on-an-embedded-trusted-root-and-rekor-v1s-search); how each shell keeps it first, and what a profile can still do: [H-36](#h-36-a-shells-profile-can-put-another-kubectl-first) |

## Login approval

A source's login needs the user's approval when it runs an exec credential plugin, reads a
`tokenFile` outside the kubeconfig's folder (its directory, subdirectories included;
[D-17](../developer/decisions.md#d-17-token-files-outside-the-kubeconfigs-folder),
[H-24](#h-24-a-dropped-kubeconfig-can-send-a-local-file-as-its-token)) or goes to a cluster with
`insecure-skip-tls-verify` ([H-37](#h-37-a-context-that-skips-the-certificate-check)):
`ExecPlugin.Kind` in [`kubeconfig/exec.go`](../../internal/kubeconfig/exec.go). Until the user
allowed exactly that login in the window, the engine refuses the connect for every origin
(`KubeconfigSources.Policy` in [`engine/kubeconfigs.go`](../../internal/engine/kubeconfigs.go)).

- **Sources**: every context comes from a file read on its own
  ([D-28](../developer/decisions.md#d-28-the-default-kubeconfig-as-sources-as-lens-has-it)): the
  files and sync folders the user added (a folder's files, hidden ones included, not its
  subfolders), `~/.kube` synced by default with `config` among its files, and the files
  `--kubeconfig`, else `$KUBECONFIG`, names, as sources of the session (removable until the next
  start, their approvals kept). The engine takes the default folder and the session files as
  options, so tests never read the real ones
  (`TestNoDefaultFolderWithoutTheOption`, `TestConfigWithAPluginAsksForApprovalOnce`,
  `TestDefaultKubeconfigIsOneFileOfTheSyncedFolder`).
- **The dialog** comes before a connect from the window (the navigator's section, the hotbar, the
  welcome page: [D-57](../developer/decisions.md#d-57-choosing-a-cluster)) while the login has no
  approval or one that does not match it. It shows the program as client-go will run it (a
  relative path resolved against the kubeconfig's folder) and where `PATH` finds it now, each
  argument on its own line, the environment by names only (a warning for `PATH`, `LD_*` and
  `DYLD_*`), the server the credential goes to and how it is reached and trusted (certificate
  check off, a proxy, a CA the kubeconfig brings: each a warning line; a TLS server name), whether
  the program or CA file lies inside the folder with the digest the approval binds, the file, the
  context and its source, and whether it changed since it was allowed; all of it escaped (control
  characters, bidirectional overrides, invalid UTF-8) and capped
  (`TestApprovalDialogShowsProxyAndInsecure`, `TestApprovalDialogEscapesUntrustedText`). "Allow
  and Connect" arms after 750 ms; no button has the focus.
- **The approval** is kept per file and context in `<user config dir>/Cogpit/exec-approvals.json`
  (0600, written atomically, never through a symlink), bound to `ExecPlugin.Fingerprint`: the
  file, context and user entry, the server URL, its CA data and file, the insecure flag, the TLS
  server name, the kubeconfig's `proxy-url` and Cogpit's own proxy for the context, the plugin's
  API version, command, arguments and environment (names and values, in order), interactive mode
  and `provideClusterInfo`, a token file that needs approval, and the SHA-256 of a program or CA
  file that resolves inside the kubeconfig's folder
  ([D-26](../developer/decisions.md#d-26-binding-a-credential-plugins-approval-to-its-program)).
  A program resolved elsewhere (`PATH`, `/usr/local/bin`) is bound by its path, so a tool update
  does not ask again (`TestProgramOutsideTheFolderIsBoundByPathOnly`); a link in the folder is
  bound by its target too; on Windows the program bound is the file `os/exec` runs (a path without
  one of `PATHEXT`'s extensions runs as the first match beside it, so `plug` binds `plug.exe`:
  `TestProgramIsBoundByTheFileThatRuns`,
  [D-111](../developer/decisions.md#d-111-cogpit-for-windows-an-nsis-setup-and-winsparkle)). Only
  regular files are hashed, at most 32 MiB for a program and 1 MiB for a CA file; a larger or
  unreadable one is bound by a note (a large one's size and modification time). At connect time
  the files are read again right before the policy decides
  (`TestExecApprovalRefusedAfterPluginBinaryChanged`, `TestCAFileSwapAsksAgain`). An unreadable
  approvals file allows nothing; removing a source forgets its approvals and closes a connection
  made from it (the GUI asks first, naming the path; the files stay on disk).
- **A token file** outside the folder is asked as "Allow this file as the login token?", naming the
  file, the server, how it is reached and trusted, the kubeconfig and the context; a context that
  also runs a plugin asks once for both. `cluster.Connect` asks before client-go reads the file
  (`checkTokenFilePolicy` in [`sources.go`](../../internal/cluster/sources.go)). The approval binds
  the path, the server and its trust, not the content, so a rotated token does not ask again
  (`TestTokenFileOutsideTheFolderIsAskedFor`, `TestTokenFileOutsideTheFolderNeedsApproval`); a link
  in the folder that points outside counts as outside
  (`TestTokenFileLinkedOutOfTheFolderNeedsApproval`); a refusal names the file and the server,
  never the content.
- **Cogpit's proxy** for the context, which replaces the kubeconfig's `proxy-url` (shown beside it
  as not used), is shown and bound from the same read of the setting the connection uses, so a
  proxy set after the approval stops the plugin before it runs
  (`TestPluginApprovalBindsTheClusterProxy`, H-34). It is bound as shown, its password `•••`: a new
  password for the same user, host and port does not ask again. A context that needs no approval
  asks nothing when its proxy changes; with the certificate checked, the proxy sees only the
  server's name and the traffic's timing and size. The environment's `HTTPS_PROXY`, which client-go
  uses when neither names a proxy, is neither shown nor bound.
- **Once per file and plugin** (D-28): a file's contexts whose logins are the same plugin
  (`ExecPlugin.Identity`: command, arguments, environment, API version, interactive mode,
  `provideClusterInfo`, program binding, token file) share one dialog that lists every context not
  allowed yet, each with its own server and trust, and one click allows them all. The approving
  command takes the `(context, fingerprint)` pairs the dialog showed, checks each against the file
  as it is then, refuses the whole list if any changed or left, and stores one approval per context
  with its own fingerprint (`TestApprovalIsRefusedAsAWholeWhenOneContextChanged`); a context added
  later asks on its own (`TestAContextAddedLaterAsksOnItsOwn`). An unchecked certificate alone is
  asked about per context, never bundled; a context that also runs a plugin or reads a foreign
  token file gets the plugin's dialog, which warns about the check, and one approval covers both.
- **An unchecked certificate** (`kubeconfig.LoginInsecure`) is asked as "Connect without checking
  the certificate?", naming the server and how it is reached; the yes binds the fingerprint above,
  not the credential (`TestInsecureContextAsksOnceBeforeItsFirstConnect`,
  `TestConnectAsksThePolicyAboutAnUncheckedCertificate`). Such a context is marked from the
  kubeconfig before it connects (`ContextInfo.Insecure`: the welcome page's row,
  [`clusterrow.go`](../../internal/ui/fyneui/clusterrow.go); a warning icon on its navigator row
  with the line in its tooltip, `navContextRow.insecure` and `navContextRow.tipText`;
  `insecure: true` in `cluster.contexts`; "not checked" in `cogpit-cli contexts`:
  `TestClusterRowsMarkAnInsecureContext`, `TestNavigatorSectionTooltipLines`,
  `TestContextsMarksAnInsecureContext`) and, from the connection's own configuration
  (`Conn.Insecure`), while connected: `connection.tlsUnchecked` in the UI state (`ui_state`,
  `cluster_connect`'s and `hotbar_openCluster`'s results), "TLS not checked" beside the context in
  the status bar, a WARN line in the connection log from
  [`engine/tlswarning.go`](../../internal/engine/tlswarning.go), one line on `cogpit-cli get`'s
  standard error (`TestStatusBarSaysTLSNotChecked` with PNG
  [dark](../../internal/ui/fyneui/testdata/statusbar_tls_unchecked_dark.png) and
  [light](../../internal/ui/fyneui/testdata/statusbar_tls_unchecked_light.png),
  `TestInsecureContextIsMarkedAndLogged`, `TestGetWarnsAboutAnUncheckedCertificate`).
- **Who.** Adding a file or a folder, removing a source and allowing a login are GUI-only
  (`kubeconfig.addFile`, `kubeconfig.addFolder`, `kubeconfig.remove`, `kubeconfig.approveExec`:
  `ErrGUIOnly`, `TestKubeconfigCommandsAreGUIOnly`): no confirmation could show the user an
  agent-chosen path for review. An agent's or the CLI's connect to a context that waits ends in an
  error naming the window (`TestInsecureContextConnectsPerOrigin`,
  `TestGetRefusesAnUncheckedContextUntilAllowed`). Agents read the pending approvals
  (`kubeconfig.pendingApprovals`) without the plugins' arguments
  (`TestPendingApprovalsAreBundledAndApprovedByTheGUIOnly`).
- **`cogpit-cli`** builds the same sources ([`internal/app`](../../internal/app/app.go)) and
  connects through the same `ExecPolicy` (`TestGetRunsNoPluginTheUserHasNotAllowed`); it writes
  neither the sources nor the approvals, refuses to connect without a configuration directory
  (`TestNoConfigDirNoConnection`), and escapes context and file names before they reach the
  terminal (`TestContextsListsTheSourcesWithoutSecrets`).

## What this does not cover

### H-104: A connection's CA file replaces the system's roots, checked by Go's verifier

An LLM connection's CA file is that connection's only root pool (`x509.NewCertPool` plus
`AppendCertsFromPEM` in [`agent/cafile.go`](../../internal/agent/cafile.go)): Go hands only a pool
marked as the system's to the platform verifier, so its own verifier checks the connection's
chains, and a public CA cannot vouch for the endpoint (`TestCAFileRefusesAPublicServer`). Live for
every connection with a CA file:

- the file vouches for every certificate its CAs signed, so a broad CA in it (a TLS-inspection CA,
  a whole bundle) vouches for whatever it signed at the base URL's host (no redirects are followed);
- Go checks validity, host name, key usage and name constraints, but no revocation (OCSP, CRL), no
  Certificate Transparency and none of the system's trust settings: a CA marked "Never Trust" in
  Keychain Access is still trusted through the file;
- a process of the user that rewrites the file, together with a place on the network path, can
  answer as the endpoint and read the key, the conversation and what the agent reads; the file is
  read anew for each request, so the change goes unnoticed. Dormant unless both hold, as for a
  kubeconfig's `certificate-authority` file;
- `SSL_CERT_FILE` or `SSL_CERT_DIR` in Cogpit's environment make Go on macOS and Windows load the
  system's roots from them instead of asking the platform verifier (unless
  `GODEBUG=x509sslcertoverrideplatform=0`), for clusters, Helm repositories and chat connections
  without a CA file. An app started from the Finder or the Dock inherits no shell variables; one
  started from a terminal does. How Cogpit behaves then is not checked beyond Go's documented rule.

### H-89: A link switches the cluster and the view under the user

Any web page, mail or app can open a `cogpit://` link: within the shown cluster it changes the view
and opens a drawer under the user at once; after one yes it connects another cluster, where an
approved credential plugin runs (an OIDC login may open a browser). Bounded by: the window comes to
the front and a toast names what opened; the shown cluster's tabs stay; a write the user confirmed
for another cluster refuses to run (`CheckConfirmedContext`); a plugin not allowed, or changed
since, never runs from a link; a page that opens links in a loop raises questions, not connections.
Browsers usually ask before they hand a link to an app; a mail or chat app may not (not verified per
app). Accepted with D-47: navigation only. Not decided: a setting that turns links off.

### H-90: A link's text reaches agents through the logs

Agents read the audit and notification logs (`audit_list`, `notifications_list`), and every link
writes into both: its parameters (a context of up to 512 bytes of any printable text, refused or
not, a view key, a namespace, an object name) or at most 256 escaped bytes of one that does not
parse, and a toast quoting up to 200 bytes of a context that names none of the user's or 64 of
another refused value. So a web page can put a short text of its choice where an agent reads: a
channel for [H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data), live whenever links
reach Cogpit and an agent reads those logs. Bounded by: control and format characters are escaped or
refused; the view, the namespace and the name have narrow forms (`[a-z0-9.-/]`, a DNS label,
`[A-Za-z0-9._:@+-]`). Not built: leaving a refused link's text out of what agents read.

### H-1: Kubeconfig files added by sync folders

A file that appears in a synced folder (a download, a shared drive, a tool writing into `~/.kube`)
becomes a set of contexts whose logins wait for the user's approval
([login approval](#login-approval)). What the approval leaves:

- kubectl in a Cogpit terminal reads the source file again, so a plugin changed after the approval
  runs there unasked (the user's own command);
- content binding stops at the file itself (accepted, D-26): a program or CA file outside the
  kubeconfig's folder is bound by its path, since whoever can write to a `PATH` directory already
  controls the account (Cogpit's `PATH` also has its built-in and the user's program folders
  appended: [D-120](../developer/decisions.md#d-120-folders-searched-for-credential-plugins),
  [H-6](credentials.md#h-6-exec-plugins-inherit-the-environment)); what a script in the folder
  sources or imports, an interpreter's libraries and a binary's own configuration are not bound;
- a program swapped after the policy's read and before client-go starts it, and swapped back, is
  not caught (microseconds, and write access at that moment);
- without a configuration directory (`os.UserConfigDir` fails, as without `$HOME`) the app has no
  sources and merges the files as kubectl does (`openKubeconfigSources` in
  [`cmd/cogpit/main.go`](../../cmd/cogpit/main.go)): plugins run, token files are read and
  unchecked contexts connect without asking; `cogpit-cli` refuses to connect. Dormant.

### H-101: A credential plugin runs again during a connection without being checked again

During a connection client-go's exec authenticator runs the plugin again by itself, by its path as
it is then: when the credential expires (its `expirationTimestamp`) and on a 401 Unauthorized.
These runs are not checked against the approval, so a program inside the kubeconfig's folder
replaced after the connect runs at the next renewal without the dialog. Live for the shown cluster
and for a cluster not shown whose "Renew the login while the cluster is not shown" is on (the
default); it needs write access to the folder during the connection. Bounded by: Cogpit's own
renewals (at wake, the reconnect after an interruption, Log in again:
[D-58](../developer/decisions.md#d-58-standby-and-lost-connections)) check the approval first and
ask for a new connect (`Connection.NeedsApproval`); the login guard hands client-go only the first
401 after a request that worked ([`cluster/health.go`](../../internal/cluster/health.go)); a cluster
not shown with that setting off runs no renewal
([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once),
[`cluster/loginhold.go`](../../internal/cluster/loginhold.go), `TestHeldPluginDoesNotRun`). Closing
it needs Cogpit to run the plugin itself, or the hold's wrapper around client-go's unexported
`interactiveFunc` to check the approval before each run, depending further on client-go's
internals, as the hold and [D-66](../developer/decisions.md#d-66-the-connection-log)'s capture do.
Not built.

### H-37: A context that skips the certificate check

With `insecure-skip-tls-verify: true` whoever answers at the server's address (a machine on the
network path, a spoofed DNS answer, a proxy) gets the context's credential
(`TestInsecureSkipTLSVerifyConnectsToAnUntrustedServer`): a bearer token (inline, a token file, an
exec plugin's or the legacy `oidc` provider's) in full, usable against the real cluster until it
expires, with a relaying impostor reading and changing everything in between; a client
certificate's key never leaves Cogpit, so an impostor can only show made-up data and swallow the
user's writes. After the user's one yes ([login approval](#login-approval)) every connect hands the
credential over. Live for every such context, common for test clusters and for servers reached by
an address their certificate does not name, where `tls-server-name` is the fix the connect error
points at. Agents and `cogpit-cli mcp` learn it from `insecure` and `connection.tlsUnchecked`, not
from a warning of their own. Not decided: refusing such contexts.

### H-30: Chart provenance is not verified

The digest a chart archive is checked against comes from the repository's own `index.yaml` on the
same host: it catches a corrupted download or an index and archive that do not belong together, not
a repository that serves a malicious chart with a matching digest. Helm's provenance files
(`<chart>-<version>.tgz.prov`, an OpenPGP signature over the chart's digest,
`helm install --verify`) are not checked, and Cogpit keeps no keyring. Such a chart's templates run
in Cogpit's process (`lookup` reads what the user may read) and its objects are created with the
user's identity after the preview and, for an agent, the user's acceptance; an upgrade to a
repository's version does the same without a preview. Live for every chart from a repository. Not
designed: which keys to trust and where they would live.

### H-87: Chart icons tell their hosts what the user looks at

While Preferences › Kubernetes › Load chart icons is on, which it is by default
([D-46](../developer/decisions.md#d-46-chart-icons-from-the-hosts-that-serve-them)), every host an
index names for an icon learns from a request, at most once a week per URL, that this computer runs
Cogpit (the user agent), when, and which chart's row or drawer the window shows; scrolling a large
repository asks the hosts of every icon in view, and a hostile index can name a URL per user to
track who opens which chart. Through a proxy (Preferences › Proxy or `HTTPS_PROXY`) the proxy
resolves the host and the non-public address check does not apply: a proxy that reaches the user's
network carries a request to any host there an index names (a GET without credentials, cookies or a
body, its answer read only as an image). Not built: a list of hosts the user trusts for icons, icons
from the repository's own host only (D-46's option (c)), resolving the host before handing a request
to a proxy.

### H-88: Chart icons are decoded in Cogpit's process

With chart icons on, any host an index names supplies bytes Cogpit decodes: a PNG or JPEG with Go's
image packages once its header passed the bounds (16 megapixels, 8,192 pixels a side); an SVG, after
the XML check (256 KiB, 20,000 tokens, 64 levels, no DOCTYPE, scripts, event handlers or outside
references), with `fyne-io/oksvg` on the goroutine that fetched it, into a 128-pixel PNG under a
recover, at that size whatever its viewBox says, never by Fyne's renderer, which sizes its buffer by
the viewBox the host chooses
([`helmcharticons_svg.go`](../../internal/ui/fyneui/helmcharticons_svg.go);
`TestRasteriseSVGDrawsTheIcon`, `TestRasteriseSVGIgnoresAHugeViewBox`,
`TestRasteriseSVGFailsWithoutTheWindow`). What remains: a bug in Go's PNG or JPEG decoder, or in
oksvg's handling of the paths, gradients and transforms an SVG may hold, is reachable from any icon
host; a panic fails that icon only, but one that does not panic (a loop within the 20,000 tokens, a
wrong drawing) is not caught. Go's decoders and the same SVG check also read the cluster icons the
user picks.

### H-41: Repositories whose content nothing authenticates

A repository Helm's file names over plain `http://`, or over https with `insecure_skip_tls_verify`
([D-29](../developer/decisions.md#d-29-helm-chart-repositories-from-helms-repositoriesyaml)),
proves nothing about who answers: anyone on the network path can serve another index and charts
that match its digests, since the digest comes from that same index
([H-30](#h-30-chart-provenance-is-not-verified)), and over http can read the basic auth the user
allowed for it ([credentials › H-40](credentials.md#h-40-credentials-over-plain-http)). Cogpit
marks such an origin in the warning colour wherever its charts show or install, and agents read the
same marks (`spec.origin`, the tabs' `origin`, the preview's `warning`; `plainHTTP` and
`insecureSkipTLSVerify` in `helm.repo.list`; an agent's install or upgrade always asks,
[D-2](../developer/decisions.md#d-2-agent-interface-scope)). It refuses an agent's install or
upgrade, or a window's upgrade, whose origin changed after it was reviewed
([architecture §8](../developer/architecture.md#8-helm-internalhelm); `TestHelmChartOriginMarked`,
`TestHelmUpgradeOriginMarked`). Open: a mark, not a refusal or a question (neither is decided), and
nothing stops a click past it. Live for every entry Helm's file names that way.

### H-42: A repository another program writes into Helm's file is used without asking

Cogpit takes over every change of Helm's `repositories.yaml` within seconds, whoever wrote it:
`helm repo add`, an editor, any program running as the user (D-29). While the Charts view or a Helm
Install tab is open, a new entry's index is downloaded at once: a request with the entry's
credentials to a host the user did not choose in Cogpit, trusting the entry's CA file and
presenting the client certificate it names, which may be any file the user can read (the key never
leaves the process). Nothing runs, so unlike [H-1](#h-1-kubeconfig-files-added-by-sync-folders)
nothing asks; its charts install only when the user, or an agent with the user's acceptance,
installs one. An `http://` entry with credentials downloads nothing until the user allowed it
([H-40](credentials.md#h-40-credentials-over-plain-http)). **Accepted.** Live whenever another
program writes the file while the Charts view or a Helm Install tab is open.

### H-35: kubectl's signature check rests on an embedded trusted root and Rekor v1's search

Every download is checked as the kubectl row of
[Boundaries and required checks](#boundaries-and-required-checks) says
([D-50](../developer/decisions.md#d-50-kubectls-signature-check);
[`verify.go`](../../internal/kubectl/verify.go), [`rekor.go`](../../internal/kubectl/rekor.go)): a
compromised mirror or CDN cannot pass it, nor a signature made later with a leaked key, whose entry
cannot carry a log-signed time inside the certificate's ten minutes (`TestRealReleaseRefused`,
`TestVerifierRefuses`). What remains:

- **A misissued certificate** passes when it carries a signed certificate timestamp (so it is
  public) and its signature is in Rekor. Dormant: needs a compromise of Fulcio or of the release's
  Google service account.
- **A stale trusted root**
  ([`sigstore_trusted_root.json`](../../internal/kubectl/sigstore_trusted_root.json)) is refreshed
  only by hand, `make sigstore-root` through sigstore's TUF repository (Renovate cannot see it). A
  release under a key sigstore rotated in is refused until a Cogpit release carries the new root:
  fails closed. Its Fulcio root and intermediate are valid until 2031-10-05.
- **Rekor v2** (`log2025-1.rekor.sigstore.dev`, in the root) has no search: a release logged only
  there is refused ("sigstore's transparency log has no entry for it") until Cogpit reads a
  sigstore bundle. Dormant: kubectl 1.36.1 and 1.37.1 are in Rekor v1 (`TestRealReleaseSignature`);
  when the Kubernetes release moves is not verified.
- **No consistency check of the log:** no earlier checkpoint is kept and no witness asked, so a
  split view is not detected. Dormant: needs Rekor's signing key.
- **The lookup tells the log what the user downloads** (release and platform, from the user's
  address or proxy); it carries no credential.
- **No download without `rekor.sigstore.dev`** (a mirror in a closed network): the user sets
  Preferences › Kubernetes › Path to kubectl or puts one on `PATH`. A cost of the check (D-50).

### H-36: A shell's profile can put another kubectl first

Cogpit puts its kubectl first on a terminal's `PATH`, and a profile that runs later can reorder it:
on macOS `path_helper`, which `/etc/zprofile` and `/etc/profile` run, moves inherited entries behind
`/etc/paths` and `/etc/paths.d`. zsh, bash and fish get the directory first again after their
startup files ([D-3](../developer/decisions.md#d-3-helm-and-kubectl-binaries),
[`terminal/kubectl.go`](../../internal/terminal/kubectl.go); `TestZshKubectlFirstAfterTheProfile`,
`TestBashKubectlFirstAfterTheProfile`, `TestFishKubectlFirstAfterTheConfig`, which runs only where
fish is installed; no CI workflow installs it). What remains:

- **A later change:** a tool that changes `PATH` at a later prompt (direnv, asdf, a typed
  `export`), a `.zshrc` that empties `precmd_functions`, and a Preferences shell argument
  `-l`/`--login`, which makes bash skip the hand-over.
- **Other shells** (sh, PowerShell, cmd): only the environment's order. Live on macOS whenever
  `/etc/paths` or a profile's directory holds a kubectl (Docker Desktop's `/usr/local/bin/kubectl`,
  Homebrew's), and on Linux distributions whose `/etc/profile` sets `PATH` anew. On Windows the
  folder of a `kubectl.exe` comes first with its other programs.

Preferences › Kubernetes › Enforce kubectl, which only the user sets, also defines a function
`kubectl` in zsh, bash and fish, after removing an alias of that name (`TestZshKubectlEnforced`);
it leaves a function a later hook defines, a call by full path, and PowerShell and cmd. The
terminal's kubectl line names the release Cogpit put first; `kubectl version --client` shows which
one runs.

### H-2: Local processes and forwarded ports

A forward listens on `127.0.0.1`, which every process of every local user can reach, with the pod's
identity behind it: arbitrary TCP protocols allow no per-connection authentication, and kubectl has
the same exposure. A forward runs until Stop, Delete, its cluster's disconnect or quitting (a
reconnect carries it over and showing another cluster does not end it,
[D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once)) and is not restored at
the next start. Agents start or edit one only with the user's acceptance (`pod.portForward`,
`service.portForward` and `portforward.edit` are mutating, D-2) unless agent writes are allowed for
the context, and open, stop and delete one freely (`portforward.open`, `portforward.stop`,
`portforward.remove`); an agent that learns the URL connects like any local process. Not verified:
whether another process can bind the same port on Windows (`SO_EXCLUSIVEADDRUSE`).

### H-19: The terminal widget trusts its input

The widget, Cogpit's patched copy of `fyne-io/terminal`
([`third_party/fyne-terminal`](../../third_party/fyne-terminal/PATCHES.md),
[D-14](../developer/decisions.md#d-14-terminal-widget)), acts on container output on Fyne's main
goroutine, where nothing recovers a panic. Cogpit's escape filter stays in front of it
([`termview/sanitize.go`](../../internal/ui/fyneui/termview/sanitize.go)). The copy, its
scrollback included
([PATCHES.md › Scrollback](../../third_party/fyne-terminal/PATCHES.md#scrollback),
[D-56](../developer/decisions.md#d-56-the-terminals-scrollback)), is fuzzed alone:
`FuzzHandleOutput`, and `FuzzRawWidget` through its public API, each failing on a panic or an input
not done within 30 s; its tests run under the race detector in CI. What remains: inputs the
minutes-long fuzz runs did not reach; the defects PATCHES.md lists as known and not patched, which
the filter keeps from the widget (DCS strings, media copy, counts above 9999) except an ESC at byte
5000 of one read; and upstream's later fixes, which reach the copy only by a sync by hand (Renovate
leaves the module alone). The copy goes once upstream has equivalent fixes (D-14).

### H-21: Access reviews guide, they do not guard

Cogpit greys and refuses what the connected user's access reviews deny, and hides the kinds they
may not list ([`accessreviews.go`](../../internal/engine/accessreviews.go)). That guides what Cogpit
shows and offers; it is no authorization boundary. Answers are kept until `cluster.refreshAccess`
or the next connect, so a role changed meanwhile is not seen; a rules review can over-report a
cluster-scoped kind that only a RoleBinding names, which a command's check confirms with an access
review and the sidebar's list of kinds does not; Kubernetes' documentation advises against rules
reviews for authorization decisions. An unknown answer offers the command. By design: the API
server stays the authority for every request.

### H-24: A dropped kubeconfig can send a local file as its token

A kubeconfig dropped into a synced folder or added by the user can pair `tokenFile: <any file the
user can read>` with a server of its own; on connect client-go sends the file's content there as a
bearer token ("file exposure" in Kubernetes'
[kubeconfig docs](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)).
A token file outside the kubeconfig's folder waits for the user's approval
([login approval](#login-approval),
[D-17](../developer/decisions.md#d-17-token-files-outside-the-kubeconfigs-folder)) in every
source file, `~/.kube/config` and the `--kubeconfig` and `$KUBECONFIG` files included
([D-28](../developer/decisions.md#d-28-the-default-kubeconfig-as-sources-as-lens-has-it)). What
remains:

- **A broad folder:** the folder includes every subdirectory, so for a file added from `~`,
  Downloads or a shared drive's root a `tokenFile` anywhere below (`~/.ssh/id_ed25519`) is not
  asked for. Live for added files there; for a sync folder only as far as its writer can already
  write.
- **A link repointed after the connect:** client-go re-reads a token file once a minute, so a link
  in the folder repointed outside is read without a question until the next connect. Live while
  such a connection is open and someone else can write the folder.
- **The window between check and read:** a swap to a link outside between the policy's decision
  and client-go's open is not caught (microseconds, write access to the folder at that moment).
- **Other files:** no approval binds a `client-certificate` or `client-key` (the key is never sent,
  the certificate only in the TLS handshake). Cogpit reads a `client-certificate` file while it
  lists the contexts and keeps only its two dates
  ([D-75](../developer/decisions.md#d-75-a-client-certificates-validity-in-the-cluster-lists),
  [file access › H-119](file-access.md#h-119-a-client-certificate-file-is-read-while-the-contexts-are-listed));
  only client-go opens the key file.
- **No configuration directory:** nothing is asked
  ([H-1](#h-1-kubeconfig-files-added-by-sync-folders)). Dormant.
