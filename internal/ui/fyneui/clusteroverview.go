package fyneui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's upper part (ui-parity › Cluster overview and charts, G12): the node group
// (Master or Worker, Lens's "master and worker nodes"), the time range, and three charts, CPU,
// Memory and Pods, each with the usage over the range and the lines Lens's documentation lists
// (requests, limits, allocatable capacity, capacity; the Pods chart allocatable capacity and
// capacity), the reason when there are none, and the providers' source row under them (D-88). The Issues below are the
// page's table, which the table view shows as it shows any view's. The data is the engine's
// (cluster.overview); the range and the group are UI state (ui.setOverviewRange,
// ui.setOverviewNodes), so the controls follow what the engine reports, whoever set it. Lens's
// documentation names the charts and their values only: the look here is Cogpit's (inferred).

// chartNoData is what an empty chart says.
const chartNoData = "No data yet"

// clusterPanel shows the cluster Overview's charts.
type clusterPanel struct {
	sh *shell

	data      *engine.ClusterOverviewModel
	master    *tipButton
	worker    *tipButton
	rangeSel  *selectField
	syncing   bool        // set while the select is set from engine state
	caption   *sourceLine // why there are no charts, or nothing while they show (metricssource.go)
	note      *textLine
	status    *providerStatusLine
	health    *overviewHealth // the nodes, pods and integrations above the charts (instrument_health.go)
	bearingOf string          // the context whose integrations the header shows (instrument_bearing.go)
	cards     [3]*overviewChartCard
	controls  *fyne.Container // the node group and the range; hidden with every chart
	reconnect *widget.Button  // Reconnect of the shown cluster (reconnect.go, D-81), at the right of the controls
	root      fyne.CanvasObject

	// Requests of the model are coalesced, as the Workloads Overview's are (overview.go).
	requests atomic.Int32
}

func newClusterPanel(sh *shell) *clusterPanel {
	p := &clusterPanel{sh: sh}
	p.master = newTipButton(sh, nil, "", func() { p.setNodes(engine.NodesMaster) })
	p.worker = newTipButton(sh, nil, "", func() { p.setNodes(engine.NodesWorker) })
	p.master.Text, p.worker.Text = "Master", "Worker"
	p.rangeSel = newSelectField(rangeLabels(false), p.setRange)
	p.rangeSel.Selected = engine.FormatChartRange(engine.DefaultOverviewWindow)
	p.caption = newSourceLine("Loading metrics…", colorMuted, theme.SizeNameCaptionText)
	p.note = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	p.note.truncate = true
	p.note.Hide()
	p.status = newProviderStatusLine(sh)
	p.status.root.Hide()
	p.health = newOverviewHealth(sh)
	for i, title := range []string{"CPU", "Memory", "Pods"} {
		p.cards[i] = newOverviewChartCard(title)
	}
	p.controls = container.NewHBox(p.master, p.worker, layout.NewSpacer(), p.rangeSel)
	p.reconnect = widget.NewButtonWithIcon(reconnectLabel, reconnectIcon(), func() { sh.reconnect(sh.ui.Context) })
	head := container.NewBorder(nil, nil, nil, container.NewHBox(newSpace(theme.SizeNamePadding), p.reconnect), p.controls)
	charts := container.NewGridWithColumns(3, p.cards[0].root, p.cards[1].root, p.cards[2].root)
	p.root = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewVBox(p.health.root, newSpace(theme.SizeNamePadding), head, charts, p.caption,
			// One row under the charts: what the history is made of at the left, quietly, and the
			// providers as chips at the right (dot, name, when it last answered).
			container.NewBorder(nil, nil, nil, p.status.root, p.note)))
	p.followReconnect()
	p.showControls()
	return p
}

