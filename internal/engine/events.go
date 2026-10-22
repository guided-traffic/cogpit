package engine

// EventType says what an Event reports.
type EventType string

// Event types.
const (
	// EventUIState: the UI state changed; Event.UIState holds the new state.
	EventUIState EventType = "uiState"
	// EventTable: a new table snapshot for Event.View is available from Session.Table.
	EventTable EventType = "table"
	// EventDetail: the drawer object changed in the cache, or access reviews answered (its actions'
	// Enabled may differ); re-read Session.Detail and the actions.
	EventDetail EventType = "detail"
	// EventMetrics: new metrics for the drawer's object arrived (Event.View is its kind); re-read
	// Session.ObjectMetrics. Sent after every poll of the open drawer's pod or node, about every 15
	// seconds, and only while the drawer is open.
	EventMetrics EventType = "metrics"
	// EventToast: a message for the user (Event.Level, Event.Message). The engine keeps it in the
	// notification log (notifications.go) and sets Event.Notification to its id; Event.WindowOnly
	// keeps its text from callers outside the window there.
	EventToast EventType = "toast"
	// EventDiagnostics: request counters or cache stats changed; re-read Session.Diagnostics.
	EventDiagnostics EventType = "diagnostics"
	// EventDrains: the connected context's pending drains changed (offered on connect, one
	// started, progressed, ended or was dismissed); re-read them with node.pendingDrains.
	// Event.Message, if set, says what to offer.
	EventDrains EventType = "drains"
	// EventHelm: the connected context's pending Helm release actions changed (offered on
	// connect, one started, ended or was dismissed); re-read them with helm.pendingOperations.
	// Event.Message, if set, says what to offer.
	EventHelm EventType = "helm"
	// EventKinds: the kinds the connected cluster serves changed (a custom resource definition was
	// added, changed or deleted), or access reviews answered whether the user may list them;
	// re-read kinds.list.
	EventKinds EventType = "kinds"
	// EventNamespaces: the namespaces the cache holds of a cluster changed while its namespace
	// filter names namespaces (nswatch.go, AW11): one was added or deleted, or began or ended
	// terminating; Event.View holds the context. Coalesced; re-read them with resources.list.
	EventNamespaces EventType = "namespaces"
	// EventEditor: an editor tab's text or base version changed (editor.setText, a conflict);
	// Event.View holds the tab id, Event.Revision the text's revision. Re-read with editor.get.
	EventEditor EventType = "editor"
	// EventContexts: the contexts Cogpit can connect to changed (a kubeconfig source was added or
	// removed, a sync folder's files changed, a credential plugin was allowed); re-read them.
	EventContexts EventType = "contexts"
	// EventDrafts: the drafts of unsaved editor text Cogpit offers changed (offered on connect,
	// one restored or discarded); re-read them with editor.drafts. Event.Message, if set, says
	// what to offer.
	EventDrafts EventType = "drafts"
	// EventHelmRepos: the Helm chart repositories changed (Helm's repositories file was read
	// again, an index download started or ended); re-read them with helm.repo.list.
	EventHelmRepos EventType = "helmRepos"
	// EventIntegrations: what Cogpit knows of a cluster's integrations changed (a detection settled,
	// one was switched on or off; integrations.go); Event.View holds the context. Re-read them with
	// cluster.settings.get or Session.IntegrationStatuses.
	EventIntegrations EventType = "integrations"
	// EventAlerts: what a cluster's Alertmanager answered changed (its alerts, or whether it
	// answers; alerts.go, D-77); Event.View holds the context. Re-read the board with alerts.board.
	EventAlerts EventType = "alerts"
)

// Toast levels (Event.Level).
const (
	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"
)

// Event is something the UI or an agent may react to. Events carry little data: the receiver
// reads the current state from the Session.
type Event struct {
	Type     EventType `json:"type"`
	View     string    `json:"view,omitempty"`
	Revision uint64    `json:"revision,omitempty"`
	UIState  *UIState  `json:"uiState,omitempty"`
	Level    string    `json:"level,omitempty"` // toast: LevelInfo, LevelWarning, LevelError
	Message  string    `json:"message,omitempty"`
	// WindowOnly marks a toast whose text may quote what the cluster returned to a Helm action
	// (H-31): the notification log withholds it from agents and the CLI (notifications.go).
	WindowOnly bool `json:"windowOnly,omitempty"`
	// Notification is a toast's id in the notification log, set by the engine.
	Notification uint64 `json:"notification,omitempty"`
	// agentText is a WindowOnly toast's text for callers outside the window (EmitWindowToast).
	agentText string
}
