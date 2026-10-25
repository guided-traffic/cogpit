package fyneui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Diagnostics dock tab (D-13 cache transparency; engine/diagnostics_view.go): what the status
// bar sums up, in full: the connected cluster's API requests by verb and resource, its watches
// with their state, and the objects and the memory they take per kind (an estimate, cache/size.go),
// read with diagnostics.get from Cogpit's own counters whenever they move (EventDiagnostics, at
// most once a second). Its first line sums the cluster's objects and memory, says where discovery
// came from (the disk cache or the server, D-65), and counts the table snapshots built since start,
// in every state (connected, the demo cluster, no cluster): the only place the GUI shows them since
// the status bar dropped them (owner, 2026-10-14). With several clusters connected (D-69 item 3,
// stage 2, 2026-10-16) a line per connected cluster follows it, the shown one first: what each one
// costs, its API requests, watches, objects and memory (DiagnosticsReport.Clusters), so that the
// cost of keeping clusters connected stays visible; the columns stay the shown cluster's. Its
// layout and wording are Cogpit's (Lens has no such view).

// diagnosticsTab is the tab's content; it is the dock content's editor (dockEditor), so the dock
// closes it.
type diagnosticsTab struct {
	sh *shell

	root       *fyne.Container
	summary    *textLine
	clusters   *fyne.Container   // a line per connected cluster while more than one is (D-69)
	perCluster fyne.CanvasObject // clusters with its inset; hidden while it has no line, so the layout stays
	requests   *fyne.Container
	watches    *fyne.Container
	kinds      *fyne.Container

	report engine.DiagnosticsReport
	seq    int
	closed bool
}

var _ dockEditor = (*diagnosticsTab)(nil)

func newDiagnosticsTab(sh *shell) *diagnosticsTab {
	d := &diagnosticsTab{sh: sh, requests: container.NewVBox(), watches: container.NewVBox(), kinds: container.NewVBox(),
		clusters: container.NewVBox()}
	d.summary = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	d.summary.truncate = true
	column := func(title string, body *fyne.Container) fyne.CanvasObject {
		return container.NewBorder(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, heading(title)), nil, nil, nil,
			newVScroll(inset(theme.SizeNameInnerPadding, "", body)))
	}
	cols := container.NewGridWithColumns(3,
		column("API requests by verb and resource", d.requests),
		column("Watches", d.watches),
		column("Objects and memory per kind", d.kinds))
	d.perCluster = inset(theme.SizeNameInnerPadding, "", d.clusters)
	d.perCluster.Hide()
	d.root = container.NewBorder(container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, d.summary),
		d.perCluster, newDivider(false)),
		nil, nil, nil, cols)
	d.reload()
	return d
}

// remote implements dockEditor; the tab has no text.
func (d *diagnosticsTab) remote(uint64) {}

// close implements dockEditor: answers that come later are dropped.
func (d *diagnosticsTab) close() { d.closed = true }

// reload reads the counters again.
func (d *diagnosticsTab) reload() {
	d.seq++
	seq := d.seq
	d.sh.dispatch("diagnostics.get", struct{}{}, func(res any, err error) {
		if d.closed || seq != d.seq {
			return
		}
		var r engine.DiagnosticsReport
		if err == nil {
			err = decode(res, &r)
		}
		if err != nil {
			d.summary.SetText("The counters cannot be read: " + errorText(err))
			return
		}
		d.show(r)
	})
}

// show draws a report.
func (d *diagnosticsTab) show(r engine.DiagnosticsReport) {
	d.report = r
	d.summary.SetText(diagnosticsSummary(r))
	d.showClusters(r)
	line := func(text string, c fyne.ThemeColorName, mono bool) fyne.CanvasObject {
		l := newTextLine(text, c, theme.SizeNameText, fyne.TextStyle{Monospace: mono})
		l.truncate = true
		return l
	}
	var reqs []fyne.CanvasObject
	for _, q := range r.RequestsByTarget {
		reqs = append(reqs, line(fmt.Sprintf("%6s  %s %s", formatCount(q.Count), q.Verb, q.Resource), theme.ColorNameForeground, true))
	}
	if len(reqs) == 0 {
		reqs = append(reqs, line(noneText(r, "No requests since connecting."), colorMuted, false))
	}
	var watches []fyne.CanvasObject
	for _, w := range r.Watches {
		color, text := theme.ColorNameForeground, fmt.Sprintf("%s · %s · %s objects · %s events", w.Scope, w.State, formatCount(int64(w.Objects)), formatCount(w.Events))
		switch {
		case w.Error != "":
			color, text = colorStatusError, text+" · "+w.Error
		case !w.Synced:
			color, text = colorStatusPending, text+" · listing"
		case w.State == engine.WatchCovered:
			color, text = colorMuted, fmt.Sprintf("%s · served by the %s watch · %s objects", w.Scope, w.CoveredBy, formatCount(int64(w.Objects)))
		case w.State == engine.WatchWarm:
			color = colorMuted
		}
		watches = append(watches, line(text, color, false))
	}
	if len(watches) == 0 {
		watches = append(watches, line(noneText(r, "No watches."), colorMuted, false))
	}
	var kinds []fyne.CanvasObject
	for _, k := range r.Kinds {
		kinds = append(kinds, line(fmt.Sprintf("%7s  %10s  %s", formatCount(int64(k.Objects)), memoryText(k.Bytes), k.Kind), theme.ColorNameForeground, true))
	}
	if len(kinds) == 0 {
		kinds = append(kinds, line(noneText(r, "No objects held."), colorMuted, false))
	}
	d.requests.Objects, d.watches.Objects, d.kinds.Objects = reqs, watches, kinds
	d.requests.Refresh()
	d.watches.Refresh()
	d.kinds.Refresh()
}

