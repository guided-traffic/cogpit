package fyneui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Drilling into a cluster slides its tree over the section's clusters, ‹ All Clusters slides it
// out to the right again (navigator_slide.go): once settled, the level shown covers the navigator
// and the other is hidden, the tree out of sight at the section. The test driver runs a slide to
// its end at once.
func TestNavigatorSlideSettles(t *testing.T) {
	sh, w, q, _ := sectionScene(t, ThemeDark)
	s, n := sh.side, sh.side.sec
	settle(w)
	width := s.slide.box.Size().Width
	if width <= 0 {
		t.Fatal("the navigator has no width")
	}
	if s.tree.Visible() || s.slide.off != 1 || s.tree.Position().X != width || s.slide.started != 0 {
		t.Errorf("the section at the start: tree shown %v, off %v, x %v, %d slides", s.tree.Visible(), s.slide.off,
			s.tree.Position().X, s.slide.started)
	}

	test.Tap(n.rows[secProd].next)
	q.until(t, "prod's tree", func() bool { return s.tree.Visible() && !n.Visible() })
	if s.slide.off != 0 || s.tree.Position().X != 0 || s.slide.anim != nil || s.slide.shade.Visible() || s.slide.started != 1 {
		t.Errorf("drilled in: off %v, x %v, running %v, shade %v, %d slides", s.slide.off, s.tree.Position().X,
			s.slide.anim != nil, s.slide.shade.Visible(), s.slide.started)
	}

	test.Tap(s.head.back)
	q.until(t, "the section", func() bool { return n.Visible() && !s.tree.Visible() })
	if s.slide.off != 1 || s.tree.Position().X != width || s.slide.anim != nil || s.slide.started != 2 {
		t.Errorf("back up: off %v, x %v, running %v, %d slides", s.slide.off, s.tree.Position().X, s.slide.anim != nil,
			s.slide.started)
	}
}

// Another cluster's tree slides in vertically, in the order All Clusters lists the clusters:
// kind-cogpit is listed above prod, so prod comes in from below and kind-cogpit from above; once
// settled the tree is in place and the leaving cluster's picture gone.
func TestNavigatorSlideBetweenClusters(t *testing.T) {
	sh, w, q, _ := sectionScene(t, ThemeDark)
	s := sh.side
	if !s.sec.listedAbove(secKind, secProd) || s.sec.listedAbove(secProd, secKind) || s.sec.listedAbove("nowhere", secKind) {
		t.Fatal("the section's order")
	}
	test.Tap(s.sec.rows[secKind])
	q.until(t, "kind's tree", func() bool { return s.tree.Visible() && sh.ui.Context == secKind })
	settle(w)
	started := s.slide.started

	for _, step := range []struct {
		to   string
		down float32
	}{{secProd, 1}, {secKind, -1}} {
		s.setScope(engine.NavScopeCluster(step.to))
		q.until(t, step.to+"'s tree", func() bool { return sh.ui.ScopeCluster() == step.to })
		started++
		sl := s.slide
		if sl.started != started || sl.down != step.down || sl.v != 0 || sl.anim != nil || sl.old.Visible() || sl.old.Image != nil ||
			s.tree.Position().Y != 0 {
			t.Errorf("to %s: %d slides, down %v, v %v, running %v, picture %v, y %v", step.to, sl.started, sl.down, sl.v,
				sl.anim != nil, sl.old.Visible(), s.tree.Position().Y)
		}
	}
}

// With Reduce motion on nothing slides: neither into a cluster nor from one cluster to another.
func TestNavigatorSlideReduceMotion(t *testing.T) {
	f := sectionFake(t)
	f.ui.Preferences = &engine.Preferences{ReduceMotion: true}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the section's clusters", func() bool { return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 })
	s := sh.side
	test.Tap(s.sec.rows[secKind])
	q.until(t, "kind's tree", func() bool { return s.tree.Visible() && !s.sec.Visible() })
	s.setScope(engine.NavScopeCluster(secProd))
	q.until(t, "prod's tree", func() bool { return sh.ui.ScopeCluster() == secProd })
	test.Tap(s.head.back)
	q.until(t, "the section", func() bool { return s.sec.Visible() && !s.tree.Visible() })
	if s.slide.started != 0 || s.slide.off != 1 || s.slide.down != 0 {
		t.Errorf("%d slides, off %v, down %v", s.slide.started, s.slide.off, s.slide.down)
	}
}

