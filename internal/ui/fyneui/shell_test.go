package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// findAll returns every object of type T in the tree under root, widgets' renderers included.
func findAll[T any](root fyne.CanvasObject) []T {
	var out []T
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil {
			return
		}
		if t, ok := any(o).(T); ok {
			out = append(out, t)
		}
		if b, ok := o.(*textButton); ok { // Fyne's button with an outline (textbutton.go): found as one
			if t, ok := any(&b.Button).(T); ok {
				out = append(out, t)
			}
		}
		switch x := o.(type) {
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

func decodeParams[P any](t *testing.T, inv engine.Invocation) P {
	t.Helper()
	var p P
	if err := json.Unmarshal(inv.Params, &p); err != nil {
		t.Fatalf("%s params %s: %v", inv.ID, inv.Params, err)
	}
	return p
}

func headerFor(t *testing.T, sh *shell, col int) *headerCell {
	t.Helper()
	for _, h := range findAll[*headerCell](sh.view.table) {
		if h.col == col && h.Visible() {
			return h
		}
	}
	t.Fatalf("no header for column %d", col)
	return nil
}

func TestHeaderTapDispatchesSort(t *testing.T) {
	f := connectedFake(t, 200)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	settle(w)

	restarts := firstDataColumn + 3 // the table column of Restarts, the fourth visible column
	test.Tap(headerFor(t, sh, restarts))
	q.until(t, "ui.sort", func() bool { return len(f.callsOf("ui.sort")) == 1 })
	if p := decodeParams[sortParams](t, f.callsOf("ui.sort")[0]); p.Column != "restarts" || p.Desc {
		t.Fatalf("first tap sent %+v, want restarts ascending", p)
	}
	q.until(t, "the sort to show", func() bool { return sh.ui.Sort.Column == "restarts" && sh.view.snap.Sort.Column == "restarts" })
	settle(w)
	if h := headerFor(t, sh, restarts); h.sort != 1 {
		t.Fatalf("header arrow %d, want ascending", h.sort)
	}

	test.Tap(headerFor(t, sh, restarts))
	q.until(t, "second ui.sort", func() bool { return len(f.callsOf("ui.sort")) == 2 })
	if p := decodeParams[sortParams](t, f.callsOf("ui.sort")[1]); p.Column != "restarts" || !p.Desc {
		t.Fatalf("second tap sent %+v, want restarts descending", p)
	}
}

// TestStoppedShellDropsQueuedWork: what the shell queued for the main goroutine before it stopped
// (here a UI state from the engine) is dropped, not applied to a window that is gone, possibly
// under another app and theme by then (TestClusterRoleBindingDrawerLinks crashed in its cleanup
// that way), as Run's fyne.Do drops it once the app stopped.
func TestStoppedShellDropsQueuedWork(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	applied := sh.ui.Revision

	f.update(func(u *engine.UIState) { u.Filter = "web" })
	sh.stop()
	q.drain()
	if sh.ui.Revision != applied || sh.ui.Filter != "" {
		t.Errorf("a stopped shell applied revision %d (filter %q), had %d", sh.ui.Revision, sh.ui.Filter, applied)
	}
}

func TestRowTapDispatchesSelect(t *testing.T) {
	f := connectedFake(t, 200)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	settle(w)

	var cell *tableCell
	for _, c := range sh.view.cells {
		if c.row == 3 && c.col == firstDataColumn+1 && c.Visible() {
			cell = c
		}
	}
	if cell == nil {
		t.Fatal("row 3 is not on screen")
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(cell).AddXY(8, cell.Size().Height/2)
	test.TapCanvas(w.Canvas(), pos)
	q.until(t, "ui.select", func() bool { return len(f.callsOf("ui.select")) == 1 })
	p := decodeParams[selectModeParams](t, f.callsOf("ui.select")[0])
	want := f.tables["pods"].Rows[3].Ref
	if len(p.Refs) != 1 || p.Refs[0] != want || p.NoDrawer || p.Mode != "open" {
		t.Fatalf("ui.select %+v, want %v in mode open, with the drawer (A73)", p, want)
	}
	q.until(t, "the drawer", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == want })
	if !sh.view.rowDrawer(3) || sh.view.rowChecked(3) || len(sh.ui.Selected) != 0 {
		t.Fatalf("row 3 after the click: the drawer's %v, checked %v, selection %v; want the drawer's only",
			sh.view.rowDrawer(3), sh.view.rowChecked(3), sh.ui.Selected)
	}

	// Fyne's own selection would swallow a second tap on the same cell; the shell clears it.
	test.TapCanvas(w.Canvas(), pos)
	q.until(t, "second ui.select", func() bool { return len(f.callsOf("ui.select")) == 2 })
}

func TestNavigateScrollsOnceSnapshotHasRow(t *testing.T) {
	f := connectedFake(t, 100)
	big := fixturePods(10000, 7)
	target := big.Rows[7000].Ref
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	settle(w)

	// An agent's ui.navigate: the state arrives before a snapshot that contains the object.
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "ui.navigate", Origin: engine.OriginExternalAgent, Params: mustJSON(t, refParams{Ref: target})})
	}()
	q.until(t, "the selection", func() bool { return len(sh.ui.Selected) == 1 && sh.ui.Selected[0] == target })
	if sh.view.pending == nil {
		t.Fatal("no pending scroll target while the snapshot lacks the row")
	}
	for _, c := range sh.view.cells {
		if c.row >= 1000 {
			t.Fatalf("scrolled to row %d before the snapshot had the object", c.row)
		}
	}

	go f.setTable("pods", big)
	q.until(t, "the scroll", func() bool {
		for _, c := range sh.view.cells {
			if c.row == 7000 && (c.drawer || c.checked) {
				return true
			}
		}
		return false
	})
	if sh.view.pending != nil {
		t.Fatal("pending scroll target kept after scrolling")
	}
}

