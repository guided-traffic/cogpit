package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The kinds a forward goes to.
const (
	kindPods     = "pods"
	kindServices = "services"
)

// forwardRun is one running forward: its listener, and the loop that keeps a connection to a pod
// for it.
type forwardRun struct {
	s       *Session
	b       *Backend
	fw      PortForwarder
	prof    *model.KindProfile
	ref     model.ObjectRef
	spec    ForwardSpec
	uid     types.UID
	created time.Time
	ln      net.Listener

	cancel context.CancelFunc
	wake   chan struct{} // the cache changed something the forward depends on
	first  chan error    // the outcome of the first connection, once
	done   chan struct{} // closed when the loop has ended and the listener is closed

	mu        sync.Mutex
	state     Forward
	firstSent bool
	lastErr   string // the last error a local connection ended with
}

func (r *forwardRun) snapshot() Forward {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// wait waits, bounded, for the loop to end.
func (r *forwardRun) wait() {
	select {
	case <-r.done:
	case <-time.After(forwardStopWait):
		r.s.log.Warn("engine: a port forward did not stop in time", "forward", r.ref.Key())
	}
}

// set records a new state and writes the forward's object.
func (r *forwardRun) set(phase, message, pod string, podPort uint16) {
	r.mu.Lock()
	changed := r.state.Phase != phase || r.state.Message != message || r.state.Pod != pod || r.state.PodPort != podPort
	r.state.Phase, r.state.Message, r.state.Pod, r.state.PodPort = phase, message, pod, podPort
	r.mu.Unlock()
	if changed {
		r.write()
	}
}

func (r *forwardRun) write() {
	if err := r.b.PutLocal(r.prof, forwardObject(r.snapshot(), r.uid, r.created)); err != nil {
		r.s.log.Warn("engine: writing a port forward failed", "forward", r.ref.Key(), "err", err)
	}
}

// reportFirst hands StartForward the first connection's outcome, once.
func (r *forwardRun) reportFirst(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.firstSent {
		r.firstSent = true
		r.first <- err
	}
}

// forwardTarget is the pod a forward connects to now, and the port in it.
type forwardTarget struct {
	pod  string
	port uint16
}

// run is the forward's loop: resolve the pod from the cache, dial it, carry connections until the
// connection is lost or the pod goes, and again, until ctx ends. A first connection that fails
// ends the forward (Failed): the caller sees the error. Later losses are dialled again, except
// an error that waiting does not cure (cluster.IsPermanentForwardError): that fails the forward
// at once and frees its port (D-20).
func (r *forwardRun) run(ctx context.Context) {
	defer close(r.done)
	defer func() { _ = r.ln.Close() }()
	conns := make(chan net.Conn)
	go r.accept(ctx, conns)

	watch, err := r.watchTargets(ctx)
	if err != nil {
		r.fail(ctx, err)
		return
	}
	defer watch.release()

	first := r.s.opts.ForwardBackoff
	backoff, connected := first, false
	var cur forwardTarget
	for ctx.Err() == nil {
		t, why := r.resolve(watch, cur.pod)
		if why != "" {
			r.set(ForwardWaiting, why, "", 0)
			r.reportFirst(nil)
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
			}
			continue
		}
		// No dial while the connection is interrupted; at once when it is back (D-58, S7).
		if waited, ok := r.awaitConnection(ctx, t); !ok {
			return
		} else if waited {
			backoff = first
		}
		phase := ForwardStarting
		if connected {
			phase = ForwardReconnecting
		}
		r.set(phase, r.snapshot().Message, t.pod, t.port)
		dctx, cancel := context.WithTimeout(ctx, forwardDialTimeout)
		tun, err := r.fw.DialPortForward(dctx, r.spec.Target.Namespace, t.pod)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !r.firstSentYet() {
				r.fail(ctx, err)
				return
			}
			if cluster.IsPermanentForwardError(err) {
				// Waiting does not cure it (D-20): Failed at once, the local port freed.
				r.fail(ctx, fmt.Errorf("not retried, waiting would not help: %w", err))
				return
			}
			r.set(ForwardReconnecting, err.Error(), t.pod, t.port)
			if !r.sleep(ctx, backoff) {
				return
			}
			backoff = min(2*backoff, forwardRetryCap)
			continue
		}
		connected, cur = true, t
		r.set(ForwardActive, "", t.pod, t.port)
		r.reportFirst(nil)
		since := time.Now()
		why, lost := r.serve(ctx, tun, t, watch, conns)
		_ = tun.Close()
		if ctx.Err() != nil {
			return
		}
		r.set(ForwardReconnecting, why, t.pod, t.port)
		if !lost {
			continue // the pod went or another is preferred: connect to the next one at once
		}
		if time.Since(since) >= forwardStable {
			backoff = first
		}
		if !r.sleep(ctx, backoff) {
			return
		}
		backoff = min(2*backoff, forwardRetryCap)
	}
}

