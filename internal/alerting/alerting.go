// Package alerting reads a cluster's Alertmanager (D-77): it finds one among the cluster's cached
// Services by its well-known names, or takes the address the user set, and asks it for the active
// alerts that are not inhibited, one GET through the API server's service proxy every poll
// interval, on the connection's own clients (counted, rate limited and authenticated as every other
// request): the silenced ones apart, for the silences page (D-166). It lists, creates and expires
// silences on the user's request (silences.go), and sends nothing else that writes. Cogpit installs
// no Alertmanager (D-61, AGENTS.md).
//
// It is an L2 package beside metrics (architecture §3): the engine owns when it polls (the
// integration switched on, the cluster shown and connected) and what the window shows of it.
package alerting

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Severity is how an alert is shown: its group on the board and the colour it gives the title
// bar's pill. Alertmanager knows no severities: they are the `severity` label the alerting rules'
// authors set.
type Severity string

// The severities Cogpit shows, worst first.
const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Severities are the shown severities, worst first.
var Severities = []Severity{SeverityCritical, SeverityWarning, SeverityInfo}

// Rank orders severities: 0 for critical, 1 for warning, 2 for info, 3 for anything else.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	}
	return 3
}

// metaAlerts are alerts that say something about the alerting pipeline, not about the cluster:
// kube-prometheus' Watchdog (always firing, to prove the pipeline works) and InfoInhibitor (firing
// to inhibit info alerts). They are left out whatever their severity (the owner: "nur wirklich
// relevantes zeigen").
var metaAlerts = map[string]bool{"Watchdog": true, "InfoInhibitor": true}

// Classify returns the severity an alert is shown with, from its alertname and its severity label,
// and false for an alert that is left out of the count and the board. The owner's rule: critical
// red, warning orange and every severity above warning too ("Orange ab einem Warning Alarm":
// major), info yellow; none (the meta alerts' severity) left out. The lead's decision for what the
// owner did not name (D-77): a missing or unknown severity label shows as warning, since the rule's
// author did not mark the alert as only informational; the label as written stays on the alert
// (SeverityLabel). Labels are compared ignoring case and surrounding spaces.
func Classify(alertname, severity string) (Severity, bool) {
	if metaAlerts[alertname] {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "none":
		return "", false
	case "critical":
		return SeverityCritical, true
	case "info":
		return SeverityInfo, true
	}
	return SeverityWarning, true // warning, major, and every other or missing label
}

// Alert is one active alert as Cogpit keeps it: the labels and the three annotations the board
// shows, bounded (maxText, maxLabels). The generator URL, the receivers and the other annotations
// are not kept.
type Alert struct {
	// Fingerprint is Alertmanager's id of the alert (a hash of its labels).
	Fingerprint string `json:"fingerprint"`
	// Name is the alertname label.
	Name string `json:"alertname"`
	// Severity is how it is shown (Classify); SeverityLabel the label as the rule wrote it, "" when
	// it has none.
	Severity      Severity `json:"severity"`
	SeverityLabel string   `json:"severityLabel,omitempty"`
	// Summary, Description and Runbook are the annotations summary, description (message, an older
	// name kube-prometheus used, when it has none) and runbook_url, as the rule wrote them.
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Runbook     string `json:"runbook,omitempty"`
	// StartsAt is when it started firing.
	StartsAt time.Time `json:"startsAt"`
	// Labels are its labels, alertname and severity included.
	Labels map[string]string `json:"labels"`
	// SilencedBy are the ids of the silences that mute it; empty for an alert the board shows.
	SilencedBy []string `json:"silencedBy,omitempty"`
}

// Bounds of what is kept of one alert, against a rule whose template renders a whole log line into
// an annotation: the board shows a line of the summary and a few lines of the description.
const (
	maxText   = 4 << 10 // an annotation's bytes
	maxLabels = 64      // labels kept per alert
	maxLabel  = 1 << 10 // a label value's bytes
)

// wireAlert is Alertmanager's API v2 gettableAlert, the fields Cogpit reads.
type wireAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    *time.Time        `json:"startsAt"`
	Status      *struct {
		State      string   `json:"state"`
		SilencedBy []string `json:"silencedBy"`
	} `json:"status"`
}

// decoded is an answer of the alerts request: the alerts the board shows (Classify, at most the
// limit; total counts them before it) and the silenced ones apart, the meta alerts among them (a
// silence often mutes Watchdog), at most the limit too.
type decoded struct {
	alerts     []Alert
	total      int
	truncated  bool
	muted      []Alert
	mutedTotal int
}

// errNotAlertmanager says that an answer is not Alertmanager's list of alerts.
var errNotAlertmanager = errors.New("not an Alertmanager")

// decodeAlerts reads the answer of GET /api/v2/alerts: a JSON array of gettable alerts. An answer of
// another shape (an object, an array of something else) is errNotAlertmanager, which is how
// detection tells an Alertmanager from another service of a similar name. Alerts beyond limit are
// dropped (truncated reports it); the meta alerts and those of severity none are left out of the
// shown alerts here. An alert some silence mutes goes to the muted alerts whatever its severity.
func decodeAlerts(body []byte, limit int) (decoded, error) {
	var wire []wireAlert
	if err := json.Unmarshal(body, &wire); err != nil {
		return decoded{}, errNotAlertmanager
	}
	d := decoded{alerts: make([]Alert, 0, min(len(wire), limit))}
	for _, w := range wire {
		if w.Labels == nil || w.StartsAt == nil || w.Status == nil {
			return decoded{}, errNotAlertmanager
		}
		name := w.Labels["alertname"]
		sev, keep := Classify(name, w.Labels["severity"])
		silenced := len(w.Status.SilencedBy) > 0
		if !keep && !silenced {
			continue
		}
		if silenced {
			d.mutedTotal++
			if len(d.muted) >= limit {
				continue
			}
		} else {
			d.total++
			if len(d.alerts) >= limit {
				d.truncated = true
				continue
			}
		}
		desc := w.Annotations["description"]
		if desc == "" {
			desc = w.Annotations["message"]
		}
		a := Alert{
			Fingerprint: clip(w.Fingerprint, 64), Name: clip(name, maxLabel), Severity: sev,
			SeverityLabel: clip(strings.TrimSpace(w.Labels["severity"]), 64),
			Summary:       clip(w.Annotations["summary"], maxText), Description: clip(desc, maxText),
			Runbook: clip(strings.TrimSpace(w.Annotations["runbook_url"]), 2048), StartsAt: *w.StartsAt,
			Labels: boundLabels(w.Labels),
		}
		if silenced {
			for i, id := range w.Status.SilencedBy {
				if i == maxMatchers {
					break
				}
				a.SilencedBy = append(a.SilencedBy, clip(id, 64))
			}
			d.muted = append(d.muted, a)
			continue
		}
		d.alerts = append(d.alerts, a)
	}
	return d, nil
}

// boundLabels keeps at most maxLabels labels, each value at most maxLabel bytes; alertname,
// severity and namespace always.
func boundLabels(in map[string]string) map[string]string {
	out := make(map[string]string, min(len(in), maxLabels))
	for _, k := range []string{"alertname", "severity", "namespace"} {
		if v, ok := in[k]; ok {
			out[k] = clip(v, maxLabel)
		}
	}
	for k, v := range in {
		if len(out) >= maxLabels {
			break
		}
		if len(k) > 128 {
			continue
		}
		out[k] = clip(v, maxLabel)
	}
	return out
}

// clip cuts s to at most n bytes at a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for range utf8.UTFMax { // a rune the cut split in two goes whole
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size != 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
