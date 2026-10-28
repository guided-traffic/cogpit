package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A claim's and a volume's drawer show the volume's usage (D-139): the claim's scope polled while
// the two are bound and the settings show it, a chart of the filesystem alone and a usage bar.

var volumesGVR = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}

const volGiB = float64(1 << 30)

// volumeProm is a Prometheus that knows the volume of claim bezon-prod/pg-1 and no other.
type volumeProm struct {
	mu      sync.Mutex
	queries []metrics.RangeQuery
}

func (*volumeProm) ID() string    { return metrics.PrometheusID }
func (*volumeProm) Label() string { return "Prometheus (monitoring/prometheus-server)" }
func (*volumeProm) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{History: true, Network: true, Filesystem: true}
}
func (*volumeProm) ListPods(context.Context, string) ([]model.PodUsage, error) { return nil, nil }
func (*volumeProm) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (*volumeProm) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }
func (*volumeProm) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

func (p *volumeProm) ObjectRange(_ context.Context, q metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	p.mu.Lock()
	p.queries = append(p.queries, q)
	p.mu.Unlock()
	out := &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{}, From: q.From, To: q.To, Step: q.Step}
	if q.Kind != metrics.KindClaims || q.Namespace != "bezon-prod" || q.Name != "pg-1" {
		return out, nil
	}
	var fs []model.FilesystemSample
	for t := q.From; !t.After(q.To); t = t.Add(q.Step) {
		fs = append(fs, model.FilesystemSample{At: t, UsageBytes: 121 * volGiB, CapacityBytes: 196 * volGiB, AvailableBytes: 65 * volGiB})
	}
	out.Targets[""] = &metrics.TargetSeries{Filesystem: fs}
	out.FilesystemSource = metrics.FilesystemUsage
	return out, nil
}

func (p *volumeProm) asked() []metrics.RangeQuery {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.queries)
}

type volumeConnector struct {
	*fakeConnector
	prov metrics.Provider
}

func (c *volumeConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err == nil {
		b.Metrics = metrics.NewHub(metrics.Fixed(c.prov), metrics.Options{ObjectInterval: 40 * time.Millisecond, MinStep: time.Second})
	}
	return b, err
}

func claimObject(name, phase, volume string) *unstructured.Unstructured {
	return testkit.Object("v1", "PersistentVolumeClaim", "bezon-prod", name, map[string]any{
		"spec":   map[string]any{"volumeName": volume, "storageClassName": "default"},
		"status": map[string]any{"phase": phase, "capacity": map[string]any{"storage": "200Gi"}},
	})
}

func volumeObject(name, claim string) *unstructured.Unstructured {
	return testkit.Object("v1", "PersistentVolume", "", name, map[string]any{
		"spec":   map[string]any{"claimRef": map[string]any{"namespace": "bezon-prod", "name": claim}, "capacity": map[string]any{"storage": "200Gi"}},
		"status": map[string]any{"phase": "Bound"},
	})
}

// volumeSession is a session over claims and volumes with a metrics hub over prov.
func volumeSession(t *testing.T) (*Session, *testkit.MemorySource, *volumeProm) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(claimsGVR, claimObject("pg-1", "Bound", "pvc-f0327b26"))
	src.Set(claimsGVR, claimObject("idle", "Bound", "pvc-idle"))
	src.Set(claimsGVR, claimObject("new", "Pending", ""))
	src.Set(volumesGVR, volumeObject("pvc-f0327b26", "pg-1"))
	prov := &volumeProm{}
	fc := &fakeConnector{src: src, profiles: []*model.KindProfile{
		builtinProfile(t, "persistentvolumeclaims"), builtinProfile(t, "persistentvolumes"), builtinProfile(t, "pods"), testEvents,
	}}
	s := New(Options{
		Connector:        &volumeConnector{fakeConnector: fc, prov: prov},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "test")
	connectTest(t, s)
	return s, src, prov
}

// openVolumeDrawer opens the drawer on ref and builds its model until it was read from the cache,
// as the window does on every EventDetail.
func openVolumeDrawer(t *testing.T, s *Session, ref model.ObjectRef) *DetailModel {
	t.Helper()
	s.UpdateUI(func(u *UIState) { u.View, u.Drawer = testEvents.Key, &ref })
	var m *DetailModel
	waitFor(t, "the drawer's model", func() bool {
		var err error
		m, err = s.Detail(ref)
		return err == nil && !m.Loading && !m.Missing
	})
	return m
}

