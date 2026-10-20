package fyneui

import (
	"fmt"
	"image/color"
	"reflect"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The hotbar (G13; engine/hotbar.go): a narrow bar at the window's left edge, left of the
// navigator, with one tile per pinned cluster, top to bottom, as captured from Lens (2026-10-11). A
// tile is the cluster's identity (clusteridentity.go: its initials in its icon colour, the accent by
// default, or its uploaded image), its name under it (the display name its Settings give it, else
// the context's) and a dot with the cluster's mark (clustermarks.go, D-69: live, (re)connecting,
// cold, its login lapsed, failed); its tooltip is the full context id, the state and its F-key.
// The shown cluster's tile is raised; a connected cluster's tile that is not shown has its dot
// only. A click opens the cluster (hotbar.openCluster: a connected one is only shown, D-69); the
// tile's menu (right-click) connects, moves it up or down and removes it; a tile dragged within the
// bar is carried under the pointer while the others make room, and lands where it is dropped
// (hotbardrag.go, D-83). F1–F12 open the first twelve tiles.
//
// One hotbar (the owner's decision of 2026-10-17, D-83): Lens's banks and their switcher ("‹ 1 ›"
// at the bar's foot, ⇧F1–⇧F12) are gone; the bar holds as many tiles as the user pins and scrolls
// when they do not fit, the "+" below the last tile. Inferred, Cogpit's own: the "+" that pins the
// connected cluster and the menus' items. Move Up and Move Down stay beside the drag, for whoever
// does not drag.
//
// The bar shows what hotbar.list returns, read again whenever UIState.Hotbar, the connection or
// the contexts change; every change goes through the hotbar.* commands (UI state is engine state).
// A new list updates the tiles in place, the same widget per context, so that a tooltip or a drag
// survives it. Without a hotbar in the engine (UIState.Hotbar nil) the bar is hidden.
type hotbar struct {
	sh *shell

	view *engine.HotbarState // the last hotbar.list; nil while there is none
	seq  int                 // the latest hotbar.list request; older answers are dropped

	tiles  *fyne.Container   // the tiles and the "+", laid out by tileStack (hotbardrag.go)
	scroll *container.Scroll // the tiles' area: it scrolls when they do not fit
	drag   *tileDrag         // a tile being dragged to another place (hotbardrag.go)
	add    *hotbarButton
	root   *fyne.Container

	menuModel *fyne.Menu // the open tile menu, for tests
	menuFor   string     // what the Cluster menu's hotbar item last offered, to rebuild it when that changes
}

func newHotbar(sh *shell) *hotbar {
	h := &hotbar{sh: sh}
	h.add = newHotbarButton(h, "+", theme.SizeNameHeadingText, h.addConnected)
	h.add.square = true
	h.drag = newTileDrag(h)
	h.tiles = box(&tileStack{h: h})
	h.scroll = container.NewVScroll(inset("", theme.SizeNamePadding, h.tiles))
	h.root = sized(sizeHotbarWidth, "", newSurface(theme.ColorNameHeaderBackground),
		container.NewBorder(nil, nil, nil, newDivider(true), h.scroll))
	h.root.Hide()
	return h
}

// stateChanged follows the UI state: hidden without a hotbar, else read again.
func (h *hotbar) stateChanged() {
	if h.sh.ui.Hotbar == nil {
		h.seq++
		h.setView(nil)
		return
	}
	h.request()
}

// marksChanged draws the tiles' dots again without rebuilding the tiles (a rebuild would drop a
// tooltip or a drag in progress): their marks read state the hotbar's list does not carry, such as
// the contexts' failed connects. Main goroutine.
func (h *hotbar) marksChanged() {
	for _, t := range h.tileWidgets() {
		t.Refresh()
	}
}

// request reads the hotbar as the user sees it (hotbar.list) and shows it. Main goroutine.
func (h *hotbar) request() {
	if h.sh.ui.Hotbar == nil {
		return
	}
	h.seq++
	seq := h.seq
	h.sh.dispatch("hotbar.list", struct{}{}, func(res any, err error) {
		if seq != h.seq || h.sh.ui.Hotbar == nil {
			return
		}
		var out struct {
			Hotbar engine.HotbarState `json:"hotbar"`
		}
		if err != nil || decode(res, &out) != nil {
			return // the bar keeps what it shows; the next change reads it again
		}
		h.setView(&out.Hotbar)
	})
}

// setView shows a hotbar, or hides the bar for nil. The tiles are updated in place: a context's
// widget stays the same while it is on the bar (its tooltip and a drag of it survive), a new one
// gets a widget, a removed one's goes.
func (h *hotbar) setView(v *engine.HotbarState) {
	h.view = v
	if v == nil {
		h.drag.cancel()
		h.root.Hide()
		h.syncMenu()
		return
	}
	had := map[string]*hotbarTile{}
	for _, t := range h.tileWidgets() {
		had[t.tile.Context] = t
	}
	objs := make([]fyne.CanvasObject, 0, len(v.Clusters)+1)
	for i, t := range v.Clusters {
		w := had[t.Context]
		if w == nil {
			w = newHotbarTile(h, t, i+1)
		} else {
			delete(had, t.Context)
			w.tile, w.slot = t, i+1
		}
		objs = append(objs, w)
	}
	if owner, ok := h.sh.tip.owner.(*hotbarTile); ok && had[owner.tile.Context] == owner {
		h.sh.tip.hide() // its tile left the bar, and would never say the pointer left
	}
	h.tiles.Objects = append(objs, h.add)
	h.drag.follow()
	h.tiles.Refresh()
	h.updateAdd()
	h.root.Show()
	h.root.Refresh()
	h.syncMenu()
}

// updateAdd says what the "+" would pin, or why it cannot.
func (h *hotbar) updateAdd() {
	ui := h.sh.ui
	switch {
	case ui.Context == "":
		h.add.tip, h.add.disabled = "Connect a cluster to pin it to the hotbar", true
	case h.view.Position(ui.Context) > 0:
		h.add.tip, h.add.disabled = engine.ContextLabel(ui.Context)+" is on the hotbar already", true
	default:
		h.add.tip, h.add.disabled = "Add "+engine.ContextLabel(ui.Context)+" to the hotbar", false
	}
	h.add.Refresh()
}

// addConnected pins the connected cluster.
func (h *hotbar) addConnected() {
	if h.view == nil || h.add.disabled {
		return
	}
	h.sh.dispatch("hotbar.addCluster", hotbarClusterParams{}, nil)
}

// open opens the tile in a slot (from 1): a missing context is explained, a credential plugin
// waiting for the user's approval is shown first (as the navigator's section does), the shown
// cluster's tile does nothing, and a connected cluster's tile only shows it (D-69: nothing is asked
// again, nothing connects).
func (h *hotbar) open(slot int) {
	if h.view == nil || slot < 1 || slot > len(h.view.Clusters) {
		return
	}
	t := h.view.Clusters[slot-1]
	if t.Missing {
		h.sh.toast("warning", t.Context+" is no longer offered by any kubeconfig source. Remove its tile, or sync its kubeconfig again.")
		return
	}
	if t.Context == h.sh.ui.Context && (t.Connection.Live() || t.Connection == engine.Connecting) {
		return
	}
	name := h.sh.identity(t.Context).name // the one short name (D-68)
	run := func() {
		h.sh.dispatch("hotbar.openCluster", hotbarSlotParams{Slot: slot}, func(res any, err error) {
			if err != nil {
				h.sh.showError(err)
				return
			}
			var out struct {
				Context string `json:"context"`
				State   string `json:"state"`
				Error   string `json:"error"`
			}
			if decode(res, &out) == nil && out.State == string(engine.ConnectError) {
				h.sh.toast("error", "Could not connect to "+name+": "+out.Error)
			}
		})
	}
	if (t.Exec == engine.ExecRequired || t.Exec == engine.ExecChanged) && !switchable(h.sh.ui, t.Context) {
		h.sh.kc.reviewPlugin(t.Context, func(allowed bool) {
			if !allowed {
				h.sh.toast("warning", "Not connected: the login of "+name+" was not allowed.")
				return
			}
			run()
		})
		return
	}
	run()
}

// key handles F1–F12: open the first twelve tiles. It reports whether the key was the hotbar's; a
// key without a tile does nothing. A shifted F-key is not the hotbar's (⇧F1–⇧F12 showed a bank
// until D-83) and goes on as any other key.
func (h *hotbar) key(name fyne.KeyName, shift bool) bool {
	n := functionKey(name)
	if n == 0 || shift {
		return false
	}
	h.open(n)
	return true
}

// functionKey is n for the key Fn (1 to 12), else 0.
func functionKey(name fyne.KeyName) int {
	for i, k := range []fyne.KeyName{fyne.KeyF1, fyne.KeyF2, fyne.KeyF3, fyne.KeyF4, fyne.KeyF5, fyne.KeyF6,
		fyne.KeyF7, fyne.KeyF8, fyne.KeyF9, fyne.KeyF10, fyne.KeyF11, fyne.KeyF12} {
		if name == k {
			return i + 1
		}
	}
	return 0
}

// shiftHeld reports whether Shift is down. Fyne delivers F-keys without their modifiers: unmodified
// and shifted keys are no shortcuts to it, so they reach TypedKey; the desktop driver knows the
// modifiers held.
func shiftHeld() bool {
	if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0
	}
	return false
}

