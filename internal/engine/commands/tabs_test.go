package commands_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// tabLine is a tab as "<title>", with " (preview)" and a star for the active one.
func tabLines(u engine.UIState) []string {
	out := make([]string, 0, len(u.Tabs))
	for _, t := range u.Tabs {
		s := t.Title
		if t.Preview {
			s += " (preview)"
		}
		if t.ID == u.ActiveTab {
			s += " *"
		}
		out = append(out, s)
	}
	return out
}

func (h *harness) tabs(t *testing.T, want ...string) engine.UIState {
	t.Helper()
	u := h.s.UIState()
	if got := tabLines(u); !slices.Equal(got, want) {
		t.Fatalf("tabs %q, want %q", got, want)
	}
	return u
}

// The tab bar's commands as agents and the GUI dispatch them: ui.openView opens in the preview tab
// as before (selection, drawer and search cleared, the namespace filter kept), ui.tab.open fixed
// tabs, ⌘n, next and previous, pin, close, back and forward; ui.state lists the tabs with titles.
// Every one runs for an agent without the user's acceptance (D-2: UI commands).
func TestTabCommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	h.tabs(t, "Pods - test (preview) *")

	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	h.must(t, gui, "ui.openView", map[string]any{"view": "services"}, nil)
	u := h.tabs(t, "Services - test (preview) *")
	if u.View != "services" || !slices.Equal(u.Namespaces, []string{"shop"}) || !slices.Equal(engine.PlaceViews(u.Tabs[0].Back), []string{"pods"}) {
		t.Errorf("openView in the preview tab: view %q namespaces %v history %q", u.View, u.Namespaces, engine.PlaceViews(u.Tabs[0].Back))
	}
	h.must(t, agent, "ui.tab.open", map[string]any{"view": "deployments", "fixed": true}, nil)
	h.tabs(t, "Services - test (preview)", "Deployments - test *")
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "web"}, nil)
	h.must(t, gui, "ui.openView", map[string]any{"view": "deployments"}, nil) // the fixed tab shows it: opened afresh
	if u := h.s.UIState(); u.Filter != "" || u.ActiveTab != "tab-2" {
		t.Errorf("openView of the active fixed tab's view: filter %q active %q", u.Filter, u.ActiveTab)
	}
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "web"}, nil)
	h.must(t, agent, "ui.tab.select", map[string]any{"index": 1}, nil)
	h.tabs(t, "Services - test (preview) *", "Deployments - test")
	h.must(t, gui, "ui.openView", map[string]any{"view": "deployments"}, nil) // focuses the fixed tab
	if u := h.s.UIState(); u.ActiveTab != "tab-2" || u.Filter != "web" {
		t.Errorf("openView of a fixed tab's view: active %q filter %q", u.ActiveTab, u.Filter)
	}
	h.must(t, agent, "ui.tab.next", nil, nil)
	h.tabs(t, "Services - test (preview) *", "Deployments - test")
	h.must(t, agent, "ui.tab.previous", nil, nil)
	h.tabs(t, "Services - test (preview)", "Deployments - test *")
	h.must(t, agent, "ui.tab.select", map[string]any{"last": true}, nil)
	h.must(t, agent, "ui.tab.select", map[string]any{"id": "tab-1"}, nil)
	if err := h.run(t, agent, "ui.tab.select", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "name the tab") {
		t.Errorf("select without a tab: %v", err)
	}
	if err := h.run(t, agent, "ui.tab.select", map[string]any{"id": "tab-9"}, nil); !errors.Is(err, engine.ErrUnknownTab) {
		t.Errorf("select an unknown tab: %v", err)
	}

	// Back and forward; disabled with the reason when there is nowhere to go.
	h.must(t, gui, "ui.tab.back", nil, nil)
	if u := h.s.UIState(); u.View != "pods" || u.ActiveTab != "tab-1" {
		t.Errorf("back: view %q in %q", u.View, u.ActiveTab)
	}
	var dis *engine.DisabledError
	if err := h.run(t, gui, "ui.tab.back", nil, nil); !errors.As(err, &dis) || dis.Reason != "no earlier view in this tab" {
		t.Errorf("back at the start: %v", err)
	}
	h.must(t, gui, "ui.tab.forward", nil, nil)
	if u := h.s.UIState(); u.View != "services" {
		t.Errorf("forward: view %q", u.View)
	}

	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	if len(st.Tabs) != 2 || st.ActiveTab != "tab-1" || st.Tabs[1].Title != "Deployments - test" || st.Tabs[1].Filter != "web" ||
		!st.Tabs[0].Preview || st.Tabs[1].Sort.Column == "" {
		t.Errorf("ui.state's tabs %+v, active %q", st.Tabs, st.ActiveTab)
	}

	h.must(t, gui, "ui.tab.pin", nil, nil)
	h.tabs(t, "Services - test *", "Deployments - test")
	h.must(t, agent, "ui.tab.close", map[string]any{"id": "tab-2"}, nil)
	h.must(t, gui, "ui.tab.close", nil, nil)
	h.tabs(t)
	if u := h.s.UIState(); u.View != "" {
		t.Errorf("no tab left, view %q", u.View)
	}
	for id, params := range map[string]any{"ui.tab.close": nil, "ui.tab.pin": nil, "ui.tab.next": nil, "ui.tab.previous": nil,
		"ui.tab.select": map[string]any{"index": 1}} {
		if err := h.run(t, gui, id, params, nil); !errors.As(err, &dis) || dis.Reason != engine.ErrNoTab.Error() {
			t.Errorf("%s without tabs: %v", id, err)
		}
	}
	if err := h.run(t, gui, "ui.tab.open", map[string]any{"view": "widgets"}, nil); !errors.Is(err, engine.ErrUnknownKind) {
		t.Errorf("an unknown view: %v", err)
	}
	h.must(t, gui, "ui.tab.open", map[string]any{"view": "workloads/overview"}, nil)
	h.tabs(t, "Overview - test (preview) *")
	if lines := h.writer.Calls(); len(lines) != 0 {
		t.Errorf("a tab command wrote to the cluster: %q", lines)
	}
}

