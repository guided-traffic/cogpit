package fyneui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	logoart "github.com/guided-traffic/cogpit/internal/ui/logo"
)

// A cluster's identity (Lens: Cluster settings › General, the name and the icon, "Upload Icon",
// "Pick Icon Color", "Reset Icon"; captured 2026-10-11): the name it shows and its icon, the
// initials on a tinted square in a colour of the theme's accent palette, or an image the user
// uploaded. The hotbar's tiles, the navigator's rows and root and the welcome page show the same
// identity, read from UIState.ClusterSettings (engine state, so a change from Cluster Settings shows
// everywhere at once; only the user changes it, in the window, engine.ErrClusterIdentityGUIOnly);
// the image's bytes come from the engine's kept copy (Session.ClusterIcon), loaded off the main
// goroutine once per image.

// iconCorner is the corner radius of a cluster's icon drawn side points wide: macOS's rounding, as
// Cogpit's own app icon has it (logo.CornerRatio, 22 on 96), at every size, an image and a colour
// tile alike (the owner's VW10 of 2026-10-24, D-119). Before, an image was drawn square and a tile
// rounded by a fixed 7 points, rounder than this at the navigator's size and the tab's.
func iconCorner(side float32) float32 { return side * float32(logoart.CornerRatio) }

// clusterIdentity is how a context shows itself.
type clusterIdentity struct {
	name     string              // the display name, else the context's name without its file
	initials string              // the letters the user set (up to five, D-119), else up to two of the name
	ink      fyne.ThemeColorName // the icon colour's token; the accent by default
	image    fyne.Resource       // the uploaded icon; nil without one, while it loads, or when unreadable
}

// iconSource reads a context's uploaded icon: the real engine (*engine.Session). A test fake
// without it draws initials.
type iconSource interface {
	ClusterIcon(context string) (data []byte, format string, err error)
}

// identity returns a context's identity from the UI state. Main goroutine.
func (sh *shell) identity(context string) clusterIdentity {
	cs := sh.ui.ClusterSettings[context]
	name := engine.ClusterName(context, cs)
	id := clusterIdentity{name: name, initials: nameInitials(name), ink: colorAccent}
	if cs.IconLetters != "" {
		id.initials = cs.IconLetters
	}
	if cs.IconColor != "" {
		id.ink = identityColorName(cs.IconColor)
	}
	if cs.Icon != "" && !cs.IconTile { // the tile chosen keeps the image for later (D-119)
		id.image = sh.icon(context, cs.Icon)
	}
	return id
}

// nameInitials are a name's initials: a context's as the sidebar takes them ("kind-cogpit" → "KC"),
// and a display name's words count as parts too ("Dev cluster" → "DC").
func nameInitials(name string) string { return initials(strings.Join(strings.Fields(name), "-")) }

// icon returns an uploaded icon as a resource; the first call for a file reads it off the main
// goroutine and returns nil, and the identities are shown again once it arrived. A file that
// cannot be read stays nil (the initials show) until another file takes its place.
func (sh *shell) icon(context, file string) fyne.Resource {
	if r, ok := sh.icons[file]; ok {
		return r
	}
	src, ok := sh.eng.(iconSource)
	if !ok || sh.iconLoading[file] {
		return nil
	}
	if sh.iconLoading == nil {
		sh.iconLoading = map[string]bool{}
	}
	sh.iconLoading[file] = true
	go func() {
		defer sh.recoverTo("cluster icon")
		data, _, err := src.ClusterIcon(context)
		sh.do(func() {
			delete(sh.iconLoading, file)
			if sh.icons == nil {
				sh.icons = map[string]fyne.Resource{}
			}
			var r fyne.Resource
			if err == nil && len(data) > 0 {
				r = fyne.NewStaticResource(file, data) // the name's extension tells Fyne an SVG
			}
			sh.icons[file] = r
			sh.identitiesChanged()
		})
	}()
	return nil
}

// identitiesChanged shows the clusters' identities again wherever they appear: the navigator's
// root and section, the hotbar's tiles, the status bar (the name, D-68), the settings page's
// title, the tabs and the welcome page.
func (sh *shell) identitiesChanged() {
	sh.side.updateHeader()
	sh.status.update()
	sh.hot.tiles.Refresh()
	sh.settings.identityChanged()
	sh.tabs.sync()
}

