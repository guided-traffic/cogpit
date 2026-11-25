package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

// State is what the poller knows of a cluster's Alertmanager.
type State string

// The poller's states.
const (
	// StateChecking: nothing is known yet (no poll has ended since it started or the address
	// changed).
	StateChecking State = "checking"
	// StateFound: an Alertmanager found by its Service's name answers; Target says which.
	StateFound State = "found"
	// StateConfigured: the address the user set answers.
	StateConfigured State = "configured"
	// StateNotFound: no Service is named like an Alertmanager, or none of those asked answers as
	// one; the user can set its address.
	StateNotFound State = "notFound"
	// StateForbidden: the user may not use the service proxy (get services/proxy).
	StateForbidden State = "forbidden"
	// StateUnavailable: an Alertmanager was found or named but does not answer, or the cluster's
	// Services cannot be read; Error says why. The last answer is kept (Snapshot.Alerts).
	StateUnavailable State = "unavailable"
)

// Snapshot is what the poller knows: its state, the Alertmanager it reads and the alerts of its
// last answer. Alerts and their labels are never changed after the snapshot is made.
type Snapshot struct {
	State  State
	Target Target // the Alertmanager read; zero while none is
	// Alerts are the last answer's shown alerts (Classify), at most Options.MaxAlerts; kept while
	// a later poll fails. Total counts them before that bound, Truncated says it cut.
	Alerts    []Alert
	Total     int
	Truncated bool
	// Muted are the last answer's silenced alerts (whatever their severity: the meta alerts too), at
	// most Options.MaxAlerts; MutedTotal counts them before that bound. Neither the pill nor the
	// board shows them; the silences page tells each silence's (D-166).
	Muted      []Alert
	MutedTotal int
	// Silences are the Alertmanager's silences as its last answer to the silences request listed
	// them (SortSilences), asked while SetSilences is on; kept while it is off and while a later
	// request fails. SilencesAsOf is when they came, zero before; SilencesError says why the last
	// request failed ("" after an answer).
	Silences      []Silence
	SilencesAsOf  time.Time
	SilencesError string
	// AsOf is when the last answer came; zero before the first.
	AsOf time.Time
	// Error says why the poller is not reading (not found, forbidden, unavailable): the API
	// server's own message or the failure's class, never what the service answered.
	Error string
	// FailingSince is when the polls began to fail after an answer; zero while they answer.
	FailingSince time.Time
}

// Answered reports whether the snapshot holds an answer of an Alertmanager.
func (s Snapshot) Answered() bool { return !s.AsOf.IsZero() }

// Options tune a poller; zero fields take the defaults.
type Options struct {
	// Interval is the time between two polls that answered: DefaultInterval.
	Interval time.Duration
	// RetryMax caps the backoff after failures, and is how often detection looks again while no
	// Alertmanager is found: DefaultRetryMax.
	RetryMax time.Duration
	// KeepFor is how long the last answer is kept once the Alertmanager stops answering: after it,
	// the next failed poll drops the alerts, so that an old count does not stand as the cluster's
	// (DefaultKeepFor).
	KeepFor time.Duration
	// Timeout bounds one request: DefaultTimeout.
	Timeout time.Duration
	// MaxBytes bounds one answer: DefaultMaxBytes.
	MaxBytes int64
	// MaxAlerts bounds the alerts kept: DefaultMaxAlerts.
	MaxAlerts int
	// Logger receives the poller's failures; nil: slog.Default().
	Logger *slog.Logger
	// Now is the clock; nil: time.Now.
	Now func() time.Time
	// OnChange runs on the poller's goroutine after the snapshot changed in what it shows (state,
	// target, error, the alerts); not after an answer that only renews AsOf. It must not block.
	OnChange func()
}

