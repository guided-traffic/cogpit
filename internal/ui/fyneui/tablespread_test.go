package fyneui

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// spreadExtra shares the free points among the columns in need: each in full while the surplus
// lasts, the smallest needs first and the rest in equal whole shares when it does not, nothing
// for a column without need; what is left over is the caller's (the flex column's).
func TestSpreadExtra(t *testing.T) {
	for _, c := range []struct {
		name    string
		need    []float32
		surplus float32
		want    []float32
	}{
		{"every need met, the rest left over", []float32{0, 120, 0, 40}, 600, []float32{0, 120, 0, 40}},
		{"none cut", []float32{0, 0, 0}, 600, []float32{0, 0, 0}},
		{"no surplus", []float32{50, 80}, 0, []float32{0, 0}},
		{"too little: equal shares", []float32{300, 400}, 200, []float32{100, 100}},
		{"too little: the small need met, the rest shared", []float32{30, 400, 500}, 230, []float32{30, 100, 100}},
		{"a fractional need met whole", []float32{12.5, 0}, 100, []float32{12.5, 0}},
		{"fewer points than columns", []float32{10, 10, 10}, 2, []float32{0, 0, 0}},
		{"no columns", nil, 100, []float32{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := spreadExtra(c.need, c.surplus)
			if !slices.Equal(got, c.want) {
				t.Fatalf("spreadExtra(%v, %v) = %v, want %v", c.need, c.surplus, got, c.want)
			}
			sum := float32(0)
			for i, e := range got {
				sum += e
				if e > c.need[i] {
					t.Errorf("column %d got %v, more than its need %v", i, e, c.need[i])
				}
			}
			if sum > c.surplus {
				t.Errorf("gave %v of %v", sum, c.surplus)
			}
		})
	}
}

// acmeHosts are an Ingress's hosts, as many as the column is to be cut by (synthetic names only).
func acmeHosts(n int) []model.WebLink {
	out := make([]model.WebLink, n)
	for i := range out {
		h := "shop-" + strconv.Itoa(i+1) + ".acme.example.net"
		out[i] = model.WebLink{Text: h, URL: "https://" + h}
	}
	return out
}

// spreadIngresses is an Ingresses view whose Hosts are wider than the column's 340 points: hosts
// many each, the first row's longer still.
func spreadIngresses(hosts int, revision uint64) *engine.TableSnapshot {
	cols := []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "loadBalancers", Title: "Load Balancers", Type: model.ColumnText},
		{ID: "hosts", Title: "Hosts", Type: model.ColumnText, Width: 340},
		{ID: "age", Title: "Age", Type: model.ColumnAge},
	}
	snap := &engine.TableSnapshot{View: "ingresses", Context: "demo", Title: "Ingresses", Namespaced: true, Columns: cols,
		Sort: engine.SortSpec{Column: "name"}, Revision: revision, Synced: true}
	for i, name := range []string{"acme-api", "acme-shop", "acme-static"} {
		n := hosts
		if i == 0 {
			n++
		}
		snap.Rows = append(snap.Rows, model.Row{
			Ref: model.ObjectRef{Context: "demo", Kind: "ingresses", Namespace: "acme", Name: name},
			Cells: []model.Cell{{Text: name}, {Text: "acme"}, {Text: "203.0.113.1" + strconv.Itoa(i)},
				model.WebCell(acmeHosts(n)), {Num: fixedNow.Unix() - 3600}},
		})
	}
	snap.Total = len(snap.Rows)
	return snap
}

// spreadFake shows the Ingresses of spreadIngresses in a window width points wide.
func spreadFake(t *testing.T, hosts int, width float32) (*fakeEngine, *shell, fyne.Window, *mainQueue) {
	t.Helper()
	f := connectedFake(t, 20)
	f.kinds = append(f.kinds, kindInfo{Kind: "ingresses", Title: "Ingresses", APIKind: "Ingress", Group: "networking.k8s.io", Version: "v1", Namespaced: true})
	f.tables["ingresses"] = spreadIngresses(hosts, 1)
	f.ui.View = "ingresses"
	sh, w, q := newTestShell(t, f, Options{})
	w.Resize(fyne.NewSize(width, 900))
	q.until(t, "the Ingresses", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "ingresses"
	})
	settle(w)
	return f, sh, w, q
}

// widthOf is the width the table gives the view's column id, by its place among the visible ones.
func spreadWidthOf(t *testing.T, tv *tableView, id string) float32 {
	t.Helper()
	for i, ci := range tv.cols {
		if tv.snap.Columns[ci].ID == id {
			return tv.setWidths[firstDataColumn+i]
		}
	}
	t.Fatalf("no visible column %s", id)
	return 0
}

