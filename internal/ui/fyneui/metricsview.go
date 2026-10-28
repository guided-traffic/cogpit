package fyneui

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer's Metrics section (G12, architecture §9), after Lens's (capture, 2026-10-12): above the
// Properties of a pod or node, a line naming the source ("Displaying metrics from Kubernetes
// Metrics Server", "Displaying metrics from Prometheus (monitoring/prometheus-server)"), and
// charts, each with its own time range (1h at first), a button for each variant (CPU, Memory,
// Network, Filesystem), a flag button that marks the object's events on the chart, and the chart
// with the requests and limits as dashed lines (CPU and Memory). Network and Filesystem need a
// provider that has them (Prometheus) and are disabled with that reason otherwise. Each container
// of a pod gets a compact chart of its own under its section's title.
//
// A chart's time range is engine state (ui.setMetricsRange), so that agents see and set it: the
// range control asks the engine and follows what the engine reports, whoever changed it.
//
// The panel keeps its widgets across the drawer's rebuilds (drawer.kept) and updates them in place
// when the engine reports new numbers (engine.EventMetrics, about every 15 seconds while the drawer
// is open), so a new measurement redraws the chart and nothing else.

const (
	metricsKey = "metrics"

	variantCPU        = "cpu"
	variantMemory     = "memory"
	variantNetwork    = "network"
	variantFilesystem = "filesystem"

	needsPrometheus = "needs Prometheus"

	// networkPerPod says under a container's Network chart that the numbers are the pod's.
	networkPerPod = "Network is per pod: its containers share it."
)

// metricsRanges are the time ranges a chart offers, labelled as ui.setMetricsRange writes them.
// Lens's default, 1h, is captured; the others are Cogpit's choice (docs/parity/ui-parity.md). The
// ranges beyond an hour need a provider with history (Prometheus): the Metrics Server keeps none,
// and the hub's series of it holds an hour at most.
var metricsRanges = []time.Duration{
	5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour,
	3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour,
	14 * 24 * time.Hour, 30 * 24 * time.Hour,
}

// defaultMetricsWindow is Lens's default time range.
const defaultMetricsWindow = time.Hour

// rangeLabels are the labels of the ranges a chart offers: up to an hour without history.
func rangeLabels(history bool) []string {
	out := make([]string, 0, len(metricsRanges))
	for _, d := range metricsRanges {
		if history || d <= time.Hour {
			out = append(out, engine.FormatChartRange(d))
		}
	}
	return out
}

type metricsPanel struct {
	*chartCtl // the pod's or node's own chart and controls

	d   *drawer
	ref model.ObjectRef

	data   *engine.ObjectMetrics
	events []engine.EventItem
	loads  int
	closed bool

	// volume says the section is a persistent volume claim's or a persistent volume's: one chart of
	// the volume's filesystem, no variant buttons, a line above it that says whose usage it shows
	// (intro) and a usage bar under it (metricsvolume.go).
	volume bool
	intro  *sourceLine // the other object's name a link
	pods   *sourceLine // the pods that mount the claim, each a link

	root    *fyne.Container
	caption *sourceLine // why there are no charts, or nothing while they show (metricssource.go)
	note    *textLine
	status  *providerStatusLine  // the providers' source row under the charts (metricsstatus.go, D-88)
	disk    *diskBars            // a node's disks under its chart, or why there are none (metricsdisk.go)
	perBox  map[string]*chartCtl // each container's compact chart and controls, by container name
}

// chartCtl is one chart with its own row of controls: the time range, the variant, the events
// flag. Its selection is its own, so changing one chart changes only that chart; the panel's data
// (one poll per drawer) feeds them all. It lives as long as the panel, so its selection survives
// the drawer's rebuilds and the polls.
type chartCtl struct {
	p       *metricsPanel
	name    string // the container's name; "" for the pod or node
	compact bool

	variant        string        // as the engine reports it (MetricsTarget.Variant), or as just asked for
	variantPending bool          // the engine has not answered the variant just asked for
	window         time.Duration // as the engine reports it (MetricsTarget.Window), or as just asked for
	pending        bool          // the engine has not answered the range just asked for
	showEvents     bool

	root     *fyne.Container // a container's controls and chart; nil for the pod's, which the panel's root holds
	controls *fyne.Container
	rangeSel *selectField // the pod's; a container's chart has rangeBtn
	rangeBtn *tipButton
	variants map[string]*tipButton
	flag     *tipButton
	plot     *chart
	hint     *textLine // under the chart: what its numbers are (a container's network is the pod's)
}

