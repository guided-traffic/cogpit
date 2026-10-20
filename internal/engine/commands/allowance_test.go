package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// The allowance of agent writes (D-2, H-14) is a cluster setting only the user changes, and the
// confirmation flow honours it: agents and the CLI are refused (allowing or forbidding) before
// anything is written; the window's change is kept in cluster-settings.json and shown to agents;
// once allowed, an agent's mutating command runs without asking and is still audited, a destructive
// one still asks; forbidden again, the mutating one asks again; a session without a confirmer (a
// headless engine reading the same file) honours no kept allowance.
func TestAgentWritesAllowance(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	audit := &syncBuffer{}
	store := engine.NewClusterSettingsStore(dir)
	h, w := newActionsHarness(t, nil, engine.Options{ClusterSettings: store, Audit: audit, Drains: engine.NewDrains(engine.NewMemoryDrainJournal())})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	file := filepath.Join(dir, engine.ClusterSettingsFile)

	for _, o := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		for _, allowed := range []bool{true, false} {
			err := h.run(t, o, "cluster.settings.setAgentWrites", map[string]any{"allowed": allowed}, nil)
			if !errors.Is(err, engine.ErrAgentWritesGUIOnly) {
				t.Errorf("%s set agentWrites %v: %v, want ErrAgentWritesGUIOnly", o, allowed, err)
			}
		}
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("a refused change wrote the settings file: %v", err)
	}
	if h.s.AgentWritesAllowed("test") || len(c.all()) != 0 {
		t.Fatal("a refused change allowed agent writes or asked the user")
	}

	// Not allowed: an agent's Cordon asks.
	h.must(t, engine.OriginExternalAgent, "node.cordon", actionNodeRef("worker-1"), nil)
	if len(c.all()) != 1 || c.last(t).Command != "node.cordon" {
		t.Fatalf("an agent's cordon did not ask: %+v", c.all())
	}

	// The user allows agent writes for the context in the window.
	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, &view)
	if !view.AgentWrites || view.Context != "test" {
		t.Errorf("the settings after allowing: %+v", view)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), `"agentWrites": true`) {
		t.Fatalf("cluster-settings.json: %s, %v", b, err)
	}
	if !h.s.UIState().ClusterSettings["test"].AgentWrites || !h.s.AgentWritesAllowed("test") || h.s.AgentWritesAllowed("other") {
		t.Errorf("the allowance in the UI state: %+v", h.s.UIState().ClusterSettings)
	}
	h.must(t, engine.OriginExternalAgent, "cluster.settings.get", struct{}{}, &view)
	if !view.AgentWrites {
		t.Error("cluster_settings_get does not show the allowance to agents")
	}

	// Allowed: Uncordon runs without asking and is audited; Drain still asks.
	asked := len(c.all())
	waitCordoned(t, h, "worker-1", true)
	h.must(t, engine.OriginExternalAgent, "node.uncordon", actionNodeRef("worker-1"), nil)
	if len(c.all()) != asked {
		t.Error("an agent's uncordon asked although agent writes are allowed")
	}
	var drained commands.DrainResult
	h.must(t, engine.OriginExternalAgent, "node.drain", actionNodeRef("worker-1"), &drained)
	if len(c.all()) != asked+1 || c.last(t).Danger != engine.DangerDestructive {
		t.Error("an agent's drain ran without the user's confirmation")
	}
	waitJob(t, h, drained.Job.ID) // a background job (D-63): the node counts as draining until it ends
	var uncordoned bool
	for _, l := range audit.Lines() {
		uncordoned = uncordoned || (l.Command == "node.uncordon" && l.Phase == "done" && l.OK && l.Context == "test")
	}
	if !uncordoned {
		t.Error("the allowed uncordon is not in the audit log")
	}
	if calls := w.Calls(); len(calls) == 0 {
		t.Error("no write reached the cluster")
	}

	// Forbidden again: Uncordon (the drain cordoned the node) asks.
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": false}, nil)
	asked = len(c.all())
	waitCordoned(t, h, "worker-1", true)
	h.must(t, engine.OriginExternalAgent, "node.uncordon", actionNodeRef("worker-1"), nil)
	if len(c.all()) != asked+1 {
		t.Error("an agent's uncordon ran without asking after the allowance was taken back")
	}
	if b, _ := os.ReadFile(file); strings.Contains(string(b), "agentWrites") {
		t.Errorf("the forbidden allowance is still in the file: %s", b)
	}

	// A headless session over the same file: the kept allowance does not count, the write is refused.
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	headless, _ := newActionsHarness(t, nil, engine.Options{ClusterSettings: engine.NewClusterSettingsStore(dir)})
	if !headless.s.UIState().ClusterSettings["test"].AgentWrites || headless.s.AgentWritesAllowed("test") {
		t.Fatalf("the headless session: kept %v, allowed %v", headless.s.UIState().ClusterSettings["test"].AgentWrites, headless.s.AgentWritesAllowed("test"))
	}
	if err := headless.run(t, engine.OriginExternalAgent, "node.cordon", actionNodeRef("worker-1"), nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("a headless agent's cordon with a kept allowance: %v, want ErrConfirmationUnavailable", err)
	}
}
