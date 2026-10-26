package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The chat's tools (D-10; the lead's choice for the owner to confirm). The registry's every
// command as a tool, as the MCP server offers them, measured 2026-10-14: 183 tools, 219,945 bytes
// of tool definitions (169 without the excluded ones, 202,445 bytes), some 50,000 tokens at four
// bytes a token with every request: more than a local 27B model's whole context holds in many
// set-ups. The core set below is 22 tools, 29,443 bytes, about 7,400 tokens
// (TestToolsetSize). So each request carries a core set of native tools, the reads and UI
// commands diagnosis and navigation need and the drawer's most common writes, and two meta-tools:
// search_commands finds any other command by words and returns its id, label, danger and parameter
// schema; run_command runs any command by id with parameters, through the same Dispatch and the
// same rules. Both kinds dispatch with origin builtInAgent.
//
// A connection the user set to send all tools (ChatConnection.AllTools, the owner's decision of
// 2026-10-15, for large cloud models) gets every command the chat may run as a native tool instead:
// the core ones first, in their order, then the rest in the registry's, and no meta-tools (nothing
// is left to find). Measured 2026-10-26 (TestToolsetSize): 197 tools, 280,080 bytes, about 70,000
// tokens with every request; the core set 22 tools, 34,508 bytes, about 8,600 tokens. The tools
// resolve and dispatch alike either way, so the rules are the same.
//
// Left out of both: GUI-only commands (Dispatch refuses them to every agent anyway, so offering
// them costs tokens and teaches nothing the refusal does not) and chat.toggle (the chat closing
// itself).

// coreCommands are the registry commands offered as native tools, in this order.
var coreCommands = []string{
	// What the user sees, and reading the cluster from Cogpit's cache.
	"ui.state", "kinds.list", "resources.find", "resources.list", "object.get", "view.rows",
	"dock.read", "pod.logs", "workload.logs", "cluster.overview", "metrics.get",
	// Showing the user something.
	"ui.navigate", "ui.openView", "ui.setNamespace", "ui.setFilter", "ui.select", "ui.closeDrawer",
	// The drawer's most asked-for writes; each waits for the user (D-2).
	"deployment.scale", "deployment.restart", "object.delete",
}

// Meta-tool names. Registry tool names are "<area>_<action>" from ids with a dot, so these two
// cannot collide with one (newToolset checks).
const (
	toolSearch = "search_commands"
	toolRun    = "run_command"
)

const searchSchema = `{"type":"object","properties":{` +
	`"query":{"type":"string","description":"words to look for in the commands' ids, labels and descriptions, e.g. \"scale statefulset\", \"helm rollback\", \"port forward\"; empty lists every command's id and label"},` +
	`"limit":{"type":"integer","minimum":1,"maximum":20,"description":"at most this many commands with their schemas (default 8)"}},` +
	`"additionalProperties":false}`

const runSchema = `{"type":"object","properties":{` +
	`"command":{"type":"string","description":"the command's id as search_commands returns it, e.g. \"statefulset.scale\""},` +
	`"params":{"type":"object","description":"the command's parameters, matching the parameters schema search_commands returned"}},` +
	`"required":["command"],"additionalProperties":false}`

// chatExcluded reports whether a command is kept from the chat's tools.
func chatExcluded(c *engine.Command) bool {
	return c.GUIOnly || c.ID == "chat.toggle"
}

// toolset is the tools of every request, built once from the registry.
type toolset struct {
	defs  []toolDef                  // the core set and the meta-tools
	bytes int                        // the definitions' JSON size
	core  map[string]*engine.Command // native tool name → command
	all   []*engine.Command          // every command the chat may run, by id
	byID  map[string]*engine.Command
	byTN  map[string]*engine.Command // tool name → command
	// every is every command the chat may run as a tool (a connection's AllTools); full its JSON
	// size and fullN its count.
	every []toolDef
	full  int
	fullN int
}

// defsFor returns the tools a request carries: every command for a connection that sends all
// tools, else the core set and the meta-tools.
func (t *toolset) defsFor(all bool) []toolDef {
	if all {
		return t.every
	}
	return t.defs
}

// newToolset builds the tools from the registry.
func newToolset(reg *engine.Registry) (*toolset, error) {
	t := &toolset{core: map[string]*engine.Command{}, byID: map[string]*engine.Command{}, byTN: map[string]*engine.Command{}}
	var rest []toolDef
	for _, c := range reg.Commands() {
		if chatExcluded(c) {
			continue
		}
		t.all = append(t.all, c)
		t.byID[c.ID] = c
		t.byTN[c.ToolName()] = c
		if def, err := commandTool(c); err == nil && !slices.Contains(coreCommands, c.ID) {
			rest = append(rest, def)
		}
	}
	if t.byTN[toolSearch] != nil || t.byTN[toolRun] != nil {
		return nil, errors.New("a registry command has a meta-tool's name")
	}
	for _, id := range coreCommands {
		c := t.byID[id]
		if c == nil {
			continue // a registry without it (a test's): the meta-tools still reach the rest
		}
		def, err := commandTool(c)
		if err != nil {
			return nil, err
		}
		t.defs = append(t.defs, def)
		t.core[def.Function.Name] = c
	}
	t.every = append(slices.Clone(t.defs), rest...)
	b, err := json.Marshal(t.every)
	if err != nil {
		return nil, err
	}
	t.full, t.fullN = len(b), len(t.every)
	t.defs = append(t.defs,
		toolDef{Type: typeFunction, Function: toolDefFunc{Name: toolSearch, Parameters: json.RawMessage(searchSchema),
			Description: "Finds Cogpit commands beyond the tools listed here (scaling a StatefulSet, Helm actions, port forwards, " +
				"node actions, editing YAML, the tab bar, …) by words, and returns each one's id, label, danger level, " +
				"description and parameter schema. Run one with run_command. Danger level: read."}},
		toolDef{Type: typeFunction, Function: toolDefFunc{Name: toolRun, Parameters: json.RawMessage(runSchema),
			Description: "Runs any Cogpit command by its id (from search_commands) with its parameters, under the same rules as " +
				"the other tools: read and ui commands run at once; mutating and destructive ones wait for the user's " +
				"confirmation in Cogpit's window and fail if the user declines."}},
	)
	if b, err = json.Marshal(t.defs); err != nil {
		return nil, err
	}
	t.bytes = len(b)
	return t, nil
}

