package fyneui

import (
	"image/color"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The view header's namespace filter (on Definitions the API group filter, the same list): the
// button opens a list below it with a search field on top, "All namespaces" and a row per namespace,
// each a check box and the name (the owner's F21, 2026-10-17; ui-parity › Tables).
//
//   - The search field has the keyboard as soon as the list opens. Typing narrows the rows to the
//     names that contain the text, ignoring case; "All namespaces" is listed only while the field is
//     empty, so that ↵ after typing always picks a namespace. The list shrinks to the matches below
//     the field, which stays where it is; the text starts empty at every opening.
//   - A click on a name filters to that namespace alone and closes the list ("All namespaces"' name:
//     every namespace); a click on a check box adds or removes that namespace and keeps the list open,
//     the keyboard back in the search field.
//   - ↑↓ (Page Up, Page Down) move a highlight, which starts on nothing when the list opens and on the
//     first match once text is typed; ↵ applies the highlighted row as a click on its name, and closes
//     the list unchanged when nothing is highlighted; Space toggles the highlighted row's box (a
//     namespace's or an API group's name has no spaces); Escape clears the text, then closes the list;
//     F1–F12 close it and go to the hotbar, as in every search field (A64).
//
// Every change of the filter is ui.setNamespace (ui.setGroups), so agents see and set the same state
// (AGENTS.md rule 2). The typed text and the highlight are the window's own, as the list itself is:
// they change what the list offers, not what a view shows (the lead's decision, 2026-10-17, open to
// revision; the precedent is the navigator's open groups, architecture §4.3). Lens's namespace filter,
// inferred and not captured: a type-to-filter field and a check per row.
type nsFilter struct {
	tv     *tableView
	popup  *widget.PopUp
	search *nsSearchEntry
	list   *fyne.Container // the rows the text matches, or the empty line
	scroll *container.Scroll
	empty  *textLine
	rows   []*nsRow // every row, "All namespaces" (name "") first
	shown  []*nsRow // the rows the text matches, in the list's order
	sel    int      // the highlighted row of shown; -1: none
	sync   bool     // set while the boxes follow the engine's state, or the field is emptied to open
	width  float32  // the list's width, fixed while it is open: its widest row's or the button's
}

func newNSFilter(tv *tableView) *nsFilter {
	f := &nsFilter{tv: tv, sel: -1}
	f.search = newNSSearchEntry(f)
	f.empty = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	f.list = container.NewVBox()
	f.scroll = newVScroll(f.list)
	return f
}

func (f *nsFilter) visible() bool { return f.popup != nil && f.popup.Visible() }

// openNamespaces shows the namespace filter's list below its button with an empty search field that
// has the keyboard; the namespaces are read from the cache again at every opening, so that one
// created since is in the list (WF10, shell.loadNamespaces).
func (tv *tableView) openNamespaces() {
	f := tv.ns
	c := fyne.CurrentApp().Driver().CanvasForObject(tv.nsButton)
	if c == nil {
		return
	}
	f.sync = true // the text is cleared, not typed
	f.search.SetText("")
	f.sync = false
	f.sel = -1 // nothing highlighted until the user types or moves
	f.fill()
	if f.popup == nil || f.popup.Canvas != c {
		f.popup = widget.NewPopUp(container.NewBorder(f.search, nil, nil, nil, f.scroll), c)
	}
	f.show()
	if !tv.groupMode() && tv.sh.namespacesListable() {
		tv.sh.loadNamespaces()
	}
}

// namespacesLoaded fills an open list with the newly loaded names, keeping the typed text and the
// highlighted row; the button follows a namespace gone or going (nsgone.go).
func (tv *tableView) namespacesLoaded() {
	tv.updateNamespaceButton()
	if f := tv.ns; f.visible() {
		f.refill()
	}
}

// refill makes an open list's rows again, keeping the typed text and the highlighted row, and
// shows it again at its new width.
func (f *nsFilter) refill() {
	f.fill()
	f.popup.Hide()
	f.show()
}

// show shows the list below the button, as wide as its widest row (at least the button's width plus
// a cell's padding, as before), with the keyboard in the search field.
func (f *nsFilter) show() {
	th := f.tv.table.Theme()
	f.width = th.Size(sizeFilterWidth) + th.Size(sizeCellPadding)*2
	for _, r := range f.rows {
		f.width = max(f.width, r.MinSize().Width)
	}
	f.fit()
	f.popup.Resize(f.popup.MinSize()) // a list shown before may have been taller
	p := f.popup
	onShown(p.Canvas, func() { p.ShowAtRelativePosition(fyne.NewPos(0, f.tv.nsButton.Size().Height), f.tv.nsButton) },
		f.hide) // its search's Escape first, this while the search has no keyboard (escape.go)
	f.popup.Canvas.Focus(f.search)
	f.reveal()
}

// fit makes the list as tall as the rows the text matches, at most the popup's tallest list, so that
// typing shrinks it below the search field, which stays where it is.
func (f *nsFilter) fit() {
	h := min(f.list.MinSize().Height, f.tv.table.Theme().Size(sizePopupMaxList))
	f.scroll.SetMinSize(fyne.NewSize(f.width, h))
	if f.visible() {
		f.popup.Resize(f.popup.MinSize())
	}
}

func (f *nsFilter) hide() {
	if f.visible() {
		f.popup.Hide()
	}
}

// fill makes a row for "All namespaces" and each namespace (or API group) the filter offers, then
// lists those the text matches; the highlighted row stays highlighted while it is listed.
func (f *nsFilter) fill() {
	all, choices, _ := f.tv.filterChoices()
	lit := f.highlighted()
	f.rows = f.rows[:0]
	f.rows = append(f.rows, newNSRow(f, "", all, nsLive))
	for _, n := range choices {
		state := nsLive
		if !f.tv.groupMode() {
			state = f.tv.sh.namespaceState(n) // deleted or terminating: marked red (nsgone.go, GW9)
		}
		f.rows = append(f.rows, newNSRow(f, n, n, state))
	}
	f.search.SetPlaceHolder("Search " + strings.ToLower(strings.TrimPrefix(all, "All ")) + "…")
	f.empty.SetText("No " + strings.ToLower(strings.TrimPrefix(all, "All ")) + " match.")
	f.syncChecks()
	f.filter()
	f.sel = -1
	for i, r := range f.shown {
		if lit != nil && r.name == lit.name {
			f.sel = i
		}
	}
	if f.sel < 0 && f.query() != "" && len(f.shown) > 0 {
		f.sel = 0 // typed text highlights its first match
	}
	f.light()
}

// highlighted is the highlighted row, or nil.
func (f *nsFilter) highlighted() *nsRow {
	if f.sel < 0 || f.sel >= len(f.shown) {
		return nil
	}
	return f.shown[f.sel]
}

// query is the typed text as the rows are matched against it.
func (f *nsFilter) query() string { return strings.ToLower(strings.TrimSpace(f.search.Text)) }

// filter lists the rows the text matches: every row while it is empty, else the names containing it.
func (f *nsFilter) filter() {
	q := f.query()
	f.shown = f.shown[:0]
	objs := make([]fyne.CanvasObject, 0, len(f.rows))
	for _, r := range f.rows {
		if q == "" || (r.name != "" && strings.Contains(strings.ToLower(r.name), q)) {
			f.shown = append(f.shown, r)
			objs = append(objs, r)
		}
	}
	if len(objs) == 0 {
		objs = append(objs, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, f.empty))
	}
	f.list.Objects = objs
	f.list.Refresh()
	f.fit()
	f.scroll.Refresh()
}

