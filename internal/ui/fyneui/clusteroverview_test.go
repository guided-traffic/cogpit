package fyneui

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's page in the window (G12): the navigator's first item, the node group and
// range controls, the three charts with their lines, the source and status lines, and the Issues
// as the page's table. The model is canned as the engine reports it with Prometheus's history and
// the Metrics Server's live values; the issues are the fixture's, by the engine's rule.

// cellOf returns a fixture row's cell by column id.
func cellOf(s *engine.TableSnapshot, r model.Row, id string) model.Cell {
	for i, c := range s.Columns {
		if c.ID == id && i < len(r.Cells) {
			return r.Cells[i]
		}
	}
	return model.Cell{}
}

// clusterIssuesSnapshot is the page's Issues table as the engine builds it from the fixture: pods
// and nodes with a warning, the Deployment short of a ready pod, the warning events; Name, Kind,
// Namespace, Message, Age, the newest first.
func clusterIssuesSnapshot(f *fakeEngine) *engine.TableSnapshot {
	var rows []model.Row
	add := func(ref model.ObjectRef, name, kind, msg string, sev model.Status, since model.Cell) {
		self := ref
		ns := model.Cell{Text: ref.Namespace}
		if ref.Namespace != "" {
			ns.Link = &model.ObjectRef{Kind: kindNamespaces, Name: ref.Namespace}
		}
		rows = append(rows, model.Row{Ref: ref, Status: sev, Cells: []model.Cell{
			{Text: name, Link: &self}, {Text: kind}, ns, {Text: msg, Status: sev}, since,
		}})
	}
	for key, kind := range map[string]string{"pods": "Pod", "nodes": "Node"} {
		s := f.tables[key]
		for _, r := range s.Rows {
			if w := cellOf(s, r, "warning"); w.Status != model.StatusNone {
				sev := model.StatusWarning
				if r.Status == model.StatusError {
					sev = model.StatusError
				}
				add(r.Ref, r.Ref.Name, kind, w.Text, sev, cellOf(s, r, "age"))
			}
		}
	}
	deps := f.tables["deployments"]
	for _, r := range deps.Rows {
		if r.Status == model.StatusWarning {
			add(r.Ref, r.Ref.Name, "Deployment", "1 of 2 pods ready", model.StatusWarning, model.AgeCell(fixedNow.Add(-12*time.Minute)))
		}
	}
	evs := f.tables["events"]
	for _, r := range evs.Rows {
		if cellOf(evs, r, "type").Text != "Warning" {
			continue
		}
		obj := cellOf(evs, r, "involvedObject")
		ref := r.Ref
		name := obj.Text
		if obj.Link != nil {
			ref, name = *obj.Link, obj.Link.Name
			ref.Context = r.Ref.Context
		}
		kind, _, _ := strings.Cut(obj.Text, ":")
		add(ref, name, kind, cellOf(evs, r, "message").Text, model.StatusWarning, model.AgeCell(time.Unix(cellOf(evs, r, "lastSeen").Num, 0)))
	}
	cols := []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "kind", Title: "Kind", Type: model.ColumnText, Width: 120},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "message", Title: "Message", Type: model.ColumnText},
		{ID: "age", Title: "Age", Type: model.ColumnAge},
	}
	engine.SortRows(rows, cols, engine.SortSpec{Column: "age", Desc: true})
	return &engine.TableSnapshot{
		View: engine.ClusterOverview, Context: "demo", Title: "Overview", Namespaced: true, Columns: cols, Rows: rows,
		Sort: engine.SortSpec{Column: "age", Desc: true}, Total: len(rows), Revision: 1, Synced: true,
	}
}

