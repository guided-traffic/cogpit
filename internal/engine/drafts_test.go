package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// draftDelay is the drafts' delay in these tests.
const draftDelay = 150 * time.Millisecond

// draftsSession is a session without a cluster whose drafts live in dir, kept by the process jd
// (newJournalDir plays another process).
func draftsSession(t *testing.T, dir string, jd *journalDir) (*Session, *Drafts) {
	t.Helper()
	d := newDrafts(dir, jd).WithDelay(draftDelay)
	s := New(Options{Drafts: d})
	t.Cleanup(s.Close)
	return s, d
}

var draftConfigMap = ObjectTarget{
	Ref:        model.ObjectRef{Context: "c", Kind: "configmaps", Namespace: "shop", Name: "settings"},
	APIVersion: "v1", Kind: "ConfigMap", Namespaced: true,
}

var draftSecret = ObjectTarget{
	Ref:        model.ObjectRef{Context: "c", Kind: "secrets", Namespace: "shop", Name: "db"},
	APIVersion: "v1", Kind: "Secret", Namespaced: true,
}

const configMapText = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: shop\n  resourceVersion: \"41\"\ndata:\n  mode: fast\n"

// openDraftTab puts an editor tab into the dock as the engine's openers do, without a cluster: an
// Edit tab on target based on text at version 41, or a tab of another type holding text.
func openDraftTab(t *testing.T, s *Session, typ DockType, text string, target ObjectTarget) string {
	t.Helper()
	e := &editor{tab: DockTab{ID: s.NewDockID(typ), Type: typ, Title: "Tab", Context: "c"}, text: text, rev: 1}
	switch typ {
	case DockEdit:
		r := target.Ref
		e.tab.Ref, e.tab.Title = &r, target.Kind+" "+target.Ref.Name
		e.target, e.baseRV, e.baseUID, e.baseText = target, "41", "uid-1", text
	case DockCreate:
		e.tab.Title, e.startText = createTabTitle, text
	}
	s.putEditor(e)
	ui := s.UpdateUI(func(u *UIState) {
		u.Dock = append(u.Dock, e.tab)
		u.ActiveDock = e.tab.ID
	})
	s.settleEditor(e.tab.ID, true, ui.Revision)
	return e.tab.ID
}

func setText(t *testing.T, s *Session, id, text string) {
	t.Helper()
	if _, err := s.SetEditorText(id, text); err != nil {
		t.Fatal(err)
	}
}

// draftFiles reads the draft files in dir.
func draftFiles(t *testing.T, dir string) []Draft {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, DraftsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []Draft
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), draftSuffix) {
			continue
		}
		path := filepath.Join(dir, DraftsDir, e.Name())
		data, err := os.ReadFile(path)
		// On Windows an open fails while the drafts replace or remove the file: tried again.
		for try := 0; err != nil && heldOpenElsewhere(err) && try < 100; try++ {
			time.Sleep(time.Millisecond)
			data, err = os.ReadFile(path)
		}
		if errors.Is(err, os.ErrNotExist) { // removed since the listing
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var dr Draft
		if err := json.Unmarshal(data, &dr); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, dr)
	}
	return out
}

// waitDrafts waits until the draft files satisfy ok.
func waitDrafts(t *testing.T, dir, what string, ok func([]Draft) bool) []Draft {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []Draft
	if !testkit.WaitFor(ctx, func() bool { got = draftFiles(t, dir); return ok(got) }) {
		t.Fatalf("%s: draft files %+v", what, got)
	}
	return got
}

// noDraftsFor checks that no draft file appears within a few delays.
func noDraftsFor(t *testing.T, dir, what string) {
	t.Helper()
	time.Sleep(4 * draftDelay)
	if got := draftFiles(t, dir); len(got) != 0 {
		t.Fatalf("%s: %d draft files, want none: %+v", what, len(got), got[0].Title)
	}
}

