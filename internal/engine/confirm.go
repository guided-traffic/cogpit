package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ConfirmRequest is what the user is asked to confirm: the command, who asked for it, and the
// objects, namespaces and context it acts on.
type ConfirmRequest struct {
	Command    string            `json:"command"`
	Label      string            `json:"label"`
	Danger     Danger            `json:"danger"`
	Origin     Origin            `json:"origin"`
	Context    string            `json:"context"`
	Namespaces []string          `json:"namespaces,omitempty"`
	Objects    []model.ObjectRef `json:"objects"`
	// Kinds name the objects' kinds as the user reads them ("Pod", "Role Binding", "Release"), in
	// the order of Objects, as the dialog lists them; Message names each object with its kind,
	// else with its kind key.
	Kinds []string `json:"kinds,omitempty"`
	// UIDs are the objects' UIDs as Cogpit's cache held them when the user was asked, in the order
	// of Objects ("" for an object not in the cache): destructive commands act on exactly those
	// objects, never on one created again under the same name while the dialog was open. Only
	// destructive commands record them.
	UIDs []string `json:"uids,omitempty"`
	// Effects say what the command does beyond its objects, one row each (Effect): the keys a
	// save removes, a drain's options, a rollback's revision. The window shows them as rows of the
	// confirmation's table, under the objects (D-85, WF33).
	Effects []Effect `json:"effects,omitempty"`
	// Detail is the effects as one line (EffectsText), which Message ends with and to which a
	// command's own dialog binds the user's answer (the Rollback dialog, a drawer's save review).
	Detail string `json:"detail,omitempty"`
	// Warning is what the user must know before answering, apart from the message: a Helm chart
	// from a repository whose content nothing authenticates (H-41, A14). The GUI's dialog shows it
	// below the message in the warning colour; Message leaves it out.
	Warning string `json:"warning,omitempty"`
	// AgentsAsk is true when an agent's mutating request asks whatever the allowance of agent
	// writes, because the command declares it for these parameters: a role binding (H-27,
	// agentsask.go). The dialog then does not offer the allowance as a way to skip such a question.
	AgentsAsk bool `json:"agentsAsk,omitempty"`
	// Undoable is set for a destructive command whose effect the user can reverse (Spec.Undoable):
	// the dialog does not say "This cannot be undone." (A101).
	Undoable bool `json:"undoable,omitempty"`
	// Review is a text the user reads before answering, which the request itself carries: an
	// agent's Helm values for an upgrade. The command runs with exactly this text (it is part of
	// its parameters). The GUI's dialog shows it in Cogpit's own window; it is never serialised,
	// so no audit line, log or MCP result carries it, and Message leaves it out.
	Review string `json:"-"`
}

// ConfirmNamed is how many objects the confirmation's message names before it gives the count of
// the rest ("and 12 more"); the dialog lists every object below it (a selection's Delete).
const ConfirmNamed = 10

// Effect is one row of what a confirmed command does beyond naming its objects, as the
// confirmation's table shows it (the owner's WF33, 2026-10-19: "eine technisch präzise
// Schnellansicht", never a sentence): a short name and a terse value. "Removes" "color, size"
// (lost), "To revision" "3" (names), "Grace period" "30 s per pod". A value may hold several
// lines ("\n"), one fact each.
type Effect struct {
	Name  string     `json:"name"`
	Value string     `json:"value"`
	Tone  EffectTone `json:"tone,omitempty"`
}

// EffectTone is how the window sets an effect's value.
type EffectTone string

const (
	// EffectPlain is a fact in the text colour: a duration, an option, where the values come from.
	EffectPlain EffectTone = ""
	// EffectNames are names the command acts on or writes (keys, a revision, an image, a chart):
	// bold, as the save review's keys.
	EffectNames EffectTone = "names"
	// EffectLost is what the command loses for good (a removed key, the resources a prune
	// deletes): bold, in the error colour, as the save review's "Removes".
	EffectLost EffectTone = "lost"
	// EffectWarning is what the user weighs before answering (root on a node, an unchecked
	// archive, a proxy that sees credentials): in the warning colour.
	EffectWarning EffectTone = "warning"
)

