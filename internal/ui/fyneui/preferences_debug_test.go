package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestDebugImagePreferenceField: Preferences › Kubernetes › Debug container image (D-102) shows
// Cogpit's default as its placeholder and says it is pinned by digest; an image typed there is
// kept through the engine, the line under it says a floating tag is one; a refused value shows
// under the field; the field follows a reset made elsewhere.
func TestDebugImagePreferenceField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls[debugImageSetting] != nil })
	p := sh.settings
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }
	entry := findAll[*widget.Entry](p.controls[debugImageSetting])[0]
	note := p.controls["debugImageNote"].(*textLine)
	if entry.PlaceHolder != engine.DefaultDebugImage || entry.Text != "" || note.text != debugNotePinned {
		t.Fatalf("placeholder %q, text %q, note %q", entry.PlaceHolder, entry.Text, note.text)
	}

	entry.SetText("registry.example/tools:1")
	q.until(t, "the image kept", func() bool { return prefs().DebugImage == "registry.example/tools:1" })
	q.until(t, "the note", func() bool { return note.text == debugNoteFloating })

	entry.SetText("registry.example/tools 1")
	q.until(t, "the refusal", func() bool { return p.errs[debugImageSetting].Visible() })
	if prefs().DebugImage != "registry.example/tools:1" {
		t.Errorf("a refused image was kept: %q", prefs().DebugImage)
	}
	entry.SetText("registry.example/tools:1")
	q.until(t, "the refusal cleared", func() bool { return !p.errs[debugImageSetting].Visible() })

	mustDispatch(t, s, "preferences.set", map[string]any{"debugImage": ""}) // a reset, as an agent may make
	q.until(t, "the field follows", func() bool { return entry.Text == "" && note.text == debugNotePinned })
}
