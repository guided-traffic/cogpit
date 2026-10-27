package commands_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeForwarder is the port forwarder of these tests: its tunnels answer every connection with
// "pod <name> port <port>", so a test that connects to a forward's local port sees which pod it
// reached. A test can fail the next dials or cut the open tunnels.
type fakeForwarder struct {
	*fakeWriter

	mu      sync.Mutex
	dials   []string // "namespace/pod"
	failing error
	tunnels []*fakeTunnel
}

func (f *fakeForwarder) DialPortForward(ctx context.Context, namespace, pod string) (cluster.PortTunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dials = append(f.dials, namespace+"/"+pod)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.failing != nil {
		return nil, f.failing
	}
	t := &fakeTunnel{pod: pod, done: make(chan struct{})}
	f.tunnels = append(f.tunnels, t)
	return t, nil
}

func (f *fakeForwarder) dialed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dials...)
}

func (f *fakeForwarder) fail(err error) {
	f.mu.Lock()
	f.failing = err
	f.mu.Unlock()
}

// cut closes every open tunnel, as a lost connection to the kubelet does.
func (f *fakeForwarder) cut() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tunnels {
		_ = t.Close()
	}
}

// open counts the tunnels not closed yet.
func (f *fakeForwarder) open() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.tunnels {
		select {
		case <-t.done:
		default:
			n++
		}
	}
	return n
}

type fakeTunnel struct {
	pod  string
	done chan struct{}
	once sync.Once
}

func (t *fakeTunnel) Forward(c net.Conn, port uint16) error {
	defer c.Close()
	select {
	case <-t.done:
		return errors.New("tunnel closed")
	default:
	}
	_, err := fmt.Fprintf(c, "pod %s port %d\n", t.pod, port)
	return err
}

func (t *fakeTunnel) Done() <-chan struct{} { return t.done }

func (t *fakeTunnel) Close() error {
	t.once.Do(func() { close(t.done) })
	return nil
}

// forwardConnector connects "test" and "other" over one source with the test profiles, the port
// forwards' local kind and the fake forwarder; a demo connector has neither connection nor writer.
type forwardConnector struct {
	src    *testkit.MemorySource
	fw     engine.Actions
	demo   bool
	health engine.HealthProber // the connection watch's probes (connection_test.go); nil: no watch
}

func (c *forwardConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "other"}, {Name: "test", Current: true}}, nil
}

func (c *forwardConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	b := &engine.Backend{Context: name, Namespace: "default", Cache: ch, Profiles: append(testProfiles(), kinds.Local()...), Close: ch.Close, Demo: c.demo}
	if !c.demo {
		b.Conn, b.Actions, b.Health = &cluster.Conn{}, c.fw, c.health
	}
	return b, nil
}

// forwardPod is a running, ready pod of app web with a TCP and a UDP port.
func forwardPod(name string, ready bool) *unstructured.Unstructured {
	status := "True"
	if !ready {
		status = "False"
	}
	return testkit.Object("v1", "Pod", "shop", name, map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app": "web"}},
		"spec": map[string]any{"containers": []any{map[string]any{
			"name": "app", "image": "registry.example/web:1",
			"ports": []any{
				map[string]any{"name": "http", "containerPort": int64(8080), "protocol": "TCP"},
				map[string]any{"name": "dns", "containerPort": int64(53), "protocol": "UDP"},
			},
		}}},
		"status": map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": status}}},
	})
}

func forwardSeed() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "shop", nil))
	src.Set(testkit.PodsGVR, forwardPod("web-0", true))
	web := testkit.Service("shop", "web", "10.96.0.80")
	_ = unstructured.SetNestedSlice(web.Object, []any{map[string]any{"name": "http", "port": int64(80), "protocol": "TCP", "targetPort": "http"}}, "spec", "ports")
	src.Set(testkit.ServicesGVR, web)
	external := testkit.Service("shop", "external", "10.96.0.81")
	unstructured.RemoveNestedField(external.Object, "spec", "selector")
	src.Set(testkit.ServicesGVR, external)
	dns := testkit.Service("shop", "dns", "10.96.0.10")
	_ = unstructured.SetNestedSlice(dns.Object, []any{
		map[string]any{"name": "udp", "port": int64(53), "protocol": "UDP"},
		map[string]any{"name": "tcp", "port": int64(53), "protocol": "TCP"},
		map[string]any{"name": "stats", "port": int64(54), "protocol": "UDP"},
	}, "spec", "ports")
	src.Set(testkit.ServicesGVR, dns)
	return src
}

