package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// Integrations (D-76; ui-parity › Cluster settings › Integrations, Cogpit's own): the tools a
// cluster runs that Cogpit finds and reads to show more, each switched on or off per cluster: the
// Kubernetes Metrics Server and Prometheus (the metrics, metrics.go), Alertmanager (the alerts pill
// and the alert board), Trivy Operator (the image ratings of Pods) and Kyverno (policy violations on
// the objects). Cogpit never installs one (AGENTS.md, D-61): it finds what runs, or the user names
// where it runs.
//
// A context's choice is ClusterSettings.IntegrationsOff: the integrations switched off, nil while
// the user has made none for it (the window then asks, D-76). Meanwhile nothing of them is read but
// what the question looks for (D-82, integrationsconsent.go); the default for new clusters
// (Preferences.IntegrationsOff) is what the question's Use my default integrations turns on, and what applies in a
// session that does not ask (the CLI's) and to the demo cluster. Every integration is on by default
// (the owner, 2026-10-17: "Unser Default ist alles an"), so an integration a later Cogpit adds is on
// for the clusters decided before it. An integration switched off sends nothing to the cluster, and the
// window shows nothing of it: no column, no drawer section, no pill, nothing in ui_state (as hidden
// metrics, hidemetrics.go).
//
// The two metrics switches are Lens's Metrics source (metricssource.go) seen per provider: both on
// is Automatic, one on is that provider alone, both off is No metrics. The setting kept before them
// (ClusterSettings.MetricsSource) is read once and moved into IntegrationsOff (normalize).

// Integration names one integration, as commands and the settings files name it.
type Integration string

// The integrations, in the settings' order.
const (
	IntegrationMetricsServer Integration = "metrics-server"
	IntegrationPrometheus    Integration = "prometheus"
	IntegrationAlertmanager  Integration = "alertmanager"
	IntegrationTrivy         Integration = "trivy"
	IntegrationKyverno       Integration = "kyverno"
)

// IntegrationSet is a set of integrations. JSON: the list of their names, in Integrations' order.
type IntegrationSet uint16

// The integrations' bits.
const (
	IntMetricsServer IntegrationSet = 1 << iota
	IntPrometheus
	IntAlertmanager
	IntTrivy
	IntKyverno

	// IntAll is every integration.
	IntAll = IntMetricsServer | IntPrometheus | IntAlertmanager | IntTrivy | IntKyverno
	// IntMetrics are the two metrics providers.
	IntMetrics = IntMetricsServer | IntPrometheus
)

// IntegrationInfo is one integration as the settings list it.
type IntegrationInfo struct {
	Set   IntegrationSet
	Name  Integration
	Label string // the settings' label
	Adds  string // what it adds to the window, one line under its switch
}

// Integrations are the integrations in the order the settings list them.
var Integrations = []IntegrationInfo{
	{IntMetricsServer, IntegrationMetricsServer, "Kubernetes Metrics Server",
		"Current CPU and memory of nodes, pods and containers"},
	{IntPrometheus, IntegrationPrometheus, "Prometheus",
		"Metrics history, network, filesystem and disks (Thanos and VictoriaMetrics too)"},
	{IntAlertmanager, IntegrationAlertmanager, "Alertmanager",
		"The alerts pill in the title bar and the alert board"},
	{IntTrivy, IntegrationTrivy, "Trivy Operator",
		"The worst image rating of every pod, and each image's in the pod's details"},
	{IntKyverno, IntegrationKyverno, "Kyverno",
		"Marks the objects that violate a policy, in the lists and the details"},
}

// IntegrationOf returns an integration's entry; ok is false for a name this Cogpit does not know.
func IntegrationOf(name Integration) (IntegrationInfo, bool) {
	i := slices.IndexFunc(Integrations, func(in IntegrationInfo) bool { return in.Name == name })
	if i < 0 {
		return IntegrationInfo{}, false
	}
	return Integrations[i], true
}

// ParseIntegrationSet reads integration names ("prometheus", "kyverno"; case and spaces ignored);
// an unknown name is an error that lists the known ones.
func ParseIntegrationSet(names []string) (IntegrationSet, error) {
	var m IntegrationSet
	for _, n := range names {
		in, ok := IntegrationOf(Integration(strings.ToLower(strings.TrimSpace(n))))
		if !ok {
			return 0, fmt.Errorf("integration %q: one of %s", n, strings.Join(IntegrationNames(IntAll), ", "))
		}
		m |= in.Set
	}
	return m, nil
}

// IntegrationNames returns the names of a set's integrations, in Integrations' order; never nil.
func IntegrationNames(m IntegrationSet) []string {
	out := []string{}
	for _, in := range Integrations {
		if m&in.Set != 0 {
			out = append(out, string(in.Name))
		}
	}
	return out
}

// Has reports whether m holds every integration of o.
func (m IntegrationSet) Has(o IntegrationSet) bool { return m&o == o }

// MarshalJSON writes the list of names.
func (m IntegrationSet) MarshalJSON() ([]byte, error) { return json.Marshal(IntegrationNames(m)) }

