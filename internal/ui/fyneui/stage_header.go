package fyneui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer's header in the Dark Cockpit (docs/developer/design-language.md): an eyebrow naming
// the kind and the place, a large name, a status that is quiet when healthy and lit when not, and a
// wash behind them that carries the object's state in its colour.

// eyebrow is the drawer's small head: the kind in lowercase and, for a namespaced object, its
// namespace, "pod · inventory". It is the only place a kind's name is lowercased on purpose.
func eyebrow(kind, namespace string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if namespace == "" {
		return kind
	}
	return kind + " · " + namespace
}

// quietStatus reports whether a status colour is a healthy one: it draws a dot and the word in the
// text colour and no plate, as the design's principle "quiet when healthy" asks.
func quietStatus(tone fyne.ThemeColorName) bool {
	return tone == colorStatusOK || tone == colorStatusSucceeded
}

// statusBadge is the drawer's status: a six-point dot and the status word. A healthy state shows
// them on the plain header; any other state puts them on a plate of the status colour at about 14 %,
// with the word in the status colour. The dot and the word keep the state readable without the colour.
type statusBadge struct {
	widget.BaseWidget
	text string
	tone fyne.ThemeColorName
	maxW float32 // the widest the badge may be; 0: as wide as its text
}

func newStatusBadge() *statusBadge {
	s := &statusBadge{tone: colorMuted}
	s.ExtendBaseWidget(s)
	return s
}

// Set changes the word and the colour.
func (s *statusBadge) Set(text string, tone fyne.ThemeColorName) {
	if text == s.text && tone == s.tone {
		return
	}
	s.text, s.tone = text, tone
	s.Refresh()
}

