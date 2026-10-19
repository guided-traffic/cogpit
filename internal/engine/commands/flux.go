package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Flux CD actions (resource-parity › Paid Lens Desktop views › Flux CD, G23; D-33, decided by
// the owner 2026-10-14): Reconcile, Reconcile with Source, Reset, Suspend and Resume on one Flux
// object and on a selection, each one JSON merge patch per object as the flux CLI sends it
// (cluster.FluxPatch), bound to the object's UID. Lens's Flux actions are not documented or
// captured: their names, menu places and rules are Cogpit's.
//
//   - Reconcile sets reconcile.fluxcd.io/requestedAt to now; for a Helm release optionally also
//     reconcile.fluxcd.io/forceAt (force), as `flux reconcile helmrelease --force` does. Not on a
//     suspended object (the controller would ignore it, as the flux CLI refuses it), nor on an
//     Alert or Provider that reports no status (notification v1beta3's static objects).
//   - Reconcile with Source (Kustomizations, Helm releases) first requests a reconcile of the
//     object's source, then of the object, as `flux reconcile … --with-source` does
//     (fluxsource.go); a Helm release's HelmChart with the chart's own source before it.
//   - Reset (Helm releases) requests a reconcile with reconcile.fluxcd.io/resetAt, as `flux
//     reconcile helmrelease --reset` does: the controller forgets the release's failure counts and
//     tries again after it ran out of retries.
//   - Suspend sets spec.suspend: true; Resume sets it false and requests a reconcile, as `flux
//     resume` does. Each is offered where the other is not.
//
// All are mutating, not destructive (D-33): agents need the user's acceptance unless agent writes
// are allowed for the context (D-2), and the audit log has every agent's.

// Command ids of the Flux actions.
const (
	fluxReconcileID               = "flux.reconcile"
	fluxSuspendID                 = "flux.suspend"
	fluxResumeID                  = "flux.resume"
	fluxReconcileWithSourceID     = "flux.reconcileWithSource"
	fluxResetID                   = "flux.reset"
	fluxReconcileManyID           = "flux.reconcileMany"
	fluxSuspendManyID             = "flux.suspendMany"
	fluxResumeManyID              = "flux.resumeMany"
	fluxReconcileWithSourceManyID = "flux.reconcileWithSourceMany"
	fluxResetManyID               = "flux.resetMany"
)

// fluxHelmRelease is the Kind of Flux's Helm releases, the only one Force and Reset take.
const fluxHelmRelease = "HelmRelease"

// Row-menu places of the Flux actions: before Edit and Delete, Reconcile first, then Reconcile with
// Source and Reset; Suspend and Resume share a place, as only one of them applies.
const (
	orderFluxReconcile           = 100
	orderFluxReconcileWithSource = 110
	orderFluxReset               = 120
	orderFluxSuspend             = 200
)

// The parameters of the Flux actions: Reconcile's take force, Suspend's and Resume's do not.
type (
	fluxReconcileParams struct {
		Ref   model.ObjectRef `json:"ref" jsonschema:"the Flux object"`
		Force bool            `json:"force,omitempty" jsonschema:"Helm releases only: also force a one-off install or upgrade even without changes (reconcile.fluxcd.io/forceAt), as flux reconcile helmrelease --force does"`
	}
	fluxReconcileManyParams struct {
		Refs  []model.ObjectRef `json:"refs" jsonschema:"the Flux objects, as ui_state's selected lists them, at most 500"`
		Force bool              `json:"force,omitempty" jsonschema:"Helm releases only: also force a one-off install or upgrade (reconcile.fluxcd.io/forceAt); refused when a selected object is not a Helm release"`
	}
	fluxRefParams struct {
		Ref model.ObjectRef `json:"ref" jsonschema:"the Flux object"`
	}
	fluxRefsParams struct {
		Refs []model.ObjectRef `json:"refs" jsonschema:"the Flux objects, as ui_state's selected lists them, at most 500"`
	}
)

// kindKeys returns the Flux kind keys an action takes.
func (a fluxAction) kindKeys() []string {
	var out []string
	for _, k := range kinds.FluxKinds() {
		if a.takes(k) {
			out = append(out, k.Key)
		}
	}
	return out
}

// takes reports whether an action applies to a Flux kind: Suspend and Resume to the kinds with
// spec.suspend, the others to the kinds a controller reconciles on request, narrowed to only.
func (a fluxAction) takes(k kinds.FluxKind) bool {
	if len(a.only) > 0 && !slices.Contains(a.only, k.APIKind) {
		return false
	}
	if a.suspend {
		return k.Suspend
	}
	return k.Reconcile
}

// fluxAction is one of the actions: what it writes and when it applies to an object.
type fluxAction struct {
	id, manyID, label, done string
	suspend                 bool     // its kinds are those with spec.suspend
	only                    []string // the Kinds it is narrowed to (Reset: HelmRelease); nil: every one
	// withSource: the action writes the object's sources first (Reconcile with Source,
	// fluxsource.go), so its targets are the sources and the object.
	withSource bool
	order      int
	// applies returns nil when the action applies to the object in its state now, else a fluxSkip
	// that says why not.
	applies func(k kinds.FluxKind, obj *unstructured.Unstructured) error
	request func(force bool) cluster.FluxRequest
}

// fluxSkip is an object an action applies to by its kind but not in its state now: a suspended
// object for Reconcile (greyed, with the reason), one suspended already for Suspend (hidden: Resume
// takes its place). An action on a selection skips such objects.
type fluxSkip struct {
	reason string
	hide   bool // the single action is not offered at all (engine.ErrNotApplicable)
}

func (e fluxSkip) Error() string { return e.reason }

// Is makes a hidden skip engine.ErrNotApplicable.
func (e fluxSkip) Is(target error) bool { return e.hide && target == engine.ErrNotApplicable }

var (
	fluxReconcile = fluxAction{
		id: fluxReconcileID, manyID: fluxReconcileManyID, label: "Reconcile", done: "reconcile requested", order: orderFluxReconcile,
		applies: func(k kinds.FluxKind, obj *unstructured.Unstructured) error {
			if kinds.IsFluxSuspended(obj) {
				return fluxSkip{reason: obj.GetName() + " is suspended: resume it to reconcile it"}
			}
			if (k.APIKind == "Alert" || k.APIKind == "Provider") && len(model.NestedSlice(obj, "status", "conditions")) == 0 {
				return fluxSkip{reason: obj.GetName() + " reports no status: the notification controller does not reconcile it", hide: true}
			}
			return nil
		},
		request: func(force bool) cluster.FluxRequest { return cluster.FluxRequest{Reconcile: true, Force: force} },
	}
	fluxReconcileWithSource = fluxAction{
		id: fluxReconcileWithSourceID, manyID: fluxReconcileWithSourceManyID, label: "Reconcile with Source",
		done: "reconcile requested with source", order: orderFluxReconcileWithSource,
		only: []string{"Kustomization", fluxHelmRelease}, withSource: true,
		applies: func(k kinds.FluxKind, obj *unstructured.Unstructured) error {
			if kinds.IsFluxSuspended(obj) {
				return fluxSkip{reason: obj.GetName() + " is suspended: resume it to reconcile it"}
			}
			if _, err := kinds.FluxSourceOf(k.Key, obj); err != nil {
				return fluxSkip{reason: err.Error()}
			}
			return nil
		},
		request: func(bool) cluster.FluxRequest { return cluster.FluxRequest{Reconcile: true} },
	}
	fluxReset = fluxAction{
		id: fluxResetID, manyID: fluxResetManyID, label: "Reset", done: "reset requested", order: orderFluxReset,
		only: []string{fluxHelmRelease},
		applies: func(_ kinds.FluxKind, obj *unstructured.Unstructured) error {
			if kinds.IsFluxSuspended(obj) {
				return fluxSkip{reason: obj.GetName() + " is suspended: resume it to reset it"}
			}
			return nil
		},
		request: func(bool) cluster.FluxRequest { return cluster.FluxRequest{Reconcile: true, Reset: true} },
	}
	fluxSuspend = fluxAction{
		id: fluxSuspendID, manyID: fluxSuspendManyID, label: "Suspend", done: "suspended", suspend: true, order: orderFluxSuspend,
		applies: func(_ kinds.FluxKind, obj *unstructured.Unstructured) error {
			if kinds.IsFluxSuspended(obj) {
				return fluxSkip{reason: obj.GetName() + " is suspended already", hide: true}
			}
			return nil
		},
		request: func(bool) cluster.FluxRequest { yes := true; return cluster.FluxRequest{Suspend: &yes} },
	}
	fluxResume = fluxAction{
		id: fluxResumeID, manyID: fluxResumeManyID, label: "Resume", done: "resumed", suspend: true, order: orderFluxSuspend,
		applies: func(_ kinds.FluxKind, obj *unstructured.Unstructured) error {
			if !kinds.IsFluxSuspended(obj) {
				return fluxSkip{reason: obj.GetName() + " is not suspended", hide: true}
			}
			return nil
		},
		request: func(bool) cluster.FluxRequest { no := false; return cluster.FluxRequest{Reconcile: true, Suspend: &no} },
	}
)

// Descriptions of the actions for agents.
var fluxDescriptions = map[string]string{
	fluxReconcileID: "Asks Flux to reconcile one object now, as flux reconcile does without --with-source: sets the annotation " +
		"reconcile.fluxcd.io/requestedAt to the current time (with force, a Helm release also gets reconcile.fluxcd.io/forceAt). " +
		"Does not wait for the controller; the object's Status column follows its conditions. Not offered for a suspended " +
		"object, nor for an Alert or Provider that reports no status. Kinds: every Flux kind but External Artifacts.",
	fluxSuspendID: "Suspends one Flux object, as flux suspend does: sets spec.suspend to true, so that its controller leaves it " +
		"alone until it is resumed. Offered only where flux_resume is not.",
	fluxResumeID: "Resumes a suspended Flux object, as flux resume does: sets spec.suspend to false and requests a reconcile " +
		"(reconcile.fluxcd.io/requestedAt). Offered only for a suspended object, in flux_suspend's place.",
	fluxReconcileWithSourceID: "Asks Flux to reconcile a Kustomization or Helm release together with its source, as flux reconcile " +
		"--with-source does, waiting as it does: a background job with a dock tab of its own sets " +
		"reconcile.fluxcd.io/requestedAt on the source first (a Kustomization's spec.sourceRef; a Helm release's chart " +
		"source, then its HelmChart; or its spec.chartRef), waits until the source's controller has handled the request " +
		"and reports it ready (at most 5 minutes), then asks for the object and waits for it too; each request bound to " +
		"its object's UID. A source that ends not ready, or times out, stops the job before the object. An OCI Helm " +
		"repository is skipped, as no controller reconciles it. Refused, with nothing done, when the object or a source " +
		"is suspended or a source is not in the cluster. Returns at once with the job's tab: ui_state shows its steps, " +
		"state and outcome (the dock tab's job); dock_close on the tab stops it, undoing nothing.",
	fluxResetID: "Resets a Helm release's failure counts and asks Flux to reconcile it, as flux reconcile helmrelease --reset " +
		"does: sets reconcile.fluxcd.io/resetAt and reconcile.fluxcd.io/requestedAt to one token, so that helm-controller " +
		"tries to install or upgrade again after it ran out of retries. Not offered for a suspended release.",
}

func registerFlux(g *registrar) {
	add(g, fluxOneSpec(fluxReconcile, func(p fluxReconcileParams) (model.ObjectRef, bool) { return p.Ref, p.Force }))
	add(g, fluxManySpec(fluxReconcile, func(p fluxReconcileManyParams) ([]model.ObjectRef, bool) { return p.Refs, p.Force }))
	for _, a := range []fluxAction{fluxReconcileWithSource, fluxReset, fluxSuspend, fluxResume} {
		add(g, fluxOneSpec(a, func(p fluxRefParams) (model.ObjectRef, bool) { return p.Ref, false }))
		add(g, fluxManySpec(a, func(p fluxRefsParams) ([]model.ObjectRef, bool) { return p.Refs, false }))
	}
	registerFluxDashboard(g)
}

type fluxDashboardParams struct {
	engine.OnCluster
	Namespaces []string `json:"namespaces,omitempty" jsonschema:"namespaces to count; omitted: the namespace filter the user has set (every namespace for a cluster the window does not show)"`
	Table      bool     `json:"table,omitempty" jsonschema:"true: include the page's table, the Flux objects of those namespaces that are not ready (most recent change first)"`
}

// FluxDashboardResult is flux.dashboard's result.
type FluxDashboardResult struct {
	engine.FluxDashboardModel
	// NotReady is the page's table: Name, Kind, Namespace, Status, Message, Age. Only with table:
	// true.
	NotReady *engine.TableSnapshot `json:"notReady,omitempty"`
}

// registerFluxDashboard registers the Flux dashboard's read (D-34): agents read what the page
// shows; ui.openView "flux/dashboard" shows it to the user.
func registerFluxDashboard(g *registrar) {
	add(g, engine.Spec[fluxDashboardParams]{
		ID: "flux.dashboard", Label: engine.FluxDashboardTitle, MenuPath: "View/" + engine.FluxDashboardTitle, Danger: engine.DangerRead,
		Verbs: cacheReads,
		Description: "Reads the Flux dashboard: for each Flux kind the cluster serves (Helm releases, Kustomizations, image " +
			"repositories, policies and update automations, alerts, providers, receivers, Git, OCI and Helm repositories, " +
			"buckets, external artifacts) the number of objects in the selected namespaces by state: ready, notReady (Ready " +
			"False or Stalled), reconciling, suspended, unknown (no Ready condition yet); with table, also every object that " +
			"is not ready, with its message. Read from the cache. ui_openView with view flux/dashboard shows the page to the " +
			"user; flux_reconcile, flux_reconcileWithSource, flux_reset, flux_suspend and flux_resume act on the objects.",
		Enabled: func(s *engine.Session, p fluxDashboardParams) error { _, err := connectedOn(s, p.Context); return err },
		Run: func(ctx context.Context, s *engine.Session, p fluxDashboardParams) (any, error) {
			m, table, err := s.FluxDashboard(ctx, p.Context, p.Namespaces, p.Table)
			if err != nil {
				return nil, err
			}
			return FluxDashboardResult{FluxDashboardModel: *m, NotReady: table}, nil
		},
	})
}

// fluxOneSpec is an action on one object: the row menu's, the drawer's and the palette's. get
// reads the object and force from its parameters.
func fluxOneSpec[P any](a fluxAction, get func(P) (model.ObjectRef, bool)) engine.Spec[P] {
	return engine.Spec[P]{
		ID: a.id, Label: a.label, MenuPath: "Flux/" + a.label, Kinds: a.kindKeys(),
		Order: a.order, Danger: engine.DangerMutating,
		Verbs:       []engine.Verb{{Verb: nwVerbPatch, Resource: "*"}},
		Description: fluxDescriptions[a.id],
		Enabled: func(s *engine.Session, p P) error {
			ref, force := get(p)
			if err := fluxApplies(s, a, ref, force); err != nil {
				return err
			}
			if err := fluxSourcePermitted(s, a, ref); err != nil {
				return err
			}
			_, _, err := fluxActionsOf(s, ref.Context)
			return err
		},
		Targets: func(s *engine.Session, p P) ([]model.ObjectRef, error) {
			ref, _ := get(p)
			if a.withSource {
				return fluxSourceTargets(s, ref)
			}
			return targetOf(s, ref)
		},
		// Whose rights the reconciliation uses (A34, fluxidentity.go).
		Effects: func(s *engine.Session, p P) []engine.Effect {
			ref, _ := get(p)
			return fluxIdentityEffects(s, []model.ObjectRef{ref})
		},
		Run: func(ctx context.Context, s *engine.Session, p P) (any, error) {
			ref, force := get(p)
			if a.withSource {
				return fluxRunWithSource(ctx, s, a, ref)
			}
			return act(ctx, s, ref, a.done, func(_ engine.Actions, ref model.ObjectRef) error {
				return fluxWrite(ctx, s, a, ref, force)
			})
		},
	}
}

// fluxManySpec is an action on a selection: the selection's menu's. It acts on the selected
// objects it applies to (Reconcile skips the suspended ones, Suspend the suspended, Resume the
// others), one after the other, as Delete of a selection does.
func fluxManySpec[P any](a fluxAction, get func(P) ([]model.ObjectRef, bool)) engine.Spec[P] {
	desc := "Does what " + strings.ReplaceAll(a.id, ".", "_") + " does for several Flux objects at once, as the table's " +
		"selection menu does: it acts on the selected objects it applies to, one after the other, each bound to its UID, " +
		"and skips the others. When an object fails the others still go on; nothing is retried. At most 500 objects. " +
		strings.ReplaceAll(jobManyDescription, "objects", "objects it acts on")
	if a.withSource {
		desc = "Does what flux_reconcileWithSource does for several Kustomizations and Helm releases at once, as the table's " +
			"selection menu does, in one background job with one dock tab: the objects' chains side by side, a source two " +
			"objects share asked once, each object asked only once its source is ready, one that fails stopping only the " +
			"objects that need it; the selected objects it cannot reconcile (suspended, without a source it can ask) are " +
			"listed as skipped. Returns at once with the job's tab; ui_state shows its steps and outcome. At most 500 objects."
	}
	return engine.Spec[P]{
		ID: a.manyID, Label: a.label, MenuPath: "Flux/" + a.label + " Selected", Kinds: a.kindKeys(),
		Order: a.order, Danger: engine.DangerMutating,
		Verbs:       []engine.Verb{{Verb: nwVerbPatch, Resource: "*"}},
		Description: desc,
		Enabled: func(s *engine.Session, p P) error {
			refs, force := get(p)
			if _, err := fluxManyTargets(s, a, refs, force); err != nil {
				return err
			}
			_, _, err := fluxActionsOf(s, refsContext(refs))
			return err
		},
		Targets: func(s *engine.Session, p P) ([]model.ObjectRef, error) {
			refs, force := get(p)
			if a.withSource {
				return fluxManySourceTargets(s, a, refs)
			}
			return fluxManyTargets(s, a, refs, force)
		},
		Effects: func(s *engine.Session, p P) []engine.Effect { // A34, fluxidentity.go
			refs, force := get(p)
			if acting, err := fluxManyTargets(s, a, refs, force); err == nil {
				refs = acting
			}
			return fluxIdentityEffects(s, refs)
		},
		Run: func(ctx context.Context, s *engine.Session, p P) (any, error) {
			refs, force := get(p)
			if a.withSource {
				return fluxRunManyWithSource(ctx, s, a, refs)
			}
			return fluxRunMany(ctx, s, a, refs, force)
		},
	}
}

// fluxApplies checks that the action applies to the object as the cache holds it.
func fluxApplies(s *engine.Session, a fluxAction, ref model.ObjectRef, force bool) error {
	k, ok := kinds.FluxKindOf(ref.Kind)
	if !ok || !a.takes(k) {
		return fmt.Errorf("%w: %s does not apply to %s", engine.ErrNotApplicable, a.label, ref.Kind)
	}
	if force && k.APIKind != fluxHelmRelease {
		return fmt.Errorf("force applies to Helm releases only, not to %s", k.Title)
	}
	obj, err := s.Lookup(ref)
	if err != nil {
		return err
	}
	return a.applies(k, obj)
}

// fluxManyTargets returns the selected objects the action applies to, each once, in order; an
// error when there are none, or a selected object is not a Flux object of the action's kinds or
// not in the cache.
func fluxManyTargets(s *engine.Session, a fluxAction, refs []model.ObjectRef, force bool) ([]model.ObjectRef, error) {
	if len(refs) == 0 {
		return nil, errors.New("no objects are selected")
	}
	if len(refs) > engine.MaxSelection {
		return nil, fmt.Errorf("at most %d objects at once, %d are selected", engine.MaxSelection, len(refs))
	}
	var out []model.ObjectRef
	var reason error
	for _, r := range refs {
		var skip fluxSkip
		err := fluxApplies(s, a, r, force)
		switch {
		case errors.As(err, &skip):
			reason = err
		case err != nil:
			return nil, fmt.Errorf("%s: %w", r.String(), err)
		default:
			ref, _, rerr := s.ResolveRef(r)
			if rerr != nil {
				return nil, fmt.Errorf("%s: %w", r.String(), rerr)
			}
			if !slices.ContainsFunc(out, ref.Same) {
				out = append(out, ref)
			}
		}
	}
	if len(out) == 0 {
		if reason == nil {
			reason = errors.New("nothing to do")
		}
		return nil, fmt.Errorf("%w: no selected object to %s: %v", engine.ErrNotApplicable, strings.ToLower(a.label), reason)
	}
	return out, nil
}

// FluxManyResult is the result of an action on a selection when every object was done.
type FluxManyResult struct {
	Action string            `json:"action"` // "reconcile requested", "suspended", "resumed", "reset requested"
	Done   []model.ObjectRef `json:"done"`
}

// FluxManyError is the error of an action on a selection that did not do every object.
type FluxManyError struct {
	Action string
	Done   []model.ObjectRef
	Failed []DeleteFailure
}

func (e *FluxManyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d of %d objects", e.Action, len(e.Done), len(e.Done)+len(e.Failed))
	if len(e.Done) > 0 {
		b.WriteString(" (" + refList(e.Done) + ")")
	}
	b.WriteString("; not done: ")
	for i, f := range e.Failed {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Ref.String() + ": " + f.Error)
	}
	b.WriteString(". Nothing was retried.")
	return b.String()
}

