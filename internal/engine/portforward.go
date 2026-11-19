package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Port forwards (gap G10; architecture §6). A forward listens on 127.0.0.1 only
// (cluster.ListenLoopback) for as long as it runs, and carries every local connection over one
// port-forward connection at a time to the pod behind it. Each forward is an object of the local
// kind portforwards (model/kinds/portforwards.go) in the backend's local store, so views, the
// drawer, row menus and agents see it like a cluster object. A forward belongs to its backend:
// switching clusters or quitting ends it (Backend.close), and nothing is left in the cluster.
//
// What a forward connects to comes from the cache, whose events wake it (portforward_run.go): a
// pod forward connects while its pod runs and waits while it does not (a stateful set's pod comes
// back under its name); a service forward picks a ready pod of the service's selector and moves
// to another when that one goes. A lost connection is dialled again after a backoff of 1 s,
// doubling to 30 s. Nothing is polled.

// PortForwarder opens port-forward connections to pods (create pods/portforward).
// *cluster.Conn implements it; commands reach it with a type assertion on Backend.Writer(), and
// tests substitute a fake as Backend.Actions.
type PortForwarder interface {
	// DialPortForward opens a port-forward connection to a pod; ctx bounds the dial.
	DialPortForward(ctx context.Context, namespace, pod string) (cluster.PortTunnel, error)
}

var _ PortForwarder = (*cluster.Conn)(nil)

// PortForwardKind is the kind key of the port forwards, a local kind.
const PortForwardKind = "portforwards"

// Forward phases: a port forward's status.phase.
const (
	ForwardStarting     = "Starting"     // listening; the first connection to the pod is being made
	ForwardActive       = "Active"       // listening and connected to a pod
	ForwardWaiting      = "Waiting"      // listening; no pod to connect to yet (the message says why)
	ForwardReconnecting = "Reconnecting" // listening; the connection to the pod is made again
	ForwardStopped      = "Stopped"      // not listening
	ForwardFailed       = "Failed"       // not listening: it could not start (the message says why)
)

// Timings of the forwards.
const (
	forwardRetryFirst = time.Second
	forwardRetryCap   = 30 * time.Second
	// forwardStable is how long a connection must have lasted for its loss to be retried at once.
	forwardStable = 30 * time.Second
	// forwardStartWait bounds how long StartForward waits for the first connection to the pod.
	forwardStartWait = 15 * time.Second
	// forwardDialTimeout bounds one dial.
	forwardDialTimeout = 30 * time.Second
	// forwardStopWait bounds how long stopping waits for a forward's loop.
	forwardStopWait = 5 * time.Second
)

// ForwardSpec is what a port forward forwards, and how.
type ForwardSpec struct {
	// Target is the pod or the service (kind "pods" or "services").
	Target    model.ObjectRef `json:"target"`
	Container string          `json:"container,omitempty"` // the container the port belongs to, if known
	// Port is the pod's port, or the service's port (forwarded to its target port in a pod).
	Port uint16 `json:"port"`
	// LocalPort is the port to listen on, on 127.0.0.1; 0 picks a free one.
	LocalPort     uint16 `json:"localPort,omitempty"`
	HTTPS         bool   `json:"https,omitempty"`         // the URL's scheme is https
	OpenInBrowser bool   `json:"openInBrowser,omitempty"` // the GUI opens the URL once it starts
}

// Forward is a port forward and its state.
type Forward struct {
	Ref       model.ObjectRef `json:"ref"` // the forward, of kind portforwards
	Spec      ForwardSpec     `json:"spec"`
	Phase     string          `json:"phase"`
	Message   string          `json:"message,omitempty"`
	LocalPort uint16          `json:"localPort,omitempty"` // the port it listens on, while it does
	Address   string          `json:"address,omitempty"`   // "127.0.0.1:<localPort>"
	URL       string          `json:"url,omitempty"`       // "http://127.0.0.1:<localPort>"
	Pod       string          `json:"pod,omitempty"`       // the pod it is connected to
	PodPort   uint16          `json:"podPort,omitempty"`   // the port in that pod
}

