package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The command palette (G13, ⌘⇧P; ui-parity › Command palette): a searchable list of what the
// window's menus offer in the current UI state. Registry.Palette builds it from the registry's
// metadata, so a command appears in it without palette code; which commands, and with which
// parameters (decided, Cogpit's reading, Lens's palette is not captured beyond its "Switch to" and
// hotbar entries):
//
//  1. Object actions. The palette's object is the drawer's, else the one selected object
//     (PaletteObject). Its kind's actions are offered as the drawer's toolbar lists them
//     (Registry.Actions: the commands with an Order, Copy included), without those that do not
//     apply to it (ErrNotApplicable); a command that takes a container gets an entry per
//     container when the pod has more than one. Without an object there are none.
//  2. Every other command needs a menu path. One without is a tool for agents (ui.state,
//     kinds.list, resources.list, editor.get, object.get, ...), and the palette's own commands
//     are left out.
//  3. A read without an Order returns data to its caller, which an agent reads and the window
//     shows in its views: it is offered only when the caller presents its result
//     (PaletteInput.Runs: the CSV download, the kubeconfig sources); hotbar.list and
//     workloads.overview are not ("Switch to Overview" opens the page).
//  4. A command that takes an object (a ref) but has no Order is left out: its object is picked
//     elsewhere (a container port in the drawer for pod.portForward, a search for ui.navigate).
//  5. A kind's command without a ref (the create commands) is offered while the open view is of
//     one of its kinds, as the view's "+" is.
//  6. Parameters the palette fills itself: ui.openView gets an entry per view the navigator
//     offers but the open one ("Switch to Pods"), hotbar.openCluster one per tile of the hotbar,
//     with its F-key for the first twelve.
//  7. Other parameters: a command with parameters is offered when the caller asks for them in a
//     dialog of its own (PaletteInput.Runs: the navigator's section for cluster.connect, the
//     create dialogs, the search field for ui.setFilter); else only when none is required, and it
//     runs with the defaults its schema describes (hotbar.addCluster pins the connected cluster).
//     One with a required parameter and no dialog is left
//     out (hotbar.moveCluster, object.apply, object.create, kubeconfig.remove without the
//     sources dialog).
//
// A command that cannot run now stays in the list with the reason Enabled gives (greyed by the
// window, as menus grey it); running an entry dispatches its command like a menu item, so the
// confirmation (D-2), the access reviews and the audit log apply unchanged.

// The palette's own commands (commands/palette.go) and the commands whose parameters the palette
// fills itself.
const (
	PaletteOpenCommand  = "ui.palette.open"
	PaletteCloseCommand = "ui.palette.close"
	openViewCommand     = "ui.openView"
	openClusterCommand  = "hotbar.openCluster"
)

// PaletteEntry is one row of the command palette: a command and the parameters it runs with.
type PaletteEntry struct {
	Command string `json:"command"`
	// Label is what the row says: the command's label, "Switch to Pods", "Logs · sidecar".
	Label string `json:"label"`
	// Category places the row: the menu path's first part ("View", "Hotbar", "Cluster"), the
	// navigator's group of a view ("Workloads"), or the name of the object an action acts on.
	Category string `json:"category,omitempty"`
	// Shortcut is the registry's shortcut, or a hotbar tile's F-key.
	Shortcut string          `json:"shortcut,omitempty"`
	Danger   Danger          `json:"danger"`
	Params   json.RawMessage `json:"params"`
	// Ref and Container are an object action's object and container (rule 1).
	Ref       *model.ObjectRef `json:"ref,omitempty"`
	Container string           `json:"container,omitempty"`
	// Own: the caller runs the command its own way (PaletteInput.Runs), asking for its parameters
	// first; Params then holds none of them.
	Own bool `json:"own,omitempty"`
	// Reason says why the command cannot run now; the row is greyed. Empty when it can.
	Reason string `json:"reason,omitempty"`
}

// PaletteView is a view the navigator offers, for a "Switch to" entry.
type PaletteView struct {
	View  string `json:"view"`            // the kind or page key ui.openView takes
	Title string `json:"title"`           // "Pods"
	Group string `json:"group,omitempty"` // the navigator's group: "Workloads", "Network › Gateway API"
}

// PaletteInput is what the palette depends on besides the registry.
type PaletteInput struct {
	State UIState
	// Views are the views the navigator offers, in its order.
	Views []PaletteView
	// Containers are the palette object's containers and init containers (a pod's), for the
	// commands that take one.
	Containers []string
	// Runs reports the commands the caller runs its own way: with a dialog for their parameters,
	// or presenting a read's result (rules 3 and 7). Nil runs none.
	Runs func(id string) bool
}

