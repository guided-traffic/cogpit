package fyneui

import (
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The show-all link in the hint above a Secret's keys (secretshowall.go): every value shown at once
// through secret.reveal from the GUI, one read per key, as a click on every Show; hide-all as a
// click on every Hide; the key x's hook revealAll. The Secret is checkout-db of secretsFake: four text keys
// and a binary one.

// secretTextKeys are checkout-db's keys with a Show, in the data's order.
var secretTextKeys = []string{"DATABASE_URL", "DB_HOST", "DB_PASSWORD", "DB_USER"}

// allShown reports whether the drawer shows every text value of checkout-db, read-only, each key
// with Hide (DB_USER's value is the namespace's name too: its Hide says it).
func allShown(sh *shell) bool {
	for _, k := range secretTextKeys {
		if !drawerHasText(sh.drawer, fixtureSecretValues[k]) {
			return false
		}
	}
	return len(drawerButtons(sh.drawer)["Hide"]) == 4
}

// noneShown reports whether the drawer shows no text value of checkout-db, its four keys masked
// (DB_USER's value is the namespace's name too: the Show buttons say it).
func noneShown(sh *shell) bool {
	for _, k := range []string{"DATABASE_URL", "DB_HOST", "DB_PASSWORD"} {
		if drawerHasText(sh.drawer, fixtureSecretValues[k]) {
			return false
		}
	}
	return len(drawerButtons(sh.drawer)["Show"]) == 4
}

// revealsOf are the keys secret.reveal was asked for, each from the GUI, from the from-th call on,
// sorted: the reads run in parallel.
func revealsOf(t *testing.T, f *fakeEngine, from int) []string {
	t.Helper()
	var keys []string
	for _, c := range f.callsOf(engine.RevealCommand)[from:] {
		if c.Origin != engine.OriginGUI {
			t.Errorf("secret.reveal dispatched by %s", c.Origin)
		}
		keys = append(keys, decodeParams[engine.RevealParams](t, c).Key)
	}
	slices.Sort(keys)
	return keys
}

// TestSecretShowAllReadOnly: where nothing can be saved, Show all reveals every text value in
// place, one GUI read per key (not whole: a long value stays cut as Show cuts it), and turns into
// Hide all, which masks them all and forgets them with no read; after a single Show, Show all reads
// the rest only. No value reaches a toast or another command.
func TestSecretShowAllReadOnly(t *testing.T) {
	f, _ := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 })
	sd := secDataOf(sh)
	if !sd.allHint.Visible() || sd.revealAllText() != "To show-all secrets at once press x." {
		t.Fatalf("the hint: visible %v, %q", sd.allHint.Visible(), sd.revealAllText())
	}

	test.Tap(sd.allLink)
	q.until(t, "every value", func() bool { return allShown(sh) && sd.allLink.text == hideAllWord })
	if got := revealsOf(t, f, 0); strings.Join(got, ",") != strings.Join(secretTextKeys, ",") {
		t.Errorf("reveals %v, want one per text key", got)
	}
	for _, c := range f.callsOf(engine.RevealCommand) {
		if p := decodeParams[engine.RevealParams](t, c); p.Whole || p.Base64 {
			t.Errorf("a read-only Show all asked for %+v", p)
		}
	}
	if b := drawerButtons(sh.drawer); len(b["Hide"]) != 4 || len(b["Show"]) != 0 || sd.revealAllText() != "To hide-all secrets at once press x." {
		t.Errorf("after show-all: %d Hide, %d Show, hint %q", len(b["Hide"]), len(b["Show"]), sd.revealAllText())
	}

	test.Tap(sd.allLink)
	if !noneShown(sh) || len(sh.drawer.revealed) != 0 || sd.allLink.text != showAllWord {
		t.Fatalf("after Hide all: %d revealed, button %q", len(sh.drawer.revealed), sd.allLink.text)
	}
	if n := len(f.callsOf(engine.RevealCommand)); n != 4 {
		t.Errorf("Hide all read: %d reveals", n)
	}

	test.Tap(drawerButtons(sh.drawer)["Show"][2]) // DB_PASSWORD
	q.until(t, "one value", func() bool { return drawerHasText(sh.drawer, fixtureSecretValues["DB_PASSWORD"]) })
	if sd.allLink.text != showAllWord {
		t.Errorf("one of four shown: the button reads %q", sd.allLink.text)
	}
	test.Tap(sd.allLink)
	q.until(t, "the rest", func() bool { return allShown(sh) && sd.allLink.text == hideAllWord })
	if got := revealsOf(t, f, 5); strings.Join(got, ",") != "DATABASE_URL,DB_HOST,DB_USER" {
		t.Errorf("Show all after a Show read %v, want the three masked keys", got)
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or another command", l)
	}
}

