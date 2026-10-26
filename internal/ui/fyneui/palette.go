package fyneui

import (
	"encoding/json"
	"image/color"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The command palette (G13; engine/palette.go): ⌘⇧P (Ctrl+Shift+P elsewhere) opens it over the
// window, on a shade a click outside closes: a filter field and the commands the window's menus
// offer in the current state (Registry.Palette), searchable by label and category. ↑↓ move, ↵
// runs, Escape or a click outside closes it. Running an entry dispatches its command as the menu
// item would (an object's action through the row menu's own path, runAction), so the confirmation
// (D-2), the parameter dialogs and the audit log apply unchanged. Its open state and its typed text
// are UI state (UIState.Palette, ui.palette.open and ui.palette.close; UIState.PaletteText,
// ui.palette.setText, the owner's A54 of 2026-10-14 evening), so that agents see them in ui_state,
// can open the palette and type into it: the window reports the user's typing a pause after the last
// key, as the navigator's search (navigatorstate.go), and shows an agent's text.
//
// An agent's text never chooses what the user's next ↵ runs (the lead's night decision, 2026-10-14,
// for the owner to confirm; D-51): on a text another origin typed last, ↵ runs a read or a UI entry
// (a view to switch to, a search), and a change (a mutating or destructive entry) runs only once the
// user picked it with the arrow keys or a click; the hint line says so. Typing makes the text the
// user's. F1–F12 close the palette and open the hotbar's cluster, as the entries listing them do
// (A64).
//
// Cogpit's reading, inferred (Lens's palette is captured only as "⌘⇧P opens it", "Switch to
// <resource>" and the hotbar's bank commands, which Cogpit's one hotbar has none of, D-83): a command that cannot run now stays in the list,
// greyed, with the reason after its label, as the menus show it; the list starts with the drawer
// object's actions; the filter matches every term, fuzzy as a last resort; the overlay has the
// size the context picker had (D-57 removed the picker).
type cmdPalette struct {
	sh *shell

	popup   *dismissOverlay
	filter  *keyEntry
	list    *widget.List
	empty   *textLine
	hint    *textLine
	content fyne.CanvasObject

	// The typed text as engine state (A54): reported as the user types; syncing while the engine's
	// text is being shown; byOther while the text shown is an agent's (or the CLI's, a link's);
	// picked once the user moved the highlight with the keys since; refused after a ↵ that ran no
	// change for want of it.
	textRep                           *navReport
	syncing, byOther, picked, refused bool

	all   []engine.PaletteEntry // what Registry.Palette returned for the state
	shown []engine.PaletteEntry // all, filtered by the text
	sel   int                   // the highlighted row of shown
	seq   int                   // the latest load; older answers are dropped
	// closing is set from the moment the window closed the palette itself (a run, Escape, a click
	// outside) until the engine's state follows, so that a state still saying open does not show
	// it again.
	closing bool
	// requested is set while the user's ⌘⇧P (or the menu item) waits for the engine: only then does
	// the palette take the keyboard focus. Opened by an agent it does not, so that what the user is
	// typing elsewhere cannot become a command run in the user's name (↵ on the first entry).
	requested bool
}

// paletteMaxCategory caps the category shown at a row's end, in runes.
const paletteMaxCategory = 40

// paletteHint is the hint line below the list while the user typed the text.
const paletteHint = "Up and Down to move · Enter to run · Escape to close"

// paletteTextParams are ui.palette.setText's parameters, mirroring the engine's schema.
type paletteTextParams struct {
	Text string `json:"text"`
}

func newCmdPalette(sh *shell) *cmdPalette {
	p := &cmdPalette{sh: sh}
	p.filter = newKeyEntry(p.key)
	p.filter.SetPlaceHolder("Type a command, a view or an object's action…")
	p.filter.SetIcon(theme.SearchIcon())
	p.filter.OnChanged = func(text string) {
		p.sel = 0
		if !p.syncing { // the user typed: the text is the user's
			p.byOther, p.picked, p.refused = false, false, false
			p.textRep.send(text, p.sh.ui.PaletteText)
			p.showHint()
		}
		p.apply()
	}
	p.textRep = newNavReport(sh, "ui.palette.setText", func(v string) any { return paletteTextParams{Text: v} }, p.applyText)
	p.list = widget.NewList(
		func() int { return len(p.shown) },
		func() fyne.CanvasObject { return newCmdPaletteRow() },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id < len(p.shown) {
				o.(*cmdPaletteRow).set(p.shown[id], id == p.sel)
			}
		})
	p.list.HideSeparators = true
	p.list.OnSelected = func(id widget.ListItemID) {
		p.list.UnselectAll()
		if id < len(p.shown) {
			p.sel = id
			p.run(p.shown[id])
		}
	}
	p.empty = newTextLine("No commands match.", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	p.empty.Hide()
	p.hint = newTextLine(paletteHint, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	p.hint.truncate = true

	listBg := newSurface(theme.ColorNameInputBackground)
	listBg.radius, listBg.stroke = theme.SizeNameInputRadius, colorHairline
	list := container.NewStack(listBg, inset(theme.SizeNamePadding, theme.SizeNamePadding,
		container.NewStack(p.list, container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, p.empty)))))
	top := container.NewVBox(p.filter, newSpace(theme.SizeNamePadding))
	bottom := container.NewVBox(newSpace(theme.SizeNamePadding), p.hint)
	p.content = sized(sizePickerWidth, sizePickerHeight,
		inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewBorder(top, bottom, nil, nil, list)))
	return p
}

