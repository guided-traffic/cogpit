package engine

import (
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// diagnosticsInterval is the shortest time between two EventDiagnostics.
const diagnosticsInterval = time.Second

// Diagnostics returns the request counters and cache stats of the shown cluster's backend, without
// the memory estimate (the status bar reads it every second).
func (s *Session) Diagnostics() Diagnostics {
	return s.diagnosticsOf(s.UIState().Context, s.Backend(), false)
}

// diagnosticsOf is the counters of one backend, with each watch's memory estimated when memory is
// set (the Diagnostics view and diagnostics_get: cache.Cache.StatsWithMemory). Without a backend
// they are empty and name contextID.
func (s *Session) diagnosticsOf(contextID string, b *Backend, memory bool) Diagnostics {
	d := Diagnostics{Context: contextID, Snapshots: s.state.snapshots.Load(), Informers: []cache.InformerStats{}}
	if b == nil {
		return d
	}
	d.Context, d.Demo = b.Context, b.Demo
	if b.Conn != nil {
		if c := b.Conn.Counter(); c != nil {
			d.Requests = c.Snapshot()
		}
		info := b.Conn.DiscoveryInfo()
		d.Discovery = &info
		d.Problems = b.Conn.DiagLog().Problems()
	}
	if b.Cache != nil && b.Cache.Listing() {
		cs, _ := s.ClusterSettingsOf(b.Context) // none kept: a stall of this connection's
		d.Streams = streamsText(b, cs)
	}
	switch {
	case b.Cache != nil && memory:
		d.Informers = b.Cache.StatsWithMemory()
	case b.Cache != nil:
		d.Informers = b.Cache.Stats()
	}
	if b.DemoStats != nil {
		d.DemoStats = s.demoStats(b)
	}
	return d
}

// demoStats calls the demo cluster's counters; a panic there must not break the status bar.
func (s *Session) demoStats(b *Backend) (out any) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("engine: demo stats panicked", "panic", fmt.Sprint(r))
			out = nil
		}
	}()
	return b.DemoStats()
}

// diagnosticsLoop emits EventDiagnostics at most once per second while a cluster is connected, and
// only when a counter of one of the connected clusters moved (D-69: every connected cluster's, not
// only the shown one's).
func (s *Session) diagnosticsLoop(stop <-chan struct{}) {
	t := time.NewTicker(diagnosticsInterval)
	defer t.Stop()
	var last string
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		fp := fmt.Sprint(s.state.snapshots.Load())
		for _, b := range s.Backends() {
			d := s.diagnosticsOf(b.Context, b, false)
			var events int64
			for _, inf := range d.Informers {
				events += inf.Events
			}
			fp += fmt.Sprint(" ", b.Context, d.Requests.Total, len(d.Informers), events, b.diagLog().Changes())
		}
		if fp == last {
			continue
		}
		last = fp
		select {
		case <-stop:
			return
		default:
		}
		s.emit(Event{Type: EventDiagnostics})
	}
}
