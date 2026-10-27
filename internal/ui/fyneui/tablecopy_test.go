package fyneui

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Copying the text of a list's cells with the drawer's copy icon (D-25, owner 2026-10-13): one
// icon per table, right after the text of the cell under the pointer, in every column but the
// blacklisted ones, copying the full text with the drawer's checkmark, never selecting the row.

// listScene is a settled list of the kinds fixture.
func listScene(t *testing.T, view string, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := kindsFake(t, view)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the "+view+" view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == view && sh.diag.total > 0
	})
	settle(w)
	return sh, w, q, f
}

// absRect returns an object's top left and bottom right in window coordinates.
func absRect(o fyne.CanvasObject) (fyne.Position, fyne.Position) {
	p := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	return p, p.Add(o.Size())
}

// middleOf is the centre of an object, in window coordinates.
func middleOf(o fyne.CanvasObject) fyne.Position {
	p, q := absRect(o)
	return fyne.NewPos((p.X+q.X)/2, (p.Y+q.Y)/2)
}

// hoverOver moves the pointer to a point and runs what it queued.
func hoverOver(w fyne.Window, q *mainQueue, p fyne.Position) {
	test.MoveMouse(w.Canvas(), p)
	q.drain()
}

// checkIconInCell fails unless the list's copy icon shows inside the cell, centred on the row, a
// padding after the cell's whole text, or over the cell's right end when the text leaves no room
// (atEnd), with the text as it is without the icon (A69: not shortened for it).
func checkIconInCell(t *testing.T, sh *shell, c *tableCell) (atEnd bool) {
	t.Helper()
	btn := sh.view.copyIcon.btn
	if !btn.Visible() {
		t.Fatalf("no copy icon over the cell %q", c.full)
	}
	b0, b1 := absRect(btn)
	c0, c1 := absRect(c)
	if b0.X < c0.X-0.5 || b1.X > c1.X+0.5 || b0.Y < c0.Y-0.5 || b1.Y > c1.Y+0.5 {
		t.Errorf("the icon %v–%v lies outside its cell %v–%v", b0, b1, c0, c1)
	}
	if d := (b0.Y + b1.Y - c0.Y - c1.Y) / 2; d < -0.5 || d > 0.5 {
		t.Errorf("the icon is %v off the row's middle", d)
	}
	side := b1.X - b0.X
	if side < 12 {
		t.Errorf("the icon is %v wide", side)
	}
	th := c.Theme()
	want := c0.X + th.Size(sizeCellPadding) + fyne.MeasureText(c.full, c.text.TextSize, c.text.TextStyle).Width + th.Size(theme.SizeNamePadding)
	if want+side > c1.X {
		want, atEnd = c1.X-side, true
	}
	if d := b0.X - want; d < -0.5 || d > 0.5 {
		t.Errorf("the icon of %q starts at %v, want %v (at the cell's end: %v)", c.full, b0.X, want, atEnd)
	}
	// The text keeps the cell's whole width (A69): whole when the icon follows it; at the cell's
	// end it runs under the icon, shortened with "…" only where the cell ends, as without the icon.
	if c.content.parts == nil && c.text.Text != "" {
		avail := c.Size().Width - 2*th.Size(sizeCellPadding)
		if want := ellipsize(c.full, avail, c.text.TextSize, c.text.TextStyle); c.text.Text != want {
			t.Errorf("the text of %q is %q with the icon, want %q as without it", c.full, c.text.Text, want)
		}
		if !atEnd && c.text.Text != c.full {
			t.Errorf("the icon follows the text but the text is shortened to %q", c.text.Text)
		}
	}
	return atEnd
}

// textCellOf returns a visible cell of the open view's column id that shows text worth copying
// (no placeholder): the column decides whether it has the icon.
func textCellOf(t *testing.T, sh *shell, id string) *tableCell {
	t.Helper()
	col := columnOf(t, sh, id)
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.Visible() && c.col == col && c.row >= 0 && c.copyText() != "" {
			return c
		}
	}
	t.Fatalf("no cell with text in the %s column", id)
	return nil
}

