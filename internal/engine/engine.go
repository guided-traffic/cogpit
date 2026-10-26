// Package engine is Cogpit's core: the Session that every origin (GUI, CLI, external agents, the
// built-in agent) dispatches commands to, the command registry, the UI state, table snapshots,
// detail models, events and the confirmation policy (architecture §4).
//
// Layer L3. The engine reaches clusters only through packages cluster, cache and terminal.
// Everything that crosses the engine's boundary is JSON-serializable.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// Origin says who dispatched a command. Confirmation rules depend on it (D-2).
type Origin string

// Origins.
const (
	OriginGUI           Origin = "gui"
	OriginCLI           Origin = "cli"
	OriginExternalAgent Origin = "externalAgent"
	OriginBuiltInAgent  Origin = "builtInAgent"
)

// IsAgent reports whether the origin is an agent.
func (o Origin) IsAgent() bool { return o == OriginExternalAgent || o == OriginBuiltInAgent }

func (o Origin) valid() bool {
	switch o {
	case OriginGUI, OriginCLI, OriginExternalAgent, OriginBuiltInAgent, OriginDeepLink:
		return true
	}
	return false
}

// Invocation is one request to run a command.
type Invocation struct {
	ID     string          `json:"id"`               // "ui.navigate"
	Params json.RawMessage `json:"params,omitempty"` // validated against the command's schema
	Origin Origin          `json:"origin"`
	// ReadsLikeUser is set by the built-in agent for a turn over a connection the user marked
	// private (a local model or a private cloud; owner, 2026-10-14 late evening): its reads then
	// return what the window shows instead of what agents are given. Honoured only with
	// OriginBuiltInAgent; never read from JSON, so no MCP or CLI request can set it. Write rules
	// (D-2) are the same either way. Dispatch carries it in the command's context
	// (engine.ReadsLikeUser, readslikeuser.go) and marks the audit line.
	ReadsLikeUser bool `json:"-"`
}

// Errors returned by Dispatch. Match them with errors.Is.
var (
	ErrUnknownCommand          = errors.New("unknown command")
	ErrDeclined                = errors.New("the user declined the command")
	ErrConfirmationUnavailable = errors.New("the command needs a confirmation and no GUI is available to confirm it")
	ErrNotConnected            = errors.New("no cluster connected")
)

// ParamsError reports parameters that do not match a command's schema.
type ParamsError struct {
	Command string
	Err     error
}

func (e *ParamsError) Error() string {
	return fmt.Sprintf("command %s: invalid params: %v", e.Command, e.Err)
}
func (e *ParamsError) Unwrap() error { return e.Err }

// DisabledError reports a command that cannot run in the current state, with the reason.
type DisabledError struct {
	Command string
	Reason  string
	err     error // the reason's error, for errors.Is (a context that is not connected: ErrNotConnected)
}

func (e *DisabledError) Error() string {
	return fmt.Sprintf("command %s is disabled: %s", e.Command, e.Reason)
}

// Unwrap returns the error the reason comes from, if any.
func (e *DisabledError) Unwrap() error { return e.err }

