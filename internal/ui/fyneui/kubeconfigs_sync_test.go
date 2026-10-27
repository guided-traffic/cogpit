package fyneui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// syncTree is a folder the sync picker browses: two kubeconfigs (one hidden), a note and a
// subfolder with a kubeconfig of its own. Nothing here is the developer's home or ~/.kube.
func syncTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"team.yaml", ".dev.kubeconfig", "notes.txt", filepath.Join("clusters", "prod.yaml")} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func openSyncPicker(t *testing.T, sh *shell, q *mainQueue, dir string) *syncPicker {
	t.Helper()
	sh.kc.startDir = func() string { return dir }
	sh.kc.addKubeconfigs() // the real pick: the sync picker
	q.until(t, "the sync picker's listing", func() bool {
		return sh.kc.syncing != nil && len(sh.kc.syncing.entries) > 0
	})
	return sh.kc.syncing
}

// TestSyncPickerTakesFilesAndFoldersInOneDialog: one dialog lists folders first, then files, hidden
// ones too; the status line names what Sync adds. A selected file goes to kubeconfig.addFile, a
// selected folder and "nothing selected" (the folder shown) to kubeconfig.addFolder.
func TestSyncPickerTakesFilesAndFoldersInOneDialog(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.addFolder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"source": map[string]any{"id": "x/", "kind": "folder"}, "contexts": []string{}}, nil
	}
	dir := syncTree(t)
	sh, w, q := newTestShell(t, f, Options{})
	sp := openSyncPicker(t, sh, q, dir)

	var names []string
	for _, e := range sp.entries {
		names = append(names, e.name)
	}
	if want := []string{"clusters", ".dev.kubeconfig", "notes.txt", "team.yaml"}; !slices.Equal(names, want) {
		t.Fatalf("entries %v, want %v", names, want)
	}
	if findButton(w, "Sync") == nil || findButton(w, "Cancel") == nil {
		t.Error("the dialog lacks its Sync and Cancel buttons")
	}
	if !strings.HasPrefix(sp.status.text, "Sync folder: "+dir) {
		t.Errorf("status %q", sp.status.text)
	}

	// A hidden file: selected, named, synced as a file.
	test.Tap(sp.rows[".dev.kubeconfig"])
	if want := "Sync file: " + filepath.Join(dir, ".dev.kubeconfig"); sp.status.text != want {
		t.Errorf("status %q, want %q", sp.status.text, want)
	}
	test.Tap(sp.syncB)
	q.until(t, "kubeconfig.addFile", func() bool { return slices.Contains(rec.ids(), "kubeconfig.addFile") })
	if p := rec.params("kubeconfig.addFile"); p["path"] != filepath.Join(dir, ".dev.kubeconfig") {
		t.Errorf("addFile %v", p)
	}
	if sh.kc.syncing != nil {
		t.Error("the dialog stays open after Sync")
	}

	// A selected folder is synced as a folder; so is the folder shown when nothing is selected.
	sp = openSyncPicker(t, sh, q, dir)
	test.Tap(sp.rows["clusters"])
	test.Tap(sp.syncB)
	q.until(t, "kubeconfig.addFolder", func() bool { return len(f.callsOf("kubeconfig.addFolder")) == 1 })
	sp = openSyncPicker(t, sh, q, dir)
	test.Tap(sp.syncB)
	q.until(t, "the second kubeconfig.addFolder", func() bool { return len(f.callsOf("kubeconfig.addFolder")) == 2 })
	var got []string
	for _, c := range f.callsOf("kubeconfig.addFolder") { // the paths decoded: JSON doubles Windows' backslashes
		var p struct{ Path string }
		if err := json.Unmarshal(c.Params, &p); err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Path)
	}
	if !strings.Contains(got[0], filepath.Join(dir, "clusters")) || !strings.Contains(got[1], dir) || strings.Contains(got[1], "clusters") {
		t.Errorf("addFolder calls %v", got)
	}
}

