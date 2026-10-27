package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// startShell starts a shell on eng (the fake engine f, or a wrapper of it) in a 1440×900
// offscreen window, as newTestShell does for f itself.
func startShell(t testing.TB, eng Engine, f *fakeEngine, opts Options) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(opts.Theme))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(eng, a, w, opts, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.confirmer.armDelay = 0
	f.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	return sh, w, q
}

// logsEngine is the fake engine whose log streams are what the real engine streams for the tab:
// a pod's container, or a deployment's pods with "<pod>/<container> " before every line, with
// the server's timestamps (every stream carries them). Streams stay open until closed. Its older
// lines are demoOlder's.
type logsEngine struct {
	*fakeEngine
	older *olderFake
}

func newLogsEngine(f *fakeEngine) logsEngine { return logsEngine{fakeEngine: f, older: &olderFake{}} }

// olderFake records the requests for older lines.
type olderFake struct {
	mu   sync.Mutex
	reqs []engine.OlderRequest
}

func (o *olderFake) requests() []engine.OlderRequest {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.reqs)
}

// demoOlder are lines written before demoMerged's.
var demoOlder = []struct{ source, ts, msg string }{
	{"checkout-api-7d9f8b6c4-x2k9p/api", "2026-10-11T14:27:58.100000000Z", "loading configuration from /etc/checkout/config.yaml"},
	{"checkout-api-7d9f8b6c4-q8w3n/api", "2026-10-11T14:27:58.200000000Z", "loading configuration from /etc/checkout/config.yaml"},
	{"checkout-api-7d9f8b6c4-x2k9p/api", "2026-10-11T14:27:59.900000000Z", "connected to postgres at db:5432"},
}

// OlderLogs answers as the engine does from demoOlder and demoMerged: of every source the tab
// shows, the lines older than the oldest it holds (with since, from then on); nothing is older
// than demoOlder, so every source is whole.
func (e logsEngine) OlderLogs(_ context.Context, tabID string, req engine.OlderRequest) (engine.OlderLines, error) {
	e.older.mu.Lock()
	e.older.reqs = append(e.older.reqs, req)
	e.older.mu.Unlock()
	e.mu.Lock()
	tab, _ := e.ui.DockTab(tabID)
	e.mu.Unlock()
	oldest := map[string]time.Time{}
	for _, h := range req.Held {
		oldest[h.Source] = h.Oldest
	}
	out := engine.OlderLines{Complete: true}
	all := append(slices.Clone(demoOlder), demoMerged...)
	for _, l := range all {
		src := l.source
		if tab.Ref.Kind == kindPods {
			if src != tab.Ref.Name+"/"+tab.Container {
				continue
			}
			src = ""
		}
		at, _ := time.Parse(time.RFC3339Nano, l.ts)
		if o, held := oldest[src]; held && !at.Before(o) || !req.Since.IsZero() && at.Before(req.Since.Truncate(time.Second)) {
			continue
		}
		if src != "" {
			out.Lines = append(out.Lines, src+" "+l.ts+" "+l.msg)
		} else {
			out.Lines = append(out.Lines, l.ts+" "+l.msg)
		}
		if !slices.Contains(out.Whole, src) {
			out.Whole = append(out.Whole, src)
		}
	}
	if len(out.Lines) > req.Room {
		out.Lines, out.Complete = out.Lines[len(out.Lines)-req.Room:], false
	}
	return out, nil
}

var demoMerged = []struct{ source, ts, msg string }{
	{"checkout-api-7d9f8b6c4-x2k9p/api", "2026-10-11T14:28:01.204118340Z", "starting checkout-api version=2.14.1 commit=9f3c2e1"},
	{"checkout-api-7d9f8b6c4-q8w3n/api", "2026-10-11T14:28:01.311902118Z", "starting checkout-api version=2.14.1 commit=9f3c2e1"},
	{"checkout-api-7d9f8b6c4-x2k9p/envoy", "2026-10-11T14:28:01.402551730Z", "envoy ready; upstream api:8080 healthy"},
	{"checkout-api-7d9f8b6c4-x2k9p/api", "2026-10-11T14:28:09.023772001Z", "POST /api/v1/checkout 201 18.2ms cart=c_8f2k1"},
	{"checkout-api-7d9f8b6c4-q8w3n/api", "2026-10-11T14:28:12.774310925Z", "ERROR payment provider unreachable: dial tcp 10.96.14.2:443: i/o timeout; retrying with exponential backoff, attempt=1 next=2s"},
	{"checkout-api-7d9f8b6c4-q8w3n/api", "2026-10-11T14:28:14.801233417Z", "payment provider reachable again after 2.03s"},
	{"checkout-api-7d9f8b6c4-x2k9p/envoy", "2026-10-11T14:28:15.112908733Z", "upstream connect error or disconnect/reset before headers. retried and the latest reset reason: connection timeout"},
	{"checkout-api-7d9f8b6c4-x2k9p/api", "2026-10-11T14:28:19.023109876Z", "GET /healthz 200 0.4ms"},
	{"checkout-api-7d9f8b6c4-q8w3n/api", "2026-10-11T14:28:22.640022190Z", "error budget burn rate 0.4 (ok)"},
}

