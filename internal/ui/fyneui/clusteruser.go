package fyneui

import (
	"image/color"
	"reflect"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The user Cogpit is logged in as, in the navigator root's subtitle (D-68, the owner, 2026-10-16;
// Cogpit's own: Lens's root row is not captured for this). Connected: the username the API server
// confirmed, and beside it a small box with an icon whose tooltip lists the user's groups (only
// the groups: no UID, no extra fields; no groups, no box). Where the API server does not know the
// review or refuses it: the kubeconfig's user entry's name with "(kubeconfig)", its tooltip "Not
// confirmed by the API server". Not connected: the last username the API server confirmed for the
// context, dimmed, its tooltip saying when it was last seen in the user's date and time format
// (D-31). Never confirmed: the kubeconfig file, as before. The demo cluster keeps "Synthetic demo
// cluster". The row stays the one hoverable thing: its tooltip is the box's or the user's while the
// pointer is on them (tipAt).

// kubeconfigMark marks a username the API server did not confirm.
const kubeconfigMark = " (kubeconfig)"

// notConfirmedTip is the tooltip of such a username.
const notConfirmedTip = "Not confirmed by the API server\nThe name of the context's user entry in its kubeconfig; its connection updates say why"

// iconGroups is two people, drawn for Cogpit: the user's groups.
var iconGroups = fyne.NewStaticResource("cogpit-groups.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<circle cx="9" cy="8" r="3.6"/><path d="M2 20v-1.2C2 15.3 5.1 13 9 13s7 2.3 7 5.8V20Z"/>`+
	`<circle cx="17.2" cy="9" r="2.8"/><path d="M15.9 13.5c3.4-.6 6.1 1.4 6.1 4.6V20h-4.4v-1.2c0-2-.6-3.8-1.7-5.3Z"/></svg>`))

// setSubtitle shows the root's second line: a text in a colour, its tooltip, and the groups' box
// (none without groups).
func (s *sidebar) setSubtitle(text string, c fyne.ThemeColorName, tip string, groups []string) {
	s.subtitle.SetText(text)
	s.subtitle.SetColor(c)
	s.userTip = tip
	s.groups.set(groups)
	if s.subLine != nil {
		s.subLine.Refresh()
	}
}

// showUser shows the user of the root's cluster; false when there is none to show (the subtitle
// then says where the context comes from).
func (s *sidebar) showUser(ui engine.UIState) bool {
	u := ui.User
	if u == nil || u.Context != ui.Context || u.Username == "" {
		return false
	}
	switch u.Source {
	case engine.UserFromAPIServer:
		s.setSubtitle(u.Username, colorMuted, "", u.Groups)
	case engine.UserFromKubeconfig:
		s.setSubtitle(u.Username+kubeconfigMark, colorMuted, notConfirmedTip, nil)
	case engine.UserLastSeen:
		s.setSubtitle(u.Username, theme.ColorNameDisabled, lastSeenTip(s.sh, u), nil)
	default:
		return false
	}
	return true
}

// userChanged shows the root's user and version again when only they changed (a renewed login, a
// reconnect to an upgraded API server, a day passing); a change of the context or the connection
// showed them already (connectionChanged).
func (s *sidebar) userChanged(st, old engine.UIState) {
	if st.Context == old.Context && st.Connection == old.Connection && (!reflect.DeepEqual(st.User, old.User) || versionChanged(st, old)) {
		s.updateHeader()
	}
}

// lastSeenTip is a last known user's tooltip: "Last seen 16.10.2026 09:12" in the user's format.
func lastSeenTip(sh *shell, u *engine.ClusterUser) string {
	tf := sh.timeFormat()
	return "Last seen " + tf.FormatDate(u.LastSeen) + " " + tf.FormatClock(u.LastSeen) +
		"\nThe user the API server last confirmed for this cluster"
}

// tipAt is the root's tooltip where the pointer is: the groups' box lists the groups, the user's
// name says what it is when it is not the API server's word and is whole when the line cuts it
// (the navigator's default width, the gear beside it, D-92), the version its upstream support,
// the rest of the row is the row's.
func (r *navRootRow) tipAt(abs fyne.Position) string {
	s := r.s
	if s.groups.Visible() && pointerOn(s.groups, abs) {
		return s.groups.tip()
	}
	if tip := s.subtitleTip(); tip != "" && s.subLine.Visible() && pointerOn(s.subtitle, abs) {
		return tip
	}
	if s.versionTip != "" && pointerOn(s.version, abs) {
		return s.versionTip // the version's upstream support (clusterversion.go, D-91)
	}
	return r.tipText()
}