// TestListCopyIconOnHover: the pointer over a cut-off Name cell shows the icon over the cell's
// right end, the name not shortened for it (A69); over the checkbox, the warning triangle, the
// container squares, the ⋮, the gaps and the header none.
func TestListCopyIconOnHover(t *testing.T) {
	sh, w, q, f := listScene(t, "pods", ThemeDark)
	btn := sh.view.copyIcon.btn
	if btn.Visible() {
		t.Fatal("the copy icon shows before the pointer is over a cell")
	}
	crash := rowOf(t, f, "pods", podNamed(t, podCrashLoop).name)
	name := cellAt(t, sh, crash, columnOf(t, sh, "name"))
	hoverOver(w, q, middleOf(name))
	if !checkIconInCell(t, sh, name) {
		t.Errorf("the cut-off name %q has its icon before the cell's end", name.full)
	}
	if sh.view.hoverRow != crash {
		t.Errorf("the row band is on row %d, want %d", sh.view.hoverRow, crash)
	}

	// The other identifier columns have it too: after a short text, at the end of a narrow column
	// (Node, 104 points) when the text leaves no room.
	for _, id := range []string{"namespace", "node", "controlledBy"} {
		c := cellAt(t, sh, crash, columnOf(t, sh, id))
		hoverOver(w, q, middleOf(c))
		if checkIconInCell(t, sh, c) && id == "namespace" {
			t.Errorf("the icon of the namespace %q is at the cell's end, not after the text", c.full)
		}
	}

	// No text, no icon.
	for what, c := range map[string]*tableCell{
		"the checkbox":          cellAt(t, sh, crash, checkColumn),
		"the warning triangle":  cellAt(t, sh, crash, columnOf(t, sh, "warning")),
		"the container squares": cellAt(t, sh, crash, columnOf(t, sh, "containers")),
		"the ⋮":                 cellAt(t, sh, crash, sh.view.menuColumn()),
	} {
		hoverOver(w, q, middleOf(name))
		hoverOver(w, q, middleOf(c))
		if btn.Visible() {
			t.Errorf("the copy icon shows over %s", what)
		}
	}

	// The gap between two rows and the header row, where the table keeps its band, hide it; back
	// on the cell it shows again.
	hoverOver(w, q, middleOf(name))
	n0, _ := absRect(name)
	hoverOver(w, q, fyne.NewPos(middleOf(name).X, n0.Y-1))
	if btn.Visible() {
		t.Error("the copy icon stays in the gap above its cell")
	}
	hoverOver(w, q, middleOf(name))
	checkIconInCell(t, sh, name)
	t0, _ := absRect(sh.view.table)
	hoverOver(w, q, fyne.NewPos(middleOf(name).X, t0.Y+4))
	if btn.Visible() {
		t.Error("the copy icon stays while the pointer is on the header")
	}
	hoverOver(w, q, middleOf(name))
	shown := name.text.Text
	hoverOver(w, q, fyne.NewPos(20, 600)) // the sidebar
	if btn.Visible() {
		t.Error("the copy icon stays after the pointer left the table")
	}
	// The icon never changed the name: the same text with it and without it (A69).
	if name.text.Text != shown {
		t.Errorf("the name shows %q without the icon, %q with it", name.text.Text, shown)
	}
}

