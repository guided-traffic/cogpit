package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/alerting"
)

// The Alertmanager integration (D-77; I4 and I5 of the owner's ticket of 2026-10-17): Cogpit finds a
// cluster's Alertmanager among its cached Services by its well-known names, or reads the address
// the user set (alerts_settings.go), and asks it for the active alerts that are neither silenced
// nor inhibited, one GET through the API server's service proxy every 30 seconds while the
// integration is on and the cluster is shown and connected (package alerting). What it answered is
// the title bar's pill (UIState.Alerts) and the alert board, a workspace page (AlertBoard,
// alerts_board.go); agents read it with alerts.list. Alerts cannot be watched, so this is the one
// integration that polls; a cluster connected in the background (D-69) is not polled, and shows
// the answer it had when it was last shown until it is shown again.
//
// Off for a cluster (integrations.go), it sends nothing, its last answer is forgotten and the
// window shows nothing of it: no pill, no board, nothing in ui_state.

// AlertBoard is the page key of the alert board.
const AlertBoard = "alerts/board"

// AlertBoardTitle is the board's title in its header and its tab.
const AlertBoardTitle = "Alerts"

func init() {
	pageTitles[AlertBoard] = AlertBoardTitle
	integrationProbes[IntegrationAlertmanager] = probeAlertmanager
	integrationFollowers = append(integrationFollowers, followAlertmanager)
}

// AlertsState is what Cogpit knows of the shown cluster's Alertmanager (alerting.State).
type AlertsState string

// The states of UIState.Alerts.
const (
	// AlertsChecking: nothing is known yet; the first poll runs.
	AlertsChecking AlertsState = "checking"
	// AlertsFound: an Alertmanager found by its Service's name answers.
	AlertsFound AlertsState = "found"
	// AlertsConfigured: the address the user set answers.
	AlertsConfigured AlertsState = "configured"
	// AlertsNotFound: none is found by name; the user can set its address.
	AlertsNotFound AlertsState = "notFound"
	// AlertsForbidden: the user may not use the service proxy (get services/proxy).
	AlertsForbidden AlertsState = "forbidden"
	// AlertsUnavailable: found or set, but it does not answer; the counts are its last answer's,
	// if it answered before (Stale).
	AlertsUnavailable AlertsState = "unavailable"
)

// AlertsSummary is the shown cluster's alerts as the title bar's pill shows them (UIState.Alerts):
// how many, by severity, and what Cogpit knows of the Alertmanager. The alerts themselves are
// alerts.list's and alerts.board's.
type AlertsSummary struct {
	State AlertsState `json:"state"`
	// Where is the Alertmanager read, "monitoring/kube-prometheus-alertmanager:http-web"; empty
	// while none is.
	Where string `json:"where,omitempty"`
	// Total counts every shown alert, the pill's number: active, neither silenced nor inhibited,
	// the meta alerts (Watchdog, InfoInhibitor) and severity none left out (D-77).
	Total    int `json:"total"`
	Critical int `json:"critical,omitempty"`
	Warning  int `json:"warning,omitempty"` // warning, major, and a missing or unknown severity
	Info     int `json:"info,omitempty"`
	// Worst is the worst severity among them (critical, warning, info), the pill's colour; empty
	// without alerts.
	Worst string `json:"worst,omitempty"`
	// Answered says the counts are an answer of the Alertmanager; false while none came (checking,
	// not found, forbidden, unavailable from the start): the pill shows a dash.
	Answered bool `json:"answered,omitempty"`
	// FailingSince is when the Alertmanager stopped answering; the counts are its last answer's,
	// kept for alerting.DefaultKeepFor at most. Zero while it answers.
	FailingSince time.Time `json:"failingSince,omitzero"`
	// Detail says why nothing is read: not found, forbidden, does not answer.
	Detail string `json:"detail,omitempty"`
}

// summaryOf counts a snapshot's alerts.
func summaryOf(snap alerting.Snapshot) AlertsSummary {
	s := AlertsSummary{State: AlertsState(snap.State), Answered: snap.Answered(), FailingSince: snap.FailingSince, Detail: snap.Error}
	if snap.Target != (alerting.Target{}) {
		s.Where = snap.Target.Where()
	}
	for _, a := range snap.Alerts {
		switch a.Severity {
		case alerting.SeverityCritical:
			s.Critical++
		case alerting.SeverityWarning:
			s.Warning++
		case alerting.SeverityInfo:
			s.Info++
		}
	}
	s.Total = snap.Total
	switch {
	case s.Critical > 0:
		s.Worst = string(alerting.SeverityCritical)
	case s.Warning > 0:
		s.Worst = string(alerting.SeverityWarning)
	case s.Info > 0:
		s.Worst = string(alerting.SeverityInfo)
	}
	return s
}

