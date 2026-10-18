package alerting

import (
	"slices"
	"testing"
)

func svc(ns, name string, ports ...ServicePort) Service {
	return Service{Namespace: ns, Name: name, Ports: ports}
}

// Candidates: the charts' and operators' names ranked (kube-prometheus-stack first, the operator's
// headless service after it), the HTTP port picked over the gossip and reloader ports, names that
// only contain "alertmanager" or end otherwise never tried.
func TestCandidates(t *testing.T) {
	web := ServicePort{Name: "web", Port: 9093}
	got := Candidates([]Service{
		svc("monitoring", "alertmanager-operated", web, ServicePort{Name: "tcp-mesh", Port: 9094}, ServicePort{Name: "udp-mesh", Port: 9094}),
		svc("monitoring", "kube-prometheus-alertmanager", ServicePort{Name: "reloader-web", Port: 8080}, ServicePort{Name: "http-web", Port: 9093}),
		svc("observability", "alertmanager-main", web),
		svc("vm", "vmalertmanager-main", ServicePort{Name: "http", Port: 9093}),
		svc("alerts", "prom-alertmanager", ServicePort{Name: "", Port: 9093}),
		svc("monitoring", "alertmanager", ServicePort{Port: 80}),
		svc("team-a", "alertmanager", ServicePort{Port: 8080}),
		svc("monitoring", "karma", ServicePort{Port: 8080}),
		svc("monitoring", "alertmanager-webhook-logger", ServicePort{Port: 8080}),
		svc("monitoring", "alertmanager-noport"),
	})
	var where []string
	for _, c := range got {
		where = append(where, c.Target.Where())
	}
	// The port by its number first, its name after (Candidate.Ports): the roles written for Lens
	// grant the service proxy by "<service>:<number>".
	want := []string{
		"monitoring/kube-prometheus-alertmanager:9093",
		"observability/alertmanager-main:9093",
		"monitoring/alertmanager-operated:9093",
		"vm/vmalertmanager-main:9093",
		"alerts/prom-alertmanager:9093",
		"monitoring/alertmanager:80",
		"team-a/alertmanager:8080",
	}
	if !slices.Equal(where, want) {
		t.Errorf("candidates\n got %q\nwant %q", where, want)
	}
	if !slices.Equal(got[0].Ports, []string{"9093", "http-web"}) || !slices.Equal(got[4].Ports, []string{"9093"}) {
		t.Errorf("the spellings of the ports: %q, %q", got[0].Ports, got[4].Ports)
	}
	for _, name := range []string{"kube-prometheus-stack-alertmanager", "kps-kube-prometheus-stack-alertmanager", "monitoring-kube-prometheus-alertmanager"} {
		if scoreName("x", name) != 100 {
			t.Errorf("%s scores %d", name, scoreName("x", name))
		}
	}
}

func TestTargetWhere(t *testing.T) {
	tg := TargetOf(Address{Namespace: "monitoring", Service: "am", Port: "9093", Prefix: "/alertmanager/", HTTPS: true})
	if got := tg.Where(); got != "monitoring/am:9093/alertmanager (https)" {
		t.Errorf("Where = %q", got)
	}
	if (Address{Namespace: "a", Service: "b"}).Valid() {
		t.Error("an address without a port is valid")
	}
}
