package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

type dialogProbe struct {
	Ref      model.ObjectRef `json:"ref"`
	Replicas *uint16         `json:"replicas,omitempty" jsonschema:"for agents: required to run"`
	Grace    uint16          `json:"grace,omitempty" jsonschema:"seconds"`
	Name     string          `json:"name,omitempty" jsonschema:"a name"`
	Quiet    bool            `json:"quiet,omitempty"`
}

func registerDialogProbe(r *Registry, dialog map[string]DialogField) error {
	return Register(r, Spec[dialogProbe]{ID: "probe.dialog", Label: "Probe", Danger: DangerRead, Dialog: dialog,
		Run: func(context.Context, *Session, dialogProbe) (any, error) { return nil, nil }})
}

// TestDialogFieldHintsAndSliders: a command's dialog hints are a person's words where it gives
// them and the schema's description where it does not; a slider is the command's, not the schema's
// (the schema says 0 to 65535, the dialog 1 to 30; GW2, D-104).
func TestDialogFieldHintsAndSliders(t *testing.T) {
	r := NewRegistry()
	if err := registerDialogProbe(r, map[string]DialogField{
		"replicas": {Hint: "The number of pods.", SliderMin: 1, SliderMax: 30},
	}); err != nil {
		t.Fatal(err)
	}
	c, _ := r.Get("probe.dialog")
	if got := c.DialogHint("replicas"); got != "The number of pods." {
		t.Errorf("replicas hint %q", got)
	}
	if got := c.DialogHint("grace"); got != "seconds" {
		t.Errorf("grace hint %q, want the schema's description", got)
	}
	if got := c.DialogHint("quiet") + c.DialogHint("nothing"); got != "" {
		t.Errorf("hints of a field without a description %q", got)
	}
	if f := c.DialogField("replicas"); !f.HasSlider() || f.SliderMin != 1 || f.SliderMax != 30 {
		t.Errorf("replicas field %+v", f)
	}
	if c.DialogField("grace").HasSlider() {
		t.Error("grace has a slider nobody asked for")
	}
}

// TestDialogFieldsAreChecked: Register refuses a dialog field that names no parameter, a slider on
// something else than a whole number, and a slider outside the schema's bounds or without room.
func TestDialogFieldsAreChecked(t *testing.T) {
	for name, tt := range map[string]struct {
		fields map[string]DialogField
		want   string
	}{
		"unknown":     {map[string]DialogField{"replica": {Hint: "x"}}, "not a parameter"},
		"string":      {map[string]DialogField{"name": {SliderMin: 1, SliderMax: 3}}, "needs an integer"},
		"boolean":     {map[string]DialogField{"quiet": {SliderMin: 0, SliderMax: 1}}, "needs an integer"},
		"too high":    {map[string]DialogField{"grace": {SliderMin: 1, SliderMax: 70000}}, "inside the schema's bounds"},
		"below zero":  {map[string]DialogField{"grace": {SliderMin: -1, SliderMax: 30}}, "inside the schema's bounds"},
		"no room":     {map[string]DialogField{"replicas": {SliderMin: 5, SliderMax: 5}}, "inside the schema's bounds"},
		"upside down": {map[string]DialogField{"replicas": {SliderMin: 30, SliderMax: 1}}, "inside the schema's bounds"},
	} {
		err := registerDialogProbe(NewRegistry(), tt.fields)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", name, err, tt.want)
		}
	}
}
