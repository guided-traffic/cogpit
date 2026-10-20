package fyneui

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Workloads Overview's upper part (resource-parity › Workloads › Overview, captured 2026-10-11):
// one link per workload kind with its count and a health bar, in two columns. The engine builds the
// model from the cache (workloads.overview); the events below are the page's table, which the
// table view shows as it shows any view's.

// overviewPanel shows the kinds of the Workloads Overview.
type overviewPanel struct {
	sh    *shell
	cols  [2]*fyne.Container
	tiles map[string]*overviewTile
	root  fyne.CanvasObject

	// Requests of the model are coalesced: one in flight, and one more if something changed
	// meanwhile. 0 idle, 1 in flight, 2 in flight with another to follow.
	requests atomic.Int32
}

func newOverviewPanel(sh *shell) *overviewPanel {
	p := &overviewPanel{sh: sh, tiles: map[string]*overviewTile{}}
	p.cols[0], p.cols[1] = container.NewVBox(), container.NewVBox()
	p.root = inset(theme.SizeNameInnerPadding, theme.SizeNamePadding,
		container.NewGridWithColumns(2, p.cols[0], p.cols[1]))
	return p
}

// request reads the model off the main goroutine and shows it. It may be called from the main
// goroutine at any rate: requests made while one is in flight become one more.
func (p *overviewPanel) request() {
	for {
		n := p.requests.Load()
		if n >= 2 {
			return
		}
		if p.requests.CompareAndSwap(n, n+1) {
			if n == 1 {
				return // the one in flight runs again when it is done
			}
			break
		}
	}
	p.sh.dispatch("workloads.overview", struct{}{}, func(res any, err error) {
		again := p.requests.Swap(0) == 2
		if err == nil {
			var m engine.WorkloadsOverviewModel
			if derr := decode(res, &m); derr == nil {
				p.show(m)
			}
		}
		// An error leaves the last counts: the page's events still show, and the next change of a
		// workload asks again.
		if again && engine.IsPage(p.sh.ui.View) {
			p.request()
		}
	})
}

// show makes the tiles show m: one per kind the cluster serves, in its column and order.
func (p *overviewPanel) show(m engine.WorkloadsOverviewModel) {
	var cols [2][]fyne.CanvasObject
	seen := map[string]bool{}
	for _, k := range m.Kinds {
		t := p.tiles[k.Kind]
		if t == nil {
			t = newOverviewTile(p.sh, k.Kind)
			p.tiles[k.Kind] = t
		}
		t.set(k)
		seen[k.Kind] = true
		col := min(max(k.Column, 0), 1)
		cols[col] = append(cols[col], t)
	}
	for key := range p.tiles {
		if !seen[key] {
			delete(p.tiles, key)
		}
	}
	for i := range cols {
		p.cols[i].Objects = cols[i]
		p.cols[i].Refresh()
	}
}

// overviewTile is one kind of the overview: its title as a link to its view, its count, and the
// health bar below.
type overviewTile struct {
	widget.BaseWidget
	sh    *shell
	kind  string
	link  *linkText
	count *textLine
	bar   *healthBar
}

func newOverviewTile(sh *shell, kind string) *overviewTile {
	t := &overviewTile{sh: sh, kind: kind}
	t.link = newLinkText("", func() { sh.dispatch("ui.openView", openViewParams{View: kind}, nil) })
	t.count = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	t.bar = newHealthBar(sh)
	t.ExtendBaseWidget(t)
	return t
}

func (t *overviewTile) set(k engine.OverviewKind) {
	if t.link.text != k.Title {
		t.link.text = k.Title
		t.link.Refresh()
	}
	count := formatCount(int64(k.Count))
	if !k.Synced {
		count += " …"
	}
	t.count.SetText(count)
	t.bar.set(k.Statuses, k.Count)
}

// CreateRenderer implements fyne.Widget.
func (t *overviewTile) CreateRenderer() fyne.WidgetRenderer {
	line := container.NewBorder(nil, nil, t.link, t.count)
	return widget.NewSimpleRenderer(inset(theme.SizeNamePadding, theme.SizeNamePadding,
		container.NewVBox(line, t.bar)))
}

// healthBar draws a kind's objects by status token: one segment per token, as wide as its share,
// in the token's colour; a kind without objects shows an empty track. Hovering it names the counts.
type healthBar struct {
	widget.BaseWidget
	sh       *shell
	statuses []engine.StatusCount
	total    int
	// names name the tokens in the tooltip where the page has its own words for them (the Flux
	// dashboard's states, fluxdashboard.go); nil: the tokens' own names.
	names map[model.Status]string
}