// The defaults (D-77; docs/developer/testing.md's budget P-AM).
const (
	// DefaultInterval: one request every 30 seconds while the cluster is shown, as karma's and
	// Alertmanager's own UI refresh, and the group_wait of Alertmanager's default route.
	DefaultInterval = 30 * time.Second
	// DefaultRetryMax: failures back off to one request in 5 minutes.
	DefaultRetryMax = 5 * time.Minute
	// DefaultKeepFor: an answer stands for 5 minutes of failures (a restarting Alertmanager, a
	// moment without endpoints) rather than turning the pill grey at the first one.
	DefaultKeepFor = 5 * time.Minute
	// DefaultTimeout bounds one request.
	DefaultTimeout = 15 * time.Second
	// DefaultMaxBytes: 16 MiB, some 10,000 alerts.
	DefaultMaxBytes = 16 << 20
	// DefaultMaxAlerts: the board's and the lists' 10,000 rows (AGENTS.md, performance).
	DefaultMaxAlerts = 10000
)

// Poller reads one cluster's Alertmanager while it is active (SetActive): the engine makes it
// active while the integration is on and the cluster is shown and connected. Inactive, it sends
// nothing and keeps its last snapshot; active again, it polls at once when the last poll is older
// than the interval. Each poll runs on the poller's own goroutine, which recovers from a panic at
// its boundary (a poll that panicked is a failure, AGENTS.md's crash rule). Safe for concurrent
// use.
type Poller struct {
	px       proxy
	services ServiceSource
	opts     Options

	mu       sync.Mutex
	addr     *Address
	active   bool
	silences bool // the silences are asked for with each poll (SetSilences)
	snap     Snapshot
	sig      uint64    // what the snapshot shows (signature), for OnChange
	target   *Target   // the Alertmanager that answered last; nil: detect (or ask the address) again
	next     time.Time // when the next poll is due; zero: at once
	failures int       // polls that failed in a row
	gen      uint64    // counts address changes: a poll under an older address is not applied
	asked    uint64    // counts SetSilences(true) and Refresh: one during a poll leaves the next due at once
	started  bool      // the goroutine runs
	closed   bool      // Close ran
	wake     chan struct{}
	quit     chan struct{}
	done     chan struct{}
	cancel   func() // the running poll's
	runCtx   context.Context
	stopRun  context.CancelFunc
}

// NewPoller returns the poller of a cluster: services is the cache's list of Services, kube
// reaches the service proxy through the connection's counting transport. It sends nothing until
// SetActive(true).
func NewPoller(kube kubernetes.Interface, services ServiceSource, opts Options) *Poller {
	return newPoller(kubeProxy{kube}, services, opts)
}

func newPoller(px proxy, services ServiceSource, opts Options) *Poller {
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.RetryMax <= 0 {
		opts.RetryMax = DefaultRetryMax
	}
	if opts.RetryMax < opts.Interval {
		opts.RetryMax = opts.Interval
	}
	if opts.KeepFor <= 0 {
		opts.KeepFor = DefaultKeepFor
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.MaxAlerts <= 0 {
		opts.MaxAlerts = DefaultMaxAlerts
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Poller{px: px, services: services, opts: opts, snap: Snapshot{State: StateChecking},
		wake: make(chan struct{}, 1), quit: make(chan struct{}), done: make(chan struct{}), runCtx: ctx, stopRun: stop}
}

// SetAddress sets (or, with nil, clears) the Alertmanager to read instead of detecting one. What was
// known goes: the state is checking until the next poll, which runs at once while active.
func (p *Poller) SetAddress(a *Address) {
	p.mu.Lock()
	if a != nil && !a.Valid() {
		a = nil
	}
	if a != nil {
		c := *a
		a = &c
	}
	if equalAddress(p.addr, a) {
		p.mu.Unlock()
		return
	}
	p.addr = a
	p.gen++
	p.target, p.next, p.failures = nil, time.Time{}, 0
	p.snap = Snapshot{State: StateChecking}
	if p.cancel != nil {
		p.cancel() // a poll of the old address ends
	}
	changed := p.noteLocked()
	p.mu.Unlock()
	p.signal()
	if changed {
		p.changed()
	}
}

func equalAddress(a, b *Address) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Address returns the address set, or nil while the poller detects.
func (p *Poller) Address() *Address {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.addr == nil {
		return nil
	}
	c := *p.addr
	return &c
}

// SetActive starts (true) or pauses (false) the polls. The first activation starts the poller's
// goroutine.
func (p *Poller) SetActive(on bool) {
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
		go p.run()
	}
	p.signal()
}

// SetSilences asks (true) or stops asking (false) for the silences with each poll: the silences page
// is shown. Turned on, the next poll is due at once.
func (p *Poller) SetSilences(on bool) {
	p.mu.Lock()
	if p.silences == on {
		p.mu.Unlock()
		return
	}
	p.silences = on
	if on {
		p.next = time.Time{}
		p.asked++
	}
	p.mu.Unlock()
	p.signal()
}

// Refresh makes the next poll due at once (while active).
func (p *Poller) Refresh() {
	p.mu.Lock()
	p.next = time.Time{}
	p.asked++
	p.mu.Unlock()
	p.signal()
}

// Snapshot returns what the poller knows.
func (p *Poller) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap
}

