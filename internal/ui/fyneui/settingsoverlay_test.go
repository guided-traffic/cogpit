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
	q.until(t, "the cluster's card", func() bool { return sh.settings.visible() && sh.settings.title.text == "Cluster Settings: staging" })
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
	c.Focus(sh.settings.controls["terminalShell"].(fyne.Focusable))
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
		return entryOf(p.controls[name])
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

// escapeInFocus sends Escape where the driver sends it: to the widget that has the keyboard, else
// to the window's handler.
func escapeInFocus(w fyne.Window) {
	if f := w.Canvas().Focused(); f != nil {
		f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
		return
	}
	pressEscape(w)
}

// typeIntoSetting focuses a settings page's text field, as a click does, and types text into it.
func typeIntoSetting(t *testing.T, w fyne.Window, q *mainQueue, p *settingsPage, name, text string) {
	t.Helper()
	q.until(t, name, func() bool { return p.controls[name] != nil })
	f, ok := p.controls[name].(fyne.Focusable)
	if !ok {
		t.Fatalf("%s is a %T, no field", name, p.controls[name])
	}
	w.Canvas().Focus(f)
	entryOf(p.controls[name]).SetText("")
	test.Type(f, text)
}

// TestSettingsEscapeInATextFieldClosesTheCard (the owner's decision (a) of 2026-10-23 after VW6,
// D-116, D-118): Escape in a text field of Preferences or Cluster Settings, the keyboard in the
// field, closes the card, and the value typed is set (D-118's flushTyping), a multi-line field and a
// field with a list (the time zone) too; before, the field kept Escape and nothing happened.
func TestSettingsEscapeInATextFieldClosesTheCard(t *testing.T) {
	s, sh, w, q := overlayScene(t, engine.SectionTerminal)
	p := sh.settings
	p.typing = time.Hour // the pause never ends by itself here: only the close sets the value
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }
	reopen := func(cmd string, args map[string]any) {
		mustDispatch(t, s, cmd, args)
		q.until(t, "the page again", func() bool { return p.visible() && w.Canvas().Overlays().Top() == p.overlay })
	}
	shellPath := "/bin/sh"
	if runtime.GOOS == "windows" { // a shell the engine finds there
		shellPath = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}

	typeIntoSetting(t, w, q, p, "terminalShell", shellPath)
	escapeInFocus(w)
	q.until(t, "Preferences closed, the shell set", func() bool { return !p.visible() && prefs().TerminalShell == shellPath })

	reopen("preferences.open", map[string]any{"section": engine.SectionTerminal})
	typeIntoSetting(t, w, q, p, "terminalShellArgs", "-l")
	escapeInFocus(w)
	q.until(t, "closed, the arguments set", func() bool { return !p.visible() && slices.Equal(prefs().TerminalShellArgs, []string{"-l"}) })

	reopen("preferences.open", map[string]any{"section": engine.SectionApp})
	typeIntoSetting(t, w, q, p, "timezone", "Europe/Berlin")
	escapeInFocus(w)
	q.until(t, "closed, the time zone set", func() bool { return !p.visible() && prefs().Timezone == "Europe/Berlin" })

	reopen("cluster.settings.open", map[string]any{"context": "kind-dev"})
	typeIntoSetting(t, w, q, p, "name", "Dev cluster")
	escapeInFocus(w)
	q.until(t, "Cluster Settings closed, the name set", func() bool {
		cs, err := s.ClusterSettingsOf("kind-dev")
		return !p.visible() && err == nil && cs.Name == "Dev cluster"
	})
	if w.Canvas().Overlays().Top() != nil {
		t.Errorf("an overlay is left: %T", w.Canvas().Overlays().Top())
	}
}

// TestSettingsEscapeInTheAgentFormClosesTheForm (D-116's innermost thing, the lead's detail of
// 2026-10-24): Escape in a field of Preferences › Agent's connection form closes the form, as its
// Cancel does, and leaves the card open; the next Escape, the keyboard nowhere, closes the card.
func TestSettingsEscapeInTheAgentFormClosesTheForm(t *testing.T) {
	sh, w, q, ch := agentPrefsShell(t, ThemeDark)
	a := sh.settings.agent
	for _, field := range []func(f *agentForm) fyne.Focusable{
		func(f *agentForm) fyne.Focusable { return f.name },
		func(f *agentForm) fyne.Focusable { return f.key },
		func(f *agentForm) fyne.Focusable { return f.model },
		func(f *agentForm) fyne.Focusable { return f.limits[0] },
	} {
		test.Tap(a.addB)
		f := a.form
		if f == nil {
			t.Fatal("Add opened no form")
		}
		w.Canvas().Focus(field(f))
		test.Type(field(f), "x")
		escapeInFocus(w)
		if a.form != nil || !sh.settings.visible() {
			t.Fatalf("Escape in %T: the form open %v, the card open %v; want the form closed, the card open", field(f), a.form != nil, sh.settings.visible())
		}
		if w.Canvas().Focused() != nil {
			t.Errorf("the keyboard stayed on the closed form's %T", w.Canvas().Focused())
		}
	}
	if n := len(ch.savesOf()); n != 0 {
		t.Errorf("Escape saved %d connections", n)
	}
	escapeInFocus(w)
	q.until(t, "the card closed", func() bool { return !sh.settings.visible() })
}
