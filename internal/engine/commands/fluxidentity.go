package commands

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The identity a Flux action is carried out with (A34, owner 2026-10-14 evening; H-64): Cogpit's
// patch is the user's, but the reconciliation it asks for is the Flux controller's. The
// confirmation names whose rights it uses: the object's spec.serviceAccountName (a Kustomization's
// or a Helm release's impersonation) or, without one, the controller's own service account.
// Whether the controller enforces a default service account (--default-service-account) is a flag
// of its Deployment Cogpit does not read: the sentence says so.

// fluxControllers names the controller that reconciles each Flux API group.
var fluxControllers = map[string]string{
	"kustomize.toolkit.fluxcd.io":    "kustomize-controller",
	"helm.toolkit.fluxcd.io":         "helm-controller",
	"source.toolkit.fluxcd.io":       "source-controller",
	"notification.toolkit.fluxcd.io": "notification-controller",
	"image.toolkit.fluxcd.io":        "image-reflector-controller or image-automation-controller",
}

// impersonates reports whether the kind reconciles with spec.serviceAccountName when it is set.
func impersonates(group string) bool {
	return group == "kustomize.toolkit.fluxcd.io" || group == "helm.toolkit.fluxcd.io"
}

// fluxIdentity is the identity one object's reconciliation uses, as the confirmation names it.
func fluxIdentity(ref model.ObjectRef, obj *unstructured.Unstructured) string {
	group := ref.Kind
	if i := strings.IndexByte(group, '.'); i >= 0 {
		group = group[i+1:]
	}
	controller, ok := fluxControllers[group]
	if !ok {
		controller = "Flux's controller"
	}
	if obj != nil && impersonates(group) {
		if sa, _, _ := unstructured.NestedString(obj.Object, "spec", "serviceAccountName"); strings.TrimSpace(sa) != "" {
			return fmt.Sprintf("ServiceAccount → %s/%s (spec.serviceAccountName, through the %s)", ref.Namespace, sa, controller)
		}
	}
	return "the " + controller + "'s own service account"
}

// fluxIdentityEffects are the confirmation's rows for the objects an action acts on: whose rights
// the reconciliation uses, one identity a line, and that it is not the user's.
func fluxIdentityEffects(s *engine.Session, refs []model.ObjectRef) []engine.Effect {
	var ids []string
	for _, ref := range refs {
		obj, _ := s.Lookup(ref) // nil when the cache does not hold it: the controller's account
		if id := fluxIdentity(ref, obj); !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return []engine.Effect{
		{Name: "Runs as", Value: cappedWith(ids, "\n"), Tone: engine.EffectNames},
		{Name: "Your rights", Value: "not used (unless the controller enforces a default service account, which Cogpit cannot see)"},
	}
}
