package fyneui

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// zoomEngine is a real engine with the real commands and preferences kept in a temporary
// directory.
func zoomEngine(t *testing.T) *engine.Session {
	t.Helper()
	s := newTestEngine(engine.Options{Preferences: engine.NewPreferencesStore(t.TempDir())})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// keptZoom is the zoom level the preferences keep.
func keptZoom(t *testing.T, s *engine.Session) int {
	t.Helper()
	p, err := s.Preferences()
	if err != nil {
		t.Fatal(err)
	}
	return p.ZoomLevel
}

// TestZoomStatusBar (D-134): the status bar's zoom right of Diagnostics: the magnifier with + zooms
// in, the percent shows the zoom and a click on it resets to 100%, the magnifier with − zooms out
// and is greyed with the reason at the smallest zoom; the percent keeps one width for every level.
func TestZoomStatusBar(t *testing.T) {
	s := zoomEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	z := sh.status.zoom
	at := func(level int) {
		t.Helper()
		q.until(t, "zoom "+engine.ZoomPercent(level), func() bool {
			return sh.zoom.level == level && z.pct.level == level && keptZoom(t, s) == level
		})
	}
	if z.pct.level != 0 || z.out.off || z.in.off {
		t.Fatalf("start: level %d, out off %v, in off %v; want 0, both on", z.pct.level, z.out.off, z.in.off)
	}
	width := z.pct.MinSize().Width

	test.Tap(z.in)
	at(1)
	if !strings.HasPrefix(z.in.tip, "Zoom In (") {
		t.Errorf("the + magnifier's tooltip %q, want Zoom In and its keys", z.in.tip)
	}
	if !strings.HasPrefix(z.pct.tip, "Reset Zoom (") {
		t.Errorf("the percent's tooltip at 120%% %q, want Reset Zoom and its keys", z.pct.tip)
	}
	if got := z.pct.MinSize().Width; got != width {
		t.Errorf("the percent is %v wide at 120%%, %v at 100%%: the magnifiers move", got, width)
	}
	test.Tap(z.pct)
	at(0)
	for l := -1; l >= engine.MinZoomLevel; l-- {
		test.Tap(z.out)
		at(l)
	}
	if !z.out.off || !strings.Contains(z.out.tip, "smallest zoom") {
		t.Errorf("at %s: the − magnifier off %v, tooltip %q; want greyed with the reason", engine.ZoomPercent(engine.MinZoomLevel), z.out.off, z.out.tip)
	}
	test.Tap(z.out) // greyed: nothing
	if q.drain(); keptZoom(t, s) != engine.MinZoomLevel {
		t.Errorf("a tap on the greyed magnifier zoomed to %d", keptZoom(t, s))
	}
}

// TestZoomMenuAndKeys (D-134): View › Zoom In, Zoom Out and Reset Zoom carry VS Code's keys as
// main-menu shortcuts (from any focus); the further keys ride on separators of a menu of their own,
// which the main menu has and the top bar does not draw, and run the command.
func TestZoomMenuAndKeys(t *testing.T) {
	s := zoomEngine(t)
	sh, _, q := newEngineShell(t, s, Options{menus: menusInBar})
	var view *fyne.Menu
	for _, m := range sh.mainMenu().Items {
		if m.Label == "View" {
			view = m
		}
	}
	if view == nil {
		t.Fatal("no View menu")
	}
	want := map[string]fyne.KeyName{"Zoom In": fyne.KeyEqual, "Zoom Out": fyne.KeyMinus, "Reset Zoom": fyne.Key0}
	for _, it := range view.Items {
		key, ok := want[it.Label]
		if !ok {
			continue
		}
		sc, isCustom := it.Shortcut.(*desktop.CustomShortcut)
		if !isCustom || sc.KeyName != key || sc.Modifier != fyne.KeyModifierShortcutDefault {
			t.Errorf("View › %s: shortcut %#v, want the default modifier and %q", it.Label, it.Shortcut, key)
		}
		delete(want, it.Label)
	}
	if len(want) > 0 {
		t.Errorf("View lacks %v", want)
	}

	// The further keys: in the window's key menu, not in the top bar.
	var keys *fyne.Menu
	for _, m := range sh.menuItems {
		if m.Label == "" {
			keys = m
		}
	}
	if keys == nil {
		t.Fatal("the main menu has no menu of further keys")
	}
	for _, m := range sh.top.menus.menus {
		if m == keys {
			t.Error("the top bar draws the menu of further keys")
		}
	}
	plus := &desktop.CustomShortcut{KeyName: fyne.KeyPlus, Modifier: fyne.KeyModifierShortcutDefault}
	var names []string
	var run func()
	for _, it := range keys.Items {
		if !it.IsSeparator || it.Shortcut == nil || it.Action == nil {
			t.Errorf("a further key %#v: want a separator with a shortcut and an action", it)
			continue
		}
		names = append(names, it.Shortcut.ShortcutName())
		if it.Shortcut.ShortcutName() == plus.ShortcutName() {
			run = it.Action
		}
	}
	wantNames := 3 // ⌘⇧= and ⌘+ for Zoom In, ⌘⇧- for Zoom Out
	if runtime.GOOS == "linux" {
		wantNames = 2 // Ctrl+Shift+- is Go Forward there
	}
	if len(names) != wantNames || run == nil {
		t.Fatalf("further keys %v, want %d with the keypad's +", names, wantNames)
	}
	run()
	q.until(t, "⌘+ zoomed in", func() bool { return keptZoom(t, s) == 1 })
}

// TestWindowZoomScale: the zoom sets FYNE_SCALE to the scale asked for at the start times the
// level's factor, and 100% gives back FYNE_SCALE as Cogpit was started with it.
func TestWindowZoomScale(t *testing.T) {
	var value string
	var set bool
	z := &windowZoom{base: 1.5, orig: "1.5", origSet: true, setenv: func(v string, ok bool) { value, set = v, ok }}
	if !z.set(1) || value != "1.800" || !set {
		t.Errorf("120%% on 1.5: FYNE_SCALE %q set %v, want 1.800", value, set)
	}
	if z.set(1) {
		t.Error("the same level again reported a change")
	}
	if !z.set(engine.MaxZoomLevel+3) || z.level != engine.MaxZoomLevel {
		t.Errorf("beyond the range: level %d, want %d", z.level, engine.MaxZoomLevel)
	}
	if !z.set(0) || value != "1.5" || !set {
		t.Errorf("100%%: FYNE_SCALE %q set %v, want the start's 1.5", value, set)
	}
	z = &windowZoom{base: 1, setenv: func(v string, ok bool) { value, set = v, ok }}
	z.set(-1)
	if value != "0.800" || !set {
		t.Errorf("80%%: FYNE_SCALE %q set %v, want 0.800", value, set)
	}
	z.set(0)
	if set {
		t.Errorf("100%% without FYNE_SCALE at the start: left %q set", value)
	}
}

// TestShortcutEqualsAndPlus: the registry's "=" and "Plus" bind Fyne's = and + keys and show as
// the platform writes them.
func TestShortcutEqualsAndPlus(t *testing.T) {
	for in, key := range map[string]fyne.KeyName{"Cmd+=": fyne.KeyEqual, "Cmd+Shift+=": fyne.KeyEqual, "Cmd+Plus": fyne.KeyPlus} {
		if sc := parseShortcut(in); sc == nil || sc.KeyName != key {
			t.Errorf("%s: %#v, want %q", in, sc, key)
		}
	}
	for in, want := range map[string]string{"Cmd+=": "⌘=", "Cmd+Plus": "⌘+", "Cmd+0": "⌘0"} {
		if got := shortcutText(in, true); got != want {
			t.Errorf("%s on macOS: %q, want %q", in, got, want)
		}
	}
	if got := shortcutText("Cmd+=", false); got != "Ctrl+=" {
		t.Errorf("Cmd+= elsewhere: %q, want Ctrl+=", got)
	}
}

// TestZoomFitsWindow (D-134): Zoom In stops where the window's content would no longer fit the
// window, since Fyne would widen it: the + greyed with the reason, the key a toast, an agent's
// ui.zoom.in refused; a larger window zooms further.
func TestZoomFitsWindow(t *testing.T) {
	s := zoomEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	least := sh.root.MinSize()
	w.Resize(fyne.NewSize(least.Width*1.1, least.Height*1.1)) // room for 100%, not for 120%
	settle(w)
	z := sh.status.zoom
	if !z.in.off || !strings.Contains(z.in.tip, "too small for 120%") {
		t.Fatalf("a window 1.1 times the content: + off %v, tooltip %q; want greyed, too small for 120%%", z.in.off, z.in.tip)
	}
	if err := s.ZoomInError(0); !errors.Is(err, engine.ErrZoomFit) {
		t.Errorf("the session's Zoom In at 100%%: %v, want ErrZoomFit", err)
	}
	sh.zoomIn() // the key
	q.drain()
	if keptZoom(t, s) != 0 {
		t.Errorf("the key zoomed to %d in a window too small", keptZoom(t, s))
	}
	w.Resize(fyne.NewSize(least.Width*1.5, least.Height*1.5)) // room for 140%
	settle(w)
	if z.in.off {
		t.Fatalf("a window 1.5 times the content: + still off (%q)", z.in.tip)
	}
	sh.zoomIn()
	q.until(t, "120%", func() bool { return keptZoom(t, s) == 1 })
}