// open asks the engine to open the palette (the menu item and ⌘⇧P); an open one takes the focus
// back to its filter.
func (p *cmdPalette) open() {
	if p.visible() {
		p.sh.win.Canvas().Focus(p.filter)
		return
	}
	p.requested = true
	p.sh.dispatch(engine.PaletteOpenCommand, struct{}{}, func(_ any, err error) {
		if err != nil {
			p.requested = false
			p.sh.showError(err)
		}
	})
}

// stateChanged follows UIState.Palette: shows the palette when it opened, hides it when it closed,
// and lists the entries again while it is open, since what applies follows the state.
func (p *cmdPalette) stateChanged() {
	if !p.sh.ui.Palette {
		p.closing = false
		p.hide()
		return
	}
	switch {
	case p.closing:
	case !p.visible():
		p.show()
	default:
		p.applyText() // an agent's text
		p.load()
	}
}

// applyText shows the engine's text (an agent's), unless a report of the user's typing waits or
// runs; it notes who typed it.
func (p *cmdPalette) applyText() {
	if p.textRep.busy > 0 || !p.visible() {
		return
	}
	want, other := p.sh.ui.PaletteText, p.sh.ui.PaletteTextByOther()
	if p.filter.Text != want {
		p.syncing = true
		p.filter.SetText(want)
		p.filter.CursorColumn = len([]rune(want))
		p.filter.Refresh()
		p.syncing = false
		p.picked, p.refused = false, false
	}
	if other != p.byOther {
		p.byOther, p.picked, p.refused = other, false, false
	}
	p.showHint()
}

// showHint says how ↵ works now: as always, or on another's text only for reads and the window.
func (p *cmdPalette) showHint() {
	text, c := paletteHint, colorMuted
	if p.byOther && !p.picked {
		text = textTyper(p.sh.ui.PaletteTextOrigin) + " typed this: Enter runs views and reads only · pick a change with Up and Down or a click"
		if p.refused {
			c = colorStatusWarning
		}
	}
	p.hint.SetText(text)
	p.hint.SetColor(c)
}

// textTyper names who typed the palette's text, for the hint.
func textTyper(o engine.Origin) string {
	switch o {
	case engine.OriginCLI:
		return "The command line"
	case engine.OriginDeepLink:
		return "A link"
	}
	return "An agent"
}

// paletteChanges reports whether an entry changes a cluster or Cogpit: what ↵ does not run on
// another's text until the user picked it.
func paletteChanges(d engine.Danger) bool {
	return d == engine.DangerMutating || d == engine.DangerDestructive
}

func (p *cmdPalette) visible() bool { return p.popup != nil && p.popup.Visible() }

// show opens the overlay with an empty filter and lists the entries. The filter takes the focus
// only when the user asked (requested); otherwise a click into it does.
func (p *cmdPalette) show() {
	if p.popup == nil {
		p.popup = newDismissOverlay(p.sh.win.Canvas(), p.content)
		p.popup.onDismiss = func() { p.dismissThen(nil) }
	}
	p.sh.tip.hide()
	p.syncing = true
	p.filter.SetText("")
	p.syncing = false
	p.byOther, p.picked, p.refused = false, false, false
	p.sel = 0
	p.popup.Show()
	// The palette lies over everything; Escape closes it while its filter has no keyboard (escape.go).
	onEscape(p.sh.win.Canvas(), p.popup, func() { p.dismissThen(nil) })
	if p.requested { // opened by an agent, the focus stays where the user's typing goes
		p.sh.win.Canvas().Focus(p.filter)
	}
	p.requested = false
	p.applyText() // the text an agent opened it with (ui.palette.setText)
	p.load()
}

