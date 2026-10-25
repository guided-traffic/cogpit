package fyneui

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The view header's status filter (D-34, owner 2026-10-14; ui-parity › Tables): beside the
// namespace filter, a button naming the selection ("All states", the one state, "3 states") opens
// the states the view's rows carry (the snapshot's StateChoices): "All states", then a check per
// state with a dot in its colour and its count, under its column's title when the view has several
// state columns (an Argo CD Application's Sync and Health). Checks toggle without closing the list,
// as the namespace filter's do; the × beside the button clears the filter in one click. On a page
// the filter sits in its table's header beside the search, since it filters the table only. Every
// change is ui.setStatusFilter, so agents see and set the same state (AGENTS.md rule 2). Lens's
// views have no status filter: the look is Cogpit's (inferred from the namespace filter's).

// allStates is the status filter's label and first entry when nothing is selected.
const allStates = "All states"

// statusFilter is the header's status filter.
type statusFilter struct {
	tv     *tableView
	button *textButton
	clear  *textButton
	box    *fyne.Container // the filter's slot: the button and the × in the namespace filter's width

	popup  *widget.PopUp
	list   *fyne.Container
	checks []stateCheck // the popup's checks, "All states" first (state "")
	shown  []engine.StateCount
	sync   bool
}

// stateCheck is one check of the popup and the state it toggles ("" for All states).
type stateCheck struct {
	state string
	check *widget.Check
}

func newStatusFilter(tv *tableView) *statusFilter {
	f := &statusFilter{tv: tv}
	f.button = newPlateButton(allStates, chevronDownIcon(), f.open) // the find bar's chevron, at the right edge (VW4)
	f.button.IconPlacement = widget.ButtonIconTrailingText
	f.button.Alignment = widget.ButtonAlignLeading
	f.button.iconEnd = true
	f.clear = newPlateButton("", theme.ContentClearIcon(), f.clearAll)
	f.clear.Importance = widget.LowImportance
	f.clear.Hide()
	f.box = sized(sizeFilterWidth, "", container.NewBorder(nil, nil, nil, f.clear, f.button))
	return f
}

// selected is the open view's status filter, the engine's.
func (f *statusFilter) selected() []string { return f.tv.sh.ui.States }

// choices are the states the open view's snapshot offers; none while the snapshot is another view's.
func (f *statusFilter) choices() []engine.StateCount {
	snap := f.tv.snap
	if snap == nil || snap.View != f.tv.sh.ui.View {
		return nil
	}
	return snap.StateChoices
}

// label names the selection: "All states", the one state, or how many.
func (f *statusFilter) label() string {
	switch sel := f.selected(); len(sel) {
	case 0:
		return allStates
	case 1:
		return sel[0]
	default:
		return formatCount(int64(len(sel))) + " states"
	}
}

// update shows the engine's selection and the snapshot's states: the button's label, the button in
// the accent colour and the × while a state is selected (A80, owner 2026-10-14 evening), the button
// greyed while the rows carry no state to choose, an open list rebuilt when its states changed.
func (f *statusFilter) update() {
	sel := f.selected()
	importance := widget.MediumImportance
	if len(sel) > 0 {
		f.clear.Show()
		importance = widget.HighImportance // the theme's primary colour: the accent
	} else {
		f.clear.Hide()
	}
	if f.button.Importance != importance {
		f.button.Importance = importance
		f.button.Refresh()
	}
	th := fyne.CurrentApp().Settings().Theme()
	room := th.Size(sizeFilterWidth) - th.Size(theme.SizeNameInlineIcon) - 4*th.Size(theme.SizeNameInnerPadding)
	if len(sel) > 0 {
		room -= f.clear.MinSize().Width
	}
	if text := ellipsize(f.label(), room, th.Size(theme.SizeNameText), fyne.TextStyle{}); f.button.Text != text {
		f.button.SetText(text)
	}
	if len(f.choices()) == 0 && len(sel) == 0 {
		f.button.Disable()
	} else {
		f.button.Enable()
	}
	if f.popup == nil || !f.popup.Visible() {
		return
	}
	if slices.Equal(f.shown, f.choices()) {
		f.syncChecks()
		return
	}
	f.fill()
}

// open shows the list of states below the button.
func (f *statusFilter) open() {
	c := fyne.CurrentApp().Driver().CanvasForObject(f.button)
	if c == nil {
		return
	}
	f.list = container.NewVBox()
	f.fill()
	th := f.tv.table.Theme()
	scroll := newVScroll(f.list)
	h := f.list.MinSize().Height
	if maxH := th.Size(sizePopupMaxList); h > maxH {
		h = maxH
	}
	scroll.SetMinSize(fyne.NewSize(th.Size(sizeFilterWidth)+th.Size(sizeCellPadding)*2, h))
	f.popup = widget.NewPopUp(scroll, c)
	p := f.popup
	onShown(c, func() { p.ShowAtRelativePosition(fyne.NewPos(0, f.button.Size().Height), f.button) }, p.Hide) // Escape closes it (escape.go)
}

