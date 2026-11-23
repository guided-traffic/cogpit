package engine

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The Image Security page's findings over time: Trivy Operator's reports keep no history, so the
// curve comes from the cluster's Prometheus where it scrapes the operator's metric
// trivy_image_vulnerabilities (metrics/promtrivy.go): the findings (a vulnerability of an image,
// each image once however many workloads and replicas run it) by severity, at a point a day (6
// hours for a week), in the namespaces of the page's namespace filter, over the range the user
// chose (UIState.ImageSecurity, trivy.setTrendRange).
//
// One range query through the service proxy (a second, instant one when it answers no series)
// when the page is shown, again every 15 minutes while it stays shown, and at once when the range
// or the namespaces change; while the page is the shown view of the shown, connected cluster and
// the Trivy and the Prometheus integration are on; none otherwise. The Prometheus is the one the
// metrics hub detected (nothing here detects on its own). The page reads a snapshot
// (imageSecurityTrend): it never waits for Prometheus. A new answer wakes the page's worker, which
// rebuilds and announces the page (EventTable). The demo cluster has no Prometheus: its curve is
// drawn (demoImageSecurityTrend).

func init() {
	integrationFollowers = append(integrationFollowers, followImageSecurityTrend)
}

// The curve's ranges and cadence.
const (
	// DefaultImageSecurityTrendDays is the range of a page whose user chose none.
	DefaultImageSecurityTrendDays = 30
	// imageSecTrendInterval: the reports change when an image is rescanned, about daily; one query
	// in 15 minutes.
	imageSecTrendInterval = 15 * time.Minute
	// imageSecTrendFirstRetry is the wait after a first failure, doubled for each further one up to
	// the interval.
	imageSecTrendFirstRetry = 30 * time.Second
	// The wording of the curve's note.
	noteTrendAsking     = "asking Prometheus"
	noteTrendNotScraped = "Prometheus does not scrape Trivy Operator's metrics (trivy_image_vulnerabilities)"
	noteTrendNoImages   = "Trivy Operator's metrics in Prometheus name no image in these namespaces"
	// demoTrendSource names the demo cluster's drawn curve.
	demoTrendSource = "Demo data"
)

// ImageSecurityTrendDays are the ranges the curve offers, in days.
var ImageSecurityTrendDays = []int{7, 30, 90}

// ImageSecurityState is the Image Security page's choices (UIState.ImageSecurity).
type ImageSecurityState struct {
	// TrendDays is the findings curve's range in days: 7, 30 or 90; 0 is the default, 30.
	TrendDays int `json:"trendDays,omitempty"`
}

// Days is the curve's range in days.
func (s ImageSecurityState) Days() int {
	if slices.Contains(ImageSecurityTrendDays, s.TrendDays) {
		return s.TrendDays
	}
	return DefaultImageSecurityTrendDays
}

// ImageSecurityTrend is the Image Security page's curve of the findings over time.
type ImageSecurityTrend struct {
	// Days is the range it covers (UIState.ImageSecurity).
	Days int `json:"days"`
	// Points are the findings by severity, oldest first, a day apart (6 hours for 7 days); a time
	// the Prometheus has nothing for has no point.
	Points []ImageSecurityTrendPoint `json:"points,omitempty"`
	// Source is where the points come from: "Prometheus (monitoring/prometheus-k8s)", "Demo data".
	Source string `json:"source,omitempty"`
	// Note says why there are no points (Prometheus is being asked, it does not scrape Trivy
	// Operator, it names no image in the namespaces) or that the last request failed (the points
	// are then the answer before it).
	Note string `json:"note,omitempty"`
	// AsOf is the last point's time.
	AsOf time.Time `json:"asOf,omitzero"`
}

// ImageSecurityTrendPoint is the findings at one time: vulnerabilities of an image, each image
// counted once, by severity.
type ImageSecurityTrendPoint struct {
	At       time.Time `json:"at"`
	Critical int       `json:"critical"`
	High     int       `json:"high"`
	Medium   int       `json:"medium"`
	Low      int       `json:"low"`
	Unknown  int       `json:"unknown"`
}

// trendKey is what a curve is asked for: the range and the namespaces, sorted and joined by
// commas, which a namespace's name cannot hold ("" for every namespace).
type trendKey struct {
	days       int
	namespaces string
}

func (k trendKey) namespaceList() []string {
	if k.namespaces == "" {
		return nil
	}
	return strings.Split(k.namespaces, ",")
}

