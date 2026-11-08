package commands

import (
	"context"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// ActionResult is the result of an action on one object.
type ActionResult struct {
	Ref    model.ObjectRef `json:"ref"`
	Action string          `json:"action"` // "restarted", "deleted"
}

func registerActions(g *registrar) {
	add(g, engine.Spec[refParams]{
		ID: "deployment.restart", Label: labelRestart, MenuPath: "Deployment/Restart", Kinds: []string{kindDeployments},
		Order: 200, RowKeys: []string{"r"}, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: "patch", Group: "apps", Resource: kindDeployments}},
		Description: "Restarts a Deployment the way kubectl rollout restart does: it sets the pod template's restartedAt " +
			"annotation and the controller replaces every pod. Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p refParams) error {
			if _, err := writableOn(s, p.Ref.Context); err != nil {
				return err
			}
			return cachedTarget(s, p.Ref, kindDeployments, "restart applies to deployments")
		},
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return act(ctx, s, p.Ref, "restarted", func(w engine.Actions, ref model.ObjectRef) error {
				uid, err := sameAsConfirmed(ctx, s, ref)
				if err != nil {
					return err
				}
				return replaced(ref, w.RestartDeployment(ctx, ref.Namespace, ref.Name, uid))
			})
		},
	})

	add(g, engine.Spec[refParams]{
		ID: "object.delete", Label: labelDelete, MenuPath: "Object/Delete", Order: engine.OrderDelete, RowKeys: deleteKeys, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: "delete", Resource: "*"}},
		Description: "Deletes an object of any kind with its default grace period, with the UID Cogpit's cache holds for it " +
			"as a precondition, so that an object created again under the same name is not deleted; its dependents are " +
			"garbage-collected, and its controller, if any, may replace it. Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p refParams) error { return cachedAny(s, p.Ref) },
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return objectTarget(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			b, err := writableOn(s, p.Ref.Context) // the object's cluster, shown or not (D-69)
			if err != nil {
				return nil, err
			}
			if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
				return nil, err
			}
			res, err := s.DeleteObject(ctx, p.Ref)
			if err != nil {
				return nil, err
			}
			s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: res.Ref.Kind + " " + res.Ref.Key() + " deleted"})
			return ActionResult{Ref: res.Ref, Action: "deleted"}, nil
		},
	})
}

// act runs one write on one object of its connected cluster, after checking that the user
// confirmed it for this cluster, and reports it as a toast.
func act(ctx context.Context, s *engine.Session, ref model.ObjectRef, done string, write func(engine.Actions, model.ObjectRef) error) (any, error) {
	b, err := writableOn(s, ref.Context) // the object's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	ref, _, err = s.ResolveRef(ref)
	if err != nil {
		return nil, err
	}
	if ref.Context != b.Context {
		return nil, fmt.Errorf("%w: the shown cluster changed", engine.ErrWrongCluster)
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	if err := write(b.Writer(), ref); err != nil {
		return nil, fmt.Errorf("%s: %w", ref.String(), err)
	}
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: ref.Kind + " " + ref.Key() + " " + done})
	return ActionResult{Ref: ref, Action: done}, nil
}