func (e logsEngine) OpenLogs(_ context.Context, tabID string) (io.ReadCloser, error) {
	e.mu.Lock()
	e.opened[tabID]++
	tab, ok := e.ui.DockTab(tabID)
	e.mu.Unlock()
	if !ok {
		return nil, io.ErrClosedPipe
	}
	if tab.Logs.Previous && tab.Ref.Kind == kindPods {
		return nil, errors.New(`logs of checkout/` + tab.Ref.Name + `: previous terminated container "api" in pod "` + tab.Ref.Name + `" not found`)
	}
	var b strings.Builder
	for _, l := range demoMerged {
		if tab.Ref.Kind != kindPods {
			b.WriteString(l.source + " ")
		} else if l.source != tab.Ref.Name+"/"+tab.Container {
			continue
		}
		b.WriteString(l.ts + " " + l.msg + "\n")
	}
	return newOpenStream(e.fakeEngine, tabID, b.String()), nil
}

// demoSources are what the deployment's tab offers: its two pods, their containers, an init
// container apart.
var demoSources = engine.LogSources{
	AllPods: true, Pods: []string{"checkout-api-7d9f8b6c4-q8w3n", "checkout-api-7d9f8b6c4-x2k9p"},
	Containers: []string{"api", "envoy"}, InitContainers: []string{"migrate-db"},
}

// withLogCommands answers logs.setOptions and logs.controller as the engine does.
func withLogCommands(t testing.TB, f *fakeEngine, deployment model.ObjectRef) {
	t.Helper()
	f.handlers["logs.setOptions"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p logOptionsParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				o := &u.Dock[i].Logs
				if u.Dock[i].ID != p.ID {
					continue
				}
				for dst, v := range map[*bool]*bool{&o.Previous: p.Previous, &o.Timestamps: p.Timestamps, &o.ResourceNames: p.ResourceNames, &o.Wrap: p.Wrap, &o.Filter: p.Filter} {
					if v != nil {
						*dst = *v
					}
				}
				if p.Search != nil {
					o.Search = *p.Search
				}
			}
		})
		return nil, nil
	}
	f.handlers["logs.controller"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p dockParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				if u.Dock[i].ID == p.ID {
					r := deployment
					u.Dock[i].Ref, u.Dock[i].Container, u.Dock[i].Title = &r, "", "Logs: Deployment "+r.Name
					u.Dock[i].Logs.ResourceNames = true
				}
			}
		})
		return nil, nil
	}
	f.handlers["logs.loadOlder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p dockParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				if u.Dock[i].ID == p.ID {
					u.Dock[i].Logs.LoadOlder++
				}
			}
		})
		return nil, nil
	}
	f.handlers["logs.jumpTo"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p logJumpParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		at, err := engine.ParseLogTime(p.Time, fixedNow) // the fake's clock, in UTC
		if err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				if u.Dock[i].ID == p.ID {
					u.Dock[i].Logs.Jump = engine.LogJump{Time: at.UTC(), Seq: u.Dock[i].Logs.Jump.Seq + 1}
				}
			}
		})
		return nil, nil
	}
	f.handlers["logs.sources"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p dockParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.mu.Lock()
		tab, _ := f.ui.DockTab(p.ID)
		f.mu.Unlock()
		if tab.Ref != nil && tab.Ref.Kind == kindPods {
			return engine.LogSources{Pods: demoSources.Pods, Containers: []string{"api", "envoy"}}, nil
		}
		return demoSources, nil
	}
	f.handlers["logs.setSource"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p logSourceParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Dock {
				t := &u.Dock[i]
				if t.ID != p.ID {
					continue
				}
				switch {
				case t.Ref.Kind == kindPods && p.Pod != nil:
					r := *t.Ref
					r.Name = *p.Pod
					t.Ref = &r
				case p.Pod != nil:
					t.Logs.Pod = *p.Pod
				}
				if p.Workload != nil { // an application's tab (logtab_group_test.go)
					t.Logs.Workload, t.Logs.Pod = *p.Workload, ""
				}
				if p.Container != nil {
					t.Container = *p.Container
				}
				if t.Ref.Kind == kindPods {
					t.Title = "Logs: " + t.Ref.Name + "/" + t.Container
				}
			}
		})
		return nil, nil
	}
	if err := engine.Register(f.reg, engine.Spec[dockParams]{ID: "logs.controller", Label: "Logs of the Controlling Resource", Danger: engine.DangerRead,
		Enabled: func(*engine.Session, dockParams) error { return f.reason("logs.controller") },
		Run:     func(context.Context, *engine.Session, dockParams) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
}

// logsScene is the pods view with a logs tab in the dock: on a pod, or on its deployment.
func logsScene(t *testing.T, mode ThemeMode, workload bool, opts engine.LogOptions) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	return logsSceneWith(t, mode, workload, opts, nil)
}