// Options configure a Session.
type Options struct {
	// Connector lists and connects kubeconfig contexts (and the demo cluster). Nil: no cluster
	// can be connected.
	Connector Connector
	// Confirmer asks the user to confirm. Nil means headless: commands that need a confirmation
	// are refused with ErrConfirmationUnavailable.
	Confirmer Confirmer
	// Audit receives one JSON line per agent-initiated command. Nil discards.
	Audit io.Writer
	// AuditPath is the file Audit appends to, which the Audit Log and audit.list read back
	// (auditlist.go). Empty: the session cannot read its audit log.
	AuditPath string
	// ConnectLogDir is where the connection log keeps one owner-only file per context, every line
	// appended as it happens and read back by the Connection Updates dialog and cluster.connectLog
	// (connectlog_files.go, D-66): <user config dir>/Cogpit/connection-logs in both binaries
	// (app.Dirs.ConnectLogDir). Empty: the log lasts for the session.
	ConnectLogDir string
	// SnapshotInterval is the minimum time between two table snapshots of a view; zero means
	// 100 ms, at most ten snapshots per second (architecture §5).
	SnapshotInterval time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// K8sReleases is the Kubernetes release table the version marks read (k8sversion.go, D-91);
	// nil means the one built in (k8seol.Embedded). Tests pass their own.
	K8sReleases *k8seol.Table
	// AfterFunc runs f once d has passed on the session's clock, as time.AfterFunc does, and
	// returns its stop; nil means time.AfterFunc. Tests that move Now fire it themselves (the
	// terminal tabs' agent-read switch, agentread.go).
	AfterFunc func(d time.Duration, f func()) (stop func() bool)
	// Logger receives the engine's logs; nil means slog.Default().
	Logger *slog.Logger
	// Drains keeps the drains Cogpit starts in a journal, so that one a crash interrupts is
	// offered again when its context is connected (D-11; drains.go). Nil: node.drain is refused.
	Drains *Drains
	// NodeShells keeps the node shell pods Cogpit creates in a journal, so that one a crash leaves
	// is deleted when its context is connected again (H-71; nodeshelljournal.go). Nil: an
	// in-memory journal, which retries a failed delete within the run only.
	NodeShells NodeShellJournal
	// Helm keeps the Helm release actions Cogpit starts in a journal, so that one a crash
	// interrupts is offered again when its context is connected (D-11; helmops.go). Nil: rollback
	// and uninstall are refused.
	Helm *HelmOps
	// Hotbar keeps the hotbar (hotbar.go), shown in UIState.Hotbar. Nil: the session has none and
	// the hotbar commands are disabled.
	Hotbar *Hotbar
	// HelmRepos reads the Helm chart repositories of Helm's repositories file and keeps their
	// indexes (helmrepos.go), whose charts are the local kind helmcharts. Nil: the session has
	// none, the repository commands are disabled and charts cannot be installed.
	HelmRepos *HelmRepos
	// Preferences keeps Cogpit's preferences (preferences.go), shown in UIState.Preferences. Nil:
	// the session has none, the defaults apply and the preferences commands are disabled.
	Preferences *PreferencesStore
	// ClusterSettings keeps each context's settings (clustersettings.go), shown in
	// UIState.ClusterSettings. Nil: the session has none and their commands are disabled.
	ClusterSettings *ClusterSettingsStore
	// Kubectl checks the kubectl of the terminals and downloads the release that matches a cluster
	// (kubectl.go, D-3). Nil: terminals run the kubectl on PATH unchecked.
	Kubectl *Kubectl
	// Drafts keeps the unsaved text of Edit and Create resource tabs on disk, so that one a crash
	// interrupts is offered again when its context is connected (D-11; drafts.go). Nil: no drafts,
	// and the draft commands are disabled.
	Drafts *Drafts
	// Workspace keeps the tab bar's tabs and the tables' column layout across a restart (D-41;
	// persist.go): restored in New without connecting anything, written when they change. Nil:
	// they last for the session.
	Workspace *Workspace
	// TimeLayout is how absolute times are written for the user (D-31): the computer's region
	// format, which the binaries look up once at start (internal/locale), shown in
	// UIState.TimeFormat and written in the Preferences' time zone (Session.TimeFormat). Empty
	// fields: ISO 8601.
	TimeLayout model.TimeLayout
	// LoginItem is the operating system's start at login for Cogpit's app (loginitem.go): the
	// desktop binary wires in macOS's; nil: the platform has none, and it is not offered.
	LoginItem LoginItem
	// ConnWatch tunes the connection watch (connwatch.go, D-58); the zero value is the owner's
	// values (a heartbeat every 15 s, reconnect attempts after 1 to 30 s). Tests shorten them.
	ConnWatch ConnWatchOptions
	// Alerts tunes the polls of each cluster's Alertmanager (alerts.go, D-77); the zero value is
	// alerting's defaults (every 30 seconds while the cluster is shown). Tests shorten them.
	Alerts alerting.Options
}

