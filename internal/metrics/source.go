package metrics

// Which providers a cluster's metrics come from (Lens: a "Metrics source" dropdown, Automatic,
// Prometheus, Kubernetes Metrics Server, No metrics, captured; Cogpit: the switches of the two
// metrics integrations, each on its section of Cluster Settings since D-87, and
// cluster.settings.set's metricsSource). Automatic is Cogpit's normal case, both providers together
// (the owner's requirement, G12); the others turn one provider or both off for the cluster: an off
// provider is never detected nor asked, and leaves the source row under the charts.

// Source is a cluster's metrics source.
type Source string

// The metrics sources.
const (
	// SourceAuto: both providers, each for its part (the default).
	SourceAuto Source = ""
	// SourcePrometheus: Prometheus alone, history and current values; the Metrics Server is off.
	SourcePrometheus Source = "prometheus"
	// SourceMetricsServer: the Metrics Server alone, current values only; Prometheus is off (no
	// history, network, filesystem or disk).
	SourceMetricsServer Source = "metrics-server"
	// SourceNone: no metrics; nothing is asked.
	SourceNone Source = "none"
)

// msgSourceNone is the state of a cluster whose metrics are turned off: both metrics integrations
// switched off (Cluster Settings › Metrics Server and › Prometheus, D-87; before, the Metrics source
// dropdown).
const msgSourceNone = "the Metrics Server and Prometheus are switched off for this cluster"

// allows reports whether the source leaves a provider of a role on. A single detector (the demo
// cluster's) is on unless metrics are off.
func (s Source) allows(r slotRole) bool {
	switch s {
	case SourceNone:
		return false
	case SourcePrometheus:
		return r != roleLive
	case SourceMetricsServer:
		return r != roleHistory
	}
	return true
}

// SetSource turns the cluster's providers on or off as the source says and reports whether that
// changed anything. A provider turned off is forgotten (found again when it is turned back on),
// the scopes' results and charts start over, and every provider still on is detected again at the
// next poll: the numbers shown came from a mix the user no longer wants. It sends nothing itself.
func (h *Hub) SetSource(s Source) bool {
	h.mu.Lock()
	changed := false
	for _, sl := range h.slots {
		off := !s.allows(sl.role)
		if off == sl.off {
			continue
		}
		changed = true
		sl.off = off
		if off {
			sl.prov, sl.state, sl.asked = nil, State{}, false
		}
	}
	if changed {
		for _, st := range h.scopes {
			st.data, st.state = nil, State{}
			if st.chart != nil {
				st.chart = newChart()
			}
		}
		h.retained, h.retainOrder, h.retainPoints = nil, nil, 0
	}
	h.mu.Unlock()
	if changed {
		h.Forget() // every provider still on is detected again; open charts ask their history again
	}
	return changed
}

// offStateLocked is the state of a hub whose every provider the source turns off.
func (h *Hub) offStateLocked() (State, bool) {
	for _, sl := range h.slots {
		if !sl.off {
			return State{}, false
		}
	}
	if len(h.slots) == 0 {
		return State{}, false
	}
	return State{Status: StatusNotServed, Message: msgSourceNone}, true
}