var _ desktop.Hoverable = (*healthBar)(nil)

func newHealthBar(sh *shell) *healthBar {
	b := &healthBar{sh: sh}
	b.ExtendBaseWidget(b)
	return b
}

func (b *healthBar) set(statuses []engine.StatusCount, total int) {
	b.statuses, b.total = statuses, total
	b.Refresh()
}

// hasState reports whether the bar has a state to draw: a kind with no objects, or whose objects
// have no status at all (an Argo CD project, a repository), shows no bar and no empty track.
func (b *healthBar) hasState() bool {
	if b.total == 0 {
		return false
	}
	for _, s := range b.statuses {
		if s.Status != model.StatusNone && s.Count > 0 {
			return true
		}
	}
	return false
}

// tipText names the segments: "2 ok · 1 pending · 1 error".
func (b *healthBar) tipText() string {
	if b.total == 0 {
		return "none"
	}
	parts := make([]string, 0, len(b.statuses))
	for _, s := range b.statuses {
		name := string(s.Status)
		switch {
		case b.names[s.Status] != "":
			name = b.names[s.Status]
		case s.Status == model.StatusNone:
			name = "no status"
		}
		parts = append(parts, fmt.Sprintf("%s %s", formatCount(int64(s.Count)), name))
	}
	return strings.Join(parts, " · ")
}

// MouseIn implements desktop.Hoverable: the counts in a tooltip.
func (b *healthBar) MouseIn(ev *desktop.MouseEvent) {
	b.sh.tip.hover(b, b.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *healthBar) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *healthBar) MouseOut() { b.sh.tip.hideFor(b) }

// CreateRenderer implements fyne.Widget.
func (b *healthBar) CreateRenderer() fyne.WidgetRenderer {
	r := &healthBarRenderer{b: b, track: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type healthBarRenderer struct {
	b     *healthBar
	track *canvas.Rectangle
	segs  []*canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (r *healthBarRenderer) MinSize() fyne.Size {
	if !r.b.hasState() {
		return fyne.NewSize(0, 0) // nothing to say about states: no bar
	}
	return fyne.NewSize(0, r.b.Theme().Size(sizeHealthBar))
}

func (r *healthBarRenderer) Layout(size fyne.Size) {
	h := r.b.Theme().Size(sizeHealthBar)
	y := (size.Height - h) / 2
	r.track.Move(fyne.NewPos(0, y))
	r.track.Resize(fyne.NewSize(size.Width, h))
	x := float32(0)
	for i, s := range r.b.statuses {
		if i >= len(r.segs) {
			break
		}
		w := float32(0)
		if r.b.total > 0 {
			w = size.Width * float32(s.Count) / float32(r.b.total)
		}
		// Tokens, not one smeared bar (Dark Cockpit): a hairline gap parts the states.
		gap := float32(2)
		if i == len(r.b.statuses)-1 || w < 6 {
			gap = 0
		}
		r.segs[i].Move(fyne.NewPos(x, y))
		r.segs[i].Resize(fyne.NewSize(max(w-gap, 1), h))
		x += w
	}
}

func (r *healthBarRenderer) Refresh() {
	th := r.b.Theme()
	radius := th.Size(sizeHealthBar) / 2
	r.track.FillColor = themeColor(th, theme.ColorNameSeparator)
	r.track.CornerRadius = radius
	r.track.Hidden = !r.b.hasState()
	for len(r.segs) < len(r.b.statuses) {
		r.segs = append(r.segs, canvas.NewRectangle(color.Transparent))
	}
	r.objs = append(r.objs[:0], r.track)
	for i, s := range r.b.statuses {
		c := statusColorName(s.Status)
		if s.Status == model.StatusNone {
			c = colorMuted
		}
		fill := themeColor(th, c)
		if s.Status == model.StatusOK || s.Status == model.StatusSucceeded {
			fill = withAlpha(fill, 0x6a) // what is healthy recedes; the light is for what is not
		}
		r.segs[i].FillColor = fill
		r.segs[i].Hidden = !r.b.hasState()
		r.segs[i].CornerRadius = min(radius, 3.5)
		r.segs[i].Refresh()
		r.objs = append(r.objs, r.segs[i])
	}
	r.Layout(r.b.Size())
	r.track.Refresh()
}

func (r *healthBarRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *healthBarRenderer) Destroy()                     {}
