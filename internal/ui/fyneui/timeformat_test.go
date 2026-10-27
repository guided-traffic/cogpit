package fyneui

import (
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Berlin on any test machine

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The window's times in the user's format (D-31). The fixtures' engine gives no layout, so most
// tests and images show ISO 8601 in UTC (the fallback); these use Germany's, the owner's, and the
// zone Europe/Berlin, to show that every place writes through the shell's format.

var (
	germanLayout = model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}
	germanTime   = germanLayout.In(time.UTC)
)

// shownStamp is a log line's timestamp as the fixtures' window shows it: ISO 8601 with
// milliseconds in UTC, the fallback without the engine's layout.
func shownStamp(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return model.TimeFormat{Zone: time.UTC}.FormatPrecise(t)
}

// inGermany gives a fixture the engine's layout Germany's and the Preferences' time zone
// Europe/Berlin: the fixtures' day is in summer time, fixedNow (14:30 UTC) is 16:30 there.
func inGermany(f *fakeEngine) {
	p := engine.DefaultPreferences()
	p.Timezone = "Europe/Berlin"
	f.ui.TimeFormat, f.ui.Preferences = germanLayout, &p
}

// TestTheWindowWritesTimesInTheUsersFormat: with the engine's layout and the Preferences' zone,
// the places the window writes a time use both: an event's Last Seen in the list, the
// notifications and the audit log (time of day today, the date before it otherwise), the drain,
// Helm and drafts banners, the metrics providers' tooltip, the charts' axes and hover panel, the
// Rollback dialog's revisions.
func TestTheWindowWritesTimesInTheUsersFormat(t *testing.T) {
	f := connectedFake(t, 10)
	inGermany(f)
	lastSeen := time.Date(2026, 10, 11, 12, 2, 0, 0, time.UTC)
	f.tables["events"] = &engine.TableSnapshot{
		View: "events", Title: "Events", Namespaced: true, Synced: true, Total: 1,
		Columns: []model.Column{
			{ID: "name", Title: "Name", Type: model.ColumnText},
			{ID: "lastSeen", Title: "Last Seen", Type: model.ColumnTime},
		},
		Rows: []model.Row{{Ref: model.ObjectRef{Kind: "events", Namespace: "default", Name: "web.1"},
			Cells: []model.Cell{{Text: "web.1"}, {Num: lastSeen.Unix()}}}},
	}
	f.ui.View = "events"
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the zone and the list", func() bool { return sh.zone != nil && len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	tf := sh.timeFormat()
	if got := tf.Format(fixedNow); got != "11.10.2026 16:30:00" {
		t.Fatalf("the shell's format writes %q", got)
	}
	shown := false
	for _, c := range sh.view.cells {
		shown = shown || c.row == 0 && c.Visible() && c.full == "11.10.2026 14:02:00"
	}
	if !shown {
		t.Error("the list does not show the event's Last Seen as 11.10.2026 14:02:00")
	}

	for _, tc := range []struct{ got, want string }{
		{sh.top.notes.timeText(fixedNow.Add(-time.Hour)), "15:30:00"},
		{sh.top.notes.timeText(fixedNow.Add(-48 * time.Hour)), "09.10.2026 16:30:00"},
		{sameDayTime(tf, fixedNow.Add(-10*time.Minute), sh.now()), "16:20:00"}, // the audit log's
		{chartData{From: fixedNow.Add(-time.Hour), To: fixedNow, Time: tf}.timeLabel(fixedNow), "16:30"},
		{chartData{From: fixedNow.Add(-48 * time.Hour), To: fixedNow, Time: tf}.timeLabel(fixedNow), "11.10. 16:30"},
		{chartData{From: fixedNow.Add(-48 * time.Hour), To: fixedNow, Time: tf}.hoverTime(fixedNow), "11.10.2026 16:30:00"},
		{helm.RevisionText(helm.HistoryEntry{Revision: 4, Chart: "web", ChartVersion: "1.0.0", AppVersion: "2.0", Updated: lastSeen}, tf),
			"4 - web-1.0.0 - 2.0, updated: 11.10.2026 14:02:00"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
	for _, tc := range []struct{ got, want string }{
		{drainDetail(fixtureDrains()[0], tf), "started 11.10.2026 15:02:00 by you"},
		{draftDetail(fixtureDrafts()[0], tf), "kept 11.10.2026 16:02:00"},
		{helmOpDetail(fixtureHelmOps()[0], tf), "started 11.10.2026 15:41:00 by you"},
		{providerTip(metrics.ProviderStatus{Label: "Prometheus", Status: metrics.StatusAvailable, LastOK: fixedNow.Add(-40 * time.Second)},
			fixedNow, tf), "Last good answer: 16:29:20 (40 s ago)"},
	} {
		if !strings.Contains(tc.got, tc.want) {
			t.Errorf("%q lacks %q", tc.got, tc.want)
		}
	}
}

// TestLogTimestampsInTheUsersFormat: a logs tab shows the server's timestamps in the user's zone
// and format with milliseconds, its download saves them so, its time field offers that format and
// shows a jump's time in it.
func TestLogTimestampsInTheUsersFormat(t *testing.T) {
	sh, _, q, f := logsSceneWith(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true}, inGermany)
	lt := logTabOf(sh)
	q.until(t, "every line in Berlin's time", func() bool {
		lines := lt.view.ShownLines()
		return len(lines) == len(demoMerged) && lines[0] == "checkout-api-7d9f8b6c4-x2k9p/api 11.10.2026 16:28:01.204 starting checkout-api version=2.14.1 commit=9f3c2e1"
	})
	if ph := lt.jumpField.PlaceHolder; ph != "Jump to time: 13.10.2026 01:20, 10m ago" {
		t.Errorf("the time field's placeholder %q", ph)
	}
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.jumpTo", Origin: engine.OriginExternalAgent,
			Params: mustJSON(t, map[string]any{"id": "logs-1", "time": "2026-10-11T14:28:12Z"})})
	}()
	q.until(t, "the jump", func() bool { return strings.Contains(lt.view.JumpedLine(), "ERROR payment") })
	if lt.jumpField.Text != "11.10.2026 16:28:12" {
		t.Errorf("the field shows the jump as %q", lt.jumpField.Text)
	}
}