// ui.tab.back after a link in a drawer comes back to the drawer the link was in, its view's
// namespace filter and search as they were (the owner's finding of 2026-10-25: a Deployment's pod
// opened from its drawer, back, and the Deployment's drawer was closed); ui.tab.forward goes to the
// link's object again. A link to another object of the same view is a step too.
func TestBackComesBackToTheDrawer(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	deployment := map[string]any{"kind": "deployments", "namespace": "shop", "name": "web"}
	h.must(t, gui, "ui.navigate", map[string]any{"ref": deployment}, nil)
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "shop"}, nil)
	h.must(t, gui, "ui.navigate", map[string]any{"ref": map[string]any{"kind": "pods", "namespace": "infra", "name": "web-0"}}, nil)
	h.must(t, gui, "ui.navigate", map[string]any{"ref": map[string]any{"kind": "pods", "namespace": "infra", "name": "web-1"}}, nil)

	drawer := func(u engine.UIState) string {
		if u.Drawer == nil {
			return u.View + " -"
		}
		return u.View + " " + u.Drawer.Namespace + "/" + u.Drawer.Name
	}
	var u engine.UIState
	h.must(t, gui, "ui.tab.back", nil, &u)
	if got := drawer(u); got != "pods infra/web-0" {
		t.Errorf("back once: %s", got)
	}
	h.must(t, gui, "ui.tab.back", nil, &u)
	if got := drawer(u); got != "deployments shop/web" || !slices.Equal(u.Namespaces, []string{"shop"}) || u.Filter != "shop" {
		t.Errorf("back to the deployment: %s, namespaces %v, search %q", got, u.Namespaces, u.Filter)
	}
	h.must(t, gui, "ui.tab.forward", nil, &u)
	if got := drawer(u); got != "pods infra/web-0" || len(u.Namespaces) != 0 || u.Filter != "" {
		t.Errorf("forward: %s, namespaces %v, search %q", got, u.Namespaces, u.Filter)
	}
}

// ui.navigate changes the active tab's view, which goes into its history; selecting a tab of
// another cluster connects to it.
func TestTabsFollowNavigateAndConnect(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	gui := engine.OriginGUI
	h.must(t, gui, "ui.tab.open", map[string]any{"view": "pods", "fixed": true}, nil) // the preview becomes fixed
	h.tabs(t, "Pods - test *")
	h.must(t, gui, "ui.navigate", map[string]any{"ref": map[string]any{"kind": "services", "namespace": "shop", "name": "checkout"}}, nil)
	u := h.tabs(t, "Services - test *")
	if !slices.Equal(engine.PlaceViews(u.Tabs[0].Back), []string{"pods"}) || u.Tabs[0].Drawer == nil || u.Tabs[0].Drawer.Name != "checkout" {
		t.Errorf("navigate in a tab: %+v", u.Tabs[0])
	}
	h.must(t, gui, "cluster.connect", map[string]any{"context": "other"}, nil)
	u = h.tabs(t, "Services - test", "Pods - other (preview) *")
	// By id: at other's scope the tab bar shows, and ⌘1 counts, other's tabs only (navscope.go).
	h.must(t, gui, "ui.tab.select", map[string]any{"id": u.Tabs[0].ID}, nil)
	u = h.tabs(t, "Services - test *", "Pods - other (preview)")
	if u.Context != "test" || u.Connection.State != engine.Connected || u.View != "services" || u.Drawer == nil {
		t.Errorf("back on test: %+v", u)
	}
	waitTable(t, h.s, "services", func(s *engine.TableSnapshot) bool { return s.Synced })
}
