package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// notesShell is a shell over a real session without a cluster, whose clock starts at fixedNow and
// moves a minute per notification, the times shown in UTC.
func notesShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *engine.Session) {
	t.Helper()
	var tick atomic.Int64
	s := newTestEngine(engine.Options{Now: func() time.Time { return fixedNow.Add(time.Duration(tick.Add(1)) * time.Minute) }})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{Theme: mode}, q.do)
	sh.now = func() time.Time { return fixedNow.Add(time.Hour) }
	sh.zone = time.UTC
	sh.confirmer.armDelay = 0
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.SetConfirmer(nil)
	})
	return sh, w, q, s
}

// shownNotes is what the open panel lists, newest first.
func shownNotes(n *notificationsUI) []string {
	out := make([]string, 0, len(n.list))
	for _, it := range n.list {
		out = append(out, it.Level+": "+it.Message)
	}
	return out
}

// tintedRows counts the panel's rows tinted as unread when it showed them.
func tintedRows(n *notificationsUI) int {
	c := 0
	for _, s := range findAll[*surface](n.panel) {
		if s.fill == colorRowHover {
			c++
		}
	}
	return c
}

func bellRendererOf(sh *shell) *bellRenderer {
	return test.WidgetRenderer(sh.top.notes.bell).(*bellRenderer)
}

// The bell counts the toasts the user has not seen, the engine's and the window's own, in the
// colour of the most severe; a click opens the panel, which lists them newest first with the
// window's own texts in full, tints them and marks them read; a toast shown while it is open joins
// the list; Escape and a click outside close it; closing a toast by its × marks it read; "Clear
// all" empties the list. An agent reads the window's own texts withheld.
// A copy's confirmation shows as a toast and is not recorded: the bell does not count it and the
// panel does not list it (the owner's F5, 2026-10-15); a toast of anything else still is.
func TestCopyToastIsNotRecorded(t *testing.T) {
	sh, _, q, _ := notesShell(t, ThemeDark)
	sh.copyToast("info", copiedToast)
	sh.copyToast("info", "Copied web-0")
	if len(toastCards(sh)) != 2 {
		t.Fatalf("%d toasts shown, want the two copies", len(toastCards(sh)))
	}
	sh.toast("info", "Created services/shop/web")
	q.until(t, "one unread", func() bool { return sh.ui.Notifications.Unread == 1 })
	q.drain()
	if got := sh.ui.Notifications.Unread; got != 1 {
		t.Errorf("%d unread, want only the create", got)
	}
}