func TestConfirmerAnswersAndHonoursContext(t *testing.T) {
	f := connectedFake(t, 10)
	sh, _, q := newTestShell(t, f, Options{})
	req := engine.ConfirmRequest{Command: "object.delete", Label: "Delete", Danger: engine.DangerDestructive,
		Origin: engine.OriginGUI, Context: "demo", Namespaces: []string{"default"},
		Objects: []model.ObjectRef{{Kind: "pods", Namespace: "default", Name: "web-0"}}}
	type answer struct {
		ok  bool
		err error
	}
	ask := func(ctx context.Context) chan answer {
		ch := make(chan answer, 1)
		go func() {
			ok, err := sh.confirmer.Confirm(ctx, req)
			ch <- answer{ok, err}
		}()
		q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
		return ch
	}

	ch := ask(context.Background())
	if imp := sh.confirmer.current.confirm.Importance; imp != widget.DangerImportance {
		t.Errorf("destructive confirm button importance %v, want danger", imp)
	}
	test.Tap(sh.confirmer.current.confirm)
	if a := <-ch; !a.ok || a.err != nil {
		t.Fatalf("confirm: got %+v, want true", a)
	}
	q.until(t, "the dialog to close", func() bool { return sh.confirmer.current == nil })

	ch = ask(context.Background())
	test.Tap(sh.confirmer.current.cancel)
	if a := <-ch; a.ok || a.err != nil {
		t.Fatalf("cancel: got %+v, want false", a)
	}
	q.until(t, "the dialog to close", func() bool { return sh.confirmer.current == nil })

	ctx, cancel := context.WithCancel(context.Background())
	ch = ask(ctx)
	cancel()
	if a := <-ch; a.ok || !errors.Is(a.err, context.Canceled) {
		t.Fatalf("cancelled context: got %+v, want context.Canceled", a)
	}
	q.until(t, "the dialog to close after ctx ended", func() bool { return sh.confirmer.current == nil })
}

