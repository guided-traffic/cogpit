package metrics_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// alertProm detects the fake Prometheus and returns its provider.
func alertProm(t *testing.T, c *promCluster) *metrics.Prometheus {
	t.Helper()
	prov, ok := promProvider(t, c, metrics.PrometheusOptions{}).(*metrics.Prometheus)
	if !ok {
		t.Fatal("the detected provider is not a *Prometheus")
	}
	return prov
}

func alertSeries(alertname, severity string, vals ...float64) promTestSeries {
	return s(map[string]string{"alertname": alertname, "severity": severity}, vals...)
}

// One range query of 24 hourly points ending now and one instant query answer the board's history:
// the onsets per rule by the hour they fall in, the firing rules, and nothing else is asked.
func TestAlertOnsets(t *testing.T) {
	c := newPromCluster(t, false)
	// The times are seconds before the query's end: -82800 is the first hour, 0 the last.
	c.prom.onRel("count_over_time",
		alertSeries("KubePodCrashLooping", "warning", -82800, 2, -3600, 1, 0, 3),
		alertSeries("KubePodCrashLooping", "major", -3600, 4),
		alertSeries("Quiet", "info", -7200, 0),
		s(map[string]string{"severity": "warning"}, 0, 5), // no alertname: left out
	)
	c.prom.onRel("count by (alertname", alertSeries("KubePodCrashLooping", "warning", 0, 2), alertSeries("Watchdog", "none", 0, 1))
	prov := alertProm(t, c)
	before := len(c.prom.requests())

	now := time.Unix(1_800_000_000, 0).Add(37 * time.Second)
	got, err := prov.AlertOnsets(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	crash := metrics.AlertKey{Alertname: "KubePodCrashLooping", Severity: "warning"}
	h := got.Hours[crash]
	if h[0] != 2 || h[22] != 1 || h[23] != 3 || h[1] != 0 {
		t.Errorf("hours %v", h)
	}
	if major := got.Hours[metrics.AlertKey{Alertname: "KubePodCrashLooping", Severity: "major"}]; major[22] != 4 {
		t.Errorf("the raw severity label is kept apart: %v", major)
	}
	if quiet := got.Hours[metrics.AlertKey{Alertname: "Quiet", Severity: "info"}]; quiet != [metrics.AlertHistoryHours]int{} {
		t.Errorf("a zero sample counts nothing: %v", quiet)
	}
	if len(got.Hours) != 3 {
		t.Errorf("series: %v", got.Hours)
	}
	if !got.Firing[crash] || !got.Firing[metrics.AlertKey{Alertname: "Watchdog", Severity: "none"}] || len(got.Firing) != 2 {
		t.Errorf("firing %v", got.Firing)
	}
	if want := now.Truncate(time.Minute); !got.AsOf.Equal(want) {
		t.Errorf("as of %v, want %v", got.AsOf, want)
	}

	reqs := c.prom.requests()[before:]
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2: %+v", len(reqs), reqs)
	}
	for _, r := range reqs {
		switch r.path {
		case "/api/v1/query_range":
			end := now.Truncate(time.Minute)
			if r.end != strconv.FormatInt(end.Unix(), 10) || r.start != strconv.FormatInt(end.Add(-23*time.Hour).Unix(), 10) || r.step != "3600" {
				t.Errorf("range %s..%s step %s", r.start, r.end, r.step)
			}
			if !strings.Contains(r.query, `unless ALERTS{alertstate="firing"} offset 1m)[1h:1m]`) || !strings.HasPrefix(r.query, "sum by (alertname, severity)") {
				t.Errorf("onsets query %q", r.query)
			}
		case "/api/v1/query":
			if r.query != `count by (alertname, severity) (ALERTS{alertstate="firing"})` {
				t.Errorf("firing query %q", r.query)
			}
		default:
			t.Errorf("request %s", r.path)
		}
	}
}

// A failure of either query is the answer's: no half history.
func TestAlertOnsetsFailure(t *testing.T) {
	c := newPromCluster(t, false)
	prov := alertProm(t, c)
	c.prom.mu.Lock()
	c.prom.status = 503
	c.prom.mu.Unlock()
	if _, err := prov.AlertOnsets(context.Background(), time.Now()); err == nil {
		t.Fatal("a Prometheus that refuses answered")
	}
}

// The hub hands out the Prometheus it detected, and detection runs once: the history never looks
// for a Prometheus on its own.
func TestHubPrometheusUsesTheDetection(t *testing.T) {
	c := newPromCluster(t, false)
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	hub := metrics.NewHubFor(metrics.Sources{History: det}, metrics.Options{})
	t.Cleanup(hub.Close)
	for range 3 {
		prov, st := hub.Prometheus(context.Background())
		if prov == nil || !st.Usable() {
			t.Fatalf("no Prometheus: %+v", st)
		}
	}
	probes := 0
	for _, r := range c.prom.requests() {
		if strings.HasSuffix(r.path, "/api/v1/status/buildinfo") {
			probes++
		}
	}
	if probes != 1 {
		t.Errorf("%d buildinfo probes, want 1", probes)
	}
}

// Without a Prometheus the hub says so, and a source that turned it off says that.
func TestHubPrometheusMissing(t *testing.T) {
	c := newPromCluster(t, false)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(), metrics.PrometheusOptions{})
	hub := metrics.NewHubFor(metrics.Sources{History: det}, metrics.Options{})
	t.Cleanup(hub.Close)
	if prov, st := hub.Prometheus(context.Background()); prov != nil || st.Status != metrics.StatusNotServed {
		t.Errorf("found %v: %+v", prov, st)
	}
	hub.SetSource(metrics.SourceNone)
	if prov, st := hub.Prometheus(context.Background()); prov != nil || !strings.Contains(st.Message, "off") {
		t.Errorf("turned off: %v %+v", prov, st)
	}
}
