package engine

import (
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
// again (Detail), and ends with the drawer.

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
	}
	return out
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
