package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Danger is a command's danger level. It decides confirmation (D-2) and the MCP tool annotations.
type Danger string

// Danger levels.
const (
	DangerRead        Danger = "read"        // reads cluster data from the cache
	DangerUI          Danger = "ui"          // changes UI state only
	DangerMutating    Danger = "mutating"    // writes to a cluster
	DangerDestructive Danger = "destructive" // deletes, evicts, restarts or otherwise disrupts
)

// ErrNotApplicable marks a command that does not apply to its object at all, as opposed to one
// that applies but cannot run now. Enabled wraps it when, for example, Cordon is asked of a node
// that is already cordoned: the row menu and the drawer then hide the command (Uncordon takes its
// place) instead of showing it greyed out with a reason. Dispatch refuses it like any disabled
// command.
var ErrNotApplicable = errors.New("does not apply")

// Positions of the actions every kind shares, for Spec.Order: Lens's row menus and drawers end
// with Edit and Delete, the kind's own actions come before them.
const (
	OrderEdit   = 900
	OrderDelete = 1000
)

// Verb is one Kubernetes API verb on one resource that a command uses: the input for access
// reviews and the privilege footprint (docs/security/privilege-footprint.md).
type Verb struct {
	Verb        string `json:"verb"`
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	// Namespace is the one namespace the command uses the verb in, when that is fixed whatever
	// object it acts on (the node shell's kube-system); empty: the object's namespace.
	Namespace string `json:"namespace,omitempty"`
}

// Spec describes a command with typed parameters P. P must be a struct with json tags; its JSON
// schema, which agents read, is generated from it (jsonschema tags give field descriptions).
type Spec[P any] struct {
	ID          string // "<kind-or-area>.<action>", lowerCamelCase segments: "ui.navigate"
	Label       string // menu label: "Navigate to Object"
	MenuPath    string // "View/Navigate"; empty: not in a menu
	Shortcut    string // Lens's default shortcut, if any: "Cmd+Shift+P"
	Description string // one or two sentences for agents (the MCP tool description)
	// MoreKeys are further keys that run the command, which menus do not show: VS Code's secondary
	// keybindings (Zoom In's Cmd+Shift+= and the keypad's +; D-134).
	MoreKeys []string
	// PlatformKeys are the keys on one platform, by GOOS ("darwin", "windows", "linux"), when they
	// differ from Shortcut and MoreKeys there: the first is the shortcut menus show, the others
	// that platform's MoreKeys. VS Code's Go Back is ⌃- on macOS, Alt+Left on Windows and
	// Ctrl+Alt+- on Linux (D-134).
	PlatformKeys map[string][]string
	Kinds        []string // kind keys the command applies to; empty: not kind-specific
	// Order places the command among its kinds' actions in the row menu (⋮) and the drawer's
	// toolbar, ascending: the kind's own actions in Lens's order below OrderEdit, then Edit and
	// Delete. Zero leaves the command out of row menus and drawers (ui.*, kinds.list, ...).
	Order  int
	Danger Danger
	Verbs  []Verb

	// DrawerOnly keeps a command with an Order out of the row menu: it is one of the drawer's
	// actions only (Copy, which Lens's drawer has and its row menus do not).
	DrawerOnly bool

	// Enabled returns nil when the command can run with p, else an error whose text says why
	// not ("no cluster connected"). Nil means always enabled.
	Enabled func(s *Session, p P) error
	// Targets returns the objects a mutating or destructive command acts on, for the confirmation
	// that names them. Required for DangerMutating and DangerDestructive.
	Targets func(s *Session, p P) ([]model.ObjectRef, error)
	// Escalate reports whether this invocation of a mutating command is destructive: Scale is
	// mutating, but scaling to zero replicas is destructive (privilege-footprint.md). Dispatch then
	// confirms and audits it as DangerDestructive. Nil never escalates; only DangerMutating
	// commands may set it.
	Escalate func(s *Session, p P) bool
	// AgentsAsk reports whether this invocation, when an agent asks for it, waits for the user's
	// acceptance even in a context where the user allowed agent writes (D-2, H-14): an RBAC write
	// (agentsask.go, H-27, A1), a Helm install or upgrade (A2); for a UI command, whether the agent's
	// invocation waits for the user at all (a proxy, A20). The
	// GUI's own invocation is unaffected. Nil never; DangerMutating and DangerUI commands may set it
	// (a destructive one always asks anyway).
	AgentsAsk func(s *Session, p P) bool
	// Prefill returns the parameters a dialog shows before the user edits them, read from the
	// cache: the current replica count for Scale. Nil shows p as it is.
	Prefill func(s *Session, p P) (P, error)
	// Dialog says how the window's generated dialog shows some of the parameters, by their JSON
	// names: a hint written for a person and a slider's range (DialogField). Agents never read it;
	// they read the schema. Nil: every field as its schema says.
	Dialog map[string]DialogField
	// Effects are the confirmation's rows for what the objects don't show (Effect): the keys a
	// save removes, the drain options that lose data, a rollback's revision. Short names and terse
	// values, never a sentence (D-85, WF33). Nil, or none, adds no row.
	Effects func(s *Session, p P) []Effect
	// Warning returns what the user must know before answering, which the confirmation shows apart
	// in the warning colour (ConfirmRequest.Warning): a Helm chart from a repository whose content
	// nothing authenticates (H-41, A14). Nil, or "", shows none.
	Warning func(s *Session, p P) string
	// Undoable marks a destructive command whose effect the user can reverse, nothing being lost:
	// a grant (deleting the binding takes it back), a rollback (the release's history keeps the
	// revision), a node shell. Its confirmation does not say "This cannot be undone." (A101, owner
	// 2026-10-14 evening: only where something is lost).
	Undoable bool
	// Refuse refuses the command for the origin that dispatched it (ctx carries it: OriginOf)
	// before anyone is asked: a command whose Run would refuse that origin anyway (an agent's
	// apply or create of a Secret, D-19) must not ask the user first. Nil refuses nothing.
	Refuse func(ctx context.Context, s *Session, p P) error
	// GUIOnly marks a command that only the user's own action in Cogpit's window runs, whatever
	// its parameters (guionly.go): Dispatch refuses every other origin with a GUIOnlyError that
	// wraps this reason, once the parameters decode and before Enabled, Refuse, a confirmation or
	// Run. Nil: the origins Refuse and Run allow.
	GUIOnly error
	// Review returns a text of the parameters the user reads in the confirmation before
	// answering (ConfirmRequest.Review): an agent's Helm values. Nil, or "", shows none.
	Review func(s *Session, p P) string
	// Run executes the command and returns a JSON-serializable result. Reads come from the cache.
	Run func(ctx context.Context, s *Session, p P) (any, error)
}

