package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The alert board's history (the owner, 2026-10-27: how often an alert fired in the past, "um das
// besser einschätzen zu können"): per card, how often its alertname started firing in each of the
// last 24 hours, read from the cluster's Prometheus (its ALERTS series, metrics/promalerts.go).
//
// Two requests through the service proxy, every 5 minutes, while the alert board is the shown view
// of the shown, connected cluster and the Alertmanager and the Prometheus integration are on; none
// otherwise. The Prometheus is the one the metrics hub detected (nothing here detects on its own).
// The last answer is kept while the board is closed and shown again when it opens; an answer older
// than the interval is asked for again at once. The board reads a snapshot (alertHistory): it never
// waits for Prometheus.

func init() {
	integrationFollowers = append(integrationFollowers, followAlertHistory)
}

// The history's defaults.
const (
	// DefaultAlertHistoryInterval: the hours change slowly; one pair of requests in 5 minutes, as
	// karma's history refresh.
	DefaultAlertHistoryInterval = 5 * time.Minute
	// alertHistoryFirstRetry is the wait after a first failure, doubled for each further one up to
	// the interval.
	alertHistoryFirstRetry = 30 * time.Second
	// The wording of the board's note.
	noteNoPrometheus  = "no Prometheus found"
	notePrometheusOff = "the Prometheus integration is off"
)

// AlertHistoryOptions tune the history polls; zero fields take the defaults.
type AlertHistoryOptions struct {
	// Interval is the time between two polls that answered, and the longest wait after failures:
	// DefaultAlertHistoryInterval.
	Interval time.Duration
	// Now is the clock; nil: time.Now.
	Now func() time.Time
	// After returns a channel that receives when d has passed; nil: time.After.
	After func(d time.Duration) <-chan time.Time
}

// cardKey is a card's key on the board: its shown severity and its alertname.
type cardKey struct{ severity, alertname string }

// historyData is an answer of Prometheus joined to the board's cards.
type historyData struct {
	hours  map[cardKey][HistoryHours]int
	firing map[cardKey]bool // the cards whose alertname this Prometheus evaluates now
	asOf   time.Time
}

// joinHistory maps the rules' severity labels to the cards' severities (alerting.Classify, as the
// board does); several labels that show as one severity are summed, and the meta alerts, which have
// no card, are left out.
func joinHistory(o metrics.AlertOnsets) *historyData {
	d := &historyData{hours: map[cardKey][HistoryHours]int{}, firing: map[cardKey]bool{}, asOf: o.AsOf}
	for k, hours := range o.Hours {
		sev, keep := alerting.Classify(k.Alertname, k.Severity)
		if !keep {
			continue
		}
		ck := cardKey{string(sev), k.Alertname}
		sum := d.hours[ck]
		for i, n := range hours {
			sum[i] += n
		}
		d.hours[ck] = sum
	}
	for k := range o.Firing {
		if sev, keep := alerting.Classify(k.Alertname, k.Severity); keep {
			d.firing[cardKey{string(sev), k.Alertname}] = true
		}
	}
	return d
}

// historySnapshot is what a poller knows. It is never changed after it is made.
type historySnapshot struct {
	// data is the last answer; nil before one, and after a poll that found no Prometheus.
	data *historyData
	// noProm says the last poll found no Prometheus.
	noProm bool
	// err says why the last poll failed: the class of the failure, never what the service answered
	// (metrics' messages). The answer before it is kept in data.
	err string
}

// same reports whether two snapshots show the same on the board: AsOf alone does not count.
func (a historySnapshot) same(b historySnapshot) bool {
	if a.noProm != b.noProm || a.err != b.err || (a.data == nil) != (b.data == nil) {
		return false
	}
	return a.data == nil || maps.Equal(a.data.hours, b.data.hours) && maps.Equal(a.data.firing, b.data.firing)
}

// errNoPrometheus is the poll's answer when the hub found no Prometheus.
var errNoPrometheus = errors.New(noteNoPrometheus)

// historyPoller reads one cluster's Prometheus while it is active (setActive). Inactive it sends
// nothing and keeps its snapshot; active again it polls at once when the next poll is due. Each
// poll runs on the poller's goroutine, which recovers from a panic at its boundary (a poll that
// panicked is a failure). Safe for concurrent use.
type historyPoller struct {
	ask      func(ctx context.Context, now time.Time) (metrics.AlertOnsets, error)
	changed  func()
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time

	mu       sync.Mutex
	active   bool
	started  bool
	closed   bool
	snap     historySnapshot
	next     time.Time // when the next poll is due; zero: at once
	failures int       // polls that failed in a row
	wake     chan struct{}
	quit     chan struct{}
	ctx      context.Context
	stopRun  context.CancelFunc
}