// filled reports whether the table's columns, with their gaps and the scroll bar's room, fill the
// table's width.
func filled(tv *tableView) bool {
	th := tv.table.Theme()
	sum := th.Size(theme.SizeNameScrollBar)
	for _, w := range tv.setWidths {
		sum += w
	}
	sum += float32(len(tv.setWidths)-1) * th.Size(theme.SizeNamePadding)
	d := sum - tv.table.Size().Width
	return d > -1 && d < 1
}

// A wide window: the cut Hosts column takes what its widest host list wants, Name the rest, and
// the columns fill the table; with the preference off, Hosts keeps its 340 points and Name takes
// it all (the owner's QW7 at 2000 points). A column the user sized keeps the user's width.
func TestFitColumnsSpreadsTheFreeWidth(t *testing.T) {
	f, sh, w, q := spreadFake(t, 4, 2000)
	tv := sh.view
	want := tv.contentWidth(3, len(tv.snap.Rows))
	if want <= 340 {
		t.Fatalf("the hosts want %v: the fixture does not cut the column", want)
	}
	if got := spreadWidthOf(t, tv, "hosts"); got != want {
		t.Fatalf("Hosts %v wide, want its content's %v", got, want)
	}
	name := spreadWidthOf(t, tv, "name")
	if name <= defaultColumnWidths["name"] || !filled(tv) {
		t.Fatalf("Name %v wide (its own 220): the rest is Name's and the columns fill the table %v", name, tv.table.Size().Width)
	}
	if got := spreadWidthOf(t, tv, "namespace"); got != defaultColumnWidths["namespace"] {
		t.Errorf("Namespace, not cut, %v wide, want its own %v", got, defaultColumnWidths["namespace"])
	}

	// Off: today's widths, at once.
	off := engine.DefaultPreferences()
	off.TableSpreadWidth = false
	f.update(func(u *engine.UIState) { u.Preferences = &off })
	q.until(t, "Hosts at its own width", func() bool { return spreadWidthOf(t, tv, "hosts") == 340 })
	if got := spreadWidthOf(t, tv, "name"); got != name+want-340 || !filled(tv) {
		t.Fatalf("with the preference off Name is %v wide, want all the free width, %v", got, name+want-340)
	}
	// On again: measured at once.
	on := engine.DefaultPreferences()
	f.update(func(u *engine.UIState) { u.Preferences = &on })
	q.until(t, "Hosts at its content's width", func() bool { return spreadWidthOf(t, tv, "hosts") == want })

	// A column the user sized keeps the user's width and takes no share.
	tv.userWidths["ingresses/hosts"] = 300
	tv.fitWidth = 0
	tv.fitColumns(tv.table.Size().Width)
	if got := spreadWidthOf(t, tv, "hosts"); got != 300 || !filled(tv) {
		t.Fatalf("the user's Hosts %v wide, want 300", got)
	}
	settle(w)
}

// No column cut: every column keeps its own width and Name takes all the free width, as with the
// preference off.
func TestFitColumnsNoneCut(t *testing.T) {
	_, sh, _, _ := spreadFake(t, 0, 2000)
	tv := sh.view
	if want := tv.contentWidth(3, len(tv.snap.Rows)); want > 340 {
		t.Fatalf("the hosts want %v: the fixture cuts the column", want)
	}
	if got := spreadWidthOf(t, tv, "hosts"); got != 340 {
		t.Fatalf("Hosts %v wide, want its own 340", got)
	}
	if got := spreadWidthOf(t, tv, "name"); got <= 1000 || !filled(tv) {
		t.Fatalf("Name %v wide: want all the free width, the columns filling the table", got)
	}
}

// Too little free width for every cut column: the frame is filled, no column is wider than its
// content wants, and Name keeps its own width.
func TestFitColumnsSpreadsTooLittle(t *testing.T) {
	_, sh, _, _ := spreadFake(t, 12, 1440)
	tv := sh.view
	want := tv.contentWidth(3, len(tv.snap.Rows))
	hosts, name := spreadWidthOf(t, tv, "hosts"), spreadWidthOf(t, tv, "name")
	if hosts <= 340 || hosts >= want {
		t.Fatalf("Hosts %v wide: more than its 340, less than its content's %v", hosts, want)
	}
	if name != defaultColumnWidths["name"] || !filled(tv) {
		t.Fatalf("Name %v wide, want its own %v with the frame filled", name, defaultColumnWidths["name"])
	}
}

