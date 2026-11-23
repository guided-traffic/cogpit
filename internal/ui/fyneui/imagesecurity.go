package fyneui

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Image Security page's upper part (D-new): a line beside the page's title that counts the
// running images and those Trivy scanned; the severity band, one segment per severity as wide as
// its share of the CVE ids, its count on it (a click keeps that severity's rows); three cards in a
// row: Top images, Top CVEs and Top namespaces (a click on an entry narrows the table to it,
// through the page's search: ui.setFilter with the entry's token). The table below is the page's
// table, which the table view shows as it shows any view's; its heading switches between the CVEs
// and the findings (trivy.imageSecurityTable), and a click on a CVE shows its findings. Colours
// follow D-calm: red and amber for Critical and High and for the grades E and D, everything else
// grey (calmStatusColor, the calm grade badge). The data is the engine's (trivy.imageSecurity).

// imageSecNav is the page's entry under the navigator's heading Integrations: shown while the
// Trivy integration is on for the cluster and the cluster serves the reports.
var imageSecNav = func() navEntry {
	e := page(engine.ImageSecurity)
	e.needs = engine.TrivyReportsKind
	e.integration = engine.IntegrationTrivy
	return e
}()

// iconImageSecurity is Cogpit's own icon for the page, drawn for Cogpit: a shield's outline, the
// shape of the grades (D-113) it ranks the images by.
var iconImageSecurity = fyne.NewStaticResource("cogpit-image-security.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<path fill-rule="evenodd" d="M12 2 4 5v6.2c0 4.9 3.4 9.3 8 10.8 4.6-1.5 8-5.9 8-10.8V5l-8-3Zm0 2.2 6 2.25v4.75c0 3.9-2.6 7.4-6 8.7-3.4-1.3-6-4.8-6-8.7V6.45l6-2.25Z"/></svg>`))

func init() { navIcons[engine.ImageSecurity] = func() fyne.Resource { return iconImageSecurity } }

// The page's texts: its search's hint, its tables' switch.
const (
	imageSecSearchHint  = "CVE, package, image…"
	imageSecCVEsLabel   = "CVEs"
	imageSecFindingsLbl = "Findings"
)

// imageSecRowNoun names a table's rows in hints: "CVEs", "findings".
func imageSecRowNoun(view string) string {
	if view == engine.ImageSecurityFindings {
		return "findings"
	}
	return imageSecCVEsLabel
}

// The cards' columns, in points: a severity's word and count, a grade's shield, a list's counts.
const (
	isSevWidth    = 64
	isCountWidth  = 40
	isShieldWidth = 18
	isNumWidth    = 28
	isShieldSide  = 16
	isLegendSide  = 9 // a legend's square
)

// imageSecPanel shows the page's header line and cards.
type imageSecPanel struct {
	sh    *shell
	sub   *textLine // beside the page's title
	box   *fyne.Container
	root  fyne.CanvasObject
	data  *engine.ImageSecurityModel
	shown bool // data is drawn

	tables *imageSecTables // the table heading's switch, CVEs | Findings
	pdf    *textButton     // Export PDF, in the page's header

	requests atomic.Int32 // coalesced as the Workloads Overview's (overview.go)
}

func newImageSecPanel(sh *shell) *imageSecPanel {
	p := &imageSecPanel{sh: sh}
	p.sub = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	p.sub.truncate = true
	p.sub.baselineOf = theme.SizeNameSubHeadingText // on the title's line
	p.box = container.NewVBox()
	p.root = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, p.box)
	p.tables = newImageSecTables(sh)
	p.pdf = newPlateButton("Export PDF", theme.DocumentSaveIcon(), p.exportPDF)
	return p
}

