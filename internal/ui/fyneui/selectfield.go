package fyneui

import (
	"slices"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// selectField is Cogpit's dropdown, every one in the window (the owner's gate walk of 2026-10-20,
// GW5; D-104). Fyne's Select opens its list below the field, the current entry among the others,
// so the choice stands twice: "verwirrend". This one opens the list over the field as a macOS
// pop-up button does: the current entry at the field's place, marked with a check, the entries
// before it above and those after it below, the list scrolled when it is taller than the window
// (a chart's hundreds of versions) so that the current entry still sits there.
//
// The field is Fyne's Select, embedded: its look, its state and its API (Options, Selected,
// SetSelected, OnChanged, PlaceHolder, Disable), so a choice reaches the code through OnChanged
// exactly as before and the commands and state agents see do not change. Only the list is
// Cogpit's. Keys as before: Space, ↑ and ↓ open it, ← and → step through the entries without it;
// in the list ↑ ↓ (Page Up, Page Down, Home, End) move, a letter jumps to the next entry starting
// with it, Return, Enter or Space choose and Escape closes; a click outside closes it too.
type selectField struct {
	widget.Select
	pop *selectPopup // the open list; nil, or hidden by a click outside, while closed

	pos  fyne.Position
	size fyne.Size
}

var (
	_ fyne.Tappable  = (*selectField)(nil)
	_ fyne.Focusable = (*selectField)(nil)
)

func newSelectField(options []string, changed func(string)) *selectField {
	s := &selectField{}
	s.Options, s.OnChanged = options, changed
	s.ExtendBaseWidget(s)
	return s
}

// Tapped implements fyne.Tappable: it focuses the field and opens the list.
func (s *selectField) Tapped(*fyne.PointEvent) {
	if s.Disabled() {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil && !fyne.CurrentDevice().IsMobile() {
		c.Focus(s)
	}
	s.open()
}

// TypedKey implements fyne.Focusable: Space, ↑ and ↓ open the list; the rest is Fyne's (← and →
// step through the entries).
func (s *selectField) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeySpace, fyne.KeyUp, fyne.KeyDown:
		s.open()
	default:
		s.Select.TypedKey(ev)
	}
}

// Hide closes the list with the field.
func (s *selectField) Hide() {
	s.close()
	s.Select.Hide()
}

// Move closes the list when the field moves: it would no longer lie over it.
func (s *selectField) Move(pos fyne.Position) {
	if pos != s.pos {
		s.close()
	}
	s.pos = pos
	s.Select.Move(pos)
}

// Resize closes the list when the field changes its size.
func (s *selectField) Resize(size fyne.Size) {
	if size != s.size {
		s.close()
	}
	s.size = size
	s.Select.Resize(size)
}

// Refresh closes a list whose entries are no longer the field's (a chart's versions loaded).
func (s *selectField) Refresh() {
	if s.isOpen() && !slices.Equal(s.pop.options, s.Options) {
		s.close()
	}
	s.Select.Refresh()
}

// isOpen reports whether the list is on screen.
func (s *selectField) isOpen() bool { return s.pop != nil && s.pop.popup.Visible() }

