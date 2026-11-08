package fyneui

import (
	"errors"
	"runtime"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Row keys (D-158, the owner's pick: k9s's single keys). While the resource table or the
// drawer has the keyboard, a key acts on the object the table's keyboard is on (its ring's row), or
// on the drawer's object: l Logs, s Shell, r Restart, e Edit, Ctrl+D Delete, ... Never in a text
// field or a terminal: those take every key while they have the keyboard, and the window's
// handlers hear a key only while nothing does (or a drawer's selectable text, which passes it on).
// Nor while a dialog, a pop-up or a settings page is over the window.
//
// Two sources, one each:
//   - the registry's commands with RowKeys (Spec.RowKeys): the kind's actions, which the row menu
//     and the drawer's toolbar offer; the key runs the first that applies to the object, as the
//     menu item would (its confirmation, dialog or container choice included);
//   - objectKeys below: keys that change what the window shows through the ui.* commands (y the
//     drawer's YAML, w the namespace filter, ⇧J the owner, ...).
//
// With rows checked, a key whose command acts on a selection (Ctrl+D Delete, r Restart) acts on the
// checked rows, as k9s acts on its marks; the others act on the ring's row.

// The registry's names of modifiers and keys a row key may carry (engine/keys.go).
const (
	modCtrl      = "Ctrl"
	modCmd       = "Cmd"
	modAlt       = "Alt"
	modShift     = "Shift"
	keyNameBack  = "Backspace"
	labelDetails = "Details"
)

// keyTarget is what a row key acts on.
type keyTarget struct {
	ref    model.ObjectRef
	sel    []model.ObjectRef // the checked rows, from the table only
	row    int               // the table's row; -1 from the drawer
	drawer bool              // the key came from the drawer (or from the window with the drawer open)
}

// objectKey is a row key the window handles through the ui.* commands, for one object.
type objectKey struct {
	keys  []string
	label string
	// applies says whether the key acts on an object of ref's kind; nil: every object.
	applies func(sh *shell, ref model.ObjectRef) bool
	run     func(sh *shell, t keyTarget)
}

// objectKeys are the row keys without a command of their own; set in init (they refer to the
// shell's methods).
var objectKeys []objectKey

func init() {
	namespaced := func(_ *shell, ref model.ObjectRef) bool { return ref.Namespace != "" }
	kindIs := func(ks ...string) func(*shell, model.ObjectRef) bool {
		return func(_ *shell, ref model.ObjectRef) bool { return slices.Contains(ks, ref.Kind) }
	}
	objectKeys = []objectKey{
		{keys: []string{"y"}, label: "YAML", run: func(sh *shell, t keyTarget) { sh.openDrawerTab(t, true) }},
		{keys: []string{"d"}, label: labelDetails, run: func(sh *shell, t keyTarget) { sh.openDrawerTab(t, false) }},
		{keys: []string{"p"}, label: "Previous Logs", applies: hasLogs, run: (*shell).previousLogsKey},
		{keys: []string{"n"}, label: "Copy Namespace", applies: namespaced, run: func(sh *shell, t keyTarget) {
			sh.copyText(t.ref.Namespace, false)
			sh.copyToast(engine.LevelInfo, "Copied "+t.ref.Namespace)
		}},
		{keys: []string{"w"}, label: "Show This Namespace Only", applies: namespaced, run: func(sh *shell, t keyTarget) {
			sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{t.ref.Namespace}}, nil)
		}},
		{keys: []string{"J"}, label: "Go to Owner", run: func(sh *shell, t keyTarget) {
			sh.followItemLink(t.ref, "Controlled By", "no owner")
		}},
		{keys: []string{"o"}, label: "Go to Node", applies: kindIs(kindPods), run: func(sh *shell, t keyTarget) {
			sh.followItemLink(t.ref, "Node", "not scheduled on a node")
		}},
		{keys: []string{"u"}, label: "Use Namespace", applies: kindIs(kindNamespaces), run: func(sh *shell, t keyTarget) {
			sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{t.ref.Name}}, nil)
		}},
		{keys: []string{"u"}, label: "Referenced By", applies: func(_ *shell, ref model.ObjectRef) bool {
			return len(kinds.BackRefNeeds(ref.Kind)) > 0
		}, run: func(sh *shell, t keyTarget) {
			sh.drawer.openAt(t.ref, kinds.TitleReferencedBy)
			if !t.drawer {
				sh.openDetails(t.ref)
			}
		}},
		{keys: []string{"f"}, label: "Port Forwards", applies: kindIs(kindPods, kindServices), run: func(sh *shell, _ keyTarget) {
			sh.dispatch("ui.openView", openViewParams{View: kindPortForwards}, nil)
		}},
		{keys: []string{"F"}, label: "Port Forward…", applies: kindIs(kindPods, kindServices), run: (*shell).forwardKey},
		{keys: []string{"x"}, label: "Show All Values", applies: kindIs(kindSecrets), run: func(sh *shell, t keyTarget) {
			sh.drawer.revealAll(t.ref) // toggles while the drawer shows the Secret's data; else once it does
			if !t.drawer && (sh.ui.Drawer == nil || !sh.ui.Drawer.Same(t.ref)) {
				sh.openDetails(t.ref)
			}
		}},
		{keys: []string{"v"}, label: "Values", applies: kindIs(helm.ReleaseKind), run: func(sh *shell, t keyTarget) {
			sh.dispatch(revisionsCommand, revisionsValuesParams{Ref: t.ref, Mode: "values"}, nil)
		}},
	}
}