// Listening reports whether the forward has a local port.
func (f Forward) Listening() bool { return f.LocalPort != 0 }

// ForwardName is the name of the forward object of a target's port: "pod-web-0-8080",
// "service-web-80". One forward per target and port: starting it again restarts it.
func ForwardName(target model.ObjectRef, port uint16) string {
	kind := "pod"
	if target.Kind == kindServices {
		kind = "service"
	}
	return kind + "-" + target.Name + "-" + strconv.Itoa(int(port))
}

// forwardURL is a forward's URL. It names the address the listener is on, 127.0.0.1, never
// localhost: localhost resolves to ::1 first on macOS, and a client (a browser, curl, Go's HTTP
// client) then reaches whatever another local process listens on at [::1] and the same port,
// never falling back to the forward (review, TestForwardURLReachesTheForward).
func forwardURL(https bool, port uint16) string {
	scheme := schemeHTTP
	if https {
		scheme = schemeHTTPS
	}
	return scheme + "://" + net.JoinHostPort(cluster.LoopbackAddress, strconv.Itoa(int(port)))
}

// The schemes of a forward's URL, its spec.protocol.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// fieldLocalPort is the local port in a forward object's spec (asked for) and status (bound).
const fieldLocalPort = "localPort"

// Errors of the forward commands.
var (
	ErrNoForwarder = errors.New("this connection cannot forward ports")
	ErrNoForwards  = errors.New("port forwards are not available on this cluster")
)

// forwardSet holds a backend's running forwards, one per forward object that listens. It lives in
// the Backend and is usable as its zero value.
type forwardSet struct {
	// opMu serialises start, stop and remove, so that two of them never race for one forward.
	opMu   sync.Mutex
	mu     sync.Mutex
	runs   map[string]*forwardRun // by namespace/name
	closed bool
}

func (fs *forwardSet) get(key string) *forwardRun {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.runs[key]
}

// stopAll ends every forward of a closing backend: listeners close, connections to pods end. It
// writes no objects, since the local store closes too.
func (fs *forwardSet) stopAll() {
	fs.mu.Lock()
	fs.closed = true
	runs := fs.runs
	fs.runs = nil
	fs.mu.Unlock()
	for _, r := range runs {
		r.cancel()
	}
	for _, r := range runs {
		r.wait()
	}
}

// forwardsOf returns a connected context's backend ("" the shown one, D-69: its forwards are its
// own and run on whatever the window shows), its port forwarder and the forwards' profile.
func (s *Session) forwardsOf(contextID string) (*Backend, PortForwarder, *model.KindProfile, error) {
	b, err := s.TargetBackend(contextID)
	if err != nil {
		return nil, nil, nil, err
	}
	prof, ok := b.Profile(PortForwardKind)
	if !ok || !prof.Local {
		return nil, nil, nil, ErrNoForwards
	}
	fw, ok := b.Writer().(PortForwarder)
	if !ok {
		return nil, nil, nil, ErrNoForwarder
	}
	return b, fw, prof, nil
}

// CanForward reports why a connected context's cluster ("" the shown one) cannot forward ports, or
// nil.
func (s *Session) CanForward(contextID string) error {
	_, _, _, err := s.forwardsOf(contextID)
	return err
}