// showMenu pops a menu up at pos (window coordinates); one that would reach into the status bar
// opens upwards from pos instead, as a tile's menu near the bar's foot does.
func (h *hotbar) showMenu(m *fyne.Menu, pos fyne.Position) {
	h.sh.tip.hide()
	h.menuModel = m
	pm := widget.NewPopUpMenu(m, h.sh.win.Canvas())
	room := h.sh.win.Canvas().Size().Height - h.sh.app.Settings().Theme().Size(sizeStatusHeight)
	if ms := pm.MinSize(); pos.Y+ms.Height > room && pos.Y > ms.Height {
		pos.Y -= ms.Height
	}
	pm.ShowAtPosition(pos)
}

// tileMenu is a tile's menu: Connect, Settings, Move Up, Move Down, Remove from Hotbar (Cogpit's,
// inferred).
func (h *hotbar) tileMenu(slot int, pos fyne.Position) {
	if h.view == nil || slot < 1 || slot > len(h.view.Clusters) {
		return
	}
	t, n := h.view.Clusters[slot-1], len(h.view.Clusters)
	connect := fyne.NewMenuItem(connectItemLabel(h.sh.ui, t.Context), func() { h.open(slot) })
	connect.Disabled = t.Missing || (t.Context == h.sh.ui.Context && t.Connection.Live())
	move := func(to int) func() {
		return func() {
			h.sh.dispatch("hotbar.moveCluster", hotbarMoveParams{Context: t.Context, Position: to}, nil)
		}
	}
	up := fyne.NewMenuItem("Move Up", move(slot-1))
	up.Disabled = slot == 1
	down := fyne.NewMenuItem("Move Down", move(slot+1))
	down.Disabled = slot == n
	remove := fyne.NewMenuItem("Remove from Hotbar", func() {
		h.sh.dispatch("hotbar.removeCluster", hotbarClusterParams{Context: t.Context}, nil)
	})
	items := []*fyne.MenuItem{connect}
	if settings := h.sh.settingsItem(t.Context); settings != nil { // the cluster's Settings page
		items = append(items, settings)
	}
	items = append(items, h.sh.clusterItems(t.Context)...) // Disconnect, Remove from Cogpit (menus_cluster.go)
	items = append(items, fyne.NewMenuItemSeparator(), up, down, fyne.NewMenuItemSeparator(), remove)
	h.showMenu(fyne.NewMenu("", items...), pos)
}

