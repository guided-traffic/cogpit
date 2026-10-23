package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// keyLineParts names what the end of a Secret key's name line holds, in order: a text button's
// word, "copy" for the copy icon, "remove" for the remove button. nil when the drawer has no name
// line for key.
func keyLineParts(d *drawer, key string) []string {
	line := keyLineOf(d, key)
	if line == nil {
		return nil
	}
	right, ok := line.Objects[1].(*fyne.Container)
	if !ok { // a binary value's line: the remove button alone
		return []string{"remove"}
	}
	var parts []string
	for _, o := range right.Objects {
		switch x := o.(type) {
		case *textButton:
			parts = append(parts, x.Text)
		case *tipButton:
			parts = append(parts, "remove")
		case *fyne.Container:
			if len(findAll[*copyButton](x)) == 1 {
				parts = append(parts, "copy")
			}
		}
	}
	return parts
}

// keyLineOf is the name line of key in the drawer: the key's name (or the name and its tag) and
// the buttons at its end.
func keyLineOf(d *drawer, key string) *fyne.Container {
	named := func(o fyne.CanvasObject) bool {
		for _, l := range findAll[*selLine](o) {
			if l.line.text == key {
				return true
			}
		}
		return false
	}
	for _, c := range findAll[*fyne.Container](d.details) {
		if len(c.Objects) != 2 {
			continue
		}
		switch c.Objects[1].(type) {
		case *fyne.Container, *tipButton:
		default:
			continue
		}
		switch first := c.Objects[0].(type) {
		case *selLine:
			if first.line.text == key {
				return c
			}
		case *fyne.Container: // an added key: its name and the tag "new"
			if len(first.Objects) == 2 && named(first.Objects[0]) {
				return c
			}
		}
	}
	return nil
}

// keyCopyIcon is the copy icon on key's name line.
func keyCopyIcon(t *testing.T, d *drawer, key string) *copyButton {
	t.Helper()
	line := keyLineOf(d, key)
	if line == nil {
		t.Fatalf("no name line for %s", key)
	}
	icons := findAll[*copyButton](line.Objects[1])
	if len(icons) != 1 {
		t.Fatalf("%d copy icons on %s's line", len(icons), key)
	}
	return icons[0]
}

// TestSecretMaskedKeyButtonsOnNameLine (the owner's RW1 of 2026-10-23): a masked key has its
// buttons where a revealed key has them, "Show [copy] [remove]" at the end of its name line, always
// shown, and the value line under it holds the mask and the size only, without a copy icon of its
// own. The same for an edited value hidden again and an added key hidden again; a binary value has
// no Show and no copy icon.
func TestSecretMaskedKeyButtonsOnNameLine(t *testing.T) {
	sh, _, q, _, _ := secShell(t, ThemeDark)
	d, sd := sh.drawer, secDataOf(sh)
	want := func(key string, parts ...string) {
		t.Helper()
		if got := keyLineParts(d, key); !slices.Equal(got, parts) {
			t.Errorf("%s's name line ends with %v, want %v", key, got, parts)
		}
	}
	for _, k := range []string{"DATABASE_URL", "DB_HOST", "DB_PASSWORD", "DB_USER"} {
		want(k, "Show", "copy", "remove")
		if icon := keyCopyIcon(t, d, k); !icon.Visible() || icon.tip != maskedCopyTip {
			t.Errorf("%s's copy icon: shown %v, tooltip %q", k, icon.Visible(), icon.tip)
		}
	}
	want("ca.crt", "remove")
	for _, l := range maskedLines(d) {
		if l.hook != nil {
			t.Errorf("the masked value %q has a copy icon of its own", l.line.text)
		}
	}
	if n := len(drawerButtons(d)["Show"]); n != 4 {
		t.Errorf("%d Show buttons, want one on each text key's name line", n)
	}

	r := showKey(t, sh, q, "DB_USER")
	want("DB_USER", "Hide", "copy", "remove")
	typeIntoRow(r.cmRow, secTyped)
	sd.hide("DB_USER")
	q.until(t, "the edited value masked", func() bool { return r.masked && drawerHasText(d, "(edited, not saved)") })
	want("DB_USER", "Show", "copy", "remove")
	if l := selLineWith(t, d, engine.MaskedValue+" (edited, not saved)"); l.hook != nil {
		t.Error("the edited masked value has a copy icon of its own")
	}
	if icon := keyCopyIcon(t, d, "DB_USER"); icon.tip != maskedCopyTip {
		t.Errorf("an edited masked value's copy icon says %q", icon.tip)
	}

	addKeyThrough(t, sd.adder, "API_TOKEN")
	q.until(t, "the added key", func() bool { return sd.addedRows["API_TOKEN"] != nil })
	want("API_TOKEN", "Hide", "copy", "remove")
	sd.hide("API_TOKEN")
	q.until(t, "the added key masked", func() bool { return sd.addedRows["API_TOKEN"].masked })
	want("API_TOKEN", "Show", "copy", "remove")
}

