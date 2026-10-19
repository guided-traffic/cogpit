package engine

import (
	"context"
	"errors"
	"fmt"
)

// A connect that shows nothing (D-69 stage 3: the restore of the last session connects the shown
// cluster first and the others behind it, restore.go). The cluster joins the pool as a cluster the
// window left: its heartbeat, discovery, access reviews and login hold (connwatch_lapse.go) start,
// its views are not listed until it is shown (it is cold at once), and the window's state stays
// as it is. UIState.Connections has its entry while it connects (connecting, so that its marks
// blink) and once it is connected; a connect that fails leaves none and says why in a toast.
//
// Its connect is the user's own connect of that cluster, made at start: the connector asks the
// kubeconfig's credential plugin as on any connect (an approved one runs; one not allowed yet, or
// whose program changed, is refused with the reason, D-26), and nothing else is sent. A connect the
// user starts meanwhile for the same context waits for it and shows its connection (awaitRun)
// rather than running a second one beside it: both are the same connect run (connect_run.go).

// bgGenBase is where the numbers of connections start (newGenLocked): a range of their own, so
// that they never take a window connect's sequence number, which decides which connect is current.
const bgGenBase = uint64(1) << 62

// errConnectedAlready is ConnectBackground's answer for a context the pool holds already.
var errConnectedAlready = errors.New("connected already")

// ConnectBackground connects a context without showing it: the cluster joins the pool as one the
// window left. A context that is connected already, or whose connect is under way, is left alone
// (errConnectedAlready for the former, nil for the latter). It returns once the connect ended; a
// connect the window began to wait for meanwhile says how it ended itself, and this returns nil.
func (s *Session) ConnectBackground(ctx context.Context, name string) error {
	connector := s.opts.Connector
	if connector == nil {
		return errors.New("no connector configured")
	}
	if r, ok := connector.(contextResolver); ok {
		resolved, err := r.ResolveContext(name)
		if err != nil {
			return fmt.Errorf("connect %s: %w", name, err)
		}
		name = resolved
	}
	st := s.state
	st.mu.Lock()
	switch {
	case st.closed:
		st.mu.Unlock()
		return ErrClosed
	case st.pool[name] != nil:
		st.mu.Unlock()
		return errConnectedAlready
	case st.runs[name] != nil:
		st.mu.Unlock()
		return nil // the window connects it already, or another background connect
	}
	r := st.newRunLocked(ctx, name, true)
	st.mu.Unlock()
	s.startConnectRun(r)
	select {
	case <-r.done:
	case <-ctx.Done():
		r.cancel(context.Cause(ctx))
		<-r.done
	}
	st.mu.Lock()
	err, e, watched := r.err, r.entry, r.watched
	st.mu.Unlock()
	switch {
	case watched:
		return nil // the window's connect waited for it and says how it ended
	case err != nil:
		if c := context.Cause(r.ctx); errors.Is(c, errConnectCancelled) {
			return fmt.Errorf("connect %s: %w", name, errConnectCancelled)
		}
		return fmt.Errorf("connect %s: %w", name, err)
	case e != nil && e.b.Context != name:
		return errConnectedAlready
	}
	return nil
}
