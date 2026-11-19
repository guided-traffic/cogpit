package commands

import (
	"context"
	"fmt"
	"slices"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Reconcile with Source (D-33, owner), as `flux reconcile kustomization|helmrelease --with-source`
// does it: a reconcile request on the object's source first, then on the object (kinds.FluxSourceOf
// names the source). A Helm release's HelmChart is reconciled with the chart's own source before
// it, as the flux CLI does; an OCI Helm repository is skipped, as the flux CLI skips it (no
// controller reconciles it). Each patch is reconciliation's annotation alone, bound to its object's
// UID like Reconcile's.
//
// Since A91 (the owner's decision) Cogpit waits as the flux CLI does: a background job asks each
// object in turn and waits for its controller before the next (fluxsourcejob.go). Every object is
// checked before the job starts: a suspended source, or one missing from the cluster, refuses the
// whole action with nothing done, where the flux CLI would stop after the sources it had already
// annotated.

// fluxSourceChainLen bounds the walk from an object to its sources: a Helm release's HelmChart,
// then the chart's source.
const fluxSourceChainLen = 2

// fluxSourceChain returns what Reconcile with Source writes for ref, in order: its sources, the
// deepest first, then ref itself (resolved, with the connected context). It reads the sources from
// the cache, waiting for their kinds' initial lists (bounded by ctx and 15 seconds), and refuses an
// object or source that is suspended or missing.
func fluxSourceChain(ctx context.Context, s *engine.Session, ref model.ObjectRef) ([]model.ObjectRef, error) {
	ref, _, err := s.ResolveRef(ref)
	if err != nil {
		return nil, err
	}
	obj, err := s.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	if kinds.IsFluxSuspended(obj) {
		return nil, fmt.Errorf("%s is suspended: resume it to reconcile it", ref.String())
	}
	b := s.BackendOf(ref.Context) // the object's cluster, shown or not (D-69)
	if b == nil {
		return nil, &engine.NotConnectedError{Context: ref.Context}
	}
	var sources []model.ObjectRef
	key, cur := ref.Kind, obj
	for range fluxSourceChainLen {
		src, err := kinds.FluxSourceOf(key, cur)
		if err != nil {
			return nil, err
		}
		src.Context = ref.Context
		if _, ok := b.Profile(src.Kind); !ok {
			return nil, fmt.Errorf("the cluster does not serve %s, the source of %s", src.Kind, ref.Name)
		}
		o, err := s.Object(ctx, src)
		if err != nil {
			return nil, fmt.Errorf("its source %s: %w", src.String(), err)
		}
		if kinds.IsFluxSuspended(o) {
			return nil, fmt.Errorf("its source %s is suspended: resume it to reconcile with it", src.String())
		}
		if !kinds.FluxSourceStatic(src.Kind, o) {
			sources = append(sources, src)
		}
		if !kinds.FluxSourceHasSource(src.Kind) {
			break
		}
		key, cur = src.Kind, o
	}
	slices.Reverse(sources)
	return append(sources, ref), nil
}

// fluxSourceTargets is Reconcile with Source's Targets: the sources and the object, for the
// confirmation that names them (an agent's, D-2).
func fluxSourceTargets(s *engine.Session, ref model.ObjectRef) ([]model.ObjectRef, error) {
	return fluxSourceChain(context.Background(), s, ref)
}

// fluxSourcePermitted greys Reconcile with Source when the access reviews say the user may not
// patch the object's direct source (the object's own patch is the command's declared verb). A
// review not answered yet lets the API server decide, as for every command.
func fluxSourcePermitted(s *engine.Session, a fluxAction, ref model.ObjectRef) error {
	if !a.withSource {
		return nil
	}
	b := s.BackendOf(ref.Context) // the object's cluster, shown or not (D-69)
	obj, err := s.Lookup(ref)
	if b == nil || err != nil {
		return err
	}
	src, err := kinds.FluxSourceOf(ref.Kind, obj)
	if err != nil {
		return err
	}
	p, ok := b.Profile(src.Kind)
	if !ok {
		return fmt.Errorf("the cluster does not serve %s, the source of %s", src.Kind, ref.Name)
	}
	if b.Access == nil {
		return nil
	}
	v := engine.Verb{Verb: nwVerbPatch, Group: p.Group, Resource: p.Resource}
	if allowed, known := b.Access.Allowed(src.Namespace, v); known && !allowed {
		return fmt.Errorf("%w: you may not patch %s.%s in %s, the source of %s", engine.ErrNotPermitted, p.Resource, p.Group, src.Namespace, ref.Name)
	}
	return nil
}

// fluxRunWithSource checks one object's chain and starts the job that writes it and waits for each
// object of it (fluxsourcejob.go); it returns the job's tab.
func fluxRunWithSource(ctx context.Context, s *engine.Session, a fluxAction, ref model.ObjectRef) (any, error) {
	b, err := writableOn(s, ref.Context) // the object's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if ref, _, err = s.ResolveRef(ref); err != nil {
		return nil, err
	}
	if ref.Context != b.Context {
		return nil, fmt.Errorf("%w: the shown cluster changed", engine.ErrWrongCluster)
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	chain, err := fluxSourceChain(ctx, s, ref)
	if err != nil {
		return nil, fmt.Errorf("%s: %w; nothing was done", ref.String(), err)
	}
	if err := engine.CheckConfirmedTargets(ctx, chain); err != nil {
		return nil, err
	}
	return fluxStartSourceJob(ctx, s, a, a.id, a.label+": "+fluxStepText(b, ref), []fluxSourcePlan{{ref: ref, chain: chain}})
}

// fluxSourcePlan is one selected object of Reconcile with Source on a selection: its chain, or why
// nothing can be written for it.
type fluxSourcePlan struct {
	ref   model.ObjectRef
	chain []model.ObjectRef
	err   error
}

// fluxSourcePlans returns the chain of every selected object Reconcile with Source applies to, and
// every object they write, each once, in order: a source two objects share is written once.
func fluxSourcePlans(ctx context.Context, s *engine.Session, a fluxAction, refs []model.ObjectRef) ([]fluxSourcePlan, []model.ObjectRef, error) {
	objs, err := fluxManyTargets(s, a, refs, false)
	if err != nil {
		return nil, nil, err
	}
	plans := make([]fluxSourcePlan, len(objs))
	var all []model.ObjectRef
	for i, r := range objs {
		chain, err := fluxSourceChain(ctx, s, r)
		plans[i] = fluxSourcePlan{ref: r, chain: chain, err: err}
		for _, c := range chain {
			if !slices.ContainsFunc(all, c.Same) {
				all = append(all, c)
			}
		}
	}
	return plans, all, nil
}

// fluxManySourceTargets is Reconcile with Source's Targets on a selection: every object it writes,
// the sources included; an error when it can write none.
func fluxManySourceTargets(s *engine.Session, a fluxAction, refs []model.ObjectRef) ([]model.ObjectRef, error) {
	plans, all, err := fluxSourcePlans(context.Background(), s, a, refs)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("%s: %w", plans[0].ref.String(), plans[0].err)
	}
	return all, nil
}

