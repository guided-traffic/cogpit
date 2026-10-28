package metrics

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The persistent volumes' usage (D-139): a claim's drawer and its volume's drawer show how full the
// volume is, over time and now. A claim holds no data itself; its volume does, and the kubelet that
// mounts the volume for a pod reports the volume's filesystem by the claim's name:
// kubelet_volume_stats_used_bytes, _capacity_bytes and _available_bytes with the labels namespace and
// persistentvolumeclaim, scraped from the kubelet's /metrics by kube-prometheus-stack and the
// prometheus chart alike. Prometheus is the only source: the Metrics Server measures no volume, and
// the kubelet's summary API (/api/v1/nodes/<node>/proxy/stats/summary) is never used, since
// nodes/proxy also lets its holder run commands in any pod through the kubelet (D-43,
// privilege-footprint › Metrics).
//
// A claim's scope (ClaimScope) is an object scope with a chart, polled like a drawer's while the
// drawer shows the claim or its volume: the Prometheus that serves history is asked for the chart's
// range once, then from its last point at the range's step; the Metrics Server is never asked. One
// range query per poll answers all three gauges (claimQuery). The kubelet reports a volume only
// while a pod on its node mounts it, and only when the volume's driver reports its usage (a raw
// block volume, some CSI drivers and NFS do not): then the chart is empty, which is a state of the
// claim, not of the cluster's metrics.

// KindClaims is the scope of one claim's volume: Scope{Kind: KindClaims, Namespace: ns, Name: claim}.
const KindClaims Kind = "volumeClaims"

// ClaimScope is the scope of a claim's volume.
func ClaimScope(namespace, claim string) Scope {
	return Scope{Kind: KindClaims, Namespace: namespace, Name: claim}
}

// The kubelet's volume gauges.
const (
	metricVolumeUsed      = "kubelet_volume_stats_used_bytes"
	metricVolumeCapacity  = "kubelet_volume_stats_capacity_bytes"
	metricVolumeAvailable = "kubelet_volume_stats_available_bytes"
)

// The parts of claimQuery's answer, by its "part" label.
const (
	partUsed      = "used"
	partCapacity  = "capacity"
	partAvailable = "available"
)

// claimQuery is the one query of a claim's volume: its bytes in use, its size and its free bytes,
// each series marked with a "part" label so that one answer carries all three. max by (namespace,
// persistentvolumeclaim) keeps one series per gauge however many kubelets report the volume (a
// ReadWriteMany volume mounted on several nodes is one filesystem) and however many jobs scrape them.
func claimQuery(namespace, claim string) string {
	matchers := "namespace=" + quoteLabel(namespace) + ",persistentvolumeclaim=" + quoteLabel(claim)
	part := func(metric, name string) string {
		return fmt.Sprintf(`label_replace(max by (namespace, persistentvolumeclaim) (%s{%s}), "part", %q, "", "")`, metric, matchers, name)
	}
	return part(metricVolumeUsed, partUsed) + " or " + part(metricVolumeCapacity, partCapacity) + " or " + part(metricVolumeAvailable, partAvailable)
}

// claimRange answers a range query of a claim's volume: one query, the volume's points as the
// whole object's filesystem ("usage"). A volume the kubelet does not report is an empty answer.
func (p *Prometheus) claimRange(ctx context.Context, q RangeQuery) (*ObjectSeries, error) {
	series, err := p.queryRange(ctx, claimQuery(q.Namespace, q.Name), q.From, q.To, q.Step)
	if err != nil {
		return nil, err
	}
	out := &ObjectSeries{Targets: map[string]*TargetSeries{}, From: q.From, To: q.To, Step: q.Step}
	if fs := volumeSamples(series); len(fs) > 0 {
		out.Targets[""] = &TargetSeries{Filesystem: fs}
		out.FilesystemSource = FilesystemUsage
	}
	return out, nil
}

// volumeSamples joins the three gauges of claimQuery's answer by time. A time without the bytes in
// use is left out (a size alone is no usage); a missing size or free amount is 0.
func volumeSamples(series []promSeries) []model.FilesystemSample {
	type parts struct {
		used, capacity, available float64
		hasUsed                   bool
	}
	byTime := map[int64]*parts{}
	for _, s := range series {
		for _, pt := range s.points {
			x := byTime[pt.t]
			if x == nil {
				x = &parts{}
				byTime[pt.t] = x
			}
			switch s.labels["part"] {
			case partUsed:
				x.used, x.hasUsed = max(x.used, pt.v), true
			case partCapacity:
				x.capacity = max(x.capacity, pt.v)
			case partAvailable:
				x.available = max(x.available, pt.v)
			}
		}
	}
	times := make([]int64, 0, len(byTime))
	for t, x := range byTime {
		if x.hasUsed {
			times = append(times, t)
		}
	}
	slices.Sort(times)
	out := make([]model.FilesystemSample, 0, len(times))
	for _, t := range times {
		x := byTime[t]
		out = append(out, model.FilesystemSample{At: time.UnixMilli(t), UsageBytes: x.used, CapacityBytes: x.capacity, AvailableBytes: x.available})
	}
	return out
}

// Messages of a claim scope's states.
const (
	msgVolumeNoPrometheus = "a volume's usage comes from the kubelet's volume stats through Prometheus, and no Prometheus was found"
	msgVolumeSourceOff    = "a volume's usage comes from Prometheus, which is switched off for this cluster"
)

// detectNotLive runs the detection of every provider but the live ones (the Metrics Server): what
// a poll of Prometheus's alone needs (the disks, a claim's volume).
func (h *Hub) detectNotLive(ctx context.Context) {
	for _, sl := range h.slots {
		if sl.role != roleLive {
			h.detect(ctx, sl)
		}
	}
}

// volumeMissingLocked is a claim scope's state when Prometheus did not answer: Prometheus's own
// state when it was looked for and is forbidden or does not answer, else not served.
func (h *Hub) volumeMissingLocked() State {
	return h.prometheusMissingLocked(msgVolumeNoPrometheus, msgVolumeSourceOff)
}
