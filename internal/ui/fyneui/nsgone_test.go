package fyneui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// nsRowMarked reports whether a row of the filter's list reads red with its state after its name.
func nsRowMarked(r *nsRow, state string) bool {
	return r.state == state && r.label.text == r.name+" · "+state && r.label.color == colorStatusError
}

// nsRowPlain reports whether a row reads as any namespace's: its name in the foreground colour.
func nsRowPlain(r *nsRow) bool {
	return r.state == nsLive && r.label.text == r.name && r.label.color != colorStatusError
}

// TestDeletedNamespaceMarkedInTheFilter (the owner's gate walk, GW9, his choice): a namespace the
// filter names that the cluster no longer has stays in the filter's list, red and "· deleted", the
// button reads red; unticked, it leaves the selection and the list, and the button reads as before.
func TestDeletedNamespaceMarkedInTheFilter(t *testing.T) {
	sh, w, q, _ := nsFilterShell(t, ThemeDark, "orders", "cogpit-gate")
	tv := sh.view
	q.until(t, "the button red", func() bool { return tv.nsTintTheme.on })
	if tv.nsButton.Text != "2 namespaces" {
		t.Fatalf("button %q", tv.nsButton.Text)
	}
	test.Tap(tv.nsButton)
	f := tv.ns
	gone, orders := nsRowNamed(t, f, "cogpit-gate"), nsRowNamed(t, f, "orders")
	if !nsRowMarked(gone, nsDeleted) || !gone.check.Checked {
		t.Fatalf("the deleted namespace's row: state %q, label %q in %s, checked %v", gone.state, gone.label.text, gone.label.color, gone.check.Checked)
	}
	if !nsRowPlain(orders) || !orders.check.Checked {
		t.Fatalf("a live namespace's row: state %q, label %q", orders.state, orders.label.text)
	}
	if len(f.rows) != len(thirtyNamespaces)+2 {
		t.Fatalf("%d rows, want All namespaces, the thirty and the deleted one", len(f.rows))
	}

	test.Type(f.search, "o") // cogpit-gate and orders in view
	gone = nsRowNamed(t, f, "cogpit-gate")
	clickAt(w, gone.check)
	q.until(t, "the deleted namespace out of the filter", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"orders"}) })
	if slices.ContainsFunc(f.rows, func(r *nsRow) bool { return r.name == "cogpit-gate" }) || len(f.rows) != len(thirtyNamespaces)+1 {
		t.Fatalf("the unticked deleted namespace stays in the list (%d rows)", len(f.rows))
	}
	if !f.visible() || w.Canvas().Focused() != f.search {
		t.Fatalf("after the untick: visible %v, focus %T", f.visible(), w.Canvas().Focused())
	}
	if tv.nsTintTheme.on || tv.nsButton.Text != "orders" {
		t.Fatalf("button red %v, %q", tv.nsTintTheme.on, tv.nsButton.Text)
	}
}

// TestNamespaceDeletedWhileFiltered: the owner's case: the view filtered to a namespace that is then
// deleted. The view's next rows read the namespaces again from the cache: the button reads red with
// no click; changes within nsRecheckEvery are read once it is over.
func TestNamespaceDeletedWhileFiltered(t *testing.T) {
	old := nsRecheckEvery
	nsRecheckEvery = 50 * time.Millisecond
	t.Cleanup(func() { nsRecheckEvery = old })
	sh, _, q, fe := nsFilterShell(t, ThemeDark, "payments")
	tv := sh.view
	q.until(t, "the view settled", func() bool { return sh.view.snap != nil })
	if tv.nsTintTheme.on {
		t.Fatal("the button is red for a live namespace")
	}
	reads := len(fe.callsOf("resources.list"))

	fe.mu.Lock()
	fe.terminatingNS = map[string]bool{"payments": true} // kubectl delete namespace payments: Terminating first
	fe.mu.Unlock()
	fe.setTable(kindPods, fixturePods(150, 9)) // its pods go
	q.until(t, "the button red for the terminating namespace", func() bool { return tv.nsTintTheme.on })

	fe.mu.Lock()
	fe.namespaces = slices.DeleteFunc(slices.Clone(fe.namespaces), func(n string) bool { return n == "payments" })
	fe.terminatingNS = nil
	fe.mu.Unlock()
	fe.setTable(kindPods, fixturePods(140, 9)) // within the interval: read once it is over
	q.until(t, "payments gone", func() bool { return sh.namespaceState("payments") == nsDeleted })
	if !tv.nsTintTheme.on {
		t.Fatal("the button is not red for the deleted namespace")
	}
	if n := len(fe.callsOf("resources.list")) - reads; n < 2 || n > 4 {
		t.Fatalf("%d reads of the namespaces for two changes of the rows", n)
	}

	// Without a namespace filter the rows read nothing.
	sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{}}, nil)
	q.until(t, "the filter cleared", func() bool { return len(sh.ui.Namespaces) == 0 && !tv.nsTintTheme.on })
	time.Sleep(2 * nsRecheckEvery)
	q.drain()
	reads = len(fe.callsOf("resources.list"))
	fe.setTable(kindPods, fixturePods(130, 9))
	time.Sleep(2 * nsRecheckEvery)
	q.drain()
	if n := len(fe.callsOf("resources.list")) - reads; n != 0 {
		t.Fatalf("rows without a namespace filter read the namespaces %d times", n)
	}
}