// metricsVariants are the buttons of every chart: icons, as Lens's are, with the name as the
// tooltip (owner, 2026-10-12).
var metricsVariants = []struct {
	id, label string
	icon      fyne.Resource
}{
	{variantCPU, "CPU", cpuIcon}, {variantMemory, "Memory", memoryIcon},
	{variantNetwork, "Network", networkIcon}, {variantFilesystem, "Filesystem", filesystemIcon},
}

func newMetricsPanel(d *drawer, ref model.ObjectRef, volume bool) *metricsPanel {
	p := &metricsPanel{d: d, ref: ref, perBox: map[string]*chartCtl{}, volume: volume}
	p.chartCtl = newChartCtl(p, "", false)
	p.intro = newSourceLine("")
	p.intro.Hide()
	p.pods = newSourceLine("")
	p.pods.Hide()
	if volume {
		p.variant = variantFilesystem
		p.showVariants([]string{}) // its one variant needs no button
	}
	p.caption = newSourceLine("Loading metrics…")
	p.note = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	p.note.truncate = true
	p.status = newProviderStatusLine(d.sh)
	p.status.context = ref.Context
	p.status.setAvail(d.contentWidth)
	p.status.root.Hide()
	p.disk = newDiskBars()
	p.root = container.NewVBox(p.intro, p.pods, p.controls, p.plot, p.hint, p.disk, p.caption, p.note, p.status.root)
	p.render()
	return p
}

// newChartCtl makes a chart with its controls: CPU selected, the default range, events off.
func newChartCtl(p *metricsPanel, name string, compact bool) *chartCtl {
	c := &chartCtl{p: p, name: name, compact: compact, variant: variantCPU, window: defaultMetricsWindow, variants: map[string]*tipButton{}}
	sh := p.d.sh

	// The pod's chart picks its range in a select; a container's compact chart in a small button
	// that opens a menu, so that its whole row of controls fits the drawer's minimum width.
	ctl := make([]fyne.CanvasObject, 0, 3+len(metricsVariants))
	label := engine.FormatChartRange(defaultMetricsWindow)
	if compact {
		c.rangeBtn = newTipButton(sh, nil, "Time range", nil)
		c.rangeBtn.Text, c.rangeBtn.Importance = label, widget.MediumImportance
		c.rangeBtn.OnTapped = func() { c.rangeMenu() }
		ctl = append(ctl, c.rangeBtn)
	} else {
		c.rangeSel = newSelectField(rangeLabels(false), c.setRange)
		c.rangeSel.Selected = label
		ctl = append(ctl, c.rangeSel)
	}
	for _, v := range metricsVariants {
		id := v.id
		b := newTipButton(sh, v.icon, v.label, func() { c.setVariant(id) })
		c.variants[id] = b
		ctl = append(ctl, b)
	}
	c.enableVariants(metrics.Capabilities{})

	c.flag = newTipButton(sh, flagIcon, "Show events", func() {
		c.showEvents = !c.showEvents
		c.p.render()
	})
	if !compact {
		ctl = append(ctl, newDivider(true))
	}
	ctl = append(ctl, c.flag)
	c.controls = box(&flowLayout{avail: p.d.contentWidth}, ctl...)
	c.plot = newChart()
	c.hint = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	c.hint.truncate = true
	c.hint.Hide()
	return c
}

// enableVariants enables the variants the provider has: CPU and Memory always, Network and
// Filesystem with a provider that has them (Prometheus), else disabled with the reason. It reports
// whether the chart's variant is among them.
func (c *chartCtl) enableVariants(caps metrics.Capabilities) bool {
	have := map[string]bool{variantCPU: true, variantMemory: true, variantNetwork: caps.Network, variantFilesystem: caps.Filesystem}
	for _, v := range metricsVariants {
		b := c.variants[v.id]
		if have[v.id] {
			b.tip = v.label
			if b.Disabled() {
				b.Enable()
			}
			continue
		}
		b.tip = v.label + ": " + needsPrometheus
		if !b.Disabled() {
			b.Disable()
		}
	}
	return have[c.variant]
}

