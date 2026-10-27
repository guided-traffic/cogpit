package commands_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// downProber is a connection that answers until the test takes it down.
type downProber struct{ down atomic.Bool }

func (p *downProber) Heartbeat(ctx context.Context) error {
	if p.down.Load() {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (p *downProber) Reachable(context.Context) error {
	if p.down.Load() {
		return errors.New("network is unreachable")
	}
	return nil
}
func (p *downProber) SetHealth(cluster.Health) {}
func (p *downProber) ResetConnections()        {}
func (p *downProber) ArmLogin()                {}

func watchedHarness(t *testing.T) (*harness, *downProber) {
	t.Helper()
	src := seed()
	p := &downProber{}
	h := &harness{src: src, writer: &fakeWriter{}}
	s := engine.New(engine.Options{
		Connector: &fakeConnector{src: src, writer: h.writer, health: p}, SnapshotInterval: 10 * time.Millisecond,
		ConnWatch: engine.ConnWatchOptions{Heartbeat: 20 * time.Millisecond, ProbeTimeout: 50 * time.Millisecond, Confirm: 10 * time.Millisecond,
			LoginWait: 300 * time.Millisecond, BackoffFirst: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond},
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h.s = s
	return h, p
}

func waitConn(t *testing.T, s *engine.Session, want engine.ConnectionState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == want }) {
		t.Fatalf("connection %s, want %s", s.UIState().Connection.State, want)
	}
}

// TestConnectionCommands: every write is refused with "connection interrupted" for the window and
// agents at once; dock.reconnect opens a new session in a shell tab the interruption ended once the
// connection is back, an agent's waits for the user, a node shell's always; cluster.login is the
// window's. cluster.reconnect has its own tests (reconnect_test.go, D-81).
func TestConnectionCommands(t *testing.T) {
	h, p := watchedHarness(t)
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	pod := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-0"}
	var de *engine.DisabledError
	var shell commands.DockResult
	h.must(t, gui, "pod.shell", map[string]any{"ref": pod}, &shell)
	h.s.SetAgentWrites("test", true)

	p.down.Store(true)
	waitConn(t, h.s, engine.Interrupted)
	for _, o := range []engine.Origin{gui, agent} {
		err := h.run(t, o, "object.delete", map[string]any{"ref": pod}, nil)
		if !errors.As(err, &de) || de.Reason != "connection interrupted" {
			t.Errorf("%s's delete while interrupted: %v", o, err)
		}
		// The selection bar's Delete names this reason in its tooltip (fyneui/tablecheck.go).
		err = h.run(t, o, "object.deleteMany", map[string]any{"refs": []model.ObjectRef{pod}}, nil)
		if !errors.As(err, &de) || de.Reason != "connection interrupted" {
			t.Errorf("%s's delete of a selection while interrupted: %v", o, err)
		}
		if err := h.run(t, o, "node.cordon", map[string]any{"ref": model.ObjectRef{Context: "test", Kind: "nodes", Name: "n1"}}, nil); !errors.As(err, &de) {
			t.Errorf("%s's cordon while interrupted: %v", o, err)
		}
	}
	if err := h.run(t, agent, "resources.list", map[string]any{"kind": "pods"}, nil); err != nil {
		t.Errorf("an agent's read while interrupted: %v", err)
	}
	// The interruption ended the shell's session (the window had it open): its tab says so.
	h.s.UpdateUI(func(u *engine.UIState) {
		for i := range u.Dock {
			if u.Dock[i].ID == shell.Tab.ID {
				u.Dock[i].Interrupted = time.Now()
			}
		}
	})
	if err := h.run(t, gui, "dock.reconnect", map[string]any{"id": shell.Tab.ID}, nil); !errors.As(err, &de) || de.Reason != "connection interrupted" {
		t.Errorf("dock.reconnect while interrupted: %v", err)
	}
	if err := h.run(t, agent, "cluster.login", nil, nil); !errors.As(err, new(*engine.GUIOnlyError)) {
		t.Errorf("an agent's cluster.login: %v", err)
	}

	p.down.Store(false)
	waitConn(t, h.s, engine.Connected)
	var again commands.DockResult
	h.must(t, gui, "dock.reconnect", map[string]any{"id": shell.Tab.ID}, &again)
	if again.Tab.Session != 1 || !again.Tab.Interrupted.IsZero() {
		t.Errorf("the tab after dock.reconnect: %+v", again.Tab)
	}
	if err := h.run(t, gui, "dock.reconnect", map[string]any{"id": shell.Tab.ID}, nil); !errors.As(err, &de) {
		t.Errorf("dock.reconnect on a working tab: %v", err)
	}
	// An agent's reconnect of a node shell is destructive: it waits for the user (none here).
	ns := engine.DockTab{ID: "node-shell-9", Type: engine.DockNodeShell, Title: "Node shell: n1", Context: "test",
		Ref: &model.ObjectRef{Context: "test", Kind: "nodes", Name: "n1"}, Interrupted: time.Now(),
		NodeShell: &engine.NodeShellTab{Node: "n1", Namespace: "kube-system", Pod: "cogpit-node-shell-x"}}
	h.s.UpdateUI(func(u *engine.UIState) { u.Dock = append(u.Dock, ns) })
	if err := h.run(t, agent, "dock.reconnect", map[string]any{"id": ns.ID}, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("an agent's node shell reconnect: %v", err)
	}
}

// TestAPortForwardWaitsForTheConnection is S7's port forwards: the interruption ends the forward's
// tunnel, no dial goes out while the connection is interrupted, and the forward dials at once when
// it is back, its local port kept. The forward's own backoff is an hour: any dial within the wait
// came at once.
func TestAPortForwardWaitsForTheConnection(t *testing.T) {
	src := forwardSeed()
	fw := &fakeForwarder{fakeWriter: &fakeWriter{}}
	p := &downProber{}
	s := engine.New(engine.Options{
		Connector: &forwardConnector{src: src, fw: fw, health: p}, SnapshotInterval: 10 * time.Millisecond,
		ConnWatch: engine.ConnWatchOptions{Heartbeat: 20 * time.Millisecond, ProbeTimeout: 50 * time.Millisecond, Confirm: 10 * time.Millisecond,
			LoginWait: 300 * time.Millisecond, BackoffFirst: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond},
		ForwardBackoff: time.Hour,
	})
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
	h := &harness{s: s, src: src}
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return fw.open() == 1 }) {
		t.Fatal("no tunnel")
	}
	dials := len(fw.dialed())
	p.down.Store(true)
	waitConn(t, s, engine.Interrupted)
	if !testkit.WaitFor(ctx, func() bool { return fw.open() == 0 }) {
		t.Fatal("the tunnel stayed open through the interruption")
	}
	waitForward(t, s, f.Ref, "waiting for the connection", func(f engine.Forward) bool {
		return f.Phase == engine.ForwardReconnecting && strings.Contains(f.Message, "the connection to the cluster is interrupted")
	})
	time.Sleep(50 * time.Millisecond) // a loop that did not wait for the connection would have dialled at once
	if n := len(fw.dialed()); n != dials {
		t.Fatalf("%d dials while interrupted", n-dials)
	}
	p.down.Store(false)
	waitConn(t, s, engine.Connected)
	if !testkit.WaitFor(ctx, func() bool { return fw.open() == 1 }) {
		t.Fatal("no dial after the reconnect")
	}
}
