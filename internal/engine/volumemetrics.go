package engine

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A persistent volume claim's and a persistent volume's drawer show the volume's usage (D-139): a
// chart of the bytes in use and the filesystem's size over the chart's range, and a usage bar of the
// newest point. A claim holds no data itself; the drawer shows its volume's for completeness, and
// the volume's drawer the same numbers. Both come from the kubelet's volume stats through
// Prometheus, labelled by the claim (metrics/volume.go), so both drawers poll the claim's scope
// (metrics.ClaimScope) while the two are bound and the cluster's settings show it (Cluster
// Settings › Metrics: Volumes, Filesystem). The binding comes from the cache: the poll starts when
// the drawer's object is known to be bound, follows a new binding when the drawer's model is built
// again (Detail), and ends with the drawer. Under the bar a note when the storage reports a
// filesystem clearly larger than the volume's size (volumeSizeNote); agents read the section in
// object.get (VolumeUsage).

// Notes of a volume's Metrics section.
const (
	msgClaimNotBound  = "the claim is not bound to a volume"
	msgVolumeNotBound = "the volume is not bound to a claim"
	msgNoVolumeStats  = "the kubelet reports no usage of this volume now: no pod mounts it, or its driver reports none"
)

// volumeOf runs a profile's Volume, recovering a panic: a profile that panics shows no volume.
func volumeOf(p *model.KindProfile, obj *unstructured.Unstructured) (vc model.VolumeClaim, bound bool) {
	if p.Volume == nil || obj == nil {
		return model.VolumeClaim{}, false
	}
	defer func() {
		if recover() != nil {
			vc, bound = model.VolumeClaim{}, false
		}
	}()
	return p.Volume(obj)
}

// metricsScope is the scope of the drawer's charts: the pod's or node's own, or a claim's or a
// volume's claim's while the two are bound (false while not, or while the cache does not hold the
// object yet). It reads the cache, so it is called without d.wmu.
func (d *drawerWatch) metricsScope() (metrics.Scope, bool) {
	if d.profile.Volume == nil {
		return usageScope(d.profile.Usage, d.ref.Namespace, d.ref.Name), true
	}
	obj, _ := KindGet(d.obj, d.profile, d.namespace(), d.ref.Name)
	vc, bound := volumeOf(d.profile, obj)
	if !bound {
		return metrics.Scope{}, false
	}
	return metrics.ClaimScope(vc.Namespace, vc.Claim), true
}