// Stop stops the poller without waiting: the poll under way is cancelled and its result dropped;
// an OnChange already under way may still run once. The engine stops a poller while it holds its
// own locks, which a poll's OnChange may wait for.
func (p *Poller) Stop() {
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

// Close stops the poller and waits for its goroutine, at most for the poll under way to end (its
// request is cancelled).
func (p *Poller) Close() {
	p.Stop()
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if started {
		<-p.done
	}
}

func (p *Poller) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Poller) changed() {
	if p.opts.OnChange != nil {
		p.opts.OnChange()
	}
}

// run is the poller's goroutine: a poll whenever one is due while active.
func (p *Poller) run() {
	defer close(p.done)
	defer func() { // the goroutine's boundary: a poll recovers on its own (poll), this is the loop's
		if r := recover(); r != nil {
			p.opts.Logger.Error("alerting: the poller's loop panicked; it stops", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	for {
		select {
		case <-p.quit:
			return
		default:
		}
		p.mu.Lock()
		active, next := p.active, p.next
		p.mu.Unlock()
		var timer <-chan time.Time
		if active {
			wait := next.Sub(p.opts.Now())
			if wait <= 0 {
				p.poll()
				continue
			}
			t := time.NewTimer(wait)
			timer = t.C
			select {
			case <-p.quit:
				t.Stop()
				return
			case <-p.wake:
				t.Stop()
			case <-timer:
			}
			continue
		}
		select {
		case <-p.quit:
			return
		case <-p.wake:
		}
	}
}

// poll asks the Alertmanager once (detecting it first when none answered yet) and schedules the
// next poll. A panic is recovered here, at the goroutine's boundary, and counts as a failure.
func (p *Poller) poll() {
	p.mu.Lock()
	gen, asked, addr, target, silences := p.gen, p.asked, p.addr, p.target, p.silences
	ctx, cancel := context.WithCancel(p.runCtx)
	p.cancel = cancel
	p.mu.Unlock()
	defer cancel()

	var res result
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.opts.Logger.Error("alerting: a poll of Alertmanager panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				res = result{state: StateUnavailable, err: "internal error"}
			}
		}()
		res = p.ask(ctx, addr, target)
		if res.target != nil && silences {
			res.silencesAsked = true
			res.silences, res.silencesErr = p.fetchSilences(ctx, *res.target)
		}
	}()
	if ctx.Err() != nil && p.runCtx.Err() != nil {
		return // closed
	}
	p.apply(gen, asked, res)
}

// result is one poll's outcome.
type result struct {
	state     State
	target    *Target // the Alertmanager that answered; nil when none did
	alerts    []Alert
	total     int
	truncated bool
	muted     []Alert
	mutedN    int
	err       string
	// silencesAsked: the silences were asked for after the alerts answered; silences is their
	// answer, silencesErr why there is none.
	silencesAsked bool
	silences      []Silence
	silencesErr   string
	lost          bool // the target answered no more as an Alertmanager (or went away): detect again
	notFound      bool // detection found none: look again after RetryMax
}

// ask runs one poll: the target that answered last, else the address, else the best candidates
// among the cached Services (at most MaxProbes). Every request is the alerts request itself.
func (p *Poller) ask(ctx context.Context, addr *Address, target *Target) result {
	var cands [][]Target // each candidate's spellings of its port (Candidate.Ports), the number first
	configured := addr != nil
	switch {
	case target != nil:
		cands = [][]Target{{*target}}
	case addr != nil:
		cands = [][]Target{{TargetOf(*addr)}}
	default:
		if p.services == nil {
			return result{state: StateNotFound, notFound: true, err: "no Alertmanager found"}
		}
		svcs, err := p.services.Services(ctx)
		if err != nil {
			return result{state: StateUnavailable, err: "the cluster's services cannot be read, so Alertmanager cannot be looked for: " + err.Error()}
		}
		found := Candidates(svcs)
		if len(found) == 0 {
			return result{state: StateNotFound, notFound: true, err: "no Alertmanager found (no service named like one in the cluster)"}
		}
		if len(found) > MaxProbes {
			found = found[:MaxProbes]
		}
		for _, c := range found {
			var spellings []Target
			for _, port := range c.Ports {
				t := c.Target
				t.Port = port
				spellings = append(spellings, t)
			}
			cands = append(cands, spellings)
		}
	}
	found := StateFound
	if configured {
		found = StateConfigured
	}
	var lastErr error
	var lastTarget Target
	var refused []string // the targets the service proxy refused: another spelling or candidate may be granted
	var refusedErr error
	for _, spellings := range cands {
		for _, t := range spellings {
			d, err := p.fetch(ctx, t)
			if err == nil {
				t := t
				return result{state: found, target: &t, alerts: d.alerts, total: d.total, truncated: d.truncated,
					muted: d.muted, mutedN: d.mutedTotal}
			}
			if ctx.Err() != nil {
				return result{state: StateUnavailable, err: "the request was cancelled"}
			}
			if forbidden(err) {
				refused, refusedErr = append(refused, t.Where()), err
				continue // another spelling of the port may be granted (an RBAC rule by resourceNames)
			}
			if target != nil && (errors.Is(err, errNotAlertmanager) || apierrors.IsNotFound(err) && !fromService(err)) {
				return result{state: StateUnavailable, lost: true, err: t.Where() + " no longer answers as an Alertmanager: " + errorText(err)}
			}
			if configured || target != nil || !errors.Is(err, errNotAlertmanager) && !apierrors.IsNotFound(err) {
				lastErr, lastTarget = err, t
			}
			break // it answered, or did not, under this spelling: the other asks the same server
		}
	}
	if len(refused) > 0 {
		return result{state: StateForbidden, err: "the current user may not use the service proxy of " + strings.Join(refused, " nor ") +
			" (get services/proxy), so Alertmanager cannot be read: " + errorText(refusedErr)}
	}
	if lastErr != nil {
		what := "looks like an Alertmanager but does not answer"
		if configured {
			what = "does not answer as an Alertmanager"
		}
		return result{state: StateUnavailable, err: fmt.Sprintf("%s %s: %s", lastTarget.Where(), what, errorText(lastErr))}
	}
	return result{state: StateNotFound, notFound: true, err: "no Alertmanager found (the services named like one do not answer as one)"}
}

// fetch sends the alerts request to t and reads the answer.
func (p *Poller) fetch(ctx context.Context, t Target) (decoded, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	body, err := p.px.get(ctx, t, alertsPath, alertsParams, p.opts.MaxBytes)
	if err != nil {
		return decoded{}, err
	}
	return decodeAlerts(body, p.opts.MaxAlerts)
}

// fetchSilences sends the silences request to t and reads the answer; a failure is its class (or
// the API server's own message), never what the service answered.
func (p *Poller) fetchSilences(ctx context.Context, t Target) ([]Silence, string) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	body, err := p.px.get(ctx, t, silencesPath, nil, p.opts.MaxBytes)
	if err == nil {
		var ss []Silence
		if ss, err = decodeSilences(body); err == nil {
			return ss, ""
		}
	}
	return nil, fmt.Sprintf("the silences of %s cannot be read: %s", t.Where(), errorText(err))
}