// contextItem is the hotbar's item in a context's menu (the navigator's and the welcome page's rows,
// the Cluster menu): "Add to Hotbar", or "Remove from Hotbar" when the hotbar holds it; nil without
// a hotbar.
func (h *hotbar) contextItem(id string) *fyne.MenuItem {
	if h.view == nil || id == "" {
		return nil
	}
	if h.view.Position(id) > 0 {
		return fyne.NewMenuItem("Remove from Hotbar", func() {
			h.sh.dispatch("hotbar.removeCluster", hotbarClusterParams{Context: id}, nil)
		})
	}
	return fyne.NewMenuItem("Add to Hotbar", func() {
		h.sh.dispatch("hotbar.addCluster", hotbarClusterParams{Context: id}, nil)
	})
}

// syncMenu rebuilds the window's menu when the Cluster menu's hotbar item would change.
func (h *hotbar) syncMenu() {
	state := ""
	if item := h.contextItem(h.sh.ui.Context); item != nil {
		state = item.Label + fmt.Sprint(item.Disabled)
	}
	if state != h.menuFor {
		h.menuFor = state
		h.sh.win.SetMainMenu(h.sh.mainMenu())
	}
}

// sameHotbar reports whether two UI states show the same hotbar.
func sameHotbar(a, b *engine.HotbarState) bool { return reflect.DeepEqual(a, b) }

