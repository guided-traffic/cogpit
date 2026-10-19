package commands

import (
	"context"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// orderRemoveFinalizers places Remove Finalizers among an object's actions: after Edit, right
// before Delete, the other action about an object's deletion (D-98).
const orderRemoveFinalizers = (engine.OrderEdit + engine.OrderDelete) / 2

// registerFinalizers registers Remove finalizers (WF43, D-98; the owner's yes, Q1 2026-10-19).
func registerFinalizers(g *registrar) {
	add(g, engine.Spec[engine.RemoveFinalizersParams]{
		ID: engine.RemoveFinalizersCommand, Label: "Remove Finalizers", MenuPath: "Object/Remove Finalizers",
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
				{Name: "Skips", Value: "the cleanup of the controllers that own them (a cloud disk or a DNS record may stay)", Tone: engine.EffectWarning},
			}
		},
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
}
