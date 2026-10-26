package engine

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
)

// Connection Diagnostics' report (D-133): what the tab shows, as plain text a colleague can paste
// into a ticket or save to a file, with the endpoints replaced (diagscrub.go). It holds the
// connection, the problems since the latest connect, Cogpit's rate limits, how the initial lists
// came, the diagnostics log, the watches, the requests and the context's latest connect attempts
// from the connection log (D-66). It is built from what Cogpit keeps: it sends no request.

// reportAttempts is how many of the context's latest connect attempts the report holds.
const reportAttempts = 3

// DiagnosticsReportText is the anonymised report.
type DiagnosticsReportText struct {
	Text string `json:"text"`
	// Name is the file name the report is offered to be saved under.
	Name string `json:"name"`
}

// DiagnosticsReportTextOf is the anonymised report of a connected context's cluster, "" the shown
// one; a context that is not connected is a NotConnectedError, the demo cluster has none.
func (s *Session) DiagnosticsReportTextOf(contextID string) (DiagnosticsReportText, error) {
	b := s.Backend()
	if contextID != "" {
		var err error
		if b, err = s.TargetBackend(contextID); err != nil {
			return DiagnosticsReportText{}, err
		}
	}
	if b == nil {
		return DiagnosticsReportText{}, &NotConnectedError{Context: contextID}
	}
	if b.diagLog() == nil {
		return DiagnosticsReportText{}, errNoDiagnostics
	}
	r, err := s.DiagnosticsReportOf(b.Context)
	if err != nil {
		return DiagnosticsReportText{}, err
	}
	attempts, _ := s.ConnectAttempts(b.Context, reportAttempts) // an unreadable file: this session's attempts
	now := time.Now()
	text := reportText(r, attempts, now)
	home, _ := os.UserHomeDir()
	scrub := newEndpointScrubber(home, text, r.Server)
	return DiagnosticsReportText{Text: scrub.scrub(text), Name: "cogpit-connection-diagnostics-" + now.Format("2006-01-02-150405") + ".txt"}, nil
}

// reportText writes the report with its endpoints still in it.
func reportText(r DiagnosticsReport, attempts []ConnectAttempt, now time.Time) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	clock := func(t time.Time) string { return t.Local().Format("15:04:05") }

	line("Cogpit Connection Diagnostics")
	line("Cogpit %s · %s/%s · written %s", buildinfo.Version(), runtime.GOOS, runtime.GOARCH, now.Local().Format(time.RFC3339))
	line("Endpoints are replaced: hosts, addresses, ports, the servers of URLs and file paths. Names of users, namespaces, objects and API groups are kept.")

	line("\n== Connection ==")
	line("Context: %s", r.Context)
	server := r.Server
	if r.ServerVersion != "" {
		server += " · Kubernetes " + r.ServerVersion
	}
	line("Server: %s", server)
	if !r.Since.IsZero() {
		line("Latest connect: %s (%s ago)", r.Since.Local().Format(time.RFC3339), FormatSpan(now.Sub(r.Since).Round(time.Second)))
	}
	if d := r.Discovery; d != nil && d.Source != "" {
		src := "from the server"
		if d.Source == "disk" {
			src = "from the disk cache"
		}
		if d.Partial {
			src += ", partial: some groups failed"
		}
		line("Discovery: %s", src)
	}
	verbs := make([]string, 0, len(r.RequestsByVerb))
	for _, v := range r.RequestsByVerb {
		verbs = append(verbs, fmt.Sprintf("%s %d", v.Name, v.Count))
	}
	line("API requests since connecting: %d (%s)", r.Requests, strings.Join(verbs, ", "))
	line("Watches: %d open, %d warm, %d covered; %d objects held", r.Open, r.Warm, r.Covered, r.Objects)

	line("\n== Problems since the latest connect ==")
	if len(r.ProblemCounts) == 0 {
		line("None.")
	}
	for _, c := range r.ProblemCounts {
		line("%-16s %d", DiagKindLabel(c.Name), c.Count)
	}

	line("\n== Cogpit's rate limits (watches never wait) ==")
	for _, bs := range r.Budgets {
		line("%s", BudgetText(bs))
	}

	line("\n== Initial lists of the open and warm watches ==")
	line("streamed by the server %d · listed %d · listed after the server did not stream them %d%s",
		r.Streaming.Streamed, r.Streaming.Listed, r.Streaming.FellBack, StreamsOffText(r.Streaming))

	line("\n== Log, the line seen last first (at most %d lines) ==", cluster.DiagLogLimit)
	if len(r.Log) == 0 {
		line("Nothing recorded.")
	}
	for _, e := range r.Log {
		line("%s", DiagEntryText(e, clock))
	}

	line("\n== Watches ==")
	for _, w := range r.Watches {
		line("%s", WatchText(w))
	}

	line("\n== API requests by verb and resource ==")
	for _, q := range r.RequestsByTarget {
		line("%8d  %s", q.Count, strings.TrimSpace(q.Verb+" "+q.Resource))
	}

	line("\n== Latest connect attempts (Connection Updates) ==")
	if len(attempts) == 0 {
		line("None recorded.")
	}
	for _, a := range attempts {
		head := a.Kind + " · " + a.Start.Local().Format(time.RFC3339)
		switch {
		case a.Running:
			head += " · running"
		case a.Outcome != "":
			head += " · " + a.Outcome
		}
		if a.TookMS > 0 {
			head += " after " + FormatSpan(time.Duration(a.TookMS)*time.Millisecond)
		}
		line("-- %s --", head)
		for _, st := range a.Stages {
			line("  stage %-10s %-10s %s", st.Name, st.Result, st.Text)
		}
		for _, l := range a.Lines {
			took := ""
			if l.TookMS > 0 {
				took = " (" + FormatSpan(time.Duration(l.TookMS)*time.Millisecond) + ")"
			}
			line("  %s  %-5s  %s%s", clock(l.Time), l.Level, l.Text, took)
		}
	}
	return b.String()
}

