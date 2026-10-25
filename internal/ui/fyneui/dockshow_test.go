package fyneui

import (
	"context"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestEditOnTheShownEditTabOpensTheDock (the owner's alpha walk, 2026-10-22, AW9): Edit on the
// object whose Edit tab is the dock's active tab, with the dock minimised, opens the dock on that
// tab. The engine opens it (UIState.ShowDockTab, engine/layout.go) with the active tab unchanged;
// the window follows the open state alone: the panel with the tab's editor, the strip on it.
func TestEditOnTheShownEditTabOpensTheDock(t *testing.T) {
	sh, _, q, f, _, et := editScene(t, ThemeDark)
	f.mu.Lock()
	f.handlers["object.edit"] = func(context.Context, engine.Invocation) (any, error) { // as Session.OpenEditTab: the open tab
		f.update(func(u *engine.UIState) { u.ShowDockTab("edit-1") })
		return nil, nil
	}
	f.mu.Unlock()
	f.update(func(u *engine.UIState) { u.DockOpen = false }) // minimised
	q.until(t, "the dock collapsed", func() bool { return !sh.dock.open() && len(sh.dock.collapsedBar.Objects) > 0 })
	sh.dispatch("object.edit", refParams{Ref: checkoutRef}, nil)
	q.until(t, "the dock open on the Edit tab", func() bool {
		return sh.dock.open() && sh.dock.active == "edit-1" && len(sh.dock.stripHolder.Objects) == 1 &&
			sh.body.Objects[0] == sh.dockSplit && et.pane.view.Visible()
	})
}