func (p *cmdPalette) hide() {
	if p.popup != nil && p.popup.Visible() {
		p.popup.Hide()
	}
}

// dismissThen closes the palette at once and asks the engine to follow; then, once the engine's
// state says closed, runs next (nil: nothing), so that a command run from the palette finds it
// closed.
func (p *cmdPalette) dismissThen(next func()) {
	p.hide()
	p.textRep.cancel() // the engine forgets the text with the palette
	p.closing = true
	p.sh.dispatch(engine.PaletteCloseCommand, struct{}{}, func(_ any, err error) {
		p.closing = false
		if err != nil {
			p.sh.showError(err)
		}
		if next != nil {
			next()
		}
	})
}

// load lists the entries for the state off the main goroutine: Registry.Palette asks every
// command's Enabled, which reads the cache.
func (p *cmdPalette) load() {
	p.seq++
	seq := p.seq
	sh := p.sh
	in := engine.PaletteInput{State: sh.ui.Clone(), Views: sh.side.paletteViews(), Runs: func(id string) bool { return sh.paletteRunner(id) != nil }}
	reg, sess := sh.eng.Registry(), sh.sess
	go func() {
		defer sh.recoverTo("command palette")
		if ref, ok := engine.PaletteObject(in.State); ok {
			in.Containers = sh.containersFor(ref)
		}
		entries := reg.Palette(sess, in)
		sh.do(func() {
			if seq != p.seq || !p.visible() {
				return
			}
			p.all = entries[:0:0]
			for _, e := range entries {
				if sh.paletteApplies(e) {
					p.all = append(p.all, e)
				}
			}
			p.apply()
		})
	}()
}

// apply filters the entries by the text and shows them.
func (p *cmdPalette) apply() {
	p.shown = engine.FilterPalette(p.all, p.filter.Text)
	p.sel = max(0, min(p.sel, len(p.shown)-1))
	if len(p.shown) == 0 && p.all != nil {
		p.empty.Show()
	} else {
		p.empty.Hide()
	}
	p.list.Refresh()
	if len(p.shown) > 0 {
		p.list.ScrollTo(p.sel)
	}
}

// move moves the highlight by d rows, stopping at the ends.
func (p *cmdPalette) move(d int) {
	if len(p.shown) == 0 {
		return
	}
	p.sel = max(0, min(p.sel+d, len(p.shown)-1))
	p.list.Refresh()
	p.list.ScrollTo(p.sel)
}

// key handles the filter's keys that are the palette's: ↑↓, Page Up and Down (the user picking an
// entry), ↵, Escape, and F1–F12 (the hotbar's, after the palette closed; a shifted one is not, D-83).
func (p *cmdPalette) key(ev *fyne.KeyEvent) bool {
	if functionKey(ev.Name) > 0 && !shiftHeld() {
		name, sh := ev.Name, p.sh
		p.dismissThen(func() { sh.hot.key(name, false) })
		return true
	}
	switch ev.Name {
	case fyne.KeyUp:
		p.pick(-1)
	case fyne.KeyDown:
		p.pick(1)
	case fyne.KeyPageUp:
		p.pick(-paletteJump)
	case fyne.KeyPageDown:
		p.pick(paletteJump)
	case fyne.KeyReturn, fyne.KeyEnter:
		if p.sel < len(p.shown) {
			p.enter(p.shown[p.sel])
		}
	case fyne.KeyEscape:
		p.dismissThen(nil)
	default:
		return false
	}
	return true
}

// paletteJump is how far Page Up and Page Down move.
const paletteJump = 8

// pick moves the highlight by d rows as the user's choice: ↵ then runs the entry whoever typed the
// text.
func (p *cmdPalette) pick(d int) {
	p.move(d)
	if p.byOther && !p.picked {
		p.picked, p.refused = true, false
		p.showHint()
	}
}

