package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The navigator's header (navigator.go; D-57, the owner's interview of 2026-10-15 evening): at the
// section the title "All Clusters", a "+" that adds kubeconfigs and the search button; at a cluster
// the button "‹ All Clusters", the one way up to the section (← on the tree's root is the
// keyboard's), and the search button. Below them the search field while it is open. Lens's header
// has a dropdown of the scope instead; Cogpit's went with the context picker, so that one list
// chooses a cluster (D-57).

// navAllClusters is the header's name of the section level; its root row keeps Lens's "Local
// Kubeconfigs" (navSectionTitle), the name the cloud sections will need beside it (G18).
const navAllClusters = "All Clusters"

// navHeader is the navigator's header.
type navHeader struct {
	s       *sidebar
	title   *textLine      // "All Clusters", at the section
	back    *navBackButton // "‹ All Clusters", at a cluster
	addB    *tipButton     // "+": Add Kubeconfigs…, at the section
	searchB *tipButton
	entry   *navSearchEntry
	field   *fyne.Container // the search field's row, hidden until the search button is pressed
	root    *fyne.Container
}

func newNavHeader(s *sidebar) *navHeader {
	h := &navHeader{s: s}
	h.title = newTextLine(navAllClusters, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	h.title.truncate = true
	h.back = newNavBackButton(h)
	h.back.Hide()
	h.addB = newBareTipButton(s.sh, theme.ContentAddIcon(), "Add Kubeconfigs…", func() { s.sh.kc.addKubeconfigs() })
	h.searchB = newBareTipButton(s.sh, theme.SearchIcon(), "Search the navigator", h.toggleSearch)
	h.entry = newNavSearchEntry(h)
	h.field = inset(theme.SizeNamePadding, "", container.NewVBox(h.entry, newSpace(theme.SizeNamePadding)))
	h.field.Hide()
	// The title sits where the scope dropdown's text sat; the back button's ‹ takes that place, and
	// the button the whole row up to the buttons at its right, so a click anywhere left of them goes
	// up (the owner's WF27, 2026-10-18: "rechts davon ist auch valid").
	title := inset(theme.SizeNameInnerPadding, "", h.title)
	top := inset(theme.SizeNamePadding, theme.SizeNamePadding, container.NewBorder(nil, nil, nil,
		container.NewHBox(container.NewCenter(h.addB), container.NewCenter(h.searchB)),
		container.NewStack(title, h.back)))
	h.root = container.NewVBox(top, h.field, newDivider(false))
	return h
}

// update follows the navigator's scope: the section's title and "+", or a cluster's way up.
func (h *navHeader) update() {
	if h.s.sh.ui.ScopeCluster() == "" {
		h.back.Hide()
		h.title.Show()
		h.addB.Show()
		h.setPlaceHolder("Search clusters…")
		return
	}
	h.title.Hide()
	h.addB.Hide()
	h.back.Show()
	h.setPlaceHolder("Search items…")
}

// setPlaceHolder names what the search filters at the level shown.
func (h *navHeader) setPlaceHolder(text string) {
	if h.entry.PlaceHolder != text {
		h.entry.SetPlaceHolder(text)
	}
}

// searching reports whether the search field is open.
func (h *navHeader) searching() bool { return h.field.Visible() }

// toggleSearch opens the search field, or closes and clears it.
func (h *navHeader) toggleSearch() {
	if h.searching() {
		h.closeSearch()
		return
	}
	h.openSearch("")
}

// openSearch shows the search field with text, which filters the tree at once, and gives it the
// keyboard.
func (h *navHeader) openSearch(text string) {
	if !h.searching() {
		h.field.Show()
		h.s.root.Refresh() // the header grew: the tree below it moves down
	}
	h.entry.SetText(text)
	h.entry.CursorColumn = len([]rune(text))
	h.entry.Refresh()
	h.s.sh.win.Canvas().Focus(h.entry)
}

// closeSearch clears the search and hides its field; the keyboard goes back to the tree if the
// field had it. A search ⌘P opened ends with it (findCluster).
func (h *navHeader) closeSearch() {
	h.s.findFrom = ""
	c := h.s.sh.win.Canvas()
	hadFocus := c.Focused() == h.entry
	h.entry.SetText("")
	h.field.Hide()
	h.s.root.Refresh()
	if hadFocus {
		c.Focus(h.s.list())
	}
}

// escape is Escape on the open search, in its field or in the section's list. A search ⌘P opened
// over a cluster's tree (findCluster, D-57) clears its text first and keeps the field; on the empty
// search it goes back to that cluster's tree while the cluster is still the connected one. Any
// other search clears and closes.
func (h *navHeader) escape() {
	s := h.s
	from := s.findFrom
	if from == "" {
		h.closeSearch()
		return
	}
	if h.entry.Text != "" {
		h.entry.SetText("")
		return
	}
	ui := s.sh.ui
	h.closeSearch()
	if s.section && ui.Context == from && (ui.Connection.State.Live() || ui.Connection.State == engine.Connecting) {
		s.setScope(engine.NavScopeCluster(from))
	}
}

// navSearchEntry is the navigator's search field: typing filters the tree, Escape clears and
// closes it (escape), ↓ moves into the tree, ↵ opens the first match.
type navSearchEntry struct {
	widget.Entry
	h *navHeader
}

func newNavSearchEntry(h *navHeader) *navSearchEntry {
	e := &navSearchEntry{h: h}
	e.ExtendBaseWidget(e)
	e.SetPlaceHolder("Search items…")
	e.OnChanged = h.s.searchTyped
	e.OnSubmitted = func(string) { h.s.openFirstMatch() }
	return e
}

// TypedKey adds Escape and ↓ to the entry's keys; F1–F12 are the hotbar's (A64).
func (e *navSearchEntry) TypedKey(ev *fyne.KeyEvent) {
	if hotbarKey(e.h.s.sh, ev) {
		return
	}
	switch ev.Name {
	case fyne.KeyEscape:
		e.h.escape()
		return
	case fyne.KeyDown:
		if e.h.s.section {
			e.h.s.sec.enter()
		} else {
			e.h.s.tree.enter()
		}
		return
	}
	e.Entry.TypedKey(ev)
}

// navBackButton is the header's "‹ All Clusters" at a cluster: the section level
// (ui.setNavigatorScope), the cluster staying connected. It takes no keyboard focus, as the
// navigator's other small buttons.
type navBackButton struct {
	widget.BaseWidget
	h       *navHeader
	text    *textLine
	hovered bool
}

var (
	_ fyne.Tappable     = (*navBackButton)(nil)
	_ desktop.Hoverable = (*navBackButton)(nil)
)

func newNavBackButton(h *navHeader) *navBackButton {
	b := &navBackButton{h: h, text: newTextLine(navAllClusters, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})}
	b.ExtendBaseWidget(b)
	return b
}

