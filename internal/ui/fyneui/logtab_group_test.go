package fyneui

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// An application's logs in one merged tab (A81): every line starts with
// "<workload>/<pod>/<container>", and the tab has a workload selector beside the pod and container
// ones.

// demoWorkers are the lines of the storefront application's second workload.
var demoWorkers = []struct{ source, ts, msg string }{
	{"payments-worker/payments-worker-5c8d7-tq2lm/worker", "2026-10-11T14:28:05.118000000Z", "consumed order o_91fe from queue payments"},
	{"payments-worker/payments-worker-5c8d7-tq2lm/worker", "2026-10-11T14:28:13.002000000Z", "WARN capture retry 1 for order o_91fe: provider timeout"},
	{"payments-worker/payments-worker-5c8d7-tq2lm/worker", "2026-10-11T14:28:16.440000000Z", "captured order o_91fe"},
}

// groupLogsEngine streams an application's tab as the engine does: checkout-api's lines and
// payments-worker's, merged in time order, each prefixed with its workload; narrowed to a workload
// in the view, as the engine narrows its stream.
type groupLogsEngine struct{ logsEngine }

func (e groupLogsEngine) OpenLogs(ctx context.Context, tabID string) (io.ReadCloser, error) {
	e.mu.Lock()
	tab, ok := e.ui.DockTab(tabID)
	e.mu.Unlock()
	if !ok || tab.Ref == nil || !engine.IsLogGroup(tab.Ref.Kind) {
		return e.logsEngine.OpenLogs(ctx, tabID)
	}
	e.mu.Lock()
	e.opened[tabID]++
	e.mu.Unlock()
	type line struct{ source, ts, msg string }
	var all []line
	for _, l := range demoMerged {
		all = append(all, line{"checkout-api/" + l.source, l.ts, l.msg})
	}
	for _, l := range demoWorkers {
		all = append(all, line(l))
	}
	slices.SortFunc(all, func(a, b line) int { return strings.Compare(a.ts, b.ts) })
	var b strings.Builder
	for _, l := range all {
		b.WriteString(l.source + " " + l.ts + " " + l.msg + "\n")
	}
	return newOpenStream(e.fakeEngine, tabID, b.String()), nil
}

// groupLogsScene is the pods view with the storefront application's logs tab in the dock.
func groupLogsScene(t *testing.T, mode ThemeMode) (*shell, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	dep := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
	withLogCommands(t, f, dep)
	sources := f.handlers["logs.sources"]
	f.handlers["logs.sources"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
		f.mu.Lock()
		tab, _ := f.ui.DockTab("logs-1")
		f.mu.Unlock()
		if tab.Ref == nil || !engine.IsLogGroup(tab.Ref.Kind) {
			return sources(ctx, inv)
		}
		src := demoSources
		src.Workloads = []string{"checkout-api", "payments-worker"}
		src.Pods = append(slices.Clone(src.Pods), "payments-worker-5c8d7-tq2lm")
		src.Containers = append(slices.Clone(src.Containers), "worker")
		return src, nil
	}
	app := model.ObjectRef{Context: "demo", Kind: kinds.ApplicationsKey, Namespace: "checkout", Name: "storefront"}
	f.ui.Dock = []engine.DockTab{{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: Application storefront", Context: "demo", Ref: &app,
		Logs: engine.LogOptions{ResourceNames: true, Timestamps: true}}}
	f.ui.ActiveDock = "logs-1"
	sh, _, q := startShell(t, groupLogsEngine{newLogsEngine(f)}, f, Options{Theme: mode})
	q.until(t, "the application's lines", func() bool {
		lt := logTabOf(sh)
		return lt != nil && lt.view != nil && len(lt.view.ShownLines()) == len(demoMerged)+len(demoWorkers)
	})
	return sh, q, f
}

