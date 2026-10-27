package fyneui

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// The window's file choosers on the system's panels (filepicker.go, D-72), driven through a
// recorder in the panels' place: what each asks the panel for, that a pick continues the
// chooser's flow and keeps the bookmarks it should, and that a cancel does nothing. No test shows
// a real panel; testdata/panel_harness.m checks the panels as built (internal/ui/nativedialog).

// fakePicker stands in for the system's panels: it records each request and answers an open with
// picks and a save with savePath, on the main goroutine as the panels do; no picks or no path is
// a cancel, which calls nothing.
type fakePicker struct {
	sh       *shell
	mu       sync.Mutex
	opens    []nativedialog.OpenOptions
	saves    []nativedialog.SaveOptions
	picks    []nativedialog.Pick
	savePath string
}

func installPicker(sh *shell) *fakePicker {
	p := &fakePicker{sh: sh}
	sh.files = p
	return p
}

func (p *fakePicker) open(o nativedialog.OpenOptions, done func([]nativedialog.Pick)) bool {
	p.mu.Lock()
	p.opens = append(p.opens, o)
	picks := p.picks
	p.mu.Unlock()
	if len(picks) > 0 {
		p.sh.do(func() { done(picks) })
	}
	return true
}

func (p *fakePicker) save(o nativedialog.SaveOptions, done func(string)) bool {
	p.mu.Lock()
	p.saves = append(p.saves, o)
	path := p.savePath
	p.mu.Unlock()
	if path != "" {
		p.sh.do(func() { done(path) })
	}
	return true
}

func (p *fakePicker) lastOpen(t *testing.T) nativedialog.OpenOptions {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.opens) == 0 {
		t.Fatal("no open panel was asked for")
	}
	return p.opens[len(p.opens)-1]
}

func (p *fakePicker) lastSave(t *testing.T) nativedialog.SaveOptions {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.saves) == 0 {
		t.Fatal("no save panel was asked for")
	}
	return p.saves[len(p.saves)-1]
}

// withBookmarks gives the shell a store in a temporary directory.
func withBookmarks(t *testing.T, sh *shell) *nativedialog.Store {
	t.Helper()
	store, err := nativedialog.OpenStore(filepath.Join(t.TempDir(), "Cogpit", nativedialog.BookmarksFile))
	if err != nil {
		t.Fatal(err)
	}
	sh.bookmarks = store
	return store
}

// Sync Files and Folders on the system's panel: one panel for files and folders, several at once,
// hidden files listed, the button "Sync", opening in the home folder (here the test's); each pick
// is synced in the order chosen, a file with kubeconfig.addFile, a folder with
// kubeconfig.addFolder, and its read-only bookmark kept. A cancel syncs nothing and shows no
// other dialog.
func TestSyncPanelSyncsEveryPick(t *testing.T) {
	f, rec := kubeconfigFake(t, engine.ExecRequired)
	f.handlers["kubeconfig.addFolder"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		rec.add(inv)
		return map[string]any{"source": map[string]any{"id": "team/", "kind": "folder"}, "contexts": []string{"a [team/a.yaml]"}}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	home := t.TempDir()
	sh.kc.startDir = func() string { return home }
	store := withBookmarks(t, sh)
	p := installPicker(sh)

	sh.kc.addKubeconfigs() // cancelled
	q.drain()
	if o := p.lastOpen(t); !reflect.DeepEqual(o, syncPanel(home)) {
		t.Errorf("Sync panel %+v, want %+v", o, syncPanel(home))
	}
	if o := syncPanel(home); !o.Files || !o.Folders || !o.Multiple || !o.ShowHidden || o.Prompt != "Sync" ||
		o.Title != "Sync Files and Folders" || o.Dir != home || o.Bookmark != nativedialog.ReadOnly || len(o.Types) != 0 {
		t.Errorf("the Sync panel's options %+v", o)
	}
	if slices.Contains(rec.ids(), "kubeconfig.addFile") || slices.Contains(rec.ids(), "kubeconfig.addFolder") || sh.kc.syncing != nil {
		t.Fatalf("a cancel synced %v or showed Cogpit's own picker", rec.ids())
	}

	p.picks = []nativedialog.Pick{
		{Path: "/Users/me/Downloads/dev.yaml", Bookmark: []byte("file-bookmark")},
		{Path: "/Users/me/Documents/team", Folder: true, Bookmark: []byte("folder-bookmark")},
	}
	sh.kc.addKubeconfigs()
	q.until(t, "both synced", func() bool {
		return slices.Contains(rec.ids(), "kubeconfig.addFile") && slices.Contains(rec.ids(), "kubeconfig.addFolder")
	})
	ids := rec.ids()
	if slices.Index(ids, "kubeconfig.addFile") > slices.Index(ids, "kubeconfig.addFolder") {
		t.Errorf("synced out of order: %v", ids)
	}
	if got := rec.params("kubeconfig.addFile")["path"]; got != "/Users/me/Downloads/dev.yaml" {
		t.Errorf("addFile %q", got)
	}
	if got := rec.params("kubeconfig.addFolder")["path"]; got != "/Users/me/Documents/team" {
		t.Errorf("addFolder %q", got)
	}
	q.until(t, "the bookmarks kept", func() bool { return len(store.Entries()) == 2 })
	for i, e := range store.Entries() {
		if e.Use != nativedialog.UseKubeconfig || e.Access != nativedialog.ReadOnly || e.Path != filepath.Clean(p.picks[i].Path) ||
			string(e.Bookmark) != string(p.picks[i].Bookmark) {
			t.Errorf("bookmark %d: %+v", i, e)
		}
	}
}

// Without the system's panels (Windows, Linux, Fyne's test driver) Sync shows Cogpit's own
// picker, which chooses files and folders together; the system's panels report that they have no
// window in the test driver, so a window without an NSWindow falls back too.
func TestSyncFallsBackToCogpitsPicker(t *testing.T) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	sh, _, q := newTestShell(t, f, Options{})
	sh.files = systemPanels{sh}
	if sh.nsWindow() != 0 {
		t.Fatal("the test driver's window has an NSWindow")
	}
	called := false
	if (systemPanels{sh}).open(syncPanel(t.TempDir()), func([]nativedialog.Pick) { called = true }) {
		t.Error("the system's panel reported shown in the test driver")
	}
	if (systemPanels{sh}).save(nativedialog.SaveOptions{Name: "x.csv"}, func(string) { called = true }) {
		t.Error("the system's save panel reported shown in the test driver")
	}
	sh.kc.addKubeconfigs()
	q.until(t, "Cogpit's own picker", func() bool { return sh.kc.syncing != nil })
	if called {
		t.Error("a panel that was not shown answered")
	}
}

