package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// A toast's long reference wraps inside the toast (the owner's QW13: it ran past the right edge):
// it breaks after its / - . _ where it has no space, every line within the width, and a selection
// across the break copies the reference whole, without a space put in where it broke.
func TestToastWrapsALongReference(t *testing.T) {
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	ref := "portforwards/acme-qw/pod-acme-podinfo-6dd6695777-fpcmx-9898"
	msg := ref + ": an entry of Port Forwarding is not an object of the cluster's API: does not apply"
	p := newParagraph(msg, colorToastText, 220)
	p.names = true
	s := newSelParagraph(p, colorToastSelection, nil)
	size := a.Settings().Theme().Size(theme.SizeNameText)
	if len(s.lines) < 3 {
		t.Fatalf("%d lines", len(s.lines))
	}
	for _, l := range s.lines {
		if w := fyne.MeasureText(l.text, size, fyne.TextStyle{}).Width; w > 220 {
			t.Errorf("line %q is %v wide, over 220", l.text, w)
		}
	}
	if !strings.HasPrefix(s.shown, ref+":") || s.shown != msg {
		t.Errorf("shown %q, want the message as it came", s.shown)
	}
	s.TypedShortcut(&fyne.ShortcutSelectAll{})
	if got := s.SelectedText(); got != msg {
		t.Errorf("selected %q", got)
	}
}
