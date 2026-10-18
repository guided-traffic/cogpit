package metrics

import (
	"context"
	"errors"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The nodes' disks (G12; resource-parity › Nodes, captured: "CPU, Memory, Disk (usage bars)"): the
// real filesystems of every node as node-exporter reports them (the root filesystem alone until
// D-90; which count as real: diskfs.go), read through Prometheus. The Metrics
// Server measures no disk, and the kubelet's summary API (/api/v1/nodes/<node>/proxy/stats/summary)
// is never used: it needs nodes/proxy, which also lets its holder run commands in any pod through
// the kubelet (privilege-footprint, Metrics). Prometheus is the only source.
//
// A scope of its own, polled like a list (the list interval, 60 s) while the Nodes list shows its
// Disk column or a node's drawer shows the disk: one instant query per poll, which answers every
// node's size and free bytes at once (diskQuery). Without Prometheus the scope's state says why
// and the column stays empty.

// KindNodeDisk is the scope of every node's filesystems: Scope{Kind: KindNodeDisk}.
const KindNodeDisk Kind = "nodeDisk"

// DiskScope is the scope of the nodes' disks.
var DiskScope = Scope{Kind: KindNodeDisk}

// DiskProvider is a provider that knows the nodes' filesystems: Prometheus, from node-exporter's
// series. The Metrics Server is not one.
type DiskProvider interface {
	// ListNodeDisks returns the real filesystems of every node that has node-exporter's series:
	// none (and no error) when the Prometheus has no such series.
	ListNodeDisks(ctx context.Context) ([]model.NodeDisk, error)
}

// ListNodeDisks implements DiskProvider: one instant query.
func (p *Prometheus) ListNodeDisks(ctx context.Context) ([]model.NodeDisk, error) {
	series, err := p.queryInstant(ctx, diskQuery())
	if err != nil {
		return nil, err
	}
	return diskUsages(series), nil
}

// Messages of the disk scope's states.
const (
	msgDiskNoPrometheus = "the disk usage comes from node-exporter through Prometheus, and no Prometheus was found"
	msgDiskSourceOff    = "the disk usage comes from Prometheus, which is switched off for this cluster"
	msgDiskNoSeries     = " has no node-exporter series of the nodes' filesystems (node_filesystem_size_bytes)"
)

// pollDisks asks the provider that knows the nodes' disks (Prometheus) for them. Prometheus is
// looked for here (the Metrics Server has no disk): a cluster without one costs no request but the
// detection's. A Prometheus without node-exporter's series answers, and the scope's state says so
// (not served) while the provider stays found for the charts.
func (h *Hub) pollDisks(ctx context.Context, sc Scope) (*dataSet, State) {
	for _, sl := range h.slots {
		if sl.role != roleLive {
			h.detect(ctx, sl)
		}
	}
	if ctx.Err() != nil {
		return nil, State{Status: StatusUnavailable, Message: msgCancelled}
	}
	h.mu.Lock()
	now := h.opts.Now()
	var (
		sl   *slot
		prov Provider
	)
	for _, s := range h.slots {
		if _, ok := s.prov.(DiskProvider); ok && h.readyLocked(s, now) {
			sl, prov = s, s.prov
			break
		}
	}
	if sl == nil {
		st := h.diskMissingLocked()
		h.mu.Unlock()
		return nil, st
	}
	h.mu.Unlock()

	disks, err := listDisks(ctx, prov.(DiskProvider))
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		s := h.failLocked(ctx, sl, sc, err)
		return nil, s
	}
	h.okLocked(sl)
	ds := &dataSet{at: h.opts.Now(), disks: make(map[string]model.NodeDisk, len(disks))}
	for _, d := range disks {
		ds.disks[d.Name] = d
	}
	if len(disks) == 0 {
		return ds, State{Status: StatusNotServed, Provider: prov.ID(), Label: prov.Label(), Message: prov.Label() + msgDiskNoSeries}
	}
	return ds, State{Status: StatusAvailable, Provider: prov.ID(), Label: prov.Label()}
}

// diskMissingLocked is the disk scope's state without a provider that knows disks: Prometheus's
// own state when it was looked for and is forbidden or does not answer, else not served.
func (h *Hub) diskMissingLocked() State {
	for _, sl := range h.slots {
		if sl.role != roleLive && sl.off {
			return State{Status: StatusNotServed, Message: msgDiskSourceOff}
		}
		if sl.role == roleLive || !sl.asked {
			continue
		}
		switch sl.state.Status {
		case StatusForbidden, StatusUnavailable:
			return sl.state
		}
	}
	return State{Status: StatusNotServed, Message: msgDiskNoPrometheus}
}

// listDisks asks a provider, recovering a panic at the boundary.
func listDisks(ctx context.Context, p DiskProvider) (d []model.NodeDisk, err error) {
	defer func() {
		if r := recover(); r != nil {
			d, err = nil, fmt.Errorf("provider panicked: %v", r)
		}
	}()
	d, err = p.ListNodeDisks(ctx)
	if errors.Is(err, ErrNoData) {
		return nil, nil
	}
	return d, err
}

// DisksPossible reports whether the cluster's providers may know disks: a hub with a Prometheus
// slot (NewHubFor's History, the engine's for a real cluster), or a single detector that found a
// provider that knows them, or a fixed one whose provider does (the demo cluster's, which makes up
// disks since D-90). A hub over the Metrics Server alone has none, so nothing subscribes the disks'
// scope there; a single detector that has not answered yet counts as none (what subscribes then
// does not poll disks until it subscribes again).
func (h *Hub) DisksPossible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sl := range h.slots {
		switch sl.role {
		case roleHistory:
			if !sl.off {
				return true
			}
		case roleAny:
			prov := sl.prov
			if f, ok := sl.det.(fixedDetector); ok && prov == nil {
				prov = f.p // known before it is asked: the demo cluster's
			}
			if _, ok := prov.(DiskProvider); ok {
				return true
			}
		}
	}
	return false
}

// DiskState is the outcome of the disk scope's last poll: available, not served (no Prometheus,
// or one without node-exporter's series; the message says which), forbidden, unavailable; unknown
// before the first poll.
func (h *Hub) DiskState() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.scopes[DiskScope]; st != nil && st.state.Status != "" {
		return st.state
	}
	return State{Status: StatusUnknown}
}

// NodeDisk returns a node's filesystems from a fresh result of the disk scope, by the first of
// keys that names one: the node's name, then its addresses (a node-exporter whose series carry
// neither a node label nor the node's name as the instance is known by its address).
func (h *Hub) NodeDisk(keys ...string) (model.NodeDisk, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.scopes[DiskScope]
	if st == nil || !h.validLocked(st) {
		return model.NodeDisk{}, false
	}
	for _, k := range keys {
		if d, ok := st.data.disks[k]; ok && k != "" {
			return d, true
		}
	}
	return model.NodeDisk{}, false
}
