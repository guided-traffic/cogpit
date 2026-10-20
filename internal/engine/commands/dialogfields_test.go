package commands_test

import (
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestDialogHintsSpeakToAPerson: the generated dialogs of Scale and Drain carry a hint for every
// field, in a person's words: no kubectl flag, no "required to run" meant for agents, no
// "default" (the owner's GW6, D-104). Scale's replicas get the slider from 1 to 30 (GW2).
func TestDialogHintsSpeakToAPerson(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"deployment.scale", "statefulset.scale", "replicaset.scale", "node.drain"} {
		c, ok := r.Get(id)
		if !ok {
			t.Fatalf("no %s", id)
		}
		for _, name := range c.DialogFields() {
			hint := c.DialogHint(name)
			if hint == "" || hint == c.Schema.Properties[name].Description {
				t.Errorf("%s %s: hint %q is the schema's", id, name, hint)
			}
			for _, agentish := range []string{"--", "required to run", "default", "kubectl"} {
				if strings.Contains(hint, agentish) {
					t.Errorf("%s %s: hint %q says %q", id, name, hint, agentish)
				}
			}
		}
	}
	for _, id := range []string{"deployment.scale", "statefulset.scale", "replicaset.scale"} {
		c, _ := r.Get(id)
		if f := c.DialogField("replicas"); f.SliderMin != 1 || f.SliderMax != 30 {
			t.Errorf("%s replicas %+v, want a slider from 1 to 30", id, f)
		}
	}
	drain, _ := r.Get("node.drain")
	for _, name := range drain.DialogFields() {
		if drain.DialogField(name).HasSlider() {
			t.Errorf("drain %s has a slider", name)
		}
	}
}
