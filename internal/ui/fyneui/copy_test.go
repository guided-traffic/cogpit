package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Copying values from the drawer (D-25): everything selectable, a copy icon on hover, entries in
// YAML syntax, Secret values masked or revealed (masked ones without showing them, the owner's
// decision of 2026-10-22) and cleared from the clipboard later.

func TestYAMLEntry(t *testing.T) {
	for entry, want := range map[string]string{
		"app.kubernetes.io/name=grafana":  "app.kubernetes.io/name: grafana",
		"team=payments":                   "team: payments",
		"NODE_ENV=":                       `NODE_ENV: ""`,
		"url=https://a.example/x?y=1":     "url: https://a.example/x?y=1",
		"10.244.2.23":                     "10.244.2.23",
		"from secret checkout-db":         "from secret checkout-db",
		"/data from data (ro)":            "/data from data (ro)",
		"has space=value":                 "has space=value", // not a key: a plain scalar
		"enabled=true":                    `enabled: "true"`,
		"note=a: b":                       "note: 'a: b'",
		"node.kubernetes.io/ready:NoExec": "node.kubernetes.io/ready:NoExec",
	} {
		if got := yamlEntry(entry); got != want {
			t.Errorf("yamlEntry(%q) = %q, want %q", entry, got, want)
		}
	}
}

func mouse() *desktop.MouseEvent { return &desktop.MouseEvent{} }

// selLineWith returns the drawer's selectable line that reads text.
func selLineWith(t *testing.T, d *drawer, text string) *selLine {
	t.Helper()
	for _, l := range findAll[*selLine](d.details) {
		if l.line.text == text {
			return l
		}
	}
	t.Fatalf("no selectable line %q in the drawer", text)
	return nil
}

func podDrawerScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "pods")
	p := richPod(t)
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(sh.drawer.details.Objects) > 2 && sh.diag.total > 0 })
	settle(w)
	return sh, w, q, ref
}

// TestDrawerCopyIconOnHover: the copy icon shows when the pointer is over a value, sits by the
// value, hides when the pointer leaves, and a tap puts the value on the clipboard and says so.
func TestDrawerCopyIconOnHover(t *testing.T) {
	sh, _, q, ref := podDrawerScene(t, ThemeDark)
	d := sh.drawer
	if d.copyBtn.Visible() {
		t.Fatal("the copy icon shows before the pointer is over a value")
	}
	name := selLineWith(t, d, ref.Name)
	name.MouseIn(mouse())
	if !d.copyBtn.Visible() {
		t.Fatal("the copy icon did not show over the Name value")
	}
	drv := fyne.CurrentApp().Driver()
	btn, val := drv.AbsolutePositionForObject(d.copyBtn), drv.AbsolutePositionForObject(name)
	if btn.X <= val.X || btn.X > val.X+name.Size().Width || btn.Y < val.Y-d.copyBtn.Size().Height || btn.Y > val.Y+name.Size().Height {
		t.Errorf("the icon is at %v, the value at %v (%v wide)", btn, val, name.Size())
	}

	sh.app.Clipboard().SetContent("")
	toasts := len(sh.toasts.Objects)
	test.Tap(d.copyBtn)
	if got := sh.app.Clipboard().Content(); got != ref.Name {
		t.Errorf("the clipboard holds %q, want %q", got, ref.Name)
	}
	if len(sh.toasts.Objects) != toasts+1 {
		t.Error("copying showed no toast")
	}
	name.MouseOut()
	if d.copyBtn.Visible() {
		t.Error("the copy icon stays after the pointer left the value")
	}
	// Moving from the value onto the icon keeps it: out of the value, then in to the icon.
	name.MouseIn(mouse())
	name.MouseOut()
	d.copyBtn.MouseIn(mouse())
	if !d.copyBtn.Visible() {
		t.Error("the icon vanished under the pointer")
	}
	d.copyBtn.MouseOut()
	if d.copyBtn.Visible() {
		t.Error("the icon stays after the pointer left it")
	}
	_ = q
}

