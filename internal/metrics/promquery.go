package metrics

import (
	"fmt"
	"strings"
)

// How Cogpit asks a cluster's Prometheus, as the user sets it per cluster (Cluster Settings ›
// Metrics; G12). Lens's provider row offers three such settings beside the service address
// (captured): a "Prometheus" dropdown of query formats, presets of how the series are named per
// installation type; a checkbox that filters empty containers out of the queries (on); and a
// checkbox for HTTPS requests to the service (off). Cogpit's presets are its own (D-12): what an
// installation changes for Cogpit's queries is where a node's series come from, so a preset names
// the installation and fixes that source; Lens's list of providers (Helm 14.x, Lens, StackLight,
// VictoriaMetrics …) is not reproduced.

// QueryFormat is how a cluster's Prometheus names the series of its nodes: QueryAuto (the default)
// finds out by trying, the others fix the source.
type QueryFormat string

// The query formats.
const (
	// QueryAuto tries node-exporter's series for a node's charts first and cAdvisor's root cgroup
	// when there are none, and remembers the answer for a detection (A43).
	QueryAuto QueryFormat = ""
	// QueryOperator is a Prometheus Operator installation (kube-prometheus-stack, kube-prometheus):
	// a node's series come from node-exporter, matched by its address; cAdvisor's root cgroup is
	// never asked, since the kubelet's ServiceMonitor drops it.
	QueryOperator QueryFormat = "prometheus-operator"
	// QueryCAdvisor is a Prometheus that scrapes the kubelets' cAdvisor and no node-exporter (the
	// prometheus chart with node-exporter off): a node's series come from its root cgroup, and
	// node-exporter is never asked.
	QueryCAdvisor QueryFormat = "cadvisor"
)

// QueryFormatOption is one entry of the settings' query format select.
type QueryFormatOption struct {
	Format QueryFormat `json:"format"`
	// Name is the format's name in commands and the settings file: "auto", "prometheus-operator",
	// "cadvisor".
	Name  string `json:"name"`
	Label string `json:"label"`
}

// QueryFormats are the formats in the select's order. The labels are Cogpit's (inferred).
var QueryFormats = []QueryFormatOption{
	{Format: QueryAuto, Name: "auto", Label: "Auto detect"},
	{Format: QueryOperator, Name: string(QueryOperator), Label: "Prometheus Operator (kube-prometheus-stack)"},
	{Format: QueryCAdvisor, Name: string(QueryCAdvisor), Label: "cAdvisor only (no node-exporter)"},
}

// Name is the format's name ("auto" for QueryAuto).
func (f QueryFormat) Name() string {
	if f == QueryAuto {
		return "auto"
	}
	return string(f)
}

// Label is the format's label in the settings.
func (f QueryFormat) Label() string {
	for _, o := range QueryFormats {
		if o.Format == f {
			return o.Label
		}
	}
	return string(f)
}

// ParseQueryFormat reads a format's name; "" and "auto" are QueryAuto.
func ParseQueryFormat(s string) (QueryFormat, error) {
	s = strings.TrimSpace(s)
	for _, o := range QueryFormats {
		if strings.EqualFold(s, o.Name) || o.Format == QueryAuto && s == "" {
			return o.Format, nil
		}
	}
	names := make([]string, len(QueryFormats))
	for i, o := range QueryFormats {
		names[i] = o.Name
	}
	return "", fmt.Errorf("query format %q: one of %s", s, strings.Join(names, ", "))
}

// QuerySettings are a cluster's settings of how Cogpit asks its Prometheus. The zero value is
// Lens's defaults: the format detected, empty containers filtered out, plain http to the service.
type QuerySettings struct {
	Format QueryFormat `json:"format,omitempty"`
	// KeepEmptyContainers turns the empty-container filter off: the pod queries keep the series
	// without a container name (container=""), so a pod's line is its own cgroup's where the
	// Prometheus has one, and a Prometheus whose cAdvisor series carry no container name still shows
	// the pods. Off by default (the filter on, Lens's default).
	KeepEmptyContainers bool `json:"keepEmptyContainers,omitempty"`
	// HTTPS asks the Prometheus service over https through the API server's service proxy
	// (/services/https:<service>:<port>/proxy/): for a Prometheus that serves TLS only (behind an
	// authenticating proxy). The API server makes the TLS connection, not Cogpit; whether it checks
	// the service's certificate is the API server's (inferred: kube-apiserver's proxy transport does
	// not; not verified against a cluster).
	HTTPS bool `json:"https,omitempty"`
}

// scheme is the service proxy's scheme for the settings: "https", or "" for http.
func (q QuerySettings) scheme() string {
	if q.HTTPS {
		return "https"
	}
	return ""
}

// filter is the pods' container matcher of the settings.
func (q QuerySettings) filter() string { return containerFilter(q.KeepEmptyContainers) }
