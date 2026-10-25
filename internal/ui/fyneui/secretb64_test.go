package fyneui

import (
	"encoding/base64"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The b64 button beside a Secret value's copy icon (the owner's VW7 of 2026-10-24; D-25): it copies
// the value base64-encoded, as the copy icon beside it copies it plain, a binary value's too.

func enc64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// keyB64 is the b64 button on key's name line: a pinned copy icon with the b64 icon (VW19).
func keyB64(t *testing.T, d *drawer, key string) *copyButton {
	t.Helper()
	line := keyLineOf(d, key)
	if line == nil {
		t.Fatalf("no name line for %s", key)
	}
	var out []*copyButton
	for _, b := range findAll[*copyButton](line.Objects[1]) {
		if b.tip == b64Tip {
			out = append(out, b)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%d b64 buttons on %s's line", len(out), key)
	}
	return out[0]
}

// TestSecretB64CopiesAMaskedValueEncoded: a masked value's b64 button reads the whole value
// base64-encoded from the GUI (a value longer than a reveal shows comes whole) and puts it on the
// clipboard for Secret values, concealed, without showing it; the drawer stays masked, the icon
// turns into the checkmark and back (VW19), and the toast names the key and says "as base64", never
// the value or its encoding. The clipboard is cleared after the TTL.
func TestSecretB64CopiesAMaskedValueEncoded(t *testing.T) {
	f := wholeRevealFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond
	sh.copyConfirm = 60 * time.Millisecond
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })

	b := keyB64(t, d, "DB_PASSWORD")
	if got, want := drawnIcon(b), themedIcon(iconBase64, theme.ColorNameForeground).Name(); got != want || b.tip != "Copy the value as base64" {
		t.Errorf("the button draws %q, its tooltip %q; want %q", got, b.tip, want)
	}
	test.Tap(b)
	q.until(t, "the copy", func() bool { return len(*concealed) == 1 })
	if got := (*concealed)[0]; got != enc64(longSecretValue) {
		t.Errorf("copied %d bytes, want the whole value's base64 (%d)", len(got), len(enc64(longSecretValue)))
	}
	calls := f.callsOf(engine.RevealCommand)
	if len(calls) != 1 || calls[0].Origin != engine.OriginGUI || !strings.Contains(string(calls[0].Params), `"key":"DB_PASSWORD"`) ||
		!strings.Contains(string(calls[0].Params), `"base64":true`) {
		t.Errorf("reveal calls %+v, want one from the GUI for DB_PASSWORD with base64", calls)
	}
	if len(d.revealed) != 0 || len(findAll[*secretText](d.details)) != 0 || drawerHasText(d, "correct-horse") {
		t.Error("the copy showed the value")
	}
	if n := len(maskedLines(d)); n != 4 {
		t.Errorf("%d masked values after the copy, want 4", n)
	}
	b = keyB64(t, d, "DB_PASSWORD")
	if got := drawnIcon(b); !b.done || got != themedIcon(theme.ConfirmIcon(), colorStatusOK).Name() {
		t.Errorf("after the copy the button draws %q, want the checkmark", got)
	}
	if !toastSays(sh, "DB_PASSWORD as base64") || !toastSays(sh, "cleared in") || toastSays(sh, "correct-horse") ||
		toastSays(sh, enc64(longSecretValue)[:16]) {
		t.Error("the toast should name the key, say as base64 and that the clipboard is cleared, never show the value")
	}
	q.until(t, "the icon again", func() bool {
		b := keyB64(t, d, "DB_PASSWORD")
		return !b.done && drawnIcon(b) == themedIcon(iconBase64, theme.ColorNameForeground).Name()
	})
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })
}

// drawnIcon names the icon a copy icon draws: the copy icon, the b64 icon or the checkmark.
func drawnIcon(b *copyButton) string {
	return test.WidgetRenderer(b).(*copyButtonRenderer).img.Resource.Name()
}

