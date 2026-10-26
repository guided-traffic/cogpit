package fyneui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// The Connection Diagnostics dock tab (D-13 cache transparency, D-133; engine/diagnostics_view.go):
// what the status bar's button opens (the owner, 2026-10-26: "Eigener Dock-Tab, die Connection
// Infos aus der Status Leiste ziehen ebenfalls dahin um … Im Diagnosics-tab können wir dann richtig
// aufdrehen mit Status-Informationen und Details. Ein Log welches Timeouts und andere
// Verbindungsprobleme aufschreibt und vorhält."), read with diagnostics.get whenever the counters
// or the log move (EventDiagnostics, at most once a second). Its first line sums the cluster's
// requests, watches, objects and memory, says where discovery came from (D-65) and counts the table
// snapshots built since start (the only place the GUI shows them since the status bar dropped them,
// owner 2026-10-14); with several clusters connected (D-69) a line per connected cluster follows it.
// Then the connection (the server, its version, the latest connect), the problems since that
// connect, how the reads' initial lists came, and Cogpit's rate limits with what waited at them;
// then four columns: the diagnostics log (the line seen last first), the API requests by verb and
// resource, the watches with their load times, and the objects and memory per kind.
//
// Overview and Report switch what the tab shows: Report is the anonymised report exactly as Copy and
// Save Report… put it out (diagnostics.report: the endpoints replaced, the owner's choice). Its
// layout and wording are Cogpit's (Lens has no such view).

// diagnosticsTab is the tab's content; it is the dock content's editor (dockEditor), so the dock
// closes it.
type diagnosticsTab struct {
	sh *shell

	root       *fyne.Container
	summary    *textLine
	clusters   *fyne.Container   // a line per connected cluster while more than one is (D-69)
	perCluster fyne.CanvasObject // clusters with its inset; hidden while it has no line, so the layout stays
	connection *selLine          // the server, its version, the latest connect
	problems   *textLine         // the problems since the latest connect and how the initial lists came
	budgets    *textLine         // Cogpit's rate limits and what waited at them
	log        *fyne.Container
	requests   *fyne.Container
	watches    *fyne.Container
	kinds      *fyne.Container

	mode             *twoWay // Overview or Report
	copyBtn, saveBtn *tipButton
	overview         fyne.CanvasObject
	reportBox        fyne.CanvasObject
	reportNote       *textLine
	reportView       *yamlview.View
	showReport       bool
	shown            engine.DiagnosticsReportText // the report Report shows; empty before

	report engine.DiagnosticsReport
	logKey string // the log last drawn: an unchanged log is not drawn again
	seq    int
	rseq   int
	closed bool
}

var _ dockEditor = (*diagnosticsTab)(nil)

func newDiagnosticsTab(sh *shell) *diagnosticsTab {
	d := &diagnosticsTab{sh: sh, log: container.NewVBox(), requests: container.NewVBox(), watches: container.NewVBox(),
		kinds: container.NewVBox(), clusters: container.NewVBox()}
	d.summary = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	d.summary.truncate = true
	d.connection = newSel(newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{}), d.copy)
	d.connection.line.truncate = true
	d.problems = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	d.problems.truncate = true
	d.budgets = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	d.budgets.truncate = true
	d.mode = newTwoWay(sh, "Overview", "Report", d.choose)
	d.copyBtn = newTipButton(sh, theme.ContentCopyIcon(), "Copy the report: the endpoints replaced", d.copyReport)
	d.saveBtn = newTipButton(sh, theme.DocumentSaveIcon(), "Save Report…: the endpoints replaced", d.saveReport)
	column := func(title string, body *fyne.Container) fyne.CanvasObject {
		return container.NewBorder(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, heading(title)), nil, nil, nil,
			newVScroll(inset(theme.SizeNameInnerPadding, "", body)))
	}
	cols := container.NewGridWithColumns(3,
		column("API requests by verb and resource", d.requests),
		column("Watches", d.watches),
		column("Objects and memory per kind", d.kinds))
	split := container.NewHSplit(column("Log, the line seen last first", d.log), cols)
	split.Offset = 0.45
	status := inset(theme.SizeNameInnerPadding, "", container.NewVBox(d.problems, d.budgets))
	d.overview = container.NewBorder(status, nil, nil, nil, split)
	d.reportNote = newTextLine(reportNoteText, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	d.reportNote.truncate = true
	d.reportView = yamlview.New()
	d.reportView.SetReadOnly(true)
	d.reportView.SetHighlighting(false)
	d.reportView.SetLineNumbers(false)
	d.reportBox = container.NewBorder(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, d.reportNote), nil, nil, nil, d.reportView)
	d.reportBox.Hide()
	d.perCluster = inset(theme.SizeNameInnerPadding, "", d.clusters)
	d.perCluster.Hide()
	// Overview and Report, Copy and Save beside the connection's line: the first line keeps the
	// tab's width, which its sums need.
	actions := container.NewHBox(d.mode.root, d.copyBtn, d.saveBtn)
	conn := inset(theme.SizeNameInnerPadding, "", container.NewBorder(nil, nil, nil, actions, d.connection))
	d.root = container.NewBorder(container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, d.summary),
		d.perCluster, conn, newDivider(false)),
		nil, nil, nil, container.NewStack(d.overview, d.reportBox))
	d.reload()
	return d
}

