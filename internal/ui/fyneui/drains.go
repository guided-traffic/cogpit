package fyneui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// drainBanner shows the connected context's unfinished drains below the top bar, one row each
// (D-11): what happened to the node, and Finish drain (node.drain again, with the settings it
// was started with, confirmed as usual), Uncordon (node.uncordon, while the node is cordoned) and
// Dismiss (node.dismissDrain). A drain running in this window has no row: it runs as a job, whose
// tab in the dock shows its steps and Cancel (D-63), so that the two never show the same thing;
// once it stops or fails, its row is here. A drain another Cogpit window runs shows as running,
// without actions. The engine announces changes with EventDrains; the banner reads the drains with
// node.pendingDrains, so that it shows what an agent would read.
type drainBanner struct {
	sh     *shell
	root   *fyne.Container // the rows; hidden while there are none
	queued atomic.Bool     // a re-read is queued; any goroutine
	drains []engine.PendingDrainInfo
}

func newDrainBanner(sh *shell) *drainBanner {
	b := &drainBanner{sh: sh, root: container.NewVBox()}
	b.root.Hide()
	return b
}

// request re-reads the pending drains. Any goroutine may call it; calls while one is queued are
// covered by it.
func (b *drainBanner) request() {
	if !b.queued.CompareAndSwap(false, true) {
		return
	}
	b.sh.do(func() {
		b.queued.Store(false)
		b.sh.dispatch("node.pendingDrains", struct{}{}, b.show)
	})
}

// show renders a node.pendingDrains result. An error (no cluster connected, no journal, an
// unreadable journal, which the engine reports itself) empties the banner.
func (b *drainBanner) show(res any, err error) {
	var list []engine.PendingDrainInfo
	if err == nil {
		err = decode(res, &list)
	}
	if err != nil {
		list = nil
	}
	// A drain running in this window: its job tab shows it.
	b.drains = slices.DeleteFunc(list, func(d engine.PendingDrainInfo) bool {
		return d.Status == engine.DrainStatusRunning && !d.Elsewhere
	})
	b.render()
}

func (b *drainBanner) render() {
	rows := make([]fyne.CanvasObject, 0, len(b.drains)+1)
	for _, d := range b.drains {
		rows = append(rows, b.row(d))
	}
	if len(rows) > 0 {
		rows = append(rows, newDivider(false))
	}
	b.root.Objects = rows
	if len(rows) == 0 {
		b.root.Hide()
	} else {
		b.root.Show()
	}
	b.root.Refresh()
}

// drainNodeKind is the kind key of nodes.
const drainNodeKind = "nodes"

// drainActionParams mirror node.drain's parameters.
type drainActionParams struct {
	Ref model.ObjectRef `json:"ref"`
	engine.DrainSettings
}

