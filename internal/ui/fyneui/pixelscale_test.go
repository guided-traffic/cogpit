package fyneui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// scaledCanvas is a canvas at a Fyne scale with a framebuffer density, as Fyne's GL driver
// multiplies them in PixelCoordinateForPosition.
type scaledCanvas struct {
	fyne.Canvas
	scale, density float32
}

func (c scaledCanvas) Scale() float32 { return c.scale }

func (c scaledCanvas) PixelCoordinateForPosition(p fyne.Position) (int, int) {
	m := c.scale * c.density
	return int(math.Round(float64(p.X * m))), int(math.Round(float64(p.Y * m)))
}

// TestCanvasDensity (D-121): the density is the framebuffer's pixels per canvas pixel, whatever
// Fyne's own scale.
func TestCanvasDensity(t *testing.T) {
	for _, c := range []struct {
		name   string
		canvas fyne.Canvas
		want   float32
	}{
		{"a display at 1x", test.NewCanvas(), 1},
		{"a Retina display", retinaCanvas{test.NewCanvas()}, 2},
		{"a Retina display at Fyne's scale 1.25", scaledCanvas{test.NewCanvas(), 1.25, 2}, 2},
		{"a display at 1.5x, Fyne's scale 1", scaledCanvas{test.NewCanvas(), 1, 1.5}, 1.5},
	} {
		if got := canvasDensity(c.canvas); math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("%s: density %v, want %v", c.name, got, c.want)
		}
	}
}

// TestThemeAppliedAgainWhenThePixelScaleChanges (D-121, a colleague's soft text after a wake):
// each of macOS's reports that the window's backing properties changed reads the density again;
// the theme is applied again, the same theme, once for each change of the density and never
// without one (a change of the colour space only, the same scale reported twice, a rounding's
// noise, a window without a framebuffer for a moment).
func TestThemeAppliedAgainWhenThePixelScaleChanges(t *testing.T) {
	sh, _, q := newTestShell(t, newFakeEngine(t), Options{Theme: ThemeLight})
	density := float32(2)
	var report func()
	sh.startDensityWatch(func() float32 { return density }, func(w fyne.Window, fn func()) {
		if w != sh.win {
			t.Errorf("the watch on %v, want the shell's window", w)
		}
		report = fn
	})
	if report == nil {
		t.Fatal("no report handed to the watch")
	}
	before := sh.app.Settings().Theme()
	applied := 0
	sh.app.Settings().AddListener(func(fyne.Settings) { applied++ })

	for _, step := range []struct {
		name    string
		density float32
		want    int // the theme's re-applies so far
	}{
		{"the colour space only", 2, 0},
		{"the displays off: 1x", 1, 1},
		{"the same scale reported again", 1, 1},
		{"the Retina display back", 2, 2},
		{"a rounding's noise", 2.00004, 2},
		{"a window without a framebuffer for a moment", 0, 2},
		{"the same 2x after it", 2, 2},
		{"a display at 1.5x", 1.5, 3},
	} {
		density = step.density
		was := applied
		report()
		if applied != was {
			t.Fatalf("%s: the theme applied before the check reached the main goroutine", step.name)
		}
		q.drain()
		if applied != step.want {
			t.Errorf("%s: the theme applied again %d times, want %d", step.name, applied, step.want)
		}
	}
	if got := sh.app.Settings().Theme(); got != before {
		t.Errorf("the theme %v after the re-applies, want the same theme %v", got, before)
	}
	if th, ok := sh.app.Settings().Theme().(*cogTheme); !ok || th.mode != ThemeLight {
		t.Errorf("the theme's mode changed: %v", sh.app.Settings().Theme())
	}
}
