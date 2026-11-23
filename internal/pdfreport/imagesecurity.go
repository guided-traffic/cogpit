package pdfreport

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/signintech/gopdf"
)

// ImageSecurity is the Image Security page's report: the page's header and cards as the user saw
// them, and every CVE of the current search with the containers it runs in. The engine fills it;
// every text is final (formatted, sorted), Render only lays it out.
type ImageSecurity struct {
	// Cluster is the context's name.
	Cluster string
	// Generated is when the report was made; TimeLayout is the Go layout for absolute times
	// ("2006-01-02 15:04" when empty).
	Generated  time.Time
	TimeLayout string
	// Namespaces is the namespace filter as the page names it: "All namespaces", "shop, billing".
	Namespaces string
	// Search is the search text; "" none.
	Search string
	// Header is the page's header line: "93 images running · 81 scanned · …".
	Header string
	// Severities are the severity band's segments, Critical to Low, then Unknown when it counts;
	// each CVE counts once, under its worst severity.
	Severities []SeverityCount
	// CVEsTotal counts the distinct CVEs, Fixable those with a fixed version.
	CVEsTotal, Fixable int
	// TopImages, TopCVEs and TopNamespaces are the three cards' rows (up to five each), the
	// *Columns their header cells.
	TopImages, TopCVEs, TopNamespaces                      []TopRow
	TopImagesColumns, TopCVEsColumns, TopNamespacesColumns []string
	// Trend is the findings curve; nil: no curve section (no Prometheus).
	Trend *Trend
	// CVEs is every CVE row of the current search, in the table's order.
	CVEs []CVE
}

// Level is how urgent a value is, which picks its colour (D-180): Critical red, High amber, the
// rest grey.
type Level int

// The levels; LevelNone draws in the text colour.
const (
	LevelNone Level = iota
	LevelLow
	LevelMedium
	LevelHigh
	LevelCritical
	LevelUnknown
)

// SeverityCount is one segment of the severity band.
type SeverityCount struct {
	Name  string
	Count int
	Level Level
}

// TopRow is one row of a top card. Level colours the cell at LevelCell (a grade, a severity).
type TopRow struct {
	Cells     []string
	Level     Level
	LevelCell int
}

// Trend is the findings curve over time.
type Trend struct {
	// Title heads the section: "Findings over time (Prometheus, 30 days)".
	Title string
	// Note qualifies the curve ("counts a CVE once per image") or says why it is empty.
	Note string
	// Points are the samples, oldest first.
	Points []TrendPoint
}

// TrendPoint is one sample of the curve.
type TrendPoint struct {
	At                                   time.Time
	Critical, High, Medium, Low, Unknown int
}

// CVE is one row of the findings table and the containers it runs in.
type CVE struct {
	ID, Severity string
	Level        Level
	// Score is the CVSS score as shown: "9.4", "—".
	Score string
	// Packages names the affected packages: "libssl3, openssl".
	Packages string
	// Fixed is the fixed version; "" no fix.
	Fixed string
	// Images and Containers count where it runs.
	Images, Containers int
	// RunsIn is every container it runs in, in the order to print.
	RunsIn []Place
}

// Place is one container a CVE runs in, with the package's installed and fixed version there.
type Place struct {
	Namespace, Workload, Container, Image, Installed, Fixed string
}

// levelText is a level's text colour.
func levelText(l Level) rgb {
	switch l {
	case LevelCritical:
		return colRed
	case LevelHigh:
		return colAmberText
	case LevelMedium, LevelLow, LevelUnknown:
		return colMuted
	}
	return colText
}

// levelFill is a level's fill in the band and the curve, and the colour its label takes on it: the
// severity bar's colours of the window (a pod's drawer, the Image Security page).
func levelFill(l Level) (fill, label rgb) {
	switch l {
	case LevelCritical:
		return colSevCritical, colWhite
	case LevelHigh:
		return colSevHigh, colWhite
	case LevelMedium:
		return colSevMedium, colSevInkDark
	case LevelLow:
		return colSevLow, colSevInkDark
	}
	return colSevUnknown, colText
}

