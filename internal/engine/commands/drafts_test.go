package commands_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// draftFileCount counts the draft files in a Cogpit folder.
func draftFileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, engine.DraftsDir))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func waitDraftFiles(t *testing.T, dir string, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return draftFileCount(t, dir) == want }) {
		t.Fatalf("%d draft files, want %d", draftFileCount(t, dir), want)
	}
}

// TestDraftRestoredAfterARestart: an Edit tab's unsaved text outlives the process that had it; the
// next start offers it when the context connects, an agent lists it without its text, and Restore
// opens the tab again with the text, based on the version the text was based on: after a change
// on the cluster meanwhile the apply is refused with a conflict before it overwrites anything,
// and once applied the draft is gone.
func TestDraftRestoredAfterARestart(t *testing.T) {
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h1, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir)})
	var opened commands.EditResult
	h1.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &opened)
	edited := strings.Replace(opened.Text, "clusterIP: 10.96.100.100", "clusterIP: 10.96.100.100\n  sessionAffinity: ClientIP", 1)
	h1.must(t, gui, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	h1.s.Close() // the quit writes the draft still waiting for its delay
	if n := draftFileCount(t, dir); n != 1 {
		t.Fatalf("%d draft files after the quit", n)
	}

	// The next start.
	h2, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir)})
	offers := make(chan string, 4)
	h2.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventDrafts && ev.Message != "" {
			select { // never block the engine's goroutine that emits
			case offers <- ev.Message:
			default:
			}
		}
	})
	if err := h2.s.Disconnect(); err != nil { // a connect again would only show the open connection (D-69)
		t.Fatal(err)
	}
	if err := h2.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-offers:
		if msg != "Cogpit kept the unsaved text of Service checkout (namespace shop) in context test" {
			t.Errorf("offer %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the draft was not offered on connect")
	}
	if _, err := h2.s.Objects(context.Background(), "services", nil); err != nil { // warm again after the reconnect
		t.Fatal(err)
	}

	var list []engine.DraftInfo
	h2.must(t, agent, "editor.drafts", map[string]any{}, &list)
	if len(list) != 1 || list[0].Type != engine.DockEdit || list[0].Title != "Service checkout" || list[0].Ref == nil ||
		!list[0].Ref.Same(checkout) || list[0].Context != "test" || list[0].BaseResourceVersion != opened.BaseResourceVersion {
		t.Fatalf("drafts %+v", list)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), "sessionAffinity") || strings.Contains(string(raw), "clusterIP") {
		t.Errorf("an agent's listing holds the text: %s", raw)
	}

	// Someone else changes the object before the user restores the draft.
	obj, err := h2.s.Lookup(checkout)
	if err != nil {
		t.Fatal(err)
	}
	bump(h2.src, testkit.ServicesGVR, obj, "changed-by", "someone-else")
	newer := cachedRV(t, h2, checkout, obj.GetResourceVersion())

	var st engine.EditorState
	h2.must(t, agent, "editor.restoreDraft", map[string]any{"id": list[0].ID}, &st)
	if st.Tab.Type != engine.DockEdit || st.Text != edited || st.BaseResourceVersion != opened.BaseResourceVersion || !st.Stale || st.CachedResourceVersion != newer {
		t.Fatalf("restored tab %+v", st)
	}
	if u := h2.s.UIState(); u.ActiveDock != st.Tab.ID || len(u.Dock) != 1 {
		t.Errorf("dock %+v active %s", u.Dock, u.ActiveDock)
	}
	h2.must(t, agent, "editor.drafts", map[string]any{}, &list)
	if len(list) != 0 {
		t.Errorf("a restored draft is still offered: %+v", list)
	}
	if err := h2.run(t, gui, "editor.restoreDraft", map[string]any{"id": st.Tab.ID}, nil); err == nil {
		t.Error("a tab id restored as a draft")
	}

	err = h2.run(t, gui, "object.apply", map[string]any{"id": st.Tab.ID, "yaml": edited}, nil)
	var ce *engine.ConflictError
	if !errors.As(err, &ce) || ce.CurrentResourceVersion != newer {
		t.Fatalf("an apply over a change made since the draft: %v", err)
	}
	if now, _ := h2.s.Lookup(checkout); now.GetAnnotations()["changed-by"] != "someone-else" {
		t.Fatal("the other change was overwritten")
	}
	var applied engine.ApplyResult
	h2.must(t, gui, "object.apply", map[string]any{"id": st.Tab.ID, "yaml": edited}, &applied)
	h2.s.Close() // the saved text needs no draft: the quit removes it
	if n := draftFileCount(t, dir); n != 0 {
		t.Errorf("%d draft files after the apply", n)
	}
}

