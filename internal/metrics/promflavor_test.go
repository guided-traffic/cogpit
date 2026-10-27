package metrics_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// Thanos's Querier and query frontend and VictoriaMetrics (single-node, the cluster's vmselect)
// are found by their Services' names as the charts and operators name them, each under its own
// name and vmselect under its default tenant's path; their store gateways, compactors, gRPC
// Services and the other components are never candidates; a Prometheus by its own names ranks
// first (D-71). Synthetic names, shaped like the charts' and a real cluster's.
func TestCandidatesThanosAndVictoriaMetrics(t *testing.T) {
	p := func(name string, port int32) metrics.ServicePort { return metrics.ServicePort{Name: name, Port: port} }
	svc := func(ns, name string, ports ...metrics.ServicePort) metrics.Service {
		return metrics.Service{Namespace: ns, Name: name, Ports: ports}
	}
	cases := []struct {
		svc    metrics.Service
		flavor metrics.Flavor
		prefix string
		score  int
	}{
		{svc("monitoring", "thanos-query", p("http", 9090)), metrics.FlavorThanos, "", 76},                                               // kube-thanos, Bitnami's release "thanos"
		{svc("obs", "obs-thanos-query", p("http", 9090), p("grpc", 10901)), metrics.FlavorThanos, "", 76},                                // Bitnami's chart
		{svc("openshift-monitoring", "thanos-querier", p("web", 9091), p("tenancy", 9092)), metrics.FlavorThanos, "", 76},                // a Thanos operator's, OpenShift's
		{svc("monitoring", "thanos-query-frontend", p("http", 9090)), metrics.FlavorThanos, "", 75},                                      // kube-thanos
		{svc("obs", "obs-thanos-query-frontend", p("http", 9090)), metrics.FlavorThanos, "", 75},                                         // Bitnami's chart
		{svc("vm", "vmsingle-main", p("http", 8428)), metrics.FlavorVictoriaMetrics, "", 74},                                             // the operator's VMSingle
		{svc("vm", "vm-victoria-metrics-single-server", p("http", 8428)), metrics.FlavorVictoriaMetrics, "", 74},                         // the chart
		{svc("vm", "vmselect-main", p("http", 8481)), metrics.FlavorVictoriaMetrics, metrics.VMSelectPrefix, 73},                         // the operator's VMCluster
		{svc("vm", "vmc-victoria-metrics-cluster-vmselect", p("http", 8481)), metrics.FlavorVictoriaMetrics, metrics.VMSelectPrefix, 73}, // the chart
	}
	for _, c := range cases {
		got := metrics.Candidates([]metrics.Service{c.svc})
		if len(got) != 1 || got[0].Score != c.score || got[0].Target.Flavor != c.flavor || got[0].Target.Prefix != c.prefix {
			t.Errorf("%s/%s: %+v, want score %d, flavor %q, prefix %q", c.svc.Namespace, c.svc.Name, got, c.score, c.flavor, c.prefix)
		}
	}
	for _, name := range []string{"thanos-query-grpc", "obs-thanos-storegateway", "thanos-compactor", "thanos-bucketweb", "thanos-ruler",
		"thanos-receive", "kube-prometheus-thanos-discovery", "vminsert-main", "vmstorage-main", "vmagent-main", "vmalert-main", "vmauth-main"} {
		if got := metrics.Candidates([]metrics.Service{svc("monitoring", name, p("http", 9090))}); len(got) != 0 {
			t.Errorf("%s is a candidate: %+v", name, got)
		}
	}

	// A cluster that runs both, as one the owner provides for tests does (kube-prometheus with a
	// Thanos sidecar, Bitnami's Thanos beside it): the Prometheus first, the Querier and the
	// frontend after the Prometheus Operator's governing service.
	got := metrics.Candidates([]metrics.Service{
		svc("monitoring", "thanos-query-frontend", p("http", 9090)),
		svc("monitoring", "thanos-query", p("http", 9090)),
		svc("monitoring", "thanos-query-grpc", p("grpc", 10901)),
		svc("monitoring", "thanos-storegateway", p("http", 9090), p("grpc", 10901)),
		svc("monitoring", "prometheus-operated", p("http-web", 9090)),
		svc("monitoring", "kube-prometheus-prometheus", p("http-web", 9090), p("reloader-web", 8080), p("thanos-sidecar", 10901)),
	})
	var names []string
	for _, c := range got {
		names = append(names, c.Target.Service+":"+c.Target.Port)
	}
	if want := []string{"kube-prometheus-prometheus:9090", "prometheus-operated:9090", "thanos-query:9090", "thanos-query-frontend:9090"}; !slices.Equal(names, want) {
		t.Errorf("candidates %v, want %v", names, want)
	}
}

