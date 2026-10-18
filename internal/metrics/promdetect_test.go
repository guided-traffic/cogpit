package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

func TestCandidatesRankKnownServices(t *testing.T) {
	svc := func(ns, name string, ports ...metrics.ServicePort) metrics.Service {
		return metrics.Service{Namespace: ns, Name: name, Ports: ports}
	}
	p := func(name string, port int32) metrics.ServicePort { return metrics.ServicePort{Name: name, Port: port} }
	got := metrics.Candidates([]metrics.Service{
		svc("default", "kubernetes", p("https", 443)),
		svc("monitoring", "prometheus-alertmanager", p("http", 9093)),
		svc("monitoring", "prometheus-node-exporter", p("metrics", 9100)),
		svc("monitoring", "prometheus-server", p("http", 80)),
		svc("monitoring", "prometheus-operated", p("web", 9090)),
		svc("monitoring", "stack-kube-prometheus-prometheus", p("reloader-web", 8080), p("http-web", 9090)),
		svc("monitoring", "stack-kube-prometheus-operator", p("https", 443)),
		svc("apps", "prometheus", p("", 9090)),
		svc("lens-metrics", "prometheus", p("web", 80)),
		svc("monitoring", "prometheus-k8s", p("web", 9090)),
		svc("monitoring", "thanos-query", p("http", 9090)),
		svc("monitoring", "prometheus-pushgateway", p("http", 9091)),
		svc("x", "prometheus-server-no-ports"),
	})
	var names []string
	for _, c := range got {
		names = append(names, c.Target.String()+":"+c.Target.Port)
	}
	// The port by its number first, as Lens writes it, its name after (Candidate.Ports).
	want := []string{
		"monitoring/stack-kube-prometheus-prometheus:9090",
		"monitoring/prometheus-k8s:9090",
		"monitoring/prometheus-operated:9090",
		"monitoring/prometheus-server:80",
		"monitoring/thanos-query:9090", // since 2026-10-16 night, Thanos by its name (D-71)
		"lens-metrics/prometheus:80",
		"apps/prometheus:9090",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("candidates:\n got %v\nwant %v", names, want)
	}
	if !slices.Equal(got[0].Ports, []string{"9090", "http-web"}) || !slices.Equal(got[len(got)-1].Ports, []string{"9090"}) {
		t.Errorf("the spellings of the ports: %q, %q", got[0].Ports, got[len(got)-1].Ports)
	}

	// kube-prometheus-stack's names by release: the release alone when it contains the chart's
	// name, else <release>-kube-prometheus-stack cut to 26 characters, so a short release keeps
	// "-stack" (kps, 2026-10-12) and a longer one loses it.
	for _, name := range []string{"kube-prometheus-stack-prometheus", "kps-kube-prometheus-stack-prometheus", "monitoring-kube-prometheus-prometheus"} {
		if c := metrics.Candidates([]metrics.Service{svc("cogpit-kps", name, p("http-web", 9090))}); len(c) != 1 || c[0].Score != 100 {
			t.Errorf("%s: candidates %+v, want it first", name, c)
		}
	}
	if c := metrics.Candidates([]metrics.Service{svc("x", "kube-prometheus-stack-operator", p("https", 443))}); len(c) != 0 {
		t.Errorf("the operator's service is a candidate: %+v", c)
	}
}

func TestDetectFoundThroughTheServiceProxy(t *testing.T) {
	c := newPromCluster(t, false)
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80), web("default", "web", 80)), metrics.PrometheusOptions{})

	prov, st := det.Detect(context.Background())
	if prov == nil || !st.Usable() || st.Provider != "prometheus" {
		t.Fatalf("state = %+v", st)
	}
	if st.Label != "Prometheus (monitoring/prometheus-server)" {
		t.Errorf("label = %q", st.Label)
	}
	if caps := prov.Capabilities(); !caps.History || !caps.Network || !caps.Filesystem {
		t.Errorf("capabilities = %+v", caps)
	}
	if n := c.proxyCount(); n != 1 {
		t.Errorf("%d service proxy requests, want 1 buildinfo probe", n)
	}
	if reqs := c.prom.requests(); len(reqs) != 1 || reqs[0].path != "/api/v1/status/buildinfo" {
		t.Errorf("requests = %+v", reqs)
	}
	if n := c.srv.Requests()["list services"]; n != 0 {
		t.Errorf("detection listed services itself (%d)", n)
	}
}

func TestDetectNotFoundCostsNothing(t *testing.T) {
	c := newPromCluster(t, false)
	for name, svcs := range map[string][]metrics.Service{
		"no services":  nil,
		"no candidate": {web("default", "web", 80), web("monitoring", "prometheus-alertmanager", 9093)},
	} {
		det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(svcs...), metrics.PrometheusOptions{})
		prov, st := det.Detect(context.Background())
		if prov != nil || st.Status != metrics.StatusNotServed {
			t.Errorf("%s: state = %+v", name, st)
		}
	}
	if n := c.proxyCount(); n != 0 {
		t.Errorf("%d proxy requests with no candidate", n)
	}
}

