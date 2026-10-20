package commands

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TriggerResult is cronjob.trigger's result.
type TriggerResult struct {
	Ref    model.ObjectRef `json:"ref"`
	Action string          `json:"action"` // "triggered"
	Job    model.ObjectRef `json:"job"`    // the created Job (kind key "jobs")
}

// scaleParams are the parameters of the Scale commands. Replicas is a pointer so that leaving it
// out is an error rather than a scale to zero; uint16 gives the schema its bounds, 0 to 65535.
type scaleParams struct {
	Ref      model.ObjectRef `json:"ref" jsonschema:"the workload"`
	Replicas *uint16         `json:"replicas,omitempty" jsonschema:"the desired number of pods, 0 to 65535 (required to run; a dialog starts from the current count). 0 stops every pod and is confirmed like a destructive action"`
}

func registerWorkloads(g *registrar) {
	// Lens's documented order: Deployment Scale, Restart, Logs; Stateful Set Restart, Scale, Logs.
	add(g, scaleSpec("deployment.scale", "Deployment/Scale", kindDeployments, "deployment", 100))
	add(g, scaleSpec("statefulset.scale", "StatefulSet/Scale", statefulSetKindKey, "stateful set", 200))

	add(g, engine.Spec[refParams]{
		ID: "cronjob.trigger", Label: "Trigger", MenuPath: "CronJob/Trigger", Kinds: []string{cronJobKindKey},
		Order: 100, Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: nwVerbCreate, Group: nwGroupBatch, Resource: "jobs"}},
		Description: "Runs a cron job now, as kubectl create job --from=cronjob/<name> does: creates a Job from the cron " +
			"job's job template, with its labels and annotations, the annotation cronjob.kubernetes.io/instantiate=manual " +
			"and the cron job as its controller, named <cronjob>-manual-<5 random characters> (within 63 characters). " +
			"Works on a suspended cron job too. Returns the Job's reference.",
		Enabled: func(s *engine.Session, p refParams) error {
			if _, err := cachedOfKind(s, p.Ref, cronJobKindKey); err != nil {
				return err
			}
			_, _, err := workloadActionsOf(s, p.Ref.Context)
			return err
		},
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			b, w, err := workloadActionsOf(s, p.Ref.Context)
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
			// The cached cron job is the one the user sees: its job template becomes the Job.
			cj, err := s.Object(ctx, ref)
			if err != nil {
				return nil, err
			}
			job, err := w.TriggerCronJob(ctx, cj)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", ref.String(), err)
			}
			jobRef := model.ObjectRef{Context: ref.Context, Kind: "jobs", Namespace: ref.Namespace, Name: job}
			userToast(s, "info", fmt.Sprintf("Job %s/%s created from cron job %s", ref.Namespace, job, ref.Name))
			return TriggerResult{Ref: ref, Action: "triggered", Job: jobRef}, nil
		},
	})

	add(g, suspendSpec(true))
	add(g, suspendSpec(false))

	add(g, engine.Spec[refParams]{
		ID: "pod.evict", Label: "Evict", MenuPath: "Pod/Evict", Kinds: []string{kindPods},
		Order: 300, Danger: engine.DangerDestructive,
		Verbs: []engine.Verb{{Verb: nwVerbCreate, Resource: kindPods, Subresource: "eviction"}},
		Description: "Evicts a pod through the eviction API, which honours PodDisruptionBudgets: the pod is deleted with " +
			"its grace period and its controller, if any, starts a replacement. When a budget allows no disruption now, " +
			"it fails at once with an error naming the budget; it does not retry. Always asks the user to confirm.",
		Enabled: func(s *engine.Session, p refParams) error {
			if _, err := cachedOfKind(s, p.Ref, kindPods); err != nil {
				return err
			}
			_, _, err := workloadActionsOf(s, p.Ref.Context)
			return err
		},
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return act(ctx, s, p.Ref, "evicted", func(w engine.Actions, ref model.ObjectRef) error {
				ww, err := asWorkloadActions(w)
				if err != nil {
					return err
				}
				// The UID the user confirmed, so that a pod created again under the name is not evicted.
				uid, err := engine.ActUID(ctx, ref, func() (string, bool) {
					obj, err := s.Lookup(ref)
					if err != nil {
						return "", false
					}
					return string(obj.GetUID()), true
				})
				if err != nil {
					return err
				}
				return ww.EvictPod(ctx, ref.Namespace, ref.Name, uid)
			})
		},
	})
}

// scaleSliderMax is the top of the Scale dialog's slider; a higher count is typed (the owner's
// GW2: "Slider von 1 bis 30, höhere Zahlen eintippen").
const scaleSliderMax = 30

