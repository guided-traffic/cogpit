package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The title bar's appearance toggle (the owner, 2026-10-21: "einen Button links neben der Glocke …
// zum Toggle zwischen light / dark mode"): one click shows the other look. It sets the preference
// Preferences › App › Theme through preferences.set, the command the Preferences page and agents use,
// so the choice is engine state and survives a restart; a Sync with computer setting becomes the
// explicit look the click asked for. The icon is the look a click goes to: a sun in the dark, a moon
// in the light.

// iconSun and iconMoon are drawn for Cogpit, each one path so that the theme recolours all of it: a
// disc with eight rays, and a crescent.
var (
	iconSun = theme.NewThemedResource(fyne.NewStaticResource("cogpit-sun.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="`+
			`M12 7.6a4.4 4.4 0 1 0 0 8.8a4.4 4.4 0 1 0 0-8.8z`+
			`M19.4 13L22.4 13L22.4 11L19.4 11z M16.53 17.94L18.65 20.06L20.06 18.65L17.94 16.53z`+
			`M11 19.4L11 22.4L13 22.4L13 19.4z M6.06 16.53L3.94 18.65L5.35 20.06L7.47 17.94z`+
			`M4.6 11L1.6 11L1.6 13L4.6 13z M7.47 6.06L5.35 3.94L3.94 5.35L6.06 7.47z`+
			`M13 4.6L13 1.6L11 1.6L11 4.6z M17.94 7.47L20.06 5.35L18.65 3.94L16.53 6.06z"/></svg>`)))
	iconMoon = theme.NewThemedResource(fyne.NewStaticResource("cogpit-moon.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="`+
			`M14.2 2.9A9.2 9.2 0 1 0 21.1 15.3A7.4 7.4 0 0 1 14.2 2.9z"/></svg>`)))
)

// themeToggle is the button left of the bell.
type themeToggle struct {
	widget.BaseWidget
	sh      *shell
	hovered bool
	// unfixed draws the button as a run without --theme shows it: the README's showcase renders
	// choose their theme the way --theme does and show the button a user sees (saveShowcase).
	unfixed bool
}

var (
	_ fyne.Tappable     = (*themeToggle)(nil)
	_ desktop.Hoverable = (*themeToggle)(nil)
)

func newThemeToggle(sh *shell) *themeToggle {
	b := &themeToggle{sh: sh}
	b.ExtendBaseWidget(b)
	return b
}

// shownDark reports whether the window shows the dark look now: the theme's mode resolved against
// the system's appearance, as cogTheme resolves it.
func (b *themeToggle) shownDark() bool {
	if t, ok := b.sh.app.Settings().Theme().(*cogTheme); ok {
		return t.variant(b.sh.app.Settings().ThemeVariant()) == theme.VariantDark
	}
	return b.sh.app.Settings().ThemeVariant() == theme.VariantDark
}

// fixed reports whether this run's look was fixed with --theme: the preference then applies from the
// next start only (preferencesChanged), so the button changes nothing now and says why.
func (b *themeToggle) fixed() bool { return b.sh.opts.Theme != "" && !b.unfixed }

// tipText is the button's tooltip.
func (b *themeToggle) tipText() string {
	if b.fixed() {
		return "This run was started with --theme " + string(b.sh.opts.Theme) + "; Preferences › App › Theme applies from the next start"
	}
	if b.shownDark() {
		return "Switch to light"
	}
	return "Switch to dark"
}

// Tapped implements fyne.Tappable: the other look, as Preferences › App › Theme would set it.
func (b *themeToggle) Tapped(*fyne.PointEvent) {
	b.sh.tip.hideFor(b)
	if b.fixed() {
		return
	}
	next := ThemeLight
	if !b.shownDark() {
		next = ThemeDark
	}
	b.sh.dispatchInOrder("preferences.set", map[string]any{"theme": string(next)}, nil)
}

// MouseIn implements desktop.Hoverable.
func (b *themeToggle) MouseIn(ev *desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
	b.sh.tip.hover(b, b.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *themeToggle) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *themeToggle) MouseOut() {
	b.hovered = false
	b.Refresh()
	b.sh.tip.hideFor(b)
}

// CreateRenderer implements fyne.Widget.
func (b *themeToggle) CreateRenderer() fyne.WidgetRenderer {
	r := &themeToggleRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), img: newIconImage()}
	r.Refresh()
	return r
}

type themeToggleRenderer struct {
	b   *themeToggle
	bg  *canvas.Rectangle
	img *iconImage
}

// MinSize is the bell's, so that the two stand as a pair.
func (r *themeToggleRenderer) MinSize() fyne.Size {
	th := r.b.Theme()
	side := bellIcon(th) + 2*th.Size(theme.SizeNameInnerPadding)
	return fyne.NewSize(side, side)
}

func (r *themeToggleRenderer) Layout(size fyne.Size) {
	is := bellIcon(r.b.Theme()) - 2 // the sun's rays reach the box's edge; a little smaller than the bell reads as its size
	r.bg.Resize(size)
	r.img.img.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
	r.img.img.Resize(fyne.NewSize(is, is))
}

func (r *themeToggleRenderer) Refresh() {
	th := r.b.Theme()
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	if r.b.hovered && !r.b.fixed() {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	}
	var icon fyne.Resource = iconMoon
	ink := theme.ColorNameForeground
	if r.b.shownDark() {
		icon = iconSun
	}
	if r.b.fixed() {
		ink = theme.ColorNameDisabled
	}
	r.img.set(th, icon, ink)
	r.Layout(r.b.Size())
	r.bg.Refresh()
}

func (r *themeToggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.img.img}
}

func (r *themeToggleRenderer) Destroy() {}
