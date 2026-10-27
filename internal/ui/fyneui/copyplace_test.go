package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// chipWrapWith returns the drawer's chip or pill wrapper whose pill reads text.
func chipWrapWith(t *testing.T, d *drawer, match func(string) bool) *hoverWrap {
	t.Helper()
	for _, w := range findAll[*hoverWrap](d.details) {
		if p := findAll[*pill](w.content); len(p) == 1 && match(p[0].text) {
			return w
		}
	}
	t.Fatal("no matching chip in the drawer")
	return nil
}

// TestCopyIconBesideChips: the icon of a chip or a pill sits after it, outside, and centred on a
// single-line chip, as the icon of a text value sits after its text.
func TestCopyIconBesideChips(t *testing.T) {
	sh, w, _, _ := podDrawerScene(t, ThemeDark)
	d := sh.drawer
	test.Tap(drawerLinks(sh)["2 Labels"])
	settle(w)
	drv := fyne.CurrentApp().Driver()
	n := 0
	for _, hw := range findAll[*hoverWrap](d.details) {
		if len(findAll[*pill](hw.content)) != 1 {
			continue
		}
		n++
		hw.MouseIn(mouse())
		if !d.copyBtn.Visible() {
			t.Fatalf("no icon over the chip %v", findAll[*pill](hw.content)[0].text)
		}
		btn, chip := drv.AbsolutePositionForObject(d.copyBtn), drv.AbsolutePositionForObject(hw)
		text := findAll[*pill](hw.content)[0].text
		right := chip.X + hw.Size().Width - hw.gutter
		hostRight := drv.AbsolutePositionForObject(d.host).X + d.host.Size().Width
		if btn.X < right-0.5 && right+d.copyBtn.Size().Width <= hostRight {
			t.Errorf("the icon of %q at x=%v hangs inside the chip, which ends at %v", text, btn.X, right)
		}
		if hw.Size().Height < 2*d.copyBtn.Size().Height {
			if dy := (btn.Y + d.copyBtn.Size().Height/2) - (chip.Y + hw.Size().Height/2); dy < -0.5 || dy > 0.5 {
				t.Errorf("the icon of %q is %v off the chip's middle", text, dy)
			}
		}
		hw.MouseOut()
	}
	if n < 3 {
		t.Fatalf("only %d chips checked", n)
	}
}

// TestCopyIconSurvivesARebuild: a watch event that rebuilds the drawer keeps the icon at the value
// under the pointer, without a new MouseIn; moving the pointer away still hides it.
func TestCopyIconSurvivesARebuild(t *testing.T) {
	sh, w, q, ref := podDrawerScene(t, ThemeDark)
	d := sh.drawer
	status := chipWrapWith(t, d, func(s string) bool { return !strings.Contains(s, "=") })
	status.MouseIn(mouse())
	if !d.copyBtn.Visible() {
		t.Fatal("no icon over the chip")
	}
	old := d.copyOwner

	m := *d.last
	m.Sections = append([]model.DetailSection(nil), m.Sections...)
	changed := false
	for i := range m.Sections {
		items := append([]model.DetailItem(nil), m.Sections[i].Items...)
		for j := range items {
			if items[j].Name == "Status" {
				items[j].Value, changed = "Something else entirely", true
			}
		}
		m.Sections[i].Items = items
	}
	if !changed {
		t.Fatal("the model has no Status item")
	}
	showDetail(sh, w, q, &m)
	if d.copyOwner == old || d.copyOwner == nil {
		t.Fatalf("the drawer did not rebuild, or lost the value: owner %v", d.copyOwner)
	}
	if !d.copyBtn.Visible() {
		t.Fatal("the copy icon is gone after the rebuild")
	}
	if d.copyHook == nil || d.copyHook.text != "Something else entirely" {
		t.Errorf("the icon copies %v, want the new status", d.copyHook)
	}
	_ = ref

	// The driver sends the pointer's MouseOut to the widget it entered, which is gone.
	status.MouseOut()
	if d.copyBtn.Visible() {
		t.Error("the icon stays after the pointer moved away")
	}

	// A hidden icon is not brought back by a rebuild.
	m2 := m
	m2.Sections = append([]model.DetailSection(nil), m.Sections...)
	m2.Sections[0].Title += " "
	showDetail(sh, w, q, &m2)
	if d.copyBtn.Visible() {
		t.Error("a rebuild showed an icon nobody asked for")
	}
}

func TestRenderCopyIconPlacementDark(t *testing.T) { renderCopyPlacement(t, ThemeDark, "dark") }

// renderCopyPlacement renders the pod drawer with a label chip, a Pod IPs chip and a text value
// hovered in turn.
func renderCopyPlacement(t *testing.T, mode ThemeMode, suffix string) {
	sh, w, _, ref := podDrawerScene(t, mode)
	d := sh.drawer
	test.Tap(drawerLinks(sh)["2 Labels"])
	settle(w)
	label := chipWrapWith(t, d, func(s string) bool { return strings.Contains(s, "=") })
	label.MouseIn(mouse())
	savePNG(t, w, "drawer_copy_label_"+suffix+".png")
	label.MouseOut()
	ip := chipWrapWith(t, d, func(s string) bool { return strings.Count(s, ".") == 3 })
	ip.MouseIn(mouse())
	savePNG(t, w, "drawer_copy_podip_"+suffix+".png")
	ip.MouseOut()
	status := chipWrapWith(t, d, func(s string) bool { return !strings.Contains(s, "=") && strings.Count(s, ".") != 3 })
	status.MouseIn(mouse())
	savePNG(t, w, "drawer_copy_pill_"+suffix+".png")
	status.MouseOut()
	selLineWith(t, d, ref.Name).MouseIn(mouse())
	savePNG(t, w, "drawer_copy_text_"+suffix+".png")
}