// Session is the engine for one desktop app or headless process. It is safe for concurrent use.
type Session struct {
	reg  *Registry
	opts Options
	log  *slog.Logger

	mu          sync.Mutex
	ui          UIState
	notes       notificationLog // the toasts the user was shown (notifications.go)
	confirmer   Confirmer
	agentWrites map[string]bool  // contexts where the user allowed agent writes (D-2)
	dockReader  DockOutputReader // the window's, for dock.read (dockoutput.go)
	zoomCeiling *int             // the largest zoom level the window's content fits at; nil: no window said (zoom.go)
	// integrationsLater are the contexts whose integrations question was put off this run
	// (integrationsask.go, D-76); guarded by mu.
	integrationsLater map[string]bool

	subMu   sync.Mutex
	subs    map[int]func(Event)
	nextSub int

	auditMu sync.Mutex

	state  *state       // views, backend and streams: views.go
	edits  editStore    // the editor tabs' texts and base versions: editor.go
	titles sync.Map     // view key → the view's title, for the tabs' titles (tabs.go)
	shells nodeShellSet // the node shells' pods by dock tab: nodeshell.go

	agentReads agentReadExpiry // when a terminal tab's agent-read switch turns itself off: agentread.go
	updates    updaterSlot     // the app's updater: updates.go
	chat       chatSlot        // the built-in agent's chat: chat.go
	jobs       jobSet          // the running background jobs: jobs.go

	drawerEdits drawerEditStore // a Config Map's values edited in the drawer, not saved: drafts_drawer.go
	users       userBook        // who Cogpit is logged in as, per context: whoami.go
	alerts      alertBook       // what each cluster's Alertmanager answered last: alerts.go
	versions    versionBook     // each connected cluster's API server version: k8sversion.go
}