// typed follows the user's typing: the list narrows and the first match is highlighted.
func (f *nsFilter) typed(string) {
	if f.sync {
		return
	}
	f.filter()
	f.sel = -1
	if f.query() != "" && len(f.shown) > 0 {
		f.sel = 0
	}
	f.light()
	f.scroll.ScrollToTop()
	f.reveal()
}

// light marks the highlighted row.
func (f *nsFilter) light() {
	lit := f.highlighted()
	for _, r := range f.rows {
		r.setLit(r == lit)
	}
}

// move moves the highlight by d rows, stopping at the ends; from nothing, ↓ goes to the first row
// and ↑ to the last.
func (f *nsFilter) move(d int) {
	n := len(f.shown)
	switch {
	case n == 0:
		return
	case f.sel < 0 && d > 0:
		f.sel = 0
	case f.sel < 0:
		f.sel = n - 1
	default:
		f.sel = max(0, min(f.sel+d, n-1))
	}
	f.light()
	f.reveal()
}

// reveal scrolls the highlighted row into the list's view.
func (f *nsFilter) reveal() {
	r := f.highlighted()
	if r == nil {
		return
	}
	y, h := r.Position().Y, r.Size().Height
	off, view := f.scroll.Offset.Y, f.scroll.Size().Height
	switch {
	case y < off:
		off = y
	case y+h > off+view:
		off = y + h - view
	default:
		return
	}
	f.scroll.ScrollToOffset(fyne.NewPos(0, off))
}

