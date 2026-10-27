package fyneui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The connection log's dialog (D-66): the attempts newest first, live while it is open, Copy, and
// the empty state of a context without a log.

const logCtx = "team [kubeconfigs/team.yaml]"

var logAt = time.Date(2026, 10, 16, 9, 41, 7, 0, time.UTC)

// successAttempt is a connect that worked, step by step.
func successAttempt(at time.Time) engine.ConnectAttempt {
	line := func(d time.Duration, step, text string, took int64) engine.ConnectLogLine {
		return engine.ConnectLogLine{Time: at.Add(d), Level: engine.LogInfo, Step: step, Text: text, TookMS: took}
	}
	return engine.ConnectAttempt{Kind: engine.AttemptConnect, Context: logCtx, Start: at, End: at.Add(1430 * time.Millisecond),
		TookMS: 1430, Outcome: engine.OutcomeOK, Lines: []engine.ConnectLogLine{
			line(0, "", "Connecting to "+logCtx, 0),
			line(5*time.Millisecond, "kubeconfig", `Kubeconfig: context "team" (cluster "team", user "oidc") in /Users/me/kubeconfigs/team.yaml`, 0),
			line(6*time.Millisecond, "server", "API server: https://team.example:6443", 0),
			line(6*time.Millisecond, "login", "Login: credential plugin kubelogin (allowed by you (D-26)), interactive mode IfAvailable", 0),
			line(6*time.Millisecond, "tls", "TLS: the server's certificate is checked against the kubeconfig's certificate-authority-data (CN=kubernetes, valid until 2035-08-01)", 0),
			line(6*time.Millisecond, "proxy", "Proxy: none, a direct connection", 0),
			line(860*time.Millisecond, "answer", "First answer: GET /version: Kubernetes v1.36.1", 852),
			line(1210*time.Millisecond, "discovery", "Discovery: 31 API groups, 214 resources (96 can be listed and watched), from the disk cache, fetched 2026-10-16 08:12:40 UTC", 348),
			line(1420*time.Millisecond, "cache", "Cache: 52 kinds offered; their lists and watches start when a view needs them; the custom resource definitions are watched", 0),
			line(1420*time.Millisecond, "heartbeat", "Heartbeat: GET /version every 15s, 5s to answer; two failed checks in a row make the connection Interrupted (D-58)", 0),
			line(1430*time.Millisecond, "", "Connected to "+logCtx, 0),
			line(1610*time.Millisecond, "access", "Access reviews: the rules of 2 namespace(s) answered", 176),
		}}
}

// failedAttempt is a login that failed in the credential plugin, with its error picture.
func failedAttempt(at time.Time) engine.ConnectAttempt {
	line := func(d time.Duration, level, step, text string, took int64) engine.ConnectLogLine {
		return engine.ConnectLogLine{Time: at.Add(d), Level: level, Step: step, Text: text, TookMS: took}
	}
	i, e := engine.LogInfo, engine.LogError
	return engine.ConnectAttempt{Kind: engine.AttemptConnect, Context: logCtx, Start: at, End: at.Add(2210 * time.Millisecond),
		TookMS: 2210, Outcome: engine.OutcomeFailed, Lines: []engine.ConnectLogLine{
			line(0, i, "", "Connecting to "+logCtx, 0),
			line(4*time.Millisecond, i, "kubeconfig", `Kubeconfig: context "team" (cluster "team", user "oidc") in /Users/me/kubeconfigs/team.yaml`, 0),
			line(5*time.Millisecond, i, "server", "API server: https://team.example:6443", 0),
			line(5*time.Millisecond, i, "login", "Login: credential plugin kubelogin (allowed by you (D-26)), interactive mode IfAvailable", 0),
			line(5*time.Millisecond, i, "tls", "TLS: the server's certificate is checked against the kubeconfig's certificate-authority-data (CN=kubernetes, valid until 2035-08-01)", 0),
			line(5*time.Millisecond, i, "proxy", "Proxy: none, a direct connection", 0),
			line(2200*time.Millisecond, e, "answer", "First answer: GET /version failed (the limit is 15s)", 2195),
			line(2200*time.Millisecond, e, "error", "Class: the credential plugin failed: exec: executable kubelogin failed with exit code 1", 0),
			line(2200*time.Millisecond, e, "error", "Error chain: *url.Error › *errors.errorString", 0),
			line(2200*time.Millisecond, e, "plugin", "The credential plugin's standard error (scrubbed, at most 4 KiB):", 0),
			line(2200*time.Millisecond, e, "plugin", "  error: get-token: AADSTS50173: the provided grant has expired due to it being revoked", 0),
			line(2200*time.Millisecond, e, "plugin", "  open https://login.example/devicelogin and enter the code •••", 0),
			line(2210*time.Millisecond, e, "", `context "team": cannot reach the API server https://team.example:6443: getting credentials: exec: executable kubelogin failed with exit code 1`, 0),
		}}
}

