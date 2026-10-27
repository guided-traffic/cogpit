package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// statesFake is a fake on the Pods view that answers ui.setStatusFilter as the engine does: the
// pods filtered by the states their Status cells carry (model.States, the engine's rule), and every
// state the pods carry offered with its count and colour, the selected ones too.
func statesFake(t testing.TB, pods int) *fakeEngine {
	f := connectedFake(t, pods)
	full := f.tables["pods"]
	f.tables["pods"] = withStates(full, nil)
	f.handlers["ui.setStatusFilter"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p statusFilterParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.SetStates(p.States) })
		f.setTable("pods", withStates(full, p.States))
		return nil, nil
	}
	return f
}

// withStates is a fixture snapshot with a status filter applied, as the engine builds it.
func withStates(full *engine.TableSnapshot, states []string) *engine.TableSnapshot {
	spec := model.StatesOf(&model.KindProfile{Key: "pods", Columns: full.Columns})
	m := spec.Matcher(states)
	out := *full
	out.Rows = nil
	counts := map[string]*engine.StateCount{}
	for _, r := range full.Rows {
		st := spec.Of(&r)
		for _, s := range st {
			c := counts[s.State]
			if c == nil {
				c = &engine.StateCount{State: s.State, Status: s.Status, Column: "Status"}
				counts[s.State] = c
			}
			c.Count++
		}
		if m.Match(st) {
			out.Rows = append(out.Rows, r)
		}
	}
	for _, s := range states {
		if counts[s] == nil {
			counts[s] = &engine.StateCount{State: s, Column: "Status"}
		}
	}
	out.StateChoices = nil
	for _, c := range counts {
		out.StateChoices = append(out.StateChoices, *c)
	}
	slices.SortFunc(out.StateChoices, func(a, b engine.StateCount) int {
		if a.State < b.State {
			return -1
		}
		if a.State > b.State {
			return 1
		}
		return 0
	})
	out.States = slices.Clone(states)
	out.Revision = full.Revision + uint64(len(states)) + 1
	return &out
}

// stateChecks returns the open list's checks by state ("" for All states).
func stateChecks(f *statusFilter) map[string]*widget.Check {
	out := map[string]*widget.Check{}
	for _, c := range f.checks {
		out[c.state] = c.check
	}
	return out
}

// TestStatusFilterInTheHeader: the status filter sits between the namespace filter and the search;
// its list offers the states the rows carry with their counts; a check sends ui.setStatusFilter
// with the selection, and the button names it; the × clears it in one click, as All states does;
// the filter follows an agent's change; a view whose rows carry no state greys the button; on a
// page the filter sits in its table's header.
func TestStatusFilterInTheHeader(t *testing.T) {
	f := statesFake(t, 40)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.snap.StateChoices) > 0
	})
	tv := sh.view
	sf := tv.states
	if len(tv.tools.Objects) != 3 || tv.tools.Objects[0] != tv.nsBox || tv.tools.Objects[1] != sf.box || tv.tools.Objects[2] != tv.searchBox {
		t.Fatalf("the header's tools %v", tv.tools.Objects)
	}
	if sf.button.Text != allStates || sf.button.Disabled() || sf.clear.Visible() || sf.button.Importance == widget.HighImportance {
		t.Fatalf("button %q disabled %v, × %v, importance %v", sf.button.Text, sf.button.Disabled(), sf.clear.Visible(), sf.button.Importance)
	}

	test.Tap(sf.button)
	q.drain()
	if sf.popup == nil || !sf.popup.Visible() {
		t.Fatal("no list")
	}
	checks := stateChecks(sf)
	if len(checks) != len(tv.snap.StateChoices)+1 || !checks[""].Checked || checks["Running"] == nil || checks["Running"].Checked {
		t.Fatalf("checks %v", checks)
	}
	labels := []string{}
	for _, l := range findAll[*textLine](sf.list) {
		labels = append(labels, l.text)
	}
	running := 0
	for _, c := range tv.snap.StateChoices {
		if c.State == "Running" {
			running = c.Count
		}
	}
	if !slices.Contains(labels, "Running") || !slices.Contains(labels, formatCount(int64(running))) || !slices.Contains(labels, allStates) {
		t.Errorf("list texts %v", labels)
	}

	// A check: one ui.setStatusFilter with the selection; the table shows the filtered rows.
	checks["Running"].SetChecked(true)
	q.until(t, "Running", func() bool { return slices.Equal(sh.ui.States, []string{"Running"}) && tv.snap.States != nil })
	if p := decodeParams[statusFilterParams](t, f.callsOf("ui.setStatusFilter")[0]); !slices.Equal(p.States, []string{"Running"}) {
		t.Errorf("sent %+v", p)
	}
	if sf.button.Importance != widget.HighImportance { // the accent colour while a filter is active (A80)
		t.Errorf("an active filter's button is not in the accent colour: %v", sf.button.Importance)
	}
	if sf.button.Text != "Running" || !sf.clear.Visible() || stateChecks(sf)[""].Checked || len(tv.snap.Rows) != running {
		t.Errorf("after Running: button %q, × %v, rows %d of %d", sf.button.Text, sf.clear.Visible(), len(tv.snap.Rows), running)
	}
	other := ""
	for _, c := range tv.snap.StateChoices {
		if c.State != "Running" {
			other = c.State
			break
		}
	}
	stateChecks(sf)[other].SetChecked(true)
	q.until(t, "two states", func() bool { return len(sh.ui.States) == 2 && sf.button.Text == "2 states" })
	stateChecks(sf)["Running"].SetChecked(false)
	q.until(t, "one state again", func() bool { return slices.Equal(sh.ui.States, []string{other}) && sf.button.Text == other })

	// The ×: one click clears it.
	test.Tap(sf.clear)
	q.until(t, "cleared", func() bool {
		return sh.ui.States == nil && sf.button.Text == allStates && !sf.clear.Visible() && sf.button.Importance != widget.HighImportance
	})
	if p := decodeParams[statusFilterParams](t, f.callsOf("ui.setStatusFilter")[len(f.callsOf("ui.setStatusFilter"))-1]); p.States == nil || len(p.States) != 0 {
		t.Errorf("× sent %+v", p) // an empty list, never null (the schema's array)
	}
	q.until(t, "All states checked", func() bool { return stateChecks(sf)[""].Checked })

	// All states clears it too; unchecking it changes nothing.
	stateChecks(sf)["Running"].SetChecked(true)
	q.until(t, "Running again", func() bool { return len(sh.ui.States) == 1 })
	stateChecks(sf)[""].SetChecked(true)
	q.until(t, "All states", func() bool { return sh.ui.States == nil })
	n := len(f.callsOf("ui.setStatusFilter"))
	stateChecks(sf)[""].SetChecked(false)
	q.drain()
	if len(f.callsOf("ui.setStatusFilter")) != n || !stateChecks(sf)[""].Checked {
		t.Error("unchecking All states sent something")
	}
	sf.popup.Hide()

	// An agent sets it: the button follows.
	go f.update(func(u *engine.UIState) { u.SetStates([]string{"Pending", "Running", "Completed"}) })
	q.until(t, "the agent's filter", func() bool { return sf.button.Text == "3 states" && sf.clear.Visible() })

	// A view whose rows carry no state: the button is greyed.
	go func() {
		f.update(func(u *engine.UIState) { u.SetStates(nil) })
		snap := *f.Table("pods")
		snap.StateChoices, snap.Revision = nil, snap.Revision+100
		f.setTable("pods", &snap)
	}()
	q.until(t, "no states", func() bool { return sf.button.Disabled() && sf.button.Text == allStates })
}