// TestTerminatingNamespaceMarkedInTheFilter: a Terminating namespace is marked as a deleted one is
// (the lead's detail), selected or not, "· terminating"; the button is red while the filter names
// it; unticked it stays in the list, since it still exists.
func TestTerminatingNamespaceMarkedInTheFilter(t *testing.T) {
	f := connectedFake(t, 200)
	f.namespaces = thirtyNamespaces
	f.terminatingNS = map[string]bool{"orders": true, "logging": true}
	f.ui.Namespaces = []string{"orders", "payments"}
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	tv := sh.view
	q.until(t, "the button red", func() bool { return len(sh.namespaces) == len(thirtyNamespaces) && tv.nsTintTheme.on })
	test.Tap(tv.nsButton)
	ns := tv.ns
	for _, n := range []string{"orders", "logging"} {
		if r := nsRowNamed(t, ns, n); !nsRowMarked(r, nsTerminating) {
			t.Errorf("%s: state %q, label %q", n, r.state, r.label.text)
		}
	}
	if r := nsRowNamed(t, ns, "payments"); !nsRowPlain(r) {
		t.Errorf("payments: state %q, label %q", r.state, r.label.text)
	}
	test.Type(ns.search, "ord")
	clickAt(w, nsRowNamed(t, ns, "orders").check)
	q.until(t, "orders out of the filter", func() bool { return slices.Equal(sh.ui.Namespaces, []string{"payments"}) })
	if r := nsRowNamed(t, ns, "orders"); r.check.Checked || !nsRowMarked(r, nsTerminating) {
		t.Fatalf("the unticked terminating namespace: checked %v, state %q", r.check.Checked, r.state)
	}
	if tv.nsTintTheme.on {
		t.Fatal("the button is red without a namespace gone or going")
	}
}

// TestNamespaceFilterMarksNothingItCannotKnow: while the user may not list namespaces, the filter's
// own namespaces are offered as before and none is marked; nothing is read.
func TestNamespaceFilterMarksNothingItCannotKnow(t *testing.T) {
	f := connectedFake(t, 200)
	f.namespaces = nil // the list is refused
	no := false
	for i := range f.kinds {
		if f.kinds[i].Kind == kindNamespaces {
			f.kinds[i].Listable = &no
		}
	}
	f.ui.Namespaces = []string{"team-a"}
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	test.Tap(sh.view.nsButton)
	if r := nsRowNamed(t, sh.view.ns, "team-a"); !nsRowPlain(r) || sh.view.nsTintTheme.on {
		t.Fatalf("team-a: state %q, button red %v", r.state, sh.view.nsTintTheme.on)
	}
}

// TestNamespaceFilterOffersEveryNamespace (GW9's side finding): a cluster with more namespaces than
// one resources.list answers by default (100) or at most (1000) offers every one, read page by page.
func TestNamespaceFilterOffersEveryNamespace(t *testing.T) {
	f := connectedFake(t, 200)
	var names []string
	for i := range 1250 {
		names = append(names, fmt.Sprintf("team-%04d", i))
	}
	f.namespaces = names
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "every namespace", func() bool { return len(sh.namespaces) == len(names) })
	var offsets []int
	for _, c := range f.callsOf("resources.list") {
		p := decodeParams[listParams](t, c)
		if p.Kind != kindNamespaces || p.Limit != nsPageSize {
			t.Fatalf("a read of %+v", p)
		}
		offsets = append(offsets, p.Offset)
	}
	if !slices.Equal(offsets[:2], []int{0, nsPageSize}) {
		t.Fatalf("pages at %v", offsets)
	}
	test.Tap(sh.view.nsButton)
	if n := len(sh.view.ns.rows); n != len(names)+1 {
		t.Fatalf("the list has %d rows, want All namespaces and %d", n, len(names))
	}
	if r := nsRowNamed(t, sh.view.ns, "team-1249"); !nsRowPlain(r) {
		t.Fatalf("the last namespace: %q", r.label.text)
	}
}

// The namespace filter's list with a deleted namespace (cogpit-gate) and a terminating one (orders)
// selected, another terminating one (logging) not, and the button red, dark and light (GW9, rule 7).
func TestRenderNamespaceFilterGoneDark(t *testing.T) {
	renderNamespaceFilterGone(t, ThemeDark, "namespace_filter_gone_dark.png")
}

func TestRenderNamespaceFilterGoneLight(t *testing.T) {
	renderNamespaceFilterGone(t, ThemeLight, "namespace_filter_gone_light.png")
}

func renderNamespaceFilterGone(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 200)
	f.namespaces = thirtyNamespaces
	f.terminatingNS = map[string]bool{"orders": true, "logging": true}
	f.ui.Namespaces = []string{"cogpit-gate", "orders"}
	f.tables[kindPods] = &engine.TableSnapshot{View: kindPods, Context: "demo", Title: "Pods", Namespaced: true, Columns: podColumns,
		Sort: engine.SortSpec{Column: "name"}, Total: 218, Revision: 2, Synced: true, Namespaces: f.ui.Namespaces}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the button red", func() bool { return sh.view.nsTintTheme.on && sh.diag.total > 0 })
	settle(w)
	test.Tap(sh.view.nsButton)
	test.Type(sh.view.ns.search, "o")
	savePNG(t, w, name)
}
