package alerting

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Classify: the owner's colours (critical red, warning and major orange, info yellow), severity none
// and the meta alerts left out, a missing or unknown label as warning (D-77).
func TestClassify(t *testing.T) {
	for _, c := range []struct {
		name, label string
		want        Severity
		keep        bool
	}{
		{"KubePodCrashLooping", "critical", SeverityCritical, true},
		{"X", " Critical ", SeverityCritical, true},
		{"X", "warning", SeverityWarning, true},
		{"KustomizationNotReady", "major", SeverityWarning, true},
		{"X", "info", SeverityInfo, true},
		{"X", "INFO", SeverityInfo, true},
		{"X", "", SeverityWarning, true},
		{"X", "page", SeverityWarning, true},
		{"X", "none", "", false},
		{"Watchdog", "none", "", false},
		{"Watchdog", "critical", "", false},
		{"InfoInhibitor", "info", "", false},
	} {
		got, keep := Classify(c.name, c.label)
		if got != c.want || keep != c.keep {
			t.Errorf("Classify(%q, %q) = %q, %v; want %q, %v", c.name, c.label, got, keep, c.want, c.keep)
		}
	}
	if SeverityCritical.Rank() >= SeverityWarning.Rank() || SeverityWarning.Rank() >= SeverityInfo.Rank() || Severity("x").Rank() <= SeverityInfo.Rank() {
		t.Error("ranks out of order")
	}
}

func wire(t *testing.T, alerts ...map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(alerts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func wireAlertOf(name, severity string, at time.Time, ann map[string]string) map[string]any {
	labels := map[string]string{"alertname": name}
	if severity != "" {
		labels["severity"] = severity
	}
	return map[string]any{"fingerprint": "fp-" + name + severity, "labels": labels, "annotations": ann,
		"startsAt": at.Format(time.RFC3339Nano), "status": map[string]any{"state": "active"}}
}

// decodeAlerts: Alertmanager's v2 shape read, the left-out alerts not counted, description falling
// back to message, the bounds kept, and any other shape refused as not an Alertmanager.
func TestDecodeAlerts(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 15, 0, 0, time.UTC)
	long := strings.Repeat("ä", maxText) // two bytes a rune: cut at a rune boundary
	body := wire(t,
		wireAlertOf("A", "critical", at, map[string]string{"summary": "s", "description": "d", "runbook_url": " https://r "}),
		wireAlertOf("B", "major", at, map[string]string{"message": "old style", "summary": long}),
		wireAlertOf("Watchdog", "none", at, nil),
		wireAlertOf("C", "none", at, nil),
		wireAlertOf("D", "", at, nil),
	)
	d, err := decodeAlerts(body, 10)
	alerts, total, truncated := d.alerts, d.total, d.truncated
	if err != nil || total != 3 || truncated || len(alerts) != 3 || len(d.muted) != 0 {
		t.Fatalf("decodeAlerts = %d alerts, total %d, truncated %v, %d muted, %v", len(alerts), total, truncated, len(d.muted), err)
	}
	a, b, dd := alerts[0], alerts[1], alerts[2]
	if a.Name != "A" || a.Severity != SeverityCritical || a.Summary != "s" || a.Description != "d" || a.Runbook != "https://r" ||
		!a.StartsAt.Equal(at) || a.Labels["alertname"] != "A" || a.SeverityLabel != "critical" {
		t.Errorf("A = %+v", a)
	}
	if b.Severity != SeverityWarning || b.SeverityLabel != "major" || b.Description != "old style" || len(b.Summary) > maxText || !strings.HasPrefix(long, b.Summary) {
		t.Errorf("B = %q %q, summary %d bytes", b.Severity, b.Description, len(b.Summary))
	}
	if dd.Severity != SeverityWarning || dd.SeverityLabel != "" {
		t.Errorf("D without a severity = %+v", dd)
	}

	// At most max kept; total counts them all.
	d, _ = decodeAlerts(body, 2)
	if len(d.alerts) != 2 || d.total != 3 || !d.truncated {
		t.Errorf("bounded: %d alerts, total %d, truncated %v", len(d.alerts), d.total, d.truncated)
	}
	// An empty list is an Alertmanager without alerts.
	if d, err := decodeAlerts([]byte(`[]`), 10); err != nil || d.total != 0 || len(d.alerts) != 0 {
		t.Errorf("empty: %v, %d, %v", d.alerts, d.total, err)
	}
	for _, other := range []string{`{"status":"success"}`, `[{"name":"x"}]`, `<html>`, `[1,2]`, `{"data":[]}`} {
		if _, err := decodeAlerts([]byte(other), 10); err != errNotAlertmanager {
			t.Errorf("%s: %v, want errNotAlertmanager", other, err)
		}
	}
}

// The silenced alerts are apart (Snapshot.Muted): not shown, not counted in total, the meta alerts
// among them kept (a silence of Watchdog mutes something), their silences' ids kept.
func TestDecodeAlertsMuted(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	muted := func(name, severity string, ids ...string) map[string]any {
		a := wireAlertOf(name, severity, at, nil)
		a["status"] = map[string]any{"state": "suppressed", "silencedBy": ids, "inhibitedBy": []string{}}
		return a
	}
	body := wire(t, wireAlertOf("A", "critical", at, nil), muted("B", "warning", "s-1"), muted("Watchdog", "none", "s-2", "s-3"))
	d, err := decodeAlerts(body, 10)
	if err != nil || d.total != 1 || len(d.alerts) != 1 || d.alerts[0].Name != "A" || len(d.alerts[0].SilencedBy) != 0 {
		t.Fatalf("shown: %+v, %v", d, err)
	}
	if d.mutedTotal != 2 || len(d.muted) != 2 || d.muted[0].Name != "B" || d.muted[0].SilencedBy[0] != "s-1" ||
		d.muted[1].Name != "Watchdog" || d.muted[1].Severity != "" || len(d.muted[1].SilencedBy) != 2 {
		t.Errorf("muted: %+v", d.muted)
	}
	d, _ = decodeAlerts(body, 1)
	if len(d.muted) != 1 || d.mutedTotal != 2 || len(d.alerts) != 1 {
		t.Errorf("bounded muted: %d of %d", len(d.muted), d.mutedTotal)
	}
}

func TestBoundLabels(t *testing.T) {
	in := map[string]string{"alertname": "A", "severity": "warning", "namespace": "ns"}
	for i := range 100 {
		in["l"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+strings.Repeat("y", i/26)] = strings.Repeat("v", 2000)
	}
	out := boundLabels(in)
	if len(out) > maxLabels || out["alertname"] != "A" || out["severity"] != "warning" || out["namespace"] != "ns" {
		t.Errorf("%d labels, alertname %q", len(out), out["alertname"])
	}
	for k, v := range out {
		if len(v) > maxLabel {
			t.Errorf("%s: %d bytes", k, len(v))
		}
	}
}
