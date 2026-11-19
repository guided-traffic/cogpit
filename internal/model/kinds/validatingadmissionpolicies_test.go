package kinds_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzValidatingAdmissionPolicies(f *testing.F) { fuzzProfile(f, "validatingadmissionpolicies") }

func TestValidatingAdmissionPoliciesStatusTexts(t *testing.T) {
	const none = model.StatusNone
	checkStatusTexts(t, []statusText{
		{"validatingadmissionpolicies", "healthy", "validations", "2", none, none},
		{"validatingadmissionpolicies", "healthy", "failurePolicy", "Fail", none, none},
		// Inferred: type checking that found problems is a warning.
		{"validatingadmissionpolicies", "failing", "failurePolicy", "Ignore", none, model.StatusWarning},
		{"validatingadmissionpolicies", "v1beta1", "validations", "1", none, none},
		{"validatingadmissionpolicies", "minimal", "validations", "0", none, none},
		{"validatingadmissionpolicies", "minimal", "failurePolicy", "", none, none},
		{"validatingadmissionpolicies", "terminating", "validations", "1", none, model.StatusTerminating},
	})
}

// TestAdmissionPolicyVersions pins which versions each admission policy kind is offered at: the
// validating ones fall back to v1beta1 and v1alpha1, the mutating ones need v1 (Lens showed them
// on Kubernetes 1.36.2 and not on 1.35.x, capture).
func TestAdmissionPolicyVersions(t *testing.T) {
	for key, fallbacks := range map[string][]string{
		"validatingadmissionpolicies":       {"v1beta1", "v1alpha1"},
		"validatingadmissionpolicybindings": {"v1beta1", "v1alpha1"},
		"mutatingadmissionpolicies":         nil,
		"mutatingadmissionpolicybindings":   nil,
	} {
		p, ok := kinds.ByKey(key)
		if !ok {
			t.Fatalf("no profile %s", key)
		}
		if p.Version != "v1" || p.Group != "admissionregistration.k8s.io" || !slices.Equal(p.FallbackVersions, fallbacks) {
			t.Errorf("%s: %s/%s, fallbacks %q; want admissionregistration.k8s.io/v1, fallbacks %q",
				key, p.Group, p.Version, p.FallbackVersions, fallbacks)
		}
		if p.Namespaced {
			t.Errorf("%s is cluster-scoped", key)
		}
	}
}