// UnmarshalJSON reads a list of names. A name this Cogpit does not know (a file a later one wrote)
// is dropped rather than refused, so that one entry does not cost the others.
func (m *IntegrationSet) UnmarshalJSON(b []byte) error {
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return err
	}
	*m = 0
	for _, n := range names {
		if s, err := ParseIntegrationSet([]string{n}); err == nil {
			*m |= s
		}
	}
	return nil
}

// integrationsOffOfSource is what a Metrics source kept before D-76 switches off.
func integrationsOffOfSource(source string) IntegrationSet {
	switch metrics.Source(source) {
	case metrics.SourcePrometheus:
		return IntMetricsServer
	case metrics.SourceMetricsServer:
		return IntPrometheus
	case metrics.SourceNone:
		return IntMetrics
	}
	return 0
}

// sourceOfIntegrationsOff is the Metrics source the two metrics switches make.
func sourceOfIntegrationsOff(off IntegrationSet) metrics.Source {
	switch off & IntMetrics {
	case IntMetricsServer:
		return metrics.SourcePrometheus
	case IntPrometheus:
		return metrics.SourceMetricsServer
	case IntMetrics:
		return metrics.SourceNone
	}
	return metrics.SourceAuto
}

// integrationsOffOf is a context's integrations switched off: its own choice; else, in a session
// that asks before it looks (D-82, integrationsconsent.go), every one but those its question ticks
// while it asks, all of them otherwise (put off with Later, connected in the background, not shown);
// else (a session that does not ask, the demo cluster) the default for new clusters.
func integrationsOffOf(u *UIState, context string) IntegrationSet {
	if u == nil {
		return 0
	}
	if off := u.ClusterSettings[context].IntegrationsOff; off != nil {
		return *off & IntAll
	}
	if u.IntegrationsAskFirst && !u.demoCluster(context) {
		if context != "" && u.IntegrationsQuestion == context {
			return IntAll &^ u.IntegrationsLookFor
		}
		return IntAll
	}
	if u.Preferences != nil {
		return u.Preferences.IntegrationsOff & IntAll
	}
	return 0
}

// IntegrationOn reports whether an integration is on for a context.
func (u *UIState) IntegrationOn(context string, i Integration) bool {
	in, ok := IntegrationOf(i)
	return ok && integrationsOffOf(u, context)&in.Set == 0
}

// IntegrationsDecided reports whether the user made a choice of integrations for a context; until
// then the window asks (D-76) and nothing of them is read but what the question looks for (D-82).
func (u *UIState) IntegrationsDecided(context string) bool {
	return u != nil && u.ClusterSettings[context].IntegrationsOff != nil
}

// SetIntegrations switches integrations on and off in a context's settings, starting from what
// applies now (its choice, else baseOff: Session.IntegrationsOffNow): the change is the user's
// choice from then on.
func (c *ClusterSettings) SetIntegrations(baseOff, on, off IntegrationSet) {
	cur := baseOff & IntAll
	if c.IntegrationsOff != nil {
		cur = *c.IntegrationsOff & IntAll
	}
	cur = (cur &^ on) | off
	c.IntegrationsOff = &cur
}

// IntegrationDefaultsOff returns the integrations off by default for new clusters (Preferences'
// IntegrationsOff; the question's Use my default integrations turns on the others, D-82): none in a session that
// keeps no preferences.
func (s *Session) IntegrationDefaultsOff() IntegrationSet {
	p, err := s.Preferences()
	if err != nil {
		return 0
	}
	return p.IntegrationsOff & IntAll
}

// IntegrationState is what Cogpit knows of an integration on a cluster.
type IntegrationState string

// The integration states.
const (
	// IntegrationNotConnected: the cluster is not connected; nothing is known.
	IntegrationNotConnected IntegrationState = "notConnected"
	// IntegrationOff: switched off; nothing is asked of the cluster.
	IntegrationOff IntegrationState = "off"
	// IntegrationChecking: detection has not settled yet.
	IntegrationChecking IntegrationState = "checking"
	// IntegrationFound: found automatically; Where says where.
	IntegrationFound IntegrationState = "found"
	// IntegrationConfigured: the address the user set answers; Where says where.
	IntegrationConfigured IntegrationState = "configured"
	// IntegrationManual: not found by name; the user can set its address (Prometheus,
	// Alertmanager). Detail says what to set.
	IntegrationManual IntegrationState = "manual"
	// IntegrationMissing: not in this cluster (its API is not served); Detail says what is missing.
	IntegrationMissing IntegrationState = "missing"
	// IntegrationForbidden: the user may not read it; Detail names the verb and the resource.
	IntegrationForbidden IntegrationState = "forbidden"
	// IntegrationUnavailable: found, but it does not answer; Detail says why.
	IntegrationUnavailable IntegrationState = "unavailable"
)

