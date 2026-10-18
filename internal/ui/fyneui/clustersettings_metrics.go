package fyneui

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Cluster settings › Metrics › Hide metrics from the UI (G12, G13; engine/hidemetrics.go). Lens
// has a multi-select of metrics, "Hide all metrics" and "Reset" buttons and a line that says all
// metrics are visible when none is hidden (captured). Cogpit's control: two rows of checks, the
// places metrics show in (Cluster: the Overview's charts; Nodes; Pods; Containers) and what they
// measure (CPU, Memory, Network, Filesystem), the two buttons and the line. The list's entries
// and the rows are Cogpit's (inferred: Lens's list is not captured). A change is dispatched at
// once (cluster.settings.set with hiddenMetrics) and the window follows the engine's state: the
// lists' columns, the drawer's chart variants and the Overview's charts leave, and nothing is
// polled for them.

// hiddenMetricsSetting is the setting's name on the page (its control and its refusal line).
const hiddenMetricsSetting = "hiddenMetrics"

// hideMetricsSetting builds the Hide metrics setting of the shown cluster and returns it with the
// function that shows the state in it.
func (p *settingsPage) hideMetricsSetting() (fyne.CanvasObject, func()) {
	ctx := p.shown.Context
	labels := func(place bool) []string {
		var out []string
		for _, h := range engine.HideableMetrics {
			if h.Place == place {
				out = append(out, h.Label)
			}
		}
		return out
	}
	places := widget.NewCheckGroup(labels(true), nil)
	places.Horizontal = true
	measures := widget.NewCheckGroup(labels(false), nil)
	measures.Horizontal = true
	selected := func() engine.MetricSet {
		var m engine.MetricSet
		for _, h := range engine.HideableMetrics {
			if slices.Contains(places.Selected, h.Label) || slices.Contains(measures.Selected, h.Label) {
				m |= h.Set
			}
		}
		return m
	}
	set := func(m engine.MetricSet) { p.setCluster(hiddenMetricsSetting, engine.MetricNames(m)) }
	changed := func([]string) {
		if !p.syncing {
			set(selected())
		}
	}
	places.OnChanged, measures.OnChanged = changed, changed
	hideAll := widget.NewButton("Hide all metrics", func() { set(engine.HideAll) })
	reset := widget.NewButton("Reset", func() { set(0) })
	line := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	line.truncate = true
	p.controls["hiddenPlaces"], p.controls["hiddenMeasures"] = places, measures
	p.controls["hideAllMetrics"], p.controls["resetMetrics"], p.controls["hiddenMetricsLine"] = hideAll, reset, line

	caption := func(text string) fyne.CanvasObject {
		return container.NewCenter(newTextLine(text, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	control := container.NewVBox(
		container.New(layout.NewFormLayout(), caption("Places"), places, caption("Measures"), measures),
		container.NewHBox(hideAll, reset),
		line,
	)
	sync := func() {
		m := p.sh.ui.ClusterSettings[ctx].HiddenMetrics // the UI state's, so an agent's change shows at once
		pick := func(place bool) []string {
			var out []string
			for _, h := range engine.HideableMetrics {
				if h.Place == place && m&h.Set != 0 {
					out = append(out, h.Label)
				}
			}
			return out
		}
		if want := pick(true); !slices.Equal(want, places.Selected) {
			places.SetSelected(want)
		}
		if want := pick(false); !slices.Equal(want, measures.Selected) {
			measures.SetSelected(want)
		}
		line.SetText(hiddenMetricsLine(m))
		if m == 0 {
			reset.Disable()
		} else {
			reset.Enable()
		}
		if m == engine.HideAll {
			hideAll.Disable()
		} else {
			hideAll.Enable()
		}
	}
	return p.setting(hiddenMetricsSetting, "Hide metrics from the UI",
		"Hidden metrics leave the lists, the drawers and the Overview, and Cogpit stops asking the cluster for them.", control), sync
}

// metricsSourcesSetting is the name of Metrics' sources on the page.
const metricsSourcesSetting = "metricsSources"

// metricsSourcesHelp says where each part of the metrics comes from and where the sources are set.
const metricsSourcesHelp = "Current values come from the Metrics Server; history, network, filesystem and disks from Prometheus."

// metricsSourceIntegrations are the integrations Metrics reads, in the rows' order.
var metricsSourceIntegrations = []engine.Integration{engine.IntegrationMetricsServer, engine.IntegrationPrometheus}

// metricsSourcesSetting builds Cluster settings › Metrics › Sources (D-87, the owner's WF22 of
// 2026-10-18: Metrics shows that it reads the two metrics integrations, their details live on their
// own sections): a row per integration, its state's dot in the cluster dots' colours, its name as a
// link to its section, and its status line as Integrations words it; under them, while the cluster
// has no choice of integrations, that nothing is asked (D-82). It replaces Lens's "Metrics source"
// dropdown (Automatic, Prometheus, Kubernetes Metrics Server, No metrics, captured), which since
// D-76 set the same two switches: they are on the integrations' sections now, and
// cluster.settings.set's metricsSource still sets them for agents. Returns the setting with the
// function that shows the state in it.
func (p *settingsPage) metricsSourcesSetting() (fyne.CanvasObject, func()) {
	ctx := p.shown.Context
	type sourceRow struct {
		name   engine.Integration
		dot    *statusDot
		status *textLine
	}
	rows := make([]sourceRow, 0, len(metricsSourceIntegrations))
	objs := make([]fyne.CanvasObject, 0, len(metricsSourceIntegrations)+1)
	for _, name := range metricsSourceIntegrations {
		in, _ := engine.IntegrationOf(name)
		r := sourceRow{name: name, dot: newStatusDot(), status: newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})}
		r.status.truncate = true
		link := newLinkText(in.Label, func() { p.openIntegrationSection(ctx, name) })
		row := box(dotLinkText{}, r.dot, link, r.status)
		p.controls["metricsSource:"+string(name)] = row
		rows = append(rows, r)
		objs = append(objs, row)
	}
	undecided := newTextLine(metricsSourceUndecided, colorStatusPending, theme.SizeNameCaptionText, fyne.TextStyle{})
	undecided.truncate = true
	p.controls["metricsSourceUndecided"] = undecided
	objs = append(objs, undecided)
	sync := func() {
		statuses := p.integrationStatuses()
		for _, r := range rows {
			st, ok := statuses[r.name]
			if !ok {
				st = engine.IntegrationStatus{Name: r.name, State: engine.IntegrationChecking}
			}
			text, c := integrationStatusLine(st)
			r.status.SetText(text)
			r.status.SetColor(c)
			r.dot.setLook(integrationDot(st.State))
		}
		if u := p.sh.ui; u.IntegrationsAskFirst && !u.IntegrationsDecided(ctx) {
			undecided.Show() // nothing is asked before the user chose (D-82)
		} else {
			undecided.Hide()
		}
	}
	return p.setting(metricsSourcesSetting, "Sources", metricsSourcesHelp, container.NewVBox(objs...)), sync
}

// metricsSourceUndecided is the line under the sources while the user chose no integrations for the
// cluster: nothing of them is asked until then (D-82), its metrics included.
const metricsSourceUndecided = "Not chosen for this cluster yet: no metrics are asked until you choose (in Integrations, or in its question)."

// dotLinkText lays out a status dot, a link at its natural width, and a text line in the rest of the
// row, each centred in the row's height.
type dotLinkText struct{}

func (dotLinkText) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

func (l dotLinkText) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(objs[0].MinSize().Width+l.gap()+textWidthOf(objs[1])+l.gap(), h)
}