// close stops the panel's loads from landing (keptCloser).
func (p *metricsPanel) close() { p.closed = true }

// setRange is the user's choice of this chart's time range, by its label: the chart shows it at once
// and the engine, which holds the range (ui.setMetricsRange), is told; a poll it starts brings the
// history. Until the engine answered, the panel's loads do not put the old range back. A drawer
// that shows another object by now refuses quietly; any other refusal shows; either way the chart
// follows the engine's range again.
func (c *chartCtl) setRange(label string) {
	d, err := engine.ParseChartRange(label)
	if err != nil || d <= 0 || d == c.window {
		c.showRange()
		return
	}
	c.window, c.pending = d, true
	c.showRange()
	p := c.p
	p.render()
	params := struct {
		Ref       model.ObjectRef `json:"ref"`
		Container string          `json:"container,omitempty"`
		Range     string          `json:"range"`
	}{p.ref, c.name, label}
	p.d.sh.dispatch("ui.setMetricsRange", params, func(_ any, err error) {
		c.pending = false
		if err != nil && !errors.Is(err, engine.ErrDrawerNotOpen) {
			p.d.sh.showError(err)
		}
		p.refresh() // the engine's range from now on, whatever the answer
	})
}

// showRange shows the chart's range on its select or button, without asking the engine.
func (c *chartCtl) showRange() {
	label := engine.FormatChartRange(c.window)
	if c.rangeSel != nil && c.rangeSel.Selected != label {
		c.rangeSel.Selected = label
		c.rangeSel.Refresh()
	}
	if c.rangeBtn != nil && c.rangeBtn.Text != label {
		c.rangeBtn.Text = label
		c.rangeBtn.Refresh()
	}
}

// follow takes the engine's state for this chart: its time range, and the variants and ranges the
// provider offers. A chart that shows a variant the provider does not have (any more) goes back to
// CPU; a moment without metrics (a failed poll) keeps the choice.
func (c *chartCtl) follow() {
	p := c.p
	var caps metrics.Capabilities
	available := p.data != nil && p.data.Status == metrics.StatusAvailable
	if available {
		caps = p.data.Capabilities
	}
	if t := p.target(c.name); t != nil && t.Variant != "" && !c.variantPending {
		c.variant = t.Variant // the engine's, whoever set it (ui.setMetricsVariant)
	}
	visible := p.variantsShown() // the cluster's settings may hide some (clustersettings_metrics.go)
	if p.volume {
		c.showVariants([]string{}) // a volume's one variant needs no button
	} else {
		c.showVariants(visible)
	}
	if (!c.enableVariants(caps) || visible != nil && !slices.Contains(visible, c.variant)) && available {
		c.variant = variantCPU
		if len(visible) > 0 && !slices.Contains(visible, variantCPU) {
			c.variant = visible[0]
		}
	}
	if c.rangeSel != nil {
		if labels := rangeLabels(caps.History); !slices.Equal(labels, c.rangeSel.Options) {
			c.rangeSel.SetOptions(labels)
		}
	}
	if t := p.target(c.name); t != nil && t.Window > 0 && !c.pending {
		c.window = t.Window
	}
	c.showRange()
}

// rangeMenu opens the menu of ranges under a compact chart's range button.
func (c *chartCtl) rangeMenu() {
	history := c.p.data != nil && c.p.data.Status == metrics.StatusAvailable && c.p.data.Capabilities.History
	labels := rangeLabels(history)
	items := make([]*fyne.MenuItem, len(labels))
	for i, label := range labels {
		items[i] = fyne.NewMenuItem(label, func() { c.setRange(label) })
		items[i].Checked = label == engine.FormatChartRange(c.window)
	}
	if cnv := fyne.CurrentApp().Driver().CanvasForObject(c.rangeBtn); cnv != nil {
		widget.ShowPopUpMenuAtRelativePosition(fyne.NewMenu("", items...), cnv, fyne.NewPos(0, c.rangeBtn.Size().Height), c.rangeBtn)
	}
}