// forwardBackoff is the forwards' first wait before they dial again in these tests: the product's
// 1 s, a wait on the real clock, made short.
const forwardBackoff = 10 * time.Millisecond

func newForwardHarness(t testing.TB, opts engine.Options) (*harness, *fakeForwarder) {
	t.Helper()
	src := forwardSeed()
	fw := &fakeForwarder{fakeWriter: &fakeWriter{}}
	opts.Connector = &forwardConnector{src: src, fw: fw}
	if opts.SnapshotInterval == 0 {
		opts.SnapshotInterval = 10 * time.Millisecond
	}
	if opts.ForwardBackoff == 0 {
		opts.ForwardBackoff = forwardBackoff
	}
	s := engine.New(opts)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"pods", "services", engine.PortForwardKind} {
		if _, err := s.Objects(context.Background(), k, nil); err != nil {
			t.Fatal(err)
		}
	}
	return &harness{s: s, src: src}, fw
}

var (
	web0       = model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	webService = model.ObjectRef{Kind: "services", Namespace: "shop", Name: "web"}
)

// ask connects to a forward's local port and returns the first line the pod side sends.
func ask(t testing.TB, addr string) (string, error) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line), err
}

// waitForward waits until the forward's state satisfies cond.
func waitForward(t testing.TB, s *engine.Session, ref model.ObjectRef, what string, cond func(engine.Forward) bool) engine.Forward {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var f engine.Forward
	var err error
	if !testkit.WaitFor(ctx, func() bool {
		f, err = s.ForwardOf(ref)
		return err == nil && cond(f)
	}) {
		t.Fatalf("forward %s: %s did not happen (last %+v, %v)", ref.Name, what, f, err)
	}
	return f
}

// assertLoopbackOnly checks that a forward listens on 127.0.0.1 and on no other address.
func assertLoopbackOnly(t *testing.T, f engine.Forward) {
	t.Helper()
	host, port, err := net.SplitHostPort(f.Address)
	if err != nil || host != "127.0.0.1" || port != strconv.Itoa(int(f.LocalPort)) {
		t.Fatalf("address %q (local port %d), want 127.0.0.1:<port>", f.Address, f.LocalPort)
	}
	if f.URL != "http://127.0.0.1:"+port && f.URL != "https://127.0.0.1:"+port {
		t.Errorf("URL %q", f.URL)
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		if c, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), port), 200*time.Millisecond); err == nil {
			c.Close()
			t.Errorf("the forward answers on %s", ipn.IP)
		}
	}
	if c, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", port), 200*time.Millisecond); err == nil {
		c.Close()
		t.Error("the forward answers on ::1")
	}
}

// TestForwardURLReachesTheForward: the URL a forward returns (and the GUI opens) reaches the
// forward's own listener, never a socket another local process holds on [::1] at the same port:
// the listener is on 127.0.0.1 only, and "localhost" resolves to ::1 first on macOS (curl, Go's
// HTTP client and browsers then connect there).
func TestForwardURLReachesTheForward(t *testing.T) {
	squatter, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		// A plain skip in CI too: the Linux runners are pods, whose node decides whether they
		// have IPv6, not the job.
		t.Skip("no IPv6 loopback:", err)
	}
	t.Cleanup(func() { _ = squatter.Close() })
	go func() {
		for {
			c, err := squatter.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("squatter\n"))
			_ = c.Close()
		}
	}()
	port := uint16(squatter.Addr().(*net.TCPAddr).Port)

	h, _ := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "container": "app", "port": 8080, "localPort": port}, &f)
	u, err := url.Parse(f.URL)
	if err != nil {
		t.Fatalf("URL %q: %v", f.URL, err)
	}
	c, err := net.DialTimeout("tcp", u.Host, 2*time.Second) // what a client of the URL dials
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	line, _ := bufio.NewReader(c).ReadString('\n')
	if got := strings.TrimSpace(line); got != "pod web-0 port 8080" {
		t.Errorf("the forward's URL %s reached %q, not the forward on %s", f.URL, got, f.Address)
	}
}

