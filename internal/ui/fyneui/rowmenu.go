package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// maxReasonRunes caps the reason a disabled menu item shows after its label.
const maxReasonRunes = 60

// disabledLabel names a command that cannot run now where it is greyed: its label, a dash and why,
// "Delete — connection interrupted". The row menus, the selection's menu and the selection bar's
// Delete (its tooltip) say it alike.
func disabledLabel(label, reason string) string {
	return label + " — " + shorten(reason, maxReasonRunes)
}

// actionState is a command evaluated for one object, off the main goroutine: the row menu and the
// drawer's toolbar are both built from these.
type actionState struct {
	cmd    *engine.Command
	reason string // why the command cannot run now; empty when it can
	// containers are the pod's containers and init containers, for a command that takes one.
	containers []string
	// dialog is set for a command that needs parameters beyond the object; prefill holds the
	// dialog's starting values (Command.Prefill).
	dialog  bool
	prefill json.RawMessage
}

// evaluateActions returns the actions of ref's kind that apply to the object, in menu order
// (engine.Registry.Actions): a command whose Enabled says ErrNotApplicable is left out, one that
// cannot run now keeps its reason. containers are the pod's containers, then its init containers.
// It reads the cache through Enabled and Prefill, so it must run off the main goroutine.
func (sh *shell) evaluateActions(ref model.ObjectRef, containers []string) []actionState {
	cmds := sh.eng.Registry().Actions(ref.Kind)
	out := make([]actionState, 0, len(cmds))
	for _, cmd := range cmds {
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
			continue
		case errors.As(err, &perr):
			// The stub parameters did not satisfy the schema: the command cannot be judged before
			// its dialog has the values; Dispatch validates them then.
		case err != nil:
			a.reason = err.Error()
		}
		if a.dialog {
			a.prefill = sh.prefill(cmd, params)
		}
		out = append(out, a)
	}
	return out
}

// prefill asks a command for its dialog's starting values; on failure the dialog starts from params.
func (sh *shell) prefill(cmd *engine.Command, params json.RawMessage) (out json.RawMessage) {
	defer func() {
		if r := recover(); r != nil {
			out = params
		}
	}()
	pre, err := cmd.Prefill(sh.sess, params)
	if err != nil {
		return params
	}
	return pre
}

// actionItems turns evaluated actions into menu items: a command that takes a container opens a
// submenu of the pod's containers, one with a dialog opens it, the others run at once. A disabled
// one is greyed and says why after its label, as far as Fyne's menu can show it.
func (sh *shell) actionItems(ref model.ObjectRef, acts []actionState) []*fyne.MenuItem {
	items := make([]*fyne.MenuItem, 0, len(acts))
	for _, a := range acts {
		item := fyne.NewMenuItem(a.cmd.Label, nil)
		switch {
		case a.reason != "":
			item.Label = disabledLabel(a.cmd.Label, a.reason)
			item.Disabled = true
		case len(a.containers) > 0:
			subs := make([]*fyne.MenuItem, 0, len(a.containers))
			for _, c := range a.containers {
				subs = append(subs, fyne.NewMenuItem(c, func() { sh.runAction(a, ref, c) }))
			}
			item.ChildMenu = fyne.NewMenu("", subs...)
		default:
			item.Action = func() { sh.runAction(a, ref, "") }
		}
		items = append(items, item)
	}
	return items
}

// runAction runs an evaluated action on ref: a command with a dialog of its own in
// itemActionDialogs opens that (the port forward's Edit), started from the command's Prefill,
// another dialog command opens its schema-generated dialog first.
func (sh *shell) runAction(a actionState, ref model.ObjectRef, container string) {
	if open, ok := itemActionDialogs[a.cmd.ID]; ok {
		params := a.cmd.ObjectParams(ref, container)
		if a.dialog && len(a.prefill) > 0 {
			params = a.prefill
		}
		open(sh, ref, model.ItemAction{Label: a.cmd.Label, Command: a.cmd.ID, Params: params})
		return
	}
	if a.dialog {
		sh.openParamDialog(a.cmd, ref, a.prefill)
		return
	}
	sh.dispatch(a.cmd.ID, a.cmd.ObjectParams(ref, container), sh.actionDone(a.cmd.ID))
}

// rowMenu builds the actions menu of a row, as Lens's (capture, 2026-10-11): Show Details, then
// the kind's actions in their order, Edit and Delete last; the drawer-only ones (Copy) are not in
// it.
func (sh *shell) rowMenu(ref model.ObjectRef, acts []actionState) *fyne.Menu {
	details := fyne.NewMenuItem("Show Details", func() {
		sh.openDetails(ref) // as a click on the row: the checked rows stay checked (A73)
	})
	row := make([]actionState, 0, len(acts))
	for _, a := range acts {
		if !a.cmd.DrawerOnly {
			row = append(row, a)
		}
	}
	return fyne.NewMenu("", append([]*fyne.MenuItem{details}, sh.actionItems(ref, row)...)...)
}

// openRowMenu evaluates the actions of the row's object off the main goroutine and shows its
// menu at pos (window coordinates), unless the view changed meanwhile. A row inside a selection of
// several objects opens the selection's menu instead, which acts on all of them (tablecheck.go);
// the one checked row's menu ends with Clear Selection (N112).
func (tv *tableView) openRowMenu(row int, pos fyne.Position) {
	if tv.snap == nil || row < 0 || row >= len(tv.snap.Rows) {
		return
	}
	sh := tv.sh
	sh.tip.hide()
	if tv.inSelection(row) {
		tv.openSelectionMenu(pos)
		return
	}
	ref := tv.snap.Rows[row].Ref
	view := tv.snap.View
	go func() {
		defer sh.recoverTo("row menu")
		acts := sh.evaluateActions(ref, sh.containersFor(ref))
		sh.do(func() {
			if tv.snap == nil || tv.snap.View != view {
				return
			}
			c := fyne.CurrentApp().Driver().CanvasForObject(tv.table)
			if c == nil {
				return
			}
			tv.menuModel = sh.rowMenu(ref, acts)
			if tv.loneChecked(ref) {
				tv.endWithClearSelection(tv.menuModel)
			}
			tv.menu = widget.NewPopUpMenu(tv.menuModel, c)
			tv.menu.ShowAtPosition(pos)
		})
	}()
}

// containersFor returns a pod's containers, init containers and ephemeral containers, for the
// container submenus; other kinds have none. It reads the drawer model and must run off the main
// goroutine.
func (sh *shell) containersFor(ref model.ObjectRef) []string {
	needed := false
	for _, c := range sh.eng.Registry().Actions(ref.Kind) {
		needed = needed || c.TakesContainer()
	}
	if !needed {
		return nil
	}
	m, err := sh.eng.Detail(ref)
	if err != nil || m == nil {
		return nil
	}
	return podContainers(m)
}

// podContainers are the containers a pod's Logs, Shell and Attach submenus offer: its containers,
// then its init containers, then its ephemeral (debug) containers (the owner's "Fix bauen",
// 2026-10-19, D-102).
func podContainers(m *engine.DetailModel) []string {
	return slices.Concat(m.Containers, m.InitContainers, m.EphemeralContainers)
}

// shorten cuts s to n runes with an ellipsis.
func shorten(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// actionTip is the tooltip of an action: its label, and why it cannot run.
func actionTip(a actionState) string {
	if a.reason == "" {
		return a.cmd.Label
	}
	return fmt.Sprintf("%s: %s", a.cmd.Label, a.reason)
}