// enter runs the highlighted entry (↵), unless another typed the text, the user has not picked the
// entry, and it is a change: then the hint says how to run it (A54).
func (p *cmdPalette) enter(e engine.PaletteEntry) {
	if p.byOther && !p.picked && paletteChanges(e.Danger) {
		p.refused = true
		p.showHint()
		return
	}
	p.run(e)
}

// run runs an entry as its menu item would, after the palette closed. A greyed one does nothing:
// its reason is on its row.
func (p *cmdPalette) run(e engine.PaletteEntry) {
	if e.Reason != "" {
		return
	}
	sh := p.sh
	p.dismissThen(func() {
		switch run := sh.paletteRunner(e.Command); {
		case run != nil && e.Ref == nil:
			run(sh, e)
		case e.Ref != nil:
			sh.runPaletteAction(e)
		default:
			sh.dispatch(e.Command, e.Params, nil)
		}
	})
}

// runPaletteAction runs an object's action through the row menu's path: evaluated again off the
// main goroutine, then runAction (a container, a dialog, the result's handling).
func (sh *shell) runPaletteAction(e engine.PaletteEntry) {
	ref := *e.Ref
	go func() {
		defer sh.recoverTo("command palette")
		acts := sh.evaluateActions(ref, sh.containersFor(ref))
		sh.do(func() {
			for _, a := range acts {
				if a.cmd.ID != e.Command {
					continue
				}
				if a.reason != "" {
					sh.toast("warning", a.cmd.Label+": "+a.reason)
					return
				}
				sh.runAction(a, ref, e.Container)
				return
			}
			sh.toast("warning", e.Label+" no longer applies to "+ref.Name)
		})
	}()
}

// paletteRunners run the commands whose parameters the window asks for in a dialog of its own,
// or whose result it presents (engine/palette.go, rules 3 and 7), as the menus and buttons that
// have them do: the navigator's section for a context (findcluster.go), the kubeconfig dialogs, the
// search field, the namespace and column menus, the CSV download.
// hotbar.openCluster goes through the hotbar, which asks for a login program's approval first.
var paletteRunners = map[string]func(sh *shell, e engine.PaletteEntry){
	connectCommand:          func(sh *shell, _ engine.PaletteEntry) { sh.findCluster() },
	"cluster.refreshAccess": func(sh *shell, _ engine.PaletteEntry) { sh.refreshAccessItem().Action() },
	"kubeconfig.addFile":    func(sh *shell, _ engine.PaletteEntry) { sh.kc.addKubeconfigs() },
	"kubeconfig.addFolder":  func(sh *shell, _ engine.PaletteEntry) { sh.kc.addKubeconfigs() },
	"kubeconfig.remove":     func(sh *shell, _ engine.PaletteEntry) { sh.kc.showSources() },
	"kubeconfig.sources":    func(sh *shell, _ engine.PaletteEntry) { sh.kc.showSources() },
	"table.exportCsv":       func(sh *shell, _ engine.PaletteEntry) { sh.view.exportCSV() },
	setFilterCommand:        func(sh *shell, _ engine.PaletteEntry) { sh.win.Canvas().Focus(sh.view.search) },
	"ui.setNamespace":       func(sh *shell, _ engine.PaletteEntry) { sh.view.openNamespaces() },
	"ui.setGroups":          func(sh *shell, _ engine.PaletteEntry) { sh.view.openNamespaces() },
	"ui.setColumns":         func(sh *shell, _ engine.PaletteEntry) { sh.view.openColumns() },
	"hotbar.openCluster": func(sh *shell, e engine.PaletteEntry) {
		var p hotbarSlotParams
		if err := json.Unmarshal(e.Params, &p); err == nil {
			sh.hot.open(p.Slot)
		}
	},
}

// paletteRunner returns how the window runs a command its own way, or nil: a runner, or for a
// create command its view's create dialog (createdialog.go).
func (sh *shell) paletteRunner(id string) func(*shell, engine.PaletteEntry) {
	if run := paletteRunners[id]; run != nil {
		return run
	}
	for kind, spec := range createDialogs {
		if spec.command == id {
			return func(sh *shell, _ engine.PaletteEntry) { sh.openCreateDialog(kind) }
		}
	}
	return nil
}

// paletteApplies leaves out what the open view does not have: the search, the columns and the CSV
// download without a list view, the namespace filter on a cluster-scoped view, the API group
// filter outside Definitions.
func (sh *shell) paletteApplies(e engine.PaletteEntry) bool {
	list := sh.ui.Connection.State.Live() && sh.ui.View != "" && sh.view.snap != nil
	switch e.Command {
	case setFilterCommand, "ui.setColumns", "table.exportCsv":
		return list
	case "ui.setNamespace":
		return list && !sh.view.groupMode() && sh.view.viewNamespaced()
	case "ui.setGroups":
		return list && sh.view.groupMode()
	}
	return true
}