// Command parameters, mirroring the engine's (commands/hotbar.go).
type (
	hotbarClusterParams struct {
		Context string `json:"context,omitempty"`
	}
	hotbarMoveParams struct {
		Context  string `json:"context"`
		Position int    `json:"position"`
	}
	hotbarSlotParams struct {
		Slot int `json:"slot"`
	}
)

// hotbarTile is one cluster on the hotbar, or (ghost) the copy of one carried under the pointer
// while it is dragged (hotbardrag.go): a ghost takes the pointer's hover, so nothing under it
// lights up, and nothing else.
type hotbarTile struct {
	widget.BaseWidget
	h       *hotbar
	tile    engine.HotbarTile
	slot    int
	hovered bool

	ghost bool    // the carried copy: drawn lifted on a card, no tap, no menu, no tooltip
	scale float32 // the ghost's size against a tile's; 0 is 1
	dy    float32 // drawn this far below its place while it slides there (tileStack)
}

var (
	_ fyne.Tappable          = (*hotbarTile)(nil)
	_ fyne.SecondaryTappable = (*hotbarTile)(nil)
	_ desktop.Hoverable      = (*hotbarTile)(nil)
)

func newHotbarTile(h *hotbar, t engine.HotbarTile, slot int) *hotbarTile {
	ht := &hotbarTile{h: h, tile: t, slot: slot}
	ht.ExtendBaseWidget(ht)
	return ht
}

// active reports whether the tile is the cluster the window shows, connected or connecting: the
// tile is raised. Another connected cluster's tile is not (D-69): its dot says it is connected.
func (t *hotbarTile) active() bool {
	live := t.tile.Connection.Live() || t.tile.Connection == engine.Connecting
	if sh := t.h.sh; sh != nil {
		return live && t.tile.Context == sh.ui.Context
	}
	return live
}

// mark is the tile's dot (clustermarks.go): the cluster's mark, a failed last connect as the
// navigator's list of contexts says it.
func (t *hotbarTile) mark() clusterMark {
	if t.h.sh == nil {
		return clusterMark{conn: engine.Connection{State: t.tile.Connection}}
	}
	return t.h.sh.mark(t.tile.Context, t.h.sh.connectProblem(t.tile.Context))
}