// followDrawerVolume makes the open drawer's volume poll follow the object's binding when the drawer
// shows ref on b: started once the object is bound, moved to another claim, ended when unbound.
func (s *Session) followDrawerVolume(b *Backend, p *model.KindProfile, ref model.ObjectRef) {
	if p.Volume == nil {
		return
	}
	st := s.state
	st.mu.Lock()
	d := st.drawer
	if d == nil || d.backend != b || d.profile != p || !d.ref.Same(ref) {
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()
	d.followHidden(s, s.hiddenMetrics(b.Context))
}

// volumeMetrics is the Metrics section of a claim's or a volume's drawer: the volume's chart from
// the claim's scope while the two are bound, and the newest point as its usage bar. It reads the
// hub's latest results and the cache and sends no request.
func (s *Session) volumeMetrics(b *Backend, p *model.KindProfile, ref model.ObjectRef, out *ObjectMetrics) *ObjectMetrics {
	h := b.Acquire(p)
	obj, _ := KindGet(h, p, ref.Namespace, ref.Name)
	h.Release()
	vc, bound := volumeOf(p, obj)
	if !bound {
		out.Message = msgVolumeNotBound
		if p.Kind == "PersistentVolumeClaim" {
			out.Message = msgClaimNotBound
		}
		return out
	}
	vc.Pods = s.volumePods(b, p, ref, vc)
	out.Volume = &vc
	if b.Metrics == nil {
		return out
	}
	scope := metrics.ClaimScope(vc.Namespace, vc.Claim)
	st := b.Metrics.State(scope)
	hist := b.Metrics.History(scope)
	out.Status, out.Message, out.Source = st.Status, st.Message, st.Label
	if hist == nil {
		// Not polled yet: the hub's state is then the providers', not the volume's.
		out.Status, out.Message, out.Source = metrics.StatusUnknown, "", ""
	}
	out.Providers = b.Metrics.ProvidersFor(scope)
	if out.Status == metrics.StatusAvailable {
		out.Message = ""
		out.Capabilities = b.Metrics.Capabilities()
	}
	out.Targets = []MetricsTarget{{Variant: VariantFilesystem}}
	setWindows(out.Targets, s.chartWindows(ref), b.Metrics.DefaultWindow())
	if hist != nil && out.Status == metrics.StatusAvailable {
		out.FilesystemSource = hist.FilesystemSource
		applyHistory(out.Targets, hist)
		if out.Fill = volumeFill(hist); out.Fill == nil {
			out.VolumeNote = msgNoVolumeStats
		}
		out.SizeNote = volumeSizeNote(vc.SizeBytes, out.Fill)
	}
	return out
}

// A storage that does not keep to a volume's size (D-139): a host directory (local-path,
// hostPath) reports the node's filesystem as the volume's, whatever the claim asked for. A
// filesystem's own metadata makes a volume's filesystem a little smaller than its size, never
// larger; a storage that rounds a size up does so by a few percent at most (10G in place of 10Gi
// is 7.4 %) or, for a small volume, by its allocation unit (a 100Mi claim on a 1 GiB disk). So the
// note is said only for a filesystem more than oversizeRatio of the size and at least
// oversizeFloor beyond it.
const (
	oversizeRatio = 1.1
	oversizeFloor = 1 << 30 // bytes
)

// volumeOversized reports whether a filesystem of capacity bytes is clearly larger than a volume
// of size bytes; false without either.
func volumeOversized(size int64, capacity float64) bool {
	if size <= 0 || capacity <= 0 {
		return false
	}
	s := float64(size)
	return capacity > s*oversizeRatio && capacity-s >= oversizeFloor
}

// volumeSizeNote is the note under a volume's usage bar when its storage reports a filesystem
// clearly larger than the volume's size; "" while it keeps to it, or without a size or a newest
// point.
func volumeSizeNote(size int64, fill *model.FilesystemSample) string {
	if fill == nil || !volumeOversized(size, fill.CapacityBytes) {
		return ""
	}
	return fmt.Sprintf("The storage reports a size of %s for a volume of %s: it does not seem to keep to the volume's size.",
		model.FormatByteAmount(fill.CapacityBytes), model.FormatBytes(size))
}

// VolumeUsage is a persistent volume claim's or a persistent volume's Metrics section as object.get
// returns it to agents: the claim and the volume while the two are bound, the newest point of the
// volume's usage and the section's notes, as the drawer shows them. The numbers come while a drawer
// shows the claim or its volume, which polls them; otherwise Status is "unknown" and Fill nil.
type VolumeUsage struct {
	Status     metrics.Status          `json:"status"`
	Message    string                  `json:"message,omitempty"`
	Volume     *model.VolumeClaim      `json:"volume,omitempty"`
	Fill       *model.FilesystemSample `json:"fill,omitempty"`
	VolumeNote string                  `json:"volumeNote,omitempty"`
	SizeNote   string                  `json:"sizeNote,omitempty"`
}

// VolumeUsage returns the volume's usage of a claim or a volume as its drawer's Metrics section
// holds it, or nil, nil for another kind. It reads the hub's latest results and the cache and sends
// no request (ObjectMetrics).
func (s *Session) VolumeUsage(ref model.ObjectRef) (*VolumeUsage, error) {
	_, _, p, err := s.resolve(ref)
	if err != nil || p.Volume == nil {
		return nil, err
	}
	m, err := s.ObjectMetrics(ref)
	if err != nil || m == nil {
		return nil, err
	}
	return &VolumeUsage{Status: m.Status, Message: m.Message, Volume: m.Volume, Fill: m.Fill, VolumeNote: m.VolumeNote, SizeNote: m.SizeNote}, nil
}

// volumePods are the pods that mount a bound claim: from the open drawer's related objects, whose
// lookup makes a pod that starts or stops mounting it rebuild the drawer (both profiles' drawers
// hold PodsByClaim), else from the cache for this call, in the claim's namespace.
func (s *Session) volumePods(b *Backend, p *model.KindProfile, ref model.ObjectRef, vc model.VolumeClaim) []string {
	rs := s.drawerRelated(b, p, ref)
	if rs == nil {
		rs = newRelatedSetWith(b, drawerNeeds(b, p), nil, false, func(rp *model.KindProfile) *cache.Handle { return b.acquireNow(rp, vc.Namespace) })
	}
	defer rs.release()
	rel, _ := rs.view()
	return kinds.ClaimPodNames(vc.Namespace, vc.Claim, rel)
}

// volumeFill is the newest point of a volume's usage while the kubelet reports it: no more than two
// steps before the chart's end (the series of a volume no pod mounts any more ends where it stopped)
// and with a size. Nil without.
func volumeFill(hist *metrics.ObjectSeries) *model.FilesystemSample {
	t := hist.Targets[""]
	if t == nil || len(t.Filesystem) == 0 {
		return nil
	}
	last := t.Filesystem[len(t.Filesystem)-1]
	if last.CapacityBytes <= 0 || hist.To.Sub(last.At) > 2*hist.Step {
		return nil
	}
	return &last
}