// A request the panels cannot take falls back to the chooser's dialog, except when the window
// already shows a panel: then it is dropped.
func TestPanelShown(t *testing.T) {
	for err, want := range map[error]bool{
		nil:                         true,
		nativedialog.ErrBusy:        true,
		nativedialog.ErrUnsupported: false,
		nativedialog.ErrNoWindow:    false,
		nativedialog.ErrNotMain:     false,
		errors.New("other"):         false,
	} {
		if got := panelShown(err); got != want {
			t.Errorf("panelShown(%v) = %v, want %v", err, got, want)
		}
	}
}

// A cluster's icon: one image, PNG, JPEG or SVG, no bookmark (the image is copied into Cogpit's
// own folder at once); a folder: folders only, its read-write bookmark kept. A cancel calls
// nothing.
func TestIconAndFolderPanels(t *testing.T) {
	sh, _, q := newTestShell(t, newFakeEngine(t), Options{})
	store := withBookmarks(t, sh)
	p := installPicker(sh)
	var got []string
	done := func(path string) { got = append(got, path) }

	sh.settings.pickIcon(done)
	sh.settings.pickFolder(done)
	q.drain()
	if len(got) != 0 {
		t.Fatalf("a cancel answered %v", got)
	}
	p.mu.Lock()
	icon, folder := p.opens[0], p.opens[1]
	p.mu.Unlock()
	if !icon.Files || icon.Folders || icon.Multiple || !slices.Equal(icon.Types, []string{"png", "jpg", "jpeg", "svg"}) ||
		icon.Bookmark != nativedialog.NoBookmark || icon.Prompt != "Upload" {
		t.Errorf("icon panel %+v", icon)
	}
	if folder.Files || !folder.Folders || folder.Multiple || folder.Bookmark != nativedialog.ReadWrite || folder.Prompt != "Choose" {
		t.Errorf("folder panel %+v", folder)
	}

	p.picks = []nativedialog.Pick{{Path: "/Users/me/Pictures/prod.png", Bookmark: []byte("unused")}}
	sh.settings.pickIcon(done)
	q.drain()
	p.picks = []nativedialog.Pick{{Path: "/Users/me/Documents/work", Folder: true, Bookmark: []byte("folder")}}
	sh.settings.pickFolder(done)
	q.until(t, "the folder's bookmark", func() bool { return len(store.Entries()) == 1 })
	if !slices.Equal(got, []string{"/Users/me/Pictures/prod.png", "/Users/me/Documents/work"}) {
		t.Errorf("picked %v", got)
	}
	if e := store.Entries()[0]; e.Use != nativedialog.UseFolder || e.Access != nativedialog.ReadWrite || e.Path != filepath.Clean("/Users/me/Documents/work") {
		t.Errorf("kept %+v; an icon keeps no bookmark", e)
	}
}