// setVariant switches this chart to CPU, Memory, Network or Filesystem: at once on screen, and in
// the engine, which holds the variant (ui.setMetricsVariant) and asks Prometheus for the network
// and the filesystem only while a chart shows them. Until the engine answered, the panel's loads do
// not put the old variant back; then the chart follows the engine's again, whatever the answer.
func (c *chartCtl) setVariant(id string) {
	if id == c.variant {
		return
	}
	c.variant, c.variantPending = id, true
	p := c.p
	p.render()
	params := struct {
		Ref       model.ObjectRef `json:"ref"`
		Container string          `json:"container,omitempty"`
		Variant   string          `json:"variant"`
	}{p.ref, c.name, id}
	p.d.sh.dispatch("ui.setMetricsVariant", params, func(_ any, err error) {
		c.variantPending = false
		if err != nil && !errors.Is(err, engine.ErrDrawerNotOpen) {
			p.d.sh.showError(err)
		}
		p.refresh()
	})
}

// setEvents hands the panel the object's events, whose times the flag button marks.
func (p *metricsPanel) setEvents(ev []engine.EventItem) {
	if slices.Equal(ev, p.events) {
		return
	}
	p.events = ev
	for _, c := range p.charts() {
		if c.showEvents {
			c.update()
		}
	}
}

// charts are the pod's or node's chart and the containers'.
func (p *metricsPanel) charts() []*chartCtl {
	out := make([]*chartCtl, 1, 1+len(p.perBox))
	out[0] = p.chartCtl
	for _, c := range p.perBox {
		out = append(out, c)
	}
	return out
}

// refresh reads the engine's current numbers off the main goroutine and renders them.
func (p *metricsPanel) refresh() {
	p.loads++
	n, ref, sh := p.loads, p.ref, p.d.sh
	go func() {
		defer sh.recoverTo("metrics")
		m, err := sh.eng.ObjectMetrics(ref)
		sh.do(func() {
			if p.closed || n != p.loads {
				return
			}
			p.apply(m, err)
		})
	}()
}

func (p *metricsPanel) apply(m *engine.ObjectMetrics, err error) {
	if err != nil || m == nil {
		m = &engine.ObjectMetrics{Ref: p.ref, Status: metrics.StatusUnavailable, Message: "could not read the metrics"}
		if err != nil {
			m.Message = err.Error()
		}
	}
	hadBoxes := len(p.containerNames())
	p.data = m
	for _, c := range p.charts() {
		c.follow()
	}
	p.render()
	if len(p.containerNames()) != hadBoxes {
		p.d.rebuild() // the containers' charts appear with the first numbers
	}
}

// containerNames are the containers the panel has numbers for.
func (p *metricsPanel) containerNames() []string {
	var names []string
	if p.data != nil && p.data.Status == metrics.StatusAvailable {
		for _, t := range p.data.Targets {
			if t.Name != "" {
				names = append(names, t.Name)
			}
		}
	}
	return names
}

// containerChart returns the chart of a container with its own controls, or nil without numbers
// for it.
func (p *metricsPanel) containerChart(name string) fyne.CanvasObject {
	if !slices.Contains(p.containerNames(), name) {
		return nil
	}
	c := p.perBox[name]
	if c == nil {
		c = newChartCtl(p, name, true)
		c.root = container.NewVBox(c.controls, c.plot, c.hint)
		p.perBox[name] = c
		c.follow()
		c.update()
	}
	return c.root
}