// alertBook holds each cluster's last snapshot, by context id: what the UI state's pill, the board
// and alerts.list read. It has its own lock, so that UpdateUI reads it without the state's lock
// (lock order, views.go).
type alertBook struct {
	mu sync.Mutex
	by map[string]alertEntry
}

type alertEntry struct {
	snap    alerting.Snapshot
	summary AlertsSummary
	// none says why the cluster has no Alertmanager to read: the demo cluster. The pill is hidden
	// for it.
	none string
}

// set keeps a snapshot; it reports whether the summary (the pill) changed, and whether what the
// integration's status shows did (its state, where, why).
func (k *alertBook) set(contextID string, snap alerting.Snapshot) (pill, status bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.by == nil {
		k.by = map[string]alertEntry{}
	}
	sum := summaryOf(snap)
	old, had := k.by[contextID]
	k.by[contextID] = alertEntry{snap: snap, summary: sum}
	if !had || old.none != "" {
		return true, true
	}
	o := old.summary
	return o != sum, o.State != sum.State || o.Where != sum.Where || o.Detail != sum.Detail
}

// setNone marks a cluster that has no Alertmanager Cogpit can read (why).
func (k *alertBook) setNone(contextID, why string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.by == nil {
		k.by = map[string]alertEntry{}
	}
	old, had := k.by[contextID]
	k.by[contextID] = alertEntry{none: why}
	return !had || old.none != why
}

// forget drops a cluster's entry; it reports whether there was one.
func (k *alertBook) forget(contextID string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, had := k.by[contextID]
	delete(k.by, contextID)
	return had
}

// entry returns a cluster's entry.
func (k *alertBook) entry(contextID string) (alertEntry, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.by[contextID]
	return e, ok
}

// syncAlerts derives the pill's state (UIState.Alerts) from the book: the shown cluster's, while it
// is live and its integration is on; checking until its poller answered.
func (u *UIState) syncAlerts(k *alertBook) {
	u.Alerts = nil
	if u.Context == "" || !u.Connection.State.Live() || !u.IntegrationOn(u.Context, IntegrationAlertmanager) {
		return
	}
	e, ok := k.entry(u.Context)
	switch {
	case ok && e.none != "":
		return
	case ok:
		sum := e.summary
		u.Alerts = &sum
	default:
		u.Alerts = &AlertsSummary{State: AlertsChecking}
	}
}

// alertSource is a backend's Alertmanager poller, while the integration is on.
type alertSource struct {
	mu sync.Mutex
	p  *alerting.Poller
}

// current returns the running poller, or nil.
func (a *alertSource) current() *alerting.Poller {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.p
}

// ensure returns the running poller, made with mk when none runs; made says it is new. mk runs
// without the source's lock.
func (a *alertSource) ensure(mk func() *alerting.Poller) (*alerting.Poller, bool) {
	a.mu.Lock()
	p := a.p
	a.mu.Unlock()
	if p != nil {
		return p, false
	}
	p = mk()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.p != nil { // made meanwhile: the follower runs under the state's lock, so not expected
		p.Stop()
		return a.p, false
	}
	a.p = p
	return p, true
}

// stop ends the running poller without waiting for it (alerting.Poller.Stop); it reports whether
// one ran.
func (a *alertSource) stop() bool {
	a.mu.Lock()
	p := a.p
	a.p = nil
	a.mu.Unlock()
	if p == nil {
		return false
	}
	p.Stop()
	return true
}

// noAlertmanager says why a backend has no Alertmanager Cogpit can read: the demo cluster, a
// backend without a connection; "" for a real cluster.
func noAlertmanager(b *Backend) string {
	switch {
	case b.Demo:
		return "the demo cluster has no Alertmanager"
	case b.Conn == nil || b.Conn.Kube() == nil:
		return "this connection cannot reach the cluster's services"
	}
	return ""
}

// noteNoAlertmanager tells the book, before a backend is published, that it cannot have an
// Alertmanager (the demo cluster, a connection without clients): the UI state that shows the new
// connection then hides the pill at once, and the follower finds nothing new to tell, so no second
// UI state follows on a goroutine of its own.
func (s *Session) noteNoAlertmanager(b *Backend) {
	if why := noAlertmanager(b); why != "" {
		s.alerts.setNone(b.Context, why)
	}
}