// logsSceneWith is logsScene with the fixture changed by setup before the window starts (a time
// format, preferences); setup may be nil.
func logsSceneWith(t *testing.T, mode ThemeMode, workload bool, opts engine.LogOptions, setup func(*fakeEngine)) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	if setup != nil {
		setup(f)
	}
	pod := model.ObjectRef{Context: "demo", Kind: kindPods, Namespace: "checkout", Name: "checkout-api-7d9f8b6c4-x2k9p"}
	dep := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
	withLogCommands(t, f, dep)
	tab := engine.DockTab{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: " + pod.Name + "/api", Context: "demo", Ref: &pod, Container: "api", Logs: opts}
	if workload {
		tab = engine.DockTab{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: Deployment checkout-api", Context: "demo", Ref: &dep, Logs: opts}
	}
	f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, "logs-1"
	sh, w, q := startShell(t, newLogsEngine(f), f, Options{Theme: mode})
	q.until(t, "the log lines", func() bool {
		lt := logTabOf(sh)
		return len(sh.kinds) > 0 && lt != nil && lt.view != nil && len(lt.view.ShownLines()) > 0
	})
	return sh, w, q, f
}

func logTabOf(sh *shell) *logTab {
	if c, ok := sh.dock.contents["logs-1"]; ok {
		return c.logs
	}
	return nil
}

// TestLogTabOptionsAreEngineState: a toggle in the toolbar asks the engine (logs.setOptions) and
// the view follows the engine's state, whoever changed it; an option that changes the request
// (previous) opens the stream again, the others (timestamps since 2026-10-13) do not.
func TestLogTabOptionsAreEngineState(t *testing.T) {
	sh, _, q, f := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	if lt.names.Visible() || !lt.controller.Visible() {
		t.Error("a pod's tab shows resource names, or not the controller button")
	}
	test.Tap(lt.wrap)
	q.until(t, "wrap from the engine", func() bool { return lt.view.View().Wrap })
	if calls := f.callsOf("logs.setOptions"); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"wrap":true`) {
		t.Errorf("calls %v", calls)
	}

	// An agent turns timestamps on and searches: the checks follow, the stream opens again.
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.setOptions", Origin: engine.OriginExternalAgent,
			Params: mustJSON(t, map[string]any{"id": "logs-1", "timestamps": true, "search": "POST", "filter": true})})
	}()
	q.until(t, "the agent's options", func() bool {
		return lt.timestamps.Checked && lt.filter.Checked && lt.search.Text == "POST" && lt.view.View().Search == "POST"
	})
	q.until(t, "the timestamped lines", func() bool {
		lines := lt.view.ShownLines()
		return len(lines) > 0 && strings.HasPrefix(lines[0], "2026-10-11 14:28:01.204 ") // in the window's format (D-31)
	})
	if n := len(f.callsOf("logs.setOptions")); n != 2 { // the agent's; following it dispatched nothing
		t.Errorf("%d setOptions calls", n)
	}
	f.mu.Lock()
	opened := f.opened["logs-1"]
	f.mu.Unlock()
	if opened != 1 {
		t.Errorf("the stream opened %d times: timestamps come with every stream", opened)
	}

	// Previous changes the request: the stream opens again, and the first one is closed.
	test.Tap(lt.previous)
	q.until(t, "the stream opened again", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.opened["logs-1"] == 2 && f.closed["logs-1"] >= 1
	})
}

// TestLogTabSourceSelectors: a deployment's tab names its pod and container above the toolbar;
// a selector's menu lists what the engine offers (logs.sources), every pod or container first and
// the init containers apart, the shown one checked; a choice asks the engine (logs.setSource) and
// the view shows that pod's lines without a new stream; an agent's choice moves the selectors. A
// pod's tab switches its container by opening the stream again.
func TestLogTabSourceSelectors(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	lt := logTabOf(sh)
	if lt.podPick.Text != "Pod: All pods" || lt.containerPick.Text != "Container: All containers" {
		t.Errorf("selectors %q %q", lt.podPick.Text, lt.containerPick.Text)
	}
	test.Tap(lt.podPick)
	if n := len(q.popUpMenu(t, w).Items); n != 4 {
		t.Errorf("the pod menu shows %d items", n)
	}
	if len(f.callsOf("logs.sources")) != 1 {
		t.Error("the menu did not ask the engine")
	}
	menu := lt.sourceMenu(demoSources, true)
	if got := sourceMenuLabels(menu); strings.Join(got, "|") != "All pods|---|checkout-api-7d9f8b6c4-q8w3n|checkout-api-7d9f8b6c4-x2k9p" {
		t.Errorf("pod menu %q", got)
	}
	if !menu.Items[0].Checked {
		t.Error("All pods is not checked")
	}
	dismissOverlays(w)
	menu.Items[2].Action()
	q.until(t, "the pod's lines", func() bool {
		lines := lt.view.ShownLines()
		return lt.podPick.Text == "Pod: checkout-api-7d9f8b6c4-q8w3n" && len(lines) == 4 && strings.HasPrefix(lines[0], "checkout-api-7d9f8b6c4-q8w3n/api ")
	})
	if calls := f.callsOf("logs.setSource"); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"pod":"checkout-api-7d9f8b6c4-q8w3n"`) ||
		strings.Contains(string(calls[0].Params), "container") {
		t.Errorf("setSource calls %v", calls)
	}
	q.until(t, "the status counts the pod's lines", func() bool { return strings.HasPrefix(lt.view.StatusText(), "4 of 9 lines") })

	menu = lt.sourceMenu(demoSources, false)
	if got := sourceMenuLabels(menu); strings.Join(got, "|") != "All containers|---|api|envoy|---|Init containers|migrate-db" {
		t.Errorf("container menu %q", got)
	}
	if !menu.Items[5].Disabled || !menu.Items[0].Checked {
		t.Error("the init containers' heading can be chosen, or All containers is not checked")
	}

	// An agent narrows to the envoys of every pod.
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.setSource", Origin: engine.OriginExternalAgent,
			Params: mustJSON(t, map[string]any{"id": "logs-1", "pod": "", "container": "envoy"})})
	}()
	q.until(t, "the envoys' lines", func() bool {
		lines := lt.view.ShownLines()
		return lt.containerPick.Text == "Container: envoy" && lt.podPick.Text == "Pod: All pods" && len(lines) == 2
	})
	f.mu.Lock()
	opened := f.opened["logs-1"]
	f.mu.Unlock()
	if opened != 1 {
		t.Errorf("the merged stream opened %d times; the engine narrows it itself", opened)
	}

	// A pod's tab: the container selector switches the container; its stream opens again.
	sh2, w2, q2, f2 := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt2 := logTabOf(sh2)
	if lt2.podPick.Text != "Pod: checkout-api-7d9f8b6c4-x2k9p" || lt2.containerPick.Text != "Container: api" {
		t.Errorf("a pod's selectors %q %q", lt2.podPick.Text, lt2.containerPick.Text)
	}
	test.Tap(lt2.containerPick)
	q2.popUpMenu(t, w2)
	dismissOverlays(w2)
	menu = lt2.sourceMenu(engine.LogSources{Pods: demoSources.Pods, Containers: []string{"api", "envoy"}}, false)
	if got := sourceMenuLabels(menu); strings.Join(got, "|") != "api|envoy" || !menu.Items[0].Checked {
		t.Errorf("a pod's container menu %q", got)
	}
	menu.Items[1].Action()
	q2.until(t, "the envoy's lines", func() bool {
		lines := lt2.view.ShownLines()
		return lt2.containerPick.Text == "Container: envoy" && len(lines) == 2 && strings.HasPrefix(lines[0], "envoy ready")
	})
	f2.mu.Lock()
	opened = f2.opened["logs-1"]
	f2.mu.Unlock()
	if opened != 2 {
		t.Errorf("a pod's tab opened its stream %d times, want again for the other container", opened)
	}
}

