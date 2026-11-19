package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Several clusters connected at once (D-69): commands choose their cluster.

// poolTestConnector connects "alpha" and "beta", each to its own objects, writer and connection
// watch answers, so that a test tells which cluster a command read or wrote.
type poolTestConnector struct {
	mu       sync.Mutex
	srcs     map[string]*testkit.MemorySource
	writers  map[string]*fakeWriter
	probers  map[string]*downProber
	connects int
}

func newPoolTestConnector() *poolTestConnector {
	c := &poolTestConnector{srcs: map[string]*testkit.MemorySource{}, writers: map[string]*fakeWriter{}, probers: map[string]*downProber{}}
	for _, n := range []string{"alpha", "beta"} {
		src := testkit.NewMemorySource()
		src.Set(testkit.PodsGVR, testkit.Pod("shop", n+"-web"))
		src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", n+"-web", nil))
		c.srcs[n], c.writers[n], c.probers[n] = src, &fakeWriter{}, &downProber{}
	}
	return c
}

func (c *poolTestConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"}}, nil
}

func (c *poolTestConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	c.mu.Lock()
	c.connects++
	c.mu.Unlock()
	src := c.srcs[name]
	if src == nil {
		return nil, errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	}
	ch := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: name, Namespace: "default", Cache: ch, Profiles: testProfiles(), Close: ch.Close,
		Conn: &cluster.Conn{}, Actions: c.writers[name], Health: c.probers[name]}, nil
}

// poolHarness connects alpha, then beta: beta is shown, alpha connected behind it.
func poolHarness(t *testing.T, opts engine.Options) (*harness, *poolTestConnector) {
	t.Helper()
	pc := newPoolTestConnector()
	opts.Connector = pc
	opts.SnapshotInterval = 10 * time.Millisecond
	opts.ConnWatch = engine.ConnWatchOptions{Heartbeat: 20 * time.Millisecond, ProbeTimeout: 50 * time.Millisecond,
		Confirm: 10 * time.Millisecond, BackoffFirst: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond}
	s := engine.New(opts)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, n := range []string{"alpha", "beta"} {
		if err := s.Connect(context.Background(), n); err != nil {
			t.Fatal(err)
		}
	}
	return &harness{s: s, src: pc.srcs["beta"], writer: pc.writers["beta"]}, pc
}

// TestCommandOnABackgroundClusterActsThere: a write on an object of the cluster the window does not
// show goes to that cluster, and the agent's confirmation names its context.
func TestCommandOnABackgroundClusterActsThere(t *testing.T) {
	var asked []engine.ConfirmRequest
	h, pc := poolHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		asked = append(asked, req)
		return true, nil
	})})
	ref := model.ObjectRef{Context: "alpha", Kind: "deployments", Namespace: "shop", Name: "alpha-web"}
	if _, err := h.s.ObjectsIn(context.Background(), "alpha", "deployments", nil); err != nil { // the cache holds it
		t.Fatal(err)
	}
	h.must(t, engine.OriginExternalAgent, "deployment.restart", map[string]any{"ref": ref}, nil)
	if got := pc.writers["alpha"].Calls(); len(got) != 1 || got[0] != "restart shop/alpha-web" {
		t.Errorf("alpha's writes %v", got)
	}
	if got := pc.writers["beta"].Calls(); len(got) != 0 {
		t.Errorf("the shown beta was written to: %v", got)
	}
	if len(asked) != 1 || asked[0].Context != "alpha" {
		t.Errorf("the confirmation named %+v", asked)
	}
	if u := h.s.UIState(); u.Context != "beta" {
		t.Errorf("the write changed the shown cluster to %q", u.Context)
	}
	// Objects of two clusters in one command are refused before anything is asked.
	asked = nil
	refs := []model.ObjectRef{ref, {Context: "beta", Kind: "deployments", Namespace: "shop", Name: "beta-web"}}
	if err := h.run(t, engine.OriginExternalAgent, "object.deleteMany", map[string]any{"refs": refs}, nil); err == nil || len(asked) != 0 {
		t.Errorf("a selection of two clusters: %v, asked %d", err, len(asked))
	}
}