// TestSyncPickerNavigates: a double tap opens a folder, the up button returns with the folder
// selected, a typed file path opens its folder with the file selected, a typed folder opens it, a
// bad path says so; Cancel adds nothing.
func TestSyncPickerNavigates(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	dir := syncTree(t)
	sh, _, q := newTestShell(t, f, Options{})
	sp := openSyncPicker(t, sh, q, dir)

	sp.rows["clusters"].DoubleTapped(nil)
	q.until(t, "the subfolder", func() bool { return sp.dir == filepath.Join(dir, "clusters") && len(sp.entries) == 1 })
	if sp.entries[0].name != "prod.yaml" || sp.pathEntry.Text != sp.dir {
		t.Errorf("entries %+v, path %q", sp.entries, sp.pathEntry.Text)
	}
	test.Tap(sp.upB)
	q.until(t, "the parent", func() bool { return sp.dir == dir && len(sp.entries) == 4 })
	if sp.selected != filepath.Join(dir, "clusters") {
		t.Errorf("after up the folder we came from is selected, got %q", sp.selected)
	}

	sp.pathEntry.SetText(filepath.Join(dir, "team.yaml"))
	sp.pathEntry.OnSubmitted(sp.pathEntry.Text)
	q.until(t, "the typed file", func() bool { return sp.selected == filepath.Join(dir, "team.yaml") })
	sp.pathEntry.OnSubmitted(filepath.Join(dir, "clusters"))
	q.until(t, "the typed folder", func() bool {
		return sp.dir == filepath.Join(dir, "clusters") && len(sp.entries) == 1 // listed, not just chosen
	})
	nope := filepath.Join(dir, "nope")
	_, notFound := os.Stat(nope) // the system's words: "no such file or directory", Windows' "cannot find the file"
	sp.pathEntry.OnSubmitted(nope)
	q.until(t, "the error", func() bool { return notFound != nil && sp.status.text == notFound.Error() })

	test.Tap(sp.cancelB)
	if sh.kc.syncing != nil || len(rec.ids()) != 0 {
		t.Errorf("Cancel left the dialog open or added something: %v", rec.ids())
	}
}

// TestSyncPickerDoubleTapOnAFileSyncsIt: the shortcut for the common case.
func TestSyncPickerDoubleTapOnAFileSyncsIt(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	dir := syncTree(t)
	sh, _, q := newTestShell(t, f, Options{})
	sp := openSyncPicker(t, sh, q, dir)
	sp.rows["team.yaml"].DoubleTapped(nil)
	q.until(t, "kubeconfig.addFile", func() bool { return slices.Contains(rec.ids(), "kubeconfig.addFile") })
	if p := rec.params("kubeconfig.addFile"); p["path"] != filepath.Join(dir, "team.yaml") {
		t.Errorf("addFile %v", p)
	}
}

// TestSyncPickerNeverStartsInTheHomeInTests: the shell the tests build opens the sync picker in a
// temporary directory, so no test lists the developer's home or ~/.kube.
func TestSyncPickerNeverStartsInTheHomeInTests(t *testing.T) {
	sh, _, _ := newTestShell(t, newFakeEngine(t), Options{})
	home, _ := os.UserHomeDir()
	if got := sh.kc.startDir(); got == "" || got == home || strings.HasPrefix(got, filepath.Join(home, ".kube")) {
		t.Errorf("startDir = %q", got)
	}
}

// TestRemoveConfirmationNamesThePathAndSaysOnlyTheLinkGoes: for a file, a folder and the default
// folder; the sources dialog asks before kubeconfig.remove, and Cancel sends nothing.
func TestRemoveConfirmationNamesThePathAndSaysOnlyTheLinkGoes(t *testing.T) {
	for _, tt := range []struct {
		src  sourceStatus
		want []string
	}{
		{sourceStatus{ID: "eks.yaml", Kind: "file", Path: "/Users/me/Downloads/eks.yaml"},
			[]string{"/Users/me/Downloads/eks.yaml", "Only Cogpit's link to the file is removed", "stays on your disk, unchanged", "connection made from it is closed"}},
		{sourceStatus{ID: "kubeconfigs/", Kind: "folder", Path: "/Users/me/kubeconfigs"},
			[]string{"/Users/me/kubeconfigs", "Only Cogpit's link to the folder is removed", "The folder and its files stay on your disk"}},
		{sourceStatus{ID: ".kube/", Kind: "folder", Path: "/Users/me/.kube", Default: true},
			[]string{"/Users/me/.kube", "Only Cogpit's link", "adds it again"}},
		{sourceStatus{ID: "kubeconfig", Kind: "file", Path: "/work/kubeconfig", Session: true},
			[]string{"/work/kubeconfig", "Only for this run", "offered again at the next start", "remembered", "stays on your disk"}},
	} {
		title, msg := removeText(tt.src)
		if !strings.Contains(title, tt.src.ID) {
			t.Errorf("title %q", title)
		}
		for _, w := range tt.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: %q lacks %q", tt.src.ID, msg, w)
			}
		}
	}

	f, rec := kubeconfigFake(t, engine.ExecRequired)
	sh, w, q := newTestShell(t, f, Options{})
	sh.kc.showSources()
	sd := sh.kc.sources
	q.until(t, "the sources", func() bool { return len(sd.rows) == 5 })
	test.Tap(sd.removeB[".kube/"])
	cancel := findButton(w, "Cancel")
	if cancel == nil || findButton(w, "Remove") == nil {
		t.Fatal("no confirmation")
	}
	test.Tap(cancel)
	q.drain()
	if slices.Contains(rec.ids(), "kubeconfig.remove") {
		t.Error("Cancel removed the source")
	}
}

