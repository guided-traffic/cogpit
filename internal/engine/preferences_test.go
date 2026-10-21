package engine

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// testShell is an absolute shell path where the tests run: on Windows an absolute path names its
// drive, so /bin/sh is refused there.
func testShell() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\cmd.exe`
	}
	return "/bin/sh"
}

// A first start shows the defaults and writes nothing; a change is written owner-only (0600 in a
// 0700 directory) and read back by the next start.
func TestPreferencesPersistPrivately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	s := New(Options{Preferences: NewPreferencesStore(dir)})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory exists before the first change: %v", err)
	}
	if got := s.UIState().Preferences; got == nil || got.Theme != ThemeSystem || !got.EditorLineNumbers || got.EditorTabSize != 2 ||
		got.EditorFontSize != DefaultFontSize || got.TerminalFontSize != DefaultFontSize {
		t.Fatalf("a first start shows the defaults: %+v", got)
	}
	if _, err := s.ChangePreferences(func(p *Preferences) error {
		p.Theme, p.Timezone, p.TableRowHeight = ThemeLight, "Europe/Berlin", 32
		p.EditorFontSize, p.EditorLineNumbers, p.EditorTabSize = 15, false, 4
		p.TerminalFontSize, p.TerminalShell, p.TerminalShellArgs = 14, testShell(), []string{"-l"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.UIState().Preferences; got.Theme != ThemeLight || got.EditorLineNumbers {
		t.Errorf("the UI state does not show the change: %+v", got)
	}
	s.Close()

	path := filepath.Join(dir, PreferencesFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("preferences.json mode %v, want 0600", fi.Mode().Perm())
		}
		if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
			t.Errorf("directory mode %v, want 0700", di.Mode().Perm())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want preferences.json alone (no temporary file left)", len(entries))
	}

	s2 := New(Options{Preferences: NewPreferencesStore(dir)})
	defer s2.Close()
	got, err := s2.Preferences()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != ThemeLight || got.Timezone != "Europe/Berlin" || got.TableRowHeight != 32 || got.EditorFontSize != 15 ||
		got.EditorLineNumbers || got.EditorTabSize != 4 || got.TerminalFontSize != 14 || got.TerminalShell != testShell() ||
		len(got.TerminalShellArgs) != 1 || got.TerminalShellArgs[0] != "-l" {
		t.Errorf("after a restart: %+v", got)
	}
	if s2.Location().String() != "Europe/Berlin" {
		t.Errorf("location %s after a restart", s2.Location())
	}
}

// A file Cogpit cannot read does not stop the start: the defaults apply, and the file is moved
// aside at the first change instead of being overwritten.
func TestPreferencesUnreadableFileIsKeptAside(t *testing.T) {
	for name, content := range map[string]string{"garbage": "{not json", "future": `{"version": 9, "theme": "light"}`} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, PreferencesFile)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s := New(Options{Preferences: NewPreferencesStore(dir)})
			defer s.Close()
			if got := s.UIState().Preferences; got == nil || got.Theme != ThemeSystem {
				t.Fatalf("an unreadable file starts with the defaults: %+v", got)
			}
			if _, err := s.ChangePreferences(func(p *Preferences) error { p.Theme = ThemeSystem; return nil }); err != nil {
				t.Fatal(err)
			}
			kept, err := os.ReadFile(path + ".unreadable")
			if err != nil || string(kept) != content {
				t.Errorf("the unreadable file was not kept aside: %q, %v", kept, err)
			}
			if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"theme": "system"`) {
				t.Errorf("the new file: %s", raw)
			}
		})
	}
}

