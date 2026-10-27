package fyneui

import (
	"fmt"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// thirtyNamespaces is a cluster's namespaces for the namespace filter's search (the owner's F21):
// twenty numbered ones and ten named, in the order the cluster lists them.
var thirtyNamespaces = func() []string {
	var out []string
	for i := 1; i <= 20; i++ {
		out = append(out, fmt.Sprintf("app-%02d", i))
	}
	return append(out, "cert-manager", "default", "ingress-nginx", "kube-node-lease", "kube-public",
		"kube-system", "logging", "monitoring", "orders", "payments")
}()

// nsFilterShell is the Pods view of a cluster with thirty namespaces, loaded, and the namespaces
// the filter starts with (none: all).
func nsFilterShell(t *testing.T, mode ThemeMode, selected ...string) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	f.namespaces = thirtyNamespaces
	f.ui.Namespaces = selected
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view and the namespaces", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.namespaces) == len(thirtyNamespaces)
	})
	return sh, w, q, f
}

// shownNames are the names of the list's rows the text matches ("" for All namespaces).
func shownNames(f *nsFilter) []string {
	var out []string
	for _, r := range f.shown {
		out = append(out, r.name)
	}
	return out
}

// nsRowNamed is the list's row of a namespace.
func nsRowNamed(t *testing.T, f *nsFilter, name string) *nsRow {
	t.Helper()
	for _, r := range f.rows {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no row for %q", name)
	return nil
}

// clickAt clicks the middle of an object as the pointer would: the canvas finds what is under it.
func clickAt(w fyne.Window, o fyne.CanvasObject) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	test.TapCanvas(w.Canvas(), pos.Add(fyne.NewPos(o.Size().Width/2, o.Size().Height/2)))
}

// listsEmptyLine reports whether the list shows its empty line in place of rows.
func listsEmptyLine(f *nsFilter) bool {
	if len(f.list.Objects) != 1 {
		return false
	}
	c, ok := f.list.Objects[0].(*fyne.Container)
	return ok && len(c.Objects) == 1 && c.Objects[0] == f.empty
}

func nsPress(f *nsFilter, k fyne.KeyName) { f.search.TypedKey(&fyne.KeyEvent{Name: k}) }

// TestNamespaceFilterTypeToFilter: the list opens with the keyboard in its search field; typing
// narrows the namespaces to those containing the text, ignoring case, without "All namespaces";
// nothing matching says so; Escape clears the text, then closes the list (the owner's F21).
func TestNamespaceFilterTypeToFilter(t *testing.T) {
	sh, w, _, _ := nsFilterShell(t, ThemeDark)
	tv := sh.view
	test.Tap(tv.nsButton)
	f := tv.ns
	if !f.visible() || w.Canvas().Focused() != f.search {
		t.Fatalf("list visible %v, focus %v: want the search field focused at once", f.visible(), w.Canvas().Focused())
	}
	if got := shownNames(f); len(got) != 31 || got[0] != "" {
		t.Fatalf("an empty search lists %d rows starting %q, want All namespaces and thirty", len(got), got[0])
	}
	if f.highlighted() != nil {
		t.Fatalf("highlight %q on opening, want none", f.highlighted().name)
	}

	test.Type(f.search, "KUBE")
	if want := []string{"kube-node-lease", "kube-public", "kube-system"}; !slices.Equal(shownNames(f), want) {
		t.Fatalf("KUBE lists %q, want %q", shownNames(f), want)
	}
	if r := f.highlighted(); r == nil || r.name != "kube-node-lease" {
		t.Fatalf("highlight %v after typing, want the first match", r)
	}
	f.search.SetText("app-1")
	if got := shownNames(f); len(got) != 10 || got[0] != "app-10" || got[9] != "app-19" {
		t.Fatalf("app-1 lists %q, want app-10 to app-19", got)
	}
	f.search.SetText("zzz")
	if len(f.shown) != 0 || !listsEmptyLine(f) || f.empty.text != "No namespaces match." || f.highlighted() != nil {
		t.Fatalf("no match: %d rows, empty line %q, highlight %v", len(f.shown), f.empty.text, f.highlighted())
	}
	nsPress(f, fyne.KeyReturn) // nothing highlighted: closes unchanged
	if f.visible() {
		t.Fatal("↵ without a highlighted row left the list open")
	}

	test.Tap(tv.nsButton)
	if f.search.Text != "" || len(f.shown) != 31 {
		t.Fatalf("reopened with %q and %d rows, want an empty search and every row", f.search.Text, len(f.shown))
	}
	test.Type(f.search, "pay")
	nsPress(f, fyne.KeyEscape)
	if !f.visible() || f.search.Text != "" || len(f.shown) != 31 || f.highlighted() != nil {
		t.Fatalf("first Escape: visible %v, text %q, %d rows, want the text cleared and the list open", f.visible(), f.search.Text, len(f.shown))
	}
	nsPress(f, fyne.KeyEscape)
	if f.visible() {
		t.Fatal("the second Escape left the list open")
	}
}

