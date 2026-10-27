package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeProber is a connection whose answers the test sets: up, down (no answer), rejecting the
// login, or a credential plugin that waits until released.
type fakeProber struct {
	mu        sync.Mutex
	down      bool          // heartbeats and reachability fail
	reject    bool          // heartbeats are rejected (401)
	rejectN   int           // reject only the next n heartbeats
	block     chan struct{} // heartbeats wait for it, ignoring their context (a plugin that waits)
	endNext   error         // the next heartbeat's plugin run is one Cogpit ends (D-73): it answers this, after block if set
	health    []cluster.Health
	resets    int
	armed     int
	beats     int
	reaches   []time.Time
	beatTimes []time.Time
	expired   bool     // the login expired by its time: the next heartbeat needs the credential plugin
	held      bool     // the engine holds the plugin (HoldLogin): an expired login lapses instead
	holds     []bool   // every HoldLogin
	renewals  int      // the plugin's runs that renewed an expired login
	versions  []string // the versions the heartbeats read, one per heartbeat in turn (SeenVersion); none: ""
	seen      string
}

// SeenVersion implements versionSeer: the version the last heartbeat read.
func (f *fakeProber) SeenVersion() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen
}

func (f *fakeProber) Heartbeat(ctx context.Context) error {
	f.mu.Lock()
	f.beats++
	f.beatTimes = append(f.beatTimes, time.Now())
	block, down, reject, end := f.block, f.down, f.reject, f.endNext
	f.endNext = nil
	if f.rejectN > 0 {
		f.rejectN--
		reject = true
	}
	if len(f.versions) > 0 {
		f.seen = f.versions[(f.beats-1)%len(f.versions)]
	}
	lapsed := false
	if f.expired && !down && block == nil {
		if f.held {
			lapsed = true // the plugin is held: nothing renews the login (cluster/loginhold.go)
		} else {
			f.expired = false // the plugin ran and renewed it
			f.renewals++
		}
	}
	f.mu.Unlock()
	if lapsed {
		return cluster.ErrLoginLapsed
	}
	if block != nil {
		<-block
		return end
	}
	switch {
	case end != nil:
		return end
	case down:
		<-ctx.Done()
		return ctx.Err()
	case reject:
		return cluster.ErrLoginRejected
	}
	return nil
}

func (f *fakeProber) Reachable(ctx context.Context) error {
	f.mu.Lock()
	f.reaches = append(f.reaches, time.Now())
	down := f.down
	f.mu.Unlock()
	if down {
		return errors.New("dial tcp 10.0.0.1:6443: connect: network is unreachable")
	}
	return nil
}

func (f *fakeProber) SetHealth(h cluster.Health) {
	f.mu.Lock()
	f.health = append(f.health, h)
	f.mu.Unlock()
}

func (f *fakeProber) ResetConnections() {
	f.mu.Lock()
	f.resets++
	f.mu.Unlock()
}

func (f *fakeProber) ArmLogin() {
	f.mu.Lock()
	f.armed++
	f.mu.Unlock()
}

func (f *fakeProber) HoldLogin(on bool) {
	f.mu.Lock()
	f.held = on
	f.holds = append(f.holds, on)
	f.mu.Unlock()
}

