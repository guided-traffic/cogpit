package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func toastCards(sh *shell) []*toastCard {
	var out []*toastCard
	for _, o := range sh.toasts.Objects {
		if c, ok := o.(*toastCard); ok {
			out = append(out, c)
		}
	}
	return out
}

// TestToastCloseButtonDismisses: the × removes the toast at once.
func TestToastCloseButtonDismisses(t *testing.T) {
	sh, _, _ := newTestShell(t, connectedFake(t, 3), Options{Theme: ThemeDark})
	sh.toast("error", "it broke")
	cards := toastCards(sh)
	if len(cards) != 1 {
		t.Fatalf("%d toasts, want 1", len(cards))
	}
	btns := findAll[*toastButton](cards[0])
	if len(btns) != 1 || len(findAll[*widget.Button](cards[0])) != 0 {
		t.Fatalf("%d close buttons in the toast, want one and no other button", len(btns))
	}
	test.Tap(btns[0])
	if len(toastCards(sh)) != 0 {
		t.Error("the toast stays after the close button")
	}
}

// TestToastLifetimeAndHover: a toast goes after its lifetime, but not while the pointer is over
// it; leaving starts the time again. Errors outlast infos.
func TestToastLifetimeAndHover(t *testing.T) {
	sh, _, q := newTestShell(t, connectedFake(t, 3), Options{Theme: ThemeDark})
	if toastLifetimes["error"] <= toastLifetimes["warning"] || toastLifetimes["warning"] <= toastLifetimes["info"] {
		t.Errorf("lifetimes %v: an error should outlast a warning, a warning an info", toastLifetimes)
	}
	sh.toastTime = time.Hour // the pointer reaches the toast before its time can run out
	sh.toast("info", "first")
	c := toastCards(sh)[0]
	if !toastArmed(c) {
		t.Fatal("a new toast's time does not run")
	}
	c.MouseIn(mouse())
	if toastArmed(c) {
		t.Fatal("the toast's time runs while the pointer is over it")
	}
	c.mu.Lock()
	c.life = time.Millisecond // leaving starts the lifetime again: a short one now
	c.mu.Unlock()
	c.MouseOut()
	q.until(t, "the toast to expire", func() bool { return len(toastCards(sh)) == 0 })
}

// toastArmed reports whether a toast's time runs: a toast with none armed never goes by itself.
func toastArmed(c *toastCard) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timer != nil
}

// TestToastSurfaceWidth: a toast is wide, and its text keeps the normal text size.
func TestToastSurfaceWidth(t *testing.T) {
	sh, w, _ := newTestShell(t, connectedFake(t, 3), Options{Theme: ThemeDark})
	sh.toast("warning", "Pod checkout-api-7d9f8b6c5-x2k4p is not ready yet")
	settle(w)
	c := toastCards(sh)[0]
	if c.Size().Width < 360 {
		t.Errorf("a toast is %v wide, want at least 360", c.Size().Width)
	}
	if h := c.Size().Height; h < fyne.CurrentApp().Settings().Theme().Size("text")*3 {
		t.Errorf("a toast is only %v high", h)
	}
}

// renderToasts: an info, a warning and an error, each with the copy icon at the end of its title
// line; the error's text selected from its second word into its second line (WF41, D-99).
func renderToasts(t *testing.T, mode ThemeMode, name string) {
	sh, w, _, _ := podDrawerScene(t, mode)
	sh.copyToast("info", copiedToast)
	sh.toast("warning", "Context demo lost its credentials; reconnect to read the cluster again")
	sh.toast("error", errorToast)
	settle(w)
	cards := toastCards(sh)
	text := cards[len(cards)-1].text
	_, lineH := text.metrics()
	from := fyne.NewPos(fyne.MeasureText("pods.delete: ", text.p.Theme().Size(text.p.size), text.p.style).Width, lineH/2)
	second := text.lines[1].text // up to "User" on the second line, wherever the text wraps
	to := fyne.NewPos(fyne.MeasureText(second[:strings.Index(second, "User")+len("User")], text.p.Theme().Size(text.p.size), text.p.style).Width, lineH*1.5)
	pressAt(text, from)
	dragTo(text, from, to)
	text.DragEnd()
	savePNG(t, w, name)
}

func TestRenderToastsDark(t *testing.T) { renderToasts(t, ThemeDark, "toasts_dark.png") }