// New returns a Session with an empty registry. Register commands (package commands) before use.
func New(opts Options) *Session {
	if opts.SnapshotInterval <= 0 {
		opts.SnapshotInterval = 100 * time.Millisecond
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NodeShells == nil {
		opts.NodeShells = NewMemoryNodeShellJournal()
	}
	if opts.AfterFunc == nil {
		opts.AfterFunc = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Session{
		reg:         NewRegistry(),
		opts:        opts,
		log:         opts.Logger,
		confirmer:   opts.Confirmer,
		agentWrites: map[string]bool{},
		subs:        map[int]func(Event){},
		ui: UIState{
			Connection:     Connection{State: Disconnected},
			Sort:           SortSpec{Column: "name"},
			NavigatorScope: NavScopeLocal, // no cluster yet: the navigator lists them (navscope.go)
			TimeFormat:     opts.TimeLayout.OrDefault(),
			// Asks before it looks at a cluster's integrations (integrationsconsent.go, D-82).
			IntegrationsAskFirst: opts.ClusterSettings != nil,
		},
	}
	s.ui.syncWelcome() // not connected: the welcome page (welcome.go)
	// Each line written to the audit log tells the Audit Log tab (auditlist.go).
	s.opts.Audit = s.notifyingAudit(opts.Audit)
	s.state = newState(s)
	s.state.logs.setup(s.opts.Now, newConnectLogFiles(opts.ConnectLogDir), // connectlog.go, D-66
		func(ctx string) { s.emit(Event{Type: EventConnectLog, View: ctx}) }, s.connectLogUnwritable)
	opts.Drains.attach(s) // nil-safe
	opts.Helm.attach(s)   // nil-safe
	opts.Hotbar.attach(s) // nil-safe
	// The node shell pods a crash left are deleted on connect (nodeshellsweep.go).
	s.Subscribe(s.followNodeShellSweep)
	opts.HelmRepos.attach(s)
	opts.Preferences.attach(s)
	opts.ClusterSettings.attach(s)
	s.users.attach(s)        // the last known users, beside the cluster settings (whoami_store.go)
	opts.Drafts.attach(s)    // nil-safe
	opts.Workspace.attach(s) // nil-safe; reads the kubeconfig sources' files, never a cluster
	s.attachLoginItem()      // offered when the binary wired one in (loginitem.go)
	return s
}

// Registry returns the command registry: menus, context menus, the palette and MCP tools come
// from it.
func (s *Session) Registry() *Registry { return s.reg }

// Now returns the session clock's time.
func (s *Session) Now() time.Time { return s.opts.Now() }

// Logger returns the session's logger.
func (s *Session) Logger() *slog.Logger { return s.log }

// SetConfirmer installs the confirmer (the GUI installs itself once its window exists).
func (s *Session) SetConfirmer(c Confirmer) {
	s.mu.Lock()
	s.confirmer = c
	s.mu.Unlock()
}

// SetAgentWrites allows or forbids mutating agent commands without asking, for one context
// (D-2). Destructive commands always ask.
func (s *Session) SetAgentWrites(context string, allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if allowed {
		s.agentWrites[context] = true
	} else {
		delete(s.agentWrites, context)
	}
}

// NeedsConfirmation is the D-2 policy: destructive commands always need the user's confirmation;
// mutating commands need it from agents unless the user allowed agent writes for the context, and
// always when the command declares that agents ask for this invocation (agentsAsk: an agent's RBAC
// write, H-27, A1); a UI command needs it from agents only when it declares so for the invocation
// (agentsAsk: an agent's proxy, A20, owner 2026-10-14 evening), the allowance of agent writes not
// bearing on it; read commands never do.
func NeedsConfirmation(d Danger, o Origin, agentWritesAllowed, agentsAsk bool) bool {
	switch d {
	case DangerDestructive:
		return true
	case DangerMutating:
		return o.IsAgent() && (agentsAsk || !agentWritesAllowed)
	case DangerUI:
		return o.IsAgent() && agentsAsk
	}
	return false
}

// Dispatch validates and runs one command: unknown ids, bad parameters and disabled commands are
// refused; commands that need a confirmation wait for the user; agent-initiated commands are
// audited; a panicking command returns an error instead of ending the process.
func (s *Session) Dispatch(ctx context.Context, inv Invocation) (result any, err error) {
	cmd, ok := s.reg.Get(inv.ID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCommand, inv.ID)
	}
	if !inv.Origin.valid() {
		return nil, fmt.Errorf("command %s: invalid origin %q", inv.ID, inv.Origin)
	}
	if err := checkDeepLinkCommand(cmd, inv.Origin); err != nil { // a link only navigates (deeplink.go)
		return nil, err
	}
	// auditContext is the context the command was dispatched for (or, once confirmed, the one the
	// user confirmed), not whatever is connected when it finishes.
	var auditContext string
	if inv.Origin.IsAgent() {
		defer func() { s.audit(inv, auditContext, auditDone, err) }()
	}
	p, err := cmd.decode(inv.Params)
	if err != nil {
		return nil, err
	}
	// A GUI-only command is refused to every other origin before its Enabled runs, which may read
	// what the refused origin must not learn (guionly.go).
	if err := cmd.refuseOrigin(inv.Origin); err != nil {
		return nil, err
	}
	// A command acts on one cluster: references of several connected clusters are refused before
	// anything is checked or asked (D-69, target.go).
	if err := oneCluster(paramsRefs(p)); err != nil {
		return nil, fmt.Errorf("command %s: %w", cmd.ID, err)
	}
	if err := s.safeCall(cmd.ID, func() error { return cmd.enabled(s, p) }); err != nil {
		return nil, &DisabledError{Command: cmd.ID, Reason: err.Error(), err: err}
	}
	if cmd.refuse != nil { // before any confirmation: never ask the user for what is refused anyway
		octx := withInvocation(ctx, inv)
		if err := s.safeCall(cmd.ID, func() error { return cmd.refuse(octx, s, p) }); err != nil {
			return nil, fmt.Errorf("command %s: %w", cmd.ID, err)
		}
	}

	// The cluster the command reads or acts on (D-69, target.go): its parameters' context, dock
	// tab or reference, else the shown cluster. The allowance of agent writes is that cluster's,
	// and so is the context its confirmation names.
	var target string
	if err := s.safeCall(cmd.ID, func() error { target = targetContext(s, p); return nil }); err != nil {
		return nil, err
	}
	s.mu.Lock()
	uiContext := target
	if uiContext == "" {
		uiContext = s.ui.Context
	}
	allowed := s.agentWritesLocked(uiContext) // the run's or the kept allowance (allowance.go)
	confirmer := s.confirmer
	s.mu.Unlock()
	auditContext = uiContext

	danger := cmd.Danger
	if err := s.safeCall(cmd.ID, func() error { danger = cmd.dangerFor(s, p); return nil }); err != nil {
		return nil, err
	}
	if err := checkDeepLinkDanger(cmd.ID, inv.Origin, danger); err != nil { // never a link's write (deeplink.go)
		return nil, err
	}
	agentsAsk := false // judged for an agent's write or UI change only: the GUI's own command is unaffected (agentsask.go)
	if inv.Origin.IsAgent() && (danger == DangerMutating || danger == DangerUI) {
		if err := s.safeCall(cmd.ID, func() error { agentsAsk = cmd.agentsAskFor(s, p); return nil }); err != nil {
			return nil, err
		}
	}
	if NeedsConfirmation(danger, inv.Origin, allowed, agentsAsk) {
		var targets []model.ObjectRef
		if err := s.safeCall(cmd.ID, func() (err error) { targets, err = cmd.targets(s, p); return err }); err != nil {
			return nil, err
		}
		if err := oneCluster(targets); err != nil { // one confirmation names one context (D-69)
			return nil, fmt.Errorf("command %s: %w", cmd.ID, err)
		}
		if confirmer == nil {
			return nil, fmt.Errorf("command %s: %w", cmd.ID, ErrConfirmationUnavailable)
		}
		req := newConfirmRequest(cmd, danger, inv.Origin, uiContext, targets)
		req.Kinds = s.kindNames(targets)
		req.AgentsAsk = agentsAsk
		if cmd.effects != nil { // rows for what the objects don't say (D-85, WF33); Detail is their text
			if err := s.safeCall(cmd.ID, func() error { req.Effects = cmd.effects(s, p); return nil }); err != nil {
				return nil, err
			}
			req.Detail = EffectsText(req.Effects)
		}
		if cmd.warning != nil { // a chart nothing authenticates (H-41)
			if err := s.safeCall(cmd.ID, func() error { req.Warning = cmd.warning(s, p); return nil }); err != nil {
				return nil, err
			}
		}
		if cmd.review != nil {
			if err := s.safeCall(cmd.ID, func() error { req.Review = cmd.review(s, p); return nil }); err != nil {
				return nil, err
			}
		}
		if danger == DangerDestructive {
			req.UIDs = s.targetUIDs(ctx, targets)
		} else {
			req.UIDs = s.cachedTargetUIDs(targets) // an agent's Scale the user accepts binds them too
		}
		ok, err := confirmer.Confirm(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("command %s: confirmation: %w", cmd.ID, err)
		}
		if !ok {
			return nil, fmt.Errorf("command %s: %w", cmd.ID, ErrDeclined)
		}
		// The connection may have broken while the user read the dialog (D-58).
		if err := s.writeBlocked(danger, req.Context); err != nil {
			return nil, &DisabledError{Command: cmd.ID, Reason: err.Error(), err: err}
		}
		ctx = context.WithValue(ctx, confirmedKey{}, req)
		auditContext = req.Context
	} else if allowed && danger == DangerMutating && inv.Origin.IsAgent() {
		// The user's allowance of agent writes is for one context: bind the command to it the way
		// a confirmation would, so that it refuses to act if the cluster changes before it runs.
		ctx = context.WithValue(ctx, confirmedKey{}, newConfirmRequest(cmd, danger, inv.Origin, uiContext, nil))
	}
	if inv.Origin.IsAgent() && (danger == DangerMutating || danger == DangerDestructive) {
		// A write is logged before it runs too, so that one still running when Cogpit quits is in
		// the log.
		s.audit(inv, auditContext, auditRequested, nil)
	}

	// The command's context carries its origin and, for the built-in agent over a private
	// connection, that it reads like the user (readslikeuser.go): only that origin's mark counts.
	ctx = withInvocation(ctx, inv)
	private := inv.privateTurn()
	err = s.safeCall(cmd.ID, func() (err error) { result, err = cmd.run(ctx, s, p); return err })
	if err == nil && inv.Origin.IsAgent() && !private && needsAgentSecrets(result) { // the Secrets it is read against (agentsecrets.go)
		if err = s.BackendOf(target).agentSecrets(ctx); err != nil { // the read cluster's Secrets (D-69)
			result = nil
		}
	}
	// The cached Secrets' values, Flux's credentials (agentview.go); a private turn's result as the
	// window reads it, a chart's README still framed.
	if err == nil && inv.Origin.IsAgent() {
		if err = s.safeCall(cmd.ID, func() error { result = s.forAgent(result, private); return nil }); err != nil {
			result = nil
		}
	}
	return result, err
}

// safeCall runs fn and turns a panic into an error, logged with its stack.
func (s *Session) safeCall(id string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("command panicked", "command", id, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("command %s failed: internal error: %v", id, r)
		}
	}()
	return fn()
}

