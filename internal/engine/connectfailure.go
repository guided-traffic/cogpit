package engine

import (
	"errors"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A failed connect's page and the connect's stages (D-114, the owner's AW17 of 2026-10-22). The
// cluster layer measures a connect in stages (cluster/connectstages.go): the kubeconfig valid and
// complete, a credential plugin's login, the API server's name looked up (DNS), its address and
// port reached (TCP), the TLS handshake, and the API server's answer with the login. Each attempt
// of the connection log keeps them (ConnectAttempt.Stages), so Connection Updates shows them for
// every connect; a failed connect of the shown cluster keeps them in the UI state too
// (UIState.ConnectFailure), and the window shows its error page from them, whether or not a tab is
// open: the welcome page shows only while no cluster is chosen (the owner's choice (b)).
//
// The connection's error (Connection.Error) is the failure's cause in a sentence, the failed
// stage's text ("the client certificate file … does not exist"); the whole error chain is in the
// connection log, its attempt's last line (D-66).

// ConnectStage is one stage of a connect and its result (cluster.Stage).
type ConnectStage struct {
	// Name is the stage: kubeconfig, login (a credential plugin's), dns, tcp, tls or api.
	Name string `json:"name"`
	// Result is ok, failed, notReached, skipped (an address needs no lookup, plain http no TLS) or
	// cancelled.
	Result string `json:"result"`
	// Text says what the stage found, or why it failed; empty for one not reached. Never a credential.
	Text string `json:"text,omitempty"`
	// TookMS is how long it took, in milliseconds; 0 when it is not timed.
	TookMS int64 `json:"tookMs,omitempty"`
	// MissingProgram is the program a failed login did not find where Cogpit looks for programs:
	// the credential plugin itself, or the kubectl plugin kubectl, run as the plugin, looked for
	// (kubectl-oidc_login). Text then says where to add its folder (Preferences › Kubernetes ›
	// Program folders, programfolders.go, D-120); the error page offers to open it.
	MissingProgram string `json:"missingProgram,omitempty"`
}

// ConnectFailure is the shown cluster's last connect while it failed: what the window's error
// page shows (D-114).
type ConnectFailure struct {
	Context string         `json:"context"`
	At      time.Time      `json:"at"`
	Stages  []ConnectStage `json:"stages,omitempty"` // none when the failure came before the cluster layer measured any
}

func (f *ConnectFailure) clone() *ConnectFailure {
	if f == nil {
		return nil
	}
	out := *f
	out.Stages = slices.Clone(f.Stages)
	return &out
}

// connectStages converts the cluster layer's stages, each text escaped and capped as a log line.
func connectStages(in []cluster.Stage) []ConnectStage {
	if len(in) == 0 {
		return nil
	}
	out := make([]ConnectStage, len(in))
	for i, st := range in {
		text := st.Text
		if st.Missing != "" {
			text += programFoldersHint(st.Missing)
		}
		out[i] = ConnectStage{Name: st.Name, Result: st.Result, Text: kubeconfig.DisplayText(text, maxConnectLogText),
			TookMS: st.Took.Milliseconds(), MissingProgram: kubeconfig.DisplayText(st.Missing, maxConnectLogText)}
	}
	return out
}

// failedConnection is the connection of a connect that failed with err: its cause in a sentence
// where the cluster layer gave one, else msg (and always msg when override is set: the caller's
// own end); and its stages.
func failedConnection(err error, msg string, override bool) (Connection, []ConnectStage) {
	stages, cause := cluster.StagesOf(err)
	if cause == "" || override {
		cause = msg
	}
	out := connectStages(stages)
	for _, st := range out {
		if st.MissingProgram != "" && st.Result == cluster.StageFailed && !override {
			cause = st.Text // the cause with where to add the program's folder (D-120)
		}
	}
	return Connection{State: ConnectError, Error: oneLine(cause)}, out
}

// withConfigStages gives a connect that failed before the cluster layer could run (no source offers
// the context, its file does not parse, Cogpit's proxy for it is not a URL) its stages: the
// kubeconfig failed with the problem, every network stage not reached (cluster.ConfigStages).
func withConfigStages(err error) error {
	var ce *cluster.ConnectError
	if err == nil || !errors.As(err, &ce) || len(ce.Stages) > 0 {
		if err != nil && ce == nil {
			cause := oneLine(err.Error())
			return &cluster.ConnectError{Problem: err.Error(), Err: err, Stages: cluster.ConfigStages(cause), Cause: cause}
		}
		return err
	}
	out := *ce
	out.Cause = oneLine(ce.Problem)
	out.Stages = cluster.ConfigStages(out.Cause)
	return &out
}

// syncConnectFailure keeps ConnectFailure to the shown cluster's failed connect: it goes when the
// connection leaves the error or the window shows another cluster.
func (u *UIState) syncConnectFailure() {
	if f := u.ConnectFailure; f != nil && (u.Connection.State != ConnectError || f.Context != u.Context) {
		u.ConnectFailure = nil
	}
}

// stagesTo records a connect's stages in its attempt: in memory, and in the file one record each
// (recordStage), the whole list again when it is reported again.
func (l *connectLogs) stagesTo(a *ConnectAttempt, stages []ConnectStage) {
	if l == nil || a == nil || len(stages) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if a.Outcome == OutcomeAbandoned {
		return
	}
	a.Stages = slices.Clone(stages)
	now := l.clock()
	for _, st := range stages {
		l.writeLocked(a.Context, logRecord{Time: now, Attempt: a.id, Event: recordStage, Step: st.Name, Outcome: st.Result,
			Text: st.Text, TookMS: st.TookMS}, false)
	}
	l.changedLocked(a.Context)
}

// hasStages reports whether attempt a recorded its stages.
func (l *connectLogs) hasStages(a *ConnectAttempt) bool {
	if l == nil || a == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(a.Stages) > 0
}

// addStage puts a stage read from a file into a: a stage of the same name read before is replaced.
func (a *ConnectAttempt) addStage(st ConnectStage) {
	for i := range a.Stages {
		if a.Stages[i].Name == st.Name {
			a.Stages[i] = st
			return
		}
	}
	a.Stages = append(a.Stages, st)
}
