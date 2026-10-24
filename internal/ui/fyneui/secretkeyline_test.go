package fyneui

import (
	"image/color"
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
			if x.tip == b64Tip {
				parts = append(parts, "b64")
			} else {
				parts = append(parts, "remove")
			}
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
// buttons where a revealed key has them, "Show [copy] [b64] [remove]" at the end of its name line
// (b64 since VW7), always shown, and the value line under it holds the mask and the size only,
// without a copy icon of its own. The same for an edited value hidden again and an added key hidden
// again; a binary value has no Show and no copy icon, only the b64 button (VW7).
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
		want(k, "Show", "copy", "b64", "remove")
		if icon := keyCopyIcon(t, d, k); !icon.Visible() || icon.tip != maskedCopyTip {
			t.Errorf("%s's copy icon: shown %v, tooltip %q", k, icon.Visible(), icon.tip)
		}
	}
	want("ca.crt", "b64", "remove")
	for _, l := range maskedLines(d) {
		if l.hook != nil {
			t.Errorf("the masked value %q has a copy icon of its own", l.line.text)
		}
	}
	if n := len(drawerButtons(d)["Show"]); n != 4 {
		t.Errorf("%d Show buttons, want one on each text key's name line", n)
	}

	r := showKey(t, sh, q, "DB_USER")
	want("DB_USER", "Hide", "copy", "b64", "remove")
	typeIntoRow(r.cmRow, secTyped)
	sd.hide("DB_USER")
	q.until(t, "the edited value masked", func() bool { return r.masked && drawerHasText(d, "(edited, not saved)") })
	want("DB_USER", "Show", "copy", "b64", "remove")
	if l := selLineWith(t, d, engine.MaskedValue+" (edited, not saved)"); l.hook != nil {
		t.Error("the edited masked value has a copy icon of its own")
	}
	if icon := keyCopyIcon(t, d, "DB_USER"); icon.tip != maskedCopyTip {
		t.Errorf("an edited masked value's copy icon says %q", icon.tip)
	}

	addKeyThrough(t, sd.adder, "API_TOKEN")
	q.until(t, "the added key", func() bool { return sd.addedRows["API_TOKEN"] != nil })
	want("API_TOKEN", "Hide", "copy", "b64", "remove")
	sd.hide("API_TOKEN")
	q.until(t, "the added key masked", func() bool { return sd.addedRows["API_TOKEN"].masked })
	want("API_TOKEN", "Show", "copy", "b64", "remove")
}

// TestSecretReadOnlyKeyButtonsOnNameLine (RW1): where the data cannot be saved, a key's name line
// ends with "Show [copy] [b64]", no remove button; Show reveals the value read-only under it and
// turns into Hide in the same place; a binary value's line ends with its b64 button alone.
func TestSecretReadOnlyKeyButtonsOnNameLine(t *testing.T) {
	f, _ := secretEditFake(t)
	f.disabled[engine.SecretSaveDataCommand] = "not permitted: update secrets in namespace checkout"
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })
	if got := keyLineParts(d, "DB_PASSWORD"); !slices.Equal(got, []string{"Show", "copy", "b64"}) {
		t.Fatalf("a read-only key's line ends with %v, want Show, copy, b64", got)
	}
	if got := keyLineParts(d, "ca.crt"); !slices.Equal(got, []string{"b64"}) {
		t.Errorf("a read-only binary value's line ends with %v, want b64", got)
	}
	test.Tap(drawerButtons(d)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return drawerHasText(d, fixtureSecretValues["DB_PASSWORD"]) })
	if got := keyLineParts(d, "DB_PASSWORD"); !slices.Equal(got, []string{"Hide", "copy", "b64"}) {
		t.Errorf("a revealed read-only key's line ends with %v, want Hide, copy, b64", got)
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
// marked as exposing, its word, outline and eye icon in the status warning's amber; Hide puts the
// value away, the eye struck through in the status OK's green (his CW2, pick C), and is exactly as
// wide as Show (CW1), so that a click where Show's eye was hits Hide.
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
			if b.tone != colorStatusOK || b.Icon == nil || b.Icon.Name() != theme.VisibilityOffIcon().Name() {
				t.Errorf("Hide's tone %q, icon %v: want the status OK's green and the eye struck through", b.tone, b.Icon)
			}
		}
	}
	if shows != 3 || hides != 1 {
		t.Errorf("%d Show and %d Hide, want 3 and 1", shows, hides)
	}
	show, hide := revealButton(showLabel, nil).MinSize().Width, revealButton(hideLabel, nil).MinSize().Width
	if show != hide {
		t.Errorf("Show is %v wide, Hide %v: want them alike", show, hide)
	}
}