func newHistoryPoller(opts AlertHistoryOptions, log *slog.Logger, ask func(context.Context, time.Time) (metrics.AlertOnsets, error), changed func()) *historyPoller {
	if opts.Interval <= 0 {
		opts.Interval = DefaultAlertHistoryInterval
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.After == nil {
		opts.After = time.After
	}
	ctx, stop := context.WithCancel(context.Background())
	return &historyPoller{ask: ask, changed: changed, log: log, interval: opts.Interval, now: opts.Now, after: opts.After,
		wake: make(chan struct{}, 1), quit: make(chan struct{}), ctx: ctx, stopRun: stop}
}

// setActive starts (true) or pauses (false) the polls; the first activation starts the goroutine.
func (p *historyPoller) setActive(on bool) {
	p.mu.Lock()
	if p.closed || p.active == on {
		p.mu.Unlock()
		return
	}
	p.active = on
	start := on && !p.started
	if start {
		p.started = true
	}
	p.mu.Unlock()
	if start {
		go p.run() // it reads the state itself
		return
	}
	p.signal()
}

// snapshot returns what the poller knows.
func (p *historyPoller) snapshot() historySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap
}

// stop ends the poller without waiting: a poll under way is cancelled and its answer dropped.
func (p *historyPoller) stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	p.stopRun()
	close(p.quit)
}

