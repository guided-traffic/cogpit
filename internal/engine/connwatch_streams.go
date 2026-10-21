package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/terminal"
)

// What an interrupted connection does to the streams and operations that use it (D-58, S7, S8):
//
//   - pod shells, node shells and attach end with a note in their tab and a Reconnect button that
//     opens a new session in the same tab (dock.reconnect): a process cannot be resumed. A node
//     shell's pod stays for that (it ends by its deadline, or when its tab closes). Local terminals
//     are not affected.
//   - log streams wait for the connection and go on from their last line (logsresume.go).
//   - port forwards do not dial while the connection is interrupted and dial at once once it is
//     back (portforward_run.go).
//   - a drain or a Helm install, upgrade or rollback stops at the interruption, as after a crash
//     (D-11): its journal keeps it, and once the connection is back the banner offers to finish
//     or dismiss it (a Helm action through OperationContext; a drain runs as a job, which the
//     interruption stops, jobs.go and commands/drainjob.go).

// ErrConnectionLost ends an operation's context when the connection is interrupted
// (OperationContext): nothing continues by itself.
var ErrConnectionLost = errors.New("the connection to the cluster was interrupted")

// trackedSession is a pod shell's, node shell's or attach session the watch ends when the
// connection is interrupted.
type trackedSession struct {
	terminal.Session
	w      *connWatch
	tab    string
	cancel context.CancelFunc // the context the session was started with: a dial still under way ends
	ended  chan struct{}      // closed when the interruption ended it
	once   sync.Once
	gone   sync.Once
}

// trackSession registers a session for its tab; the returned session ends with the connection.
func (w *connWatch) trackSession(tab string, sess terminal.Session, cancel context.CancelFunc) terminal.Session {
	t := &trackedSession{Session: sess, w: w, tab: tab, cancel: cancel, ended: make(chan struct{})}
	if w == nil {
		return t
	}
	w.mu.Lock()
	if w.state != Interrupted {
		w.sessions[t] = struct{}{}
		w.mu.Unlock()
		return t
	}
	w.mu.Unlock()
	go w.s.endSessions(w, []*trackedSession{t}, w.s.Now()) // interrupted while it opened
	return t
}

// Close closes the session once and forgets it.
func (t *trackedSession) Close() error {
	t.untrack()
	var err error
	t.once.Do(func() {
		t.cancel()
		err = t.Session.Close()
	})
	return err
}

// untrack removes the session from its watch.
func (t *trackedSession) untrack() {
	t.gone.Do(func() {
		if t.w == nil {
			return
		}
		t.w.mu.Lock()
		delete(t.w.sessions, t)
		t.w.mu.Unlock()
	})
}

// interrupted reports whether the interruption ended the session.
func (t *trackedSession) interrupted() bool {
	select {
	case <-t.ended:
		return true
	default:
		return false
	}
}

func (w *connWatch) takeSessionsLocked() []*trackedSession {
	out := make([]*trackedSession, 0, len(w.sessions))
	for t := range w.sessions {
		out = append(out, t)
	}
	clear(w.sessions)
	return out
}

// endSessions ends the sessions an interruption caught: their tabs say when, then the sessions
// close (the tab's terminal shows that its session ended).
func (s *Session) endSessions(w *connWatch, sessions []*trackedSession, at time.Time) {
	if len(sessions) == 0 {
		return
	}
	ids := make(map[string]bool, len(sessions))
	for _, t := range sessions {
		ids[t.tab] = true
		close(t.ended)
	}
	s.UpdateUI(func(u *UIState) {
		if u.connGen != w.gen {
			return
		}
		for i := range u.Dock {
			if ids[u.Dock[i].ID] {
				u.Dock[i].Interrupted = at
			}
		}
	})
	for _, t := range sessions {
		go func() {
			defer s.recoverTo("ending a shell")
			_ = t.Close()
		}()
	}
}

