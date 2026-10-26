package commands_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestZoom: ui.zoom.in and ui.zoom.out step the kept zoom level by one, within -4 to 4, each greyed
// at its end; ui.zoom.reset sets it back to 0, greyed there; agents may zoom (D-134).
func TestZoom(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{Preferences: engine.NewPreferencesStore(t.TempDir())})
	zoom := func(origin engine.Origin, id string) commands.ZoomResult {
		t.Helper()
		var r commands.ZoomResult
		h.must(t, origin, id, struct{}{}, &r)
		return r
	}
	enabled := func(id string) error {
		t.Helper()
		c, ok := h.s.Registry().Get(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		return c.Enabled(h.s, json.RawMessage(`{}`))
	}
	if err := enabled("ui.zoom.reset"); !errors.Is(err, engine.ErrZoomReset) {
		t.Errorf("reset at 100%%: %v, want ErrZoomReset", err)
	}
	if r := zoom(engine.OriginGUI, "ui.zoom.in"); r.ZoomLevel != 1 || r.Zoom != "120%" {
		t.Errorf("zoom in: %+v, want level 1, 120%%", r)
	}
	if r := zoom(engine.OriginExternalAgent, "ui.zoom.in"); r.ZoomLevel != 2 || r.Zoom != "140%" {
		t.Errorf("an agent's zoom in: %+v, want level 2, 140%%", r)
	}
	for range 2 {
		zoom(engine.OriginGUI, "ui.zoom.in")
	}
	if err := enabled("ui.zoom.in"); !errors.Is(err, engine.ErrZoomMax) {
		t.Errorf("zoom in at %d: %v, want ErrZoomMax", engine.MaxZoomLevel, err)
	}
	if err := h.run(t, engine.OriginGUI, "ui.zoom.in", struct{}{}, nil); err == nil {
		t.Error("zoom in beyond the largest level ran")
	}
	if p, _ := h.s.Preferences(); p.ZoomLevel != engine.MaxZoomLevel {
		t.Errorf("kept level %d, want %d", p.ZoomLevel, engine.MaxZoomLevel)
	}
	if r := zoom(engine.OriginGUI, "ui.zoom.reset"); r.ZoomLevel != 0 || r.Zoom != "100%" {
		t.Errorf("reset: %+v, want level 0, 100%%", r)
	}
	for range -engine.MinZoomLevel {
		zoom(engine.OriginBuiltInAgent, "ui.zoom.out")
	}
	if err := enabled("ui.zoom.out"); !errors.Is(err, engine.ErrZoomMin) {
		t.Errorf("zoom out at %d: %v, want ErrZoomMin", engine.MinZoomLevel, err)
	}
	if p, _ := h.s.Preferences(); p.ZoomLevel != engine.MinZoomLevel {
		t.Errorf("kept level %d, want %d", p.ZoomLevel, engine.MinZoomLevel)
	}
	// preferences.set takes a level in the range and refuses one beyond it.
	h.must(t, engine.OriginGUI, "preferences.set", map[string]any{"zoomLevel": 3}, nil)
	if p, _ := h.s.Preferences(); p.ZoomLevel != 3 {
		t.Errorf("preferences.set zoomLevel 3: kept %d", p.ZoomLevel)
	}
	if err := h.run(t, engine.OriginGUI, "preferences.set", map[string]any{"zoomLevel": 5}, nil); err == nil {
		t.Error("preferences.set zoomLevel 5 was kept")
	}
}

// TestZoomKeys: the zoom's keys and Go Back's and Go Forward's are VS Code's on the platform Cogpit
// runs on (D-134, its windowActions.ts and editorActions.ts).
func TestZoomKeys(t *testing.T) {
	type keys struct {
		shortcut string
		more     []string
	}
	want := map[string]map[string]keys{
		"darwin": {
			"ui.zoom.in": {"Cmd+=", []string{"Cmd+Shift+=", "Cmd+Plus"}}, "ui.zoom.out": {"Cmd+-", []string{"Cmd+Shift+-"}},
			"ui.zoom.reset": {"Cmd+0", nil}, "ui.tab.back": {"Ctrl+-", nil}, "ui.tab.forward": {"Ctrl+Shift+-", nil},
		},
		"windows": {
			"ui.zoom.in": {"Cmd+=", []string{"Cmd+Shift+=", "Cmd+Plus"}}, "ui.zoom.out": {"Cmd+-", []string{"Cmd+Shift+-"}},
			"ui.zoom.reset": {"Cmd+0", nil}, "ui.tab.back": {"Alt+Left", nil}, "ui.tab.forward": {"Alt+Right", nil},
		},
		"linux": {
			"ui.zoom.in": {"Cmd+=", []string{"Cmd+Shift+=", "Cmd+Plus"}}, "ui.zoom.out": {"Cmd+-", nil},
			"ui.zoom.reset": {"Cmd+0", nil}, "ui.tab.back": {"Ctrl+Alt+-", nil}, "ui.tab.forward": {"Ctrl+Shift+-", nil},
		},
	}
	for goos, cmds := range want {
		r := engine.NewRegistryFor(goos)
		if err := commands.RegisterAll(r); err != nil {
			t.Fatal(err)
		}
		for id, k := range cmds {
			c, ok := r.Get(id)
			if !ok {
				t.Fatalf("%s: %s not registered", goos, id)
			}
			if c.Shortcut != k.shortcut || len(c.MoreKeys) != len(k.more) {
				t.Errorf("%s %s: %q %v, want %q %v", goos, id, c.Shortcut, c.MoreKeys, k.shortcut, k.more)
				continue
			}
			for i := range k.more {
				if c.MoreKeys[i] != k.more[i] {
					t.Errorf("%s %s: further keys %v, want %v", goos, id, c.MoreKeys, k.more)
				}
			}
		}
	}
}

// TestKeysDoNotCollideOnAnyPlatform: on macOS, Windows and Linux no two commands share a key, the
// further keys included (D-134); "Cmd" is Ctrl on Windows and Linux.
func TestKeysDoNotCollideOnAnyPlatform(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		r := engine.NewRegistryFor(goos)
		if err := commands.RegisterAll(r); err != nil {
			t.Fatal(err)
		}
		bound := map[string]string{}
		for _, c := range r.Commands() {
			for _, key := range append([]string{c.Shortcut}, c.MoreKeys...) {
				if key == "" || strings.Contains(key, "–") {
					continue
				}
				if goos != "darwin" {
					key = strings.ReplaceAll(key, "Cmd+", "Ctrl+")
				}
				mods := strings.Split(key, "+")
				last := mods[len(mods)-1]
				mods = mods[:len(mods)-1]
				slices.Sort(mods)
				norm := strings.Join(append(mods, last), "+")
				if other, ok := bound[norm]; ok {
					t.Errorf("%s: %s and %s share %s", goos, c.ID, other, key)
				}
				bound[norm] = c.ID
			}
		}
	}
}