// TestListCopyIconAfterTheText: the owner's feedback (2026-10-13): in a wide Name column the icon
// sits right after the name, not at the column's far end; the name stays whole.
func TestListCopyIconAfterTheText(t *testing.T) {
	sh, w, q, _ := listScene(t, "nodes", ThemeDark)
	name := textCellOf(t, sh, "name")
	hoverOver(w, q, middleOf(name))
	if checkIconInCell(t, sh, name) {
		t.Fatalf("the icon of the node %q is at the cell's end", name.full)
	}
	b0, b1 := absRect(sh.view.copyIcon.btn)
	_, c1 := absRect(name)
	// The Name column keeps room after the name: 40 units at least since the Nodes list's usage
	// bars and Disk column took some of its width (2026-10-14, G12).
	if c1.X-b1.X < 40 {
		t.Errorf("the icon ends %v before the column's end, want it right after the name in a wide column", c1.X-b1.X)
	}
	c0, _ := absRect(name)
	if text := fyne.MeasureText(name.full, name.text.TextSize, name.text.TextStyle).Width; b0.X-(c0.X+name.text.Position().X+text) > 8 {
		t.Errorf("the icon starts %v after the name's end", b0.X-(c0.X+name.text.Position().X+text))
	}
}

// withUsage gives every pod of the fixture a CPU and a Memory value, so that the columns' rule,
// not an empty cell, decides about their icon.
func withUsage(t *testing.T, f *fakeEngine) {
	t.Helper()
	snap := f.tables["pods"]
	cpu, mem := -1, -1
	for i, c := range snap.Columns {
		switch c.ID {
		case model.ColumnCPU:
			cpu = i
		case model.ColumnMemory:
			mem = i
		}
	}
	if cpu < 0 || mem < 0 {
		t.Fatal("the pods have no CPU or Memory column")
	}
	for i := range snap.Rows {
		cells := append([]model.Cell(nil), snap.Rows[i].Cells...)
		for len(cells) <= max(cpu, mem) {
			cells = append(cells, model.Cell{})
		}
		cells[cpu], cells[mem] = model.Cell{Text: "0.012", Num: 12}, model.Cell{Text: model.FormatBytes(20447232), Num: 20447232}
		snap.Rows[i].Cells = cells
	}
}

// fixtureServiceObjects are three Services with cluster IPs.
func fixtureServiceObjects() []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for i, s := range []struct{ ns, name, ip string }{
		{"checkout", "checkout-api", "10.96.12.34"}, {"payments", "payments-worker", "10.96.40.7"}, {"storefront", "web-frontend", "10.96.200.11"},
	} {
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]any{"name": s.name, "namespace": s.ns, "uid": "uid-svc-" + s.name, "creationTimestamp": ts(time.Duration(2+i) * 24 * time.Hour)},
			"spec": map[string]any{"type": "ClusterIP", "clusterIP": s.ip, "selector": map[string]any{"app": s.name},
				"ports": []any{map[string]any{"name": "http", "port": int64(80), "targetPort": int64(8080), "protocol": "TCP"}}},
		}})
	}
	return out
}

// backupKind is a custom resource kind with an integer, a string and a date printer column.
var backupKind = kinds.CustomKind{
	Group: "example.com", Version: "v1", Plural: "backups", Kind: "Backup", Namespaced: true,
	Columns: []kinds.PrinterColumn{
		{Name: "Replicas", Type: "integer", JSONPath: ".spec.replicas"},
		{Name: "Host", Type: "string", JSONPath: ".spec.host"},
		{Name: "Ready", Type: "string", JSONPath: ".status.ready"},
		{Name: "Last Backup", Type: "date", JSONPath: ".status.lastBackup"},
	},
}

// backupsFake is the kinds fixture with the Backups view open: three Backups.
func backupsFake(t *testing.T) *fakeEngine {
	t.Helper()
	f := kindsFake(t, backupKind.Key())
	p := backupKind.Profile()
	f.kinds = append(f.kinds, kindInfo{Kind: p.Key, Title: p.Title, APIKind: p.Kind, Group: backupKind.Group, Version: backupKind.Version, Namespaced: true})
	var rows []model.Row
	for i, host := range []string{"db-0.backups.example.com", "db-1.backups.example.com", "db-2.backups.example.com"} {
		o := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.com/v1", "kind": "Backup",
			"metadata": map[string]any{"name": "nightly-" + string(rune('a'+i)), "namespace": "data-pipeline", "uid": "uid-backup-" + host, "creationTimestamp": ts(72 * time.Hour)},
			"spec":     map[string]any{"replicas": int64(3 + i), "host": host},
			"status":   map[string]any{"ready": "True", "lastBackup": ts(time.Duration(1+i) * time.Hour)},
		}}
		rows = append(rows, model.BuildRowWith(p, "demo", o, nil))
	}
	f.tables[p.Key] = &engine.TableSnapshot{
		View: p.Key, Context: "demo", Title: p.Title, Namespaced: true, Columns: p.Columns, Rows: rows,
		Sort: engine.SortSpec{Column: "name"}, Total: len(rows), Revision: 1, Synced: true,
	}
	return f
}