// Command is a registered command. Its fields are read-only after registration.
type Command struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	MenuPath string `json:"menuPath,omitempty"`
	Shortcut string `json:"shortcut,omitempty"` // this platform's (Spec.PlatformKeys)
	// MoreKeys are this platform's further keys, which menus do not show (Spec.MoreKeys).
	MoreKeys    []string `json:"moreKeys,omitempty"`
	Description string   `json:"description,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
	Order       int      `json:"order,omitempty"`
	DrawerOnly  bool     `json:"drawerOnly,omitempty"` // in the drawer's actions, not in the row menu
	Danger      Danger   `json:"danger"`
	// Escalates is true when some invocations are destructive although Danger is mutating.
	Escalates bool `json:"escalates,omitempty"`
	// AgentsAsk is true when some invocations by agents wait for the user's acceptance whatever the
	// allowance of agent writes (Spec.AgentsAsk).
	AgentsAsk bool `json:"agentsAsk,omitempty"`
	// GUIOnly is true for a command only the user's own action in the window runs (Spec.GUIOnly).
	GUIOnly bool               `json:"guiOnly,omitempty"`
	Verbs   []Verb             `json:"verbs,omitempty"`
	Schema  *jsonschema.Schema `json:"schema"`

	guiOnly   error // the reason a GUI-only command gives every other origin
	undoable  bool  // Spec.Undoable
	resolved  *jsonschema.Resolved
	decode    func(raw json.RawMessage) (any, error)
	enabled   func(s *Session, p any) error
	targets   func(s *Session, p any) ([]model.ObjectRef, error)
	escalate  func(s *Session, p any) bool
	agentsAsk func(s *Session, p any) bool
	prefill   func(s *Session, p any) (any, error)
	dialog    map[string]DialogField // Spec.Dialog
	effects   func(s *Session, p any) []Effect
	warning   func(s *Session, p any) string
	review    func(s *Session, p any) string
	refuse    func(ctx context.Context, s *Session, p any) error
	run       func(ctx context.Context, s *Session, p any) (any, error)
}

// ToolName returns the command's MCP tool name: the id with dots replaced by underscores.
func (c *Command) ToolName() string { return strings.ReplaceAll(c.ID, ".", "_") }

// Enabled reports whether the command can run with the given parameters: nil, or an error whose
// text is the reason shown to the user.
func (c *Command) Enabled(s *Session, params json.RawMessage) error {
	p, err := c.decode(params)
	if err != nil {
		return err
	}
	return c.enabled(s, p)
}

// Applicable reports whether the command applies to the given parameters at all: false when
// Enabled says ErrNotApplicable. A command that applies may still be disabled; Enabled says why.
func (c *Command) Applicable(s *Session, params json.RawMessage) bool {
	return !errors.Is(c.Enabled(s, params), ErrNotApplicable)
}

// Prefill returns the parameters a dialog for the command starts with: params completed from the
// cache by the command's Prefill, or params unchanged.
func (c *Command) Prefill(s *Session, params json.RawMessage) (json.RawMessage, error) {
	p, err := c.decode(params)
	if err != nil {
		return nil, err
	}
	if c.prefill == nil {
		return normalizeParams(params), nil
	}
	out, err := c.prefill(s, p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// dangerFor is the danger of one invocation: the command's level, or destructive when the
// command escalates for these parameters.
func (c *Command) dangerFor(s *Session, p any) Danger {
	if c.escalate != nil && c.escalate(s, p) {
		return DangerDestructive
	}
	return c.Danger
}

// agentsAskFor reports whether an agent's invocation with p waits for the user whatever the
// allowance of agent writes (Spec.AgentsAsk).
func (c *Command) agentsAskFor(s *Session, p any) bool {
	return c.agentsAsk != nil && c.agentsAsk(s, p)
}

// Registry holds every command. It is safe for concurrent use.
type Registry struct {
	mu   sync.RWMutex
	cmds map[string]*Command
	goos string // the platform whose keys the commands carry (keys.go)
}

// NewRegistry returns an empty registry whose commands carry the keys of the platform Cogpit runs
// on.
func NewRegistry() *Registry { return NewRegistryFor(runtime.GOOS) }

// NewRegistryFor returns an empty registry whose commands carry the keys of the platform goos
// ("darwin", "windows", "linux"; keys.go).
func NewRegistryFor(goos string) *Registry { return &Registry{cmds: map[string]*Command{}, goos: goos} }

// Get returns the command with the given id.
func (r *Registry) Get(id string) (*Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cmds[id]
	return c, ok
}

// Commands returns every command, sorted by id.
func (r *Registry) Commands() []*Command {
	r.mu.RLock()
	out := make([]*Command, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, c)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Register adds a command. It fails on a duplicate or malformed id, a missing Run, an unknown
// danger level, a mutating or destructive command without Targets, or parameters whose schema
// cannot be generated.
func Register[P any](r *Registry, spec Spec[P]) error {
	if err := validID(spec.ID); err != nil {
		return err
	}
	if spec.Run == nil {
		return fmt.Errorf("command %s: Run is required", spec.ID)
	}
	switch spec.Danger {
	case DangerRead, DangerUI:
	case DangerMutating, DangerDestructive:
		if spec.Targets == nil {
			return fmt.Errorf("command %s: %s commands need Targets for the confirmation", spec.ID, spec.Danger)
		}
	default:
		return fmt.Errorf("command %s: unknown danger %q", spec.ID, spec.Danger)
	}
	if spec.Escalate != nil && spec.Danger != DangerMutating {
		return fmt.Errorf("command %s: only mutating commands escalate", spec.ID)
	}
	if spec.AgentsAsk != nil && spec.Danger != DangerMutating && spec.Danger != DangerUI {
		return fmt.Errorf("command %s: only mutating and UI commands declare AgentsAsk (a destructive one always asks)", spec.ID)
	}
	if spec.Order < 0 {
		return fmt.Errorf("command %s: negative order", spec.ID)
	}
	if spec.DrawerOnly && spec.Order == 0 {
		return fmt.Errorf("command %s: a drawer-only command needs an order", spec.ID)
	}
	if err := checkKeys(spec.Shortcut, spec.MoreKeys, spec.PlatformKeys); err != nil {
		return fmt.Errorf("command %s: %w", spec.ID, err)
	}
	schema, err := jsonschema.For[P](nil)
	if err != nil {
		return fmt.Errorf("command %s: params schema: %w", spec.ID, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("command %s: resolve params schema: %w", spec.ID, err)
	}
	if err := checkDialogFields(schema, spec.Dialog); err != nil {
		return fmt.Errorf("command %s: %w", spec.ID, err)
	}
	shortcut, more := keysOn(r.goos, spec.Shortcut, spec.MoreKeys, spec.PlatformKeys)
	c := &Command{
		ID: spec.ID, Label: spec.Label, MenuPath: spec.MenuPath, Shortcut: shortcut, MoreKeys: more,
		Description: spec.Description, Kinds: spec.Kinds, Order: spec.Order, DrawerOnly: spec.DrawerOnly, Danger: spec.Danger,
		Escalates: spec.Escalate != nil, AgentsAsk: spec.AgentsAsk != nil, GUIOnly: spec.GUIOnly != nil, guiOnly: spec.GUIOnly, undoable: spec.Undoable, Verbs: spec.Verbs, Schema: schema,
		resolved: resolved, dialog: spec.Dialog,
	}
	c.decode = func(raw json.RawMessage) (any, error) {
		var p P
		raw = normalizeParams(raw)
		var inst any
		if err := json.Unmarshal(raw, &inst); err != nil {
			return nil, &ParamsError{Command: spec.ID, Err: err}
		}
		if err := resolved.Validate(inst); err != nil {
			return nil, &ParamsError{Command: spec.ID, Err: err}
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return nil, &ParamsError{Command: spec.ID, Err: err}
		}
		return p, nil
	}
	c.enabled = func(s *Session, p any) error {
		var own error
		if spec.Enabled != nil {
			// A command that does not apply stays hidden whatever the access reviews say (Uncordon
			// on a node that is not cordoned is not offered, permitted or not).
			if own = spec.Enabled(s, p.(P)); errors.Is(own, ErrNotApplicable) {
				return own
			}
		}
		// No write starts on a connection that does not answer: its outcome would be unknown
		// (D-58, connwatch.go), the command's cluster's connection deciding (D-69, target.go). The
		// reason is the window's and the agents' alike.
		if err := s.writeBlocked(c.Danger, targetContext(s, p)); err != nil {
			return err
		}
		if err := s.permitted(c, p); err != nil {
			return err
		}
		return own
	}
	c.targets = func(s *Session, p any) ([]model.ObjectRef, error) {
		if spec.Targets == nil {
			return nil, nil
		}
		return spec.Targets(s, p.(P))
	}
	if spec.Escalate != nil {
		c.escalate = func(s *Session, p any) bool { return spec.Escalate(s, p.(P)) }
	}
	if spec.AgentsAsk != nil {
		c.agentsAsk = func(s *Session, p any) bool { return spec.AgentsAsk(s, p.(P)) }
	}
	if spec.Prefill != nil {
		c.prefill = func(s *Session, p any) (any, error) { return spec.Prefill(s, p.(P)) }
	}
	if spec.Effects != nil {
		c.effects = func(s *Session, p any) []Effect { return spec.Effects(s, p.(P)) }
	}
	if spec.Warning != nil {
		c.warning = func(s *Session, p any) string { return spec.Warning(s, p.(P)) }
	}
	if spec.Review != nil {
		c.review = func(s *Session, p any) string { return spec.Review(s, p.(P)) }
	}
	if spec.Refuse != nil {
		c.refuse = func(ctx context.Context, s *Session, p any) error { return spec.Refuse(ctx, s, p.(P)) }
	}
	c.run = func(ctx context.Context, s *Session, p any) (any, error) {
		return spec.Run(ctx, s, p.(P))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.cmds[spec.ID]; dup {
		return fmt.Errorf("command %s: registered twice", spec.ID)
	}
	r.cmds[spec.ID] = c
	return nil
}

func normalizeParams(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("{}")
	}
	return raw
}

func validID(id string) error {
	if id == "" {
		return errors.New("command id is empty")
	}
	segs := strings.Split(id, ".")
	if len(segs) < 2 {
		return fmt.Errorf("command id %q: want <kind-or-area>.<action>", id)
	}
	for _, seg := range segs {
		if seg == "" {
			return fmt.Errorf("command id %q: empty segment", id)
		}
		for i, r := range seg {
			lower := r >= 'a' && r <= 'z'
			upper := r >= 'A' && r <= 'Z'
			digit := r >= '0' && r <= '9'
			if !lower && (i == 0 || (!upper && !digit)) {
				return fmt.Errorf("command id %q: segments are lowerCamelCase", id)
			}
		}
	}
	return nil
}