func (r *forwardRun) firstSentYet() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.firstSent
}

// fail ends a forward that could not start: Failed with the reason, the listener closed, and the
// run out of the set, so that the forward's object says what happened.
func (r *forwardRun) fail(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	r.state.Phase, r.state.Message = ForwardFailed, err.Error()
	r.state.LocalPort, r.state.Address, r.state.URL, r.state.Pod, r.state.PodPort = 0, "", "", "", 0
	r.mu.Unlock()
	_ = r.ln.Close()
	r.write()
	r.b.forwards.drop(r)
	r.reportFirst(err)
}

// drop removes a run from the set if it is still the forward's run.
func (fs *forwardSet) drop(r *forwardRun) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.runs[r.ref.Key()] == r {
		delete(fs.runs, r.ref.Key())
	}
}

// sleep waits d, or until ctx ends; it reports whether ctx is still live.
func (r *forwardRun) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// awaitConnection waits while the cluster's connection is interrupted or its login expired,
// saying so in the forward's state. It reports whether it waited, and false for ok once ctx ended.
func (r *forwardRun) awaitConnection(ctx context.Context, t forwardTarget) (waited, ok bool) {
	back, _ := r.b.watch.usable()
	select {
	case <-back:
		return false, true
	default:
	}
	r.set(ForwardReconnecting, forwardInterrupted, t.pod, t.port)
	select {
	case <-back:
		return true, true
	case <-ctx.Done():
		return true, false
	}
}

// forwardInterrupted is a forward's message while its cluster's connection is interrupted.
const forwardInterrupted = "the connection to the cluster is interrupted; the forward connects again once it is back"

// serve carries local connections over tun until ctx ends, the connection is lost (lost true:
// dial again after the backoff) or the cache says the pod is no longer the one to use (lost
// false: dial the next one at once). why says which. A connection to the cluster that is
// interrupted ends the tunnel too (lost false): the loop waits for it, then dials at once.
func (r *forwardRun) serve(ctx context.Context, tun cluster.PortTunnel, t forwardTarget, watch *forwardWatch, conns <-chan net.Conn) (why string, lost bool) {
	_, broken := r.b.watch.usable()
	for {
		select {
		case <-ctx.Done():
			return "", false
		case <-broken:
			return forwardInterrupted, false
		case <-tun.Done():
			why = "lost the connection to pod " + t.pod
			r.mu.Lock()
			if r.lastErr != "" {
				why += ": " + r.lastErr
				r.lastErr = ""
			}
			r.mu.Unlock()
			return why, true
		case c := <-conns:
			go r.carry(tun, c, t.port)
		case <-r.wake:
			next, whyNot := r.resolve(watch, t.pod)
			switch {
			case whyNot != "":
				return whyNot, false
			case next != t:
				return "moving to pod " + next.pod, false
			}
		}
	}
}