// TestListCopyIconByColumn: the owner's feedback (2026-10-13): a name, an IP, a node name, an
// Event's message have the icon; Age, CPU, Memory, Restarts and the other blacklisted columns do
// not, though their cells show text. A custom resource's integer and date printer columns have
// none, its string column has one.
func TestListCopyIconByColumn(t *testing.T) {
	for _, sc := range []struct {
		view      string
		fake      func(*testing.T) *fakeEngine
		with, not []string
	}{
		{"pods", func(t *testing.T) *fakeEngine { f := kindsFake(t, "pods"); withUsage(t, f); return f },
			[]string{"name", "namespace", "node", "controlledBy"}, []string{"age", "cpu", "memory", "restarts", "status", "qos"}},
		{"services", func(t *testing.T) *fakeEngine {
			f := kindsFake(t, "services")
			f.tables["services"] = kindSnapshot(t, "services", fixtureServiceObjects())
			return f
		}, []string{"name", "clusterIP", "ports", "selector", "type"}, []string{"age"}},
		{"events", func(t *testing.T) *fakeEngine { return kindsFake(t, "events") },
			[]string{"message", "involvedObject", "source", "type"}, []string{"count", "lastSeen"}},
		{"nodes", func(t *testing.T) *fakeEngine { return kindsFake(t, "nodes") },
			[]string{"name", "roles", "version"}, []string{"age", "conditions", "taints"}},
		{backupKind.Key(), backupsFake, []string{"name", "namespace", "host"}, []string{"replicas", "ready", "lastBackup"}},
	} {
		t.Run(sc.view, func(t *testing.T) {
			f := sc.fake(t)
			sh, w, q := newTestShell(t, f, Options{})
			q.until(t, "the "+sc.view+" view", func() bool {
				return sh.view.snap != nil && sh.view.snap.View == sc.view && sh.diag.total > 0
			})
			settle(w)
			btn := sh.view.copyIcon.btn
			for _, id := range sc.with {
				c := textCellOf(t, sh, id)
				hoverOver(w, q, middleOf(c))
				checkIconInCell(t, sh, c)
				sh.app.Clipboard().SetContent("")
				test.Tap(btn)
				if got := sh.app.Clipboard().Content(); got != c.full {
					t.Errorf("the %s cell copied %q, want %q", id, got, c.full)
				}
			}
			for _, id := range sc.not {
				c := textCellOf(t, sh, id)
				hoverOver(w, q, fyne.NewPos(20, 600))
				hoverOver(w, q, middleOf(c))
				if btn.Visible() {
					t.Errorf("the %s cell %q has the copy icon", id, c.full)
				}
			}
		})
	}
}