// Render lays the report out on A4 landscape pages and returns the PDF.
func Render(r ImageSecurity) ([]byte, error) {
	if r.TimeLayout == "" {
		r.TimeLayout = "2006-01-02 15:04"
	}
	title := "Image Security"
	if r.Cluster != "" {
		title += " · " + r.Cluster
	}
	w, err := newWriter(gopdf.PdfInfo{Title: title, Creator: "Cogpit", Producer: "Cogpit", CreationDate: r.Generated})
	if err != nil {
		return nil, err
	}
	w.newPage()
	r.drawHeading(w)
	r.drawBand(w)
	r.drawCards(w)
	if r.Trend != nil {
		r.drawTrend(w)
	}
	r.drawCVEs(w)
	if err := w.footers("Cogpit · " + title); err != nil {
		return nil, err
	}
	return w.bytes()
}

// drawHeading draws the title, the line of what the report covers and the page's header line.
func (r *ImageSecurity) drawHeading(w *writer) {
	x := margin
	tw := w.text("Image Security", x, w.y, contentW(), 9, bold, 18, colText, left)
	if r.Cluster != "" {
		w.text(r.Cluster, x+tw+4, w.y, contentW()-tw-4, 9, regular, 18, colMuted, left)
	}
	w.y += 10
	parts := []string{"Generated " + r.Generated.Format(r.TimeLayout)}
	if r.Namespaces != "" {
		parts = append(parts, r.Namespaces)
	}
	if r.Search != "" {
		parts = append(parts, "Search “"+r.Search+"”")
	} else {
		parts = append(parts, "No search")
	}
	w.text(strings.Join(parts, " · "), x, w.y, contentW(), 5, regular, 9, colMuted, left)
	w.y += 5
	if r.Header != "" {
		w.text(r.Header, x, w.y, contentW(), 5, regular, 9, colSecondary, left)
		w.y += 5
	}
	w.y += 4
}

// section draws a section's title and, right after it, a muted aside.
func section(w *writer, x, maxW float64, title, aside string) {
	tw := w.text(title, x, w.y, maxW, 6, bold, 11, colText, left)
	if aside != "" {
		w.text(aside, x+tw+3, w.y+0.4, maxW-tw-3, 6, regular, 8.5, colMuted, left)
	}
	w.y += 7
}

// drawBand draws the severity band: one stacked bar over the full width, each segment as wide as
// its share and labelled with its count, a legend and the totals under it.
func (r *ImageSecurity) drawBand(w *writer) {
	section(w, margin, contentW(), "Vulnerabilities", "")
	const barH = 9.0
	total := 0
	for _, s := range r.Severities {
		total += max(s.Count, 0)
	}
	x, y, bw := margin, w.y, contentW()
	if total == 0 {
		w.fill(x, y, bw, barH, colRule)
		w.text("No CVEs", x, y, bw, barH, regular, 8.5, colMuted, center)
	} else {
		cx := x
		for _, s := range r.Severities {
			if s.Count <= 0 {
				continue
			}
			sw := bw * float64(s.Count) / float64(total)
			fill, label := levelFill(s.Level)
			w.fill(cx, y, sw, barH, fill)
			n := strconv.Itoa(s.Count)
			if w.textWidth(n, bold, 8.5)+3 <= sw {
				w.text(n, cx, y, sw, barH, bold, 8.5, label, center)
			}
			cx += sw
		}
		// White hairlines part the segments.
		cx = x
		for i, s := range r.Severities {
			if s.Count <= 0 {
				continue
			}
			cx += bw * float64(s.Count) / float64(total)
			if i < len(r.Severities)-1 && cx < x+bw-0.1 {
				w.gp.SetLineWidth(0.5)
				w.gp.SetStrokeColor(255, 255, 255)
				w.gp.Line(cx, y, cx, y+barH)
			}
		}
	}
	w.y += barH + 2
	lx := margin
	for _, s := range r.Severities {
		fill, _ := levelFill(s.Level)
		w.fill(lx, w.y+1.3, 2.6, 2.6, fill)
		lx += 4
		lx += w.text(s.Name, lx, w.y, 60, 5, regular, 8, colSecondary, left) + 1.5
		lx += w.text(strconv.Itoa(s.Count), lx, w.y, 30, 5, bold, 8, levelText(s.Level), left) + 7
	}
	totals := fmt.Sprintf("%d CVEs · %d fixable", r.CVEsTotal, r.Fixable)
	w.text(totals, lx, w.y, margin+contentW()-lx, 5, bold, 8.5, colText, right)
	w.y += 9
}

