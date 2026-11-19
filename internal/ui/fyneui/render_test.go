package fyneui

import (
	"context"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The render tests draw the shell offscreen with Fyne's software painter: the visual check
// docs/developer/conventions.md asks for. With -update they rewrite testdata/*.png, with
// COGPIT_PNG_DIR they write their pictures there; otherwise they capture the window, which paints
// every renderer, and write nothing (D-112), so a run never changes the tree.
var updatePNG = flag.Bool("update", false, "rewrite the PNGs in testdata")

// livePNG adds the pictures of the churning demo cluster to -update: its pods change and its
// charts gather measurements by the computer's clock, so they differ from run to run, and -update
// alone leaves them as they are (saveLivePNG).
var livePNG = flag.Bool("live", false, "with -update: rewrite the pictures of the churning demo cluster too")

// settle lays the window out again from the top, as the GL driver's EnsureMinSize does every
// frame: a widget whose minimum size changed (a label got its text) gets room in its parent.
// Fyne's software canvas has no such pass, so without it a capture shows the first layout.
func settle(w fyne.Window) {
	c := w.Canvas()
	for range 3 {
		relayout(c.Content())
		for _, o := range c.Overlays().List() {
			relayout(o)
		}
	}
}

func relayout(o fyne.CanvasObject) {
	if o == nil || !o.Visible() {
		return
	}
	switch x := o.(type) {
	case *fyne.Container:
		if x.Layout != nil {
			x.Layout.Layout(x.Objects, x.Size())
		}
		for _, c := range x.Objects {
			relayout(c)
		}
	case fyne.Widget:
		r := test.WidgetRenderer(x)
		r.Layout(x.Size())
		for _, c := range r.Objects() {
			relayout(c)
		}
	}
}

// pngKept reports whether the renders' pictures are written (-update, COGPIT_PNG_DIR): a scene that
// waits only to give its picture a shape (a chart's series) waits that long only then.
func pngKept() bool { return *updatePNG || os.Getenv("COGPIT_PNG_DIR") != "" }

// livePNGKept reports whether a picture of the churning demo cluster is written: with -update
// -live, or to COGPIT_PNG_DIR without -update.
func livePNGKept() bool { return pngKept() && (!*updatePNG || *livePNG) }

// saveLivePNG is savePNG for a picture of the churning demo cluster: with -update but not -live
// the window is only captured, which paints every renderer.
func saveLivePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	if *updatePNG && !*livePNG {
		awaitScene(t, w)
		settle(w)
		w.Canvas().Capture()
		return
	}
	savePNG(t, w, name)
}

// lookPNG captures the window, which paints every renderer (the check), and writes the picture to
// COGPIT_PNG_DIR when that is set, for a look, never to testdata: the pictures a test only shows,
// as against the ones writePNG keeps in testdata. Before D-112 it went to a temporary folder the
// test removed when COGPIT_PNG_DIR was not set.
func lookPNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	img := w.Canvas().Capture()
	dir := os.Getenv("COGPIT_PNG_DIR")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Log("rendered", f.Name())
}

func savePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	awaitScene(t, w)
	settle(w)
	writePNG(t, w, name)
}

// awaitScene drains the window's queue until its shell's reads are in (scene.settled): before a
// picture, and before a test scrolls to or looks up what a read still to come would move.
func awaitScene(t testing.TB, w fyne.Window) {
	t.Helper()
	if sc := sceneOf(w); sc != nil {
		sc.q.until(t, "the window's reads", sc.settled)
	}
}

// A scene is a test's shell and its main queue, kept by its window (keepScene), so that savePNG
// draws what the shell shows once its reads are in rather than whatever the timing left: -update
// twice in a row writes the same pictures.
type scene struct {
	sh *shell
	q  *mainQueue
	// fakeBlink: the test moves the blinker's clock itself (fakeBlinkClock) and draws the blink
	// it chose.
	fakeBlink bool
}

var (
	scenesMu sync.Mutex
	scenes   = map[fyne.Window]*scene{}
)

// demoStatsDue reports whether the real engine's demo cluster is connected and the status bar does
// not show its counters yet (a fake's are whatever the test gave it).
func demoStatsDue(sh *shell) bool {
	_, session := sh.eng.(*engine.Session)
	return session && sh.ui.Connection.Demo && sh.ui.Connection.State == engine.Connected && !sh.diag.demo
}

// keepScene registers a test's shell under its window for savePNG, until the test ends.
func keepScene(t testing.TB, w fyne.Window, sh *shell, q *mainQueue) {
	scenesMu.Lock()
	scenes[w] = &scene{sh: sh, q: q}
	scenesMu.Unlock()
	t.Cleanup(func() {
		scenesMu.Lock()
		delete(scenes, w)
		scenesMu.Unlock()
	})
}

func sceneOf(w fyne.Window) *scene {
	scenesMu.Lock()
	defer scenesMu.Unlock()
	return scenes[w]
}

