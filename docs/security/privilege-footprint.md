# Privilege footprint

What Cogpit sends to a cluster, verb by verb, and what each action changes; the tables are the
contract the `verbs` and `Danger` of the commands in
[`internal/engine/commands`](../../internal/engine/commands/) must match
([architecture §4.2](../developer/architecture.md#42-the-command-registry)). Cogpit holds no identity
of its own and installs nothing for its own features
([below](#nothing-installed-for-cogpits-own-features)): it acts with the credentials of the
kubeconfig context a command acts on, so it can do no more than `kubectl` and `helm` with that
context; agents use the same verbs under the rules of [agents and MCP](agents-and-mcp.md). Verbs and
subresources are Kubernetes RBAC's ([RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/),
[request verbs](https://kubernetes.io/docs/reference/access-authn-authz/authorization/#determine-the-request-verb));
the actions are Lens's ([resource parity](../parity/resource-parity.md)).

## On connect and while browsing

### Connection

| Action | Requests | Notes |
|---|---|---|
| Connect (`cluster.connect`; `hotbar.openCluster`, a tile or F1–F12; at start the last session's clusters while Preferences › App › Restore the last session is on) | `get /version`; discovery, unless the discovery cache on disk answers ([D-65](../developer/decisions.md#d-65-discovery-cached-on-disk)); `create selfsubjectrulesreviews` per namespace Cogpit knows of (the context's, the accessible ones, the filter's on each change, and the cached namespaces while 32 or fewer if the user may list them); `create selfsubjectaccessreviews` where the rules cannot answer (a cluster-scoped or all-namespaces question such as listing namespaces; incomplete rules; wildcards); if the user may list namespaces, `list`, `watch` on `namespaces` for the connection's life, shared with the filter and the engine ([`nswatch.go`](../../internal/engine/nswatch.go)) | Every authenticated user may send these (`system:basic-user`, `system:discovery`); the server stores nothing. Reviews: own budget (10/s, burst 20), never polled, kept until `cluster.refreshAccess` or the next connect; none after a failed one until a refresh or a lost connection's return; at most 512 namespaces and 2,048 questions ([`access.go`](../../internal/cluster/access.go), [`accessreviews.go`](../../internal/engine/accessreviews.go); `TestBudgetAccessReviews`, `TestKindAccessReviews`, `TestAccessReviewsAreBounded`). Restore is off by default; only the user turns it on (`ErrRestoreSessionGUIOnly`) |
| Reconnect (`cluster.reconnect`; `cluster.login` of a context without a credential plugin or whose file login changed; by itself when the server rejects the login, the file holds another and no plugin runs) | a connect's requests on a new connection, then the open views' lists and watches; the old watches end; port forwards listen again (`create pods/portforward`) | Writes nothing; shells, attach and node shells are not reopened (`dock.reconnect` asks). Refused while a drain or Helm action runs; one at a time per cluster; a file's new login tried once per file version. Refused to agents and the CLI while the login expired or lapsed and a credential plugin would run (`ErrReconnectLoginGUIOnly`) |
| Who the user is (no command: each connect of a real cluster, each return from an interruption or renewed login) | `create selfsubjectreviews` (`authentication.k8s.io/v1`, after a 404 `v1beta1` for Kubernetes 1.27), bounded by 5 s at the connect | `kubectl auth whoami`'s question, granted to every authenticated user (`system:basic-user`). Package `cluster` keeps username and groups and drops UID and extra fields, which can hold a token's credential id or identity-provider claims ([`whoami.go`](../../internal/cluster/whoami.go)). A 404, 403 or failure never fails the connect: the kubeconfig's user name stands in, marked (`TestReviewUserAsksTheAPIServer`, `TestReviewUserFallsBackToV1beta1`, `TestReviewUserFallsBackToTheKubeconfigUser`, `TestConnectLearnsTheUser`, `TestKindWhoAmI`). Only a confirmed username is kept, per context, in `cluster-users.json` (owner-only) |
| The connection's checks (no command: each connected real cluster, shown or not; [D-58](../developer/decisions.md#d-58-standby-and-lost-connections)) | `get /version` with credentials every 15 s and at once on wake, network change or a broken watch (5 s to answer, a second 2 s after a failure); while interrupted one `get /version` **without credentials** per attempt (after 1, 2, 4, 8, 16, then every 30 s), one with them once answered; nothing while the login is expired, or lapsed on a hidden cluster with "Renew the login while the cluster is not shown" off | The anonymous probe counts 401 or 403 as an answer and never runs a credential plugin. About 4 requests a minute per cluster (`TestStandbyFrozenControlPlane`, `TestStandbyRevokedLogin`). Interrupted or expired, nothing else goes out: lists and watches wait at a gate, other requests are refused unsent, mutating commands are disabled for every origin, a running drain or Helm action stops in its journal ([`health.go`](../../internal/cluster/health.go), [`connwatch.go`](../../internal/engine/connwatch.go)); a lapsed login disables writes too |
| A connected cluster the window does not show ([D-69](../developer/decisions.md#d-69-several-clusters-connected-at-once)) | its checks, the custom resource definitions' watch, access reviews when refreshed, port forwards, logs, shells, running jobs; its views' watches until the warm period (5 minutes) ends | Shown again within it: no request; later its views list again (`TestSwitchingClustersOnARealStack`). Connected until `cluster.disconnect` or quit. A read never connects a cluster ([which cluster a command acts on](README.md#what-this-does-not-cover)). Kyverno, Trivy and Alertmanager read the shown cluster only |

### Views

| Action | Requests | Notes |
|---|---|---|
| Custom resource kinds | `list`, `watch` on `customresourcedefinitions.apiextensions.k8s.io` (cluster-scoped) while connected; after a refused list, API discovery | Definitions become views live ([`customkinds.go`](../../internal/engine/customkinds.go)); objects, schemas included, are held in memory. A refused list ends the watch until `cluster.refreshAccess` or the next connect (`TestCustomKindsStopAfterARefusal`, `TestKindNamespaceLimitedUser`); that user gets the curated kinds discovery says are served (Flux, Argo CD, Vertical Pod Autoscalers), no other custom views ([`customkinds_discovery.go`](../../internal/engine/customkinds_discovery.go)) |
| Open a list view | `list`, `watch` on the kind, in all namespaces or per namespace; with the cluster's Streaming opt-in a streaming `watch` instead of the `list` | One watch per kind and scope, shared by views and agents, until the warm period after its last reader ([architecture §5](../developer/architecture.md#5-cache)); a scope refused with 403 is not kept warm (`TestRefusedScopeIsNotKeptWarm`). A user the reviews deny a kind in all namespaces gets no cluster-wide read: one `list`, `watch` per named namespace (the accessible ones, Cluster Settings › Namespaces or `cluster.settings.set`'s `accessibleNamespaces`, and the filter's; else the context's), none whose rules deny it ([`accessiblens_plan.go`](../../internal/engine/accessiblens_plan.go)); deciding costs at most one `create selfsubjectaccessreviews` per kind, none without named namespaces (`TestBudgetAccessibleNamespaces`, budget P15). Until the context's rules review answers, the window's views, drawer, menus, Overviews and dashboards read nothing of the kind ([`overview.go`](../../internal/engine/overview.go), [`clusteroverview.go`](../../internal/engine/clusteroverview.go), `TestPagesFollowTheAccessReviews`); an agent's or the CLI's read waits up to 10 s, then reads all namespaces. A 403 to a cluster-wide read, whoever sent it, moves every reader of the kind to namespaces until a refresh (`cache.Cache.OnRefused`, `TestTargetReadsFollowARefusedClusterWideList`). For a user denied the kind in all namespaces, one object's drawer, `object.get` and `cogpit-cli get` read its namespace alone (`TestDetailWaitReadsTheObjectsNamespace`). Accessible namespaces narrow reads, they grant nothing |
| A Terminating namespace's Blocking Deletion (its drawer, `object.get`; [D-98](../developer/decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)) | per kind its `NamespaceContentRemaining` condition names, `list`, `watch`, **metadata only**, **in that namespace only**, while the drawer shows it; its rules review if not asked; while `NamespaceDeletionDiscoveryFailure` is True, `list`, `watch` on `apiservices.apiregistration.k8s.io` | Read only ([`nsblockers.go`](../../internal/engine/nsblockers.go)); the condition names kinds and counts, never objects. A kind the rules deny is not read (counted from the condition); a 403 ends a read while the drawer stays. No Secret value or custom spec crosses the wire, no cluster-wide list starts (`TestBudgetTerminatingNamespaceBlockers`) |
| Secrets list (the view, a Secret's drawer, an agent's read of `secrets`) | `list`, `watch` on `secrets` in full, values included; no `get` | Values in memory: [H-8](#h-8-secret-data-arrives-with-the-list). Drawer and reveal send nothing (`TestBudgetSecretScopes`, `TestBudgetSecretsViewAndDrawer`, `TestKindSecretScopes`). The cache drops each Secret's `kubectl.kubernetes.io/last-applied-configuration`, which repeats the values (`TestSecretsLoseTheLastAppliedAnnotation`, `TestSecretDrawerRedactsTheLastAppliedAnnotation`). `resources.find` without a kind starts no such watch (`TestBudgetFindWithoutKindStartsNoReleaseWatch`) |
| Helm releases (the view, a release's drawer, an agent's read) | `list`, `watch` on `secrets` and, where served, `configmaps` labelled `owner=helm`; none while the full Secrets or Config Maps watch runs; per namespace for a user who may not list them everywhere (`TestHelmReleasesOfANamespaceLimitedUser`) | RBAC cannot narrow a list by label: releases need every Secret (ConfigMap) of the namespace ([Helm storage backends](https://helm.sh/docs/topics/advanced/#storage-backends)); either driver alone shows its releases. The label scope reads from a running full watch and never keeps it alive ([architecture §5](../developer/architecture.md#scopes-local-and-virtual-kinds); `TestBudgetSecretsAndReleasesShareOneWatch`, `TestCoverExpiryHandsOverToOwnInformer`, `TestBudgetHelmReleasesWithConfigMaps`). Payloads decode into a memo cleared 5 minutes after the last read ([`releases.go`](../../internal/helm/releases.go), `TestDeriveMemoForgetsWhenIdle`), kept from agents ([H-8](#h-8-secret-data-arrives-with-the-list)) |

### Integrations

| Action | Requests | Notes |
|---|---|---|
| Integrations before the user chose them (the window's session; [D-82](../developer/decisions.md#d-82-a-pre-choice-before-any-integration-is-looked-for-and-ask-again)) | **none**: no `metrics.k8s.io` discovery or list, no `services/proxy`, no `vulnerabilityreports` or `policyreports` | Off until the pre-choice is answered, also while put off with Later or connected in the background; Check all and Choose look for what they tick (`TestIntegrationsQuestionSteps`, `TestIntegrationsOffUntilAnswered`, `TestKindIntegrationsQuestion`). Only the user, in the window, switches an integration either way, answers or re-asks; other origins are refused before any read (`RefuseIntegrationSwitches`, H-126, H-133; `TestIntegrationsOnAreTheUsers`) but may put the question off (`cluster.integrations.later`). `cogpit-cli` applies none of the user's switches ([H-120](agents-and-mcp.md#h-120-the-clis-headless-engine-does-not-apply-a-clusters-integration-switches)) |
| Kyverno's policy reports ([D-79](../developer/decisions.md#d-79-kyvernos-policy-violations-on-the-objects)) | `list`, `watch` on `policyreports.wgpolicyk8s.io` (all or named namespaces) and `clusterpolicyreports.wgpolicyk8s.io` while the integration is on and the cluster shown; nothing when neither is served | Read only, trimmed to fail and warn results ([`kyverno.go`](../../internal/engine/kyverno.go)); policies are not read. `kyverno.violations` reads the cache and refuses a hidden cluster. A refused list is a state Cluster Settings shows |
| Argo CD | `list`, `watch` on `applications`, `applicationsets`, `appprojects` of `argoproj.io` while used; for Settings › Repositories and Clusters `secrets` labelled `argocd.argoproj.io/secret-type in (repo-creds,repository)` or `=cluster`, none while the full Secrets watch runs | Kinds follow Argo CD's definitions; settings views only while `applications.argoproj.io` is served. The Secrets arrive in full ([H-8](#h-8-secret-data-arrives-with-the-list)), shown through an allowlist of keys ([D-35](../developer/decisions.md#d-35-what-argo-cds-repository-and-cluster-secrets-show), [H-66](credentials.md#h-66-argo-cds-credentials-are-in-cogpits-memory-while-its-settings-are-read)) |
| Metrics (Metrics Server) | discovery `get` on `/apis/metrics.k8s.io/v1beta1`, again after 10 minutes (2 while not served, forbidden or unavailable); `list` of `pods.metrics.k8s.io` (all namespaces, or those the Pods view reads, [D-39](../developer/decisions.md#d-39-pod-metrics-for-a-user-who-may-list-pods-in-some-namespaces-only)) and `nodes.metrics.k8s.io`; `get` of one object | Polled, never watched, only while something shows them (lists 60 s, drawers 15 s; [`internal/metrics`](../../internal/metrics/), [architecture §9](../developer/architecture.md#9-metrics-internalmetrics)); all hidden, no poll (`TestHiddenMetricsAreNotPolled`); nothing open, no request, discovery included, but one detection while the integrations question or Cluster Settings show them. `metrics.get` starts no poll. Without `list` the state is "forbidden" (`TestErrorsAreStates`); a namespace's refusal blanks only its pods, keeps the provider and does not ask Prometheus instead (`TestBudgetNamespaceLimitedUser`). Counts: [testing P12](../developer/testing.md#performance-budgets) |
| Metrics (Prometheus, Thanos, VictoriaMetrics through the API server) | `get services/proxy` of **the one Service found or set**: `GET /api/v1/namespaces/<ns>/services/<name>:<port>/proxy/api/v1/status/buildinfo`, `…/query`, `…/query_range` (`https:<name>:<port>` with Use HTTPS); while looking, up to 3 candidates, each port by number then name: up to 6 probes, each refusal a 403 in the audit log; `list`, `watch` on `services` through the cache | Only `GET`, only these read-only paths, though `services/proxy` would carry any request; **never `nodes/proxy`**, the kubelet's whole API. Only while a drawer, list, Overview, the Nodes' Disk column, a claim's or a volume's drawer (the kubelet's volume stats, [D-139](../developer/decisions.md#d-139-a-volumes-usage-in-its-claims-and-its-own-drawer)) or the alert board's history (the `ALERTS` series, [D-138](../developer/decisions.md#d-138-the-alert-boards-cards)) needs it, the Metrics Server first, and one detection while the integrations question or Cluster Settings show it; counts: [testing P13, P14, P18, P19](../developer/testing.md#performance-budgets). A metrics integration off gets no request. Queries carry pod, namespace, node and claim names and node addresses, quoted (`FuzzQuoteLabel`, `TestQueries`, `TestClaimNamesAreQuotedOnTheWire`), never a credential. **Not limited by namespace:** RBAC gates the proxy, not the series; a namespace-limited user's queries name their namespaces, nothing narrows the answer, Overview pod counts include pods they cannot list, and a persistent volume's drawer shows the usage of its claim in a namespace they may not read (numbers only, D-39). **Found by name** ([H-26](#h-26-prometheus-is-found-by-name)). Failures show the API server's Status or a class, never the body (`errorClass` in [`status.go`](../../internal/metrics/status.go), `TestErrorClassesCarryNoBodies`); a refused proxy is "forbidden" (`TestDetectForbiddenIsAState`, `TestDetectTriesThePortsNumberThenName`); a user known not to list Services gets no watch and "unavailable" |
| Trivy Operator's reports ([D-78](../developer/decisions.md#d-78-trivy-operators-image-ratings)) | `list`, `watch` on `vulnerabilityreports.aquasecurity.github.io` (all or named namespaces, nothing before the reviews decide) while the integration is on, the cluster shown and the kind served | Read only; no scanner installed. Off or hidden: released, ends after the warm period. Kept without the vulnerability lists, in memory ([`trivy.go`](../../internal/engine/trivy.go)); the reports' own view adds a whole watch. Without `list`: "forbidden" with verb and resource (`TestTrivyNotServedOrForbidden`) |
| Alerts (Alertmanager through the API server, [D-77](../developer/decisions.md#d-77-alertmanager-the-alerts-pill-and-the-alert-board)) | `get services/proxy` of **the one Service found or set**: `GET /api/v1/namespaces/<ns>/services/<name>:<port>/proxy/api/v2/alerts?active=true&silenced=false&inhibited=false` (`https:` with Use HTTPS); this request is the probe, up to 3 candidates × 2 port spellings; never `/api/v2/status`, which returns the configuration; `list`, `watch` on `services` through the cache | Only `GET`: silences are not Cogpit's ([`internal/alerting`](../../internal/alerting/)). One request per 30 s **only while the integration is on and the cluster shown and connected**; failing, backoff to 5 minutes; none found, none ([testing P17](../developer/testing.md#performance-budgets)). **Not limited by namespace.** Failures show the Status or a class, never the body (`TestErrorText`). **Found by name** ([H-121](#h-121-alertmanager-is-found-by-name-and-its-texts-reach-the-window-and-agents)) |

## Actions that write

`Danger` is the registry's level: `Destructive` asks every origin for a confirmation that names the
objects, the namespace and the context ([conventions](../developer/conventions.md#never-surprise-a-cluster)).
Every row follows these rules:

- **Field manager.** Cogpit's own writes name `cogpit`
  ([`cluster/actions.go`](../../internal/cluster/actions.go) `FieldManager`), the node shell pod's
  create excepted; the Helm SDK's carry Helm's default, the executable's file name.
- **Bound to the confirmed object.** A patch, eviction or delete carries the UID the user confirmed
  (else the cache's) as `metadata.uid`, a `test` or a precondition, so the API server refuses it on
  an object created again under the name (`cluster.ErrReplaced`,
  [`cluster/uidbound.go`](../../internal/cluster/uidbound.go); `TestKindPatchesBoundToTheUID`,
  `TestPatchesCarryTheUID`, `TestRestartAndScaleCarryTheUID`). Suspend, Resume, Cordon and Uncordon
  carry none; Helm's actions are bound to the release's newest record (their rows).
- **Agents always ask** for a Role, ClusterRole, RoleBinding or ClusterRoleBinding in `object.apply`,
  `object.create` and the create dialogs, and for every Helm install and upgrade, also where agent
  writes are allowed ([D-2](../developer/decisions.md#d-2-agent-interface-scope);
  [H-27](#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)).
- **A Secret's values are the user's.** `secret.reveal`, `secret.reviewData`, `secret.saveData`,
  `secret.create`, a Secret's Edit tab and a Secret in a Create text are GUI only: agents and the
  CLI are refused before anything is asked or sent (`Spec.Refuse`, `ErrSecretGUIOnly`;
  `TestSecretWritesFromAgentsRefusedBeforeAsking`). No value reaches the audit log (a Secret's
  logged `yaml` is redacted), an error, a toast or a draft; an API message quoting one is scrubbed in
  raw, base64 and decoded forms (`TestSecretWriteErrorsNeverQuoteValues`;
  [D-19](../developer/decisions.md#d-19-editing-and-creating-secrets-from-yaml)).
- **Removing a data key** of a Config Map or Secret, in its drawer or Edit tab, is destructive: the
  confirmation names the keys, never a value (`TestApplyRemovingADataKeyIsDestructive`,
  `TestAgentApplyRemovingADataKeyAlwaysAsks`, `TestApplyRemovingASecretKeyIsDestructive`,
  `TestConfigMapDataAgentRemovalAsks`).
- **Reviewed text.** An apply, install or upgrade from a tab names the `textRevision` the user
  reviewed; a text an agent changed since, also while the confirmation was open, is refused
  (`TestApplyFromTabBoundToTheReviewedText`, `TestHelmUpgradeTabBoundToTheReviewedText`).
- **Selections.** At most 500 objects (`engine.MaxSelection`) under one confirmation; more than 10
  (`engine.DirectSelection`) run as a background job, one request per object in turn, `job.cancel`
  (any origin) stopping before the next request; nothing is retried or undone
  ([`commands/manyjob.go`](../../internal/engine/commands/manyjob.go),
  [D-63](../developer/decisions.md#d-63-long-commands-as-background-jobs)).
- **No answer.** A request that is not a stream ends after 30 s without an answer
  (`cluster.DefaultRequestTimeout`, [`cluster/health.go`](../../internal/cluster/health.go)); a
  write whose answer never came ends as `cluster.OutcomeUnknownError` and is never sent again
  (`TestARequestWithoutAnAnswerEndsAfterTheTimeout`).

### Reads behind actions

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Logs (`pod.logs`, `workload.logs`, `logs.controller`, `application.logs`, `argocd.logs`, `flux.logs`; `logs.setSource`, `logs.sources`, `logs.loadOlder`, `logs.jumpTo`) | `get pods/log`; the pods, Replica Sets and a group's object and workloads from the cache | Read | Nothing. A tab streams at most 40 containers at once (a group's of at most 20 workloads, [`engine/grouplogs.go`](../../internal/engine/grouplogs.go)); older lines and a jump to a time are one request per container, only on the user's or an agent's action, bounded by the tab's 10,000 lines ([`engine/logsolder.go`](../../internal/engine/logsolder.go)) |
| Secret drawer, reveal (`secret.reveal`) | none: the full Secrets watch's cache (declared `list secrets`) | Read, GUI only | Nothing. A value (4 KiB or 40 lines; whole with `whole`, [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)) lives only in the window's widgets until Hide, another object or the drawer's close; the reveal names the Secret's `resourceVersion`, so a changed Secret is refused (`TestRevealedValueDroppedWhenSecretChanges`). An agent's refused reveal is audited without the value (`TestRevealIsGUIOnly`) |
| Refresh access (`cluster.refreshAccess`) | the access reviews of a connect | Read | Nothing; the cached answers are fetched again |
| Server-side dry run (`object.dryRun`) | `update` with `dryRun=All` of an Edit tab's text | Read | Nothing stored; RBAC still asks for `update`, and admission webhooks with side effects refuse it. A conflict never moves the edit's base version, so an agent's check cannot make the user's next apply overwrite a concurrent change |
| A release's revisions (`helm.release.revision`, `helm.release.diff`, `ui.helm.revisions`, `ui.helm.revisions.set`) | none: the cached release records | Read; `ui.helm.*` UI | Nothing (`TestReleaseRevision`); values and rendered Secrets as [H-8](#h-8-secret-data-arrives-with-the-list) says |

### Pods and workloads

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Pod shell, attach (`pod.shell`, `pod.attach`; `dock.reconnect` after an interruption) | `create pods/exec`, `create pods/attach`; from Kubernetes 1.35 the WebSocket upgrade is authorised as `create` too (`AuthorizePodWebsocketUpgradeCreatePermission`) | Mutating; `dock.reconnect` of a node shell Destructive | A process in the container, or an attach to its main process: typed input reaches it, Ctrl+C and Ctrl+D included, which can end the main process and restart the container. Closing an attach tab detaches; the process runs on (`TestKindWorkloadLogsAttachRestartScale`) |
| Port forward (`pod.portForward`, `service.portForward`, `portforward.edit`; `argocd.openUI`, to the Service labelled `app.kubernetes.io/name=argocd-server`) | `create pods/portforward` (a WebSocket upgrade, counted as `get pods/portforward`, else an SPDY `POST`); pods and Services from the cache | Mutating; `portforward.open`, `portforward.stop`, `portforward.remove` UI | A listener on `127.0.0.1` only ([H-2](trust-boundaries.md#h-2-local-processes-and-forwarded-ports)); nothing in the cluster changes and a crash leaves nothing there ([`cluster/portforward.go`](../../internal/cluster/portforward.go)). A service forward moves to another ready pod; a forward ends with Stop, Delete, its context's disconnect or quitting, not when another cluster is shown (`TestKindPortForward`) |
| Scale (`deployment.scale`, `statefulset.scale`, `replicaset.scale`) | `patch` on the `scale` subresource | Mutating; Destructive to zero | The replica count; a Replica Set a Deployment controls is set back by it, which the GUI says |
| Restart (`deployment.restart`, `daemonset.restart`, `statefulset.restart`; a selection: `workload.restartMany`, [`commands/restartmany.go`](../../internal/engine/commands/restartmany.go)) | `patch`: kubectl's `rollout restart` patch plus `metadata.uid` (`TestRestartWorkloadSendsKubectlsPatch`) | Destructive: it replaces every pod | The template's `kubectl.kubernetes.io/restartedAt` annotation; with the OnDelete strategy pods change only as they are deleted, which the confirmation says. A selection: one confirmation naming every workload (`TestRestartMany`) |
| Suspend, resume (`cronjob.suspend`, `cronjob.resume`) | `patch cronjobs` of `spec.suspend` alone ([`cluster/workloads.go`](../../internal/cluster/workloads.go) `SetCronJobSuspend`) | Mutating | `spec.suspend`; running Jobs go on |
| Trigger (`cronjob.trigger`) | `create jobs` | Mutating | A Job as `kubectl create job --from=cronjob/<name>` makes it, with `cronjob.kubernetes.io/instantiate: manual`, named `<cronjob>-manual-<5 random characters>` |
| Evict (`pod.evict`) | `create pods/eviction` | Destructive | The pod deleted, honouring PodDisruptionBudgets; a budget's refusal is an error naming it, not retried |
| Debug container (`pod.debug`, [D-102](../developer/decisions.md#d-102-debug-containers-added-from-the-drawer-shown-in-blue-reached-by-logs-shell-and-attach)) | `patch pods/ephemeralcontainers` (the new container alone), then `create pods/attach` (`TestAddEphemeralContainerSendsTheGuardedPatch`, `TestAddEphemeralContainerRefusals`) | Destructive | An ephemeral container that stays as long as the pod and, with a target, sees its processes and files ([below](#debug-container), [H-134](#h-134-a-debug-container-stays-in-the-pod-and-sees-its-target); `TestDebugAddsTheConfirmedContainer`, `TestDebugRefusals`, `TestKindDebugContainer`) |

### Objects, Config Maps and Secrets

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Edit and apply (`object.apply`) | `update` with `dryRun=All`, then for real; the text comes from the cache | Mutating; Destructive when it sets `spec.replicas` to 0 or removes a data key; agents always ask for RBAC objects | The object, with the tab's base resourceVersion as precondition: a concurrent change is a conflict, never overwritten; every manager's `managedFields` entries are kept. A Secret's tab shows its values (base64 in `data`, plain in `stringData`; `TestSecretEditAndCreateAreGUIOnly`) |
| A Config Map's values in the drawer (`configMap.saveData`, `configMap.reviewData`, `configMap.readData`, `configMap.keepEdits`; [D-55](../developer/decisions.md#d-55-a-config-maps-values-edited-in-the-drawer)) | save: `update configmaps` with `dryRun=All`, then for real; review: the dry run alone; read: the cache | `saveData` Mutating, Destructive when it removes a key; `reviewData`, `readData` Read; `keepEdits` UI, GUI only | Named keys' values, keys added and removed (`TestConfigMapDataAddsAndRemovesKeys`), on the cached object with the values' base resourceVersion as precondition: a changed Config Map is not written, and the error names the version a second call must name to write over it (`TestConfigMapDataNeverOverwritesUnasked`). A review given the keys' base values (`from`) rebases onto a version none of them changed in; the save never rebases (`TestConfigMapDataRebases`, `TestKindConfigMapDataSave`). Refused before sending: an unknown key, binary data, a Helm release record, an immutable Config Map, a placeholder Cogpit showed for a Secret's value (`TestConfigMapDataRefusals`, `TestConfigMapDataKeyRefusals`, `TestConfigMapDataAgents`). An agent's save shows its diff in the confirmation. Unsaved edits stay in memory and an owner-only draft |
| A Secret's values in the drawer (`secret.saveData`, `secret.reviewData`; [D-59](../developer/decisions.md#d-59-a-secrets-values-edited-in-the-drawer)) | save: `update secrets` with `dryRun=All`, then for real; review: the dry run; read: the cache | `saveData` Mutating, Destructive when it removes a key; `reviewData` Read; both GUI only | As a Config Map's, base64-encoded, the Secret sent whole. The update drops kubectl's last-applied annotation, which the cache does not hold ([credentials › H-103](credentials.md#h-103-a-secret-saved-from-cogpit-loses-kubectls-last-applied-annotation)). Also refused: a binary value to change, a Secret the drawer does not show (`TestSecretDrawerDataSave`, `TestSecretDrawerDataAddsAndRemovesKeys`, `TestSecretDrawerDataRefusals`, `TestSecretDrawerDataIsGUIOnly`, `TestKindSecretDataSave`) |
| Create (`object.create`) | discovery `get`; per object `create` with `dryRun=All`, then for real | Mutating; agents always ask for RBAC objects | New objects in order; a failure is reported and the next one tried; a Secret in an agent's or the CLI's text is an item error, and a Create tab or `editor.setText` holding one is refused. An object whose namespace comes from the namespace filter lands only in the namespace the confirmation named |
| Create dialogs (`namespace.create`, `serviceAccount.create`, `role.create`, `clusterRole.create`, `roleBinding.create`, `clusterRoleBinding.create`, `resourceQuota.create`, `secret.create`; [`commands/createdialogs.go`](../../internal/engine/commands/createdialogs.go)) | discovery `get`; `create` of the kind, with `dryRun=All` first; a binding dialog reads `roles`, `clusterroles` and `serviceaccounts` from the cache. A role with rules the user lacks needs `escalate`, a binding to one `bind`, the dry run too; Cogpit declares neither | Mutating; `clusterRoleBinding.create` Destructive; `roleBinding.create` Destructive for `cluster-admin` or a cached role allowing every verb on every resource of every group; agents always ask for roles and bindings (`TestAgentRoleBindingsAlwaysAsk`); `secret.create` GUI only | One object, checked before sending and bound to the confirmed context and object; greyed "not permitted" when the access reviews deny `create` (`CreatePermitted`, `TestCreateDialogsFollowAccessReviews`). The confirmation's rows name a binding's role and subjects, a role's rules (`Effects`; `TestCreateDialogsAgentsNeedAcceptance`). What the every-origin rule misses: [H-27](#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache). Create Secret: [credentials](credentials.md#secret-values-in-the-create-secret-dialog) (`TestSecretCreateIsGUIOnly`, `TestSecretCreateErrorsNeverQuoteValues`) |
| Delete (`object.delete`, `object.deleteMany`) | `delete`, one per object | Destructive | The object and its garbage-collected dependents; one the cache did not hold at the confirmation is refused. A selection's dialog lists every object; a failure is reported while the others go on; a denied access review on any object greys the action (`TestDeleteManyOneConfirmation`, `TestDeleteManyPartialFailure`, `TestDeleteManyNeedsTheAgentsAcceptance`, `TestDeleteManyJobCancelled`, `TestKindDeleteSelection`, `TestKindDeleteSelectionAsAJob`) |
| Force delete (pod) | none: not built, no `delete` with grace period 0 | – | – |
| Remove finalizers (`object.removeFinalizers`, [D-98](../developer/decisions.md#d-98-what-holds-a-terminating-namespace-and-remove-finalizers)) | one JSON patch: `test /metadata/uid`, `test /metadata/finalizers` (exactly those confirmed, in order), `remove /metadata/finalizers`; never `/finalize`, `spec.finalizers`, namespaces or custom resource definitions | Destructive | Every finalizer of an object being deleted, so it goes at once without its controllers' cleanup ([H-9](#h-9-force-finalize-skips-cleanup)); a changed object or finalizer list fails a `test` and nothing is removed (`TestRemoveFinalizersSendsTheGuardedPatch`, `TestRemoveFinalizersRefusals`, `TestKindTerminatingNamespaceBlockers`) |

### Nodes

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Cordon, uncordon (`node.cordon`, `node.uncordon`) | `patch nodes`, never an `update` | Mutating | `spec.unschedulable` |
| Drain (`node.drain`) | `patch nodes`; discovery `get /api/v1`; `list pods` in all namespaces (field selector `spec.nodeName`); `get daemonsets.apps`; `create pods/eviction` per pod; `get pods` while waiting; never `delete pods` | Destructive | A cordon, then every evictable pod evicted (kubectl's drain library). Defaults ([`engine/drainjournal.go`](../../internal/engine/drainjournal.go) `DrainSettings`): DaemonSet pods left; pods with emptyDir data or without a controller refuse the drain unless `deleteEmptyDirData` or `force`, which the confirmation names; each pod's grace period; 300 s. A job: `job.cancel` (any origin, [H-111](agents-and-mcp.md#h-111-an-agent-can-stop-a-drain-the-user-confirmed)), closing its tab or the cluster's disconnect or interruption stop it between requests, uncordoning nothing. `<user config dir>/Cogpit/drains.json` (0600) records it before the cordon: a crash, failure or stop leaves a cordoned node that the next connect offers to finish, uncordon or dismiss (`TestKindDrainCrashFinishUncordon`) |
| Node shell (`node.shell`; `cluster.settings.setNodeShell` sends nothing) | in `kube-system`: `create pods`; `get`, `watch` on that pod; `create pods/exec`; `delete pods`; the access reviews are asked there (`Verb.Namespace`, `TestNodeShellFollowsAccessReviews`) | Destructive | A privileged pod, root on the node for at most the maximum duration ([below](#node-shell), [H-7](#h-7-node-shell-is-root-on-the-node); `TestNodeShellPodAndConfirmation`, `TestNodeShellPodGoesWithItsTab`, `TestKindNodeShell`) |
| Node shell pods a crash left (no command: each connect; `NodeShellCleanupVerbs`) | in `kube-system`: `delete pods` with the journaled UID; `get pods` by name for an entry without one | Not asked: only Cogpit's own pods, audited as `cogpit.nodeShellCleanup` | Those pods deleted; another UID, a gone pod or another live Cogpit process's are left alone (`TestNodeShellSweepDeletesOwnPodsOnly`, [H-71](#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)) |

### Flux and Argo CD

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Flux actions (`flux.reconcile`, `flux.reconcileWithSource`, `flux.reset`, `flux.suspend`, `flux.resume`; on a selection `flux.reconcileMany`, `flux.reconcileWithSourceMany`, `flux.resetMany`, `flux.suspendMany`, `flux.resumeMany`; [D-33](../developer/decisions.md#d-33-the-flux-actions)) | `patch` on the object's Flux kind, a JSON merge patch (`TestKindFlux`); Reconcile with Source also on its sources (Git, OCI and Helm repositories but OCI Helm ones, buckets, Helm charts) in their namespaces | Mutating | Reconcile: `reconcile.fluxcd.io/requestedAt` (with `force` also `forceAt`: a Helm release installs or upgrades, hooks included, even without a change); Reset: `resetAt` (a Helm release's failure counts); Suspend, Resume: `spec.suspend`. Reconcile with Source is a job that waits through the cache, at most 5 minutes, for each source to be ready before the next; a suspended or missing source refuses the whole action ([`commands/fluxsourcejob.go`](../../internal/engine/commands/fluxsourcejob.go)). The controller then acts with its own identity ([H-64](#h-64-a-flux-action-is-carried-out-with-the-controllers-identity)) |
| Argo CD Refresh (`argocd.application.refresh`, `argocd.application.refreshMany`) | `patch applications.argoproj.io` | Mutating | The annotation `argocd.argoproj.io/refresh` (`normal`, `hard`). Cogpit never talks to Argo CD's API server |
| Argo CD Sync (`argocd.application.sync`, `argocd.application.syncMany`) | `patch applications.argoproj.io` with `metadata.resourceVersion` too ([D-36](../developer/decisions.md#d-36-how-argo-cds-sync-writes-the-operation)), three attempts at most after conflicts; refused while an operation is requested or running | Mutating; Destructive with prune | The `operation` as `argocd app sync` writes it (`initiatedBy: cogpit`), which Argo CD's controller runs with **its** service account, applying and pruning what the Application's project allows; refused inside a deny sync window ([H-67](#h-67-a-sync-acts-with-argo-cds-rights-not-the-users)) |

### Helm

| Action | Requests | Danger | What changes |
|---|---|---|---|
| Helm rollback, uninstall (`helm.release.rollback`, `helm.release.uninstall`) | the Helm SDK with the user's context: `list`, `get`, `create` (rollback), `update`, `delete` on the release records (`secrets`, or `configmaps` for that driver, the access check asking for the release's own: `TestHelmVerbsFollowTheReleasesDriver`, `TestHelmConfigMapReleaseActions`, `TestActionsUseTheReleasesDriver`, `TestKindHelmConfigMapDriver`), discovery, and whatever its manifests and hooks need | Destructive | The release's objects and records; 10 revisions kept, as `helm rollback` and Lens keep them. Bound to the newest record's UID and refused when the server holds a newer revision (`TestActionsRefuseWhenTheServerMoved`); the newest revision as target applies its manifest again (`TestRollbackDialogCurrentRevision`, `TestHelmRollbackAndUninstall`). The window's Rollback dialog is the confirmation, its answer used once, within 10 s, for that release, context and revision (`TestRollbackDialogAnswerBinding`). `<user config dir>/Cogpit/helm.json` (0600) records each action before the SDK runs; an interrupted one is offered on the next connect ([D-11](../developer/decisions.md#d-11-crash-stance); `TestHelmCrashJournalOffer`, `TestKindHelmReleases`). The SDK runs to its 5-minute timeout |
| Helm upgrade (`helm.release.upgrade`; `helm.release.upgradeTab` opens the tab) | the Helm SDK: `list`, `create`, `update` and, past 10 revisions, `delete` on the release records; `get` and `patch` (server-side apply) on its objects, `create` and `delete` for objects added or dropped; discovery (`TestKindHelmUpgrade`, `TestKindHelmUpgradeFromRepository`). Another chart version is first downloaded from a repository of Helm's repositories file | `upgrade` Mutating, agents always ask (`AgentsAsk` in [`commands/helmupgrade.go`](../../internal/engine/commands/helmupgrade.go), `TestHelmUpgradeAgents`); `upgradeTab` UI | A new revision with the new values and the release's chart or another version a repository lists, checked against the index's SHA-256 when it has one (provenance is not: [H-30](trust-boundaries.md#h-30-chart-provenance-is-not-verified)). The confirmation names the versions, the repository, the digest check (`ErrTargetsChanged` when they changed) and an agent's inline values ([D-27](../developer/decisions.md#d-27-an-agents-helm-values-in-the-confirmation); `TestHelmUpgradeFromARepositoryAgents`, `TestHelmUpgradeRepositoryRefusals`); no preview renders them ([H-31](#h-31-an-agents-helm-values-can-copy-what-lookup-reads)). Bound to the revision the values were read from, rechecked right before the SDK runs (`TestHelmUpgradeRefusedWhenTheReleaseMoved`, `TestHelmActionsRefusedWhenTheServerMovedUnseen`, `TestHelmUpgradeTabSaves`). Values are scrubbed from errors, toasts, the journal and the SDK's log, best effort ([`helm/values.go`](../../internal/helm/values.go) `ScrubValues`; `TestUpgradeScrubsValuesFromErrors`, `TestScrubValuesEscapedForms`, `TestRedactSDKLog`), and audited by size; a failed upgrade's error is withheld from agents and the CLI (H-31). An interrupted upgrade is offered as a rollback to the revision it started from (`TestHelmUpgradeCrashJournal`, `TestHelmUpgradeRepositoryCrashJournal`) |
| Helm install (`helm.chart.install`, `helm.chart.preview`; `helm.chart.installTab` opens the tab) | the Helm SDK. The window's preview (`--dry-run=server`): discovery, a `get` per rendered object and what the templates `lookup`, nothing written; an agent's or the CLI's renders offline, no request and no `lookup` (the built-in agent over a private connection previews as the window, [H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows); `TestKindHelmInstall`). The install: `list`, `create`, `update` on the release records, a server-side apply (`patch`, `create`) of every rendered object, `create customresourcedefinitions` for `crds/`, the namespace with `createNamespace`, and the hooks' objects | `install` Mutating, agents always ask (`AgentsAsk` in [`commands/helminstall.go`](../../internal/engine/commands/helminstall.go), `TestHelmInstallAgents`); `preview` Read; `installTab` UI | A new release and every object of the chart, made with the user's identity; the chart's templates run in Cogpit's process ([trust boundaries](trust-boundaries.md#boundaries-and-required-checks)) and can copy through `lookup` what the user may read into an object. Digest check, scrubbing and audit as for an upgrade (an unverified archive is said so; `TestRepositoryRefusesWhatDoesNotFit`, `TestInstallRefusalsAndScrubbing`, `TestHelmInstallFailures`); refused when the release exists (`TestHelmInstallFromTab`). After an interrupted install nothing is offered: the banner shows the release's state, the user uninstalls it ([`engine/helmjournal.go`](../../internal/engine/helmjournal.go); `TestHelmInstallCrashJournal`, `TestHelmBannerOffersNothingAfterAnInstall`). The `configmap` storage driver makes values readable by whoever may read ConfigMaps in the namespace: only the user chooses it (`ErrHelmDriverGUIOnly`; `TestHelmDriverPreference`, `TestHelmInstallUsesTheDriverPreference`) |

## Node shell

As in Lens ([cluster settings](https://docs.lenshq.io/k8slens/cluster/cluster-settings/)), the node
shell is a privileged pod in `kube-system` sharing the host PID namespace, which the Baseline
[Pod Security Standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
forbids; [`kubectl debug node/<name>`](https://kubernetes.io/docs/tasks/debug/debug-cluster/kubectl-node-debug/)'s
profile, which shares the host namespaces and mounts the root filesystem, violates it too
([D-37](../developer/decisions.md#d-37-the-node-shell),
[`engine/nodeshell.go`](../../internal/engine/nodeshell.go)).

- **Confirmation.** Destructive for every origin; its rows name the privileged pod in `kube-system`
  on the node, root access, the image and its digest (or "a floating tag"), the pull secret and when
  the pod ends at the latest (`NodeShellEffects`). A changed image, pull secret or maximum duration,
  a node created again under its name or another context refuses it before anything is created
  (`StartNodeShell`; `TestNodeShellNeedsEveryOriginsConfirmation`,
  `TestNodeShellBoundToWhatWasConfirmed`).
- **Image.** Cluster Settings › Node Shell; the default `DefaultNodeShellImage` is the newest
  supported Alpine (3.24) pinned by its index digest, which Renovate moves for a new release or a
  rebuilt tag once CI's kind node shell passes
  ([H-72](#h-72-the-default-node-shell-image-ages-with-the-build-that-pins-it)). Only the user sets
  the image and the pull secret; agents and the CLI may reset them
  ([D-38](../developer/decisions.md#d-38-who-sets-the-node-shells-image), `ErrNodeShellGUIOnly`).
- **Pod.** `nodeName`, a toleration `operator: Exists`, `hostPID: true`, `privileged: true`, no
  service account token, no service links, `restartPolicy: Never`, a 1-second grace period, labels
  `app.kubernetes.io/name: cogpit-node-shell` and `app.kubernetes.io/managed-by: cogpit`, the name
  `cogpit-node-shell-<8 random characters>`, the annotation `cogpit.io/node-shell-id` (H-71). The
  shell is `nsenter -t 1 -m -u -i -n -p`, then the node's bash (else sh) as root
  ([`terminal/nodeshell.go`](../../internal/terminal/nodeshell.go)).
- **Lifetime.** `activeDeadlineSeconds` and the container's `sleep` are Cluster Settings › Node
  Shell › Maximum duration: 2 hours, or whole minutes from 5 minutes to 24 hours, set only by the
  user ([`engine/nodeshell_duration.go`](../../internal/engine/nodeshell_duration.go);
  `TestNodeShellMaxDurationSetting`). The pod is deleted with its UID when the shell ends, its tab
  closes or Cogpit quits (10 s at most); one that cannot start within 3 minutes ends the session and
  is deleted; one a crash leaves goes at the next connect
  ([H-71](#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)).
- **`kube-system`** must allow privileged pods: under Baseline or Restricted the API server's
  refusal is the node shell's error.

## Debug container

Debug adds an ephemeral container through the pod's `ephemeralcontainers` subresource and attaches
to it, as `kubectl debug -it <pod> --image=<image> --target=<container>` does
([ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/),
[`engine/debugcontainer.go`](../../internal/engine/debugcontainer.go)):

- **Confirmation.** Destructive for every origin; rows Context › Namespace › Pod, Target, what the
  container sees of it, Image and Digest, and "This cannot be undone." (`DebugEffects`). The
  window's Debug dialog is the confirmation, its answer bound to the pod's UID, the target and the
  image; another pod under the name, another image or another context refuses it before anything is
  sent (`TestDebugNeedsEveryOriginsConfirmation`).
- **Image.** Preferences › Kubernetes › Debug container image; the default `DefaultDebugImage`,
  `nicolaka/netshoot:v0.16` pinned by its index digest, which Renovate moves as the node shell's.
  Only the user sets it (`ErrDebugImageGUIOnly`); an agent's `pod.debug` may name any image, which
  its confirmation shows with its digest or as a floating tag.
- **Container.** `debugger-<5 characters>`, stdin and a TTY, `terminationMessagePolicy: File`, the
  image's command, `targetContainerName` the picked container. No security context of its own: the
  image's user and the namespace's Pod Security level decide, and Pod Security admission, webhooks
  and RBAC refuse with their own message (`TestRealDebugContainer`).
- **Refused:** a pod that has ended or is being deleted, a target that is none of its containers or
  init containers, an image that is no reference.
- **Cache first.** Debug waits for the new container in the pod's watch (30 s at most); the Attach
  tab waits through the cache while the image is pulled (3 minutes at most).

## Commands that send no request

| Commands | What they touch instead |
|---|---|
| Helm's chart repositories (`helm.repo.list`, `helm.repo.update`) | Helm's own `repositories.yaml`, read, never written; the indexes come from those repositories with the credentials the file names ([architecture §8](../developer/architecture.md#8-helm-internalhelm)) |
| Kubeconfig sources (`kubeconfig.*`) | Cogpit's source list and approvals; what they let run locally: [credentials](credentials.md), [trust boundaries › H-1](trust-boundaries.md#h-1-kubeconfig-files-added-by-sync-folders) |
| Preferences, Cluster Settings (`preferences.*`, `cluster.settings.*`) | Cogpit's `preferences.json`, `cluster-settings.json`, `cluster-icons/`, never a kubeconfig (`TestClusterSettingsCommandsNeverTouchTheKubeconfig`); a terminal namespace goes into the terminal's own kubeconfig ([credentials](credentials.md#the-terminals-kubeconfig)); `preferences.setStartAtLogin` sets the app's macOS login item, on only from the window |
| Program folders (`preferences.addProgramFolder`, GUI only; `preferences.removeProgramFolder`; [D-120](../developer/decisions.md#d-120-folders-searched-for-credential-plugins)) | where Cogpit's own `PATH` looks for a credential plugin's programs, after its own folders ([credentials › H-6](credentials.md#h-6-exec-plugins-inherit-the-environment)) |
| Disconnect (`cluster.disconnect`) | ends the connection's informers, port forwards, logs and shells (`TestDisconnectAndWelcomeCommands`) |
| Remove from Cogpit (`cluster.remove`, GUI only) | Cogpit's link to a kubeconfig file or folder, never the file; its contexts lose their plugin approvals and cluster settings, the agents' write allowance included (`TestClusterRemove`, `TestRemovingASourceForgetsItsClustersSettings`) |
| Hotbar (`hotbar.*` but `hotbar.openCluster`), notifications (`notifications.*`) | Cogpit's `hotbar.json`; the session's memory of its toasts |

## Nothing installed for Cogpit's own features

Cogpit installs nothing into a cluster for its own features (metrics, agents, exporters): it reads
what the cluster runs, and installing is the user's action on their own charts
([AGENTS.md](../../AGENTS.md#never-surprise-a-cluster-outranks-feature-work)), so no namespace,
role or workload of Cogpit's gathers cluster-wide reads (H-105). Releases `cogpit-prometheus`,
`cogpit-kube-state-metrics` and `cogpit-node-exporter` in `cogpit-metrics`, which a former Cogpit
Metrics stack installed ([D-61](../developer/decisions.md#d-61-cogpit-metrics-the-bundled-metrics-stack)),
are the user's own Helm releases: the Helm Releases view uninstalls them, and detection reads that
Prometheus like any other ([H-26](#h-26-prometheus-is-found-by-name)).

## What this does not cover

### H-64: A Flux action is carried out with the controller's identity

Live wherever the user may `patch` a Flux kind: the patch is the user's, the reconciliation runs
with the controller's service account (often cluster-wide) unless the object names a
`spec.serviceAccountName` or the controller enforces impersonation. Reconcile and Resume apply what
the source holds now; a forced Helm release runs its hooks again. Cogpit adds no power, since who
may patch the object may already change what it applies; it becomes real where agent writes are
allowed ([H-14](agents-and-mcp.md#h-14-allowing-agent-writes-for-a-context)): a steered agent
([H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)) can then reconcile, resume or
force without a dialog. A confirmation names the identity in rows "Runs as" and "Your rights"
(`fluxIdentityEffects` in [`commands/fluxidentity.go`](../../internal/engine/commands/fluxidentity.go),
`TestFluxConfirmationNamesTheIdentity`). What remains: the name says whose rights are used, not what
they allow, and a controller's `--default-service-account` is not read.


### H-7: Node shell is root on the node

Live: whoever may create pods in `kube-system`, exec into them and confirm is root on any node
Cogpit lists, while the pod lives; the rules above bound and announce it, they cannot make it less
powerful. After a crash the pod lives until the next connect
([H-71](#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)) or its deadline, up to 24
hours where the user set that. Not built: a non-privileged `kubectl debug`-style profile (host
namespaces, the root filesystem at `/host`), which would still be root on the node's files.


### H-71: A crash leaves the node shell's pod until the next connect

Live. A crash, a `kill -9`, a sleeping laptop or a lost connection during the delete leave the pod
running, privileged and idle, until its deadline, after which its object stays `Failed`
(`DeadlineExceeded`); meanwhile whoever may exec into pods in `kube-system` can use it, as they
could a pod of their own.

What narrows it ([`engine/nodeshelljournal.go`](../../internal/engine/nodeshelljournal.go),
[`engine/nodeshellsweep.go`](../../internal/engine/nodeshellsweep.go)): each pod is recorded in
`<user config dir>/Cogpit/nodeshells.json` (owner-only, replaced atomically, under the journals'
lock) **before** its create is sent, with a random id the pod carries as `cogpit.io/node-shell-id`,
and gets its UID once created; a journal that cannot be written refuses the node shell. The
context's next connect deletes the pods its journal names by UID (an entry without one by name and
id), says so, and tries a refused delete again at the following connect
(`TestNodeShellSweepDeletesOwnPodsOnly`, `TestNodeShellJournalBeforeTheCreate`,
`TestFileNodeShellJournal`).

What remains: the pod lives until the next connect to that context with the same configuration
directory, or until its deadline; a user who never connects again, or lacks `delete pods` in
`kube-system`, leaves the `Failed` object. An entry without a UID on a connection that cannot read
single pods waits for the deadline. A process that can write the journal can name only a pod
carrying the id it names, which whoever may read pods in `kube-system` can copy: that delete is then
the user's own write where the attacker already creates pods.


### H-72: The default node shell image ages with the build that pins it

Live for every released build:

- **A build keeps its pin.** A Cogpit that is not updated runs its build's image, out of support
  once its Alpine branch is (3.24 on 2028-06-01), without the tag's later fixes.
- **Renovate's update waits for a release.** Its `build(deps):` commits are not released; the pin
  ships with the next `feat:` or `fix:`, and a flaky check holds the pull request back
  ([releasing › Renovate](../developer/releasing.md#renovate)).
- **Nothing scans the image.** CI checks that it runs `nsenter` on a node, not its advisories.

Only BusyBox's `sleep` and `nsenter` run from the image; the shell is the node's own bash. An image
the user sets is the user's to keep current.

### H-134: A debug container stays in the pod and sees its target

Live wherever the user may `patch pods/ephemeralcontainers`: Kubernetes never removes the container
before the pod goes and lets no one change it. With a target it reads that container's environment
and, through `/proc/<pid>/root`, its files, mounted Secrets and service account token; the default
image runs as root with network tools. Closing the Attach tab only detaches: the shell runs until
someone types `exit` or the pod is deleted, and whoever may attach to the pod reaches it. In the
way: the destructive confirmation with its target, what it sees and "This cannot be undone."; Pod
Security and admission policies (restricted refuses netshoot). What remains: Cogpit cannot end the
shell when the tab closes (Kubernetes stops an ephemeral container only through its own process),
and the image ages as the node shell's ([H-72](#h-72-the-default-node-shell-image-ages-with-the-build-that-pins-it)).


### H-8: Secret data arrives with the list

Listing or watching Secrets returns their data, and Cogpit watches them in full so that the
Secrets view shows Keys and Type as Lens does ([D-13](../developer/decisions.md#d-13-cache)):

- **Every value in memory while the scope runs.** The full watch holds every Secret of the
  namespaces it covers while the Secrets view, a Secret's drawer or an agent's read of kind
  `secrets` uses it, plus the warm period (5 minutes by default); Helm's releases read it too, and
  their own `owner=helm` watch takes over within a minute of its end.
- **RBAC cannot narrow `list secrets`:** a user who may list Secrets transfers every value.
- **Wiping.** The drawer decodes each value to measure it and zeroes the bytes; a reveal hands a Go
  string to the GUI. The cache's base64 strings and a revealed string wait for the garbage
  collector.
- **What agents get.** Names, namespaces, labels, annotations but last-applied, key names, the type,
  and per key its size and whether it is text: never a value (`model.Redact`, the drawer's masked
  Data section, `secret.reveal` GUI-only), on every read path an agent has (`kinds.list`,
  `resources.list`, `resources.find` with and without a kind, `object.get`, `ui.openView`,
  `ui.navigate`, `ui.state`, `table.exportCsv`, `workloads.overview`), the audit log, events and
  the log (`TestAgentsReadSecretsRedacted`). The built-in agent over a connection the user marked
  private reads as the window does
  ([agents and MCP › H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)).
  The last-applied annotation is decoded before the cache's transform drops it; one that reaches
  the drawer another way reads `<redacted>`.
- **Helm's release Secrets** hold values (credentials, often) and manifests that render Secrets: the
  newest revision of each stays decoded in the releases' memo
  ([above](#on-connect-and-while-browsing)), shown in the GUI, withheld from agents. The Revisions
  tab ([D-131](../developer/decisions.md#d-131-a-releases-revisions)) keeps an older revision's
  decoded text until it shows another or closes, a rendered Secret masked until the user's Show,
  which only the window holds. Agents read values and rendered Secrets withheld, the changed
  values' key paths only
  ([agents and MCP › H-144](agents-and-mcp.md#h-144-a-changed-values-key-path-reaches-agents)), the
  manifest's other objects through the agent view (H-82).
- **Helm's release ConfigMaps** (the ConfigMaps driver) carry the same payload in `data.release`,
  decoded under the same rules. A ConfigMap labelled `owner=helm` has it redacted in every YAML
  (`model.Redact`), its drawer, lists, CSV and search show its size or key only, and an agent can
  neither read nor write it through an Edit or Create tab (`TestHelmConfigMapPayloadsKeptFromAgents`,
  `TestBudgetHelmReleasesWithConfigMaps`, `TestKindHelmConfigMapDriver`). The label alone decides: a
  ConfigMap Helm did not write but labelled `owner=helm` is treated as a record too.

### H-9: Force finalize skips cleanup

Removing finalizers deletes the object without its controllers' cleanup (cloud load balancers,
volumes, DNS records), and Cogpit cannot know what a finalizer guards. Live wherever the user may
`patch` an object being deleted (`object.removeFinalizers`, offered on such an object and in a
Terminating namespace's Blocking Deletion list). Bounded by: the destructive confirmation names the
object, each finalizer and a warning that the controllers' cleanup is skipped; the patch removes
exactly those finalizers from exactly that object, or nothing; a namespace's own finalizer is never
removed and `/finalize` never called, so a namespace goes only once its controller found it empty.
The rest is the user's judgement. Deleting finalizers in an object's YAML is an apply like any
other.


### H-22: Two Cogpit processes share the journals

Every Cogpit process of the user shares the drain, Helm and node shell journals (`drains.json`,
`helm.json`, `nodeshells.json`) in the user's config directory. A lock on `<dir>/journals.lock`
around every read-modify-write (a writer that waits more than 10 s gets `ErrJournalBusy`, and the
action does not start) and an owner file each process keeps locked for its life
([architecture §8.6](../developer/architecture.md#86-journals),
[`journallock.go`](../../internal/engine/journallock.go); `TestJournalsKeepEveryProcessesEntries`,
`TestJournalEntryOfAKilledProcess`) keep the processes from losing each other's entries. The lock
is `flock` on macOS and Linux and `LockFileEx` on Windows, where the owner file shares its deletion,
so its folder can be removed while Cogpit runs, and a journal's rename retries for up to a second
while another process holds the file open. A running action of another live process shows as
running "in another Cogpit window" and is neither offered, finished nor dismissed here; a node
shell pod it recorded is left to it ([H-71](#h-71-a-crash-leaves-the-node-shells-pod-until-the-next-connect)).
Open:

- A config directory on a network file system, where `flock` may not hold across machines.
- A missing owner file counts as an ended process (`ownerFileAlive`): an `owners/` folder removed
  while Cogpit runs makes its running actions look interrupted to another Cogpit process.
- Entries are keyed by context id, the context's name and its file (`SourceContextID` in
  [`kubeconfigs.go`](../../internal/engine/kubeconfigs.go)): the same cluster reached through
  another context or file, or a renamed file of a sync folder, does not see them.
- Helm itself does not refuse a second rollback of a release by another tool: the SDK's `Rollback`
  has no pending check, unlike its `Upgrade`.
- Dormant: a build for a platform without `flock` or `LockFileEx` gets no lock
  ([`journallock_other.go`](../../internal/engine/journallock_other.go)).

### H-26: Prometheus is found by name

Live for every cluster with the Prometheus integration on and no address set. Detection probes at
most three cached Services ranked by well-known names, Thanos's and VictoriaMetrics' after
Prometheus's ([D-71](../developer/decisions.md#d-71-thanos-and-victoriametrics-read-as-prometheus)),
and takes the first that answers `GET /api/v1/status/buildinfo` like a Prometheus (`promNames` in
[`promdetect.go`](../../internal/metrics/promdetect.go),
[architecture §9](../developer/architecture.md#9-metrics-internalmetrics)). A name proves nothing:
whoever may create a Service can take one, any name ending in `prometheus-server` included, and a
higher-ranked name in any namespace wins (`monitoring`, `prometheus` and `lens-metrics` only break
ties).

- **What a lookalike learns:** the names of the namespaces, pods and nodes the user opens, and the
  nodes' addresses ([`promql.go`](../../internal/metrics/promql.go)); no credential or Secret value.
- **What it makes Cogpit say:** false numbers in charts and columns. Failures are a class
  ([above](#on-connect-and-while-browsing)), but label values are not checked: where Prometheus
  serves `metrics.get`, the namespace, pod, container and node names an agent reads are the
  answer's (`podUsages` in [`prom.go`](../../internal/metrics/prom.go)), text a lookalike chooses
  ([H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)).
- **The user's check:** the source row's tooltip names the provider and its service
  (`Prometheus monitoring/prometheus-server:http`, likewise Thanos and VictoriaMetrics), and the
  status line of Cluster Settings › Prometheus links its namespace and service. Use HTTPS
  authenticates no service: the TLS connection is the API server's proxy's, which does not verify
  the service's certificate (`InsecureSkipVerify` in kube-apiserver's `CreateProxyTransport`).
- **Closing it per cluster:** `cluster.setPrometheus` pins a service and turns detection off, kept
  in `metrics.json` (owner-only), checked to be a namespace, service, port and path prefix without
  `.` or `..`; an agent's call always waits for the user, whatever the allowance of agent writes,
  the confirmation naming the service and what it sees (`AgentsAsk` in
  [`metricsprometheus.go`](../../internal/engine/commands/metricsprometheus.go),
  `TestAgentSetPrometheusAsks`; [H-14](agents-and-mcp.md#h-14-allowing-agent-writes-for-a-context)).

Not a lookalike: a real Thanos Querier or VictoriaMetrics holding several clusters' series answers
with all of them, since Cogpit's queries carry no cluster label, so the Overview's sums can count
other clusters' nodes; anyone who may use its service proxy can query it anyway.

### H-121: Alertmanager is found by name, and its texts reach the window and agents

Live for every cluster with the Alertmanager integration on (the question's Check all integrations
turns it on) and no address set. Detection asks at most three cached Services and takes the first
whose answer to `GET /api/v2/alerts` reads as Alertmanager's list (`amNames` in
[`detect.go`](../../internal/alerting/detect.go)): `<release>-kube-prometheus-alertmanager` 100,
`alertmanager-main` 95, `alertmanager-operated` 90, `vmalertmanager-<name>` 85, any
`<release>-alertmanager` 80, `alertmanager` 70 in `monitoring`, `prometheus` or `alertmanager`, 40
elsewhere; the namespaces only break ties. As with [H-26](#h-26-prometheus-is-found-by-name), a
higher-ranked lookalike wins over the cluster's own (an `x-kube-prometheus-alertmanager` in a
team's namespace over a bare `alertmanager-operated`).

A lookalike writes text the user and agents read: the pill's count and colour, the board's
summaries and descriptions, the objects its labels name (links into Cogpit's own views only) and
the runbook links, opened in the browser on a click (absolute http and https URLs without user
information only, [trust boundaries](trust-boundaries.md#boundaries-and-required-checks)). The
request carries nothing of the user's: no parameter but the three filters, and the API server drops
the user's `Authorization` header before it proxies. What makes it real: someone who may create
Services but not read the monitoring namespace, and a user who follows a runbook link to a page
that asks for something.

The cluster's own Alertmanager is no more trusted: summaries and descriptions are the alerting
rules' templates, filled with label values that whoever runs a workload sets through the metrics it
exports. Cogpit shows them as text, never markup, and hands them to agents as data without a frame
([H-11](agents-and-mcp.md#h-11-instructions-hidden-in-cluster-data)).

The user's check: the status line of Cluster Settings › Alertmanager names the service in use, its
namespace and service as links (`Found automatically: monitoring/kube-prometheus-alertmanager:9093`);
the board and the pill do not (agents read it as `where` in `alerts.board` and `alerts.list`).
`cluster.setAlertmanager` pins the service and turns detection off, kept in `metrics.json`
(owner-only) and checked as Prometheus's; an agent's call always waits for the user
(`TestAlertCommands`). Would narrow it, not decided: a mark on the board for an Alertmanager found
outside the preferred namespaces; a frame around alert texts for agents like a chart README's
([H-86](agents-and-mcp.md#h-86-a-charts-readme-reaches-agents)).

### H-27: A binding is judged by its role's name and the cache

The rule that agents always ask ([above](#actions-that-write)) is declared per call (`AgentsAsk` in
[`registry.go`](../../internal/engine/registry.go); `RBACKind`, `TextHoldsRBAC` and
`EditorHoldsRBAC` in [`agentsask.go`](../../internal/engine/agentsask.go); `NeedsConfirmation` in
[`engine.go`](../../internal/engine/engine.go)), and the confirmation says that the allowance of
agent writes ([H-14](agents-and-mcp.md#h-14-allowing-agent-writes-for-a-context)) does not skip the
question.

| An agent's path to RBAC | Waits for the user | Evidence |
|---|---|---|
| `role.create`, `clusterRole.create`, `roleBinding.create` | always | `TestCreateDialogsAgentsNeedAcceptance` |
| `clusterRoleBinding.create` | always, for every origin (destructive) | [`createdialogs.go`](../../internal/engine/commands/createdialogs.go) |
| `object.create` (documents, items of a v1 List) | when one is an RBAC kind, matched ignoring case; text that does not parse asks, then is refused | `TestAgentRoleBindingsAlwaysAsk`, `TestTextHoldsRBAC` |
| `object.apply` (from `yaml`, or the tab's text after `editor.setText`) | when the tab edits an RBAC object or the text holds one | `TestAgentRoleBindingsAlwaysAsk` |
| `helm.chart.install`, `helm.release.upgrade` | always, whatever the chart renders; rollback and uninstall are destructive | `TestHelmInstallAgents`, `TestHelmUpgradeAgents` |
| `argocd.application.sync`, the Flux actions and their `…Many` forms | **no**: the controller writes what its source holds, bindings included, with its own identity | [H-64](#h-64-a-flux-action-is-carried-out-with-the-controllers-identity), [H-67](#h-67-a-sync-acts-with-argo-cds-rights-not-the-users) |

Open, live today:

- **Which grant asks every origin is judged by the role's name and the cache.** `roleBinding.create`
  is confirmed for every origin, the GUI too, only when it binds the cluster role `cluster-admin` by
  name, or a role the cache already holds whose rules allow every verb on every resource of every
  group (`grantsAll`; it starts no watch). Missed: `admin` or `edit`, a role that grants nearly
  everything without the triple wildcard (`pods/exec` and `secrets`), a role not cached yet, a role
  changed after the check (the binding names the role, not its rules).
- **The every-origin confirmation is the dialogs' alone.** `clusterRoleBinding.create` and
  `roleBinding.create`'s escalation ask the user; the window's Create and Edit tabs
  (`object.create`, `object.apply`) write any binding, a cluster role binding or one to
  `cluster-admin` included, without a question.

The API server still refuses a binding to a role whose permissions the user does not hold unless
the user may `bind` it: without `bind`, a binding hands on only what the user's identity holds.

### H-31: An agent's Helm values can copy what lookup reads

Live for `helm.chart.install` and `helm.release.upgrade` started by an agent. A chart that renders
values through `tpl` lets the values call `lookup`, which reads whatever the user may read, Secrets
included; the accepted action can write what it read into an object the chart creates (a
ConfigMap's data, a label), which the agent then reads like any other. Every such call waits for
the user ([H-27](#h-27-a-binding-is-judged-by-its-roles-name-and-the-cache)) and the confirmation
shows an agent's inline values
([D-27](../developer/decisions.md#d-27-an-agents-helm-values-in-the-confirmation)), where a
`lookup` or a `tpl` is visible to a user who reads them; nothing flags it. An agent's preview
renders offline and cannot `lookup`, except over a private connection, where it renders as the
window's.

The action's error can quote what `lookup` read:

- The command's result and `helm.pendingOperations` withhold it from agents and the CLI
  (`forCaller`, `withheldHelmError` in [`commands/helm.go`](../../internal/engine/commands/helm.go),
  `TestHelmInstallErrorStaysInTheWindow`); the built-in agent over a private connection reads it in
  full ([H-94](agents-and-mcp.md#h-94-a-private-connection-gives-the-model-what-the-window-shows)).
- A failed Helm action's toast, whoever started it, reaches agents in `notifications.list` as text,
  the action's inline values of 4 bytes or more removed and the cached Secrets' values of 10 bytes
  or more replaced (`EmitWindowToast`, `Notifications` in
  [`notifications.go`](../../internal/engine/notifications.go);
  [H-82](agents-and-mcp.md#h-82-secret-values-printed-into-logs-reach-agents)): a ConfigMap's
  data, a Secret the cache does not hold, a shorter value or one the chart transformed passes.
  Would close it, not decided: withholding Helm's error toasts from agents.

### H-67: A Sync acts with Argo CD's rights, not the user's

Live for `argocd.application.sync` and `argocd.application.syncMany`. Cogpit's request needs only
`patch` on the Application; the sync, and with prune its deletions, are run by Argo CD's
application controller with its own service account, in whatever destination the Application's
project allows. Whoever may patch Applications in Argo CD's namespace, or an agent the user allowed
writes, can so change objects in namespaces and clusters their own identity cannot touch, and
Cogpit's access reviews, which ask only about the patch, say nothing about it. Argo CD's RBAC,
which its API server checks for `argocd app sync`, is not emulated (accepted). What stands in the
way:

- **Sync windows.** Cogpit checks the AppProject's windows as Argo CD 3.5.5's `CanSync` checks a
  manual sync (`kinds.ArgoSyncBlocked` in
  [`argocd_syncwindow.go`](../../internal/model/kinds/argocd_syncwindow.go),
  `TestArgoSyncRespectsSyncWindows`): inside a deny window Sync is greyed with the reason and the
  write refused, since an operation written there waits in the controller until someone terminates
  it; a window Cogpit cannot parse blocks. The project is looked up in the namespace the controller
  runs in (`status.controllerNamespace`, `ArgoProjectNamespace` in
  [`argocd.go`](../../internal/model/kinds/argocd.go)).
- **Prune.** A sync with prune is destructive and confirmed for every origin; the confirmation names
  the resources the Application's status marks `requiresPruning` (the controller's last comparison,
  which can be older than Git's latest state), ten by name and the count of the rest.

Open: a project Cogpit may not read, or does not find, blocks nothing on Cogpit's side (the
controller still honours its windows), and an Application outside Argo CD's namespace that the
controller has not reconciled yet has no `status.controllerNamespace`, so Cogpit looks for its
project in the Application's own namespace
([`argosyncwindow.go`](../../internal/engine/commands/argosyncwindow.go)).
