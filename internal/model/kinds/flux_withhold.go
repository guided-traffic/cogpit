package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Flux's credential fields in what agents read (agents-and-mcp H-63, owner): the fields of a Flux
// object that can hold a credential inline are withheld from the YAML an agent reads, as a Helm
// release's values are (model.WithholdPrivate): a Helm release's inline spec.values, a notification
// Provider's spec.address (a webhook URL with its token) and spec.proxy, a Receiver's
// status.webhookPath (for a generic receiver all a caller needs to trigger reconciliations), and
// the user information in a source's URL (GitRepository, HelmRepository, OCIRepository, Bucket).
// The window's YAML shows them; the Flux views' cells and drawers never did (TestFluxKeepsSecretsOut).
// A copy of the object kubectl kept in the last-applied annotation repeats them and is withheld
// with them.

// fluxWithheldUser replaces the user information of a source's URL for agents.
const fluxWithheldUser = "<withheld>"

// Kinds and fields of Flux's credential fields.
const (
	fluxKindProvider = "Provider"
	fluxKindBucket   = "Bucket"
	fluxStatusField  = "status"
)

// fluxCredentialFields are the fields withheld whole, by API group and kind.
var fluxCredentialFields = map[[2]string][][]string{
	{fluxHelm, "HelmRelease"}:            {{fieldSpec, "values"}},
	{fluxNotification, fluxKindProvider}: {{fieldSpec, "address"}, {fieldSpec, "proxy"}},
	{fluxNotification, "Receiver"}:       {{fluxStatusField, "webhookPath"}},
}

// fluxURLKinds are the sources whose URL may carry user information, with its fields.
var fluxURLKinds = map[string][][]string{
	"GitRepository":  {{fieldSpec, idURL}},
	"HelmRepository": {{fieldSpec, idURL}},
	"OCIRepository":  {{fieldSpec, idURL}},
	fluxKindBucket:   {{fieldSpec, "endpoint"}, {fieldSpec, idURL}},
}

// lastAppliedAnnotation is where kubectl apply keeps the manifest it applied.
const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// IsFluxCredentialKind reports whether objects of apiVersion and kind are Flux objects whose YAML
// can hold a credential inline (WithholdFluxCredentials).
func IsFluxCredentialKind(apiVersion, kind string) bool {
	group, _, _ := strings.Cut(apiVersion, "/")
	if _, ok := fluxCredentialFields[[2]string{group, kind}]; ok {
		return true
	}
	_, ok := fluxURLKinds[kind]
	return ok && group == fluxSource
}

// WithholdFluxCredentials returns a copy of obj with Flux's credential fields replaced by
// model.Withheld (a URL's user information by <withheld>), and whether it replaced any; obj itself
// when it is no such object or holds none of them.
func WithholdFluxCredentials(obj *unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	if obj == nil || !IsFluxCredentialKind(obj.GetAPIVersion(), obj.GetKind()) {
		return obj, false
	}
	group, _, _ := strings.Cut(obj.GetAPIVersion(), "/")
	out := obj.DeepCopy()
	changed := false
	for _, f := range fluxCredentialFields[[2]string{group, obj.GetKind()}] {
		if _, found, _ := unstructured.NestedFieldNoCopy(out.Object, f...); found {
			_ = unstructured.SetNestedField(out.Object, model.Withheld, f...)
			changed = true
		}
	}
	if group == fluxSource {
		for _, f := range fluxURLKinds[obj.GetKind()] {
			if u, ok, _ := unstructured.NestedString(out.Object, f...); ok {
				if w := withheldUserInfo(u); w != u {
					_ = unstructured.SetNestedField(out.Object, w, f...)
					changed = true
				}
			}
		}
	}
	if ann := out.GetAnnotations(); ann[lastAppliedAnnotation] != "" {
		ann[lastAppliedAnnotation] = model.Withheld
		out.SetAnnotations(ann)
		changed = true
	}
	if !changed {
		return obj, false
	}
	return out, true
}

// withheldUserInfo is fluxURL with <withheld> in place of the user information, which an agent
// cannot mistake for a value to write back (agentview.go refuses a write that holds it).
func withheldUserInfo(s string) string {
	masked := fluxURL(s)
	if masked == s {
		return s
	}
	return strings.Replace(masked, "://"+model.Redacted+"@", "://"+fluxWithheldUser+"@", 1)
}