// DiagKindLabel names a kind of diagnostics line in a word or two.
func DiagKindLabel(kind string) string {
	switch k := cluster.DiagKind(kind); k {
	case cluster.DiagRefused:
		return "refused"
	case cluster.DiagUnauthorized:
		return "unauthorized"
	case cluster.DiagTooMany:
		return "too many (429)"
	case cluster.DiagWarming:
		return "cache warming"
	case cluster.DiagServerError:
		return "server error"
	case cluster.DiagTimeout:
		return "no answer"
	case cluster.DiagNetwork:
		return "network"
	case cluster.DiagSlow:
		return "slow answer"
	case cluster.DiagThrottled:
		return "rate limited"
	case cluster.DiagFallback:
		return "not streamed"
	case cluster.DiagSlowLoad:
		return "slow load"
	case cluster.DiagNamespaced:
		return "per namespace"
	case cluster.DiagConnect:
		return "connect"
	}
	return kind
}

// Severities of a diagnostics line (DiagSeverity): the UI's colour for it.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// DiagEntrySeverity is how bad a diagnostics line is: a line that is no problem is information (a
// connect, a kind read namespace by namespace, a cache warming up that did not repeat), else its
// kind's (DiagSeverity).
func DiagEntrySeverity(e cluster.DiagEntry) string {
	if !e.Problem {
		return SeverityInfo
	}
	return DiagSeverity(e.Kind)
}

// DiagSeverity is how bad a kind of diagnostics line is: a refusal, a request without an answer or
// a server's error is an error; a wait, a slow answer or load, a 429, a cache warming up that
// repeated and an initial list not streamed a warning; a connect and a kind read namespace by
// namespace information.
func DiagSeverity(k cluster.DiagKind) string {
	switch k {
	case cluster.DiagRefused, cluster.DiagUnauthorized, cluster.DiagTimeout, cluster.DiagNetwork, cluster.DiagServerError:
		return SeverityError
	case cluster.DiagConnect, cluster.DiagNamespaced:
		return SeverityInfo
	}
	return SeverityWarning
}

// BudgetPressed reports whether a rate limit holds a request back now, or held one back a second or
// more (cluster.ThrottleNote): the requests wait for Cogpit, not for the cluster.
func BudgetPressed(b cluster.BudgetStats) bool {
	return b.Waiting > 0 || b.Longest >= cluster.ThrottleNote
}

