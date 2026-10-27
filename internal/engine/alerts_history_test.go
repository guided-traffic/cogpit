package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func TestHistoryHoursMatchMetrics(t *testing.T) {
	if HistoryHours != metrics.AlertHistoryHours {
		t.Fatalf("the board has %d hours, metrics %d", HistoryHours, metrics.AlertHistoryHours)
	}
}

func onsetHours(at ...int) [metrics.AlertHistoryHours]int {
	var h [metrics.AlertHistoryHours]int
	for i := 0; i+1 < len(at); i += 2 {
		h[at[i]] = at[i+1]
	}
	return h
}

// The rules' severity labels become the cards' severities as the board's Classify makes them:
// "major" and a missing label show as warning and are summed with it, the meta alerts and severity
// none have no card.
func TestJoinHistory(t *testing.T) {
	asOf := time.Unix(1_800_000_000, 0)
	d := joinHistory(metrics.AlertOnsets{
		AsOf: asOf,
		Hours: map[metrics.AlertKey][metrics.AlertHistoryHours]int{
			{Alertname: "KustomizationNotReady", Severity: "major"}:    onsetHours(3, 2, 23, 1),
			{Alertname: "KustomizationNotReady", Severity: "warning"}:  onsetHours(3, 1),
			{Alertname: "KustomizationNotReady", Severity: ""}:         onsetHours(5, 4),
			{Alertname: "CertificateExpiration", Severity: "Critical"}: onsetHours(0, 1),
			{Alertname: "Watchdog", Severity: "none"}:                  onsetHours(1, 1),
			{Alertname: "InfoInhibitor", Severity: "info"}:             onsetHours(1, 1),
			{Alertname: "Silent", Severity: "none"}:                    onsetHours(1, 1),
		},
		Firing: map[metrics.AlertKey]bool{
			{Alertname: "KustomizationNotReady", Severity: "major"}: true,
			{Alertname: "Watchdog", Severity: "none"}:               true,
		},
	})
	kust := cardKey{"warning", "KustomizationNotReady"}
	if got, want := d.hours[kust], onsetHours(3, 3, 5, 4, 23, 1); got != want {
		t.Errorf("warning/KustomizationNotReady\n got %v\nwant %v", got, want)
	}
	if got := d.hours[cardKey{"critical", "CertificateExpiration"}]; got != onsetHours(0, 1) {
		t.Errorf("critical/CertificateExpiration %v", got)
	}
	if len(d.hours) != 2 {
		t.Errorf("cards %v", d.hours)
	}
	if !d.firing[kust] || len(d.firing) != 1 || !d.asOf.Equal(asOf) {
		t.Errorf("firing %v as of %v", d.firing, d.asOf)
	}
}

// A card's history by what the poller knows.
func TestBoardHistoryCard(t *testing.T) {
	asOf := time.Unix(1_800_000_000, 0)
	crash := cardKey{"warning", "KubePodCrashLooping"}
	crashHours := onsetHours(0, 2, 23, 3)
	data := &historyData{hours: map[cardKey][metrics.AlertHistoryHours]int{crash: crashHours},
		firing: map[cardKey]bool{crash: true}, asOf: asOf}
	cases := []struct {
		name string
		h    boardHistory
		want *AlertHistory
		note string
	}{
		{"no Prometheus", boardHistory{why: noteNoPrometheus}, nil, noteNoPrometheus},
		{"integration off", boardHistory{why: notePrometheusOff}, nil, notePrometheusOff},
		{"not asked yet", boardHistory{}, &AlertHistory{State: HistoryChecking}, ""},
		{"failed from the start", boardHistory{snap: historySnapshot{err: "Prometheus monitoring/p failed"}},
			&AlertHistory{State: HistoryUnavailable, Detail: "Prometheus monitoring/p failed"}, ""},
		{"answered", boardHistory{snap: historySnapshot{data: data}}, &AlertHistory{State: HistoryOK, Total: 5, AsOf: asOf,
			Hours: crashHours[:]}, ""},
		{"answer kept while it fails", boardHistory{snap: historySnapshot{data: data, err: "x"}},
			&AlertHistory{State: HistoryOK, Total: 5, AsOf: asOf, Hours: crashHours[:]}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.h.card("warning", "KubePodCrashLooping")
			gj, _ := json.Marshal(got)
			wj, _ := json.Marshal(c.want)
			if string(gj) != string(wj) {
				t.Errorf("card\n got %s\nwant %s", gj, wj)
			}
			if c.h.note() != c.note {
				t.Errorf("note %q, want %q", c.h.note(), c.note)
			}
		})
	}
	// The Prometheus does not evaluate this alertname at this severity: no count, not zeros.
	h := boardHistory{snap: historySnapshot{data: data}}
	for _, k := range []cardKey{{"critical", "KubePodCrashLooping"}, {"warning", "ElsewhereAlert"}} {
		got := h.card(k.severity, k.alertname)
		if got == nil || got.State != HistoryNoRule || len(got.Hours) != 0 || got.Total != 0 || !got.AsOf.Equal(asOf) || got.Detail == "" {
			t.Errorf("%v: %+v", k, got)
		}
	}
}