// card is one top card's content.
type card struct {
	title   string
	columns []string
	rows    []TopRow
}

// Card metrics: the title line, the header line and a row line, mm.
const (
	cardPad    = 3.0
	cardTitleH = 6.5
	cardHeadH  = 4.5
	cardRowH   = 5.0
	cardGap    = 6.0
)

// drawCards draws Top images, Top CVEs and Top namespaces side by side.
func (r *ImageSecurity) drawCards(w *writer) {
	cards := []card{
		{"Top images", r.TopImagesColumns, r.TopImages},
		{"Top CVEs", r.TopCVEsColumns, r.TopCVEs},
		{"Top namespaces", r.TopNamespacesColumns, r.TopNamespaces},
	}
	rows := 1
	for _, c := range cards {
		rows = max(rows, len(c.rows))
	}
	h := cardPad + cardTitleH + cardHeadH + float64(rows)*cardRowH + cardPad
	w.ensure(h)
	cw := (contentW() - 2*cardGap) / 3
	for i, c := range cards {
		x := margin + float64(i)*(cw+cardGap)
		w.frame(x, w.y, cw, h, colBorder)
		drawCard(w, c, x+cardPad, w.y+cardPad, cw-2*cardPad)
	}
	w.y += h + 8
}

// drawCard draws one card's title and table into the width cw from (x, y).
func drawCard(w *writer, c card, x, y, cw float64) {
	w.text(c.title, x, y, cw, cardTitleH-1.5, bold, 9.5, colText, left)
	y += cardTitleH
	ncol := len(c.columns)
	for _, row := range c.rows {
		ncol = max(ncol, len(row.Cells))
	}
	if ncol == 0 || len(c.rows) == 0 {
		w.text("None", x, y+cardHeadH, cw, cardRowH, regular, 8, colMuted, left)
		return
	}
	widths, numeric := cardColumns(w, c, ncol, cw)
	cx := x
	for i := range ncol {
		a := left
		if numeric[i] {
			a = right
		}
		if i < len(c.columns) {
			w.text(c.columns[i], cx, y, widths[i], cardHeadH, regular, 7, colMuted, a)
		}
		cx += widths[i] + cardCellGap
	}
	y += cardHeadH
	w.rule(x, x+cw, y, 0.2, colRule)
	for _, row := range c.rows {
		cx = x
		for i := range ncol {
			cell := ""
			if i < len(row.Cells) {
				cell = row.Cells[i]
			}
			a := left
			if numeric[i] {
				a = right
			}
			col, fam := colText, regular
			if row.Level != LevelNone && i == row.LevelCell {
				col = levelText(row.Level)
				if row.Level == LevelCritical || row.Level == LevelHigh {
					fam = bold
				}
			}
			w.text(cell, cx, y, widths[i], cardRowH, fam, 8, col, a)
			cx += widths[i] + cardCellGap
		}
		y += cardRowH
	}
}

// cardCellGap is the space between a card's columns.
const cardCellGap = 2.5

// cardColumns sizes a card's columns: each takes its widest value, numbers align right, and the
// widest column takes what is left over or gives up what is missing.
func cardColumns(w *writer, c card, ncol int, cw float64) (widths []float64, numeric []bool) {
	widths = make([]float64, ncol)
	numeric = make([]bool, ncol)
	for i := range ncol {
		numeric[i] = true
		if i < len(c.columns) {
			widths[i] = w.textWidth(c.columns[i], regular, 7)
		}
		for _, row := range c.rows {
			if i >= len(row.Cells) {
				continue
			}
			cell := row.Cells[i]
			widths[i] = max(widths[i], w.textWidth(cell, bold, 8))
			if !isNumber(cell) {
				numeric[i] = false
			}
		}
	}
	avail := cw - float64(ncol-1)*cardCellGap
	widest := 0
	sum := 0.0
	for i, cwi := range widths {
		sum += cwi
		if cwi > widths[widest] {
			widest = i
		}
	}
	widths[widest] = max(avail-(sum-widths[widest]), 4)
	return widths, numeric
}