// clusterModelFor is the Overview as the engine reports it for a choice, with Prometheus's history
// over the range and the Metrics Server's live values: the workers (8 nodes) or the master.
func clusterModelFor(choice engine.OverviewChoice) engine.ClusterOverviewModel {
	window := time.Hour
	if d, err := engine.ParseChartRange(choice.Range); err == nil && d > 0 {
		window = d
	}
	nodes := choice.Nodes
	if nodes == "" {
		nodes = engine.NodesWorker
	}
	scale := 1.0
	if nodes == engine.NodesMaster {
		scale = 0.18
	}
	step := max(30*time.Second, window/240)
	var cpu, mem, pods []engine.OverviewSample
	for t, i := fixedNow.Add(-window), 0; !t.After(fixedNow); t, i = t.Add(step), i+1 {
		wave := 0.5 + 0.5*math.Sin(float64(i)/11) + 0.15*math.Sin(float64(i)/2.3)
		cpu = append(cpu, engine.OverviewSample{At: t, Value: scale * (11 + 6*wave)})
		mem = append(mem, engine.OverviewSample{At: t, Value: scale * float64(int64(41<<30)+int64(wave*float64(6<<30)))})
	}
	for t, i := fixedNow.Add(-9*time.Minute), 0; !t.After(fixedNow); t, i = t.Add(15*time.Second), i+1 {
		pods = append(pods, engine.OverviewSample{At: t, Value: math.Round(scale * float64(43+i/9))})
	}
	f := func(v float64) *float64 { return &v }
	last := func(s []engine.OverviewSample) *float64 { return f(s[len(s)-1].Value) }
	return engine.ClusterOverviewModel{
		Nodes: nodes, Range: engine.FormatChartRange(window), Window: window,
		Groups: []engine.OverviewNodeGroup{{Name: engine.NodesMaster, Title: "Master", Nodes: 1}, {Name: engine.NodesWorker, Title: "Worker", Nodes: 8}},
		Charts: []engine.OverviewChart{
			{Metric: engine.OverviewCPU, Title: "CPU", Unit: "cores", Current: last(cpu), At: fixedNow, Samples: cpu,
				Requests: f(scale * 18.5), Limits: f(scale * 29), Allocatable: scale * 31.2, Capacity: scale * 32},
			{Metric: engine.OverviewMemory, Title: "Memory", Unit: "bytes", Current: last(mem), At: fixedNow, Samples: mem,
				Requests: f(scale * float64(52<<30)), Limits: f(scale * float64(88<<30)), Allocatable: scale * float64(118<<30), Capacity: scale * float64(128<<30)},
			{Metric: engine.OverviewPods, Title: "Pods", Unit: "pods", Current: last(pods), At: fixedNow, Samples: pods,
				Allocatable: math.Round(scale * 880), Capacity: math.Round(scale * 880)},
		},
		Status: metrics.StatusAvailable, Source: promSource + " and Kubernetes Metrics Server", History: true,
		Providers: []metrics.ProviderStatus{
			{Provider: metrics.PrometheusID, Label: "Prometheus", Service: "monitoring/prometheus-server:http", Namespace: "monitoring", ServiceName: "prometheus-server",
				Status: metrics.StatusAvailable, LastOK: fixedNow.Add(-40 * time.Second), Serves: []string{metrics.ServesHistory, metrics.ServesNetwork, metrics.ServesFilesystem}},
			{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusAvailable, LastOK: fixedNow.Add(-5 * time.Second),
				Serves: []string{metrics.ServesLive}},
		},
		NodeStates: []engine.StatusCount{{Status: model.StatusOK, Count: 7}, {Status: model.StatusWarning, Count: 1}, {Status: model.StatusError, Count: 1}},
		PodStates: []engine.StatusCount{{Status: model.StatusOK, Count: 38}, {Status: model.StatusSucceeded, Count: 3}, {Status: model.StatusPending, Count: 2},
			{Status: model.StatusWarning, Count: 4}, {Status: model.StatusError, Count: 3}},
		Synced: true,
	}
}