// The life of a pod forward from the GUI: start on a free port, reach the pod, see it in the Port
// Forwarding view, open, edit (another port, https), stop, start again with Edit, delete.
func TestPortForwardLifecycle(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	gui := engine.OriginGUI
	var f engine.Forward
	h.must(t, gui, "pod.portForward", map[string]any{"ref": web0, "container": "app", "port": 8080, "openInBrowser": true}, &f)
	if f.Phase != engine.ForwardActive || f.Pod != "web-0" || f.PodPort != 8080 || !f.Spec.OpenInBrowser || f.Ref.Name != "pod-web-0-8080" {
		t.Fatalf("started %+v", f)
	}
	assertLoopbackOnly(t, f)
	if got, err := ask(t, f.Address); got != "pod web-0 port 8080" {
		t.Fatalf("through the forward: %q, %v", got, err)
	}

	h.must(t, gui, "ui.openView", map[string]any{"view": engine.PortForwardKind}, nil)
	snap := waitTable(t, h.s, engine.PortForwardKind, func(ts *engine.TableSnapshot) bool {
		return len(ts.Rows) == 1 && ts.Rows[0].Cells[6].Text == "Active"
	})
	cells := []string{}
	for i, c := range snap.Rows[0].Cells {
		cells = append(cells, model.CellText(snap.Columns[i], c, time.Now()))
	}
	if got, want := strings.Join(cells, "|"), "web-0|shop|Pod|8080|"+strconv.Itoa(int(f.LocalPort))+"|http|Active"; got != want {
		t.Errorf("row %s, want %s", got, want)
	}

	ref := f.Ref
	var opened engine.Forward
	h.must(t, gui, "portforward.open", map[string]any{"ref": ref}, &opened)
	if opened.URL != f.URL {
		t.Errorf("open: %+v", opened)
	}

	// Edit: another local port and https; the old port closes.
	free := freePort(t)
	var edited engine.Forward
	h.must(t, gui, "portforward.edit", map[string]any{"ref": ref, "localPort": free, "https": true}, &edited)
	if edited.LocalPort != free || edited.URL != "https://127.0.0.1:"+strconv.Itoa(int(free)) || edited.Phase != engine.ForwardActive || !edited.Spec.OpenInBrowser {
		t.Fatalf("edited %+v", edited)
	}
	if _, err := ask(t, f.Address); err == nil {
		t.Error("the old local port still answers")
	}
	if got, _ := ask(t, edited.Address); got != "pod web-0 port 8080" {
		t.Errorf("after edit: %q", got)
	}

	var stopped engine.Forward
	h.must(t, gui, "portforward.stop", map[string]any{"ref": ref}, &stopped)
	if stopped.Phase != engine.ForwardStopped || stopped.Listening() {
		t.Fatalf("stopped %+v", stopped)
	}
	if _, err := ask(t, edited.Address); err == nil {
		t.Error("a stopped forward still answers")
	}
	waitForward(t, h.s, ref, "the cache shows it stopped", func(f engine.Forward) bool { return f.Phase == engine.ForwardStopped })
	for _, id := range []string{"portforward.open", "portforward.stop"} {
		if err := h.run(t, gui, id, map[string]any{"ref": ref}, nil); err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Errorf("%s on a stopped forward: %v", id, err)
		}
	}
	if fw.open() != 0 {
		t.Errorf("%d tunnels open after stop", fw.open())
	}

	// Edit starts a stopped forward again, with its settings.
	var again engine.Forward
	h.must(t, gui, "portforward.edit", map[string]any{"ref": ref}, &again)
	if again.Phase != engine.ForwardActive || again.LocalPort != free || !again.Spec.HTTPS {
		t.Fatalf("started again %+v", again)
	}

	h.must(t, gui, "portforward.remove", map[string]any{"ref": ref}, nil)
	if _, err := ask(t, again.Address); err == nil {
		t.Error("a deleted forward still answers")
	}
	waitTable(t, h.s, engine.PortForwardKind, func(ts *engine.TableSnapshot) bool { return len(ts.Rows) == 0 })
	if err := h.run(t, gui, "portforward.open", map[string]any{"ref": ref}, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("open after delete: %v", err)
	}
	if got := fw.dialed(); len(got) != 3 {
		t.Errorf("dials %v, want 3 (start, edit, start again)", got)
	}
}

