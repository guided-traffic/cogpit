package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// trendAsker answers a poller's asks and records them.
type trendAsker struct {
	mu    sync.Mutex
	keys  []trendKey
	reply func(k trendKey) (trendAnswer, error)
}

func (a *trendAsker) ask(_ context.Context, now time.Time, k trendKey) (trendAnswer, error) {
	a.mu.Lock()
	a.keys = append(a.keys, k)
	reply := a.reply
	a.mu.Unlock()
	return reply(k)
}

func (a *trendAsker) asked() []trendKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]trendKey(nil), a.keys...)
}

func (a *trendAsker) set(reply func(k trendKey) (trendAnswer, error)) {
	a.mu.Lock()
	a.reply = reply
	a.mu.Unlock()
}

// trendOf answers a curve whose last point has n Critical findings.
func trendOf(n int) func(trendKey) (trendAnswer, error) {
	return func(k trendKey) (trendAnswer, error) {
		return trendAnswer{source: "Prometheus (monitoring/prom)", findings: metrics.TrivyFindings{Scraped: true,
			Step: metrics.TrivyFindingsStep(k.days), Points: []metrics.TrivyFindingsPoint{{At: time.Unix(1_800_000_000, 0), Critical: n, High: 2}}}}, nil
	}
}

func newTestTrendPoller(t *testing.T, clk *histClock, a *trendAsker, changes *atomic.Int64) *trendPoller {
	t.Helper()
	p := newTrendPoller(trendOptions{now: clk.Now, after: clk.After}, slog.New(slog.DiscardHandler), a.ask, func() { changes.Add(1) })
	t.Cleanup(p.stop)
	return p
}

// The cadence: a poll at once when it becomes active, then one every 15 minutes; another range or
// other namespaces are asked for at once; paused it sends nothing and keeps its answer; active
// again after the interval it polls at once.
func TestTrendPollerCadence(t *testing.T) {
	clk, a, changes := newHistClock(), &trendAsker{reply: trendOf(1)}, &atomic.Int64{}
	p := newTestTrendPoller(t, clk, a, changes)
	month, week := trendKey{days: 30}, trendKey{days: 7, namespaces: "shop"}

	p.set(false, month)
	if len(a.asked()) != 0 {
		t.Fatal("it asked before it was active")
	}
	p.set(true, month)
	if w := clk.until(t, imageSecTrendInterval); len(a.asked()) != 1 || w.d != imageSecTrendInterval {
		t.Fatalf("first poll: %v", a.asked())
	}
	if snap := p.snapshot(); snap.key != month || snap.ans == nil || snap.ans.findings.Points[0].Critical != 1 || changes.Load() != 1 {
		t.Fatalf("snapshot %+v, %d changes", snap, changes.Load())
	}

	// Another curve: asked at once.
	p.set(true, week)
	clk.until(t, imageSecTrendInterval)
	if got := a.asked(); len(got) != 2 || got[1] != week {
		t.Fatalf("asks %v", got)
	}
	if p.snapshot().key != week || changes.Load() != 2 {
		t.Fatalf("snapshot %+v", p.snapshot())
	}

	// Paused: nothing is asked when the time comes, the answer stays.
	p.set(false, week)
	clk.passAll(time.Hour)
	p.set(false, week)
	if len(a.asked()) != 2 || p.snapshot().ans == nil {
		t.Fatalf("paused: %v", a.asked())
	}

	// Active again after the interval: at once; the same answer is no change.
	p.set(true, week)
	clk.until(t, imageSecTrendInterval)
	if len(a.asked()) != 3 || changes.Load() != 2 {
		t.Fatalf("asks %v, %d changes", a.asked(), changes.Load())
	}
}