// reportNoteText heads the Report view.
const reportNoteText = "The report as Copy and Save Report… put it out: hosts, addresses, ports, the servers of URLs " +
	"and file paths replaced; names of users, namespaces, objects and API groups kept."

// remote implements dockEditor; the tab has no text.
func (d *diagnosticsTab) remote(uint64) {}

// close implements dockEditor: answers that come later are dropped.
func (d *diagnosticsTab) close() { d.closed = true }

// copy puts a selected text on the clipboard.
func (d *diagnosticsTab) copy(text string) { d.sh.copyText(text, false) }

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

// choose switches between Overview (0) and Report (1); Report reads the report anew.
func (d *diagnosticsTab) choose(i int) {
	d.mode.choose(i)
	d.showReport = i == 1
	if d.showReport {
		d.overview.Hide()
		d.reportBox.Show()
		d.readReport(func(engine.DiagnosticsReportText) {})
		return
	}
	d.reportBox.Hide()
	d.overview.Show()
}

// readReport reads the anonymised report (diagnostics.report), shows it when Report is chosen and
// hands it to done; a report that cannot be read is said in a toast.
func (d *diagnosticsTab) readReport(done func(engine.DiagnosticsReportText)) {
	d.rseq++
	seq := d.rseq
	d.sh.dispatch("diagnostics.report", struct{}{}, func(res any, err error) {
		if d.closed || seq != d.rseq {
			return
		}
		var r engine.DiagnosticsReportText
		if err == nil {
			err = decode(res, &r)
		}
		if err != nil {
			d.sh.toast(engine.LevelError, "The report cannot be written: "+errorText(err))
			return
		}
		d.shown = r
		if d.showReport {
			d.reportView.SetText(r.Text)
		}
		done(r)
	})
}

// withReport hands done the report to put out: the one Report shows, exactly, else a new one.
func (d *diagnosticsTab) withReport(done func(engine.DiagnosticsReportText)) {
	if d.showReport && d.shown.Text != "" {
		done(d.shown)
		return
	}
	d.readReport(done)
}

// copyReport copies the anonymised report.
func (d *diagnosticsTab) copyReport() {
	d.withReport(func(r engine.DiagnosticsReportText) {
		d.sh.copyText(r.Text, false)
		d.sh.toast(engine.LevelInfo, "Report copied: the endpoints replaced")
	})
}

// saveReport saves the anonymised report into the file the user picks in the system's save panel,
// or Fyne's save dialog where the system has none Cogpit uses.
func (d *diagnosticsTab) saveReport() {
	d.withReport(func(r engine.DiagnosticsReportText) {
		saved := func(path string, err error) {
			if err != nil {
				d.sh.toast(engine.LevelError, "Report not saved: "+err.Error())
				return
			}
			d.sh.toast(engine.LevelInfo, "Report saved to "+path)
		}
		if d.sh.files.save(nativedialog.SaveOptions{Title: "Save Report", Name: r.Name, Types: []string{"txt"}}, func(path string) {
			d.sh.writePicked(path, []byte(r.Text), func(err error) { saved(path, err) })
		}) {
			return
		}
		dlg := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err == nil && w == nil {
				return // cancelled
			}
			if err == nil {
				err = writeAndClose(w, []byte(r.Text))
			}
			path := ""
			if w != nil {
				path = filepath.FromSlash(w.URI().Path())
			}
			saved(path, err)
		}, d.sh.win)
		dlg.SetFileName(r.Name)
		d.sh.showDialog(dlg)
	})
}

// show draws a report.
func (d *diagnosticsTab) show(r engine.DiagnosticsReport) {
	d.report = r
	d.summary.SetText(diagnosticsSummary(r))
	d.showClusters(r)
	d.showConnection(r)
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
		if w.LoadedIn > 0 {
			text += " · loaded in " + engine.FormatSpan(w.LoadedIn) + engine.InitialText(w.Initial)
		}
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
	d.showLog(r)
}

