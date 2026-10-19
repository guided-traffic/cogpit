package fyneui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The one form of an object's kind and name (the owner's WF32, 2026-10-19): the kind muted with an
// arrow, the name bold, in a column of their own; several grouped by kind, the kind on its group's
// first line only.
func TestObjectNamesSetTheKindApart(t *testing.T) {
	test.NewTempApp(t)
	c := objectNames([]namedObject{{"Pod", "web-0"}, {"Config Map", "walk-cm"}, {"Pod", "web-1"}}, 400)
	c.Resize(c.MinSize())
	var kinds, names []string
	for i := 0; i+1 < len(c.Objects); i += 2 {
		k, n := c.Objects[i].(*textLine), c.Objects[i+1].(*paragraph)
		kinds, names = append(kinds, k.text), append(names, n.text)
		if k.color != colorMuted || !n.style.Bold || n.color != theme.ColorNameForeground {
			t.Errorf("row %d: kind in %s, name bold %v in %s", i/2, k.color, n.style.Bold, n.color)
		}
		if x := c.Objects[i+1].Position().X; x != c.Objects[1].Position().X {
			t.Errorf("row %d: the name at x %v, the first at %v", i/2, x, c.Objects[1].Position().X)
		}
	}
	if !slices.Equal(kinds, []string{"Pod →", "", "Config Map →"}) || !slices.Equal(names, []string{"web-0", "web-1", "walk-cm"}) {
		t.Errorf("kinds %q names %q", kinds, names)
	}
	if got := objectText("Config Map", "walk-cm"); got != "Config Map → walk-cm" {
		t.Errorf("objectText %q", got)
	}
}

// The Edit tab's Apply that removes a key (the owner's WF33, 2026-10-19: "Removes key added. Ist
// wieder als Satz geschrieben"): the confirmation names the object in the one form and the removed
// key in a row of its own, bold and red as the drawer's save review does, never inside a sentence.
func TestConfirmationEffectsAreRows(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	ref := model.ObjectRef{Context: "demo", Kind: "configmaps", Namespace: "walk-test", Name: "walk-cm"}
	effects := engine.DataKeysEffects(nil, []string{"added"})
	go func() {
		_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{
			Command: "object.apply", Label: "Apply", Danger: engine.DangerDestructive, Origin: engine.OriginGUI, Context: "demo",
			Namespaces: []string{"walk-test"}, Objects: []model.ObjectRef{ref}, Kinds: []string{"Config Map"},
			Effects: effects, Detail: engine.EffectsText(effects),
		})
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	settle(w)
	top := w.Canvas().Overlays().Top()
	var lines []string
	for _, l := range findAll[*textLine](top) {
		lines = append(lines, l.text)
	}
	if want := []string{"Context", "Namespace", "Object", "Config Map →", "Removes", "Requested by"}; !slices.Equal(lines, want) {
		t.Errorf("the rows' names %q, want %q", lines, want)
	}
	var removed, name *paragraph
	for _, p := range findAll[*paragraph](top) {
		switch p.text {
		case "added":
			removed = p
		case "walk-cm":
			name = p
		}
		if strings.Contains(p.text, "Removes key") || strings.Contains(p.text, "Config Map walk-cm") {
			t.Errorf("a sentence: %q", p.text)
		}
	}
	if removed == nil || removed.color != colorStatusError || !removed.style.Bold {
		t.Errorf("the removed key's value %+v", removed)
	}
	if name == nil || !name.style.Bold {
		t.Errorf("the object's name %+v", name)
	}
	var shown []string
	for _, c := range findAll[*canvas.Text](top) {
		shown = append(shown, c.Text)
	}
	if !slices.Contains(shown, "This cannot be undone.") {
		t.Errorf("a removal without its line: %q", shown)
	}
	test.Tap(sh.confirmer.current.cancel)
}