// open shows the list over the field; nothing for a field without entries.
func (s *selectField) open() {
	if s.Disabled() || len(s.Options) == 0 {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil { // taken off its canvas between the event and now
		return
	}
	s.close()
	s.pop = newSelectPopup(s, c)
	s.pop.show()
}

// close takes the list down.
func (s *selectField) close() {
	if s.pop == nil {
		return
	}
	p := s.pop
	s.pop = nil
	p.popup.Hide()
}

// choose picks the entry i of the open list, as a click on it does.
func (s *selectField) choose(option string) {
	s.close()
	s.SetSelected(option) // Fyne's: OnChanged, also for the entry already chosen, as its own list did
}

// selectPopup is a selectField's open list: a row per entry, in a pop-up over the field.
type selectPopup struct {
	widget.BaseWidget
	sel     *selectField
	canvas  fyne.Canvas
	options []string // the entries as the list opened
	rows    []*selectRow
	list    *fyne.Container
	scroll  *container.Scroll
	popup   *widget.PopUp
	active  int     // the highlighted row; -1 for none
	rowH    float32 // a row's height: the field's
}

var _ fyne.Focusable = (*selectPopup)(nil)

func newSelectPopup(s *selectField, c fyne.Canvas) *selectPopup {
	p := &selectPopup{sel: s, canvas: c, options: slices.Clone(s.Options), active: s.SelectedIndex()}
	p.ExtendBaseWidget(p)
	p.rowH = max(s.Size().Height, s.MinSize().Height)
	p.list = box(selectListLayout{p: p})
	for i, o := range p.options {
		r := newSelectRow(p, i, o, o == s.Selected)
		p.rows = append(p.rows, r)
		p.list.Objects = append(p.list.Objects, r)
	}
	p.scroll = container.NewVScroll(p.list)
	p.popup = widget.NewPopUp(p, c)
	return p
}

// selectInsets are the list's measures: its rows' vertical inset at its top and bottom, the
// margin it keeps from the window's edges, and where a row's text begins (after the check's room).
func selectInsets() (inset, margin, textX, iconW float32) {
	th := fyne.CurrentApp().Settings().Theme()
	pad, inner := th.Size(theme.SizeNamePadding), th.Size(theme.SizeNameInnerPadding)
	iconW = th.Size(theme.SizeNameInlineIcon)
	return pad / 2, pad, inner + iconW + pad, iconW
}

// fieldTextX is where the field's own text begins: Fyne's Select draws it at the inner padding.
func fieldTextX() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

// show places the list over the field, the current entry (else the first) where the field is,
// scrolled where the window has no room for the rows above it, and gives it the keyboard.
func (p *selectPopup) show() {
	inset, margin, textX, _ := selectInsets()
	th := fyne.CurrentApp().Settings().Theme()
	area := p.canvas.Size()
	fieldPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(p.sel)
	fieldW := p.sel.Size().Width

	width := fieldW + textX - fieldTextX()
	for _, o := range p.options {
		width = max(width, textX+fyne.MeasureText(o, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width+2*th.Size(theme.SizeNameInnerPadding))
	}
	width = min(width, area.Width-2*margin)
	total := float32(len(p.options))*p.rowH + 2*inset
	height := min(total, area.Height-2*margin)

	k := max(p.active, 0)
	rowTop := inset + float32(k)*p.rowH // the current row's place in the list
	top, off := fieldPos.Y-rowTop, float32(0)
	if top < margin { // no room above: from the window's top, scrolled so that the row is at the field
		off, top = margin-top, margin
	}
	if top+height > area.Height-margin { // no room below
		top = area.Height - margin - height
		off = top + rowTop - fieldPos.Y
	}
	off = min(max(off, 0), total-height)
	left := min(max(fieldPos.X+fieldTextX()-textX, margin), area.Width-margin-width)

	p.scroll.SetMinSize(fyne.NewSize(width, height))
	p.popup.ShowAtPosition(fyne.NewPos(left, top)) // never at 0,0: Fyne would centre it
	p.popup.Resize(fyne.NewSize(width, height))
	p.scroll.ScrollToOffset(fyne.NewPos(0, off))
	p.canvas.Focus(p)
	p.setActive(p.active)
}

// setActive highlights row i (-1: none) and scrolls it into view.
func (p *selectPopup) setActive(i int) {
	if i >= len(p.rows) {
		i = len(p.rows) - 1
	}
	for j, r := range p.rows {
		r.setActive(j == i)
	}
	p.active = i
	if i < 0 {
		return
	}
	inset, _, _, _ := selectInsets()
	y := inset + float32(i)*p.rowH
	view := p.scroll.Size().Height
	switch off := p.scroll.Offset.Y; {
	case y < off:
		p.scroll.ScrollToOffset(fyne.NewPos(0, y-inset))
	case y+p.rowH > off+view:
		p.scroll.ScrollToOffset(fyne.NewPos(0, y+p.rowH+inset-view))
	}
}

// pick chooses row i.
func (p *selectPopup) pick(i int) {
	if i < 0 || i >= len(p.options) {
		p.dismiss()
		return
	}
	if p.sel.pop == p {
		p.sel.choose(p.options[i])
		return
	}
	p.popup.Hide()
}

// dismiss closes the list without a choice.
func (p *selectPopup) dismiss() {
	if p.sel.pop == p {
		p.sel.close()
		return
	}
	p.popup.Hide()
}

// FocusGained implements fyne.Focusable.
func (p *selectPopup) FocusGained() {}

// FocusLost implements fyne.Focusable.
func (p *selectPopup) FocusLost() {}

// TypedKey implements fyne.Focusable: the list's keys.
func (p *selectPopup) TypedKey(ev *fyne.KeyEvent) {
	page := max(int(p.scroll.Size().Height/p.rowH)-1, 1)
	start := p.active
	if start < 0 {
		start = max(p.sel.SelectedIndex(), 0)
	}
	switch ev.Name {
	case fyne.KeyDown:
		if p.active < 0 {
			p.setActive(start)
		} else {
			p.setActive(min(start+1, len(p.rows)-1))
		}
	case fyne.KeyUp:
		if p.active < 0 {
			p.setActive(start)
		} else {
			p.setActive(max(start-1, 0))
		}
	case fyne.KeyPageDown:
		p.setActive(min(start+page, len(p.rows)-1))
	case fyne.KeyPageUp:
		p.setActive(max(start-page, 0))
	case fyne.KeyHome:
		p.setActive(0)
	case fyne.KeyEnd:
		p.setActive(len(p.rows) - 1)
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeySpace:
		p.pick(p.active)
	case fyne.KeyEscape:
		p.dismiss()
	}
}

// TypedRune implements fyne.Focusable: a letter jumps to the next entry starting with it.
func (p *selectPopup) TypedRune(r rune) {
	n := len(p.options)
	if n == 0 || unicode.IsSpace(r) {
		return
	}
	want := strings.ToLower(string(r))
	for d := 1; d <= n; d++ {
		i := (max(p.active, 0) + d) % n
		if strings.HasPrefix(strings.ToLower(p.options[i]), want) {
			p.setActive(i)
			return
		}
	}
}

// CreateRenderer implements fyne.Widget.
func (p *selectPopup) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.scroll)
}

