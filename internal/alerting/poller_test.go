package alerting

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeProxy answers the poller's GETs by target, and counts them.
type fakeProxy struct {
	mu      sync.Mutex
	answers map[string]func() ([]byte, error) // by Target.Where()
	asked   []string
	panics  bool
}

func (f *fakeProxy) set(where string, fn func() ([]byte, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answers == nil {
		f.answers = map[string]func() ([]byte, error){}
	}
	f.answers[where] = fn
}

func (f *fakeProxy) get(_ context.Context, t Target, path string, params map[string]string, _ int64) ([]byte, error) {
	f.mu.Lock()
	f.asked = append(f.asked, t.Where()+path)
	fn, panics := f.answers[t.Where()], f.panics
	f.mu.Unlock()
	if panics {
		panic("boom")
	}
	if path != alertsPath || params["active"] != "true" || params["silenced"] != "false" || params["inhibited"] != "false" {
		return nil, errors.New("unexpected request " + path)
	}
	if fn == nil {
		return nil, apierrors.NewServiceUnavailable("no endpoints available for service")
	}
	return fn()
}

func (f *fakeProxy) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func answer(b []byte) func() ([]byte, error) { return func() ([]byte, error) { return b, nil } }

func services(svcs ...Service) ServiceSource {
	return ServiceSourceFunc(func(context.Context) ([]Service, error) { return svcs, nil })
}

// waitFor polls cond until it holds or two seconds passed.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newTestPoller(px proxy, src ServiceSource, changes *atomic.Int32) *Poller {
	return newPoller(px, src, Options{Interval: 20 * time.Millisecond, RetryMax: 80 * time.Millisecond, KeepFor: time.Hour,
		OnChange: func() { changes.Add(1) }})
}

// The poller: nothing is sent until it is active; detection asks the best candidate with the alerts
// request itself, passing over one that does not answer as an Alertmanager; then one request per
// interval; inactive, it stops; a change of what it shows calls OnChange, an equal answer does not.
func TestPollerDetectsAndPolls(t *testing.T) {
	px := &fakeProxy{}
	at := time.Now().Add(-time.Hour)
	px.set("monitoring/kube-prometheus-alertmanager:9093", answer([]byte(`{"status":"success","data":{}}`))) // not an Alertmanager
	px.set("monitoring/alertmanager-operated:9093", answer(wire(t, wireAlertOf("A", "warning", at, nil))))
	var changes atomic.Int32
	p := newTestPoller(px, services(
		svc("monitoring", "alertmanager-operated", ServicePort{Name: "web", Port: 9093}),
		svc("monitoring", "kube-prometheus-alertmanager", ServicePort{Name: "http-web", Port: 9093}),
	), &changes)
	defer p.Close()
	time.Sleep(30 * time.Millisecond)
	if px.count() != 0 || p.Snapshot().State != StateChecking {
		t.Fatalf("an inactive poller asked %d times, state %s", px.count(), p.Snapshot().State)
	}
	p.SetActive(true)
	waitFor(t, "the answer", func() bool { return p.Snapshot().Answered() })
	snap := p.Snapshot()
	if snap.State != StateFound || snap.Target.Where() != "monitoring/alertmanager-operated:9093" || snap.Total != 1 || snap.Alerts[0].Name != "A" {
		t.Fatalf("snapshot %+v", snap)
	}
	waitFor(t, "more polls", func() bool { return px.count() >= 5 })
	if c := changes.Load(); c != 1 {
		t.Errorf("OnChange ran %d times for one change", c)
	}
	p.SetActive(false)
	n := px.count()
	time.Sleep(60 * time.Millisecond)
	if px.count() > n+1 { // one poll may have been under way
		t.Errorf("an inactive poller went on: %d requests after %d", px.count(), n)
	}
	// Active again after more than an interval: at once.
	p.SetActive(true)
	waitFor(t, "a poll after the pause", func() bool { return px.count() > n+1 })
}

