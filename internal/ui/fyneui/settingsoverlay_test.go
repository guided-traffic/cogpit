package fyneui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// overlayScene opens Preferences at section on the real engine, the card over the window.
func overlayScene(t *testing.T, section string) (*engine.Session, *shell, fyne.Window, *mainQueue) {
	t.Helper()
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": section})
	q.until(t, "the card", func() bool { return sh.settings.visible() && w.Canvas().Overlays().Top() == sh.settings.overlay })
	return s, sh, w, q
}

// The card is the window's top overlay, so the pointer reaches nothing behind it (Fyne hands it to
// the top overlay alone, D-118): a button behind the shade gets no hover and no tap; the tap on the
// shade closes the page instead, and the window's views stay where they are.
func TestSettingsOverlayKeepsThePointerFromTheWindow(t *testing.T) {
	_, sh, w, q := overlayScene(t, engine.SectionApp)
	c := w.Canvas()
	behind := findAll[*tipButton](sh.top.root)
	if len(behind) == 0 {
		t.Fatal("no button in the top bar")
	}
	b := behind[len(behind)-1]
	at := centre(b)
	if card := sh.settings.root; at.Y >= card.Position().Y {
		t.Fatalf("the top bar's button at %v is not above the card at %v", at, card.Position())
	}

	test.MoveMouse(c, at)
	if sh.tip.owner != nil {
		t.Errorf("a button behind the shade asked for its tooltip (%T)", sh.tip.owner)
	}
	test.TapCanvas(c, at)
	q.until(t, "the page closed by the shade", func() bool { return !sh.settings.visible() })
	if top := c.Overlays().Top(); top != nil {
		t.Errorf("the tap reached the window behind the shade: %T over it", top)
	}
}

// A tap on the card where nothing takes it (below the section list) leaves the page open; a tap and
// a right-click on the shade close it.
func TestSettingsOverlayClosesOnTheShadeOnly(t *testing.T) {
	s, sh, w, q := overlayScene(t, engine.SectionApp)
	c := w.Canvas()
	card := sh.settings.root
	test.TapCanvas(c, card.Position().AddXY(40, card.Size().Height-30))
	q.drain()
	if !sh.settings.visible() {
		t.Fatal("a tap on the card closed it")
	}
	sh.settings.overlay.TappedSecondary(&fyne.PointEvent{Position: fyne.NewPos(5, c.Size().Height/2)})
	q.until(t, "closed by a right-click on the shade", func() bool { return !sh.settings.visible() })

	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "staging"})
	q.until(t, "the cluster's card", func() bool { return sh.settings.visible() && sh.settings.title.text == "Settings - staging" })
	if !sh.settings.badge.Visible() || sh.settings.closeB.tip != "Close (Esc)" {
		t.Errorf("badge shown %v, the ×'s tooltip %q", sh.settings.badge.Visible(), sh.settings.closeB.tip)
	}
	pressEscape(w)
	q.until(t, "closed by Escape", func() bool { return !sh.settings.visible() && c.Overlays().Top() == nil })
}

// The card has a focus of its own: Tab goes round its fields and never on to one behind it.
func TestSettingsOverlayKeepsTabInTheCard(t *testing.T) {
	_, sh, w, q := overlayScene(t, engine.SectionTerminal)
	q.until(t, "the Terminal section", func() bool { return sh.settings.controls["terminalShell"] != nil })
	c := w.Canvas()
	inCard := findAll[fyne.CanvasObject](sh.settings.root)
	c.Focus(sh.settings.controls["terminalShell"].(*widget.Entry))
	seen := map[fyne.Focusable]bool{}
	for range 40 {
		c.FocusNext()
		f := c.Focused()
		if f == nil || !slices.Contains(inCard, f.(fyne.CanvasObject)) {
			t.Fatalf("Tab went to %T, not a field of the card", f)
		}
		seen[f] = true
	}
	if len(seen) < 3 {
		t.Errorf("Tab went round %d fields only", len(seen))
	}
}