// render draws the state: the source line or the reason there is none, and the charts.
func (p *metricsPanel) render() {
	m := p.data
	switch {
	case m == nil:
		p.setState(true, "Loading metrics…")
	case m.Status == metrics.StatusAvailable:
		p.setState(true, "")
		p.caption.Hide() // the source row under the charts names the providers (metricsstatus.go, D-88)
	case m.Status == metrics.StatusUnknown:
		p.setState(true, "Waiting for the first measurement…")
	case m.Status == metrics.StatusNotServed:
		// A cluster without metrics-server or Prometheus is a state, not a problem: said quietly.
		p.setState(false, noMetricsCaption(p.d.sh.ui, p.ref.Context, m.Message))
		p.caption.SetColor(colorMuted)
	case m.Status == engine.StatusHidden: // the cluster's settings hide them: the user's choice, said quietly
		p.setState(false, "Metrics are "+m.Message)
		p.caption.SetColor(colorMuted)
	default:
		p.setState(false, "Metrics are not available: "+m.Message)
	}
	if m != nil && m.Status == metrics.StatusAvailable {
		var note string
		if t := p.target(""); t != nil && t.Current == nil && showsUsage(t.Variant) {
			// A chart of the network or the filesystem alone asks for no CPU or memory (the hub's
			// SetUsage), so it has no current values: not a missing measurement.
			note = "No metrics for this " + singular(p.d.sh.kindTitle(p.ref.Kind)) + " yet."
		} else if t != nil && !m.Capabilities.History && !p.coversWindow(t) {
			note = "The Metrics Server keeps no history: Cogpit keeps what it measured, the last hour at most."
		} else if m.Capabilities.History && m.HistorySource == metrics.UsageNone {
			note = noHistoryNote("this " + singular(p.d.sh.kindTitle(p.ref.Kind)))
		}
		p.setNote(note)
	}
	if m != nil {
		p.status.set(m.Providers, p.d.sh.now())
	}
	p.showIntro(m) // a volume's: whose usage it shows (metricsvolume.go)
	p.showDisk(m)
	for _, c := range p.charts() {
		c.update()
	}
	p.root.Refresh()
}

// showsUsage reports whether a chart's variant, as the engine reports it, is CPU or memory ("": the
// default, CPU).
func showsUsage(variant string) bool {
	return variant == "" || variant == variantCPU || variant == variantMemory
}

// setState shows or hides the controls and the chart, and sets the caption; without charts there is
// no note either.
func (p *metricsPanel) setState(charts bool, caption string) {
	for _, o := range []fyne.CanvasObject{p.controls, p.plot} {
		if charts {
			o.Show()
		} else {
			o.Hide()
		}
	}
	if !charts {
		p.hint.Hide()
	}
	if !charts {
		p.caption.SetColor(colorStatusWarning)
	} else {
		p.caption.SetColor(colorMuted)
	}
	if caption != "" { // "": the caller hides the line, the source row says it
		p.caption.SetText(caption)
		p.caption.Show()
	}
	if !charts {
		p.setNote("")
	}
}

// noHistoryNote says that the cluster's Prometheus has no CPU or memory series of what the charts
// show (engine HistorySource "none", A43): no node-exporter or cAdvisor series of a node, none of
// a pod, so the charts begin where Cogpit began measuring rather than an hour back.
func noHistoryNote(what string) string {
	return "Prometheus has no history of " + what + "; Cogpit shows what it measured."
}

// setNote shows a note under the source line, or no line at all for none, so that the status line
// follows the source line without a gap.
func (p *metricsPanel) setNote(text string) {
	p.note.SetText(text)
	if text == "" {
		p.note.Hide()
	} else {
		p.note.Show()
	}
}

func (p *metricsPanel) target(name string) *engine.MetricsTarget {
	if p.data == nil {
		return nil
	}
	for i := range p.data.Targets {
		if p.data.Targets[i].Name == name {
			return &p.data.Targets[i]
		}
	}
	return nil
}

// coversWindow reports whether the series reaches back over the chosen range.
func (p *metricsPanel) coversWindow(t *engine.MetricsTarget) bool {
	if len(t.Samples) == 0 {
		return false
	}
	return p.d.sh.now().Sub(t.Samples[0].At) >= p.window
}

// update shows the chart's selection on its buttons and fills the chart.
func (c *chartCtl) update() {
	for id, b := range c.variants {
		b.Importance = widget.MediumImportance
		if id == c.variant {
			b.Importance = widget.HighImportance
		}
		b.Refresh()
	}
	c.flag.Importance = widget.LowImportance
	c.flag.tip = "Show events"
	if c.showEvents {
		c.flag.Importance = widget.HighImportance
		c.flag.tip = "Hide events"
	}
	c.flag.Refresh()
	c.renderTarget()
	if c.root != nil {
		c.root.Refresh()
	}
}

