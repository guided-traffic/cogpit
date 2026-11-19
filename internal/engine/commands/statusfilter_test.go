package commands_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// statesHarness is a session over Cogpit's own kind profiles (a pod's Status column) with four
// pods: shop/web-0 and infra/db-0 Running, shop/web-1 Pending, infra/job-0 Failed.
func statesHarness(t *testing.T) *harness {
	t.Helper()
	src := testkit.NewMemorySource()
	phase := func(ns, name, phase string) {
		src.Set(testkit.PodsGVR, testkit.Object("v1", "Pod", ns, name, map[string]any{
			"spec":   map[string]any{"containers": []any{map[string]any{"name": "app", "image": "registry.example/app:1.0"}}},
			"status": map[string]any{"phase": phase},
		}))
	}
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	phase("shop", "web-1", "Pending")
	src.Set(testkit.PodsGVR, testkit.Pod("infra", "db-0"))
	phase("infra", "job-0", "Failed")
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	h := &harness{src: src, writer: &fakeWriter{}}
	h.s = engine.New(engine.Options{Connector: enginekit.MemoryConnector{Context: "test", Src: src, Profiles: kinds.Builtin()},
		SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.Synced && len(s.Rows) == 4 })
	return h
}

func viewRowNames(v engine.ViewRows) []string {
	out := make([]string, 0, len(v.Rows))
	for i := range v.Rows {
		ref := v.RefAt(i)
		out = append(out, ref.Namespace+"/"+ref.Name)
	}
	return out
}

