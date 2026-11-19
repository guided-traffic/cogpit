package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Selections (ui-parity › Tables, G5, G7, G9). UIState.Selected is the open view's selection, the
// checked rows, which ui.select sets for the user (the row checkboxes, the header's checkbox, ⌘- and
// ⇧-click) and for agents alike, so both see and drive the same selection; each view keeps its own,
// remembered by the tab while another view is shown (selectionmemory.go). An action on a selection
// is a command whose parameters take the objects in a required "refs" array (ParamRefs) and that
// has an Order: object.deleteMany. Registry.SelectionActions lists them for a selection's menu, and
// Registry.Actions (the menus of one object) leaves them out.
//
// The owner's decision (A73): a plain click on a row opens its drawer and keeps the checked
// rows checked. The click is ui.select's mode open (SelectOpen): the drawer moves to the row's
// object and the selection stays as it is, the clicked row neither checked nor unchecked. Cogpit's
// defaults (inferred, not the owner's decision): a checkbox, ⌘-click and ⇧-click change the
// selection without opening the drawer; the header's checkbox selects the rows the view shows (the
// namespace filter and the search applied) or clears the selection; an action takes at most
// MaxSelection objects at once.

// ParamRefs is the parameter of a command that acts on a selection: the objects (model.ObjectRef).
const ParamRefs = "refs"

// MaxSelection is the most objects one action on a selection takes (Cogpit's bound, inferred;
// the owner confirmed it for Delete, A72): 500 deletes take about 100 seconds at the cluster
// connection's 5 requests per second. Since G4 a selection of more than DirectSelection objects
// runs as a background job whose dock tab shows its progress and can be cancelled; the bound stays,
// the confirmation listing every object (the lead's decision for the owner to confirm).
const MaxSelection = 500

// DirectSelection is the most objects an action on a selection acts on within its call, returning
// what it did as one object's action does; a larger selection runs as a background job (jobs.go)
// and the command returns at once with the job's tab (G4; the lead's decision for the owner to
// confirm). Ten is the cluster connection's burst of requests (cluster.DefaultBurst): up to ten go
// out at once and end within about a second on a healthy connection, a toast saying how they went;
// beyond it the connection's 5 requests per second spread them over seconds to minutes, which the
// job's tab shows step by step.
const DirectSelection = 10

// SelectMode says how ui.select changes the selection.
type SelectMode string

// Selection modes.
const (
	SelectReplace SelectMode = "replace" // the objects become the selection (the default)
	SelectAdd     SelectMode = "add"     // the objects join the selection
	SelectToggle  SelectMode = "toggle"  // each object joins, or leaves when it is selected
	SelectRemove  SelectMode = "remove"  // the objects leave the selection
	SelectOpen    SelectMode = "open"    // the selection stays; the drawer opens on the one object (a plain click, A73)
)

// ParseSelectMode reads a mode as ui.select takes it; "" is SelectReplace.
func ParseSelectMode(s string) (SelectMode, error) {
	switch m := SelectMode(s); m {
	case "":
		return SelectReplace, nil
	case SelectReplace, SelectAdd, SelectToggle, SelectRemove, SelectOpen:
		return m, nil
	}
	return "", fmt.Errorf("unknown selection mode %q: want replace, add, toggle, remove or open", s)
}

// selKey identifies an object within one cluster: a selection holds the objects of the connected
// cluster only, and a reference without a context means that one (model.ObjectRef.Same).
func selKey(r model.ObjectRef) string { return r.Kind + "\x00" + r.Namespace + "\x00" + r.Name }

// ApplySelection returns the selection after mode applied refs to cur. The result keeps cur's
// order, then the objects that joined in refs' order, and holds no object twice; SelectOpen keeps
// cur as it is (the drawer is ui.select's). It allocates a new slice; cur and refs are not changed.
func ApplySelection(cur, refs []model.ObjectRef, mode SelectMode) []model.ObjectRef {
	if mode == SelectOpen {
		return slices.Clone(cur)
	}
	in := make(map[string]bool, len(refs))
	for _, r := range refs {
		in[selKey(r)] = true
	}
	out := make([]model.ObjectRef, 0, len(cur)+len(refs))
	have := make(map[string]bool, len(cur)+len(refs))
	keep := func(r model.ObjectRef) {
		if k := selKey(r); !have[k] {
			have[k] = true
			out = append(out, r)
		}
	}
	switch mode {
	case SelectAdd, SelectToggle, SelectRemove:
		for _, r := range cur {
			if (mode == SelectToggle || mode == SelectRemove) && in[selKey(r)] {
				have[selKey(r)] = true // toggled off: refs must not add it back
				continue
			}
			keep(r)
		}
		if mode == SelectRemove {
			return out
		}
		was := make(map[string]bool, len(cur))
		for _, r := range cur {
			was[selKey(r)] = true
		}
		for _, r := range refs {
			if mode == SelectToggle && was[selKey(r)] {
				continue
			}
			keep(r)
		}
	default: // SelectReplace
		for _, r := range refs {
			keep(r)
		}
	}
	return out
}

// SelectionIncludes reports whether ref is in the selection.
func SelectionIncludes(sel []model.ObjectRef, ref model.ObjectRef) bool {
	return slices.ContainsFunc(sel, ref.Same)
}

// TakesSelection reports whether the command acts on a selection: its parameters require the
// objects in "refs".
func (c *Command) TakesSelection() bool {
	return c.Schema != nil && c.Schema.Properties[ParamRefs] != nil && c.Requires(ParamRefs)
}

// SelectionActions returns the commands a selection's menu offers for objects of the given kinds:
// the commands with an Order that take a selection and apply to every one of the kinds (no Kinds:
// every kind), sorted by Order, then by id. A menu still asks Enabled with SelectionParams, which
// says why a command cannot act on these objects now.
func (r *Registry) SelectionActions(kinds []string) []*Command {
	r.mu.RLock()
	var out []*Command
	for _, c := range r.cmds {
		if c.Order == 0 || !c.TakesSelection() {
			continue
		}
		all := true
		for _, k := range kinds {
			all = all && (len(c.Kinds) == 0 || slices.Contains(c.Kinds, k))
		}
		if all {
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

// SelectionParams returns the parameters of an action on a selection.
func SelectionParams(refs []model.ObjectRef) json.RawMessage {
	if refs == nil {
		refs = []model.ObjectRef{}
	}
	raw, err := json.Marshal(map[string]any{ParamRefs: refs})
	if err != nil {
		return json.RawMessage(`{"refs":[]}`)
	}
	return raw
}

// SelectionKinds returns the kinds of a selection's objects, each once, in their first order.
func SelectionKinds(refs []model.ObjectRef) []string {
	var out []string
	for _, r := range refs {
		if !slices.Contains(out, r.Kind) {
			out = append(out, r.Kind)
		}
	}
	return out
}
