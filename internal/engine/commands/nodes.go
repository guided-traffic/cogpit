package commands

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Kind keys of the node and workload actions. Their profiles may come from other files or, for
// kinds Cogpit has no profile for yet, not exist: the commands are then disabled.
const (
	nodeKindKey        = "nodes"
	statefulSetKindKey = "statefulsets"
	cronJobKindKey     = "cronjobs"
)

// API verbs and groups of the node and workload actions' Verbs (prefixed: other areas declare
// their own).
const (
	nwVerbCreate = "create"
	nwVerbGet    = "get"
	nwVerbList   = "list"
	nwVerbWatch  = "watch"
	nwVerbPatch  = "patch"
	nwGroupApps  = "apps"
	nwGroupBatch = "batch"
)

// DrainResult is node.drain's result: the drain runs as a background job (drainjob.go), and the
// command returns once it started.
type DrainResult struct {
	Ref    model.ObjectRef `json:"ref"`
	Action string          `json:"action"` // "job started"
	// Job is the job's dock tab as it started: its steps the cordon, then the pods the cache holds
	// on the node (the drain's own list replaces them once it has listed the node's pods). ui_state
	// shows them as they move and the outcome once the job ends (dock[].job); job_cancel stops it.
	Job engine.DockTab `json:"job"`
}

// drainStarted is DrainResult's action.
const drainStarted = "job started"

type drainParams struct {
	Ref model.ObjectRef `json:"ref" jsonschema:"the node: kind nodes and its name"`
	engine.DrainSettings
}

type pendingDrainsParams struct {
	All bool `json:"all,omitempty" jsonschema:"list the pending drains of every context, not only the shown cluster's"`
}

// drainEffects are the confirmation's rows of the drain options that lose data or end pods for
// good, so that "Finish drain" or an agent's drain with them never reads like a plain drain: what
// is lost in the error colour, the other options plain.
func drainEffects(_ *engine.Session, p drainParams) []engine.Effect {
	var out []engine.Effect
	if p.DeleteEmptyDirData {
		out = append(out, engine.Effect{Name: "Loses", Value: "the emptyDir data of evicted pods", Tone: engine.EffectLost})
	}
	if p.Force {
		out = append(out, engine.Effect{Name: "Evicts", Value: "pods no controller manages, not started again", Tone: engine.EffectLost})
	}
	if p.IgnoreDaemonSets != nil && !*p.IgnoreDaemonSets {
		out = append(out, engine.Effect{Name: "DaemonSet pods", Value: "not ignored: a node that runs them is refused"})
	}
	if p.GracePeriodSeconds > 0 {
		out = append(out, engine.Effect{Name: "Grace period", Value: fmt.Sprintf("%d s per pod", p.GracePeriodSeconds)})
	}
	return out
}

// drainVerbs are the requests kubectl's drain library makes, as cluster.DrainNode documents them;
// the discovery GET on /api/v1 is not a resource and is allowed to every authenticated user.
var drainVerbs = []engine.Verb{
	{Verb: nwVerbPatch, Resource: nodeKindKey},
	{Verb: nwVerbList, Resource: kindPods},
	{Verb: nwVerbGet, Resource: kindPods},
	{Verb: nwVerbCreate, Resource: kindPods, Subresource: "eviction"},
	{Verb: nwVerbGet, Group: nwGroupApps, Resource: "daemonsets"},
}