// One field out of bounds in the file costs that field only; Check refuses each bad value with a
// reason, and a refused change changes nothing.
func TestPreferencesCheckAndNormalize(t *testing.T) {
	dir := t.TempDir()
	bad := `{"version":1,"theme":"neon","timezone":"Mars/Olympus","tableRowHeight":3,"editorFontSize":99,` +
		`"editorLineNumbers":false,"editorTabSize":3,"terminalFontSize":12,"terminalShell":"zsh","terminalShellArgs":["-l","a\nb"],` +
		`"debugImage":"https://example.com/x"}`
	if err := os.WriteFile(filepath.Join(dir, PreferencesFile), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Preferences: NewPreferencesStore(dir)})
	defer s.Close()
	got, _ := s.Preferences()
	want := DefaultPreferences()
	want.EditorLineNumbers, want.EditorTabSize, want.TerminalFontSize = false, 3, 12
	if got.Theme != want.Theme || got.Timezone != "" || got.TableRowHeight != 0 || got.EditorFontSize != want.EditorFontSize ||
		got.EditorLineNumbers || got.EditorTabSize != 3 || got.TerminalFontSize != 12 || got.TerminalShell != "" || got.TerminalShellArgs != nil ||
		got.DebugImage != "" {
		t.Errorf("normalized %+v, want the good fields kept and the bad ones defaulted", got)
	}

	for name, fn := range map[string]func(*Preferences){
		"theme":      func(p *Preferences) { p.Theme = "Dark" },
		"zone":       func(p *Preferences) { p.Timezone = "Local" },
		"zone path":  func(p *Preferences) { p.Timezone = "../../etc/passwd" },
		"row height": func(p *Preferences) { p.TableRowHeight = 100 },
		"font":       func(p *Preferences) { p.EditorFontSize = 7 },
		"terminal":   func(p *Preferences) { p.TerminalFontSize = 40 },
		"tab":        func(p *Preferences) { p.EditorTabSize = 0 },
		"shell":      func(p *Preferences) { p.TerminalShell = "bin/zsh" },
		"args":       func(p *Preferences) { p.TerminalShellArgs = []string{"ok", "two\nlines"} },
		"debug":      func(p *Preferences) { p.DebugImage = "nicolaka/netshoot v0.16" },
	} {
		before, _ := s.Preferences()
		_, err := s.ChangePreferences(func(p *Preferences) error { fn(p); return nil })
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		if after, _ := s.Preferences(); after.Theme != before.Theme || after.TableRowHeight != before.TableRowHeight {
			t.Errorf("%s: a refused change changed the preferences", name)
		}
	}
	if err := CheckShell("/no/such/shell"); err == nil {
		t.Error("CheckShell accepted a missing file")
	}
	if runtime.GOOS != "windows" {
		if err := CheckShell("/bin/sh"); err != nil {
			t.Errorf("CheckShell(/bin/sh): %v", err)
		}
	}
}

// A session without a preferences store has the defaults and refuses changes.
func TestNoPreferencesStore(t *testing.T) {
	s := New(Options{})
	defer s.Close()
	if s.UIState().Preferences != nil {
		t.Error("a session without a store reports preferences")
	}
	if _, err := s.ChangePreferences(func(*Preferences) error { return nil }); !errors.Is(err, ErrNoPreferences) {
		t.Errorf("change without a store: %v", err)
	}
	if s.Location() != time.Local {
		t.Errorf("location %v, want the computer's", s.Location())
	}
}