// carry forwards one local connection; an error the pod reports is kept for the state's message.
func (r *forwardRun) carry(tun cluster.PortTunnel, c net.Conn, port uint16) {
	defer func() {
		if p := recover(); p != nil {
			r.s.log.Error("engine: a forwarded connection panicked", "forward", r.ref.Key(), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			_ = c.Close()
		}
	}()
	if err := tun.Forward(c, port); err != nil {
		r.mu.Lock()
		r.lastErr = err.Error()
		r.mu.Unlock()
	}
}

// accept hands the listener's connections to the loop. While the forward is not connected to a
// pod, a connection waits here (and others in the listen backlog) until it is: a request made
// while a pod restarts is answered by the next one.
func (r *forwardRun) accept(ctx context.Context, conns chan<- net.Conn) {
	defer func() {
		if p := recover(); p != nil {
			slog.Default().Error("engine: a port forward's listener panicked", "forward", r.ref.Key(), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
		}
	}()
	for {
		c, err := r.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if !r.sleep(ctx, 50*time.Millisecond) { // EMFILE and the like: try again shortly
				return
			}
			continue
		}
		select {
		case conns <- c:
		case <-ctx.Done():
			_ = c.Close()
			return
		}
	}
}

// forwardWatch holds the cache handles a forward reads its pods (and service) from; their events
// wake the forward.
type forwardWatch struct {
	pods, services             *cache.Handle
	podsProf, servicesProf     *model.KindProfile
	cancelPods, cancelServices func()
}

func (w *forwardWatch) release() {
	for _, c := range []func(){w.cancelPods, w.cancelServices} {
		if c != nil {
			c()
		}
	}
	for _, h := range []*cache.Handle{w.pods, w.services} {
		if h != nil {
			h.Release()
		}
	}
}

// watchTargets acquires the pods (and for a service forward the services) of the backend's cache,
// subscribed to the events that concern the forward, and waits for their initial lists.
func (r *forwardRun) watchTargets(ctx context.Context) (*forwardWatch, error) {
	w := &forwardWatch{}
	ns, name := r.spec.Target.Namespace, r.spec.Target.Name
	var ok bool
	if w.podsProf, ok = r.b.Profile(kindPods); !ok {
		return nil, errors.New("this cluster serves no pods")
	}
	if r.spec.Target.Kind == kindServices {
		if w.servicesProf, ok = r.b.Profile(kindServices); !ok {
			return nil, errors.New("this cluster serves no services")
		}
	}
	// The target's namespace, once the access reviews said how (accessiblens_plan.go acquireIn): a
	// forward starts at connect too (a permanent one), when a handle taken at once would read in
	// all namespaces, which a user who may not list pods there is refused, and never sync. A read
	// in all namespaces that the server refuses all the same (a RoleBinding in the context's
	// namespace alone) is taken again in the target's namespace (readFollowing).
	for _, read := range []struct {
		p  *model.KindProfile
		to **cache.Handle
	}{{w.podsProf, &w.pods}, {w.servicesProf, &w.services}} {
		if read.p == nil {
			continue
		}
		h, err := r.b.readFollowing(ctx, read.p, r.b.Scope(read.p).String(), r.b.inNamespace(read.p, ns))
		if err == nil {
			if err = refusedIn(h, h.Scope().String()); err != nil {
				h.Release()
			}
		}
		if err != nil {
			w.release()
			return nil, err
		}
		*read.to = h
	}
	wake := func() { signal(r.wake) }
	w.cancelPods = w.pods.Subscribe(func(ev cache.Event) {
		if ev.Object.GetNamespace() == ns && (r.spec.Target.Kind != kindPods || ev.Object.GetName() == name) {
			wake()
		}
	})
	if w.services != nil {
		w.cancelServices = w.services.Subscribe(func(ev cache.Event) {
			if ev.Object.GetNamespace() == ns && ev.Object.GetName() == name {
				wake()
			}
		})
	}
	return w, nil
}

// resolve finds the pod and port to connect to from the cache, preferring the pod connected now;
// why says why there is none.
func (r *forwardRun) resolve(w *forwardWatch, prefer string) (forwardTarget, string) {
	ns, name := r.spec.Target.Namespace, r.spec.Target.Name
	if r.spec.Target.Kind == kindPods {
		pod, ok := KindGet(w.pods, w.podsProf, ns, name)
		if !ok {
			return forwardTarget{}, "pod " + name + " does not exist"
		}
		if why := podNotRunning(pod); why != "" {
			return forwardTarget{}, why
		}
		return forwardTarget{pod: name, port: r.spec.Port}, ""
	}
	svc, ok := KindGet(w.services, w.servicesProf, ns, name)
	if !ok {
		return forwardTarget{}, "service " + name + " does not exist"
	}
	return servicePod(svc, r.spec.Port, KindListNamespace(w.pods, w.podsProf, ns), prefer)
}

// podNotRunning says why a pod cannot be forwarded to now, or "".
func podNotRunning(pod *unstructured.Unstructured) string {
	switch phase := model.NestedString(pod.Object, "status", "phase"); {
	case pod.GetDeletionTimestamp() != nil:
		return "pod " + pod.GetName() + " is terminating"
	case phase != "Running":
		if phase == "" {
			phase = "not running"
		}
		return "pod " + pod.GetName() + " is " + strings.ToLower(phase)
	}
	return ""
}

// podReady reports whether a pod runs, is not terminating and is Ready: what a service sends
// traffic to.
func podReady(pod *unstructured.Unstructured) bool {
	if podNotRunning(pod) != "" {
		return false
	}
	for _, c := range model.NestedSlice(pod.Object, "status", "conditions") {
		if model.NestedString(c, "type") == "Ready" {
			return model.NestedString(c, "status") == "True"
		}
	}
	return false
}

// servicePod picks a ready pod of a service's selector and the port in it that the service's port
// targets, as kubectl port-forward svc/… does: the target port by number, a named target port
// looked up in the pod's containers, or the service's own port when none is set. It prefers the
// pod connected now, else the oldest, then by name.
func servicePod(svc *unstructured.Unstructured, port uint16, pods []*unstructured.Unstructured, prefer string) (forwardTarget, string) {
	name := svc.GetName()
	sp, ok := servicePortSpec(svc, port)
	if !ok {
		return forwardTarget{}, fmt.Sprintf("service %s has no TCP port %d", name, port)
	}
	selector := model.NestedStringMap(svc.Object, "spec", "selector")
	if len(selector) == 0 {
		return forwardTarget{}, "service " + name + " has no selector: its endpoints are not pods Cogpit can find"
	}
	var ready []*unstructured.Unstructured
	for _, p := range pods {
		if podReady(p) && matchesSelector(p.GetLabels(), selector) {
			ready = append(ready, p)
		}
	}
	slices.SortFunc(ready, func(a, b *unstructured.Unstructured) int {
		switch {
		case a.GetName() == prefer:
			return -1
		case b.GetName() == prefer:
			return 1
		}
		if c := a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time); c != 0 {
			return c
		}
		return strings.Compare(a.GetName(), b.GetName())
	})
	for _, p := range ready {
		if target, ok := targetPort(sp, p); ok {
			return forwardTarget{pod: p.GetName(), port: target}, ""
		}
	}
	if len(ready) > 0 {
		return forwardTarget{}, fmt.Sprintf("no ready pod of service %s has the port %s", name, model.NestedIntOrString(sp, "targetPort"))
	}
	return forwardTarget{}, "no ready pod for service " + name
}