// TestDrawerCopiesEntriesInYAML: a value of several entries copies the entry under the pointer in
// YAML syntax, labels and annotations as key: value; the folded count has no icon.
func TestDrawerCopiesEntriesInYAML(t *testing.T) {
	sh, _, _, _ := podDrawerScene(t, ThemeDark)
	d := sh.drawer
	fold := drawerLinks(sh)["2 Labels"]
	if fold == nil {
		t.Fatalf("no folded labels among %v", keys(drawerLinks(sh)))
	}
	fold.MouseIn(mouse())
	if d.copyBtn.Visible() {
		t.Error("the folded count has a copy icon")
	}
	fold.MouseOut()
	test.Tap(fold)

	var chips []*hoverWrap
	for _, w := range findAll[*hoverWrap](d.details) {
		if p := findAll[*pill](w.content); len(p) == 1 && strings.Contains(p[0].text, "=") {
			chips = append(chips, w)
		}
	}
	if len(chips) != 2 {
		t.Fatalf("%d label chips with a copy icon, want 2", len(chips))
	}
	for _, w := range chips {
		text := findAll[*pill](w.content)[0].text
		k, v, _ := strings.Cut(text, "=")
		w.MouseIn(mouse())
		if !d.copyBtn.Visible() {
			t.Fatalf("no copy icon over the chip %q", text)
		}
		sh.app.Clipboard().SetContent("")
		test.Tap(d.copyBtn)
		if got, want := sh.app.Clipboard().Content(), k+": "+v; got != want {
			t.Errorf("chip %q copied %q, want %q", text, got, want)
		}
		w.MouseOut()
	}

	// Annotations unfold into lines, each with its own icon.
	test.Tap(drawerLinks(sh)["2 Annotations"])
	var ann *selLine
	for _, l := range findAll[*selLine](d.details) {
		if strings.Contains(l.line.text, "=") && l.hook != nil && strings.Contains(l.hook.text, ": ") {
			ann = l
			break
		}
	}
	if ann == nil {
		t.Fatal("no annotation line with a copy icon")
	}
	k, v, _ := strings.Cut(ann.line.text, "=")
	if ann.hook.text != yamlEntry(ann.line.text) || !strings.HasPrefix(ann.hook.text, k+": ") || !strings.Contains(ann.hook.text, strings.Trim(v, `"'`)) {
		t.Errorf("the annotation %q copies %q", ann.line.text, ann.hook.text)
	}

	// A single value is copied as it is, not as YAML.
	// A section's head is a bracket label (stage_header.go), no selectable line: no icon.
	if len(findAll[*sectionHead](d.details)) == 0 || slices.ContainsFunc(findAll[*selLine](d.details), func(l *selLine) bool { return l.line.text == "Properties" }) {
		t.Error("a section title is a selectable line with a copy icon")
	}
}

