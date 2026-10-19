package cluster

import (
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"k8s.io/client-go/plugin/pkg/client/auth/exec"
	"k8s.io/client-go/rest"
)

// A credential plugin held while its cluster is not shown (D-69 item 4, stage 3: the cluster
// setting "Renew the login while the cluster is not shown", off). The engine holds a background
// cluster's login (HoldLogin) so that no browser login pops up while the user works with another
// cluster; showing the cluster, or Log in again, lets the plugin run again.
//
// client-go's exec authenticator starts the plugin by itself in three places
// (k8s.io/client-go v0.37.1, plugin/pkg/client/auth/exec/exec.go): getCreds, when no credential is
// cached or the cached one expired by its time (exec.go:402); maybeRefreshCreds, after a 401
// (exec.go:419); and cert, for a client certificate in the TLS handshake, through getCreds
// (exec.go:394).
// The login guard (health.go) sees the 401 only; the other two run in client-go's round-tripper,
// above every round-tripper of Cogpit's but the outer gate, and in the transports of exec, attach,
// port forwards and Helm, in front of which Cogpit has none.
//
// So the hold sits where client-go starts the plugin: refreshCredsLocked asks the Authenticator's
// interactiveFunc before it builds the command, and runs nothing when it fails (exec.go:436–439).
// registerLoginHold wraps that unexported field once per Authenticator, under the Authenticator's
// own lock, as pluginstderr.go sets its stderr: while every connection that uses the Authenticator
// holds its login, the wrapper fails with errPluginHeld and no plugin process starts; each of
// those connections counts the refusal (loginGuard.lapses), so that the request ends as
// ErrLoginLapsed for the engine. A held connection's 401 is not handed to the plugin either: the
// login guard ends it as ErrLoginLapsed.
//
// The same wrapper starts a run only for a connection that asks for one (admit; WF38, the lead's
// walk of 2026-10-19): one that is open and that the user did not cancel (Conn.CancelLogin, a
// cancelled Connect, Close) since he last connected or logged in (ArmLogin). client-go's getCreds
// waits for the Authenticator's lock without its request's context, so a request of a cancelled
// connect that queued behind a run took the lock once that run ended and started the plugin anew,
// one run per cancelled connect, with nobody waiting for it. Such a request now fails with
// errLoginNotAsked and no process starts; a request that queued behind a run while another
// connection asks for one may start it, and the run then serves that connection.
//
// client-go keeps one Authenticator per plugin configuration (its command, arguments and
// environment, and the cluster when provideClusterInfo is set, exec.go:166–170): two contexts with
// the same plugin configuration share one login, and it is held only while every connection using
// it holds it. If a later client-go renames the field or its lock, nothing is wrapped (logged
// once): the 401 path stays held, the expiry path is not, and TestHeldPluginDoesNotRun fails.

// ErrLoginLapsed refuses a request of a connection whose login lapsed while it was held: the
// credential plugin was not run to renew it.
var ErrLoginLapsed = errors.New("the login to the cluster lapsed and Cogpit does not renew it while the cluster is not shown; " +
	"show the cluster in Cogpit's window to renew it")

// errPluginHeld is what the wrapped interactiveFunc returns while the plugin is held; client-go
// wraps it into its own message ("getting credentials: exec plugin cannot support interactive
// mode: …") without keeping it as a cause, which is why the refusal is counted instead.
var errPluginHeld = errors.New("cogpit holds the credential plugin while the cluster is not shown")

// errLoginNotAsked is what the wrapped interactiveFunc returns when no open connection asks for a
// run: the ones that use the plugin were cancelled or closed (admit). client-go wraps it as it does
// errPluginHeld.
var errLoginNotAsked = errors.New("no connect or login under way asks for the credential plugin: " +
	"the one that asked for it was cancelled or closed, so cogpit does not start it")

