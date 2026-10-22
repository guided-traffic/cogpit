package engine

import (
	"context"
	"fmt"
)

// A connect of the cluster the window's connect waits for already (the lead's finding of
// 2026-10-22, in the family of the owner's AW16): a second cluster.connect of a cluster that is
// connecting, from the navigator's section, the palette or an agent, joined the connect run under
// way (no second login, D-97) but ended the first connect's wait as a connect of another cluster
// does, with ErrConnectInBackground, and the window toasted "another cluster was shown; the
// connect goes on in the background" although nothing else was shown. Now the second connect joins
// the first's wait instead: the first goes on waiting, and both answer with the first's answer
// (connected, its failure, cancelled, or in the background when another cluster is shown after
// all). Nothing about the cluster changes: the run, its login and the window's state are the
// first's.

// windowConnect is the window's latest connect while it runs (Session.connect, state.connectWait).
type windowConnect struct {
	name string
	ctx  context.Context // its wait: ends when a newer connect, a show, Disconnect or the close ends it
	done chan struct{}   // closed once it returned; err holds its answer then
	err  error
	// joined counts the connects of the same cluster that wait for its answer (guarded by state.mu).
	joined int
}

// join waits for w's answer and returns it; its own ctx's end stops only this wait.
func (w *windowConnect) join(ctx context.Context) error {
	select {
	case <-w.done:
		return w.err
	case <-ctx.Done():
		return fmt.Errorf("connect %s: %w", w.name, context.Cause(ctx))
	}
}

// endWindowConnect records w's answer for the connects that joined it; a connect that comes later
// is a connect of its own.
func (st *state) endWindowConnect(w *windowConnect, err error) {
	st.mu.Lock()
	if st.connectWait == w {
		st.connectWait = nil
	}
	st.mu.Unlock()
	w.err = err
	close(w.done)
}
