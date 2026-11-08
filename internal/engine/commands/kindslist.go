package commands

import (
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// kinds.list (G17; resources.go registers it): every kind a connected cluster serves as Cogpit
// shows it, with what an agent can do with its objects: the tools of an object's row menu and
// drawer, of a selection's menu and the kind's other tools (its create dialog), whether Cogpit's
// cache holds it now, and, with all, the built-in kinds the cluster does not serve.

type kindsListParams struct {
	engine.OnCluster
	All bool `json:"all,omitempty" jsonschema:"also list the built-in kinds the cluster does not serve, each with notServed"`
}

// listKinds is kinds.list's result for a connected backend.
func listKinds(s *engine.Session, b *engine.Backend, p kindsListParams) []KindInfo {
	all := b.AllProfiles()
	var filter []string
	if u := s.UIState(); u.Context == b.Context { // the window's filter, of the shown cluster only (D-69)
		filter = u.Namespaces
	}
	reg := s.Registry()
	cmds := reg.Commands()
	out := make([]KindInfo, 0, len(all))
	served := make(map[string]bool, len(all))
	for _, prof := range all {
		served[prof.Key] = true
		info := kindInfo(prof)
		names := b.KindNames(prof)
		info.Singular, info.ShortNames = names.Singular, names.ShortNames
		if listable, known := b.Listable(prof, filter); known {
			info.Listable = &listable
		}
		info.Cached = b.Cached(prof)
		info.Actions = toolNames(reg.Actions(prof.Key))
		info.SelectionActions = toolNames(reg.SelectionActions([]string{prof.Key}))
		info.Commands = kindCommands(cmds, prof.Key, info.Actions, info.SelectionActions)
		out = append(out, info)
	}
	if !p.All {
		return out
	}
	for _, prof := range kinds.Builtin() {
		if !served[prof.Key] {
			info := kindInfo(prof)
			info.NotServed = true
			out = append(out, info)
		}
	}
	return out
}

// kindInfo is a profile as kinds.list describes it, without what depends on the cluster.
func kindInfo(p *model.KindProfile) KindInfo {
	return KindInfo{
		Kind: p.Key, Title: p.Title, APIKind: p.Kind, Group: p.Group, Version: p.Version,
		Namespaced: p.Namespaced, Columns: p.Columns, Fields: fieldNames(p),
		Local: p.Local, Virtual: p.Derive != nil,
	}
}

// toolNames are the commands' MCP tool names, in their order.
func toolNames(cmds []*engine.Command) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.ToolName())
	}
	return out
}

// kindCommands are the tools that name the kind and are none of its actions: its create dialog,
// the kind's own reads and pages.
func kindCommands(cmds []*engine.Command, kind string, actions, selection []string) []string {
	var out []string
	for _, c := range cmds {
		name := c.ToolName()
		if slices.Contains(c.Kinds, kind) && !slices.Contains(actions, name) && !slices.Contains(selection, name) {
			out = append(out, name)
		}
	}
	return out
}
