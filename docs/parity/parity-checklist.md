# Parity checklist

> **Generated** by `go run ./hack/parity` from the checklists; do not edit. `make parity` (CI)
> fails when this page no longer matches them. Method and rules: [README](README.md);
> the assessment: [target-app-parity](target-app-parity.md); the work list: [gaps](gaps.md).

Every row of the checklists that carries a status, in one place: each Lens Desktop view, action,
interface and platform row, and Cogpit's own rows beyond Lens, with its status and the section
of the checklist that holds its evidence. The status is the row's first word there.

## Counts

| What | Rows | Exist | Done | Partial | Missing |
|---|---|---|---|---|---|
| Resource views (free) | 64 | 64 | 1 | 63 | 0 |
| Resource actions (free; exist = registered commands) | 42 | 42 | – | – | 0 |
| Interface rows, free plan | 122 | 119 | 9 | 110 | 3 |
| Interface rows, paid plans | 12 | 8 | 0 | 8 | 4 |
| Platform rows (packages, authentication, versions, tools, display) | 38 | 21 | 0 | 21 | 17 |
| Beyond Lens (not in feature breadth) | 74 | 73 | 61 | 12 | 1 |

Not counted: 10 interface rows that are not a target, 5 platform rows that are paid or have nothing to build.

## Resource views and actions