// Not found: no candidate costs no request, and detection looks again after RetryMax; an address
// is asked alone, configured; a forbidden service proxy says so; set back to nil, detection again.
func TestPollerStates(t *testing.T) {
	px := &fakeProxy{}
	var changes atomic.Int32
	p := newTestPoller(px, services(svc("default", "web", ServicePort{Port: 80})), &changes)
	defer p.Close()
	p.SetActive(true)
	waitFor(t, "not found", func() bool { return p.Snapshot().State == StateNotFound })
	if px.count() != 0 {
		t.Errorf("not found sent %d requests", px.count())
	}

	px.set("alerts/am:9093/am", answer([]byte(`[]`)))
	p.SetAddress(&Address{Namespace: "alerts", Service: "am", Port: "9093", Prefix: "am"})
	waitFor(t, "configured", func() bool { return p.Snapshot().State == StateConfigured })
	if snap := p.Snapshot(); !snap.Answered() || snap.Total != 0 || snap.Target.Where() != "alerts/am:9093/am" {
		t.Errorf("configured %+v", snap)
	}

	px.set("alerts/am:9093/am", func() ([]byte, error) {
		return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services/proxy"}, "am", errors.New("RBAC"))
	})
	waitFor(t, "forbidden", func() bool { return p.Snapshot().State == StateForbidden })
	if snap := p.Snapshot(); snap.Answered() || snap.Error == "" {
		t.Errorf("forbidden keeps %+v", snap)
	}
	p.SetAddress(nil)
	// The new address drops the old state at once; detection without a candidate sends nothing and
	// may already have found nothing (CI's race job, 2026-10-17: NotFound before the check ran).
	if st := p.Snapshot().State; st != StateChecking && st != StateNotFound {
		t.Errorf("a new address keeps %s", st)
	}
	waitFor(t, "detection again", func() bool { return p.Snapshot().State == StateNotFound })
}

// Failures: the last answer is kept while the Alertmanager does not answer, with since when, the
// polls back off; an answer again clears it. A service that answers no more as an Alertmanager is
// looked for again. A panicking poll is a failure, and the poller goes on.
func TestPollerFailures(t *testing.T) {
	px := &fakeProxy{}
	at := time.Now().Add(-time.Hour)
	good := wire(t, wireAlertOf("A", "critical", at, nil))
	where := "monitoring/alertmanager-operated:9093"
	px.set(where, answer(good))
	var changes atomic.Int32
	p := newTestPoller(px, services(svc("monitoring", "alertmanager-operated", ServicePort{Name: "web", Port: 9093})), &changes)
	defer p.Close()
	p.SetActive(true)
	waitFor(t, "the answer", func() bool { return p.Snapshot().Answered() })

	px.set(where, func() ([]byte, error) { return nil, apierrors.NewServiceUnavailable("no endpoints available") })
	waitFor(t, "unavailable", func() bool { return p.Snapshot().State == StateUnavailable })
	snap := p.Snapshot()
	if snap.Total != 1 || snap.FailingSince.IsZero() || snap.Target.Where() != where || snap.Error == "" {
		t.Errorf("unavailable %+v", snap)
	}

	px.set(where, answer(good))
	waitFor(t, "found again", func() bool { return p.Snapshot().State == StateFound })
	if snap := p.Snapshot(); !snap.FailingSince.IsZero() || snap.Error != "" {
		t.Errorf("found again %+v", snap)
	}

	px.mu.Lock()
	px.panics = true
	px.mu.Unlock()
	waitFor(t, "the panic as a failure", func() bool { return p.Snapshot().Error == "internal error" })
	px.mu.Lock()
	px.panics = false
	px.mu.Unlock()
	waitFor(t, "found after the panic", func() bool { return p.Snapshot().State == StateFound })

	// The service turns into something else: looked for again; nothing else is there: not found.
	px.set(where, answer([]byte(`{}`)))
	waitFor(t, "not found", func() bool { return p.Snapshot().State == StateNotFound })
	if snap := p.Snapshot(); snap.Answered() || snap.Total != 0 {
		t.Errorf("not found keeps %+v", snap)
	}
}

// The backoff doubles from the interval up to RetryMax; not found waits RetryMax; an answer kept
// beyond KeepFor is dropped at the next failure.
func TestPollerBackoffAndKeep(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	p := newPoller(&fakeProxy{}, nil, Options{Interval: 30 * time.Second, Now: func() time.Time { return now }})
	defer p.Close()
	for i, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		p.failures = i + 1
		if got := p.backoffLocked(false); got != want {
			t.Errorf("after %d failures: %v, want %v", i+1, got, want)
		}
	}
	if got := p.backoffLocked(true); got != DefaultRetryMax {
		t.Errorf("not found: %v", got)
	}

	target := &Target{Namespace: "m", Service: "am", Port: "web"}
	p.apply(0, result{state: StateFound, target: target, alerts: []Alert{{Name: "A"}}, total: 1})
	if s := p.Snapshot(); s.AsOf != now || s.Total != 1 {
		t.Fatalf("answer %+v", s)
	}
	fail := result{state: StateUnavailable, err: "unavailable (HTTP 503)"}
	now = now.Add(time.Minute)
	p.apply(0, fail)
	if s := p.Snapshot(); s.Total != 1 || !s.FailingSince.Equal(now) {
		t.Errorf("kept %+v", s)
	}
	now = now.Add(DefaultKeepFor)
	p.apply(0, fail)
	if s := p.Snapshot(); s.Answered() || s.Total != 0 || s.State != StateUnavailable || s.Target != (Target{}) {
		t.Errorf("after KeepFor %+v", s)
	}
	// An older address's poll is not applied.
	p.gen = 5
	p.apply(4, result{state: StateFound, target: target, total: 9})
	if p.Snapshot().Total == 9 {
		t.Error("a poll of an older address was applied")
	}
}