// A value typed just before the page closes, or before another section shows, is set: by the ×, by
// Escape, by a click on the shade, by the section list (D-118; before, the typing pause was stopped
// and the value dropped). A refusal that comes after the page closed shows as a toast.
func TestSettingsOverlayTypedValueIsSetOnClose(t *testing.T) {
	s, sh, w, q := overlayScene(t, engine.SectionTerminal)
	p := sh.settings
	p.typing = time.Hour // the pause never ends by itself here
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }
	entry := func(name string) *widget.Entry {
		q.until(t, name, func() bool { return p.controls[name] != nil })
		return p.controls[name].(*widget.Entry)
	}
	reopen := func(section string) {
		mustDispatch(t, s, "preferences.open", map[string]any{"section": section})
		q.until(t, "the page again", func() bool { return p.visible() && p.shown.Section == section })
	}
	shellPath := "/bin/sh"
	if runtime.GOOS == "windows" { // a shell the engine finds there
		shellPath = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}

	entry("terminalShell").SetText(shellPath)
	test.Tap(p.closeB)
	q.until(t, "the shell set by the ×", func() bool { return !p.visible() && prefs().TerminalShell == shellPath })

	reopen(engine.SectionTerminal)
	entry("terminalShellArgs").SetText("-l")
	pressEscape(w)
	q.until(t, "the arguments set by Escape", func() bool { return !p.visible() && slices.Equal(prefs().TerminalShellArgs, []string{"-l"}) })

	reopen(engine.SectionKubernetes)
	entry("kubectlMirror").SetText("https://mirror.example.com/k8s")
	test.TapCanvas(w.Canvas(), fyne.NewPos(5, w.Canvas().Size().Height/2))
	q.until(t, "the mirror set by the shade", func() bool {
		return !p.visible() && prefs().KubectlMirror == "https://mirror.example.com/k8s"
	})

	reopen(engine.SectionTerminal)
	entry("terminalShellArgs").SetText("--norc")
	test.Tap(p.navItems[engine.SectionApp])
	q.until(t, "the arguments set by the section list", func() bool {
		return p.shown.Section == engine.SectionApp && slices.Equal(prefs().TerminalShellArgs, []string{"--norc"})
	})

	reopen(engine.SectionTerminal)
	entry("terminalShell").SetText("/no/such/shell")
	pressEscape(w)
	q.until(t, "the refusal as a toast", func() bool {
		return slices.ContainsFunc(walkToastTexts(sh), func(s string) bool { return strings.Contains(s, "/no/such/shell") })
	})
	if prefs().TerminalShell != shellPath {
		t.Errorf("a refused shell was kept: %q", prefs().TerminalShell)
	}
}

// The window's tooltip layer and toasts lie above the card while it shows, and go back to the
// window's content when it closes: a toast shows over the card, and the ×'s tooltip shows when the
// pointer rests on it.
func TestSettingsOverlayShowsToastsAndTooltipsAboveIt(t *testing.T) {
	_, sh, w, q := overlayScene(t, engine.SectionApp)
	sh.tip.delay = time.Millisecond
	objs := test.WidgetRenderer(sh.settings.overlay).Objects()
	card := slices.Index(objs, fyne.CanvasObject(sh.settings.root))
	if card < 0 || slices.Index(objs, fyne.CanvasObject(sh.toasts)) < card || slices.Index(objs, fyne.CanvasObject(sh.tip.layer)) < card {
		t.Fatal("the toasts or the tooltip layer are not above the card")
	}
	if slices.Contains(sh.root.Objects, fyne.CanvasObject(sh.toasts)) || slices.Contains(sh.root.Objects, fyne.CanvasObject(sh.tip.layer)) {
		t.Fatal("the toasts or the tooltip layer stayed in the window's content as well")
	}
	sh.toast("info", "over the card")
	if !slices.Contains(walkToastTexts(sh), "over the card") {
		t.Errorf("toasts %q", walkToastTexts(sh))
	}

	test.MoveMouse(w.Canvas(), centre(sh.settings.closeB))
	q.until(t, "the ×'s tooltip", func() bool { return sh.tip.visibleText() == "Close (Esc)" })

	pressEscape(w)
	q.until(t, "the page closed", func() bool { return !sh.settings.visible() })
	n := len(sh.root.Objects)
	if n < 2 || sh.root.Objects[n-2] != sh.tip.layer || sh.root.Objects[n-1] != sh.toasts {
		t.Error("the tooltip layer and the toasts are not back on top of the window's content")
	}
}

// The window's own keys act on what lies behind the card and are held while it shows: ⌘F does not
// take the list's search behind it.
func TestSettingsOverlayHoldsTheWindowKeys(t *testing.T) {
	_, sh, w, q := overlayScene(t, engine.SectionApp)
	cmdF(t, sh)
	pressEscape(w)
	q.until(t, "the page closed", func() bool { return !sh.settings.visible() })
	if f := w.Canvas().Focused(); f != nil && f == fyne.Focusable(sh.view.search) {
		t.Error("⌘F under the card took the list's search behind it")
	}
}