// isNumber reports whether a cell holds a count or a score (or the dash of none).
func isNumber(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "—" || s == "-" {
		return true
	}
	_, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return err == nil
}

// Trend chart metrics, mm.
const (
	trendPlotH = 48.0
	trendAxisW = 12.0
	trendXLabH = 6.0
)

// trendLayers are the curve's layers, bottom to top.
var trendLayers = []struct {
	name  string
	level Level
	value func(TrendPoint) int
}{
	{"Critical", LevelCritical, func(p TrendPoint) int { return p.Critical }},
	{"High", LevelHigh, func(p TrendPoint) int { return p.High }},
	{"Medium", LevelMedium, func(p TrendPoint) int { return p.Medium }},
	{"Low", LevelLow, func(p TrendPoint) int { return p.Low }},
	{"Unknown", LevelUnknown, func(p TrendPoint) int { return p.Unknown }},
}

// drawTrend draws the findings curve as stacked areas (stacked bars for a single sample), with
// its title, note, legend and axes.
func (r *ImageSecurity) drawTrend(w *writer) {
	t := r.Trend
	h := 7.0 + 5 + trendPlotH + trendXLabH
	if len(t.Points) == 0 {
		h = 7 + 5
	}
	w.ensure(h)
	title := t.Title
	if title == "" {
		title = "Findings over time"
	}
	section(w, margin, contentW(), title, "")
	if t.Note != "" {
		w.text(t.Note, margin, w.y-1.5, contentW()*0.6, 5, regular, 8, colMuted, left)
	}
	if len(t.Points) == 0 {
		w.y += 5 + 6
		return
	}
	trendLegend(w, t.Points)
	w.y += 5
	peak := 0
	for _, p := range t.Points {
		peak = max(peak, total(p))
	}
	pl := plot{w: w, pts: t.Points, x: margin + trendAxisW, top: w.y, width: contentW() - trendAxisW, ymax: niceCeil(peak)}
	pl.grid()
	pl.areas()
	pl.xLabels(axisLayout(r.TimeLayout, t.Points))
	w.y = pl.top + trendPlotH + trendXLabH + 6
}

// trendLegend draws the curve's legend right-aligned on the note's line; Unknown only when a
// sample counts one.
func trendLegend(w *writer, pts []TrendPoint) {
	lx := margin + contentW()
	for i := len(trendLayers) - 1; i >= 0; i-- {
		l := trendLayers[i]
		if l.level == LevelUnknown && !anyUnknown(pts) {
			continue
		}
		lw := w.textWidth(l.name, regular, 7.5)
		lx -= lw
		w.text(l.name, lx, w.y-1.5, lw, 5, regular, 7.5, colSecondary, left)
		lx -= 4
		fill, _ := levelFill(l.level)
		w.fill(lx, w.y-1.5+1.2, 2.6, 2.6, fill)
		lx -= 5
	}
}

// plot is the curve's drawing area: the samples, the plot's left edge, top and width (mm) and
// the value at its top.
type plot struct {
	w             *writer
	pts           []TrendPoint
	x, top, width float64
	ymax          int
}

// yOf is a value's y on the page.
func (p plot) yOf(v float64) float64 { return p.top + trendPlotH - v/float64(p.ymax)*trendPlotH }

// xOf is sample i's x on the page; a single sample sits in the middle.
func (p plot) xOf(i int) float64 {
	if len(p.pts) < 2 {
		return p.x + p.width/2
	}
	return p.x + p.width*float64(i)/float64(len(p.pts)-1)
}

// grid draws the gridlines and their labels: zero, half, the top.
func (p plot) grid() {
	for _, v := range []int{0, p.ymax / 2, p.ymax} {
		gy := p.yOf(float64(v))
		p.w.rule(p.x, p.x+p.width, gy, 0.2, colRule)
		p.w.text(strconv.Itoa(v), margin, gy-2, trendAxisW-2, 4, regular, 7, colMuted, right)
	}
}

