package fyneui

import (
	"errors"
	"image"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
)

// The smoke run (cogpit --smoke, D-112): the window opened with the real driver until the GL
// driver presents a drawn frame, then the app quits (Options.Smoke).

// smokePoll is how often a smoke run reads the window's presented frame back.
const smokePoll = 200 * time.Millisecond

// smokeSamples is about how many points of a frame's shorter side drawnFrame compares.
const smokeSamples = 256

// startSmoke reads the window's presented frame back every smokePoll, on the main goroutine, until
// one holds a drawn window or reading it fails: then it hands the frame's size in pixels, or the
// error, to Options.Smoke, once, and quits the app. It stops when the window closes; ended closes
// when its goroutine has.
func (sh *shell) startSmoke() (ended <-chan struct{}) {
	var reported atomic.Bool
	check := func() {
		if reported.Load() {
			return
		}
		w, h, err := presentedFrame(sh.win.Canvas())
		if err == nil && w == 0 {
			// Nothing drawn yet: ask for another frame, as a driver may give a front buffer it
			// allocated for the read its content only at the next swap, and Fyne paints only a
			// changed canvas.
			sh.win.Canvas().Refresh(sh.root)
			return
		}
		reported.Store(true)
		sh.opts.Smoke(w, h, err)
		sh.app.Quit()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(smokePoll)
		defer t.Stop()
		for !reported.Load() {
			select {
			case <-sh.ctx.Done():
				return
			case <-t.C:
				sh.do(check)
			}
		}
	}()
	return done
}

// presentedFrame reads back the frame the window last presented (Canvas.Capture, the GL driver's
// front buffer) and returns its size in pixels when it holds a drawn window, more than one colour;
// 0 × 0 while it holds none. A window without a GL surface (its creation failed, which Fyne logs,
// or it is closing) has no frame to read back: that is the error.
func presentedFrame(c fyne.Canvas) (w, h int, err error) {
	img := c.Capture()
	if img == nil {
		return 0, 0, errors.New("the window has no GL surface to read a frame from")
	}
	if img.Bounds().Empty() || !drawnFrame(img) {
		return 0, 0, nil
	}
	return img.Bounds().Dx(), img.Bounds().Dy(), nil
}

// drawnFrame reports whether img holds more than one colour, compared on a grid of about
// smokeSamples points along its shorter side: a frame the driver cleared and drew nothing on holds
// one.
func drawnFrame(img image.Image) bool {
	b := img.Bounds()
	step := max(1, min(b.Dx(), b.Dy())/smokeSamples)
	r0, g0, b0, a0 := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if r, g, bl, a := img.At(x, y).RGBA(); r != r0 || g != g0 || bl != b0 || a != a0 {
				return true
			}
		}
	}
	return false
}
