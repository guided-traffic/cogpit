package fyneui

import (
	"context"
	"encoding/json"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// withCopy registers Copy on the fake and answers it with the object's name, as the engine does.
func withCopy(t testing.TB, f *fakeEngine) {
	t.Helper()
	if err := engine.Register(f.reg, engine.Spec[probeRef]{ID: "object.copyName", Label: "Copy", Order: 10, DrawerOnly: true,
		Danger: engine.DangerUI, Run: func(context.Context, *engine.Session, probeRef) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	f.handlers["object.copyName"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p probeRef
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		return map[string]any{"ref": p.Ref, "name": p.Ref.Name}, nil
	}
}

// copyScene is the pod drawer with Copy among its actions.
func copyScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "pods")
	registerProbes(t, f)
	withCopy(t, f)
	ref := podDrawerRef(t)
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer's actions", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 && sh.diag.total > 0
	})
	return sh, w, q, ref
}

// TestCopyPutsTheNameOnTheClipboard: Copy sits right after the object's name, not among the
// actions at the header's right end (F4); it puts the name on the clipboard and says so.
func TestCopyPutsTheNameOnTheClipboard(t *testing.T) {
	sh, _, q, ref := copyScene(t, ThemeDark)
	b := sh.drawer.nameCopy
	if !b.Visible() || b.tip != "Copy" {
		t.Fatalf("the name's copy icon: visible %v, tip %q", b.Visible(), b.tip)
	}
	for _, o := range sh.drawer.actions.Objects {
		if tb, ok := o.(*tipButton); ok && tb.tip == "Copy" {
			t.Error("Copy is still among the actions at the right")
		}
	}
	title := fyne.CurrentApp().Driver().AbsolutePositionForObject(sh.drawer.title)
	icon := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	textW := fyne.MeasureText(ref.Name, sh.app.Settings().Theme().Size(sh.drawer.title.size), sh.drawer.title.style).Width
	if gap := icon.X - (title.X + textW); gap < 0 || gap > 3*sh.app.Settings().Theme().Size(theme.SizeNamePadding) {
		t.Errorf("the copy icon is %v after the name's end, want right after it", gap)
	}
	sh.app.Clipboard().SetContent("")
	toasts := len(sh.toasts.Objects)
	test.Tap(b)
	q.until(t, "the name on the clipboard", func() bool { return sh.app.Clipboard().Content() == ref.Name })
	q.until(t, "the toast", func() bool { return len(sh.toasts.Objects) > toasts })
	if got := labels(sh.rowMenu(ref, sh.evaluateActions(ref, []string{"app"}))); got[1] == "Copy" {
		t.Errorf("Copy in the row menu: %q", got)
	}
}

func TestRenderCopyDark(t *testing.T) {
	sh, w, q, _ := copyScene(t, ThemeDark)
	test.Tap(sh.drawer.nameCopy)
	q.until(t, "the toast", func() bool { return len(sh.toasts.Objects) > 0 })
	savePNG(t, w, "drawer_copy_dark.png")
}