// renderTarget fills the chart with its target's series in the chart's variant, the requests and
// limits (CPU and Memory), the hint under it and the events.
func (c *chartCtl) renderTarget() {
	p := c.p
	to := p.d.sh.now()
	d := chartData{
		From: to.Add(-c.window), To: to, Compact: c.compact, Time: p.d.sh.timeFormat(), Empty: "No data yet",
		Binary: c.variant != variantCPU, // bytes, or bytes per second
	}
	t := p.target(c.name)
	if t == nil || p.data == nil || p.data.Status != metrics.StatusAvailable {
		d.Format, d.Detail = variantFormats(c.variant, "")
		c.setHint("")
		c.plot.Set(d)
		return
	}
	hint := ""
	switch c.variant {
	case variantNetwork:
		d.Series, d.Format, d.Detail = networkSeries(t.Network)
		if t.NetworkOfPod {
			hint = networkPerPod
		}
	case variantFilesystem:
		if p.volume {
			d.Series, d.Format, d.Detail = volumeSeries(t.Filesystem) // metricsvolume.go
		} else {
			d.Series, d.Format, d.Detail = filesystemSeries(t.Filesystem, p.data.FilesystemSource)
		}
	default:
		d.Series, d.Lines, d.Format, d.Detail = usageSeries(t, c.variant == variantCPU, p.ref.Kind == string(model.UsageNodes))
	}
	if c.showEvents {
		for _, e := range p.events {
			if e.LastSeen > 0 {
				d.Markers = append(d.Markers, chartMarker{
					At: time.Unix(e.LastSeen, 0), Name: "Events", Color: colorStatusPending,
					Reason: e.Reason, Message: e.Message, Count: e.Count,
				})
			}
		}
	}
	c.setHint(hint)
	c.plot.Set(d)
}

// setHint shows a line under the chart, or hides it.
func (c *chartCtl) setHint(text string) {
	c.hint.SetText(text)
	if text == "" {
		c.hint.Hide()
	} else {
		c.hint.Show()
	}
}

// variantFormats are a variant's axis and hover formats: cores, bytes, or bytes per second (the
// network, and the filesystem's read and write rates). Bytes read the same on the axis and in the
// hover panel: binary units with one decimal (D-32).
func variantFormats(variant, fsSource string) (axis, detail func(float64) string) {
	switch {
	case variant == variantCPU:
		return formatCores, formatCoresExact
	case variant == variantNetwork, variant == variantFilesystem && fsSource == filesystemIO:
		return model.FormatByteRate, model.FormatByteRate
	}
	return model.FormatByteAmount, model.FormatByteAmount
}

// filesystemIO is ObjectMetrics.FilesystemSource for read and write rates instead of bytes in use.
const filesystemIO = "io"

// usageSeries is the CPU or Memory chart: the usage, and the requests and limits (a node's
// allocatable capacity and capacity) as lines.
func usageSeries(t *engine.MetricsTarget, cpu, node bool) ([]chartSeries, []chartLine, func(float64) string, func(float64) string) {
	value := func(u model.Usage) float64 {
		if cpu {
			return float64(u.CPUMilli) / 1000
		}
		return float64(u.MemoryBytes)
	}
	title, variant := "Memory usage", variantMemory
	if cpu {
		title, variant = "CPU usage", variantCPU
	}
	// A point without this part (Usage.Missing: Prometheus had memory but no CPU rate yet) is left
	// out, not drawn as 0.
	has := func(u model.Usage) bool { return cpu && u.HasCPU() || !cpu && u.HasMemory() }
	s := chartSeries{Name: title, Color: colorAccent}
	for _, sm := range t.Samples {
		if has(sm.Usage) {
			s.Points = append(s.Points, chartPoint{At: sm.At, Value: value(sm.Usage)})
		}
	}
	if len(s.Points) == 0 && t.Current != nil && has(*t.Current) {
		s.Points = append(s.Points, chartPoint{At: t.At, Value: value(*t.Current)})
	}
	lines := []chartLine{
		{Name: "Requests", Color: colorStatusWarning, Value: value(t.Requests)},
		{Name: "Limits", Color: colorStatusError, Value: value(t.Limits)},
	}
	if node {
		lines = []chartLine{
			{Name: "Allocatable capacity", Color: colorStatusWarning, Value: value(t.Allocatable)},
			{Name: seriesCapacity, Color: colorStatusError, Value: value(t.Capacity)},
		}
	}
	axis, detail := variantFormats(variant, "")
	return []chartSeries{s}, lines, axis, detail
}