// renderApplyRemoves shows the owner's WF33 case: the Edit tab's Apply of a Config Map whose text
// lost the key "added".
func renderApplyRemoves(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 10000)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	ref := model.ObjectRef{Context: "demo", Kind: "configmaps", Namespace: "walk-test", Name: "walk-cm"}
	effects := engine.DataKeysEffects(nil, []string{"added"})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{
			Command: "object.apply", Label: "Apply", Danger: engine.DangerDestructive, Origin: engine.OriginGUI, Context: "demo",
			Namespaces: []string{"walk-test"}, Objects: []model.ObjectRef{ref}, Kinds: []string{"Config Map"},
			Effects: effects, Detail: engine.EffectsText(effects),
		})
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	savePNG(t, w, name)
	test.Tap(sh.confirmer.current.cancel)
	<-done
}

func TestRenderApplyRemovesDark(t *testing.T) {
	renderApplyRemoves(t, ThemeDark, "confirm_apply_removes_dark.png")
}

func TestRenderApplyRemovesLight(t *testing.T) {
	renderApplyRemoves(t, ThemeLight, "confirm_apply_removes_light.png")
}

// A request that carries only a Detail text (none of Cogpit's commands since D-100) still shows it,
// in a row "Effect": what the user confirms is never hidden.
func TestConfirmationShowsADetailWithoutEffects(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	go func() {
		_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{Command: "x.y", Label: "Run", Danger: engine.DangerDestructive,
			Origin: engine.OriginExternalAgent, Context: "demo", Detail: "Something only this text says."})
	}()
	q.until(t, "the dialog", func() bool { return sh.confirmer.current != nil })
	if all := texts(w.Canvas().Overlays().Top()); !strings.Contains(all, "Effect") || !strings.Contains(all, "Something only this text says.") ||
		strings.Contains(all, "Object") {
		t.Errorf("the rows: %s", all)
	}
	test.Tap(sh.confirmer.current.cancel)
}

// The data footer: Add Key at the start, the edited state's pills, Revert and Save at the end on one
// line where the details' width has room, else the pills on a line of their own above the buttons,
// at its end (WF31); hidden pills take no room.
func TestDataFooterLayout(t *testing.T) {
	test.NewTempApp(t)
	gap := theme.Padding()
	rect := func(w, h float32) *canvas.Rectangle {
		r := canvas.NewRectangle(nil)
		r.SetMinSize(fyne.NewSize(w, h))
		return r
	}
	add, pills, revert, save := rect(90, 36), rect(200, 20), rect(70, 36), rect(70, 36)
	l := &dataFooterLayout{width: 600}
	objs := []fyne.CanvasObject{add, pills, revert, save}
	if got, want := l.MinSize(objs), fyne.NewSize(90+200+70+70+3*gap, 36); got != want {
		t.Errorf("one line: min %v, want %v", got, want)
	}
	l.Layout(objs, fyne.NewSize(600, 36))
	if add.Position() != fyne.NewPos(0, 0) || save.Position().X != 530 || revert.Position().X != 530-gap-70 ||
		pills.Position() != fyne.NewPos(530-gap-70-gap-200, 8) {
		t.Errorf("one line: add %v pills %v revert %v save %v", add.Position(), pills.Position(), revert.Position(), save.Position())
	}

	l.width = 380 // too narrow for all four: the pills above the buttons
	if got, want := l.MinSize(objs), fyne.NewSize(90+70+70+2*gap, 20+gap+36); got != want {
		t.Errorf("two lines: min %v, want %v", got, want)
	}
	l.Layout(objs, fyne.NewSize(380, 20+gap+36))
	if pills.Position() != fyne.NewPos(180, 0) || add.Position().Y != 20+gap || save.Position() != fyne.NewPos(310, 20+gap) {
		t.Errorf("two lines: add %v pills %v save %v", add.Position(), pills.Position(), save.Position())
	}

	pills.Hide() // nothing edited: one line however narrow, no room for the pills
	if got, want := l.MinSize(objs), fyne.NewSize(90+70+70+2*gap, 36); got != want {
		t.Errorf("no pills: min %v, want %v", got, want)
	}
}