// trendKeyOf is the curve the page of b's cluster shows in the UI state now: its range and the
// namespace filter while the window shows the cluster, every namespace otherwise.
func trendKeyOf(b *Backend, now UIState) trendKey {
	k := trendKey{days: now.ImageSecurity.Days()}
	if now.Context == b.Context && len(now.Namespaces) > 0 {
		ns := slices.Clone(now.Namespaces)
		slices.Sort(ns)
		k.namespaces = strings.Join(slices.Compact(ns), ",")
	}
	return k
}

// trendAnswer is one answer of Prometheus and where it came from.
type trendAnswer struct {
	findings metrics.TrivyFindings
	source   string
}

// trendSnapshot is what a poller knows. It is never changed after it is made.
type trendSnapshot struct {
	key trendKey // what the last poll asked for
	// ans is the last answer for key; nil before one, and after a poll that found no Prometheus.
	ans *trendAnswer
	// noProm says the last poll found no Prometheus.
	noProm bool
	// err says why the last poll failed; the answer before it, for the same key, is kept in ans.
	err string
}

// same reports whether two snapshots show the same on the page: AsOf alone does not count.
func (a trendSnapshot) same(b trendSnapshot) bool {
	if a.key != b.key || a.noProm != b.noProm || a.err != b.err || (a.ans == nil) != (b.ans == nil) {
		return false
	}
	return a.ans == nil || a.ans.source == b.ans.source && a.ans.findings.Scraped == b.ans.findings.Scraped &&
		slices.EqualFunc(a.ans.findings.Points, b.ans.findings.Points, func(x, y metrics.TrivyFindingsPoint) bool {
			x.At, y.At = time.Time{}, time.Time{}
			return x == y
		})
}

// trendOptions tune a poller; zero fields take the defaults. Tests inject the clock.
type trendOptions struct {
	interval time.Duration                        // imageSecTrendInterval
	now      func() time.Time                     // time.Now
	after    func(time.Duration) <-chan time.Time // time.After
}

// trendPoller reads one cluster's Prometheus for the curve while it is active (set). Inactive it
// sends nothing and keeps its snapshot; active again it polls at once when the next poll is due,
// and at once when the curve asked for changed. Each poll runs on the poller's goroutine, which
// recovers from a panic at its boundary (a poll that panicked is a failure). Safe for concurrent
// use.
type trendPoller struct {
	ask      func(ctx context.Context, now time.Time, k trendKey) (trendAnswer, error)
	changed  func()
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time

	mu       sync.Mutex
	active   bool
	started  bool
	closed   bool
	want     trendKey
	snap     trendSnapshot
	next     time.Time // when the next poll is due; zero: at once
	failures int       // polls that failed in a row
	wake     chan struct{}
	quit     chan struct{}
	ctx      context.Context
	stopRun  context.CancelFunc
}

