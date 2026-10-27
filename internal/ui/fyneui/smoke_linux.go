//go:build linux

package fyneui

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <X11/Xutil.h>

// What smokeXRead found: a read image, or why there is none.
enum { smokeXOK, smokeXHidden, smokeXOffScreen, smokeXNoImage, smokeXFailed };

static int smokeXErr;

static int smokeXError(Display *d, XErrorEvent *e) {
	(void)d;
	if (!smokeXErr)
		smokeXErr = e->error_code;
	return 0;
}

typedef struct {
	int status, width, height, drawn, error;
	unsigned long first, red, green, blue;
	char errorText[96];
} smokeXFrame;

// smokeXRead reads what window win shows on the X server: its size and whether its part on the
// screen holds more than one pixel value on a grid of about samples points along its shorter
// side, else that value and the masks of its channels. The X errors of the read are caught (error,
// the first one's code, and its text) and the error handler the caller had comes back.
static smokeXFrame smokeXRead(Display *d, Window win, int samples) {
	smokeXFrame r = {0};
	XSync(d, False);
	int (*old)(Display *, XErrorEvent *) = XSetErrorHandler(smokeXError);
	smokeXErr = 0;
	XWindowAttributes wa, ra;
	if (!XGetWindowAttributes(d, win, &wa) || wa.map_state != IsViewable || !XGetWindowAttributes(d, wa.root, &ra)) {
		r.status = smokeXHidden;
	} else {
		r.width = wa.width;
		r.height = wa.height;
		int ax = 0, ay = 0;
		Window child;
		XTranslateCoordinates(d, win, wa.root, 0, 0, &ax, &ay, &child);
		// Only the part on the screen: XGetImage refuses a rectangle that is not.
		int x0 = ax < 0 ? -ax : 0, y0 = ay < 0 ? -ay : 0;
		int x1 = wa.width < ra.width - ax ? wa.width : ra.width - ax;
		int y1 = wa.height < ra.height - ay ? wa.height : ra.height - ay;
		XImage *img = NULL;
		if (x1 <= x0 || y1 <= y0)
			r.status = smokeXOffScreen;
		else if (!(img = XGetImage(d, win, x0, y0, x1 - x0, y1 - y0, AllPlanes, ZPixmap)))
			r.status = smokeXNoImage;
		if (img) {
			int step = (img->width < img->height ? img->width : img->height) / samples;
			if (step < 1)
				step = 1;
			r.first = XGetPixel(img, 0, 0);
			r.red = img->red_mask;
			r.green = img->green_mask;
			r.blue = img->blue_mask;
			for (int y = 0; y < img->height && !r.drawn; y += step)
				for (int x = 0; x < img->width; x += step)
					if (XGetPixel(img, x, y) != r.first) {
						r.drawn = 1;
						break;
					}
			XDestroyImage(img);
		}
	}
	XSync(d, False);
	XSetErrorHandler(old);
	if (smokeXErr) {
		r.status = smokeXFailed;
		r.error = smokeXErr;
		XGetErrorText(d, r.error, r.errorText, sizeof r.errorText);
	}
	return r;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"image/color"
	"math/bits"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// smokeFromX is where windowSystemFrame reads a frame.
const smokeFromX = "the X server's copy of the window"

// smokeX is a smoke run's own connection to the X server, to $DISPLAY as GLFW's is, opened at the
// first read; nil without one.
var smokeX struct {
	once sync.Once
	d    *C.Display
}

// windowSystemFrame reads the window's presented frame where the window system holds it: on X11
// the X server's copy of the window, what the screen shows. Under Xvfb, Mesa's software rasteriser
// (llvmpipe, as in CI's Linux Package job) presents a desktop GL window's back buffer to the X
// window and leaves the front buffer, which Capture reads, empty: Fyne's desktop GL build (amd64)
// reads one colour there whatever was drawn, its GLES build (arm64) the frame. An X error or a
// window not on the screen yet is a read without pixels, tried again at the next poll. ok is false
// off X11 (Wayland, Fyne's test driver).
func windowSystemFrame(w fyne.Window) (f SmokeFrame, ok bool) {
	nw, native := w.(driver.NativeWindow)
	if !native {
		return f, false
	}
	var win uintptr
	var x11 bool
	nw.RunNative(func(ctx any) {
		if xc, is := ctx.(driver.X11WindowContext); is {
			win, x11 = xc.WindowHandle, true
		}
	})
	if !x11 {
		return f, false
	}
	f.From = smokeFromX
	smokeX.once.Do(func() { smokeX.d = C.XOpenDisplay(nil) })
	switch {
	case smokeX.d == nil:
		f.Err = errors.New("the smoke run could not connect to the X server ($DISPLAY) to read the window's frame")
		return f, true
	case win == 0:
		f.Err = errors.New("the window has no X11 window to read a frame from")
		return f, true
	}
	r := C.smokeXRead(smokeX.d, C.Window(win), C.int(smokeSamples))
	f.Width, f.Height = int(r.width), int(r.height)
	switch r.status {
	case C.smokeXHidden:
		f.Unread = "the window is not viewable"
	case C.smokeXOffScreen:
		f.Unread = "the window is off the screen"
	case C.smokeXNoImage:
		f.Unread = "the X server gave no image of the window"
	case C.smokeXFailed:
		f.Unread = fmt.Sprintf("the X server refused the window's image: %s (X error %d)",
			C.GoString(&r.errorText[0]), int(r.error))
	default:
		f.Drawn = r.drawn != 0
		if !f.Drawn {
			p := uint64(r.first)
			f.Colour = color.NRGBA{R: channel(p, uint64(r.red)), G: channel(p, uint64(r.green)),
				B: channel(p, uint64(r.blue)), A: 0xff}
		}
	}
	return f, true
}

// channel is the 8-bit value of the channel that mask selects in pixel p.
func channel(p, mask uint64) uint8 {
	if mask == 0 {
		return 0
	}
	v, n := (p&mask)>>bits.TrailingZeros64(mask), bits.OnesCount64(mask)
	switch {
	case n == 8:
		return uint8(v)
	case n > 8:
		return uint8(v >> (n - 8))
	}
	return uint8(v * 0xff / (1<<n - 1))
}