// TestKeyLineButtonsOneHeight (the owner's finding of 2026-10-23 and his CW3): the buttons at the
// end of a Secret key's name line are one height, Show, the copy icon, the b64 button (VW7) and the
// remove button, a binary value's b64 and remove button too, and
// Undo on a key marked for removal is as tall as they are; the copy icon is outlined as the remove
// button is (his pick A), not on a chip.
func TestKeyLineButtonsOneHeight(t *testing.T) {
	sh, w, q, _, _ := secShell(t, ThemeDark)
	d := sh.drawer
	showKey(t, sh, q, "DB_PASSWORD")
	settle(w)
	heights := func(key string) map[string]float32 {
		t.Helper()
		line := keyLineOf(d, key)
		if line == nil {
			t.Fatalf("no name line for %s", key)
		}
		out := map[string]float32{}
		for _, b := range findAll[*textButton](line.Objects[1]) {
			out[b.Text] = b.Size().Height
		}
		for _, b := range findAll[*tipButton](line.Objects[1]) {
			if b.tip == b64Tip {
				out["b64"] = b.Size().Height
			} else {
				out["remove"] = b.Size().Height
			}
		}
		for _, b := range findAll[*copyButton](line.Objects[1]) {
			out["copy"] = b.Size().Height
		}
		return out
	}
	alike := func(key string, want ...string) {
		t.Helper()
		got := heights(key)
		for _, k := range want {
			if got[k] == 0 || got[k] != got[want[0]] {
				t.Errorf("%s's line: heights %v, want %q alike", key, got, want)
				return
			}
		}
	}
	alike("DB_USER", "Show", "copy", "b64", "remove")
	alike("DB_PASSWORD", "Hide", "copy", "b64", "remove")
	alike("ca.crt", "b64", "remove")
	button := heights("DB_USER")["Show"]

	icon := keyCopyIcon(t, d, "DB_USER")
	r := test.WidgetRenderer(icon).(*copyButtonRenderer)
	th, v := sh.app.Settings().Theme(), sh.app.Settings().ThemeVariant()
	if r.bg.FillColor != color.Transparent || r.bg.StrokeColor != th.Color(theme.ColorNameInputBorder, v) {
		t.Errorf("the copy icon's plate %v, stroke %v: want no fill and the field's outline", r.bg.FillColor, r.bg.StrokeColor)
	}

	remove := slices.DeleteFunc(findAll[*tipButton](keyLineOf(d, "DB_USER").Objects[1]), func(b *tipButton) bool { return b.tip == b64Tip })
	if len(remove) != 1 {
		t.Fatalf("%d remove buttons on DB_USER's line", len(remove))
	}
	test.Tap(remove[0])
	settle(w)
	var undo float32
	for _, b := range findAll[*textButton](d.details) {
		if b.Text == "Undo" {
			undo = b.Size().Height
		}
	}
	if undo != button {
		t.Errorf("Undo is %v tall, the line's other buttons %v", undo, button)
	}
}

// TestButtonOutlineAndRoom (the owner's answers of 2026-10-23, D-104): an icon button of a content
// surface is outlined, one of the window's chrome is not; a neutral plate is outlined, a coloured
// one is not; a button with a word keeps textButtonSide beside it on either side, a coloured one
// too (his CW4: "rechts und links … 3-5px", his pick +4), an icon button keeps Fyne's square.
func TestButtonOutlineAndRoom(t *testing.T) {
	sh, w, _, _, _ := secShell(t, ThemeDark)
	th, v := sh.app.Settings().Theme(), sh.app.Settings().ThemeVariant()
	border := th.Color(theme.ColorNameInputBorder, v)
	stroke := func(b fyne.Widget) color.Color {
		w.SetContent(b)
		return test.WidgetRenderer(b).(*textButtonRenderer).outline.StrokeColor
	}
	if c := stroke(newTipButton(sh, theme.DeleteIcon(), "Remove", func() {})); c != border {
		t.Errorf("a content surface's icon button: outline %v, want the field's", c)
	}
	if c := stroke(newBareTipButton(sh, theme.SearchIcon(), "Search", func() {})); c != color.Transparent {
		t.Errorf("a chrome icon button: outline %v, want none", c)
	}
	if c := stroke(newPlateButton("Cancel", nil, func() {})); c != border {
		t.Errorf("a neutral plate: outline %v, want the field's", c)
	}
	save := newPlateButton("Save", nil, func() {})
	save.Importance = widget.HighImportance
	if c := stroke(save); c != color.Transparent {
		t.Errorf("a coloured plate: outline %v, want none", c)
	}

	for _, imp := range []widget.Importance{widget.LowImportance, widget.MediumImportance, widget.HighImportance} {
		fy := widget.NewButton("Edit", nil)
		fy.Importance = imp
		ours := newTextButton("Edit", nil, nil)
		ours.Importance = imp
		if got, want := ours.MinSize().Width, fy.MinSize().Width+2*textButtonSide; got != want {
			t.Errorf("Edit at importance %v: %v wide, want Fyne's %v and the room", imp, got, want)
		}
	}
	icon := newTipButton(sh, theme.DeleteIcon(), "Remove", nil)
	if got, want := icon.MinSize(), widget.NewButtonWithIcon("", theme.DeleteIcon(), nil).MinSize(); got != want {
		t.Errorf("an icon button is %v, want Fyne's %v", got, want)
	}
}