// paletteViews are the views the navigator offers, in its order, for "Switch to": every node
// that is not a group, with the titles of the groups above it; a kind listed twice (a curated
// section and Custom Resources) is the same view.
func (s *sidebar) paletteViews() []engine.PaletteView {
	var out []engine.PaletteView
	var walk func(id string, path []string)
	walk = func(id string, path []string) {
		for _, c := range s.children[id] {
			if strings.HasPrefix(c, groupPrefix) {
				walk(c, append(path, s.titles[c]))
				continue
			}
			out = append(out, engine.PaletteView{View: strings.TrimPrefix(c, aliasPrefix), Title: s.titles[c], Group: strings.Join(path, " › ")})
		}
	}
	walk("", nil)
	return out
}

// keyEntry is a text field that lets its owner take keys before it: the palette's ↑↓, ↵ and
// Escape (a focused field gets every key, so the window's own handler never sees them).
type keyEntry struct {
	widget.Entry
	onKey func(*fyne.KeyEvent) bool // true: taken
}

// CreateRenderer implements fyne.Widget: the wheel passed on (wheelpass.go).
func (e *keyEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}

func newKeyEntry(onKey func(*fyne.KeyEvent) bool) *keyEntry {
	e := &keyEntry{onKey: onKey}
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey implements fyne.Focusable.
func (e *keyEntry) TypedKey(ev *fyne.KeyEvent) {
	if e.onKey != nil && e.onKey(ev) {
		return
	}
	e.Entry.TypedKey(ev)
}

// cmdPaletteRow is one entry: its label (greyed, with the reason, when it cannot run), its category
// and its shortcut; the highlighted row has the selection's colour.
type cmdPaletteRow struct {
	widget.BaseWidget
	bg       *canvas.Rectangle
	label    *textLine
	reason   *textLine
	category *textLine
	key      *textLine
	content  fyne.CanvasObject
	selected bool
}

func newCmdPaletteRow() *cmdPaletteRow {
	r := &cmdPaletteRow{bg: canvas.NewRectangle(color.Transparent)}
	r.label = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	r.reason = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Italic: true})
	r.reason.truncate = true
	r.category = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	r.key = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	right := container.NewHBox(r.category, r.key)
	r.content = container.NewStack(r.bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, r.label, right, r.reason)))
	r.ExtendBaseWidget(r)
	return r
}

// set shows an entry.
func (r *cmdPaletteRow) set(e engine.PaletteEntry, selected bool) {
	r.label.SetText(e.Label)
	if e.Reason != "" {
		r.label.SetColor(colorMuted)
		r.reason.SetText("— " + e.Reason)
	} else {
		r.label.SetColor(theme.ColorNameForeground)
		r.reason.SetText("")
	}
	r.category.SetText(shorten(e.Category, paletteMaxCategory))
	r.key.SetText(shortcutText(e.Shortcut, runtime.GOOS == "darwin"))
	if selected != r.selected {
		r.selected = selected
		r.Refresh()
	}
}

// CreateRenderer implements fyne.Widget.
func (r *cmdPaletteRow) CreateRenderer() fyne.WidgetRenderer {
	return &cmdPaletteRowRenderer{r: r, simple: widget.NewSimpleRenderer(r.content)}
}

type cmdPaletteRowRenderer struct {
	r      *cmdPaletteRow
	simple fyne.WidgetRenderer
}

func (rr *cmdPaletteRowRenderer) Layout(size fyne.Size)        { rr.simple.Layout(size) }
func (rr *cmdPaletteRowRenderer) MinSize() fyne.Size           { return rr.simple.MinSize() }
func (rr *cmdPaletteRowRenderer) Objects() []fyne.CanvasObject { return rr.simple.Objects() }
func (rr *cmdPaletteRowRenderer) Destroy()                     {}

func (rr *cmdPaletteRowRenderer) Refresh() {
	th := rr.r.Theme()
	rr.r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	rr.r.bg.FillColor = color.Transparent
	if rr.r.selected {
		rr.r.bg.FillColor = themeColor(th, theme.ColorNameSelection)
	}
	rr.r.bg.Refresh()
	rr.simple.Refresh()
}

