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

// orderRemoveFinalizers places Remove Finalizers among an object's actions: after Edit, right
// before Delete, the other action about an object's deletion (D-98).
const orderRemoveFinalizers = (engine.OrderEdit + engine.OrderDelete) / 2

// labelRemoveFinalizers is Remove finalizers' label, one object's and a selection's.
const labelRemoveFinalizers = "Remove Finalizers"

// registerFinalizers registers Remove finalizers (WF43, D-98; the owner's yes, Q1 2026-10-19) and
// its action on a selection (the owner's pick of 2026-11-04).
func registerFinalizers(g *registrar) {
	add(g, engine.Spec[engine.RemoveFinalizersParams]{
		ID: engine.RemoveFinalizersCommand, Label: labelRemoveFinalizers, MenuPath: "Object/Remove Finalizers",
		Order: orderRemoveFinalizers, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Resource: "*"}},
		Description: "Removes the finalizers of one object that is being deleted (it has a deletionTimestamp), so that the API " +
			"server deletes it at once, without the cleanup the finalizers' controllers would have done: what they guard " +
			"outside the cluster (a cloud disk, a load balancer, a DNS record) may stay behind. A Terminating namespace's " +
			"object_get lists the objects that hold it, with this action's params. The confirmation names the finalizers it " +
			"removes (the ones passed, which must be exactly the object's, else the cached object's), and one JSON patch " +
			"tests the object's UID and exactly those finalizers, then removes metadata.finalizers, so a finalizer added or " +
			"removed since is refused, never overwritten. Never a namespace and never /finalize. Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p engine.RemoveFinalizersParams) error {
			return s.FinalizersRemovable(p.Ref, p.Finalizers)
		},
		Targets: func(s *engine.Session, p engine.RemoveFinalizersParams) ([]model.ObjectRef, error) {
			return objectTarget(s, p.Ref)
		},
		Effects: func(s *engine.Session, p engine.RemoveFinalizersParams) []engine.Effect {
			return []engine.Effect{
				{Name: engine.EffectRemovesFinalizers, Value: strings.Join(s.FinalizersShown(p.Ref, p.Finalizers), "\n"), Tone: engine.EffectLost},
			}
		},
		Warning: func(*engine.Session, engine.RemoveFinalizersParams) string { return engine.FinalizersWarning },
		Run: func(ctx context.Context, s *engine.Session, p engine.RemoveFinalizersParams) (any, error) {
			b, err := writableOn(s, p.Ref.Context) // the object's cluster, shown or not (D-69)
			if err != nil {
				return nil, err
			}
			if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
				return nil, err
			}
			// Exactly the finalizers the confirmation showed, which a destructive command always has.
			if fins, ok := engine.ConfirmedFinalizers(ctx); ok {
				p.Finalizers = fins
			}
			res, err := s.RemoveFinalizers(ctx, p)
			if err != nil {
				return nil, err
			}
			userToast(s, engine.LevelInfo, res.Ref.Kind+" "+res.Ref.Key()+": finalizers removed")
			return res, nil
		},
	})
	registerFinalizersMany(g)
}

// registerFinalizersMany registers Remove finalizers on a table's selection: the selection bar's
// button beside Delete and the selection's menu offer it while a selected object is being deleted
// and holds finalizers.
func registerFinalizersMany(g *registrar) {
	add(g, engine.Spec[selectionParams]{
		ID: engine.RemoveFinalizersManyCommand, Label: labelRemoveFinalizers, MenuPath: "Object/Remove Finalizers of Selected",
		Order: orderRemoveFinalizers, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Resource: "*"}},
		Description: "Does what object_removeFinalizers does for several objects at once, as Remove Finalizers on the table's " +
			"selection does: it acts on the selected objects that are being deleted and hold finalizers (never a namespace " +
			"or a custom resource definition) and leaves the others alone. One confirmation names them all, the finalizers " +
			"it removes (every one any of them holds, each once) and how many selected objects it leaves out; then each " +
			"object's finalizers are removed by one JSON patch bound to the UID Cogpit's cache held when the user confirmed " +
			"and to the finalizers the object holds then, which must all be among the confirmed ones, one object after the " +
			"other. At most 500 objects. When one fails the others still go on; nothing is retried. " +
			strings.ReplaceAll(jobManyDescription, "objects", "objects it acts on") + " Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p selectionParams) error {
			_, _, err := finalizerTargets(s, p.Refs)
			return err
		},
		Targets: func(s *engine.Session, p selectionParams) ([]model.ObjectRef, error) {
			targets, _, err := finalizerTargets(s, p.Refs)
			return targets, err
		},
		Effects: func(s *engine.Session, p selectionParams) []engine.Effect {
			targets, left, err := finalizerTargets(s, p.Refs)
			if err != nil {
				return nil
			}
			var held []string
			for _, r := range targets {
				for _, f := range s.FinalizersShown(r, nil) {
					if !slices.Contains(held, f) {
						held = append(held, f)
					}
				}
			}
			out := []engine.Effect{{Name: engine.EffectRemovesFinalizers, Value: strings.Join(held, "\n"), Tone: engine.EffectLost}}
			if left > 0 {
				out = append(out, engine.Effect{Name: "Leaves out", Value: countOf(left, "selected object") + " not being deleted or without finalizers"})
			}
			return out
		},
		Warning: func(*engine.Session, selectionParams) string { return engine.FinalizersWarning },
		Run:     removeFinalizersMany,
	})
}