// TestDrawerTextIsSelectable: a drag selects, a double click selects a word, the copy shortcut
// copies the selection; Tab does not stop at the lines.
func TestDrawerTextIsSelectable(t *testing.T) {
	sh, w, _, ref := podDrawerScene(t, ThemeDark)
	l := selLineWith(t, sh.drawer, ref.Name)
	size := l.Theme().Size(l.line.size)
	at := func(prefix string) fyne.Position {
		return fyne.NewPos(fyne.MeasureText(prefix, size, l.line.style).Width, l.Size().Height/2)
	}
	runes := []rune(ref.Name)

	// A drag from before the third character to after the sixth.
	l.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: at(string(runes[:2]))}, Button: desktop.MouseButtonPrimary})
	// The first event of a drag carries the delta since the press: the start is position - delta.
	from, to := at(string(runes[:2])), at(string(runes[:6]))
	l.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, 0)})
	l.DragEnd()
	if got, want := l.SelectedText(), string(runes[2:6]); got != want {
		t.Fatalf("a drag over characters 2 to 6 selected %q, want %q", got, want)
	}
	sh.app.Clipboard().SetContent("")
	l.TypedShortcut(&fyne.ShortcutCopy{Clipboard: sh.app.Clipboard()})
	if got := sh.app.Clipboard().Content(); got != l.SelectedText() {
		t.Errorf("the copy shortcut put %q on the clipboard, want the selection %q", got, l.SelectedText())
	}

	l.TypedShortcut(&fyne.ShortcutSelectAll{})
	if l.SelectedText() != ref.Name {
		t.Errorf("select all: %q", l.SelectedText())
	}
	l.FocusLost()
	if l.SelectedText() != "" {
		t.Errorf("the selection stays when the focus leaves: %q", l.SelectedText())
	}

	// A double click selects the word: a name has no spaces, so all of it.
	l.DoubleTapped(&fyne.PointEvent{Position: at(string(runes[:3]))})
	if l.SelectedText() != ref.Name {
		t.Errorf("double click selected %q, want %q", l.SelectedText(), ref.Name)
	}

	// Tab skips the lines: the first focusable object is no selectable line.
	l.FocusLost()
	for range 3 {
		w.Canvas().FocusNext()
		switch f := w.Canvas().Focused().(type) {
		case *selLine, *secretText:
			t.Fatalf("Tab stopped at %T", f)
		}
	}
}

// TestDrawerLongNamesSelectUnderTheEllipsis: a value cut off with an ellipsis selects and copies
// whole when the selection reaches the ellipsis.
func TestDrawerLongNamesSelectUnderTheEllipsis(t *testing.T) {
	sh, _, _, _ := podDrawerScene(t, ThemeDark)
	d := sh.drawer
	long := "a-very-long-name-that-cannot-fit-the-drawer-whatever-its-width-is-0123456789-0123456789-0123456789-0123456789-0123456789"
	l := d.line(long, "cogpit.muted", "text", fyne.TextStyle{}, true, long)
	box := test.NewTempWindow(t, l)
	box.Resize(fyne.NewSize(120, 30))
	shown, cut := l.shown()
	if !cut || shown == long {
		t.Fatalf("the line is not cut off: %q", shown)
	}
	l.TypedShortcut(&fyne.ShortcutSelectAll{})
	if l.SelectedText() != long {
		t.Errorf("select all of a cut-off line gave %q", l.SelectedText())
	}
	// A press right of the ellipsis is the end of the text.
	if got := l.offset(fyne.NewPos(l.Size().Width+50, 5)); got != len([]rune(long)) {
		t.Errorf("a press past the ellipsis is offset %d, want the end %d", got, len([]rune(long)))
	}
}

// longSecretValue is a value longer than a reveal shows (4 KiB): a copy of it whole proves that the
// copy asked for the whole value.
var longSecretValue = strings.Repeat("correct-horse-battery-staple-", 200)

// wholeRevealFake is secretsFake with DB_PASSWORD longer than 4 KiB, and secret.reveal answering
// as the engine does: cut to 4 KiB unless the whole value is asked for.
func wholeRevealFake(t *testing.T) *fakeEngine {
	t.Helper()
	f, _ := secretsFake(t)
	f.handlers[engine.RevealCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p engine.RevealParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		if inv.Origin != engine.OriginGUI {
			t.Errorf("secret.reveal dispatched by %s", inv.Origin)
		}
		v := fixtureSecretValues[p.Key]
		if p.Key == "DB_PASSWORD" {
			v = longSecretValue
		}
		if cut := 4 << 10; !p.Whole && len(v) > cut {
			return engine.SecretValue{Key: p.Key, Value: v[:cut], Size: len(v), Cut: true}, nil
		}
		return engine.SecretValue{Key: p.Key, Value: v, Size: len(v)}, nil
	}
	return f
}

// maskedLines are the drawer's masked Secret values, in the drawer's order (the text keys sorted:
// DATABASE_URL, DB_HOST, DB_PASSWORD, DB_USER).
func maskedLines(d *drawer) []*selLine {
	var out []*selLine
	for _, l := range findAll[*selLine](d.details) {
		if strings.HasPrefix(l.line.text, engine.MaskedValue) {
			out = append(out, l)
		}
	}
	return out
}

