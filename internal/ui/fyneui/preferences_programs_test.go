package fyneui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/ui/nativedialog"
)

// Preferences › Kubernetes › Program folders (D-120): Add Folder… asks the system's folder panel
// (hidden folders listed, a read-only bookmark) and adds the folder through the engine, which puts
// it on PATH; each folder is a row whose remove button takes it out again; a refused folder (one
// every user may write to) shows under the setting and is not kept; the page follows a folder an
// agent removed.
func TestProgramFoldersSetting(t *testing.T) {
	t.Setenv("PATH", filepath.FromSlash("/usr/bin"))
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	picker := installPicker(sh)
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls["programFolderList"] != nil })
	p := sh.settings
	list := p.controls["programFolderList"].(*fyne.Container)
	if len(list.Objects) != 1 || list.Objects[0].(*textLine).text != programFoldersEmpty {
		t.Fatalf("the empty list shows %d objects", len(list.Objects))
	}
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }

	krew := filepath.Join(t.TempDir(), ".krew", "bin")
	if err := os.MkdirAll(krew, 0o755); err != nil {
		t.Fatal(err)
	}
	picker.picks = []nativedialog.Pick{{Path: krew, Folder: true}}
	test.Tap(buttonLabeled(t, p.body, "Add Folder…"))
	q.until(t, "the folder kept", func() bool { return slices.Equal(prefs().ProgramFolders, []string{krew}) })
	if len(picker.opens) != 1 || !picker.opens[0].Folders || picker.opens[0].Files || !picker.opens[0].ShowHidden ||
		picker.opens[0].Bookmark != nativedialog.ReadOnly {
		t.Errorf("the panel asked for %+v", picker.opens)
	}
	if got := os.Getenv("PATH"); !strings.HasSuffix(got, string(os.PathListSeparator)+krew) {
		t.Errorf("PATH %q lacks the folder", got)
	}
	q.until(t, "its row", func() bool { return len(findAll[*tipButton](list)) == 1 })
	if names := findAll[*textLine](list); len(names) != 1 || names[0].text != krew {
		t.Errorf("the row names %v", names)
	}

	if !isWindows() {
		open := t.TempDir()
		if err := os.Chmod(open, 0o777); err != nil {
			t.Fatal(err)
		}
		picker.picks = []nativedialog.Pick{{Path: open, Folder: true}}
		test.Tap(buttonLabeled(t, p.body, "Add Folder…"))
		q.until(t, "the refusal", func() bool { return p.errs[programFoldersSetting].Visible() })
		if !strings.Contains(p.errs[programFoldersSetting].text, "every user of this computer may write") || len(prefs().ProgramFolders) != 1 {
			t.Errorf("refusal %q, kept %q", p.errs[programFoldersSetting].text, prefs().ProgramFolders)
		}
	}

	test.Tap(findAll[*tipButton](list)[0])
	q.until(t, "the folder removed", func() bool { return len(prefs().ProgramFolders) == 0 })
	q.until(t, "the empty line again", func() bool { return len(findAll[*tipButton](list)) == 0 })
	if got := os.Getenv("PATH"); strings.Contains(got, krew) {
		t.Errorf("PATH %q keeps a removed folder", got)
	}

	// An agent's remove: the page follows.
	mustDispatch(t, s, "preferences.addProgramFolder", map[string]any{"path": krew}) // the window's own origin
	q.until(t, "the row back", func() bool { return len(findAll[*tipButton](list)) == 1 })
	raw, _ := json.Marshal(map[string]any{"path": krew})
	if _, err := s.Dispatch(t.Context(), engine.Invocation{ID: "preferences.removeProgramFolder", Params: raw, Origin: engine.OriginExternalAgent}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the row gone", func() bool { return len(findAll[*tipButton](list)) == 0 })
}

