package fyneui

import (
	"runtime"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The key overview (D-158, the owner's pick: a card over the window, as the palette is):
// the single keys that act where the keyboard is, generated from the registry's row keys, the
// window's object keys (rowkeys.go) and the keyHints of the place (the table's, a logs tab's) and
// the window's, so that it never goes stale. On the left the object's keys, a key that cannot run on
// it greyed with the reason; on the right the place's keys and the window's. ? or Escape closes it;
// whether it shows is UIState.KeyOverview (ui.keyOverview).

// keyPlace is where the keyboard was when the overview opened: the object keys act on, and whether
// a logs tab had it.
type keyPlace struct {
	ref    *model.ObjectRef // the drawer's object or the table's keyboard row; nil: none
	drawer bool
	logs   []keyHint // the logs tab's keys while it had the keyboard; nil otherwise
}

// keyOverview is the overview's card.
type keyOverview struct {
	sh    *shell
	popup *dismissOverlay
	body  *fyne.Container
	seq   int // the overview shown; an evaluation of an older one is dropped
}

func newKeyOverview(sh *shell) *keyOverview {
	return &keyOverview{sh: sh, body: container.NewVBox()}
}

// currentKeyPlace is where the keyboard is now, for the overview: a logs tab's lines; the drawer's
// object while the drawer is open and the table does not have the keyboard; else the table's
// keyboard row.
func (sh *shell) currentKeyPlace() keyPlace {
	focused := sh.win.Canvas().Focused()
	if c, ok := sh.dock.contents[sh.dock.active]; ok && c.logs != nil && c.logs.view != nil &&
		focused != nil && focused == c.logs.view.FocusTarget() {
		return keyPlace{logs: logTabKeyHints(runtime.GOOS == goosMac)} // a logs tab's lines (logtab_keys.go)
	}
	tableHas := focused == fyne.Focusable(sh.view.table)
	if sh.ui.Drawer != nil && !tableHas {
		ref := *sh.ui.Drawer
		return keyPlace{ref: &ref, drawer: true}
	}
	if t, ok := sh.view.ringTarget(); ok {
		return keyPlace{ref: &t.ref}
	}
	return keyPlace{}
}

// stateChanged follows UIState.KeyOverview: it shows or hides the card only when that differs from
// what the window shows, so it runs at every state change.
func (k *keyOverview) stateChanged() {
	if !k.sh.ui.KeyOverview {
		k.hide()
		return
	}
	if k.popup == nil || !k.popup.Visible() {
		k.show()
	}
}

// show builds the card for the place the keyboard was and shows it.
func (k *keyOverview) show() {
	sh := k.sh
	if k.popup == nil {
		k.popup = newDismissOverlay(sh.win.Canvas(), inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, k.body))
		k.popup.onDismiss = k.close
	}
	sh.tip.hide()
	k.seq++
	place := sh.keyPlace
	k.render(place, nil)
	k.popup.Show()
	onEscape(sh.win.Canvas(), k.popup, k.close)
	if place.ref == nil {
		return
	}
	seq, ref := k.seq, *place.ref
	go func() { // what applies to the object reads the cache
		defer sh.recoverTo("key overview")
		states := sh.objectKeyStates(ref)
		sh.do(func() {
			if k.seq == seq && k.popup.Visible() {
				k.render(place, states)
			}
		})
	}()
}

// close hides the card and tells the engine.
func (k *keyOverview) close() {
	k.hide()
	if k.sh.ui.KeyOverview {
		open := false
		k.sh.dispatch("ui.keyOverview", struct {
			Open *bool `json:"open"`
		}{&open}, nil)
	}
}

func (k *keyOverview) hide() {
	if k.popup != nil && k.popup.Visible() {
		k.popup.Hide()
	}
}

// objectKeyState is an object key as the overview lists it: greyed with why when it cannot run on
// the object.
type objectKeyState struct {
	key, label, reason string
}

// objectKeyStates evaluates the object's keys off the main goroutine: each command row key once (its
// first key), the command that applies of those sharing a key (Suspend or Resume), then the
// window's object keys for the kind.
func (sh *shell) objectKeyStates(ref model.ObjectRef) []objectKeyState {
	var out []objectKeyState
	seen := map[string]bool{}
	var containers []string
	acts := sh.eng.Registry().Actions(ref.Kind)
	if slices.ContainsFunc(acts, (*engine.Command).TakesContainer) {
		containers = sh.containersFor(ref)
	}
	for _, cmd := range acts {
		if len(cmd.RowKeys) == 0 || seen[cmd.RowKeys[0]] {
			continue
		}
		key := cmd.RowKeys[0]
		own := ownFirst(sh.eng.Registry().RowKeyActions(ref.Kind, key))
		if !slices.Contains(own, cmd) {
			continue // another command of the kind takes the key (c cordons a node)
		}
		var st *objectKeyState
		for _, c := range own {
			a, err := sh.evaluateAction(c, ref, containers)
			if err != nil {
				if st == nil {
					st = &objectKeyState{key: key, label: c.Label, reason: "does not apply"}
				}
				continue
			}
			st = &objectKeyState{key: key, label: c.Label, reason: a.reason}
			break
		}
		if st != nil {
			seen[key] = true
			out = append(out, *st)
		}
	}
	for _, ok := range objectKeys {
		key := ok.keys[0]
		if seen[key] || (ok.applies != nil && !ok.applies(sh, ref)) {
			continue
		}
		seen[key] = true
		out = append(out, objectKeyState{key: key, label: ok.label})
	}
	return out
}

// render lays the card out: the object's keys (states; nil while they are evaluated: the keys
// without their reasons), the place's and the window's.
func (k *keyOverview) render(place keyPlace, states []objectKeyState) {
	sh := k.sh
	title := "Keys"
	var left []fyne.CanvasObject
	switch {
	case place.logs != nil:
		title = "Keys · Logs"
		left = append(left, keySection("Logs", hintRows(place.logs)...))
	case place.ref != nil:
		kind := sh.kindTitle(place.ref.Kind)
		title = "Keys · " + kind
		if states == nil {
			states = sh.objectKeyHints(*place.ref)
		}
		rows := make([]fyne.CanvasObject, 0, len(states))
		for _, s := range states {
			rows = append(rows, keyLine(rowKeyText(s.key), s.label, s.reason))
		}
		left = append(left, keySection(kind, rows...))
		if !place.drawer {
			left = append(left, newTextLine("With rows marked, Delete and Restart act on the marked rows.",
				colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
		}
	}
	var right []fyne.CanvasObject
	if place.ref != nil && !place.drawer {
		right = append(right, keySection("Table", hintRows(tableKeyHints)...))
	}
	right = append(right, keySection("Window", hintRows(windowKeyHints)...))
	head := newTextLine(title, theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	column := func(objs []fyne.CanvasObject) fyne.CanvasObject {
		return sized(sizeKeyCardColumn, "", container.NewVBox(objs...))
	}
	cols := container.NewHBox(column(left), newSpace(theme.SizeNameInnerPadding), column(right))
	if len(left) == 0 {
		cols = container.NewHBox(column(right))
	}
	k.body.Objects = []fyne.CanvasObject{head, newSpace(theme.SizeNamePadding), cols}
	k.body.Refresh()
}

// objectKeyHints are an object's keys before they are evaluated: every key of its kind, none greyed.
func (sh *shell) objectKeyHints(ref model.ObjectRef) []objectKeyState {
	var out []objectKeyState
	seen := map[string]bool{}
	for _, cmd := range sh.eng.Registry().Actions(ref.Kind) {
		if len(cmd.RowKeys) == 0 || seen[cmd.RowKeys[0]] {
			continue
		}
		if own := ownFirst(sh.eng.Registry().RowKeyActions(ref.Kind, cmd.RowKeys[0])); slices.Contains(own, cmd) {
			seen[cmd.RowKeys[0]] = true
			out = append(out, objectKeyState{key: cmd.RowKeys[0], label: cmd.Label})
		}
	}
	for _, ok := range objectKeys {
		if key := ok.keys[0]; !seen[key] && (ok.applies == nil || ok.applies(sh, ref)) {
			seen[key] = true
			out = append(out, objectKeyState{key: key, label: ok.label})
		}
	}
	return out
}

// hintRows are keyHints as the card's rows.
func hintRows(hints []keyHint) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(hints))
	for _, h := range hints {
		out = append(out, keyLine(h.Keys, h.Label, ""))
	}
	return out
}

// keySection is a titled group of the card's rows.
func keySection(title string, rows ...fyne.CanvasObject) fyne.CanvasObject {
	head := newTextLine(title, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	return container.NewVBox(append([]fyne.CanvasObject{head}, rows...)...)
}

// keyLine is one key of the card: the key in the accent's text colour, what it does, and, greyed,
// why it cannot run now.
func keyLine(key, label, reason string) fyne.CanvasObject {
	keyColor, labelColor := colorAccent, theme.ColorNameForeground
	if reason != "" {
		keyColor, labelColor = colorMuted, colorMuted
		label = disabledLabel(label, reason)
	}
	k := newTextLine(key, keyColor, theme.SizeNameText, fyne.TextStyle{Monospace: true, Bold: true})
	l := newTextLine(label, labelColor, theme.SizeNameText, fyne.TextStyle{})
	l.truncate = true
	return container.NewBorder(nil, nil, sized(sizeKeyColumn, "", k), nil, l)
}
