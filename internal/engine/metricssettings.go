package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The Prometheus a cluster's metrics come from, as the user sets it (G12; Lens: Cluster settings →
// Metrics, "Prometheus service address" and a path prefix): a service namespace/service:port and
// a route prefix that replace detection by name, or nothing, and detection finds one. It is kept
// per context in Cogpit's own preferences, <user config dir>/Cogpit/metrics.json, owner-only:
// never in the kubeconfig and never in the cluster (D-2: no implicit kubeconfig writes). Setting
// it sends nothing; the next poll that needs Prometheus probes that service alone (one buildinfo
// GET through the service proxy).

// MetricsSettingsFile is the preferences file of the Prometheus overrides.
const MetricsSettingsFile = "metrics.json"

// maxMetricsSettingsSize bounds the file Cogpit reads back.
const maxMetricsSettingsSize = 1 << 20

// MetricsSettings keeps each context's Prometheus override and its query settings (the query
// format, the empty-container filter, HTTPS; metrics.QuerySettings, since 2026-10-16). Safe for
// concurrent use. A nil MetricsSettings keeps nothing: a setting then lasts for the connection.
type MetricsSettings struct {
	mu    sync.Mutex
	path  string
	by    map[string]metrics.Override
	query map[string]metrics.QuerySettings
	am    map[string]alerting.Address // each context's Alertmanager address (alerts_settings.go, D-77)
}

type metricsSettingsFile struct {
	Prometheus map[string]metrics.Override      `json:"prometheus"`
	Query      map[string]metrics.QuerySettings `json:"query,omitempty"`
	// Alertmanager is each context's Alertmanager address (D-77, alerts_settings.go).
	Alertmanager map[string]alerting.Address `json:"alertmanager,omitempty"`
}

// NewMetricsSettings reads the overrides kept in dir (MetricsSettingsFile). A file that cannot be
// read is left alone and starts empty; err says why.
func NewMetricsSettings(dir string) (*MetricsSettings, error) {
	m := &MetricsSettings{path: filepath.Join(dir, MetricsSettingsFile), by: map[string]metrics.Override{}, query: map[string]metrics.QuerySettings{},
		am: map[string]alerting.Address{}}
	var f metricsSettingsFile
	if err := readPrivateJSON(m.path, maxMetricsSettingsSize, &f); err != nil {
		return m, err
	}
	for ctx, o := range f.Prometheus {
		if CheckPrometheusOverride(o) == nil {
			m.by[ctx] = o
		}
	}
	for ctx, a := range f.Alertmanager {
		if CheckAlertmanagerAddress(a) == nil {
			m.am[ctx] = a
		}
	}
	for ctx, q := range f.Query {
		if _, err := metrics.ParseQueryFormat(q.Format.Name()); err == nil {
			m.query[ctx] = q
		}
	}
	return m, nil
}

