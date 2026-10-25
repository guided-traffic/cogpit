package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
)

// Each view keeps its checked rows (N111, the owner's decision in the audit of 2026-10-16), and
// the selection bar is the shown view's: with two pods checked, the Services view shows no bar, so
// its Delete cannot reach the pods; back in Pods the two are checked and the bar is there again.
// The real engine, since the window only draws the selection it is given.
func TestSelectionBarIsTheShownViews(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, n := range []string{"web-0", "web-1", "web-2"} {
		src.Set(testkit.PodsGVR, testkit.Pod("shop", n))
	}
	src.Set(testkit.ServicesGVR, testkit.Service("shop", "checkout", "10.96.0.1"))
	conn := enginekit.MemoryConnector{Context: "kind-shop", Src: src, Profiles: kinds.Builtin()}
	s := newTestEngine(engine.Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: "kind-shop"}, q.do)
	sh.now = func() time.Time { return fixedNow }
	sh.kc.startDir = t.TempDir
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	shows := func(view string, rows int) func() bool {
		return func() bool {
			return sh.ui.View == view && sh.view.snap != nil && sh.view.snap.View == view && len(sh.view.snap.Rows) == rows
		}
	}
	q.until(t, "the pods", shows("pods", 3))
	settle(w)
	for _, row := range []int{0, 2} {
		test.Tap(cellAt(t, sh, row, checkColumn))
		q.until(t, "the row checked", func() bool { return sh.view.check.has(sh.view.snap.Rows[row].Ref) })
	}
	bar := sh.view.check.bar
	if !bar.root.Visible() || bar.count.text != "2 selected" {
		t.Fatalf("the pods' bar: shown %v, %q", bar.root.Visible(), bar.count.text)
	}

	sh.dispatch("ui.openView", openViewParams{View: "services"}, nil)
	q.until(t, "the services", shows("services", 1))
	if bar.root.Visible() || len(sh.view.selected) != 0 || len(sh.ui.Selected) != 0 {
		t.Errorf("the services: bar shown %v, selected %v (window) %v (engine)", bar.root.Visible(), sh.view.selected, sh.ui.Selected)
	}

	sh.dispatch("ui.openView", openViewParams{View: "pods"}, nil)
	q.until(t, "the pods again, two checked", func() bool { return shows("pods", 3)() && len(sh.view.selected) == 2 })
	if !bar.root.Visible() || bar.count.text != "2 selected" {
		t.Errorf("the pods' bar again: shown %v, %q", bar.root.Visible(), bar.count.text)
	}
}