func TestDeleteGoesThroughConfirmation(t *testing.T) {
	f := connectedFake(t, 20)
	row := f.tables["pods"].Rows[2]
	f.details[row.Ref.String()] = fixturePodDetail(row)
	ref := row.Ref
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the toolbar", func() bool { return len(sh.drawer.actions.Objects) == 3 })
	del := sh.drawer.actions.Objects[2].(*tipButton) // Shell 200, Logs 500, Delete 1000
	if del.tip != "Delete" || del.Disabled() {
		t.Fatalf("third action %q disabled=%v", del.tip, del.Disabled())
	}
	test.Tap(del)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	test.Tap(sh.confirmer.current.cancel)
	q.until(t, "the dialog to close", func() bool { return sh.confirmer.current == nil })
	if n := len(f.callsOf("object.delete")); n != 1 {
		t.Fatalf("object.delete dispatched %d times", n)
	}
}

func TestDrawerActionsDisabledWithReason(t *testing.T) {
	f := connectedFake(t, 20)
	f.disabled["pod.shell"] = "not available on the demo cluster"
	f.disabled["object.delete"] = "not available on the demo cluster"
	row := f.tables["pods"].Rows[1]
	f.details[row.Ref.String()] = fixturePodDetail(row)
	ref := row.Ref
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the toolbar", func() bool { return len(sh.drawer.actions.Objects) == 3 })
	// The icons' tooltips name the action and, for a disabled one, why.
	var tips []string
	states := map[string]bool{}
	for _, o := range sh.drawer.actions.Objects {
		b := o.(*tipButton)
		tips = append(tips, b.tip)
		states[b.tip] = b.Disabled()
	}
	want := []string{"Shell: not available on the demo cluster", "Logs", "Delete: not available on the demo cluster"}
	if strings.Join(tips, "|") != strings.Join(want, "|") {
		t.Fatalf("tooltips %q, want %q", tips, want)
	}
	if states["Logs"] || !states[want[0]] || !states[want[2]] {
		t.Fatalf("disabled states %v, want Shell and Delete disabled", states)
	}
}

func TestSearchIsDebounced(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	sh.searchWait = time.Hour // the pause ends with fireTimer, not with a slow runner's gap between keys
	test.Type(sh.view.search, "web")
	q.drain()
	if n := len(f.callsOf("ui.setFilter")); n != 0 || sh.view.filterBusy != 1 {
		t.Fatalf("while typing: %d ui.setFilter, %d searches pending", n, sh.view.filterBusy)
	}
	if !fireTimer(sh.view.searchTimer) {
		t.Fatal("no pause pending after the typing")
	}
	q.until(t, "ui.setFilter", func() bool { return sh.view.filterBusy == 0 })
	if calls := f.callsOf("ui.setFilter"); len(calls) != 1 || decodeParams[filterParams](t, calls[0]).Filter != "web" {
		t.Fatalf("one burst of typing sent %+v, want the whole word once", calls)
	}

	// A filter set elsewhere (an agent) shows in the entry.
	go f.update(func(u *engine.UIState) { u.Filter = "checkout" })
	q.until(t, "the entry to follow", func() bool { return sh.view.search.Text == "checkout" })
}

// Closing the window ends the dock's streams (fyneui.go runs windowClosed).
func TestWindowCloseEndsTheDocksStreams(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	ref := f.tables["pods"].Rows[0].Ref
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "pod.logs", Origin: engine.OriginGUI,
			Params: mustJSON(t, containerParams{Ref: ref, Container: "api"})})
	}()
	streams := func(of map[string]int) int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return of["logs-1"]
	}
	q.until(t, "the log stream", func() bool { return streams(f.opened) == 1 })
	sh.windowClosed()
	q.until(t, "the stream closed", func() bool { return streams(f.closed) == 1 })
}