// TestNamespaceFilterKeys: ↑↓ move the highlight (from nothing ↓ to the first row, ↑ to the last,
// scrolled into view), ↵ applies the highlighted namespace alone and closes the list, Space toggles
// its box and keeps the list open, an F-key closes it.
func TestNamespaceFilterKeys(t *testing.T) {
	sh, _, q, fe := nsFilterShell(t, ThemeDark)
	tv := sh.view
	test.Tap(tv.nsButton)
	f := tv.ns

	nsPress(f, fyne.KeyUp)
	r := f.highlighted()
	if r == nil || r.name != "payments" {
		t.Fatalf("↑ from nothing highlights %v, want the last row", r)
	}
	if off, view := f.scroll.Offset.Y, f.scroll.Size().Height; off <= 0 || r.Position().Y < off || r.Position().Y+r.Size().Height > off+view+0.5 {
		t.Fatalf("the last row at %v (height %v) not in view: offset %v, view %v", r.Position().Y, r.Size().Height, off, view)
	}
	nsPress(f, fyne.KeyPageUp)
	nsPress(f, fyne.KeyPageUp)
	nsPress(f, fyne.KeyPageUp)
	nsPress(f, fyne.KeyPageUp)
	if r := f.highlighted(); r == nil || r.name != "" || f.scroll.Offset.Y != 0 {
		t.Fatalf("Page Up to the top highlights %v at offset %v, want All namespaces at the top", r, f.scroll.Offset.Y)
	}

	test.Type(f.search, "pay")
	test.Type(f.search, " ") // Space: toggles the highlighted row, types nothing
	q.until(t, "payments added", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"payments"}) })
	if !f.visible() || f.search.Text != "pay" || !nsRowNamed(t, f, "payments").check.Checked {
		t.Fatalf("after Space: visible %v, text %q", f.visible(), f.search.Text)
	}

	f.search.SetText("OR") // monitoring, orders
	nsPress(f, fyne.KeyDown)
	if r := f.highlighted(); r == nil || r.name != "orders" {
		t.Fatalf("↓ highlights %v, want orders", r)
	}
	nsPress(f, fyne.KeyDown) // stops at the end
	nsPress(f, fyne.KeyUp)
	if r := f.highlighted(); r == nil || r.name != "monitoring" {
		t.Fatalf("↓↑ highlights %v, want monitoring", r)
	}
	nsPress(f, fyne.KeyDown)
	nsPress(f, fyne.KeyReturn)
	q.until(t, "orders alone", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"orders"}) })
	if f.visible() {
		t.Fatal("↵ left the list open")
	}
	calls := fe.callsOf("ui.setNamespace")
	if got := decodeParams[namespacesParams](t, calls[len(calls)-1]).Namespaces; !slices.Equal(got, []string{"orders"}) {
		t.Fatalf("↵ dispatched %q", got)
	}

	test.Tap(tv.nsButton)
	nsPress(f, fyne.KeyF5)
	if f.visible() {
		t.Fatal("an F-key left the list open")
	}
}

