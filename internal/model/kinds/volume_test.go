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

// A volume's size, which its filesystem is checked against (D-139): a claim's the larger of its
// capacity and its request, a volume's its capacity.
func TestVolumeSizeOfClaimsAndVolumes(t *testing.T) {
	const gi = int64(1) << 30
	claim := func(request, capacity any) *unstructured.Unstructured {
		spec := map[string]any{"volumeName": "pvc-5c8e2a41"}
		if request != nil {
			spec["resources"] = map[string]any{"requests": map[string]any{"storage": request}}
		}
		status := map[string]any{"phase": "Bound"}
		if capacity != nil {
			status["capacity"] = map[string]any{"storage": capacity}
		}
		o := &unstructured.Unstructured{Object: map[string]any{"spec": spec, "status": status}}
		o.SetNamespace("acme-shop")
		o.SetName("data-acme-redis-0")
		return o
	}
	volume := func(capacity any) *unstructured.Unstructured {
		spec := map[string]any{"claimRef": map[string]any{"namespace": "acme-shop", "name": "data-acme-redis-0"}}
		if capacity != nil {
			spec["capacity"] = map[string]any{"storage": capacity}
		}
		o := &unstructured.Unstructured{Object: map[string]any{"spec": spec, "status": map[string]any{"phase": "Bound"}}}
		o.SetName("pvc-5c8e2a41")
		return o
	}
	for name, tc := range map[string]struct {
		p    *model.KindProfile
		obj  *unstructured.Unstructured
		want int64
	}{
		"claim as requested":         {persistentVolumeClaims, claim("10Gi", "10Gi"), 10 * gi},
		"claim on a larger volume":   {persistentVolumeClaims, claim("10Gi", "100Gi"), 100 * gi},
		"claim resized, not yet":     {persistentVolumeClaims, claim("20Gi", "10Gi"), 20 * gi},
		"claim with a request alone": {persistentVolumeClaims, claim("10Gi", nil), 10 * gi},
		"claim in decimal units":     {persistentVolumeClaims, claim("10G", nil), 10_000_000_000},
		"claim without a size":       {persistentVolumeClaims, claim(nil, nil), 0},
		"claim with garbage":         {persistentVolumeClaims, claim("lots", []any{"10Gi"}), 0},
		"volume":                     {persistentVolumes, volume("10Gi"), 10 * gi},
		"volume without a capacity":  {persistentVolumes, volume(nil), 0},
	} {
		got, ok := tc.p.Volume(tc.obj)
		if !ok || got.SizeBytes != tc.want {
			t.Errorf("%s: size %d (bound %v), want %d", name, got.SizeBytes, ok, tc.want)
		}
	}
}