// request reads the model off the main goroutine and shows it. It may be called from the main
// goroutine at any rate: requests made while one is in flight become one more.
func (p *clusterPanel) request() {
	for {
		n := p.requests.Load()
		if n >= 2 {
			return
		}
		if p.requests.CompareAndSwap(n, n+1) {
			if n == 1 {
				return // the one in flight runs again when it is done
			}
			break
		}
	}
	params := struct {
		Series   bool `json:"series"`
		NoIssues bool `json:"noIssues"`
	}{true, true}
	p.sh.dispatch("cluster.overview", params, func(res any, err error) {
		again := p.requests.Swap(0) == 2
		if err == nil {
			var m engine.ClusterOverviewModel
			if derr := decode(res, &m); derr == nil {
				p.show(&m)
			}
		}
		// An error leaves the last charts; the next poll asks again.
		if again && p.sh.ui.View == engine.ClusterOverview {
			p.request()
		}
	})
}

// show draws a model: the controls as the engine holds them, the source and status lines, the
// charts.
func (p *clusterPanel) show(m *engine.ClusterOverviewModel) {
	p.data = m
	p.showControls()
	p.health.setStates(m.NodeStates, m.PodStates)
	if p.bearingOf != p.sh.ui.Context {
		p.requestBearing()
	}
	now := p.sh.now()
	switch m.Status {
	case metrics.StatusAvailable:
		p.caption.Hide() // the source row under the charts names the providers (metricsstatus.go, D-88)
	case metrics.StatusUnknown, "":
		p.setCaption("Waiting for the first measurement…", colorMuted)
	case metrics.StatusNotServed:
		p.setCaption(noMetricsCaption(p.sh.ui, p.sh.ui.Context, m.Message), colorMuted) // a state, said quietly
	case engine.StatusHidden:
		p.setCaption("Metrics are "+m.Message, colorMuted) // the user's choice, said quietly
	default:
		p.setCaption("Metrics are not available: "+m.Message, colorStatusWarning)
	}
	note := "Pods are counted from the cluster's objects since this page opened."
	switch {
	case m.Status == metrics.StatusAvailable && !m.History:
		note = "The Metrics Server keeps no history: Cogpit keeps what it measured, the last hour at most."
	case m.Status == metrics.StatusAvailable && m.UsageHistory == metrics.UsageNone:
		note = noHistoryNote("the nodes")
	case m.PodsHistory == metrics.PodsFromKubeStateMetrics:
		note = "Pods before this page opened: kube-state-metrics through Prometheus; since then the cluster's objects."
	case m.PodsHistory == metrics.PodsFromNowhere:
		note = "Pods are counted from the cluster's objects since this page opened: Prometheus has no kube-state-metrics series."
	}
	p.note.SetText(note)
	p.note.Show()
	p.status.set(m.Providers, now)
	if m.Status == engine.StatusHidden {
		p.status.root.Hide() // nothing is asked of the providers for hidden charts
	}
	window := engine.DefaultOverviewWindow // the model's Range: its Window does not travel in JSON
	if d, err := engine.ParseChartRange(m.Range); err == nil && d > 0 {
		window = d
	}
	shown := 0
	for i, c := range p.cards {
		if i < len(m.Charts) {
			if m.Charts[i].Hidden { // the cluster's settings hide it (clustersettings_metrics.go)
				c.root.Hide()
				continue
			}
			shown++
			c.root.Show()
			c.set(m.Charts[i], window, now, p.sh.timeFormat())
		}
	}
	if shown == 0 {
		p.controls.Hide()
		p.note.Hide() // the note is about the charts
	} else {
		p.controls.Show()
	}
}

// requestBearing reads the shown cluster's integrations and shows them as the header's bearing. The
// engine answers from what its probes already hold (the cache and API discovery), so it asks the
// cluster nothing; while the user has not chosen integrations for the cluster (D-82) nothing is
// read and every token is dim. Main goroutine.
func (p *clusterPanel) requestBearing() {
	ctx := p.sh.ui.Context
	p.bearingOf = ctx
	if noIntegrationsChosen(p.sh.ui, ctx) {
		items := bearingItems(nil)
		for i := range items {
			items[i].tip = integrationNavLabels[items[i].name] + "\nNot chosen for this cluster yet"
		}
		p.health.setBearing(items)
		return
	}
	p.sh.dispatch("cluster.settings.get", clusterSettingsContext{Context: ctx}, func(res any, err error) {
		var v engine.ClusterSettingsView
		if err != nil || decode(res, &v) != nil || p.sh.ui.Context != ctx {
			p.bearingOf = "" // the next model asks again
			return
		}
		p.health.setBearing(bearingItems(v.Integrations))
	})
}