// TestNamespaceFilterClickOnNameSelectsOnly: from two namespaces checked, a click on a third's name
// filters to it alone and closes the list, typed text or not; on "All namespaces"' name to every
// namespace.
func TestNamespaceFilterClickOnNameSelectsOnly(t *testing.T) {
	sh, w, q, fe := nsFilterShell(t, ThemeDark, "orders", "payments")
	tv := sh.view
	test.Tap(tv.nsButton)
	f := tv.ns
	if !nsRowNamed(t, f, "orders").check.Checked || !nsRowNamed(t, f, "payments").check.Checked || f.rows[0].check.Checked {
		t.Fatal("the boxes do not show orders and payments checked")
	}
	clickAt(w, nsRowNamed(t, f, "app-03").label)
	q.until(t, "app-03 alone", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"app-03"}) })
	if f.visible() {
		t.Fatal("a click on a name left the list open")
	}
	calls := fe.callsOf("ui.setNamespace")
	if got := decodeParams[namespacesParams](t, calls[len(calls)-1]).Namespaces; !slices.Equal(got, []string{"app-03"}) {
		t.Fatalf("the click dispatched %q", got)
	}

	sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{"orders", "payments"}}, nil)
	q.until(t, "two namespaces again", func() bool { return len(sh.ui.Namespaces) == 2 })
	test.Tap(tv.nsButton)
	test.Type(f.search, "default")
	clickAt(w, nsRowNamed(t, f, "default").label)
	q.until(t, "default alone", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"default"}) })
	if f.visible() {
		t.Fatal("a click on a matching name left the list open")
	}

	test.Tap(tv.nsButton)
	if f.highlighted() != nil || f.scroll.Offset.Y != 0 {
		t.Fatalf("reopened with %v highlighted at offset %v, want nothing at the top", f.highlighted(), f.scroll.Offset.Y)
	}
	clickAt(w, f.rows[0].label)
	q.until(t, "every namespace", func() bool { return len(sh.ui.Namespaces) == 0 })
	if f.visible() || tv.nsButton.Text != "All namespaces" {
		t.Fatalf("after All namespaces' name: visible %v, button %q", f.visible(), tv.nsButton.Text)
	}
}

// TestNamespaceFilterCheckboxToggles: a click on a box adds or removes that namespace, the list stays
// open and the keyboard goes back to the search field; the typed text stays.
func TestNamespaceFilterCheckboxToggles(t *testing.T) {
	sh, w, q, _ := nsFilterShell(t, ThemeDark, "orders", "payments")
	tv := sh.view
	test.Tap(tv.nsButton)
	f := tv.ns
	test.Type(f.search, "e")
	clickAt(w, nsRowNamed(t, f, "default").check)
	q.until(t, "default added", func() bool {
		return slices.Equal(sh.ui.Namespaces, []string{"orders", "payments", "default"})
	})
	if !f.visible() || w.Canvas().Focused() != f.search || f.search.Text != "e" {
		t.Fatalf("after a box: visible %v, focus %v, text %q", f.visible(), w.Canvas().Focused(), f.search.Text)
	}
	clickAt(w, nsRowNamed(t, f, "orders").check)
	q.until(t, "orders removed", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"payments", "default"}) })
	if !f.visible() || nsRowNamed(t, f, "orders").check.Checked || !nsRowNamed(t, f, "default").check.Checked {
		t.Fatal("the boxes do not follow the filter")
	}
	if tv.nsButton.Text != "2 namespaces" {
		t.Fatalf("button %q", tv.nsButton.Text)
	}
}

// TestNamespaceFilterKeepsTextWhenNamesArrive: names that arrive while the list is open fill it
// without losing what the user typed or the keyboard.
func TestNamespaceFilterKeepsTextWhenNamesArrive(t *testing.T) {
	sh, w, q, _ := nsFilterShell(t, ThemeDark)
	sh.namespaces = nil // as before the first list of the namespaces answered
	tv := sh.view
	test.Tap(tv.nsButton)
	f := tv.ns
	test.Type(f.search, "kube")
	if len(f.shown) != 0 {
		t.Fatalf("before the names: %q", shownNames(f))
	}
	q.until(t, "the names", func() bool { return len(sh.namespaces) == len(thirtyNamespaces) })
	if want := []string{"kube-node-lease", "kube-public", "kube-system"}; !f.visible() || f.search.Text != "kube" || !slices.Equal(shownNames(f), want) {
		t.Fatalf("after the names: visible %v, text %q, rows %q", f.visible(), f.search.Text, shownNames(f))
	}
	if w.Canvas().Focused() != f.search {
		t.Fatal("the names took the keyboard from the search field")
	}
	if r := f.highlighted(); r == nil || r.name != "kube-node-lease" {
		t.Fatalf("highlight %v after the names, want the first match", r)
	}
}

// The namespace filter's list with "kube" typed, kube-system checked and the first match highlighted,
// dark and light (AGENTS.md rule 7).
func TestRenderNamespaceFilterSearchDark(t *testing.T) {
	renderNamespaceFilterSearch(t, ThemeDark, "namespace_filter_search_dark.png")
}

func renderNamespaceFilterSearch(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := nsFilterShell(t, mode, "kube-system")
	q.until(t, "the diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	test.Tap(sh.view.nsButton)
	test.Type(sh.view.ns.search, "kube")
	savePNG(t, w, name)
}