// revisionsValuesParams are ui.helm.revisions' parameters the values key sets.
type revisionsValuesParams struct {
	Ref  model.ObjectRef `json:"ref"`
	Mode string          `json:"mode"`
}

// hasLogs reports whether an object's kind has a Logs action (its l key), whose previous
// container's log p opens.
func hasLogs(sh *shell, ref model.ObjectRef) bool {
	return len(sh.eng.Registry().RowKeyActions(ref.Kind, "l")) > 0
}

// rowKey runs a row key on its target and reports whether the key means something there. It runs
// on the main goroutine; what reads the cache runs off it.
func (sh *shell) rowKey(key string, t keyTarget) bool {
	reg := sh.eng.Registry()
	if len(t.sel) > 0 {
		if cmds := reg.RowKeySelectionActions(engine.SelectionKinds(t.sel), key); len(cmds) > 0 {
			sh.runSelectionKey(cmds, t.sel)
			return true
		}
	}
	if cmds := ownFirst(reg.RowKeyActions(t.ref.Kind, key)); len(cmds) > 0 {
		sh.runCommandKey(cmds, t, nil)
		return true
	}
	for _, k := range objectKeys {
		if slices.Contains(k.keys, key) && (k.applies == nil || k.applies(sh, t.ref)) {
			k.run(sh, t)
			return true
		}
	}
	return false
}

// ownFirst keeps the kind's own commands when it has any (c cordons a node and never copies its
// name, even on a node cordoned already), else the commands of every kind.
func ownFirst(cmds []*engine.Command) []*engine.Command {
	if len(cmds) > 0 && len(cmds[0].Kinds) > 0 {
		return slices.DeleteFunc(slices.Clone(cmds), func(c *engine.Command) bool { return len(c.Kinds) == 0 })
	}
	return cmds
}

// runCommandKey runs the first of cmds that applies to the target's object, as its menu item does:
// greyed, it says why in a toast; one that takes a container asks which when the pod has several.
// None applying says why the first does not (Uncordon on a node that is not cordoned). then, when
// set, gets the command's result once it ran (Previous Logs sets the tab's option).
func (sh *shell) runCommandKey(cmds []*engine.Command, t keyTarget, then func(res any)) {
	go func() {
		defer sh.recoverTo("row key")
		var containers []string
		if slices.ContainsFunc(cmds, (*engine.Command).TakesContainer) {
			containers = sh.containersFor(t.ref)
		}
		var notApplicable error
		for _, cmd := range cmds {
			a, err := sh.evaluateAction(cmd, t.ref, containers)
			if err != nil {
				if notApplicable == nil {
					notApplicable = err
				}
				continue
			}
			sh.do(func() { sh.runKeyAction(a, t, then) })
			return
		}
		if notApplicable != nil {
			msg := cmds[0].Label + ": " + strings.TrimPrefix(notApplicable.Error(), engine.ErrNotApplicable.Error()+": ")
			sh.do(func() { sh.showToast(engine.LevelInfo, msg, 0) })
		}
	}()
}