// A failure keeps the answer for the same curve and says why; the retry backs off from 30 seconds.
// A Prometheus that is gone says so; an answer for another curve is not kept under the new one.
func TestTrendPollerFailures(t *testing.T) {
	clk, a, changes := newHistClock(), &trendAsker{reply: trendOf(4)}, &atomic.Int64{}
	p := newTestTrendPoller(t, clk, a, changes)
	month := trendKey{days: 30}
	p.set(true, month)
	w := clk.until(t, imageSecTrendInterval)
	a.set(func(trendKey) (trendAnswer, error) {
		return trendAnswer{}, errors.New("GET /api/v1/query_range: refused")
	})
	clk.pass(w)
	w = clk.next(t)
	if w.d != imageSecTrendFirstRetry {
		t.Fatalf("retry in %v", w.d)
	}
	if snap := p.snapshot(); snap.err == "" || snap.ans == nil || snap.ans.findings.Points[0].Critical != 4 {
		t.Fatalf("after the failure %+v", snap)
	}
	clk.pass(w)
	if w := clk.next(t); w.d != 2*imageSecTrendFirstRetry {
		t.Fatalf("second retry in %v", w.d)
	}

	p.set(true, trendKey{days: 90})
	clk.until(t, imageSecTrendFirstRetry)
	if snap := p.snapshot(); snap.key.days != 90 || snap.ans != nil || snap.err == "" {
		t.Fatalf("another curve's failure %+v", snap)
	}

	a.set(func(trendKey) (trendAnswer, error) { return trendAnswer{}, errNoPrometheus })
	p.set(true, month)
	clk.next(t)
	if snap := p.snapshot(); !snap.noProm || snap.ans != nil {
		t.Fatalf("no Prometheus %+v", snap)
	}
}

// trendBackend is a backend that may have a Prometheus, its poller holding snap.
func trendBackend(t *testing.T, snap *trendSnapshot) *Backend {
	t.Helper()
	b := &Backend{Context: "c", Metrics: &metrics.Hub{}, PrometheusDetector: &metrics.PrometheusDetector{}}
	if snap != nil {
		p := newTrendPoller(trendOptions{}, slog.New(slog.DiscardHandler), nil, nil)
		t.Cleanup(p.stop)
		p.snap = *snap
		b.secTrend.p = p
	}
	return b
}

