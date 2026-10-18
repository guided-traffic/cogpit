package metrics

import (
	"regexp"
	"strings"
)

// The servers that speak the Prometheus HTTP API (G12, D-71, night run 2026-10-16/17): besides
// Prometheus itself, Thanos's Querier and its query frontend, which answer for the Prometheus
// servers and the object storage behind them, and VictoriaMetrics, single-node or the cluster's
// vmselect, which serves the API under /select/<tenant>/prometheus. Cogpit asks each the same
// queries through the service proxy (promql.go); what differs is the name the source line, the
// status line and the settings show, and for vmselect the path. Each is found by its Service's name
// as Prometheus is (promdetect.go), from the charts' and the operators' documented defaults
// (clean-room, D-12), and probed with the same buildinfo request: Thanos and VictoriaMetrics answer
// it as Prometheus does (Thanos 0.40's Querier and query frontend verified on a real cluster,
// VictoriaMetrics single-node on kind; VictoriaMetrics' vmselect inferred from its documentation
// and checked against a fake).

// Flavor is which server speaks the Prometheus HTTP API at a Target.
type Flavor string

// The flavors.
const (
	// FlavorPrometheus is Prometheus itself, and any server Cogpit does not tell apart.
	FlavorPrometheus Flavor = ""
	// FlavorThanos is Thanos's Querier or its query frontend.
	FlavorThanos Flavor = "thanos"
	// FlavorVictoriaMetrics is VictoriaMetrics: single-node, or the cluster's vmselect.
	FlavorVictoriaMetrics Flavor = "victoriametrics"
)

// Name is the flavor's product name, which the source line, the status line and the settings
// show: "Prometheus", "Thanos", "VictoriaMetrics".
func (f Flavor) Name() string {
	switch f {
	case FlavorThanos:
		return "Thanos"
	case FlavorVictoriaMetrics:
		return "VictoriaMetrics"
	}
	return PrometheusLabel
}

// VMSelectPrefix is the path VictoriaMetrics' cluster vmselect serves the Prometheus API under for
// the default tenant, 0 (its accountID); another tenant is the user's to name in the path prefix
// (Cluster Settings › Prometheus, cluster.setPrometheus).
const VMSelectPrefix = "/select/0/prometheus"

// vmSelectPath is a path prefix that names a tenant of vmselect: /select/<accountID>[:<projectID>]/prometheus.
var vmSelectPath = regexp.MustCompile(`^/?select/[0-9]+(:[0-9]+)?/prometheus/?$`)

// flavorOf is the flavor a Service's name and a path prefix say: a known name's, else
// VictoriaMetrics for a vmselect tenant's path, else Prometheus. The user's override is classified
// by it, so a named Thanos or VictoriaMetrics shows under its own name too.
func flavorOf(name, prefix string) Flavor {
	if n, ok := matchName(name); ok && n.flavor != FlavorPrometheus {
		return n.flavor
	}
	if vmSelectPath.MatchString(strings.TrimSpace(prefix)) {
		return FlavorVictoriaMetrics
	}
	return FlavorPrometheus
}

// matchName is the first well-known name that a Service's name matches.
func matchName(name string) (promName, bool) {
	for _, n := range promNames {
		if n.re.MatchString(name) {
			return n, true
		}
	}
	return promName{}, false
}
