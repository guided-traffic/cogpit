package fyneui

import (
	"image"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	logoart "github.com/guided-traffic/cogpit/internal/ui/logo"
)

// TestHeaderMark: the header's logo is the current version's tile (D-24; owner, 2026-10-15), 20
// pixels, the same picture in both themes.
func TestHeaderMark(t *testing.T) {
	want := logoart.Current.Tile(20)
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			a := test.NewTempApp(t)
			a.Settings().SetTheme(newTheme(mode))
			l := newLogo()
			w := test.NewTempWindow(t, l)
			w.Resize(fyne.NewSize(40, 40))

			if got := l.MinSize(); got.Width != 20 || got.Height != 20 {
				t.Fatalf("the mark is %v, want 20×20", got)
			}
			img, ok := test.WidgetRenderer(l.(fyne.Widget)).Objects()[0].(*canvas.Image).Image.(*image.NRGBA)
			if !ok || img.Bounds().Dx() != 20 {
				t.Fatalf("the mark's picture is %T, want a 20-pixel NRGBA", img)
			}
			if string(img.Pix) != string(want.Pix) {
				t.Error("the mark is not the current logo's tile")
			}
			if got := img.NRGBAAt(0, 0); got.A != 0 {
				t.Errorf("the corner is %v, want transparent: the plate is rounded", got)
			}
			if got := img.NRGBAAt(10, 10); got.A != 0xff {
				t.Errorf("the centre is %v, want the opaque plate", got)
			}
		})
	}
}

// TestLogoTileDrawnOncePerSize: every widget that shows the logo at one size shares one drawing of
// it (logoTile, D-112), the current version's tile; another size is a drawing of its own.
func TestLogoTileDrawnOncePerSize(t *testing.T) {
	a, b := logoTile(24), logoTile(24)
	if a != b {
		t.Error("the logo was drawn again for a size it had been drawn at")
	}
	if want := logoart.Current.Tile(24); string(a.Pix) != string(want.Pix) {
		t.Error("the shared tile is not the current logo's tile")
	}
	if c := logoTile(25); c == a || c.Bounds().Dx() != 25 {
		t.Errorf("25 pixels gave %v, the 24-pixel tile's %v", c.Bounds(), a.Bounds())
	}
}