// TestCopyColumns: the blacklist by type and by id, and a custom resource's printer columns by
// their type (integer and number, date) and name.
func TestCopyColumns(t *testing.T) {
	for _, tt := range []struct {
		col  model.Column
		want bool
	}{
		{model.Column{ID: "name", Type: model.ColumnText}, true},
		{model.Column{ID: "clusterIP", Type: model.ColumnText}, true},
		{model.Column{ID: "message", Type: model.ColumnText}, true},
		{model.Column{ID: "type", Type: model.ColumnText}, true},
		{model.Column{ID: "pods", Type: model.ColumnText}, true},    // a claim's pods: their names
		{model.Column{ID: "pods", Type: model.ColumnNumber}, false}, // a workload's "3/3"
		{model.Column{ID: "age", Type: model.ColumnAge}, false},
		{model.Column{ID: "restarts", Type: model.ColumnNumber}, false},
		{model.Column{ID: model.ColumnCPU, Type: model.ColumnMetric}, false},
		{model.Column{ID: model.ColumnMemory, Type: model.ColumnMetric}, false},
		{model.Column{ID: "nextExecution", Type: model.ColumnUntil}, false},
		{model.Column{ID: "warning", Type: model.ColumnWarning}, false},
		{model.Column{ID: "status", Type: model.ColumnText}, false},
		{model.Column{ID: "qos", Type: model.ColumnText}, false},
		{model.Column{ID: "suspend", Type: model.ColumnText}, false},
	} {
		if got := copyColumn(tt.col); got != tt.want {
			t.Errorf("copyColumn(%s, %s) = %v, want %v", tt.col.ID, tt.col.Type, got, tt.want)
		}
	}
	want := map[string]bool{"name": true, "namespace": true, "replicas": false, "host": true, "ready": false, "lastBackup": false, "age": false}
	for _, c := range backupKind.Profile().Columns {
		if w, ok := want[c.ID]; !ok || copyColumn(c) != w {
			t.Errorf("the Backup column %s (%s): copy %v, want %v (known %v)", c.ID, c.Type, copyColumn(c), w, ok)
		}
	}
}

// TestListCopyIconPlaceholders: an empty cell and the placeholders show no text worth copying.
func TestListCopyIconPlaceholders(t *testing.T) {
	for text, want := range map[string]string{
		"web-7d4b9": "web-7d4b9", "": "", "-": "", "–": "", "—": "", "<none>": "", "  ": "", "0": "0",
	} {
		c := &tableCell{content: cellContent{text: text}}
		if got := c.copyText(); got != want {
			t.Errorf("copyText of %q = %q, want %q", text, got, want)
		}
	}
	marks := []model.CellMark{{Text: "app: running"}}
	for _, c := range []*tableCell{
		{content: cellContent{text: "1/1", marks: &marks}},
		{content: cellContent{text: "CrashLoopBackOff", kind: cellWarning}},
		{content: cellContent{kind: cellMenu}},
		{content: cellContent{kind: cellCheck}},
	} {
		if got := c.copyText(); got != "" {
			t.Errorf("a cell of kind %d copies %q", c.content.kind, got)
		}
	}
}

