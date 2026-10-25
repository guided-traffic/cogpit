package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// renderDockOffset gives the dock more of the window in the editor PNGs, as a user dragging the
// split would, so that the diff and the picker show in full.
const renderDockOffset = 0.42

// setDockOffset gives the workspace this share of the body's height above the open dock, as a
// drag of the dock's edge would (dockLayout).
func setDockOffset(sh *shell, offset float32) {
	sh.dockLay.offset = offset
	sh.dockSplit.Refresh()
}

// renderEditDiff shows an Edit tab reviewing two changes to a service, a new line and a changed
// port, after the server's dry run accepted them.
func renderEditDiff(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _, et := editScene(t, mode)
	setDockOffset(sh, renderDockOffset)
	typeLineAfter(et.pane, 16, "sessionAffinity: ClientIP")
	et.pane.view.SetCursorPosition(13, 1<<20)
	for range 4 {
		et.pane.view.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	}
	test.Type(et.pane.view, "9090")
	test.Tap(et.saveBtn)
	q.until(t, "the review", func() bool { return et.reviewing && !et.applyBtn.Disabled() })
	if !strings.Contains(et.banner.text, "2 added, 1 removed") {
		t.Fatalf("banner %q", et.banner.text)
	}
	savePNG(t, w, name)
}

func TestRenderEditDiffDark(t *testing.T)  { renderEditDiff(t, ThemeDark, "edit_diff_dark.png") }
func TestRenderEditDiffLight(t *testing.T) { renderEditDiff(t, ThemeLight, "edit_diff_light.png") }

// renderCreate shows a Create resource tab with the example Pod and its template picker open.
func renderCreate(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 200)
	newEditorFake(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	sh.dispatch("resource.createTab", createTabParams{}, nil)
	var ct *createTab
	q.until(t, "the create tab", func() bool {
		c, ok := sh.dock.contents["create-1"]
		if !ok {
			return false
		}
		ct, _ = c.editor.(*createTab)
		return ct != nil && strings.Contains(ct.pane.view.Text(), "kind: Pod") && len(ct.templates) > 0
	})
	setDockOffset(sh, renderDockOffset)
	settle(w)
	ct.showTemplates()
	q.drain()
	savePNG(t, w, name)
	ct.menu.Hide()
}

func TestRenderCreateDark(t *testing.T)  { renderCreate(t, ThemeDark, "create_dark.png") }
func TestRenderCreateLight(t *testing.T) { renderCreate(t, ThemeLight, "create_light.png") }