// A fresh destructive confirmation gives none of its buttons the keyboard: Return and Enter, the
// keys that press a focused button, confirm nothing, even with the button armed at once.
func TestReturnConfirmsNoDestructiveCommand(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{}) // armDelay 0: only the focus keeps Return from it
	req := engine.ConfirmRequest{Command: "object.delete", Label: "Delete", Danger: engine.DangerDestructive,
		Origin: engine.OriginGUI, Context: "demo", Namespaces: []string{"default"},
		Objects: []model.ObjectRef{{Kind: "pods", Namespace: "default", Name: "web-0"}}}
	answer := make(chan bool, 1)
	go func() {
		ok, _ := sh.confirmer.Confirm(context.Background(), req)
		answer <- ok
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	cd := sh.confirmer.current
	c := w.Canvas()
	if foc := c.Focused(); foc == cd.confirm || foc == cd.cancel {
		t.Fatalf("a button of the fresh confirmation has the keyboard: %T", foc)
	}
	for _, key := range []fyne.KeyName{fyne.KeyReturn, fyne.KeyEnter} {
		ev := &fyne.KeyEvent{Name: key}
		if foc := c.Focused(); foc != nil {
			foc.TypedKey(ev)
		} else if h := c.OnTypedKey(); h != nil {
			h(ev)
		}
		q.drain()
	}
	if sh.confirmer.current != cd {
		t.Fatal("Return answered the destructive confirmation")
	}
	test.Tap(cd.cancel)
	if ok := <-answer; ok {
		t.Error("Cancel confirmed")
	}
}

func TestDockOpensAndClosesStreams(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	ref := f.tables["pods"].Rows[0].Ref
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "pod.logs", Origin: engine.OriginGUI,
			Params: mustJSON(t, containerParams{Ref: ref, Container: "api"})})
	}()
	q.until(t, "the log stream", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.opened["logs-1"] == 1
	})
	if !sh.dock.open() {
		t.Fatal("dock did not open for a new tab")
	}
	go func() {
		_, _ = f.Dispatch(context.Background(), engine.Invocation{ID: "dock.close", Origin: engine.OriginGUI,
			Params: mustJSON(t, dockParams{ID: "logs-1"})})
	}()
	q.until(t, "the stream to close", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.closed["logs-1"] == 1
	})
	if sh.dock.open() {
		t.Fatal("dock still open without tabs")
	}
}

// Without a connection the navigator shows its section; a click on a cluster's row connects it
// (ui.setNavigatorScope) and the cluster's pods show.
func TestSectionConnects(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	f.kinds = fixtureKinds()
	f.tables["pods"] = fixturePods(50, 1)
	fakeNavScope(t, f)
	sh, _, q := newTestShell(t, f, Options{})
	n := sh.side.sec
	q.until(t, "the section", func() bool { return n.Visible() && n.loaded && len(n.rows) == len(f.contexts) })
	test.Tap(n.rows["demo"])
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected && sh.side.tree.Visible() })
	if got := scopeCalls(t, f); len(got) != 1 || got[0] != engine.NavScopeCluster("demo") {
		t.Fatalf("scopes %q", got)
	}
	q.until(t, "the pods table", func() bool { return sh.view.snap != nil && len(sh.kinds) > 0 })
}

// A --context that connects leaves the navigator's search closed.
func TestAutoConnectLeavesTheSearchClosed(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	f.kinds = fixtureKinds()
	sh, _, q := newTestShell(t, f, Options{AutoConnect: "demo"})
	q.until(t, "the connection", func() bool { return sh.ui.Connection.State == engine.Connected })
	if sh.side.head.searching() {
		t.Fatal("the navigator's search opened although the --context connected")
	}
}

// A --context the engine refuses says why in a toast and shows the navigator's section with its
// search focused, as ⌘P does (D-57).
func TestAutoConnectRefusedShowsTheSection(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	f.handlers["cluster.connect"] = func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New(`no context "nope" in the kubeconfig sources`)
	}
	sh, w, q := newTestShell(t, f, Options{AutoConnect: "nope"})
	h := sh.side.head
	q.until(t, "the section's search", func() bool { return sh.side.sec.Visible() && h.searching() && w.Canvas().Focused() == h.entry })
	if shown := walkToastTexts(sh); !hasText(shown, `Could not connect to nope: no context "nope" in the kubeconfig sources`) {
		t.Errorf("toasts %q", shown)
	}
}

