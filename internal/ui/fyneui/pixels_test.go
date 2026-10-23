package fyneui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// retinaCanvas is a canvas as Fyne's GL driver has it on macOS with a Retina display: its scale 1,
// its framebuffer twice its points (Fyne's texScale).
type retinaCanvas struct{ fyne.Canvas }

func (retinaCanvas) Scale() float32 { return 1 }

func (retinaCanvas) PixelCoordinateForPosition(p fyne.Position) (int, int) {
	return int(math.Round(float64(p.X * 2))), int(math.Round(float64(p.Y * 2)))
}

// TestLogoDrawnAtTheDisplaysPixels (the owner's question of 2026-10-23, G16): a picture Cogpit draws
// itself, the logo in the header and on the welcome page, has as many pixels as the display shows it
// at, not the canvas scale's, which stays 1 on a Retina display on macOS.
func TestLogoDrawnAtTheDisplaysPixels(t *testing.T) {
	for _, c := range []struct {
		name   string
		canvas fyne.Canvas
		want   int
	}{
		{"a Retina display", retinaCanvas{test.NewCanvas()}, 48},
		{"a display at 1x", test.NewCanvas(), 24},
		{"no canvas yet", nil, 24},
	} {
		if got := pixelsFor(c.canvas, 24); got != c.want {
			t.Errorf("%s: a 24 point logo drawn at %d pixels, want %d", c.name, got, c.want)
		}
	}
}
