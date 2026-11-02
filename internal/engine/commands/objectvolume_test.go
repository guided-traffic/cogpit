package commands_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// object.get carries a claim's volume usage as its drawer's Metrics section shows it (D-139):
// volumeUsage, with or without related; another kind's result has none.
func TestObjectGetCarriesAClaimsVolumeUsage(t *testing.T) {
	src := testkit.NewMemorySource()
	claims := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	src.Set(claims, testkit.Object("v1", "PersistentVolumeClaim", "acme-shop", "data-acme-redis-0", map[string]any{
		"spec":   map[string]any{"volumeName": "pvc-5c8e2a41", "resources": map[string]any{"requests": map[string]any{"storage": "10Gi"}}},
		"status": map[string]any{"phase": "Bound", "capacity": map[string]any{"storage": "10Gi"}},
	}))
	src.Set(testkit.PodsGVR, testkit.Pod("acme-shop", "acme-redis-0"))
	s := engine.New(engine.Options{Connector: &claimsConnector{src: src}, SnapshotInterval: 10 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s, src: src}

	type result struct {
		Related     *json.RawMessage    `json:"related"`
		VolumeUsage *engine.VolumeUsage `json:"volumeUsage"`
	}
	claim := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "acme-shop", Name: "data-acme-redis-0"}
	for _, related := range []bool{false, true} {
		var got result
		h.must(t, engine.OriginExternalAgent, "object.get", map[string]any{"ref": claim, "noYaml": true, "related": related}, &got)
		vu := got.VolumeUsage
		if vu == nil || vu.Volume == nil || vu.Volume.Volume != "pvc-5c8e2a41" || vu.Volume.SizeBytes != 10<<30 || vu.Fill != nil || vu.SizeNote != "" {
			t.Errorf("related %v: volume usage %+v", related, vu)
		}
		if (got.Related != nil) != related {
			t.Errorf("related %v: related %v", related, got.Related != nil)
		}
	}

	var pod result
	h.must(t, engine.OriginExternalAgent, "object.get", map[string]any{"ref": model.ObjectRef{Context: "test", Kind: "pods", Namespace: "acme-shop", Name: "acme-redis-0"}, "noYaml": true}, &pod)
	if pod.VolumeUsage != nil || pod.Related != nil {
		t.Errorf("a pod's object.get: %+v", pod)
	}
}
