package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// The steps a connect writes into the connection log (D-66). The cluster layer reports its own
// (cluster.Options.Trace: kubeconfig, server, login, certificate check, proxy, first answer, and the
// error picture of a failed one); the connector adds discovery and the kinds it offers; the session
// adds the access reviews' first answers and the heartbeat. Session.Connect hands the connector a
// recorder through the connect's context (withConnectSteps), so the Connector interface stays as it
// is and a connector that knows nothing of it (the demo, tests' fakes) records nothing.

// Steps of the engine's own (ConnectLogLine.Step).
const (
	StepDiscovery = "discovery"
	StepCache     = "cache"
	StepAccess    = "access"
	StepHeartbeat = "heartbeat"
)

// connectSteps records the steps of one connect into its attempt.
type connectSteps struct {
	logs    *connectLogs
	attempt *ConnectAttempt
	// cancelled reports whether the user cancelled the connect (Cancel Connecting, D-73): the
	// steps it ended are the user's own act, written INFO, not ERROR (D-66 keeps ERROR for a
	// connect that failed; the lead's note of the walk of 2026-10-19). Nil: never.
	cancelled func() bool
	// diag is the context's diagnostics log the connection notes in (diaglogs.go, D-133); nil: the
	// connection's own.
	diag *cluster.DiagLog
}

// cluster records one of the cluster layer's steps.
func (r *connectSteps) cluster(st cluster.ConnectStep) {
	if r == nil {
		return
	}
	if st.Name == cluster.StepStages { // the connect's stages, at its end (D-114)
		r.logs.stagesTo(r.attempt, connectStages(st.Stages))
		return
	}
	level := LogInfo
	switch {
	case (st.Failed || st.Warn) && r.cancelled != nil && r.cancelled():
	case st.Failed:
		level = LogError
	case st.Warn:
		level = LogWarn
	}
	r.logs.stepTo(r.attempt, level, st.Name, st.Text, st.Took)
}

// step records a step of the engine's own.
func (r *connectSteps) step(level, step, text string, took time.Duration) {
	if r != nil {
		r.logs.stepTo(r.attempt, level, step, text, took)
	}
}

type connectStepsKey struct{}

// withConnectSteps hands r to the connector through ctx.
func withConnectSteps(ctx context.Context, r *connectSteps) context.Context {
	return context.WithValue(ctx, connectStepsKey{}, r)
}

// connectStepsOf is the recorder of the connect ctx belongs to; nil records nothing.
func connectStepsOf(ctx context.Context) *connectSteps {
	r, _ := ctx.Value(connectStepsKey{}).(*connectSteps)
	return r
}

// traceFunc is r as the cluster layer's Trace; nil without a recorder.
func (r *connectSteps) traceFunc() func(cluster.ConnectStep) {
	if r == nil {
		return nil
	}
	return r.cluster
}

// logDiscovery records what discovery found and where it came from (the disk cache, D-65, or the
// server), or that it failed.
func (r *connectSteps) logDiscovery(conn *cluster.Conn, served int, took time.Duration, err error) {
	if r == nil {
		return
	}
	if err != nil {
		r.step(LogWarn, StepDiscovery, "Discovery failed, every built-in kind is offered: "+oneLine(err.Error()), took)
		return
	}
	info := conn.DiscoveryInfo()
	from := "asked the server"
	if info.Source == "disk" {
		from = "from the disk cache, fetched " + info.FetchedAt.UTC().Format(time.DateTime) + " UTC"
	}
	text := fmt.Sprintf("Discovery: %d API groups, %d resources (%d can be listed and watched), %s", info.Groups, info.Resources, served, from)
	if info.Partial {
		text += "; groups that did not answer are left out"
	}
	r.step(LogInfo, StepDiscovery, text, took)
}

// logConnectedSteps records what a connection that is about to stand starts: the kinds its cache
// offers, and the heartbeat of the connection watch (D-58).
func logConnectedSteps(r *connectSteps, b *Backend) {
	if r == nil || b == nil {
		return
	}
	text := fmt.Sprintf("Cache: %d kinds offered; their lists and watches start when a view needs them", len(b.Profiles))
	if b.custom != nil {
		text += "; the custom resource definitions are watched"
	}
	r.step(LogInfo, StepCache, text, 0)
	if w := b.watch; w != nil {
		r.step(LogInfo, StepHeartbeat, fmt.Sprintf("Heartbeat: GET /version every %s, %s to answer; two failed checks in a row "+
			"make the connection Interrupted (D-58)", w.o.Heartbeat, w.o.ProbeTimeout), 0)
	}
}

// noteAccessReviews has b's access reviews add their first answers to the connect's attempt.
func (s *Session) noteAccessReviews(b *Backend, r *connectSteps) {
	a, ok := b.Access.(*accessReviews)
	if !ok || r == nil {
		return
	}
	a.noteFirst(func(level, text string, took time.Duration) { r.step(level, StepAccess, text, took) })
}

// connectLogUnwritable says once that the connection log's file cannot be written: the session
// keeps the log in memory. It runs on its own goroutine, never under the log's lock.
func (s *Session) connectLogUnwritable(err error) {
	go func() {
		defer s.recoverTo("the connection log's warning")
		s.log.Warn("engine: the connection log cannot be written", "err", err)
		s.emit(Event{Type: EventToast, Level: LevelWarning,
			Message: "The connection log cannot be written to disk (" + oneLine(err.Error()) + "); this session keeps it in memory."})
	}()
}

// pluginStderrLines are the scrubbed standard error of a credential plugin that failed a check of
// the connection watch, as log lines; nil when err carries none.
func pluginStderrLines(err error) string {
	var pe *cluster.PluginError
	if !errors.As(err, &pe) || pe.Stderr == "" {
		return ""
	}
	return "The credential plugin's standard error (scrubbed, at most 4 KiB):\n" + pe.Stderr
}
