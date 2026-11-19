package demo

import (
	"time"

	"k8s.io/apimachinery/pkg/watch"
)

// maxTerminating caps the pods the churn keeps in Terminating at once.
const maxTerminating = 200

// operation is one kind of change; it returns the watch events it produced, 0 when it does not
// apply right now.
type operation struct {
	weight int
	run    func(c *Cluster, now time.Time) int
}

// operations is the churn's mix, by weight. Pods dominate: readiness flips, restarts, pods
// replaced (Terminating, then gone, with a replacement that is created, then starts); then
// deployment status catching up with its pods, new and repeated events, node heartbeats.
var operations = []operation{
	{30, (*Cluster).opReadiness},
	{15, (*Cluster).opRestart},
	{4, (*Cluster).opTerminate},
	{6, (*Cluster).opReap},
	{6, (*Cluster).opStart},
	{15, (*Cluster).opDeploymentStatus},
	{12, (*Cluster).opNewEvent},
	{8, (*Cluster).opRepeatEvent},
	{4, (*Cluster).opNodeHeartbeat},
}

var totalWeight = func() int {
	sum := 0
	for _, op := range operations {
		sum += op.weight
	}
	return sum
}()

// step runs operations until they have produced at least n watch events and returns how many
// they produced. Each operation reads the clock, so its timestamps do not depend on how the
// generator batches operations into ticks. Called with c.mu held.
func (c *Cluster) step(n int) int {
	produced := 0
	for produced < n {
		produced += c.operate(c.clock())
	}
	return produced
}

// operate runs one operation, falling back to a readiness flip or a new event when the chosen
// one does not apply.
func (c *Cluster) operate(now time.Time) int {
	r := c.rng.IntN(totalWeight)
	for _, op := range operations {
		if r < op.weight {
			if n := op.run(c, now); n > 0 {
				return n
			}
			break
		}
		r -= op.weight
	}
	if n := c.opReadiness(now); n > 0 {
		return n
	}
	return c.opNewEvent(now)
}

// publishPod publishes a pod's new state and marks its deployment's status stale.
func (c *Cluster) publishPod(p *pod, typ watch.EventType) {
	c.publish(kindPods, typ, c.renderPod(p))
	if d := p.deploy; d != nil && !d.dirty {
		d.dirty = true
		c.w.dirty = append(c.w.dirty, d)
	}
}

// randomPod returns a random pod that satisfies ok, or nil after a few misses.
func (c *Cluster) randomPod(ok func(*pod) bool) *pod {
	if len(c.w.pods) == 0 {
		return nil
	}
	for range 64 {
		if p := c.w.pods[c.rng.IntN(len(c.w.pods))]; ok(p) {
			return p
		}
	}
	return nil
}

// replaceable reports whether a pod is a running, ready pod of a healthy deployment's current
// replica set: the pods the churn makes unready or replaces.
func replaceable(p *pod) bool {
	return p.phase == phaseRunning && p.ready && p.deleting.IsZero() &&
		p.deploy != nil && p.deploy.profile == profileHealthy && p.rs == p.deploy.current
}

// opReadiness makes a pod fail or pass its readiness probe, keeping the number of failing pods
// near its initial value.
func (c *Cluster) opReadiness(now time.Time) int {
	w := &c.w
	heal := len(w.notReady) > w.notReadyTarget || (len(w.notReady) == w.notReadyTarget && c.rng.IntN(2) == 0)
	if heal && len(w.notReady) > 0 {
		i := c.rng.IntN(len(w.notReady))
		p := w.notReady[i]
		w.notReady[i] = w.notReady[len(w.notReady)-1]
		w.notReady = w.notReady[:len(w.notReady)-1]
		p.ready, p.readySince = true, now
		c.publishPod(p, watch.Modified)
		return 1
	}
	p := c.randomPod(replaceable)
	if p == nil {
		return 0
	}
	p.ready, p.readySince = false, now
	w.notReady = append(w.notReady, p)
	c.publishPod(p, watch.Modified)
	return 1
}

// opRestart moves a crash-looping pod between back-off and a short run (a restart), or an image
// pull between ErrImagePull and ImagePullBackOff.
func (c *Cluster) opRestart(now time.Time) int {
	if len(c.w.unstable) == 0 {
		return 0
	}
	p := pick(c, c.w.unstable)
	switch p.phase {
	case phaseCrashLoop:
		if p.inBackOff {
			p.inBackOff, p.lastStart = false, now
			p.restarts++
		} else {
			p.inBackOff, p.lastExit = true, now
		}
	case phaseImagePull:
		p.pullBackOff = !p.pullBackOff
	default:
		return 0
	}
	c.publishPod(p, watch.Modified)
	return 1
}

