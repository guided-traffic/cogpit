package fyneui

import (
	"image/color"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The tab bar of cluster views above the workspace, right of the navigator (ui-parity › Layout;
// the tabs are engine state, engine/tabs.go). Captured from Lens Desktop (2026-10-11): a tab reads
// "<view> - <context>" with the cluster's icon and a close button, the preview tab's title is
// italic (that italic means preview is inferred), hovering a tab shows its full title; a single
// click in the navigator opens a preview tab, a double click or ⇧↵ a fixed tab. Cogpit's defaults
// (inferred, not the owner's decision, 2026-10-13):
//   - a tab's icon is the cluster's icon as the navigator and the hotbar's tiles show it (cluster
//     settings, clusteridentity.go): its uploaded image, else its initials badge in its icon colour,
//     the accent by default (the owner's F25, 2026-10-17); muted while its cluster is not connected,
//     an image faded (D-69 stage 2: a tab of another connected cluster is not muted);
//   - the bar takes the dock's tab colours; the active tab is raised and underlined in the accent
//     colour, as the dock's active tab is;
//   - a title wider than the dock's widest tab title is cut with an ellipsis; tabs that do not fit
//     scroll sideways, the active one scrolled into view;
//   - a double click on a preview tab fixes it (ui.tab.pin), as editors do;
//   - the bar is hidden while there is no tab (before the first connect);
//   - the shortcuts are main-menu items of the View menu, so they work whatever has the focus:
//     ⌘W, ⌘⌥→ and ⌘⌥←, ⌘1–⌘8 and ⌘9 (the last tab), ⌃- and ⌃⇧-; on Windows and Linux they are the
//     Ctrl forms of the same keys, not Lens's Ctrl+PageUp/PageDown and Alt+←/→ (G22);
//   - a tab dragged along the bar moves there (ui.tab.move; stripdrag.go, the owner's GW4), and
//     ⌘⌥⇧← and ⌘⌥⇧→ (View › Move Tab Left and Right) move the active tab one place.
//
// The bar changes the tabs only through ui.tab.* commands: UI state is engine state.

// doubleClickTime is how close two clicks on the same tab, navigator item or list header separator
// make a double click: macOS's default interval (Fyne's own double tap holds every single tap for
// the system's interval, 0.5 s by default on macOS and 300 ms elsewhere, which would slow every
// click on the navigator, so the bar, the navigator and the list's table count clicks themselves).
const doubleClickTime = 500 * time.Millisecond

// tabBar is the bar of tabs.
type tabBar struct {
	sh     *shell
	root   *fyne.Container // the bar and its divider; hidden while there is no tab
	row    *fyne.Container
	scroll *container.Scroll
	shown  []*viewTab // the tab widgets in order
	// welcome is the workspace's page while no tab is active (welcome.go).
	welcome *welcomePage
	errPage *errorPage // a failed connect's page over the workspace (errorpage.go, D-114)

	now      func() time.Time // the clock of double clicks (a test moves it)
	tabClick click            // the last click on a tab
	navClick click            // the last click on a navigator item
	drag     *stripDrag       // a tab dragged to another place (stripdrag.go, GW4)
}

// click is a click on something with an id, for telling a double click.
type click struct {
	id string
	at time.Time
}

func newTabBar(sh *shell) *tabBar {
	b := &tabBar{sh: sh, now: time.Now}
	b.drag = newStripDrag(nil, sh.reduceMotion)
	b.row = container.New(&stripLayout{d: b.drag})
	b.drag.row = b.row
	b.drag.idOf = func(o fyne.CanvasObject) string {
		if t, ok := o.(*viewTab); ok {
			return t.tab.ID
		}
		return ""
	}
	b.drag.shownIDs = func() []string {
		ids := make([]string, len(b.shown))
		for i, t := range b.shown {
			ids[i] = t.tab.ID
		}
		return ids
	}
	b.drag.move = func(id string, index int, failed func()) {
		sh.dispatch("ui.tab.move", tabMoveParams{ID: id, Index: index}, func(_ any, err error) {
			if err != nil {
				sh.showError(err)
				failed()
			}
		})
	}
	b.drag.click = func(o fyne.CanvasObject, at fyne.Position) {
		t, ok := o.(*viewTab)
		switch {
		case !ok:
		case pressedOn(t.close, at):
			t.close.OnTapped()
		default:
			b.tapped(t.tab)
		}
	}
	b.drag.lifting = func() { sh.tip.hide() }
	b.scroll = container.NewHScroll(b.row)
	b.root = container.NewBorder(nil, newDivider(false), nil, nil, container.NewStack(newSurface(colorDock), b.scroll))
	b.root.Hide()
	b.welcome = newWelcomePage(sh)
	b.errPage = newErrorPage(sh)
	return b
}

// double reports whether a click on id is the second of a double click, and remembers it.
func (b *tabBar) double(last *click, id string) bool {
	now := b.now()
	if last.id == id && now.Sub(last.at) >= 0 && now.Sub(last.at) < doubleClickTime {
		*last = click{}
		return true
	}
	*last = click{id: id, at: now}
	return false
}

// sync shows the shown cluster's tabs (UIState.TabsShown, wherever the navigator stands; every tab
// while no cluster is shown; engine/navscope.go): one widget per tab, the active one raised and
// scrolled into view.
func (b *tabBar) sync() {
	defer b.welcome.sync() // the welcome page while the workspace has nothing else (welcome.go)
	defer b.errPage.sync() // a failed connect's page (errorpage.go, D-114)
	ui := b.sh.ui
	tabs := ui.TabsShown()
	if len(tabs) == 0 {
		b.drag.cancel()
		b.shown, b.row.Objects = nil, nil
		b.root.Hide()
		b.row.Refresh()
		return
	}
	for len(b.shown) < len(tabs) {
		b.shown = append(b.shown, newViewTab(b))
	}
	b.shown = b.shown[:len(tabs)]
	objs := make([]fyne.CanvasObject, len(tabs))
	var active *viewTab
	for i, t := range tabs {
		w := b.shown[i]
		// Muted while its cluster is not connected; interrupted counts as connected (D-58), and so
		// does a connected cluster the window does not show (D-69).
		w.set(t, t.ID == ui.ActiveTab, clusterConnected(ui, t.Context), b.sh.identity(t.Context))
		if w.active {
			active = w
		}
		objs[i] = w
	}
	b.row.Objects = objs
	b.drag.follow() // a tab carried or its move pending (stripdrag.go)
	b.root.Show()
	b.scroll.Refresh()
	if active != nil {
		b.reveal(active)
	}
}

// reveal scrolls a tab into view.
func (b *tabBar) reveal(t *viewTab) {
	x, w, view, off := t.Position().X, t.Size().Width, b.scroll.Size().Width, b.scroll.Offset.X
	switch {
	case view <= 0:
	case x < off:
		b.scroll.ScrollToOffset(fyne.NewPos(x, 0))
	case x+w > off+view:
		b.scroll.ScrollToOffset(fyne.NewPos(x+w-view, 0))
	}
}

// tapped is a click on a tab: it activates the tab, and the second click of a double click on the
// preview tab fixes it.
func (b *tabBar) tapped(t engine.ViewTab) {
	b.sh.dock.keysLast = false // Move Tab's keys are the tab bar's again (tabmovekeys.go)
	if b.double(&b.tabClick, t.ID) && t.Preview {
		b.sh.dispatch("ui.tab.pin", tabIDParams{ID: t.ID}, nil)
		return
	}
	ui := b.sh.ui
	if t.ID != ui.ActiveTab || !ui.Connection.State.Live() {
		b.sh.dispatch("ui.tab.select", tabSelectParams{ID: t.ID}, nil)
	}
}

// menuItems are the View menu's tab items with the registry's shortcuts: main-menu items, so that
// the keys work whatever has the focus. An item does nothing where its command could not run (no
// tab, nowhere to go back to), rather than showing why in a toast at every keypress.
func (b *tabBar) menuItems() []*fyne.MenuItem {
	sh := b.sh
	var items []*fyne.MenuItem
	add := func(id string, run func()) {
		if c, ok := sh.eng.Registry().Get(id); ok {
			items = append(items, sh.withShortcut(fyne.NewMenuItem(c.Label, run), id))
		}
	}
	whenTabs := func(id string) func() {
		return func() {
			sh.dock.keysLast = false // the tab bar's keys: Move Tab's are its own again (tabmovekeys.go)
			if len(sh.ui.TabsShown()) > 0 {
				sh.dispatch(id, struct{}{}, nil)
			}
		}
	}
	add("ui.tab.close", func() {
		if sh.ui.ActiveTab != "" { // the welcome page shows no tab (welcome.go): nothing to close
			sh.dispatch("ui.tab.close", struct{}{}, nil)
		}
	})
	add("ui.tab.pin", func() {
		if t, ok := sh.ui.Tab(sh.ui.ActiveTab); ok && t.Preview {
			sh.dispatch("ui.tab.pin", struct{}{}, nil)
		}
	})
	add("ui.tab.next", whenTabs("ui.tab.next"))
	add("ui.tab.previous", whenTabs("ui.tab.previous"))
	// Move Tab Left and Right (GW4): the active tab of the bar the keyboard works in, the dock's
	// included (AW10, tabmovekeys.go), only where it has a place to go.
	add("ui.tab.moveLeft", func() { sh.stepTab("ui.tab.moveLeft", -1) })
	add("ui.tab.moveRight", func() { sh.stepTab("ui.tab.moveRight", 1) })
	add("ui.tab.back", b.history(true))
	add("ui.tab.forward", b.history(false))
	if c, ok := sh.eng.Registry().Get("ui.tab.select"); ok {
		var goTo []*fyne.MenuItem
		for n := 1; n <= 9; n++ {
			label, params := "Tab "+string(rune('0'+n)), tabSelectParams{Index: n}
			if n == 9 {
				label, params = "Last Tab", tabSelectParams{Last: true}
			}
			item := fyne.NewMenuItem(label, func() {
				sh.dock.keysLast = false
				if n := len(sh.ui.TabsShown()); n >= params.Index && n > 0 { // the tabs the bar shows
					sh.dispatch(c.ID, params, nil)
				}
			})
			item.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyName(string(rune('0' + n))), Modifier: fyne.KeyModifierShortcutDefault}
			goTo = append(goTo, item)
		}
		parent := fyne.NewMenuItem(c.Label, nil)
		parent.ChildMenu = fyne.NewMenu("", goTo...)
		items = append(items, parent)
	}
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	return items
}

// history is the action of Back (or Forward): it runs only when the active tab has somewhere to go.
func (b *tabBar) history(back bool) func() {
	return func() {
		t, ok := b.sh.ui.Tab(b.sh.ui.ActiveTab)
		switch {
		case !ok:
		case back && len(t.Back) > 0:
			b.sh.dispatch("ui.tab.back", struct{}{}, nil)
		case !back && len(t.Forward) > 0:
			b.sh.dispatch("ui.tab.forward", struct{}{}, nil)
		}
	}
}

// tabsChanged reports whether the tab bar shows something else in st than in old.
func tabsChanged(st, old engine.UIState) bool {
	return st.ActiveTab != old.ActiveTab || st.Context != old.Context || st.Connection.State != old.Connection.State ||
		st.Connection.Error != old.Connection.Error || st.Welcome != old.Welcome || connectFailureChanged(st.ConnectFailure, old.ConnectFailure) ||
		st.Scope() != old.Scope() || connectionsChanged(st, old) || mixedEnvironments(st) != mixedEnvironments(old) ||
		!slices.EqualFunc(st.Tabs, old.Tabs, func(a, b engine.ViewTab) bool {
			return a.ID == b.ID && a.Title == b.Title && a.Preview == b.Preview && a.Context == b.Context
		})
}

// mixedEnvironments reports whether the preference shows every cluster's tabs (GW11, TabsShown).
func mixedEnvironments(u engine.UIState) bool {
	return u.Preferences != nil && u.Preferences.MixedEnvironments
}

// openFixed opens a navigator item's view in a fixed tab (⇧↵, a double click); on a group or the
// root it does what ↵ does.
func (s *sidebar) openFixed(id string) {
	if id == "" || id == rootNode || s.isBranch(id) {
		s.activate(id)
		return
	}
	key := strings.TrimPrefix(id, aliasPrefix)
	s.marked = id
	s.tree.rebuild()
	s.sh.dispatch("ui.tab.open", tabOpenParams{View: key, Fixed: true}, func(_ any, err error) {
		if err != nil {
			s.sh.showError(err)
			s.selectView(s.sh.ui.View)
		}
	})
}

// navClicked tells a navigator item's double click: the second click of one opens the item's view
// in a fixed tab (its first opened the preview tab). Groups and the root have none.
func (s *sidebar) navClicked(id string) bool {
	if id == rootNode || s.isBranch(id) {
		return false
	}
	return s.sh.tabs.double(&s.sh.tabs.navClick, id)
}

// Command parameters of the tab commands, mirroring the engine's schemas.
type (
	tabIDParams struct {
		ID string `json:"id,omitempty"`
	}
	tabSelectParams struct {
		ID    string `json:"id,omitempty"`
		Index int    `json:"index,omitempty"`
		Last  bool   `json:"last,omitempty"`
	}
	tabOpenParams struct {
		View  string `json:"view"`
		Fixed bool   `json:"fixed,omitempty"`
	}
	tabMoveParams struct {
		ID    string `json:"id,omitempty"`
		Index int    `json:"index"`
	}
)

// viewTab is one tab of the bar: the cluster's badge, the title (italic for the preview tab) and a
// close button; the full title is its tooltip.
type viewTab struct {
	widget.BaseWidget
	bar     *tabBar
	tab     engine.ViewTab
	active  bool
	live    bool            // its cluster is connected, shown or not (D-69)
	id      clusterIdentity // its cluster's initials, icon colour and uploaded image (cluster settings)
	hovered bool
	close   *widget.Button
}

var (
	_ fyne.Tappable     = (*viewTab)(nil)
	_ desktop.Hoverable = (*viewTab)(nil)
	_ fyne.Draggable    = (*viewTab)(nil)
)

func newViewTab(b *tabBar) *viewTab {
	t := &viewTab{bar: b}
	t.close = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		b.sh.tip.hideFor(t)
		b.sh.dispatch("ui.tab.close", tabIDParams{ID: t.tab.ID}, nil)
	})
	t.close.Importance = widget.LowImportance
	t.ExtendBaseWidget(t)
	return t
}