// key handles the search field's keys that are the list's: true when it took the key.
func (f *nsFilter) key(ev *fyne.KeyEvent) bool {
	if functionKey(ev.Name) > 0 {
		f.hide()
		hotbarKey(f.tv.sh, ev)
		return true
	}
	switch ev.Name {
	case fyne.KeyUp:
		f.move(-1)
	case fyne.KeyDown:
		f.move(1)
	case fyne.KeyPageUp:
		f.move(-paletteJump)
	case fyne.KeyPageDown:
		f.move(paletteJump)
	case fyne.KeyReturn, fyne.KeyEnter:
		if r := f.highlighted(); r != nil {
			f.only(r.name)
		} else {
			f.hide()
		}
	case fyne.KeyEscape:
		if f.search.Text != "" {
			f.search.SetText("")
		} else {
			f.hide()
		}
	default:
		return false
	}
	return true
}

// space toggles the highlighted row's box, as a click on it does.
func (f *nsFilter) space() {
	if r := f.highlighted(); r != nil {
		r.check.SetChecked(!r.check.Checked)
	}
}

// only filters to one namespace (none: every namespace) and closes the list: a click on a name, ↵.
func (f *nsFilter) only(name string) {
	next := []string{}
	if name != "" {
		next = []string{name}
	}
	f.hide()
	f.set(next)
}

// syncChecks makes the boxes show the engine's namespaces (or groups).
func (f *nsFilter) syncChecks() {
	_, _, sel := f.tv.filterChoices()
	f.sync = true
	for _, r := range f.rows {
		want := len(sel) == 0
		if r.name != "" {
			want = slices.Contains(sel, r.name)
		}
		if r.check.Checked != want {
			r.check.SetChecked(want)
		}
	}
	f.sync = false
}

// syncNamespaceChecks makes an open list's boxes show the engine's namespaces (or groups); a closed
// one follows when it opens. A deleted namespace unticked leaves the list (GW9): the list is made
// again when the namespaces it offers changed.
func (tv *tableView) syncNamespaceChecks() {
	f := tv.ns
	if !f.visible() {
		return
	}
	if _, choices, _ := tv.filterChoices(); !f.offers(choices) {
		f.refill()
		return
	}
	f.syncChecks()
}

// offers reports whether the list's rows are "All namespaces" and the names given, in order.
func (f *nsFilter) offers(names []string) bool {
	if len(f.rows) != len(names)+1 {
		return false
	}
	for i, n := range names {
		if f.rows[i+1].name != n {
			return false
		}
	}
	return true
}

// toggled follows a click on a box (or Space): "All namespaces" clears the filter, a namespace is
// added or removed; the list stays open and the keyboard goes back to the search field.
func (f *nsFilter) toggled(name string, on bool) {
	if f.sync {
		return
	}
	if f.visible() {
		f.popup.Canvas.Focus(f.search)
	}
	_, _, cur := f.tv.filterChoices()
	next := []string{}
	switch {
	case name == "":
		if !on { // unchecking "All namespaces" changes nothing
			f.syncChecks()
			return
		}
	case on:
		next = append(slices.Clone(cur), name)
	default:
		for _, n := range cur {
			if n != name {
				next = append(next, n)
			}
		}
	}
	f.set(next)
}

// set dispatches the filter: ui.setNamespace, or ui.setGroups on Definitions.
func (f *nsFilter) set(next []string) {
	if f.tv.groupMode() {
		f.tv.sh.dispatch("ui.setGroups", groupsParams{Groups: next}, nil)
		return
	}
	f.tv.sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: next}, nil)
}

// nsSearchEntry is the list's search field: the list takes ↑↓, ↵, Escape, F1–F12 and Space first.
type nsSearchEntry struct {
	widget.Entry
	f *nsFilter
}

// CreateRenderer implements fyne.Widget: the wheel passed on (wheelpass.go).
func (e *nsSearchEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}

