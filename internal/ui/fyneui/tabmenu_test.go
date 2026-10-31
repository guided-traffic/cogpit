package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A right-click on a tab, in the tab bar and in the dock, offers Close, Close Others and Close All,
// each dispatching its bar's command for the tab clicked; Close Others is greyed while the bar shows
// no other tab (D-144).
func TestTabMenus(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	want := []string{"Close", "Close Others", "Close All"}
	called := func(id string) engine.Invocation {
		t.Helper()
		q.until(t, id, func() bool { return len(f.callsOf(id)) == 1 })
		return f.callsOf(id)[0]
	}

	tab := sh.tabs.shown[0]
	id := tab.tab.ID // the widget shows another tab once the fake closed this one
	test.TapSecondary(tab)
	m := sh.tabs.menuModel
	if m == nil || !slices.Equal(menuLabels(m), want) || m.Items[1].Disabled {
		t.Fatalf("the tab's menu %+v", m)
	}
	m.Items[0].Action()
	m.Items[1].Action()
	m.Items[2].Action()
	if got := decodeParams[tabIDParams](t, called("ui.tab.close")).ID; got != id {
		t.Errorf("Close closed %q, want %q", got, id)
	}
	if got := decodeParams[tabIDParams](t, called("ui.tab.closeOthers")).ID; got != id {
		t.Errorf("Close Others kept %q, want %q", got, id)
	}
	called("ui.tab.closeAll")

	f.update(func(u *engine.UIState) {
		u.Dock = []engine.DockTab{{ID: "x-1", Type: "test", Title: "One"}, {ID: "x-2", Type: "test", Title: "Two"}}
		u.ActiveDock = "x-2"
	})
	q.until(t, "the dock's tabs", func() bool { return len(sh.dock.tabRow.Objects) == 2 })
	test.TapSecondary(sh.dock.tabRow.Objects[0].(*tabButton))
	m = sh.dock.menuModel
	if m == nil || !slices.Equal(menuLabels(m), want) || m.Items[1].Disabled {
		t.Fatalf("the dock tab's menu %+v", m)
	}
	m.Items[0].Action()
	m.Items[1].Action()
	m.Items[2].Action()
	if got := decodeParams[dockParams](t, called("dock.close")).ID; got != "x-1" {
		t.Errorf("Close closed %q", got)
	}
	if got := decodeParams[dockParams](t, called("dock.closeOthers")).ID; got != "x-1" {
		t.Errorf("Close Others kept %q", got)
	}
	called("dock.closeAll")

	f.update(func(u *engine.UIState) { u.Dock = u.Dock[:1] })
	q.until(t, "one dock tab", func() bool { return len(sh.dock.tabRow.Objects) == 1 })
	test.TapSecondary(sh.dock.tabRow.Objects[0].(*tabButton))
	if m = sh.dock.menuModel; !m.Items[1].Disabled {
		t.Error("Close Others offered without another tab")
	}
}
