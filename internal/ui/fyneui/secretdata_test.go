package fyneui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/yamlview"
)

// A Secret's values edited in the drawer, the window's only (the owner's audit answer of
// 2026-10-15, D-59): Show puts the whole value into an editor, Save reviews a masked diff and a
// dry run and writes through secret.saveData, a newer version rebases or shows the conflict,
// edited values the drawer moves away from stay in the window's memory, never on disk, and no
// value reaches a toast, the notifications or a command but the GUI-only ones. The Secret is
// checkout-db of secretsFake; every value is made up.

const secTyped = "new-horse-battery-staple-77"

// secFake answers the drawer's Secret commands as the engine does, on checkout-db: the whole value
// on Show, a masked review with a dry run, a save that stores the values under a new version;
// change is another client's write, which makes the drawer model list the keys at the new version.
type secFake struct {
	mu     sync.Mutex
	f      *fakeEngine
	t      testing.TB
	ref    model.ObjectRef
	values map[string]string
	rv     int
	// confirmed, when set, answers a save that removes keys as the engine's confirmation does.
	confirmed func(engine.ConfirmRequest) (ok, handled bool)
}

func secretEditFake(t testing.TB) (*fakeEngine, *secFake) {
	f, ref := secretsFake(t)
	s := &secFake{f: f, t: t, ref: ref, values: map[string]string{}, rv: 48210}
	for k, v := range fixtureSecretValues {
		s.values[k] = v
	}
	f.handlers[engine.RevealCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p engine.RevealParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		if inv.Origin != engine.OriginGUI {
			t.Errorf("secret.reveal dispatched by %s", inv.Origin)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return fakeRevealed(p, s.values[p.Key]), nil
	}
	params := func(inv engine.Invocation) (secretDataParams, error) {
		if inv.Origin != engine.OriginGUI {
			t.Errorf("%s dispatched by %s", inv.ID, inv.Origin)
		}
		var p secretDataParams
		err := json.Unmarshal(normalize(inv.Params), &p)
		return p, err
	}
	f.handlers[engine.SecretReviewDataCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p, err := params(inv)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		cur := strconv.Itoa(s.rv)
		r := engine.SecretDataReview{Ref: s.ref, UID: "uid-checkout-db", Changes: s.changesOf(p), BaseResourceVersion: p.ResourceVersion, CachedResourceVersion: cur}
		if p.ResourceVersion != cur {
			rebase := len(p.From) > 0 || (len(p.Data)+len(p.Remove) == 0 && len(p.Add) > 0)
			for _, k := range append(mapKeys(p.Data), p.Remove...) {
				if from, ok := p.From[k]; !ok || from != s.values[k] {
					rebase = false
				}
			}
			for k := range p.Add {
				if _, ok := s.values[k]; ok {
					rebase = false
				}
			}
			if rebase {
				r.RebasedFrom, r.BaseResourceVersion = p.ResourceVersion, cur
			} else {
				r.Stale = true
			}
		}
		r.Accepted = len(r.Changes) > 0 && !r.Stale
		return r, nil
	}
	f.handlers[engine.SecretSaveDataCommand] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p, err := params(inv)
		if err != nil {
			return nil, err
		}
		if len(p.Remove) > 0 && s.confirmed != nil { // destructive: the engine asks the confirmer
			req := engine.ConfirmRequest{Command: engine.SecretSaveDataCommand, Origin: inv.Origin, Context: p.Ref.Context,
				Danger: engine.DangerDestructive, Objects: []model.ObjectRef{p.Ref}, UIDs: []string{"uid-checkout-db"},
				Detail: engine.SecretSaveDetail(mapKeys(p.Data), mapKeys(p.Add), p.Remove)}
			if ok, handled := s.confirmed(req); !handled || !ok {
				return nil, engine.ErrDeclined
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if cur := strconv.Itoa(s.rv); p.ResourceVersion != cur {
			return nil, &engine.DataConflictError{Ref: s.ref, BaseResourceVersion: p.ResourceVersion, CurrentResourceVersion: cur, Changes: s.changesOf(p)}
		}
		var keys, added []string
		for k, v := range p.Data {
			s.values[k] = v
			keys = append(keys, k)
		}
		for k, v := range p.Add {
			s.values[k] = v
			added = append(added, k)
		}
		for _, k := range p.Remove {
			delete(s.values, k)
		}
		sort.Strings(keys)
		sort.Strings(added)
		s.rv++
		s.publish()
		return engine.SecretDataSaved{Ref: s.ref, Keys: keys, KeysAdded: added, KeysRemoved: p.Remove, ResourceVersion: strconv.Itoa(s.rv)}, nil
	}
	if err := engine.Register(f.reg, engine.Spec[secretDataParams]{ID: engine.SecretSaveDataCommand, Label: "Save Secret Data",
		Danger: engine.DangerMutating, GUIOnly: engine.ErrSecretDataGUIOnly,
		Enabled: func(*engine.Session, secretDataParams) error { return f.reason(engine.SecretSaveDataCommand) },
		Targets: func(_ *engine.Session, p secretDataParams) ([]model.ObjectRef, error) {
			return []model.ObjectRef{p.Ref}, nil
		},
		Run: func(context.Context, *engine.Session, secretDataParams) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	return f, s
}

// changes is the masked diff of data against the fake's values; mu is held.
func (s *secFake) changes(data map[string]string) []engine.SecretKeyChange {
	var out []engine.SecretKeyChange
	for k, v := range data {
		if s.values[k] != v {
			out = append(out, engine.SecretKeyChange{Key: k, Change: engine.SecretKeyChanged, Bytes: len(s.values[k]), NewBytes: len(v)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// changesOf is the masked diff of an edit against the fake's values, as the engine reports it
// against a newer version too; mu is held.
func (s *secFake) changesOf(p secretDataParams) []engine.SecretKeyChange {
	out := s.changes(p.Data)
	for k, v := range p.Add {
		old, ok := s.values[k]
		switch {
		case !ok:
			out = append(out, engine.SecretKeyChange{Key: k, Change: engine.SecretKeyAdded, NewBytes: len(v)})
		case old != v:
			out = append(out, engine.SecretKeyChange{Key: k, Change: engine.SecretKeyChanged, Bytes: len(old), NewBytes: len(v)})
		}
	}
	for _, k := range p.Remove {
		if old, ok := s.values[k]; ok {
			out = append(out, engine.SecretKeyChange{Key: k, Change: engine.SecretKeyRemoved, Bytes: len(old)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// change is another client's write of one value: a new version the drawer model lists.
func (s *secFake) change(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	s.rv++
	s.publish()
}

// publish builds the drawer model of the Secret at the fake's version and tells the window; mu is
// held.
func (s *secFake) publish() {
	obj := fixtureSecretObjects()[0].DeepCopy()
	data := map[string]any{"ca.crt": obj.Object["data"].(map[string]any)["ca.crt"]}
	for k, v := range s.values {
		data[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	obj.Object["data"] = data
	obj.SetResourceVersion(strconv.Itoa(s.rv))
	p, _ := kinds.ByKey("secrets")
	m, err := engine.BuildDetail(p, s.ref, obj, nil, fixedNow)
	if err != nil {
		s.t.Fatal(err)
	}
	s.f.mu.Lock()
	s.f.details[s.ref.String()] = m
	s.f.mu.Unlock()
	go s.f.emit(engine.Event{Type: engine.EventDetail})
}

func secDataOf(sh *shell) *secData {
	sd, _ := sh.drawer.kept[secDataKept].(*secData)
	return sd
}

// secShell opens the drawer on checkout-db with the save enabled.
func secShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine, *secFake) {
	t.Helper()
	f, s := secretEditFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 && secDataOf(sh) != nil
	})
	return sh, w, q, f, s
}

// showKey taps Show beside key and waits for its editor.
func showKey(t *testing.T, sh *shell, q *mainQueue, key string) *secRow {
	t.Helper()
	keys := []string{"DATABASE_URL", "DB_HOST", "DB_PASSWORD", "DB_USER"} // Show's order: the text keys, sorted
	i := -1
	for j, k := range keys {
		if k == key {
			i = j
		}
	}
	shows := drawerButtons(sh.drawer)["Show"]
	n := 0
	for _, k := range keys[:i] {
		if secDataOf(sh).rows[k] == nil {
			n++
		}
	}
	test.Tap(shows[n])
	q.until(t, "the editor of "+key, func() bool { return secDataOf(sh).rows[key] != nil })
	return secDataOf(sh).rows[key]
}

// valueLeaks reports a Secret value of the fixture, or the typed one, in what the window shows
// outside the drawer's editors (its toasts, which the notification log keeps) or in any command's
// parameters but the GUI-only reveal, review and save.
func valueLeaks(sh *shell, f *fakeEngine) string {
	// The distinctive values: DB_USER's "checkout" is the namespace's name too.
	vals := []string{secTyped, fixtureSecretValues["DB_PASSWORD"], fixtureSecretValues["DATABASE_URL"]}
	var texts []string
	for _, o := range sh.toasts.Objects {
		for _, l := range findAll[*textLine](o) {
			texts = append(texts, l.text)
		}
		for _, p := range findAll[*paragraph](o) {
			texts = append(texts, p.text)
		}
	}
	for _, c := range f.calls { // no command but the GUI-only reveal, review and save carries a value
		switch c.ID {
		case engine.RevealCommand, engine.SecretReviewDataCommand, engine.SecretSaveDataCommand:
		default:
			texts = append(texts, string(c.Params))
		}
	}
	for _, t := range texts {
		for _, v := range vals {
			if strings.Contains(t, v) {
				return v
			}
		}
	}
	return ""
}

// TestSecretDrawerEditAndSave: Show reads the whole value into an editor below its key, editable,
// Hide at the key's end; typing enables Save; Save reviews a masked diff (the key and its sizes,
// no value) and the dry run, based on the version the value was revealed at, the value read sent
// as from; its Save writes exactly the edited value through secret.saveData.
func TestSecretDrawerEditAndSave(t *testing.T) {
	sh, w, q, f, s := secShell(t, ThemeDark)
	sd := secDataOf(sh)
	if !sd.buttons.Visible() || !sd.adder.button.Visible() || sd.revert.Visible() || sd.save.Visible() || sd.status.Visible() {
		t.Error("before a value is in an editor the footer shows Add Key only")
	}
	r := showKey(t, sh, q, "DB_PASSWORD")
	calls := f.callsOf(engine.RevealCommand)
	if len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"whole":true`) || calls[0].Origin != engine.OriginGUI {
		t.Fatalf("reveal calls %+v", calls)
	}
	if r.view.ReadOnly() || r.view.Text() != fixtureSecretValues["DB_PASSWORD"] || r.at != "48210" || r.masked {
		t.Fatalf("the editor: read-only %v, text %q, at %s", r.view.ReadOnly(), r.view.Text(), r.at)
	}
	if !sd.buttons.Visible() || !sd.save.Disabled() || len(drawerButtons(sh.drawer)["Hide"]) != 1 {
		t.Errorf("the footer of an unedited value: buttons %v, save disabled %v", sd.buttons.Visible(), sd.save.Disabled())
	}
	settle(w)
	typeIntoRow(r.cmRow, secTyped)
	if sd.save.Disabled() || sd.counts.text() != "1 value edited" || sd.status.Visible() {
		t.Fatalf("after editing: save disabled %v, counts %q, status %q", sd.save.Disabled(), sd.counts.text(), sd.status.text)
	}
	test.Tap(sd.save)
	q.until(t, "the review", func() bool { return sd.review != nil && !sd.review.save.Disabled() })
	rp := decodeParams[secretDataParams](t, f.callsOf(engine.SecretReviewDataCommand)[0])
	if rp.ResourceVersion != "48210" || rp.Data["DB_PASSWORD"] != secTyped || rp.From["DB_PASSWORD"] != fixtureSecretValues["DB_PASSWORD"] || len(rp.Data) != 1 {
		t.Errorf("review params %+v", rp)
	}
	var lines []string
	for _, l := range findAll[*textLine](sd.review.changes) {
		lines = append(lines, l.text)
	}
	if got := strings.Join(lines, " | "); !strings.Contains(got, "DB_PASSWORD") || !strings.Contains(got, "changed, 28 B → 27 B") {
		t.Errorf("the masked diff: %q", got)
	}
	for _, text := range append(lines, sd.review.banner.text, sd.review.status.text) {
		if strings.Contains(text, secTyped) || strings.Contains(text, fixtureSecretValues["DB_PASSWORD"]) {
			t.Errorf("the review shows a value: %q", text)
		}
	}
	settle(w)
	test.Tap(sd.review.save)
	q.until(t, "the save", func() bool { return sd.review == nil && strings.HasPrefix(sd.status.text, "Saved") })
	sp := decodeParams[secretDataParams](t, f.callsOf(engine.SecretSaveDataCommand)[0])
	if sp.ResourceVersion != "48210" || sp.Data["DB_PASSWORD"] != secTyped || len(sp.Data) != 1 || len(sp.From) != 0 {
		t.Errorf("save params %+v", sp)
	}
	if s.values["DB_PASSWORD"] != secTyped || sd.dirty() || !strings.HasPrefix(sd.status.text, "Saved 1 value at ") {
		t.Errorf("after the save: stored %v, dirty %v, status %q", s.values["DB_PASSWORD"] == secTyped, sd.dirty(), sd.status.text)
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}
}

// TestSecretDrawerHide: Hide on an unedited value masks it and the window forgets it; on an edited
// one it masks the value and keeps the typing, which Show brings back without reading the Secret.
func TestSecretDrawerHide(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	sd := secDataOf(sh)
	showKey(t, sh, q, "DB_USER")
	test.Tap(drawerButtons(sh.drawer)["Hide"][0])
	if sd.rows["DB_USER"] != nil || len(drawerButtons(sh.drawer)["Hide"]) != 0 || len(findAll[*yamlviewView](sh.drawer.details)) != 0 {
		t.Fatal("Hide kept an unedited value")
	}
	r := showKey(t, sh, q, "DB_PASSWORD")
	typeIntoRow(r.cmRow, secTyped)
	test.Tap(drawerButtons(sh.drawer)["Hide"][0])
	if !r.masked || !drawerHasText(sh.drawer, "(edited, not saved)") {
		t.Fatal("Hide on an edited value did not mask it")
	}
	reveals := len(f.callsOf(engine.RevealCommand))
	test.Tap(drawerButtons(sh.drawer)["Show"][2]) // DB_PASSWORD's, on its masked line
	if r.masked || r.view.Text() != secTyped || len(f.callsOf(engine.RevealCommand)) != reveals {
		t.Errorf("Show of an edited value: masked %v, text %q, reveals %d (was %d)", r.masked, r.view.Text(), len(f.callsOf(engine.RevealCommand)), reveals)
	}
}

// TestSecretDrawerKeepsUnsavedInMemory: edited values the drawer moves away from stay in the
// window's memory, never handed to a command (no draft); back on the Secret they are masked, and
// Show brings them back based on the version they were edited at. Revert drops them.
func TestSecretDrawerKeepsUnsavedInMemory(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	r := showKey(t, sh, q, "DB_PASSWORD")
	typeIntoRow(r.cmRow, secTyped)
	other := model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "payments", Name: "stripe-webhook"}
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{other}, &other })
	q.until(t, "the other Secret", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == other })
	if u := sh.drawer.secretUnsaved[sdRefOf(f).String()]; u == nil || u.values["DB_PASSWORD"] != secTyped || u.base != "48210" {
		t.Fatalf("kept %+v", u)
	}
	if r.view.Undo() { // the values typed are kept; their undo history, which holds them too, is not (D-84)
		t.Error("the editor's undo history outlived the drawer")
	}
	if !toastSays(sh, "kept in this window's memory until Cogpit quits, never on disk") {
		t.Error("no toast says where the changes are kept")
	}
	for _, c := range f.calls {
		if strings.Contains(string(c.Params), secTyped) && c.ID != engine.SecretReviewDataCommand && c.ID != engine.SecretSaveDataCommand {
			t.Errorf("the typed value reached %s", c.ID)
		}
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}

	ref := sdRefOf(f)
	f.update(func(u *engine.UIState) { u.Selected, u.Drawer = []model.ObjectRef{ref}, &ref })
	q.until(t, "the Secret again", func() bool { sd := secDataOf(sh); return sd != nil && sd.ref == ref && sd.rows["DB_PASSWORD"] != nil })
	sd := secDataOf(sh)
	back := sd.rows["DB_PASSWORD"]
	if !back.masked || back.view.Text() != secTyped || back.from != fixtureSecretValues["DB_PASSWORD"] || sd.base != "48210" ||
		!strings.Contains(sd.status.text, "are back, masked") || drawerHasText(sh.drawer, secTyped) {
		t.Errorf("back: masked %v, text %q, base %s, status %q", back.masked, back.view.Text(), sd.base, sd.status.text)
	}
	if len(sh.drawer.secretUnsaved) != 0 {
		t.Error("the kept values stay kept after they came back")
	}
	test.Tap(sd.revert)
	if sd.dirty() || sd.rows["DB_PASSWORD"] != nil || !sd.save.Disabled() {
		t.Error("Revert did not drop the edit")
	}
}

func sdRefOf(*fakeEngine) model.ObjectRef {
	return model.ObjectRef{Context: "demo", Kind: "secrets", Namespace: "checkout", Name: "checkout-db"}
}

// TestSecretDrawerRebaseAndConflict: another key changed since the value was revealed: the review
// comes back rebased onto the newer version and Save writes with it. The edited key changed: the
// conflict with Save over version N, the explicit second step. An unedited value revealed at the
// older version is masked again (R-6).
func TestSecretDrawerRebaseAndConflict(t *testing.T) {
	sh, w, q, f, s := secShell(t, ThemeDark)
	sd := secDataOf(sh)
	r := showKey(t, sh, q, "DB_PASSWORD")
	showKey(t, sh, q, "DB_USER")
	typeIntoRow(r.cmRow, secTyped)
	s.change("DB_HOST", "db.other.invalid")
	q.until(t, "the newer listing", func() bool { return sd.latest == "48211" })
	if sd.rows["DB_USER"] != nil || sd.rows["DB_PASSWORD"] == nil {
		t.Errorf("after the change: an unedited value kept %v, the edited one kept %v", sd.rows["DB_USER"] != nil, sd.rows["DB_PASSWORD"] != nil)
	}
	if !strings.Contains(sd.status.text, "Changed on the cluster since your edits began") {
		t.Errorf("status %q", sd.status.text)
	}
	test.Tap(sd.save)
	q.until(t, "the rebased review", func() bool { return sd.review != nil && !sd.review.save.Disabled() })
	if sd.review.base != "48211" || !strings.Contains(texts(sd.review.keys), "rebased onto the cluster's change in other keys") || sd.review.over != "" {
		t.Fatalf("the rebased review: base %s, %q", sd.review.base, texts(sd.review.keys))
	}
	settle(w)
	test.Tap(sd.review.save)
	q.until(t, "the save", func() bool { return sd.review == nil && strings.HasPrefix(sd.status.text, "Saved") })
	if p := decodeParams[secretDataParams](t, f.callsOf(engine.SecretSaveDataCommand)[0]); p.ResourceVersion != "48211" {
		t.Errorf("the rebased save based on %s", p.ResourceVersion)
	}

	// The edited key changes on the cluster: the conflict.
	q.until(t, "the saved listing", func() bool { return sd.latest == "48212" })
	r = sd.rows["DB_PASSWORD"]
	typeIntoRow(r.cmRow, secTyped+"-2")
	s.change("DB_PASSWORD", "rotated-by-someone-else")
	test.Tap(sd.save)
	q.until(t, "the conflict", func() bool { return sd.review != nil && sd.review.over == "48213" })
	if sd.review.save.Text != "Overwrite" || len(f.callsOf(engine.SecretSaveDataCommand)) != 1 {
		t.Fatalf("the conflict: button %q", sd.review.save.Text)
	}
	settle(w)
	test.Tap(sd.review.save)
	q.until(t, "the save over", func() bool { return sd.review == nil && strings.HasPrefix(sd.status.text, "Saved") })
	if s.values["DB_PASSWORD"] != secTyped+"-2" {
		t.Error("the save over the newer version did not write")
	}
}

// TestSecretDrawerReadOnly: where the save is not permitted, Show reveals the value read-only as
// before (no editor) and the line under the data says why.
func TestSecretDrawerReadOnly(t *testing.T) {
	f, _ := secretEditFake(t)
	f.disabled[engine.SecretSaveDataCommand] = "not permitted: update secrets in namespace checkout"
	sh, _, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerButtons(sh.drawer)["Show"]) == 4 })
	test.Tap(drawerButtons(sh.drawer)["Show"][2]) // DB_PASSWORD
	q.until(t, "the value", func() bool { return drawerHasText(sh.drawer, fixtureSecretValues["DB_PASSWORD"]) })
	sd := secDataOf(sh)
	if len(sd.rows) != 0 || sd.buttons.Visible() || sd.status.text != "Read-only: not permitted: update secrets in namespace checkout" {
		t.Errorf("read-only: %d editors, buttons %v, status %q", len(sd.rows), sd.buttons.Visible(), sd.status.text)
	}
	if calls := f.callsOf(engine.RevealCommand); strings.Contains(string(calls[0].Params), `"whole"`) {
		t.Errorf("a read-only reveal asked for the whole value: %s", calls[0].Params)
	}
}

// TestSecretEditorCopiesAsASecret: a copy from a value's editor goes to the clipboard as a Secret's
// value: cleared after the time, a toast that says so and holds no value.
func TestSecretEditorCopiesAsASecret(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	sh.secretClipTTL = 40 * time.Millisecond
	r := showKey(t, sh, q, "DB_PASSWORD")
	r.view.SelectAll()
	r.view.TypedShortcut(&fyne.ShortcutCopy{Clipboard: sh.app.Clipboard()})
	if got := sh.app.Clipboard().Content(); got != fixtureSecretValues["DB_PASSWORD"] {
		t.Fatalf("the clipboard holds %q", got)
	}
	if !toastSays(sh, "cleared in") {
		t.Error("no toast says the clipboard is cleared")
	}
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}
}

// TestSecretEditorCopyIcon (the owner's audit decision of 2026-10-16, D-59 (c)): a value in an
// editor has a copy icon in its key's line, above the editor's right end and over none of its
// text; a click copies the whole value as typed so far through the clipboard for Secret values
// (concealed, cleared after the time), never the plain clipboard, with the copy icons' toast and
// checkmark. An empty value copies nothing; a masked value has no pinned icon (the drawer's icon on
// hover copies it without showing it, TestSecretEditorMaskedValueCopiedWithoutShowing); an added
// key's editor has one.
func TestSecretEditorCopyIcon(t *testing.T) {
	sh, w, q, f, _ := secShell(t, ThemeDark)
	sh.secretClipTTL = 40 * time.Millisecond
	var concealed []string
	sh.setConcealed = func(text string) bool { // the system clipboard's concealed path (H-25)
		concealed = append(concealed, text)
		sh.app.Clipboard().SetContent(text)
		return true
	}
	sd := secDataOf(sh)
	if n := len(copyIcons(sd.list)); n != 4 { // one in each text key's line (RW1)
		t.Fatalf("%d copy icons before a value is in an editor, want 4", n)
	}
	r := showKey(t, sh, q, "DB_PASSWORD")
	settle(w)
	if icons := copyIcons(sd.list); len(icons) != 4 || !slices.Contains(icons, r.copyBtn) || !r.copyBtn.Visible() {
		t.Fatalf("copy icons %d, the editor's shown %v", len(icons), r.copyBtn != nil && r.copyBtn.Visible())
	}
	ip, iq := absRect(r.copyBtn)
	ep, eq := absRect(r.frame)
	if iq.Y > ep.Y || ip.X < (ep.X+eq.X)/2 || iq.X > eq.X {
		t.Errorf("the icon at %v–%v, the editor at %v–%v: want it above the editor's right end", ip, iq, ep, eq)
	}
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(r.copyBtn))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == "Copy the whole value" })
	test.MoveMouse(w.Canvas(), ep.AddXY(5, 5))
	if !r.copyBtn.Visible() {
		t.Error("the icon hid when the pointer left it")
	}

	test.Tap(r.copyBtn)
	if len(concealed) != 1 || concealed[0] != fixtureSecretValues["DB_PASSWORD"] {
		t.Fatalf("the icon copied %d values through the concealed clipboard", len(concealed))
	}
	if !r.copyBtn.done || !toastSays(sh, "cleared in") {
		t.Errorf("the copy's checkmark %v, the toast says it is cleared %v", r.copyBtn.done, toastSays(sh, "cleared in"))
	}
	q.until(t, "the clipboard to be cleared", func() bool { return sh.app.Clipboard().Content() == "" })

	typeIntoRow(r.cmRow, secTyped) // the value as typed
	test.Tap(r.copyBtn)
	if len(concealed) != 2 || concealed[1] != secTyped {
		t.Fatalf("the edited value was not copied whole (%d copies)", len(concealed))
	}
	typeIntoRow(r.cmRow, "")
	sh.app.Clipboard().SetContent("the user's own")
	test.Tap(r.copyBtn)
	if len(concealed) != 2 || sh.app.Clipboard().Content() != "the user's own" || !toastSays(sh, "nothing was copied") {
		t.Errorf("an empty value: %d copies, the clipboard %q", len(concealed), sh.app.Clipboard().Content())
	}

	sd.hide("DB_PASSWORD") // the edited value masked: its icon copies without showing (RW1)
	q.until(t, "the masked value", func() bool { return r.masked && r.copyBtn.tip == maskedCopyTip })

	addKeyThrough(t, sd.adder, "API_TOKEN")
	a := sd.addedRows["API_TOKEN"]
	typeIntoRow(a.cmRow, secTyped+"-new")
	q.until(t, "the added key's icon", func() bool {
		icons := copyIcons(sd.list)
		return len(icons) == 5 && slices.Contains(icons, a.copyBtn)
	})
	test.Tap(a.copyBtn)
	if len(concealed) != 3 || concealed[2] != secTyped+"-new" {
		t.Errorf("the added key's value was not copied (%d copies)", len(concealed))
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}
}

// TestSecretEditorMaskedValueCopiedWithoutShowing (the owner's decision of 2026-10-22, D-25, D-59):
// where the values can be saved, a masked value's copy icon reads the whole value and copies it
// without showing it, and opens no editor; a value edited and hidden again copies what was typed
// from the editor's memory, still masked; an added key hidden again likewise. No value reaches a
// toast or a command but the GUI-only reveal.
func TestSecretEditorMaskedValueCopiedWithoutShowing(t *testing.T) {
	sh, _, q, f, _ := secShell(t, ThemeDark)
	sh.secretClipTTL = time.Minute
	concealed := concealedClipboard(sh)
	d, sd := sh.drawer, secDataOf(sh)

	if icon := keyCopyIcon(t, d, "DB_PASSWORD"); !icon.Visible() || icon.tip != maskedCopyTip {
		t.Fatalf("a masked key's copy icon: shown %v, its tooltip %q", icon.Visible(), icon.tip)
	}
	test.Tap(keyCopyIcon(t, d, "DB_PASSWORD"))
	q.until(t, "the copy", func() bool { return len(*concealed) == 1 })
	if (*concealed)[0] != fixtureSecretValues["DB_PASSWORD"] {
		t.Errorf("copied %d bytes, want DB_PASSWORD's", len((*concealed)[0]))
	}
	if calls := f.callsOf(engine.RevealCommand); len(calls) != 1 || !strings.Contains(string(calls[0].Params), `"whole":true`) {
		t.Errorf("reveal calls %+v, want one for the whole value", calls)
	}
	if len(sd.rows) != 0 || len(drawerButtons(d)["Hide"]) != 0 || drawerHasText(d, "correct-horse") {
		t.Error("the copy opened an editor or showed the value")
	}
	if !toastSays(sh, "DB_PASSWORD") || !toastSays(sh, "cleared in") {
		t.Error("the toast does not name the key or the clearing")
	}

	// An edited value hidden again: the typed text, from the window's memory, no reveal.
	r := showKey(t, sh, q, "DB_USER")
	typeIntoRow(r.cmRow, secTyped)
	sd.hide("DB_USER")
	q.until(t, "the edited value masked", func() bool { return r.masked && drawerHasText(d, "(edited, not saved)") })
	if icon := keyCopyIcon(t, d, "DB_USER"); icon.tip != maskedCopyTip {
		t.Fatalf("an edited masked value's copy icon says %q", icon.tip)
	}
	reveals := len(f.callsOf(engine.RevealCommand))
	test.Tap(keyCopyIcon(t, d, "DB_USER"))
	if len(*concealed) != 2 || (*concealed)[1] != secTyped {
		t.Fatalf("the edited value was not copied as typed (%d copies)", len(*concealed))
	}
	if len(f.callsOf(engine.RevealCommand)) != reveals {
		t.Error("copying a value the editor holds read it from the engine again")
	}
	if !r.masked || drawerHasText(d, secTyped) {
		t.Error("the copy showed the edited value")
	}

	// An added key hidden again: what was typed into it.
	addKeyThrough(t, sd.adder, "API_TOKEN")
	a := sd.addedRows["API_TOKEN"]
	typeIntoRow(a.cmRow, secTyped+"-new")
	sd.hide("API_TOKEN")
	q.until(t, "the added key masked", func() bool { return a.masked && keyLineOf(d, "API_TOKEN") != nil })
	test.Tap(keyCopyIcon(t, d, "API_TOKEN"))
	if len(*concealed) != 3 || (*concealed)[2] != secTyped+"-new" {
		t.Errorf("the added key's value was not copied (%d copies)", len(*concealed))
	}
	if l := valueLeaks(sh, f); l != "" {
		t.Errorf("%q in a toast or the notifications", l)
	}
}

// TestConfigMapEditorsHaveNoCopyIcon: the copy icon over an editor is a Secret's (D-59); a Config
// Map's editors, an added key's included, copy with ⌘C only.
func TestConfigMapEditorsHaveNoCopyIcon(t *testing.T) {
	sh, w, q, _, _ := cmShell(t, ThemeDark)
	cd := cmDataOf(sh)
	addKeyThrough(t, cd.adder, "enable-ipv6")
	q.until(t, "the added key", func() bool { return cd.addedRows["enable-ipv6"] != nil })
	settle(w)
	if len(cd.rows) == 0 || len(findAll[*yamlview.View](cd.root)) < len(cd.rows) {
		t.Fatalf("%d rows, %d editors", len(cd.rows), len(findAll[*yamlview.View](cd.root)))
	}
	if n := len(findAll[*copyButton](cd.root)); n != 0 {
		t.Errorf("%d copy icons in a Config Map's data", n)
	}
}

// typeIntoRow replaces a row's value as typing does.
func typeIntoRow(r *cmRow, text string) {
	r.view.SelectAll()
	r.view.ReplaceSelection(text)
}

// renderSecretEdit renders the Secrets list with checkout-db's drawer: one value in its editor,
// edited, another masked with Show, Revert and Save at the end.
func renderSecretEdit(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := secShell(t, mode)
	q.until(t, "the list", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	r := showKey(t, sh, q, "DB_PASSWORD")
	typeIntoRow(r.cmRow, secTyped)
	settle(w)
	savePNG(t, w, name)
}

func TestRenderSecretEditDark(t *testing.T) { renderSecretEdit(t, ThemeDark, "secret_edit_dark.png") }
func TestRenderSecretEditLight(t *testing.T) {
	renderSecretEdit(t, ThemeLight, "secret_edit_light.png")
}

// renderSecretSave renders the review of a Secret's save: the masked diff and the dry run.
func renderSecretSave(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := secShell(t, mode)
	q.until(t, "the list", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	r := showKey(t, sh, q, "DB_PASSWORD")
	typeIntoRow(r.cmRow, secTyped)
	u := showKey(t, sh, q, "DB_USER")
	typeIntoRow(u.cmRow, "checkout-api")
	sd := secDataOf(sh)
	test.Tap(sd.save)
	q.until(t, "the review", func() bool { return sd.review != nil && !sd.review.save.Disabled() })
	settle(w)
	savePNG(t, w, name)
}

func TestRenderSecretSaveDark(t *testing.T) { renderSecretSave(t, ThemeDark, "secret_save_dark.png") }
func TestRenderSecretSaveLight(t *testing.T) {
	renderSecretSave(t, ThemeLight, "secret_save_light.png")
}

// yamlviewView is the editor's widget type, for findAll.
type yamlviewView = yamlview.View
