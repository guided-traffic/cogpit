package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// jumpFieldRoom is the width a logs tab's time field leaves its text, as widget.Entry lays it out
// (Fyne 2.8.1, entryRenderer.Layout): the field's width less the leading icon's inset (the icon and
// the line spacing) and the text's inner padding on both sides.
func jumpFieldRoom(lt *logTab) float32 {
	th := lt.jumpField.Theme()
	icon := th.Size(theme.SizeNameInlineIcon) + th.Size(theme.SizeNameLineSpacing)
	return lt.jumpField.Size().Width - icon - 2*th.Size(theme.SizeNameInnerPadding)
}

// TestLogJumpFieldShowsItsPlaceholderWhole (#169, a defect the owner reported on 2026-10-14
// evening): at a window width of 1440 points the time field's placeholder, which names the forms
// it takes, is shown whole, in ISO 8601's format and in Germany's, the longest Cogpit writes; so
// is a time a jump writes into the field.
func TestLogJumpFieldShowsItsPlaceholderWhole(t *testing.T) {
	for name, setup := range map[string]func(*fakeEngine){"iso": nil, "de": inGermany} {
		sh, w, q, _ := logsSceneWith(t, ThemeDark, true, engine.LogOptions{ResourceNames: true, Timestamps: true}, setup)
		lt := logTabOf(sh)
		q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
		settle(w)
		th := lt.jumpField.Theme()
		size := th.Size(theme.SizeNameText)
		for _, text := range []string{lt.jumpField.PlaceHolder, sh.timeFormat().Format(fixedNow)} {
			need := fyne.MeasureText(text, size, fyne.TextStyle{}).Width
			if room := jumpFieldRoom(lt); need > room {
				t.Errorf("%s: %q needs %.0f points, the field leaves it %.0f (field %.0f wide)", name, text, need, room, lt.jumpField.Size().Width)
			}
		}
	}
}

// TestRenderLogsJumpField: a deployment's tab before any jump, the time field showing its
// placeholder, in Germany's format, dark and light (#169).
func TestRenderLogsJumpField(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		sh, w, q, _ := logsSceneWith(t, mode, true, engine.LogOptions{ResourceNames: true, Timestamps: true}, inGermany)
		lt := logTabOf(sh)
		q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
		savePNG(t, w, "logs_jump_field_"+string(mode)+".png")
	}
}
