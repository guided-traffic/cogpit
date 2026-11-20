package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

func TestReportWritesTheAPIServersAnswers(t *testing.T) {
	now := time.Date(2026, 3, 4, 14, 29, 12, 0, time.UTC)
	at := now.Add(-time.Minute)
	ts := cluster.TransportStats{
		Version: cluster.Latency{Count: 343, Median: 45 * time.Millisecond, Longest: 1200 * time.Millisecond, Latest: 40 * time.Millisecond, LatestAt: at},
		Streams: []cluster.StreamStats{{Kind: cluster.StreamLogs, Latency: cluster.Latency{Count: 9, Failed: 1, Median: 120 * time.Millisecond,
			Longest: 14 * time.Second, Latest: 130 * time.Millisecond, LatestAt: at}}},
		Waiting: []cluster.WaitingStream{{Kind: cluster.StreamLogs, What: "get pods/web-0 (log) in team-a", Method: "GET",
			URL: "https://api.example.test/api/v1/namespaces/team-a/pods/web-0/log?follow=true", Since: now.Add(-47 * time.Second)}},
		Connections: cluster.ConnectionStats{Setup: cluster.Latency{Count: 2, Latest: 180 * time.Millisecond, Longest: 2100 * time.Millisecond, LatestAt: at},
			Proto: "HTTP/2.0", TCP: 20 * time.Millisecond, TLS: 150 * time.Millisecond},
	}
	r := ReportOf(Diagnostics{Context: "c"}, true)
	r.Transport = &ts
	clock := func(t time.Time) string { return t.UTC().Format("15:04:05") }
	var b strings.Builder
	transportReport(func(f string, a ...any) { b.WriteString(strings.TrimSpace(fmt.Sprintf(f, a...)) + "\n") }, r.Transport, now, clock)
	text := b.String()
	for _, want := range []string{
		"== The API server's answers ==",
		"GET /version: 343 answered, median 45 ms, longest 1.2 s, latest 40 ms at 14:28:12",
		"Connections: HTTP/2.0 · 2 connections opened since connecting, the latest at 14:28:12, set up in 180 ms (TCP 20 ms, TLS 150 ms), the longest 2.1 s",
		"logs: 9 opened, median 120 ms, longest 14 s, latest 130 ms at 14:28:12 · 1 failed",
		"logs: get pods/web-0 (log) in team-a · waiting 47 s for the API server's answer · GET https://api.example.test/api/v1/namespaces/team-a/pods/web-0/log?follow=true",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
	if !StreamWaitPressed(ts, now) || StreamWaitPressed(ts, now.Add(-45*time.Second)) {
		t.Error("a stream waiting 47 s presses, one waiting 2 s does not")
	}
	full := reportText(r, nil, now)
	if !strings.Contains(full, "== The API server's answers ==") || !strings.Contains(full, "waiting 47 s") {
		t.Errorf("the report lacks the section:\n%s", full)
	}
}

func TestReportOfAConnectionWithoutTimings(t *testing.T) {
	var b strings.Builder
	transportReport(func(f string, a ...any) { b.WriteString(fmt.Sprintf(f, a...) + "\n") }, &cluster.TransportStats{}, time.Now(), func(time.Time) string { return "" })
	for _, want := range []string{"GET /version: not asked yet", "Connections: none opened yet", "  none opened", "  none"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, b.String())
		}
	}
}

func TestSlowStreamLine(t *testing.T) {
	e := cluster.DiagEntry{Kind: cluster.DiagSlowStream, Problem: true, What: "get pods/web-0 (log) in team-a", Status: 200, Took: 12 * time.Second,
		Text: "the API server opens the stream at the kubelet of the pod's node before it answers", Method: "GET", URL: "https://x/api/v1/namespaces/team-a/pods/web-0/log",
		First: time.Unix(0, 0), Last: time.Unix(0, 0), Count: 1}
	got := DiagEntryText(e, func(time.Time) string { return "14:28:56" })
	if !strings.Contains(got, "slow stream") || !strings.Contains(got, "took 12 s") || DiagEntrySeverity(e) != SeverityWarning {
		t.Fatalf("line = %q, severity %s", got, DiagEntrySeverity(e))
	}
}
