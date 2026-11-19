package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Reconcile with Source waits as the flux CLI does (A91, the owner's decision; D-33): `flux
// reconcile kustomization|helmrelease --with-source` asks the source's controller to reconcile it,
// waits until the controller has handled the request and the source is ready (at most --timeout, 5
// minutes by default), and only then asks for the object, and waits for it too. Cogpit runs that as
// a background job (engine/jobs.go) with a dock tab of its own: the command checks everything, as
// before, and returns the job's tab at once; the job writes each reconcile request in its chain's
// order, the deepest source first, and waits for each through the cache
// (Session.WaitFluxReconciled, no polling), at most fluxJobWait per object. A source that ends not
// ready, times out or fails stops what depends on it: its object is not asked to reconcile, as the
// flux CLI stops. On a selection the objects' chains run side by side, a source two objects share
// is asked once, and one that fails fails only the objects that need it.
//
// Its steps (one per object written, the sources first) and its outcome are the tab's engine state,
// which agents read with ui_state; a toast says how it ended. Closing the tab stops the job; what
// it wrote stays. A crash ends it as well and leaves at most sources asked to reconcile whose
// objects were not asked yet: harmless, since the source's controller only fetches the source
// again, and kustomize-controller and helm-controller reconcile the object at their next interval
// or when the source's artifact changes (Flux's documentation), as if nobody had asked. So the job
// keeps no journal (D-11).

// fluxJobWait bounds the wait for each object's controller: the flux CLI's --timeout default.
var fluxJobWait = 5 * time.Minute

// FluxSourceJobResult is Reconcile with Source's result: the job it started, which writes the
// targets in order, and the selected objects it leaves out, with why.
type FluxSourceJobResult struct {
	Action string `json:"action"` // "reconcile with source started"
	// Job is the job's dock tab; ui_state shows its steps, state and outcome (dock[].job).
	Job     engine.DockTab    `json:"job"`
	Targets []model.ObjectRef `json:"targets"`
	Skipped []DeleteFailure   `json:"skipped,omitempty"`
}

// fluxJobNode is one object the job writes and waits for.
type fluxJobNode struct {
	ref  model.ObjectRef
	text string // its step's text: "GitRepository flux-system/podinfo"
	uid  string // the object the user confirmed (or the cache's): each request is bound to it
	dep  int    // the node it waits for, its source in the chain; -1: none
	step int    // its step in the job's tab
	// state and why are its outcome: JobDone, or JobFailed with the reason that fails what depends
	// on it. Only the job's goroutine and the one waiting for this node write them, one after the
	// other.
	state engine.JobState
	why   string
}

// fluxStartSourceJob starts the job of Reconcile with Source over the plans (each object's chain,
// or why it cannot be written), each object bound to the UID the user confirmed. It returns the
// job's tab, or an error with nothing started.
func fluxStartSourceJob(ctx context.Context, s *engine.Session, a fluxAction, id, title string, plans []fluxSourcePlan) (any, error) {
	contextID := ""
	if len(plans) > 0 {
		contextID = plans[0].ref.Context // the objects' cluster, shown or not (D-69)
	}
	b, w, err := fluxActionsOf(s, contextID)
	if err != nil {
		return nil, err
	}
	var (
		nodes   []fluxJobNode
		steps   []engine.JobStep
		targets []model.ObjectRef
		skipped []DeleteFailure
		index   = map[model.ObjectRef]int{}
	)
	for _, pl := range plans {
		if pl.err != nil {
			r := pl.ref
			skipped = append(skipped, DeleteFailure{Ref: r, Error: pl.err.Error()})
			steps = append(steps, engine.JobStep{Ref: &r, Text: fluxStepText(b, r), State: engine.JobFailed, Detail: "Not reconciled: " + pl.err.Error()})
			continue
		}
		dep := -1
		for _, r := range pl.chain {
			if i, ok := index[r]; ok {
				dep = i
				continue
			}
			uid, err := sameAsConfirmed(ctx, s, r)
			if err != nil {
				return nil, err // the confirmed object replaced: nothing started
			}
			index[r] = len(nodes)
			ref, text := r, fluxStepText(b, r)
			nodes = append(nodes, fluxJobNode{ref: r, text: text, uid: uid, dep: dep, step: len(steps)})
			steps = append(steps, engine.JobStep{Ref: &ref, Text: text})
			targets = append(targets, r)
			dep = index[r]
		}
	}
	if len(nodes) == 0 {
		return nil, &FluxManyError{Action: a.done, Failed: skipped}
	}
	tab, err := s.StartJob(engine.JobSpec{
		Command: id, Title: title, Context: b.Context, Steps: steps, Parent: ctx,
		Run: func(jctx context.Context, j *engine.Job) (string, error) {
			outcome, err := runFluxSourceJob(jctx, s, j, nodes, func(ctx context.Context, n *fluxJobNode) error {
				p, ok := b.Profile(n.ref.Kind)
				if !ok {
					return fmt.Errorf("%w: the cluster does not serve %s", engine.ErrNotApplicable, n.ref.Kind)
				}
				return replaced(n.ref, w.PatchFlux(ctx, p.GVR(), n.ref.Namespace, n.ref.Name, n.uid, a.request(false)))
			}, len(skipped))
			switch {
			case jctx.Err() != nil: // stopped: the tab says why, if it is still open
			case err != nil:
				userToast(s, engine.LevelError, title+": "+err.Error())
			default:
				userToast(s, engine.LevelInfo, title+": "+outcome)
			}
			return outcome, err
		},
	})
	if err != nil {
		return nil, err
	}
	return FluxSourceJobResult{Action: "reconcile with source started", Job: tab, Targets: targets, Skipped: skipped}, nil
}

// fluxStepText names a step's object: "GitRepository flux-system/podinfo".
func fluxStepText(b *engine.Backend, ref model.ObjectRef) string { return stepText(b, ref) }

// fluxJobEnd is how the wait for one node ended.
type fluxJobEnd struct {
	i     int
	state engine.JobState
	why   string
}

// runFluxSourceJob is the job's work. It asks every object without a source, or whose source is
// ready, to reconcile, one request after the other, and waits for all of them side by side; as each
// wait ends, it asks the objects that depend on that one, or fails them when it failed, so that one
// chain never waits for another. skipped counts the selected objects left out before it started.
// Only this goroutine writes the nodes' states.
func runFluxSourceJob(ctx context.Context, s *engine.Session, j *engine.Job, nodes []fluxJobNode, patch func(context.Context, *fluxJobNode) error, skipped int) (string, error) {
	ends := make(chan fluxJobEnd, len(nodes)) // a wait never blocks on a job that stopped
	waiting := 0
	ask := func(i int) {
		n := &nodes[i]
		n.state = engine.JobRunning
		prev := ""
		if obj, err := s.Lookup(n.ref); err == nil {
			prev = engine.LastHandledReconcile(obj)
		}
		since := s.Now()
		if err := patch(ctx, n); err != nil {
			n.state, n.why = engine.JobFailed, "could not be asked to reconcile: "+err.Error()
			j.SetStep(n.step, engine.JobFailed, upperFirst(n.why))
			return
		}
		j.SetStep(n.step, engine.JobRunning, fmt.Sprintf("Reconcile requested; waiting for its controller (at most %s)", fluxJobWait))
		waiting++
		node := *n
		go func() {
			state, why := fluxJobWaitFor(ctx, s, j, node, since, prev)
			ends <- fluxJobEnd{i: i, state: state, why: why}
		}()
	}
	for {
		for fluxJobNext(ctx, j, nodes, ask) {
		}
		if waiting == 0 {
			break
		}
		select {
		case e := <-ends:
			waiting--
			nodes[e.i].state, nodes[e.i].why = e.state, e.why
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fluxJobOutcome(nodes, skipped)
}

// fluxJobNext moves the nodes that can move: one whose source failed fails, saying which; one
// without a source, or whose source is ready, is asked. It reports whether any moved.
func fluxJobNext(ctx context.Context, j *engine.Job, nodes []fluxJobNode, ask func(int)) bool {
	moved := false
	for i := range nodes {
		n := &nodes[i]
		if n.state != "" || ctx.Err() != nil {
			continue
		}
		if n.dep >= 0 {
			switch src := nodes[n.dep]; src.state {
			case engine.JobFailed:
				n.state, n.why = engine.JobFailed, "was not asked to reconcile either"
				j.SetStep(n.step, engine.JobFailed, "Not asked to reconcile: its source "+src.text+" "+src.why)
				moved = true
				continue
			case engine.JobDone:
			default:
				continue // its source is still under way
			}
		}
		ask(i)
		moved = true
	}
	return moved
}

// fluxJobWaitFor waits for one node's controller, shows how it ended in its step, and returns that
// as the node's state with the reason that fails what depends on it. A panic fails the node.
func fluxJobWaitFor(ctx context.Context, s *engine.Session, j *engine.Job, n fluxJobNode, since time.Time, prev string) (state engine.JobState, why string) {
	defer func() {
		if r := recover(); r != nil {
			s.Logger().Error("flux.reconcileWithSource: waiting failed", "ref", n.ref.String(), "panic", fmt.Sprint(r))
			state, why = engine.JobFailed, "was not waited for: an internal error ended the wait"
			j.SetStep(n.step, engine.JobFailed, "An internal error ended the wait.")
		}
	}()
	wctx, cancel := context.WithTimeout(ctx, fluxJobWait)
	defer cancel()
	res, err := s.WaitFluxReconciled(wctx, n.ref, n.uid, since, prev, func(state string) {
		j.SetStep(n.step, engine.JobRunning, "Waiting for its controller: "+state)
	})
	switch {
	case err != nil && ctx.Err() != nil:
		return engine.JobCancelled, "was not waited for" // the job was stopped: its tab says why
	case errors.Is(err, context.DeadlineExceeded):
		last := ""
		if st, ok := j.Step(n.step); ok {
			last = strings.TrimPrefix(st.Detail, "Waiting for its controller: ")
		}
		j.SetStep(n.step, engine.JobFailed, fmt.Sprintf("Its controller did not finish within %s; last seen: %s", fluxJobWait, last))
		return engine.JobFailed, fmt.Sprintf("was not ready within %s", fluxJobWait)
	case err != nil:
		j.SetStep(n.step, engine.JobFailed, upperFirst(err.Error()))
		return engine.JobFailed, "failed: " + err.Error()
	case !res.Ready:
		j.SetStep(n.step, engine.JobFailed, "Not ready: "+res.Message)
		return engine.JobFailed, "is not ready: " + res.Message
	}
	j.SetStep(n.step, engine.JobDone, "Ready: "+res.Message)
	return engine.JobDone, ""
}

// fluxJobOutcome sums the job up once every node ended: done when every object is ready, else an
// error that counts what was not.
func fluxJobOutcome(nodes []fluxJobNode, skipped int) (string, error) {
	done, failed := 0, skipped
	for _, n := range nodes {
		if n.state == engine.JobDone {
			done++
		} else {
			failed++
		}
	}
	if failed > 0 {
		return "", fmt.Errorf("%d of %d objects reconciled and ready; the steps say why the others are not", done, done+failed)
	}
	return fmt.Sprintf("Reconciled with its source: %d objects ready.", done), nil
}

// upperFirst writes a reason as a sentence: its first letter upper case.
func upperFirst(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
