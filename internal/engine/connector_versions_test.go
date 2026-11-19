package engine

import (
	"maps"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// TestServedProfilesFallBackToAnOlderVersion checks how discovery picks a profile's version: the
// preferred one when the cluster serves it (the profile itself), else the first served fallback
// version (a copy at that version, the shared profile untouched), else no view at all.
func TestServedProfilesFallBackToAnOlderVersion(t *testing.T) {
	policies := &model.KindProfile{
		Key: "validatingadmissionpolicies", Group: "admissionregistration.k8s.io", Version: "v1",
		FallbackVersions: []string{"v1beta1", "v1alpha1"}, Resource: "validatingadmissionpolicies",
	}
	mutating := &model.KindProfile{
		Key: "mutatingadmissionpolicies", Group: "admissionregistration.k8s.io", Version: "v1",
		Resource: "mutatingadmissionpolicies",
	}
	gvr := func(resource, version string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: version, Resource: resource}
	}
	tests := []struct {
		name   string
		served []schema.GroupVersionResource
		want   map[string]string // kind key → version offered
	}{
		{"every version served", []schema.GroupVersionResource{
			gvr("validatingadmissionpolicies", "v1"), gvr("validatingadmissionpolicies", "v1beta1"),
			gvr("mutatingadmissionpolicies", "v1"), gvr("mutatingadmissionpolicies", "v1beta1"),
		}, map[string]string{policies.Key: "v1", mutating.Key: "v1"}},
		{"only beta served", []schema.GroupVersionResource{
			gvr("validatingadmissionpolicies", "v1beta1"), gvr("validatingadmissionpolicies", "v1alpha1"),
			gvr("mutatingadmissionpolicies", "v1beta1"),
		}, map[string]string{policies.Key: "v1beta1"}},
		{"only alpha served", []schema.GroupVersionResource{
			gvr("validatingadmissionpolicies", "v1alpha1"),
		}, map[string]string{policies.Key: "v1alpha1"}},
		{"none served", nil, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := map[schema.GroupVersionResource]bool{}
			for _, g := range tt.served {
				served[g] = true
			}
			got := servedProfiles([]*model.KindProfile{policies, mutating}, served)
			if len(got) != len(tt.want) {
				t.Fatalf("offered %d kinds, want %d: %+v", len(got), len(tt.want), got)
			}
			for _, p := range got {
				if want, ok := tt.want[p.Key]; !ok || p.Version != want {
					t.Errorf("%s offered at %q, want %q (offered: %v)", p.Key, p.Version, want, ok)
				}
				if !served[p.GVR()] {
					t.Errorf("%s offered at %v, which the cluster does not serve", p.Key, p.GVR())
				}
				if p.Version == "v1" && p != policies && p != mutating {
					t.Errorf("%s: the preferred version should keep the shared profile", p.Key)
				}
			}
		})
	}
	if policies.Version != "v1" {
		t.Errorf("the shared profile changed to %q", policies.Version)
	}
}

// TestBuiltinAdmissionPoliciesFollowTheServedVersion checks the built-in admission policy views
// on the discovery of three Kubernetes versions: 1.36 serves every kind at v1; 1.29 with the beta
// APIs switched on serves the validating policies at v1beta1 and the mutating ones not at all,
// whose views stay hidden as Lens hides them before 1.36 (capture); a cluster that serves only
// v1alpha1 gets the validating views at v1alpha1.
func TestBuiltinAdmissionPoliciesFollowTheServedVersion(t *testing.T) {
	const group = "admissionregistration.k8s.io"
	served := func(version string, resources ...string) map[schema.GroupVersionResource]bool {
		m := map[schema.GroupVersionResource]bool{}
		for _, r := range resources {
			m[schema.GroupVersionResource{Group: group, Version: version, Resource: r}] = true
		}
		return m
	}
	all := []string{"mutatingadmissionpolicies", "mutatingadmissionpolicybindings", "validatingadmissionpolicies", "validatingadmissionpolicybindings"}
	validating := all[2:]
	tests := []struct {
		name   string
		served map[schema.GroupVersionResource]bool
		want   map[string]string // kind key → version offered
	}{
		{"1.36", served("v1", all...), map[string]string{
			all[0]: "v1", all[1]: "v1", all[2]: "v1", all[3]: "v1",
		}},
		{"1.29 with beta APIs", served("v1beta1", all...), map[string]string{all[2]: "v1beta1", all[3]: "v1beta1"}},
		{"alpha only", served("v1alpha1", validating...), map[string]string{all[2]: "v1alpha1", all[3]: "v1alpha1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			for _, p := range servedProfiles(kinds.Builtin(), tt.served) {
				if p.Group == group && strings.Contains(p.Key, "admissionpolic") { // not the webhooks
					got[p.Key] = p.Version
				}
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("offered %v, want %v", got, tt.want)
			}
		})
	}
}
