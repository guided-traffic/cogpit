package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer, row menu, dialog and tooltip renders: the real drawer models of the synthetic pods
// and nodes, and the row-menu probe commands, so that the menus show what Lens's show.
// PNGs: testdata/{pod,node}_drawer_*.png, row_menu_*.png, scale_dialog_*.png, tooltip_*.png.

func renderDrawer(t *testing.T, view string, ref model.ObjectRef, mode ThemeMode, name string) {
	f := kindsFake(t, view)
	registerProbes(t, f)
	f.disabled["node.drain"] = "the demo cluster cannot be changed"
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func podDrawerRef(t *testing.T) model.ObjectRef {
	p := richPod(t)
	return model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
}

var nodeDrawerRef = model.ObjectRef{Context: "demo", Kind: "nodes", Name: cordonedNode}

func TestRenderPodDrawerDark(t *testing.T) {
	renderDrawer(t, "pods", podDrawerRef(t), ThemeDark, "pod_drawer_dark.png")
}

func TestRenderPodDrawerLight(t *testing.T) {
	renderDrawer(t, "pods", podDrawerRef(t), ThemeLight, "pod_drawer_light.png")
}

func TestRenderNodeDrawerDark(t *testing.T) {
	renderDrawer(t, "nodes", nodeDrawerRef, ThemeDark, "node_drawer_dark.png")
}

func TestRenderNodeDrawerLight(t *testing.T) {
	renderDrawer(t, "nodes", nodeDrawerRef, ThemeLight, "node_drawer_light.png")
}

// renderRowMenu opens a pod's actions menu with a right-click and its Shell submenu by hovering.
func renderRowMenu(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	var pod podSpec
	for _, p := range fixturePodSpecs() {
		if len(p.containers) == 2 && len(p.init) == 1 {
			pod = p
			break
		}
	}
	c := cellAt(t, sh, rowOf(t, f, "pods", pod.name), 0)
	test.TapSecondaryAt(c, fyne.NewPos(120, c.Size().Height/2))
	q.until(t, "the row menu", func() bool { return sh.view.menu != nil })
	settle(w)
	// Hover the Shell item: its submenu of containers opens.
	for _, txt := range findAll[*canvas.Text](sh.view.menu) {
		if txt.Text == "Shell" {
			pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(txt).AddXY(4, txt.Size().Height/2)
			test.MoveMouse(w.Canvas(), pos)
			break
		}
	}
	settle(w)
	savePNG(t, w, name)
}

func TestRenderRowMenuDark(t *testing.T)  { renderRowMenu(t, ThemeDark, "row_menu_dark.png") }
func TestRenderRowMenuLight(t *testing.T) { renderRowMenu(t, ThemeLight, "row_menu_light.png") }

// renderScaleDialog opens the schema-generated dialog of the Scale probe, prefilled with the
// replicas from the cache.
func renderScaleDialog(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "deployments")
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the deployments view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	m := openMenu(t, sh, q, rowOf(t, f, "deployments", "checkout-api"))
	sh.view.menu.Hide() // as a click on the item does
	item(t, m, "Scale").Action()
	q.until(t, "the dialog", func() bool {
		top := w.Canvas().Overlays().Top()
		return top != nil && len(entriesIn(top)) == 1
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderScaleDialogDark(t *testing.T) {
	renderScaleDialog(t, ThemeDark, "scale_dialog_dark.png")
}
func TestRenderScaleDialogLight(t *testing.T) {
	renderScaleDialog(t, ThemeLight, "scale_dialog_light.png")
}

// renderTooltip hovers an event message that is cut off: its full text shows in a tooltip.
func renderTooltip(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "events")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the events view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	sh.tip.delay = time.Millisecond
	col := columnOf(t, sh, "message")
	var cut *tableCell
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.col == col && c.truncated && len(c.full) > 150 && (cut == nil || c.row < cut.row) {
			cut = c
		}
	}
	if cut == nil {
		t.Fatal("no long message cut off on screen")
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(cut).AddXY(160, cut.Size().Height/2)
	test.MoveMouse(w.Canvas(), pos)
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	settle(w)
	savePNG(t, w, name)
}

func TestRenderTooltipDark(t *testing.T)  { renderTooltip(t, ThemeDark, "tooltip_dark.png") }
func TestRenderTooltipLight(t *testing.T) { renderTooltip(t, ThemeLight, "tooltip_light.png") }
