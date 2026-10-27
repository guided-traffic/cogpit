package fyneui

import (
	"errors"
	"image"
	"image/color"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
)

// The smoke run (cogpit --smoke, D-112): the window opened with the real driver until the GL
// driver presents a drawn frame, then the app quits (Options.Smoke).

// smokePoll is how often a smoke run reads the window's presented frame back.
const smokePoll = 200 * time.Millisecond

// smokeSamples is about how many points of a frame's shorter side frameColour compares.
const smokeSamples = 256

// smokeFromGL is where presentedFrame reads a frame without the window system's copy.
const smokeFromGL = "the GL front buffer"

// SmokeFrame is one read of the window's presented frame in a smoke run (Options.Smoke).
type SmokeFrame struct {
	// From is where the frame was read: the window system's copy of the window, or the GL front
	// buffer (presentedFrame).
	From string
	// Width and Height are the frame's size in pixels: 0 × 0 while the canvas has no size.
	Width, Height int
	// Drawn: the frame holds a drawn window, more than one colour. The run ends with it.
	Drawn bool
	// Colour is the one colour of a frame that holds no drawn window yet.
	Colour color.NRGBA
	// Unread is why the read found no pixels to compare ("the window is not viewable", an X
	// error), "" after a read; the run reads again at its next poll.
	Unread string
	// Err: the window has no frame to read (no GL surface, no X11 window, no connection to the X
	// server). The run ends with it.
	Err error
}

// startSmoke reads the window's presented frame back every smokePoll, on the main goroutine, and
// hands each read to Options.Smoke until one holds a drawn window or there is no frame to read;
// then it quits the app. It stops when the window closes; ended closes when its goroutine has.
func (sh *shell) startSmoke() (ended <-chan struct{}) {
	var reported atomic.Bool
	check := func() {
		if reported.Load() {
			return
		}
		f := presentedFrame(sh.win)
		sh.opts.Smoke(f)
		if !f.Drawn && f.Err == nil {
			// Nothing drawn yet: ask for another frame, as a driver may give a front buffer it
			// allocated for the read its content only at the next swap, and Fyne paints only a
			// changed canvas.
			sh.win.Canvas().Refresh(sh.root)
			return
		}
		reported.Store(true)
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

// presentedFrame reads the frame the window last presented: its size in pixels, and whether it
// holds a drawn window, more than one colour, or else its one colour. It reads it where the window
// system holds it, on X11 (windowSystemFrame), and elsewhere back from the GL driver's front buffer
// (Canvas.Capture). A window without a GL surface (its creation failed, which Fyne logs, or it is
// closing) has no frame to read back: that is the error.
func presentedFrame(w fyne.Window) SmokeFrame {
	if f, ok := windowSystemFrame(w); ok {
		return f
	}
	img := w.Canvas().Capture()
	if img == nil {
		return SmokeFrame{From: smokeFromGL, Err: errors.New("the window has no GL surface to read a frame from")}
	}
	b := img.Bounds()
	f := SmokeFrame{From: smokeFromGL, Width: b.Dx(), Height: b.Dy()}
	if !b.Empty() {
		f.Colour, f.Drawn = frameColour(img)
	}
	return f
}

// frameColour reports whether img holds more than one colour, compared on a grid of about
// smokeSamples points along its shorter side, and else returns its one colour: a frame the driver
// cleared and drew nothing on holds one.
func frameColour(img image.Image) (c color.NRGBA, drawn bool) {
	b := img.Bounds()
	step := max(1, min(b.Dx(), b.Dy())/smokeSamples)
	first := img.At(b.Min.X, b.Min.Y)
	r0, g0, b0, a0 := first.RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			if r, g, bl, a := img.At(x, y).RGBA(); r != r0 || g != g0 || bl != b0 || a != a0 {
				return color.NRGBA{}, true
			}
		}
	}
	return color.NRGBAModel.Convert(first).(color.NRGBA), false
}