// networkSeries is the Network chart: bytes per second received and transmitted.
func networkSeries(samples []model.NetworkSample) ([]chartSeries, func(float64) string, func(float64) string) {
	rx := chartSeries{Name: "Receive", Color: colorAccent}
	tx := chartSeries{Name: "Transmit", Color: colorChartSecond}
	for _, sm := range samples {
		rx.Points = append(rx.Points, chartPoint{At: sm.At, Value: sm.ReceiveBytesPerSec})
		tx.Points = append(tx.Points, chartPoint{At: sm.At, Value: sm.TransmitBytesPerSec})
	}
	axis, detail := variantFormats(variantNetwork, "")
	return []chartSeries{rx, tx}, axis, detail
}

// filesystemSeries is the Filesystem chart: the bytes in use, or with the source "io" the bytes per
// second read and written.
func filesystemSeries(samples []model.FilesystemSample, source string) ([]chartSeries, func(float64) string, func(float64) string) {
	axis, detail := variantFormats(variantFilesystem, source)
	if source == filesystemIO {
		rd := chartSeries{Name: "Read", Color: colorAccent}
		wr := chartSeries{Name: "Write", Color: colorChartSecond}
		for _, sm := range samples {
			rd.Points = append(rd.Points, chartPoint{At: sm.At, Value: sm.ReadBytesPerSec})
			wr.Points = append(wr.Points, chartPoint{At: sm.At, Value: sm.WriteBytesPerSec})
		}
		return []chartSeries{rd, wr}, axis, detail
	}
	u := chartSeries{Name: seriesUsage, Color: colorAccent}
	for _, sm := range samples {
		u.Points = append(u.Points, chartPoint{At: sm.At, Value: sm.UsageBytes})
	}
	return []chartSeries{u}, axis, detail
}

// flagIcon is the events toggle's icon: a flag, as Lens's "Show events" button has (a Material
// glyph, drawn in the theme's foreground like Fyne's own icons).
var flagIcon = theme.NewThemedResource(fyne.NewStaticResource("flag.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M14.4 6 14 4H5v17h2v-7h5.6l.4 2h7V6z"/></svg>`)))

// The variant icons are Cogpit's own drawings (clean-room, D-12), filled shapes on a 24-unit grid
// so that Fyne draws them in the theme's foreground like its own icons.
var (
	// cpuIcon is a chip with a pin on each side.
	cpuIcon = metricIcon("cpu.svg", `<path fill-rule="evenodd" d="M7 7h10v10H7zm3 3v4h4v-4z"/>`+
		`<path d="M9 4h2v3H9zm4 0h2v3h-2zM9 17h2v3H9zm4 0h2v3h-2zM4 9h3v2H4zm0 4h3v2H4zm13-4h3v2h-3zm0 4h3v2h-3z"/>`)
	// memoryIcon is a memory module: a board with three chips and its contacts.
	memoryIcon = metricIcon("memory.svg", `<path fill-rule="evenodd" d="M2 6h20v9H2zm2.5 2.5v4h4v-4zm5.5 0v4h4v-4zm5.5 0v4h4v-4z"/>`+
		`<path d="M4 16h1.5v3H4zm3 0h1.5v3H7zm3 0h1.5v3H10zm3 0h1.5v3H13zm3 0h1.5v3H16zm3 0h1.5v3H19z"/>`)
	// networkIcon is traffic both ways: an arrow up and an arrow down.
	networkIcon = metricIcon("network.svg", `<path d="M8 3 3 8h3.5v12h3V8H13zm8 18 5-5h-3.5V4h-3v12H11z"/>`)
	// filesystemIcon is a stack of three drives, each with its light.
	filesystemIcon = metricIcon("filesystem.svg", `<path fill-rule="evenodd" d="M3 3h18v5H3zm13 2v1.5h2V5zM3 9.5h18v5H3zm13 2V13h2v-1.5zM3 16h18v5H3zm13 2v1.5h2V18z"/>`)
)

// metricIcon is a themed icon from SVG elements on a 24-unit grid.
func metricIcon(name, elements string) fyne.Resource {
	return theme.NewThemedResource(fyne.NewStaticResource(name,
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">`+elements+`</svg>`)))
}

