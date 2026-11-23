// Package pdfreport renders Cogpit's reports as PDF documents: values in, the document's bytes
// out. The engine converts a page's model into this package's input and writes or hands on the
// bytes; nothing here reads a file, the network or a cluster.
//
// The documents are text, not pictures: every word is a searchable glyph run in the embedded Go
// fonts, so a reader can search, select and copy it. Text never overflows its column: it is
// measured with the font's own advances and cut with "…".
//
// Layer L0: no Cogpit package, no I/O.
package pdfreport

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The page: A4 landscape in millimetres, a 10 mm margin, the footer inside the bottom margin's
// upper edge.
const (
	pageW   = 297.0
	pageH   = 210.0
	margin  = 10.0
	footerH = 6.0
	// ptMM is one typographic point in millimetres: font sizes are in points, the page in mm.
	ptMM = 25.4 / 72
	// capRatio is the Go fonts' cap height in em: a line's baseline sits so that capitals are
	// centred in its box.
	capRatio = 0.70
	// maxRunes caps what a cell measures: no column holds more, so a huge value costs nothing.
	maxRunes = 1000
	ellipsis = "…"
)

// The fonts' family names in the document.
const (
	regular = "regular"
	bold    = "bold"
)

// rgb is a colour for text, lines and fills.
type rgb struct{ r, g, b uint8 }

// The palette, made for white paper: the text in near-black, secondary text in a mid grey, rules
// faint. Red and amber are the calm colours' two steps of urgency (D-calm); the other severities
// are greys of falling weight.
var (
	colText      = rgb{33, 37, 41}
	colSecondary = rgb{73, 80, 87}
	colMuted     = rgb{108, 117, 125}
	colRule      = rgb{222, 226, 230}
	colBorder    = rgb{206, 212, 218}
	colWhite     = rgb{255, 255, 255}
	colRed       = rgb{192, 41, 43}
	colAmberText = rgb{168, 98, 0}
	colAmberFill = rgb{232, 160, 30}
	colGreyDark  = rgb{120, 128, 136}
	colGreyMid   = rgb{173, 181, 189}
	colGreyLight = rgb{214, 218, 222}
)

// face is one embedded font: its parsed outline for measuring, and per rune the advance at one
// point (mm) or whether the font lacks the glyph.
type face struct {
	f       *sfnt.Font
	buf     sfnt.Buffer
	upem    fixed.Int26_6
	advance map[rune]float64
	missing map[rune]bool
}

func newFace(ttf []byte) (*face, error) {
	f, err := sfnt.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return &face{f: f, upem: fixed.I(int(f.UnitsPerEm())), advance: map[rune]float64{}, missing: map[rune]bool{}}, nil
}

// has reports whether the font draws r.
func (fc *face) has(r rune) bool {
	if fc.missing[r] {
		return false
	}
	if _, ok := fc.advance[r]; ok {
		return true
	}
	idx, err := fc.f.GlyphIndex(&fc.buf, r)
	if err != nil || idx == 0 {
		fc.missing[r] = true
		return false
	}
	adv, err := fc.f.GlyphAdvance(&fc.buf, idx, fc.upem, font.HintingNone)
	if err != nil {
		fc.missing[r] = true
		return false
	}
	// adv is in font units (ppem = units per em): one em at one point is ptMM.
	fc.advance[r] = float64(adv) / float64(fc.upem) * ptMM
	return true
}

// width is s's advance in mm at size points; s holds only runes the font has (see clean).
func (fc *face) width(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		if fc.has(r) {
			w += fc.advance[r]
		}
	}
	return w * size
}

// writer draws one document: the gopdf document, the fonts, the cursor and the page break rule.
type writer struct {
	gp    *gopdf.GoPdf
	faces map[string]*face
	// y is the cursor: the top of the next block on the current page.
	y float64
	// onNewPage draws what a page repeats at its top (a table's header); nil: nothing.
	onNewPage func()
	// err is the first drawing error; drawing stops being useful after one, Render returns it.
	err error
}

func newWriter(info gopdf.PdfInfo) (*writer, error) {
	gp := &gopdf.GoPdf{}
	gp.Start(gopdf.Config{Unit: gopdf.UnitMM, PageSize: *gopdf.PageSizeA4Landscape})
	gp.SetInfo(info)
	w := &writer{gp: gp, faces: map[string]*face{}}
	for name, ttf := range map[string][]byte{regular: goregular.TTF, bold: gobold.TTF} {
		if err := gp.AddTTFFontData(name, ttf); err != nil {
			return nil, fmt.Errorf("pdfreport: font %s: %w", name, err)
		}
		fc, err := newFace(ttf)
		if err != nil {
			return nil, fmt.Errorf("pdfreport: font %s: %w", name, err)
		}
		w.faces[name] = fc
	}
	return w, nil
}

// contentW is the width between the margins.
func contentW() float64 { return pageW - 2*margin }

// bottom is the lowest y a block may reach: above the footer.
func bottom() float64 { return pageH - margin - footerH }

// fail keeps the first error.
func (w *writer) fail(err error) {
	if err != nil && w.err == nil {
		w.err = err
	}
}