// apply makes a poll's result the snapshot and schedules the next poll: the interval after an
// answer, a backoff doubling from it after failures up to RetryMax, RetryMax while none is found;
// at once when the silences were asked for or a refresh came while it ran (asked, the count when
// it started), which the poll did not answer.
func (p *Poller) apply(gen, asked uint64, res result) {
	now := p.opts.Now()
	p.mu.Lock()
	if p.closed || gen != p.gen {
		p.mu.Unlock()
		return // the address changed meanwhile: the next poll asks the new one
	}
	s := p.snap
	switch {
	case res.target != nil: // an answer
		p.target, p.failures = res.target, 0
		s = Snapshot{State: res.state, Target: *res.target, Alerts: res.alerts, Total: res.total, Truncated: res.truncated,
			Muted: res.muted, MutedTotal: res.mutedN, AsOf: now,
			Silences: s.Silences, SilencesAsOf: s.SilencesAsOf, SilencesError: s.SilencesError}
		if s.Target != p.snap.Target { // another Alertmanager's silences are not this one's
			s.Silences, s.SilencesAsOf, s.SilencesError = nil, time.Time{}, ""
		}
		switch {
		case res.silencesAsked && res.silencesErr == "":
			s.Silences, s.SilencesAsOf, s.SilencesError = res.silences, now, ""
		case res.silencesAsked:
			s.SilencesError = res.silencesErr
		}
		p.next = now.Add(p.opts.Interval)
	default:
		p.failures++
		if res.lost {
			p.target = nil
		}
		s.State, s.Error = res.state, res.err
		if res.state != StateUnavailable || !s.Answered() {
			s.Target = Target{}
		}
		if s.Answered() && s.FailingSince.IsZero() {
			s.FailingSince = now
		}
		if res.state != StateUnavailable || s.Answered() && now.Sub(s.FailingSince) >= p.opts.KeepFor {
			// Not found or forbidden, or failing for longer than KeepFor: what was answered
			// before no longer stands.
			s.Alerts, s.Total, s.Truncated, s.AsOf, s.FailingSince = nil, 0, false, time.Time{}, time.Time{}
			s.Muted, s.MutedTotal, s.Silences, s.SilencesAsOf, s.SilencesError = nil, 0, nil, time.Time{}, ""
			if res.state == StateUnavailable {
				s.Target = Target{}
			}
		}
		p.next = now.Add(p.backoffLocked(res.notFound))
	}
	if p.asked != asked {
		p.next = time.Time{}
	}
	p.snap = s
	changed := p.noteLocked()
	p.mu.Unlock()
	if !res.notFound && res.target == nil {
		p.opts.Logger.Debug("alerting: Alertmanager does not answer", "state", res.state, "error", res.err)
	}
	if changed {
		p.changed()
	}
}