// StartForward starts a port forward, or starts the forward of the same target and port again
// with these settings (Edit). It returns once the forward listens and its first connection to the
// pod is made, or it waits for a pod (phase Waiting), or ctx or 15 s end that wait (phase
// Starting): the forward then goes on in the background. A forward that cannot listen or whose
// first connection fails is not left running; its error is returned.
func (s *Session) StartForward(ctx context.Context, spec ForwardSpec) (Forward, error) {
	b, fw, prof, err := s.forwardsOf(spec.Target.Context)
	if err != nil {
		return Forward{}, err
	}
	target, _, err := s.ResolveRef(spec.Target)
	if err != nil {
		return Forward{}, err
	}
	if target.Kind != kindPods && target.Kind != kindServices {
		return Forward{}, fmt.Errorf("port forwards go to pods and services, not %s", target.Kind)
	}
	if spec.Port == 0 {
		return Forward{}, errors.New("say which port to forward: port is required")
	}
	spec.Target = target
	ref := model.ObjectRef{Context: b.Context, Kind: PortForwardKind, Namespace: target.Namespace, Name: ForwardName(target, spec.Port)}

	fs := &b.forwards
	fs.opMu.Lock()
	r, err := s.startRun(b, fw, prof, ref, spec)
	fs.opMu.Unlock()
	if err != nil {
		return r.snapshot(), err
	}
	err = r.firstOutcome(ctx, forwardStartWait)
	return r.snapshot(), err
}

// firstOutcome waits, bounded by ctx and timeout, for the outcome of the run's first connection:
// its error, or nil once it connected or waits for a pod, or nil when the run was stopped
// meanwhile. A run whose first connection failed reports that before it ends, so the error wins
// when both are ready: select alone picks either, and the command answered a forward that failed
// to start as started.
func (r *forwardRun) firstOutcome(ctx context.Context, timeout time.Duration) error {
	wait := time.NewTimer(timeout)
	defer wait.Stop()
	select {
	case err := <-r.first:
		return err
	case <-r.done: // stopped meanwhile, or failed: then its error is in first
		select {
		case err := <-r.first:
			return err
		default:
			return nil
		}
	case <-ctx.Done():
	case <-wait.C:
	}
	return nil
}

// startRun stops the forward's current run, if any, binds the listener and starts the new run.
// The caller holds opMu.
func (s *Session) startRun(b *Backend, fw PortForwarder, prof *model.KindProfile, ref model.ObjectRef, spec ForwardSpec) (*forwardRun, error) {
	return s.startRunWith(b, fw, prof, ref, spec, nil)
}

// startRunWith is startRun with the forward's UID and creation time given (a forward carried to a
// new connection, reconnect.go); nil reads them as startRun does. The caller holds opMu.
func (s *Session) startRunWith(b *Backend, fw PortForwarder, prof *model.KindProfile, ref model.ObjectRef, spec ForwardSpec,
	given *forwardMetaInfo) (*forwardRun, error) {
	fs := &b.forwards
	key := ref.Key()
	old := fs.take(key)
	if old != nil {
		old.cancel()
		old.wait()
	}
	var meta forwardMetaInfo
	if given != nil {
		meta = *given
	} else {
		meta = s.forwardMeta(b, prof, ref, old)
	}
	r := &forwardRun{
		s: s, b: b, fw: fw, prof: prof, ref: ref, spec: spec, uid: meta.uid, created: meta.created,
		wake: make(chan struct{}, 1), first: make(chan error, 1), done: make(chan struct{}),
	}
	r.state = Forward{Ref: ref, Spec: spec, Phase: ForwardStarting}
	ln, err := cluster.ListenLoopback(spec.LocalPort)
	if err != nil {
		r.state.Phase, r.state.Message = ForwardFailed, err.Error()
		close(r.done)
		r.write() // the forward's object says why it failed; Edit tries again
		return r, err
	}
	r.ln = ln
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	r.state.LocalPort, r.state.Address, r.state.URL = port, ln.Addr().String(), forwardURL(spec.HTTPS, port)

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	fs.mu.Lock()
	if fs.closed {
		fs.mu.Unlock()
		cancel()
		_ = ln.Close()
		close(r.done)
		return r, ErrNotConnected
	}
	if fs.runs == nil {
		fs.runs = map[string]*forwardRun{}
	}
	fs.runs[key] = r
	fs.mu.Unlock()
	r.write()
	s.goSafe("port forward "+ref.Key(), func() { r.run(ctx) })
	return r, nil
}