// selectListLayout stacks the rows, each the field's height, inside the list's inset.
type selectListLayout struct{ p *selectPopup }

func (l selectListLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	inset, _, _, _ := selectInsets()
	var w float32
	for _, o := range objs {
		w = max(w, o.MinSize().Width)
	}
	return fyne.NewSize(w, float32(len(objs))*l.p.rowH+2*inset)
}

func (l selectListLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	inset, _, _, _ := selectInsets()
	for i, o := range objs {
		o.Move(fyne.NewPos(0, inset+float32(i)*l.p.rowH))
		o.Resize(fyne.NewSize(size.Width, l.p.rowH))
	}
}

// selectRow is one entry of the list: the check where it is the current one, its text, the
// highlight while it is under the pointer or the keys.
type selectRow struct {
	widget.BaseWidget
	p       *selectPopup
	i       int
	text    string
	current bool
	active  bool
	bg      *canvas.Rectangle
}

var (
	_ desktop.Hoverable = (*selectRow)(nil)
	_ fyne.Tappable     = (*selectRow)(nil)
)

func newSelectRow(p *selectPopup, i int, text string, current bool) *selectRow {
	r := &selectRow{p: p, i: i, text: text, current: current}
	r.ExtendBaseWidget(r)
	return r
}

func (r *selectRow) setActive(on bool) {
	if r.active == on {
		return
	}
	r.active = on
	r.Refresh()
}

// MouseIn implements desktop.Hoverable.
func (r *selectRow) MouseIn(*desktop.MouseEvent) { r.p.setActive(r.i) }

// MouseMoved implements desktop.Hoverable.
func (r *selectRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *selectRow) MouseOut() {
	if r.p.active == r.i {
		r.p.setActive(-1)
	}
}

// Tapped implements fyne.Tappable.
func (r *selectRow) Tapped(*fyne.PointEvent) { r.p.pick(r.i) }

// CreateRenderer implements fyne.Widget.
func (r *selectRow) CreateRenderer() fyne.WidgetRenderer {
	th := r.Theme()
	r.bg = canvas.NewRectangle(themeColor(th, theme.ColorNameFocus))
	r.bg.CornerRadius = th.Size(theme.SizeNameMenuRadius)
	check := canvas.NewImageFromResource(themedIcon(theme.ConfirmIcon(), theme.ColorNameForeground))
	check.FillMode = canvas.ImageFillContain
	text := canvas.NewText(r.text, themeColor(th, theme.ColorNameForeground))
	text.TextSize = th.Size(theme.SizeNameText)
	rr := &selectRowRenderer{r: r, bg: r.bg, check: check, text: text}
	rr.Refresh()
	return rr
}

type selectRowRenderer struct {
	r     *selectRow
	bg    *canvas.Rectangle
	check *canvas.Image
	text  *canvas.Text
}

func (rr *selectRowRenderer) Layout(size fyne.Size) {
	_, _, textX, iconW := selectInsets()
	th := rr.r.Theme()
	pad, inner := th.Size(theme.SizeNamePadding), th.Size(theme.SizeNameInnerPadding)
	rr.bg.Move(fyne.NewPos(pad/2, 0))
	rr.bg.Resize(fyne.NewSize(size.Width-pad, size.Height))
	rr.check.Move(fyne.NewPos(inner, (size.Height-iconW)/2))
	rr.check.Resize(fyne.NewSquareSize(iconW))
	room := size.Width - textX - 2*inner
	if shown := ellipsize(rr.r.text, room, rr.text.TextSize, rr.text.TextStyle); shown != rr.text.Text {
		rr.text.Text = shown
		rr.text.Refresh()
	}
	ts := rr.text.MinSize()
	rr.text.Move(fyne.NewPos(textX, (size.Height-ts.Height)/2))
	rr.text.Resize(ts)
}

func (rr *selectRowRenderer) MinSize() fyne.Size {
	_, _, textX, iconW := selectInsets()
	inner := rr.r.Theme().Size(theme.SizeNameInnerPadding)
	ts := fyne.MeasureText(rr.r.text, rr.text.TextSize, rr.text.TextStyle)
	return fyne.NewSize(textX+ts.Width+2*inner, max(ts.Height, iconW))
}

func (rr *selectRowRenderer) Refresh() {
	th := rr.r.Theme()
	rr.bg.FillColor = themeColor(th, theme.ColorNameFocus)
	rr.bg.Hidden = !rr.r.active
	rr.check.Hidden = !rr.r.current
	rr.text.Color = themeColor(th, theme.ColorNameForeground)
	rr.text.Text = rr.r.text
	rr.Layout(rr.r.Size())
	rr.bg.Refresh()
	rr.text.Refresh()
}

func (rr *selectRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{rr.bg, rr.check, rr.text}
}

func (rr *selectRowRenderer) Destroy() {}
