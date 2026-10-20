package fyneui

import (
	"errors"
	"fmt"
	"runtime"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Row checkboxes and actions on a selection (ui-parity › Tables, G5, G7, G9). Captured (2026-10-11):
// a checkbox column at the left of every list, the header's checkbox selects every row. The
// selection is the engine's (UIState.Selected, set with ui.select), so a checkbox, a click and an
// agent change the same selection, and each tab keeps its own.
//
// The owner's decision (A73, 2026-10-14 evening; built 2026-10-15): a plain click on a row opens its
// drawer and keeps the checked rows checked (ui.select's mode open, openDetails); the row is
// highlighted as the drawer's while its checkbox stays as it was. So are ↵ and Space on a row, a
// click on a link to the row's own object and the row menu's Show Details.
//
// Cogpit's defaults (inferred, not the owner's decision, 2026-10-13):
//   - a checkbox toggles its row without opening the drawer;
//   - ⌘-click (Ctrl-click off macOS) toggles a row, ⇧-click (on a row or a checkbox) adds the rows
//     from the last row clicked or checked, neither opens the drawer; ⌘ and ⇧ are the click's own,
//     from its mouse event, never the driver's memory of the last key (the lead's choice of
//     2026-10-19, architecture.md §4.4);
//   - the header's checkbox selects the rows the view shows (the namespace filter and the search
//     applied), or clears the selection when every one of them is selected already; it shows a
//     dash while some are;
//   - with two objects or more selected, the header shows "N selected", Delete and a clear button,
//     and the row menu of a selected row acts on the selection (Delete, Clear Selection); Delete is
//     the only action on a selection, the others stay on one object.
//
// The owner's decision (N112, 2026-10-16 evening): while exactly one row is checked, that row's
// menu is the object's full menu with Clear Selection after a separator; from two checked rows the
// selection's menu as above.

// checkColumn is the table column of the row checkboxes; the view's visible columns follow it from
// firstDataColumn, and the ⋮ ends the row (tableView.menuColumn).
const (
	checkColumn     = 0
	firstDataColumn = 1
)

// The header checkbox's states.
const (
	checkNone = iota // no row the view shows is selected
	checkSome        // some are
	checkAll         // every one is
)

// minSelectionBar is the selection size from which the header shows the selection's actions:
// one checked row (owner, 2026-10-15; was 2).
const minSelectionBar = 1

// minSelectionMenu is the selection size from which a checked row's menu acts on the selection:
// the row menu of the one checked object acts on that object, with every action of one object, and
// ends with Clear Selection (loneChecked, N112).
const minSelectionMenu = 2

// selectModeParams are ui.select's parameters with its mode (engine/commands/ui.go).
type selectModeParams struct {
	Refs     []model.ObjectRef `json:"refs"`
	Mode     string            `json:"mode,omitempty"`
	All      bool              `json:"all,omitempty"`
	NoDrawer bool              `json:"noDrawer"`
}

// keyEventModifiers returns the modifier keys of the key event being handled, for ⇧-Space on a
// checkbox. Fyne's desktop driver keeps the modifiers of the last key event it delivered and writes
// them before it hands the key on (internal/driver/glfw/window_desktop.go:714 in v2.8.2), so they are
// exact while a key is handled. They are not the keys held now: nothing in Fyne resets them when
// the window loses the focus, and GLFW drops the release of a key it never saw pressed, so a ⌘ that
// went down outside the window can stay "held" until the next key event. A click takes its keys
// from its own mouse event instead (tableCell.MouseDown, clickModifiers): a click once toggled every
// row it hit as if ⌘ were held (G16, 2026-10-19). The test driver has no modifiers, so tests replace
// it.
var keyEventModifiers = func() fyne.KeyModifier {
	if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()
	}
	return 0
}