// histClock drives a poller's time: After hands the wait to the test, which advances the clock and
// fires it.
type histClock struct {
	mu    sync.Mutex
	now   time.Time
	open  []chan time.Time // the waits not fired yet
	waits chan histWait
}

type histWait struct {
	d    time.Duration
	fire chan time.Time
}

func newHistClock() *histClock {
	return &histClock{now: time.Unix(1_800_000_000, 0), waits: make(chan histWait, 64)}
}

func (c *histClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *histClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *histClock) After(d time.Duration) <-chan time.Time {
	fire := make(chan time.Time, 1)
	c.mu.Lock()
	c.open = append(c.open, fire)
	c.mu.Unlock()
	c.waits <- histWait{d, fire}
	return fire
}

// next waits for the poller to wait again: everything it did before is done.
func (c *histClock) next(t *testing.T) histWait {
	t.Helper()
	select {
	case w := <-c.waits:
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("the poller did not wait for its next poll")
		return histWait{}
	}
}

// until waits for the poller to wait for d, passing over waits it armed on the way (a wake that
// came with the state unchanged may arm it again).
func (c *histClock) until(t *testing.T, d time.Duration) histWait {
	t.Helper()
	for {
		if w := c.next(t); w.d == d {
			return w
		}
	}
}

// passAll moves the clock by d and ends every wait open: whichever of them the poller waits on, it
// has been due.
func (c *histClock) passAll(d time.Duration) {
	c.advance(d)
	c.mu.Lock()
	open := c.open
	c.open = nil
	c.mu.Unlock()
	for _, f := range open {
		f <- c.Now()
	}
}

// pass moves the clock by the wait and ends it.
func (c *histClock) pass(w histWait) {
	c.advance(w.d)
	w.fire <- c.Now()
}

type histAsker struct {
	mu    sync.Mutex
	calls []time.Time
	reply func() (metrics.AlertOnsets, error)
}

func (a *histAsker) ask(_ context.Context, now time.Time) (metrics.AlertOnsets, error) {
	a.mu.Lock()
	a.calls = append(a.calls, now)
	reply := a.reply
	a.mu.Unlock()
	return reply()
}

func (a *histAsker) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func (a *histAsker) set(reply func() (metrics.AlertOnsets, error)) {
	a.mu.Lock()
	a.reply = reply
	a.mu.Unlock()
}

func answerOf(n int) func() (metrics.AlertOnsets, error) {
	return func() (metrics.AlertOnsets, error) {
		k := metrics.AlertKey{Alertname: "A", Severity: "warning"}
		return metrics.AlertOnsets{Hours: map[metrics.AlertKey][metrics.AlertHistoryHours]int{k: onsetHours(23, n)},
			Firing: map[metrics.AlertKey]bool{k: true}}, nil
	}
}

func newTestHistoryPoller(t *testing.T, clk *histClock, a *histAsker, changes *atomic.Int64) *historyPoller {
	t.Helper()
	p := newHistoryPoller(AlertHistoryOptions{Now: clk.Now, After: clk.After}, slog.New(slog.DiscardHandler),
		func(ctx context.Context, now time.Time) (metrics.AlertOnsets, error) { return a.ask(ctx, now) },
		func() { changes.Add(1) })
	t.Cleanup(p.stop)
	return p
}

