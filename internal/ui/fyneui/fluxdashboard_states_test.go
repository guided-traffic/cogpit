package fyneui

import (
	"context"
	"slices"
	"testing"

	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Flux dashboard's tiles while a kind is still being listed and once it is (fluxdashboard.go),
// over the fake: flux.dashboard answers the reading the test publishes, so that each state is shown
// because the test asks for it, not because the engine's informers happen to be slower than the
// window (under the race detector they seldom are).

// fluxNotReadyColumns are the columns of the dashboard's table, as the engine's profile of it has
// them (engine/fluxdashboard.go).
var fluxNotReadyColumns = []model.Column{
	{ID: "name", Title: "Name", Type: model.ColumnText},
	{ID: "kind", Title: "Kind", Type: model.ColumnText, Width: 150},
	{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
	{ID: "status", Title: "Status", Type: model.ColumnText, Width: 110},
	{ID: "message", Title: "Message", Type: model.ColumnText},
	{ID: "age", Title: "Age", Type: model.ColumnAge},
}

// fluxReadingFake is the demo fake on the Flux dashboard of a cluster that serves Flux. Its
// flux.dashboard answers the last reading publish was given; publish sets it behind the fake's lock
// and publishes the page's table with it, as the engine's worker does after a change, which makes
// the window ask for the model again. Before the shell starts, publish only sets the first reading.
func fluxReadingFake(t *testing.T) (*fakeEngine, func(engine.FluxDashboardModel)) {
	f := connectedFake(t, 1)
	f.kinds = append(f.kinds, fluxKindInfos()...)
	f.ui.View = engine.FluxDashboard
	f.ui.Sort = engine.SortSpec{Column: "age", Desc: true}
	var reading engine.FluxDashboardModel
	f.handlers["flux.dashboard"] = func(context.Context, engine.Invocation) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return reading, nil
	}
	var rev uint64
	publish := func(m engine.FluxDashboardModel) {
		f.mu.Lock()
		reading = m
		f.mu.Unlock()
		rev++
		f.setTable(engine.FluxDashboard, &engine.TableSnapshot{
			View: engine.FluxDashboard, Context: "demo", Title: engine.FluxDashboardTitle, Namespaced: true, Columns: fluxNotReadyColumns,
			Rows: []model.Row{}, Sort: engine.SortSpec{Column: "age", Desc: true}, Revision: rev, Synced: m.Synced,
		})
	}
	return f, publish
}

// fluxReadingKind is a Flux kind of the dashboard's model as the engine counts it: its title and
// sub-group from the kind's profile, its states in kinds.FluxStates' order with the views' words
// and tokens, zero counts left out.
func fluxReadingKind(t *testing.T, key string, synced bool, counts map[kinds.FluxState]int) engine.FluxDashboardKind {
	t.Helper()
	info, ok := kinds.FluxKindOf(key)
	if !ok {
		t.Fatalf("%s is no Flux kind", key)
	}
	k := engine.FluxDashboardKind{Kind: key, Title: info.Title, Section: info.Section, Synced: synced}
	for _, st := range kinds.FluxStates {
		if c := counts[st]; c > 0 {
			k.States = append(k.States, engine.FluxStateCount{State: st, Title: kinds.FluxStateText(st), Status: kinds.FluxStateToken(st), Count: c})
			k.Count += c
		}
	}
	return k
}

// TestFluxDashboardTileWhileListing: a Flux kind still being listed with nothing counted yet is a
// dim card (a hairline, no plate: the card dims by its count) whose count ends in " …" and whose
// states read "No objects", where a listed kind without objects shows its "0" alone; once the kind
// is listed with objects the same card gets its plate back, the count alone, the bar and a link per
// state in place of "No objects".
func TestFluxDashboardTileWhileListing(t *testing.T) {
	const ksKey, hrKey = "kustomizations.kustomize.toolkit.fluxcd.io", "helmreleases.helm.toolkit.fluxcd.io"
	f, publish := fluxReadingFake(t)
	releases := fluxReadingKind(t, hrKey, true, nil) // listed: the cluster has none
	publish(engine.FluxDashboardModel{Kinds: []engine.FluxDashboardKind{releases, fluxReadingKind(t, ksKey, false, nil)}})
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.flux
	q.until(t, "the Kustomizations being listed", func() bool {
		k := p.tiles[ksKey]
		return k != nil && k.count.text == "0 …"
	})
	ks, hr := p.tiles[ksKey], p.tiles[hrKey]
	dim := func(tile *fluxTile) bool {
		return tile.plate.fill == theme.ColorNameBackground && tile.plate.stroke == theme.ColorNameSeparator
	}
	if got := visibleTexts(ks); !slices.Equal(got, []string{"Kustomizations", "0 …", "No objects"}) || !dim(ks) ||
		ks.bar.hasState() || len(ks.links) != 0 {
		t.Fatalf("the listing card shows %q, plate %q/%q, bar %v, links %v", got, ks.plate.fill, ks.plate.stroke, ks.bar.hasState(), ks.links)
	}
	if got := visibleTexts(hr); !slices.Equal(got, []string{"Helm Releases", "0", "No objects"}) || !dim(hr) {
		t.Fatalf("a listed kind without objects shows %q, plate %q/%q", got, hr.plate.fill, hr.plate.stroke)
	}

	// Listed: five Kustomizations in four states.
	listed := fluxReadingKind(t, ksKey, true, map[kinds.FluxState]int{
		kinds.FluxReady: 2, kinds.FluxNotReady: 1, kinds.FluxReconciling: 1, kinds.FluxSuspended: 1,
	})
	publish(engine.FluxDashboardModel{Synced: true, Kinds: []engine.FluxDashboardKind{releases, listed}})
	q.until(t, "the listed Kustomizations", func() bool { return ks.count.text == "5" })
	if p.tiles[ksKey] != ks {
		t.Fatal("the listed kind got a new card in place of the one that showed it empty")
	}
	want := []string{"Kustomizations", "5", "2 Ready", "1 Not Ready", "1 Reconciling", "1 Suspended"}
	if got := visibleTexts(ks); !slices.Equal(got, want) {
		t.Fatalf("the listed card shows %q, want %q", got, want)
	}
	if ks.plate.fill != theme.ColorNameInputBackground || ks.plate.stroke != "" {
		t.Fatalf("the card with objects keeps the empty kind's look: plate %q, hairline %q", ks.plate.fill, ks.plate.stroke)
	}
	if !ks.bar.hasState() || ks.bar.tipText() != "2 Ready · 1 Not Ready · 1 Reconciling · 1 Suspended" {
		t.Fatalf("bar shown %v, tip %q", ks.bar.hasState(), ks.bar.tipText())
	}
	for _, st := range []kinds.FluxState{kinds.FluxReady, kinds.FluxNotReady, kinds.FluxReconciling, kinds.FluxSuspended} {
		if ks.links[string(st)] == nil {
			t.Errorf("no link to the %s Kustomizations: %v", st, ks.links)
		}
	}
	if got := visibleTexts(hr); !slices.Equal(got, []string{"Helm Releases", "0", "No objects"}) || !dim(hr) {
		t.Fatalf("the kind without objects changed with the other: %q, plate %q/%q", got, hr.plate.fill, hr.plate.stroke)
	}
}