// TestLogTabOfAnApplication (A81): an application's tab shows every workload's lines, each named
// <workload>/<pod>/<container>, offers its workloads in a selector of their own, and narrows to
// the one picked; the engine holds the choice (logs.setSource with workload).
func TestLogTabOfAnApplication(t *testing.T) {
	sh, q, f := groupLogsScene(t, ThemeDark)
	lt := logTabOf(sh)
	if !lt.workloadPick.Visible() || lt.workloadPick.Text != "Workload: All workloads" {
		t.Errorf("workload selector %q, visible %v", lt.workloadPick.Text, lt.workloadPick.Visible())
	}
	lines := lt.view.ShownLines()
	if !slices.Contains(lines, "payments-worker/payments-worker-5c8d7-tq2lm/worker "+shownStamp("2026-10-11T14:28:05.118000000Z")+" consumed order o_91fe from queue payments") {
		t.Errorf("lines %q", lines)
	}
	menu := lt.workloadMenu(engine.LogSources{Workloads: []string{"checkout-api", "payments-worker"}})
	if got := sourceMenuLabels(menu); !slices.Equal(got, []string{allWorkloadsLabel, "---", "checkout-api", "payments-worker"}) || !menu.Items[0].Checked {
		t.Errorf("menu %q", got)
	}
	menu.Items[3].Action()
	q.until(t, "the worker's lines alone", func() bool {
		lines := lt.view.ShownLines()
		return lt.workloadPick.Text == "Workload: payments-worker" && len(lines) == len(demoWorkers)
	})
	if calls := f.callsOf("logs.setSource"); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"workload":"payments-worker"`) {
		t.Errorf("setSource calls %v", calls)
	}
	// A pod's or a workload's tab has no workload selector.
	other := logTabOfAfter(t, sh, q, f)
	if other.workloadPick.Visible() {
		t.Error("a workload's tab shows the workload selector")
	}
}

// logTabOfAfter moves the scene's tab to a Deployment and returns it once it follows.
func logTabOfAfter(t *testing.T, sh *shell, q *mainQueue, f *fakeEngine) *logTab {
	t.Helper()
	f.update(func(u *engine.UIState) {
		r := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
		u.Dock[0].Ref, u.Dock[0].Logs.Workload, u.Dock[0].Title = &r, "", "Logs: Deployment checkout-api"
	})
	q.until(t, "the deployment's tab", func() bool { lt := logTabOf(sh); return lt != nil && lt.tab.Ref.Kind == "deployments" })
	return logTabOf(sh)
}

// TestRenderLogsApplication: the storefront application's logs tab, its workload selector open,
// dark and light (A81).
func TestRenderLogsApplication(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		f := connectedFake(t, 200)
		dep := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
		withLogCommands(t, f, dep)
		sources := f.handlers["logs.sources"]
		f.handlers["logs.sources"] = func(ctx context.Context, inv engine.Invocation) (any, error) {
			src, err := sources(ctx, inv)
			if s, ok := src.(engine.LogSources); ok {
				s.Workloads = []string{"checkout-api", "payments-worker"}
				return s, err
			}
			return src, err
		}
		app := model.ObjectRef{Context: "demo", Kind: kinds.ApplicationsKey, Namespace: "checkout", Name: "storefront"}
		f.ui.Dock = []engine.DockTab{{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: Application storefront", Context: "demo", Ref: &app,
			Logs: engine.LogOptions{ResourceNames: true, Timestamps: true}}}
		f.ui.ActiveDock = "logs-1"
		sh, w, q := startShell(t, groupLogsEngine{newLogsEngine(f)}, f, Options{Theme: mode})
		q.until(t, "the application's lines", func() bool {
			lt := logTabOf(sh)
			return lt != nil && lt.view != nil && len(lt.view.ShownLines()) == len(demoMerged)+len(demoWorkers)
		})
		lt := logTabOf(sh)
		test.Tap(lt.workloadPick)
		q.popUpMenu(t, w)
		savePNG(t, w, "logs_application_"+string(mode)+".png")
	}
}