func (p *historyPoller) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// run is the poller's goroutine: a poll whenever one is due while active.
func (p *historyPoller) run() {
	defer func() { // the goroutine's boundary: a poll recovers on its own (poll), this is the loop's
		if r := recover(); r != nil {
			p.log.Error("alert history: the poller's loop panicked; it stops", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	var due <-chan time.Time // the wait for the poll due at dueFor; kept across wakes that change nothing
	var dueFor time.Time
	for {
		p.mu.Lock()
		active, next := p.active, p.next
		p.mu.Unlock()
		if !active {
			due = nil
		} else {
			wait := next.Sub(p.now())
			if wait <= 0 {
				p.poll()
				due = nil
				select {
				case <-p.quit:
					return
				default:
				}
				continue
			}
			if due == nil || !next.Equal(dueFor) {
				due, dueFor = p.after(wait), next
			}
		}
		select {
		case <-p.quit:
			return
		case <-p.wake:
		case <-due:
			due = nil
		}
	}
}

// poll asks Prometheus once and schedules the next poll. A panic is recovered here and counts as a
// failure.
func (p *historyPoller) poll() {
	var (
		ans metrics.AlertOnsets
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("alert history: a poll of Prometheus panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				err = errors.New("internal error")
			}
		}()
		ans, err = p.ask(p.ctx, p.now())
	}()
	if p.ctx.Err() != nil {
		return // stopped
	}
	p.mu.Lock()
	old := p.snap
	snap := old
	switch {
	case err == nil:
		snap = historySnapshot{data: joinHistory(ans)}
		p.failures, p.next = 0, p.now().Add(p.interval)
	case errors.Is(err, errNoPrometheus):
		snap = historySnapshot{noProm: true}
		p.failures++
		p.next = p.now().Add(p.backoffLocked())
	default:
		snap.noProm, snap.err = false, err.Error()
		p.failures++
		p.next = p.now().Add(p.backoffLocked())
	}
	p.snap = snap
	changed := !old.same(snap)
	p.mu.Unlock()
	if changed && p.changed != nil {
		p.changed()
	}
}

// backoffLocked is the wait after the failure just counted: 30 seconds, doubled for each failure in
// a row, up to the interval.
func (p *historyPoller) backoffLocked() time.Duration {
	d := alertHistoryFirstRetry
	for i := 1; i < p.failures && d < p.interval; i++ {
		d *= 2
	}
	return min(d, p.interval)
}

// historySource is a backend's history poller, while the board needs one.
type historySource struct {
	mu sync.Mutex
	p  *historyPoller
}

// current returns the poller, or nil.
func (h *historySource) current() *historyPoller {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.p
}

// ensure returns the poller, made with mk when none runs; mk runs without the source's lock.
func (h *historySource) ensure(mk func() *historyPoller) *historyPoller {
	if p := h.current(); p != nil {
		return p
	}
	p := mk()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.p != nil { // made meanwhile: the follower runs under the state's lock, so not expected
		p.stop()
		return h.p
	}
	h.p = p
	return p
}

// stop ends the poller without waiting for it and forgets its snapshot; it reports whether one ran.
func (h *historySource) stop() bool {
	h.mu.Lock()
	p := h.p
	h.p = nil
	h.mu.Unlock()
	if p == nil {
		return false
	}
	p.stop()
	return true
}

// wantsHistory says why a backend has no Prometheus to ask for the history, "" when it may have:
// the demo cluster and a hub over a source of its own (the metrics of the demo cluster) detect none.
func wantsHistory(b *Backend) string {
	if b.Demo || b.Metrics == nil || b.PrometheusDetector == nil {
		return noteNoPrometheus
	}
	return ""
}

// followAlertHistory is the integration's follower (integrations.go): the poller exists while both
// integrations are on, and is active while the alert board is the shown view of the shown,
// connected cluster. Off, it is stopped and its answer forgotten. It runs with the state's lock
// held: nothing here waits for Prometheus.
func followAlertHistory(st *state, b *Backend, now UIState) {
	s := st.s
	if wantsHistory(b) != "" || !now.IntegrationOn(b.Context, IntegrationAlertmanager) || !now.IntegrationOn(b.Context, IntegrationPrometheus) {
		if b.history.stop() {
			s.alertHistoryNoticed(b.Context)
		}
		return
	}
	p := b.history.ensure(func() *historyPoller { return s.newHistoryPoller(b) })
	p.setActive(now.View == AlertBoard && now.Context == b.Context && now.connectionState(b.Context) == Connected)
}

// newHistoryPoller makes a backend's poller, asking the Prometheus its metrics hub detected.
func (s *Session) newHistoryPoller(b *Backend) *historyPoller {
	var p *historyPoller
	ask := func(ctx context.Context, now time.Time) (metrics.AlertOnsets, error) {
		prom, state := b.Metrics.Prometheus(ctx)
		switch {
		case prom != nil:
			return prom.AlertOnsets(ctx, now)
		case state.Status == metrics.StatusNotServed:
			return metrics.AlertOnsets{}, errNoPrometheus
		}
		return metrics.AlertOnsets{}, errors.New(state.Message)
	}
	p = newHistoryPoller(s.opts.AlertHistory, s.log, ask, func() {
		if b.history.current() == p { // a stopped poller's late change is dropped
			s.alertHistoryNoticed(b.Context)
		}
	})
	return p
}

// alertHistoryNoticed tells the board, on a goroutine of its own (the follower holds the state's
// lock), that a cluster's history changed. It is not a change of the UI state.
func (s *Session) alertHistoryNoticed(contextID string) {
	s.goSafe("the alert history's notice", func() {
		s.emit(Event{Type: EventAlerts, View: contextID})
	})
}

// boardHistory is the board's history of a cluster: a snapshot of what the poller knows. The board
// model reads it without a lock and without a request.
type boardHistory struct {
	why  string // why there is none: set, no card has a history
	snap historySnapshot
}

// alertHistory returns the board's history of a backend: what its poller knows, or why Cogpit has
// none to ask (no Prometheus, the integration off). It sends no request.
func (s *Session) alertHistory(b *Backend) boardHistory {
	u := s.UIState()
	switch {
	case wantsHistory(b) != "":
		return boardHistory{why: noteNoPrometheus}
	case !u.IntegrationOn(b.Context, IntegrationPrometheus):
		return boardHistory{why: notePrometheusOff}
	}
	p := b.history.current()
	if p == nil {
		return boardHistory{} // not asked yet
	}
	snap := p.snapshot()
	if snap.noProm {
		return boardHistory{why: noteNoPrometheus}
	}
	return boardHistory{snap: snap}
}

// card returns a card's history, nil while there is none to show for the cluster at all. A card
// whose alertname this Prometheus does not evaluate (its rule runs elsewhere) has no count, not
// zeros.
func (h boardHistory) card(severity, alertname string) *AlertHistory {
	switch {
	case h.why != "":
		return nil
	case h.snap.data == nil && h.snap.err != "":
		return &AlertHistory{State: HistoryUnavailable, Detail: h.snap.err}
	case h.snap.data == nil:
		return &AlertHistory{State: HistoryChecking}
	}
	d := h.snap.data
	key := cardKey{severity, alertname}
	if !d.firing[key] {
		return &AlertHistory{State: HistoryNoRule, AsOf: d.asOf, Detail: "no rule of this name is evaluated by the Prometheus Cogpit reads"}
	}
	hours := d.hours[key]
	out := &AlertHistory{State: HistoryOK, Hours: hours[:], AsOf: d.asOf}
	for _, n := range hours {
		out.Total += n
	}
	return out
}

// note says, for the board's header, why no card has a history: "" while they have one or are
// being asked for.
func (h boardHistory) note() string { return h.why }