// clickModifiers turns a mouse event's modifier keys into the keys' convention, in which
// fyne.KeyModifierShortcutDefault is ⌘ on macOS and Ctrl elsewhere. Fyne's GLFW driver reports ⌘ in
// a mouse event as Control on macOS, and a Ctrl-click there as a secondary click without it
// (convertMouseButton, internal/driver/glfw/window_desktop.go:486–497 in v2.8.2); elsewhere Ctrl
// stays Control.
func clickModifiers(m fyne.KeyModifier) fyne.KeyModifier {
	if runtime.GOOS == goosMac && m&fyne.KeyModifierControl != 0 {
		m = m&^fyne.KeyModifierControl | fyne.KeyModifierSuper
	}
	return m
}

// selRef identifies a row's object in the open view: comparing it allocates nothing, so that each
// cell update asks the selection in O(1).
type selRef struct{ kind, namespace, name string }

func keyOf(r model.ObjectRef) selRef { return selRef{r.Kind, r.Namespace, r.Name} }

// tableChecks is the table's selection as the checkboxes show it: the engine's selection as a set,
// the anchor of ⇧-click, the header's checkbox and the selection bar.
type tableChecks struct {
	set    map[selRef]bool
	anchor *model.ObjectRef // the row last clicked or checked
	head   *headerCell      // the header cell that shows the checkbox, while it does
	state  int              // the header checkbox's state
	bar    *selectionBar
}

// setSelection takes the engine's selection.
func (c *tableChecks) setSelection(sel []model.ObjectRef) {
	c.set = make(map[selRef]bool, len(sel))
	for _, r := range sel {
		c.set[keyOf(r)] = true
	}
}

// has reports whether an object is selected.
func (c *tableChecks) has(r model.ObjectRef) bool { return c.set[keyOf(r)] }

// showHeader makes a header cell the header's checkbox.
func (c *tableChecks) showHeader(h *headerCell) {
	c.head = h
	h.setCheck(checkColumn, c.state)
}

// dataColumn returns the visible column a table column shows, or false for the checkbox and the ⋮.
func (tv *tableView) dataColumn(col int) (int, bool) {
	i := col - firstDataColumn
	return i, i >= 0 && i < len(tv.cols)
}

// checksChanged follows the selection or the rows: the header's checkbox and the selection bar.
// It visits every row once to count the selected ones the view shows.
func (tv *tableView) checksChanged() {
	state := checkNone
	if tv.snap != nil && len(tv.check.set) > 0 {
		n := 0
		for i := range tv.snap.Rows {
			if tv.check.has(tv.snap.Rows[i].Ref) {
				n++
			}
		}
		switch {
		case n > 0 && n == len(tv.snap.Rows):
			state = checkAll
		case n > 0:
			state = checkSome
		}
	}
	tv.check.state = state
	if h := tv.check.head; h != nil && h.box && h.col == checkColumn {
		h.setCheck(checkColumn, state)
	}
	if tv.check.bar != nil {
		tv.check.bar.update(len(tv.selected))
	}
}

// openDetails opens the drawer on an object of the open view as a plain click on its row does:
// ui.select's mode open, which leaves the selection, the checked rows, as it is (A73).
func (sh *shell) openDetails(ref model.ObjectRef) {
	sh.dispatch("ui.select", selectModeParams{Refs: []model.ObjectRef{ref}, Mode: string(engine.SelectOpen)}, nil)
}

// selectRows dispatches ui.select with a mode, never opening the drawer.
func (tv *tableView) selectRows(refs []model.ObjectRef, mode engine.SelectMode) {
	if refs == nil {
		refs = []model.ObjectRef{}
	}
	tv.sh.dispatch("ui.select", selectModeParams{Refs: refs, Mode: string(mode), NoDrawer: true}, nil)
}

