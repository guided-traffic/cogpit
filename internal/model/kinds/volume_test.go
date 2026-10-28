package kinds

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A claim's and a volume's drawer show the volume's usage while the two are bound, found by the
// claim (D-139).
func TestVolumeOfClaimsAndVolumes(t *testing.T) {
	claim := func(phase, volume string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"volumeName": volume}, "status": map[string]any{"phase": phase},
		}}
		o.SetNamespace("bezon-prod")
		o.SetName("pg-1")
		return o
	}
	volume := func(phase string, claimRef map[string]any) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"claimRef": claimRef}, "status": map[string]any{"phase": phase},
		}}
		o.SetName("pvc-f0327b26")
		return o
	}
	ref := map[string]any{"namespace": "bezon-prod", "name": "pg-1"}
	want := model.VolumeClaim{Namespace: "bezon-prod", Claim: "pg-1", Volume: "pvc-f0327b26"}
	for name, tc := range map[string]struct {
		p     *model.KindProfile
		obj   *unstructured.Unstructured
		bound bool
	}{
		"bound claim":           {persistentVolumeClaims, claim("Bound", "pvc-f0327b26"), true},
		"pending claim":         {persistentVolumeClaims, claim("Pending", ""), false},
		"lost claim":            {persistentVolumeClaims, claim("Lost", "pvc-f0327b26"), false},
		"bound volume":          {persistentVolumes, volume("Bound", ref), true},
		"released volume":       {persistentVolumes, volume("Released", ref), false},
		"available volume":      {persistentVolumes, volume("Available", nil), false},
		"claim without fields":  {persistentVolumeClaims, &unstructured.Unstructured{Object: map[string]any{"spec": "garbage"}}, false},
		"volume without fields": {persistentVolumes, &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"claimRef": 7}}}, false},
	} {
		got, ok := tc.p.Volume(tc.obj)
		if ok != tc.bound || ok && !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, %v; want %+v, %v", name, got, ok, want, tc.bound)
		}
	}
}