// subtitleTip is the subtitle's tooltip: its whole text first when the line cuts it, then what it
// is (userTip); "" for a whole text with nothing to say.
func (s *sidebar) subtitleTip() string {
	t := s.subtitle
	if t.text == "" || fyne.MeasureText(t.text, t.Theme().Size(t.size), t.style).Width <= t.Size().Width+0.5 {
		return s.userTip
	}
	if s.userTip == "" {
		return t.text
	}
	return t.text + "\n" + s.userTip
}

// pointerOn reports whether an absolute position lies on a shown object.
func pointerOn(o fyne.CanvasObject, abs fyne.Position) bool {
	app := fyne.CurrentApp()
	if app == nil || !o.Visible() {
		return false
	}
	p, sz := app.Driver().AbsolutePositionForObject(o), o.Size()
	return abs.X >= p.X && abs.X < p.X+sz.Width && abs.Y >= p.Y && abs.Y < p.Y+sz.Height
}

// userLineLayout lays the subtitle out with the groups' box right after its text: the text takes
// what it needs, cut with an ellipsis when the box would not fit beside it.
type userLineLayout struct {
	text *textLine
	box  *groupsBox
}

func (l *userLineLayout) gap() float32 { return l.text.Theme().Size(theme.SizeNameInnerPadding) / 2 }

func (l *userLineLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	h := l.text.MinSize().Height
	if l.box.Visible() {
		h = fyne.Max(h, l.box.MinSize().Height)
	}
	return fyne.NewSize(0, h) // any width: the text is cut to fit
}

func (l *userLineLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	th := l.text.Theme()
	natural := fyne.MeasureText(l.text.text, th.Size(l.text.size), l.text.style).Width
	textW := size.Width
	if l.box.Visible() {
		bs := l.box.MinSize()
		textW = fyne.Max(0, fyne.Min(natural, size.Width-bs.Width-l.gap()))
		l.box.Move(fyne.NewPos(textW+l.gap(), (size.Height-bs.Height)/2))
		l.box.Resize(bs)
	}
	l.text.Move(fyne.NewPos(0, 0))
	l.text.Resize(fyne.NewSize(textW, size.Height))
}

// groupsBox is the small box beside a username: an icon for the user's groups, which its tooltip
// lists. Hidden without groups.
type groupsBox struct {
	widget.BaseWidget
	groups []string
}

func newGroupsBox() *groupsBox {
	b := &groupsBox{}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

// set shows a user's groups; none hides the box.
func (b *groupsBox) set(groups []string) {
	if len(groups) == 0 {
		b.groups = nil
		b.Hide()
		return
	}
	changed := strings.Join(groups, "\n") != strings.Join(b.groups, "\n")
	b.groups = append([]string(nil), groups...)
	b.Show()
	if changed {
		b.Refresh()
	}
}

// tip lists the groups, one a line.
func (b *groupsBox) tip() string { return strings.Join(b.groups, "\n") }

// CreateRenderer implements fyne.Widget.
func (b *groupsBox) CreateRenderer() fyne.WidgetRenderer {
	r := &groupsBoxRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), img: newIconImage()}
	r.Refresh()
	return r
}

type groupsBoxRenderer struct {
	b   *groupsBox
	bg  *canvas.Rectangle
	img *iconImage
}

// sizes are the icon's side and the box's padding around it: as high as a caption line.
func (r *groupsBoxRenderer) sizes() (icon, pad float32) {
	th := r.b.Theme()
	line := fyne.MeasureText("Mg", th.Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Height
	pad = 2
	return line - 2*pad, pad
}

func (r *groupsBoxRenderer) MinSize() fyne.Size {
	icon, pad := r.sizes()
	return fyne.NewSize(icon+4*pad, icon+2*pad)
}

func (r *groupsBoxRenderer) Layout(size fyne.Size) {
	icon, _ := r.sizes()
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.img.img.Move(fyne.NewPos((size.Width-icon)/2, (size.Height-icon)/2))
	r.img.img.Resize(fyne.NewSquareSize(icon))
}

func (r *groupsBoxRenderer) Refresh() {
	th := r.b.Theme()
	r.bg.FillColor = withAlpha(themeColor(th, colorMuted), 0x1c)
	r.bg.StrokeColor = withAlpha(themeColor(th, colorMuted), 0x80)
	r.bg.StrokeWidth = 1
	r.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.img.set(th, iconGroups, colorMuted)
	r.Layout(r.b.Size())
	r.bg.Refresh()
}

func (r *groupsBoxRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.img.img}
}
func (r *groupsBoxRenderer) Destroy() {}
