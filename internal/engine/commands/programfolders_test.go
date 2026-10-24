package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// Program folders (D-120): the window adds a folder, which is kept in preferences.json, shown in the
// UI state and on the process's PATH at once, after PATH's own folders; agents and the CLI are
// refused before anything is written, but may remove one, which leaves PATH at once; a folder that
// is missing, a file, one every user may write to, or one kept already is refused; a new session
// over the same folder puts the kept folders on PATH again, and cogpit-cli reads them too.
func TestProgramFoldersAddRemoveAndKeep(t *testing.T) {
	t.Setenv("PATH", filepath.FromSlash("/usr/bin"))
	s, cfg, _ := settingsSession(t)
	mine := t.TempDir()
	second := t.TempDir()

	for _, origin := range []engine.Origin{engine.OriginCLI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		err := dispatch(t, s, origin, "preferences.addProgramFolder", map[string]any{"path": mine}, nil)
		var ge *engine.GUIOnlyError
		if !errors.As(err, &ge) || !errors.Is(err, engine.ErrProgramFoldersGUIOnly) {
			t.Errorf("%s: %v, want the GUI-only refusal", origin, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg, engine.PreferencesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused add wrote the preferences: %v", err)
	}
	if got := os.Getenv("PATH"); strings.Contains(got, mine) {
		t.Errorf("a refused add reached PATH: %q", got)
	}

	var res commands.PreferencesResult
	if err := dispatch(t, s, engine.OriginGUI, "preferences.addProgramFolder", map[string]any{"path": mine}, &res); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(t, s, engine.OriginGUI, "preferences.addProgramFolder", map[string]any{"path": second + string(filepath.Separator)}, &res); err != nil {
		t.Fatal(err)
	}
	if want := []string{mine, second}; !slices.Equal(res.Preferences.ProgramFolders, want) {
		t.Errorf("result %q, want %q", res.Preferences.ProgramFolders, want)
	}
	if ui := s.UIState(); ui.Preferences == nil || !slices.Equal(ui.Preferences.ProgramFolders, []string{mine, second}) {
		t.Errorf("UI state %+v", ui.Preferences)
	}
	sep := string(os.PathListSeparator)
	if got, want := os.Getenv("PATH"), filepath.FromSlash("/usr/bin")+sep+mine+sep+second; got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(cfg, engine.PreferencesFile))
	if err != nil || !strings.Contains(string(data), `"programFolders"`) {
		t.Errorf("preferences.json: %s %v", data, err)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	open := t.TempDir()
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	refusals := []struct{ path, want string }{
		{mine, "already"},
		{filepath.Join(mine, "missing"), "does not exist"},
		{file, "not a folder"},
		{"relative/bin", "absolute path"},
	}
	if runtime.GOOS != "windows" {
		refusals = append(refusals, struct{ path, want string }{open, "every user of this computer may write"})
	}
	for _, c := range refusals {
		if err := dispatch(t, s, engine.OriginGUI, "preferences.addProgramFolder", map[string]any{"path": c.path}, nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("add %s: %v, want %q", c.path, err, c.want)
		}
	}

	// A new session over the same preferences puts them on PATH again; the CLI reads them.
	t.Setenv("PATH", filepath.FromSlash("/usr/bin"))
	s2 := engine.New(engine.Options{Preferences: engine.NewPreferencesStore(cfg)})
	t.Cleanup(s2.Close)
	if got := os.Getenv("PATH"); !strings.HasSuffix(got, sep+mine+sep+second) {
		t.Errorf("a new session's PATH %q lacks the kept folders", got)
	}
	if got := engine.ReadProgramFolders(cfg); !slices.Equal(got, []string{mine, second}) {
		t.Errorf("ReadProgramFolders = %q", got)
	}

	if err := dispatch(t, s, engine.OriginExternalAgent, "preferences.removeProgramFolder", map[string]any{"path": mine}, &res); err != nil {
		t.Fatalf("an agent removing a folder: %v", err)
	}
	if !slices.Equal(res.Preferences.ProgramFolders, []string{second}) {
		t.Errorf("after the remove: %q", res.Preferences.ProgramFolders)
	}
	if got := os.Getenv("PATH"); strings.Contains(got, mine) || !strings.HasSuffix(got, second) {
		t.Errorf("PATH after the remove: %q", got)
	}
	if err := dispatch(t, s, engine.OriginGUI, "preferences.removeProgramFolder", map[string]any{"path": mine}, nil); err == nil {
		t.Error("removing a folder that is not one worked")
	}
	res = commands.PreferencesResult{}
	if err := dispatch(t, s, engine.OriginGUI, "preferences.removeProgramFolder", map[string]any{"path": second}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Preferences.ProgramFolders != nil {
		t.Errorf("all removed: %q", res.Preferences.ProgramFolders)
	}
	if got := os.Getenv("PATH"); got != filepath.FromSlash("/usr/bin") {
		t.Errorf("PATH with no program folders: %q", got)
	}
}

// A preferences file with folders Preferences cannot keep (relative, twice, more than the bound)
// loads with the rest: the bad entries go, the others stay in order.
func TestProgramFoldersNormalizedOnLoad(t *testing.T) {
	cfg := t.TempDir()
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.FromSlash("/opt/a"), filepath.FromSlash("/opt/b")
	if runtime.GOOS == "windows" {
		a, b = `C:\a`, `C:\b`
	}
	doc := `{"version": 1, "theme": "dark", "programFolders": [` + quoteJSON(a) + `, "relative", ` + quoteJSON(a+string(filepath.Separator)) +
		`, ` + quoteJSON(b) + `, "line\nbreak"]}`
	if err := os.WriteFile(filepath.Join(cfg, engine.PreferencesFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := engine.ReadProgramFolders(cfg); !slices.Equal(got, []string{a, b}) {
		t.Errorf("ReadProgramFolders = %q, want %q", got, []string{a, b})
	}
	if got := engine.ReadProgramFolders(filepath.Join(cfg, "none")); got != nil {
		t.Errorf("no file: %q", got)
	}
}

func quoteJSON(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }
