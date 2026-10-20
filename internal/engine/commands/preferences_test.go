package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// settingsKubeconfig is a user's kubeconfig the settings commands must never write: one context
// whose server nobody answers (no command here connects).
const settingsKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: dev
  cluster:
    server: https://127.0.0.1:1
users:
- name: dev
  user:
    token: secret-token
contexts:
- name: kind-dev
  context:
    cluster: dev
    user: dev
current-context: kind-dev
`

// settingsSession is a session over a real kubeconfig file (never connected) with every
// preference kept in its own temporary folder.
func settingsSession(t *testing.T) (s *engine.Session, cfg, kubeconfig string) {
	t.Helper()
	cfg = filepath.Join(t.TempDir(), "Cogpit")
	kubeconfig = filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfig, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s = engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}, IncludeDemo: true},
		Preferences:     engine.NewPreferencesStore(cfg),
		ClusterSettings: engine.NewClusterSettingsStore(cfg),
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, cfg, kubeconfig
}

func dispatch(t *testing.T, s *engine.Session, origin engine.Origin, id string, params any, out any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: raw, Origin: origin})
	if err == nil && out != nil {
		b, _ := json.Marshal(res)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	return err
}

// Agents set the theme, the time zone and the editor freely (UI commands, D-2); the local
// terminal's shell and arguments only the GUI may set, since they name a program to run.
func TestPreferencesCommands(t *testing.T) {
	s, _, _ := settingsSession(t)
	agent := engine.OriginExternalAgent
	var res commands.PreferencesResult
	if err := dispatch(t, s, agent, "preferences.set", map[string]any{"theme": "light", "timezone": "UTC", "editorTabSize": 4, "editorLineNumbers": false}, &res); err != nil {
		t.Fatal(err)
	}
	if p := res.Preferences; p.Theme != "light" || p.Timezone != "UTC" || p.EditorTabSize != 4 || p.EditorLineNumbers || res.Timezone != "UTC" || !res.Kept {
		t.Errorf("after an agent's set: %+v", res)
	}
	if u := s.UIState().Preferences; u == nil || u.Theme != "light" {
		t.Errorf("ui_state shows %+v", u)
	}
	// Less motion is the agents' too, either way: it changes only how the window moves.
	for _, on := range []bool{true, false} {
		var got commands.PreferencesResult // omitempty: a false is left out of the result
		if err := dispatch(t, s, agent, "preferences.set", map[string]any{"reduceMotion": on}, &got); err != nil || got.Preferences.ReduceMotion != on {
			t.Errorf("an agent's reduceMotion %v: %v, %v", on, err, got.Preferences.ReduceMotion)
		}
	}
	for _, origin := range []engine.Origin{agent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		for _, params := range []map[string]any{{"terminalShell": "/bin/sh"}, {"terminalShellArgs": []string{"-c", "touch /tmp/x"}}} {
			if err := dispatch(t, s, origin, "preferences.set", params, nil); !errors.Is(err, engine.ErrShellGUIOnly) {
				t.Errorf("%s set %v: %v, want ErrShellGUIOnly", origin, params, err)
			}
		}
	}
	if p, _ := s.Preferences(); p.TerminalShell != "" || p.TerminalShellArgs != nil {
		t.Errorf("a refused shell was kept: %+v", p)
	}
	if runtime.GOOS != "windows" {
		if err := dispatch(t, s, engine.OriginGUI, "preferences.set", map[string]any{"terminalShell": "/bin/sh", "terminalShellArgs": []string{"-l"}}, &res); err != nil {
			t.Fatal(err)
		}
		if res.Preferences.TerminalShell != "/bin/sh" || !slices.Equal(res.Preferences.TerminalShellArgs, []string{"-l"}) {
			t.Errorf("the user's shell: %+v", res.Preferences)
		}
	}
	if err := dispatch(t, s, engine.OriginGUI, "preferences.set", map[string]any{"terminalShell": "/no/such/shell"}, nil); err == nil {
		t.Error("a missing shell accepted")
	}
	if err := dispatch(t, s, agent, "preferences.set", map[string]any{"theme": "neon"}, nil); err == nil {
		t.Error("theme neon accepted")
	}

	// The page is UI state: opened at a section, closed again.
	if err := dispatch(t, s, agent, "preferences.open", map[string]any{"section": "terminal"}, nil); err != nil {
		t.Fatal(err)
	}
	if p := s.UIState().Settings; p == nil || p.Page != engine.SettingsPreferences || p.Section != "terminal" {
		t.Errorf("open: %+v", p)
	}
	if err := dispatch(t, s, agent, "preferences.open", map[string]any{"section": "nope"}, nil); err == nil {
		t.Error("an unknown section opened")
	}
	if err := dispatch(t, s, agent, "settings.close", struct{}{}, nil); err != nil || s.UIState().Settings != nil {
		t.Errorf("close: %v, %+v", err, s.UIState().Settings)
	}
}

// The palette offers both pages through the registry's metadata (MenuPath, no required
// parameter), with their shortcuts; the setters, the getters and settings.close are agents' tools.
func TestPaletteOpensTheSettingsPages(t *testing.T) {
	cfg := t.TempDir()
	h := newHarness(t, seed(), engine.Options{Preferences: engine.NewPreferencesStore(cfg), ClusterSettings: engine.NewClusterSettingsStore(cfg)})
	entries := paletteOf(h, nil)
	prefs, ok := entryOf(entries, "preferences.open")
	if !ok || prefs.Label != "Preferences" || prefs.Shortcut != "Cmd+," || prefs.Reason != "" {
		t.Errorf("preferences.open entry %+v", prefs)
	}
	cluster, ok := entryOf(entries, "cluster.settings.open")
	if !ok || cluster.Label != "Cluster Settings" || cluster.Shortcut != "Cmd+Shift+," || cluster.Category != "Cluster" || cluster.Reason != "" {
		t.Errorf("cluster.settings.open entry %+v", cluster)
	}
	for _, id := range []string{"preferences.get", "preferences.set", "settings.close", "cluster.settings.get", "cluster.settings.set",
		"cluster.settings.uploadIcon", "cluster.settings.resetIcon"} {
		if e, ok := entryOf(entries, id); ok {
			t.Errorf("%s offered: %+v", id, e)
		}
	}
	h.must(t, engine.OriginGUI, "cluster.settings.open", struct{}{}, nil) // the connected cluster's, as ⌘⇧,
	if p := h.s.UIState().Settings; p == nil || p.Context != "test" || p.Section != engine.SectionGeneral {
		t.Errorf("⌘⇧, opened %+v", p)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A context's settings change through the commands, for any context a source offers; the name, the
// icon colour, the upload and the reset are the GUI's (H-125); none of it writes the kubeconfig, and
// everything lands in Cogpit's own folder.
func TestClusterSettingsCommandsNeverTouchTheKubeconfig(t *testing.T) {
	s, cfg, kubeconfig := settingsSession(t)
	before, err := os.Stat(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	agent := engine.OriginExternalAgent
	var v engine.ClusterSettingsView
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "kind-dev",
		"terminalDir": "$HOME/src", "terminalNamespace": "team-a"}, &v); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev", "iconColor": "Violet"}, &v); err != nil {
		t.Fatal(err)
	}
	if v.Name != "Dev" || !v.NameSet || v.IconColor != "violet" || v.TerminalDir != "$HOME/src" || v.TerminalNamespace != "team-a" ||
		v.ContextName != "kind-dev" || v.Connected || !v.Kept {
		t.Errorf("after the agent's and the user's set: %+v", v)
	}
	if err := dispatch(t, s, agent, "cluster.settings.set", map[string]any{"context": "nope", "terminalDir": "$HOME"}, nil); err == nil {
		t.Error("settings for a context no source offers")
	}
	if err := dispatch(t, s, agent, "cluster.settings.get", struct{}{}, nil); err == nil {
		t.Error("get without a context or a connection")
	}

	img := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(img, testPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []engine.Origin{agent, engine.OriginBuiltInAgent, engine.OriginCLI} {
		if err := dispatch(t, s, origin, "cluster.settings.uploadIcon", map[string]any{"context": "kind-dev", "path": img}, nil); !errors.Is(err, engine.ErrIconGUIOnly) {
			t.Errorf("%s upload: %v, want ErrIconGUIOnly", origin, err)
		}
	}
	v = engine.ClusterSettingsView{} // omitted fields stay empty
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.uploadIcon", map[string]any{"context": "kind-dev", "path": img}, &v); err != nil {
		t.Fatal(err)
	}
	if v.IconImage != "png" || v.IconColor != "" {
		t.Errorf("after the upload: %+v", v)
	}
	v = engine.ClusterSettingsView{}
	if err := dispatch(t, s, engine.OriginGUI, "cluster.settings.resetIcon", map[string]any{"context": "kind-dev"}, &v); err != nil || v.IconImage != "" || v.IconColor != "" {
		t.Errorf("reset: %+v, %v", v, err)
	}
	if err := dispatch(t, s, agent, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "terminal"}, nil); err != nil {
		t.Fatal(err)
	}
	if p := s.UIState().Settings; p == nil || p.Page != engine.SettingsCluster || p.Context != "kind-dev" || p.Section != "terminal" {
		t.Errorf("open: %+v", p)
	}
	if err := dispatch(t, s, agent, "cluster.settings.open", struct{}{}, nil); err == nil {
		t.Error("⌘⇧, opened without a connected cluster")
	}
	if err := dispatch(t, s, agent, "preferences.set", map[string]any{"theme": "system"}, nil); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(kubeconfig); string(raw) != settingsKubeconfig || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		t.Errorf("the kubeconfig changed: %s", raw)
	}
	var files []string
	_ = filepath.WalkDir(cfg, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(cfg, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(files) != 2 || !slices.Contains(files, engine.PreferencesFile) || !slices.Contains(files, engine.ClusterSettingsFile) {
		t.Errorf("Cogpit's folder holds %q, want the two settings files (the reset icon removed)", files)
	}
	if raw, _ := os.ReadFile(filepath.Join(cfg, engine.ClusterSettingsFile)); strings.Contains(string(raw), "secret-token") {
		t.Error("a credential reached the settings file")
	}
}