// TestListCopyIconCopiesTheFullText: a click on the icon of a cut-off cell copies its full text,
// turns the icon into the checkmark and shows the drawer's toast; the row is not selected, no
// checkbox is toggled and the drawer stays closed. The pointer's way onto the icon keeps the row
// band and the icon; off the table both go.
func TestListCopyIconCopiesTheFullText(t *testing.T) {
	sh, w, q, f := listScene(t, "events", ThemeDark)
	sh.copyConfirm = time.Hour // the checkmark outlasts the steps below; fireTimer ends it
	tv := sh.view
	btn := tv.copyIcon.btn
	var cut *tableCell
	for _, c := range findAll[*tableCell](tv.table) {
		if c.Visible() && c.truncated && c.col == columnOf(t, sh, "message") {
			cut = c
			break
		}
	}
	if cut == nil {
		t.Fatal("no cut-off message on screen")
	}
	hoverOver(w, q, middleOf(cut))
	checkIconInCell(t, sh, cut)

	// The pointer moves onto the icon: the table sees it leave, the icon keeps band and icon.
	hoverOver(w, q, middleOf(btn))
	if !btn.Visible() || tv.hoverRow != cut.row {
		t.Fatalf("on the icon: visible %v, band on row %d, want %d", btn.Visible(), tv.hoverRow, cut.row)
	}

	sh.app.Clipboard().SetContent("")
	toasts := len(sh.toasts.Objects)
	test.TapCanvas(w.Canvas(), middleOf(btn))
	q.drain()
	if got := sh.app.Clipboard().Content(); got != cut.full || got == cut.text.Text {
		t.Errorf("the clipboard holds %q, want the full text %q (shown %q)", got, cut.full, cut.text.Text)
	}
	if !btn.done {
		t.Error("no checkmark after the copy")
	}
	if len(sh.toasts.Objects) != toasts+1 {
		t.Error("copying showed no toast")
	}
	if calls := f.callsOf("ui.select"); len(calls) != 0 {
		t.Errorf("the click on the icon selected: %d ui.select", len(calls))
	}
	if sh.ui.Drawer != nil || len(sh.ui.Selected) != 0 || tv.check.state != checkNone {
		t.Errorf("the click on the icon opened %v, selected %v, checkbox state %d", sh.ui.Drawer, sh.ui.Selected, tv.check.state)
	}
	if sh.clipSecret != "" || sh.clipTimer != nil {
		t.Error("a list's copy is cleared from the clipboard like a Secret's value")
	}

	// A right-click on the icon opens the row's actions menu, as anywhere else on the row.
	tv.menu, tv.menuModel = nil, nil
	test.TapSecondary(btn)
	q.until(t, "the row menu", func() bool { return tv.menu != nil && tv.menuModel != nil })
	tv.menu.Hide()

	// Back on the cell the icon shows again, still the checkmark; then the copy icon.
	hoverOver(w, q, middleOf(cut))
	if !btn.Visible() || !btn.done {
		t.Errorf("back on the cell: visible %v, checkmark %v", btn.Visible(), btn.done)
	}
	if !fireTimer(tv.copyIcon.done) {
		t.Fatal("no checkmark timer armed")
	}
	q.until(t, "the copy icon to return", func() bool { return !btn.done })
	if !btn.Visible() {
		t.Error("the icon went with the checkmark although the pointer is on the cell")
	}

	// Another cell drops the checkmark at once.
	test.TapCanvas(w.Canvas(), middleOf(btn))
	other := cellAt(t, sh, cut.row, columnOf(t, sh, "type"))
	hoverOver(w, q, middleOf(other))
	if btn.done {
		t.Error("the checkmark moved on with the icon")
	}

	// From the icon off the table: icon and band go.
	hoverOver(w, q, middleOf(btn))
	hoverOver(w, q, fyne.NewPos(20, 600))
	if btn.Visible() || tv.hoverRow != -1 {
		t.Errorf("off the table: icon %v, band on row %d", btn.Visible(), tv.hoverRow)
	}
}