// forwardMetaInfo is what a forward keeps across restarts: its UID and creation time.
type forwardMetaInfo struct {
	uid     types.UID
	created time.Time
}

// forwardMeta reads them from the forward's run, if it has one (its object may not have reached
// the store yet), else from its object; a new forward gets new ones.
func (s *Session) forwardMeta(b *Backend, prof *model.KindProfile, ref model.ObjectRef, run *forwardRun) forwardMetaInfo {
	if run != nil {
		return forwardMetaInfo{uid: run.uid, created: run.created}
	}
	h := b.Acquire(prof)
	defer h.Release()
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	if h.WaitForSync(ctx) {
		if obj, ok := KindGet(h, prof, ref.Namespace, ref.Name); ok {
			return forwardMetaInfo{uid: obj.GetUID(), created: obj.GetCreationTimestamp().Time}
		}
	}
	return forwardMetaInfo{uid: types.UID(newUID()), created: s.Now()}
}

// newUID returns a random UID for a forward object.
func newUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// StopForward stops a forward: its listener closes and its connection to the pod ends. The
// forward stays, phase Stopped, and Edit starts it again.
func (s *Session) StopForward(ref model.ObjectRef) (Forward, error) {
	b, _, prof, err := s.forwardsOf(ref.Context)
	if err != nil {
		return Forward{}, err
	}
	f, err := s.forwardOf(b, prof, ref)
	if err != nil {
		return Forward{}, err
	}
	fs := &b.forwards
	fs.opMu.Lock()
	defer fs.opMu.Unlock()
	r := fs.take(f.Ref.Key())
	if r != nil {
		r.cancel()
		r.wait()
		f = r.snapshot()
	}
	f.Phase, f.Message = ForwardStopped, ""
	f.LocalPort, f.Address, f.URL, f.Pod, f.PodPort = 0, "", "", "", 0
	meta := s.forwardMeta(b, prof, f.Ref, r)
	if err := b.PutLocal(prof, forwardObject(f, meta.uid, meta.created)); err != nil {
		return f, err
	}
	return f, nil
}

// RemoveForward stops a forward and deletes it.
func (s *Session) RemoveForward(ref model.ObjectRef) (Forward, error) {
	b, _, prof, err := s.forwardsOf(ref.Context)
	if err != nil {
		return Forward{}, err
	}
	f, err := s.forwardOf(b, prof, ref)
	if err != nil {
		return Forward{}, err
	}
	fs := &b.forwards
	fs.opMu.Lock()
	defer fs.opMu.Unlock()
	if r := fs.take(f.Ref.Key()); r != nil {
		r.cancel()
		r.wait()
	}
	return f, b.DeleteLocal(prof, f.Ref.Namespace, f.Ref.Name)
}

// take removes a forward's run from the set and returns it.
func (fs *forwardSet) take(key string) *forwardRun {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	r := fs.runs[key]
	delete(fs.runs, key)
	return r
}

// ForwardOf returns a forward and its state: the running forward's own, else what its object says.
func (s *Session) ForwardOf(ref model.ObjectRef) (Forward, error) {
	b, _, prof, err := s.forwardsOf(ref.Context)
	if err != nil {
		return Forward{}, err
	}
	return s.forwardOf(b, prof, ref)
}

func (s *Session) forwardOf(b *Backend, prof *model.KindProfile, ref model.ObjectRef) (Forward, error) {
	return s.lookupForward(b, prof, ref, true)
}

// LookupForward is ForwardOf without waiting for the local store's first list: Enabled checks
// use it. A forward that runs is found at once, before its object reaches the store.
func (s *Session) LookupForward(ref model.ObjectRef) (Forward, error) {
	b, _, prof, err := s.forwardsOf(ref.Context)
	if err != nil {
		return Forward{}, err
	}
	return s.lookupForward(b, prof, ref, false)
}