// Preferences › App › Reduce motion is off at first; the user's tap turns the preference on and off.
func TestPreferencesReduceMotion(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["reduceMotion"].(*widget.Check); return ok })
	check := sh.settings.controls["reduceMotion"].(*widget.Check)
	if check.Checked || check.Text != reduceMotionCheck {
		t.Fatalf("checked %v, label %q", check.Checked, check.Text)
	}
	test.Tap(check)
	q.until(t, "on", func() bool { p, _ := s.Preferences(); return p.ReduceMotion && check.Checked })
	test.Tap(check)
	q.until(t, "off", func() bool { p, _ := s.Preferences(); return !p.ReduceMotion && !check.Checked })
}

// Halfway through a slide into a cluster both levels show, the tree from the middle on with the
// shadow at its left edge, and both are clipped to the navigator.
func TestRenderNavigatorSlideDark(t *testing.T) {
	renderNavigatorSlide(t, ThemeDark, "navigator_slide_dark.png")
}
func TestRenderNavigatorSlideLight(t *testing.T) {
	renderNavigatorSlide(t, ThemeLight, "navigator_slide_light.png")
}

func renderNavigatorSlide(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := sectionScene(t, mode)
	s := sh.side
	test.Tap(s.sec.rows[secKind])
	q.until(t, "the tree", func() bool { return s.tree.Visible() && s.tree.rows["deployments"] != nil })
	s.sec.Show()
	s.slide.off = 0.5
	settle(w)
	if !s.slide.shade.Visible() {
		t.Error("no shadow halfway")
	}
	savePNG(t, w, name)
}

// A third of the way through a change from kind-cogpit to prod: prod's tree comes in from below, the picture
// of kind-cogpit's leaves upwards, both clipped to the navigator.
func TestRenderNavigatorSwapDark(t *testing.T) {
	renderNavigatorSwap(t, ThemeDark, "navigator_swap_dark.png")
}
func TestRenderNavigatorSwapLight(t *testing.T) {
	renderNavigatorSwap(t, ThemeLight, "navigator_swap_light.png")
}

func renderNavigatorSwap(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := sectionScene(t, mode)
	s := sh.side
	test.Tap(s.sec.rows[secKind])
	q.until(t, "kind's tree", func() bool { return s.tree.Visible() && s.tree.rows["deployments"] != nil })
	settle(w)
	img := s.slide.picture()
	if img == nil {
		t.Fatal("no picture of the navigator")
	}
	if b, want := img.Bounds().Size(), s.slide.box.Size(); math.Abs(float64(b.X)-float64(want.Width)) > 1 ||
		math.Abs(float64(b.Y)-float64(want.Height)) > 1 { // the canvas's scale is 1: a pixel a point, rounded
		t.Errorf("picture %v, navigator %v", b, want)
	}
	s.setScope(engine.NavScopeCluster(secProd))
	q.until(t, "prod's tree", func() bool { return sh.ui.ScopeCluster() == secProd })
	sl := s.slide
	sl.old.Image = img
	sl.old.Show()
	sl.down, sl.v = 1, 0.7 // the old tree's lower rows still in sight
	settle(w)
	savePNG(t, w, name)
}

// macOS's Reduce motion stops the slides as the preference does, and every motion asks the same
// question: the navigator, the hotbar's and the strips' drags, the connection's blink.
func TestReduceMotionIsThePreferenceOrTheSystems(t *testing.T) {
	sh, _, q, _ := sectionScene(t, ThemeDark)
	for _, c := range []struct {
		pref, system, want bool
	}{{false, false, false}, {true, false, true}, {false, true, true}, {true, true, true}} {
		sh.ui.Preferences = &engine.Preferences{ReduceMotion: c.pref}
		sh.systemReduce = func() bool { return c.system }
		if got := sh.reduceMotion(); got != c.want {
			t.Errorf("preference %v, macOS %v: %v", c.pref, c.system, got)
		}
		for name, reduce := range map[string]func() bool{"hotbar": sh.hot.drag.reduce, "tab bar": sh.tabs.drag.reduce,
			"dock": sh.dock.drag.reduce, "blink": sh.blink.reduce} {
			if reduce() != c.want {
				t.Errorf("preference %v, macOS %v: the %s's %v", c.pref, c.system, name, !c.want)
			}
		}
	}

	sh.ui.Preferences = nil
	s := sh.side
	test.Tap(s.sec.rows[secKind])
	q.until(t, "kind's tree", func() bool { return s.tree.Visible() && !s.sec.Visible() })
	if s.slide.started != 0 {
		t.Errorf("macOS's Reduce motion: %d slides", s.slide.started)
	}
}
