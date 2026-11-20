package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// Connection Diagnostics' timings of the connection (D-133, cluster/diagstats.go): how long the API
// server takes to answer GET /version and the requests that open a container's stream, which of
// those wait now, and the HTTP client's connections, as the report and the tab write them.

// VersionText is GET /version's timings: "GET /version: 343 answered, median 45 ms, longest 1.2 s,
// latest 40 ms at 14:28:50".
func VersionText(l cluster.Latency, clock func(time.Time) string) string {
	if l.Count == 0 {
		return "GET /version: not asked yet"
	}
	return "GET /version: " + latencyText(l, "answered", clock)
}

// ConnectionsText is the HTTP client's connections: the protocol, how many it opened, how long the
// latest took to set up (its TCP connect and TLS handshake) and the longest.
func ConnectionsText(c cluster.ConnectionStats, clock func(time.Time) string) string {
	if c.Setup.Count == 0 {
		return "Connections: none opened yet"
	}
	out := "Connections: "
	if c.Proto != "" {
		out += c.Proto + " · "
	}
	out += fmt.Sprintf("%s opened since connecting, the latest at %s, set up in %s", nounCount(c.Setup.Count, "connection", "connections"),
		clock(c.Setup.LatestAt), FormatSpan(c.Setup.Latest))
	if c.TCP > 0 || c.TLS > 0 {
		out += fmt.Sprintf(" (TCP %s, TLS %s)", FormatSpan(c.TCP), FormatSpan(c.TLS))
	}
	if c.Setup.Count > 1 {
		out += ", the longest " + FormatSpan(c.Setup.Longest)
	}
	if c.Setup.Failed > 0 {
		out += fmt.Sprintf(" · %d TLS handshakes failed", c.Setup.Failed)
	}
	return out
}

// StreamText is the timings of one kind of stream: "logs: 9 opened, answered in median 120 ms,
// longest 14 s, latest 130 ms at 13:45:02".
func StreamText(s cluster.StreamStats, clock func(time.Time) string) string {
	return s.Kind + ": " + latencyText(s.Latency, "opened", clock)
}

// WaitingText is a request that opens a stream and has no answer yet, at now: what it is for, how
// long it waits, the request.
func WaitingText(w cluster.WaitingStream, now time.Time) string {
	return fmt.Sprintf("%s: %s · waiting %s for the API server's answer · %s %s", w.Kind, w.What,
		FormatSpan(max(now.Sub(w.Since), 0)), w.Method, w.URL)
}

// latencyText writes a Latency: how many, the median, the longest and the latest, the failed.
func latencyText(l cluster.Latency, done string, clock func(time.Time) string) string {
	parts := []string{fmt.Sprintf("%d %s", l.Count, done)}
	if l.Count > 1 {
		parts = append(parts, fmt.Sprintf("median %s, longest %s, latest %s at %s", FormatSpan(l.Median), FormatSpan(l.Longest),
			FormatSpan(l.Latest), clock(l.LatestAt)))
	} else {
		parts = append(parts, fmt.Sprintf("in %s at %s", FormatSpan(l.Latest), clock(l.LatestAt)))
	}
	out := strings.Join(parts, ", ")
	if l.Failed > 0 {
		out += fmt.Sprintf(" · %d failed", l.Failed)
	}
	return out
}

// nounCount writes a count with its noun: "1 connection", "3 connections".
func nounCount(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// StreamWaitPressed reports whether a request that opens a stream has waited SlowRequest or more
// for its answer at now: the tab shows its line in the warning colour.
func StreamWaitPressed(ts cluster.TransportStats, now time.Time) bool {
	return len(ts.Waiting) > 0 && now.Sub(ts.Waiting[0].Since) >= cluster.SlowRequest
}

// transportReport writes the report's section of the connection's timings.
func transportReport(line func(string, ...any), ts *cluster.TransportStats, now time.Time, clock func(time.Time) string) {
	line("\n== The API server's answers ==")
	if ts == nil {
		line("Not measured.")
		return
	}
	line("%s", VersionText(ts.Version, clock))
	line("%s", ConnectionsText(ts.Connections, clock))
	line("Streams, the time to the API server's answer (it opens a log, exec, attach or port forward at the kubelet of the pod's node first):")
	if len(ts.Streams) == 0 {
		line("  none opened")
	}
	for _, s := range ts.Streams {
		line("  %s", StreamText(s, clock))
	}
	line("Waiting for an answer now:")
	if len(ts.Waiting) == 0 {
		line("  none")
	}
	for _, w := range ts.Waiting {
		line("  %s", WaitingText(w, now))
	}
}

// AnswersLine is the Connection Diagnostics tab's line of the API server's answers, shorter than the
// report's: the streams that wait for an answer now first, then GET /version, each kind of stream
// opened and the connections; pressed while a stream has waited cluster.SlowRequest or more
// (StreamWaitPressed).
func AnswersLine(ts *cluster.TransportStats, now time.Time) (text string, pressed bool) {
	const head = "The API server's answers: "
	if ts == nil {
		return head + "not measured", false
	}
	var parts []string
	if len(ts.Waiting) > 0 {
		w := ts.Waiting[0]
		p := fmt.Sprintf("%s waiting %s for an answer", w.What, FormatSpan(max(now.Sub(w.Since), 0)))
		if n := len(ts.Waiting); n > 1 {
			p += fmt.Sprintf(" (and %d more)", n-1)
		}
		parts = append(parts, p)
	}
	short := func(name string, l cluster.Latency) string {
		if l.Count < 2 {
			return fmt.Sprintf("%s %s", name, FormatSpan(l.Latest))
		}
		return fmt.Sprintf("%s median %s, longest %s", name, FormatSpan(l.Median), FormatSpan(l.Longest))
	}
	if ts.Version.Count > 0 {
		parts = append(parts, short("GET /version", ts.Version))
	}
	for _, s := range ts.Streams {
		parts = append(parts, short(fmt.Sprintf("%s (%d)", s.Kind, s.Count), s.Latency))
	}
	if c := ts.Connections; c.Setup.Count > 0 {
		parts = append(parts, strings.TrimSpace(fmt.Sprintf("%s %s, the latest set up in %s", c.Proto,
			nounCount(c.Setup.Count, "connection", "connections"), FormatSpan(c.Setup.Latest))))
	}
	if len(parts) == 0 {
		return head + "none yet", false
	}
	return head + strings.Join(parts, " · "), StreamWaitPressed(*ts, now)
}
