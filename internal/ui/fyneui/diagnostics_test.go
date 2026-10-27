package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// diagnosticsFake is a fake connected to a real cluster's counters: requests by verb and resource,
// open, warm and covered watches; it answers diagnostics.get as the engine does (engine.ReportOf).
func diagnosticsFake(t *testing.T) *fakeEngine {
	f := connectedFake(t, 20)
	f.ui.Context, f.ui.Connection = "kind-dev", engine.Connection{State: engine.Connected}
	// The counters cross the seam as JSON: the UI may not import package cache to name its types.
	diag := `{"context": "kind-dev", "snapshots": 318, "requests": {"total": 41,
	  "byVerb": {"list": 6, "watch": 7, "get": 22, "discovery": 4, "create": 2},
	  "byVerbResource": {"get pods/log": 14, "get nodes": 8, "list pods": 2, "watch pods": 3, "list services": 1,
	    "watch services": 1, "list deployments.apps": 1, "watch deployments.apps": 1, "list secrets": 1, "watch secrets": 1,
	    "list nodes": 1, "watch nodes": 1, "discovery": 4, "create selfsubjectrulesreviews.authorization.k8s.io": 2}},
	  "discovery": {"source": "disk", "checked": true, "groups": 31, "resources": 212, "persisted": true},
	  "informers": [
	    {"scope": {"gvr": {"Version": "v1", "Resource": "pods"}}, "objects": 214, "handles": 2, "synced": true, "events": 1840, "bytes": 1363149},
	    {"scope": {"gvr": {"Version": "v1", "Resource": "nodes"}}, "objects": 6, "handles": 1, "synced": true, "events": 412, "bytes": 30000},
	    {"scope": {"gvr": {"Version": "v1", "Resource": "secrets"}}, "objects": 57, "handles": 1, "synced": true, "events": 57, "bytes": 120000},
	    {"scope": {"gvr": {"Version": "v1", "Resource": "secrets"}, "labelSelector": "owner=helm"}, "objects": 9, "handles": 1, "synced": true, "coveredBy": "secrets"},
	    {"scope": {"gvr": {"Group": "apps", "Version": "v1", "Resource": "deployments"}}, "objects": 31, "synced": true, "events": 31, "warmUntil": "2026-10-11T14:34:00Z", "bytes": 160000},
	    {"scope": {"gvr": {"Version": "v1", "Resource": "services"}}, "objects": 44, "synced": true, "events": 44, "warmUntil": "2026-10-11T14:32:00Z", "bytes": 130000}]}`
	f.diag = engine.Diagnostics{} // not the demo cluster's
	if err := json.Unmarshal([]byte(diag), &f.diag); err != nil {
		t.Fatal(err)
	}
	f.handlers["diagnostics.get"] = func(context.Context, engine.Invocation) (any, error) {
		r := engine.ReportOf(f.Diagnostics(), true)
		if err := json.Unmarshal([]byte(connectionDiag), &r); err != nil {
			return nil, err
		}
		return r, nil
	}
	f.handlers["diagnostics.report"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.DiagnosticsReportText{Text: "Cogpit Connection Diagnostics\nServer: https://host-1\n", Name: "cogpit-connection-diagnostics-x.txt"}, nil
	}
	f.diag.Problems = 4
	return f
}