func (f *fakeProber) set(fn func(*fakeProber)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (f *fakeProber) get() fakeProber {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeProber{health: append([]cluster.Health(nil), f.health...), resets: f.resets, armed: f.armed, beats: f.beats,
		reaches: append([]time.Time(nil), f.reaches...), expired: f.expired, held: f.held, holds: append([]bool(nil), f.holds...),
		renewals: f.renewals}
}

// fastWatch are the watch's options in milliseconds instead of seconds.
var fastWatch = ConnWatchOptions{
	Heartbeat: 20 * time.Millisecond, ProbeTimeout: 50 * time.Millisecond, Confirm: 10 * time.Millisecond,
	LoginWait: 300 * time.Millisecond, BackoffFirst: 20 * time.Millisecond, BackoffMax: 160 * time.Millisecond,
	ProbeSlack: 50 * time.Millisecond,
}

// watchedSession connects a session whose backend the fake prober answers for.
func watchedSession(t *testing.T, opts ConnWatchOptions) (*Session, *fakeProber) {
	t.Helper()
	p := &fakeProber{}
	return watchedSessionOf(t, opts, p), p
}

// watchedSessionOf is watchedSession with a prober the test set up before the first heartbeat.
func watchedSessionOf(t *testing.T, opts ConnWatchOptions, p *fakeProber) *Session {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-1"))
	fc := &fakeConnector{src: src, profiles: testProfiles(), conn: &cluster.Conn{}, health: p}
	s := New(Options{Connector: fc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: opts})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func waitState(t *testing.T, s *Session, want ConnectionState) Connection {
	t.Helper()
	var got Connection
	waitUntil(t, "the connection "+string(want), func() bool {
		got = s.UIState().Connection
		return got.State == want
	})
	return got
}

// TestTwoFailedHeartbeatsInterruptTheConnection is S2 and S4: two heartbeats without an answer make
// the connection Interrupted with its time and next attempt; the gate holds, the network
// connections close; every mutating command is disabled with the reason "connection interrupted"
// for the window and agents alike, reads and UI commands run on.
func TestTwoFailedHeartbeatsInterruptTheConnection(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	registerWriteAndRead(t, s)
	if err := s.Registry().mustGet(t, "test.write").Enabled(s, nil); err != nil {
		t.Fatalf("a write while connected: %v", err)
	}
	before := time.Now()
	p.set(func(f *fakeProber) { f.down = true })
	c := waitState(t, s, Interrupted)
	if c.Since.Before(before) || c.Reason != "no answer from the API server" {
		t.Errorf("connection %+v", c)
	}
	got := p.get()
	if len(got.health) == 0 || got.health[0] != cluster.HealthInterrupted || got.resets != 1 {
		t.Errorf("the gate %v, resets %d", got.health, got.resets)
	}
	if got.beats < 2 {
		t.Errorf("%d heartbeats before the interruption, want two", got.beats)
	}
	err := s.Registry().mustGet(t, "test.write").Enabled(s, nil)
	if err == nil || err.Error() != "connection interrupted" {
		t.Fatalf("a write while interrupted: %v", err)
	}
	for _, o := range []Origin{OriginGUI, OriginExternalAgent, OriginBuiltInAgent} {
		start := time.Now()
		_, err := s.Dispatch(context.Background(), Invocation{ID: "test.write", Origin: o})
		var de *DisabledError
		if !errors.As(err, &de) || de.Reason != "connection interrupted" || time.Since(start) > time.Second {
			t.Errorf("%s's write while interrupted: %v", o, err)
		}
	}
	if _, err := s.Dispatch(context.Background(), Invocation{ID: "test.read", Origin: OriginExternalAgent}); err != nil {
		t.Errorf("a read while interrupted: %v", err)
	}
	if note := s.StaleNote(); !strings.Contains(note, "interrupted since") || !strings.Contains(note, "data from") {
		t.Errorf("the agents' note: %q", note)
	}
	// The view keeps the cache's last state.
	if tbl := waitTable(t, s, "pods", func(tb *TableSnapshot) bool { return tb.Synced }); len(tbl.Rows) != 1 || tbl.Error != "" {
		t.Errorf("the view while interrupted: %d rows, error %q", len(tbl.Rows), tbl.Error)
	}
}

// TestReconnectBacksOffAndComesBack is S5: the attempts follow 1, 2, 4, 8, ... at most 30 s (here
// in twentieths), TryNow tries at once and starts again, and once the cluster answers the
// connection is Connected again, the gate open, the writes enabled.
func TestReconnectBacksOffAndComesBack(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	registerWriteAndRead(t, s)
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	waitUntil(t, "six attempts", func() bool { return len(p.get().reaches) >= 6 })
	r := p.get().reaches
	for i, want := range []time.Duration{40, 80, 160, 160} { // after the 1st: 2, 4, 8, capped
		gap := r[i+2].Sub(r[i+1]) // never shorter than the backoff; longer only by the scheduler's delay
		if gap < want*time.Millisecond*8/10 || gap > want*time.Millisecond+500*time.Millisecond {
			t.Errorf("wait %d: %v, want about %v", i+2, gap, want*time.Millisecond)
		}
	}
	if c := s.UIState().Connection; c.NextAttempt.IsZero() && c.Reason == "" {
		t.Errorf("no next attempt or reason: %+v", c)
	}
	n := len(p.get().reaches)
	if err := s.TryNow(""); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "an attempt at once", func() bool { return len(p.get().reaches) > n })
	if d := time.Since(p.get().reaches[n]); d > time.Second {
		t.Errorf("TryNow waited %v", d)
	}
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	got := p.get()
	if got.health[len(got.health)-1] != cluster.HealthOK {
		t.Errorf("the gate after the reconnect: %v", got.health)
	}
	if err := s.Registry().mustGet(t, "test.write").Enabled(s, nil); err != nil {
		t.Errorf("a write after the reconnect: %v", err)
	}
	if c := s.UIState().Connection; !c.Since.IsZero() || c.Reason != "" {
		t.Errorf("connection after the reconnect: %+v", c)
	}
	// The reconnect publishes Connected before it posts its toast (connwatch.go reconnect): wait for
	// the toast, not for the state that comes before it (CI, 2026-10-21: the toast was not there yet).
	waitUntil(t, "the reconnect's toast", func() bool { return strings.Contains(lastToast(s), "Reconnected to test") })
}

// TestAWakeChecksAtOnce is S2's first signal: at wake one failed check interrupts.
func TestAWakeChecksAtOnce(t *testing.T) {
	opts := fastWatch
	opts.Heartbeat = time.Hour // only the wake checks
	s, p := watchedSession(t, opts)
	p.set(func(f *fakeProber) { f.down = true })
	s.NoteWake()
	waitState(t, s, Interrupted)
	if b := p.get().beats; b != 1 {
		t.Errorf("%d heartbeats, want the wake's one", b)
	}
}

// TestAStreamThatBreaksChecksAtOnce is S2's third signal: a broken watch asks for a check; two
// failed ones interrupt.
func TestAStreamThatBreaksChecksAtOnce(t *testing.T) {
	opts := fastWatch
	opts.Heartbeat = time.Hour
	s, p := watchedSession(t, opts)
	w := s.connWatch("")
	p.set(func(f *fakeProber) { f.down = true })
	from, _, err := func() (time.Time, time.Time, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan struct{})
		var f, to time.Time
		var e error
		go func() { f, to, e = w.await(ctx, s.Now(), nil); close(done) }()
		waitState(t, s, Interrupted)
		p.set(func(f *fakeProber) { f.down = false })
		<-done
		return f, to, e
	}()
	if err != nil || from.IsZero() {
		t.Fatalf("await: from %v, err %v", from, err)
	}
}