func registerNodes(g *registrar) {
	add(g, engine.Spec[refParams]{
		ID: "node.cordon", Label: "Cordon", MenuPath: "Node/Cordon", Kinds: []string{nodeKindKey},
		Order: 200, Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Resource: nodeKindKey}},
		Description: "Cordons a node as kubectl cordon does: sets spec.unschedulable, so that no new pods are scheduled " +
			"on it; running pods stay. Not offered for a cordoned node, where node_uncordon takes its place.",
		Enabled: func(s *engine.Session, p refParams) error { return nodeToggleEnabled(s, p.Ref, false) },
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return act(ctx, s, p.Ref, "cordoned", func(w engine.Actions, ref model.ObjectRef) error {
				nw, err := asNodeActions(w)
				if err != nil {
					return err
				}
				return nw.SetNodeUnschedulable(ctx, ref.Name, true)
			})
		},
	})

	add(g, engine.Spec[refParams]{
		ID: "node.uncordon", Label: "Uncordon", MenuPath: "Node/Uncordon", Kinds: []string{nodeKindKey},
		Order: 200, Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Resource: nodeKindKey}},
		Description: "Uncordons a cordoned node as kubectl uncordon does: removes spec.unschedulable, so that pods are " +
			"scheduled on it again. Offered only for a cordoned node, in node_cordon's place. Also clears the node's " +
			"unfinished drain from node_pendingDrains.",
		Enabled: func(s *engine.Session, p refParams) error { return nodeToggleEnabled(s, p.Ref, true) },
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			res, err := act(ctx, s, p.Ref, "uncordoned", func(w engine.Actions, ref model.ObjectRef) error {
				nw, err := asNodeActions(w)
				if err != nil {
					return err
				}
				return nw.SetNodeUnschedulable(ctx, ref.Name, false)
			})
			if err != nil {
				return nil, err
			}
			forgetDrainAfterUncordon(s, res.(ActionResult).Ref)
			return res, nil
		},
	})

	add(g, engine.Spec[drainParams]{
		ID: "node.drain", Label: "Drain", MenuPath: "Node/Drain", Kinds: []string{nodeKindKey},
		Order: 300, Danger: engine.DangerDestructive,
		Verbs: drainVerbs,
		Description: "Drains a node as kubectl drain does: cordons it, then evicts every pod on it through the eviction " +
			"API, which honours PodDisruptionBudgets, and waits until they are gone. By default it leaves DaemonSet pods " +
			"running and refuses (before evicting anything) when the node runs pods with emptyDir data or pods no " +
			"controller manages; deleteEmptyDirData and force allow those. Always asks the user to confirm. Runs as a " +
			"background job and returns at once with its dock tab (job): its steps are the cordon, then one per pod, each " +
			"pending, running with what it waits for (a PodDisruptionBudget that allows no disruption is retried), done, " +
			"failed or stopped; ui_state shows them and the outcome once the job ends (dock[].job), and job_cancel " +
			"stops it. Ending your call does not stop it. A drain that fails, times out or is stopped leaves the node " +
			"cordoned and partly drained, and nothing is uncordoned by itself; it stays in node_pendingDrains, and so " +
			"does one a crash of Cogpit interrupts. Run it again to finish it.",
		Effects: drainEffects,
		Enabled: func(s *engine.Session, p drainParams) error {
			if _, err := cachedOfKind(s, p.Ref, nodeKindKey); err != nil {
				return err
			}
			b, _, err := nodeActionsOf(s, p.Ref.Context)
			if err != nil {
				return err
			}
			d := s.Drains()
			if d == nil {
				return engine.ErrNoDrainJournal
			}
			if d.Running(b.Context, p.Ref.Name) {
				return fmt.Errorf("a drain of node %s is running", p.Ref.Name)
			}
			return nil
		},
		Targets: func(s *engine.Session, p drainParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Prefill: func(_ *engine.Session, p drainParams) (drainParams, error) {
			p.DrainSettings = p.WithDefaults()
			return p, nil
		},
		Run: runDrain,
	})

	add(g, engine.Spec[pendingDrainsParams]{
		ID: "node.pendingDrains", Label: "Unfinished Drains", Danger: engine.DangerRead,
		Verbs: []engine.Verb{{Verb: nwVerbList, Resource: nodeKindKey}, {Verb: nwVerbWatch, Resource: nodeKindKey}},
		Description: "Lists the drains Cogpit started on the connected context (all: on every context) that did not " +
			"finish: context, node, when and by whom started, the settings, status (running in this Cogpit, with its " +
			"job's dock tab, or, with elsewhere and its owner's process, in another one; interrupted by an earlier run " +
			"of Cogpit ending; failed with its error; or stopped with why: its job cancelled, its tab closed, the " +
			"connection interrupted) and the node's state from the cache (cordoned, schedulable, missing, unknown). " +
			"Offer node_drain to finish one, node_uncordon to give up and make the node schedulable, or " +
			"node_dismissDrain to forget it.",
		Run: func(_ context.Context, s *engine.Session, p pendingDrainsParams) (any, error) {
			list, err := s.PendingDrains(p.All)
			if err != nil {
				return nil, err
			}
			if list == nil {
				list = []engine.PendingDrainInfo{}
			}
			return list, nil
		},
	})

	add(g, engine.Spec[refParams]{
		ID: "node.dismissDrain", Label: "Dismiss Unfinished Drain", Danger: engine.DangerUI,
		Description: "Forgets an unfinished drain from node_pendingDrains without changing the cluster: the node stays " +
			"as it is (cordoned and partly drained, unless someone changed it). The reference's context defaults to " +
			"the shown one.",
		Enabled: func(s *engine.Session, p refParams) error {
			if s.Drains() == nil {
				return engine.ErrNoDrainJournal
			}
			if p.Ref.Kind != nodeKindKey || p.Ref.Name == "" {
				return errors.New("name a node: kind nodes and its name")
			}
			return nil
		},
		Run: func(_ context.Context, s *engine.Session, p refParams) (any, error) {
			ref := p.Ref
			if ref.Context == "" {
				b := s.Backend()
				if b == nil {
					return nil, engine.ErrNotConnected
				}
				ref.Context = b.Context
			}
			ref.Namespace = ""
			d := s.Drains()
			if _, ok, err := d.Pending(ref.Context, ref.Name); err != nil {
				return nil, err
			} else if !ok {
				return nil, fmt.Errorf("no unfinished drain of node %s in context %s", ref.Name, ref.Context)
			}
			if err := d.Forget(ref.Context, ref.Name); err != nil {
				return nil, err
			}
			return ActionResult{Ref: ref, Action: "dismissed"}, nil
		},
	})
}

