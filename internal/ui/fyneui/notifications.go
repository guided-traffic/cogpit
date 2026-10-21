package fyneui

import (
	"image/color"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The title bar's notifications (ui-parity › Appearance, "Notifications in the title bar"; a bell
// in Lens's title bar, captured 2026-10-11; what it lists and how is not captured, so the panel is
// Cogpit's, inferred): a bell at the top bar's right end with a badge of the unread count, in the
// colour of the most severe unread level, and a panel below it listing the session's toasts newest
// first. The list is engine state (engine/notifications.go): the window reads it with
// notifications.list, opening the panel marks what it shows read (notifications.markRead), "Clear
// all" empties it (notifications.clear), and closing a toast by its × marks that one read, so the
// badge counts what went by unseen. Whether the panel is open is the window's, as the context
// picker's was: agents read the list itself (Cogpit's default, 2026-10-13).

// iconBell is the bell, drawn for Cogpit: a dome on a rim, and the clapper below it.
var iconBell = theme.NewThemedResource(fyne.NewStaticResource("cogpit-bell.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M12 2a1.6 1.6 0 0 0-1.6 1.6v.5a6.6 6.6 0 0 0-5.2 6.5v4.6l-1.7 2v1.4h17v-1.4l-1.7-2v-4.6a6.6 6.6 0 0 0-5.2-6.5v-.5A1.6 1.6 0 0 0 12 2z"/>`+
		`<path d="M9.5 19.8h5a2.5 2.5 0 0 1-5 0z"/></svg>`)))

// windowToasts is the session's side of the window's own toasts: the real session keeps them in
// its notification log; a test fake need not.
type windowToasts interface {
	RecordWindowToast(level, message string) uint64
}

var _ windowToasts = (*engine.Session)(nil)

// recordToast keeps a toast the window raised itself in the engine's notification log and returns
// its id, 0 when the engine keeps none.
func (sh *shell) recordToast(level, msg string) uint64 {
	if n, ok := sh.eng.(windowToasts); ok {
		return n.RecordWindowToast(level, msg)
	}
	return 0
}

// markToastRead marks the notification of a toast the user closed read.
func (sh *shell) markToastRead(id uint64) {
	if id != 0 {
		sh.dispatch("notifications.markRead", notificationIDs{IDs: []uint64{id}}, func(any, error) {})
	}
}

type notificationIDs struct {
	IDs []uint64 `json:"ids"`
}

// notificationsUI is the bell and its panel.
type notificationsUI struct {
	sh    *shell
	bell  *bellButton
	panel *notificationPanel // the open panel, nil while closed

	list   []engine.Notification      // what the panel shows, newest first
	loaded bool                       // the open panel read the list once
	fresh  map[uint64]bool            // ids that were unread when the open panel first showed them
	seen   engine.NotificationSummary // the summary the list was read at
	seq    int                        // the latest notifications.list request; older answers are dropped
}

func newNotificationsUI(sh *shell) *notificationsUI {
	n := &notificationsUI{sh: sh}
	n.bell = newBellButton(n)
	if _, ok := sh.eng.Registry().Get("notifications.list"); !ok {
		n.bell.Hide() // an engine without the log (a test fake)
	}
	return n
}

// stateChanged follows the UI state: the badge, and the open panel's list when the log has new
// notifications or was cleared.
func (n *notificationsUI) stateChanged() {
	n.bell.Refresh()
	if n.panel == nil {
		return
	}
	cur := n.sh.ui.Notifications
	if cur.Latest != n.seen.Latest || cur.Total != n.seen.Total {
		n.request()
	}
}

// visible reports whether the panel is open.
func (n *notificationsUI) visible() bool { return n.panel != nil }

// toggle opens the panel, or closes it.
func (n *notificationsUI) toggle() {
	if n.panel != nil {
		n.close()
		return
	}
	n.open()
}

// open shows the panel below the bell and reads the list.
func (n *notificationsUI) open() {
	sh := n.sh
	sh.tip.hide()
	n.fresh, n.list, n.loaded = map[uint64]bool{}, nil, false
	n.panel = newNotificationPanel(n)
	sh.win.Canvas().Overlays().Add(n.panel)
	n.panel.Resize(sh.win.Canvas().Size())
	n.rebuild()
	n.request()
}

// close hides the panel.
func (n *notificationsUI) close() {
	if n.panel == nil {
		return
	}
	n.sh.win.Canvas().Overlays().Remove(n.panel)
	n.panel = nil
	n.fresh = nil
}

// request reads the list; the answer marks what it shows read.
func (n *notificationsUI) request() {
	n.seq++
	seq := n.seq
	n.seen = n.sh.ui.Notifications
	n.sh.dispatch("notifications.list", struct{}{}, func(res any, err error) {
		if seq != n.seq || n.panel == nil {
			return
		}
		var l engine.NotificationList
		if err != nil || decode(res, &l) != nil {
			return // the panel keeps what it shows; the next change reads it again
		}
		n.list, n.loaded = l.Notifications, true
		var unread []uint64
		for _, it := range l.Notifications {
			if !it.Read {
				n.fresh[it.ID] = true
				unread = append(unread, it.ID)
			}
		}
		n.rebuild()
		if len(unread) > 0 {
			n.sh.dispatch("notifications.markRead", notificationIDs{IDs: unread}, func(any, error) {})
		}
	})
}

// clearAll empties the log.
func (n *notificationsUI) clearAll() {
	n.sh.dispatch("notifications.clear", struct{}{}, nil)
}

// rebuild shows the list in the open panel.
func (n *notificationsUI) rebuild() {
	if n.panel != nil {
		n.panel.setContent(n.content())
	}
}

// content is the panel's card: a header with "Clear all", and a row per notification.
func (n *notificationsUI) content() fyne.CanvasObject {
	sh := n.sh
	th := sh.app.Settings().Theme()
	width := th.Size(sizeToastWidth)
	title := newTextLine("Notifications", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	clearAll := widget.NewButton("Clear all", n.clearAll)
	clearAll.Importance = widget.LowImportance
	if len(n.list) == 0 {
		clearAll.Disable()
	}
	head := inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewBorder(nil, nil, nil, clearAll, title))

	var body fyne.CanvasObject
	if len(n.list) == 0 {
		say := "No notifications"
		if !n.loaded {
			say = "Loading…"
		}
		empty := newTextLine(say, colorMuted, theme.SizeNameText, fyne.TextStyle{})
		empty.align = fyne.TextAlignCenter
		body = inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, empty)
	} else {
		rows := container.NewVBox()
		for i, it := range n.list {
			if i > 0 {
				rows.Add(newDivider(false))
			}
			rows.Add(n.row(it, width))
		}
		body = rows
		if most := th.Size(sizePickerHeight); rows.MinSize().Height > most { // a long list scrolls
			scroll := container.NewVScroll(rows)
			scroll.SetMinSize(fyne.NewSize(width, most))
			body = scroll
		}
	}
	return container.NewBorder(container.NewVBox(head, newDivider(false)), nil, nil, nil, body)
}

// row is one notification: the level's icon in its colour, the level's title as on its toast, the
// time and the text; a row that was unread when the panel showed it first is tinted. Its text is
// selectable, and a copy icon after the time, shown while the pointer is on the row, copies the
// whole message (D-99, toastcopy.go).
func (n *notificationsUI) row(it engine.Notification, width float32) fyne.CanvasObject {
	sh := n.sh
	th := sh.app.Settings().Theme()
	c, title, icon := toastStyle(it.Level)
	pad := th.Size(theme.SizeNamePadding)
	inner := th.Size(theme.SizeNameInnerPadding)
	textW := width - th.Size(theme.SizeNameInlineIcon) - 2*inner - 3*pad
	glyph := newCopyGlyph(sh, it.Message, theme.ColorNameForeground)
	head := container.NewBorder(nil, nil, nil,
		container.NewHBox(newTextLine(n.timeText(it.Time), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}), glyphSlot(glyph)),
		newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}))
	text := newSelParagraph(newParagraph(it.Message, theme.ColorNameForeground, textW), theme.ColorNameSelection,
		func(sel string) { sh.copyText(sel, false) })
	ic := widget.NewIcon(themedIcon(icon, c))
	line := inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewBorder(nil, nil, container.NewVBox(ic), nil, container.NewVBox(head, text)))
	if !n.fresh[it.ID] {
		return newNoteRow(line, glyph)
	}
	bg := newSurface(colorRowHover)
	return newNoteRow(container.NewStack(bg, line), glyph)
}

// timeText is when a notification was shown, in the user's zone and format (D-31): the time of day
// today, the date and the time of day before.
func (n *notificationsUI) timeText(t time.Time) string {
	return sameDayTime(n.sh.timeFormat(), t, n.sh.now())
}

// sameDayTime writes t as its time of day when it falls on now's day in f's zone, else with its
// date before (the notifications, the audit log).
func sameDayTime(f model.TimeFormat, t, now time.Time) string {
	if f.FormatDate(t) == f.FormatDate(now) {
		return f.FormatTime(t)
	}
	return f.Format(t)
}

// toastStyle is a level's colour, title and icon, as its toast shows them.
func toastStyle(level string) (fyne.ThemeColorName, string, fyne.Resource) {
	switch level {
	case engine.LevelError:
		return colorStatusError, "Error", theme.ErrorIcon()
	case engine.LevelWarning:
		return colorStatusWarning, "Warning", theme.WarningIcon()
	}
	return colorAccent, "Info", theme.InfoIcon()
}

// notificationPanel lies over the window while the panel is open: the card below the bell, at the
// window's right edge, and around it a transparent layer whose click closes the panel (a click on
// the bell too, which then does not open it again).
type notificationPanel struct {
	widget.BaseWidget
	n       *notificationsUI
	card    *canvas.Rectangle
	content *fyne.Container
}

var (
	_ fyne.Tappable          = (*notificationPanel)(nil)
	_ fyne.SecondaryTappable = (*notificationPanel)(nil)
)

func newNotificationPanel(n *notificationsUI) *notificationPanel {
	p := &notificationPanel{n: n, card: canvas.NewRectangle(color.Transparent), content: container.NewStack()}
	p.ExtendBaseWidget(p)
	return p
}

func (p *notificationPanel) setContent(o fyne.CanvasObject) {
	p.content.Objects = []fyne.CanvasObject{o}
	p.content.Refresh()
	p.Refresh()
}

// Tapped implements fyne.Tappable: a tap reaches the layer only outside the card's widgets.
func (p *notificationPanel) Tapped(ev *fyne.PointEvent) {
	pos, size := p.card.Position(), p.card.Size()
	if ev.Position.X < pos.X || ev.Position.X > pos.X+size.Width || ev.Position.Y < pos.Y || ev.Position.Y > pos.Y+size.Height {
		p.n.close()
	}
}

// TappedSecondary implements fyne.SecondaryTappable: a right-click outside closes it too.
func (p *notificationPanel) TappedSecondary(ev *fyne.PointEvent) { p.Tapped(ev) }

// CreateRenderer implements fyne.Widget.
func (p *notificationPanel) CreateRenderer() fyne.WidgetRenderer {
	return &notificationPanelRenderer{p: p}
}

type notificationPanelRenderer struct{ p *notificationPanel }

// Layout puts the card below the top bar, its right edge at the bell's.
func (r *notificationPanelRenderer) Layout(size fyne.Size) {
	p := r.p
	th := p.Theme()
	pad := th.Size(theme.SizeNamePadding)
	cs := p.content.MinSize()
	top := p.n.sh.top.root.Size().Height + pad
	right := size.Width - th.Size(theme.SizeNameInnerPadding)
	if b := p.n.bell; b.Visible() {
		right = fyne.CurrentApp().Driver().AbsolutePositionForObject(b).X + b.Size().Width
	}
	cs = fyne.NewSize(min(cs.Width, size.Width-2*pad), min(cs.Height, size.Height-top-pad))
	x := max(pad, min(right, size.Width-pad)-cs.Width)
	p.card.Move(fyne.NewPos(x, top))
	p.card.Resize(cs)
	p.content.Move(fyne.NewPos(x, top))
	p.content.Resize(cs)
}

func (r *notificationPanelRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *notificationPanelRenderer) Refresh() {
	p := r.p
	th := p.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	p.card.FillColor = th.Color(theme.ColorNameOverlayBackground, v)
	p.card.StrokeColor = themeColor(th, theme.ColorNameSeparator)
	p.card.StrokeWidth = 1
	p.card.CornerRadius = th.Size(theme.SizeNamePopupRadius)
	p.card.Shadow = canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 14,
		Offset: fyne.NewPos(0, 4), Variant: canvas.DropShadow}
	p.card.Refresh()
	r.Layout(p.Size())
	p.content.Refresh()
}

func (r *notificationPanelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.p.card, r.p.content}
}

func (r *notificationPanelRenderer) Destroy() {}

// bellButton is the bell in the top bar: a click opens or closes the panel, the badge counts the
// unread notifications, the tooltip says how many.
type bellButton struct {
	widget.BaseWidget
	n       *notificationsUI
	hovered bool
}

var (
	_ fyne.Tappable     = (*bellButton)(nil)
	_ desktop.Hoverable = (*bellButton)(nil)
)

func newBellButton(n *notificationsUI) *bellButton {
	b := &bellButton{n: n}
	b.ExtendBaseWidget(b)
	return b
}

// tipText is the bell's tooltip.
func (b *bellButton) tipText() string {
	if u := b.n.sh.ui.Notifications.Unread; u > 0 {
		return "Notifications: " + strconv.Itoa(u) + " unread"
	}
	return "Notifications"
}

// Tapped implements fyne.Tappable.
func (b *bellButton) Tapped(*fyne.PointEvent) {
	b.n.sh.tip.hideFor(b)
	b.n.toggle()
}

// MouseIn implements desktop.Hoverable.
func (b *bellButton) MouseIn(ev *desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
	b.n.sh.tip.hover(b, b.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *bellButton) MouseMoved(ev *desktop.MouseEvent) { b.n.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *bellButton) MouseOut() {
	b.hovered = false
	b.Refresh()
	b.n.sh.tip.hideFor(b)
}

// badgeText is the badge's count, "" for none.
func badgeText(unread int) string {
	switch {
	case unread <= 0:
		return ""
	case unread > 99:
		return countOverflow
	}
	return strconv.Itoa(unread)
}

// CreateRenderer implements fyne.Widget.
func (b *bellButton) CreateRenderer() fyne.WidgetRenderer {
	r := &bellRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), img: newIconImage(),
		badge: canvas.NewRectangle(color.Transparent), count: canvas.NewText("", color.Transparent)}
	r.count.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type bellRenderer struct {
	b     *bellButton
	bg    *canvas.Rectangle
	img   *iconImage
	badge *canvas.Rectangle
	count *canvas.Text
}

// bellIcon is the bell's size: a little more than an inline icon, as a bell fills less of its box
// than an arrow does.
func bellIcon(th fyne.Theme) float32 { return th.Size(theme.SizeNameInlineIcon) + 2 }

func (r *bellRenderer) MinSize() fyne.Size {
	th := r.b.Theme()
	side := bellIcon(th) + 2*th.Size(theme.SizeNameInnerPadding)
	return fyne.NewSize(side, side)
}

func (r *bellRenderer) Layout(size fyne.Size) {
	th := r.b.Theme()
	is := bellIcon(th)
	r.bg.Resize(size)
	ix, iy := (size.Width-is)/2, (size.Height-is)/2
	r.img.img.Move(fyne.NewPos(ix, iy))
	r.img.img.Resize(fyne.NewSize(is, is))
	// The badge sits on the bell's upper right, a pill around the count.
	ts := r.count.MinSize()
	h := ts.Height
	w := max(h, ts.Width+h/2)
	x := min(ix+is-w/2+1, size.Width-w)
	y := max(iy-h/2+2, 0)
	r.badge.Move(fyne.NewPos(x, y))
	r.badge.Resize(fyne.NewSize(w, h))
	r.badge.CornerRadius = h / 2
	r.count.Move(fyne.NewPos(x+(w-ts.Width)/2, y))
	r.count.Resize(ts)
}

func (r *bellRenderer) Refresh() {
	th := r.b.Theme()
	sum := r.b.n.sh.ui.Notifications
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	if r.b.hovered || r.b.n.visible() {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
	}
	r.img.set(th, iconBell, theme.ColorNameForeground)
	text := badgeText(sum.Unread)
	r.count.Text = text
	r.count.TextSize = th.Size(theme.SizeNameCaptionText) - 2
	if text == "" {
		r.badge.Hide()
		r.count.Hide()
	} else {
		c, _, _ := toastStyle(sum.UnreadLevel)
		r.badge.FillColor = themeColor(th, c)
		r.count.Color = themeColor(th, theme.ColorNameForegroundOnError)
		r.badge.Show()
		r.count.Show()
	}
	r.Layout(r.b.Size())
	r.bg.Refresh()
	r.badge.Refresh()
	r.count.Refresh()
}

func (r *bellRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.img.img, r.badge, r.count}
}

func (r *bellRenderer) Destroy() {}
