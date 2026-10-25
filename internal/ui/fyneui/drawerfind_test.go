package fyneui

import (
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestDrawerYAMLFind (the owner's request of 2026-10-25): the drawer's read-only YAML has its find
// bar over it for good, without replace and ×; ⌘F puts the keys in its field while the YAML shows,
// from the drawer, the list's table or nowhere, but not from the list's search; typed text selects
// the first match and counts them; Escape in the field clears it and gives the YAML the keys, and
// the YAML's Escape goes on to the window.
func TestDrawerYAMLFind(t *testing.T) {
	f := kindsFake(t, "pods")
	p := richPod(t)
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && sh.drawer.yamlText != "" })
	d := sh.drawer
	if d.find.replace != nil || !d.find.root.Visible() {
		t.Fatalf("the YAML's bar: a replace field %v, shown %v", d.find.replace != nil, d.find.root.Visible())
	}

	cmdF(t, sh) // the Details tab: ⌘F is the list's
	if w.Canvas().Focused() == d.find.find {
		t.Fatal("⌘F on the Details tab went to the YAML's field")
	}
	d.tabs.Select(d.yamlTab)
	w.Canvas().Focus(sh.view.table)
	cmdF(t, sh)
	if w.Canvas().Focused() != d.find.find {
		t.Fatalf("⌘F from the table with the YAML shown: the keys are in %T", w.Canvas().Focused())
	}
	test.Type(d.find.find, "image")
	want := strings.Count(strings.ToLower(d.yamlText), "image")
	if want == 0 {
		t.Fatal("the pod's YAML names no image")
	}
	if got := d.find.count.text; !strings.HasSuffix(got, " of "+strconv.Itoa(want)) || len(d.find.matches) != want {
		t.Errorf("count %q, %d matches, want %d", got, len(d.find.matches), want)
	}
	if sel := d.yaml.SelectedText(); !strings.EqualFold(sel, "image") {
		t.Errorf("selected %q", sel)
	}

	w.Canvas().Focus(sh.view.search)
	cmdF(t, sh)
	if w.Canvas().Focused() == d.find.find {
		t.Error("⌘F from the list's search went to the YAML's field")
	}

	w.Canvas().Focus(d.find.find)
	d.find.find.escape()
	if d.find.find.Text != "" || w.Canvas().Focused() != d.yaml || !d.find.root.Visible() {
		t.Errorf("Escape in the field: text %q, keys in %T, bar shown %v", d.find.find.Text, w.Canvas().Focused(), d.find.root.Visible())
	}
	if d.yaml.OnEscape() {
		t.Error("the YAML's Escape was kept: the window's must close the drawer")
	}
}

// TestReleaseValuesFind (the owner's finding of 2026-10-25, "Es gibt keine Suche in Helm-Release
// Values"): the release drawer's values editor has the drawer's find bar over it for good, without
// a replace row; ⌘F in the editor puts the keys in its field, a search counts and selects the
// matches and follows the text as it is typed; ⌘F from the list stays the list's search.
func TestReleaseValuesFind(t *testing.T) {
	sh, w, _, _, _ := releaseDrawerShell(t, ThemeDark)
	rv := sh.drawer.kept[releaseValuesKey].(*releaseValues)
	if rv.find == nil || rv.find.replace != nil || !rv.find.root.Visible() {
		t.Fatalf("the values' bar: %+v", rv.find)
	}
	settle(w)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject
	if at(rv.find.root).Y >= at(rv.view).Y {
		t.Errorf("the bar at %v, the editor at %v: want it over the editor", at(rv.find.root), at(rv.view))
	}

	w.Canvas().Focus(sh.view.table)
	cmdF(t, sh)
	if w.Canvas().Focused() == rv.find.find {
		t.Error("⌘F from the list went to the values' field")
	}
	w.Canvas().Focus(rv.view)
	cmdF(t, sh)
	if w.Canvas().Focused() != rv.find.find {
		t.Fatalf("⌘F in the values: the keys are in %T", w.Canvas().Focused())
	}

	test.Type(rv.find.find, "image")
	want := strings.Count(strings.ToLower(rv.view.Text()), "image")
	if want == 0 {
		t.Fatal("the values name no image")
	}
	if got := rv.find.count.text; !strings.HasSuffix(got, " of "+strconv.Itoa(want)) || len(rv.find.matches) != want {
		t.Errorf("count %q, %d matches, want %d", got, len(rv.find.matches), want)
	}
	if sel := rv.view.SelectedText(); !strings.EqualFold(sel, "image") {
		t.Errorf("selected %q", sel)
	}

	lines := strings.Count(rv.view.Text(), "\n")
	rv.view.SetCursorPosition(lines+1, 0)
	test.Type(rv.view, "\nextra-image: two") // typed into the values
	if got := len(rv.find.matches); got != want+1 || rv.find.count.text != strconv.Itoa(want+1)+" found" {
		t.Errorf("after typing: %d matches, count %q, want %d", got, rv.find.count.text, want+1)
	}
}