// showConnection draws the status lines: the connection, the problems since the latest connect with
// how the initial lists came, and Cogpit's rate limits; and enables Copy and Save for a cluster
// that has a connection to report on.
func (d *diagnosticsTab) showConnection(r engine.DiagnosticsReport) {
	reportable := r.Connected && r.Server != ""
	for _, b := range []*tipButton{d.copyBtn, d.saveBtn} {
		switch {
		case reportable:
			b.Enable()
		default:
			b.Disable()
		}
	}
	why := "No cluster connected"
	if r.Demo {
		why = "The demo cluster has no connection to report on"
	}
	if reportable {
		d.copyBtn.setTip("Copy the report: the endpoints replaced")
		d.saveBtn.setTip("Save Report…: the endpoints replaced")
	} else {
		d.copyBtn.setTip(why)
		d.saveBtn.setTip(why)
	}
	if !reportable {
		text := notConnected + "."
		switch {
		case r.Demo:
			text = "The demo cluster: no connection, no rate limits, no log."
		case r.Connected:
			text = "" // a connection whose report has not said its server yet
		}
		setSel(d.connection, text)
		d.problems.Hide()
		d.budgets.Hide()
		return
	}
	d.problems.Show()
	d.budgets.Show()
	conn := "Server " + r.Server
	if r.ServerVersion != "" {
		conn += " · Kubernetes " + r.ServerVersion
	}
	if !r.Since.IsZero() {
		conn += " · latest connect " + d.sh.timeFormat().Format(r.Since)
	}
	setSel(d.connection, conn)
	text, c := problemsText(r)
	d.problems.SetText(text)
	d.problems.SetColor(c)
	text, c = budgetsText(r)
	d.budgets.SetText(text)
	d.budgets.SetColor(c)
}

// problemsText is the line of the problems since the latest connect and of how the open and warm
// watches' initial lists came, in the warning colour while there are problems.
func problemsText(r engine.DiagnosticsReport) (string, fyne.ThemeColorName) {
	parts := make([]string, 0, len(r.ProblemCounts))
	for _, p := range r.ProblemCounts {
		parts = append(parts, engine.DiagKindLabel(p.Name)+" "+formatCount(p.Count))
	}
	text, c := "No problem since the latest connect", colorMuted
	if len(parts) > 0 {
		text, c = "Since the latest connect: "+strings.Join(parts, " · "), colorStatusWarning
	}
	s := r.Streaming
	lists := fmt.Sprintf("initial lists: %d streamed, %d listed", s.Streamed, s.Listed)
	if s.FellBack > 0 {
		lists += fmt.Sprintf(", %d listed after the server did not stream them", s.FellBack)
	}
	return text + " · " + lists, c
}

// budgetsText is the line of Cogpit's rate limits: each with its rate, and what waited at it; in the
// warning colour while a request waits or one waited a second or more (engine.BudgetPressed).
func budgetsText(r engine.DiagnosticsReport) (string, fyne.ThemeColorName) {
	c := colorMuted
	parts := make([]string, 0, len(r.Budgets))
	for _, b := range r.Budgets {
		p := fmt.Sprintf("%s %s/s (burst %d): ", b.Name, strconv.FormatFloat(float64(b.QPS), 'f', -1, 32), b.Burst)
		if b.Delayed == 0 {
			p += "none waited"
		} else {
			p += fmt.Sprintf("%s of %s waited %s, the longest %s", formatCount(b.Delayed), formatCount(b.Requests),
				engine.FormatSpan(b.Waited), engine.FormatSpan(b.Longest))
		}
		if b.Waiting > 0 {
			p += fmt.Sprintf(", %s waiting now", formatCount(b.Waiting))
		}
		if engine.BudgetPressed(b) {
			c = colorStatusWarning
		}
		parts = append(parts, p)
	}
	return "Cogpit's rate limits (watches never wait): " + strings.Join(parts, " · "), c
}

// showLog draws the diagnostics log, the line seen last first, when it changed: each line in its
// kind's colour and selectable.
func (d *diagnosticsTab) showLog(r engine.DiagnosticsReport) {
	var key strings.Builder
	for _, e := range r.Log {
		fmt.Fprintf(&key, "%s|%s|%d|%d;", e.Kind, e.What, e.Count, e.Last.UnixNano())
	}
	if !r.Connected {
		key.WriteString("not connected")
	}
	if key.String() == d.logKey && len(d.log.Objects) > 0 {
		return
	}
	d.logKey = key.String()
	tf := d.sh.timeFormat()
	var objs []fyne.CanvasObject
	for _, e := range r.Log {
		l := newSel(newTextLine(engine.DiagEntryText(e, tf.FormatTime), severityColor(engine.DiagSeverity(e.Kind)), theme.SizeNameText, fyne.TextStyle{}), d.copy)
		l.line.truncate = true
		objs = append(objs, l)
	}
	if len(objs) == 0 {
		none := newTextLine(noneText(r, "Nothing recorded."), colorMuted, theme.SizeNameText, fyne.TextStyle{})
		objs = append(objs, none)
	}
	d.log.Objects = objs
	d.log.Refresh()
}

// severityColor is a log line's colour by its severity (engine.DiagSeverity).
func severityColor(severity string) fyne.ThemeColorName {
	switch severity {
	case engine.SeverityError:
		return colorStatusError
	case engine.SeverityInfo:
		return colorMuted
	}
	return colorStatusWarning
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