// TestDraftWrittenAfterTypingStops: a Create resource tab's text is written as a draft once it has
// not changed for the delay, privately (0600 in a 0700 folder) and whole, naming this process; a
// text that is the one the tab opened with again has no draft.
func TestDraftWrittenAfterTypingStops(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, d := draftsSession(t, dir, newJournalDir(dir))
	id := openDraftTab(t, s, DockCreate, "apiVersion: v1\nkind: Pod\n", ObjectTarget{})
	if got := draftFiles(t, dir); len(got) != 0 {
		t.Fatalf("a tab with the text it opened with has a draft: %+v", got)
	}

	var last string
	var gap time.Duration // the longest pause between two changes; a loaded machine may stretch it
	prev := time.Now()
	for i := range 5 { // typing: a change every 37 ms, less than the delay
		last = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-" + strings.Repeat("x", i) + "\n"
		setText(t, s, id, last)
		gap, prev = max(gap, time.Since(prev)), time.Now()
		time.Sleep(draftDelay / 4)
	}
	if got := draftFiles(t, dir); len(got) != 0 && max(gap, time.Since(prev)) < draftDelay {
		t.Fatalf("a draft was written while the text kept changing: %+v", got)
	}
	got := waitDrafts(t, dir, "the draft after typing stopped", func(l []Draft) bool { return len(l) == 1 })
	dr := got[0]
	if dr.Text != last || dr.Type != DockCreate || dr.Context != "c" || dr.Title != createTabTitle || !validOwnerID(dr.ID) || dr.Version != draftVersion {
		t.Errorf("draft %+v", dr)
	}
	if dr.Owner == nil || dr.Owner.ID != d.jd.selfID() || dr.Owner.PID != os.Getpid() {
		t.Errorf("the draft names owner %+v", dr.Owner)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, DraftsDir, dr.ID+draftSuffix))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("draft file mode %v, %v", fi.Mode(), err)
		}
		di, err := os.Stat(filepath.Join(dir, DraftsDir))
		if err != nil || di.Mode().Perm() != 0o700 {
			t.Errorf("drafts folder mode %v, %v", di.Mode(), err)
		}
	}
	// This session's own open tab is not offered.
	if list, err := s.EditorDrafts(true); err != nil || len(list) != 0 {
		t.Errorf("drafts offered while their tab is open: %+v, %v", list, err)
	}

	setText(t, s, id, "apiVersion: v1\nkind: Pod\n")
	waitDrafts(t, dir, "the draft of a text back to its start", func(l []Draft) bool { return len(l) == 0 })
}

// TestDraftRemovedWhenSavedOrClosed: an Edit tab's draft goes when its text is the object it is
// based on again (an apply sets both), and a tab's draft goes when the tab is closed.
func TestDraftRemovedWhenSavedOrClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := draftsSession(t, dir, newJournalDir(dir))
	edit := openDraftTab(t, s, DockEdit, configMapText, draftConfigMap)
	changed := strings.Replace(configMapText, "mode: fast", "mode: slow", 1)
	setText(t, s, edit, changed)
	got := waitDrafts(t, dir, "the Edit tab's draft", func(l []Draft) bool { return len(l) == 1 })
	if dr := got[0]; dr.Type != DockEdit || dr.Ref == nil || !dr.Ref.Same(draftConfigMap.Ref) || dr.BaseResourceVersion != "41" ||
		dr.BaseUID != "uid-1" || dr.APIVersion != "v1" || dr.Kind != "ConfigMap" || dr.Text != changed || dr.Title != "ConfigMap settings" {
		t.Errorf("edit draft %+v", dr)
	}
	// An apply makes the stored object the text and the base (ApplyEdit's update).
	if _, err := s.updateEditor(edit, func(e *editor) { e.baseRV, e.baseText = "42", e.text; e.rev++ }); err != nil {
		t.Fatal(err)
	}
	waitDrafts(t, dir, "the draft after the apply", func(l []Draft) bool { return len(l) == 0 })

	create := openDraftTab(t, s, DockCreate, "kind: Pod\n", ObjectTarget{})
	setText(t, s, create, "kind: Pod\nmetadata: {name: a}\n")
	setText(t, s, edit, strings.Replace(changed, "mode: slow", "mode: medium", 1))
	waitDrafts(t, dir, "two drafts", func(l []Draft) bool { return len(l) == 2 })
	s.UpdateUI(func(u *UIState) {
		u.Dock = slices.DeleteFunc(u.Dock, func(x DockTab) bool { return x.ID == create })
	})
	got = waitDrafts(t, dir, "the closed tab's draft gone", func(l []Draft) bool { return len(l) == 1 })
	if got[0].Type != DockEdit {
		t.Errorf("the open tab's draft went: %+v", got[0])
	}
	// A tab closed while its draft waits for the delay leaves none.
	setText(t, s, edit, changed+"# more\n")
	s.UpdateUI(func(u *UIState) { u.Dock = nil })
	noDraftsFor(t, dir, "a tab closed with its draft waiting")
}

