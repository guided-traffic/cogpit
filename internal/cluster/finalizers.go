package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ErrFinalizersChanged is returned when the object no longer holds exactly the finalizers a
// removal named, or is another object under its name by now: nothing was removed. Match it with
// errors.Is.
var ErrFinalizersChanged = errors.New("the object's finalizers, or the object itself, changed since they were read; nothing was removed")

// ErrNoFinalizers refuses a removal that names no finalizer or no UID: Cogpit removes only what the
// user saw, from the object they saw.
var ErrNoFinalizers = errors.New("a removal of finalizers needs the object's UID and the finalizers it holds")

// FinalizersPatch is the JSON patch (RFC 6902) that removes an object's metadata.finalizers only
// while the object is the one with uid and holds exactly finalizers, in that order: two test
// operations, then the removal. The API server applies the patch to the object it holds and
// refuses it as a whole when a test fails (422 Invalid, whose message it does not detail; seen on
// kind v1.36.1, 2026-10-19), so a finalizer added or removed since, or another object under the
// name, is never overwritten. It names no other field, and never spec.finalizers or a subresource:
// a namespace's own finalizer is removed only through /finalize, which Cogpit never calls.
func FinalizersPatch(uid string, finalizers []string) ([]byte, error) {
	if uid == "" || len(finalizers) == 0 {
		return nil, ErrNoFinalizers
	}
	return json.Marshal([]jsonPatchOp{
		{Op: "test", Path: "/metadata/uid", Value: uid},
		{Op: "test", Path: pathFinalizers, Value: finalizers},
		{Op: "remove", Path: pathFinalizers},
	})
}

// jsonPatchOp is one operation of a JSON patch (RFC 6902).
type jsonPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// pathFinalizers is the one field a removal of finalizers names.
const pathFinalizers = "/metadata/finalizers"

// RemoveFinalizers removes the finalizers of one object of any kind with FinalizersPatch, as
// Cogpit's field manager: the object then goes as soon as nothing else holds it, without the
// cleanup its finalizers' controllers would have done. A failed test is ErrFinalizersChanged.
func (c *Conn) RemoveFinalizers(ctx context.Context, gvr schema.GroupVersionResource, namespace, name, uid string, finalizers []string) error {
	what := describeObject(gvr, namespace, name)
	patch, err := FinalizersPatch(uid, finalizers)
	if err != nil {
		return fmt.Errorf("remove the finalizers of %s: %w", what, err)
	}
	_, err = c.dyn.Resource(gvr).Namespace(namespace).Patch(ctx, name, types.JSONPatchType, patch,
		metav1.PatchOptions{FieldManager: FieldManager})
	switch {
	case err == nil:
		return nil
	case failedTest(err):
		return fmt.Errorf("remove the finalizers of %s: %w (%v)", what, ErrFinalizersChanged, err)
	}
	return fmt.Errorf("remove the finalizers of %s: %w", what, err)
}

// failedTest reports whether the API server refused a JSON patch because a test operation failed:
// 422 Invalid without the field causes a validation error carries.
func failedTest(err error) bool {
	if !apierrors.IsInvalid(err) {
		return false
	}
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return false
	}
	d := status.Status().Details
	return d == nil || len(d.Causes) == 0
}