// The cadence: a poll at once when it becomes active, then one every 5 minutes; paused it sends
// nothing and keeps its answer; active again within 5 minutes it waits for what is left, after them
// it polls at once.
func TestHistoryPollerCadence(t *testing.T) {
	clk, a, changes := newHistClock(), &histAsker{reply: answerOf(1)}, &atomic.Int64{}
	p := newTestHistoryPoller(t, clk, a, changes)

	if p.snapshot().data != nil || a.count() != 0 {
		t.Fatal("it asked before it was active")
	}
	p.setActive(true)
	w := clk.next(t)
	if a.count() != 1 || w.d != DefaultAlertHistoryInterval {
		t.Fatalf("first poll: %d asks, next in %v", a.count(), w.d)
	}
	if d := p.snapshot().data; d == nil || d.hours[cardKey{"warning", "A"}][23] != 1 {
		t.Fatalf("snapshot %+v", p.snapshot())
	}
	if changes.Load() != 1 {
		t.Errorf("%d changes after the first answer", changes.Load())
	}

	// The same answer again: asked, nothing to tell.
	clk.pass(w)
	w = clk.next(t)
	if a.count() != 2 || w.d != DefaultAlertHistoryInterval || changes.Load() != 1 {
		t.Fatalf("second poll: %d asks, next in %v, %d changes", a.count(), w.d, changes.Load())
	}

	// Paused for 2 minutes and shown again, it polls once the 3 minutes left have passed, not before.
	p.setActive(false)
	clk.advance(2 * time.Minute)
	p.setActive(true)
	clk.passAll(3 * time.Minute)
	clk.until(t, DefaultAlertHistoryInterval)
	if a.count() != 3 {
		t.Fatalf("shown again within the interval: %d asks, want one more", a.count())
	}

	// Paused past the interval: the answer is old, so it is asked again at once.
	p.setActive(false)
	clk.advance(10 * time.Minute)
	if a.count() != 3 {
		t.Fatalf("it asked while paused: %d", a.count())
	}
	a.set(answerOf(2))
	p.setActive(true)
	clk.until(t, DefaultAlertHistoryInterval)
	if a.count() != 4 || changes.Load() != 2 {
		t.Fatalf("shown after the interval: %d asks, %d changes", a.count(), changes.Load())
	}
	if p.snapshot().data.hours[cardKey{"warning", "A"}][23] != 2 {
		t.Errorf("snapshot %+v", p.snapshot())
	}
}

// Failures back off from 30 seconds, doubled, to the interval; the answer before them is kept; a
// failure is told once; an answer ends the backoff.
func TestHistoryPollerBackoff(t *testing.T) {
	clk, a, changes := newHistClock(), &histAsker{reply: answerOf(1)}, &atomic.Int64{}
	p := newTestHistoryPoller(t, clk, a, changes)
	p.setActive(true)
	w := clk.next(t)

	a.set(func() (metrics.AlertOnsets, error) {
		return metrics.AlertOnsets{}, errors.New("Prometheus ns/p does not answer")
	})
	var waits []time.Duration
	for range 7 {
		clk.pass(w)
		w = clk.next(t)
		waits = append(waits, w.d)
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits %v, want %v", waits, want)
		}
	}
	snap := p.snapshot()
	if snap.data == nil || snap.err != "Prometheus ns/p does not answer" {
		t.Errorf("the answer is not kept: %+v", snap)
	}
	if changes.Load() != 2 { // the answer, then the failure; not each further one
		t.Errorf("%d changes", changes.Load())
	}

	a.set(answerOf(1))
	clk.pass(w)
	if w = clk.next(t); w.d != DefaultAlertHistoryInterval {
		t.Errorf("after an answer the wait is %v", w.d)
	}
	if p.snapshot().err != "" || changes.Load() != 3 {
		t.Errorf("snapshot %+v, %d changes", p.snapshot(), changes.Load())
	}
}

// A poll that panics is a failure; the loop goes on. No Prometheus drops the answer.
func TestHistoryPollerPanicAndNoPrometheus(t *testing.T) {
	clk, a, changes := newHistClock(), &histAsker{}, &atomic.Int64{}
	a.reply = func() (metrics.AlertOnsets, error) { panic("bug") }
	p := newTestHistoryPoller(t, clk, a, changes)
	p.setActive(true)
	w := clk.next(t)
	if w.d != 30*time.Second || p.snapshot().err != "internal error" {
		t.Fatalf("panic: next in %v, %+v", w.d, p.snapshot())
	}

	a.set(answerOf(1))
	clk.pass(w)
	w = clk.next(t)
	if p.snapshot().data == nil {
		t.Fatal("no answer after the panic")
	}
	a.set(func() (metrics.AlertOnsets, error) {
		return metrics.AlertOnsets{}, fmt.Errorf("x: %w", errNoPrometheus)
	})
	clk.pass(w)
	clk.next(t)
	if s := p.snapshot(); !s.noProm || s.data != nil {
		t.Errorf("no Prometheus: %+v", s)
	}
}

// --- the whole path: a session, a fake API server, an Alertmanager and a Prometheus behind it ---

// histProm is a Prometheus behind the service proxy that answers the board's two queries and counts
// them.
type histProm struct {
	ranges, instants, probes atomic.Int64
	fail                     atomic.Bool
	mu                       sync.Mutex
	onsets                   map[string][]float64 // "alertname/severity": count per hour
	firing                   []string
}

