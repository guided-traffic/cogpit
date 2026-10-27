package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Keys added and removed in a Config Map's and a Secret's drawer (the owner's audit decision of
// 2026-10-16, N152 (b)): Add Key checks the name as the API server does and against the keys there
// are, each key's remove button marks it removed with Undo, the review names the keys removed in
// the error colour with the object, its namespace and its context, its Save arms after the
// confirmation's delay and is the engine's confirmation of the removal, and the kept edits hold the
// keys too.

// removeButton returns the remove button of key's line under root.
func removeButtonOf(t *testing.T, root fyne.CanvasObject, key string) *tipButton {
	t.Helper()
	for _, b := range findAll[*tipButton](root) {
		if b.tip == "Remove the key "+key+" when you save" {
			return b
		}
	}
	t.Fatalf("no remove button for %s", key)
	return nil
}

// addKeyThrough adds key with the footer's Add Key, as the user types it.
func addKeyThrough(t *testing.T, a *keyAdder, key string) {
	t.Helper()
	test.Tap(a.button)
	if !a.row.Visible() {
		t.Fatal("Add Key did not open the row")
	}
	a.entry.SetText(key)
	if a.add.Disabled() {
		t.Fatalf("Add is disabled for %q: %s", key, a.hint.text)
	}
	test.Tap(a.add)
}

// texts are the texts of the text lines and paragraphs under root.
func texts(root fyne.CanvasObject) string {
	var out []string
	for _, l := range findAll[*textLine](root) {
		out = append(out, l.text)
	}
	for _, p := range findAll[*paragraph](root) {
		out = append(out, p.text)
	}
	return strings.Join(out, " | ")
}

// dialogConfirms makes a fake's save that removes keys ask the shell's confirmer for the dialog's
// answer, as the engine does, and records what it answered.
func dialogConfirms(sh *shell, answered *[]bool) func(engine.ConfirmRequest) (bool, bool) {
	return func(req engine.ConfirmRequest) (bool, bool) {
		ok, handled, _ := sh.confirmer.takeDialogAnswer(req)
		*answered = append(*answered, handled && ok)
		return ok, handled
	}
}

// TestConfigMapDrawerAddsAndRemovesKeys: Add Key refuses a name the API server refuses and one the
// Config Map holds, adds a valid one at the end of the data with an empty editor; a key's remove
// button marks it removed with Undo. The review names the keys, the removed one with the Config
// Map, its namespace and its context, sends them with the values they are based on, and its Save
// is the engine's confirmation of the removal; after the save the added key is a key of the data.
func TestConfigMapDrawerAddsAndRemovesKeys(t *testing.T) {
	sh, w, q, f, c := cmShell(t, ThemeDark)
	sh.confirmer.armDelay = 0
	var answered []bool
	c.confirmed = dialogConfirms(sh, &answered)
	cd := cmDataOf(sh)
	if !cd.adder.button.Visible() || cd.adder.row.Visible() {
		t.Fatal("Add Key is not in the footer of a Config Map that can be saved")
	}
	test.Tap(cd.adder.button)
	for key, want := range map[string]string{
		"two words":       "only letters, digits",
		"..data":          "does not start with",
		"allow-localhost": "holds this key already",
	} {
		cd.adder.entry.SetText(key)
		if !cd.adder.add.Disabled() || !strings.Contains(cd.adder.hint.text, want) || cd.adder.hint.color != colorStatusError {
			t.Errorf("%q: Add disabled %v, hint %q", key, cd.adder.add.Disabled(), cd.adder.hint.text)
		}
	}
	test.Tap(findAll[*widget.Button](cd.adder.row)[1]) // Cancel
	addKeyThrough(t, cd.adder, "log-level")
	r := cd.addedRows["log-level"]
	if r == nil || r.view.ReadOnly() || r.view.Text() != "" || !slices.Equal(cd.added, []string{"log-level"}) || cd.adder.row.Visible() {
		t.Fatalf("the added key: %+v, added %v", r, cd.added)
	}
	typeInto(r, "debug")
	test.Tap(removeButtonOf(t, cd.list, "bpf-lb-acceleration"))
	if !cd.removed["bpf-lb-acceleration"] || !strings.Contains(texts(cd.list), "Removed: Save removes this key and its value.") ||
		cd.counts.text() != "1 key added · 1 key removed" || cd.status.Visible() || cd.save.Disabled() {
		t.Fatalf("after a removal: counts %q, status %q", cd.counts.text(), cd.status.text)
	}
	// The pills in the footer, in the colours a diff marks its lines with (WF31; the layout:
	// TestDataFooterLayout).
	if cd.counts.removed.fg != colorStatusError || cd.counts.added.fg != colorStatusOK || cd.counts.values.Visible() ||
		len(cd.buttons.Objects) != 4 || cd.buttons.Objects[1] != cd.counts.box {
		t.Errorf("the pills: values shown %v, colours %s %s", cd.counts.values.Visible(), cd.counts.added.fg, cd.counts.removed.fg)
	}
	addKeyThrough(t, cd.adder, "log-format")
	cd.adder.entry.SetText("log-format")
	if !strings.Contains(cd.adder.reason(), "added this key already") {
		t.Errorf("a key added twice: %q", cd.adder.reason())
	}
	test.Tap(findAll[*tipButton](cd.list)[len(findAll[*tipButton](cd.list))-1]) // drop log-format
	if cd.addedRows["log-format"] != nil {
		t.Fatal("the new key's drop button did not drop it")
	}

	test.Tap(cd.save)
	q.until(t, "the review", func() bool { return cd.review != nil && !cd.review.save.Disabled() })
	rv := cd.review
	block := texts(rv.keys)
	if !strings.Contains(block, "Context | Namespace | Object | Config Map → | Changes | Removes | Adds | demo | kube-system | cilium-config |") ||
		!strings.Contains(block, "| bpf-lb-acceleration | log-level |") || strings.Contains(block, "workload") ||
		!strings.Contains(block, "This cannot be undone.") || rv.save.Importance != widget.DangerImportance {
		t.Errorf("the review's keys: %q, Save %v", block, rv.save.Importance)
	}
	rp := decodeParams[configMapDataParams](t, f.callsOf("configMap.reviewData")[0])
	if rp.Add["log-level"] != "debug" || !slices.Equal(rp.Remove, []string{"bpf-lb-acceleration"}) || rp.From["bpf-lb-acceleration"] != "disabled" || len(rp.Data) != 0 {
		t.Errorf("review params %+v", rp)
	}
	settle(w)
	test.Tap(rv.save)
	q.until(t, "the save", func() bool { return cd.review == nil && strings.HasPrefix(cd.status.text, "Saved") })
	sp := decodeParams[configMapDataParams](t, f.callsOf("configMap.saveData")[0])
	if sp.Add["log-level"] != "debug" || !slices.Equal(sp.Remove, []string{"bpf-lb-acceleration"}) || len(sp.From) != 0 {
		t.Errorf("save params %+v", sp)
	}
	if !slices.Equal(answered, []bool{true}) {
		t.Errorf("the engine's confirmation took the review's answer: %v", answered)
	}
	if !strings.HasPrefix(cd.status.text, "Saved 1 key added, 1 key removed at ") || cd.dirty() || len(cd.added) != 0 {
		t.Errorf("after the save: %q, dirty %v", cd.status.text, cd.dirty())
	}
	q.until(t, "the keys read again", func() bool { return cd.rows["log-level"] != nil && cd.rows["bpf-lb-acceleration"] == nil })
	if c.values["log-level"] != "debug" {
		t.Error("the fake did not store the new key")
	}
}

// TestConfigMapDrawerRemovalArms: the review of a save that removes a key enables Save only after
// the confirmation's delay, as the confirmation dialog does; one that only adds a key at once.
func TestConfigMapDrawerRemovalArms(t *testing.T) {
	sh, _, q, _, _ := cmShell(t, ThemeDark)
	sh.confirmer.armDelay = time.Hour // unarmed however long the dry run takes; TestArmSaveAfterTheDelay arms one
	cd := cmDataOf(sh)
	test.Tap(removeButtonOf(t, cd.list, "allow-localhost"))
	test.Tap(cd.save)
	q.until(t, "the dry run", func() bool { return cd.review != nil && cd.review.ready })
	if !cd.review.save.Disabled() {
		t.Error("Save of a removal armed at once")
	}
	cd.review.hide()
	test.Tap(cd.revert)
	addKeyThrough(t, cd.adder, "log-level")
	test.Tap(cd.save)
	q.until(t, "the review of an added key", func() bool { return cd.review != nil && cd.review.ready })
	if cd.review.save.Disabled() {
		t.Error("Save of an added key waits for the delay")
	}
}

// TestArmSaveAfterTheDelay: a review's Save of a removal arms once the confirmation's delay has
// passed since the review opened, if the review is still ready; one that removes nothing at once.
func TestArmSaveAfterTheDelay(t *testing.T) {
	sh, _, q := newTestShell(t, connectedFake(t, 1), Options{})
	sh.confirmer.armDelay = time.Millisecond
	removal := newTextButton("Save", nil, nil)
	armSave(sh, removal, true, time.Now(), func() bool { return true })
	q.until(t, "Save of a removal armed", func() bool { return !removal.Disabled() })
	sh.confirmer.armDelay = time.Hour
	added := newTextButton("Save", nil, nil)
	added.Disable()
	armSave(sh, added, false, time.Now(), func() bool { return true })
	if added.Disabled() {
		t.Error("Save of a review without a removal waits for the delay")
	}
}

// TestConfigMapDrawerKeysConflict: the key the user adds appears on the cluster, and a key the user
// removes is removed there too: the review shows the conflict; Save over version N writes the
// added key as a change of its value and drops the removal of the key gone, as the review showed.
func TestConfigMapDrawerKeysConflict(t *testing.T) {
	sh, w, q, f, c := cmShell(t, ThemeDark)
	sh.confirmer.armDelay = 0
	cd := cmDataOf(sh)
	addKeyThrough(t, cd.adder, "log-level")
	typeInto(cd.addedRows["log-level"], "debug")
	test.Tap(removeButtonOf(t, cd.list, "bpf-lb-acceleration"))
	c.change("log-level", "info")
	c.mu.Lock()
	delete(c.values, "bpf-lb-acceleration")
	c.mu.Unlock()
	test.Tap(cd.save)
	q.until(t, "the stale review", func() bool { return cd.review != nil && cd.review.over == "4212" })
	if cd.review.save.Text != "Overwrite" || !strings.Contains(cd.review.status.text, "added or removed changed there too") {
		t.Fatalf("the stale review: %q / %q", cd.review.save.Text, cd.review.status.text)
	}
	settle(w)
	test.Tap(cd.review.save)
	q.until(t, "the save over", func() bool { return cd.review == nil && strings.HasPrefix(cd.status.text, "Saved") })
	sp := decodeParams[configMapDataParams](t, f.callsOf("configMap.saveData")[0])
	if sp.ResourceVersion != "4212" || sp.Data["log-level"] != "debug" || len(sp.Add) != 0 || len(sp.Remove) != 0 {
		t.Errorf("the save over: %+v", sp)
	}
}

// TestConfigMapDrawerKeepsKeys: the keys added and removed go to configMap.keepEdits with the
// values, a removed key's value at the base as from; the drawer moving on and back brings them
// back; Revert drops them.
func TestConfigMapDrawerKeepsKeys(t *testing.T) {
	sh, _, q, f, c := cmShell(t, ThemeDark)
	cd := cmDataOf(sh)
	addKeyThrough(t, cd.adder, "log-level")
	typeInto(cd.addedRows["log-level"], "debug")
	test.Tap(removeButtonOf(t, cd.list, "allow-localhost"))
	q.until(t, "the keys kept", func() bool {
		k := c.keptValues()
		return k != nil && k.Add["log-level"] == "debug" && len(k.Remove) == 1
	})
	if k := c.keptValues(); k.From["allow-localhost"] != "auto" || k.BaseResourceVersion != "4211" || len(k.Values) != 0 {
		t.Errorf("kept %+v", k)
	}
	other := f.tables["pods"].Rows[0].Ref
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other object", func() bool { return sh.drawer.last != nil && sh.drawer.last.Ref == other })
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ciliumRef}, &ciliumRef })
	q.until(t, "the Config Map again", func() bool { cd := cmDataOf(sh); return cd != nil && cd.loaded && cd.dirty() })
	cd = cmDataOf(sh)
	if r := cd.addedRows["log-level"]; r == nil || r.view.Text() != "debug" || !cd.removed["allow-localhost"] || cd.base != "4211" {
		t.Fatalf("back: added %v, removed %v, base %s", cd.added, cd.removed, cd.base)
	}
	test.Tap(cd.revert)
	if cd.dirty() || len(cd.added) != 0 || len(cd.removed) != 0 {
		t.Error("Revert kept the keys")
	}
	q.until(t, "the kept edits dropped", func() bool { return c.keptValues() == nil })
}