// exportPDF renders the page as a PDF report (trivy.exportImageSecurityPdf: the namespaces and the
// search as the page has them, every CVE with its containers) and saves it where the user says.
func (p *imageSecPanel) exportPDF() {
	p.pdf.Disable() // one at a time: a large cluster takes a second
	p.sh.dispatch("trivy.exportImageSecurityPdf", struct{}{}, func(res any, err error) {
		p.pdf.Enable()
		if err != nil {
			p.sh.showError(err)
			return
		}
		var out struct {
			Filename string `json:"filename"`
			CVEs     int    `json:"cves"`
			PDF      []byte `json:"pdf"`
		}
		if derr := decode(res, &out); derr != nil {
			p.sh.toast("error", "Unexpected trivy.exportImageSecurityPdf result: "+derr.Error())
			return
		}
		p.sh.saveReport(out.Filename, out.CVEs, out.PDF)
	})
}

// imageSecTables is the switch in the table's heading: CVEs | Findings, the shown one chosen.
type imageSecTables struct {
	sh       *shell
	cves     *tipButton
	findings *tipButton
	seg      *segmented
	root     fyne.CanvasObject
}

func newImageSecTables(sh *shell) *imageSecTables {
	t := &imageSecTables{sh: sh}
	t.cves = newTipButton(sh, nil, "One row per CVE: how many images and containers it is found in, and which", func() { t.show("cves") })
	t.findings = newTipButton(sh, nil, "One row per CVE, workload and container", func() { t.show("findings") })
	t.cves.Text, t.findings.Text = imageSecCVEsLabel, imageSecFindingsLbl
	t.seg = newSegmented(sh, t.cves, t.findings)
	t.root = t.seg.root
	return t
}

// choose marks the table a view shows.
func (t *imageSecTables) choose(view string) {
	switch view {
	case engine.ImageSecurity:
		t.seg.choose(0)
	case engine.ImageSecurityFindings:
		t.seg.choose(1)
	}
}

// show switches the page's table, the search carried over.
func (t *imageSecTables) show(table string) {
	t.sh.dispatch("trivy.imageSecurityTable", imageSecTableParams{Table: table}, func(_ any, err error) { t.sh.showError(err) })
}

// imageSecTableParams are trivy.imageSecurityTable's parameters (engine/commands/trivy_security.go).
type imageSecTableParams struct {
	Table string `json:"table"`
	CVE   string `json:"cve,omitempty"`
}

// request reads the model off the main goroutine and shows it. It may be called from the main
// goroutine at any rate: requests made while one is in flight become one more.
func (p *imageSecPanel) request() {
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
	p.sh.dispatch("trivy.imageSecurity", struct{}{}, func(res any, err error) {
		again := p.requests.Swap(0) == 2
		var m engine.ImageSecurityModel
		if err == nil {
			err = decode(res, &m)
		}
		if err != nil {
			// The integration off or the reports not served: the page's table says why.
			m = engine.ImageSecurityModel{Error: err.Error()}
		}
		p.show(&m)
		if again && engine.IsImageSecurity(p.sh.ui.View) {
			p.request()
		}
	})
}

// show draws m: the header line and the cards; nothing while the page reads nothing.
func (p *imageSecPanel) show(m *engine.ImageSecurityModel) {
	if p.shown && reflect.DeepEqual(p.data, m) {
		p.sub.SetText(p.headLine(m)) // the newest scan's age moves on
		return
	}
	p.data, p.shown = m, true
	p.sub.SetText(p.headLine(m))
	if m.Error != "" && m.Reports == 0 {
		p.box.Objects = nil
		p.box.Refresh()
		return
	}
	p.box.Objects = []fyne.CanvasObject{p.band(m), newSpace(theme.SizeNamePadding),
		container.NewGridWithColumns(3, p.imagesCard(m), p.cvesCard(m), p.namespacesCard(m))}
	if m.Trend != nil {
		p.box.Objects = append(p.box.Objects, newSpace(theme.SizeNamePadding), p.trendCard(m.Trend))
	}
	p.box.Refresh()
}