// waitVolume rebuilds the drawer's model (the window's part) until its Metrics section satisfies cond.
func waitVolume(t *testing.T, s *Session, ref model.ObjectRef, what string, cond func(*ObjectMetrics) bool) *ObjectMetrics {
	t.Helper()
	var om *ObjectMetrics
	waitFor(t, what, func() bool {
		if _, err := s.Detail(ref); err != nil {
			return false
		}
		var err error
		om, err = s.ObjectMetrics(ref)
		return err == nil && om != nil && cond(om)
	})
	return om
}

func TestClaimDrawerShowsItsVolume(t *testing.T) {
	s, _, prov := volumeSession(t)
	ref := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "bezon-prod", Name: "pg-1"}
	if m := openVolumeDrawer(t, s, ref); !m.Metrics {
		t.Fatal("a claim's drawer has no Metrics section")
	}
	om := waitVolume(t, s, ref, "the volume's usage", func(om *ObjectMetrics) bool { return om.Fill != nil })
	want := model.VolumeClaim{Namespace: "bezon-prod", Claim: "pg-1", Volume: "pvc-f0327b26"}
	if om.Status != metrics.StatusAvailable || om.Volume == nil || !reflect.DeepEqual(*om.Volume, want) || om.VolumeNote != "" ||
		!slices.Equal(om.Variants, []string{VariantFilesystem}) || om.FilesystemSource != metrics.FilesystemUsage {
		t.Fatalf("metrics = %+v", om)
	}
	if len(om.Targets) != 1 || om.Targets[0].Variant != VariantFilesystem || len(om.Targets[0].Filesystem) == 0 || om.Targets[0].Range != "1h" {
		t.Fatalf("targets = %+v", om.Targets)
	}
	if f := om.Fill; f.UsageBytes != 121*volGiB || f.CapacityBytes != 196*volGiB || f.AvailableBytes != 65*volGiB {
		t.Errorf("fill = %+v", f)
	}
	for _, q := range prov.asked() {
		if q.Kind != metrics.KindClaims || q.Namespace != "bezon-prod" || q.Name != "pg-1" || q.Want != metrics.WantFilesystem {
			t.Errorf("Prometheus was asked %+v", q)
		}
	}

	// Only the filesystem: another variant is refused; the range works as for any chart.
	if err := s.SetMetricsVariant(ref, "", VariantCPU); !errors.Is(err, ErrNotApplicable) {
		t.Errorf("CPU on a volume's chart: %v", err)
	}
	if err := s.SetMetricsRange(ref, "", 6*time.Hour); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the six hours asked for", func() bool {
		qs := prov.asked()
		last := qs[len(qs)-1]
		return last.To.Sub(last.From) >= 6*time.Hour-last.Step
	})
	waitVolume(t, s, ref, "the chart's range", func(om *ObjectMetrics) bool { return om.Targets[0].Range == "6h" })

	// Closed, nothing is polled.
	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	waitFor(t, "the poll released", func() bool { return s.Backend().Metrics.Subscribed() == 0 })
}

func TestVolumeDrawerShowsItsClaimsUsage(t *testing.T) {
	s, _, prov := volumeSession(t)
	ref := model.ObjectRef{Context: "test", Kind: "persistentvolumes", Name: "pvc-f0327b26"}
	openVolumeDrawer(t, s, ref)
	om := waitVolume(t, s, ref, "the volume's usage", func(om *ObjectMetrics) bool { return om.Fill != nil })
	if want := (model.VolumeClaim{Namespace: "bezon-prod", Claim: "pg-1", Volume: "pvc-f0327b26"}); om.Volume == nil || !reflect.DeepEqual(*om.Volume, want) {
		t.Errorf("volume = %+v", om.Volume)
	}
	if qs := prov.asked(); len(qs) == 0 || qs[0].Namespace != "bezon-prod" || qs[0].Name != "pg-1" {
		t.Errorf("Prometheus was asked %+v, want the claim's scope", qs)
	}
}