// tipText is the tooltip: the full context id, its state and its key (F1–F12, the first twelve
// tiles), and the client certificate's dates for a context that has one (D-75).
func (t *hotbarTile) tipText() string {
	m := t.mark()
	state := m.state
	if state == "" {
		state = connectionText(m.conn)
	}
	if t.tile.Missing {
		state = "No kubeconfig source offers this context any more"
	}
	tip := t.tile.Context + "\n" + state
	if t.slot >= 1 && t.slot <= engine.HotbarKeys {
		tip += " · F" + strconv.Itoa(t.slot)
	}
	if sh := t.h.sh; sh != nil && !t.tile.Missing {
		if line := sh.certTip(sh.clientCert(t.tile.Context)); line != "" {
			tip += "\n" + line
		}
	}
	return tip
}

// Tapped implements fyne.Tappable: open the cluster.
func (t *hotbarTile) Tapped(*fyne.PointEvent) {
	if t.ghost {
		return
	}
	t.h.sh.tip.hideFor(t)
	t.h.open(t.slot)
}

// TappedSecondary implements fyne.SecondaryTappable: the tile's menu.
func (t *hotbarTile) TappedSecondary(ev *fyne.PointEvent) {
	if !t.ghost {
		t.h.tileMenu(t.slot, ev.AbsolutePosition)
	}
}