// set changes what the tab shows.
func (t *viewTab) set(tab engine.ViewTab, active, live bool, id clusterIdentity) {
	if tab.ID == t.tab.ID && tab.Title == t.tab.Title && tab.Preview == t.tab.Preview && tab.Context == t.tab.Context &&
		active == t.active && live == t.live && id.initials == t.id.initials && id.ink == t.id.ink && id.image == t.id.image {
		t.tab = tab
		return
	}
	t.tab, t.active, t.live, t.id = tab, active, live, id
	t.Refresh()
}

// Tapped implements fyne.Tappable.
func (t *viewTab) Tapped(*fyne.PointEvent) {
	t.bar.sh.tip.hideFor(t)
	t.bar.tapped(t.tab)
}

// Dragged implements fyne.Draggable: the tab is carried to another place (stripdrag.go).
func (t *viewTab) Dragged(ev *fyne.DragEvent) { t.bar.drag.dragged(t, ev) }

// DragEnd implements fyne.Draggable.
func (t *viewTab) DragEnd() { t.bar.drag.ended(t) }

// MouseIn implements desktop.Hoverable: the full title as the tooltip; nothing while a tab is
// dragged.
func (t *viewTab) MouseIn(ev *desktop.MouseEvent) {
	if t.bar.drag.active {
		return
	}
	t.hovered = true
	t.Refresh()
	t.bar.sh.tip.hover(t, t.tipText(), ev.AbsolutePosition)
}