// TestSecretDrawerAddsAndRemovesKeys: Add Key on a Secret puts a key with an empty editor at the
// end, typed as text; a key whose value was never shown is removed with its button; the masked
// review names both, the removed one in the error colour with the Secret, its namespace and its
// context, and holds no value; its Save is the engine's confirmation. Moving away keeps the keys in
// the window's memory only, masked when they come back.
func TestSecretDrawerAddsAndRemovesKeys(t *testing.T) {
	sh, w, q, f, s := secShell(t, ThemeDark)
	sh.confirmer.armDelay = 0
	var answered []bool
	s.confirmed = dialogConfirms(sh, &answered)
	sd := secDataOf(sh)
	test.Tap(sd.adder.button)
	sd.adder.entry.SetText("DB_HOST")
	if !sd.adder.add.Disabled() || !strings.Contains(sd.adder.hint.text, "holds this key already") {
		t.Errorf("an existing key: %q", sd.adder.hint.text)
	}
	sd.adder.close()
	addKeyThrough(t, sd.adder, "API_TOKEN")
	r := sd.addedRows["API_TOKEN"]
	if r == nil || r.view.ReadOnly() || !sd.revert.Visible() || !sd.save.Visible() {
		t.Fatal("the added key has no editor, or no Save")
	}
	typeIntoRow(r.cmRow, secTyped)
	test.Tap(removeButtonOf(t, sd.list, "DB_HOST"))
	if !sd.removed["DB_HOST"] || !strings.Contains(texts(sd.list), "Removed: Save removes this key") {
		t.Fatal("DB_HOST not shown removed")
	}
	if len(f.callsOf(engine.RevealCommand)) != 0 {
		t.Error("a removal or an added key read a value")
	}

	// Away and back: kept in memory, masked.
	ref := sdRefOf(f)
	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == other })
	if u := sh.drawer.secretUnsaved[ref.String()]; u == nil || u.add["API_TOKEN"] != secTyped || !slices.Equal(u.remove, []string{"DB_HOST"}) {
		t.Fatalf("kept %+v", u)
	}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "the Secret again", func() bool {
		sd := secDataOf(sh)
		return sd != nil && sd.ref == ref && sd.addedRows["API_TOKEN"] != nil
	})
	sd = secDataOf(sh)
	if r := sd.addedRows["API_TOKEN"]; !r.masked || !sd.removed["DB_HOST"] || drawerHasText(sh.drawer, secTyped) {
		t.Fatalf("back: masked %v, removed %v", r.masked, sd.removed)
	}

	test.Tap(sd.save)
	q.until(t, "the review", func() bool { return sd.review != nil && !sd.review.save.Disabled() })
	rv := sd.review
	all := texts(rv.changes) + " | " + texts(rv.keys) + " | " + rv.banner.text + " | " + rv.status.text
	if !strings.Contains(all, "API_TOKEN") || !strings.Contains(all, "added, 27 B") || !strings.Contains(all, "removed, 38 B") ||
		!strings.Contains(all, "Context | Namespace | Object | Secret → | Changes | Removes | Adds | demo | checkout | checkout-db |") ||
		!strings.Contains(all, "1 key added, 1 key removed · no value shown | DB_HOST | API_TOKEN | This cannot be undone.") ||
		strings.Contains(all, "workload") {
		t.Errorf("the review: %q", all)
	}
	if strings.Contains(all, secTyped) || strings.Contains(all, fixtureSecretValues["DB_HOST"]) {
		t.Errorf("the review shows a value: %q", all)
	}
	for _, l := range findAll[*textLine](rv.changes) {
		if strings.HasPrefix(l.text, "removed") && l.color != colorStatusError {
			t.Errorf("a removal not in the error colour: %q", l.text)
		}
	}
	settle(w)
	test.Tap(rv.save)
	q.until(t, "the save", func() bool { return sd.review == nil && strings.HasPrefix(sd.status.text, "Saved") })
	sp := decodeParams[secretDataParams](t, f.callsOf(engine.SecretSaveDataCommand)[0])
	if sp.Add["API_TOKEN"] != secTyped || !slices.Equal(sp.Remove, []string{"DB_HOST"}) || len(sp.Data) != 0 {
		t.Errorf("save params %+v", sp)
	}
	if !slices.Equal(answered, []bool{true}) || s.values["API_TOKEN"] != secTyped {
		t.Errorf("the confirmation's answer %v, stored %v", answered, s.values["API_TOKEN"] == secTyped)
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}
}