// TestSectionMarksAFailedConnectAndOpensItsLog: a context whose last connect failed has a red dot
// in the navigator's section, its tooltip says so, and its menu's Show Connection Updates… opens
// "Connection Updates: <context>" with the engine's lines; contexts without a problem have none of
// it (the context picker's warning icon went, D-57; the welcome page's stays).
func TestSectionMarksAFailedConnectAndOpensItsLog(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	at := time.Date(2026, 10, 12, 14, 3, 9, 0, time.UTC)
	f.handlers["cluster.connectLog"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"context": "team [kubeconfigs/team.yaml]", "lines": []map[string]any{
			{"time": at, "level": "INFO", "text": "Connecting to team [kubeconfigs/team.yaml]"},
			{"time": at.Add(2 * time.Second), "level": "ERROR", "text": "exec: fork/exec /opt/bin/team-login: no such file or directory"},
		}}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	n := sh.side.sec
	q.until(t, "the section's rows", func() bool { return n.loaded && len(n.rows) == len(f.contexts) })
	var warned []string
	for _, c := range f.contexts {
		r := n.rows[c.Name]
		red := r.dot.color == statusColorName(model.StatusError)
		says := strings.Contains(r.tipText(), "The last connect failed")
		if red != says {
			t.Errorf("%s: red dot %v, tooltip %q", c.Name, red, r.tipText())
		}
		if red {
			warned = append(warned, c.Name)
		}
	}
	if !slices.Equal(warned, []string{"team [kubeconfigs/team.yaml]"}) {
		t.Fatalf("rows with a failed connect: %v", warned)
	}
	n.rowMenu("team [kubeconfigs/team.yaml]", fyne.NewPos(10, 10))
	items := n.menuModel.Items
	if last := items[len(items)-1]; last.Label != connectLogItem {
		t.Fatalf("team's menu ends with %q", last.Label)
	} else {
		last.Action()
	}
	q.until(t, "the log dialog", func() bool { return sh.kc.connectLog != nil })
	if p := rec.params("cluster.connectLog"); p["context"] != "team [kubeconfigs/team.yaml]" {
		t.Errorf("connectLog %v", p)
	}
	if n := len(sh.kc.connectLog.view.Lines); n != 2 {
		t.Errorf("%d lines", n)
	}
	test.Tap(sh.kc.connectLog.close)
	if sh.kc.connectLog != nil {
		t.Error("Close left the log open")
	}
}

// TestDisconnectedByAGoneFileSaysWhy: a connection Cogpit closed because its kubeconfig file went
// away leaves the workspace saying so, with the way to connect again; the toast reaches the user.
func TestDisconnectedByAGoneFileSaysWhy(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	sh, _, q := newTestShell(t, f, Options{})
	reason := "Disconnected from kind-cogpit [kind.yaml]: its kubeconfig file /Users/me/kind.yaml is gone."
	f.update(func(u *engine.UIState) {
		u.Context = "kind-cogpit [kind.yaml]"
		u.Connection = engine.Connection{State: engine.Disconnected, Error: reason}
	})
	q.until(t, "the placeholder", func() bool { return sh.view.placeholderText.text == reason })
	if !sh.view.connectButton.Visible() {
		t.Error("no way to connect again")
	}
	f.emit(engine.Event{Type: engine.EventToast, Level: engine.LevelWarning, Message: reason})
	q.drain()
}

func TestRenderSyncPicker(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	dir := syncTree(t)
	sh, w, q := newTestShell(t, f, Options{})
	sp := openSyncPicker(t, sh, q, dir)
	test.Tap(sp.rows[".dev.kubeconfig"])
	savePNG(t, w, "sync_picker_dark.png")
}

func TestRenderRemoveConfirmation(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	sh, w, q := newTestShell(t, f, Options{})
	sh.kc.showSources()
	sd := sh.kc.sources
	q.until(t, "the sources", func() bool { return len(sd.rows) == 5 })
	test.Tap(sd.removeB[".kube/"])
	savePNG(t, w, "kubeconfig_remove_confirm_dark.png")
}

