package fyneui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// agentTypes is an agent's ui.palette.setText through the fake engine.
func agentTypes(t *testing.T, f *fakeEngine, text string) {
	t.Helper()
	raw, _ := json.Marshal(paletteTextParams{Text: text})
	if _, err := f.Dispatch(context.Background(), engine.Invocation{ID: "ui.palette.setText", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
}

// The palette's typed text is engine state (A54): the user's typing is reported once per pause, as
// the user's; an agent's text shows in the field.
func TestCommandPaletteTextIsEngineState(t *testing.T) {
	sh, _, q, f, _ := paletteShell(t, ThemeDark)
	sh.palette.textRep.delay = time.Hour // a slow runner's pause between two letters is no pause of the user's; fireTimer ends it
	openPalette(t, sh, q)
	typeInPalette(t, sh, q, "swi")
	if !fireTimer(sh.palette.textRep.timer) {
		t.Fatal("no report waiting for the typed text")
	}
	q.until(t, "the text reported", func() bool {
		u := f.UIState()
		return u.PaletteText == "swi" && u.PaletteTextOrigin == engine.OriginGUI
	})
	if n := len(f.callsOf("ui.palette.setText")); n != 1 {
		t.Errorf("%d reports for one word, want one", n)
	}
	q.until(t, "the report done", func() bool { return sh.palette.textRep.busy == 0 })
	agentTypes(t, f, "logs")
	q.until(t, "the agent's text", func() bool { return sh.palette.filter.Text == "logs" && sh.palette.byOther })
	if got := shownLabels(sh.palette); len(got) == 0 || got[0] != "Logs" {
		t.Errorf("entries for the agent's text %q", got)
	}
	pressInPalette(sh, q, fyne.KeyEscape)
	q.until(t, "closed", func() bool { return !sh.ui.Palette })
	if u := f.UIState(); u.PaletteText != "" {
		t.Errorf("closed with the text %q", u.PaletteText)
	}

	// An agent's text opens a closed palette, without the keyboard focus.
	agentTypes(t, f, "switch")
	q.until(t, "opened with the agent's text", func() bool { return sh.palette.visible() && sh.palette.filter.Text == "switch" })
	if sh.win.Canvas().Focused() == sh.palette.filter {
		t.Error("an agent's text took the focus")
	}
}

// An agent's text never chooses what the user's ↵ runs (A54, the lead's night decision): on it ↵
// runs a read or a UI entry, but a change (Delete, destructive) only once the user picked it with
// the arrow keys; the hint says so, in the warning colour after a refused ↵. Typing makes the text
// the user's, and ↵ runs again whatever the entry.
func TestCommandPaletteAgentTextNeverRunsAChange(t *testing.T) {
	sh, _, q, f, _ := paletteShell(t, ThemeDark)
	p := sh.palette
	openPalette(t, sh, q)
	q.until(t, "the open state reported", func() bool { return f.UIState().Palette })
	agentTypes(t, f, "delete")
	q.until(t, "the agent's text", func() bool { return p.filter.Text == "delete" && len(p.shown) > 0 })
	if p.shown[0].Label != "Delete" || p.hint.text == paletteHint {
		t.Fatalf("first entry %q, hint %q", p.shown[0].Label, p.hint.text)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	if !p.visible() || len(f.callsOf("object.delete")) != 0 || p.hint.color != colorStatusWarning {
		t.Fatalf("↵ on an agent's Delete: open %v, %d deletes, hint in %s", p.visible(), len(f.callsOf("object.delete")), p.hint.color)
	}
	pressInPalette(sh, q, fyne.KeyDown)
	pressInPalette(sh, q, fyne.KeyUp) // the user picks Delete
	if p.hint.text != paletteHint {
		t.Errorf("hint after picking %q", p.hint.text)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the confirmation", func() bool { return sh.confirmer.current != nil })
	if n := len(f.callsOf("object.delete")); n != 1 {
		t.Errorf("%d deletes after the user picked it", n)
	}
	test.Tap(sh.confirmer.current.cancel)
	q.until(t, "the dialog gone", func() bool { return sh.confirmer.current == nil })

	// A read on an agent's text runs on ↵.
	openPalette(t, sh, q)
	q.until(t, "the open state", func() bool { return f.UIState().Palette })
	agentTypes(t, f, "logs")
	q.until(t, "the agent's logs", func() bool { return p.filter.Text == "logs" && len(p.shown) > 0 && p.shown[0].Label == "Logs" })
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the logs", func() bool { return len(f.callsOf("pod.logs")) == 1 && !p.visible() })

	// The user's typing after the agent's makes the text the user's.
	openPalette(t, sh, q)
	q.until(t, "the open state again", func() bool { return f.UIState().Palette })
	agentTypes(t, f, "delet")
	q.until(t, "the agent's delet", func() bool { return p.filter.Text == "delet" && p.byOther })
	sh.win.Canvas().Focus(p.filter)
	typeInPalette(t, sh, q, "e")
	if p.byOther || p.hint.text != paletteHint {
		t.Fatalf("after typing: by another %v, hint %q", p.byOther, p.hint.text)
	}
	pressInPalette(sh, q, fyne.KeyReturn)
	q.until(t, "the confirmation of the user's Delete", func() bool { return sh.confirmer.current != nil })
	test.Tap(sh.confirmer.current.cancel)
	q.until(t, "the dialog gone again", func() bool { return sh.confirmer.current == nil })
}

func TestRenderCommandPaletteAgentTextDark(t *testing.T) {
	renderPaletteAgentText(t, ThemeDark, "command_palette_agent_dark.png")
}

// renderPaletteAgentText: the palette with an agent's "delete" and the user's refused ↵: the hint
// in the warning colour says how to run the change.
func renderPaletteAgentText(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, f, _ := paletteShell(t, mode)
	openPalette(t, sh, q)
	q.until(t, "the open state", func() bool { return f.UIState().Palette })
	agentTypes(t, f, "delete")
	q.until(t, "the agent's text", func() bool { return sh.palette.filter.Text == "delete" && len(sh.palette.shown) > 0 })
	pressInPalette(sh, q, fyne.KeyReturn)
	savePNG(t, w, name)
}
