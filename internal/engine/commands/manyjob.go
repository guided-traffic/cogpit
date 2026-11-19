package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Actions on a selection, within the call or as a background job (G4): Delete (object.deleteMany),
// Restart (workload.restartMany), the Flux actions but Reconcile with Source (flux.reconcileMany,
// suspendMany, resumeMany, resetMany; Reconcile with Source is always a job, fluxsourcejob.go) and
// Argo CD's Sync and Refresh (argocd.application.syncMany, refreshMany). Each asks for its
// confirmation as before, in Dispatch, before it runs; then it acts on the objects the user
// confirmed, one after the other, each bound to its confirmed UID.
//
// At most engine.DirectSelection objects (10) run within the call and return what was done and
// what was not, as before. More run as a job (engine.StartJob): the command returns at once with
// the job's dock tab, whose steps are the objects, so the window and agents (ui_state, the tab's
// job) follow each object's progress, and job.cancel or the tab's Cancel button stops what is left.
// A request under way when the job is stopped finishes, so that its step says what it did (D-58's
// request timeouts bound one that would not end); the steps after it are not attempted and say why.
// A failure is that object's: the others go on, and nothing is retried. The job's outcome counts
// what was done, what failed and what was not attempted; a toast says it when the job ends by
// itself.
//
// A crash ends the job like a stop, and what it wrote stays (D-11, jobs.go): each object's action
// is one request (Argo CD's Sync at most argoSyncAttempts of the same write), so no object is left
// half-done, only objects done and objects not attempted, which the lists show; each command's
// comment says why that is harmless for it.

// jobManyDescription tells agents how an action on a selection returns.
var jobManyDescription = fmt.Sprintf("Up to %[1]d objects: returns what was done, or an error that names what was "+
	"done and what was not, each with its reason. More than %[1]d: returns at once with a background job's dock tab "+
	"(job), whose steps are the objects; ui_state shows each step's state and detail, the counts, and the outcome once "+
	"it ends (dock[].job), and job_cancel stops the steps not yet started (a request under way finishes); nothing done "+
	"is undone.", engine.DirectSelection)

// manyAction is an action on a selection's objects, one request each.
type manyAction struct {
	id    string // the command
	label string // its label, the job tab's title before the count: "Delete: 24 objects"
	done  string // what a step did, lower case: "deleted", "restarted", "reconcile requested"
	noun  string // the objects, plural: "objects", "workloads", "Flux objects", "applications"
	// b is the cluster the user confirmed the objects on; a step after it is gone is not attempted.
	b       *engine.Backend
	targets []model.ObjectRef
	// do acts on one object and returns it as acted on.
	do func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error)
	// after runs once, after the last step, with the objects done (Delete: they leave the
	// selection); nil: nothing.
	after func(done []model.ObjectRef)
}

// manyOutcome is what an action on a selection did: the objects done, and those not done, each
// with why; skipped of them were not attempted.
type manyOutcome struct {
	done    []model.ObjectRef
	failed  []DeleteFailure
	skipped int
}

// ManyJobResult is the result of an action on a selection of more than engine.DirectSelection
// objects: the job it started. ui_state shows the job's steps, counts, state and outcome (the dock
// tab's job); job_cancel stops it.
type ManyJobResult struct {
	Action string `json:"action"` // "job started"
	// Job is the job's dock tab as it started, its steps left out: they are Targets, in order, all
	// pending.
	Job     engine.DockTab    `json:"job"`
	Targets []model.ObjectRef `json:"targets"`
}

// runMany runs the action: within the call for at most engine.DirectSelection objects, direct
// turning its outcome into the command's result or error; else as a job.
func runMany(ctx context.Context, s *engine.Session, a manyAction, direct func(manyOutcome) (any, error)) (any, error) {
	if len(a.targets) > engine.DirectSelection {
		return a.startJob(ctx, s)
	}
	out := a.each(ctx, s, nil)
	if a.after != nil {
		a.after(out.done)
	}
	return direct(out)
}

