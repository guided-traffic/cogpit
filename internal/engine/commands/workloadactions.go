package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Kind keys of the Restart and Scale commands of this file.
const (
	daemonSetKindKey  = "daemonsets"
	replicaSetKindKey = "replicasets"
)

func registerWorkloadActions(g *registrar) {
	// Lens's documented orders: Daemon Set Restart, Logs; Stateful Set Restart, Scale (200), Logs;
	// Replica Set Scale, Logs.
	add(g, restartSpec("daemonset.restart", "DaemonSet/Restart", daemonSetKindKey, "daemon set"))
	add(g, restartSpec("statefulset.restart", "StatefulSet/Restart", statefulSetKindKey, "stateful set"))

	rs := scaleSpec("replicaset.scale", "ReplicaSet/Scale", replicaSetKindKey, "replica set", 100)
	rs.Description += " A replica set that a Deployment controls is set back to the Deployment's count by its controller."
	run := rs.Run
	rs.Run = func(ctx context.Context, s *engine.Session, p scaleParams) (any, error) {
		res, err := run(ctx, s, p)
		if err == nil {
			if owner := deploymentOf(s, p.Ref); owner != "" {
				userToast(s, engine.LevelWarning, fmt.Sprintf("Replica set %s is controlled by deployment %s, which may set its count back", p.Ref.Name, owner))
			}
		}
		return res, err
	}
	add(g, rs)
}

// restartSpec builds the Restart command of a Daemon Set or Stateful Set: kubectl rollout
// restart's patch, destructive (it replaces every pod), confirmed for every origin (owner,
// 2026-10-11), first in the kind's menu.
func restartSpec(id, menuPath, kind, noun string) engine.Spec[refParams] {
	return engine.Spec[refParams]{
		ID: id, Label: labelRestart, MenuPath: menuPath, Kinds: []string{kind},
		Order: 100, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Group: nwGroupApps, Resource: kind}},
		Description: "Restarts a " + noun + " the way kubectl rollout restart does: it sets the pod template's " +
			"kubectl.kubernetes.io/restartedAt annotation, and the controller replaces every pod by its update strategy " +
			"(with OnDelete, only as pods are deleted). Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p refParams) error {
			if _, err := cachedOfKind(s, p.Ref, kind); err != nil {
				return err
			}
			_, _, err := restartsOf(s, p.Ref.Context)
			return err
		},
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Effects: func(s *engine.Session, p refParams) []engine.Effect {
			obj, err := s.Lookup(p.Ref)
			if err != nil {
				return nil
			}
			if t := model.NestedString(obj, "spec", "updateStrategy", "type"); t == updateStrategyOnDelete {
				return []engine.Effect{{Name: "Update strategy", Value: "OnDelete: the pods are replaced only when deleted", Tone: engine.EffectWarning}}
			}
			return nil
		},
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return act(ctx, s, p.Ref, "restarted", func(w engine.Actions, ref model.ObjectRef) error {
				rw, ok := w.(engine.WorkloadRestarts)
				if !ok {
					return errConnectionCannotRestart
				}
				uid, err := sameAsConfirmed(ctx, s, ref)
				if err != nil {
					return err
				}
				return replaced(ref, rw.RestartWorkload(ctx, kind, ref.Namespace, ref.Name, uid))
			})
		},
	}
}

var errConnectionCannotRestart = errors.New("this connection cannot restart workloads")

// restartsOf is the writable check of the Restart commands.
func restartsOf(s *engine.Session, contextID string) (*engine.Backend, engine.WorkloadRestarts, error) {
	b, err := writableOn(s, contextID) // the object's cluster, shown or not (D-69)
	if err != nil {
		return nil, nil, err
	}
	rw, ok := b.Writer().(engine.WorkloadRestarts)
	if !ok {
		return nil, nil, errConnectionCannotRestart
	}
	return b, rw, nil
}

// sameAsConfirmed returns the UID a patch on ref carries (Restart, Scale): the one the user
// confirmed, else the cache's. It refuses when the cache holds the object under another UID by
// now (deleted and created again under the name while the dialog was open); the patch's UID then
// makes the API server refuse what the cache has not seen yet (cluster.ErrReplaced, mapped by
// replaced). kubectl's patches carry no UID; Cogpit adds it.
func sameAsConfirmed(ctx context.Context, s *engine.Session, ref model.ObjectRef) (string, error) {
	cached := func() (string, bool) {
		obj, err := s.Lookup(ref)
		if err != nil {
			return "", false
		}
		return string(obj.GetUID()), true
	}
	uid, err := engine.ActUID(ctx, ref, cached)
	if err != nil {
		return "", err
	}
	if now, ok := cached(); !ok || now != uid {
		return "", fmt.Errorf("%w: %s is no longer the object that was confirmed; nothing was done", engine.ErrTargetsChanged, ref.String())
	}
	return uid, nil
}

// replaced reports the API server's refusal of a UID-bound patch as ErrTargetsChanged.
func replaced(ref model.ObjectRef, err error) error {
	if errors.Is(err, cluster.ErrReplaced) {
		return fmt.Errorf("%w: %s is no longer the object that was confirmed: %w", engine.ErrTargetsChanged, ref.String(), err)
	}
	return err
}

// deploymentOf returns the name of the Deployment that controls a cached replica set, or "".
func deploymentOf(s *engine.Session, ref model.ObjectRef) string {
	obj, err := s.Lookup(ref)
	if err != nil {
		return ""
	}
	for _, o := range obj.GetOwnerReferences() {
		if o.Controller != nil && *o.Controller && o.Kind == "Deployment" {
			return o.Name
		}
	}
	return ""
}