// concealedClipboard records what goes to the clipboard for Secret values (H-25) and holds it, as
// the system clipboard would.
func concealedClipboard(sh *shell) *[]string {
	var got []string
	sh.setConcealed = func(text string) bool {
		got = append(got, text)
		sh.app.Clipboard().SetContent(text)
		return true
	}
	return &got
}

// TestSecretMaskedValueCopiedWithoutShowing (the owner's decision of 2026-10-22, D-25): a masked
// Secret value has the copy icon in its key's line (RW1), its tooltip says it copies without
// showing; a tap
// reads the whole value from the GUI (a value longer than a reveal shows comes whole) and puts it
// on the clipboard for Secret values, concealed, without showing it: the drawer stays masked, the
// value enters no widget and not the revealed map, and the toast names the key, never the value.
// The clipboard is cleared after the TTL, shortened for the test.
func TestSecretMaskedValueCopiedWithoutShowing(t *testing.T) {
	f := wholeRevealFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })

	masked := maskedLines(d)
	if len(masked) != 4 {
		t.Fatalf("%d masked values, want 4", len(masked))
	}
	icon := keyCopyIcon(t, d, "DB_PASSWORD")
	if !icon.Visible() {
		t.Fatal("no copy icon in a masked key's line")
	}
	if icon.tip != "Copy the value without showing it" {
		t.Errorf("a masked key's copy icon says %q", icon.tip)
	}

	sh.app.Clipboard().SetContent("the user's own")
	test.Tap(icon)
	q.until(t, "the copy", func() bool { return len(*concealed) == 1 })
	if got := (*concealed)[0]; got != longSecretValue {
		t.Errorf("copied %d bytes, want the whole value's %d", len(got), len(longSecretValue))
	}
	calls := f.callsOf(engine.RevealCommand)
	if len(calls) != 1 || calls[0].Origin != engine.OriginGUI || !strings.Contains(string(calls[0].Params), `"key":"DB_PASSWORD"`) ||
		!strings.Contains(string(calls[0].Params), `"whole":true`) {
		t.Errorf("reveal calls %+v, want one from the GUI for the whole of DB_PASSWORD", calls)
	}

	// Still masked: nothing shown, nothing kept by the drawer.
	if len(d.revealed) != 0 || len(d.revealedLen) != 0 {
		t.Error("the copy put the value into the drawer's revealed map")
	}
	if len(findAll[*secretText](d.details)) != 0 || drawerHasText(d, "correct-horse") {
		t.Error("the copy showed the value")
	}
	if b := drawerButtons(d); len(b["Show"]) != 4 || len(b["Hide"]) != 0 {
		t.Errorf("buttons after the copy: Show %d, Hide %d", len(b["Show"]), len(b["Hide"]))
	}
	if n := len(maskedLines(d)); n != 4 {
		t.Errorf("%d masked values after the copy, want 4", n)
	}
	if !keyCopyIcon(t, d, "DB_PASSWORD").done {
		t.Error("no checkmark after the copy")
	}
	if !toastSays(sh, "DB_PASSWORD") || !toastSays(sh, "cleared in") || toastSays(sh, "correct-horse") {
		t.Error("the toast should name the key and say the clipboard is cleared, never show the value")
	}
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })
	if sh.clipSecret != "" || sh.clipTimer != nil {
		t.Error("the shell still remembers the value")
	}
}