// each acts on the targets in order and, given a job, shows each step's progress. It stops before
// the next object when ctx ends (the call, or the job stopped) or its cluster was disconnected (not
// when the window shows another one, D-69), or when an
// object's error says so; the objects after it are not attempted. In a job, a request under way
// finishes despite a stop.
func (a manyAction) each(ctx context.Context, s *engine.Session, j *engine.Job) manyOutcome {
	var out manyOutcome
	step := func(i int, st engine.JobState, detail string) {
		if j != nil {
			j.SetStep(i, st, detail)
		}
	}
	var stop error
	for i, ref := range a.targets {
		if stop == nil && ctx.Err() != nil {
			stop = context.Cause(ctx)
		}
		if stop == nil && s.BackendOf(a.b.Context) != a.b { // its cluster disconnected; another one shown goes on (D-69)
			stop = fmt.Errorf("%w: its cluster was disconnected", engine.ErrNotConnected)
		}
		if stop != nil {
			out.failed = append(out.failed, DeleteFailure{Ref: ref, Error: fmt.Sprintf("%v: %v", errNotAttempted, stop)})
			out.skipped++
			step(i, engine.JobCancelled, "Not attempted: "+stop.Error())
			continue
		}
		step(i, engine.JobRunning, "")
		rctx := ctx
		if j != nil {
			rctx = context.WithoutCancel(ctx) // the request under way finishes; its step says what it did
		}
		got, err := a.do(rctx, ref)
		if err != nil {
			if errors.Is(err, engine.ErrWrongCluster) || errors.Is(err, engine.ErrNotConnected) {
				stop = err
			}
			out.failed = append(out.failed, DeleteFailure{Ref: ref, Error: err.Error()})
			step(i, engine.JobFailed, upperFirst(err.Error()))
			continue
		}
		out.done = append(out.done, got)
		step(i, engine.JobDone, upperFirst(a.done))
	}
	return out
}

// summary is a job's outcome: "Deleted: 24 of 24 objects.", or an error that counts what failed and
// what was not attempted.
func (a manyAction) summary(out manyOutcome) (string, error) {
	text := fmt.Sprintf("%s: %d of %d %s", upperFirst(a.done), len(out.done), len(a.targets), a.noun)
	if len(out.failed) == 0 {
		return text + ".", nil
	}
	var parts []string
	if n := len(out.failed) - out.skipped; n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", n))
	}
	if out.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d not attempted", out.skipped))
	}
	return "", errors.New(text + "; " + strings.Join(parts, ", ") + ": the job's steps say why")
}

// startJob runs the action as a job with a step per object and returns the job's tab at once.
func (a manyAction) startJob(ctx context.Context, s *engine.Session) (any, error) {
	steps := make([]engine.JobStep, len(a.targets))
	for i, r := range a.targets {
		ref := r
		steps[i] = engine.JobStep{Ref: &ref, Text: stepText(a.b, r)}
	}
	title := fmt.Sprintf("%s: %d %s", a.label, len(a.targets), a.noun)
	tab, err := s.StartJob(engine.JobSpec{
		Command: a.id, Title: title, Context: a.b.Context, Steps: steps, Parent: ctx,
		Run: func(jctx context.Context, j *engine.Job) (string, error) {
			out := a.each(jctx, s, j)
			if a.after != nil {
				a.after(out.done)
			}
			outcome, err := a.summary(out)
			switch {
			case err == nil:
				userToast(s, engine.LevelInfo, outcome)
			case jctx.Err() == nil: // stopped: the tab says why and what was done
				userToast(s, engine.LevelError, err.Error())
			}
			return outcome, err
		},
	})
	if err != nil {
		return nil, err
	}
	if tab.Job != nil { // its steps are the targets, all pending: ui_state shows them as they move
		brief := *tab.Job
		brief.Steps = nil
		tab.Job = &brief
	}
	return ManyJobResult{Action: "job started", Job: tab, Targets: a.targets}, nil
}

// stepText names a step's object: "Pod shop/web-0", "Node worker-1".
func stepText(b *engine.Backend, ref model.ObjectRef) string {
	kind := ref.Kind
	if p, ok := b.Profile(ref.Kind); ok && p.Kind != "" {
		kind = p.Kind
	}
	if ref.Namespace == "" {
		return kind + " " + ref.Name
	}
	return kind + " " + ref.Namespace + "/" + ref.Name
}
