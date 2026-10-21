package commands_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A Config Map's values edited in the drawer and not saved (the owner's answer of 2026-10-15): the
// window keeps them through configMap.keepEdits, in memory and as a draft on disk; a restart
// offers the draft when the context connects and editor.restoreDraft opens the drawer on the
// Config Map with them; only the window keeps or reads them.

// cmAppIn is cilium-config of the harness's context "test", as the drawer names it.
var cmAppIn = func() any {
	r := cmApp
	r.Context = "test"
	return r
}()

// draftFiles reads the draft files of a Cogpit folder.
func draftFiles(t *testing.T, dir string) []engine.Draft {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, engine.DraftsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []engine.Draft
	for _, e := range entries {
		path := filepath.Join(dir, engine.DraftsDir, e.Name())
		assertMode(t, path, 0o600)
		raw, err := os.ReadFile(path)
		// On Windows an open fails while the drafts replace the file: tried again.
		for try := 0; err != nil && runtime.GOOS == "windows" && try < 100; try++ {
			time.Sleep(time.Millisecond)
			raw, err = os.ReadFile(path)
		}
		if err != nil {
			t.Fatal(err)
		}
		var dr engine.Draft
		if err := json.Unmarshal(raw, &dr); err != nil {
			t.Fatal(err)
		}
		out = append(out, dr)
	}
	return out
}

// TestConfigMapDrawerDrafts: the window's unsaved values are kept in memory and on disk, an older
// call of the window's is ignored, no values drop them; agents and the CLI can neither keep nor
// read them; a restart offers the draft, an agent lists it without a value, and restoring it opens
// the drawer on the Config Map with the values back in the window's read.
func TestConfigMapDrawerDrafts(t *testing.T) {
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h1, _ := configMapHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(20 * time.Millisecond)})
	var data engine.ConfigMapData
	h1.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if data.Unsaved != nil {
		t.Fatalf("unsaved values before any edit: %+v", data.Unsaved)
	}
	keep := func(origin engine.Origin, rev int, values map[string]any) error {
		return h1.run(t, origin, "configMap.keepEdits", map[string]any{"ref": cmAppIn, "resourceVersion": data.ResourceVersion,
			"data": values, "from": map[string]any{"enable-ipv4": "true"}, "revision": rev}, nil)
	}
	if err := keep(gui, 2, map[string]any{"enable-ipv4": "false"}); err != nil {
		t.Fatal(err)
	}
	if err := keep(gui, 1, map[string]any{"enable-ipv4": "older"}); err != nil { // overtaken by revision 2
		t.Fatal(err)
	}
	for _, origin := range []engine.Origin{agent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		if err := keep(origin, 9, map[string]any{"enable-ipv4": "planted"}); !errors.Is(err, engine.ErrConfigMapEditsGUIOnly) {
			t.Errorf("%s kept values: %v", origin, err)
		}
	}
	h1.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if u := data.Unsaved; u == nil || u.Values["enable-ipv4"] != "false" || u.From["enable-ipv4"] != "true" || u.BaseResourceVersion != data.ResourceVersion || u.Restored {
		t.Fatalf("the window's read: %+v", data.Unsaved)
	}
	var agentData engine.ConfigMapData
	h1.must(t, agent, "configMap.readData", map[string]any{"ref": cmApp}, &agentData)
	if agentData.Unsaved != nil {
		t.Errorf("an agent read the unsaved values: %+v", agentData.Unsaved)
	}
	waitDraftFiles(t, dir, 1)
	if files := draftFiles(t, dir); files[0].Type != engine.DraftConfigMapData || files[0].Values["enable-ipv4"] != "false" ||
		files[0].Ref == nil || files[0].Ref.Name != "cilium-config" || files[0].BaseResourceVersion != data.ResourceVersion {
		t.Fatalf("the draft %+v", files[0])
	}

	// Reverted: no values drop them and the draft; typed again, kept again.
	if err := keep(gui, 3, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	waitDraftFiles(t, dir, 0)
	data = engine.ConfigMapData{} // Unmarshal keeps a field the answer leaves out
	h1.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if data.Unsaved != nil {
		t.Errorf("reverted values still kept: %+v", data.Unsaved)
	}
	if err := keep(gui, 4, map[string]any{"enable-ipv4": "false"}); err != nil {
		t.Fatal(err)
	}
	waitDraftFiles(t, dir, 1)
	h1.s.Close()

	// The next start: offered on connect, listed without a value, restored into the drawer.
	h2, _ := configMapHarness(t, engine.Options{Drafts: engine.NewDrafts(dir).WithDelay(20 * time.Millisecond)})
	var list []engine.DraftInfo
	h2.must(t, agent, "editor.drafts", map[string]any{}, &list)
	if len(list) != 1 || list[0].Type != engine.DraftConfigMapData || list[0].Title != "ConfigMap cilium-config" ||
		!slices.Equal(list[0].Keys, []string{"enable-ipv4"}) || list[0].Ref == nil || list[0].Ref.Namespace != "shop" {
		t.Fatalf("drafts %+v", list)
	}
	if raw, _ := json.Marshal(list); strings.Contains(string(raw), `"false"`) {
		t.Errorf("an agent's listing holds a value: %s", raw)
	}
	var restored engine.ConfigMapDraftRestored
	h2.must(t, agent, "editor.restoreDraft", map[string]any{"id": list[0].ID}, &restored)
	if restored.Ref.Name != "cilium-config" || !slices.Equal(restored.Keys, []string{"enable-ipv4"}) || restored.BaseResourceVersion == "" {
		t.Fatalf("restored %+v", restored)
	}
	if raw, _ := json.Marshal(restored); strings.Contains(string(raw), `"false"`) {
		t.Errorf("an agent's restore returned a value: %s", raw)
	}
	if u := h2.s.UIState(); u.Drawer == nil || u.Drawer.Name != "cilium-config" || u.View != "configmaps" {
		t.Errorf("the drawer after the restore: view %s, drawer %+v", u.View, u.Drawer)
	}
	h2.must(t, gui, "configMap.readData", map[string]any{"ref": cmApp}, &data)
	if u := data.Unsaved; u == nil || u.Values["enable-ipv4"] != "false" || !u.Restored {
		t.Fatalf("the window's read after the restore: %+v", data.Unsaved)
	}
	h2.must(t, agent, "editor.drafts", map[string]any{}, &list)
	if len(list) != 0 {
		t.Errorf("a restored draft is still offered: %+v", list)
	}
	// Saved in the window: no values, no draft.
	if err := h2.run(t, gui, "configMap.keepEdits", map[string]any{"ref": cmAppIn, "revision": 1}, nil); err != nil {
		t.Fatal(err)
	}
	waitDraftFiles(t, dir, 0)
}