// TestContextParameterReadsAnyConnectedCluster: a read with context reads that connected cluster;
// a context that is not connected is refused and nothing connects.
func TestContextParameterReadsAnyConnectedCluster(t *testing.T) {
	h, pc := poolHarness(t, engine.Options{})
	agent := engine.OriginExternalAgent
	var alpha, shown commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods", "context": "alpha"}, &alpha)
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods"}, &shown)
	if len(alpha.Rows) != 1 || alpha.RefAt(0).Context != "alpha" || alpha.RefAt(0).Name != "alpha-web" {
		t.Errorf("alpha's pods %+v", alpha)
	}
	if len(shown.Rows) != 1 || shown.RefAt(0).Context != "beta" {
		t.Errorf("the shown cluster's pods %+v", shown)
	}
	before := pc.connects
	err := h.run(t, agent, "resources.list", map[string]any{"kind": "pods", "context": "gamma"}, nil)
	if !errors.Is(err, engine.ErrNotConnected) {
		t.Errorf("gamma: %v", err)
	}
	if pc.connects != before {
		t.Error("a read connected gamma")
	}
	var tab commands.DockResult
	h.must(t, engine.OriginGUI, "terminal.open", map[string]any{"context": "alpha"}, &tab)
	if tab.Tab.Context != "alpha" {
		t.Errorf("terminal.open with context: %+v", tab.Tab)
	}
	var st commands.StateResult
	h.must(t, agent, "ui.state", struct{}{}, &st)
	if len(st.Connections) != 2 || st.Connections[0].Context != "alpha" || st.Connections[0].Shown || !st.Connections[1].Shown {
		t.Errorf("ui_state's connections %+v", st.Connections)
	}
}

// TestBackgroundInterruptionDisablesItsWrites: an interrupted cluster the window does not show
// refuses writes to it with the interruption's reason; the shown cluster's run on.
func TestBackgroundInterruptionDisablesItsWrites(t *testing.T) {
	h, pc := poolHarness(t, engine.Options{Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil })})
	pc.probers["alpha"].down.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		c, ok := h.s.UIState().ConnectionOf("alpha")
		return ok && c.Connection.State == engine.Interrupted
	}) {
		t.Fatal("alpha was not interrupted")
	}
	if _, err := h.s.ObjectsIn(context.Background(), "beta", "deployments", nil); err != nil {
		t.Fatal(err)
	}
	var dis *engine.DisabledError
	err := h.run(t, engine.OriginGUI, "deployment.restart", map[string]any{"ref": model.ObjectRef{Context: "alpha", Kind: "deployments", Namespace: "shop", Name: "alpha-web"}}, nil)
	if !errors.As(err, &dis) || dis.Reason != "connection interrupted" {
		t.Errorf("a write to the interrupted alpha: %v", err)
	}
	h.must(t, engine.OriginGUI, "deployment.restart", map[string]any{"ref": model.ObjectRef{Context: "beta", Kind: "deployments", Namespace: "shop", Name: "beta-web"}}, nil)
	if u := h.s.UIState(); u.Connection.State != engine.Connected {
		t.Errorf("the banner's state is the shown cluster's: %s", u.Connection.State)
	}
	if got := pc.writers["alpha"].Calls(); len(got) != 0 {
		t.Errorf("alpha was written to: %v", got)
	}
}

// TestDisconnectWithContext: cluster.disconnect names a connected cluster the window does not show;
// the shown one stays.
func TestDisconnectWithContext(t *testing.T) {
	h, _ := poolHarness(t, engine.Options{})
	var u engine.UIState
	h.must(t, engine.OriginExternalAgent, "cluster.disconnect", map[string]any{"context": "alpha"}, &u)
	if u.Context != "beta" || u.Connection.State != engine.Connected || len(u.Connections) != 1 || u.Connections[0].Context != "beta" {
		t.Errorf("after alpha's disconnect: %q %s %+v", u.Context, u.Connection.State, u.Connections)
	}
	if err := h.run(t, engine.OriginExternalAgent, "cluster.disconnect", map[string]any{"context": "alpha"}, nil); !errors.Is(err, engine.ErrNotConnected) {
		t.Errorf("alpha again: %v", err)
	}
}