// OperationContext returns a context for a multi-step cluster operation (a Helm action; a drain
// runs as a job, which the interruption stops) on a connected context, "" the shown one (D-69),
// that ends, with ErrConnectionLost as its cause, when that cluster's connection is interrupted or
// its login expires: the operation stops there and stays in its journal (D-11, S8). cancel
// releases it.
func (s *Session) OperationContext(ctx context.Context, contextID string) (context.Context, context.CancelFunc) {
	w := s.connWatch(contextID)
	ctx, cancel := context.WithCancelCause(ctx)
	if w == nil {
		return ctx, func() { cancel(nil) }
	}
	w.mu.Lock()
	broken := w.broken
	w.mu.Unlock()
	stop := make(chan struct{})
	go func() {
		select {
		case <-broken:
			cancel(ErrConnectionLost)
		case <-stop:
		case <-ctx.Done():
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() { close(stop) })
		cancel(nil)
	}
}

// errWatchEnded ends a wait for the connection whose backend closed.
var errWatchEnded = errors.New("the connection closed")

// await waits, after a stream of the connection opened at since broke, until the connection is
// usable: it asks for a check at once and waits for its verdict, and while the connection is
// interrupted, its login expired or its login lapsed (D-69 item 4), until it is back. It returns
// the interruption the stream lived through (from, to): the one it waited out, or one that ended
// after since, since a stream can break only after the connection is back (its TCP connection
// outlived the interruption); zero when the connection did not break; errWatchEnded when the
// backend closed, or ctx's error. lapsed, when given, is called once before it waits for a lapsed
// login: the stream says so in a line of its own. An interruption it waited out counts without
// comparing times: Windows' clock moves in steps of up to 16 ms, so one that ended within the
// stream's first step would end at since, not after it.
func (w *connWatch) await(ctx context.Context, since time.Time, lapsed func()) (from, to time.Time, err error) {
	if w == nil {
		return time.Time{}, time.Time{}, nil
	}
	ch := make(chan struct{})
	w.mu.Lock()
	if w.state == Connected {
		w.waiters = append(w.waiters, ch)
	} else {
		close(ch)
	}
	w.mu.Unlock()
	w.kick(kickStream)
	select {
	case <-ch:
	case <-ctx.Done():
		return time.Time{}, time.Time{}, ctx.Err()
	case <-w.done:
		return time.Time{}, time.Time{}, errWatchEnded
	}
	waited := false // back closed: only reconnect closes it, after it recorded lastFrom and lastTo
	for {
		w.mu.Lock()
		st, back, lf, lt := w.state, w.back, w.lastFrom, w.lastTo
		w.mu.Unlock()
		if st == Connected {
			if waited || lt.After(since) {
				return lf, lt, nil
			}
			return time.Time{}, time.Time{}, nil
		}
		if st == stateLapsed && lapsed != nil {
			lapsed()
			lapsed = nil
		}
		select {
		case <-back:
			waited = true
		case <-ctx.Done():
			return time.Time{}, time.Time{}, ctx.Err()
		case <-w.done:
			return time.Time{}, time.Time{}, errWatchEnded
		}
	}
}

// nodeShellSettleWait bounds how long an ended node shell waits for the connection's verdict before
// its pod is deleted or kept.
const nodeShellSettleWait = 15 * time.Second

// settle asks for a check and returns the connection's state once the check decided, or after
// bound: whether a stream that just ended broke with the connection. Nil-safe (Connected).
func (w *connWatch) settle(bound time.Duration) ConnectionState {
	if w == nil {
		return Connected
	}
	ch := make(chan struct{})
	w.mu.Lock()
	if w.state != Connected {
		w.mu.Unlock()
		return w.current()
	}
	w.waiters = append(w.waiters, ch)
	w.mu.Unlock()
	w.kick(kickStream)
	t := time.NewTimer(bound)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-w.done:
	}
	return w.current()
}

// usable returns, for a backend's port forwards, a channel closed once the connection is Connected
// (closed already while it is) and one closed once it is not (closed already while it is not).
func (w *connWatch) usable() (back, broken <-chan struct{}) {
	if w == nil {
		open := make(chan struct{})
		close(open)
		return open, make(chan struct{})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.back, w.broken
}