// followReconnect shows Reconnect while the shown cluster is connected (not the demo cluster),
// greyed while it reconnects. Main goroutine.
func (p *clusterPanel) followReconnect() {
	ui := p.sh.ui
	if _, ok := p.sh.eng.Registry().Get("cluster.reconnect"); !ok || !reconnectable(ui, ui.Context) {
		p.reconnect.Hide()
		return
	}
	p.reconnect.Show()
	if reconnecting(ui, ui.Context) {
		p.reconnect.SetText(reconnectingText)
		p.reconnect.Disable()
		return
	}
	p.reconnect.SetText(reconnectLabel)
	p.reconnect.Enable()
}

func (p *clusterPanel) setCaption(text string, color fyne.ThemeColorName) {
	p.caption.SetText(text)
	p.caption.SetColor(color)
	p.caption.Show()
}

// showControls marks the node group and range the engine holds (the UI state, else the model's
// defaults), and disables a group without nodes.
func (p *clusterPanel) showControls() {
	ui := p.sh.ui.Overview
	nodes, rng := ui.Nodes, ui.Range
	history := false
	counts := map[string]int{}
	if m := p.data; m != nil {
		nodes, rng, history = m.Nodes, m.Range, m.History
		for _, g := range m.Groups {
			counts[g.Name] = g.Nodes
		}
	}
	if rng == "" {
		rng = engine.FormatChartRange(engine.DefaultOverviewWindow)
	}
	for _, b := range []struct {
		btn  *tipButton
		name string
		noun string
	}{{p.master, engine.NodesMaster, "control-plane"}, {p.worker, engine.NodesWorker, "worker"}} {
		b.btn.Importance = widget.MediumImportance
		if b.name == nodes {
			b.btn.Importance = widget.HighImportance
		}
		n, known := counts[b.name]
		switch {
		case !known:
			b.btn.tip = ""
		case n == 0:
			b.btn.tip = "This cluster has no " + b.noun + " nodes"
		case n == 1:
			b.btn.tip = "1 " + b.noun + " node"
		default:
			b.btn.tip = strconv.Itoa(n) + " " + b.noun + " nodes"
		}
		if known && n == 0 && b.name != nodes {
			b.btn.Disable()
		} else {
			b.btn.Enable()
		}
		b.btn.Refresh()
	}
	opts := rangeLabels(history)
	if !slices.Contains(opts, rng) {
		opts = append(opts, rng) // a range an agent set beyond what the providers offer
	}
	p.syncing = true
	p.rangeSel.Options = opts
	p.rangeSel.Selected = rng
	p.rangeSel.Refresh()
	p.syncing = false
}

// setNodes is a click on Master or Worker: the engine holds the choice.
func (p *clusterPanel) setNodes(nodes string) {
	params := struct {
		Nodes string `json:"nodes"`
	}{nodes}
	p.sh.dispatch("ui.setOverviewNodes", params, func(_ any, err error) {
		if err != nil {
			p.sh.showError(err)
		}
	})
}

// setRange is the user's choice of the charts' time range: the engine holds it, and the charts
// follow when it answers (the UI state's change asks for the model again).
func (p *clusterPanel) setRange(label string) {
	if p.syncing || p.data != nil && label == p.data.Range {
		return
	}
	params := struct {
		Range string `json:"range"`
	}{label}
	p.sh.dispatch("ui.setOverviewRange", params, func(_ any, err error) {
		if err != nil {
			p.sh.showError(err)
			p.showControls()
		}
	})
}

