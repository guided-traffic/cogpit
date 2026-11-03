package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A setting's switch draws its label at its left and the track at its right edge; a tap on the
// label switches, as a checkbox's label does. An integration's switch draws no label.
func TestLabelledSwitch(t *testing.T) {
	test.NewTempApp(t)
	var got []bool
	s := newLabelledSwitch("Reduce motion", func(on bool) { got = append(got, on) })
	ms := s.MinSize()
	label := fyne.MeasureText("Reduce motion", fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameText), fyne.TextStyle{})
	if ms.Width < label.Width+switchLabelGap+switchWidth || ms.Height < switchHeight {
		t.Fatalf("min size %v for a label of %v", ms, label)
	}
	s.Resize(fyne.NewSize(600, ms.Height))
	r := test.WidgetRenderer(s).(*pillSwitchRenderer)
	if r.label.Text != "Reduce motion" || r.label.Position().X != 0 {
		t.Errorf("label %q at %v", r.label.Text, r.label.Position())
	}
	if x := r.track.Position().X + r.track.Size().Width; x != 600-2 {
		t.Errorf("the track ends at %v of 600", x)
	}
	test.TapAt(s, fyne.NewPos(10, ms.Height/2)) // on the label
	if !s.Checked || !slices.Equal(got, []bool{true}) {
		t.Errorf("a tap on the label: checked %v, changes %v", s.Checked, got)
	}

	plain := newPillSwitch("Prometheus", nil)
	plain.Resize(plain.MinSize())
	if r := test.WidgetRenderer(plain).(*pillSwitchRenderer); r.label.Text != "" || plain.MinSize().Width != switchWidth+4 {
		t.Errorf("an integration's switch draws %q, %v wide", r.label.Text, plain.MinSize().Width)
	}
}

// A switch group holds a check group's value: the options switched on, in their order; SetSelected
// reports once.
func TestSwitchGroup(t *testing.T) {
	test.NewTempApp(t)
	var calls [][]string
	g := newSwitchGroup([]string{"CPU", "Memory", "Network"}, func(sel []string) { calls = append(calls, sel) })
	test.WidgetRenderer(g)
	g.SetSelected([]string{"Network", "CPU"})
	if !slices.Equal(g.Selected, []string{"CPU", "Network"}) || len(calls) != 1 {
		t.Fatalf("selected %v, %d changes", g.Selected, len(calls))
	}
	test.Tap(g.switches[1])
	if want := []string{"CPU", "Memory", "Network"}; !slices.Equal(g.Selected, want) || len(calls) != 2 || !slices.Equal(calls[1], want) {
		t.Errorf("after Memory's tap: selected %v, changes %v", g.Selected, calls)
	}
	if n := len(findAll[*pillSwitch](g)); n != 3 {
		t.Errorf("%d switches drawn", n)
	}
}

// An integration's tile stands at the row's left with its top level with the name's capitals (the
// owner's QS2): not above the name, and the lines beside it start after it.
func TestIntegrationRowTileAtTitle(t *testing.T) {
	test.NewTempApp(t)
	in, _ := engine.IntegrationOf(engine.IntegrationPrometheus)
	r := newIntegrationRow(in, nil)
	r.box.Resize(fyne.NewSize(600, r.box.MinSize().Height))
	lines := r.box.Objects[1]
	l := tileAtTitle{first: lines.(*fyne.Container).Objects[0], title: r.title}
	tile := r.tile.Position()
	text := findAll[*canvas.Text](r.title)[0]
	textTop := fyne.CurrentApp().Driver().AbsolutePositionForObject(text).Y - fyne.CurrentApp().Driver().AbsolutePositionForObject(r.box).Y
	if tile.X != 0 || tile.Y != l.tileTop() || tile.Y <= textTop || tile.Y >= textTop+text.MinSize().Height/2 {
		t.Errorf("tile at %v (want y %v), the name's text from %v", tile, l.tileTop(), textTop)
	}
	if x := lines.Position().X; x != r.tile.Size().Width+tileGap {
		t.Errorf("the lines start at %v", x)
	}
}

// Every on/off setting of Preferences and of a cluster's Settings is a switch: no section shows a
// checkbox.
func TestSettingsShowSwitchesOnly(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	check := func(page, section string) {
		settle(w)
		if checks := findAll[*widget.Check](sh.settings.root); len(checks) > 0 {
			t.Errorf("%s › %s shows %d checkboxes, the first %q", page, section, len(checks), checks[0].Text)
		}
	}
	for _, sec := range engine.PreferencesSections {
		mustDispatch(t, s, "preferences.open", map[string]any{"section": sec})
		q.until(t, "Preferences › "+sec, func() bool { return sh.settings.visible() && sh.settings.shown.Section == sec })
		check("Preferences", sec)
	}
	for _, sec := range engine.ClusterSettingsSections {
		mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": sec})
		q.until(t, "Cluster Settings › "+sec, func() bool { return sh.settings.visible() && sh.settings.shown.Section == sec })
		check("Cluster Settings", sec)
	}
}
