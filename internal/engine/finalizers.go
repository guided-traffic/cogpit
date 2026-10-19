package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Remove finalizers (WF43, D-98): a destructive command on one object being deleted that removes
// exactly the finalizers the user saw, so that the object goes without the cleanup their
// controllers would have done. The owner's yes (Q1, 2026-10-19): "ja gerne".

// RemoveFinalizersCommand is the id of the command that removes an object's finalizers; the
// Terminating namespace's drawer offers it beside each object that holds the deletion.
const RemoveFinalizersCommand = "object.removeFinalizers"

// RemoveFinalizersParams are its parameters: the object, and, optionally, its finalizers exactly as
// the caller read them. The confirmation names the finalizers it removes (these, else the cached
// object's), and the removal acts only while the object holds exactly those.
type RemoveFinalizersParams struct {
	Ref        model.ObjectRef `json:"ref" jsonschema:"the object being deleted whose finalizers to remove (any kind but namespaces)"`
	Finalizers []string        `json:"finalizers,omitempty" jsonschema:"the object's metadata.finalizers exactly as read, in their order (object_get's YAML, or the action's params in a Terminating namespace's Blocking Deletion section); omitted: the cached object's, as the confirmation shows them. Cogpit removes them only while the object still holds exactly these"`
}

// EffectRemovesFinalizers names the confirmation's row of the finalizers a removal removes, one a
// line. The command removes exactly the ones that row showed (ConfirmedFinalizers).
const EffectRemovesFinalizers = "Removes finalizers"

// FinalizersShown are the finalizers a removal's confirmation names: the given ones, else the
// cached object's (none when the cache does not hold it).
func (s *Session) FinalizersShown(ref model.ObjectRef, given []string) []string {
	if len(given) > 0 {
		return given
	}
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	t, b, err := s.ResolveTarget(ctx, ref)
	if err != nil {
		return nil
	}
	obj, err := cachedObject(ctx, b, t)
	if err != nil {
		return nil
	}
	return obj.GetFinalizers()
}

// ConfirmedFinalizers returns the finalizers the user confirmed the running removal for, read back
// from the confirmation's row (EffectRemovesFinalizers); false without a confirmation.
func ConfirmedFinalizers(ctx context.Context) ([]string, bool) {
	req, ok := Confirmed(ctx)
	if !ok {
		return nil, false
	}
	for _, e := range req.Effects {
		if e.Name == EffectRemovesFinalizers && e.Value != "" {
			return strings.Split(e.Value, "\n"), true
		}
	}
	return nil, true
}

// FinalizerWriter is the cluster side of Remove finalizers. *cluster.Conn implements it; tests put
// a fake in Backend.Actions. The engine reaches it with a type assertion on Backend.Writer.
type FinalizerWriter interface {
	// RemoveFinalizers removes metadata.finalizers of one object while it has uid and holds
	// exactly finalizers (cluster.FinalizersPatch); otherwise cluster.ErrFinalizersChanged.
	RemoveFinalizers(ctx context.Context, gvr schema.GroupVersionResource, namespace, name, uid string, finalizers []string) error
}

// RemoveFinalizersResult is what the command reports.
type RemoveFinalizersResult struct {
	Ref     model.ObjectRef `json:"ref"`
	Removed []string        `json:"removed"`
	UID     string          `json:"uid,omitempty"`
	Action  string          `json:"action"` // "finalizers removed"
}

// ErrNotBeingDeleted refuses Remove finalizers on an object without a deletion timestamp: its
// finalizers hold nothing yet, and removing them is not what the command is for.
var ErrNotBeingDeleted = errors.New("the object is not being deleted: only an object being deleted has finalizers that hold it")

// errNamespaceFinalizers says why Remove finalizers never applies to a namespace.
var errNamespaceFinalizers = fmt.Errorf("a namespace's deletion is never forced: it finishes by itself once nothing in it holds it, and its own finalizer is the namespace controller's, which Cogpit never removes (%w)", ErrNotApplicable)

// FinalizersRemovable checks Remove finalizers on an object the cache holds: it applies only to an
// object of the cluster's API, not a namespace, that is being deleted and holds finalizers, and,
// when finalizers are given, the object must hold exactly those. Objects of a kind without a
// profile pass here and are checked when the command runs.
func (s *Session) FinalizersRemovable(ref model.ObjectRef, finalizers []string) error {
	b, err := s.TargetBackend(ref.Context)
	if err != nil {
		return err
	}
	// Whether it applies at all comes first, so that the menus of objects it does not apply to
	// (every object not being deleted, the demo cluster's included) do not show it greyed.
	if err := s.finalizersApply(b, ref, finalizers); err != nil {
		return err
	}
	if !b.CanWrite() {
		return errors.New("the demo cluster cannot be changed")
	}
	if _, ok := b.Writer().(FinalizerWriter); !ok {
		return errors.New("this cluster connection cannot remove finalizers")
	}
	return nil
}

