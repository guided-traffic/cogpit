package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// cmdF types ⌘F as macOS delivers it: the View › Search item's key equivalent (menukeys.go). The
// fake's registry gets the real ui.setFilter's label, menu path and shortcut first.
func cmdF(t *testing.T, sh *shell) {
	t.Helper()
	if f, ok := sh.eng.(*fakeEngine); ok {
		if _, ok := f.reg.Get(setFilterCommand); !ok {
			c, _ := realCommands(t).Get(setFilterCommand)
			probe[filterParams](t, f, c)
		}
	}
	view := menuNamed(sh.mainMenu(), "View")
	if view == nil {
		t.Fatal("no View menu")
	}
	sh.menuKey = func() bool { return true }
	menuItem(t, view, "Search").Action()
}

// cmdFKey is ⌘F (Ctrl+F off macOS) as the focused widget receives it on Windows and Linux.
var cmdFKey = &desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault}

// editorTabCase is one dock tab kind with a YAML editor and a find bar.
type editorTabCase struct {
	name  string
	scene func(t *testing.T) (*shell, fyne.Window, *mainQueue, *findBar, fyne.Focusable)
}

// editorTabCases are the dock's editor tabs: Edit, Create, Helm Upgrade and Helm Install, each with
// its find bar and the editor that has the keys.
func editorTabCases() []editorTabCase {
	return []editorTabCase{
		{"Edit", func(t *testing.T) (*shell, fyne.Window, *mainQueue, *findBar, fyne.Focusable) {
			sh, w, q, _, _, et := editScene(t, ThemeDark)
			return sh, w, q, et.find, et.pane.view
		}},
		{"Create", func(t *testing.T) (*shell, fyne.Window, *mainQueue, *findBar, fyne.Focusable) {
			sh, w, q, ct := createScene(t, ThemeDark)
			return sh, w, q, ct.find, ct.pane.view
		}},
		{"Helm Upgrade", func(t *testing.T) (*shell, fyne.Window, *mainQueue, *findBar, fyne.Focusable) {
			f, ref := releasesFake(t)
			ed := newEditorFake(f)
			tab := engine.DockTab{ID: "helmUpgrade-1", Type: engine.DockHelmUpgrade, Title: "Helm Upgrade: ingress-nginx", Context: "demo", Ref: &ref,
				Helm: &engine.HelmUpgradeTab{Chart: "ingress-nginx", Version: "4.12.1", Versions: []string{"4.12.1"}, UserSuppliedOnly: true}}
			ed.texts[tab.ID], ed.revs[tab.ID] = "controller:\n  replicaCount: 2\n", 1
			f.ui.Dock, f.ui.ActiveDock = []engine.DockTab{tab}, tab.ID
			sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
			var u *helmUpgradeTab
			q.until(t, "the upgrade tab", func() bool {
				if c := sh.dock.contents[tab.ID]; c != nil {
					u, _ = c.editor.(*helmUpgradeTab)
				}
				return u != nil && !u.busy && strings.Contains(u.pane.view.Text(), "replicaCount")
			})
			return sh, w, q, u.find, u.pane.view
		}},
		{"Helm Install", func(t *testing.T) (*shell, fyne.Window, *mainQueue, *findBar, fyne.Focusable) {
			f := chartsFake(t)
			tab := installTabFixture(t, f, true)
			sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
			u := installTabOf(t, sh, q, tab.ID)
			return sh, w, q, u.find, u.pane.view
		}},
	}
}

