package engine

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A storage that does not keep to a volume's size (D-139): a filesystem clearly larger than the
// volume, as a host directory reports the node's disk, gets a note under the usage bar, in the
// claim's drawer, the volume's and object.get's volumeUsage.

const volGi = int64(1) << 30

func TestVolumeOversized(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int64
		capacity float64
		want     bool
	}{
		{"filesystem smaller than the volume (ext4)", 200 * volGi, 196.7 * volGiB, false},
		{"filesystem as large as the volume", 10 * volGi, 10 * volGiB, false},
		{"at the ratio", 10 * volGi, 11 * volGiB, false},
		{"just above the ratio", 10 * volGi, 11*volGiB + 1<<20, true},
		{"the node's disk under a 10Gi claim", 10 * volGi, 122.9 * volGiB, true},
		{"below the floor though twice the size", volGi, 1.9 * volGiB, false},
		{"at the floor", volGi, 2 * volGiB, true},
		{"a small claim on a 1 GiB disk", 100 << 20, volGiB, false},
		{"above the floor, below the ratio", 100 * volGi, 108 * volGiB, false},
		{"10G asked, 10Gi given", 10_000_000_000, 10 * volGiB, false},
		{"100G asked, 100Gi given", 100_000_000_000, 100 * volGiB, false},
		{"no size", 0, 122.9 * volGiB, false},
		{"no capacity", 10 * volGi, 0, false},
	} {
		if got := volumeOversized(tc.size, tc.capacity); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	fill := &model.FilesystemSample{UsageBytes: 30 * volGiB, CapacityBytes: 122.9 * volGiB, AvailableBytes: 86.6 * volGiB}
	if got, want := volumeSizeNote(10*volGi, fill), "Size not enforced: this 10.0 GiB volume sits on 122.9 GiB of storage and can grow past 10.0 GiB."; got != want {
		t.Errorf("note %q, want %q", got, want)
	}
	if got := volumeSizeNote(10*volGi, nil); got != "" {
		t.Errorf("without a newest point: %q", got)
	}
}

// sizedProm is a Prometheus that reports each claim's volume with its own numbers.
type sizedProm struct {
	*volumeProm
	fs map[string]model.FilesystemSample // by namespace/claim
}

func (p *sizedProm) ObjectRange(_ context.Context, q metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	out := &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{}, From: q.From, To: q.To, Step: q.Step}
	f, ok := p.fs[q.Namespace+"/"+q.Name]
	if q.Kind != metrics.KindClaims || !ok {
		return out, nil
	}
	var fs []model.FilesystemSample
	for at := q.From; !at.After(q.To); at = at.Add(q.Step) {
		f.At = at
		fs = append(fs, f)
	}
	out.Targets[""] = &metrics.TargetSeries{Filesystem: fs}
	out.FilesystemSource = metrics.FilesystemUsage
	return out, nil
}

func acmeClaim(name, volume, size string) *unstructured.Unstructured {
	return testkit.Object("v1", "PersistentVolumeClaim", "acme-shop", name, map[string]any{
		"spec": map[string]any{
			"volumeName": volume, "storageClassName": "local-path",
			"resources": map[string]any{"requests": map[string]any{"storage": size}},
		},
		"status": map[string]any{"phase": "Bound", "capacity": map[string]any{"storage": size}},
	})
}

func acmeVolume(name, claim, size string) *unstructured.Unstructured {
	spec := map[string]any{"capacity": map[string]any{"storage": size}}
	phase := "Available"
	if claim != "" {
		spec["claimRef"], phase = map[string]any{"namespace": "acme-shop", "name": claim}, "Bound"
	}
	return testkit.Object("v1", "PersistentVolume", "", name, map[string]any{"spec": spec, "status": map[string]any{"phase": phase}})
}