// formatCores formats cores for the value axis: "0.25", "1.5", "2".
func formatCores(v float64) string { return fmt.Sprintf("%.3g", v) }

// formatCoresExact formats cores for the hover panel: millicores below one core ("250m"), else
// cores with up to three decimals ("1.25").
func formatCoresExact(v float64) string {
	if v < 1 {
		return fmt.Sprintf("%dm", int(math.Round(v*1000)))
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}

// metricsPanel returns the drawer's Metrics panel for the shown object, made on first use; nil for
// a kind without a Metrics section.
func (d *drawer) metricsPanel(m *engine.DetailModel) *metricsPanel {
	if !m.Metrics {
		return nil
	}
	p, ok := d.kept[metricsKey].(*metricsPanel)
	if !ok || p.ref != m.Ref || p.volume != m.VolumeMetrics {
		p = newMetricsPanel(d, m.Ref, m.VolumeMetrics)
		d.kept[metricsKey] = p
		p.refresh()
	} else if p.volume {
		p.refresh() // the pods that mount the claim changed, or its binding: the engine says which
	}
	p.setEvents(m.Events)
	return p
}

// metricsChanged runs when the engine reports new metrics for the open drawer.
func (d *drawer) metricsChanged() {
	if p, ok := d.kept[metricsKey].(*metricsPanel); ok {
		p.refresh()
	}
}

// metricsSection returns the objects of the Metrics section: its title, the divider and the panel.
func (d *drawer) metricsSection(p *metricsPanel) []fyne.CanvasObject {
	return []fyne.CanvasObject{
		newSectionHead("Metrics", 0),
		newSpace(theme.SizeNamePadding), p.root,
	}
}

// flowLayout lays its objects out left to right and starts a new line when the next one does not
// fit in the available width, so that a row of buttons never asks for more than the drawer has.
type flowLayout struct {
	avail func() float32
}

func (l *flowLayout) width(size fyne.Size) float32 {
	if size.Width > 0 {
		return size.Width
	}
	if l.avail != nil {
		return l.avail()
	}
	return 0
}

// place returns each visible object's position for a width, and the height used.
func (l *flowLayout) place(objs []fyne.CanvasObject, width float32) ([]fyne.Position, float32) {
	gap := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
	pos := make([]fyne.Position, len(objs))
	var x, y, lineH float32
	for i, o := range objs {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		if x > 0 && x+ms.Width > width {
			x, y, lineH = 0, y+lineH+gap, 0
		}
		pos[i] = fyne.NewPos(x, y)
		x += ms.Width + gap
		lineH = fyne.Max(lineH, ms.Height)
	}
	return pos, y + lineH
}

// MinSize implements fyne.Layout: the widest object, and the height of the lines that fit the
// available width.
func (l *flowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w float32
	for _, o := range objs {
		if o.Visible() {
			w = fyne.Max(w, o.MinSize().Width)
		}
	}
	_, h := l.place(objs, fyne.Max(l.width(fyne.Size{}), w))
	return fyne.NewSize(w, h)
}

// Layout implements fyne.Layout.
func (l *flowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pos, _ := l.place(objs, l.width(size))
	for i, o := range objs {
		if o.Visible() {
			o.Move(pos[i])
			o.Resize(o.MinSize())
		}
	}
}

// containerTitleHasChart reports whether a drawer section is a container whose metrics the panel
// has: the sections of the group "Containers" are titled by the container's name.
func containerTitleHasChart(sec model.DetailSection) bool {
	return sec.Group == "Containers" && strings.TrimSpace(sec.Title) != ""
}