// runKeyAction runs an evaluated action for a key: its reason when it cannot run, the container
// menu when the pod has several, else the action as the menu item runs it.
func (sh *shell) runKeyAction(a actionState, t keyTarget, then func(res any)) {
	switch {
	case a.reason != "":
		sh.showToast(engine.LevelWarning, disabledLabel(a.cmd.Label, a.reason), 0)
	case len(a.containers) > 1:
		items := make([]*fyne.MenuItem, 0, len(a.containers))
		for _, c := range a.containers {
			items = append(items, fyne.NewMenuItem(c, func() { sh.runActionThen(a, t.ref, c, then) }))
		}
		sh.keyMenu(t, fyne.NewMenu("", items...))
	case len(a.containers) == 1:
		sh.runActionThen(a, t.ref, a.containers[0], then)
	default:
		sh.runActionThen(a, t.ref, "", then)
	}
}

// runActionThen runs an action as runAction does and hands then the result of a command that ran
// without a dialog.
func (sh *shell) runActionThen(a actionState, ref model.ObjectRef, container string, then func(res any)) {
	if then == nil || a.dialog {
		sh.runAction(a, ref, container)
		return
	}
	if _, ok := itemActionDialogs[a.cmd.ID]; ok {
		sh.runAction(a, ref, container)
		return
	}
	done := sh.actionDone(a.cmd.ID)
	sh.dispatch(a.cmd.ID, a.cmd.ObjectParams(ref, container), func(res any, err error) {
		done(res, err)
		if err == nil {
			then(res)
		}
	})
}

// runSelectionKey runs the first of cmds that applies to the checked rows, as the selection's
// menu does (its confirmation names every object).
func (sh *shell) runSelectionKey(cmds []*engine.Command, sel []model.ObjectRef) {
	refs := slices.Clone(sel)
	go func() {
		defer sh.recoverTo("row key")
		acts := sh.evaluateSelection(refs)
		for _, cmd := range cmds {
			for _, a := range acts {
				if a.cmd.ID != cmd.ID {
					continue
				}
				sh.do(func() {
					if a.reason != "" {
						sh.showToast(engine.LevelWarning, disabledLabel(a.cmd.Label, a.reason), 0)
						return
					}
					sh.view.runOnSelection(a.cmd.ID, refs)
				})
				return
			}
		}
	}()
}

// keyMenu shows a menu a key opened (a pod's containers) at the ring's row, or under the drawer's
// actions.
func (sh *shell) keyMenu(t keyTarget, m *fyne.Menu) {
	c := sh.win.Canvas()
	drv := fyne.CurrentApp().Driver()
	pos := fyne.NewPos(c.Size().Width/2, c.Size().Height/3)
	var at fyne.CanvasObject
	if t.drawer {
		at = sh.drawer.actions
	} else {
		at = sh.view.ringCell()
	}
	if at != nil && at.Visible() {
		pos = drv.AbsolutePositionForObject(at).Add(fyne.NewPos(0, at.Size().Height))
	}
	widget.NewPopUpMenu(m, c).ShowAtPosition(pos)
}

// previousLogsKey opens the log of the object's previous container (k9s's p): its Logs, then the
// tab's Previous option, through logs.setOptions.
func (sh *shell) previousLogsKey(t keyTarget) {
	cmds := ownFirst(sh.eng.Registry().RowKeyActions(t.ref.Kind, "l"))
	sh.runCommandKey(cmds, t, func(res any) {
		var tab struct {
			ID string `json:"id"`
		}
		if decode(res, &tab) != nil || tab.ID == "" {
			return
		}
		previous := true
		sh.dispatch("logs.setOptions", logOptionsParams{ID: tab.ID, Previous: &previous}, nil)
	})
}