// TestPollerTriesThePortsNumberThenName (the owner's acme-mgmt, 2026-10-18): a refused spelling of
// the port, or a refused candidate, does not end the search; the spelling granted is used.
func TestPollerTriesThePortsNumberThenName(t *testing.T) {
	refuse := func() ([]byte, error) {
		return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services/proxy"}, "x", errors.New("not granted"))
	}
	px := &fakeProxy{}
	at := time.Now().Add(-time.Hour)
	px.set("monitoring/kube-prometheus-alertmanager:9093", refuse)
	px.set("monitoring/kube-prometheus-alertmanager:http-web", refuse)
	px.set("monitoring/alertmanager-operated:9093", refuse)
	px.set("monitoring/alertmanager-operated:web", answer(wire(t, wireAlertOf("A", "warning", at, nil))))
	var changes atomic.Int32
	p := newTestPoller(px, services(
		svc("monitoring", "alertmanager-operated", ServicePort{Name: "web", Port: 9093}),
		svc("monitoring", "kube-prometheus-alertmanager", ServicePort{Name: "http-web", Port: 9093}),
	), &changes)
	defer p.Close()
	p.SetActive(true)
	waitFor(t, "the answer", func() bool { return p.Snapshot().Answered() })
	if snap := p.Snapshot(); snap.State != StateFound || snap.Target.Where() != "monitoring/alertmanager-operated:web" {
		t.Errorf("found %+v", snap)
	}

	// Every spelling of every candidate refused: forbidden, each named.
	px2 := &fakeProxy{}
	for _, w := range []string{"monitoring/kube-prometheus-alertmanager:9093", "monitoring/kube-prometheus-alertmanager:http-web"} {
		px2.set(w, refuse)
	}
	p2 := newTestPoller(px2, services(svc("monitoring", "kube-prometheus-alertmanager", ServicePort{Name: "http-web", Port: 9093})), &changes)
	defer p2.Close()
	p2.SetActive(true)
	waitFor(t, "forbidden", func() bool { return p2.Snapshot().State == StateForbidden })
	if e := p2.Snapshot().Error; !strings.Contains(e, "monitoring/kube-prometheus-alertmanager:9093 nor monitoring/kube-prometheus-alertmanager:http-web") {
		t.Errorf("forbidden error %q", e)
	}
}

// Close cancels the poll under way and returns.
func TestPollerCloseCancels(t *testing.T) {
	px := &fakeProxy{}
	block := make(chan struct{})
	px.set("m/am:web", func() ([]byte, error) { <-block; return nil, context.Canceled })
	p := newPoller(slowProxy{px, block}, nil, Options{Interval: time.Second})
	p.SetAddress(&Address{Namespace: "m", Service: "am", Port: "web"})
	p.SetActive(true)
	waitFor(t, "the request", func() bool { return px.count() == 1 })
	done := make(chan struct{})
	go func() { p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited for the request")
	}
	close(block)
}

// slowProxy ends a request when its context ends.
type slowProxy struct {
	*fakeProxy
	block chan struct{}
}

func (s slowProxy) get(ctx context.Context, t Target, path string, params map[string]string, limit int64) ([]byte, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-s.block:
		}
		close(done)
	}()
	go func() { _, _ = s.fakeProxy.get(ctx, t, path, params, limit) }()
	<-done
	return nil, ctx.Err()
}

func TestErrorText(t *testing.T) {
	// An answer of the service itself is never quoted.
	err := apierrors.NewGenericServerResponse(http.StatusForbidden, "get", schema.GroupResource{Resource: "services"}, "am", "secret body", 0, true)
	if got := errorText(err); got != "forbidden (HTTP 403)" || forbidden(err) {
		t.Errorf("the service's 403: %q, forbidden %v", got, forbidden(err))
	}
	rbac := apierrors.NewForbidden(schema.GroupResource{Resource: "services/proxy"}, "am", errors.New("no"))
	if !forbidden(rbac) || errorText(rbac) == "" {
		t.Error("the API server's refusal is not forbidden")
	}
	if errorClass(context.DeadlineExceeded) != "timeout" || errorClass(errTooLarge) != "answer too large" {
		t.Error("classes")
	}
}