// connectionDiag is Connection Diagnostics' part of the fake's report (D-133): a limited user on a
// server without WatchList, one aggregated API down, the requests waiting at Cogpit's rate limit.
const connectionDiag = `{"server": "https://api.ocp.example.com:6443", "serverVersion": "v1.31.4", "since": "2026-10-26T08:01:02Z",
  "budgets": [
    {"name": "requests", "qps": 5, "burst": 10, "requests": 1204, "delayed": 214, "waited": 161000000000, "longest": 38000000000, "waiting": 12},
    {"name": "discovery", "qps": 50, "burst": 300, "requests": 71},
    {"name": "access reviews", "qps": 10, "burst": 20, "requests": 3}],
  "streaming": {"streamed": 0, "listed": 0, "fellBack": 5},
  "problems": 4,
  "problemCounts": [{"name": "throttled", "count": 34}, {"name": "refused", "count": 12}, {"name": "fallback", "count": 5}, {"name": "serverError", "count": 2}],
  "log": [
    {"kind": "throttled", "problem": true, "what": "pods in team-a", "text": "Cogpit's rate limit for requests: 5 per second, burst 10",
     "took": 38000000000, "longest": 38000000000, "first": "2026-10-26T08:01:40Z", "last": "2026-10-26T08:03:10Z", "count": 34},
    {"kind": "fallback", "problem": true, "what": "pods in team-a", "text": "the server did not stream the initial list (WatchList); listed instead: sendInitialEvents is forbidden for watch unless the WatchList feature gate is enabled",
     "first": "2026-10-26T08:01:05Z", "last": "2026-10-26T08:01:05Z", "count": 1},
    {"kind": "namespaced", "problem": false, "what": "pods", "text": "the user may not list it in all namespaces: read namespace by namespace, in team-a",
     "first": "2026-10-26T08:01:04Z", "last": "2026-10-26T08:01:04Z", "count": 1},
    {"kind": "refused", "problem": true, "what": "watchlist pods", "status": 403, "method": "GET", "url": "https://api.ocp.example.com:6443/api/v1/pods?watch=true&sendInitialEvents=true",
     "text": "pods is forbidden: User \"jdoe\" cannot list resource \"pods\" in API group \"\" at the cluster scope",
     "first": "2026-10-26T08:01:04Z", "last": "2026-10-26T08:02:30Z", "count": 12},
    {"kind": "serverError", "problem": true, "what": "discovery /apis/metrics.k8s.io/v1beta1", "status": 503, "method": "GET", "url": "https://api.ocp.example.com:6443/apis/metrics.k8s.io/v1beta1",
     "text": "service unavailable", "first": "2026-10-26T08:01:03Z", "last": "2026-10-26T08:01:03Z", "count": 2},
    {"kind": "connect", "problem": false, "what": "https://api.ocp.example.com:6443", "first": "2026-10-26T08:01:02Z", "last": "2026-10-26T08:01:02Z", "count": 1}]}`

// openDiagnostics adds the Diagnostics tab to the fake's dock, as diagnostics.open does.
func openDiagnostics(f *fakeEngine) {
	f.update(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "diagnostics-1", Type: engine.DockDiagnostics, Title: "Connection Diagnostics"})
		u.ActiveDock = "diagnostics-1"
	})
}

func diagnosticsTabOf(sh *shell) *diagnosticsTab {
	for _, c := range sh.dock.contents {
		if d, ok := c.editor.(*diagnosticsTab); ok {
			return d
		}
	}
	return nil
}

// The Diagnostics tab shows the counters in full: the requests by verb and resource, most first,
// the watches open, then warm, then covered, and the objects and memory per kind, the most memory
// first, their sums and where discovery came from; it follows the counters when they move
// (EventDiagnostics).
func TestDiagnosticsTab(t *testing.T) {
	f := diagnosticsFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	openDiagnostics(f)
	q.until(t, "the Diagnostics", func() bool { d := diagnosticsTabOf(sh); return d != nil && len(d.watches.Objects) == 6 })
	d := diagnosticsTabOf(sh)
	text := func(o any) string { return o.(*textLine).text }
	if got := text(d.requests.Objects[0]); !strings.Contains(got, "14  get pods/log") {
		t.Errorf("first request row %q", got)
	}
	var watches []string
	for _, o := range d.watches.Objects {
		watches = append(watches, text(o))
	}
	if !strings.HasPrefix(watches[0], "nodes · open") || !strings.HasPrefix(watches[2], "secrets · open") ||
		!strings.HasPrefix(watches[3], "deployments.apps · warm") || !strings.Contains(watches[5], "served by the secrets watch") {
		t.Errorf("watches %q", watches)
	}
	if got := text(d.kinds.Objects[0]); !strings.Contains(got, "214     1.3 MiB  pods") || len(d.kinds.Objects) != 5 {
		t.Errorf("kinds: first %q, %d kinds", got, len(d.kinds.Objects))
	}
	if got := text(d.kinds.Objects[1]); !strings.Contains(got, "31   156.2 KiB  deployments.apps") {
		t.Errorf("kinds: second %q, want the next most memory", got)
	}
	for _, want := range []string{"41 API requests since connecting", "3 open watches, 2 warm, 1 covered",
		"352 objects held (1.7 MiB estimated)", "discovery from the disk cache"} {
		if !strings.Contains(d.summary.text, want) {
			t.Errorf("summary %q, want %q", d.summary.text, want)
		}
	}

	f.mu.Lock()
	f.diag.Requests.Total = 99
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventDiagnostics})
	q.until(t, "the new counters", func() bool { return strings.Contains(d.summary.text, "99 API requests") })
}