// TestStatusFilterCommand: ui.setStatusFilter sets the open view's status filter for every origin;
// ui.state shows it and the rows it leaves; view.rows returns the filtered rows and the states the
// view offers; select all and the CSV export take the filtered rows; resources.list takes states
// too; ui.openView opens a view filtered and another view clears it; ui.navigate clears it when it
// hides the object.
func TestStatusFilterCommand(t *testing.T) {
	h := statesHarness(t)
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI

	var rows engine.ViewRows
	h.must(t, agent, "view.rows", map[string]any{}, &rows)
	var offered []string
	for _, c := range rows.StateChoices {
		offered = append(offered, c.State+"="+string(rune('0'+c.Count))+"/"+c.Column)
	}
	if !slices.Equal(offered, []string{"Failed=1/Status", "Pending=1/Status", "Running=2/Status"}) || rows.States != nil {
		t.Fatalf("offered %v, states %v", offered, rows.States)
	}

	var st engine.UIState
	h.must(t, agent, "ui.setStatusFilter", map[string]any{"states": []string{" Running ", "Running", ""}}, &st)
	if !slices.Equal(st.States, []string{"Running"}) {
		t.Fatalf("states %v", st.States)
	}
	waitTable(t, h.s, "pods", func(s *engine.TableSnapshot) bool { return s.CaughtUp(h.s.UIState()) }) // ui.state reads the snapshot as it is
	var raw map[string]any
	h.must(t, agent, "ui.state", map[string]any{}, &raw)
	if b, _ := json.Marshal(raw["states"]); string(b) != `["Running"]` || raw["rows"] != float64(2) {
		t.Errorf("ui_state: states %s, rows %v", b, raw["rows"])
	}
	rows = engine.ViewRows{} // a fresh value: a decode keeps the cells an earlier result's rows shared
	h.must(t, agent, "view.rows", map[string]any{}, &rows)
	if got := viewRowNames(rows); !slices.Equal(got, []string{"infra/db-0", "shop/web-0"}) || rows.Shown != 2 || rows.Total != 4 ||
		!slices.Equal(rows.States, []string{"Running"}) || len(rows.StateChoices) != 3 {
		t.Errorf("view_rows %v shown %d total %d states %v", got, rows.Shown, rows.Total, rows.States)
	}
	h.must(t, agent, "ui.select", map[string]any{"all": true, "noDrawer": true}, nil)
	if got := refNames(h.s.UIState().Selected); !slices.Equal(got, []string{"infra/db-0", "shop/web-0"}) {
		t.Errorf("select all %v", got)
	}
	var csv commands.CSVResult
	h.must(t, agent, "table.exportCsv", map[string]any{}, &csv)
	if csv.Rows != 2 || strings.Contains(csv.CSV, "Pending") || strings.Contains(csv.CSV, "Failed") {
		t.Errorf("csv %d rows: %s", csv.Rows, csv.CSV)
	}

	// The window's own origin sets it the same way; an empty list clears it.
	h.must(t, gui, "ui.setStatusFilter", map[string]any{"states": []string{"Pending", "Failed"}}, nil)
	rows = engine.ViewRows{} // a fresh value: a decode keeps the cells an earlier result's rows shared
	h.must(t, agent, "view.rows", map[string]any{}, &rows)
	if got := viewRowNames(rows); !slices.Equal(got, []string{"infra/job-0", "shop/web-1"}) {
		t.Errorf("Pending or Failed: %v", got)
	}
	var cleared engine.UIState // a fresh value: an omitted field leaves a decoded one as it was
	h.must(t, gui, "ui.setStatusFilter", map[string]any{}, &cleared)
	if cleared.States != nil || h.s.UIState().States != nil {
		t.Errorf("cleared to %v", cleared.States)
	}

	// resources.list filters by states whatever the view shows.
	var list commands.ListResult
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods", "states": []string{"Pending"}}, &list)
	if list.Total != 1 || list.RefAt(0).Name != "web-1" {
		t.Errorf("resources_list %+v", list)
	}
	list = commands.ListResult{}
	h.must(t, agent, "resources.list", map[string]any{"kind": "pods", "states": []string{"Running"}, "namespaces": []string{"infra"}}, &list)
	if list.Total != 1 || list.RefAt(0).Name != "db-0" {
		t.Errorf("resources_list in infra %+v", list)
	}

	// ui.openView with states opens the view filtered; another view clears it.
	h.must(t, agent, "ui.openView", map[string]any{"view": "services"}, nil)
	h.must(t, agent, "ui.openView", map[string]any{"view": "pods", "states": []string{"Failed"}}, &st)
	if st.View != "pods" || !slices.Equal(st.States, []string{"Failed"}) {
		t.Fatalf("opened filtered: %s %v", st.View, st.States)
	}
	rows = engine.ViewRows{} // a fresh value: a decode keeps the cells an earlier result's rows shared
	h.must(t, agent, "view.rows", map[string]any{}, &rows)
	if got := viewRowNames(rows); !slices.Equal(got, []string{"infra/job-0"}) {
		t.Errorf("opened filtered rows %v", got)
	}
	var other engine.UIState
	h.must(t, agent, "ui.openView", map[string]any{"view": "services"}, &other)
	if other.States != nil || h.s.UIState().States != nil {
		t.Errorf("another view kept %v", other.States)
	}

	// ui.navigate keeps a filter that shows the object and clears one that hides it.
	h.must(t, agent, "ui.openView", map[string]any{"view": "pods", "states": []string{"Running"}}, nil)
	h.must(t, agent, "ui.navigate", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}}, nil)
	if u := h.s.UIState(); !slices.Equal(u.States, []string{"Running"}) {
		t.Errorf("navigate to a shown pod: %v", u.States)
	}
	h.must(t, agent, "ui.navigate", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "infra", Name: "job-0"}}, nil)
	if u := h.s.UIState(); u.States != nil || u.Drawer == nil || u.Drawer.Name != "job-0" {
		t.Errorf("navigate to a hidden pod: states %v, drawer %v", u.States, u.Drawer)
	}
}

// TestStatusFilterNeedsAView: without an open view the command is not enabled and says why.
func TestStatusFilterNeedsAView(t *testing.T) {
	h := statesHarness(t)
	h.s.UpdateUI(func(u *engine.UIState) { u.View = "" })
	if err := h.run(t, engine.OriginExternalAgent, "ui.setStatusFilter", map[string]any{"states": []string{"Running"}}, nil); err == nil {
		t.Fatal("set without a view")
	}
}
