package fyneui

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Escape closes the innermost thing open, and only that (the owner's alpha walk of 2026-10-22,
// AW2, AW3, AW4 and AW19): an open dropdown's list, and its field lets the next Escape go on; a
// dialog, as its Cancel, the drawer behind it untouched; a find bar or a list's search; then the
// window's order as before (escape.go).

// escapeKey types Escape as the desktop driver delivers it: to the widget with the keyboard focus,
// or to the window's key handler when nothing has it (Fyne's processKeyPressed).
func escapeKey(w fyne.Window) {
	ev := &fyne.KeyEvent{Name: fyne.KeyEscape}
	c := w.Canvas()
	if f := c.Focused(); f != nil {
		f.TypedKey(ev)
		return
	}
	if h := c.OnTypedKey(); h != nil {
		h(ev)
	}
}

// openRollback shows the Rollback dialog over ingress-nginx's drawer.
func openRollback(t *testing.T, sh *shell, q *mainQueue) *rollbackDialog {
	t.Helper()
	var rd *rollbackDialog
	sh.dispatch("helm.release.history", refParams{Ref: *sh.drawer.shown}, func(res any, err error) {
		var h releaseHistory
		if err == nil && decode(res, &h) == nil {
			rd = sh.showRollbackDialog(h)
		}
	})
	q.until(t, "the Rollback dialog", func() bool { return rd != nil && rd.d.shown })
	return rd
}

// TestEscapeCancelsTheRollbackDialog (AW3): Escape over the Rollback dialog is its Cancel, with
// nothing focused and with its dropdown focused after a choice; the drawer behind it stays open
// and nothing is rolled back.
func TestEscapeCancelsTheRollbackDialog(t *testing.T) {
	sh, w, q, f, _ := releaseDrawerShell(t, ThemeDark)
	rd := openRollback(t, sh, q)
	w.Canvas().Unfocus()
	escapeKey(w)
	q.until(t, "Escape handled", func() bool { return !rd.d.shown || sh.ui.Drawer == nil })
	if rd.d.shown || sh.ui.Drawer == nil {
		t.Fatalf("Escape over the dialog: dialog shown %v, drawer %v", rd.d.shown, sh.ui.Drawer)
	}

	rd = openRollback(t, sh, q)
	test.Tap(rd.revision)
	test.Tap(rd.revision.pop.rows[1]) // a choice: the field keeps the keyboard
	if w.Canvas().Focused() != rd.revision {
		t.Fatalf("after a choice the focus is %T", w.Canvas().Focused())
	}
	escapeKey(w)
	q.drain()
	if rd.d.shown || sh.ui.Drawer == nil {
		t.Fatalf("Escape on the chosen dropdown: dialog shown %v, drawer %v", rd.d.shown, sh.ui.Drawer)
	}
	if n := len(f.callsOf(rollbackCommand)); n != 0 {
		t.Errorf("Escape rolled back %d times", n)
	}
}

// TestEscapeOnAnOpenDropdown (AW2): Escape closes the list and takes the keyboard off its field,
// so that the field is no longer drawn focused and the next Escape goes on: in a dialog it cancels
// the dialog, on Preferences it closes the page.
func TestEscapeOnAnOpenDropdown(t *testing.T) {
	sh, w, q, _, _ := releaseDrawerShell(t, ThemeDark)
	rd := openRollback(t, sh, q)
	test.Tap(rd.revision)
	if !rd.revision.isOpen() {
		t.Fatal("the dropdown did not open")
	}
	escapeKey(w)
	if rd.revision.isOpen() || !rd.d.shown || w.Canvas().Focused() != nil {
		t.Fatalf("Escape on the list: open %v, dialog shown %v, focus %T", rd.revision.isOpen(), rd.d.shown, w.Canvas().Focused())
	}
	escapeKey(w)
	q.drain()
	if rd.d.shown || sh.ui.Drawer == nil {
		t.Fatalf("the next Escape: dialog shown %v, drawer %v", rd.d.shown, sh.ui.Drawer)
	}
}

func TestEscapeOnAnOpenDropdownInPreferences(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	sh.dispatch("preferences.open", struct{}{}, nil)
	q.until(t, "the Preferences page", func() bool { return sh.settings.visible() })
	sel := sh.settings.controls["theme"].(*selectField)
	test.Tap(sel)
	if !sel.isOpen() {
		t.Fatal("the Theme dropdown did not open")
	}
	escapeKey(w)
	if sel.isOpen() || w.Canvas().Focused() != nil || !sh.settings.visible() {
		t.Fatalf("Escape on the list: open %v, focus %T, page %v", sel.isOpen(), w.Canvas().Focused(), sh.settings.visible())
	}
	escapeKey(w)
	q.until(t, "the page closed", func() bool { return !sh.settings.visible() })

	// A dropdown with the keyboard (a choice made, or Tab) hands Escape on to the window too.
	sh.dispatch("preferences.open", struct{}{}, nil)
	q.until(t, "the Preferences page again", func() bool { return sh.settings.visible() })
	sel = sh.settings.controls["theme"].(*selectField)
	w.Canvas().Focus(sel)
	escapeKey(w)
	q.until(t, "the page closed by the focused dropdown's Escape", func() bool { return !sh.settings.visible() })
}

// TestEscapeClosesTheConnectionUpdates (AW19): the Connection Updates window closes with Escape,
// and the drawer behind it stays.
func TestEscapeClosesTheConnectionUpdates(t *testing.T) {
	sh, w, q, f, _ := releaseDrawerShell(t, ThemeDark)
	f.mu.Lock()
	f.handlers["cluster.connectLog"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"context": "demo", "lines": []any{}, "attempts": []engine.ConnectAttempt{successAttempt(logAt)}}, nil
	}
	f.mu.Unlock()
	sh.kc.showConnectLog("demo")
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	w.Canvas().Unfocus()
	escapeKey(w)
	q.drain()
	if sh.kc.connectLog != nil || w.Canvas().Overlays().Top() != nil || sh.ui.Drawer == nil {
		t.Fatalf("Escape: dialog open %v, an overlay %v, drawer %v", sh.kc.connectLog != nil, w.Canvas().Overlays().Top() != nil, sh.ui.Drawer)
	}
}