// TestSecretB64OnABinaryValue (the owner's pick (b) of 2026-10-24): a binary value has the b64
// button and nothing else to copy it, no Show and no copy icon; the button copies its bytes
// base64-encoded through the binary value's own base64 read, and the drawer still shows it by its
// size, without a button of the model's action.
func TestSecretB64OnABinaryValue(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })

	if got := keyLineParts(d, "ca.crt"); !slices.Contains(got, "b64") || slices.Contains(got, "Show") || slices.Contains(got, "copy") {
		t.Fatalf("the binary value's line ends with %v, want b64 and no Show or copy", got)
	}
	if len(drawerButtons(d)[engine.Base64ActionLabel]) != 0 {
		t.Error("the model's base64 action is drawn as a button of its own")
	}
	test.Tap(keyB64(t, d, "ca.crt"))
	q.until(t, "the copy", func() bool { return len(*concealed) == 1 })
	if got, want := (*concealed)[0], base64.StdEncoding.EncodeToString(fixtureSecretBinary()); got != want {
		t.Errorf("copied %q…, want the binary value's base64", got[:min(len(got), 16)])
	}
	calls := f.callsOf(engine.RevealCommand)
	if len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"key":"ca.crt"`) || !strings.Contains(string(calls[0].Params), `"base64":true`) ||
		!strings.Contains(string(calls[0].Params), `"resourceVersion":"48210"`) {
		t.Errorf("reveal calls %+v, want the binary value's base64 read at the listed version", calls)
	}
	if !drawerHasText(d, "binary data, 1.2 KiB") {
		t.Error("the binary value is no longer shown by its size")
	}
	if !toastSays(sh, "ca.crt as base64") {
		t.Error("the toast does not name the key")
	}
}

// TestSecretB64OfAValueInAnEditor: beside an editor's copy icon the b64 button copies the value as
// typed, base64-encoded, from the editor's memory with no reveal, shown or masked again, an added
// key's too; an empty value copies nothing and says so.
func TestSecretB64OfAValueInAnEditor(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	concealed := concealedClipboard(sh)
	d, sd := sh.drawer, secDataOf(sh)

	r := showKey(t, sh, q, "DB_USER")
	typeIntoRow(r.cmRow, secTyped)
	reveals := len(f.callsOf(engine.RevealCommand))
	test.Tap(keyB64(t, d, "DB_USER"))
	if len(*concealed) != 1 || (*concealed)[0] != enc64(r.view.Text()) {
		t.Fatalf("copied %q, want the typed value's base64", *concealed)
	}
	sd.hide("DB_USER")
	q.until(t, "the edited value masked", func() bool { return r.masked && drawerHasText(d, "(edited, not saved)") })
	test.Tap(keyB64(t, d, "DB_USER"))
	if len(*concealed) != 2 || (*concealed)[1] != enc64(r.view.Text()) {
		t.Errorf("a masked edited value copied %q, want the typed value's base64", (*concealed)[1:])
	}
	if n := len(f.callsOf(engine.RevealCommand)); n != reveals {
		t.Errorf("the b64 button of an editor read the value again (%d reveals, want %d)", n, reveals)
	}

	addKeyThrough(t, sd.adder, "API_TOKEN")
	q.until(t, "the added key", func() bool { return sd.addedRows["API_TOKEN"] != nil })
	sh.app.Clipboard().SetContent("the user's own")
	test.Tap(keyB64(t, d, "API_TOKEN"))
	if len(*concealed) != 2 || sh.app.Clipboard().Content() != "the user's own" || !toastSays(sh, "empty") {
		t.Error("an empty value's b64 copy changed the clipboard or did not say so")
	}
	typeIntoRow(sd.addedRows["API_TOKEN"].cmRow, "tok-1")
	test.Tap(keyB64(t, d, "API_TOKEN"))
	if len(*concealed) != 3 || (*concealed)[2] != enc64(sd.addedRows["API_TOKEN"].view.Text()) {
		t.Errorf("an added key copied %q, want its typed value's base64", (*concealed)[2:])
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("a value reached the window outside the editors: %q", l)
	}
}

// TestSecretB64Refusals: an answer that is not the value base64-encoded (an engine that ignored
// base64) copies nothing and says so, never the plain value; an answer for a drawer that moved on
// copies nothing and says nothing.
func TestSecretB64Refusals(t *testing.T) {
	f := wholeRevealFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })
	ref := *d.shown
	h := func() {}

	d.encodedRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Value: longSecretValue, Size: len(longSecretValue)}, nil)
	if len(*concealed) != 0 || !toastSays(sh, "not read whole") || toastSays(sh, "correct-horse") {
		t.Error("a plain answer to a base64 read was copied, or the toast did not say so")
	}
	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return d.shown != nil && *d.shown == other })
	toasts := len(sh.toasts.Objects)
	d.encodedRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Value: enc64(longSecretValue), Size: len(longSecretValue), Base64: true}, nil)
	if len(*concealed) != 0 || len(sh.toasts.Objects) != toasts {
		t.Error("an answer for the drawer that moved on was copied or shown")
	}
}

// TestSecretB64ButtonLook (D-104; the owner's VW19 of 2026-10-25, his pick O1): the b64 button is an
// icon made as the copy icon beside it: outlined as it is, as wide and as high, and no wider than
// the remove button; with the checkmark in its icon's place it keeps its size (his VW18: the word's
// button grew after it). A binary value's line holds it and the remove button only, the same size.
func TestSecretB64ButtonLook(t *testing.T) {
	sh, w, q, _, _ := secShell(t, ThemeDark)
	settle(w)
	d := sh.drawer
	b := keyB64(t, d, "DB_PASSWORD")
	icon := keyCopyIcon(t, d, "DB_PASSWORD")
	r, ir := test.WidgetRenderer(b).(*copyButtonRenderer), test.WidgetRenderer(icon).(*copyButtonRenderer)
	th, v := sh.app.Settings().Theme(), sh.app.Settings().ThemeVariant()
	if r.bg.StrokeColor != th.Color(theme.ColorNameInputBorder, v) || r.bg.FillColor != color.Transparent ||
		r.bg.CornerRadius != ir.bg.CornerRadius || r.bg.StrokeWidth != ir.bg.StrokeWidth {
		t.Errorf("the b64 button's plate %v, outline %v: want the copy icon's", r.bg.FillColor, r.bg.StrokeColor)
	}
	size := b.MinSize()
	if size != icon.MinSize() || b.Size() != icon.Size() {
		t.Errorf("the b64 button is %v (laid out %v), the copy icon %v (%v)", size, b.Size(), icon.MinSize(), icon.Size())
	}
	remove := findAll[*tipButton](keyLineOf(d, "DB_PASSWORD").Objects[1])
	if len(remove) != 1 || size.Width > remove[0].MinSize().Width {
		t.Errorf("the b64 button is %v wide, the remove buttons %v", size.Width, remove)
	}
	sh.copyConfirm = 20 * time.Millisecond
	secDataOf(sh).b64Of("DB_PASSWORD").confirm()
	settle(w)
	if got := b.MinSize(); !b.done || got != size || b.Size() != icon.Size() {
		t.Errorf("with the checkmark (%v) the button is %v (laid out %v), without %v", b.done, got, b.Size(), size)
	}
	q.until(t, "the icon again", func() bool { return !b.done })
	settle(w)
	if got := b.MinSize(); got != size || b.Size() != icon.Size() {
		t.Errorf("after the checkmark the button is %v (laid out %v), before %v", got, b.Size(), size)
	}

	if got := keyLineParts(d, "ca.crt"); !slices.Equal(got, []string{"b64", "remove"}) {
		t.Errorf("the binary value's line ends with %v, want b64 and remove", got)
	}
	if bin := keyB64(t, d, "ca.crt"); bin.MinSize() != size {
		t.Errorf("the binary value's b64 button is %v, the other keys' %v", bin.MinSize(), size)
	}
}