// logFake is a fake engine whose cluster.connectLog answers with attempts, which the test changes.
type logFake struct {
	*fakeEngine
	mu       sync.Mutex
	attempts []engine.ConnectAttempt
}

func newLogFake(t *testing.T, attempts ...engine.ConnectAttempt) *logFake {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	lf := &logFake{fakeEngine: f, attempts: attempts}
	f.handlers["cluster.connectLog"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		lf.mu.Lock()
		defer lf.mu.Unlock()
		return map[string]any{"context": logCtx, "lines": []any{}, "attempts": append([]engine.ConnectAttempt{}, lf.attempts...)}, nil
	}
	return lf
}

func (lf *logFake) set(attempts ...engine.ConnectAttempt) {
	lf.mu.Lock()
	lf.attempts = attempts
	lf.mu.Unlock()
}

// The dialog follows the log while it is open: a running attempt's new lines and its end appear
// after the engine's EventConnectLog; another context's event reads nothing.
func TestConnectLogDialogFollowsTheLog(t *testing.T) {
	running := successAttempt(logAt)
	running.Lines, running.End, running.TookMS, running.Outcome, running.Running = running.Lines[:3], time.Time{}, 0, "", true
	lf := newLogFake(t, running)
	sh, _, q := newTestShell(t, lf.fakeEngine, Options{})
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	cl := sh.kc.connectLog
	if a := cl.view.attempts(); len(a) != 1 || !a[0].Running || len(a[0].Lines) != 3 {
		t.Fatalf("the first read: %+v", cl.view)
	}
	reads := len(lf.callsOf("cluster.connectLog"))
	lf.emit(engine.Event{Type: engine.EventConnectLog, View: "another context"})
	q.drain()
	if n := len(lf.callsOf("cluster.connectLog")); n != reads {
		t.Errorf("another context's change read the log: %d reads", n)
	}
	lf.set(successAttempt(logAt))
	lf.emit(engine.Event{Type: engine.EventConnectLog, View: logCtx})
	q.until(t, "the finished attempt", func() bool {
		a := cl.view.attempts()
		return len(a) == 1 && !a[0].Running && a[0].Outcome == engine.OutcomeOK && len(a[0].Lines) == 12
	})
	if sh.kc.connectLog != cl {
		t.Error("the dialog was opened again instead of updated")
	}
	test.Tap(cl.close)
	lf.emit(engine.Event{Type: engine.EventConnectLog, View: logCtx}) // a closed dialog reads nothing
	q.drain()
}