// opTerminate deletes a pod gracefully, as a rollout or an eviction would: it becomes
// Terminating, and its replica set creates a replacement at once.
func (c *Cluster) opTerminate(now time.Time) int {
	if len(c.w.terminating) >= maxTerminating {
		return 0
	}
	p := c.randomPod(replaceable)
	if p == nil {
		return 0
	}
	p.deleting, p.since = now, c.rv
	c.w.terminating = append(c.w.terminating, p)
	c.publishPod(p, watch.Modified)

	d := p.deploy
	r := c.newDeployPod(d, d.current, phaseCreating, now, pick(c, c.w.workers))
	r.since = c.rv
	c.w.creating = append(c.w.creating, r)
	c.publishPod(r, watch.Added)
	return 2
}

// opReap removes the oldest Terminating pod once its grace period (settle changes) has passed.
func (c *Cluster) opReap(time.Time) int {
	if len(c.w.terminating) == 0 || c.w.terminating[0].since+c.w.settle > c.rv {
		return 0
	}
	p := c.w.terminating[0]
	c.w.terminating = c.w.terminating[1:]
	c.removePod(p)
	c.publishPod(p, watch.Deleted)
	return 1
}

// opStart starts the oldest ContainerCreating pod once it has been creating for settle changes.
func (c *Cluster) opStart(now time.Time) int {
	if len(c.w.creating) == 0 || c.w.creating[0].since+c.w.settle > c.rv {
		return 0
	}
	p := c.w.creating[0]
	c.w.creating = c.w.creating[1:]
	p.phase, p.ready = phaseRunning, true
	p.started, p.readySince = now, now
	p.ip = p.node.podIP()
	c.publishPod(p, watch.Modified)
	return 1
}

// opDeploymentStatus publishes the status of the deployment whose pods changed longest ago and of
// those of its replica sets whose counts changed, as the controllers do.
func (c *Cluster) opDeploymentStatus(now time.Time) int {
	if len(c.w.dirty) == 0 {
		return 0
	}
	d := c.w.dirty[0]
	c.w.dirty = c.w.dirty[1:]
	d.dirty = false
	d.refresh(now)
	n := 1
	for _, rs := range d.replicaSetsChanged() { // the replica sets report first
		c.publish(kindReplicaSets, watch.Modified, c.renderReplicaSet(d, rs))
		n++
	}
	c.publish(kindDeployments, watch.Modified, c.renderDeployment(d))
	return n
}

// opNewEvent records a new event and removes the oldest beyond Options.Events, as the API
// server's event TTL would.
func (c *Cluster) opNewEvent(now time.Time) int {
	e := c.newEvent(now)
	c.w.events = append(c.w.events, e)
	c.publish(kindEvents, watch.Added, c.renderEvent(e))
	n := 1
	for len(c.w.events)-c.w.evHead > c.opts.Events {
		old := c.w.events[c.w.evHead]
		c.w.events[c.w.evHead] = nil
		c.w.evHead++
		c.publish(kindEvents, watch.Deleted, c.renderEvent(old))
		n++
	}
	if c.w.evHead > 1024 && 2*c.w.evHead > len(c.w.events) {
		c.w.events = append([]*event(nil), c.w.events[c.w.evHead:]...)
		c.w.evHead = 0
	}
	return n
}

// opRepeatEvent counts another occurrence of an existing event.
func (c *Cluster) opRepeatEvent(now time.Time) int {
	live := len(c.w.events) - c.w.evHead
	if live == 0 {
		return 0
	}
	e := c.w.events[c.w.evHead+c.rng.IntN(live)]
	e.count++
	e.last = now
	c.publish(kindEvents, watch.Modified, c.renderEvent(e))
	return 1
}

// opNodeHeartbeat refreshes a node's condition heartbeats. The NotReady node's kubelet is silent.
func (c *Cluster) opNodeHeartbeat(now time.Time) int {
	n := pick(c, c.w.nodes)
	if n.notReady {
		return 0
	}
	n.heartbeat = now
	c.publish(kindNodes, watch.Modified, c.renderNode(n))
	return 1
}
