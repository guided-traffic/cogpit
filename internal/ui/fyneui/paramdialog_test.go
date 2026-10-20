package fyneui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// realCommand is one of Cogpit's commands as the engine registers it.
func realCommand(t testing.TB, id string) *engine.Command {
	t.Helper()
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	c, ok := r.Get(id)
	if !ok {
		t.Fatalf("no command %s", id)
	}
	return c
}

// openScaleProbe opens the Scale probe's dialog (the real Scale's parameters and dialog fields) for
// checkout-api with replicas prefilled.
func openScaleProbe(t *testing.T, replicas int) (*shell, fyne.Window, *mainQueue, *fakeEngine, *paramDialog) {
	t.Helper()
	f := kindsFake(t, "deployments")
	registerProbes(t, f)
	sh, w, q := settledShell(t, f, "deployments")
	cmd, _ := f.reg.Get("deployment.scale")
	ref := model.ObjectRef{Context: "demo", Kind: "deployments", Namespace: "checkout", Name: "checkout-api"}
	pre, _ := json.Marshal(map[string]any{"ref": ref, "replicas": replicas})
	pd := sh.openParamDialog(cmd, ref, pre)
	if pd == nil {
		t.Fatal("no dialog")
	}
	return sh, w, q, f, pd
}

// TestScaleSliderAndCount: the slider runs from 1 to 30 and the count beside it takes any number
// the schema allows (GW2): a drag writes the count, a typed count moves the slider, to its end
// when the count lies beyond it; a count the schema refuses says why under the field and greys
// Scale; 0 is only typed and is sent (the engine confirms it as destructive).
func TestScaleSliderAndCount(t *testing.T) {
	_, _, q, f, pd := openScaleProbe(t, 3)
	fld := pd.fields[0]
	if fld.slider == nil || fld.slider.Min != 1 || fld.slider.Max != 30 || fld.slider.Value != 3 || fld.entry.Text != "3" {
		t.Fatalf("slider %+v, count %q", fld.slider, fld.entry.Text)
	}
	fld.slider.SetValue(12) // as a drag ends there
	if fld.entry.Text != "12" {
		t.Errorf("a drag to 12 wrote %q", fld.entry.Text)
	}
	for _, tt := range []struct {
		typed   string
		slider  float64
		problem string
	}{
		{"50", 30, ""},
		{"0", 1, ""},
		{"25", 25, ""},
		{"70000", 25, "at most 65535"},
		{"2.5", 25, "not a whole number"},
		{"many", 25, "not a number"},
		{"", 25, "a number"},
		{"7", 7, ""},
	} {
		fld.entry.SetText(tt.typed)
		if fld.slider.Value != tt.slider {
			t.Errorf("%q: slider at %v, want %v", tt.typed, fld.slider.Value, tt.slider)
		}
		shown := ""
		if fld.problem.Visible() {
			shown = fld.problem.text
		}
		if shown != tt.problem || pd.confirm.Disabled() != (tt.problem != "") {
			t.Errorf("%q: problem %q, Scale disabled %v; want %q", tt.typed, shown, pd.confirm.Disabled(), tt.problem)
		}
		if tt.typed != "7" && tt.problem == "" && fld.entry.Text != tt.typed {
			t.Errorf("%q: the count became %q", tt.typed, fld.entry.Text)
		}
	}
	test.Tap(pd.confirm)
	q.until(t, "deployment.scale", func() bool { return len(f.callsOf("deployment.scale")) == 1 })
	if p := decodeParams[probeScale](t, f.callsOf("deployment.scale")[0]); p.Replicas == nil || *p.Replicas != 7 {
		t.Errorf("scale params %+v", p)
	}
}

// TestScaleToZeroIsSent: a typed 0 is sent; the engine's Escalate makes it destructive.
func TestScaleToZeroIsSent(t *testing.T) {
	_, _, q, f, pd := openScaleProbe(t, 3)
	pd.fields[0].entry.SetText("0")
	test.Tap(pd.confirm)
	q.until(t, "deployment.scale", func() bool { return len(f.callsOf("deployment.scale")) == 1 })
	if p := decodeParams[probeScale](t, f.callsOf("deployment.scale")[0]); p.Replicas == nil || *p.Replicas != 0 {
		t.Errorf("scale params %+v", p)
	}
}

