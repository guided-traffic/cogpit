package cluster

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8stesting "k8s.io/client-go/testing"
)

// debugEC is the container the tests add.
var debugEC = corev1.EphemeralContainer{
	EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name: "debugger-abcde", Image: "docker.io/nicolaka/netshoot:v0.16", Stdin: true, TTY: true,
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
	},
	TargetContainerName: "app",
}

// TestAddEphemeralContainerSendsTheGuardedPatch: the container goes to the pod's
// ephemeralcontainers subresource as a strategic merge patch naming it alone, with the pod's UID
// as the precondition and Cogpit as the field manager (D-102); without a UID nothing is sent.
func TestAddEphemeralContainerSendsTheGuardedPatch(t *testing.T) {
	c, cs := fakeConn()
	var got []k8stesting.PatchActionImpl
	cs.PrependReactor("patch", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		got = append(got, a.(k8stesting.PatchActionImpl))
		return true, &corev1.Pod{}, nil
	})
	if err := c.AddEphemeralContainer(context.Background(), "shop", "web-0", "uid-web-0", debugEC); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d patches", len(got))
	}
	p := got[0]
	want := `{"metadata":{"uid":"uid-web-0"},"spec":{"ephemeralContainers":[{"name":"debugger-abcde","image":"docker.io/nicolaka/netshoot:v0.16",` +
		`"resources":{},"terminationMessagePolicy":"File","stdin":true,"tty":true,"targetContainerName":"app"}]}}`
	if p.GetPatchType() != types.StrategicMergePatchType || p.GetSubresource() != "ephemeralcontainers" || p.GetNamespace() != "shop" ||
		p.GetName() != "web-0" || p.PatchOptions.FieldManager != FieldManager || string(p.GetPatch()) != want {
		t.Errorf("patch %s %q %s/%s %+v\n%s", p.GetPatchType(), p.GetSubresource(), p.GetNamespace(), p.GetName(), p.PatchOptions, p.GetPatch())
	}
	if err := c.AddEphemeralContainer(context.Background(), "shop", "web-0", "", debugEC); !errors.Is(err, ErrNoUID) || len(got) != 1 {
		t.Errorf("without a UID: %v, %d patches", err, len(got))
	}
}

// TestAddEphemeralContainerRefusals: the API server's refusal of another pod's UID (422 on
// metadata.uid) is ErrReplaced; its other refusals (Pod Security admission's 403) pass with their
// message.
func TestAddEphemeralContainerRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		err      error
		replaced bool
	}{
		"uid": {apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "web-0", field.ErrorList{
			field.Invalid(field.NewPath("metadata", "uid"), "uid-other", "field is immutable")}), true},
		"pod security": {apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "web-0",
			errors.New(`violates PodSecurity "baseline:latest": non-default capabilities`)), false},
	} {
		conn, cs := fakeConn()
		cs.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, c.err })
		err := conn.AddEphemeralContainer(context.Background(), "shop", "web-0", "uid-web-0", debugEC)
		if errors.Is(err, ErrReplaced) != c.replaced || (!c.replaced && !apierrors.IsForbidden(err)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