// fluxRunManyWithSource checks each selected object's chain and starts one job for all of them
// (fluxsourcejob.go): the chains side by side, a source two objects share asked once, one that
// fails failing only the objects that need it, the objects whose chain cannot be written listed
// as skipped. It returns the job's tab; an error with nothing started when no object can be
// written.
func fluxRunManyWithSource(ctx context.Context, s *engine.Session, a fluxAction, refs []model.ObjectRef) (any, error) {
	b, err := writableOn(s, refsContext(refs)) // the selection's cluster, shown or not (D-69)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedContext(ctx, b.Context); err != nil {
		return nil, err
	}
	plans, all, err := fluxSourcePlans(ctx, s, a, refs)
	if err != nil {
		return nil, err
	}
	if err := engine.CheckConfirmedTargets(ctx, all); err != nil {
		return nil, err
	}
	for _, pl := range plans {
		if pl.err == nil && pl.ref.Context != b.Context {
			return nil, fmt.Errorf("%w: the shown cluster changed", engine.ErrWrongCluster)
		}
	}
	title := fmt.Sprintf("%s: %d objects", a.label, len(plans))
	if len(plans) == 1 {
		title = a.label + ": " + fluxStepText(b, plans[0].ref)
	}
	return fluxStartSourceJob(ctx, s, a, a.manyID, title, plans)
}