// checkTapped toggles a row's checkbox; with ⇧ in mods (the click's, or the key's for Space and ↵)
// it adds the rows from the anchor to it.
func (tv *tableView) checkTapped(row int, mods fyne.KeyModifier) {
	if tv.snap == nil || row < 0 || row >= len(tv.snap.Rows) {
		return
	}
	if mods&fyne.KeyModifierShift != 0 && tv.selectRange(row) {
		return
	}
	ref := tv.snap.Rows[row].Ref
	tv.check.anchor = &ref
	tv.selectRows([]model.ObjectRef{ref}, engine.SelectToggle)
}

// modifiedClick handles a click on a row with ⌘ (Ctrl off macOS) or ⇧ in its own modifier keys
// (clickModifiers), and reports whether it did; a plain click selects the row through the table.
func (tv *tableView) modifiedClick(row int, mods fyne.KeyModifier) bool {
	if tv.snap == nil || row < 0 || row >= len(tv.snap.Rows) {
		return false
	}
	switch {
	case mods&fyne.KeyModifierShortcutDefault != 0:
		ref := tv.snap.Rows[row].Ref
		tv.check.anchor = &ref
		tv.selectRows([]model.ObjectRef{ref}, engine.SelectToggle)
		return true
	case mods&fyne.KeyModifierShift != 0:
		if !tv.selectRange(row) {
			ref := tv.snap.Rows[row].Ref
			tv.check.anchor = &ref
			tv.selectRows([]model.ObjectRef{ref}, engine.SelectToggle)
		}
		return true
	}
	return false
}

// selectRange adds the rows from the anchor to row, in the table's order, and reports whether it
// could: false without an anchor the view shows.
func (tv *tableView) selectRange(row int) bool {
	if tv.check.anchor == nil {
		return false
	}
	from := tv.snap.IndexOf(*tv.check.anchor)
	if from < 0 {
		return false
	}
	lo, hi := min(from, row), max(from, row)
	refs := make([]model.ObjectRef, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		refs = append(refs, tv.snap.Rows[i].Ref)
	}
	tv.selectRows(refs, engine.SelectAdd)
	return true
}

// toggleAll is the header's checkbox: every row the view shows, or none when all of them are
// selected already.
func (tv *tableView) toggleAll() {
	if tv.snap == nil {
		return
	}
	if tv.check.state == checkAll {
		tv.clearSelection()
		return
	}
	tv.sh.dispatch("ui.select", selectModeParams{Refs: []model.ObjectRef{}, All: true, NoDrawer: true}, nil)
}

// clearSelection empties the selection and leaves the drawer open.
func (tv *tableView) clearSelection() {
	tv.selectRows(nil, engine.SelectReplace)
}

// inSelection reports whether a row's menu acts on the selection: the row is selected, with others.
func (tv *tableView) inSelection(row int) bool {
	return len(tv.selected) >= minSelectionMenu && tv.rowChecked(row)
}

// loneChecked reports whether ref is the one checked object: its row menu is the object's and
// ends with Clear Selection (the owner, 2026-10-16 evening, N112).
func (tv *tableView) loneChecked(ref model.ObjectRef) bool {
	return len(tv.selected) == 1 && tv.check.has(ref)
}