// Copy puts every attempt shown on the clipboard as plain text: the header with the outcome, the
// lines with their level and duration.
func TestConnectLogDialogCopy(t *testing.T) {
	lf := newLogFake(t, failedAttempt(logAt.Add(time.Hour)), successAttempt(logAt))
	sh, _, q := newTestShell(t, lf.fakeEngine, Options{})
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	test.Tap(sh.kc.connectLog.copy)
	got := sh.app.Clipboard().Content()
	for _, want := range []string{
		"Connection Updates: " + logCtx + "\n",
		"== Connect · ", "· failed after 2.2 s ==", "· succeeded after 1.4 s ==",
		"ERROR  Class: the credential plugin failed",
		"INFO   First answer: GET /version: Kubernetes v1.36.1  (852 ms)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the clipboard lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "failed after") > strings.Index(got, "succeeded after") {
		t.Errorf("the attempts are not newest first:\n%s", got)
	}
}

// A context without a log opens the dialog too, saying so.
func TestConnectLogDialogEmpty(t *testing.T) {
	lf := newLogFake(t)
	sh, _, q := newTestShell(t, lf.fakeEngine, Options{})
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	rows := sh.kc.connectLog.rows.Objects
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	if p, ok := rows[0].(*paragraph); !ok || !strings.HasPrefix(p.text, "Nothing was recorded for "+logCtx+" in the last 7 days") {
		t.Errorf("the empty state: %#v", rows[0])
	}
}

func TestRenderConnectionUpdatesFailure(t *testing.T) {
	lf := newLogFake(t, failedAttempt(logAt.Add(26*time.Hour)), successAttempt(logAt))
	sh, w, q := newTestShell(t, lf.fakeEngine, Options{})
	q.until(t, "the section's rows", func() bool { return sh.side.sec.loaded })
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	savePNG(t, w, "connection_updates_failure_dark.png")
}

// TestRenderConnectionUpdatesReconnect (WF37): a reconnect that succeeded after an interruption.
// The interruption is an entry of its own under the reconnect; each entry reads top down and ends
// in how it ended, its header in its outcome's colour, the durations written one way.
func TestRenderConnectionUpdatesReconnect(t *testing.T) {
	at := logAt.Add(2 * time.Hour)
	line := func(d time.Duration, level, step, text string) engine.ConnectLogLine {
		return engine.ConnectLogLine{Time: at.Add(d), Level: level, Step: step, Text: text}
	}
	interruption := engine.ConnectAttempt{Kind: engine.AttemptInterruption, Context: logCtx, Start: at, End: at,
		Outcome: engine.OutcomeInterrupted, Lines: []engine.ConnectLogLine{
			line(0, engine.LogWarn, "", "Connection interrupted: no answer from the API server"),
			line(0, engine.LogWarn, "error", "Class: timeout: the API server did not answer in time"),
			line(0, engine.LogWarn, "error", `Error chain: Get "https://team.example:6443/version": context deadline exceeded`),
			line(0, engine.LogWarn, "error", "Innermost error: context deadline exceeded"),
		}}
	reconnect := engine.ConnectAttempt{Kind: engine.AttemptReconnect, Context: logCtx, Start: at, End: at.Add(55 * time.Millisecond),
		TookMS: 55, Outcome: engine.OutcomeOK, Lines: []engine.ConnectLogLine{
			line(0, engine.LogInfo, "", "Reconnecting to "+logCtx),
			line(55*time.Millisecond, engine.LogInfo, "", "Reconnected to "+logCtx+" after 55 ms"),
			line(80*time.Millisecond, engine.LogInfo, "", "Logged in again as hfi@example.com (from the API server's SelfSubjectReview)"),
		}}
	lf := newLogFake(t, reconnect, interruption, successAttempt(logAt))
	sh, w, q := newTestShell(t, lf.fakeEngine, Options{})
	q.until(t, "the section's rows", func() bool { return sh.side.sec.loaded })
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	text := connectLogText(logCtx, sh.kc.connectLog.view, sh.timeFormat())
	if i, j := strings.Index(text, "== Reconnect"), strings.Index(text, "== Interruption"); i < 0 || j < i ||
		!strings.Contains(text, "succeeded after 55 ms") || !strings.Contains(text, "the connection was lost") {
		t.Errorf("the log:\n%s", text)
	}
	savePNG(t, w, "connection_updates_reconnect_dark.png")
}