func (l dotLinkText) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for i, o := range objs {
		w := o.MinSize().Width
		switch i {
		case 1:
			w = textWidthOf(o)
		case 2:
			w = max(size.Width-x, 0)
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
		o.Resize(fyne.NewSize(w, h))
		x += w + l.gap()
	}
}

// hiddenMetricsLine says what is hidden: "All metrics are visible.", or the hidden ones.
func hiddenMetricsLine(m engine.MetricSet) string {
	switch m {
	case 0:
		return "All metrics are visible."
	case engine.HideAll:
		return "All metrics are hidden."
	}
	return "Hidden: " + strings.Join(m.Labels(), ", ") + "."
}

// showVariants shows a chart's variant buttons the cluster's settings leave visible (all of them
// for a panel without the engine's list yet) and hides the others; the row closes up around them.
func (c *chartCtl) showVariants(visible []string) {
	for id, b := range c.variants {
		if visible == nil || slices.Contains(visible, id) {
			b.Show()
		} else {
			b.Hide()
		}
	}
	c.controls.Refresh()
}

// variantsShown are the variants the panel's charts offer: the engine's list, nil (all) before it
// answered.
func (p *metricsPanel) variantsShown() []string {
	if p.data == nil || p.data.Variants == nil {
		return nil
	}
	return p.data.Variants
}