// TestSecretMaskedCopyRefusals: what a masked value's copy puts on the clipboard when the answer is
// no whole value, or comes late. An empty value copies nothing and says so, so that the click does
// not empty the clipboard; an answer cut short (an engine that ignored whole) copies nothing, since a
// password pasted unseen and cut would be wrong; an answer for a drawer that moved on meanwhile
// copies nothing and says nothing, its error included. The answers are handed to the copy's
// callback directly: the drawer's move must come between the tap and the answer.
func TestSecretMaskedCopyRefusals(t *testing.T) {
	f := wholeRevealFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })
	ref := *d.shown
	h := func() {}
	sh.app.Clipboard().SetContent("the user's own")
	untouched := func(what string) {
		t.Helper()
		if len(*concealed) != 0 || sh.app.Clipboard().Content() != "the user's own" || sh.clipSecret != "" {
			t.Errorf("%s: %d concealed copies, the clipboard holds %d bytes", what, len(*concealed), len(sh.app.Clipboard().Content()))
		}
	}

	d.wholeRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Size: 0}, nil)
	untouched("an empty value")
	if !toastSays(sh, "empty") {
		t.Error("an empty value's copy does not say that nothing was copied")
	}
	d.wholeRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Value: longSecretValue[:4<<10], Size: len(longSecretValue), Cut: true}, nil)
	untouched("a cut value")
	if !toastSays(sh, "not read whole") || toastSays(sh, "correct-horse") {
		t.Error("a cut value's copy does not say that nothing was copied, or shows the value")
	}

	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return d.shown != nil && *d.shown == other })
	toasts := len(sh.toasts.Objects)
	d.wholeRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Value: longSecretValue, Size: len(longSecretValue)}, nil)
	untouched("an answer for the drawer that moved on")
	d.wholeRead(ref, "DB_PASSWORD", h, nil, engine.ErrNoSecretData)
	if len(sh.toasts.Objects) != toasts {
		t.Error("an answer for the drawer that moved on showed a toast")
	}
	f.update(func(u *engine.UIState) { u.Drawer = nil })
	q.until(t, "the drawer closed", func() bool { return d.shown == nil })
	d.wholeRead(ref, "DB_PASSWORD", h, engine.SecretValue{Key: "DB_PASSWORD", Value: longSecretValue, Size: len(longSecretValue)}, nil)
	untouched("an answer for the closed drawer")
}

// TestSecretCutRevealedValueCopiedWhole (the lead's decision of 2026-10-22, D-25): a revealed value
// that Show cut at 4 KiB copies the whole value through its copy icon, read again with whole as a
// masked value's copy is, its tooltip "Copy the whole value", the toast naming the key; the drawer
// keeps showing what Show showed, cut, and nothing more. A selection and ⌘C still copy exactly the
// selection.
func TestSecretCutRevealedValueCopiedWhole(t *testing.T) {
	f := wholeRevealFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	concealed := concealedClipboard(sh)
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return d.last != nil && len(drawerButtons(d)["Show"]) == 4 })

	test.Tap(drawerButtons(d)["Show"][2]) // DB_PASSWORD, longer than a reveal shows
	q.until(t, "the value", func() bool { return len(findAll[*secretText](d.details)) == 1 })
	st := findAll[*secretText](d.details)[0]
	shown := st.text
	if !strings.HasPrefix(shown, longSecretValue[:4<<10]) || !strings.Contains(shown, "[the first ") {
		t.Fatalf("Show did not cut the value: %d bytes shown", len(shown))
	}
	icon := keyCopyIcon(t, d, "DB_PASSWORD")
	if icon.tip != "Copy the whole value" {
		t.Errorf("a cut value's copy icon says %q", icon.tip)
	}
	test.Tap(icon)
	q.until(t, "the copy", func() bool { return len(*concealed) == 1 })
	if got := (*concealed)[0]; got != longSecretValue {
		t.Errorf("the icon copied %d bytes, want the whole value's %d", len(got), len(longSecretValue))
	}
	calls := f.callsOf(engine.RevealCommand)
	if len(calls) != 2 || strings.Contains(string(calls[0].Params), `"whole":true`) || !strings.Contains(string(calls[1].Params), `"whole":true`) {
		t.Errorf("reveal calls %+v, want Show's and then the copy's with whole", calls)
	}
	if sts := findAll[*secretText](d.details); len(sts) != 1 || sts[0].text != shown {
		t.Error("the copy changed what the drawer shows")
	}
	if !toastSays(sh, "DB_PASSWORD") || !toastSays(sh, "cleared in") || toastSays(sh, "correct-horse") {
		t.Error("the toast should name the key and say the clipboard is cleared, never show the value")
	}

	// A selection copies exactly the selection.
	st = findAll[*secretText](d.details)[0]
	st.TypedShortcut(&fyne.ShortcutSelectAll{})
	st.TypedShortcut(&fyne.ShortcutCopy{Clipboard: sh.app.Clipboard()})
	if len(*concealed) != 2 || (*concealed)[1] != shown {
		t.Errorf("⌘C on the whole selection copied %d bytes, want the %d selected", len((*concealed)[len(*concealed)-1]), len(shown))
	}
	if n := len(f.callsOf(engine.RevealCommand)); n != 2 {
		t.Errorf("⌘C read the value again (%d reveals)", n)
	}
}