func freePort(t testing.TB) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

// A service forward follows the cache: when its pod goes, the forward moves to another ready pod
// of the selector; with none, connections wait until one is ready.
func TestServiceForwardMovesToAnotherPod(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "service.portForward", map[string]any{"ref": webService, "port": 80}, &f)
	if f.Phase != engine.ForwardActive || f.Pod != "web-0" || f.PodPort != 8080 || f.Ref.Name != "service-web-80" {
		t.Fatalf("started %+v", f)
	}
	assertLoopbackOnly(t, f)
	if got, _ := ask(t, f.Address); got != "pod web-0 port 8080" {
		t.Fatalf("through the service forward: %q", got)
	}

	// The pod is replaced: web-0 goes, web-1 comes up not ready, then ready.
	h.src.Delete(testkit.PodsGVR, "shop", "web-0")
	waitForward(t, h.s, f.Ref, "waiting for a ready pod", func(f engine.Forward) bool {
		return f.Phase == engine.ForwardWaiting && strings.Contains(f.Message, "no ready pod")
	})
	answered := make(chan string, 1)
	go func() { line, _ := ask(t, f.Address); answered <- line }() // waits for the next pod
	h.src.Set(testkit.PodsGVR, forwardPod("web-1", false))
	time.Sleep(50 * time.Millisecond)
	if fw.open() != 0 {
		t.Errorf("connected to a pod that is not ready")
	}
	h.src.Set(testkit.PodsGVR, forwardPod("web-1", true))
	moved := waitForward(t, h.s, f.Ref, "connected to web-1", func(f engine.Forward) bool { return f.Phase == engine.ForwardActive && f.Pod == "web-1" })
	if moved.LocalPort != f.LocalPort {
		t.Errorf("the local port changed from %d to %d", f.LocalPort, moved.LocalPort)
	}
	select {
	case got := <-answered:
		if got != "pod web-1 port 8080" {
			t.Errorf("the waiting connection got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the connection made while no pod was ready was never answered")
	}
}

// A pod forward waits while its pod does not run and connects again when it does; a lost
// connection is dialled again.
func TestPodForwardReconnects(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &f)

	fw.cut() // the kubelet drops the connection
	waitForward(t, h.s, f.Ref, "dialled again", func(engine.Forward) bool { return len(fw.dialed()) == 2 && fw.open() == 1 })
	waitForward(t, h.s, f.Ref, "active again", func(f engine.Forward) bool { return f.Phase == engine.ForwardActive })

	pending := forwardPod("web-0", false)
	_ = unstructured.SetNestedField(pending.Object, "Pending", "status", "phase")
	h.src.Set(testkit.PodsGVR, pending)
	waitForward(t, h.s, f.Ref, "waiting for the pod", func(f engine.Forward) bool {
		return f.Phase == engine.ForwardWaiting && f.Message == "pod web-0 is pending" && fw.open() == 0
	})
	h.src.Set(testkit.PodsGVR, forwardPod("web-0", true))
	waitForward(t, h.s, f.Ref, "connected again", func(f engine.Forward) bool { return f.Phase == engine.ForwardActive && fw.open() == 1 })
	if got, _ := ask(t, f.Address); got != "pod web-0 port 8080" {
		t.Errorf("after the restart: %q", got)
	}
	if n := len(fw.dialed()); n != 3 {
		t.Errorf("%d dials, want 3", n)
	}

	h.src.Delete(testkit.PodsGVR, "shop", "web-0")
	waitForward(t, h.s, f.Ref, "waiting for the deleted pod", func(f engine.Forward) bool {
		return f.Phase == engine.ForwardWaiting && strings.Contains(f.Message, "does not exist")
	})
}

