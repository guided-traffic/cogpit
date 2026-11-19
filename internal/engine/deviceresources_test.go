package engine

import (
	"maps"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// TestBuiltinDeviceResourcesFollowTheServedVersion checks the Device Resources views on the
// discovery of three clusters: one that serves resource.k8s.io/v1 (Kubernetes 1.34 and later) gets
// the four views at v1; one that serves only the beta versions (1.32 and 1.33 with the beta APIs
// switched on) gets none, as Lens reads v1 only (its documentation); a cluster without dynamic
// resource allocation gets none.
func TestBuiltinDeviceResourcesFollowTheServedVersion(t *testing.T) {
	const group = "resource.k8s.io"
	all := []string{"resourceclaims", "resourceclaimtemplates", "deviceclasses", "resourceslices"}
	served := func(versions ...string) map[schema.GroupVersionResource]bool {
		m := map[schema.GroupVersionResource]bool{}
		for _, v := range versions {
			for _, r := range all {
				m[schema.GroupVersionResource{Group: group, Version: v, Resource: r}] = true
			}
		}
		return m
	}
	tests := []struct {
		name   string
		served map[schema.GroupVersionResource]bool
		want   map[string]string // kind key → version offered
	}{
		{"1.36", served("v1", "v1beta2"), map[string]string{all[0]: "v1", all[1]: "v1", all[2]: "v1", all[3]: "v1"}},
		{"1.33 with beta APIs", served("v1beta2", "v1beta1"), map[string]string{}},
		{"no dynamic resource allocation", served(), map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			for _, p := range servedProfiles(kinds.Builtin(), tt.served) {
				if p.Group == group {
					got[p.Key] = p.Version
				}
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("offered %v, want %v", got, tt.want)
			}
		})
	}
}
