package cluster

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// finalizerConn is a Conn on client-go's fake dynamic client serving one ConfigMap; every patch is
// recorded and answered by reply (nil: accepted).
func finalizerConn(t *testing.T, reply func(k8stesting.PatchActionImpl) error) (*Conn, *[]k8stesting.PatchActionImpl) {
	t.Helper()
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "walk-cm", "namespace": "walk-fin", "uid": "uid-1", "finalizers": []any{"example.com/x"}},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "ConfigMapList"}, cm)
	var patches []k8stesting.PatchActionImpl
	dyn.PrependReactor("patch", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		p := a.(k8stesting.PatchActionImpl)
		patches = append(patches, p)
		if reply != nil {
			if err := reply(p); err != nil {
				return true, nil, err
			}
		}
		return true, cm, nil
	})
	return &Conn{contextName: "test", namespace: "default", dyn: dyn, counter: NewRequestCounter()}, &patches
}

// TestRemoveFinalizersSendsTheGuardedPatch: a JSON patch that tests the UID and the finalizers as
// read, then removes metadata.finalizers, as Cogpit's field manager; nothing without both.
func TestRemoveFinalizersSendsTheGuardedPatch(t *testing.T) {
	c, patches := finalizerConn(t, nil)
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	if err := c.RemoveFinalizers(context.Background(), gvr, "walk-fin", "walk-cm", "uid-1", []string{"example.com/x", "example.com/y"}); err != nil {
		t.Fatal(err)
	}
	want := `[{"op":"test","path":"/metadata/uid","value":"uid-1"},{"op":"test","path":"/metadata/finalizers","value":["example.com/x","example.com/y"]},{"op":"remove","path":"/metadata/finalizers"}]`
	if len(*patches) != 1 {
		t.Fatalf("%d patches", len(*patches))
	}
	p := (*patches)[0]
	if p.GetPatchType() != types.JSONPatchType || string(p.GetPatch()) != want || p.GetNamespace() != "walk-fin" ||
		p.GetName() != "walk-cm" || p.PatchOptions.FieldManager != FieldManager || p.GetSubresource() != "" {
		t.Errorf("patch %s %s %+v %q", p.GetPatchType(), p.GetPatch(), p.PatchOptions, p.GetSubresource())
	}
	for _, bad := range []struct {
		uid  string
		fins []string
	}{{"", []string{"a"}}, {"uid-1", nil}} {
		if err := c.RemoveFinalizers(context.Background(), gvr, "walk-fin", "walk-cm", bad.uid, bad.fins); !errors.Is(err, ErrNoFinalizers) {
			t.Errorf("without %q/%v: %v", bad.uid, bad.fins, err)
		}
	}
	if len(*patches) != 1 {
		t.Error("a removal without its preconditions was sent")
	}
}

// TestRemoveFinalizersFailedTest: the API server's answer to a failed test (422 Invalid, no
// causes, as kind v1.36.1 answers it) is ErrFinalizersChanged; a validation error with causes and
// other errors pass unchanged.
func TestRemoveFinalizersFailedTest(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	gk := schema.GroupKind{Kind: "ConfigMap"}
	failed := apierrors.NewInvalid(gk, "walk-cm", nil)
	c, _ := finalizerConn(t, func(k8stesting.PatchActionImpl) error { return failed })
	if err := c.RemoveFinalizers(context.Background(), gvr, "walk-fin", "walk-cm", "uid-1", []string{"example.com/x"}); !errors.Is(err, ErrFinalizersChanged) {
		t.Errorf("failed test: %v", err)
	}
	invalid := apierrors.NewInvalid(gk, "walk-cm", field.ErrorList{field.Invalid(field.NewPath("data"), "x", "too long")})
	c, _ = finalizerConn(t, func(k8stesting.PatchActionImpl) error { return invalid })
	if err := c.RemoveFinalizers(context.Background(), gvr, "walk-fin", "walk-cm", "uid-1", []string{"example.com/x"}); errors.Is(err, ErrFinalizersChanged) || !apierrors.IsInvalid(err) {
		t.Errorf("a validation error: %v", err)
	}
	forbidden := apierrors.NewForbidden(gvr.GroupResource(), "walk-cm", errors.New("no"))
	c, _ = finalizerConn(t, func(k8stesting.PatchActionImpl) error { return forbidden })
	if err := c.RemoveFinalizers(context.Background(), gvr, "walk-fin", "walk-cm", "uid-1", []string{"example.com/x"}); !apierrors.IsForbidden(err) {
		t.Errorf("forbidden: %v", err)
	}
}
