package fyneui

import (
	"image/color"
	"io"
	"log"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The window's menus on Windows and Linux (the owner, 2026-10-21, after the first look at Cogpit on
// Ubuntu, three bars on top of each other, GNOME's title bar, Fyne's menu bar and Cogpit's own:
// "zwei Leisten, nativ"): the system's title bar stays, with its buttons and everything the window
// manager does with it, and the menus' titles move into Cogpit's top bar, at its left before the
// logo, instead of Fyne's menu bar as a row of its own. On macOS the menus are the system's menu
// bar (Fyne's native menu) and Cogpit's bar is the window's title bar (titlebar.go).
//
// Fyne draws its menu bar for any main menu on Windows and Linux, and it also looks a typed shortcut
// up in the main menu before the focused widget is offered it, which keeps ⌃⇧P, ⌃P and the other
// menu keys working while a text field has the keys (a field swallows every shortcut it does not
// know). So the window keeps a main menu for the keys alone (installMenuKeys): set once, empty,
// while the window exists, which makes Fyne build no menu bar (Fyne 2.8's glCanvas.buildMenu, read
// in its source), then filled with the menus, and after every rebuild of the menus refilled in
// place; Fyne reads it when a key comes. Before the window exists (fyneui.Run, before ShowAndRun)
// Fyne would build its bar later, from the filled menu, so there the keys wait for the window's
// start (deferMenuKeys).
//
// The lead's decisions (2026-10-21), open to revision: the titles in the bar's text, a hover pill
// behind them as the bar's other buttons have; one menu open at a time, under its title below the
// bar; while one is open, the pointer on another title opens that one, a click outside or Escape
// closes it, ← and → go to the neighbouring menu where no submenu takes them; Alt no longer opens
// the menus, as Fyne's bar did (the command palette, ⌃⇧P, reaches every command from the
// keyboard); Quit at the end of the first menu, as Fyne added it there.

// quitLabel is the first menu's last item on Windows and Linux, as Fyne's own menu bar added it.
const quitLabel = "Quit"

// setMainMenu builds the window's menus and puts them where this system shows them: the system's
// menu bar on macOS, Cogpit's top bar elsewhere (menuBar), with the window's main menu kept for the
// menus' keys.
func (sh *shell) setMainMenu() {
	m := sh.mainMenu()
	if !sh.inlineMenus {
		sh.win.SetMainMenu(m)
		return
	}
	if len(m.Items) > 0 {
		first := m.Items[0]
		first.Items = append(first.Items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem(quitLabel, sh.app.Quit))
	}
	sh.top.menus.set(m)
	sh.menuItems = m.Items
	if sh.menuKeys != nil {
		sh.menuKeys.Items = m.Items // in place: the window reads them when a key comes
	} else if !sh.deferMenuKeys {
		sh.installMenuKeys()
	}
}

// installMenuKeys gives the window the main menu it looks shortcuts up in, without the menu bar Fyne
// would draw for it: empty while Fyne builds it, filled at once after (see above). Fyne logs that a
// main menu must have a child menu; that line is kept out of Cogpit's log, since the menu is empty
// on purpose.
func (sh *shell) installMenuKeys() {
	sh.deferMenuKeys = false
	keys := &fyne.MainMenu{}
	out := log.Writer()
	log.SetOutput(io.Discard)
	sh.win.SetMainMenu(keys)
	log.SetOutput(out)
	keys.Items = sh.menuItems
	sh.menuKeys = keys
}

// menuPlace is where the window's menus are.
type menuPlace int

// Where the menus are: this system's place, the system's menu bar, Cogpit's top bar.
const (
	menusHere menuPlace = iota
	menusNative
	menusInBar
)

// inBar reports whether the menus are drawn in Cogpit's top bar: everywhere but macOS, unless a
// test chose.
func (p menuPlace) inBar() bool {
	switch p {
	case menusNative:
		return false
	case menusInBar:
		return true
	}
	return runtime.GOOS != goosMac
}

// menuBar is the menus' titles in the top bar and the menu open under one (Windows and Linux).
type menuBar struct {
	sh     *shell
	root   *fyne.Container // the titles
	titles []*menuTitle
	menus  []*fyne.Menu
	open   *menuOverlay // the menu open now; nil while none is
}

func newMenuBar(sh *shell) *menuBar {
	return &menuBar{sh: sh, root: container.NewHBox()}
}

// set shows the menus' titles. A menu that is open opens again with its rebuilt items when the
// titles stay the same (the menus are rebuilt when a cluster connects or its kinds come), else it
// closes.
func (b *menuBar) set(m *fyne.MainMenu) {
	b.menus = m.Items
	same := len(b.titles) == len(m.Items)
	for i := 0; same && i < len(m.Items); i++ {
		same = b.titles[i].label == m.Items[i].Label
	}
	if same {
		if b.open != nil {
			b.show(b.open.index)
		}
		return
	}
	b.close()
	b.titles = b.titles[:0]
	objs := make([]fyne.CanvasObject, 0, len(m.Items))
	for i, menu := range m.Items {
		t := newMenuTitle(b, i, menu.Label)
		b.titles = append(b.titles, t)
		objs = append(objs, box(compactLayout{}, t))
	}
	b.root.Objects = objs
	b.root.Refresh()
}

// toggle opens the menu of title i, or closes it when it is the one open.
func (b *menuBar) toggle(i int) {
	if b.open != nil && b.open.index == i {
		b.close()
		return
	}
	b.show(i)
}

// show opens the menu of title i under its title, closing the one open.
func (b *menuBar) show(i int) {
	if i < 0 || i >= len(b.menus) || i >= len(b.titles) {
		return
	}
	c := b.sh.win.Canvas()
	b.close()
	o := newMenuOverlay(b, i)
	b.open = o
	b.titles[i].setOpen(true)
	c.Overlays().Add(o)
	onEscape(c, o, b.close) // while the menu does not have the keyboard (escape.go)
	o.Resize(c.Size())
	o.place()
	c.Focus(o)
}

// close closes the menu open, if one is.
func (b *menuBar) close() {
	o := b.open
	if o == nil {
		return
	}
	b.open = nil
	if o.index < len(b.titles) {
		b.titles[o.index].setOpen(false)
	}
	b.sh.win.Canvas().Overlays().Remove(o)
}

// menuTitle is one menu's title in the top bar.
type menuTitle struct {
	widget.BaseWidget
	bar     *menuBar
	index   int
	label   string
	hovered bool
	opened  bool
}

var (
	_ fyne.Tappable     = (*menuTitle)(nil)
	_ desktop.Hoverable = (*menuTitle)(nil)
)

func newMenuTitle(b *menuBar, i int, label string) *menuTitle {
	t := &menuTitle{bar: b, index: i, label: label}
	t.ExtendBaseWidget(t)
	return t
}

func (t *menuTitle) setOpen(on bool) {
	if t.opened != on {
		t.opened = on
		t.Refresh()
	}
}

// Tapped opens the title's menu, or closes it.
func (t *menuTitle) Tapped(*fyne.PointEvent) { t.bar.toggle(t.index) }

// MouseIn implements desktop.Hoverable.
func (t *menuTitle) MouseIn(*desktop.MouseEvent) {
	t.hovered = true
	t.Refresh()
}

// MouseMoved implements desktop.Hoverable.
func (t *menuTitle) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (t *menuTitle) MouseOut() {
	t.hovered = false
	t.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (t *menuTitle) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	text := canvas.NewText(t.label, color.Black)
	r := &menuTitleRenderer{t: t, bg: bg, text: text}
	r.Refresh()
	return r
}

type menuTitleRenderer struct {
	t    *menuTitle
	bg   *canvas.Rectangle
	text *canvas.Text
}

func (r *menuTitleRenderer) MinSize() fyne.Size {
	th := r.t.Theme()
	ts := fyne.MeasureText(r.t.label, th.Size(theme.SizeNameText), fyne.TextStyle{})
	pad := th.Size(theme.SizeNameInnerPadding)
	return fyne.NewSize(ts.Width+2*pad, ts.Height)
}

func (r *menuTitleRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	ts := r.text.MinSize()
	r.text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
	r.text.Resize(ts)
}

func (r *menuTitleRenderer) Refresh() {
	th := r.t.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	r.text.Text = r.t.label
	r.text.TextSize = th.Size(theme.SizeNameText)
	r.text.Color = th.Color(theme.ColorNameForeground, v)
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	switch {
	case r.t.opened:
		r.bg.FillColor = th.Color(theme.ColorNamePressed, v)
	case r.t.hovered:
		r.bg.FillColor = th.Color(theme.ColorNameHover, v)
	default:
		r.bg.FillColor = color.Transparent
	}
	r.Layout(r.t.Size())
	r.bg.Refresh()
	r.text.Refresh()
}

func (r *menuTitleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.text}
}