// TestListCopyIconFollowsTheCells: a refresh keeps the icon on its cell and the icon copies the
// text the cell shows then; a scroll hides it until the pointer moves, then it is on the cell
// under the pointer.
func TestListCopyIconFollowsTheCells(t *testing.T) {
	f := connectedFake(t, 10000)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	tv := sh.view
	btn := tv.copyIcon.btn
	name := cellAt(t, sh, 3, columnOf(t, sh, "name"))
	at := middleOf(name)
	hoverOver(w, q, at)
	checkIconInCell(t, sh, name)

	tv.refreshAges()
	if !btn.Visible() {
		t.Fatal("the icon went with a refresh")
	}
	snap := *tv.snap
	snap.Rows = append([]model.Row(nil), snap.Rows...)
	row := snap.Rows[3]
	row.Cells = append([]model.Cell(nil), row.Cells...)
	row.Cells[0].Text = "renamed-by-a-watch-event"
	snap.Rows[3] = row
	snap.Revision++
	go f.setTable("pods", &snap)
	q.until(t, "the new snapshot", func() bool { return tv.snap.Revision == snap.Revision })
	if !btn.Visible() {
		t.Fatal("the icon went with a watch event")
	}
	checkIconInCell(t, sh, name) // after the new name's end
	sh.app.Clipboard().SetContent("")
	test.Tap(btn)
	if got := sh.app.Clipboard().Content(); got != "renamed-by-a-watch-event" {
		t.Errorf("after the watch event the icon copied %q", got)
	}

	// An agent's ui.navigate scrolls to the row it selects (scrollTo), the wheel through the
	// table's scroller; either moves the cells under a pointer that stays.
	var scroller *container.Scroll
	for _, o := range test.WidgetRenderer(tv.table).Objects() {
		if s, ok := o.(*container.Scroll); ok && s.OnScrolled != nil {
			scroller = s
		}
	}
	if scroller == nil {
		t.Fatal("no scroller in the table")
	}
	// What a wheel event does, without the scroll bar's timer (TestTooltips): the scroller's
	// offset, its OnScrolled, its refresh.
	wheel := func(dy float32) {
		scroller.Offset = scroller.Offset.AddXY(0, dy)
		scroller.OnScrolled(scroller.Offset)
		scroller.Refresh()
	}
	for i, scroll := range []func(){
		func() { tv.scrollTo(400) },
		// 120 rows up, a whole number of rows (a cell and the padding between rows), so that the
		// pointer stays in a row's middle and never lands in the gap between two, where the table
		// sees no row.
		func() { wheel(-120 * (name.Size().Height + theme.Padding())) },
	} {
		hoverOver(w, q, at)
		if !btn.Visible() {
			t.Fatalf("scroll %d: no icon before it", i)
		}
		scroll()
		q.drain()
		if btn.Visible() {
			t.Fatalf("scroll %d: the icon stayed while the cells moved under it", i)
		}
		hoverOver(w, q, at.AddXY(1, 0))
		under := tv.cellUnderPointer(tv.hoverRow, tv.hoverCol)
		if under == nil || under.row < 100 {
			t.Fatalf("scroll %d: the pointer is over %+v", i, under)
		}
		checkIconInCell(t, sh, under)
		sh.app.Clipboard().SetContent("")
		test.Tap(btn)
		if got := sh.app.Clipboard().Content(); got != under.full {
			t.Errorf("scroll %d: the icon copied %q, want %q", i, got, under.full)
		}
		at = at.AddXY(2, 0)
	}

	// A cell that went away under a resting icon is not copied: the icon checks its cell first.
	hoverOver(w, q, at)
	tv.table.ScrollTo(widget.TableCellID{Row: 9000, Col: 0}) // Fyne's own scroll, as its keys do
	sh.app.Clipboard().SetContent("")
	test.Tap(btn)
	if got := sh.app.Clipboard().Content(); got != "" {
		if under := tv.cellUnderPointer(tv.hoverRow, tv.hoverCol); under == nil || under.full != got {
			t.Errorf("the icon copied %q from a cell no longer under it", got)
		}
	}
}