// row is one drain: a tinted strip with an icon, two lines of text and the actions.
func (b *drainBanner) row(d engine.PendingDrainInfo) fyne.CanvasObject {
	ref := model.ObjectRef{Context: d.Context, Kind: drainNodeKind, Name: d.Node}
	tint, icon := colorStatusWarning, theme.WarningIcon()
	if d.Status == engine.DrainStatusRunning {
		tint, icon = colorAccent, theme.InfoIcon()
	}
	title := newTextLine(drainTitle(d), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	title.truncate = true
	detail := newTextLine(drainDetail(d, b.sh.timeFormat()), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	detail.truncate = true

	buttons := container.NewHBox()
	if d.Status != engine.DrainStatusRunning {
		dismiss := newTextButton("Dismiss", nil, func() { b.sh.dispatch("node.dismissDrain", refParams{Ref: ref}, nil) })
		buttons.Add(dismiss)
		if d.NodeState == engine.DrainNodeCordoned {
			buttons.Add(widget.NewButton("Uncordon", func() { b.sh.dispatch("node.uncordon", refParams{Ref: ref}, nil) }))
		}
		finish := widget.NewButtonWithIcon("Finish drain", theme.MediaPlayIcon(), func() {
			b.sh.dispatch("node.drain", drainActionParams{Ref: ref, DrainSettings: d.Settings}, nil)
		})
		finish.Importance = widget.HighImportance
		buttons.Add(finish)
	}
	content := container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(themedIcon(icon, tint))),
		container.NewCenter(buttons),
		container.NewVBox(title, detail))
	return container.NewStack(newBannerTint(tint), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// drainTitle is a row's first line.
func drainTitle(d engine.PendingDrainInfo) string {
	switch d.Status {
	case engine.DrainStatusRunning:
		return "Draining node " + d.Node
	case engine.DrainStatusFailed:
		return "The drain of node " + d.Node + " failed"
	case engine.DrainStatusStopped:
		return "The drain of node " + d.Node + " was stopped"
	}
	return "The drain of node " + d.Node + " did not finish"
}

// drainDetail is a row's second line: when and by whom the drain was started, in which context,
// the state the node is in, and why the drain did not finish (its error, or why it was stopped).
// The cause comes last because it can be long and is cut off first; the full text is in
// node.pendingDrains. The time is written in tf, the user's zone and format (D-31).
func drainDetail(d engine.PendingDrainInfo, tf model.TimeFormat) string {
	parts := []string{fmt.Sprintf("Context %s, started %s by %s", d.Context, tf.Format(d.Started), starterText(d.StartedBy))}
	if d.Status == engine.DrainStatusRunning { // in another window; this window's are in their job tabs
		if d.Elsewhere && d.Owner != nil {
			parts = append(parts, fmt.Sprintf("in another Cogpit window (process %d)", d.Owner.PID))
		}
		return strings.Join(parts, " · ")
	}
	switch d.NodeState {
	case engine.DrainNodeCordoned:
		parts = append(parts, "the node is cordoned and partly drained")
	case engine.DrainNodeSchedulable:
		parts = append(parts, "the node is schedulable again")
	case engine.DrainNodeMissing:
		parts = append(parts, "the node no longer exists")
	}
	switch d.Status {
	case engine.DrainStatusFailed:
		parts = append(parts, strings.TrimSuffix(strings.TrimPrefix(d.Error, "drain node "+d.Node+": "), "."))
	case engine.DrainStatusStopped:
		parts = append(parts, strings.TrimSuffix(d.Error, "."))
	default:
		parts = append(parts, "Cogpit ended before the drain did")
	}
	return strings.Join(parts, " · ")
}

// starterText names who started a drain, for the banner.
func starterText(o engine.Origin) string {
	switch o {
	case engine.OriginGUI:
		return "you"
	case engine.OriginCLI:
		return "cogpit-cli"
	case engine.OriginExternalAgent:
		return "an external agent"
	case engine.OriginBuiltInAgent:
		return "the built-in agent"
	}
	return "Cogpit"
}

// bannerTint is a banner row's background: its status hue, translucent, so that the row reads as
// a notice in both themes. The colour comes from the theme on every refresh.
type bannerTint struct {
	widget.BaseWidget
	color fyne.ThemeColorName
}

func newBannerTint(c fyne.ThemeColorName) *bannerTint {
	t := &bannerTint{color: c}
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer implements fyne.Widget.
func (t *bannerTint) CreateRenderer() fyne.WidgetRenderer {
	r := &bannerTintRenderer{t: t, rect: canvas.NewRectangle(color.Transparent), edge: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type bannerTintRenderer struct {
	t          *bannerTint
	rect, edge *canvas.Rectangle
}

func (r *bannerTintRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
	w := r.t.Theme().Size(theme.SizeNamePadding) / 2 // a slim stripe at the leading edge
	r.edge.Move(fyne.NewPos(0, 0))
	r.edge.Resize(fyne.NewSize(w, size.Height))
}

func (r *bannerTintRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *bannerTintRenderer) Refresh() {
	c := themeColor(r.t.Theme(), r.t.color)
	r.rect.FillColor = withAlpha(c, 0x24)
	r.edge.FillColor = c
	r.Layout(r.t.Size())
	r.rect.Refresh()
	r.edge.Refresh()
}

func (r *bannerTintRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rect, r.edge}
}
func (r *bannerTintRenderer) Destroy() {}