// UIState returns a copy of the current UI state.
func (s *Session) UIState() UIState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ui.Clone()
}

// UpdateUI changes the UI state with fn, keeps the tabs, the welcome page and the navigator's scope
// in step with it (UIState.syncTabs, syncWelcome, syncNavScope), bumps its revision, lets the views follow the change (acquire
// and release cache handles, rebuild snapshots) and emits EventUIState. Commands call it; the GUI never does directly. It returns the
// new state.
func (s *Session) UpdateUI(fn func(*UIState)) UIState {
	s.mu.Lock()
	old := s.ui.Clone()
	fn(&s.ui)
	s.ui.syncConnections()          // the shown cluster's entry of Connections (connections.go, D-69)
	s.ui.syncStates()               // a status filter ends with its view (statusfilter.go)
	s.ui.syncTabs(old, s.viewTitle) // the active tab follows the view, whoever changed it (tabs.go)
	s.ui.syncConnectFailure()       // a failed connect's page, while it is the shown cluster's (connectfailure.go, D-114)
	s.ui.syncWelcome()              // the welcome page shows while the workspace has nothing else (welcome.go)
	s.ui.syncNavScope(old)          // the navigator shows the connected cluster, or the section (navscope.go)
	s.ui.syncNavigator(old)         // its search and keyboard follow the level and the view (navigator.go)
	// The question of a new cluster's integrations (integrationsask.go, D-76).
	s.ui.syncIntegrationsQuestion(s.opts.ClusterSettings != nil, s.integrationsLater)
	// A chart drawer's version ends with the drawer (helmchartdrawer.go).
	s.ui.syncDrawerChartVersion(old)
	s.ui.SyncDock(old) // a new or activated tab opens the dock, none closes it (layout.go)
	// Who Cogpit is logged in as (whoami.go).
	s.ui.syncUser(old, &s.users, s.opts.Now())
	s.ui.syncAlerts(&s.alerts) // the shown cluster's alerts, the title bar's pill (alerts.go, D-77)
	// The shown cluster's Kubernetes version and its upstream support (k8sversion.go, D-91).
	s.ui.syncClusterVersion(&s.versions, s.releases(), s.opts.Now())
	s.ui.Revision = old.Revision + 1
	now := s.ui.Clone()
	s.mu.Unlock()

	s.state.uiChanged(old, now)
	s.users.flush(s)                          // a connection that ended was its user's last sighting (whoami_store.go)
	s.endClosedNodeShells(old.Dock, now.Dock) // a closed node shell tab's pod goes (nodeshell.go)
	s.followJobs(old, now)                    // a closed job tab, or its cluster's end, ends its job (jobs.go)
	s.emit(Event{Type: EventUIState, UIState: &now})
	return now
}