// followAlertmanager is the integration's follower (integrations.go): it runs the cluster's poller
// while the integration is on, active while the cluster is shown and connected, and stops it when
// the integration goes off. It runs with the state's lock held: nothing here waits for the poller.
func followAlertmanager(st *state, b *Backend, now UIState) {
	s := st.s
	if !now.IntegrationOn(b.Context, IntegrationAlertmanager) {
		stopped := b.alerts.stop()
		if s.alerts.forget(b.Context) || stopped {
			s.alertsNoticed(b.Context, true, true)
		}
		return
	}
	if why := noAlertmanager(b); why != "" {
		if s.alerts.setNone(b.Context, why) {
			s.alertsNoticed(b.Context, true, true)
		}
		return
	}
	p, made := b.alerts.ensure(func() *alerting.Poller { return s.newAlertPoller(b) })
	if made {
		// What an earlier connection of the context answered no longer stands; then the user's
		// address, which tells the book through OnChange when there is one.
		if s.alerts.forget(b.Context) {
			s.alertsNoticed(b.Context, true, true)
		}
		p.SetAddress(b.MetricsSettings.Alertmanager(b.Context))
	}
	p.SetActive(now.Context == b.Context && now.connectionState(b.Context) == Connected)
}

// newAlertPoller makes a backend's poller: the cache's Services for detection, the connection's
// clients for the service proxy, the address the user set for the context (if any).
func (s *Session) newAlertPoller(b *Backend) *alerting.Poller {
	opts := s.opts.Alerts
	if opts.Logger == nil {
		opts.Logger = s.log
	}
	var p *alerting.Poller
	opts.OnChange = func() { s.alertsChanged(b, p) }
	p = alerting.NewPoller(b.Conn.Kube(), alerting.ServiceSourceFunc(b.alertServices), opts)
	return p
}

// alertServices lists the cluster's Services for detection, from the cache (as Prometheus's,
// promServices: no list of its own).
func (b *Backend) alertServices(ctx context.Context) ([]alerting.Service, error) {
	svcs, err := b.promServices(ctx)
	if err != nil {
		if p, ok := b.Profile("services"); ok {
			if listable, known := b.Listable(p, nil); known && !listable {
				return nil, errors.New("the current user may not list services; set the Alertmanager's address")
			}
		}
		return nil, errors.New("they are not loaded; set the Alertmanager's address")
	}
	out := make([]alerting.Service, 0, len(svcs))
	for _, sv := range svcs {
		as := alerting.Service{Namespace: sv.Namespace, Name: sv.Name}
		for _, p := range sv.Ports {
			as.Ports = append(as.Ports, alerting.ServicePort{Name: p.Name, Port: p.Port})
		}
		out = append(out, as)
	}
	return out, nil
}

// alertsChanged follows a poller's change (its OnChange, on the poller's goroutine): the book takes
// its snapshot, the board and the settings hear it, and the UI state's pill follows when it is the
// shown cluster's. A stopped poller's late change is dropped.
func (s *Session) alertsChanged(b *Backend, p *alerting.Poller) {
	if p == nil || b.alerts.current() != p {
		return
	}
	pill, status := s.alerts.set(b.Context, p.Snapshot())
	s.alertsNoticed(b.Context, pill, status)
}

// alertsNoticed tells the window and agents, on a goroutine of its own (its callers may hold the
// state's lock), that a cluster's alerts changed: the pill through the UI state when it is the
// shown cluster's and changed, the board (EventAlerts), and the integration's status when it
// changed (EventIntegrations).
func (s *Session) alertsNoticed(contextID string, pill, status bool) {
	s.goSafe("the alerts' notice", func() {
		if pill && s.UIState().Context == contextID {
			s.UpdateUI(func(*UIState) {}) // syncAlerts reads the book
		}
		s.emit(Event{Type: EventAlerts, View: contextID})
		if status {
			s.integrationsChanged(contextID)
		}
	})
}