// A failed connect whose login did not find a program offers Program Folders…, which opens
// Preferences › Kubernetes; another failure does not.
func TestErrorPageOffersProgramFolders(t *testing.T) {
	sh, _, q, f, _ := marksScene(t, ThemeDark, false)
	failRestored(f, cFailed)
	p := sh.tabs.errPage
	q.until(t, "the error page", func() bool { return p.shown && len(p.rows) == 5 })
	if p.folders.Visible() {
		t.Error("Program Folders… offered for a certificate file that is gone")
	}
	f.update(func(u *engine.UIState) {
		u.Connection.Error = missingPluginFailure(cFailed).Stages[1].Text
		u.ConnectFailure = missingPluginFailure(cFailed)
	})
	q.until(t, "the button", func() bool { return p.folders.Visible() })
	test.Tap(p.folders)
	q.until(t, "Preferences opened", func() bool { return len(f.callsOf("preferences.open")) == 1 })
	var params preferencesOpenParams
	if err := json.Unmarshal(f.callsOf("preferences.open")[0].Params, &params); err != nil || params.Section != engine.SectionKubernetes {
		t.Errorf("opened %+v (%v)", params, err)
	}
}

// missingPluginFailure is a connect whose credential plugin kubectl did not find krew's
// kubectl-oidc_login, as the owner's colleague's.
func missingPluginFailure(ctx string) *engine.ConnectFailure {
	return &engine.ConnectFailure{Context: ctx, At: fixedNow, Stages: []engine.ConnectStage{
		{Name: "kubeconfig", Result: "ok", Text: `context "` + ctx + `" in /Users/me/.kube/config: server https://oidc.example:6443, login by the credential plugin kubectl`},
		{Name: "login", Result: "failed", TookMS: 229, MissingProgram: "kubectl-oidc_login",
			Text: "the credential plugin kubectl failed: it found no kubectl-oidc_login on PATH (kubectl: unknown command); if kubectl-oidc_login " +
				"is installed, add the folder it is in under Preferences › Kubernetes › Program folders (Cogpit does not read your shell's PATH)"},
		{Name: "dns", Result: "notReached"}, {Name: "tcp", Result: "notReached"}, {Name: "tls", Result: "notReached"}, {Name: "api", Result: "notReached"},
	}}
}

func isWindows() bool { return filepath.Separator == '\\' }

// buttonLabeled finds the text button with label under o.
func buttonLabeled(t *testing.T, o fyne.CanvasObject, label string) *textButton {
	t.Helper()
	for _, b := range findAll[*textButton](o) {
		if b.Text == label {
			return b
		}
	}
	t.Fatalf("no button %q", label)
	return nil
}

// The PNGs of the pull request: Preferences › Kubernetes with two program folders, dark and light;
// the error page of a login that did not find krew's plugin.
func TestRenderProgramFoldersDark(t *testing.T)  { renderProgramFolders(t, ThemeDark) }
func TestRenderProgramFoldersLight(t *testing.T) { renderProgramFolders(t, ThemeLight) }

func renderProgramFolders(t *testing.T, mode ThemeMode) {
	if isWindows() {
		t.Skip("the picture shows macOS paths")
	}
	t.Setenv("PATH", "/usr/bin")
	// Kept as a user's file holds them: folders of another home, which Cogpit keeps and skips while
	// they do not exist here.
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	doc := `{"version": 1, "theme": "` + string(mode) + `", "programFolders": ["/Users/me/.krew/bin", "/Users/me/tools/bin"]}`
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, engine.PreferencesFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	kc := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kc, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newTestEngine(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kc}},
		Preferences: engine.NewPreferencesStore(cfg), ClusterSettings: engine.NewClusterSettingsStore(cfg), Hotbar: engine.NewHotbar(cfg)})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the rows", func() bool {
		l, ok := sh.settings.controls["programFolderList"].(*fyne.Container)
		return sh.settings.visible() && ok && len(findAll[*tipButton](l)) == 2
	})
	savePNG(t, w, "preferences_program_folders_"+string(mode)+".png")
}

func TestRenderConnectFailedMissingPluginDark(t *testing.T) {
	sh, w, q, f, _ := marksScene(t, ThemeDark, false)
	failRestored(f, cFailed)
	f.update(func(u *engine.UIState) {
		u.Connection.Error = missingPluginFailure(cFailed).Stages[1].Text
		u.ConnectFailure = missingPluginFailure(cFailed)
	})
	q.until(t, "the button", func() bool { return sh.tabs.errPage.shown && sh.tabs.errPage.folders.Visible() })
	q.drain()
	savePNG(t, w, "connect_failed_missing_plugin_dark.png")
}