// TestSecretsListCopiesNoValue: the Secrets list holds no values. Every cell with text of every
// column but Age copies with the icon, a plain copy, and no copy contains a value of the
// fixture's Secrets, decoded or base64.
func TestSecretsListCopiesNoValue(t *testing.T) {
	f, _ := secretsFake(t)
	f.ui.Selected, f.ui.Drawer = nil, nil
	sh, w, q := newTestShell(t, f, Options{})
	sh.toastTime = 5 * time.Millisecond // the toasts of earlier copies would pile up over the list
	q.until(t, "the secrets", func() bool { return sh.view.snap != nil && sh.view.snap.View == "secrets" && sh.diag.total > 0 })
	settle(w)
	// The values, decoded and as stored; the short ones ("checkout", "admin", "true") read as
	// names elsewhere, so only the distinctive ones are looked for.
	var values []string
	for _, o := range fixtureSecretObjects() {
		for _, v := range model.NestedMap(o.Object, "data") {
			s, _ := v.(string)
			raw, _ := base64.StdEncoding.DecodeString(s)
			for _, x := range []string{s, string(raw)} {
				if len(x) >= 10 {
					values = append(values, x)
				}
			}
		}
	}
	if len(values) < 10 {
		t.Fatalf("only %d values to look for", len(values))
	}
	copied := map[string]int{}
	for _, c := range findAll[*tableCell](sh.view.table) {
		vis, data := sh.view.dataColumn(c.col)
		if !c.Visible() || !data || c.row < 0 || sh.view.copyIcon.textOf(c) == "" {
			continue
		}
		hoverOver(w, q, middleOf(c))
		btn := sh.view.copyIcon.btn
		if !btn.Visible() {
			c0, c1 := absRect(c)
			t0, t1 := absRect(sh.view.table)
			t.Fatalf("no icon over %q: cell %v–%v, table %v–%v", c.full, c0, c1, t0, t1)
		}
		sh.app.Clipboard().SetContent("")
		test.TapCanvas(w.Canvas(), middleOf(btn))
		got := sh.app.Clipboard().Content()
		if got != c.full {
			t.Errorf("copied %q, want %q", got, c.full)
		}
		for _, v := range values {
			if strings.Contains(got, v) {
				t.Errorf("a list cell copied a Secret's value: %q", got)
			}
		}
		if sh.clipSecret != "" {
			t.Errorf("the copy of %q was a Secret's", got)
		}
		copied[sh.view.snap.Columns[sh.view.cols[vis]].ID]++
		q.until(t, "the toast to go", func() bool { return len(sh.toasts.Objects) == 0 })
	}
	for _, id := range []string{"name", "namespace", "labels", "keys", "type"} {
		if copied[id] == 0 {
			t.Errorf("no cell of the %s column copied (copied %v)", id, copied)
		}
	}
	if copied["age"] != 0 {
		t.Error("an Age cell has the copy icon")
	}
}

// TestListCopyIconHasNoCommand: copying a cell, like copying a drawer value, dispatches no
// command: nothing an agent could run, nothing in the audit log.
func TestListCopyIconHasNoCommand(t *testing.T) {
	sh, w, q, f := listScene(t, "pods", ThemeDark)
	hoverOver(w, q, middleOf(cellAt(t, sh, 1, columnOf(t, sh, "name"))))
	hoverOver(w, q, middleOf(sh.view.copyIcon.btn))
	before := f.mark()
	test.TapCanvas(w.Canvas(), middleOf(sh.view.copyIcon.btn))
	q.drain()
	if sh.app.Clipboard().Content() == "" {
		t.Fatal("nothing copied")
	}
	for _, c := range f.callsSince(before) {
		t.Errorf("copying a cell dispatched %s", c.ID)
	}
}

// renderListCopy shows the Nodes list with the pointer on a node's name and the copy icon right
// after it, far from the wide column's end; after the copy, the checkmark and the toast.
func renderListCopy(t *testing.T, mode ThemeMode, confirm bool, name string) {
	sh, w, q, f := listScene(t, "nodes", mode)
	cell := cellAt(t, sh, rowOf(t, f, "nodes", cordonedNode), columnOf(t, sh, "name"))
	hoverOver(w, q, middleOf(cell))
	if checkIconInCell(t, sh, cell) {
		t.Fatalf("the icon of %q is at the cell's end", cell.full)
	}
	if confirm {
		hoverOver(w, q, middleOf(sh.view.copyIcon.btn))
		test.TapCanvas(w.Canvas(), middleOf(sh.view.copyIcon.btn))
		q.drain()
	}
	savePNG(t, w, name)
}

func TestRenderListCopyIcon(t *testing.T) {
	renderListCopy(t, ThemeDark, false, "list_copy_hover_dark.png")
	renderListCopy(t, ThemeLight, false, "list_copy_hover_light.png")
	renderListCopy(t, ThemeDark, true, "list_copy_done_dark.png")
	renderListCopy(t, ThemeLight, true, "list_copy_done_light.png")
}