// probeAlertmanager is the integration's probe (integrations.go): what the poller knows, no
// request.
func probeAlertmanager(s *Session, b *Backend, on bool) (IntegrationState, string, string) {
	if !on {
		return IntegrationOff, "", ""
	}
	if why := noAlertmanager(b); why != "" {
		return IntegrationMissing, "", why
	}
	p := b.alerts.current()
	if p == nil {
		return IntegrationChecking, "", ""
	}
	snap := p.Snapshot()
	where := ""
	if snap.Target != (alerting.Target{}) {
		where = snap.Target.Where()
	}
	switch snap.State {
	case alerting.StateFound:
		return IntegrationFound, where, ""
	case alerting.StateConfigured:
		return IntegrationConfigured, where, ""
	case alerting.StateNotFound:
		return IntegrationManual, "", "none found by name: set its address"
	case alerting.StateForbidden:
		return IntegrationForbidden, where, snap.Error
	case alerting.StateUnavailable:
		return IntegrationUnavailable, where, snap.Error
	}
	if s.UIState().Context != b.Context {
		return IntegrationChecking, "", "looked for while the cluster is shown"
	}
	return IntegrationChecking, "", ""
}

// alertSnapshot is a connected cluster's last answer ("" the shown cluster's), for alerts.list and
// alerts.board: the backend, its snapshot, and false when the integration is off or the cluster
// has no Alertmanager to read (the error says which).
func (s *Session) alertSnapshot(contextID string) (*Backend, alerting.Snapshot, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return nil, alerting.Snapshot{}, err
	}
	if u := s.UIState(); !u.IntegrationOn(b.Context, IntegrationAlertmanager) {
		return nil, alerting.Snapshot{}, ErrAlertmanagerOff
	}
	if why := noAlertmanager(b); why != "" {
		return nil, alerting.Snapshot{}, fmt.Errorf("%w: %s", ErrNotApplicable, why)
	}
	e, ok := s.alerts.entry(b.Context)
	if !ok {
		return b, alerting.Snapshot{State: alerting.StateChecking}, nil
	}
	return b, e.snap, nil
}

// ErrAlertmanagerOff refuses the alert commands of a cluster whose Alertmanager integration is off.
var ErrAlertmanagerOff = errors.New("the Alertmanager integration is off for this cluster: switch it on in Cluster Settings › Integrations")

// AlertmanagerReady says why the alert commands do not apply to a cluster ("" the shown one): not
// connected, the integration off, the demo cluster; nil when they do.
func (s *Session) AlertmanagerReady(contextID string) error {
	_, _, err := s.alertSnapshot(contextID)
	return err
}

// RefreshAlerts asks a connected cluster's Alertmanager again at once ("" the shown one), while the
// cluster is shown; it sends nothing itself.
func (s *Session) RefreshAlerts(contextID string) error {
	b, _, err := s.alertSnapshot(contextID)
	if err != nil {
		return err
	}
	if p := b.alerts.current(); p != nil {
		p.Refresh()
	}
	return nil
}

// alertFoldKeys bounds AlertBoardState's lists: a key per group or card.
const maxAlertFoldKeys = 500

// AlertBoardState is the alert board's folding (UIState.AlertBoard): the keys of the severity groups
// and alert cards folded ("critical", "warning/KubePodCrashLooping"), of the cards whose
// description is unfolded, and of the cards that show all their rows. It outlives a cluster: a key
// whose group or card is gone does nothing.
type AlertBoardState struct {
	Folded   []string `json:"folded,omitempty"`
	Expanded []string `json:"expanded,omitempty"`
	Rows     []string `json:"rows,omitempty"`
}

func (a AlertBoardState) clone() AlertBoardState {
	return AlertBoardState{Folded: slices.Clone(a.Folded), Expanded: slices.Clone(a.Expanded), Rows: slices.Clone(a.Rows)}
}

// IsZero reports whether nothing is folded or unfolded.
func (a AlertBoardState) IsZero() bool {
	return len(a.Folded) == 0 && len(a.Expanded) == 0 && len(a.Rows) == 0
}

// AlertFold is what alerts.fold changes: a group or card folded (the default), a card's description
// shown, or all of a card's rows shown.
type AlertFold int

// The folds of AlertFold.
const (
	FoldCard AlertFold = iota
	FoldDescription
	FoldRows
)

// SetFold turns a fold of a group or card on (true: folded; a description or all rows shown) or
// off, by its key.
func (a *AlertBoardState) SetFold(key string, on bool, fold AlertFold) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	list := &a.Folded
	switch fold {
	case FoldDescription:
		list = &a.Expanded
	case FoldRows:
		list = &a.Rows
	}
	*list = slices.DeleteFunc(*list, func(k string) bool { return k == key })
	if on {
		*list = append(*list, key)
		if len(*list) > maxAlertFoldKeys {
			*list = (*list)[len(*list)-maxAlertFoldKeys:]
		}
	}
}
