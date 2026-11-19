package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A background cluster's login, held (D-69 item 4; the owner: by default a background cluster's
// login is renewed as the shown cluster's is, configurable). The cluster setting "Renew the login
// while the cluster is not shown" (ClusterSettings.HoldBackgroundLogin, its negation, so that the
// default is a zero value) is on by default. Off, a connected cluster that the window does not show
// holds its credential plugin (cluster.Conn.HoldLogin): a login that expires by its time, or that
// the API server rejects, is not renewed, so no browser login pops up while the user works with
// another cluster. The cluster's watch then lapses (stateLapsed):
//
//   - the UI state shows the connection Connected with ConnectedCluster.LoginLapsed and the reason
//     lapsedReason: grey, not red (the owner: red says the user must act, and whether a renewal
//     would work is not known);
//   - the heartbeat pauses (a request would only fail), the gates hold the informers' lists and
//     watches and refuse every other request with cluster.ErrLoginLapsed, writes are disabled with
//     "login lapsed: show the cluster to renew it";
//   - a stream that is open goes on (the API server checked the login when it opened); one that has
//     to open again waits with a notice line (logsresume.go, workloadlogs.go), port forwards do not
//     dial, a drain or a Helm action stops as at an interruption (D-11);
//   - showing the cluster renews the login as D-58 renews the shown cluster's: the approved plugin
//     runs once (one whose program changed asks first, D-26), LoginExpired with Log in again when
//     that does not help within 30 s; Log in again (cluster.login with the context) renews it with
//     the plugin's browser for as long as the user takes; turning the setting on renews it in the
//     background. An agent's read or write never renews it: it gets the lapsed login's error, and
//     its reads from the cache a note (lapsedNote).

// lapse makes the login lapsed: the plugin was held when the login needed it. Coming from an
// interruption, its time stays.
func (w *connWatch) lapse() {
	now := w.s.Now()
	w.mu.Lock()
	if w.state == Connected {
		close(w.broken)
		w.back = make(chan struct{})
		w.since = now
	} else if w.since.IsZero() {
		w.since = now
	}
	w.state, w.reason, w.needsApproval = stateLapsed, lapsedReason, false
	w.next = time.Time{}
	w.releaseWaitersLocked()
	held := w.held
	w.mu.Unlock()
	w.p.SetHealth(cluster.HealthLoginLapsed)
	w.logs().add(w.b.Context, LogInfo, "The login lapsed while the cluster was not shown; Cogpit renews it when the cluster is shown (Cluster Settings › Renew the login while the cluster is not shown is off)")
	w.s.log.Info("engine: login lapsed", "context", w.b.Context)
	w.publish()
	if !held {
		w.kick(kickRenew) // shown, or let go, meanwhile: renew it now
	}
}

// lapsedStep is one step while the login lapsed: nothing is asked of the cluster until the hold ends
// (the cluster is shown, or its setting is on again) or the user logs in again.
func (w *connWatch) lapsedStep() bool {
	for {
		select {
		case <-w.ctx.Done():
			return false
		case k := <-w.kicks:
			switch k {
			case kickRenew:
				if w.isHeld() {
					continue // held again before the renewal began
				}
				w.renewLapsed()
				return true
			case kickLogin:
				w.login()
				return true
			}
		}
	}
}

// renewLapsed renews a lapsed login as D-58 renews the shown cluster's: the approved credential
// plugin runs once (with a browser if it needs one) and the next check decides; a plugin that has
// not answered within LoginWait makes the login LoginExpired, and its answer later still reconnects.
func (w *connWatch) renewLapsed() {
	if w.approvalChanged() {
		w.expire("the login program of this context changed since you allowed it; log in again to review it", true)
		return
	}
	w.setRenewing(true)
	defer w.setRenewing(false)
	w.logs().begin(w.b.Context, AttemptLogin, LogInfo,
		"Renewing the login that lapsed while the cluster was not shown: the credential plugin may run once")
	w.p.ArmLogin()
	res, err := w.check(w.o.LoginWait)
	if res == checkRejected { // the guard handed this 401 to the plugin, which ran once: ask again
		res, err = w.check(w.o.LoginWait)
	}
	switch res {
	case checkQuit:
	case checkOK:
		w.reconnect()
	case checkDown:
		w.interrupt(err)
	case checkRejected:
		w.expire(rejectedReason(err), false)
	case checkWaiting:
		w.expire(fmt.Sprintf(loginWaitReason, w.o.LoginWait), false)
	case checkLapsed: // held again meanwhile: the cluster left before the renewal ran
		w.logs().end(w.b.Context, AttemptLogin, OutcomeAbandoned, LogInfo, "The cluster is not shown any more; the login stays lapsed")
		w.publish()
	case checkEnded: // Cogpit ended the plugin's run (D-73)
		w.expire(endedReason(err), false)
	}
}