// cachedOfKind is the first Enabled check of actions on one object: a reference of another kind
// does not apply at all, and the object must be in the cache.
func cachedOfKind(s *engine.Session, ref model.ObjectRef, kind string) (*unstructured.Unstructured, error) {
	if ref.Kind != kind {
		return nil, fmt.Errorf("%w: this action applies to %s", engine.ErrNotApplicable, kind)
	}
	return s.Lookup(ref)
}

// nodeActionsOf is the writable check of the node commands.
func nodeActionsOf(s *engine.Session, contextID string) (*engine.Backend, engine.NodeActions, error) {
	b, err := writableOn(s, contextID) // the node's cluster, shown or not (D-69)
	if err != nil {
		return nil, nil, err
	}
	w, err := asNodeActions(b.Writer())
	if err != nil {
		return nil, nil, err
	}
	return b, w, nil
}

func asNodeActions(w engine.Actions) (engine.NodeActions, error) {
	nw, ok := w.(engine.NodeActions)
	if !ok {
		return nil, errors.New("this connection cannot change nodes")
	}
	return nw, nil
}

// nodeToggleEnabled is the Enabled check of Cordon (wantCordoned false) and Uncordon (true). Which
// of the two applies comes from the cache first, so that a row menu hides the other one even on a
// cluster Cogpit cannot change; then whether the change can be made.
func nodeToggleEnabled(s *engine.Session, ref model.ObjectRef, wantCordoned bool) error {
	obj, err := cachedOfKind(s, ref, nodeKindKey)
	if err != nil {
		return err
	}
	cordoned, _ := model.NestedBool(obj.Object, "spec", "unschedulable")
	switch {
	case cordoned && !wantCordoned:
		return fmt.Errorf("%w: node %s is cordoned already", engine.ErrNotApplicable, ref.Name)
	case !cordoned && wantCordoned:
		return fmt.Errorf("%w: node %s is not cordoned", engine.ErrNotApplicable, ref.Name)
	}
	b, _, err := nodeActionsOf(s, ref.Context)
	if err != nil {
		return err
	}
	if s.Drains().Running(b.Context, ref.Name) {
		return fmt.Errorf("a drain of node %s is running", ref.Name)
	}
	return nil
}