// followItemLink shows the object an item of ref's Properties links to (Controlled By, a pod's
// Node), through ui.navigate; without one it says so (none).
func (sh *shell) followItemLink(ref model.ObjectRef, name, none string) {
	go func() {
		defer sh.recoverTo("row key")
		m, err := sh.eng.Detail(ref)
		var link *model.ObjectRef
		if err == nil && m != nil {
			for _, sec := range m.Sections {
				for _, it := range sec.Items {
					if it.Name == name && it.Link != nil && link == nil {
						l := *it.Link
						link = &l
					}
				}
			}
		}
		sh.do(func() {
			if link == nil {
				sh.showToast(engine.LevelInfo, ref.Name+": "+none, 0)
				return
			}
			sh.dispatch("ui.navigate", refParams{Ref: *link}, nil)
		})
	}()
}

// forwardKey opens the forward dialog of a pod's or a service's port (k9s's ⇧F), as the drawer's
// Forward… beside the port does: at once for the one port, from a menu of them for several.
func (sh *shell) forwardKey(t keyTarget) {
	go func() {
		defer sh.recoverTo("row key")
		m, err := sh.eng.Detail(t.ref)
		var acts []model.ItemAction
		var names []string
		if err == nil && m != nil {
			for _, sec := range m.Sections {
				for _, it := range sec.Items {
					for _, a := range it.Actions {
						if !a.Link && (a.Command == "pod.portForward" || a.Command == "service.portForward") {
							acts = append(acts, a)
							names = append(names, portName(sec, it))
						}
					}
				}
			}
		}
		sh.do(func() {
			switch len(acts) {
			case 0:
				sh.showToast(engine.LevelInfo, t.ref.Name+": no port to forward", 0)
			case 1:
				openForward(sh, t.ref, acts[0])
			default:
				items := make([]*fyne.MenuItem, len(acts))
				for i, a := range acts {
					items[i] = fyne.NewMenuItem(names[i], func() { openForward(sh, t.ref, a) })
				}
				sh.keyMenu(t, fyne.NewMenu("", items...))
			}
		})
	}()
}

// portName names a port in the forward key's menu: its container (the section) and its value.
func portName(sec model.DetailSection, it model.DetailItem) string {
	if sec.Group != "" && sec.Title != "" {
		return sec.Title + ": " + it.Value
	}
	return it.Value
}

// openDrawerTab opens the drawer on the target's object at its YAML or its Details (k9s's y and d).
func (sh *shell) openDrawerTab(t keyTarget, yaml bool) {
	sh.drawer.openTab(t.ref, yaml)
	if sh.ui.Drawer == nil || !sh.ui.Drawer.Same(t.ref) {
		sh.openDetails(t.ref)
	}
}

// keyNotations are the registry notations a typed shortcut may be written in on goos ("Cmd" is ⌘
// on macOS and Ctrl elsewhere, "Ctrl" the Control key everywhere): Ctrl+Backspace off macOS is
// both "Ctrl+Backspace" and "Cmd+Backspace".
func keyNotations(sc *desktop.CustomShortcut, goos string) []string {
	name := string(sc.KeyName)
	switch sc.KeyName {
	case fyne.KeyBackspace:
		name = keyNameBack
	case fyne.KeyReturn, fyne.KeyEnter:
		name = "Return"
	}
	m := sc.Modifier
	var ctrl, cmd []string
	if m&fyne.KeyModifierControl != 0 {
		ctrl = []string{modCtrl}
		if goos != goosMac {
			cmd = []string{modCmd}
		}
	}
	if m&fyne.KeyModifierSuper != 0 && goos == goosMac {
		cmd = []string{modCmd}
	}
	var rest []string
	if m&fyne.KeyModifierAlt != 0 {
		rest = append(rest, modAlt)
	}
	if m&fyne.KeyModifierShift != 0 {
		rest = append(rest, modShift)
	}
	join := func(mods ...[]string) string {
		var parts []string
		for _, ms := range mods {
			parts = append(parts, ms...)
		}
		return strings.Join(append(append(parts, rest...), name), "+")
	}
	out := []string{}
	if ctrl != nil || cmd == nil {
		out = append(out, join(ctrl))
	}
	if cmd != nil {
		out = append(out, join(cmd))
	}
	return out
}