// MouseIn implements desktop.Hoverable. While a tile is dragged the others neither light up nor
// show their tooltip: they make room for it.
func (t *hotbarTile) MouseIn(ev *desktop.MouseEvent) {
	if t.ghost || t.h.drag.active {
		return
	}
	t.hovered = true
	t.Refresh()
	t.h.sh.tip.hover(t, t.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (t *hotbarTile) MouseMoved(ev *desktop.MouseEvent) {
	if !t.ghost {
		t.h.sh.tip.moved(t, ev.AbsolutePosition)
	}
}

// MouseOut implements desktop.Hoverable.
func (t *hotbarTile) MouseOut() {
	if t.ghost {
		return
	}
	t.hovered = false
	t.Refresh()
	t.h.sh.tip.hideFor(t)
}

// zoom is the tile's scale: a ghost's lift, else 1.
func (t *hotbarTile) zoom() float32 {
	if t.scale <= 0 {
		return 1
	}
	return t.scale
}

// CreateRenderer implements fyne.Widget.
func (t *hotbarTile) CreateRenderer() fyne.WidgetRenderer {
	r := &hotbarTileRenderer{t: t,
		card: canvas.NewRectangle(color.Transparent), plate: newPlate(),
		bg: canvas.NewRectangle(color.Transparent), initials: canvas.NewText("", color.Transparent),
		ring: canvas.NewCircle(color.Transparent), dot: newConnDot(t.h.sh), still: canvas.NewCircle(color.Transparent),
		label: canvas.NewText("", color.Transparent),
		img:   &canvas.Image{FillMode: canvas.ImageFillContain, ScaleMode: canvas.ImageScaleSmooth}}
	r.initials.TextStyle = fyne.TextStyle{Bold: true}
	r.card.Hide()
	r.still.Hide()
	for i := range r.glow {
		r.glow[i] = canvas.NewRectangle(color.Transparent)
	}
	r.Refresh()
	return r
}

type hotbarTileRenderer struct {
	t        *hotbarTile
	card     *canvas.Rectangle // a ghost's lifted card under the tile, with its shadow
	bg       *canvas.Rectangle
	plate    *plate               // the tile's face: the identity colour as a gradient (chrome_plate.go)
	glow     [3]*canvas.Rectangle // the shown cluster's glow in the glow blue, as glowFrame's rings
	initials *canvas.Text
	ring     *canvas.Circle
	dot      *connDot       // a click on it opens the cluster's connection log, not the cluster (F13)
	still    *canvas.Circle // a ghost's dot: drawn, no target
	label    *canvas.Text
	img      *canvas.Image   // the cluster's uploaded icon (clusteridentity.go), over the square
	id       clusterIdentity // the cluster's name and icon, from its settings
}

func (r *hotbarTileRenderer) sizes() (side, pad, dot float32) {
	th, z := r.t.Theme(), r.t.zoom()
	return th.Size(sizeHotbarTile) * z, th.Size(theme.SizeNamePadding) * z, th.Size(sizeDotDiameter) * z
}

func (r *hotbarTileRenderer) MinSize() fyne.Size {
	side, pad, _ := r.sizes()
	lh := fyne.MeasureText("Mg", r.label.TextSize, r.label.TextStyle).Height
	return fyne.NewSize(side+2*pad, pad+side+pad/2+lh+pad)
}

func (r *hotbarTileRenderer) Layout(size fyne.Size) {
	side, pad, d := r.sizes()
	r.card.Move(fyne.NewPos(0, 0))
	r.card.Resize(size)
	x := (size.Width - side) / 2
	r.bg.Move(fyne.NewPos(x, pad))
	r.bg.Resize(fyne.NewSize(side, side))
	r.plate.img.Move(r.bg.Position())
	r.plate.img.Resize(r.bg.Size())
	for i, g := range r.glow {
		o := float32(i) * 2 * r.t.zoom()
		g.Move(fyne.NewPos(x-o, pad-o))
		g.Resize(fyne.NewSize(side+2*o, side+2*o))
		g.CornerRadius = r.bg.CornerRadius + o
	}
	r.img.Move(fyne.NewPos(x+2, pad+2))
	r.img.Resize(fyne.NewSize(side-4, side-4))
	is := r.initials.MinSize()
	r.initials.Move(fyne.NewPos(x+(side-is.Width)/2, pad+(side-is.Height)/2))
	r.initials.Resize(is)
	ring := d + 4*r.t.zoom()
	r.ring.Move(fyne.NewPos(x+side-ring+4, pad+side-ring+4))
	r.ring.Resize(fyne.NewSize(ring, ring))
	r.dot.Move(fyne.NewPos(x+side-d+2, pad+side-d+2)) // a badge on the corner, cut out of the tile by its ring
	r.dot.Resize(fyne.NewSize(d, d))
	r.still.Move(r.dot.Position())
	r.still.Resize(r.dot.Size())
	r.label.Text = ellipsize(r.id.name, size.Width-pad, r.label.TextSize, r.label.TextStyle)
	ls := r.label.MinSize()
	r.label.Move(fyne.NewPos((size.Width-ls.Width)/2, pad+side+pad/2))
	r.label.Resize(ls)
}

func (r *hotbarTileRenderer) Refresh() {
	t := r.t
	th := t.Theme()
	label := t.tile.DisplayName // the engine's name of the tile (D-68); a test's tile may carry its label only
	if label == "" {
		label = t.tile.Label
	}
	r.id = clusterIdentity{name: label, initials: nameInitials(label), ink: colorAccent}
	if sh := t.h.sh; sh != nil {
		r.id = sh.identity(t.tile.Context)
	}
	r.initials.Text = r.id.initials
	accent := themeColor(th, r.id.ink) // the cluster's icon colour, the accent by default
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius) * t.zoom()
	r.bg.StrokeWidth = 0
	r.initials.TextSize = th.Size(theme.SizeNameSubHeadingText) * t.zoom()
	r.label.TextSize = th.Size(theme.SizeNameCaptionText) * t.zoom()
	r.label.Color = themeColor(th, colorMuted)
	r.initials.Color = accent
	plated := !t.tile.Missing && !t.carried() && !t.ghost // the face is the plate; the others keep the flat square
	glowing := plated && t.active()
	switch {
	case t.tile.Missing: // an empty outline in the warning colour: the context is gone
		r.bg.FillColor = color.Transparent
		r.bg.StrokeColor, r.bg.StrokeWidth = themeColor(th, colorStatusWarning), th.Size(theme.SizeNameInputBorder)
		r.initials.Color = themeColor(th, colorMuted)
	case t.active():
		r.bg.FillColor, r.bg.StrokeWidth = color.Transparent, 0
		r.label.Color = themeColor(th, theme.ColorNameForeground)
	default:
		r.bg.FillColor, r.bg.StrokeWidth = withAlpha(accent, 0x2c), 0 // ghost and carried place: show() repaints
	}
	top, base := uint8(0x38), uint8(0x1a) // the plate: about 22 % down to 10 %
	switch {
	case t.hovered && t.tile.Missing:
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	case t.hovered:
		top, base = 0x4a, 0x2c
		if !plated {
			r.bg.FillColor = withAlpha(accent, 0x66)
		}
	}
	r.plate.set(accent, top, base, r.bg.CornerRadius)
	setVisible(r.plate.img, plated)
	if plated {
		r.bg.FillColor = color.Transparent
	}
	glow := themeColor(th, theme.ColorNamePrimary)
	for i, g := range r.glow {
		g.FillColor, g.StrokeColor, g.StrokeWidth = color.Transparent, withAlpha(glow, glowAlphas[i]), 2
		setVisible(g, glowing)
	}
	m := t.mark() // the cluster's mark, with its reason and times for the dot's tooltip (D-69)
	if t.tile.Missing {
		m.color, m.ring = colorStatusWarning, false
	}
	r.dot.setMark(t.tile.Context, m)
	r.ring.FillColor = themeColor(th, theme.ColorNameHeaderBackground) // sets the dot off the tile
	r.img.Translucency = 0
	if r.id.image != nil && !t.tile.Missing { // the uploaded icon in place of the initials
		r.img.Resource = r.id.image
	} else {
		r.img.Resource = nil
	}
	r.show(th, accent, m)
	r.Layout(t.Size())
	for _, o := range r.Objects() {
		o.Refresh()
	}
}

// show picks what the tile draws: the tile; the empty place of a tile being carried elsewhere
// (hotbardrag.go), an outline only; or the carried ghost, the tile on a lifted card with a shadow
// and its dot drawn still.
func (r *hotbarTileRenderer) show(th fyne.Theme, accent color.Color, m clusterMark) {
	t := r.t
	place := t.carried()
	image := r.img.Resource != nil
	setVisible(r.img, image && !place)
	setVisible(r.initials, !image && !place)
	setVisible(r.label, !place)
	setVisible(r.ring, !place)
	setVisible(r.dot, !place && !t.ghost)
	setVisible(r.still, t.ghost)
	setVisible(r.card, t.ghost)
	r.card.Shadow = canvas.Shadow{}
	switch {
	case place:
		r.bg.FillColor = color.Transparent
		r.bg.StrokeColor, r.bg.StrokeWidth = withAlpha(accent, 0x70), th.Size(theme.SizeNameInputBorder)
	case t.ghost:
		v := fyne.CurrentApp().Settings().ThemeVariant()
		r.card.FillColor = themeColor(th, theme.ColorNameHeaderBackground)
		r.card.CornerRadius = th.Size(theme.SizeNameInputRadius) * t.zoom()
		r.card.StrokeColor, r.card.StrokeWidth = themeColor(th, theme.ColorNameInputBorder), 1
		r.card.Shadow = canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 12,
			Offset: fyne.NewPos(0, 4), Variant: canvas.DropShadow}
		if !t.tile.Missing {
			r.bg.FillColor = withAlpha(accent, 0x66) // picked up: the hover's tint
		}
		name := m.color
		if name == "" {
			name = connectionColor(m.conn)
		}
		c := themeColor(th, name)
		if m.ring {
			r.still.FillColor, r.still.StrokeColor, r.still.StrokeWidth = color.Transparent, c, ringStroke
		} else {
			r.still.FillColor, r.still.StrokeColor, r.still.StrokeWidth = c, color.Transparent, 0
		}
	}
}