// headLine is the line beside the title: "150 images running · 142 scanned · 8 not scanned ·
// newest scan 2h ago"; the running images only while the page reads the pods.
func (p *imageSecPanel) headLine(m *engine.ImageSecurityModel) string { return m.HeadLine(p.sh.now()) }

// isCard is one card: its title in the text colour, a muted line at its right, its rows.
func isCard(title, meta string, rows ...fyne.CanvasObject) fyne.CanvasObject {
	head := container.NewBorder(nil, nil,
		newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}),
		newTextLine(meta, colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	plate := newSurface(theme.ColorNameInputBackground)
	plate.radius = theme.SizeNameSelectionRadius
	body := append([]fyne.CanvasObject{head}, rows...)
	return container.NewStack(plate, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewVBox(body...)))
}

// noneLine is a card's line while it has nothing to list.
func noneLine() fyne.CanvasObject {
	return newTextLine("None", colorMuted, theme.SizeNameText, fyne.TextStyle{})
}

// severityWords are the severities as the engine writes them, in the severity bar's order.
var severityWords = [5]string{"Critical", "High", "Medium", "Low", "Unknown"}

// band is the severity band: the drawer's severity bar (newSeverityBar, its colours and its
// shares) over the distinct CVE ids by severity, as wide as the page; under it a legend and "188
// CVEs · 119 fixable". A click on a segment keeps that severity's rows (the search's sev tokens
// replaced).
func (p *imageSecPanel) band(m *engine.ImageSecurityModel) fyne.CanvasObject {
	var counts model.VulnerabilityCounts
	for _, s := range m.Severities {
		n := int64(s.Count)
		switch s.Severity {
		case "Critical":
			counts.Critical = n
		case "High":
			counts.High = n
		case "Medium":
			counts.Medium = n
		case "Low":
			counts.Low = n
		default:
			counts.Unknown = n
		}
	}
	bar := newSeverityBar(counts)
	bar.onTap = func(i int) { p.narrowSeverity(severityWords[i]) }
	legend := make([]fyne.CanvasObject, 0, 3*len(severityWords))
	for i, w := range severityWords {
		legend = append(legend, newLegendSquare(severityFills[i]), newTextLine(w, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
			newSpace(theme.SizeNamePadding))
	}
	totals := newTextLine(formatCount(int64(m.CVEs))+" CVEs · "+formatCount(int64(m.Fixable))+" fixable", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	return container.NewVBox(bar, container.NewBorder(nil, nil, container.NewHBox(legend...), totals))
}

// narrowSeverity keeps one severity's rows: the search's severity tokens replaced by its.
func (p *imageSecPanel) narrowSeverity(sev string) {
	filter := engine.ImageSecurityFilterSeverity(p.sh.ui.Filter, sev)
	p.sh.dispatch("ui.setFilter", filterParams{Filter: filter}, func(_ any, err error) { p.sh.showError(err) })
}

// newLegendSquare is a legend's square in a colour, centred in its line.
func newLegendSquare(fill fyne.ThemeColorName) fyne.CanvasObject {
	sq := newSurface(fill)
	return container.NewCenter(box(fixedSize{fyne.NewSquareSize(isLegendSide)}, sq))
}

// fixedSize lays its one object out at a fixed size.
type fixedSize struct{ size fyne.Size }

func (l fixedSize) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(l.size)
	}
}

func (l fixedSize) MinSize([]fyne.CanvasObject) fyne.Size { return l.size }

// trendHeight is the findings curve's height, in points.
const trendHeight = 96