func (s *Session) lookupForward(b *Backend, prof *model.KindProfile, ref model.ObjectRef, wait bool) (Forward, error) {
	if ref.Kind != PortForwardKind {
		return Forward{}, fmt.Errorf("%w: not a port forward: %s", ErrNotApplicable, ref.Kind)
	}
	ref, _, err := s.ResolveRef(ref)
	if err != nil {
		return Forward{}, err
	}
	if r := b.forwards.get(ref.Key()); r != nil {
		return r.snapshot(), nil
	}
	h := b.Acquire(prof)
	defer h.Release()
	if wait {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		if err := waitSynced(ctx, h, prof.Key); err != nil {
			return Forward{}, err
		}
	} else if !h.HasSynced() {
		return Forward{}, fmt.Errorf("%w %s yet", ErrNotSynced, prof.Key)
	}
	obj, ok := KindGet(h, prof, ref.Namespace, ref.Name)
	if !ok {
		return Forward{}, fmt.Errorf("%s: %w", ref.String(), ErrNotFound)
	}
	f := parseForward(obj)
	f.Ref = ref
	return f, nil
}

// forwardObject is a forward as an object of the local kind (model/kinds/portforwards.go).
func forwardObject(f Forward, uid types.UID, created time.Time) *unstructured.Unstructured {
	kind := "Pod"
	if f.Spec.Target.Kind == kindServices {
		kind = "Service"
	}
	protocol := schemeHTTP
	if f.Spec.HTTPS {
		protocol = schemeHTTPS
	}
	spec := map[string]any{
		"kind": kind, metadataName: f.Spec.Target.Name, "port": int64(f.Spec.Port),
		fieldLocalPort: int64(f.Spec.LocalPort), "protocol": protocol, "openInBrowser": f.Spec.OpenInBrowser,
	}
	if f.Spec.Container != "" {
		spec["container"] = f.Spec.Container
	}
	status := map[string]any{"phase": f.Phase}
	if f.Message != "" {
		status["message"] = f.Message
	}
	if f.LocalPort != 0 {
		status[fieldLocalPort], status["address"], status["url"] = int64(f.LocalPort), f.Address, f.URL
	}
	if f.Pod != "" {
		status["pod"], status["podPort"] = f.Pod, int64(f.PodPort)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{"spec": spec, "status": status}}
	obj.SetName(f.Ref.Name)
	obj.SetNamespace(f.Ref.Namespace)
	obj.SetUID(uid)
	obj.SetCreationTimestamp(metav1.NewTime(created.UTC().Truncate(time.Second)))
	return obj
}

// parseForward reads a forward object back.
func parseForward(obj *unstructured.Unstructured) Forward {
	port16 := func(fields ...string) uint16 {
		n, _ := model.NestedInt64(obj.Object, fields...)
		if n < 0 || n > 65535 {
			return 0
		}
		return uint16(n)
	}
	target := model.ObjectRef{Kind: kindPods, Namespace: obj.GetNamespace(), Name: model.NestedString(obj.Object, "spec", "name")}
	if model.NestedString(obj.Object, "spec", "kind") == "Service" {
		target.Kind = kindServices
	}
	open, _ := model.NestedBool(obj.Object, "spec", "openInBrowser")
	return Forward{
		Spec: ForwardSpec{
			Target: target, Container: model.NestedString(obj.Object, "spec", "container"),
			Port: port16("spec", "port"), LocalPort: port16("spec", fieldLocalPort),
			HTTPS: model.NestedString(obj.Object, "spec", "protocol") == schemeHTTPS, OpenInBrowser: open,
		},
		Phase:     model.NestedString(obj.Object, "status", "phase"),
		Message:   model.NestedString(obj.Object, "status", "message"),
		LocalPort: port16("status", fieldLocalPort),
		Address:   model.NestedString(obj.Object, "status", "address"),
		URL:       model.NestedString(obj.Object, "status", "url"),
		Pod:       model.NestedString(obj.Object, "status", "pod"),
		PodPort:   port16("status", "podPort"),
	}
}