// TestCmdFOpensTheEditorTabsFindBar (the owner's gate walk, GW3): in every dock tab with an editor,
// ⌘F opens the tab's find bar with the keys in its field, whether the editor has the keys or
// nothing has, by macOS's key equivalent and by the key the editor gets elsewhere; with the keys in
// the list, ⌘F stays the list's search; the list's search again once the dock is collapsed.
func TestCmdFOpensTheEditorTabsFindBar(t *testing.T) {
	for _, c := range editorTabCases() {
		t.Run(c.name, func(t *testing.T) {
			sh, w, q, find, editor := c.scene(t)
			q.until(t, "the dock open", func() bool { return sh.dock.open() })
			settle(w)
			canvas := w.Canvas()
			if find.visible() {
				t.Fatal("the find bar is open at first")
			}

			// The editor has the keys: macOS's key equivalent.
			canvas.Focus(editor)
			cmdF(t, sh)
			if !find.visible() || canvas.Focused() != find.find {
				t.Fatalf("⌘F in the editor: find bar %v, focus %T", find.visible(), canvas.Focused())
			}
			// ⌘F on the open bar keeps it open, the keys in its field.
			canvas.Focus(editor)
			cmdF(t, sh)
			if !find.visible() || canvas.Focused() != find.find {
				t.Fatalf("⌘F again: find bar %v, focus %T", find.visible(), canvas.Focused())
			}
			find.close()

			// The editor has the keys: the key itself, as Windows and Linux hand it to the editor.
			canvas.Focus(editor)
			editor.(fyne.Shortcutable).TypedShortcut(cmdFKey)
			if !find.visible() || canvas.Focused() != find.find {
				t.Fatalf("the editor's ⌘F: find bar %v, focus %T", find.visible(), canvas.Focused())
			}
			find.close()

			// Nothing has the keys (a click on one of the tab's buttons): the tab's find bar, by the
			// key equivalent and by the window's own shortcut.
			canvas.Unfocus()
			cmdF(t, sh)
			if !find.visible() || canvas.Focused() != find.find {
				t.Fatalf("⌘F without a focus: find bar %v, focus %T", find.visible(), canvas.Focused())
			}
			find.close()
			canvas.Unfocus()
			sh.searchKey()
			if !find.visible() {
				t.Fatal("the window's ⌘F without a focus did not open the find bar")
			}
			find.close()

			// The list has the keys: its search, the find bar stays closed.
			canvas.Focus(sh.view.table)
			cmdF(t, sh)
			if find.visible() || canvas.Focused() != sh.view.search {
				t.Fatalf("⌘F in the list: find bar %v, focus %T", find.visible(), canvas.Focused())
			}

			// The dock collapsed: the list's search, whatever has the keys.
			sh.eng.(*fakeEngine).update(func(u *engine.UIState) { u.DockOpen = false }) // ui.toggleDock, as the engine does
			q.until(t, "the dock collapsed", func() bool { return !sh.dock.open() })
			canvas.Unfocus()
			cmdF(t, sh)
			if find.visible() || canvas.Focused() != sh.view.search {
				t.Fatalf("⌘F with the dock collapsed: find bar %v, focus %T", find.visible(), canvas.Focused())
			}
		})
	}
}

// TestCmdFInAnEditTabsReview: an Edit tab showing its review has no editor in view, so ⌘F is the
// list's search; back in the editor it is the tab's find bar again.
func TestCmdFInAnEditTabsReview(t *testing.T) {
	sh, w, q, _, _, et := editScene(t, ThemeDark)
	q.until(t, "the dock open", func() bool { return sh.dock.open() })
	et.showReview("apiVersion: v1\n", nil, "Review", colorMuted)
	canvas := w.Canvas()
	canvas.Unfocus()
	cmdF(t, sh)
	if et.find.visible() || canvas.Focused() != sh.view.search {
		t.Fatalf("⌘F in the review: find bar %v, focus %T", et.find.visible(), canvas.Focused())
	}
	et.backToEditor()
	canvas.Unfocus()
	cmdF(t, sh)
	if !et.find.visible() || canvas.Focused() != et.find.find {
		t.Fatalf("⌘F back in the editor: find bar %v, focus %T", et.find.visible(), canvas.Focused())
	}
}

func TestRenderEditFindDark(t *testing.T) { renderEditFind(t, ThemeDark, "edit_find_dark.png") }

// renderEditFind: an Edit tab whose find bar ⌘F opened from the editor, a search typed.
func renderEditFind(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _, et := editScene(t, mode)
	q.until(t, "the view", func() bool { return sh.view.snap != nil && sh.diag.total > 0 && sh.dock.open() })
	w.Canvas().Focus(et.pane.view)
	cmdF(t, sh)
	test.Type(et.find.find, "port")
	if !et.find.visible() || et.find.count.text == "" || et.find.count.text == "No results" {
		t.Fatalf("the find bar: visible %v, count %q", et.find.visible(), et.find.count.text)
	}
	savePNG(t, w, name)
}
