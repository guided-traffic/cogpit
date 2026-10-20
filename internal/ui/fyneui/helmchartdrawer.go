package fyneui

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A Helm chart's drawer (G11, HELM2; engine/helmchartdrawer.go): under the title a version picker,
// whose choice is engine state (ui.setChartVersion), and the chart's README as Markdown in the
// drawer's README section. Lens's chart details show the README and a version picker (inferred
// from Lens's documentation; the drawer is not captured). The README is remote text the chart's
// author wrote: it is rendered with Fyne's RichText (headings, lists, emphasis, code, tables,
// links), its images are never loaded (a placeholder with their alt text: an image would be a
// fetch from any host the README names), raw HTML is left out (Fyne's Markdown drops it), and a
// link opens in the browser only on the user's click and only for http and https, like Cogpit's
// other links (no agent opens a browser; README links may be relative to the chart or name
// another program's scheme, which Cogpit does not open).

func init() {
	drawerSections[helmChartsKind+"\x00"+engine.ReadmeSection] = chartReadmeSection
	drawerHeaderParts[helmChartsKind] = chartDrawerHeader
}

// Keys of what the chart drawer keeps in drawer.kept.
const (
	chartVersionKey = "helm.chartVersion"
	chartReadmeKey  = "helm.readme"
)

// chartVersionPicker is the drawer's version picker of one chart.
type chartVersionPicker struct {
	sel     *selectField
	root    fyne.CanvasObject
	syncing bool // the picker follows the model: no dispatch
}

// chartDrawerHeader returns the chart drawer's version picker under the title.
func chartDrawerHeader(d *drawer, m *engine.DetailModel) (lead, extra fyne.CanvasObject) {
	hc := m.HelmChart
	if hc == nil || len(hc.Versions) == 0 {
		return nil, nil
	}
	lead = chartHeaderIcon(d, hc.Icon) // left of the title (helmcharticons.go, D-46)
	p, _ := d.kept[chartVersionKey].(*chartVersionPicker)
	if p == nil {
		p = newChartVersionPicker(d, m.Ref)
		d.kept[chartVersionKey] = p
	}
	p.syncing = true
	if !slices.Equal(p.sel.Options, hc.Versions) {
		p.sel.SetOptions(hc.Versions)
	}
	if p.sel.Selected != hc.Version {
		p.sel.SetSelected(hc.Version)
	}
	p.syncing = false
	return lead, p.root
}

func newChartVersionPicker(d *drawer, ref model.ObjectRef) *chartVersionPicker {
	p := &chartVersionPicker{}
	p.sel = newSelectField(nil, func(v string) {
		if p.syncing {
			return
		}
		d.sh.dispatch("ui.setChartVersion", chartVersionParams{refParams: refParams{Ref: ref}, Version: v}, nil)
	})
	label := newTextLine("Version", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	p.root = container.NewHBox(container.NewCenter(label), p.sel)
	return p
}

// chartVersionParams mirror ui.setChartVersion's parameters.
type chartVersionParams struct {
	refParams
	Version string `json:"version,omitempty"`
}

// chartReadme is a rendered README the drawer keeps across rebuilds.
type chartReadme struct {
	key  string // the version and the text's hash
	root fyne.CanvasObject
}

// chartReadmeSection renders the README section: the README as Markdown once it is loaded, with a
// note when it was cut; else what the section says (loading, none, an error).
func chartReadmeSection(d *drawer, _ model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	text := ""
	if len(sec.Items) > 0 {
		text = sec.Items[0].Value
	}
	m := d.last
	if m == nil || m.HelmChart == nil || m.HelmChart.Readme.State != engine.ReadmeLoaded {
		c := colorMuted
		if m != nil && m.HelmChart != nil && m.HelmChart.Readme.State == engine.ReadmeFailed {
			c = colorStatusError
		}
		return newParagraph(text, c, d.contentWidth())
	}
	hc := m.HelmChart
	key := fmt.Sprintf("%s\x00%x", hc.Version, sha256.Sum256([]byte(text)))
	if kept, ok := d.kept[chartReadmeKey].(*chartReadme); ok && kept.key == key {
		return kept.root
	}
	rt := widget.NewRichText(readmeSegments(text, d.sh.openReadmeLink)...)
	rt.Wrapping = fyne.TextWrapWord
	var root fyne.CanvasObject = rt
	if r := hc.Readme; r.Truncated {
		note := fmt.Sprintf("Cut at %s of %s: Cogpit shows a README's first %s.", model.FormatBytes(int64(len(text))),
			model.FormatBytes(int64(r.Size)), model.FormatBytes(helm.MaxReadmeBytes))
		root = container.NewVBox(newParagraph(note, colorStatusWarning, d.contentWidth()), rt)
	}
	d.kept[chartReadmeKey] = &chartReadme{key: key, root: root}
	return root
}

// markdownImage is an inline Markdown image, ![alt](destination "title").
var markdownImage = regexp.MustCompile(`!\[([^\]\n]*)\]\(([^)\n]*)\)`)

// readmeSegments parses a README's Markdown into rich text without images: an image is its alt
// text in parentheses ("(image: Build status)"; brackets in a link's text would split it), and
// every link opens through open.
func readmeSegments(text string, open func(*url.URL)) []widget.RichTextSegment {
	return safeSegments(widget.NewRichTextFromMarkdown(withoutImages(text)).Segments, open)
}

// withoutImages replaces the inline images outside fenced code blocks with their alt text.
func withoutImages(text string) string {
	lines := strings.SplitAfter(text, "\n")
	fence := ""
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		case strings.HasPrefix(t, "```"):
			fence = "```"
			continue
		case strings.HasPrefix(t, "~~~"):
			fence = "~~~"
			continue
		}
		lines[i] = markdownImage.ReplaceAllStringFunc(l, func(img string) string {
			alt := strings.TrimSpace(markdownImage.FindStringSubmatch(img)[1])
			if alt == "" {
				alt = "image"
			}
			return "(image: " + alt + ")"
		})
	}
	return strings.Join(lines, "")
}

