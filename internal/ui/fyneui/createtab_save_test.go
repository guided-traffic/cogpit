package fyneui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/test"
)

const settingsManifest = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: storefront\ndata:\n  mode: fast\n"

// createScene is the shell with a Create resource tab holding the example Pod.
func createScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *createTab) {
	t.Helper()
	f := connectedFake(t, 200)
	newEditorFake(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	sh.dispatch("resource.createTab", createTabParams{}, nil)
	var ct *createTab
	q.until(t, "the create tab", func() bool {
		c, ok := sh.dock.contents["create-1"]
		if !ok {
			return false
		}
		ct, _ = c.editor.(*createTab)
		return ct != nil && strings.Contains(ct.pane.view.Text(), "kind: Pod") && len(ct.templates) > 0
	})
	return sh, w, q, ct
}

// TestCreateTabSavesToAFile: the save button asks where to save the tab's text with the system's
// file save dialog, suggesting the first object's kind and name, and writes the text there.
func TestCreateTabSavesToAFile(t *testing.T) {
	_, w, q, ct := createScene(t, ThemeDark)
	ct.pane.view.SetText(settingsManifest)
	dir := t.TempDir()
	start, err := storage.ListerForURI(storage.NewFileURI(dir))
	if err != nil {
		t.Fatal(err)
	}
	var d *dialog.FileDialog
	ct.saveDialog = func(name string, data []byte) { d = ct.showSaveDialog(name, data, start) }
	test.Tap(ct.saveBtn)
	q.until(t, "the save dialog", func() bool { return d != nil })
	save := findButton(w, lang.L("Save")) // Fyne's dialog speaks the system's language
	if save == nil {
		t.Fatal("no Save button in the dialog")
	}
	test.Tap(save)
	q.drain()
	got, err := os.ReadFile(filepath.Join(dir, "configmap-settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != settingsManifest || !strings.HasPrefix(ct.status.text, "Saved to ") {
		t.Errorf("saved %q, status %q", got, ct.status.text)
	}
}

// TestCreateTabKeepsSecretsOffDisk: a text that holds a Secret's manifest is not saved to a file;
// an empty one has nothing to save.
func TestCreateTabKeepsSecretsOffDisk(t *testing.T) {
	_, _, _, ct := createScene(t, ThemeDark)
	asked := false
	ct.saveDialog = func(string, []byte) { asked = true }
	ct.pane.view.SetText("apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\nstringData:\n  password: hunter2\n")
	test.Tap(ct.saveBtn)
	if asked || ct.status.text != createSaveRefused {
		t.Errorf("a Secret: asked %v, status %q", asked, ct.status.text)
	}
	ct.pane.view.SetText("  \n")
	test.Tap(ct.saveBtn)
	if asked || !strings.HasPrefix(ct.status.text, "Nothing to save") {
		t.Errorf("an empty text: asked %v, status %q", asked, ct.status.text)
	}
}

// TestCreateTabSearchOpensTheFindWidget: the search button opens the editor's find widget below
// the editor, and closes it again.
func TestCreateTabSearchOpensTheFindWidget(t *testing.T) {
	_, w, _, ct := createScene(t, ThemeDark)
	if ct.find.visible() {
		t.Fatal("the find widget shows before the search button is tapped")
	}
	test.Tap(ct.searchBtn)
	if !ct.find.visible() || w.Canvas().Focused() != ct.find.find {
		t.Fatalf("after the tap: visible %v, focus %v", ct.find.visible(), w.Canvas().Focused())
	}
	test.Type(ct.find.find, "nginx")
	if ct.find.count.text == "" || ct.find.count.text == "No results" {
		t.Errorf("the example Pod has no match: %q", ct.find.count.text)
	}
	test.Tap(ct.searchBtn)
	if ct.find.visible() {
		t.Error("the second tap did not close the find widget")
	}
}

func TestManifestFileName(t *testing.T) {
	for text, want := range map[string]string{
		settingsManifest: "configmap-settings.yaml",
		"apiVersion: v1\nkind: Pod\nmetadata:\n  name: ../../etc/passwd\n": "pod-..-..-etc-passwd.yaml",
		"kind: Deployment\n": "resource.yaml",
		"not: [yaml":         "resource.yaml",
		"":                   "resource.yaml",
		"apiVersion: v1\nkind: A\nmetadata:\n  name: Ü\n": "a.yaml",
	} {
		if got := manifestFileName(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

func renderCreateTools(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, ct := createScene(t, mode)
	setDockOffset(sh, renderDockOffset)
	test.Tap(ct.searchBtn)
	test.Type(ct.find.find, "nginx")
	q.drain()
	savePNG(t, w, name)
}

func TestRenderCreateToolsDark(t *testing.T) {
	renderCreateTools(t, ThemeDark, "create_tools_dark.png")
}
func TestRenderCreateToolsLight(t *testing.T) {
	renderCreateTools(t, ThemeLight, "create_tools_light.png")
}