// TestParamHintIsATooltip: no hint line stands under a field (GW6); the "?" after its name shows
// the hint in the dialog's own tooltip, over the dialog, on hover anywhere on the name's cell and
// at once on a click, a second click hiding it.
func TestParamHintIsATooltip(t *testing.T) {
	_, w, q, _, pd := openScaleProbe(t, 3)
	settle(w)
	top := w.Canvas().Overlays().Top()
	for _, txt := range findAll[*canvas.Text](top) {
		if strings.Contains(probeScaleHint, txt.Text) && len(txt.Text) > 12 {
			t.Fatalf("the hint stands in the dialog: %q", txt.Text)
		}
	}
	name := pd.fields[0].label
	if icons := findAll[*widget.Icon](name); len(icons) != 1 {
		t.Fatalf("%d icons after the name, want the ?", len(icons))
	}
	tip := pd.d.tip
	tip.delay = time.Millisecond
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(name).AddXY(2, 2) // the cell's corner, not the icon
	test.MoveMouse(w.Canvas(), at)
	q.until(t, "the hint's tooltip", func() bool { return tip.visibleText() == probeScaleHint })
	settle(w)
	var bubble fyne.CanvasObject
	if len(tip.layer.Objects) == 1 {
		bubble = tip.layer.Objects[0]
	}
	inTop := false
	for _, o := range findAll[fyne.CanvasObject](top) {
		if o == bubble {
			inTop = true
		}
	}
	if bubble == nil || !inTop {
		t.Fatal("the tooltip is not in the dialog's overlay")
	}
	pos, size := fyne.CurrentApp().Driver().AbsolutePositionForObject(bubble), bubble.Size()
	win := w.Canvas().Size()
	if pos.X < 0 || pos.Y < 0 || pos.X+size.Width > win.Width || pos.Y+size.Height > win.Height {
		t.Errorf("the tooltip at %v %v leaves the window %v", pos, size, win)
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(5, 5))
	if tip.visibleText() != "" {
		t.Error("the tooltip stays after the pointer left")
	}
	test.Tap(name)
	if tip.visibleText() != probeScaleHint {
		t.Error("a click does not show the hint at once")
	}
	test.Tap(name)
	if tip.visibleText() != "" {
		t.Error("a second click does not hide it")
	}
}

// TestDrainDialogFields: the real Drain's options, prefilled with its defaults, each with its hint
// for a person (D-104), the checks as checks and the numbers in fields sized to five digits, the
// dialog narrower than the old 420-point form.
func TestDrainDialogFields(t *testing.T) {
	f := kindsFake(t, "nodes")
	sh, w, _ := settledShell(t, f, "nodes")
	cmd := realCommand(t, "node.drain")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: cordonedNode}
	pre, err := cmd.Prefill(nil, cmd.ObjectParams(ref, ""))
	if err != nil {
		t.Fatal(err)
	}
	pd := sh.openParamDialog(cmd, ref, pre)
	settle(w)
	var names []string
	checks, entries := 0, 0
	for _, fld := range pd.fields {
		names = append(names, fld.label.text)
		if fld.label.hint == "" || fld.label.hint == cmd.Schema.Properties[fld.name].Description {
			t.Errorf("%s: hint %q", fld.name, fld.label.hint)
		}
		switch {
		case fld.check != nil:
			checks++
		case fld.entry != nil:
			entries++
			if wd := fld.entry.Size().Width; wd > 90 || wd < 40 {
				t.Errorf("%s: a field %v wide for five digits", fld.name, wd)
			}
			if want := map[string]string{"gracePeriodSeconds": "", "timeoutSeconds": "300"}[fld.name]; fld.entry.Text != want {
				t.Errorf("%s prefilled %q, want %q (DrainSettings.WithDefaults)", fld.name, fld.entry.Text, want)
			}
		}
	}
	if strings.Join(names, ",") != "Ignore daemon sets,Delete empty dir data,Force,Grace period seconds,Timeout seconds" || checks != 3 || entries != 2 {
		t.Errorf("fields %v, %d checks, %d entries", names, checks, entries)
	}
	if !pd.fields[0].check.Checked {
		t.Error("Ignore daemon sets is not on by default")
	}
	if wd := pd.d.popup.Size().Width; wd >= 420 {
		t.Errorf("the dialog is %v wide", wd)
	}
}

// TestDialogTitleShapesTheArrowApart: a framed dialog's title names the object in the one form
// without the gap Fyne's one-run title leaves after the arrow (GW2): the arrow ends its own text,
// the name's text begins with the text font's narrow space.
func TestDialogTitleShapesTheArrowApart(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	title := objectDialogTitle("Scale", "Deployment", "gate-web", "")
	if got := titleString(title); got != "Scale Deployment → gate-web" {
		t.Fatalf("title %q", got)
	}
	if len(title.Segments) != 2 {
		t.Fatalf("%d segments", len(title.Segments))
	}
	head, name := title.Segments[0].(*widget.TextSegment).Text, title.Segments[1].(*widget.TextSegment).Text
	if !strings.HasSuffix(head, objectArrow) || !strings.HasPrefix(name, " ") {
		t.Fatalf("segments %q, %q", head, name)
	}
	bold := fyne.TextStyle{Bold: true}
	space := fyne.MeasureText(name, 14, bold).Width - fyne.MeasureText(strings.TrimSpace(name), 14, bold).Width
	if space > 6 {
		t.Errorf("the space after the arrow is %v wide", space)
	}
	if install := titleString(objectDialogTitle("Install", "Release", "web", "?")); install != "Install Release → web?" {
		t.Errorf("install title %q", install)
	}
}