func TestHiddenColumnsAreSkipped(t *testing.T) {
	f := connectedFake(t, 10)
	f.tables["events"] = &engine.TableSnapshot{
		View: "events", Title: "Events", Namespaced: true, Synced: true, Total: 1,
		Columns: []model.Column{
			{ID: "name", Title: "Name", Type: model.ColumnText, Hidden: true},
			{ID: "type", Title: "Type", Type: model.ColumnText},
			{ID: "message", Title: "Message", Type: model.ColumnText},
			{ID: "age", Title: "Age", Type: model.ColumnAge},
		},
		Rows: []model.Row{{Ref: model.ObjectRef{Kind: "events", Namespace: "default", Name: "web.1"},
			Cells: []model.Cell{{Text: "web.1"}, {Text: "Warning", Status: model.StatusWarning}, {Text: "Back-off"}, {Num: fixedNow.Unix() - 60}}}},
	}
	f.ui.View = "events"
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	settle(w)
	if rows, cols := sh.view.length(); rows != 1 || cols != 5 {
		t.Fatalf("table %d×%d, want 1×5: the checkbox, three columns without the hidden name, and the ⋮", rows, cols)
	}
	texts := map[int]string{}
	for _, c := range sh.view.cells {
		if c.row == 0 && c.Visible() {
			texts[c.col] = c.full
		}
	}
	if texts[1] != "Warning" || texts[2] != "Back-off" || texts[3] != "1m" {
		t.Fatalf("row cells %v", texts)
	}
	if sh.view.flexColumn() != 1 {
		t.Fatalf("flex column %d, want the message", sh.view.flexColumn())
	}
}

