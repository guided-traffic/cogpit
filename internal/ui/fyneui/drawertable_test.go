package fyneui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestDrawerTableLinks: a table cell that names an object of a kind the view can show is a link
// that navigates to it; one whose kind Cogpit has no view for stays plain text, as drawer values
// do.
func TestDrawerTableLinks(t *testing.T) {
	f := connectedFake(t, 3)
	ref := f.tables["pods"].Rows[0].Ref
	node := &model.ObjectRef{Kind: "nodes", Name: "worker-a-01"}
	secret := &model.ObjectRef{Kind: "secrets", Namespace: ref.Namespace, Name: "db-password"}
	f.details[ref.String()] = &engine.DetailModel{
		Ref: ref, Title: "Pod: " + ref.Name, Events: []engine.EventItem{},
		Sections: []model.DetailSection{{Title: "Placement", Table: model.CellTable(
			[]model.Column{{ID: "kind", Title: "Kind"}, {ID: "name", Title: "Name"}},
			[][]model.Cell{{{Text: "Node"}, {Text: "worker-a-01", Link: node}}, {{Text: "Secret"}, {Text: "db-password", Link: secret}}},
		)}},
	}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer and the kinds", func() bool {
		return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 && len(sh.kinds) > 0
	})

	links := drawerLinks(sh)
	if links["worker-a-01"] == nil {
		t.Fatalf("the node cell is not a link: links %v, texts %q", keys(links), drawerTexts(sh))
	}
	if links["db-password"] != nil || !slices.Contains(drawerTexts(sh), "db-password") {
		t.Errorf("a cell naming a kind without a view must be plain text: links %v", keys(links))
	}
	test.Tap(links["worker-a-01"])
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != (model.ObjectRef{Context: "demo", Kind: "nodes", Name: "worker-a-01"}) {
		t.Errorf("node link %+v", got)
	}
}

// TestDrawerLinksFollowTheKinds: a drawer shown before the cluster's kinds are read offers every
// link (nothing says a kind has no view yet); once they are read, a cell naming a kind without a
// view is plain text. The test of the links above failed once on Windows (CI, 2026-10-23), where
// the drawer was built before the kinds came.
func TestDrawerLinksFollowTheKinds(t *testing.T) {
	f := connectedFake(t, 3)
	ref := f.tables["pods"].Rows[0].Ref
	secret := &model.ObjectRef{Kind: "secrets", Namespace: ref.Namespace, Name: "db-password"}
	f.details[ref.String()] = &engine.DetailModel{
		Ref: ref, Title: "Pod: " + ref.Name, Events: []engine.EventItem{},
		Sections: []model.DetailSection{{Title: "Volumes", Table: model.CellTable(
			[]model.Column{{ID: "kind", Title: "Kind"}, {ID: "name", Title: "Name"}},
			[][]model.Cell{{{Text: "Secret"}, {Text: "db-password", Link: secret}}},
		)}},
	}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	gate := make(chan struct{})
	f.kindsGate = gate
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })
	if len(sh.kinds) != 0 {
		t.Fatal("the kinds came before the drawer: the test needs them late")
	}
	close(gate)
	q.until(t, "the kinds", func() bool { return len(sh.kinds) > 0 })
	if links := drawerLinks(sh); links["db-password"] != nil {
		t.Errorf("after the kinds came, a cell naming a kind without a view is still a link: links %v", keys(links))
	}
}

// TestDrawerTableLines: a cell of several lines shows them one under the other from the row's top,
// and the next row starts below the tallest cell.
func TestDrawerTableLines(t *testing.T) {
	f := connectedFake(t, 3)
	ref := f.tables["pods"].Rows[0].Ref
	f.details[ref.String()] = &engine.DetailModel{
		Ref: ref, Title: "Pod: " + ref.Name, Events: []engine.EventItem{},
		Sections: []model.DetailSection{{Title: "Rules", Table: &model.DetailTable{
			Columns: []string{"Resources", "Verbs"},
			Rows:    [][]string{{"pods\npods/log", "get\nlist\nwatch"}, {"configmaps", "update"}},
		}}},
	}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 })

	pos := map[string]float32{}
	for _, l := range findAll[*textLine](sh.drawer.details) {
		pos[l.text] = fyne.CurrentApp().Driver().AbsolutePositionForObject(l).Y
	}
	for _, s := range []string{"pods", "pods/log", "get", "list", "watch", "configmaps", "update"} {
		if _, ok := pos[s]; !ok {
			t.Fatalf("no line %q in %q", s, drawerTexts(sh))
		}
	}
	if pos["pods"] != pos["get"] || pos["pods/log"] <= pos["pods"] || pos["watch"] <= pos["list"] {
		t.Errorf("lines not one under the other from the row's top: %v", pos)
	}
	if pos["configmaps"] <= pos["watch"] || pos["configmaps"] != pos["update"] {
		t.Errorf("the next row must start below the tallest cell: %v", pos)
	}
}