// TestStatusFilterOnAPage: a page's status filter filters its table, so it sits in the table's
// header beside the search, the namespace filter alone in the page's.
func TestStatusFilterOnAPage(t *testing.T) {
	f := statesFake(t, 5)
	f.tables[engine.FluxDashboard] = &engine.TableSnapshot{View: engine.FluxDashboard, Title: "Flux Dashboard", Synced: true, Namespaced: true,
		StateChoices: []engine.StateCount{{State: "Not Ready", Count: 2, Status: model.StatusError, Column: "Status"}}}
	f.handlers["flux.dashboard"] = func(context.Context, engine.Invocation) (any, error) { return engine.FluxDashboardModel{}, nil }
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	go f.update(func(u *engine.UIState) { u.View = engine.FluxDashboard })
	q.until(t, "the page", func() bool {
		return sh.view.pageMode && sh.view.snap != nil && sh.view.snap.View == engine.FluxDashboard
	})
	tv := sh.view
	if len(tv.tools.Objects) != 1 || tv.tools.Objects[0] != tv.nsBox {
		t.Errorf("the page's header %v", tv.tools.Objects)
	}
	if len(tv.eventsTools.Objects) != 2 || tv.eventsTools.Objects[0] != tv.states.box || tv.eventsTools.Objects[1] != tv.searchBox {
		t.Errorf("the table's header %v", tv.eventsTools.Objects)
	}
	if tv.states.button.Disabled() {
		t.Error("the page's states are greyed")
	}
}

// renderStatusFilter renders the Pods view filtered to two states, the list open when menu is set.
func renderStatusFilter(t *testing.T, mode ThemeMode, name string, menu bool) {
	f := statesFake(t, 40)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.snap.StateChoices) > 0 && sh.diag.total > 0
	})
	sh.dispatch("ui.setStatusFilter", statusFilterParams{States: []string{"CrashLoopBackOff", "Pending"}}, nil)
	q.until(t, "filtered", func() bool { return len(sh.view.snap.States) == 2 && sh.view.states.button.Text == "2 states" })
	settle(w)
	if menu {
		test.Tap(sh.view.states.button)
		q.drain()
	}
	savePNG(t, w, name)
}

func TestRenderStatusFilterMenuDark(t *testing.T) {
	renderStatusFilter(t, ThemeDark, "status_filter_menu_dark.png", true)
}