// clusterBadge is a cluster's icon: its initials on a tinted square in its icon colour (the
// accent by default), or its uploaded image, either with macOS's rounded corners (iconCorner);
// "–" in the muted colour without a cluster. The
// navigator's root and rows, the welcome page and the settings page show it; the hotbar's tiles draw the
// same in their own renderer.
type clusterBadge struct {
	widget.BaseWidget
	text  string
	muted bool
	ink   fyne.ThemeColorName // empty: the accent
	img   fyne.Resource
	side  fyne.ThemeSizeName // the square's side; sizeNavBadge by default
}

func newClusterBadge() *clusterBadge {
	b := &clusterBadge{text: "–", muted: true, side: sizeNavBadge}
	b.ExtendBaseWidget(b)
	return b
}

// set changes the initials; muted draws them without a cluster's colour or image.
func (b *clusterBadge) set(text string, muted bool) {
	if text == b.text && muted == b.muted && b.ink == "" && b.img == nil {
		return
	}
	b.text, b.muted, b.ink, b.img = text, muted, "", nil
	b.Refresh()
}

// setIdentity shows a cluster's identity.
func (b *clusterBadge) setIdentity(id clusterIdentity) {
	if id.initials == b.text && !b.muted && id.ink == b.ink && id.image == b.img {
		return
	}
	b.text, b.muted, b.ink, b.img = id.initials, false, id.ink, id.image
	b.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (b *clusterBadge) CreateRenderer() fyne.WidgetRenderer {
	r := &clusterBadgeRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), txt: canvas.NewText("", color.Transparent),
		img: &canvas.Image{FillMode: canvas.ImageFillContain, ScaleMode: canvas.ImageScaleSmooth}}
	r.txt.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type clusterBadgeRenderer struct {
	b   *clusterBadge
	bg  *canvas.Rectangle
	txt *canvas.Text
	img *canvas.Image
}

func (r *clusterBadgeRenderer) sideOf() float32 { return r.b.Theme().Size(r.b.side) }

func (r *clusterBadgeRenderer) MinSize() fyne.Size {
	side := r.sideOf()
	return fyne.NewSize(side, side)
}

func (r *clusterBadgeRenderer) Layout(size fyne.Size) {
	side := r.sideOf()
	x, y := (size.Width-side)/2, (size.Height-side)/2
	r.bg.Move(fyne.NewPos(x, y))
	r.bg.Resize(fyne.NewSize(side, side))
	r.img.Move(fyne.NewPos(x, y))
	r.img.Resize(fyne.NewSize(side, side))
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(x+(side-ts.Width)/2, y+(side-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *clusterBadgeRenderer) Refresh() {
	th := r.b.Theme()
	ink := colorAccent
	switch {
	case r.b.muted:
		ink = colorMuted
	case r.b.ink != "":
		ink = r.b.ink
	}
	c := themeColor(th, ink)
	r.bg.FillColor = withAlpha(c, 0x2c) // the hotbar tile's tint
	r.bg.CornerRadius = iconCorner(r.sideOf())
	r.img.CornerRadius = r.bg.CornerRadius // the image rounded as the tile (VW10)
	r.txt.Text = r.b.text
	r.txt.Color = c
	base := th.Size(theme.SizeNameText)
	if r.sideOf() > th.Size(sizeNavBadge) {
		base = th.Size(theme.SizeNameSubHeadingText)
	}
	r.txt.TextSize = fitTextSize(r.b.text, base, r.sideOf()-4) // up to five letters of the user's (D-119)
	if r.b.img != nil && !r.b.muted {
		r.img.Resource = r.b.img
		r.img.Show()
		r.txt.Hide()
		r.bg.FillColor = color.Transparent
	} else {
		r.img.Resource = nil
		r.img.Hide()
		r.txt.Show()
	}
	r.Layout(r.b.Size())
	r.bg.Refresh()
	r.txt.Refresh()
	r.img.Refresh()
}

func (r *clusterBadgeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.img, r.txt}
}
func (r *clusterBadgeRenderer) Destroy() {}