// pluginHold is one Authenticator's hold: the connections that use it and what each asks; and its
// plugin's runs (loginrun.go, D-73).
type pluginHold struct {
	mu        sync.Mutex
	conns     map[*loginGuard]connLogin
	installed bool // the interactiveFunc is wrapped
	notAsked  int  // runs refused because no connection asked for them (admit)

	run     *pluginRun       // the run under way; nil while none is
	last    *pluginRun       // the last run that ended
	stopped *LoginEndedError // Cogpit ended a run: none starts until the user connects or logs in again
	bound   time.Duration    // a run's bound; zero: DefaultLoginTimeout
	program string           // the plugin's program, its file name
	command []string         // the command line the plugin runs, to find its process
}

// holds are the pluginHolds by Authenticator, which client-go keeps for the life of the process;
// patchMu (pluginstderr.go) guards the map and serialises the wrapping.
var (
	holds         = map[*exec.Authenticator]*pluginHold{}
	holdWarnOnce  sync.Once
	interactiveFn = reflect.TypeFor[func() (bool, error)]()
)

// connLogin is what one connection asks of its Authenticator's plugin.
type connLogin struct {
	held      bool // the engine holds the login (HoldLogin): no run for this connection
	withdrawn bool // the user cancelled (CancelLogin): no run for it until ArmLogin
}

// asks reports whether the connection may have the plugin run.
func (c connLogin) asks() bool { return !c.held && !c.withdrawn }

// loginHold is a connection's part of its Authenticator's hold.
type loginHold struct {
	a *exec.Authenticator
	p *pluginHold
	g *loginGuard
}

// registerLoginHold registers the connection's login guard with the hold of the Authenticator its
// configuration uses, wrapping the Authenticator if that has not happened yet; nil without a
// credential plugin. It must run on the configuration the connection's HTTP client is built from.
// bound is its plugin's runs' bound (Options.LoginTimeout); the registration is the user's connect,
// so a plugin whose run Cogpit ended may run again (loginrun.go).
func registerLoginHold(cfg *rest.Config, g *loginGuard, bound time.Duration) *loginHold {
	a := authenticatorFor(cfg)
	if a == nil || g == nil {
		return nil
	}
	patchMu.Lock()
	p := holds[a]
	if p == nil {
		p = &pluginHold{conns: map[*loginGuard]connLogin{}}
		holds[a] = p
	}
	patchMu.Unlock()
	p.mu.Lock()
	p.conns[g] = connLogin{}
	p.mu.Unlock()
	p.configureRuns(cfg, g, bound)
	h := &loginHold{a: a, p: p, g: g}
	h.install()
	return h
}

// install wraps the Authenticator's interactiveFunc once; it reports whether it is wrapped. A plugin
// run that holds the Authenticator's lock right now leaves it for the next try (HoldLogin).
func (h *loginHold) install() bool {
	patchMu.Lock()
	defer patchMu.Unlock()
	h.p.mu.Lock()
	done := h.p.installed
	h.p.mu.Unlock()
	if done {
		return true
	}
	if !wrapInteractive(h.a, h.p) {
		return false
	}
	h.p.mu.Lock()
	h.p.installed = true
	h.p.mu.Unlock()
	return true
}

// set records whether the connection holds the login.
func (h *loginHold) set(on bool) {
	h.p.mu.Lock()
	if st, ok := h.p.conns[h.g]; ok {
		st.held = on
		h.p.conns[h.g] = st
	}
	h.p.mu.Unlock()
}

// drop forgets the connection (Close): a closed connection neither holds nor frees the login.
// Nil-safe.
func (h *loginHold) drop() {
	if h == nil {
		return
	}
	h.p.mu.Lock()
	delete(h.p.conns, h.g)
	h.p.mu.Unlock()
}

