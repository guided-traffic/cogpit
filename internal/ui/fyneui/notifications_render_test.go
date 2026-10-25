package fyneui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// titleScene is the demo cluster over a real session: the preview tab went from Pods to Services
// (Back enabled, Forward not), and four toasts went by unseen, one of them the window's own error.
func titleScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 3
	s := newTestEngine(engine.Options{Connector: conn})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{Theme: mode, AutoConnect: engine.DemoContext}, q.do)
	sh.zone = time.UTC
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.SetConfirmer(nil)
	})
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the demo's pods", func() bool {
		return len(sh.ui.Tabs) == 1 && len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.snap.Rows) > 0
	})
	raw, _ := json.Marshal(openViewParams{View: "services"})
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "ui.openView", Params: raw, Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	until("the services", func() bool {
		return sh.view.snap != nil && sh.view.snap.View == "services" && len(sh.view.snap.Rows) > 0 && !sh.top.history.back.Disabled()
	})

	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: "Release checkout rolled back to revision 4"})
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning,
		Message: "Node worker-2: the eviction of pod checkout-api-7d9f8b6c5-x2k4p waits: Cannot evict pod as it would violate the pod's disruption budget."})
	sh.toast("error", "pods.delete: pods \"checkout-api-7d9f8b6c5-x2k4p\" is forbidden: User cannot delete resource \"pods\" in namespace \"shop\"")
	sh.toast("info", "Port forward started: http://127.0.0.1:8080 → checkout-api:8080")
	until("four unread", func() bool { return sh.ui.Notifications.Unread == 4 })
	for _, c := range toastCards(sh) { // they went by: only the bell remembers them
		sh.dismissToast(c)
	}
	return sh, w, q
}

// renderTitleHistory: the top bar with the history arrows, Back hovered with its tooltip, and the
// bell counting four unread notifications in the error colour.
func renderTitleHistory(t *testing.T, mode ThemeMode, name string) {
	sh, w, q := titleScene(t, mode)
	sh.tip.delay = time.Millisecond
	settle(w)
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(sh.top.history.back).AddXY(8, 8))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}

func TestRenderTitleHistoryDark(t *testing.T) {
	renderTitleHistory(t, ThemeDark, "titlebar_history_dark.png")
}
func TestRenderTitleHistoryLight(t *testing.T) {
	renderTitleHistory(t, ThemeLight, "titlebar_history_light.png")
}

// renderNotifications: the panel open below the bell, newest first, every row tinted as unread when
// the panel showed it; the pointer on the warning, its copy icon shown after the time, and a word
// of its text selected (WF41, D-99).
func renderNotifications(t *testing.T, mode ThemeMode, name string) {
	sh, w, q := titleScene(t, mode)
	test.Tap(sh.top.notes.bell)
	q.until(t, "the list", func() bool { return len(sh.top.notes.list) == 4 && sh.ui.Notifications.Unread == 0 })
	settle(w)
	row := findAll[*noteRow](sh.top.notes.panel)[2]
	row.MouseIn(mouse())
	text := findAll[*selParagraph](row)[0]
	_, lineH := text.metrics()
	text.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(fyne.MeasureText("Node worker-2: the evi", text.p.Theme().Size(text.p.size), text.p.style).Width, lineH/2)})
	savePNG(t, w, name)
}

func TestRenderNotificationsDark(t *testing.T) {
	renderNotifications(t, ThemeDark, "notifications_panel_dark.png")
}
func TestRenderNotificationsLight(t *testing.T) {
	renderNotifications(t, ThemeLight, "notifications_panel_light.png")
}