// commandTool is a command as a function tool.
func commandTool(c *engine.Command) (toolDef, error) {
	schema, err := chatSchema(c)
	if err != nil {
		return toolDef{}, err
	}
	return toolDef{Type: typeFunction, Function: toolDefFunc{Name: c.ToolName(), Description: c.ToolDescription(), Parameters: schema}}, nil
}

// typeFunction is the type of every tool and tool call.
const typeFunction = "function"

const jsonNull = "null"

// chatSchema is the command's parameter schema as the endpoints take it: with a "properties"
// object even for a command without parameters. LM Studio refuses a tool whose parameters lack
// one (HTTP 400, "path": [n, "function", "parameters", "properties"], found by the live test of
// 2026-10-14); MCP clients take the registry's schema as it is.
func chatSchema(c *engine.Command) (json.RawMessage, error) {
	schema, err := c.ToolSchema()
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(schema, &m); err != nil {
		return nil, err
	}
	if _, ok := m["properties"]; ok {
		return schema, nil
	}
	m["properties"] = json.RawMessage("{}")
	return json.Marshal(m)
}

// resolved is a tool call made a command invocation.
type resolved struct {
	cmd    *engine.Command // nil for search_commands
	params json.RawMessage
	search *searchArgs
}

type searchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type runArgs struct {
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params"`
}

// resolve maps a tool call to a command and its parameters; the error is the text the model is sent.
func (t *toolset) resolve(name, args string) (resolved, error) {
	raw := json.RawMessage(strings.TrimSpace(args))
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if !json.Valid(raw) {
		return resolved{}, fmt.Errorf("the arguments of %s are not valid JSON; call it again with a JSON object", name)
	}
	switch name {
	case toolSearch:
		var a searchArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return resolved{}, fmt.Errorf("invalid arguments for %s: %v", toolSearch, err)
		}
		return resolved{search: &a}, nil
	case toolRun:
		var a runArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return resolved{}, fmt.Errorf("invalid arguments for %s: %v", toolRun, err)
		}
		c := t.byID[strings.TrimSpace(a.Command)]
		if c == nil {
			c = t.byTN[strings.TrimSpace(a.Command)]
		}
		if c == nil {
			return resolved{}, fmt.Errorf("there is no command %q the chat may run; find one with %s", a.Command, toolSearch)
		}
		p := a.Params
		if len(p) == 0 || string(p) == jsonNull {
			p = json.RawMessage("{}")
		}
		return resolved{cmd: c, params: p}, nil
	}
	c := t.core[name]
	if c == nil {
		if c = t.byTN[name]; c == nil { // a model that calls a command by its tool name directly
			return resolved{}, fmt.Errorf("there is no tool %q; the tools are listed in the request, and %s finds Cogpit's other commands", name, toolSearch)
		}
	}
	return resolved{cmd: c, params: raw}, nil
}

// searchResult is what search_commands returns.
type searchResult struct {
	Commands []searchEntry `json:"commands"`
	More     int           `json:"more,omitempty"`
	Note     string        `json:"note"`
}

type searchEntry struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Danger      engine.Danger   `json:"danger,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// maxSearchDescription bounds one description in a search result.
const maxSearchDescription = 1500

// search finds commands by words: each word found in a command's id, label or menu path counts
// three, in its description one; the best first. An empty query lists every id and label.
func (t *toolset) search(a searchArgs) searchResult {
	words := strings.Fields(strings.ToLower(a.Query))
	if len(words) == 0 {
		r := searchResult{Note: "Every command the chat may run, by id and label. Call search_commands with words to get one's " +
			"parameters, then run_command."}
		for _, c := range t.all {
			r.Commands = append(r.Commands, searchEntry{ID: c.ID, Label: c.Label})
		}
		return r
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 8
	}
	limit = min(limit, 20)
	type scored struct {
		c *engine.Command
		n int
	}
	var hits []scored
	for _, c := range t.all {
		head := strings.ToLower(c.ID + " " + c.Label + " " + c.MenuPath + " " + c.ToolName())
		desc := strings.ToLower(c.Description)
		n := 0
		for _, w := range words {
			if strings.Contains(head, w) {
				n += 3
			} else if strings.Contains(desc, w) {
				n++
			}
		}
		if n > 0 {
			hits = append(hits, scored{c, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	r := searchResult{Note: "Run one with run_command {\"command\": <id>, \"params\": {...}}. Commands that are tools of their own " +
		"can be called directly too."}
	for i, h := range hits {
		if i == limit {
			r.More = len(hits) - limit
			break
		}
		schema, _ := h.c.ToolSchema()
		desc := h.c.ToolDescription()
		if len(desc) > maxSearchDescription {
			desc = truncateUTF8(desc, maxSearchDescription) + "…"
		}
		r.Commands = append(r.Commands, searchEntry{ID: h.c.ID, Label: h.c.Label, Danger: h.c.Danger, Description: desc, Parameters: schema})
	}
	if len(r.Commands) == 0 {
		r.Note = "No command matched; try other words, or an empty query for every command's id and label."
	}
	return r
}