// Both drawers name the pods that mount the claim, sorted, and follow them: one pod (a
// ReadWriteOnce claim's), a second on the same node, none.
func TestVolumePodsFollowTheMounts(t *testing.T) {
	s, src, _ := volumeSession(t)
	claim := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "bezon-prod", Name: "pg-1"}
	volume := model.ObjectRef{Context: "test", Kind: "persistentvolumes", Name: "pvc-f0327b26"}
	src.Set(testkit.PodsGVR, podOnNode("bezon-prod", "pg-1-b", "worker-a", "pg-1"))
	src.Set(testkit.PodsGVR, podOnNode("bezon-prod", "web-0", "worker-a", "other"))
	for _, ref := range []model.ObjectRef{claim, volume} {
		openVolumeDrawer(t, s, ref)
		waitVolume(t, s, ref, ref.Kind+": one pod", func(om *ObjectMetrics) bool {
			return om.Volume != nil && slices.Equal(om.Volume.Pods, []string{"pg-1-b"})
		})
	}
	src.Set(testkit.PodsGVR, podOnNode("bezon-prod", "pg-1-a", "worker-a", "pg-1"))
	waitVolume(t, s, volume, "two pods on one node", func(om *ObjectMetrics) bool {
		return om.Volume != nil && slices.Equal(om.Volume.Pods, []string{"pg-1-a", "pg-1-b"})
	})
	src.Delete(testkit.PodsGVR, "bezon-prod", "pg-1-a")
	src.Delete(testkit.PodsGVR, "bezon-prod", "pg-1-b")
	waitVolume(t, s, volume, "no pod", func(om *ObjectMetrics) bool { return om.Volume != nil && om.Volume.Pods == nil })
}

// A volume the kubelet does not report (no pod mounts it) has an empty chart and says why; a claim
// that is not bound polls nothing until it is.
func TestVolumeWithoutUsageAndUnboundClaim(t *testing.T) {
	s, src, prov := volumeSession(t)
	idle := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "bezon-prod", Name: "idle"}
	openVolumeDrawer(t, s, idle)
	om := waitVolume(t, s, idle, "the idle volume's answer", func(om *ObjectMetrics) bool { return om.Status == metrics.StatusAvailable })
	if om.Fill != nil || om.VolumeNote != msgNoVolumeStats || len(om.Targets) != 1 || len(om.Targets[0].Filesystem) != 0 {
		t.Errorf("an unreported volume = %+v", om)
	}

	pending := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "bezon-prod", Name: "new"}
	openVolumeDrawer(t, s, pending)
	om = waitVolume(t, s, pending, "the pending claim's state", func(om *ObjectMetrics) bool { return om.Message == msgClaimNotBound })
	if om.Status != metrics.StatusNotServed || om.Volume != nil {
		t.Errorf("a pending claim = %+v", om)
	}
	waitFor(t, "the idle claim's poll released", func() bool { return s.Backend().Metrics.Subscribed() == 0 })
	before := len(prov.asked())

	// Bound meanwhile: the poll starts with the drawer's next model.
	src.Set(claimsGVR, claimObject("new", "Bound", "pvc-new"))
	waitVolume(t, s, pending, "the bound claim's answer", func(om *ObjectMetrics) bool {
		return om.Volume != nil && om.Volume.Volume == "pvc-new" && om.Status == metrics.StatusAvailable
	})
	qs := prov.asked()
	if len(qs) <= before || qs[len(qs)-1].Name != "new" {
		t.Errorf("after the binding Prometheus was asked %+v", qs[before:])
	}
}

func TestVolumesHidden(t *testing.T) {
	s, _, _ := volumeSession(t)
	for _, hide := range []MetricSet{HideVolumes, HideFilesystem} {
		if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error {
			cs.HiddenMetrics = hide
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		ref := model.ObjectRef{Context: "test", Kind: "persistentvolumeclaims", Namespace: "bezon-prod", Name: "pg-1"}
		if m := openVolumeDrawer(t, s, ref); m.Metrics {
			t.Errorf("%v hidden: the drawer has a Metrics section", hide.Labels())
		}
		om, err := s.ObjectMetrics(ref)
		if err != nil || om.Status != StatusHidden {
			t.Errorf("%v hidden: metrics %+v, %v", hide.Labels(), om, err)
		}
		if n := s.Backend().Metrics.Subscribed(); n != 0 {
			t.Errorf("%v hidden: %d scopes polled", hide.Labels(), n)
		}
	}
}
