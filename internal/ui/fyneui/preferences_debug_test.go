package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestDebugImagePreferenceField: Preferences › Kubernetes › Debug container image (D-102) shows
// Cogpit's default by its short name as its placeholder (R30) and says it is pinned by digest; an
// image typed there is kept through the engine, the line under it says a floating tag is one; a
// refused value shows under the field; the field follows a reset made elsewhere.
func TestDebugImagePreferenceField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls[debugImageSetting] != nil })
	p := sh.settings
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }
	entry := entryOf(findAll[*fieldEntry](p.controls[debugImageSetting])[0])
	note := p.controls["debugImageNote"].(*textLine)
	if entry.PlaceHolder != engine.DefaultDebugImageName || entry.Text != "" || note.text != debugNotePinned {
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

// TestDebugImagePreferenceShortName (the owner's R30, 2026-10-20): a pinned image set shows by its
// short name and is said to be pinned; the short name left as it is keeps the digest, the default's
// short name typed sets the default (empty), and a changed name is kept as typed, floating.
func TestDebugImagePreferenceShortName(t *testing.T) {
	const pinned = "registry.example.com/tools/netshoot:v1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	mustDispatch(t, s, "preferences.set", map[string]any{"debugImage": pinned})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls[debugImageSetting] != nil })
	p := sh.settings
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }
	entry := findAll[*widget.Entry](p.controls[debugImageSetting])[0]
	note := p.controls["debugImageNote"].(*textLine)
	q.until(t, "the short name", func() bool {
		return entry.Text == "registry.example.com/tools/netshoot:v1" && note.text == debugNotePinned
	})

	w.Canvas().Focus(entry)
	entry.SetText("registry.example.com/tools/netshoot:v1 ")
	entry.SetText("registry.example.com/tools/netshoot:v1")
	q.until(t, "the pause", func() bool { return p.timers[debugImageSetting] == nil })
	q.drain()
	if got := prefs().DebugImage; got != pinned {
		t.Errorf("the short name left as it is set %q, want the pinned %q", got, pinned)
	}

	entry.SetText(engine.DefaultDebugImageName)
	q.until(t, "the default", func() bool { return prefs().DebugImage == "" })

	entry.SetText("registry.example.com/tools/netshoot:v2")
	q.until(t, "the typed image", func() bool { return prefs().DebugImage == "registry.example.com/tools/netshoot:v2" })
	w.Canvas().Unfocus()
	q.until(t, "the floating note", func() bool { return note.text == debugNoteFloating })
}

// TestImageSetting: what an image field's text sets (R30): the set image unchanged while the text is
// its short name, empty for the default, its short name or an empty field, else the text as typed.
func TestImageSetting(t *testing.T) {
	const pinned = "registry.example.com/tools/netshoot:v1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	def := engine.DefaultDebugImage
	for _, tc := range []struct{ text, set, want string }{
		{"registry.example.com/tools/netshoot:v1", pinned, pinned},
		{" registry.example.com/tools/netshoot:v1 ", pinned, pinned},
		{engine.DefaultDebugImageName, pinned, ""},
		{engine.DefaultDebugImageName, "", ""},
		{"", pinned, ""},
		{def, "", ""},
		{engine.DefaultDebugImageName, def, def}, // the default kept whole stays as it was
		{"busybox:1.37", pinned, "busybox:1.37"},
		{"nicolaka/netshoot:v0.17", "", "nicolaka/netshoot:v0.17"},
	} {
		if got := imageSetting(tc.text, tc.set, def); got != tc.want {
			t.Errorf("imageSetting(%q, %q) = %q, want %q", tc.text, tc.set, got, tc.want)
		}
	}
}