func TestNotificationBellAndPanel(t *testing.T) {
	sh, w, q, s := notesShell(t, ThemeDark)
	n := sh.top.notes
	if !n.bell.Visible() || bellRendererOf(sh).badge.Visible() {
		t.Fatal("the bell is hidden, or counts without notifications")
	}
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: "Created services/shop/web"})
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning, Message: "Node n1: the eviction of pod web-0 waits"})
	sh.toast("error", "pods.delete: pods \"web-0\" is forbidden") // the window's own, as a dispatch error
	q.until(t, "three unread", func() bool { return sh.ui.Notifications.Unread == 3 })
	r := bellRendererOf(sh)
	if !r.badge.Visible() || r.count.Text != "3" || r.badge.FillColor != themeColor(sh.app.Settings().Theme(), colorStatusError) {
		t.Errorf("badge %q visible %v colour %v", r.count.Text, r.badge.Visible(), r.badge.FillColor)
	}
	if got := n.bell.tipText(); got != "Notifications: 3 unread" {
		t.Errorf("tooltip %q", got)
	}

	test.Tap(n.bell)
	if !n.visible() {
		t.Fatal("the panel did not open")
	}
	want := []string{"error: pods.delete: pods \"web-0\" is forbidden", "warning: Node n1: the eviction of pod web-0 waits", "info: Created services/shop/web"}
	q.until(t, "the list", func() bool { return slices.Equal(shownNotes(n), want) })
	q.until(t, "every one read", func() bool { return sh.ui.Notifications.Unread == 0 })
	settle(w)
	if tintedRows(n) != 3 || bellRendererOf(sh).badge.Visible() {
		t.Errorf("%d rows tinted, badge visible %v", tintedRows(n), bellRendererOf(sh).badge.Visible())
	}
	times := findAll[*textLine](n.panel)
	if !slices.ContainsFunc(times, func(l *textLine) bool { return l.text == "14:31:00" }) {
		t.Error("the first notification's time is not shown as 14:31:00")
	}

	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: "Release web uninstalled"})
	q.until(t, "the new one on top", func() bool { return len(n.list) == 4 && n.list[0].Message == "Release web uninstalled" })
	q.until(t, "it read too", func() bool { return sh.ui.Notifications.Unread == 0 })

	sh.win.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if n.visible() {
		t.Fatal("Escape left the panel open")
	}

	// Closing a toast by its × marks its notification read; one that expires stays unread.
	s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning, Message: "Context demo lost its credentials"})
	q.until(t, "one unread", func() bool {
		return sh.ui.Notifications.Unread == 1 && sh.ui.Notifications.UnreadLevel == engine.LevelWarning
	})
	cards := toastCards(sh)
	last := cards[len(cards)-1]
	test.Tap(findAll[*toastButton](last)[0])
	q.until(t, "the closed toast read", func() bool { return sh.ui.Notifications.Unread == 0 })

	// An agent reads what the engine said, and the window's own text filtered (A19; withheld before).
	raw, _ := json.Marshal(struct{}{})
	res, err := s.Dispatch(context.Background(), engine.Invocation{ID: "notifications.list", Params: raw, Origin: engine.OriginExternalAgent})
	if err != nil {
		t.Fatal(err)
	}
	agent := res.(engine.NotificationList)
	if len(agent.Notifications) != 5 || agent.Notifications[2].Message != "pods.delete: pods \"web-0\" is forbidden" || agent.Notifications[3].Message != "Node n1: the eviction of pod web-0 waits" {
		t.Errorf("the agent reads %+v", agent.Notifications)
	}

	test.Tap(n.bell)
	q.until(t, "five listed", func() bool { return len(n.list) == 5 })
	clearAll := panelButton(t, n.panel, "Clear all")
	test.Tap(clearAll)
	q.until(t, "an empty list", func() bool { return len(n.list) == 0 && sh.ui.Notifications.Total == 0 })
	settle(w)
	if b := panelButton(t, n.panel, "Clear all"); !b.Disabled() {
		t.Error("Clear all is enabled on an empty list")
	}
	if !slices.ContainsFunc(findAll[*textLine](n.panel), func(l *textLine) bool { return l.text == "No notifications" }) {
		t.Error("the empty list does not say so")
	}
	n.panel.Tapped(&fyne.PointEvent{Position: fyne.NewPos(20, 600)})
	if n.visible() {
		t.Error("a click outside left the panel open")
	}
}

// The crash report the last run left (D-11) is a warning toast at start, and in the list.
func TestCrashNoticeInTheList(t *testing.T) {
	sh, _, q, _ := notesShell(t, ThemeDark)
	notice := "Cogpit ended unexpectedly on 2026-10-11 14:00. The crash report is in /tmp/crashes/crash-1.log; please attach it when you report the problem."
	sh.toast("warning", notice) // what Run does with Options.StartWarning
	q.until(t, "one unread", func() bool { return sh.ui.Notifications.Unread == 1 })
	test.Tap(sh.top.notes.bell)
	q.until(t, "the notice", func() bool { return slices.Equal(shownNotes(sh.top.notes), []string{"warning: " + notice}) })
}

// panelButton returns the button labelled text below root.
func panelButton(t *testing.T, root fyne.CanvasObject, text string) *widget.Button {
	t.Helper()
	for _, b := range findAll[*widget.Button](root) {
		if b.Text == text {
			return b
		}
	}
	t.Fatalf("no button %q", text)
	return nil
}

func TestBadgeText(t *testing.T) {
	for in, want := range map[int]string{0: "", -1: "", 1: "1", 99: "99", 100: "99+", 250: "99+"} {
		if got := badgeText(in); got != want {
			t.Errorf("badgeText(%d) = %q, want %q", in, got, want)
		}
	}
}