func (r *hotbarTileRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.card, r.glow[2], r.glow[1], r.plate.img, r.bg, r.glow[0], r.img, r.initials, r.ring, r.dot, r.still, r.label}
}

func (r *hotbarTileRenderer) Destroy() {}

// hotbarButton is the hotbar's "+" with a tooltip, drawn as an empty tile.
type hotbarButton struct {
	widget.BaseWidget
	h        *hotbar
	text     string
	size     fyne.ThemeSizeName
	square   bool // drawn as an empty tile with an outline
	tip      string
	disabled bool
	hovered  bool
	onTap    func()
}

var (
	_ fyne.Tappable     = (*hotbarButton)(nil)
	_ desktop.Hoverable = (*hotbarButton)(nil)
)

func newHotbarButton(h *hotbar, text string, size fyne.ThemeSizeName, tap func()) *hotbarButton {
	b := &hotbarButton{h: h, text: text, size: size, onTap: tap}
	b.ExtendBaseWidget(b)
	return b
}

// Tapped implements fyne.Tappable.
func (b *hotbarButton) Tapped(*fyne.PointEvent) {
	b.h.sh.tip.hideFor(b)
	if !b.disabled && b.onTap != nil {
		b.onTap()
	}
}

// MouseIn implements desktop.Hoverable.
func (b *hotbarButton) MouseIn(ev *desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
	b.h.sh.tip.hover(b, b.tip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *hotbarButton) MouseMoved(ev *desktop.MouseEvent) { b.h.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *hotbarButton) MouseOut() {
	b.hovered = false
	b.Refresh()
	b.h.sh.tip.hideFor(b)
}

// CreateRenderer implements fyne.Widget.
func (b *hotbarButton) CreateRenderer() fyne.WidgetRenderer {
	r := &hotbarButtonRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), txt: canvas.NewText(b.text, color.Transparent)}
	r.Refresh()
	return r
}