// PaletteObject is the object the palette's actions act on, as a menu's: the drawer's object,
// else the one selected object. False when there is none, or several are selected.
func PaletteObject(st UIState) (model.ObjectRef, bool) {
	if st.Drawer != nil {
		return *st.Drawer, true
	}
	if len(st.Selected) == 1 {
		return st.Selected[0], true
	}
	return model.ObjectRef{}, false
}

// Palette returns the palette's entries for the input's state, by the rules above: the object's
// actions first, then the open view's kind commands, the other commands by menu path, the
// hotbar's tiles, and the views to switch to. It evaluates every entry's Enabled, which
// reads the cache: call it off a UI thread. s may be nil when the commands' Enabled do not use it
// (test registries).
func (r *Registry) Palette(s *Session, in PaletteInput) []PaletteEntry {
	runs := func(id string) bool { return in.Runs != nil && in.Runs(id) }
	var objects, kinds, plain, tiles, views []PaletteEntry
	if ref, ok := PaletteObject(in.State); ok {
		objects = r.objectEntries(s, ref, in.Containers)
	}
	for _, c := range r.Commands() {
		if c.Order > 0 || c.MenuPath == "" || c.ID == PaletteOpenCommand || c.ID == PaletteCloseCommand || c.takesRef() {
			continue // rules 1, 2 and 4
		}
		own := runs(c.ID)
		if c.Danger == DangerRead && !own {
			continue // rule 3
		}
		switch c.ID { // rule 6
		case openViewCommand:
			views = append(views, r.viewEntries(s, c, in)...)
			continue
		case openClusterCommand:
			tiles = append(tiles, tileEntries(s, c, in.State.Hotbar, in.State.ClusterSettings)...)
			continue
		}
		if len(c.Kinds) > 0 && !slices.Contains(c.Kinds, in.State.View) {
			continue // rule 5
		}
		if c.hasParams() && !own && len(c.Schema.Required) > 0 {
			continue // rule 7
		}
		e, ok := paletteEntry(s, c, nil)
		if !ok {
			continue
		}
		e.Own = own
		if len(c.Kinds) > 0 {
			kinds = append(kinds, e)
		} else {
			plain = append(plain, e)
		}
	}
	sort.SliceStable(plain, func(i, j int) bool {
		a, _ := r.Get(plain[i].Command)
		b, _ := r.Get(plain[j].Command)
		return a.MenuPath < b.MenuPath
	})
	out := make([]PaletteEntry, 0, len(objects)+len(kinds)+len(plain)+len(tiles)+len(views))
	for _, part := range [][]PaletteEntry{objects, kinds, plain, tiles, views} {
		out = append(out, part...)
	}
	return out
}

// objectEntries are the object's actions (rule 1).
func (r *Registry) objectEntries(s *Session, ref model.ObjectRef, containers []string) []PaletteEntry {
	var out []PaletteEntry
	for _, c := range r.Actions(ref.Kind) {
		each := []string{""}
		if c.TakesContainer() && len(containers) > 1 {
			each = containers
		}
		for _, container := range each {
			params := c.ObjectParams(ref, container)
			reason, ok := paletteReason(s, c, params)
			if !ok {
				break // does not apply to the object
			}
			label := c.Label
			if container != "" {
				label += " · " + container
			}
			r := ref
			out = append(out, PaletteEntry{Command: c.ID, Label: label, Category: ref.Name, Shortcut: c.Shortcut,
				Danger: c.Danger, Params: params, Ref: &r, Container: container, Reason: reason})
		}
	}
	return out
}

// viewEntries are "Switch to" a view, one per view the navigator offers but the open one.
func (r *Registry) viewEntries(s *Session, c *Command, in PaletteInput) []PaletteEntry {
	out := make([]PaletteEntry, 0, len(in.Views))
	seen := map[string]bool{in.State.View: true}
	for _, v := range in.Views {
		if v.View == "" || seen[v.View] {
			continue // a kind the navigator lists twice (Custom Resources) is one entry
		}
		seen[v.View] = true
		e, ok := paletteEntry(s, c, map[string]any{"view": v.View})
		if !ok {
			continue
		}
		e.Label, e.Category = "Switch to "+v.Title, v.Group
		if e.Category == "" {
			e.Category = "View"
		}
		out = append(out, e)
	}
	return out
}

// tileEntries open the hotbar's clusters, the first twelve with their keys F1–F12.
func tileEntries(s *Session, c *Command, h *HotbarState, settings map[string]ClusterSettings) []PaletteEntry {
	if h == nil {
		return nil
	}
	var out []PaletteEntry
	for i, t := range h.Clusters {
		slot := i + 1
		e, ok := paletteEntry(s, c, map[string]any{"slot": slot})
		if !ok {
			continue
		}
		// The cluster by the name it shows everywhere (D-68); its full context name at the row's end,
		// where the palette's search finds it too (the palette's rows have no tooltip).
		e.Label, e.Shortcut = "Open "+ClusterName(t.Context, settings[t.Context]), fKey(slot)
		e.Category = ContextLabel(t.Context)
		out = append(out, e)
	}
	return out
}