// TestSecretRevealedValueCopied: after Show the copy icon in the key's line copies the value shown
// (RW1), to the clipboard, and the clipboard is cleared after the TTL if it still holds the value.
func TestSecretRevealedValueCopied(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(d)["Show"]) == 4 })

	test.Tap(drawerButtons(d)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return len(findAll[*secretText](d.details)) == 1 })
	st := findAll[*secretText](d.details)[0]
	if st.text != "correct-horse-battery-staple" {
		t.Fatalf("the revealed text is %q", st.text)
	}
	icon := keyCopyIcon(t, d, "DB_PASSWORD")
	if icon.tip != wholeCopyTip {
		t.Errorf("a revealed value's copy icon says %q", icon.tip)
	}
	sh.app.Clipboard().SetContent("")
	test.Tap(icon)
	if got := sh.app.Clipboard().Content(); got != "correct-horse-battery-staple" {
		t.Fatalf("the clipboard holds %q", got)
	}
	var toast string
	for _, tl := range findAll[*paragraph](sh.toasts) {
		toast += tl.text
	}
	if !strings.Contains(toast, "cleared") || strings.Contains(toast, "correct-horse") {
		t.Errorf("the toast %q should say the clipboard is cleared, and never show the value", toast)
	}
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })
	if sh.clipSecret != "" || sh.clipTimer != nil {
		t.Error("the shell still remembers the value")
	}
}

// TestSecretClipboardKeepsWhatReplacedIt: when something else was copied meanwhile, the clearing
// leaves it alone; a second Secret copy restarts the clearing.
func TestSecretClipboardKeepsWhatReplacedIt(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond

	sh.copyText("secret-one", true)
	sh.app.Clipboard().SetContent("something the user copied elsewhere")
	time.Sleep(120 * time.Millisecond)
	q.drain()
	if got := sh.app.Clipboard().Content(); got != "something the user copied elsewhere" {
		t.Errorf("the clearing took the user's own copy: %q", got)
	}

	sh.copyText("secret-two", false) // a plain copy: never cleared
	time.Sleep(120 * time.Millisecond)
	q.drain()
	if got := sh.app.Clipboard().Content(); got != "secret-two" {
		t.Errorf("a plain copy was cleared: %q", got)
	}

	sh.copyText("secret-three", true)
	sh.clearSecretClipboard() // as the window's close does
	if got := sh.app.Clipboard().Content(); got != "" {
		t.Errorf("clearing on close left %q", got)
	}
}