// A chat connection's CA file: Choose… opens the system's panel for a .pem, .crt or .cer file,
// the pick fills the field and keeps a read-only bookmark; a cancel leaves the field.
func TestCAFilePanel(t *testing.T) {
	sh, _, q, _ := agentPrefsShell(t, ThemeDark)
	store := withBookmarks(t, sh)
	p := installPicker(sh)
	a := sh.settings.agent
	a.edit(litellm())
	before := a.form.caFile.Text
	test.Tap(a.form.caB)
	q.drain()
	if a.form.caFile.Text != before {
		t.Errorf("a cancel set the field to %q", a.form.caFile.Text)
	}
	if o := p.lastOpen(t); !o.Files || o.Folders || !slices.Equal(o.Types, []string{"pem", "crt", "cer"}) || o.Bookmark != nativedialog.ReadOnly {
		t.Errorf("CA panel %+v", o)
	}
	p.picks = []nativedialog.Pick{{Path: "/Users/me/Downloads/corp-ca.pem", Bookmark: []byte("ca")}}
	test.Tap(a.form.caB)
	q.until(t, "the field", func() bool { return a.form.caFile.Text == "/Users/me/Downloads/corp-ca.pem" })
	q.until(t, "the bookmark", func() bool { return len(store.Entries()) == 1 })
	if e := store.Entries()[0]; e.Use != nativedialog.UseCAFile || e.Access != nativedialog.ReadOnly {
		t.Errorf("kept %+v", e)
	}
}

// The table's CSV download on the system's save panel: it suggests the engine's file name and the
// .csv type, writes the CSV to the path picked, owner-only; a cancel writes nothing.
func TestCSVSavePanel(t *testing.T) {
	f := kindsFake(t, "nodes")
	handleExport(f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the nodes view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "nodes" })
	p := installPicker(sh)
	test.Tap(sh.view.csvButton)
	q.until(t, "the save panel", func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.saves) == 1 })
	if o := p.lastSave(t); o.Name != "nodes-demo.csv" || !slices.Equal(o.Types, []string{"csv"}) {
		t.Errorf("save panel %+v", o)
	}
	dir := t.TempDir()
	p.savePath = filepath.Join(dir, "nodes.csv")
	test.Tap(sh.view.csvButton)
	untilSaved(t, q, sh, "nodes.csv")
	data, _ := os.ReadFile(p.savePath)
	if !strings.HasPrefix(string(data), "Name,") {
		t.Errorf("saved %q", data)
	}
	assertOwnerOnly(t, p.savePath)
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("%d files: the cancel wrote one", len(entries))
	}
}

// The Create tab's save on the system's save panel: the manifest's kind and name as the name,
// the YAML types, the text written to the path picked; a cancel writes nothing and says nothing.
func TestManifestSavePanel(t *testing.T) {
	_, _, q, ct := createScene(t, ThemeDark)
	p := installPicker(ct.sh)
	ct.pane.view.SetText(settingsManifest)
	status := ct.status.text
	test.Tap(ct.saveBtn)
	q.drain()
	if o := p.lastSave(t); o.Name != "configmap-settings.yaml" || !slices.Equal(o.Types, []string{"yaml", "yml"}) {
		t.Errorf("save panel %+v", o)
	}
	if ct.status.text != status {
		t.Errorf("a cancel said %q", ct.status.text)
	}
	p.savePath = filepath.Join(t.TempDir(), "settings.yaml")
	test.Tap(ct.saveBtn)
	q.until(t, "saved", func() bool { return strings.HasPrefix(ct.status.text, "Saved to ") })
	if got, err := os.ReadFile(p.savePath); err != nil || string(got) != settingsManifest {
		t.Errorf("saved %q, %v", got, err)
	}
	assertOwnerOnly(t, p.savePath)
}

// A log tab's download on the system's save panel: named after the tab, any type, the lines the
// tab shows written to the path picked.
func TestLogSavePanel(t *testing.T) {
	sh, _, q, _ := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	lt := logTabOf(sh)
	p := installPicker(sh)
	p.savePath = filepath.Join(t.TempDir(), "checkout.log")
	want := lt.view.DownloadLines()
	test.Tap(lt.download)
	untilSaved(t, q, sh, "checkout.log")
	if o := p.lastSave(t); o.Name != "Deployment_checkout-api-demo.log" || len(o.Types) != 0 {
		t.Errorf("save panel %+v", o)
	}
	fh, err := os.Open(p.savePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	var lines []string
	for sc := bufio.NewScanner(fh); sc.Scan(); {
		lines = append(lines, sc.Text())
	}
	if !slices.Equal(lines, want) {
		t.Errorf("saved %d lines, want %d", len(lines), len(want))
	}
}

// untilSaved waits for a download's end, its toast "Saved … to <name>" once the file is written
// and closed; a "Could not save" toast fails the test. The file's size says nothing of the end:
// the file grows write by write.
func untilSaved(t *testing.T, q *mainQueue, sh *shell, name string) {
	t.Helper()
	q.until(t, "the save's toast", func() bool {
		toasts := toastTexts(sh)
		if strings.Contains(toasts, "Could not save") {
			t.Fatalf("the save failed: %s", toasts)
		}
		return strings.Contains(toasts, " to "+name)
	})
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s: mode %v, %v; want 0600", path, fi.Mode().Perm(), err)
	}
}