// admit decides whether client-go may start the plugin now; it runs under the Authenticator's lock
// (inside refreshCredsLocked), before every run. A run starts only while some open connection asks
// for one (connLogin.asks). Otherwise, while one still holds the login it fails with errPluginHeld
// and each connection that holds counts the refusal (its request ends as ErrLoginLapsed); with none
// (every connection that uses the plugin was cancelled or closed) with errLoginNotAsked. After
// Cogpit ended a run, none starts until the user connects or logs in again (loginrun.go): the
// LoginEndedError, counted for every connection.
func (p *pluginHold) admit() error {
	p.mu.Lock()
	asks, held := false, false
	for _, st := range p.conns {
		asks = asks || st.asks()
		held = held || (st.held && !st.withdrawn)
	}
	switch {
	case !asks && held:
		for g, st := range p.conns {
			if st.held && !st.withdrawn {
				g.lapses.Add(1)
			}
		}
		p.mu.Unlock()
		return errPluginHeld
	case !asks:
		p.notAsked++
		program := p.program
		p.mu.Unlock()
		slog.Info("cluster: did not start a credential plugin: no connect or login under way asks for it", "program", program)
		return errLoginNotAsked
	case p.stopped != nil:
		p.tellLocked(p.stopped)
		e := p.stopped
		p.mu.Unlock()
		return e
	}
	p.mu.Unlock()
	return nil
}

// wrapInteractive sets a's unexported interactiveFunc to one that refuses a run p does not admit
// (the plugin held, no connection asking, or after Cogpit ended a run of it, loginrun.go), and
// otherwise asks the original and notes the run client-go starts next; under a's own lock
// (client-go reads the field under it, in refreshCredsLocked). It reports false, setting nothing,
// when the fields are not what client-go v0.37.1 has, or a plugin run holds the lock right now.
func wrapInteractive(a *exec.Authenticator, p *pluginHold) bool {
	v := reflect.ValueOf(a).Elem()
	field, lock := v.FieldByName("interactiveFunc"), v.FieldByName("mu")
	if !field.IsValid() || field.Type() != interactiveFn || !lock.IsValid() || lock.Type() != reflect.TypeFor[sync.Mutex]() {
		holdWarnOnce.Do(func() {
			slog.Warn("cluster: a credential plugin cannot be held with this client-go; a cluster that is not shown renews its login by the credential's time")
		})
		return false
	}
	// #nosec G103 -- the address of client-go's own lock in the Authenticator, checked to be a sync.Mutex above.
	mu := (*sync.Mutex)(unsafe.Pointer(lock.UnsafeAddr()))
	if !mu.TryLock() {
		return false
	}
	defer mu.Unlock()
	// #nosec G103 -- the interactiveFunc field checked to be a func() (bool, error) above.
	fn := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	orig, ok := fn.Interface().(func() (bool, error))
	if !ok || orig == nil {
		return false
	}
	fn.Set(reflect.ValueOf(func() (bool, error) {
		if err := p.admit(); err != nil {
			return false, err
		}
		interactive, err := orig()
		if err == nil {
			p.begin(mu) // client-go builds and runs the command next, under mu
		}
		return interactive, err
	}))
	return true
}

// HoldLogin holds the connection's credential plugin, or lets it run again (D-69 item 4): while
// held, a login that expires by its time or that the API server rejects is not renewed, and the
// requests that would have needed the plugin end as ErrLoginLapsed. A connection without a
// credential plugin has nothing to hold: its rejected login stays ErrLoginRejected. Safe for
// concurrent use.
func (c *Conn) HoldLogin(on bool) {
	if c == nil || c.hold == nil {
		return
	}
	c.hold.g.held.Store(on)
	c.hold.set(on)
	if on {
		c.hold.install() // wrapped at connect, unless a plugin run held the Authenticator's lock then
	}
}

// LoginHoldable reports whether the connection's credential plugin can be held: it has one, and
// client-go's Authenticator could be wrapped.
func (c *Conn) LoginHoldable() bool {
	if c == nil || c.hold == nil {
		return false
	}
	return c.hold.install()
}

// lapseMark is the count of plugin runs the hold refused for this connection: a request that
// fails while it grows needed the plugin.
func (c *Conn) lapseMark() uint64 {
	if c == nil || c.hold == nil {
		return 0
	}
	return c.hold.g.lapses.Load()
}

// lapsedSince reports whether the hold refused a plugin run for this connection since mark.
func (c *Conn) lapsedSince(mark uint64) bool {
	return c != nil && c.hold != nil && c.hold.g.lapses.Load() != mark
}

// heldGuard is the part of the login guard the hold uses.
type heldGuard struct {
	held   atomic.Bool   // the engine holds the login: a 401 is not handed to the plugin
	lapses atomic.Uint64 // plugin runs the hold refused
}