// TestEscapeClosesTheFindBar (AW4): in every editor tab Escape closes the find bar from its find
// field, from its replace field and from the editor, and gives the editor the keys back; the find
// text stays for the next ⌘F.
func TestEscapeClosesTheFindBar(t *testing.T) {
	for _, c := range editorTabCases() {
		t.Run(c.name, func(t *testing.T) {
			_, w, q, find, editor := c.scene(t)
			settle(w)
			cnv := w.Canvas()
			find.open()
			find.find.SetText("replica")
			if cnv.Focused() != find.find {
				t.Fatalf("the find bar's field has no focus: %T", cnv.Focused())
			}
			escapeKey(w)
			q.drain()
			if find.visible() || cnv.Focused() != editor || find.find.Text != "replica" {
				t.Fatalf("Escape in the find field: bar %v, focus %T, text %q", find.visible(), cnv.Focused(), find.find.Text)
			}
			find.open()
			cnv.Focus(find.replace)
			escapeKey(w)
			q.drain()
			if find.visible() || cnv.Focused() != editor {
				t.Fatalf("Escape in the replace field: bar %v, focus %T", find.visible(), cnv.Focused())
			}
			find.open()
			cnv.Focus(editor)
			escapeKey(w)
			q.drain()
			if find.visible() || cnv.Focused() != editor {
				t.Fatalf("Escape in the editor: bar %v, focus %T", find.visible(), cnv.Focused())
			}
		})
	}
}

// TestEscapeLeavesTheListSearch (AW4): Escape in a list's search clears it and gives the table the
// keys; the drawer stays until the next Escape, in the table.
func TestEscapeLeavesTheListSearch(t *testing.T) {
	f := connectedFake(t, 50)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	row := f.tables["pods"].Rows[3]
	sh.dispatch("ui.select", selectParams{Refs: []model.ObjectRef{row.Ref}}, nil)
	q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil })
	cmdF(t, sh)
	cnv := w.Canvas()
	if cnv.Focused() != sh.view.search {
		t.Fatalf("⌘F: focus %T", cnv.Focused())
	}
	test.Type(sh.view.search, "web")
	q.until(t, "the filter", func() bool { return sh.ui.Filter == "web" })
	escapeKey(w)
	q.until(t, "the search cleared", func() bool { return sh.ui.Filter == "" && sh.view.search.Text == "" })
	if cnv.Focused() != sh.view.table || sh.ui.Drawer == nil {
		t.Fatalf("Escape in the search: focus %T, drawer %v", cnv.Focused(), sh.ui.Drawer)
	}
	escapeKey(w)
	q.until(t, "the drawer closed by the table's Escape", func() bool { return sh.ui.Drawer == nil })
}