// IntegrationStatus is one integration on one cluster: whether it is on, and what Cogpit found.
type IntegrationStatus struct {
	// Name is the integration's name, Label its label in the settings.
	Name  Integration `json:"name"`
	Label string      `json:"label"`
	// On says whether it is switched on for the cluster (the user's choice, else the default).
	On bool `json:"on"`
	// State is what Cogpit knows of it on the cluster.
	State IntegrationState `json:"state"`
	// Where is where it was found or set: "monitoring/kube-prometheus-alertmanager:9093",
	// "wgpolicyk8s.io/v1alpha2"; empty otherwise.
	Where string `json:"where,omitempty"`
	// Detail says more: what is missing, the verb the user may not use, why it does not answer.
	Detail string `json:"detail,omitempty"`
	// Adds is what it adds to the window, one line.
	Adds string `json:"adds"`
}

// integrationProbe tells what an integration's code knows of it on a connected cluster. It sends no
// request: it reads what the integration's own reads, the cache and API discovery hold. on says
// whether the integration is switched on; a probe of an integration switched off answers what it
// knows without asking (IntegrationOff when that is nothing).
type integrationProbe func(s *Session, b *Backend, on bool) (state IntegrationState, where, detail string)

// integrationProbes are the integrations' probes, by name; each integration's file adds its own in
// an init function. An integration without one shows as checking.
var integrationProbes = map[Integration]integrationProbe{
	IntegrationMetricsServer: probeMetricsProvider(metrics.ServerID),
	IntegrationPrometheus:    probeMetricsProvider(metrics.PrometheusID),
}

// integrationFollower applies the UI state's integration settings to one connected cluster: it
// starts the integration's reads when it is on and stops them when it is off. It runs while the
// engine reconciles (views.go, with its lock held): it must not block and must not wait for the
// cluster.
type integrationFollower func(st *state, b *Backend, now UIState)

// integrationFollowers are the integrations' followers; each integration's file adds its own in an
// init function.
var integrationFollowers []integrationFollower

// followIntegrations applies the integration settings to one connected cluster.
func (st *state) followIntegrations(b *Backend, now UIState) {
	st.followMetricsSource(b, now)
	for _, f := range integrationFollowers {
		f(st, b, now)
	}
}

// IntegrationStatuses returns every integration of a context, in Integrations' order: whether it is
// on and, while the cluster is connected, what Cogpit found. It sends no request.
func (s *Session) IntegrationStatuses(context string) []IntegrationStatus {
	u := s.UIState()
	var b *Backend
	if u.connectionState(context).Live() {
		b = s.BackendOf(context)
	}
	out := make([]IntegrationStatus, 0, len(Integrations))
	for _, in := range Integrations {
		st := IntegrationStatus{Name: in.Name, Label: in.Label, Adds: in.Adds, On: u.IntegrationOn(context, in.Name),
			State: IntegrationNotConnected}
		if b != nil {
			st.State = IntegrationChecking
			if probe := integrationProbes[in.Name]; probe != nil {
				st.State, st.Where, st.Detail = probe(s, b, st.On)
			}
		}
		out = append(out, st)
	}
	return out
}

// integrationsChanged tells the window and agents that what Cogpit knows of a cluster's
// integrations changed (a detection settled, an integration was switched); re-read
// IntegrationStatuses.
func (s *Session) integrationsChanged(context string) {
	s.emit(Event{Type: EventIntegrations, View: context})
}

// probeMetricsProvider is the probe of a metrics provider: the hub's status of it (metrics.go).
func probeMetricsProvider(id string) integrationProbe {
	return func(_ *Session, b *Backend, on bool) (IntegrationState, string, string) {
		if !on {
			return IntegrationOff, "", ""
		}
		if b.Metrics == nil {
			return IntegrationMissing, "", "this cluster has no metrics"
		}
		for _, p := range b.Metrics.Providers() {
			if p.Provider != id {
				continue
			}
			switch p.Status {
			case metrics.StatusAvailable:
				state := IntegrationFound
				if id == metrics.PrometheusID && b.PrometheusDetector != nil && b.PrometheusDetector.Override() != nil {
					state = IntegrationConfigured
				}
				return state, p.Service, ""
			case metrics.StatusNotServed:
				if id == metrics.PrometheusID {
					return IntegrationManual, "", "none found by name"
				}
				return IntegrationMissing, "", "the cluster does not serve metrics.k8s.io"
			case metrics.StatusForbidden:
				return IntegrationForbidden, p.Service, p.LastError
			case metrics.StatusUnavailable:
				return IntegrationUnavailable, p.Service, p.LastError
			}
			return IntegrationChecking, p.Service, ""
		}
		if id == metrics.PrometheusID && b.PrometheusDetector == nil {
			// A hub over one source of its own (the demo cluster's): no Prometheus is looked for, so
			// none is ever found (noPrometheusDetection's reasons).
			why := "no Prometheus detection for this connection"
			if b.Demo {
				why = "the demo cluster's metrics are its own"
			}
			return IntegrationMissing, "", why
		}
		return IntegrationChecking, "", ""
	}
}