// TestSecretReadOnlyKeyButtonsOnNameLine (RW1): where the data cannot be saved, a key's name line
// ends with "Show [copy]", no remove button; Show reveals the value read-only under it and turns
// into Hide in the same place.
func TestSecretReadOnlyKeyButtonsOnNameLine(t *testing.T) {
	f, _ := secretEditFake(t)
	f.disabled[engine.SecretSaveDataCommand] = "not permitted: update secrets in namespace checkout"
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })
	if got := keyLineParts(d, "DB_PASSWORD"); !slices.Equal(got, []string{"Show", "copy"}) {
		t.Fatalf("a read-only key's line ends with %v, want Show, copy", got)
	}
	test.Tap(drawerButtons(d)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return drawerHasText(d, fixtureSecretValues["DB_PASSWORD"]) })
	if got := keyLineParts(d, "DB_PASSWORD"); !slices.Equal(got, []string{"Hide", "copy"}) {
		t.Errorf("a revealed read-only key's line ends with %v, want Hide, copy", got)
	}
	if icon := keyCopyIcon(t, d, "DB_PASSWORD"); icon.tip != wholeCopyTip {
		t.Errorf("a revealed value's copy icon says %q", icon.tip)
	}
}

// TestTextButtonsHaveTheFieldOutline (the owner's RW2 of 2026-10-23, D-104): a button with a word
// and no plate of its own draws the field's outline, the primary colour while it has the keyboard,
// the disabled colour while disabled, its tone where it has one; no plain LowImportance button with
// a word is left in the Secret's drawer.
func TestTextButtonsHaveTheFieldOutline(t *testing.T) {
	sh, w, _, _, _ := secShell(t, ThemeDark)
	d := sh.drawer
	th := sh.app.Settings().Theme()
	v := sh.app.Settings().ThemeVariant()
	outlined := map[*widget.Button]bool{}
	for _, b := range findAll[*textButton](d.details) {
		outlined[&b.Button] = true
	}
	for _, b := range findAll[*widget.Button](d.details) {
		if b.Text != "" && b.Importance == widget.LowImportance && !outlined[b] {
			t.Errorf("the text button %q has no outline", b.Text)
		}
	}

	b := newTextButton("Dismiss", nil, func() {})
	w.SetContent(b)
	r := test.WidgetRenderer(b).(*textButtonRenderer)
	if r.outline.StrokeColor != th.Color(theme.ColorNameInputBorder, v) || r.outline.StrokeWidth <= 0 {
		t.Errorf("outline %v at %v, want the input border", r.outline.StrokeColor, r.outline.StrokeWidth)
	}
	w.Canvas().Focus(b)
	if r.outline.StrokeColor != th.Color(theme.ColorNamePrimary, v) {
		t.Errorf("a focused button's outline is %v, want the primary colour", r.outline.StrokeColor)
	}
	w.Canvas().Unfocus()
	b.Disable()
	if r.outline.StrokeColor != th.Color(theme.ColorNameDisabled, v) {
		t.Errorf("a disabled button's outline is %v, want the disabled colour", r.outline.StrokeColor)
	}
	b.Enable()
	b.setTone(colorStatusWarning)
	if r.outline.StrokeColor != themeColor(th, colorStatusWarning) {
		t.Errorf("a toned button's outline is %v, want its tone", r.outline.StrokeColor)
	}
	for _, rt := range findAll[*widget.RichText](b) {
		if seg := rt.Segments[0].(*widget.TextSegment); seg.Style.ColorName != colorStatusWarning {
			t.Errorf("a toned button's word is in %q", seg.Style.ColorName)
		}
	}
}

// TestSecretShowMarkedAsExposing (the owner's RW3 of 2026-10-23, his pick A): a Secret's Show is
// marked as exposing, its word, outline and eye icon in the status warning's amber; Hide is plain.
func TestSecretShowMarkedAsExposing(t *testing.T) {
	sh, _, q, _, _ := secShell(t, ThemeDark)
	d := sh.drawer
	showKey(t, sh, q, "DB_PASSWORD")
	var shows, hides int
	for _, b := range findAll[*textButton](d.details) {
		switch b.Text {
		case "Show":
			shows++
			if b.tone != colorStatusWarning || b.Icon == nil || b.Icon.Name() != theme.VisibilityIcon().Name() {
				t.Errorf("Show's tone %q, icon %v: want the warning's amber and the eye", b.tone, b.Icon)
			}
		case "Hide":
			hides++
			if b.tone != "" || b.Icon != nil {
				t.Errorf("Hide's tone %q, icon %v: want it plain", b.tone, b.Icon)
			}
		}
	}
	if shows != 3 || hides != 1 {
		t.Errorf("%d Show and %d Hide, want 3 and 1", shows, hides)
	}
}
