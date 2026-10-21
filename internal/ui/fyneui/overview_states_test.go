package fyneui

import (
	"context"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestWorkloadsOverviewTileWhileListing: a workload kind still being listed shows its count so far
// with " …", the listed kind beside it its count alone; the reading after its list shows the count
// alone on the same tile. The fake answers workloads.overview with the reading the test publishes,
// so that the listing is shown on purpose, not when the engine happens to be slower than the window.
func TestWorkloadsOverviewTileWhileListing(t *testing.T) {
	f := overviewFake(t)
	var reading engine.WorkloadsOverviewModel
	f.handlers["workloads.overview"] = func(context.Context, engine.Invocation) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return reading, nil
	}
	events := *f.Table(engine.WorkloadsOverview)
	// publish sets the reading behind the fake's lock and publishes the page's events with it, as
	// the engine's worker does after a change, which makes the window ask for the model again.
	publish := func(m engine.WorkloadsOverviewModel) {
		f.mu.Lock()
		reading = m
		f.mu.Unlock()
		events.Revision++
		snap := events
		f.setTable(engine.WorkloadsOverview, &snap)
	}
	pods := engine.OverviewKind{Kind: "pods", Title: "Pods", Column: 0, Count: 1200,
		Statuses: []engine.StatusCount{{Status: model.StatusOK, Count: 1150}, {Status: model.StatusPending, Count: 50}}}
	deployments := engine.OverviewKind{Kind: "deployments", Title: "Deployments", Column: 0, Count: 3, Synced: true,
		Statuses: []engine.StatusCount{{Status: model.StatusOK, Count: 3}}}
	publish(engine.WorkloadsOverviewModel{Kinds: []engine.OverviewKind{pods, deployments}}) // the pods still listing
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.overview
	q.until(t, "the pods being listed", func() bool {
		k := p.tiles["pods"]
		return k != nil && k.count.text == "1,200 …"
	})
	tile := p.tiles["pods"]
	if got := visibleTexts(tile); !slices.Equal(got, []string{"Pods", "1,200 …"}) {
		t.Fatalf("the listing tile shows %q", got)
	}
	if got := visibleTexts(p.tiles["deployments"]); !slices.Equal(got, []string{"Deployments", "3"}) {
		t.Fatalf("a listed kind beside it shows %q", got)
	}

	// Listed.
	pods.Count, pods.Synced = 1234, true
	pods.Statuses = []engine.StatusCount{{Status: model.StatusOK, Count: 1180}, {Status: model.StatusPending, Count: 54}}
	publish(engine.WorkloadsOverviewModel{Synced: true, Kinds: []engine.OverviewKind{pods, deployments}})
	q.until(t, "the listed pods", func() bool { return tile.count.text == "1,234" })
	if p.tiles["pods"] != tile {
		t.Fatal("the listed kind got a new tile")
	}
	if got := visibleTexts(tile); !slices.Equal(got, []string{"Pods", "1,234"}) || tile.bar.tipText() != "1,180 ok · 54 pending" {
		t.Fatalf("the listed tile shows %q, bar %q", got, tile.bar.tipText())
	}
}