// TestCapWidths: columns that do not fit give up width from the widest down; the narrow ones keep
// theirs, and columns that fit are left alone.
func TestCapWidths(t *testing.T) {
	w := []float32{150, 45, 110, 125, 165}
	capWidths(w, 485)
	if want := []float32{110, 45, 110, 110, 110}; !slices.Equal(w, want) {
		t.Errorf("capped %v, want %v", w, want)
	}
	fits := []float32{80, 40}
	capWidths(fits, 200)
	if !slices.Equal(fits, []float32{80, 40}) {
		t.Errorf("columns that fit changed: %v", fits)
	}
	capWidths(nil, 10)
	w = []float32{200, 40, 120, 30}
	capWidths(w, 250)
	if !slices.Equal(w, []float32{90, 40, 90, 30}) {
		t.Errorf("capped widths %v", w)
	}
	w = []float32{200, 40}
	capWidths(w, 10)
	if !slices.Equal(w, []float32{5, 5}) {
		t.Errorf("widths in too little room %v", w)
	}
}

// TestDrawerCellTable: a drawer table of cells (model.CellTable) draws a cell with a link as a
// link that navigates, a status in its colour, and the warning column as an icon whose tooltip
// is the reason, under no title; Rows stays the text agents read.
func TestDrawerCellTable(t *testing.T) {
	f := kindsFake(t, "nodes")
	ref := nodeDrawerRef
	pod := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	tbl := model.CellTable([]model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "warning", Title: "Warning", Type: model.ColumnWarning},
		{ID: "status", Title: "Status", Type: model.ColumnText},
	}, [][]model.Cell{
		{{Text: pod.Name, Link: &pod}, {Text: "CrashLoopBackOff; 4 restarts", Num: 1, Status: model.StatusWarning}, {Text: "CrashLoopBackOff", Status: model.StatusError}},
		{{Text: "web-1", Link: &model.ObjectRef{Kind: "secrets", Namespace: "shop", Name: "web-1"}}, {}, {Text: "Running", Status: model.StatusOK}},
	})
	f.details[ref.String()] = &engine.DetailModel{
		Ref: ref, Title: "Node: " + ref.Name, Events: []engine.EventItem{},
		Sections: []model.DetailSection{{Title: "Pods", Table: tbl}},
	}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer and the cluster's kinds", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(findAll[*tipIcon](sh.drawer.details)) > 0
	})
	settle(w)

	texts := drawerTexts(sh)
	if slices.Contains(texts, "Warning") || !slices.Contains(texts, "Name") || !slices.Contains(texts, "Status") {
		t.Errorf("the warning column has a title, or the others none: %q", texts)
	}
	if !slices.Contains(texts, "web-1") {
		t.Errorf("a link to a kind without a view must be plain text: %q", texts)
	}
	for _, l := range findAll[*textLine](sh.drawer.details) {
		if l.text == "CrashLoopBackOff" && l.color != statusColorName(model.StatusError) {
			t.Errorf("status cell colour %q", l.color)
		}
	}
	icons := findAll[*tipIcon](sh.drawer.details)
	if len(icons) != 1 || icons[0].tip != "CrashLoopBackOff; 4 restarts" {
		t.Fatalf("warning icons %+v", icons)
	}

	sh.tip.delay = time.Millisecond
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(icons[0]).AddXY(2, icons[0].Size().Height/2)
	test.MoveMouse(w.Canvas(), pos)
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == "CrashLoopBackOff; 4 restarts" })

	link := drawerLinks(sh)[pod.Name]
	if link == nil {
		t.Fatalf("no link for the pod among %v", keys(drawerLinks(sh)))
	}
	test.Tap(link)
	q.until(t, "ui.navigate", func() bool { return len(f.callsOf("ui.navigate")) == 1 })
	want := pod
	want.Context = ref.Context
	if got := decodeParams[refParams](t, f.callsOf("ui.navigate")[0]).Ref; got != want {
		t.Errorf("pod link %+v, want %+v", got, want)
	}
}
