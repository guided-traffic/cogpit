package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// The cluster side of a debug container (engine/debugcontainer.go, D-102): an ephemeral container
// added to a running pod as kubectl debug adds one, through the pod's ephemeralcontainers
// subresource. The API server takes no delete or change of an ephemeral container afterwards:
// it stays in the pod's spec and status as long as the pod exists.

// ErrNoUID refuses an ephemeral container's patch without the pod's UID: Cogpit adds one only to
// the pod the user confirmed.
var ErrNoUID = errors.New("adding an ephemeral container needs the pod's UID")

// EphemeralContainerPatch is the strategic merge patch that adds ec to a pod's ephemeral
// containers, as kubectl debug sends it to the ephemeralcontainers subresource: the list merges by
// name, so the patch names the new container alone. metadata.uid is Cogpit's precondition: the
// subresource's update keeps the patch's UID and the API server refuses its change (422 Invalid on
// metadata.uid, ValidatePodEphemeralContainersUpdate), so a pod created again under the name is
// never debugged in place of the confirmed one. A container whose name the pod already has would
// change an existing ephemeral container, which the API server refuses ("may not be changed").
func EphemeralContainerPatch(uid string, ec corev1.EphemeralContainer) ([]byte, error) {
	if uid == "" {
		return nil, ErrNoUID
	}
	return json.Marshal(map[string]any{
		"metadata": uidMeta(uid),
		fieldSpec:  map[string]any{"ephemeralContainers": []corev1.EphemeralContainer{ec}},
	})
}

// AddEphemeralContainer adds ec to the pod with uid (EphemeralContainerPatch) as Cogpit's field
// manager. A refusal for another UID is ErrReplaced; the API server's other refusals (Pod Security
// admission, a policy engine's webhook, no permission) pass with its message.
func (c *Conn) AddEphemeralContainer(ctx context.Context, namespace, pod, uid string, ec corev1.EphemeralContainer) error {
	patch, err := EphemeralContainerPatch(uid, ec)
	if err != nil {
		return fmt.Errorf("add ephemeral container %s to pod %s/%s: %w", ec.Name, namespace, pod, err)
	}
	_, err = c.kube.CoreV1().Pods(namespace).Patch(ctx, pod, types.StrategicMergePatchType, patch,
		metav1.PatchOptions{FieldManager: FieldManager}, "ephemeralcontainers")
	if err != nil {
		return fmt.Errorf("add ephemeral container %s to pod %s/%s: %w", ec.Name, namespace, pod, replacedError(err, uid))
	}
	return nil
}