func TestDetectCandidateThatIsNotPrometheus(t *testing.T) {
	c := newPromCluster(t, false)
	c.srv.SetProxy("monitoring", "prometheus-server:80", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>hello</html>"))
	}))
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov != nil || st.Status != metrics.StatusNotServed {
		t.Errorf("state = %+v, want notServed", st)
	}
}

// TestDetectForbiddenIsAState: when the service proxy refuses every spelling of every candidate,
// detection says forbidden and names each target it was refused; a refusal stops nothing, so
// every candidate's spellings are asked (2 candidates × 2 spellings).
func TestDetectForbiddenIsAState(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.status = http.StatusForbidden
	for _, name := range []string{"prometheus-server:80", "prometheus-server:http", "prometheus-operated:9090", "prometheus-operated:http"} {
		c.srv.SetProxy("monitoring", name, c.prom)
	}
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80), web("monitoring", "prometheus-operated", 9090)), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov != nil || st.Status != metrics.StatusForbidden || !strings.Contains(st.Message, "services/proxy") ||
		!strings.Contains(st.Message, "monitoring/prometheus-operated:9090 nor monitoring/prometheus-operated:http") {
		t.Fatalf("state = %+v", st)
	}
	if n := c.proxyCount(); n != 4 {
		t.Errorf("%d probes, want 4: each spelling of each candidate", n)
	}
}