// TestSecretShowAllEditable: where the values can be saved, Show all reads each whole into its
// editor, as Show does there; Hide all drops the unedited ones and masks an edited or added one,
// which Show all brings back from the window's memory without a read.
func TestSecretShowAllEditable(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	sd := secDataOf(sh)
	test.Tap(sd.allLink)
	q.until(t, "every editor", func() bool { return len(sd.rows) == 4 && sd.allLink.text == hideAllWord })
	for _, c := range f.callsOf(engine.RevealCommand) {
		if p := decodeParams[engine.RevealParams](t, c); !p.Whole {
			t.Errorf("an editable Show all read %s cut", p.Key)
		}
	}
	for _, k := range secretTextKeys {
		if r := sd.rows[k]; r == nil || r.masked || r.view.Text() != fixtureSecretValues[k] {
			t.Errorf("%s's editor: %+v", k, r)
		}
	}
	typeIntoRow(sd.rows["DB_PASSWORD"].cmRow, secTyped)
	addKeyThrough(t, sd.adder, "API_KEY")
	q.until(t, "the added key", func() bool { return sd.addedRows["API_KEY"] != nil })
	typeIntoRow(sd.addedRows["API_KEY"].cmRow, "added-value")

	test.Tap(sd.allLink)
	if len(sd.rows) != 1 || !sd.rows["DB_PASSWORD"].masked || !sd.addedRows["API_KEY"].masked || sd.allLink.text != showAllWord {
		t.Fatalf("after Hide all: %d rows, button %q", len(sd.rows), sd.allLink.text)
	}
	if !sd.dirty() || sd.rows["DB_PASSWORD"].view.Text() != secTyped {
		t.Error("Hide all dropped an edit")
	}

	reads := len(f.callsOf(engine.RevealCommand))
	test.Tap(sd.allLink)
	q.until(t, "every editor again", func() bool { return len(sd.rows) == 4 && sd.allLink.text == hideAllWord })
	if got := revealsOf(t, f, reads); strings.Join(got, ",") != "DATABASE_URL,DB_HOST,DB_USER" {
		t.Errorf("Show all over a masked edit read %v, want the unedited keys", got)
	}
	if r := sd.rows["DB_PASSWORD"]; r.masked || r.view.Text() != secTyped || sd.addedRows["API_KEY"].masked {
		t.Error("Show all did not bring the masked edits back")
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or another command", l)
	}
}

// TestSecretShowAllHiddenWithoutText: a Secret whose data has no value with a Show has no hint.
func TestSecretShowAllHiddenWithoutText(t *testing.T) {
	sh, _, q, _, _ := secShell(t, ThemeDark)
	sd := secDataOf(sh)
	for _, k := range secretTextKeys { // removed keys have Undo, no Show
		sd.removeKey(k)
	}
	q.until(t, "the keys removed", func() bool { return len(drawerButtons(sh.drawer)["Show"]) == 0 })
	if sd.allHint.Visible() {
		t.Error("Show all with no key to show")
	}
}

// TestShowAllHasNoCommand: Show all is the window's, through secret.reveal, which agents and the
// CLI are refused (TestRevealIsGUIOnly): no command, so no MCP tool, shows or hides every value.
func TestShowAllHasNoCommand(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Commands() {
		id := strings.ToLower(c.ID)
		if strings.Contains(id, "showall") || strings.Contains(id, "revealall") || strings.Contains(id, "hideall") {
			t.Errorf("command %q shows or hides every value", c.ID)
		}
	}
}

