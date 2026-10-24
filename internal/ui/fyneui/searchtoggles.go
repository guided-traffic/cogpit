package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The search field's toggles (ui-parity › Tables, G5): match case ("Aa") and regular expression
// (".*"), captured in Lens Desktop's search field (2026-10-11). Their state is the engine's
// (UIState.FilterOptions, set with ui.setFilter), each tab its own, so an agent sees and sets what
// the user sees. Cogpit's look and rules, inferred (Lens's are not captured): the two sit inside
// the field at its right end, look as the editor's find widget's switches do (findbar.go: filled
// in the accent colour while on), and a regular expression that does not compile, or is longer
// than engine.MaxFilterPattern, draws the field's border in the error colour and turns the ".*"
// red, its tooltip saying why; the view then shows every row (engine/filteroptions.go).

// Tooltips of the toggles (Cogpit's wording).
const (
	tipMatchCase = "Match case"
	tipRegex     = "Regular expression"
)

// searchToggles are the search field's two switches and its error border.
type searchToggles struct {
	tv               *tableView
	matchCase, regex *tipButton
	ring             *errorRing
	err              string // why the field's text filters nothing out; "" when it applies
}

// newSearchToggles puts the switches into the search entry, which must not have been drawn yet.
func newSearchToggles(tv *tableView) *searchToggles {
	s := &searchToggles{tv: tv, ring: newErrorRing()}
	s.matchCase = newBareTipButton(tv.sh, nil, tipMatchCase, func() { s.toggle(false) })
	s.matchCase.Text = "Aa"
	s.regex = newBareTipButton(tv.sh, nil, tipRegex, func() { s.toggle(true) })
	s.regex.Text = ".*"
	oneWidth(s.matchCase, s.regex) // one width, as the find bar's (VW3)
	// Inset by three points from the field's border and two apart (Cogpit's look).
	tv.search.ActionItem = container.New(layout.NewCustomPaddedLayout(3, 3, 0, 3),
		container.New(layout.NewCustomPaddedHBoxLayout(2), s.matchCase, s.regex))
	s.ring.Hide()
	return s
}

// field is the search entry with the error border over it.
func (s *searchToggles) field() fyne.CanvasObject {
	return container.NewStack(s.tv.search, s.ring)
}

// toggle flips one switch through ui.setFilter. The field's text goes along, and a debounced
// send of it still waiting is dropped, so that the engine gets text and switch together.
func (s *searchToggles) toggle(regex bool) {
	tv := s.tv
	o := tv.sh.ui.FilterOptions
	p := filterParams{Filter: tv.search.Text}
	if regex {
		on := !o.Regex
		p.Regex = &on
	} else {
		on := !o.MatchCase
		p.MatchCase = &on
	}
	if tv.searchTimer != nil && tv.searchTimer.Stop() {
		tv.filterBusy--
	}
	tv.filterBusy++
	tv.sh.dispatch("ui.setFilter", p, func(_ any, err error) {
		tv.filterBusy--
		tv.sh.showError(err)
		tv.syncSearch()
	})
}

// sync shows the engine's switches, and the error of the text in the field (which may be ahead of
// the engine while the user types) under them.
func (s *searchToggles) sync() {
	o := s.tv.sh.ui.FilterOptions
	s.err = engine.FilterError(s.tv.search.Text, o)
	setImportance(s.matchCase, o.MatchCase, false)
	setImportance(s.regex, o.Regex, s.err != "")
	s.regex.tip = tipRegex
	if s.err != "" {
		s.regex.tip = tipRegex + ": " + s.err
		s.ring.Show()
	} else {
		s.ring.Hide()
	}
}

// setImportance shows a switch on (accent), off (plain) or on with an error (red).
func setImportance(b *tipButton, on, failed bool) {
	imp := widget.LowImportance
	switch {
	case on && failed:
		imp = widget.DangerImportance
	case on:
		imp = widget.HighImportance
	}
	if b.Importance != imp {
		b.Importance = imp
		b.Refresh()
	}
}

// errorRing is a border in the error colour around the search field; it takes no input.
type errorRing struct {
	widget.BaseWidget
}

func newErrorRing() *errorRing {
	r := &errorRing{}
	r.ExtendBaseWidget(r)
	return r
}

// CreateRenderer implements fyne.Widget.
func (r *errorRing) CreateRenderer() fyne.WidgetRenderer {
	rr := &errorRingRenderer{r: r, rect: canvas.NewRectangle(color.Transparent)}
	rr.Refresh()
	return rr
}

type errorRingRenderer struct {
	r    *errorRing
	rect *canvas.Rectangle
}

func (rr *errorRingRenderer) Layout(size fyne.Size)        { rr.rect.Resize(size) }
func (rr *errorRingRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (rr *errorRingRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{rr.rect} }
func (rr *errorRingRenderer) Destroy()                     {}

func (rr *errorRingRenderer) Refresh() {
	th := rr.r.Theme()
	rr.rect.FillColor = color.Transparent
	rr.rect.StrokeColor = themeColor(th, colorStatusError)
	rr.rect.StrokeWidth = 2 * th.Size(theme.SizeNameInputBorder)
	rr.rect.CornerRadius = th.Size(theme.SizeNameInputRadius)
	rr.rect.Refresh()
}