// safeSegments replaces what the Markdown parser still made an image (a reference-style image)
// with a placeholder, routes every link through open, in nested lists and paragraphs too, and
// writes a table as a list of its rows: Fyne's table segment draws its rows past the drawer's
// scrolled edge, and a chart's parameter table of hundreds of rows reads as well row by row.
func safeSegments(segs []widget.RichTextSegment, open func(*url.URL)) []widget.RichTextSegment {
	out := make([]widget.RichTextSegment, 0, len(segs))
	for _, s := range segs {
		switch t := s.(type) {
		case *widget.ImageSegment:
			title := strings.TrimSpace(t.Title)
			if title == "" {
				title = "image"
			}
			s = &widget.TextSegment{Style: widget.RichTextStyleParagraph, Text: "(image: " + title + ")"}
		case *widget.HyperlinkSegment:
			u := t.URL
			t.OnTapped = func() { open(u) }
		case *widget.ListSegment:
			t.Items = safeSegments(t.Items, open)
		case *widget.ParagraphSegment:
			t.Texts = safeSegments(t.Texts, open)
		case *widget.TableSegment:
			out = append(out, tableRows(t, open)...)
			continue
		}
		out = append(out, s)
	}
	return out
}

// tableRows writes a Markdown table as its header in bold, then a list of its rows, a row's cells
// separated by " · ".
func tableRows(t *widget.TableSegment, open func(*url.URL)) []widget.RichTextSegment {
	row := func(cells [][]widget.RichTextSegment, bold bool) []widget.RichTextSegment {
		var texts []widget.RichTextSegment
		for j, c := range cells {
			if j > 0 {
				texts = append(texts, &widget.TextSegment{Style: widget.RichTextStyleInline, Text: " · "})
			}
			c = safeSegments(c, open)
			for _, s := range c {
				if ts, ok := s.(*widget.TextSegment); ok && bold {
					ts.Style.TextStyle.Bold = true
				}
			}
			texts = append(texts, c...)
		}
		return texts
	}
	var out []widget.RichTextSegment
	if len(t.Headers) > 0 { // the header's line, ended as Markdown ends a paragraph
		out = append(append(out, row(t.Headers, true)...), &widget.TextSegment{Style: widget.RichTextStyleParagraph})
	}
	items := make([]widget.RichTextSegment, 0, len(t.Rows))
	for _, r := range t.Rows {
		items = append(items, &widget.ParagraphSegment{Texts: row(r, false)})
	}
	if len(items) > 0 {
		out = append(out, &widget.ListSegment{Items: items})
	}
	return out
}

// The schemes of the web links a README may open.
const (
	schemeWeb       = "http"
	schemeWebSecure = "https"
)

// openReadmeLink opens a README's link in the browser on the user's click: an absolute http or
// https URL only.
func (sh *shell) openReadmeLink(u *url.URL) {
	if u == nil || (u.Scheme != schemeWebSecure && u.Scheme != schemeWeb) || u.Host == "" {
		where := "it"
		if u != nil {
			where = shorten(strings.ToValidUTF8(u.Redacted(), "?"), 120)
		}
		sh.toast("warning", "Cogpit opens only http and https links from a README, not "+where+".")
		return
	}
	if err := openBrowser(sh.app, u); err != nil {
		sh.toast("error", "Could not open "+u.Redacted()+": "+err.Error())
	}
}