// TestRevealAllToggles: the key x's hook on the Secret the drawer shows toggles as the button does;
// on any other kind it does nothing.
func TestRevealAllToggles(t *testing.T) {
	f, ref := secretsFake(t)
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 })
	sh.drawer.revealAll(model.ObjectRef{Context: "demo", Kind: "pods", Namespace: "checkout", Name: "checkout-db"})
	if sh.drawer.revealAllWant != nil || len(f.callsOf(engine.RevealCommand)) != 0 {
		t.Fatal("revealAll acted on a pod")
	}
	sh.drawer.revealAll(ref)
	q.until(t, "every value", func() bool { return allShown(sh) })
	sh.drawer.revealAll(ref)
	if !noneShown(sh) || len(sh.drawer.revealed) != 0 {
		t.Error("revealAll on a fully shown Secret did not hide it")
	}
}

// TestRevealAllBeforeTheDrawerBuildsIt: revealAll for a Secret the drawer was just asked for shows
// every value once its data is built, once; a request the drawer moves away from is dropped.
func TestRevealAllBeforeTheDrawerBuildsIt(t *testing.T) {
	f, ref := secretsFake(t)
	f.ui.Selected, f.ui.Drawer = nil, nil
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the list", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })

	sh.drawer.revealAll(ref) // as the key x does: the drawer is asked for ref, then the hook runs
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "every value", func() bool { return allShown(sh) && secDataOf(sh).allLink.text == hideAllWord })
	if sh.drawer.revealAllWant != nil || len(revealsOf(t, f, 0)) != 4 {
		t.Errorf("the request stayed, or read %v", revealsOf(t, f, 0))
	}

	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	third := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "checkout", Name: "checkout-api-token"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == other })
	sh.drawer.revealAll(ref) // ref is asked for, but the drawer goes elsewhere
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{third}, &third })
	q.until(t, "the third Secret", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == third })
	if sh.drawer.revealAllWant != nil {
		t.Fatal("the drawer moved to another object and kept the request")
	}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "the first Secret", func() bool {
		return sh.drawer.last != nil && sh.drawer.last.Ref == ref && len(drawerButtons(sh.drawer)["Show"]) == 4
	})
	if !noneShown(sh) || len(revealsOf(t, f, 0)) != 4 {
		t.Errorf("a dropped request revealed: %v", revealsOf(t, f, 0))
	}

	sh.drawer.revealAll(ref)
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = nil, nil })
	q.until(t, "the drawer closed", func() bool { return sh.drawer.shown == nil })
	if sh.drawer.revealAllWant != nil {
		t.Error("the closed drawer kept the request")
	}
}

// renderShowAll renders checkout-db's drawer with Show all in the data's head; shown taps it, so
// that every value shows and the button reads Hide all.
func renderShowAll(t *testing.T, mode ThemeMode, name string, editable, shown bool) {
	var (
		sh *shell
		q  *mainQueue
	)
	if editable {
		s, w, mq, _, _ := secShell(t, mode)
		sh, q = s, mq
		defer func() { settle(w); lookPNG(t, w, name) }()
	} else {
		f, _ := secretsFake(t)
		s, w, mq := newTestShell(t, f, Options{Theme: mode})
		sh, q = s, mq
		defer func() { settle(w); lookPNG(t, w, name) }()
	}
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.drawer.last != nil && secDataOf(sh) != nil && sh.diag.total > 0
	})
	if shown {
		test.Tap(secDataOf(sh).allLink)
		q.until(t, "every value", func() bool { return secDataOf(sh).allLink.text == hideAllWord })
	}
}

func TestRenderSecretShowAll(t *testing.T) {
	for _, c := range []struct {
		name            string
		mode            ThemeMode
		editable, shown bool
	}{
		{"showall_readonly_dark.png", ThemeDark, false, false},
		{"showall_readonly_light.png", ThemeLight, false, false},
		{"showall_readonly_shown_dark.png", ThemeDark, false, true},
		{"showall_readonly_shown_light.png", ThemeLight, false, true},
		{"showall_editable_dark.png", ThemeDark, true, false},
		{"showall_editable_shown_dark.png", ThemeDark, true, true},
		{"showall_editable_shown_light.png", ThemeLight, true, true},
	} {
		t.Run(c.name, func(t *testing.T) { renderShowAll(t, c.mode, c.name, c.editable, c.shown) })
	}
}
