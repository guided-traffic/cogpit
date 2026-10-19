package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Restart of a selection of workloads (A74, owner 2026-10-14 evening): the Deployments, Daemon
// Sets and Stateful Sets of a table's selection restarted as kubectl rollout restart does, one
// confirmation for every origin naming them all (destructive, as one Restart is), then each in
// turn, bound to the UID the user confirmed. Scale stays on one object.

// labelRestart is Restart's label, one workload's and a selection's.
const labelRestart = "Restart"

// restartKinds are the kinds Restart applies to.
var restartKinds = []string{kindDeployments, daemonSetKindKey, statefulSetKindKey}

// RestartManyResult is workload.restartMany's result: the workloads restarted.
type RestartManyResult struct {
	Restarted []model.ObjectRef `json:"restarted"`
}

// RestartManyError is workload.restartMany's error when it did not restart every workload: which
// it restarted and which not, each with its reason. Nothing is retried.
type RestartManyError struct {
	Restarted []model.ObjectRef `json:"restarted"`
	Failed    []DeleteFailure   `json:"failed"`
}

func (e *RestartManyError) Error() string {
	parts := make([]string, 0, len(e.Failed))
	for _, f := range e.Failed {
		parts = append(parts, f.Ref.String()+": "+f.Error)
	}
	return fmt.Sprintf("%d of %d workloads restarted; not restarted: %s", len(e.Restarted), len(e.Restarted)+len(e.Failed),
		strings.Join(parts, "; "))
}

func registerRestartMany(g *registrar) {
	add(g, engine.Spec[selectionParams]{
		ID: "workload.restartMany", Label: labelRestart, MenuPath: "Workload/Restart Selected", Kinds: restartKinds, Order: 100,
		Danger: engine.DangerDestructive,
		Verbs:  []engine.Verb{{Verb: nwVerbPatch, Group: nwGroupApps, Resource: "*"}},
		Description: "Restarts several Deployments, Daemon Sets and Stateful Sets at once, as Restart on the table's selection " +
			"does: one confirmation names them all, for every origin; then each is restarted the way kubectl rollout restart " +
			"does (the pod template's restartedAt annotation), bound to the UID Cogpit's cache held when the user confirmed, " +
			"one after the other. At most 500. When one is not restarted the others still are; nothing is retried. " +
			jobManyDescription,
		Enabled: func(s *engine.Session, p selectionParams) error {
			if len(p.Refs) == 0 {
				return errors.New("no workloads are selected")
			}
			if len(p.Refs) > engine.MaxSelection {
				return fmt.Errorf("at most %d workloads at once, %d are selected", engine.MaxSelection, len(p.Refs))
			}
			for _, r := range p.Refs {
				if !slices.Contains(restartKinds, r.Kind) {
					return fmt.Errorf("%w: %s is no Deployment, Daemon Set or Stateful Set", engine.ErrNotApplicable, r.String())
				}
				if _, err := cachedOfKind(s, r, r.Kind); err != nil {
					return fmt.Errorf("%s: %w", r.String(), err)
				}
			}
			_, _, err := restartsOf(s, refsContext(p.Refs))
			return err
		},
		Targets: func(s *engine.Session, p selectionParams) ([]model.ObjectRef, error) {
			return selectionTargets(s, p.Refs)
		},
		Effects: func(s *engine.Session, p selectionParams) []engine.Effect {
			onDelete := 0
			for _, r := range p.Refs {
				if obj, err := s.Lookup(r); err == nil && model.NestedString(obj, "spec", "updateStrategy", "type") == updateStrategyOnDelete {
					onDelete++
				}
			}
			out := []engine.Effect{{Name: "Replaces", Value: "every pod of each, by its update strategy"}}
			if onDelete > 0 {
				out = append(out, engine.Effect{Name: updateStrategyOnDelete, Value: fmt.Sprintf("%d of them: their pods are replaced only when deleted", onDelete),
					Tone: engine.EffectWarning})
			}
			return out
		},
		Run: restartMany,
	})
}

// restartMany implements workload.restartMany: the workloads the user confirmed, one after the
// other, each bound to its confirmed UID; more than engine.DirectSelection as a job (manyjob.go).
// A crash in the middle leaves some workloads restarted and the others not, each one patch of its
// pod template's restartedAt annotation, which its controller rolls out whether Cogpit runs or
// not: harmless, as Restart of those not restarted finishes what the user asked for (D-11).
func restartMany(ctx context.Context, s *engine.Session, p selectionParams) (any, error) {
	b, rw, err := restartsOf(s, refsContext(p.Refs))
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	targets, err := selectionTargets(s, p.Refs)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, targets); err != nil {
		return nil, err
	}
	a := manyAction{
		id: "workload.restartMany", label: labelRestart, done: "restarted", noun: "workloads", b: b, targets: targets,
		do: func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error) {
			uid, err := sameAsConfirmed(ctx, s, ref)
			if err == nil {
				err = replaced(ref, rw.RestartWorkload(ctx, ref.Kind, ref.Namespace, ref.Name, uid))
			}
			return ref, err
		},
	}
	return runMany(ctx, s, a, func(out manyOutcome) (any, error) {
		if len(out.failed) > 0 {
			return nil, &RestartManyError{Restarted: out.done, Failed: out.failed}
		}
		userToast(s, engine.LevelInfo, fmt.Sprintf("%d workloads restarted", len(out.done)))
		return RestartManyResult{Restarted: out.done}, nil
	})
}