// A forward whose first connection fails does not stay: the command fails, the object says why
// and nothing listens. A local port in use fails the same way.
func TestPortForwardFailsToStart(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	fw.fail(errors.New(`pods "web-0" is forbidden: User "dev" cannot create resource "pods/portforward"`))
	err := h.run(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, nil)
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("start: %v", err)
	}
	ref := model.ObjectRef{Kind: engine.PortForwardKind, Namespace: "shop", Name: "pod-web-0-8080"}
	f := waitForward(t, h.s, ref, "failed", func(f engine.Forward) bool { return f.Phase == engine.ForwardFailed })
	if f.Listening() || !strings.Contains(f.Message, "forbidden") {
		t.Errorf("failed forward %+v", f)
	}

	fw.fail(nil)
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	err = h.run(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080, "localPort": port}, nil)
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:"+strconv.Itoa(port)) {
		t.Fatalf("a local port in use: %v", err)
	}
}

// A context's forwards go on while the window shows another cluster (D-69), and every one ends
// when the context is disconnected: the listener closes, the connection to the pod ends.
func TestPortForwardsEndWithTheBackend(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	var a, b engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &a)
	h.must(t, engine.OriginGUI, "service.portForward", map[string]any{"ref": webService, "port": 80}, &b)
	if fw.open() != 2 {
		t.Fatalf("%d tunnels open", fw.open())
	}
	if err := h.s.Connect(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	if fw.open() != 2 {
		t.Errorf("%d tunnels open while another cluster is shown", fw.open())
	}
	other := a.Ref
	other.Context = "other"
	if _, err := h.s.ForwardOf(other); err == nil {
		t.Error("the shown context has the other context's forward")
	}
	if f, err := h.s.ForwardOf(a.Ref); err != nil || !f.Listening() {
		t.Errorf("test's forward while other is shown: %+v %v", f, err)
	}
	if err := h.s.DisconnectContext(a.Ref.Context); err != nil {
		t.Fatal(err)
	}
	for _, f := range []engine.Forward{a, b} {
		if _, err := ask(t, f.Address); err == nil {
			t.Errorf("%s still answers after its cluster was disconnected", f.Ref.Name)
		}
	}
	if fw.open() != 0 {
		t.Errorf("%d tunnels open after its cluster was disconnected", fw.open())
	}
}

// Starting a forward opens a stream into a pod: agents need the user's acceptance (D-2) and are
// audited; stopping, opening and deleting are UI commands that run freely.
func TestPortForwardConfirmationPerOrigin(t *testing.T) {
	conf := &confirmations{answer: false}
	audit := &syncBuffer{}
	h, fw := newForwardHarness(t, engine.Options{Confirmer: conf, Audit: audit})
	agent := engine.OriginExternalAgent
	params := map[string]any{"ref": web0, "port": 8080}

	err := h.run(t, agent, "pod.portForward", params, nil)
	if !errors.Is(err, engine.ErrDeclined) || len(fw.dialed()) != 0 {
		t.Fatalf("declined: %v, dials %v", err, fw.dialed())
	}
	req := conf.last(t)
	if req.Danger != engine.DangerMutating || len(req.Objects) != 1 || req.Objects[0].Name != "web-0" || req.Context != "test" ||
		!strings.Contains(req.Detail, "Listens on: 127.0.0.1") || !strings.Contains(req.Detail, "Forwards to: port 8080.") {
		t.Errorf("confirmation %+v", req)
	}

	conf.mu.Lock()
	conf.answer = true
	conf.mu.Unlock()
	var f engine.Forward
	h.must(t, agent, "pod.portForward", params, &f)
	if f.URL == "" || !f.Listening() {
		t.Fatalf("agent's forward %+v", f)
	}
	h.must(t, agent, "portforward.edit", map[string]any{"ref": f.Ref, "https": true}, nil)
	if n := len(conf.all()); n != 3 {
		t.Errorf("%d confirmations, want 3 (declined, start, edit)", n)
	}
	var opened engine.Forward
	h.must(t, agent, "portforward.open", map[string]any{"ref": f.Ref}, &opened)
	h.must(t, agent, "portforward.stop", map[string]any{"ref": f.Ref}, nil)
	// A stopped forward is found in the cache only, which shows the stop a moment later.
	waitForward(t, h.s, f.Ref, "the cache shows it stopped", func(f engine.Forward) bool { return f.Phase == engine.ForwardStopped })
	h.must(t, agent, "portforward.remove", map[string]any{"ref": f.Ref}, nil)
	if n := len(conf.all()); n != 3 {
		t.Errorf("open, stop and delete asked for confirmation: %d", n)
	}
	if !strings.HasPrefix(opened.URL, "https://127.0.0.1:") {
		t.Errorf("the agent's open: %+v", opened)
	}
	var ids []string
	for _, e := range audit.Lines() {
		ids = append(ids, e.Command)
	}
	for _, id := range []string{"pod.portForward", "portforward.edit", "portforward.open", "portforward.stop", "portforward.remove"} {
		if !strings.Contains(strings.Join(ids, ","), id) {
			t.Errorf("%s not in the audit log: %v", id, ids)
		}
	}

	// Without a GUI to ask, an agent cannot start a forward.
	h.s.SetConfirmer(nil)
	if err := h.run(t, agent, "service.portForward", map[string]any{"ref": webService, "port": 80}, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("headless agent: %v", err)
	}
	// The user may allow agent writes for the context: then no question.
	h.s.SetAgentWrites("test", true)
	h.must(t, agent, "service.portForward", map[string]any{"ref": webService, "port": 80}, nil)
}