// backoffLocked is the wait before the next poll after p.failures failures.
func (p *Poller) backoffLocked(notFound bool) time.Duration {
	if notFound {
		return p.opts.RetryMax
	}
	d := p.opts.Interval
	for i := 1; i < p.failures && d < p.opts.RetryMax; i++ {
		d *= 2
	}
	return min(d, p.opts.RetryMax)
}

// noteLocked updates the snapshot's signature and reports whether what it shows changed.
func (p *Poller) noteLocked() bool {
	sig := signature(p.snap)
	if sig == p.sig {
		return false
	}
	p.sig = sig
	return true
}

// signature hashes what a snapshot shows, AsOf left out.
func signature(s Snapshot) uint64 {
	h := fnv.New64a()
	w := func(parts ...string) {
		for _, x := range parts {
			_, _ = h.Write([]byte(x))
			_, _ = h.Write([]byte{0})
		}
	}
	w(string(s.State), s.Target.Where(), s.Error, s.FailingSince.String(), fmt.Sprint(s.Total, s.Truncated, s.Answered()))
	for _, a := range s.Alerts {
		w(a.Fingerprint, string(a.Severity), a.Summary, a.Description, a.StartsAt.String())
	}
	w(fmt.Sprint(s.MutedTotal), s.SilencesError, fmt.Sprint(s.SilencesAsOf.IsZero()))
	for _, a := range s.Muted {
		w(a.Fingerprint, string(a.Severity), a.Summary, a.StartsAt.String())
		w(a.SilencedBy...)
	}
	for _, x := range s.Silences {
		w(x.ID, string(x.State), x.StartsAt.String(), x.EndsAt.String(), x.UpdatedAt.String(), x.CreatedBy, x.Comment)
		for _, m := range x.Matchers {
			w(m.String())
		}
	}
	return h.Sum64()
}