// RemoveFinalizersManyResult is object.removeFinalizersMany's result: the objects whose finalizers
// it removed.
type RemoveFinalizersManyResult struct {
	Done []model.ObjectRef `json:"done"`
}

// RemoveFinalizersManyError is object.removeFinalizersMany's error when it did not remove every
// object's finalizers: whose it removed and whose not, each with its reason. Nothing is retried.
type RemoveFinalizersManyError struct {
	Done   []model.ObjectRef `json:"done"`
	Failed []DeleteFailure   `json:"failed"`
}

func (e *RemoveFinalizersManyError) Error() string {
	parts := make([]string, 0, len(e.Failed))
	for _, f := range e.Failed {
		parts = append(parts, f.Ref.String()+": "+f.Error)
	}
	return fmt.Sprintf("finalizers removed from %d of %d objects; not removed: %s. Nothing was retried", len(e.Done),
		len(e.Done)+len(e.Failed), strings.Join(parts, "; "))
}

// countOf counts a noun: "1 object", "3 objects".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// finalizerTargets returns the selected objects Remove finalizers applies to, each once, in order,
// and how many it leaves out: those not being deleted or without finalizers, a namespace, and
// one the cache no longer holds (gone: nothing holds it). An error when none applies, or the
// selection's cluster cannot be written.
func finalizerTargets(s *engine.Session, refs []model.ObjectRef) ([]model.ObjectRef, int, error) {
	if len(refs) == 0 {
		return nil, 0, errors.New("no objects are selected")
	}
	if len(refs) > engine.MaxSelection {
		return nil, 0, fmt.Errorf("at most %d objects at once, %d are selected", engine.MaxSelection, len(refs))
	}
	var out []model.ObjectRef
	left := 0
	var writeErr error
	for _, r := range refs {
		err := s.FinalizersRemovable(r, nil)
		switch {
		case errors.Is(err, engine.ErrNotApplicable), errors.Is(err, engine.ErrNotFound):
			left++
			continue
		case err != nil && writeErr == nil:
			writeErr = err // the demo cluster, a connection that cannot remove finalizers: said once the selection applies
			continue
		case err != nil:
			continue
		}
		ref, _, err := s.ResolveRef(r)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", r.String(), err)
		}
		if !slices.ContainsFunc(out, ref.Same) {
			out = append(out, ref)
		}
	}
	if writeErr != nil {
		return nil, 0, writeErr
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("%w: no selected object is being deleted and held by finalizers", engine.ErrNotApplicable)
	}
	return out, left, nil
}

// removeFinalizersMany implements object.removeFinalizersMany: the objects the user confirmed, one
// after the other, each bound to its confirmed UID and to the finalizers it holds then, which must
// all be among the confirmed ones; more than engine.DirectSelection as a job (manyjob.go). A crash
// in the middle leaves some objects without their finalizers, gone, and the others as they were,
// each wholly one or the other (one patch each): the list shows which are left, and Remove
// Finalizers of those finishes what the user asked for (D-11).
func removeFinalizersMany(ctx context.Context, s *engine.Session, p selectionParams) (any, error) {
	b, err := writableOn(s, refsContext(p.Refs)) // the selection's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	targets, _, err := finalizerTargets(s, p.Refs)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, targets); err != nil {
		return nil, err
	}
	confirmed, ok := engine.ConfirmedFinalizers(ctx)
	if !ok {
		return nil, errors.New("removing finalizers needs the user's confirmation")
	}
	a := manyAction{
		id: engine.RemoveFinalizersManyCommand, label: labelRemoveFinalizers, done: "finalizers removed", noun: "objects", b: b, targets: targets,
		do: func(ctx context.Context, ref model.ObjectRef) (model.ObjectRef, error) {
			held := s.FinalizersShown(ref, nil)
			for _, f := range held {
				if !slices.Contains(confirmed, f) {
					return ref, fmt.Errorf("%w: %s holds the finalizer %s now, which was not confirmed; nothing was removed", engine.ErrTargetsChanged, ref.String(), f)
				}
			}
			res, err := s.RemoveFinalizers(ctx, engine.RemoveFinalizersParams{Ref: ref, Finalizers: held})
			if err != nil {
				return ref, err
			}
			return res.Ref, nil
		},
		after: func(done []model.ObjectRef) {
			if len(done) > 0 { // gone with their finalizers: they leave the selection, as deleted objects do
				s.UpdateUI(func(u *engine.UIState) {
					u.Selected = engine.ApplySelection(u.Selected, done, engine.SelectRemove)
				})
			}
		},
	}
	return runMany(ctx, s, a, func(out manyOutcome) (any, error) {
		if len(out.failed) > 0 {
			return nil, &RemoveFinalizersManyError{Done: out.done, Failed: out.failed}
		}
		userToast(s, engine.LevelInfo, fmt.Sprintf("finalizers removed from %s", countOf(len(out.done), "object")))
		return RemoveFinalizersManyResult{Done: out.done}, nil
	})
}