// showClusters draws a line per connected cluster, the shown one first, while more than one is
// connected; with one the summary says it all.
func (d *diagnosticsTab) showClusters(r engine.DiagnosticsReport) {
	var objs []fyne.CanvasObject
	if len(r.Clusters) > 1 {
		cs := slices.Clone(r.Clusters)
		slices.SortStableFunc(cs, func(a, b engine.DiagCluster) int { return boolOrder(b.Shown) - boolOrder(a.Shown) })
		objs = append(objs, newTextLine(fmt.Sprintf("%d clusters connected", len(cs)), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
		for _, c := range cs {
			l := newTextLine(diagClusterLine(d.sh.identity(c.Context).name, c), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
			l.truncate = true
			objs = append(objs, l)
		}
	}
	d.clusters.Objects = objs
	d.clusters.Refresh()
	if len(objs) == 0 {
		d.perCluster.Hide()
	} else {
		d.perCluster.Show()
	}
}

// boolOrder is 1 for true, 0 for false: an order with the true ones first.
func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// diagClusterLine is one connected cluster's line: its name, shown or in the background, its API
// requests since connecting, its watches, and the objects and memory its cache holds.
func diagClusterLine(name string, c engine.DiagCluster) string {
	where := "in the background"
	if c.Shown {
		where = "shown"
	}
	held := formatCount(int64(c.Objects)) + " objects"
	if c.Bytes > 0 {
		held += " (" + model.FormatBytes(c.Bytes) + ")"
	}
	return strings.Join([]string{name + " (" + where + ")", formatCount(c.Requests) + " API requests",
		fmt.Sprintf("%d open watches, %d warm", c.Open, c.Warm), held}, " · ")
}

// memoryText is a kind's estimated memory, empty when nothing was estimated.
func memoryText(b int64) string {
	if b <= 0 {
		return ""
	}
	return model.FormatBytes(b)
}

// discoveryText says where the connection's discovery answer came from; empty before it was read
// and for the demo cluster.
func discoveryText(r engine.DiagnosticsReport) string {
	d := r.Discovery
	switch {
	case d == nil || d.Source == "":
		return ""
	case d.Source == "disk":
		return "discovery from the disk cache"
	case d.Partial:
		return "partial discovery from the server"
	}
	return "discovery from the server"
}

// noneText is what an empty column says: that no cluster is connected, or text.
func noneText(r engine.DiagnosticsReport, text string) string {
	if !r.Connected {
		return notConnected + "."
	}
	return text
}

// diagnosticsSummary is the tab's first line.
func diagnosticsSummary(r engine.DiagnosticsReport) string {
	if !r.Connected {
		return fmt.Sprintf("%s · %s table snapshots built", notConnected, formatCount(int64(r.Snapshots)))
	}
	parts := []string{r.Context}
	if r.Demo {
		parts = append(parts, "the demo cluster: no API requests")
	} else {
		parts = append(parts, formatCount(r.Requests)+" API requests since connecting")
	}
	held := formatCount(int64(r.Objects)) + " objects held"
	if r.Bytes > 0 {
		held += " (" + model.FormatBytes(r.Bytes) + " estimated)"
	}
	parts = append(parts, fmt.Sprintf("%d open watches, %d warm, %d covered", r.Open, r.Warm, r.Covered), held)
	if d := discoveryText(r); d != "" {
		parts = append(parts, d)
	}
	parts = append(parts, formatCount(int64(r.Snapshots))+" table snapshots")
	return strings.Join(parts, " · ")
}

// diagnosticsChanged reads the counters again for an open Diagnostics tab (EventDiagnostics).
func (d *dock) diagnosticsChanged() {
	for _, c := range d.contents {
		if t, ok := c.editor.(*diagnosticsTab); ok && !c.closed {
			t.reload()
		}
	}
}

// diagnosticsMenuItem is Help › Diagnostics (diagnostics.open); nil when the build has no such
// command.
func (sh *shell) diagnosticsMenuItem() *fyne.MenuItem {
	c, ok := sh.eng.Registry().Get("diagnostics.open")
	if !ok {
		return nil
	}
	return fyne.NewMenuItem(c.Label, func() { sh.dispatch(c.ID, struct{}{}, nil) })
}