// runDrain is node.drain's Run: record the drain, then start its job (drainJob.run: drain, record
// the outcome) and return the job's tab. The journal entry is written before the job starts and
// so before the node is cordoned, so that a crash at any later point leaves it behind; a job that
// does not start leaves no entry, since nothing was written to the cluster.
func runDrain(ctx context.Context, s *engine.Session, p drainParams) (any, error) {
	b, w, err := nodeActionsOf(s, p.Ref.Context)
	if err != nil {
		return nil, err
	}
	ref, _, err := s.ResolveRef(p.Ref)
	if err != nil {
		return nil, err
	}
	if ref.Context != b.Context {
		return nil, fmt.Errorf("%w: the shown cluster changed", engine.ErrWrongCluster)
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	d := s.Drains()
	if d == nil {
		return nil, engine.ErrNoDrainJournal
	}
	entry := engine.PendingDrain{
		Context: ref.Context, Node: ref.Name, Started: s.Now().UTC(), Settings: p.WithDefaults(),
	}
	if req, ok := engine.Confirmed(ctx); ok {
		entry.StartedBy = req.Origin
	}
	dj := newDrainJob(s, b, ref, entry.Settings)
	if err := d.Begin(entry); err != nil {
		return nil, err
	}
	opts := p.ClusterOptions()
	// The job keeps the call's values (the origin, the confirmation), not its end: a caller that
	// gives up does not stop the drain. Its own stops (job.cancel, its tab closed, another cluster,
	// an interrupted connection, Cogpit closing) end the drain's context, and the journal keeps the
	// drain as stopped (D-58, S8).
	tab, err := s.StartJob(engine.JobSpec{
		Command: "node.drain", Title: "Drain: node " + ref.Name, Context: b.Context, Parent: ctx,
		Steps: dj.initialSteps(),
		Run: func(jctx context.Context, j *engine.Job) (string, error) {
			return dj.run(jctx, s, j, w, entry, opts)
		},
	})
	if err != nil {
		if endErr := d.End(entry, nil); endErr != nil { // nothing was written to the cluster
			s.Logger().Error("node.drain: forgetting a drain that did not start", "node", ref.Name, "err", endErr)
		}
		return nil, err
	}
	return DrainResult{Ref: ref, Action: drainStarted, Job: tab}, nil
}

// drainLeft says what a drain that did not finish left of the node, from its last step: the node
// as it was when not even the cordon went through; cordoned without an evicted pod when the drain
// refused the node's pods before evicting any (local storage, pods no controller manages, on a
// real cluster 2026-10-16) or every eviction waited; cordoned and partly drained, with the count,
// otherwise.
func drainLeft(cordoned bool, last cluster.DrainProgress) string {
	switch {
	case !cordoned:
		return "is unchanged: the cordon did not go through"
	case last.Done == 0:
		return "stays cordoned; this drain evicted no pod"
	}
	return fmt.Sprintf("stays cordoned and partly drained (%d of %d pods evicted)", last.Done, last.Total)
}

// drainSafely runs the drain and turns a panic into an error: the drain's goroutine boundary
// (D-11). The drain's record is then kept as failed.
func drainSafely(ctx context.Context, s *engine.Session, w engine.NodeActions, node string, opts cluster.DrainOptions,
	progress func(cluster.DrainProgress)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.Logger().Error("node.drain panicked", "node", node, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("drain node %s: internal error: %v", node, r)
		}
	}()
	return w.DrainNode(ctx, node, opts, progress)
}

// forgetDrainAfterUncordon clears the node's pending drain once it is uncordoned: the user gave
// the drain up.
func forgetDrainAfterUncordon(s *engine.Session, ref model.ObjectRef) {
	d := s.Drains()
	if d == nil {
		return
	}
	_, ok, err := d.Pending(ref.Context, ref.Name)
	if err == nil && ok {
		err = d.Forget(ref.Context, ref.Name)
	}
	if err != nil {
		s.Logger().Warn("node.uncordon: the unfinished drain stays recorded", "node", ref.Name, "err", err)
		userToast(s, "warning", "Node "+ref.Name+" is uncordoned, but its unfinished drain stays recorded: "+err.Error())
	}
}

// userToast emits a toast event.
func userToast(s *engine.Session, level, msg string) {
	s.Emit(engine.Event{Type: engine.EventToast, Level: level, Message: msg})
}