// Subscribe registers fn for every engine event. fn runs on engine goroutines and must not block;
// the GUI hands events to its main goroutine with fyne.Do. The returned function unsubscribes.
func (s *Session) Subscribe(fn func(Event)) (cancel func()) {
	s.subMu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	s.subMu.Unlock()
	return func() {
		s.subMu.Lock()
		delete(s.subs, id)
		s.subMu.Unlock()
	}
}

// emit delivers an event to every subscriber. A panicking subscriber is logged and skipped. A
// toast goes into the notification log first, and the event carries its id (notifications.go).
func (s *Session) emit(ev Event) {
	if ev.Type == EventToast && ev.Message != "" {
		ev.Notification = s.recordToast(ev.Level, ev.Message, ev.WindowOnly, ev.agentText)
	}
	s.subMu.Lock()
	fns := make([]func(Event), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.subMu.Unlock()
	for _, fn := range fns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.log.Error("event subscriber panicked", "event", ev.Type, "panic", fmt.Sprint(r))
				}
			}()
			fn(ev)
		}()
	}
}

// Emit delivers an event to every subscriber. Commands use it for toasts.
func (s *Session) Emit(ev Event) { s.emit(ev) }

// Close disconnects and stops every view. The session must not be used afterwards.
func (s *Session) Close() {
	s.opts.Drafts.close()    // the drafts still waiting for their timer are written first (nil-safe)
	s.opts.Workspace.close() // the tabs as the user left them, before the connection closes (nil-safe)
	s.endNodeShells()        // the node shells' pods are deleted before the connection goes (nodeshell.go)
	s.agentReads.stop()      // no switch turns itself off after the session (agentread.go)
	s.jobs.stopAll()         // the background jobs end before the connection goes (jobs.go)
	s.state.close()
}
