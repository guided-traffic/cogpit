package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// dockStepIDs are the ids of the dock's tabs in their order.
func dockStepIDs(u engine.UIState) []string {
	var out []string
	for _, d := range u.Dock {
		out = append(out, d.ID)
	}
	return out
}

// TestMoveTabKeysMoveTheDocksTab (the owner's alpha walk, 2026-10-22, AW10: ⌘⌥⇧← and ⌘⌥⇧→ did
// nothing in the dock): View › Move Tab Left and Right move the active tab of the bar the keyboard
// works in. The dock's while the keys are in it: its Edit tab's editor, also after a click on that
// tab, which gives the editor the keys (D-115), and after ⌘⌥→ switched the tab bar's tabs, which
// leaves them there; or while nothing has them and a dock tab was clicked last (the Audit Log, which
// takes no keys). The tab bar's while the keys are elsewhere (the list's search), after a click on
// one of its tabs, or, with nothing focused, after ⌘⌥→ switched its tabs. Nothing is sent at a bar's
// end. test.Tap moves the focus as a click does: a tap on a tab button takes it away.
func TestMoveTabKeysMoveTheDocksTab(t *testing.T) {
	f := tabsFake(t)
	ed := newEditorFake(f)
	r := checkoutRef
	f.ui.Dock = []engine.DockTab{
		{ID: "audit-1", Type: engine.DockAudit, Title: "Audit Log"},
		{ID: "edit-1", Type: engine.DockEdit, Title: "Service checkout", Context: "demo", Ref: &r},
	}
	f.ui.ActiveDock = "edit-1"
	ed.setText("edit-1", serviceYAML)
	step := func(d int) func(context.Context, engine.Invocation) (any, error) { // as the engine, for both bars
		return func(_ context.Context, inv engine.Invocation) (any, error) {
			var p struct {
				Dock bool `json:"dock"`
			}
			if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
				return nil, err
			}
			f.update(func(u *engine.UIState) {
				if p.Dock {
					i := slices.IndexFunc(u.Dock, func(x engine.DockTab) bool { return x.ID == u.ActiveDock })
					tab := u.Dock[i]
					u.Dock = slices.Insert(slices.Delete(slices.Clone(u.Dock), i, i+1), i+d, tab)
					return
				}
				i := slices.IndexFunc(u.Tabs, func(x engine.ViewTab) bool { return x.ID == u.ActiveTab })
				tab := u.Tabs[i]
				u.Tabs = slices.Insert(slices.Delete(slices.Clone(u.Tabs), i, i+1), i+d, tab)
			})
			return nil, nil
		}
	}
	f.mu.Lock()
	f.handlers["ui.tab.moveLeft"], f.handlers["ui.tab.moveRight"] = step(-1), step(1)
	f.handlers["ui.tab.next"] = func(context.Context, engine.Invocation) (any, error) {
		f.update(func(u *engine.UIState) {
			i := slices.IndexFunc(u.Tabs, func(x engine.ViewTab) bool { return x.ID == u.ActiveTab })
			u.ActiveTab = u.Tabs[(i+1)%len(u.Tabs)].ID
		})
		return nil, nil
	}
	f.mu.Unlock()
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	var et *editorTab
	q.until(t, "the editor and the tabs", func() bool {
		if c, ok := sh.dock.contents["edit-1"]; ok {
			et, _ = c.editor.(*editorTab)
		}
		return et != nil && et.pane.view.Text() == serviceYAML && len(sh.tabs.shown) == 3 && sh.dock.open()
	})
	settle(w)
	view := menuNamed(sh.mainMenu(), "View")
	left, right := menuItem(t, view, "Move Tab Left"), menuItem(t, view, "Move Tab Right")
	sent := func(id string) int { return len(f.callsOf(id)) }
	tabsBefore := stateTabIDs(sh.ui)

	w.Canvas().Focus(et.pane.view) // the keys in the dock's editor
	right.Action()                 // the Edit tab is the dock's last: nothing
	left.Action()
	q.until(t, "the Edit tab first in the dock", func() bool {
		return slices.Equal(dockStepIDs(sh.ui), []string{"edit-1", "audit-1"})
	})
	if p := decodeParams[struct {
		Dock bool `json:"dock"`
	}](t, f.callsOf("ui.tab.moveLeft")[0]); !p.Dock {
		t.Errorf("Move Tab Left from the dock's editor sent %+v", p)
	}
	if n := sent("ui.tab.moveRight"); n != 0 {
		t.Errorf("Move Tab Right at the dock's end sent %d", n)
	}
	if got := stateTabIDs(sh.ui); !slices.Equal(got, tabsBefore) {
		t.Errorf("the tab bar moved: %v", got)
	}

	w.Canvas().Focus(sh.view.search)                 // elsewhere first
	test.Tap(sh.dock.tabRow.Objects[0].(*tabButton)) // a click on the shown Edit tab: its editor takes the keys
	q.until(t, "the editor's keys", func() bool { return w.Canvas().Focused() == et.pane.view })
	right.Action()
	q.until(t, "the Edit tab back", func() bool { return slices.Equal(dockStepIDs(sh.ui), []string{"audit-1", "edit-1"}) })

	w.Canvas().Focus(sh.view.search) // the keys in the list's search: the tab bar's, the dock's click before
	right.Action()                   // Pods, the active tab (second), one place right
	q.until(t, "Pods third", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-1", "tab-3", "tab-2"}) })

	test.Tap(sh.dock.tabRow.Objects[1].(*tabButton)) // the dock's again, then a tab of the tab bar
	test.Tap(sh.tabs.shown[2])
	if f := w.Canvas().Focused(); f != nil {
		t.Fatalf("a click on the tab bar's tab left the keys in %T", f)
	}
	left.Action()
	q.until(t, "Pods second again", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-1", "tab-2", "tab-3"}) })

	test.Tap(sh.dock.tabRow.Objects[0].(*tabButton)) // the Audit Log, which takes no keys: nothing has them
	q.until(t, "the Audit Log shown", func() bool { return sh.ui.ActiveDock == "audit-1" && sh.dock.active == "audit-1" })
	if f := w.Canvas().Focused(); f != nil {
		t.Fatalf("the Audit Log's click left the keys in %T", f)
	}
	right.Action() // the dock's: its tab was clicked last
	q.until(t, "the Audit Log second", func() bool { return slices.Equal(dockStepIDs(sh.ui), []string{"edit-1", "audit-1"}) })
	menuItem(t, view, "Next Tab").Action() // the tab bar's tabs switched by keys, nothing focused
	q.until(t, "the next tab", func() bool { return sh.ui.ActiveTab == "tab-3" })
	left.Action()
	q.until(t, "the third tab second", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-1", "tab-3", "tab-2"}) })

	test.Tap(sh.dock.tabRow.Objects[0].(*tabButton)) // the Edit tab: its editor takes the keys
	q.until(t, "the editor's keys again", func() bool { return sh.ui.ActiveDock == "edit-1" && w.Canvas().Focused() == et.pane.view })
	menuItem(t, view, "Next Tab").Action() // switching the tab bar's tabs leaves the keys in the editor
	q.until(t, "the next tab again", func() bool { return sh.ui.ActiveTab == "tab-2" })
	right.Action() // the dock's: the keys are there
	q.until(t, "the Edit tab last", func() bool { return slices.Equal(dockStepIDs(sh.ui), []string{"audit-1", "edit-1"}) })
	if got := stateTabIDs(sh.ui); !slices.Equal(got, []string{"tab-1", "tab-3", "tab-2"}) {
		t.Errorf("the tab bar moved: %v", got)
	}
}