// What the page shows of what the poller knows: nothing without a Prometheus or with an
// integration off; "asking" before an answer for the curve shown; the points, oldest first, with
// where they come from; a note when Prometheus does not scrape Trivy Operator, names no image here,
// or the last request failed (its points kept).
func TestImageSecurityTrendModel(t *testing.T) {
	s := &Session{}
	u := UIState{Context: "c", Namespaces: []string{"shop", "pay"}, ImageSecurity: ImageSecurityState{TrendDays: 7}}
	key := trendKey{days: 7, namespaces: "pay,shop"}
	if k := trendKeyOf(trendBackend(t, nil), u); k != key {
		t.Fatalf("key %+v", k)
	}
	ans := trendAnswer{source: "Prometheus (monitoring/prom)", findings: metrics.TrivyFindings{Scraped: true, AsOf: time.Unix(100, 0),
		Points: []metrics.TrivyFindingsPoint{{At: time.Unix(50, 0), Critical: 1, High: 2, Medium: 3, Low: 4, Unknown: 5}, {At: time.Unix(100, 0), Low: 1}}}}

	for _, tc := range []struct {
		name string
		snap *trendSnapshot
		u    func(*UIState)
		none bool
		note string
		pts  int
	}{
		{name: "no poller yet", note: noteTrendAsking},
		{name: "another curve's answer", snap: &trendSnapshot{key: trendKey{days: 30}, ans: &ans}, note: noteTrendAsking},
		{name: "an answer", snap: &trendSnapshot{key: key, ans: &ans}, pts: 2},
		{name: "a failure, the answer kept", snap: &trendSnapshot{key: key, ans: &ans, err: "refused"}, note: "refused", pts: 2},
		{name: "a failure", snap: &trendSnapshot{key: key, err: "refused"}, note: "refused"},
		{name: "not scraped", snap: &trendSnapshot{key: key, ans: &trendAnswer{source: "x"}}, note: noteTrendNotScraped},
		{name: "no image here", snap: &trendSnapshot{key: key, ans: &trendAnswer{source: "x", findings: metrics.TrivyFindings{Scraped: true}}}, note: noteTrendNoImages},
		{name: "no Prometheus", snap: &trendSnapshot{key: key, noProm: true}, none: true},
		{name: "Prometheus off", snap: &trendSnapshot{key: key, ans: &ans}, none: true, u: func(u *UIState) {
			off := IntPrometheus
			u.ClusterSettings = map[string]ClusterSettings{"c": {IntegrationsOff: &off}}
		}},
		{name: "Trivy off", snap: &trendSnapshot{key: key, ans: &ans}, none: true, u: func(u *UIState) {
			off := IntTrivy
			u.ClusterSettings = map[string]ClusterSettings{"c": {IntegrationsOff: &off}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := u
			if tc.u != nil {
				tc.u(&now)
			}
			got := s.imageSecurityTrend(trendBackend(t, tc.snap), now)
			if tc.none {
				if got != nil {
					t.Fatalf("a curve %+v", got)
				}
				return
			}
			if got == nil || got.Days != 7 || got.Note != tc.note || len(got.Points) != tc.pts {
				t.Fatalf("curve %+v", got)
			}
			if tc.pts > 0 && (got.Source != ans.source || !got.AsOf.Equal(time.Unix(100, 0)) ||
				got.Points[0] != (ImageSecurityTrendPoint{At: time.Unix(50, 0), Critical: 1, High: 2, Medium: 3, Low: 4, Unknown: 5})) {
				t.Fatalf("curve %+v", got)
			}
		})
	}
	// A cluster without a Prometheus to ask (a hub over a source of its own): none.
	if got := s.imageSecurityTrend(&Backend{Context: "c"}, u); got != nil {
		t.Fatalf("no Prometheus detection: %+v", got)
	}
}

// The demo cluster's curve is drawn, not asked: the same for the same range, namespaces and hour;
// a point per step ending now; falling toward now, of the magnitude of the demo's reports; a
// namespace filter takes a share of it.
func TestDemoImageSecurityTrend(t *testing.T) {
	now := time.Date(2026, 11, 23, 14, 37, 0, 0, time.UTC)
	s := &Session{opts: Options{Now: func() time.Time { return now }}}
	b := &Backend{Context: DemoContext, Demo: true}
	u := UIState{Context: DemoContext}
	month := s.imageSecurityTrend(b, u)
	if month == nil || month.Days != 30 || len(month.Points) != 30 || month.Source != demoTrendSource || month.Note != "" {
		t.Fatalf("curve %+v", month)
	}
	last, first := month.Points[29], month.Points[0]
	if !last.At.Equal(now.Truncate(time.Hour)) || !first.At.Equal(last.At.Add(-29*24*time.Hour)) {
		t.Fatalf("times %v .. %v", first.At, last.At)
	}
	total := func(p ImageSecurityTrendPoint) int { return p.Critical + p.High + p.Medium + p.Low + p.Unknown }
	if n := total(last); n < 6_000 || n > 7_200 {
		t.Errorf("now %d findings", n)
	}
	if total(first) <= total(last) || first.Critical <= last.Critical {
		t.Errorf("not falling: %+v .. %+v", first, last)
	}
	again, _ := json.Marshal(s.imageSecurityTrend(b, u))
	if was, _ := json.Marshal(month); string(again) != string(was) {
		t.Error("the curve is drawn otherwise the second time")
	}

	u.ImageSecurity.TrendDays = 7
	week := s.imageSecurityTrend(b, u)
	if len(week.Points) != 28 || week.Points[27].At.Sub(week.Points[26].At) != 6*time.Hour || week.Points[27] != last {
		t.Fatalf("week %+v", week.Points[26:])
	}
	u.ImageSecurity.TrendDays = 90
	if quarter := s.imageSecurityTrend(b, u); len(quarter.Points) != 90 || quarter.Points[89] != last {
		t.Fatalf("quarter %d points", len(quarter.Points))
	}

	u.ImageSecurity.TrendDays = 0
	u.Namespaces = []string{"demo-shop"}
	shop := s.imageSecurityTrend(b, u)
	if n := total(shop.Points[29]); n == 0 || n >= total(last)/5 {
		t.Errorf("one namespace: %d of %d", n, total(last))
	}
	off := IntPrometheus
	u.ClusterSettings = map[string]ClusterSettings{DemoContext: {IntegrationsOff: &off}}
	if got := s.imageSecurityTrend(b, u); got != nil {
		t.Errorf("the Prometheus integration off: %+v", got)
	}
}

// trendProm is a Prometheus behind the service proxy that answers the curve's query by the range's
// step and counts it.
type trendProm struct {
	ranges  atomic.Int64
	mu      sync.Mutex
	steps   []string
	queries []string
}

func (h *trendProm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query()
	switch r.URL.Path {
	case "/api/v1/status/buildinfo":
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.55.1","revision":"x"}}`))
		return
	case "/api/v1/query_range", "/api/v1/query":
	default:
		http.NotFound(w, r)
		return
	}
	kind := "vector"
	if r.URL.Path == "/api/v1/query_range" {
		kind = "matrix"
	}
	if !strings.Contains(q.Get("query"), "trivy_image_vulnerabilities") || kind == "vector" {
		// The metrics hub's own queries, and the scraped question: no series.
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"` + kind + `","result":[]}}`))
		return
	}
	h.ranges.Add(1)
	h.mu.Lock()
	h.steps = append(h.steps, q.Get("step"))
	h.queries = append(h.queries, q.Get("query"))
	h.mu.Unlock()
	end, _ := strconv.ParseFloat(q.Get("end"), 64)
	result := []any{map[string]any{"metric": map[string]string{"severity": "Critical"}, "values": [][]any{{end, "3"}}}}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": result}})
}

func (h *trendProm) last() (step, query string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.steps[len(h.steps)-1], h.queries[len(h.queries)-1]
}

// The curve end to end: Prometheus is asked only while the Image Security page is the shown view,
// again at once for another range or other namespaces, never while the page is closed; the
// integration off ends the poller and the curve.
func TestImageSecurityTrendOnThePage(t *testing.T) {
	all := []testkit.FakeRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"": all}}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.ServicesGVR, promService("monitoring", "kube-prometheus-prometheus"))
	prom := &trendProm{}
	srv.SetProxy("monitoring", "kube-prometheus-prometheus:http-web", prom)
	ms, err := NewMetricsSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Connector:        &ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}, MetricsSettings: ms},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: NewClusterSettingsStore(""),
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "fake")
	mustConnect(t, s, "fake")
	b := s.BackendOf("fake")
	trend := func() *ImageSecurityTrend { return s.imageSecurityTrend(b, s.UIState()) }
	waitFor(t, "the poller", func() bool { return b.secTrend.current() != nil })
	if p := b.secTrend.current(); p.started || prom.ranges.Load() != 0 {
		t.Fatal("Prometheus was asked before the page was shown")
	}
	if tr := trend(); tr == nil || tr.Note != noteTrendAsking {
		t.Fatalf("before the page: %+v", tr)
	}

	s.UpdateUI(func(u *UIState) { u.View = ImageSecurity })
	waitFor(t, "the month's curve", func() bool { tr := trend(); return tr != nil && len(tr.Points) == 1 })
	if tr := trend(); tr.Days != 30 || tr.Points[0].Critical != 3 || tr.Note != "" || !strings.Contains(tr.Source, "monitoring/kube-prometheus-prometheus") {
		t.Fatalf("curve %+v", tr)
	}
	if step, query := prom.last(); step != "86400" || strings.Contains(query, "namespace") || prom.ranges.Load() != 1 {
		t.Fatalf("step %s, query %s, %d asks", step, query, prom.ranges.Load())
	}

	// Another range and a namespace filter: asked at once.
	s.UpdateUI(func(u *UIState) { u.ImageSecurity.TrendDays = 7; u.Namespaces = []string{"shop"} })
	waitFor(t, "the week's curve", func() bool { tr := trend(); return tr != nil && tr.Days == 7 && len(tr.Points) == 1 })
	if step, query := prom.last(); step != "21600" || !strings.Contains(query, `namespace=~"(shop)"`) {
		t.Fatalf("step %s, query %s", step, query)
	}

	// Closed: paused, nothing more is asked.
	asks := prom.ranges.Load()
	s.UpdateUI(func(u *UIState) { u.View = "pods" })
	p := b.secTrend.current()
	waitFor(t, "the poller to pause", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return !p.active })
	s.UpdateUI(func(u *UIState) { u.ImageSecurity.TrendDays = 90 })
	if prom.ranges.Load() != asks {
		t.Fatal("Prometheus was asked while the page was closed")
	}

	// The Prometheus integration off: no curve, the poller ends.
	if _, err := s.ChangeClusterSettings("fake", func(cs *ClusterSettings) error {
		off := IntPrometheus
		cs.IntegrationsOff = &off
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the poller to end", func() bool { return b.secTrend.current() == nil })
	if tr := trend(); tr != nil {
		t.Fatalf("curve with the integration off: %+v", tr)
	}
}
