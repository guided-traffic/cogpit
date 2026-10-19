package fyneui

import (
	"context"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The render tests draw the shell offscreen with Fyne's software painter: the visual check
// docs/developer/conventions.md asks for. With -update they rewrite testdata/*.png; otherwise
// they render to a temporary directory (or COGPIT_PNG_DIR), so a run never changes the tree.
var updatePNG = flag.Bool("update", false, "rewrite the PNGs in testdata")

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

func savePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	settle(w)
	writePNG(t, w, name)
}

// writePNG captures the window as it is, without settling it: for a test that lays out only what
// the GL driver would (glPass, tablereuse_test.go).
func writePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	dir := os.Getenv("COGPIT_PNG_DIR")
	switch {
	case *updatePNG:
		dir = "testdata"
	case dir == "":
		dir = t.TempDir()
	}
	img := w.Canvas().Capture()
	if b := img.Bounds(); b.Dx() < 1440 || b.Dy() < 900 {
		t.Fatalf("%s: captured %v, want at least 1440×900", name, b)
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