// endWithClearSelection ends a lone checked row's menu with a separator and Clear Selection.
func (tv *tableView) endWithClearSelection(m *fyne.Menu) {
	m.Items = append(m.Items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Clear Selection", tv.clearSelection))
}

// selectionAction is an action on a selection evaluated off the main goroutine.
type selectionAction struct {
	cmd    *engine.Command
	reason string
}

// evaluateSelection returns the actions on a selection that apply to its objects, each with why
// it cannot run now. It reads the cache through Enabled: call it off the main goroutine.
func (sh *shell) evaluateSelection(refs []model.ObjectRef) []selectionAction {
	params := engine.SelectionParams(refs)
	cmds := sh.eng.Registry().SelectionActions(engine.SelectionKinds(refs))
	out := make([]selectionAction, 0, len(cmds))
	for _, cmd := range cmds {
		a := selectionAction{cmd: cmd}
		err := sh.commandEnabled(cmd.ID, params)
		switch {
		case errors.Is(err, engine.ErrNotApplicable):
			continue
		case err != nil:
			a.reason = err.Error()
		}
		out = append(out, a)
	}
	return out
}

// selectionMenu is the row menu of a selected row while several are selected: what the selection
// is, its actions, and Clear Selection.
func (tv *tableView) selectionMenu(refs []model.ObjectRef, acts []selectionAction) *fyne.Menu {
	title := fyne.NewMenuItem(fmt.Sprintf("%d objects selected", len(refs)), nil)
	title.Disabled = true
	items := make([]*fyne.MenuItem, 0, len(acts)+4)
	items = append(items, title, fyne.NewMenuItemSeparator())
	for _, a := range acts {
		item := fyne.NewMenuItem(a.cmd.Label, nil)
		if a.reason != "" {
			item.Label = disabledLabel(a.cmd.Label, a.reason)
			item.Disabled = true
		} else {
			id := a.cmd.ID
			item.Action = func() { tv.runOnSelection(id, refs) }
		}
		items = append(items, item)
	}
	items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Clear Selection", tv.clearSelection))
	return fyne.NewMenu("", items...)
}

// openSelectionMenu evaluates the actions on the selection off the main goroutine and shows their
// menu at pos (window coordinates), unless the selection changed meanwhile.
func (tv *tableView) openSelectionMenu(pos fyne.Position) {
	sh := tv.sh
	refs := slices.Clone(tv.selected)
	go func() {
		defer sh.recoverTo("selection menu")
		acts := sh.evaluateSelection(refs)
		sh.do(func() {
			if !slices.Equal(tv.selected, refs) {
				return
			}
			c := fyne.CurrentApp().Driver().CanvasForObject(tv.table)
			if c == nil {
				return
			}
			tv.menuModel = tv.selectionMenu(refs, acts)
			tv.menu = widget.NewPopUpMenu(tv.menuModel, c)
			tv.menu.ShowAtPosition(pos)
		})
	}()
}

// runOnSelection runs an action on the selection's objects; the engine asks the user to confirm
// a destructive one, naming every object.
func (tv *tableView) runOnSelection(id string, refs []model.ObjectRef) {
	if open, ok := selectionDialogs[id]; ok { // an action that asks first (Argo CD's Sync and Refresh)
		open(tv.sh, refs)
		return
	}
	tv.sh.dispatch(id, engine.SelectionParams(refs), tv.sh.actionDone(id))
}

// selectionBar is the header's part for a selection: "3 selected", Delete and a button that clears
// the selection (Cogpit's placement, inferred). It shows from one checked row (owner, 2026-10-15)
// and then takes the place of the view's item count and CSV button, with Delete and Clear as icon
// buttons with tooltips, so that it fits beside the filters of a view narrowed by an open drawer
// (the lead's choice for the owner to confirm).
type selectionBar struct {
	tv    *tableView
	count *textLine
	del   *tipButton
	plate *dangerPlate // del's outlined plate (chrome_actions.go)
	clear *tipButton
	root  *fyne.Container
	// asked is the connection (its context and state) the engine was last asked about why Delete
	// cannot run; "" while Delete works.
	asked string
}

// deleteManyID is the command the selection bar's Delete runs.
const deleteManyID = "object.deleteMany"

// deleteSelectedTip is the selection bar's Delete's tooltip while it works.
const deleteSelectedTip = "Delete the selected objects…"

func newSelectionBar(tv *tableView) *selectionBar {
	b := &selectionBar{tv: tv}
	b.count = newTextLine("", colorAccent, theme.SizeNameText, fyne.TextStyle{Bold: true})
	b.del = newTipButton(tv.sh, theme.NewErrorThemedResource(theme.DeleteIcon()), deleteSelectedTip, func() {
		tv.runOnSelection(deleteManyID, slices.Clone(tv.selected))
	})
	b.plate = newDangerPlate(b.del) // an outlined plate, not a solid red block (chrome_actions.go)
	b.clear = newTipButton(tv.sh, theme.CancelIcon(), "Clear the selection", tv.clearSelection)
	b.root = container.NewHBox(newHGap(theme.SizeNamePadding), b.count, b.plate, b.clear)
	b.root.Hide()
	return b
}

// update shows the bar for n selected objects, from minSelectionBar on.
func (b *selectionBar) update(n int) {
	if n < minSelectionBar {
		b.root.Hide()
	} else {
		b.count.SetText(formatCount(int64(n)) + " selected")
		b.root.Show()
	}
	b.followConnection()
	b.tv.showHeadingCount()
}

// followConnection greys Delete while the shown cluster's connection is not connected or its login
// lapsed: the engine refuses every write then (D-58, D-69), so the button says so rather than a
// refusal after the click. Its tooltip then names the engine's reason as the row menus do, "Delete
// — connection interrupted" (the owner's audit decision of 2026-10-16), and says what Delete does
// again once the connection is back.
func (b *selectionBar) followConnection() {
	ui := b.tv.sh.ui
	entry, _ := ui.ConnectionOf(ui.Context)
	if ui.Connection.State == engine.Connected && !entry.LoginLapsed {
		if b.del.Disabled() {
			b.del.Enable()
		}
		b.asked = ""
		b.del.setTip(deleteSelectedTip)
		b.plate.Refresh()
		return
	}
	if !b.del.Disabled() {
		b.del.Disable()
	}
	b.plate.Refresh()
	at := fmt.Sprintf("%s\x00%s\x00%t", ui.Context, ui.Connection.State, entry.LoginLapsed)
	if at != b.asked && b.root.Visible() {
		b.asked = at
		b.askReason(at)
	}
}

// askReason asks the engine off the main goroutine why Delete cannot run on the selection (its
// Enabled reads the cache) and names the reason in Delete's tooltip, unless the connection changed
// meanwhile. No reason (the connection came back in between) leaves the tooltip as it is.
func (b *selectionBar) askReason(at string) {
	sh := b.tv.sh
	refs := slices.Clone(b.tv.selected)
	go func() {
		defer sh.recoverTo("the selection's Delete")
		label := "Delete"
		if cmd, ok := sh.eng.Registry().Get(deleteManyID); ok {
			label = cmd.Label
		}
		err := sh.commandEnabled(deleteManyID, engine.SelectionParams(refs))
		sh.do(func() {
			if err != nil && b.asked == at {
				b.del.setTip(disabledLabel(label, err.Error()))
			}
		})
	}()
}

// setCheck makes the header cell the header's checkbox in a state.
func (h *headerCell) setCheck(col, state int) {
	h.col = col
	if h.box && h.boxed == state && h.title == "" && h.sort == 0 && !h.menu {
		return
	}
	h.title, h.sort, h.menu, h.box, h.boxed = "", 0, false, true, state
	h.Refresh()
}

// headerCheckIcon is the header checkbox's icon and colour for a state.
func headerCheckIcon(state int) (fyne.Resource, fyne.ThemeColorName) {
	switch state {
	case checkAll:
		return theme.Icon(theme.IconNameCheckButtonChecked), colorAccent
	case checkSome:
		return theme.Icon(theme.IconNameCheckButtonPartial), colorAccent
	}
	return theme.Icon(theme.IconNameCheckButton), colorMuted
}

// checkIconKey is a row checkbox's icon: checked on a selected row, in the selected row's text
// colour, else empty in the muted colour.
func checkIconKey(selected bool) cellIconKey {
	if selected {
		return cellIconKey{theme.IconNameCheckButtonChecked, colorRowSelectedText}
	}
	return cellIconKey{theme.IconNameCheckButton, colorMuted}
}