func TestPortForwardDisabledReasons(t *testing.T) {
	h, _ := newForwardHarness(t, engine.Options{})
	reg := h.s.Registry()
	reason := func(id string, params string) string {
		cmd, ok := reg.Get(id)
		if !ok {
			t.Fatalf("no %s", id)
		}
		if err := cmd.Enabled(h.s, []byte(params)); err != nil {
			return err.Error()
		}
		return ""
	}
	pod := `{"ref":{"kind":"pods","namespace":"shop","name":"web-0"}`
	svc := `{"ref":{"kind":"services","namespace":"shop","name":"%s"},"port":%d}`
	for _, tt := range []struct{ id, params, want string }{
		{"pod.portForward", pod + `,"port":8080}`, ""},
		{"pod.portForward", pod + `,"port":53}`, "UDP"},
		{"pod.portForward", pod + `,"port":0}`, "port is required"},
		{"pod.portForward", pod + `,"port":8080,"container":"sidecar"}`, `no container "sidecar"`},
		{"pod.portForward", `{"ref":{"kind":"pods","namespace":"shop","name":"gone"},"port":80}`, "not found"},
		{"service.portForward", fmt.Sprintf(svc, "web", 80), ""},
		{"service.portForward", fmt.Sprintf(svc, "web", 81), "has no port 81"},
		{"service.portForward", fmt.Sprintf(svc, "external", 80), "no selector"},
		{"service.portForward", fmt.Sprintf(svc, "dns", 53), ""}, // TCP and UDP
		{"service.portForward", fmt.Sprintf(svc, "dns", 54), "is UDP"},
		{"portforward.open", `{"ref":{"kind":"portforwards","namespace":"shop","name":"pod-web-0-1"}}`, "not found"},
	} {
		if got := reason(tt.id, tt.params); (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
			t.Errorf("%s %s: %q, want %q", tt.id, tt.params, got, tt.want)
		}
	}
}