// loginHolder is a connection whose credential plugin the watch can hold: *cluster.Conn. A fake
// prober of the tests may be one.
type loginHolder interface {
	HoldLogin(on bool)
}

// setHold holds the login, or lets it go: the engine's wish for a cluster that is not shown and
// whose setting is off (applyLoginHolds). Letting go of a lapsed login renews it. Nil-safe.
func (w *connWatch) setHold(on bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	changed := w.held != on
	w.held = on
	lapsed := w.state == stateLapsed
	w.applyHoldLocked()
	w.mu.Unlock()
	if changed && !on && lapsed {
		w.kick(kickRenew)
	}
}

// setRenewing marks a renewal that lets the plugin run although the login is held (Log in again on
// a cluster that is not shown).
func (w *connWatch) setRenewing(on bool) {
	w.mu.Lock()
	w.renewing = on
	w.applyHoldLocked()
	w.mu.Unlock()
	w.publish()
}

// applyHoldLocked tells the connection whether its plugin is held: held and no renewal running.
// w.mu is held; HoldLogin takes no engine lock.
func (w *connWatch) applyHoldLocked() {
	if h, ok := w.p.(loginHolder); ok {
		h.HoldLogin(w.held && !w.renewing)
	}
}

// isHeld reports whether the engine holds the login.
func (w *connWatch) isHeld() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.held
}

// holdsBackgroundLogin reports whether a context's setting holds its login while the cluster is
// not shown (Renew the login while the cluster is not shown, off).
func (s *Session) holdsBackgroundLogin(contextID string) bool {
	cs, err := s.ClusterSettingsOf(contextID)
	return err == nil && cs.HoldBackgroundLogin
}

// applyLoginHolds holds the logins of the connected clusters the window does not show whose
// setting says so, and lets go of the others' (D-69 item 4). It runs after every change of the
// shown cluster, of the pool and of the setting; letting go of a lapsed login renews it.
func (s *Session) applyLoginHolds() {
	st := s.state
	st.mu.Lock()
	shown := st.shown
	backends := make([]*Backend, 0, len(st.pool))
	for _, e := range st.pool {
		backends = append(backends, e.b)
	}
	st.mu.Unlock()
	for _, b := range backends {
		b.watch.setHold(b != shown && s.holdsBackgroundLogin(b.Context))
	}
}

// LoginLapsed reports whether a connected context's login lapsed while it was not shown (D-69
// item 4): the UI state's ConnectedCluster.LoginLapsed.
func (u *UIState) loginLapsed(contextID string) bool {
	if i := u.connectionIndex(contextID, 0); i >= 0 {
		return u.Connections[i].LoginLapsed
	}
	return false
}

// ErrRenewLoginGUIOnly refuses an agent's or the CLI's "Renew the login while the cluster is not
// shown" turned on: on, a cluster the user does not look at may run its credential plugin, a
// browser login included, so turning it on is the user's, in Cogpit's window. Turning it off (the
// plugin runs less) is open to them.
var ErrRenewLoginGUIOnly = errors.New("only the user, in Cogpit's own window, can turn on renewing the login while the cluster is " +
	"not shown (it lets the credential plugin run, a browser login included, while the user works with another cluster); " +
	"agents and the CLI may only turn it off")

// RefuseRenewLogin refuses turning on the renewal of a background cluster's login from anywhere
// but the user's own action in Cogpit's window.
func RefuseRenewLogin(ctx context.Context, on *bool) error {
	if on != nil && *on && OriginOf(ctx) != OriginGUI {
		return ErrRenewLoginGUIOnly
	}
	return nil
}