// popUpMenu waits for the pop-up menu a selector shows and returns its menu as it draws it.
func (q *mainQueue) popUpMenu(t *testing.T, w fyne.Window) *widget.Menu {
	t.Helper()
	var m *widget.Menu
	q.until(t, "a pop-up menu", func() bool {
		if top := w.Canvas().Overlays().Top(); top != nil {
			if menus := findAll[*widget.PopUpMenu](top); len(menus) > 0 {
				m = menus[0].Menu
				return true
			}
		}
		return false
	})
	return m
}

// sourceMenuLabels lists a menu's labels, "---" for a separator.
func sourceMenuLabels(m *fyne.Menu) []string {
	var out []string
	for _, it := range m.Items {
		if it.IsSeparator {
			out = append(out, "---")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

// dismissOverlays closes the window's pop-ups.
func dismissOverlays(w fyne.Window) {
	for _, o := range w.Canvas().Overlays().List() {
		w.Canvas().Overlays().Remove(o)
	}
}

// TestLogTabSearchIsDebouncedAndSlashFocusesIt: typing in the search sends it once the user
// pauses; "/" with nothing focused puts the focus into the search.
func TestLogTabSearchIsDebouncedAndSlashFocusesIt(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	w.Canvas().Unfocus()
	test.TypeOnCanvas(w.Canvas(), "/")
	if w.Canvas().Focused() != lt.search {
		t.Fatalf("focused %v, want the search", w.Canvas().Focused())
	}
	test.Type(lt.search, "health")
	q.until(t, "the search in the engine", func() bool { return lt.view.View().Search == "health" })
	if calls := f.callsOf("logs.setOptions"); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"search":"health"`) {
		t.Errorf("search calls %d: %v", len(calls), calls)
	}
	if lt.search.Text != "health" {
		t.Errorf("search entry %q", lt.search.Text)
	}
}

// TestLogTabMovesToTheController: the pod's tab moves to its deployment's logs (logs.controller):
// the stream opens again for all pods, with their names; the button greys when the pod has no
// controller.
func TestLogTabMovesToTheController(t *testing.T) {
	sh, _, q, f := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	q.until(t, "the controller button", func() bool { return !lt.controller.Disabled() })
	test.Tap(lt.controller)
	q.until(t, "logs.controller", func() bool { return len(f.callsOf("logs.controller")) == 1 })
	q.until(t, "the deployment's logs", func() bool {
		lines := lt.view.ShownLines()
		return lt.view.View().Prefixed && len(lines) == len(demoMerged) && strings.HasPrefix(lines[0], "checkout-api-7d9f8b6c4-x2k9p/api ")
	})
	if !lt.names.Visible() || !lt.names.Checked || lt.controller.Visible() {
		t.Error("the deployment's tab hides its names option or shows the controller button")
	}
	if b := sh.dock.tabRow.Objects[0].(*tabButton); b.title != "Logs: Deployment checkout-api" {
		t.Errorf("tab title %q", b.title)
	}

	f2 := connectedFake(t, 10)
	withLogCommands(t, f2, model.ObjectRef{})
	f2.disabled["logs.controller"] = "pod web-0 has no controlling resource"
	pod := f2.tables["pods"].Rows[0].Ref
	f2.ui.Dock, f2.ui.ActiveDock = []engine.DockTab{{ID: "logs-1", Type: engine.DockLogs, Title: "Logs", Context: "demo", Ref: &pod, Container: "api"}}, "logs-1"
	sh2, _, q2 := startShell(t, newLogsEngine(f2), f2, Options{})
	q2.until(t, "the greyed controller button", func() bool {
		lt := logTabOf(sh2)
		return lt != nil && lt.controller.Disabled() && strings.Contains(lt.controller.tip, "no controlling resource")
	})
}

// olderOf is the fake's record of the requests for older lines.
func olderOf(sh *shell) *olderFake { return sh.eng.(logsEngine).older }

// TestLogTabLoadsOlderLines: the row above a tab's lines loads older ones through the engine's
// state (logs.loadOlder), whoever asks: the tab sends what it holds of each container, puts the
// older lines on top without repeating one, stops following and then says it holds the beginning;
// a request it holds everything for adds nothing.
func TestLogTabLoadsOlderLines(t *testing.T) {
	sh, _, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true})
	lt := logTabOf(sh)
	q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
	if lt.view.Head() != headLoadOlder || !lt.view.Following() {
		t.Fatalf("head %q, following %v", lt.view.Head(), lt.view.Following())
	}
	lt.requestOlder() // the head row's action
	q.until(t, "the older lines", func() bool { return len(lt.view.ShownLines()) == len(demoMerged)+len(demoOlder) })
	if calls := f.callsOf("logs.loadOlder"); len(calls) != 1 {
		t.Errorf("loadOlder calls %v", calls)
	}
	reqs := olderOf(sh).requests()
	if len(reqs) != 1 || len(reqs[0].Held) != 3 || reqs[0].Room != lt.view.Capacity()-len(demoMerged) || !reqs[0].Since.IsZero() {
		t.Fatalf("requests %+v", reqs)
	}
	lines := lt.view.ShownLines()
	if lines[0] != demoOlder[0].source+" "+shownStamp(demoOlder[0].ts)+" "+demoOlder[0].msg || lines[3] != demoMerged[0].source+" "+shownStamp(demoMerged[0].ts)+" "+demoMerged[0].msg {
		t.Errorf("lines %q", lines[:4])
	}
	q.until(t, "the beginning", func() bool { return lt.view.Head() == headBeginning })
	if lt.view.Following() {
		t.Error("the tab still follows after older lines came")
	}

	// An agent asks again: the tab sends its sources as whole, and nothing is added.
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.loadOlder", Origin: engine.OriginExternalAgent,
			Params: mustJSON(t, map[string]any{"id": "logs-1"})})
	}()
	q.until(t, "the agent's request", func() bool { return len(olderOf(sh).requests()) == 2 })
	q.until(t, "its answer", func() bool { return lt.view.Head() == headBeginning && !lt.older.busy })
	if got := olderOf(sh).requests()[1]; !got.Held[0].Whole {
		t.Errorf("held %+v: want the sources whole", got.Held)
	}
	if n := len(lt.view.ShownLines()); n != len(demoMerged)+len(demoOlder) {
		t.Errorf("%d lines after loading nothing new", n)
	}
}

// TestLogTabOlderLinesAfterReopening: a tab whose stream opens again forgets what it knew of
// older lines and offers them again.
func TestLogTabOlderLinesAfterReopening(t *testing.T) {
	sh, _, q, _ := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	lt.requestOlder()
	q.until(t, "the pod's older lines", func() bool { return len(lt.view.ShownLines()) == 5 && lt.view.Head() == headBeginning })
	test.Tap(lt.previous) // the fake refuses a previous container: the tab shows why, without the row
	q.until(t, "the error", func() bool { return lt.failed && lt.view.Head() == "" })
	test.Tap(lt.previous)
	q.until(t, "the row again", func() bool { return !lt.failed && lt.view.Head() == headLoadOlder && len(lt.view.ShownLines()) == 3 })
}

// TestLogTabJumpsToATime: the time field asks the engine (logs.jumpTo) and the tab follows the
// jump, whoever asked: a time it holds lines of scrolls to the first line at or after it without a
// request; an older one loads the lines since then first; a time after every line says so; a
// time the engine cannot read is an error and no jump.
func TestLogTabJumpsToATime(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true})
	lt := logTabOf(sh)
	q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
	w.Canvas().Focus(lt.jumpField)
	test.Type(lt.jumpField, "2026-10-11 14:28:12")
	test.TypeOnCanvas(w.Canvas(), "\n")
	lt.jumpField.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the jump", func() bool { return strings.Contains(lt.view.JumpedLine(), "ERROR payment provider unreachable") })
	if calls := f.callsOf("logs.jumpTo"); len(calls) == 0 || !strings.Contains(string(calls[0].Params), `"time":"2026-10-11 14:28:12"`) {
		t.Errorf("jumpTo calls %v", calls)
	}
	if n := len(olderOf(sh).requests()); n != 0 {
		t.Errorf("%d requests for a time the tab holds", n)
	}
	if lt.view.Following() {
		t.Error("the tab follows after a jump")
	}
	if lt.jumpField.Text != "2026-10-11 14:28:12" && !strings.HasPrefix(lt.jumpField.Text, "2026-10-11") {
		t.Errorf("the field shows %q", lt.jumpField.Text)
	}

	// An agent jumps before the oldest line: the lines since then come first.
	jump := func(text string) {
		go func() {
			_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.jumpTo", Origin: engine.OriginExternalAgent,
				Params: mustJSON(t, map[string]any{"id": "logs-1", "time": text})})
		}()
	}
	jump("2026-10-11T14:27:59Z")
	q.until(t, "the older line", func() bool { return strings.Contains(lt.view.JumpedLine(), "connected to postgres") })
	reqs := olderOf(sh).requests()
	if len(reqs) != 1 || !reqs[0].Since.Equal(time.Date(2026, 10, 11, 14, 27, 59, 0, time.UTC)) {
		t.Fatalf("requests %+v", reqs)
	}
	if n := len(lt.view.ShownLines()); n != len(demoMerged)+1 {
		t.Errorf("%d lines: want the one since 14:27:59 added", n)
	}

	// After every line: no jump, a message.
	toasts := len(sh.toasts.Objects)
	jump("2026-10-11 14:29")
	q.until(t, "the message", func() bool { return len(sh.toasts.Objects) > toasts && lt.view.JumpedLine() == "" })
	if n := len(olderOf(sh).requests()); n != 1 {
		t.Errorf("a time after every line asked again: %d requests", n)
	}

	// Unreadable: an error, and the tab's jump stays.
	seq := lt.jump.seen
	lt.jumpField.SetText("teatime")
	lt.jumpField.OnSubmitted("teatime")
	q.until(t, "the error", func() bool { return len(f.callsOf("logs.jumpTo")) >= 4 && len(sh.toasts.Objects) > toasts+1 })
	if lt.jump.seen != seq {
		t.Error("an unreadable time jumped")
	}
}

// TestLogDownloadSavesWhatTheTabShows (A84, the owner's decision of 2026-10-14 evening): the
// download writes the lines the tab shows, as shown: with Filter on only those matching the search,
// of the pod and container its selectors show; with Filter off every line it holds, the search only
// highlighting; to the file the save dialog names, proposed after the tab.
func TestLogDownloadSavesWhatTheTabShows(t *testing.T) {
	sh, _, q, _ := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "error", Filter: true})
	lt := logTabOf(sh)
	q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
	shown := func(i int) string {
		return demoMerged[i].source + " " + shownStamp(demoMerged[i].ts) + " " + demoMerged[i].msg
	}
	if got := savedLogs(t, lt.view.DownloadLines()); !slices.Equal(got, []string{shown(4), shown(6), shown(8)}) {
		t.Errorf("filtered, saved %q", got)
	}
	test.Tap(lt.filter)
	q.until(t, "the filter off", func() bool { return !lt.view.View().Filter })
	if got := savedLogs(t, lt.view.DownloadLines()); len(got) != len(demoMerged) || got[4] != shown(4) || got[0] != shown(0) {
		t.Errorf("unfiltered, saved %q", got)
	}
	for _, c := range []struct{ title, cluster, want string }{
		{"Logs: Deployment checkout-api", "kind-cogpit", "Deployment_checkout-api-kind-cogpit.log"},
		{"Logs: web-0/app", "arn:aws:eks:eu-central-1:1:cluster/prod", "web-0_app-arn_aws_eks_eu-central-1_1_cluster_prod.log"},
		{"Logs: \x1b]0;x\a", "", "0_x.log"},
		{"", "", "logs.log"},
	} {
		if got := logFileName(c.title, c.cluster); got != c.want {
			t.Errorf("logFileName(%q, %q) = %q, want %q", c.title, c.cluster, got, c.want)
		}
	}
	// The button opens the save dialog over the window.
	test.Tap(lt.download)
	if len(sh.win.Canvas().Overlays().List()) == 0 {
		t.Error("no save dialog")
	}
}

// TestLogDownloadOfANarrowedTab (A84): a controlling resource's tab narrowed to one pod saves that
// pod's lines its filter shows, and only them.
func TestLogDownloadOfANarrowedTab(t *testing.T) {
	sh, _, q, _ := logsScene(t, ThemeLight, true, engine.LogOptions{ResourceNames: true, Search: "error", Filter: true, Pod: "checkout-api-7d9f8b6c4-q8w3n"})
	lt := logTabOf(sh)
	q.until(t, "the pod's lines", func() bool { return len(lt.view.ShownLines()) == 4 })
	got := savedLogs(t, lt.view.DownloadLines())
	want := []string{demoMerged[4].source + " " + demoMerged[4].msg, demoMerged[8].source + " " + demoMerged[8].msg}
	if !slices.Equal(got, want) {
		t.Errorf("saved %q, want %q", got, want)
	}
}

// savedLogs saves lines as the download does and reads the file back, line by line.
func savedLogs(t *testing.T, lines []string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveLogs(f, lines); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// TestRenderLogsWorkloadSources: a deployment's tab narrowed to one pod by its selectors, dark and
// light, and its container selector open.
func TestRenderLogsWorkloadSources(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		sh, w, q, _ := logsScene(t, mode, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Pod: "checkout-api-7d9f8b6c4-x2k9p"})
		lt := logTabOf(sh)
		q.until(t, "the pod's lines", func() bool { return len(lt.view.ShownLines()) == 5 })
		savePNG(t, w, "logs_workload_sources_"+string(mode)+".png")
		if mode == ThemeDark {
			test.Tap(lt.containerPick)
			q.popUpMenu(t, w)
			savePNG(t, w, "logs_workload_containers_dark.png")
		}
	}
}

// TestRenderLogsJump: a deployment's tab after a jump to 14:28:12, the line found selected at the
// top, the field showing the time, dark and light.
func TestRenderLogsJump(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		sh, w, q, f := logsScene(t, mode, true, engine.LogOptions{ResourceNames: true, Timestamps: true})
		lt := logTabOf(sh)
		q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
		go func() {
			_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "logs.jumpTo", Origin: engine.OriginGUI,
				Params: mustJSON(t, map[string]any{"id": "logs-1", "time": "2026-10-11 14:28:12"})})
		}()
		q.until(t, "the jump", func() bool { return strings.Contains(lt.view.JumpedLine(), "ERROR payment") })
		savePNG(t, w, "logs_jump_"+string(mode)+".png")
	}
}

// TestRenderLogsOlder: a pod's tab after loading its older lines, the row above them saying it
// holds the beginning, dark and light.
func TestRenderLogsOlder(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		sh, w, q, _ := logsScene(t, mode, false, engine.LogOptions{Timestamps: true})
		lt := logTabOf(sh)
		lt.requestOlder()
		q.until(t, "the older lines", func() bool { return len(lt.view.ShownLines()) == 5 && lt.view.Head() == headBeginning })
		savePNG(t, w, "logs_older_"+string(mode)+".png")
	}
}

func TestRenderLogsWorkloadDark(t *testing.T) {
	_, w, _, _ := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "error"})
	savePNG(t, w, "logs_workload_dark.png")
}

func TestRenderLogsWorkloadLight(t *testing.T) {
	_, w, _, _ := logsScene(t, ThemeLight, true, engine.LogOptions{ResourceNames: true, Timestamps: true, Search: "error"})
	savePNG(t, w, "logs_workload_light.png")
}

// TestRenderLogsWrapFilter: a pod's tab with timestamps, wrapped lines and a filter.
func TestRenderLogsWrapFilter(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		sh, w, q, _ := logsScene(t, mode, true, engine.LogOptions{Timestamps: true, Wrap: true, Search: "error", Filter: true})
		lt := logTabOf(sh)
		q.until(t, "the filtered rows", func() bool { return lt.view.View().Filter && len(lt.view.ShownLines()) == len(demoMerged) })
		savePNG(t, w, "logs_wrap_filter_"+string(mode)+".png")
	}
}

// TestRenderLogsPod: a pod's tab, with the button that moves it to its controller's logs.
func TestRenderLogsPod(t *testing.T) {
	sh, w, q, _ := logsScene(t, ThemeDark, false, engine.LogOptions{Timestamps: true})
	lt := logTabOf(sh)
	q.until(t, "the controller button", func() bool { return lt.controller.Visible() && !lt.controller.Disabled() })
	savePNG(t, w, "logs_pod_dark.png")
}

// TestLogTabShowsAnOpenErrorBelowItsToolbar: a stream the server refuses (no previous container)
// shows the error as the tab's line, under the toolbar, so that the option can be turned off.
func TestLogTabShowsAnOpenErrorBelowItsToolbar(t *testing.T) {
	sh, _, q, _ := logsScene(t, ThemeDark, false, engine.LogOptions{})
	lt := logTabOf(sh)
	test.Tap(lt.previous)
	q.until(t, "the error line", func() bool {
		lines := lt.view.ShownLines()
		return len(lines) == 1 && strings.Contains(lines[0], `previous terminated container "api"`)
	})
	if !lt.previous.Checked {
		t.Error("the Previous check is off although the engine holds it on")
	}
	test.Tap(lt.previous)
	q.until(t, "the container's lines again", func() bool {
		lines := lt.view.ShownLines()
		return len(lines) == 3 && strings.HasPrefix(lines[0], "starting checkout-api")
	})
}