// tipText is the tab's tooltip: its full title, and the cluster's full context id under it (the
// title names the cluster by its short name, D-68).
func (t *viewTab) tipText() string {
	if t.tab.Context == "" {
		return t.tab.Title
	}
	return t.tab.Title + "\n" + t.tab.Context
}

// MouseMoved implements desktop.Hoverable.
func (t *viewTab) MouseMoved(ev *desktop.MouseEvent) { t.bar.sh.tip.moved(t, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (t *viewTab) MouseOut() {
	t.hovered = false
	t.Refresh()
	t.bar.sh.tip.hideFor(t)
}

// CreateRenderer implements fyne.Widget.
func (t *viewTab) CreateRenderer() fyne.WidgetRenderer {
	r := &viewTabRenderer{t: t, bg: canvas.NewRectangle(color.Transparent), under: canvas.NewRectangle(color.Transparent),
		halo:  canvas.NewRectangle(color.Transparent),
		badge: canvas.NewRectangle(color.Transparent), initials: canvas.NewText("", color.Transparent),
		title: canvas.NewText("", color.Transparent),
		img:   &canvas.Image{FillMode: canvas.ImageFillContain, ScaleMode: canvas.ImageScaleSmooth}}
	r.initials.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type viewTabRenderer struct {
	t                *viewTab
	bg, under, badge *canvas.Rectangle
	halo             *canvas.Rectangle // the active tab's wider, faint underline under the crisp one
	initials, title  *canvas.Text
	img              *canvas.Image // the cluster's uploaded icon in the badge's place (clusteridentity.go)
}

// tabHaloExtra is how far the active tab's faint underline rises above the crisp one.
const tabHaloExtra = 3

// notLiveFade is how much a tab fades its cluster's image while the cluster is not connected, as
// its initials take the muted colour.
const notLiveFade = 0.55

// dims are the inner padding, the badge's side, the widest title and the title's style.
func (r *viewTabRenderer) dims() (pad, side, maxText float32, style fyne.TextStyle) {
	th := r.t.Theme()
	return th.Size(theme.SizeNameInnerPadding), th.Size(theme.SizeNameInlineIcon) + 2, th.Size(sizeDockTabMaxLen),
		fyne.TextStyle{Italic: r.t.tab.Preview}
}

func (r *viewTabRenderer) textWidth() float32 {
	_, _, maxText, style := r.dims()
	w := fyne.MeasureText(r.t.tab.Title, r.t.Theme().Size(theme.SizeNameText), style).Width
	return fyne.Min(w, maxText)
}

func (r *viewTabRenderer) MinSize() fyne.Size {
	pad, side, _, _ := r.dims()
	c := r.t.close.MinSize()
	return fyne.NewSize(pad+side+pad+r.textWidth()+pad/2+c.Width, fyne.Max(c.Height, side+2*pad))
}

func (r *viewTabRenderer) Layout(size fyne.Size) {
	t := r.t
	th := t.Theme()
	pad, side, _, style := r.dims()
	line := th.Size(theme.SizeNameSeparatorThickness) * 2
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.under.Move(fyne.NewPos(0, size.Height-line))
	r.under.Resize(fyne.NewSize(size.Width, line))
	r.under.CornerRadius = line / 2
	r.halo.Move(fyne.NewPos(0, size.Height-line-tabHaloExtra))
	r.halo.Resize(fyne.NewSize(size.Width, line+tabHaloExtra))
	r.halo.CornerRadius = (line + tabHaloExtra) / 2
	r.badge.Move(fyne.NewPos(pad, (size.Height-side)/2))
	r.badge.Resize(fyne.NewSize(side, side))
	r.img.Move(r.badge.Position())
	r.img.Resize(r.badge.Size())
	is := r.initials.MinSize()
	r.initials.Move(fyne.NewPos(pad+(side-is.Width)/2, (size.Height-is.Height)/2))
	r.initials.Resize(is)
	c := t.close.MinSize()
	x := pad + side + pad
	tw := fyne.Max(size.Width-x-pad/2-c.Width, 0)
	r.title.Text = ellipsize(t.tab.Title, tw, th.Size(theme.SizeNameText), style)
	ts := r.title.MinSize()
	r.title.Move(fyne.NewPos(x, (size.Height-ts.Height)/2))
	r.title.Resize(fyne.NewSize(tw, ts.Height))
	t.close.Move(fyne.NewPos(size.Width-c.Width, (size.Height-c.Height)/2))
	t.close.Resize(c)
}

func (r *viewTabRenderer) Refresh() {
	t := r.t
	th := t.Theme()
	accent := themeColor(th, colorAccent)
	ink := accent
	if t.id.ink != "" {
		ink = themeColor(th, t.id.ink) // the cluster's icon colour (cluster settings), the accent by default
	}
	if !t.live {
		ink = themeColor(th, colorMuted)
	}
	r.badge.FillColor = withAlpha(ink, 0x2c) // the navigator badge's and the hotbar tile's tint
	r.badge.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.initials.Text = t.id.initials
	if r.initials.Text == "" {
		r.initials.Text = initials(engine.ContextLabel(t.tab.Context))
	}
	r.initials.Color = ink
	_, side, _, _ := r.dims()
	// Up to five letters of the user's drawn smaller to fit (D-119); two keep their size.
	r.initials.TextSize = fitTextSize(r.initials.Text, th.Size(theme.SizeNameCaptionText), side)
	if t.id.image != nil { // the uploaded icon in place of the initials (F25), faded while not connected
		r.img.Resource = t.id.image
		r.img.Translucency = 0
		if !t.live {
			r.img.Translucency = notLiveFade
		}
		r.img.Show()
		r.badge.Hide()
		r.initials.Hide()
	} else {
		r.img.Resource = nil
		r.img.Hide()
		r.badge.Show()
		r.initials.Show()
	}
	r.bg.Shadow = liftShadow(th, t.bar.drag.carries(t.tab.ID)) // carried (stripdrag.go)
	r.title.TextStyle = fyne.TextStyle{Italic: t.tab.Preview}
	r.title.TextSize = th.Size(theme.SizeNameText)
	switch {
	case t.active || t.bar.drag.carries(t.tab.ID): // a carried tab raised as the active one (stripdrag.go)
		// The active tab is the stage itself, lit from below in the glow blue: a crisp 2 point line
		// and a wider faint one (the logo's glow without blur).
		glow := themeColor(th, theme.ColorNamePrimary)
		r.bg.FillColor = themeColor(th, theme.ColorNameBackground)
		r.title.Color = themeColor(th, colorDockTabText)
		r.under.FillColor = glow
		r.halo.FillColor = withAlpha(glow, 0x30)
	case t.hovered:
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
		r.title.Color = themeColor(th, colorDockTabDim)
		r.under.FillColor, r.halo.FillColor = color.Transparent, color.Transparent
	default: // ghost: no plate, muted text
		r.bg.FillColor = color.Transparent
		r.title.Color = themeColor(th, colorDockTabDim)
		r.under.FillColor, r.halo.FillColor = color.Transparent, color.Transparent
	}
	r.Layout(t.Size())
	for _, o := range []fyne.CanvasObject{r.bg, r.halo, r.under, r.badge, r.initials, r.img, r.title} {
		o.Refresh()
	}
}

func (r *viewTabRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.halo, r.under, r.badge, r.initials, r.img, r.title, r.t.close}
}

func (r *viewTabRenderer) Destroy() {}