// The time zone preference reaches the drawer: Created is written in that zone, in the
// computer's format the session was given (D-31).
func TestCreatedFollowsTheTimezone(t *testing.T) {
	s := New(Options{Preferences: NewPreferencesStore(t.TempDir()), Now: func() time.Time {
		return time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)
	}, TimeLayout: model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}})
	defer s.Close()
	obj := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{
		"name": "web-0", "creationTimestamp": "2026-10-01T12:00:00Z"}}}
	created := func() string {
		m, err := BuildDetailIn(&model.KindProfile{Key: "pods", Kind: "Pod"}, model.ObjectRef{Kind: "pods", Name: "web-0"},
			obj, nil, s.displayNow(), nil, s.TimeFormat())
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range m.Sections[0].Items {
			if it.Name == "Created" {
				return it.Value
			}
		}
		return ""
	}
	if _, err := s.ChangePreferences(func(p *Preferences) error { p.Timezone = "UTC"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := created(); got != "10d ago (01.10.2026 12:00:00)" {
		t.Errorf("UTC: %q", got)
	}
	if _, err := s.ChangePreferences(func(p *Preferences) error { p.Timezone = "Europe/Berlin"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := created(); got != "10d ago (01.10.2026 14:00:00)" {
		t.Errorf("Europe/Berlin: %q", got)
	}
	if got := s.UIState().TimeFormat; got != (model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}) {
		t.Errorf("ui_state's time format %+v", got)
	}
}

// The local terminal starts with the Preferences' shell and arguments and the cluster's working
// directory and namespace; without them it keeps the filter's namespace and the home directory.
func TestLocalTerminalOptionsFromTheSettings(t *testing.T) {
	cfg := t.TempDir()
	s := New(Options{Preferences: NewPreferencesStore(cfg), ClusterSettings: NewClusterSettingsStore(cfg)})
	defer s.Close()
	base := terminal.LocalOptions{KubeconfigPaths: []string{"/k"}, Context: "dev", Namespace: "from-filter"}
	opts, err := s.LocalTerminalOptions("dev [.kube/config]", base)
	if err != nil || opts.Dir != "" || opts.Namespace != "from-filter" || opts.Shell != "" || opts.Args != nil {
		t.Fatalf("without settings: %+v, %v", opts, err)
	}
	work := t.TempDir()
	if _, err := s.ChangePreferences(func(p *Preferences) error {
		p.TerminalShell, p.TerminalShellArgs = testShell(), []string{"-i"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("dev [.kube/config]", func(c *ClusterSettings) error {
		c.TerminalDir, c.TerminalNamespace = work, "team-a"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	opts, err = s.LocalTerminalOptions("dev [.kube/config]", base)
	if err != nil || opts.Dir != work || opts.Namespace != "team-a" || opts.Shell != testShell() || len(opts.Args) != 1 ||
		opts.KubeconfigPaths[0] != "/k" || opts.Context != "dev" {
		t.Errorf("with settings: %+v, %v", opts, err)
	}
	if other, _ := s.LocalTerminalOptions("prod", base); other.Dir != "" || other.Namespace != "from-filter" {
		t.Errorf("another cluster took dev's settings: %+v", other)
	}

	home, _ := os.UserHomeDir()
	if _, err := s.ChangeClusterSettings("dev [.kube/config]", func(c *ClusterSettings) error { c.TerminalDir = "$HOME"; return nil }); err != nil {
		t.Fatal(err)
	}
	if opts, _ := s.LocalTerminalOptions("dev [.kube/config]", base); opts.Dir != home {
		t.Errorf("$HOME resolved to %q, want %q", opts.Dir, home)
	}
	if _, err := s.ChangeClusterSettings("dev [.kube/config]", func(c *ClusterSettings) error {
		c.TerminalDir = filepath.Join(work, "gone")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LocalTerminalOptions("dev [.kube/config]", base); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("a missing working directory: %v", err)
	}
}

func TestSettingsSections(t *testing.T) {
	for _, c := range []struct{ page, in, want string }{
		{SettingsPreferences, "", "app"}, {SettingsPreferences, "terminal", "terminal"},
		{SettingsCluster, "", "general"}, {SettingsCluster, "metrics", "metrics"},
		{SettingsCluster, "integrations", "integrations"}, {SettingsCluster, "prometheus", "prometheus"},
		{SettingsCluster, "metrics-server", "metrics-server"},
	} {
		if got, err := CheckSection(c.page, c.in); err != nil || got != c.want {
			t.Errorf("%s %q: %q, %v", c.page, c.in, got, err)
		}
	}
	if _, err := CheckSection(SettingsCluster, "editor"); err == nil {
		t.Error("the cluster page took the editor section")
	}
	// The integrations after Agents, the heading's section first, then one per integration named as
	// it is (D-87).
	want := []string{"general", "proxy", "terminal", "namespaces", "metrics", "nodeShell", "agents", "integrations",
		"metrics-server", "prometheus", "alertmanager", "trivy", "kyverno"}
	if !slices.Equal(ClusterSettingsSections, want) {
		t.Errorf("cluster sections %q", ClusterSettingsSections)
	}
	for _, in := range Integrations {
		if got, ok := IntegrationOfSection(IntegrationSection(in.Name)); !ok || got.Name != in.Name {
			t.Errorf("%s: section %q gives %v %v", in.Name, IntegrationSection(in.Name), got.Name, ok)
		}
	}
	if _, ok := IntegrationOfSection("metrics"); ok {
		t.Error("metrics is an integration's section")
	}
}

// The one detection while shown runs while the question, Integrations, Metrics or a metrics
// integration's own section shows the context (D-87), not for another section or context.
func TestIntegrationsShownSections(t *testing.T) {
	for section, want := range map[string]bool{
		SectionIntegrations: true, SectionMetrics: true, "metrics-server": true, "prometheus": true,
		"alertmanager": false, "kyverno": false, SectionGeneral: false,
	} {
		u := &UIState{Settings: &SettingsPage{Page: SettingsCluster, Context: "ctx", Section: section}}
		if got := integrationsShown(u, "ctx"); got != want {
			t.Errorf("%s: %v", section, got)
		}
		if integrationsShown(u, "other") {
			t.Errorf("%s shown for another context", section)
		}
	}
	if !integrationsShown(&UIState{IntegrationsQuestion: "ctx"}, "ctx") {
		t.Error("the question")
	}
}