// finalizersApply is FinalizersRemovable's check of the object, without the writer's.
func (s *Session) finalizersApply(b *Backend, ref model.ObjectRef, finalizers []string) error {
	p, ok := b.Profile(ref.Kind)
	if !ok {
		if ref.Kind == namespacesKind {
			return errNamespaceFinalizers
		}
		if ref.Name == "" {
			return errors.New("the reference has no name")
		}
		return nil
	}
	if !p.APIObject() {
		return fmt.Errorf("%s are not objects of the cluster's API (%w)", p.Title, ErrNotApplicable)
	}
	if isNamespaces(p) {
		return errNamespaceFinalizers
	}
	obj, err := s.Lookup(ref)
	switch {
	case err != nil && len(finalizers) > 0 && errors.Is(err, ErrNotSynced):
		// A Terminating namespace's Blocking Deletion list read the object in a scope of its own
		// (nsblockers.go) while no view lists its kind: Run checks it against the kind's cache.
		return nil
	case err != nil:
		return err
	}
	return finalizersHeld(ref, obj, finalizers)
}

// finalizersHeld checks the object's deletion and finalizers against the ones given (none: any).
func finalizersHeld(ref model.ObjectRef, obj *unstructured.Unstructured, finalizers []string) error {
	held := obj.GetFinalizers()
	switch {
	case obj.GetDeletionTimestamp() == nil:
		return fmt.Errorf("%s: %w (%w)", ref.Name, ErrNotBeingDeleted, ErrNotApplicable)
	case len(held) == 0:
		return fmt.Errorf("%s holds no finalizers (%w)", ref.Name, ErrNotApplicable)
	case len(finalizers) > 0 && !slices.Equal(held, finalizers):
		return fmt.Errorf("%w: %s holds the finalizers %v now, not %v", ErrTargetsChanged, ref.Name, held, finalizers)
	}
	return nil
}

// RemoveFinalizers removes the given finalizers of one object being deleted: bound to the UID the
// user confirmed (else the cache's) and to exactly these finalizers, checked against the cache
// first and by the API server in the patch (cluster.FinalizersPatch). The caller has checked the
// confirmation's context.
func (s *Session) RemoveFinalizers(ctx context.Context, p RemoveFinalizersParams) (RemoveFinalizersResult, error) {
	if len(p.Finalizers) == 0 {
		return RemoveFinalizersResult{}, errors.New("name the finalizers to remove, exactly as the object holds them (metadata.finalizers)")
	}
	t, b, err := s.ResolveTarget(ctx, p.Ref)
	if err != nil {
		return RemoveFinalizersResult{}, err
	}
	if t.GVR.Group == "" && t.GVR.Resource == namespacesKind {
		return RemoveFinalizersResult{}, errNamespaceFinalizers
	}
	w, ok := b.Writer().(FinalizerWriter)
	if !ok {
		return RemoveFinalizersResult{}, errors.New("this cluster connection cannot remove finalizers")
	}
	obj, err := cachedObject(ctx, b, t)
	if err != nil {
		return RemoveFinalizersResult{}, err
	}
	if err := finalizersHeld(t.Ref, obj, p.Finalizers); err != nil {
		return RemoveFinalizersResult{}, err
	}
	uid, err := ActUID(ctx, t.Ref, func() (string, bool) { return string(obj.GetUID()), true })
	if err != nil {
		return RemoveFinalizersResult{}, err
	}
	if string(obj.GetUID()) != uid {
		return RemoveFinalizersResult{}, fmt.Errorf("%w: %s is no longer the object that was confirmed; nothing was done", ErrTargetsChanged, t.Ref.String())
	}
	if err := w.RemoveFinalizers(ctx, t.GVR, t.Ref.Namespace, t.Ref.Name, uid, p.Finalizers); err != nil {
		if errors.Is(err, cluster.ErrFinalizersChanged) {
			return RemoveFinalizersResult{}, fmt.Errorf("%w: %v", ErrTargetsChanged, err)
		}
		return RemoveFinalizersResult{}, fmt.Errorf("%s: %w", t.Ref.String(), err)
	}
	return RemoveFinalizersResult{Ref: t.Ref, Removed: slices.Clone(p.Finalizers), UID: uid, Action: "finalizers removed"}, nil
}