// TestAStreamThatBreaksAfterTheReconnectStillResumes: a stream opened before an interruption whose
// TCP connection outlived it breaks only once the connection is back; asked by when it opened, the
// watch still names the interruption it lived through, so the log resumes instead of ending (the
// race CI met on 2026-10-16 in TestStandbyWindowStaysUsable). A stream opened after it hears none.
func TestAStreamThatBreaksAfterTheReconnectStillResumes(t *testing.T) {
	opts := fastWatch
	s, p := watchedSession(t, opts)
	w := s.connWatch("")
	opened := s.Now()
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	from, to, err := w.await(ctx, opened, nil)
	if err != nil || from.IsZero() || to.Before(from) {
		t.Fatalf("a stream opened before the interruption: from %v to %v, err %v", from, to, err)
	}
	if from, _, err := w.await(ctx, s.Now(), nil); err != nil || !from.IsZero() {
		t.Errorf("a stream opened after it: from %v, err %v", from, err)
	}
}

// TestALastingUnauthorizedExpiresTheLogin is S6: the first rejection renews the login (the guard's
// one run of the plugin), the second makes it LoginExpired; no heartbeat runs while it is, so no
// plugin either; Log in again arms the guard and connects again.
func TestALastingUnauthorizedExpiresTheLogin(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	registerWriteAndRead(t, s)
	p.set(func(f *fakeProber) { f.reject = true })
	c := waitState(t, s, LoginExpired)
	if !strings.Contains(c.Reason, "401") {
		t.Errorf("reason %q", c.Reason)
	}
	if got := p.get().health; got[len(got)-1] != cluster.HealthLoginExpired {
		t.Errorf("the gate: %v", got)
	}
	if err := s.Registry().mustGet(t, "test.write").Enabled(s, nil); err == nil || !strings.Contains(err.Error(), "login expired") {
		t.Errorf("a write while the login expired: %v", err)
	}
	beats := p.get().beats
	time.Sleep(10 * fastWatch.Heartbeat) // the heartbeats a connected watch would make
	if b := p.get().beats; b != beats {
		t.Fatalf("%d heartbeats while the login expired", b-beats)
	}
	if err := s.TryNow(""); err == nil {
		t.Error("TryNow while the login expired")
	}
	p.set(func(f *fakeProber) { f.reject = false })
	if err := s.Login(); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, Connected)
	if p.get().armed != 1 {
		t.Errorf("the guard armed %d times", p.get().armed)
	}
}

// TestAPluginThatWaitsExpiresTheLogin is S6's other case: a heartbeat whose credential plugin does
// not answer within 30 s (here 300 ms), as one waiting for a browser login, makes the login
// expired; once the plugin answers after all, the connection is back without a click.
func TestAPluginThatWaitsExpiresTheLogin(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	block := make(chan struct{})
	p.set(func(f *fakeProber) { f.block = block })
	c := waitState(t, s, LoginExpired)
	if !strings.Contains(c.Reason, "has not answered") {
		t.Errorf("reason %q", c.Reason)
	}
	beats := p.get().beats
	p.set(func(f *fakeProber) { f.block = nil })
	close(block) // the user logged in in the browser
	waitState(t, s, Connected)
	if b := p.get().beats; b-beats > 2 {
		t.Errorf("%d heartbeats after the plugin answered", b-beats)
	}
}