func TestRenderDisconnectedPlaceholder(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	sh, w, q := newTestShell(t, f, Options{})
	reason := "Disconnected from kind-cogpit [kind.yaml]: its kubeconfig file /Users/me/kind.yaml is gone."
	f.update(func(u *engine.UIState) {
		u.Context = "kind-cogpit [kind.yaml]"
		u.Connection = engine.Connection{State: engine.Disconnected, Error: reason}
	})
	q.until(t, "the placeholder", func() bool { return sh.view.placeholderText.text == reason })
	savePNG(t, w, "disconnected_file_gone_dark.png")
}

// A path typed or pasted into the entry and not opened with Enter is what Sync adds, not the folder
// the dialog shows (owner, 2026-10-12: Sync added the home directory instead of the folder typed).
// One that does not exist keeps the dialog open with the reason; a tap on an entry after typing
// chooses from the list again.
func TestSyncPickerSyncsATypedPath(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.addFolder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"source": map[string]any{"id": "x/", "kind": "folder"}, "contexts": []string{"a"}}, nil
	}
	dir := syncTree(t)
	other := t.TempDir()
	sh, _, q := newTestShell(t, f, Options{})

	sp := openSyncPicker(t, sh, q, dir)
	sp.pathEntry.SetText(other + "/") // pasted, not opened with Enter
	if !strings.HasPrefix(sp.status.text, "Sync: "+other) {
		t.Errorf("status %q does not name the typed path", sp.status.text)
	}
	test.Tap(sp.syncB)
	q.until(t, "kubeconfig.addFolder", func() bool { return len(f.callsOf("kubeconfig.addFolder")) == 1 })
	if p := rec.params("kubeconfig.addFolder"); p["path"] != other {
		t.Errorf("addFolder %v, want the typed %s", p, other)
	}

	sp = openSyncPicker(t, sh, q, dir)
	sp.pathEntry.SetText(filepath.Join(dir, "missing"))
	test.Tap(sp.syncB)
	q.until(t, "the reason", func() bool { return sp.status.color == colorStatusError })
	if sh.kc.syncing != sp || len(f.callsOf("kubeconfig.addFolder")) != 1 || len(f.callsOf("kubeconfig.addFile")) != 0 {
		t.Error("a missing typed path was synced or closed the dialog")
	}

	sp.pathEntry.SetText(other)
	test.Tap(sp.rows["team.yaml"]) // a tap chooses from the list
	test.Tap(sp.syncB)
	q.until(t, "kubeconfig.addFile", func() bool { return len(f.callsOf("kubeconfig.addFile")) == 1 })
	if p := rec.params("kubeconfig.addFile"); p["path"] != filepath.Join(dir, "team.yaml") {
		t.Errorf("addFile %v", p)
	}
}

// A sync that finds no cluster says so in a dialog, with the files it skipped and why, and offers
// to remove the source again; a toast saying "0 contexts" went unnoticed (owner, 2026-10-12).
func TestSyncWithoutClustersSaysWhy(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.addFolder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"source": map[string]any{"id": "configs/", "kind": "folder", "path": "/u/configs"}, "contexts": []string{},
			"skipped": 3, "skippedFiles": []string{"main.png: not a kubeconfig, or unreadable", "notes.txt: not a kubeconfig, or unreadable"}}, nil
	}
	f.handlers["kubeconfig.remove"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{}, nil
	}
	sh, w, q := newTestShell(t, f, Options{})
	sh.kc.addPath("kubeconfig.addFolder", "/u/configs")
	q.until(t, "the dialog", func() bool { return sh.kc.noClustersDialog != nil })
	texts := strings.Join(allTexts(w.Canvas().Overlays().Top()), "\n")
	for _, want := range []string{"None of the 3 files in the folder /u/configs defines a cluster context", "main.png: not a kubeconfig", "and 1 more"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the dialog lacks %q:\n%s", want, texts)
		}
	}
	test.Tap(findButton(w, "Remove"))
	q.until(t, "kubeconfig.remove", func() bool { return len(f.callsOf("kubeconfig.remove")) == 1 })
	if p := rec.params("kubeconfig.remove"); p["source"] != "configs/" {
		t.Errorf("remove %v", p)
	}
}

// allTexts collects the text of every text line and paragraph under root.
func allTexts(root fyne.CanvasObject) []string {
	var out []string
	for _, l := range findAll[*textLine](root) {
		out = append(out, l.text)
	}
	for _, p := range findAll[*paragraph](root) {
		out = append(out, p.text)
	}
	return out
}