// TestRestoreIntoTheOpenEditTab: a draft goes into the Edit tab already open on its object unless
// that tab has unsaved changes; a draft of another context waits for that context.
func TestRestoreIntoTheOpenEditTab(t *testing.T) {
	gui := engine.OriginGUI
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h1, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir)})
	var opened commands.EditResult
	h1.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &opened)
	edited := strings.Replace(opened.Text, "type: ClusterIP", "type: NodePort", 1)
	h1.must(t, gui, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": edited}, nil)
	h1.s.Close()

	h2, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir)})
	var list []engine.DraftInfo
	h2.must(t, gui, "editor.drafts", map[string]any{}, &list)
	if len(list) != 1 {
		t.Fatalf("drafts %+v", list)
	}
	var tab commands.EditResult
	h2.must(t, gui, "object.edit", map[string]any{"ref": checkout}, &tab)
	h2.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": tab.Text + "# mine\n"}, nil)
	if err := h2.run(t, gui, "editor.restoreDraft", map[string]any{"id": list[0].ID}, nil); err == nil || !strings.Contains(err.Error(), "unsaved changes of its own") {
		t.Fatalf("a restore over unsaved text: %v", err)
	}
	h2.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": tab.Text}, nil)
	var st engine.EditorState
	h2.must(t, gui, "editor.restoreDraft", map[string]any{"id": list[0].ID}, &st)
	if st.Tab.ID != tab.Tab.ID || st.Text != edited || len(h2.s.UIState().Dock) != 1 {
		t.Errorf("restored into %+v", st)
	}
	// Closing the tab discards the restored text, and its draft with it.
	h2.must(t, gui, "dock.close", map[string]any{"id": tab.Tab.ID}, nil)
	waitDraftFiles(t, dir, 0)
}

// TestCreateTabDraft: a Create resource tab's text is kept once it differs from the template it
// opened with, and goes when the text is created in full or the tab is closed.
func TestCreateTabDraft(t *testing.T) {
	gui := engine.OriginGUI
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(50 * time.Millisecond)})
	var tab commands.EditResult
	h.must(t, gui, "resource.createTab", map[string]any{"template": "ConfigMap"}, &tab)
	time.Sleep(200 * time.Millisecond)
	if n := draftFileCount(t, dir); n != 0 {
		t.Fatalf("a template has a draft (%d files)", n)
	}
	text := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: drafted\n  namespace: shop\ndata:\n  a: \"1\"\n"
	h.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": text}, nil)
	waitDraftFiles(t, dir, 1)
	var res engine.CreateResult
	h.must(t, gui, "object.create", map[string]any{"yaml": text}, &res)
	if res.Created != 1 {
		t.Fatalf("create %+v", res)
	}
	waitDraftFiles(t, dir, 0)

	h.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": text + "  b: \"2\"\n"}, nil)
	waitDraftFiles(t, dir, 1)
	h.must(t, gui, "dock.close", map[string]any{"id": tab.Tab.ID}, nil)
	waitDraftFiles(t, dir, 0)

	// A restored Create tab: the text is unsaved work again.
	h.must(t, gui, "resource.createTab", map[string]any{}, &tab)
	h.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": text}, nil)
	h.s.Close()
	h2, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(50 * time.Millisecond)})
	var list []engine.DraftInfo
	h2.must(t, gui, "editor.drafts", map[string]any{"all": true}, &list)
	if len(list) != 1 || list[0].Type != engine.DockCreate || list[0].Ref != nil || list[0].Title != "Create resource" {
		t.Fatalf("drafts %+v", list)
	}
	var st engine.EditorState
	h2.must(t, gui, "editor.restoreDraft", map[string]any{"id": list[0].ID}, &st)
	if st.Tab.Type != engine.DockCreate || st.Text != text {
		t.Fatalf("restored %+v", st)
	}
	h2.s.Close()
	if n := draftFileCount(t, dir); n != 1 {
		t.Errorf("the restored Create tab's draft: %d files", n)
	}
}

// TestNoDraftsOfSecretTabs (D-19): the user's Edit tab of a Secret and a Create tab holding a
// Secret's manifest leave nothing on disk, not even after a quit.
func TestNoDraftsOfSecretTabs(t *testing.T) {
	gui := engine.OriginGUI
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h, _ := editHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(50 * time.Millisecond)})
	h.src.Set(secretsGVR, testkit.Object("v1", "Secret", "shop", "db", map[string]any{
		"data": map[string]any{"password": "aHVudGVyMg=="},
	}))
	var opened commands.EditResult
	h.must(t, gui, "object.edit", map[string]any{"ref": dbSecret}, &opened)
	h.must(t, gui, "editor.setText", map[string]any{"id": opened.Tab.ID, "yaml": strings.Replace(opened.Text, "aHVudGVyMg==", "bmV3", 1)}, nil)
	var tab commands.EditResult
	h.must(t, gui, "resource.createTab", map[string]any{}, &tab)
	h.must(t, gui, "editor.setText", map[string]any{"id": tab.Tab.ID, "yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: x\nstringData:\n  token: abc\n"}, nil)
	time.Sleep(300 * time.Millisecond)
	h.s.Close()
	if n := draftFileCount(t, dir); n != 0 {
		t.Fatalf("%d draft files of Secrets", n)
	}
}

// TestDraftCommandsWithoutDrafts: a session that keeps no drafts (the CLI's) disables them.
func TestDraftCommandsWithoutDrafts(t *testing.T) {
	h, _ := editHarness(t, engine.Options{})
	var dis *engine.DisabledError
	draft := map[string]any{"id": strings.Repeat("a", 32)}
	for id, params := range map[string]any{"editor.drafts": map[string]any{}, "editor.restoreDraft": draft, "editor.discardDraft": draft} {
		if err := h.run(t, engine.OriginGUI, id, params, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "keeps no drafts") {
			t.Errorf("%s: %v", id, err)
		}
	}
}