// navBackTip is the back button's tooltip.
const navBackTip = "Back to every cluster; the cluster stays connected"

// Tapped implements fyne.Tappable: the section level.
func (b *navBackButton) Tapped(*fyne.PointEvent) {
	b.h.s.sh.tip.hideFor(b)
	b.h.s.goUp()
}

// MouseIn implements desktop.Hoverable.
func (b *navBackButton) MouseIn(ev *desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
	b.h.s.sh.tip.hover(b, navBackTip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *navBackButton) MouseMoved(ev *desktop.MouseEvent) {
	b.h.s.sh.tip.moved(b, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (b *navBackButton) MouseOut() {
	b.hovered = false
	b.Refresh()
	b.h.s.sh.tip.hideFor(b)
}

// CreateRenderer implements fyne.Widget.
func (b *navBackButton) CreateRenderer() fyne.WidgetRenderer {
	r := &navBackRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), arrow: newIconImage()}
	r.Refresh()
	return r
}

// navBackRenderer draws the ‹ and the text after it on a hover background.
type navBackRenderer struct {
	b     *navBackButton
	bg    *canvas.Rectangle
	arrow *iconImage
}

func (r *navBackRenderer) MinSize() fyne.Size {
	th := r.b.Theme()
	inner, pad, is := th.Size(theme.SizeNameInnerPadding), th.Size(theme.SizeNamePadding), th.Size(theme.SizeNameInlineIcon)
	ts := r.b.text.MinSize()
	return fyne.NewSize(pad+is+pad+ts.Width+inner, fyne.Max(ts.Height, is)+2*inner)
}

func (r *navBackRenderer) Layout(size fyne.Size) {
	th := r.b.Theme()
	pad, is := th.Size(theme.SizeNamePadding), th.Size(theme.SizeNameInlineIcon)
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.arrow.img.Move(fyne.NewPos(pad, (size.Height-is)/2))
	r.arrow.img.Resize(fyne.NewSize(is, is))
	x := pad + is + pad
	r.b.text.Move(fyne.NewPos(x, 0))
	r.b.text.Resize(fyne.NewSize(fyne.Max(0, size.Width-x), size.Height))
}

func (r *navBackRenderer) Refresh() {
	th := r.b.Theme()
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	if r.b.hovered {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	}
	r.arrow.set(th, iconChevronLeft, theme.ColorNameForeground)
	r.Layout(r.b.Size())
	r.bg.Refresh()
	r.b.text.Refresh()
}

func (r *navBackRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.b.text, r.arrow.img}
}

func (r *navBackRenderer) Destroy() {}