// trendCard is the findings curve from Prometheus: a stacked bar per point, the severities' colours
// as the drawer's severity bar has them; its title, the source and the range's choice above, the largest total at
// the left, the first and the last time below; a note in place of the bars while it has none.
func (p *imageSecPanel) trendCard(t *engine.ImageSecurityTrend) fyne.CanvasObject {
	rng := newSelectField(trendRangeLabels(), func(label string) {
		days, _ := strconv.Atoi(strings.TrimSuffix(label, " days"))
		p.sh.dispatch("trivy.setTrendRange", struct {
			Days int `json:"days"`
		}{days}, func(_ any, err error) { p.sh.showError(err) })
	})
	rng.Selected = strconv.Itoa(t.Days) + " days"
	meta := "a CVE counts once per image"
	if t.Source != "" {
		meta = t.Source + " · " + meta
	}
	head := container.NewBorder(nil, nil,
		container.NewHBox(newTextLine("Findings over time", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}),
			newTextLine(meta, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})), rng)
	var body fyne.CanvasObject
	if len(t.Points) == 0 {
		note := t.Note
		if note == "" {
			note = "No points in this range."
		}
		body = newTextLine(note, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	} else {
		body = p.trendBars(t)
	}
	rows := []fyne.CanvasObject{head, body}
	if len(t.Points) > 0 && t.Note != "" {
		rows = append(rows, newTextLine(t.Note, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	plate := newSurface(theme.ColorNameInputBackground)
	plate.radius = theme.SizeNameSelectionRadius
	return container.NewStack(plate, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewVBox(rows...)))
}

// trendRangeLabels are the curve's ranges as its choice writes them.
func trendRangeLabels() []string {
	out := make([]string, 0, len(engine.ImageSecurityTrendDays))
	for _, d := range engine.ImageSecurityTrendDays {
		out = append(out, strconv.Itoa(d)+" days")
	}
	return out
}

// trendBars draws the points as stacked bars, Critical at the bottom, with the largest total at the
// left and the first and last time under the bars.
func (p *imageSecPanel) trendBars(t *engine.ImageSecurityTrend) fyne.CanvasObject {
	lay := &trendLayout{}
	objs := make([]fyne.CanvasObject, 0, 5*len(t.Points))
	for _, pt := range t.Points {
		counts := [5]int{pt.Critical, pt.High, pt.Medium, pt.Low, pt.Unknown}
		lay.points = append(lay.points, counts)
		lay.peak = max(lay.peak, pt.Critical+pt.High+pt.Medium+pt.Low+pt.Unknown)
		for _, fill := range severityFills {
			objs = append(objs, newSurface(fill))
		}
	}
	bars := container.New(lay, objs...)
	tf := p.sh.timeFormat()
	first, last := t.Points[0].At, t.Points[len(t.Points)-1].At
	axis := container.NewBorder(nil, nil, newTextLine(tf.FormatDate(first), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
		newTextLine(tf.FormatDate(last), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	peak := newTextLine(formatCount(int64(lay.peak)), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	return container.NewBorder(nil, axis, container.NewVBox(peak), nil, bars)
}

// trendLayout lays the stacked bars out: one column per point, a gap between, each severity's
// rectangle as tall as its share of the peak.
type trendLayout struct {
	points [][5]int
	peak   int
}

func (l *trendLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	n := len(l.points)
	if n == 0 || l.peak == 0 {
		return
	}
	gap := float32(2)
	if size.Width/float32(n) < 6 {
		gap = 1
	}
	w := max((size.Width-gap*float32(n-1))/float32(n), 1)
	for i, counts := range l.points {
		x := float32(i) * (w + gap)
		y := size.Height
		for j, c := range counts {
			h := size.Height * float32(c) / float32(l.peak)
			o := objs[i*5+j]
			y -= h
			o.Move(fyne.NewPos(x, y))
			o.Resize(fyne.NewSize(w, h))
		}
	}
}

func (l *trendLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, trendHeight) }

// imagesCard is Top images: the grade's shield, the image, its Critical and High.
func (p *imageSecPanel) imagesCard(m *engine.ImageSecurityModel) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(m.TopImages))
	for _, im := range m.TopImages {
		name := newTextLine(im.Image, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true})
		name.truncate = true
		rows = append(rows, p.entry(im.Token, isShield(im.Grade), name, isNum(im.Critical), isNum(im.High)))
	}
	if len(rows) == 0 {
		rows = append(rows, noneLine())
	}
	return isCard("Top images", "Critical · High", rows...)
}

// cvesCard is Top CVEs: the severity's word, the id, the images it is found in (by digest) and the
// containers that run them.
func (p *imageSecPanel) cvesCard(m *engine.ImageSecurityModel) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(m.TopCVEs))
	for _, c := range m.TopCVEs {
		word := newTextLine(c.Severity, calmStatusColor(c.Status), theme.SizeNameText, fyne.TextStyle{Bold: c.Status != model.StatusNone})
		id := newTextLine(c.ID, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		id.truncate = true
		rows = append(rows, p.entryOf([]float32{isSevWidth, 0, isNumWidth, isCountWidth}, c.Token, word, id,
			isNum(int64(c.Images)), isNum(int64(c.Containers))))
	}
	if len(rows) == 0 {
		rows = append(rows, noneLine())
	}
	return isCard("Top CVEs", "Images · Containers", rows...)
}

// namespacesCard is Top namespaces: the worst grade's shield, the namespace, its Critical and High.
func (p *imageSecPanel) namespacesCard(m *engine.ImageSecurityModel) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(m.TopNamespaces))
	for _, n := range m.TopNamespaces {
		name := newTextLine(n.Namespace, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		name.truncate = true
		rows = append(rows, p.entry(n.Token, isShield(n.Grade), name, isNum(int64(n.Critical)), isNum(int64(n.High))))
	}
	if len(rows) == 0 {
		rows = append(rows, noneLine())
	}
	return isCard("Top namespaces", "Critical · High", rows...)
}