// fluxRunMany runs an action on the selection's objects it applies to, one after the other, each
// bound to its UID; every failure is kept for the report; more than engine.DirectSelection as a
// job (manyjob.go). A crash in the middle leaves some objects patched (an annotation that asks for
// a reconcile or a reset, or spec.suspend) and the others not, each wholly one or the other: their
// controllers act on what was written whether Cogpit runs or not, and the action on those left
// finishes what the user asked for (D-11).
func fluxRunMany(ctx context.Context, s *engine.Session, a fluxAction, refs []model.ObjectRef, force bool) (any, error) {
	b, err := writableOn(s, refsContext(refs)) // the selection's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	targets, err := fluxManyTargets(s, a, refs, force)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, targets); err != nil {
		return nil, err
	}
	m := manyAction{
		id: a.manyID, label: a.label, done: a.done, noun: "Flux objects", b: b, targets: targets,
		do: func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error) {
			if ref.Context != b.Context {
				return ref, fmt.Errorf("%w: the objects are in another cluster", engine.ErrWrongCluster)
			}
			return ref, fluxWrite(ctx, s, a, ref, force)
		},
	}
	return runMany(ctx, s, m, func(out manyOutcome) (any, error) {
		if len(out.failed) > 0 {
			return nil, &FluxManyError{Action: a.done, Done: out.done, Failed: out.failed}
		}
		userToast(s, engine.LevelInfo, fmt.Sprintf("%d Flux objects %s", len(out.done), a.done))
		return FluxManyResult{Action: a.done, Done: out.done}, nil
	})
}

// fluxWrite patches one object: its kind's served version, the UID the user confirmed (or the
// cache's), the action's request.
func fluxWrite(ctx context.Context, s *engine.Session, a fluxAction, ref model.ObjectRef, force bool) error {
	b, w, err := fluxActionsOf(s, ref.Context) // the object's cluster, shown or not (D-69)
	if err != nil {
		return err
	}
	p, ok := b.Profile(ref.Kind)
	if !ok {
		return fmt.Errorf("%w: the cluster does not serve %s", engine.ErrNotApplicable, ref.Kind)
	}
	uid, err := sameAsConfirmed(ctx, s, ref)
	if err != nil {
		return err
	}
	return replaced(ref, w.PatchFlux(ctx, p.GVR(), ref.Namespace, ref.Name, uid, a.request(force)))
}

// fluxActionsOf is the writable check of the Flux commands.
func fluxActionsOf(s *engine.Session, contextID string) (*engine.Backend, engine.FluxActions, error) {
	b, err := writableOn(s, contextID)
	if err != nil {
		return nil, nil, err
	}
	w, ok := b.Writer().(engine.FluxActions)
	if !ok {
		return nil, nil, errors.New("this connection cannot change Flux objects")
	}
	return b, w, nil
}
