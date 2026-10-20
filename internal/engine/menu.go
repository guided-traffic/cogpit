package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The parameters of a command that acts on one object, as row menus and the drawer fill them.
const (
	ParamRef       = "ref"       // the object (model.ObjectRef)
	ParamContainer = "container" // a pod's container, picked in a submenu
)

// Actions returns the commands of a kind's row menu (⋮ and right-click) and of its drawer's
// toolbar, in Lens's order: commands with an Order whose Kinds contain the kind, and those with an
// Order and no Kinds (Edit and Delete apply to every kind), sorted by Order, then by id. A menu
// shows each one unless [Command.Applicable] says it does not apply to the object (Cordon on a
// cordoned node), greyed when [Command.Enabled] gives a reason. The row menu leaves out the
// drawer-only ones (Command.DrawerOnly: Copy). The actions on a selection (Command.TakesSelection,
// selection.go) are a selection's menu's, not one object's.
func (r *Registry) Actions(kind string) []*Command {
	r.mu.RLock()
	out := make([]*Command, 0, 8)
	for _, c := range r.cmds {
		if c.Order > 0 && !c.TakesSelection() && (len(c.Kinds) == 0 || slices.Contains(c.Kinds, kind)) {
			out = append(out, c)
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// TakesContainer reports whether the command's parameters have a container: a menu offers it as
// a submenu of the pod's containers.
func (c *Command) TakesContainer() bool {
	return c.Schema != nil && c.Schema.Properties[ParamContainer] != nil
}

// DialogFields returns the parameters a dialog asks for before the command runs, in the order the
// parameter struct declares them: those beyond ref and container that the schema requires, and,
// for a command with a Prefill, the optional ones too (a Prefill means the command's values start
// from the cache in a dialog: Scale's replicas, optional in its schema so that agents may leave
// them to the dialog). A command with any needs a dialog; it is generated from each field's schema.
func (c *Command) DialogFields() []string {
	if c.Schema == nil {
		return nil
	}
	var out []string
	for _, name := range propertyOrder(c.Schema) {
		if name == ParamRef || name == ParamContainer {
			continue
		}
		if c.prefill != nil || slices.Contains(c.Schema.Required, name) {
			out = append(out, name)
		}
	}
	return out
}

// ObjectParams returns parameters for the command on one object: the ref, the container when the
// command takes one and container is set, and a zero value for every other required property
// (0, "", false, the minimum of a bounded number, the first value of an enum). With them, Enabled,
// Applicable and Prefill can be asked before a dialog has collected the real values. Optional
// properties stay unset, so that Prefill fills them from the cache.
func (c *Command) ObjectParams(ref model.ObjectRef, container string) json.RawMessage {
	params := map[string]any{ParamRef: ref}
	if container != "" && c.TakesContainer() {
		params[ParamContainer] = container
	}
	for _, name := range c.DialogFields() {
		if c.Requires(name) {
			params[name] = zeroValue(c.Schema.Properties[name])
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// propertyOrder returns a schema's property names in declaration order, then any others sorted.
func propertyOrder(s *jsonschema.Schema) []string {
	out := make([]string, 0, len(s.Properties))
	for _, name := range s.PropertyOrder {
		if _, ok := s.Properties[name]; ok && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	rest := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		if !slices.Contains(out, name) {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// DialogField is how the window's generated dialog shows one parameter beyond what its schema
// says (Spec.Dialog; the owner's gate walk of 2026-10-20, GW2 and GW6, D-104). The schema's
// description is written for agents ("required to run; a dialog starts from the current count");
// Hint says the same to a person, in the tooltip of the "?" after the field's name. A whole number
// whose schema allows far more than anyone picks (Scale's 0 to 65535) gets a slider over the range
// most values fall in, SliderMin to SliderMax, and the number field beside it for any other value
// the schema allows.
type DialogField struct {
	// Hint is the field's tooltip; empty shows the schema's description.
	Hint string
	// SliderMin and SliderMax are the slider's range; both zero: no slider. Only an integer
	// field has one, inside the schema's bounds, SliderMin below SliderMax.
	SliderMin, SliderMax int
}

// HasSlider reports whether the field gets a slider.
func (f DialogField) HasSlider() bool { return f.SliderMin != 0 || f.SliderMax != 0 }

// DialogField returns how the generated dialog shows the parameter name (Spec.Dialog); the zero
// value when the command says nothing about it.
func (c *Command) DialogField(name string) DialogField { return c.dialog[name] }

// DialogHint is the text of the "?" after a dialog field's name: the command's hint for a person
// (DialogField.Hint), else the schema's description; "" when neither says anything.
func (c *Command) DialogHint(name string) string {
	if h := c.dialog[name].Hint; h != "" {
		return h
	}
	if c.Schema == nil || c.Schema.Properties[name] == nil {
		return ""
	}
	return c.Schema.Properties[name].Description
}

// checkDialogFields refuses a Spec.Dialog entry that names no parameter of the schema, and a
// slider on anything but an integer, outside the schema's bounds, or with no room between its ends.
func checkDialogFields(schema *jsonschema.Schema, fields map[string]DialogField) error {
	for name, f := range fields {
		prop := schema.Properties[name]
		if prop == nil {
			return fmt.Errorf("dialog field %q is not a parameter", name)
		}
		if !f.HasSlider() {
			continue
		}
		if SchemaType(prop) != "integer" {
			return fmt.Errorf("dialog field %q: a slider needs an integer", name)
		}
		lo, hi := float64(f.SliderMin), float64(f.SliderMax)
		if lo >= hi || (prop.Minimum != nil && lo < *prop.Minimum) || (prop.Maximum != nil && hi > *prop.Maximum) {
			return fmt.Errorf("dialog field %q: slider %d to %d is not inside the schema's bounds", name, f.SliderMin, f.SliderMax)
		}
	}
	return nil
}

// Requires reports whether the command's schema requires a parameter.
func (c *Command) Requires(name string) bool {
	return c.Schema != nil && slices.Contains(c.Schema.Required, name)
}

// SchemaType returns a property schema's JSON type: its Type, or the first of its Types that is
// not "null".
func SchemaType(s *jsonschema.Schema) string {
	if s == nil {
		return ""
	}
	if s.Type != "" {
		return s.Type
	}
	for _, t := range s.Types {
		if t != "null" {
			return t
		}
	}
	return ""
}

// zeroValue is a value that validates against a simple property schema.
func zeroValue(s *jsonschema.Schema) any {
	if s == nil {
		return nil
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}
	switch SchemaType(s) {
	case "integer", "number":
		if s.Minimum != nil && *s.Minimum > 0 {
			return *s.Minimum
		}
		return 0
	case "string":
		return ""
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	}
	return nil
}