func newTrendPoller(opts trendOptions, log *slog.Logger, ask func(context.Context, time.Time, trendKey) (trendAnswer, error), changed func()) *trendPoller {
	if opts.interval <= 0 {
		opts.interval = imageSecTrendInterval
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.after == nil {
		opts.after = time.After
	}
	ctx, stop := context.WithCancel(context.Background())
	return &trendPoller{ask: ask, changed: changed, log: log, interval: opts.interval, now: opts.now, after: opts.after,
		wake: make(chan struct{}, 1), quit: make(chan struct{}), ctx: ctx, stopRun: stop}
}

// set starts (on) or pauses the polls of the curve want; a curve not asked for before is due at
// once. The first activation starts the goroutine.
func (p *trendPoller) set(on bool, want trendKey) {
	p.mu.Lock()
	if p.closed || p.active == on && p.want == want {
		p.mu.Unlock()
		return
	}
	if p.want != want {
		p.want, p.next, p.failures = want, time.Time{}, 0
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
	signal(p.wake)
}

// snapshot returns what the poller knows.
func (p *trendPoller) snapshot() trendSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap
}

// stop ends the poller without waiting: a poll under way is cancelled and its answer dropped.
func (p *trendPoller) stop() {
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

// run is the poller's goroutine: a poll whenever one is due while active.
func (p *trendPoller) run() {
	defer func() { // the goroutine's boundary: a poll recovers on its own (poll), this is the loop's
		if r := recover(); r != nil {
			p.log.Error("image security trend: the poller's loop panicked; it stops", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
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

// poll asks Prometheus once for the curve wanted now and schedules the next poll; when another
// curve was asked for meanwhile, that one is due at once. A panic is recovered here and counts as a
// failure.
func (p *trendPoller) poll() {
	p.mu.Lock()
	key := p.want
	p.mu.Unlock()
	var (
		ans trendAnswer
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("image security trend: a poll of Prometheus panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				err = errors.New("internal error")
			}
		}()
		ans, err = p.ask(p.ctx, p.now(), key)
	}()
	if p.ctx.Err() != nil {
		return // stopped
	}
	p.mu.Lock()
	old := p.snap
	snap := trendSnapshot{key: key}
	switch {
	case err == nil:
		snap.ans = &ans
	case errors.Is(err, errNoPrometheus):
		snap.noProm = true
	default:
		snap.err = err.Error()
		if old.key == key {
			snap.ans = old.ans
		}
	}
	if err == nil {
		p.failures = 0
	} else {
		p.failures++
	}
	if p.want == key {
		wait := p.interval
		if err != nil {
			wait = p.backoffLocked()
		}
		p.next = p.now().Add(wait)
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
func (p *trendPoller) backoffLocked() time.Duration {
	d := imageSecTrendFirstRetry
	for i := 1; i < p.failures && d < p.interval; i++ {
		d *= 2
	}
	return min(d, p.interval)
}

// trendSource is a backend's curve poller, while the page may need one.
type trendSource struct {
	mu sync.Mutex
	p  *trendPoller
}

// current returns the poller, or nil.
func (t *trendSource) current() *trendPoller {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.p
}

// ensure returns the poller, made with mk when none runs; mk runs without the source's lock.
func (t *trendSource) ensure(mk func() *trendPoller) *trendPoller {
	if p := t.current(); p != nil {
		return p
	}
	p := mk()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.p != nil { // made meanwhile: the follower runs under the state's lock, so not expected
		p.stop()
		return t.p
	}
	t.p = p
	return p
}

// stop ends the poller without waiting for it and forgets its snapshot; it reports whether one ran.
func (t *trendSource) stop() bool {
	t.mu.Lock()
	p := t.p
	t.p = nil
	t.mu.Unlock()
	if p == nil {
		return false
	}
	p.stop()
	return true
}

// followImageSecurityTrend is the curve's integration follower (integrations.go): the poller exists
// while the Trivy and the Prometheus integration are on for a cluster that may have a Prometheus
// (wantsHistory), and is active while the Image Security page is the shown view of the shown,
// connected cluster. Off, it is stopped and its answer forgotten. It runs with the state's lock
// held: nothing here waits for Prometheus.
func followImageSecurityTrend(st *state, b *Backend, now UIState) {
	s := st.s
	if wantsHistory(b) != "" || !now.IntegrationOn(b.Context, IntegrationTrivy) || !now.IntegrationOn(b.Context, IntegrationPrometheus) {
		if b.secTrend.stop() {
			s.imageSecTrendNoticed(b)
		}
		return
	}
	p := b.secTrend.ensure(func() *trendPoller { return s.newTrendPoller(b) })
	p.set(IsImageSecurity(now.View) && now.Context == b.Context && now.connectionState(b.Context) == Connected, trendKeyOf(b, now))
}

// newTrendPoller makes a backend's poller, asking the Prometheus its metrics hub detected.
func (s *Session) newTrendPoller(b *Backend) *trendPoller {
	var p *trendPoller
	ask := func(ctx context.Context, now time.Time, k trendKey) (trendAnswer, error) {
		prom, state := b.Metrics.Prometheus(ctx)
		switch {
		case prom != nil:
			f, err := prom.TrivyFindings(ctx, now, k.days, k.namespaceList())
			return trendAnswer{findings: f, source: prom.Label()}, err
		case state.Status == metrics.StatusNotServed:
			return trendAnswer{}, errNoPrometheus
		}
		return trendAnswer{}, errors.New(state.Message)
	}
	p = newTrendPoller(trendOptions{now: s.opts.Now}, s.log, ask, func() {
		if b.secTrend.current() == p { // a stopped poller's late change is dropped
			s.imageSecTrendNoticed(b)
		}
	})
	return p
}

// imageSecTrendNoticed wakes the open Image Security page of b's cluster, on a goroutine of its own
// (the follower holds the state's lock): its worker rebuilds and announces the page (EventTable),
// whose model then reads the new curve. It is not a change of the UI state.
func (s *Session) imageSecTrendNoticed(b *Backend) {
	s.goSafe("the image security trend's notice", func() {
		s.state.mu.Lock()
		w := s.state.imageSec
		s.state.mu.Unlock()
		if w != nil && w.backend == b {
			signal(w.wake)
		}
	})
}

// imageSecurityTrend is the curve the Image Security page of b's cluster shows in the UI state now,
// from what its poller knows: it sends no request. Nil when there is no curve to show at all: the
// Trivy or the Prometheus integration is off, or the cluster has no Prometheus. With a Note and no
// points while Prometheus is being asked, does not scrape Trivy Operator or names no image in the
// namespaces; with a Note and the points before it when the last request failed.
func (s *Session) imageSecurityTrend(b *Backend, now UIState) *ImageSecurityTrend {
	if b == nil || !now.IntegrationOn(b.Context, IntegrationTrivy) || !now.IntegrationOn(b.Context, IntegrationPrometheus) {
		return nil
	}
	key := trendKeyOf(b, now)
	if b.Demo {
		return demoImageSecurityTrend(key.days, key.namespaceList(), s.Now())
	}
	if wantsHistory(b) != "" {
		return nil
	}
	out := &ImageSecurityTrend{Days: key.days, Note: noteTrendAsking}
	p := b.secTrend.current()
	if p == nil {
		return out
	}
	snap := p.snapshot()
	switch {
	case snap.key != key: // another curve's answer, or none yet
		return out
	case snap.noProm:
		return nil
	case snap.ans == nil && snap.err != "":
		out.Note = snap.err
		return out
	case snap.ans == nil:
		return out
	}
	f := snap.ans.findings
	out.Source, out.AsOf, out.Note = snap.ans.source, f.AsOf, snap.err
	switch {
	case !f.Scraped && out.Note == "":
		out.Note = noteTrendNotScraped
	case len(f.Points) == 0 && out.Note == "":
		out.Note = noteTrendNoImages
	}
	out.Points = make([]ImageSecurityTrendPoint, 0, len(f.Points))
	for _, pt := range f.Points {
		out.Points = append(out.Points, ImageSecurityTrendPoint{At: pt.At, Critical: pt.Critical, High: pt.High, Medium: pt.Medium, Low: pt.Low, Unknown: pt.Unknown})
	}
	if len(out.Points) == 0 {
		out.Points = nil
	}
	return out
}

// demoTrendNow are the demo cluster's findings now in every namespace, each image once: the
// magnitude its reports have (internal/demo/trivy.go), Critical to Unknown.
var demoTrendNow = [5]float64{506, 912, 2260, 2683, 236}

// demoImageSecurityTrend draws the demo cluster's curve, which has no Prometheus: falling slowly
// toward now, the older points higher by up to a third for 90 days, a patch day 12 days ago that
// halved Critical and cut High by a fifth, and a small noise. A point's value depends on its age
// and the namespaces alone, so every render of a range draws the same curve. Namespaces take a
// share of the whole drawn from their names.
func demoImageSecurityTrend(days int, namespaces []string, now time.Time) *ImageSecurityTrend {
	step := metrics.TrivyFindingsStep(days)
	n := int(time.Duration(days) * 24 * time.Hour / step)
	end := now.Truncate(time.Hour)
	share := demoTrendShare(namespaces)
	out := &ImageSecurityTrend{Days: days, Source: demoTrendSource, AsOf: end, Points: make([]ImageSecurityTrendPoint, n)}
	for i := range n {
		ageSteps := n - 1 - i
		age := time.Duration(ageSteps) * step
		ageDays := age.Hours() / 24
		var v [5]int
		for sev, base := range demoTrendNow {
			f := 1 + 0.0037*ageDays + 0.02*demoNoise(int(age.Hours()), sev)
			if ageDays > 12 {
				f *= [5]float64{2, 1.25, 1, 1, 1}[sev]
			}
			v[sev] = int(math.Round(base * share * f))
		}
		out.Points[i] = ImageSecurityTrendPoint{At: end.Add(-age), Critical: v[0], High: v[1], Medium: v[2], Low: v[3], Unknown: v[4]}
	}
	return out
}

// demoTrendShare is the share of the demo's findings in the namespaces: 1 for every namespace,
// else 2 to 10 percent for each, drawn from its name, at most the whole.
func demoTrendShare(namespaces []string) float64 {
	if len(namespaces) == 0 {
		return 1
	}
	share := 0.0
	for _, ns := range namespaces {
		h := fnv.New64a()
		_, _ = h.Write([]byte(ns))
		share += 0.02 + 0.08*float64(h.Sum64()%1000)/1000
	}
	return min(share, 1)
}

// demoNoise is a number in [-1, 1] drawn from a point's age in hours and a severity.
func demoNoise(ageHours, sev int) float64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d/%d", ageHours, sev)
	return float64(h.Sum64()%2001)/1000 - 1
}
