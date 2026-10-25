package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestASettingsSectionStartsAtItsTop (the owner's finding of 2026-10-25): a section shown after
// another that was scrolled down starts at its top, as does the page opened again; before, the
// scroller kept the old offset and a shorter section drew nothing until a scroll.
func TestASettingsSectionStartsAtItsTop(t *testing.T) {
	s, _ := settingsEngine(t)
	ch := newFakeChat()
	ch.notify = s.NotifyChat
	ch.conns = []engine.ChatConnection{lmStudio(), litellm()}
	s.SetChatAgent(ch)
	sh, w, q := newEngineShell(t, s, Options{})
	w.Resize(fyne.NewSize(1100, 520)) // short enough that the agent's form scrolls
	mustDispatch(t, s, "preferences.open", map[string]any{"section": engine.SectionAgent})
	q.until(t, "Preferences › Agent", func() bool { return sh.settings.agent != nil && sh.settings.agent.loaded })
	sh.settings.agent.edit(litellm()) // the form: the section far taller than the card
	settle(w)
	p := sh.settings
	p.scroll.ScrollToBottom()
	if p.scroll.Offset.Y == 0 {
		t.Fatal("the agent's section did not scroll")
	}
	mustDispatch(t, s, "preferences.open", map[string]any{"section": engine.SectionApp})
	q.until(t, "Preferences › App", func() bool { return p.shown.Section == engine.SectionApp })
	settle(w)
	if y := p.scroll.Offset.Y; y != 0 {
		t.Errorf("App shown at offset %v, want its top", y)
	}

	mustDispatch(t, s, "preferences.open", map[string]any{"section": engine.SectionAgent})
	q.until(t, "Preferences › Agent again", func() bool { return p.shown.Section == engine.SectionAgent && sh.settings.agent != nil })
	sh.settings.agent.edit(litellm())
	settle(w)
	p.scroll.ScrollToBottom()
	sh.settings.close()
	q.until(t, "the card closed", func() bool { return !p.visible() })
	mustDispatch(t, s, "preferences.open", map[string]any{"section": engine.SectionAgent})
	q.until(t, "the card open again", func() bool { return p.visible() })
	settle(w)
	if y := p.scroll.Offset.Y; y != 0 {
		t.Errorf("opened again at offset %v, want its top", y)
	}
}
