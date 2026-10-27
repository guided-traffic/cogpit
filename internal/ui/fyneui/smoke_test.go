package fyneui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// TestSmokeReportsTheFirstDrawnFrame: a smoke run (cogpit --smoke, Options.Smoke) reads the
// window's frame back and reports each read until one holds a drawn window, at its size in pixels,
// without an error; then its reads end. The shell's window at its start is a drawn one.
func TestSmokeReportsTheFirstDrawnFrame(t *testing.T) {
	var got []SmokeFrame
	sh, w, q := newTestShell(t, newFakeEngine(t), Options{Smoke: func(f SmokeFrame) { got = append(got, f) }})
	ended := sh.startSmoke()
	q.until(t, "the smoke run's drawn frame", func() bool { return len(got) > 0 && got[len(got)-1].Drawn })
	select {
	case <-ended:
	case <-time.After(15 * time.Second):
		t.Fatal("the smoke run's reads go on after its drawn frame")
	}
	q.drain()
	size := w.Canvas().Size()
	want := SmokeFrame{From: smokeFromGL, Width: int(size.Width * w.Canvas().Scale()),
		Height: int(size.Height * w.Canvas().Scale()), Drawn: true}
	if last := got[len(got)-1]; last != want {
		t.Fatalf("the last read %+v, want %+v", last, want)
	}
	for _, f := range got[:len(got)-1] {
		if f.Drawn || f.Err != nil {
			t.Fatalf("reads %+v: one ended the run before the last", got)
		}
	}
}

// TestSmokeFrameHoldsMoreThanOneColour: a frame of one colour is no drawn window yet, and the read
// names that colour; one with anything on it is drawn; both at their size in pixels.
func TestSmokeFrameHoldsMoreThanOneColour(t *testing.T) {
	test.NewTempApp(t)
	fill := color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff}
	bg := canvas.NewRectangle(fill)
	w := test.NewTempWindow(t, bg)
	w.SetPadded(false)
	w.Resize(fyne.NewSize(200, 100))
	if f := presentedFrame(w); f != (SmokeFrame{From: smokeFromGL, Width: 200, Height: 100, Colour: fill}) {
		t.Fatalf("a frame of one colour: %+v; want 200×100, not drawn, of %v", f, fill)
	}
	dot := canvas.NewRectangle(color.White)
	dot.Resize(fyne.NewSize(2, 2))
	dot.Move(fyne.NewPos(150, 70))
	w.SetContent(container.NewStack(bg, container.NewWithoutLayout(dot)))
	if f := presentedFrame(w); f != (SmokeFrame{From: smokeFromGL, Width: 200, Height: 100, Drawn: true}) {
		t.Fatalf("a frame with a dot on it: %+v; want 200×100, drawn", f)
	}
}

// noSurface is a canvas whose window has no GL surface: Fyne's GL canvas captures nothing then.
type noSurface struct{ fyne.Canvas }

func (noSurface) Capture() image.Image { return nil }

// noSurfaceWindow is a window with that canvas.
type noSurfaceWindow struct{ fyne.Window }

func (noSurfaceWindow) Canvas() fyne.Canvas { return noSurface{} }

// TestSmokeWindowWithoutASurface: a window the driver could not give a GL surface (its creation
// failed) has no frame to read back, which ends the smoke run with that error at once instead of
// at its bound.
func TestSmokeWindowWithoutASurface(t *testing.T) {
	if f := presentedFrame(noSurfaceWindow{}); f.Width != 0 || f.Height != 0 || f.Drawn || f.Err == nil {
		t.Fatalf("no surface: %+v; want 0×0, not drawn, and an error", f)
	}
}