// Found through the service proxy, a Thanos and a VictoriaMetrics are named so in the source line;
// vmselect is asked under /select/0/prometheus; an override is classified by its name and its
// path; the status line names the flavor.
func TestDetectThanosAndVictoriaMetrics(t *testing.T) {
	c := newPromCluster(t, false)
	c.srv.SetProxy("monitoring", "thanos-query:9090", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "thanos-query", 9090), web("monitoring", "thanos-storegateway", 9090)), metrics.PrometheusOptions{})
	prov, st := det.Detect(context.Background())
	if prov == nil || st.Label != "Thanos (monitoring/thanos-query)" || st.Provider != metrics.PrometheusID {
		t.Fatalf("Thanos: state %+v", st)
	}
	if l, ok := prov.(metrics.Located); !ok || l.Where() != "monitoring/thanos-query:9090" {
		t.Errorf("where %v", prov)
	}
	if n, ok := prov.(metrics.Named); !ok || n.Name() != "Thanos" {
		t.Errorf("name %v", prov)
	}

	vm := newPromCluster(t, false)
	vm.prom.prefix = metrics.VMSelectPrefix
	vm.srv.SetProxy("vm", "vmselect-main:8481", vm.prom)
	det = metrics.NewPrometheusDetector(vm.conn.Kube(), servicesOf(web("vm", "vmselect-main", 8481), web("vm", "vminsert-main", 8480)), metrics.PrometheusOptions{})
	prov, st = det.Detect(context.Background())
	if prov == nil || st.Label != "VictoriaMetrics (vm/vmselect-main)" {
		t.Fatalf("vmselect: state %+v", st)
	}
	if reqs := vm.prom.requests(); len(reqs) != 1 || reqs[0].path != metrics.VMSelectPrefix+"/api/v1/status/buildinfo" {
		t.Errorf("vmselect requests %+v", reqs)
	}
	if l := prov.(metrics.Located).Where(); l != "vm/vmselect-main:8481/select/0/prometheus" {
		t.Errorf("vmselect where %q", l)
	}

	// The user's override: by its name, a tenant's path, else Prometheus.
	for _, o := range []struct {
		ov   metrics.Override
		want string
	}{
		{metrics.Override{Namespace: "vm", Service: "vmselect-main", Port: "http", Prefix: "/select/7/prometheus"}, "VictoriaMetrics (vm/vmselect-main)"},
		{metrics.Override{Namespace: "vm", Service: "metrics", Port: "8481", Prefix: "/select/0:1/prometheus"}, "VictoriaMetrics (vm/metrics)"},
		{metrics.Override{Namespace: "obs", Service: "obs-thanos-query-frontend", Port: "http"}, "Thanos (obs/obs-thanos-query-frontend)"},
		{metrics.Override{Namespace: "obs", Service: "metrics", Port: "9090", Prefix: "/prometheus"}, "Prometheus (obs/metrics)"},
	} {
		oc := newPromCluster(t, false)
		oc.prom.prefix = strings.TrimSuffix(o.ov.Prefix, "/")
		oc.srv.SetProxy(o.ov.Namespace, o.ov.Service+":"+o.ov.Port, oc.prom)
		d := metrics.NewPrometheusDetector(oc.conn.Kube(), nil, metrics.PrometheusOptions{})
		d.SetOverride(&o.ov)
		if _, st := d.Detect(context.Background()); st.Label != o.want {
			t.Errorf("override %+v: %+v, want %q", o.ov, st, o.want)
		}
	}
}
