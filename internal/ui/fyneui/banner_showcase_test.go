package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	logoart "github.com/guided-traffic/cogpit/internal/ui/logo"
)

// The README's banner (A111; showcase_test.go's pictures): Cogpit's logo, its name and one
// sentence, on the theme's background, in the dark and the light theme like every other picture.
// It is a render rather than an SVG: an SVG's text would be drawn in whatever font the reader's
// browser has, the render draws it in the app's own. The repository's social preview is the same
// layout at GitHub's 1280×640.
const (
	bannerWidth   = 1280 // points; the PNG is showcaseScale times as large
	bannerHeight  = 320
	bannerLogo    = 200 // the logo's side
	socialHeight  = 640 // the social preview's height; its width is the banner's
	socialLogo    = 280
	bannerGap     = 48  // between the logo and the text
	bannerName    = 112 // the name's text size
	bannerLine    = 34  // the sentence's text size
	bannerLeading = 4   // between the name and the sentence
	// bannerRise lifts the text: its boxes hold room above the name's capitals and below the
	// sentence's descenders, so that centring the boxes left the glyphs 17 points below the logo's
	// centre (measured on the render).
	bannerRise     = 17
	bannerSentence = "A fast, native Kubernetes IDE for your desktop."
)

// 0. The banner at the README's top.
func TestShowcaseBanner(t *testing.T) {
	eachTheme(t, func(t *testing.T, mode ThemeMode) {
		a := test.NewTempApp(t)
		th := newTheme(mode)
		a.Settings().SetTheme(th)
		w := test.NewTempWindow(t, showcaseBanner(th, bannerHeight, bannerLogo))
		w.SetPadded(false)
		size := fyne.NewSize(bannerWidth, bannerHeight)
		w.Resize(size)
		saveShowcaseSized(t, w, "banner", mode, size)
	})
}

// The repository's social preview (owner), the picture of a link to it: the banner at 1280×640 in
// the dark theme. GitHub takes it only by hand, under Settings › General › Social preview; its API
// has no way to set it.
func TestShowcaseSocialPreview(t *testing.T) {
	showcaseOnly(t)
	a := test.NewTempApp(t)
	th := newTheme(ThemeDark)
	a.Settings().SetTheme(th)
	w := test.NewTempWindow(t, showcaseBanner(th, socialHeight, socialLogo))
	w.SetPadded(false)
	size := fyne.NewSize(bannerWidth, socialHeight)
	w.Resize(size)
	saveShowcaseSized(t, w, "social_preview", ThemeDark, size)
}

// showcaseBanner lays a banner of the given height out: the logo and the two lines of text beside
// it, centred as a group. The logo is the current version's tile (internal/ui/logo, D-24), as the
// header and the welcome page show it, rasterised at the size the capture shows it at; the name
// and the sentence take the theme's text colour.
func showcaseBanner(th fyne.Theme, height, side float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(themeColor(th, theme.ColorNameBackground))
	bg.Resize(fyne.NewSize(bannerWidth, height))

	logo := canvas.NewImageFromImage(logoart.Current.Tile(int(side) * showcaseScale))
	logo.FillMode, logo.ScaleMode = canvas.ImageFillContain, canvas.ImageScaleSmooth

	name := canvas.NewText("Cogpit", themeColor(th, theme.ColorNameForeground))
	name.TextSize, name.TextStyle = bannerName, fyne.TextStyle{Bold: true}
	line := canvas.NewText(bannerSentence, themeColor(th, theme.ColorNameForeground))
	line.TextSize = bannerLine

	ns, ls := name.MinSize(), line.MinSize()
	group := side + bannerGap + max(ns.Width, ls.Width)
	x := (bannerWidth - group) / 2
	logo.Move(fyne.NewPos(x, (height-side)/2))
	logo.Resize(fyne.NewSize(side, side))
	tx := x + side + bannerGap
	y := (height-(ns.Height+bannerLeading+ls.Height))/2 - bannerRise
	name.Move(fyne.NewPos(tx, y))
	name.Resize(ns)
	line.Move(fyne.NewPos(tx, y+ns.Height+bannerLeading))
	line.Resize(ls)
	return container.NewWithoutLayout(bg, logo, name, line)
}