// TestNoDraftsForSecretsOrHelmValues (D-19): a Secret's Edit tab, a Create tab whose text holds a
// Secret or may be one being typed, and the Helm Upgrade and Install tabs never have a draft; a
// draft whose text became a Secret's is removed.
func TestNoDraftsForSecretsOrHelmValues(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := draftsSession(t, dir, newJournalDir(dir))
	const value = "aHVudGVyMg=="
	secretText := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: shop\ndata:\n  password: " + value + "\n"

	sec := openDraftTab(t, s, DockEdit, secretText, draftSecret)
	setText(t, s, sec, strings.Replace(secretText, value, "bmV3", 1))
	create := openDraftTab(t, s, DockCreate, "", ObjectTarget{})
	for _, text := range []string{
		secretText,
		"apiVersion: v1\nkind: Sec\ndata:\n  password: " + value + "\n",            // still being typed
		"apiVersion: v1\nkind: List\nitems:\n- kind: Secret\n  data: {password: x", // a list that does not parse
		`{"apiVersion": "v1", "kind": "secret", "stringData": {"password": "x"}`,   // JSON, any case
	} {
		setText(t, s, create, text)
	}
	for _, typ := range []DockType{DockHelmUpgrade, DockHelmInstall} {
		h := openDraftTab(t, s, typ, "password: one\n", ObjectTarget{})
		setText(t, s, h, "password: two\n")
	}
	noDraftsFor(t, dir, "Secrets and Helm values")

	// A draft whose text turns into a Secret's goes.
	setText(t, s, create, "apiVersion: v1\nkind: ConfigMap\ndata:\n  a: b\n")
	waitDrafts(t, dir, "the ConfigMap's draft", func(l []Draft) bool { return len(l) == 1 })
	setText(t, s, create, secretText)
	waitDrafts(t, dir, "the draft that became a Secret", func(l []Draft) bool { return len(l) == 0 })

	for _, text := range []string{"kind: Service\n", "kind: StatefulSet\n", "kind: ServiceAccount\n", "kind: SecretProviderClass\n"} {
		if textMayHoldSecret(text) {
			t.Errorf("%q counts as a Secret", text)
		}
	}
	for _, text := range []string{"kind: S\n", "kind: secretlist\n", "  - kind: 'Secret'\n", "{kind: Secret}"} {
		if !textMayHoldSecret(text) {
			t.Errorf("%q does not count as a Secret", text)
		}
	}
}

// TestDraftsFlushedOnClose: a quit writes the drafts still waiting for their delay.
func TestDraftsFlushedOnClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	d := newDrafts(dir, newJournalDir(dir)).WithDelay(time.Hour)
	s := New(Options{Drafts: d})
	id := openDraftTab(t, s, DockCreate, "kind: Pod\n", ObjectTarget{})
	setText(t, s, id, "kind: Pod\nmetadata: {name: quit}\n")
	s.Close()
	if got := draftFiles(t, dir); len(got) != 1 || got[0].Text != "kind: Pod\nmetadata: {name: quit}\n" {
		t.Fatalf("drafts after a quit: %+v", got)
	}
}