// clusterFake is the fixture on the cluster Overview, answering cluster.overview with the canned
// model for the UI state's choice, and the choice's commands as the engine does.
func clusterFake(t *testing.T) *fakeEngine {
	f := kindsFake(t, engine.ClusterOverview)
	f.tables[engine.ClusterOverview] = clusterIssuesSnapshot(f)
	f.ui.Sort = engine.SortSpec{Column: "age", Desc: true}
	f.handlers["cluster.overview"] = func(context.Context, engine.Invocation) (any, error) {
		f.mu.Lock()
		choice := f.ui.Overview
		f.mu.Unlock()
		return clusterModelFor(choice), nil
	}
	// The header's bearing reads the integrations' states (instrument_bearing.go): the fixture of
	// Cluster Settings › Integrations, with Prometheus found here as the metrics source says.
	f.handlers["cluster.settings.get"] = func(context.Context, engine.Invocation) (any, error) {
		v := engine.ClusterSettingsView{Connected: true, IntegrationsDecided: true}
		for _, st := range integrationsFixture {
			in, _ := engine.IntegrationOf(st.Name)
			st.Label, st.Adds, st.On = in.Label, in.Adds, true
			if st.Name == engine.IntegrationPrometheus {
				st.State, st.Where, st.Detail = engine.IntegrationFound, "monitoring/prometheus-server:http", ""
			}
			v.Integrations = append(v.Integrations, st)
		}
		return v, nil
	}
	f.handlers["ui.setOverviewRange"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Range string `json:"range"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.Overview.Range = p.Range })
		return nil, nil
	}
	f.handlers["ui.setOverviewNodes"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Nodes string `json:"nodes"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.Overview.Nodes = p.Nodes })
		return nil, nil
	}
	return f
}

func lineNames(d chartData) []string {
	var out []string
	for _, l := range d.Lines {
		out = append(out, l.Name)
	}
	return out
}

