package fyneui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// solidIcon is an opaque single-colour PNG resource, an uploaded icon without a corner of its own.
func solidIcon(t *testing.T, c color.NRGBA) fyne.Resource {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return fyne.NewStaticResource("solid.png", b.Bytes())
}

func sameRadius(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

// TestClusterIconCornersAsMacOS (the owner's VW10): a cluster's icon is rounded as macOS rounds an
// app icon, Cogpit's own included (22 on a side of 96), at every size it is drawn: the navigator's
// badge, the hotbar's tile and its "+", the settings' badge at the hotbar's size, a tab's; an
// uploaded image as much as the colour tile, so that the image is no longer drawn square.
func TestClusterIconCornersAsMacOS(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	if got := iconCorner(96); !sameRadius(got, 22) {
		t.Fatalf("iconCorner(96) = %v, want the app icon's 22", got)
	}
	img := solidIcon(t, color.NRGBA{R: 0xff, A: 0xff})
	for _, side := range []fyne.ThemeSizeName{sizeNavBadge, sizeHotbarTile} {
		b := newClusterBadge()
		b.side = side
		b.setIdentity(clusterIdentity{name: "x", initials: "X", ink: colorAccent})
		r := test.WidgetRenderer(b).(*clusterBadgeRenderer)
		want := iconCorner(r.sideOf())
		if !sameRadius(r.bg.CornerRadius, want) {
			t.Errorf("the %s tile's corner %v, want %v", side, r.bg.CornerRadius, want)
		}
		b.setIdentity(clusterIdentity{name: "x", initials: "X", ink: colorAccent, image: img})
		if !r.img.Visible() || !sameRadius(r.img.CornerRadius, want) {
			t.Errorf("the %s image's corner %v (shown %v), want %v", side, r.img.CornerRadius, r.img.Visible(), want)
		}
	}

	// Drawn: the image's corner pixel is the window's, its middle the image's.
	b := newClusterBadge()
	b.side = sizeHotbarTile
	b.setIdentity(clusterIdentity{name: "x", initials: "X", ink: colorAccent, image: img})
	w := test.NewTempWindow(t, container.NewCenter(b))
	w.Resize(fyne.NewSize(80, 80))
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	shot := w.Canvas().Capture()
	red := func(x, y float32) bool {
		r, g, bl, _ := shot.At(int(at.X+x), int(at.Y+y)).RGBA()
		return r>>8 > 0xe0 && g>>8 < 0x20 && bl>>8 < 0x20
	}
	side := b.Theme().Size(sizeHotbarTile)
	if !red(side/2, side/2) || !red(side/2, 1) || !red(1, side/2) {
		t.Error("the image is not drawn at its middle and edges")
	}
	for _, c := range [][2]float32{{0, 0}, {side - 1, 0}, {0, side - 1}, {side - 1, side - 1}, {1, 1}} {
		if red(c[0], c[1]) {
			t.Errorf("the image's corner pixel %v is drawn square", c)
		}
	}
}

// The hotbar's tiles, an image's and the "+", and a tab's badge take the same rounding.
func TestHotbarAndTabIconCorners(t *testing.T) {
	sh, _, q, f, _ := hotbarScene(t, ThemeDark)
	img := iconResource(t)
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"prod-eu-central": {Icon: iconFile}}
	})
	sh.icons = map[string]fyne.Resource{iconFile: img}
	sh.identitiesChanged()
	q.drain()
	th := sh.app.Settings().Theme()
	tile := th.Size(sizeHotbarTile)
	for i, tl := range tilesOf(sh.hot)[:2] {
		r := tileRenderer(tl)
		if !sameRadius(r.bg.CornerRadius, iconCorner(tile)) || !sameRadius(r.img.CornerRadius, iconCorner(tile-4)) {
			t.Errorf("tile %d: the square's corner %v, the image's %v; want %v and %v", i, r.bg.CornerRadius,
				r.img.CornerRadius, iconCorner(tile), iconCorner(tile-4))
		}
	}
	if r := tileRenderer(tilesOf(sh.hot)[1]); r.img.Resource != img || !r.img.Visible() {
		t.Errorf("prod-eu-central's tile shows no image: %v", r.img.Resource)
	}
	add := test.WidgetRenderer(sh.hot.add).(*hotbarButtonRenderer)
	if !sameRadius(add.bg.CornerRadius, iconCorner(tile)) {
		t.Errorf("the +'s corner %v, want the tiles' %v", add.bg.CornerRadius, iconCorner(tile))
	}

	tsh, _, tq, tf := tabsShell(t, ThemeDark)
	tf.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {Icon: iconFile}}
	})
	tsh.icons = map[string]fyne.Resource{iconFile: img}
	tsh.identitiesChanged()
	tq.drain()
	r := tabRenderer(tsh.tabs.shown[0])
	_, side, _, _ := r.dims()
	if !r.img.Visible() || !sameRadius(r.img.CornerRadius, iconCorner(side)) || !sameRadius(r.badge.CornerRadius, iconCorner(side)) {
		t.Errorf("a tab's badge %v and image %v (shown %v), want %v", r.badge.CornerRadius, r.img.CornerRadius, r.img.Visible(), iconCorner(side))
	}
}
