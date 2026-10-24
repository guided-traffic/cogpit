package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// wantGUIOnly are the commands only the user's own action in the window runs, whatever their
// parameters (engine.Spec.GUIOnly). A command that drops the mark fails here, and one that gains it
// is checked by TestGUIOnlyCommandsRefuseOtherOrigins without being listed.
var wantGUIOnly = []string{
	"cluster.remove", "cluster.settings.setAgentWrites", "cluster.settings.setAccentColor", "cluster.settings.setLightStrip", "cluster.settings.uploadIcon", "dock.allowAgentRead",
	// H-125 and H-126 (the owner's decisions of 2026-10-19).
	"cluster.settings.resetIcon", "cluster.integrations.step", "cluster.integrations.lookFor", "cluster.integrations.answer",
	"cluster.integrations.ask",
	"kubeconfig.addFile", "kubeconfig.addFolder", "kubeconfig.approveExec", "kubeconfig.remove",
	"kubectl.download", engine.SecretCreateCommand, engine.RevealCommand, "configMap.keepEdits",
	engine.SecretReviewDataCommand, engine.SecretSaveDataCommand,
	"preferences.addProgramFolder", // D-120: a folder an agent added could hold the program a login runs
}

// guiOnlyParams returns parameters that decode for a command: every required property with a value
// of its type, a ref as a pod's reference.
func guiOnlyParams(cmd *engine.Command) json.RawMessage {
	params := map[string]any{}
	if cmd.Schema != nil {
		for _, name := range cmd.Schema.Required {
			params[name] = sampleValue(name, cmd.Schema.Properties[name])
		}
	}
	raw, _ := json.Marshal(params)
	return raw
}

func sampleValue(name string, s *jsonschema.Schema) any {
	if name == engine.ParamRef {
		return model.ObjectRef{Context: "test", Kind: "secrets", Namespace: "shop", Name: "web"}
	}
	if s != nil && len(s.Enum) > 0 {
		return s.Enum[0]
	}
	switch engine.SchemaType(s) {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	}
	return "x"
}

// TestGUIOnlyCommandsRefuseOtherOrigins: every command marked GUI-only refuses the CLI, external
// agents and the built-in agent with its reason, before anyone is asked and before it runs, even
// where agent writes are allowed; the refusal is in the audit log. (That the window's own call runs
// each command's own tests show.)
func TestGUIOnlyCommandsRefuseOtherOrigins(t *testing.T) {
	var asked atomic.Int32
	audit := &syncBuffer{}
	h := newHarness(t, seed(), engine.Options{
		Audit: audit, ClusterSettings: engine.NewClusterSettingsStore(""),
		Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) {
			asked.Add(1)
			return true, nil
		}),
	})
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	asked.Store(0) // turning the allowance on is the user's own

	var marked []string
	for _, cmd := range h.s.Registry().Commands() {
		if !cmd.GUIOnly {
			continue
		}
		marked = append(marked, cmd.ID)
		params := guiOnlyParams(cmd)
		for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
			_, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: cmd.ID, Params: params, Origin: origin})
			var ge *engine.GUIOnlyError
			switch {
			case !errors.As(err, &ge):
				t.Errorf("%s from %s: %v, want a GUI-only refusal", cmd.ID, origin, err)
				continue
			case ge.Command != cmd.ID || ge.Origin != origin || ge.Reason == nil:
				t.Errorf("%s from %s: refusal %+v", cmd.ID, origin, ge)
			case !errors.Is(err, ge.Reason):
				t.Errorf("%s from %s: the error does not unwrap to its reason", cmd.ID, origin)
			}
			if msg := err.Error(); !strings.Contains(msg, cmd.ID) || !strings.Contains(msg, "window") {
				t.Errorf("%s from %s: %q does not name the command and the window", cmd.ID, origin, msg)
			}
		}
	}
	for _, id := range wantGUIOnly {
		if !slices.Contains(marked, id) {
			t.Errorf("%s is not marked GUI-only", id)
		}
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("the user was asked %d times for refused commands", n)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Errorf("refused commands wrote: %v", calls)
	}
	refusals := 0
	for _, e := range audit.Lines() {
		if e.Origin.IsAgent() && slices.Contains(marked, e.Command) {
			if e.OK || !strings.Contains(e.Error, "window") {
				t.Errorf("audit line of a refused %s: %+v", e.Command, e)
			}
			refusals++
		}
	}
	if want := 2 * len(marked); refusals != want {
		t.Errorf("%d audit lines of refused agent calls, want %d", refusals, want)
	}
}