// TestEngineEventsWithoutRaces floods the shell with events from several engine goroutines while
// the "main goroutine" applies them. Run with -race: a widget touched outside sh.do is a race
// between an engine goroutine and the test goroutine.
func TestEngineEventsWithoutRaces(t *testing.T) {
	f := connectedFake(t, 2000)
	snaps := []*engine.TableSnapshot{fixturePods(2000, 1), fixturePods(2000, 2), fixturePods(1500, 3)}
	row := f.tables["pods"].Rows[0]
	f.details[row.Ref.String()] = fixturePodDetail(row)
	sh, _, q := newTestShell(t, f, Options{})

	const n = 150
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := range n {
			f.setTable("pods", snaps[i%len(snaps)])
		}
	}()
	go func() {
		defer wg.Done()
		for i := range n {
			f.update(func(u *engine.UIState) {
				u.Filter = strings.Repeat("x", i%4)
				r := row.Ref
				u.Selected, u.Drawer = []model.ObjectRef{r}, &r
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range n {
			f.emit(engine.Event{Type: engine.EventDiagnostics})
			f.emit(engine.Event{Type: engine.EventDetail})
		}
	}()
	go func() {
		defer wg.Done()
		for i := range n {
			f.emit(engine.Event{Type: engine.EventToast, Level: "info", Message: "event " + strings.Repeat("·", i%3)})
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
			q.drain()
		}
	}
	// The engine's newest state, read at every check: the window's own reports move it too, the
	// navigator's row a debounce after the kinds came (navigatorstate.go, D-51). On a slow runner
	// that report lands after the flood, and a revision read once here waits forever (CI,
	// 2026-10-18: run 39161105286).
	q.until(t, "the last state", func() bool {
		return sh.ui.Revision == f.UIState().Revision && sh.view.snap == f.Table("pods")
	})
	// Every refresh measures a fresh template cell; only shown cells may be kept.
	if n := len(sh.view.cells); n > 600 {
		t.Fatalf("%d cells tracked after %d snapshots: template cells leak", n, n)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestConfirmationsOneAtATimeAndArmed checks the review finding that confirmations stacked: a
// second request waits until the first is answered, and a fresh dialog's confirm button ignores
// taps until it arms.
// A confirmation that carries a review text (an agent's Helm values) shows it in the dialog,
// monospace and selectable, in a box that scrolls instead of growing past the window (owner,
// 2026-10-12).
func TestConfirmationShowsTheReviewText(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	values := "controller:\n  image: registry.example/evil:latest\n" + strings.Repeat("  extra: line\n", 60)
	req := engine.ConfirmRequest{Command: "helm.release.upgrade", Label: "Upgrade", Danger: engine.DangerMutating,
		Origin: engine.OriginExternalAgent, Context: "demo", Namespaces: []string{"apps"},
		Objects: []model.ObjectRef{{Kind: "helmreleases", Namespace: "apps", Name: "web"}}, Kinds: []string{"Release"},
		Effects: []engine.Effect{{Name: "Chart", Value: "web 0.9.0 → 1.0.0", Tone: engine.EffectNames},
			{Name: "Values", Value: "below, replacing the release's user-supplied values whole"}}, Review: values}
	go func() { _, _ = sh.confirmer.Confirm(context.Background(), req) }()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	var shown *widget.Label
	for _, l := range findAll[*widget.Label](w.Canvas().Overlays().Top()) {
		if l.Text == values {
			shown = l
		}
	}
	if shown == nil || !shown.Selectable || !shown.TextStyle.Monospace {
		t.Fatalf("the values are not shown as selectable monospace text: %v", shown)
	}
	if h := w.Canvas().Overlays().Top().MinSize().Height; h > 900 {
		t.Errorf("the dialog grows to %v with a long text", h)
	}
	savePNG(t, w, "confirm_review_dark.png")
	test.Tap(sh.confirmer.current.cancel)
}

func TestConfirmationsOneAtATimeAndArmed(t *testing.T) {
	f := connectedFake(t, 10)
	sh, _, q := newTestShell(t, f, Options{})
	sh.confirmer.armDelay = time.Hour // unarmed until fireTimer arms it, however slow the steps before
	mk := func(name string) engine.ConfirmRequest {
		return engine.ConfirmRequest{Command: "object.delete", Label: "Delete", Danger: engine.DangerDestructive,
			Origin: engine.OriginExternalAgent, Context: "demo", Namespaces: []string{"default"},
			Objects: []model.ObjectRef{{Kind: "pods", Namespace: "default", Name: name}}}
	}
	answers := make(chan string, 2)
	ask := func(name string) {
		go func() {
			ok, _ := sh.confirmer.Confirm(context.Background(), mk(name))
			answers <- fmt.Sprintf("%s=%v", name, ok)
		}()
	}
	ask("first")
	q.until(t, "the first dialog", func() bool { return sh.confirmer.current != nil })
	first := sh.confirmer.current
	ask("second")
	time.Sleep(50 * time.Millisecond)
	q.drain()
	if sh.confirmer.current != first {
		t.Fatal("a second dialog opened while the first was on screen")
	}
	test.Tap(first.confirm) // not armed yet: ignored
	q.drain()
	if sh.confirmer.current != first || !first.confirm.Disabled() {
		t.Fatal("an unarmed confirm button answered")
	}
	if !fireTimer(first.arm) {
		t.Fatal("no arming timer")
	}
	q.until(t, "the confirm button to arm", func() bool { return !first.confirm.Disabled() })
	test.Tap(first.confirm)
	if a := <-answers; a != "first=true" {
		t.Fatalf("first answer %q", a)
	}
	q.until(t, "the second dialog", func() bool { return sh.confirmer.current != nil && sh.confirmer.current != first })
	test.Tap(sh.confirmer.current.cancel)
	if a := <-answers; a != "second=false" {
		t.Fatalf("second answer %q", a)
	}
}