// overviewChartCard is one chart of the Overview: its title and headline (the usage against the
// allocatable capacity), and the chart.
type overviewChartCard struct {
	title    *textLine
	headline *textLine
	plot     *chart
	root     fyne.CanvasObject
}

func newOverviewChartCard(title string) *overviewChartCard {
	c := &overviewChartCard{
		title:    newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}),
		headline: newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
		plot:     newChart(),
	}
	c.headline.truncate = true
	c.root = inset(theme.SizeNamePadding, "", container.NewVBox(container.NewBorder(nil, nil, c.title, nil, c.headline), c.plot))
	return c
}

// set draws a chart of the model: the usage over the range and its lines.
func (c *overviewChartCard) set(ch engine.OverviewChart, window time.Duration, now time.Time, tf model.TimeFormat) {
	axis, detail, binary := overviewFormats(ch.Metric)
	d := chartData{From: now.Add(-window), To: now, Time: tf, Empty: chartNoData, Format: axis, Detail: detail, Binary: binary}
	usage := chartSeries{Name: "Usage", Color: colorAccent}
	for _, s := range ch.Samples {
		usage.Points = append(usage.Points, chartPoint{At: s.At, Value: s.Value})
	}
	if len(usage.Points) == 0 && ch.Current != nil {
		usage.Points = append(usage.Points, chartPoint{At: ch.At, Value: *ch.Current})
	}
	d.Series = []chartSeries{usage}
	if ch.Requests != nil {
		d.Lines = append(d.Lines, chartLine{Name: "Requests", Color: colorStatusWarning, Value: *ch.Requests})
	}
	if ch.Limits != nil {
		d.Lines = append(d.Lines, chartLine{Name: "Limits", Color: colorStatusError, Value: *ch.Limits})
	}
	d.Lines = append(d.Lines,
		chartLine{Name: "Allocatable", Color: colorChartSecond, Value: ch.Allocatable, Soft: true},
		chartLine{Name: "Capacity", Color: colorMuted, Value: ch.Capacity, Soft: true})
	c.plot.Set(d)
	c.headline.SetText(overviewHeadline(ch, detail))
}

// overviewHeadline is a chart's usage against its allocatable capacity: "1.5 of 7.6 cores (20%)",
// "4.0 GiB of 14.0 GiB (29%)", "2 of 220 pods (1%)"; "–" for a usage not measured yet.
func overviewHeadline(ch engine.OverviewChart, format func(float64) string) string {
	used := "–"
	if ch.Current != nil {
		used = format(*ch.Current)
	}
	unit := ""
	switch ch.Metric {
	case engine.OverviewCPU:
		unit = " cores"
	case engine.OverviewPods:
		unit = " pods"
	}
	s := used + " of " + format(ch.Allocatable) + unit
	if ch.Current != nil && ch.Allocatable > 0 {
		s += " (" + strconv.Itoa(int(math.Round(*ch.Current/ch.Allocatable*100))) + "%)"
	}
	return s
}

// overviewFormats are a chart's axis and hover formats: cores, binary bytes, or a count of pods.
func overviewFormats(metric string) (axis, detail func(float64) string, binary bool) {
	switch metric {
	case engine.OverviewCPU:
		return formatClusterCores, formatClusterCoresExact, false
	case engine.OverviewMemory:
		return model.FormatByteAmount, model.FormatByteAmount, true // binary units, one decimal (D-32)
	}
	count := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return count, func(v float64) string { return fmt.Sprintf("%.0f", v) }, false
}

// formatClusterCores formats cores for a cluster's axis: as a drawer's ("0.25", "1.5"), without an
// exponent for a cluster's hundreds and thousands ("750", "1500").
func formatClusterCores(v float64) string {
	if v >= 100 {
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64)
	}
	return formatCores(v)
}

// formatClusterCoresExact formats cores for the headline and the hover: as a drawer's, with one
// decimal from ten cores ("285.9").
func formatClusterCoresExact(v float64) string {
	if v >= 10 {
		return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	}
	return formatCoresExact(v)
}
