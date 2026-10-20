package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The renders of the owner's gate walk of 2026-10-20 (GW2, GW6; D-104): the real Drain's dialog,
// the Scale dialog with its hint's tooltip, the Rollback dialog's title. PNGs:
// testdata/drain_dialog_*.png, scale_dialog_hint_*.png, rollback_dialog_*.png; the Scale dialog's
// own: scale_dialog_*.png (render_drawer_test.go).

func renderDrainDialog(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "nodes")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the nodes view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	cmd := realCommand(t, "node.drain")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: cordonedNode}
	pre, err := cmd.Prefill(nil, cmd.ObjectParams(ref, ""))
	if err != nil {
		t.Fatal(err)
	}
	sh.openParamDialog(cmd, ref, pre)
	savePNG(t, w, name)
}

func TestRenderDrainDialogDark(t *testing.T) {
	renderDrainDialog(t, ThemeDark, "drain_dialog_dark.png")
}
func TestRenderDrainDialogLight(t *testing.T) {
	renderDrainDialog(t, ThemeLight, "drain_dialog_light.png")
}

// renderScaleHint shows the Scale dialog with the pointer on its field's name: the hint's tooltip.
func renderScaleHint(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "deployments")
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the deployments view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	m := openMenu(t, sh, q, rowOf(t, f, "deployments", "checkout-api"))
	sh.view.menu.Hide() // as a click on the item does
	item(t, m, "Scale").Action()
	var names []*fieldName
	q.until(t, "the dialog", func() bool {
		top := w.Canvas().Overlays().Top()
		names = nil
		if top != nil {
			names = findAll[*fieldName](top)
		}
		return len(names) == 1
	})
	settle(w)
	names[0].tip.delay = time.Millisecond
	icon := names[0].Size()
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(names[0]).AddXY(icon.Width-6, icon.Height/2))
	q.until(t, "the tooltip", func() bool { return names[0].tip.visibleText() != "" })
	savePNG(t, w, name)
}

func TestRenderScaleHintDark(t *testing.T) {
	renderScaleHint(t, ThemeDark, "scale_dialog_hint_dark.png")
}
func TestRenderScaleHintLight(t *testing.T) {
	renderScaleHint(t, ThemeLight, "scale_dialog_hint_light.png")
}

// renderRollback shows the Rollback dialog of a release with four revisions, the current one
// chosen.
func renderRollback(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := releaseDrawerShell(t, mode)
	var rd *rollbackDialog
	sh.dispatch("helm.release.history", refParams{Ref: *sh.drawer.shown}, func(res any, err error) {
		var h releaseHistory
		if err == nil && decode(res, &h) == nil {
			rd = sh.showRollbackDialog(h)
		}
	})
	q.until(t, "the dialog", func() bool { return rd != nil })
	settle(w)
	savePNG(t, w, name)
}

func TestRenderRollbackDialog(t *testing.T) {
	renderRollback(t, ThemeDark, "rollback_dialog_dark.png")
}
func TestRenderRollbackDialogLight(t *testing.T) {
	renderRollback(t, ThemeLight, "rollback_dialog_light.png")
}