// scaleSpec builds the Scale command of a workload kind.
func scaleSpec(id, menuPath, kind, noun string, order int) engine.Spec[scaleParams] {
	return engine.Spec[scaleParams]{
		ID: id, Label: "Scale", MenuPath: menuPath, Kinds: []string{kind},
		Order: order, Danger: engine.DangerMutating,
		Verbs: []engine.Verb{{Verb: nwVerbPatch, Group: nwGroupApps, Resource: kind, Subresource: "scale"}},
		Description: "Sets a " + noun + "'s replica count, as kubectl scale does: a patch of spec.replicas on its scale " +
			"subresource. Scaling to 0 stops every pod and is confirmed like a destructive action, for every origin. " +
			"A HorizontalPodAutoscaler, if one manages the " + noun + ", may change the count again.",
		Enabled: func(s *engine.Session, p scaleParams) error {
			if _, err := cachedOfKind(s, p.Ref, kind); err != nil {
				return err
			}
			_, _, err := workloadActionsOf(s, p.Ref.Context)
			return err
		},
		Escalate: func(_ *engine.Session, p scaleParams) bool { return p.Replicas != nil && *p.Replicas == 0 },
		// The dialog: a slider from 1 to 30, a higher count typed (the owner's GW2, 2026-10-20), 0 only
		// typed; the hint a person reads in place of the schema's words for agents (D-104).
		Dialog: map[string]engine.DialogField{"replicas": {
			Hint:      "The number of pods. Drag up to 30 or type up to 65,535; 0 stops every pod and is confirmed first.",
			SliderMin: 1, SliderMax: scaleSliderMax,
		}},
		Prefill: func(s *engine.Session, p scaleParams) (scaleParams, error) {
			if p.Replicas != nil {
				return p, nil
			}
			obj, err := cachedOfKind(s, p.Ref, kind)
			if err != nil {
				return p, err
			}
			n := uint16(1) // the API server's default when spec.replicas is not set
			if v, ok := model.NestedInt64(obj.Object, "spec", "replicas"); ok {
				n = uint16(min(max(v, 0), math.MaxUint16))
			}
			p.Replicas = &n
			return p, nil
		},
		Targets: func(s *engine.Session, p scaleParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p scaleParams) (any, error) {
			if p.Replicas == nil {
				return nil, errors.New("say how many replicas: replicas is required")
			}
			n := *p.Replicas
			return act(ctx, s, p.Ref, fmt.Sprintf("scaled to %d", n), func(w engine.Actions, ref model.ObjectRef) error {
				ww, err := asWorkloadActions(w)
				if err != nil {
					return err
				}
				// Only the object the user saw: confirmed (to zero, Escalate; an agent's accepted
				// scale) or, unconfirmed, the cache's.
				uid, err := sameAsConfirmed(ctx, s, ref)
				if err != nil {
					return err
				}
				return replaced(ref, ww.ScaleWorkload(ctx, kind, ref.Namespace, ref.Name, uid, int32(n)))
			})
		},
	}
}

// suspendSpec builds Suspend (suspend true) or Resume (false) of cron jobs. Resume takes Suspend's
// place on a suspended cron job: each is not applicable where the other is.
func suspendSpec(suspend bool) engine.Spec[refParams] {
	id, label, done := "cronjob.resume", "Resume", "resumed"
	desc := "Resumes a suspended cron job: sets spec.suspend to false, so that it runs on its schedule again. Offered " +
		"only for a suspended cron job, in cronjob_suspend's place."
	if suspend {
		id, label, done = "cronjob.suspend", "Suspend", "suspended"
		desc = "Suspends a cron job: sets spec.suspend to true, so that it starts no Jobs on its schedule; running Jobs " +
			"go on. Not offered for a suspended cron job, where cronjob_resume takes its place."
	}
	return engine.Spec[refParams]{
		ID: id, Label: label, MenuPath: "CronJob/" + label, Kinds: []string{cronJobKindKey},
		Order: 200, Danger: engine.DangerMutating,
		Verbs:       []engine.Verb{{Verb: nwVerbPatch, Group: nwGroupBatch, Resource: cronJobKindKey}},
		Description: desc,
		Enabled: func(s *engine.Session, p refParams) error {
			obj, err := cachedOfKind(s, p.Ref, cronJobKindKey)
			if err != nil {
				return err
			}
			suspended, _ := model.NestedBool(obj.Object, "spec", "suspend")
			switch {
			case suspended && suspend:
				return fmt.Errorf("%w: cron job %s is suspended already", engine.ErrNotApplicable, p.Ref.Name)
			case !suspended && !suspend:
				return fmt.Errorf("%w: cron job %s is not suspended", engine.ErrNotApplicable, p.Ref.Name)
			}
			_, _, err = workloadActionsOf(s, p.Ref.Context)
			return err
		},
		Targets: func(s *engine.Session, p refParams) ([]model.ObjectRef, error) { return targetOf(s, p.Ref) },
		Run: func(ctx context.Context, s *engine.Session, p refParams) (any, error) {
			return act(ctx, s, p.Ref, done, func(w engine.Actions, ref model.ObjectRef) error {
				ww, err := asWorkloadActions(w)
				if err != nil {
					return err
				}
				return ww.SetCronJobSuspend(ctx, ref.Namespace, ref.Name, suspend)
			})
		},
	}
}

// workloadActionsOf is the writable check of the workload commands.
func workloadActionsOf(s *engine.Session, contextID string) (*engine.Backend, engine.WorkloadActions, error) {
	b, err := writableOn(s, contextID) // the object's cluster, shown or not (D-69)
	if err != nil {
		return nil, nil, err
	}
	w, err := asWorkloadActions(b.Writer())
	if err != nil {
		return nil, nil, err
	}
	return b, w, nil
}

func asWorkloadActions(w engine.Actions) (engine.WorkloadActions, error) {
	ww, ok := w.(engine.WorkloadActions)
	if !ok {
		return nil, errors.New("this connection cannot change workloads")
	}
	return ww, nil
}