// servicePortSpec returns the service's TCP port entry for port.
func servicePortSpec(svc *unstructured.Unstructured, port uint16) (map[string]any, bool) {
	for _, p := range model.NestedSlice(svc.Object, "spec", "ports") {
		n, _ := model.NestedInt64(p, "port")
		proto := model.NestedString(p, "protocol")
		m, ok := p.(map[string]any)
		if ok && n == int64(port) && (proto == "" || proto == "TCP") {
			return m, true
		}
	}
	return nil, false
}

// targetPort is the port in pod a service port goes to.
func targetPort(sp map[string]any, pod *unstructured.Unstructured) (uint16, bool) {
	target := model.NestedIntOrString(sp, "targetPort")
	if target == "" || target == "0" {
		n, _ := model.NestedInt64(sp, "port")
		return validPort(n)
	}
	if n, err := strconv.ParseInt(target, 10, 64); err == nil {
		return validPort(n)
	}
	for _, c := range model.NestedSlice(pod.Object, "spec", "containers") {
		for _, p := range model.NestedSlice(c, "ports") {
			if model.NestedString(p, "name") == target {
				n, _ := model.NestedInt64(p, "containerPort")
				return validPort(n)
			}
		}
	}
	return 0, false
}

func validPort(n int64) (uint16, bool) {
	if n < 1 || n > 65535 {
		return 0, false
	}
	return uint16(n), true
}

// matchesSelector reports whether labels match a service's equality selector.
func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}
