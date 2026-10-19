package engine

import (
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// The connection watch's lines in the connection log (D-66): an interruption is an entry of its
// own with the error picture of the check that failed, ended at once, and the reconnect after it
// the next attempt (the owner's WF37 of 2026-10-19: each entry reads top down and ends in how it
// ended, the newest on top); the reconnect's tries are written when their reason changes (a reconnect that waits all night writes a line per change, not one every 30
// seconds); it ends when the connection is back or the login expired. A login the API server
// rejects begins a login attempt (connwatch.go), and a credential plugin that failed a check adds
// its scrubbed standard error to the attempt that runs.

// logInterrupted writes an interruption with its cause, then begins the reconnect after it.
func (w *connWatch) logInterrupted(reason string, cause error) {
	ctx := w.b.Context
	logs := w.logs()
	a := logs.begin(ctx, AttemptInterruption, LogWarn, "Connection interrupted: "+reason)
	for _, line := range cluster.ErrorPicture(cause) {
		logs.stepTo(a, LogWarn, cluster.StepError, line, 0)
	}
	if text := pluginStderrLines(cause); text != "" {
		logs.stepTo(a, LogWarn, cluster.StepPlugin, text, 0)
	}
	logs.finish(a, OutcomeInterrupted, LogWarn, "")
	logs.begin(ctx, AttemptReconnect, LogInfo, "Reconnecting to "+ctx)
	w.mu.Lock()
	w.loggedReason = reason
	w.mu.Unlock()
}

// logTry writes a failed reconnect try when its reason differs from the last one written.
func (w *connWatch) logTry(reason string, next time.Duration) {
	w.mu.Lock()
	same := reason == w.loggedReason
	w.loggedReason = reason
	w.mu.Unlock()
	if same {
		return
	}
	w.logs().add(w.b.Context, LogWarn, fmt.Sprintf("A try to reach the cluster failed: %s; the next in %s (tries "+
		"with the same reason are not written again)", reason, next))
}

// logPluginStderr adds a failed credential plugin's scrubbed standard error to the attempt that
// runs (a reconnect, a login). A check of a connection that stands has none: the interruption it
// may lead to writes its cause's (logInterrupted).
func (w *connWatch) logPluginStderr(err error) {
	if text := pluginStderrLines(err); text != "" {
		w.logs().stepRunning(w.b.Context, LogWarn, cluster.StepPlugin, text)
	}
}

// FormatSpan writes how long something took as the connection log's headers do: "55 ms", "2.4 s",
// "42 s", "10 min 2 s", "3 h 5 min" (one way everywhere, WF37: "after 0s" in a line under a header
// that said "succeeded after 55 ms").
func FormatSpan(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", max(d.Milliseconds(), 0))
	case d < 10*time.Second:
		return fmt.Sprintf("%.1f s", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Round(time.Second).Seconds()))
	case d < time.Hour:
		d = d.Round(time.Second)
		return fmt.Sprintf("%d min %d s", int(d.Minutes()), int(d.Seconds())%60)
	}
	d = d.Round(time.Minute)
	return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}
