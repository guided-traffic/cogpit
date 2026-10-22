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

// A dialog's Context row (the owner's alpha walk, AW7, 2026-10-22: "Dann lass es gerne in dem Format
// wie es ist, aber grau wäre top"): a source context's id stays as it is, "<context> [<label>]", the
// context in the text colour and its source label in the kind word's grey, on the context's line
// where it fits, else wrapped as before; the paragraph keeps the whole id; an id without a label is
// the context alone.
func TestContextRowMutesTheSourceLabel(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	th := fyne.CurrentApp().Settings().Theme()
	fg, muted := themeColor(th, theme.ColorNameForeground), themeColor(th, colorMuted)
	size := th.Size(theme.SizeNameText)
	const name, label = "kind-cogpit", "[kubernetes_configs/walk-kind.yaml]"
	id := name + " " + label
	value := func(context string, width float32) (*paragraph, []*canvas.Text) {
		t.Helper()
		rows := box(kvLayout{})
		placeRows(rows, context, nil, nil, width)
		p, ok := rows.Objects[1].(*paragraph)
		if !ok {
			t.Fatalf("the Context row's value is a %T", rows.Objects[1])
		}
		p.Resize(p.MinSize())
		return p, findAll[*canvas.Text](p)
	}
	type drawn struct {
		text  string
		muted bool
		x, y  float32
	}
	draw := func(texts []*canvas.Text) []drawn {
		var out []drawn
		for _, c := range texts {
			if c.Color != fg && c.Color != muted {
				t.Errorf("%q drawn in %v", c.Text, c.Color)
			}
			out = append(out, drawn{c.Text, c.Color == muted, c.Position().X, c.Position().Y})
		}
		return out
	}

	p, texts := value(id, 600) // room for both: one line
	if p.text != id {
		t.Errorf("the paragraph holds %q, want the whole id", p.text)
	}
	got := draw(texts)
	if len(got) != 2 || got[0] != (drawn{name, false, 0, 0}) || got[1].text != label || !got[1].muted || got[1].y != 0 ||
		got[1].x <= fyne.MeasureText(name, size, fyne.TextStyle{}).Width {
		t.Errorf("one line: %+v", got)
	}
	labelW := fyne.MeasureText(label, size, fyne.TextStyle{}).Width
	_, texts = value(id, labelW+2) // the label wraps to a line of its own, as before
	got = draw(texts)
	if len(got) != 2 || got[0] != (drawn{name, false, 0, 0}) || got[1].text != label || !got[1].muted || got[1].x != 0 || got[1].y <= 0 {
		t.Errorf("wrapped: %+v", got)
	}
	_, texts = value(id, labelW/2) // a label wider than the column breaks anywhere, grey to its end
	got = draw(texts)
	if len(got) < 3 || got[0] != (drawn{name, false, 0, 0}) {
		t.Fatalf("broken label: %+v", got)
	}
	var rest string
	for _, d := range got[1:] {
		if !d.muted || d.x != 0 {
			t.Errorf("a piece of the label: %+v", d)
		}
		rest += d.text
	}
	if rest != label {
		t.Errorf("the label's pieces %q", rest)
	}
	_, texts = value("demo", 600) // no source: the context alone
	if got = draw(texts); len(got) != 1 || got[0] != (drawn{"demo", false, 0, 0}) {
		t.Errorf("demo: %+v", got)
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
	if want := []string{"Context", "Namespace", "Object", "Config Map →", "Removes", "Requested by", "This cannot be undone."}; !slices.Equal(lines, want) { // the footer's note last
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
// lost the key "added", on a context of a synced folder's file, its source label grey (AW7).
func renderApplyRemoves(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 10000)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	const walkContext = "kind-cogpit [kubernetes_configs/walk-kind.yaml]"
	ref := model.ObjectRef{Context: walkContext, Kind: "configmaps", Namespace: "walk-test", Name: "walk-cm"}
	effects := engine.DataKeysEffects(nil, []string{"added"})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sh.confirmer.Confirm(context.Background(), engine.ConfirmRequest{
			Command: "object.apply", Label: "Apply", Danger: engine.DangerDestructive, Origin: engine.OriginGUI, Context: walkContext,
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
