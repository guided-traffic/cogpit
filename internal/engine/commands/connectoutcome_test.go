package commands_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// heldConnector is the harness's connector with one more context, "held", whose connect waits as a
// login in a browser waits: until its context ends (Disconnect, the session's close).
type heldConnector struct{ *fakeConnector }

func (c heldConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "held"}, {Name: "other"}, {Name: "test", Current: true}}, nil
}

func (c heldConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	if name == "held" {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}
	return c.fakeConnector.Connect(ctx, name)
}

// heldHarness is a harness on heldConnector, connected to "test", with "held" on the hotbar's top
// tile and a tab of "held" (as the last session's tabs come back, D-41).
func heldHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{src: seed(), writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: heldConnector{&fakeConnector{src: h.src, writer: h.writer}},
		Hotbar: engine.NewHotbar(t.TempDir()), SnapshotInterval: 10 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	h.must(t, engine.OriginGUI, "cluster.connect", map[string]any{"context": "test"}, nil)
	h.must(t, engine.OriginGUI, "hotbar.addCluster", map[string]any{"context": "held"}, nil)
	h.s.UpdateUI(func(u *engine.UIState) {
		u.Tabs = append(u.Tabs, engine.ViewTab{ID: "tab-held", Context: "held", View: "pods", Title: "Pods - held"})
	})
	return h
}

// The commands that connect a cluster, as the window and agents dispatch them.
var connectCommands = []struct {
	id     string
	params map[string]any
	result bool // answers a ConnectResult; else the UI state
}{
	{"cluster.connect", map[string]any{"context": "held"}, true},
	{"hotbar.openCluster", map[string]any{"slot": 1}, true},
	{"ui.tab.select", map[string]any{"id": "tab-held"}, false},
	{"ui.setNavigatorScope", map[string]any{"scope": engine.NavScopeCluster("held")}, false},
}

// connectingHeld dispatches c as an agent and waits until the window shows "held" connecting; the
// command's result and error arrive on the channel.
func connectingHeld(t *testing.T, h *harness, id string, params map[string]any) <-chan dispatched {
	t.Helper()
	out := make(chan dispatched, 1)
	go func() {
		var res struct {
			Context string                 `json:"context"`
			State   engine.ConnectionState `json:"state"`
		}
		err := h.run(t, engine.OriginExternalAgent, id, params, &res)
		out <- dispatched{context: res.Context, state: res.State, err: err}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		u := h.s.UIState()
		return u.Context == "held" && u.Connection.State == engine.Connecting
	}) {
		t.Fatalf("%s: held never connected: %+v", id, h.s.UIState().Connection)
	}
	return out
}

type dispatched struct {
	context string
	state   engine.ConnectionState
	err     error
}

// Another cluster shown while a command's connect waits: every command answers without an error
// (the owner's AW16 of 2026-10-22; D-97 had it for cluster.connect only): cluster.connect and
// hotbar.openCluster say "held" is connecting, the tab and the navigator answer the UI state as
// it stood when the connect stopped waiting; the window shows the cluster chosen last, and the
// connect goes on in the background.
func TestConnectCommandsAnswerABackgroundConnect(t *testing.T) {
	for _, c := range connectCommands {
		t.Run(c.id, func(t *testing.T) {
			h := heldHarness(t)
			got := connectingHeld(t, h, c.id, c.params)
			h.must(t, engine.OriginGUI, "cluster.connect", map[string]any{"context": "test"}, nil) // connected already: only shown
			d := <-got
			if d.err != nil {
				t.Fatalf("%s: %v, want an answer without an error", c.id, d.err)
			}
			if want := (dispatched{context: "held", state: engine.Connecting}); c.result && d != want {
				t.Errorf("%s answered %+v, want %+v", c.id, d, want)
			}
			u := h.s.UIState()
			if u.Context != "test" || u.Connection.State != engine.Connected {
				t.Errorf("shown %q %s", u.Context, u.Connection.State)
			}
			if e, ok := u.ConnectionOf("held"); !ok || e.Connection.State != engine.Connecting {
				t.Errorf("held's entry while it goes on: %+v (%v)", e, ok)
			}
		})
	}
}

// Cancel Connecting while a command's connect waits (cluster.disconnect, the window's or an
// agent's): every command says it with engine.ErrConnectCancelled (AW15), which the window shows no
// toast for and an agent reads as cancelled, never as a superseded connect.
func TestConnectCommandsAnswerACancel(t *testing.T) {
	for _, c := range connectCommands {
		t.Run(c.id, func(t *testing.T) {
			h := heldHarness(t)
			got := connectingHeld(t, h, c.id, c.params)
			h.must(t, engine.OriginGUI, "cluster.disconnect", map[string]any{}, nil)
			d := <-got
			if !errors.Is(d.err, engine.ErrConnectCancelled) || !engine.ConnectNotFailed(d.err) {
				t.Errorf("%s after Cancel Connecting: %v, want engine.ErrConnectCancelled", c.id, d.err)
			}
		})
	}
}