// EffectsText is the effects on one line, for ConfirmRequest.Detail: "Removes: color, size.
// Adds: debug." A value's lines are joined with "; ".
func EffectsText(effects []Effect) string {
	parts := make([]string, 0, len(effects))
	for _, e := range effects {
		parts = append(parts, e.Name+": "+strings.ReplaceAll(e.Value, "\n", "; ")+".")
	}
	return strings.Join(parts, " ")
}

// Message is the confirmation text: "Delete Pod web-0 in namespace default (context kind-cogpit)?"
// with the origin named when an agent asked. Several objects are counted first and named up to
// ConfirmNamed: "Delete 3 objects: Config Map a, Config Map b, Config Map c in namespace shop
// (context kind-cogpit)?". An object whose kind has no name in Kinds is named by its kind key.
func (r ConfirmRequest) Message() string {
	var b strings.Builder
	if r.Origin.IsAgent() {
		b.WriteString("An agent asks to run: ")
	}
	b.WriteString(r.Label)
	b.WriteString(" ")
	if len(r.Objects) > 1 {
		fmt.Fprintf(&b, "%d objects: ", len(r.Objects))
	}
	names := make([]string, 0, min(len(r.Objects), ConfirmNamed))
	for i, o := range r.Objects[:min(len(r.Objects), ConfirmNamed)] {
		kind := o.Kind
		if i < len(r.Kinds) && r.Kinds[i] != "" {
			kind = r.Kinds[i]
		}
		names = append(names, kind+" "+o.Name)
	}
	b.WriteString(strings.Join(names, ", "))
	if more := len(r.Objects) - ConfirmNamed; more > 0 {
		fmt.Fprintf(&b, " and %d more", more)
	}
	switch {
	case len(r.Namespaces) == 1:
		b.WriteString(" in namespace ")
		b.WriteString(r.Namespaces[0])
	case len(r.Namespaces) > 1:
		b.WriteString(" in namespaces ")
		b.WriteString(strings.Join(r.Namespaces, ", "))
	}
	fmt.Fprintf(&b, " (context %s)?", r.Context)
	if r.Detail != "" {
		b.WriteString(" ")
		b.WriteString(r.Detail)
	}
	return b.String()
}

// Confirmer asks the user to confirm a command. Confirm blocks until the user answers or ctx ends
// and must not be called on the GUI's main goroutine.
type Confirmer interface {
	Confirm(ctx context.Context, req ConfirmRequest) (bool, error)
}

// ConfirmFunc adapts a function to a Confirmer.
type ConfirmFunc func(ctx context.Context, req ConfirmRequest) (bool, error)

// Confirm calls f.
func (f ConfirmFunc) Confirm(ctx context.Context, req ConfirmRequest) (bool, error) {
	return f(ctx, req)
}

func newConfirmRequest(cmd *Command, danger Danger, origin Origin, uiContext string, targets []model.ObjectRef) ConfirmRequest {
	req := ConfirmRequest{
		Command: cmd.ID, Label: cmd.Label, Danger: danger, Origin: origin,
		Context: uiContext, Objects: targets, Undoable: cmd.undoable,
	}
	for _, t := range targets {
		if t.Context != "" {
			req.Context = t.Context
		}
		if t.Namespace != "" && !slices.Contains(req.Namespaces, t.Namespace) {
			req.Namespaces = append(req.Namespaces, t.Namespace)
		}
	}
	return req
}

// kindNames are the targets' kinds as the user reads them, for ConfirmRequest.Kinds: the drawer's
// singular title where the kind has one ("Release", "Chart"), else its view's title in the
// singular ("Pods" → "Pod", "Network Policies" → "Network Policy"; a custom kind's title is its
// Kind already). Without the kind's profile the name is left empty. A real cluster's confirmations
// read "Drain nodes worker-1" and "Delete helmreleases web" before (2026-10-16).
func (s *Session) kindNames(targets []model.ObjectRef) []string {
	if len(targets) == 0 {
		return nil
	}
	b := s.BackendOf(targets[0].Context) // the targets' cluster (one: oneCluster, D-69)
	if b == nil {
		return nil
	}
	out := make([]string, len(targets))
	for i, t := range targets {
		p, ok := b.Profile(t.Kind)
		switch {
		case !ok:
		case p.DetailTitle != "":
			out[i] = p.DetailTitle
		default:
			out[i] = singularTitle(p.Title)
		}
	}
	return out
}