// The demo cluster has no connection: no forwards there.
func TestPortForwardNotOnTheDemoCluster(t *testing.T) {
	src := forwardSeed()
	s := engine.New(engine.Options{Connector: &forwardConnector{src: src, demo: true}, SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Objects(context.Background(), "pods", nil); err != nil {
		t.Fatal(err)
	}
	cmd, _ := s.Registry().Get("pod.portForward")
	if err := cmd.Enabled(s, []byte(`{"ref":{"kind":"pods","namespace":"shop","name":"web-0"},"port":8080}`)); err == nil {
		t.Error("pod.portForward enabled on the demo cluster")
	}
}

// The drawers' port actions (the pods and services profiles) dispatch as they are: the port link
// forwards to a free port and asks to open the browser, Forward… carries what the dialog needs.
func TestDrawerPortActionsDispatch(t *testing.T) {
	h, _ := newForwardHarness(t, engine.Options{})
	for _, tc := range []struct {
		kind string
		obj  *unstructured.Unstructured
	}{
		{"pods", forwardPod("web-0", true)},
		{"services", func() *unstructured.Unstructured {
			objs, _ := h.s.Objects(context.Background(), "services", []string{"shop"})
			for _, o := range objs {
				if o.GetName() == "web" {
					return o
				}
			}
			t.Fatal("no service web")
			return nil
		}()},
	} {
		p, _ := kinds.ByKey(tc.kind)
		ref := model.ObjectRef{Kind: tc.kind, Namespace: "shop", Name: tc.obj.GetName()}
		m, err := engine.BuildDetail(p, ref, tc.obj, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var acts []model.ItemAction
		for _, sec := range m.Sections {
			for _, it := range sec.Items {
				acts = append(acts, it.Actions...)
			}
		}
		if len(acts) != 2 || !acts[0].Link || acts[1].Link {
			t.Fatalf("%s: actions %+v (the UDP port must have none)", tc.kind, acts)
		}
		for _, a := range acts {
			var f engine.Forward
			h.must(t, engine.OriginGUI, a.Command, a.Params, &f)
			if f.Phase != engine.ForwardActive || f.Spec.OpenInBrowser != a.Link {
				t.Errorf("%s %s: %+v", tc.kind, a.Label, f)
			}
		}
	}
}

// Forwards are objects like any other for agents: resources_list, resources_find and object_get
// read them unchanged, and the row menu is the captured one (Open, Edit, Stop, Delete): Edit and
// Delete of API objects do not apply to a local kind.
func TestPortForwardsForAgentsAndMenus(t *testing.T) {
	h, _ := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "service.portForward", map[string]any{"ref": webService, "port": 80}, &f)
	waitForward(t, h.s, f.Ref, "in the store", func(engine.Forward) bool { _, err := h.s.Lookup(f.Ref); return err == nil })
	waitForward(t, h.s, f.Ref, "active", func(fw engine.Forward) bool { return fw.Phase == "Active" })
	actx, acancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer acancel()
	if !testkit.WaitFor(actx, func() bool {
		obj, err := h.s.Lookup(f.Ref)
		return err == nil && model.NestedString(obj, "status", "phase") == "Active"
	}) {
		t.Fatal("the store never showed the forward active")
	}
	agent := engine.OriginExternalAgent

	var kinds []commands.KindInfo // Port Forwarding is a local kind: the GUI shows no "+" on it
	h.must(t, agent, "kinds.list", nil, &kinds)
	for _, k := range kinds {
		if k.Local != (k.Kind == engine.PortForwardKind || k.Kind == "helmcharts") || k.Virtual { // the Helm charts are local too (G11)
			t.Errorf("kinds.list: %s local %v, virtual %v", k.Kind, k.Local, k.Virtual)
		}
	}

	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": engine.PortForwardKind}, &list)
	if list.Total != 1 || list.Rows[0].Cells["Name"] != "web" || list.Rows[0].Cells["Kind"] != "Service" ||
		list.Rows[0].Cells["Local Port"] != strconv.Itoa(int(f.LocalPort)) || list.Rows[0].Cells["Status"] != "Active" {
		t.Errorf("resources.list: %+v", list)
	}
	var found commands.FindResult
	h.must(t, agent, "resources.find", map[string]any{"kind": engine.PortForwardKind, "field": "localPort", "value": strconv.Itoa(int(f.LocalPort))}, &found)
	if found.Count != 1 || found.Matches[0].Ref.Name != "service-web-80" {
		t.Errorf("resources.find: %+v", found)
	}
	var detail engine.DetailModel
	h.must(t, agent, "object.get", map[string]any{"ref": f.Ref, "noYaml": true}, &detail)
	open := false
	for _, it := range detail.Sections[0].Items {
		for _, a := range it.Actions {
			open = open || (it.Name == "URL" && it.Value == f.URL && a.Command == "portforward.open" && a.Link)
		}
	}
	if !open {
		t.Errorf("object.get has no URL that opens the forward: %+v", detail.Sections)
	}

	var labels []string
	for _, c := range h.s.Registry().Actions(engine.PortForwardKind) {
		if !c.DrawerOnly && c.Applicable(h.s, c.ObjectParams(f.Ref, "")) { // the row menu leaves drawer-only Copy out
			labels = append(labels, c.Label)
		}
	}
	if got := strings.Join(labels, ", "); got != "Open, Edit, Stop, Delete" {
		t.Errorf("row menu %s", got)
	}
}