// refusing answers every request with a Kubernetes 403, as an RBAC rule by resourceNames refuses a
// spelling of the proxy's name it does not list.
var refusing = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,` +
		`"message":"services \"x\" is forbidden: cannot get resource \"services/proxy\""}`))
})

// TestDetectTriesThePortsNumberThenName (the owner's mgmt-p, 2026-10-18): a role that grants the
// service proxy by resourceNames matches the name as written. Where only "<svc>:9090" is granted
// (as the roles written for Lens name it), the number, asked first, is found at once; where only
// "<svc>:http-web" is, the number is refused and the name found next.
func TestDetectTriesThePortsNumberThenName(t *testing.T) {
	svc := metrics.Service{Namespace: "monitoring", Name: "kube-prometheus-prometheus", Ports: []metrics.ServicePort{{Name: "reloader-web", Port: 8080}, {Name: "http-web", Port: 9090}}}
	for _, tc := range []struct {
		granted, refused string
		probes           int
	}{
		{"kube-prometheus-prometheus:9090", "kube-prometheus-prometheus:http-web", 1},
		{"kube-prometheus-prometheus:http-web", "kube-prometheus-prometheus:9090", 2},
	} {
		c := newPromCluster(t, false)
		c.srv.SetProxy("monitoring", tc.granted, c.prom)
		c.srv.SetProxy("monitoring", tc.refused, refusing)
		det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(svc), metrics.PrometheusOptions{})
		prov, st := det.Detect(context.Background())
		if prov == nil || st.Status != metrics.StatusAvailable {
			t.Fatalf("granted %s: state = %+v", tc.granted, st)
		}
		if n := c.proxyCount(); n != int64(tc.probes) {
			t.Errorf("granted %s: %d probes, want %d", tc.granted, n, tc.probes)
		}
	}
}

// TestDetectARefusedCandidateDoesNotStopTheNext: the best candidate refused under both spellings,
// the next one is asked and found (before 2026-10-18 the first 403 ended detection).
func TestDetectARefusedCandidateDoesNotStopTheNext(t *testing.T) {
	c := newPromCluster(t, false)
	c.srv.SetProxy("monitoring", "kube-prometheus-prometheus:9090", refusing)
	c.srv.SetProxy("monitoring", "kube-prometheus-prometheus:http", refusing)
	c.srv.SetProxy("monitoring", "prometheus-operated:9090", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "kube-prometheus-prometheus", 9090), web("monitoring", "prometheus-operated", 9090)), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov == nil || st.Status != metrics.StatusAvailable {
		t.Fatalf("state = %+v", st)
	}
}

func TestDetectUnreachableIsUnavailableAndTriesTheNext(t *testing.T) {
	c := newPromCluster(t, false)
	// prometheus-operated has no endpoints (no handler: 503); prometheus-server works.
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-operated", 9090), web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov == nil || !st.Usable() || !strings.Contains(st.Label, "prometheus-server") {
		t.Fatalf("state = %+v", st)
	}
	if n := c.proxyCount(); n != 2 {
		t.Errorf("%d probes, want 2 (the dead one, then the live one)", n)
	}

	c2 := newPromCluster(t, false)
	det = metrics.NewPrometheusDetector(c2.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	prov, st = det.Detect(context.Background())
	if prov != nil || st.Status != metrics.StatusUnavailable || !strings.Contains(st.Message, "prometheus-server") {
		t.Errorf("only a dead candidate: state = %+v", st)
	}
}

func TestDetectBoundsTheProbes(t *testing.T) {
	c := newPromCluster(t, false)
	var svcs []metrics.Service
	for _, ns := range []string{"a", "b", "c", "d", "e"} {
		svcs = append(svcs, web(ns, "prometheus-server", 80))
	}
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(svcs...), metrics.PrometheusOptions{})
	_, st := det.Detect(context.Background())
	if st.Status != metrics.StatusUnavailable {
		t.Errorf("state = %+v", st)
	}
	if n := c.proxyCount(); n != metrics.MaxProbes {
		t.Errorf("%d probes, want at most %d", n, metrics.MaxProbes)
	}
}

func TestDetectOverride(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.prefix = "/prom"
	c.srv.SetProxy("obs", "thanos:9090", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	det.SetOverride(&metrics.Override{Namespace: "obs", Service: "thanos", Port: "9090", Prefix: "/prom/"})
	prov, st := det.Detect(context.Background())
	if prov == nil || st.Label != "Prometheus (obs/thanos)" {
		t.Fatalf("state = %+v", st)
	}
	if n := c.proxyCount(); n != 1 {
		t.Errorf("%d requests: an override is probed alone", n)
	}
	if reqs := c.prom.requests(); len(reqs) != 1 || reqs[0].path != "/prom/api/v1/status/buildinfo" {
		t.Errorf("requests = %+v", reqs)
	}
	det.SetOverride(&metrics.Override{Namespace: "obs"}) // not valid: cleared, detection is back
	if prov, _ := det.Detect(context.Background()); prov != nil {
		t.Error("an invalid override still applied")
	}
}

func TestDetectServicesFailureIsAState(t *testing.T) {
	c := newPromCluster(t, false)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), metrics.ServiceSourceFunc(func(context.Context) ([]metrics.Service, error) {
		return nil, errors.New("services are not synced")
	}), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov != nil || st.Status != metrics.StatusUnavailable {
		t.Errorf("state = %+v", st)
	}
}

func TestChainPrefersPrometheusAndFallsBackToTheMetricsServer(t *testing.T) {
	t.Run("prometheus found", func(t *testing.T) {
		c := newPromCluster(t, true)
		c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
		det, _, err := metrics.ChainForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
		if err != nil {
			t.Fatal(err)
		}
		prov, st := det.Detect(context.Background())
		if prov == nil || prov.ID() != "prometheus" || !st.Usable() {
			t.Errorf("state = %+v", st)
		}
		if n := c.conn.Counter().Snapshot().ByVerbResource["discovery"]; n > 4 {
			t.Logf("discovery requests: %d", n) // the Metrics Server is not asked once Prometheus answers
		}
	})
	t.Run("not found falls back", func(t *testing.T) {
		c := newPromCluster(t, true)
		det, _, err := metrics.ChainForConn(c.conn, servicesOf(), metrics.PrometheusOptions{})
		if err != nil {
			t.Fatal(err)
		}
		prov, st := det.Detect(context.Background())
		if prov == nil || prov.ID() != "metrics-server" || !st.Usable() {
			t.Errorf("state = %+v", st)
		}
		if n := c.proxyCount(); n != 0 {
			t.Errorf("%d proxy requests without a candidate", n)
		}
	})
	t.Run("forbidden falls back", func(t *testing.T) {
		c := newPromCluster(t, true)
		c.prom.status = http.StatusForbidden
		c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
		det, _, _ := metrics.ChainForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
		prov, st := det.Detect(context.Background())
		if prov == nil || prov.ID() != "metrics-server" || !st.Usable() {
			t.Errorf("state = %+v", st)
		}
	})
	t.Run("neither: the telling state wins", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.status = http.StatusForbidden
		c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
		det, _, _ := metrics.ChainForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
		prov, st := det.Detect(context.Background())
		if prov != nil || st.Status != metrics.StatusForbidden {
			t.Errorf("state = %+v, want the forbidden proxy, not 'no metrics-server'", st)
		}
	})
	t.Run("neither and nothing wrong", func(t *testing.T) {
		c := newPromCluster(t, false)
		det, _, _ := metrics.ChainForConn(c.conn, servicesOf(), metrics.PrometheusOptions{})
		prov, st := det.Detect(context.Background())
		if prov != nil || st.Status != metrics.StatusNotServed {
			t.Errorf("state = %+v", st)
		}
	})
}