// Query returns the query settings kept for a context: Lens's defaults when none are.
func (m *MetricsSettings) Query(context string) metrics.QuerySettings {
	if m == nil {
		return metrics.QuerySettings{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.query[context]
}

// Override returns the override kept for a context, or nil.
func (m *MetricsSettings) Override(context string) *metrics.Override {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.by[context]
	if !ok {
		return nil
	}
	return &o
}

// SetOverride keeps (or, with nil, forgets) a context's override and writes the file; the
// context's query settings stay.
func (m *MetricsSettings) SetOverride(context string, o *metrics.Override) error {
	return m.Set(context, o, m.Query(context))
}

// Set keeps a context's override (nil: detection) and its query settings (the zero value: Lens's
// defaults, kept as no entry) and writes the file.
func (m *MetricsSettings) Set(context string, o *metrics.Override, q metrics.QuerySettings) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next, nextQ := maps.Clone(m.by), maps.Clone(m.query)
	if nextQ == nil {
		nextQ = map[string]metrics.QuerySettings{}
	}
	if o == nil {
		delete(next, context)
	} else {
		next[context] = *o
	}
	if q == (metrics.QuerySettings{}) {
		delete(nextQ, context)
	} else {
		nextQ[context] = q
	}
	data, err := json.MarshalIndent(metricsSettingsFile{Prometheus: next, Query: nextQ, Alertmanager: m.am}, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(m.path, data); err != nil {
		return fmt.Errorf("keep the Prometheus setting: %w", err)
	}
	m.by, m.query = next, nextQ
	return nil
}

// CheckPrometheusOverride refuses what is not a service address: the namespace and the service
// are DNS labels, the port a number (1 to 65535) or a port name, the prefix a path of letters,
// digits, "-", "_", "." and "/" without "." or ".." segments, so that it can only name a path on
// that service behind the service proxy.
func CheckPrometheusOverride(o metrics.Override) error {
	if errs := validation.IsDNS1123Label(o.Namespace); len(errs) > 0 {
		return fmt.Errorf("namespace %q: %s", o.Namespace, strings.Join(errs, "; "))
	}
	if errs := validation.IsDNS1123Label(o.Service); len(errs) > 0 {
		return fmt.Errorf("service %q: %s", o.Service, strings.Join(errs, "; "))
	}
	if n, err := strconv.Atoi(o.Port); err == nil {
		if n < 1 || n > 65535 {
			return fmt.Errorf("port %d: a port is 1 to 65535", n)
		}
	} else if errs := validation.IsValidPortName(o.Port); len(errs) > 0 {
		return fmt.Errorf("port %q: a number or a port name: %s", o.Port, strings.Join(errs, "; "))
	}
	if len(o.Prefix) > 200 {
		return errors.New("the path prefix is longer than 200 characters")
	}
	if strings.IndexFunc(o.Prefix, notPathRune) >= 0 {
		return fmt.Errorf("path prefix %q: only letters, digits, -, _, . and /", o.Prefix)
	}
	for _, seg := range strings.Split(o.Prefix, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("path prefix %q: no . or .. segments", o.Prefix)
		}
	}
	return nil
}

// notPathRune reports a rune a path prefix may not have.
func notPathRune(r rune) bool {
	letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	return !letter && !strings.ContainsRune("-_./", r)
}

// ParsePrometheusAddress reads Lens's form of the address, "namespace/service:port".
func ParsePrometheusAddress(s string) (namespace, service, port string, err error) {
	s = strings.TrimSpace(s)
	ns, rest, ok := strings.Cut(s, "/")
	if !ok {
		return "", "", "", fmt.Errorf("%q: write the address as namespace/service:port", s)
	}
	svc, port, ok := strings.Cut(rest, ":")
	if !ok || port == "" {
		return "", "", "", fmt.Errorf("%q: the address needs a port: namespace/service:port", s)
	}
	return ns, svc, port, nil
}

// PrometheusSetting is the Prometheus a connected cluster's metrics come from: the override, or
// detection by name (Auto), and how it is asked (since 2026-10-16: Lens's query format,
// empty-container filter and HTTPS, Cluster Settings › Prometheus since D-87).
type PrometheusSetting struct {
	// Context is the connected context the setting belongs to.
	Context string `json:"context"`
	// Auto: no override; Prometheus is detected among the cluster's Services by name.
	Auto bool `json:"auto"`
	// Address is the override as Lens writes it, "namespace/service:port"; Prefix its route prefix.
	Address string `json:"address,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	// Format is the query format's name (metrics.QueryFormats): auto, prometheus-operator,
	// cadvisor; FormatLabel the settings' label for it.
	Format      string `json:"format"`
	FormatLabel string `json:"formatLabel"`
	// FilterEmptyContainers is Lens's checkbox, on by default: the pod queries leave the series
	// without a container name out.
	FilterEmptyContainers bool `json:"filterEmptyContainers"`
	// HTTPS asks the Prometheus service over TLS through the service proxy.
	HTTPS bool `json:"https"`
	// Kept says whether the setting is in Cogpit's preferences (it outlives the connection); false
	// when Cogpit has no configuration directory.
	Kept bool `json:"kept"`
	// InUse is the Prometheus the cluster's metrics come from now, as the status line names it
	// ("Prometheus monitoring/prometheus-k8s:web", "Thanos monitoring/thanos-query:http"), and
	// InUseStatus what Cogpit knows of it (metrics.Status: unknown while nothing looked for it,
	// notServed when detection found none). Read from the hub's last answers: no request. Since
	// 2026-10-16 night, for the address's line on Cluster Settings › Prometheus.
	InUse       string         `json:"inUse,omitempty"`
	InUseStatus metrics.Status `json:"inUseStatus,omitempty"`
}

func settingOf(context string, o *metrics.Override, q metrics.QuerySettings, kept bool) PrometheusSetting {
	s := PrometheusSetting{Context: context, Auto: o == nil, Kept: kept, Format: q.Format.Name(), FormatLabel: q.Format.Label(),
		FilterEmptyContainers: !q.KeepEmptyContainers, HTTPS: q.HTTPS}
	if o != nil {
		s.Address, s.Prefix = o.Namespace+"/"+o.Service+":"+o.Port, o.Prefix
	}
	return s
}

// withInUse adds the Prometheus the hub found (its status line's row) to a setting.
func withInUse(s PrometheusSetting, hub *metrics.Hub) PrometheusSetting {
	if hub == nil {
		return s
	}
	for _, ps := range hub.Providers() {
		if ps.Provider != metrics.PrometheusID {
			continue
		}
		s.InUseStatus = ps.Status
		if ps.Service != "" {
			s.InUse = strings.TrimSpace(ps.Label + " " + ps.Service)
		}
	}
	return s
}

// noPrometheusDetection refuses a Prometheus setting for a backend without detection: the demo
// cluster, whose metrics are its own, or a connection without a metrics client.
func noPrometheusDetection(b *Backend) error {
	why := "no metrics client for this connection"
	if b.Demo {
		why = "the demo cluster's metrics are its own"
	}
	return fmt.Errorf("%w: %s has no Prometheus detection (%s)", ErrNotApplicable, b.Context, why)
}

// PrometheusSetting returns a connected cluster's Prometheus setting, "" the shown cluster's (D-69).
func (s *Session) PrometheusSetting(contextID string) (PrometheusSetting, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return PrometheusSetting{}, err
	}
	if b.PrometheusDetector == nil {
		return PrometheusSetting{}, noPrometheusDetection(b)
	}
	d := b.PrometheusDetector
	return withInUse(settingOf(b.Context, d.Override(), d.Query(), b.MetricsSettings != nil), b.Metrics), nil
}

// SetPrometheusOverride sets a connected cluster's Prometheus ("" the shown one; nil: detect by
// name again) and keeps its query settings: SetPrometheusSetting with them.
func (s *Session) SetPrometheusOverride(contextID string, o *metrics.Override) (PrometheusSetting, error) {
	b := s.BackendOf(contextID)
	if b == nil || b.PrometheusDetector == nil {
		return s.SetPrometheusSetting(contextID, o, metrics.QuerySettings{})
	}
	return s.SetPrometheusSetting(contextID, o, b.PrometheusDetector.Query())
}

// SetPrometheusSetting sets a connected cluster's Prometheus ("" the shown one, D-69; nil: detect by
// name again) and how it is asked (q), keeps both in Cogpit's preferences for that context, and
// makes the next poll that needs Prometheus detect again with them: a drawer that is open asks the
// new one for its history at once. It writes nothing to the cluster or the kubeconfig and sends no
// request itself.
func (s *Session) SetPrometheusSetting(contextID string, o *metrics.Override, q metrics.QuerySettings) (PrometheusSetting, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return PrometheusSetting{}, err
	}
	if b.PrometheusDetector == nil || b.Metrics == nil {
		return PrometheusSetting{}, noPrometheusDetection(b)
	}
	if o != nil {
		c := *o
		c.Prefix = strings.Trim(c.Prefix, "/")
		if c.Prefix != "" {
			c.Prefix = "/" + c.Prefix
		}
		if err := CheckPrometheusOverride(c); err != nil {
			return PrometheusSetting{}, err
		}
		o = &c
	}
	if _, err := metrics.ParseQueryFormat(q.Format.Name()); err != nil {
		return PrometheusSetting{}, err
	}
	if err := b.MetricsSettings.Set(b.Context, o, q); err != nil {
		return PrometheusSetting{}, err
	}
	d := b.PrometheusDetector
	d.SetOverride(o)
	d.SetQuery(q)
	b.Metrics.Forget()
	s.emit(Event{Type: EventMetrics})
	return withInUse(settingOf(b.Context, d.Override(), d.Query(), b.MetricsSettings != nil), b.Metrics), nil
}