// A Secret's value goes to the system clipboard through the concealed path (marked for clipboard
// managers to skip, this Mac only: the owner's decision on H-25), a plain copy never does, and a
// concealed copy that fails falls back to Fyne's clipboard; the clearing works on either.
func TestSecretCopyGoesThroughTheConcealedClipboard(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond
	var concealed []string
	works := true
	sh.setConcealed = func(text string) bool {
		concealed = append(concealed, text)
		if works {
			sh.app.Clipboard().SetContent(text) // as the system clipboard would hold it
		}
		return works
	}

	sh.copyText("plain-name", false)
	sh.copyText("secret-one", true)
	if len(concealed) != 1 || concealed[0] != "secret-one" || sh.app.Clipboard().Content() != "secret-one" {
		t.Fatalf("concealed copies %q, clipboard %q", concealed, sh.app.Clipboard().Content())
	}
	q.until(t, "the concealed copy to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })

	works = false
	sh.copyText("secret-two", true)
	if len(concealed) != 2 || sh.app.Clipboard().Content() != "secret-two" {
		t.Errorf("a failed concealed copy did not fall back: clipboard %q", sh.app.Clipboard().Content())
	}
}

// TestSecretTextSelectionCopiesAsASecret: the copy shortcut on a revealed value goes through the
// same clearing as the icon.
func TestSecretTextSelectionCopiesAsASecret(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	sh.secretClipTTL = 40 * time.Millisecond
	d := sh.drawer
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(d)["Show"]) == 4 })
	test.Tap(drawerButtons(d)["Show"][0]) // DATABASE_URL, a long value that wraps
	q.until(t, "the value", func() bool { return len(findAll[*secretText](d.details)) == 1 })
	st := findAll[*secretText](d.details)[0]
	test.NewTempWindow(t, st).Resize(fyne.NewSize(200, 200))

	if lines := st.lines(); len(lines) < 2 || strings.Join(lines, "") != st.text {
		t.Errorf("the value wraps into %q", lines)
	}
	st.TypedShortcut(&fyne.ShortcutSelectAll{})
	if st.SelectedText() != st.text {
		t.Fatalf("select all: %q", st.SelectedText())
	}
	st.TypedShortcut(&fyne.ShortcutCopy{Clipboard: sh.app.Clipboard()})
	if got := sh.app.Clipboard().Content(); got != st.text {
		t.Fatalf("the clipboard holds %q", got)
	}
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })

	// A drag across the first line selects a part of it.
	charW, lineH := st.metrics()
	st.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(charW*2+1, lineH/2)}, Button: desktop.MouseButtonPrimary})
	st.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(charW*7+1, lineH/2)}, Dragged: fyne.NewDelta(charW*5, 0)})
	st.DragEnd()
	if got, want := st.SelectedText(), st.text[2:7]; got != want {
		t.Errorf("a drag over columns 2 to 7 selected %q, want %q", got, want)
	}
}

// TestCopyHasNoCommand: copying is a GUI behaviour (D-25); the registry has no command that puts a
// value on the clipboard but the object name's, so no agent can copy a Secret to the clipboard.
func TestCopyHasNoCommand(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Commands() {
		id := strings.ToLower(c.ID)
		if (strings.Contains(id, "copy") || strings.Contains(id, "clipboard")) && c.ID != "object.copyName" {
			t.Errorf("command %q copies to the clipboard", c.ID)
		}
	}
}

func TestRenderDrawerCopyIconDark(t *testing.T) {
	renderCopyIcon(t, ThemeDark, "drawer_copyicon_dark.png")
}
func TestRenderDrawerCopyIconLight(t *testing.T) {
	renderCopyIcon(t, ThemeLight, "drawer_copyicon_light.png")
}

// renderCopyIcon shows the pod drawer with its labels unfolded, the pointer over one label and the
// copy icon beside it, and one line selected.
func renderCopyIcon(t *testing.T, mode ThemeMode, name string) {
	sh, w, _, ref := podDrawerScene(t, mode)
	d := sh.drawer
	test.Tap(drawerLinks(sh)["2 Labels"])
	settle(w)
	sel := selLineWith(t, d, ref.Name)
	sel.TypedShortcut(&fyne.ShortcutSelectAll{})
	for _, hw := range findAll[*hoverWrap](d.details) {
		if p := findAll[*pill](hw.content); len(p) == 1 && strings.Contains(p[0].text, "=") {
			hw.MouseIn(mouse())
			break
		}
	}
	savePNG(t, w, name)
}
