package fyneui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// agentWritesBox is the shown Agents section's check.
func agentWritesBox(sh *shell) *widget.Check {
	c, ok := sh.settings.controls[agentWritesSetting].(*fyne.Container)
	if !ok {
		return nil
	}
	check, _ := c.Objects[0].(*widget.Check)
	return check
}

// A cluster's Settings › Agents, last in the list above the integrations (D-87): the check is off by default; turning it on asks
// first, and Cancel leaves it off; Allow keeps it in Cogpit's cluster settings and the engine then
// lets an agent's mutating command run without asking; turning it off asks nothing. An agent that
// tries the same is refused and the setting stays.
func TestClusterSettingsAgentsSection(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "agents"})
	q.until(t, "the Agents section", func() bool { return agentWritesBox(sh) != nil })
	if labels := navLabels(sh.settings); labels[slices.Index(labels, "—")-1] != agentsSectionLabel {
		t.Errorf("sections %q: Agents last above the integrations", labels)
	}
	check := agentWritesBox(sh)
	if check.Checked || check.Disabled() {
		t.Fatalf("checked %v, disabled %v: want an enabled, unchecked box", check.Checked, check.Disabled())
	}
	if note := sh.settings.controls["agentWritesNote"].(*textLine).text; note != agentWritesNoteOff {
		t.Errorf("note %q", note)
	}

	allowed := func() bool { cs, _ := s.ClusterSettingsOf("kind-dev"); return cs.AgentWrites }
	test.Tap(check)
	q.until(t, "the question", func() bool { return sh.settings.agentsDialog != nil })
	test.Tap(sh.settings.agentsDialog.cancel)
	q.until(t, "the box back off", func() bool { return sh.settings.agentsDialog == nil && !agentWritesBox(sh).Checked })
	if allowed() {
		t.Fatal("Cancel allowed agent writes")
	}

	test.Tap(agentWritesBox(sh))
	q.until(t, "the question", func() bool { return sh.settings.agentsDialog != nil })
	test.Tap(sh.settings.agentsDialog.confirm)
	q.until(t, "the allowance kept", func() bool { return allowed() && sh.ui.ClusterSettings["kind-dev"].AgentWrites })
	q.until(t, "the box on", func() bool {
		return agentWritesBox(sh).Checked && sh.settings.controls["agentWritesNote"].(*textLine).text == agentWritesNoteOn
	})
	if !s.AgentWritesAllowed("kind-dev") || s.AgentWritesAllowed("staging") {
		t.Errorf("the engine's allowance: kind-dev %v, staging %v", s.AgentWritesAllowed("kind-dev"), s.AgentWritesAllowed("staging"))
	}

	// An agent cannot take it back (nor give it).
	raw := mustJSON(t, map[string]any{"context": "kind-dev", "allowed": false})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.settings.setAgentWrites", Params: raw, Origin: engine.OriginExternalAgent}); !errors.Is(err, engine.ErrAgentWritesGUIOnly) {
		t.Errorf("an agent's change: %v", err)
	}
	if !allowed() {
		t.Fatal("an agent changed the allowance")
	}

	test.Tap(agentWritesBox(sh)) // off: no question
	q.until(t, "the allowance off", func() bool { return !allowed() && !agentWritesBox(sh).Checked })
	if sh.settings.agentsDialog != nil {
		t.Error("turning the allowance off asked")
	}
}

// The confirmation of an agent's request says how the allowance bears on it: a mutating command
// points to Cluster Settings › Agents; a destructive one, with the allowance on, says it asks
// anyway; the user's own commands carry no such line.
func TestConfirmationNamesTheAllowance(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	req := engine.ConfirmRequest{Command: "deployment.scale", Label: "Scale", Danger: engine.DangerMutating,
		Origin: engine.OriginExternalAgent, Context: "kind-dev", Namespaces: []string{"apps"},
		Objects: []model.ObjectRef{{Kind: "deployments", Namespace: "apps", Name: "web"}}}
	texts := func() []string {
		var out []string
		for _, p := range findAll[*paragraph](w.Canvas().Overlays().Top()) {
			out = append(out, p.text)
		}
		return out
	}
	ask := func(req engine.ConfirmRequest) []string {
		t.Helper()
		go func() { _, _ = sh.confirmer.Confirm(context.Background(), req) }()
		q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
		got := texts()
		test.Tap(sh.confirmer.current.cancel)
		q.until(t, "the dialog closed", func() bool { return sh.confirmer.current == nil })
		return got
	}
	if got := ask(req); !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "Cluster Settings › Agents") }) {
		t.Errorf("an agent's Scale: %q", got)
	}
	mustDispatch(t, s, "cluster.settings.setAgentWrites", map[string]any{"context": "kind-dev", "allowed": true})
	q.until(t, "the allowance in the window", func() bool { return sh.ui.ClusterSettings["kind-dev"].AgentWrites })
	req.Command, req.Label, req.Danger = "object.delete", "Delete", engine.DangerDestructive
	if got := ask(req); !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "destructive commands always ask") }) {
		t.Errorf("an agent's Delete with the allowance on: %q", got)
	}
	// An agent's role binding asks with the allowance on (H-27): the dialog says so, and does not
	// offer the allowance as a way around the question.
	binding := engine.ConfirmRequest{Command: "roleBinding.create", Label: "Create Role Binding", Danger: engine.DangerMutating,
		Origin: engine.OriginExternalAgent, Context: "kind-dev", Namespaces: []string{"apps"}, AgentsAsk: true,
		Objects: []model.ObjectRef{{Kind: "rolebindings", Namespace: "apps", Name: "ci"}}}
	has := func(texts []string, part string) bool {
		return slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, part) })
	}
	if got := ask(binding); !has(got, "A role binding an agent asks for always waits for you") || has(got, "allow it in Cluster Settings") {
		t.Errorf("an agent's role binding with the allowance on: %q", got)
	}
	req.Origin = engine.OriginGUI
	if got := ask(req); slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "Agents") }) {
		t.Errorf("the user's own Delete names the allowance: %q", got)
	}
}

// Settings › Agents, dark and light, with the allowance on: the PNGs of the pull request (AGENTS.md
// rule 7).
func TestRenderClusterSettingsAgentsDark(t *testing.T) {
	renderClusterSettingsAgents(t, ThemeDark, "cluster_settings_agents_dark.png")
}

func TestRenderClusterSettingsAgentsLight(t *testing.T) {
	renderClusterSettingsAgents(t, ThemeLight, "cluster_settings_agents_light.png")
}

func renderClusterSettingsAgents(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet"})
	mustDispatch(t, s, "cluster.settings.setAgentWrites", map[string]any{"context": "kind-dev", "allowed": true})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "agents"})
	q.until(t, "the Agents section", func() bool {
		c := agentWritesBox(sh)
		return sh.settings.visible() && c != nil && c.Checked
	})
	q.drain()
	savePNG(t, w, name)
}