func newNSSearchEntry(f *nsFilter) *nsSearchEntry {
	e := &nsSearchEntry{f: f}
	e.ExtendBaseWidget(e)
	e.SetIcon(theme.SearchIcon())
	e.OnChanged = f.typed
	return e
}

// TypedKey implements fyne.Focusable.
func (e *nsSearchEntry) TypedKey(ev *fyne.KeyEvent) {
	if e.f.key(ev) {
		return
	}
	e.Entry.TypedKey(ev)
}

// TypedRune implements fyne.Focusable: Space toggles the highlighted row instead of typing a space,
// which no namespace's name has.
func (e *nsSearchEntry) TypedRune(r rune) {
	if r == ' ' {
		e.f.space()
		return
	}
	e.Entry.TypedRune(r)
}

// nsRow is one row of the list: a check box that toggles the namespace, and its name, a click on
// which filters to it alone. The row under the pointer and the keyboard's row are highlighted.
type nsRow struct {
	widget.BaseWidget
	f       *nsFilter
	name    string // "" for "All namespaces"
	state   string // nsDeleted or nsTerminating: marked red (nsgone.go); nsLive otherwise
	check   *widget.Check
	label   *textLine
	bg      *canvas.Rectangle
	content fyne.CanvasObject
	hovered bool
	lit     bool
}

var (
	_ fyne.Tappable      = (*nsRow)(nil)
	_ desktop.Hoverable  = (*nsRow)(nil)
	_ desktop.Cursorable = (*nsRow)(nil)
)

// newNSRow makes a row; a namespace deleted or terminating (state, nsgone.go) reads red and says
// which after its name.
func newNSRow(f *nsFilter, name, text, state string) *nsRow {
	r := &nsRow{f: f, name: name, state: state, bg: canvas.NewRectangle(color.Transparent)}
	r.check = widget.NewCheck("", func(on bool) { f.toggled(name, on) })
	ink := theme.ColorNameForeground
	if state != nsLive {
		text, ink = text+" · "+state, colorStatusError
	}
	r.label = newTextLine(text, ink, theme.SizeNameText, fyne.TextStyle{})
	r.content = container.NewStack(r.bg, container.NewBorder(nil, nil, r.check, nil, r.label))
	r.ExtendBaseWidget(r)
	return r
}

// Tapped implements fyne.Tappable: a click beside the box, on the name.
func (r *nsRow) Tapped(*fyne.PointEvent) { r.f.only(r.name) }

// MouseIn implements desktop.Hoverable.
func (r *nsRow) MouseIn(*desktop.MouseEvent) { r.setHovered(true) }

// MouseMoved implements desktop.Hoverable.
func (r *nsRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *nsRow) MouseOut() { r.setHovered(false) }

// Cursor implements desktop.Cursorable.
func (r *nsRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (r *nsRow) setHovered(on bool) {
	if r.hovered != on {
		r.hovered = on
		r.Refresh()
	}
}

func (r *nsRow) setLit(on bool) {
	if r.lit != on {
		r.lit = on
		r.Refresh()
	}
}

// CreateRenderer implements fyne.Widget.
func (r *nsRow) CreateRenderer() fyne.WidgetRenderer {
	return &nsRowRenderer{r: r, simple: widget.NewSimpleRenderer(r.content)}
}

type nsRowRenderer struct {
	r      *nsRow
	simple fyne.WidgetRenderer
}

func (rr *nsRowRenderer) Layout(size fyne.Size)        { rr.simple.Layout(size) }
func (rr *nsRowRenderer) MinSize() fyne.Size           { return rr.simple.MinSize() }
func (rr *nsRowRenderer) Objects() []fyne.CanvasObject { return rr.simple.Objects() }
func (rr *nsRowRenderer) Destroy()                     {}

// Refresh colours the row: the keyboard's row as the palette's, the row under the pointer as a hover.
func (rr *nsRowRenderer) Refresh() {
	th := rr.r.Theme()
	bg := rr.r.bg
	bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	switch {
	case rr.r.lit:
		bg.FillColor = themeColor(th, theme.ColorNameSelection)
	case rr.r.hovered:
		bg.FillColor = themeColor(th, theme.ColorNameHover)
	default:
		bg.FillColor = color.Transparent
	}
	bg.Refresh()
	rr.simple.Refresh()
}