// rowKeyShortcut runs a typed shortcut as a row key on t and reports whether one took it.
func (sh *shell) rowKeyShortcut(sc *desktop.CustomShortcut, t keyTarget) bool {
	for _, k := range keyNotations(sc, runtime.GOOS) {
		if sh.rowKey(k, t) {
			return true
		}
	}
	return false
}

// rowKeyItemShortcut is a row key as a menu item shows it after its label (Fyne draws the
// modifiers' symbols, then the key): "l", ⇧J, ⌃D, ⌘⌫. Nil for a key it cannot draw.
func rowKeyItemShortcut(key string) fyne.Shortcut {
	if r := []rune(key); len(r) == 1 {
		var mods fyne.KeyModifier
		if r[0] >= 'A' && r[0] <= 'Z' {
			mods = fyne.KeyModifierShift
		}
		return &shownShortcut{desktop.CustomShortcut{KeyName: fyne.KeyName(key), Modifier: mods}}
	}
	parts := strings.Split(key, "+")
	var mods fyne.KeyModifier
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case modCtrl:
			mods |= fyne.KeyModifierControl
		case modCmd:
			mods |= fyne.KeyModifierShortcutDefault
		case modAlt:
			mods |= fyne.KeyModifierAlt
		case modShift:
			mods |= fyne.KeyModifierShift
		}
	}
	name := fyne.KeyName(parts[len(parts)-1])
	if name == keyNameBack {
		name = fyne.KeyBackspace
	}
	return &shownShortcut{desktop.CustomShortcut{KeyName: name, Modifier: mods}}
}

// rowKeyText is a row key as the key overview and tooltips write it: "l", "⇧J", "⌃D", "⌘⌫".
func rowKeyText(key string) string {
	if r := []rune(key); len(r) == 1 {
		if r[0] >= 'A' && r[0] <= 'Z' {
			return "⇧" + key
		}
		return key
	}
	parts := strings.Split(key, "+")
	var b strings.Builder
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case modCtrl:
			b.WriteString(modText("⌃", "Ctrl+"))
		case modCmd:
			b.WriteString(modText("⌘", "Ctrl+"))
		case modAlt:
			b.WriteString(modText("⌥", "Alt+"))
		case modShift:
			b.WriteString(modText("⇧", "Shift+"))
		}
	}
	name := parts[len(parts)-1]
	if name == keyNameBack {
		name = modText("⌫", "Backspace")
	}
	return b.String() + name
}

// modText is a modifier's text on the platform Cogpit runs on: its symbol on macOS, its name
// elsewhere.
func modText(mac, other string) string {
	if runtime.GOOS == goosMac {
		return mac
	}
	return other
}

// evaluateAction evaluates one command for an object off the main goroutine, as the row menu does
// (evaluateActions): an error when it does not apply (ErrNotApplicable), else the action with why
// it cannot run now, if it cannot.
func (sh *shell) evaluateAction(cmd *engine.Command, ref model.ObjectRef, containers []string) (actionState, error) {
	a := actionState{cmd: cmd, dialog: len(cmd.DialogFields()) > 0}
	first := ""
	if cmd.TakesContainer() && len(containers) > 0 {
		a.containers, first = containers, containers[0]
	}
	params := cmd.ObjectParams(ref, first)
	err := sh.commandEnabled(cmd.ID, params)
	var perr *engine.ParamsError
	switch {
	case errors.Is(err, engine.ErrNotApplicable):
		return a, err
	case errors.As(err, &perr):
		// The stub parameters did not satisfy the schema: the command cannot be judged before
		// its dialog has the values; Dispatch validates them then.
	case err != nil:
		a.reason = err.Error()
	}
	if a.dialog {
		a.prefill = sh.prefill(cmd, params)
	}
	return a, nil
}

// rowKeyFor is the row key that runs cmd on an object of kind, as menus and tooltips show it: its
// first key, when that key reaches cmd there (c cordons a node: Copy's c does not reach a node's
// Copy); "" for none.
func (sh *shell) rowKeyFor(kind string, cmd *engine.Command) string {
	if len(cmd.RowKeys) == 0 {
		return ""
	}
	if !slices.Contains(ownFirst(sh.eng.Registry().RowKeyActions(kind, cmd.RowKeys[0])), cmd) {
		return ""
	}
	return cmd.RowKeys[0]
}
