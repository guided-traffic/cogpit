package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// The version beside the wordmark: "v" and a release's version, a v of its own not doubled, no
// tooltip; "dev" for a development build, its commit as buildinfo has it in the tooltip, "-dirty"
// for a tree with changes (F12).
func TestBuildLabelText(t *testing.T) {
	for _, c := range []struct {
		info      buildinfo.Info
		text, tip string
	}{
		{buildinfo.Info{Version: "0.16.0", Release: true, Commit: "0691fb32abcd"}, "v0.16.0", ""},
		{buildinfo.Info{Version: "v0.16.0", Release: true}, "v0.16.0", ""},
		{buildinfo.Info{Version: buildinfo.Dev, Commit: "0691fb32abcd"}, "dev", "dev 0691fb32abcd"},
		{buildinfo.Info{Version: buildinfo.Dev, Commit: "0691fb32abcd", Modified: true}, "dev", "dev 0691fb32abcd-dirty"},
		{buildinfo.Info{Version: buildinfo.Dev}, "dev", "Development build"},
	} {
		if text, tip := buildLabelText(c.info); text != c.text || tip != c.tip {
			t.Errorf("%+v: %q, %q; want %q, %q", c.info, text, tip, c.text, c.tip)
		}
	}
}

// The label sits right of the wordmark in its style (the same size, bold, the foreground colour),
// the history arrows a gap of the theme's inner padding beyond it; a development build's label names its commit in the tooltip.
func TestBuildLabelBesideTheWordmark(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	settle(w)
	v := sh.top.version
	var wordmark *textLine
	for _, l := range findAll[*textLine](sh.top.left) {
		if l.text == "Cogpit" {
			wordmark = l
		}
	}
	if wordmark == nil {
		t.Fatal("no wordmark")
	}
	// The wordmark's colour and weight, about 30% smaller (11 against 16), on its baseline.
	th := fyne.CurrentApp().Settings().Theme()
	if v.style != wordmark.style || v.color != wordmark.color || v.size != theme.SizeNameCaptionText ||
		wordmark.size != theme.SizeNameSubHeadingText || th.Size(v.size) > 0.72*th.Size(wordmark.size) {
		t.Errorf("the label's style %v %+v %v, the wordmark's %v %+v %v", v.size, v.style, v.color, wordmark.size, wordmark.style, wordmark.color)
	}
	if v.text != "dev" { // a test binary is a development build
		t.Errorf("a test build's label %q", v.text)
	}

	pad := padding()
	inner := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	_, wordEnd := absRect(wordmark)
	at, vEnd := absRect(v)
	near(t, "the label right of the wordmark", at.X, wordEnd.X+pad)
	if !sh.top.history.shown() {
		t.Fatal("the arrows are hidden on the real engine")
	}
	arrows, _ := absRect(sh.top.history.root)
	near(t, "the arrows a gap right of the label", arrows.X, vEnd.X+pad+inner+pad)
	near(t, "the label's baseline on the wordmark's", baselineOf(v), baselineOf(wordmark))

	v.set(buildinfo.Info{Version: buildinfo.Dev, Commit: "0691fb32abcd", Modified: true})
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(v))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == "dev 0691fb32abcd-dirty" })

	v.set(buildinfo.Info{Version: "0.16.0", Release: true})
	if v.text != "v0.16.0" || v.tip != "" {
		t.Errorf("a release's label %q, tooltip %q", v.text, v.tip)
	}
}

// baselineOf is the absolute height of a text line's baseline: its canvas text's top and the
// font's baseline.
func baselineOf(l fyne.Widget) float32 {
	var txt *canvas.Text
	for _, o := range test.WidgetRenderer(l).Objects() {
		if c, ok := o.(*canvas.Text); ok {
			txt = c
		}
	}
	_, base := fyne.CurrentApp().Driver().RenderedTextSize("Mg", txt.TextSize, txt.TextStyle, nil)
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(txt).Y + base
}