// renderKeysDrawer renders a drawer with a key added and one removed, and its review over it.
func renderKeysReview(t *testing.T, sh *shell, w fyne.Window, q *mainQueue, save *textButton, open func() bool, name string) {
	t.Helper()
	q.until(t, "the list", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	savePNG(t, w, strings.Replace(name, "_review", "_drawer", 1))
	test.Tap(save)
	q.until(t, "the review", open)
	settle(w)
	savePNG(t, w, name)
}

func renderConfigMapKeys(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "configmaps")
	newCMFakeWith(t, f, map[string]string{
		"agent-not-ready-taint-key": ciliumValues["agent-not-ready-taint-key"], "bpf-lb-acceleration": "disabled",
		"enable-ipv4": "true", "kube-proxy-replacement-healthz-bind-address": "",
	})
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	sh.confirmer.armDelay = 0
	q.until(t, "the values", func() bool { cd := cmDataOf(sh); return cd != nil && cd.loaded })
	cd := cmDataOf(sh)
	test.Tap(removeButtonOf(t, cd.list, "bpf-lb-acceleration"))
	addKeyThrough(t, cd.adder, "enable-ipv6")
	typeInto(cd.addedRows["enable-ipv6"], "true")
	renderKeysReview(t, sh, w, q, cd.save, func() bool { return cd.review != nil && !cd.review.save.Disabled() }, name)
}

func TestRenderConfigMapKeysDark(t *testing.T) {
	renderConfigMapKeys(t, ThemeDark, "configmap_keys_review_dark.png")
}

func renderSecretKeys(t *testing.T, mode ThemeMode, name string) {
	sh, _, q, _, _ := secShell(t, mode)
	sh.confirmer.armDelay = 0
	sd := secDataOf(sh)
	w := sh.win
	test.Tap(removeButtonOf(t, sd.list, "DB_HOST"))
	addKeyThrough(t, sd.adder, "API_TOKEN")
	typeIntoRow(sd.addedRows["API_TOKEN"].cmRow, secTyped)
	renderKeysReview(t, sh, w, q, sd.save, func() bool { return sd.review != nil && !sd.review.save.Disabled() }, name)
}

func TestRenderSecretKeysDark(t *testing.T) {
	renderSecretKeys(t, ThemeDark, "secret_keys_review_dark.png")
}