// TestEscapeCancelsEveryDialog: Escape is Cancel (or Close) over every kind of dialog Cogpit
// shows, framed or Fyne's, with nothing focused and with one of its fields focused; the drawer
// behind stays open.
func TestEscapeCancelsEveryDialog(t *testing.T) {
	t.Run("confirmation", func(t *testing.T) {
		f := connectedFake(t, 10)
		sh, w, q := newTestShell(t, f, Options{})
		req := engine.ConfirmRequest{Command: "object.delete", Label: "Delete", Danger: engine.DangerDestructive,
			Origin: engine.OriginGUI, Context: "demo", Namespaces: []string{"default"},
			Objects: []model.ObjectRef{{Kind: "pods", Namespace: "default", Name: "web-0"}}}
		answer := make(chan bool, 1)
		go func() {
			ok, _ := sh.confirmer.Confirm(context.Background(), req)
			answer <- ok
		}()
		q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
		escapeKey(w)
		q.until(t, "the dialog closed", func() bool { return sh.confirmer.current == nil })
		if ok := <-answer; ok {
			t.Error("Escape confirmed the delete")
		}
	})
	t.Run("scale, its count focused", func(t *testing.T) {
		_, w, q, f, pd := openScaleProbe(t, 3)
		w.Canvas().Focus(pd.fields[0].entry)
		escapeKey(w)
		q.drain()
		if pd.d.shown || len(f.callsOf("deployment.scale")) != 0 {
			t.Fatalf("Escape in the count: shown %v, scaled %d", pd.d.shown, len(f.callsOf("deployment.scale")))
		}
	})
	t.Run("scale, its slider focused", func(t *testing.T) {
		_, w, q, _, pd := openScaleProbe(t, 3)
		w.Canvas().Focus(pd.fields[0].slider)
		escapeKey(w)
		q.drain()
		if pd.d.shown {
			t.Fatal("Escape on the slider left the dialog open")
		}
	})
	t.Run("drain, a check focused", func(t *testing.T) {
		f := kindsFake(t, "nodes")
		sh, w, q := newTestShell(t, f, Options{})
		q.until(t, "the nodes view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
		cmd := realCommand(t, "node.drain")
		ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: cordonedNode}
		pre, err := cmd.Prefill(nil, cmd.ObjectParams(ref, ""))
		if err != nil {
			t.Fatal(err)
		}
		pd := sh.openParamDialog(cmd, ref, pre)
		var check *fieldCheck
		for _, fld := range pd.fields {
			if fld.check != nil {
				check = fld.check
			}
		}
		if check == nil {
			t.Fatal("Drain has no check")
		}
		test.Tap(check)
		if w.Canvas().Focused() == nil {
			t.Fatal("a tapped check has no focus")
		}
		escapeKey(w)
		q.drain()
		if pd.d.shown {
			t.Fatal("Escape on a check left the dialog open")
		}
	})
	t.Run("create namespace, its name focused", func(t *testing.T) {
		f := createFake(t, kindNamespaces)
		sh, w, q := newCreateShell(t, f, ThemeDark)
		form := openForm(t, sh, q, kindNamespaces)
		w.Canvas().Focus(form.name)
		test.Type(form.name, "team-x")
		if w.Canvas().Focused() == nil {
			t.Fatal("the name has no focus")
		}
		escapeKey(w)
		q.drain()
		if form.shown || len(f.callsOf("namespace.create")) != 0 {
			t.Fatalf("Escape in the name: shown %v", form.shown)
		}
	})
	t.Run("about", func(t *testing.T) {
		sh, w, q := newTestShell(t, newFakeEngine(t), Options{})
		sh.showAboutFor(aboutRelease)
		q.drain()
		if w.Canvas().Overlays().Top() == nil {
			t.Fatal("no About")
		}
		escapeKey(w)
		q.drain()
		if w.Canvas().Overlays().Top() != nil {
			t.Fatal("Escape left About open")
		}
	})
	t.Run("status filter", func(t *testing.T) {
		f := connectedFake(t, 50)
		sh, w, q := newTestShell(t, f, Options{})
		q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
		row := f.tables["pods"].Rows[3]
		sh.dispatch("ui.select", selectParams{Refs: []model.ObjectRef{row.Ref}}, nil)
		q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil })
		sh.view.states.open()
		if sh.view.states.popup == nil || !sh.view.states.popup.Visible() {
			t.Fatal("the status filter did not open")
		}
		escapeKey(w)
		q.drain()
		if sh.view.states.popup.Visible() || sh.ui.Drawer == nil {
			t.Fatalf("Escape over the status filter: open %v, drawer %v", sh.view.states.popup.Visible(), sh.ui.Drawer)
		}
	})
}

