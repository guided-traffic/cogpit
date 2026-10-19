package engine

import (
	"maps"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// UIState is the state of the user interface, held by the engine so that agents see and drive the
// same state as the user (architecture §4.3). The GUI changes it only through ui.* commands.
type UIState struct {
	// Revision increases with every change.
	Revision uint64 `json:"revision"`
	// Context is the kubeconfig context of the cluster the window shows (D-69: other clusters may
	// be connected too, Connections lists them), empty before connecting.
	Context string `json:"context"`
	// Connection is the state of the connection to Context.
	Connection Connection `json:"connection"`
	// Connections are the clusters the engine holds a connection to (D-69), sorted by context id:
	// the shown one (Context, its entry mirroring Connection), the one being connected, and every
	// cluster the window left connected, each with its connection's state and whether it is cold
	// (connections.go). Empty while nothing is connected.
	Connections []ConnectedCluster `json:"connections,omitempty"`
	// ClusterVersion is the API server's version of Context and its minor's upstream support (D-91,
	// k8sversion.go): the end of life from the Kubernetes release table built into this Cogpit,
	// estimated for a minor newer than the table, and the state on the day the UI state last
	// changed (ui_state recomputes it when read); nil while Context is not live or its version is
	// not known. The engine derives it after every change.
	ClusterVersion *ClusterVersion `json:"clusterVersion,omitempty"`
	// User is who Cogpit is logged in as on Context (whoami.go, D-68): the username and groups the
	// API server confirmed at the connect, the kubeconfig's user entry's name where it did not, or
	// while not connected the last username it confirmed and when; nil without a context, for the
	// demo cluster and before a first confirmation. The engine derives it after every change.
	User *ClusterUser `json:"user,omitempty"`
	// View is the open view's kind key ("pods") or page key ("workloads/overview", overview.go;
	// "cluster/overview", clusteroverview.go).
	View string `json:"view"`
	// Namespaces filter namespaced views; empty means all namespaces.
	Namespaces []string `json:"namespaces"`
	// Groups filter the Definitions view by API group; empty means all groups.
	Groups []string `json:"groups,omitempty"`
	// HiddenColumns are the ids of the columns each view hides, by view key; a view without an
	// entry hides the columns its kind hides by default (model.Column.Hidden).
	HiddenColumns map[string][]string `json:"hiddenColumns,omitempty"`
	// ColumnOrder are the column ids of each view in the order the user arranged them (dragging a
	// header, ui.setColumnOrder), by view key; a view without an entry shows its kind's order.
	ColumnOrder map[string][]string `json:"columnOrder,omitempty"`
	// Filter is the view's search text.
	Filter string `json:"filter"`
	// FilterOptions are the search field's toggles, match case and regular expression
	// (filteroptions.go).
	FilterOptions FilterOptions `json:"filterOptions"`
	// States is the open view's status filter: the states its rows must carry, as the view's
	// snapshot offers them (TableSnapshot.StateChoices; statusfilter.go); empty: every row. It
	// belongs to the view it was set for: opening another view clears it.
	States []string `json:"states,omitempty"`
	// statesView is the view States was set for (syncStates).
	statesView string
	// Sort is the open view's sort order.
	Sort SortSpec `json:"sort"`
	// Selected are the selected objects of the open view.
	Selected []model.ObjectRef `json:"selected"`
	// Drawer is the object the detail drawer shows; nil when it is closed.
	Drawer *model.ObjectRef `json:"drawer,omitempty"`
	// DrawerSort is the order of the drawer's sortable tables (a node's or a workload's Pods), by
	// the table's section title ("Pods"), as ui.sortDrawerTable sets it (drawersort.go). It stays
	// when the drawer shows another object and is the same in every tab; a table without an entry
	// is sorted by name.
	DrawerSort map[string]SortSpec `json:"drawerSort,omitempty"`
	// DrawerChartVersion is the version a Helm chart's drawer shows, as ui.setChartVersion sets it
	// (helmchartdrawer.go): its properties and README are that version's. Empty: the version the
	// chart is listed with. It ends when the drawer closes or shows another object.
	DrawerChartVersion string `json:"drawerChartVersion,omitempty"`
	// Dock lists the dock's tabs; ActiveDock is the id of the shown one.
	// The dock shows the shown cluster's tabs and those of no cluster (D-69, dockscope.go):
	// ActiveDock is the shown cluster's active tab, and dockMemo keeps every other cluster's active
	// tab and open state while it is not shown.
	Dock       []DockTab `json:"dock"`
	ActiveDock string    `json:"activeDock,omitempty"`
	dockMemo   map[string]dockMemo
	// Hotbar is the hotbar: one list of tiles, top to bottom (hotbar.go, D-83); nil in a session
	// without one. Its tiles carry their contexts only; ui.state adds what each tile shows
	// (Session.HotbarView).
	Hotbar *HotbarState `json:"hotbar,omitempty"`
	// Overview is what the cluster Overview's charts show: their time range and node group
	// (clusteroverview.go); zero: the defaults.
	Overview OverviewChoice `json:"overview,omitzero"`
	// Palette is set while the command palette is open (ui.palette.open, ⌘⇧P; palette.go).
	Palette bool `json:"palette,omitempty"`
	// PaletteText is what is typed in the open palette's field (D-51, A54; palettetext.go), "" while
	// it is closed; PaletteTextOrigin is who set it last: gui for the user, else the agent's (or the
	// CLI's) origin, set with ui.palette.setText. On a text another origin set, the window's ↵ runs
	// no change until the user picked the entry.
	PaletteText       string `json:"paletteText,omitempty"`
	PaletteTextOrigin Origin `json:"paletteTextOrigin,omitempty"`
	// Navigator is the navigator's search text, the row its keyboard is on and the Local
	// Kubeconfigs section's collapsed source groups (D-51, A51, A63; navigator.go).
	Navigator NavigatorState `json:"navigator,omitzero"`
	// Chat is set while the built-in agent's chat column is shown (chat.toggle, ⌘I; chat.go).
	// What is said in it stays in the window: agents see only that it is open.
	Chat bool `json:"chat,omitempty"`
	// NavigatorHidden is set while the navigator is hidden (ui.toggleNavigator, ⌘B; layout.go):
	// the views take its room, the hotbar stays. Kept across restarts (workspace.json).
	NavigatorHidden bool `json:"navigatorHidden,omitempty"`
	// DockOpen is set while the dock shows its panel, not only its tab strip (ui.toggleDock, ⌘J;
	// layout.go): a tab opened or activated opens it, and it is closed while it has no tabs.
	DockOpen bool `json:"dockOpen,omitempty"`
	// Update is the newer release the app's updater knows of (updates.go): the window's Update
	// button shows while it is available. Zero without one and in a session without an updater.
	Update UpdateState `json:"update,omitzero"`
	// NavigatorScope is what the navigator shows (navscope.go): "section:local", the Local
	// Kubeconfigs section with every cluster Cogpit offers, or "cluster:<context id>", the
	// connected cluster's tree. The tab bar shows the tabs of the scope. The engine keeps it in step
	// with the connection; ui.setNavigatorScope sets it.
	NavigatorScope string `json:"navigatorScope"`
	// Welcome is set while the workspace shows the welcome page (welcome.go): no cluster is
	// connected and none is connecting, or one is and no tab is active. The engine derives it
	// after every change (syncWelcome); ui.welcome.open shows the page over a connected cluster.
	Welcome bool `json:"welcome,omitempty"`
	// Preferences are Cogpit's own settings (preferences.go); nil in a session that keeps none.
	Preferences *Preferences `json:"preferences,omitempty"`
	// LoginItem is Cogpit's start at login as the operating system reports it (loginitem.go); nil
	// where the platform offers none.
	LoginItem *LoginItemState `json:"loginItem,omitempty"`
	// TimeFormat is how the window writes absolute times (D-31): Go layouts of a date and a time
	// of day, from the computer's region settings ("02.01.2006", "15:04:05"), in the Preferences'
	// time zone. Agents read the drawn texts (view rows, drawer items) in it; structured times of
	// results are RFC 3339.
	TimeFormat model.TimeLayout `json:"timeFormat"`
	// ClusterSettings are the settings of every context that has some, by context id
	// (clustersettings.go): display names and icons the hotbar, the navigator and the welcome page show.
	ClusterSettings map[string]ClusterSettings `json:"clusterSettings,omitempty"`
	// Settings is the open settings page, Preferences (⌘,) or a context's Cluster settings (⌘⇧,);
	// nil while none is open.
	Settings *SettingsPage `json:"settings,omitempty"`
	// IntegrationsQuestion is the context whose integrations the window asks the user to choose
	// (D-76, integrationsask.go): the shown cluster while it is connected and the user made no choice
	// for it (ClusterSettings.IntegrationsOff unset), unless it is the demo cluster or the question
	// was put off this run; empty otherwise. Until it is answered nothing of the cluster's
	// integrations is read but what the question ticks (D-82); nothing waits for it.
	// cluster.integrations.step moves it, .lookFor ticks, .answer answers it (these three the
	// window's only, H-126), .later puts it off; cluster.settings.get lists each integration's status. The engine derives it after every change.
	IntegrationsQuestion string `json:"integrationsQuestion,omitempty"`
	// IntegrationsStep is the page the question shows (D-82, integrationsconsent.go): preChoice,
	// checkAll, choose or dontUse; empty while there is no question.
	IntegrationsStep IntegrationsStep `json:"integrationsStep,omitempty"`
	// IntegrationsLookFor are the integrations the question ticks, looked for and read while it asks.
	IntegrationsLookFor IntegrationSet `json:"integrationsLookFor,omitempty"`
	// IntegrationsAskFirst is set in a session that asks before it looks (one that keeps cluster
	// settings, the window's): a cluster without a choice of integrations reads none but what its
	// question ticks (D-82, integrations.go). Unset in one that does not ask (the CLI's), where the
	// default for new clusters applies.
	IntegrationsAskFirst bool `json:"integrationsAskFirst,omitempty"`
	// Notifications sums up the notification log, the toasts the user was shown (notifications.go):
	// how many, how many unread, the most severe unread level; notifications.list reads them.
	Notifications NotificationSummary `json:"notifications,omitzero"`
	// Alerts is what the shown cluster's Alertmanager answered, the title bar's pill (alerts.go,
	// D-77): its state and the active alerts counted by severity; nil while the Alertmanager
	// integration is off for the shown cluster, while none is shown and live, and for the demo
	// cluster. The engine derives it after every change; alerts.list and alerts.board read the
	// alerts themselves.
	Alerts *AlertsSummary `json:"alerts,omitempty"`
	// AlertBoard is the alert board's folding (alerts.fold): the severity groups and alert cards
	// folded, the alerts whose description is unfolded.
	AlertBoard AlertBoardState `json:"alertBoard,omitzero"`
	// Tabs are the tab bar's tabs in their order, each a view of one cluster with its own state
	// (tabs.go); ActiveTab is the shown one's id, empty when none is shown. View, Namespaces,
	// Groups, Filter, FilterOptions, States, Sort, Selected and Drawer above are the active tab's.
	Tabs      []ViewTab `json:"tabs,omitempty"`
	ActiveTab string    `json:"activeTab,omitempty"`
	tabSeq    uint64    // the number of the last tab id handed out
	tabClock  uint64    // counts tab activations, for ViewTab.used
	connGen   uint64    // the connect that made the connection Connected: its watch writes Connection (connwatch.go)
}

// Connection is the state of one cluster connection: the shown cluster's (UIState.Connection), or
// another connected cluster's (ConnectedCluster.Connection).
type Connection struct {
	State ConnectionState `json:"state"`
	Error string          `json:"error,omitempty"` // set in ConnectError, and in Disconnected when Cogpit closed the connection (its kubeconfig file went away); never contains a credential
	Demo  bool            `json:"demo,omitempty"`  // the synthetic demo cluster
	// TLSUnchecked is set while the connection does not check the API server's certificate
	// (insecure-skip-tls-verify in its kubeconfig), from the connection's own configuration: the
	// status bar says "TLS not checked" beside the context (A29, owner 2026-10-14 evening; H-37).
	TLSUnchecked bool `json:"tlsUnchecked,omitempty"`
	// Since is when the connection was interrupted, or the login expired; zero otherwise
	// (connwatch.go, D-58). The views show the cache's state as of then.
	Since time.Time `json:"since,omitzero"`
	// NextAttempt is when Cogpit next tries to reach the cluster while Interrupted; zero while an
	// attempt runs and in the other states. cluster.reconnect connects anew at once (D-81).
	NextAttempt time.Time `json:"nextAttempt,omitzero"`
	// Reason says, in a sentence without credentials, why the connection is Interrupted or the
	// login LoginExpired ("no answer from the API server within 5s").
	Reason string `json:"reason,omitempty"`
	// LoggingIn is set while a login cluster.login started runs (its credential plugin may wait for
	// the user in a browser).
	LoggingIn bool `json:"loggingIn,omitempty"`
	// NeedsApproval is set in LoginExpired when the context's credential plugin is not allowed as it
	// is now (its program changed, D-26): logging in again connects anew, which asks first.
	NeedsApproval bool `json:"needsApproval,omitempty"`
	// WaitingForLogin is set while the connection waits for its credential plugin (loginwait.go,
	// D-73): a connect's first request, a renewal, Log in again. The plugin may wait for the user in
	// a browser; Cogpit ends it at its Until, and cluster.disconnect ends it (and a connect under
	// way) at once; a run Cogpit could not end has no Until (LoginWait.RunsOn), and
	// cluster.disconnect then only stops the wait. While Connecting, Reason says it in a sentence.
	WaitingForLogin LoginWait `json:"waitingForLogin,omitzero"`
	// CredentialPlugin is set for a connection whose context logs in through a credential plugin
	// (exec): Log in again runs it once. Without it, Log in again connects anew from the kubeconfig
	// file (cluster.reconnect), the only way to use a certificate or token renewed there (D-81).
	CredentialPlugin bool `json:"credentialPlugin,omitempty"`
	// Reconnecting is set while cluster.reconnect (or Log in again, or Cogpit by itself after the
	// kubeconfig file's login changed) builds a new connection from the kubeconfig file; the
	// connection it replaces goes on until the new one stands (D-81).
	Reconnecting bool `json:"reconnecting,omitempty"`
}

// LoginWait is a credential plugin's run that a connection waits for (Connection.WaitingForLogin).
type LoginWait struct {
	Program string    `json:"program"`        // the plugin's program, its file name ("pinniped")
	Since   time.Time `json:"since"`          // when it began
	Until   time.Time `json:"until,omitzero"` // when Cogpit ends it unless it returned; none when RunsOn
	// RunsOn: Cogpit stopped waiting for this run (a cancel, or its bound passed) and could not end
	// its process, which keeps client-go's run going (cluster.LoginRun.RunsOn, H-118): it has no end
	// time, and the connection waits behind it until that process ends. Cancel Connecting stops a
	// connect's wait; the process runs on.
	RunsOn bool `json:"runsOn,omitempty"`
}

// ConnectionState is a connection's lifecycle state.
type ConnectionState string

// Connection states.
const (
	Disconnected ConnectionState = "disconnected"
	Connecting   ConnectionState = "connecting"
	Connected    ConnectionState = "connected"
	ConnectError ConnectionState = "error"
	// Interrupted: the connected cluster stopped answering (the computer slept, the network or a
	// VPN went away). The views keep the cache's last state, writes are disabled, and Cogpit
	// reconnects by itself (connwatch.go, D-58).
	Interrupted ConnectionState = "interrupted"
	// LoginExpired: the API server rejects the connection's login and renewing it once did not
	// help. Writes are disabled until the user logs in again (cluster.login).
	LoginExpired ConnectionState = "loginExpired"
)

// Live reports whether the state holds a connected cluster: connected, interrupted or with an
// expired login. Its backend, cache, tabs and views stay; only Connected can write.
func (c ConnectionState) Live() bool {
	return c == Connected || c == Interrupted || c == LoginExpired
}

// asConnected is Connected for every Live state: for what follows connects, an interruption and
// its end are no connect.
func (c ConnectionState) asConnected() ConnectionState {
	if c.Live() {
		return Connected
	}
	return c
}

// SortSpec is a table's sort order.
type SortSpec struct {
	Column string `json:"column" jsonschema:"column id, e.g. name, age, restarts"`
	Desc   bool   `json:"desc,omitempty" jsonschema:"descending order"`
}

// DockTab is one tab of the bottom dock.
type DockTab struct {
	ID        string           `json:"id"`
	Type      DockType         `json:"type"`
	Title     string           `json:"title"`
	Context   string           `json:"context,omitempty"`
	Ref       *model.ObjectRef `json:"ref,omitempty"`       // the pod for logs and shells
	Container string           `json:"container,omitempty"` // the container for logs and shells
	Logs      LogOptions       `json:"logs,omitzero"`       // a logs tab's options (logs.go)
	Helm      *HelmUpgradeTab  `json:"helm,omitempty"`      // a Helm Upgrade tab's chart and version
	// HelmInstall is a Helm Install tab's chart, version and defaults (helminstall.go).
	HelmInstall *HelmInstallTab `json:"helmInstall,omitempty"`
	// Kubectl is a terminal tab's kubectl: the check made when it opened, the offer of a matching
	// release and its download (kubectl.go).
	Kubectl *KubectlCheck `json:"kubectl,omitempty"`
	// Audit is the Audit Log tab's filter (auditlist.go).
	Audit AuditTab `json:"audit,omitzero"`
	// NodeShell is a node shell tab's node and pod (nodeshell.go).
	NodeShell *NodeShellTab `json:"nodeShell,omitempty"`
	// Job is a job tab's progress: its steps, state and outcome (jobs.go).
	Job *JobTab `json:"job,omitempty"`
	// AgentRead is set on a terminal tab (terminal, shell, attach, node shell) whose output the
	// user lets agents read (dock.allowAgentRead, the tab's switch; D-44); off when it opens.
	// Agents read logs tabs without it (dockoutput.go).
	AgentRead bool `json:"agentRead,omitempty"`
	// AgentReadOn is when the switch went on; zero while it is off. AgentLastRead is when an agent
	// last read the tab since then; zero before the first read. AgentReadUntil is when the switch
	// turns itself off: AgentReadIdle after it went on or after the last read, at the latest
	// AgentReadMax after it went on; zero while it is off. AgentReadExpired is set once it turned
	// itself off, until the user turns it on again, and AgentReadCapped when that was at its
	// AgentReadMax rather than for want of a read (agentread.go, H-83).
	AgentReadOn      time.Time `json:"agentReadOn,omitzero"`
	AgentLastRead    time.Time `json:"agentLastRead,omitzero"`
	AgentReadUntil   time.Time `json:"agentReadUntil,omitzero"`
	AgentReadExpired bool      `json:"agentReadExpired,omitempty"`
	AgentReadCapped  bool      `json:"agentReadCapped,omitempty"`
	// Interrupted is when a shell, node shell or attach tab's session ended because the connection
	// was interrupted (D-58); zero otherwise. The tab says so and offers Reconnect (dock.reconnect),
	// which clears it.
	Interrupted time.Time `json:"interrupted,omitzero"`
	// Reconnected is set with Interrupted when the session ended because Cogpit connected the
	// cluster anew (cluster.reconnect, D-81) rather than lost the connection.
	Reconnected bool `json:"reconnected,omitempty"`
	// Session counts the sessions opened in the tab after the first: dock.reconnect's in a shell,
	// attach or node shell tab, and in a logs tab each reconnect of its cluster (D-81), whose stream
	// opens again on the new connection. The window opens a new session or stream when it changes.
	Session int `json:"session,omitempty"`
}

// HelmUpgradeTab is what a Helm Upgrade tab shows beside its values editor (Lens's "Helm Upgrade:
// <name>" tab, captured 2026-10-12): the release's chart, the version the upgrade installs and the
// versions its picker offers. The tab's Ref is the release (kind helmreleases); its values are the
// tab's editor text (editor.get, editor.setText), which agents read withheld (model.Withheld; the
// built-in agent over a private connection as the window shows it, H-94). helm.release.upgradeTab with a repository or a version changes the choice (helmupgrade.go).
type HelmUpgradeTab struct {
	Chart string `json:"chart"`
	// Version is the version the upgrade installs: the release's own (its stored chart), or one of
	// Repository's.
	Version string `json:"version"`
	// ReleaseVersion is the release's own chart version, which the picker marks as the current one.
	ReleaseVersion string `json:"releaseVersion,omitempty"`
	// Repository is the chart repository whose versions the picker offers, "" when no repository
	// of Helm's repositories file lists the chart; Repositories are every one that does, in the file's
	// order.
	Repository   string   `json:"repository,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	// Origin is Repository's origin when nothing authenticates its content (H-41, A14):
	// kinds.HelmOriginHTTP or kinds.HelmOriginInsecureTLS; the tab marks it, and an upgrade that
	// downloads from it says so in its confirmation.
	Origin string `json:"origin,omitempty"`
	// Versions are the picker's choices: the release's own chart version, and Repository's versions
	// newest first, prereleases after the others.
	Versions []string `json:"versions"`
	// UserSuppliedOnly: the editor holds the user-supplied values only, not the chart's defaults
	// merged in (Lens's "User-supplied values only").
	UserSuppliedOnly bool `json:"userSuppliedOnly"`
}

// DockType is a dock tab's kind.
type DockType string

// Dock tab types.
const (
	DockLogs     DockType = "logs"     // a container's log stream
	DockTerminal DockType = "terminal" // a local shell bound to the context
	DockShell    DockType = "shell"    // a shell in a container
	DockAttach   DockType = "attach"   // attached to a container's main process (pod.attach)
	DockEdit     DockType = "edit"     // a YAML editor on one object (object.edit)
	DockCreate   DockType = "create"   // a YAML editor for new objects (resource.createTab)
	// DockHelmUpgrade: a release's values and chart version, saved by helm.release.upgrade.
	DockHelmUpgrade DockType = "helmUpgrade"
	// DockHelmInstall: a chart's values and version, installed by helm.chart.install (helminstall.go).
	DockHelmInstall DockType = "helmInstall"
)

// Clone returns a deep copy.
func (u UIState) Clone() UIState {
	out := u
	out.Namespaces = slices.Clone(u.Namespaces)
	out.Groups = slices.Clone(u.Groups)
	out.States = slices.Clone(u.States)
	if u.HiddenColumns != nil {
		out.HiddenColumns = make(map[string][]string, len(u.HiddenColumns))
		for view, ids := range u.HiddenColumns {
			out.HiddenColumns[view] = slices.Clone(ids)
		}
	}
	if u.ColumnOrder != nil {
		out.ColumnOrder = make(map[string][]string, len(u.ColumnOrder))
		for view, ids := range u.ColumnOrder {
			out.ColumnOrder[view] = slices.Clone(ids)
		}
	}
	out.Connections = slices.Clone(u.Connections)
	out.Selected = slices.Clone(u.Selected)
	if u.Drawer != nil {
		d := *u.Drawer
		out.Drawer = &d
	}
	out.DrawerSort = maps.Clone(u.DrawerSort)
	out.Dock = make([]DockTab, len(u.Dock))
	for i, t := range u.Dock {
		out.Dock[i] = t
		if t.Ref != nil {
			r := *t.Ref
			out.Dock[i].Ref = &r
		}
		if t.Helm != nil {
			h := *t.Helm
			h.Versions = slices.Clone(t.Helm.Versions)
			h.Repositories = slices.Clone(t.Helm.Repositories)
			out.Dock[i].Helm = &h
		}
		if t.Kubectl != nil {
			k := t.Kubectl.clone()
			out.Dock[i].Kubectl = &k
		}
		out.Dock[i].Job = t.Job.clone()
	}
	if u.Hotbar != nil {
		h := u.Hotbar.Clone()
		out.Hotbar = &h
	}
	if u.Preferences != nil {
		p := u.Preferences.Clone()
		out.Preferences = &p
	}
	if u.LoginItem != nil {
		l := *u.LoginItem
		out.LoginItem = &l
	}
	out.ClusterSettings = maps.Clone(u.ClusterSettings)
	out.User = u.User.clone()
	if u.ClusterVersion != nil {
		v := *u.ClusterVersion
		out.ClusterVersion = &v
	}
	if u.Alerts != nil {
		a := *u.Alerts
		out.Alerts = &a
	}
	out.AlertBoard = u.AlertBoard.clone()
	if u.Settings != nil {
		p := *u.Settings
		out.Settings = &p
	}
	out.Tabs = cloneTabs(u.Tabs)
	out.Navigator = u.Navigator.clone()
	out.dockMemo = maps.Clone(u.dockMemo)
	return out
}

// DockTab returns the dock tab with the given id.
func (u UIState) DockTab(id string) (DockTab, bool) {
	for _, t := range u.Dock {
		if t.ID == id {
			return t, true
		}
	}
	return DockTab{}, false
}