// TestDraftsOfAnotherProcess (H-22): a draft whose process still runs belongs to its tab there: it
// is neither offered nor discarded here. Once that process ended it is offered, and discarding
// deletes it.
func TestDraftsOfAnotherProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	ja := newJournalDir(dir)
	a, _ := draftsSession(t, dir, ja)
	id := openDraftTab(t, a, DockCreate, "kind: Pod\n", ObjectTarget{})
	setText(t, a, id, "kind: Pod\nmetadata: {name: a}\n")
	got := waitDrafts(t, dir, "a's draft", func(l []Draft) bool { return len(l) == 1 })
	defer runtime.KeepAlive(ja) // a lives until the test says otherwise (TestDrainsOfAnotherProcess)

	b, _ := draftsSession(t, dir, newJournalDir(dir))
	if list, err := b.EditorDrafts(true); err != nil || len(list) != 0 {
		t.Fatalf("b offers a's open draft: %+v, %v", list, err)
	}
	if err := b.DiscardDraft(got[0].ID); err == nil || !strings.Contains(err.Error(), "another Cogpit window") {
		t.Errorf("b discarded a's open draft: %v", err)
	}

	_ = ja.owner.f.Close() // a ends
	list, err := b.EditorDrafts(true)
	if err != nil || len(list) != 1 || list[0].ID != got[0].ID || list[0].Type != DockCreate || list[0].Lines != 2 || list[0].Bytes != len(got[0].Text) {
		t.Fatalf("b's drafts after a ended: %+v, %v", list, err)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "name: a") {
		t.Errorf("the listing holds the text: %s", raw)
	}
	if _, err := b.EditorDrafts(false); !errors.Is(err, ErrNotConnected) {
		t.Errorf("the connected context's drafts without a cluster: %v", err)
	}
	if err := b.DiscardDraft(got[0].ID); err != nil {
		t.Fatal(err)
	}
	if left := draftFiles(t, dir); len(left) != 0 {
		t.Errorf("after discarding: %+v", left)
	}
	if err := b.DiscardDraft(got[0].ID); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("discarding again: %v", err)
	}
	if err := b.DiscardDraft("../../etc/passwd"); !errors.Is(err, ErrDraftNotFound) {
		t.Errorf("a path as a draft id: %v", err)
	}
}

// TestDraftFilesReadBack: a listing skips what is not a draft and removes a draft that holds a
// Secret's values, which Cogpit never writes.
func TestDraftFilesReadBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s, _ := draftsSession(t, dir, newJournalDir(dir))
	folder := filepath.Join(dir, DraftsDir)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	put := func(name string, v any) {
		data, ok := v.([]byte)
		if !ok {
			data, _ = json.Marshal(v)
		}
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	good := Draft{Version: 1, ID: strings.Repeat("a", 32), Type: DockCreate, Context: "c", Title: createTabTitle, Text: "kind: Pod\n", Saved: time.Now()}
	put(good.ID+draftSuffix, good)
	secret := Draft{Version: 1, ID: strings.Repeat("b", 32), Type: DockCreate, Context: "c", Text: "kind: Secret\nstringData: {a: b}\n"}
	put(secret.ID+draftSuffix, secret)
	secretEdit := Draft{Version: 1, ID: strings.Repeat("c", 32), Type: DockEdit, Context: "c", Text: "data: {}\n",
		Ref: &model.ObjectRef{Kind: "secrets", Namespace: "shop", Name: "db"}, APIVersion: "v1", Kind: "Secret"}
	put(secretEdit.ID+draftSuffix, secretEdit)
	put(strings.Repeat("d", 32)+draftSuffix, []byte("{not json"))
	put(strings.Repeat("e", 32)+draftSuffix, Draft{ID: strings.Repeat("f", 32), Type: DockCreate, Context: "c"}) // names another id
	put(strings.Repeat("0", 32)+draftSuffix, Draft{ID: strings.Repeat("0", 32), Type: DockHelmUpgrade, Context: "c"})
	put("notes.txt", []byte("hello"))
	put("short"+draftSuffix, good)

	list, err := s.EditorDrafts(true)
	if err != nil || len(list) != 1 || list[0].ID != good.ID {
		t.Fatalf("drafts: %+v, %v", list, err)
	}
	for _, id := range []string{secret.ID, secretEdit.ID} {
		if _, err := os.Stat(filepath.Join(folder, id+draftSuffix)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a draft holding a Secret was kept: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "notes.txt")); err != nil {
		t.Errorf("a file that is not a draft was touched: %v", err)
	}
}