// sceneOfShell is the scene a shell was registered with, or nil.
func sceneOfShell(sh *shell) *scene {
	scenesMu.Lock()
	defer scenesMu.Unlock()
	for _, sc := range scenes {
		if sc.sh == sh {
			return sc
		}
	}
	return nil
}

// settled reports whether the shell's own reads, the ones every scene starts, are in: the
// clusters of the navigator, the welcome page and the hotbar, the status bar's counters, the
// namespaces, a settings section's reads, the cluster Overview's integrations, the drawer's detail
// and its metrics. A
// connected cluster's blink past its connect (the minimum it lasts) is ended, as if its three
// seconds had passed: a picture shows the steady dot, not a phase of the blink. Main goroutine.
func (sc *scene) settled() bool {
	sh := sc.sh
	if sec := sh.side.sec; sec.loadSeq > 0 && !sec.loaded {
		return false
	}
	if wp := sh.tabs.welcome; wp.shown && !wp.loaded {
		return false
	}
	if sh.diagQueued.Load() || sh.lastDiagFetch.Load() != 0 && !sh.diagShown ||
		demoStatsDue(sh) {
		return false // the status bar's counters, the demo cluster's once it is connected
	}
	if sh.tableQueued.Load() || sh.detailQueued.Load() || sh.metricsQueued.Load() {
		return false // a refresh the engine asked for
	}
	if sh.nsLoads > 0 || sh.settings.reads > 0 {
		return false // the namespace filter's names, a settings section's reads
	}
	if sh.ui.Hotbar != nil && sh.hot.answered < sh.hot.seq || sh.view.cluster != nil && sh.view.cluster.bearingAsks > 0 {
		return false // the hotbar's clusters, the cluster Overview's integrations
	}
	if d := sh.drawer; sh.ui.Drawer != nil {
		if d.busy || d.dirty {
			return false
		}
		if p, ok := d.kept[metricsKey].(*metricsPanel); ok && p.data == nil {
			return false
		}
	}
	if !sc.fakeBlink {
		ended := false
		for _, k := range sh.blink.blinks {
			if !k.live {
				k.until, ended = time.Time{}, true
			}
		}
		if ended {
			sh.blink.tick()
		}
	}
	return true
}

// writePNG captures the window as it is, without settling it: for a test that lays out only what
// the GL driver would (glPass, tablereuse_test.go).
func writePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	dir := os.Getenv("COGPIT_PNG_DIR")
	if *updatePNG {
		dir = "testdata"
	}
	img := w.Canvas().Capture()
	if b := img.Bounds(); b.Dx() < 1440 || b.Dy() < 900 {
		t.Fatalf("%s: captured %v, want at least 1440×900", name, b)
	}
	// The capture paints every renderer, which is the check; without -update or COGPIT_PNG_DIR the
	// picture went to a temporary folder the test removes, and encoding it cost a third as much
	// again as the capture, for nobody (D-112).
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// shellScene is the full shell: 10,000 pods, the drawer open on a pod, a logs tab in the dock.
func shellScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	f := connectedFake(t, 10000)
	row := f.tables["pods"].Rows[5]
	ref := row.Ref
	f.details[ref.String()] = fixturePodDetail(row)
	f.ui.Selected = []model.ObjectRef{ref}
	f.ui.Drawer = &ref
	f.ui.Dock = []engine.DockTab{
		{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: " + ref.Name, Context: "demo", Ref: &ref, Container: "api"},
		{ID: "terminal-2", Type: engine.DockTerminal, Title: "Terminal: demo", Context: "demo"},
	}
	f.ui.ActiveDock = "logs-1"
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell to settle", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && sh.diag.total > 0 && f.opened["logs-1"] == 1
	})
	return sh, w, q
}

func TestRenderShellDark(t *testing.T) {
	_, w, _ := shellScene(t, ThemeDark)
	savePNG(t, w, "shell_dark.png")
}

func TestRenderShellLight(t *testing.T) {
	_, w, _ := shellScene(t, ThemeLight)
	savePNG(t, w, "shell_light.png")
}

// TestRenderTable shows the whole pod table, status colours included, without drawer and dock.
func TestRenderTable(t *testing.T) {
	f := connectedFake(t, 10000)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.diag.total > 0 })
	settle(w)
	sh.view.table.ScrollTo(widget.TableCellID{Row: 230, Col: 0}) // a stretch with every status
	savePNG(t, w, "table_dark.png")
}

func TestRenderConfirm(t *testing.T) {
	f := connectedFake(t, 10000)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	row := f.tables["pods"].Rows[42]
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{
			Command: "object.delete", Label: "Delete", Danger: engine.DangerDestructive,
			Origin: engine.OriginExternalAgent, Context: "demo",
			Namespaces: []string{row.Ref.Namespace}, Objects: []model.ObjectRef{row.Ref}, Kinds: []string{"Pod"},
		})
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	savePNG(t, w, "confirm.png")
	test.Tap(sh.confirmer.current.cancel)
	<-done
}
