package metrics

import (
	"context"
	"math"
	"time"
)

// The alert board's history (engine/alerts_history.go): per alert, how often it started firing in
// each of the last 24 hours, read from the ALERTS series of the cluster's Prometheus. Two queries
// answer it for every alert at once: one range query for the onsets, one instant query for the
// alerts this Prometheus evaluates now (an alert whose rule runs in another Prometheus or a Thanos
// Ruler has no series here, and zeros would claim it never fired).

// AlertHistoryHours is how many hours AlertOnsets covers, an hour each.
const AlertHistoryHours = 24

// AlertKey names an alert's rule by the labels the history keeps apart: the alertname and the
// severity label as the rule wrote it. The engine maps the severity to a card's.
type AlertKey struct {
	Alertname string
	Severity  string
}

// AlertOnsets is Prometheus's answer for the alert board.
type AlertOnsets struct {
	// Hours are the onsets per hour of each rule that fired in them, oldest first; the last hour
	// ends at AsOf. A rule with no point in an hour started no firing in it.
	Hours map[AlertKey][AlertHistoryHours]int
	// Firing are the rules with a firing alert in this Prometheus now.
	Firing map[AlertKey]bool
	// AsOf is the end of the last hour: the request's time, cut to the minute.
	AsOf time.Time
}

// alertOnsetsQuery counts, per rule, the minutes in each hour in which a firing series exists that
// did not exist a minute earlier: one per onset. A firing alert that goes pending and fires again
// counts again; one that is only pending does not. The inner step sits on the absolute minute
// grid, so the hours do not drift.
func alertOnsetsQuery() string {
	return `sum by (alertname, severity) (count_over_time((ALERTS{alertstate="firing"} unless ALERTS{alertstate="firing"} offset 1m)[1h:1m]))`
}

// alertFiringQuery is the rules with a firing alert now.
func alertFiringQuery() string {
	return `count by (alertname, severity) (ALERTS{alertstate="firing"})`
}

// AlertOnsets asks the Prometheus for the last 24 hours of alert onsets: one range query of 24
// points an hour apart, the last at now, and one instant query, at once. An error from either is
// the answer's.
func (p *Prometheus) AlertOnsets(ctx context.Context, now time.Time) (AlertOnsets, error) {
	end := now.Truncate(time.Minute)
	start := end.Add(-(AlertHistoryHours - 1) * time.Hour)
	var (
		onsets, firing []promSeries
		oerr, ferr     error
	)
	runParallel(
		func() { onsets, oerr = p.queryRange(ctx, alertOnsetsQuery(), start, end, time.Hour) },
		func() { firing, ferr = p.queryInstant(ctx, alertFiringQuery()) },
	)
	if oerr != nil || ferr != nil {
		return AlertOnsets{}, firstStateError(oerr, ferr)
	}
	out := AlertOnsets{Hours: map[AlertKey][AlertHistoryHours]int{}, Firing: map[AlertKey]bool{}, AsOf: end}
	for _, s := range onsets {
		k, ok := alertKeyOf(s)
		if !ok {
			continue
		}
		hours := out.Hours[k]
		for _, pt := range s.points {
			i := int(math.Round(float64(pt.t-start.UnixMilli()) / float64(time.Hour.Milliseconds())))
			if i >= 0 && i < AlertHistoryHours && pt.v > 0 {
				hours[i] += int(math.Round(pt.v))
			}
		}
		out.Hours[k] = hours
	}
	for _, s := range firing {
		if k, ok := alertKeyOf(s); ok {
			out.Firing[k] = true
		}
	}
	return out, nil
}

// alertKeyOf is a series' rule; false for a series without an alertname.
func alertKeyOf(s promSeries) (AlertKey, bool) {
	name := s.labels["alertname"]
	return AlertKey{Alertname: name, Severity: s.labels["severity"]}, name != ""
}

// Prometheus returns the cluster's Prometheus for what only it answers (the alert board's history),
// from the detection the hub already made: the hub detects when it has not or its answer is due
// (DetectTTL, RetryDetectTTL), never on every call. When there is none, the state says why (not
// served, forbidden, unavailable); a cancelled ctx leaves it as it was.
func (h *Hub) Prometheus(ctx context.Context) (*Prometheus, State) {
	h.detectHistory(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sl := range h.slots {
		if sl.role != roleHistory {
			continue
		}
		if sl.off {
			return nil, State{Status: StatusNotServed, Message: "the Prometheus integration is off"}
		}
		if p, ok := sl.prov.(*Prometheus); ok {
			return p, sl.state
		}
		return nil, sl.state
	}
	return nil, State{Status: StatusNotServed, Message: "no Prometheus detection for this connection"}
}