// areas draws the layers stacked, bottom to top; one sample is a stacked bar.
func (p plot) areas() {
	n := len(p.pts)
	if n == 1 {
		bw := min(p.width/4, 12.0)
		bx := p.xOf(0) - bw/2
		base := 0.0
		for _, l := range trendLayers {
			v := float64(max(l.value(p.pts[0]), 0))
			fill, _ := levelFill(l.level)
			p.w.fill(bx, p.yOf(base+v), bw, p.yOf(base)-p.yOf(base+v), fill)
			base += v
		}
	} else {
		lower := make([]float64, n)
		for _, l := range trendLayers {
			upper := make([]float64, n)
			drawn := false
			for i, pt := range p.pts {
				v := max(l.value(pt), 0)
				drawn = drawn || v > 0
				upper[i] = lower[i] + float64(v)
			}
			if drawn {
				poly := make([]gopdf.Point, 0, 2*n)
				for i := range n {
					poly = append(poly, gopdf.Point{X: p.xOf(i), Y: p.yOf(upper[i])})
				}
				for i := n - 1; i >= 0; i-- {
					poly = append(poly, gopdf.Point{X: p.xOf(i), Y: p.yOf(lower[i])})
				}
				fill, _ := levelFill(l.level)
				p.w.polygon(poly, fill)
			}
			lower = upper
		}
	}
	p.w.rule(p.x, p.x+p.width, p.yOf(0), 0.3, colMuted)
}

// xLabels labels up to six samples, evenly spread, the first and the last included; the outer
// ones align with the plot's edges.
func (p plot) xLabels(layout string) {
	n := len(p.pts)
	ly := p.top + trendPlotH + 1
	labels := min(n, 6)
	for k := range labels {
		i := 0
		if labels > 1 {
			i = int(math.Round(float64(k) * float64(n-1) / float64(labels-1)))
		}
		s := p.pts[i].At.Format(layout)
		x := p.xOf(i)
		lw := p.w.textWidth(s, regular, 7)
		a, bx := center, x-lw/2-1
		switch {
		case n > 1 && i == 0:
			a, bx = left, x
		case n > 1 && i == n-1:
			a, bx = right, x-lw-2
		}
		p.w.text(s, bx, ly, lw+2, 4, regular, 7, colMuted, a)
	}
}

// anyUnknown reports whether a sample counts an Unknown finding.
func anyUnknown(pts []TrendPoint) bool {
	for _, p := range pts {
		if p.Unknown > 0 {
			return true
		}
	}
	return false
}

// total is a sample's count over every severity.
func total(p TrendPoint) int {
	return max(p.Critical, 0) + max(p.High, 0) + max(p.Medium, 0) + max(p.Low, 0) + max(p.Unknown, 0)
}

// niceCeil rounds a peak up to a round, even scale top, so the half line is a whole number: up
// to ten the next even number, above it 1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6 or 8 times a power of
// ten.
func niceCeil(v int) int {
	if v <= 10 {
		return max(2, v+v%2)
	}
	p := 1
	for p*100 < v {
		p *= 10
	}
	// p*10 < v <= p*100: the steps in tenths of p*100.
	for _, m := range []int{12, 15, 20, 25, 30, 40, 50, 60, 80, 100} {
		if c := m * p; c >= v && c%2 == 0 {
			return c
		}
	}
	return 100 * p
}

// axisLayout is the x axis's time layout: the date part of the page's layout when the samples
// span two days or more, else the full layout.
func axisLayout(layout string, pts []TrendPoint) string {
	if len(pts) > 1 && pts[len(pts)-1].At.Sub(pts[0].At) < 48*time.Hour {
		return layout
	}
	for _, tok := range []string{"15", "03", "3:", "PM", "pm"} {
		if i := strings.Index(layout, tok); i >= 0 {
			layout = layout[:i]
		}
	}
	if d := strings.TrimRight(layout, " ,T"); d != "" {
		return d
	}
	return "2006-01-02"
}

// The findings table's columns: x offset from the margin and width, mm.
type column struct {
	title string
	x, w  float64
	a     align
}

var cveColumns = []column{
	{"Severity", 0, 18, left},
	{"CVE", 20, 39, left},
	{"Score", 60, 11, right},
	{"Images", 76, 14, right},
	{"Containers", 96, 18, right},
	{"Package", 124, 88, left},
	{"Fixed", 214, 63, left},
}