// Connection Diagnostics (D-133): the tab shows the connection, the problems since the latest
// connect with how the initial lists came, Cogpit's rate limits, and the log, each line in its
// severity's colour; Report shows the anonymised report, and Copy puts exactly that on the
// clipboard.
func TestDiagnosticsTabConnection(t *testing.T) {
	f := diagnosticsFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	openDiagnostics(f)
	q.until(t, "the log", func() bool { d := diagnosticsTabOf(sh); return d != nil && len(d.log.Objects) == 6 })
	d := diagnosticsTabOf(sh)
	if got := d.connection.line.text; !strings.Contains(got, "Server https://api.ocp.example.com:6443 · Kubernetes v1.31.4 · latest connect") {
		t.Errorf("connection %q", got)
	}
	if got := d.problems.text; !strings.HasPrefix(got, "Since the latest connect: rate limited 34 · refused 12 · not streamed 5 · server error 2") ||
		!strings.Contains(got, "5 listed after the server did not stream them") || d.problems.color != colorStatusWarning {
		t.Errorf("problems %q in %s", got, d.problems.color)
	}
	if got := d.budgets.text; !strings.Contains(got, "requests 5/s (burst 10): 214 of 1,204 waited 2 min 41 s, the longest 38 s, 12 waiting now") ||
		!strings.Contains(got, "discovery 50/s (burst 300): none waited") || d.budgets.color != colorStatusWarning {
		t.Errorf("budgets %q in %s", got, d.budgets.color)
	}
	lines := map[string]fyne.ThemeColorName{}
	for _, o := range d.log.Objects {
		l := o.(*selLine).line
		lines[l.text] = l.color
	}
	want := map[string]fyne.ThemeColorName{"rate limited": colorStatusWarning, "refused": colorStatusError, "per namespace": colorMuted, "connecting to": colorMuted}
	for word, c := range want {
		found := false
		for text, color := range lines {
			if strings.Contains(text, word) {
				found = true
				if color != c {
					t.Errorf("%q in %s, want %s", text, color, c)
				}
			}
		}
		if !found {
			t.Errorf("no log line with %q: %v", word, lines)
		}
	}
	if d.copyBtn.Disabled() || d.saveBtn.Disabled() {
		t.Error("Copy and Save are enabled for a connected cluster")
	}

	d.mode.buttons[1].OnTapped()
	q.until(t, "the report", func() bool { return d.reportView.Text() != "" })
	if !d.reportBox.Visible() || d.overview.Visible() || !strings.Contains(d.reportView.Text(), "Server: https://host-1") {
		t.Errorf("Report shows %q", d.reportView.Text())
	}
	sh.app.Clipboard().SetContent("")
	d.copyBtn.OnTapped()
	q.until(t, "the copy", func() bool { return sh.app.Clipboard().Content() != "" })
	if got := sh.app.Clipboard().Content(); got != d.reportView.Text() {
		t.Errorf("copied %q, want what Report shows", got)
	}
}

// The status bar's button (D-133): muted "Diagnostics" without a problem, the warning colour and the
// count once there are problems, "99+" beyond 99; a click opens the tab.
func TestDiagnosticsButton(t *testing.T) {
	f := diagnosticsFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	b := sh.status.diag
	for _, tc := range []struct {
		problems int
		text     string
		color    fyne.ThemeColorName
	}{{0, "Diagnostics", colorMuted}, {4, "Diagnostics 4", colorStatusWarning}, {120, "Diagnostics 99+", colorStatusWarning}} {
		f.mu.Lock()
		f.diag.Problems = tc.problems
		f.diag.Requests.Total++ // the event's counters moved
		f.mu.Unlock()
		f.emit(engine.Event{Type: engine.EventDiagnostics})
		q.until(t, tc.text, func() bool { return b.pill.text == tc.text })
		if b.pill.fg != tc.color || b.pill.tinted != (tc.problems > 0) {
			t.Errorf("%d problems: %q in %s (tinted %v)", tc.problems, b.pill.text, b.pill.fg, b.pill.tinted)
		}
	}
	if !strings.Contains(b.tip, "120 problems since the latest connect") {
		t.Errorf("tooltip %q", b.tip)
	}
	b.Tapped(nil)
	q.until(t, "diagnostics.open", func() bool { return len(f.callsOf("diagnostics.open")) == 1 })
}

// Connection Diagnostics with problems in a dock dragged higher, its Overview and its Report: the
// PNGs of the pull request (rule 7).
func TestRenderConnectionDiagnosticsDark(t *testing.T) {
	f := diagnosticsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	openDiagnostics(f)
	q.until(t, "the Diagnostics", func() bool {
		d := diagnosticsTabOf(sh)
		return d != nil && len(d.log.Objects) == 6 && sh.dock.open()
	})
	sh.dockLay.offset = 0.4
	sh.dockSplit.Refresh()
	q.drain()
	savePNG(t, w, "diagnostics_log_dark.png")
	d := diagnosticsTabOf(sh)
	d.mode.buttons[1].OnTapped()
	q.until(t, "the report", func() bool { return d.reportView.Text() != "" })
	q.drain()
	savePNG(t, w, "diagnostics_report_dark.png")
}
