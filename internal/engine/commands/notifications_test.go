package commands_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The notification commands as agents and the window dispatch them: an agent reads what the user
// was told, the window's own messages as text too since A19 (filtered); the window reads every
// text; marking read and clearing are the window's (A18): an agent is refused, and audited.
func TestNotificationCommands(t *testing.T) {
	audit := &syncBuffer{}
	h := newHarness(t, seed(), engine.Options{Audit: audit})
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	h.s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelInfo, Message: "Created services/shop/web"})
	h.s.RecordWindowToast(engine.LevelError, "pods.delete: pods \"web-0\" is forbidden")
	h.s.Emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning, Message: "Release web was not changed: quoted", WindowOnly: true})

	var forAgent, forWindow engine.NotificationList
	h.must(t, agent, "notifications.list", struct{}{}, &forAgent)
	h.must(t, gui, "notifications.list", struct{}{}, &forWindow)
	agentText := func(l engine.NotificationList) []string {
		var out []string
		for _, n := range l.Notifications {
			out = append(out, n.Level+": "+n.Message)
		}
		return out
	}
	if got, want := agentText(forAgent), []string{"warning: Release web was not changed: quoted", "error: pods.delete: pods \"web-0\" is forbidden",
		"info: Created services/shop/web"}; !slices.Equal(got, want) {
		t.Errorf("the agent reads %q, want %q", got, want)
	}
	if got := agentText(forWindow); got[1] != "error: pods.delete: pods \"web-0\" is forbidden" || got[0] != "warning: Release web was not changed: quoted" {
		t.Errorf("the window reads %q", got)
	}
	if forAgent.Unread != 3 || forAgent.UnreadLevel != engine.LevelError || forAgent.Total != 3 {
		t.Errorf("the summary: %+v", forAgent.NotificationSummary)
	}

	for _, id := range []string{"notifications.markRead", "notifications.clear"} {
		if err := h.run(t, agent, id, struct{}{}, nil); !errors.Is(err, engine.ErrNotificationsGUIOnly) {
			t.Errorf("an agent's %s: %v", id, err)
		}
	}
	var u engine.UIState
	h.must(t, gui, "notifications.markRead", map[string]any{"ids": []uint64{forAgent.Notifications[1].ID}}, &u)
	if u.Notifications.Unread != 2 || u.Notifications.UnreadLevel != engine.LevelWarning {
		t.Errorf("after the window marked the error read: %+v", u.Notifications)
	}
	var all engine.UIState
	h.must(t, gui, "notifications.markRead", struct{}{}, &all)
	if all.Notifications.Unread != 0 || all.Notifications.Total != 3 {
		t.Errorf("after the window marked every one read: %+v", all.Notifications)
	}
	var cleared engine.UIState
	h.must(t, gui, "notifications.clear", struct{}{}, &cleared)
	if cleared.Notifications != (engine.NotificationSummary{}) || len(h.s.Notifications(true).Notifications) != 0 {
		t.Errorf("after the window cleared the list: %+v", cleared.Notifications)
	}
	var done, refused []string
	for _, e := range audit.Lines() {
		switch {
		case e.Phase == "done" && e.OK:
			done = append(done, e.Command)
		case e.Phase == "done":
			refused = append(refused, e.Command)
		}
	}
	if want := []string{"notifications.list"}; !slices.Equal(done, want) {
		t.Errorf("audited %q, want %q", done, want)
	}
	if want := []string{"notifications.markRead", "notifications.clear"}; !slices.Equal(refused, want) {
		t.Errorf("audited refusals %q, want %q", refused, want)
	}
}

// A Helm action's error the toast quotes reaches agents through the notification list as text
// since A19 (owner), without the action's inline values; the window reads it whole (H-31). What a
// chart's lookup read and the filter does not know (a ConfigMap's data, a Secret the cache does not
// hold) reaches the agent: the residual risk H-31 names. The command's own error stays withheld
// from the agent.
func TestHelmErrorToastReachesAgentsFiltered(t *testing.T) {
	h, fi := installHarness(t)
	h.answer = true
	params := map[string]any{"repository": "test", "chart": "web", "version": "0.2.0", "values": "password: inline-s3cr3t\n", "namespace": "apps", "name": "web"}
	// The Helm package withholds the release's user values from its errors already (<value
	// withheld>); the notification's removal of inline values is a second line behind it.
	fi.err = errors.New("helm install apps/web: template: web/templates/cm.yaml: name LOOKED-UP-SECRET-DATA is not valid; password inline-s3cr3t")
	if _, err := h.dispatch(engine.OriginExternalAgent, "helm.chart.install", params); err == nil {
		t.Fatal("the install did not fail")
	}
	res, err := h.dispatch(engine.OriginExternalAgent, "notifications.list", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	agent := res.(engine.NotificationList)
	quoted := 0
	for _, n := range agent.Notifications {
		if strings.Contains(n.Message, "inline-s3cr3t") || n.Message == model.Withheld {
			t.Errorf("an agent reads the inline value, or nothing: %+v", n)
		}
		if strings.Contains(n.Message, "LOOKED-UP-SECRET-DATA") {
			quoted++
		}
	}
	window := h.s.Notifications(true)
	shown := slices.ContainsFunc(window.Notifications, func(n engine.Notification) bool {
		return n.WindowOnly && strings.Contains(n.Message, "LOOKED-UP-SECRET-DATA")
	})
	if quoted != 1 || !shown {
		t.Errorf("the agent's list %+v; the window's list %+v", agent.Notifications, window.Notifications)
	}
}
