package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// What a connection waits for while its credential plugin runs, and the cancel of a connect that
// waits (D-73; the owner's finding of 2026-10-16 night: a connect waiting for an OIDC login that
// the user had abandoned blinked for ten minutes, without a timeout, and Disconnect waited behind
// it). The cluster layer follows each run of a context's plugin, ends it on a cancel and after its
// bound (cluster/loginrun.go); the session says it:
//
//   - the UI state: Connection.WaitingForLogin (the program, since, until) wherever the cluster's
//     connection shows, while connecting with Reason "waiting for the login through <program>; …";
//     agents read it in ui_state;
//   - the connection log: the run's begin in the connect's attempt, or in the attempt that runs (a
//     login, a reconnect), and how it ended;
//   - Disconnect of a cluster that connects (cluster.disconnect, the window's Cancel Connecting)
//     cancels the connect: the cluster layer ends the plugin's process, the connect returns at once
//     and its attempt says "cancelled"; a background connect of the restore too.

// errConnectCancelled is the cause of a connect that Disconnect cancelled.
var errConnectCancelled = errors.New("connecting cancelled")

// errConnectSuperseded is the cause of a connect that a newer connect or a show superseded.
var errConnectSuperseded = errors.New("superseded by a newer connect")

type loginWatchKey struct{}

// withLoginWatch hands the connect's watch of its credential plugin to the connector through ctx.
func withLoginWatch(ctx context.Context, w *cluster.LoginWatch) context.Context {
	return context.WithValue(ctx, loginWatchKey{}, w)
}

// loginWatchOf is the watch the connect of ctx gives its credential plugin; nil for none.
func loginWatchOf(ctx context.Context) *cluster.LoginWatch {
	w, _ := ctx.Value(loginWatchKey{}).(*cluster.LoginWatch)
	return w
}

// loginNotes follows one connection's plugin runs for the UI state and the connection log, from its
// connect on (Backend.login once it stands).
type loginNotes struct {
	s          *Session
	contextID  string
	steps      *connectSteps // the connect's attempt, while connecting
	lw         *cluster.LoginWatch
	connecting atomic.Bool
	reconnect  bool // a reconnect's connect (reconnect.go): the connection it replaces shows its waits

	mu     sync.Mutex
	logged time.Time // the Since of the run whose begin the log has
	closed time.Time // the Since of the run whose end the log has
}

// newLoginNotes makes the notes of a connect of contextID that writes into steps' attempt.
func (s *Session) newLoginNotes(contextID string, steps *connectSteps) *loginNotes {
	n := &loginNotes{s: s, contextID: contextID, steps: steps}
	n.connecting.Store(true)
	n.lw = cluster.NewLoginWatch(n.changed)
	return n
}

// connected says the connect's attempt ended: later runs write into the attempt that runs.
func (n *loginNotes) connected() {
	if n != nil {
		n.connecting.Store(false)
	}
}

// wait is the run under way as the UI state shows it; zero while none is. Nil-safe.
func (n *loginNotes) wait() LoginWait {
	if n == nil {
		return LoginWait{}
	}
	run, on := n.lw.Current()
	if !on {
		return LoginWait{}
	}
	return LoginWait{Program: run.Program, Since: run.Since, Until: run.Until}
}

// waitingReason is a connecting cluster's Reason while it waits for its plugin.
func waitingReason(w LoginWait) string {
	if w.Program == "" {
		return ""
	}
	return "waiting for the login through " + kubeconfig.DisplayText(w.Program, 80) + "; it may wait for you to log in, in a browser"
}

// changed follows a run's begin or end (the cluster layer's notify, on a goroutine of its own).
func (n *loginNotes) changed() {
	defer n.s.recoverTo("the login's notes")
	n.log()
	n.s.UpdateUI(func(u *UIState) {
		wait := n.wait() // read here: the latest of concurrent notices wins
		if u.Context == n.contextID && u.Connection.State == Connecting {
			u.Connection.WaitingForLogin, u.Connection.Reason = wait, waitingReason(wait)
		}
		if n.reconnect && n.connecting.Load() && u.Context == n.contextID && u.Connection.Reconnecting {
			u.Connection.WaitingForLogin = wait // the connection the reconnect replaces says what it waits for
		}
		for i := range u.Connections {
			c := &u.Connections[i]
			switch {
			case c.Context != n.contextID:
			case c.Connection.State == Connecting:
				c.Connection.WaitingForLogin, c.Connection.Reason = wait, waitingReason(wait)
			case n.reconnect && n.connecting.Load() && c.Connection.Reconnecting:
				c.Connection.WaitingForLogin = wait
			}
		}
	})
	if b := n.s.BackendOf(n.contextID); b != nil && b.login == n && b.watch != nil {
		b.watch.publish() // a connection that stands: its watch writes its Connection (connwatch.go)
	}
}

// log writes a run's begin, and the end of one that ran for a connection that stands; a connect's
// trace writes how its own run ended (cluster.Conn.traceLoginRun), and the watch writes a run
// Cogpit ended (its expire).
func (n *loginNotes) log() {
	run, on := n.lw.Current()
	tf := n.s.clockText
	n.mu.Lock()
	begin := on && !run.Since.Equal(n.logged)
	if begin {
		n.logged = run.Since
	}
	n.mu.Unlock()
	if begin {
		text := fmt.Sprintf("Login: waiting for %s since %s; it may wait for you to log in, in a browser. Cogpit ends it at %s "+
			"(after %s) unless it returns", kubeconfig.DisplayText(run.Program, 80), tf(run.Since), tf(run.Until),
			boundWords(run.Until.Sub(run.Since)))
		if n.connecting.Load() {
			n.steps.step(LogInfo, cluster.StepLogin, text+"; Cancel Connecting ends it now", 0)
		} else {
			n.s.state.logs.stepRunning(n.contextID, LogInfo, cluster.StepLogin, text)
		}
		return
	}
	if on || n.connecting.Load() {
		return
	}
	last, ended, by, ok := n.lw.Last()
	n.mu.Lock()
	write := ok && by == nil && last.Since.Equal(n.logged) && !last.Since.Equal(n.closed)
	if write {
		n.closed = last.Since
	}
	n.mu.Unlock()
	if write {
		n.s.state.logs.stepRunning(n.contextID, LogInfo, cluster.StepLogin, fmt.Sprintf("Login: %s answered after %s",
			kubeconfig.DisplayText(last.Program, 80), ended.Sub(last.Since).Round(100*time.Millisecond)))
	}
}

// boundWords writes a run's bound: "5 min", else Go's form ("200ms").
func boundWords(d time.Duration) string {
	if d > 0 && d%time.Minute == 0 {
		return fmt.Sprintf("%d min", d/time.Minute)
	}
	return d.String()
}

// endedReason is LoginExpired's reason when Cogpit ended the plugin's run (its bound, a cancel).
func endedReason(err error) string {
	var e *cluster.LoginEndedError
	if !errors.As(err, &e) {
		return "the login program was ended; log in again to start it again"
	}
	if e.RunsOn { // its process holds client-go's lock until it ends (H-118)
		return e.Error() + "; Log in again starts it again once that process ended"
	}
	return e.Error() + "; Log in again starts it again"
}