// The place lines' columns, indented under the CVE: the image under the package, the versions
// under the fixed version.
var placeColumns = []column{
	{"Namespace", 6, 32, left},
	{"Workload", 40, 46, left},
	{"Container", 88, 34, left},
	{"Image", 124, 88, left},
	{"Installed → fixed", 214, 63, left},
}

// Table metrics, mm and points.
const (
	tableHeadH = 5.0
	tableSubH  = 4.0
	cveRowH    = 5.6
	placeH     = 4.1
	cveSize    = 8.0
	placeSize  = 7.0
)

// drawCVEs draws the CVEs table: a row per CVE, under it a line per container, the header
// repeated on every page it fills.
func (r *ImageSecurity) drawCVEs(w *writer) {
	w.ensure(7 + tableHeadH + tableSubH + cveRowH + placeH)
	aside := fmt.Sprintf("%d %s", len(r.CVEs), plural(len(r.CVEs), "CVE", "CVEs"))
	if r.Search != "" {
		aside += " matching “" + r.Search + "”"
	}
	section(w, margin, contentW(), "CVEs", aside)
	drawTableHead(w)
	w.onNewPage = func() { drawTableHead(w) }
	defer func() { w.onNewPage = nil }()
	if len(r.CVEs) == 0 {
		w.text("No CVEs match.", margin, w.y+1, contentW(), cveRowH, regular, cveSize, colMuted, left)
		w.y += cveRowH
		return
	}
	first := true
	for _, c := range r.CVEs {
		need := cveRowH
		if len(c.RunsIn) > 0 {
			need += placeH
		}
		if w.ensure(need) {
			first = true
		}
		if !first {
			w.rule(margin, margin+contentW(), w.y, 0.2, colRule)
		}
		first = false
		drawCVERow(w, c)
		for _, p := range c.RunsIn {
			if w.ensure(placeH) {
				w.text(c.ID+" (continued)", margin+placeColumns[0].x, w.y, contentW(), placeH, bold, placeSize, colMuted, left)
				w.y += placeH
			}
			drawPlace(w, p)
		}
		w.y += 0.8
	}
}

// drawTableHead draws the table's header: the CVE columns and, smaller, the place columns.
func drawTableHead(w *writer) {
	for _, c := range cveColumns {
		w.text(c.title, margin+c.x, w.y, c.w, tableHeadH, bold, 7.5, colSecondary, c.a)
	}
	w.y += tableHeadH
	for _, c := range placeColumns {
		w.text(c.title, margin+c.x, w.y, c.w, tableSubH, regular, 6.5, colMuted, c.a)
	}
	w.y += tableSubH
	w.rule(margin, margin+contentW(), w.y+0.3, 0.35, colMuted)
	w.y++
}

// drawCVERow draws a CVE's row.
func drawCVERow(w *writer, c CVE) {
	sevFam := regular
	if c.Level == LevelCritical || c.Level == LevelHigh {
		sevFam = bold
	}
	fixed, fixedCol := c.Fixed, colText
	if fixed == "" {
		fixed, fixedCol = "no fix", colMuted
	}
	score := c.Score
	if score == "" {
		score = "—"
	}
	cells := []struct {
		s   string
		fam string
		col rgb
	}{
		{c.Severity, sevFam, levelText(c.Level)},
		{c.ID, bold, colText},
		{score, regular, colText},
		{strconv.Itoa(c.Images), regular, colText},
		{strconv.Itoa(c.Containers), regular, colText},
		{c.Packages, regular, colText},
		{fixed, regular, fixedCol},
	}
	for i, col := range cveColumns {
		w.text(cells[i].s, margin+col.x, w.y, col.w, cveRowH, cells[i].fam, cveSize, cells[i].col, col.a)
	}
	w.y += cveRowH
}

// drawPlace draws one container line under its CVE.
func drawPlace(w *writer, p Place) {
	version := p.Installed
	if p.Fixed != "" {
		version += " → " + p.Fixed
	}
	cells := []string{p.Namespace, p.Workload, p.Container, p.Image, version}
	for i, col := range placeColumns {
		w.text(cells[i], margin+col.x, w.y, col.w, placeH, regular, placeSize, colSecondary, col.a)
	}
	w.y += placeH
}

// plural picks the word for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