type hotbarButtonRenderer struct {
	b   *hotbarButton
	bg  *canvas.Rectangle
	txt *canvas.Text
}

func (r *hotbarButtonRenderer) MinSize() fyne.Size {
	th := r.b.Theme()
	pad := th.Size(theme.SizeNamePadding)
	if r.b.square {
		side := th.Size(sizeHotbarTile)
		return fyne.NewSize(side+2*pad, side+2*pad)
	}
	ts := r.txt.MinSize()
	return fyne.NewSize(ts.Width+2*pad, ts.Height+pad)
}

func (r *hotbarButtonRenderer) Layout(size fyne.Size) {
	th := r.b.Theme()
	if r.b.square {
		side, pad := th.Size(sizeHotbarTile), th.Size(theme.SizeNamePadding)
		r.bg.Move(fyne.NewPos((size.Width-side)/2, pad))
		r.bg.Resize(fyne.NewSize(side, side))
		ts := r.txt.MinSize()
		r.txt.Move(fyne.NewPos((size.Width-ts.Width)/2, pad+(side-ts.Height)/2))
		r.txt.Resize(ts)
		return
	}
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *hotbarButtonRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	r.txt.Text = b.text
	r.txt.TextSize = th.Size(b.size)
	fg := theme.ColorNameForeground
	if b.disabled || b.square {
		fg = colorMuted
	}
	r.txt.Color = themeColor(th, fg)
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	r.bg.StrokeWidth = 0
	if b.square {
		r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
		r.bg.StrokeColor, r.bg.StrokeWidth = themeColor(th, theme.ColorNameInputBorder), th.Size(theme.SizeNameInputBorder)
	}
	if b.hovered && !b.disabled {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	}
	r.Layout(b.Size())
	r.txt.Refresh()
	r.bg.Refresh()
}

func (r *hotbarButtonRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.txt} }
func (r *hotbarButtonRenderer) Destroy()                     {}