// singularTitle turns a view's title into the singular: "Ingress Classes" → "Ingress Class",
// "Network Policies" → "Network Policy", "Pods" → "Pod"; a title without a plural ending stays.
func singularTitle(title string) string {
	switch {
	case strings.HasSuffix(title, "sses"):
		return strings.TrimSuffix(title, "es")
	case strings.HasSuffix(title, "ies"):
		return strings.TrimSuffix(title, "ies") + "y"
	case strings.HasSuffix(title, "s"):
		return strings.TrimSuffix(title, "s")
	}
	return title
}

// originKey is the context key under which Dispatch hands a command its origin.
type originKey struct{}

// OriginOf returns the origin that dispatched the running command; empty outside Dispatch.
// Commands use it where the answer depends on who asks: agents get Secret values redacted.
func OriginOf(ctx context.Context) Origin {
	o, _ := ctx.Value(originKey{}).(Origin)
	return o
}

// confirmedKey is the context key under which Dispatch hands a command the request the user
// confirmed.
type confirmedKey struct{}

// Confirmed returns the request the user confirmed for the running command, if it needed one.
// Commands that write check it against the cluster they are about to act on: if the user switched
// clusters while the dialog was open, the confirmation named another context and must not apply.
func Confirmed(ctx context.Context) (ConfirmRequest, bool) {
	req, ok := ctx.Value(confirmedKey{}).(ConfirmRequest)
	return req, ok
}

// ErrTargetsChanged is returned when the objects a command would act on are not the ones the user
// confirmed. Match it with errors.Is.
var ErrTargetsChanged = errors.New("the objects changed since they were confirmed")

// ConfirmedUID returns the UID the confirmation recorded for ref and whether the running command
// is bound to it. A destructive command whose object was not in the cache when the user was asked
// gets "" and true: it must refuse rather than act on whatever has the name now. A mutating
// command's confirmation records the UIDs the cache held already (2026-10-12); for an object it
// did not hold, or without a confirmation, the result is "" and false, and the command reads the
// cache.
func ConfirmedUID(ctx context.Context, ref model.ObjectRef) (uid string, confirmed bool) {
	req, ok := Confirmed(ctx)
	destructive := ok && req.Danger == DangerDestructive
	if !ok || len(req.UIDs) == 0 {
		return "", destructive
	}
	for i, o := range req.Objects {
		if i < len(req.UIDs) && o.Kind == ref.Kind && o.Namespace == ref.Namespace && o.Name == ref.Name {
			if req.UIDs[i] == "" && !destructive {
				return "", false
			}
			return req.UIDs[i], true
		}
	}
	return "", destructive
}

// CheckConfirmedTargets fails when the objects a command is about to act on are not the ones the
// user confirmed: a namespace that came from the UI's filter and changed while the dialog was
// open, a manifest whose objects moved. Commands without a confirmation, or confirmed without
// objects (an agent allowance), pass.
func CheckConfirmedTargets(ctx context.Context, refs []model.ObjectRef) error {
	req, ok := Confirmed(ctx)
	if !ok || len(req.Objects) == 0 {
		return nil
	}
	same := len(refs) == len(req.Objects)
	for i := 0; same && i < len(refs); i++ {
		a, b := refs[i], req.Objects[i]
		same = a.Kind == b.Kind && a.Namespace == b.Namespace && a.Name == b.Name
	}
	if !same {
		return fmt.Errorf("%w (the namespace filter or the text moved); nothing was done", ErrTargetsChanged)
	}
	return nil
}

// CheckConfirmedContext fails when the running command was confirmed for another context than
// the one it is about to act on. Commands without a confirmation pass.
func CheckConfirmedContext(ctx context.Context, actOn string) error {
	req, ok := Confirmed(ctx)
	if !ok || req.Context == actOn {
		return nil
	}
	return fmt.Errorf("%w: confirmed for context %q, the connected context is now %q", ErrWrongCluster, req.Context, actOn)
}