// SortAlerts orders alerts as the board lists them: by severity, then alertname, then the newest
// first, then by fingerprint.
func SortAlerts(alerts []Alert) {
	slices.SortStableFunc(alerts, func(a, b Alert) int {
		if a.Severity != b.Severity {
			return a.Severity.Rank() - b.Severity.Rank()
		}
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		if !a.StartsAt.Equal(b.StartsAt) {
			return b.StartsAt.Compare(a.StartsAt)
		}
		if a.Fingerprint < b.Fingerprint {
			return -1
		}
		if a.Fingerprint > b.Fingerprint {
			return 1
		}
		return 0
	})
}

// ErrNoAlertmanager refuses a silence while no Alertmanager answers: Cogpit writes only to the one
// whose alerts it shows.
var ErrNoAlertmanager = errors.New("no Alertmanager answers: Cogpit creates and expires silences only in the Alertmanager whose alerts it shows")

// answering returns the Alertmanager that answered last, which the writes go to.
func (p *Poller) answering() (Target, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.target == nil {
		return Target{}, ErrNoAlertmanager
	}
	return *p.target, nil
}

// CreateSilence creates a silence in the Alertmanager that answered last (POST /api/v2/silences
// through the service proxy) and returns its id and the Alertmanager. The next poll runs at once,
// so that the alerts it mutes leave the board. n is validated first (NewSilence.Validate).
func (p *Poller) CreateSilence(ctx context.Context, n NewSilence) (string, Target, error) {
	if err := n.Validate(p.opts.Now()); err != nil {
		return "", Target{}, err
	}
	t, err := p.answering()
	if err != nil {
		return "", Target{}, err
	}
	body, err := json.Marshal(n)
	if err != nil {
		return "", t, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	answer, err := p.px.send(ctx, t, http.MethodPost, silencesPath, body, 64<<10)
	if err != nil {
		return "", t, err
	}
	var a silenceAnswer
	if json.Unmarshal(answer, &a) != nil || !ValidSilenceID(a.ID) {
		return "", t, fmt.Errorf("POST %s to Alertmanager %s: the answer names no silence", silencesPath, t.Where())
	}
	p.Refresh()
	return a.ID, t, nil
}

// ExpireSilence expires a silence in the Alertmanager that answered last (DELETE
// /api/v2/silence/<id> through the service proxy) and returns the Alertmanager: it ends now and
// stays listed as expired for Alertmanager's retention. The next poll runs at once.
func (p *Poller) ExpireSilence(ctx context.Context, id string) (Target, error) {
	if !ValidSilenceID(id) {
		return Target{}, fmt.Errorf("silence id %.70q: not an id Alertmanager gives", id)
	}
	t, err := p.answering()
	if err != nil {
		return Target{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if _, err := p.px.send(ctx, t, http.MethodDelete, silencePath+id, nil, 64<<10); err != nil {
		return t, err
	}
	p.Refresh()
	return t, nil
}

// ReadSilences asks the Alertmanager that answered last for its silences now, outside the polls (an
// agent's alerts.silences while the silences page is not shown), and keeps the answer in the
// snapshot as a poll's would be kept.
func (p *Poller) ReadSilences(ctx context.Context) error {
	t, err := p.answering()
	if err != nil {
		return err
	}
	ss, why := p.fetchSilences(ctx, t)
	now := p.opts.Now()
	p.mu.Lock()
	if p.closed || p.target == nil || *p.target != t {
		p.mu.Unlock()
		return errors.New("the Alertmanager changed while its silences were read")
	}
	if why == "" {
		p.snap.Silences, p.snap.SilencesAsOf, p.snap.SilencesError = ss, now, ""
	} else {
		p.snap.SilencesError = why
	}
	changed := p.noteLocked()
	p.mu.Unlock()
	if changed {
		p.changed()
	}
	if why != "" {
		return errors.New(why)
	}
	return nil
}
