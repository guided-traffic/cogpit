package metrics_test

import (
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// A drawer whose charts show only the network asks neither the Metrics Server nor Prometheus for
// CPU and memory (Subscription.SetUsage), while the network goes on at its step; shown again, the
// live values go on and the time between is filled once from Prometheus. A second subscription of
// the scope that shows CPU keeps them asked.
func TestUsageNotAskedWhileNoChartShowsIt(t *testing.T) {
	d := newDual(t)
	d.open(t)
	gets0, _ := d.live.counts()
	if gets0 == 0 || len(d.hist.ranges()) != 1 {
		t.Fatalf("the open drawer asked %d live values and %d range queries", gets0, len(d.hist.ranges()))
	}
	d.sub.SetWants(metrics.WantNetwork) // one change at a time: each kicks one poll
	d.wait(t)
	d.sub.SetUsage(false)
	d.wait(t)
	gets1, _ := d.live.counts()
	n1 := len(d.hist.ranges())
	for range 4 {
		d.next(t, 15*time.Second)
	}
	gets2, _ := d.live.counts()
	qs := d.hist.ranges()
	if gets2 != gets1 {
		t.Errorf("the network alone: %d live values asked in a minute, want none", gets2-gets1)
	}
	for _, q := range qs[n1-1:] {
		if q.Want.Has(metrics.WantUsage) {
			t.Errorf("the network alone: CPU and memory asked of Prometheus %+v", q)
		}
	}
	if len(qs) <= n1 {
		t.Errorf("the network was not asked at its step: %d range queries", len(qs))
	}
	if h := d.h.History(podA); h == nil || len(h.Targets[""].Network) == 0 {
		t.Fatalf("no network chart: %+v", h)
	}

	// Another subscriber of the scope that shows CPU: the live values are asked again.
	other := d.h.Subscribe(podA, nil)
	d.next(t, 15*time.Second)
	if g, _ := d.live.counts(); g == gets2 {
		t.Error("a subscription that shows CPU got no live value")
	}
	other.Release()
	gets3, _ := d.live.counts()
	d.next(t, 15*time.Second)
	if g, _ := d.live.counts(); g != gets3 {
		t.Errorf("after the CPU subscriber left: %d live values asked", g-gets3)
	}

	// CPU shown again: the live value, and the time without points filled once.
	d.sub.SetUsage(true)
	d.wait(t)
	d.sub.SetWants(0)
	d.wait(t)
	d.next(t, 15*time.Second)
	if g, _ := d.live.counts(); g == gets3 {
		t.Error("CPU shown again: no live value")
	}
	usage := 0
	for _, q := range d.hist.ranges() {
		if q.Want.Has(metrics.WantUsage) {
			usage++
		}
	}
	if usage < 2 {
		t.Errorf("CPU shown again: %d usage range queries in all, want the history and the gap's", usage)
	}
	d.chart(t)
}

// Charts set to the network while no provider has it (Prometheus not found any more) show CPU, in
// the engine and the window: then CPU and memory are asked whatever the subscription says.
func TestUsageAskedWhenTheNetworkHasNoProvider(t *testing.T) {
	d := newDual(t)
	d.open(t)
	d.histDet.set(nil, metrics.State{Status: metrics.StatusNotServed, Message: "no Prometheus found"})
	d.h.Forget() // kicks the drawer: its poll detects again and finds no Prometheus
	d.wait(t)
	d.sub.SetWants(metrics.WantNetwork)
	d.wait(t)
	d.sub.SetUsage(false)
	d.wait(t)
	gets0, _ := d.live.counts()
	d.next(t, 15*time.Second)
	if g, _ := d.live.counts(); g == gets0 {
		t.Error("without a provider of the network: no live value, so the CPU chart the window falls back to stays empty")
	}
}
