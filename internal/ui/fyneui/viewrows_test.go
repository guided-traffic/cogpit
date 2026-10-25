package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// Over the real engine and a small demo cluster: what an agent reads with view_rows is what the
// window's table shows, for a view filtered by namespace and search, sorted descending, its
// columns reordered and some hidden: the same columns left to right, the same rows top to bottom,
// each cell's text as the table draws it.
func TestViewRowsAreTheTablesRows(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 3
	s := newTestEngine(engine.Options{Connector: conn})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the Pods view", func() bool {
		tbl := s.Table("pods")
		return tbl != nil && tbl.Synced && len(tbl.Rows) > 0 && sh.view.snap != nil
	})
	ns := s.Table("pods").Rows[0].Ref.Namespace
	dispatch := func(id string, params any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	dispatch("ui.setNamespace", map[string]any{"namespaces": []string{ns}})
	dispatch("ui.setFilter", map[string]any{"filter": "-", "regex": false})
	dispatch("ui.sort", map[string]any{"column": "name", "desc": true})
	dispatch("ui.setColumnOrder", map[string]any{"order": []string{"status", "name", "node"}})
	dispatch("ui.setColumns", map[string]any{"hidden": []string{"age", "namespace", "warning", "cpu", "memory"}}) // ages tick: compare what stays
	until("the table to follow", func() bool {
		snap := sh.view.snap
		return snap != nil && snap.View == "pods" && snap.Filter == "-" && slices.Equal(snap.Namespaces, []string{ns}) &&
			snap.Sort.Desc && snap.Order != nil && len(snap.Rows) > 1 && visibleIDs(sh.view)[0] == "status"
	})

	var got engine.ViewRows
	until("view.rows and the table on one snapshot", func() bool { // the demo's metrics bring new snapshots
		raw, _ := json.Marshal(map[string]any{"limit": 500})
		res, err := s.Dispatch(context.Background(), engine.Invocation{ID: "view.rows", Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res)
		got = engine.ViewRows{}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		q.drain()
		return got.Revision == sh.view.snap.Revision
	})
	ids := make([]string, len(got.Columns))
	for i, c := range got.Columns {
		ids[i] = c.ID
	}
	if shown := visibleIDs(sh.view); !slices.Equal(ids, shown) {
		t.Fatalf("view.rows columns %v, the table shows %v", ids, shown)
	}
	if len(got.Rows) != len(sh.view.snap.Rows) || got.Shown != len(sh.view.snap.Rows) {
		t.Fatalf("view.rows has %d rows (shown %d), the table %d", len(got.Rows), got.Shown, len(sh.view.snap.Rows))
	}
	cell := newTableCell(sh.view)
	for r, row := range got.Rows {
		if !row.Ref.Same(sh.view.snap.Rows[r].Ref) {
			t.Fatalf("row %d: view.rows %v, the table %v", r, row.Ref, sh.view.snap.Rows[r].Ref)
		}
		for c, text := range row.Cells {
			sh.view.updateCell(widget.TableCellID{Row: r, Col: firstDataColumn + c}, cell) // as the table draws the cell
			if cell.full != text {
				t.Errorf("row %d (%s), column %s: view.rows %q, the table %q", r, row.Ref.Name, ids[c], text, cell.full)
			}
		}
	}
	for i := 1; i < len(got.Rows); i++ {
		if got.Rows[i-1].Ref.Name < got.Rows[i].Ref.Name {
			t.Fatalf("rows not sorted by name, descending: %s before %s", got.Rows[i-1].Ref.Name, got.Rows[i].Ref.Name)
		}
	}
}