// TestClosedDropdownHasAnOutline (AW1): a closed dropdown draws the input border as an entry does,
// so that it reads on a dialog's card, where the input background is the card's own colour.
func TestClosedDropdownHasAnOutline(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		s, _, _ := selectWindow(t, []string{"alpha", "bravo"}, "alpha", 400)
		fyne.CurrentApp().Settings().SetTheme(newTheme(mode))
		s.Refresh()
		th := fyne.CurrentApp().Settings().Theme()
		want := th.Color(theme.ColorNameInputBorder, fyne.CurrentApp().Settings().ThemeVariant())
		var outline *canvas.Rectangle
		for _, o := range test.WidgetRenderer(s).Objects() {
			if r, ok := o.(*canvas.Rectangle); ok && r.StrokeWidth > 0 && r.StrokeColor == want {
				outline = r
			}
		}
		if outline == nil || outline.Size() != s.Size() && outline.Size().Width < s.Size().Width-2 {
			t.Errorf("%s: no outline in the input border's colour around the closed dropdown (%v)", mode, outline)
		}
	}
}

// entriesIn, checksIn and slidersIn return the text fields, checks and sliders under root in the
// tree's order, a dialog's own (fieldEntry, fieldCheck, fieldSlider, escape.go) as Fyne's widget
// they embed.
func entriesIn(root fyne.CanvasObject) []*widget.Entry {
	var out []*widget.Entry
	for _, o := range findAll[fyne.CanvasObject](root) {
		switch e := o.(type) {
		case *widget.Entry:
			out = append(out, e)
		case *fieldEntry:
			out = append(out, &e.Entry)
		}
	}
	return out
}

func checksIn(root fyne.CanvasObject) []*widget.Check {
	var out []*widget.Check
	for _, o := range findAll[fyne.CanvasObject](root) {
		switch c := o.(type) {
		case *widget.Check:
			out = append(out, c)
		case *fieldCheck:
			out = append(out, &c.Check)
		}
	}
	return out
}

func slidersIn(root fyne.CanvasObject) []*widget.Slider {
	var out []*widget.Slider
	for _, o := range findAll[fyne.CanvasObject](root) {
		switch s := o.(type) {
		case *widget.Slider:
			out = append(out, s)
		case *fieldSlider:
			out = append(out, &s.Slider)
		}
	}
	return out
}

// TestEveryDialogTakesEscape reads the package's sources: a dialog of Fyne's dialog package is shown
// through showDialog, never by its own Show or a dialog.Show function, and a widget.PopUp through
// onShown, so that Escape over every one of them closes it (escape.go). A new dialog that skips them
// fails here, not in the owner's next walk.
func TestEveryDialogTakesEscape(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	isCall := func(e ast.Expr, pkg, prefix string) bool {
		c, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == pkg && strings.HasPrefix(sel.Sel.Name, prefix)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		dialogs := map[string]bool{} // what a dialog.New… was assigned to, as written
		popUps := 0
		ast.Inspect(f, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == len(a.Rhs) {
				for i, r := range a.Rhs {
					if isCall(r, "dialog", "New") {
						dialogs[types.ExprString(a.Lhs[i])] = true
					}
				}
			}
			if e, ok := n.(ast.Expr); ok && isCall(e, "widget", "NewPopUp") &&
				e.(*ast.CallExpr).Fun.(*ast.SelectorExpr).Sel.Name == "NewPopUp" { // a PopUpMenu takes the keyboard and closes on Escape itself
				popUps++
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			at := fset.Position(c.Pos())
			if isCall(c, "dialog", "Show") {
				t.Errorf("%s: dialog.%s shows a dialog Escape does not close; use dialog.New… and sh.showDialog",
					at, c.Fun.(*ast.SelectorExpr).Sel.Name)
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Show" || len(c.Args) != 0 {
				return true
			}
			if isCall(sel.X, "dialog", "New") || dialogs[types.ExprString(sel.X)] {
				t.Errorf("%s: %s.Show() shows a Fyne dialog Escape does not close; use sh.showDialog", at, types.ExprString(sel.X))
			}
			return true
		})
		if shown := strings.Count(string(src), "onShown("); popUps > shown {
			t.Errorf("%s: %d widget.NewPopUp, %d onShown: a pop-up Escape does not close", name, popUps, shown)
		}
	}
}
