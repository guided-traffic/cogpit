package alerting

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Service is what detection needs to know about a cached Service: where it is and which ports it
// has. The engine fills it from the cache's Services; no request is made for it.
type Service struct {
	Namespace string
	Name      string
	Ports     []ServicePort
}

// ServicePort is one port of a Service.
type ServicePort struct {
	Name string
	Port int32
}

// ServiceSource lists the cluster's Services, from the cache: detection never lists Services
// itself. An error is a state (unavailable), not a failure.
type ServiceSource interface {
	Services(ctx context.Context) ([]Service, error)
}

// ServiceSourceFunc adapts a function to a ServiceSource.
type ServiceSourceFunc func(ctx context.Context) ([]Service, error)

// Services calls f.
func (f ServiceSourceFunc) Services(ctx context.Context) ([]Service, error) { return f(ctx) }

// Address is the Alertmanager the user names for a cluster instead of detection (Cluster Settings ›
// Integrations, cluster.setAlertmanager): a Service, its port by number or name, the path
// Alertmanager serves under (its --web.route-prefix) and whether the service proxy asks it over
// TLS. Kept in Cogpit's own settings, never in the kubeconfig or the cluster.
type Address struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      string `json:"port"`
	Prefix    string `json:"prefix,omitempty"`
	HTTPS     bool   `json:"https,omitempty"`
}

// Valid reports whether the address names a service and a port.
func (a Address) Valid() bool { return a.Namespace != "" && a.Service != "" && a.Port != "" }

// Target is an Alertmanager reached through the API server's service proxy:
// /api/v1/namespaces/<Namespace>/services/<Scheme>:<Service>:<Port>/proxy<Prefix>/api/v2/alerts.
type Target struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	// Port is the service port's number or name.
	Port string `json:"port"`
	// Prefix is the route prefix, "" for the root.
	Prefix string `json:"prefix,omitempty"`
	// Scheme is the service proxy's scheme: "https" asks the service over TLS, "" over http.
	Scheme string `json:"scheme,omitempty"`
}

// TargetOf is the target an address names.
func TargetOf(a Address) Target {
	t := Target{Namespace: a.Namespace, Service: a.Service, Port: a.Port, Prefix: a.Prefix}
	if a.HTTPS {
		t.Scheme = "https"
	}
	return t
}

// Where is "namespace/service:port" with the route prefix if one is set and " (https)" when the
// service is asked over TLS, as the settings and the board name it.
func (t Target) Where() string {
	w := t.Namespace + "/" + t.Service
	if t.Port != "" {
		w += ":" + t.Port
	}
	w += t.prefix()
	if t.Scheme == "https" {
		w += " (https)"
	}
	return w
}

func (t Target) prefix() string {
	p := strings.Trim(t.Prefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// Candidate is a Service that may be an Alertmanager, with the rank detection gave it.
type Candidate struct {
	Target Target
	Score  int
	// Ports are the spellings of the target's port the service proxy is asked with, best first:
	// its number, then its name, as Prometheus' detection asks them (metrics.Candidate.Ports): a
	// role granting services/proxy by resourceNames matches the name "<service>:<port>" as written.
	Ports []string
}

// Detection of Alertmanager by well-known Service names, from the documentation of the charts and
// operators that install one (clean-room, D-12), as Prometheus is found (D-71):
//
//	score 100  <release>-kube-prometheus-alertmanager       kube-prometheus-stack
//	            kube-prometheus-stack-alertmanager
//	score  95  alertmanager-main                            kube-prometheus' manifests
//	score  90  alertmanager-operated                        the Prometheus Operator's governing
//	                                                        service (headless; the proxy picks a pod)
//	score  85  vmalertmanager-<name>                        VictoriaMetrics' operator (a VMAlertmanager
//	                                                        runs Alertmanager itself)
//	score  80  <release>-alertmanager                       the alertmanager chart, the prometheus
//	                                                        chart's bundled one
//	score  70  alertmanager in monitoring, prometheus or alertmanager
//	score  40  alertmanager in any other namespace
//
// A name that is not one of these is never tried: probing every Service of a cluster would send a
// request to each. Ties go to the namespaces named in preferredNamespaces, then by name.
var amNames = []struct {
	re    *regexp.Regexp
	score int
}{
	{regexp.MustCompile(`^(.+-)?kube-prometheus(-stack)?-alertmanager$`), 100},
	{regexp.MustCompile(`^alertmanager-main$`), 95},
	{regexp.MustCompile(`^alertmanager-operated$`), 90},
	{regexp.MustCompile(`^vmalertmanager-.+$`), 85},
	{regexp.MustCompile(`^.+-alertmanager$`), 80},
}

var preferredNamespaces = []string{"monitoring", "prometheus", "alertmanager"}

// MaxProbes bounds how many candidates one detection asks, as Prometheus' detection does (D-71).
const MaxProbes = 3

// webPortNames are the names the charts and operators give Alertmanager's HTTP port, best first.
var webPortNames = []string{"http-web", "web", "http", "http-alertmanager"}

// Candidates ranks the Services that may be an Alertmanager, best first. It is pure.
func Candidates(services []Service) []Candidate {
	var out []Candidate
	for _, s := range services {
		score := scoreName(s.Namespace, s.Name)
		if score == 0 {
			continue
		}
		port, ok := pickPort(s.Ports)
		if !ok {
			continue
		}
		forms := portForms(port)
		out = append(out, Candidate{Target: Target{Namespace: s.Namespace, Service: s.Name, Port: forms[0]}, Score: score, Ports: forms})
	}
	slices.SortFunc(out, func(a, b Candidate) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if pa, pb := nsRank(a.Target.Namespace), nsRank(b.Target.Namespace); pa != pb {
			return pa - pb
		}
		return strings.Compare(a.Target.Where(), b.Target.Where())
	})
	return out
}

func scoreName(namespace, name string) int {
	for _, n := range amNames {
		if n.re.MatchString(name) {
			return n.score
		}
	}
	if name == "alertmanager" {
		if slices.Contains(preferredNamespaces, namespace) {
			return 70
		}
		return 40
	}
	return 0
}

func nsRank(ns string) int {
	if i := slices.Index(preferredNamespaces, ns); i >= 0 {
		return i
	}
	return len(preferredNamespaces)
}

// pickPort chooses the port Alertmanager's API listens on: one named as the charts name it, else
// 9093, else 80, else the first. The cluster gossip ports (mesh, cluster) and a config reloader's
// are skipped by name.
func pickPort(ports []ServicePort) (ServicePort, bool) {
	var usable []ServicePort
	for _, p := range ports {
		n := strings.ToLower(p.Name)
		if strings.Contains(n, "mesh") || strings.Contains(n, "cluster") || strings.Contains(n, "reloader") || strings.Contains(n, "grpc") {
			continue
		}
		usable = append(usable, p)
	}
	for _, want := range webPortNames {
		for _, p := range usable {
			if strings.EqualFold(p.Name, want) {
				return p, true
			}
		}
	}
	for _, want := range []int32{9093, 80} {
		for _, p := range usable {
			if p.Port == want {
				return p, true
			}
		}
	}
	if len(usable) > 0 {
		return usable[0], true
	}
	return ServicePort{}, false
}

// portForms are a port's spellings in the service proxy's name, as Candidate.Ports asks them: the
// number first, then the name.
func portForms(p ServicePort) []string {
	forms := []string{strconv.Itoa(int(p.Port))}
	if p.Name != "" && p.Name != forms[0] {
		forms = append(forms, p.Name)
	}
	return forms
}
