package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Every icon colour the engine offers has a token in both variants, and a cluster's initials in it
// read at 3:1 where they are drawn: the hotbar (the header's surface), the navigator (the
// sidebar's), the input background (the context picker's list until D-57; dialogs' fields) and the
// settings page (the background).
func TestIdentityColorsContrast(t *testing.T) {
	for name, p := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		for _, c := range engine.IconColors {
			ink, ok := p.fyne[identityColorName(c)]
			if !ok {
				t.Errorf("%s: no token for %s", name, c)
				continue
			}
			for _, bg := range []fyne.ThemeColorName{theme.ColorNameHeaderBackground, colorSidebar, theme.ColorNameInputBackground, theme.ColorNameBackground} {
				if got := contrastRatio(ink, p.fyne[bg]); got < 3.0 {
					t.Errorf("%s: %s on %s is %.2f:1, want 3:1", name, c, bg, got)
				}
			}
		}
	}
}

// A cluster's identity: its display name or context name, initials of either, and its colour.
func TestIdentityFromTheClusterSettings(t *testing.T) {
	for in, want := range map[string]string{"kind-cogpit": "KC", "Dev cluster": "DC", "prod": "PR", "  Two  Words ": "TW"} {
		if got := nameInitials(in); got != want {
			t.Errorf("nameInitials(%q) = %q, want %q", in, got, want)
		}
	}
	f := newFakeEngine(t)
	f.ui.ClusterSettings = map[string]engine.ClusterSettings{"kind-dev [.kube/config]": {Name: "Dev cluster", IconColor: "green"}}
	sh, _, q := newTestShell(t, f, Options{})
	q.drain()
	id := sh.identity("kind-dev [.kube/config]")
	if id.name != "Dev cluster" || id.initials != "DC" || id.ink != identityColorName("green") || id.image != nil {
		t.Errorf("set: %+v", id)
	}
	if id := sh.identity("prod-eu [.kube/prod.yaml]"); id.name != "prod-eu" || id.initials != "PE" || id.ink != colorAccent {
		t.Errorf("default: %+v", id)
	}
	// A fake without icons draws the initials even when a file is named.
	sh.ui.ClusterSettings = map[string]engine.ClusterSettings{"kind-dev [.kube/config]": {Icon: "x.png"}}
	if id := sh.identity("kind-dev [.kube/config]"); id.image != nil || id.initials != "KD" {
		t.Errorf("icon without a source: %+v", id)
	}
}

// TestCustomIdentityColorContrast (A93): a freely picked colour is drawn in a shade of its hue that
// keeps 3:1 on every identity surface of both themes, whatever was picked (white on light, black
// on dark, a pale yellow, a navy), and a colour that is legible already stays as picked.
func TestCustomIdentityColorContrast(t *testing.T) {
	for _, c := range []string{"#ffffff", "#000000", "#ffff66", "#000080", "#808080", "#ff0000", "#00ff00", "#3366ff", "#1f7f78"} {
		for name, v := range map[string]fyne.ThemeVariant{"dark": theme.VariantDark, "light": theme.VariantLight} {
			shade, ok := customIdentityColor(identityColorName(c), v)
			if !ok {
				t.Fatalf("%s: not a custom colour", c)
			}
			p := paletteFor(v)
			for _, bg := range identitySurfaces {
				if got := contrastRatio(shade, p.fyne[bg]); got < 3.0 {
					t.Errorf("%s in %s: %v on %s is %.2f:1, want 3:1", c, name, shade, bg, got)
				}
			}
		}
	}
	if blue, _ := customIdentityColor(identityColorName("#3366ff"), theme.VariantDark); blue.B <= blue.R || blue.B <= blue.G {
		t.Errorf("the hue moved: %v", blue)
	}
	if _, ok := customIdentityColor(identityColorName("green"), theme.VariantDark); ok {
		t.Error("an accent's name read as a custom colour")
	}
}

// The Internal icon's dialog with a colour picked freely (A93, D-119): its swatch shows selected
// beside the eight, the picker button after it; the PNGs of the pull request.
func TestRenderClusterSettingsCustomColorDark(t *testing.T) {
	renderClusterSettingsCustomColor(t, ThemeDark, "cluster_settings_custom_color_dark.png")
}

func TestRenderClusterSettingsCustomColorLight(t *testing.T) {
	renderClusterSettingsCustomColor(t, ThemeLight, "cluster_settings_custom_color_light.png")
}

func renderClusterSettingsCustomColor(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "#FFE066"})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the page", func() bool {
		e, _ := sh.settings.controls["name"].(*widget.Entry)
		return sh.settings.visible() && e != nil && e.Text == "Dev cluster"
	})
	sh.settings.openTileDialog("kind-dev")
	q.until(t, "the custom swatch", func() bool {
		sw := sh.settings.tileDialog.custom
		return sw.Visible() && sw.selected && sw.color == "#ffe066"
	})
	q.drain()
	savePNG(t, w, name)
}
