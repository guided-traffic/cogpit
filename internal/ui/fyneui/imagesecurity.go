package fyneui

import (
	"reflect"
	"slices"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Image Security page's upper part (D-new): a line beside the page's title that counts the
// running images and those Trivy scanned, and four cards in a row: Vulnerabilities (the CVE ids by
// severity, a bar each, scaled to the largest), Top images, Top CVEs and Top namespaces. A click on
// an entry of the last three narrows the findings table below to it, through the page's search
// (ui.setFilter with the entry's token). The table is the page's table, which the table view shows
// as it shows any view's. Colours follow D-calm: red and amber for Critical and High and for the
// grades E and D, everything else grey (calmStatusColor, the calm grade badge). The data is the
// engine's (trivy.imageSecurity).

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

// The page's texts: its table's heading, its search's hint, its rows' noun.
const (
	imageSecTableTitle = "Findings"
	imageSecSearchHint = "CVE, package, image…"
	imageSecRowNoun    = "findings"
)

// The cards' columns, in points: a severity's word and count, a grade's shield, a list's counts.
const (
	isSevWidth    = 64
	isCountWidth  = 40
	isShieldWidth = 18
	isNumWidth    = 28
	isBarHeight   = 6
	isShieldSide  = 16
)

// imageSecPanel shows the page's header line and cards.
type imageSecPanel struct {
	sh    *shell
	sub   *textLine // beside the page's title
	box   *fyne.Container
	root  fyne.CanvasObject
	data  *engine.ImageSecurityModel
	shown bool // data is drawn

	requests atomic.Int32 // coalesced as the Workloads Overview's (overview.go)
}

func newImageSecPanel(sh *shell) *imageSecPanel {
	p := &imageSecPanel{sh: sh}
	p.sub = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	p.sub.truncate = true
	p.sub.baselineOf = theme.SizeNameSubHeadingText // on the title's line
	p.box = container.NewVBox()
	p.root = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, p.box)
	return p
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
		if again && p.sh.ui.View == engine.ImageSecurity {
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
	p.box.Objects = []fyne.CanvasObject{container.NewGridWithColumns(4,
		p.severityCard(m), p.imagesCard(m), p.cvesCard(m), p.namespacesCard(m))}
	p.box.Refresh()
}

// headLine is the line beside the title: "150 images running · 142 scanned · 8 not scanned ·
// newest scan 2h ago"; the running images only while the page reads the pods.
func (p *imageSecPanel) headLine(m *engine.ImageSecurityModel) string {
	if m.Error != "" && m.Reports == 0 {
		return ""
	}
	var parts []string
	if m.PodsRead {
		noun := " images running"
		if m.Running == 1 {
			noun = " image running"
		}
		parts = append(parts, formatCount(int64(m.Running))+noun, formatCount(int64(m.Scanned))+" scanned",
			formatCount(int64(m.NotScanned))+" not scanned")
	}
	if !m.NewestScan.IsZero() {
		parts = append(parts, "newest scan "+model.FormatAge(p.sh.now().Sub(m.NewestScan))+" ago")
	}
	return strings.Join(parts, " · ")
}

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

// severityCard is Vulnerabilities: per severity its word, a bar scaled to the largest count, and
// the count; Critical and High in their colours, the others grey.
func (p *imageSecPanel) severityCard(m *engine.ImageSecurityModel) fyne.CanvasObject {
	most := 0
	for _, s := range m.Severities {
		most = max(most, s.Count)
	}
	rows := make([]fyne.CanvasObject, 0, len(m.Severities))
	for _, s := range m.Severities {
		tone := calmStatusColor(s.Status)
		style := fyne.TextStyle{Bold: s.Status != model.StatusNone}
		word := newTextLine(s.Severity, tone, theme.SizeNameText, style)
		countTone := tone
		if s.Status == model.StatusWarning {
			countTone = theme.ColorNameForeground // one colour a row: High's word and bar carry it
		}
		count := newTextLine(formatCount(int64(s.Count)), countTone, theme.SizeNameText, fyne.TextStyle{Bold: s.Status != model.StatusNone})
		count.align = fyne.TextAlignTrailing
		frac := float32(0)
		if most > 0 {
			frac = float32(s.Count) / float32(most)
		}
		rows = append(rows, box(&isRowLayout{widths: []float32{isSevWidth, 0, isCountWidth}}, word, newIsBar(severityFill(s.Status), frac), count))
	}
	return isCard("Vulnerabilities", formatCount(int64(m.CVEs))+" CVEs · "+formatCount(int64(m.Fixable))+" fixable", rows...)
}

// severityFill is a severity bar's colour: the crests' red and amber for Critical and High (the
// shields' E and D), a faint grey for the others.
func severityFill(s model.Status) fyne.ThemeColorName {
	switch s {
	case model.StatusError:
		return colorGradeE
	case model.StatusWarning:
		return colorGradeD
	}
	return theme.ColorNameDisabled
}

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

// cvesCard is Top CVEs: the severity's word, the id, the images it affects.
func (p *imageSecPanel) cvesCard(m *engine.ImageSecurityModel) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(m.TopCVEs))
	for _, c := range m.TopCVEs {
		word := newTextLine(c.Severity, calmStatusColor(c.Status), theme.SizeNameText, fyne.TextStyle{Bold: c.Status != model.StatusNone})
		id := newTextLine(c.ID, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		id.truncate = true
		n := isNum(int64(c.Images))
		rows = append(rows, p.entryOf([]float32{isSevWidth, 0, isCountWidth}, c.Token, word, id, n))
	}
	if len(rows) == 0 {
		rows = append(rows, noneLine())
	}
	return isCard("Top CVEs", "images", rows...)
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

// isBar is a severity's bar: frac of its width, isBarHeight tall, centred in its line.
type isBar struct {
	fill *surface
	frac float32
}

func newIsBar(color fyne.ThemeColorName, frac float32) fyne.CanvasObject {
	b := &isBar{fill: newSurface(color), frac: frac}
	b.fill.radius = theme.SizeNameScrollBarSmall
	return box(b, b.fill)
}

func (b *isBar) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := size.Width * b.frac
	if b.frac > 0 {
		w = max(w, isBarHeight) // a count above zero shows
	}
	objs[0].Move(fyne.NewPos(0, (size.Height-isBarHeight)/2))
	objs[0].Resize(fyne.NewSize(w, isBarHeight))
}

func (b *isBar) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, isBarHeight) }

// pageHeading is the header's left part on a page: the title, and on Image Security the line
// beside it.
func (tv *tableView) pageHeading(view string) []fyne.CanvasObject {
	if view == engine.ImageSecurity {
		return []fyne.CanvasObject{tv.title, tv.imageSec.sub}
	}
	return []fyne.CanvasObject{tv.title}
}

// searchHint is the search field's hint: "Search pods…", on Image Security what its tokens take.
func (tv *tableView) searchHint() string {
	if tv.sh.ui.View == engine.ImageSecurity {
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
	if tv.snap == nil || tv.snap.View != engine.ImageSecurity || row < 0 || row >= len(tv.snap.Rows) {
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