| Row | Status | Section |
|---|---|---|
| List view per kind the cluster serves and the user may list; hidden otherwise | partial | [Every kind](resource-parity.md#every-kind) |
| Generic columns: Name, Namespace (namespaced kinds), Age; plus additionalPrinterColumns for custom resources | partial | [Every kind](resource-parity.md#every-kind) |
| Detail drawer: metadata, labels, annotations, owner references, events of the object; Lens merges metadata and the kind's state into one Pr… | partial | [Every kind](resource-parity.md#every-kind) |
| Copy name (object.copyName) in the detail drawer | partial | [Every kind](resource-parity.md#every-kind) |
| Edit (object.edit): YAML in a dock tab, offered when the user may patch | partial | [Every kind](resource-parity.md#every-kind) |
| Delete (object.delete): confirm, offered when the user may delete; acts on the selection | partial | [Every kind](resource-parity.md#every-kind) |
| Row menu (⋮): Show Details, Edit, Delete for a custom resource; the kinds' own actions are added for built-in kinds (capture, 2026-10-11, o… | partial | [Every kind](resource-parity.md#every-kind) |
| Right-click on a row opens the actions menu (since 2025.10.230725) | done | [Every kind](resource-parity.md#every-kind) |
| Overview | partial | [Cluster](resource-parity.md#cluster) |
| Applications | partial | [Cluster](resource-parity.md#cluster) |
| Nodes | partial | [Cluster](resource-parity.md#cluster) |
| Namespaces | partial | [Cluster](resource-parity.md#cluster) |
| Events | partial | [Cluster](resource-parity.md#cluster) |
| Overview | partial | [Workloads](resource-parity.md#workloads) |
| Pods | partial | [Workloads](resource-parity.md#workloads) |
| Deployments | partial | [Workloads](resource-parity.md#workloads) |
| Daemon Sets | partial | [Workloads](resource-parity.md#workloads) |
| Stateful Sets | partial | [Workloads](resource-parity.md#workloads) |
| Replica Sets | partial | [Workloads](resource-parity.md#workloads) |
| Jobs | partial | [Workloads](resource-parity.md#workloads) |
| Replication Controllers | partial | [Workloads](resource-parity.md#workloads) |
| Cron Jobs | partial | [Workloads](resource-parity.md#workloads) |
| Config Maps; checked against a real cluster, 2026-10-16: 210 config maps, Keys naming as many keys as kubectl's Data counts (165 for one) | partial | [Config](resource-parity.md#config) |
| Secrets; checked against a real cluster, 2026-10-16: 261 secrets, Type and Keys as kubectl's Type and Data; a TLS Secret's drawer decodes a… | partial | [Config](resource-parity.md#config) |
| Resource Quotas | partial | [Config](resource-parity.md#config) |
| Limit Ranges | partial | [Config](resource-parity.md#config) |
| Horizontal Pod Autoscalers | partial | [Config](resource-parity.md#config) |
| Vertical Pod Autoscalers | partial | [Config](resource-parity.md#config) |
| Pod Disruption Budgets | partial | [Config](resource-parity.md#config) |
| Priority Classes | partial | [Config](resource-parity.md#config) |
| Runtime Classes | partial | [Config](resource-parity.md#config) |
| Leases | partial | [Config](resource-parity.md#config) |
| Mutating Webhook Configurations | partial | [Config](resource-parity.md#config) |
| Validating Webhook Configurations | partial | [Config](resource-parity.md#config) |
| Mutating Admission Policies | partial | [Config](resource-parity.md#config) |
| Mutating Admission Policy Bindings | partial | [Config](resource-parity.md#config) |
| Validating Admission Policies | partial | [Config](resource-parity.md#config) |
| Validating Admission Policy Bindings | partial | [Config](resource-parity.md#config) |
| Services | partial | [Network](resource-parity.md#network) |
| Endpoint Slices | partial | [Network](resource-parity.md#network) |
| Endpoints | partial | [Network](resource-parity.md#network) |
| Ingresses | partial | [Network](resource-parity.md#network) |
| Ingress Classes | partial | [Network](resource-parity.md#network) |
| Network Policies | partial | [Network](resource-parity.md#network) |
| Port Forwarding | partial | [Network](resource-parity.md#network) |
| Gateway API › Gateway Classes, Gateways, HTTP Routes, Backend TLS Policies, gRPC Routes, Listener Sets, Reference Grants (7 views; sidebar… | partial | [Network](resource-parity.md#network) |
| Gateway API › TCP Routes, TLS Routes, UDP Routes (3 views; experimental channel) | partial | [Network](resource-parity.md#network) |
| Persistent Volume Claims | partial | [Storage](resource-parity.md#storage) |
| Persistent Volumes | partial | [Storage](resource-parity.md#storage) |
| Storage Classes | partial | [Storage](resource-parity.md#storage) |
| Resource Claims | partial | [Device Resources (dynamic resource allocation)](resource-parity.md#device-resources-dynamic-resource-allocation) |
| Resource Claim Templates | partial | [Device Resources (dynamic resource allocation)](resource-parity.md#device-resources-dynamic-resource-allocation) |
| Device Classes | partial | [Device Resources (dynamic resource allocation)](resource-parity.md#device-resources-dynamic-resource-allocation) |
| Resource Slices | partial | [Device Resources (dynamic resource allocation)](resource-parity.md#device-resources-dynamic-resource-allocation) |
| Charts | partial | [Helm](resource-parity.md#helm) |
| Releases | partial | [Helm](resource-parity.md#helm) |
| Service Accounts | partial | [Access Control](resource-parity.md#access-control) |
| Cluster Roles | partial | [Access Control](resource-parity.md#access-control) |
| Roles | partial | [Access Control](resource-parity.md#access-control) |
| Cluster Role Bindings | partial | [Access Control](resource-parity.md#access-control) |
| Role Bindings | partial | [Access Control](resource-parity.md#access-control) |
| Pod Security Policies | partial | [Access Control](resource-parity.md#access-control) |
| Definitions | partial | [Custom Resources](resource-parity.md#custom-resources) |
| Custom Resources: one view per custom resource kind the cluster serves, titled by its singular Kind name, under one collapsed group per API… | done | [Custom Resources](resource-parity.md#custom-resources) |
| Flux CD › Dashboard | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Flux CD › Helm Releases, Kustomizations (2 views) | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Flux CD › Image Automation › Image Repositories, Image Policies, Image Update Automations (3 views) | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Flux CD › Notifications › Alerts, Providers, Receivers (3 views) | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Flux CD › Sources › Git Repositories, OCI Repositories, Helm Repositories, Buckets, External Artifacts (5 views) | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Argo CD | partial | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Security Center | missing | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |
| Cost Monitoring | missing | [Paid Lens Desktop views](resource-parity.md#paid-lens-desktop-views) |

## Interface

| Row | Status | Section |
|---|---|---|
| Title bar with history back and forward, notifications and a context menu with Preferences; captured (2026-10-11): back and forward arrows,… | partial | [Layout](ui-parity.md#layout) |
| Hotbar down the left edge (Hotbar); captured: one tile per cluster with its icon or image and a short label, and a bank switcher ("‹ 1 ›")… | partial | [Layout](ui-parity.md#layout) |
| Navigator with the cluster's resource tree (Cluster sidebar) | partial | [Layout](ui-parity.md#layout) |
| Navigator header: a dropdown of the scope the tree shows, one cluster or a section such as "Local Kubeconfigs", and a search button; the tr… | partial | [Layout](ui-parity.md#layout) |
| Tab bar of cluster views: single click opens a preview tab, double-click (or ⇧↵) a fixed tab; a tab reads "<view> - <context>" with the clu… | partial | [Layout](ui-parity.md#layout) |
| Workspace showing the active view | partial | [Layout](ui-parity.md#layout) |
| The window opens where it was closed: as large, on the same display, in full screen if it was (Lens Desktop does, inferred: not in its docu… | partial | [Layout](ui-parity.md#layout) |
| Detail drawer on the right (Detail drawer) | partial | [Layout](ui-parity.md#layout) |
| Dock along the bottom (Dock) | partial | [Layout](ui-parity.md#layout) |
| Status bar | partial | [Layout](ui-parity.md#layout) |
| Welcome page (Help › Welcome) | partial | [Layout](ui-parity.md#layout) |
| Kubeconfig files in ~/.kube/ (%USERPROFILE%\.kube on Windows) are found without setup; Lens lists ~/.kube as an ordinary, removable synced… | partial | [Clusters](ui-parity.md#clusters) |
| Kubeconfig Syncs: add files and folders to watch | partial | [Clusters](ui-parity.md#clusters) |
| Add clusters by pasting a kubeconfig; Lens stores the text itself, not in ~/.kube/config; its "Add Kubeconfigs" menu offers "Paste from cli… | not a target | [Clusters](ui-parity.md#clusters) |
| Navigator sections: Kubernetes Clusters (Local Kubeconfigs and the cloud sections), Teamwork, Extensions | partial | [Clusters](ui-parity.md#clusters) |
| Search clusters by name (⌘P) | partial | [Clusters](ui-parity.md#clusters) |
| Cluster menu: connect, disconnect, settings, add to hotbar, remove (labels: capture needed) | partial | [Clusters](ui-parity.md#clusters) |
| Cluster identity: upload an icon (.jpeg, .png, .svg) or pick a colour | partial | [Clusters](ui-parity.md#clusters) |
| Drill into a section or cluster with its chevron to filter the tabs to that scope | partial | [Clusters](ui-parity.md#clusters) |
| Navigator keyboard: ↑↓ move, → opens, ← closes, Tab and ⇧Tab, ↵ preview tab, ⇧↵ fixed tab | partial | [Clusters](ui-parity.md#clusters) |
| Cloud sections list clusters from AWS, Azure and Google accounts | missing | [Clusters](ui-parity.md#clusters) |
| Hotbar of up to 12 clusters per bank | partial | [Hotbar](ui-parity.md#hotbar) |
| Banks: create, rename, remove, switch from the command palette | not a target | [Hotbar](ui-parity.md#hotbar) |
| F1–F12 switch to a cluster in the bank, ⇧F1–⇧F12 switch bank | partial | [Hotbar](ui-parity.md#hotbar) |
| Add a cluster from its menu ("Add to hotbar") | partial | [Hotbar](ui-parity.md#hotbar) |
| Reorder tiles by dragging them within the hotbar (Lens: tiles are dragged to reorder, as the first row of this table notes; capture needed) | partial | [Hotbar](ui-parity.md#hotbar) |
| The tree above | partial | [Cluster sidebar](ui-parity.md#cluster-sidebar) |
| A group or item appears only when the cluster serves its kinds (Admission Policies, Gateway API, Device Resources) | partial | [Cluster sidebar](ui-parity.md#cluster-sidebar) |
| Groups expand and collapse | partial | [Cluster sidebar](ui-parity.md#cluster-sidebar) |
| Sort by a column header, ascending and descending arrows, empty values last | partial | [Tables](ui-parity.md#tables) |
| Resize columns by dragging | partial | [Tables](ui-parity.md#tables) |
| Double-click a column separator to fit its content | partial | [Tables](ui-parity.md#tables) |
| Reorder columns by dragging | partial | [Tables](ui-parity.md#tables) |
| Show and hide columns (⋮ at the end of the header row) | partial | [Tables](ui-parity.md#tables) |
| Search field per view, with toggles for match case ("Aa") and regular expressions (".*") (capture, 2026-10-11) | partial | [Tables](ui-parity.md#tables) |
| A cell too narrow for its text shows the full text in a tooltip on hover (capture of an event message, 2026-10-11) | done | [Tables](ui-parity.md#tables) |
| Namespace filter: multi-select, default "All namespaces", Space toggles without closing; none on cluster-scoped views (captures of Nodes, I… | partial | [Tables](ui-parity.md#tables) |
| Actions menu per row (⋮ at the row's end), starting with Show Details and ending with Edit and Delete, the kind's own actions between; item… | partial | [Tables](ui-parity.md#tables) |
| Right-click on a row opens the actions menu (since 2025.10.230725) | done | [Tables](ui-parity.md#tables) |
| Keyboard: ↑↓ rows, Tab columns, ↵ or Space opens the details | done | [Tables](ui-parity.md#tables) |
| Export the table as CSV (a download button beside the item count, "18 items"; the format is inferred) | partial | [Tables](ui-parity.md#tables) |
| Select rows with a checkbox column, all rows with the header's checkbox (capture, 2026-10-11) | partial | [Tables](ui-parity.md#tables) |
| A warning column right after Name in built-in views (Pods, Pod Disruption Budgets, Cron Jobs): an orange triangle on rows with a problem (s… | partial | [Tables](ui-parity.md#tables) |
| A floating "+" button at the bottom right of every list opens a Create resource tab in the dock (capture, 2026-10-11) | partial | [Tables](ui-parity.md#tables) |
| The sorted column shows an arrow beside its title | partial | [Tables](ui-parity.md#tables) |
| Cells that name another object are links: Namespace, Controlled By, Node, Involved Object, a definition's resource (captures, 2026-10-11);… | partial | [Tables](ui-parity.md#tables) |
| Row line height (preference) | partial | [Tables](ui-parity.md#tables) |
| Status colours per state (palette: capture needed; seen: "Running" green, "Evicted" red) | partial | [Tables](ui-parity.md#tables) |
| Opens from the right when a row is selected, beside the narrowed table; titled "<Kind>: <name>" (capture, 2026-10-11) | partial | [Detail drawer](ui-parity.md#detail-drawer) |
| Actions at the top: Copy, Edit, Delete and the kind's own actions; captured as icons in the drawer's title bar: for a pod seven icons endin… | partial | [Detail drawer](ui-parity.md#detail-drawer) |
| Drawer sections in order: Metrics, Properties, the kind's own sections, Events ("No events found" when none) (capture of a pod and a node) | partial | [Detail drawer](ui-parity.md#detail-drawer) |
| Long values fold: Labels, Annotations, Tolerations, environment variables, mounts and volumes show a count ("6 Labels") and expand on click… | partial | [Detail drawer](ui-parity.md#detail-drawer) |
| Object references in the drawer are links (namespace, controller, node, service account, secrets); container ports have a "Forward…" button… | done | [Detail drawer](ui-parity.md#detail-drawer) |
| Bottom dock with tabs: Terminal session, Create resource, Edit, logs, Helm Install, Helm Upgrade; tabs named after their object ("Pod <name… | partial | [Dock](ui-parity.md#dock) |
| "New tab" menu: Terminal session, Create resource | done | [Dock](ui-parity.md#dock) |
| Tab icons take the cluster's colour | partial | [Dock](ui-parity.md#dock) |
| Edit tab: YAML editor with syntax highlighting and Save | partial | [Editor and create resource](ui-parity.md#editor-and-create-resource) |
| Diff view: "Show changes", "Revert to original", "Exit diff view" | partial | [Editor and create resource](ui-parity.md#editor-and-create-resource) |
| Create resource tab: template picker, "Create & Close"; captured: a dock tab with save and search buttons, a "Select Template …" picker who… | partial | [Editor and create resource](ui-parity.md#editor-and-create-resource) |
| Bundled templates, device-resource templates included (2026.9) | partial | [Editor and create resource](ui-parity.md#editor-and-create-resource) |
| Dialogs instead of YAML to create Namespaces, Secrets, Resource Quotas, Service Accounts and RBAC objects | partial | [Editor and create resource](ui-parity.md#editor-and-create-resource) |
| Pod and container selectors | partial | [Logs](ui-parity.md#logs) |
| Follow; jump to the latest line | partial | [Logs](ui-parity.md#logs) |
| Show timestamps | partial | [Logs](ui-parity.md#logs) |
| Word wrap | partial | [Logs](ui-parity.md#logs) |
| Show the previous terminated container | partial | [Logs](ui-parity.md#logs) |
| Logs of a controlling resource (all its pods), with resource names shown | partial | [Logs](ui-parity.md#logs) |
| Filter matching lines on or off (/) | partial | [Logs](ui-parity.md#logs) |
| Download logs | partial | [Logs](ui-parity.md#logs) |
| Select the lines' text and copy it (Lens's log view is part of its web page, where text selects as on any page; how far is not captured) | partial | [Logs](ui-parity.md#logs) |
| Jump to a time and date | partial | [Logs](ui-parity.md#logs) |
| Load older lines when scrolling up | partial | [Logs](ui-parity.md#logs) |
| Terminal session for a cluster, with the working directory and default namespace from cluster settings | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Scrolling back through a terminal's earlier output: the wheel, the trackpad, a scroll bar (Lens's terminal is xterm.js, which keeps a scrol… | done | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Pod shell, attach and node shell open as dock tabs | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Copy and paste with the right mouse button (preference) | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Copy and paste keys: ⌘C / ⌘V; Windows and Linux ⌃C, ⌃⇧C or ⌃Ins, and ⌃V, ⌃⇧V or ⇧Ins | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Theme, font size and font family (preferences) | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| Shell path and shell arguments (2026.10: one argument per line) | partial | [Terminal and shells](ui-parity.md#terminal-and-shells) |
| A container port link ("<name>: <port>/<protocol>" in the drawer, captured) forwards to a random local port and opens the browser | done | [Port forwarding](ui-parity.md#port-forwarding) |
| Forward dialog, from the "Forward…" button beside a container port (init containers too): titled "Port Forwarding for <pod>", a local port… | done | [Port forwarding](ui-parity.md#port-forwarding) |
| Port Forwarding view to open, edit, stop and delete forwards (resource parity) | partial | [Port forwarding](ui-parity.md#port-forwarding) |
| ⌘⇧P opens it | partial | [Command palette](ui-parity.md#command-palette) |
| "Switch to *resource*" from a cluster view | partial | [Command palette](ui-parity.md#command-palette) |
| Hotbar bank commands (hotbar: create bank, rename, remove) | not a target | [Command palette](ui-parity.md#command-palette) |
| Application menu with Preferences (items: capture needed) | partial | [Application menu](ui-parity.md#application-menu) |
| Window: Minimize; on macOS Zoom, Bring All to Front and the list of windows; on Windows and Linux Close Window (2026.10.51501) | partial | [Application menu](ui-parity.md#application-menu) |
| Help › Welcome, with "Open Onboarding Wizard..." | partial | [Application menu](ui-parity.md#application-menu) |
| Command palette | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Search a cluster by name | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Search in the tab | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Preferences | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Cluster settings | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Ask AI (cluster tab) | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Terminal session (cluster tab) | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Close the tab | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Reload | missing | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Back / forward in the tab's history | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Next tab | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Previous tab | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Tab *n* | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Cluster in the hotbar bank / switch bank | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| New space (Teamwork) | not a target | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Quit | partial | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Table: rows / columns / details | done | [Keyboard shortcuts](ui-parity.md#keyboard-shortcuts) |
| Theme: Sync with computer, Dark, Light | partial | [Preferences](ui-parity.md#preferences) |
| Start at login | partial | [Preferences](ui-parity.md#preferences) |
| Update channel: Stable, Beta, Alpha | partial | [Preferences](ui-parity.md#preferences) |
| Locale timezone | partial | [Preferences](ui-parity.md#preferences) |
| Command-line tool on or off ("Lens CLI") | missing | [Preferences](ui-parity.md#preferences) |
| HTTP proxy for non-cluster traffic | partial | [Preferences](ui-parity.md#preferences) |
| Allow untrusted certificate authorities for non-cluster traffic | missing | [Preferences](ui-parity.md#preferences) |
| kubectl: download binaries, download mirror (Google or Azure), directory for binaries, path to kubectl | partial | [Preferences](ui-parity.md#preferences) |
| Helm repositories: pick public ones, add custom (name, URL, key, CA and certificate files, username and password; OCI registries) | partial | [Preferences](ui-parity.md#preferences) |
| HELM_DRIVER: secret (default), configmap, sql | partial | [Preferences](ui-parity.md#preferences) |
| Kubeconfig syncs | partial | [Preferences](ui-parity.md#preferences) |
| Editor: show minimap and its position, line numbers, tab size, font size, font family | partial | [Preferences](ui-parity.md#preferences) |
| Tables: row line height | partial | [Preferences](ui-parity.md#preferences) |
| Terminal: shell path, shell arguments, copy and paste behaviour, theme, font size, font family | partial | [Preferences](ui-parity.md#preferences) |
| Integrations: AWS EKS, Azure AKS, Google GKE | missing | [Preferences](ui-parity.md#preferences) |
| External tools: the AI command-line tool for Ask AI | not a target | [Preferences](ui-parity.md#preferences) |
| Extensions: install from file system, auto-update | not a target | [Preferences](ui-parity.md#preferences) |
| Telemetry, surveys, error reports; Teamwork | not a target | [Preferences](ui-parity.md#preferences) |
| General: cluster name (a text field, the context name until changed), icon (the image beside it, with a "…" menu: Upload Icon, Pick Icon Co… | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Proxy: HTTP proxy for this cluster's API traffic, one field (placeholder "http://<address>:<port>") (capture) | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Terminal: starting working directory (a field with a folder picker, "$HOME" when unset), default namespace for kubectl ("default" when unse… | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Accessible namespaces (for users who may not list namespaces): a field that adds one namespace at a time (capture) | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Metrics provider: auto-detect or Helm 14.x, Helm, Lens, Prometheus Operator, StackLight, VictoriaMetrics, VictoriaMetrics single; service a… | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Hide metrics from the UI: a multi-select of metrics, "Hide all metrics" and "Reset" buttons, and a line saying all metrics are visible when… | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Lens Metrics: install a bundled Prometheus, kube-state-metrics and node-exporter into the cluster on request; captured as three switches (a… | not a target | [Cluster settings](ui-parity.md#cluster-settings) |
| Node shell: image (the default, shown as the field's placeholder, is the tag docker.io/alpine:3.19), an optional image pull secret in kube-… | partial | [Cluster settings](ui-parity.md#cluster-settings) |
| Cost: the source as a choice of Automatic (showing which source it uses), Kubecost, OpenCost, an estimate from node types and list prices (… | missing | [Cluster settings](ui-parity.md#cluster-settings) |
| CPU, memory and pod usage against requests, limits, allocatable and capacity | partial | [Cluster overview and charts](ui-parity.md#cluster-overview-and-charts) |
| Master and worker node charts | partial | [Cluster overview and charts](ui-parity.md#cluster-overview-and-charts) |
| Time range from 1 hour to 2 months | partial | [Cluster overview and charts](ui-parity.md#cluster-overview-and-charts) |
| Issues | partial | [Cluster overview and charts](ui-parity.md#cluster-overview-and-charts) |
| Metrics charts in detail drawers: pods, each container of a pod, nodes (captured); a time range (default "1h"), one tab per metric (CPU, me… | partial | [Cluster overview and charts](ui-parity.md#cluster-overview-and-charts) |
| Dark and light themes following the system | partial | [Appearance and application](ui-parity.md#appearance-and-application) |
| Zoom in, out and reset; full screen (capture needed) | partial | [Appearance and application](ui-parity.md#appearance-and-application) |
| Several windows (Window menu) | missing | [Appearance and application](ui-parity.md#appearance-and-application) |
| Deep links open the right view (Lens: lens://; Cogpit: cogpit://) | partial | [Appearance and application](ui-parity.md#appearance-and-application) |
| Notifications in the title bar | partial | [Appearance and application](ui-parity.md#appearance-and-application) |
| Safe mode (--safe-mode, extensions off) | not a target | [Appearance and application](ui-parity.md#appearance-and-application) |
| Ask AI: title-bar button and row actions launch the configured AI command-line tool | not a target | [Appearance and application](ui-parity.md#appearance-and-application) |

## Platforms

| Row | Status | Section |
|---|---|---|
| macOS | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Windows | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Linux | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Homebrew cask (macOS) | missing | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| winget and Scoop (Windows) | missing | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Flathub (Linux) | missing | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| APT and RPM repositories | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Snap | – | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Update notice | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| One-click update | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Update channels | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Signed update manifest, verified before install | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Checksums and SBOM per release | partial | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Activation or login before first use | – | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Network endpoints the app needs besides clusters | missing | [Operating systems and packages](platform-parity.md#operating-systems-and-packages) |
| Client certificate and key, inline or as files | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Bearer token, inline or token file | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec credential plugin (client.authentication.k8s.io) | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec plugin: aws eks get-token and aws-iam-authenticator (EKS) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec plugin: gke-gcloud-auth-plugin (GKE) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec plugin: kubelogin (AKS, Entra ID) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec plugin: kubectl oidc-login (generic OIDC) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Exec plugins that open a browser or prompt (interactive mode) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Legacy oidc auth provider | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Legacy azure auth provider | – | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| proxy-url in the kubeconfig | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| HTTP proxy set per cluster in cluster settings | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Custom CA, tls-server-name, insecure-skip-tls-verify | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Users who may list only some namespaces (accessible namespaces) | partial | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Cluster Connect: reach clusters through Lens's relay (paid) | – | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Cloud account integrations: list and add EKS, AKS, GKE clusters (paid) | missing | [Cluster authentication and connectivity](platform-parity.md#cluster-authentication-and-connectivity) |
| Every minor version the Kubernetes project supports (1.35, 1.36 and 1.37 on 2026-10-11) | partial | [Kubernetes versions](platform-parity.md#kubernetes-versions) |
| Older clusters | partial | [Kubernetes versions](platform-parity.md#kubernetes-versions) |
| Kinds and fields newer than the client libraries | partial | [Kubernetes versions](platform-parity.md#kubernetes-versions) |
| The sidebar follows the served APIs, version by version | partial | [Kubernetes versions](platform-parity.md#kubernetes-versions) |
| Device resources (resource.k8s.io/v1, 1.34 and later) and Gateway API (CRDs) | missing | [Kubernetes versions](platform-parity.md#kubernetes-versions) |
| kubectl for terminals | partial | [External tools](platform-parity.md#external-tools) |
| helm | missing | [External tools](platform-parity.md#external-tools) |
| High-DPI displays and per-monitor scaling | missing | [Display, input and language](platform-parity.md#display-input-and-language) |
| Wayland and X11 on Linux | missing | [Display, input and language](platform-parity.md#display-input-and-language) |
| Screen readers | missing | [Display, input and language](platform-parity.md#display-input-and-language) |
| Input methods (IME) in editor and terminal | missing | [Display, input and language](platform-parity.md#display-input-and-language) |
| Interface language | missing | [Display, input and language](platform-parity.md#display-input-and-language) |

## Beyond Lens

| Row | Status | Section |
|---|---|---|
| Every registry command is an MCP tool, with its schema and annotations | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Read the UI state: context, view, namespaces, filter, selection, drawer object, dock tabs | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Read the rows the user sees in the current view | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| List the kinds the cluster serves and the user may list | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Find objects by a searchable field (clusterIP, podIP, node, image, label) | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| List objects of a kind, bounded | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Read one object (YAML, events, related objects) from the cache | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Open a view, set namespaces and filter | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Select objects and open the drawer | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Navigate to an object: open its view, select it, scroll to it, open the drawer | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Open and read dock tabs (logs, terminal output, editor) | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Run cluster actions with the GUI's verbs and danger levels | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Mutating commands from agents wait for the user's acceptance unless allowed for the context | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Destructive commands from agents always wait for the user's confirmation in the GUI | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Audit log of every agent-initiated command, readable in the GUI | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Streamable HTTP on 127.0.0.1 with a bearer token; Host and Origin checks | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| cogpit-cli mcp stdio bridge for stdio-only agents | done | [MCP control of UI and cluster](beyond-lens.md#mcp-control-of-ui-and-cluster) |
| Chat tab beside the views, running while the user keeps working | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Knows what the user sees: the current view, selection and drawer object are part of every turn | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Answers questions about the selected object from the cache (YAML, events, related objects, logs on request) | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Navigates the UI for the user ("show me the Service with ClusterIP 1.2.3.4") | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Runs cluster actions under the confirmation rules of D-2 | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| LLM through an OpenAI-compatible Chat Completions endpoint with tool calling and streaming: LM Studio, LiteLLM, Open WebUI or any compatibl… | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Settings: base URL, API key (kept owner-only; the owner's requirement named the operating system's keychain until he dropped it on 2026-10-… | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Limits per turn: tool calls, tokens, time | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| Data sent to the endpoint is visible to the user; Secret values are never sent unless the user marked the connection private (owner, 2026-1… | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| ⌘I / Ctrl+I opens the chat for the current cluster (Lens's Ask AI shortcut) | done | [Built-in agent](beyond-lens.md#built-in-agent) |
| One click updates the macOS app through Sparkle 2, the Linux deb through apt and the Windows setup through WinSparkle; AppImage and tar.xz… | partial | [Updates](beyond-lens.md#updates) |
| Package-manager installs: the button runs or shows the manager's command | partial | [Updates](beyond-lens.md#updates) |
| Signed update manifest verified with a key built into the app, before anything is replaced | done | [Updates](beyond-lens.md#updates) |
| Diagnostics view: API requests per cluster by verb and resource, open and warm watches, objects and memory per kind | done | [Cache transparency](beyond-lens.md#cache-transparency) |
| On start, Helm releases left pending-install, pending-upgrade or pending-rollback by Cogpit are shown with "finish" and "roll back" | partial | [Crash recovery](beyond-lens.md#crash-recovery) |
| On start, nodes Cogpit cordoned for a drain it did not finish are shown with "continue drain" and "uncordon" | done | [Crash recovery](beyond-lens.md#crash-recovery) |
| Unsaved editor content restored from its local draft | done | [Crash recovery](beyond-lens.md#crash-recovery) |
| An interrupted connection noticed within seconds: on a wake, a network change, a watch that broke, and by a heartbeat every 15 s | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| "Reconnecting…" with an amber dot, a banner saying since when and the next attempt, Reconnect (Reconnect now until 2026-10-17, D-81); the w… | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| The views keep the cache's state; every write disabled with the reason, for the window and agents | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| Reconnecting by itself, at once on a wake or a network change, then after 1 to 30 s without end | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| A login that expired renewed once by itself, then "Login expired" and Log in again, never a pile of plugin runs | partial | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| Logs resume with a marker of the gap, port forwards wait and dial at once, shells end with Reconnect | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| A drain or a Helm action stops at the interruption and is offered after the reconnect, as after a crash | partial | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| A cluster that stays connected while another one is shown renews its login by default; its setting "Renew the login while the cluster is no… | partial | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| The window does not hang after the Mac slept | partial | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| Reconnect: a connected cluster connected anew from its kubeconfig file as it is now, the tabs kept (logs open again, shells end with their… | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| Every connect, reconnect, login and disconnect documented step by step, the successful ones too, with times, durations and a failure's erro… | done | [Standby and lost connections](beyond-lens.md#standby-and-lost-connections) |
| Every value in the detail drawer can be selected, and has a copy icon on hover; an entry of a value with several entries is copied in YAML… | partial | [Everyday use](beyond-lens.md#everyday-use) |
| Names in the lists (Pods, Deployments and every other view) are copyable: a list's cells get the drawer's copy icon on hover, at least the… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Certificates in Secrets decoded, in the drawer and for agents: for every key that holds a PEM certificate, each certificate's information i… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Certificates in Secrets: whether the chain leads to a public CA, to a CA this Mac trusts (a corporate root counts here) or to an internal o… | done | [Everyday use](beyond-lens.md#everyday-use) |
| A status filter in every view: the states the view's rows carry (Status, Not Ready, Degraded, …), several at once, cleared with one click,… | done | [Everyday use](beyond-lens.md#everyday-use) |
| A link to the owner at the status bar's left end, opening hans-fischer.com in the browser on a click, a GUI link and not a command (D-23) | done | [Everyday use](beyond-lens.md#everyday-use) |
| The window's layout from the top bar, as VS Code's title bar has it: three toggles at its right end that show and hide the navigator (⌘B),… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Restore the last session at start, a preference that is off by default: on, the tabs of the last run come back and the clusters connected a… | done | [Everyday use](beyond-lens.md#everyday-use) |
| How long a context's client certificate is valid, where the user chooses a cluster: the time left or "expired" (just that, the owner, 2026-… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Every cluster has an accent colour beside its icon that the user configures, to mark parts of the window later so that the user sees at a g… | partial | [Everyday use](beyond-lens.md#everyday-use) |
| A node's disks at a glance: a small usage bar per real filesystem the node mounts (the root first, then a data disk such as /data), the num… | done | [Everyday use](beyond-lens.md#everyday-use) |
| The cluster's Kubernetes version in the navigator's cluster row with a warning symbol when it is past its end of life and how long it has l… | done | [Everyday use](beyond-lens.md#everyday-use) |
| A namespace stuck in Terminating says what holds it: its drawer lists the objects whose finalizers keep it, with links, deleted ones vanish… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Debug containers: ephemeral containers stand out in blue among a pod's containers in the Pods list; a drawer icon adds one, asking for the… | done | [Everyday use](beyond-lens.md#everyday-use) |
| The navigator moves: going into a cluster slides its tree in from the right over All Clusters, ‹ All Clusters slides it out again; showing… | done | [Everyday use](beyond-lens.md#everyday-use) |
| Drag a hotbar tile out of the window and drop it to open that cluster in another Cogpit window (owner, 2026-10-14 evening) | missing | [Everyday use](beyond-lens.md#everyday-use) |
| Cluster Settings › Integrations: each integration of the cluster switched on or off, and at once whether Cogpit found it automatically (and… | partial | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| A cluster without integration settings asks at its first connect which integrations to enable, with the same found or manual status per int… | partial | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| Every integration on by default, the default for new clusters the user's to change (the owner, 2026-10-17, I3) | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| A short pre-choice before any integration is looked for, since the search runs with the user's privileges: "check all integrations" looks f… | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| The question can be shown again after it was answered (the owner's WF4, 2026-10-17: "Gibt es eine Möglichkeit den Dialog nochmal angezeigt… | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| A pod's drawer rates each of its images from Trivy Operator's reports, when one has a report: the worst severity found and how many of it,… | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| Every pod in the Pods list shows the worst image rating of its containers; nothing while the integration is off (the owner, 2026-10-17, I6,… | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| Kyverno: the objects that violate a policy show it, in their lists and their drawer: an Audit policy's fail, and an Enforce policy's on an… | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| Agents read every policy violation of a cluster in one call, with each object's reference, from the cache (D-79) | done | [Cluster integrations](beyond-lens.md#cluster-integrations) |
| The title bar's alerts pill shows the shown cluster's active alerts while the Alertmanager integration is on: red from a critical alert, or… | done | [Alerts](beyond-lens.md#alerts) |
| An alert board like karma's, tidier: the alerts grouped by severity and alertname with their counts, each with its summary, its description… | done | [Alerts](beyond-lens.md#alerts) |
| cogpit-cli contexts lists the contexts Cogpit offers, from the app's kubeconfig sources and with its credential plugin approvals, as a tabl… | done | [Command line](beyond-lens.md#command-line) |
| cogpit-cli get <kind> [name] reads objects from a headless engine's cache, as the views show them, read-only, Secret values never printed | partial | [Command line](beyond-lens.md#command-line) |
