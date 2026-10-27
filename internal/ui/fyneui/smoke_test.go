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
// window's frame back until it holds a drawn window, reports its size in pixels once, without an
// error, and its reads end; the shell's window at its start is a drawn one.
func TestSmokeReportsTheFirstDrawnFrame(t *testing.T) {
	type report struct {
		w, h int
		err  error
	}
	var got []report
	sh, w, q := newTestShell(t, newFakeEngine(t), Options{Smoke: func(width, height int, err error) {
		got = append(got, report{width, height, err})
	}})
	ended := sh.startSmoke()
	q.until(t, "the smoke run's report", func() bool { return len(got) > 0 })
	select {
	case <-ended:
	case <-time.After(15 * time.Second):
		t.Fatal("the smoke run's reads go on after its report")
	}
	q.drain()
	size := w.Canvas().Size()
	want := report{int(size.Width * w.Canvas().Scale()), int(size.Height * w.Canvas().Scale()), nil}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("reports %+v, want one: %+v", got, want)
	}
}

// TestSmokeFrameHoldsMoreThanOneColour: a frame of one colour is no drawn window yet, one with
// anything on it is, at its size in pixels.
func TestSmokeFrameHoldsMoreThanOneColour(t *testing.T) {
	test.NewTempApp(t)
	bg := canvas.NewRectangle(color.NRGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})
	w := test.NewTempWindow(t, bg)
	w.SetPadded(false)
	w.Resize(fyne.NewSize(200, 100))
	if fw, fh, err := presentedFrame(w.Canvas()); fw != 0 || fh != 0 || err != nil {
		t.Fatalf("a frame of one colour: %d×%d, %v; want 0×0 and no error", fw, fh, err)
	}
	dot := canvas.NewRectangle(color.White)
	dot.Resize(fyne.NewSize(2, 2))
	dot.Move(fyne.NewPos(150, 70))
	w.SetContent(container.NewStack(bg, container.NewWithoutLayout(dot)))
	if fw, fh, err := presentedFrame(w.Canvas()); fw != 200 || fh != 100 || err != nil {
		t.Fatalf("a frame with a dot on it: %d×%d, %v; want 200×100 and no error", fw, fh, err)
	}
}

// noSurface is a canvas whose window has no GL surface: Fyne's GL canvas captures nothing then.
type noSurface struct{ fyne.Canvas }

func (noSurface) Capture() image.Image { return nil }

// TestSmokeWindowWithoutASurface: a window the driver could not give a GL surface (its creation
// failed) has no frame to read back, which ends the smoke run with that error at once instead of
// at its bound.
func TestSmokeWindowWithoutASurface(t *testing.T) {
	if fw, fh, err := presentedFrame(noSurface{}); fw != 0 || fh != 0 || err == nil {
		t.Fatalf("no surface: %d×%d, %v; want 0×0 and an error", fw, fh, err)
	}
}