// DiagEntryText is a diagnostics line as the report writes it: when it was seen last, its kind,
// what it is about, how often and how long, what happened and the request, with its real values
// (the report replaces the endpoints after).
func DiagEntryText(e cluster.DiagEntry, clock func(time.Time) string) string {
	if e.Kind == cluster.DiagConnect {
		return fmt.Sprintf("%s  -- connecting to %s --", clock(e.Last), e.What)
	}
	parts := []string{fmt.Sprintf("%s  %-14s %s", clock(e.Last), DiagKindLabel(string(e.Kind)), e.What)}
	if e.Count > 1 {
		parts = append(parts, fmt.Sprintf("%d times since %s", e.Count, clock(e.First)))
	}
	if e.Status != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", e.Status))
	}
	if timed(e) {
		took := FormatSpan(e.Took)
		switch e.Kind {
		case cluster.DiagThrottled:
			took = "waited " + took
		case cluster.DiagSlowLoad:
			took = "loaded in " + took
		default:
			took = "took " + took
		}
		if e.Count > 1 && e.Longest > e.Took {
			took += ", longest " + FormatSpan(e.Longest)
		}
		parts = append(parts, took)
	}
	if e.RetryAfter != "" {
		parts = append(parts, "Retry-After "+e.RetryAfter)
	}
	if e.Text != "" {
		parts = append(parts, e.Text)
	}
	if e.URL != "" {
		parts = append(parts, e.Method+" "+e.URL)
	}
	return strings.Join(parts, " · ")
}

// timed reports whether a line's time says something: a wait, a load, a slow answer, a request
// without one, or any that took a second or more (a refusal's milliseconds do not).
func timed(e cluster.DiagEntry) bool {
	switch e.Kind {
	case cluster.DiagThrottled, cluster.DiagSlowLoad, cluster.DiagSlow, cluster.DiagTimeout:
		return e.Took > 0
	}
	return e.Took >= time.Second
}

// BudgetText is one of Cogpit's rate limits and what waited at it.
func BudgetText(bs cluster.BudgetStats) string {
	out := fmt.Sprintf("%s: %s per second, burst %d · %s", bs.Name, formatRate(bs.QPS), bs.Burst, requestsCount(bs.Requests))
	if bs.Delayed == 0 {
		return out + ", none waited"
	}
	out += fmt.Sprintf(", %d waited %s together, the longest %s", bs.Delayed, FormatSpan(bs.Waited), FormatSpan(bs.Longest))
	if bs.Waiting > 0 {
		out += fmt.Sprintf(" · %d waiting now", bs.Waiting)
	}
	return out
}

// WatchText is one watch as the report writes it: its scope, state, objects, how long its initial
// list took and how it came, and its error.
func WatchText(w DiagWatch) string {
	parts := []string{w.Scope, w.State, fmt.Sprintf("%d objects", w.Objects)}
	switch {
	case w.State == WatchCovered:
		parts = append(parts, "served by the "+w.CoveredBy+" watch")
	case !w.Synced:
		parts = append(parts, "loading")
	case w.LoadedIn > 0:
		parts = append(parts, "loaded in "+FormatSpan(w.LoadedIn)+InitialText(w.Initial))
	}
	if w.Error != "" {
		parts = append(parts, w.Error)
	}
	return strings.Join(parts, " · ")
}

// InitialText says how a watch's initial list came, after its load time: " (streamed)",
// " (listed)", " (listed, the server did not stream it)"; empty when not known.
func InitialText(initial string) string {
	switch initial {
	case cache.InitialStream:
		return " (streamed)"
	case cache.InitialList:
		return " (listed)"
	case cache.InitialFallback:
		return " (listed, the server did not stream it)"
	}
	return ""
}

// requestsCount writes a count of requests: "1 request", "3 requests".
func requestsCount(n int64) string {
	if n == 1 {
		return "1 request"
	}
	return fmt.Sprintf("%d requests", n)
}

// formatRate writes a rate without a needless fraction: "5", "2.5".
func formatRate(q float32) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", q), "0"), ".")
}

// StreamsOffText is what the initial lists' line adds while the connection lists every read
// instead of streaming it, and why (D-135); "" while it streams.
func StreamsOffText(s DiagStreaming) string {
	if s.Off == "" {
		return ""
	}
	return " · streams off, every read lists: " + s.Off
}