// Rows that change while the view is open leave the widths as they are, though they would be
// measured wider now; the next open measures them again. A view opened before its rows are in is
// measured when they come.
func TestSpreadWidthOnlyWhenTheViewOpens(t *testing.T) {
	f, sh, w, q := spreadFake(t, 4, 2000)
	tv := sh.view
	before := slices.Clone(tv.setWidths)

	f.setTable("ingresses", spreadIngresses(5, 2)) // wider hosts while the view is open
	q.until(t, "the new rows", func() bool { return tv.snap.Revision == 2 })
	settle(w)
	if !slices.Equal(tv.setWidths, before) {
		t.Fatalf("the widths followed rows changed while the view is open: %v, were %v", tv.setWidths, before)
	}

	mustOpen := func(view string) {
		t.Helper()
		sh.dispatch("ui.openView", openViewParams{View: view}, nil)
		q.until(t, "the "+view+" view", func() bool { return tv.snap != nil && tv.snap.View == view })
		settle(w)
	}
	mustOpen("pods")
	mustOpen("ingresses")
	want := tv.contentWidth(3, len(tv.snap.Rows))
	if got := spreadWidthOf(t, tv, "hosts"); got != want || got <= before[firstDataColumn+3] {
		t.Fatalf("Hosts %v wide after the next open, want the wider rows' %v (it was %v)", got, want, before[firstDataColumn+3])
	}

	// A view entered before its list is synced: measured once its rows are in, not before.
	empty := spreadIngresses(4, 3)
	empty.Rows, empty.Synced, empty.Total = nil, false, 0
	f.setTable("ingresses", empty)
	mustOpen("pods")
	mustOpen("ingresses")
	if got := spreadWidthOf(t, tv, "hosts"); got != 340 {
		t.Fatalf("Hosts %v wide without rows, want its own 340", got)
	}
	f.setTable("ingresses", spreadIngresses(4, 4))
	q.until(t, "the rows", func() bool { return tv.snap.Revision == 4 })
	if got, want := spreadWidthOf(t, tv, "hosts"), before[firstDataColumn+3]; got != want {
		t.Fatalf("Hosts %v wide once the rows came, want %v", got, want)
	}
}

// A column shown while the view stays open is measured as it shows; the others keep their measure.
func TestSpreadMeasuresAColumnShown(t *testing.T) {
	f, sh, w, q := spreadFake(t, 4, 2000)
	tv := sh.view
	hidden := spreadIngresses(4, 2)
	hidden.Columns = slices.Clone(hidden.Columns)
	hidden.Columns[3].Hidden = true
	f.setTable("ingresses", hidden)
	q.until(t, "Hosts hidden", func() bool { return len(tv.cols) == 4 })
	shown := spreadIngresses(5, 3) // wider hosts than at the open
	f.setTable("ingresses", shown)
	q.until(t, "Hosts shown", func() bool { return len(tv.cols) == 5 })
	settle(w)
	if got, want := spreadWidthOf(t, tv, "hosts"), tv.contentWidth(3, len(shown.Rows)); got != want {
		t.Fatalf("Hosts shown %v wide, want its content's %v", got, want)
	}
}

// Preferences › Tables › Free width is on at first; the user's tap turns the preference off and on,
// and an agent's change shows in the row.
func TestPreferencesTableSpreadWidth(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "tables"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["tableSpreadWidth"].(*widget.Check); return ok })
	check := sh.settings.controls["tableSpreadWidth"].(*widget.Check)
	q.until(t, "checked", func() bool { return check.Checked })
	if check.Text != spreadWidthCheck {
		t.Fatalf("label %q", check.Text)
	}
	test.Tap(check)
	q.until(t, "off", func() bool {
		p, _ := s.Preferences()
		return !p.TableSpreadWidth && !check.Checked && !sh.view.spreadOn
	})
	test.Tap(check)
	q.until(t, "on", func() bool { p, _ := s.Preferences(); return p.TableSpreadWidth && check.Checked && sh.view.spreadOn })
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "preferences.set", Origin: engine.OriginExternalAgent,
		Params: mustJSON(t, map[string]any{"tableSpreadWidth": false})}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "an agent's off", func() bool { return !check.Checked && !sh.view.spreadOn })
}

// BenchmarkSpreadMeasure10k measures what a view of 10,000 pods costs the main goroutine when it
// opens: its columns' content in the first spreadRows rows (measureColumns), and for comparison in
// every row.
func BenchmarkSpreadMeasure10k(b *testing.B) {
	f := connectedFake(b, 10000)
	sh, w, q := newTestShell(b, f, Options{})
	q.until(b, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	tv := sh.view
	for _, n := range []int{spreadRows, len(tv.snap.Rows)} {
		b.Run(strconv.Itoa(n)+"rows", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, ci := range tv.cols {
					tv.contentWidth(ci, n)
				}
			}
		})
	}
}