func (h *histProm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/status/buildinfo":
		h.probes.Add(1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.55.1","revision":"x"}}`))
		return
	case "/api/v1/query_range", "/api/v1/query":
	default:
		http.NotFound(w, r)
		return
	}
	if !strings.Contains(r.URL.Query().Get("query"), "ALERTS{") {
		// The metrics hub's own queries (the cluster's usage): not the board's.
		kind := "vector"
		if r.URL.Path == "/api/v1/query_range" {
			kind = "matrix"
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"` + kind + `","result":[]}}`))
		return
	}
	if h.fail.Load() {
		http.Error(w, "refused", http.StatusServiceUnavailable)
		return
	}
	labels := func(key string) map[string]string {
		for i := range key {
			if key[i] == '/' {
				return map[string]string{"alertname": key[:i], "severity": key[i+1:]}
			}
		}
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var result []any
	if r.URL.Path == "/api/v1/query" {
		h.instants.Add(1)
		for _, k := range h.firing {
			result = append(result, map[string]any{"metric": labels(k), "value": []any{float64(time.Now().Unix()), "1"}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
		return
	}
	h.ranges.Add(1)
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	for k, counts := range h.onsets {
		var vals [][]any
		for i, n := range counts {
			vals = append(vals, []any{start + float64(i)*3600, strconv.FormatFloat(n, 'f', -1, 64)})
		}
		result = append(result, map[string]any{"metric": labels(k), "values": vals})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": result}})
}

func promService(namespace, name string) *unstructured.Unstructured {
	return testkit.Object("v1", "Service", namespace, name, map[string]any{"spec": map[string]any{
		"type": "ClusterIP", "clusterIP": "10.96.0.94",
		"ports": []any{map[string]any{"name": "http-web", "port": int64(9090), "protocol": "TCP"}},
	}})
}

func hours24(at ...int) []float64 {
	h := make([]float64, metrics.AlertHistoryHours)
	for i := 0; i+1 < len(at); i += 2 {
		h[at[i]] = float64(at[i+1])
	}
	return h
}

func cardOf(t *testing.T, m AlertBoardModel, severity, name string) AlertCard {
	t.Helper()
	for _, g := range m.Groups {
		if g.Key != severity {
			continue
		}
		for _, c := range g.Cards {
			if c.Name == name {
				return c
			}
		}
	}
	t.Fatalf("no card %s/%s", severity, name)
	return AlertCard{}
}

func waitHistory(t *testing.T, s *Session, what string, cond func(AlertBoardModel) bool) AlertBoardModel {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		m, err := s.AlertBoard("")
		if err == nil && cond(m) {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (%v)", what, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The history end to end. Prometheus is asked only while the alert board is the shown view: not
// before it opens, not after it closes, and not again while the answer is younger than 5 minutes;
// the board shows the kept answer when it reopens. The join: a rule labelled "major" is a warning
// card, a card whose alertname the Prometheus does not evaluate has no count, and switching the
// integration off ends the polls and the strips.
func TestAlertHistoryOnTheBoard(t *testing.T) {
	clk := newHistClock()
	all := []testkit.FakeRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"": all}}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.ServicesGVR, amService("monitoring", "kube-prometheus-alertmanager"))
	srv.Set(testkit.ServicesGVR, promService("monitoring", "kube-prometheus-prometheus"))
	for _, ns := range []string{"monitoring", "shop", "flux-system"} {
		srv.Set(testkit.NamespacesGVR, testkit.Namespace(ns))
	}
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(time.Now())...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:http-web", am)
	prom := &histProm{
		onsets: map[string][]float64{
			"KubePodCrashLooping/warning":   hours24(0, 2, 23, 3),
			"KustomizationNotReady/major":   hours24(10, 1),
			"KustomizationNotReady/warning": hours24(10, 2, 22, 1),
			"Watchdog/none":                 hours24(1, 1),
		},
		firing: []string{"KubePodCrashLooping/warning", "KustomizationNotReady/major", "Watchdog/none"},
	}
	srv.SetProxy("monitoring", "kube-prometheus-prometheus:http-web", prom)
	ms, err := NewMetricsSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}, MetricsSettings: ms},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: NewClusterSettingsStore(""),
		Alerts:       alerting.Options{Interval: 40 * time.Millisecond, RetryMax: 200 * time.Millisecond},
		AlertHistory: AlertHistoryOptions{Now: clk.Now, After: clk.After},
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "fake")
	mustConnect(t, s, "fake")
	waitUI(t, s, "the first answer", answered)

	// Not shown: the poller exists, never ran, and nothing was asked.
	b := s.BackendOf("fake")
	p := b.history.current()
	if p == nil || p.started {
		t.Fatalf("poller %+v before the board is shown", p)
	}
	m, err := s.AlertBoard("")
	if err != nil {
		t.Fatal(err)
	}
	if h := cardOf(t, m, "warning", "KubePodCrashLooping").History; h == nil || h.State != HistoryChecking {
		t.Errorf("before the first answer: %+v", h)
	}
	if prom.ranges.Load()+prom.instants.Load() != 0 {
		t.Fatal("Prometheus was asked before the board was shown")
	}

	// Shown: one pair of requests.
	s.UpdateUI(func(u *UIState) { u.View = AlertBoard })
	m = waitHistory(t, s, "the first history", func(m AlertBoardModel) bool {
		h := cardOf(t, m, "warning", "KubePodCrashLooping").History
		return h != nil && h.State == HistoryOK
	})
	if m.HistoryNote != "" {
		t.Errorf("note %q", m.HistoryNote)
	}
	crash := cardOf(t, m, "warning", "KubePodCrashLooping").History
	if len(crash.Hours) != HistoryHours || crash.Hours[0] != 2 || crash.Hours[23] != 3 || crash.Total != 5 {
		t.Errorf("crash looping %+v", crash)
	}
	kust := cardOf(t, m, "warning", "KustomizationNotReady").History
	if kust.State != HistoryOK || kust.Total != 4 || kust.Hours[10] != 3 || kust.Hours[22] != 1 {
		t.Errorf("major and warning are one card: %+v", kust)
	}
	if cert := cardOf(t, m, "critical", "CertificateExpiration").History; cert.State != HistoryNoRule || cert.Hours != nil {
		t.Errorf("no rule here: %+v", cert)
	}
	if prom.ranges.Load() != 1 || prom.instants.Load() != 1 || prom.probes.Load() != 1 {
		t.Errorf("requests: %d range, %d instant, %d probes", prom.ranges.Load(), prom.instants.Load(), prom.probes.Load())
	}
	clk.next(t) // waiting for the next poll in 5 minutes

	// Closed: nothing is asked, the answer stays.
	s.UpdateUI(func(u *UIState) { u.View = "pods" })
	waitFor(t, "the poller to pause", func() bool { return !p.isActive() })
	clk.advance(30 * time.Minute)
	m, _ = s.AlertBoard("")
	if h := cardOf(t, m, "warning", "KubePodCrashLooping").History; h.State != HistoryOK || h.Total != 5 {
		t.Errorf("the kept answer: %+v", h)
	}
	if prom.ranges.Load() != 1 || prom.instants.Load() != 1 {
		t.Fatalf("Prometheus was asked while the board was closed: %d, %d", prom.ranges.Load(), prom.instants.Load())
	}

	// Opened again after 30 minutes: the answer is old, so it is asked again at once.
	prom.mu.Lock()
	prom.onsets["KubePodCrashLooping/warning"] = hours24(23, 7)
	prom.mu.Unlock()
	s.UpdateUI(func(u *UIState) { u.View = AlertBoard })
	waitHistory(t, s, "the second history", func(m AlertBoardModel) bool {
		return cardOf(t, m, "warning", "KubePodCrashLooping").History.Total == 7
	})
	if prom.ranges.Load() != 2 || prom.instants.Load() != 2 {
		t.Errorf("requests: %d range, %d instant", prom.ranges.Load(), prom.instants.Load())
	}

	// Prometheus fails: the kept answer stays, nothing is made up.
	prom.fail.Store(true)
	clk.pass(clk.next(t))
	waitFor(t, "the failure", func() bool { return p.snapshot().err != "" })
	m, _ = s.AlertBoard("")
	if h := cardOf(t, m, "warning", "KubePodCrashLooping").History; h.State != HistoryOK || h.Total != 7 {
		t.Errorf("after the failure: %+v", h)
	}

	// The integration off: no strips, the note says why, the polls end.
	if _, err := s.ChangeClusterSettings("fake", func(cs *ClusterSettings) error {
		off := IntPrometheus
		cs.IntegrationsOff = &off
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m = waitHistory(t, s, "the note", func(m AlertBoardModel) bool { return m.HistoryNote != "" })
	if m.HistoryNote != notePrometheusOff || cardOf(t, m, "warning", "KubePodCrashLooping").History != nil {
		t.Errorf("note %q, history %+v", m.HistoryNote, cardOf(t, m, "warning", "KubePodCrashLooping").History)
	}
	waitFor(t, "the poller to end", func() bool { return b.history.current() == nil })
}

func (p *historyPoller) isActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}