func (r *menuTitleRenderer) Destroy() {}

// menuOverlay is the open menu's layer over the window, as Fyne's own menu bar has one: the menu
// under its title, the titles' places, where the pointer opens their menu, and the rest, a click on
// which closes the menu. It takes the keys while open.
type menuOverlay struct {
	widget.BaseWidget
	bar   *menuBar
	index int
	menu  *widget.Menu
	under *menuUnderlay
	zones []*menuZone
}

var _ fyne.Focusable = (*menuOverlay)(nil)

func newMenuOverlay(b *menuBar, i int) *menuOverlay {
	o := &menuOverlay{bar: b, index: i}
	o.ExtendBaseWidget(o)
	o.menu = widget.NewMenu(b.menus[i])
	o.menu.OnDismiss = func() {
		if b.open == o {
			b.close()
		}
	}
	o.under = &menuUnderlay{close: b.close}
	o.under.ExtendBaseWidget(o.under)
	for j := range b.titles {
		z := &menuZone{bar: b, index: j}
		z.ExtendBaseWidget(z)
		o.zones = append(o.zones, z)
	}
	return o
}

// place puts the zones over the titles and the menu under its title, below the top bar, inside the
// window.
func (o *menuOverlay) place() {
	d := fyne.CurrentApp().Driver()
	size := o.Size()
	o.under.Move(fyne.Position{})
	o.under.Resize(size)
	for j, z := range o.zones {
		if j >= len(o.bar.titles) {
			break
		}
		t := o.bar.titles[j]
		z.Move(d.AbsolutePositionForObject(t))
		z.Resize(t.Size())
	}
	title := o.bar.titles[o.index]
	pos := d.AbsolutePositionForObject(title)
	top := o.bar.sh.top.root
	pos.Y = d.AbsolutePositionForObject(top).Y + top.Size().Height
	ms := o.menu.MinSize()
	if pos.X+ms.Width > size.Width {
		pos.X = max(0, size.Width-ms.Width)
	}
	if pos.Y+ms.Height > size.Height {
		ms.Height = max(0, size.Height-pos.Y)
	}
	o.menu.Move(pos)
	o.menu.Resize(ms)
}