// parseShortcut turns a registry shortcut into Fyne's: "Cmd+Shift+P", where Cmd is ⌘ on macOS and
// Ctrl elsewhere (fyne.KeyModifierShortcutDefault). Nil for what Fyne makes no shortcut of: a key
// without a modifier or with Shift alone (the F-keys, which reach the window's key handler), a
// range ("F1–F12"), an unknown key.
func parseShortcut(s string) *desktop.CustomShortcut {
	parts := strings.Split(s, "+")
	if len(parts) < 2 {
		return nil
	}
	var mod fyne.KeyModifier
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "Cmd":
			mod |= fyne.KeyModifierShortcutDefault
		case "Ctrl":
			mod |= fyne.KeyModifierControl
		case "Shift":
			mod |= fyne.KeyModifierShift
		case "Alt":
			mod |= fyne.KeyModifierAlt
		default:
			return nil
		}
	}
	key := shortcutKey(parts[len(parts)-1])
	if key == "" || mod == fyne.KeyModifierShift {
		return nil
	}
	return &desktop.CustomShortcut{KeyName: key, Modifier: mod}
}

// shortcutKey is the key of a shortcut's last part: a letter, a digit, a comma, a minus, an equals
// sign, Plus (the keypad's +, which Fyne also names the + key of a German keyboard), Left or Right
// (the arrows), F1 to F12.
func shortcutKey(k string) fyne.KeyName {
	if len(k) == 1 {
		switch c := k[0]; {
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			return fyne.KeyName(k)
		case c == ',':
			return fyne.KeyComma
		case c == '-':
			return fyne.KeyMinus
		case c == '=':
			return fyne.KeyEqual
		}
		return ""
	}
	if k == "Plus" {
		return fyne.KeyPlus
	}
	if n := functionKey(fyne.KeyName(k)); n > 0 || k == string(fyne.KeyLeft) || k == string(fyne.KeyRight) {
		return fyne.KeyName(k)
	}
	return ""
}

// shortcutText shows a registry shortcut as the platform writes it: ⌘⇧P on macOS, Ctrl+Shift+P
// elsewhere; "Shift+F2" as ⇧F2 on macOS.
func shortcutText(s string, mac bool) string {
	if s == "" {
		return ""
	}
	if !mac {
		return strings.ReplaceAll(s, "Cmd+", "Ctrl+")
	}
	symbols := map[string]string{"Cmd": "⌘", "Shift": "⇧", "Alt": "⌥", "Ctrl": "⌃"}
	arrows := map[string]string{"Left": "←", "Right": "→", "Plus": "+"}
	parts := strings.Split(s, "+")
	var b strings.Builder
	for i, part := range parts {
		last := i == len(parts)-1
		if sym, ok := symbols[part]; ok && !last {
			b.WriteString(sym)
			continue
		}
		if arrow, ok := arrows[part]; ok && last && i > 0 {
			b.WriteString(arrow)
			continue
		}
		b.WriteString(part)
		if !last {
			b.WriteByte('+')
		}
	}
	return b.String()
}

// registryShortcut is the shortcut the registry declares for a command, as Fyne binds it; nil when
// the registry has no such command or Fyne cannot bind it.
func (sh *shell) registryShortcut(id string) *desktop.CustomShortcut {
	c, ok := sh.eng.Registry().Get(id)
	if !ok {
		return nil
	}
	return parseShortcut(c.Shortcut)
}

// withShortcut gives a menu item the shortcut the registry declares for a command, if any. A
// main-menu item's shortcut works whatever has the focus: Fyne asks the main menu before the
// focused widget, and on macOS it is the native menu's key equivalent.
func (sh *shell) withShortcut(item *fyne.MenuItem, id string) *fyne.MenuItem {
	if sc := sh.registryShortcut(id); sc != nil {
		item.Shortcut = sc
	}
	return item
}

// paletteMenuItem is the View menu's "Command Palette…" with ⌘⇧P (ui.palette.open's shortcut); nil
// in a build whose registry lacks the command.
func (sh *shell) paletteMenuItem() *fyne.MenuItem {
	c, ok := sh.eng.Registry().Get(engine.PaletteOpenCommand)
	if !ok {
		return nil
	}
	return sh.withShortcut(fyne.NewMenuItem(c.Label+"…", sh.palette.open), c.ID)
}