// fill makes the list show the snapshot's states: All states, then each state with its dot and
// count, under its column's title when the states come from several columns.
func (f *statusFilter) fill() {
	choices := f.choices()
	f.shown = slices.Clone(choices)
	f.checks = f.checks[:0]
	objs := []fyne.CanvasObject{f.row("", allStates, model.StatusNone, -1)}
	columns := map[string]bool{}
	for _, c := range choices {
		columns[c.Column] = true
	}
	heading := ""
	for i, c := range choices {
		if len(columns) > 1 && (i == 0 || c.Column != heading) {
			heading = c.Column
			title := heading
			if title == "" {
				title = "Any column"
			}
			objs = append(objs, inset(theme.SizeNameInnerPadding, "",
				newTextLine(title, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})))
		}
		text := c.State
		if rest, ok := strings.CutPrefix(text, c.Column+": "); ok && len(columns) > 1 && c.Column != "" {
			text = rest // the heading names the column
		}
		objs = append(objs, f.row(c.State, text, c.Status, c.Count))
	}
	if len(choices) == 0 {
		objs = append(objs, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
			newTextLine("The rows carry no states.", colorMuted, theme.SizeNameText, fyne.TextStyle{})))
	}
	if f.list != nil {
		f.list.Objects = objs
		f.list.Refresh()
	}
	f.syncChecks()
}

// row is one entry of the list: a check, a dot in the state's colour, its text, its count; a click
// anywhere on it toggles the check.
func (f *statusFilter) row(state, text string, status model.Status, count int) fyne.CanvasObject {
	check := widget.NewCheck("", func(on bool) { f.toggle(state, on) })
	f.checks = append(f.checks, stateCheck{state: state, check: check})
	label := newTextLine(text, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	left := []fyne.CanvasObject{check, newHGap(sizeDotDiameter)} // All states' text in line with the states'
	if state != "" {
		dc := statusColorName(status)
		if status == model.StatusNone {
			dc = colorMuted
		}
		left[1] = newDot(dc)
	}
	var right fyne.CanvasObject
	if count >= 0 {
		right = inset(theme.SizeNameInnerPadding, "", newTextLine(formatCount(int64(count)), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	}
	toggle := func() { check.SetChecked(!check.Checked) }
	return container.NewBorder(nil, nil, container.NewHBox(left...), right, newTapTarget(label, toggle))
}

// syncChecks makes the checks show the engine's selection.
func (f *statusFilter) syncChecks() {
	sel := f.selected()
	f.sync = true
	for _, c := range f.checks {
		want := len(sel) == 0
		if c.state != "" {
			want = slices.Contains(sel, c.state)
		}
		if c.check.Checked != want {
			c.check.SetChecked(want)
		}
	}
	f.sync = false
}

// toggle changes the filter: All states clears it, a state is added or removed.
func (f *statusFilter) toggle(state string, on bool) {
	if f.sync {
		return
	}
	cur := f.selected()
	next := []string{}
	switch {
	case state == "":
		if !on { // unchecking All states changes nothing
			f.syncChecks()
			return
		}
	case on:
		next = append(slices.Clone(cur), state)
	default:
		for _, s := range cur {
			if s != state {
				next = append(next, s)
			}
		}
	}
	f.set(next)
}

// clearAll clears the filter: the × beside the button.
func (f *statusFilter) clearAll() { f.set([]string{}) }

func (f *statusFilter) set(states []string) {
	f.tv.sh.dispatch("ui.setStatusFilter", statusFilterParams{States: states}, func(_ any, err error) {
		f.tv.sh.showError(err)
		f.update()
	})
}

// tableStateChanged reports whether the open view's filters or sort changed: its namespaces, API
// groups, search, search toggles, status filter or sort.
func tableStateChanged(st, old engine.UIState) bool {
	return !slices.Equal(st.Namespaces, old.Namespaces) || !slices.Equal(st.Groups, old.Groups) ||
		st.Filter != old.Filter || st.FilterOptions != old.FilterOptions || st.Sort != old.Sort || !slices.Equal(st.States, old.States) ||
		!slices.Equal(st.AlertBoard.Folded, old.AlertBoard.Folded) || !slices.Equal(st.AlertBoard.Expanded, old.AlertBoard.Expanded) // the alert board's folding (alertboard.go)
}

// statusFilterParams are ui.setStatusFilter's.
type statusFilterParams struct {
	States []string `json:"states"`
}