// entry is a shield's line of a card: the shield, the name, two counts.
func (p *imageSecPanel) entry(token string, objs ...fyne.CanvasObject) fyne.CanvasObject {
	return p.entryOf([]float32{isShieldWidth, 0, isNumWidth, isNumWidth}, token, objs...)
}

// entryOf is a card's line in columns of the widths (0: the rest) that a click narrows the
// table to.
func (p *imageSecPanel) entryOf(widths []float32, token string, objs ...fyne.CanvasObject) fyne.CanvasObject {
	return newTapTarget(box(&isRowLayout{widths: widths}, objs...), func() { p.narrow(token) })
}

// narrow puts a card entry's token into the page's search, in place of one of its key.
func (p *imageSecPanel) narrow(token string) {
	filter := engine.ImageSecurityFilterWith(p.sh.ui.Filter, token)
	p.sh.dispatch("ui.setFilter", filterParams{Filter: filter}, func(_ any, err error) { p.sh.showError(err) })
}

// isShield is a calm grade shield, centred in its column.
func isShield(g model.ImageGrade) fyne.CanvasObject {
	return container.NewCenter(newCalmGradeBadge(isShieldSide, g))
}

// isNum is a count of a card's list: the text colour, right-aligned.
func isNum(n int64) fyne.CanvasObject {
	t := newTextLine(formatCount(n), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	t.align = fyne.TextAlignTrailing
	return t
}

// isRowLayout lays a card's line out in columns: each of its width, the one of width 0 taking
// what is left; a gap of the theme's padding between them.
type isRowLayout struct{ widths []float32 }

func (l *isRowLayout) gap() float32 { return theme.Padding() * 2 }

func (l *isRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	fixed := l.gap() * float32(max(len(objs)-1, 0))
	for i := range objs {
		if i < len(l.widths) {
			fixed += l.widths[i]
		}
	}
	x := float32(0)
	for i, o := range objs {
		w := float32(0)
		if i < len(l.widths) {
			w = l.widths[i]
		}
		if w == 0 {
			w = max(size.Width-fixed, 0)
		}
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + l.gap()
	}
}

func (l *isRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for i, o := range objs {
		ms := o.MinSize()
		h = max(h, ms.Height)
		if i < len(l.widths) && l.widths[i] > 0 {
			w += l.widths[i]
		}
	}
	return fyne.NewSize(w+l.gap()*float32(max(len(objs)-1, 0)), h)
}

// pageHeading is the header's left part on a page: the title, and on Image Security the line
// beside it.
func (tv *tableView) pageHeading(view string) []fyne.CanvasObject {
	if engine.IsImageSecurity(view) {
		return []fyne.CanvasObject{tv.title, tv.imageSec.sub}
	}
	return []fyne.CanvasObject{tv.title}
}

// tableHeading is a page's table's heading: its title, on Image Security the switch between its
// tables.
func (tv *tableView) tableHeading(view string) fyne.CanvasObject {
	if engine.IsImageSecurity(view) {
		return tv.imageSec.tables.root
	}
	return tv.eventsTitle
}

// pageTools are a page's header tools at the right: the namespace filter, which applies to the
// whole page; on Image Security its Export PDF before it.
func (tv *tableView) pageTools(view string) []fyne.CanvasObject {
	if engine.IsImageSecurity(view) {
		return []fyne.CanvasObject{tv.imageSec.pdf, tv.nsBox}
	}
	return []fyne.CanvasObject{tv.nsBox}
}

// rowsAreObjects reports whether the shown table's rows are objects, with a checkbox, a menu and a
// drawer: all but Image Security's CVEs.
func (tv *tableView) rowsAreObjects() bool {
	return tv.snap == nil || tv.snap.View != engine.ImageSecurity
}

// imageSecCVETapped shows a CVE's findings: the findings table with its cve: token.
func (tv *tableView) imageSecCVETapped(row int) {
	if tv.snap == nil || tv.snap.View != engine.ImageSecurity || row < 0 || row >= len(tv.snap.Rows) {
		return
	}
	cve := tv.snap.Rows[row].Ref.Name
	tv.sh.dispatch("trivy.imageSecurityTable", imageSecTableParams{Table: "findings", CVE: cve}, func(_ any, err error) { tv.sh.showError(err) })
}

// searchHint is the search field's hint: "Search pods…", on Image Security what its tokens take.
func (tv *tableView) searchHint() string {
	if engine.IsImageSecurity(tv.sh.ui.View) {
		return imageSecSearchHint
	}
	return "Search " + tv.rowNoun() + "…"
}

// imageSecTone is a findings cell's colour on the page (D-calm), "" for the text colour: the
// Severity cell Critical's and High's colours and grey for the others, the installed version grey.
// No cell of the page is a pill.
func imageSecTone(col *model.Column, cell model.Cell) fyne.ThemeColorName {
	switch col.ID {
	case "severity":
		return calmStatusColor(cell.Status)
	case "installed":
		return colorMuted
	}
	return ""
}

// imageSecRowTapped opens the drawer of a finding's workload at its container's section (D-150's
// openAt): a pod's drawer has one per container; a workload's that has none opens at its top.
func (tv *tableView) imageSecRowTapped(row int) {
	if tv.snap == nil || tv.snap.View != engine.ImageSecurityFindings || row < 0 || row >= len(tv.snap.Rows) {
		return
	}
	ci := slices.IndexFunc(tv.snap.Columns, func(c model.Column) bool { return c.ID == "container" })
	r := tv.snap.Rows[row]
	if ci < 0 || ci >= len(r.Cells) {
		return
	}
	tv.sh.drawer.openAt(r.Ref, r.Cells[ci].Text)
}

// integrationsChanged rebuilds the navigator when an integration is switched for the cluster
// shown: an entry may show only while one is on (navEntry.integration, imageSecNav).
func (s *sidebar) integrationsChanged(st, old engine.UIState) {
	if st.Context == "" || st.Context != old.Context {
		return // a cluster shown anew loads its kinds, which rebuilds the tree
	}
	for _, in := range engine.Integrations {
		if st.IntegrationOn(st.Context, in.Name) != old.IntegrationOn(st.Context, in.Name) {
			s.setKinds(s.sh.kinds)
			s.selectView(st.View)
			return
		}
	}
}
