package cache

import "time"

// A refused scope is not kept warm: its informer stops with its last handle, so that client-go's
// backoff does not list it again (release). What the API server answered is remembered instead,
// for as long as a scope that answered stays warm (Options.WarmPeriod): a reader that acquires the
// scope again meanwhile (a page that starts again because the plan of another of its kinds
// changed, a drawer opened again) gets a handle that reports the refusal at once, without a list.
// ForgetRefusals (cluster.refreshAccess) and Close forget them, so the next reader lists again.

// rememberedRefusal is what a refused scope's last handle left: its error and whether it had
// synced, and the timer that forgets it.
type rememberedRefusal struct {
	err    error
	synced bool
	timer  *time.Timer
}

// rememberLocked keeps e's refusal as it retires. Called with c.mu held.
func (c *Cache) rememberLocked(e *entry) {
	var (
		err    error
		synced bool
	)
	switch {
	case e.view != nil:
		err, synced = e.view.err(), e.view.synced()
	case e.run != nil:
		err, synced = e.run.err(), e.run.synced()
	}
	if err == nil {
		return
	}
	if c.remembered == nil {
		c.remembered = map[Scope]*rememberedRefusal{}
	}
	if old := c.remembered[e.scope]; old != nil {
		old.timer.Stop()
	}
	r := &rememberedRefusal{err: err, synced: synced}
	scope := e.scope
	r.timer = time.AfterFunc(c.opts.WarmPeriod, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.remembered[scope] == r {
			delete(c.remembered, scope)
		}
	})
	c.remembered[scope] = r
}

// rememberedLocked returns a handle on a remembered refusal of scope, nil without one: released
// already (Release does nothing), its refusal kept as ReleaseWithErr keeps one, and given back
// once by its first ReleaseWithErr, so that its reader counts the refusal as given back. Called
// with c.mu held.
func (c *Cache) rememberedLocked(scope Scope) *Handle {
	r := c.remembered[scope]
	if r == nil {
		return nil
	}
	h := &Handle{scope: scope, c: c}
	h.kept.Store(&keptErr{err: r.err, synced: r.synced})
	h.released.Store(true)
	h.remembered.Store(true)
	return h
}

// ForgetRefusals forgets every remembered refusal: the next reader of a scope the API server
// refused lists it again. cluster.refreshAccess calls it.
func (c *Cache) ForgetRefusals() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetRefusalsLocked()
}

func (c *Cache) forgetRefusalsLocked() {
	for scope, r := range c.remembered {
		r.timer.Stop()
		delete(c.remembered, scope)
	}
}