// limit sets the widest the badge may be; a longer word is cut with an ellipsis. The second
// argument keeps the pill's signature (a status word never wraps).
func (s *statusBadge) limit(maxW float32, _ bool) {
	if maxW == s.maxW {
		return
	}
	s.maxW = maxW
	s.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (s *statusBadge) CreateRenderer() fyne.WidgetRenderer {
	r := &statusBadgeRenderer{s: s, plate: canvas.NewRectangle(color.Transparent),
		dot: canvas.NewCircle(color.Transparent), txt: canvas.NewText("", color.Transparent)}
	r.Refresh()
	return r
}

type statusBadgeRenderer struct {
	s     *statusBadge
	plate *canvas.Rectangle
	dot   *canvas.Circle
	txt   *canvas.Text
}

const (
	badgeDot  = 6 // the dot's diameter
	badgePadH = 8 // the plate's padding left and right
	badgePadV = 3
	badgeGap  = 5 // between the dot and the word
)

func (r *statusBadgeRenderer) textSize() float32 {
	return r.s.Theme().Size(theme.SizeNameCaptionText) + 1
}

// shown is the word, cut to the width the badge may have.
func (r *statusBadgeRenderer) shown() string {
	if r.s.maxW <= 0 {
		return r.s.text
	}
	return ellipsize(r.s.text, r.s.maxW-2*badgePadH-badgeDot-badgeGap, r.textSize(), fyne.TextStyle{})
}

func (r *statusBadgeRenderer) MinSize() fyne.Size {
	ts := fyne.MeasureText(r.shown(), r.textSize(), fyne.TextStyle{})
	return fyne.NewSize(ts.Width+badgeDot+badgeGap+2*badgePadH, ts.Height+2*badgePadV)
}

func (r *statusBadgeRenderer) Layout(size fyne.Size) {
	ms := r.MinSize()
	y := (size.Height - ms.Height) / 2 // the badge keeps its height and sits centred
	r.plate.Move(fyne.NewPos(0, y))
	r.plate.Resize(ms)
	r.dot.Move(fyne.NewPos(badgePadH, y+(ms.Height-badgeDot)/2))
	r.dot.Resize(fyne.NewSize(badgeDot, badgeDot))
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(badgePadH+badgeDot+badgeGap, y+(ms.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *statusBadgeRenderer) Refresh() {
	th := r.s.Theme()
	c := themeColor(th, r.s.tone)
	r.txt.Text, r.txt.TextSize = r.shown(), r.textSize()
	r.dot.FillColor = c
	if quietStatus(r.s.tone) {
		r.plate.FillColor = color.Transparent
		r.txt.Color = themeColor(th, theme.ColorNameForeground)
	} else {
		r.plate.FillColor = withAlpha(c, 0x24) // about 14 %
		r.txt.Color = c
	}
	r.plate.CornerRadius = th.Size(sizeChipRadius)
	r.Layout(r.s.Size())
	r.plate.Refresh()
	r.dot.Refresh()
	r.txt.Refresh()
}

func (r *statusBadgeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.plate, r.dot, r.txt}
}
func (r *statusBadgeRenderer) Destroy() {}

// stateWash is the header's backdrop: the drawer's colour with a faint vertical gradient from the
// object's status colour to nothing, so that the drawer carries the object's state before a word is
// read. A healthy object is washed lightly, an object without a status not at all.
type stateWash struct {
	widget.BaseWidget
	tone  fyne.ThemeColorName
	alpha uint8
}

func newStateWash() *stateWash {
	w := &stateWash{}
	w.ExtendBaseWidget(w)
	return w
}

// Set tints the wash with tone at alpha; an empty tone clears it.
func (w *stateWash) Set(tone fyne.ThemeColorName, alpha uint8) {
	if tone == w.tone && alpha == w.alpha {
		return
	}
	w.tone, w.alpha = tone, alpha
	w.Refresh()
}

// setStatus washes the header for a status: about 14 % for a state that needs a look; a healthy
// object and one without a status are not washed at all (quiet when healthy: a 6 % wash was
// invisible and only cost a gradient).
func (w *stateWash) setStatus(s model.Status) {
	if s == model.StatusNone || quietStatus(statusColorName(s)) {
		w.Set("", 0)
		return
	}
	w.Set(statusColorName(s), 0x24)
}

// stateWashReach is how far the wash reaches down from the header's top edge, in points: it fades
// out inside the header instead of ending in a band at the rule below.
const stateWashReach = 48

// CreateRenderer implements fyne.Widget.
func (w *stateWash) CreateRenderer() fyne.WidgetRenderer {
	r := &stateWashRenderer{w: w, bg: canvas.NewRectangle(color.Transparent),
		grad: canvas.NewLinearGradient(color.Transparent, color.Transparent, 0)}
	r.Refresh()
	return r
}

type stateWashRenderer struct {
	w    *stateWash
	bg   *canvas.Rectangle
	grad *canvas.LinearGradient
}

func (r *stateWashRenderer) MinSize() fyne.Size { return fyne.NewSize(0, 0) }

func (r *stateWashRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.grad.Resize(fyne.NewSize(size.Width, min(size.Height, stateWashReach)))
}

func (r *stateWashRenderer) Refresh() {
	th := r.w.Theme()
	r.bg.FillColor = themeColor(th, colorDrawer)
	if r.w.tone == "" {
		r.grad.Hidden = true
	} else {
		c := themeColor(th, r.w.tone)
		r.grad.Hidden = false
		r.grad.StartColor, r.grad.EndColor = withAlpha(c, r.w.alpha), withAlpha(c, 0)
	}
	r.Layout(r.w.Size())
	r.bg.Refresh()
	r.grad.Refresh()
}

func (r *stateWashRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.grad} }
func (r *stateWashRenderer) Destroy()                     {}

// sectionHead is a section's head in the drawer: its title in lowercase with the number of rows it
// holds, "properties · 14", as a bracket label in the light of cluster data. It keeps the title as
// the model gave it, so that a reader of the drawer (a test, an accessibility layer) finds
// "Properties" and not the label's lowercase.
type sectionHead struct {
	widget.BaseWidget
	title string
	n     int
}

func newSectionHead(title string, n int) *sectionHead {
	h := &sectionHead{title: title, n: n}
	h.ExtendBaseWidget(h)
	return h
}

// CreateRenderer implements fyne.Widget.
func (h *sectionHead) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(newBracketLabel(headText(h.title, h.n), colorAccent))
}

// sectionHeadOf is an ungrouped section's head: its title and its rows.
func sectionHeadOf(sec model.DetailSection) fyne.CanvasObject {
	n := len(sec.Items)
	if sec.Table != nil {
		n += len(sec.Table.Rows)
	}
	return newSectionHead(sec.Title, n)
}

// headText is a bracket label's text for a title and a count; no count: the title alone.
func headText(title string, n int) string {
	title = strings.ToLower(title)
	if n <= 0 {
		return title
	}
	return fmt.Sprintf("%s · %d", title, n)
}

// groupSize is the number of sections in a group ("containers · 2").
func groupSize(secs []model.DetailSection, group string) int {
	n := 0
	for _, s := range secs {
		if s.Group == group {
			n++
		}
	}
	return n
}