func TestVolumeLargerThanItsSize(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(claimsGVR, acmeClaim("data-acme-redis-0", "pvc-5c8e2a41", "10Gi"))
	src.Set(claimsGVR, acmeClaim("data-acme-pg-0", "pvc-9b1d07e3", "200Gi"))
	src.Set(volumesGVR, acmeVolume("pvc-5c8e2a41", "data-acme-redis-0", "10Gi"))
	src.Set(volumesGVR, acmeVolume("pvc-spare", "", "10Gi"))
	prov := &sizedProm{volumeProm: &volumeProm{}, fs: map[string]model.FilesystemSample{
		// local-path: a directory on the node's disk, which the kubelet reports as the volume's
		"acme-shop/data-acme-redis-0": {UsageBytes: 30 * volGiB, CapacityBytes: 122.9 * volGiB, AvailableBytes: 86.6 * volGiB},
		// a block volume with ext4 on it: a little smaller than its size
		"acme-shop/data-acme-pg-0": {UsageBytes: 121 * volGiB, CapacityBytes: 196.7 * volGiB, AvailableBytes: 65 * volGiB},
	}}
	s := volumeSessionOver(t, src, prov)
	const note = "Size not enforced: this 10.0 GiB volume sits on 122.9 GiB of storage and can grow past 10.0 GiB."

	redis := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "acme-shop", Name: "data-acme-redis-0"}
	volume := model.ObjectRef{Context: "test", Kind: "persistentvolumes", Name: "pvc-5c8e2a41"}
	for _, ref := range []model.ObjectRef{redis, volume} {
		openVolumeDrawer(t, s, ref)
		om := waitVolume(t, s, ref, ref.Kind+": the volume's usage", func(om *ObjectMetrics) bool { return om.Fill != nil })
		if om.SizeNote != note || om.Volume.SizeBytes != 10*volGi || om.VolumeNote != "" {
			t.Errorf("%s: size %d, note %q, volume note %q", ref.Kind, om.Volume.SizeBytes, om.SizeNote, om.VolumeNote)
		}
		// Agents read the same in object.get's volumeUsage.
		vu, err := s.VolumeUsage(ref)
		if err != nil || vu == nil || vu.Status != metrics.StatusAvailable || vu.SizeNote != note || vu.Fill == nil ||
			vu.Fill.CapacityBytes != 122.9*volGiB || vu.Volume == nil || vu.Volume.Claim != "data-acme-redis-0" {
			t.Errorf("%s: volume usage %+v, %v", ref.Kind, vu, err)
		}
	}

	// A storage that keeps to the size: no note.
	pg := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "acme-shop", Name: "data-acme-pg-0"}
	openVolumeDrawer(t, s, pg)
	if om := waitVolume(t, s, pg, "the kept size's usage", func(om *ObjectMetrics) bool { return om.Fill != nil }); om.SizeNote != "" {
		t.Errorf("a 200Gi claim with 196.7 GiB: note %q", om.SizeNote)
	}

	// A volume without a claim shows no usage and no note.
	spare := model.ObjectRef{Context: "test", Kind: "persistentvolumes", Name: "pvc-spare"}
	openVolumeDrawer(t, s, spare)
	om := waitVolume(t, s, spare, "the unbound volume's state", func(om *ObjectMetrics) bool { return om.Message == msgVolumeNotBound })
	if om.Volume != nil || om.Fill != nil || om.SizeNote != "" {
		t.Errorf("an unbound volume = %+v", om)
	}
	if vu, err := s.VolumeUsage(spare); err != nil || vu == nil || vu.Volume != nil || vu.SizeNote != "" || vu.Message != msgVolumeNotBound {
		t.Errorf("an unbound volume's usage %+v, %v", vu, err)
	}

	// Other kinds have none.
	if vu, err := s.VolumeUsage(model.ObjectRef{Context: "test", Kind: "pods", Namespace: "acme-shop", Name: "acme-redis-0"}); vu != nil || err != nil {
		t.Errorf("a pod's volume usage %+v, %v", vu, err)
	}
}
