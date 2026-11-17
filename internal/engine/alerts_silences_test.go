package engine

import (
	"slices"
	"testing"
	"time"
)

// A silence's length in the form's units: Go's durations, days and weeks; written back the same way.
func TestSilenceDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"2h": 2 * time.Hour, "90m": 90 * time.Minute, "1d": 24 * time.Hour, "1w": 7 * 24 * time.Hour,
		"1d12h": 36 * time.Hour, " 2H ": 2 * time.Hour, "1.5d": 36 * time.Hour, "1w2d3h4m": (9*24+3)*time.Hour + 4*time.Minute,
	} {
		got, err := ParseSilenceDuration(in)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "2", "h", "-1h", "0m", "1x"} {
		if d, err := ParseSilenceDuration(bad); err == nil {
			t.Errorf("%q accepted as %v", bad, d)
		}
	}
	for d, want := range map[time.Duration]string{2 * time.Hour: "2h", 24 * time.Hour: "1d", 36 * time.Hour: "1d12h",
		9 * 24 * time.Hour: "1w2d", 90 * time.Minute: "1h30m", 30 * time.Second: "1m"} {
		if got := FormatSilenceDuration(d); got != want {
			t.Errorf("%v = %q, want %q", d, got, want)
		}
	}
}

// A card's draft takes the labels all its alerts share, alertname first, namespace next, an empty
// value never.
func TestSharedLabels(t *testing.T) {
	got := sharedLabels([]map[string]string{
		{"alertname": "A", "namespace": "shop", "pod": "a-1", "severity": "warning", "job": "kube-state-metrics", "x": ""},
		{"alertname": "A", "namespace": "shop", "pod": "a-2", "severity": "warning", "job": "kube-state-metrics", "x": ""},
	})
	if want := []string{"alertname", "namespace", "job", "severity"}; !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if sharedLabels(nil) != nil {
		t.Error("labels of no alert")
	}
}

// The silences page's groups fold as the board's, the expired one folded until it is unfolded.
func TestSilenceGroupFolding(t *testing.T) {
	var a AlertBoardState
	if a.SilenceGroupFolded(SilenceGroupActive) || !a.SilenceGroupFolded(SilenceGroupExpired) {
		t.Fatalf("defaults: %+v", a)
	}
	a.SetSilenceGroupFolded(SilenceGroupExpired, false)
	a.SetSilenceGroupFolded(SilenceGroupActive, true)
	if !a.SilenceGroupFolded(SilenceGroupActive) || a.SilenceGroupFolded(SilenceGroupExpired) || a.SilenceGroupFolded(SilenceGroupPending) {
		t.Errorf("toggled: %+v", a)
	}
}