// Resize implements fyne.Widget: the layer covers the window.
func (o *menuOverlay) Resize(size fyne.Size) {
	o.BaseWidget.Resize(size)
	o.place()
}

// FocusGained implements fyne.Focusable.
func (o *menuOverlay) FocusGained() {}

// FocusLost implements fyne.Focusable.
func (o *menuOverlay) FocusLost() {}

// TypedRune implements fyne.Focusable.
func (o *menuOverlay) TypedRune(rune) {}

// TypedKey moves through the menu as Fyne's pop-up menu does; ← and → go to the neighbouring menu
// where no submenu takes them, and Escape closes it.
func (o *menuOverlay) TypedKey(e *fyne.KeyEvent) {
	n := len(o.bar.menus)
	switch e.Name {
	case fyne.KeyDown:
		o.menu.ActivateNext()
	case fyne.KeyUp:
		o.menu.ActivatePrevious()
	case fyne.KeyEnter, fyne.KeyReturn, fyne.KeySpace:
		o.menu.TriggerLast()
	case fyne.KeyEscape:
		o.bar.close()
	case fyne.KeyRight:
		if !o.menu.ActivateLastSubmenu() && n > 0 {
			o.bar.show((o.index + 1) % n)
		}
	case fyne.KeyLeft:
		if !o.menu.DeactivateLastSubmenu() && n > 0 {
			o.bar.show((o.index + n - 1) % n)
		}
	}
}

// CreateRenderer implements fyne.Widget.
func (o *menuOverlay) CreateRenderer() fyne.WidgetRenderer {
	objs := []fyne.CanvasObject{o.under}
	for _, z := range o.zones {
		objs = append(objs, z)
	}
	return widget.NewSimpleRenderer(container.NewWithoutLayout(append(objs, o.menu)...))
}

// menuUnderlay is the open menu's layer outside the menu and the titles: a click there closes it.
type menuUnderlay struct {
	widget.BaseWidget
	close func()
}

// Tapped closes the menu.
func (u *menuUnderlay) Tapped(*fyne.PointEvent) { u.close() }

// TappedSecondary closes the menu.
func (u *menuUnderlay) TappedSecondary(*fyne.PointEvent) { u.close() }

// CreateRenderer implements fyne.Widget.
func (u *menuUnderlay) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// menuZone is a title's place in the open menu's layer: the pointer on it opens its menu, a click
// opens it or, on the open menu's own title, closes it.
type menuZone struct {
	widget.BaseWidget
	bar   *menuBar
	index int
}

var (
	_ fyne.Tappable     = (*menuZone)(nil)
	_ desktop.Hoverable = (*menuZone)(nil)
)

// Tapped opens the zone's menu, or closes it when it is open.
func (z *menuZone) Tapped(*fyne.PointEvent) { z.bar.toggle(z.index) }

// MouseIn opens the zone's menu in place of the one open.
func (z *menuZone) MouseIn(*desktop.MouseEvent) {
	if z.bar.open != nil && z.bar.open.index != z.index {
		z.bar.show(z.index)
	}
}

// MouseMoved implements desktop.Hoverable.
func (z *menuZone) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (z *menuZone) MouseOut() {}

// CreateRenderer implements fyne.Widget.
func (z *menuZone) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}