// TestClusterOverviewPage: Overview is the navigator's first item and opens the page; the page
// shows the node groups (Worker selected), the range, the three charts with the usage and their
// lines, the source row, and the Issues with their own header; Master and a range
// go to the engine and the charts follow its state; an issue's name opens its details over the
// page.
func TestClusterOverviewPage(t *testing.T) {
	f := clusterFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	p := sh.view.cluster
	q.until(t, "the page", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && p.data != nil })
	if top := sh.side.children[""]; len(top) == 0 || top[0] != engine.ClusterOverview || sh.side.titles[engine.ClusterOverview] != "Overview" {
		t.Fatalf("the navigator's top: %v", top)
	}
	tv := sh.view
	if !tv.pageMode || tv.pageTop.Objects[0] != p.root || tv.eventsTitle.text != "Issues" || tv.title.text != "Overview" {
		t.Fatalf("page layout %v, header %q, table %q", tv.pageMode, tv.title.text, tv.eventsTitle.text)
	}
	if want := itemCount(len(f.tables[engine.ClusterOverview].Rows), f.tables[engine.ClusterOverview].Total); tv.count.text != want || tv.search.PlaceHolder != "Search issues…" {
		t.Fatalf("count %q (want %q), search %q", tv.count.text, want, tv.search.PlaceHolder)
	}
	if p.worker.Importance != widget.HighImportance || p.master.Importance == widget.HighImportance || p.master.Disabled() || p.master.tip != "1 control-plane node" {
		t.Fatalf("node groups: worker %v, master %v (%q)", p.worker.Importance, p.master.Importance, p.master.tip)
	}
	if p.rangeSel.Selected != "1h" || !slices.Contains(p.rangeSel.Options, "30d") {
		t.Fatalf("range %q of %v", p.rangeSel.Selected, p.rangeSel.Options)
	}
	// The source row, no prose above it (D-88): Prometheus at the left, the Metrics Server at the
	// right, no link while both answer.
	if got := chipTexts(p.status); p.caption.Visible() || !p.status.root.Visible() ||
		!slices.Equal(got, []string{"Prometheus ● 40 s ago", "Metrics Server ● 5 s ago"}) {
		t.Fatalf("caption shown %v (%q), source row %q", p.caption.Visible(), p.caption.text, got)
	}
	cpu, mem, pods := p.cards[0].plot.data, p.cards[1].plot.data, p.cards[2].plot.data
	if len(cpu.Series) != 1 || len(cpu.Series[0].Points) != 121 || !slices.Equal(lineNames(cpu), []string{"Requests", "Limits", "Allocatable", "Capacity"}) ||
		!cpu.To.Equal(fixedNow) || !cpu.From.Equal(fixedNow.Add(-time.Hour)) {
		t.Fatalf("cpu chart: %d series, lines %v, %v–%v", len(cpu.Series), lineNames(cpu), cpu.From, cpu.To)
	}
	if !mem.Binary || !slices.Equal(lineNames(pods), []string{"Allocatable", "Capacity"}) || len(pods.Series[0].Points) != 37 {
		t.Fatalf("memory binary %v, pods lines %v, %d pods points", mem.Binary, lineNames(pods), len(pods.Series[0].Points))
	}
	if got := p.cards[2].headline.text; got != "47 of 880 pods (5%)" {
		t.Fatalf("pods headline %q", got)
	}
	if got := p.cards[0].headline.text; !strings.HasSuffix(got, " of 31.2 cores (45%)") && !strings.Contains(got, " of 31.2 cores (") {
		t.Fatalf("cpu headline %q", got)
	}

	// The master: the engine holds the choice; the charts follow.
	test.Tap(p.master)
	q.until(t, "the master's charts", func() bool { return p.data != nil && p.data.Nodes == engine.NodesMaster })
	if p.master.Importance != widget.HighImportance || p.cards[0].plot.data.Lines[2].Value != 0.18*31.2 {
		t.Fatalf("master: importance %v, allocatable %v", p.master.Importance, p.cards[0].plot.data.Lines[2].Value)
	}
	// Six hours.
	p.rangeSel.SetSelected("6h")
	q.until(t, "six hours", func() bool { return p.data != nil && p.data.Range == "6h" })
	if d := p.cards[1].plot.data; !d.From.Equal(fixedNow.Add(-6*time.Hour)) || p.rangeSel.Selected != "6h" {
		t.Fatalf("memory chart from %v, range %q", d.From, p.rangeSel.Selected)
	}
	if calls := f.callsOf("ui.setOverviewRange"); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"6h"`) {
		t.Fatalf("ui.setOverviewRange calls %v", calls)
	}

	// An issue's name opens its object's details over the page.
	row := tv.snap.Rows[0]
	test.Tap(tv.cellAt(t, 0, firstDataColumn))
	q.until(t, "the issue's drawer", func() bool { return sh.ui.Drawer != nil })
	if d := *sh.ui.Drawer; !d.Same(row.Ref) || sh.ui.View != engine.ClusterOverview || len(f.callsOf("ui.navigate")) != 0 {
		t.Fatalf("the name link opened %v on %s", d, sh.ui.View)
	}

	// No issues: Lens's quiet state (wording inferred).
	empty := *f.tables[engine.ClusterOverview]
	empty.Rows, empty.Total, empty.Revision = nil, 0, 2
	f.setTable(engine.ClusterOverview, &empty)
	q.until(t, "no issues", func() bool { return tv.placeholderText.text == "No issues found." })
}

func renderClusterOverview(t *testing.T, mode ThemeMode, name string) {
	f := clusterFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the page", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.cluster.data != nil && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderClusterOverviewDark(t *testing.T) {
	renderClusterOverview(t, ThemeDark, "cluster_overview_dark.png")
}

func TestRenderClusterOverviewLight(t *testing.T) {
	renderClusterOverview(t, ThemeLight, "cluster_overview_light.png")
}