// fKey is the key Fn of a hotbar tile: F1 to F12 for the first twelve, none for the others.
func fKey(n int) string {
	if n < 1 || n > HotbarKeys {
		return ""
	}
	return fmt.Sprintf("F%d", n)
}

// paletteEntry is a command's entry with the given parameters (nil: none), its required
// parameters stubbed so that Enabled can be asked; false when the command does not apply.
func paletteEntry(s *Session, c *Command, params map[string]any) (PaletteEntry, bool) {
	if params == nil {
		params = map[string]any{}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return PaletteEntry{}, false
	}
	reason, ok := paletteReason(s, c, c.stubRequired(params))
	if !ok {
		return PaletteEntry{}, false
	}
	category, _, _ := strings.Cut(c.MenuPath, "/")
	return PaletteEntry{Command: c.ID, Label: c.Label, Category: category, Shortcut: c.Shortcut, Danger: c.Danger,
		Params: raw, Reason: reason}, true
}

// paletteReason asks a command's Enabled: the reason it cannot run, or "" when it can; false when
// it does not apply at all. Parameters that do not satisfy the schema (a stub for a dialog's
// fields) cannot be judged yet, so they count as enabled, as in the menus; a panic counts as a
// reason.
func paletteReason(s *Session, c *Command, params json.RawMessage) (reason string, applies bool) {
	defer func() {
		if p := recover(); p != nil {
			reason, applies = fmt.Sprintf("unavailable: %v", p), true
		}
	}()
	err := c.Enabled(s, params)
	var perr *ParamsError
	switch {
	case err == nil, errors.As(err, &perr):
		return "", true
	case errors.Is(err, ErrNotApplicable):
		return "", false
	}
	return err.Error(), true
}

// takesRef reports whether the command's parameters name an object.
func (c *Command) takesRef() bool { return c.Schema != nil && c.Schema.Properties[ParamRef] != nil }

// hasParams reports whether the command has parameters at all.
func (c *Command) hasParams() bool { return c.Schema != nil && len(c.Schema.Properties) > 0 }

// stubRequired adds a zero value for every required parameter params lacks.
func (c *Command) stubRequired(params map[string]any) json.RawMessage {
	full := make(map[string]any, len(params))
	for k, v := range params {
		full[k] = v
	}
	if c.Schema != nil {
		for _, name := range c.Schema.Required {
			if _, ok := full[name]; !ok {
				full[name] = zeroValue(c.Schema.Properties[name])
			}
		}
	}
	raw, err := json.Marshal(full)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// FilterPalette returns the entries that match the query, best first: every whitespace-separated
// term must occur in the entry's label or category, case-insensitively, or its letters in order in
// the label from the start of a word (fuzzy: "swdep" finds "Switch to Deployments"). A term at the start of the label ranks first,
// then at the start of a word, anywhere in the label, in the category, and fuzzy last; among
// equals, entries that can run come before greyed ones and the palette's order holds. An empty
// query returns the entries as they are (inferred: Lens's palette is not captured).
func FilterPalette(entries []PaletteEntry, query string) []PaletteEntry {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries
	}
	type scored struct {
		e     PaletteEntry
		score int
	}
	var hits []scored
	for _, e := range entries {
		label, category := strings.ToLower(e.Label), strings.ToLower(e.Category)
		total := 0
		for _, t := range terms {
			s := termScore(label, category, t)
			if s == 0 {
				total = 0
				break
			}
			total += s
		}
		if total > 0 {
			hits = append(hits, scored{e, total})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].e.Reason == "" && hits[j].e.Reason != ""
	})
	out := make([]PaletteEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

// termScore rates how one lower-case term matches an entry's lower-case label and category; 0
// when it does not.
func termScore(label, category, t string) int {
	switch {
	case strings.HasPrefix(label, t):
		return 5
	case strings.Contains(" "+label, " "+t):
		return 4
	case strings.Contains(label, t):
		return 3
	case strings.Contains(category, t):
		return 2
	case fuzzy(label, t):
		return 1
	}
	return 0
}

// fuzzy reports whether t's letters occur in the label in order, the first at the start of a word:
// "swdep" in "switch to deployments", not "term" in "switch to deployment controllers".
func fuzzy(label, t string) bool {
	for i := 0; i < len(label); i++ {
		if (i == 0 || label[i-1] == ' ') && strings.HasPrefix(label[i:], t[:1]) && subsequence(label[i:], t) {
			return true
		}
	}
	return false
}

// subsequence reports whether t's letters occur in s in order.
func subsequence(s, t string) bool {
	rs := []rune(s)
	i := 0
	for _, r := range t {
		for i < len(rs) && rs[i] != r {
			i++
		}
		if i == len(rs) {
			return false
		}
		i++
	}
	return true
}