// TestALoginRejectedAgainStaysExpired: Log in again whose renewed login is rejected too stays
// expired, saying so, and runs the plugin no more.
func TestALoginRejectedAgainStaysExpired(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	p.set(func(f *fakeProber) { f.reject = true })
	waitState(t, s, LoginExpired)
	if err := s.Login(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the login's answer", func() bool {
		c := s.UIState().Connection
		return c.State == LoginExpired && strings.Contains(c.Reason, "still rejects")
	})
	beats := p.get().beats
	time.Sleep(10 * fastWatch.Heartbeat)
	if b := p.get().beats; b != beats {
		t.Errorf("%d heartbeats after a failed login", b-beats)
	}
}

// TestAnOperationStopsAtTheInterruption is S8: the context of a Helm action ends with
// ErrConnectionLost when the connection is interrupted (a drain's job stops by itself:
// TestDrainJobStopsWhenInterrupted).
func TestAnOperationStopsAtTheInterruption(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	ctx, cancel := s.OperationContext(context.Background(), "")
	defer cancel()
	p.set(func(f *fakeProber) { f.down = true })
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the operation's context did not end")
	}
	if !errors.Is(context.Cause(ctx), ErrConnectionLost) {
		t.Errorf("cause %v", context.Cause(ctx))
	}
}

// TestADemoClusterHasNoWatch: the demo cluster (and a backend without a connection) cannot lose
// one: no watch, writes as before.
func TestADemoClusterHasNoWatch(t *testing.T) {
	src := testkit.NewMemorySource()
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	if s.connWatch("") != nil {
		t.Fatal("a watch without a connection")
	}
	if err := s.writeBlocked(DangerDestructive, ""); err != nil {
		t.Fatal(err)
	}
	s.NoteWake() // nil-safe
	s.NoteNetworkChange()
}

// registerWriteAndRead adds a mutating and a read command of the test's own.
func registerWriteAndRead(t *testing.T, s *Session) {
	t.Helper()
	if err := Register(s.Registry(), Spec[struct{}]{
		ID: "test.write", Label: "Write", Danger: DangerMutating, Description: "a write of the test's own",
		Targets: func(*Session, struct{}) ([]model.ObjectRef, error) { return nil, nil },
		Run:     func(context.Context, *Session, struct{}) (any, error) { return "written", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := Register(s.Registry(), Spec[struct{}]{
		ID: "test.read", Label: "Read", Danger: DangerRead, Description: "a read of the test's own",
		Run: func(context.Context, *Session, struct{}) (any, error) { return "read", nil },
	}); err != nil {
		t.Fatal(err)
	}
	s.SetAgentWrites("test", true) // the write would run without asking: only the connection refuses it
}

func (r *Registry) mustGet(t *testing.T, id string) *Command {
	t.Helper()
	c, ok := r.Get(id)
	if !ok {
		t.Fatalf("no command %s", id)
	}
	return c
}

// lastToast is the newest notification's text.
func lastToast(s *Session) string {
	n := s.Notifications(false).Notifications
	if len(n) == 0 {
		return ""
	}
	return n[0].Message
}

// TestAccessReviewsFailedDuringAnInterruptionAreAskedAgain: the reviews an interruption made fail
// (refused by the gate) do not leave the user's access unknown until a refresh: the reconnect
// forgets the failures and asks again.
func TestAccessReviewsFailedDuringAnInterruptionAreAskedAgain(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"default": {rule("list", "", "pods")}})
	rev.failRules, rev.failAccess = true, true // the connection is down: every review fails
	p := &fakeProber{down: true}
	fc := &fakeConnector{src: testkit.NewMemorySource(), profiles: testProfiles(), conn: &cluster.Conn{}, health: p}
	s := New(Options{Connector: accessConnector{fakeConnector: fc, rev: rev, namespace: "default"}, SnapshotInterval: 5 * time.Millisecond,
		ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	waitState(t, s, Interrupted)
	waitCond(t, "a failed review", func() bool {
		rules, _ := rev.calls()
		return rules["default"] >= 1
	})
	if _, known := b.Access.Allowed("default", listPods); known {
		t.Fatal("known from a failed review")
	}
	rev.set(func(f *fakeReviewer) { f.failRules, f.failAccess = false, false })
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	if !waitAllowed(t, b.Access, "default", listPods) {
		t.Error("list pods not allowed after the reconnect")
	}
}