// newPage starts a page and draws what it repeats.
func (w *writer) newPage() {
	w.gp.AddPage()
	w.y = margin
	if w.onNewPage != nil {
		w.onNewPage()
	}
}

// ensure starts a new page when a block of height h no longer fits on this one; it reports
// whether it did.
func (w *writer) ensure(h float64) bool {
	if w.y+h <= bottom() {
		return false
	}
	w.newPage()
	return true
}

// clean makes s drawable in a font: control characters become spaces, a rune the font lacks
// becomes "?", and s is cut to maxRunes (the cut marked with an ellipsis). It reports whether it
// cut s.
func (w *writer) clean(s, family string) (string, bool) {
	fc := w.faces[family]
	ok := true
	n := 0
	for _, r := range s {
		n++
		if n > maxRunes || r == utf8.RuneError || unicode.IsControl(r) || !fc.has(r) {
			ok = false
			break
		}
	}
	if ok {
		return s, false
	}
	var b strings.Builder
	n = 0
	for _, r := range s {
		if n == maxRunes {
			return b.String(), true
		}
		n++
		switch {
		case unicode.IsControl(r):
			b.WriteByte(' ')
		case r == utf8.RuneError || !fc.has(r):
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), false
}

// fit returns s as it fits into maxW mm in the font: whole, or cut with an ellipsis; "" when not
// even the ellipsis fits.
func (w *writer) fit(s, family string, size, maxW float64) string {
	s, cut := w.clean(s, family)
	fc := w.faces[family]
	if !cut && fc.width(s, size) <= maxW {
		return s
	}
	room := maxW - fc.width(ellipsis, size)
	if room < 0 {
		return ""
	}
	acc := 0.0
	for i, r := range s {
		acc += fc.advance[r] * size
		if acc > room {
			return strings.TrimRight(s[:i], " ") + ellipsis
		}
	}
	return s + ellipsis
}

// textWidth is s's width in mm, as drawn.
func (w *writer) textWidth(s, family string, size float64) float64 {
	s, _ = w.clean(s, family)
	return w.faces[family].width(s, size)
}

// align is where a text sits in its box.
type align int

const (
	left align = iota
	right
	center
)

// text draws s into the box [x, x+boxW] of a line whose top is top and height lineH, cut to fit
// the box; it returns the width drawn.
func (w *writer) text(s string, x, top, boxW, lineH float64, family string, size float64, c rgb, a align) float64 {
	if s == "" || boxW <= 0 {
		return 0
	}
	s = w.fit(s, family, size, boxW)
	if s == "" {
		return 0
	}
	tw := w.faces[family].width(s, size)
	switch a {
	case right:
		x += boxW - tw
	case center:
		x += (boxW - tw) / 2
	}
	baseline := top + (lineH+capRatio*size*ptMM)/2
	w.fail(w.gp.SetFont(family, "", size))
	w.gp.SetTextColor(c.r, c.g, c.b)
	w.gp.SetXY(x, baseline)
	w.fail(w.gp.Text(s))
	return tw
}

// rule draws a horizontal line.
func (w *writer) rule(x0, x1, y, width float64, c rgb) {
	w.gp.SetLineWidth(width)
	w.gp.SetStrokeColor(c.r, c.g, c.b)
	w.gp.Line(x0, y, x1, y)
}

// fill draws a filled rectangle.
func (w *writer) fill(x, y, rw, rh float64, c rgb) {
	if rw <= 0 || rh <= 0 {
		return
	}
	w.gp.SetFillColor(c.r, c.g, c.b)
	w.gp.RectFromUpperLeftWithStyle(x, y, rw, rh, "F")
}

// frame draws a rectangle's outline with rounded corners.
func (w *writer) frame(x, y, rw, rh float64, c rgb) {
	w.gp.SetLineWidth(0.25)
	w.gp.SetStrokeColor(c.r, c.g, c.b)
	w.fail(w.gp.Rectangle(x, y, x+rw, y+rh, "D", 1.5, 6))
}

// polygon draws a filled polygon.
func (w *writer) polygon(pts []gopdf.Point, c rgb) {
	if len(pts) < 3 {
		return
	}
	w.gp.SetFillColor(c.r, c.g, c.b)
	w.gp.SetStrokeColor(c.r, c.g, c.b)
	w.gp.SetLineWidth(0.05)
	w.gp.Polygon(pts, "FD")
}

// footers writes every page's footer, now that the page count is known: left the label, right
// "page n of m".
func (w *writer) footers(label string) error {
	n := w.gp.GetNumberOfPages()
	top := pageH - margin - footerH + 2
	for p := 1; p <= n; p++ {
		if err := w.gp.SetPage(p); err != nil {
			return err
		}
		w.rule(margin, pageW-margin, top, 0.2, colRule)
		pageLabel := fmt.Sprintf("page %d of %d", p, n)
		pw := w.textWidth(pageLabel, regular, 7)
		w.text(label, margin, top, contentW()-pw-6, footerH-2, regular, 7, colMuted, left)
		w.text(pageLabel, margin, top, contentW(), footerH-2, regular, 7, colMuted, right)
	}
	return w.err
}

// bytes is the finished document.
func (w *writer) bytes() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	b, err := w.gp.GetBytesPdfReturnErr()
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("pdfreport: empty document")
	}
	return b, nil
}
