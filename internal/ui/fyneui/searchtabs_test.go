package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A search typed in one tab and not sent yet (the field waits for a typing pause) when another tab
// becomes active stays with the tab it was typed in: ui.setFilter applies to the active tab, so the
// debounced send would put it into the other tab's search. The field shows the other tab's search.
func TestPendingSearchStaysWithItsTab(t *testing.T) {
	f := connectedFake(t, 20)
	f.tables["deployments"] = &engine.TableSnapshot{View: "deployments", Context: "demo", Title: "Deployments", Synced: true, Revision: 1}
	f.ui.Tabs = []engine.ViewTab{
		{ID: "tab-1", Context: "demo", View: "pods", Title: "Pods - demo"},
		{ID: "tab-2", Context: "demo", View: "deployments", Title: "Deployments - demo", Filter: "api"},
	}
	f.ui.ActiveTab = "tab-1"
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })

	sh.searchWait = time.Hour // pending until the tab switch, however slow the runner
	test.Type(sh.view.search, "web")
	f.update(func(u *engine.UIState) { u.ActiveTab, u.View, u.Filter = "tab-2", "deployments", "api" })
	q.drain()
	if fireTimer(sh.view.searchTimer) || sh.view.filterBusy != 0 {
		t.Errorf("tab-1's search is still pending after tab-2 became active (%d pending)", sh.view.filterBusy)
	}
	q.drain()
	if calls := f.callsOf("ui.setFilter"); len(calls) != 0 {
		t.Errorf("tab-1's search was sent after tab-2 became active: %+v", decodeParams[filterParams](t, calls[0]))
	}
	if sh.view.search.Text != "api" {
		t.Errorf("the search field shows %q, want tab-2's search %q", sh.view.search.Text, "api")
	}
}
